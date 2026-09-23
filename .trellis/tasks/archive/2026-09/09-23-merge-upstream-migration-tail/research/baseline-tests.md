# 改动前基线（2026-09-23，本机 Windows）

> 采集者：trellis-implement 子代理，在合并前的工作树（`59d75050`）上真实执行。
> 按 dispatch 边界，**未运行全量 `go test ./internal/...`**（主线负责，避免重复长耗时），
> 因此这里是"区域基线"而非逐包全量基线。

| 命令 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test ./internal/storage/... -count=1` | exit 0（`storage` / `dbtx` / `migrations` / `models` 全 ok） |
| `go test . -count=1` | exit 0 |

原始日志：`tmp/baseline-20260923-165432.log`（`tmp/` 为本机忽略目录）。

未采集：`go test ./internal/...` 的逐包失败集合。合并后（`0af4e399`）同样只跑了 `./internal/storage/...`，
故 AC3 的"与基线逐包对比"需要主线补跑一次全量后确认。

---

## 主线补充：全量逐包基线对比（2026-09-23）

为满足 AC3，主线在**仓库外的纯净副本**上采集全量基线，避免与合并中的工作树互相干扰：

```bash
git archive 59d75050 | tar -x -C tmp/baseline-59d75050
cd tmp/baseline-59d75050 && go test -count=1 . ./internal/... > ../baseline-59d75050-tests.txt
```

合并后（`0af4e399`）以同样命令采集 `tmp/merged-tests.txt`，逐包/逐用例对比：

| 维度 | 基线 `59d75050` | 合并后 `0af4e399` |
|---|---|---|
| `go build ./...` | exit 0 | exit 0 |
| `go vet ./...` | exit 0 | exit 0 |
| 失败包 | `catalog`、`container`、`gateway`、`platform/authkey`、`webui` | 同左（集合完全一致） |
| 失败用例 | 见基线日志 | 基线全部失败用例 + **1 个新增**：`TestCodexModelCatalogSnapshotDigest` |

### 新增失败项已定位为环境伪失败（非回归）

`internal/catalog/client_model_test.go` 断言嵌入 JSON 的 sha256：

```
client_model_test.go:13: Codex model catalog digest = "05e9b0…", want "7b15fec…"
```

原因：本机 `core.autocrlf=true` 把 `internal/catalog/codex_client_models_0.155.0.json` 检出为 CRLF（工作区实测 1389 处 CRLF），
而期望摘要取自仓库内的 LF 内容。在 blob 级别直接校验：

```bash
git cat-file blob HEAD:internal/catalog/codex_client_models_0.155.0.json   # sha256 = 7b15fec55ed279c2a0f4b6dfd1f7d2617d7c9f22e94d385242a8cd9534411d38 = 期望值
```

把该文件临时改写为 LF 后单跑该测试 → `ok gpt-load/internal/catalog 1.395s`。
结论：**Linux CI（LF 检出）不受影响，本机属检出伪失败**，与 2026-09-20 会话记录的 CRLF 假阳性同类。

### 前端门禁（主线执行）

| 命令 | 结果 |
|---|---|
| `pnpm run type-check` | exit 0 |
| `pnpm run lint` | exit 0（`eslint --max-warnings=0` + 新版视觉变量/复用/断点检查通过） |
| `pnpm run build` | exit 0（vite build 15.3s） |

> 本机 `pnpm` 默认解析到全局 10.32.1，与 `engines.pnpm=11.17.0` 冲突；按仓库 Dockerfile 的写法 `corepack install --global pnpm@11.17.0`
> 并在 PATH 前置 `tmp/shims/pnpm.cmd`（内容为 `corepack pnpm %*`，供脚本内嵌套调用 `pnpm` 使用）后全部通过。未改动任何仓库文件。
