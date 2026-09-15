# new-api 捐献调用方契约

## 1. 范围

本文约束 new-api 调用 gpt-load、持久化捐献来源并发放永久额度的跨仓库行为。代码在 new-api 的 `model/donation*.go`、`service/donation*.go`、`controller/donation.go` 和 `router/donation-router.go`；编码规范以 new-api 自己的 AGENTS.md 为准，不能套用 gpt-load 的测试或前端规则。接收端见 [donation-integration.md](donation-integration.md)。

## 2. 签名与持久实体

- `OpenDonationStore` 加载独立持久秘密；`PrepareBatch` 冻结活动/用户/目标/金额和逐项归属。
- `ApplyReceipt` 校验可信远端回执并保存逐项事实；`CreditDonationReward` 是唯一奖励边界。
- 主库保存 DonationSecret、Connection、Campaign/Revision、Batch、Item、Resource、Reward、Retry、ReviewAction、TestAttempt、Event；日志不是账本。动作与测试表参与秘密丢失检测和备份。
- 全 key 指纹唯一，Reward 的资源指纹和成功 item 各自唯一；批次请求键按认证用户作用域去重。

| new-api 接口 | 契约 |
| --- | --- |
| GET `/api/donations/campaigns` | 已登录用户的活动投影，不返回连接秘密 |
| POST `/api/donations/batches` | `campaign_id/keys_text`，小写 UUID v4 Idempotency-Key；收款人/组/金额由后端确定 |
| GET `/api/donations/batches`、`/:id` | 仅本人，分页参数 `p/page_size` |
| POST `/api/donations/batches/:id/retry` | `{}` 或 `item_ids`，新的动作 Idempotency-Key，不需已暂存 key |
| GET/PUT `/api/donations/admin/connection` | PUT `base_url/token?`；空 token 保留原值，GET 不回显秘密 |
| GET `/api/donations/admin/group-options` | 已连接 gpt-load 的最小目录；加载失败不等于空数据 |
| GET/POST `/api/donations/admin/campaigns`、PATCH `/:id` | 名称/说明、已有 group_id、整数 reward_quota、enabled、validation_mode；模式默认 auto，活动缺省关闭 |
| GET `/api/donations/admin/records`、`/:item_id` | 独立管理权限、分页/筛选/历史详情 |
| GET `/api/donations/admin/records/:item_id/review-context` | 真实 effective_mode、item_revision、review_target_revision、review_action、can_review/can_reject/can_test、模型及限额 |
| POST `/api/donations/admin/records/:item_id/review-actions` | 严格 JSON `{kind,expected_item_revision,review_target_revision?,note?}`，动作 UUID 仅来自 Idempotency-Key |
| GET `/api/donations/admin/records/:item_id/review-actions/:action_id` | 原意图及 pending/applied/rejected 结果；不创建新动作 |
| POST `/api/donations/admin/records/:item_id/tests` | `{expected_item_revision,review_target_revision,model,prompt,system_prompt?,max_output_tokens?,stream?}`，test UUID 仅来自 Idempotency-Key |
| GET `/api/donations/admin/records/:item_id/tests/:test_id` | 安全元数据，无保存的正文，也不重发模型请求 |

管理权限分别是 `donation_config.read`、`donation_config.write`、`donation_records.read`，同时保留 AdminAuth。审核/测试 POST 还分别要求 `donation_records.review` / `donation_records.test`，写操作仍须 records.read。actor 取认证 user ID，由 new-api 填为 `user:<id>`；浏览器不能指定 actor、action_id 或 test_id JSON 字段。

## 3. 数据与恢复约束

