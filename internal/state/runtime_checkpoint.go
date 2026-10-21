package state

import (
	"sort"
	"time"
)

// CredentialRuntimeCheckpoint contains only the mutable health state that is safe to
// carry across a process restart. Persisted key configuration remains owned by
// SQLite and is matched by ID plus group ID during restore.
type CredentialRuntimeCheckpoint struct {
	ID                 uint                 `json:"id"`
	GroupID            uint                 `json:"group_id"`
	CooldownUntil      time.Time            `json:"cooldown_until"`
	Blacklisted        bool                 `json:"blacklisted"`
	FailureCount       int                  `json:"failure_count"`
	IdentityGeneration uint64               `json:"identity_generation,omitempty"`
	ModelCooldowns     map[string]time.Time `json:"model_cooldowns,omitempty"`
	// 额度窗口锚点：与 ModelCooldowns 同时保存，但独立于冷却是否过期。
	// 仅恢复带可信版本标记的锚点；旧检查点保留冷却，但额度窗口退回近 24 小时。
	ModelCycleStarts map[string]time.Time `json:"model_cycle_starts,omitempty"`
	ModelNextResets  map[string]time.Time `json:"model_next_resets,omitempty"`
	// Version 1 anchors originate exclusively from explicit quota reset hints.
	ModelCycleVersion   int                  `json:"model_cycle_version,omitempty"`
	QuotaCooldownUntil  time.Time            `json:"quota_cooldown_until,omitempty"`
	QuotaModelCooldowns map[string]time.Time `json:"quota_model_cooldowns,omitempty"`
}

// CaptureRuntimeCheckpoint returns detached runtime health state in stable
// order. It intentionally excludes credentials and failure generations.
func (r *CredentialRegistry) CaptureRuntimeCheckpoint() []CredentialRuntimeCheckpoint {
	r.ExpireModelCooldowns(time.Now())
	r.mu.RLock()
	checkpoints := make([]CredentialRuntimeCheckpoint, 0, len(r.credentialGroups))
	for _, bucket := range r.buckets {
		for _, entry := range bucket {
			checkpoints = append(checkpoints, CredentialRuntimeCheckpoint{
				ID:                  entry.ID,
				GroupID:             entry.GroupID,
				CooldownUntil:       entry.CooldownUntil,
				Blacklisted:         entry.Blacklisted,
				FailureCount:        entry.FailureCount,
				IdentityGeneration:  entry.IdentityGeneration,
				ModelCooldowns:      cloneModelTimes(entry.ModelCooldowns),
				ModelCycleStarts:    cloneModelTimes(entry.ModelCycleStarts),
				ModelNextResets:     cloneModelTimes(entry.ModelNextResets),
				ModelCycleVersion:   1,
				QuotaCooldownUntil:  entry.QuotaCooldownUntil,
				QuotaModelCooldowns: cloneModelTimes(entry.QuotaModelCooldowns),
			})
		}
	}
	r.mu.RUnlock()
	sort.Slice(checkpoints, func(i, j int) bool {
		if checkpoints[i].GroupID != checkpoints[j].GroupID {
			return checkpoints[i].GroupID < checkpoints[j].GroupID
		}
		return checkpoints[i].ID < checkpoints[j].ID
	})
	return checkpoints
}

// RestoreRuntimeCheckpoint applies matching runtime health state and skips
// keys that were removed or moved to another group while the process was down.
func (r *CredentialRegistry) RestoreRuntimeCheckpoint(checkpoints []CredentialRuntimeCheckpoint) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	restored := 0
	for _, checkpoint := range checkpoints {
		if checkpoint.ID == 0 || checkpoint.GroupID == 0 ||
			checkpoint.FailureCount < 0 {
			continue
		}
		groupID, ok := r.credentialGroups[checkpoint.ID]
		if !ok || groupID != checkpoint.GroupID {
			continue
		}
		entry, ok := r.buckets[groupID][checkpoint.ID]
		if !ok {
			continue
		}
		entry.CooldownUntil = checkpoint.CooldownUntil
		entry.Blacklisted = checkpoint.Blacklisted
		entry.FailureCount = checkpoint.FailureCount
		if checkpoint.IdentityGeneration == entry.IdentityGeneration {
			entry.ModelCooldowns = cloneModelTimes(checkpoint.ModelCooldowns)
			entry.QuotaCooldownUntil = checkpoint.QuotaCooldownUntil
			entry.QuotaModelCooldowns = cloneModelTimes(checkpoint.QuotaModelCooldowns)
			pruneModelCooldowns(entry.QuotaModelCooldowns, time.Now())
			pruneModelCooldowns(entry.ModelCooldowns, time.Now())
			// 恢复路径只能做过期清理，不能复用 pruneModelCooldowns 的语义：
			// 冷却此刻可能已经过期，但周期锚点必须活着。
			entry.ModelCycleStarts = nil
			entry.ModelNextResets = nil
			if checkpoint.ModelCycleVersion == 1 {
				entry.ModelCycleStarts = cloneModelTimes(checkpoint.ModelCycleStarts)
				entry.ModelNextResets = cloneModelTimes(checkpoint.ModelNextResets)
			}
			pruneModelCycles(entry.ModelCycleStarts, entry.ModelNextResets, time.Now())
		}
		r.scheduling.SyncCredential(runtimeView(entry))
		restored++
	}
	return restored
}
