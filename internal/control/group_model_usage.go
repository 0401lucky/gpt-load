package control

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gpt-load/internal/platform/epochms"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/response"
	"gpt-load/internal/requestlog"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
	"gpt-load/internal/usage"
)

const (
	// groupModelUsageMaxRows 限制单响应行数；超出时按总 token 降序截断，
	// 并用既有 pagination 形态下发截断前后的行数。
	groupModelUsageMaxRows = 500
	// groupModelUsageFallbackWindowMS 是无周期记录时的兜底窗口。上游额度按日计算，
	// 近 24 小时是有原则的近似；该组合一旦产生周期记录即自动切换为精确窗口。
	groupModelUsageFallbackWindowMS = epochms.MillisecondsPerDay

	groupModelUsageWindowSourceReset       = "reset"
	groupModelUsageWindowSourceFallback24h = "fallback_24h"
)

type groupModelUsageReader interface {
	QueryGroupModelUsage(context.Context, uint, int64, int64) ([]requestlog.GroupModelUsageBucket, error)
}

type groupModelUsagePagination struct {
	Page       int64 `json:"page"`
	PageSize   int64 `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int64 `json:"total_pages"`
}

type groupModelUsageItemResponse struct {
	CredentialID            uint   `json:"credential_id"`
	Model                   string `json:"model"`
	WindowStartMS           int64  `json:"window_start_ms"`
	WindowSource            string `json:"window_source"`
	CooldownUntilMS         *int64 `json:"cooldown_until_ms"`
	RequestCount            int64  `json:"request_count"`
	SuccessCount            int64  `json:"success_count"`
	FailureCount            int64  `json:"failure_count"`
	UncachedInputTokens     int64  `json:"uncached_input_tokens"`
	CacheReadTokens         int64  `json:"cache_read_tokens"`
	CacheWrite5MTokens      int64  `json:"cache_write_5m_tokens"`
	CacheWrite1HTokens      int64  `json:"cache_write_1h_tokens"`
	CacheWriteUnknownTokens int64  `json:"cache_write_unknown_tokens"`
	OutputTokens            int64  `json:"output_tokens"`
	TotalTokens             int64  `json:"total_tokens"`
}

type groupModelUsageResponse struct {
	ObservedAtMS  int64                         `json:"observed_at_ms"`
	CountedFromMS int64                         `json:"counted_from_ms"`
	CountedToMS   int64                         `json:"counted_to_ms"`
	Items         []groupModelUsageItemResponse `json:"items"`
	Pagination    groupModelUsagePagination     `json:"pagination"`
}

// GetGroupModelUsage 汇总分组内每个 (凭据, 上游模型) 在其自身额度窗口内的已用 token
// 与冷却状态。纯读：不参与调度、选取或任何冷却判定。
func (s *Service) GetGroupModelUsage(ctx context.Context, groupID uint) (groupModelUsageResponse, error) {
	if s == nil || groupID == 0 {
		return groupModelUsageResponse{}, app_errors.ErrBadRequest
	}
	reader, ok := s.usageStats.(groupModelUsageReader)
	if !ok {
		return groupModelUsageResponse{}, app_errors.ErrInternalServer
	}
	var group models.Group
	if err := s.db.WithContext(ctx).Where("id = ?", groupID).Take(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return groupModelUsageResponse{}, groupNotFoundError()
		}
		return groupModelUsageResponse{}, app_errors.ParseDBError(err)
	}
	observedAtMS, err := safeEpochMilliseconds(s.now())
	if err != nil {
		return groupModelUsageResponse{}, err
	}
	now, err := epochms.ToTime(observedAtMS)
	if err != nil {
		return groupModelUsageResponse{}, err
	}
	countedToMS, err := epochms.AlignDown(observedAtMS, epochms.MillisecondsPerHour)
	if err != nil {
		return groupModelUsageResponse{}, err
	}
	cycles := s.registry.GroupModelCycles(groupID, now)
	// 检索下界必须按本组最早的窗口起点确定：若图省事用 now-24h，
	// 起点更早的行会被系统性低估。
	earliestMS := observedAtMS - groupModelUsageFallbackWindowMS
	for _, list := range cycles {
		for _, cycle := range list {
			if startMS, _ := groupModelUsageWindow(cycle, observedAtMS); startMS < earliestMS {
				earliestMS = startMS
			}
		}
	}
	countedFromMS, err := epochms.AlignUp(earliestMS, epochms.MillisecondsPerHour)
	if err != nil {
		return groupModelUsageResponse{}, err
	}
	var rows []requestlog.GroupModelUsageBucket
	if countedFromMS < countedToMS {
		rows, err = reader.QueryGroupModelUsage(ctx, groupID, countedFromMS, countedToMS)
		if err != nil {
			return groupModelUsageResponse{}, app_errors.ParseDBError(err)
		}
	}
	items, totalItems, err := buildGroupModelUsageItems(rows, cycles, observedAtMS, countedToMS)
	if err != nil {
		return groupModelUsageResponse{}, err
	}
	return groupModelUsageResponse{
		ObservedAtMS:  observedAtMS,
		CountedFromMS: countedFromMS,
		CountedToMS:   countedToMS,
		Items:         items,
		Pagination: groupModelUsagePagination{
			Page:       1,
			PageSize:   groupModelUsageMaxRows,
			TotalItems: totalItems,
			TotalPages: (totalItems + groupModelUsageMaxRows - 1) / groupModelUsageMaxRows,
		},
	}, nil
}

// groupModelUsageWindow 按 design 3.2 的三态口径给出窗口起点与口径标识。
// 首次冷却尚无上一周期起点，只能退回兜底窗口，但冷却状态单独下发。
func groupModelUsageWindow(cycle state.CredentialModelCycleView, nowMS int64) (int64, string) {
	// 非正锚点只可能来自损坏的检查点，按无记录处理，不能让整张矩阵失败。
	startMS := int64(0)
	if value := cycle.CycleStart.UnixMilli(); !cycle.CycleStart.IsZero() && value > 0 {
		startMS = value
	}
	// 周期起点只在「设置冷却」时迁移，因此在重置已发生但尚未再次 429 的间隙里，
	// NextReset 代表的才是最近一次已发生的重置 —— 它比陈旧周期起点更晚也更准确。
	if value := cycle.NextReset.UnixMilli(); !cycle.NextReset.IsZero() && value > 0 &&
		value <= nowMS && value > startMS {
		startMS = value
	}
	if startMS > 0 {
		return startMS, groupModelUsageWindowSourceReset
	}
	return max(nowMS-groupModelUsageFallbackWindowMS, 0), groupModelUsageWindowSourceFallback24h
}

type groupModelUsageAccumulator struct {
	credentialID  uint
	model         string
	cycle         state.CredentialModelCycleView
	windowStartMS int64
	windowSource  string
	cooling       bool
	// fromMS 是窗口起点按整点向上对齐后的统计下界：usage_stats 是小时桶，
	// 只有完全落在窗口内的桶才参与统计，单侧误差不超过一小时。
	fromMS    int64
	aggregate requestlog.UsageAggregate
}

// buildGroupModelUsageItems 在内存中按 (凭据, 上游模型) 归并窗口内用量。每行按各自的
// 窗口起点裁剪桶，绝不允许按组统一窗口。返回截断前的总行数与已排序截断后的行集合。
func buildGroupModelUsageItems(
	rows []requestlog.GroupModelUsageBucket,
	cycles map[uint][]state.CredentialModelCycleView,
	observedAtMS, countedToMS int64,
) ([]groupModelUsageItemResponse, int64, error) {
	accumulators := make(map[groupModelUsageKey]*groupModelUsageAccumulator, len(cycles))
	ordered := make([]*groupModelUsageAccumulator, 0, len(cycles))
	accumulatorFor := func(credentialID uint, model string) *groupModelUsageAccumulator {
		key := groupModelUsageKey{credentialID: credentialID, model: model}
		if existing := accumulators[key]; existing != nil {
			return existing
		}
		accumulator := &groupModelUsageAccumulator{credentialID: credentialID, model: model}
		accumulators[key] = accumulator
		ordered = append(ordered, accumulator)
		return accumulator
	}
	startWindow := func(accumulator *groupModelUsageAccumulator, cycle state.CredentialModelCycleView) error {
		accumulator.cycle = cycle
		accumulator.windowStartMS, accumulator.windowSource = groupModelUsageWindow(cycle, observedAtMS)
		accumulator.cooling = !cycle.CooldownUntil.IsZero()
		fromMS, err := epochms.AlignUp(accumulator.windowStartMS, epochms.MillisecondsPerHour)
		if err != nil {
			return err
		}
		accumulator.fromMS = fromMS
		return nil
	}
	// 先登记周期锚点：正在冷却但窗口内没有用量的组合也必须出现在矩阵里。
	for credentialID, list := range cycles {
		for _, cycle := range list {
			accumulator := accumulatorFor(credentialID, cycle.Model)
			if err := startWindow(accumulator, cycle); err != nil {
				return nil, 0, err
			}
		}
	}
	for _, row := range rows {
		// 行集只含当前仍属于该分组的凭据，已删除凭据的历史用量不进入矩阵。
		if _, registered := cycles[row.CredentialID]; !registered || row.CredentialID == 0 {
			continue
		}
		accumulator := accumulatorFor(row.CredentialID, row.Model)
		if accumulator.windowSource == "" {
			if err := startWindow(accumulator, state.CredentialModelCycleView{}); err != nil {
				return nil, 0, err
			}
		}
		if row.BucketStartMS < accumulator.fromMS || row.BucketStartMS >= countedToMS {
			continue
		}
		summed, err := requestlog.AddUsageAggregates(accumulator.aggregate, row.UsageAggregate)
		if err != nil {
			return nil, 0, fmt.Errorf("merge group model usage: %w", err)
		}
		accumulator.aggregate = summed
	}

	items := make([]groupModelUsageItemResponse, 0, len(ordered))
	for _, accumulator := range ordered {
		// 窗口内既无用量又未处于冷却的组合不下发，避免空行撑爆矩阵。
		if !accumulator.cooling && accumulator.aggregate.RequestCount == 0 &&
			!hasGroupModelUsageTokens(accumulator.aggregate) {
			continue
		}
		item, err := mapGroupModelUsageItem(accumulator)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TotalTokens != items[j].TotalTokens {
			return items[i].TotalTokens > items[j].TotalTokens
		}
		if items[i].CredentialID != items[j].CredentialID {
			return items[i].CredentialID < items[j].CredentialID
		}
		return items[i].Model < items[j].Model
	})
	totalItems := int64(len(items))
	if len(items) > groupModelUsageMaxRows {
		items = items[:groupModelUsageMaxRows]
	}
	return items, totalItems, nil
}

type groupModelUsageKey struct {
	credentialID uint
	model        string
}

func hasGroupModelUsageTokens(aggregate requestlog.UsageAggregate) bool {
	for _, value := range [...]int64{
		aggregate.UncachedInputTokens,
		aggregate.CacheReadTokens,
		aggregate.CacheWrite5MTokens,
		aggregate.CacheWrite1HTokens,
		aggregate.CacheWriteUnknownTokens,
		aggregate.OutputTokens,
	} {
		if value != 0 {
			return true
		}
	}
	return false
}

func mapGroupModelUsageItem(
	accumulator *groupModelUsageAccumulator,
) (groupModelUsageItemResponse, error) {
	aggregate := accumulator.aggregate
	if err := validateGroupModelUsageIntegers(aggregate, accumulator.windowStartMS); err != nil {
		return groupModelUsageItemResponse{}, err
	}
	cooldownUntilMS, err := optionalSafeEpochMilliseconds(accumulator.cycle.CooldownUntil)
	if err != nil {
		return groupModelUsageItemResponse{}, fmt.Errorf("map group model usage cooldown: %w", err)
	}
	// total_tokens 复用 usage 包的既有求和口径，不另立算法。
	totalTokens, ok := usage.CheckedTotal(usage.Tokens{
		UncachedInput:     aggregate.UncachedInputTokens,
		CacheRead:         aggregate.CacheReadTokens,
		CacheWrite5M:      aggregate.CacheWrite5MTokens,
		CacheWrite1H:      aggregate.CacheWrite1HTokens,
		CacheWriteUnknown: aggregate.CacheWriteUnknownTokens,
		Output:            aggregate.OutputTokens,
	})
	if !ok || totalTokens > maxSafeInteger {
		return groupModelUsageItemResponse{}, fmt.Errorf("map group model usage: unsafe total tokens")
	}
	return groupModelUsageItemResponse{
		CredentialID:            accumulator.credentialID,
		Model:                   accumulator.model,
		WindowStartMS:           accumulator.windowStartMS,
		WindowSource:            accumulator.windowSource,
		CooldownUntilMS:         cooldownUntilMS,
		RequestCount:            aggregate.RequestCount,
		SuccessCount:            aggregate.SuccessCount,
		FailureCount:            aggregate.FailureCount,
		UncachedInputTokens:     aggregate.UncachedInputTokens,
		CacheReadTokens:         aggregate.CacheReadTokens,
		CacheWrite5MTokens:      aggregate.CacheWrite5MTokens,
		CacheWrite1HTokens:      aggregate.CacheWrite1HTokens,
		CacheWriteUnknownTokens: aggregate.CacheWriteUnknownTokens,
		OutputTokens:            aggregate.OutputTokens,
		TotalTokens:             totalTokens,
	}, nil
}

func validateGroupModelUsageIntegers(aggregate requestlog.UsageAggregate, windowStartMS int64) error {
	if windowStartMS < 0 || windowStartMS > maxSafeInteger {
		return fmt.Errorf("map group model usage: unsafe window start")
	}
	for _, value := range [...]int64{
		aggregate.RequestCount,
		aggregate.SuccessCount,
		aggregate.FailureCount,
		aggregate.UncachedInputTokens,
		aggregate.CacheReadTokens,
		aggregate.CacheWrite5MTokens,
		aggregate.CacheWrite1HTokens,
		aggregate.CacheWriteUnknownTokens,
		aggregate.OutputTokens,
	} {
		if value < 0 || value > maxSafeInteger {
			return fmt.Errorf("map group model usage: unsafe integer")
		}
	}
	for _, field := range [...]int64{
		aggregate.SuccessCount,
		aggregate.FailureCount,
	} {
		if field > aggregate.RequestCount {
			return fmt.Errorf("map group model usage: inconsistent counts")
		}
	}
	return nil
}

func (server *Server) handleGetGroupModelUsage(c *gin.Context) {
	id, ok := groupID(c, "get_group_model_usage")
	if !ok {
		return
	}
	result, err := server.service.GetGroupModelUsage(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, "get_group_model_usage", err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.SuccessI18n(c, "common.success", result)
}
