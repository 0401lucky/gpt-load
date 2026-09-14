# gpt-load 捐献接收与校验接口

## Goal

提供受限分组目录、多行 key 暂存校验、幂等接收、逐项回执和长期来源记录。

父任务：[密钥捐献与社区额度奖励](../09-14-key-donation-rewards/prd.md)。本子任务仅负责 `D:/code/Claude code program/gpt-load` 后端；2026-09-14 续接获得明确本地实施授权，已启动为 in_progress，正在实施与验证。

## Requirements

- 覆盖父 R2、R7–R10、R12–R14、R16；准确接口遵循父任务 [集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。
- 专用集成鉴权提供分组目录，继承已有渠道/验证配置，不新增平台参数。
- 加密暂存后逐条探测、去重和接收，成功回执稳定关联凭据 ID；无效或暂不确定项不报成功。
- 已有资源返回 existing；并发、跨组及重复请求不能重复接收，同一明细可恢复原结果。
- 删除组/凭据或压缩操作记录不删除接收历史，不把已接收旧 key 当作新资源。

## Acceptance Criteria

- [x] 父 AC2/AC11/AC13：继承配置且仅使用被捐 key 探测，校验前不参与正常调度。
- [x] 父 AC4/AC5/AC12/AC18：重复、并发、提交后恢复及删除后再提交有稳定回执，没有第二次资格。
- [x] 父 AC16/AC17：混合多行逐条处理，失败不影响合格项，明细 ID 不混淆。
- [x] 集成凭据不能读明文、调用普通管理功能或其他来源记录；未配置时模块不可用。
- [x] 原管理导入/路由契约兼容，相关测试和 make check 通过；未验证项明确记录。

验收证据：独立复核与实际 MySQL/PostgreSQL 见 research/database-verification.md；完整 make check 在当前代码的 LF Linux worktree 通过，日志 research/intake-make-check-linux.log，原 Windows 检出/工具限制见父 progress.md。

## Scope and Dependencies

- 前置为父需求与契约定稿；可以独立使用测试调用方验证。
- 不负责 new-api 用户、余额或页面，不创建 gpt-load 独立捐献前端。
- 本任务完成后由 new-api 后端任务联调，父任务负责跨端最终验收。
