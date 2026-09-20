# 合并上游 main 更新并重编号 donation 迁移

## Goal

将 `upstream/main` 的 28 个新提交合并进 fork，解决本地 donation 迁移与上游迁移的**编号空间冲突**（本地 `0015`/`0016` 需顺延为 `0018`/`0019`），并保证已在生产运行的 gptload2 实例**平滑升级、数据零丢失**。

## Context

### 分叉现状

- merge-base: `91df89ef`；上游领先 28 个提交，本地领先 12 个提交。
- git 文本冲突仅 4 个文件，全在 `internal/storage/`：`migration.go`、`migration_test.go`、`db_test.go`、`database_integration_test.go`。
- README 三语、`internal/control/http_routes.go` 可自动合并；`web/` 本地未改动，上游的 classic/modern 前端重构不影响。

### 编号空间冲突（git 不报，编译失败）

| 编号 | 本地 fork | 上游 |
|---|---|---|
| 0015 | `0015_donation_intake` | `0015_group_usage_index` |
| 0016 | `0016_donation_manual_review` | `0016_credential_quota_history` |
| 0017 | — | `0017_request_log_operation_index` |

两边都在 `internal/storage/migrations` 包声明 `const ID0015` / `ID0016`，直接合并会造成 Go 重复声明错误。

### 迁移账本的严格位置语义

`internal/storage/migration.go` 的 `applyMigrationsLocked` 按**下标**校验账本：

```go
for index, id := range applied {
    if index >= len(entries) || entries[index].ID != id {
        return fmt.Errorf("schema_migrations contains unknown or non-contiguous migration %q", id)
    }
}
```

迁移 ID 的**顺序即身份**，因此本地 donation 迁移必须整体顺延到上游 0017 之后。

### 生产环境实测（yunyou-99，2026-09-20 只读排查）

| 项 | 实测值 |
|---|---|
| 项目/容器 | `/root/apps/gptload2`，容器 `gpt-load-2`，镜像 `ghcr.io/0401lucky/gpt-load:main` |
| 当前版本 | `2.0.0-dev.4.g56c19ba6`（= 当前 fork main HEAD） |
| 数据库 | **SQLite**，卷 `gptload2-data` → `/app/data/gpt-load.db`（163 MB，另有活跃 WAL 6.4 MB） |
| 账本 | 16 条：`0001`–`0014` + `0015_donation_intake` + `0016_donation_manual_review` |
| 业务数据 | 身份 1 / 批次 30 / 捐献项 36 / **已接收资源 439** / 审核动作 23 / 测试尝试 24 |
| 持久身份 | `instance_id` = `c1e34efe-1f8b-45b7-897f-168d7e1ab926`，`source_id` = `49193b3f-900b-446e-9f6b-a426d1014dc7`（与部署档案一致，**不得变化**） |
| 磁盘 | 49 G 中已用 30 G，可用 19 G |
| 既有备份 | `gptload2-data.bak-20260915-090535.tar.gz`（19 MB，对应升级前的旧库，**已过时**） |

结论：生产库**跑过 donation 迁移且持有真实业务数据**，必须账本平滑升级，禁止重建库。

## Requirements

### R1 合并上游

- 以 merge 方式（与上次 `85811f21` 一致）将 `upstream/main` 并入 `main`，保留双方历史。
- 解决 4 个文件的文本冲突，**以上游的 0015/0016/0017 定义为准**，本地 donation 条目顺延。

### R2 迁移重编号

- `0015_donation_intake` → `0018_donation_intake`，`0016_donation_manual_review` → `0019_donation_manual_review`。
- 同步重命名包内所有 `xxx0015` / `xxx0016` 后缀标识符与常量（`ID0015`→`ID0018`、`Up0015`→`Up0018`、`SchemaModels0015`→`SchemaModels0018` 等）。
- 同步更新全部引用点：`internal/storage/migration.go`、`migration_test.go`、`internal/control/donation_database_integration_test.go`、`donation_manual_database_integration_test.go`。
- 迁移 ID 字符串必须与注册表下标严格对齐（第 18/19 位）。

### R3 生产库升级路径可行且可验证

- 升级方式：**先删除账本中 donation 的两条记录，再用新镜像启动**，由系统按正确顺序重新执行 `0015`–`0019`。
- 依据：删后账本剩 14 条，下标 0–13 与注册表完全匹配；`Up0018`（`AutoMigrate`）与 `Up0019`（`HasColumn`/`HasConstraint` 逐项判空、`rebuildSQLiteDonationItems0016` 带完成态前置检查）均设计为幂等。
- **该幂等性必须实测证明**，不得仅凭代码阅读下结论。

### R4 交付物

- 本地完成合并、重编号、全量验证后 push，由既有 `ghcr-main.yml` 流水线产出 `:main` / `:latest` 滚动镜像。
- 交付一份生产升级手册，含：前置备份命令、账本修复 SQL、升级与验证命令、**逐级回滚点**。
- 本次**不执行**生产升级。

## Acceptance Criteria

- [ ] AC1 `go build ./...` 与 `go vet ./...` 通过，`internal/storage/migrations` 包无重复声明。
- [ ] AC2 `make check` 全绿（含 gofmt、`go mod tidy -diff`、前端 lint/format/build、`go test . ./internal/...`）。
- [ ] AC3 迁移注册表恰为 19 条，顺序为 `0001`–`0014`、`0015_group_usage_index`、`0016_credential_quota_history`、`0017_request_log_operation_index`、`0018_donation_intake`、`0019_donation_manual_review`。
- [ ] AC4 **迁移兼容性演练**：在已跑过旧 `0015_donation_intake`/`0016_donation_manual_review` 的库上，执行"删账本两条 → 跑新代码迁移"，结果账本为完整 19 条，且 donation 业务数据零丢失。**SQLite / MySQL / PostgreSQL 三库均须通过**（本地 `gl-verify-*` 容器即为此状态的现成靶子）。
- [ ] AC5 幂等性反证：对已升级完成的库再启动一次，迁移过程不报错、不重复改写数据。
- [ ] AC6 合并后 donation 全部既有测试通过（`internal/control/donation*_test.go`、`internal/storage/migrations/0019_*_test.go`）。
- [ ] AC7 `:main` 镜像构建并 promote 成功，版本号可追溯至本次合并提交。
- [ ] AC8 升级手册可在无额外判断的前提下照做执行，回滚路径明确到具体 tag 与文件。

## Out of Scope

- 不执行生产升级（仅交付手册）。
- 不改动 `standalone-gpt-load`（`gptload.lucky0625.tech`，1.x 代码线）。
- 不追逐上游未合并的 `tbphp/jev-routing-prototype` / `v1` 分支。
- 不清理上游新增的 `screenshot/` 图片变更与三语 README（照单合并）。

## Notes

- 上游 `release.yml`（68 行）与 `Dockerfile`（新增 `modern_page_routes.json` 拷贝）本地均未改动，可自动合并，但需实测构建。
- 上游 `internal/webui/workflow_self_hosted_test.go` 有改动，本地新增 `workflow_main_images_test.go`，须确认二者断言不互斥。
- 生产 WAL 文件表明服务有活跃写入；升级手册须包含停机与 WAL checkpoint 注意事项。
