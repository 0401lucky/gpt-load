# new-api 捐献前端完整复核

日期：2026-09-14。角色：独立 `trellis-check`。目标代码为 `D:/code/Claude code program/new-api/web`；任务记录保存在 gpt-load。本轮保持生产源码只读，将行为修复交给实施方，另按明确分工补充七语提示；未修改 Go、clover 图片、全局设置或提交代码。

## 结论

**本次前端改动的代码与定向质量检查通过。** 审查发现的三个恢复/输入边界已修复，独立最终运行 5 个文件、24 个测试全部通过，完整类型检查和 38 个变更 TS/TSX 文件的 lint 通过。

这不等于全仓历史门禁全部通过：完整 lint、格式和版权检查仍有未改动文件的问题，完整列表和范围核对见下方。最终构建上的桌面/移动端、管理员及跨账号真实浏览器验收由主会话完成，本报告不把旧预览当作最终 UI 验收。

## 上下文与范围

已读取当前 `check.jsonl`、PRD/design/implement、后端实际 API handoff，沿用 new-api 根及 web `AGENTS.md`、项目 shadcn-ui 入口和组件复用约定。检查覆盖捐献页面/API/hooks/types/schema/labels、全部管理表单与记录、路由、公共/应用/移动顶栏、导航配置解析/保存、admin-permissions、生成路由接入、静态文案登记和七语 JSON。

本次共 46 个变更 web 文件：38 个手工 TS/TSX、1 个生成路由文件、7 个 locale。没有将 gpt-load 的 Vue/pnpm/后端测试规则套到这里。

## Findings (fixed)

### 1. 迟到 POST 不能撤销 GET 确认或影响另一份恢复输入

- 文件：`src/features/donations/hooks/use-donation-submission.ts`、`__tests__/submission-recovery.test.tsx`。
- 原行为：GET 已确认后清空原 key/ref，迟到的旧 POST 仍可能重新设为 unconfirmed，留下被禁用的空输入框；用户切换到另一份待恢复提交后，旧成功/错误也可能改动当前表单。
- 修复：活动提交保留非秘密 request_key、generation 和 confirmed；在飞行中的正文单独保存。确认或切换恢复意图时取消旧传输，所有回调先核对当前标识/代次/确认事实。Mutation 变量仅包含非秘密标识，不含 key。
- 三个针对实际异步顺序的用例先失败后通过；证据见 [review-regressions-before.log](review-regressions-before.log)、[review-regressions-after.log](review-regressions-after.log)。

### 2. 后续轮询不能清空下一份未提交草稿

- 同一 hook 的首次修复仍会在同批次每次 GET 更新时重复清空表单。原提交已确认后，用户刚输入的下一份草稿可能被原批次验证/到账进度覆盖。
- 独立重读后补充 `active.confirmed` 的首次确认保护：只清理原输入一次，已确认事实继续用于阻挡旧回调。
- “已确认 → 输入新草稿 → 原批次后续 GET” 用例已先失败后通过；见 [draft-regression-before.log](draft-regression-before.log)、[draft-regression-after.log](draft-regression-after.log)。

### 3. 活动说明的长度边界与后端不一致

- 文件：`src/features/donations/lib/schema.ts`、`__tests__/management.test.tsx`、七个 locale。
- 原 `.max(4000)` 按 JS 字符数检查，而后端限制 4000 UTF-8 字节。例如 1334 个“界”前端通过、后端拒绝。
- 修复：使用 `TextEncoder` 检查实际 UTF-8 长度；提示改为 `Please shorten the campaign description.`，七语均已补齐，未改后端约束。
- 超界及边界测试纳入回归；原 4000 字符提示的旧译文保留，当前调用使用新提示。

导航测试的库类型问题由主会话与实施方修复：使用真实 root bootstrap guard、同名 pathless authenticated 父路由及子路由，去掉不相容 cast；Promise gate 与 RTL 查询也按当前库类型处理。独立类型检查和完整相关测试已确认通过。

## 实际代码路径核对

