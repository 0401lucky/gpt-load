package control

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gpt-load/internal/execution"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/protocol"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
)

var (
	// errDonationResponseTooLarge marks a bounded-execution stop. It is recorded as
	// a test reason, never surfaced as an infrastructure failure.
	errDonationResponseTooLarge = errors.New("donation test response exceeded its limit")
	errDonationTestReplay       = errors.New("donation test insert collided")
)

const (
	donationTestStateRunning     = "running"
	donationTestStateSucceeded   = "succeeded"
	donationTestStateFailed      = "failed"
	donationTestStateCancelled   = "cancelled"
	donationTestStateInterrupted = "interrupted"

	donationTestReasonTimeout        = "timeout"
	donationTestReasonCancelled      = "cancelled"
	donationTestReasonInterrupted    = "interrupted"
	donationTestReasonUpstreamError  = "upstream_error"
	donationTestReasonRateLimited    = "rate_limited"
	donationTestReasonUnauthorized   = "unauthorized"
	donationTestReasonModelMissing   = "model_unavailable"
	donationTestReasonEmptyOutput    = "empty_output"
	donationTestReasonTooLarge       = "response_too_large"
	donationTestReasonTargetChanged  = "target_changed"
	donationTestReasonTargetMissing  = "target_unavailable"
	donationTestReasonStagingExpired = "staging_expired"
	donationTestReasonUnavailable    = "test_unavailable"
	donationTestReasonIncomplete     = "incomplete_stream"
)

type DonationTestRequest struct {
	TestID               string `json:"test_id"`
	Actor                string `json:"actor"`
	ExpectedItemRevision int64  `json:"expected_item_revision"`
	ReviewTargetRevision string `json:"review_target_revision"`
	Model                string `json:"model"`
	Prompt               string `json:"prompt"`
	SystemPrompt         string `json:"system_prompt"`
	MaxOutputTokens      int    `json:"max_output_tokens"`
	Stream               bool   `json:"stream"`
}

// DonationTestResult is the metadata-only projection. Text is populated for the
// non-streaming response and is never persisted.
type DonationTestResult struct {
	TestID         string `json:"test_id"`
	BatchID        string `json:"batch_id"`
	ItemID         string `json:"item_id"`
	Model          string `json:"model"`
	Stream         bool   `json:"stream"`
	State          string `json:"state"`
	ReasonCode     string `json:"reason_code"`
	StatusCode     int    `json:"status_code"`
	StartRevision  int64  `json:"start_revision"`
	TargetRevision string `json:"target_revision"`
	StartedAtMS    int64  `json:"started_at_ms"`
	FinishedAtMS   int64  `json:"finished_at_ms"`
	OutputBytes    int64  `json:"output_bytes"`
	// Text is returned only by the non-streaming call that produced it. The
	// metadata query never populates it, and nothing here is ever persisted.
	Text  string         `json:"text,omitempty"`
	Usage *donationUsage `json:"usage,omitempty"`
}

func (r DonationTestResult) withText(text string) DonationTestResult {
	r.Text = text
	return r
}

func donationTestResultFromRow(row models.DonationTestAttempt) DonationTestResult {
	result := DonationTestResult{TestID: row.TestID, BatchID: row.BatchID, ItemID: row.ItemID, Model: row.Model,
		Stream: row.Stream, State: row.State, ReasonCode: row.ReasonCode, StatusCode: row.StatusCode,
		StartRevision: row.StartRevision, TargetRevision: row.TargetRevision, StartedAtMS: row.StartedAtMS,
		FinishedAtMS: row.FinishedAtMS, OutputBytes: row.OutputBytes}
	if row.InputTokens != nil && row.OutputTokens != nil {
		result.Usage = &donationUsage{InputTokens: *row.InputTokens, OutputTokens: *row.OutputTokens}
	}
	return result
}

