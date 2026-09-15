package control

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
	stateloader "gpt-load/internal/state/loader"
	"gpt-load/internal/storage/models"
)

type donationWork struct {
	item   models.DonationItem
	target donationTarget
}

// DrainDonationWork makes bounded progress independently for each durable item.
// Database leases fence old workers across restarts and multiple processes.
func (s *Service) DrainDonationWork(ctx context.Context) error {
	var rows []models.DonationItem
	nowMS := s.donationNowMS()
	err := s.db.WithContext(ctx).Where(
		"(state = ? AND next_attempt_at_ms <= ?) OR (state IN ? AND expires_at_ms <= ?) OR "+
			"(state IN ? AND attempts < ? AND next_attempt_at_ms <= ?) OR (state = ? AND lease_until_ms <= ?)",
		"committing", nowMS, []string{"queued", "retry_pending"}, nowMS,
		[]string{"queued", "retry_pending"}, donationMaxAttempts, nowMS, "validating", nowMS).
		Order("next_attempt_at_ms ASC, id ASC").Limit(32).Find(&rows).Error
	if err != nil {
		return app_errors.ParseDBError(err)
	}
	var failures []error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.processDonationItem(ctx, row.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Service) processDonationItem(ctx context.Context, id uint) error {
	work, err := s.claimDonationItem(ctx, id)
	if err != nil || work == nil {
		return err
	}
	if work.item.State == "committing" {
		return s.recoverDonationItem(ctx, id)
	}
	probeID, validID := donationVirtualCredentialID(work.item.ID, false)
	if !validID {
		return app_errors.ErrInternalServer
	}
	if err := donationVirtualCredentialRangeAvailable(s.db.WithContext(ctx)); err != nil {
		return err
	}
	defer s.retireCredentialRuntime(probeID)
	probeCtx, cancel := context.WithTimeout(ctx, donationProbeTimeout)
	probe := newCredentialProbeExecutor(s.encryption, s.channelRegistry, s.executor)
	executed, probeErr := probe.Probe(probeCtx, work.target.group, work.target.probe, state.CredentialRef{
		ID: probeID, GroupID: work.item.GroupID, Version: 1, IdentityGeneration: 1,
		Fingerprint: work.item.CredentialFingerprint, EncryptedValue: work.item.EncryptedPayload,
	})
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	outcome, reason := classifyCredentialProbeResult(executed.result)
	reasonCode := "upstream_error"
	if reason != nil {
		reasonCode = string(*reason)
	}
	if probeErr != nil {
		outcome = CredentialProbeOutcomeInconclusive
		if errors.Is(probeErr, context.DeadlineExceeded) {
			reasonCode = "timeout"
		}
	}
	return s.completeDonationProbe(ctx, *work, outcome, reasonCode)
}

func (s *Service) claimDonationItem(ctx context.Context, id uint) (*donationWork, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var work *donationWork
	err := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		var item models.DonationItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&item).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		nowMS := s.donationNowMS()
		if item.State == "committing" {
			if item.NextAttemptAtMS <= nowMS {
				work = &donationWork{item: item}
			}
			return nil
		}
		if item.State != "queued" && item.State != "retry_pending" && item.State != "validating" {
			return nil
		}
		if normalizeStoredDonationMode(item.EffectiveMode) != donationModeAuto {
			return nil
		}
		if item.State == "validating" && item.LeaseUntilMS > nowMS {
			return nil
		}
		if item.NextAttemptAtMS > nowMS {
			return nil
		}
		resource, err := s.lockDonationResource(tx, item.Fingerprint)
		if err != nil {
			return err
		}
		if resource.AcquiredAtMS != nil {
			return s.finishDonationItem(tx, &item, "existing", "already_exists", true)
		}
		var inventory models.Credential
		inventoryQuery := tx.Where("fingerprint = ?", item.CredentialFingerprint).Order("id ASC").Limit(1).Find(&inventory)
		if inventoryQuery.Error != nil {
			return app_errors.ParseDBError(inventoryQuery.Error)
		}
		if inventoryQuery.RowsAffected != 0 {
			if err := s.markDonationInventory(tx, item.Fingerprint, inventory); err != nil {
				return err
			}
			return s.finishDonationItem(tx, &item, "existing", "already_exists", true)
		}
		if item.ExpiresAtMS <= nowMS || item.EncryptedPayload == "" {
			return s.finishDonationItem(tx, &item, "invalid", "staging_expired", true)
		}
		if item.Attempts >= donationMaxAttempts {
			return s.finishDonationItem(tx, &item, "retry_pending", "retry_exhausted", true)
		}
		if resource.OwnerItemID != nil && *resource.OwnerItemID != item.ID {
			item.NextAttemptAtMS = nowMS + time.Second.Milliseconds()
			return s.finishDonationItem(tx, &item, "retry_pending", "resource_busy", false)
		}
		target, reason, err := s.loadDonationTarget(ctx, tx, item.GroupID)
		if err != nil {
			return err
		}
		item.Attempts++
		if reason != "" {
			return s.deferDonationItem(tx, &item, reason)
		}
		token, err := newOperationID(rand.Reader)
		if err != nil {
			return app_errors.ErrInternalServer
		}
		item.LeaseToken, item.LeaseUntilMS = token, nowMS+donationLeaseDuration.Milliseconds()
		item.State, item.ReasonCode, item.ValidationRevision = "validating", "", target.revision
		if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ? AND acquired_at_ms IS NULL", item.Fingerprint).
			Updates(map[string]any{"owner_item_id": item.ID, "updated_at_ms": nowMS}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).Updates(map[string]any{
			"state": item.State, "reason_code": "", "attempts": item.Attempts,
			"lease_token": item.LeaseToken, "lease_until_ms": item.LeaseUntilMS,
			"validation_revision": item.ValidationRevision, "updated_at_ms": nowMS,
		}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		work = &donationWork{item: item, target: target}
		return nil
	})
	return work, err
}

