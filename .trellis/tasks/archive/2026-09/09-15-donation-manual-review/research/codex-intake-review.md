# Codex 接收端独立审查与修复

- 日期：2026-09-15；工具链：Go 1.27.0 windows/amd64。
- 范围：仅 gpt-load 捐献接收、审核、真实调用、0016 迁移及相关验证。new-api、规范同步、两进程和浏览器验收由其他审查者/主会话负责。
- 依据：原批准 `prd.md` / `design.md` / `implement.md` 和 check.jsonl。交接中的临时 wire 语义不自动成为设计依据；字段变更已与主会话和调用方审查者协调。
- 无 commit、push、部署、生产请求、任务归档或任务状态修改。全部 key、上游与数据库验证数据均为本地合成材料。

## 已修复的实际缺陷

| 范围 | 审查发现 | 最终处理与证据 |
| --- | --- | --- |
| 人工目标资格 | `review-context` / approve / prepare-test 仍把自动 `probe_unavailable` 当作人工不可用，违背跳过自动测试的目的 | 只按 `manualReason` 判断人工资格；无模型的历史分组可人工批准。`TestDonationManualApproveDoesNotRequireProbe` |
| 上下文事实 | 尚未转审的 auto 项虚报 manual_review；单一 can_review 无法表达目标异常时仍可拒绝 | effective_mode 恢复真实存储模式；review_action 表达 enter_review/approve；新增独立 can_reject，目标删、禁用、变化仍可拒绝。生命周期回归覆盖三种情况 |
| 转审绑定 | enter_review 禁止传目标修订，管理员看到的配置与实际冻结配置可能不同 | enter_review/approve 都要求 64 位小写 hex 目标，转审比对当前人工目标；原批次摘要、Attempts、截止不变，回执补 entry_action_id。真实 0015 历史升级/转审测试覆盖 |
| 恢复屏障 | 手工 approve 在永久凭据写入前漏掉既有控制恢复屏障 | 身份和动作重放先确认，再于事务外、持 writeMu 执行屏障，之后才能取得凭据。故障注入先复现“屏障失败但已 committing”，修后仍 pending_review |
| 审核事实绑定 | item.reviewed_at_ms 与 action.applied_at_ms 分别取 now，真库网络延迟使两值不同；调用方严格奖励校验会拒绝 | 使用同一动作时间写审核、接收与动作回执；真实双连接 approve 用例断言二者相等 |
| 回执版本 | committing→accepted、测试开始/结束/中断没有推进 item_revision；同版本出现不同事实 | 人工路径每次可见事实变更递增 revision；legacy auto 的 0 版本维持旧行为；缺失/未知模式不再默认为 auto。故障恢复和测试元数据回归覆盖 |
| 动作幂等 | 并发相同 action INSERT 冲突直接报重用，不退出失败事务后读取原结果 | 普通 INSERT，唯一冲突退出事务，再独立读取并核对完整请求摘要、batch/item。MySQL/PG 两连接相同 approve 都得到同一持久动作 |
| 全局资源 | 人工暂存/测试不查库存与 owner，冷清理不处理 resource_busy 或库存抢先；到期未释放 owner | 人工按稳定指纹锁序预留未 acquired owner；库存立即 existing，其他未决 owner 保持 pending_review/resource_busy；测试必须属于原 owner。冷对账释放匹配未取得 owner、收敛库存抢先且不发请求 |
| 测试/拒绝互斥 | reject 未检查活动测试，能在测试执行中清空暂存并拒收 | reject 与 approve 都检查持久测试租约和原截止；运行中的测试阻止相反审核。明确业务拒绝仍落动作账 |
| 测试槽与截止 | 先保存 150 秒租约，再无限等待进程槽；满载可等过租约后才执行 | 新测试占用槽失败立即返回 busy，不创建 attempt；重复 ID 可只读原元数据；整个调用和短清理都受 context 约束，等待 writeMu 也可取消 |
| 多进程测试 | MySQL 重复读可能用旧快照判断测试；对不存在 test/无独立索引的 inventory 做 FOR UPDATE 会堵住无关项目 | 身份/重放预检查后，新事务先锁 item，再建立读取快照；普通导入/批准继续以权威 resource current lock 协调。测试索引与实际 source+item 查询一致。强同步双连接测试先复现阻塞，修后 MySQL 通过 |
| 测试终态 | finish 返回 void，持久失败仍可返回 succeeded；即时响应 finished_at_ms=0、started_at_ms 与原记录不同；usage 丢失 | 完成函数返回已提交元数据，校验租约，失败绝不发布成功；保存/回读真实时间和可得用量，0016 增加可空非负用量列。写故障与时间/用量一致性回归覆盖 |
| 测试操作者 | 测试 actor 使用集成 source UUID，不是管理员 | 请求新增 actor，由 new-api 认证层填 user:<id>；接收端执行可见 ASCII/长度校验并纳入摘要，不能因相同 ID 换操作者重放 |
| 虚拟凭据身份 | 原 `MaxUint/2-attemptID` 会落进正式 ID 范围，例如 attemptID=MaxUint/2-1 得到 1 | 自动/人工使用 uint 上半区奇偶分区，输入仅 1..MaxUint/4；边界测试证明注入映射及不相交。执行前用可移植 signed 参数检查正式超大 ID；MySQL 实际插入上半区正式 ID 后明确阻止虚拟执行 |
| 配置指纹 | 人工目标用裸 SHA 而非批准的独立 HMAC 域；参数规则把 JSON Pointer 段直接用 `/` 拼接，`/a~1b` 与 `/a/b` 碰撞 | 人工目标使用独立 HMAC 域；Pointer 对段数和各段分别编码。`TestFingerprintDistinguishesEscapedPointerSegments` 先失败后通过 |
| 覆盖后请求 | 仅复检字节数，未复检 tokens、工具、附件、多份 completion、文字消息结构 | 保留组参数覆盖，并拒绝超范围配置；不能静默丢弃组规则。覆盖回归包含 4097 tokens、移除上限、tools、附件、n=2 与合法温度规则 |
| SSE 与文本 | HTTP 200 在校验前提交；重放缺 meta；错误体被当 SSE；跨网络片段无完整帧可无限增长；字节截断破坏 UTF-8；超时记成用户取消 | 新调用准备完成后才开始 SSE；重放返回 JSON 元数据；只投影白名单文本，丢弃上游错误体；完整帧/尾部/总量有界，按 rune 输出并预留 JSON 转义空间；deadline 与 cancel 分开，partial/error 不能成功 |
| 下游中断 | 没有写截止；写库失败仍可能发送成功 done | 每次事件按更严格请求截止/idle 设置可取消写期限；持久失败不发成功 done，调用方按断流/中断对账。上下文贯穿实际执行及安全短清理 |
| 冷队列 | ticks%60 假设每次自动 drain 一秒，但 32 个慢 probe 会使冷清理延后很久 | 同一应用 lifetime 下独立分钟 ticker，单次最多 32 项，检查后移到后续队列；退出等待冷协程结束。阻塞自动 probe 时人工过期仍完成的回归通过 |

