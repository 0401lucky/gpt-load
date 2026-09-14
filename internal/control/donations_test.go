package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	"gpt-load/internal/storage/models"
)

func TestDonationBatchStagesEncryptedAndProcessesItemsIndependently(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-inventory-key")
	identity, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || !groups[0].CanProbe {
		t.Fatalf("groups = %#v, error = %v", groups, err)
	}
	input := DonationBatchRequest{BatchID: donationTestID(1), GroupID: groupID, TargetRevision: groups[0].TargetRevision,
		Items: []DonationItemRequest{
			{ItemID: donationTestID(2), Key: " synthetic-new-key "},
			{ItemID: donationTestID(3), Key: "synthetic-new-key"},
			{ItemID: donationTestID(4), Key: "synthetic-inventory-key"},
			{ItemID: donationTestID(5), Key: "synthetic-invalid-key"},
			{ItemID: donationTestID(6), Key: "synthetic-rate-limit-key"},
			{ItemID: donationTestID(7), Key: "not a key\nsecond line"},
		},
	}
	executor := &credentialProbeTestExecutor{execute: func(spec execution.AttemptSpec) execution.AttemptResult {
		var value struct {
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal(spec.Credential.Data(), &value); err != nil {
			t.Error(err)
		}
		var inventory int64
		if err := fixture.db.Model(&models.Credential{}).Count(&inventory).Error; err != nil {
			t.Error(err)
		}
		if value.APIKey == "synthetic-new-key" && inventory != 1 {
			t.Errorf("new credential entered inventory before its probe: %d", inventory)
		}
		switch value.APIKey {
		case "synthetic-new-key":
			return successfulCredentialProbeResult()
		case "synthetic-invalid-key":
			return failedCredentialProbeResult(http.StatusUnauthorized, execution.ErrorKindHTTP, execution.FailureHintInvalidCredential)
		case "synthetic-rate-limit-key":
			return failedCredentialProbeResult(http.StatusTooManyRequests, execution.ErrorKindHTTP, execution.FailureHintRateLimited)
		default:
			t.Errorf("unexpected probe for %q", value.APIKey)
			return successfulCredentialProbeResult()
		}
	}}
	fixture.service.executor = executor
	result, err := fixture.service.ReceiveDonationBatch(t.Context(), identity.SourceID, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Items[0].State != "queued" || len(executor.recordedCalls()) != 0 {
		t.Fatalf("intake did not only stage: %#v", result)
	}
	var staged models.DonationItem
	if err := fixture.db.Where("source_id = ? AND item_id = ?", identity.SourceID, input.Items[0].ItemID).Take(&staged).Error; err != nil {
		t.Fatal(err)
	}
	if staged.EncryptedPayload == "" || strings.Contains(staged.EncryptedPayload, "synthetic-new-key") {
		t.Fatal("staged credential is not encrypted")
	}
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err = fixture.service.GetDonationBatch(t.Context(), identity.SourceID, input.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	wantStates := []string{"accepted", "existing", "existing", "invalid", "retry_pending", "invalid"}
	for index, want := range wantStates {
		if result.Items[index].State != want {
			t.Errorf("item %d state = %q, want %q (%s)", index, result.Items[index].State, want, result.Items[index].ReasonCode)
		}
	}
	if result.Items[0].CredentialID == nil || result.Items[0].AcceptedAtMS == nil {
		t.Fatal("accepted item omitted durable credential receipt")
	}
	if len(executor.recordedCalls()) != 3 {
		t.Fatalf("probe count = %d, want only the three new well-formed keys", len(executor.recordedCalls()))
	}
	replayed, err := fixture.service.ReceiveDonationBatch(t.Context(), identity.SourceID, input)
	if err != nil || replayed.Items[0].CredentialID == nil || *replayed.Items[0].CredentialID != *result.Items[0].CredentialID {
		t.Fatalf("replay = %#v, error = %v", replayed, err)
	}
}

func donationTestID(value uint64) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", value)
}

func stageDonationTestBatch(t *testing.T, fixture serviceFixture, groupID uint, sequence uint64, keys ...string) (string, DonationBatchRequest) {
	t.Helper()
	capabilities, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request := DonationBatchRequest{BatchID: donationTestID(sequence), GroupID: groupID}
	for _, group := range groups {
		if group.ID == groupID {
			request.TargetRevision = group.TargetRevision
		}
	}
	for index, key := range keys {
		request.Items = append(request.Items, DonationItemRequest{ItemID: donationTestID(sequence + 1 + uint64(index)), Key: key})
	}
	if _, err := fixture.service.ReceiveDonationBatch(t.Context(), capabilities.SourceID, request); err != nil {
		t.Fatal(err)
	}
	return capabilities.SourceID, request
}

func newDonationTestGroup(t *testing.T, fixture serviceFixture, name, credentials string) uint {
	t.Helper()
	result, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{
		Name: &name, ChannelID: channel.OpenAI, Params: json.RawMessage(`{}`),
		Models:      optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gpt-4o"}}},
		Credentials: credentials, ConnectionType: "api_key", ConfirmSameTarget: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.GroupID
}

func donationTestItem(t *testing.T, fixture serviceFixture, source, itemID string) models.DonationItem {
	t.Helper()
	var item models.DonationItem
	if err := fixture.db.Where("source_id = ? AND item_id = ?", source, itemID).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}
