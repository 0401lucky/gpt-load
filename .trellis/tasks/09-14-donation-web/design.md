# new-api 捐献前端设计

权威需求和提示原文见父 [PRD](../09-14-key-donation-rewards/prd.md)，数据见 [集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。实施前读 web/AGENTS.md、shadcn-ui 技能并检索共享组件。

- 复用 useTopNavLinks、HeaderNavModules、PublicHeader/AppHeader 和现有登录返回，兼容旧导航配置及移动端。
- 捐献页展示活动、每 key 永久奖励、多行输入和本人结果；R18 提示在输入区附近，使用可读的辅助文字样式与自然换行。
- 提交及网络重试保留稳定幂等身份；原文仅在输入/发送所需内存中存在，不进入日志、URL 或 localStorage，结果只保留掩码/引用。
- 显示逐项与汇总结果，区分网络、业务错误及处理中；刷新不能重提已成功项。
- 管理表单复用共享选择器，以组 ID 绑定；空、错和已选组失效均明确显示，不静默改选。
- 管理记录复用 DataTablePage/useDataTable 与筛选/详情模式，历史接收/奖励与当前失效状态分开。
- 账号切换隔离 query cache，权限由前后端共同保证；i18n 使用既有英文源 key，小字也需翻译。

不做品牌重设计，不增加追扣、有效期、领奖上限或平台配置。组件拆分遵循现有 feature 约定。
