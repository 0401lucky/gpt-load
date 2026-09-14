# 日志准则（后端）

> 加任何一条日志前先读这里。日志脱敏是强制的，写日志本身不得影响主流程。

---

## 库与初始化

**logrus**（`github.com/sirupsen/logrus v1.10.2`）。不用 zap / zerolog / slog。

`internal/platform/utils/logger_utils.go`：

```go
func SetupLogger(config LogConfig) {
	logrus.SetOutput(os.Stdout)
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		logrus.Warn("Invalid log level, using info")   // 解析失败只警告，不 fatal
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	if config.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{TimestampFormat: "2006-01-02T15:04:05.000Z07:00"})
	} else {
		logrus.SetFormatter(newCompactTextFormatter())
	}
}
```

- 全局 `logrus.StandardLogger()`，输出 stdout。
- 两种 format：`json` / 默认 compact text（`internal/platform/utils/compact_formatter.go`）。
- 日志在 `runtime.go:28` 的 `Invoke` 里配置，同时挂上脱敏 Hook（见下）。

---

## 必须用封装助手，不要裸调

`internal/platform/utils/event_log.go:50-92`：

```go
func LogBestEffort(logger *logrus.Logger, level logrus.Level, fields logrus.Fields, message string)

func LogPlaneBestEffort(logger *logrus.Logger, level logrus.Level, plane LogPlane, fields logrus.Fields, message string)
```

- **`plane` 是必填且被校验的**：`LogPlaneData = "data"`（数据面/转发）、`LogPlaneControl = "control"`（控制面/管理 API），非法值直接 return 不输出。message 会被加 `[DATA] ` / `[CONTROL] ` 前缀，fields 里的 `plane` 键会被删掉防重复。
- **请求相关日志必须用 `LogPlaneBestEffort`**（全项目 28 处）；非请求的通用日志用 `LogBestEffort`。
- **两条助手都做了 nil logger 检查 + `defer func(){ _ = recover() }()`** —— 日志绝不能让请求失败。这是有意的设计，不要"优化"掉。

另有防刷屏的 `RateLimitedEventCounter.Observe() (uint64, bool)`，`control/server.go:69-72` 用它做 1 分钟窗口限制 auth 失败日志。

---

## 结构化字段

**字段名一律 snake_case**：`group_id`、`access_key_id`、`credential_id`、`retry_after_seconds`、`error_code`、`mismatch_kind`。

**唯一例外**：数据面请求完成日志用极短别名（`ak_id` / `kid` / `up_model` / `in_tokens`），因为它们逐请求输出。字段顺序由 `compact_formatter.go` 的 `compactLogFieldPriority` 权威定义（`time`/`level`/`msg`/`error` 在前，然后 `event`/`status`/`http`/`proto`/`model`/`up_model`/`duration`/`in_tokens`/`out_tokens`/`cost_usd`/`ak_id`/`group`/`kid`/`attempts`/`usage`/`cost_state`/`err`/`err_msg`）。

**`event` 字段是强约定**，值是小写点分或下划线：

- 生命周期用 `<phase>.<noun>`：`startup.ready`、`startup.failed`、`shutdown.http_drain`。
- 审计失败用 `<domain>.<action>_failed`：`subscription.credential_refresh_failed`。
- 简单事件用 snake_case：`request_completed`、`auth_failed`、`mutation`、`model_cooldown`。

**惯用法：先建常用字段字面量，再条件追加可选字段** —— 避免零值噪音（`control/server.go:1266`）：

```go
fields := logrus.Fields{
	"operation":  operation,
	"error_code": code,
	"error_type": fmt.Sprintf("%T", err),
}
if operationErr.groupID != 0 {
	fields["group_id"] = operationErr.groupID
}
```

常见字段组合：服务错误 → `operation`/`error_code`/`error_type`（`fmt.Sprintf("%T", err)`）；鉴权失败 → `event`/`peer_ip`/`reason`/`total`；变更审计 → `event`/`operation`/`resource_type`/`resource_locator`/`outcome`/`status_code`/`error_code`/`peer_ip`。

---

## 级别

| 级别 | 用途 |
|---|---|
| Info | 成功事件；请求成功或取消 |
| Warn | 失败但可恢复、非预期但非故障；审计的失败态 |
| Error | **仅 HTTP 5xx**、不变量破坏、记账饱和 |

**4xx 客户端错误不写日志**（明确的降噪约定，`control/server.go:1162`）：`if apiErr.HTTPStatus >= http.StatusInternalServerError { logServiceError(...) }`。

先建 `fields` 的惯用法见上；级别按结果选择的例子见 `mutation_audit.go:173`（`outcome == "succeeded"` → Info，否则 Warn）。

---

## 脱敏（强制，不是可选）

`internal/platform/redact/hook.go` 是挂在 `logrus.AllLevels` 的 Hook，**对 message 和每个 field 递归脱敏**：

```go
entry.Message = h.redactor.String(entry.Message)
for name, value := range entry.Data {
	if sensitiveFieldName(name) {
		entry.Data[name] = Placeholder    // "[REDACTED]"
		continue
	}
	entry.Data[name] = h.redactValue(value)
}
```

- `sensitiveFieldName`（`redactor.go:89-97`）归一化后匹配：`authorization` / `apikey` / `xapikey` / `xgoogapikey` / `accesskey` / `clientsecret` / `refreshtoken` / `idtoken` / `accesstoken` / `cookie` / `setcookie` / `password` / `passcode` / `secret` / `session` / `signature` / `key` / `token`。
- 递归深度上限 `maxValueDepth = 16`，超限整体置 `[REDACTED]`；遇到 `reflect.Struct` 也整体置 `[REDACTED]`（保守策略）。
- `redact/error_message.go` 的 `ExtractErrorMessage` 会拒绝非 UTF-8、控制字符、疑似 HTML/XML markup 的内容，识别不出已知 message 字段时**返回空串**让调用方回退到安全文案。

对应 `CONTRIBUTING.md:97`：**提交、日志、测试数据和截图中不得包含 `AUTH_KEY`、`ENCRYPTION_KEY`、上游密钥或任何真实凭据。**

---

## 请求关联

- **数据面有 request id**：手写 UUID v4（`internal/gateway/request_id.go`，不引第三方库），响应头 `X-GPTLoad-Request-ID` 回写。**生成失败不中断请求** —— 只是 requestID 置空、遥测关闭并记一条 Warn。request id 也持久化到 DB（`request_logs.request_id`，路由 `/logs/:request_id`）。
- **控制面没有 request id**：靠 `operation` 字符串（`"update_model_price"`）与业务主键（`group_id` / `credential_id` / `access_key_id`）做关联。
- **没有 `logrus.WithContext`，也没有 context 携带 logger 的模式** —— request id 是显式参数传递。

---

## 反模式

- ❌ 裸调 `logrus.Info(...)` 打请求日志 —— 用 `LogPlaneBestEffort` 并带 plane。
- ❌ `logrus.Fatal*` —— 全项目零使用。
- ❌ 把 `err.Error()` 当 message —— 用 `fields["error"] = err` 或 `fields["error_type"] = fmt.Sprintf("%T", err)`，让 Hook 结构化脱敏。
- ❌ 依赖 Hook 兜底而不做字段命名规范 —— `redact` 是最后一道防线，不是借口。
- ❌ 给 4xx 记 Error。
- ❌ 输出零值字段噪音（用条件追加）。
- ❌ 让日志影响主流程 —— 不要去掉助手里面的 nil 检查与 recover。
