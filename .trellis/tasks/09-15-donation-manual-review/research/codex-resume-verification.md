# Codex 续审验证记录

- 日期：2026-09-15。用户要求从中断记录继续，复用既有任务；未创建新任务。
- 状态：代码复核发现已修复，本地真实进程、数据库及构建验证通过；**浏览器验收已于同日由用户授权用 Playwright MCP 完成**，并在其中发现并修复一个阻塞级前端崩溃缺陷。详见 [浏览器验收记录](browser-verification.md)。
- 保留 `in_progress` 和当前任务指针；无提交、推送、归档、部署、生产上游调用或全局配置变更。

## 1. 本轮解决的阻塞

中断前真实回执为 `state=accepted, review_decision=approved`，对应 `kind=approve, outcome=applied`。new-api 却用命令值 approve/reject 校验、查询决定字段，合法批准在 `validateDonationReceiptItem` 即被拒绝；测试替身使用同一错误常量，普通单测没有揭露这个协议差异。

已新增独立决定常量并显式映射到动作，覆盖回执确认、唯一奖励门槛及本人拒绝原因投影。原时间、目标、动作身份和 revision 校验均保留；错误命令值、反向决定、错动作/目标/时间、同版本矛盾仍拒绝。

同时修复批准时库存已抢先取得的合法 `existing + approved` 回执：完整绑定原动作后对账，丢响应时使用实际终态 revision 确认动作，保持零奖励。回归均记录修前失败、修后通过，详情见 [续审代理报告](codex-receipt-resume-review.md)。

本轮产品修改仅在 new-api 的 model/donation.go、model/donation_reward.go、model/donation_batch.go 和现有 model/service donation_test.go；未改 gpt-load 产品代码或前端。主会话同步两份 donation spec、design 和 wire-contract，纠正枚举、请求白名单、真实 effective_mode、can_reject、actor、UUID 唯一、版本/时间、虚拟 ID 及脱敏边界的旧说明。

## 2. 最终验证

| 范围 | 结果与证据 |
| --- | --- |
| 两个最终二进制 | gpt-load 使用 Go 1.27.0；new-api 使用 Go 1.25.5，包含现有最新嵌入 UI。均 Windows amd64，构建成功。 |
| 自动捐献真实双进程 | `TestDonationIntegrationRealServices` **47.52s，通过**。 |
| 人工审核真实双进程 | `TestDonationManualReviewIntegrationRealServices` **35.55s，通过**。两项合计 controller 83.335s；[完整日志](codex-resume-real-services.txt)。 |
| new-api 最新受影响测试 | `go test -count=1 ./model ./service -run '^TestDonation' -timeout 10m`：model 1.839s / service 7.708s，通过。gofmt、go vet、定向 diff check 通过，见续审代理报告。 |
| new-api 真实三数据库 | SQLite **3.50.4**、MySQL **8.4.11**、PostgreSQL **15.19**：完整 `TestDonationDatabase` **18.279s，通过**，含 fresh、真实旧捐献数据升级、重复迁移、跨连接及实际唯一约束；MySQL clientFoundRows=true。新增回归另在 clientFoundRows=false 下通过 5.347s。 |
| gpt-load 最终完整门禁 | 隔离 Linux 副本执行 `make check PNPM='corepack pnpm@11.17.0'`，退出码 0；Go 1.27.0、Node 24.11.0。依赖锁检查、gofmt、tidy -diff、vet、前端 lint/format/typecheck/build、Go build、根包及 internal 全测试和 diff check 均通过；[日志](codex-resume-make-check.txt)。 |
| gpt-load 源码一致性 | 门禁完成后 `sync-snapshot.py --only gpt-load --check` 核对 **1,707 个文件**，归一化 CRLF 后 SHA256 全部一致。保存 [当时的源清单](codex-resume-make-check-source.json)。之后仅修改任务/spec 文档，未再改产品或测试代码。 |
| 接收端真实数据库及 HTTP | 延用源码未变的 [接收端报告](codex-intake-review.md)：MySQL/PostgreSQL 的 0015→0016 历史升级、重复迁移及并发；OpenAI native/Gemini converted 的 stream/nonstream 本地 HTTP；不把未执行的最低数据库版本算成通过。 |
| new-api 前端 | 延用源码未变的 [前端报告](codex-frontend-review.md)：5 files / **56 tests**、typecheck、定向 lint/format、build:check 和七语键检查通过。本轮没有重复执行或宣称全仓前端测试通过。 |
| Trellis 上下文 | task validate：implement.jsonl 11 条、check.jsonl 10 条，全部有效。 |

真实进程命令（两个 binary 环境变量均已配置，不是 skip）：

```text
GOFLAGS=-p=2 GOMAXPROCS=2
go test -count=1 ./controller -run '^TestDonation(ManualReview)?IntegrationRealServices$' -v -timeout 15m
```

