# 捐献人工审核与真实调用执行计划

本文件保留执行计划，对应 [PRD](prd.md)、[设计](design.md) 和上下文清单 `implement.jsonl/check.jsonl`。用户指定 DeepSeek 实施、Codex 审查并保留任务；2026-09-15 续审状态与逐项证据见 [验收记录](research/codex-resume-verification.md)。实现与代码检查已完成，浏览器验收被访问策略阻断，任务仍 in_progress；下方原规划的未勾选项不作为最新执行状态。

## 1. 进入实现前

- [x] 用户在最新完整规划摘要后指定 DeepSeek 实施，并要求完成后先交给 Codex 审查、不归档；当前 Codex 只准备交接提示词。
- [x] `python ./.trellis/scripts/task.py validate .trellis/tasks/09-15-donation-manual-review` 通过；两份 jsonl 均引用真实 spec/research 文档。
- [x] 复核两个仓库状态及各自 AGENTS；保留 gpt-load 的 `.playwright-mcp/` 和 new-api 既有 `clover-*.png`，不覆盖无关改动。
- [ ] 激活任务，从两仓库各自当前基线建立 `feat/donation-manual-review` 工作分支；不拉取/重置无关代码。
- [ ] 按 trellis-before-dev/native context injection 加载所负责仓库规范；new-api UI 读取其 `web/AGENTS.md` 和项目 shadcn-ui skill。
- [ ] 涉及管理权限及 API key 的实现，阅读 new-api AGENTS 指定的适用 OWASP 官方指引并记录控制/验证点；不升级认证、数据库或 SDK 依赖。

## 2. 分工与实施顺序

这是一个端到端交付，使用同一任务而非互不完整的独立产品任务。协议已经在 design.md 固定；实现发现必须改变产品或协议语义时，先回到规划更新并审阅。

以下角色表表示职责与文件边界。用户现指定 DeepSeek 实施，可根据所在平台串行处理或使用可用的代理，不依赖 Codex 专属子代理名称。如派发代理，提示词以 `Active task: .trellis/tasks/09-15-donation-manual-review` 开头，明确 ownership，并说明他人也在修改代码、不得回滚他人修改。实施者负责自查与验证，Codex 留待交接后独立审查。

| 所有者 | 负责路径 | 依赖/边界 |
| --- | --- | --- |
| gpt-load 接收实现 | `internal/control/donation_*.go`、必要窄复用 helper、`internal/storage/models/donation.go`、新迁移/注册、集成路由/错误/三语 locale、三语 README、相关现有 Go 测试 | 不修改 new-api；不修改 gateway 调度/通用钱包；从接收协议到实际单 key 调用可独立验证 |
| new-api 后端实现 | `../new-api/model/donation*.go`、`service/donation*.go`、`controller/donation.go`、`router/donation-router.go`、`service/authz` 必要注册、后端 locale、现有 model/service/controller donation 测试 | 遵循同一协议，可先用受控 HTTP fixture；两端真实联调等待接收实现完成 |
| new-api 前端实现 | `../new-api/web/src/features/donations/`、必要权限常量、七语 locale、有限共享流控制器兼容扩展及对应测试 | 不修改后端；类型及 API 按 design.md，可先用既有 MSW fixture；最终对接真实接口 |
| 主会话 | 任务文档/spec、测试环境、跨端协议一致性和最终验收 | 不与实现者同时修改其产品文件；需改 controller 联调 fixture 时先取得明确交接 |

### 2.1 模式、存储及旧协议兼容

- [ ] 增量持久化 Campaign/Batch 模式、Item 有效模式/远端 revision/人工目标/动作引用、ReviewAction/TestAttempt 与测试租约。
- [ ] 增加 gpt-load 后续冻结迁移及 CHECK/恢复校验；new-api 处理已有 NULL/空模式，但不重写旧活动版本 JSON、请求摘要、owner 或奖励。
- [ ] 自动模式缺省和旧 comparator 保持字节/语义兼容，人工模式独立 comparator；原目录 revision 不变，增加人工目录及 feature 协商。
- [ ] 回归旧接收端缺 item revision 的 auto 状态推进；已版本化/转人工后仍拒绝 0/旧回执覆盖，不能把两个兼容分支混用。
- [ ] 先补旧 schema/请求 golden 和人工提交不 probe、不入池、不发奖的失败用例，再完成实现。

### 2.2 审核动作和发奖恢复

