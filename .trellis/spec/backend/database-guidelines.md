# 数据库准则（Database Guidelines）

> gpt-load 持久层的实际约定。写任何触及 SQLite / GORM / 迁移的代码前先读这里。

---

## 总览

- **GORM v2 + 方言分派**。SQLite 是默认运行形态，同时代码库已实现 MySQL / PostgreSQL 方言路径（`internal/storage/migration_lock.go`、`.github/workflows/ci.yml` 的 `database-contract` 矩阵）。**不要**按"只支持 SQLite"来写新代码。
- **唯一入口**：`internal/storage` 是数据库开箱与迁移的 owner。业务包只 import 两个子包 —— `gpt-load/internal/storage/models` 与 `gpt-load/internal/storage/dbtx`。
- **storage 从不直接读环境变量**（`internal/storage/database.go` 注释明文规定）：DSN 解析与默认值归 `internal/platform/config`。
- **没有 repository 模式**（全仓 `grep "type.*Repository"` 零命中）。Service 结构体直接持有 `*gorm.DB`，接口在**消费方**定义且刻意收窄，典型例子是 `internal/requestlog/service.go` 里的 `passiveQuotaFlusher`。

---

## 打开数据库：source 语义决定权限行为

三个入口都在 `internal/storage/database.go`：

```go
func Open(dsn string) (*gorm.DB, error)                      // 等价于 external
func OpenWithSource(dsn string, source config.DatabaseSource)
func OpenConfigured(cfg *config.Config) (*gorm.DB, error)    // 生产路径，走 dig 容器
```

`config.DatabaseSource`（`internal/platform/config/config.go:59-66`）只有两个值，行为**完全不同**：

| source | 行为 |
|---|---|
| `managed` | 仅 SQLite 允许。打开前后各收紧一次 `db` / `db-wal` / `db-shm` 权限；文件库额外开 WAL。非 SQLite 直接报错 |
| `external` | **不动任何文件权限**，只打一条 `logExternalDatabaseSource` 日志；operator 自负其责 |

关键约束：

- 权限收紧走 `internal/platform/securefile`（Unix 实现在 `managed_unix.go`）：`O_NOFOLLOW` 防符号链接、fstat 校验属主、用 **fd 版 chmod** 防 TOCTOU。目录 `0700`、文件 `0600`。
- **fail closed**：无法安全确认目录/文件/属主/链接类型时直接拒绝启动。
- **首装哨兵**：`rejectExistingDatabaseWithoutMigrationLedger` 会以只读模式先查 —— 库里已有 `groups` 表但没有 `schema_migrations` 表，直接拒绝启动（防止把旧 Beta 库当新库建表）。
- **DSN pragma 会被强制覆盖**：`withSQLiteRuntimeOptions` 丢弃 operator 写的 `foreign_keys` / `busy_timeout` / `journal_mode`，强制写回 `foreign_keys(1)`、`busy_timeout(5000)`、`journal_mode(WAL)`；随后 `verifySQLiteRuntime` 把 pragma 读回来逐条断言，不匹配就关连接报错。
- **连接池**：`databasePoolLimits` 对 SQLite **硬编码 1/1**（单写者 + 共享 `:memory:` 兼容），忽略配置；网络库才读 `cfg.DatabasePool`。项目**不设置** `SetConnMaxLifetime` / `SetConnMaxIdleTime`。

---

## 模型定义

两套模型并存，互为**冻结关系**：

| 位置 | 用途 |
|---|---|
| `internal/storage/models/*.go` | 运行时模型（`group.go` / `access_key.go` / `request.go` / `model_price.go` / `system.go` / `subscription.go` / `control_operation.go`） |
| `internal/storage/migrations/000N_*.go` | **迁移局部冻结模型**，带版本后缀（`initialGroup`、`accessKeyCostLimitRule0002`） |

`migrations/0001_initial.go` 顶部注释是硬约束：迁移局部模型冻结未发布的 v2 基线，运行时模型只能通过后续迁移演进。

