package requestlog

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"gpt-load/internal/execution"
	"gpt-load/internal/platform/epochms"
	"gpt-load/internal/storage/dbtx"
	"gpt-load/internal/storage/models"
)

// GroupModelUsageBucket 是 usage_stats 的一行：某个 (凭据, 上游模型) 在一个小时桶内的用量。
// 矩阵中每行的额度窗口起点不同，因此这里保留桶粒度交给调用方按行裁剪。
type GroupModelUsageBucket struct {
	CredentialID  uint
	Model         string
	BucketStartMS int64
	UsageAggregate
}

// GroupModelWindowUsage is the exact window total, including retained boundaries.
type GroupModelWindowUsage struct {
	CredentialID          uint
	Model                 string
	DataIncomplete        bool
	BoundaryUsageEvidence bool
	UsageAggregate
}

type GroupModelUsageKey struct {
	CredentialID uint
	Model        string
}

// QueryGroupModelWindowUsage reads candidate buckets and precise boundaries in
// one snapshot. Windows absent from starts use fallbackMS; only current IDs pass.
func (service *Service) QueryGroupModelWindowUsage(ctx context.Context, groupID uint, fallbackMS, toMS int64,
	starts map[GroupModelUsageKey]int64, credentials map[uint]bool,
) ([]GroupModelWindowUsage, error) {
	const maxSafeMilliseconds int64 = 1<<53 - 1
	if service == nil || service.db == nil || groupID == 0 || fallbackMS < 0 || toMS <= fallbackMS || toMS > maxSafeMilliseconds {
		return nil, fmt.Errorf("query group model windows: invalid scope")
	}
	const hour = epochms.MillisecondsPerHour
	earliest := fallbackMS
	for _, start := range starts {
		if start < 0 || start > toMS {
			return nil, fmt.Errorf("query group model windows: invalid start")
		}
		earliest = min(earliest, start)
	}
	result := map[GroupModelUsageKey]*GroupModelWindowUsage{}
	window := func(key GroupModelUsageKey) int64 {
		if start, ok := starts[key]; ok {
			return start
		}
		return fallbackMS
	}
	boundaryHours := map[int64]bool{}
	ensure := func(key GroupModelUsageKey) *GroupModelWindowUsage {
		if row := result[key]; row != nil {
			return row
		}
		start := window(key)
		row := &GroupModelWindowUsage{CredentialID: key.CredentialID, Model: key.Model}
		result[key] = row
		if start < toMS {
			firstEnd := min((start+hour-1)/hour*hour, toMS)
			lastStart := max(toMS/hour*hour, firstEnd)
			for _, edge := range [][2]int64{{start, firstEnd}, {lastStart, toMS}} {
				if edge[0] < edge[1] {
					boundaryHours[edge[0]/hour*hour] = true
					row.DataIncomplete = row.DataIncomplete || !service.requestLogWindowRetained(edge[0])
				}
			}
		}
		return row
	}
	err := dbtx.Run(ctx, service.db, dbtx.Options{Mode: dbtx.ReadSnapshot, CleanupTimeout: usageRollbackTimeout,
		Operation: "group model window usage read transaction"}, func(db *gorm.DB) error {
		var buckets []GroupModelUsageBucket
		scope := db.Model(&models.UsageStat{}).Where("group_id = ? AND model <> ?", groupID, "").
			Where("bucket_start_ms >= ? AND bucket_start_ms < ?", earliest/hour*hour, toMS)
		if err := validateUsageStatIntegrity(scope.Session(&gorm.Session{})); err != nil {
			return err
		}
		if err := scope.Select("credential_id, model, bucket_start_ms, " + usageAggregateSelect).
			Group("credential_id, model, bucket_start_ms").Find(&buckets).Error; err != nil {
			return err
		}
		for key := range starts {
			if credentials[key.CredentialID] {
				ensure(key)
			}
		}
		for _, bucket := range buckets {
			if !credentials[bucket.CredentialID] {
				continue
			}
			key := GroupModelUsageKey{bucket.CredentialID, bucket.Model}
			start := window(key)
			if bucket.BucketStartMS+hour <= start {
				continue
			}
			if err := validateUsageAggregate(bucket.UsageAggregate); err != nil {
				return err
			}
			row := ensure(key)
			row.BoundaryUsageEvidence = row.BoundaryUsageEvidence || bucket.RequestCount > 0 ||
				bucket.UncachedInputTokens > 0 || bucket.CacheReadTokens > 0 || bucket.CacheWrite5MTokens > 0 ||
				bucket.CacheWrite1HTokens > 0 || bucket.CacheWriteUnknownTokens > 0 || bucket.OutputTokens > 0
			if bucket.BucketStartMS >= (start+hour-1)/hour*hour && bucket.BucketStartMS < toMS/hour*hour {
				var err error
				row.UsageAggregate, err = AddUsageAggregates(row.UsageAggregate, bucket.UsageAggregate)
				if err != nil {
					return err
				}
			}
		}
		// The fallback/current hour also discovers new models before an hourly row exists.
		if fallbackMS%hour != 0 {
			boundaryHours[fallbackMS/hour*hour] = true
		}
		if toMS%hour != 0 {
			boundaryHours[toMS/hour*hour] = true
		}
		hours := make([]int64, 0, len(boundaryHours))
		for h := range boundaryHours {
			hours = append(hours, h)
		}
		sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
		intervals := [][2]int64{}
		for _, h := range hours {
			if len(intervals) > 0 && intervals[len(intervals)-1][1] == h {
				intervals[len(intervals)-1][1] = h + hour
			} else {
				intervals = append(intervals, [2]int64{h, h + hour})
			}
		}
		// Bound SQL parameters independently of the number of credentials.
		for offset := 0; offset < len(intervals); offset += 200 {
			predicate := db.Where("1 = 0")
			for _, edge := range intervals[offset:min(offset+200, len(intervals))] {
				predicate = predicate.Or("completed_at_ms >= ? AND completed_at_ms < ?", edge[0], min(edge[1], toMS))
			}
			var logs []struct {
				CredentialID  uint
				Model         string
				CompletedAtMS int64
				UsageAggregate
			}
			if err := db.Model(&models.RequestLog{}).Where("group_id = ? AND upstream_model <> ? AND operation <> ?", groupID, "", string(execution.OperationWebSearch)).Where(predicate).
				Select("credential_id, upstream_model AS model, completed_at_ms, " + credentialRequestLogUsageSelect).
				Group("credential_id, upstream_model, completed_at_ms").Find(&logs).Error; err != nil {
				return err
			}
			for _, log := range logs {
				if !credentials[log.CredentialID] {
					continue
				}
				key := GroupModelUsageKey{log.CredentialID, log.Model}
				start := window(key)
				if log.CompletedAtMS < start || log.CompletedAtMS >= toMS {
					continue
				}
				if log.CompletedAtMS >= (start+hour-1)/hour*hour && log.CompletedAtMS < toMS/hour*hour {
					continue
				}
				row := ensure(key)
				var err error
				row.UsageAggregate, err = AddUsageAggregates(row.UsageAggregate, log.UsageAggregate)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("query group model windows: %w", err)
	}
	rows := make([]GroupModelWindowUsage, 0, len(result))
	for _, row := range result {
		rows = append(rows, *row)
	}
	return rows, nil
}

// QueryGroupModelUsage 一次取回分组在桶区间内按 (凭据, 上游模型, 小时桶) 的用量行。
// 调用方在内存里按行计算窗口后求和，禁止逐行发 SQL。
func (service *Service) QueryGroupModelUsage(
	ctx context.Context,
	groupID uint,
	fromMS, toMS int64,
) ([]GroupModelUsageBucket, error) {
	if service == nil || service.db == nil {
		return nil, fmt.Errorf("query group model usage: database is nil")
	}
	if groupID == 0 || fromMS < 0 || toMS <= fromMS {
		return nil, fmt.Errorf("query group model usage: invalid scope")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var rows []GroupModelUsageBucket
	err := dbtx.Run(ctx, service.db, dbtx.Options{
		Mode:           dbtx.ReadSnapshot,
		CleanupTimeout: usageRollbackTimeout,
		Operation:      "group model usage read transaction",
	}, func(connection *gorm.DB) error {
		scope := connection.Model(&models.UsageStat{}).
			Where("group_id = ?", groupID).
			Where("bucket_start_ms >= ? AND bucket_start_ms < ?", fromMS, toMS).
			// 矩阵以上游模型 ID 为行键，空模型名不是可展示的组合。
			Where("model <> ?", "")
		if err := validateUsageStatIntegrity(scope.Session(&gorm.Session{})); err != nil {
			return err
		}
		if err := scope.
			Select("credential_id, model, bucket_start_ms, " + usageAggregateSelect).
			Group("credential_id, model, bucket_start_ms").
			Order("credential_id ASC, model ASC, bucket_start_ms ASC").
			Find(&rows).Error; err != nil {
			return fmt.Errorf("query group model usage rows: %w", err)
		}
		for index := range rows {
			if err := validateUsageAggregate(rows[index].UsageAggregate); err != nil {
				return fmt.Errorf("validate group model usage row: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("query group model usage: %w", err)
	}
	return rows, nil
}
