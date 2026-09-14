package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/execution"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage"
	migrationfiles "gpt-load/internal/storage/migrations"
	"gpt-load/internal/storage/models"
)

// 不标记 t.Parallel()：外部数据库测试显式 opt-in，使用独立的临时库/schema。
func TestExternalDatabaseDonationMigrationBackfillsInventory(t *testing.T) {
	fixture, dsn := newExternalDonationFixture(t)
	assertExternalDonationMigration(t, fixture.db)
	if err := storage.AutoMigrate(fixture.db); err != nil {
		t.Fatalf("repeat fresh migration: %v", err)
	}

	firstGroup := newDonationTestGroup(t, fixture, "external-donation-legacy-first", "synthetic-external-legacy-shared")
	newDonationTestGroup(t, fixture, "external-donation-legacy-second", "synthetic-external-legacy-shared\nsynthetic-external-legacy-other")
	var beforeGroups []models.Group
	var beforeCredentials []models.Credential
	if err := fixture.db.Order("id ASC").Find(&beforeGroups).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Order("id ASC").Find(&beforeCredentials).Error; err != nil {
		t.Fatal(err)
	}
	if len(beforeCredentials) != 3 {
		t.Fatalf("legacy inventory count = %d, want 3", len(beforeCredentials))
	}

	// 只在本测试新建的隔离库移除增量 0015，重建带库存的 0014 升级起点。
	// 其余 schema 来自真实迁移执行器，不复制历史 DDL，也不修改库存行。
	for _, table := range migrationfiles.TableNames0015() {
		if err := fixture.db.Migrator().DropTable(table); err != nil {
			t.Fatalf("prepare pre-donation schema: %v", err)
		}
	}
	if err := fixture.db.Exec("DELETE FROM schema_migrations WHERE id = ?", migrationfiles.ID0015).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.AutoMigrate(fixture.db); err != nil {
		t.Fatalf("upgrade populated inventory to donation schema: %v", err)
	}
	assertExternalDonationMigration(t, fixture.db)
	assertExternalDonationCount(t, fixture.db.Model(&models.DonationResource{}), 0)

	if err := fixture.service.EnsureInitialState(t.Context()); err != nil {
		t.Fatalf("bootstrap actual inventory backfill: %v", err)
	}
	identity, err := fixture.service.DonationCapabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := fixture.service.EnsureInitialState(t.Context()); err != nil {
			t.Fatalf("repeat inventory bootstrap: %v", err)
		}
	}
	assertExternalDonationCount(t, fixture.db.Model(&models.DonationResource{}), 2)
	var resource models.DonationResource
	if err := fixture.db.Where("fingerprint = ?", fixture.encryption.Hash(donationFingerprintDomain+"synthetic-external-legacy-shared")).Take(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if resource.Origin != "inventory" || resource.OwnerItemID != nil || resource.GroupID != firstGroup ||
		resource.CredentialID == nil || *resource.CredentialID != beforeCredentials[0].ID ||
		resource.AcquiredAtMS == nil || *resource.AcquiredAtMS != beforeCredentials[0].CreatedAtMS {
		t.Fatalf("backfilled shared inventory identity = %#v", resource)
	}

	restarted := donationSecondService(t, dsn)
	if err := storage.AutoMigrate(restarted.db); err != nil {
		t.Fatalf("migrate reopened database: %v", err)
	}
	if err := restarted.service.EnsureInitialState(t.Context()); err != nil {
		t.Fatalf("bootstrap reopened database: %v", err)
	}
	reopenedIdentity, err := restarted.service.DonationCapabilities(t.Context())
	if err != nil || reopenedIdentity.InstanceID != identity.InstanceID || reopenedIdentity.SourceID != identity.SourceID {
		t.Fatalf("identity changed after reopening: %#v, error = %v", reopenedIdentity, err)
	}
	var afterGroups []models.Group
	var afterCredentials []models.Credential
	if err := restarted.db.Order("id ASC").Find(&afterGroups).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.db.Order("id ASC").Find(&afterCredentials).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeGroups, afterGroups) || !reflect.DeepEqual(beforeCredentials, afterCredentials) {
		t.Fatal("migration/bootstrap changed existing groups or encrypted inventory")
	}
	assertExternalDonationCount(t, restarted.db.Model(&models.DonationResource{}), 2)
	assertExternalDonationMigration(t, restarted.db)
}

