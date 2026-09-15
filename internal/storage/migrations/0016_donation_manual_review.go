package migrations

import (
	"fmt"
	"strings"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const ID0016 = "0016_donation_manual_review"

const (
	donationItemTable0016         = "donation_items"
	donationItemStateCheck0016    = "chk_donation_item_state"
	donationItemPriorState0016    = "state IN ('queued','validating','committing','accepted','invalid','existing','retry_pending')"
	donationItemCurrentState0016  = "state IN ('queued','validating','committing','accepted','invalid','existing','retry_pending','pending_review','rejected')"
	donationReviewActionTable0016 = "donation_review_actions"
	donationTestAttemptTable0016  = "donation_test_attempts"
)

// donationBatch0016 adds the frozen request mode to the immutable comparator row.
type donationBatch0016 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:1"`
	BatchID        string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:2"`
	RequestDigest  string `gorm:"type:varchar(64);not null"`
	GroupID        uint   `gorm:"not null"`
	GroupName      string `gorm:"type:varchar(255);not null"`
	ChannelID      string `gorm:"type:varchar(64);not null"`
	TargetRevision string `gorm:"type:varchar(64);not null"`
	ValidationMode string `gorm:"type:varchar(16);not null;default:'auto'"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

func (donationBatch0016) TableName() string { return "donation_batches" }

// donationItem0016 carries the manual-review projection and the widened state set.
type donationItem0016 struct {
	EffectiveMode        string `gorm:"type:varchar(16);not null;default:'auto'"`
	ItemRevision         int64  `gorm:"not null;default:0;check:chk_donation_item_revision,item_revision >= 0"`
	ReviewTargetRevision string `gorm:"type:varchar(64);not null;default:''"`
	EntryActionID        string `gorm:"type:varchar(36);not null;default:''"`
	ReviewActionID       string `gorm:"type:varchar(36);not null;default:''"`
	ReviewDecision       string `gorm:"type:varchar(16);not null;default:'';check:chk_donation_item_review_decision,review_decision IN ('','approved','rejected')"`
	ReviewedAtMS         int64  `gorm:"column:reviewed_at_ms;not null;default:0"`
	State                string `gorm:"type:varchar(32);not null;check:chk_donation_item_state,state IN ('queued','validating','committing','accepted','invalid','existing','retry_pending','pending_review','rejected')"`
}

func (donationItem0016) TableName() string { return donationItemTable0016 }

// donationReviewAction0016 records one durable manual review command per action ID.
type donationReviewAction0016 struct {
	ID               uint   `gorm:"primaryKey;autoIncrement"`
	SourceID         string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_review_action_source,priority:1"`
	ActionID         string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_review_action_source,priority:2"`
	BatchID          string `gorm:"type:varchar(36);not null;index:idx_donation_review_action_item,priority:1"`
	ItemID           string `gorm:"type:varchar(36);not null;index:idx_donation_review_action_item,priority:2"`
	Kind             string `gorm:"type:varchar(32);not null;check:chk_donation_review_action_kind,kind IN ('enter_review','approve','reject')"`
	Actor            string `gorm:"type:varchar(128);not null"`
	ExpectedRevision int64  `gorm:"not null;default:0"`
	TargetRevision   string `gorm:"type:varchar(64);not null;default:''"`
	Note             string `gorm:"type:varchar(2048);not null;default:''"`
	RequestDigest    string `gorm:"type:varchar(64);not null"`
	Outcome          string `gorm:"type:varchar(16);not null;check:chk_donation_review_action_outcome,outcome IN ('applied','rejected')"`
	ReasonCode       string `gorm:"type:varchar(64);not null"`
	EffectRevision   int64  `gorm:"not null;default:0"`
	CreatedAtMS      int64  `gorm:"column:created_at_ms;not null"`
	AppliedAtMS      int64  `gorm:"column:applied_at_ms;not null;default:0"`
}

func (donationReviewAction0016) TableName() string { return donationReviewActionTable0016 }

