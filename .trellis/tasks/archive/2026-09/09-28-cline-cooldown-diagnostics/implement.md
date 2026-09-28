# 实施计划

## 阶段门槛

- [x] 已完成只读线上证据核对。
- [x] 用户确认两项一起修。
- [x] PRD 收敛，design.md 与本计划就绪。
- [x] 用户审阅最新规划摘要并回复“开始吧”；task.py start 已激活，分支 fix/cline-cooldown-diagnostics。

## 执行顺序与责任

1. trellis-implement：用量窗口修复。
   - 负责 internal/requestlog/group_model_usage.go 及测试、internal/control/group_model_usage.go 及测试。
   - 同步两套模型用量 DTO/projector/组件和对应三语言文案。
   - 先补“09:43–09:50 的 112 请求 15,662,497 tokens 被排除”合成回归，再实现批量边界补齐与 data_complete。
   - 验证同小时/整点/跨日/当前小时/多模型、无用量冷却项、删除凭据过滤、截断前行数以及 retention 过期。
2. 同一或后续 trellis-implement：测试诊断。
   - 负责 internal/control/credential_probe.go、credential_probe_test.go、classic_api_contract_test.go 与其他受影响契约测试。
   - 同步两套 credential-actions/credentials 资源、DTO、CredentialTestDialog 与三语言文案。
   - 新增 upstream_status_code、error_summary、retry_after_seconds，既有分类/恢复合同不变。
   - 测试已知秘密与异常内容脱敏、重试头优先级/日期、无状态写入。
3. 主会话更新 backend/database-guidelines.md，写明批量边界合并和完整性语义，保留旧任务历史文档作为决策记录。
4. trellis-check：核对全部 AC、跨层字段一致性及最小必要修复。共享文件顺序修改，派发时明确不可回滚其他人的改动。

两项不拆独立子任务：共同修改控制面契约测试、DTO、资源及翻译文件，统一门禁更清楚。可在单个实现代理中顺序处理，禁止并行争写共享文件。

## 验证命令

- 定向：go test -count=1 ./internal/requestlog ./internal/control
- 最终：make check（若 shell 无 make，按 Makefile 的等价步骤执行并记录差异）。
- 不访问真实上游，不写生产库；使用内存 SQLite 和 fake executor。
- 必要时两套界面的本地视觉检查仅验证字段换行/不完整提示，不把 E2E 框架引入项目。

## 检查要点

- 小时数据和请求明细是否在同一 DB 快照，边界是否互斥。
- 小时桶在首小时内耗尽时不能丢行、不能取前周期用量。
- data_complete 不能把缺失明细的 0 包装成完整结果。
- Query 数量上限必须测试，禁止按凭据逐个请求数据库。
- success_count 使用 status，不用 HTTP 200；所有 token 分项和整数上限沿用现有契约。
- 诊断字段来自上游响应，不能使用管理 API 状态冒充；没有明确 retry-after 时为 null，正文提示保留在摘要。
- 脱敏先于截断，前端不渲染 HTML；无真实凭据进入日志、测试或文档。
- classic/modern projector 白名单、三语言、契约测试同时更新。

## 回滚与交付

- 不修改 .playwright-mcp/ 原有目录，不自动提交无关文件。
- 无迁移。只修改本地代码；线上冷却、配置、容器和数据保持原状。
- 本地质量通过后汇报修复和测试结果；部署如被请求再检查镜像、备份与版本，发布/部署需明确授权。

## 完成状态

- [x] 两项产品实现、两套前端和三语言同步。
- [x] 独立检查完成，补充字段契约、跨日整点与完整缓存 token 回归。
- [x] 本次相关测试通过，完整门禁已执行并记录 Windows 环境限制。
- [x] 数据库和错误诊断 spec 已更新。
- 验证明细：verification.md；本地交付不包含线上部署。