func TestExternalDatabaseDonationIntakeTransactions(t *testing.T) {
	fixture, dsn := newExternalDonationFixture(t)
	if err := fixture.service.EnsureInitialState(t.Context()); err != nil {
		t.Fatalf("bootstrap fresh donation database: %v", err)
	}

	t.Run("receipt_recovery_and_retained_history", func(t *testing.T) {
		first := newServiceFixtureWithDatabase(t, fixture.db)
		groupID := newExternalDonationEmptyGroup(t, first, "external-donation-receipt")
		otherGroup := newDonationTestGroup(t, first, "external-donation-receipt-other", "synthetic-external-existing-inventory")
		const key = "synthetic-external-received-key"
		source, request := stageDonationTestBatch(t, first, groupID, 1000, key)
		staged := donationTestItem(t, first, source, request.Items[0].ItemID)
		if staged.State != "queued" || staged.EncryptedPayload == "" || strings.Contains(staged.EncryptedPayload, key) {
			t.Fatal("request did not persist encrypted staging")
		}
		executor := &credentialProbeTestExecutor{execute: func(spec execution.AttemptSpec) execution.AttemptResult {
			var credential struct {
				APIKey string `json:"api_key"`
			}
			if err := json.Unmarshal(spec.Credential.Data(), &credential); err != nil {
				t.Error(err)
			}
			if credential.APIKey != key {
				t.Error("probe did not use the submitted synthetic key")
			}
			var count int64
			if err := first.db.Model(&models.Credential{}).Where("fingerprint = ?", staged.CredentialFingerprint).Count(&count).Error; err != nil {
				t.Error(err)
			}
			if count != 0 || len(first.registry.CaptureActiveCredentialRefs([]uint{groupID})) != 0 {
				t.Error("unvalidated donation entered live inventory")
			}
			return successfulCredentialProbeResult()
		}}
		first.service.executor = executor
		first.manager.SetSnapshotReconciler(donationFailingSnapshotReconciler{})
		if err := first.service.DrainDonationWork(t.Context()); !errors.Is(err, app_errors.ErrControlOperationIncomplete) {
			t.Fatalf("failed runtime publication error = %v", err)
		}
		committed := donationTestItem(t, first, source, request.Items[0].ItemID)
		if committed.State != "committing" || committed.CredentialID == nil || committed.AcceptedAtMS == nil || committed.EncryptedPayload != "" {
			t.Fatalf("committed receipt = %#v", committed)
		}
		pending, err := first.service.GetDonationBatch(t.Context(), source, request.BatchID)
		if err != nil || len(pending.Items) != 1 || pending.Items[0].State != "committing" || pending.Items[0].CredentialID != nil {
			t.Fatalf("premature accepted receipt = %#v, error = %v", pending, err)
		}

		restarted := donationSecondService(t, dsn)
		restarted.service.now = func() time.Time { return time.Now().Add(time.Minute) }
		restartedExecutor := &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
		restarted.service.executor = restartedExecutor
		if err := restarted.service.EnsureInitialState(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := restarted.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		receipt, err := restarted.service.GetDonationBatch(t.Context(), source, request.BatchID)
		if err != nil || len(receipt.Items) != 1 || receipt.Items[0].State != "accepted" ||
			receipt.Items[0].CredentialID == nil || *receipt.Items[0].CredentialID != *committed.CredentialID ||
			receipt.Items[0].AcceptedAtMS == nil || *receipt.Items[0].AcceptedAtMS != *committed.AcceptedAtMS {
			t.Fatalf("recovered receipt = %#v, error = %v", receipt, err)
		}
		if _, ok := restarted.registry.CredentialRef(*committed.CredentialID); !ok {
			t.Fatal("accepted credential is absent from recovered runtime")
		}

		_, duplicateRequest := stageDonationTestBatch(t, restarted, otherGroup, 1010, key, "synthetic-external-existing-inventory")
		if err := restarted.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		for _, input := range duplicateRequest.Items {
			item := donationTestItem(t, restarted, source, input.ItemID)
			if item.State != "existing" || item.CredentialID != nil || item.AcceptedAtMS != nil {
				t.Fatalf("existing inventory received new qualification: %#v", item)
			}
		}
		if err := restarted.service.DeleteGroupCredential(t.Context(), groupID, *committed.CredentialID); err != nil {
			t.Fatal(err)
		}
		if err := restarted.service.DeleteGroup(t.Context(), groupID); err != nil {
			t.Fatal(err)
		}
		if _, err := restarted.service.CompactCompletedOperations(t.Context(), time.Now().Add(365*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := storage.AutoMigrate(restarted.db); err != nil {
			t.Fatal(err)
		}
		if err := restarted.service.EnsureInitialState(t.Context()); err != nil {
			t.Fatal(err)
		}
		_, afterDeletion := stageDonationTestBatch(t, restarted, otherGroup, 1020, key)
		if err := restarted.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		if item := donationTestItem(t, restarted, source, afterDeletion.Items[0].ItemID); item.State != "existing" || item.CredentialID != nil {
			t.Fatalf("deleted key received new qualification: %#v", item)
		}
		replayed, err := restarted.service.ReceiveDonationBatch(t.Context(), source, request)
		if err != nil || !reflect.DeepEqual(receipt, replayed) {
			t.Fatalf("receipt changed after deletion/replay: %#v, error = %v", replayed, err)
		}
		var resource models.DonationResource
		if err := restarted.db.Where("fingerprint = ?", committed.Fingerprint).Take(&resource).Error; err != nil {
			t.Fatal(err)
		}
		if resource.Origin != "donation" || resource.OwnerItemID == nil || *resource.OwnerItemID != committed.ID ||
			resource.CredentialID == nil || *resource.CredentialID != *committed.CredentialID ||
			resource.AcquiredAtMS == nil || *resource.AcquiredAtMS != *committed.AcceptedAtMS {
			t.Fatal("deletion/bootstrap changed permanent acquisition history")
		}
		assertExternalDonationCount(t, restarted.db.Model(&models.Credential{}).Where("fingerprint = ?", committed.CredentialFingerprint), 0)
		if len(executor.recordedCalls()) != 1 || len(restartedExecutor.recordedCalls()) != 0 {
			t.Fatal("recovery or duplicate detection probed an acquired key again")
		}
	})

	t.Run("concurrent_batch_replay_and_item_conflict_rollback", func(t *testing.T) {
		first := newServiceFixtureWithDatabase(t, fixture.db)
		second := donationSecondService(t, dsn)
		groupID := newExternalDonationEmptyGroup(t, first, "external-donation-replay")
		identity, err := first.service.DonationCapabilities(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		groups, err := first.service.ListDonationGroups(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		request := DonationBatchRequest{BatchID: donationTestID(2000), GroupID: groupID,
			Items: []DonationItemRequest{{ItemID: donationTestID(2001), Key: "synthetic-external-replay"}}}
		for _, group := range groups {
			if group.ID == groupID {
				request.TargetRevision = group.TargetRevision
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		t.Cleanup(cancel)
		missing, release := make(chan struct{}, 2), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(unblock)
		for _, peer := range []serviceFixture{first, second} {
			var once sync.Once
			const hook = "test:external_donation_batch_missing"
			if err := peer.db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
				if tx.Statement.Table == "donation_batches" && tx.RowsAffected == 0 &&
					(tx.Error == nil || errors.Is(tx.Error, gorm.ErrRecordNotFound)) {
					once.Do(func() {
						missing <- struct{}{}
						select {
						case <-release:
						case <-ctx.Done():
						}
					})
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := peer.db.Callback().Query().Remove(hook); err != nil {
					t.Errorf("remove batch barrier: %v", err)
				}
			})
		}
		type intakeResult struct {
			response DonationBatchResponse
			err      error
		}
		results := make(chan intakeResult, 2)
		var workers sync.WaitGroup
		for _, peer := range []serviceFixture{first, second} {
			workers.Go(func() {
				response, err := peer.service.ReceiveDonationBatch(ctx, identity.SourceID, request)
				results <- intakeResult{response, err}
			})
		}
		t.Cleanup(func() { unblock(); cancel(); workers.Wait() })
		for range 2 {
			select {
			case <-missing:
			case <-ctx.Done():
				t.Fatal("independent transactions did not both observe the missing batch")
			}
		}
		unblock()
		for range 2 {
			select {
			case result := <-results:
				if result.err != nil || len(result.response.Items) != 1 || result.response.Items[0].State != "queued" {
					t.Fatalf("concurrent replay = %#v, error = %v", result.response, result.err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent batch intake did not finish")
			}
		}
		assertExternalDonationCount(t, first.db.Model(&models.DonationBatch{}).Where("source_id = ? AND batch_id = ?", identity.SourceID, request.BatchID), 1)
		assertExternalDonationCount(t, first.db.Model(&models.DonationItem{}).Where("source_id = ? AND batch_id = ?", identity.SourceID, request.BatchID), 1)
		conflict := request
		conflict.BatchID = donationTestID(2010)
		if _, err := second.service.ReceiveDonationBatch(t.Context(), identity.SourceID, conflict); !errors.Is(err, app_errors.ErrIdempotencyKeyReused) {
			t.Fatalf("reuse item in another batch error = %v", err)
		}
		assertExternalDonationCount(t, first.db.Model(&models.DonationBatch{}).Where("source_id = ? AND batch_id = ?", identity.SourceID, conflict.BatchID), 0)
		conflict = request
		conflict.Items = []DonationItemRequest{{ItemID: request.Items[0].ItemID, Key: "synthetic-external-replay-changed"}}
		if _, err := second.service.ReceiveDonationBatch(t.Context(), identity.SourceID, conflict); !errors.Is(err, app_errors.ErrIdempotencyKeyReused) {
			t.Fatalf("change replay content error = %v", err)
		}
		first.service.executor = &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
		if err := first.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("retry_action_replay_preserves_consumed_budget", func(t *testing.T) {
		first := newServiceFixtureWithDatabase(t, fixture.db)
		second := donationSecondService(t, dsn)
		now := time.Now()
		first.service.now = func() time.Time { return now }
		second.service.now = func() time.Time { return now }
		groupID := newExternalDonationEmptyGroup(t, first, "external-donation-retry-action")
		source, request := stageDonationTestBatch(t, first, groupID, 6000, "synthetic-external-retry-action")
		executor := &credentialProbeTestExecutor{result: failedCredentialProbeResult(http.StatusTooManyRequests, execution.ErrorKindHTTP, execution.FailureHintRateLimited)}
		first.service.executor = executor
		for range donationMaxAttempts {
			now = now.Add(time.Minute)
			if err := first.service.DrainDonationWork(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		exhausted := donationTestItem(t, second, source, request.Items[0].ItemID)
		if exhausted.State != "retry_pending" || exhausted.ReasonCode != "retry_exhausted" || exhausted.Attempts != donationMaxAttempts {
			t.Fatalf("exhausted retry item = %#v", exhausted)
		}
		action := donationTestID(6010)
		retry := DonationRetryRequest{ItemIDs: []string{request.Items[0].ItemID}}
		if _, err := first.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, retry); err != nil {
			t.Fatal(err)
		}
		if err := first.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		consumed := donationTestItem(t, second, source, request.Items[0].ItemID)
		if consumed.State != "retry_pending" || consumed.Attempts != 1 {
			t.Fatalf("resumed item = %#v", consumed)
		}
		if _, err := second.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, retry); err != nil {
			t.Fatal(err)
		}
		if replayed := donationTestItem(t, second, source, request.Items[0].ItemID); !reflect.DeepEqual(consumed, replayed) {
			t.Fatal("replayed retry action reset a later probe's budget or state")
		}
		if _, err := second.service.RetryDonationBatch(t.Context(), source, request.BatchID, action, DonationRetryRequest{}); !errors.Is(err, app_errors.ErrIdempotencyKeyReused) {
			t.Fatalf("changed retry action error = %v", err)
		}
		assertExternalDonationCount(t, first.db.Model(&models.DonationRetry{}).Where("source_id = ? AND idempotency_key = ?", source, action), 1)
		now = now.Add(time.Minute)
		executor.result = successfulCredentialProbeResult()
		if err := first.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		if item := donationTestItem(t, first, source, request.Items[0].ItemID); item.State != "accepted" || item.CredentialID == nil {
			t.Fatalf("retried item did not recover: %#v", item)
		}
	})

	t.Run("independent_connections_fence_resource_ownership", func(t *testing.T) {
		peers := []serviceFixture{newServiceFixtureWithDatabase(t, fixture.db), donationSecondService(t, dsn)}
		firstGroup := newExternalDonationEmptyGroup(t, peers[0], "external-donation-owner-first")
		secondGroup := newExternalDonationEmptyGroup(t, peers[0], "external-donation-owner-second")
		source, firstRequest := stageDonationTestBatch(t, peers[0], firstGroup, 3000, "synthetic-external-contended-key")
		_, secondRequest := stageDonationTestBatch(t, peers[1], secondGroup, 3010, "synthetic-external-contended-key")
		items := []models.DonationItem{
			donationTestItem(t, peers[0], source, firstRequest.Items[0].ItemID),
			donationTestItem(t, peers[1], source, secondRequest.Items[0].ItemID),
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		t.Cleanup(cancel)
		start, release := make(chan struct{}), make(chan struct{})
		entered := make(chan int, 2)
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(unblock)
		type workResult struct {
			index int
			err   error
		}
		results := make(chan workResult, 2)
		var workers sync.WaitGroup
		for index, peer := range peers {
			peer.service.executor = &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
				entered <- index
				select {
				case <-release:
				case <-ctx.Done():
				}
				return successfulCredentialProbeResult()
			}}
			workers.Go(func() {
				<-start
				results <- workResult{index, peer.service.processDonationItem(ctx, items[index].ID)}
			})
		}
		t.Cleanup(func() { unblock(); cancel(); workers.Wait() })
		close(start)
		var owner int
		select {
		case owner = <-entered:
		case <-ctx.Done():
			t.Fatal("neither contender acquired the resource")
		}
		select {
		case result := <-results:
			if result.err != nil || result.index == owner {
				t.Fatalf("non-owner work result = %#v", result)
			}
		case <-entered:
			t.Fatal("independent database transactions allowed two concurrent probes")
		case <-ctx.Done():
			t.Fatal("contending request did not observe durable ownership")
		}
		loser := 1 - owner
		busy := donationTestItem(t, peers[loser], source, items[loser].ItemID)
		if busy.State != "retry_pending" || busy.ReasonCode != "resource_busy" || busy.CredentialID != nil {
			t.Fatalf("competing item = %#v", busy)
		}
		unblock()
		select {
		case result := <-results:
			if result.err != nil || result.index != owner {
				t.Fatalf("owner work result = %#v", result)
			}
		case <-ctx.Done():
			t.Fatal("owner did not finish acceptance")
		}
		peers[loser].service.now = func() time.Time { return time.Now().Add(time.Minute) }
		if err := peers[loser].service.processDonationItem(t.Context(), items[loser].ID); err != nil {
			t.Fatal(err)
		}
		accepted := donationTestItem(t, peers[owner], source, items[owner].ItemID)
		duplicate := donationTestItem(t, peers[loser], source, items[loser].ItemID)
		if accepted.State != "accepted" || accepted.CredentialID == nil || duplicate.State != "existing" || duplicate.CredentialID != nil {
			t.Fatalf("contended receipt states = %q / %q", accepted.State, duplicate.State)
		}
		assertExternalDonationCount(t, fixture.db.Model(&models.Credential{}).Where("fingerprint = ?", accepted.CredentialFingerprint), 1)
	})

	t.Run("credential_and_receipt_transaction_rollback", func(t *testing.T) {
		first := newServiceFixtureWithDatabase(t, fixture.db)
		observer := donationSecondService(t, dsn)
		groupID := newExternalDonationEmptyGroup(t, first, "external-donation-rollback")
		source, request := stageDonationTestBatch(t, first, groupID, 4000, "synthetic-external-rollback-key")
		first.service.executor = &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
		const hook = "test:external_donation_receipt_failure"
		if err := first.db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
			updates, ok := tx.Statement.Dest.(map[string]any)
			if tx.Statement.Table == "donation_items" && ok && updates["state"] == "committing" {
				_ = tx.AddError(errors.New("synthetic failure after credential and acquisition writes"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := first.db.Callback().Update().Remove(hook); err != nil {
				t.Errorf("remove receipt failure: %v", err)
			}
		})
		if err := first.service.DrainDonationWork(t.Context()); err == nil {
			t.Fatal("injected receipt failure reported success")
		}
		rolledBack := donationTestItem(t, observer, source, request.Items[0].ItemID)
		if rolledBack.State != "validating" || rolledBack.CredentialID != nil || rolledBack.AcceptedAtMS != nil || rolledBack.EncryptedPayload == "" {
			t.Fatalf("independent connection observed partial receipt: %#v", rolledBack)
		}
		assertExternalDonationCount(t, observer.db.Model(&models.Credential{}).Where("fingerprint = ?", rolledBack.CredentialFingerprint), 0)
		var resource models.DonationResource
		if err := observer.db.Where("fingerprint = ?", rolledBack.Fingerprint).Take(&resource).Error; err != nil {
			t.Fatal(err)
		}
		if resource.AcquiredAtMS != nil || resource.CredentialID != nil || resource.Origin != "" {
			t.Fatal("rollback left a permanent acquisition without a credential/receipt")
		}
		observer.service.now = func() time.Time { return time.Now().Add(2 * donationLeaseDuration) }
		observer.service.executor = &credentialProbeTestExecutor{result: successfulCredentialProbeResult()}
		if err := observer.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		recovered := donationTestItem(t, observer, source, request.Items[0].ItemID)
		if recovered.State != "accepted" || recovered.CredentialID == nil {
			t.Fatalf("rolled-back item did not recover: %#v", recovered)
		}
		assertExternalDonationCount(t, observer.db.Model(&models.Credential{}).Where("fingerprint = ?", recovered.CredentialFingerprint), 1)
	})

	t.Run("management_import_preempts_reserved_donation", func(t *testing.T) {
		first := newServiceFixtureWithDatabase(t, fixture.db)
		management := donationSecondService(t, dsn)
		donationGroup := newExternalDonationEmptyGroup(t, first, "external-donation-import-race")
		managementGroup := newExternalDonationEmptyGroup(t, first, "external-donation-import-race-admin")
		source, request := stageDonationTestBatch(t, first, donationGroup, 5000, "synthetic-external-admin-import-race")
		first.service.executor = &credentialProbeTestExecutor{execute: func(execution.AttemptSpec) execution.AttemptResult {
			result, err := management.service.ImportGroupCredentials(t.Context(), managementGroup, CredentialImportRequest{Credentials: request.Items[0].Key})
			if err != nil || result.CredentialsAdded != 1 {
				t.Errorf("management import during probe = %#v, error = %v", result, err)
			}
			return successfulCredentialProbeResult()
		}}
		if err := first.service.DrainDonationWork(t.Context()); err != nil {
			t.Fatal(err)
		}
		item := donationTestItem(t, first, source, request.Items[0].ItemID)
		if item.State != "existing" || item.CredentialID != nil || item.AcceptedAtMS != nil {
			t.Fatalf("donation acquired concurrently imported inventory: %#v", item)
		}
		var resource models.DonationResource
		if err := first.db.Where("fingerprint = ?", item.Fingerprint).Take(&resource).Error; err != nil {
			t.Fatal(err)
		}
		if resource.Origin != "inventory" || resource.OwnerItemID != nil || resource.GroupID != managementGroup || resource.CredentialID == nil {
			t.Fatalf("management acquisition = %#v", resource)
		}
		assertExternalDonationCount(t, first.db.Model(&models.Credential{}).Where("fingerprint = ?", item.CredentialFingerprint), 1)
	})
}

func newExternalDonationEmptyGroup(t *testing.T, fixture serviceFixture, name string) uint {
	t.Helper()
	groupID := newDonationTestGroup(t, fixture, name, "synthetic-"+name+"-seed")
	var credential models.Credential
	if err := fixture.db.Where("group_id = ?", groupID).Take(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DeleteGroupCredential(t.Context(), groupID, credential.ID); err != nil {
		t.Fatal(err)
	}
	return groupID
}

func newExternalDonationFixture(t *testing.T) (serviceFixture, string) {
	t.Helper()
	rawDSN := strings.TrimSpace(os.Getenv("GPT_LOAD_DATABASE_TEST_DSN"))
	if rawDSN == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	parsed, err := url.Parse(rawDSN)
	if err != nil {
		t.Fatal("parse external donation database DSN")
	}
	admin := openControlTestDBWithoutMigration(t, rawDSN)
	name := fmt.Sprintf("gpt_load_donation_%d", time.Now().UnixNano())
	target := *parsed
	switch strings.ToLower(parsed.Scheme) {
	case "mysql":
		if err := admin.Exec("CREATE DATABASE `" + name + "`").Error; err != nil {
			t.Fatalf("create isolated donation database: %v", err)
		}
		t.Cleanup(func() {
			if err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
				t.Errorf("drop isolated donation database: %v", err)
			}
		})
		target.Path, target.RawPath = "/"+name, ""
	case "postgres", "postgresql":
		if err := admin.Exec(`CREATE SCHEMA "` + name + `"`).Error; err != nil {
			t.Fatalf("create isolated donation schema: %v", err)
		}
		t.Cleanup(func() {
			if err := admin.Exec(`DROP SCHEMA IF EXISTS "` + name + `" CASCADE`).Error; err != nil {
				t.Errorf("drop isolated donation schema: %v", err)
			}
		})
		query := target.Query()
		query.Set("search_path", name)
		target.RawQuery = query.Encode()
	default:
		t.Fatalf("external donation tests require MySQL or PostgreSQL, got %q", parsed.Scheme)
	}
	dsn := target.String()
	db := openControlTestDBWithoutMigration(t, dsn)
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("external donation database: %s", version)
	isolationQuery := "SHOW transaction_isolation"
	if db.Dialector.Name() == "mysql" {
		isolationQuery = "SELECT @@transaction_isolation"
	}
	var isolation string
	if err := db.Raw(isolationQuery).Scan(&isolation).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("external donation transaction isolation: %s", isolation)
	if db.Migrator().HasTable("groups") || db.Migrator().HasTable("donation_resources") {
		t.Fatal("isolated donation schema is not fresh")
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate fresh isolated donation schema: %v", err)
	}
	return newServiceFixtureWithDatabase(t, db), dsn
}

func assertExternalDonationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range migrationfiles.TableNames0015() {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("donation migration omitted table %q", table)
		}
	}
	assertExternalDonationCount(t, db.Table("schema_migrations").Where("id = ?", migrationfiles.ID0015), 1)
	assertExternalDonationCount(t, db.Table("schema_migrations").Where("id LIKE ?", "%#building"), 0)
}

func assertExternalDonationCount(t *testing.T, query *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := query.Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("database row count = %d, want %d", count, want)
	}
}
