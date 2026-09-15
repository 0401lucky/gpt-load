# Research: gpt-load 人工审核接收与指定暂存 key 的真实调用设计

- Query: 在保持既有捐献身份、全局去重、数据库租约和运行时恢复契约的前提下，如何最小可靠地增加人工审核模式、逐项审核、正常聊天测试及历史自动失败项转审。
- Scope: internal；只读 gpt-load 源码及当前 PRD、首轮研究、相关后端 spec。调用方兼容事实由同任务 `rewards_design_research` 研究者核对并同步。
- Date: 2026-09-15

## Findings

### 1. 推荐结论

1. 人工模式应是独立接收策略，不能给现有 probe 返回伪造的 passed。增加明确的 `pending_review`、`rejected` 状态；批准后复用既有 `committing → accepted` 路径。过期继续用 `invalid/staging_expired` 即可，不必再增一个接收终态。
2. 正式凭据、全局资源首次取得、审核决定与 `committing` 必须同事务写入。运行时恢复成功前仍不返回 accepted；真实聊天测试从不入库、从不发奖、从不自动批准。
3. 批次的提交模式、原 request digest、原 `target_revision` 不可变。历史自动失败项转审是独立的逐项动作，另存 effective mode、人工目标修订、动作关联和单调 `item_revision`。
4. 聊天测试直接构造一个普通 `OperationChatCompletion` 的 `AttemptSpec`，绑定暂存 key 的独立 `CredentialSnapshot`，调用现有 `Executor.ExecuteStream`。不经过 gateway、组内 key 选择器、new-api 普通 Relay 或多组重试。
5. 不能裸转发 `StreamEvent.Data`。正常成功内容不保证已脱敏，必须由捐献出口解析允许的文本字段、跨分片脱敏、限制大小，并把取消传到上游。

### 2. 主要代码证据与文件地图

| 文件 | 已核实职责 / 约束 |
| --- | --- |
| `internal/storage/models/donation.go:15` | batch 保存原请求摘要与目标快照；`:29` 是暂存及永久逐项回执；`:40` 有严格状态 CHECK；`:55` 是全局资源 owner/首次取得账本。 |
| `internal/storage/migrations/0015_donation_intake.go:39` | 已发布的冻结模型具有同一状态 CHECK，新增状态需要后续迁移，不能修改该冻结定义替代升级。 |
| `internal/control/donation_catalog.go:22` | 单批 100、key 4096 字节、body 1 MiB、自动 5 次、暂存 7 天、probe 30 秒、租约 90 秒。 |
| `internal/control/donation_catalog.go:146` | 当前资格包含 enabled、普通单字段 api_key、编译目标、probe 可用性与认证头规则检查。 |
| `internal/control/donation_batches.go:57` | 请求整体 JSON（其中 key 先 HMAC）参与 batch digest；新增 JSON 字段会影响历史摘要。 |
| `internal/control/donation_batches.go:94` | Receive 只承诺加密暂存已持久化；网络 probe 由应用生命周期 worker 负责。 |
| `internal/control/donation_batches.go:177` | 格式、规范化、加密、全 key 指纹、7 天截止；当前合格项进入 queued。 |
| `internal/control/donation_batches.go:207` | 回执投影有 processing 状态白名单；只有 accepted 暴露凭据 ID 与时间。 |
| `internal/control/donation_batches.go:242` | 自动 retry 动作持久幂等；同一动作重放不能重复重置自动预算。 |
| `internal/control/donation_worker.go:25` | 工作扫描只处理原状态白名单；新增待审过期、测试租约恢复要显式加入。 |
| `internal/control/donation_worker.go:88` | 先锁 item，再锁 resource；判断已取得/库存/过期/预算/owner，然后创建 probe 租约。 |
| `internal/control/donation_worker.go:203` | 写锁及恢复屏障后，检查 state + lease token，再检查 resource、目标和库存，最后事务提交凭据。 |
| `internal/control/donation_worker.go:301` | committing 恢复不重新 probe、不重新插入凭据；先恢复运行时，再核对首次取得事实并 accepted。 |
| `internal/control/donation_resources.go:27` | 全局资源唯一插入与行锁供捐献和普通管理导入共用，进程锁不是去重屏障。 |
| `internal/control/donation_resources.go:61` | 管理导入可优先取得尚未 acquired 的资源，即使存在暂存 owner，也不得冒认捐献来源。 |
| `internal/control/credential_probe.go:130` | 现有暂存执行准备方式：解密、规范化、指定凭据、解析代理、应用组请求头、构造并验证 AttemptSpec。 |
| `internal/control/validation.go:398` | 旧 probe signature 包含组 ID、渠道、目标、选定 probe 协议/模型、代理和请求头；不包含普通请求参数覆盖。 |
| `internal/execution/contracts.go:168` | CredentialSnapshot 私有保存 key，JSON 只包含身份元数据；`:569` 约定执行 exactly selected attempt。 |
| `internal/execution/bifrost/executor.go:179` | 现有普通流式执行，包含父 context、首包与 idle deadline、同步 sink 及终态错误。 |
| `internal/execution/bifrost/executor.go:938` | probe 禁止流式、body、自定义 path；普通 OpenAI chat 要求 POST `/v1/chat/completions`。 |
| `internal/gateway/handler.go:865` | 普通流量会执行组 ParameterOverrides，直接调用 executor 不会自动补做这一层。 |
| `internal/platform/redact/redactor.go:63` | 可复用的已知秘密及通用字符串脱敏，但它本身不是流式跨片段处理器。 |
| `internal/control/http_routes.go:20` | 新接口应留在 OwnerDonation/AuthDonation 模块；不可放宽 `/api` 管理鉴权。 |