func validateDonationTestRequest(source string, request DonationTestRequest) error {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(request.TestID) != nil {
		return app_errors.ErrBadRequest
	}
	if request.ExpectedItemRevision < 0 || !validDonationTargetRevision(request.ReviewTargetRevision) || !validDonationActor(request.Actor) {
		return app_errors.ErrBadRequest
	}
	if !utf8.ValidString(request.Prompt) || !utf8.ValidString(request.SystemPrompt) || !utf8.ValidString(request.Model) ||
		strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(request.Model) == "" || len(request.Model) > 255 {
		return app_errors.ErrBadRequest
	}
	if len(request.Prompt)+len(request.SystemPrompt) > donationMaxPromptBytes {
		return app_errors.ErrRequestTooLarge
	}
	if request.MaxOutputTokens == 0 {
		request.MaxOutputTokens = donationDefaultOutputTokens
	}
	if request.MaxOutputTokens < 1 || request.MaxOutputTokens > donationMaxOutputTokens {
		return app_errors.ErrBadRequest
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > donationMaxTestRequestBytes {
		return app_errors.ErrRequestTooLarge
	}
	return nil
}

func (s *Service) donationTestDigest(request DonationTestRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		Actor                string `json:"actor"`
		ExpectedItemRevision int64  `json:"expected_item_revision"`
		ReviewTargetRevision string `json:"review_target_revision"`
		Model                string `json:"model"`
		Prompt               string `json:"prompt"`
		SystemPrompt         string `json:"system_prompt"`
		MaxOutputTokens      int    `json:"max_output_tokens"`
		Stream               bool   `json:"stream"`
	}{request.Actor, request.ExpectedItemRevision, request.ReviewTargetRevision, request.Model,
		request.Prompt, request.SystemPrompt, request.MaxOutputTokens, request.Stream})
	if err != nil {
		return "", app_errors.ErrInternalServer
	}
	return s.encryption.Hash("gpt-load/donation-test/v1\x00" + string(encoded)), nil
}

// donationTestClaim carries the frozen facts a running test needs after the
// claiming transaction has committed and released its locks.
type donationTestClaim struct {
	attemptID      uint
	itemID         uint
	itemUUID       string
	batchID        string
	groupID        uint
	fingerprint    string
	encrypted      string
	expiresAtMS    int64
	startedAtMS    int64
	revision       int64
	targetRevision string
	leaseToken     string
}

func (s *Service) GetDonationTest(ctx context.Context, source, testID string) (DonationTestResult, error) {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(testID) != nil {
		return DonationTestResult{}, app_errors.ErrBadRequest
	}
	var result DonationTestResult
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		var row models.DonationTestAttempt
		if err := tx.Where("source_id = ? AND test_id = ?", source, testID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return app_errors.ErrDonationTestNotFound
			}
			return app_errors.ParseDBError(err)
		}
		result = donationTestResultFromRow(row)
		return nil
	})
	return result, err
}

