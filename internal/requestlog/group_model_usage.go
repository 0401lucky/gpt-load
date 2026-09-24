package requestlog

import (
	"context"
	"fmt"

	"gorm.io/gorm"

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