### 3. 接收状态与全局资源 owner

推荐状态机：

| 模式 / 触发 | 状态 | 凭据、奖励、暂存 |
| --- | --- | --- |
| 原自动模式 | `queued → validating → committing → accepted` | 保持原行为。 |
| 人工模式格式及资格合格 | `pending_review` | key 加密暂存；不进入 CredentialRegistry/调度；无奖励。 |
| 人工批准事务成功 | `pending_review → committing` | 正式凭据、首次取得账本、批准事实已同事务写入；尚不发 accepted 回执。 |
| committing 运行时恢复成功 | `committing → accepted` | 回执含正式凭据及取得时间，并绑定同一批准动作。 |
| 人工拒绝 | `pending_review → rejected` | 清空暂存，仅释放未 acquired 且仍归本 item 的 owner；保留指纹、batch、动作和历史。 |
| 暂存截止 | `pending_review → invalid/staging_expired` | 同样仅清暂存及未取得 owner；拒绝新测试、批准或转审。 |
| 库存或历史已取得 | `existing/already_exists` | 不入新凭据、不发新奖励；不能清除原来源。 |

建议直接在人工批次受理事务中完成本地资格检查并生成 `pending_review`，而不是把人工项当普通 queued 留给旧 probe 分支。需抽取“本地资格检查及资源占用”逻辑，复用 `claimDonationItem` 的全局资源和库存判断；**不要调用 probe**。批量资源锁按 fingerprint 排序，保留用户的 Position，不去锁其他 owner 的 item 行，避免与普通导入形成反向锁顺序。

同批重复保持现有 `existing/duplicate_item`。跨批另一个 owner 尚未取得时不能说 already_exists：保存 `pending_review/resource_busy`，测试/批准暂不可用；后台有限扫描或下一次管理动作重新检查 owner。只有当 resource 已取得后才能变成 existing。待审 owner 可保持至批准、拒绝或原 7 天截止；日常等待不持有 DB 事务或 `writeMu`。

后台必须有显式人工分支：处理待审过期、已被库存取得、resource_busy 重新评估及已失效测试租约；绝不能把这些项送入自动 probe，不能消耗/重置原自动 Attempts。`RetryDonationBatch` 只重试仍处于自动 effective mode 的项，转审后调用旧 retry 不得重新变成 queued。

**正式提交复用点**：抽出 `completeDonationProbe` 中 `donation_worker.go:246` 起的库存复查、Credential 创建、`BuildGroupCredentialEntriesWithProxy`/`ValidateCredentialEntries`、资源取得及 committing 更新为窄 helper。自动调用者须先有真实 passed + 当前租约，人工调用者须先有合法批准动作；helper 不接收“假 passed”。

人工批准仍必须：

1. 持有 `writeMu` 后调用 `enforceOperationRecoveryBarrierLocked(ctx, 0)`。
2. 写事务内锁本 item，再锁全局资源，复核 pending、版本、有效模式、未过期、没有活动测试、目标和库存。
3. 同事务写批准动作结果、正式 Credential、DonationResource 首次取得、item.committing 及最终批准 action ID；任何失败全部回滚。
4. 事务结束后调用 `recoverDonationItem`；失败留在 committing，应用恢复循环继续，不能再次审核/探测/插凭据。

对应证据：`donation_worker.go:203`、`:259`、`:274`、`:279`、`:301`；运行时发布真正包含 credential registry、quota observations 和 config manager，见 `internal/control/service.go:503`。取得进程锁不能替代恢复屏障，见 `internal/control/operation_recovery.go:69`。

