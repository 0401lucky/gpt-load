package control

import (
	"errors"
	"testing"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage/models"
)

func TestDonationCatalogAllowsEmptyGroupAndPreservesInventoryHistory(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-catalog-inventory")
	var credential models.Credential
	if err := fixture.db.Where("group_id = ?", groupID).Take(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DeleteGroupCredential(t.Context(), groupID, credential.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || !groups[0].CanProbe || groups[0].UnavailableReason != "" {
		t.Fatalf("empty group = %#v, %v", groups, err)
	}
	source, request := stageDonationTestBatch(t, fixture, groupID, 500, "synthetic-catalog-inventory")
	executor := &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
	fixture.service.executor = executor
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if item := donationTestItem(t, fixture, source, request.Items[0].ItemID); item.State != "existing" || len(executor.recordedCalls()) != 0 {
		t.Fatalf("deleted inventory state = %q", item.State)
	}
	if _, err := fixture.service.UpdateGroupSettings(t.Context(), groupID, GroupSettingsUpdateRequest{Enabled: optionalField[bool]{Set: true, Value: false}}); err != nil {
		t.Fatal(err)
	}
	groups, err = fixture.service.ListDonationGroups(t.Context())
	if err != nil || groups[0].CanProbe || groups[0].UnavailableReason != "group_disabled" {
		t.Fatalf("disabled group = %#v, %v", groups, err)
	}
	request.BatchID, request.Items = donationTestID(510), []DonationItemRequest{{ItemID: donationTestID(511), Key: "synthetic-disabled-target-key"}}
	_, err = fixture.service.ReceiveDonationBatch(t.Context(), source, request)
	assertAPIErrorCode(t, err, app_errors.ErrDonationTargetUnavailable.Code)
}

func TestDonationIntegrationTokenCannotBecomeAnAccessKey(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	fixture.service.donationTokenFingerprint = fixture.encryption.Hash(donationTestToken)
	if _, err := fixture.service.prepareAccessKeyCredential(donationTestToken); !errors.Is(err, app_errors.ErrInvalidCustomAccessKey) {
		t.Fatalf("integration token collision = %v", err)
	}
}
