package control

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gpt-load/internal/execution"
	"gpt-load/internal/storage/models"
)

// donationChatTestExecutor records chat attempts so a test can prove which key,
// model and body actually reached the upstream, and replay synthetic frames.
type donationChatTestExecutor struct {
	mu           sync.Mutex
	calls        []execution.AttemptSpec
	result       execution.AttemptResult
	streamFrames [][]byte
	streamResult execution.StreamResult
	execute      func(execution.AttemptSpec) execution.AttemptResult
	sawStream    bool
}

func (executor *donationChatTestExecutor) Execute(_ context.Context, spec execution.AttemptSpec) execution.AttemptResult {
	executor.mu.Lock()
	executor.calls = append(executor.calls, spec.Clone())
	execute := executor.execute
	result := executor.result.Clone()
	executor.mu.Unlock()
	if execute != nil {
		return execute(spec.Clone())
	}
	return result
}

func (executor *donationChatTestExecutor) ExecuteStream(_ context.Context, spec execution.AttemptSpec, sink execution.StreamSink) execution.StreamResult {
	executor.mu.Lock()
	executor.calls = append(executor.calls, spec.Clone())
	executor.sawStream = true
	frames := append([][]byte(nil), executor.streamFrames...)
	result := executor.streamResult.Clone()
	executor.mu.Unlock()
	for index, frame := range frames {
		if err := sink(execution.StreamEvent{Sequence: uint64(index + 1), Kind: execution.StreamEventData, Data: frame}); err != nil {
			result.Error = &execution.ErrorEvidence{Kind: execution.ErrorKindInternal, Code: "sink_closed"}
			return result
		}
	}
	return result
}

func (executor *donationChatTestExecutor) recordedCalls() []execution.AttemptSpec {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	result := make([]execution.AttemptSpec, len(executor.calls))
	copy(result, executor.calls)
	return result
}

