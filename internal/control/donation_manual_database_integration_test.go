package control

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/execution"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage"
	"gpt-load/internal/storage/migrations"
	"gpt-load/internal/storage/models"
)

// Real database coverage for the manual path. The fixture creates and later
// removes only its own isolated database/schema, never the DSN's existing data.
func TestExternalDatabaseDonationManualReviewTransactions(t *testing.T) {
	first, dsn := newExternalDonationFixture(t)
	second := donationSecondService(t, dsn)
	groupID := newExternalDonationEmptyGroup(t, first, "external-manual-review")
	source, batch, target := stageDonationReviewBatch(t, first, groupID, 6000, "synthetic-external-manual-key")
	input := DonationTestRequest{TestID: donationTestID(6010), Actor: "user:7", ExpectedItemRevision: 1,
		ReviewTargetRevision: target, Model: "gpt-4o", Prompt: "hello"}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	start, release := make(chan struct{}), make(chan struct{})
	entered := make(chan struct{}, 2)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	executor := &donationChatTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return successfulChatResult("database review response")
	}}
	first.service.executor, second.service.executor = executor, executor
	type testResult struct {
		result DonationTestResult
		err    error
	}
	results := make(chan testResult, 2)
	var workers sync.WaitGroup
	for index, fixture := range []serviceFixture{first, second} {
		request := input
		request.TestID = donationTestID(uint64(6010 + index))
		workers.Go(func() {
			<-start
			result, err := fixture.service.RunDonationTest(ctx, source, batch.BatchID, batch.Items[0].ItemID, request, nil, nil)
			results <- testResult{result, err}
		})
	}
	t.Cleanup(func() { unblock(); cancel(); workers.Wait() })
	close(start)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("neither process started the reserved test")
	}
	select {
	case result := <-results:
		if !errors.Is(result.err, app_errors.ErrDonationTestBusy) && !errors.Is(result.err, app_errors.ErrDonationTestConflict) {
			t.Fatalf("concurrent test result = %#v / %v", result.result, result.err)
		}
	case <-entered:
		t.Fatal("two processes called the same staged key simultaneously")
	case <-ctx.Done():
		t.Fatal("the non-owner test never returned")
	}
	item := donationTestItem(t, second, source, batch.Items[0].ItemID)
	rejected, err := second.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, item.ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(6020), Kind: donationReviewKindReject, Actor: "user:8", ExpectedItemRevision: item.ItemRevision, Note: "test still active"})
	if err != nil || rejected.Outcome != donationReviewOutcomeRejected || rejected.ReasonCode != donationReviewReasonTestRunning {
		t.Fatalf("other process ignored active test lease: %#v, %v", rejected, err)
	}
	unblock()
	select {
	case result := <-results:
		if result.err != nil || result.result.State != donationTestStateSucceeded || result.result.FinishedAtMS == 0 {
			t.Fatalf("reserved call = %#v, %v", result.result, result.err)
		}
	case <-ctx.Done():
		t.Fatal("reserved call never finished")
	}
	workers.Wait()
	if len(executor.recordedCalls()) != 1 {
		t.Fatalf("upstream call count=%d, want 1", len(executor.recordedCalls()))
	}
	item = donationTestItem(t, second, source, item.ItemID)
	if item.ItemRevision != 3 || item.State != "pending_review" {
		t.Fatalf("test facts did not advance the manual receipt: %d/%s", item.ItemRevision, item.State)
	}
	approve := DonationReviewActionRequest{ActionID: donationTestID(6030), Kind: donationReviewKindApprove,
		Actor: "user:7", ExpectedItemRevision: item.ItemRevision, ReviewTargetRevision: target}
	type actionResult struct {
		result DonationReviewActionResponse
		err    error
	}
	actions := make(chan actionResult, 2)
	startActions := make(chan struct{})
	for _, fixture := range []serviceFixture{first, second} {
		workers.Go(func() {
			<-startActions
			result, err := fixture.service.ApplyDonationReviewAction(ctx, source, batch.BatchID, item.ItemID, approve)
			actions <- actionResult{result, err}
		})
	}
	close(startActions)
	var original DonationReviewActionResponse
	for range 2 {
		select {
		case result := <-actions:
			if result.err != nil || result.result.Outcome != donationReviewOutcomeApplied {
				t.Fatalf("concurrent approve = %#v, %v", result.result, result.err)
			}
			if original.ActionID != "" && original != result.result {
				t.Fatalf("action replay diverged: %#v / %#v", original, result.result)
			}
			original = result.result
		case <-ctx.Done():
			t.Fatal("approval never returned")
		}
	}
	workers.Wait()
	item = donationTestItem(t, second, source, item.ItemID)
	if item.State != "accepted" || item.ItemRevision <= original.EffectRevision {
		t.Fatalf("accepted fact reused committing version: %s/%d action=%d", item.State, item.ItemRevision, original.EffectRevision)
	}
	if item.ReviewedAtMS != original.AppliedAtMS {
		t.Fatalf("approval timestamps diverged: item=%d, action=%d", item.ReviewedAtMS, original.AppliedAtMS)
	}
	assertExternalDonationCount(t, second.db.Model(&models.Credential{}).Where("fingerprint = ?", item.CredentialFingerprint), 1)
	assertExternalDonationCount(t, second.db.Model(&models.DonationReviewAction{}).Where("action_id = ?", approve.ActionID), 1)
	t.Run("unrelated_items_can_reserve_together", func(t *testing.T) {
		_, parallelBatch, parallelTarget := stageDonationReviewBatch(t, first, groupID, 6100,
			"synthetic-external-parallel-one", "synthetic-external-parallel-two")
		parallelCtx, parallelCancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(parallelCancel)
		ready, proceed := make(chan struct{}, 2), make(chan struct{})
		var proceedOnce sync.Once
		releaseInserts := func() { proceedOnce.Do(func() { close(proceed) }) }
		var parallelWorkers sync.WaitGroup
		t.Cleanup(func() { releaseInserts(); parallelCancel(); parallelWorkers.Wait() })
		const hook = "test:manual_parallel_test_insert"
		for _, peer := range []serviceFixture{first, second} {
			if err := peer.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
				if tx.Statement.Table == "donation_test_attempts" {
					ready <- struct{}{}
					select {
					case <-proceed:
					case <-parallelCtx.Done():
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := peer.db.Callback().Create().Remove(hook); err != nil {
					t.Error(err)
				}
			})
		}
		parallelResults := make(chan testResult, 2)
		for index, peer := range []serviceFixture{first, second} {
			parallelWorkers.Go(func() {
				result, err := peer.service.RunDonationTest(parallelCtx, source, parallelBatch.BatchID, parallelBatch.Items[index].ItemID,
					DonationTestRequest{TestID: donationTestID(uint64(6110 + index)), Actor: "user:7", ExpectedItemRevision: 1,
						ReviewTargetRevision: parallelTarget, Model: "gpt-4o", Prompt: "hello"}, nil, nil)
				parallelResults <- testResult{result, err}
			})
		}
		for range 2 {
			select {
			case <-ready:
			case <-parallelCtx.Done():
				t.Fatal("unrelated item reservations blocked each other before insert")
			}
		}
		releaseInserts()
		for range 2 {
			select {
			case result := <-parallelResults:
				if result.err != nil || result.result.State != donationTestStateSucceeded {
					t.Fatalf("parallel reservation = %s, %v", result.result.State, result.err)
				}
			case <-parallelCtx.Done():
				t.Fatal("parallel reservation did not finish")
			}
		}
		parallelWorkers.Wait()
	})
	if err := storage.AutoMigrate(second.db); err != nil {
		t.Fatalf("repeat migration with manual history: %v", err)
	}
	if first.db.Dialector.Name() == "mysql" {
		// MySQL permits unsigned IDs beyond MaxInt64. Synthetic execution must
		// fail closed instead of aliasing an explicitly imported real identity.
		highID, ok := donationVirtualCredentialID(1, true)
		if !ok {
			t.Fatal("invalid synthetic boundary fixture")
		}
		if err := first.db.Exec("UPDATE credentials SET id = ? WHERE id = ?", strconv.FormatUint(uint64(highID), 10), *item.CredentialID).Error; err != nil {
			t.Fatal(err)
		}
		if err := donationVirtualCredentialRangeAvailable(first.db); !errors.Is(err, app_errors.ErrDonationTestUnavailable) {
			t.Fatalf("oversized real credential did not block synthetic range: %v", err)
		}
	}
}

