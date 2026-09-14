package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gpt-load/internal/channel"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
	stateloader "gpt-load/internal/state/loader"
	"gpt-load/internal/storage/models"
)

const (
	donationMaxItems          = 100
	donationMaxKeyBytes       = 4096
	donationMaxBodyBytes      = 1 << 20
	donationMaxAttempts       = 5
	donationStagingTTL        = 7 * 24 * time.Hour
	donationProbeTimeout      = 30 * time.Second
	donationLeaseDuration     = 90 * time.Second
	donationFingerprintDomain = "gpt-load/donation-resource/v1\x00"
	donationKeyProofDomain    = "gpt-load/donation-fingerprint-key/v1"
)

type DonationLimits struct {
	MaxItems                int   `json:"max_items"`
	MaxKeyBytes             int   `json:"max_key_bytes"`
	MaxBodyBytes            int   `json:"max_body_bytes"`
	StagingRetentionSeconds int64 `json:"staging_retention_seconds"`
}

type DonationCapabilities struct {
	ProtocolVersion string         `json:"protocol_version"`
	InstanceID      string         `json:"instance_id"`
	SourceID        string         `json:"source_id"`
	InputTypes      []string       `json:"input_types"`
	Limits          DonationLimits `json:"limits"`
}

type DonationGroup struct {
	ID                uint                  `json:"id"`
	Name              string                `json:"name"`
	ChannelID         channel.ID            `json:"channel_id"`
	ConnectionType    models.ConnectionType `json:"connection_type"`
	Enabled           bool                  `json:"enabled"`
	CanProbe          bool                  `json:"can_probe"`
	TargetRevision    string                `json:"target_revision"`
	UnavailableReason string                `json:"unavailable_reason"`
}

type donationTarget struct {
	row      models.Group
	group    state.GroupView
	probe    groupValidationTarget
	revision string
}

func (s *Service) donationNowMS() int64 { return max(s.now().UnixMilli(), 1) }

func (s *Service) ensureDonationIdentity(tx *gorm.DB) (models.DonationIdentity, error) {
	if s.encryption == nil {
		return models.DonationIdentity{}, app_errors.ErrInternalServer
	}
	var identity models.DonationIdentity
	query := tx.Where("id = ?", 1).Limit(1).Find(&identity)
	if query.Error != nil {
		return identity, app_errors.ParseDBError(query.Error)
	}
	if query.RowsAffected == 0 {
		instance, err := newOperationID(rand.Reader)
		if err != nil {
			return identity, app_errors.ErrInternalServer
		}
		source, err := newOperationID(rand.Reader)
		if err != nil {
			return identity, app_errors.ErrInternalServer
		}
		identity = models.DonationIdentity{ID: 1, InstanceID: instance, SourceID: source,
			KeyProof: s.encryption.Hash(donationKeyProofDomain), CreatedAtMS: s.donationNowMS()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; err != nil {
			return identity, app_errors.ParseDBError(err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).Take(&identity).Error; err != nil {
			return identity, app_errors.ParseDBError(err)
		}
	}
	if identity.KeyProof == "" || identity.KeyProof != s.encryption.Hash(donationKeyProofDomain) ||
		validateIdempotencyKey(identity.InstanceID) != nil || validateIdempotencyKey(identity.SourceID) != nil {
		return identity, app_errors.ErrDonationUnavailable
	}
	return identity, nil
}

func (s *Service) DonationCapabilities(ctx context.Context) (DonationCapabilities, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var identity models.DonationIdentity
	err := s.withControlTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		identity, err = s.ensureDonationIdentity(tx)
		return err
	})
	if err != nil {
		return DonationCapabilities{}, err
	}
	return DonationCapabilities{ProtocolVersion: "1", InstanceID: identity.InstanceID, SourceID: identity.SourceID,
		InputTypes: []string{"api_key"}, Limits: DonationLimits{MaxItems: donationMaxItems,
			MaxKeyBytes: donationMaxKeyBytes, MaxBodyBytes: donationMaxBodyBytes,
			StagingRetentionSeconds: int64(donationStagingTTL / time.Second)}}, nil
}

