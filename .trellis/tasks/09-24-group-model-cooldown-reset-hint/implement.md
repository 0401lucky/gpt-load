# 执行计划：按上游 429 报错里的重置时间冷却单个模型凭据

## 前置

- 复现基线：`internal/health/execution_judge.go:582-637`（`rateLimitDecision`）、`internal/health/model_cooldown_test.go`（现有按模型冷却的测试矩阵）、`internal/gateway/handler.go:1524-1528`（全冷却出口）。
- 改动前固定动作：`grep` 每个新键名与字段名，确认无重名（`.trellis/spec/guides/index.md` 的 Pre-Modification Rule）。
- 依赖边界：`internal/health` 不得 import `storage` / `control` / `gorm`；本次改动不新增跨层依赖（`regexp`、`time` 可用）。

## 实施清单（按序）

1. **解析函数（先写测试）**
   - 新增 `internal/health/reset_hint_test.go`：锚点命中/不命中、`3h 18m`、`45m`、`1d 2h`、大小写、0/负数、上下限钳制、余量。
   - 新增 `internal/health/reset_hint.go`：`ParseResetHint(summary string) (time.Duration, bool)`，规则见 design.md 3.3。
   - 验证：`go test ./internal/health/ -run ResetHint -count=1`。

2. **配置链路（照抄 affinity_enabled）**
   - `internal/state/runtime_settings.go`：常量、`RuntimeSettings` 字段、`ResolvedGroupSettings` 字段、默认值 `false`、允许键列表、系统级 `case`、分组级 `case`。
   - `internal/state/snapshot.go`：`GroupView` 透传该字段。
   - `internal/control/group_detail.go`：`GroupEffectiveConfigResponse` 增加 `rate_limit_reset_hint_enabled`。
   - 验证：`go test ./internal/state/ ./internal/control/ -count=1`；补一个「全局默认关闭 / 分组置 true / 非布尔值被拒」的用例，风格对齐 `internal/control/group_settings_test.go:205-223`。

3. **判定接入**
   - `internal/health/decision.go`：`DecisionContext` 增加 `RateLimitResetHint bool`。
   - `internal/health/execution_judge.go`：`rateLimitDecision` 在默认冷却之前插入解析分支（`RuleID = "rate_limit.reset_hint"`）。
   - `internal/gateway/handler.go`：`decisionContextForSelection` 从 `selection.Group` 读取开关。
   - 其它构造 `DecisionContext` 的路径（websocket / jev / 内部调用）：默认值为 `false`，即行为不变，逐一确认无需显式赋值。
   - 验证：`go test ./internal/health/ ./internal/gateway/ -count=1`。补两类用例：开关开 + Summary 带 `Try again in 3h 18m` → 冷却约 3h19m 且规则为 `rate_limit.reset_hint`；开关关 → 仍是 1 分钟默认冷却。

4. **全冷却出口文案**
   - `internal/gateway/reason.go`：新增 `reasonModelAccountsUnavailable`（429 / code 保持 `upstream_rate_limited` / Message 含占位）。
   - `internal/gateway/handler.go:1524-1528`：组装文案（有模型名则带上，无则退化措辞），沿用 `setCooldownRetryAfter` 写 `Retry-After`。
   - 更新受影响的既有测试文案断言：`internal/gateway/handler_test.go:3047`、`internal/gateway/request_log_test.go:1604`（确认这两处断言的是哪条路径，只改全冷却分支的断言）。
   - 验证：`go test ./internal/gateway/ -count=1`。

5. **前端 classic**
   - `web/src/frontends/classic/app/resources/groups.ts`：`runtimeSettingFields`、`groupRuntimeSettingFields`、`GroupRuntimeConfigDto`/`GroupEffectiveConfigDto` 字段、`projectRuntimeConfig`。
   - `web/src/frontends/classic/app/api/control/types.ts`：DTO。
   - `web/src/frontends/classic/features/groups/settings/GroupSettingsTab.vue`：计算属性、赋值函数、`SettingRow` 模板行。
   - `web/src/frontends/classic/features/groups/settings/group-settings-patch.ts`：`cloneOverrides` 与提交路径。
   - i18n：`web/src/frontends/classic/i18n/locales/{zh-CN,en-US,ja-JP}/group.ts` 三份同步。

6. **前端 modern**
   - `web/src/frontends/modern/api/group-detail.ts`：`runtimeSwitches` 增加键。
   - i18n：`web/src/frontends/modern/i18n/locales/group-detail.ts` 的 `runtimeFields` 三种语言同步（`enUS: typeof zhCN` 会强制键一致）。

7. **门禁与回归**
   - `make check`（含 gofmt、`go mod tidy -diff`、`go vet`、前端 lint/format/build、全量单测、`git diff --check`）。
   - 本机为 Windows/CRLF 检出，若 gofmt/prettier/tidy 报错，先按既有记忆 `windows-crlf-breaks-local-checks` 判断是否属行尾假阳性，不凭本地结果改文件。

## 风险与回滚点

- **高危点 1：classic 白名单漏登记** → 分组设置页整页报错。实现后必须用真实响应体验证：`GET /api/groups/4/settings` 的 `effective` 与 `overrides` 两处投影都能被 classic 投影器接受。
- **高危点 2：modern 漏登记** → 保存设置时静默丢弃该键。需实测「开启 → 保存 → 重新读取仍为 true」的往返。
- **高危点 3：`Summary` 截断** → 解析永不命中（功能静默失效，不报错）。实现期用生产原文长度验证。
- **降级风险**：开关置 `true` 后回退旧镜像会导致旧版本拒绝加载配置。交付说明里必须写明「回退前先关闭开关」。
- 代码回滚：本任务不涉及迁移，`git revert` 即可；已开启开关的部署环境需先关闭开关再回退镜像。

## 交付后（不在本任务内执行）

- 合并后由用户决定是否升级 gptload2 并给 cline 分组开启开关；本任务不含服务器操作、不改动生产配置。
