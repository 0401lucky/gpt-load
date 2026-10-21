package state

import (
	"testing"
	"time"
)

func TestQuotaModelResetEvidenceSurvivesLongerCooldownAndRecovery(t *testing.T) {
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	start, reset, quotaUntil := now.Add(-time.Hour), now.Add(2*time.Hour), now.Add(4*time.Hour)
	if accepted, changed := r.SetQuotaCooldownWithReset(ref, "model-a", start, start, now.Add(-2*time.Hour)); !accepted || !changed {
		t.Fatal("previous quota reset was not recorded")
	}
	if accepted, changed := r.SetQuotaCooldown(ref, "model-a", quotaUntil, now); !accepted || !changed {
		t.Fatal("quota cooldown was not recorded")
	}
	if accepted, changed := r.SetQuotaCooldownWithReset(ref, "model-a", now.Add(time.Minute), reset, now); !accepted || !changed {
		t.Fatal("shorter cooldown swallowed trusted reset evidence")
	}
	if _, changed := r.SetQuotaCooldownWithReset(ref, "model-a", now.Add(time.Minute), reset, now); changed {
		t.Fatal("duplicate quota evidence changed the cycle")
	}
	ordinaryUntil := now.Add(30 * time.Minute)
	r.SetModelCooldown(ref, "model-a", ordinaryUntil, now)
	cycle, ok := modelCycleOf(t, r, 10, 1, "model-a", now)
	if !ok || !cycle.CycleStart.Equal(start) || !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.Equal(quotaUntil) {
		t.Fatalf("quota cooldown and cycle = %+v, present=%t", cycle, ok)
	}
	if view := r.Snapshot()[0]; !view.ModelCooldowns["model-a"].Equal(quotaUntil) {
		t.Fatalf("runtime view lost quota cooldown: %+v", view)
	}
	if !r.RestoreObservedQuotaCooldowns(1, now.Add(time.Second).UnixMilli()) {
		t.Fatal("new quota observation did not restore quota cooldown")
	}
	cycle, _ = modelCycleOf(t, r, 10, 1, "model-a", now)
	if !cycle.CycleStart.Equal(start) || !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.Equal(ordinaryUntil) {
		t.Fatalf("quota recovery changed the cycle or ordinary cooldown: %+v", cycle)
	}
}

func TestQuotaRecoveryRejectsInFlightResetEvidence(t *testing.T) {
	r, _, now := modelCooldownFixture(t)
	before, _ := r.CredentialRef(1)
	reset := now.Add(time.Hour)
	r.SetQuotaCooldownWithReset(before, "model-a", reset, reset, now)
	r.RestoreQuotaCooldowns(1)
	for _, write := range []struct {
		name string
		set  func(CredentialRef, string, time.Time, time.Time, time.Time) (bool, bool)
	}{
		{name: "quota cooldown", set: r.SetQuotaCooldownWithReset},
		{name: "custom cooldown with trusted reset", set: r.SetModelCooldownWithReset},
	} {
		t.Run(write.name, func(t *testing.T) {
			if accepted, changed := write.set(before, "model-a", now.Add(3*time.Hour), now.Add(2*time.Hour), now); accepted || changed {
				t.Fatal("in-flight response before recovery rewrote model state")
			}
		})
	}
	cycle, _ := modelCycleOf(t, r, 10, 1, "model-a", now)
	if !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.IsZero() {
		t.Fatalf("rejected response changed trusted cycle: %+v", cycle)
	}
	if accepted, changed := r.SetModelCooldown(before, "ordinary", now.Add(time.Minute), now); !accepted || !changed {
		t.Fatal("quota recovery rejected an unrelated ordinary cooldown")
	}
	after, _ := r.CredentialRef(1)
	if accepted, changed := r.SetQuotaCooldownWithReset(after, "model-a", now.Add(3*time.Hour), now.Add(2*time.Hour), now); !accepted || !changed {
		t.Fatal("post-recovery quota evidence was rejected")
	}
}

