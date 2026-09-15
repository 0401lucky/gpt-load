package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	bifrostexecutor "gpt-load/internal/execution/bifrost"
	"gpt-load/internal/platform/config"
	"gpt-load/internal/storage/models"
)

type donationWireTestExecutor struct {
	execution.Executor
	streamResult execution.StreamResult
}

func (e *donationWireTestExecutor) ExecuteStream(ctx context.Context, spec execution.AttemptSpec, sink execution.StreamSink) execution.StreamResult {
	e.streamResult = e.Executor.ExecuteStream(ctx, spec, sink)
	return e.streamResult
}

func TestDonationManualChatUsesStagedKeyOnActualHTTPWire(t *testing.T) {
	t.Parallel()
	for _, channelID := range []channel.ID{channel.OpenAICompatible, channel.Gemini} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream_%v", channelID, streaming), func(t *testing.T) {
				t.Parallel()
				fixture := newServiceFixture(t)
				const validKey, invalidKey = "synthetic-review-wire-valid", "synthetic-review-wire-invalid"
				model := "review-text-model"
				var mu sync.Mutex
				seen := map[string]int{}
				upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Error(err)
						return
					}
					key := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
					if channelID == channel.Gemini {
						key = request.Header.Get("X-Goog-Api-Key")
						if key == "" {
							key = request.URL.Query().Get("key")
						}
					}
					mu.Lock()
					seen[key]++
					mu.Unlock()
					if request.Method != http.MethodPost || !bytes.Contains(body, []byte("hello-review")) {
						t.Errorf("chat did not send the selected prompt: method=%s", request.Method)
					}
					if channelID == channel.OpenAICompatible {
						if request.URL.Path != "/v1/chat/completions" || !bytes.Contains(body, []byte(model)) {
							t.Errorf("wrong native model/path: %s", request.URL.Path)
						}
					} else if !strings.Contains(request.URL.Path, "models/"+model+":") {
						t.Errorf("wrong converted model/path: %s", request.URL.Path)
					}
					if key != validKey {
						if key != invalidKey {
							t.Error("chat used a key outside the two submitted staging items")
						}
						writer.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(writer, `{"error":{"code":401,"message":"synthetic unauthorized"}}`)
						return
					}
					if streaming {
						writer.Header().Set("Content-Type", "text/event-stream")
						for _, text := range []string{"中文回复 " + validKey[:10], validKey[10:] + " complete"} {
							if channelID == channel.OpenAICompatible {
								_, _ = writer.Write(sseChatFrame(text))
							} else {
								candidate := map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}}}
								payload, _ := json.Marshal(map[string]any{"candidates": []any{candidate}, "modelVersion": model})
								_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
							}
							writer.(http.Flusher).Flush()
						}
						if channelID == channel.OpenAICompatible {
							_, _ = io.WriteString(writer, "data: [DONE]\n\n")
						} else {
							payload, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
								"index": 0, "content": map[string]any{"role": "model", "parts": []any{}}, "finishReason": "STOP"}},
								"usageMetadata": map[string]any{"promptTokenCount": 7, "candidatesTokenCount": 11, "totalTokenCount": 18}, "modelVersion": model})
							_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
						}
						return
					}
					writer.Header().Set("Content-Type", "application/json")
					if channelID == channel.OpenAICompatible {
						_, _ = writer.Write(successfulChatBody("中文回复 " + validKey + " complete"))
					} else {
						_ = json.NewEncoder(writer).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "中文回复 " + validKey + " complete"}}}, "finishReason": "STOP"}}, "modelVersion": model})
					}
				}))
				t.Cleanup(upstream.Close)
				baseURL := upstream.URL + "/v1"
				if channelID == channel.Gemini {
					baseURL = upstream.URL + "/v1beta"
				}
				params, _ := json.Marshal(map[string]any{"base_url": baseURL})
				name := "manual-wire-group"
				created, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{Name: &name,
					ChannelID: channelID, Params: params, ConnectionType: models.ConnectionTypeAPIKey,
					Models: optionalGroupModels{Set: true, Values: []GroupModel{{ID: model}}}, Credentials: "synthetic-working-inventory"})
				if err != nil {
					t.Fatal(err)
				}
				runtime, err := bifrostexecutor.NewRuntime(t.Context(), fixture.channelRegistry)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(runtime.Shutdown)
				wireExecutor := &donationWireTestExecutor{Executor: runtime}
				fixture.service.executor = wireExecutor
				source, batch, revision := stageDonationReviewBatch(t, fixture, created.GroupID, 3000, validKey, invalidKey)
				for index, item := range batch.Items {
					var output strings.Builder
					testRequest := DonationTestRequest{TestID: donationTestID(uint64(3010 + index)), Actor: "admin-1", ExpectedItemRevision: 1,
						ReviewTargetRevision: revision, Model: model, Prompt: "hello-review", Stream: streaming, MaxOutputTokens: 32}
					result, err := fixture.service.RunDonationTest(t.Context(), source, batch.BatchID, item.ItemID, testRequest,
						func(event donationProjectedEvent) error { output.WriteString(event.Text); return nil }, nil)
					if err != nil {
						t.Fatal(err)
					}
					if index == 0 {
						output.WriteString(result.Text)
						if result.State != donationTestStateSucceeded || !strings.Contains(output.String(), "中文回复 ") || strings.Contains(output.String(), validKey) {
							t.Fatalf("actual HTTP chat failed or exposed key: state=%s, reason=%s, text=%q, execution_error=%#v", result.State, result.ReasonCode, output.String(), wireExecutor.streamResult.Error)
						}
					} else if result.State != donationTestStateFailed {
						t.Fatalf("rejected staged key fell back to inventory: %s", result.State)
					}
					if _, err := fixture.service.RunDonationTest(t.Context(), source, batch.BatchID, item.ItemID, testRequest, nil, nil); err != nil {
						t.Fatal(err)
					}
					if item := donationTestItem(t, fixture, source, item.ItemID); item.State != "pending_review" {
						t.Fatalf("test changed review decision to %s", item.State)
					}
				}
				mu.Lock()
				defer mu.Unlock()
				if len(seen) != 2 || seen[validKey] != 1 || seen[invalidKey] != 1 {
					t.Fatalf("unexpected upstream call counts: %#v", seen)
				}
			})
		}
	}
}

