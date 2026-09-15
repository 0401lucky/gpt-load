# 本 fork 的主分支镜像发布

## 边界与入口

- `.github/workflows/ghcr-main.yml` 为 `0401lucky/gpt-load` 的独立发布流程，仅接受该仓库的 main push 或 main workflow_dispatch。
- 不把现有 `ci.yml` / `release.yml` 的自托管Runner、tbphp镜像、Docker Hub或Render发布配置改成本fork用途。正式tag Release与main开发镜像保持独立。
- GitHub Actions使用固定SHA依赖及托管Ubuntu；只有build/promote授予packages:write，verify使用packages:read，认证使用GITHUB_TOKEN。

## 镜像与通道契约

- 使用现有Dockerfile的source-build target，一次构建linux/amd64和linux/arm64。
- 提交标签为 `ghcr.io/0401lucky/gpt-load:sha-<完整提交号>`。同提交重建可能改变tag对应digest；需要精确固定镜像时使用 `@sha256:<digest>`。
- VERSION与OCI version为 `2.0.0-dev.<run_number>.g<sha12>`，revision为完整GITHUB_SHA，source为本仓库地址。版本应满足现有SemVer helper，不能加入它不支持的build metadata。
- 验证和推广只使用build输出的digest，不重新读取可被重建覆盖的提交tag。复用已有digest/revision/version脚本核对两个平台，再在两个原生架构Runner运行Docker smoke。
- main流程明确不执行漏洞扫描，不把运行验证表述为完整Release安全扫描。
- 只有build与全部verify成功才可推广。推广job使用固定并发组、禁止中途取消；小脚本再次校验来源、digest、标签，并在写入前查询远端main。
- 远端main已变化时输出promoted=false，不回退到checkout中的旧引用；API、registry和校验失败均中止。
- latest/main由同一个已验证digest更新，再分别核对。两个tag写入不是事务：失败可能留下部分更新，但写入的候选必须已经验证；在当前main重跑可重新核对通道。

## 服务器消费

- Compose默认 `ghcr.io/0401lucky/gpt-load:latest`，`GPT_LOAD_IMAGE` 可覆盖为完整镜像引用、提交tag或digest。
- 服务器在原Compose项目中pull并up，保留原数据库、data卷与身份/加密材料。数据库和encryption.key必须共同备份，不能把换镜像描述为1.x到2.x的原地迁移。
- 新GHCR包不应假定公开。首次发布后验证实际拉取条件；私有包需要相应读取凭据，公开访问应以匿名请求验证。

## 最小验证

- actionlint、Bash语法、实际Compose解析与MainImage推广故障合约，随后按质量准则执行make check。
- 推广合约覆盖固定digest、过时main、API/registry失败、版本不符和写入后digest不符，不以只匹配YAML文本代替行为。
- 验收应包含实际构建/容器运行和GitHub运行记录；本地fake registry用例、YAML检查或跳过的opt-in测试不是已发布镜像证明。