// RunDonationTest reserves one test ID, then performs exactly one call against
// the staged key of this item. A replay of the same test ID returns the stored
// metadata and never re-consumes the upstream.
func (s *Service) RunDonationTest(ctx context.Context, source, batchID, itemID string, request DonationTestRequest, sink func(donationProjectedEvent) error, emitMeta func(DonationTestResult) error) (DonationTestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, donationTestTotalTimeout)
	defer cancel()
	if validateIdempotencyKey(batchID) != nil || validateIdempotencyKey(itemID) != nil {
		return DonationTestResult{}, app_errors.ErrBadRequest
	}
	if err := validateDonationTestRequest(source, request); err != nil {
		return DonationTestResult{}, err
	}
	if request.MaxOutputTokens == 0 {
		request.MaxOutputTokens = donationDefaultOutputTokens
	}
	digest, err := s.donationTestDigest(request)
	if err != nil {
		return DonationTestResult{}, err
	}
	var claim *donationTestClaim
	var replay *DonationTestResult
	acquiredSlot := false
	defer func() {
		if acquiredSlot {
			<-s.donationTestSlots
		}
	}()
	if err := s.lockDonationTestWrite(ctx); err != nil {
		return DonationTestResult{}, err
	}
	err = s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		if _, err := s.ensureDonationIdentity(tx); err != nil {
			return err
		}
		var existing models.DonationTestAttempt
		query := tx.Where("source_id = ? AND test_id = ?", source, request.TestID).Limit(1).Find(&existing)
		if query.Error != nil {
			return app_errors.ParseDBError(query.Error)
		}
		if query.RowsAffected != 0 {
			if existing.InputDigest != digest || existing.ItemID != itemID || existing.BatchID != batchID {
				return app_errors.ErrDonationTestConflict
			}
			// A recorded attempt is never re-sent upstream, whatever its outcome.
			result := donationTestResultFromRow(existing)
			replay = &result
			return nil
		}
		return nil
	})
	if err == nil && replay == nil {
		err = s.withControlTransaction(ctx, func(tx *gorm.DB) error {
			// Establish this transaction's first consistent snapshot only after the
			// item lock. It sees any test the previous holder committed, without
			// taking a MySQL gap lock for a nonexistent test ID (which would deadlock
			// parallel inserts for unrelated items into an empty attempts table).
			var item models.DonationItem
			query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ? AND batch_id = ? AND item_id = ?", source, batchID, itemID).Limit(1).Find(&item)
			if query.Error != nil {
				return app_errors.ParseDBError(query.Error)
			}
			if query.RowsAffected == 0 {
				_, err := s.loadDonationItem(tx, source, batchID, itemID, false)
				return err
			}
			var existing models.DonationTestAttempt
			query = tx.Where("source_id = ? AND test_id = ?", source, request.TestID).Limit(1).Find(&existing)
			if query.Error != nil {
				return app_errors.ParseDBError(query.Error)
			}
			if query.RowsAffected != 0 {
				if existing.InputDigest != digest || existing.ItemID != itemID || existing.BatchID != batchID {
					return app_errors.ErrDonationTestConflict
				}
				result := donationTestResultFromRow(existing)
				replay = &result
				return nil
			}
			if normalizeStoredDonationMode(item.EffectiveMode) != donationModeManualReview || item.State != "pending_review" {
				return app_errors.ErrDonationTestUnavailable
			}
			nowMS := s.donationNowMS()
			if item.ExpiresAtMS <= nowMS || item.EncryptedPayload == "" {
				return app_errors.ErrDonationTestUnavailable
			}
			if item.ItemRevision != request.ExpectedItemRevision {
				return app_errors.ErrDonationTestConflict
			}
			if item.ReviewTargetRevision != request.ReviewTargetRevision {
				return app_errors.ErrDonationTestConflict
			}
			running, err := s.donationTestRunning(tx, source, item.ItemID, nowMS, false)
			if err != nil {
				return err
			}
			if running {
				return app_errors.ErrDonationTestBusy
			}
			resource, acquired, err := s.donationReviewResource(tx, &item)
			if err != nil {
				return err
			}
			if acquired || resource.OwnerItemID == nil || *resource.OwnerItemID != item.ID {
				return app_errors.ErrDonationTestUnavailable
			}
			target, _, err := s.loadDonationTarget(ctx, tx, item.GroupID)
			if err != nil {
				return err
			}
			if target.manualReason != "" || target.manualRevision != request.ReviewTargetRevision {
				return app_errors.ErrDonationTestUnavailable
			}
			if _, found := donationTestModel(target.group, request.Model); !found {
				return app_errors.ErrDonationTestUnavailable
			}
			if _, supported := target.group.ResolvedTarget.ModeForModel(protocol.OpenAICompletions, execution.OperationChatCompletion, request.Model); !supported {
				return app_errors.ErrDonationTestUnavailable
			}
			// Never queue a persisted lease behind the process concurrency limit.
			// The reserved call must start within its fixed 150-second lease.
			select {
			case s.donationTestSlots <- struct{}{}:
				acquiredSlot = true
			default:
				return app_errors.ErrDonationTestBusy
			}
			token, err := newOperationID(rand.Reader)
			if err != nil {
				return app_errors.ErrInternalServer
			}
			attempt := models.DonationTestAttempt{SourceID: source, TestID: request.TestID, BatchID: batchID,
				ItemID: itemID, Actor: request.Actor, StartRevision: item.ItemRevision,
				TargetRevision: item.ReviewTargetRevision, Model: request.Model, InputDigest: digest,
				Stream: request.Stream, State: donationTestStateRunning, LeaseToken: token,
				LeaseUntilMS: nowMS + donationTestLeaseDuration.Milliseconds(), StartedAtMS: nowMS}
			if err := tx.Create(&attempt).Error; err != nil {
				if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
					return errDonationTestReplay
				}
				return app_errors.ParseDBError(err)
			}
			if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).
				Updates(map[string]any{"item_revision": gorm.Expr("item_revision + 1"), "updated_at_ms": nowMS}).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			claim = &donationTestClaim{attemptID: attempt.ID, itemID: item.ID, itemUUID: item.ItemID,
				batchID: item.BatchID, groupID: item.GroupID, fingerprint: item.Fingerprint,
				encrypted: item.EncryptedPayload, expiresAtMS: item.ExpiresAtMS, revision: item.ItemRevision,
				targetRevision: item.ReviewTargetRevision, leaseToken: token, startedAtMS: nowMS}
			return nil
		})
	}
	s.writeMu.Unlock()
	if errors.Is(err, errDonationTestReplay) {
		var existing models.DonationTestAttempt
		if readErr := s.db.WithContext(ctx).Where("source_id = ? AND test_id = ?", source, request.TestID).Take(&existing).Error; readErr != nil {
			err = app_errors.ParseDBError(readErr)
		} else if existing.InputDigest != digest || existing.ItemID != itemID || existing.BatchID != batchID {
			err = app_errors.ErrDonationTestConflict
		} else {
			return donationTestResultFromRow(existing), nil
		}
	}
	if err != nil {
		return DonationTestResult{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	if claim == nil {
		return DonationTestResult{}, app_errors.ErrDonationTestBusy
	}
	return s.executeDonationTest(ctx, *claim, request, sink, emitMeta)
}

func (s *Service) executeDonationTest(ctx context.Context, claim donationTestClaim, request DonationTestRequest, sink func(donationProjectedEvent) error, emitMeta func(DonationTestResult) error) (DonationTestResult, error) {
	base := DonationTestResult{TestID: request.TestID, BatchID: claim.batchID, ItemID: claim.itemUUID,
		Model: request.Model, Stream: request.Stream, StartRevision: claim.revision,
		TargetRevision: claim.targetRevision, StartedAtMS: claim.startedAtMS, State: donationTestStateRunning}
	prepared, reason, err := s.prepareDonationTest(ctx, claim, request)
	if err != nil {
		finished, finishErr := s.finishDonationTest(ctx, claim, base, donationTestStateFailed, reason, 0, nil)
		return finished, errors.Join(err, finishErr)
	}
	defer s.retireCredentialRuntime(prepared.credentialID)
	defer prepared.close()

	total := prepared.spec.Timeouts.Request
	testCtx, cancel := context.WithTimeout(ctx, total)
	defer cancel()
	if emitMeta != nil {
		if err := emitMeta(base); err != nil {
			finished, finishErr := s.finishDonationTest(ctx, claim, base, donationTestStateCancelled, donationTestReasonCancelled, 0, nil)
			return finished, errors.Join(err, finishErr)
		}
	}

	secrets := prepared.secrets
	if request.Stream {
		return s.streamDonationTest(testCtx, claim, request, base, prepared, secrets, sink)
	}
	result := prepared.executor.Execute(testCtx, prepared.spec)
	stateName, reasonCode, statusCode, text, usage, classifyErr := classifyDonationTestResult(testCtx, result, secrets)
	if classifyErr != nil {
		stateName, reasonCode, text = donationTestStateFailed, donationTestReasonTooLarge, ""
	}
	finished := base
	finished.State, finished.ReasonCode, finished.StatusCode = stateName, reasonCode, statusCode
	finished.OutputBytes = int64(len(text))
	finished.Usage = usage
	finished, err = s.finishDonationTest(ctx, claim, finished, stateName, reasonCode, statusCode, usage)
	if err != nil || finished.State != donationTestStateSucceeded {
		return finished, err
	}
	return finished.withText(text), nil
}

func (s *Service) streamDonationTest(ctx context.Context, claim donationTestClaim, request DonationTestRequest, base DonationTestResult, prepared donationTestPrepared, secrets []string, sink func(donationProjectedEvent) error) (DonationTestResult, error) {
	projection := newDonationStreamProjection(secrets)
	var sinkErr error
	cancelledBySink := false
	upstreamRejected := false
	streamResult := prepared.executor.ExecuteStream(ctx, prepared.spec, func(event execution.StreamEvent) error {
		if event.Kind == execution.StreamEventReady {
			upstreamRejected = event.StatusCode < 200 || event.StatusCode >= 300
			return nil
		}
		if event.Kind != execution.StreamEventData {
			return nil
		}
		if upstreamRejected {
			// An HTTP error body is not an SSE conversation and must never enter
			// either the projection or a client-visible transport error message.
			return nil
		}
		events, err := projection.Push(event.Data)
		if err != nil {
			sinkErr = err
			return err
		}
		for _, projected := range events {
			projected.ctx = ctx
			if projectErr := sink(projected); projectErr != nil {
				sinkErr = projectErr
				cancelledBySink = true
				return projectErr
			}
		}
		return nil
	})
	tail, tailErr := projection.Finish()
	if tailErr != nil {
		sinkErr = tailErr
	} else if tail != "" {
		for _, projected := range donationTextEvents(tail) {
			projected.ctx = ctx
			if projectErr := sink(projected); projectErr != nil && sinkErr == nil {
				sinkErr = projectErr
				cancelledBySink = true
				break
			}
		}
	}
	stateName, reasonCode, statusCode := donationTestStateSucceeded, "", streamResult.StatusCode
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		stateName, reasonCode = donationTestStateFailed, donationTestReasonTimeout
	case ctx.Err() != nil || cancelledBySink:
		stateName, reasonCode = donationTestStateCancelled, donationTestReasonCancelled
	case errors.Is(sinkErr, errDonationResponseTooLarge):
		stateName, reasonCode = donationTestStateFailed, donationTestReasonTooLarge
	case streamResult.Error != nil && streamResult.Error.Kind == execution.ErrorKindTimeout:
		stateName, reasonCode = donationTestStateFailed, donationTestReasonTimeout
	case streamResult.Error != nil || streamResult.StatusCode < 200 || streamResult.StatusCode >= 300:
		stateName, reasonCode = donationTestStateFailed, donationTestReasonFromStatus(streamResult.StatusCode)
	case sinkErr != nil || !projection.Complete():
		// A stream that never reported completion is not a successful call.
		stateName, reasonCode = donationTestStateFailed, donationTestReasonIncomplete
	case !projection.Visible():
		stateName, reasonCode = donationTestStateFailed, donationTestReasonEmptyOutput
	}
	finished := base
	finished.State, finished.ReasonCode, finished.StatusCode = stateName, reasonCode, statusCode
	finished.OutputBytes = int64(projection.emittedBytes)
	finished.Usage = projection.Usage()
	finished, finishErr := s.finishDonationTest(ctx, claim, finished, stateName, reasonCode, statusCode, finished.Usage)
	if finishErr != nil {
		return finished, finishErr
	}
	return finished, nil
}

