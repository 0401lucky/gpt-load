# i18n 准则（后端）

> 后端 i18n 只覆盖控制面错误与提示文案。它和前端 `web/src/i18n/` 是**两套独立系统**，不要混用。

---

## 库与文件

`github.com/nicksnyder/go-i18n/v2/i18n` + `golang.org/x/text/language`。

```
internal/platform/i18n/i18n.go            # Init / 语言解析 / T()
internal/platform/i18n/middleware.go      # Middleware() / AttachRequestLanguage / GetLocalizerFromContext / Message
internal/platform/i18n/locales/zh-CN.go   # 33 条
internal/platform/i18n/locales/en-US.go   # 33 条
internal/platform/i18n/locales/ja-JP.go   # 33 条
```

**locale 资源不是 JSON，是 Go 源码里的 `map[string]string`**（避免 embed / 运行时 IO，键名可编译期检查）：

```go
package locales

// MessagesZhCN contains Simplified Chinese control-plane translations.
var MessagesZhCN = map[string]string{
	"access_key.custom_invalid": "密钥最多 256 个字符，仅支持英文字母、数字和英文符号，不允许空白。",
	"common.success":            "操作成功",
	"route.not_found":           "路由不存在",
}
```

三份文件**键名必须完全一致**，值按 key 字母序排列（gofmt 对齐冒号）。加载在 `Init()` 里逐条 `bundle.AddMessages(...)`，**只填 `Other` 字段**，不用 plural 形式；模板变量用 `{name}` 语法。

---

## 中间件与语言协商

```go
// internal/control/http_routes.go:40
BeforeAuth: gin.HandlersChain{i18n.Middleware()},
```

挂在 `BeforeAuth` 上意味着**鉴权之前就解析好语言**，所以 401/403 响应也是本地化的。404/405 兜底 handler 不走 `BeforeAuth`，需手工调 `i18n.AttachRequestLanguage(c)`（`http_routes.go:525`）。

协商规则（`i18n.go`）：

- **只读 `Accept-Language` 头**，没有 query 参数 / cookie 兜底。
- 默认语言是 **`zh-CN`**（不是 en-US）。
- **安全上限**：header > 4 KiB 或 > 32 个 entry → **静默回退默认语言**（防 DoS，`i18n_test.go:159` 有测试）。
- 按 q 权重降序，`sort.SliceStable` 保稳定。
- Base language 映射：`zh`/`mul` → `zh-CN`，`en` → `en-US`，`ja` → `ja-JP`，**其余（如 `fr-FR`）跳过不报错**，继续看下一个候选。通配符 `*` 映射到 `mul` → `zh-CN`。

---

## key 的组织

点分小写，两种形态：

1. **错误类 key = APIError.Code 的小写下划线形式，按域加前缀**：
   `bad_request` / `internal_error` / `request_too_large` / `bad_gateway` / `route.not_found` / `route.method_not_allowed` / `auth.invalid_key` / `auth.forbidden` / `auth.locked` / `group.not_found` / `group.name_exists` / `group.in_use` / `credential.not_found` / `access_key.custom_invalid` / `idempotency.required` / `model_price.not_found` / `reset_credit.unavailable` …
2. **通用 key**：`common.success`。

规律：**无点的 key 用于通用错误；有点的 key 加域前缀**。`VALIDATION_FAILED`、`INVALID_JSON` 等没有独立 key，统一映射到 `bad_request`。

**翻译失败时返回 msgID 本身**（`i18n.go:142`）—— 漏配 key 不会 panic，客户端会看到裸 key，可被测试发现。

---

## Handler 里怎么用

**正常路径下 handler 从不直接调 i18n**，而是通过 `response` 助手传 msgID：

```go
response.SuccessI18n(c, "common.success", result)
response.ErrorI18nFromAPIError(c, apiErr, "auth.locked")
```

底层 API 只在这几处用：`i18n.GetLanguageFromContext(c)` 取语言写响应头。**任何会随语言变化的响应必须带 `Content-Language` + `Vary: Accept-Language`**：

```go
// internal/control/server.go:1102
func writeSettingsResponse(c *gin.Context, settings SettingsResponse) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Content-Language", i18n.GetLanguageFromContext(c))
	c.Header("Vary", "Accept-Language")
	response.SuccessI18n(c, "common.success", settings)
}
```

**Code → key 的映射集中在 `serviceErrorMessageID`**（`control/server.go:1184`）一个 `switch`，见 `error-handling.md`。

---

## 前后端是两套

| | 后端 | 前端 |
|---|---|---|
| 库 | go-i18n v2 | vue-i18n |
| 资源 | Go `map[string]string`，33 条，编译进二进制 | TS 模块，按 namespace 懒加载 |
| key 形态 | 点分小写（`common.success`） | 嵌套 camelCase（`groups.collection.loading`） |
| 覆盖范围 | 仅控制面错误/提示 | 全部 UI 文案 |

**唯一接口点是**：后端返回**已经本地化好的 `message` 字符串**，前端**直接展示**，并只拿 `code` 做程序化分支（`web/src/api/client.ts:151`、`web/src/app/mutation-outcome.ts:90`）。

→ **前端不得用 `code` 去查自己的 i18n 表重写文案**，那会与后端语言不一致。前端 locale 只负责 UI 静态文案。

---

## 反模式

- ❌ 在 handler 里硬编码用户可见文案（`CONTRIBUTING.md:87` 明文禁止）。
- ❌ 新增 key 时只改一份 locale 文件 —— 三份必须同步，否则漏配语言会显示裸 key。
- ❌ 把 `APIError.Message` 的英文串当作给用户看的文案 —— 它只用于 `err.Error()` 与日志。
- ❌ 让前端按 `code` 查前端 i18n 表覆盖 message。
- ❌ 对 `Accept-Language` 不做上限校验。
- ❌ 忘了给随语言变化的响应加 `Content-Language` / `Vary`。

---

## 验证

```bash
go test -count=1 ./internal/platform/i18n/...
```

新增 key 后自查：三个 `locales/*.go` 的 key 集合是否完全一致。
