package state

import (
	"sort"
	"strings"
	"time"
)

// modelCycleRetention 决定周期锚点的保留时长。锚点只服务于额度窗口口径，
// 与冷却是否过期无关，因此必须比冷却存活更久。
const modelCycleRetention = 48 * time.Hour

// SetModelCooldown 只接受当前目标、当前恢复代次的结果；并发限制只延长期限。
func (r *CredentialRegistry) SetModelCooldown(ref CredentialRef, model string, until, now time.Time) (bool, bool) {
	if model == "" || strings.TrimSpace(model) != model || !until.After(now) {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entryLocked(ref.ID)
	if !ok || entry.GroupID != ref.GroupID || entry.IdentityGeneration != ref.IdentityGeneration ||
		entry.ModelCooldownGeneration != ref.ModelCooldownGeneration {
		return false, false
	}
	pruneModelCooldowns(entry.ModelCooldowns, now)
	if !until.After(entry.ModelCooldowns[model]) {
		return true, false
	}
	if entry.ModelCooldowns == nil {
		entry.ModelCooldowns = make(map[string]time.Time)
	}
	// 最近一次已知的重置时刻若已经发生，它就是本组合新的周期起点。
	// 该锚点只在能确认重置已发生时迁移，冷却期间的重复 429 不会改写它。
	if previousReset, ok := entry.ModelNextResets[model]; ok && !previousReset.After(now) {
		if entry.ModelCycleStarts == nil {
			entry.ModelCycleStarts = make(map[string]time.Time)
		}
		entry.ModelCycleStarts[model] = previousReset
	}
	if entry.ModelNextResets == nil {
		entry.ModelNextResets = make(map[string]time.Time)
	}
	entry.ModelNextResets[model] = until
	pruneModelCycles(entry.ModelCycleStarts, entry.ModelNextResets, now)
	entry.ModelCooldowns[model] = until
	return true, true
}

func (r *CredentialRegistry) ModelCooldowns(credentialID uint, now time.Time) map[string]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entryLocked(credentialID)
	if !ok {
		return nil
	}
	pruneModelCooldowns(entry.ModelCooldowns, now)
	return cloneModelTimes(entry.ModelCooldowns)
}

// CredentialModelCycleView 是一个 (凭据, 上游模型) 的周期锚点与冷却状态快照。
type CredentialModelCycleView struct {
	Model string
	// CycleStart 是上一次已发生的重置时刻，即当前额度窗口的起点；零值表示尚无周期记录。
	CycleStart time.Time
	// NextReset 是最近一次已知的下一次重置时刻；零值表示未知。
	NextReset time.Time
	// CooldownUntil 仅在正在冷却时非零。
	CooldownUntil time.Time
}

// GroupModelCycles 返回分组内每个已注册凭据的模型周期锚点，按模型名稳定排序；
// 没有任何周期记录的凭据也会出现（列表为空），因此调用方可以据此判断凭据是否仍在组内。
// 纯内存读取，不触发 IO，也不参与调度或冷却判定。
func (r *CredentialRegistry) GroupModelCycles(groupID uint, now time.Time) map[uint][]CredentialModelCycleView {
	if r == nil || groupID == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	bucket := r.buckets[groupID]
	if len(bucket) == 0 {
		return nil
	}
	views := make(map[uint][]CredentialModelCycleView, len(bucket))
	for _, entry := range bucket {
		models := make(map[string]struct{}, len(entry.ModelCycleStarts)+len(entry.ModelNextResets)+len(entry.ModelCooldowns))
		for model := range entry.ModelCycleStarts {
			models[model] = struct{}{}
		}
		for model := range entry.ModelNextResets {
			models[model] = struct{}{}
		}
		for model, until := range entry.ModelCooldowns {
			if until.After(now) {
				models[model] = struct{}{}
			}
		}
		list := make([]CredentialModelCycleView, 0, len(models))
		for model := range models {
			cycle := CredentialModelCycleView{
				Model:      model,
				CycleStart: entry.ModelCycleStarts[model],
				NextReset:  entry.ModelNextResets[model],
			}
			if until := entry.ModelCooldowns[model]; until.After(now) {
				cycle.CooldownUntil = until
			}
			list = append(list, cycle)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Model < list[j].Model })
		views[entry.ID] = list
	}
	return views
}

// ClearModelCooldowns 属于显式恢复；不由普通成功、token 刷新或额度同步调用。
func (r *CredentialRegistry) ClearModelCooldowns(credentialID uint) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entryLocked(credentialID)
	if !ok {
		return false
	}
	entry.ModelCooldowns = nil
	entry.ModelCooldownGeneration++
	return true
}

// ExpireModelCooldowns 复用现有运行态维护时机清理，不新增定时任务。
func (r *CredentialRegistry) ExpireModelCooldowns(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, bucket := range r.buckets {
		for _, entry := range bucket {
			pruneModelCooldowns(entry.ModelCooldowns, now)
			pruneModelCycles(entry.ModelCycleStarts, entry.ModelNextResets, now)
		}
	}
}

func pruneModelCooldowns(limits map[string]time.Time, now time.Time) {
	for model, until := range limits {
		if model == "" || !until.After(now) {
			delete(limits, model)
		}
	}
}

// pruneModelCycles 刻意不复用 pruneModelCooldowns：后者在冷却过期时立即删除条目，
// 而周期锚点必须保留，否则窗口口径会在冷却结束的瞬间退化为近 24 小时。
// 只有超过保留期的陈旧锚点才被清理，两个 map 始终有界。
func pruneModelCycles(starts, resets map[string]time.Time, now time.Time) {
	cutoff := now.Add(-modelCycleRetention)
	for model, resetAt := range resets {
		if model == "" || resetAt.Before(cutoff) {
			delete(resets, model)
			delete(starts, model)
		}
	}
	for model, startAt := range starts {
		if _, tracked := resets[model]; tracked {
			continue
		}
		if model == "" || startAt.Before(cutoff) {
			delete(starts, model)
		}
	}
}

func cloneModelTimes(values map[string]time.Time) map[string]time.Time {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]time.Time, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func preserveModelCooldowns(next *CredentialEntry, previous *CredentialEntry) {
	if previous == nil {
		return
	}
	next.ModelCooldownGeneration = previous.ModelCooldownGeneration
	if next.ID == previous.ID && next.GroupID == previous.GroupID && next.IdentityGeneration == previous.IdentityGeneration {
		next.ModelCooldowns = cloneModelTimes(previous.ModelCooldowns)
		next.ModelCycleStarts = cloneModelTimes(previous.ModelCycleStarts)
		next.ModelNextResets = cloneModelTimes(previous.ModelNextResets)
	} else {
		next.ModelCooldowns = nil
		next.ModelCycleStarts = nil
		next.ModelNextResets = nil
		next.ModelCooldownGeneration++
	}
}
