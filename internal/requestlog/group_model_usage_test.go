package requestlog

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

func groupModelUsageStat(credentialID uint, model string, bucket time.Time, requestCount int64) models.UsageStat {
	row := usageStat(bucket, 17, model, requestCount)
	row.CredentialID = credentialID
	return row
}

func TestQueryGroupModelUsageGroupsRowsByCredentialModelAndHourBucket(t *testing.T) {
	db := openRequestLogQueryDB(t)
	service := newRequestLogTestService(db)
	now := time.Date(2026, time.September, 24, 6, 30, 0, 0, time.UTC)
	from := now.Add(-30 * time.Hour).Truncate(time.Hour)

	otherGroup := usageStat(from, 18, "model-a", 11)
	otherGroup.CredentialID = 31
	createUsageStats(t, db,
		groupModelUsageStat(31, "model-a", from, 2),
		groupModelUsageStat(31, "model-a", from.Add(time.Hour), 3),
		groupModelUsageStat(31, "model-b", from, 5),
		groupModelUsageStat(32, "model-a", from, 7),
		groupModelUsageStat(31, "", from, 13),
		groupModelUsageStat(31, "model-a", from.Add(-time.Hour), 17),
		otherGroup,
	)

	toMS := now.Truncate(time.Hour).UnixMilli()
	got, err := service.QueryGroupModelUsage(t.Context(), 17, from.UnixMilli(), toMS)
	if err != nil {
		t.Fatalf("QueryGroupModelUsage() error = %v", err)
	}
	type key struct {
		credentialID uint
		model        string
		bucketMS     int64
	}
	gotRows := make(map[key]int64, len(got))
	for _, row := range got {
		gotRows[key{row.CredentialID, row.Model, row.BucketStartMS}] = row.RequestCount
	}
	// 空模型名、越界的桶与其他分组的行都不进入矩阵。
	want := map[key]int64{
		{31, "model-a", from.UnixMilli()}:                2,
		{31, "model-a", from.Add(time.Hour).UnixMilli()}: 3,
		{31, "model-b", from.UnixMilli()}:                5,
		{32, "model-a", from.UnixMilli()}:                7,
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v, want %+v", gotRows, want)
	}
	for expected, requestCount := range want {
		if gotRows[expected] != requestCount {
			t.Fatalf("row %+v = %d, want %d", expected, gotRows[expected], requestCount)
		}
	}
	for index := 1; index < len(got); index++ {
		previous, current := got[index-1], got[index]
		if previous.CredentialID > current.CredentialID ||
			(previous.CredentialID == current.CredentialID && previous.Model > current.Model) ||
			(previous.CredentialID == current.CredentialID && previous.Model == current.Model &&
				previous.BucketStartMS >= current.BucketStartMS) {
			t.Fatalf("rows are not ordered by credential, model, bucket: %+v", got)
		}
	}
}

func TestQueryGroupModelUsageReadsTheBucketRangeInOneRowQuery(t *testing.T) {
	db := openRequestLogQueryDB(t)
	service := newRequestLogTestService(db)
	now := time.Date(2026, time.September, 24, 6, 30, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour).Truncate(time.Hour)
	created := make([]models.UsageStat, 0, 24)
	for index := 0; index < 24; index++ {
		created = append(created, groupModelUsageStat(uint(31+index), "model-a",
			from.Add(time.Duration(index)*time.Hour), 1))
	}
	createUsageStats(t, db, created...)

	usageStatQueries := 0
	const callbackName = "test:group_model_usage_query_count"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_stats" {
			usageStatQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callbackName); err != nil {
			t.Errorf("remove query callback: %v", err)
		}
	})

	got, err := service.QueryGroupModelUsage(t.Context(), 17, from.UnixMilli(), now.Truncate(time.Hour).UnixMilli())
	if err != nil {
		t.Fatalf("QueryGroupModelUsage() error = %v", err)
	}
	if len(got) != len(created) {
		t.Fatalf("rows = %d, want %d", len(got), len(created))
	}
	// 一次完整性校验加一次行查询；逐行发 SQL 会让这个计数随行数增长。
	if usageStatQueries != 2 {
		t.Fatalf("usage_stats queries = %d, want 2", usageStatQueries)
	}
}

func TestQueryGroupModelUsageRejectsInvalidScope(t *testing.T) {
	service := newRequestLogTestService(openRequestLogQueryDB(t))
	for _, test := range []struct {
		name         string
		groupID      uint
		fromMS, toMS int64
	}{
		{name: "zero group", groupID: 0, fromMS: 0, toMS: 1},
		{name: "negative start", groupID: 17, fromMS: -1, toMS: 1},
		{name: "empty range", groupID: 17, fromMS: 5, toMS: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.QueryGroupModelUsage(t.Context(), test.groupID, test.fromMS, test.toMS); err == nil {
				t.Fatal("QueryGroupModelUsage() accepted an invalid scope")
			}
		})
	}
	if _, err := NewService(nil, nil, nil).QueryGroupModelUsage(t.Context(), 17, 0, 1); err == nil {
		t.Fatal("QueryGroupModelUsage() accepted a nil database")
	}
}

func TestQueryGroupModelUsageRejectsCorruptRows(t *testing.T) {
	db := openRequestLogQueryDB(t)
	service := newRequestLogTestService(db)
	now := time.Date(2026, time.September, 24, 6, 30, 0, 0, time.UTC)
	corrupt := groupModelUsageStat(31, "model-a", now.Add(-time.Hour).Truncate(time.Hour), 1)
	corrupt.SuccessCount = 5
	createCorruptUsageStats(t, db, corrupt)

	if _, err := service.QueryGroupModelUsage(t.Context(), 17,
		now.Add(-2*time.Hour).UnixMilli(), now.UnixMilli()); err == nil {
		t.Fatal("QueryGroupModelUsage() accepted a corrupt row")
	}
}
