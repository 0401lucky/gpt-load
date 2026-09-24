package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/config"
	"gpt-load/internal/requestlog"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
)

const groupModelUsageModelGLM = "z-ai/glm-5.3-flash"

type groupModelUsageRow struct {
	CredentialID        uint   `json:"credential_id"`
	Model               string `json:"model"`
	WindowStartMS       int64  `json:"window_start_ms"`
	WindowSource        string `json:"window_source"`
	CooldownUntilMS     *int64 `json:"cooldown_until_ms"`
	RequestCount        int64  `json:"request_count"`
	UncachedInputTokens int64  `json:"uncached_input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
}

type groupModelUsagePaginationPayload struct {
	Page       int64 `json:"page"`
	PageSize   int64 `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int64 `json:"total_pages"`
}

type groupModelUsagePayload struct {
	ObservedAtMS  int64                            `json:"observed_at_ms"`
	CountedFromMS int64                            `json:"counted_from_ms"`
	CountedToMS   int64                            `json:"counted_to_ms"`
	Items         []groupModelUsageRow             `json:"items"`
	Pagination    groupModelUsagePaginationPayload `json:"pagination"`
}

func newGroupModelUsageFixture(t *testing.T) (serviceFixture, *models.Group, time.Time) {
	t.Helper()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	now := time.Date(2026, time.September, 24, 6, 30, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return now }
	fixture.service.usageStats = requestlog.NewService(fixture.db, nil, nil)
	group := createGroupCollectionGroup(t, fixture, "model-usage-group", true, nil)
	return fixture, group, now
}

func registerGroupModelUsageCredentials(
	t *testing.T,
	registry *state.CredentialRegistry,
	groupID uint,
	credentialIDs ...uint,
) {
	t.Helper()
	entries := make([]state.CredentialEntry, 0, len(credentialIDs))
	for _, credentialID := range credentialIDs {
		entries = append(entries, state.CredentialEntry{
			ID: credentialID, GroupID: groupID, Version: 1, IdentityGeneration: 1,
			Status: state.CredentialStatusActive, AuthState: state.CredentialAuthStateReady,
			Fingerprint:    fmt.Sprintf("fingerprint-%d", credentialID),
			EncryptedValue: fmt.Sprintf("encrypted-%d", credentialID),
		})
	}
	if err := registry.ReplaceCredentials(entries); err != nil {
		t.Fatalf("registry.ReplaceCredentials() error = %v", err)
	}
}

func mustSetGroupModelCooldown(
	t *testing.T,
	registry *state.CredentialRegistry,
	credentialID uint,
	model string,
	until, now time.Time,
) {
	t.Helper()
	ref, ok := registry.CredentialRef(credentialID)
	if !ok {
		t.Fatalf("credential %d is missing from the registry", credentialID)
	}
	if accepted, changed := registry.SetModelCooldown(ref, model, until, now); !accepted || !changed {
		t.Fatalf("SetModelCooldown(%d, %q, %s, %s) = %t, %t, want accepted change",
			credentialID, model, until, now, accepted, changed)
	}
}

func createGroupModelUsageStat(
	t *testing.T,
	fixture serviceFixture,
	groupID, credentialID uint,
	model string,
	bucketStart time.Time,
	requestCount int64,
) {
	t.Helper()
	if err := fixture.db.Create(&models.UsageStat{
		BucketStartMS: bucketStart.UTC().UnixMilli(), GroupID: groupID, ChannelID: "openai_compatible",
		CredentialID: credentialID, AccessKeyID: 1, Model: model,
		RequestCount: requestCount, SuccessCount: requestCount,
		UncachedInputTokens: requestCount * 10, OutputTokens: requestCount * 2,
	}).Error; err != nil {
		t.Fatalf("create usage stat: %v", err)
	}
}

