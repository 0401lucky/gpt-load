# new-api 捐献前端实现交接

日期：2026-09-14。目标为 `D:/code/Claude code program/new-api/web/src`；后端契约见 rewards-backend 的 implementation-handoff.md。本子任务没有修改 Go 生产代码、legacy web/default、现有 clover 图片或部署配置，未提交或发布。

## 页面与入口

- `/donations`：现有登录保护下的活动选择、多行 key 输入、风险小字、当前批次逐行结果、永久到账汇总和本人历史。
- `/donations/settings`：独立 `donation_config.read/write` 控制连接读取/保存、活动列表/编辑。只显示连接 URL、已配置状态和实例 ID，保存 token 后清空输入；活动只包含名称、说明、真实组、固定永久奖励和开关。
- `/donations/records`：独立 `donation_records.read` 控制管理记录，支持用户/活动/组/接收状态/奖励状态/日期/记录 ID/凭据 ID 筛选、分页及详情，保留原用户、行号、掩码、组、凭据、奖励与处理事件。
- 三条路由位于 `_authenticated/donations`，复用原登录返回 `/sign-in?redirect=/donations`，管理路由额外作权限 guard；组件也同步响应权限变化。
- `HeaderNavModules` 增加默认开启的 `donations` 布尔项，兼容旧配置；顶部导航设置可以控制显示。捐献导航沿用原登录跳转，不增加风险确认弹窗。
- AppHeader 原先外层 `hidden lg:block` 隐藏了 TopNav 自带的移动菜单，本次移除该遮挡并复用原菜单。应用导航在窄屏折叠，公共导航在平板/手机折叠；公共菜单补 aria-expanded/aria-controls 及关闭时 inert/aria-hidden。

## 复用与风格

采用已有 Public Sans/Lora、语义颜色、卡片、字号和间距体系。没有新建通用 UI 库或引入依赖。

- 页面：`SectionPageLayout`；导航链接继续使用 TanStack Link、现有 TopNav/PublicHeader/AppHeader。
- 表单：React Hook Form、Zod、共享 Form/FormField/FormControl/FormMessage、FieldGroup、Input/Combobox/Switch/InputGroup/Textarea。接收组使用已有 Combobox options 模式，不能手工输入新组 ID。
- 状态/弹层：共享 ErrorState、EmptyState、LoadingState、Dialog、StatusBadge、CopyButton。
- 表格：本人历史、逐项结果和管理记录使用 `DataTablePage/useDataTable`；少量配置/事件数据使用 `StaticDataTable`。
- 管理筛选复用现有 `LogsFilterToolbar/LogsFilterField/LogsFilterInput` 以及 `CompactDateTimeRangePicker`，保留现有桌面/移动行为。
- 金额复用 `formatQuota/parseQuotaFromDollars/quotaUnitsToDollars`。编辑其他字段时保留未修改的原始整数额度，避免显示舍入改变奖励；最大安全整数额度的改名回归已覆盖。

## 隐私与恢复

`features/donations/api.ts` 始终使用统一 api 客户端，并复制发起时的用户 ID/session SID。它在预刷新前订阅登录变化，变化时同步 abort；预刷新后、响应后再次核对身份，设置 skipAuthRefresh/skipErrorHandler，禁止共享 401 拦截器用另一会话自动重放密钥或连接写入。返回错误仅保留安全错误码/消息/状态，丢弃含请求正文的 Axios config/cause。

查询键按用户和 SID 隔离，页面工作区也以会话键重建；GC 不长期保留无人使用的捐献查询。登录切换及卸载会清空表单与私密引用，迟到结果不能进入新账号页面。

密钥和集成 token 只在必要的表单/请求内存中存在。Mutation 变量至多包含非秘密的 request_key/代次，正文留在单独的当前请求引用中，不写 URL、localStorage、sessionStorage、IndexedDB 或日志。confirmed/local_only 后清空原文；后续 GET 确认也会立即清空并取消旧传输。

未确认请求保留原 UUID/原活动/原文本以便重放。刷新后可从本人历史选择未确认批次，重新输入原内容和原行位置，自动携带服务器保留的 request_key；不生成另一份奖励请求。已确认的 retry_pending 项使用无 key 的原动作请求，丢响应时沿用同一动作 UUID。

