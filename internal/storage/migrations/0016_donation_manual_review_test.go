package migrations_test

import (
	"testing"

	"gpt-load/internal/storage/migrations"
)

// TestDonationManualReviewMigrationUpgradesLegacyHistory proves the upgrade path
// keeps real pre-existing donation rows and opens the widened state set.
func TestDonationManualReviewMigrationUpgradesLegacyHistory(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := db.AutoMigrate(migrations.SchemaModels0015()...); err != nil {
		t.Fatalf("create the 0015 donation schema: %v", err)
	}
	if err := migrations.Validate0015(db); err != nil {
		t.Fatalf("Validate0015() error = %v", err)
	}
	const (
		sourceID = "11111111-1111-4111-8111-111111111111"
		batchID  = "22222222-2222-4222-8222-222222222222"
		itemID   = "33333333-3333-4333-8333-333333333333"
	)
	if err := db.Exec(`INSERT INTO donation_batches
		(source_id, batch_id, request_digest, group_id, group_name, channel_id, target_revision, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sourceID, batchID, "legacy-digest", 7, "legacy-group", "openai", "rev", 1000).Error; err != nil {
		t.Fatalf("insert legacy batch: %v", err)
	}
	if err := db.Exec(`INSERT INTO donation_items
		(source_id, item_id, batch_id, position, group_id, fingerprint, credential_fingerprint,
			encrypted_payload, validation_revision, state, reason_code, attempts, lease_token,
			lease_until_ms, next_attempt_at_ms, expires_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sourceID, itemID, batchID, 0, 7, "fingerprint", "credential-fingerprint",
		"ciphertext", "rev", "retry_pending", "retry_exhausted", 5, "", 0, 0, 9000, 1000, 1000).Error; err != nil {
		t.Fatalf("insert legacy item: %v", err)
	}

	// The migration runner wraps SQLite migrations in a foreign-key-isolated
	// connection; a direct call has to reproduce that precondition.
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if err := migrations.Up0016(db); err != nil {
		t.Fatalf("Up0016() error = %v", err)
	}
	if err := migrations.Validate0016(db); err != nil {
		t.Fatalf("Validate0016() error = %v", err)
	}
	// The historical row survives with its original identity, mode and budget.
	var state, mode, reason, digest string
	var attempts int
	if err := db.Raw(`SELECT i.state, i.effective_mode, i.reason_code, i.attempts, b.request_digest
		FROM donation_items i JOIN donation_batches b ON b.batch_id = i.batch_id WHERE i.item_id = ?`,
		itemID).Row().Scan(&state, &mode, &reason, &attempts, &digest); err != nil {
		t.Fatalf("read upgraded legacy item: %v", err)
	}
	if state != "retry_pending" || reason != "retry_exhausted" || attempts != 5 {
		t.Fatalf("legacy item changed: %s/%s/%d", state, reason, attempts)
	}
	if mode != "auto" || digest != "legacy-digest" {
		t.Fatalf("legacy mode or comparator changed: %q/%q", mode, digest)
	}
	// The widened constraint admits the manual states and still rejects anything
	// outside the closed set.
	if err := db.Exec(`UPDATE donation_items SET state = 'pending_review' WHERE item_id = ?`, itemID).Error; err != nil {
		t.Fatalf("pending_review was not admitted by the widened state constraint: %v", err)
	}
	if err := db.Exec(`UPDATE donation_items SET state = 'rejected' WHERE item_id = ?`, itemID).Error; err != nil {
		t.Fatalf("rejected was not admitted by the widened state constraint: %v", err)
	}
	if err := db.Exec(`UPDATE donation_items SET state = 'not_a_state' WHERE item_id = ?`, itemID).Error; err == nil {
		t.Fatal("the state constraint no longer rejects unknown states")
	}
	// A second run is a no-op and keeps the schema stable.
	if err := migrations.Up0016(db); err != nil {
		t.Fatalf("second Up0016() error = %v", err)
	}
	if err := migrations.Validate0016(db); err != nil {
		t.Fatalf("second Validate0016() error = %v", err)
	}
	if err := migrations.Validate0015(db); err != nil {
		t.Fatalf("the frozen 0015 validator no longer passes after the upgrade: %v", err)
	}
	var counts struct{ Batches, Items, Actions, Tests int }
	if err := db.Raw(`SELECT
		(SELECT COUNT(*) FROM donation_batches) AS batches,
		(SELECT COUNT(*) FROM donation_items) AS items,
		(SELECT COUNT(*) FROM donation_review_actions) AS actions,
		(SELECT COUNT(*) FROM donation_test_attempts) AS tests`).Scan(&counts).Error; err != nil {
		t.Fatalf("count upgraded history: %v", err)
	}
	if counts.Batches != 1 || counts.Items != 1 {
		t.Fatalf("the upgrade duplicated or dropped history: %#v", counts)
	}
}
