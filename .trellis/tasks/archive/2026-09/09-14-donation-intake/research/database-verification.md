# 捐献接收真实数据库验证

验证日期：2026-09-14。范围为 gpt-load 接收端的新数据库路径；本记录不代替父任务跨端验收。

## 环境与隔离

- MySQL：`8.4.11`，专用临时容器 `donation-test-mysql84-20260914`，本地端口 `23306`。
- PostgreSQL：`17.10`，Alpine 构建，专用临时容器 `donation-test-pg17-20260914`，本地端口 `25432`。
- DSN 指向仅用于本任务的 `donation_gptload`。每个顶层测试再创建唯一的 MySQL database 或 PostgreSQL schema，所有迁移、库存和历史操作均在该隔离范围执行，结束后自动删除该范围。
- 仅使用合成 key 和本地探测替身，没有真实上游请求、线上数据库或真实 API 凭据。数据库连接、DDL、约束、事务、锁、回滚和独立连接查询均由真实服务器执行。
- 测试文件：`internal/control/donation_database_integration_test.go`。仅缺少 `GPT_LOAD_DATABASE_TEST_DSN` 时跳过；设置后任何数据库或断言失败均使测试失败。命名遵循 `^TestExternal`，现有数据库 CI 脚本会自动包含。

## 覆盖的行为

| 用例 | 实际验证 |
| --- | --- |
| `TestExternalDatabaseDonationMigrationBackfillsInventory` | 全新迁移、重复迁移、从仅移除增量 0015 的隔离库重建带库存的 0014 升级起点、再次迁移、显式执行 `EnsureInitialState` 回填 |
| 同上 | 两组中的三条旧凭据保留原 ID、密文、指纹和配置；共享 key 只生成一条长期资格；重复启动及新连接重开保持 instance/source 和库存 |
| `receipt_recovery_and_retained_history` | 已清空凭据的有效组可接收；加密暂存；探测执行时新 key 尚未进入库存或运行时；仅被提交 key 传给探测替身 |
| 同上 | 数据库提交后模拟运行时发布失败，外部仍只见 committing；新连接恢复同一凭据/时间，不再次探测；跨组重复、原有库存、删除凭据/组和压缩操作后重放保留回执/资格 |
| `concurrent_batch_replay_and_item_conflict_rollback` | 两个独立连接的事务被同步到都查无原批次，再并发插入；两方获得同一请求结果，数据库只有一份批次/明细；跨批次重用明细 ID 使新批次事务回滚 |
| `retry_action_replay_preserves_consumed_budget` | 耗尽自动探测后通过原暂存发起重试；另一连接重放相同动作不重置之后消耗的预算或状态，修改动作内容返回冲突；原暂存可继续接收 |
| `independent_connections_fence_resource_ownership` | 两个独立服务/连接同时处理相同 key 的跨组请求；只允许一个探测，另一项持久化 resource_busy；最终仅一条凭据/accepted，竞争项为 existing |
| `credential_and_receipt_transaction_rollback` | 在真实凭据和资源更新之后、回执更新之前注入故障；独立连接确认凭据/永久资格均回滚且暂存保留，再从原明细恢复一次接收 |
| `management_import_preempts_reserved_donation` | 探测进行时另一连接经正常管理入口导入同 key；捐献结果为 existing，来源为 inventory，无错误 accepted 回执 |

## 执行命令

以下密码是本地一次性测试容器的合成值。两个引擎顺序执行。

最终复验在当前 PowerShell 进程限制 Go 构建并行度，避免 Windows 构建资源耗尽：

```powershell
$env:GOFLAGS = '-p=2'
$env:GOMAXPROCS = '2'
```

```powershell
$env:GPT_LOAD_DATABASE_TEST_DSN = 'mysql://root:donation-local-only@127.0.0.1:23306/donation_gptload?charset=utf8mb4&transaction_isolation=%27READ-COMMITTED%27'
go test -count=1 ./internal/control -run '^TestExternalDatabaseDonation' -v -timeout 5m
```

```powershell
$env:GPT_LOAD_DATABASE_TEST_DSN = 'postgres://postgres:donation-local-only@127.0.0.1:25432/donation_gptload?sslmode=disable'
go test -count=1 ./internal/control -run '^TestExternalDatabaseDonation' -v -timeout 5m
```

MySQL 默认隔离级别额外验证：

