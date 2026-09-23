# 技术设计：合并上游 main（B 方案：上游迁移顺延到 fork 链尾）

## 1. 迁移编号方案

### 1.1 为什么选 B（fork 冻结、上游顺延）

`internal/storage/migration.go` 的 `applyMigrationsLocked` 以**数组下标**逐条比对账本 ID，任何一条对不上就拒绝启动：

```go
for index, id := range applied {
    if index >= len(entries) || entries[index].ID != id {
        return fmt.Errorf("schema_migrations contains unknown or non-contiguous migration %q", id)
    }
}
```

推论：**只有把新迁移追加在链尾，已上线库才能免修账本升级**。

| 方案 | fork 迁移 | 上游新迁移 | 生产升级 | 代价 |
|---|---|---|---|---|
| A（上轮选用） | 顺延到上游之后 | 保持 0018–0021 | 删账本 3 行 + 重放 7 条迁移，需停机 + 备份 | 上游每出一批迁移就重做一次 |
| **B（本轮）** | **冻结在 0018–0020** | **顺延为 0021–0024** | **纯追加 4 行，`pull` + `up -d`** | 每轮需把上游新编号改写到 fork 链尾（约 10 行机械改动） |

上游约每两周产出一批迁移，A 的停机手术会反复发生；B 把代价固定为一次性代码改动，且生产升级退化为常规流程。

### 1.2 编号映射

| 上游文件 / ID | fork 文件 / ID | Go 导出符号 |
|---|---|---|
| `0018_auto_model.go` / `0018_auto_model` | `0021_auto_model.go` / `0021_auto_model` | `ID0018`→`ID0021`、`Up0018`→`Up0021`、`Validate0018`→`Validate0021`、`ValidateRecoverable0018`→`ValidateRecoverable0021` |
| `0019_auto_decision_attribution.go` / `0019_auto_decision_attribution`（+ `_test.go`） | `0022_auto_decision_attribution.go`（+ `_test.go`） | 同上，`*0019`→`*0022` |
| `0020_client_model_overrides.go` / `0020_client_model_overrides` | `0023_client_model_overrides.go` | 同上，`*0020`→`*0023` |
| `0021_request_audit.go` / `0021_request_audit` | `0024_request_audit.go` | 同上，`*0021`→`*0024` |

### 1.3 只改编号标识，不改 schema 级标识（关键约束）

上游迁移文件内部还有一批**会落进数据库 DDL 的版本后缀名**，例如：

- `autoDecisionUsageBuildTable0019 = "auto_decision_usage_stats_0019"`、`..._0019_old`
- `chk_auto_decision_usage_0019_cost` / `_unpriced` / `_partial` 等 check 约束名
- 私有类型 `autoLog0019` / `autoUsage0019` / `autoUsageBuild0019`

**这些名字一律保持原样**，只改文件编号与导出符号。理由：

1. 这些 schema 名已在**上游**发布并落库，上游未来的迁移/校验可能按名字引用它们（如 `HasConstraint(&autoUsage0019{}, "chk_...")`）；fork 一旦改名，fork 库的 schema 就与上游永久分叉，后续合并需要逐处对照修补。
2. fork 库从未落过这些迁移，改名并无收益。
3. 私有符号（`autoLog0019` 等）改名同样只会增加冲突面。

为可读性，改动后的文件顶部加一行注释说明来源，例如：`// 上游 0019_auto_decision_attribution，fork 中顺延为 0022（内部 0019 后缀名保持上游原样）。`

### 1.4 新注册表（终态）

```
01–17  0001_initial … 0017_request_log_operation_index   （与上游一致）
18     0018_donation_intake                              （fork）
19     0019_donation_manual_review                       （fork）
20     0020_credential_note                              （fork）
21     0021_auto_model                                   （上游 0018 顺延）
22     0022_auto_decision_attribution                    （上游 0019 顺延）
23     0023_client_model_overrides                       （上游 0020 顺延）
24     0024_request_audit                                （上游 0021 顺延）
```

生产账本现有 20 行正好是新注册表的前缀 ⇒ 启动后仅追加 21–24。

## 2. 冲突解决策略

`git merge-tree` 预演冲突文件（5 个，全在 `internal/storage/`）与解法：

