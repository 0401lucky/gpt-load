package control

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"gpt-load/internal/channel"
	bifrostexecutor "gpt-load/internal/execution/bifrost"
	"gpt-load/internal/storage/models"
)

// This test uses the production executor and a real local Gemini HTTP server.
// Inventory contains a working key, so a credential fallback would be observable.
func TestDonationGeminiProbeUsesSubmittedKeyOnActualHTTPWire(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	var mu sync.Mutex
	seen := make(map[string]int)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := request.Header.Get("X-Goog-Api-Key")
		if key == "" {
			key = request.URL.Query().Get("key")
		}
		mu.Lock()
		seen[key]++
		mu.Unlock()
		if request.Method != http.MethodPost || request.URL.Path != "/v1beta/models/gemini-donation-test:generateContent" {
			t.Errorf("unexpected Gemini probe target: %s %s", request.Method, request.URL.Path)
		}
		var inventory int64
		if err := fixture.db.Model(&models.Credential{}).Where("fingerprint = ?", fixture.encryption.Hash(`{"api_key":"`+key+`"}`)).Count(&inventory).Error; err != nil {
			t.Error(err)
		}
		if inventory != 0 {
			t.Errorf("submitted key entered inventory before probe")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch key {
		case "synthetic-gemini-donation-valid":
			_, _ = io.WriteString(writer, `{"candidates":[{"content":{"role":"model","parts":[{"text":"pong"}]},"finishReason":"STOP"}],"modelVersion":"gemini-donation-test"}`)
		case "synthetic-gemini-donation-invalid":
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"error":{"code":401,"status":"UNAUTHENTICATED","message":"synthetic-gemini-donation-invalid"}}`)
		default:
			t.Errorf("probe used a key other than either submitted key")
			writer.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(upstream.Close)
	params, err := json.Marshal(map[string]string{"base_url": upstream.URL + "/v1beta"})
	if err != nil {
		t.Fatal(err)
	}
	name := "gemini-donation-wire"
	group, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{Name: &name, ChannelID: channel.Gemini,
		Params: params, Models: optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gemini-donation-test"}}},
		Credentials: "synthetic-gemini-working-inventory", ConnectionType: models.ConnectionTypeAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := bifrostexecutor.NewRuntime(t.Context(), fixture.channelRegistry)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Shutdown)
	fixture.service.executor = runtime
	source, request := stageDonationTestBatch(t, fixture, group.GroupID, 300,
		"synthetic-gemini-donation-valid", "synthetic-gemini-donation-invalid")
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Items[0].State != "accepted" || result.Items[1].State != "invalid" {
		t.Fatalf("wire probe results = %#v", result.Items)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen["synthetic-gemini-donation-valid"] != 1 || seen["synthetic-gemini-donation-invalid"] != 1 {
		t.Fatalf("wire probe counts = %#v", seen)
	}
}
