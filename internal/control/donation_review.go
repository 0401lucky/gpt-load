package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
	stateloader "gpt-load/internal/state/loader"
	"gpt-load/internal/storage/models"
)

// Definite business refusals of a well-formed review command. They are recorded
// on the action row so a caller can reconcile a lost response, and they are
// deliberately distinct from the transport and validation failures that must
// never be persisted as a business outcome.
const (
	donationReviewReasonRevisionMismatch  = "revision_mismatch"
	donationReviewReasonModeConflict      = "mode_conflict"
	donationReviewReasonTargetChanged     = "target_changed"
	donationReviewReasonTargetUnavailable = "target_unavailable"
	donationReviewReasonStagingExpired    = "staging_expired"
	donationReviewReasonItemState         = "item_state"
	donationReviewReasonAlreadyReviewed   = "already_reviewed"
	donationReviewReasonNotReviewable     = "not_reviewable"
	donationReviewReasonTestRunning       = "test_running"
	donationReviewReasonResourceBusy      = "resource_busy"
	donationReviewReasonModelUnavailable  = "model_unavailable"
)

const (
	donationReviewKindEnterReview = "enter_review"
	donationReviewKindApprove     = "approve"
	donationReviewKindReject      = "reject"
	donationReviewOutcomeApplied  = "applied"
	donationReviewOutcomeRejected = "rejected"
	donationReviewReasonRejected  = "review_rejected"
	donationMaxActorBytes         = 128
)

var errDonationReviewActionReplay = errors.New("donation review action insert collided")

func validDonationReviewKind(kind string) bool {
	switch kind {
	case donationReviewKindEnterReview, donationReviewKindApprove, donationReviewKindReject:
		return true
	default:
		return false
	}
}

type DonationReviewContext struct {
	BatchID              string               `json:"batch_id"`
	ItemID               string               `json:"item_id"`
	GroupID              uint                 `json:"group_id"`
	State                string               `json:"state"`
	EffectiveMode        string               `json:"effective_mode"`
	ItemRevision         int64                `json:"item_revision"`
	ReviewTargetRevision string               `json:"review_target_revision"`
	ExpiresAtMS          int64                `json:"expires_at_ms"`
	CanReview            bool                 `json:"can_review"`
	CanReject            bool                 `json:"can_reject"`
	CanTest              bool                 `json:"can_test"`
	UnavailableReason    string               `json:"unavailable_reason"`
	TestModels           []string             `json:"test_models"`
	ReviewLimits         DonationReviewLimits `json:"review_limits"`
	// ReviewAction identifies enter_review or approve. CanReject independently
	// describes rejection, which does not depend on a live unchanged target.
	ReviewAction string `json:"review_action"`
}

type DonationReviewActionRequest struct {
	ActionID string `json:"action_id"`
	Kind     string `json:"kind"`
	// Actor is the caller's authenticated operator identity. gpt-load never
	// derives it from the integration token, which is shared by every command.
	Actor                string `json:"actor"`
	ExpectedItemRevision int64  `json:"expected_item_revision"`
	ReviewTargetRevision string `json:"review_target_revision"`
	Note                 string `json:"note"`
}

type DonationReviewActionResponse struct {
	ActionID             string `json:"action_id"`
	BatchID              string `json:"batch_id"`
	ItemID               string `json:"item_id"`
	Kind                 string `json:"kind"`
	ExpectedItemRevision int64  `json:"expected_item_revision"`
	ReviewTargetRevision string `json:"review_target_revision"`
	Outcome              string `json:"outcome"`
	ReasonCode           string `json:"reason_code"`
	EffectRevision       int64  `json:"effect_revision"`
	AppliedAtMS          int64  `json:"applied_at_ms"`
}

