package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const ID0020 = "0020_credential_note"

// Up0020 为凭据增加可编辑备注列；既有行统一取空串，不推断历史备注。
func Up0020(db *gorm.DB) error {
	if err := ValidateRecoverable0020(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn("credentials", "note") {
		if err := db.Exec("ALTER TABLE credentials ADD COLUMN note VARCHAR(2048) NOT NULL DEFAULT ''").Error; err != nil {
			return fmt.Errorf("add credential note: %w", err)
		}
	}
	return Validate0020(db)
}

// ValidateRecoverable0020 接受原子加列前后的状态，以支持 MySQL 的 DDL 中断恢复。
func ValidateRecoverable0020(db *gorm.DB) error {
	if !db.Migrator().HasTable("credentials") {
		return fmt.Errorf("credential note: credentials table is missing")
	}
	if !db.Migrator().HasColumn("credentials", "note") {
		return nil
	}
	return validateCredentialNote0020(db)
}

func Validate0020(db *gorm.DB) error {
	if !db.Migrator().HasColumn("credentials", "note") {
		return fmt.Errorf("credential note is missing")
	}
	return validateCredentialNote0020(db)
}

func validateCredentialNote0020(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes("credentials")
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "note" {
			continue
		}
		if !strings.Contains(strings.ToLower(column.DatabaseTypeName()), "char") {
			return fmt.Errorf("credential note must be varchar")
		}
		if nullable, known := column.Nullable(); !known || nullable {
			return fmt.Errorf("credential note must be non-null")
		}
		if length, known := column.Length(); known && length != 2048 {
			return fmt.Errorf("credential note length must be 2048")
		}
		value, known := column.DefaultValue()
		if !known || (value != "" && value != "''" && value != "''::character varying") {
			return fmt.Errorf("credential note must default to empty")
		}
		return nil
	}
	return fmt.Errorf("credential note is missing")
}
