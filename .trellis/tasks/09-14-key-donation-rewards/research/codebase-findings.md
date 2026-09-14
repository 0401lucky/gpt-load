# 两个仓库的代码研究

研究日期：2026-09-14（最终规划：不追回奖励、加入主账号风险小字）。基线：gpt-load `57965b94`；new-api `bc223d567`。源码结论来自本地检查；另只读查阅 Google 官方公开文档，未连接线上实例，未调用真实上游或额度写接口。

下文 `gpt-load/...` 相对当前仓库，`new-api/...` 相对 `D:/code/Claude code program/new-api`。

## 1. gpt-load 已有能力与缺口

| 事实 | 代码证据 | 对捐献功能的影响 |
| --- | --- | --- |
| 分组保存渠道、连接类型、验证协议/模型及凭据列表 | `gpt-load/internal/storage/models/group.go:14` | 活动可引用已有分组，继承上游与验证配置；不需要再建一套渠道体系 |
| API key 与订阅凭证是不同连接类型 | `gpt-load/internal/storage/models/group.go:51`；`gpt-load/internal/control/credentials.go:202` | 不能把少量 OAuth 凭证当作普通 key 文本导入；首版范围需明确 |
| 普通 key 导入进行格式规范化、渠道字段校验和指纹计算 | `gpt-load/internal/control/group_write.go:255` | 此处的 ValidateCredential 不是调用上游验证余额或可用性 |
| 持久化 key 会加密，现有查重和唯一约束以分组为界 | `gpt-load/internal/control/group_write.go:403`；`gpt-load/internal/storage/models/group.go:70` | 可复用加密入库，但“不同组不能重复领奖”需要独立的捐献去重记录 |
| API key 导入已有 Idempotency-Key 操作账本，成功结果可重放 | `gpt-load/internal/control/group_idempotency.go:212`；`gpt-load/internal/control/server.go:781` | 应复用事务和操作恢复经验；不要误判现有导入完全没有幂等性 |
| 当前幂等摘要使用管理端认证作用域 | `gpt-load/internal/control/idempotency_digest.go:20` | 捐献用户/集成调用需要自己的作用域与长期奖励身份，不能直接复用管理身份 |
| 实际调用探测有 passed / failed / inconclusive 三类结果 | `gpt-load/internal/control/credential_probe.go:30`、`:130`、`:265`、`:271` | 超时、限流不宜直接算无效 key；通过只证明本次指定调用成功，不证明剩余余额或长期价值 |
| 公开的现有探测方法针对已入库凭据 | `gpt-load/internal/control/credential_probe.go:349` | 捐献要增加暂存/隔离校验编排，避免未验 key 先成为正常服务流量的候选 |
| 入库后还要校验并更新运行时注册表 | `gpt-load/internal/control/credentials.go:202`；`gpt-load/internal/control/group_idempotency.go:212` | 直接写数据库不等于 key 已参与调度，必须通过现有控制面写入流程 |
| 控制面管理鉴权与普通 AccessKey 权限分离 | `gpt-load/internal/control/auth.go:43`；`gpt-load/internal/control/http_routes.go:33` | 当前没有社区用户会话；不能给捐献人发管理 token 或放开现有导入接口 |
| Vue 页面路由与 Go SPA 路由共享清单 | `gpt-load/web/src/app/router.ts:35`；`gpt-load/internal/webui/page_routes.json:1` | 若在 gpt-load 加独立页面，要处理路由、布局、鉴权和直接刷新访问 |

尚无捐献活动、捐献人与 key 归属、奖励规则或奖励流水的现成业务实体。本次在控制面、页面路由和相关文档检索中未发现该功能。

## 2. new-api 已有能力与缺口

