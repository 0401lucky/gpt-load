package state

import (
	"testing"
	"time"
)

type modelCycleRegistry interface {
	GroupModelCycles(uint, time.Time) map[uint][]CredentialModelCycleView
}

func modelCycleOf(t *testing.T, registry *CredentialRegistry, groupID, credentialID uint, model string, now time.Time) (CredentialModelCycleView, bool) {
	t.Helper()
	api, ok := any(registry).(modelCycleRegistry)
	if !ok {
		t.Fatal("registry does not expose model cycles")
	}
	for _, cycle := range api.GroupModelCycles(groupID, now)[credentialID] {
		if cycle.Model == model {
			return cycle, true
		}
	}
	return CredentialModelCycleView{}, false
}

func TestModelCycleStartMovesOnlyAfterTheRecordedResetHasHappened(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)

	firstReset := now.Add(3 * time.Hour)
	if accepted, changed := api.SetModelCooldown(ref, "model-a", firstReset, now); !accepted || !changed {
		t.Fatal("first cooldown was not recorded")
	}
	cycle, ok := modelCycleOf(t, r, 10, 1, "model-a", now)
	if !ok {
		t.Fatal("first cooldown did not produce a cycle record")
	}
	// 首次冷却只知道下一次重置时刻；上一周期起点无从得知，必须保持零值。
	if !cycle.CycleStart.IsZero() || !cycle.NextReset.Equal(firstReset) || !cycle.CooldownUntil.Equal(firstReset) {
		t.Fatalf("first cycle = %+v, want zero start and next reset %s", cycle, firstReset)
	}

	// 冷却尚未过期时的延长只更新下一次重置时刻，不得迁移周期起点。
	laterReset := firstReset.Add(30 * time.Minute)
	if _, changed := api.SetModelCooldown(ref, "model-a", laterReset, now.Add(time.Minute)); !changed {
		t.Fatal("longer cooldown was not recorded")
	}
	cycle, _ = modelCycleOf(t, r, 10, 1, "model-a", now.Add(time.Minute))
	if !cycle.CycleStart.IsZero() {
		t.Fatalf("cycle start migrated before the reset happened: %+v", cycle)
	}

	// 重置时刻已经发生后的新 429 才把该时刻变成新的周期起点。
	afterReset := laterReset.Add(time.Minute)
	nextReset := afterReset.Add(4 * time.Hour)
	if accepted, changed := api.SetModelCooldown(ref, "model-a", nextReset, afterReset); !accepted || !changed {
		t.Fatal("post-reset cooldown was not recorded")
	}
	cycle, _ = modelCycleOf(t, r, 10, 1, "model-a", afterReset)
	if !cycle.CycleStart.Equal(laterReset) || !cycle.NextReset.Equal(nextReset) {
		t.Fatalf("second cycle = %+v, want start %s and next reset %s", cycle, laterReset, nextReset)
	}
	if !cycle.CooldownUntil.Equal(nextReset) {
		t.Fatalf("cooling until = %s, want %s", cycle.CooldownUntil, nextReset)
	}
}

func TestModelCycleAnchorSurvivesCooldownExpiry(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	reset := now.Add(2 * time.Hour)
	api.SetModelCooldown(ref, "model-a", reset, now)

	expired := reset.Add(time.Minute)
	r.ExpireModelCooldowns(expired)
	if len(api.ModelCooldowns(1, expired)) != 0 {
		t.Fatal("cooldown did not expire")
	}
	cycle, ok := modelCycleOf(t, r, 10, 1, "model-a", expired)
	if !ok || !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.IsZero() {
		t.Fatalf("expired cooldown dropped its cycle anchor: %+v (present=%t)", cycle, ok)
	}

	checkpoint := r.CaptureRuntimeCheckpoint()
	if len(checkpoint) != 1 || !checkpoint[0].ModelNextResets["model-a"].Equal(reset) {
		t.Fatalf("checkpoint dropped the cycle anchor: %+v", checkpoint)
	}
}