### 4. 最小持久契约及动作幂等

以下是推荐字段含义，最终命名应由两端共同固定：

| 层次 | 必须保存 / 返回的事实 |
| --- | --- |
| Batch | `validation_mode`（缺省 `auto`）、原 request digest、原 `target_revision`；后两项原样保留。 |
| Item | 单调 `item_revision`、`effective_mode`、`review_origin`（campaign / admin_override）、`review_target_revision`、`staging_expires_at_ms`。 |
| 人工来源关联 | 历史转审的 `entry_action_id`；最终 approve/reject 的 `review_action_id`、decision、decided_at_ms；这两个 action ID 不应混用。 |
| 审核动作账本 | source + action_id 唯一；batch/item、kind、expected item revision、expected review target、操作者受控标识、原因、请求 comparator、稳定 outcome、effect_revision、时间。 |
| 测试记录 | source + test_id 唯一；item/version、模型/协议/route、目标修订、操作者、开始/结束、status、延迟、允许的 usage、静态 reason；另存测试租约。 |

`item_revision` 应覆盖所有对外可见状态/原因/模式/决定变化，不只覆盖人工分支；SQL 更新用行锁或 CAS，并同事务 `revision + 1`。租约续期可不变化，但测试进入/退出、过期和 mode 改变需有可对账的新版本。相同版本应表示相同公开事实。旧回执缺失版本视为 legacy 0，不能覆盖已观察到正版本的人工事实。

这解决 new-api 当前 GET 可能用旧非终态覆盖新状态的问题；该调用方风险已由同任务研究者同步。已 accepted/rewarded 仍维持现有单调规则。

建议新增服务间动作入口（仍在 `/integrations/donations/v1`）：

- POST `/batches/:batch_id/items/:item_id/review-actions`：kind 为 enter_review / approve / reject，带新的动作幂等键和预期版本。
- GET `/review-actions/:action_id`：查询同一来源的长期动作结果，用于 POST 回应丢失后的恢复。
- GET `/batches/:batch_id/items/:item_id/review-context`：返回可审/可测原因、当前目标修订、允许模型和边界，不返回完整 key/目标秘密。
- POST `/batches/:batch_id/items/:item_id/tests`：发起一次受限流式调用；test_id 幂等。同一次 ID 重放只返回已有状态，不重新消费上游。
- GET 对应 test_id 的元数据状态：网络中断后证明该次测试已结束/中断，不恢复或透明重放模型调用。

**动作顺序**必须为“校验身份及输入 → 查已有同 action ID → 核对 comparator → 未存在才检查 expected version”。同 action 重放不能被自己成功后变化的 item revision 拒绝。

动作结果区分 `outcome=applied|rejected`（这里 rejected 表示命令未获应用，不能与 item 的审核拒绝状态混淆）。合法命令因版本/状态/截止/目标/库存冲突而被拒绝时，也提交稳定业务结果；鉴权失败、非法输入不必进入业务账本。基础设施故障不能伪造成终态业务拒绝。

响应或动作查询应能核对 action ID、kind、source、batch/item、请求目标、expected revision、outcome/effect revision。HMAC comparator 由接收端用于幂等，不能要求 new-api 使用自己的不同秘密算出同一个 HMAC；跨端要么核对回传的明确语义字段，要么另定义双方相同的无秘密 canonical 请求摘要。

普通 INSERT + 唯一冲突后回滚、新事务读取原 action 的模式继续沿用，不能把 MySQL no-op upsert 的 RowsAffected 当首次处理，见 `donation_batches.go:157`、`:290`。HTTP 超时、代理 409、一次 GET 404 均不能代替上述稳定动作结果；调用方继续原 ID 对账，不能释放 owner 或宣称拒绝已完成。

### 5. 能力、模式与旧客户端兼容

保留 `protocol_version="1"`、原 input types 和原 limits，特别是 7 天 `staging_retention_seconds=604800`。增加命名明确的能力字段，例如 manual_review、review_chat_test、manual_review_override，并分别给出动作/测试边界。

旧 `can_probe`、`target_revision`、`unavailable_reason` 保持当前含义。另加 `can_accept_manual`、`manual_target_revision`、人工不可用原因及聊天能力；不能让 can_probe=false 的目录突然携带一个改变语义的旧 target_revision。目录完整聊天模型列表可通过 review-context 按 item 获取，不必把秘密配置传给前端。

