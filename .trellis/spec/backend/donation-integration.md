# 捐献集成契约

## 1. 适用范围

修改 `internal/control/donation_*.go`、普通凭据导入与捐献去重的衔接、迁移 `0018_donation_intake` / `0019_donation_manual_review`，或集成路由/配置时阅读。gpt-load 负责指定 key 校验、人工审核裁决、接收和长期回执；new-api 负责用户身份、活动与永久奖励，两者不共享数据库。

## 2. 接口与数据签名

所有接口在 `/integrations/donations/v1`，以 `httproute.OwnerDonation` / `AuthDonation` 注册，由 container 装配，不能放宽 `/api` 管理鉴权。

| 方法 / 相对路径 | 输入 / 行为 |
| --- | --- |
| GET `/capabilities` | 协议版本、持久 instance/source 身份、普通 key 能力及请求边界 |
| GET `/groups` | 只读投影组 ID、名称、渠道、连接类型、enabled/can_probe、目标修订与不可用原因 |
| POST `/batches` | `DonationBatchRequest{batch_id,group_id,target_revision,items:[{item_id,key}]}`，先加密持久暂存 |
| GET `/batches/:batch_id` | `GetDonationBatch`；仅当前来源的逐项回执，查询不重新排队 |
| POST `/batches/:batch_id/retry` | `DonationRetryRequest{item_ids?:[]}`，对原暂存项执行幂等重试动作 |

持久模型在 `internal/storage/models/donation.go`，冻结迁移模型在 `internal/storage/migrations/0018_donation_intake.go`：

- `DonationIdentity`：独立于访问 token 的 instance/source UUID，及指纹密钥校验依据。
- `DonationBatch`：来源 + batch_id 唯一，保存请求摘要与目标快照。
- `DonationItem`：来源 + item_id 唯一，保存加密暂存、租约/重试、接收状态和凭据关联。
- `DonationResource`：规范化完整 key 的 HMAC 为主键，记录首次库存取得/捐献归属；不以 group_id、可见掩码或随机密文去重。
- `DonationRetry`：来源 + 动作幂等键唯一，绑定批次和请求摘要。

这些历史实体没有随组或凭据删除的级联生命周期。

## 3. 请求、响应与配置

- `DONATION_INTEGRATION_TOKEN` 缺省关闭集成。配置时为独立于 `AUTH_KEY` 的 32–256 字节可见 ASCII 秘密，仅用 Bearer 头传递；不能用于普通管理或 AccessKey 流量。
- 成功包装沿用 `{code:0,message,data}`，错误 code 为字符串。new-api 的 `{success,message,data}` 是另一种包装，跨端不能混淆。
- capabilities 的 `protocol_version` 是字符串 `"1"`；instance/source 是持久 UUID，token 或连接 URL 变化不能生成另一份业务身份。
- 初次 POST 的 `Idempotency-Key` 等于 batch_id；重试动作使用新的 UUID v4 幂等键。同标识同语义请求读取原结果，改变内容冲突。
- 单批 100 项、单 key 4096 字节、请求体 1 MiB。边界从 capabilities 提供，是单次请求限制，不是用户累计捐献或领奖上限。
- `target_revision` 不依赖显示名称；目标从分组渠道、验证模型/协议及有效配置解析。符合配置的空组可选，不能用是否有库存 key 判断能否接收。
- `DonationItemResponse` 返回 item_id、state、reason_code、credential_id、accepted_at_ms、retryable。仅 accepted 返回成功凭据引用；响应不含 key、暂存密文或集成 token。
- key 只去首尾空白，保留大小写；逐项格式失败不回滚其他项。请求摘要先以 HMAC 替代 key，不串入明文序列化比较器。

## 4. 验证与错误矩阵

| 条件 | 结果 |
| --- | --- |
| 未配置集成或持久身份/指纹密钥不一致 | 503 `DONATION_UNAVAILABLE`，不回退为匿名或普通管理认证 |
| Bearer 缺失、错误或存在多份 Authorization | 401；秘密响应头禁止缓存 |
| 查询含不支持的参数、JSON/UUID/幂等键不合格 | 标准 400 错误，不回显输入秘密 |
| 报文或批次数超边界 | 标准 413 错误 |
| 目标删除、禁用、类型不支持或自动模式无法解析探测 | 409 `DONATION_TARGET_UNAVAILABLE`，安全 reason_code；人工模式按独立资格判断 |
| 接收时 target_revision 已改变 | 409 `DONATION_TARGET_CHANGED`，不能静默改投其他组 |
| 批次不存在或不属于当前来源 | 404 `DONATION_NOT_FOUND` |
| 同幂等标识内容不同 | 既有幂等冲突错误 |
| key 已在任意组库存或历史成功账本 | 逐项 existing，不冒认原来源、不产生新资格 |
| 上游拒绝 / 超时或限流 | invalid / retry_pending，均不得报告 accepted |

