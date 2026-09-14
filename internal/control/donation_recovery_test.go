package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
)

func TestDonationWaitsForControlOperationRecovery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		committed bool
	}{
		{name: "before_credential_commit"},
		{name: "before_runtime_publication", committed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newServiceFixture(t)
			groupID := newDonationTestGroup(t, fixture, "donation-recovery-barrier", "synthetic-donation-barrier-seed")
			source, request := stageDonationTestBatch(t, fixture, groupID, 90, "synthetic-donation-barrier-key")
			fixture.service.executor = &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
			if test.committed {
				fixture.manager.SetSnapshotReconciler(donationFailingSnapshotReconciler{})
				if err := fixture.service.DrainDonationWork(t.Context()); !errors.Is(err, app_errors.ErrControlOperationIncomplete) {
					t.Fatalf("stage committing receipt error = %v", err)
				}
				fixture.manager.SetSnapshotReconciler(controlAccessQuotaReconciler{runtime: fixture.accessQuota})
			}

			fixture.service.reconcileRegistryGroup = func(uint, []state.CredentialEntry) (bool, error) {
				return false, errors.New("synthetic older operation recovery failure")
			}
			name := "pending-control-operation"
			_, err := fixture.service.CreateGroupIdempotent(t.Context(), donationTestID(99), GroupCreateRequest{
				Name: &name, ChannelID: channel.OpenAI, Params: json.RawMessage(`{}`),
				Models:      optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gpt-4o"}}},
				Credentials: "synthetic-pending-operation-key", ConnectionType: "api_key", ConfirmSameTarget: true,
			})
			assertAPIErrorCode(t, err, app_errors.ErrControlOperationIncomplete.Code)
			fixture.service.now = func() time.Time { return time.Now().Add(time.Minute) }
			err = fixture.service.DrainDonationWork(t.Context())
			assertAPIErrorCode(t, err, app_errors.ErrControlRecoveryPending.Code)
			item := donationTestItem(t, fixture, source, request.Items[0].ItemID)
			wantState := "validating"
			if test.committed {
				wantState = "committing"
			}
			if item.State != wantState || (!test.committed && item.CredentialID != nil) {
				t.Fatalf("donation crossed pending recovery barrier: %#v", item)
			}

			fixture.service.reconcileRegistryGroup = fixture.registry.ReconcileGroup
			fixture.service.now = func() time.Time { return time.Now().Add(3 * donationLeaseDuration) }
			if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
				t.Fatalf("resume donation after older recovery: %v", err)
			}
			item = donationTestItem(t, fixture, source, request.Items[0].ItemID)
			if item.State != "accepted" || item.CredentialID == nil {
				t.Fatalf("recovered donation = %#v", item)
			}
			var operation models.ControlOperation
			if err := fixture.db.Where("idempotency_key = ?", donationTestID(99)).Take(&operation).Error; err != nil {
				t.Fatal(err)
			}
			if operation.CompletedAtMS == nil {
				t.Fatal("donation completed before the older control operation")
			}
		})
	}
}

func TestDonationRecoversCommittedReceiptAndKeepsHistoryAfterDeletion(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-recovery-seed")
	source, request := stageDonationTestBatch(t, fixture, groupID, 20, "synthetic-recovery-donation")
	executor := &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
	fixture.service.executor = executor
	fixture.manager.SetSnapshotReconciler(donationFailingSnapshotReconciler{})
	if err := fixture.service.DrainDonationWork(t.Context()); !errors.Is(err, app_errors.ErrControlOperationIncomplete) {
		t.Fatalf("drain error = %v, want durable runtime recovery failure", err)
	}
	persisted := donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if persisted.State != "committing" || persisted.CredentialID == nil || persisted.AcceptedAtMS == nil || persisted.EncryptedPayload != "" {
		t.Fatalf("committed receipt = %#v", persisted)
	}
	before, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID)
	if err != nil || before.Items[0].State != "committing" || before.Items[0].CredentialID != nil {
		t.Fatalf("premature accepted response = %#v, error = %v", before, err)
	}
	restarted := newServiceFixtureWithDatabase(t, fixture.db)
	restarted.service.now = func() time.Time { return time.Now().Add(time.Minute) }
	restartedExecutor := &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
	restarted.service.executor = restartedExecutor
	if err := restarted.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, err := restarted.service.GetDonationBatch(t.Context(), source, request.BatchID)
	if err != nil || receipt.Items[0].State != "accepted" || receipt.Items[0].CredentialID == nil || *receipt.Items[0].CredentialID != *persisted.CredentialID ||
		receipt.Items[0].AcceptedAtMS == nil || *receipt.Items[0].AcceptedAtMS != *persisted.AcceptedAtMS {
		t.Fatalf("recovered receipt = %#v, error = %v", receipt, err)
	}
	if len(restartedExecutor.recordedCalls()) != 0 {
		t.Fatal("restart probed an already committed key")
	}
	if err := restarted.service.DeleteGroupCredential(t.Context(), groupID, *persisted.CredentialID); err != nil {
		t.Fatal(err)
	}
	if err := restarted.service.DeleteGroup(t.Context(), groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.service.CompactCompletedOperations(t.Context(), time.Now().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	otherGroup := newDonationTestGroup(t, restarted, "recovery-other-group", "synthetic-recovery-other-seed")
	_, duplicate := stageDonationTestBatch(t, restarted, otherGroup, 30, request.Items[0].Key)
	if err := restarted.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := restarted.service.GetDonationBatch(t.Context(), source, duplicate.BatchID)
	if err != nil || result.Items[0].State != "existing" || result.Items[0].CredentialID != nil {
		t.Fatalf("deleted-key retry = %#v, error = %v", result, err)
	}
	original, err := restarted.service.ReceiveDonationBatch(t.Context(), source, request)
	if err != nil || original.Items[0].CredentialID == nil || *original.Items[0].CredentialID != *persisted.CredentialID {
		t.Fatalf("original replay after deletion = %#v, error = %v", original, err)
	}
	if len(restartedExecutor.recordedCalls()) != 0 {
		t.Fatal("historically acquired key was probed again")
	}
}

func TestDonationRetryIsDurableBoundedAndDoesNotRequireKey(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	now := time.Now()
	fixture.service.now = func() time.Time { return now }
	groupID := createGroupWithCredentials(t, fixture, "synthetic-retry-seed")
	source, request := stageDonationTestBatch(t, fixture, groupID, 40, "synthetic-retry-donation")
	executor := &credentialProbeTestExecutor{result: failedCredentialProbeResult(http.StatusTooManyRequests, execution.ErrorKindHTTP, execution.FailureHintRateLimited)}
	fixture.service.executor = executor
	exhaust := func() {
		for range donationMaxAttempts {
			now = now.Add(time.Minute)
			if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}
	exhaust()
	item := donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.State != "retry_pending" || item.ReasonCode != "retry_exhausted" || item.EncryptedPayload == "" || item.Attempts != donationMaxAttempts {
		t.Fatalf("exhausted item = %#v", item)
	}
	for range 2 {
		if _, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID); err != nil {
			t.Fatal(err)
		}
		if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(executor.recordedCalls()) != donationMaxAttempts {
		t.Fatal("GET or exhausted queue retried a probe")
	}
	action := donationTestID(50)
	retry := DonationRetryRequest{ItemIDs: []string{request.Items[0].ItemID}}
	if _, err := fixture.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, retry); err != nil {
		t.Fatal(err)
	}
	exhaust()
	if _, err := fixture.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, retry); err != nil {
		t.Fatal(err)
	}
	item = donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.Attempts != donationMaxAttempts || item.State != "retry_pending" {
		t.Fatal("replayed action reset the later probe budget")
	}
	if _, err := fixture.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, DonationRetryRequest{}); !errors.Is(err, app_errors.ErrIdempotencyKeyReused) {
		t.Fatalf("changed retry action = %v", err)
	}
	executor.result = successfulCredentialProbeResult()
	if _, err := fixture.service.RetryDonationBatch(t.Context(), source, request.BatchID, donationTestID(51), retry); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	item = donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.State != "accepted" || item.CredentialID == nil {
		t.Fatalf("retry result = %#v", item)
	}
	if _, err := fixture.service.RetryDonationBatch(t.Context(), source, request.BatchID, donationTestID(52), retry); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(executor.recordedCalls()) != 2*donationMaxAttempts+1 {
		t.Fatal("accepted item was probed again")
	}
}

