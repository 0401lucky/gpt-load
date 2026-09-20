# 技术设计：合并上游并重编号 donation 迁移

## 1. 迁移编号重排方案

### 1.1 重编号映射

| 旧 ID（fork） | 新 ID | 常量前缀变更 |
|---|---|---|
| `0015_donation_intake` | `0018_donation_intake` | `*0015` → `*0018` |
| `0016_donation_manual_review` | `0019_donation_manual_review` | `*0016` → `*0019` |

上游 `0015`/`0016`/`0017` 保持原编号不动 —— fork 永远让位于上游，避免后续再撞。

### 1.2 文件重命名

```
internal/storage/migrations/0015_donation_intake.go            → 0018_donation_intake.go
internal/storage/migrations/0016_donation_manual_review.go     → 0019_donation_manual_review.go
internal/storage/migrations/0016_donation_manual_review_test.go → 0019_donation_manual_review_test.go
```

### 1.3 标识符替换范围

必须替换，且**仅**替换带数字后缀的标识符（避免误伤 `donation_items__0016` 这类迁移内自洽的临时表名需一并处理，见下）：

- 导出符号：`ID00NN`、`Up00NN`、`Validate00NN`、`ValidateCurrent00NN`、`ValidateRecoverable00NN`、`SchemaModels00NN`、`TableNames00NN`
- 包内私有符号：`donationIdentity0015`、`donationBatch0016`、`rebuildSQLiteDonationItems0016`、`donationItemColumns0016`、`donationConstraintNormalizer0016` 等约 40 处
- 迁移内临时表名：`donation_items__0016` → `donation_items__0019`（仅在单个迁移函数体内构造与消费，改名不影响已落库 schema）

**替换安全网**：替换后用 `grep -n "0015\|0016" internal/storage/migrations/00{18,19}_*.go` 复查，确认残留项均为上游语义或已解释的常量。

### 1.4 引用点同步

| 文件 | 变更 |
|---|---|
| `internal/storage/migration.go` | 注册表顺序：保留上游三条，donation 两条追加为第 18/19 项 |
| `internal/storage/migration_test.go` | `wantIDs` 补充至 19 项 |
| `internal/storage/db_test.go` | `wantMigrationIDs` 补充至 19 项 |
| `internal/storage/database_integration_test.go` | `len(migrationIDs)` 与链式断言改为 `0001`–`0019` |
| `internal/control/donation_database_integration_test.go` | `migrationfiles.ID0015/ID0016` → `ID0018/ID0019` |
| `internal/control/donation_manual_database_integration_test.go` | 同上（`migrations.ID0016`） |

## 2. 账本升级协议（核心设计）

### 2.1 问题

生产库账本 16 条，新代码注册表 19 条，且第 15/16 位语义不同：

```
生产库下标 14: 0015_donation_intake         新注册表 14: 0015_group_usage_index   ← 不匹配，启动即失败
```

`applyMigrationsLocked` 的校验按**下标严格比对 ID**，无任何容忍分支，故直接换镜像必然启动失败。

### 2.2 方案：账本前移 + 幂等重放

**升级步骤**（停机窗口内）：

1. 停止 `gpt-load-2` 容器（释放 SQLite 写锁，触发 WAL 落盘）。
2. 备份整个 `gptload2-data` 卷。
3. 对生产库执行账本修正：
   ```sql
   DELETE FROM schema_migrations WHERE id IN ('0015_donation_intake', '0016_donation_manual_review');
   ```
   账本退回 14 条，下标 0–13 与注册表完全一致。
4. 换用新镜像启动。系统按顺序执行 5 条迁移：
   - `0015_group_usage_index`、`0016_credential_quota_history`、`0017_request_log_operation_index` —— 上游新增，全新执行。
   - `0018_donation_intake`（原 `Up0015`）—— `AutoMigrate` 对已存在表幂等。
   - `0019_donation_manual_review`（原 `Up0016`）—— 见 2.3 幂等性论证。

### 2.3 `Up0019`（原 `Up0016`）幂等性论证

逐项检查代码路径，确认在"目标 schema 已就位"的库上重跑是空操作：

| 步骤 | 代码 | 幂等依据 |
|---|---|---|
| 前置检查 | `HasTable(donation_items)` / `HasTable(donation_batches)` | 已存在则继续 |
| 可恢复校验 | `ValidateRecoverable0016(db)` | 只读校验，已满足 |
| 加 `validation_mode` 列 | `if !HasColumn(...)` | 存在则跳过 |
| SQLite 表重建 | `rebuildSQLiteDonationItems0016` | 起始即判断 DDL 是否已含目标状态约束与 `chk_donation_item_revision`，满足则提前返回 |
| 非 SQLite 加列 | 逐列 `if HasColumn(...) { continue }` | 存在则跳过 |
| 状态约束替换 | `replaceDonationStateConstraint0016` | 按名判存，存在则先 drop 再建（等价重建，结果一致） |
| 补建约束 | `if HasConstraint(...) { continue }` | 存在则跳过 |
| 审核表 | `AutoMigrate` | 幂等 |
| 终态校验 | `Validate0016(db)` | 只读校验，已满足 |

