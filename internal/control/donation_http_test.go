package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"gpt-load/internal/platform/config"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/storage/models"
)

const donationTestToken = "synthetic-donation-integration-token-2026"

func donationHTTPTestEngine(t *testing.T, server *Server) *gin.Engine {
	t.Helper()
	initControlI18n(t)
	engine := gin.New()
	registry, err := httproute.NewRegistry(server.HTTPModule(), server.DonationHTTPModule())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind(engine); err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestDonationHTTPAuthenticationIsolationAndSecretProtection(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-http-inventory")
	server := NewServer(&config.Config{AuthKey: "synthetic-admin-key", DonationIntegrationToken: donationTestToken}, fixture.service)
	var logs bytes.Buffer
	server.logger = logrus.New()
	server.logger.SetOutput(&logs)
	engine := donationHTTPTestEngine(t, server)
	const prefix = "/integrations/donations/v1"
	for _, token := range []string{"", "synthetic-admin-key", "synthetic-wrong-token"} {
		response := serveCredentialRequest(t, engine, http.MethodGet, prefix+"/groups", "", token, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("wrong token status = %d", response.Code)
		}
	}
	response := serveCredentialRequest(t, engine, http.MethodGet, "/api/settings", "", donationTestToken, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("integration token gained ordinary API access: %d", response.Code)
	}
	response = serveCredentialRequest(t, engine, http.MethodGet, prefix+"/groups", "", donationTestToken, "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"params"`) || strings.Contains(response.Body.String(), "synthetic-http-inventory") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe directory response: %d %s", response.Code, response.Body.String())
	}
	var groupEnvelope struct {
		Data []DonationGroup `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &groupEnvelope); err != nil {
		t.Fatal(err)
	}
	request := DonationBatchRequest{BatchID: donationTestID(100), GroupID: groupID, TargetRevision: groupEnvelope.Data[0].TargetRevision,
		Items: []DonationItemRequest{{ItemID: donationTestID(101), Key: "synthetic-http-submitted-key"}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response = serveCredentialRequest(t, engine, http.MethodPost, prefix+"/batches", string(body), donationTestToken, "")
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing idempotency status = %d", response.Code)
	}
	response = serveCredentialRequest(t, engine, http.MethodPost, prefix+"/batches", string(body), donationTestToken, request.BatchID)
	if response.Code != http.StatusOK {
		t.Fatalf("create response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), request.Items[0].Key) || strings.Contains(response.Body.String(), donationTestToken) {
		t.Fatal("create response exposed secret")
	}
	changed := request
	changed.Items = []DonationItemRequest{{ItemID: request.Items[0].ItemID, Key: "synthetic-different-key"}}
	changedBody, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	response = serveCredentialRequest(t, engine, http.MethodPost, prefix+"/batches", string(changedBody), donationTestToken, request.BatchID)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), app_errors.ErrIdempotencyKeyReused.Code) {
		t.Fatalf("changed replay = %d %s", response.Code, response.Body.String())
	}
	other := request
	other.BatchID, other.Items = donationTestID(102), []DonationItemRequest{{ItemID: donationTestID(103), Key: "synthetic-other-source-key"}}
	if _, err := fixture.service.ReceiveDonationBatch(t.Context(), donationTestID(199), other); err != nil {
		t.Fatal(err)
	}
	response = serveCredentialRequest(t, engine, http.MethodGet, prefix+"/batches/"+other.BatchID, "", donationTestToken, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("other source was visible: %d", response.Code)
	}
	response = serveCredentialRequest(t, engine, http.MethodGet, prefix+"/batches/"+other.BatchID+"?source_id="+donationTestID(199), "", donationTestToken, "")
	if response.Code != http.StatusBadRequest {
		t.Fatal("source query override accepted")
	}
	response = serveCredentialRequest(t, engine, http.MethodPost, prefix+"/batches/"+request.BatchID+"/retry", `{"item_ids":["`+other.Items[0].ItemID+`"]}`, donationTestToken, donationTestID(104))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("foreign retry target = %d", response.Code)
	}
	for _, secret := range []string{donationTestToken, request.Items[0].Key, changed.Items[0].Key, other.Items[0].Key} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("mutation audit leaked a secret")
		}
	}
}

func TestDonationIdentitySurvivesTokenRotationAndRejectsKeyMaterialChange(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	first, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rotated := donationHTTPTestEngine(t, NewServer(&config.Config{AuthKey: "synthetic-admin-key", DonationIntegrationToken: donationTestToken + "-rotated"}, fixture.service))
	response := serveCredentialRequest(t, rotated, http.MethodGet, "/integrations/donations/v1/capabilities", "", donationTestToken+"-rotated", "")
	var envelope struct {
		Data DonationCapabilities `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || envelope.Data.InstanceID != first.InstanceID || envelope.Data.SourceID != first.SourceID {
		t.Fatalf("rotated identity = %#v", envelope)
	}
	if response := serveCredentialRequest(t, rotated, http.MethodGet, "/integrations/donations/v1/capabilities", "", donationTestToken, ""); response.Code != http.StatusUnauthorized {
		t.Fatal("old integration token still worked")
	}
	if err := fixture.db.Model(&models.DonationIdentity{}).Where("id = ?", 1).Update("key_proof", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.DonationCapabilities(t.Context()); !errors.Is(err, app_errors.ErrDonationUnavailable) {
		t.Fatalf("changed key proof = %v", err)
	}
}

func TestDonationHTTPDisabledWithoutConfigurationAndStrictRequestLimits(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	disabled := donationHTTPTestEngine(t, NewServer(&config.Config{AuthKey: "synthetic-admin-key"}, fixture.service))
	response := serveCredentialRequest(t, disabled, http.MethodGet, "/integrations/donations/v1/capabilities", "", donationTestToken, "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured integration = %d", response.Code)
	}
	engine := donationHTTPTestEngine(t, NewServer(&config.Config{AuthKey: "synthetic-admin-key", DonationIntegrationToken: donationTestToken}, fixture.service))
	for _, body := range []string{`{"source_id":"forged"}`, `{"batch_id":"first","batch_id":"second"}`, `[]`} {
		response := serveCredentialRequest(t, engine, http.MethodPost, "/integrations/donations/v1/batches", body, donationTestToken, donationTestID(200))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid JSON status = %d", response.Code)
		}
	}
	response = serveCredentialRequest(t, engine, http.MethodPost, "/integrations/donations/v1/batches", strings.Repeat("x", donationMaxBodyBytes+1), donationTestToken, donationTestID(200))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request = %d", response.Code)
	}
}