func TestQuotaModelResetSnapshotsAndRebuildsStayDetached(t *testing.T) {
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	start, reset := now.Add(-time.Hour), now.Add(2*time.Hour)
	r.SetQuotaCooldownWithReset(ref, "model-a", start, start, now.Add(-2*time.Hour))
	r.SetQuotaCooldownWithReset(ref, "model-a", reset.Add(time.Hour), reset, now)
	checkpoint := r.CaptureRuntimeCheckpoint()
	entries, err := r.SnapshotGroupCredentialEntriesExact(10, []uint{1})
	if err != nil {
		t.Fatal(err)
	}
	r.SetQuotaCooldownWithReset(ref, "model-b", reset, reset, now)
	if len(entries[0].QuotaModelCooldowns) != 1 || len(checkpoint[0].QuotaModelCooldowns) != 1 {
		t.Fatal("quota snapshot shares live cooldown maps")
	}
	if err := r.RestoreGroupCredentialEntriesExact(10, entries); err != nil {
		t.Fatal(err)
	}
	if cycles := r.GroupModelCycles(10, now)[1]; len(cycles) != 1 {
		t.Fatalf("exact snapshot did not restore quota cycle: %+v", cycles)
	}
	desired := []CredentialEntry{{ID: 1, GroupID: 10, Version: 2, IdentityGeneration: 1,
		Name: "renamed", Status: CredentialStatusActive, AuthState: CredentialAuthStateReady,
		Fingerprint: "rotated", EncryptedValue: "rotated"}}
	if _, err := r.ReconcileGroup(10, desired); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceCredentials(desired); err != nil {
		t.Fatal(err)
	}
	cycle, _ := modelCycleOf(t, r, 10, 1, "model-a", now)
	if !cycle.CycleStart.Equal(start) || !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.Equal(reset.Add(time.Hour)) {
		t.Fatalf("identity-preserving rebuild lost quota state: %+v", cycle)
	}
	restored := NewCredentialRegistry()
	mustReplaceKeyEntries(t, restored, desired)
	if restored.RestoreRuntimeCheckpoint(checkpoint) != 1 {
		t.Fatal("quota checkpoint was not restored")
	}
	restored.RestoreQuotaCooldowns(1)
	cycle, _ = modelCycleOf(t, restored, 10, 1, "model-a", now)
	if !cycle.CycleStart.Equal(start) || !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.IsZero() {
		t.Fatalf("checkpoint restore or quota recovery changed cycle: %+v", cycle)
	}
	desired[0].IdentityGeneration++
	if _, err := r.ReconcileGroup(10, desired); err != nil {
		t.Fatal(err)
	}
	r.RestoreRuntimeCheckpoint(checkpoint)
	if cycles := r.GroupModelCycles(10, now)[1]; len(cycles) != 0 {
		t.Fatalf("new identity inherited quota cycle from rebuild or checkpoint: %+v", cycles)
	}
}

func TestRegistryReplacementKeepsQuotaCooldownInSchedulingState(t *testing.T) {
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	until := now.Add(time.Hour)
	r.SetQuotaCooldown(ref, "", until, now)
	if err := r.ReplaceCredentials([]CredentialEntry{{ID: 1, GroupID: 10, Version: 2, IdentityGeneration: 1,
		Status: CredentialStatusActive, AuthState: CredentialAuthStateReady,
		Fingerprint: "rotated", EncryptedValue: "rotated"}}); err != nil {
		t.Fatal(err)
	}
	r.SchedulingState().WithLock(func(ledger *SchedulingLedger) {
		member := ledger.Members[1]
		if member == nil || !member.suspended || !member.cooldownUntil.Equal(until) {
			t.Fatalf("registry replacement lost preserved cooldown in scheduler: %+v", member)
		}
	})
}