// donationTestPrepared holds the wire-ready attempt. It never exposes the key.
type donationTestPrepared struct {
	executor     execution.Executor
	spec         execution.AttemptSpec
	secrets      []string
	credentialID uint
	closer       func()
}

func (p donationTestPrepared) close() {
	if p.closer != nil {
		p.closer()
	}
}

func (s *Service) prepareDonationTest(ctx context.Context, claim donationTestClaim, request DonationTestRequest) (donationTestPrepared, string, error) {
	if s.executor == nil || s.encryption == nil || s.channelRegistry == nil {
		return donationTestPrepared{}, "internal_error", app_errors.ErrInternalServer
	}
	var view state.GroupView
	var target donationTarget
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		if err := donationVirtualCredentialRangeAvailable(tx); err != nil {
			return err
		}
		var loadErr error
		target, _, loadErr = s.loadDonationTarget(ctx, tx, claim.groupID)
		if loadErr != nil {
			return loadErr
		}
		view = target.group
		return nil
	})
	if err != nil {
		return donationTestPrepared{}, "internal_error", err
	}
	if target.manualReason != "" {
		return donationTestPrepared{}, donationTestReasonTargetMissing, app_errors.ErrDonationTestUnavailable
	}
	// The frozen manual target changed after the test was reserved. The old test
	// no longer proves anything about the current configuration.
	if target.manualRevision != claim.targetRevision {
		return donationTestPrepared{}, donationTestReasonTargetChanged, app_errors.ErrDonationTestUnavailable
	}
	if claim.expiresAtMS <= s.donationNowMS() {
		return donationTestPrepared{}, donationTestReasonStagingExpired, app_errors.ErrDonationTestUnavailable
	}
	model, found := donationTestModel(view, request.Model)
	if !found {
		return donationTestPrepared{}, donationTestReasonModelMissing, app_errors.ErrDonationTestUnavailable
	}
	routeMode, supported := view.ResolvedTarget.ModeForModel(protocol.OpenAICompletions, execution.OperationChatCompletion, model.ID)
	if !supported {
		return donationTestPrepared{}, donationTestReasonModelMissing, app_errors.ErrDonationTestUnavailable
	}
	plaintext, err := s.encryption.Decrypt(claim.encrypted)
	if err != nil {
		return donationTestPrepared{}, "credential_unavailable", app_errors.ErrInternalServer
	}
	credential, err := normalizeStoredCredential(s.channelRegistry, view.ChannelID, plaintext)
	plaintext = ""
	if err != nil {
		return donationTestPrepared{}, "credential_unavailable", app_errors.ErrInternalServer
	}
	apiKey, _ := credential.Value("api_key")
	if strings.TrimSpace(apiKey) == "" {
		return donationTestPrepared{}, "credential_unavailable", app_errors.ErrInternalServer
	}
	proxy, proxyFingerprint, err := validationAttemptProxy(s.encryption, view.Proxy, state.CredentialRef{})
	if err != nil {
		return donationTestPrepared{}, "proxy_unavailable", app_errors.ErrInternalServer
	}
	externalModel := model.ID
	if alias := strings.TrimSpace(model.Alias); alias != "" {
		externalModel = alias
	}
	body, err := donationChatRequestBody(externalModel, request)
	if err != nil {
		return donationTestPrepared{}, "request_invalid", app_errors.ErrBadRequest
	}
	// Group parameter overrides are ordinary request policy and must still apply
	// on this direct execution path, including their size and shape validation.
	overridden, _, err := view.ParameterOverrides.Apply(protocol.OpenAICompletions, execution.OperationChatCompletion, externalModel, body)
	if err != nil {
		return donationTestPrepared{}, "request_invalid", app_errors.ErrBadRequest
	}
	if len(overridden) > donationMaxTestRequestBytes {
		return donationTestPrepared{}, "request_invalid", app_errors.ErrRequestTooLarge
	}
	if err := validateDonationChatRequestBody(overridden, externalModel, request.Stream); err != nil {
		return donationTestPrepared{}, "request_invalid", err
	}
	requestID, err := newOperationID(rand.Reader)
	if err != nil {
		return donationTestPrepared{}, "internal_error", app_errors.ErrInternalServer
	}
	attemptID, err := newOperationID(rand.Reader)
	if err != nil {
		return donationTestPrepared{}, "internal_error", app_errors.ErrInternalServer
	}
	credentialID, validID := donationVirtualCredentialID(claim.attemptID, true)
	if !validID {
		return donationTestPrepared{}, donationTestReasonUnavailable, app_errors.ErrInternalServer
	}
	spec := execution.NewAttemptSpec(execution.AttemptSpec{
		RequestID: requestID, AttemptID: attemptID, Sequence: 1,
		ChannelID: string(view.ChannelID), RouteMode: routeMode,
		ClientProtocol: protocol.OpenAICompletions, Operation: execution.OperationChatCompletion,
		ClientModel: externalModel, UpstreamModel: model.ID,
		Method: http.MethodPost, Path: "/v1/chat/completions",
		Header: applyControlHeaderRules(view.HeaderRules, apiKey), ConfiguredHeaders: view.HeaderRules.ConfiguredNames(),
		Body: overridden, IncludeUsage: request.Stream,
		TargetConfig: view.ResolvedTarget.TargetConfig,
		Timeouts:     donationTestTimeouts(view, time.Duration(claim.expiresAtMS-s.donationNowMS())*time.Millisecond),
		Credential:   execution.NewCredentialSnapshot(credentialID, 1, 1, credential.CanonicalJSON()),
		Proxy:        proxy, ProxyFingerprint: proxyFingerprint,
	})
	if err := spec.Validate(); err != nil {
		return donationTestPrepared{}, "request_invalid", app_errors.ErrBadRequest
	}
	secrets := []string{apiKey}
	for _, values := range spec.Header {
		for _, value := range values {
			if value != "" {
				secrets = append(secrets, value)
			}
		}
	}
	return donationTestPrepared{executor: s.executor, spec: spec, secrets: secrets, credentialID: credentialID}, "", nil
}