风格要点（以 `internal/storage/models/group.go` 为例）：

- **时间字段一律 `int64` 毫秒**，字段名 `XxxAtMS`，tag 形如 `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:..."`。**没有 `time.Time`、没有 `gorm.Model` 内嵌**。
- **不使用软删除**：全仓 `DeletedAt` / `Unscoped()` **零命中**，删除即硬删，模型里没有 `deleted_at` 列。
- **表名**：运行时模型**不写** `TableName()`（靠 GORM 复数化）；**迁移局部模型必须写**，如 `func (initialGroup) TableName() string { return "groups" }`。
- **约束命名**：索引 `idx_*`、检查约束 `chk_*`，check 里写完整 SQL 表达式（`secret_version > 0`、`kind IN ('total','periodic')`）。
- **可空用指针**（`*int64` / `*string`）；JSON 用 `models.JSON`（实现 `sql.Scanner` / `driver.Valuer` 并拒绝非法 JSON，见 `models/json.go`）。
- 模型里有 `BeforeSave` / `BeforeCreate` 钩子；注意 `models/group_test.go` 专门测"`BeforeSave` 不臆造 `ConnectionType`"（fail-closed），改钩子前先看这个测试。
- `models/group.go` 顶部注明：**API DTO 与运行时状态视图必须定义在 storage 包之外**。

---

## 查询与事务

**查询以链式为主**，`Raw` 只用于方言相关处（`PRAGMA foreign_keys` / `PRAGMA foreign_key_check` / `SELECT GET_LOCK`）。

```go
// internal/requestlog/credential_window_usage.go
query := db.Model(&models.RequestLog{}).
    Where("credential_id = ?", input.CredentialID).
    Where("completed_at_ms >= ? AND completed_at_ms < ?", input.FromMS, input.ToMS)
if err := query.Select(credentialRequestLogUsageSelect).Find(&row).Error; err != nil {
    return CredentialWindowUsage{}, fmt.Errorf("query request logs: %w", err)
}
```

- 长 SELECT 抽成包级常量（如上 `credentialRequestLogUsageSelect`）。
- `Pluck` / `Count` / `CreateInBatches` / `Clauses(clause.OnConflict{DoNothing: true})` 都在实际使用（`internal/requestlog/worker.go`）。

**不要用 upsert 的 RowsAffected 判定本次是否首次插入**：`internal/storage/database.go` 的 MySQL DSN 会强制 `clientFoundRows=true`，`ON DUPLICATE KEY UPDATE id=id` 的无变化匹配也可能返回 1。需要区分首次创建/幂等重放时，参照 `ReceiveDonationBatch` / `RetryDonationBatch`：普通 INSERT，唯一冲突让当前事务回滚，再用新读取比较原始持久摘要。PostgreSQL 唯一冲突会使当前事务失效，不能在同一失败事务中继续查询原行。详见 [捐献集成契约](./donation-integration.md)。

**事务统一走 `dbtx.Run`，不要用裸 `db.Transaction`**（后者只出现在 storage 迁移内部）：

```go
// internal/storage/dbtx/transaction.go
err := dbtx.Run(ctx, service.db, dbtx.Options{
    Mode:           dbtx.ReadSnapshot,   // 或 dbtx.Write
    CleanupTimeout: usageRollbackTimeout,
    Operation:      "credential window usage read transaction",
}, func(connection *gorm.DB) error { ... })
```

- `Mode` 驱动方言化 BEGIN（SQLite `BEGIN IMMEDIATE` / MySQL `START TRANSACTION WITH CONSISTENT SNAPSHOT` / PG `BEGIN ISOLATION LEVEL REPEATABLE READ`）。
- 错误语义是设计过的：回调错误**原样返回**保留业务语义，基础设施失败包成 `*dbtx.Error{Operation, Phase, Err}`，调用方用 `dbtx.IsInfrastructure(err)` 区分。
- 回调失败回滚、回滚或提交失败都会丢弃连接，防止带着未知事务状态回到池里。

