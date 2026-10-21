package state

import (
	"testing"
	"time"
)

type modelCycleRegistry interface {
	GroupModelCycles(uint, time.Time) map[uint][]CredentialModelCycleView
}

func TestOrdinaryModelCooldownNeverEstablishesCycle(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i) * time.Minute)
		api.SetModelCooldown(ref, "model-a", at.Add(time.Minute), at)
		cycle, _ := modelCycleOf(t, r, 10, 1, "model-a", at)
		if !cycle.CycleStart.IsZero() || !cycle.NextReset.IsZero() {
			t.Fatalf("ordinary retry established quota cycle: %+v", cycle)
		}
	}
}

func TestModelResetEvidenceSurvivesLongerExistingCooldown(t *testing.T) {
	r, api, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	cooldown, reset := now.Add(4*time.Hour), now.Add(time.Hour)
	api.SetModelCooldown(ref, "model-a", cooldown, now)
	if accepted, changed := r.SetModelCooldownWithReset(ref, "model-a", reset, reset, now); !accepted || !changed {
		t.Fatal("shorter cooldown swallowed independent quota evidence")
	}
	if _, changed := r.SetModelCooldownWithReset(ref, "model-a", reset, reset, now); changed {
		t.Fatal("duplicate hint changed cycle")
	}
	cycle, _ := modelCycleOf(t, r, 10, 1, "model-a", now)
	if !cycle.NextReset.Equal(reset) || !cycle.CooldownUntil.Equal(cooldown) || !cycle.CycleStart.IsZero() {
		t.Fatalf("independent deadlines = %+v", cycle)
	}
	r.ClearModelCooldowns(1)
	if accepted, _ := r.SetModelCooldownWithReset(ref, "model-a", cooldown, cooldown, now); accepted {
		t.Fatal("stale generation wrote quota evidence")
	}
}

func TestModelResetRejectsNonFutureEvidenceWithoutMutation(t *testing.T) {
	for _, offset := range []time.Duration{-time.Minute, 0} {
		r, _, now := modelCooldownFixture(t)
		ref, _ := r.CredentialRef(1)
		if accepted, changed := r.SetModelCooldownWithReset(ref, "model-a", now.Add(time.Hour), now.Add(offset), now); accepted || changed {
			t.Fatalf("non-future reset accepted: offset=%v, accepted=%t, changed=%t", offset, accepted, changed)
		}
		if cycles := r.GroupModelCycles(10, now); len(cycles[1]) != 0 {
			t.Fatalf("invalid evidence mutated model state: %+v", cycles)
		}
	}
}

func TestLegacyModelCyclesAreDiscardedWithoutLosingHealth(t *testing.T) {
	for _, version := range []int{0, 2} {
		r, _, now := modelCooldownFixture(t)
		until := now.Add(time.Hour)
		checkpoint := CredentialRuntimeCheckpoint{ID: 1, GroupID: 10, IdentityGeneration: 1,
			Blacklisted: true, FailureCount: 3, CooldownUntil: until, ModelCycleVersion: version,
			ModelCooldowns:     map[string]time.Time{"model-a": until},
			ModelCycleStarts:   map[string]time.Time{"model-a": now.Add(-time.Hour)},
			ModelNextResets:    map[string]time.Time{"model-a": until},
			QuotaCooldownUntil: until, QuotaModelCooldowns: map[string]time.Time{"model-a": until}}
		r.RestoreRuntimeCheckpoint([]CredentialRuntimeCheckpoint{checkpoint})
		got := r.CaptureRuntimeCheckpoint()[0]
		if !got.Blacklisted || got.FailureCount != 3 || !got.CooldownUntil.Equal(until) ||
			!got.ModelCooldowns["model-a"].Equal(until) || !got.QuotaCooldownUntil.Equal(until) ||
			!got.QuotaModelCooldowns["model-a"].Equal(until) || len(got.ModelCycleStarts) != 0 || len(got.ModelNextResets) != 0 {
			t.Fatalf("legacy restore changed health or trusted old cycle: %+v", got)
		}
	}
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
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)

	firstReset := now.Add(3 * time.Hour)
	if accepted, changed := r.SetModelCooldownWithReset(ref, "model-a", firstReset, firstReset, now); !accepted || !changed {
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
	if _, changed := r.SetModelCooldownWithReset(ref, "model-a", laterReset, laterReset, now.Add(time.Minute)); !changed {
		t.Fatal("longer cooldown was not recorded")
	}
	cycle, _ = modelCycleOf(t, r, 10, 1, "model-a", now.Add(time.Minute))
	if !cycle.CycleStart.IsZero() {
		t.Fatalf("cycle start migrated before the reset happened: %+v", cycle)
	}

	// 重置时刻已经发生后的新 429 才把该时刻变成新的周期起点。
	afterReset := laterReset.Add(time.Minute)
	nextReset := afterReset.Add(4 * time.Hour)
	if accepted, changed := r.SetModelCooldownWithReset(ref, "model-a", nextReset, nextReset, afterReset); !accepted || !changed {
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
	r.SetModelCooldownWithReset(ref, "model-a", reset, reset, now)

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
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	firstReset := now.Add(time.Hour)
	r.SetModelCooldownWithReset(ref, "model-a", firstReset, firstReset, now)
	secondReset := firstReset.Add(2 * time.Hour)
	r.SetModelCooldownWithReset(ref, "model-a", secondReset.Add(time.Hour), secondReset.Add(time.Hour), secondReset)
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
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	r.SetModelCooldownWithReset(ref, "model-a", now.Add(time.Hour), now.Add(time.Hour), now)

	// 冷却过期本身不清理锚点。
	r.ExpireModelCooldowns(now.Add(2 * time.Hour))
	if _, ok := modelCycleOf(t, r, 10, 1, "model-a", now.Add(2*time.Hour)); !ok {
		t.Fatal("anchor was pruned with the cooldown")
	}

	// 仍在保留期内：其他模型的冷却不会误删已有锚点。
	withinRetention := now.Add(40 * time.Hour)
	r.SetModelCooldownWithReset(ref, "model-b", withinRetention.Add(time.Hour), withinRetention.Add(time.Hour), withinRetention)
	cycles := r.GroupModelCycles(10, withinRetention)
	if len(cycles[1]) != 2 {
		t.Fatalf("model cycles = %+v, want model-a and model-b", cycles[1])
	}

	// 超过保留期：陈旧锚点被清理，较新的锚点保留。
	beyondRetention := now.Add(53 * time.Hour)
	r.SetModelCooldownWithReset(ref, "model-c", beyondRetention.Add(time.Hour), beyondRetention.Add(time.Hour), beyondRetention)
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
	r, _, now := modelCooldownFixture(t)
	ref, _ := r.CredentialRef(1)
	reset := now.Add(time.Hour)
	r.SetModelCooldownWithReset(ref, "model-a", reset, reset, now)

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
