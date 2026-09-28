package control

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	"gpt-load/internal/health"
	"gpt-load/internal/platform/epochms"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/redact"
	"gpt-load/internal/platform/utils"
	"gpt-load/internal/protocol"
	"gpt-load/internal/state"
	stateloader "gpt-load/internal/state/loader"
	"gpt-load/internal/storage/models"
)

type CredentialProbeOutcome string

const (
	CredentialProbeOutcomePassed       CredentialProbeOutcome = "passed"
	CredentialProbeOutcomeFailed       CredentialProbeOutcome = "failed"
	CredentialProbeOutcomeInconclusive CredentialProbeOutcome = "inconclusive"
)

type CredentialProbeReason string

const (
	CredentialProbeReasonInvalidCredential CredentialProbeReason = "invalid_credential"
	CredentialProbeReasonModelUnavailable  CredentialProbeReason = "model_unavailable"
	CredentialProbeReasonRateLimited       CredentialProbeReason = "rate_limited"
	CredentialProbeReasonTimeout           CredentialProbeReason = "timeout"
	CredentialProbeReasonUpstreamError     CredentialProbeReason = "upstream_error"
	CredentialProbeReasonIncompatible      CredentialProbeReason = "probe_incompatible"
	CredentialProbeReasonUnknown           CredentialProbeReason = "unknown"
)

type CredentialProbeResponse struct {
	UpstreamStatusCode *int                   `json:"upstream_status_code"`
	ErrorSummary       *string                `json:"error_summary"`
	RetryAfterSeconds  *int64                 `json:"retry_after_seconds"`
	Outcome            CredentialProbeOutcome `json:"outcome"`
	Model              string                 `json:"model"`
	Protocol           protocol.Protocol      `json:"protocol"`
	LatencyMS          int64                  `json:"latency_ms"`
	Reason             *CredentialProbeReason `json:"reason"`
	CanRestore         bool                   `json:"can_restore"`
	RestoreProof       *string                `json:"restore_proof"`
	TestedAtMS         int64                  `json:"tested_at_ms"`
}

type CredentialProbeRequest struct {
	Protocol optionalField[protocol.Protocol] `json:"protocol"`
	Model    optionalField[string]            `json:"model"`
}

type CredentialProbeRestoreRequest struct {
	RestoreProof string `json:"restore_proof"`
}

const credentialProbeRestoreProofDomain = "gpt-load/control/credential-probe-restore/v1"

type credentialProbeRestoreProofPayload struct {
	CredentialID       uint   `json:"credential_id"`
	CooldownUntil      string `json:"cooldown_until"`
	EncryptedProxy     string `json:"encrypted_proxy"`
	EncryptedValue     string `json:"encrypted_value"`
	FailureGeneration  uint64 `json:"failure_generation"`
	Fingerprint        string `json:"fingerprint"`
	GroupID            uint   `json:"group_id"`
	IdentityGeneration uint64 `json:"identity_generation"`
	ProxyFingerprint   string `json:"proxy_fingerprint"`
	TargetSignature    string `json:"target_signature"`
	Version            uint64 `json:"version"`
}

type credentialProbeCredential struct {
	ref           state.CredentialRef
	cooldownUntil time.Time
}

type credentialProbeExecution struct {
	diagnostics credentialProbeDiagnostics
	result      execution.AttemptResult
	latency     time.Duration
	protocol    protocol.Protocol
}

type credentialProbeExecutor struct {
	decryptor credentialDecryptor
	channels  *channel.Registry
	executor  execution.Executor
	now       func() time.Time
}

type credentialProbeFailure struct {
	stage string
	cause error
}

func (failure *credentialProbeFailure) Error() string {
	return "credential probe preparation failed at " + failure.stage
}

func (failure *credentialProbeFailure) Unwrap() error {
	return failure.cause
}

func newCredentialProbeExecutor(
	decryptor credentialDecryptor,
	channels *channel.Registry,
	executor execution.Executor,
) *credentialProbeExecutor {
	return &credentialProbeExecutor{
		decryptor: decryptor,
		channels:  channels,
		executor:  executor,
		now:       time.Now,
	}
}

