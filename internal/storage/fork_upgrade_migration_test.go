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
	for _, test := range []struct {
		name   string
		nextID string
		count  int
	}{
		{name: "24_to_32", nextID: migrationfiles.ID0025, count: 24},
		{name: "27_to_32", nextID: migrationfiles.ID0028, count: 27},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testForkMigrationUpgrade(t, test.nextID, test.count)
		})
	}
}

func testForkMigrationUpgrade(t *testing.T, nextID string, publishedCount int) {
	t.Helper()
	db := openInternalMigrationTestDatabase(t)
	index := migrationIndexByID(t, nextID)
	if index != publishedCount {
		t.Fatalf("published migration prefix length = %d, want %d", index, publishedCount)
	}
	if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
		t.Fatalf("create published fork schema: %v", err)
	}
	if db.Migrator().HasTable("proxies") ||
		db.Migrator().HasColumn("credentials", "name") ||
		db.Migrator().HasColumn("groups", "priority") ||
		db.Migrator().HasColumn("request_logs", "client_ip") {
		t.Fatal("published schema already contains pending upstream migrations")
	}
	if publishedCount == 27 && (!db.Migrator().HasColumn("request_logs", "first_output_ms") ||
		!db.Migrator().HasColumn("request_logs", "last_output_ms")) {
		t.Fatal("published 0027 schema is missing its output timing columns")
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
		query := db
		switch fixture.(type) {
		case *models.Group:
			query = db.Omit("Priority")
		case *models.Credential:
			query = db.Omit("Name")
		}
		if err := query.Create(fixture).Error; err != nil {
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
	if err := db.Omit("ClientIP").Create(&request).Error; err != nil {
		t.Fatalf("create legacy request: %v", err)
	}
	if publishedCount == 27 {
		if err := db.Table("request_logs").Where("id = ?", request.ID).
			Updates(map[string]any{"first_output_ms": 100, "last_output_ms": 800}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("request_log_attempts").Create(map[string]any{
		"request_id": request.ID, "sequence": 1, "completed_at_ms": request.CompletedAtMS,
		"group_id": 17, "group_name": "fork group", "credential_id": credentialID,
		"status_code": 200, "duration_ms": 900, "failure_category": "ok", "action": "terminate",
		"error_summary": "", "pricing_receipt": `{"preserved":true}`,
	}).Error; err != nil {
		t.Fatalf("create legacy request attempt: %v", err)
	}

	var oldLedger []string
	if err := db.Table(migrationLedgerTable).Order("id").Pluck("id", &oldLedger).Error; err != nil {
		t.Fatal(err)
	}
	if len(oldLedger) != publishedCount {
		t.Fatalf("old ledger length = %d, want %d", len(oldLedger), publishedCount)
	}
	// Preserve all original columns except the explicitly retired output timing,
	// including identity, ciphertext, timestamps, notes, donation receipts and usage.
	snapshots := make(map[string]forkUpgradeTableSnapshot)
	for _, table := range []string{
		"groups", "credentials", "access_keys", "request_logs", "request_log_attempts", "usage_stats", "schema_migrations",
		"donation_identities", "donation_batches", "donation_items", "donation_resources",
		"donation_review_actions", "donation_retries",
	} {
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, column := range columns {
			if table == "request_logs" && (column.Name() == "first_output_ms" || column.Name() == "last_output_ms") {
				continue
			}
			names = append(names, column.Name())
		}
		snapshots[table] = forkUpgradeTableSnapshot{columns: names, rows: readForkUpgradeRows(t, db, table, names)}
		if table != "schema_migrations" && len(snapshots[table].rows) != 1 {
			t.Fatalf("fixture %s does not contain exactly one row", table)
		}
	}
	wantLedger := slices.Clone(oldLedger)
	if publishedCount == 24 {
		wantLedger = append(wantLedger, "0025_rpm_stats", "0026_access_key_concurrency", "0027_request_log_output_timing")
	}
	wantLedger = append(wantLedger, "0028_proxy_catalog", "0029_credential_names", "0030_remove_request_log_output_timing", "0031_request_log_client_ip", "0032_group_priority")
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
			query := db
			if table == "schema_migrations" {
				query = db.Where("id IN ?", oldLedger)
			}
			if rows := readForkUpgradeRows(t, query, table, snapshot.columns); !reflect.DeepEqual(rows, snapshot.rows) {
				t.Errorf("migration run %d changed existing %s rows", run+1, table)
			}
		}
		var rpmRows int64
		if err := db.Table("rpm_stats").Count(&rpmRows).Error; err != nil || rpmRows != 0 {
			t.Fatalf("RPM history after migration run %d: rows = %d, error = %v", run+1, rpmRows, err)
		}
		var defaults struct {
			ConcurrencyLimit *int64
			Name             string
			Priority         int32
			ClientIP         *string
		}
		if err := db.Raw(`SELECT access_keys.concurrency_limit, credentials.name, groups.priority, request_logs.client_ip
			FROM request_logs JOIN access_keys ON request_logs.access_key_id = access_keys.id
			JOIN credentials ON request_logs.credential_id = credentials.id
			JOIN groups ON credentials.group_id = groups.id`).Scan(&defaults).Error; err != nil {
			t.Fatal(err)
		}
		if defaults.ConcurrencyLimit != nil || defaults.Name != "" || defaults.Priority != 0 || defaults.ClientIP != nil {
			t.Fatalf("migration run %d invented legacy values: %+v", run+1, defaults)
		}
		assertOutputTimingRemoved(t, db)
		if !db.Migrator().HasTable("proxies") {
			t.Fatal("proxy catalog is missing")
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
	if table == migrationLedgerTable {
		db = db.Order("id")
	}
	if err := db.Table(table).Select(columns).Find(&rows).Error; err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return rows
}
