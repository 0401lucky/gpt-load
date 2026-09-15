# Research: new-api 人工审核、永久奖励与恢复设计

- Query: 在活动可跳过自动模型测试、必须人工审核、真实调用只供管理员使用的前提下，核对 new-api 的最小可靠跨端契约、兼容迁移和验证范围。
- Scope: internal；只读取 sibling `../new-api` 与本任务资料；与接收端研究者同步接口建议。
- Date: 2026-09-15
- Status: 规划研究；以下新增模型/接口为推荐设计，未实现、未执行迁移或真实上游调用。

## Findings

### 1. 已有代码与规范锚点

| 文件 | 职责与关键锚点 |
| --- | --- |
| `../new-api/AGENTS.md:49` | 权限/认证相关改动遵守适用的 OWASP 指引；日志不得包含可用凭据。 |
| `../new-api/AGENTS.md:92` | SQLite、MySQL >= 5.7.8、PostgreSQL >= 9.6；涉及数据库行为必须真实三库验证，新建、代表性旧库升级及重复启动。 |
| `../new-api/AGENTS.md:109` | 避免业务默认值使用 `gorm:"default:true"`，不能未经三库验证换成 `default:1`。 |
| `../new-api/AGENTS.md:134` | 使用现有有意义测试，新增/重写 Go 测试使用 testify；不要按跨层文件数量散布重复测试。 |
| `../new-api/model/donation.go:34` | Campaign、CampaignRevision；目前没有审核模式字段。 |
| `../new-api/model/donation.go:58` | Batch 冻结活动版本、目标、用户、固定奖励；已有恢复标志、退避与租约。 |
| `../new-api/model/donation.go:87` | Item 分离接收与奖励状态；Resource 持有 fingerprint、owner、generation 与永久 acquired 事实。 |
| `../new-api/model/donation.go:119` | Reward 对 fingerprint、item ID 分别唯一；Retry 以用户与请求键唯一。 |
| `../new-api/model/donation.go:147` | 捐献表统一 `MigrateDonations → AutoMigrate`；`model/main.go:389` 在主库启动路径调用。 |
| `../new-api/model/donation.go:169` | 事务重试处理唯一争用、死锁/序列化失败和 SQLite busy；先退出失败事务再重读。 |
| `../new-api/model/donation_batch.go:25` | 原请求摘要只含 campaign ID、行号与完整 key HMAC，域为 `new-api/donation-request/v1`。 |
| `../new-api/model/donation_batch.go:136` | PrepareBatch 在事务中重新读取活动、冻结规则并按指纹顺序取得唯一 owner；new-api 不存捐献 key 明文。 |
| `../new-api/model/donation_batch.go:343` | 只有唯一首次发送的明确未暂存拒绝可以释放 owner；多次/未知发送结果不能用后来的拒绝证明首次未接收。 |
| `../new-api/model/donation_batch.go:378` | PrepareRetry 持久动作与唤醒批次；现校验只确认归属/dispatch，没有 manual 状态限制。 |
| `../new-api/model/donation_batch.go:446` | ClaimRecovery 最多 32 条，按 next_poll/id 排序，120 秒租约；ReleaseRecovery 最高 60 秒退避。 |
| `../new-api/model/donation_reward.go:51` | ApplyReceipt 验证实例/来源、batch/group/revision、完整派发项集合、accepted 凭据/时间；当前白名单不接受待审/拒绝。 |
| `../new-api/model/donation_reward.go:112` | accepted 不被旧观察撤销，invalid/existing 不可改成其他终态；其余中间态尚无单调版本防回退。 |
| `../new-api/model/donation_reward.go:159` | 唯一发奖边界；资源、item、流水 current read、用户状态、钱包额度、事务与缓存增量均已有保护。 |
| `../new-api/model/donation_reward.go:254` | SettleBatch 先尝试发奖，再在锁住 batch 的事务中计算 waiting/retries 与 needs_recovery。 |
| `../new-api/model/donation_secret.go:43` | 独立持久秘密；新增业务表后也须加入“有历史时不能重新生成身份”的检查范围。 |
| `../new-api/service/donation.go:67` | checkedClient 固定连接与 batch 的 instance/source，且每次核对 capabilities。 |
| `../new-api/service/donation.go:102` | availableDonationGroup 强制 enabled、can_probe、api_key；SaveCampaign、Campaigns、Submit 均使用它。 |
| `../new-api/service/donation.go:181` | Submit 优先寻找旧 request；未知接收先 GET，再用原内容和原 batch/item ID 恢复 POST。 |
| `../new-api/service/donation.go:316` | ReconcileBatch 先本地结算，GET 对账，再重放持久 Retry；目前用 Summary.Processing 决定是否提前返回。 |
| `../new-api/service/donation.go:372` | 每条最多 45 秒、当前批次串行恢复；StartDonationRecovery 每 3 秒一轮。 |
| `../new-api/service/donation_client.go:71` | 专用 HTTPS/loopback、系统 TLS 校验、无代理/重定向的受限客户端；普通 JSON 请求总超时 12 秒。 |
| `../new-api/service/donation_client.go:118` | 专用 `{code:0,data}` 包装；POST 清空 GetBody 防 Go 透明重发；错误静态分类。 |
| `../new-api/service/donation_client.go:191` | v1 capabilities 精确校验原限额，包括 retention=604800 秒。 |
| `../new-api/common/json.go:28` | common.Unmarshal 最终使用 encoding/json.Unmarshal，没有 DisallowUnknownFields，响应新增字段会被旧 client 忽略。 |
| `../new-api/router/donation-router.go:10` | 普通用户自身记录；admin 组同时使用 AdminAuth 与独立配置/记录权限。 |
| `../new-api/service/authz/resources_donation.go:3` | 目前只有 donation_config.read/write 与 donation_records.read，无审核/测试写权限。 |
| `../new-api/controller/donation.go:54` | JSON 类型、大小、字段白名单、拒绝显式 null；actor 取认证上下文。 |
| `../new-api/controller/donation.go:251` | PATCH 用指针字段，未给字段保留原值；是新模式字段的兼容入口。 |
| `../new-api/middleware/auth.go:83` | 校验启用账号和管理员等级、设置用户上下文、管理写审计；`:203` 的凭据分类只读 Authorization。 |
| `../new-api/middleware/audit.go:194` | 通用审计从 HTTP/JSON 推断成功，SSE 的 HTTP 200 不能作为模型调用成功证据。 |
| `../new-api/controller/playground.go:29` | Playground 构建站内 token 后调用普通 Relay，不能作为捐献单 key 测试后端。 |
| `../new-api/web/src/features/donations/api.ts:133` | 原会话绑定、认证刷新前后再检查、取消、无自动业务重试，错误不保留带 key 的 Axios 对象。 |
| `../new-api/web/src/features/donations/api.ts:297` | donationBatchIsProcessing 用 reception/summary/reward 状态控制用户页面轮询。 |
| `../new-api/web/src/features/donations/components/record-detail.tsx:43` | 当前管理详情只读，没有轮询/审核动作；查询按 session/item 隔离。 |
| `../new-api/web/src/features/playground/hooks/use-stream-request.ts:62` | 流式控制器有 request generation、stop/dispose；可参考交互生命周期，协议 DTO/后端不可照搬。 |