type donationFailingSnapshotReconciler struct{}

func (donationFailingSnapshotReconciler) ReconcileConfigSnapshot(*state.ConfigSnapshot) error {
	return errors.New("synthetic publication failure")
}

func TestDonationStagingExpiryClearsOnlyTemporaryCiphertext(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-expiry-seed")
	source, request := stageDonationTestBatch(t, fixture, groupID, 60, "synthetic-expiring-donation")
	fixture.service.now = func() time.Time { return time.Now().Add(donationStagingTTL + time.Hour) }
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.State != "invalid" || item.ReasonCode != "staging_expired" || item.EncryptedPayload != "" {
		t.Fatalf("expired item = %#v", item)
	}
	if _, err := fixture.service.GetDonationBatch(t.Context(), source, request.BatchID); err != nil {
		t.Fatal(err)
	}
	var resource models.DonationResource
	if err := fixture.db.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if resource.AcquiredAtMS != nil || resource.OwnerItemID != nil {
		t.Fatal("unaccepted expired item retained permanent eligibility ownership")
	}
}

func TestDonationTargetChangesRequireAnotherProbeAndNamesDoNotChangeRevision(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-target-seed")
	source, request := stageDonationTestBatch(t, fixture, groupID, 70, "synthetic-target-donation")
	if _, err := fixture.service.UpdateGroupSettings(t.Context(), groupID, GroupSettingsUpdateRequest{Name: optionalField[string]{Set: true, Value: "renamed-donation-group"}}); err != nil {
		t.Fatal(err)
	}
	groups, err := fixture.service.ListDonationGroups(t.Context())
	if err != nil || groups[0].TargetRevision != request.TargetRevision {
		t.Fatalf("name changed revision: %#v, %v", groups, err)
	}
	executor := &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		if _, err := fixture.service.UpdateGroupSettings(t.Context(), groupID, GroupSettingsUpdateRequest{ValidationModel: optionalField[string]{Set: true, Value: "new-validation-model"}}); err != nil {
			t.Error(err)
		}
		return successfulCredentialProbeResult()
	}}
	fixture.service.executor = executor
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.State != "retry_pending" || item.ReasonCode != "target_changed" || item.CredentialID != nil {
		t.Fatalf("stale probe accepted: %#v", item)
	}
	stale := request
	stale.BatchID, stale.Items = donationTestID(80), []DonationItemRequest{{ItemID: donationTestID(81), Key: "synthetic-stale-target"}}
	if _, err := fixture.service.ReceiveDonationBatch(t.Context(), source, stale); !errors.Is(err, app_errors.ErrDonationTargetChanged) {
		t.Fatalf("stale request error = %v", err)
	}
	fixture.service.now = func() time.Time { return time.Now().Add(time.Minute) }
	executor.execute, executor.result = nil, successfulCredentialProbeResult()
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	item = donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.State != "accepted" || len(executor.recordedCalls()) != 2 || executor.recordedCalls()[1].UpstreamModel != "new-validation-model" {
		t.Fatalf("revalidated item = %#v", item)
	}
}
