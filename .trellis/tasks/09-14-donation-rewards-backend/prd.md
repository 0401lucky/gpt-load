# new-api 捐献管理与永久奖励后端

## Goal

实现活动配置、gpt-load 集成、批次恢复、全站 key 去重、永久奖励事务和管理查询。

父任务：[密钥捐献与社区额度奖励](../09-14-key-donation-rewards/prd.md)。目标为 `D:/code/Claude code program/new-api` 后端；任务文档托管在 gpt-load。2026-09-14 已按续接授权启动为 in_progress，承接已通过定向复核的 intake 接口。

## Requirements

- 覆盖父 R1、R3–R10、R12、R15–R17，遵守 [集成契约](../09-14-key-donation-rewards/research/integration-contract.md)。
- 管理员配置名称、真实组和固定奖励；用户/目标/奖励不能由捐献请求任意指定。
- 多行逐项接收与恢复，规范化 key 全站长期去重；存量 key 无新奖励、不修改旧来源。
- 首次校验通过且成功接收的 key 只入账一次固定永久额度，不设领奖上限、不进入签到临时桶、不追回。
- 持久关联用户、key 指纹/掩码、明细、远端凭据和奖励；失效、删除或日志清理后仍可追溯。

## Acceptance Criteria

- [x] 父 AC1/AC6/AC9/AC10：账号、管理权限及配置检查正确，用户只读本人信息，不泄露秘密。
- [x] 父 AC3/AC4/AC5/AC12/AC18：资源唯一性和奖励事务通过并发/重试/断点验证，已有 key 不奖。
- [x] 父 AC14/AC15/AC19：不同合格 key 不限领奖数量、奖励永久有效、后续失效不追扣。
- [x] 父 AC16/AC17：混合批次逐项结果准确，未确认请求按原标识恢复。
- [x] 真实 SQLite/MySQL/PostgreSQL 新建、升级、重复迁移与账务事务验证通过，记录命令/版本/结果。

验收证据：research/implementation-handoff.md、research/check-handoff.md 及父任务 research/cross-service-validation.md。页面表现由 donation-web 子任务与最终浏览器验收继续验证。

## Scope and Dependencies

- 契约冻结后可用假 gpt-load 开发；最终联调依赖 [donation-intake](../09-14-donation-intake/prd.md)。
- 提供前端所需 API，不修改 new-api/web 或 gpt-load 业务代码。
- 遵守目标仓库 AGENTS.md，不能误套 gpt-load 的测试和数据库约定。