| 项目 | 结论 |
| --- | --- |
| 导航和登录返回 | 顶部内建 `/donations` 接入公共及应用顶栏；旧 HeaderNavModules 缺省启用，显式 false 可保存。移动端复用现有菜单并补可访问状态，保护路由沿用真实登录返回逻辑，没有新建捐献登录或确认流程。 |
| 组件复用 | 布局用 SectionPageLayout；数据表用 DataTablePage/useDataTable/StaticDataTable；弹窗、状态、表单、Combobox、复制按钮复用既有组件。记录筛选复用现有日志工具栏和时间范围选择器，没有另造通用交互。 |
| 权限与账号隔离 | 设置/记录分别检查 config.read/write、records.read，并有页面和路由边界；查询键及工作区按 userId+SID 隔离，切换账号会取消旧请求、清空原表单与私密引用。真正授权仍由后端执行。 |
| 请求与错误 | 订阅身份变化后再预刷新认证，发送前/返回后重核对；AbortSignal 配合 skipAuthRefresh 禁止跨会话或 401 后自动重放秘密写入。错误只保留安全代码/文案/状态，不把 Axios config/cause 交给缓存或错误提示。 |
| 明文生命周期 | key/token 仅在表单与必要请求内存中；没有进入 URL、query/mutation 数据、localStorage/sessionStorage/IndexedDB 或日志。确认后清空原文，新草稿不会再被旧查询清掉。 |
| 接收与奖励 | 逐项 intake/reward 分开，显示真实行号、脱敏 key、失败原因及实际已到账额度；汇总来自后端，不以 HTTP 成功或部分接收冒充整批完成。待奖励的 accepted 项继续查询。 |
| 原标识恢复 | 未确认请求保留原内容和请求 UUID；历史恢复使用服务器保存的 request_key 与冻结活动。已确认 retry 使用无 key 正文的新动作 UUID，丢响应后保留动作身份。关闭活动不改用另一份规则。 |
| 管理分组/连接 | 分组来自后端真实投影，禁用/不可验证项不可选；空组可用。刷新失败保留选择与明确错误，不伪装为空列表；关闭既有活动可在目标不可用时保存。已保存的 token 不回显，连接反馈对应真实验证。 |
| 记录及金额/时间 | 用户、活动、组、状态、凭据、记录 ID 和时间筛选与实际 DTO 一致，分页从 1 开始。时间显式按毫秒处理；金额使用现有显示/换算路径，改名/关闭时保留未改动的原始精确整数奖励。 |
| 永久规则与 R18 | 没有奖励到期、业务领奖上限、签到额度或追回入口。R18 在 key 输入区附近、提交前可见，正常提交无需额外弹窗或勾选；中文原文及变量完整性已校验。 |
| i18n | 当前 182 个源键七语齐全。每语新增 156 条、共 6918 条，旧 6762 条逐项不变；UTF-8、JSON 重复键、占位符、非英文占位值均检查通过。详见 [i18n-verification.md](i18n-verification.md)。 |

## Verification

工作目录均为 `D:/code/Claude code program/new-api/web`，Node 24.10.0、Bun 1.3.8；测试使用 `--maxWorkers=2`。

- **Tests: pass**：独立运行 `bun run test src/features/donations/__tests__ src/components/layout/__tests__/donation-navigation.test.tsx --maxWorkers=2`，5 文件 / 24 用例通过，88.49s，退出码 0；见 [check-tests.log](check-tests.log)。JSDOM 的 scrollTo 未实现提示不代表应用测试失败；实际视口由主会话浏览器验证。
- **TypeCheck: pass**：独立 `bun run typecheck` 退出码 0；见 [check-typecheck.log](check-typecheck.log)。
- **本次文件 lint: pass**：独立 `oxlint -c .oxlintrc.json <38个新增/修改TS/TSX文件>` 退出码 0，无输出；自动生成的 routeTree.gen.ts 沿用生成器边界。
- **构建: pass**：已核对实施方 `bun run build:check` 最终退出码 0 与 [final-build.log](final-build.log)。独立复核未重复写入主会话正在使用的 dist。
- **七语格式: pass**：只对七个 JSON 运行原生 `oxfmt --check`；未运行会快照/写回全仓的格式脚本。
- **变更文件格式/版权: pass**：另已核对实施方对 45 个手工 TS/TSX/locale 文件的只读 Oxfmt API 比对，格式差异 0、版权缺失 0；见 [touched-format-copyright.json](touched-format-copyright.json)。

## Findings (not fixed) 与剩余验收

1. **全仓历史 lint**：独立 `bun run lint` 退出码 1，共 239 errors / 65 warnings；完整逐项位置见 [check-global-lint.log](check-global-lint.log)。包括旧 `default/src`、图标、其它功能与共享文件；路径集合与本次 46 个变更 web 文件的交集为零。本任务不扩大为这些未改动代码的重构。
2. **全仓历史格式/版权**：格式仍有 92 项、版权 22 项，均在实施前 98/22 基线集合内，与本次变更路径交集为零；完整列表见 [format-check.log](format-check.log)、[copyright-check.log](copyright-check.log)。实施方按顺序完成检查，本复核未在编辑或构建中执行全仓格式写回。
3. **最终真实浏览器**：本独立代码复核结束时交由主会话继续。主会话随后使用最终构建完成桌面/移动入口、R18可读性、管理员连接/活动/记录、账号切换和混合结果恢复，全部通过；最新证据见父任务 [browser-validation.md](../../09-14-key-donation-rewards/research/browser-validation.md)。预览记录仍单独保留，最终结果没有以预览代替。

没有其它未修复的本功能生产发现。后续源码如再变化，应针对变化补复核；当前未更改需求或提出新产品决策。

## 后端补充测试只读复核

按主会话补充要求，独立阅读 `model/donation_retention_test.go`，没有修改 Go 或重跑已完成的数据库矩阵。

- 备份用真实 SQLite `VACUUM INTO` 包含已提交 WAL 数据，再复制到另一个 TempDir、以独立 GORM 连接打开；验证指纹、可解密 token、旧账本/余额，以及更换地址/token 后跨用户重复与不重复发奖。没有用空账本或单纯内存对象代替恢复。
- 过期用例只在私有 SQLite 中映射 Checkin 固定表名，以历史时间数据调用真实 `GetActiveTemporaryQuota`，再删除该用户的过期桶并核对永久余额、历史和重放。Redis 状态保存/恢复，不改全局时钟；数据库句柄和临时文件由 fixture 清理，不触及外部数据库。
- 两个测试有效且隔离；实际运行结果沿用主会话的 [retention-verification.md](../../09-14-donation-rewards-backend/research/retention-verification.md)：SQLite 3.50.4 两项通过、model 全包通过及 go vet 通过。