**错误转换分两层**：

- HTTP 边界（`internal/control/*.go`）用 `app_errors.ParseDBError(err)` 转成 `*APIError`。它依赖 `gorm.Config{TranslateError: true}`（在 `internal/storage/database.go` 设置），并把 `ErrRecordNotFound` → `ErrResourceNotFound`、`ErrDuplicatedKey` → `ErrDuplicateResource`，兜底 `ErrDatabase`（**绝不把数据库错误原文返回给客户端**）。
- storage / requestlog / state 层**不用** `ParseDBError`，用 `fmt.Errorf("...: %w", err)` 包装上抛。

---

## 留存与清理：哪些数据会被删

清理由 `requestlog.(*Service).Sweep` 执行（`internal/requestlog/retention.go`）。**四张表会被裁剪**，各自留存的语义不同 —— 写历史数据分析类功能前必须先看这张表，用错数据源会静默低估：

| 表 | 留存 | 裁剪依据 |
|---|---|---|
| `request_logs`（逐请求明细） | `request_log_retention_days`，默认 **7 天**（`state.DefaultRuntimeSettings()`；可由系统设置改） | `completed_at_ms < cutoff` |
| `usage_aggregation_journal`（聚合幂等日志） | **35 天** | `bucket_start_ms < cutoff` |
| `credential_quota_histories`（订阅类渠道的百分比额度历史） | **35 天** | `observed_at_ms < cutoff` |
| `rpm_stats`（AccessKey / 凭据的分钟请求速率） | 与 `request_log_retention_days` 相同 | `minute_ms < cutoff` |
| **`usage_stats`（小时用量聚合）** | **永久保留，不裁剪** | — |

> **Warning**：`usage_stats` 是**唯一**不受留存影响的历史用量来源（`retention.go:22-24` 的文档注释明写 "Hourly aggregates are retained indefinitely"；`retention_test.go` 有 `TestRetentionSweepKeepsUsageStats…` 固定该行为）。因此**任何跨天/跨周期的 token 用量统计都应基于 `usage_stats`**；`request_logs` 只适合「留存窗口内的逐请求精确边界」，超出留存即静默缺数据。

**取窗口用量的两条路径**（都在 `internal/requestlog/credential_window_usage.go`）：

- `SourceHourlyStats`：整点对齐后查 `usage_stats` 取主体，再从 `request_logs` 合并首尾**不完整的小时**做精确边界；`DataComplete` 反映边界段是否仍在 `request_logs` 留存内。**单个凭据 + 单个窗口**的形态。
- `SourceRequestLogs`：整段走 `request_logs`，`DataComplete = requestLogWindowRetained(FromMS)`。窗口超出留存时统计不完整但**不报错** —— 调用方必须读 `DataComplete`。

⚠️ 需要**多个不同窗口**（例：按 `(凭据, 模型)` 各自的重置时刻划窗）时**不能**对每行调用上面这条单窗口接口 —— 那是 N 次查询。必须批量读取小时聚合与首尾边界明细，按每行窗口归并；两种来源须在同一个 `dbtx.ReadSnapshot` 内读取。**禁止仅将起点向上取整后舍弃首小时**：额度可能在几分钟内耗尽，时间误差小于一小时仍可造成 100% token 漏算。分组用量合同见下节。

### 分组模型用量的精确边界合同

