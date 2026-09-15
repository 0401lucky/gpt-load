# 捐献人工审核与真实调用设计

本文为本任务实施设计。产品要求以 [prd.md](prd.md) 为准；接口命名及边界以本文为准，`research/` 中的候选命名不构成另一份契约。用户指定 DeepSeek 实施、Codex 复核并保留任务；本文件已同步复核确认的字段细节，验收状态见 [Codex 复核记录](research/codex-review-progress.md)。

## 1. 范围与系统边界

本任务交付一个跨仓库完整流程，使用同一任务协调三个实现边界：gpt-load 接收/执行、new-api 审核/奖励、new-api 管理 UI。三者共同依赖本协议，暂不拆独立产品子任务。

| 系统 | 持有的事实与职责 |
| --- | --- |
| new-api | 用户/管理员身份与权限、活动和批次冻结规则、审核动作意图及远端确认、测试元数据、唯一永久奖励与用户页面 |
| gpt-load | 原分组与有效执行配置、唯一暂存 key、资源去重与 owner、动作裁决、指定 key 的模型调用、正式凭据及运行时恢复 |
| 浏览器 | 操作已授权捐献项，显示状态与脱敏输出；不持有捐献 key/集成 token，不声明自己已通过测试或审核 |

调用方向仍为 `管理员浏览器 → new-api → gpt-load → 原分组上游`。测试不进入 new-api 普通 Relay/Playground、用户钱包或 gpt-load 的组内选 key 路由。

## 2. 模式与兼容性

统一模式枚举为 `auto | manual_review`。活动表单用一个开关表示，默认 auto；人工模式必经人工审核，没有“跳过但自动奖励”的组合。

- gpt-load 保留 `/integrations/donations/v1`、`protocol_version: "1"`、原 input_types 和所有原 limits；新增 `features: ["manual_review_v1"]` 及独立 `review_limits`。这一 feature 代表本文的人工接收、转审、动作查询、版本化回执及测试完整契约。
- 原 `can_probe/target_revision/unavailable_reason` 含义和算法不变；目录新增 `can_manual_review/manual_target_revision/manual_unavailable_reason`。聊天模型在逐项 review-context 查询，不能把不可聊天误判为不可人工审核。
- 人工资格保留组启用、普通单字段 api_key、渠道/目标可编译及认证不被覆盖的检查，只解除 probe 必须可构造的依赖。静态替换/删除认证头仍不允许。
- 人工目标修订使用独立 HMAC 域，包含组 ID、渠道/连接、resolved target、有效代理/头规则、模型 ID 与别名、有效参数覆盖及超时；组名等展示字段不影响。不能直接序列化私有字段的编译 Rules 得到 `{}`；从有效配置源形成确定性签名。
- gpt-load 的 auto 批次摘要使用与旧 DTO 完全相同的冻结 comparator 及原 HMAC 域。缺省或显式 auto 归入旧表示；人工请求显式带 `validation_mode: "manual_review"`，使用包含模式的新 comparator/域。同批次 ID 改模式为幂等冲突。
- new-api 向旧接收端发送 auto 请求时不加字段；自己的 ParseLines 原请求摘要保持不变。已有请求键先匹配原冻结 batch，不根据修改后的活动重建发送内容。
- 新能力缺失时禁止启用或提交人工模式；不降级、不假报成功。关闭原活动仍可在远端离线时进行；更改模式/目标要核对真实能力和对应修订。

旧客户端忽略新增响应字段，但不识别人工状态；新能力启用后不得用旧二进制或混合版本处理同一套人工记录。

## 3. 状态与持久事实

| 路径 | 状态及副作用 |
| --- | --- |
| 自动模式 | 原 `queued → validating → committing → accepted` 及失败/重试行为不变 |
| 人工提交 | 格式、资格及去重后 `pending_review`，仅加密暂存，不进入正式 CredentialRegistry |
| 通过 | `pending_review → committing → accepted`；凭据、首次取得与批准事实同事务，运行时恢复后才 accepted |
| 拒绝 | `pending_review → rejected`；清暂存，释放尚未 acquired 且仍属于原项的 owner，保留历史 |
| 过期 | `pending_review → invalid/staging_expired`；UI 显示暂存过期，不声称 key 无效 |
| 竞争库存已取得 | `existing/already_exists`；无新奖励，不改写原来源 |