func (s *Service) ListDonationGroups(ctx context.Context) ([]DonationGroup, error) {
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	result := make([]DonationGroup, 0)
	err := s.withReadSnapshot(ctx, func(tx *gorm.DB) error {
		var rows []models.Group
		if err := tx.Order("id ASC").Find(&rows).Error; err != nil {
			return app_errors.ParseDBError(err)
		}
		for _, row := range rows {
			item := DonationGroup{ID: row.ID, Name: row.Name, ChannelID: channel.ID(row.ChannelID),
				ConnectionType: normalizeGroupConnectionType(row.ConnectionType), Enabled: row.Enabled}
			target, reason, err := s.buildDonationTarget(ctx, tx, row)
			if err != nil {
				return err
			}
			item.CanProbe, item.UnavailableReason = reason == "", reason
			item.TargetRevision = target.revision
			result = append(result, item)
		}
		return nil
	})
	return result, err
}

func (s *Service) buildDonationTarget(ctx context.Context, tx *gorm.DB, row models.Group) (donationTarget, string, error) {
	if !row.Enabled {
		return donationTarget{}, "group_disabled", nil
	}
	if normalizeGroupConnectionType(row.ConnectionType) != models.ConnectionTypeAPIKey || !s.donationChannelSupportsPlainKey(channel.ID(row.ChannelID)) {
		return donationTarget{}, "unsupported_input", nil
	}
	group, err := mapGroupRowToState(row)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrInternalServer
	}
	group.Proxy, err = decryptProxyOverride(s.encryption, row.ProxyConfig)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrInternalServer
	}
	settings, proxy, err := stateloader.LoadSystemSettingsAndProxy(ctx, tx, s.encryption)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrDatabase
	}
	snapshot, err := state.Compile(state.CompileInput{SystemSettings: settings, GlobalProxy: proxy,
		EnvironmentProxy: s.environmentProxy, ChannelRegistry: s.channelRegistry, Groups: []state.GroupConfig{group}})
	if err != nil {
		return donationTarget{}, "target_unavailable", nil
	}
	view, exists := snapshot.Groups[row.ID]
	if !exists {
		return donationTarget{}, "target_unavailable", nil
	}
	probe, valid := buildGroupValidationTarget(view)
	if !valid {
		return donationTarget{}, "probe_unavailable", nil
	}
	// A configured static authentication override could test a different key.
	// Such groups must be repaired by the operator before they can accept donations.
	for name, value := range view.HeaderRules.Set {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "x-api-key", "x-goog-api-key", "api-key":
			if !strings.Contains(value, "${API_KEY}") {
				return donationTarget{}, "credential_override", nil
			}
		}
	}
	for _, name := range view.HeaderRules.Remove {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "x-api-key", "x-goog-api-key", "api-key":
			return donationTarget{}, "credential_override", nil
		}
	}
	timeouts, err := json.Marshal(view.Timeouts)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrInternalServer
	}
	revision := s.encryption.Hash("gpt-load/donation-target/v1\x00" + hex.EncodeToString(probe.signature[:]) + string(timeouts))
	return donationTarget{row: row, group: view, probe: probe, revision: revision}, "", nil
}

func (s *Service) donationChannelSupportsPlainKey(id channel.ID) bool {
	descriptor, exists := s.channelRegistry.Get(id)
	if !exists || len(descriptor.CredentialFields) != 1 {
		return false
	}
	return descriptor.CredentialFields[0].Key == "api_key"
}

func (s *Service) loadDonationTarget(ctx context.Context, tx *gorm.DB, groupID uint) (donationTarget, string, error) {
	var row models.Group
	if err := tx.Where("id = ?", groupID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return donationTarget{}, "group_deleted", nil
		}
		return donationTarget{}, "", app_errors.ParseDBError(err)
	}
	return s.buildDonationTarget(ctx, tx, row)
}