## 审查中排除的误报

- 曾疑虑认证头只检查 Contains(`${API_KEY}`) 会接受混入静态 key。继续追踪实际 `state.parseHeaderRulesWithPolicy` 后确认：SDK-owned Authorization / X-Api-Key 等头在编译层已一律禁止，包括看似合法的 Bearer 模板；这条路径已有更强保护。没有修改或放宽这层规则，也撤回了重复模板判断。新增反例验证人工模式不能绕过该编译约束。
- Gemini 合成流仅在含文字的末帧放 STOP、但无最终 usage 帧时，现有 SDK 判定为 `upstream stream terminated before completion`。实际 HTTP fixture 改为仓库既有 Gemini 流格式（正文帧 + 独立空 parts/STOP/usage 尾帧），四条真实 HTTP 路径全部成功。没有为了错误 fixture 放宽生产成功标准，主会话的两进程 fixture 也已告知同一事实。

## 验证记录

### 已通过

1. `go test -count=1 ./internal/control ./internal/storage/... ./internal/parameteroverride ./internal/platform/httproute ./internal/platform/errors -timeout 5m`
   - 最新受影响全包：control 20.622s；storage 10.153s；dbtx 0.289s；migrations 6.223s；models 0.517s；parameteroverride 1.014s；httproute 0.312s；errors 0.533s。
   - 证据：[codex-intake-unit-final.txt](codex-intake-unit-final.txt)。随后局部修复有另行定向复验，见下方。