人工目标的基础资格仍要求：启用、普通 api_key 连接、目标可编译、受支持凭据类型、认证不被替换。只是移除“必须 buildGroupValidationTarget 成功”的条件。`can_accept_manual` 与 `can_chat_test` 分开：只有 embedding/rerank 或没有可用聊天模型的组可以明确显示聊天不支持，不能把此类失败当 key 无效。

**旧摘要保护**：不要继续把扩展后的 DTO 直接整体 json.Marshal 用于 auto。改用字段顺序/JSON tag 与旧定义完全相同的冻结 comparator（batch_id、group_id、target_revision、items），缺省与显式 auto 都用原 `donation-batch/v1` 算法；manual_review 用独立域及包含规范化 mode 的新 comparator。加入旧 JSON golden fixture，验证升级前摘要、同标识重放、缺省/显式 auto、auto/manual 冲突。

**旧 revision 保护**：现有算法是 HMAC(`gpt-load/donation-target/v1` + probe.signature + timeouts JSON)，见 `donation_catalog.go:198`，不得重写。新 `manual_target_revision` 使用单独域及 canonical 配置，至少覆盖 group ID、channel/connection/provider、resolved target、有效代理、认证/普通头规则、模型 ID/别名映射、有效参数覆盖及超时。名称/展示文字不影响 revision；ValidationModel/ValidationProtocol 等仅用于 probe 的设置不应变成人工模式的硬依赖。

特别注意 `parameteroverride.Rules` 内部字段不导出（`internal/parameteroverride/rules.go:29`、`:38`），不能 json.Marshal 编译后的 Rules 得到 `{}` 后误以为已签入。应从有效配置源构造 canonical 表达或添加窄的稳定签名 helper。旧 probe signature 没包含这些普通聊天设置，这正是要独立人工 revision 的原因。

调用方研究者已核实旧 new-api 用 `common.Unmarshal` 解码响应（`service/donation_client.go:170`、`common/json.go:28`），容忍新增字段，故 additive v1 capability 可行；它严格检查原 limits，不能顺便改已有值。旧 gpt-load 的请求绑定拒绝未知字段（`donation_http.go:83`）；新 new-api 在 capability 未声明人工能力时不得发送新字段/动作，更不能退回自动提交后在本地称为人工模式。

升级顺序建议先部署支持旧自动行为及新能力的 gpt-load，再部署新 new-api，最后人工开启活动。新状态/模式启用后不要让旧二进制与新进程共同写同一数据库；降级应关闭新活动入口、保留新版处理既有人工记录，不能把人工项转换成旧自动状态求兼容。

### 6. 真实聊天执行路径

推荐首版只接受服务端白名单字段：item 路径、test ID、预期 item/target revision、目录中的模型 ID、文本 prompt，以及受限输出 token 参数。浏览器不能提交 key、Authorization、base_url、group_id、任意 path/query、provider/fallback、任意 body、tools 或附件 URL。

首版可以统一固定 client protocol 为 OpenAI chat，通过 `ResolvedTarget.ModeForModel(protocol.OpenAICompletions, OperationChatCompletion, upstreamModel)` 确认 native/converted；该路径不支持时明确 chat_test_unavailable。`internal/channel/modules/openai_compatible.go:37` 声明普通 chat 的 native route，覆盖 cline 的当前渠道类型；`internal/channel/modules/gemini.go:37` 声明 OpenAI chat 到 Gemini 的 converted route。已有 `internal/execution/bifrost/converted_protocols_test.go:708`、`:729`、`:740` 用本地 Gemini SSE 上游验证这一普通 ExecuteStream 路径、指定 X-Goog-Api-Key、客户端模型别名及正常文本/usage/DONE。因此无需让浏览器选择协议或提供任意路径/JSON/headers，也无需为 Gemini 在审核层另外实现一套原生请求。该结论是代码与现有测试设计证据，本研究没有运行测试或调用真实上游。

**非流式也复用同一路由**：在相同受控 `OperationChatCompletion` / OpenAICompletions / POST `/v1/chat/completions` spec 中将 stream=false（或省略），调用 `Executor.Execute` 即可，无需额外 provider route。`internal/execution/bifrost/executor.go:67` 进入同一 `prepare(spec, false)`，`:119` 调用普通 ChatCompletionRequest，`:166` 返回 AttemptResult；`internal/execution/bifrost/converted_protocols_test.go:452`、`:482`、`:490` 已有指定 key 的 OpenAI chat → Gemini generateContent 本地测试设计。非流式仍须限长解析返回 JSON，只输出允许的文字/usage 并脱敏，不能原样转发 Body。stream 模式必须在该 test 动作开始前确定；流式失败后不能用同 test_id 隐式追加一次非流式调用。

