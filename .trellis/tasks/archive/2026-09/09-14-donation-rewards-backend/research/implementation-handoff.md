# new-api 捐献后端实现交接

日期：2026-09-14。实现目录 `D:/code/Claude code program/new-api`，本地分支 `feat/key-donation-rewards`，未提交、未发布、未操作生产。本文件由 backend implementer 维护；父任务状态及前端由主会话维护。

## 实现边界

- 新增 `model/donation*.go`：主库活动/修订、连接/独立秘密、批次/明细、全局资源资格、唯一奖励、恢复动作及状态事件。`model/main.go` 的正常迁移调用 `MigrateDonations`；不迁移日志库，不改变既有 User/Checkin/TopUp 模型。
- 新增 `service/donation.go`、`donation_client.go`：受限 gpt-load 客户端、连接身份核对、活动目标检查、原标识恢复与 worker。
- 新增 `controller/donation.go`、`router/donation-router.go`、authz 捐献资源注册；原有 `router/api-router.go` 接入。所有本人接口复用 UserAuth，所有管理接口同时要求 AdminAuth 和对应权限；JSON 字段白名单拒绝客户端指定收款人、目标组或金额。
- `main.go` 启动恢复器；新增可选 `HTTP_LISTEN_HOST`，通过 `net.JoinHostPort` 装配监听地址。未设置时仍为原来的 `:port`；本地验收设置 `127.0.0.1`。
- 前端、gpt-load 业务代码、现有 clover 图片均不属于本子任务修改范围。独立联调代理拥有 `controller/donation_integration_test.go`。

## 秘密、连接与权限

`donation_secrets` 的唯一 `v1` 槽保存 64 字节独立随机材料与校验值；前 32 字节用于 HMAC，后 32 字节用于 AES-GCM 保护集成 token。创建竞争采用普通 INSERT，唯一冲突退出事务后读取真实已存材料，不依赖 MySQL RowsAffected。秘密材料/密文读写使用 silent GORM session，包含 DEBUG 模式。

材料不依赖会话 CryptoSecret、URL、活动或可轮换 token。连接、批次和资源保存材料 ID；密钥缺失、损坏、历史材料不一致（包括 NULL）均 fail closed。有任何历史记录时不自动生成新秘密。该独立内部表沿用本仓库持久秘密模式，备份必须同时包含秘密、连接和业务表。

连接读取只返回 `base_url/configured/instance_id/source_id/version/updated_at_ms`，不回显 token。初次保存通过 capabilities 验证并永久固定 instance/source UUID；更新 URL/token 仍必须匹配，不静默改绑。

客户端仅允许 HTTPS；本地开发允许 loopback HTTP。URL 不得带用户信息、查询串、片段或额外路径。禁止重定向，不继承 relay 的 TLS_INSECURE_SKIP_VERIFY 或环境代理，使用系统 TLS 信任根。带 body 的 POST 清空 `Request.GetBody`，防止 Go Transport 对 Idempotency-Key 请求隐式重发；所有发送次数由持久状态记录。

管理权限：

- `donation_config.read`：连接状态、分组目录、活动列表。
- `donation_config.write`：连接及活动写入。
- `donation_records.read`：全站记录及详情。

三个权限默认授予 admin，并沿用 root/显式用户覆盖规则。普通用户即使知道管理路由，也不能绕过 AdminAuth。

OWASP 依据沿用父 `security-controls.md`（ASVS 5.0.0，8.2.1–8.2.3、8.3.1–8.3.2、3.5.1、1.2.4、1.4.2、15.4.2、13.3.1、14.3.3、16.2.5）。已读取 Authentication、Session Management、REST Security 指南；验证包含 Authorization-only 既有认证、JSON 输入、字段白名单、权限隔离、无秘密回显、TLS/redirect、幂等与原子额度。没有变更既有登录流程，也不作全站安全认证声明。

## 资格、接收与永久入账