func donationReviewActionResponse(row models.DonationReviewAction) DonationReviewActionResponse {
	return DonationReviewActionResponse{ActionID: row.ActionID, BatchID: row.BatchID, ItemID: row.ItemID,
		Kind: row.Kind, ExpectedItemRevision: row.ExpectedRevision, ReviewTargetRevision: row.TargetRevision,
		Outcome: row.Outcome, ReasonCode: row.ReasonCode, EffectRevision: row.EffectRevision,
		AppliedAtMS: row.AppliedAtMS}
}

func validateDonationReviewActionRequest(source string, request DonationReviewActionRequest) error {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(request.ActionID) != nil ||
		!validDonationReviewKind(request.Kind) || request.ExpectedItemRevision < 0 {
		return app_errors.ErrBadRequest
	}
	if !utf8.ValidString(request.Note) || len(request.Note) > donationMaxNoteBytes || !validDonationActor(request.Actor) {
		return app_errors.ErrBadRequest
	}
	switch request.Kind {
	case donationReviewKindApprove, donationReviewKindEnterReview:
		if !validDonationTargetRevision(request.ReviewTargetRevision) {
			return app_errors.ErrBadRequest
		}
	case donationReviewKindReject:
		if request.ReviewTargetRevision != "" || strings.TrimSpace(request.Note) == "" {
			return app_errors.ErrBadRequest
		}
	}
	return nil
}

func validDonationActor(actor string) bool {
	if strings.TrimSpace(actor) == "" || len(actor) > donationMaxActorBytes {
		return false
	}
	for _, character := range actor {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func validDonationTargetRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (s *Service) donationReviewActionDigest(request DonationReviewActionRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		Kind                 string `json:"kind"`
		Actor                string `json:"actor"`
		ExpectedItemRevision int64  `json:"expected_item_revision"`
		ReviewTargetRevision string `json:"review_target_revision"`
		Note                 string `json:"note"`
	}{request.Kind, request.Actor, request.ExpectedItemRevision, request.ReviewTargetRevision, request.Note})
	if err != nil {
		return "", app_errors.ErrInternalServer
	}
	return s.encryption.Hash("gpt-load/donation-review-action/v1\x00" + string(encoded)), nil
}

func (s *Service) GetDonationReviewContext(ctx context.Context, source, batchID, itemID string) (DonationReviewContext, error) {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(batchID) != nil || validateIdempotencyKey(itemID) != nil {
		return DonationReviewContext{}, app_errors.ErrBadRequest
	}
	var result DonationReviewContext
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		item, err := s.loadDonationItem(tx, source, batchID, itemID, false)
		if err != nil {
			return err
		}
		result, err = s.buildDonationReviewContext(ctx, tx, item)
		return err
	})
	return result, err
}