**结论：代码层面幂等性成立。** 但 `AC4` 要求实测证明 —— 该论证必须由在三库上的实际演练结果背书，而非仅凭阅读。

### 2.4 为何不采用其他方案

| 方案 | 否决理由 |
|---|---|
| 手工执行上游 0015–0017 的 DDL 并补写账本 | 需手工复刻索引/建表语句，与代码漂移风险高，且无法用代码校验兜底 |
| 把 donation 迁移插到 0014 之前 | 违反位置语义，且生产库已固化 15/16 位，改动面更大 |
| 让上游迁移顺延到 0018+ | fork 需长期维护补丁，下次上游更新继续撞 |
| 重建生产库 | 439 条已接收资源与持久身份 UUID 将丢失，不可接受 |

## 3. 验证矩阵

### 3.1 三库演练（AC4/AC5）

本地现成靶子即"已跑旧 donation 迁移"的状态：

| 环境 | 现状 | 演练内容 |
|---|---|---|
| `gl-verify-pg`（PostgreSQL 15） | 账本 16 条，含旧 donation 两条 | 删两条 → 跑新迁移 → 断 19 条 + 数据完整 → 再跑一次断幂等 |
| `gl-verify-mysql`（MySQL 8.4） | 同上 | 同上 |
| 本地 SQLite 复刻 | 由测试夹具构造 16 条账本后升级 | 同上 |
| **生产同构验证** | `/tmp` 上的生产库副本（只读排查中已证明可行） | 用副本预演，**不触碰生产卷** |

生产为 SQLite，故 SQLite 路径是最高优先级，但三库须一致通过（仓库既有质量基线）。

### 3.2 数据完整性判据

升级前后比对下列计数与身份，必须完全一致：

```
donation_identities=1, donation_batches=30, donation_items=36,
donation_resources=439, donation_review_actions=23, donation_test_attempts=24
instance_id=c1e34efe-1f8b-45b7-897f-168d7e1ab926
source_id=49193b3f-900b-446e-9f6b-a426d1014dc7
```

（数值取自 2026-09-20 生产排查；**实际演练以演练当刻的实测值为准**。）

### 3.3 构建与回归

`make check` 为总门禁：gofmt、`go mod tidy -diff`、`go vet ./...`、前端 lint/format/build、`go build`、`go test . ./internal/...`、`git diff --check`。

## 4. 交付与回滚

### 4.1 交付链

```
本地合并+重编号 → make check → push main → ghcr-main.yml (build→verify→promote) → :main / :latest 滚动 tag
```

`:main` tag 不写死 sha（沿用既有约定），版本号可用于反查提交。

### 4.2 回滚设计（三级）

| 级别 | 触发场景 | 动作 |
|---|---|---|
| L1 镜像回滚 | 新版本启动异常 | compose 改回 `ghcr.io/0401lucky/gpt-load:rollback-<date>`（升级前对旧 digest 打 tag），`up -d` |
| L2 账本回滚 | 迁移中途失败 | 恢复升级前的 `gptload2-data` 卷快照，连同 `encryption.key` 一起还原 |
| L3 数据回滚 | 数据异常 | 上述卷快照即全量数据备份，含 439 条资源与身份 UUID |

**关键约束**：L1 单独不足以回滚 —— 账本已被修正为 19 条，旧镜像（16 条注册表）同样会因下标不匹配而启动失败。因此镜像回滚**必须**与卷快照恢复配套，手册中需显式说明。

### 4.3 停机窗口注意事项

- 生产库存在活跃 WAL（6.4 MB），升级前须 `docker compose stop` 而非 `restart`，确保连接释放、WAL 落盘。
- 备份在容器停止后进行，避免复制到不一致快照。
- 备份须同时覆盖 `gpt-load.db`、`gpt-load.db-wal`、`gpt-load.db-shm`（若存在）与 `encryption.key` —— 缺 `encryption.key` 则已接收的密钥密文不可解。

## 5. 风险登记

| 风险 | 影响 | 缓解 |
|---|---|---|
| `Up0019` 在目标 schema 已就位时非幂等 | 迁移失败或数据被改写 | 三库实测 + 生产副本预演（AC4/AC5） |
| 上游 `web/` 大重构破坏本地构建 | `make check` 失败 | 本地未改 `web/`，冲突面为零；构建结果为准 |
| 上游 `workflow_self_hosted_test.go` 与本地 `workflow_main_images_test.go` 断言互斥 | CI 失败 | 合并后立即跑该测试文件确认 |
| SQLite 表重建过程若中途失败 | 库损坏 | 已在 `BEGIN IMMEDIATE` 事务内执行，失败整体回滚；另有卷快照兜底 |
| 生产库体积（163 MB）导致备份耗时 | 停机窗口变长 | 实测备份耗时并写入手册 |
