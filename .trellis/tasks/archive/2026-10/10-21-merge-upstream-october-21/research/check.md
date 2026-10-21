# 全范围兼容审查（2026-10-21）

## Verdict

本次审查未发现新增的阻断问题，无需追加代码修复。可按已批准设计完成本地双亲合并与 main 集成。完整 Go 测试仍有本轮同环境固定基线已经复现的失败，不能把结论写成门禁全绿。

审查输入：fork `216024cc31c5de4c31a11777c41746bf9ac752d3`、上游 `41c82ac02be5c26bd8851256236036d404e4829b`。先读取保存的 native hook、check manifest、PRD/design/implement 和各 owner 研究记录，再按 HEAD/MERGE_HEAD 双向差异识别 55 个双方同时修改的路径，复核冲突解决、自动合并及关联消费者。没有修改 Git 索引、分支、提交、任务状态、spec 或用户原有文件。

## Findings (fixed)

本审查没有发现需要现场修复的新问题；没有修改产品源码。

已有实现修复经本次复核确认有效：`internal/state/registry.go` 的 `ReplaceCredentials` 从保留后的实际 buckets 同步 SchedulingState，避免保留账号额度冷却后调度投影丢失。`TestRegistryReplacementKeepsQuotaCooldownInSchedulingState` 已在最终完整测试中通过，相关 spec 已由主会话同步。

## Findings (not fixed)

- **既有平台/环境失败**：根完整测试 5 个包失败，23 个顶层、82 个含子测试失败。重新解析本轮 `baseline-tests.jsonl` 和 `merged-tests.jsonl`，新增和消除失败集合均为空。catalog/authkey 的 Windows 引号路径、container 当前代理环境、gateway TCP 慢读端写截止以及 webui 的 Bash/Windows/Python 路径问题属于本轮基线，未删测试、加 Skip 或放宽断言。精确名单与错误语义见 `baseline.md`。
- **既有日文 fallback**：classic monitor 的 `affinityPromptCacheKey`、`affinityPromptPrefix`、`affinityResponseContinuity` 三项在固定 HEAD 与 MERGE_HEAD 均缺少日文键。本次新增语言键没有新增该缺口，沿用 fallback；不在兼容合并里扩展语言修复范围。
- **未实测边界**：未配置真实 MySQL/PostgreSQL DSN；未执行真实上游请求、浏览器 E2E、Docker 构建/运行或 GitHub workflow。SQLite 与源码/静态检查证据不能替代这些验证。它们不是本次本地合并的新增代码缺陷。

## 合同复核