- new-api 包装是 `{success,message,data}`，gpt-load 是 `{code:0,message,data}`；双方成功字段不能互换。
- 连接固定 gpt-load instance/source UUID。URL/token 更改不重置 key 身份，不静默改绑历史。服务端只允许 HTTPS 或本地 loopback HTTP，拒绝 URL 凭据、查询串、片段、额外路径和重定向。
- 私有 v1 秘密槽独立于 CryptoSecret、登录、连接 token 和活动。HMAC/加密材料分开使用；材料缺失、损坏或历史材料不一致（含 NULL）时明确拒绝，不为旧历史生成新资格。
- key 只在请求必要内存中存在；只 trim 外侧空白，保留大小写和原行号。短 key 完全掩码，长 key 最多保留末四字符。
- 只有取得本地资源 owner 的 item 发往远端，避免两端并发选出不同获奖者。实际转发 JSON 的字节上限在占用 owner 前校验，包括 HTML 字符转义膨胀。
- 批次 `reception_state` 为 `local_only/unconfirmed/confirmed`。未确认不能宣称可靠接收；HTTP 超时、404 或租约过期不释放 owner。原内容可用本人批次的非秘密 `request_key` 恢复，后台没有明文时只对账。
- 显式 send_attempts 之外不得发生透明 POST 重发；客户端带 body 的 POST 清空 GetBody，恢复由持久状态驱动。
- 逐项接收和奖励状态分开：`accepted` 后才可 `pending/paused → rewarded`。旧观察不能撤销已接收事实。仅可信终态未接收或唯一首次发送的明确拒绝可释放未决归属。
- 时间字段为毫秒并以 `_at_ms` 命名；金额为整数额度单位。页面换算/时间格式必须遵守边界，不能把秒和毫秒混用。
- 页面按会话及原请求标识隔离回调，确认事实只能前进。GET 先确认后，迟到 POST 不能降级；旧请求不能覆盖另一份恢复表单。确认时只清理原输入一次，后续旧批次进度不能擦除用户正在输入的新草稿。
- 活动说明限制按 UTF-8 字节数校验；Go 的 `len(string)` 不能直接等同于浏览器的字符串字符数。
- `HTTP_LISTEN_HOST` 是可选监听地址，缺省仍为原 `:port`；本地隔离验证使用 loopback。

### 人工审核与恢复

- 活动/批次冻结 `validation_mode=auto|manual_review`，未知值拒绝。历史 auto 请求不新增模式字段，旧活动快照、请求摘要、资源身份和奖励不改写；远端无 `manual_review_v1` 不得启用或提交人工模式。
- Item 的 effective_mode 与原 Batch 模式不同：auto 耗尽项可显式 enter_review，但不改变原批次、Attempts、暂存截止或金额。context 如实返回 auto，使用 review_action 表示转审；enter_review/approve 都绑定所见目标，reject 不携带目标。
- 审核先持久保存原 UUID、actor、kind、expected revision、目标和备注，再访问远端。action_id/test_id 在本地台账全局唯一，跨 actor 复用冲突；未决意图冻结，不能另开相反动作或测试。
- 完整回执是 item revision 的唯一推进来源，部分 context/action 响应不能替代它。旧版本忽略，同正版本不同事实拒绝；从未进入人工的 legacy auto 仍允许持续 0 版本按原状态推进。人工事实不能被高版本 auto 降级。
- 三套枚举分别是 action.kind=`enter_review|approve|reject`、action.status=`pending|applied|rejected`、item.review_decision=`approved|rejected` 或空。`confirmReviewFactFromReceipt` 显式映射 approved→approve、rejected→reject，不能直接用决定值查询命令。
- 人工 committing/accepted/rejected 绑定原 item/batch、动作、决定、目标、版本及源时间；`reviewed_at_ms` 必须等于对应 action.applied_at_ms。accepted 先于动作查询到达时，只能按完整回执确认原 pending approve；`CreditDonationReward` 独立复核同一批准事实。
- 批准时库存已先取得可合法得到 existing + approved：同样绑定原 approve，按该终态实际 revision 确认丢失的动作结果，不假定所有动作只加一；existing 不发奖励、不认成本项首次取得。
- 本地 TTL、404 或超时不证明远端未接收，不能释放 owner。仅可信未接收终态释放尚未 acquired 的本项归属；批准已 committing 后截止不能撤销取得。
- 只有 pending_review 的批次进入有限冷对账；pending 动作、自动处理或待发奖进入热恢复。逐批开工才领租约，release 重新判断条件，不能用旧冷任务覆盖新动作的唤醒。
- 管理详情投影 pending_review_action、latest_test 及各最近 20 条 recent_review_actions/recent_tests；有 records.read 可读其他管理员安全历史，不能冒用 actor 发动作。本人批次只展示确切最终 reject 的备注，不下放批准备注或测试正文。

