# 合并验证记录（2026-10-21）

## 输入与验证范围

- fork 起点：`216024cc31c5de4c31a11777c41746bf9ac752d3`。
- 上游目标：`41c82ac02be5c26bd8851256236036d404e4829b`。
- 本地集成分支：`merge/upstream-20261021`；17 个原始冲突的工作树与索引均已解决。
- 工具：Go `1.27.3 windows/amd64`、Node `25.6.0`、pnpm `11.17.0`、Python `3.11.14`。
- 本机没有 GNU make，以 PowerShell 逐项执行 Makefile 等价门禁。未修改全局工具、代理或 Git 配置。

## 门禁结果

| 检查 | 结果与边界 |
| --- | --- |
| `pnpm --dir web install --frozen-lockfile` | 通过，精确 pnpm 11.17.0；工具来自临时目录 |
| gofmt | 1372 个跟踪/本任务实际 Go 源文件无输出；未处理忽略目录中的历史副本 |
| 根 `go mod tidy -diff` | 通过，保留上游精确依赖与锁文件 |
| 根 `go vet ./...` | 通过 |
| 前端 `run lint` | 通过，包含 ESLint 零 warning 和 check:styles |
| 前端 `run format` | 通过 |
| 前端 `run build` | 通过，包含 vue-tsc / tsc 类型检查，生成内嵌 UI |
| 根 `go build` | 通过；可执行文件只输出到仓库外临时目录 |
| 根 `go test -json -count=1 . ./internal/...` | 完整执行：65 包通过、8 包无测试/跳过、5 包失败；全部失败与本轮固定旧版本相同，详见 `baseline.md` |
| 独立 CPA `go mod tidy -diff` / `go vet ./...` | 通过；固定同一 Go 1.27.3 工具链 |
| 独立 CPA `go test -json -count=1 ./...` | 最终 2 包全部通过，embedded 与 modelcatalog；目录更新、客户端版本、Claude reset-credit、WebSocket 中断等回归包含在内 |
| 发布脚本 fake 回归 | `PYTHONUTF8=1 python -m unittest scripts.test_release_mini scripts.test_release_cli scripts.test_release_tags scripts.test_release_git`：17 个测试通过；没有执行真实发布 |
| 开发版自更新守卫 | 实际执行构建产物 `version` 显示 `2.0.0-dev`，`update` 在下载前拒绝，退出码 1；单测新增 fork main `2.0.0-dev.123.g41c82ac02be5`，断言 fetch 次数为 0 且文件不变 |
| `git diff --check` 与 `git diff --cached --check` | 最终通过；已解决所有未合并索引项 |

完整测试不能描述为全绿：Windows 平台和当前环境仍有 23 个顶层、82 个含子测试失败。本轮同条件基线失败集合逐项相同，新增失败集合为空。没有跳过失败测试或放宽产品断言。

## 格式与工具环境处理

- 首轮 `gofmt -l .` 包含 1019 个忽略的 `tmp` 历史副本，另有实际源码 CRLF。按 Git 跟踪及本任务新增路径检查/整理源码，避免修改用户原有忽略目录；1372 个实际源码最终无格式输出。
- `core.autocrlf=true` 的工作树使 tidy、Prettier 和嵌入 JSON 的精确 digest 首次失败。只规范化相关文本行尾，最终 Git 源码 blob 不增加无关变化；旧 32 个迁移 blob 严格相同，JSON 恢复固定上游 SHA256 `943ca7fe1d19ed019054158303f0dbbd80b3bef43b3aa425a71aa7cb3fb52c2b`。
- 默认 goproxy.cn 返回 unexpected EOF；仅验证进程改用 `GOPROXY=https://proxy.golang.org,direct` 重试成功，不更改依赖版本或全局配置。
- Python 默认 GBK 读取 UTF-8 summary 的两项失败在固定旧版本重现；进程级 `PYTHONUTF8=1` 后 17 项通过。

## 兼容专项

- 旧迁移 0001–0032 冻结，`ValidateCurrent0027` 保留，新增请求体记录为 0033。真实文件 SQLite 的 24/27/32→33、关闭重开、重复执行和业务记录逐列快照通过，见 `storage.md`。
- 可信周期与普通/额度冷却独立；额度恢复不截断统计窗口，旧在途错误不能重写恢复后状态。补充 state/health/gateway/control 交叉回归及凭据重建调度投影回归，见 `core.md`。
- 完整根测试的上述关键新增用例均在最终编辑后编译/运行并通过；随后主会话两轮定向交叉回归再次通过。未以 `[no tests to run]` 的包作为该用例通过证据。
- modern 备注/别名、请求所有权和缓存保留与移动设置分区共存；单项诊断和批量测试共存，两套前端保留 fork 配置和模型用量入口，见 `frontend.md`。
- 捐献恢复和 CPA 目录更新均随 Runtime 生命周期启动/取消，捐献独立鉴权与原路由保留。
- fork `.github/workflows/ghcr-main.yml`、Compose 的 `ghcr.io/0401lucky/gpt-load:latest` 与镜像覆盖合同没有内容变化；上游 release action SHA 和 Docker Go 基镜像更新保留。

## 未验证范围

- 没有配置真实 MySQL/PostgreSQL 测试 DSN；外部数据库升级及 MySQL 隐式提交恢复未实测。
- 没有运行浏览器 E2E、真实上游凭据请求、镜像构建/容器运行或 GitHub workflow；这些不是本次本地合并已完成的证据。
- 没有推送、发布、部署或连接生产数据库。
- Windows 基线失败和双方已有的日文日志三项 affinity 缺键保留并记录，没有夹带无关修复。

## 原始证据位置

本轮日志与只读基线位于仓库外 `%TEMP%/gpt-load-merge-20261021-b0fc72ed/`：`baseline-tests.jsonl`、`merged-tests.jsonl`、对应 stderr、`failure-comparison.json`、`cpa-final-tests.jsonl`、`root-tidy.log`、`root-vet.log`、`root-build.log`、`cpa-tidy.log`、`cpa-vet.log`、`python-tests-utf8.log`、`final-cross-regressions.log`。原有未跟踪内容的 95 个文件 SHA256 快照为 `preserved-user-files.json`，交付前再次核对。

## 本地集成结果

工作合并提交为 `0aee0a0037baaf72ee6cfe0311e33e65d15f3d81`，两个父提交恰为固定 fork 起点及固定上游目标。本地 main 已快进至该提交，双方均为 main 祖先，`main..41c82ac0` 提交数为0。提交后再次核对旧32个生产迁移blob和95个用户原有文件完全一致；任务/日志随后单独归档。未推送或部署。

## 最终全范围审查

Trellis check 复核全部变更与55个 fork/upstream 重叠路径，确认旧迁移冻结/0033逆映射、捐献鉴权与恢复、quota/可信周期/调度投影、备注缓存与单/批诊断、请求体权限/预算和发布配置符合方案；未发现新增阻断问题，无额外源码修复。审查代理再次执行 pnpm11.17.0 lint/styles 与 type-check，退出码均0，重新解析本轮完整根测试确认 added/removed failure sets均为空。详情见 `check.md`。