相关规范：`.trellis/spec/backend/donation-caller-contract.md`（主库账本、冻结规则、仅可信终态释放、唯一奖励）、`donation-integration.md`（远端暂存/接收/恢复屏障）、`guides/cross-layer-thinking-guide.md`（字段与事件只设一处契约），以及 new-api 自己的 AGENTS/web AGENTS。new-api 未发现 `.trellis/spec/`；不能套用 gpt-load 的 Vue 或不用 testify 规则。

### 2. 推荐最小持久模型

模式字符串建议统一为 `auto | manual_review`，界面开关映射到它，避免多个布尔值出现“跳过但自动发奖”的组合。

| 实体 | 最小新增事实 | 原因 |
| --- | --- | --- |
| Campaign | `validation_mode` | 当前活动规则；默认 auto。 |
| CampaignRevision | 新版本 snapshot 自动包含模式 | 不改写旧 snapshot JSON。 |
| Batch | `validation_mode` | 提交时冻结，后续活动编辑与历史转审不修改。原 CampaignVersion/金额/target_revision/RequestDigest 仍不变。 |
| Item / 审核上下文 | `effective_mode`、单调 `remote_item_revision`、`effective_target_revision`、转审 `entry_action_id`、最终 `review_action_id` | 区分原自动项与显式转审；回执防旧消息降级；绑定实际配置。review_origin 可从原 batch 模式 + 转审动作派生，若持久则限制枚举。 |
| DonationReviewAction | `id`、认证 `actor_id`/审计名称、`request_key`、`digest`、`batch_id`/`item_id`、kind=`request_review/approve/reject`、原 `expected_item_revision`、`target_revision`、安全 note、可选 `test_id`、status=`pending/applied/rejected`、结果 revision/reason/时间 | 本地管理员决定意图先落主库，之后恢复原动作；status=applied 才是远端确认事实。actor/request_key 唯一，id 为发给接收端的稳定 UUID。 |
| DonationTestAttempt（或等价独立记录） | `test_id`、认证 actor、batch/item、模式/目标修订/审核上下文、模型、受控输入摘要、开始/结束、结果状态/安全原因 | 真实调用的独立证据；不以 DonationEvent.state 冒充审核状态；不存完整捐献 key 或 token。 |