// donationVirtualCredentialID reserves the unsigned upper half for donation
// execution. Odd IDs are automatic probes and even IDs are manual tests. Both
// mappings are injective and disjoint from positive signed database IDs. The
// explicit quarter-range bound prevents underflow or crossing that boundary.
func donationVirtualCredentialID(id uint, manual bool) (uint, bool) {
	if id == 0 || id > ^uint(0)/4 {
		return 0, false
	}
	virtual := ^uint(0) - 2*id
	if manual {
		virtual++
	}
	return virtual, true
}

// SQLite and PostgreSQL cannot store IDs in this unsigned range; MySQL's uint
// columns can if an operator imported explicit oversized IDs. Refuse synthetic
// execution in that case. The query parameter itself fits a signed integer on
// every driver, including SQLite and PostgreSQL.
func donationVirtualCredentialRangeAvailable(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.Credential{}).Where("id > ?", ^uint(0)/2).Count(&count).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	if count != 0 {
		return app_errors.ErrDonationTestUnavailable
	}
	return nil
}

func donationTestModel(view state.GroupView, requested string) (state.ModelConfig, bool) {
	requested = strings.TrimSpace(requested)
	for _, model := range view.Models {
		if model.ID == requested {
			return model, true
		}
	}
	return state.ModelConfig{}, false
}

