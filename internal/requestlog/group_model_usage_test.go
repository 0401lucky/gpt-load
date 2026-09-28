package requestlog

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

func TestGroupModelWindowUsageRestoresBurstAndDeduplicatesBoundaries(t *testing.T) {
	for _, sameHour := range []bool{true, false} {
		t.Run(fmt.Sprint(sameHour), func(t *testing.T) {
			db := openRequestLogQueryDB(t)
			service := newRequestLogTestService(db)
			start := time.Date(2026, 9, 28, 9, 43, 0, 0, time.UTC)
			end := start.Add(15 * time.Minute)
			if !sameHour {
				end = start.Add(3 * time.Hour)
			}
			service.now = func() time.Time { return end }
			const total = int64(15662497)
			for i := 0; i < 112; i++ {
				amount := total / 112
				if i == 111 {
					amount += total % 112
				}
				row := requestLogWindowRow(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1000), 31, start.Add(time.Duration(i)*time.Second), amount)
				row.UpstreamModel = "burst"
				row.OutputTokens = 0
				createRequestLogQueryRow(t, db, row)
			}
			createUsageStats(t, db, groupModelUsageStat(31, "burst", start.Truncate(time.Hour), 112))
			queries := 0
			const callback = "test:window_batch_queries"
			if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "usage_stats" || tx.Statement.Table == "request_logs" {
					queries++
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
			starts := map[GroupModelUsageKey]int64{}
			ids := map[uint]bool{}
			for i := uint(31); i < 131; i++ {
				ids[i] = true
				starts[GroupModelUsageKey{i, "burst"}] = start.UnixMilli()
			}
			got, err := service.QueryGroupModelWindowUsage(t.Context(), 17, end.Add(-24*time.Hour).UnixMilli(), end.UnixMilli(), starts, ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range got {
				if row.CredentialID == 31 && (row.RequestCount != 112 || row.SuccessCount != 112 || row.UncachedInputTokens != total || row.DataIncomplete) {
					t.Fatalf("burst = %+v", row)
				}
			}
			if len(got) != 100 || queries != 3 {
				t.Fatalf("rows=%d queries=%d", len(got), queries)
			}
		})
	}
}

func TestGroupModelWindowUsageMergesExactEdgesAndRetainsIncompleteRows(t *testing.T) {
	db := openRequestLogQueryDB(t)
	service := newRequestLogTestService(db)
	start := time.Date(2026, 9, 28, 9, 43, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	service.now = func() time.Time { return end }
	for i, at := range []time.Time{start.Add(-time.Minute), start, start.Add(time.Hour), end.Add(-time.Minute), end} {
		row := requestLogWindowRow(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+2000), 31, at, 10)
		row.UpstreamModel = "a"
		if i == 3 {
			row.Status = "incomplete"
			row.StatusCode = 200
		}
		createRequestLogQueryRow(t, db, row)
	}
	createUsageStats(t, db, groupModelUsageStat(31, "a", start.Add(time.Hour).Truncate(time.Hour), 1))
	// Current-hour model has no aggregate yet, and uses fallback.
	newRow := requestLogWindowRow("00000000-0000-4000-8000-000000009999", 32, end.Add(-time.Minute), 20)
	newRow.UpstreamModel = "new"
	createRequestLogQueryRow(t, db, newRow)
	old := start.Add(-40 * 24 * time.Hour)
	createUsageStats(t, db, groupModelUsageStat(33, "expired", old.Truncate(time.Hour), 1))
	got, err := service.QueryGroupModelWindowUsage(t.Context(), 17, end.Add(-24*time.Hour).UnixMilli(), end.UnixMilli(), map[GroupModelUsageKey]int64{{31, "a"}: start.UnixMilli(), {33, "expired"}: old.UnixMilli()}, map[uint]bool{31: true, 32: true, 33: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows=%+v", got)
	}
	for _, row := range got {
		switch row.Model {
		case "a":
			if row.RequestCount != 3 || row.SuccessCount != 2 || row.FailureCount != 1 || row.DataIncomplete {
				t.Fatalf("a=%+v", row)
			}
		case "new":
			if row.RequestCount != 1 || row.UncachedInputTokens != 20 {
				t.Fatalf("new=%+v", row)
			}
		case "expired":
			if !row.DataIncomplete || row.RequestCount != 0 {
				t.Fatalf("expired=%+v", row)
			}
		}
	}
}

func groupModelUsageStat(credentialID uint, model string, bucket time.Time, requestCount int64) models.UsageStat {
	row := usageStat(bucket, 17, model, requestCount)
	row.CredentialID = credentialID
	return row
}

func TestGroupModelWindowUsageUsesOneReadSnapshot(t *testing.T) {
	db, dsn := openRequestLogFileDB(t)
	service := newRequestLogTestService(db)
	now := time.Date(2026, 9, 28, 12, 25, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	from := now.Add(-5 * time.Hour)
	createUsageStats(t, db, groupModelUsageStat(42, "codex-a", from.Add(time.Hour).Truncate(time.Hour), 1))
	writer, closeWriter := openUsageQueryWriterDB(t, dsn)
	t.Cleanup(closeWriter)
	inserted := false
	const callback = "test:group_model_window_snapshot"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if inserted || tx.Statement.Table != "usage_stats" {
			return
		}
		inserted = true
		late := requestLogWindowRow("00000000-0000-4000-8000-000000007777", 42, from.Add(time.Minute), 40)
		if err := writer.Create(&late).Error; err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	got, err := service.QueryGroupModelWindowUsage(t.Context(), 17, from.UnixMilli(), now.UnixMilli(), nil, map[uint]bool{42: true})
	if err != nil {
		t.Fatal(err)
	}
	if !inserted || len(got) != 1 || got[0].RequestCount != 1 {
		t.Fatalf("snapshot=%+v inserted=%v", got, inserted)
	}
}

func TestGroupModelWindowUsageRejectsUnsafeWindows(t *testing.T) {
	service := newRequestLogTestService(openRequestLogQueryDB(t))
	for _, start := range []int64{-1, 101, 1<<63 - 1} {
		if _, err := service.QueryGroupModelWindowUsage(t.Context(), 17, 0, 100, map[GroupModelUsageKey]int64{{31, "a"}: start}, map[uint]bool{31: true}); err == nil {
			t.Fatalf("accepted start %d", start)
		}
	}
}

func TestGroupModelWindowUsageAlignedExpiredWindowsKeepAllTokenFields(t *testing.T) {
	db := openRequestLogQueryDB(t)
	service := newRequestLogTestService(db)
	start := time.Date(2026, 8, 1, 23, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	service.now = func() time.Time { return end.Add(40 * 24 * time.Hour) }
	for i := 0; i < 2; i++ {
		row := groupModelUsageStat(31, "all-tokens", start.Add(time.Duration(i)*time.Hour), 1)
		row.UncachedInputTokens, row.CacheReadTokens, row.CacheWrite5MTokens = 1, 2, 3
		row.CacheWrite1HTokens, row.CacheWriteUnknownTokens, row.OutputTokens = 4, 5, 6
		createUsageStats(t, db, row)
	}
	got, err := service.QueryGroupModelWindowUsage(t.Context(), 17, start.UnixMilli(), end.UnixMilli(), nil, map[uint]bool{31: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows=%+v", got)
	}
	row := got[0]
	if row.DataIncomplete || row.RequestCount != 2 || row.SuccessCount != 2 ||
		row.UncachedInputTokens != 2 || row.CacheReadTokens != 4 || row.CacheWrite5MTokens != 6 ||
		row.CacheWrite1HTokens != 8 || row.CacheWriteUnknownTokens != 10 || row.OutputTokens != 12 {
		t.Fatalf("aligned expired window=%+v", row)
	}
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
