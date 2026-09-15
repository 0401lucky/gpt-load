# GHCR main 镜像审查交接

审查范围：本任务的新 workflow、main 通道推广脚本、Compose 镜像覆盖、三语 README 及相关 Go 基础设施合约。按主会话分工保持源码只读，仅写本报告。源码审查、静态检查和独立 Linux 高风险分支验证已完成，未发现代码问题。

## Findings (fixed)

- 无。审查期间未发现需要修改的局部代码问题，未改动实现方文件。

## Findings (not fixed)

- 当前未发现源码阻断问题。
- 完整 `make check`、真实镜像构建/运行及 GitHub 首次发布由主会话负责；本报告中的静态和模拟测试不能代替这些证据。首次发布后仍需核实 GHCR 包可见性、实际拉取及通道 digest。尚未执行的项目不记为通过。
- 现有 `.trellis/spec/` 没有容器 main 发布契约条目，已建议主会话补充长期约束：fork main 与上游 Release 分离、按已验证 digest 串行推广并检查远端 main，以及镜像覆盖时保留原项目数据卷。

## 源码核对

- `ghcr-main.yml` 仅接受本仓库 main 的 push/manual 入口；各 job 重复约束仓库、ref 和事件，Actions 固定完整 SHA，checkout 不保留凭据。
- 权限以 `contents: read` 为默认；build/promote 使用 `packages: write`，verify 仅有 `packages: read`，均使用 `GITHUB_TOKEN`。
- 复用 Dockerfile 的 `source-build` 和已有非 root runtime；提交镜像包含完整 revision/source 标签及可识别的 2.x dev 版本。构建阶段只发布完整提交 SHA 标签。
- verify 在 amd64/arm64 原生 runner 上按构建返回的 digest 运行已有烟雾脚本；版本、双架构 revision 和管理页面等检查通过后，promote 才具备执行条件。跳过漏洞扫描在 workflow 注释、烟雾输出和运行摘要中如实声明。
- promote 使用共享并发组且不取消正在推广的 job；先检查已验证 digest 的清单、revision、版本，再立即读取远端 main。查询失败/格式异常直接失败，旧 SHA 不写通道；单次 `imagetools create` 从固定 digest 设置两个通道，并读回两个标签核对。
- Compose 仅改变镜像字段，保留端口、环境、健康检查、卷和优雅停止语义。文档说明同一项目 pull/up、备份数据库与加密密钥、SHA 标签可重建而 digest 不可变、上游产物与本 fork 的区别，保留 1.x 不可原地迁移说明。

## Verification

- Lint：`actionlint 1.7.12 -oneline .github/workflows/ghcr-main.yml` 通过，工具由主会话核对官方 checksum；`git diff --check` 通过，两份改动 Go 测试文件的 `gofmt -l` 无输出。
- TypeCheck / 静态分析：当前源码 `go vet ./internal/webui` 通过；Compose 测试编译通过，覆盖新增 Go 测试文件的类型检查。
- Tests：Windows 当前源码执行 `go test -count=1 ./internal/webui -run '^TestCompose'` 通过，结果 `ok gpt-load/internal/webui 2.705s`，使用真实 Docker Compose 配置渲染，包含新增默认/空值/完整 SHA/digest 覆盖及数据卷合约。
- Linux 动态故障分支：使用主会话同步的 LF 副本与 `env.sh`，独立执行 `TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest` 下的 `current_digest_ignores_rebuilt_commit_tag`、`stale_run`、`remote_failure`、`incorrect_promoted_digest` 四个子用例，全部通过，结果 `ok gpt-load/internal/webui 0.952s`。这些测试实际调用推广脚本及既有 digest/revision/version helper，以本地替身记录 registry/API 交互；不等同于真实 GHCR 验证。
- 验证来源：比较当前仓库与 LF 副本的九份改动源码，仅归一化 CRLF/LF，全部相同。已确认旧 `ci.yml`、`release.yml` 和 `Dockerfile` 无改动。
- 以上 Go 命令均使用 `GOFLAGS=-p=2`、`GOMAXPROCS=2`，未运行本地 race。

Linux 抽验命令为 `go test -v -count=1 ./internal/webui -run '^TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest/(current_digest_ignores_rebuilt_commit_tag|stale_run|remote_failure|incorrect_promoted_digest)$'`。通过 `wsl --distribution Ubuntu --exec bash <验证根目录>/env.sh ...` 调用；`--exec` 避免默认 zsh 在 Go 启动前展开正则。