func donationChatRequestBody(model string, request DonationTestRequest) ([]byte, error) {
	messages := make([]map[string]string, 0, 2)
	if strings.TrimSpace(request.SystemPrompt) != "" {
		messages = append(messages, map[string]string{"role": "system", "content": request.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": request.Prompt})
	payload := map[string]any{"model": model, "messages": messages, "max_tokens": request.MaxOutputTokens}
	if request.Stream {
		payload["stream"] = true
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(body) > donationMaxTestRequestBytes {
		return nil, errDonationResponseTooLarge
	}
	return body, nil
}

// A group's overrides still apply, but a donation test remains a bounded text
// call. Reject incompatible policy instead of silently dropping it or allowing
// tools, attachments, multiple completions or an unbounded output budget.
func validateDonationChatRequestBody(body []byte, model string, stream bool) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return app_errors.ErrBadRequest
	}
	for field := range fields {
		switch field {
		case "model", "messages", "max_tokens", "max_completion_tokens", "stream", "stream_options", "n",
			"temperature", "top_p", "presence_penalty", "frequency_penalty", "seed", "stop", "logit_bias",
			"logprobs", "top_logprobs", "user", "reasoning_effort", "service_tier", "verbosity", "response_format", "metadata":
		default:
			return app_errors.ErrDonationTestUnavailable
		}
	}
	var actualModel string
	if json.Unmarshal(fields["model"], &actualModel) != nil || actualModel != model {
		return app_errors.ErrBadRequest
	}
	var actualStream bool
	if raw, ok := fields["stream"]; ok && json.Unmarshal(raw, &actualStream) != nil {
		return app_errors.ErrBadRequest
	}
	if actualStream != stream {
		return app_errors.ErrBadRequest
	}
	bounded := false
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		if raw, ok := fields[field]; ok {
			var tokens int
			if json.Unmarshal(raw, &tokens) != nil || tokens < 1 || tokens > donationMaxOutputTokens {
				return app_errors.ErrDonationTestUnavailable
			}
			bounded = true
		}
	}
	if !bounded {
		return app_errors.ErrDonationTestUnavailable
	}
	if raw, ok := fields["n"]; ok {
		var count int
		if json.Unmarshal(raw, &count) != nil || count != 1 {
			return app_errors.ErrDonationTestUnavailable
		}
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(fields["messages"], &messages) != nil || len(messages) == 0 || len(messages) > 2 {
		return app_errors.ErrDonationTestUnavailable
	}
	textBytes, userMessages := 0, 0
	for _, message := range messages {
		if len(message) != 2 {
			return app_errors.ErrDonationTestUnavailable
		}
		var role, content string
		if json.Unmarshal(message["role"], &role) != nil || json.Unmarshal(message["content"], &content) != nil || !utf8.ValidString(content) {
			return app_errors.ErrDonationTestUnavailable
		}
		switch role {
		case "user":
			if strings.TrimSpace(content) == "" {
				return app_errors.ErrDonationTestUnavailable
			}
			userMessages++
		case "system", "developer":
		default:
			return app_errors.ErrDonationTestUnavailable
		}
		textBytes += len(content)
	}
	if userMessages != 1 || textBytes > donationMaxPromptBytes {
		return app_errors.ErrDonationTestUnavailable
	}
	return nil
}

// donationTestTimeouts takes the stricter of the group policy and the fixed
// first-version ceiling, and never outlives the remaining staging window.
func donationTestTimeouts(view state.GroupView, remaining time.Duration) execution.AttemptTimeouts {
	firstByte := donationTestFirstByteTimeout
	if view.Timeouts.FirstByte > 0 && view.Timeouts.FirstByte < firstByte {
		firstByte = view.Timeouts.FirstByte
	}
	idle := donationTestIdleTimeout
	if view.Timeouts.StreamIdle > 0 && view.Timeouts.StreamIdle < idle {
		idle = view.Timeouts.StreamIdle
	}
	request := donationTestTotalTimeout
	if view.Timeouts.Request > 0 && view.Timeouts.Request < request {
		request = view.Timeouts.Request
	}
	if remaining > 0 && remaining < request {
		request = remaining
	}
	return execution.AttemptTimeouts{FirstByte: firstByte, Request: request, StreamIdle: idle}
}

func donationTestReasonFromStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return donationTestReasonUnauthorized
	case status == http.StatusTooManyRequests:
		return donationTestReasonRateLimited
	case status == http.StatusNotFound:
		return donationTestReasonModelMissing
	default:
		return donationTestReasonUpstreamError
	}
}