2. `go vet ./internal/control ./internal/storage/... ./internal/parameteroverride ./internal/platform/httproute ./internal/platform/errors`：通过。
3. `go build ./...`：通过；手工变更 Go 文件经 gofmt；`git diff --check` 通过（仅 Windows autocrlf 提示）。
4. 本地真实 HTTP：OpenAI compatible native 与 Gemini converted 各自 stream/nonstream，全部断言原 key、所选模型、提示词、脱敏、库存 key 不接替、重放不发第二次调用、测试后仍待审核。位于 `donation_test_integration_test.go`。
   - 自动请求摘要另外用独立的原四字段 wire 字符串冻结比较；`TestDonationCapabilitiesAdvertiseManualReviewAndKeepAutoDigestFrozen` 最后定向通过（0.393s），避免只比较新版 implicit/explicit auto 的同源漏检。
5. MySQL **8.4.11 / REPEATABLE READ**：人工旧 schema 升级、同 action 并发、同项 test 互斥、不同项并行、后续批准、动作时间、永久凭据唯一性及超大正式 ID 防冲突。
   - 最后唯一失败（无关库存的范围锁）修复后，完整 `TestExternalDatabaseDonationManualReviewTransactions` 重跑 **13.055s，通过**；内部强同步并行子例 0.11s。
   - 证据：[codex-intake-mysql-concurrency-fixed.txt](codex-intake-mysql-concurrency-fixed.txt)。
6. PostgreSQL **15.19**：捐献控制、人工/旧 0015 升级、通用控制 External 测试通过（56.811s）；并发迁移、生命周期、模型价标识等通过（20.365s）。仅明确 MySQL 专项按平台跳过。
   - 证据：[codex-intake-postgres-isolated.txt](codex-intake-postgres-isolated.txt)。
7. MySQL 通用并发迁移、Lifecycle、模型价标识、RepeatableRead 检查通过（52.866s）。更早本轮完整 External 运行中的 MySQL baseline 中断恢复、增量、部分 DDL 恢复均通过。
   - 证据：[codex-intake-mysql-isolated.txt](codex-intake-mysql-isolated.txt) 与 [codex-intake-mysql-final.txt](codex-intake-mysql-final.txt)。这两个组合命令各有下述已解释/已定向修复的失败，不能单独写作整条命令通过。

### 失败后处理与证据边界

- 第一轮 regression 先复现了屏障、运行中拒绝、假成功、终态时间、版本不推进、槽无限等待、分片事件无界、deadline 误分类；修后受影响全包通过。保留新增测试于源码。
- 原验证 DSN 的 `gptload` 主库含 DeepSeek 之前运行的**未发布中间版 0016**，缺本轮用量列。旧的通用 External 测试直接访问该库，所以 ValidateCurrent 正确拒绝。未改该旧库，另建隔离库/模式 `gpt_load_codex_intake_20260915_1420` 复跑。`codex-intake-*-final.txt` 的这些失败不表示从已发布 0015 升级失败。
- MySQL 改用隔离库后，唯一产品失败是新增“不同 item 并行预留”强同步用例；已修复并用整个人工事务测试复验。该轮其他捐献与通用测试已通过，不为刷全绿重复成本较高、代码不受影响的中断迁移测试。
- 最后局部回归（冷队列、SDK-owned 认证头保护、真实 HTTP、资源冷对账）通过，control 0.546s；精确结果见 [codex-intake-last-regressions.txt](codex-intake-last-regressions.txt)。最后 `gofmt -l` 无输出、`git diff --check` 通过。

## 待主会话收口

- 主会话负责把以上已协调事实同步到 spec / wire-contract / 总体验收；本审查者未修改这些公共文档。
- Linux `make check`、两仓库真实进程联调与浏览器权限验收由主会话/其他审查者执行；本报告不将其代为写成通过。
- 生产 cline 当前可调用性、旧数据库引擎下限版本（例如 MySQL 5.7）未测试。数据库通过结论严格限于这里记录的版本。
- 保留任务 in_progress。验证容器未删除；本审查新建的隔离 base 库/模式保留给主会话复跑，各测试内部生成的临时 schema/db 均由其 fixture 清理。原有验证库、截图及他人修改保留。
