# 技术设计：按上游 429 报错里的重置时间冷却单个模型凭据

## 1. 设计目标

1. 不新造冷却机制：复用上游 #599 已有的「按 (凭据, 上游模型) 冷却」能力，只补上「冷却时长从哪里来」。
2. 解析行为由分组开关控制，默认关闭，绝大多数分组行为零变化。
3. 出口文案只改「所有候选凭据都在冷却」这一条分支，不触碰其他错误文案与状态码。
4. 改动集中在少数几个文件，避免与上游后续演进大面积冲突。

## 2. 落点选择

**结论：解析放在判定层（`internal/health`），不放执行器。**

- 执行器（`internal/execution/bifrost/mapping.go:257-275` 等）构造 `ErrorEvidence` 时拿不到分组配置，而开关是分组级配置。若在执行器解析，就要把分组配置透传进执行器，会污染与上游共享的接口面。
- 判定层天然持有分组上下文与 `DecisionContext`（`internal/health/decision.go:61-66`），且 429 证据的 `Summary` 字段已经保留上游原始文案（生产实证：`Error 429: Daily free limit reached on model z-ai/glm-5.3-flash. Try again in 3h 18m`），无需改执行器即可拿到文本。
- 副作用：`Summary` 是脱敏截断后的文本。截断长度需要确认能否容纳候选句式；若实测被截断，则在执行器侧额外保留一个「原文片段」字段，这属于实现期的兜底分支，不改变整体设计。

## 3. 数据流与契约

### 3.1 配置链路（照抄 `affinity_enabled` 的既有链路）

- 键名新增常量 `SettingRateLimitResetHintEnabled = "rate_limit_reset_hint_enabled"`（`internal/state/runtime_settings.go`，与 `SettingAffinityEnabled` 并列）。
- 作用域：**分组专属键**，照 `parameter_overrides` 的既有先例，**不**登记进 `IsRuntimeSettingKey`。系统级只提供默认值与继承基准（`DefaultRuntimeSettings` 中为 `false`，`ResolveRuntimeSettings` 提供同名 `case` 供分组继承），**不**提供全局可写入口。
  - 依据：全局 PUT（`internal/control/settings.go:258`）与全局 `overrides` 投影（`:383`）都按 `IsRuntimeSettingKey` 放行。若登记进去，一次全局写入就会让该键出现在 `GET /api/settings` 的 `overrides` 里，而 classic（`app/resources/settings.ts:230`）与 modern（`api/settings.ts:171`）都对未知 `overrides` 键硬失败，两套前端的全局设置页会整页报错。分组专属键不进该列表即可从根上消除这条路径，且不需要新增全局用户可见开关。
  - 守护测试对齐 `internal/state/parameter_overrides_test.go:39-40`：反向断言本键**不在** `IsRuntimeSettingKey` 中。
  - 分组侧不受影响：分组 overrides 的写入校验不经过 `IsRuntimeSettingKey`（只经 `normalizeGroupSettings` 的特判与 `ResolveGroupRuntimeSettings`）。
- 分组级：`ResolveGroupRuntimeSettings` 增加同名 `case` 分支，解析用 `strictBoolean`（非布尔值报错）；未提供时继承系统值。
- 快照：`GroupConfig.Settings` → `ResolvedGroupSettings` → `GroupView`（`internal/state/snapshot.go`）。
- 管理面：`GroupEffectiveConfigResponse` 增加 `rate_limit_reset_hint_enabled`（`internal/control/group_detail.go`），`effective` 与 `overrides` 都经既有的 `ResolvedGroupSettings`/`Overrides` 投影下发。
- 数据库：无迁移。配置与 `affinity_enabled` 一样存在分组 `overrides`（JSON 列）里。

### 3.2 判定链路