func requestGroupModelUsage(t *testing.T, engine *gin.Engine, groupID uint) groupModelUsagePayload {
	t.Helper()
	recorder := performGroupCollectionRequest(
		engine,
		fmt.Sprintf("/api/groups/%d/model-usage", groupID),
		"Bearer "+authTestKey,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload groupModelUsagePayload
	if err := json.Unmarshal(decodeGroupCollectionSuccessData(t, recorder), &payload); err != nil {
		t.Fatalf("decode group model usage: %v", err)
	}
	return payload
}

func TestGetGroupModelUsageResolvesEachRowWindowIndependently(t *testing.T) {
	t.Parallel()
	fixture, group, now := newGroupModelUsageFixture(t)
	registerGroupModelUsageCredentials(t, fixture.registry, group.ID, 523, 35, 445)
	// 523 有两段冷却：第一次 429 给出的重置时刻已经发生，因此它成为该组合的周期起点。
	mustSetGroupModelCooldown(t, fixture.registry, 523, groupModelUsageModelGLM,
		now.Add(-3*time.Hour), now.Add(-6*time.Hour))
	mustSetGroupModelCooldown(t, fixture.registry, 523, groupModelUsageModelGLM,
		now.Add(3*time.Hour), now)
	// 35 的窗口起点落在近 24 小时之外，检索下界必须随之下探。
	mustSetGroupModelCooldown(t, fixture.registry, 35, groupModelUsageModelGLM,
		now.Add(-30*time.Hour), now.Add(-40*time.Hour))

	for _, row := range []struct {
		credentialID uint
		model        string
		bucketStart  time.Time
		requestCount int64
	}{
		// 523 的窗口起点是 03:30，整点对齐后 03:00 的桶必须被排除。
		{credentialID: 523, model: groupModelUsageModelGLM, bucketStart: hourBucket(now.Add(-3 * time.Hour)), requestCount: 100},
		{credentialID: 523, model: groupModelUsageModelGLM, bucketStart: hourBucket(now.Add(-2 * time.Hour)), requestCount: 2},
		{credentialID: 523, model: groupModelUsageModelGLM, bucketStart: hourBucket(now.Add(-time.Hour)), requestCount: 3},
		// 35 的窗口起点是前一天 00:30，01:00 的桶计入，证明起点更早的行没有被截断。
		{credentialID: 35, model: groupModelUsageModelGLM, bucketStart: hourBucket(now.Add(-30 * time.Hour)).Add(time.Hour), requestCount: 7},
		// 445 没有周期记录，按近 24 小时兜底；更早的桶不计入。
		{credentialID: 445, model: "cline-free/mimo-v2.6-flash", bucketStart: hourBucket(now.Add(-time.Hour)), requestCount: 4},
		{credentialID: 445, model: "cline-free/mimo-v2.6-flash", bucketStart: hourBucket(now.Add(-48 * time.Hour)), requestCount: 50},
		// 不属于该分组的凭据不进入矩阵。
		{credentialID: 999, model: groupModelUsageModelGLM, bucketStart: hourBucket(now.Add(-time.Hour)), requestCount: 9},
	} {
		createGroupModelUsageStat(t, fixture, group.ID, row.credentialID, row.model, row.bucketStart, row.requestCount)
	}

	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	payload := requestGroupModelUsage(t, engine, group.ID)

	if payload.ObservedAtMS != now.UnixMilli() || payload.CountedToMS != hourBucket(now).UnixMilli() {
		t.Fatalf("counted interval = [%d, %d), want observed %d and aligned end %d",
			payload.CountedFromMS, payload.CountedToMS, now.UnixMilli(), hourBucket(now).UnixMilli())
	}
	if want := hourBucket(now.Add(-30 * time.Hour)).Add(time.Hour).UnixMilli(); payload.CountedFromMS != want {
		t.Fatalf("counted_from_ms = %d, want %d (aligned earliest window start)", payload.CountedFromMS, want)
	}
	if payload.Pagination.TotalItems != 3 || payload.Pagination.Page != 1 ||
		payload.Pagination.PageSize != groupModelUsageMaxRows || payload.Pagination.TotalPages != 1 {
		t.Fatalf("pagination = %+v, want one full page of 3 rows", payload.Pagination)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %+v, want 3 rows", payload.Items)
	}

	rows := map[uint]groupModelUsageRow{}
	for _, row := range payload.Items {
		rows[row.CredentialID] = row
	}
	cooling := rows[523]
	if cooling.WindowStartMS != now.Add(-3*time.Hour).UnixMilli() || cooling.WindowSource != "reset" ||
		cooling.RequestCount != 5 || cooling.UncachedInputTokens != 50 || cooling.OutputTokens != 10 ||
		cooling.TotalTokens != 60 {
		t.Fatalf("523 usage = %+v, want the 03:00 bucket excluded", cooling)
	}
	if cooling.CooldownUntilMS == nil || *cooling.CooldownUntilMS != now.Add(3*time.Hour).UnixMilli() {
		t.Fatalf("523 cooldown = %v, want %s", cooling.CooldownUntilMS, now.Add(3*time.Hour))
	}
	older := rows[35]
	if older.WindowStartMS != now.Add(-30*time.Hour).UnixMilli() || older.WindowSource != "reset" ||
		older.RequestCount != 7 || older.TotalTokens != 7*12 || older.CooldownUntilMS != nil {
		t.Fatalf("35 usage = %+v, want the pre-24h window counted and no cooldown", older)
	}
	fallback := rows[445]
	if fallback.WindowStartMS != now.Add(-24*time.Hour).UnixMilli() ||
		fallback.WindowSource != "fallback_24h" || fallback.RequestCount != 4 || fallback.TotalTokens != 48 {
		t.Fatalf("445 usage = %+v, want the 24h fallback window", fallback)
	}
	// 排序：默认按总 token 降序。
	if payload.Items[0].CredentialID != 35 || payload.Items[2].CredentialID != 445 {
		t.Fatalf("items order = %+v, want descending total tokens", payload.Items)
	}
}

func TestGetGroupModelUsageKeepsCoolingRowsWithoutUsage(t *testing.T) {
	t.Parallel()
	fixture, group, now := newGroupModelUsageFixture(t)
	registerGroupModelUsageCredentials(t, fixture.registry, group.ID, 523)
	mustSetGroupModelCooldown(t, fixture.registry, 523, groupModelUsageModelGLM, now.Add(2*time.Hour), now)

	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	payload := requestGroupModelUsage(t, engine, group.ID)

	if len(payload.Items) != 1 {
		t.Fatalf("items = %+v, want the cooling row only", payload.Items)
	}
	row := payload.Items[0]
	// 首次冷却没有上一周期起点，只能退回兜底口径，但冷却状态必须照常下发。
	if row.WindowSource != "fallback_24h" || row.WindowStartMS != now.Add(-24*time.Hour).UnixMilli() ||
		row.CooldownUntilMS == nil || *row.CooldownUntilMS != now.Add(2*time.Hour).UnixMilli() ||
		row.TotalTokens != 0 {
		t.Fatalf("cooling row = %+v", row)
	}
}

func TestGetGroupModelUsageDropsRowsWithoutUsageOrCooldown(t *testing.T) {
	t.Parallel()
	fixture, group, now := newGroupModelUsageFixture(t)
	registerGroupModelUsageCredentials(t, fixture.registry, group.ID, 523)
	// 用量全部落在兜底窗口之外，且该组合没有冷却，因此不下发空行。
	createGroupModelUsageStat(t, fixture, group.ID, 523, groupModelUsageModelGLM,
		hourBucket(now.Add(-72*time.Hour)), 12)

	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	payload := requestGroupModelUsage(t, engine, group.ID)

	if len(payload.Items) != 0 || payload.Pagination.TotalItems != 0 {
		t.Fatalf("payload = %+v, want no rows", payload)
	}
	// 无行时 counted 区间仍按兜底窗口下发，便于前端解释空状态。
	if payload.CountedFromMS != hourBucket(now.Add(-24*time.Hour)).Add(time.Hour).UnixMilli() ||
		payload.CountedToMS != hourBucket(now).UnixMilli() {
		t.Fatalf("counted interval = [%d, %d)", payload.CountedFromMS, payload.CountedToMS)
	}
}

func TestGetGroupModelUsageResponseContractFields(t *testing.T) {
	t.Parallel()
	fixture, group, now := newGroupModelUsageFixture(t)
	registerGroupModelUsageCredentials(t, fixture.registry, group.ID, 523)
	createGroupModelUsageStat(t, fixture, group.ID, 523, groupModelUsageModelGLM,
		hourBucket(now.Add(-time.Hour)), 2)

	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	recorder := performGroupCollectionRequest(
		engine,
		fmt.Sprintf("/api/groups/%d/model-usage", group.ID),
		"Bearer "+authTestKey,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(decodeGroupCollectionSuccessData(t, recorder), &payload); err != nil {
		t.Fatal(err)
	}
	// 字段集合与两套前端投影白名单一一对应，漏登记会让 classic 整页报错。
	assertJSONFields(t, payload, []string{
		"observed_at_ms", "counted_from_ms", "counted_to_ms", "items", "pagination",
	})
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(payload["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want one row", items)
	}
	assertJSONFields(t, items[0], []string{
		"credential_id", "model", "window_start_ms", "window_source", "cooldown_until_ms",
		"request_count", "success_count", "failure_count",
		"uncached_input_tokens", "cache_read_tokens", "cache_write_5m_tokens",
		"cache_write_1h_tokens", "cache_write_unknown_tokens", "output_tokens", "total_tokens",
	})
	if string(items[0]["cooldown_until_ms"]) != "null" {
		t.Fatalf("cooldown_until_ms = %s, want null when the row is not cooling", items[0]["cooldown_until_ms"])
	}
	var pagination map[string]json.RawMessage
	if err := json.Unmarshal(payload["pagination"], &pagination); err != nil {
		t.Fatal(err)
	}
	assertJSONFields(t, pagination, []string{"page", "page_size", "total_items", "total_pages"})
}

func TestGetGroupModelUsageRejectsUnknownGroupAndInvalidIdentifiers(t *testing.T) {
	t.Parallel()
	fixture, _, _ := newGroupModelUsageFixture(t)
	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)

	for _, target := range []string{"/api/groups/0/model-usage", "/api/groups/not-a-number/model-usage"} {
		recorder := performGroupCollectionRequest(engine, target, "Bearer "+authTestKey)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", target, recorder.Code)
		}
	}
	recorder := performGroupCollectionRequest(engine, "/api/groups/4321/model-usage", "Bearer "+authTestKey)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown group status = %d, want 404: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "NOT_FOUND" {
		t.Fatalf("unknown group code = %q, want NOT_FOUND", envelope.Code)
	}
	unauthorized := performGroupCollectionRequest(engine, "/api/groups/1/model-usage", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
}

func TestGetGroupModelUsageRequiresTheUsageReader(t *testing.T) {
	t.Parallel()
	fixture, group, _ := newGroupModelUsageFixture(t)
	fixture.service.usageStats = nil
	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	recorder := performGroupCollectionRequest(
		engine,
		fmt.Sprintf("/api/groups/%d/model-usage", group.ID),
		"Bearer "+authTestKey,
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 without a usage reader: %s", recorder.Code, recorder.Body.String())
	}
}

// 周期起点只在设置冷却时迁移，所以在重置已发生但尚未再次 429 的间隙里，
// 窗口起点必须取更晚的那个已发生重置，否则会把上一周期的用量算进本窗口。
func TestGroupModelUsageWindowUsesTheMostRecentOccurredReset(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 24, 6, 30, 0, 0, time.UTC)
	nowMS := now.UnixMilli()
	cycleStart := now.Add(-20 * time.Hour)
	occurredReset := now.Add(-2 * time.Hour)

	for _, test := range []struct {
		name       string
		cycle      state.CredentialModelCycleView
		wantStart  int64
		wantSource string
	}{
		{
			name:       "cycle start only",
			cycle:      state.CredentialModelCycleView{CycleStart: cycleStart},
			wantStart:  cycleStart.UnixMilli(),
			wantSource: groupModelUsageWindowSourceReset,
		},
		{
			name:       "cooling keeps the previous cycle start",
			cycle:      state.CredentialModelCycleView{CycleStart: cycleStart, NextReset: now.Add(3 * time.Hour)},
			wantStart:  cycleStart.UnixMilli(),
			wantSource: groupModelUsageWindowSourceReset,
		},
		{
			name:       "reset already happened outranks the stale cycle start",
			cycle:      state.CredentialModelCycleView{CycleStart: cycleStart, NextReset: occurredReset},
			wantStart:  occurredReset.UnixMilli(),
			wantSource: groupModelUsageWindowSourceReset,
		},
		{
			name:       "first reset already happened",
			cycle:      state.CredentialModelCycleView{NextReset: occurredReset},
			wantStart:  occurredReset.UnixMilli(),
			wantSource: groupModelUsageWindowSourceReset,
		},
		{
			name:       "first cooling without anchors falls back",
			cycle:      state.CredentialModelCycleView{NextReset: now.Add(3 * time.Hour)},
			wantStart:  now.Add(-24 * time.Hour).UnixMilli(),
			wantSource: groupModelUsageWindowSourceFallback24h,
		},
		{
			name:       "rejects non-positive anchors from a corrupt checkpoint",
			cycle:      state.CredentialModelCycleView{CycleStart: time.Unix(-5, 0), NextReset: time.Unix(-5, 0)},
			wantStart:  now.Add(-24 * time.Hour).UnixMilli(),
			wantSource: groupModelUsageWindowSourceFallback24h,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotStart, gotSource := groupModelUsageWindow(test.cycle, nowMS)
			if gotStart != test.wantStart || gotSource != test.wantSource {
				t.Fatalf("groupModelUsageWindow() = (%d, %q), want (%d, %q)",
					gotStart, gotSource, test.wantStart, test.wantSource)
			}
		})
	}
}

func hourBucket(value time.Time) time.Time {
	return value.UTC().Truncate(time.Hour)
}

func assertJSONFields(t *testing.T, raw map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(raw))
	for field := range raw {
		got = append(got, field)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if len(got) != len(sorted) {
		t.Fatalf("fields = %v, want %v", got, sorted)
	}
	for index := range got {
		if got[index] != sorted[index] {
			t.Fatalf("fields = %v, want %v", got, sorted)
		}
	}
}
