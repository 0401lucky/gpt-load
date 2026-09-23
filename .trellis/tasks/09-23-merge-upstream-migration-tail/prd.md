# 合并上游 main：迁移编号改为「上游顺延到链尾」（B 方案）

## Goal

把 `upstream/main`（`f2840466`，领先 33 个提交）以 merge commit 方式并入 fork `main`，并解决**双方迁移编号再次撞车**的问题：

| 编号 | fork main（现行） | 上游 main（新增） | 本轮决定（B 方案） |
|---|---|---|---|
| 0018 | `0018_donation_intake` | `0018_auto_model` | fork 保持；上游顺延 |
| 0019 | `0019_donation_manual_review` | `0019_auto_decision_attribution` | fork 保持；上游顺延 |
| 0020 | `0020_credential_note` | `0020_client_model_overrides` | fork 保持；上游顺延 |
| 0021 | — | `0021_request_audit` | 上游顺延 |

B 方案的**核心目的**：生产账本（`schema_migrations`）保持**纯追加**语义，升级不再需要删账本行、不再需要停机窗口。

## Context

- fork `main`（`59d75050`）已含 donation 迁移 `0018`/`0019` 与凭据备注迁移 `0020`，三者均已上线。
- 生产 `gptload2` 当前跑 `ghcr.io/0401lucky/gpt-load:sha-59d7505…`，账本 **20 条**（`0001`–`0017` + fork 三条），业务数据健康（batches 36 / items 62 / resources 486 / review_actions 25 / test_attempts 33，`credentials.note` 列已存在）。
- 试合并（`git merge-tree`）文本冲突仅 **5 个文件**，全在 `internal/storage/`：`migration.go`、`migration_test.go`、`db_test.go`、`database_integration_test.go`、`operation_index_migration_test.go`；其余 337 个文件自动合并。
- 上一轮（2026-09-20）采用的是 A 方案（fork 让位上游、本地迁移顺延），代价是生产必须停机 + 删账本 + 重放迁移。上游约每两周产出一批迁移，A 方案的代价会**每轮重复**，故本轮改选 B。

## Requirements

### R1 合并上游

- 以 merge commit（`--no-ff`）把 `upstream/main` 并入 `main`，保留双方历史，与历史合并（`85811f21`、`b2aba7d6`）保持同一形态。
- 5 个冲突文件全部解决；上游其余改动（`web/` 大重构、`internal/gateway`、`internal/execution`、`third_party/cpaembedded`、CI、依赖升级等）照单合并，不做本地裁剪。
- 不动 fork 既有特性：donation（`internal/control/donation_*`）、凭据备注、`ghcr-main.yml` 镜像流水线、Trellis 脚手架。

### R2 迁移编号（B 方案）

- fork 的 `0018_donation_intake` / `0019_donation_manual_review` / `0020_credential_note` **编号、ID 字符串、导出符号全部保持不变**。
- 上游新增的 4 条迁移顺延为链尾：

| 上游 ID | fork 新 ID | Go 文件 |
|---|---|---|
| `0018_auto_model` | `0021_auto_model` | `0021_auto_model.go` |
| `0019_auto_decision_attribution` | `0022_auto_decision_attribution` | `0022_auto_decision_attribution.go`（+ `_test.go`） |
| `0020_client_model_overrides` | `0023_client_model_overrides` | `0023_client_model_overrides.go` |
| `0021_request_audit` | `0024_request_audit` | `0024_request_audit.go` |

- **只改编号相关标识**：`ID00NN`、`Up00NN` / `Validate00NN` / `ValidateRecoverable00NN` / `ValidateCurrent00NN` / `SchemaModels00NN` / `TableNames00NN` 等导出符号，以及注册表与链式断言测试中的 ID 引用。
- **不改上游迁移内部的 schema 级标识符**：如 `chk_auto_decision_usage_0019_cost`、`auto_decision_usage_stats_0019` 这类会落进数据库 DDL 的名字保持原样（改名会让 fork 库 schema 与上游永久分叉，且上游未来迁移按名引用时会错位）。这些内部名与 Go 文件编号不必一致，只需在文件顶部注释说明来源。
- 修改后迁移注册表恰为 24 条，顺序：`0001`–`0017`、`0018_donation_intake`、`0019_donation_manual_review`、`0020_credential_note`、`0021_auto_model`、`0022_auto_decision_attribution`、`0023_client_model_overrides`、`0024_request_audit`。

### R3 生产升级保持零停机

- 升级路径必须退化为「`docker compose pull` + `up -d`」：账本既有 20 行**一行不改**，新版启动后仅在尾部追加 4 行。
- 不得要求生产执行任何 SQL 修正、备份停机或人工干预。

### R4 验证

- 门禁：`go build ./...`、`go vet ./...`、`go test ./internal/...`、`go test .`（与改动前基线对比，Windows 既有失败包不算回归）。
- 迁移专项：全新空库跑完整 24 条迁移成功；**生产库副本**（从生产只读导出的 SQLite）升级后账本 20 → 24 条且前 20 条逐行不变、业务数据计数与 `instance_id`/`source_id` 不变；对升级后的库再启动一次（幂等）无变化。
- 前端：`web` 的 type-check / lint / build 通过（上游改了 120 个前端文件）。

### R5 文档与 spec

- 更新 `.trellis/spec/backend/database-guidelines.md`：迁移编号策略由「fork 让位上游」改为「fork 迁移编号冻结、上游新增迁移顺延到 fork 链尾」，并同步迁移文件清单（到 `0024`）。
- 交付一份**简洁**的生产升级说明（无需账本手术，只需常规 `upgrade.sh`/compose 流程），含验证与回滚要点。

## Acceptance Criteria

- [ ] AC1 `go build ./...` 与 `go vet ./...` 通过；`internal/storage/migrations` 无重复声明。
- [ ] AC2 迁移注册表恰 24 条，顺序与 R2 表一致；`migration_test.go`、`db_test.go`、`database_integration_test.go`、`operation_index_migration_test.go` 的链式断言同步。
- [ ] AC3 `go test ./internal/...` 与 `go test .` 结果与基线一致（无新增失败包）。
- [ ] AC4 全新空库迁移成功，账本 24 条；重复启动幂等。
- [ ] AC5 **生产库副本升级演练**：20 → 24 条、前 20 行逐行未变、业务数据与身份 UUID 不变。
- [ ] AC6 前端 type-check / lint / build 通过。
- [ ] AC7 spec 已更新，生产升级说明已产出（无需 SQL 修正、无需停机）。
- [ ] AC8 合并提交落在 `main`（本地），push 与镜像发布另行征得用户同意后执行。

## Out of Scope

- 本轮**不执行**生产升级、不 push、不触发镜像发布（需用户单独确认）。
- 不重命名上游迁移内部 schema 级标识符（见 R2）。
- 不追上游未合并的其他分支（`feat/key-rpm-statistics`、`claude/*` 等）。
- 不改 `standalone-gpt-load`（1.x 线）。

## Notes

- B 方案的已知代价：今后每次合并上游若带来新迁移，都要把上游的新编号顺延到 fork 链尾（约 10 行机械改动），并在 spec 中维持该约定。
- 上游 `internal/storage/operation_index_migration_test.go` 与 fork 此前的修复（用 ID 定位下标替代 `len(migrations)-1`）需要在冲突解决时一并保留。
