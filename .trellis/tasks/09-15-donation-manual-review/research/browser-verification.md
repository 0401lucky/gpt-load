# 浏览器验收记录（Claude 续跑）

- 日期：2026-09-15。用户明确授权使用 Playwright MCP 连接本机 Chrome 完成此前被工具策略阻断的本地页面验收。
- 范围：只访问 `127.0.0.1` 上的 fixture 临时库与合成上游；未连接生产数据、未调用生产 cline、未改全局配置。
- 结论：**真实浏览器验收已完成**，并在此过程中发现并修复了一个此前所有自动化测试都未覆盖的**前端崩溃缺陷**。

## 1. 本轮发现并修复的缺陷（阻塞级）

**现象**：普通用户提交人工审核捐献后，整个页面渲染为 500 错误页。

**报错**（浏览器 console）：

```text
RangeError: Invalid language tag: zhCN
    at new RelativeTimeFormat
```

**根因**：`web/src/features/donations/components/item-results.tsx` 的 `DonationIntakeStatus` 把 `i18n.language` 直接传给 `formatTimestampRelative` → `new Intl.RelativeTimeFormat(locale)`。本项目的 i18n 语言码是非标准的 `zhCN` / `zhTW`（`web/src/i18n/config.ts:48` 的 `supportedLngs`），而 `Intl` 只接受 BCP-47 标签（`zh-CN` / `zh-TW`），因此抛 `RangeError`。

**影响面（为何是阻塞级）**：`DonationIntakeStatus` 同时被两处使用，且**只在 `state === 'pending_review'` 且有 `staging_expires_at_ms` 时渲染该分支**，也就是恰好只在“人工待审记录”这一本任务新功能的核心路径上触发：

- `batch-results.tsx:201` —— 捐献人提交结果页
- `records.tsx:135` —— 管理员捐献记录页

即：只要存在任何一条待审记录，捐献人与管理员两侧页面都会整体崩溃。

**为何既有 56 项前端测试未发现**：`web/src/test-setup.ts` 把测试环境的 i18next 固定初始化为 `lng: 'en'` 且资源为空，因此 `i18n.language` 永远是安全的 `en`，`zhCN` 这条真实用户路径在单元测试中不可达。

**修复**：改用项目**既有** helper `toIntlLocale()`（`web/src/i18n/languages.ts:86`）。该函数的文档注释已明确警告过这个坑：

> `new Intl.NumberFormat('zhCN')` throws `RangeError: Invalid language tag`, so any locale derived from `i18n.language` / `i18n.resolvedLanguage` MUST be run through this before it reaches an `Intl` constructor.

`system-instances-panel.tsx:440`、`system-tasks-panel.tsx:187`、`api-keys-columns.tsx:74` 等既有代码都在正确使用该 helper，只有捐献模块这一处漏用。修复与既有约定一致。

**回归测试**：`web/src/features/donations/__tests__/review.test.tsx` 新增用例，显式 `changeLanguage('zhCN')` 后渲染管理记录页并断言待审倒计时可见。用法结束后恢复 `en`，避免影响同文件其余用例。

**修复验证**：前端 `bun run build:check`（`tsgo -b` + `rsbuild build`）通过；捐献测试 57 项通过（原 56 项 + 新增 1 项）。

## 2. 真实浏览器验收结果

环境：`TestDonationBrowserFixture` 真实双进程（gpt-load + new-api，均为最终二进制）、全新临时 SQLite、合成 Gemini 上游、三个角色账号；新前端资源嵌入 new-api 二进制。

