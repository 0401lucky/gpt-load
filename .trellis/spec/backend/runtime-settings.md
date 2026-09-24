# 运行时配置键：作用域与联动（后端）

> 新增或修改任何 runtime 配置键（系统级 / 分组级）前先读这里。
> **作用域选错的后果不是"字段被忽略"，而是两套前端的全局设置页整页报错。**

---

## 1. 触发条件

改动命中以下任一条，就必须走完本文的作用域判定与联动清单：

- 在 `internal/state/runtime_settings.go` 新增/删除 `SettingXxx` 常量
- 往 `RuntimeSettings` / `ResolvedGroupSettings` 加字段
- 改 `IsRuntimeSettingKey` / `ValidateRuntimeSetting` / `ResolveRuntimeSettings` / `ResolveGroupRuntimeSettings` 的 case
- 往 `GroupEffectiveConfigResponse`（`control/group_detail.go`）加字段
- 往分组的 `overrides` JSON 列写新键

这是**跨层契约变更**：同一个键会同时流经「配置写入 → 快照编译 → 控制面投影 → 前端白名单」，任一层漏登记都会静默失效或整页报错。

---

## 2. 两条链路的契约

### 2.1 全局链路（系统级设置）

| 环节 | 位置 | 判据 |
|---|---|---|
| 写入校验 | `control/settings.go` 的 `normalizeSettingUpdates`（键放行） | 只放行 `state.IsRuntimeSettingKey(key)` 与少数特例键（`requestredact` / `outboundproxy` / `jev` / `requestaudit`），否则 `app_errors.ErrValidation` |
| 值校验 | 同文件调用 `state.ValidateRuntimeSetting(key, value)` | 该函数的 `default` 分支对未知键报 `unknown runtime setting` |
| 读回 `overrides` | `control/settings.go` 的读取路径 | 按 `state.IsRuntimeSettingKey(row.Key)` 过滤 DB 已存键 —— **凡是能全局写入的键，一定会出现在 `GET /api/settings` 的 `overrides` 里** |
| 读回 `values` | `SettingsValuesResponse`（`control/settings.go:46`，在 `:422` 显式构造） | 逐字段显式赋值，**不是** `RuntimeSettings` 直出；新增 `RuntimeSettings` 字段不会自动泄漏到这里 |

### 2.2 分组链路

| 环节 | 位置 |
|---|---|
| 写入 | `normalizeGroupSettings`（`control/group_create.go`）只特判 `retry_count`（禁止）与 `parameter_overrides`（专用校验）；**不查 `IsRuntimeSettingKey`** |
| 接受/解析 | `state.ResolveGroupRuntimeSettings`（`state/runtime_settings.go`）的 `case`；未知键落到 `default` → `unknown group setting` |
| 基线继承 | 同函数把 `base RuntimeSettings` 的对应值种入 `ResolvedGroupSettings` 初值，分组显式值覆盖 |
| 快照 | `state.Compile` → `GroupView`（`state/snapshot.go`） |
| 控制面投影 | `effectiveGroupConfig` → `GroupEffectiveConfigResponse`（`control/group_detail.go`）的 `effective`；`overrides` 是 `config.Settings` 原始 map，**无白名单、原样下发** |

**关键不对称**：全局链路按 `IsRuntimeSettingKey` 放行，分组链路不查它。这正是作用域机制能被"选"出来的原因，也是本节存在的理由。

---

## 3. 作用域二选一（新增键的第一步）

新增键前必须先回答：它是**公共 runtime 键**还是**分组专属键**？

| | 公共 runtime 键 | 分组专属键 |
|---|---|---|
| `IsRuntimeSettingKey` | **登记** | **不登记** |
| `ValidateRuntimeSetting` | 加 `case` | 不加（全局 PUT 已被挡在前面，加了是死代码） |
| `RuntimeSettings` 字段 | 需要 | **仍然需要**（分组继承的基线来源） |
| `DefaultRuntimeSettings` | 加默认值 | **仍然要加** |
| `ResolveRuntimeSettings` case | 需要 | **仍然需要**（供分组继承） |
| `ResolvedGroupSettings` + `ResolveGroupRuntimeSettings` case | 需要 | **需要** |
| 全局可写 | 是 | **否**（全局 PUT 被拒） |
| 前端要动的页面 | 全局设置页 **+** 分组设置页 | 只动分组设置页 |
| 后端全局 `values` | 要进 `SettingsValuesResponse` | 不进 |

**判据**：分组专属键能成立的前提是「它不需要被全局改」。一旦登记进 `IsRuntimeSettingKey`，全局 PUT 就接受它、全局 `overrides` 就会带上它，而两套前端的**全局**设置页对白名单外的 `overrides` 键是**硬失败**：

- classic：`web/src/frontends/classic/app/resources/settings.ts:230`、`:239` 的 `if (!runtimeSettingKeys.includes(...)) invalidResponse()`
- modern：`web/src/frontends/modern/api/settings.ts:171` 的 `oneOf(key, settingKeys)`

即：**一次 `PUT /api/settings` 就能让两套前端的全局设置页整页报错**，而 API 会明确接受这次写入（不是脏数据）。

**先例**：`parameter_overrides` 是分组专属键的既有样板 —— 常量在 `runtime_settings.go`、case 在 `ResolveGroupRuntimeSettings`，但**不在** `IsRuntimeSettingKey`、**不在** `RuntimeSettings`、**不在** `ValidateRuntimeSetting`。它的守护测试是本模式的范本：

