# 捐献接口实施安全控制

阅读日期：2026-09-14。适用于本次新增 API 凭据、捐献权限与奖励事务；不改变既有登录方式，不宣称完成全站安全合规认证。

## 已阅读的官方依据

- [OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/) 当前稳定版 5.0.0；已读取 [稳定版机器可读要求](https://github.com/OWASP/ASVS/releases/download/v5.0.0_release/OWASP_Application_Security_Verification_Standard_5.0.0_en.json) 的适用控制。
- [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)：认证主体、TLS、敏感操作与错误/审计边界。
- [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)：复用现有会话、服务端有效性、禁用/退出及安全传输。
- [CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)：现有防护优先、非简单请求头、同源限制与 Cookie 的局限。
- [REST Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html)：逐接口授权、状态机、请求边界、敏感参数不进 URL、禁止秘密回显及 no-store。

## 对应实现和验证要求

| 依据 | 本功能的控制 | 需要的行为验证 |
| --- | --- | --- |
| v5.0.0-8.2.1 / 8.2.2 / 8.2.3 / 8.3.1 | new-api 复用 UserAuth/AdminAuth 和 authz；所有权、收款人、组与金额由后端决定；gpt-load 使用独立集成身份 | 匿名、错误凭据、普通用户调用管理接口、跨用户批次/详情、伪造收款人/组/额度均拒绝 |
| v5.0.0-8.3.2 | 受理时检查资格；发奖事务重新检查账号状态，禁用时暂停 | 已受理后禁用不发奖，恢复后按原资源与奖励标识续办 |
| v5.0.0-3.5.1 | 复用仅接受 Authorization 的 dashboard 鉴权，新增变更接口只接受 JSON；不新增 Cookie-only 认证 | 缺 Authorization / 仅 Cookie 的请求不能执行捐献或配置操作 |
| v5.0.0-1.2.4 / 1.4.2 / 15.4.2 | GORM 参数化；奖励金额和钱包上界；数据库唯一约束、条件更新和原子事务 | 真实三数据库并发争用、重试、余额溢出、事务中断及唯一约束 |
| v5.0.0-13.3.1 / 14.3.3 / 16.2.5 | 运行时秘密配置与稳定指纹密钥；加密暂存；输入仅浏览器内存；接口和日志脱敏 | 集成 token、完整 key 不进入响应、普通日志、审计请求体或浏览器持久存储；连接 URL 不含凭据且不跟随不可信重定向 |
| REST workflow guidance | 只有确认通过且成功新增的可信回执能转入奖励；状态与幂等身份持久保存 | 乱序/重复回执、丢响应、重启、existing/invalid/inconclusive 均不误奖；已领奖资源删除后不再奖 |

new-api 现有 `middleware/auth.go` 的 `classifyDashboardCredential` 仅从 Authorization 解析 dashboard/PAT，随后验证登录会话和用户状态。复用此边界，另需核对新增配置/记录的自动管理审计不会采集秘密。实际测试结果在对应子任务 handoff 与父任务验收记录中填写；本文件不是通过证明。