- [ ] 实现 pending_review、rejected、过期及资源竞争；复用正式接收/committing 恢复，不伪造 probe passed。
- [ ] 实现本地 intent、远端动作幂等/查询、合法业务拒绝台账、单调回执和完整动作绑定；先幂等查重，后 expected revision。
- [ ] ApplyReceipt 和 CreditDonationReward 双重检查人工 approve；原自动模式、账号暂停、永久奖励及缓存行为保持。
- [ ] 只对满足条件的 retry_exhausted 项显式 enter_review，原批次/摘要/截止不变；旧 retry 和迟到 probe 不能恢复自动处理。
- [ ] 处理人工截止、活动关闭、目标变化、管理库存先取得、动作竞争和服务器重启。
- [ ] 将等待人工的冷对账和活跃恢复隔离，领取/release 尊重新动作唤醒；汇总状态与前端轮询同步。

### 2.3 真实对话测试

- [ ] test_id 在发上游前持久占用，单项租约和有界并发，重复 ID 不发第二次请求。
- [ ] 唯一暂存凭据 + 正常 chat operation，支持原生 OpenAI compatible 与已有 Gemini 转换路径；stream/nonstream 都由原执行器执行。
- [ ] 复用组头、代理和参数覆盖，覆盖后校验大小/token/文本能力；不暴露任意 key、URL、headers、path 或工具请求。
- [ ] 实现受控增量协议投影及跨片段秘密脱敏；同一规则处理非流式，结果正常持久完成才返回 succeeded。
- [ ] 从浏览器到上游传播取消，限制慢下游与总时长，短 cleanup context 释放租约；后台只能结束遗留记录，不自动重测。
- [ ] 实现与审核/目标/原 item 绑定的测试元数据查询；不保存完整对话、不扣站内钱包。

### 2.4 管理界面及用户进度

- [ ] 活动开关/模式说明、目录能力检查和旧活动默认值；离线关闭例外不误用于切换模式。
- [ ] 待审筛选、审核详情中的模型/提示词/有界输出/流式开关/停止/回复、独立通过/拒绝/转审。
- [ ] 复用 ConfirmDialog、DataTable、Dialog、表单和会话组件；仅记录读权无写能力，前后端同时执行权限。
- [ ] 展示拒绝原因、到期、目标变化、操作冲突与测试非成功；混合批次状态正确，长期待审不持续快速轮询。
- [ ] 本人 batch/item 的 review_note 只投影已确认拒绝原因，reason_code 保持闭集；不把管理备注/测试详情暴露给普通用户。
- [ ] 切换记录/账号/退出/关闭时取消并清正文；过期响应不覆盖新页面或新决定，受影响 cache 正确失效。
- [ ] 七语文案及必要后端文案同步，保留原捐献风险说明和版权。

## 3. 验证顺序与命令

全程只使用隔离本地数据库、合成 key 和本地 HTTP 上游；不调用生产 cline、不迁移 yunyou-99。复用现有 fixtures 和测试文件，避免为每个层重复建同一套测试。

### 3.1 gpt-load

```text
go test -count=1 ./internal/control ./internal/platform/httproute ./internal/storage/... -run 'Donation|Migration' -timeout 10m
make check
```

针对新功能需补的实际契约：manual can_probe=false 仍可待审；真实 key/prompt/model 无库存 fallback；approve/reject/TTL/测试/旧 retry 竞争；动作丢回复重放；committing 恢复屏障；库存抢先及永久防重；流式拆片回显 key、断流、取消、空回复/超限与慢下游。

gpt-load 门禁按仓库原 Makefile，**不额外跑本地 race，也不引入 Vue 前端测试**。若 Windows 原生 shell 无法执行 POSIX Makefile，使用可用的隔离 Linux 验证环境并验证源码一致，不能把历史通过结果当作当前证据。

### 3.2 new-api 后端及三数据库

```text
go test -count=1 ./model ./service -run '^TestDonation' -timeout 10m
go test -count=1 ./service/authz
go test -count=1 ./model -run '^TestDonationDatabase$' -v -timeout 10m
```

