package storage

import (
	"reflect"
	"slices"
	"testing"

	"gorm.io/gorm"

	migrationfiles "gpt-load/internal/storage/migrations"
	"gpt-load/internal/storage/models"
)

func TestForkMigrationUpgradePreservesPublishedLedgerAndData(t *testing.T) {
	t.Parallel()
	db := openInternalMigrationTestDatabase(t)
	index := migrationIndexByID(t, migrationfiles.ID0025)
	if index != 24 {
		t.Fatalf("published migration prefix length = %d, want 24", index)
	}
	if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
		t.Fatalf("create published fork schema: %v", err)
	}
	if db.Migrator().HasTable("rpm_stats") ||
		db.Migrator().HasColumn("access_keys", "concurrency_limit") ||
		db.Migrator().HasColumn("request_logs", "first_output_ms") ||
		db.Migrator().HasColumn("request_logs", "last_output_ms") {
		t.Fatal("published schema already contains pending upstream migrations")
	}

	credentialID, itemID, acceptedAtMS := uint(29), uint(37), int64(123456)
	fixtures := []any{
		&models.Group{
			ID: 17, Name: "fork group", ChannelID: "openai", ConnectionType: models.ConnectionTypeAPIKey,
			Params: models.JSON(`{"base_url":"https://example.invalid"}`), Models: models.JSON(`["test-model"]`),
			Overrides: models.JSON(`{"rate_limit_reset_hint_enabled":true}`), CreatedAtMS: 101, UpdatedAtMS: 102,
		},
		&models.Credential{
			ID: credentialID, GroupID: 17, Data: "synthetic-encrypted-credential", Fingerprint: "credential-fingerprint",
			IdentityFingerprint: "stable-credential-identity", SecretVersion: 7, Note: "保留人工备注",
			CreatedAtMS: 103, UpdatedAtMS: 104,
		},
		&models.DonationIdentity{ID: 1, InstanceID: "instance-identity", SourceID: "source-identity", KeyProof: "synthetic-key-proof", CreatedAtMS: 105},
		&models.DonationBatch{
			ID: 31, SourceID: "source-identity", BatchID: "batch-identity", RequestDigest: "immutable-request-digest",
			GroupID: 17, GroupName: "fork group", ChannelID: "openai", TargetRevision: "target-revision",
			ValidationMode: "manual_review", CreatedAtMS: 106,
		},
		&models.DonationItem{
			ID: itemID, SourceID: "source-identity", ItemID: "item-identity", BatchID: "batch-identity", GroupID: 17,
			Fingerprint: "donation-fingerprint", CredentialFingerprint: "credential-fingerprint", EncryptedPayload: "synthetic-staged-payload",
			ValidationRevision: "validation-revision", State: "accepted", EffectiveMode: "manual_review", ItemRevision: 3,
			ReviewTargetRevision: "target-revision", ReviewActionID: "action-identity", ReviewDecision: "approved",
			ReviewedAtMS: acceptedAtMS, CredentialID: &credentialID, AcceptedAtMS: &acceptedAtMS,
			CreatedAtMS: 107, UpdatedAtMS: 108,
		},
		&models.DonationResource{
			Fingerprint: "donation-fingerprint", OwnerItemID: &itemID, Origin: "donation", GroupID: 17,
			CredentialID: &credentialID, AcquiredAtMS: &acceptedAtMS, CreatedAtMS: 109, UpdatedAtMS: 110,
		},
		&models.DonationReviewAction{
			ID: 41, SourceID: "source-identity", ActionID: "action-identity", BatchID: "batch-identity", ItemID: "item-identity",
			Kind: "approve", Actor: "test-admin", ExpectedRevision: 2, TargetRevision: "target-revision", Note: "保留审核记录",
			RequestDigest: "review-request-digest", Outcome: "applied", EffectRevision: 3, CreatedAtMS: 111, AppliedAtMS: acceptedAtMS,
		},
		&models.DonationRetry{ID: 43, SourceID: "source-identity", IdempotencyKey: "retry-identity", BatchID: "batch-identity", RequestDigest: "retry-digest", CreatedAtMS: 112},
		&models.UsageStat{
			ID: 47, BucketStartMS: 3_600_000, AccessKeyID: 53, GroupID: 17, ChannelID: "openai", CredentialID: credentialID,
			Model: "test-model", RequestCount: 1, SuccessCount: 1, UncachedInputTokens: 17, OutputTokens: 23, CacheReadTokens: 31,
			CacheWrite5MTokens: 37, CacheWrite1HTokens: 41, CacheWriteUnknownTokens: 43,
		},
	}
	for _, fixture := range fixtures {
		if err := db.Create(fixture).Error; err != nil {
			t.Fatalf("create legacy %T: %v", fixture, err)
		}
	}
	key := models.AccessKey{ID: 53, Name: "existing key", KeyValue: "synthetic-encrypted-key", KeyHash: "access-key-hash", KeySuffix: "cafe", CreatedAtMS: 113, UpdatedAtMS: 114}
	if err := db.Omit("ConcurrencyLimit").Create(&key).Error; err != nil {
		t.Fatalf("create legacy access key: %v", err)
	}
	request := models.RequestLog{
		ID: "request-identity", CompletedAtMS: 3_600_123, AccessKeyID: key.ID, GroupID: 17, ChannelID: "openai", CredentialID: credentialID,
		Protocol: "openai-responses", ClientModel: "test-model", UpstreamModel: "test-model", Status: "success", StatusCode: 200,
		DurationMs: 900, OutputTokens: 23,
	}
	if err := db.Omit("FirstOutputMs", "LastOutputMs").Create(&request).Error; err != nil {
		t.Fatalf("create legacy request: %v", err)
	}

	var oldLedger []string
	if err := db.Table(migrationLedgerTable).Order("id").Pluck("id", &oldLedger).Error; err != nil {
		t.Fatal(err)
	}
	if len(oldLedger) != 24 {
		t.Fatalf("old ledger length = %d, want 24", len(oldLedger))
	}
	// Preserve every original column, including identity, ciphertext, timestamps,
	// notes, immutable donation receipts and cached-token usage components.
	snapshots := make(map[string]forkUpgradeTableSnapshot)
	for _, table := range []string{
		"groups", "credentials", "access_keys", "request_logs", "usage_stats",
		"donation_identities", "donation_batches", "donation_items", "donation_resources",
		"donation_review_actions", "donation_retries",
	} {
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, column := range columns {
			names = append(names, column.Name())
		}
		snapshots[table] = forkUpgradeTableSnapshot{columns: names, rows: readForkUpgradeRows(t, db, table, names)}
		if len(snapshots[table].rows) != 1 {
			t.Fatalf("fixture %s does not contain exactly one row", table)
		}
	}
	wantLedger := append(slices.Clone(oldLedger), "0025_rpm_stats", "0026_access_key_concurrency", "0027_request_log_output_timing")
	for run := range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatalf("migration run %d: %v", run+1, err)
		}
		var ledger []string
		if err := db.Table(migrationLedgerTable).Order("id").Pluck("id", &ledger).Error; err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ledger, wantLedger) {
			t.Fatalf("migration run %d ledger = %v, want %v", run+1, ledger, wantLedger)
		}
		for table, snapshot := range snapshots {
			if rows := readForkUpgradeRows(t, db, table, snapshot.columns); !reflect.DeepEqual(rows, snapshot.rows) {
				t.Errorf("migration run %d changed existing %s rows", run+1, table)
			}
		}
		var rpmRows int64
		if err := db.Table("rpm_stats").Count(&rpmRows).Error; err != nil || rpmRows != 0 {
			t.Fatalf("RPM history after migration run %d: rows = %d, error = %v", run+1, rpmRows, err)
		}
		var defaults struct {
			ConcurrencyLimit *int64
			FirstOutputMs    *int64
			LastOutputMs     *int64
		}
		if err := db.Raw(`SELECT concurrency_limit, first_output_ms, last_output_ms
			FROM access_keys JOIN request_logs ON request_logs.access_key_id = access_keys.id`).Scan(&defaults).Error; err != nil {
			t.Fatal(err)
		}
		if defaults.ConcurrencyLimit != nil || defaults.FirstOutputMs != nil || defaults.LastOutputMs != nil {
			t.Fatalf("migration run %d invented legacy concurrency or output timing: %+v", run+1, defaults)
		}
	}
}

type forkUpgradeTableSnapshot struct {
	columns []string
	rows    []map[string]any
}

func readForkUpgradeRows(t *testing.T, db *gorm.DB, table string, columns []string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := db.Table(table).Select(columns).Find(&rows).Error; err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return rows
}
