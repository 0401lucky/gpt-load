# 执行计划：合并上游 main（B 方案）

> 目标分支：`main`（本地 = `59d75050`）。上游目标：`upstream/main` = `f2840466`。
> 顺序执行；每步的验证命令必须真实跑过并记录结果，禁止跳步。

## 0. 准备

- [ ] 0.1 确认工作区仅有可忽略的未跟踪项（`.playwright-mcp/`、`tmp/`）；必要时切到 `main`（当前 `feat/credential-note` 与 `main` 同点）。
- [ ] 0.2 采集**改动前基线**：`go build ./...`、`go vet ./...`、`go test . ./internal/...`，记录失败包集合（本机 Windows 既有失败包：`catalog`/`container`/`gateway`/`platform-authkey`/`webui` 一类，需实测确认）。写入 `research/baseline-tests.md`。
- [ ] 0.3 确认 `upstream/main` 已 fetch 到 `f2840466`。

## 1. 合并与冲突解决

- [ ] 1.1 `git merge --no-ff upstream/main -m "merge: 合并上游 main 至 f2840466（上游新增迁移顺延为 0021-0024）"`。
- [ ] 1.2 解决 5 个冲突文件（`internal/storage/{migration.go,migration_test.go,db_test.go,database_integration_test.go,operation_index_migration_test.go}`），终态顺序见 `design.md` §1.4；`operation_index_migration_test.go` **保留 fork 的按 ID 定位实现**。
- [ ] 1.3 逐文件人工复核冲突解决结果：不夹带 `<<<<<<<`/`>>>>>>>`，fork 的 `0018/0019/0020` 注册项完整。

## 2. 上游迁移顺延（B 方案核心）

- [ ] 2.1 重命名上游 4 个迁移文件（含 1 个 `_test.go`）：`0018_auto_model.go`→`0021_auto_model.go`、`0019_auto_decision_attribution.go`→`0022_auto_decision_attribution.go`、`0019_auto_decision_attribution_test.go`→`0022_auto_decision_attribution_test.go`、`0020_client_model_overrides.go`→`0023_client_model_overrides.go`、`0021_request_audit.go`→`0024_request_audit.go`。
- [ ] 2.2 改 ID 常量与导出符号：`ID00NN`、`Up00NN`、`Validate00NN`、`ValidateRecoverable00NN`、`ValidateCurrent00NN`、`SchemaModels00NN`、`TableNames00NN`（仅这几个文件内）。
- [ ] 2.3 **不改** schema 级内部名（`auto_decision_usage_stats_0019`、`chk_auto_decision_usage_0019_*`、`autoLog0019`/`autoUsage0019`/`autoUsageBuild0019` 等），文件顶部加来源注释（见 `design.md` §1.3）。
- [ ] 2.4 同步引用点：`internal/storage/migration.go`（注册表 4 条）、`migration_test.go`、`db_test.go`、`database_integration_test.go`。
- [ ] 2.5 复查：`rg -n "ID00(18|19|20|21)\b" internal/` 应只剩 fork 的 donation/credential 迁移与 donation 集成测试；`rg -n "0018_auto_model|0019_auto_decision_attribution|0020_client_model_overrides|0021_request_audit" .` 无残留引用。
- [ ] 2.6 验证：`go build ./...` + `go vet ./...` 通过；`go test ./internal/storage/...` 通过。

## 3. 迁移专项验证

- [ ] 3.1 注册表形态：断言 24 条、顺序等于 `design.md` §1.4（用 `go test ./internal/storage/...` 中的链式断言 + 一次性脚本双确认）。
- [ ] 3.2 **全新空库**：空 `DATA_DIR` 启动新二进制，账本 24 条、`startup.ready` 出现。
- [ ] 3.3 **生产库副本**：从生产只读导出 `gpt-load.db`（含 `-wal`/`-shm`）与 `encryption.key` 到本地 `tmp/`；记录升级前账本 20 行与业务计数；用新二进制在同副本上启动；断言账本 24 条且**前 20 行逐行不变**、业务计数与 `instance_id`/`source_id` 不变。
- [ ] 3.4 **幂等**：对 3.3 终态库再启动一次，无错误、账本仍 24、数据不变。
- [ ] 3.5 演练后清理本地副本（敏感数据不外传、不留存）。

## 4. 全量回归

- [ ] 4.1 `go test . ./internal/...`，与 0.2 基线逐包对比，确认无新增失败包。
- [ ] 4.2 前端：`web` 下 type-check / lint / build（按仓库既有脚本，如 `pnpm run type-check`、`pnpm run lint`、`pnpm run build`，以 `package.json` 实际脚本名为准）。
- [ ] 4.3 若仓库有 `make check` 等价项（gofmt / prettier / 版权头 / `go mod tidy -diff`），逐项跑或说明本机限制。

## 5. 文档与收尾

- [ ] 5.1 更新 `.trellis/spec/backend/database-guidelines.md`（编号策略 + 文件清单到 `0024`）。
- [ ] 5.2 产出 `research/production-upgrade-note.md`：常规 `pull` + `up -d`、验证项、回滚要点（无需 SQL 修正、无需停机）。
- [ ] 5.3 独立复核（trellis-check）：注册表、冲突解决、上游改动完整性、演练证据。
- [ ] 5.4 提交（merge commit 已存在；文档/spec 另起 `chore(spec)` 提交），push 待用户确认。

## 回滚点

| 阶段 | 回滚动作 |
|---|---|
| 合并冲突未解完 | `git merge --abort` |
| 合并后验证失败 | `git reset --hard 59d75050`（未 push，无外部副作用） |
| 已 push 但生产未升级 | 生产继续跑旧镜像；本地 revert 合并提交后重新出镜像 |
