# new-api 捐献页面与管理员记录

## Goal

实现顶部导航、多行捐献、风险小字、活动分组配置和用户与 key 关联记录界面。

父任务：[密钥捐献与社区额度奖励](../09-14-key-donation-rewards/prd.md)。目标为 `D:/code/Claude code program/new-api/web`；2026-09-14 已按续接授权启动为 in_progress，接入已通过独立复核、真实数据库和双服务联调的后端。

## Requirements

- 覆盖父 R3、R6、R9、R11–R13、R16、R18，遵守 [集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。
- 顶部内建捐献入口，沿用登录，多行粘贴每行一个 key，查看逐项进度和永久奖励。
- 管理员配置名称、真实组、固定永久额度和开关；不增加独立平台参数、有效期或领奖上限。
- 管理记录关联用户、key 掩码、远端凭据、组和奖励，支持筛选/详情，不暴露秘密。
- 按父 R18 展示风险小字，提交前可见，不增加确认弹窗或强制勾选。

## Acceptance Criteria

- [x] 父 AC6/AC9/AC11：公共页、登录后和移动顶栏可达；真实组下拉有错误/空状态，不回显 token。
- [x] 父 AC10/AC16/AC17：逐项状态/原行号正确，混合批次不误报全成功；普通用户只看本人，管理端可追溯。
- [x] 父 AC14/AC15/AC19：展示固定永久奖励，无有效期、上限或追回入口。
- [x] 父 AC20：风险小字可读、提交前可见、多语言可用，无额外确认步骤。
- [x] 交互测试、完整typecheck、变更文件lint/格式/版权及构建通过，复用组件且不持久缓存明文 key；全仓既有问题单独保留并记录。

完成证据：[implementation-handoff.md](research/implementation-handoff.md)、[独立复核](research/check-handoff.md) 与父任务 [最终真实浏览器验收](../09-14-key-donation-rewards/research/browser-validation.md)。24个交互用例及最终生产构建通过；全仓239条lint error/65条warning、92个格式项、22个版权项均不涉及本次改动，未据此声称全仓检查全绿。

## Scope and Dependencies

- 真实用户流程验收依赖 [new-api 后端](../09-14-donation-rewards-backend/prd.md)；可先按冻结契约使用假响应开发。
- 仅负责 new-api 前端、i18n 和测试，不修改两个仓库后端。
- 复用 new-api 现有 React 组件，不套用 gpt-load 的 Vue/pnpm/无前端测试约定。