func (s *Service) finishDonationItem(tx *gorm.DB, item *models.DonationItem, stateName, reason string, release bool) error {
	if stateName == "invalid" || stateName == "existing" {
		item.EncryptedPayload = ""
	}
	if release {
		if err := tx.Model(&models.DonationResource{}).
			Where("fingerprint = ? AND owner_item_id = ? AND acquired_at_ms IS NULL", item.Fingerprint, item.ID).
			Updates(map[string]any{"owner_item_id": nil, "updated_at_ms": s.donationNowMS()}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
	}
	updates := map[string]any{
		"state": stateName, "reason_code": reason, "encrypted_payload": item.EncryptedPayload,
		"lease_token": "", "lease_until_ms": 0, "attempts": item.Attempts,
		"next_attempt_at_ms": item.NextAttemptAtMS, "updated_at_ms": s.donationNowMS(),
	}
	if item.ItemRevision > 0 && (item.State != stateName || item.ReasonCode != reason) {
		item.ItemRevision++
		updates["item_revision"] = item.ItemRevision
	}
	if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).Updates(updates).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	item.State, item.ReasonCode = stateName, reason
	return nil
}

func (s *Service) deferDonationItem(tx *gorm.DB, item *models.DonationItem, reason string) error {
	backoff := time.Second << min(max(item.Attempts-1, 0), 4)
	item.NextAttemptAtMS = s.donationNowMS() + backoff.Milliseconds()
	release := item.Attempts >= donationMaxAttempts
	if release {
		reason = "retry_exhausted"
	}
	return s.finishDonationItem(tx, item, "retry_pending", reason, release)
}

func (s *Service) completeDonationProbe(ctx context.Context, work donationWork, outcome CredentialProbeOutcome, reason string) error {
	s.writeMu.Lock()
	if err := s.enforceOperationRecoveryBarrierLocked(ctx, 0); err != nil {
		s.writeMu.Unlock()
		return err
	}
	committed := false
	err := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		var item models.DonationItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", work.item.ID).Take(&item).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		// Lease expiry alone is not proof of failure. Only the current generation
		// can persist this probe's result; a replacement worker fences old replies.
		if item.State != "validating" || item.LeaseToken != work.item.LeaseToken {
			return nil
		}
		resource, err := s.lockDonationResource(tx, item.Fingerprint)
		if err != nil {
			return err
		}
		if resource.AcquiredAtMS != nil {
			return s.finishDonationItem(tx, &item, "existing", "already_exists", true)
		}
		if resource.OwnerItemID == nil || *resource.OwnerItemID != item.ID {
			return s.deferDonationItem(tx, &item, "resource_busy")
		}
		if outcome == CredentialProbeOutcomeFailed {
			return s.finishDonationItem(tx, &item, "invalid", reason, true)
		}
		if outcome != CredentialProbeOutcomePassed {
			return s.deferDonationItem(tx, &item, reason)
		}
		target, targetReason, err := s.loadDonationTarget(ctx, tx, item.GroupID)
		if err != nil {
			return err
		}
		if targetReason != "" {
			return s.deferDonationItem(tx, &item, targetReason)
		}
		if target.revision != work.target.revision {
			return s.deferDonationItem(tx, &item, "target_changed")
		}
		// Also recognize pre-feature inventory in tests, upgrades, or import paths
		// that have not yet run startup fingerprint backfill.
		var existing models.Credential
		query := tx.Where("fingerprint = ?", item.CredentialFingerprint).Order("id ASC").Limit(1).Find(&existing)
		if query.Error != nil {
			return app_errors.ParseDBError(query.Error)
		}
		if query.RowsAffected != 0 {
			if err := s.markDonationInventory(tx, item.Fingerprint, existing); err != nil {
				return err
			}
			return s.finishDonationItem(tx, &item, "existing", "already_exists", true)
		}
		nowMS := s.donationNowMS()
		credential := models.Credential{GroupID: item.GroupID, Data: item.EncryptedPayload,
			Fingerprint: item.CredentialFingerprint, IdentityFingerprint: item.CredentialFingerprint,
			SecretVersion: 1, AuthState: models.CredentialAuthStateReady, Status: models.CredentialStatusActive,
			CreatedAtMS: nowMS, UpdatedAtMS: nowMS}
		if err := tx.Create(&credential).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		entries, err := stateloader.BuildGroupCredentialEntriesWithProxy(ctx, tx, item.GroupID, s.encryption)
		if err != nil {
			return app_errors.ErrInternalServer
		}
		if err := state.ValidateCredentialEntries(entries); err != nil {
			return app_errors.ErrInternalServer
		}
		if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ? AND owner_item_id = ? AND acquired_at_ms IS NULL", item.Fingerprint, item.ID).
			Updates(map[string]any{"origin": "donation", "credential_id": credential.ID,
				"group_id": item.GroupID, "acquired_at_ms": nowMS, "updated_at_ms": nowMS}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		if err := tx.Model(&models.DonationItem{}).Where("id = ?", item.ID).Updates(map[string]any{
			"state": "committing", "reason_code": "runtime_pending", "credential_id": credential.ID,
			"accepted_at_ms": nowMS, "encrypted_payload": "", "lease_token": "", "lease_until_ms": 0,
			"next_attempt_at_ms": nowMS, "updated_at_ms": nowMS,
		}).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		committed = true
		return nil
	})
	s.writeMu.Unlock()
	if err != nil {
		return err
	}
	if committed {
		return s.recoverDonationItem(ctx, work.item.ID)
	}
	return nil
}