1. **适用范围**：按 `(credential_id, upstream_model)` 各自划窗的管理统计，入口为 `GET /api/groups/:group_id/model-usage`，实现位于 `internal/requestlog/group_model_usage.go` 和 `internal/control/group_model_usage.go`。
2. **签名与来源**：读取当前组的凭据/模型窗口，批量使用 `usage_stats` 与 `request_logs`。不新增数据库表，不逐凭据调用 `QueryCredentialWindowUsage`。
3. **响应合同**：`window_start_ms` 是该行真实起点，`window_source` 区分重置锚点与近 24h 兜底；`data_complete` 表示所需边界明细的留存是否完整，不能代表上游 token 完整。`usage_missing_count` / `usage_partial_count` 分别统计缺少用量和部分用量的请求，小时桶与边界明细统一归并；失败请求也可能 missing，不能宣称每个 missing 请求都产生了实际消耗。顶层 `counted_from_ms` / `counted_to_ms` 是整体窗口覆盖范围，每行仍用自己的起点。token 含全部缓存分项；成功以 `status = 'success'` 定义，不以 HTTP 200 代替。
4. **校验与错误矩阵**：非法范围/损坏聚合数值 → 返回错误；首尾同小时 → 明细区间只合并一次；边界明细超留存 → 返回已知数值并标记 `data_complete=false`，不能静默宣称完整；并发写入 → 用同一 DB 快照避免混合新旧数据。
5. **正常/基础/异常样例**：09:43 起、09:50 前耗尽的全部 token 必须计入；整点窗口直接复用完整桶；边界缺失时 UI 显示统计不完整，不能把 0 当作上游没有消耗额度。
6. **必要测试断言**：同小时爆发用量、首尾重叠、整点、当前小时新行、多模型不同窗口、删除凭据过滤、留存不完整、批量查询数量不随 key 数逐个增长。复用现有安全整数和 `UsageAggregate` 求和校验。
7. **错误与正确做法**：错误：`ceilHour(start)` 后只查小时桶。正确：完整小时桶 + `[start, ceilHour(start))` 和 `[floorHour(end), end)` 的精确明细，先裁到 `[start,end)` 并消除重叠；禁止用前周期的整个边界桶补数。

---

### 模型冷却与额度周期的证据边界

1. **触发场景**：限流后的调度等待到期不等于额度重置；订阅额度恢复可能提前解除额度冷却，不能因此重置用量窗口。
2. **签名**：`health.Decision` 独立携带 `QuotaCooldown`、`QuotaResetAt`、`QuotaResetSource`。gateway 对额度模型冷却调用 `SetQuotaCooldownWithReset(ref, model, until, reset, now) (accepted, changed bool)`，普通/custom 模型冷却调用 `SetModelCooldownWithReset`；账号额度冷却调用 `SetQuotaCooldown(ref, "", until, now)`。`RestoreQuotaCooldowns(id)` / `RestoreObservedQuotaCooldowns(id, observedAtMS)` 只恢复额度冷却。
3. **状态合同**：`QuotaCooldownUntil` / `QuotaModelCooldowns` 保存额度冷却，`CooldownUntil` / `ModelCooldowns` 保存其他冷却；`ModelCycleStarts` / `ModelNextResets` 保存可信周期。写入在 registry 同一锁内校验组、身份和模型恢复代次；额度冷却及任何非零 trusted reset 还需匹配 `QuotaCooldownGeneration`。恢复只清额度地图并推进该代次，即使没有现存冷却也要拒绝恢复前的在途额度结果；保留普通冷却、黑名单、失败状态和周期。`RuntimeView` / `ModelCooldowns` / `GroupModelCycles` 合并两类有效期限，统计只读可信周期。重建后 `SchedulingState` 必须同步实际保留后的 buckets，不能使用尚未保留运行时状态的原始输入 entries。
4. **校验矩阵**：模型为空/含外围空白、期限不晚于 now、非零 reset 不晚于 now、身份或恢复代次不匹配 → 拒绝模型状态写入。普通无 reset 冷却不因额度恢复而被拒绝。额度观察时间不严格晚于最近额度拒绝 → 不解除冷却。只有 `model_cycle_version=1` 恢复周期；旧/未知 checkpoint 版本保留健康冷却、丢弃周期并回退 `fallback_24h`。
5. **正常/基础/异常样例**：较长冷却中收到较短冷却但新的可信 reset，仍保存 reset；额度恢复后用量窗口/token 不变，普通冷却仍有效；重复提示不提前截断周期。可信来源仅为已验证的 `Daily free limit reached on model … Try again in …`；一般 `Try again in`、默认 60 秒及 `Retry-After` 均不产生周期。`ParseResetHint` 的一分钟余量/钳制只用于调度，不能进入统计边界；HTTP 200/0 的正文仍可能提供限流证据。
6. **必要回归**：`quota_model_cycle_test.go` 覆盖两套地图与可信 reset、恢复后的旧在途写回拒绝、精确快照/重建/checkpoint/身份变化、`ReplaceCredentials` 调度投影；`gateway/model_cooldown_test.go` 覆盖配置链与余量分离，`health/model_cooldown_test.go` 覆盖内置/custom/已提交作用域，`control/group_model_usage_test.go` 验证恢复不截断真实用量。旧版 checkpoint 和 missing/partial 用量提示仍须覆盖。
7. **错误/正确**：错误：把 `CooldownUntil` 当作周期证据，或先写普通冷却再补额度冷却。正确：按决策选择一种冷却地图，并在同一身份/代次校验与锁内独立保存可信 reset；恢复额度时保持周期和其他健康状态。

