package migrations_test

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"gpt-load/internal/storage/migrations"
)

func TestCredentialNoteMigrationAddsColumnWithoutRewritingExistingRows(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := db.Exec(`INSERT INTO groups (
		id, name, channel_id, connection_type, params, models, enabled, created_at_ms, updated_at_ms
	) VALUES (1, 'credential note', 'openai', 'api_key', '{}', '[]', true, 1, 1)`).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Exec(`INSERT INTO credentials (
		id, group_id, data, fingerprint, identity_fingerprint, secret_version,
		auth_state, auth_error_code, status, created_at_ms, updated_at_ms
	) VALUES (1, 1, 'credential-cipher', 'fingerprint', 'identity', 1,
		'ready', '', 'active', 1, 1)`).Error; err != nil {
		t.Fatalf("create credential: %v", err)
	}

	if err := migrations.Up0020(db); err != nil {
		t.Fatalf("Up0020() error = %v", err)
	}
	if err := migrations.Validate0020(db); err != nil {
		t.Fatalf("Validate0020() error = %v", err)
	}
	assertCredentialNoteColumn(t, db)

	var row struct {
		ID                  uint
		Data                string
		Fingerprint         string
		IdentityFingerprint string
		Note                string
	}
	if err := db.Table("credentials").
		Select("id", "data", "fingerprint", "identity_fingerprint", "note").
		Where("id = ?", 1).Take(&row).Error; err != nil {
		t.Fatalf("read migrated credential: %v", err)
	}
	if row.ID != 1 || row.Data != "credential-cipher" || row.Fingerprint != "fingerprint" ||
		row.IdentityFingerprint != "identity" {
		t.Fatalf("existing credential was rewritten by the note migration: %#v", row)
	}
	if row.Note != "" {
		t.Fatalf("existing credential note = %q, want empty", row.Note)
	}

	if err := migrations.Up0020(db); err != nil {
		t.Fatalf("second Up0020() error = %v", err)
	}
	var count int64
	if err := db.Table("credentials").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("credential row count after a second Up0020() = %d, error = %v", count, err)
	}
}

func TestCredentialNoteRecoverableValidationAcceptsPartialColumn(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.ValidateRecoverable0020(db); err == nil {
		t.Fatal("missing credentials table accepted")
	}
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := migrations.ValidateRecoverable0020(db); err != nil {
		t.Fatalf("ValidateRecoverable0020() error = %v", err)
	}
	if err := migrations.Validate0020(db); err == nil {
		t.Fatal("missing credential note accepted")
	}
	if err := migrations.Up0020(db); err != nil {
		t.Fatalf("Up0020() after partial schema error = %v", err)
	}
	if err := migrations.ValidateRecoverable0020(db); err != nil {
		t.Fatalf("ValidateRecoverable0020() after upgrade error = %v", err)
	}
}

// assertCredentialNoteColumn 断言备注列的类型、上限、非空与默认值，避免只检查列名。
func assertCredentialNoteColumn(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes("credentials")
	if err != nil {
		t.Fatalf("inspect credentials columns: %v", err)
	}
	for _, column := range columns {
		if column.Name() != "note" {
			continue
		}
		if !strings.Contains(strings.ToLower(column.DatabaseTypeName()), "char") {
			t.Errorf("credentials.note type = %q, want varchar", column.DatabaseTypeName())
		}
		if length, known := column.Length(); !known || length != 2048 {
			t.Errorf("credentials.note length = %d (known=%v), want 2048", length, known)
		}
		if nullable, known := column.Nullable(); !known || nullable {
			t.Errorf("credentials.note nullable = %v (known=%v), want NOT NULL", nullable, known)
		}
		if value, known := column.DefaultValue(); !known || value != "''" {
			t.Errorf("credentials.note default = %q (known=%v), want empty string", value, known)
		}
		return
	}
	t.Fatal("credentials.note is missing")
}
