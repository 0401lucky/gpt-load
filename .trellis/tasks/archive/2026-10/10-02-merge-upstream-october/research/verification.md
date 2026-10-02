# 最终验证记录（2026-10-02）

## 范围

本轮合并固定输入：fork `c79bca8fcee20cec2a6306b474a8a10e3a1061c6` 与 upstream `addd69870cb58108c584d69219c44e23d4bdf3db`。上游独有 44 个提交，fork 独有 33 个提交。16 个文本冲突已解决，另修复未被 Git 标记的迁移注册表/导出符号冲突。

本地合并提交：`37ce4c93b2ac1802169313ef07cc86ebe28b97ba`，双亲分别为上述两个输入。`main` 已快进至该提交，两个 `git merge-base --is-ancestor` 均退出 0；相对固定上游的落后数为 0。提交后的 Git 状态仅剩原有 `.playwright-mcp/` 与 `.trellis/tasks/09-28-credential-probe-real-chat/` 两个未跟踪目录。

## 门禁结果

| 检查 | 结果 |
| --- | --- |
| Go 源码格式 | 通过：1257 个仓库 Go 源文件，`gofmt -l` 零输出 |
| 根 module `go mod tidy -diff` | 通过，退出 0 |
| 根 module `go vet ./...` | 通过，退出 0 |
| `corepack pnpm --dir web install --frozen-lockfile` | 通过，pnpm 11.17.0 |
| 前端 lint / 样式检查 | 通过 |
| 前端 Prettier format | 通过 |
| 前端 type-check / Vite build | 通过，3863 modules |
| `go build -o <临时目录>/gpt-load-upstream-20261002.exe .` | 通过，退出 0 |
| `go test -json -count=1 . ./internal/...` | 已完整执行，63 包通过、8 包无测试/跳过、5 包失败；失败经下述同条件基线复核 |
| CPA module `go mod tidy -diff` / `go vet ./...` | 均通过，退出 0 |
| `git diff --check` / `git diff --cached --check` | 通过 |
| 独立 Trellis 检查 | 未发现兼容性缺陷；修复一处上游测试 gofmt 缩进，定向测试通过 |

本机 PATH 没有 make，按 Makefile 的等价步骤逐项执行。不能把本轮描述为 `make check` 全绿：完整测试仍有已复现的 Windows 基线失败。

## 兼容性专项

- `go test -count=1 ./internal/storage/...` 全部通过；新增测试在真实临时 SQLite 内建立旧 24 条链，保留 11 张表的既有字段/数据、身份、备注、捐献审核与回执，升级后只追加三条账本，再执行一次幂等。新库完整链与新 DDL 中断恢复测试也通过。
- 旧 24 条迁移 production 文件与原 HEAD 内容一致；新三条迁移去掉来源注释并映射导出符号后与上游逐字一致。
- config/state/parameteroverride/control/health/requestlog 定向测试通过；新增控制面断言覆盖新旧设置同次写入、modern 备注与 RPM 同存且 classic 不泄漏。
- 前端审查覆盖两套严格投影、读取、clone/dirty/保存和三语言键；保留模型用量矩阵、可识别的凭据名称与连接诊断。
- 独立检查复核了自动合并的网关、日志用量归属、探测参数指纹及捐献装配。细节见 `storage-verification.md`、`backend-verification.md`、`frontend-verification.md`、`review.md`。

## Windows 基线与环境修复

1. 在 `%TEMP%/gpt-load-upstream-20261002-c79bca8f` 独立 worktree 执行原版本完整测试，固定原提交并复制合并前的忽略 UI 产物。原始 CRLF 基线：61 包通过、8 包无测试/跳过、5 包失败。
2. Git `core.autocrlf=true` 使格式检查、模型目录摘要与 fixture 断言出现差异。工作树只对 CRLF→LF 后与对应 stage-0 blob 完全相同的文件归一，保留并发编辑/内容有差异的文件；因此没有额外 Git 内容变化。前端单独按已跟踪文本清单归一。没有修改全局 Git 配置。
3. 原版本与合并版本都在 LF 条件下复测 5 个失败包：`catalog`、`container`、`platform/authkey`、`gateway`、`webui`。两边均为相同的 **23 个顶层失败测试**，连同失败子测试一起比较，新增失败集合为空。不能仅凭失败包名称相同做此结论。
4. 具体原因一致：catalog/authkey 对 Windows 路径转义的日志断言；container 的环境代理预期；gateway 真 TCP 慢读端时限；webui 的 Bash/Python/PowerShell 脚本与 Windows 路径环境。基线和合并的 SSE fixture、模型目录摘要在 LF 后均通过。
5. pnpm script 内裸 `pnpm` 最初命中另一版本，使用临时 corepack shim 和仅当前子进程 PATH 修正。具体命令见前端记录。
6. `gofmt -l .` 还扫描到原有忽略目录 `tmp/baseline-59d75050`。该目录未删除、移动或改写；最终格式检查使用 `git ls-files --cached --others --exclude-standard '*.go'` 去重后的现存源码列表分批执行，包含本轮新增迁移/测试。

完整失败测试清单见 `baseline.md`。原始 JSON 日志保留在本机 `%TEMP%`：

```text
gpt-load-upstream-20261002-baseline-tests.jsonl
gpt-load-upstream-20261002-merged-tests.jsonl
gpt-load-upstream-20261002-baseline-lf-tests.jsonl
gpt-load-upstream-20261002-merged-lf-tests.jsonl
```

## 明确未验证

- 未配置外部数据库测试 DSN，因此没有实测真实 MySQL/PostgreSQL；SQLite 专项通过，方言实现与 opt-in 测试保留。
- 未执行生产部署、真实上游凭据请求、Docker 镜像发布或浏览器端到端验收；本轮交付是经过上述验证的本地合并。
- 现有 Windows 基线失败未通过跳过、放宽断言或改写无关生产代码掩盖。
