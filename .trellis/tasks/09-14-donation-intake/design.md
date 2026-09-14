# gpt-load 接收接口设计

权威设计见父任务 [设计](../09-14-key-donation-rewards/design.md) 和 [集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。

- `internal/control/` 实现接收应用逻辑与 handler；模型/迁移放 `internal/storage/`，网络探测不占用长事务。
- `internal/platform/httproute/` 明确新 Owner/Auth，`internal/container/` 汇总模块；集成 token 与普通管理权限分离。
- 目录复用 ListGroupOptions 与目标解析，返回最小字段；空组可选，无法验证的目标返回原因。
- 暂存、处理归属/代次、重试和回执持久化；逐条复用规范化与 probe，不整批遇错回滚或切换其他 key 探测。
- 正式接收复用控制面事务、唯一性和运行时更新，全部完成才返回 accepted。持久历史独立于短期操作回放结果。
- 资源指纹及来源独立于组/凭据生命周期；查询限定集成来源；普通库存也视为已有资源。
- 增量迁移，不清理现有 key；回退关闭新集成入口并保留历史与恢复依据。

迁移编号、内部租约与秘密配置落点实施前按现有规范确定并记录，不改变父契约。