动作表已有完整绑定时，item 上的 decision/actor/time 可通过动作投影取得，避免多份可独立修改的审核真相。发奖不得只读取 item.Approved 布尔值。新增表为主库业务历史，不随用户、组或凭据删除，不受普通日志保留策略清理。

#### 模式迁移与 NULL

1. 为存储模式使用普通字符串列，不依赖 GORM bool 默认。POST 省略模式规范化为 auto；PATCH 省略保留原值；显式 null/未知值拒绝。所有新 batch/item 都显式写规范化模式。
2. 添加列后，代表性旧行可能是 NULL 或空串。迁移可用 GORM `Where("validation_mode IS NULL OR validation_mode = ?", "").Update(...)` 规范旧 Campaign/Batch 为 auto；兼容读取仍处理旧缺省值。不可用 `mode != manual_review` 将任意未知模式视为 auto。
3. item 有已确认转审/审核动作时，空或矛盾的模式不能回退 auto；校验必须同时看不可变 batch 模式和已确认 entry/review 动作。legacy auto 的空字段兼容范围要与新增人工事实隔离。
4. 保留历史 CampaignRevision JSON、RequestDigest、指纹身份、Reward 唯一约束，不能因回填重新生成请求/奖励资格。重复迁移不得更新已有 manual_review 行或已确认动作。
5. 当前 `TestDonationDatabase` 的 upgrade=true 只先建 User/TopUp/Checkin（`model/donation_test.go:85`），不是“已有捐献 schema 升级”。必须补冻结的本次前 DonationCampaign/Batch/Item/Reward 表和数据，再执行新迁移两次，检查 NULL/空串、所有旧活动/批次 auto、manual 新行保留、旧流水/owner/唯一性不变、第二次无 schema mutation。
6. `OpenDonationStore` 的历史存在检查、测试清理列表、真实 SQLite 备份恢复都要覆盖 ReviewAction/TestAttempt；丢了秘密但只剩这些历史时也不能生成新身份。

### 3. v1 兼容与目标修订

- 保留 `protocol_version="1"` 和原 limits；新增显式 capability，例如人工审核、逐项动作/动作查询、真实调用及其边界。缺失能力时人工活动不可用，禁止自动降级为跳过测试直接接受。
- **旧响应 client 会忽略新增字段**，但会拒绝新状态值。新增状态只能出现在被明确送入人工模式的项；旧自动批次仍按原状态/回执运行。
- 目录保留原 `can_probe/target_revision`，另外给 `can_accept_api_key`、`can_manual_review`、`can_test_chat`、`manual_target_revision`。人工保存/提交按人工接收资格，不强制 can_probe；“不能跑通用 probe”与“连普通 key 都不能接收”必须分开。
- 自动批次发往远端时保留原 JSON 形状和原 digest 字节，不新增一个非 omitempty 的 `validation_mode:"auto"` 改写旧幂等请求。人工批次显式传 manual_review，使用其独立序列化/摘要域。
- new-api 的原 ParseLines 摘要本来只比较用户提交的 campaign + 原行指纹，继续保留；同 request key 重发先命中已有冻结 batch，不取活动的新模式重建远端请求。
- 原人工 batch 的 target_revision 是 manual hash；原自动 batch 转审后仍保存原 probe hash，item/action 另存 effective_target_revision。不能拿旧 batch hash 冒充人工测试/批准配置。
- 首版不提供刷新目标/改绑动作：转审时冻结同组 manual target，之后 test/approve 要求当前组与已冻结目标仍一致；变化返回明确错误。允许恢复原配置，或拒绝后重新捐献；reject 不依赖目标在线。更改活动不静默重定向历史项。

### 4. 权限与建议 API

