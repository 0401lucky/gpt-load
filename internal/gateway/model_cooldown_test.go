package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gpt-load/internal/dialect"
	"gpt-load/internal/execution"
	"gpt-load/internal/platform/config"
	"gpt-load/internal/protocol"
	"gpt-load/internal/state"
)

func TestModelCooldownHandlesSSERejectionAndCanceledResponse(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		result := UpstreamResult{StatusCode: 200, Header: http.Header{"Retry-After": {"3600"}},
			Body: []byte(`{"error":{"type":"rate_limit_error"}}`), ProviderErrorBeforeCommit: true,
			RequestWritten: true, DispatchState: execution.DispatchMaybeSent, ResponseStarted: true,
			ExecutionError: &execution.ErrorEvidence{Kind: execution.ErrorKindProvider, Hint: execution.FailureHintRateLimited,
				ScopeHint: execution.ErrorScopeModel, StatusCode: 429, ReplaySafety: execution.ReplaySafetyRejectedBeforeProcessing}}
		forwarder := &scriptedForwarder{results: []UpstreamResult{result}}
		if canceled {
			forwarder.onCall = func(int) { cancel() }
		}
		engine, _, _, registry := newRequestLogHandlerTestRuntime(t, forwarder, &recordingAccessKeyRPMLimiter{}, &recordingRequestLogSink{}, "sk-one")
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`)).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer gl-client")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		cancel()
		if !canceled && response.Code != 429 {
			t.Fatalf("SSE limit became %d", response.Code)
		}
		if len(registry.ModelCooldowns(1, time.Now())) != 1 {
			t.Fatalf("canceled=%t: lost decoded quota rejection", canceled)
		}
	}
}

func TestModelCooldownRetriesAndFiltersFutureRequests(t *testing.T) {
	forwarder := &scriptedForwarder{results: []UpstreamResult{
		{StatusCode: 429, Header: http.Header{"Retry-After": {"3600"}}, Body: []byte(`{"error":{"code":"rate_limit_exceeded"}}`),
			RequestWritten: true, DispatchState: execution.DispatchMaybeSent, ExecutionError: &execution.ErrorEvidence{
				Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited, StatusCode: 429}},
		successfulAffinityResult(), successfulAffinityResult(),
	}}
	engine, _, _, registry := newRequestLogHandlerTestRuntime(t, forwarder, &recordingAccessKeyRPMLimiter{}, &recordingRequestLogSink{}, "sk-one", "sk-two")
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
		request.Header.Set("Authorization", "Bearer gl-client")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("response=%d %s", response.Code, response.Body.String())
		}
	}
	if len(forwarder.inputs) != 3 || forwarder.inputs[0].Credential.ID == forwarder.inputs[1].Credential.ID || forwarder.inputs[0].Credential.ID == forwarder.inputs[2].Credential.ID {
		t.Fatalf("unexpected candidate retry chain: %#v", forwarder.inputs)
	}
	limited := forwarder.inputs[0].Credential.ID
	if len(registry.ModelCooldowns(limited, time.Now())) != 1 {
		t.Fatal("no model cooldown")
	}
	if until, _ := registry.CredentialCooldownUntil(limited); !until.IsZero() {
		t.Fatal("limited the whole credential")
	}
}

func TestOnlyModelCooledCandidatesReturn429WithoutDispatch(t *testing.T) {
	forwarder := &scriptedForwarder{}
	engine, _, _, registry := newRequestLogHandlerTestRuntime(t, forwarder, &recordingAccessKeyRPMLimiter{}, &recordingRequestLogSink{}, "sk-one")
	now := time.Now()
	ref, _ := registry.CredentialRef(1)
	registry.SetModelCooldown(ref, "gpt-4o", now.Add(time.Hour), now)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	request.Header.Set("Authorization", "Bearer gl-client")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" || len(forwarder.inputs) != 0 {
		t.Fatalf("limited candidate response = %d %s; attempts=%d", response.Code, response.Body.String(), len(forwarder.inputs))
	}
}

func TestOnlyModelCooledCandidatesReportUnavailableAccounts(t *testing.T) {
	forwarder := &scriptedForwarder{}
	engine, handler, _, registry := newRequestLogHandlerTestRuntime(t, forwarder, &recordingAccessKeyRPMLimiter{}, &recordingRequestLogSink{}, "sk-one")
	now := time.Now()
	handler.now = func() time.Time { return now }
	ref, _ := registry.CredentialRef(1)
	registry.SetModelCooldown(ref, "gpt-4o", now.Add(90*time.Minute), now)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	request.Header.Set("Authorization", "Bearer gl-client")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests || len(forwarder.inputs) != 0 {
		t.Fatalf("limited candidate response = %d %s; attempts=%d",
			response.Code, response.Body.String(), len(forwarder.inputs))
	}
	if got := response.Header().Get("Retry-After"); got != "5400" {
		t.Fatalf("Retry-After = %q, want %q", got, "5400")
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
	// 与改动前的限流 code 交叉锁定：下游按 code 归类渠道错误，口径必须不变。
	if body.Code != reasonUpstreamRateLimited.Code ||
		!strings.Contains(body.Message, "No available account for model gpt-4o.") ||
		!strings.Contains(body.Message, "Earliest recovery in about 1h 30m.") {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

// TestRateLimitResetHintCooldownFollowsGroupSwitch 覆盖「分组配置 → DecisionContext → 冷却时长」
// 这条接线：开关只从当前选中分组读，未开启时必须完全走默认冷却。
func TestRateLimitResetHintCooldownFollowsGroupSwitch(t *testing.T) {
	const summary = "Error 429: Daily free limit reached on model gpt-4o. Try again in 3h 18m"
	for _, test := range []struct {
		name    string
		enabled bool
		want    time.Duration
	}{
		{name: "enabled uses the upstream deadline", enabled: true, want: 3*time.Hour + 19*time.Minute},
		{name: "disabled keeps the default cooldown", enabled: false, want: time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			forwarder := &scriptedForwarder{results: []UpstreamResult{{
				StatusCode: http.StatusTooManyRequests, Header: make(http.Header),
				Body: []byte(`{"error":{"code":"rate_limit_exceeded"}}`), RequestWritten: true,
				DispatchState: execution.DispatchMaybeSent,
				ExecutionError: &execution.ErrorEvidence{
					Kind: execution.ErrorKindHTTP, Hint: execution.FailureHintRateLimited,
					StatusCode: http.StatusTooManyRequests, Summary: summary,
				},
			}}}
			engine, registry := newDialectGatewayEngineWithForwarder(
				t,
				protocol.OpenAICompletions,
				"gpt-4o",
				dialect.NewSet(dialect.NewOpenAI()),
				forwarder,
				dialectGatewayGroup{
					id: 1, name: "reset-hint", upstreamURL: "https://provider.example/v1",
					apiKeys:  []string{"sk-provider"},
					settings: config.Settings{state.SettingRateLimitResetHintEnabled: test.enabled},
				},
			)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
				strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
			request.Header.Set("Authorization", "Bearer gl-client")
			response := httptest.NewRecorder()
			issued := time.Now()
			engine.ServeHTTP(response, request)

			cooldowns := registry.ModelCooldowns(1, time.Now())
			until, exists := cooldowns["gpt-4o"]
			if !exists || len(cooldowns) != 1 {
				t.Fatalf("model cooldowns = %#v; response = %d %s", cooldowns, response.Code, response.Body.String())
			}
			if elapsed := until.Sub(issued); elapsed < test.want-30*time.Second || elapsed > test.want+30*time.Second {
				t.Fatalf("cooldown = %v from request time, want about %v", elapsed, test.want)
			}
		})
	}
}
