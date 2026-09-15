# Codex 前端复核

- 日期：2026-09-15。
- 范围：`../new-api/web` 的捐献活动、审核、真实调用、管理历史、权限和七语文案。没有修改后端、gpt-load 产品代码或共享任务状态。
- 结论：本范围已发现的确定缺陷已修复；最终捐献前端测试 **5 files / 56 tests passed**，lint、类型检查、定向格式检查和生产构建通过。真实两进程及浏览器验证由主会话另行记录。
- 任务仍交主会话审查；本代理没有提交、推送、部署、归档或运行 finish。

## Findings (fixed)

### 1. 审核和测试请求不符合真实后端 JSON 白名单

- File：`web/src/features/donations/api.ts`、`types.ts`、`components/record-test.tsx`、`hooks/use-donation-test.ts`。
- Issue：审核 JSON 多发 `action_id`、测试 JSON 多发 `test_id`，会被 `controller/donation.go` 的严格解析直接拒绝；测试还未发送必需的 `expected_item_revision` 和 `review_target_revision`。原 axios 模拟没有保护这个边界。
- Fix：UUID 仅通过 `Idempotency-Key` 发送；两种测试都携带当前详情的期望版本和目标。TypeScript 将期望版本改为必填；回归用例核对真实字段及没有多余 ID 字段。没有要求后端放宽白名单。

### 2. 转审入口与拒绝入口错误绑定

- File：`components/record-review.tsx`、`types.ts`、`__tests__/fixtures.tsx`。
- Issue：依据 context 的 `effective_mode` 推断是否已经转审；交接中的临时语义会使旧自动项失去转审入口。`can_review=false` 又会把目标变化/离线时仍应允许的拒绝一并隐藏。
- Fix：经主会话与两端审核者确认，context 的 `effective_mode` 如实表示条目有效模式；入口分别使用 `review_action` 和独立 `can_reject`。转审/通过都绑定用户看到的 `review_target_revision`，拒绝不发送目标修订。测试进行中、未知审核结果期间不提供相反动作。

### 3. 未知审核结果没有冻结原意图，也不能从已确认历史收敛

- File：`components/record-review.tsx`、`components/record-detail.tsx`、`api.ts`。
- Issue：原代码只保留 UUID，备注/上下文仍可改变，重新打开确认还能产生新 UUID；pending 被当成完成。后台已经确认的原操作也不能自动清除本地“等待确认”。
- Fix：冻结 UUID、原操作者、kind、版本、目标和备注。未知结果时只允许检查原动作；查到 pending 不写入，原动作确实不存在时仅原操作者以原 UUID/原内容重放。重开详情从 `pending_review_action` 恢复；只由同 UUID、同操作者的可信终态解除等待，不根据一个空 pending 字段推断已完成。后台恢复后的可信 `recent_review_actions`/最终动作可收敛界面，官方最终动作优先于本地较早的转审结果。错误保存安全消息键/业务码，渲染时翻译。

### 4. 真实调用的停止、协议绑定与资源清理不足

- File：`hooks/use-donation-test.ts`、`lib/schema.ts`、`components/record-test.tsx`。
- Issue：停止后只有 running=false，没有非成功说明；done 后不主动关闭流；meta/done 直接类型断言，无法拒绝缺 meta、错 item、超限输出或不完整成功声明。流式请求收到已保存 JSON 元数据时没有恢复路径。
- Fix：保留会话及代次守卫，取消/结束关闭流、AbortController 和总时限定时器；停止保留部分正文并显示取消。使用 Zod 校验白名单事件，绑定 test/batch/item/model/revision/target，限制事件及累计文本大小；没有完整终态和有效输出不显示成功。JSON 重放只通过 GET 读取原元数据，不再次调用模型，明确正文不保留。正文不进入 Query/Mutation 缓存、localStorage 或历史 DTO。

### 5. 活动关闭例外、备注校验和结果文案

