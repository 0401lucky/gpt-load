# 合并验证记录

## 固定输入

- fork 起点：`72b24ef8765688577965a668ca1c6bdfcd5564c2`。
- 上游目标：`e4a6ddec49327b2245c3bf5dc6479277d6821180`，相对共同祖先有 50 个提交。
- 本地集成分支：`merge/upstream-20261119`；保留真实 merge 的两个父提交。

## 兼容检查与修复

- 旧 `0001–0033` 共 33 个生产迁移的 staged/working Git blob 与 fork 起点逐项一致；上游价格关联迁移顺延为 `0034`，内部模型及 DDL 名字保持上游形态。
- SQLite `24/27/32/33→34`、旧账本严格前缀、业务/身份/价格/请求正文保留、关闭重开及再次迁移的既有回归已通过。
- 连接测试保留三项 nullable 诊断和可选 `error_detail`，classic/modern DTO、投影、显示、批量 owner/proof 守卫一致；已知凭据、认证头、代理材料及其编码变体共用安全投影。
- `prepareRequest` 脱敏失败可能返回空请求。上游恢复改写块先访问 `prepared.request.Body` 导致 panic；已将完整的原准备错误处理分支前移，lineage/item_id 与恢复后跨组 failover 的回归通过。
- 诊断中的小写/混合百分号转义回显已真实复现；局部修复仅按百分号十六进制做等价匹配，普通 token 字符保持大小写语义。对应两个字段集/元数据及公开 reference 保留的回归通过，修复已进入最终验收。
- 捐献模块、demo 全局白名单阻断及其回归、凭据备注、按模型窗口用量、可信周期/额度冷却分离、参数指纹和 fork GHCR/Compose 目标均保留。
- 最终 Trellis review 审计 497 个上游路径并重点复核 40 个共同修改路径；4 个 reviewer 编辑文件的定向回归、gofmt、gateway/control vet 与前端完整静态门禁通过，无任务范围内待修复问题。

## Windows 同环境证据

- 原完整 Go 测试有 70 个包通过；失败包为 `catalog`、`container`、`platform/authkey`、`gateway`、`webui`。
- 在固定 fork 起点的独立 worktree、相同 Go 1.27.3 与 Git Bash 环境中复现了原有失败：日志 `%q` 路径转义断言、代理变量大小写、真实 TCP 慢读 deadline、`python3` 商店占位程序及 POSIX 发布脚本执行/路径。
- Windows 精确依赖、gofmt、根 module tidy/vet、前端 lint/styles/format/type-check 及独立 CPA tidy/vet/完整测试已有通过记录。
- 原始日志保留在本机 TEMP 的 `gpt-load-20261119-full-go-tests.jsonl` 与 `gpt-load-20261119-baseline-failures.jsonl`；任务未以跳过测试规避失败。

## Linux 完整验收

- WSL Ubuntu 使用独立持久缓存 `/home/lucky0401/.cache/gpt-load-20261119-resume`，Linux clone 通过 staged tree 同步源码，LF/执行位及 tree 身份可验证；不修改全局 Git/工具配置。
- 工具版本：Go 1.27.3、Node 25.6.0、pnpm 11.17.0、jq 1.8.1、Compose 5.6.0。便携工具按官方校验值验证，依赖按 frozen lockfile 安装。
- 首轮 `make check PNPM=pnpm` 在 tree `c6958ac532040806bb4f3b7702ee112628e8d23e` 上完成静态门禁且 74 个包通过；原 Windows 失败均未出现，剩余失败仅为 `webui` 的 Compose CLI 缺失。
- 临时 `docker` shim 只转发到官方 standalone Compose；真实配置解析无需 Docker daemon。`go test -count=1 ./internal/webui -run '^TestCompose'` 已通过。
- 最终 tree `525d32260a943ea8ca0585eb8e995307c5af4aa8` 的 `make check PNPM=pnpm` 通过：75 个根/internal Go 测试包（`-count=1`）、gofmt、根 module tidy/vet、前端 lint/styles/format/type-check、Vite build、最终 Go build 与 diff --check 全通过。
- `third_party/cpaembedded` 的 `go mod tidy -diff`、`go vet ./...`、`go test -count=1 ./...` 全通过，2 个有测试的包通过；另完成 staged diff --check。
- 全部结果和日志保留在该独立缓存的 `logs/results.json`、`make-check.log`、`cpa-{tidy,vet,tests}.log`。交付前按已验收 tree 核对源码，记录更新不会改变验收代码。

## 验证边界与基线观察

- 未提供专用 MySQL/PostgreSQL 测试 DSN，真实网络库未实测；现有方言和恢复合同测试保留。测试进程清除真实 provider/live 与外部数据库 opt-in，不操作生产数据或服务。
- 后端三语言各 50 个 key 一致；本次诊断字段和文案同步。classic 日语 `monitor.ts` 的 `affinityPromptPrefix`、`affinityPromptCacheKey`、`affinityResponseContinuity` 在两个父提交都已缺失，保留为既有 fallback 观察，不扩大本任务修改范围。
- 初始未跟踪 `.playwright-mcp/`、`.trellis/tasks/09-28-credential-probe-real-chat/` 与 ignored `tmp/baseline-59d75050/` 均保留。