// donationTestAttempt0016 reserves one administrator-issued chat call per test ID.
type donationTestAttempt0016 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_test_source,priority:1;index:idx_donation_test_item,priority:1"`
	TestID         string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_test_source,priority:2"`
	BatchID        string `gorm:"type:varchar(36);not null"`
	ItemID         string `gorm:"type:varchar(36);not null;index:idx_donation_test_item,priority:2"`
	Actor          string `gorm:"type:varchar(128);not null"`
	StartRevision  int64  `gorm:"not null;default:0"`
	TargetRevision string `gorm:"type:varchar(64);not null;default:''"`
	Model          string `gorm:"type:varchar(255);not null"`
	InputDigest    string `gorm:"type:varchar(64);not null"`
	Stream         bool   `gorm:"not null;default:false"`
	State          string `gorm:"type:varchar(24);not null;check:chk_donation_test_state,state IN ('running','succeeded','failed','cancelled','interrupted')"`
	ReasonCode     string `gorm:"type:varchar(64);not null;default:''"`
	StatusCode     int    `gorm:"not null;default:0"`
	OutputBytes    int64  `gorm:"not null;default:0;check:chk_donation_test_output_bytes,output_bytes >= 0"`
	InputTokens    *int64 `gorm:"check:chk_donation_test_input_tokens,input_tokens IS NULL OR input_tokens >= 0"`
	OutputTokens   *int64 `gorm:"check:chk_donation_test_output_tokens,output_tokens IS NULL OR output_tokens >= 0"`
	LeaseToken     string `gorm:"type:varchar(36);not null;default:''"`
	LeaseUntilMS   int64  `gorm:"column:lease_until_ms;not null;default:0;index:idx_donation_test_lease,priority:1"`
	StartedAtMS    int64  `gorm:"column:started_at_ms;not null"`
	FinishedAtMS   int64  `gorm:"column:finished_at_ms;not null;default:0"`
}

func (donationTestAttempt0016) TableName() string { return donationTestAttemptTable0016 }

// SchemaModels0016 lists the full donation schema at this revision so callers can
// validate a fresh install, an upgraded one, and an interrupted upgrade alike.
func SchemaModels0016() []any {
	return []any{&donationBatch0016{}, &donationItem0016{}, &donationReviewAction0016{}, &donationTestAttempt0016{}}
}

func TableNames0016() []string {
	return []string{"donation_batches", donationItemTable0016, donationReviewActionTable0016, donationTestAttemptTable0016}
}

var donationItemColumns0016 = []struct{ field, column string }{
	{"EffectiveMode", "effective_mode"},
	{"ItemRevision", "item_revision"},
	{"ReviewTargetRevision", "review_target_revision"},
	{"EntryActionID", "entry_action_id"},
	{"ReviewActionID", "review_action_id"},
	{"ReviewDecision", "review_decision"},
	{"ReviewedAtMS", "reviewed_at_ms"},
}

// Up0016 adds the manual review projection without rewriting any historical fact.
func Up0016(db *gorm.DB) error {
	if !db.Migrator().HasTable(donationItemTable0016) || !db.Migrator().HasTable("donation_batches") {
		return fmt.Errorf("add donation manual review: donation tables are missing")
	}
	if err := ValidateRecoverable0016(db); err != nil {
		return err
	}
	batch := &donationBatch0016{}
	if !db.Migrator().HasColumn(batch, "validation_mode") {
		if err := db.Migrator().AddColumn(batch, "ValidationMode"); err != nil {
			return fmt.Errorf("add donation_batches.validation_mode: %w", err)
		}
	}
	if strings.EqualFold(db.Dialector.Name(), "sqlite") {
		if err := rebuildSQLiteDonationItems0016(db); err != nil {
			return err
		}
	} else {
		item := &donationItem0016{}
		for _, column := range donationItemColumns0016 {
			if db.Migrator().HasColumn(item, column.column) {
				continue
			}
			if err := db.Migrator().AddColumn(item, column.field); err != nil {
				return fmt.Errorf("add donation_items.%s: %w", column.column, err)
			}
		}
		if err := replaceDonationStateConstraint0016(db); err != nil {
			return err
		}
		// A column addition never carries its CHECK clause on MySQL or
		// PostgreSQL, so the new constraints are created explicitly.
		for name, expression := range donationItemAddedConstraintExpressions0016 {
			if db.Migrator().HasConstraint(donationItemTable0016, name) {
				continue
			}
			if err := createDonationConstraint0016(db, name, expression); err != nil {
				return err
			}
		}
	}
	if err := db.AutoMigrate(&donationReviewAction0016{}, &donationTestAttempt0016{}); err != nil {
		return fmt.Errorf("create donation review schema: %w", err)
	}
	return Validate0016(db)
}