func (s *Service) loadDonationItem(tx *gorm.DB, source, batchID, itemID string, lock bool) (models.DonationItem, error) {
	var batch models.DonationBatch
	if err := tx.Where("source_id = ? AND batch_id = ?", source, batchID).Limit(1).Find(&batch).Error; err != nil {
		return models.DonationItem{}, app_errors.ParseDBError(err)
	}
	if batch.ID == 0 {
		return models.DonationItem{}, app_errors.ErrDonationNotFound
	}
	var item models.DonationItem
	query := tx.Where("source_id = ? AND batch_id = ? AND item_id = ?", source, batchID, itemID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Limit(1).Find(&item).Error; err != nil {
		return models.DonationItem{}, app_errors.ParseDBError(err)
	}
	if item.ID == 0 {
		return models.DonationItem{}, app_errors.ErrDonationItemNotFound
	}
	return item, nil
}

// buildDonationReviewContext projects the current actionable state. A chat model
// list is reported separately from reviewability: an administrator can still
// decide manually when no text model is available.
func (s *Service) buildDonationReviewContext(ctx context.Context, tx *gorm.DB, item models.DonationItem) (DonationReviewContext, error) {
	nowMS := s.donationNowMS()
	mode := normalizeStoredDonationMode(item.EffectiveMode)
	result := DonationReviewContext{BatchID: item.BatchID, ItemID: item.ItemID, GroupID: item.GroupID,
		State: item.State, EffectiveMode: mode, ItemRevision: item.ItemRevision,
		ReviewTargetRevision: item.ReviewTargetRevision, ExpiresAtMS: item.ExpiresAtMS,
		ReviewLimits: defaultDonationReviewLimits(), TestModels: []string{}}
	if mode == donationModeAuto {
		if item.State == "retry_pending" && item.ReasonCode == "retry_exhausted" &&
			item.EncryptedPayload != "" && item.ExpiresAtMS > nowMS {
			target, _, err := s.loadDonationTarget(ctx, tx, item.GroupID)
			if err != nil {
				return result, err
			}
			if target.manualReason != "" {
				result.UnavailableReason = donationReviewReasonTargetUnavailable
				return result, nil
			}
			result.CanReview = true
			result.ReviewTargetRevision = target.manualRevision
			result.ReviewAction = donationReviewKindEnterReview
			return result, nil
		}
		result.UnavailableReason = donationReviewReasonItemState
		return result, nil
	}
	if mode != donationModeManualReview || item.State != "pending_review" {
		result.UnavailableReason = donationReviewReasonItemState
		return result, nil
	}
	if item.ExpiresAtMS <= nowMS || item.EncryptedPayload == "" {
		result.UnavailableReason = donationReviewReasonStagingExpired
		return result, nil
	}
	running, err := s.donationTestRunning(tx, item.SourceID, item.ItemID, nowMS, false)
	if err != nil {
		return result, err
	}
	if running {
		result.UnavailableReason = donationReviewReasonTestRunning
		return result, nil
	}
	result.CanReject = true
	var resource models.DonationResource
	if err := tx.Where("fingerprint = ?", item.Fingerprint).Limit(1).Find(&resource).Error; err != nil {
		return result, app_errors.ParseDBError(err)
	}
	if resource.AcquiredAtMS != nil {
		result.CanReject = false
		result.UnavailableReason = donationReviewReasonItemState
		return result, nil
	}
	if resource.OwnerItemID != nil && *resource.OwnerItemID != item.ID {
		result.UnavailableReason = donationReviewReasonResourceBusy
		return result, nil
	}
	target, _, err := s.loadDonationTarget(ctx, tx, item.GroupID)
	if err != nil {
		return result, err
	}
	if target.manualReason != "" {
		// Approval needs a live target; rejection never does.
		result.UnavailableReason = donationReviewReasonTargetUnavailable
		return result, nil
	}
	if target.manualRevision != item.ReviewTargetRevision {
		result.UnavailableReason = donationReviewReasonTargetChanged
		return result, nil
	}
	result.CanReview = true
	result.ReviewAction = donationReviewKindApprove
	result.CanTest = len(target.chatModels) > 0
	if !result.CanTest {
		result.UnavailableReason = donationReviewReasonModelUnavailable
	}
	result.TestModels = target.chatModels
	return result, nil
}

func (s *Service) donationTestRunning(tx *gorm.DB, source, itemID string, nowMS int64, lock bool) (bool, error) {
	var row models.DonationTestAttempt
	query := tx.Where("source_id = ? AND item_id = ? AND state = ? AND lease_until_ms > ?", source, itemID, "running", nowMS)
	if lock {
		// The item row lock serializes contenders; use a current read after that
		// lock so MySQL REPEATABLE READ cannot hide the previous caller's test.
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	query = query.Limit(1).Find(&row)
	if query.Error != nil {
		return false, app_errors.ParseDBError(query.Error)
	}
	return query.RowsAffected != 0, nil
}

// ApplyDonationReviewAction is the single durable entry point for enter_review,
// approve and reject. The action ID is caller-supplied so a replay is compared
// against the stored command instead of being applied twice.
func (s *Service) ApplyDonationReviewAction(ctx context.Context, source, batchID, itemID string, request DonationReviewActionRequest) (DonationReviewActionResponse, error) {
	if validateIdempotencyKey(batchID) != nil || validateIdempotencyKey(itemID) != nil {
		return DonationReviewActionResponse{}, app_errors.ErrBadRequest
	}
	if err := validateDonationReviewActionRequest(source, request); err != nil {
		return DonationReviewActionResponse{}, err
	}
	digest, err := s.donationReviewActionDigest(request)
	if err != nil {
		return DonationReviewActionResponse{}, err
	}
	var response DonationReviewActionResponse
	approved, committing := false, uint(0)
	s.writeMu.Lock()
	if request.Kind == donationReviewKindApprove {
		// Resolve identity and replays before a barrier that can itself fail.
		// Recovery must run outside the write transaction, before any credential
		// is committed, just like the automatic donation path.
		replayed := false
		err = s.withControlTransaction(ctx, func(tx *gorm.DB) error {
			if _, err := s.ensureDonationIdentity(tx); err != nil {
				return err
			}
			var existing models.DonationReviewAction
			query := tx.Where("source_id = ? AND action_id = ?", source, request.ActionID).Limit(1).Find(&existing)
			if query.Error != nil {
				return app_errors.ParseDBError(query.Error)
			}
			if query.RowsAffected != 0 {
				if existing.RequestDigest != digest || existing.BatchID != batchID || existing.ItemID != itemID {
					return app_errors.ErrIdempotencyKeyReused
				}
				response, replayed = donationReviewActionResponse(existing), true
			}
			return nil
		})
		if err == nil && !replayed {
			err = s.enforceOperationRecoveryBarrierLocked(ctx, 0)
		}
		if err != nil || replayed {
			s.writeMu.Unlock()
			return response, err
		}
	}
	err = s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		if _, err := s.ensureDonationIdentity(tx); err != nil {
			return err
		}
		var existing models.DonationReviewAction
		query := tx.Where("source_id = ? AND action_id = ?", source, request.ActionID).Limit(1).Find(&existing)
		if query.Error != nil {
			return app_errors.ParseDBError(query.Error)
		}
		if query.RowsAffected != 0 {
			if existing.RequestDigest != digest || existing.BatchID != batchID || existing.ItemID != itemID {
				return app_errors.ErrIdempotencyKeyReused
			}
			response = donationReviewActionResponse(existing)
			return nil
		}
		item, err := s.loadDonationItem(tx, source, batchID, itemID, true)
		if err != nil {
			return err
		}
		action := models.DonationReviewAction{SourceID: source, ActionID: request.ActionID, BatchID: batchID,
			ItemID: itemID, Kind: request.Kind, Actor: request.Actor, ExpectedRevision: request.ExpectedItemRevision,
			TargetRevision: request.ReviewTargetRevision, Note: request.Note, RequestDigest: digest,
			CreatedAtMS: s.donationNowMS()}
		outcome, reason, effectRevision, commit, applyErr := s.applyDonationReviewKind(ctx, tx, &item, request, action.CreatedAtMS)
		if applyErr != nil {
			return applyErr
		}
		action.Outcome, action.ReasonCode = outcome, reason
		action.EffectRevision, action.AppliedAtMS = effectRevision, action.CreatedAtMS
		if err := tx.Create(&action).Error; err != nil {
			if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
				return errDonationReviewActionReplay
			}
			return app_errors.ParseDBError(err)
		}
		response = donationReviewActionResponse(action)
		approved, committing = commit, item.ID
		return nil
	})
	s.writeMu.Unlock()
	if errors.Is(err, errDonationReviewActionReplay) {
		var existing models.DonationReviewAction
		if readErr := s.db.WithContext(ctx).Where("source_id = ? AND action_id = ?", source, request.ActionID).Take(&existing).Error; readErr != nil {
			err = app_errors.ParseDBError(readErr)
		} else if existing.RequestDigest != digest || existing.BatchID != batchID || existing.ItemID != itemID {
			err = app_errors.ErrIdempotencyKeyReused
		} else {
			return donationReviewActionResponse(existing), nil
		}
	}
	if err != nil {
		return DonationReviewActionResponse{}, err
	}
	// Runtime publication runs outside the transaction. recoverDonationItem takes
	// its own lock and re-reads the committed row, so a crash here resumes later.
	if approved {
		if recoverErr := s.recoverDonationItem(ctx, committing); recoverErr != nil {
			return response, recoverErr
		}
	}
	return response, nil
}

