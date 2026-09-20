package storage

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/gorm"

	migrationfiles "gpt-load/internal/storage/migrations"
)

func TestCredentialNoteMigrationContract(t *testing.T) {
	testCredentialNoteMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalCredentialNoteMigrationContract(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testCredentialNoteMigration(t, func(t *testing.T) *gorm.DB {
		return openExternalIncrementalMigrationDatabase(t, dsn)
	})
}

// credentialNoteMigrationIndex 返回 0020 在注册表中的下标。
// 后续迁移会追加在链尾，故不能假设 0020 是最后一条。
func credentialNoteMigrationIndex(t *testing.T) int {
	t.Helper()
	for index, entry := range migrations {
		if entry.ID == migrationfiles.ID0020 {
			return index
		}
	}
	t.Fatal("migration 0020 is not registered")
	return -1
}

func testCredentialNoteMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	t.Helper()
	for _, scenario := range []string{"fresh", "existing", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			db := open(t)
			index := credentialNoteMigrationIndex(t)
			if scenario != "fresh" {
				if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
					t.Fatal(err)
				}
				if err := db.Exec(`INSERT INTO groups (
					id, name, channel_id, connection_type, params, models, enabled, created_at_ms, updated_at_ms
				) VALUES (1, 'credential note', 'openai', 'api_key', '{}', '[]', true, 1, 1)`).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Exec(`INSERT INTO credentials (
					id, group_id, data, fingerprint, identity_fingerprint, secret_version,
					auth_state, auth_error_code, status, created_at_ms, updated_at_ms
				) VALUES (1, 1, 'credential-cipher', 'fingerprint', 'identity', 1,
					'ready', '', 'active', 1, 1)`).Error; err != nil {
					t.Fatal(err)
				}
				if scenario == "interrupted" {
					registry := append([]migration(nil), migrations[:index+1]...)
					up := registry[index].Up
					registry[index].Up = func(tx *gorm.DB) error {
						if err := up(tx); err != nil {
							return err
						}
						return fmt.Errorf("interrupt after credential note DDL")
					}
					if err := applyMigrationRegistry(db, registry); err == nil {
						t.Fatal("expected migration interruption")
					}
				}
			}
			for range 2 {
				if err := AutoMigrate(db); err != nil {
					t.Fatal(err)
				}
				if !db.Migrator().HasColumn("credentials", "note") {
					t.Fatal("credential note column is missing")
				}
				if scenario == "fresh" {
					continue
				}
				var row struct {
					Data                string
					Fingerprint         string
					IdentityFingerprint string
					Note                string
				}
				if err := db.Table("credentials").
					Select("data", "fingerprint", "identity_fingerprint", "note").
					Where("id = ?", 1).Take(&row).Error; err != nil {
					t.Fatal(err)
				}
				if row.Data != "credential-cipher" || row.Fingerprint != "fingerprint" ||
					row.IdentityFingerprint != "identity" || row.Note != "" {
					t.Fatalf("existing credential after upgrade = %#v", row)
				}
				var count int64
				if err := db.Table("credentials").Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("credential row count = %d, error = %v", count, err)
				}
			}
		})
	}
}