`pending_review/resource_busy` 可表示其他未决 owner 尚未释放；不可测试/通过，不提前宣称 already_exists。后台有限检查或下一次管理操作重新判断资格。人工状态绝不送入自动 probe，不消耗或重置自动 Attempts。

最小持久结构：

| 层次 | 字段/实体及约束 |
| --- | --- |
| 两端 Batch | 冻结 `validation_mode`，保留原请求摘要、原 `target_revision`、时间/用户/奖励；历史转审不修改它们 |
| 两端 Item | 有效模式、远端单调 `item_revision`、人工 `review_target_revision`、转审 `entry_action_id`、最终 `review_action_id`、暂存截止；不混用原 Resource.Generation |
| gpt-load ReviewAction | source + action_id 唯一；batch/item、kind、操作者标识、expected revision、目标、备注、请求 comparator、稳定应用结果及生效 revision/时间 |
| new-api ReviewAction | action UUID 在本地全局唯一，绑定 actor 与原意图，同 UUID 发往远端；跨 actor 复用冲突，不新增另一层 request_key；`pending/applied/rejected` 是恢复用业务台账 |
| 两端 TestAttempt | test UUID、捐献项、操作者、开始时版本/目标、模型、受控输入摘要、时间/结果/安全原因；不存完整 key 或对话正文 |
| gpt-load 测试租约 | 每项最多一个运行测试，token/截止持久化；批准/拒绝与活动测试互斥，网络期间不持 DB 锁 |

审核 decision/actor/time 从动作投影，避免多个可独立修改的“已批准”真相。命令 kind 的 approve/reject 映射为条目决定 approved/rejected，不能共用同一枚举；reviewed_at_ms 与该动作 applied_at_ms 完全相同。test UUID 同样在 new-api 本地全局唯一并绑定 actor。所有新业务历史遵守原来不随组、凭据、普通日志删除的规则，并纳入秘密丢失检测和备份。

新版接收端的所有可见 item 状态、模式、原因、审核/测试状态变化产生新 revision；进入版本化路径后同 revision 必须表示同事实。new-api 对已经观察过正 revision 或有人工事实的项忽略低版本，拒绝同版本矛盾，不能被 0/缺省覆盖。对于确认从未进入人工且一直处于 legacy auto 的项，旧接收端每次都缺版本（按 0 读取），必须沿原状态单调规则允许 queued(0) → validating(0) → accepted(0)，不能套用严格等版本比较。已 accepted/existing 和奖励的单调性继续保留。

## 4. API 契约

全部路由继续沿各项目现有认证和响应包装。JSON 使用严格字段白名单；actor、实际组/上游、key、收款人和金额从服务端记录获得。动作 UUID 与 Idempotency-Key 使用已有校验规则。

| new-api 路径（前缀 `/api/donations/admin`） | 行为 |
| --- | --- |
| 既有 POST/PATCH `/campaigns[/id]` | 新增模式字段；POST 缺省 auto，PATCH 缺省保留，显式 null/未知值拒绝 |
| GET `/records/:item_id/review-context` | 返回真实 effective_mode、原目标、版本、截止、review_action、独立 can_review/can_reject/can_test、文本模型及限额；不返回配置秘密 |
| POST `/records/:item_id/review-actions` | `{kind, expected_item_revision, review_target_revision?, note?}`；kind=`enter_review/approve/reject`，动作 UUID 只放幂等头 |
| GET `/records/:item_id/review-actions/:action_id` | 查询该动作的原意图、确认状态及结果；读取不重发模型测试 |
| POST `/records/:item_id/tests` | `{expected_item_revision, review_target_revision, model, prompt, system_prompt?, max_output_tokens?, stream?}`；必需 test 幂等键 |
| GET `/records/:item_id/tests/:test_id` | 元数据/结果查询，不恢复正文、不重新调用上游 |

gpt-load 对应路径为：

- GET `/batches/:batch_id/items/:item_id/review-context`。
- POST `/batches/:batch_id/items/:item_id/review-actions`，由 new-api 附加认证得到的 actor 标识，目标以原 item 的 GroupID 为准。
- GET `/review-actions/:action_id`。
- POST `/batches/:batch_id/items/:item_id/tests`、GET `/tests/:test_id`。