func classifyDonationTestResult(ctx context.Context, result execution.AttemptResult, secrets []string) (string, string, int, string, *donationUsage, error) {
	status := result.StatusCode
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return donationTestStateFailed, donationTestReasonTimeout, status, "", nil, nil
	}
	if err := ctx.Err(); err != nil {
		return donationTestStateCancelled, donationTestReasonCancelled, status, "", nil, nil
	}
	if result.Error != nil {
		if result.Error.Kind == execution.ErrorKindTimeout {
			return donationTestStateFailed, donationTestReasonTimeout, status, "", nil, nil
		}
		return donationTestStateFailed, donationTestReasonFromStatus(result.Error.StatusCode), status, "", nil, nil
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return donationTestStateFailed, donationTestReasonFromStatus(status), status, "", nil, nil
	}
	text, usage, visible, err := projectDonationChatBody(result.Body, secrets)
	if err != nil {
		if errors.Is(err, errDonationResponseTooLarge) {
			return donationTestStateFailed, donationTestReasonTooLarge, status, "", nil, nil
		}
		return donationTestStateFailed, donationTestReasonUpstreamError, status, "", nil, nil
	}
	if !visible {
		return donationTestStateFailed, donationTestReasonEmptyOutput, status, "", usage, nil
	}
	return donationTestStateSucceeded, "", status, text, usage, nil
}

