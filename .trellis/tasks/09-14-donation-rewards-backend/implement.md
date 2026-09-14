# new-api 后端执行计划

2026-09-14 已按续接授权开始。所有权为 new-api 后端、模型/迁移和测试；协作时不是唯一修改者，不回滚其他改动，不处理已有截图。本地代码分支为 new-api 的 `feat/key-donation-rewards`，任务文件仍在 gpt-load。

1. [x] 读取目标 AGENTS.md、父契约和认证/额度/数据库代码，确定秘密材料持久化及迁移方式。
2. [x] 实现集成客户端、连接配置、组目录投影、活动修订与权限；验证两端包装/错误差异。
3. [x] 实现批次/明细、指纹、资格竞争及原标识恢复，不能用数量猜关联。
4. [x] 实现永久奖励事务、缓存和审计，同资源与明细只能奖励一次，不添加上限或追扣。
5. [x] 提供本人批次与管理记录查询，覆盖筛选、分页、账号禁用和组变化。
6. [x] 在 new-api 根运行受影响测试，候选命令 `go test ./model ./controller ./middleware ./service/...`，按实际范围收敛。
7. [x] 用真实 SQLite/MySQL/PostgreSQL 验证新库、存量升级、重复迁移、并发唯一性和账务中断，记录版本与命令，不用 mock 替代。
8. [x] 与 intake 联调多行、丢回执和重启，向 web 子任务交接真实 API 样例，再由父任务最终验收。

完成证据见 [implementation-handoff.md](research/implementation-handoff.md)、[check-handoff.md](research/check-handoff.md) 和父任务 [真实双服务联调](../09-14-key-donation-rewards/research/cross-service-validation.md)。SQLite 3.50.4、MySQL 8.4.11/5.7.44、PostgreSQL 17.10/9.6.24 的实际矩阵通过；未提供 binary 或 DSN 的 skip 不计作验收。

认证/凭据改动前读取适用 OWASP 指南并验证。回退关闭新提交和 worker，保留余额/业务记录，不直接 SQL 覆盖余额。
