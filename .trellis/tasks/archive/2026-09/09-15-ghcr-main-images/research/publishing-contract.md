# 已核实的发布与验证约束

- 基线main=3b96cfd5，origin为https://github.com/0401lucky/gpt-load.git；本轮分支ci/ghcr-main-images，开始前仅.playwright-mcp未跟踪。
- 远端公开仓库，Actions enabled=true、allowed_actions=all；当前账号具有push/admin权限，无需更改仓库Actions设置。
- .github/workflows/ci.yml仅pull_request；release.yml仅push/v2.*，包含self-hosted Linux/macOS ARM64、tbphp镜像、Docker Hub及Render路径，不宜复用于本fork的main自动发布。
- Dockerfile已有source-build默认target和共享非root runtime，Go1.27、Node24/pnpm11，版本可通过VERSION build-arg注入。不要改变其runtime数据/权限契约。
- release-verify-image-revision.sh已验证linux/amd64与linux/arm64的OCI revision；release-image-digest.sh读取manifest digest。release-image-version.sh仅接受SemVer（不接受build metadata），主分支VERSION应兼容或不错误套用此函数。
- release-docker-smoke.sh可用RELEASE_SMOKE_SOURCE_IMAGE复用已构建镜像，按Docker宿主架构进行实际运行验证；可选择RELEASE_SMOKE_SKIP_SCAN，但必须如实说明范围，不能声称做了被跳过的漏洞扫描。脚本隐藏含合成凭据的详细输出，只清理自己创建的Docker资源。
- internal/webui/container_contract_test.go使用真实docker compose config验证env/端口等行为；workflow_test.go及相关workflow_*_test.go维护现有发布契约。新流程应增加必要的安全/消费行为验证，不改旧Release断言来迁就本fork流程。
- 三语README目前快速开始克隆tbphp/gpt-load，并描述官方镜像:2与:latest的特殊区别；需要明确fork使用方式，保留1.x不能原地迁移到2.x及数据库/encryption.key共同备份的既有说明。
- 本地Docker Desktop 29.6.1、Buildx0.35已可用；上个任务的Linux Go1.27/pnpm11工具缓存可复用，但新变更须重新验证，不能借旧日志作为本轮通过证据。
