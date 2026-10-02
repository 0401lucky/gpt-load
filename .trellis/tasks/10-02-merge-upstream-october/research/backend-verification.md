# 后端兼容合并实施与定向验证

## 实施范围

固定上游 `addd69870cb58108c584d69219c44e23d4bdf3db`；当前集成分支为 `merge/upstream-20261002`，本地起点 `c79bca8f`。

实际后端文本冲突为以下 6 个文件，已逐项解决，没有选择全局 ours/theirs：

| 文件 | 合并结果 |
| --- | --- |
| `internal/platform/config/config.go` | 保留 `DonationIntegrationToken` 字段、加载及独立验证函数；保留上游 `CodexLiveConfig`、媒体参数解析与验证函数，函数边界各自闭合。 |
| `internal/state/runtime_settings.go` | 保留冷却开关常量、默认 false、基线解析、分组覆盖和继承；接纳空响应重试、Codex Live、并发限制全链路。冷却键仍不进入 `IsRuntimeSettingKey` 和全局写入白名单。 |
| `internal/state/snapshot.go` | `GroupView` 和 `Compile` 同时保留上述新旧字段，不丢失快照值。 |
| `internal/control/group_detail.go` | 分组 `effective` 同时输出冷却、空响应重试、Live 模式及并发限制。 |
| `internal/control/modern_credentials.go` | `ModernCredentialItem` 同时保留 `Note`、`RPMPeakHour`；列表/详情构造均显式赋值。共用凭据 DTO 仍对两字段使用 `json:"-"`，不改变 classic 投影。 |
| `internal/parameteroverride/rules.go` | 完整保留 `Fingerprint()` 与 `ConfiguredFields()` 两个独立方法，保留上游 Gemini embeddings 等协议支持。 |

增加了两个既有控制面测试的兼容断言：

- `internal/control/group_settings_test.go`：冷却开关启用、禁用和继承时，同次提交 `empty_response_retry=true`、`codex_live_mode=relay`、`concurrency_limit=3`，读回 effective 与运行时快照必须同时保留；继承仍清除冷却覆盖键。
- `internal/control/credential_note_test.go`：写入真实本地 RPM 合成记录，备注修改、清除、可选字段更新期间，modern 列表仍返回峰值 9；classic 列表与详情均不输出备注或 RPM 字段。

上述 8 个 Go 文件均执行过 `gofmt -w`，作用域内 `git diff --check` 通过。

## 自动合并区域审查

- 网关 `DecisionContext` 仍显式传入 `selection.Group.RateLimitResetHintEnabled`；全候选模型冷却仍使用 fork 的恢复时间错误文案。
- `health.JudgeExecution` 的上游空响应分支保持 `RetryNextCandidate`、`EffectNone`；它与 429 的 header/reset-hint/default-cooldown 分流相互独立，空响应不污染凭据失败计数。
- 上游 RPM 记录位于每次实际转发/业务 WebSocket turn，终态日志保留原 credential/upstream_model 字段，新增首末输出时间不改变 fork 用量矩阵的标识和窗口归属。
- `requestlog.QueryGroupModelWindowUsage` 保留同一 DB 快照中的完整小时桶与精确首尾明细；相关 control/requestlog 全包测试通过。
- 普通凭据导入仍调用捐献资源锁与库存标记；捐献暂存、审核、回执路由及来源隔离未被上游覆盖。
- 人工捐献目标仍调用 `ParameterOverrides.Fingerprint()`，避免私有字段 JSON 序列化为 `{}` 导致目标修订碰撞；指纹转义路径回归与 control 捐献/探测回归均包含在下述包测试中。
- 已向前端实现代理同步新增与 fork 字段集合，以及冷却开关分组专属、备注/RPM modern 专属的边界。

## 执行的验证

命令：

```text
go test -count=1 ./internal/platform/config ./internal/state ./internal/parameteroverride ./internal/control ./internal/health ./internal/gateway ./internal/requestlog
```

| 包 | 结果 |
| --- | --- |
| `internal/platform/config` | 通过，2.922s |
| `internal/state` | 通过，3.518s |
| `internal/parameteroverride` | 通过，1.329s |
| `internal/control` | 通过，24.545s |
| `internal/health` | 通过，0.750s |
| `internal/requestlog` | 通过，18.092s |
| `internal/gateway` | 失败，52.035s；已见与基线相同的用例和错误，下述完整门禁由主会话核实最终失败集合。 |

补充两项兼容断言后，执行：

```text
go test -count=1 ./internal/control -run 'TestCredentialNoteStaysOutOfClassicResponseAndHonorsOptionalSemantics|TestUpdateGroupSettingsOverridesRateLimitResetHint'
```

结果通过，0.481s。

## 环境失败与后续门禁

已读取主会话固定起点工作树的基线 JSON：`C:/Users/lucky0401/AppData/Local/Temp/gpt-load-upstream-20261002-baseline-tests.jsonl`。以下本次可见错误与基线逐用例、逐消息一致：

- `TestSSEScannerRecognizesThreeDialectFixtures/openai`、`/anthropic`、`/gemini`：`sse_test.go:109: fixture has no complete first event`，输入夹具包含 `\r\n`。
- `TestBufferedWriteDeadlineStopsRealTCPSlowReader`：`stream_integration_test.go:780: writeUpstreamResponse() error = nil`。

本次 gateway 普通文本输出曾被工具截断，因此不以这份记录声称已穷举全部失败；主会话负责完整门禁 JSON 比对、独立检查与最终证据。没有修改产品代码、跳过用例或放宽断言来绕过这些环境失败。真实外部数据库测试依赖 `GPT_LOAD_DATABASE_TEST_DSN`，本代理没有运行外部数据库实例。
