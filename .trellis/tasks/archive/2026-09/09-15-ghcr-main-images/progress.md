# GHCR 自动镜像进度

- 用户已同意新建任务并明确要求补上自动构建；按本会话既定的GitHub/main交付方向执行，服务器生产操作不在范围。
- 已完成独立main workflow、固定digest推广、Compose镜像覆盖和三语说明，12种推广路径、6种版本场景及真实Compose验证通过，独立检查无代码阻断。
- Docker本地source-build与临时容器验证通过；健康、内嵌页面、捐献capabilities和日志脱敏均正常，临时容器已清理。
- 续接保留远端最新上游合并85811f21，11份候选文件与LF验证副本一致，最新完整make check退出0。
- 提交范围：本任务的workflow/脚本、Compose/.env.example、三语README、两份Go合约测试、容器发布规范及当前任务记录。原.playwright-mcp、旧任务、新-api和clover图片均不纳入变更。
- 已完成工作提交7a522cb7并快进推送main，远端SHA已核对一致。首次自动运行34913061296的build、amd64/arm64原生verify和promote全部成功，workflow状态active。
- latest/main/sha-7a522cb7ffaf22a7bc53e166de0ef1ffefa75e13均为同一digest，两个平台的revision/version/user一致。无需用户凭据的registry请求验证了三种标签、两个平台配置和18个layer，全部HTTP200。
- 首发镜像已可用于服务器拉取；更新方式与固定digest见research/published-image.md。未调整令牌权限或包访问设置，未操作服务器。归档与日志只补记录，不修改已验证应用或workflow。
