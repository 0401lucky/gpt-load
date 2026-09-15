package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	"gpt-load/internal/parameteroverride"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/protocol"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
)

func TestDonationManualApproveDoesNotRequireProbe(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-without-probe", "synthetic-seed")
	// A historical group may have no configured models. Manual eligibility must
	// not inherit the automatic probe's requirement for a model.
	if err := fixture.db.Model(&models.Group{}).Where("id = ?", groupID).
		Updates(map[string]any{"models": models.JSON(`[]`), "validation_model": ""}).Error; err != nil {
		t.Fatal(err)
	}
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2000, "synthetic-no-probe")
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].CanProbe {
		t.Fatalf("fixture must have no probe: %#v, %v", groups, err)
	}
	view := mustReviewContext(t, fixture, source, batch.BatchID, batch.Items[0].ItemID)
	if !view.CanReview || view.CanTest || view.UnavailableReason != donationReviewReasonModelUnavailable {
		t.Errorf("manual review without probe = %#v", view)
	}
	action, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(2010), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision})
	if err != nil || action.Outcome != donationReviewOutcomeApplied {
		t.Fatalf("approve without probe = %#v, %v", action, err)
	}
}

func TestDonationManualRejectWaitsForRunningTest(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "reject-running-test", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2020, "synthetic-running-key")
	nowMS := fixture.service.donationNowMS()
	attempt := models.DonationTestAttempt{SourceID: source, TestID: donationTestID(2030), BatchID: batch.BatchID,
		ItemID: batch.Items[0].ItemID, Actor: "admin-1", StartRevision: 1, TargetRevision: revision,
		Model: "gpt-4o", InputDigest: strings.Repeat("b", 64), State: donationTestStateRunning,
		LeaseToken: donationTestID(2031), LeaseUntilMS: nowMS + 60_000, StartedAtMS: nowMS}
	if err := fixture.db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	action, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(2032), Kind: donationReviewKindReject,
			Actor: "admin-2", ExpectedItemRevision: 1, Note: "reject while call active"})
	if err != nil || action.Outcome != donationReviewOutcomeRejected || action.ReasonCode != donationReviewReasonTestRunning {
		t.Fatalf("reject while running = %#v, %v", action, err)
	}
	if item := donationTestItem(t, fixture, source, batch.Items[0].ItemID); item.State != "pending_review" || item.EncryptedPayload == "" {
		t.Fatal("running test lost its undecided staging")
	}
}

func TestDonationManualApproveWaitsBeforeCredentialCommit(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-recovery-barrier", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2040, "synthetic-manual-barrier")
	fixture.service.reconcileRegistryGroup = func(uint, []state.CredentialEntry) (bool, error) {
		return false, errors.New("synthetic older operation recovery failure")
	}
	name := "manual-pending-operation"
	_, err := fixture.service.CreateGroupIdempotent(t.Context(), donationTestID(2050), GroupCreateRequest{
		Name: &name, ChannelID: channel.OpenAI, Params: json.RawMessage(`{}`),
		Models:      optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gpt-4o"}}},
		Credentials: "synthetic-operation-key", ConnectionType: "api_key", ConfirmSameTarget: true,
	})
	assertAPIErrorCode(t, err, app_errors.ErrControlOperationIncomplete.Code)
	_, err = fixture.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(2051), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision})
	assertAPIErrorCode(t, err, app_errors.ErrControlRecoveryPending.Code)
	item := donationTestItem(t, fixture, source, batch.Items[0].ItemID)
	if item.State != "pending_review" || item.CredentialID != nil {
		t.Fatalf("approval crossed older recovery barrier: state=%s, credential=%v", item.State, item.CredentialID)
	}
}