func replaceDonationStateConstraint0016(db *gorm.DB) error {
	if !db.Migrator().HasConstraint(donationItemTable0016, donationItemStateCheck0016) {
		return createDonationConstraint0016(db, donationItemStateCheck0016, donationItemCurrentState0016)
	}
	if err := dropDonationConstraint0016(db, donationItemStateCheck0016); err != nil {
		return fmt.Errorf("replace donation item state constraint: %w", err)
	}
	return createDonationConstraint0016(db, donationItemStateCheck0016, donationItemCurrentState0016)
}

func createDonationConstraint0016(db *gorm.DB, name, expression string) error {
	table, constraint := quoteDonationIdentifier0016(db, donationItemTable0016), quoteDonationIdentifier0016(db, name)
	if err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", table, constraint, expression)).Error; err != nil {
		return fmt.Errorf("create donation constraint %q: %w", name, err)
	}
	return nil
}

func dropDonationConstraint0016(db *gorm.DB, name string) error {
	if strings.EqualFold(db.Dialector.Name(), "mysql") {
		if dialector, ok := db.Dialector.(*gormmysql.Dialector); ok && dialector.Config != nil &&
			mysqlRequiresCheckDropSyntax0003(dialector.ServerVersion) {
			return db.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP CHECK `%s`", donationItemTable0016, name)).Error
		}
	}
	return db.Migrator().DropConstraint(donationItemTable0016, name)
}

func quoteDonationIdentifier0016(db *gorm.DB, value string) string {
	if strings.EqualFold(db.Dialector.Name(), "mysql") {
		return "`" + value + "`"
	}
	return `"` + value + `"`
}

// donationItemAddedColumns0016 is the exact SQLite DDL for the new item columns.
var donationItemAddedColumns0016 = []string{
	"effective_mode varchar(16) NOT NULL DEFAULT 'auto'",
	"item_revision integer NOT NULL DEFAULT 0",
	"review_target_revision varchar(64) NOT NULL DEFAULT ''",
	"entry_action_id varchar(36) NOT NULL DEFAULT ''",
	"review_action_id varchar(36) NOT NULL DEFAULT ''",
	"review_decision varchar(16) NOT NULL DEFAULT ''",
	"reviewed_at_ms integer NOT NULL DEFAULT 0",
}

// donationItemAddedConstraints0016 carries the checks ALTER TABLE cannot attach.
var donationItemAddedConstraints0016 = []string{
	"CONSTRAINT chk_donation_item_revision CHECK (item_revision >= 0)",
	"CONSTRAINT chk_donation_item_review_decision CHECK (review_decision IN ('','approved','rejected'))",
}

// donationItemAddedConstraintExpressions0016 is the same pair addressed by name
// for engines that need an explicit ALTER TABLE ADD CONSTRAINT.
var donationItemAddedConstraintExpressions0016 = map[string]string{
	"chk_donation_item_revision":        "item_revision >= 0",
	"chk_donation_item_review_decision": "review_decision IN ('','approved','rejected')",
}