| 文件 | 冲突原因 | 解法 |
|---|---|---|
| `internal/storage/migration.go` | 双方都在链尾追加注册表条目 | 保留 fork 的 `0018/0019/0020`，其后追加改写为 `0021–0024` 的上游四条 |
| `internal/storage/migration_test.go` | `wantIDs` 列表 | 同上，终态 24 项 |
| `internal/storage/db_test.go` | `wantMigrationIDs` 列表 | 同上，终态 24 项 |
| `internal/storage/database_integration_test.go` | 账本长度 + 逐项断言 | 长度改 24；`[17..20]` 为 fork 三条 + 上游 0021，其余按 1.4 表对齐 |
| `internal/storage/operation_index_migration_test.go` | fork 用 `operationIndexMigrationIndex`（按 ID 定位）替换了上游的 `len(migrations)-1` | **保留 fork 的按 ID 定位实现**；同步上游对 0017 断言的其他改动 |

其余 337 个文件自动合并，不额外裁剪。

## 3. 验证矩阵

| 编号 | 内容 | 手段 |
|---|---|---|
| V1 | 构建与静态检查 | `go build ./...`、`go vet ./...` |
| V2 | 单元/集成测试（含上游新增） | `go test . ./internal/...`，与改动前基线逐包对比 |
| V3 | 注册表形态 | 断言 24 条且顺序等于 §1.4；`migrations` 包无重复声明 |
| V4 | 全新空库 | 新二进制在空 `DATA_DIR` 跑完整 24 条迁移 |
| V5 | **生产库副本升级（最高优先级）** | 从生产只读导出 `gpt-load.db`（+ `-wal`/`-shm`/`encryption.key`）到本地副本，用新二进制启动：账本 20→24 且**前 20 行逐行不变**；业务计数与 `instance_id`/`source_id` 不变 |
| V6 | 幂等 | 对 V5 终态库再启动一次：无错误、账本仍 24、数据不变 |
| V7 | 前端 | `web` 的 type-check / lint / build（上游改了 120 个前端文件） |

> V5/V6 复用上轮的演练方法（新/旧二进制 + 同一 `DATA_DIR`），区别是**本轮不需要删账本**，正好用来证明 B 方案的升级路径成立。

## 4. 风险登记

| 风险 | 影响 | 缓解 |
|---|---|---|
| 上游 4 条迁移在 fork 链尾执行（晚于 fork 三条）出现顺序依赖问题 | 迁移失败或 schema 不一致 | 四条均为加列/建表型且带 `HasColumn`/`HasTable` 判存（非破坏性）；V2 全量测试 + V4/V5 实测兜底 |
| 冲突解决时误删 fork 的 donation / 凭据备注注册项 | fork 功能失效、账本不匹配 | V3 断言 + V5 对生产副本实测 |
| 上游迁移文件重命名漏改引用（注册表、测试） | 编译失败或断言失败 | 改完用 `rg -n 'ID00(18|19|20|21)\b' internal/` 复查零残留 |
| 上游 `web/` 大重构与 fork 前端改动叠加 | 前端构建失败 | fork 对 `web/` 改动很小（agent 面板、凭据备注），冲突自动合并；V7 兜底 |
| 生产副本演练泄漏敏感数据 | 安全问题 | 副本仅落在本地 `tmp/`，演练后删除；不外传、不入库 |

## 5. 交付与回滚

- **交付形态**：本地 `main` 上的 merge commit + spec 更新 + 生产升级说明。push / 镜像发布待用户确认。
- **回滚**：本轮不改动生产；本地若验证失败，`git merge --abort` 或重置到 `59d75050` 即可，无外部副作用。
- **生产升级说明**：升级只需 `docker compose pull gpt-load && docker compose up -d gpt-load`；验证账本 24 条、业务计数与身份 UUID 不变、健康检查通过；回滚仍为「恢复卷快照 + 旧镜像」成对操作（本次升级不改账本行，但旧镜像注册表只有 20 条，仍不支持直接换回）。

## 6. 与 spec 的同步

`.trellis/spec/backend/database-guidelines.md` 需从「fork 让位于上游、把本地迁移整体顺延」改为：

- fork 迁移编号**冻结**，不得再改动已发布编号；
- 上游新增迁移进入 fork 时，**顺延到 fork 链尾**（只改 ID/导出符号/文件名，schema 级内部名保持上游原样）；
- 迁移文件清单更新到 `0024`；
- 说明该约定的目的：保证已上线库的账本永远是注册表前缀，实现零停机追加升级。