// finishDonationTest persists the terminal state under a short cleanup context so
// a cancelled caller still records the real outcome and frees the lease.
func (s *Service) finishDonationTest(ctx context.Context, claim donationTestClaim, result DonationTestResult, stateName, reason string, statusCode int, usage *donationUsage) (DonationTestResult, error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTransactionCleanupTimeout)
	defer cancel()
	if err := s.lockDonationTestWrite(cleanupCtx); err != nil {
		result.State, result.ReasonCode, result.Text = donationTestStateInterrupted, donationTestReasonInterrupted, ""
		return result, err
	}
	defer s.writeMu.Unlock()
	var persisted DonationTestResult
	finishErr := s.withControlTransaction(cleanupCtx, func(tx *gorm.DB) error {
		// Keep the same item -> attempt lock order as claiming and reviewing.
		var item models.DonationItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claim.itemID).Take(&item).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		var row models.DonationTestAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claim.attemptID).Take(&row).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		if row.State != donationTestStateRunning {
			persisted = donationTestResultFromRow(row)
			return nil
		}
		if claim.leaseToken == "" || row.LeaseToken != claim.leaseToken {
			return app_errors.ErrDonationTestConflict
		}
		nowMS := s.donationNowMS()
		if row.LeaseUntilMS <= nowMS {
			stateName, reason = donationTestStateInterrupted, donationTestReasonInterrupted
		}
		row.State, row.ReasonCode, row.StatusCode = stateName, reason, statusCode
		row.OutputBytes, row.FinishedAtMS = max(result.OutputBytes, 0), max(nowMS, row.StartedAtMS)
		row.LeaseToken, row.LeaseUntilMS = "", 0
		if usage != nil {
			row.InputTokens, row.OutputTokens = &usage.InputTokens, &usage.OutputTokens
		}
		updates := map[string]any{"state": row.State, "reason_code": row.ReasonCode, "status_code": row.StatusCode,
			"output_bytes": row.OutputBytes, "finished_at_ms": row.FinishedAtMS,
			"input_tokens": row.InputTokens, "output_tokens": row.OutputTokens, "lease_token": "", "lease_until_ms": 0}
		if err := tx.Model(&models.DonationTestAttempt{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).
			Updates(map[string]any{"item_revision": gorm.Expr("item_revision + 1"), "updated_at_ms": nowMS}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		persisted = donationTestResultFromRow(row)
		return nil
	})
	if finishErr != nil {
		// Never publish an undurable success. The original running lease remains
		// available for cold reconciliation, without a second upstream call.
		result.State, result.ReasonCode, result.Text = donationTestStateInterrupted, donationTestReasonInterrupted, ""
		return result, finishErr
	}
	return persisted, nil
}

// A short cleanup context must also bound waiting for the process write lock,
// not just the SQL issued after it. No upstream work runs while holding it.
func (s *Service) lockDonationTestWrite(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.writeMu.TryLock() {
		return nil
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.writeMu.TryLock() {
				return nil
			}
		}
	}
}

// ReconcileDonationReviews expires overdue manual items and closes test leases
// abandoned by a crash. It never probes and never calls an upstream.
func (s *Service) ReconcileDonationReviews(ctx context.Context) error {
	nowMS := s.donationNowMS()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var items []models.DonationItem
	if err := s.db.WithContext(ctx).Where("(state = ? AND next_attempt_at_ms <= ?) OR EXISTS "+
		"(SELECT 1 FROM donation_test_attempts WHERE donation_test_attempts.source_id = donation_items.source_id "+
		"AND donation_test_attempts.item_id = donation_items.item_id AND donation_test_attempts.state = ? AND donation_test_attempts.lease_until_ms <= ?)",
		"pending_review", nowMS, donationTestStateRunning, nowMS).
		Order("next_attempt_at_ms ASC, id ASC").Limit(32).Find(&items).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	var failures []error
	for _, selected := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
			var item models.DonationItem
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", selected.ID).Take(&item).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			var expired []models.DonationTestAttempt
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ? AND item_id = ? AND state = ? AND lease_until_ms <= ?",
				item.SourceID, item.ItemID, donationTestStateRunning, nowMS).Order("id ASC").Limit(32).Find(&expired).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			for _, attempt := range expired {
				if err := tx.Model(&models.DonationTestAttempt{}).Where("id = ?", attempt.ID).
					Updates(map[string]any{"state": donationTestStateInterrupted, "reason_code": donationTestReasonInterrupted,
						"finished_at_ms": max(nowMS, attempt.StartedAtMS), "lease_token": "", "lease_until_ms": 0}).Error; err != nil {
					return app_errors.ParseDBError(err)
				}
			}
			if len(expired) > 0 {
				item.ItemRevision += int64(len(expired))
				if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).
					Updates(map[string]any{"item_revision": item.ItemRevision, "updated_at_ms": nowMS}).Error; err != nil {
					return app_errors.ParseDBError(err)
				}
			}
			if err := s.reconcileDonationReviewItem(tx, &item); err != nil {
				return err
			}
			// Move inspected pending items behind the next bounded batch so a
			// permanently busy key cannot starve later expiry/ownership work.
			if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", item.ID, "pending_review").
				Update("next_attempt_at_ms", nowMS+time.Minute.Milliseconds()).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			return nil
		}); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
