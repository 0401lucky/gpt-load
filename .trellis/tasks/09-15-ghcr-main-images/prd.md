# gpt-load main 自动构建与 GHCR 更新

## Goal

让 `0401lucky/gpt-load` 的 main 更新自动产出可供服务器拉取的自有镜像，部署不再误用上游缺少本分支功能的镜像。

## Authorization

用户在获知当前没有 main 自动镜像后，明确同意“新建一个 Trellis 任务，把自动镜像构建补上”，并要求“补上吧，没有自动构建不行”。本轮实现既定的 main → GHCR 流程；沿用本会话已明确的 GitHub/main 交付方向。服务器生产操作不在本轮范围。

## Requirements

- main 推送自动构建；支持在 main 手动重跑，无需自托管Runner或Docker Hub凭据。
- 发布到我们自己的 `ghcr.io/0401lucky/gpt-load`，提供latest、main及可追溯的提交号标签；支持linux/amd64、linux/arm64。
- 镜像使用仓库Dockerfile源码构建，包含已有捐献实现与管理UI，版本/OCI revision能对应提交。
- 失败或过时的运行不能把未验证镜像覆盖为当前更新通道；凭据只通过现有GITHUB_TOKEN的最小所需权限使用。
- 默认Compose采用自有镜像，可用明确配置固定提交号或digest；原有端口、数据卷、身份/加密材料及其它运行参数不变。
- 三语README说明首次使用、拉取并重建、指定版本及保留现有数据的更新方式，并区分本fork主分支镜像与上游正式发布。
- 需要真实构建/运行或GitHub实际运行证据；静态YAML检查不能代替镜像可用性。

## Acceptance Criteria

- [x] main触发和手动main触发配置正确，不允许PR或其它分支发布更新通道。
- [ ] 两个目标平台的镜像清单及revision一致，存在提交号标签。
- [ ] 真实容器健康、版本及应用入口验证通过后更新latest/main；构建失败不会推广通道。
- [x] Compose默认与覆盖配置验证通过；三语部署指引对应实际镜像。
- [x] 定向测试和适用make check通过，独立复核完成。
- [ ] GitHub首次实际构建/发布结果与拉取条件已核实，记录可直接给服务器使用的镜像引用。

## Out of Scope

不重写现有tag Release流水线，不推送上游命名空间或Docker Hub，不部署服务器，不修改业务逻辑/数据库，也不改new-api或原有图片。