## 迁移

`internal/storage/migration.go` 是核心：**版本化注册表 + 每个迁移内部调用 GORM 的 AutoMigrate**。

```go
var migrations = []migration{
    {ID: migrationfiles.ID0001, Up: migrationfiles.Up0001, Validate: ..., ValidateCurrent: ..., ValidateRecoverable: ...},
    // ... 至 ID0033
}
func AutoMigrate(db *gorm.DB) error { return applyMigrations(db) }
```

**注意**：这里的 `AutoMigrate` **不是** GORM 自带的方法，而是本项目的迁移执行器。

规则：

- ID 必须匹配 `^(\d{4})_[a-z0-9]+(?:_[a-z0-9]+)*$`，且编号必须等于数组下标 —— `validateMigrationRegistry` 会自我校验，三个函数指针都不能为 nil。
- 导出符号按迁移形状分两类，**不是每个迁移都导出一整套**：
  - **建表型**：`Up000N` / `Validate000N` / `ValidateCurrent000N` / `ValidateRecoverable000N`，外加 `SchemaModels000N() []any`（确定性 DDL 顺序）与 `TableNames000N() []string`（先例 `0019_donation_manual_review.go`）。
  - **加列 / 改列型**：只需 `Up000N` / `Validate000N` / `ValidateRecoverable000N`，**不导出** `SchemaModels` / `TableNames` / `ValidateCurrent`（先例 `0005_proxy_config.go`、`0014_affinity_kind.go`、`0020_credential_note.go`）。`Up` 内先 `HasColumn` 判重再 `ALTER TABLE`，末尾 `return Validate000N(db)`。
- 文件命名：`0001_initial.go` … `0014_affinity_kind.go`、`0015_group_usage_index.go`、`0016_credential_quota_history.go`、`0017_request_log_operation_index.go`、`0018_donation_intake.go`、`0019_donation_manual_review.go`、`0020_credential_note.go`、`0021_auto_model.go`、`0022_auto_decision_attribution.go`、`0023_client_model_overrides.go`、`0024_request_audit.go`、`0025_rpm_stats.go`、`0026_access_key_concurrency.go`、`0027_request_log_output_timing.go`。fork 的 `0021`–`0027` 分别来自上游 `0018`–`0024`，顺延原因见下面的编号约定。
- 追加迁移会连带改到**断言完整链**的测试，漏改就红：`internal/storage/migration_test.go`（注册表 ID 列表）、`internal/storage/db_test.go`（`schema_migrations` ID 列表）、`internal/storage/database_integration_test.go`（账本长度 + 期望列清单）。另注意**建旧 schema 的测试**要用 `db.Omit("新列")` 建行（先例 `migrations/0003_remove_observation_fresh_until_test.go` 的 `Omit("ProxyConfig", "Note")`），否则 GORM 会往尚不存在的列里写。
- **账本顺序即身份**：`applyMigrationsLocked` 用数组下标逐条比对 `schema_migrations` 里的 ID，任何一条对不上就拒绝启动。因此迁移只能追加在链尾，**不得插入、重排或重编号已发布的迁移**。
- **fork 与上游的编号约定（2026-09-23 起）**：fork 自己的迁移编号**冻结不动**；上游新增迁移进入 fork 时**顺延到 fork 链尾**（例：上游 `0018`–`0021` → fork `0021`–`0024`）。这样已上线库的账本恒为注册表的**前缀**，升级退化为 `docker compose pull` + `up -d`，无需停机、无需改账本。
  - 顺延只改：文件名、`ID00NN` 与 `Up00NN` / `Validate00NN` / `ValidateCurrent00NN` / `ValidateRecoverable00NN` / `SchemaModels00NN` / `TableNames00NN` 等导出符号、注册表与链式断言。
  - **不改上游迁移内部会落进 DDL 的名字**（`auto_decision_usage_stats_0019`、`chk_auto_decision_usage_0019_*`、`autoLog0019` / `autoUsage0019` 等）：改名会让 fork 库 schema 与上游永久分叉，上游后续迁移按名引用时会错位。文件顶部加一行来源注释即可。
  - 反例：把 fork 迁移整体顺延到上游之后（2026-09-20 曾采用）会让已上线库的账本中段与注册表错位，每次合并都要停机 + 删账本行 + 重放迁移。
