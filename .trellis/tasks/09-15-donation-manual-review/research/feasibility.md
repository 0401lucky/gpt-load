# 人工审核与真实调用：首轮可行性研究

## 已核实的现有行为

1. **现有测试已经发送真实请求，但请求形态固定。** `internal/execution/bifrost/executor.go:878` 的 `newProbeRequest` 使用用户消息 `ping` 和 1 个输出 token；`openai_compatible` 使用 `max_tokens: 1`。同文件 `supportedRequestShape` 的 probe 分支不允许流式、请求体或自定义路径。不能把它加一个前端按钮就称为可输入内容的对话测试，也不能仅凭这个差异断言 cline 的具体失败根因。
2. **底层具备执行正常对话和流式的能力。** `internal/execution/contracts.go:571` 的 `Executor` 和 `internal/execution/bifrost/executor.go:179` 的 `ExecuteStream` 是可复用边界；正式设计应把审核测试转换成受控的普通对话操作，同时锁定暂存的单一凭据。
3. **暂存凭据不需要提前加入调度池。** 现有 `internal/control/donation_worker.go:49` 为暂存 key 构造独立临时 `CredentialRef`，回收对应运行时缓存。复用这种绑定方式，不通过普通网关选择组内凭据。
4. **当前接收资格与 probe 绑定。** `internal/control/donation_catalog.go` 的 `buildDonationTarget` 要求 `buildGroupValidationTarget` 成功。人工模式必须区分“组可以接收普通 key”“组支持某种对话测试”和“组可以跑现有 probe”，不能仅修改表单的 `can_probe` 判断。
5. **new-api 已有合适的审核展示位置。** `web/src/features/donations/components/record-detail.tsx` 展示明细、奖励与历史；`campaign-form.tsx` 已使用共享 `Dialog`、`Switch`、`Combobox`、表单和状态组件，可在现有结构中增加活动模式和审核动作。
6. **现有聊天前端可提供流式交互参考，但后台路由不能直接复用。** `web/src/features/playground/hooks/use-stream-request.ts` 的 `createStreamRequestController` 支持请求代次、流式事件和取消；`controller/playground.go` 会构造站内用户 token 并进入普通 `Relay`。后者不能保证使用指定的待审核捐献 key，不能充当审核测试后端。`web/src/features/channels/components/dialogs/channel-test-dialog.tsx` 当前围绕已有渠道和模型批量测试，不提供待审核 key 的上下文。
7. **当前权限只有配置读写和记录读取。** `service/authz/resources_donation.go` 与 `router/donation-router.go` 没有审核/调用权限；新增写能力应明确授权，不沿用仅可读记录的权限。
8. **两端都有严格状态白名单。** gpt-load 的模型及冻结迁移 `0015_donation_intake.go` 有状态 CHECK；new-api 的 `model/donation_reward.go:48` 对回执状态、全部明细、组、修订、凭据与时间进行验证。新增待审核/拒绝语义需要同步迁移、投影、批次汇总和恢复逻辑，不能只改 worker。
9. **奖励触发是正式 accepted 回执。** `model/donation_reward.go` 的 `ApplyReceipt` 和 `CreditDonationReward` 构成事实及奖励边界。人工模式应在此增加人工决定条件，保留原永久奖励事务，而不调用管理增额接口另行发放。
10. **既有暂存有期限。** `internal/control/donation_catalog.go` 配置 7 天暂存，`donation_worker.go` 清理过期 payload，`new-api/model/donation_reward.go` 只在可信终态释放未接收资源 owner。正式设计需覆盖待审核过期、拒绝、重复提交及已耗尽自动测试的旧记录，避免长期占用或重复资格。

## 建议的产品行为（尚未作为最终方案批准）

- 在管理端捐献详情里为单笔待审核 key 提供轻量对话测试：从真实目标组选择模型，输入内容，查看流式响应、结束/失败状态和耗时，可取消调用。
- 测试结果是审核参考，测试成功不自动批准；测试失败也不直接冒充 key 无效。人工通过/拒绝是独立、有审计记录的决定。
- 待审核 key 保持加密暂存；只有通过后的正式入库与运行时发布完成，才满足永久奖励条件。
- 不允许调用方传入其他 key、目标 URL、Authorization 或切换到任意组；配置、认证和凭据取自服务端的原捐献记录。
- 模型响应作为不可信文本展示；测试或失败结果不得包含实际认证材料，不能保存完整 key 到浏览器缓存。
- 第一次范围确认只询问真实调用工具的使用角色；用户已同意跳过自动测试必须人工审核，不重复询问该决定。

## 后续设计必须落实

- 人工模式的能力协商、目标修订、批次模式快照及旧客户端兼容；请求摘要新增字段不得导致旧幂等请求失配。
- 审核决定与远端接收的可恢复顺序、并发动作裁决、账号禁用、重复库存与审核时目标变更。
- 针对原捐献项的调用协议、模型选择、输入/输出与时间边界、流式取消传播、测试动作记录和权限校验。
- 暂存过期与拒绝的终态、旧 `retry_exhausted` 记录的显式处理路径；不以修改活动的方式静默重写历史。
- 按各仓库约定执行后续验证；数据库变更需要 new-api 的真实 SQLite/MySQL/PostgreSQL 兼容与重复迁移验证。

## 本轮验证边界

本轮只读取源码、历史契约和已取得的服务器脱敏证据，写入任务规划资料。未修改产品代码、未触发真实上游调用、未迁移数据库或启动实现任务。`git diff --check` 通过；这不构成功能验证。