执行准备应复用 `credential_probe.go:151` 起的解密/normalizeStoredCredential、`validationAttemptProxy`、`applyControlHeaderRules`。构造：

- 独立正 uint 虚拟 credential ID、非零 version/generation；key 来自原 item.EncryptedPayload。
- `OperationChatCompletion`、POST `/v1/chat/completions`、选定 client/upstream model、普通 messages、stream=true；无 probe operation、无 ping/1-token 固定探测。
- group ResolvedTarget、Proxy/ProxyFingerprint、ConfiguredHeaders 和受限 Timeouts；`NewAttemptSpec` 后 Validate。
- 每个 test 只调用一次 `ExecuteStream`。Executor 本身执行指定凭据；Bifrost DirectKey 绑定见 `executor.go:1377`，内部 MaxRetries=0 见 `runtime.go:193`，请求 fallback 清理见 `executor.go:860`。

**普通组参数覆盖**：直接用 executor 会跳过 `gateway/handler.go:865` 的 ParameterOverrides。要表达“使用同组正常调用配置”，应在受控 body 上应用相同 Rules，并在覆盖后再验证 body 大小及安全边界。model/stream/store 已属参数覆盖禁改根字段（`rules.go:27`），仍需防配置注入巨量输出、tools/外部动作等超出本次测试范围的内容；发现无法保证受限聊天语义时拒绝测试并说明配置不兼容，不能静默违背组配置。不要让 control import gateway，分层 spec 明确禁止；可复用纯域 helper/现有 Rules。

**认证规则**：当前 `donation_catalog.go:180` 已拒绝 Authorization/x-api-key/x-goog-api-key/api-key 的静态覆盖及删除；人工模式必须同样检查，不能因为跳过 probe 就允许“测试实际用了配置里的另一把 key”。这些检查不构成对上游服务内部路由方式的保证。

**运行时缓存**：当前暂存探测使用 `^uint(0)-item.ID` 隔离正式 DB ID（`donation_worker.go:57`）。可沿用同一 item 的虚拟 ID，但测试与 probe 必须互斥，并使用测试代次的非零 IdentityGeneration，避免失效租约的迟到完成影响新调用。结束路径均 `defer retireCredentialRuntime(id)`。`runtime_manager.go:705` 的分区含 credential ID/generation；`:533` 的 retire 按 ID 标记退休，`:554` 只有 refs==0 才真正关闭。若允许重叠代次，应改为独立且证明不碰撞的虚拟 ID 分配，不能用 Test.ID 与 Item.ID 各自直接取补码而产生跨表碰撞。

#### 流式投影、脱敏与取消

`ExecuteStream` 的 ready 不是调用成功证据；终态 `StreamResult` 验证通过且无错误、协议正常结束才可标记 completed。中途已有文字后再失败/取消，应保留“部分输出 + failed/cancelled/inconclusive”，不显示测试通过。上游无正文正常结束也与“已证明可用”区分。

`StreamEvent.Data` 是协议 SSE 字节，可跨事件/JSON边界，不是可直接显示的字符串。Bifrost 正常成功 chat 分支直接 frameSSE 输出（`executor.go:337`、`:350`）；native SSE 只有非 2xx 分支做 redactSecrets（`passthrough.go:642`）。因此推荐在 gpt-load 的审核测试出口：

1. 限长增量解析 SSE，JSON 解码后只取允许的回复文本（必要时单独的 reasoning 文本）、允许的 finish reason/usage；不转发任意字段、headers、工具参数、URL 或原始错误 body。
2. 用本次已知完整 key 及认证材料进行脱敏；跨网络块、跨 SSE、跨文字 delta 保留匹配前缀，不能逐 chunk `ReplaceAll`。JSON 转义应先解码后检查。复用通用 Redactor 的模式规则，但补独立流式状态。
3. 对下游重新封装 meta/delta/terminal 事件；错误 code 用自有白名单及静态文案。模型输出当不可信文本显示，不执行 HTML/脚本/工具调用。
4. 控制累计响应字节、单事件长度和内存，不落完整 prompt/response 到普通日志；保存必要测试元数据即可。若要保留可回看对话，是额外持久内容范围，不能无意中通过原始 response JSON 实现。

测试使用 HTTP 请求的 context，而不是自动 worker 的应用存活 context。浏览器停止/离开 → new-api 取消转发 → gpt-load context 取消 → executor 停止；sink 写失败返回 error，同步终止上游。`contracts.go:566` 明确 sink error 终止；`executor.go:270`、`:283` 处理取消。结果登记/释放租约使用短且有上限的独立 cleanup context，避免连接一断测试永远 busy；若清理失败由租约恢复标记 interrupted。

