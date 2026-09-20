package storage

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/gorm"

	migrationfiles "gpt-load/internal/storage/migrations"
)

func TestOperationIndexMigrationContract(t *testing.T) {
	testOperationIndexMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalOperationIndexMigrationContract(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testOperationIndexMigration(t, func(t *testing.T) *gorm.DB {
		return openExternalIncrementalMigrationDatabase(t, dsn)
	})
}

// operationIndexMigrationIndex 返回 0017 在注册表中的下标。
// 本 fork 在 0017 之后追加了 donation 迁移，故不能再假设 0017 是最后一条，
// 否则"0017 之前的迁移前缀"会变成"0019 之前的迁移前缀"，测试会静默测错对象。
func operationIndexMigrationIndex(t *testing.T) int {
	t.Helper()
	for index, entry := range migrations {
		if entry.ID == migrationfiles.ID0017 {
			return index
		}
	}
	t.Fatal("migration 0017 is not registered")
	return -1
}

func testOperationIndexMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	t.Helper()
	for _, scenario := range []string{"fresh", "existing", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			db := open(t)
			index := operationIndexMigrationIndex(t)
			if scenario != "fresh" {
				if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
					t.Fatal(err)
				}
				if err := db.Table("request_logs").Create(map[string]any{
					"id": "existing", "completed_at_ms": 1, "access_key_id": 1,
					"protocol": "openai-responses", "operation": "responses_retrieve",
					"client_model": "test", "upstream_model": "test", "status": "error",
					"status_code": 500, "duration_ms": 1, "error_summary": "existing diagnostic",
				}).Error; err != nil {
					t.Fatal(err)
				}
				if scenario == "interrupted" {
					registry := append([]migration(nil), migrations[:index+1]...)
					up := registry[index].Up
					registry[index].Up = func(tx *gorm.DB) error {
						if err := up(tx); err != nil {
							return err
						}
						return fmt.Errorf("interrupt after operation index DDL")
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
				if !db.Migrator().HasIndex("request_logs", "idx_request_logs_operation_completed_id") {
					t.Fatal("operation cursor index is missing")
				}
				if scenario != "fresh" {
					var operation string
					if err := db.Table("request_logs").Where("id = ?", "existing").Pluck("operation", &operation).Error; err != nil || operation != "responses_retrieve" {
						t.Fatalf("existing operation = %q, error = %v", operation, err)
					}
				}
			}
		})
	}
	t.Run("unexpected index definition", func(t *testing.T) {
		db := open(t)
		if err := applyMigrationRegistry(db, migrations[:operationIndexMigrationIndex(t)]); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE INDEX idx_request_logs_operation_completed_id ON request_logs (status)").Error; err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db); err == nil {
			t.Fatal("unexpected operation index definition accepted")
		}
	})
	t.Run("unexpected index direction", func(t *testing.T) {
		db := open(t)
		if err := applyMigrationRegistry(db, migrations[:operationIndexMigrationIndex(t)]); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(
			"CREATE INDEX idx_request_logs_operation_completed_id ON request_logs (operation, completed_at_ms DESC, id ASC)",
		).Error; err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db); err == nil {
			t.Fatal("unexpected operation index direction accepted")
		}
	})
}