func (s *Service) applyDonationReviewKind(ctx context.Context, tx *gorm.DB, item *models.DonationItem, request DonationReviewActionRequest, actionAtMS int64) (string, string, int64, bool, error) {
	if item.ItemRevision != request.ExpectedItemRevision {
		return donationReviewOutcomeRejected, donationReviewReasonRevisionMismatch, 0, false, nil
	}
	switch item.State {
	case "rejected", "accepted", "existing":
		// A terminal item can never be re-decided by a late or conflicting action.
		return donationReviewOutcomeRejected, donationReviewReasonAlreadyReviewed, item.ItemRevision, false, nil
	}
	switch request.Kind {
	case donationReviewKindEnterReview:
		return s.applyDonationEnterReview(ctx, tx, item, request.ActionID, request.ReviewTargetRevision)
	case donationReviewKindApprove:
		return s.applyDonationApprove(ctx, tx, item, request.ActionID, request.ReviewTargetRevision, actionAtMS)
	case donationReviewKindReject:
		return s.applyDonationReject(tx, item, request.ActionID, request.Note, actionAtMS)
	default:
		return "", "", 0, false, app_errors.ErrBadRequest
	}
}

func (s *Service) applyDonationEnterReview(ctx context.Context, tx *gorm.DB, item *models.DonationItem, actionID, targetRevision string) (string, string, int64, bool, error) {
	if normalizeStoredDonationMode(item.EffectiveMode) != donationModeAuto {
		return donationReviewOutcomeRejected, donationReviewReasonModeConflict, 0, false, nil
	}
	if item.State != "retry_pending" || item.ReasonCode != "retry_exhausted" || item.EncryptedPayload == "" {
		return donationReviewOutcomeRejected, donationReviewReasonNotReviewable, 0, false, nil
	}
	if item.ExpiresAtMS <= s.donationNowMS() {
		return donationReviewOutcomeRejected, donationReviewReasonStagingExpired, 0, false, nil
	}
	running, err := s.donationTestRunning(tx, item.SourceID, item.ItemID, s.donationNowMS(), true)
	if err != nil {
		return "", "", 0, false, err
	}
	if running {
		return donationReviewOutcomeRejected, donationReviewReasonTestRunning, 0, false, nil
	}
	target, _, err := s.loadDonationTarget(ctx, tx, item.GroupID)
	if err != nil {
		return "", "", 0, false, err
	}
	if target.manualReason != "" {
		return donationReviewOutcomeRejected, donationReviewReasonTargetUnavailable, 0, false, nil
	}
	if target.manualRevision != targetRevision {
		return donationReviewOutcomeRejected, donationReviewReasonTargetChanged, 0, false, nil
	}
	resource, acquired, err := s.donationReviewResource(tx, item)
	if err != nil {
		return "", "", 0, false, err
	}
	if acquired {
		if err := s.finishDonationItem(tx, item, "existing", "already_exists", true); err != nil {
			return "", "", 0, false, err
		}
		return donationReviewOutcomeRejected, donationReviewReasonItemState, item.ItemRevision, false, nil
	}
	if resource.OwnerItemID != nil && *resource.OwnerItemID != item.ID {
		return donationReviewOutcomeRejected, donationReviewReasonResourceBusy, 0, false, nil
	}
	if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ?", item.Fingerprint).
		Updates(map[string]any{"owner_item_id": item.ID, "updated_at_ms": s.donationNowMS()}).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	// The original batch, request summary, attempts and deadline are untouched:
	// only the item's effective mode, state and monotonic revision change.
	revision := item.ItemRevision + 1
	if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", item.ID, "retry_pending").Updates(map[string]any{
		"state": "pending_review", "reason_code": "", "effective_mode": donationModeManualReview,
		"item_revision": revision, "review_target_revision": target.manualRevision,
		"entry_action_id": actionID, "next_attempt_at_ms": 0,
		"lease_token": "", "lease_until_ms": 0, "updated_at_ms": s.donationNowMS(),
	}).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	return donationReviewOutcomeApplied, "", revision, false, nil
}

