# new-api 捐献前端执行计划

2026-09-14 已开始。所有权为 new-api/web 的捐献、导航/配置接入、i18n 和测试；协作时不是唯一修改者，不回滚他人改动。实际接口以 backend research/implementation-handoff.md 为准，保留项目既有设计与版权头。

1. [x] 读取 web/AGENTS.md、shadcn-ui 技能并检索/阅读共享表单、选择器、表格、状态和导航组件，记录复用方案。
2. [x] 增加内建顶部导航与页面，兼容旧设置、登录返回和移动入口。
3. [x] 实现活动、多行输入、风险小字、请求身份、逐项进度及本人记录，无确认弹窗。
4. [x] 实现管理员连接/活动配置和分组下拉，不回显 token，不新增平台、有效期或上限字段。
5. [x] 实现管理记录筛选/详情，关联用户、key 掩码、远端 ID 和奖励。
6. [x] 在模块 __tests__ 覆盖混合批次、分组失败、权限/账号切换、永久奖励与提交前可见的风险提示。
7. [x] 在 new-api/web 运行 `bun run typecheck`、`bun run lint`、受影响测试、`bun run format:check`、`bun run copyright:check`、`bun run build:check`。
8. [x] 后端就绪后验证真实流程与移动入口，记录结果供父任务验收；不能运行的检查注明阻塞。

最终24个用例、完整类型检查、变更文件lint/格式/版权和build:check均通过；全仓未修改文件的既有lint/格式/版权失败有完整清单。真实桌面/移动、账号切换、管理配置/记录和秘密保护验证见父任务 [browser-validation.md](../09-14-key-donation-rewards/research/browser-validation.md)。2026-09-14已用task.py start切回父任务，执行完成跨仓库验收。

回退隐藏新提交入口，不删除记录或改变已发奖励。实现细化不得改变父需求中的小字提示、无追回和无领奖上限。
