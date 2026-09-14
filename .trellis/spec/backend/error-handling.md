# 错误处理与 API 契约（后端）

> 写任何 handler / service / 错误返回前先读这里。控制面错误出口只有一个，绕过它就是 bug。

---

## APIError 与预定义错误

`internal/platform/errors/errors.go` 是全项目唯一源文件：

```go
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
	Data       any
}

func (e *APIError) Error() string { return e.Message }
```

- HTTP 状态码**内嵌在错误里**，不靠调用点再判断。
- **没有 `Unwrap()`、没有自定义 `Is()`/`As()`** —— 判定完全依赖指针身份。
- 共 **45 个包级 `var` 单例**（`ErrBadRequest`、`ErrGroupInUse`、`ErrIdempotencyKeyRequired`、`ErrModelPriceReferenced` …），按 HTTP 状态分组：400 / 401 / 403 / 404 / 409 / 410 / 413 / 428 / 429 / 500 / 502 / 503。
- **Code 是全大写 SCREAMING_SNAKE_CASE 字符串**（`"BAD_REQUEST"`、`"GROUP_IN_USE"`）。Code 名与变量名语义一致但不等同 —— `ErrResourceNotFound.Code` 是 `"NOT_FOUND"`，`ErrValidation.Code` 是 `"VALIDATION_FAILED"`。
- `Message` 里是英文默认串，**不是给客户端看的**（客户端看到的是 i18n 值，见下）。

---

## import 别名是硬约定

只要文件同时用到 stdlib `errors`，**一律别名 `app_errors`**（全仓 109 处）：

```go
import (
	"errors"                                    // stdlib
	app_errors "gpt-load/internal/platform/errors"
)
```

唯一无别名的一处是 `internal/control/project_model_collection_query.go:8`，因为它不用 stdlib `errors`。

---

## 判定与富化

**判定用 stdlib**，但因为 `APIError` 没有 `Is()`，`errors.Is` 退化为**指针相等** —— 只在服务层原样返回同一个单例时才成立：

```go
// internal/control/credential_import_batch.go:261
switch {
case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
	return "import_timeout"
case errors.Is(err, app_errors.ErrCredentialReauthorizationRequired):
	return "reauthorization_required"
case errors.Is(err, app_errors.ErrCredentialAuthOutcomeUnknown):
	return "auth_outcome_unknown"
}
```

**需要携带上下文时，用自定义类型 + `Unwrap()` 指回单例** —— 这是本项目最重要的错误包装惯用法（`internal/control/operation_error.go:26-63`）：

```go
type controlOperationError struct {
	stage        string
	mismatchKind string
	groupID      uint
	credentialID uint
}

func (e *controlOperationError) Error() string {
	return "control operation invariant failed"   // 泛化短语，从不含内部细节
}

func (e *controlOperationError) Unwrap() error {
	return app_errors.ErrInternalServer
}
```

细节放在结构体字段里，由 handler 边界的 `errors.As` 取出来映射 i18n key 与日志字段。

**禁止修改全局单例**：要用 `NewAPIErrorWithData(base, data)` 返回副本（`errors_test.go:65-74` 有 `TestNewAPIErrorWithDataDoesNotMutateBase` 专门守护）：

```go
apiErr := app_errors.NewAPIErrorWithData(
	app_errors.ErrAuthLocked,
	authLockedData{RetryAfterSeconds: seconds},
)
response.ErrorI18nFromAPIError(c, apiErr, "auth.locked")
```

---

## 响应契约

`internal/platform/response/response.go` —— **只有 3 个导出函数**：

