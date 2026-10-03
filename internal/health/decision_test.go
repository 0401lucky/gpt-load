package health

import (
	"testing"
	"time"

	"gpt-load/internal/execution"
)

func TestDecisionValidatesQuotaResetEvidence(t *testing.T) {
	valid := Decision{Category: FailureCategoryRateLimited, Origin: execution.ErrorOriginUpstream,
		Scope: execution.ErrorScopeModel, Retry: RetryNone, Effect: EffectCooldownModel,
		CooldownUntil: time.Now().Add(time.Minute), QuotaResetAt: time.Now().Add(time.Hour),
		QuotaResetSource: "reset_hint", RuleID: "rate_limit.retry_after"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Decision){
		func(d *Decision) { d.QuotaResetSource = "retry_after" },
		func(d *Decision) { d.QuotaResetSource = "" },
		func(d *Decision) { d.QuotaResetAt = time.Time{} },
		func(d *Decision) { d.Effect = EffectCooldownCredential },
		func(d *Decision) { d.Origin = execution.ErrorOriginInternal },
	} {
		invalid := valid
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted invalid evidence: %+v", invalid)
		}
	}
}

func TestDecisionShouldRetry(t *testing.T) {
	tests := []struct {
		retry RetryDirective
		want  bool
	}{
		{retry: RetryNone, want: false},
		{retry: RetryRefreshCredential, want: true},
		{retry: RetryNextCandidate, want: true},
		{retry: RetryDirective("unknown"), want: false},
		{retry: "", want: false},
	}
	for _, test := range tests {
		if got := (Decision{Retry: test.retry}).ShouldRetry(); got != test.want {
			t.Fatalf("Decision{Retry:%q}.ShouldRetry() = %t, want %t", test.retry, got, test.want)
		}
	}
}