管理端沿用 AdminAuth；新增独立 `donation_records.review`、`donation_records.test`（具体命名由总设计统一），并保留 records.read 的读取边界。只读记录用户不能批准、拒绝、转审或发起测试。默认管理员角色可按既有资源注册方式赋权，不能通过 config.write 隐含获得这些动作。

| 建议接口（new-api） | 行为 |
| --- | --- |
| POST `/api/donations/admin/records/:item_id/review-actions` | kind=request_review/approve/reject、expected_item_revision、受限 note、可选 test_id；Idempotency-Key 必填。目标从服务端上下文冻结/核对，不接受任意 group/url/key/header。 |
| GET `/api/donations/admin/records/:item_id/review-actions/:action_id` | 查询动作持久结果；也可在已有 record detail 投影 pending/完成动作，但要能准确定位原 ID。 |
| GET `/api/donations/admin/records/:item_id/test-options` | 从该项原目标取得可调用模型和安全限额，不能读取任意组；与记录/测试权限结合。 |
| POST `/api/donations/admin/records/:item_id/tests` | 指定模型、受限对话输入、目标修订，建立稳定 test_id 并流式返回；详情只需轻量对话能力。 |
| GET `/api/donations/admin/records/:item_id/tests/:test_id` | 对账测试元数据/终态，不触发上游请求。 |

所有 actor 取 `c.GetInt("id")`，UUID 严格用现有 ValidDonationID。沿用 readDonationJSON 白名单和静态错误出口，接收人/金额/捐献 key/服务 token 不可从管理员请求覆写。已有 dashboard 是 Authorization 认证，不能为 SSE 改成 query token/cookie-only 降低边界。权限是在服务端实施；前端隐藏按钮只是对应展示。

审核 note/输入也可能被人粘入秘密，应限制字节、以文本展示、不给裸上游错误/认证内容进入持久日志。现有通用 AdminAuth 审计只记路由/HTTP 结果，并不能替代主库 ReviewAction 与 TestAttempt 的结果事实。

### 5. 审核动作与响应丢失的可靠顺序

1. 刷新该项可信远端事实，校验当前项可操作、owner/generation 仍属于该项；有其他 pending 决定时返回其可查询状态或冲突。
2. 先在本地事务中保存审核动作 pending（原 expected revision、有效目标、actor、输入摘要），同时唤醒恢复。网络调用不在数据库事务内。重放同 actor/request_key 要返回原动作；同键改 decision/item/note/config/test_id 冲突。
3. 将稳定 action.id 作为远端幂等键。远端先查动作账本再校验当前 revision，防成功后重放因状态已前进而误报失败。支持查询同 action ID。
4. 接收端对合法请求的**业务结果**持久化 applied/rejected，包括 stale revision、目标变化、过期等确定结果；返回绑定 action/item/kind/原摘要及结果 revision 的结构。认证失败、非法请求和基础设施错误不伪造业务结果。
5. apply 时用数据库行锁/CAS 裁决 first legal action。approve 与正式凭据/资源取得/committing 的事务绑定；runtime 发布恢复完成才 accepted。reject 或 expiry 首先获胜则无正式接收；approve 已 committing 后 expiry 不再清理或撤销。持久化决定不可被并发另一位管理员更改。
6. new-api 原 POST 响应丢失后查相同 action ID/batch；必须验证本地 pending 动作和远端回执的完整绑定，再在事务中转为 applied/rejected。远端业务拒绝结束该动作，但**不等于该 key 被拒收**；是否释放 owner 仍取决于 item 可信终态。
7. timeout、404、代理错误、丢失 body、普通 409 都不能自行清 pending 后另开相反决定，更不能释放 owner。恢复只重放同动作 ID 和原语义；确定失败后需要纠正的下一次操作使用新 ID。浏览器关闭不撤销已记录审核决定。
8. 原结果有 accepted + review_action_id 时，可作为 matching local approve intent 的远端成功确认，原子完成动作事实和 ApplyReceipt；若该 batch 回执不含足够绑定信息，先 GET 原动作结果，再应用。不能因为本地动作仍 pending 就永久拒收唯一已完成的远端结果。

锁顺序应与现有 `resource → item → reward/user` 一致，pending 动作创建与 needs_recovery 更新同事务。新增批次锁不得与 ApplyReceipt 最后更新 batch 形成没有重试保护的相反锁序。继续使用 `lockForUpdate`（`model/locking.go:20`）、现有 transaction 重试和唯一约束；不使用 upsert.RowsAffected 猜首次所有权。

