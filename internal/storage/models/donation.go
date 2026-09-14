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
