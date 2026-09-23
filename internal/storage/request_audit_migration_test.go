package storage

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	migrationfiles "gpt-load/internal/storage/migrations"
)

func TestRequestAuditMigrationContract(t *testing.T) {
	testRequestAuditMigration(t, openInternalMigrationTestDatabase)
}
func TestExternalRequestAuditMigrationContract(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GPT_LOAD_DATABASE_TEST_DSN"))
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testRequestAuditMigration(t, func(t *testing.T) *gorm.DB { return openExternalIncrementalMigrationDatabase(t, dsn) })
}

// requestAuditMigrationIndex 返回 0024 在注册表中的下标。
// 上游 0021_request_audit 在本 fork 中顺延为 0024，因此不能沿用上游的固定下标。
func requestAuditMigrationIndex(t *testing.T) int {
	t.Helper()
	for index, entry := range migrations {
		if entry.ID == migrationfiles.ID0024 {
			return index
		}
	}
	t.Fatal("migration 0024 is not registered")
	return -1
}

func testRequestAuditMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	for _, scenario := range []string{"fresh", "upgrade", "interrupted_column", "interrupted_table"} {
		t.Run(scenario, func(t *testing.T) {
			db := open(t)
			index := requestAuditMigrationIndex(t)
			if scenario != "fresh" {
				if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(scenario, "interrupted") {
				registry := append([]migration(nil), migrations...)
				up := registry[index].Up
				registry[index].Up = func(tx *gorm.DB) error {
					if scenario == "interrupted_column" {
						if err := tx.Exec("ALTER TABLE request_logs ADD COLUMN request_audit JSON").Error; err != nil {
							return err
						}
					} else {
						if err := up(tx); err != nil {
							return err
						}
					}
					return fmt.Errorf("interrupt audit migration")
				}
				if err := applyMigrationRegistry(db, registry); err == nil {
					t.Fatal("expected interruption")
				}
			}
			for range 2 {
				if err := AutoMigrate(db); err != nil {
					t.Fatal(err)
				}
			}
			if !db.Migrator().HasTable("request_audit_usages") || !db.Migrator().HasColumn("request_logs", "request_audit") {
				t.Fatal("audit schema missing")
			}
		})
	}
}