- 测试里若需要表达「某迁移之前的链前缀」，**按 ID 定位下标**，不要用 `len(migrations)-1` —— 追加迁移后该表达式会静默指向别的迁移（见 `operationIndexMigrationIndex`）。
- 2026-10-08 新链尾：上游 `0025_proxy_catalog`、`0026_credential_names`、`0027_remove_request_log_output_timing`、`0028_request_log_client_ip`、`0029_group_priority` 分别顺延为 fork `0028`–`0032`；旧 `0001`–`0027` 生产迁移文件保持原样。
- 2026-10-21 当前链尾：上游 `0030_request_body_recording` 追加为 fork `0033_request_body_recording`；旧 `0001`–`0032` 生产迁移 Git blob 保持不变。`accessKey0030`、`requestLogBody0030` 和实际表/约束名字保留上游形态。
- **上游迁移测试常硬编码注册表下标**（`migrations[:18]`、`migrations[19]`、`migrations[:20]`）。顺延编号后这些下标会指向别的迁移：要么直接失败，要么**静默测错对象却仍然通过**。合并上游并顺延编号时，必须把这类文件改成按 ID 定位（先例：`autoDecisionAttributionMigrationIndex`、`clientModelOverrideMigrationIndex`、`requestAuditMigrationIndex`）。
- `ValidateRecoverable000N` 用于检测中断的 MySQL 半成品表（表存在但非空、有意外列 → 拒绝继续）。

### 合并上游时的注册表完整性

1. **适用范围**：上游新增迁移编号与 fork 已发布编号重叠时，即使 `git merge` 没有报告 `migration.go` 冲突，也必须检查完整注册表。
2. **签名与结构**：`migrations []migration` 的 `ID`、`Up`、`Validate`、`ValidateCurrent`、`ValidateRecoverable` 都须与目标迁移对应；例如上游 `0022_rpm_stats` 在 fork 注册为 `ID0025` / `Up0025` / `Validate0025` / `ValidateRecoverable0025`。
3. **合同**：旧账本是新链的严格前缀。已有 `ID0022` 仍属于 `auto_decision_attribution`，不能为修复同名新符号而全仓替换旧编号。只迁移新文件和它的调用者；DDL 名字保留上游形态。
4. **校验与错误矩阵**：同名导出符号 → 编译失败；新文件存在但注册项遗漏 → 新表/列缺失；旧编号重排 → 启动时账本不匹配；迁移测试硬编码下标 → 可能测错对象却仍通过。
5. **正常/基础/异常样例**：旧 24 条 + 新 3 条 = 27 条；没有新增迁移时旧链不变；自动合并只留下 `ID0001`–`ID0024` 却同时增加三个文件属于错误结果。
6. **必要测试断言**：完整 ID 列表、空库迁移、带 fork 业务记录和身份数据的旧链升级、旧账本和记录不变、二次执行幂等；新测试按 ID 查找迁移边界。
7. **错误与正确做法**：错误：消除冲突标记后直接认定迁移合并成功。正确：逐个对应「迁移文件 → 唯一导出符号 → 注册项 → 正确语义边界的测试」，并验证升级前缀与数据不变。