### 6. ApplyReceipt / CreditDonationReward 的双重门槛

#### ApplyReceipt

- 保留现有实例/source/batch/group/原 target_revision、完整 dispatch item 集合、凭据/时间的校验，新增模式与 item revision 的可信绑定。
- 对 manual_review 或显式转审项，验证 effective_mode、effective target、entry_action（如有）、review_action 与本地主库对应动作一致。无本地动作、动作是 reject/pending 未确认、属于另一项/实例/目标或远端模式矛盾时，返回 invalid receipt，不标记可奖励。
- 远端 item revision 小于本地已记录 revision：旧观察不能回退；相同 revision 但事实不一致：拒绝；大于则按允许的状态转移应用。自动 legacy 允许字段缺失/0，但绝不能用缺省回执覆盖已转人工项。
- accepted 必须绑定 **已确认 approve 的同 action ID**，而且仍满足凭据和 accepted_at_ms。旧 automatic accepted 不要求审核 ID，保持完整兼容。
- 新 `pending_review` 非终态且不发奖；`rejected` 为可信人工拒收终态；暂存过期可沿用 invalid/staging_expired 以减少新状态，页面显示“暂存已过期”。受信任 rejected/expired 只在 owner/generation 对应且未 acquired 时释放未决归属。
- accepted/existing 永久 acquired；旧接受事实、已发奖励绝不因新活动模式、远端删除、测试失败或迟到拒绝撤销。

#### CreditDonationReward

在现有事务 `accepted + Dispatch + CredentialID/AcceptedAt + acquired/owner/generation + SecretID` 基础上，再读取冻结 batch 模式与 item 的已确认有效模式：

- legacy/未转审 auto：走旧规则，无新增人工条件。
- 原 manual batch 或存在已确认 entry_action 的 item：必须匹配已 applied 的 approve action（同 item/batch、有效目标、远端 action ID）；否则没有任何额度变化。
- 已受理 accepted 结算只依赖保存的可信事实，不再次调用远端、不要求当前活动启用或远端凭据仍存在（保留 `service/donation.go:317` 的恢复保证）。
- 原 `Reward` 双唯一、MySQL current read、启用用户检查、钱包上界、creditTopUpQuota 事务、奖励事件、首次提交才同步缓存都不改变。仍只增加永久 User.Quota，不经过管理增额、签到临时桶或普通 Relay 账单。

数据库里只有一个 approval=true、页面显示测试成功、模型返回了正文、管理员 POST 已发出、远端 committing，都不足以发奖。

### 7. 旧 retry_exhausted 与不确定批次的转审

- **可显式转审**：远端确认仍未接收、加密 payload 尚存、同 owner、无活跃自动测试租约的 retry_pending 项；优先覆盖 retry_exhausted。必要时也可对已安静下来的其他不确定 probe 结果提供同一入口。
- 本地可能显示 unconfirmed：先恢复读取原 batch。只有确认已暂存且取得可比对 item revision 后才能转审；后台没有明文时不能补造新 batch，也不能拿“远端一次 404”证明没有接收。
- 原远端暂存尚未成功时，用户仍按原 request_key、原 key、原自动模式恢复原提交；恢复确认后管理员另发转审动作。不能通过活动改 manual 来改变原 POST。
- 转审保持原 user/reward/campaign version/request digest/owner/generation/batch 模式；item 增 entry_action_id/effective_mode/effective target，state 到 pending_review；不释放再争抢归属，不重新取得领奖资格。
- 原正在 validating 的 worker 与转审不能同时接收：以接收端行锁/expected revision/租约裁决；首版可以要求待当前测试结束再操作，不在本地强制改成 pending_review。
- 已 accepted/existing/invalid 终态不能转审。payload 过期则可信终态释放未决 owner 后用户新提交；永久 acquired 不释放。
- 转审不续期，仍沿用原 7 天暂存到期时间；UI 显示原截止时间。rejected 不入库、不发奖，旧已发奖励不追回。
- 用户 retry 在 manual/pending_review 上不能恢复自动 probe；PrepareRetry/service/远端需三处形成一致允许条件，空 item_ids 的“全部重试”也不能绕过。

### 8. 待审核冷对账与热恢复隔离

当前算法把除 accepted/invalid/existing 外所有 dispatch 项计作 waiting（`donation_reward.go:272`），若只扩充白名单，长等待会一直占热恢复 32 条且最多每分钟查一次。页面还会每 5 秒请求，管理详情却没有自动刷新。

