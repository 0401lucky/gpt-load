package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/execution"
	"gpt-load/internal/storage"
	"gpt-load/internal/storage/models"
)

func donationSecondService(t *testing.T, dsn string) serviceFixture {
	t.Helper()
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	return newServiceFixtureWithDatabase(t, db)
}

func TestDonationIndependentDatabaseConnectionsSerializeGlobalQualification(t *testing.T) {
	t.Parallel()
	first, dsn := newFileServiceFixture(t)
	second := donationSecondService(t, dsn)
	firstGroup := newDonationTestGroup(t, first, "first-donation-group", "synthetic-concurrency-first-seed")
	secondGroup := newDonationTestGroup(t, first, "second-donation-group", "synthetic-concurrency-second-seed")
	source, firstRequest := stageDonationTestBatch(t, first, firstGroup, 400, "synthetic-concurrent-donation")
	_, secondRequest := stageDonationTestBatch(t, second, secondGroup, 410, "synthetic-concurrent-donation")
	firstItem := donationTestItem(t, first, source, firstRequest.Items[0].ItemID)
	secondItem := donationTestItem(t, second, source, secondRequest.Items[0].ItemID)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	executor := &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		entered <- struct{}{}
		<-release
		return successfulCredentialProbeResult()
	}}
	first.service.executor, second.service.executor = executor, executor
	done := make(chan error, 1)
	go func() { done <- first.service.processDonationItem(t.Context(), firstItem.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first probe did not start")
	}
	competing := make(chan error, 1)
	go func() { competing <- second.service.processDonationItem(t.Context(), secondItem.ID) }()
	select {
	case err := <-competing:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("competing process did not observe the database reservation")
	}
	if len(executor.recordedCalls()) != 1 {
		t.Fatal("two requests probed the same reserved resource")
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("acceptance did not complete")
	}
	second.service.now = func() time.Time { return time.Now().Add(time.Minute) }
	if err := second.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	accepted := donationTestItem(t, first, source, firstRequest.Items[0].ItemID)
	duplicate := donationTestItem(t, second, source, secondRequest.Items[0].ItemID)
	if accepted.State != "accepted" || duplicate.State != "existing" || duplicate.CredentialID != nil {
		t.Fatalf("competing results = %s/%s", accepted.State, duplicate.State)
	}
	var count int64
	if err := first.db.Model(&models.Credential{}).Where("fingerprint = ?", accepted.CredentialFingerprint).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(executor.recordedCalls()) != 1 {
		t.Fatalf("global acquisition count = %d", count)
	}
}

func TestDonationConcurrentManagementImportWinsWithoutDonationReceipt(t *testing.T) {
	t.Parallel()
	first, dsn := newFileServiceFixture(t)
	second := donationSecondService(t, dsn)
	donationGroup := newDonationTestGroup(t, first, "donation-import-race", "synthetic-import-race-first-seed")
	managementGroup := newDonationTestGroup(t, first, "management-import-race", "synthetic-import-race-second-seed")
	source, request := stageDonationTestBatch(t, first, donationGroup, 420, "synthetic-import-race-key")
	first.service.executor = &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
		result, err := second.service.ImportGroupCredentials(t.Context(), managementGroup, CredentialImportRequest{Credentials: request.Items[0].Key})
		if err != nil || result.CredentialsAdded != 1 {
			t.Errorf("concurrent management import = %#v, %v", result, err)
		}
		return successfulCredentialProbeResult()
	}}
	if err := first.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := donationTestItem(t, first, source, request.Items[0].ItemID)
	if item.State != "existing" || item.CredentialID != nil {
		t.Fatalf("donation stole an inventory acquisition: %#v", item)
	}
	var resource models.DonationResource
	if err := first.db.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if resource.Origin != "inventory" || resource.OwnerItemID != nil || resource.GroupID != managementGroup {
		t.Fatalf("resource origin = %#v", resource)
	}
}

func TestDonationExpiredLeaseFencesOldWorkerAndResumesSameItem(t *testing.T) {
	t.Parallel()
	first, dsn := newFileServiceFixture(t)
	second := donationSecondService(t, dsn)
	groupID := createGroupWithCredentials(t, first, "synthetic-lease-seed")
	source, request := stageDonationTestBatch(t, first, groupID, 430, "synthetic-lease-donation")
	item := donationTestItem(t, first, source, request.Items[0].ItemID)
	old, err := first.service.claimDonationItem(t.Context(), item.ID)
	if err != nil || old == nil {
		t.Fatalf("initial claim = %#v, %v", old, err)
	}
	second.service.now = func() time.Time { return time.Now().Add(2 * donationLeaseDuration) }
	replacement, err := second.service.claimDonationItem(t.Context(), item.ID)
	if err != nil || replacement == nil || replacement.item.LeaseToken == old.item.LeaseToken {
		t.Fatalf("replacement claim = %#v, %v", replacement, err)
	}
	if err := first.service.completeDonationProbe(t.Context(), *old, CredentialProbeOutcomePassed, ""); err != nil {
		t.Fatal(err)
	}
	stillClaimed := donationTestItem(t, first, source, request.Items[0].ItemID)
	if stillClaimed.State != "validating" || stillClaimed.CredentialID != nil || stillClaimed.LeaseToken != replacement.item.LeaseToken {
		t.Fatal("old worker bypassed generation fence")
	}
	if err := second.service.completeDonationProbe(t.Context(), *replacement, CredentialProbeOutcomePassed, ""); err != nil {
		t.Fatal(err)
	}
	if item := donationTestItem(t, second, source, request.Items[0].ItemID); item.State != "accepted" {
		t.Fatalf("replacement state = %s", item.State)
	}
}

func TestDonationCredentialCommitRollbackIsRecoverable(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	groupID := createGroupWithCredentials(t, fixture, "synthetic-rollback-seed")
	source, request := stageDonationTestBatch(t, fixture, groupID, 440, "synthetic-rollback-donation")
	fixture.service.executor = &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
	const hook = "test:donation_insert_failure"
	if err := fixture.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "credentials" {
			_ = tx.AddError(errors.New("synthetic insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			_ = fixture.db.Callback().Create().Remove(hook)
		}
	})
	if err := fixture.service.DrainDonationWork(t.Context()); err == nil {
		t.Fatal("failed transaction was reported successful")
	}
	item := donationTestItem(t, fixture, source, request.Items[0].ItemID)
	if item.CredentialID != nil || item.State != "validating" || item.EncryptedPayload == "" {
		t.Fatalf("rolled back item = %#v", item)
	}
	var resource models.DonationResource
	if err := fixture.db.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if resource.AcquiredAtMS != nil {
		t.Fatal("rollback retained a false permanent acquisition")
	}
	if err := fixture.db.Callback().Create().Remove(hook); err != nil {
		t.Fatal(err)
	}
	removed = true
	fixture.service.now = func() time.Time { return time.Now().Add(2 * donationLeaseDuration) }
	if err := fixture.service.DrainDonationWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if item := donationTestItem(t, fixture, source, request.Items[0].ItemID); item.State != "accepted" {
		t.Fatalf("resumed state = %s", item.State)
	}
}

func TestDonationBackgroundWorkerStopsWithApplicationContext(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	fixture.service.donationsEnabled = true
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { fixture.service.RunDonationRecovery(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("donation worker ignored shutdown")
	}
}
