# 捐献集成契约

## 1. 适用范围

修改 `internal/control/donation_*.go`、普通凭据导入与捐献去重的衔接、迁移 `0015_donation_intake`，或集成路由/配置时阅读。gpt-load 负责指定 key 校验、接收和长期回执；new-api 负责用户身份、活动与永久奖励，两者不共享数据库。

## 2. 接口与数据签名

所有接口在 `/integrations/donations/v1`，以 `httproute.OwnerDonation` / `AuthDonation` 注册，由 container 装配，不能放宽 `/api` 管理鉴权。

| 方法 / 相对路径 | 输入 / 行为 |
| --- | --- |
| GET `/capabilities` | 协议版本、持久 instance/source 身份、普通 key 能力及请求边界 |
| GET `/groups` | 只读投影组 ID、名称、渠道、连接类型、enabled/can_probe、目标修订与不可用原因 |
| POST `/batches` | `DonationBatchRequest{batch_id,group_id,target_revision,items:[{item_id,key}]}`，先加密持久暂存 |
| GET `/batches/:batch_id` | `GetDonationBatch`；仅当前来源的逐项回执，查询不重新排队 |
| POST `/batches/:batch_id/retry` | `DonationRetryRequest{item_ids?:[]}`，对原暂存项执行幂等重试动作 |

持久模型在 `internal/storage/models/donation.go`，冻结迁移模型在 `internal/storage/migrations/0015_donation_intake.go`：

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
| 目标删除、禁用、类型不支持或无法解析探测 | 409 `DONATION_TARGET_UNAVAILABLE`，安全 reason_code |
| 接收时 target_revision 已改变 | 409 `DONATION_TARGET_CHANGED`，不能静默改投其他组 |
| 批次不存在或不属于当前来源 | 404 `DONATION_NOT_FOUND` |
| 同幂等标识内容不同 | 既有幂等冲突错误 |
| key 已在任意组库存或历史成功账本 | 逐项 existing，不冒认原来源、不产生新资格 |
| 上游拒绝 / 超时或限流 | invalid / retry_pending，均不得报告 accepted |

所有 handler 错误通过 `writeServiceError`；网络错误不直接带上游原文。新增可见错误同步三份后端 locale。

## 5. 正常、基础与错误场景

- 正常：一批中有效、无效、重复、已有、限流项独立处理；只有指定 key 实际探测通过，凭据/长期资格事务提交且运行时恢复完成的项进入 accepted。
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
| 依据组内另一个可用 key、格式合法或前端声明来接受捐献 | 对暂存的单一 CredentialRef 真实探测，拒绝替换认证材料的配置 |
| token 轮换、资源删除或日志清理时重建指纹身份 | 保留独立持久身份与长期账本；密钥丢失或不匹配时明确拒绝继续 |