建议有单 item 的持久测试互斥、受限服务级并发，不设用户累计捐献/奖励上限。可作为技术初值的单次边界：prompt 16 KiB、请求 64 KiB、累计文本 128 KiB、输出默认 1024/最大 2048 token、总时限 60 秒、首包 30 秒、idle 20 秒、测试租约 90 秒；最终值由总设计统一并通过 capability 返回。有效时限须取组配置、测试上限、暂存剩余时间的较小正值。测试失败因受限预算只说明本次测试未完成。

不能把请求的取消等同于上游绝不计费或已经绝对停止：Bifrost 注释明确 SDK 清理可能晚于调用取消，`executor.go:1589` 有有限清理预算。test_id 幂等应禁止透明重新调用；用户再次测试用新 ID。

gpt-load HTTP server 为支持流式使用 WriteTimeout=0（`internal/app/app_test.go:506`）；新的流式出口还要防慢下游写永久阻塞，采用可支持的响应写截止或有界转发机制，不能只依赖 upstream context。代理缓冲/连接超时在两端集成验证中覆盖，不需本轮变更生产配置。

### 7. 历史 retry_exhausted 明确转审

可行，并且不需要捐献人再次提供 key。`donation_worker.go:193` 到第 5 次只释放未取得 owner，仍保留密文；`donation_recovery_test.go:161` 明确断言 `retry_pending/retry_exhausted` 的密文仍在。

推荐 enter_review 动作仅允许：

- 原 item 仍为自动 effective mode，状态恰为 retry_pending/retry_exhausted；非 accepted/committing/invalid/existing。
- 暂存尚未过期且可解密，CredentialID/AcceptedAtMS 尚未取得；全局 resource/inventory 没有先取得。
- 无活动 probe/test 租约。锁 item 后复查最新 state/token，再锁 resource；若另一次用户 retry 已把它送回 validating，返回稳定 busy/版本冲突，不抢占在途 probe。
- 当前同一 group 可人工接收，提交者已提供见过的 manual_target_revision；不要求它可通过 probe。

成功事务只修改 item 的 effective_mode、review_origin=admin_override、entry_action_id、review_target_revision、state=pending_review、item_revision，清失效自动租约/自动 next_attempt；**不改 batch.validation_mode、原 digest、原 target_revision、原 created/expires、自动 Attempts 和原奖励快照**。

如果读出旧候选后自动 worker 才开始，行锁使它或转审先提交；后到者必须复查模式/state/version。已经跑完但迟到的旧 probe 结果会被 `state != validating || lease_token != old` 拦截（`donation_worker.go:215`）。显式转审不支持将已清理 key 的 invalid 项复活，也不通过重建 batch/item 获取新的资格。

之后 accepted 回执的 effective_mode 必须仍为 manual_review，并携带最终 approve action 和有效人工目标；new-api 不能因为原 batch 是 auto 就绕过本地 approve 校验。原 auto key POST 重放仍返回同一批当前事实，不能被新 mode 破坏摘要一致性。

### 8. 截止、并发、目标变动及库存竞争矩阵

| 边界 | 推荐结果 |
| --- | --- |
| 新/旧人工待审的 7 天截止 | 从原暂存时刻算，不因测试、转审、retry、刷新页面续期。过期清密文，不清永久标识；回执返回明确截止和 staging_expired。 |
| approve 与过期同时发生 | item 行锁下，首次合法提交裁决。批准事务在截止前已到 committing，则后续 TTL 不撤销取得；截止已到则不能再批准，即使后台尚未清理密文。 |
| approve 与 reject 并发 | 原 item revision + 行锁 + 动作唯一性裁决；一个成功，另一个稳定业务拒绝。不能 last-write-wins。 |
| 同项多个测试 | 同一 test_id 重放不发第二次上游；不同 ID 在活动租约存在时 busy。 |
| 测试与审核同时发生 | 首版统一互斥：有活动测试时批准/拒绝暂不可执行，管理员停止并等待结束后再审。测试结果迟到不覆盖决定。 |
| 测试跨过 TTL / 服务重启 | 限制 context 不超过有效截止；失效租约结束为 interrupted/expired，不自动重放；审核决定不由失效测试补写。 |
| 修改活动开关/金额/关闭活动 | gpt-load 不拥有活动；原 batch/mode 与 new-api 奖励快照继续冻结。关闭只影响新批次，不取消既有审核事实。 |
| 组被改名 | 展示可变，不改变执行目标修订。 |
| 目标地址、渠道、模型映射、有效头/代理/参数覆盖发生变化 | 新旧人工修订不同；测试/批准核对管理员见到的修订及当前配置，stale 返回明确冲突，旧测试标为对旧配置的证据。 |
| 组禁用/删除 | 不测试、不批准、不换 group；拒绝与过期处理仍可执行。历史账本不随组级联删除。 |
| 普通管理导入在待审期间先取得 | 全局 resource 行裁决；原项最终 existing，不发奖，不能覆盖 inventory 来源。见 `donation_database_integration_test.go:511`。 |
| 捐献批准先取得，之后组/key 被删除 | 保留既有捐献取得和奖励事实，不能重新领取；按照现有永久奖励政策处理。 |
| 运行时发布失败 / HTTP 回应丢失 | committing 继续恢复，GET/动作查询恢复状态；不得重复插凭据/批准/发奖。 |

