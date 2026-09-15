package control

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"gpt-load/internal/channel"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage/models"
)

var (
	errDonationBatchReplay = errors.New("donation batch insert collided")
	errDonationRetryReplay = errors.New("donation retry insert collided")
)

const (
	donationModeAuto         = "auto"
	donationModeManualReview = "manual_review"
)

// normalizeDonationMode maps the absent and explicit auto representations onto
// one value so an older caller keeps its exact frozen request comparator.
func normalizeDonationMode(value string) (string, error) {
	switch value {
	case "", donationModeAuto:
		return donationModeAuto, nil
	case donationModeManualReview:
		return donationModeManualReview, nil
	default:
		return "", app_errors.ErrBadRequest
	}
}

type DonationItemRequest struct {
	ItemID string `json:"item_id"`
	Key    string `json:"key"`
}

type DonationBatchRequest struct {
	BatchID string `json:"batch_id"`
	GroupID uint   `json:"group_id"`
	// ValidationMode must stay omitempty: an auto request has to serialize to the
	// same bytes the original contract produced or its HMAC summary would change.
	ValidationMode string                `json:"validation_mode,omitempty"`
	TargetRevision string                `json:"target_revision"`
	Items          []DonationItemRequest `json:"items"`
}

type DonationRetryRequest struct {
	ItemIDs []string `json:"item_ids,omitempty"`
}

type DonationItemResponse struct {
	ItemID       string `json:"item_id"`
	State        string `json:"state"`
	ReasonCode   string `json:"reason_code"`
	CredentialID *uint  `json:"credential_id"`
	AcceptedAtMS *int64 `json:"accepted_at_ms"`
	Retryable    bool   `json:"retryable"`
	// Manual review projection. Legacy auto items keep the zero values so an
	// older consumer observes the same receipt it always did.
	EffectiveMode         string `json:"effective_mode,omitempty"`
	ItemRevision          int64  `json:"item_revision"`
	ReviewTargetRevision  string `json:"review_target_revision,omitempty"`
	ReviewDecision        string `json:"review_decision,omitempty"`
	ReviewActionID        string `json:"review_action_id,omitempty"`
	EntryActionID         string `json:"entry_action_id,omitempty"`
	ReviewedAtMS          int64  `json:"reviewed_at_ms,omitempty"`
	StagingExpiresAtMS    int64  `json:"staging_expires_at_ms"`
	ManualReviewAvailable bool   `json:"manual_review_available,omitempty"`
}

type DonationBatchResponse struct {
	BatchID        string                 `json:"batch_id"`
	GroupID        uint                   `json:"group_id"`
	TargetRevision string                 `json:"target_revision"`
	ValidationMode string                 `json:"validation_mode"`
	CreatedAtMS    int64                  `json:"created_at_ms"`
	State          string                 `json:"state"`
	Items          []DonationItemResponse `json:"items"`
}

// donationBatchDigest keeps the frozen auto comparator byte-identical and gives
// the manual mode its own HMAC domain. The same batch ID submitted under two
// modes therefore collides instead of silently reusing the first result.
func (s *Service) donationBatchDigest(request DonationBatchRequest, mode string) (string, error) {
	// Keep secrets out of serialized comparators, including database errors.
	copy := request
	if mode == donationModeAuto {
		copy.ValidationMode = ""
	}
	copy.Items = make([]DonationItemRequest, len(request.Items))
	for index, item := range request.Items {
		copy.Items[index] = DonationItemRequest{ItemID: item.ItemID,
			Key: s.encryption.Hash("donation-request-key/v1\x00" + strings.TrimSpace(item.Key))}
	}
	encoded, err := json.Marshal(copy)
	if err != nil {
		return "", app_errors.ErrInternalServer
	}
	if mode == donationModeManualReview {
		return s.encryption.Hash("donation-batch-manual/v1\x00" + string(encoded)), nil
	}
	return s.encryption.Hash("donation-batch/v1\x00" + string(encoded)), nil
}

