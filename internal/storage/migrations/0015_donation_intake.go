package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ID0015 adds independent, durable donation staging and acquisition history.
const ID0015 = "0015_donation_intake"

// donationIdentity0015 binds this database and its integration source independently
// of a rotatable access token. KeyProof prevents silently resetting HMAC history.
type donationIdentity0015 struct {
	ID          uint   `gorm:"primaryKey;autoIncrement:false"`
	InstanceID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_instance"`
	SourceID    string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_source"`
	KeyProof    string `gorm:"type:varchar(64);not null"`
	CreatedAtMS int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

// donationBatch0015 retains the immutable request comparator and target snapshot.
// None of these historical tables cascade with groups or live credentials.
type donationBatch0015 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:1"`
	BatchID        string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:2"`
	RequestDigest  string `gorm:"type:varchar(64);not null"`
	GroupID        uint   `gorm:"not null"`
	GroupName      string `gorm:"type:varchar(255);not null"`
	ChannelID      string `gorm:"type:varchar(64);not null"`
	TargetRevision string `gorm:"type:varchar(64);not null"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

// donationItem0015 is both encrypted staging and the durable per-item receipt.
// Ciphertext can expire; request identities and acceptance facts cannot.
type donationItem0015 struct {
	ID                    uint   `gorm:"primaryKey;autoIncrement"`
	SourceID              string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_item_source,priority:1;index:idx_donation_item_batch,priority:1"`
	ItemID                string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_item_source,priority:2"`
	BatchID               string `gorm:"type:varchar(36);not null;index:idx_donation_item_batch,priority:2"`
	Position              int    `gorm:"not null"`
	GroupID               uint   `gorm:"not null"`
	Fingerprint           string `gorm:"type:varchar(64);not null;index:idx_donation_item_fingerprint"`
	CredentialFingerprint string `gorm:"type:varchar(128);not null"`
	EncryptedPayload      string `gorm:"type:text;not null"`
	ValidationRevision    string `gorm:"type:varchar(64);not null"`
	State                 string `gorm:"type:varchar(32);not null;index:idx_donation_item_work,priority:1;check:chk_donation_item_state,state IN ('queued','validating','committing','accepted','invalid','existing','retry_pending')"`
	ReasonCode            string `gorm:"type:varchar(64);not null"`
	Attempts              int    `gorm:"not null;default:0;check:chk_donation_item_attempts,attempts >= 0"`
	LeaseToken            string `gorm:"type:varchar(36);not null"`
	LeaseUntilMS          int64  `gorm:"column:lease_until_ms;not null;default:0"`
	NextAttemptAtMS       int64  `gorm:"column:next_attempt_at_ms;not null;default:0;index:idx_donation_item_work,priority:2"`
	ExpiresAtMS           int64  `gorm:"column:expires_at_ms;not null"`
	CredentialID          *uint
	AcceptedAtMS          *int64 `gorm:"column:accepted_at_ms"`
	CreatedAtMS           int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
	UpdatedAtMS           int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli"`
}

// donationResource0015 permanently records the first inventory acquisition of a
// normalized API key. OwnerItemID is fenced by that item's durable lease.
type donationResource0015 struct {
	Fingerprint  string `gorm:"type:varchar(64);primaryKey;not null"`
	OwnerItemID  *uint  `gorm:"index:idx_donation_resource_owner"`
	Origin       string `gorm:"type:varchar(16);not null;default:''"`
	GroupID      uint   `gorm:"not null;default:0"`
	CredentialID *uint
	AcquiredAtMS *int64 `gorm:"column:acquired_at_ms"`
	CreatedAtMS  int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
	UpdatedAtMS  int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli"`
}

// donationRetry0015 keeps action idempotency after probe budgets have been reset.
type donationRetry0015 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_retry_source,priority:1"`
	IdempotencyKey string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_retry_source,priority:2"`
	BatchID        string `gorm:"type:varchar(36);not null"`
	RequestDigest  string `gorm:"type:varchar(64);not null"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

func (donationIdentity0015) TableName() string { return "donation_identities" }
func (donationBatch0015) TableName() string    { return "donation_batches" }
func (donationItem0015) TableName() string     { return "donation_items" }
func (donationResource0015) TableName() string { return "donation_resources" }
func (donationRetry0015) TableName() string    { return "donation_retries" }
func SchemaModels0015() []any {
	return []any{&donationIdentity0015{}, &donationBatch0015{}, &donationItem0015{}, &donationResource0015{}, &donationRetry0015{}}
}

func TableNames0015() []string {
	return []string{"donation_identities", "donation_batches", "donation_items", "donation_resources", "donation_retries"}
}

func Up0015(db *gorm.DB) error {
	if err := db.AutoMigrate(SchemaModels0015()...); err != nil {
		return fmt.Errorf("create donation intake schema: %w", err)
	}
	return Validate0015(db)
}

func ValidateCurrent0015(db *gorm.DB) error { return Validate0015(db) }

func Validate0015(db *gorm.DB) error {
	for _, model := range SchemaModels0015() {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return fmt.Errorf("parse donation schema: %w", err)
		}
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("donation table %q is missing", statement.Schema.Table)
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !db.Migrator().HasColumn(model, field.DBName) {
				return fmt.Errorf("donation column %q.%q is missing", statement.Schema.Table, field.DBName)
			}
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if !db.Migrator().HasIndex(model, index.Name) {
				return fmt.Errorf("donation index %q is missing", index.Name)
			}
		}
		for name := range statement.Schema.ParseCheckConstraints() {
			if !db.Migrator().HasConstraint(model, name) {
				return fmt.Errorf("donation constraint %q is missing", name)
			}
		}
	}
	return nil
}

func ValidateRecoverable0015(db *gorm.DB) error {
	for _, model := range SchemaModels0015() {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return fmt.Errorf("parse donation schema: %w", err)
		}
		if !db.Migrator().HasTable(model) {
			continue
		}
		var count int64
		if err := db.Table(statement.Schema.Table).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect interrupted donation table: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("interrupted donation table %q contains data", statement.Schema.Table)
		}
		columns, err := db.Migrator().ColumnTypes(model)
		if err != nil {
			return fmt.Errorf("inspect interrupted donation columns: %w", err)
		}
		known := make(map[string]bool)
		for _, field := range statement.Schema.Fields {
			known[strings.ToLower(field.DBName)] = true
		}
		for _, column := range columns {
			if !known[strings.ToLower(column.Name())] {
				return fmt.Errorf("interrupted donation table %q contains unexpected column", statement.Schema.Table)
			}
		}
	}
	return nil
}
