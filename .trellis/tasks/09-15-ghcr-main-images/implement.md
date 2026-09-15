# 执行计划

1. [x] 核对用户授权、main基线、Actions权限与现有Docker/Release能力，建立独立任务及分支。
2. [x] 使用Trellis implement代理实现主分支镜像workflow、Compose覆盖、三语文档及定向契约测试。
3. [x] 主会话准备隔离Docker/静态检查环境；实现方运行定向检查并交接。
4. [x] Trellis check代理独立复核发布来源、权限、通道顺序、可追溯性及数据保留说明。
5. [x] 主会话运行适用make check和真实镜像验证；必要修复后复验，不操作生产。
6. [ ] 同步项目规范，按已确定的main交付方向完成提交/发布，观察GitHub首次运行及实际镜像引用。
7. [ ] 记录实际结果和剩余外部限制，完成任务归档与会话记录。

所有代码/Git/测试均在gpt-load执行。现有.playwright-mcp与tmp验证产物不纳入提交；new-api及clover图片不在改动范围。完整make check优先在Linux LF副本运行，以避免现有Windows CRLF与平台测试基线问题，必须核对被测源码与交付源码。
