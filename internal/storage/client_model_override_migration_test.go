package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	migrationfiles "gpt-load/internal/storage/migrations"
)

func TestClientModelOverrideMigrationContract(t *testing.T) {
	t.Parallel()
	testClientModelOverrideMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalClientModelOverrideMigrationContract(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testClientModelOverrideMigration(t, func(t *testing.T) *gorm.DB {
		return openExternalIncrementalMigrationDatabase(t, dsn)
	})
}

// clientModelOverrideMigrationIndex 返回 0023 在注册表中的下标。
// 上游 0020_client_model_overrides 在本 fork 中顺延为 0023，因此不能沿用上游的固定下标。
func clientModelOverrideMigrationIndex(t *testing.T) int {
	t.Helper()
	for index, entry := range migrations {
		if entry.ID == migrationfiles.ID0023 {
			return index
		}
	}
	t.Fatal("migration 0023 is not registered")
	return -1
}

func testClientModelOverrideMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	for _, scenario := range []string{"fresh", "upgrade", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			db := open(t)
			if db.Dialector.Name() == "sqlite" {
				t.Parallel()
			}
			index := clientModelOverrideMigrationIndex(t)
			if scenario != "fresh" {
				if err := applyMigrationRegistry(db, migrations[:index]); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "interrupted" {
				entry := migrations[index]
				up := entry.Up
				entry.Up = func(tx *gorm.DB) error {
					if err := up(tx); err != nil {
						return err
					}
					return fmt.Errorf("interrupted client model override DDL")
				}
				registry := append(append([]migration(nil), migrations[:index]...), entry)
				if err := applyMigrationRegistry(db, registry); err == nil {
					t.Fatal("expected interruption")
				}
			}
			if err := applyMigrations(db); err != nil {
				t.Fatal(err)
			}
			if !db.Migrator().HasTable("client_model_overrides") {
				t.Fatal("client model overrides table is missing")
			}
			modelNames := []string{"Model", "model", "模型🚀", "é", "é", strings.Repeat("模型", 600)}
			for _, name := range modelNames {
				identity := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
				row := map[string]any{"model_hash": identity, "client_model": name, "overrides": `{"display_name":"Custom"}`}
				if err := db.Table("client_model_overrides").Create(row).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Table("client_model_overrides").Create(row).Error; err == nil {
					t.Fatal("duplicate model identity accepted")
				}
				var saved struct{ ClientModel, Overrides string }
				if err := db.Table("client_model_overrides").Where("model_hash = ?", identity).Take(&saved).Error; err != nil {
					t.Fatal(err)
				}
				if saved.ClientModel != name || saved.Overrides != row["overrides"] {
					t.Fatalf("model identity or overrides changed: %+v", saved)
				}
			}
			if err := applyMigrations(db); err != nil {
				t.Fatalf("repeat with persisted overrides: %v", err)
			}
			var count int64
			if err := db.Table("client_model_overrides").Count(&count).Error; err != nil || count != int64(len(modelNames)) {
				t.Fatalf("persisted overrides count = %d, error = %v", count, err)
			}
		})
	}
}
