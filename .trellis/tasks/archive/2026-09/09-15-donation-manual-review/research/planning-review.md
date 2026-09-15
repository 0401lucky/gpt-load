# 最终规划复核

- 日期：2026-09-15。
- 范围：PRD、技术设计、执行计划和子代理上下文；不包含产品实现或功能测试。

## 已收敛决定

用户已同意创建任务、开启跳过模型测试时使用人工审核，以及真实调用首版仅管理员审核时使用。完整审阅稿同时明确逐项通过/拒绝、旧耗尽记录显式转审、原 7 天暂存期限、单次文字测试和兼容恢复。

## 独立研究与修订

接收端和调用方分别按实际源码复核。最终设计审阅确认两个具体缺口，均已修订：

1. **legacy auto 版本兼容**：旧接收端不返回 item_revision，合法的多个状态都为 0。已明确该分支沿原状态规则推进；观察过正版本或人工事实后才禁止旧/0覆盖并执行等版本一致性。
2. **捐献人读取拒绝原因**：已增加本人 batch/item 的安全 review_note 投影，仅来自已 applied 的最终 reject 动作；不把自由文本装进 reason_code，不开放管理测试详情。

另已明确现有 writeMu/snapshot.Revision 不是跨进程全局配置锁，未将本任务扩展为全站锁重构；stream/nonstream 均复用普通聊天执行器。研究详见 gpt-load-design-review.md 和 new-api-design-review.md。

## 文档检查

- PRD 已按 Goal/Background/Requirements/Acceptance Criteria/Out of Scope/Known Limits/Planning Status 收敛，保留原证据与需求编号；无未解决问题或占位符。
- design.md 和 implement.md 已生成，定义系统责任、接口/状态、恢复/兼容、验证顺序及回退边界。
- 过长研究原文保留为按需参考，另以 implementation-context.md 提供可完整注入摘要，没有调整全局配置。
- task.py validate 已通过，两份 jsonl 含真实 spec/research 引用；文档链接、UTF-8、JSONL 及 git diff --check 已检查。
- 仅任务目录产生新增文件；两个项目的已有未跟踪文件保持原状。

## 阶段边界

最终规划摘要已交用户；用户随后指定由 DeepSeek 实施，完成后先交 Codex 审查、不归档。当前交接会话保持 planning，DeepSeek 根据 deepseek-handoff.md 激活现有任务，完成后保留 in_progress 等待复核。功能测试、真实数据库升级及双服务联调均尚未运行；本轮没有生产写入、上游测试、部署或发布。
