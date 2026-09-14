# 目录结构与分层（后端）

> 决定"这段代码该放哪个包/哪一层"时先读这里。分层边界在本项目不是口号 —— 有测试用 `go list -deps` 强制它。

---

## 依赖方向

```
无内部依赖（叶子层）: accessquota, connection, httplifecycle, outboundproxy,
                      protocol, ratelimit, reasoning, usage, platform/*
纯域层（无 IO）:      scheduler, health, pricing, dialect, telemetry, affinity
运行时状态层:         state（+ state/loader 做「持久化 → 运行时状态」适配）
数据面编排:           gateway   → state, scheduler, dialect, health, ratelimit …
管理面编排:           control   → state, storage/{models,dbtx}, requestlog, subscription/*
生命周期编排:         app       → health, httplifecycle, state, storage
装配根:               container → 几乎全部包（唯一知道全图的地方）
```

关键点：

- `gateway` **不依赖** `control` / `storage` / `requestlog`。
- `control` **不依赖** `gateway`。
- `app` 通过接口反向要求 `control` / `gateway` 实现，不直接 import 它们。
- **`container` 是唯一 import 全部包的节点**，其余包都不该知道装配全貌。

---

## 依赖边界由测试强制

改代码前先知道这几道墙（新增 import 会直接让测试红）：

| 测试文件 | 禁止事项 |
|---|---|
| `internal/gateway/dependency_test.go` | `gateway` / `scheduler` / `state` / `health` / `telemetry` 及其传递依赖，**禁止**出现 `internal/storage`、`internal/control`、`gorm.io/gorm` |
| `internal/scheduler/dependency_test.go` | `scheduler` / `state` / `health` 禁止 `control`、`storage`、`gateway`、`requestlog`、`platform/encryption`、`gorm`、`gin` |
| `internal/telemetry/dependency_test.go` | 禁止 `storage`、`control`、`gorm`、`gin` |
| `internal/subscription/providers/dependency_test.go` | 第三方桥接 SDK（`CLIProxyAPI/v7/gptload-embedded/embedded`）**只能**被 `internal/subscription/providers/**` 直接 import；通用 subscription runtime 不得 import 具体 provider 实现 |

实现方式：`exec.Command("go", "list", "-deps", root)` 后逐个依赖名比对前缀。所以**间接依赖也算**。

---

## 包文档即契约

每个包有 `doc.go` 或首行注释声明职责与纪律，写新包时照做：

```go
// internal/gateway/doc.go
// Package gateway implements the data-plane request validation, routing, and forwarding pipeline.

// internal/health/doc.go
// Package health contains pure-domain upstream result classification and
// failure judgment. It must not depend on HTTP handlers or persistence.

// internal/scheduler/scheduler.go:1
// Package scheduler selects channel targets and credentials without IO or persistence access.
```

---

## 三层落点

| 层 | 位置 | 判据 |
|---|---|---|
| **HTTP handler** | `internal/control/server.go` 与 `control/*_http.go` 的 `func (s *Server) handleXxx(c *gin.Context)`；`gateway/handler.go` 的 `Handle` | 只做解析/校验/响应封装。**handler 层不 import `gorm.io/gorm`** |
| **Service / 应用层** | `internal/control/service.go` 的 `type Service`，以及 `control/*.go` 的 `func (s *Service) Xxx` | 持有 `*gorm.DB` 与事务边界 |
| **持久层** | `internal/storage/{models,dbtx,migrations}` | 只被 `control` / `state/loader` / `requestlog` 使用 |

结构事实（可复核）：`Server` 结构体**没有 `*gorm.DB` 字段**，只有一个 `service *Service`；`gateway.Handler` 同样没有 `*gorm.DB`，对日志只有 `requestLogSink telemetry.RequestLogSink`。方法数量比 handler : service ≈ 1 : 3，业务逻辑确实沉在 service。

---

## 文件命名

每个包大致是固定几件套：