二进制位于既有隔离目录 `C:/Users/lucky0401/AppData/Local/Temp/donation-codex-review-05f242e3363842ad99c18d03b5bcd987/`：

- `gpt-load-resume.exe` SHA256：`b9d3ff401366b62207413abdfc13f2d268531c1d13f8f393db862ef04ff50b81`。
- `new-api-resume-final.exe` SHA256：`02aa3845f2ef65c058f6419d95cd46f77b0d00ce8e80c5b259ed442a0478d196`。

第一次未显式指定 PNPM 的 make check 被旧 Corepack 的全局默认 pnpm 入口阻断；使用仓库原有 PNPM 参数指定已锁定的 11.17.0 后整条门禁通过。没有更改仓库锁文件、依赖或全局配置。

## 3. 验收对应关系

| AC | 当前证据 |
| --- | --- |
| AC1 / AC12 | 原自动双进程、旧摘要 golden、旧 0 revision 与模式/能力用例；自动、暂停恢复及永久奖励保持。 |
| AC2 / AC4 / AC5 | 人工双进程：待审不入池/不发奖，测试成功仍待审，重复批准只发一次；丢动作响应后 new-api 重启恢复。模型测试另验证无可信批准不发奖及 approved-existing 零奖励。 |
| AC3 | 接收端 native/converted 实际 HTTP 与人工双进程 stream/nonstream，断言原 key、模型、prompt、库存不接替、脱敏及双方站内钱包不扣费。 |
| AC6 | 双进程验证普通用户/只读管理员 POST 403、只读元数据及本人拒绝原因；前端组件权限测试通过。**真实页面已验收**：只读管理员详情仅 `Close`（无通过/拒绝/测试面板），捐献人看到已确认拒绝原因且看不到管理员备注。 |
| AC7 / AC8 | 实际 HTTP 跨帧回显脱敏、双进程取消后元数据非 running 且仅一次调用、持久失败/断流测试及组件会话守卫。**真实页面已验收**：流式响应回显显示为 `[redacted]`；运行中可停止并得到 `测试已取消` 明确终态，不产生部分成功结果。 |
| AC9 | 人工双进程执行旧 retry_exhausted 转审、保留 auto 批次和截止、批准不发第六次 probe、重启后原金额到账。**真实页面已验收**：详情出现 `转人工审核` 入口，确认文案声明保留原提交内容/截止/奖励，执行后模式转为人工审核并完整保留原暂存截止时间。 |
| AC10 / AC11 | 过期/owner/配置/租约用例与三库并发；双进程在删除目标后仍可拒绝，捐献人看到最终原因。 |
| AC13 | 两端真实三库历史升级/重复迁移及实际唯一约束；精确版本与原证据见对应报告。 |
| AC14 | 接收端独立冷队列、调用方逐批租约/冷热恢复及前端轮询用例；**真实页面已验收**：长期待审记录在捐献人与管理员两侧均正常渲染、不持续快速轮询，审核完成后刷新即得最新状态。 |

表中是测试证据对应关系，不能替代原计划要求的真实浏览器验收，也不据此将全部 PRD 验收框勾为完成。

## 4. 浏览器验收（已由 Claude 续跑完成）

Codex 轮次内置浏览器未发现可用实例；Chrome 虽已连接，但导航到本地 fixture 被工具安全策略拒绝（enterprise network policy），当时未采用任何绕过手段，验收标签页释放、fixture 经 stop 文件正常退出。

2026-09-15 稍后，用户明确授权改用本机已配置的 Playwright MCP 连接 Chrome 续跑本地验收。该轮**已完成全部计划内的页面验收**，并发现并修复一个此前所有自动化测试均未覆盖的阻塞级前端缺陷（`Intl.RelativeTimeFormat` 收到非 BCP-47 的 `zhCN` 语言码导致整页崩溃）。完整结果、证据与边界见 [浏览器验收记录](browser-verification.md)。

## 5. 尚未完成与边界

1. 浏览器验收已完成（见 [浏览器验收记录](browser-verification.md)）；该轮修复的 `zhCN` 崩溃缺陷已补回归测试并通过构建与 57 项捐献测试。页面验收仍限于 fixture 的合成上游与临时 SQLite，不代表生产环境。
2. 原全仓 `bun run copyright:check` 在 **22 个本任务未改文件**失败；本范围版权/格式检查通过。路径清单在前端报告，未顺带改动无关文件，也不声称全仓版权门禁通过。
3. 生产 cline 可用性、生产迁移、真实上游消耗及发布不在本地授权范围，均未执行；本次数据库证据不覆盖未实测最低版本。

除此之外，本次已确认的代码缺陷均已修复（含浏览器验收新发现并修复的 `zhCN` 前端崩溃），保留所有修改及本地证据供用户继续审查。
