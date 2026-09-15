package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/protocol"
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

	// donationFeatureManualReview is the negotiated capability marker. A caller
	// that does not see it must not offer or accept the manual review mode.
	donationFeatureManualReview = "manual_review_v1"

	donationMaxPromptBytes       = 16 << 10
	donationMaxTestRequestBytes  = 64 << 10
	donationDefaultOutputTokens  = 1024
	donationMaxOutputTokens      = 4096
	donationMaxResponseBytes     = 128 << 10
	donationMaxEventBytes        = 64 << 10
	donationMaxNoteBytes         = 2048
	donationTestTotalTimeout     = 120 * time.Second
	donationTestFirstByteTimeout = 30 * time.Second
	donationTestIdleTimeout      = 20 * time.Second
	donationTestLeaseDuration    = 150 * time.Second
	donationMaxParallelTests     = 4
)

type DonationLimits struct {
	MaxItems                int   `json:"max_items"`
	MaxKeyBytes             int   `json:"max_key_bytes"`
	MaxBodyBytes            int   `json:"max_body_bytes"`
	StagingRetentionSeconds int64 `json:"staging_retention_seconds"`
}

// DonationReviewLimits is returned independently of Limits so an older caller
// that only understands the original contract keeps its exact expectations.
type DonationReviewLimits struct {
	MaxPromptBytes          int   `json:"max_prompt_bytes"`
	MaxRequestBytes         int   `json:"max_request_bytes"`
	DefaultOutputTokens     int   `json:"default_output_tokens"`
	MaxOutputTokens         int   `json:"max_output_tokens"`
	MaxResponseBytes        int   `json:"max_response_bytes"`
	MaxEventBytes           int   `json:"max_event_bytes"`
	TotalTimeoutSeconds     int64 `json:"total_timeout_seconds"`
	FirstByteTimeoutSeconds int64 `json:"first_byte_timeout_seconds"`
	IdleTimeoutSeconds      int64 `json:"idle_timeout_seconds"`
	MaxNoteBytes            int   `json:"max_note_bytes"`
}

func defaultDonationReviewLimits() DonationReviewLimits {
	return DonationReviewLimits{
		MaxPromptBytes: donationMaxPromptBytes, MaxRequestBytes: donationMaxTestRequestBytes,
		DefaultOutputTokens: donationDefaultOutputTokens, MaxOutputTokens: donationMaxOutputTokens,
		MaxResponseBytes: donationMaxResponseBytes, MaxEventBytes: donationMaxEventBytes,
		TotalTimeoutSeconds:     int64(donationTestTotalTimeout / time.Second),
		FirstByteTimeoutSeconds: int64(donationTestFirstByteTimeout / time.Second),
		IdleTimeoutSeconds:      int64(donationTestIdleTimeout / time.Second),
		MaxNoteBytes:            donationMaxNoteBytes,
	}
}

type DonationCapabilities struct {
	ProtocolVersion string               `json:"protocol_version"`
	InstanceID      string               `json:"instance_id"`
	SourceID        string               `json:"source_id"`
	InputTypes      []string             `json:"input_types"`
	Limits          DonationLimits       `json:"limits"`
	Features        []string             `json:"features"`
	ReviewLimits    DonationReviewLimits `json:"review_limits"`
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
	// Manual review relaxes only the probe-constructibility dependency. Group
	// enablement, a plain single-field api_key channel and an un-overridden
	// credential header are still required.
	CanManualReview         bool     `json:"can_manual_review"`
	ManualTargetRevision    string   `json:"manual_target_revision"`
	ManualUnavailableReason string   `json:"manual_unavailable_reason"`
	ChatModels              []string `json:"chat_models"`
}

type donationTarget struct {
	row            models.Group
	group          state.GroupView
	probe          groupValidationTarget
	revision       string
	manualRevision string
	manualReason   string
	chatModels     []string
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
			StagingRetentionSeconds: int64(donationStagingTTL / time.Second)},
		Features:     []string{donationFeatureManualReview},
		ReviewLimits: defaultDonationReviewLimits()}, nil
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
			item.CanManualReview = target.manualReason == ""
			item.ManualUnavailableReason = target.manualReason
			item.ManualTargetRevision = target.manualRevision
			item.ChatModels = append([]string(nil), target.chatModels...)
			result = append(result, item)
		}
		return nil
	})
	return result, err
}