func TestDonationManualAcceptanceAdvancesRevisionAfterRuntimeRecovery(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-runtime-revision", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2060, "synthetic-runtime-key")
	fixture.manager.SetSnapshotReconciler(donationFailingSnapshotReconciler{})
	_, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(2070), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision})
	assertAPIErrorCode(t, err, app_errors.ErrControlOperationIncomplete.Code)
	committing := donationTestItem(t, fixture, source, batch.Items[0].ItemID)
	fixture.manager.SetSnapshotReconciler(controlAccessQuotaReconciler{runtime: fixture.accessQuota})
	if err := fixture.service.recoverDonationItem(t.Context(), committing.ID); err != nil {
		t.Fatal(err)
	}
	accepted := donationTestItem(t, fixture, source, batch.Items[0].ItemID)
	if accepted.State != "accepted" || accepted.ItemRevision <= committing.ItemRevision {
		t.Fatalf("changed fact reused revision: committing=%d, accepted=%d/%s", committing.ItemRevision, accepted.ItemRevision, accepted.State)
	}
}

func TestDonationTestResponseMatchesPersistedTerminalMetadata(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "test-terminal-metadata", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2080, "synthetic-metadata-key")
	fixture.service.executor = &donationChatTestExecutor{result: successfulChatResult("hello")}
	result, err := fixture.service.RunDonationTest(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationTestRequest{TestID: donationTestID(2090), Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision, Model: "gpt-4o", Prompt: "hello"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.service.GetDonationTest(t.Context(), source, result.TestID)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinishedAtMS <= 0 || result.StartedAtMS != stored.StartedAtMS || result.FinishedAtMS != stored.FinishedAtMS {
		t.Errorf("immediate and durable times disagree: result=%#v, stored=%#v", result, stored)
	}
	if result.Usage == nil || stored.Usage == nil || *result.Usage != *stored.Usage {
		t.Errorf("durable test lost available usage: result=%#v, stored=%#v", result.Usage, stored.Usage)
	}
}

func TestDonationTestDoesNotSucceedWhenTerminalWriteFails(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "test-failed-terminal-write", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2100, "synthetic-write-failure-key")
	fixture.service.executor = &donationChatTestExecutor{result: successfulChatResult("hello")}
	const hook = "test:donation_terminal_failure"
	if err := fixture.db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "donation_test_attempts" {
			_ = tx.AddError(errors.New("synthetic terminal write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fixture.db.Callback().Update().Remove(hook) })
	result, err := fixture.service.RunDonationTest(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
		DonationTestRequest{TestID: donationTestID(2110), Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision, Model: "gpt-4o", Prompt: "hello"}, nil, nil)
	if err == nil || result.State == donationTestStateSucceeded {
		t.Fatalf("undurable success escaped: state=%q, err=%v", result.State, err)
	}
}

func TestDonationTestBusyDoesNotQueueBeyondDurableLease(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "test-bounded-slots", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 2120, "synthetic-slot-key")
	for range cap(fixture.service.donationTestSlots) {
		fixture.service.donationTestSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for len(fixture.service.donationTestSlots) > 0 {
			<-fixture.service.donationTestSlots
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := fixture.service.RunDonationTest(ctx, source, batch.BatchID, batch.Items[0].ItemID,
		DonationTestRequest{TestID: donationTestID(2130), Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision, Model: "gpt-4o", Prompt: "hello"}, nil, nil)
	if !errors.Is(err, app_errors.ErrDonationTestBusy) {
		t.Fatalf("full execution slots must refuse before reservation, got %v", err)
	}
	var count int64
	if err := fixture.db.Model(&models.DonationTestAttempt{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("busy request reserved a durable attempt: %d, %v", count, err)
	}
}

func TestDonationProjectionPreservesUTF8AndBoundsWholeEvents(t *testing.T) {
	t.Parallel()
	t.Run("utf8", func(t *testing.T) {
		projection := newDonationStreamProjection([]string{"test-secret"})
		text := "中文回复应当完整保留。"
		var output strings.Builder
		for _, fragment := range []string{text[:18], text[18:]} {
			events, err := projection.Push(sseChatFrame(fragment))
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if !utf8.ValidString(event.Text) {
					t.Errorf("projected delta splits a UTF-8 rune: %x", event.Text)
				}
				output.WriteString(event.Text)
			}
		}
		tail, err := projection.Finish()
		if err != nil || !utf8.ValidString(tail) {
			t.Fatalf("invalid tail: %x, %v", tail, err)
		}
		output.WriteString(tail)
		if output.String() != text {
			t.Fatalf("output=%q, want %q", output.String(), text)
		}
	})
	t.Run("unframed_limit", func(t *testing.T) {
		projection := newDonationStreamProjection(nil)
		chunk := []byte(strings.Repeat("x", donationMaxEventBytes/2))
		var err error
		for range 3 {
			_, err = projection.Push(chunk)
			if err != nil {
				break
			}
		}
		if !errors.Is(err, errDonationResponseTooLarge) {
			t.Fatalf("split event exceeded limit without rejection: %v", err)
		}
	})
}

func TestDonationTestDeadlineIsNotUserCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	stateName, reason, _, _, _, err := classifyDonationTestResult(ctx,
		execution.AttemptResult{StatusCode: http.StatusOK}, nil)
	if err != nil || stateName != donationTestStateFailed || reason != donationTestReasonTimeout {
		t.Fatalf("deadline classified as %s/%s, err=%v", stateName, reason, err)
	}
}

func TestDonationVirtualCredentialRangesAreDisjoint(t *testing.T) {
	t.Parallel()
	limit := ^uint(0) / 4
	seen := map[uint]bool{}
	for _, id := range []uint{1, 2, limit - 1, limit} {
		for _, manual := range []bool{false, true} {
			virtual, ok := donationVirtualCredentialID(id, manual)
			if !ok || virtual <= ^uint(0)/2 || seen[virtual] {
				t.Fatalf("id=%d manual=%v maps outside its disjoint range: %d/%v", id, manual, virtual, ok)
			}
			seen[virtual] = true
		}
	}
	for _, id := range []uint{0, limit + 1, ^uint(0)/2 - 1, ^uint(0)} {
		for _, manual := range []bool{false, true} {
			if _, ok := donationVirtualCredentialID(id, manual); ok {
				t.Fatalf("out-of-range id %d manual=%v was accepted", id, manual)
			}
		}
	}
}

func TestDonationReviewCanRejectDisabledOrChangedTarget(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"disabled", "deleted", "changed"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			fixture := newServiceFixture(t)
			groupID := newDonationTestGroup(t, fixture, "manual-target-lifecycle", "synthetic-seed")
			source, batch, _ := stageDonationReviewBatch(t, fixture, groupID, 2140, "synthetic-offline-key")
			switch mutation {
			case "disabled":
				_, err := fixture.service.UpdateGroupSettings(t.Context(), groupID, GroupSettingsUpdateRequest{Enabled: optionalField[bool]{Set: true, Value: false}})
				if err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := fixture.service.DeleteGroup(t.Context(), groupID); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := fixture.db.Model(&models.Group{}).Where("id = ?", groupID).Update("models", models.JSON(`[{"id":"different-model"}]`)).Error; err != nil {
					t.Fatal(err)
				}
			}
			view := mustReviewContext(t, fixture, source, batch.BatchID, batch.Items[0].ItemID)
			if view.CanReview || view.CanTest || !view.CanReject {
				t.Fatalf("broken target disabled independent rejection: %#v", view)
			}
			action, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, batch.BatchID, batch.Items[0].ItemID,
				DonationReviewActionRequest{ActionID: donationTestID(2150), Kind: donationReviewKindReject, Actor: "admin-1", ExpectedItemRevision: view.ItemRevision, Note: "target cannot be used"})
			if err != nil || action.Outcome != donationReviewOutcomeApplied {
				t.Fatalf("reject = %#v, %v", action, err)
			}
		})
	}
}