1. LF/CRLF 逐行解析，忽略空行、trim 首尾空白，保留大小写和 1 起始原行号。100 项、4096 字节/key、1 MiB/body 仅为请求工程边界。非法项和批内重复独立记录；短 key 完全掩码，长 key 最多显示末 4 个字符。
2. HMAC 输入仅由固定版本域及规范化 key 决定，不包含用户、活动、组或 URL。同一事务冻结活动、用户快照、目标和每 key 整数奖励，并确定资源唯一 owner。资源锁按指纹排序。只把 owner 项送往 gpt-load；其他项返回 duplicate，避免两个系统选出不同的获奖者。
3. 在取得 owner 前用实际 `common.Marshal` 预检候选转发报文大小，计入 `<>&` 的 JSON 转义膨胀；本地超限不留下未知远端占用。
4. gpt-load 的成功包装必须为数值 `code:0`，不是 new-api 的 `success`。capabilities 版本为字符串 `"1"`，固定 instance/source/input_types/limits。不可用分组允许空 target_revision，保留不可选原因。
5. new-api 不保存明文 key。初次请求可能返回本地元数据且 `reception_state=unconfirmed`，此时尚未确认可靠接收。先查询原远端 batch；同内容原请求可继续传输，不能创建新获奖身份。后台只有元数据时只对账，不伪造已接收。
6. 404、超时、租约过期都不释放 owner。仅原明细可信的终态 invalid，或唯一首次发送得到明确的目标拒绝，才能释放尚未成功的占用；已有任何未知/并发发送时，后来的拒绝不足以释放。
7. 回执逐项匹配 ID，不靠数组顺序。实例、来源、批次、组、原 revision、完整 item 集合，以及 accepted 的非零 credential/time 都须吻合。保存 accepted 事实后才可结算；旧 queued/invalid 观察不能撤销既有 accepted。
8. `CreditDonationReward` 在一个事务内锁资源/明细/用户，验证唯一资源和明细、原用户、固定金额、账号状态，写唯一奖励记录、调用 `creditTopUpQuota` 增加永久 User.Quota、标记奖励及事件。金额为现有整数额度，范围 `1..common.MaxWalletQuota`，钱包边界仍用原子安全增额路径。
9. 奖励流水读取使用 `lockForUpdate`，包括重放查询，避免 MySQL REPEATABLE READ 等待锁前的旧快照看不到刚提交的流水。INSERT 冲突先完整回滚，重试在新事务进行。
10. 只在实际首次成功提交后调用 `syncCreditUserQuotaCache`，保留预扣差额。已提交重放不重复加缓存、不覆盖钱包。普通日志为提交后派生审计，主库奖励与事件为长期依据。

不写 Checkin、不伪造现金 TopUp，不设每日/累计领奖数量限制，不设到期、不实现追扣。活动关闭拒绝新批次，已受理项沿用冻结规则；账号禁用将未发奖励置 paused，恢复后原 owner 继续入账。凭据、分组、用户改名/删除及日志清理不删除业务历史。

## 恢复行为

worker 默认每 3 秒扫描，单轮最多 32 个批次，持久租约 120 秒，单批操作上下文 45 秒，查询退避最高 60 秒。租约只调度恢复，不决定资源资格。accepted 的待奖励项可直接依据持久回执结算，不依赖远端仍存在或活动仍开启。

用户 retry 先对账，再持久保存新的动作 UUID 和所选 item IDs。请求失败时重放同一动作；gpt-load 负责从已保存密文重排可重试项，不重探测 accepted/committing。普通 GET 仅返回保存的状态，不发起写动作。

unconfirmed 批次的响应包含非秘密 `request_key`（原浏览器 Idempotency-Key UUID），刷新后可用于原内容重放。前端可保存在请求状态中、自动带回，不需要向用户展示技术标识；不得持久保存 key 文本。若服务器从未确认暂存且原文本已丢失，不能声称可凭元数据自动补发。

## 前端 API

所有接口响应为 new-api `{success,message,data}`；错误含稳定 `code`。变更接口要求 `Content-Type: application/json`，批次提交与 retry 的 Idempotency-Key 使用规范小写 UUID v4。

| 方法和路径 | 输入/返回 data |
| --- | --- |
| GET `/api/donations/campaigns` | 数组 `{id,name,description,reward_quota,enabled,available,unavailable_reason}` |
| POST `/api/donations/batches` | `{campaign_id:number,keys_text:string}` → BatchDetail |
| GET `/api/donations/batches?p=1&page_size=20` | `{page,page_size,total,items:BatchDetail[]}`，仅本人；不接受 user_id 替换查询身份 |
| GET `/api/donations/batches/:id` | 本人 BatchDetail |
| POST `/api/donations/batches/:id/retry` | `{}` 或 `{item_ids:string[]}`，新动作幂等键 → 本人 BatchDetail |
| GET/PUT `/api/donations/admin/connection` | PUT `{base_url,token?}`；token 省略/空串时保留原值；返回 ConnectionView |
| GET `/api/donations/admin/group-options` | `[{id,name,channel_id,connection_type,enabled,can_probe,target_revision,unavailable_reason}]`；错误与空列表区分 |
| GET/POST `/api/donations/admin/campaigns` | POST `{name,description?,group_id,reward_quota,enabled?}`；enabled 缺省 false；返回活动 |
| PATCH `/api/donations/admin/campaigns/:id` | 上述字段的部分更新，新增规则版本；仅关闭时不依赖目标连通 |
| GET `/api/donations/admin/records` | 分页 `{page,page_size,total,items:Record[]}`；过滤 `user_id/campaign_id/group_id/state/reward_state/from_at_ms/to_at_ms/item_id/credential_id` |
| GET `/api/donations/admin/records/:item_id` | Record，额外含 events |