func (s *Service) applyDonationApprove(ctx context.Context, tx *gorm.DB, item *models.DonationItem, actionID, targetRevision string, actionAtMS int64) (string, string, int64, bool, error) {
	if normalizeStoredDonationMode(item.EffectiveMode) != donationModeManualReview {
		return donationReviewOutcomeRejected, donationReviewReasonModeConflict, 0, false, nil
	}
	if item.State != "pending_review" || item.EncryptedPayload == "" {
		return donationReviewOutcomeRejected, donationReviewReasonItemState, 0, false, nil
	}
	if item.ExpiresAtMS <= s.donationNowMS() {
		return donationReviewOutcomeRejected, donationReviewReasonStagingExpired, 0, false, nil
	}
	running, err := s.donationTestRunning(tx, item.SourceID, item.ItemID, s.donationNowMS(), true)
	if err != nil {
		return "", "", 0, false, err
	}
	if running {
		return donationReviewOutcomeRejected, donationReviewReasonTestRunning, 0, false, nil
	}
	target, _, err := s.loadDonationTarget(ctx, tx, item.GroupID)
	if err != nil {
		return "", "", 0, false, err
	}
	if target.manualReason != "" {
		return donationReviewOutcomeRejected, donationReviewReasonTargetUnavailable, 0, false, nil
	}
	if target.manualRevision != item.ReviewTargetRevision || item.ReviewTargetRevision != targetRevision {
		return donationReviewOutcomeRejected, donationReviewReasonTargetChanged, 0, false, nil
	}
	nowMS := actionAtMS
	resource, err := s.lockDonationResource(tx, item.Fingerprint)
	if err != nil {
		return "", "", 0, false, err
	}
	if resource.AcquiredAtMS != nil {
		// Another item already brought this key into inventory. Accept the result
		// without inventing a new source and without claiming a first acquisition.
		if err := s.finishDonationItem(tx, item, "existing", "already_exists", true); err != nil {
			return "", "", 0, false, err
		}
		if err := s.markDonationReviewDecision(tx, item, actionID, "approved", nowMS); err != nil {
			return "", "", 0, false, err
		}
		return donationReviewOutcomeApplied, "", item.ItemRevision + 1, false, nil
	}
	if resource.OwnerItemID != nil && *resource.OwnerItemID != item.ID {
		return donationReviewOutcomeRejected, donationReviewReasonResourceBusy, 0, false, nil
	}
	var inventory models.Credential
	inventoryQuery := tx.Where("fingerprint = ?", item.CredentialFingerprint).Order("id ASC").Limit(1).Find(&inventory)
	if inventoryQuery.Error != nil {
		return "", "", 0, false, app_errors.ParseDBError(inventoryQuery.Error)
	}
	if inventoryQuery.RowsAffected != 0 {
		if err := s.markDonationInventory(tx, item.Fingerprint, inventory); err != nil {
			return "", "", 0, false, err
		}
		if err := s.finishDonationItem(tx, item, "existing", "already_exists", true); err != nil {
			return "", "", 0, false, err
		}
		if err := s.markDonationReviewDecision(tx, item, actionID, "approved", nowMS); err != nil {
			return "", "", 0, false, err
		}
		return donationReviewOutcomeApplied, "", item.ItemRevision + 1, false, nil
	}
	// Resource or inventory locks may have waited past the original deadline.
	// Recheck immediately before the first permanent acquisition write.
	if item.ExpiresAtMS <= s.donationNowMS() {
		return donationReviewOutcomeRejected, donationReviewReasonStagingExpired, 0, false, nil
	}
	credential := models.Credential{GroupID: item.GroupID, Data: item.EncryptedPayload,
		Fingerprint: item.CredentialFingerprint, IdentityFingerprint: item.CredentialFingerprint,
		SecretVersion: 1, AuthState: models.CredentialAuthStateReady, Status: models.CredentialStatusActive,
		CreatedAtMS: nowMS, UpdatedAtMS: nowMS}
	if err := tx.Create(&credential).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	entries, err := stateloader.BuildGroupCredentialEntriesWithProxy(ctx, tx, item.GroupID, s.encryption)
	if err != nil {
		return "", "", 0, false, app_errors.ErrInternalServer
	}
	if err := state.ValidateCredentialEntries(entries); err != nil {
		return "", "", 0, false, app_errors.ErrInternalServer
	}
	if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ? AND acquired_at_ms IS NULL", item.Fingerprint).
		Updates(map[string]any{"origin": "donation", "credential_id": credential.ID, "owner_item_id": item.ID,
			"group_id": item.GroupID, "acquired_at_ms": nowMS, "updated_at_ms": nowMS}).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	revision := item.ItemRevision + 1
	if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", item.ID, "pending_review").Updates(map[string]any{
		"state": "committing", "reason_code": "runtime_pending", "credential_id": credential.ID,
		"accepted_at_ms": nowMS, "encrypted_payload": "", "lease_token": "", "lease_until_ms": 0,
		"next_attempt_at_ms": nowMS, "review_action_id": actionID, "review_decision": "approved",
		"reviewed_at_ms": nowMS, "item_revision": revision, "updated_at_ms": nowMS,
	}).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	return donationReviewOutcomeApplied, "", revision, true, nil
}