func TestDonationManualOwnershipAndInventoryReconciliation(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-resource-owner", "synthetic-inventory")
	executor := &donationChatTestExecutor{result: successfulChatResult("unexpected")}
	fixture.service.executor = executor
	source, inventory, _ := stageDonationReviewBatch(t, fixture, groupID, 2160, "synthetic-inventory")
	if item := donationTestItem(t, fixture, source, inventory.Items[0].ItemID); item.State != "existing" || item.EncryptedPayload != "" {
		t.Fatal("preexisting inventory was staged for manual use")
	}
	_, first, _ := stageDonationReviewBatch(t, fixture, groupID, 2170, "synthetic-contended-key")
	_, second, revision := stageDonationReviewBatch(t, fixture, groupID, 2180, "synthetic-contended-key")
	secondItem := donationTestItem(t, fixture, source, second.Items[0].ItemID)
	if secondItem.State != "pending_review" || secondItem.ReasonCode != donationReviewReasonResourceBusy {
		t.Fatalf("pending owner was mistaken for acquisition: %s/%s", secondItem.State, secondItem.ReasonCode)
	}
	_, err := fixture.service.RunDonationTest(t.Context(), source, second.BatchID, second.Items[0].ItemID,
		DonationTestRequest{TestID: donationTestID(2182), Actor: "admin-1", ExpectedItemRevision: secondItem.ItemRevision,
			ReviewTargetRevision: revision, Model: "gpt-4o", Prompt: "hello"}, nil, nil)
	if err == nil || len(executor.recordedCalls()) != 0 {
		t.Fatal("a non-owner tested an occupied resource")
	}
	firstItem := donationTestItem(t, fixture, source, first.Items[0].ItemID)
	if _, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, first.BatchID, firstItem.ItemID,
		DonationReviewActionRequest{ActionID: donationTestID(2172), Kind: donationReviewKindReject, Actor: "admin-1", ExpectedItemRevision: firstItem.ItemRevision, Note: "release reservation"}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.ReconcileDonationReviews(t.Context()); err != nil {
		t.Fatal(err)
	}
	secondItem = donationTestItem(t, fixture, source, second.Items[0].ItemID)
	if secondItem.State != "pending_review" || secondItem.ReasonCode != "" {
		t.Fatalf("released owner was not reconciled: %s/%s", secondItem.State, secondItem.ReasonCode)
	}
	if _, err := fixture.service.ImportGroupCredentials(t.Context(), groupID, CredentialImportRequest{Credentials: "synthetic-contended-key"}); err != nil {
		t.Fatal(err)
	}
	fixture.service.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if err := fixture.service.ReconcileDonationReviews(t.Context()); err != nil {
		t.Fatal(err)
	}
	secondItem = donationTestItem(t, fixture, source, second.Items[0].ItemID)
	if secondItem.State != "existing" || secondItem.EncryptedPayload != "" || secondItem.CredentialID != nil {
		t.Fatal("ordinary import did not permanently preempt manual acquisition")
	}
}

