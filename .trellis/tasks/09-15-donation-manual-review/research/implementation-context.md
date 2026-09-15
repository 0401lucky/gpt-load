# 实施/检查所需研究摘要

供上下文注入使用，完整证据保留在 [gpt-load 研究](gpt-load-design-review.md)、[new-api 研究](new-api-design-review.md)、[UI 与仓库约定](new-api-ui-and-conventions.md)。协议字段和首版边界统一以本任务 `design.md` 为准；研究原文的候选命名不另作契约。不要调整全局注入上限来加载长报告。

## 核心不变量

1. `auto | manual_review`；人工提交或显式转审不能自动发奖。待审 key 仅加密暂存，批准后复用 committing/accepted 的正式接收与运行时恢复。
2. new-api 的不可变 batch 提交模式、用户、奖励、RequestDigest、原 target_revision 不被活动修改或转审改写。原 manual batch 或已确认转审项永远不能因缺字段/未知值回退 auto。
3. Item 的 effective_mode、单调 item_revision、review_target_revision、entry_action_id、review_action_id 与原 Resource.Generation 是不同概念。已经版本化或进入人工的项禁止迟到低版本/0覆盖，同版本矛盾拒绝。一直是 legacy auto、从未有人工事实的旧接收端记录每次都缺版本，必须允许 queued(0) → validating(0) → accepted(0) 按旧规则推进，不能误套同版本冲突。
4. ReviewAction 先在 new-api 主库保存 pending 意图，之后用原 action UUID 对账/提交。gpt-load 先查动作幂等，再查 expected revision；确定业务拒绝也落账。网络错误、404 或普通 409 不证明未执行，不能释放 owner 或另发相反动作。
5. 人工奖励需要同项/同目标已 applied 的 approve 动作与匹配 accepted 回执；ApplyReceipt 和 CreditDonationReward 各自保护。只有 pending 意图、测试成功、HTTP 200 或 committing 不足以发奖。accepted 提前到达可在完整绑定下确认原 pending 意图，避免永久卡住。
6. 正式凭据、全局资源首次取得、批准事实和 committing 同事务；之后调用原运行时恢复。失败不得再次插入凭据、重复增额或绕过已有控制恢复屏障。
7. 全局资源行与普通管理导入共用；库存先取得时 existing，不冒认来源、不发奖。拒绝/过期只释放未取得且仍归本 item 的 owner，已取得事实永不清除。
8. 原 7 天截止不因转审、测试或 retry 续期；超过截止不能批准。批准已进入 committing 后，TTL 不撤销取得。

## 接收端必须注意

- `donation_batches.go:57` 的旧 digest 是旧请求整体 JSON 的 HMAC。auto 使用冻结的原 comparator/域及原序列化字段次序；人工模式单独包含 mode。显式 auto 与缺省 auto 等价，人工和自动同 UUID 不同语义冲突。
- `donation_catalog.go:198` 的旧 target_revision 是 probe signature + timeouts，不变。新增人工 revision 覆盖正常执行配置，不能将私有字段的 ParameterOverrides.Rules 直接 Marshal 成 `{}`；组名和纯 probe 设置不应成为人工测试的硬依赖。
- 目录分开 can_probe 与 can_manual_review；人工模式仍检查启用、普通 api_key、合法目标和认证不被覆盖。旧 input_types/limits（特别 retention=604800）保持不变。
- 新 `pending_review/rejected` 需后续冻结迁移、状态 CHECK、scan/claim/retry/finish/receipt 全链变更，不能只加 UI。过期沿用 invalid/staging_expired。
- enter_review 首版只允许有效暂存、无活动租约的 retry_pending/retry_exhausted 项；原 batch、Attempts、截止不变，旧 retry 和迟到 probe 不能改回自动。
- 测试/通过核对已冻结人工目标；配置变化不静默重绑，不新增刷新目标动作。拒绝不依赖目标在线。
- `writeMu` 仅同 Service；dbtx.Write 在 SQLite 用 BEGIN IMMEDIATE，MySQL/PostgreSQL 非全局串行化。不要把进程内 snapshot.Revision 或 ControlOperation.CommitSequence 宣称为跨进程配置版本，也不要扩成全站配置锁重构。沿既有事务处理与并发配置操作的重叠语义，见完整研究补充。