func TestModelCycleCheckpointRoundTripRestoresAnchors(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	firstReset := now.Add(time.Hour)
	api.SetModelCooldown(ref, "model-a", firstReset, now)
	secondReset := firstReset.Add(2 * time.Hour)
	api.SetModelCooldown(ref, "model-a", secondReset.Add(time.Hour), secondReset)
	checkpoint := r.CaptureRuntimeCheckpoint()

	restoredRegistry := NewCredentialRegistry()
	mustReplaceKeyEntries(t, restoredRegistry, []CredentialEntry{{ID: 1, GroupID: 10, Version: 1,
		IdentityGeneration: 1, Status: CredentialStatusActive, AuthState: CredentialAuthStateReady,
		Fingerprint: "fixture", EncryptedValue: "fixture"}})
	if restored := restoredRegistry.RestoreRuntimeCheckpoint(checkpoint); restored != 1 {
		t.Fatalf("RestoreRuntimeCheckpoint() = %d, want 1", restored)
	}
	cycle, ok := modelCycleOf(t, restoredRegistry, 10, 1, "model-a", now)
	if !ok || !cycle.CycleStart.Equal(firstReset) || !cycle.NextReset.Equal(secondReset.Add(time.Hour)) {
		t.Fatalf("restored cycle = %+v (present=%t)", cycle, ok)
	}
}

func TestModelCycleCheckpointFromOlderFormatRestoresEmpty(t *testing.T) {
	r := NewCredentialRegistry()
	mustReplaceKeyEntries(t, r, []CredentialEntry{{ID: 1, GroupID: 10, Version: 1,
		IdentityGeneration: 1, Status: CredentialStatusActive, AuthState: CredentialAuthStateReady,
		Fingerprint: "fixture", EncryptedValue: "fixture"}})
	legacy := []CredentialRuntimeCheckpoint{{ID: 1, GroupID: 10, IdentityGeneration: 1}}
	if restored := r.RestoreRuntimeCheckpoint(legacy); restored != 1 {
		t.Fatalf("RestoreRuntimeCheckpoint() = %d, want 1", restored)
	}
	if cycles := r.GroupModelCycles(10, time.Now().UTC()); len(cycles[1]) != 0 {
		t.Fatalf("legacy checkpoint produced cycle anchors: %+v", cycles)
	}
}

func TestModelCyclePruningKeepsAnchorsUntilTheyAreStale(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	api.SetModelCooldown(ref, "model-a", now.Add(time.Hour), now)

	// 冷却过期本身不清理锚点。
	r.ExpireModelCooldowns(now.Add(2 * time.Hour))
	if _, ok := modelCycleOf(t, r, 10, 1, "model-a", now.Add(2*time.Hour)); !ok {
		t.Fatal("anchor was pruned with the cooldown")
	}

	// 仍在保留期内：其他模型的冷却不会误删已有锚点。
	withinRetention := now.Add(40 * time.Hour)
	api.SetModelCooldown(ref, "model-b", withinRetention.Add(time.Hour), withinRetention)
	cycles := r.GroupModelCycles(10, withinRetention)
	if len(cycles[1]) != 2 {
		t.Fatalf("model cycles = %+v, want model-a and model-b", cycles[1])
	}

	// 超过保留期：陈旧锚点被清理，较新的锚点保留。
	beyondRetention := now.Add(53 * time.Hour)
	api.SetModelCooldown(ref, "model-c", beyondRetention.Add(time.Hour), beyondRetention)
	cycles = r.GroupModelCycles(10, beyondRetention)
	if len(cycles[1]) != 2 {
		t.Fatalf("model cycles = %+v, want a bounded set of two models", cycles[1])
	}
	if _, ok := modelCycleOf(t, r, 10, 1, "model-a", beyondRetention); ok {
		t.Fatal("stale anchor was not pruned")
	}
	if _, ok := modelCycleOf(t, r, 10, 1, "model-c", beyondRetention); !ok {
		t.Fatal("fresh anchor was pruned")
	}
}

func TestModelCycleFollowsCredentialIdentityPreservation(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	reset := now.Add(time.Hour)
	api.SetModelCooldown(ref, "model-a", reset, now)

	entries := []CredentialEntry{{ID: 1, GroupID: 10, Version: 2, IdentityGeneration: 1,
		Status: CredentialStatusDisabled, Fingerprint: "new-secret", EncryptedValue: "new-secret"}}
	if _, err := r.ReconcileGroup(10, entries); err != nil {
		t.Fatal(err)
	}
	if cycle, ok := modelCycleOf(t, r, 10, 1, "model-a", now); !ok || !cycle.NextReset.Equal(reset) {
		t.Fatalf("identity-preserving rebuild lost the anchor: %+v (present=%t)", cycle, ok)
	}

	entries[0].IdentityGeneration = 2
	if _, err := r.ReconcileGroup(10, entries); err != nil {
		t.Fatal(err)
	}
	if cycles := r.GroupModelCycles(10, now); len(cycles[1]) != 0 {
		t.Fatalf("new account identity inherited stale anchors: %+v", cycles)
	}
}