```go
type SuccessResponse struct {
	Code    int    `json:"code"`              // 永远是 0
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`              // 大写字符串，与 APIError.Code 一致
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
```

- **成功 `code` 是数字 `0`；失败 `code` 是大写字符串**。没有 `status`/`success` 布尔字段。
- `data` 是 `omitempty`，nil 时**不出现**（`response_test.go:36-38` 明确断言无 `data` 键）。
- 前端严格镜像这组类型（`web/src/api/types.ts:3-13`）。

三个函数：

```go
func Error(c *gin.Context, apiErr *app_errors.APIError)
func SuccessI18n(c *gin.Context, msgID string, data any, templateData ...map[string]any)
func ErrorI18nFromAPIError(c *gin.Context, apiErr *app_errors.APIError, msgID string, templateData ...map[string]any)
```

- **没有非 i18n 的 `Success`**。`SuccessI18n` 全项目 62 处调用，**62 处全部传 `"common.success"`**，无例外。
- `response.Error`（非 i18n）全项目只有 **1 处**：panic recovery 中间件。
- 删除类操作显式传 `nil`，不省略参数：`response.SuccessI18n(c, "common.success", nil)`。

---

## 唯一错误出口：`writeServiceError`

`internal/control/server.go:1142-1176`，约 100+ 处调用。它强制做四件事：

```go
func writeServiceErrorResponse(c *gin.Context, operation string, err error) {
	if requestWasCanceled(c.Request.Context(), err) {
		return   // ① 客户端断连：既不写响应也不记日志
	}

	var apiErr *app_errors.APIError
	if errors.As(err, &apiErr) {
		setMutationErrorCode(c, apiErr.Code)                    // ③ 供审计中间件读取
		if apiErr.HTTPStatus >= http.StatusInternalServerError {
			logServiceError(operation, err, apiErr.Code)        // ④ 只有 5xx 才记 Error
		}
		response.ErrorI18nFromAPIError(c, apiErr, serviceErrorMessageID(operation, err, apiErr))
		return
	}

	// ② 非 APIError 的裸 error 一律降级为 500，原文永不外泄
	setMutationErrorCode(c, app_errors.ErrInternalServer.Code)
	logServiceError(operation, err, app_errors.ErrInternalServer.Code)
	response.ErrorI18nFromAPIError(c, app_errors.ErrInternalServer, "internal_error")
}
```

**Code → i18n key 的映射集中在 `serviceErrorMessageID`**（`server.go:1184-1264`）的一个 `switch` 里，多对一映射，并用 `operation` 字符串消歧（如 `update_model_price` 的 not-found 走 `model_price.not_found`，其余走 `group.not_found` / `credential.not_found`）。

---

## Handler 标准流程

```
请求
 → i18n.Middleware()                     // BeforeAuth，鉴权前就解析语言，所以 401/403 也是本地化的
 → s.authenticate()                      // 失败直接 ErrorI18nFromAPIError + Abort
 → auditMutation(...)                    // 仅变更路由，defer 记 outcome
 → handleXxx(c)
 → service.Xxx(c.Request.Context(), …)
 → writeServiceError / response.SuccessI18n