提交状态与奖励状态分别显示，依据服务器逐项结果和汇总，不把队列/处理中或部分成功显示为全成功。没有领奖数量上限、有效期、签到桶或追回操作。

独立审查发现并修复了乱序回执边界：当前请求单独保留非秘密 request_key/代次/confirmed 事实，迟到 POST 不能覆盖先到的 GET 确认，也不能影响用户后来打开的另一份恢复表单。新增 3 个竞态回归和 1 个说明长度回归先失败后通过，见 `review-regressions-before.log` / `review-regressions-after.log`。后续再补充“已确认批次刷新不得清掉下一份草稿”的失败→通过回归（`draft-regression-before.log` / `draft-regression-after.log`），确认后的清理只执行一次。

活动说明按 UTF-8 不超过 4000 字节校验，与冻结后端一致；错误文案只提示缩短说明。

## 文案

所有新增文本使用英文源 key 和现有 `{translation:{...}}` 扁平命名。七语增补由独立代理完成：每种语言保留原 6762 条，新增 156 条，总计 6918 条；占位符、重复键、UTF-8、非英文占位值和精确 R18 已校验，详见 `i18n-verification.md`。

中文风险提示在输入旁、提交按钮之前可见，无附加勾选或弹窗：

> 请勿使用主账号的 API key。捐献后的使用可能因平台风控或其他原因导致账号受限、封禁，或 key 失效，请确认能承担相关风险。

重复资源状态使用 `Duplicate key`，避免复用其它语言中表示“复制/克隆”动作的旧 `Duplicate` 译文。API/schema/后端权限目录动态文案在 static-keys.ts 注册。

## 验证与既有门禁问题

工作目录统一为 new-api/web，Node 24.10.0、Bun 1.3.8。测试使用 Vitest/RTL 与真实 Axios 拦截器，网络/浏览器边界可控；没有 mock 业务模块。

- 最终 `bun run test src/features/donations/__tests__ src/components/layout/__tests__/donation-navigation.test.tsx --maxWorkers=2`：**5 文件、24 用例全部通过**，35.96s（`final-tests.log`）。jsdom 的 scrollTo 提示属于测试环境缺失浏览器 API，不是用例失败，实际布局/滚动由主会话浏览器验收。
- 新增会话切换、预刷新期间切换、401 不重放及错误去除请求正文；混合逐行结果、原请求/原动作恢复、迟到结果、权限、失败分组保留、永久奖励、精确整数、移动导航、原登录返回和 R18 验证。
- 独立审查 4 个新增失败用例已修复并通过。
- 最终 `bun run typecheck` **退出 0**（`final-typecheck.log`）；导航测试使用真实 RootRoute 启动 guard、同名 `_authenticated` pathless 父路由及 donations 子路由，未通过 unknown cast 绕过类型。
- 本次修改/新增文件的定向 oxlint **退出 0**（`touched-lint.log`）。最终 `bun run lint` 仍报告 239 条错误、涉及 121 个未修改文件，与本次修改集合交集为空（`final-lint.log`）；不扩大重构到 legacy default/auth/assets 等历史范围。
- 已完成顺序运行 `bun run format:check` 和 `bun run copyright:check`：全局仍有 **92 个格式项、22 个版权项**，全部属于会前 **98/22** 基线集合；本次修改/新增文件与问题集合交集均为空，没有新增问题。日志 `format-check.log`、`copyright-check.log`。
- 定向格式化保留原版权头，仅处理本次文件；没有执行全仓 format 修复。全局 format:check 的快照/恢复期间没有并行源码编辑或构建。
- 最后使用 Oxfmt `format()` 的只读 API，去除并保留版权头后检查全部 **45 个本次 TS/TSX/locale 文件**：格式差异 0、缺版权 0（`touched-format-copyright.json`）。`git diff --check` 通过。该最后检查不写文件，不与独立审查争用快照或构建缓存。
- 最终 `bun run build:check`（tsgo + Rsbuild）**退出 0**（`final-build.log`，Rsbuild 32.3s）。最终 dist 已通知主会话稳定，主会话可重建 Go embed binary 进行真实桌面/移动验收；本子任务不再写 dist。最新主要入口为 `index.79c996e12c.js`。

已知边界：只有未确认元数据而原文丢失时无法自动补交，需要原内容恢复；这是后端不保存明文的既定行为。服务响应、历史和奖励状态仍以服务器授权结果为准。