func (s *Service) applyDonationReject(tx *gorm.DB, item *models.DonationItem, actionID, note string, actionAtMS int64) (string, string, int64, bool, error) {
	if normalizeStoredDonationMode(item.EffectiveMode) != donationModeManualReview {
		return donationReviewOutcomeRejected, donationReviewReasonModeConflict, 0, false, nil
	}
	if item.State != "pending_review" {
		return donationReviewOutcomeRejected, donationReviewReasonItemState, 0, false, nil
	}
	if item.ExpiresAtMS <= s.donationNowMS() || item.EncryptedPayload == "" {
		return donationReviewOutcomeRejected, donationReviewReasonStagingExpired, 0, false, nil
	}
	running, err := s.donationTestRunning(tx, item.SourceID, item.ItemID, s.donationNowMS(), true)
	if err != nil {
		return "", "", 0, false, err
	}
	if running {
		return donationReviewOutcomeRejected, donationReviewReasonTestRunning, 0, false, nil
	}
	if strings.TrimSpace(note) == "" {
		return "", "", 0, false, app_errors.ErrBadRequest
	}
	// Rejection never needs a live target, so an offline or changed group is
	// still rejectable and the donor keeps a way out.
	if err := s.releaseDonationOwner(tx, item); err != nil {
		return "", "", 0, false, err
	}
	nowMS := actionAtMS
	revision := item.ItemRevision + 1
	if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", item.ID, "pending_review").Updates(map[string]any{
		"state": "rejected", "reason_code": donationReviewReasonRejected, "encrypted_payload": "",
		"lease_token": "", "lease_until_ms": 0, "next_attempt_at_ms": 0,
		"review_action_id": actionID, "review_decision": "rejected", "reviewed_at_ms": nowMS,
		"item_revision": revision, "updated_at_ms": nowMS,
	}).Error; err != nil {
		return "", "", 0, false, app_errors.ParseDBError(err)
	}
	return donationReviewOutcomeApplied, "", revision, false, nil
}