func (probe *credentialProbeExecutor) Probe(
	ctx context.Context,
	group state.GroupView,
	target groupValidationTarget,
	ref state.CredentialRef,
) (credentialProbeExecution, error) {
	if probe == nil || probe.executor == nil || probe.channels == nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"missing_executor",
			app_errors.ErrInternalServer,
		)
	}
	if probe.decryptor == nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"decrypt",
			app_errors.ErrInternalServer,
		)
	}
	if err := ctx.Err(); err != nil {
		return credentialProbeExecution{}, err
	}
	plaintext, err := probe.decryptor.Decrypt(ref.EncryptedValue)
	if err != nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"decrypt",
			app_errors.ErrInternalServer,
		)
	}
	credential, err := normalizeStoredCredential(probe.channels, group.ChannelID, plaintext)
	plaintext = ""
	if err != nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"credential",
			app_errors.ErrInternalServer,
		)
	}
	apiKey, _ := credential.Value("api_key")
	proxy, proxyFingerprint, err := validationAttemptProxy(probe.decryptor, group.Proxy, ref)
	if err != nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"proxy",
			app_errors.ErrInternalServer,
		)
	}
	requestID, err := newOperationID(cryptorand.Reader)
	if err != nil {
		return credentialProbeExecution{}, newCredentialProbeFailure(
			"request_identity",
			app_errors.ErrInternalServer,
		)
	}
	version := ref.Version
	if version == 0 {
		version = 1
	}
	generation := ref.IdentityGeneration
	if generation == 0 {
		generation = 1
	}
	probeProtocols := []protocol.Protocol{target.protocol}
	probeProtocols = append(probeProtocols, target.fallbackProtocols...)
	startedAt := probe.now()
	for index, probeProtocol := range probeProtocols {
		routeMode, supported := group.ResolvedTarget.ModeForModel(
			probeProtocol,
			execution.OperationProbe,
			target.model,
		)
		if !supported {
			return credentialProbeExecution{}, newCredentialProbeFailure(
				"request",
				app_errors.ErrValidation,
			)
		}
		attemptID, attemptErr := newOperationID(cryptorand.Reader)
		if attemptErr != nil {
			return credentialProbeExecution{}, newCredentialProbeFailure(
				"attempt_identity",
				app_errors.ErrInternalServer,
			)
		}
		spec := execution.NewAttemptSpec(execution.AttemptSpec{
			RequestID: requestID, AttemptID: attemptID, Sequence: uint32(index + 1),
			ChannelID: string(group.ChannelID),
			RouteMode: execution.RouteMode(routeMode), ClientProtocol: probeProtocol,
			Operation: execution.OperationProbe, ClientModel: target.model, UpstreamModel: target.model,
			Header:            applyControlHeaderRules(group.HeaderRules, apiKey),
			ConfiguredHeaders: group.HeaderRules.ConfiguredNames(),
			TargetConfig:      group.ResolvedTarget.TargetConfig,
			Timeouts:          executionTimeouts(group.Timeouts),
			Credential: execution.NewCredentialSnapshot(
				ref.ID,
				version,
				generation,
				credential.CanonicalJSON(),
			),
			Proxy: proxy, ProxyFingerprint: proxyFingerprint,
		})
		if err := spec.Validate(); err != nil {
			return credentialProbeExecution{}, newCredentialProbeFailure(
				"request",
				app_errors.ErrValidation,
			)
		}
		result := probe.executor.Execute(ctx, spec)
		if err := ctx.Err(); err != nil {
			return credentialProbeExecution{}, err
		}
		latency := max(probe.now().Sub(startedAt), 0)
		executed := credentialProbeExecution{
			result: result, latency: latency, protocol: probeProtocol,
			diagnostics: projectCredentialProbeDiagnostics(result, spec, startedAt),
		}
		if credentialProbePassed(result) || index+1 == len(probeProtocols) ||
			!validationProbeNeedsProtocolFallback(result) {
			return executed, nil
		}
	}
	return credentialProbeExecution{}, newCredentialProbeFailure(
		"probe",
		app_errors.ErrInternalServer,
	)
}

type credentialProbeDiagnostics struct {
	status  *int
	summary *string
	retry   *int64
}

