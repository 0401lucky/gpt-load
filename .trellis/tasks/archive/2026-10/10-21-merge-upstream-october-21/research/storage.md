# 存储层兼容合并与升级验证（2026-10-21）

## 实现结果

- 固定 fork 起点：`216024cc31c5de4c31a11777c41746bf9ac752d3`；固定上游：`41c82ac02be5c26bd8851256236036d404e4829b`。
- 已解决 `database_integration_test.go`、`db_test.go`、`migration.go`、`migration_test.go` 的工作树冲突，保留 fork 的 0001–0032 迁移链及捐献表，追加请求体记录到 0033。
- 上游 `migrations/0030_request_body_recording.go` 在工作树中移至 `0033_request_body_recording.go`；仅改 ID、导出函数符号和来源注释。内部 `accessKey0030`、`requestLogBody0030` 等模型、DDL 表名、检查约束和外键保持上游原样。
- 注册表旧 32 项保持顺序及函数映射，尤其保留 `ID0027.ValidateCurrent = ValidateCurrent0027`，使已有 0030 清理账本的数据库能重复启动。
- 完整注册表、SQLite 建表与 opt-in 外部库生命周期断言同步为 33 项；保留旧捐献、备注、名称、并发限制、代理、优先级和请求客户端 IP 合同。
- 请求体迁移测试通过 `migrationIndexByID(t, ID0033)` 定位语义边界，移除上游固定 `[:29]` / `[29]`。历史 AccessKey fixture 在列尚不存在时省略 `RecordRequestBody`，不修改冻结迁移模型。
- `migration_validation_queries_test.go` 的注释更新为本轮实测 33 条链、1160 次初始化查询；初始化和重复启动的门槛保持原值。

## 已发布源码一致性

以 `git ls-tree HEAD internal/storage/migrations` 枚举全部 32 个已发布生产迁移，逐个比较 `git rev-parse HEAD:<path>` 与 `git hash-object --path=<path> <path>`：

- 格式整理前后均为 32 个文件、0 个 Git 规范化 blob 差异。
- 格式整理前逐个将 `gofmt` 输出与 HEAD 源码比较，除 CRLF→LF 外无内容变化；整理后再次核对 blob 一致性。
- 新 0033 逆映射 `ID0033` / `Up0033` / `Validate0033` / `ValidateRecoverable0033` 与迁移 ID，去掉新增来源注释后，与固定上游 0030 文件全文一致。

仅当前工作树换行由 CRLF 规范为 LF；旧迁移不会产生 Git 源码变化。代理未操作 Git 索引、分支、提交或任务状态，主会话负责暂存四个原冲突路径以及 0030→0033 的删除/新增。

## 临时 SQLite 升级回归

`TestForkMigrationUpgradePreservesPublishedLedgerAndData` 使用 `t.TempDir()` 下的真实 SQLite 文件，分别创建旧 24、27、32 条迁移链，写入合成业务数据，关闭连接，再重新打开并执行完整 33 条链，随后再次关闭、重开和执行：

- 原始账本与固定旧 ID 列表逐项相同；升级后账本等于完整 33 项，旧账本仍为严格前缀，没有重编号或重放旧迁移。
- 比较所有既存列的行快照，包括分组、凭据、访问密钥、请求、尝试、小时用量、迁移账本以及 6 张捐献表；仅旧版输出计时列按原有 0030 合同删除。
- 凭据 ID、密文、fingerprint、identity fingerprint、secret version、人工备注、捐献去重/接收/审核/回执关联和缓存 token 分项均保持原值。
- 32→33 fixture 使用非空凭据别名、非零组优先级、并发限制及客户端 IP，升级与重启均保留。
- 请求体记录列和子表在旧链中不存在，升级后存在；已有访问密钥的记录开关默认为关闭。
- 32→33 首次升级后显式开启记录并写入合成请求体，第二次启动保留开关与请求体内容。
- 新表拒绝非法状态、录入状态空内容、跳过状态有内容、负大小以及孤立请求；删除请求后请求体级联删除。原有请求及业务行继续保留。

## 验证证据

环境：`go version go1.27.3 windows/amd64`；未配置 `GPT_LOAD_DATABASE_TEST_DSN`。

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 ./internal/storage/...` | 通过；storage 22.555s、dbtx 0.717s、migrations 5.345s、models 0.388s |
| `go test -count=1 -v ./internal/storage -run '^Test(ForkMigrationUpgradePreservesPublishedLedgerAndData\|RequestBodyRecordingMigrationContract\|ExternalRequestBodyRecordingMigrationContract\|MigrationValidationLimitsMetadataQueries)$'` | 通过；24→33、27→33、32→33 与 fresh/upgrade/interrupted_column/interrupted_complete 全部通过；外部迁移 opt-in 测试按约定跳过；3.939s |
| SQLite 元数据查询预算 | 初始化 1160 ≤ 1200；重复启动 254 ≤ 400，未放宽门槛 |
| `go vet ./internal/storage/...` | 通过，无输出 |
| `gofmt -l internal/storage`（整理后） | 无输出 |
| `git diff --check HEAD -- internal/storage` | 通过，无空白错误；Git 的 LF/CRLF 提示不影响源码 blob |
| 工作树冲突标记扫描 | `internal/storage/**` 无残留冲突标记 |

测试输出中的检查约束/外键拒绝日志来自预期失败断言，所有对应子测试通过。

## 验证边界与主会话交接

- 本轮存储范围没有出现新增测试失败或 Windows 平台失败，因而不需要为该范围建立失败基线。
- 真实 MySQL/PostgreSQL 新装、升级及 MySQL 隐式提交中断恢复未实测；保留上游方言代码及外部 opt-in 测试，不能以 SQLite 中断回滚测试代替网络数据库恢复证据。
- 本代理仅完成存储层定向验证；完整 Makefile 门禁、其他模块测试及独立 CPA module 由主会话统一执行。
- 建议主会话将数据库规范的当前链尾更新为 0033，并在升级说明中记录请求体记录默认关闭；不调整旧 0001–0032 源码和账本。