func TestDonationTestHTTPValidatesBeforeSSEAndReplaysMetadata(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := newDonationTestGroup(t, fixture, "test-http-contract", "synthetic-seed")
	source, batch, revision := stageDonationReviewBatch(t, fixture, groupID, 3100, "synthetic-http-review-key")
	executor := &donationChatTestExecutor{streamFrames: [][]byte{sseChatFrame("visible reply"), []byte("data: [DONE]\n\n")}, streamResult: execution.StreamResult{StatusCode: 200}}
	fixture.service.executor = executor
	server := NewServer(&config.Config{AuthKey: "synthetic-admin-key", DonationIntegrationToken: donationTestToken}, fixture.service)
	engine := donationHTTPTestEngine(t, server)
	input := DonationTestRequest{TestID: donationTestID(3110), Actor: "admin-1", ExpectedItemRevision: 1, ReviewTargetRevision: revision, Model: "gpt-4o", Prompt: "hello", Stream: true}
	post := func(input DonationTestRequest) *httptest.ResponseRecorder {
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(http.MethodPost, "/integrations/donations/v1/batches/"+batch.BatchID+"/items/"+batch.Items[0].ItemID+"/tests", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+donationTestToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", input.TestID)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	invalid := input
	invalid.Prompt = ""
	bad := post(invalid)
	if bad.Code != http.StatusBadRequest || strings.HasPrefix(bad.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("invalid request committed SSE: %d %s", bad.Code, bad.Body.String())
	}
	fresh := post(input)
	if fresh.Code != 200 || !strings.Contains(fresh.Body.String(), "event: meta") || !strings.Contains(fresh.Body.String(), "event: delta") || !strings.Contains(fresh.Body.String(), "event: done") {
		t.Fatalf("fresh stream = %d %s", fresh.Code, fresh.Body.String())
	}
	replay := post(input)
	if replay.Code != 200 || !strings.HasPrefix(replay.Header().Get("Content-Type"), "application/json") || strings.Contains(replay.Body.String(), "visible reply") || len(executor.recordedCalls()) != 1 {
		t.Fatalf("replay did not return metadata only: %d %s", replay.Code, replay.Body.String())
	}
	stored, err := fixture.service.GetDonationTest(t.Context(), source, input.TestID)
	if err != nil || stored.FinishedAtMS == 0 {
		t.Fatalf("terminal metadata not committed: %#v, %v", stored, err)
	}
}