// Project while the attempt's known secrets are still in scope. No raw body,
// transport error or internal preparation failure crosses this boundary.
func projectCredentialProbeDiagnostics(result execution.AttemptResult, spec execution.AttemptSpec, now time.Time) credentialProbeDiagnostics {
	var diagnostics credentialProbeDiagnostics
	if result.Validate() != nil {
		return diagnostics
	}
	if result.StatusCode >= 100 && result.StatusCode <= 599 {
		value := result.StatusCode
		diagnostics.status = &value
	}
	if result.Error == nil {
		return diagnostics
	}
	if result.StatusCode == 0 || (result.Error.Kind != execution.ErrorKindHTTP && result.Error.Kind != execution.ErrorKindProvider) {
		return diagnostics
	}
	secrets := []string{string(spec.Credential.Data()), spec.Proxy.Config.URL}
	var fields map[string]string
	if json.Unmarshal(spec.Credential.Data(), &fields) == nil {
		for _, value := range fields {
			secrets = append(secrets, value)
		}
	}
	for _, values := range spec.Header {
		secrets = append(secrets, values...)
		for _, value := range values {
			parts := strings.Fields(value)
			if len(parts) == 2 && (strings.EqualFold(parts[0], "Bearer") || strings.EqualFold(parts[0], "Basic")) {
				secrets = append(secrets, parts[1])
			}
		}
	}
	if parsed, err := url.Parse(spec.Proxy.Config.URL); err == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		username := parsed.User.Username()
		secrets = append(secrets, parsed.User.String(), username, password, base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}
	summary := redact.ExtractErrorMessage([]byte(result.Error.Summary))
	// Nested message fields can still contain HTML; never expose markup or stacks.
	if strings.ContainsAny(summary, "<>") || strings.Contains(summary, "goroutine ") || strings.Contains(summary, "Traceback (") {
		summary = ""
	}
	summary = redact.New().String(summary, secrets...)
	summary = strings.Join(strings.Fields(summary), " ")
	if runes := []rune(summary); len(runes) > execution.MaxErrorSummaryLength {
		summary = string(runes[:execution.MaxErrorSummaryLength-1]) + "…"
	}
	if summary != "" {
		diagnostics.summary = &summary
	}
	delay := result.Error.RetryAfter
	// Evidence takes precedence, matching execution's explicit upstream evidence.
	if delay <= 0 {
		for _, header := range []http.Header{result.Header, result.Error.Header} {
			if until, ok := health.ParseExplicitRetryAfter(header, now); ok {
				delay = until.Sub(now)
				break
			}
		}
	}
	if delay > 0 {
		seconds := int64(delay / time.Second)
		if delay%time.Second != 0 {
			seconds++
		}
		if seconds <= maxSafeInteger {
			diagnostics.retry = &seconds
		}
	}
	return diagnostics
}

func newCredentialProbeFailure(stage string, cause error) error {
	return &credentialProbeFailure{stage: stage, cause: cause}
}

func credentialProbeFailureStage(err error) string {
	var failure *credentialProbeFailure
	if errors.As(err, &failure) && failure.stage != "" {
		return failure.stage
	}
	return "probe"
}

func credentialProbePassed(result execution.AttemptResult) bool {
	return result.Validate() == nil && result.Error == nil &&
		result.StatusCode >= http.StatusOK &&
		result.StatusCode < http.StatusMultipleChoices
}

func classifyCredentialProbeResult(result execution.AttemptResult) (
	CredentialProbeOutcome,
	*CredentialProbeReason,
) {
	if result.Validate() != nil {
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonUnknown,
		)
	}
	if credentialProbePassed(result) {
		return CredentialProbeOutcomePassed, nil
	}
	if result.Error == nil {
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonUnknown,
		)
	}
	decision := health.JudgeExecution(health.ExecutionAttempt{
		DispatchState: result.DispatchState,
		StatusCode:    result.StatusCode,
		Header:        result.Header,
		Evidence:      result.Error,
		Now:           time.Now(),
	}, health.DecisionContext{Operation: execution.OperationProbe})
	switch decision.Category {
	case health.FailureCategoryInvalidKey:
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeFailed,
			CredentialProbeReasonInvalidCredential,
		)
	case health.FailureCategoryModelUnavailable:
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeFailed,
			CredentialProbeReasonModelUnavailable,
		)
	case health.FailureCategoryRateLimited:
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonRateLimited,
		)
	}
	if result.Error.Kind == execution.ErrorKindTimeout {
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonTimeout,
		)
	}
	if result.Error.Kind == execution.ErrorKindConversionUnsupported ||
		result.Error.Kind == execution.ErrorKindInvalidRequest {
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonIncompatible,
		)
	}
	if decision.Category == health.FailureCategoryUpstreamHostError ||
		result.Error.Kind == execution.ErrorKindTransport ||
		result.Error.Kind == execution.ErrorKindHTTP ||
		result.Error.Kind == execution.ErrorKindProvider {
		return credentialProbeOutcomeWithReason(
			CredentialProbeOutcomeInconclusive,
			CredentialProbeReasonUpstreamError,
		)
	}
	return credentialProbeOutcomeWithReason(
		CredentialProbeOutcomeInconclusive,
		CredentialProbeReasonUnknown,
	)
}