```

**读接口模板**（`internal/control/group_collection_http.go:23`）：

```go
func (s *Server) handleListGroupCollection(c *gin.Context) {
	query, apiErr := parseGroupCollectionQuery(c.Request.URL.RawQuery, c.Request.URL.ForceQuery)
	if apiErr != nil {
		writeServiceError(c, "list_groups", apiErr)
		return
	}
	result, err := s.service.ListGroupCollection(c.Request.Context(), query)
	if err != nil {
		writeServiceError(c, "list_groups", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}
```

**写接口模板**（`internal/control/server.go:964`）：严格 JSON 绑定 → 校验 → （需幂等时）`requiredIdempotencyKey` → `service.XxxIdempotent` → `writeServiceError` / `SuccessI18n`。

---

## 输入校验的硬规则

**Query 参数**（`parseGroupCollectionQuery` 是范本，`access_key_collection_http.go` 同构）：

- **拒绝未知参数**：`switch key { case "q", "status", …: default: return …, app_errors.ErrBadRequest }`。
- **拒绝重复参数**：`if len(entries) != 1 { return …, app_errors.ErrBadRequest }`。
- `forceQuery && rawQuery == ""`（即裸 `?`）也是 `ErrBadRequest`。
- 数字手写解析，**禁止前导零、负数、非数字**。
- 枚举用显式 `switch` 白名单，`default` 返回 `ErrBadRequest`。
- 长度限制用 `utf8.RuneCountInString`（**按 rune 不按 byte**）。
- 解析函数签名固定 `func(...) (XxxQuery, *app_errors.APIError)` —— **校验失败返回 `*APIError` 而非 error**。

**JSON body**（`bindStrictJSON` + `internal/control/strict_json.go`）：

- 上限 `maxControlJSONBodyBytes = 32 << 20`（32 MiB），超限 → `ErrRequestTooLarge`。
- body **必须是 JSON object**，拒绝数组/裸值/空 body。
- **拒绝重复字段**（`rejectDuplicateJSONFields` 全量 token 扫描）。
- `DisallowUnknownFields()` + `UseNumber()`，且**拒绝尾部多余内容**（`"multiple values"`）。
- 错误映射单一入口 `mapControlJSONError`：`MaxBytesError`→`ErrRequestTooLarge`，其余→`ErrInvalidJSON`。

**PATH 参数**：helper 返回 `(T, bool)` 且**内部自己写响应**，handler 里只写 `if !ok { return }`，不要重复写错误：

```go
func accessKeyID(c *gin.Context) (uint, bool) {
	parsed, err := strconv.ParseUint(c.Param("id"), 10, strconv.IntSize)
	if err != nil || parsed == 0 {
		writeServiceError(c, "access_key_id", app_errors.ErrBadRequest)
		return 0, false
	}
	return uint(parsed), true
}
```

---

## panic 兜底

`internal/app/recovery.go`：用 `gin.New()` + 手工 `engine.Use(recoveryMiddleware())`，**不用 `gin.Default()`**（避免 gin 自带 Logger 打印未脱敏信息 + 双份 Recovery 冲突）。

```go
defer func() {
	if recovered := recover(); recovered == nil {
		return
	}
	logrus.Error("Recovered from HTTP handler panic")   // panic 值本身不写日志
	if !c.Writer.Written() {
		response.Error(c, app_errors.ErrInternalServer)
	}
	c.Abort()
}()
```

变更审计中间件（`mutation_audit.go:118-131`）在 panic 时**先记审计再 re-panic**，把错误交给上面的 recovery。

---

## 反模式

- ❌ **把裸 error 返回给客户端** —— `writeServiceErrorResponse` 会兜底成 500 `internal_error`，别指望绕过它。
- ❌ **在 handler 里 `c.JSON`** —— 全 `internal/` 只有 2 处非测试调用：`/health` 探针与 `http.MaxBytesReader`。所有 `/api/*` 必须走 `response.*`。
- ❌ **直接改全局 `*APIError` 单例的 `Data`** —— 用 `NewAPIErrorWithData`。
- ❌ **用 GORM 原始错误构造响应** —— 必须经 `app_errors.ParseDBError`（见 `database-guidelines.md`）。
- ❌ **在 handler 里硬编码用户可见文案** —— `CONTRIBUTING.md:87` 明文禁止，走 i18n key。
- ❌ **用 `panic` 传播业务错误** —— 非测试代码只有 6 处 `panic`，全是启动期致命错误或不可达分支。
- ❌ **吞错** —— 只有三类允许：日志/遥测路径的 `defer func(){ _ = recover() }()`、限流降噪计数器、客户端断连。
- ❌ **4xx 也记 Error 日志** —— 只有 5xx 记 Error（降噪约定）。
- ❌ **`logrus.Fatal*`** —— 全项目零使用。

---

## 验证

```bash
go test -count=1 ./internal/platform/errors/... ./internal/platform/response/...
go test -count=1 ./internal/control/...
make check
```