```powershell
$env:GPT_LOAD_DATABASE_TEST_DSN = 'mysql://root:donation-local-only@127.0.0.1:23306/donation_gptload?charset=utf8mb4'
go test -count=1 ./internal/control -run '^TestExternalDatabaseDonationIntakeTransactions$/(concurrent_batch_replay_and_item_conflict_rollback|retry_action_replay_preserves_consumed_budget)$' -v -timeout 5m
```

静态检查：全部改动 Go 文件的 `gofmt -l`、`git diff --check` 无输出；`go vet ./internal/control ./internal/platform/config ./internal/platform/httproute ./internal/storage/...` 通过。普通无 DSN 执行也验证新增文件可编译，并按 opt-in 契约跳过数据库连接。

## 已确认问题与复验

- MySQL 首轮新用例在 `EnsureInitialState → backfillDonationInventory` 复现 `ERROR 1064`：`JOIN groups` 未引用 MySQL 8.4 的保留字。新表迁移成功不能排除此启动失败；带库存和空库存两种 bootstrap 均失败。已交主代理修复为 GORM 子查询，保留 API-key 组过滤与凭据 ID 顺序。
- MySQL 并发批次测试另复现：同标识同内容请求错误返回 `Idempotency-Key was already used for another request`。storage 强制 `clientFoundRows=true`，`ON DUPLICATE KEY` 无变化时仍可返回 `RowsAffected=1`，旧代码据此误判为新增并再次插入明细。实施代理已改为普通 INSERT、唯一冲突后完整回滚、新读核对已提交摘要；重试动作采用相同处理，补充动作预算回归。
- PostgreSQL 首轮迁移/库存回填用例通过。事务用例的初始空组 fixture 不符合已有管理创建规则，已修正为创建合成种子并调用正式删除入口得到空组；没有调整生产创建规则。

最终源码（包含控制操作恢复屏障和首次缺失查询修复）的结果：

| 数据库 / 隔离级别 | 实际范围 | 结果 |
| --- | --- | --- |
| MySQL 8.4.11 / READ-COMMITTED | 两个顶层用例，含全部六个事务子场景 | PASS，22.851s |
| PostgreSQL 17.10 / read committed | 两个顶层用例，含全部六个事务子场景 | PASS，17.348s |
| MySQL 8.4.11 / REPEATABLE-READ | 并发批次重放、重试动作预算两项 | PASS，7.785s |

隔离级别由测试连接查询服务器并打印，并非仅依据 DSN 推断。预期唯一冲突在日志中会出现 duplicate-key 信息；对应断言和测试退出状态均通过。

收尾分别查询 MySQL `information_schema.SCHEMATA` 和 PostgreSQL `pg_namespace`，`gpt_load_donation_%` 临时范围计数均为 0，确认测试清理完成。

## 完整接收端复核补充

- 新增 `TestDonationWaitsForControlOperationRecovery`，通过正式 `CreateGroupIdempotent` 制造尚未恢复的管理操作。修复前，凭据提交和 committing 后运行时发布两个场景均越过恢复屏障、错误返回成功；现于两处复用 `enforceOperationRecoveryBarrierLocked`，旧操作恢复后再完成接收，两项回归通过。
- 首次尚未创建的 identity/batch 查询改为 `Limit(1).Find`，避免把预期缺失写成数据库错误。已有启动日志回归通过。
- 目录测试按带 Data 的 `APIError.Code` 断言；补齐 `internal/storage/db_test.go` 的 0015 迁移期望。
- `go test -count=1 ./internal/control ./internal/platform/config ./internal/platform/httproute ./internal/storage/...` 全部通过：control 16.952s、config 0.706s、httproute 0.142s、storage 6.628s，storage 的 dbtx/migrations/models 子包也通过。包含生产 Gemini HTTP executor 的本地 wire 用例、租约代次、混合条目、鉴权/来源隔离、重试和删除后历史测试。
- 已对照 PRD/设计/集成契约复核路由、container、配置、三语 README/i18n、暂存/回执模型和迁移，并同步 backend 的捐献与分层规范。当前没有未修复的已确认接收端实现问题。

## 验证边界

本组测试证明数据库语义及探测调用的输入绑定，不独立证明生产 SDK/网络协议。真实本地 Gemini HTTP wire 由实现代理的 `donation_probe_integration_test.go` 验证；鉴权、自动重试和其他 SQLite 契约由对应 hermetic 用例验证。`make check` 和父任务两仓库最终联调由主会话汇总。