// markDonationReviewDecision stamps the approving action on an item that ended as
// existing. It never overwrites a terminal state or claims a first acquisition.
func (s *Service) markDonationReviewDecision(tx *gorm.DB, item *models.DonationItem, actionID, decision string, nowMS int64) error {
	if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).Updates(map[string]any{
		"review_action_id": actionID, "review_decision": decision,
		"reviewed_at_ms": nowMS, "item_revision": item.ItemRevision + 1, "updated_at_ms": nowMS,
	}).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	return nil
}

// releaseDonationOwner drops an unacquired ownership claim that still belongs to
// this item. A permanently acquired resource is never released.
func (s *Service) releaseDonationOwner(tx *gorm.DB, item *models.DonationItem) error {
	if item.Fingerprint == "" {
		return nil
	}
	if err := tx.Model(&models.DonationResource{}).
		Where("fingerprint = ? AND owner_item_id = ? AND acquired_at_ms IS NULL", item.Fingerprint, item.ID).
		Updates(map[string]any{"owner_item_id": nil, "updated_at_ms": s.donationNowMS()}).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	return nil
}

// donationReviewResource uses a current read of the shared resource lock. The
// legacy inventory lookup is a snapshot read: normal imports acquire this same
// resource first, and FOR UPDATE on credentials.fingerprint (not a standalone
// index) would lock unrelated inventory and serialize independent tests.
func (s *Service) donationReviewResource(tx *gorm.DB, item *models.DonationItem) (models.DonationResource, bool, error) {
	resource, err := s.lockDonationResource(tx, item.Fingerprint)
	if err != nil || resource.AcquiredAtMS != nil {
		return resource, resource.AcquiredAtMS != nil, err
	}
	var inventory models.Credential
	query := tx.Where("fingerprint = ?", item.CredentialFingerprint).
		Order("id ASC").Limit(1).Find(&inventory)
	if query.Error != nil {
		return resource, false, app_errors.ParseDBError(query.Error)
	}
	if query.RowsAffected != 0 {
		if err := s.markDonationInventory(tx, item.Fingerprint, inventory); err != nil {
			return resource, false, err
		}
		return resource, true, nil
	}
	return resource, false, nil
}