### 清理列迁移与旧版本校验

1. **触发**：后续迁移删除旧迁移创建的列，例如 `0030_remove_request_log_output_timing` 删除 `first_output_ms` / `last_output_ms`。
2. **签名**：新文件定义 `ValidateCurrent0027(*gorm.DB) error`；注册表将其挂在旧 `ID0027` 的 `ValidateCurrent`，旧文件与严格 `Validate0027` 保持冻结。
3. **合同**：仅账本有 `ID0030` 时允许按新结构校验；MySQL 的 `ID0030#building` 可进入恢复校验。上游旧 `0024` 的所有校验引用在 fork 指向 `0027`，不可误指 `0024_request_audit`。
4. **错误矩阵**：无清理账本但缺列 → 拒绝；有孤立约束或错误删除顺序 → 拒绝；合法清理完成 → 重启成功；MySQL 合法中断 → 继续恢复。
5. **样例**：27→33 保留业务数据并由既有0030移除两列；32→33只追加请求体记录；新库直接到33；仅手工删除旧列不构成升级证据。
6. **断言**：`fork_upgrade_migration_test.go` 覆盖24/27/32→33及关闭重开幂等，`remove_output_timing_migration_test.go` 覆盖无证据缺列、错误顺序与恢复。SQL查询预算按完整fork schema校准，重启元数据门槛仍需保留。
7. **错误/正确**：只重编号新 `ID0030` 会留下旧校验引用错位；正确做法同时映射跨迁移引用和 `ValidateCurrent` 注册，并验证重复启动。

方言分支（改迁移时最容易踩的地方）：

- **SQLite**（`migration_sqlite.go`）：pin 连接、`PRAGMA foreign_keys = OFF`（否则重建被引用表会级联删数据）、`BEGIN IMMEDIATE`、`useMigrationTransactions=false`、COMMIT 后恢复 FK 并**回读校验**，清理失败就 `driver.ErrBadConn` 丢弃连接。
- **MySQL**（`migration_recovery.go`）：DDL 隐式提交，用 `<id>#building` **恢复标记** + `ValidateRecoverable` 门禁，最后在真事务里 delete 标记、insert 正式 ID。
- **网络库迁移锁**（`migration_lock.go`）：`SELECT GET_LOCK('gpt-load:schema-migrations:v2', 60)` / `pg_try_advisory_lock(hashtext(?))`，带 100ms 重试循环。
- **迁移后完整性**：`migration_validation.go` 跑 `PRAGMA foreign_key_check`（仅 SQLite）。

### 请求体记录与追加迁移 0033