- `DecisionContext` 增加 `RateLimitResetHint bool`，由请求侧按当前选中分组赋值：`internal/gateway/handler.go` 的 `decisionContextForSelection`（约 972-989 行）已有按分组取值的先例（`connection.Normalize(selection.Group.ConnectionType)`），此处同样从 `selection.Group` 读开关。
- `internal/health/execution_judge.go:582-637` 的 `rateLimitDecision` 中，在现有三级优先级的**最末一级之前**插入文本解析：
  1. 证据 `RetryAfter > 0`（不变）；
  2. `Retry-After` 响应头（不变，仅 model 作用域走 `ParseExplicitRetryAfter`）；
  3. **新增**：开关开启时，从 `attempt.Evidence.Summary` 解析出时长，命中则 `CooldownUntil = now + 解析值 + 1 分钟余量`，`RuleID = "rate_limit.reset_hint"`；
  4. 默认冷却 1 分钟（不变）。
- 冷却作用域沿用现有分流结果：`Cline` 场景下 `ScopeHint` 为空且操作为 chat，落到 `ErrorScopeModel` + `EffectCooldownModel` + `rate_limit.model.default_cooldown`；命中文本解析后仅替换时长与 `RuleID`，作用域不变。
- 该分支与 1 小时上限的关系：`maxRateLimitResetDelay = time.Hour` 只约束**响应头**解析（`internal/health/ratelimit.go:11`），证据 `RetryAfter` 分支本身无上限（`internal/health/execution_judge.go:616-619`）。因此文本解析值只受本设计自设的 [1 分钟, 24 小时] 约束。

### 3.3 解析规则

新增 `internal/health/reset_hint.go`，导出纯函数（可单测、无 IO）：

```
ParseResetHint(summary string) (time.Duration, bool)
```

- 触发锚点：大小写不敏感的 `try again in`。不做泛化的「任意 in + 时长」匹配，避免误吃无关文本。
- 时长部分：按顺序消费 `数字+单位` 片段，单位 `d/w/h/m/s`（天/周/时/分/秒），至少命中一个片段才算成功；支持 `3h 18m`、`45m`、`30s`、`1d 2h`、`1h`。
- 归一化与钳制：得到总时长 `d` 后
  - `d <= 0` → 解析失败；
  - 结果再加 1 分钟余量（上游文案只精确到分钟，边界上会早到）；
  - 上限 24 小时，超过则取 24 小时；
  - 下限 1 分钟。
- 失败语义：返回 `false` 时判定完全走原有默认冷却路径，不产生任何新副作用。

### 3.4 对外报错契约（R4）

- 新增 reason：`reasonModelAccountsUnavailable = reason{Status: 429, Code: "upstream_rate_limited", Message: "No available account for model %s. %s"}`（`internal/gateway/reason.go`）。
  - **保留 Code `upstream_rate_limited`**：下游 new-api 的渠道错误统计、重试分类都按 code 归类，改 code 会改变既有统计口径。
  - Message 动态组装：`No available account for model glm-5.3-flash. Earliest recovery in about 3h 18m.`
- 修改出口：`internal/gateway/handler.go:1524-1528` 由 `reasonUpstreamRateLimited` 换为新 reason；`Retry-After` 仍由既有 `setCooldownRetryAfter` 写入最早恢复时间。
- 模型名来源：该分支位于 `evaluateTargets` 之后，客户端请求的对外模型名已解析（`Handler.Handle` 早于选凭据就解析出模型名）。模型名缺失时退化为不含模型名的措辞，不返回空占位。
- 其余使用 `reasonUpstreamRateLimited` 的位置（`internal/gateway/reason.go:26` 的 `providerErrorReason`）保持不变。
- 该分支不区分分组：其他分组的凭据全部冷却时也会看到新文案。这是有意的（文案本身准确），已写入 PRD 的 R4 与验收 AC7。

## 4. 前端设计

