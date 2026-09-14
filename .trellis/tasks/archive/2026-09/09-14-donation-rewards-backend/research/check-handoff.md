# new-api 捐献后端完整复核

日期：2026-09-14。检查角色：独立 `trellis-check`，在真实双服务联调通过后继续检查全部后端改动。代码目录为 `D:/code/Claude code program/new-api`；任务与本报告保存在 gpt-load。没有提交或发布，也没有修改前端、clover 图片或生产配置。

## 结论

**后端复核通过，可进入前端实现。** 未发现需要追加生产修复的权限、资格、恢复或账务问题。本次完整检查没有改动生产源码；此前联调夹具的修复与通过证据保留在父任务 [cross-service-validation.md](../../09-14-key-donation-rewards/research/cross-service-validation.md)。

依据包括本任务 `check.jsonl`、PRD/design/implement，父任务 PRD/design、集成契约、安全控制和 new-api 约定，以及两个仓库的 `AGENTS.md`。目标仓库没有独立 `.trellis/spec/`；没有套用 gpt-load 的测试框架或数据库约定。

## 检查范围与实际代码路径

已逐项读取 `model/donation.go`、`donation_batch.go`、`donation_reward.go`、`donation_secret.go`，`service/donation.go`、`donation_client.go`，`controller/donation.go`、`router/donation-router.go`，authz 注册和测试，以及 main/router/model 迁移装配的完整差异。同步核对两层 donation 测试、既有额度增量/缓存辅助函数和 TestMain。

| 契约 | 复核结果 |
| --- | --- |
| 身份与所有权 | 普通接口统一 `UserAuth`，用户 ID 来自认证上下文；本人批次和 retry 查询附带用户 ID。管理端全部 `AdminAuth`，再分别检查 `donation_config.read/write`、`donation_records.read`。JSON 白名单拒绝收款人、目标组和奖励覆盖，沿用 Authorization-only 鉴权与 no-store。 |
| 活动及目标冻结 | 保存活动前查询真实分组；空组可用、禁用/不可探测组不可用。事务核对活动版本并冻结用户、实例/来源、组/revision 和金额。关闭活动允许在远端离线时完成，已有批次继续原规则。 |
| 秘密与客户端 | 独立随机材料持久化；缺失、损坏或历史 secret_id 不一致（包括 NULL）拒绝继续。私密读写使用 silent GORM 会话。token 以 AES-GCM 保存，API 不回显。客户端不继承 relay 不安全 TLS/环境代理，禁止重定向；仅 loopback 允许 HTTP。 |
| 全站资格与请求幂等 | 规范化 key 的稳定 HMAC 不包含用户/组/活动；批次按用户+请求 UUID 唯一，摘要包括原行号。唯一资源 owner 与 generation 在事务中取得，跨连接竞争依赖数据库约束和行锁；重放内容变化拒绝。 |
| 未知结果及恢复 | 未确认批次持久保存原标识；404、超时、租约过期均不释放 owner。仅确定的 invalid 回执或唯一首次发送的明确受理前拒绝释放。发送次数先持久化，POST 禁止 Transport 隐式重放；转发 JSON 大小在占用资源前检查。 |
| 回执身份与状态 | 校验实例/来源、batch、group、原 revision 和完整 item ID 集合；accepted 必须包含有效 credential/time。仅可信接收结果进入待发奖状态，旧排队/失败观察不能撤销 accepted。重试动作有持久 UUID，丢回执仍按同一动作恢复。 |
| 奖励事务 | 锁资源、明细、唯一流水及账号，检查固定金额/收款人；奖励、`User.Quota` 增量、明细与事件同事务。MySQL RR 的流水重放使用 current read，不用 no-op RowsAffected 推断创建资格。钱包上界沿用 `creditTopUpQuota` 原子条件更新。 |
| 缓存与账号 | 仅首次成功提交后应用一次 `syncCreditUserQuotaCache` 增量；重放不覆盖钱包或预扣缓存。禁用/删除账号的待发奖励暂停，恢复账号后沿用原明细继续。 |
| 永久性与历史 | 没有 Checkin/TopUp 伪订单写入，没有奖励到期、每日/累计业务上限或追回路径。捐献表不带级联到用户/远端凭据/组的关联，删除资源不清理原奖励与资格。主库业务事件不依赖可清理日志。 |
| 装配及兼容 | 正常主库迁移接入 `MigrateDonations`，日志库 schema 未改变；main 启动持久恢复 worker。`HTTP_LISTEN_HOST` 缺省经 `net.JoinHostPort` 仍为原 `:port`，真实进程已核对只绑定 loopback 的配置行为。 |

## Findings (fixed)

完整后端复核阶段没有追加生产修复。实施阶段已修复的 MySQL 插入/当前读语义、不可用组空 revision、TLS Transport、POST 隐式重发、转发报文边界和秘密 NULL 检查，均纳入上述源码复核及相应测试证据。

既有 `service/text_quota_test.go` 的单行夹具修复已独立确认：

