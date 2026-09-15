# new-api 审核界面与仓库约定

- 范围：`../new-api` 的活动设置、捐献记录、用户状态展示与后续验证。
- 日期：2026-09-15。
- 产品角色已确认：真实调用仅管理员审核时使用，不增加捐献人自测入口。

## 规范来源

实施时以 live `../new-api/AGENTS.md`、`../new-api/web/AGENTS.md` 和 `../new-api/.agents/skills/shadcn-ui/SKILL.md` 为准，不能套用 gpt-load 的 Vue 或禁写前端测试规则。

- React 19、TypeScript、TanStack Query/Router、Base UI、Tailwind 4；前端使用 Bun。
- 现有表单使用 React Hook Form/Zod；文案沿捐献模块已有英文源文 key，并同步 `en/zh/zh-TW/fr/ru/ja/vi` 七份 locale。
- 优先复用项目业务组件，先查 props/实际使用，不能只导入基础 Button 就重新实现共有弹窗、确认、表格和复制行为。
- 普通请求使用统一 `api` 客户端及现有捐献会话封装；有副作用的审核/测试不自动重试。取消、过期响应、换用户和退出登录必须遵守现有会话代次守卫。
- 修改 TypeScript 后跑 `bun run typecheck`；对涉及文件做 lint/format；扩展现有 Vitest/Testing Library 用例，断言可见行为，不按文件数量散落重复测试。
- Go 业务 JSON 通过 `common.*` 包装；新写/大改测试使用 testify `require`/`assert`。数据库锁复用 `lockForUpdate`。
- 模型/迁移变更需要真实 SQLite/MySQL/PostgreSQL，含已有捐献历史 schema 的升级与第二次迁移，不仅是旧 User/TopUp/Checkin 的启动测试。
- 不新增 `new-api/docs/` 文档，不改品牌、版权或无关文件；保留现有未跟踪截图。

## 组件复用证据

在 `new-api/web` 执行 `bunx --no-install shadcn info --json` 成功。当前 base 为 `base`，style 为 `base-nova`，Tailwind v4，别名 `@/components`、`@/components/ui`、`@/lib`；所需 Dialog、Combobox、Textarea、Input、Switch、Alert、Spinner 已安装，不需要安装新 UI 依赖或改主题。

| 用途 | 现有文件与可复用能力 |
| --- | --- |
| 活动模式 | `web/src/features/donations/components/campaign-form.tsx`：Dialog、RHF、Switch、Combobox、服务端错误、离线关闭活动的既有例外 |
| 审核详情容器 | `web/src/features/donations/components/record-detail.tsx`：用户、活动、key 掩码、原行、组、版本、状态、奖励和历史事件 |
| 通过/拒绝确认 | `web/src/components/confirm-dialog.tsx`：标题/说明、可嵌入 children、加载/禁用状态和确认回调；复用它展示发奖后果或拒绝原因 |
| 列表与筛选 | `web/src/features/donations/records.tsx`、`components/record-filters.tsx`：DataTablePage、服务端分页、状态筛选、错误重试 |
| 状态展示 | `web/src/features/donations/components/item-results.tsx`、`lib/labels.ts`：接收状态、原因与奖励状态分开显示 |
| 权限 | `web/src/features/donations/lib/access.ts`、`web/src/lib/admin-permissions.ts`：管理员身份加显式权限，记录只读权不能获得审核/测试写权 |
| 会话与取消 | `hooks/use-donation-session.ts`、`hooks/use-donation-submission.ts`：AbortController、会话键、旧请求结果隔离、秘密不落 Query variables |
| 流式状态机 | `web/src/features/playground/hooks/use-stream-request.ts`：`createStreamRequestController` 已支持请求代次、SSE 事件、关闭/取消和错误；应评估兼容复用/抽取其控制器，而非复制整个 Playground |

现有 Playground 页面的存储、钱包 Relay 和普通渠道选择不适用于待审核 key，不能直接跳转过去测试。新增 `donations` feature 组件的实际能力缺口是“绑定一笔待审核捐献，并把调用结果和独立审核动作放在一起”；通用输入、确认、布局仍沿用上述组件。

## 建议交互契约

- 活动表单显示“跳过模型测试，改为人工审核”，默认关闭；开启时保留组选择与固定永久奖励设置，并说明通过审核且成功接收后才到账。
- 活动列表/用户捐献页显示审核模式。用户提交后可看到待审核与剩余暂存期，不显示持续自动测试的旋转进度，也不提供给自己审核或测试的入口。
- 管理记录增加待审核筛选和审核状态。详情显示模式、审核决定/操作者/时间/原因、最近测试的结果与目标是否变化。
- 待审核详情可选择当前已绑定组内支持文字对话的模型，输入用户提示词（可选系统提示词），设置有界输出 token，发起流式或非流式测试并查看响应；停止、关闭详情或切换记录会取消调用。
- 一次测试只使用当前 key。测试成功不自动批准；测试失败不自动拒绝。需要以其他方式核实的管理员仍可做明确的人工决定，确认文案显示用户、key 掩码和固定奖励金额。
- 拒绝需要可展示给捐献人的原因；通过可留备注。测试内容仅当前打开详情中显示，审计长期保存模型、时间、结果与关联动作等元数据，不保存完整对话或完整 key。
- 原自动测试已耗尽但仍有暂存 key 的记录，可提供“转人工审核”动作；这是显式逐项决定，不是修改活动后悄悄改变所有旧批次。
- 模式、目标变化、暂存过期、已被其他管理员处理、目标下线、空模型列表、无权限和断流都有明确状态；测试失败不阻止管理员进行仍符合条件的独立审核。
- 仅等待审核时停止前端高频自动轮询；操作后的详情/列表/用户批次失效保持在捐献 API/Query 约定内，不能让晚到的响应覆盖新审核状态。

## 后续验收

现有测试集中在 `web/src/features/donations/__tests__/`。优先扩展 `management.test.tsx`、`submission.test.tsx`、`session-requests.test.ts`；如需拆分新的测试组件职责，只新增对应 `__tests__/review.test.tsx`，不复制 fixtures。

核心行为为模式读写、无写权限时不能操作、待审核及拒绝原因呈现、只测试当前记录、流式完成/停止/断流、切换记录和账号后旧结果不可见、通过/拒绝的竞态与动作恢复、混合批次汇总、目标变化和暂存过期。

实际命令以 `web/package.json` 为准：`bun run test <相关文件>`、`bun run typecheck`、`bunx oxlint -c .oxlintrc.json <变更文件>`、仓库的 `format:check`/`copyright:check` 定向选项，以及 `bun run build:check`。不因 gpt-load 前端无测试而省略 new-api 的必需检查。

本文件是规划研究；没有执行上述功能测试或改动产品代码。
