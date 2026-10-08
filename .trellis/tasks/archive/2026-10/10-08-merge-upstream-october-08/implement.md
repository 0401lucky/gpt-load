# 执行计划

- [x] 检查固定输入、17 提交、338 文件、14 冲突，读取前次合并记录与迁移规范。
- [x] 获得创建并继续合并授权，完成 PRD 收敛、设计和上下文清单。
- [x] 激活任务，创建集成分支并发起不自动提交的 merge。
- [x] 存储子代理：独占 internal/storage/**，处理 0028–0032、注册表/恢复校验/测试，验证旧库升级。
- [x] 后端子代理：独占 internal/control/**，解决备注与别名，并审查关联业务契约和添加必要回归。
- [x] 前端子代理：独占 web/**，解决凭据详情、API 和 locale，验证 lint/format/build。
- [x] 主会话：处理 .env.example；审查自动合并的 gateway/state/requestlog、依赖、CPA 与 CI。
- [x] 全部实现后检查冲突与变更；受平台 agent thread limit 限制，复用前端代理执行 trellis-check 全范围职责，重点独立审查其未实施的后端与存储。
- [x] 完整门禁已逐项执行；64包通过、8无测试/跳过、5包存在23个本轮基线可复现失败，不能表述全绿，详见 research/verification.md。
- [x] CPA 独立 module：go mod tidy -diff、go vet ./... 均通过。
- [x] 新失败修复；按顶层及子测试比较新增失败集合为空，环境错误逐用例核对，详见 research/baseline.md。
- [x] 更新迁移规范与验证/升级说明，提交本地 merge 并将 main 快进，验证 ancestry 和用户原有文件。
- [x] 移交 trellis-finish-work 归档任务并记录工作日志。

合并提交：`b2cbb156b347495d822a607590f4633c153de4ae`。本地main已快进，两个固定输入的祖先检查均为0，上游待合并数为0。提交后只剩两项用户原有未跟踪目录。