// rebuildSQLiteDonationItems0016 rewrites the table so the widened state check and
// the new constraints exist. Every historical row and index is preserved verbatim.
// SQLite requires every column definition to precede the table constraints, so the
// added columns are inserted ahead of the existing constraint list.
func rebuildSQLiteDonationItems0016(db *gorm.DB) error {
	ddl, err := sqliteTableDDL0016(db, donationItemTable0016)
	if err != nil {
		return err
	}
	if strings.Contains(ddl, donationItemCurrentState0016) && strings.Contains(ddl, "chk_donation_item_revision") {
		return nil
	}
	if strings.Contains(ddl, "chk_donation_item_revision") {
		return fmt.Errorf("unexpected prior donation item schema")
	}
	if strings.Count(ddl, donationItemPriorState0016) != 1 {
		return fmt.Errorf("unexpected prior donation item state constraint")
	}
	ddl = strings.Replace(ddl, donationItemPriorState0016, donationItemCurrentState0016, 1)
	close := strings.LastIndex(ddl, ")")
	if close < 0 {
		return fmt.Errorf("invalid donation item table definition")
	}
	insertAt := close
	if marker := strings.Index(ddl, ",CONSTRAINT"); marker >= 0 {
		insertAt = marker
	}
	if strings.HasSuffix(strings.TrimSpace(ddl[:insertAt]), "(") {
		ddl = ddl[:insertAt] + strings.Join(donationItemAddedColumns0016, ", ") + ddl[insertAt:]
	} else {
		ddl = ddl[:insertAt] + ", " + strings.Join(donationItemAddedColumns0016, ", ") + ddl[insertAt:]
	}
	close = strings.LastIndex(ddl, ")")
	ddl = ddl[:close] + ", " + strings.Join(donationItemAddedConstraints0016, ", ") + ddl[close:]
	start := strings.Index(ddl, "(")
	if start < 0 {
		return fmt.Errorf("invalid donation item table definition")
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		return err
	}
	if foreignKeys != 0 {
		return fmt.Errorf("donation item rebuild requires migration foreign key isolation")
	}
	var objects []string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type, name",
		donationItemTable0016).Scan(&objects).Error; err != nil {
		return err
	}
	var sequence int64
	if err := db.Raw("SELECT COALESCE(MAX(seq), 0) FROM sqlite_sequence WHERE name = ?", donationItemTable0016).Scan(&sequence).Error; err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(donationItemTable0016)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		if strings.ContainsAny(column.Name(), "\"`\x00") {
			return fmt.Errorf("invalid donation item column")
		}
		names = append(names, `"`+column.Name()+`"`)
	}
	projection := strings.Join(names, ",")
	statements := []string{
		"CREATE TABLE donation_items__0016 " + ddl[start:],
		"INSERT INTO donation_items__0016 (" + projection + ") SELECT " + projection + " FROM donation_items",
		"DROP TABLE donation_items",
		"ALTER TABLE donation_items__0016 RENAME TO donation_items",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("rebuild SQLite donation items: %w", err)
		}
	}
	if sequence > 0 {
		if err := db.Exec("UPDATE sqlite_sequence SET seq = MAX(seq, ?) WHERE name = ?", sequence, donationItemTable0016).Error; err != nil {
			return fmt.Errorf("restore SQLite donation item sequence: %w", err)
		}
	}
	for _, statement := range objects {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("restore SQLite donation item object: %w", err)
		}
	}
	return nil
}

func sqliteTableDDL0016(db *gorm.DB, table string) (string, error) {
	var ddl string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&ddl).Error; err != nil {
		return "", fmt.Errorf("read SQLite donation item definition: %w", err)
	}
	if strings.TrimSpace(ddl) == "" {
		return "", fmt.Errorf("SQLite donation item definition is missing")
	}
	return ddl, nil
}

// ValidateRecoverable0016 accepts any prefix of the idempotent upgrade so an
// interrupted migration can resume instead of failing the process.
func ValidateRecoverable0016(db *gorm.DB) error {
	for _, table := range []string{"donation_batches", donationItemTable0016} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("validate recoverable donation manual review: table %q is missing", table)
		}
	}
	return nil
}