func (s *Service) buildDonationTarget(ctx context.Context, tx *gorm.DB, row models.Group) (donationTarget, string, error) {
	if !row.Enabled {
		return donationTarget{manualReason: "group_disabled"}, "group_disabled", nil
	}
	if normalizeGroupConnectionType(row.ConnectionType) != models.ConnectionTypeAPIKey || !s.donationChannelSupportsPlainKey(channel.ID(row.ChannelID)) {
		return donationTarget{manualReason: "unsupported_input"}, "unsupported_input", nil
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
		return donationTarget{manualReason: "target_unavailable"}, "target_unavailable", nil
	}
	view, exists := snapshot.Groups[row.ID]
	if !exists {
		return donationTarget{manualReason: "target_unavailable"}, "target_unavailable", nil
	}
	// A configured static authentication override could test a different key.
	// Such groups must be repaired by the operator before they can accept donations.
	for name, value := range view.HeaderRules.Set {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "x-api-key", "x-goog-api-key", "api-key":
			if !strings.Contains(value, "${API_KEY}") {
				return donationTarget{manualReason: "credential_override"}, "credential_override", nil
			}
		}
	}
	for _, name := range view.HeaderRules.Remove {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "x-api-key", "x-goog-api-key", "api-key":
			return donationTarget{manualReason: "credential_override"}, "credential_override", nil
		}
	}
	manualRevision, err := s.donationManualRevision(view)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrInternalServer
	}
	target := donationTarget{row: row, group: view, manualRevision: manualRevision,
		chatModels: donationChatModels(view)}
	probe, valid := buildGroupValidationTarget(view)
	if !valid {
		return target, "probe_unavailable", nil
	}
	timeouts, err := json.Marshal(view.Timeouts)
	if err != nil {
		return donationTarget{}, "", app_errors.ErrInternalServer
	}
	target.probe = probe
	target.revision = s.encryption.Hash("gpt-load/donation-target/v1\x00" + hex.EncodeToString(probe.signature[:]) + string(timeouts))
	return target, "", nil
}

// donationChatModels lists the models this group can actually serve through a
// single controlled OpenAI-compatible chat completion, native or converted.
func donationChatModels(view state.GroupView) []string {
	models := make([]string, 0, len(view.Models))
	for _, model := range view.Models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, supported := view.ResolvedTarget.ModeForModel(protocol.OpenAICompletions, execution.OperationChatCompletion, id); !supported {
			continue
		}
		models = append(models, id)
	}
	return models
}

// donationManualRevision signs the effective execution configuration under its
// own HMAC domain. Display-only fields such as the group name are excluded so a
// rename does not invalidate a frozen manual target.
func (s *Service) donationManualRevision(view state.GroupView) (string, error) {
	overrides, err := view.ParameterOverrides.Fingerprint()
	if err != nil {
		return "", err
	}
	timeouts, err := json.Marshal(view.Timeouts)
	if err != nil {
		return "", err
	}
	models, err := json.Marshal(view.Models)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	write := func(value []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(value)
	}
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(view.ID))
	_, _ = hasher.Write(encoded[:])
	write([]byte(view.ChannelID))
	write([]byte(view.ConnectionType))
	write([]byte(view.ResolvedTarget.ProviderKind))
	write(view.ResolvedTarget.TargetConfig)
	write([]byte(view.Proxy.Source))
	write([]byte(view.Proxy.Config.Mode))
	write([]byte(view.Proxy.Config.URL))
	type headerSetPart struct{ name, value string }
	setParts := make([]headerSetPart, 0, len(view.HeaderRules.Set))
	for name, value := range view.HeaderRules.Set {
		setParts = append(setParts, headerSetPart{name: normalizeValidationHeaderName(name), value: value})
	}
	sort.Slice(setParts, func(i, j int) bool {
		if setParts[i].name != setParts[j].name {
			return setParts[i].name < setParts[j].name
		}
		return setParts[i].value < setParts[j].value
	})
	binary.BigEndian.PutUint64(encoded[:], uint64(len(setParts)))
	_, _ = hasher.Write(encoded[:])
	for _, part := range setParts {
		write([]byte(part.name))
		write([]byte(part.value))
	}
	removeNames := make([]string, len(view.HeaderRules.Remove))
	for index, name := range view.HeaderRules.Remove {
		removeNames[index] = normalizeValidationHeaderName(name)
	}
	sort.Strings(removeNames)
	binary.BigEndian.PutUint64(encoded[:], uint64(len(removeNames)))
	_, _ = hasher.Write(encoded[:])
	for _, name := range removeNames {
		write([]byte(name))
	}
	write(models)
	write([]byte(overrides))
	write(timeouts)
	return s.encryption.Hash("gpt-load/donation-manual-target/v1\x00" + hex.EncodeToString(hasher.Sum(nil))), nil
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
			return donationTarget{manualReason: "group_deleted"}, "group_deleted", nil
		}
		return donationTarget{}, "", app_errors.ParseDBError(err)
	}
	return s.buildDonationTarget(ctx, tx, row)
}
