# 捐献集成契约 v1

最终规划的跨仓库契约，正在本地实施。产品约束以 [PRD](../prd.md) 为准。2026-09-14 实施补足无明文重试动作及请求边界；均服务于既定恢复要求，不改变产品规则。尚未发布到线上。

## 系统边界与认证

- new-api 拥有用户、活动、批次明细、全站奖励去重和永久余额结算；gpt-load 拥有分组、上游探测、加密凭据和可恢复的接收结果。
- 只需要 new-api 后端主动调用 gpt-load。浏览器调用同源 new-api 接口，不接触 gpt-load 集成 token；gpt-load 不持有 new-api 管理权限。
- gpt-load 增加专用于捐献集成的 Bearer 凭据，仅能调用下述目录和接收接口，不能访问凭据明文、删除资源或调整其他管理配置。未配置时该模块不可用，不回退为匿名或普通 AccessKey 认证。
- source/instance 标识持久保存，不从可轮换 token 或服务 URL 推导。new-api 保存目标实例身份，连接地址变更不清空历史去重；检测到不匹配的实例不能静默改绑历史资源。
- 新模块使用独立 `/integrations/donations/v1` 命名空间，经 gpt-load `httproute` 注册并明确新增的 Owner/Auth 契约及装配；不放开现有 `/api` 管理路由。具体 token 持久化遵循仓库秘密配置机制，不共享两端加密主密钥。
- new-api 的用户接口沿用 `UserAuth`；管理接口沿用 `AdminAuth` 及现有 authz 注册方式，配置与管理记录读取分开授权，普通用户不能访问全量分组或全站记录。

## gpt-load 提供的接口

| 方法与路径 | 输入 | 输出 data 与行为 |
| --- | --- | --- |
| GET `/integrations/donations/v1/capabilities` | 集成凭据 | `protocol_version`、稳定 `instance_id`、受支持的普通 key 输入能力及单次请求边界 |
| GET `/integrations/donations/v1/groups` | 集成凭据 | `id/name/channel_id/connection_type/enabled/can_probe/target_revision/unavailable_reason`；不返回凭据或原始 params |
| POST `/integrations/donations/v1/batches` | `batch_id/group_id/target_revision/items[{item_id,key}]` | 持久化加密暂存后返回批次与逐项状态；同标识同请求重放原结果，内容不同返回冲突 |
| GET `/integrations/donations/v1/batches/:batch_id` | 集成身份 + 原批次 ID | 逐项 `item_id/state/reason_code/credential_id/accepted_at_ms`；仅返回该集成身份的批次 |
| POST `/integrations/donations/v1/batches/:batch_id/retry` | `Idempotency-Key`（新动作 UUID v4）及可选 `item_ids` | 按原暂存密文重排符合条件的未接收项；省略 item_ids 时处理批次全部可重试项，不重探测 accepted/committing 项；动作幂等且查询保持只读 |

每个 item_id 稳定且全批唯一，group_id 不由捐献人指定。target_revision 是 gpt-load 提供的配置版本标识，不从显示名称生成。调用方不提供“已验证”标志，不把客户端 HMAC 当作上游验证证明。

实施边界：初次 POST batches 的 `Idempotency-Key` 等于 `batch_id`；单批最多 100 项，单项 key 最多 4096 字节，请求体最多 1 MiB，能力接口公开相同边界。这些是单请求工程边界，不限制用户累计捐献或领奖数量。自动探测有界重试，耗尽后仍可按原标识请求重试；失败暂存密文保留 7 天后可清理，长期回执、来源和成功资格不随之清理。

能力接口 data 的具体类型在实施中固定为：`protocol_version: "1"`（字符串），`instance_id` / `source_id` 为持久 UUID，`input_types: ["api_key"]`，`limits: {max_items: 100, max_key_bytes: 4096, max_body_bytes: 1048576, staging_retention_seconds: 604800}`。来源身份由集成认证确定，不接受浏览器或任意请求体指定来源。

已有 `/api/groups/options` 的目录查询、渠道解析和 probe 可复用；新增接口收敛投影、权限和接收回执。原管理导入响应保持兼容，不额外暴露全量凭据列表来让调用方猜 ID。

## new-api 提供的接口

下表方法和 data 字段采用 new-api 既有响应包装；所有所有权/权限判断在后端执行。

| 方法与路径 | 用途与关键输入 |
| --- | --- |
| GET `/api/donations/campaigns` | 已登录用户的活动列表，包含显示名称、说明、每 key 永久奖励和可接收状态；不泄露连接密钥或底层配置 |
| POST `/api/donations/batches` | `campaign_id/keys_text`，必须携带 `Idempotency-Key`；用户 ID、目标组、规则版本和奖励额度由服务端确定 |
| GET `/api/donations/batches` | 当前用户批次列表，支持分页；不接受替换收款/查询用户的参数 |
| GET `/api/donations/batches/:id` | 本人批次及逐项进度、失败原因、脱敏标识和已到账汇总 |
| POST `/api/donations/batches/:id/retry` | 本人批次中可重试项，新的动作幂等键；先对账，绝不重发已成功项 |
| GET/PUT `/api/donations/admin/connection` | 管理员维护一个 gpt-load 连接；读取仅返回地址、配置状态和实例标识，不回显 token |
| GET `/api/donations/admin/group-options` | 后端读取并投影目标目录，区分无分组和连接失败 |
| GET/POST `/api/donations/admin/campaigns` | 管理员查看/创建活动：`name/description/group_id/reward_quota/enabled` |
| PATCH `/api/donations/admin/campaigns/:id` | 修改活动生成新规则版本，支持关闭；不删除已受理批次及历史 |
| GET `/api/donations/admin/records` | 用户、活动、组、状态、时间、记录/凭据 ID 筛选与分页 |
| GET `/api/donations/admin/records/:item_id` | 稳定关联的用户、原输入行、历史处理结果、远端凭据引用及奖励记录 |