所有 handler 错误通过 `writeServiceError`；网络错误不直接带上游原文。新增可见错误同步三份后端 locale。

## 5. 正常、基础与错误场景

- 正常：一批中有效、无效、重复、已有、限流项独立处理；自动路径须指定 key 探测通过，人工路径须有效批准，两者都在凭据/长期资格事务提交且运行时恢复完成后进入 accepted。
- 基础：`queued → validating → committing → accepted`。探测在数据库事务之外执行，租约持久化；过期 worker 必须通过租约 token 条件拒绝提交。
- 错误：落库后进程中断时保持 committing，恢复既有凭据与运行时，不能重新探测或插入另一条凭据。普通库存导入和捐献取得资格需锁定同一全局资源行。
- 正式写入凭据和恢复发布运行时前，都须在持有 `writeMu` 时先执行 `enforceOperationRecoveryBarrierLocked`，遵守已有控制操作的提交后恢复顺序；仅取得进程锁不能代替恢复屏障。
- 自动探测最多 5 次；可重试失败项可用原标识重新开始一轮，不要求调用方再次提供 key。暂存密文 7 天后可清理，但回执、指纹和来源不能一起清理。
- 删除/禁用凭据、删除组、清理日志或压缩控制操作账本，均不能释放已取得资源的捐献资格。已发奖励的政策由 new-api 执行，接收端不能通过删除历史恢复领奖资格。

## 6. 必需验证点

- `donations_test.go`：暂存不进入调度、混合结果、逐项稳定关联与无明文泄露。
- `donation_probe_integration_test.go`：用本地 Gemini HTTP 上游走生产 probe/runtime，断言实际 key、模型路径及没有库存 key 回退。
- `donation_recovery_test.go` / `donation_concurrency_test.go`：提交中断、租约失效、重试预算、暂存过期、删除后历史、多连接资格争用和管理导入竞态。
- `donation_database_integration_test.go`：真实 MySQL/PostgreSQL 的带库存升级与启动回填、独立连接批次竞争、重试动作重放、资源归属、提交回滚和长期回执；只有数据库 DSN 未配置时跳过。
- `donation_http_test.go`：受限鉴权、禁用模块、来源隔离、严格请求解析、token 轮换和指纹秘密变化。
- opt-in 外部数据库测试必须实际调用新 backfill/接收路径；已有不触及新路径的迁移或控制面测试通过不能替代这些断言。
- 按仓库门禁运行 `make check`，外部数据库按测试 DSN 显式选择。不要把本次本地工具路径或容器名变成项目全局要求。

## 7. 常见错误与正确做法

| 错误做法 | 正确做法 |
| --- | --- |
| 调用管理批量导入后根据新增数量猜测接收人与凭据 ID | 用独立逐项回执与事务内凭据关联；管理导入保持自身契约 |
| 仅用进程锁或“先查不存在再插入”防重 | 用数据库唯一约束、稳定锁顺序和租约条件更新；进程锁只辅助运行时一致性 |
| 用 MySQL `ON DUPLICATE KEY` 的 `RowsAffected == 0` 判定批次/重试动作重放 | storage 强制 `clientFoundRows=true`，未变化的冲突也可能返回 1；使用普通 INSERT，唯一冲突后完整回滚，再用新读核对已提交摘要，不能改全局驱动不变量 |
| 用 `OnConflict{DoNothing:true}` 的 RowsAffected 判断是否新插入 | gpt-load 强制 MySQL clientFoundRows=true，no-op 也可能返回 1；批次/动作使用普通 INSERT，唯一冲突后回滚，再在新读取中验证持久摘要 |
| PostgreSQL 唯一冲突后继续在同一失败事务中读回原行 | 先结束失败事务，再读回原结果；不同内容冲突，同内容重放不再次插入条目或重置重试预算 |
| 将裸 `JOIN groups` 写进查询 | 使用 GORM 子查询或方言安全引用；GROUPS 在 MySQL 8.4 是保留字，反引号也不能直接套到 PostgreSQL |
| 依据组内另一个可用 key、格式合法或前端声明来接受捐献 | 自动路径对暂存的单一 CredentialRef 探测；人工路径绑定明确批准，真实调用只使用原 key；拒绝替换认证材料的配置 |
| token 轮换、资源删除或日志清理时重建指纹身份 | 保留独立持久身份与长期账本；密钥丢失或不匹配时明确拒绝继续 |

## 8. 人工审核与真实调用（迁移 0019）

