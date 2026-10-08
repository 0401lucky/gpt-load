package control

import (
	"errors"
	"testing"

	"gpt-load/internal/outboundproxy"
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

func TestDonationCatalogResolvesManagedProxyAndBindsEffectiveTarget(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-managed-proxy-inventory")
	const firstURL = "http://first-proxy.example:8080"
	proxy, err := fixture.service.SaveProxy(t.Context(), 0, ProxySaveRequest{Name: "donation proxy", URL: firstURL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.UpdateGroupSettings(t.Context(), groupID, GroupSettingsUpdateRequest{
		Proxy: optionalField[outboundproxy.Config]{Set: true, Value: outboundproxy.Config{Mode: outboundproxy.ModeCustom, ProxyID: proxy.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || !groups[0].CanProbe || !groups[0].CanManualReview {
		t.Fatalf("managed proxy donation catalog = %#v, %v", groups, err)
	}
	original := groups[0]
	target, reason, err := fixture.service.loadDonationTarget(t.Context(), fixture.db, groupID)
	if err != nil || reason != "" || target.group.Proxy.Config.URL != firstURL || target.group.Proxy.Config.ProxyID != 0 {
		t.Fatalf("managed proxy target was not resolved: reason=%q, error=%v", reason, err)
	}
	executor := &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
	fixture.service.executor = executor
	source, request := stageDonationTestBatch(t, fixture, groupID, 700, "synthetic-managed-proxy-donation")
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if item := donationTestItem(t, fixture, source, request.Items[0].ItemID); item.State != "accepted" {
		t.Fatalf("managed proxy donation state = %q", item.State)
	}
	if calls := executor.recordedCalls(); len(calls) != 1 || calls[0].Proxy.Config.URL != firstURL {
		t.Fatal("donation probe did not use the resolved proxy")
	}
	if _, err := fixture.service.SaveProxy(t.Context(), proxy.ID, ProxySaveRequest{Name: "renamed proxy"}); err != nil {
		t.Fatal(err)
	}
	groups, err = fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].TargetRevision != original.TargetRevision || groups[0].ManualTargetRevision != original.ManualTargetRevision {
		t.Fatalf("proxy rename changed frozen donation targets: %#v, %v", groups, err)
	}
	if _, err := fixture.service.SaveProxy(t.Context(), proxy.ID, ProxySaveRequest{Name: "renamed proxy", URL: "http://second-proxy.example:8080"}); err != nil {
		t.Fatal(err)
	}
	groups, err = fixture.service.ListDonationGroups(t.Context())
	if err != nil || len(groups) != 1 || !groups[0].CanProbe || !groups[0].CanManualReview ||
		groups[0].TargetRevision == original.TargetRevision || groups[0].ManualTargetRevision == original.ManualTargetRevision {
		t.Fatalf("proxy edit failed to change donation targets: %#v, %v", groups, err)
	}
	request.BatchID = donationTestID(710)
	request.Items = []DonationItemRequest{{ItemID: donationTestID(711), Key: "synthetic-stale-proxy-donation"}}
	_, err = fixture.service.ReceiveDonationBatch(t.Context(), source, request)
	assertAPIErrorCode(t, err, app_errors.ErrDonationTargetChanged.Code)
	if err := fixture.service.BatchProxies(t.Context(), ProxyBatchRequest{IDs: []uint{proxy.ID}, Action: "disable"}); err != nil {
		t.Fatal(err)
	}
	target, reason, err = fixture.service.loadDonationTarget(t.Context(), fixture.db, groupID)
	if err != nil || reason != "" || target.group.Proxy.Config.URL != "" || target.group.Proxy.Config.Mode != outboundproxy.ModeDirect {
		t.Fatalf("disabled proxy did not use inherited direct target: reason=%q, error=%v", reason, err)
	}
}
