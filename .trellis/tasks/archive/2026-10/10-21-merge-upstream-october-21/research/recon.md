# 预合并研究（2026-10-21）

## 输入证据

- `git fetch --no-tags upstream main`：上游 main 从 `a5c691bb` 前进至 `41c82ac0`。
- `git fetch --no-tags origin main`：origin/main 与本地 HEAD 同为 `216024cc31c5de4c31a11777c41746bf9ac752d3`。
- `git merge-base HEAD upstream/main`：`a5c691bb0559f856ebb587879d82e341ff480202`。
- `git rev-list --left-right --count HEAD...upstream/main`：fork 42、upstream 41。
- `git diff --shortstat a5c691bb upstream/main`：450 files changed, 21751 insertions(+), 3540 deletions(-)。
- `git merge-tree --write-tree --name-only HEAD upstream/main`：返回 1（存在预期文本冲突）；只写 Git 临时 tree，不修改工作树或索引。

## 17 处文本冲突与 owner

| Owner | 文件 |
| --- | --- |
| core | `internal/control/runtime.go` |
| core | `internal/gateway/handler.go`、`handler_test.go`、`model_cooldown_test.go` |
| core | `internal/health/decision.go`、`execution_judge.go` |
| core | `internal/state/model_cooldown.go`、`registry.go`、`runtime_checkpoint.go`、`runtime_view.go` |
| storage | `internal/storage/database_integration_test.go`、`db_test.go`、`migration.go`、`migration_test.go` |
| main | `internal/subscription/runtime/runtime_test.go` |
| web | `web/src/frontends/modern/features/groups/CredentialDetailPanel.vue`、`CredentialTestDialog.vue` |

## 上游主要能力

- `78884001`：可配置上游错误规则。
- `9702cc09`：订阅额度恢复解除额度类冷却。
- `ab655129`：按访问密钥记录客户端原始请求体，默认关闭。
- `52b6cc70`：批量 API key 测试；`d64c954f`：日志和用量共享自动刷新。
- `52a82ef1`：聚合搜索和 passthrough；`3d04d9f1`：合作渠道层级和 Modelflare；`c24243f7`：参数覆盖数组通配符。
- `207a948c`：Claude 订阅 reset-credit；`935aff97`、`1b309fe9` 等：CPA 模型目录/客户端版本维护。
- `3745fbab`：version/update CLI；上游 update 只接受官方发布版本，fork main 的开发版本已有拒绝守卫。
- `1cedccf2`、`d5e44e6f`、`6cc3b889`：modern 移动布局、列表滚动、代理和表单交互。
- `41c82ac0` 等：Go/SDK 依赖更新和 billed usage 保留。

## 兼容证据与易漏触点

- fork 0001–0032 注册表在 `internal/storage/migration.go`；旧 `0030` 为移除输出计时，不能覆盖。上游唯一新增迁移为 `0030_request_body_recording`，需追加为 fork 0033。
- 上游 `request_body_recording_migration_test.go` 使用固定 `[:29]`、`[29]`；必须按 `ID0033` 定位。多份历史 fixture 用当前 AccessKey 模型建行，新增字段尚不存在时需要 Omit。
- quota 新字段与 fork 周期字段的 clone/preserve/checkpoint 冲突必须全部合并。GroupModelCycles 原来只读取 ModelCooldowns，要读取两个来源的有效期限。
- `health.rateLimitDecision` 两方代码分别新增可信 reset 与 QuotaCooldown；安全约束结尾有两组不同的清理条件，需同时保留。
- `control.Runtime.Run` 两方分别新增 donationRecovery 与 cpaModelCatalog；不是二选一。
- modern 备注保存采用 classic API，返回体未携带 note；fork 已有 emit 缓存补值与 nameDirty/namePending 护栏，不能被上游 mobile 改动覆盖。
- 请求探测单项诊断已有 upstream status/error/retry 详细字段；optional row / batchCount 与这些结果展示需兼容。
- 上次合并记录在 `.trellis/tasks/archive/2026-10/10-08-merge-upstream-october-08/`。上一轮没有新增失败，但有 Windows 平台失败；本轮必须重新取基线。

## 工具环境

- 当前 Go 1.27.0 windows/amd64，`GOTOOLCHAIN=auto`；上游根 module 要求 Go 1.27.3。
- 上游 Bifrost core v1.13.1、CPA SDK v8.0.36；嵌入 CPA module Go 1.26.6 / 同一 SDK v8.0.36。
- Node v25.6.0；仓库 pnpm 精确版本 11.17.0；未发现 make。
- 进程级工具路径/自动下载可用于验证，不改全局配置。