最小改法可复用现有 Batch 字段，不必引入通用任务平台：

1. `SettleBatch` 同事务统计 active item、pending_review、未发奖励、pending Retry 与 pending ReviewAction。**只有 pending_review、无其他活动工作**时 `needs_recovery=false`；完整终态也为 false，但冷查询通过待审项存在性区分。
2. 热循环仍取 needs_recovery=true。单独的小配额低频循环取 `needs_recovery=false AND next_poll_at_ms <= now AND lease_until_ms <= now AND EXISTS(该 batch 有 pending_review)`；例如每分钟领取少量、每批数分钟一次。该循环只 GET 事实，独立于热循环，不能用同步等待长冷任务堵住新接收/发奖。
3. PrepareReviewAction/Retry 与批次唤醒同事务：needs_recovery=true、next_poll=now、重置退避。已确认新远端进展、accepted 未奖也回到热工作。
4. 领取 CAS 不仅复核租约，还复核对应队列 eligibility；冷查询后刚有批准动作，不能再按旧 cold 结果覆盖 active。Release 持有正确 lease token 并按最新 flags/待处理动作重算退避，不能把刚被唤醒的 active 项延后到数分钟。
5. 保留按 next_poll/id 排序的到期队列，处理后移至未来；不要用 offset 分页扫描不断变动的恢复集合。验证超过一页待审与热任务混合时，两条队列都持续前进；terminal 无 pending 动作后停止轮询。
6. 当前 Recover 一次预领 32 条、每条 45 秒且租约只有 120 秒，后面条目可能开工前就过期。这是现有约束，新增冷队列不要复制扩大；建议按小批/开工前领取，或按实际执行预算限定每次领取数量，始终以租约 token 做提交条件。
7. Summary 增 pending_review/rejected（或等价显式字段），不要让拒绝永远落入 Processing 默认分支。ReconcileBatch 的早退需包含待审数量和 ReviewAction，否则冷队列虽然选到也不会读远端。待审主要靠冷对账接收过期等事实；receipt.State 不取代逐项统计。
8. UI `donationBatchIsProcessing` 区分活跃处理和等待人工；用户待审可以低频刷新，管理详情打开时刷新且动作后立即 invalidate 当前详情/批次/列表。终态停止；页面只查看本页不影响后台全量对账。

所有后台恢复只可以对账/重放原审核命令，**不自动发起或重发真实模型测试**。

### 9. 流式真实调用与独立证据

`admin browser → new-api donation service → gpt-load integration test → 指定暂存 CredentialRef`，不经过 new-api 的 Playground/Relay、站内 token、渠道分发、用户钱包或自动换 key 链。上游测试可能产生提供方用量，但站内用户余额不应被预扣、结算或退款。

- new-api 使用独立受限的流式 client 方法；沿用固定 URL/TLS/禁止重定向/无代理与 server token，不直接使用当前 12 秒 + ReadAll JSON 方法。设置有限总时长、单事件/总输出字节上限，Content-Type/event 类型白名单，向浏览器 flush 结构化安全文本事件。
- 模型来自该项有效目标的 server options。请求只允许模型、受限 messages/提示词与小范围生成参数；拒绝 group/key/base_url/Authorization/任意 path、工具执行、文件/图片、provider 自由扩展字段等绕过凭据绑定的输入。
- gpt-load 在开始网络请求前保存 test_id/执行意图，单次调用持久状态可查询；相同 test ID 绝不重复真实调用。收到重复/断流时只查原结果，若仍不确定显示 inconclusive/interrupted，用户显式新测试才产生新 ID。不能靠普通 POST 的“幂等请求”保证第三方上游恰好一次执行。
- 测试结果至少绑定：instance/source、batch/item、完整凭据的服务端身份、effective target revision、模型、请求摘要、审核上下文/开始时 item revision、执行结果、时间、test ID。浏览器不可上传 success 或替换模型/配置充当证据。
- 测试状态 success/failed/cancelled/interrupted 与审核 approve/reject 分开；成功仅在收到有效完整结束且结果持久化后成立。流开了、收到部分 delta、HTTP 200、取消/超时/中断都不算成功。
- 取消链由 Abort/关窗/会话切换一路传到 new-api request context、远端 HTTP body/执行 context；首版不用“重连后自动继续调用”。服务重启后遗留 running 记录只能标 interrupted/不确定或读取原远端结果。
- 审核可选引用匹配 test_id；不是“必须测试成功才可人工批准”。引用证据只允许同 item、有效目标、模型上下文的完成记录；目标变化时旧证据不能当作当前配置的证明。是否批准仍是独立显式决定。
- 推荐最小持久内容为结果元数据和安全原因，不将完整聊天 transcript 当作新通用会话历史；流式回复在管理员当前详情查看。测试证据存主库，不依赖日志保留。
- new-api 从未拥有完整捐献 key，因此不能独自负责上游回显 key 的脱敏。接收端必须先把输出归一化，并对已知 key/token 做跨分片脱敏；原始 error/body/header 不能直通。new-api 二次限制结构与长度，以 React 文本显示，不能执行 HTML/模型指令。
- 通用管理审计会把 SSE 200 视作 HTTP 成功；持久 TestAttempt 才是测试结果，必要时显式写安全终态审计，不能从通用 audit.Success 推导模型可用。
- 前端沿用 donations 的 session/user/item 隔离与 getFreshAuthHeaders；playground 控制器只作代次、stop/dispose 参考。既有 sse.js/axios 的自动重连或刷新行为需显式禁用，防悄悄重复测试。