BatchDetail 示例（掩码、UUID、数值均为合成说明）：

```json
{
  "id": "a533a45d-e637-4f9b-8258-b71d5d31c160",
  "request_key": "1036f0b9-b2a3-44d1-aafd-319f8fd38ea1",
  "user_id": 2,
  "username": "donor",
  "linux_do_id": "community-1",
  "campaign_id": 1,
  "campaign_version": 1,
  "campaign_name": "Community keys",
  "instance_id": "060b5f96-cd0b-47b9-b08b-0466821f65da",
  "group_id": 1,
  "group_name": "Gemini",
  "reward_quota": 25,
  "reception_state": "confirmed",
  "last_error": "",
  "created_at_ms": 1789300000000,
  "updated_at_ms": 1789300001000,
  "items": [{
    "id": "307c88ac-6247-4594-aa6b-fa3856773a49",
    "batch_id": "a533a45d-e637-4f9b-8258-b71d5d31c160",
    "line": 2,
    "key_mask": "••••0001",
    "state": "accepted",
    "reason_code": "",
    "retryable": false,
    "credential_id": 100,
    "accepted_at_ms": 1789300000000,
    "reward_state": "rewarded",
    "reward_reason": "",
    "rewarded_quota": 25,
    "rewarded_at_ms": 1789300001000,
    "created_at_ms": 1789300000000,
    "updated_at_ms": 1789300001000
  }],
  "summary": {"total":1,"accepted":1,"invalid":0,"duplicate":0,"processing":0,"rewarded":1,"rewarded_quota":25}
}
```

`reception_state` 为 `local_only`（全部本地无效/重复/确定拒绝）、`unconfirmed`、`confirmed`。逐项 `state` 为 `unconfirmed/queued/validating/committing/accepted/retry_pending/invalid/existing/duplicate`；`reward_state` 为 `none/pending/paused/rewarded`。成功接收与奖励到账必须分别显示。只有 accepted 返回非空 credential_id/accepted_at_ms；只有 rewarded 返回非空 rewarded_at_ms。

本地原因：`invalid_format/duplicate_item/already_donated/resource_processing`。远端原因限定为 `invalid_credential/model_unavailable/rate_limited/timeout/upstream_error/probe_incompatible/unknown/retry_exhausted/resource_busy/runtime_pending/target_changed/group_disabled/group_deleted/unsupported_input/target_unavailable/probe_unavailable/credential_override/staging_expired/already_exists`。奖励原因包括 `account_disabled/wallet_limit/reward_pending`；批次 last_error 可能为 `connection_unavailable/receipt_not_found/integration_unauthorized/target_changed/target_unavailable/invalid_receipt/instance_mismatch/protocol_mismatch/recovery_pending`。

Record 为 `{item:DonationItem,batch:DonationBatch,reward:null|{id,item_id,user_id,quota,credited_at_ms},events?:[{id,item_id,state,reason_code,created_at_ms}]}`。列表不带 events；详情包含状态变迁、reward_paused 和 rewarded 事件。Fingerprint、秘密 ID、资源 owner、租约及明文 key 不返回。

主要错误码：`DONATION_INVALID_REQUEST`(400)、`DONATION_NOT_FOUND`(404)、`DONATION_CONFLICT`(409)、`DONATION_UNAVAILABLE`(409)、`DONATION_ACCOUNT_DISABLED`(403)、`DONATION_IDENTITY_UNAVAILABLE`(503)、`DONATION_INVALID_RECEIPT`(502)，以及封闭集成原因形成的 `DONATION_*`(502)。既有认证中间件保留其原认证错误码。

## 验证记录