**主会话已确定的首版规划边界**：不增加同组目标刷新/改绑动作。原 batch target hash 必须冻结，人工过程使用独立且冻结的 `review_target_revision`；target_changed 后禁止测试和通过，允许拒绝后重新提交或恢复原配置。旧测试标明目标已变化；GET、后台或再次点击测试均不静默重绑。跨组改投同样不属本首版，转审沿用原 7 天 expires，不续期。

旧自动模式目前允许下一轮用同组当前 probe 配置重新测试，并非永远固定受理时的 probe；`donation_recovery_test.go:245` 最终要求新模型第二次 probe 后 accepted。因此不要借人工模式的修订设计全局收紧旧自动重试或改变旧批次语义。

#### 补核实：控制面已有的串行边界及其限度

本任务复用现有控制事务边界，不扩展成全站配置锁或跨进程配置版本重构。此前“必须锁住 group 与系统设置”的建议过强，不能作为现有全局保证。实际代码是：

- group 修改经 `internal/control/service.go:323` 的 `writeGroupConfig`，system settings 修改经 `internal/control/settings.go:150` 调用 `service.go:410` 的 `writeConfig`；两者在同一个 Service 内都持 `writeMu` 并先执行 recovery barrier。审核提交复用同一锁与屏障，可以与同 Service 的配置写串行。
- `withControlTransaction`（`service.go:555`）使用 dbtx.Write。`internal/storage/dbtx/transaction.go:96`、`:243` 明确：SQLite 写为 BEGIN IMMEDIATE，数据库串行写；MySQL/PostgreSQL 写仅为 BEGIN，遵守连接现有隔离级别，不等于 SERIALIZABLE 或全库配置互斥。ReadSnapshot 使用的 REPEATABLE READ 规则不能误套到 Write。
- `ConfigSnapshot.Revision` 是 `internal/state/manager.go:126` 在本进程发布时递增的内存排序元数据，不是跨进程持久配置 CAS。`SystemSetting` 只有 Key/Value/UpdatedAtMS（`internal/storage/models/system.go:8`），时间戳也不是全局 generation。
- `ControlOperation.CommitSequence` 是操作账本自增主键（`internal/storage/models/control_operation.go:6`），用来恢复已记录的操作；`internal/control/idempotency_operation.go:196` 先做业务 mutation，再插账本。普通 settings/group update 并不都写这份账本，故不能拿它充当覆盖全部配置变更的 revision 或锁。

**最小可证明语义**：管理员读取审核上下文时捕获有效人工目标；测试开始前重新校验并冻结该次目标；批准在持有同 Service 的 `writeMu`、通过恢复屏障后，于正式接收写事务内重新编译/比较目标，再写 Credential/resource/决定/committing。已在该次新事务开始前完成的配置修改必须被重新读取并导致修订不符时拒绝；同 Service 内与审核竞争的配置写，由已有写锁决定先后。不要用 UI 的旧缓存或进程内 snapshot.Revision 替代该次数据库目标重读。

跨进程相互重叠的配置更新保留现有数据库隔离语义：审核可能基于自身事务已核对的旧配置完成，回执及测试证据绑定其实际核对的旧修订，不能把它称作新配置下成功；不宣称“正式提交瞬间所有配置行仍是全局最新”或多行配置的跨进程可串行化保证。MySQL 事务快照内重复普通 SELECT 尤其不能被描述为 fresh/current read。捐献项、动作、全局资源的数据库行锁/唯一性保证仍需实现，不因配置边界限制而放松。

测试网络期间不持配置锁；测试后当前修订不同则该证据显示已过期。凭据取得后，管理员仍可正常修改/禁用/删除配置；`committing → accepted` 恢复的是已提交取得事实与现有运行时，不增加“接收后配置保持不变”的条件。需要更强的多进程全局配置原子快照或提交时 CAS，属于另一个控制面一致性任务，本首版不承诺。