### 10. 必须保护的测试契约与命令

优先扩展现有 `model/donation_test.go`、`service/donation_test.go`、`controller/donation_integration_test.go` 和 donations 各职责的前端测试，避免每层新增相同 fixture。所有新状态测试验证实际额度/归属/动作结果，不只断言字段存在。

| 边界 | 必测结果 |
| --- | --- |
| 旧协议/模式 | 缺字段活动与旧 auto batch 行为相同；旧请求字节与摘要不变；新响应附加字段不破坏旧 client；未知模式和缺失人工能力拒绝。 |
| 真三库升级 | 新库 + 含旧 DonationCampaign/Batch/Item/Reward 的旧库；NULL/空值、重复两次无 schema DDL、manual 值不被覆盖、旧唯一性/owner/奖励完整。MySQL 两种 clientFoundRows 及 REPEATABLE READ。 |
| 最小完整流程 | 手工提交待审、零奖励、未入池；approve 同 action accepted 后永久奖励恰一次；reject/expiry 无凭据/奖励。 |
| 双重发奖防绕过 | 无本地 approve、错 action/item/source/target、只有 pending intent、reject/错模式、测试成功但未批准，ApplyReceipt/Credit 均无额度变化。 |
| 并发 | 同键同义重复、同键改义、两管理员 approve/reject、approve/expiry、转审/自动 worker、Retry/转审、approve/测试进行中、两个 DB 连接同时发奖。 |
| 恢复 | 本地动作先提交后进程退出；远端已 applied 但响应丢失；本地确认后发奖失败；committing 后远端重启；多个后端同时重放同 action；GET 404/超时不释放 owner。 |
| 单调事实 | stale auto 回执不能覆盖已转审；旧 pending_review 不能撤销 reject/accepted；同 revision 矛盾拒绝；同 accepted 凭据/时间不同拒绝。 |
| 旧耗尽转审 | 原 key 不重输、原 batch/request/reward/owner 不变；转审不续期；已过期/已 acquired 不可重新取得资格；未确认批次先恢复、不能重写模式。 |
| 账号/钱包 | approve 后用户停用/删除只暂停发奖、恢复后原金额恰一次；钱包上界/事务失败完整回滚；缓存保留既有预扣；已奖不追回。 |
| 冷热队列 | 超过 32 条待审不阻塞新 accepted 发奖；冷队列持续对账过期；并发批准唤醒不被旧 cold release 延后；多页无饥饿；rejected 终态停止轮询。 |
| 授权 | 匿名/普通用户/只读管理员不能测试或审核；传他人 actor/group/key/quota 拒绝；服务 token 不回浏览器；管理员当前权限已撤销则新动作拒绝。 |
| 指定 key 调用 | 本地模拟上游记录真实 key/model，组里其他有效 key 不能替代；new-api 用户额度/预扣/consume log 无变化；自动 probe 与聊天测试分别可观察。 |
| 流式边界 | 分片 UTF-8/跨片 key 回显、无效事件、过大单事件/总输出、部分输出后失败、浏览器取消、双端重启，均得到安全终态且不自动发新上游调用。 |
| 前端操作 | 模式 PATCH 保留/切换；读权无按钮/写权独立；同 action 恢复、item/session 切换取消、迟到事件不污染另一项、输出文本安全、实际决定与证据各自展示。 |

