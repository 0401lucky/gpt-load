# gpt-load 接收接口执行计划

2026-09-14 已按续接授权开始实施。所有权为 gpt-load 后端及必要规范/测试；协作时不是唯一修改者，不回滚他人文件。接口改动同步父契约与 new-api 后端任务。主会话管理进度与完整门禁，独立检查代理补充真实数据库验证。

1. [x] 读取 backend 规范、父需求/设计/契约及目标代码，确定迁移和秘密配置落点。
2. [x] 实现受限鉴权、Owner/Auth 注册、能力与分组目录接口。
3. [x] 实现加密暂存、逐项规范化、数据库唯一性和长期来源记录。
4. [x] 接入单 key 探测、有界恢复、正式入库与运行时更新，提供稳定回执。
5. [x] 扩展相关测试：混合多行、跨组重复、并发、校验错误、提交后故障、越权和删除后重试。
6. [x] 在 gpt-load 根运行 `go test ./internal/control ./internal/platform/httproute ./internal/storage/...`，再运行 `make check`；只用合成 key 和假上游。
7. [x] 交接接口/错误实例与验证记录，配合父任务跨端验收。

完成证据见 [implementation-handoff.md](research/implementation-handoff.md)、[database-verification.md](research/database-verification.md) 和 [完整 make check 输出](research/intake-make-check-linux.log)。最终 LF 检查快照与当前工作区的 39 份代码/配置/README 内容归一化哈希一致；文档继续按父任务验收更新。

回退关闭新入口；保留凭据、回执和指纹，不删表或补偿扣款。
