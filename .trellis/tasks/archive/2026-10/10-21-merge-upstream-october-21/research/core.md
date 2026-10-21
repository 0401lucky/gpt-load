# 核心兼容合并与验证

## 合并范围

- 完成并审查 10 个核心冲突文件：`internal/control/runtime.go`；`internal/gateway/{handler.go,handler_test.go,model_cooldown_test.go}`；`internal/health/{decision.go,execution_judge.go}`；`internal/state/{model_cooldown.go,registry.go,runtime_checkpoint.go,runtime_view.go}`。接续代理已有修改，全部保留双方必要字段与行为。核心目录无冲突标记；子代理未操作 Git 索引、分支或提交。
- `health.Decision` 同时携带 `QuotaCooldown` 与独立的 `QuotaResetAt` / `QuotaResetSource`。内置额度规则保持可恢复标记；自定义模型冷却保留同一响应的可信重置证据，同时使用普通冷却地图。已提交响应的安全约束仍拒绝扩大作用域，并清理已废弃效果的字段。
- 保留 `SetQuotaCooldown(ref, model, until, now)`，新增 `SetQuotaCooldownWithReset(ref, model, until, reset, now)`。同一锁内校验身份、额度恢复代次、模型恢复代次后保存额度冷却与可信重置。`SetModelCooldownWithReset` 在非零 reset 时额外校验额度恢复代次；无 reset 的普通冷却仍可在额度恢复后写入。
- gateway 按 `QuotaCooldown` 选择额度或普通冷却，不重复写两种地图；账号额度冷却继续使用 `SetQuotaCooldown`，token 刷新冷却保持版本校验。缺失模型时不误写账号冷却。
- 额度恢复只清除 `QuotaCooldownUntil` / `QuotaModelCooldowns`，保留普通冷却、黑名单、失败状态以及 `ModelCycleStarts` / `ModelNextResets`。`RuntimeView`、`ModelCooldowns` 与 `GroupModelCycles` 合并两种冷却期限，周期仍只使用可信证据。
- registry 重建、精确快照/回滚、checkpoint 复制同时包含额度地图与周期地图。旧/未知 `model_cycle_version` 保留两种健康冷却，但丢弃不可信周期。
- `control.Runtime.Run` 同时启动捐献恢复与 CPA 模型目录 updater，二者均纳入 WaitGroup，取消后等待所有后台任务退出。

## 自动合并遗漏及修复

`ReplaceCredentials` 会在 cloned buckets 中保留当前身份的额度冷却，却使用原始输入 entries 同步 `SchedulingState`。这样 registry 与调度账本会对账号冷却产生不同投影，恢复边界也会丢失。

新增 `TestRegistryReplacementKeepsQuotaCooldownInSchedulingState` 先复现失败：保留后的账号仍冷却，但调度 member 的 `suspended=false` 且 `cooldownUntil` 为空。修复为从实际 buckets 构建调度视图；该回归复验通过。

## 交叉回归与审查