func TestDonationManualExpiryReleasesOnlyPendingOwner(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-expiry-owner", "synthetic-seed")
	source, batch, _ := stageDonationReviewBatch(t, fixture, groupID, 2190, "synthetic-expiry-owner")
	item := donationTestItem(t, fixture, source, batch.Items[0].ItemID)
	fixture.service.now = func() time.Time { return time.UnixMilli(item.ExpiresAtMS + 1) }
	if err := fixture.service.ReconcileDonationReviews(t.Context()); err != nil {
		t.Fatal(err)
	}
	var resource models.DonationResource
	if err := fixture.db.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if resource.OwnerItemID != nil || resource.AcquiredAtMS != nil {
		t.Fatal("expiry retained an unacquired reservation")
	}
}

func TestDonationChatOverridesRetainTextAndTokenBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		rule   map[string]any
		wantOK bool
	}{
		{name: "bounded_temperature", rule: map[string]any{"set": map[string]any{"temperature": 0.5}}, wantOK: true},
		{name: "too_many_tokens", rule: map[string]any{"set": map[string]any{"max_tokens": 4097}}},
		{name: "missing_limit", rule: map[string]any{"remove": []any{"/max_tokens"}}},
		{name: "tools", rule: map[string]any{"set": map[string]any{"tools": []any{map[string]any{"type": "web_search"}}}}},
		{name: "multiple_completions", rule: map[string]any{"set": map[string]any{"n": 2}}},
		{name: "attachment", rule: map[string]any{"set": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image_url", "image_url": "https://invalid.test"}}}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rules, err := parameteroverride.Compile([]any{test.rule})
			if err != nil {
				t.Fatal(err)
			}
			body, err := donationChatRequestBody("gpt-4o", DonationTestRequest{Prompt: "hello", MaxOutputTokens: 32})
			if err != nil {
				t.Fatal(err)
			}
			body, _, err = rules.Apply(protocol.OpenAICompletions, execution.OperationChatCompletion, "gpt-4o", body)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateDonationChatRequestBody(body, "gpt-4o", false); (err == nil) != test.wantOK {
				t.Fatalf("override acceptance=%v, want %v", err == nil, test.wantOK)
			}
		})
	}
}