- **`http_routes.go`** —— 唯一的路由声明文件（`app/`、`control/`、`gateway/`、`webui/` 各一份）。
- **`server.go`** —— HTTP 处理器聚合体。
- **`doc.go`** —— 包文档 + 分层纪律。
- **`<feature>_http.go`** —— handler + 查询串解析；**`<feature>_query.go`** —— 查询 DTO + 纯函数过滤/排序/分页；`<feature>.go` —— 领域逻辑。范例：`control/access_key_collection.go` / `_http.go` / `_query.go`。
- 测试与源文件同名 + `_test.go`；跨文件测试辅助用 `*_test_helpers_test.go`。

---

## 构造函数与 DI

- **一律 `NewXxx(params)`**，无 builder、无 setter 模式。同名跨包允许（`NewService` 出现在 3 个包）。
- **生产版包测试版**是固定手法：`NewHandlerWithLifecycle` 内部先调 `NewHandler`，再补生命周期依赖（`gateway/handler.go:196`）。同型：`app.NewEngineWithLifecycle`、`control.NewServerWithReleaseUpdateChecker`、`encryption.NewServiceWithKeyFile`。
- 包内私有构造用小写：`newEngine`、`newServer`。

**装配全部集中在 `internal/container/container.go` 的 `BuildContainer()`**：

```go
providers := []any{
    config.Load,
    func(cfg *config.Config) (*gorm.DB, error) { ... },
    httplifecycle.NewCoordinator,
    state.NewCredentialRegistry,
    // ...
}
for _, provider := range providers {
    if err := dependencyContainer.Provide(provider); err != nil { return nil, err }
}
```

- **不使用 `dig.Out` / `dig.Name` / `dig.Group`**。全仓库唯一的 `dig.In` 是 `app.AppParams`（`internal/app/app.go:82`），可选依赖用 `optional:"true"` tag。
- **多实现靠"闭包 provider 收敛成窄接口"**：同一个 `*requestlog.Service` 被注册成 6 个不同接口（`telemetry.RequestLogSink`、`control.RequestLogReader`、`app.RequestLogRuntime` …），见 `container.go:97-117`。
- **接口定义在消费方包**，实现方不反向 import。例：`gateway.AccessKeyRPMLimiter` 定义在 `gateway/handler.go:53-66`，`ratelimit` 只是实现它。
- 路由绑定在 `BuildContainer` 末尾一次 `Invoke` 完成，通过 `registry.Bind(engine)`。

---

## 路由注册

**生产代码里没有任何一处直接调用 `engine.GET/POST/Group`**（只有测试这么做）。唯一注册点是 `internal/platform/httproute/registry.go` 的 `Bind`。

新增端点的做法是往所属模块的 `HTTPModule()` 里加声明：

```go
// internal/control/http_routes.go
func (s *Server) HTTPModule() httproute.Module {
    return httproute.Module{
        Name:              "control",
        Owner:             httproute.OwnerControl,
        Auth:              httproute.AuthControl,
        Prefix:            "/api",
        NamespacePrefixes: []string{"/api"},
        BeforeAuth:        gin.HandlersChain{i18n.Middleware()},
        Authenticate:      s.authenticate(),
        Routes: []httproute.Route{
            controlRoute("control.groups.list", http.MethodGet, "/groups", s.handleListGroupCollection),
            // 变更类路由前面挂 auditMutation
            controlRoute("control.groups.create", http.MethodPost, "/groups",
                s.auditMutation(newMutationDescriptor("group_create", "group", staticMutationLocator("new"))),
                s.handleCreateGroup),
        },
    }
}
```

规则：

- 路由名 = `<owner>.<resource>.<verb>`；**路径不写前缀**，`Prefix` 由 registry 拼接。
- **Owner 与 Auth 是绑定的**，`validateOwnerAuth` 会拒绝不匹配：`OwnerControl`→`AuthControl`、`OwnerData`→`AuthAccessKey`、`OwnerDonation`→`AuthDonation`、`OwnerSystem`/`OwnerWeb`→`AuthNone`。
- 五个模块在 `container.go` 的 `newHTTPRegistry` 汇总：`app`（system）、`controlServer.HTTPModule()`（control）、`controlServer.DonationHTTPModule()`（donation）、`gatewayHandler`（data）、`webUIServer`（web）。捐献模块的受限鉴权与长期回执契约见 [donation-integration.md](./donation-integration.md)。
- **数据面不变量**：所有 data 路由的 `Handlers` 只有一个 `handler.Handle`，endpoint 差异全部塞进 `Prepare` 与 `PathValidator`；endpoint 目录声明在 `gateway/router.go` 的 `dataPlaneEndpointCatalog()`。
- **全局 fallback 只允许一个**（`multiple global fallbacks are not allowed`），目前唯一使用者是 webui 的 SPA（`webui/http_routes.go:54`）。
- handler 命名固定 `handleXxx`。

