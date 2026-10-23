# 兼容合并上游 main 的最新提交

## Goal

将 `upstream/main` 相对本地 `main` 的唯一新提交兼容地带入本地分支，使经典版和新版管理界面的订阅余额按上游约定显示为完整积分。

## Confirmed Facts

- 本地 `main` 与 `upstream/main` 的共同祖先为 `41c82ac0`，本地领先 45 个提交，上游领先 1 个提交。
- 上游提交为 `dd33ae390c23575988b04b3f551873ae70f3eba0`（`fix(ui): round credit balances up to whole points (#899)`）。
- 上游提交只修改两个文件：
  - `web/src/frontends/classic/features/groups/credentials/SubscriptionAccountCard.vue`
  - `web/src/frontends/modern/features/groups/SubscriptionCredentialCard.vue`
- 两处修改都把正数余额的显示从 `n(balance, { maximumFractionDigits: 20 })` 改为 `n(Math.ceil(balance))`；无限额、非有限值和非正值的现有行为保持不变。
- 工作区已有未跟踪目录 `.playwright-mcp/`、`.trellis/tasks/09-28-credential-probe-real-chat/`，合并过程必须保留。

## Requirements

1. 将上游提交 `dd33ae3` 带入当前 `main`，优先保持其提交语义；若实际应用时出现冲突，只调整本地差异以实现同等行为。
2. 经典版和新版订阅凭据卡片对正数余额均向上取整后本地化显示。
3. 不回滚、覆盖或清理与本次合并无关的现有工作区内容。
4. 合并后检查工作树、提交历史和差异，确认只引入上游提交对应的变更及必要的兼容调整。

## Acceptance Criteria

- [x] `dd33ae3` 已作为真实 merge 的父提交带入本地提交 `a3c71d5c`。
- [x] 经典版与新版两个目标文件都使用 `Math.ceil(balance)` 显示正余额。
- [x] 无限额、非有限值、零值和负值分支行为未被改变。
- [x] `git diff --check` 通过，相关前端类型检查/构建验证通过。
- [x] 最终报告列出实际应用的提交、验证命令和任何未验证范围；详见 `tmp/upstream-merge-quality-20261023.md`，不声称全量 Go 测试或 make check 通过。
