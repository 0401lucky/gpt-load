package health

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gpt-load/internal/execution"
)

func TestInferenceRateLimitsCooldownOnlyTheirModel(t *testing.T) {
	now := time.Now()
	for _, scope := range []execution.ErrorScope{"", execution.ErrorScopeModel, execution.ErrorScopeCredential, execution.ErrorScopeRequest} {
		for _, committed := range []bool{false, true} {
			result := JudgeExecution(ExecutionAttempt{DispatchState: execution.DispatchMaybeSent, StatusCode: 429,
				DownstreamCommitted: committed, Now: now, Evidence: &execution.ErrorEvidence{
					Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited, StatusCode: 429,
					ScopeHint: scope, RetryAfter: 7 * time.Hour, ReplaySafety: execution.ReplaySafetyRejectedBeforeProcessing,
				}}, DecisionContext{Operation: execution.OperationChatCompletion})
			want := Effect("cooldown_model")
			if scope == execution.ErrorScopeCredential {
				want = EffectCooldownCredential
			}
			if scope == execution.ErrorScopeRequest {
				want = EffectNone
			}
			if result.Effect != want {
				t.Fatalf("scope=%s committed=%t decision=%#v", scope, committed, result)
			}
			if result.QuotaCooldown != (want != EffectNone) {
				t.Fatalf("quota recovery flag disagrees with effect: %+v", result)
			}
			if want != EffectNone && !result.CooldownUntil.Equal(now.Add(7*time.Hour)) {
				t.Fatalf("deadline = %v", result.CooldownUntil)
			}
			if committed && result.Retry != RetryNone {
				t.Fatal("committed response retried")
			}
			if err := result.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestQuotaResetRequiresDailyQuotaEvidenceAndKeepsRetryDeadline(t *testing.T) {
	now := time.Now()
	for _, summary := range []string{"rate limited", "Try again in 30s", "Daily free limit reached on model fixture. Try again in 3h 18m"} {
		for _, enabled := range []bool{false, true} {
			for _, status := range []int{0, 200, 429} {
				result := JudgeExecution(ExecutionAttempt{DispatchState: execution.DispatchMaybeSent, StatusCode: status, Now: now,
					Evidence: &execution.ErrorEvidence{Kind: execution.ErrorKindProvider, Hint: execution.FailureHintRateLimited,
						Summary: summary, RetryAfter: time.Minute}},
					DecisionContext{Operation: execution.OperationChatCompletion, RateLimitResetHint: enabled})
				if !result.CooldownUntil.Equal(now.Add(time.Minute)) {
					t.Fatalf("retry scheduling changed: %+v", result)
				}
				if !result.QuotaCooldown {
					t.Fatalf("quota cooldown lost its recovery flag: %+v", result)
				}
				wantReset := enabled && summary != "rate limited" && summary != "Try again in 30s"
				if wantReset {
					if result.QuotaResetSource != "reset_hint" || !result.QuotaResetAt.Equal(now.Add(3*time.Hour+18*time.Minute)) {
						t.Fatalf("reset swallowed or included retry margin: %+v", result)
					}
				} else if !result.QuotaResetAt.IsZero() || result.QuotaResetSource != "" {
					t.Fatalf("untrusted evidence produced quota reset: %+v", result)
				}
				if err := result.Validate(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestModelCooldownHonorsLongExplicitRetryAfter(t *testing.T) {
	now := time.Now()
	result := JudgeExecution(ExecutionAttempt{DispatchState: execution.DispatchMaybeSent, StatusCode: 429, Now: now,
		Header: http.Header{"Retry-After": {"172800"}}, Evidence: &execution.ErrorEvidence{
			Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited, StatusCode: 429,
		}}, DecisionContext{Operation: execution.OperationChatCompletion})
	if !result.CooldownUntil.Equal(now.Add(48 * time.Hour)) {
		t.Fatalf("deadline = %v", result.CooldownUntil)
	}
}

func TestNonInferenceUnknownLimitsDoNotCooldownInference(t *testing.T) {
	for _, operation := range []execution.Operation{execution.OperationCountTokens, execution.OperationResponsesInputTokens, execution.OperationListModels, execution.OperationResponsesRetrieve, execution.OperationProbe} {
		result := JudgeExecution(ExecutionAttempt{DispatchState: execution.DispatchMaybeSent, StatusCode: 429, Now: time.Now(), Evidence: &execution.ErrorEvidence{Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited, StatusCode: 429}}, DecisionContext{Operation: operation})
		if result.Effect != EffectNone {
			t.Errorf("%s writes inference cooldown: %#v", operation, result)
		}
	}
}

func TestCanceledTransientCapacityRejectionDoesNotCreateModelCooldown(t *testing.T) {
	result := JudgeExecution(ExecutionAttempt{
		DispatchState: execution.DispatchMaybeSent, StatusCode: http.StatusOK,
		Now: time.Now(), DownstreamErr: context.Canceled,
		Evidence: &execution.ErrorEvidence{Kind: execution.ErrorKindProvider, Hint: execution.FailureHintRateLimited,
			OriginHint: execution.ErrorOriginUpstream, Code: "rate_limit_exceeded",
			ReplaySafety: execution.ReplaySafetyRejectedBeforeProcessing},
	}, DecisionContext{Operation: execution.OperationResponsesCreate})
	if result.Effect != EffectNone || !result.CooldownUntil.IsZero() || result.Retry != RetryNone {
		t.Fatalf("cancellation changed a transient rejection into a cooldown: %#v", result)
	}
}

func TestModelCooldownDoesNotUseUnrelatedWindowResetHeaders(t *testing.T) {
	now := time.Now()
	for _, duration := range []time.Duration{time.Minute, 10 * time.Minute} {
		result := JudgeExecution(ExecutionAttempt{
			DispatchState: execution.DispatchMaybeSent, StatusCode: http.StatusTooManyRequests, Now: now,
			Header:   http.Header{"Anthropic-Ratelimit-Tokens-Reset": {now.Add(30 * time.Minute).Format(time.RFC3339)}},
			Evidence: &execution.ErrorEvidence{Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited},
		}, DecisionContext{Operation: execution.OperationChatCompletion, DefaultRateLimitCooldown: duration})
		if result.Effect != EffectCooldownModel || !result.CooldownUntil.Equal(now.Add(duration)) {
			t.Fatalf("default %v replaced by an unrelated window: %#v", duration, result)
		}
	}
}

func TestModelCooldownUsesResetHintOnlyWhenEnabled(t *testing.T) {
	now := time.Now()
	const resetHint = "Error 429: Daily free limit reached on model z-ai/glm-5.3-flash. Try again in 3h 18m"
	for _, test := range []struct {
		name     string
		enabled  bool
		summary  string
		want     time.Duration
		wantRule RuleID
	}{
		{
			name: "enabled parses the upstream deadline", enabled: true, summary: resetHint,
			want: 3*time.Hour + 19*time.Minute, wantRule: "rate_limit.reset_hint",
		},
		{
			name: "disabled keeps the default cooldown", enabled: false, summary: resetHint,
			want: time.Minute, wantRule: "rate_limit.model.default_cooldown",
		},
		{
			name: "enabled without a usable deadline keeps the default cooldown", enabled: true,
			summary: "Error 429: Daily free limit reached on model z-ai/glm-5.3-flash.",
			want:    time.Minute, wantRule: "rate_limit.model.default_cooldown",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := JudgeExecution(ExecutionAttempt{
				DispatchState: execution.DispatchMaybeSent, StatusCode: http.StatusTooManyRequests, Now: now,
				Evidence: &execution.ErrorEvidence{
					Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited,
					StatusCode: http.StatusTooManyRequests, Summary: test.summary,
				},
			}, DecisionContext{Operation: execution.OperationChatCompletion, RateLimitResetHint: test.enabled})
			if result.Effect != EffectCooldownModel || result.RuleID != test.wantRule ||
				!result.CooldownUntil.Equal(now.Add(test.want)) {
				t.Fatalf("decision = %#v; want effect=%s rule=%s deadline=%v",
					result, EffectCooldownModel, test.wantRule, now.Add(test.want))
			}
		})
	}
}

func TestCustomCooldownKeepsTrustedModelResetIndependentOfQuotaRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, effect := range []Effect{EffectCooldownModel, EffectCooldownCredential, EffectNone} {
		for _, committed := range []bool{false, true} {
			cooldownSeconds := int64(90)
			if effect == EffectNone {
				cooldownSeconds = 0
			}
			rules := mustCompileErrorRules(t, []ErrorRule{{Keywords: []string{"daily free limit"},
				Retry: RetryNone, Effect: effect, CooldownSeconds: cooldownSeconds}}, "group")
			result := JudgeExecution(ExecutionAttempt{DispatchState: execution.DispatchMaybeSent,
				StatusCode: 200, Now: now, DownstreamCommitted: committed,
				Evidence: &execution.ErrorEvidence{Kind: execution.ErrorKindProvider,
					Hint: execution.FailureHintRateLimited, OriginHint: execution.ErrorOriginUpstream,
					ScopeHint: execution.ErrorScopeModel, ReplaySafety: execution.ReplaySafetyRejectedBeforeProcessing,
					Summary:    "Daily free limit reached on model fixture. Try again in 3h 18m",
					RetryAfter: time.Hour}}, DecisionContext{ErrorRules: rules, RateLimitResetHint: true,
				Method: http.MethodPost, Operation: execution.OperationChatCompletion})
			wantEffect := effect
			if committed && effect == EffectCooldownCredential {
				wantEffect = EffectNone
			}
			if result.Effect != wantEffect || result.QuotaCooldown {
				t.Fatalf("effect=%s committed=%t: custom cooldown changed the evidence boundary: %+v", effect, committed, result)
			}
			if wantEffect != EffectNone && !result.CooldownUntil.Equal(now.Add(90*time.Second)) {
				t.Fatalf("custom retry scheduling was not applied: %+v", result)
			}
			if wantEffect == EffectNone && !result.CooldownUntil.IsZero() {
				t.Fatalf("discarded effect retained a scheduling deadline: %+v", result)
			}
			if effect == EffectCooldownModel {
				if !result.QuotaResetAt.Equal(now.Add(3*time.Hour+18*time.Minute)) || result.QuotaResetSource != "reset_hint" {
					t.Fatalf("custom scheduling erased independent quota evidence: %+v", result)
				}
			} else if !result.QuotaResetAt.IsZero() || result.QuotaResetSource != "" {
				t.Fatalf("non-model effect retained model reset: %+v", result)
			}
			if err := result.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
