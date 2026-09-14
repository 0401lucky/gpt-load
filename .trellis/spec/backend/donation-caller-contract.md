# new-api 捐献调用方契约

## 1. 范围

本文约束 new-api 调用 gpt-load、持久化捐献来源并发放永久额度的跨仓库行为。代码在 new-api 的 `model/donation*.go`、`service/donation*.go`、`controller/donation.go` 和 `router/donation-router.go`；编码规范以 new-api 自己的 AGENTS.md 为准，不能套用 gpt-load 的测试或前端规则。接收端见 [donation-integration.md](donation-integration.md)。

## 2. 签名与持久实体

- `OpenDonationStore` 加载独立持久秘密；`PrepareBatch` 冻结活动/用户/目标/金额和逐项归属。
- `ApplyReceipt` 校验可信远端回执并保存逐项事实；`CreditDonationReward` 是唯一奖励边界。
- 主库保存 DonationSecret、Connection、Campaign/Revision、Batch、Item、Resource、Reward、Retry、Event；日志不是账本。
- 全 key 指纹唯一，Reward 的资源指纹和成功 item 各自唯一；批次请求键按认证用户作用域去重。

| new-api 接口 | 契约 |
| --- | --- |
| GET `/api/donations/campaigns` | 已登录用户的活动投影，不返回连接秘密 |
| POST `/api/donations/batches` | `campaign_id/keys_text`，小写 UUID v4 Idempotency-Key；收款人/组/金额由后端确定 |
| GET `/api/donations/batches`、`/:id` | 仅本人，分页参数 `p/page_size` |
| POST `/api/donations/batches/:id/retry` | `{}` 或 `item_ids`，新的动作 Idempotency-Key，不需已暂存 key |
| GET/PUT `/api/donations/admin/connection` | PUT `base_url/token?`；空 token 保留原值，GET 不回显秘密 |
| GET `/api/donations/admin/group-options` | 已连接 gpt-load 的最小目录；加载失败不等于空数据 |
| GET/POST `/api/donations/admin/campaigns`、PATCH `/:id` | 名称/说明、已有 group_id、整数 reward_quota、enabled；缺省关闭 |
| GET `/api/donations/admin/records`、`/:item_id` | 独立管理权限、分页/筛选/历史详情 |

管理权限分别是 `donation_config.read`、`donation_config.write`、`donation_records.read`，同时保留 AdminAuth。所有权不取自浏览器用户参数。

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

## 5. 正常、基础与错误例

- 正常：混合有效、无效、库存、重复、限流项分别记录；只有真实校验且新增的 item 增加永久 User.Quota。
- 基础：同资源/同成功 item 至多一个 Reward；唯一流水、余额增量、已奖励状态与事件同事务提交。缓存只在该首次提交后应用增量，保留既有预扣。
- 错误：已有库存不奖励、不冒认来源；凭据/组失效、禁用、删除或普通日志清理均不清除历史，不恢复资格、不追回已发奖励。
- 不增加每日/累计领奖上限、签到临时桶或奖励有效期。只有单次请求/数值安全边界，不能把它们变成累计业务上限。

## 6. 验证点

- `TestDonationDatabase`：真实 SQLite、MySQL、PostgreSQL；新建与已有 User/TopUp/Checkin 数据升级、重复迁移、两个独立连接、唯一性、回滚、暂停、上界和缓存预扣。
- MySQL 同时覆盖 clientFoundRows 两种语义及 REPEATABLE READ；奖励唯一流水使用 current read，不能被等待锁之前的旧快照隐藏。
- service 测试覆盖真实 TCP 连接断开不透明 POST 重发、丢回执、首次明确拒绝、先前未知结果、身份误绑、JSON 膨胀和静态错误脱敏。
- `TestDonationIntegrationRealServices` 用真实两个应用进程和临时 SQLite、本地 Gemini HTTP，验证真实登录/配置/提交/worker、响应丢失与两端重启、永久奖励和删除后历史；无 binary 时的 skip 不算验收。
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