func credentialProbeOutcomeWithReason(
	outcome CredentialProbeOutcome,
	reason CredentialProbeReason,
) (CredentialProbeOutcome, *CredentialProbeReason) {
	return outcome, &reason
}

func (s *Service) TestGroupCredential(
	ctx context.Context,
	groupID uint,
	credentialID uint,
	requests ...CredentialProbeRequest,
) (CredentialProbeResponse, error) {
	if groupID == 0 || credentialID == 0 {
		return CredentialProbeResponse{}, app_errors.ErrBadRequest
	}
	request := CredentialProbeRequest{}
	if len(requests) > 0 {
		request = requests[0]
	}
	if request.Protocol.Set && (request.Protocol.Null || !request.Protocol.Value.Valid()) {
		return CredentialProbeResponse{}, app_errors.ErrValidation
	}
	if request.Model.Set {
		if request.Model.Null {
			return CredentialProbeResponse{}, app_errors.ErrValidation
		}
		model, err := normalizeValidationModel(request.Model.Value)
		if err != nil {
			return CredentialProbeResponse{}, err
		}
		request.Model.Value = model
	}
	group, target, credential, err := s.captureCredentialProbe(ctx, groupID, credentialID, request)
	if err != nil {
		return CredentialProbeResponse{}, err
	}

	probe := newCredentialProbeExecutor(s.encryption, s.channelRegistry, s.executor)
	executed, err := probe.Probe(ctx, group, target, credential.ref)
	if err != nil {
		return CredentialProbeResponse{}, err
	}
	outcome, reason := classifyCredentialProbeResult(executed.result)
	testedAt, err := epochms.FromTime(s.now().UTC())
	if err != nil {
		return CredentialProbeResponse{}, fmt.Errorf(
			"encode credential probe time: %w",
			app_errors.ErrInternalServer,
		)
	}
	response := CredentialProbeResponse{
		UpstreamStatusCode: executed.diagnostics.status,
		ErrorSummary:       executed.diagnostics.summary,
		RetryAfterSeconds:  executed.diagnostics.retry,
		Outcome:            outcome, Model: target.model, Protocol: executed.protocol,
		LatencyMS:  max(executed.latency.Milliseconds(), 0),
		Reason:     reason,
		TestedAtMS: testedAt,
	}
	if outcome == CredentialProbeOutcomePassed {
		response.RestoreProof = s.currentCredentialProbeRestoreProof(credential, target.signature)
		response.CanRestore = response.RestoreProof != nil
	}
	logCredentialProbe(credential.ref, response)
	return response, nil
}

func (s *Service) RestoreTestedGroupCredential(
	ctx context.Context,
	groupID uint,
	credentialID uint,
	restoreProof string,
) (CredentialItemResponse, error) {
	if groupID == 0 || credentialID == 0 {
		return CredentialItemResponse{}, app_errors.ErrBadRequest
	}
	restoreProof = strings.TrimSpace(restoreProof)
	if restoreProof == "" {
		return CredentialItemResponse{}, app_errors.ErrValidation
	}
	return s.restoreGroupCredential(ctx, groupID, credentialID, restoreProof)
}