// A committing receipt survives response loss or a crash after the credential
// transaction. Recovery reloads committed runtime state without probing again.
func (s *Service) recoverDonationItem(ctx context.Context, id uint) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var item models.DonationItem
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	if item.State != "committing" {
		return nil
	}
	if item.CredentialID == nil || item.AcceptedAtMS == nil {
		return app_errors.ErrInternalServer
	}
	if err := s.enforceOperationRecoveryBarrierLocked(ctx, 0); err != nil {
		return err
	}
	if err := s.recoverCommittedRuntime(ctx, true); err != nil {
		updateErr := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
			if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", id, "committing").Updates(map[string]any{
				"next_attempt_at_ms": s.donationNowMS() + time.Second.Milliseconds(), "updated_at_ms": s.donationNowMS(),
			}).Error; err != nil {
				return app_errors.ParseDBError(err)
			}
			return nil
		})
		return errors.Join(app_errors.ErrControlOperationIncomplete, updateErr)
	}
	return s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		var resource models.DonationResource
		if err := tx.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		if resource.Origin != "donation" || resource.OwnerItemID == nil || *resource.OwnerItemID != item.ID ||
			resource.CredentialID == nil || *resource.CredentialID != *item.CredentialID || resource.AcquiredAtMS == nil || *resource.AcquiredAtMS != *item.AcceptedAtMS {
			return app_errors.ErrInternalServer
		}
		updates := map[string]any{
			"state": "accepted", "reason_code": "", "next_attempt_at_ms": 0, "updated_at_ms": s.donationNowMS(),
		}
		if item.ItemRevision > 0 || normalizeStoredDonationMode(item.EffectiveMode) == donationModeManualReview {
			updates["item_revision"] = gorm.Expr("item_revision + 1")
		}
		if err := tx.Model(&models.DonationItem{}).Where("id = ? AND state = ?", id, "committing").Updates(updates).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		return nil
	})
}

func (s *Service) RunDonationRecovery(ctx context.Context) {
	if s == nil || !s.donationsEnabled {
		return
	}
	reviewTicker := time.NewTicker(time.Minute)
	defer reviewTicker.Stop()
	s.runDonationRecovery(ctx, reviewTicker.C)
}

func (s *Service) runDonationRecovery(ctx context.Context, reviewTicks <-chan time.Time) {
	// A hot batch can contain many slow automatic probes. Keep the bounded,
	// database-only review reconciliation on its own lifetime so those network
	// calls cannot delay expiry or abandoned test cleanup for hours.
	reviewCtx, stopReview := context.WithCancel(ctx)
	reviewDone := make(chan struct{})
	go func() {
		defer close(reviewDone)
		for {
			if err := s.ReconcileDonationReviews(reviewCtx); err != nil && reviewCtx.Err() == nil {
				logServiceError("donation_review_recovery", err, app_errors.ErrInternalServer.Code)
			}
			select {
			case <-reviewCtx.Done():
				return
			case <-reviewTicks:
			}
		}
	}()
	defer func() { stopReview(); <-reviewDone }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.DrainDonationWork(ctx); err != nil && ctx.Err() == nil {
			logServiceError("donation_recovery", err, app_errors.ErrInternalServer.Code)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.donationRecoveryWake:
		}
	}
}

func (s *Service) wakeDonationRecovery() {
	select {
	case s.donationRecoveryWake <- struct{}{}:
	default:
	}
}