func successfulChatBody(text string) []byte {
	encoded, _ := json.Marshal(map[string]any{"choices": []any{
		map[string]any{"message": map[string]any{"role": "assistant", "content": text}},
	}, "usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 11}})
	return encoded
}

func successfulChatResult(text string) execution.AttemptResult {
	return execution.AttemptResult{DispatchState: execution.DispatchMaybeSent, ResponseStarted: true,
		StatusCode: http.StatusOK, Header: http.Header{}, Body: successfulChatBody(text)}
}

func sseChatFrame(text string) []byte {
	payload, _ := json.Marshal(map[string]any{"choices": []any{
		map[string]any{"delta": map[string]any{"content": text}},
	}})
	return []byte("data: " + string(payload) + "\n\n")
}

// stageDonationReviewBatch stages a manual batch and returns its source, request
// and the resolved manual target revision.
func stageDonationReviewBatch(t *testing.T, fixture serviceFixture, groupID uint, sequence uint64, keys ...string) (string, DonationBatchRequest, string) {
	t.Helper()
	capabilities, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request := DonationBatchRequest{BatchID: donationTestID(sequence), GroupID: groupID, ValidationMode: donationModeManualReview}
	manualRevision := ""
	for _, group := range groups {
		if group.ID == groupID {
			if !group.CanManualReview {
				t.Fatalf("group %d cannot accept manual review: %q", groupID, group.ManualUnavailableReason)
			}
			manualRevision = group.ManualTargetRevision
			request.TargetRevision = group.ManualTargetRevision
		}
	}
	if manualRevision == "" {
		t.Fatalf("group %d has no manual target revision", groupID)
	}
	for index, key := range keys {
		request.Items = append(request.Items, DonationItemRequest{ItemID: donationTestID(sequence + 1 + uint64(index)), Key: key})
	}
	if _, err := fixture.service.ReceiveDonationBatch(t.Context(), capabilities.SourceID, request); err != nil {
		t.Fatal(err)
	}
	return capabilities.SourceID, request, manualRevision
}

func TestDonationCapabilitiesAdvertiseManualReviewAndKeepAutoDigestFrozen(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	capabilities, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities.Features) != 1 || capabilities.Features[0] != donationFeatureManualReview {
		t.Fatalf("features = %#v", capabilities.Features)
	}
	if capabilities.ReviewLimits.MaxPromptBytes != donationMaxPromptBytes || capabilities.ReviewLimits.MaxNoteBytes != donationMaxNoteBytes {
		t.Fatalf("review limits = %#v", capabilities.ReviewLimits)
	}
	// The original limits keep their exact values; new capability advertisement
	// must not change what an older caller validates.
	if capabilities.ProtocolVersion != "1" || capabilities.Limits.StagingRetentionSeconds != 604800 {
		t.Fatalf("legacy limits changed: %#v", capabilities.Limits)
	}
	// An explicit auto mode must serialize exactly like the absent original field,
	// because that byte sequence is the frozen request comparator.
	base := DonationBatchRequest{BatchID: donationTestID(90), GroupID: 1, TargetRevision: strings.Repeat("a", 64),
		Items: []DonationItemRequest{{ItemID: donationTestID(91), Key: "synthetic-digest-key"}}}
	explicit := base
	explicit.ValidationMode = donationModeAuto
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "validation_mode") {
		t.Fatalf("auto request leaks the new field into the frozen summary: %s", encoded)
	}
	implicitDigest, err := fixture.service.donationBatchDigest(base, donationModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	explicitDigest, err := fixture.service.donationBatchDigest(explicit, donationModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if implicitDigest != explicitDigest {
		t.Fatalf("explicit auto digest %q != implicit %q", explicitDigest, implicitDigest)
	}
	// Freeze the original four-field wire comparator independently of the new
	// request DTO. Comparing only implicit/explicit auto would miss a future
	// accidental field addition or field-order change shared by both requests.
	legacyWire := `{"batch_id":"` + base.BatchID + `","group_id":1,"target_revision":"` + base.TargetRevision +
		`","items":[{"item_id":"` + base.Items[0].ItemID + `","key":"` +
		fixture.encryption.Hash("donation-request-key/v1\x00synthetic-digest-key") + `"}]}`
	if want := fixture.encryption.Hash("donation-batch/v1\x00" + legacyWire); implicitDigest != want {
		t.Fatalf("auto comparator changed: got %q, want frozen legacy %q", implicitDigest, want)
	}
	manual := base
	manual.ValidationMode = donationModeManualReview
	manualDigest, err := fixture.service.donationBatchDigest(manual, donationModeManualReview)
	if err != nil {
		t.Fatal(err)
	}
	if manualDigest == implicitDigest {
		t.Fatal("manual and auto share one comparator domain")
	}
}

func TestDonationManualBatchStagesWithoutProbeOrPoolEntry(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-review-group", "synthetic-inventory-key")
	executor := &donationChatTestExecutor{}
	fixture.service.executor = executor
	baseline := acquiredDonationResources(t, fixture)
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 10, "synthetic-manual-key")

	result, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidationMode != donationModeManualReview || result.State != "processing" {
		t.Fatalf("batch = %#v", result)
	}
	item := result.Items[0]
	if item.State != "pending_review" || item.EffectiveMode != donationModeManualReview || item.ItemRevision != 1 {
		t.Fatalf("item = %#v", item)
	}
	if item.ReviewTargetRevision != manualRevision || item.StagingExpiresAtMS == 0 {
		t.Fatalf("item target = %#v", item)
	}
	// No automatic work may run for a manual item, so a drain is a no-op here.
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(executor.recordedCalls()) != 0 {
		t.Fatal("a manual item was sent to the automatic probe path")
	}
	var credentials int64
	if err := fixture.db.Model(&models.Credential{}).Count(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	if credentials != 1 {
		t.Fatalf("manual staging changed the credential pool: %d", credentials)
	}
	if acquiredDonationResources(t, fixture) != baseline {
		t.Fatal("manual staging acquired inventory before review")
	}
	staged := donationTestItem(t, fixture, source, item.ItemID)
	if staged.EncryptedPayload == "" || strings.Contains(staged.EncryptedPayload, "synthetic-manual-key") {
		t.Fatal("manual staging did not keep the key encrypted")
	}
}

func TestDonationManualApproveAcceptsOnceAndIgnoresLateDecisions(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-approve-group", "synthetic-inventory-key")
	executor := &donationChatTestExecutor{}
	fixture.service.executor = executor
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 20, "synthetic-approve-key")
	itemID := request.Items[0].ItemID

	context := mustReviewContext(t, fixture, source, request.BatchID, itemID)
	if !context.CanReview || !context.CanTest || context.ItemRevision != 1 {
		t.Fatalf("review context = %#v", context)
	}
	if len(context.TestModels) == 0 || context.TestModels[0] != "gpt-4o" {
		t.Fatalf("test models = %#v", context.TestModels)
	}

	approve := DonationReviewActionRequest{ActionID: donationTestID(30), Kind: donationReviewKindApprove,
		Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: manualRevision}
	applied, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID, approve)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Outcome != donationReviewOutcomeApplied || applied.EffectRevision != 2 {
		t.Fatalf("approve = %#v", applied)
	}
	receipt, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Items[0].State != "accepted" || receipt.Items[0].ReviewDecision != "approved" {
		t.Fatalf("receipt = %#v", receipt.Items[0])
	}
	if receipt.Items[0].CredentialID == nil || receipt.Items[0].AcceptedAtMS == nil {
		t.Fatal("accepted item is missing its credential or acceptance time")
	}
	// A replay of the same action returns the stored outcome instead of deciding
	// again, and a late conflicting rejection cannot overwrite the acceptance.
	replayed, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID, approve)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != applied {
		t.Fatalf("replay = %#v, want %#v", replayed, applied)
	}
	late, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(31), Kind: donationReviewKindReject,
			Actor: "admin-2", ExpectedItemRevision: receipt.Items[0].ItemRevision, Note: "late rejection"})
	if err != nil {
		t.Fatal(err)
	}
	if late.Outcome != donationReviewOutcomeRejected || late.ReasonCode != donationReviewReasonAlreadyReviewed {
		t.Fatalf("late decision = %#v", late)
	}
	var credentials int64
	if err := fixture.db.Model(&models.Credential{}).Where("fingerprint IS NOT NULL").Count(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	if credentials != 2 {
		t.Fatalf("credential count = %d, want the seeded plus exactly one accepted", credentials)
	}
}