仍以 source 身份隔离所有查询。动作结果返回 action_id、batch/item、kind、原 expected revision/目标、`outcome=applied|rejected`、reason、effect revision 和时间，供调用方与本地原意图核对。这里 outcome=rejected 是“命令未应用”，不是捐献项被拒收。合法请求的确定业务拒绝也持久化；鉴权、非法输入或基础设施失败不伪造业务终态。

new-api 的审核/测试 JSON 不接受 action_id/test_id/actor；前两者来自 Idempotency-Key，actor 来自认证 user ID。发往 gpt-load 时才由服务端补全对应 UUID 与 `actor=user:<id>`。enter_review 与 approve 都要求管理员所见的人工 target revision，reject 不携带目标。上下文仍为 auto 的旧项用 review_action=enter_review 表示转审；目标改变、下线或删除时仍可单独 can_reject。

批次回执保留原 `target_revision` 和完整 items 集合；新增 batch.validation_mode。item 增加 `item_revision/effective_mode/review_target_revision/entry_action_id/review_action_id/review_decision/reviewed_at_ms/staging_expires_at_ms`，人工 accepted 必须携带最终 approve 事实。自动分支可省略无关审核字段，原凭据/接收时间语义不变。批次顶层仍为 processing/completed；pending_review 属未完成，但前端和后台根据独立计数判断是否需快速处理。

approve 时库存已抢先取得可形成 existing/already_exists + approved。调用方在完整绑定原 approve 后接收它，使用该终态实际 revision 确认原 pending 动作，不产生奖励或本项首次取得事实；不能要求所有 applied 动作一律只推进一次 revision。

new-api 的既有本人 GET `/api/donations/batches[/id]` 在 item 投影增加安全 `review_note`：只从该项已 applied 的最终 reject 动作取得供本人查看的拒绝原因；静态 reason_code 继续使用闭集，不能装入自由文本。批准备注和其他测试/审核细节仍在有权管理员记录中，普通用户不因此获得管理动作接口权限。

权限新增 `donation_records.review` 与 `donation_records.test`，结合 AdminAuth 及 records.read；只读记录管理员、普通用户不能获得写能力。上下文读取允许有读权的管理员查看安全状态；只有 test 权限能调用，只有 review 权限能转审/通过/拒绝。

## 5. 审核、接收与奖励的可靠顺序

1. new-api 读取/对账原捐献项，验证权限、owner/generation、版本及动作参数。在事务中保存 pending 动作和原语义，同时唤醒该批次恢复。测试成功不是动作授权证明。
2. 以同一个 action UUID 查询/提交给 gpt-load。接收端顺序为：身份和输入 → 查已有动作并核对 comparator → 无旧动作才校验 expected revision/状态。重放不会被自己的成功更新拒绝。
3. 通过时持 `writeMu` 并遵守现有控制恢复屏障；事务内按既有稳定锁序锁 item/resource，复核有效人工模式、版本、原截止、测试互斥、目标和库存。复用窄的正式接收 helper，不向 probe 伪造 passed。
4. 同事务写审核事实、正式 Credential、DonationResource 首次取得、item.committing；事务外复用 recoverDonationItem。运行时发布完成才 accepted，失败继续按原 committing 恢复。
5. new-api 校验同动作查询及批次事实后，原子确认 action 并应用回执。accepted 先于动作查询到达时，只有包含足够信息且完整匹配本地 approve intent 才可一并确认；否则先查询原动作，不能把唯一已接收项永久卡住。
6. CreditDonationReward 保留原唯一奖励、用户状态、金额上界与缓存行为，独立增加人工门槛：已 applied 的同 item/batch/目标 approve action + 匹配的 accepted 回执。原 manual batch 或已显式转审项即使字段损坏，也不能回退 auto 发奖。

timeout、404、代理错误和普通 HTTP 409 不证明操作未执行。new-api 不释放 owner、不另开相反决定，使用原 action ID 对账/重放。浏览器关闭不撤销已持久化的审核意图；永久奖励不依赖远端当前在线、当前活动开启或凭据仍存在。

每个业务动作普通 INSERT，唯一冲突需退出失败事务再读原结果，不用 MySQL upsert RowsAffected 判定是否首次。两个系统分别遵守自身锁序和事务重试，不持跨网络数据库事务。