func validateDonationBatch(source string, request DonationBatchRequest) error {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(request.BatchID) != nil || request.GroupID == 0 || len(request.TargetRevision) != 64 {
		return app_errors.ErrBadRequest
	}
	if len(request.Items) == 0 {
		return app_errors.ErrBadRequest
	}
	if len(request.Items) > donationMaxItems {
		return app_errors.ErrRequestTooLarge
	}
	seen := make(map[string]bool, len(request.Items))
	for _, item := range request.Items {
		if validateIdempotencyKey(item.ItemID) != nil || seen[item.ItemID] {
			return app_errors.ErrBadRequest
		}
		seen[item.ItemID] = true
	}
	return nil
}

// ReceiveDonationBatch acknowledges only a committed encrypted staging request.
// Probe work is driven by the application lifetime, never by the request lifetime.
func (s *Service) ReceiveDonationBatch(ctx context.Context, source string, request DonationBatchRequest) (DonationBatchResponse, error) {
	if err := validateDonationBatch(source, request); err != nil {
		return DonationBatchResponse{}, err
	}
	mode, err := normalizeDonationMode(request.ValidationMode)
	if err != nil {
		return DonationBatchResponse{}, err
	}
	digest, err := s.donationBatchDigest(request, mode)
	if err != nil {
		return DonationBatchResponse{}, err
	}
	s.writeMu.Lock()
	err = s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		if _, err := s.ensureDonationIdentity(tx); err != nil {
			return err
		}
		var previous models.DonationBatch
		query := tx.Where("source_id = ? AND batch_id = ?", source, request.BatchID).Limit(1).Find(&previous)
		if query.Error != nil {
			return app_errors.ParseDBError(query.Error)
		}
		if query.RowsAffected != 0 {
			if previous.RequestDigest != digest || normalizeStoredDonationMode(previous.ValidationMode) != mode {
				return app_errors.ErrIdempotencyKeyReused
			}
			return nil
		}
		target, reason, err := s.loadDonationTarget(ctx, tx, request.GroupID)
		if err != nil {
			return err
		}
		if mode == donationModeManualReview {
			if target.manualReason != "" {
				return app_errors.NewAPIErrorWithData(app_errors.ErrDonationTargetUnavailable, map[string]string{"reason_code": target.manualReason})
			}
			if target.manualRevision != request.TargetRevision {
				return app_errors.ErrDonationTargetChanged
			}
		} else {
			if reason != "" {
				return app_errors.NewAPIErrorWithData(app_errors.ErrDonationTargetUnavailable, map[string]string{"reason_code": reason})
			}
			if target.revision != request.TargetRevision {
				return app_errors.ErrDonationTargetChanged
			}
		}
		batch := models.DonationBatch{SourceID: source, BatchID: request.BatchID, RequestDigest: digest,
			GroupID: request.GroupID, GroupName: target.row.Name, ChannelID: target.row.ChannelID,
			TargetRevision: request.TargetRevision, ValidationMode: mode, CreatedAtMS: s.donationNowMS()}
		if err := tx.Create(&batch).Error; err != nil {
			if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
				return errDonationBatchReplay
			}
			return app_errors.ParseDBError(err)
		}
		seen := make(map[string]bool, len(request.Items))
		items := make([]models.DonationItem, 0, len(request.Items))
		fingerprints := make([]string, 0, len(request.Items))
		for index, input := range request.Items {
			item, err := s.stageDonationItem(source, request, input, index, channel.ID(target.row.ChannelID), mode, target.manualRevision)
			if err != nil {
				return err
			}
			if item.Fingerprint != "" && seen[item.Fingerprint] {
				item.State, item.ReasonCode, item.EncryptedPayload = "existing", "duplicate_item", ""
			}
			if item.Fingerprint != "" && !seen[item.Fingerprint] {
				fingerprints = append(fingerprints, item.Fingerprint)
			}
			seen[item.Fingerprint] = true
			items = append(items, item)
		}
		if mode == donationModeManualReview {
			// Reserve pending ownership using the same stable fingerprint order as
			// ordinary imports. A reservation is not an inventory acquisition.
			sort.Strings(fingerprints)
			for _, fingerprint := range fingerprints {
				if _, err := s.lockDonationResource(tx, fingerprint); err != nil {
					return err
				}
			}
		}
		for _, item := range items {
			if err := tx.Create(&item).Error; err != nil {
				if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
					return app_errors.ErrIdempotencyKeyReused
				}
				return app_errors.ParseDBError(err)
			}
			if mode == donationModeManualReview && item.State == "pending_review" {
				if err := s.reconcileDonationReviewItem(tx, &item); err != nil {
					return err
				}
			}
		}
		return nil
	})
	s.writeMu.Unlock()
	// MySQL clientFoundRows makes an ON DUPLICATE KEY no-op look inserted.
	// A plain INSERT and a new read after rollback work on all three drivers;
	// PostgreSQL must leave its failed transaction before reading the winner.
	if errors.Is(err, errDonationBatchReplay) {
		var persisted models.DonationBatch
		if readErr := s.db.WithContext(ctx).Where("source_id = ? AND batch_id = ?", source, request.BatchID).Take(&persisted).Error; readErr != nil {
			err = app_errors.ParseDBError(readErr)
		} else if persisted.RequestDigest != digest || normalizeStoredDonationMode(persisted.ValidationMode) != mode {
			err = app_errors.ErrIdempotencyKeyReused
		} else {
			err = nil
		}
	}
	if err != nil {
		return DonationBatchResponse{}, err
	}
	s.wakeDonationRecovery()
	return s.GetDonationBatch(ctx, source, request.BatchID)
}