func TestExternalDatabaseDonationManualReviewUpgrades0015History(t *testing.T) {
	fixture, _ := newExternalDonationFixture(t)
	groupID := newExternalDonationEmptyGroup(t, fixture, "external-manual-upgrade")
	capabilities, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 {
		t.Fatalf("catalog = %#v, %v", groups, err)
	}
	// Restore the actual frozen pre-feature donation tables in this isolated
	// fixture, rather than merely upgrading User or a pre-donation empty schema.
	for _, table := range []string{"donation_review_actions", "donation_test_attempts", "donation_items", "donation_batches"} {
		if err := fixture.db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.db.AutoMigrate(migrations.SchemaModels0015()...); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec("DELETE FROM schema_migrations WHERE id = ?", migrations.ID0016).Error; err != nil {
		t.Fatal(err)
	}
	source, batchID, itemID := capabilities.SourceID, donationTestID(7000), donationTestID(7001)
	const key = "synthetic-0015-upgrade-key"
	request := DonationBatchRequest{BatchID: batchID, GroupID: groupID, TargetRevision: groups[0].TargetRevision,
		Items: []DonationItemRequest{{ItemID: itemID, Key: key}}}
	digest, err := fixture.service.donationBatchDigest(request, donationModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	canonical := `{"api_key":"` + key + `"}`
	encrypted, err := fixture.encryption.Encrypt(canonical)
	if err != nil {
		t.Fatal(err)
	}
	nowMS := fixture.service.donationNowMS()
	if err := fixture.db.Exec(`INSERT INTO donation_batches
		(source_id, batch_id, request_digest, group_id, group_name, channel_id, target_revision, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, source, batchID, digest, groupID, groups[0].Name, string(groups[0].ChannelID), request.TargetRevision, nowMS).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec(`INSERT INTO donation_items
		(source_id, item_id, batch_id, position, group_id, fingerprint, credential_fingerprint,
		 encrypted_payload, validation_revision, state, reason_code, attempts, lease_token,
		 lease_until_ms, next_attempt_at_ms, expires_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, source, itemID, batchID, 0,
		groupID, fixture.encryption.Hash(donationFingerprintDomain+key), fixture.encryption.Hash(canonical), encrypted,
		request.TargetRevision, "retry_pending", "retry_exhausted", 5, "", 0, nowMS, nowMS+donationStagingTTL.Milliseconds(), nowMS, nowMS).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.AutoMigrate(fixture.db); err != nil {
		t.Fatalf("upgrade real 0015 history: %v", err)
	}
	if err := storage.AutoMigrate(fixture.db); err != nil {
		t.Fatalf("repeat upgraded migration: %v", err)
	}
	old, err := fixture.service.ReceiveDonationBatch(t.Context(), source, request)
	if err != nil || len(old.Items) != 1 || old.Items[0].State != "retry_pending" || old.Items[0].ItemRevision != 0 {
		t.Fatalf("old comparator no longer replays: %#v, %v", old, err)
	}
	view := mustReviewContext(t, fixture, source, batchID, itemID)
	if view.EffectiveMode != donationModeAuto || view.ReviewAction != donationReviewKindEnterReview {
		t.Fatalf("legacy context invented manual facts: %#v", view)
	}
	wrong, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(7010), Kind: donationReviewKindEnterReview, Actor: "user:7",
			ExpectedItemRevision: 0, ReviewTargetRevision: strings.Repeat("f", 64)})
	if err != nil || wrong.Outcome != donationReviewOutcomeRejected || wrong.ReasonCode != donationReviewReasonTargetChanged {
		t.Fatalf("changed entry target not refused: %#v, %v", wrong, err)
	}
	entered, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(7011), Kind: donationReviewKindEnterReview, Actor: "user:7",
			ExpectedItemRevision: 0, ReviewTargetRevision: view.ReviewTargetRevision})
	if err != nil || entered.Outcome != donationReviewOutcomeApplied {
		t.Fatalf("legacy enter review = %#v, %v", entered, err)
	}
	item := donationTestItem(t, fixture, source, itemID)
	if item.Attempts != 5 || item.ExpiresAtMS != nowMS+donationStagingTTL.Milliseconds() || item.EntryActionID != entered.ActionID {
		t.Fatal("legacy conversion changed attempts, expiry, or entry identity")
	}
	if _, err := fixture.service.ReceiveDonationBatch(t.Context(), source, request); err != nil {
		t.Fatalf("entry changed original auto request digest: %v", err)
	}
}