func TestDonationManualRejectClearsStagingAndKeepsHistory(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-reject-group", "synthetic-inventory-key")
	source, request, _ := stageDonationReviewBatch(t, fixture, groupID, 40, "synthetic-reject-key")
	itemID := request.Items[0].ItemID

	rejected, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(50), Kind: donationReviewKindReject,
			Actor: "admin-1", ExpectedItemRevision: 1, Note: "上游账单异常，请核对后重新捐献"})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Outcome != donationReviewOutcomeApplied || rejected.EffectRevision != 2 {
		t.Fatalf("reject = %#v", rejected)
	}
	item := donationTestItem(t, fixture, source, itemID)
	if item.State != "rejected" || item.ReasonCode != donationReviewReasonRejected || item.EncryptedPayload != "" {
		t.Fatalf("rejected item = %#v", item)
	}
	if item.ReviewDecision != "rejected" || item.ReviewActionID != donationTestID(50) || item.ReviewedAtMS == 0 {
		t.Fatalf("rejected item lost its decision: %#v", item)
	}
	if item.ExpiresAtMS == 0 || item.BatchID != request.BatchID {
		t.Fatal("rejection rewrote the historical batch or deadline")
	}
	var credentials int64
	if err := fixture.db.Model(&models.Credential{}).Where("fingerprint IS NOT NULL").Count(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	if credentials != 1 {
		t.Fatal("rejection created a credential")
	}
	// The free-text reason lives only on the action, never in the closed reason set.
	action, err := fixture.service.GetDonationReviewAction(t.Context(), source, donationTestID(50))
	if err != nil || action.Outcome != donationReviewOutcomeApplied {
		t.Fatalf("action = %#v, %v", action, err)
	}
}