// normalizeStoredDonationMode reads the zero value left by an upgraded legacy
// row as auto. Unknown values stay unknown: corrupt manual facts must never be
// silently interpreted as an automatic donation.
func normalizeStoredDonationMode(value string) string {
	if value == "" {
		return donationModeAuto
	}
	return value
}

func (s *Service) stageDonationItem(source string, batch DonationBatchRequest, input DonationItemRequest, position int, channelID channel.ID, mode, manualRevision string) (models.DonationItem, error) {
	nowMS := s.donationNowMS()
	item := models.DonationItem{SourceID: source, ItemID: input.ItemID, BatchID: batch.BatchID,
		Position: position, GroupID: batch.GroupID, State: "invalid", ReasonCode: "invalid_format",
		EffectiveMode: mode, CreatedAtMS: nowMS, UpdatedAtMS: nowMS, NextAttemptAtMS: nowMS,
		ExpiresAtMS: nowMS + donationStagingTTL.Milliseconds()}
	if mode == donationModeManualReview {
		item.ItemRevision = 1
		item.ReviewTargetRevision = manualRevision
	}
	key := strings.TrimSpace(input.Key)
	if key == "" || len(key) > donationMaxKeyBytes || strings.HasPrefix(key, "{") {
		return item, nil
	}
	for _, character := range key {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return item, nil
		}
	}
	normalized, err := s.normalizeCredentials(channelID, key)
	if err != nil || len(normalized.candidates) != 1 {
		return item, nil
	}
	candidate := normalized.candidates[0]
	ciphertext, err := s.encryption.Encrypt(string(candidate.canonical))
	if err != nil {
		return item, app_errors.ErrInternalServer
	}
	item.Fingerprint = s.encryption.Hash(donationFingerprintDomain + key)
	item.CredentialFingerprint = candidate.fingerprint
	item.EncryptedPayload = ciphertext
	if mode == donationModeManualReview {
		// A manual item only needs encrypted staging. It must never enter the
		// automatic probe path, acquire inventory, or claim credit.
		item.State, item.ReasonCode = "pending_review", ""
		return item, nil
	}
	item.State, item.ReasonCode = "queued", ""
	return item, nil
}

