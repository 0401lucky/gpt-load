# Codex 续审：人工审核回执阻塞

- 日期：2026-09-15；Go `go1.25.5 windows/amd64`。
- 范围：new-api 的回执应用、审核事实绑定、奖励边界及现有 model/service 捐献测试。未修改 gpt-load 产品代码、controller 联调设施、前端或公共规范。
- 任务保持 `in_progress`；未提交、推送、部署、归档或执行 finish。

## 已修复：决定事实与命令枚举混用

`manual-integration-windows-5.log` 的真实回执包含 `state=accepted`、`review_decision=approved`；对应动作的 `kind=approve`、`outcome=applied`。new-api 却把 `DonationActionApprove/Reject` 同时用于决定校验、动作查询、发奖校验和本人拒绝原因投影。最早在 `validateDonationReceiptItem` 就拒绝 `approved`，后续直接用决定查 action.kind 也不成立。

原日志中的 `expected_item_revision=5 < effect_revision=6 <= item_revision=7` 合法，`reviewed_at_ms=accepted_at_ms=applied_at_ms=1789455935216` 一致。空 item/action reason 也合法；其他待审项和 existing 项的相同版本可以重放。因此本次没有放宽时间、版本、reason 或身份校验。

修复明确区分三套闭集：

| 字段 | 值 |
| --- | --- |
| 命令 kind | `enter_review / approve / reject` |
| 动作 outcome/status | `pending / applied / rejected`（远端 outcome 不含 pending） |
| 条目 review_decision | 空、`approved / rejected` |

`confirmReviewFactFromReceipt` 显式把 approved/rejected 映射为 approve/reject，再按原 item、batch、action、kind 查询；目标、原决定、有效版本和源时间继续完整绑定。`manualApproveFact` 和本人拒绝备注投影使用独立的决定常量。

测试替身原本也误用了命令常量，掩盖了真实 wire 差异。现有 model/service fixture 改为接收端字面值，避免调用方同源常量再次掩盖契约错误。

## 已修复：批准时库存已先取得

继续追踪接收端 `applyDonationApprove` 的两个库存分支，确认它会在同一事务内先 finish 为 `existing/already_exists`，再记录 `approved` 决定和 applied 动作。调用方原来一律禁止 existing 携带审核事实，仍会卡成 invalid_receipt。

现在仅在完整 approved 事实能绑定原 approve 动作时接收这种 existing 回执；无决定的 ordinary existing 仍按原路径处理，其他伪造或残缺决定仍被拒绝。existing 永远不发奖，也不写成本项首次捐献取得。

这个分支在接收端先 finish、再 stamp，共推进两次 revision。丢动作响应时，调用方使用该终态回执的实际 revision 确认 effect_revision；不能按普通批准的 expected+1 猜测，否则后续原动作查询会发生矛盾。accepted/committing 的原生效版本推导没有改变。

## 改动文件

- `../new-api/model/donation.go`：独立决定常量。
- `../new-api/model/donation_reward.go`：决定闭集、显式动作映射、奖励校验和批准后 existing 对账。
- `../new-api/model/donation_batch.go`：本人已拒绝备注投影。
- `../new-api/model/donation_test.go`：扩展既有三库 fixture，覆盖正确 wire、错误命令/反向决定/动作/目标/时间、等版本矛盾、批准与拒绝、库存已取得及动作响应丢失。
- `../new-api/service/donation_test.go`：现有 HTTP fixture 使用真实决定字面值，原丢响应恢复测试因此也执行正确协议。

## 验证

所有 Go 命令使用 `GOFLAGS=-p=2`、`GOMAXPROCS=2`。

- 修前复现：SQLite `manual_review_and_reward` 拒绝合法 approved，反而接受 command-as-decision；`manual_acceptance_requires_approval` 拒绝合法 rejected。修后两者通过，错误事实仍拒绝，永久奖励只能发放一次。
- 库存分支修前复现：`manual_approval_finds_existing/applied` 与 `/pending` 均返回 invalid_receipt。修后两者通过，后续动作查询与回执完全一致，existing 不奖励。

| 命令/检查 | 最终结果 |
| --- | --- |
| `go test -count=1 ./model ./service -run '^TestDonation' -timeout 10m` | 通过；model 1.839s、service 7.708s；[日志](codex-receipt-unit.log) |
| 配置真实 DSN，`go test -count=1 ./model -run '^TestDonationDatabase$' -v -timeout 10m` | 通过，18.279s；SQLite 3.50.4、MySQL 8.4.11、PostgreSQL 15.19；MySQL clientFoundRows=true；[日志](codex-receipt-database.log) |
| MySQL clientFoundRows=false，`go test -count=1 ./model -run '^TestDonationDatabase$/mysql/.*/(manual_review_and_reward\|manual_acceptance_requires_approval\|manual_approval_finds_existing)$' -v -timeout 10m` | 通过，5.347s；[日志](codex-receipt-mysql-default.log) |
| `go vet ./model ./service` | 通过 |
| 五个变更 Go 文件 `gofmt -l`、定向 `git diff --check` | 无输出，退出码 0 |
| Go 类型/编译检查 | 由上述 model/service 测试编译及 vet 通过；未修改 TypeScript |

完整矩阵执行三引擎各自 fresh、真实旧捐献历史升级、重复迁移、唯一性、跨连接和本次回归。两个网络引擎各自新建隔离库 `new_api_codex_receipt_20260915_1600`，仅使用既有本地验证容器和合成凭据；fixture 清理自身前缀表/临时 schema，空隔离库保留，未改旧验证库。未声称测试 MySQL 5.7 或 PostgreSQL 9.6。

## 交接边界

- 本范围没有剩余未修复发现，产品代码已通知主会话稳定，可重建。
- 需要主会话把三套枚举区别及 approved-existing 无奖励规则同步到公共 spec / wire-contract；这些文档由主会话持有。
- 主会话已反馈第一版 enum 修复二进制的自动/人工真实两进程联调通过（49.68s / 36.43s）；包含 existing 修复的最终二进制复验、浏览器验收和 gpt-load Linux 门禁仍由主会话汇总。本报告不代写那些最终结果。
