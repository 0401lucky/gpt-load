# 后端开发准则（Backend Guidelines）

> gpt-load Go 后端的实际工程约定。**写代码前先读这里**，再按下面的清单进入对应文件。

---

## Pre-Development Checklist

按你的改动落在哪一层，读对应文件：

| 你要做的事 | 必读 |
|---|---|
| 决定代码放哪个包、加 handler、注册路由、改 DI 装配 | [directory-structure.md](./directory-structure.md) |
| 新增/修改运行时配置键（系统级或分组级）、动 `IsRuntimeSettingKey` | [runtime-settings.md](./runtime-settings.md) |
| 返回错误、写 handler 响应、解析请求参数、加新 APIError | [error-handling.md](./error-handling.md) |
| 加日志、改日志字段、处理敏感信息 | [logging-guidelines.md](./logging-guidelines.md) |
| 新增用户可见文案、加翻译 key | [i18n-guidelines.md](./i18n-guidelines.md) |
| 碰 GORM / SQLite / 迁移 / 事务 / 模型 | [database-guidelines.md](./database-guidelines.md) |
| 写测试、提交前自查、改代码风格 | [quality-guidelines.md](./quality-guidelines.md) |
| 捐献集成、暂存探测、全局接收去重或长期回执 | [donation-integration.md](./donation-integration.md) |
| 维护 new-api 与 gpt-load 捐献的跨仓库调用契约 | [donation-caller-contract.md](./donation-caller-contract.md) |
| 本 fork 的 GHCR 主分支镜像、通道推广和 Compose 更新 | [container-publishing.md](./container-publishing.md) |

**跨层改动**（同时涉及 handler + service + DB，或前后端）额外读 `.trellis/spec/guides/cross-layer-thinking-guide.md`。

**改动前的固定动作**：`grep` 你要改的那个值/常量/字段名。`.trellis/spec/guides/index.md` 的 Pre-Modification Rule 说明了原因 —— 这一步能挡掉大多数"忘了改 X"的 bug。

---

## 三条最容易踩的红线

1. **依赖边界有测试守着**。`gateway` / `scheduler` / `state` / `health` / `telemetry` 里 import `storage`、`control` 或 `gorm` 会让 `dependency_test.go` 直接红。见 [directory-structure.md](./directory-structure.md)。
2. **错误出口只有一个**。控制面的所有错误都经 `writeServiceError`，它保证裸 error 不外泄、客户端断连不写响应、只有 5xx 记日志。见 [error-handling.md](./error-handling.md)。
3. **门禁是固定的**：`make check`。不要自行追加 race、前端测试、E2E 或覆盖率要求；本地也不跑 race。见 [quality-guidelines.md](./quality-guidelines.md)。

---

## Quality Check

在声称完成之前逐条核对：

- [ ] `make check` 通过（跑不了就在结论里说明原因与未验证范围，不要含糊带过）。
- [ ] 没有跨越 [directory-structure.md](./directory-structure.md) 里列出的依赖边界。
- [ ] 错误经 `app_errors` 构造、经 `writeServiceError` 出口；没有裸 error 或 DB 原文外泄。
- [ ] 新增用户可见文案走了 i18n key，且 `locales/{zh-CN,en-US,ja-JP}.go` 三份同步。
- [ ] 改了持久化结构 → 有配套迁移，且迁移遵循 [database-guidelines.md](./database-guidelines.md) 的注册表格式。
- [ ] 日志走 `LogPlaneBestEffort` 并带 plane，字段名 snake_case，无敏感值。
- [ ] 行为变更/缺陷修复带了能复现的测试；测试不用 testify、用 `t.Cleanup`、不依赖真实网络与真实库。
- [ ] 没有夹带无关的重构或格式化（改动聚焦）。

---

## 仓库地图

```
main.go / runtime.go / service_*.go   → 进程入口与 Windows 服务
internal/                             → Go 后端（33 个包，分层见 directory-structure.md）
web/                                  → Vue 3 管理前端（见 .trellis/spec/frontend/）
third_party/cpaembedded               → 独立 Go module，不在 make check 覆盖内
Dockerfile / docker-compose.yml       → 容器与发布
```

常用命令：

```bash
make dev     # 构建 UI 并以 race 检测运行
make run     # 构建 UI 并运行
make build   # 构建 UI 与二进制
make test    # go test -count=1 . ./internal/...
make check   # 完整验收门禁
```

---

**语言**：本 spec 用简体中文书写（与项目注释惯例一致，标识符与代码保持英文）。