func (s *Service) GetDonationBatch(ctx context.Context, source, batchID string) (DonationBatchResponse, error) {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(batchID) != nil {
		return DonationBatchResponse{}, app_errors.ErrBadRequest
	}
	var result DonationBatchResponse
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		var batch models.DonationBatch
		if err := tx.Where("source_id = ? AND batch_id = ?", source, batchID).Take(&batch).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return app_errors.ErrDonationNotFound
			}
			return app_errors.ParseDBError(err)
		}
		var rows []models.DonationItem
		if err := tx.Where("source_id = ? AND batch_id = ?", source, batchID).Order("position ASC").Find(&rows).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		result = DonationBatchResponse{BatchID: batch.BatchID, GroupID: batch.GroupID,
			TargetRevision: batch.TargetRevision, ValidationMode: normalizeStoredDonationMode(batch.ValidationMode),
			CreatedAtMS: batch.CreatedAtMS, State: "completed", Items: make([]DonationItemResponse, 0, len(rows))}
		for _, row := range rows {
			item := DonationItemResponse{ItemID: row.ItemID, State: row.State, ReasonCode: row.ReasonCode,
				Retryable:     row.State == "retry_pending" && row.EncryptedPayload != "" && row.ExpiresAtMS > s.donationNowMS(),
				EffectiveMode: normalizeStoredDonationMode(row.EffectiveMode), ItemRevision: row.ItemRevision,
				ReviewTargetRevision: row.ReviewTargetRevision, ReviewDecision: row.ReviewDecision,
				ReviewActionID: row.ReviewActionID, ReviewedAtMS: row.ReviewedAtMS,
				EntryActionID:      row.EntryActionID,
				StagingExpiresAtMS: row.ExpiresAtMS}
			if row.State == "accepted" {
				item.CredentialID, item.AcceptedAtMS = row.CredentialID, row.AcceptedAtMS
			}
			// A manual item stays unfinished until an administrator decides, so a
			// caller never reports a pending review as an already-completed batch.
			switch row.State {
			case "queued", "validating", "committing", "retry_pending", "pending_review":
				result.State = "processing"
			}
			result.Items = append(result.Items, item)
		}
		return nil
	})
	return result, err
}

func (s *Service) RetryDonationBatch(ctx context.Context, source, batchID, actionID string, request DonationRetryRequest) (DonationBatchResponse, error) {
	if validateIdempotencyKey(source) != nil || validateIdempotencyKey(batchID) != nil || validateIdempotencyKey(actionID) != nil || len(request.ItemIDs) > donationMaxItems {
		return DonationBatchResponse{}, app_errors.ErrBadRequest
	}
	ids := append([]string(nil), request.ItemIDs...)
	sort.Strings(ids)
	for index, id := range ids {
		if validateIdempotencyKey(id) != nil || (index > 0 && ids[index-1] == id) {
			return DonationBatchResponse{}, app_errors.ErrBadRequest
		}
	}
	digest := s.encryption.Hash("donation-retry/v1\x00" + batchID + "\x00" + strings.Join(ids, "\x00"))
	s.writeMu.Lock()
	err := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		var batch models.DonationBatch
		if err := tx.Where("source_id = ? AND batch_id = ?", source, batchID).Take(&batch).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return app_errors.ErrDonationNotFound
			}
			return app_errors.ParseDBError(err)
		}
		action := models.DonationRetry{SourceID: source, BatchID: batchID, IdempotencyKey: actionID,
			RequestDigest: digest, CreatedAtMS: s.donationNowMS()}
		if err := tx.Create(&action).Error; err != nil {
			if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
				return errDonationRetryReplay
			}
			return app_errors.ParseDBError(err)
		}
		query := tx.Model(&models.DonationItem{}).Where("source_id = ? AND batch_id = ?", source, batchID)
		if len(ids) > 0 {
			query = query.Where("item_id IN ?", ids)
			var count int64
			if err := query.Session(&gorm.Session{}).Count(&count).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			if count != int64(len(ids)) {
				return app_errors.ErrBadRequest
			}
		}
		if err := query.Where("state = ? AND effective_mode IN ? AND encrypted_payload <> ? AND expires_at_ms > ?", "retry_pending", []string{"", donationModeAuto}, "", s.donationNowMS()).
			Updates(map[string]any{"state": "queued", "reason_code": "", "attempts": 0,
				"lease_token": "", "lease_until_ms": 0, "next_attempt_at_ms": s.donationNowMS(), "updated_at_ms": s.donationNowMS()}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		return nil
	})
	s.writeMu.Unlock()
	if errors.Is(err, errDonationRetryReplay) {
		var persisted models.DonationRetry
		if readErr := s.db.WithContext(ctx).Where("source_id = ? AND idempotency_key = ?", source, actionID).Take(&persisted).Error; readErr != nil {
			err = app_errors.ParseDBError(readErr)
		} else if persisted.RequestDigest != digest {
			err = app_errors.ErrIdempotencyKeyReused
		} else {
			err = nil
		}
	}
	if err != nil {
		return DonationBatchResponse{}, err
	}
	s.wakeDonationRecovery()
	return s.GetDonationBatch(ctx, source, batchID)
}