1. **适用范围**：访问密钥显式开启客户端请求体记录；与错误诊断摘要的脱敏链分开，由 gateway/telemetry/requestlog/control 传递。
2. **签名与结构**：`AccessKey.RecordRequestBody bool` ↔ `record_request_body`，默认 false；`ID0033` 注册 `Up0033` / `Validate0033` / `ValidateRecoverable0033`。`request_log_bodies(request_id PK/FK → request_logs.id ON DELETE CASCADE, state, original_bytes, body)` 保存记录，`GET /api/logs/:request_id` 的 `request_body` 为 null 或 `{state, original_bytes, body}`。
3. **合同**：仅开启开关的访问密钥记录客户端原始 HTTP 正文及 WebSocket turn frame；不开启时不记录。管理员和该访问密钥本人可在既有日志可见范围内查看，读取正文前必须先确认日志归属。正文表随请求日志删除，永久小时用量不受影响。原样正文最多 256 KiB；较大合法 JSON 仅截断字符串值，仍放不下则只留状态/原始大小；待写正文预算为 64 MiB。详情格式化不得超过 1 MiB，超出时返回存储文本。
4. **校验与错误矩阵**：`original` / `truncated` 必须有正文；`non_json` / `too_large` / `queue_full` 正文必须 null；非法 state、负 original_bytes、孤立 request_id 均由数据库约束拒绝。未记录 → request_body=null；越权请求 ID → 既有 unauthorized/not-found 出口，不能直接读取正文绕过归属。
5. **正常/基础/异常样例**：旧32条链升级只追加0033且开关关闭；显式开启后保存请求正文，二次启动保持开关/正文；未开启、非法JSON、过大或队列预算不足按对应状态处理。旧 fixture 用当前模型建行时 `Omit("RecordRequestBody")`，避免写入尚不存在的列。
6. **必要回归**：`request_body_recording_migration_test.go` 按 `ID0033` 定位 fresh/upgrade/interrupted 边界、FK/check/cascade；`fork_upgrade_migration_test.go` 核对旧账本严格前缀与业务/身份/备注/捐献逐列数据保留；gateway 断言 opt-in HTTP/WebSocket，control 断言 admin/owner 读取与格式化预算，requestlog 断言持久化/留存/队列预算。真实 MySQL/PostgreSQL 未配置 DSN 时必须注明未实测。
7. **错误/正确**：错误：用上游0030覆盖 fork 已发布0030，或按 `migrations[:29]` 测试错误边界。正确：只追加0033，保持内部DDL名称与旧32个迁移 blob，并以语义ID定位升级测试、验证关闭重开及重复执行。

---

## 测试中的数据库

- **默认内存库**：`storage.Open(":memory:")`，或跨包 fixture `sqlitetest.OpenMigrated(t)`（`internal/testutil/sqlitetest/sqlitetest.go`）。后者用 `sync.Once` 跑一次真实 `storage.AutoMigrate`，把 DDL dump 成模板 SQL，之后每个测试只 `Exec` 一次 —— schema 永远与真实迁移一致，且测试之间完全隔离。需要文件库时用 `t.TempDir()`。
- **真实 MySQL / PG 仅 opt-in**：设了 `GPT_LOAD_DATABASE_TEST_DSN` 才跑，否则 `t.Skip`。
- **不连服务器也能断言方言 SQL**：三种 dialector + `gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true}`，范本见 `internal/requestlog/worker_test.go` 的 `TestUsageStatUpsertUsesDialectSpecificGORMConflictSQL`。
- **不要混淆两组"方言"**：`fakeupstream` 的三方言指 **OpenAI / Anthropic / Gemini 协议**（`internal/testutil/fakeupstream/testdata/{openai,anthropic,gemini}/`），与数据库方言 sqlite/mysql/postgres 无关。

---

## 常见错误（反模式）

- ❌ 在业务包里定义持久化模型 —— 必须放 `internal/storage/models`。
- ❌ 用 `time.Time` / `gorm.Model` / `gorm.DeletedAt` 建模 —— 本项目一律 int64 毫秒 + 硬删除。
- ❌ 绕过 `dbtx.Run` 直接在业务代码里 `db.Transaction(...)`。
- ❌ 在 storage / requestlog / state 层调用 `ParseDBError` —— 那是 HTTP 边界的职责。
- ❌ 让 `external` source 的库被收紧权限，或让 `managed` 去动 operator 管的文件。
- ❌ 给 SQLite 调大连接池 —— `databasePoolLimits` 已硬编码 1/1。
- ❌ 指望 operator 写在 DSN 里的 pragma 生效 —— 会被 `withSQLiteRuntimeOptions` 覆盖。
- ❌ 新增迁移时跳号或改已发布迁移的 ID 格式 —— 注册表自校验会直接失败。

---

## 验证命令

```bash
go test -count=1 ./internal/storage/...     # 迁移与权限
go test -count=1 ./internal/...             # 全量
make check                                  # 完整门禁
```
