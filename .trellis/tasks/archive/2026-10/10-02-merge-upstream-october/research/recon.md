# 合并勘查（2026-10-02）

## 固定输入与只读证据

- `origin`: `https://github.com/0401lucky/gpt-load.git`
- `upstream`: `https://github.com/tbphp/gpt-load.git`，已 fetch。
- HEAD: `c79bca8fcee20cec2a6306b474a8a10e3a1061c6`
- upstream/main: `addd69870cb58108c584d69219c44e23d4bdf3db`，2026-10-01，`fix(logs): Unify request latency and output speed (#804)`。
- merge-base: `f284046682bdd76c154ad8ac85557172370f3ea1`
- `git rev-list --left-right --count HEAD...upstream/main`: `33 44`。
- `git diff --shortstat HEAD...upstream/main`: `560 files changed, 37007 insertions(+), 2196 deletions(-)`。
- `git merge-tree --write-tree --name-only HEAD upstream/main`: 预演树 `56b1c5661cf51d0897d84e4f872cdf878c434b32`；只生成对象，未修改工作树、索引或分支。
- 初始未跟踪项：`.playwright-mcp/`、`.trellis/tasks/09-28-credential-probe-real-chat/`。

## 16 个文本冲突文件

```text
README.md
README_CN.md
README_JP.md
internal/control/group_detail.go
internal/control/modern_credentials.go
internal/parameteroverride/rules.go
internal/platform/config/config.go
internal/state/runtime_settings.go
internal/state/snapshot.go
internal/storage/database_integration_test.go
internal/storage/db_test.go
web/src/frontends/classic/api/control/types.ts
web/src/frontends/classic/app/resources/groups.ts
web/src/frontends/classic/features/groups/settings/GroupSettingsTab.vue
web/src/frontends/classic/features/groups/settings/group-settings-patch.ts
web/src/frontends/modern/api/group-detail.ts
```

## 语义冲突

- 迁移：上游添加 `0022_rpm_stats`、`0023_access_key_concurrency`、`0024_request_log_output_timing`，与本地已经发布的 22–24 编号冲突。预演中 `internal/storage/migration.go` 自动合并后仍然只有 `ID0001`–`ID0024`，不能原样接受；新旧文件同名导出符号也会冲突。沿用已确定的「fork 编号冻结、上游顺延链尾」策略，追加为 25–27。
- `parameteroverride/rules.go`：fork 的 `Fingerprint()` 与上游的 `ConfiguredFields()` 插入在同一位置；必须保留两个完整方法，特别注意冲突外的共同右大括号。
- `modern_credentials.go`：`Note` 与 `RPMPeakHour` 及其集合/详情赋值互相冲突，两者均保留。共用 DTO 的备注 JSON 隐藏规则不能放宽。
- 分组设置：`RateLimitResetHintEnabled` 与 `EmptyResponseRetry` 等字段冲突，涉及状态默认值、解析、快照、控制面 DTO 和前端白名单/保存全链。

## 上游更新类别

- RPM 统计与数据面并发控制；凭据文件导入；日志耗时、输出速度和推理设置归属。
- Codex 实时语音及模式/重试、WebSocket 恢复、空响应检测重试。
- Cline API key 通道、Mistral 原生路由、Gemini embeddings、OpenCode 和多个兼容通道预设。
- 脱敏/内容审核增强；Bifrost `v1.11.0`、CPA `v8.0.8` 等依赖与前端更新。
- 发布验收脚本随上游代码合入，不在本轮执行发布。

## 既有承重证据

- `.trellis/spec/backend/database-guidelines.md`：已发布账本按数组下标比对，必须是新链前缀；上游迁移顺延时保留 schema 级名字；测试按 ID 查找而非硬编码下标。
- `.trellis/spec/backend/runtime-settings.md`：冷却开关为分组专属，`IsRuntimeSettingKey` 不能把它暴露成全局设置。
- `.trellis/spec/frontend/data-layer.md`：严格投影拒绝所有白名单外字段；classic 设置六处与 modern switches 联动。
- `internal/state/runtime_settings_test.go:351`：`TestRateLimitResetHintDefaultsOffAndIsGroupOverridable`。
- `internal/health/model_cooldown_test.go:91`：`TestModelCooldownUsesResetHintOnlyWhenEnabled`。
- `internal/gateway/model_cooldown_test.go:126`：`TestRateLimitResetHintCooldownFollowsGroupSwitch`。
- `internal/storage/credential_note_migration_test.go:29`、`internal/storage/operation_index_migration_test.go:30` 等：按 ID 定位迁移范式。
- `internal/control/credential_note_test.go:20`：备注可选写入语义与 classic 响应隔离。
- `internal/control/credential_probe_test.go:77`、`:321`、`:406`：诊断脱敏、探测不变更运行态，以及配置改变后失去恢复资格。
- `internal/parameteroverride/fingerprint_test.go:5`：转义指针段的配置指纹区分。
- `internal/requestlog/group_model_usage_test.go:13`、`:68`、`:163`：跨窗口边界、残缺行及过期整齐窗口 token 字段回归。
- `internal/control/donation_manual_review_test.go`、`donation_review_regression_test.go`、`donation_concurrency_test.go`：已有审核、并发、幂等与恢复回归可复用。
- 2026-09-23 归档任务与 journal 已有迁移策略，足以回答历史决定，无需检索额外会话。

## 工具与验证边界

- Go `1.27.0 windows/amd64`、Node `25.6.0` 可用，corepack/pnpm 可发现；当前 PATH 无 make。
- `Makefile` 的 check 为格式、依赖 tidy、vet、前端 lint/format/build、Go build/test 和 diff check；CPA 是独立 module，另跑 tidy/vet。
- 历史 journal 记录 Windows CRLF/权限等失败，不能当成本轮基线。需同条件复测并比较具体用例。
- 当前规划阶段未运行产品测试、修改产品代码或操作生产；完整验证属于批准后的实施阶段。
