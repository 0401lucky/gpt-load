# 技术设计

## 当前缺口与边界

现有CI只在PR运行，Release只在v2标签运行且绑定自托管Runner、tbphp镜像和Docker Hub凭据；默认Compose也指向上游。缺口位于本fork的镜像发布与消费配置。

新增独立main镜像workflow，使用GitHub托管Ubuntu、仓库已有固定SHA Actions和source-build target。现有Release/CI保持各自职责。只修改新workflow、必要的可复用小脚本、Compose/.env.example、三语README与相关infra契约测试。

## 发布契约

- 仓库限定为0401lucky/gpt-load，发布分支限定refs/heads/main；push/main和workflow_dispatch作为入口。
- 构建linux/amd64、linux/arm64，先生成提交号标签的镜像和digest，不直接在未经验证时推动latest/main。
- OCI source指向本仓库，revision为完整GITHUB_SHA；应用VERSION采用可识别的2.x主分支开发版本，不冒充tag Release。
- 优先复用release-verify-image-revision.sh、release-image-digest.sh、release-docker-smoke.sh已有能力。main镜像的运行验证应如实区分完整Release检查与本轮范围，不伪称已执行未运行的扫描。
- 更新通道操作串行，推广前重新核对远端main仍对应本次提交；过时运行不能回写latest/main。以已验证digest推广，事后核对通道引用。
- workflow顶层contents:read，写镜像的job增加packages:write，使用GITHUB_TOKEN；不新增个人凭据，不把凭据写到日志或镜像。

## 消费契约

Compose默认镜像改为ghcr.io/0401lucky/gpt-load:latest，并提供GPT_LOAD_IMAGE覆盖，允许完整镜像引用、提交号tag或digest。原服务名、数据卷、挂载、环境、端口和健康检查保持语义。README的本fork快速开始使用本仓库地址；上游正式发布说明保留并标明区别。

## 验证与恢复

定向检查事件/权限/标签推广条件、真实Compose渲染和镜像覆盖；运行actionlint及适用make check。实际构建后验证平台清单/revision与当前架构真实容器。GitHub首次发布后检查包的可见性和实际拉取条件，不假定新GHCR包默认公开。

服务器更新只更换镜像并重建原服务，保留同一Compose项目、数据库和data/encryption.key；回退可固定前一提交镜像，但数据库回退仍应遵循应用兼容规则。本轮不执行服务器操作。