// Validate0016 verifies the manual review projection, the widened state set and
// the durable review/test ledgers.
func Validate0016(db *gorm.DB) error {
	if err := ValidateRecoverable0016(db); err != nil {
		return err
	}
	batch := &donationBatch0016{}
	if !db.Migrator().HasColumn(batch, "validation_mode") {
		return fmt.Errorf("donation_batches.validation_mode is missing")
	}
	item := &donationItem0016{}
	for _, column := range donationItemColumns0016 {
		if !db.Migrator().HasColumn(item, column.column) {
			return fmt.Errorf("donation_items.%s is missing", column.column)
		}
	}
	if !db.Migrator().HasConstraint(donationItemTable0016, donationItemStateCheck0016) {
		return fmt.Errorf("donation item state constraint is missing")
	}
	if !db.Migrator().HasConstraint(donationItemTable0016, "chk_donation_item_revision") {
		return fmt.Errorf("donation item revision constraint is missing")
	}
	if !db.Migrator().HasConstraint(donationItemTable0016, "chk_donation_item_review_decision") {
		return fmt.Errorf("donation item review decision constraint is missing")
	}
	if err := validateDonationStateConstraint0016(db); err != nil {
		return err
	}
	for _, model := range []any{&donationReviewAction0016{}, &donationTestAttempt0016{}} {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return fmt.Errorf("parse donation review schema: %w", err)
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

// donationItemStateValues0016 is the closed set the widened constraint must admit.
var donationItemStateValues0016 = []string{
	"queued", "validating", "committing", "accepted", "invalid", "existing",
	"retry_pending", "pending_review", "rejected",
}

// donationConstraintNormalizer0016 removes the engine-specific rendering around a
// CHECK expression. MySQL stores charset introducers (`_utf8mb4'queued'`) and
// PostgreSQL rewrites `IN (...)` into `= ANY (ARRAY[...])` with explicit casts,
// so only the quoted literals can be compared portably.
var donationConstraintNormalizer0016 = strings.NewReplacer(
	" ", "", "\n", "", "\t", "", "\r", "", "`", "", `"`, "", "(", "", ")", "",
	// MySQL returns the stored clause with backslash-escaped literals.
	"\\", "",
	"_utf8mb4", "", "_utf8", "", "_latin1", "", "_binary", "", "_ascii", "",
	"::text", "", "::charactervarying", "", "::varchar", "",
)

func validateDonationStateConstraint0016(db *gorm.DB) error {
	definition, err := donationStateConstraintDefinition0016(db)
	if err != nil {
		return err
	}
	normalized := donationConstraintNormalizer0016.Replace(strings.ToLower(definition))
	for _, value := range donationItemStateValues0016 {
		if !strings.Contains(normalized, "'"+value+"'") {
			return fmt.Errorf("donation item state constraint does not admit %q", value)
		}
	}
	return nil
}

func donationStateConstraintDefinition0016(db *gorm.DB) (string, error) {
	var definition string
	var err error
	switch strings.ToLower(db.Dialector.Name()) {
	case "sqlite":
		err = db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", donationItemTable0016).Scan(&definition).Error
	case "mysql":
		err = db.Raw("SELECT CHECK_CLAUSE FROM information_schema.check_constraints WHERE constraint_schema = DATABASE() AND constraint_name = ?", donationItemStateCheck0016).Scan(&definition).Error
	case "postgres", "postgresql":
		err = db.Raw("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = ? AND conrelid = ?::regclass", donationItemStateCheck0016, donationItemTable0016).Scan(&definition).Error
	default:
		return "", fmt.Errorf("unsupported donation manual review migration driver")
	}
	if err != nil {
		return "", fmt.Errorf("inspect donation item state constraint: %w", err)
	}
	return definition, nil
}

// ValidateCurrent0016 is the steady-state validator once later migrations exist.
func ValidateCurrent0016(db *gorm.DB) error { return Validate0016(db) }
