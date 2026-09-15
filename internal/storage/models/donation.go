package models

// DonationIdentity binds this database and its integration source independently
// of a rotatable access token. KeyProof prevents silently resetting HMAC history.
type DonationIdentity struct {
	ID          uint   `gorm:"primaryKey;autoIncrement:false"`
	InstanceID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_instance"`
	SourceID    string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_source"`
	KeyProof    string `gorm:"type:varchar(64);not null"`
	CreatedAtMS int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

// DonationBatch retains the immutable request comparator and target snapshot.
// None of these historical tables cascade with groups or live credentials.
type DonationBatch struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:1"`
	BatchID        string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_batch_source,priority:2"`
	RequestDigest  string `gorm:"type:varchar(64);not null"`
	GroupID        uint   `gorm:"not null"`
	GroupName      string `gorm:"type:varchar(255);not null"`
	ChannelID      string `gorm:"type:varchar(64);not null"`
	TargetRevision string `gorm:"type:varchar(64);not null"`
	// ValidationMode is frozen with the request. Legacy rows and auto requests
	// keep the zero-value default so the original comparator stays byte-identical.
	ValidationMode string `gorm:"type:varchar(16);not null;default:'auto'"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

// DonationItem is both encrypted staging and the durable per-item receipt.
// Ciphertext can expire; request identities and acceptance facts cannot.
type DonationItem struct {
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
	State                 string `gorm:"type:varchar(32);not null;index:idx_donation_item_work,priority:1;check:chk_donation_item_state,state IN ('queued','validating','committing','accepted','invalid','existing','retry_pending','pending_review','rejected')"`
	ReasonCode            string `gorm:"type:varchar(64);not null"`
	Attempts              int    `gorm:"not null;default:0;check:chk_donation_item_attempts,attempts >= 0"`
	LeaseToken            string `gorm:"type:varchar(36);not null"`
	LeaseUntilMS          int64  `gorm:"column:lease_until_ms;not null;default:0"`
	NextAttemptAtMS       int64  `gorm:"column:next_attempt_at_ms;not null;default:0;index:idx_donation_item_work,priority:2"`
	ExpiresAtMS           int64  `gorm:"column:expires_at_ms;not null"`
	// Manual-review facts are monotonic and independent of Resource.Generation.
	// EffectiveMode is the item-level mode: a legacy retry_exhausted item moved to
	// manual review keeps its original auto batch while this stays manual_review.
	EffectiveMode        string `gorm:"type:varchar(16);not null;default:'auto'"`
	ItemRevision         int64  `gorm:"not null;default:0;check:chk_donation_item_revision,item_revision >= 0"`
	ReviewTargetRevision string `gorm:"type:varchar(64);not null;default:''"`
	EntryActionID        string `gorm:"type:varchar(36);not null;default:''"`
	ReviewActionID       string `gorm:"type:varchar(36);not null;default:''"`
	ReviewDecision       string `gorm:"type:varchar(16);not null;default:'';check:chk_donation_item_review_decision,review_decision IN ('','approved','rejected')"`
	ReviewedAtMS         int64  `gorm:"column:reviewed_at_ms;not null;default:0"`
	CredentialID         *uint
	AcceptedAtMS         *int64 `gorm:"column:accepted_at_ms"`
	CreatedAtMS          int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
	UpdatedAtMS          int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli"`
}

// DonationResource permanently records the first inventory acquisition of a
// normalized API key. OwnerItemID is fenced by that item's durable lease.
type DonationResource struct {
	Fingerprint  string `gorm:"type:varchar(64);primaryKey;not null"`
	OwnerItemID  *uint  `gorm:"index:idx_donation_resource_owner"`
	Origin       string `gorm:"type:varchar(16);not null;default:''"`
	GroupID      uint   `gorm:"not null;default:0"`
	CredentialID *uint
	AcquiredAtMS *int64 `gorm:"column:acquired_at_ms"`
	CreatedAtMS  int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
	UpdatedAtMS  int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli"`
}

// DonationRetry keeps action idempotency after probe budgets have been reset.
type DonationRetry struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	SourceID       string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_retry_source,priority:1"`
	IdempotencyKey string `gorm:"type:varchar(36);not null;uniqueIndex:idx_donation_retry_source,priority:2"`
	BatchID        string `gorm:"type:varchar(36);not null"`
	RequestDigest  string `gorm:"type:varchar(64);not null"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

// DonationReviewAction is the durable, request-scoped record of one manual
// review command. The action identifier is caller-supplied so a lost response
// can be replayed and compared instead of re-applied.
type DonationReviewAction struct {
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
	// Outcome is "applied" only when the command changed state. A persisted
	// "rejected" row is a definite business refusal, not an infrastructure error.
	Outcome        string `gorm:"type:varchar(16);not null;check:chk_donation_review_action_outcome,outcome IN ('applied','rejected')"`
	ReasonCode     string `gorm:"type:varchar(64);not null"`
	EffectRevision int64  `gorm:"not null;default:0"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null"`
	AppliedAtMS    int64  `gorm:"column:applied_at_ms;not null;default:0"`
}

// DonationTestAttempt durably reserves one administrator-issued chat call
// against a single staged key. Response bodies are never persisted.
type DonationTestAttempt struct {
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
	// The lease fences a crashed or superseded caller. At most one running test
	// per item is enforced by the item row lock in the creating transaction.
	LeaseToken   string `gorm:"type:varchar(36);not null;default:''"`
	LeaseUntilMS int64  `gorm:"column:lease_until_ms;not null;default:0;index:idx_donation_test_lease,priority:1"`
	StartedAtMS  int64  `gorm:"column:started_at_ms;not null"`
	FinishedAtMS int64  `gorm:"column:finished_at_ms;not null;default:0"`
}