// reconcileDonationReviewItem only updates staging and ownership. It is shared
// by initial manual intake and bounded cold reconciliation and never probes.
func (s *Service) reconcileDonationReviewItem(tx *gorm.DB, item *models.DonationItem) error {
	if item.State != "pending_review" {
		return nil
	}
	if item.ExpiresAtMS <= s.donationNowMS() || item.EncryptedPayload == "" {
		return s.finishDonationItem(tx, item, "invalid", "staging_expired", true)
	}
	resource, acquired, err := s.donationReviewResource(tx, item)
	if err != nil {
		return err
	}
	if acquired {
		return s.finishDonationItem(tx, item, "existing", "already_exists", true)
	}
	reason := ""
	if resource.OwnerItemID != nil && *resource.OwnerItemID != item.ID {
		reason = donationReviewReasonResourceBusy
	} else if resource.OwnerItemID == nil {
		if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ?", item.Fingerprint).
			Updates(map[string]any{"owner_item_id": item.ID, "updated_at_ms": s.donationNowMS()}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
	}
	if item.ReasonCode != reason {
		return s.finishDonationItem(tx, item, "pending_review", reason, false)
	}
	return nil
}

func (s *Service) GetDonationReviewAction(ctx context.Context, source, actionID string) (DonationReviewActionResponse, error) {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(actionID) != nil {
		return DonationReviewActionResponse{}, app_errors.ErrBadRequest
	}
	var result DonationReviewActionResponse
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		var action models.DonationReviewAction
		if err := tx.Where("source_id = ? AND action_id = ?", source, actionID).Take(&action).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return app_errors.ErrDonationReviewNotFound
			}
			return app_errors.ParseDBError(err)
		}
		result = donationReviewActionResponse(action)
		return nil
	})
	return result, err
}