func TestDonationReviewRefusesStaleRevisionAndChangedTarget(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-conflict-group", "synthetic-inventory-key")
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 60, "synthetic-conflict-key")
	itemID := request.Items[0].ItemID

	stale, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(70), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 0, ReviewTargetRevision: manualRevision})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Outcome != donationReviewOutcomeRejected || stale.ReasonCode != donationReviewReasonRevisionMismatch {
		t.Fatalf("stale approve = %#v", stale)
	}
	changed, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(71), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Outcome != donationReviewOutcomeRejected || changed.ReasonCode != donationReviewReasonTargetChanged {
		t.Fatalf("changed target approve = %#v", changed)
	}
	// A refusal still leaves the item decidable by a correct command.
	item := donationTestItem(t, fixture, source, itemID)
	if item.State != "pending_review" || item.ItemRevision != 1 {
		t.Fatalf("refusals mutated the item: %#v", item)
	}
}

func TestDonationEnterReviewPreservesLegacyAutoHistory(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-entry-group", "synthetic-inventory-key")
	// A rate-limited answer is inconclusive, so the automatic worker retries
	// until its attempt budget is exhausted instead of deciding the key.
	executor := &donationChatTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		return failedCredentialProbeResult(http.StatusTooManyRequests, execution.ErrorKindHTTP, execution.FailureHintRateLimited)
	}}
	fixture.service.executor = executor
	source, request := stageDonationTestBatch(t, fixture, groupID, 80, "synthetic-exhausted-key")
	itemID := request.Items[0].ItemID
	for attempt := 0; attempt < donationMaxAttempts; attempt++ {
		fixture.service.now = func() time.Time { return time.Now().Add(time.Duration(attempt+2) * time.Minute) }
		if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	fixture.service.now = time.Now
	before := donationTestItem(t, fixture, source, itemID)
	if before.State != "retry_pending" || before.ReasonCode != "retry_exhausted" {
		t.Fatalf("legacy item = %#v", before)
	}
	var batchBefore models.DonationBatch
	if err := fixture.db.Where("source_id = ? AND batch_id = ?", source, request.BatchID).Take(&batchBefore).Error; err != nil {
		t.Fatal(err)
	}
	entered, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(95), Kind: donationReviewKindEnterReview,
			Actor: "admin-1", ExpectedItemRevision: before.ItemRevision,
			ReviewTargetRevision: mustReviewContext(t, fixture, source, request.BatchID, itemID).ReviewTargetRevision})
	if err != nil {
		t.Fatal(err)
	}
	if entered.Outcome != donationReviewOutcomeApplied {
		t.Fatalf("enter_review = %#v", entered)
	}
	after := donationTestItem(t, fixture, source, itemID)
	if after.State != "pending_review" || after.EffectiveMode != donationModeManualReview {
		t.Fatalf("entered item = %#v", after)
	}
	// The original batch, request summary, automatic attempts and 7-day deadline
	// must survive an explicit conversion.
	if after.BatchID != before.BatchID || after.Attempts != before.Attempts || after.ExpiresAtMS != before.ExpiresAtMS ||
		after.Fingerprint != before.Fingerprint || after.EntryActionID != donationTestID(95) {
		t.Fatalf("conversion rewrote history: before=%#v after=%#v", before, after)
	}
	var batchAfter models.DonationBatch
	if err := fixture.db.Where("source_id = ? AND batch_id = ?", source, request.BatchID).Take(&batchAfter).Error; err != nil {
		t.Fatal(err)
	}
	if batchAfter.RequestDigest != batchBefore.RequestDigest || batchAfter.ValidationMode != donationModeAuto ||
		batchAfter.TargetRevision != batchBefore.TargetRevision {
		t.Fatal("conversion rewrote the original batch")
	}
	// The legacy worker must not resume automatic processing for this item.
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if donationTestItem(t, fixture, source, itemID).State != "pending_review" {
		t.Fatal("the automatic worker reclaimed a converted item")
	}
}