---

## DTO 与请求体

- 命名：`XxxRequest` / `XxxResult` / `XxxResponse`（`GroupCreateRequest`、`AccessKeyCreateResult`、`GroupSummaryResponse`）。
- **JSON tag 一律 `snake_case`**。
- 内部结构体显式小写：`normalizedGroupCreate`、`credentialCandidate`。
- **可选字段用泛型 `optionalField[T]`**（区分"未传 / 传了 null / 传了值"，`control/group_write.go:81`）：`struct { Set bool; Null bool; Value T }`。
- **请求体一律走严格 JSON 绑定**：`bindStrictJSON` / `bindOptionalEmptyJSONObject` / `bindOptionalProbeJSON`（`control/server.go:1030` 起）—— 禁用未知字段、拒绝多值、限制 32 MiB。

---

## 两条完整调用链（照抄结构时用）

**写路径（创建分组）**：
`control/http_routes.go` → `server.go` 的 `handleCreateGroup`（要求 `Idempotency-Key`）→ `control/group_idempotency.go` → `group_create.go` 的 `CreateGroup` → `service.go` 的 `writeGroupConfig`（持 `s.writeMu`）→ `withControlTransaction`（`dbtx.Run` + `Mode: dbtx.Write`）→ `storage/models` → `stateloader.BuildCompileInput*` → `state.Compile` → `state.Manager.Publish`。

**读路径（分组详情）**：
`http_routes.go` → `server.go` 的 `handleGetGroupSummary` → `group_detail.go` 的 `GetGroupSummary` → `service.go` 的 `withReadSnapshot`（`Mode: dbtx.ReadSnapshot`）→ `state/loader` → `storage/models`。

**数据面请求**：
`gateway/router.go` 的 catalog → `http_routes.go` → `auth.go` 认证（AccessKey）→ `handler.go` 的 `Handle` → `scheduler.New(snapshot, registry, query)` 选号 → `execution_forward.go` → `provideradapter.Registry` → `execution/bifrost` 或 `execution/cpa` → `gateway/request_log.go` → `requestlog.Service` 异步批写。

---

## 硬约束（改代码时必须守）

- **内存态写入必须经 `state.Manager.Publish`**，发布前必须 `state.Compile(input)` 校验。
- 编译输入必须**从当前事务 `tx` 读取**（`stateloader.BuildCompileInputWithProxy(ctx, tx, ...)`），不是从 `s.db` 读。
- **所有控制面写操作持 `s.writeMu`**（`service.go:320`、`bootstrap.go:21`）。
- **变更类接口必须要求 `Idempotency-Key`**（`requiredIdempotencyKey(c, "create_group")`）。
- `state.Manager.WithCurrentSnapshot` 的回调**不得**解密、探测、编译、访问 DB/网络或打日志（`state/manager.go:55` 明文规定）。

---

## 反模式

- ❌ 在 handler 里写业务逻辑或直接碰 `*gorm.DB`。
- ❌ 在 `gateway` / `scheduler` / `state` / `health` / `telemetry` 里 import `storage` / `control` / `gorm` —— 测试会红。
- ❌ 用 `dig.Out` / `dig.Name` / `dig.Group` 做多实现 —— 用消费方定义的窄接口 + 闭包 provider。
- ❌ 在 `gateway/router.go` 之外直接往 gin engine 注册路由。
- ❌ 把 API DTO 定义进 `internal/storage`（`models/group.go:10` 明文禁止）。
- ❌ 新增第二个全局 fallback。
