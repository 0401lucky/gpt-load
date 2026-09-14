# new-api 捐献后端设计

权威设计见父任务 [设计](../09-14-key-donation-rewards/design.md)、[集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。遵守 [new-api 约定摘要](../09-14-key-donation-rewards/research/new-api-conventions.md) 和原始 AGENTS.md。

- 沿用 router/middleware/controller/model/service 分层与现有登录；管理连接、活动和全站记录分开鉴权。
- 保存单 gpt-load 连接及活动版本；正确区分两端响应包装，前端不接触底层 params 或集成 token。
- 主库保存批次、明细、全局资源资格和奖励；HMAC 秘密稳定持久，独立于会话和集成凭据，不长期保存明文 key。
- 确认 gpt-load 加密持久接收后才宣称可靠受理；丢回执先查询原批次，后台依据持久进度恢复。
- accepted 条目在同一事务内检查资源/明细唯一性、增加 User.Quota 并记录奖励；提交后按既有预扣语义同步缓存与审计。
- 不写 Checkin，不设置业务领奖配额、到期或追回流程；后续 key 状态不改变历史事实与已发奖励。
- 账号禁用暂停待发，活动关闭拒绝新批次；已受理项按冻结规则处理，目标变化显式核对。
- 数据库遵守三方兼容、lockForUpdate 和金额边界；增量迁移，回退保留业务唯一性与历史。