| 事实 | 代码证据 | 对捐献功能的影响 |
| --- | --- | --- |
| Linux DO OAuth 将稳定的社区用户 ID 写入 new-api 用户 | `new-api/oauth/linuxdo.go:162`、`:176`；`new-api/model/user.go:107` | 关联应使用服务端确认的 ID，而不是用户填写的昵称 |
| 已有按 Linux DO ID 精确反查用户的接口 | `new-api/controller/user.go:469`；`new-api/router/api-router.go:181` | 独立门户可以复用查询能力；接口在 AdminAuth 下，仅后端集成可调用 |
| 登录后接口从 Bearer dashboard token/PAT 验证用户与会话 | `new-api/middleware/auth.go:90`、`:197`；`new-api/router/api-router.go:121` | 站内页面可以直接复用身份；不能假设另一个域的 gpt-load 自动共享登录 cookie |
| 前端登录跳转限制为同源目标 | `new-api/web/src/features/auth/lib/auth-redirect.ts:46` | 独立门户不能仅拼一个外部 return_url 就获得可信用户身份 |
| 管理接口支持永久额度增减/覆盖 | `new-api/router/api-router.go:187`；`new-api/controller/user.go:1241`；`new-api/controller/user_quota.go:14` | 可以复用额度边界、审计与缓存同步逻辑，但接口没有捐献业务单号 |
| 永久额度调整在事务中检查权限、范围，提交后按差额更新缓存 | `new-api/model/user_quota_adjustment.go:26` | 直接 SQL 加 quota 会绕过这些约束；奖励应由 new-api 自己结算 |
| 已有给福利站使用的当日限时额度接口 | `new-api/controller/user.go:501`；`new-api/router/api-router.go:188`；`new-api/model/checkin.go:233` | 当前语义是签到时区次日零点过期，并非任意 N 天有效期；同日永久签到记录还会发生冲突 |
| 当日限时额度每次成功调用都会继续累加 | `new-api/model/checkin.go:267`、`:319` | handler 注释中的“幂等叠加”不代表按业务请求去重；自动重试同一奖励会重复发放 |
| 充值订单已有唯一单号、事务状态检查与入账模式 | `new-api/model/topup.go:21`、`:239` | 可借鉴业务唯一性和事务边界；捐献奖励无需伪装成真实支付订单 |
| 某些业务失败以 HTTP 200 + success:false 返回 | `new-api/controller/user_by_linuxdo_test.go:100` | 跨服务客户端必须同时判断 HTTP 与业务响应，不能只看 200 |

本次检索未发现可直接作为 gpt-load 登录提供方使用的通用 OAuth 授权服务端接口。new-api 现有 Linux DO OAuth 是登录消费端，不能据此宣称已支持对外 SSO。

## 3. 可以确定的技术结论

1. 不必让 gpt-load 直连 new-api 数据库。new-api 自己确认用户并结算奖励，通过后端 API 调用 gpt-load 接收资源，保留双方的数据边界。
2. 按已确认的站内路线，分组、上游协议和密钥由 gpt-load 负责；活动配置、用户、捐献来源与奖励账目由 new-api 负责。new-api 本地结算无需 gpt-load 持有其管理员凭据。
3. 活动配置已收敛为“名称 + 目标分组 + 每 key 固定永久额度 + 开关”；校验依组配置执行，同 key 全站只奖励一次，不设领奖数量上限。
4. 现有 key 导入的幂等性、同组资源去重、跨活动奖励去重是三个不同问题，必须分别覆盖。
5. 自动发奖需要 new-api 内部的奖励唯一标识和额度入账处于同一事务；跨服务接收结果丢失时按原标识恢复，不能仅凭数量汇总决定再次发奖。
6. 一次 key 调用成功只证明该次请求可用；当前没有研究或承诺凭普通 key 查询余额、上游账号身份或免费层级。奖励模型仍需由产品规则决定。

## 4. 当前不应假定的事实

- 部署中的两个版本、域名与网络可达性、分组渠道及验证模型是否与本地一致。
- Gemini AI Studio 普通 key 是否能够提供额外账号/额度证明，不能从现有探测代码推定；这些额外查询当前不是用户要求。
- 线上哪些登录方式和账号状态已获允许，由 new-api 现有配置决定；本功能沿用既有资格，不额外引入登录来源门槛。
- key 的未来可用性无法保证；用户已决定奖励不追回，并要求提交前用小字提示主账号、封号和封 key 风险。
- 线上目标分组的真实类型、验证配置与启用状态；本地不能替代实际可选项确认。

目前无需服务器凭据即可推进产品讨论。首批资源已经确定，方案定稿后再按需要核对既有 Gemini 分组的脱敏配置或经授权进行只读部署检查。

## 5. 后续实施必须遵守的仓库约定

- gpt-load：见 `.trellis/spec/backend/directory-structure.md`、`database-guidelines.md`、`logging-guidelines.md` 与前端 specs。控制面不能绕过运行时更新；新增路由必须通过 `httproute` 注册。用户鉴权若独立于现有 control/data 平面，需显式设计 Owner/Auth 契约。
- new-api：遵守其 `AGENTS.md`，数据库行为变更需要真实 SQLite/MySQL/PostgreSQL 验证；认证改动实施前读取适用 OWASP 指南并确定针对性验证。本轮未进行安全合规审计，不作合规结论。
- 本轮只有任务文档检查，不运行与文档无关的应用测试，也不把未运行的验证标记为通过。

## 6. v1：顶部导航、分组选择与逐 key 关联

已确认产品选择：捐献入口在 new-api 顶部导航；活动配置也在 new-api；管理员需看到用户与每个 key 的对应关系。无需继续评估独立 gpt-load 门户或跨站用户登录。

