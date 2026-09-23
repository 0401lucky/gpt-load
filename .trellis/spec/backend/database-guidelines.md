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

## 迁移

`internal/storage/migration.go` 是核心：**版本化注册表 + 每个迁移内部调用 GORM 的 AutoMigrate**。

```go
var migrations = []migration{
    {ID: migrationfiles.ID0001, Up: migrationfiles.Up0001, Validate: ..., ValidateCurrent: ..., ValidateRecoverable: ...},
    // ... 至 ID0020
}
func AutoMigrate(db *gorm.DB) error { return applyMigrations(db) }
```

**注意**：这里的 `AutoMigrate` **不是** GORM 自带的方法，而是本项目的迁移执行器。

规则：

- ID 必须匹配 `^(\d{4})_[a-z0-9]+(?:_[a-z0-9]+)*$`，且编号必须等于数组下标 —— `validateMigrationRegistry` 会自我校验，三个函数指针都不能为 nil。
- 导出符号按迁移形状分两类，**不是每个迁移都导出一整套**：
  - **建表型**：`Up000N` / `Validate000N` / `ValidateCurrent000N` / `ValidateRecoverable000N`，外加 `SchemaModels000N() []any`（确定性 DDL 顺序）与 `TableNames000N() []string`（先例 `0019_donation_manual_review.go`）。
  - **加列 / 改列型**：只需 `Up000N` / `Validate000N` / `ValidateRecoverable000N`，**不导出** `SchemaModels` / `TableNames` / `ValidateCurrent`（先例 `0005_proxy_config.go`、`0014_affinity_kind.go`、`0020_credential_note.go`）。`Up` 内先 `HasColumn` 判重再 `ALTER TABLE`，末尾 `return Validate000N(db)`。
- 文件命名：`0001_initial.go` … `0014_affinity_kind.go`、`0015_group_usage_index.go`、`0016_credential_quota_history.go`、`0017_request_log_operation_index.go`、`0018_donation_intake.go`、`0019_donation_manual_review.go`、`0020_credential_note.go`、`0021_auto_model.go`、`0022_auto_decision_attribution.go`、`0023_client_model_overrides.go`、`0024_request_audit.go`。后四条本属上游 `0018`–`0021`，顺延原因见下面的编号约定。
- 追加迁移会连带改到**断言完整链**的测试，漏改就红：`internal/storage/migration_test.go`（注册表 ID 列表）、`internal/storage/db_test.go`（`schema_migrations` ID 列表）、`internal/storage/database_integration_test.go`（账本长度 + 期望列清单）。另注意**建旧 schema 的测试**要用 `db.Omit("新列")` 建行（先例 `migrations/0003_remove_observation_fresh_until_test.go` 的 `Omit("ProxyConfig", "Note")`），否则 GORM 会往尚不存在的列里写。
- **账本顺序即身份**：`applyMigrationsLocked` 用数组下标逐条比对 `schema_migrations` 里的 ID，任何一条对不上就拒绝启动。因此迁移只能追加在链尾，**不得插入、重排或重编号已发布的迁移**。
- **fork 与上游的编号约定（2026-09-23 起）**：fork 自己的迁移编号**冻结不动**；上游新增迁移进入 fork 时**顺延到 fork 链尾**（例：上游 `0018`–`0021` → fork `0021`–`0024`）。这样已上线库的账本恒为注册表的**前缀**，升级退化为 `docker compose pull` + `up -d`，无需停机、无需改账本。
  - 顺延只改：文件名、`ID00NN` 与 `Up00NN` / `Validate00NN` / `ValidateCurrent00NN` / `ValidateRecoverable00NN` / `SchemaModels00NN` / `TableNames00NN` 等导出符号、注册表与链式断言。
  - **不改上游迁移内部会落进 DDL 的名字**（`auto_decision_usage_stats_0019`、`chk_auto_decision_usage_0019_*`、`autoLog0019` / `autoUsage0019` 等）：改名会让 fork 库 schema 与上游永久分叉，上游后续迁移按名引用时会错位。文件顶部加一行来源注释即可。
  - 反例：把 fork 迁移整体顺延到上游之后（2026-09-20 曾采用）会让已上线库的账本中段与注册表错位，每次合并都要停机 + 删账本行 + 重放迁移。
- 测试里若需要表达「某迁移之前的链前缀」，**按 ID 定位下标**，不要用 `len(migrations)-1` —— 追加迁移后该表达式会静默指向别的迁移（见 `operationIndexMigrationIndex`）。
- **上游迁移测试常硬编码注册表下标**（`migrations[:18]`、`migrations[19]`、`migrations[:20]`）。顺延编号后这些下标会指向别的迁移：要么直接失败，要么**静默测错对象却仍然通过**。合并上游并顺延编号时，必须把这类文件改成按 ID 定位（先例：`autoDecisionAttributionMigrationIndex`、`clientModelOverrideMigrationIndex`、`requestAuditMigrationIndex`）。
- `ValidateRecoverable000N` 用于检测中断的 MySQL 半成品表（表存在但非空、有意外列 → 拒绝继续）。

方言分支（改迁移时最容易踩的地方）：

- **SQLite**（`migration_sqlite.go`）：pin 连接、`PRAGMA foreign_keys = OFF`（否则重建被引用表会级联删数据）、`BEGIN IMMEDIATE`、`useMigrationTransactions=false`、COMMIT 后恢复 FK 并**回读校验**，清理失败就 `driver.ErrBadConn` 丢弃连接。
- **MySQL**（`migration_recovery.go`）：DDL 隐式提交，用 `<id>#building` **恢复标记** + `ValidateRecoverable` 门禁，最后在真事务里 delete 标记、insert 正式 ID。
- **网络库迁移锁**（`migration_lock.go`）：`SELECT GET_LOCK('gpt-load:schema-migrations:v2', 60)` / `pg_try_advisory_lock(hashtext(?))`，带 100ms 重试循环。
- **迁移后完整性**：`migration_validation.go` 跑 `PRAGMA foreign_key_check`（仅 SQLite）。

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