- 新增 `internal/state/quota_model_cycle_test.go`：长额度冷却不能吞掉较短冷却响应中的可信 reset；重复证据不改周期；恢复保留周期及较短普通冷却；恢复前 quota/custom reset 写回被拒绝，恢复后有效写回被接受；quota/cycle 的精确快照、重建、checkpoint 与身份变化互相兼容。
- 增强 `internal/state/model_cycle_test.go`：旧/未知周期版本不能恢复可信锚点，也不能丢失额度冷却。
- 增强 `internal/health/model_cooldown_test.go`：内置额度恢复标记与模型/账号/请求作用域一致；custom 模型调度保留独立 reset，custom 账号/无效果不携带模型 reset；提交安全约束保留。
- 增强 `internal/gateway/model_cooldown_test.go`：真实 handler 配置链保存额度地图与可信 reset，调度的一分钟余量不进入统计边界，禁用开关不建周期，额度恢复后不留重复普通冷却；quota/custom 在途响应不能恢复旧 reset。
- 增强 `internal/control/runtime_test.go`：捐献恢复与 CPA updater 同时运行，updater 已退出但捐献恢复尚未退出时 Runtime 继续等待。
- 增强 `internal/control/group_model_usage_test.go`：额度恢复前后真实用量窗口、请求量和 token 不变，只清除额度冷却投影。
- 增强既有 `credential_metadata_merge_test.go`：别名/备注各类写入和拒绝路径在同时存在额度冷却及可信周期时保留全部健康状态。
- 审查捐献鉴权/恢复屏障/长期回执、备注投影及显式 key probe 路径：上游合并未替换 fork 契约；既有捐献、备注、诊断回归均包含在通过的 control 包测试内。
- 审查请求体日志链：只对已开启访问密钥记录客户端正文，默认关闭；管理员请求详情读取正文，访问密钥用户的日志投影仍受既有权限范围约束，WebSocket 按已开启开关记录客户端 turn frame；诊断摘要仍走秘密脱敏。最新失败摘要与永久小时聚合/精确边界用量保持独立。既有 request body、activity 与 group model usage 回归通过。

## 验证证据

统一使用进程环境 `GOTOOLCHAIN=go1.27.3`、`GOPROXY=https://proxy.golang.org,direct`。未改全局配置，未访问生产数据库。

1. `gofmt -w` 限于 owner 下 555 个 Go 源码；不处理忽略的历史 tmp 副本。`gofmt -l internal/control internal/gateway internal/health internal/state internal/requestlog` 无输出。
2. 完整核心包：`go test -count=1 ./internal/state ./internal/state/loader ./internal/health ./internal/gateway ./internal/control ./internal/requestlog`。
   - state、state/loader、health、control、requestlog 通过。
   - gateway 仅已知 TCP 写截止用例失败；见下方本轮基线比较。
3. 最终专项：`go test -count=1 ./internal/state ./internal/health ./internal/gateway ./internal/control ./internal/requestlog -run 'Quota|Cooldown|ModelCycle|ModelReset|RegistryReplacement|CredentialName|CredentialNote|Credential.*Diagnostics|GroupModelUsage|Donation|RequestBody|CustomError|ErrorRules|RuntimeKeepsDonation|RuntimeRunsCPAModelCatalog'`：五包全部通过。
4. `go vet ./internal/state/... ./internal/health/... ./internal/gateway/... ./internal/control/... ./internal/requestlog/...` 通过。
5. `git diff --check` 与 `git diff --check HEAD` 限于核心 owner 路径通过；`rg` 未发现核心冲突标记。

## 本轮基线与限制

主会话在固定 fork 起点 `216024cc31c5de4c31a11777c41746bf9ac752d3` 建立仓库外基线，使用同一 Go 1.27.3 / Windows 环境执行完整测试。直接解析本轮 `baseline-tests.jsonl` 与 `merged-tests.jsonl` 后，核心 owner 的失败集合完全相同：只有 `gpt-load/internal/gateway/TestBufferedWriteDeadlineStopsRealTCPSlowReader`（`stream_integration_test.go` 的真实 TCP 慢读者写截止断言，得到 `error=nil`）；新增和消失失败均为 0。日志位于 `C:/Users/lucky0401/AppData/Local/Temp/gpt-load-merge-20261021-b0fc72ed/`。

最晚测试编辑时间 UTC：registry `14:28:14`、gateway `14:29:35`、control runtime `14:31:26`、health `14:33:27`。上述子代理完整核心/最终专项均在最终编辑后执行；主会话如使用较早全库编译结果，只需复验新增定向范围。

完整项目门禁、独立 CPA module、Git 双亲合并与 main 集成由主会话执行。真实 MySQL/PostgreSQL DSN 未提供，不宣称生产库或外部数据库实测。保留 TCP 写截止平台失败作为明确验证限制，不删测试、不改断言掩盖失败。