实现后的定向命令（本研究未运行）：

- `go test ./model -run '^TestDonationDatabase$' -count=1 -v`，配置受控测试 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN`；记录真实版本和每个子测试，skip 不算通过三库。
- `go test ./model -run '^TestDonation' -count=1`；新增动作/模式测试集中加入现有 suite。
- `go test ./service -run '^TestDonation' -count=1`，扩展现有断 TCP/丢回执场景。
- `go test ./service/authz -run 'Donation' -count=1`。
- `go test ./controller -run '^TestDonationIntegrationRealServices$' -count=1 -v`，配置 freshly built `DONATION_TEST_GPT_LOAD_BIN`、`DONATION_TEST_NEW_API_BIN`；只用隔离库和本地模拟上游。
- frontend 按 `web/package.json` 已有脚本运行受影响 donations/playground 共享组件用例、typecheck、涉及文件 lint/build；不能只用 mock 代替两端真实接口串通验证。

## External References

- 版本依据 `../new-api/go.mod:4` / `:63`：Go 1.25.4、GORM 1.25.12、mysql driver 1.5.7、postgres driver 1.5.9、glebarez/sqlite 1.11.0。该任务不建议升级依赖。
- OWASP ASVS 与 Authentication/Session Management/Authorization/REST Security/Logging Cheat Sheets 是 new-api AGENTS 要求的实现参考；本次没有修改认证流程，也未声称完成 ASVS 合规验证。后续涉及新增权限/流式认证的实现应核对适用官方控制并记录验证。
- [OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)
- [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)
- [Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html)

## Caveats / Not Found

- 此研究只写本文件，没有连接生产、发真实模型请求、执行数据库迁移/测试或改 git 状态；真实三库与双进程联调均待实现阶段验证。
- 既有代码已经使用真实 probe；新对话测试不能保证绕过渠道的客户端/协议限制，也不证明 cline 最近错误的具体原因。
- 接口/字段名仍由主会话总设计统一，不能让两个仓库分别采用不同命名。接收端需要同步动作查询、业务拒绝账本、有效目标、item revision、流式取消/脱敏；new-api 单端不能补足这些事实。
- 升级先让接收端提供新能力，再更新 new-api；启用人工后不能把旧 new-api 当作可安全处理人工状态的回退版本。回退应先禁止新人工入口并保留新账本/模式；不能重置身份、清空表或将待审自动改成 auto。
- 首版保持 7 天暂存，转审不续期；不加刷新目标/改绑、捐献人自测、自动测试重发、累计领奖限制、奖励失效或追回。

## Final Planning Consistency Review — 2026-09-15

本轮只读最终 `prd.md`、`design.md`，未测试或修改产品。最终协议名称以 design 为准：`auto | manual_review`、`review_target_revision`、`enter_review`；本文前面的候选 `effective_target_revision/request_review` 不构成并行契约。

只发现两处需要小修订的明确缺口，其余人工动作确认、双重发奖条件、转审保留 owner/摘要、committing 不被截止撤销及冷对账链路一致：

1. **legacy auto revision=0 的兼容例外需明确。** design §3 同时规定“同 revision 矛盾事实拒绝”和“legacy auto 缺失版本按 0 兼容”。新 new-api 对旧 gpt-load 会合法收到 queued(0) → validating(0) → accepted(0)，若套严格等版本规则将拒绝真实进展。建议明确：从未进入人工/版本化路径且旧端缺版本的 auto 项，使用既有 accepted/invalid/existing 单调规则；已记录 revision>0 或任何人工事实的项才强制版本规则，且绝不能被 0/缺省回执覆盖。验证应包含新调用方连接无新 capability 的旧端自动流程。
2. **用户侧拒绝备注出口需补齐。** PRD R2/AC2 要求捐献人看到拒绝原因，design §4 的 item 回执/投影只列 review_decision/reviewed_at_ms，尚未明确已有 self batch 查询如何读 note。建议在 new-api 本人 batch/item 投影中提供安全 `review_note`，来自同项已 applied 的 reject 动作；静态 reason_code 与自由备注分开，避免 DonationReason 闭集将备注丢成 unknown。无需对普通用户开放 admin 动作/测试查询；历史拒绝备注随已有批次恢复查询持续可读。

以上两条已即时发送主会话。最终设计修订后以其更新文本为准；本报告不声称实现验证完成。