## 6. 旧记录转审与目标生命周期

首版 enter_review 只接受原有效模式为 auto、远端确认 `retry_pending/retry_exhausted`、仍有原暂存材料、未 acquired、无活动 probe/test 的项。未确认批次先恢复原接收，不因活动已改人工而改变原 POST。

转审作为独立项动作：记录 entry_action_id 和当前同组 manual_target_revision，将有效模式置 manual_review、state 置 pending_review，增加 revision。保留原批次、原请求、原创建/截止、自动 Attempts、用户、奖励和 new-api owner/generation。与 user retry/旧 worker 冲突时，行锁及 lease fencing 决定结果；旧 probe 迟到不能提交。

人工目标在提交或转审时冻结。之后目标变化禁止测试/通过，旧测试显示过期；首版没有 refresh-target 或换组动作，允许恢复原配置或拒绝后重新提交。拒绝不要求目标在线。旧自动模式继续按原来同组当前配置重试，不因新设计而收紧。

截止仍为首次暂存起 7 天，转审、测试、刷新或重试不续期。批准事务在截止前已提交 committing 时，截止不撤销取得；否则即使清理 worker 尚未运行也拒绝批准。过期/拒绝保留历史，仅释放未取得资源；已取得资源永久不释放领奖资格。

## 7. 真实调用执行与边界

使用统一 `OpenAICompletions + OperationChatCompletion`，受控 POST `/v1/chat/completions`；通过原 ResolvedTarget.ModeForModel 选择支持的 native/converted 执行。现有 openai_compatible 原生路径覆盖 cline，Gemini 使用已有转换路径。模型目录仅列原组支持的文本模型；无兼容模型明确禁用测试，仍可独立人工核实。

gpt-load 从原暂存解密并规范化 key，复用代理、组 header rules 和凭据准备，构造唯一 CredentialSnapshot，调用一次 ExecuteStream 或 Execute。测试的虚拟凭据 ID/代次必须与正式 ID、自动 probe 及其他测试隔离，结束回收缓存；实现需用边界测试证明不碰撞。绝不走调度器、其他 key、其他组或第二次 fallback。

普通组 ParameterOverrides 要按现有纯域规则应用，不能因为直接调用 executor 而遗漏；覆盖后重新验证请求大小、文本请求形态及 token 边界。超范围工具/附件/外部动作配置明确报不兼容，不静默绕过组配置。control 不 import gateway。

单次技术限额通过 review_limits 返回，前后端一致执行：

| 项 | 首版边界 |
| --- | --- |
| prompt + system_prompt | UTF-8 合计 16 KiB，用户 prompt 不能为空 |
| 完整请求体 | 64 KiB，按实际序列化字节复核 |
| 输出 tokens | 默认 1024，可选 1–4096；覆盖后同样有界 |
| 响应文本 | 累计 128 KiB，单个协议事件 64 KiB；超限结束为非成功 |
| 时限 | 总计最多 120 秒、首包 30 秒、idle 20 秒，取原组更严格约束及暂存剩余时间 |
| 执行互斥 | 每项一个持久测试租约（最多 150 秒），每 gpt-load 进程最多 4 个并行测试；不限制累计捐献/奖励数量 |
| 审核备注 | UTF-8 最多 2 KiB，拒绝需非空，文本展示且普通日志不记录输入正文 |

客户端普通 12 秒 JSON 方法不能用于长测试；新增沿用原连接信任约束的受限流式/非流式方法，关闭透明 POST 重发和自动重连。总超时、下游写截止、context 及 body close 全链传播；短 cleanup context 写测试结果/释放租约，崩溃后按失效租约结束为 interrupted，绝不后台重测。

test_id 在发上游前持久占用；同 ID 重放返回原元数据，不重新消费上游。正文不持久保存，网络丢失不能重放已丢正文；管理员要重测时显式用新 ID。第三方可能已计费，不能把取消宣称为撤销上游用量。

### 输出契约