func (s *Service) captureCredentialProbe(
	ctx context.Context,
	groupID uint,
	credentialID uint,
	request CredentialProbeRequest,
) (state.GroupView, groupValidationTarget, credentialProbeCredential, error) {
	if s == nil || s.db == nil || s.manager == nil || s.registry == nil {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, app_errors.ErrInternalServer
	}
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	groupRow, err := loadGroupRow(s.db.WithContext(ctx), groupID)
	if err != nil {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, err
	}
	if normalizeGroupConnectionType(groupRow.ConnectionType) == models.ConnectionTypeSubscription {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, app_errors.ErrForbidden
	}
	var credentialRow models.Credential
	if err := s.db.WithContext(ctx).
		Where("id = ? AND group_id = ?", credentialID, groupID).
		Take(&credentialRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, credentialNotFoundError()
		}
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, app_errors.ParseDBError(err)
	}
	view, exists := findRuntimeCredential(s.registry.Snapshot(), credentialID)
	if err := validateCredentialRuntimeRow(groupRow, credentialRow, view, exists); err != nil {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, err
	}
	entries, err := s.registry.SnapshotGroupCredentialEntriesExact(groupID, []uint{credentialID})
	if err != nil || len(entries) != 1 {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, dbRegistryMismatch(
			mismatchMissingRegistry,
			groupID,
			credentialID,
		)
	}
	entry := entries[0]
	expectedProxy, expectedProxyFingerprint, err := storedProxyIdentity(s.encryption, credentialRow.ProxyConfig)
	if err != nil {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, err
	}
	if entry.Fingerprint != credentialRow.Fingerprint ||
		entry.EncryptedValue != credentialRow.Data ||
		entry.EncryptedProxy != expectedProxy ||
		entry.ProxyFingerprint != expectedProxyFingerprint {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, dbRegistryMismatch(
			mismatchIdentity,
			groupID,
			credentialID,
		)
	}
	snapshot := s.manager.Current()
	if snapshot == nil {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, app_errors.ErrInternalServer
	}
	group, exists := snapshot.Groups[groupID]
	if !exists && !groupRow.Enabled {
		group, err = s.compileDisabledGroupProbe(ctx, groupRow)
		if err != nil {
			return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, err
		}
		exists = true
	}
	if !exists {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, dbRegistryMismatch(
			mismatchMissingRegistry,
			groupID,
			credentialID,
		)
	}
	baseTarget, baseValid := buildGroupValidationTarget(group)
	probeGroup := group
	if request.Protocol.Set {
		probeGroup.ValidationProtocol = request.Protocol.Value
	}
	if request.Model.Set {
		probeGroup.ValidationModel = request.Model.Value
	}
	target, valid := buildGroupValidationTarget(probeGroup)
	if baseValid {
		target.signature = baseTarget.signature
	}
	if !valid {
		return state.GroupView{}, groupValidationTarget{}, credentialProbeCredential{}, app_errors.ErrValidation
	}
	return group, target, credentialProbeCredentialFromEntry(entry), nil
}

func credentialProbeRef(entry state.CredentialEntry) state.CredentialRef {
	return state.CredentialRef{
		ID: entry.ID, GroupID: entry.GroupID,
		Version: entry.Version, IdentityGeneration: entry.IdentityGeneration,
		Fingerprint: entry.Fingerprint, EncryptedValue: entry.EncryptedValue,
		EncryptedProxy: entry.EncryptedProxy, ProxyFingerprint: entry.ProxyFingerprint,
		FailureGeneration: entry.FailureGeneration,
	}
}

func credentialProbeCredentialFromEntry(entry state.CredentialEntry) credentialProbeCredential {
	return credentialProbeCredential{
		ref:           credentialProbeRef(entry),
		cooldownUntil: entry.CooldownUntil,
	}
}