最后一个命令配置受控 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN`，SQLite 使用真实临时文件；不能以 skip、mock 或只编译代替。gpt-load 分别配置同一隔离引擎的 `GPT_LOAD_DATABASE_TEST_DSN` 运行：

```text
go test -count=1 ./internal/control -run '^TestExternalDatabaseDonation' -v -timeout 10m
go test -count=1 ./internal/storage -run '^TestExternalDatabase(ConcurrentMigrations|Lifecycle)$' -v -timeout 10m
```

- [ ] 每个引擎测试新建、含旧 DonationCampaign/Batch/Item/Reward/原动作历史的升级、重复迁移；检查模式回填、第二次 schema 稳定、原唯一约束和身份材料。
- [ ] 两独立连接覆盖首次动作竞争、approve/reject、旧回执、TTL、钱包事务与缓存；MySQL 关注真实隔离/current read 和 affected rows，PostgreSQL 唯一冲突后不能留在失败事务查询。
- [ ] 至少各一支持版本；若迁移依赖 CHECK/DDL 等版本差异，覆盖 new-api 支持下限 MySQL 5.7.8+/PostgreSQL 9.6+ 或采用已验证兼容实现。记录实际版本/命令/结果，未执行不声称兼容。
- [ ] 扩展秘密材料与 SQLite 备份恢复检查，使新动作/测试表不会被历史检测或清理遗漏。

数据库测试命令中的测试模式应在实现时核对实际名称，确保测试确实被执行，不能接受“no tests to run”。

### 3.3 new-api 前端

在 `../new-api/web`：

```text
bun run test src/features/donations/__tests__
bun run typecheck
bunx oxlint -c .oxlintrc.json <变更的前端文件>
bun run format:check <仓库支持的变更文件参数>
bun run copyright:check <仓库支持的变更文件参数>
bun run build:check
```

使用各脚本实际帮助/实现确认定向参数，避免全仓格式重写。若兼容扩展了共享流控制器，运行其受影响测试。行为覆盖对应 AC6–AC8/AC14，并验证深色/浅色、键盘、移动窄屏的关键审核操作；不以 class 快照或只有 render 的测试代替。

### 3.4 两端真实进程与界面

等待三个实现段完成，使用最新前端资源和两个最新 Go 二进制：

```text
DONATION_TEST_GPT_LOAD_BIN=<隔离构建产物>
DONATION_TEST_NEW_API_BIN=<隔离构建产物>
go test -count=1 ./controller -run '^TestDonationIntegrationRealServices$' -v -timeout 15m
```

沿用并扩展当前真实服务 fixture，验证：管理员配置 → 普通用户捐献 → 待审/零奖励 → 管理员指定 key 流式或非流式测试 → 独立通过/拒绝 → 首次永久奖励；同时覆盖旧耗尽转审、丢 action 回应、远端 committing 重启、本地发奖重启、重复和目标变化。无 binary 的 skip 不算验收。

使用本地浏览器完成管理员/普通用户、只读管理员三种关键权限及操作检查，确认界面状态与真实库/回执相符。不要为页面验收连接生产数据或真实上游。

## 4. 质量复核与交付

- [ ] DeepSeek 按 check.jsonl 先自查协议名/枚举/DTO、权限、接收-审核-奖励链、迁移、秘密处理、UI 复用及测试；修复确认问题并定向复验。
- [ ] DeepSeek 完成后写 `research/implementation-handoff.md`，列出改动、验收项、准确测试结果/数据库版本/证据路径和剩余问题，保持任务 `in_progress` 等待 Codex 独立审查。
- [ ] Codex 收到交接后按自身平台流程进行完整审查；在这之前不执行 archive/finish、不标记 completed、不清空任务指针，不自动运行会收尾归档的 finish-work。
- [ ] 主会话核对全部 AC、两仓库 diff 和未跟踪文件，验证没有无关格式化/依赖/品牌改动。
- [ ] 按 trellis-update-spec 将已实现且已验证的人工模式/动作/测试/回执契约更新到两份 donation spec，保留自动模式原约束；未验证建议不能写成事实。
- [ ] 把数据库版本、实际命令、结果和本地真实服务证据记录到本任务 research，标注未验证的生产行为；适用 OWASP 引用与验证随交付记录。
- [ ] 只有必要检查通过才报告本地实现完成；提交/推送/部署按届时会话明确授权和对应阶段规则处理，不能在本规划轮执行。

## 5. 停止与回退点

- 协议或用户可见行为需要改变：暂停依赖段，更新 PRD/设计并重新审阅，不由单仓库自行改语义。
- 本地未发布实现可通过普通定向补丁修复；不回滚他人工作、不删除现有数据库。
- 未来上线前保存完整数据库及身份/加密材料，接收端先升级。人工记录启用后如需回退，优先关闭新受理并保留新版恢复；禁止把待审转 auto、删除动作/资源账本或追回已发奖励。

实施与 Codex 复核已推进至浏览器验收，原计划和真实执行结果分开保留。浏览器验收已由用户授权用 Playwright MCP 完成，含三种角色、人工活动、流式/非流式/停止/通过/拒绝/转审/历史、深浅主题、窄屏与键盘操作；过程中发现并修复的 `zhCN` 前端崩溃缺陷已补回归测试并通过构建与 57 项捐献测试（详见 [浏览器验收记录](research/browser-verification.md)）。页面验收仅限 fixture 合成上游与临时 SQLite，不代表生产；仍不授权 commit/push、生产操作或归档。