| 事实 | 代码证据 | 设计影响 |
| --- | --- | --- |
| 顶部链接集中从站点状态和导航配置生成 | `new-api/web/src/hooks/use-top-nav-links.ts:41`；`new-api/web/src/lib/nav-modules.ts:25` | 新增捐献项应贯穿状态、解析与设置，兼容旧配置；无需复制一套导航 |
| 公共页和登录后顶栏都消费上述链接 | `new-api/web/src/components/layout/components/public-header.tsx:68`；`new-api/web/src/components/layout/components/app-header.tsx:98` | 需要同时检查这些使用场景，登录后顶栏还存在窄屏隐藏逻辑，不能只验证桌面首页 |
| 已有管理员顶部导航配置页 | `new-api/web/src/features/system-settings/maintenance/header-navigation-section.tsx:137`；`new-api/web/src/features/system-settings/maintenance/config.ts:33` | 内建捐献项需和配置、启用状态及登录要求衔接 |
| gpt-load 已有完整分组选项列表接口 | `gpt-load/internal/control/http_routes.go:186`；`gpt-load/internal/control/group_options.go:16`、`:36` | new-api 后端可复用查询能力；只投影管理选择所需字段，不原样透传 params |
| 分组选项包含 enabled、连接类型和模型；collection 的 unavailable 可能只是缺少 key | `gpt-load/internal/control/group_options.go:16`；`gpt-load/internal/control/group_collection.go:28` | 捐献目的常是填充空组，不能用“当前有可用 key”作为可选择目标的条件 |
| 现有导入结果只有 group_id 与数量汇总，没有逐项 credential_id | `gpt-load/internal/control/credentials.go:27` | R12 需要捐献专用的稳定逐项回执与持久关联；不能从汇总结果推测哪条 key 属于谁 |
| 已有日志表格、筛选与 all/self 查询模式 | `new-api/web/src/features/usage-logs/audit/components/audit-log-viewer.tsx:28`；`new-api/web/src/features/usage-logs/audit/api.ts:55`；`new-api/web/src/components/data-table/README.md:1` | 复用表格/分页/详情交互，捐献业务实体仍独立保存 |
| 普通 Log 可位于独立日志库并按时间清理 | `new-api/model/main.go:230`；`new-api/model/log.go:829` | 用户→key→奖励关联和发奖去重依据保存在主库业务记录，不能仅写 Log.Content |

需要新增的最小跨端关联是“new-api 捐献明细 ID ↔ gpt-load 接收结果 ↔ 凭据 ID”。用户/组名称变更或凭据删除时保留历史快照和稳定标识；记录中失败、重复与成功接收必须区分。

## 7. v2：分组决定资源类型与验证方式

用户明确要求：在 new-api 自定义活动名称（例如 `gemini aistudio free key`），选择已配好的 gpt-load 分组即可完成资源配置。当前实际需要 Gemini AI Studio 普通 key，不需要在捐献模块再单独配置平台。

- `gpt-load/internal/channel/modules/gemini.go:9`：已有 Gemini 渠道，连接类型为 API key，凭据字段为普通 `api_key`，并声明了可用探测协议。该能力应由 gpt-load 使用，new-api 无需新增 Gemini 调用适配器。
- `gpt-load/internal/control/validation.go:291`：验证目标从 `GroupView` 的渠道、解析目标、验证模型/协议生成；验证模型为空时沿用组内首个模型。没有有效目标则返回不可探测。
- `gpt-load/internal/control/group_settings.go:393`：分组层已有验证模型及回退解析；`availableValidationProtocols` 从渠道声明读取协议能力，不另维护平台清单。

设计据此收敛为「活动名称 + 目标组引用 + 奖励配置 + 开关」。渠道/协议等可作为选择结果的只读说明，不能成为另一组必填参数。活动名称中的 free 是站长给出的展示说明，代码检索不支持将其当作已验证计费层级。

Gemini 是首批验收用例；分组驱动是实现边界。无需为了接收当前资源而先建设多平台余额查询、订阅凭证接收或重复渠道配置。

## 8. v3：防重复领奖与后续失效

用户已确认每个有效、未重复且成功接收的 key 发放活动固定额度，强调重复提交不能薅奖励，后续 key 失效仍需保留记录。