- 非流式返回脱敏文本和终态元数据；流式由 gpt-load 重新封装 `meta/delta/done` 事件，new-api 校验后转发。
- meta 绑定 test/item、开始 revision、模型、人工目标和时间；delta 只含允许的正文/推理文本；done 含 `succeeded/failed/cancelled/interrupted`、受控 reason、状态码、时间与可得的用量。只有协议完整结束、有效可见输出且结果已持久化才 succeeded。
- 不裸传 StreamEvent.Data、原始 JSON、上游 headers 或原始错误 body。增量解码后进行跨网络块、SSE 和文本 delta 的已知秘密脱敏；仅逐 chunk ReplaceAll 不合格。非流式使用同一脱敏/字段白名单。
- 部分文字后断流/失败/取消保留非成功标记，不宣称 key 不可用。HTML/脚本和工具调用仅当不可信数据处理，不执行。
- 主库只留测试元数据及安全原因，不留完整提示词/回复。响应仅在当前管理员详情显示；切换 item、账号、关闭详情或退出取消请求并清空正文。

## 8. 后台恢复与界面

new-api 复用 needs_recovery/next_poll/lease 字段：含自动处理、pending 审核动作或待发奖励的批次留在热恢复；仅 pending_review 的批次进入独立小配额冷对账，建议每分钟调度、每批约 5 分钟查询一次。冷对账只 GET，不发测试；领取时复核队列条件，逐条临开工取租约，避免一批 32 个慢任务耗尽后续租约。

准备审核/转审动作立即唤醒热恢复；release 要按最新状态重算，不能用旧冷任务把新批准延迟。接收端对待审截止、resource_busy、库存先取得和测试租约进行有限清理，均不 probe。两端终态、Summary.PendingReview/Rejected 与用户页面轮询保持一致。

UI 复用已有 record-detail、ConfirmDialog、DataTable、Switch、Combobox 和会话守卫；具体证据见 [UI 研究](research/new-api-ui-and-conventions.md)。审核确认显示受益用户、key 掩码和固定奖励；有权管理员可在未取得成功测试时确认通过。调用成功与人工决定分别呈现。只有待审核时停快速轮询，打开详情及操作后刷新。

管理详情提供 pending_review_action、latest_test 和各最近 20 条 recent_review_actions/recent_tests。只读管理员可看安全元数据，不能提交审核/测试；完整提示词与回复不进入历史。未知动作冻结原 UUID/actor/内容，从同动作可信终态恢复，不能按空 pending 字段猜测成功。

## 9. 迁移、验证与回退

- gpt-load 新增下一编号冻结迁移，扩充状态 CHECK、模式/revision 字段及动作/测试结构；保持 0015 冻结内容，注册正常/当前/中断恢复校验。升级已有历史表的恢复验证不能使用“必须空表”的旧规则。
- new-api 在 MigrateDonations 中增量演进，显式处理旧 NULL/空模式到 auto；未知模式拒绝，已有人工事实不可兼容回退。旧 CampaignRevision JSON、原摘要、指纹秘密和奖励唯一约束不重写。
- 两端均验证含真实旧捐献表/历史的升级和重复迁移；当前 new-api upgrade fixture 仅建 User/TopUp/Checkin 不足，需补冻结旧捐献 schema。
- 重点验证三数据库、批准/拒绝/过期/测试/旧 retry 竞态、响应丢失、双进程恢复、完整回执绑定、流式边界与真实本地 HTTP 唯一 key。命令和顺序见 [implement.md](implement.md)。
- 实现不新增依赖、不改通用认证/钱包语义。权限/凭据边界实施前核对 new-api AGENTS 要求的适用 OWASP 官方指引，记录检查，不能用设计文字宣称已验证合规。
- 将来发布先接收端、再调用方、最后开人工活动；本任务授权只含本地实现/验证。回退先关闭新人工受理，保留新版处理已有人工记录与奖励；不能清账本、重生身份、降为自动或回退已发奖励。

## 10. 审阅结论边界

用户已确认人工审核及管理员专用入口。旧失败项转审、原 7 天期限、单次文字调用与兼容恢复已随最终摘要呈现；用户随后指定 DeepSeek 实施并交 Codex 审查，不归档。2026-09-15 Codex 已修复复核发现并完成两服务真实进程、数据库及构建门禁；浏览器验收被访问策略阻断，任务保持 in_progress。完整证据及尚未验证范围以复核记录为准，不声称生产上游可用或生产迁移已完成。