func credentialProbeCooldownIdentity(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func (s *Service) currentCredentialProbeRestoreProof(
	tested credentialProbeCredential,
	testedSignature groupValidationSignature,
) *string {
	if s == nil || s.manager == nil || s.registry == nil || s.encryption == nil || s.mutations == nil {
		return nil
	}
	var proof *string
	s.manager.WithCurrentSnapshot(func(snapshot *state.ConfigSnapshot) bool {
		if snapshot == nil {
			return false
		}
		group, exists := snapshot.Groups[tested.ref.GroupID]
		if !exists {
			return false
		}
		currentTarget, valid := buildGroupValidationTarget(group)
		if !valid || currentTarget.signature != testedSignature {
			return false
		}
		capture := func() {
			entries, err := s.registry.SnapshotGroupCredentialEntriesExact(
				tested.ref.GroupID,
				[]uint{tested.ref.ID},
			)
			if err != nil || len(entries) != 1 {
				return
			}
			current := entries[0]
			currentCredential := credentialProbeCredentialFromEntry(current)
			if current.Status != state.CredentialStatusActive || !current.Blacklisted ||
				currentCredential.ref != tested.ref ||
				!currentCredential.cooldownUntil.Equal(tested.cooldownUntil) {
				return
			}
			value, ok := s.credentialProbeRestoreProof(tested, testedSignature)
			if ok {
				proof = &value
			}
		}
		s.mutations.Do(tested.ref.ID, capture)
		return proof != nil
	})
	return proof
}

func (s *Service) credentialProbeRestoreProof(
	credential credentialProbeCredential,
	targetSignature groupValidationSignature,
) (string, bool) {
	if s == nil || s.encryption == nil {
		return "", false
	}
	ref := credential.ref
	payload, err := json.Marshal(credentialProbeRestoreProofPayload{
		CredentialID: ref.ID, GroupID: ref.GroupID,
		CooldownUntil: credentialProbeCooldownIdentity(credential.cooldownUntil),
		Version:       ref.Version, IdentityGeneration: ref.IdentityGeneration,
		Fingerprint: ref.Fingerprint, EncryptedValue: ref.EncryptedValue,
		EncryptedProxy: ref.EncryptedProxy, ProxyFingerprint: ref.ProxyFingerprint,
		FailureGeneration: ref.FailureGeneration,
		TargetSignature:   hex.EncodeToString(targetSignature[:]),
	})
	if err != nil {
		return "", false
	}
	proof := s.encryption.Hash(credentialProbeRestoreProofDomain + "\n" + string(payload))
	return proof, proof != ""
}

func (s *Service) credentialProbeRestoreProofMatches(
	entry state.CredentialEntry,
	targetSignature groupValidationSignature,
	expected string,
) bool {
	if entry.Status != state.CredentialStatusActive || !entry.Blacklisted {
		return false
	}
	current, ok := s.credentialProbeRestoreProof(
		credentialProbeCredentialFromEntry(entry),
		targetSignature,
	)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(current), []byte(expected)) == 1
}

func logCredentialProbe(ref state.CredentialRef, response CredentialProbeResponse) {
	reason := ""
	if response.Reason != nil {
		reason = string(*response.Reason)
	}
	fields := logrus.Fields{
		"event": "credential_probe_completed", "credential_id": ref.ID, "group_id": ref.GroupID,
		"outcome": response.Outcome, "reason": reason, "model": response.Model, "protocol": response.Protocol, "latency_ms": response.LatencyMS,
	}
	if response.UpstreamStatusCode != nil {
		fields["upstream_status_code"] = *response.UpstreamStatusCode
	}
	if response.ErrorSummary != nil {
		fields["error_summary"] = *response.ErrorSummary
	}
	if response.RetryAfterSeconds != nil {
		fields["retry_after_seconds"] = *response.RetryAfterSeconds
	}
	utils.LogPlaneBestEffort(
		logrus.StandardLogger(),
		logrus.InfoLevel,
		utils.LogPlaneControl,
		fields,
		"Credential probe completed",
	)
}

// 禁用分组不进入数据面快照；只为手动测试编译局部视图，不发布或启用分组。
func (s *Service) compileDisabledGroupProbe(ctx context.Context, row models.Group) (state.GroupView, error) {
	group, err := mapGroupRowToState(row)
	if err != nil {
		return state.GroupView{}, err
	}
	group.Enabled = true
	group.Proxy, err = decryptProxyOverride(s.encryption, row.ProxyConfig)
	if err != nil {
		return state.GroupView{}, err
	}
	settings, globalProxy, err := stateloader.LoadSystemSettingsAndProxy(ctx, s.db, s.encryption)
	if err != nil {
		return state.GroupView{}, err
	}
	snapshot, err := state.Compile(state.CompileInput{
		SystemSettings: settings, GlobalProxy: globalProxy, EnvironmentProxy: s.environmentProxy,
		ChannelRegistry: s.channelRegistry, Groups: []state.GroupConfig{group},
	})
	if err != nil {
		return state.GroupView{}, err
	}
	view, exists := snapshot.Groups[row.ID]
	if !exists {
		return state.GroupView{}, app_errors.ErrInternalServer
	}
	return view, nil
}