- 开关键统一为 `rate_limit_reset_hint_enabled`，三态语义与其他分组开关一致：未设置 = 继承系统默认（关闭）。
- classic：登记进 `runtimeSettingFields` 与 `groupRuntimeSettingFields` 白名单（`web/src/frontends/classic/app/resources/groups.ts:119-128`），补 `GroupSettingsTab.vue` 的读写与模板行、`group-settings-patch.ts` 的 `cloneOverrides` 与提交路径、以及 `api/control/types.ts` 的 DTO；文案补 `zh-CN/en-US/ja-JP` 三份。
- modern：加入 `runtimeSwitches`（`web/src/frontends/modern/api/group-detail.ts:40`），开关由 `GroupAdvancedPanel.vue` 的 `v-for` 自动渲染，只需补文案（modern i18n 三种语言，`runtimeFields`）。
- 漏登记的后果（已在探查中确认）：classic 漏登记 → 分组页整页报错；modern 漏登记 → `readRuntime` 丢弃该键，保存时把它抹掉。两侧必须同时登记，否则开启后会失效。

## 5. 兼容与迁移

- 无数据库迁移；SQLite / MySQL / PostgreSQL 行为一致。
- 降级风险：开关置 `true` 后回退到不认识该键的旧镜像，旧版本 `ResolveGroupRuntimeSettings` 会返回 `unknown group setting` 并拒绝加载配置。**回退前必须先把该分组的开关清掉。** 实现完成后需要在 PRD 的运维说明与服务器档案里各记一条。
- 与上游的冲突面：仅新增一个 switch 分支、一个字段与一个纯函数文件，函数名与字段名避免与上游可能使用的命名重合。

## 6. 权衡

| 选择 | 备选 | 理由 |
|---|---|---|
| 判定层解析文本 | 执行器解析 | 执行器拿不到分组配置，透传会污染与上游共享的接口 |
| 解析受分组开关控制 | 全局无条件解析 | 上游文案格式不受我们控制；用户明确要求只对当前分组生效，避免影响其他分组 |
| 新增独立 reason（code 不变、仅换文案） | 改 `reasonUpstreamRateLimited.Message` | provider 流式错误路径也用它，改常量会连带改变无关分支的文案 |
| 固定内置上下限（1 分钟 ~ 24 小时）+ 1 分钟余量 | 可配置化 | 无第二方使用需求，避免把配置面扩大到三个新键 |
| 分组专属键（不进 `IsRuntimeSettingKey`） | 完整全局开关 | 本功能只要求分组级开关；登记进全局键列表会新增未授权的全局用户可见开关，并引入「全局写入该键 → 两套前端全局设置页整页报错」的可达缺陷。先例：`parameter_overrides` |

## 7. 测试策略

- `internal/health`：`reset_hint_test.go` 覆盖成功解析（`3h 18m`、`45m`、`1d 2h`、大小写差异）、失败解析（无锚点、0、负数、纯数字无单位）、上下限钳制、以及带余量的边界。
- `internal/health`：在 `model_cooldown_test.go` 同层级补充「开关开启 + Summary 带重置时间 → `CooldownUntil` 约为解析值 + 1 分钟且 `RuleID` 正确」「开关关闭 → 与现状一致」。
- `internal/gateway`：补充「所有候选凭据冷却 → 429、`Retry-After` 等于最早恢复时间、响应体含模型名与无可用账号语义」。
- `internal/state` / `internal/control`：沿用 `affinity_enabled` 的既有测试矩阵（全局 × 分组继承、非布尔值被拒、`effective`/`overrides` 往返）。
- 门禁：`make check`。

## 8. 未决与已知不确定项（实现期确认，不阻塞）

- ~~`ErrorEvidence.Summary` 的截断长度是否总能容纳 `Try again in …` 片段~~ —— **已解决（2026-09-24，实现期实测）**：截断上限是 `execution.MaxErrorSummaryLength = 4096` runes（`internal/execution/contracts.go:301`，由 `mapping.go` 的 `truncateErrorEvidenceSummary` 施加）；生产原文 `Error 429: Daily free limit reached on model z-ai/glm-5.3-flash. Try again in 4h 17m` 长 84 runes，且纯文本 / `{"error":{"message":…}}` / `{"error":"…"}` 三种 body 形态实测均原样通过。余量约 48 倍，**兜底分支不触发，`internal/execution/**` 一行未改**。
- 流式场景（200 + SSE 错误事件）下的限流识别。生产实测 Cline 直接返回 HTTP 429，暂不覆盖；若后续观测到该形态再作为增量处理。