func TestDonationExpiredReviewCannotBeApproved(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-expiry-group", "synthetic-inventory-key")
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 100, "synthetic-expiry-key")
	itemID := request.Items[0].ItemID
	fixture.service.now = func() time.Time { return time.Now().Add(donationStagingTTL + time.Hour) }
	expired, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(110), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: manualRevision})
	if err != nil {
		t.Fatal(err)
	}
	if expired.Outcome != donationReviewOutcomeRejected || expired.ReasonCode != donationReviewReasonStagingExpired {
		t.Fatalf("expired approve = %#v", expired)
	}
	// The slow reconciliation closes overdue staging without probing anything.
	if err := fixture.service.ReconcileDonationReviews(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := donationTestItem(t, fixture, source, itemID)
	if item.State != "invalid" || item.ReasonCode != "staging_expired" || item.EncryptedPayload != "" {
		t.Fatalf("expired item = %#v", item)
	}
}

func TestDonationTestUsesOnlyTheStagedKeyAndReplaysWithoutSecondCall(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-test-group", "synthetic-inventory-key")
	executor := &donationChatTestExecutor{result: successfulChatResult("synthetic-reply")}
	fixture.service.executor = executor
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 120, "synthetic-called-key")
	itemID := request.Items[0].ItemID

	testRequest := DonationTestRequest{TestID: donationTestID(130), Actor: "admin-1", ExpectedItemRevision: 1,
		ReviewTargetRevision: manualRevision, Model: "gpt-4o", Prompt: "hello", MaxOutputTokens: 64}
	result, err := fixture.service.RunDonationTest(t.Context(), source, request.BatchID, itemID, testRequest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != donationTestStateSucceeded || result.Text != "synthetic-reply" {
		t.Fatalf("test result = %#v", result)
	}
	calls := executor.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("executor calls = %d", len(calls))
	}
	call := calls[0]
	if call.Operation != execution.OperationChatCompletion || call.ClientProtocol != "openai-completions" {
		t.Fatalf("wrong operation: %#v", call.Operation)
	}
	var credential struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(call.Credential.Data(), &credential); err != nil {
		t.Fatal(err)
	}
	if credential.APIKey != "synthetic-called-key" {
		t.Fatalf("test used %q instead of the staged key", credential.APIKey)
	}
	if call.UpstreamModel != "gpt-4o" || !strings.Contains(string(call.Body), "hello") {
		t.Fatalf("test did not send the selected model and prompt: %s", call.Body)
	}
	// A replay of the same test ID answers from storage and never calls upstream.
	replay, err := fixture.service.RunDonationTest(t.Context(), source, request.BatchID, itemID, testRequest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if replay.State != donationTestStateSucceeded || len(executor.recordedCalls()) != 1 {
		t.Fatalf("replay re-sent the request: %#v", replay)
	}
	// The metadata query must not expose the response text.
	stored, err := fixture.service.GetDonationTest(t.Context(), source, donationTestID(130))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Text != "" || stored.OutputBytes == 0 {
		t.Fatalf("stored test = %#v", stored)
	}
}

func TestDonationTestFailsClosedWhenNoChatModelIsSupported(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-no-chat-group", "synthetic-inventory-key")
	source, request, _ := stageDonationReviewBatch(t, fixture, groupID, 140, "synthetic-no-chat-key")
	itemID := request.Items[0].ItemID
	executor := &donationChatTestExecutor{result: successfulChatResult("unreachable")}
	fixture.service.executor = executor
	result, err := fixture.service.RunDonationTest(t.Context(), source, request.BatchID, itemID,
		DonationTestRequest{TestID: donationTestID(150), Actor: "admin-1", ExpectedItemRevision: 1,
			ReviewTargetRevision: request.TargetRevision, Model: "not-a-group-model", Prompt: "hello"}, nil, nil)
	if err == nil {
		t.Fatal("an unsupported model was accepted")
	}
	if len(executor.recordedCalls()) != 0 {
		t.Fatal("an unsupported model still reached the upstream")
	}
	if result.State == donationTestStateSucceeded {
		t.Fatal("a refused test reported success")
	}
}

