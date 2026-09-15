# 本地验证进度

2026-09-15，主会话负责。实际发布结果在GitHub运行完成后补充，不以本页的静态检查代替。

- 独立LF clone位于 `C:/Users/lucky0401/AppData/Local/Temp/ghcr-main-verification-20260915-ef20b018c6924b9cb8e307ddfbb78bb4/repo`；不是改写上一任务的验证快照。9份候选源码按归一化内容哈希同步，独立检查方再次核对一致。
- Go1.27.0 Linux、Node24.11.0、pnpm11.17.0、jq1.8.1、DockerCompose5.3；actionlint1.7.12从官方release下载并核对SHA-256。工具位于本任务临时目录或既有用户缓存，没有修改全局配置/应用依赖。
- Dockerfile的Node、Go、Alpine固定digest均由registry实际确认存在；Go镜像首个匿名token请求出现EOF，单次重试后成功，没有改动Dockerfile。
- Windows Compose用例及Go vet、actionlint、gofmt/Bash语法已由独立检查通过；Linux完整MainImage成功/失败用例及关键分支独立复核通过，详见两份handoff。
- Windows Docker Desktop从同一LF候选树完成真实source-build（linux/amd64），退出0。临时镜像 `gpt-load-ghcr-check:20260915-ef20b018`，manifest digest `sha256:a006db02694329ecde765bdbaae45833ceea702d826e5e70f003bb8d35995ffa`，原始输出在docker-build.log。
- 本地构建使用开发验证版本 `2.0.0-dev.0.g3b96cfd5a5ff`；这验证候选Docker构建路径和未修改的应用源码，不将本地tag当成已发布GHCR镜像。
- 实际容器以10001:10001运行，使用tmpfs /app/data与随机loopback端口；/health为ok并返回预期版本、/返回200 HTML、配置合成独立凭据后捐献capabilities返回protocol_version=1。普通容器日志未命中两个合成凭据。结果见docker-runtime.json。
- 本地运行测试仅使用新建带codex.task=ghcr-main-images标签的容器；结束前核对完整容器ID、标签和镜像，已移除该容器。没有操作服务器、旧临时目录或既有容器。
- 首次完整make check的全过程输出至make-check-linux.log，包含全部Go包及末尾diff检查；会话中断后原执行会话已不可恢复，因此不凭日志推测其退出码。
- 本机gh令牌没有read:packages权限，无法通过Packages REST读取可见性；不扩大令牌权限。首次发布后以实际registry拉取验证可用性，必要时如实说明读取条件。

## 续接与最终基线

- 续接时远端main已到 `85811f21e80175fd5d5360b2ccf2059aa6133add`，包括上游工具兼容转发与WebSocket重连两个修复及合并提交。实际影响14份execution/gateway文件，与本任务CI/Compose改动无冲突；工作分支和LF clone均已快进保留这些更改。
- 三语README在原独立审查后各补一条GHCR公开/私有拉取条件，不包含PAT值；没有改变代码或发布流程。
- 在最新基线重新运行完整make check，**退出码0**，完成于2026-09-15 08:18:08 +08:00。日志为make-check-final.log，结构化退出证据为make-check-final-result.json。包含新增推广合约、全量应用测试、Go/前端检查及构建，webui包5.802s。
- source-manifest.json记录最新基线与11份候选代码/配置/规范文件的归一化哈希；包括补齐的三语README。actionlint及task.py上下文校验在续接后再次通过。
- 本地Docker构建/运行证据对应最初3b96cfd5应用基线；最新85811f21及本轮提交的实际双架构镜像将由GitHub原生Runner验证。不会把旧本地镜像当作新发布镜像。
