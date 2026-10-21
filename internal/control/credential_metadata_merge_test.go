package control

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/channel"
	"gpt-load/internal/platform/config"
	"gpt-load/internal/storage/models"
)

func TestCredentialNameAndNoteUpdatesPreserveIdentityHealthAndModelCycles(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	created, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{
		Name: stringPointer("credential metadata"), ChannelID: channel.OpenAI,
		ConnectionType: models.ConnectionTypeAPIKey, Models: optionalGroupModels{Set: true},
		Credentials: "credential-metadata-test-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	var before models.Credential
	if err := fixture.db.Where("group_id = ?", created.GroupID).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture.service.now = func() time.Time { return now }
	ref, exists := fixture.registry.CredentialRef(before.ID)
	if !exists {
		t.Fatal("missing credential ref")
	}
	for index, observation := range []struct {
		at, cooldown, reset time.Time
	}{
		{now.Add(-2 * time.Hour), now.Add(-time.Hour), now.Add(-time.Hour)},
		{now, now.Add(time.Hour), now.Add(2 * time.Hour)},
	} {
		setCooldown := fixture.registry.SetModelCooldownWithReset
		if index == 1 {
			setCooldown = fixture.registry.SetQuotaCooldownWithReset
		}
		if accepted, changed := setCooldown(ref, "model-a", observation.cooldown, observation.reset, observation.at); !accepted || !changed {
			t.Fatal("record model cooldown and credible reset")
		}
	}
	if !fixture.registry.SetBlacklisted(before.ID) {
		t.Fatal("blacklist credential")
	}
	runtimeBefore, err := fixture.registry.SnapshotGroupCredentialEntriesExact(created.GroupID, []uint{before.ID})
	if err != nil || len(runtimeBefore) != 1 {
		t.Fatalf("snapshot credential: %v", err)
	}
	if !runtimeBefore[0].ModelCycleStarts["model-a"].Equal(now.Add(-time.Hour)) ||
		!runtimeBefore[0].ModelNextResets["model-a"].Equal(now.Add(2*time.Hour)) {
		t.Fatal("fixture has no credible model cycle")
	}
	engine := gin.New()
	NewServer(&config.Config{AuthKey: "test-auth-key"}, fixture.service).RegisterRoutes(engine)
	classicList := fmt.Sprintf("/api/groups/%d/credentials", created.GroupID)
	classicDetail := fmt.Sprintf("%s/%d", classicList, before.ID)
	modernList := fmt.Sprintf("/api/modern/groups/%d/credentials", created.GroupID)
	modernDetail := fmt.Sprintf("%s/%d", modernList, before.ID)
	previousUpdatedAt := before.UpdatedAtMS
	for _, step := range []struct {
		name, body, wantName, wantNote string
		invalid                        bool
	}{
		{name: "combined update", body: `{"name":"  生产账号  ","note":"  账单备注\n第二行  "}`, wantName: "生产账号", wantNote: "  账单备注\n第二行  "},
		{name: "name only keeps note", body: `{"name":"备用账号"}`, wantName: "备用账号", wantNote: "  账单备注\n第二行  "},
		{name: "note only keeps name", body: `{"note":"新备注"}`, wantName: "备用账号", wantNote: "新备注"},
		{name: "clear name keeps note", body: `{"name":""}`, wantNote: "新备注"},
		{name: "rename and null note", body: `{"name":"重命名","note":null}`, wantName: "重命名"},
		{name: "independent rune limits", body: `{"name":"` + strings.Repeat("名", 255) + `","note":"` + strings.Repeat("账", 2048) + `"}`, wantName: strings.Repeat("名", 255), wantNote: strings.Repeat("账", 2048)},
		{name: "reset metadata", body: `{"name":"保留别名","note":"保留备注"}`, wantName: "保留别名", wantNote: "保留备注"},
		{name: "invalid null name is atomic", body: `{"name":null,"note":"不应写入"}`, wantName: "保留别名", wantNote: "保留备注", invalid: true},
		{name: "invalid control name is atomic", body: `{"name":"line\nbreak","note":"不应写入"}`, wantName: "保留别名", wantNote: "保留备注", invalid: true},
		{name: "oversize name is atomic", body: `{"name":"` + strings.Repeat("名", 256) + `","note":"不应写入"}`, wantName: "保留别名", wantNote: "保留备注", invalid: true},
		{name: "oversize note is atomic", body: `{"name":"不应写入","note":"` + strings.Repeat("账", 2049) + `"}`, wantName: "保留别名", wantNote: "保留备注", invalid: true},
		{name: "clear both fields", body: `{"name":"","note":""}`},
	} {
		t.Run(step.name, func(t *testing.T) {
			wantStatus := http.StatusOK
			if step.invalid {
				wantStatus = http.StatusBadRequest
			}
			res := serveCredentialRequest(t, engine, http.MethodPut, classicDetail, step.body, "test-auth-key", "")
			if res.Code != wantStatus {
				t.Fatalf("update status = %d, want %d: %s", res.Code, wantStatus, res.Body.String())
			}
			if strings.Contains(res.Body.String(), `"note":`) {
				t.Fatal("classic update response exposes note")
			}
			var after models.Credential
			if err := fixture.db.First(&after, before.ID).Error; err != nil {
				t.Fatal(err)
			}
			if step.invalid && after.UpdatedAtMS != previousUpdatedAt || !step.invalid && after.UpdatedAtMS <= previousUpdatedAt {
				t.Fatalf("updated_at_ms = %d after %d, invalid = %v", after.UpdatedAtMS, previousUpdatedAt, step.invalid)
			}
			previousUpdatedAt = after.UpdatedAtMS
			wantRow := before
			wantRow.Name, wantRow.Note, wantRow.UpdatedAtMS = step.wantName, step.wantNote, after.UpdatedAtMS
			if !reflect.DeepEqual(after, wantRow) {
				t.Fatal("metadata update changed credential identity/configuration or lost name/note")
			}
			entries, err := fixture.registry.SnapshotGroupCredentialEntriesExact(created.GroupID, []uint{before.ID})
			if err != nil {
				t.Fatal(err)
			}
			wantEntries := append(runtimeBefore[:0:0], runtimeBefore...)
			wantEntries[0].Name = step.wantName
			if !reflect.DeepEqual(entries, wantEntries) {
				t.Fatal("metadata update changed runtime identity, health, cooldown or credible cycle")
			}
			for _, route := range []struct {
				path         string
				list, modern bool
			}{
				{classicList, true, false}, {classicDetail, false, false},
				{modernList, true, true}, {modernDetail, false, true},
			} {
				item, _ := readGroupDetailLedgerItem(t, engine, route.path, route.list)
				if string(item["name"]) != mustJSONString(t, step.wantName) {
					t.Fatalf("%s name = %s, want %q", route.path, item["name"], step.wantName)
				}
				if note, exists := item["note"]; exists != route.modern || route.modern && string(note) != mustJSONString(t, step.wantNote) {
					t.Fatalf("%s note = %s, exists = %v", route.path, note, exists)
				}
			}
		})
	}
}