修改人工模式、审核动作或真实调用时阅读本节；调用方的完整回执与唯一奖励约束见 [donation-caller-contract.md](donation-caller-contract.md)。任务 `09-15-donation-manual-review` 的设计、线协议与审查报告保留验证证据。

### 8.1 模式与摘要兼容

- 统一模式 `auto | manual_review`。auto（缺省或显式）使用冻结的原四字段 comparator，不能仅依赖新 DTO 的 `omitempty` 保证摘要兼容；原 HMAC 域和字段次序不变。调用方向旧接收端发送 auto 时省略模式字段；人工请求使用独立摘要域并绑定模式。
- 同批次 ID 改模式因摘要不同而冲突，不能静默复用首次结果。
- capabilities 新增 `features: ["manual_review_v1"]` 与独立 `review_limits`；原 `protocol_version`、`input_types`、`limits`（含 `staging_retention_seconds=604800`）不变。
- `/groups` 新增 `can_manual_review`、`manual_target_revision`、`manual_unavailable_reason`、`chat_models`；原 `can_probe`/`target_revision`/`unavailable_reason` 语义不变。人工资格只解除「probe 必须可构造」这一依赖，仍要求组启用、单字段普通 `api_key`、目标可编译且认证未被静态覆盖。
- 人工目标修订使用独立 HMAC 域，覆盖组 ID、渠道/连接、resolved target、生效代理与头规则、模型 ID/别名、参数覆盖指纹与超时；**组名等展示字段不影响**。参数覆盖是私有字段的编译结构，序列化会得到 `{}`，必须用 `parameteroverride.Rules.Fingerprint()`。

### 8.2 状态与动作

- 合格人工条目进入 `pending_review`（初始 `item_revision=1`），只加密暂存并预留未取得的 resource owner，不创建正式凭据、不发奖；库存已有则 existing，其他未决 owner 用 `pending_review/resource_busy` 表示。legacy auto 条目 `item_revision` 保持 0。
- 自动队列不选 `pending_review`；`ReconcileDonationReviews` 用独立分钟 ticker、有界批次处理过期、失效测试租约、库存抢先和 owner 释放，不被慢自动 probe 阻塞，也不发上游请求。
- `DonationReviewAction` 以 source + action_id 唯一，`action_id` 同时是调用方 `Idempotency-Key`。顺序固定为：身份 → 动作幂等查重（比对 `RequestDigest`）→ 条目行锁 → revision 校验。
- **确定的业务拒绝也落账**（`outcome=rejected` + 闭集 reason_code）；鉴权、非法输入与基础设施失败不落账。`outcome=rejected` 表示命令未应用，不表示捐献项被拒收。
- 拒绝自由文本只存 `note`，`items.reason_code` 保持闭集；`approve`/`reject` 与运行中的测试租约互斥。
- `enter_review` 首版只接受原有效模式 auto、`retry_pending/retry_exhausted`、暂存仍在、未 acquired 的项；原批次、请求摘要、自动 Attempts、捐献人与 7 天截止全部不变，只改有效模式/状态与单调 revision。
- review-context 的 `effective_mode` 始终是实际条目模式；`review_action=enter_review|approve` 表示主动作，独立 `can_reject` 表示拒绝能力。目标变化/删除/禁用不阻断仍有效待审项的拒绝，无文本模型只禁止测试。
- `enter_review` 和 `approve` 都要求管理员所见的 `review_target_revision`；`reject` 不携带目标。new-api 为审核和测试填写 `actor=user:<id>`，接收端校验 1–128 字节可见 ASCII 并纳入幂等摘要。
- 批准先执行控制恢复屏障，再以稳定锁序验证 owner/库存、原截止、测试互斥和目标。批准、接收和动作应用使用同一毫秒时间；`committing → accepted` 及测试开始/结束/中断均推进人工 item revision。
- 命令 `kind=approve|reject` 与条目 `review_decision=approved|rejected` 是不同闭集；批次回执包含冻结模式、转审 `entry_action_id` 和完整决定事实。批准时库存已先取得可返回 `existing/already_exists` + `approved`，必须绑定原动作且不发新奖励。

人工接口仍在 `/integrations/donations/v1`：GET `/batches/:batch_id/items/:item_id/review-context`；POST 同项 `/review-actions` 或 `/tests`；GET `/review-actions/:action_id` 或 `/tests/:test_id`。接收端请求体的 action_id/test_id 必须等于幂等头，不能把这一形状直接用于 new-api 的浏览器接口。

### 8.3 真实调用