```go
// internal/state/parameter_overrides_test.go
if IsRuntimeSettingKey(SettingParameterOverrides) {
    t.Fatal("IsRuntimeSettingKey() exposed group-only parameter overrides")
}
```

> **Warning**：`ResolveRuntimeSettings` 为分组专属键保留 `case` 是**有意的不对称**，看起来像"忘了加进白名单"。
> 不要为了"对称"把它补进 `IsRuntimeSettingKey` —— 那会重新打开上面的整页报错路径。请在该 `case` 上就近写明理由。

---

## 4. 校验与错误矩阵

| 条件 | 结果 |
|---|---|
| 全局 PUT 带分组专属键 | `app_errors.ErrValidation` |
| 全局 PUT 带不存在的键 | `app_errors.ErrValidation` |
| 全局 PUT 带公共键但值类型错 | `app_errors.ErrValidation`（`ValidateRuntimeSetting`） |
| 分组 overrides 带未知键 | 快照编译期 `unknown group setting`，该分组配置无法加载 |
| 分组 overrides 带该键但值非布尔（布尔键） | `strictBoolean` → 报错；**不要**用弱转换静默取默认值 |
| 键未设置 | 取系统级基线；基线本身由 `DefaultRuntimeSettings` 决定 |

---

## 5. Good / Base / Bad

- **Good**：新增分组专属开关 → 常量 + `RuntimeSettings`/`ResolvedGroupSettings` 字段 + 默认值 + 两个 `case`；**不进** `IsRuntimeSettingKey`；补反向守护断言；只登记分组设置页。
- **Base**：新增全局开关（如 `affinity_enabled`）→ 登记 `IsRuntimeSettingKey` + `ValidateRuntimeSetting` + `SettingsValuesResponse`，并**同步登记两套前端的全局设置页白名单**（classic `runtimeSettingKeys` + `TimeoutSettingKey` 的 `Exclude`、modern `settingSwitches`）与分组设置页。
- **Bad**：只做了后端全局登记，前端只补了**分组**设置页 —— 编译、单测、`go vet` 全绿，问题只在一次全局写入后才暴露。

---

## 6. 必须的测试

断言点（缺一不可）：

1. **默认值与继承**：`ResolveRuntimeSettings(nil)` 取到新默认值；`ResolveGroupRuntimeSettings(基线, nil)` 继承它；分组显式值覆盖它。
2. **作用域守护**（分组专属键）：`IsRuntimeSettingKey(新键)` 为 **false**，并用反向断言写法对齐 `parameter_overrides`；同时把该键补进 `TestIsRuntimeSettingKeyRecognizesOnlyPublicRuntimeKeys` 的负例列表。
3. **非布尔值被拒**：对 `strictBoolean` 类键，`ResolveRuntimeSettings` / `ResolveGroupRuntimeSettings` 收到 `nil` / `0` / `"true"` 都要报错。
4. **全局路径封闭**（分组专属键）：把该键塞进全局设置更新的入口（`normalizeSettingUpdates`）必须拿到 `ErrValidation`。**同时跑一个既有公共键作为反向对照**，证明用例真的走了那条分支而不是恒失败。
5. **控制面往返**：`GetGroupSettings` → `UpdateGroupSettings` → 再 `GetGroupSettings`，`effective` 与 `overrides` 都要保住新值；`inherit` 之后 `overrides` 里**不应**残留该键。
6. **快照同步**：同一用例里断言 `manager.Current().Groups[id]` 上的对应字段。

---

## 7. Wrong vs Correct

#### Wrong

```go
// 分组专属键，却登记进了全局放行列表 —— 单测全绿，一次全局写入后
// 两套前端的全局设置页整页报错。
func IsRuntimeSettingKey(key string) bool {
	switch key {
	case SettingAffinityEnabled,
		SettingRateLimitResetHintEnabled, // ← 错：这是分组专属键
		...
```

#### Correct

```go
// 不进全局放行列表；全局 PUT 在 normalizeSettingUpdates 里就被拒。
func IsRuntimeSettingKey(key string) bool {
	switch key {
	case SettingAffinityEnabled,
		SettingResponsesWebsocketEnabled,
		...
```

```go
// 但仍保留系统级 case，为分组提供可继承的基线（不对称是有意的）。
case SettingRateLimitResetHintEnabled:
	// 分组专属键：不在 IsRuntimeSettingKey 里，这里只解析可被分组继承的基线值。
	parsed, err := strictBoolean(key, value)
	if err != nil {
		return RuntimeSettings{}, err
	}
	resolved.RateLimitResetHintEnabled = parsed
```

---

## 相关

- 前端侧的登记清单见 [frontend/data-layer.md](../frontend/data-layer.md) 的「新增分组级设置键」。
- classic 的 `assertNoSecretLikeFields` 严格白名单总则见 [frontend/data-layer.md](../frontend/data-layer.md)；"只有 modern 该看到的字段"用 `json:"-"` 处理，见 [directory-structure.md](./directory-structure.md) 的「DTO 与请求体」。
- 降级方向是**单向**的：新版本接受新键，回退到不认识该键的旧镜像时旧版本会拒绝加载该分组配置。**回退前必须先清掉该键**，并写进交付说明。
