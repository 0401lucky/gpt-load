# 质量与验收准则（后端）

> 声称"改完了"之前先读这里。本项目的门禁是固定的，不要自行加码。

---

## 唯一验收门禁

```bash
make check
```

它按顺序跑 7 件事（`Makefile`）：

1. `gofmt -l .` 必须**无输出**
2. `go mod tidy -diff` 必须干净
3. `go vet ./...`
4. `pnpm --dir web run lint`（eslint，`--max-warnings=0`）
5. `pnpm --dir web run format`（prettier `--check`，只检查不改写）
6. `pnpm --dir web run build`（含 `vue-tsc --noEmit` + `tsc --noEmit`）
7. `go build -o gpt-load .` + `go test -count=1 . ./internal/...` + `git --no-pager diff --check`

**完整测试的固定命令是 `go test -count=1 . ./internal/...`** —— `-count=1` 关缓存，范围是根包 + `internal/...`。

---

## 门禁的边界（不要自行加码）

- **本地不跑 race 测试**。CI 有独立的 `race-tests` job，本地 `make dev` 用 `go run -race`。
- **前端不写也不跑任何测试**（无 vitest / jest / playwright / cypress，`web/` 下无 `*.test.ts`）。前端质量靠静态门禁 + Go 侧契约测试（`internal/webui/*_test.go` 校验 `page_routes.json`）。
- **没有覆盖率要求**，CI 里搜不到 `-cover`，无 codecov 步骤。
- `third_party/cpaembedded` 是**独立 Go module，不在 `make check` 覆盖内**。改它要额外跑：
  ```bash
  cd third_party/cpaembedded && go mod tidy -diff && go vet ./...
  ```
  它的 race 测试按约定只在 CI 跑。

---

## 测试约定

- **不用 testify**（不在 `go.mod`），无 mock 生成器。全部 stdlib `testing` + **手写 fake**。
- **表格驱动是主流**（682 处）：`for _, test := range []struct{ name string; ... }{...}` + `t.Run(test.name, ...)`。
- **断言模板 `got, want`**：`t.Fatalf("newDatabaseDialector() = %q, want %q", got, want)`。循环内累积用 `t.Errorf`，前置条件用 `t.Fatalf`。
- **资源清理用 `t.Cleanup`**（203 处）而非 `defer`；清理里的失败用 `t.Errorf`；重复清理的守卫用 `sync.Once`。
- **环境变量用 `t.Setenv`**（175 处），不要手写 `os.Setenv` + 手工还原。
- **`t.Parallel()` 大量使用**（1248 处），但**改动包级 var 或 `os.Chdir` 的测试不能加**。
- **`t.Skip` 只允许用于 opt-in 外部依赖/平台能力**：未设 `GPT_LOAD_DATABASE_TEST_DSN`、MySQL 专有 DDL、Windows 专有行为。**不允许**用来跳过失败或不稳定的测试。
- **helper 命名有约定**：`mustXxx`（`mustCreate`）、`openXxx(t, ...)`、`assertXxx(t, ...)`、`newXxxFixture(t)`，**首行一律 `t.Helper()`**。
- **测试替身范式**：`serviceFixture` 结构体 + `newServiceFixture(t)`；跨文件共享放 `*_test_helpers_test.go`。
- **同包白盒与外部包黑盒混用**：`package storage`（访问未导出符号）与 `package storage_test`（迁移契约）并存。平台差异用 build tag 拆文件（`db_permissions_unix_test.go` / `_windows_test.go`）。

**注入缝惯用法**：生产代码里放包级函数变量，测试替换 + `t.Cleanup` 还原：

```go
// 生产代码
var hardenManagedFileIfExists = securefile.HardenManagedFileIfExists

// 测试
originalHarden := hardenManagedFileIfExists
hardenManagedFileIfExists = func(path string) error { ... }
t.Cleanup(func() { hardenManagedFileIfExists = originalHarden })
```

**测试不做真实网络与真实库**：内存 SQLite（`sqlitetest.OpenMigrated(t)` 或 `storage.Open(":memory:")`）、需要文件时 `t.TempDir()`、HTTP 用 `httptest`。方言 SQL 用 GORM `DryRun` 断言（详见 `database-guidelines.md`）。

---

## 代码风格

- **gofmt 格式（Tab）**；`.editorconfig` 是权威（UTF-8 / LF / 文件末尾保留换行 / 去尾随空白；`.go` 与 `Makefile` 用 Tab，其余 2 空格，Markdown 例外允许尾随空白）。
- **import 分组（空行分隔）：stdlib → 第三方 → 内部包**（`gpt-load/internal/...`）。
- **别名**：`app_errors "gpt-load/internal/platform/errors"`（避免与 stdlib `errors` 冲突，见 `error-handling.md`）。
- **命名**：导出 `PascalCase`，非导出 `camelCase`；struct JSON tag 用 `snake_case`；构造函数 `NewXxx(params) *Xxx`。
- **标识符用英文，注释优先简体中文**（`CONTRIBUTING.md:86`）。注意 storage 核心代码的注释实际多为英文 —— 跟随所在文件的既有语言。
- **提交格式** `<type>(scope): <summary>`，type ∈ `feat`/`fix`/`refactor`/`docs`/`test`/`chore`：
  ```
  fix(subscription): 避免临时刷新失败锁死凭据
  feat(gateway): 支持 OpenAI Embeddings API
  ```
- 改动保持**小而聚焦**，不夹带无关重构或格式化。

---

## 安全与操作规则

- **未经明确批准不新增依赖**（且要检查许可证兼容性）。
- **未经明确确认不执行破坏性命令**。
- **未被明确要求不 commit、不 push**。
- **不在提交、日志、测试数据、截图中出现任何真实凭据**（`AUTH_KEY` / `ENCRYPTION_KEY` / 上游密钥）。
- **不忽略错误，不写吞掉失败的空处理逻辑**（例外见 `error-handling.md`）。
- 涉及 `ENCRYPTION_KEY` 或未来密钥轮换的操作，**先备份 SQLite 数据库与 `encryption.key`**。
- 缺陷修复或行为变更**先补一个能复现问题的测试**（`CONTRIBUTING.md:60`）。

---

## PR 约定

- 从 `main` 切分支，改动聚焦。
- 按 `.github/pull_request_template.md` 填写，保持**双语标题与章节结构不变**，如实勾选自查清单。
- 改动涉及用户可见能力时，**三份 README（`README.md` / `README_CN.md` / `README_JP.md`）必须同步更新**。
- 提交前跑 `make check`；跑不了要在 PR 里说明原因与未验证范围。

---

## Code Review 自查清单

- [ ] `make check` 通过（跑了就写结论，没跑说明原因）。
- [ ] 新增 import 没有跨越 `dependency_test.go` 划定的依赖边界。
- [ ] 错误走 `app_errors` + `writeServiceError`，没有裸 error 外泄。
- [ ] 新增用户可见文案走 i18n，三份 locale 同步。
- [ ] 新增/修改的持久化字段有配套迁移（见 `database-guidelines.md`）。
- [ ] 行为变更/缺陷修复带了能复现的测试。
- [ ] 日志用 `LogPlaneBestEffort` 且带 plane，字段 snake_case，无敏感值。
- [ ] 没有夹带无关的重构或格式化。