- 统一 `OpenAICompletions + OperationChatCompletion`，`POST /v1/chat/completions`；由原 `ResolvedTarget.ModeForModel` 决定 native/converted，不新建 provider 路由。
- 从原条目的加密暂存解密并规范化 key，构造唯一 `CredentialSnapshot`。虚拟 ID 通过 `donationVirtualCredentialID` 映射到 uint 上半区：auto 为 `MaxUint-2*id`（奇数），manual 为该值加一（偶数），输入仅 `1..MaxUint/4`。执行前检查正式库存没有占用上半区，结束回收缓存；不得走调度器、其他 key/组或二次 fallback。
- 组 `ParameterOverrides` 仍按纯域规则应用，覆盖后复检请求大小、tokens、纯文字消息、工具/附件及多份 completion。参数指纹对 JSON Pointer 的段数和各段编码，不能把 `/a~1b` 与 `/a/b` 合并成同一指纹。
- `test_id` 在发上游前持久占用；同 ID 重放只读元数据，不发第二次请求。响应正文不持久化。
- 不得裸传 `StreamEvent.Data`、原始 JSON、上游 headers 或错误体。只取白名单文本，在完整缓冲中确定秘密匹配边界后输出，保留跨片段匹配尾部并维持 UTF-8 边界；固定 holdback 不能截断已经发现的完整匹配。非流式走同一规则。
- 只有协议完整结束、有有效可见输出且结果已持久化才 `succeeded`；部分输出后错误/取消/断流都是非成功，且不得宣称 key 不可用。
- 终态用短 cleanup context 写入并释放租约；进程中断后由冷对账收敛为 `interrupted`，**绝不后台重测**。
- 新测试须先取得进程槽再持久占用，满载立即 busy；整个请求与下游写入受总计/首包/idle/暂存截止约束。准备成功后才能提交 SSE 200；重放返回 JSON 元数据，包含 running 时也不能再发模型请求。
- `meta → delta* → done` 绑定 test/batch/item/model/start revision/目标/时间；终态持久失败不能发送 succeeded。可得 usage 保存后回读，普通日志及业务历史不保存提示词或回复正文。

### 8.4 该特性的必需验证点

- `donation_manual_review_test.go`：能力协商与 auto 摘要冻结、人工暂存不发 probe/不取库存、批准恰好一次入库、拒绝清密文且不建凭据、revision/目标变化被拒、旧 `retry_exhausted` 转审保留历史、过期不可通过、只调用本条目 key 且重放不发上游、无聊天模型时不上上游、跨帧回显 key 脱敏、租约阻塞审核且废弃租约不重试。
- `0019_donation_manual_review_test.go`：含真实旧捐献数据的升级、状态 CHECK 扩集（仍拒绝未知状态）、重复迁移幂等、冻结的 `Validate0018` 升级后仍通过。
- `donation_review_regression_test.go`：屏障故障、运行中拒绝、结果持久失败、终态时间/revision、取消/超时、槽限额、冷队列、资源归属和虚拟 ID 边界。
- `donation_test_integration_test.go`：OpenAI native 与 Gemini converted 的真实本地 HTTP，分别覆盖 stream/nonstream、原 key/模型/提示词、库存不接替、脱敏及重放不重测。Gemini 合成流使用真实 SDK 契约的独立 STOP + usage 尾帧，不能为错误 fixture 放宽成功条件。
- `donation_manual_database_integration_test.go`：真实 MySQL/PostgreSQL 的 0018 历史升级、重复迁移、双连接相同动作/不同项测试并行、唯一约束和动作时间绑定。2026-09-15 已验证 SQLite、MySQL 8.4.11、PostgreSQL 15.19；证据见本任务 `research/codex-intake-review.md`，不据此宣称其他版本已实测。2026-09-20 的迁移重编号（`0015/0016` → `0018/0019`）另经三库升级演练，证据见 `09-20-merge-upstream-main/research/migration-upgrade-rehearsal.md`。

### 8.5 判断示例与防回归

| 条件 | 必须结果 |
| --- | --- |
| 没有可构造的 probe，但人工目标合法 | 可待审/批准；无文本模型仍不可测试 |
| 合法 approve，正式接收完成 | `review_decision=approved`，同动作时间，accepted 后才可发奖 |
| 合法 approve，但库存已先取得 | existing + approved，记录动作但不奖励 |
| 目标改变或下线 | 停止测试/通过；尚有效待审项仍可拒绝 |
| 原截止已到、测试占用或 owner 不属于本项 | 拒绝冲突动作，不续期、不换 key |
| SSE 只收到部分文本、没有完整终态或持久化失败 | 非成功，不自动重测 |

错误：用 `MaxUint/2-attemptID` 假设不碰正式 ID，或将 `approved` 直接当作 action.kind 查询。正确：使用有界奇偶映射，并显式映射决定与命令；用真实接收端字面值验证回执。