| 验收点 | 结果与页面证据 |
| --- | --- |
| 普通用户提交人工活动 | 提交 3 个 key 成功，页面**不再崩溃**，显示 `3 个 key 正在等待人工审核`、`已接收 0/3`、`$0`；每行 `待人工审核` + `暂存到期时间 2026-09-22 17:02:23 (7天后)` + `无奖励`（AC2、AC6、R6） |
| 管理员记录页 | 记录页正常渲染（修复前此页必崩），筛选/分页组件可用；三项均显示待审与倒计时 |
| 非流式真实调用测试 | 详情内选择 `gemini-donation-test`、填入提示词、`发起测试` → `测试成功`，**审核决定仍为“待人工审核”**（AC3、AC4：测试成功不自动通过） |
| 流式真实调用测试 | 开启流式开关后 `发起测试` → `测试成功`，响应渲染为 `manual reply [redacted] done` —— **上游回显的 key 已被脱敏为 `[redacted]`**（AC3、AC7 跨帧脱敏） |
| 测试失败不自动拒绝 | 对上游返回 401 的 key 发起测试 → `测试失败` + `模型测试未成功完成，不能仅据此认定 key 不可用`，状态仍为待人工审核、`凭据 ID —`、`无奖励`（AC4） |
| 停止测试 | 测试运行中按钮为 `停止测试`（`发起测试` 禁用）→ 点击后显示 `测试已取消`，记录起止时间，按钮恢复可用；未产生部分成功结果（AC8） |
| 人工通过 | 确认框显示捐赠人、key 掩码、`$1` 奖励与可选备注 → 确认后 `审核决定 已通过`、操作者 `管理员 #1`、原因已记录，历史显示 `操作已生效`；列表变为 `凭据 3 已接收 奖励已到账 永久额度：$1`（AC2、R5） |
| 人工拒绝 | 拒绝原因必填（未填时按钮禁用）→ 填写后 `审核决定 已拒绝`；列表变为 `审核未通过 无奖励` |
| 仅记录只读管理员 | 可查看记录元数据与审核/测试历史；打开详情仅有 `Close` 按钮，**无“通过/拒绝”、无模型测试面板、无流式开关**（AC6、R8） |
| 捐献人查看拒绝原因 | 本人批次详情显示 `••••ject 审核未通过 该密钥上游返回未授权，无法通过人工核实`；**未显示管理员备注**（AC6、R8 边界） |
| 旧 retry_exhausted 转审 | 提交限流 key 至自动活动 → 重试耗尽显示 `可重试 / 自动校验重试次数已用完`；详情出现 `转人工审核` 按钮，确认文案说明“保留原提交内容、截止时间与奖励”；执行后 `校验模式 人工审核`、`审核决定 已转人工审核`、操作者与时间已记录，**原暂存截止时间 17:14:48 保留**，并出现通过/拒绝/测试入口（AC9、R10） |
| 深色主题 | 切换后 `html class="dark"`，待审记录与倒计时正常渲染 |
| 窄屏（390×812） | 捐献页与记录页 `scrollWidth === clientWidth`，**无横向溢出**；记录表格转为卡片布局；审核弹窗宽 358px、内部无溢出，`通过/拒绝/发起测试` 均可用 |
| 键盘操作 | 登录表单 Enter 提交可用；详谈及确认弹窗 Escape 关闭可用 |

## 3. 证据文件

截图位于本任务 `research/evidence/`：

- `evidence-01-zhCN-crash-500.png` —— 修复前的崩溃页
- `evidence-02-user-pending-review-fixed.png` —— 修复后捐献人待审视图（含 `(7天后)`）
- `evidence-03-admin-records-pending.png` —— 管理员记录页待审列表
- `evidence-04-test-nonstream-success.png` —— 非流式测试成功
- `evidence-05-test-stream-redacted.png` —— 流式测试成功与 `[redacted]`
- `evidence-06-approved-rewarded.png` —— 通过后 `已接收 / 奖励已到账 $1`
- `evidence-07-test-cancelled.png` —— 停止测试结果
- `evidence-08-readonly-admin-no-actions.png` —— 只读管理员无写操作
- `evidence-09-donor-sees-reject-reason.png` —— 捐献人看到拒绝原因
- `evidence-10-dark-theme-pending.png` —— 深色主题
- `evidence-11-narrow-donor.png`、`evidence-12-narrow-admin-review.png` —— 窄屏
- `evidence-13-enter-review.png` —— 转审结果

测试/构建日志：

- `web-build-zhcn-fix.log`、`web-test-zhcn.log`、`web-test-zhcn-2.log`、`go-build-fix.log`（隔离目录）
- `browser-claude.log`（修复前 fixture）、`browser-claude-fixed.log`（修复后 fixture）

## 4. 边界与未覆盖项

1. 浏览器验收只覆盖 fixture 的合成上游与临时 SQLite，不构成生产环境、真实上游或未实测最低数据库版本的验证。
2. 拒绝原因长度上限、拒绝后暂存材料清理的数据库侧效果未在页面层逐项断言（分别由组件测试与后端测试覆盖）。
3. Console 中出现的 401 均为登出后页面轮询产生的预期响应，非缺陷。
4. 全仓 `copyright:check` 在 22 个本任务未改文件上失败的既有状况未改变，本轮未顺带改动无关文件。
