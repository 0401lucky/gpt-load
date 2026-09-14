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

type DonationItemRequest struct {
	ItemID string `json:"item_id"`
	Key    string `json:"key"`
}

type DonationBatchRequest struct {
	BatchID        string                `json:"batch_id"`
	GroupID        uint                  `json:"group_id"`
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
}

type DonationBatchResponse struct {
	BatchID        string                 `json:"batch_id"`
	GroupID        uint                   `json:"group_id"`
	TargetRevision string                 `json:"target_revision"`
	CreatedAtMS    int64                  `json:"created_at_ms"`
	State          string                 `json:"state"`
	Items          []DonationItemResponse `json:"items"`
}

func (s *Service) donationBatchDigest(request DonationBatchRequest) (string, error) {
	// Keep secrets out of serialized comparators, including database errors.
	copy := request
	copy.Items = make([]DonationItemRequest, len(request.Items))
	for index, item := range request.Items {
		copy.Items[index] = DonationItemRequest{ItemID: item.ItemID,
			Key: s.encryption.Hash("donation-request-key/v1\x00" + strings.TrimSpace(item.Key))}
	}
	encoded, err := json.Marshal(copy)
	if err != nil {
		return "", app_errors.ErrInternalServer
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
	digest, err := s.donationBatchDigest(request)
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
			if previous.RequestDigest != digest {
				return app_errors.ErrIdempotencyKeyReused
			}
			return nil
		}
		target, reason, err := s.loadDonationTarget(ctx, tx, request.GroupID)
		if err != nil {
			return err
		}
		if reason != "" {
			return app_errors.NewAPIErrorWithData(app_errors.ErrDonationTargetUnavailable, map[string]string{"reason_code": reason})
		}
		if target.revision != request.TargetRevision {
			return app_errors.ErrDonationTargetChanged
		}
		batch := models.DonationBatch{SourceID: source, BatchID: request.BatchID, RequestDigest: digest,
			GroupID: request.GroupID, GroupName: target.row.Name, ChannelID: target.row.ChannelID,
			TargetRevision: request.TargetRevision, CreatedAtMS: s.donationNowMS()}
		if err := tx.Create(&batch).Error; err != nil {
			if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
				return errDonationBatchReplay
			}
			return app_errors.ParseDBError(err)
		}
		seen := make(map[string]bool, len(request.Items))
		for index, input := range request.Items {
			item, err := s.stageDonationItem(source, request, input, index, channel.ID(target.row.ChannelID))
			if err != nil {
				return err
			}
			if item.Fingerprint != "" && seen[item.Fingerprint] {
				item.State, item.ReasonCode, item.EncryptedPayload = "existing", "duplicate_item", ""
			}
			seen[item.Fingerprint] = true
			if err := tx.Create(&item).Error; err != nil {
				if app_errors.ParseDBError(err) == app_errors.ErrDuplicateResource {
					return app_errors.ErrIdempotencyKeyReused
				}
				return app_errors.ParseDBError(err)
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
	return s.GetDonationBatch(ctx, source, request.BatchID)
}

func (s *Service) stageDonationItem(source string, batch DonationBatchRequest, input DonationItemRequest, position int, channelID channel.ID) (models.DonationItem, error) {
	nowMS := s.donationNowMS()
	item := models.DonationItem{SourceID: source, ItemID: input.ItemID, BatchID: batch.BatchID,
		Position: position, GroupID: batch.GroupID, State: "invalid", ReasonCode: "invalid_format",
		CreatedAtMS: nowMS, UpdatedAtMS: nowMS, NextAttemptAtMS: nowMS, ExpiresAtMS: nowMS + donationStagingTTL.Milliseconds()}
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
			TargetRevision: batch.TargetRevision, CreatedAtMS: batch.CreatedAtMS, State: "completed", Items: make([]DonationItemResponse, 0, len(rows))}
		for _, row := range rows {
			item := DonationItemResponse{ItemID: row.ItemID, State: row.State, ReasonCode: row.ReasonCode,
				Retryable: row.State == "retry_pending" && row.EncryptedPayload != "" && row.ExpiresAtMS > s.donationNowMS()}
			if row.State == "accepted" {
				item.CredentialID, item.AcceptedAtMS = row.CredentialID, row.AcceptedAtMS
			}
			if row.State == "queued" || row.State == "validating" || row.State == "committing" || row.State == "retry_pending" {
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
		if err := query.Where("state = ? AND encrypted_payload <> ? AND expires_at_ms > ?", "retry_pending", "", s.donationNowMS()).
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