- File：`components/campaign-form.tsx`、`components/record-review.tsx`、`components/item-results.tsx`、`lib/labels.ts`。
- Issue：离线关闭活动的例外也允许切换模式；批准备注没有 UTF-8 上限；暂存过期显示 Invalid；`review_rejected` 被显示为临时处理故障；测试原因直接展示内部码。
- Fix：关闭例外要求模式和目标都保持原值；通过/拒绝都校验备注字节上限；过期明确显示“临时存储过期”，拒绝显示已确认原因，测试原因使用安全本地化说明。补充上游配额/站内钱包及停止不撤销已发生用量的说明。活动测试覆盖 `can_probe=false` 而可人工审核的分组。

### 6. 缺失管理历史与最近测试恢复

- File：新增 `components/record-history.tsx`，以及 `components/record-detail.tsx`、`components/record-review.tsx`、`types.ts`。
- Issue：最近测试只有当前窗口内状态，重开会误报从未测试；没有 PRD R6 要求的审核/测试历史。
- Fix：与调用方后端同步 `latest_test`、`pending_review_action`、`recent_review_actions`、`recent_tests` 安全投影。复用 StaticDataTable 展示各最近 20 条；展示操作者、命令结果、备注、模型和时间。命令“未生效”与捐献“被拒收”分别表达；备注当作普通文本；历史没有提示词或回复。过期目标的最近测试明确标记。待人工审核保持空闲，正在测试、审核恢复或接收/发奖中的详情继续只读轮询。

### 7. 测试质量和七语完整性

- File：`__tests__/management.test.tsx`、`__tests__/submission.test.tsx`、`__tests__/review.test.tsx`、`__tests__/fixtures.tsx`，七份 locale 和 `static-keys.ts`。
- Issue：初跑 39 项中的“Access Forbidden”断言在路由首帧元素尚不可见时失败；描述字节上限用例缺少新增必填模式，可能因错误字段而通过。新增权限 catalog 文案没有七语登记。
- Fix：等待明确的可见状态，不使用 sleep；补齐描述测试的有效其他字段；复用原 fixtures 扩充真实 DTO、恢复、取消、断流、边界、会话和历史用例。补齐新增权限、错误和历史文案。最终扫描 246 个相关页面/权限键，七语均无缺项。

## 组件与安全边界复核