### 真实调用与页面状态

- 单 key 测试沿受限专用客户端到 gpt-load，不走普通 Relay/Playground 或站内钱包；同 ID 无论 running/终态只读原元数据。取消、超时或断流不自动重测。
- 请求总计最多 120 秒、首包 30 秒、idle 20 秒，使用更严格原组/暂存限制；正文和单事件分别最多 128 KiB/64 KiB。prompt 合计 16 KiB，输出默认 1024、上限 4096 tokens；以协商的 review_limits 为准。
- 非流式与 SSE 的元数据都校验 test/batch/item/model/start revision/目标/时间。流式须 meta→delta*→done，完整协议、有效文本且本地元数据持久成功后才能发 succeeded；JSON 重放不能伪造成新的流正文。
- 已知秘密在完整缓冲上确定匹配边界再输出，保持 UTF-8；不能固定截断跨 holdback 的秘密。提示词和回复不进入普通日志、业务历史、浏览器持久存储或 Query 缓存。
- 页面按 item/账号/会话隔离；关闭详情、切项或退出取消旧调用并清正文。未知审核结果只查原 UUID，原动作确实不存在时也只能用原 actor/原内容重放；测试成功不是人工批准门槛。

## 4. 校验与错误矩阵

| 场景 | 必须行为 |
| --- | --- |
| 匿名/普通用户读取管理配置或他人记录 | 使用既有认证和权限出口拒绝 |
| 请求体额外指定 user_id、group_id 或 reward_quota | 拒绝越权字段，不能覆盖后端快照 |
| 同请求键同内容/不同内容 | 返回原结果 / DONATION_CONFLICT |
| 回执身份、组、revision、完整 item 集合、凭据或时间不匹配 | DONATION_INVALID_RECEIPT，不发奖 |
| 账号禁用或删除 | 保留归属并暂停未发奖励；恢复后原单继续 |
| 钱包上界或事务故障 | 整笔奖励与余额一起回滚，保留待处理事实 |
| 活动关闭或目标失效 | 关闭拒绝新批次；已受理按冻结规则处理；目标不静默替换 |
| 连接/指纹秘密损坏 | DONATION_IDENTITY_UNAVAILABLE，不重置历史 |
| review_decision 误传 approve/reject、决定与原动作相反或源时间不符 | DONATION_INVALID_RECEIPT，不确认动作或发奖 |
| 人工 accepted 没有可信同项 approve，或企图降为 auto | 拒绝回执/奖励，不从裸 accepted 生成批准 |
| 完整 approved + existing 回执 | 记录原动作与已有库存，保持零奖励 |
| 目标变化/下线，但原暂存仍有效 | 禁止测试/通过；保留拒绝入口 |
| 仅 records.read 或普通用户提交审核/测试 | 服务端拒绝；不能只依赖隐藏按钮 |
| 停止/缺 meta/缺 done/错模型/持久失败 | 非成功、不重测、不保存对话正文 |

## 5. 正常、基础与错误例

- 正常：混合有效、无效、库存、重复、限流项分别记录；只有自动校验通过或人工批准且首次新增的 item 增加永久 User.Quota。
- 基础：同资源/同成功 item 至多一个 Reward；唯一流水、余额增量、已奖励状态与事件同事务提交。缓存只在该首次提交后应用增量，保留既有预扣。
- 错误：已有库存不奖励、不冒认来源；凭据/组失效、禁用、删除或普通日志清理均不清除历史，不恢复资格、不追回已发奖励。
- 不增加每日/累计领奖上限、签到临时桶或奖励有效期。只有单次请求/数值安全边界，不能把它们变成累计业务上限。