func TestDonationStreamRejectsPartialAndErrorTrailers(t *testing.T) {
	t.Parallel()
	for _, trailer := range []string{`data: {"error":{"message":"upstream error"}}` + "\n\n", "data: [DO"} {
		projection := newDonationStreamProjection(nil)
		if _, err := projection.Push(sseChatFrame("partial answer")); err != nil {
			t.Fatal(err)
		}
		_, pushErr := projection.Push([]byte(trailer))
		_, finishErr := projection.Finish()
		if pushErr == nil && finishErr == nil && projection.Complete() {
			t.Fatal("partial/error stream became complete")
		}
	}
}

func TestDonationReviewCleanupDoesNotWaitForSlowAutomaticProbe(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-independent-cleanup", "synthetic-seed")
	_, _ = stageDonationTestBatch(t, fixture, groupID, 2200, "synthetic-slow-auto-key")
	source, manual, _ := stageDonationReviewBatch(t, fixture, groupID, 2210, "synthetic-expiring-manual-key")
	entered, release := make(chan struct{}), make(chan struct{})
	fixture.service.executor = &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		close(entered)
		<-release
		return successfulCredentialProbeResult()
	}}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	reviewTicks := make(chan time.Time, 1)
	go func() { defer close(finished); fixture.service.runDonationRecovery(ctx, reviewTicks) }()
	t.Cleanup(func() { cancel(); close(release); <-finished })
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("automatic probe did not start")
	}
	if err := fixture.db.Model(&models.DonationItem{}).Where("source_id = ? AND item_id = ?", source, manual.Items[0].ItemID).
		Updates(map[string]any{"expires_at_ms": fixture.service.donationNowMS() - 1, "next_attempt_at_ms": 0}).Error; err != nil {
		t.Fatal(err)
	}
	reviewTicks <- time.Now()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("manual expiry waited behind the in-flight automatic probe")
		case <-poll.C:
			item := donationTestItem(t, fixture, source, manual.Items[0].ItemID)
			if item.State == "invalid" && item.ReasonCode == "staging_expired" {
				return
			}
		}
	}
}

func TestDonationManualCannotBypassSDKOwnedAuthenticationHeaders(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "bare", header: "Authorization", value: "${API_KEY}"},
		{name: "bearer", header: "Authorization", value: "bEaReR ${API_KEY}"},
		{name: "api_key", header: "X-Api-Key", value: "${API_KEY}"},
		{name: "mixed_bearer", header: "Authorization", value: "Bearer synthetic-fixed-key ${API_KEY}"},
		{name: "mixed_header", header: "X-Api-Key", value: "${API_KEY},synthetic-fixed-key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newServiceFixture(t)
			groupID := newDonationTestGroup(t, fixture, "manual-auth-template", "synthetic-seed")
			overrides, err := json.Marshal(map[string]any{"header_rules": map[string]any{"set": map[string]string{test.header: test.value}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.Model(&models.Group{}).Where("id = ?", groupID).Update("overrides", models.JSON(overrides)).Error; err != nil {
				t.Fatal(err)
			}
			groups, err := fixture.service.ListDonationGroups(t.Context())
			if err != nil || len(groups) != 1 || groups[0].CanProbe || groups[0].CanManualReview {
				t.Fatalf("manual intake bypassed the SDK-owned header rule: %#v, %v", groups, err)
			}
			capabilities, err := fixture.service.DonationCapabilities(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			_, err = fixture.service.ReceiveDonationBatch(t.Context(), capabilities.SourceID, DonationBatchRequest{BatchID: donationTestID(2230), GroupID: groupID,
				ValidationMode: donationModeManualReview, TargetRevision: strings.Repeat("a", 64),
				Items: []DonationItemRequest{{ItemID: donationTestID(2231), Key: "synthetic-template-key"}}})
			assertAPIErrorCode(t, err, app_errors.ErrDonationTargetUnavailable.Code)
		})
	}
}