- 用 Go `-overlay` 创建临时旧夹具，只撤掉 `AutoMigrate` 的 `&model.Checkin{}`，共享源码保持原样。
- 单独运行 `go test -overlay 'C:/Users/lucky0401/AppData/Local/Temp/donation-backend-check-04ed73dd06eb4c9397dd88824479e018/original-fixture-overlay.json' ./service -run '^TestFixedPriceBillingDatabaseMatrix$/^sqlite$' -count=1 -v`，没有运行任何 donation 测试。
- 结果为预期的失败（exit 1，0.329s）：13 个 SQLite 子用例出现 `no such table: checkins` / `query_data_error`，调用来自既有 `model/temporary_quota.go:98`。
- 固定价格测试会自行切换到另一份新数据库；TestMain 的 Checkin 表不能替代其夹具迁移。因此补这一张表是原有夹具修复，不是捐献全局状态污染，也没有改变生产计费。
- 当前单行修复保留时，同测试通过。donation service fixture 显式恢复 DB、LOG_DB、RedisEnabled 和数据库类型；未修改签到配置。

本次独立旧夹具及结果位于临时目录 `C:/Users/lucky0401/AppData/Local/Temp/donation-backend-check-04ed73dd06eb4c9397dd88824479e018/`（`original-fixture-overlay.json`、`original-fixed-price.log`），不会进入提交。

## Findings (not fixed)

没有未修复的审查发现。授权范围是本地代码和验证；本报告不代表生产部署或全站安全认证。

原始 key 尚未被远端确认且用户已丢失输入时，只能保留未确认元数据并继续查询，不能从 HMAC 还原 key；该行为符合既定契约。前端须区分 reception/state/reward_state，保留原请求标识用于重放，并避免持久保存 key 文本。

## Verification

所有下列 Go 命令的工作目录均为 new-api 根，使用 `GOFLAGS=-p=2`、`GOMAXPROCS=2`，工具链 `go1.25.5 windows/amd64`。

### 本检查角色执行

- **Lint: pass**：全部 17 个新增/修改 Go 文件 `gofmt -l` 无输出，`git diff --check` 无输出；`go vet ./model ./controller ./middleware ./service/... ./router .` 退出码 0。
- **TypeCheck: pass**：目标程序已实际构建；最终定向测试同时编译上述包与 main，没有绕过类型检查。
- **Tests: pass**：`go test ./model ./service ./service/authz ./controller ./router ./middleware . -run '^(TestDonation|TestFixedPriceBillingDatabaseMatrix)' -count=1 -v` 退出码 0。model 0.998s、service 1.152s、authz 0.225s、controller 0.311s；router/middleware/main 完成编译。此轮没有提供网络数据库 DSN 或集成 binary，相关 opt-in 用例明确 skip，实际执行证据分别见下方矩阵和双进程联调，未将 skip 当作通过。
- **真实双进程联调: pass**：最终 `TestDonationIntegrationRealServices` 47.45s，package 47.696s。真实 gpt-load 与 new-api、独立临时 SQLite、真实管理/注册/登录/捐献处理器及 worker，本地假 Gemini 上游。覆盖混合多行、跨用户/活动/组争用、429 重试、真实回执丢失、两个进程中断重启、禁用暂停/冻结金额、凭据与组删除后历史、Checkin 零写入、日志/审计脱敏及 loopback 绑定。

独立定向测试的完整输出保存在上述临时目录 `review-scoped-tests.log`。二进制路径、哈希和完整运行方法沿用父任务联调记录；后端复核没有改动该 47.45 秒通过版本的生产源码。

### 已读取并核对的实施方最终验证

| 证据 | 真实引擎及结果 |
| --- | --- |
| [database-final-default.log](database-final-default.log) | SQLite 3.50.4、MySQL 8.4.11（clientFoundRows=false）、PostgreSQL 17.10；最终 `^TestDonation` 全通过，model 6.264s。 |
| [database-modern.log](database-modern.log) | SQLite 3.50.4、MySQL 8.4.11（clientFoundRows=true）、PostgreSQL 17.10；真实数据库矩阵通过。 |
| [database-minimum-final.log](database-minimum-final.log) | SQLite 3.50.4、MySQL 5.7.44（clientFoundRows=true）、PostgreSQL 9.6.24；最新并发秘密初始化及数据库矩阵通过，model 4.814s。 |
| [backend-final-test.log](backend-final-test.log) | `go test ./model ./controller ./middleware ./service/... ./router . -count=1` 全通过：model 10.923s、controller 69.342s、middleware 0.564s、service 1.604s、authz 0.230s、router 0.472s。 |

矩阵代码实际覆盖新库、代表性既有 User/TopUp/Checkin 数据升级、重复迁移无 DDL、既有唯一约束、两个独立连接的资源/奖励竞争、事务中断回滚、暂停/钱包边界，以及预扣缓存的单次增量。数据库夹具仅操作自己的 `dv_<UUID>_` 前缀表；本检查没有另行操作这些数据库。

共享可执行契约已写在父任务集成/安全控制和后端 implementation-handoff 中，本次无需修改 gpt-load 的项目规范。后续前端与部署验收由父任务继续推进。