所有 Go 命令工作目录为 new-api 根，进程环境 `GOFLAGS=-p=2`、`GOMAXPROCS=2`；不改变全局配置。

- `go test ./model -run '^TestDonation' -count=1 -v`：通过，日志 `model-sqlite-test.log`。SQLite 版本为 Go 实际使用的 `SELECT sqlite_version()` = **3.50.4**。
- `go test ./service -run '^TestDonation' -count=1 -v`：通过，日志 `service-test.log`。覆盖丢回执/重启服务对象、暂停/冻结、原动作重放、假 404 后目标拒绝、JSON 转义超限、不同 key 无数量门禁、短掩码、身份误绑、包装差异及真实 TCP hijack 断开不隐式重发。
- `go test ./model -run '^TestDonationDatabase$' -count=1 -v`：真实 SQLite **3.50.4**、MySQL **8.4.11 / 5.7.44**、PostgreSQL **17.10 / 9.6.24** 全部通过。现代及最低版本日志分别为 `database-modern.log`、`database-minimum.log`；MySQL 两次明确设置 `clientFoundRows=true`。所有矩阵同时覆盖新表创建、已有 User/TopUp/Checkin 数据升级、两次重复迁移无 DDL、唯一性保留、独立连接并发 owner/credit、事务中断回滚、金额/收款人冲突、账号暂停、钱包边界、无签到写入及原 owner 不因 404/租约释放。只创建/清理 `dv_<UUID>_` 前缀测试表。
- 最终源码再次执行完整 `go test ./model ./controller ./middleware ./service/... ./router . -count=1` 已全部通过（`backend-final-test.log`：model 10.923s、controller 69.342s、middleware 0.564s、service 1.604s、authz 0.230s、router 0.472s、main/passkey 无测试但构建通过）。此前失败的两个 fixture 已修复；独立 `go test ./service/... -count=1` 也全部通过（`service-final-test.log`）。authz 既有完整权限快照补充新增权限并验证独立 deny；既有 fixed-price 测试缺 Checkin 表在独立运行中同样失败（`fixed-price-fixture-before.log`），只补其 fixture AutoMigrate，没有改变生产计费。
- 真实两个服务进程 + 本地假 Gemini HTTP 已由独立代理跑通；覆盖内容及最终二进制证据见父 `research/cross-service-validation.md`。本文件不把单服务 fake 代替跨端验收。
- 最后 `go vet ./model ./controller ./middleware ./service/... ./router .` 通过（`backend-vet.log` 为空，exit 0）；所有修改/新增 Go 的 `gofmt -l` 输出为空，`git diff --check` 通过。
- 最新源码补验并发秘密初始化与普通 MySQL 语义：`database-final-default.log`，MySQL 8.4.11 的 `clientFoundRows=false`、PostgreSQL 17.10、SQLite 3.50.4，`go test ./model -run '^TestDonation' -count=1 -v` 全部通过（6.264s）。`database-minimum-final.log` 再次验证 MySQL 5.7.44 的 `clientFoundRows=true`、PostgreSQL 9.6.24、SQLite 3.50.4，最新并发秘密初始化及完整新建/升级/账务套件全部通过（4.814s）。
- 独立代理针对最后生产源码重建后的真实双进程验收已通过（47.45s）；new-api binary SHA256 `A47718179B71D72E6A20360D787CC211FF0DD8120051EA310DD753F3FEF3FD7E`，具体运行环境和日志路径见父验收文档。

矩阵使用父任务已提供的本地端口：MySQL 23306/23307、PostgreSQL 25432/25433，目标数据库 `donation_newapi`；SQLite 为每个测试的独立临时文件。MySQL DSN 设置 `charset=utf8mb4&parseTime=true&clientFoundRows=true`，PostgreSQL 设置 `sslmode=disable`（仅这些 loopback 测试数据库）。

## 已知边界

- 初始暂存未确认且用户已丢失原 key 时，只能显示未确认元数据并继续对账，不能凭 HMAC 重新构造 key。原请求键保留用于拥有原内容时恢复。
- 日志与 Redis 是提交后派生状态，跨库不宣称天然原子；进程在主库提交后退出时，重放不会额外套用缓存增量，缓存按既有 TTL/水合语义恢复。永久奖励流水不受缓存或日志清理影响。
- 不查询 Google 项目身份、不证明不同 key 拥有独立上游配额，遵守父 PRD 已明确的按 key 资格边界。生产地址、凭据和分组仍需另行授权配置。