管理组选择与活动保存均校验真实目标；无可用 key 的空组仍可作为接收目标，禁用/删除/不支持验证的组不可接收。正常捐献页面不提供活动配置和全站记录能力。

## 包装、标识与金额

- gpt-load 现有成功包装为 `{code: 0, message, data}`，错误 code 是字符串（`internal/platform/response/response.go:14`）；new-api 使用 `{success, message, data}`。客户端各自严格解码，不能对两端套同一个 success 判定。
- 批次受理返回成功包装只代表已持久化接收请求，`state=queued/validating` 不等于已入库或到账。接口可沿用各仓库 HTTP 200 成功包装，通过明确状态表达进度。
- HTTP 故障和业务失败分开；重试依据稳定业务标识，不依据“上次没看到成功”创建新奖励单。新增错误由各仓库标准错误出口处理，不返回上游原始含密钥内容。
- UUID 等不透明字符串用于批次/明细/请求幂等标识；持久 ID 不复用。HTTP 幂等键按已认证用户/来源作用域隔离，相同键改变内容返回冲突。
- `reward_quota` 使用 new-api 钱包的整数额度单位，按现有严格换算及数值上界验证；页面沿用当前额度展示方式。活动修订冻结到批次，不从 key 文本、活动名或浏览器金额发奖。
- 对外时间字段显式标明 `_at_ms`；接入 new-api 使用秒的既有接口时在边界转换，避免混用。原输入行号为 1 起始，忽略空行但保留定位能力。

## 数据与唯一约束

| 归属与逻辑实体 | 必需数据/唯一性 |
| --- | --- |
| new-api 活动与版本 | 活动 ID、显示名称、目标实例/组、奖励整数、启用状态、版本；受理时冻结 |
| new-api 批次 | 当前用户、活动版本、请求键与摘要、接收确认状态、创建时间、重试信息 |
| new-api 明细 | 明细 ID、批次/原行号、资源引用、key 掩码、状态、原因、远端组/凭据 ID、处理时间 |
| new-api 资源资格 | 规范化 key 的带版本 HMAC 唯一；当前处理归属/代次、首次成功明细、永久已接收标记 |
| new-api 奖励 | 资源身份和成功明细各自唯一、收款用户、固定金额、提交状态/时间；与永久余额变化同事务 |
| gpt-load 暂存与回执 | 来源+批次/明细唯一、目标及版本、加密暂存、校验状态、永久接收资格、凭据 ID 与接收时间 |

这是逻辑实体契约，物理表名/迁移编号由子任务落实；任何合并都必须保留唯一性、恢复和独立历史生命周期。任务进度、重试计数和租约应持久化，可放在批次/明细内，不需要另引入外部消息队列。

new-api 不长期保存明文 key。只有 gpt-load 确认已将 key 加密持久化，new-api 才可向用户确认已受理。初次传输未确认时保留元数据并查询原批次；确证未接收才允许重新提交原内容，不能丢失 key 后仍宣称已可靠受理。

## 状态与恢复

- gpt-load：`queued → validating → committing → accepted`；分支为 `invalid`、`existing`、`retry_pending`。只有持久化凭据和必要运行时更新完成才返回 accepted；提交后失败由原操作恢复，不再新建凭据。
- new-api 接收状态与奖励状态分开。仅可信的 accepted 回执可进入 `reward_pending → rewarded`；重复/已有/无效/校验不确定项不进入发奖事务。
- 奖励事务检查资源与明细唯一性、账号状态及金额，提交永久余额和奖励记录；提交后执行既有缓存同步/审计派生逻辑。重放返回原结果，不能重复应用缓存增量。
- 同 key 正在处理时其他请求不获得第二份资格；确认未接收的失败尝试可以有界重试，已接收历史永不释放。租约过期或网络超时不能单独证明远端失败。
- 失败项重试使用原资源/明细归属，明确失败可重试，不确定项先对账。无需用户再次提供已被 gpt-load 接收的 key。无效 key 更换为另一串值应是新提交，不修改原明细。
- 冻结活动版本后按该版本处理；活动关闭只拒绝新受理，账号禁用暂停未发奖励，目标配置变化需重新核对验证。后续 key 失效或删除仅影响当前资源状态，既不追回奖励也不清除历史。

## 验收与有限延后项

关键验证为混合多行、跨用户/组/请求重复、并发资格竞争、两端断点重启、接收/入账响应丢失、日志清理与凭据删除后再次提交，以及永久奖励不追回。

可在实施子任务中确定的内部细节仅包括具体迁移序号、受控重试延时/次数、组件拆分和持久秘密配置落点；须记录代码依据并测试，不改变“未确认不发奖、接收后可恢复、永久去重、不设业务领奖上限”的行为。
