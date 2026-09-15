# GHCR 自动镜像进度

- 用户已同意新建任务并明确要求补上自动构建；按本会话既定的GitHub/main交付方向执行，服务器生产操作不在范围。
- 已完成独立main workflow、固定digest推广、Compose镜像覆盖和三语说明，12种推广路径、6种版本场景及真实Compose验证通过，独立检查无代码阻断。
- Docker本地source-build与临时容器验证通过；健康、内嵌页面、捐献capabilities和日志脱敏均正常，临时容器已清理。
- 续接保留远端最新上游合并85811f21，11份候选文件与LF验证副本一致，最新完整make check退出0。
- 提交范围：本任务的workflow/脚本、Compose/.env.example、三语README、两份Go合约测试、容器发布规范及当前任务记录。原.playwright-mcp、旧任务、新-api和clover图片均不纳入变更。
- 下一步提交并快进main，观察首次GitHub构建及两种原生平台运行验证，核对GHCR标签、digest和实际读取条件，再完成归档记录。