- **迁移**：旧 0001–0032 生产迁移的 Git 规范化 blob 无差异；注册顺序及 `ValidateCurrent0027` 保留。0033 逆映射导出符号/ID并移除来源注释后与固定上游 0030 全文相同，内部 DDL 名称未变。请求体迁移按语义 ID 定位。最终完整测试已通过真实临时文件 SQLite 的 24/27/32→33、关闭重开、逐列业务快照、默认关闭、开启后重启保存及 FK/check/cascade 回归。
- **冷却和周期**：quota/custom 分支分别写额度和普通冷却；非零 trusted reset 与冷却在同一 registry 锁内校验身份/恢复代次。额度恢复推进代次、拒绝旧在途结果，并保留普通冷却、黑名单、失败状态与可信周期。clone/reconcile/exact restore/checkpoint/runtime view/scheduler 和 GroupModelCycles 的两种冷却来源齐全；周期仍只用可信 reset，旧/未知版本 checkpoint 丢弃周期。state/health/gateway/control 交叉回归通过。
- **捐献与诊断**：独立 donation HTTP module 和管理鉴权拒绝 donation token 的隔离保留；恢复屏障、租约、库存/长期回执合同未被替换。集中代理引用仍在当前读快照中展开。参数覆盖新增数组通配符后 compiled rule 的 fingerprint 仍包含 set、匹配条件及逐段 remove 路径；指定 key probe 不走库存 fallback。单项诊断保留安全上游状态、摘要和明确 Retry-After，秘密脱敏及恢复证明的身份/目标/冷却匹配仍在。
- **生命周期**：Runtime 同时启动 donation recovery 和 CPA catalog updater，并统一等待取消退出；DI 构造和新增 Claude reset-credit 签名一致。
- **前端**：classic 严格白名单/DTO/projector/patch/UI 与 modern runtime consumers 同步 error_rules、request_body 及既有分组 reset-hint；fork 模型用量入口与 missing/partial 提示保留。modern name-only 响应保留持久 note 与备注草稿，设置保存受 nameDirty/namePending 和 owner/controller 守卫。optional row/batchCount 单批模式、依赖改变/卸载取消、当前轮次拒绝旧回写齐全；移动 overview 区域保留模型用量矩阵。未引入前端测试框架。
- **请求体记录**：访问密钥开关默认 false；HTTP 原始正文和 WebSocket turn frame 仅在开启时记录。日志归属检查先于正文读取，管理员与归属访问密钥可查看；其他管理权限和捐献权限未扩张。持久化队列预算的分配/入队失败/写入/关闭释放路径齐全，JSON 截断、详情格式化预算和日志级联留存沿上游合同。
- **其他上游自动合并**：错误规则作用域/重试/提交后安全约束、聚合 search passthrough、DeepSeek 显式 remove 优先、模型响应观测、billed usage 的 partial/missing 语义及 WebSocket interrupt 已连同消费者/既有回归检查。最新失败摘要与永久小时聚合及 fork 精确用量边界仍独立。
- **依赖与发布**：根 Go 1.27.3、Bifrost 1.13.1 和根/CPA SDK v8.0.36 对齐固定上游。fork GHCR main workflow、推广脚本、Compose 内容与 HEAD 相同；上游 release 变化为固定 action SHA 更新和 Docker Go 基镜像更新。开发版 update 在 Fetch/下载前拒绝，含 fork main 版本回归。没有 Trellis 平台模板/config 源码变动，无需追加模板升级改动。
- **Spec**：主会话新增数据库额度/可信周期、0033/请求体和 modern 单批诊断合同，当前文档与审查的代码路径一致。

## Verification

| 检查 | 结果与证据 |
| --- | --- |
| Lint | **PASS，审查阶段重跑**。精确 pnpm 11.17.0 `--dir web run lint`，包含 ESLint 零 warning 与 check:styles；退出码 0，`check-review-lint.log`。 |
| TypeCheck | **PASS，审查阶段重跑**。同一 pnpm `--dir web run type-check`，vue-tsc/tsc 均通过；退出码 0，`check-review-typecheck.log`。 |
| 格式/构建 | 主会话最终 1372 个实际 Go 源码 gofmt、root tidy/vet/build、前端 frozen install/format/build 均通过；无产品源码追加编辑需要使其失效。 |
| 完整根 Tests | **FAIL，仅相同基线失败**。两份完整 `go test -json -count=1 . ./internal/...`：64 pass / 8 skip / 5 fail → 65 / 8 / 5；23 顶层、82 含子测试失败完全一致，added/removed=`[]`。新增通过包为 `internal/execution/responsemodel`。 |
| 核心专项 Tests | **PASS**。审查重新读取最终 cross-regressions 两份日志及完整 JSON；quota/cooldown/cycle/donation/note/diagnostic/model usage/registry replacement/runtime 回归均通过。 |
| 独立 CPA | **PASS**。最终 tidy/vet，无差异；最终完整 JSON 中 embedded、modelcatalog 两包通过。 |
| release fake/CLI | **PASS**。进程级 PYTHONUTF8=1 的 17 项 release fake tests；开发版实际 update 拒绝证据与不下载回归均已取得。 |
| Git/冲突 | **PASS，审查阶段重查**。`git -c core.autocrlf=input diff --check HEAD`、`diff --cached --check` 无输出；未解决索引项为空，改动源码行首冲突标记扫描为空；旧32迁移 blob 与0033逆映射一致。 |

原始证据目录：`C:/Users/lucky0401/AppData/Local/Temp/gpt-load-merge-20261021-b0fc72ed/`。本审查新增日志仅为 `check-review-lint.log`、`check-review-typecheck.log`；没有重复运行无新增疑点的完整 Go、Docker 或浏览器测试。
