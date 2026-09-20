package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/channel"
	"gpt-load/internal/platform/config"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage/models"
)

// 备注只在 /api/modern 通道下发：经典响应多一个未知字段就会让 classic 前端整页
// invalidResponse，因此这里同时断言经典响应没有 note 键与 modern 侧的读写语义。
func TestCredentialNoteStaysOutOfClassicResponseAndHonorsOptionalSemantics(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	mustEnsureInitialPrices(t, fixture)
	created, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{
		Name: stringPointer("credential-note"), ChannelID: channel.OpenAI,
		Params: json.RawMessage(`{}`), Models: optionalGroupModels{Set: true},
		Credentials: "sk-credential-note", ConnectionType: "api_key",
	})
	if err != nil {
		t.Fatal(err)
	}
	var credential models.Credential
	if err := fixture.db.Where("group_id = ?", created.GroupID).Take(&credential).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	NewServer(&config.Config{AuthKey: "test-auth-key"}, fixture.service).RegisterRoutes(engine)
	classicList := fmt.Sprintf("/api/groups/%d/credentials", created.GroupID)
	classicDetail := fmt.Sprintf("%s/%d", classicList, credential.ID)
	modernList := fmt.Sprintf("/api/modern/groups/%d/credentials", created.GroupID)
	modernDetail := fmt.Sprintf("%s/%d", modernList, credential.ID)
	routes := []struct {
		path   string
		list   bool
		modern bool
	}{
		{path: classicList, list: true},
		{path: classicDetail},
		{path: modernList, list: true, modern: true},
		{path: modernDetail, modern: true},
	}
	for _, route := range routes {
		t.Run(route.path+" starts without a note", func(t *testing.T) {
			assertCredentialNote(t, engine, route.path, route.list, route.modern, "")
		})
	}

	// 只传 note 的请求必须被接受，且经典响应始终不出现 note 键。
	for _, step := range []struct {
		name    string
		request string
		want    string
	}{
		{name: "written note", request: `{"note":"上游账号 A"}`, want: "上游账号 A"},
		{name: "unrelated update keeps note", request: `{"weight_manual":25}`, want: "上游账号 A"},
		{name: "empty string clears note", request: `{"note":""}`},
		{name: "note can be set again", request: `{"note":"再次设置"}`, want: "再次设置"},
		{name: "explicit null clears note", request: `{"note":null}`},
		{name: "maximum rune length accepted", request: `{"note":"` + strings.Repeat("账", 2048) + `"}`, want: strings.Repeat("账", 2048)},
	} {
		t.Run(step.name, func(t *testing.T) {
			updateCredentialNote(t, engine, classicDetail, step.request, http.StatusOK, "")
			for _, route := range routes {
				assertCredentialNote(t, engine, route.path, route.list, route.modern, step.want)
			}
		})
	}

	// 逐 rune 计数：2049 个中文字符必须被既有校验错误拒绝，而不是按字节提前截断。
	updateCredentialNote(t, engine, classicDetail,
		`{"note":"`+strings.Repeat("账", 2049)+`"}`, http.StatusBadRequest, app_errors.ErrValidation.Code)
	updateCredentialNote(t, engine, classicDetail, `{}`, http.StatusBadRequest, app_errors.ErrBadRequest.Code)
	for _, route := range routes {
		assertCredentialNote(t, engine, route.path, route.list, route.modern, strings.Repeat("账", 2048))
	}
}

// assertCredentialNote 断言经典响应不含 note 键、modern 响应含期望的备注值。
func assertCredentialNote(
	t *testing.T,
	engine *gin.Engine,
	path string,
	list bool,
	modern bool,
	want string,
) {
	t.Helper()
	item, raw := readGroupDetailLedgerItem(t, engine, path, list)
	note, exists := item["note"]
	if !modern {
		if exists || strings.Contains(raw, `"note":`) {
			t.Fatalf("classic %s exposes the note key: %s", path, raw)
		}
		return
	}
	if !exists || string(note) != mustJSONString(t, want) {
		t.Fatalf("modern %s note = %s, want %q", path, note, want)
	}
}

func updateCredentialNote(
	t *testing.T,
	engine *gin.Engine,
	path string,
	body string,
	wantStatus int,
	wantCode string,
) {
	t.Helper()
	recorder := serveGroupDetailLedgerRoute(t, engine, http.MethodPut, path, body, "Bearer test-auth-key")
	assertGroupDetailLedgerEnvelope(t, recorder, wantStatus, wantCode)
	// 更新响应也是经典格式，modern 前端靠请求值回填备注。
	if strings.Contains(recorder.Body.String(), `"note":`) {
		t.Fatalf("credential update response exposes the note key: %s", recorder.Body.String())
	}
}

// readGroupDetailLedgerItem 返回单条凭据对象与其 JSON 原文，便于同时断言字段与文本。
func readGroupDetailLedgerItem(
	t *testing.T,
	engine *gin.Engine,
	path string,
	list bool,
) (map[string]json.RawMessage, string) {
	t.Helper()
	recorder := serveGroupDetailLedgerRoute(t, engine, http.MethodGet, path, "", "Bearer test-auth-key")
	assertGroupDetailLedgerEnvelope(t, recorder, http.StatusOK, "")
	var envelope struct {
		Data struct {
			Items      []map[string]json.RawMessage `json:"items"`
			Credential map[string]json.RawMessage   `json:"credential"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !list {
		raw, err := json.Marshal(envelope.Data.Credential)
		if err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Credential, string(raw)
	}
	if len(envelope.Data.Items) != 1 {
		t.Fatalf("%s returned %d credentials, want 1", path, len(envelope.Data.Items))
	}
	raw, err := json.Marshal(envelope.Data.Items)
	if err != nil {
		t.Fatal(err)
	}
	return envelope.Data.Items[0], string(raw)
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