| 事实 | 证据 | 设计影响 |
| --- | --- | --- |
| gpt-load 对凭据生成稳定 HMAC-SHA256 指纹，指纹子密钥与加密子密钥区分 | `gpt-load/internal/platform/encryption/encryption.go:21`、`:124` | 可以复用这一设计；长期去重依赖稳定指纹密钥，不能比较随机密文或共享两端加密主密钥 |
| 当前凭据唯一索引以 group_id 和 fingerprint 为组合 | `gpt-load/internal/storage/models/group.go:60` | 现有组内约束不能覆盖不同组的重复奖励，需要站点业务级唯一身份 |
| 单条凭据删除直接删除存储行，组删除也会移除组及关联资源 | `gpt-load/internal/control/credential_mutations.go:227`、`:250`；`gpt-load/internal/control/group_delete.go:93`；`gpt-load/internal/storage/models/group.go:27` | 防重复与捐献来源必须独立保留，不能随活动/组/key 级联删除 |
| 控制面操作结果具有压缩状态，压缩后重放返回结果过期 | `gpt-load/internal/storage/models/control_operation.go:5`；`gpt-load/internal/control/idempotency_operation.go:261` | 请求级恢复账本不能代替长期的资源资格和奖励依据 |

Google 官方 [Gemini API Rate limits](https://ai.google.dev/gemini-api/docs/rate-limits)（2026-09-14 读取，页面标注 Last updated 2026-09-02 UTC）明确说明：

> Rate limits are applied per project, not per API key.

因此同一个 Google 项目下的不同 API key 可能共享 RPM/TPM/RPD 配额。此事实不改变按分组配置的架构，但意味着“不同 key”不必然等于“独立新增供给”。现有 `CredentialProbeResponse`（`gpt-load/internal/control/credential_probe.go:50`）没有提供可信 Google 项目身份，不能据此承诺按项目防重。

v4 产品决定：用户明确不设置领奖数量上限，预计参与人数较少。对应建议已从待办移除，首版按 key 去重和一次性固定奖励执行，不增加每日计数/累计奖励配额配置，也不额外收集 Google 管理凭证。项目配额共享作为已说明的技术边界保留，不再据此重复请求设置上限。

## 9. v5：奖励进入永久余额

用户已明确奖励必须发放永久额度，不设过期时间。此前研究到的当日临时额度接口仅保留为现状证据，本任务不采用该路径。

- `new-api/model/user_quota_adjustment.go:26`：管理调整更新 `User.Quota` 并在提交后按差额同步缓存，但自身拥有事务；不能直接拼接为“调整完成后另写奖励记录”的两步操作。
- `new-api/model/topup.go:109`：已有接受 `*gorm.DB` 事务句柄的安全永久增额逻辑，通过带余额边界条件的增量 UPDATE 防止溢出及并发越界，可作为捐献奖励事务的复用/兼容扩展参考。
- `new-api/model/user_cache.go:166`：已有永久增额后的缓存同步入口，接入时需保留原有预扣与派生状态语义。

实现必须使捐献唯一奖励记录和永久余额增量处于同一事务；不制造现金充值订单，不写签到临时桶，不新增奖励有效期选择器。

## 10. v6：多行粘贴与现有导入的差异

用户确认首版支持每行一个 key 的多行粘贴，并沿用前一轮提出的逐条校验、入库、奖励和失败原因展示。

- `gpt-load/internal/control/group_write.go:255`：已有按行输入的规范化、忽略空行和首尾空白处理，并按指纹去除批次内重复。但遇到单项格式/渠道校验错误时会返回整次错误，公开捐献应采用独立逐项处理契约。
- `gpt-load/internal/control/group_write.go:365`：现有普通凭据输入按行拆分，可复用输入语义。
- `gpt-load/internal/control/credential_import_limit_test.go:13`：现有管理导入测试覆盖多行空白、5000 个非空项、结果重放及不同请求 ID 的重复导入；这些是已读测试证据，本轮未执行。管理导入能力不能直接等同于“对 5000 个真实 key 同步探测完成”。

批次、逐项记录和奖励事务应分层：一个条目失败不能回滚其他已成功条目；接口丢响应或进程重启时按稳定明细 ID 恢复。已在库中的资源按既定“首次新增、非重复才奖励”条件返回已有结果，不因为它此前由管理员导入就重新计奖。

## 11. 最终决定与接口包装核对

用户明确捐献是善意，不应因后续失效追回奖励；同时要求页面用小字提醒不要使用主账号，存在账号受限/封禁或 key 失效风险。准确文案统一记录在 PRD R18，不额外设计免责确认弹窗。

`gpt-load/internal/platform/response/response.go:14` 定义的是 `code/message/data` 包装（成功 code 为 0，错误 code 为字符串），与 new-api 的 `success/message/data` 不同。跨端客户端必须按各自契约解码，这一点已纳入 [集成契约](integration-contract.md)。

`gpt-load/internal/platform/httproute/registry.go:18`、`:28` 当前只声明 system/control/data/web 及对应鉴权。专用集成命名空间需要显式增加 Owner/Auth 和装配契约，并补已有路由边界测试；不能假定任意新前缀自动获得正确鉴权。
