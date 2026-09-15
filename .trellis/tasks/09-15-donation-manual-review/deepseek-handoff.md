Active task: .trellis/tasks/09-15-donation-manual-review

请接手并完成“捐献人工审核与真实调用测试”的本地实现和验证。已有完整规划，请按它直接实施，不要重新创建任务或重复开展需求讨论。完成后先交给 Codex 审查，暂不归档。

工作区包含两个独立仓库：
- gpt-load：D:/code/Claude code program/gpt-load
- new-api：D:/code/Claude code program/new-api

先读取两仓库各自的 AGENTS.md，以及 gpt-load 下本任务的：
1. prd.md
2. design.md（最终协议、字段及边界以它为准）
3. implement.md
4. research/implementation-context.md
5. research/new-api-ui-and-conventions.md
6. implement.jsonl / check.jsonl 中与你修改范围直接相关的规范。

需要详细源码依据时，再读 research/gpt-load-design-review.md 和 research/new-api-design-review.md。new-api 前端还必须遵循 web/AGENTS.md 和项目 shadcn-ui skill，不要把 gpt-load 的 Vue/无前端测试规则套到 React 项目。

从 gpt-load 工作目录激活已有任务：
python ./.trellis/scripts/task.py start .trellis/tasks/09-15-donation-manual-review

按你所在平台支持的方式实现，职责可以串行处理，不依赖 Codex 专属子代理。尊重已有改动，不回滚无关文件；保留 gpt-load 的 .playwright-mcp/ 和 new-api 已有 clover-*.png。手工修改优先使用 apply_patch，不做无关重构、格式化或依赖升级。

必须完成的行为：
1. 活动新增“跳过模型测试，改为人工审核”，默认关闭；旧活动、旧自动批次和旧客户端行为兼容。
2. 人工模式只加密暂存、等待审核，不能提前入调度池或发奖。管理员逐项通过/拒绝；通过并成功正式接收后仅发一次原定永久奖励，拒绝原因在捐献人本人的批次详情可见。
3. 真实调用仅管理员审核详情使用：选原组文本模型、输入提示词、流式/非流式查看回复并可停止。必须锁定这笔捐献的 key，不能用库存 key/其他组补位；不走普通 Relay，不扣站内钱包。
4. 测试结果与人工决定独立；测试成功不自动批准，失败不自动拒绝，也不强制测试成功才能人工通过。
5. 旧 retry_exhausted 项仍有暂存时可显式转审，保留原批次、原请求摘要、用户、奖励、原始模式和原 7 天截止。测试/转审不续期，过期不能通过。
6. 按设计完成动作幂等、并发审核、完整回执绑定、重启恢复与发奖双重条件。只存在本地 pending 意图或浏览器“成功”不能发奖。
7. 注意 legacy auto 的缺版本回执仍可正常推进；已经版本化或转人工的项不得被旧/0版本覆盖。自由拒绝原因放在安全 review_note，不塞入静态 reason_code。
8. 流式和非流式响应都必须防 key/token 外泄；尤其处理跨片段回显 key、断流、取消、迟到响应和跨账号/记录污染。不能直接透传原始上游 SSE/错误/认证头。

按 implement.md 完成最小充分验证，至少包含相关 Go 测试、gpt-load make check、new-api 前端测试/typecheck/变更范围 lint/构建、真实 SQLite/MySQL/PostgreSQL 的旧捐献数据升级及重复迁移，以及本地两服务真实进程联调。复用现有 fixtures，测试使用合成 key、本地上游和隔离数据库。skip 或 no tests to run 不能算通过。能修复的相关失败继续修复；确有外部阻塞时准确记录未验证部分，不能宣称通过。

不连接或修改生产，不用真实上游 key，不发布、不部署、不 commit、不 push。

交付给 Codex：
- 写入本任务 research/implementation-handoff.md：两仓库改动清单、各需求完成情况、测试命令/数据库实际版本/结果、日志路径、迁移与兼容注意事项、剩余问题、需要 Codex 重点检查的位置。
- 更新任务进度与已真实完成的验收项；未执行的检查保持未完成。
- 保留全部代码改动和审查证据，任务保持 in_progress，并注明“等待 Codex 审查”。
- 不执行 task.py archive、task.py finish 或会归档/清空任务的 finish-work，不标记 completed，不移动任务目录。
- 最后向我报告已完成内容、验证结果及交接文档路径，然后停止，等待 Codex 复核。