- 已读 live `new-api/AGENTS.md`、`web/AGENTS.md`、项目 shadcn-ui skill 以及 Trellis 注入的 check 上下文、PRD、design、implement 和交接资料。
- `bunx --no-install shadcn info --json` 成功：Base UI / base-nova / Tailwind v4。复用现有 Dialog、ConfirmDialog、StaticDataTable、Combobox、Form、Switch、Textarea、StatusBadge；未引入依赖或自建通用弹窗/表格。
- 读取并评估既有 `playground/hooks/use-stream-request.ts`：其 controller 固定为 ChatCompletionRequest、OpenAI message/done 解析及普通 Relay 路径，不能直接承载捐献的 meta/delta/done 与元数据响应。保留捐献专用 hook，复用已安装 sse.js 及捐献会话生命周期，没有改动共享 Playground。
- 参考 OWASP ASVS 官方入口（当时 latest stable 为 5.0.0）和 Authentication / Session Management Cheat Sheets；后两者页面返回 403 后读官方 OWASP/CheatSheetSeries 源文件。适用复核点为原会话绑定、退出/换账号取消、读写权限分离和凭据/正文不进入普通缓存。没有修改登录协议，也不声称全站已通过 ASVS。
- 参考地址：[ASVS](https://owasp.org/www-project-application-security-verification-standard/)、[Authentication Cheat Sheet 官方源文](https://raw.githubusercontent.com/OWASP/CheatSheetSeries/master/cheatsheets/Authentication_Cheat_Sheet.md)、[Session Management Cheat Sheet 官方源文](https://raw.githubusercontent.com/OWASP/CheatSheetSeries/master/cheatsheets/Session_Management_Cheat_Sheet.md)。未据此声明某条 ASVS 需求完成，故没有引用未经逐条验证的 requirement ID。
- 普通用户无管理入口；只读记录管理员可以读安全历史，但不能测试或审核。服务端权限、三库和真实 key 调用由对应后端/主会话独立验证，前端门禁不替代这些验证。

## Verification

所有前端命令工作目录均为 `D:/code/Claude code program/new-api/web`。

| 检查 | 实际结果 |
| --- | --- |
| `bun run test src/features/donations/__tests__ --maxWorkers=2` | 最终 **5 files / 56 tests passed**；14:34:12 开始，62.23s。先前新增的 8 个核心回归、过期、历史缺失、后台恢复状态各有明确失败后修复的记录。 |
| `bun run typecheck` | 已独立通过；最终构建中的 `tsgo -b` 也通过。 |
| `bunx --no-install oxlint -c .oxlintrc.json src/features/donations src/lib/admin-permissions.ts src/i18n/static-keys.ts` | 最终 exit 0，无 lint error。 |
| `bunx --no-install oxfmt --check <本次变更的前端文件>` | 最终 29 files 通过。文件集由限定 web 路径的 `git diff --name-only` 与 `git ls-files --others --exclude-standard` 合并去重得到。未运行全仓格式重写。 |
| `bun run build:check` | 最终 exit 0；Rsbuild 2.1.6，构建 12.0s；当前产物 `dist/index.html` 引用 `index.2228d68442.js`。 |
| 七语键扫描 | 捐献生产代码中的 literal t 键及新权限 catalog 键，共 246 个；`en/zh/zh-TW/fr/ru/ja/vi` 无缺项。 |
| 定向版权检查 | 22 个变更 TS/TSX 文件均匹配仓库脚本的 canonical copyright header。 |
| `git diff --check -- web` | 通过。 |

`format:check` 包装脚本没有定向参数，且其 check 实现会临时改写后恢复全仓文件，故用已安装 oxfmt 的定向 check；版权脚本也没有定向参数，已实际运行全仓 check 并另行核对本范围。

测试中的 jsdom `Window.scrollTo` 提示不影响通过结果；没有通过新增 skip、关闭断言或替换生产模块消除失败。

## Findings (not fixed)

1. **全仓 `bun run copyright:check` 失败于 22 个本次未改文件。** 此项仍是 fail，不能写成全仓通过；定向 22 个本次变更 TS/TSX 文件已通过。对以下路径执行 git diff quiet 未发现本任务改动，未顺带修改：

   - `src/components/ai-elements/response-fade.ts`
   - `src/components/model-group-selector-layout.ts`
   - `src/features/active-tasks/types.ts`
   - `src/features/auth/lib/oauth-callback-mode.ts`
   - `src/features/channels/lib/__tests__/channel-form.test.ts`
   - `src/features/channels/lib/channel-field-update.ts`
   - `src/features/channels/lib/model-categories.ts`
   - `src/features/dashboard/components/users/user-charts.tsx`
   - `src/features/dynamic-ratio/types.ts`
   - `src/features/fingerprints/types.ts`
   - `src/features/model-health/types.ts`
   - `src/features/profile/components/__tests__/login-session-utils.test.ts`
   - `src/features/recent-calls/types.ts`
   - `src/features/security/components/__tests__/login-session-utils.test.ts`
   - `src/features/system-settings/lib/checkin-time.ts`
   - `src/features/task-plugins/__tests__/plugin-icon-image.test.tsx`
   - `src/features/task-plugins/components/javascript-viewer.tsx`
   - `src/features/task-plugins/components/plugin-detail-sheet.tsx`
   - `src/features/task-plugins/lib/host-protocols.ts`
   - `src/lib/chunk-reload.ts`
   - `src/lib/localized-text.ts`
   - `src/lib/session-hint.ts`

2. **真实浏览器/双服务验证不是本代理的执行结果。** 已交主会话使用最新 dist 验证管理员、普通用户、只读管理员、窄屏/主题、真实 HTTP 调用和发奖。本文只证明前端静态检查与受控组件/请求测试，不声称生产 cline 或真实上游可用。

本范围没有保留已确认但未修复的捐献前端代码缺陷。保留全部代码和测试，未触碰既有 `clover-*.png`。