### 9. 迁移与针对性验证建议

本轮不运行迁移或测试。后续实现最小充分检查应覆盖：

- 后续冻结迁移扩充状态 CHECK、新字段和 action/test 表；注册表当前最后一项是 `internal/storage/migration.go:108` 的 0015。真实 SQLite/MySQL/PostgreSQL 的带旧捐献数据升级、重复迁移、DDL 中断恢复。新迁移不能复制 0015 的“已有表必须为空”恢复条件用于含历史数据的表演进。
- manual can_accept=true 且 can_probe=false 的本地 fixture，不发 probe，进入待审；普通 auto 的旧 JSON/revision golden 与功能回归。
- 同 source、同 batch/item、同 action、同 key 的并发；先幂等后 version；动作 response loss 后 query；业务拒绝结果也稳定。
- approve/reject/expiry/test/历史 retry 的两独立数据库连接竞态；旧 probe late reply 被 fencing。
- committing 运行时失败恢复、老控制操作 recovery barrier、凭据插入失败事务回滚、管理导入抢先、删除后永久账本，沿用现有 donation_recovery/concurrency/database 测试模式。
- 本地假上游实际收到唯一暂存 key、正常 prompt/模型和合理 token limit；不使用已有有效 key，也不发生第二个 provider/协议/组回退；测试不创建正式凭据、不更新普通 key 健康或配额观测。
- SSE 被任意网络拆片、key 横跨 JSON/SSE delta、成功响应回显 key、错误 headers/body 反射 key、流中途失败、空输出、超限、慢下游、停止/断线/重启；元数据/日志/浏览器均无完整秘密。
- 端到端 new-api 管理权限、记录只读权限与测试/审核写权限分离；本地 approve intent 不能发奖，远端 accepted 必须匹配同 approve action 的实例/来源/批次/项/有效模式/目标。普通捐献人无执行入口。
- 按 gpt-load spec 运行 `make check`；外部数据库和两真实服务测试依其显式 fixture/DSN 执行，跳过不能算已验证。

### 10. Related specs and references

- `.trellis/spec/backend/donation-integration.md`：独立鉴权、单 key 真实证明、全局资源、恢复屏障、永久账本。
- `.trellis/spec/backend/donation-caller-contract.md`：本地 owner、严格回执验证、唯一永久奖励、响应丢失恢复和账号暂停。
- `.trellis/spec/backend/database-guidelines.md`：后续冻结迁移、三数据库、dbtx、MySQL clientFoundRows 及 PostgreSQL 冲突事务规则。
- `.trellis/spec/backend/directory-structure.md`：control 不 import gateway，handler 不 import GORM，路由统一 registry。
- `.trellis/spec/backend/logging-guidelines.md`：受控错误、结构化日志、秘密脱敏。
- `.trellis/spec/guides/cross-layer-thinking-guide.md`：能力探测、显式新旧契约及状态投影边界。
- `.trellis/tasks/09-15-donation-manual-review/prd.md` 与 `research/feasibility.md`：当前用户范围，管理员限定、独立人工决定及暂存原则。
- 外部文档：本研究没有依赖网络资料或重新查询生产。运行时版本依据本仓 `go.mod:3`、`:13`、`:24`：Go 1.27.0、Bifrost core 1.8.4、GORM 1.31.2（MySQL driver 1.6.0、PostgreSQL driver 1.6.2）。

## Caveats / Not Found

- 本研究是设计建议，不代表已实现、已迁移或已验证生产 cline 的当前可用性。没有再次连接服务器、使用真实 key、调用真实上游或进行 git 操作。
- “普通对话”与 Cline/Coding Agent 的真实完整交互不是同一种请求。若上游要求专用工具、身份或客户端流程，本次聊天仍可能失败；不能把未通过聊天当作 key 无效，也不能承诺绕过限制。
- 接收后成功发布运行时是持久边界，不是保证 key 此后每次调用成功。用户可随后删除/禁用组或凭据，既有永久奖励政策不因此回滚。
- `pending_review/resource_busy` 的可操作性投影与测试预算常量仍由主会话统一；同组目标刷新/改绑已明确排除首版。不能将剩余技术细节解释成静默换目标、自动续期或自动发奖。
- 只有新增状态 CHECK 或 UI 按钮仍不够；需要同步 claim/scan/retry/finish、动作幂等、单调回执、new-api 奖励条件及流式脱敏，否则将留下可复现竞态。