func TestDonationStreamRedactsKeyEchoedAcrossChunks(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-stream-group", "synthetic-inventory-key")
	key := "synthetic-echoed-stream-key-0123456789"
	head, tail := key[:18], key[18:]
	executor := &donationChatTestExecutor{
		streamFrames: [][]byte{sseChatFrame("prefix " + head), sseChatFrame(tail + " suffix"), []byte("data: [DONE]\n\n")},
		streamResult: execution.StreamResult{DispatchState: execution.DispatchMaybeSent, ResponseStarted: true,
			StatusCode: http.StatusOK, Header: http.Header{}},
	}
	fixture.service.executor = executor
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 160, key)
	itemID := request.Items[0].ItemID

	var streamed strings.Builder
	result, err := fixture.service.RunDonationTest(t.Context(), source, request.BatchID, itemID,
		DonationTestRequest{TestID: donationTestID(170), Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: manualRevision,
			Model: "gpt-4o", Prompt: "echo", Stream: true},
		func(event donationProjectedEvent) error {
			streamed.WriteString(event.Text)
			return nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != donationTestStateSucceeded {
		t.Fatalf("stream result = %#v", result)
	}
	if strings.Contains(streamed.String(), key) {
		t.Fatalf("the staged key leaked into the projected stream: %q", streamed.String())
	}
	if !strings.Contains(streamed.String(), donationRedactionMarker) {
		t.Fatalf("the echoed key was not redacted: %q", streamed.String())
	}
	if !strings.Contains(streamed.String(), "prefix ") || !strings.Contains(streamed.String(), " suffix") {
		t.Fatalf("redaction removed legitimate text: %q", streamed.String())
	}
}

func TestDonationTestLeaseBlocksConcurrentReviews(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "manual-lease-group", "synthetic-inventory-key")
	source, request, manualRevision := stageDonationReviewBatch(t, fixture, groupID, 180, "synthetic-lease-key")
	itemID := request.Items[0].ItemID
	executor := &donationChatTestExecutor{}
	fixture.service.executor = executor
	item := donationTestItem(t, fixture, source, itemID)
	nowMS := fixture.service.donationNowMS()
	attempt := models.DonationTestAttempt{SourceID: source, TestID: donationTestID(190), BatchID: request.BatchID,
		ItemID: itemID, Actor: "admin-1", StartRevision: item.ItemRevision, TargetRevision: item.ReviewTargetRevision,
		Model: "gpt-4o", InputDigest: strings.Repeat("c", 64), State: donationTestStateRunning,
		LeaseToken: donationTestID(191), LeaseUntilMS: nowMS + 60_000, StartedAtMS: nowMS}
	if err := fixture.db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ApplyDonationReviewAction(t.Context(), source, request.BatchID, itemID,
		DonationReviewActionRequest{ActionID: donationTestID(192), Kind: donationReviewKindApprove,
			Actor: "admin-1", ExpectedItemRevision: item.ItemRevision, ReviewTargetRevision: manualRevision}); err != nil {
		t.Fatal(err)
	}
	context := mustReviewContext(t, fixture, source, request.BatchID, itemID)
	if context.CanReview || context.UnavailableReason != donationReviewReasonTestRunning {
		t.Fatalf("a running test did not block review: %#v", context)
	}
	// Once the lease is abandoned the reconciliation closes it as interrupted and
	// never re-sends the model request.
	fixture.db.Model(&models.DonationTestAttempt{}).Where("id = ?", attempt.ID).Update("lease_until_ms", nowMS-1)
	if err := fixture.service.ReconcileDonationReviews(t.Context()); err != nil {
		t.Fatal(err)
	}
	var stored models.DonationTestAttempt
	if err := fixture.db.Where("id = ?", attempt.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.State != donationTestStateInterrupted || len(executor.recordedCalls()) != 0 {
		t.Fatalf("abandoned lease = %#v with %d calls", stored.State, len(executor.recordedCalls()))
	}
}

func mustReviewContext(t *testing.T, fixture serviceFixture, source, batchID, itemID string) DonationReviewContext {
	t.Helper()
	result, err := fixture.service.GetDonationReviewContext(t.Context(), source, batchID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func acquiredDonationResources(t *testing.T, fixture serviceFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.db.Model(&models.DonationResource{}).Where("acquired_at_ms IS NOT NULL").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