合法人工事实片段示例：动作结果为 `{"kind":"approve","expected_item_revision":5,"effect_revision":6,"outcome":"applied"}`，item 回执包含 `{"state":"accepted","review_decision":"approved","item_revision":7}`，且完整动作/目标/时间都匹配。错误地把 `review_decision` 写成 `approve` 必须拒绝；测试替身也使用真实 wire 字面值，不能复用命令常量掩盖差异。

## 6. 验证点

- `TestDonationDatabase`：真实 SQLite、MySQL、PostgreSQL；新建、冻结的旧 DonationCampaign/Batch/Item/Reward 历史升级、重复迁移、两个独立连接、唯一性、回滚、暂停、上界和缓存预扣。PostgreSQL fixture 使用独立 schema，显式重复 INSERT 验证 action/test 唯一索引真实生效，不能只检查 DDL 成功。
- MySQL 同时覆盖 clientFoundRows 两种语义及 REPEATABLE READ；奖励唯一流水使用 current read，不能被等待锁之前的旧快照隐藏。
- service 测试覆盖真实 TCP 连接断开不透明 POST 重发、丢回执、首次明确拒绝、先前未知结果、身份误绑、JSON 膨胀和静态错误脱敏。
- `TestDonationIntegrationRealServices` 用真实两个应用进程和临时 SQLite、本地 Gemini HTTP，验证真实登录/配置/提交/worker、响应丢失与两端重启、永久奖励和删除后历史；无 binary 时的 skip 不算验收。
- `TestDonationManualReviewIntegrationRealServices` 覆盖人工零奖励待审、普通用户/只读管理员写权限、stream/nonstream 指定 key、幂等与停止、批准后一次永久奖励、旧耗尽项转审、丢动作响应后进程重启恢复和目标删除后拒绝原因。
- model 的 manual_review_and_reward / manual_acceptance_requires_approval / manual_approval_finds_existing 用真实 approved/rejected 字面值，验证反向决定、错动作/目标/时间及等版本矛盾仍拒绝；applied/pending 两种批准后 existing 都不奖励。
- `TestDonationReviewReconcilesLostResponse` 覆盖动作执行前/后丢回应，后者只有一次 POST；其他 service 用例验证 SSE 身份、缺 meta、首包/idle 限额、取消及 holdback 脱敏。
- 页面必须用真实接口补验证：风险小字、移动入口、混合状态、原请求恢复，以及跨用户切换的请求/结果/缓存隔离。
- `TestDonationSQLiteBackupRetainsIdentityAndRewards` 用真实 SQLite 独立备份/恢复验证秘密材料、原奖励和跨用户防重复；备份必须保留 DonationSecret 与完整主库业务表，不能仅恢复可见记录后重新生成身份。
- `TestDonationPermanentRewardSurvivesCheckinRetention` 使用实际过期桶查询与清理验证永久奖励保留；当前签到按查询时间排除过期桶，不能把不存在的清理定时任务写成已执行证据。

## 7. 错误写法与正确方式

| 错误 | 正确 |
| --- | --- |
| 先调用独立事务的管理增额，再写捐献流水 | 在同一个事务中写唯一 Reward、调用事务内 creditTopUpQuota 并标记 item |
| 用 upsert RowsAffected=1 证明首次资格 | 唯一约束裁决；冲突事务回滚后用新/current read 核对原内容 |
| 请求超时后释放 key 给新用户/新批次 | 保留原 owner/batch/item，先对账，不以一次404证明未接收 |
| 重放已奖励结果时再加缓存或覆盖余额 | 只有首次提交的事务同步增量，重放读取原流水 |
| 401 自动刷新后把旧 key 随当前账号重发 | 页面请求绑定发起用户+会话，身份变化中止并丢弃晚结果 |
| 用 approve/reject 常量同时构造动作和回执 fixture | 动作与决定各自闭集，fixture 使用接收端 approved/rejected 字面值，并跑两服务真实联调 |
| accepted 或 succeeded 足以发人工奖励 | ApplyReceipt 与 CreditDonationReward 独立验证同项已应用 approve 和完整接收事实 |
| 固定扣掉 maxSecret-1 后输出，不看完整匹配 | 在完整缓冲确定秘密边界，保留跨片段匹配与 UTF-8 边界 |
