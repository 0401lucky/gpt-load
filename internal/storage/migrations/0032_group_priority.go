package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Upstream migration 0029 is appended after the fork's published migration chain.
const ID0032 = "0032_group_priority"

type group0029 struct {
	Priority int32 `gorm:"type:integer;not null;default:0"`
}

func (group0029) TableName() string { return "groups" }

// Up0032 旧分组统一使用 0，保留原有同档调度行为。
func Up0032(db *gorm.DB) error {
	if err := ValidateRecoverable0032(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(&group0029{}, "priority") {
		if err := db.Migrator().AddColumn(&group0029{}, "Priority"); err != nil {
			return fmt.Errorf("add group priority: %w", err)
		}
	}
	return Validate0032(db)
}

func ValidateRecoverable0032(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported group priority migration driver %q", db.Dialector.Name())
	}
	if !db.Migrator().HasTable(&group0029{}) {
		return fmt.Errorf("groups table is missing")
	}
	if db.Migrator().HasColumn(&group0029{}, "priority") {
		return Validate0032(db)
	}
	return nil
}

func Validate0032(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes(&group0029{})
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "priority" {
			continue
		}
		switch strings.ToLower(column.DatabaseTypeName()) {
		case "int", "integer", "int4":
		default:
			return fmt.Errorf("group priority must be an integer")
		}
		if kind, known := column.ColumnType(); known && strings.Contains(strings.ToLower(kind), "unsigned") {
			return fmt.Errorf("group priority must be signed")
		}
		if nullable, known := column.Nullable(); !known || nullable {
			return fmt.Errorf("group priority must not be nullable")
		}
		value, known := column.DefaultValue()
		if !known || strings.Trim(strings.SplitN(value, "::", 2)[0], "'\"() ") != "0" {
			return fmt.Errorf("group priority default must be zero")
		}
		return nil
	}
	return fmt.Errorf("group priority is missing")
}