## 指定 key 的真实调用

- 统一受控 OpenAICompletions + OperationChatCompletion，POST `/v1/chat/completions`。cline 所属 openai_compatible 为 native，Gemini 有现成 converted 路径；stream/nonstream 分别用 ExecuteStream/Execute，不新建 provider 路由。
- 解密原 item 暂存，构造独立 CredentialSnapshot；不经过 scheduler、new-api Relay/Playground、钱包或其他 key fallback。虚拟凭据 ID/代次与正式/自动 probe/其他测试要可证明隔离。
- 应用原组 header/proxy/ParameterOverrides，覆盖后再校验大小、文本形态和 tokens；不接受任意 key/group/url/path/header/tools/附件。
- 正常成功响应没有统一秘密脱敏保证。不能裸转发 StreamEvent.Data；增量解析、只取允许文本、跨网络块/SSE/delta 脱敏，非流式同一规则。原始上游 headers/error body 不直出。
- 协议完整结束、有效输出且结果已持久化后才 succeeded；部分输出后错误/取消是非成功。SSE HTTP 200 或通用 admin audit.Success 不是可用性证明。
- test_id 先持久占用，重放只查元数据，不能再次调用。取消全链传播；短 cleanup 释放租约，进程中断后记 interrupted，不后台重测。只保存元数据，不保存完整对话，不扣站内钱包。
- test 和审核首版互斥；停止并完成清理后可审核。测试结果不自动通过/拒绝，也不是人工通过的必备条件。

## new-api 恢复/迁移/界面

- 新权限为 records.read 加独立 review/test，actor 由认证上下文取得。普通用户或只读管理员不能写；UI 隐藏不能替代后端验证。
- 本人 batch/item 投影增加安全 review_note，只取已 applied 的最终 reject 备注，供捐献人看到拒绝原因；reason_code 保持闭集。批准备注和管理测试详情不下放到普通用户接口。
- 专用 JSON client 现为 12 秒，长测试需要独立有界方法，继续沿用 HTTPS/loopback、TLS、无重定向/代理及关闭 POST 透明重放规则。
- 待审进入低频小配额对账；pending 动作/待奖励进入热恢复。唤醒与动作同事务，领取与 release 重查最新 flags，冷任务不能延误新批准。Summary 和 frontend polling 需要独立 PendingReview/Rejected。
- 新模式列显式规范旧 NULL/空串，未知值拒绝；不使用默认 true 布尔字段，不改旧 CampaignRevision JSON。新动作/测试表加入秘密材料历史检测和备份范围。
- 旧 upgrade fixture 只创建 User/TopUp/Checkin，不能证明本次兼容。补本次前真实 DonationCampaign/Batch/Item/Reward schema 和数据，三库各执行升级/重复迁移，检查旧唯一性和奖励。
- 复用现有 record-detail、ConfirmDialog、DataTable、Switch/Combobox、RHF 和捐献 session/cancel 守卫；现成 Playground 整页及其普通 Relay 不适用于待审 key。
- new-api 按自己的 Go/testify、React/Bun、七语及测试规范；gpt-load 按自己的 stdlib 测试、make check、无本地 race/无 Vue 前端测试规范。

## 重点验证

覆盖旧摘要 golden、缺新 capability、没有 probe 的人工待审、同一 key 的真实 HTTP 请求、批准/拒绝/过期/旧 retry/库存竞争、动作回应丢失与双端重启、无批准事实的 accepted 不发奖、回执版本倒退、流式秘密跨片段、取消/慢下游、跨 item/session 旧输出、冷热队列公平性及三库旧数据升级。

以上是研究与实施约束，不表示任何功能测试已通过。全部测试只用本地隔离库与合成上游，生产 cline 的当前调用能力未验证。
