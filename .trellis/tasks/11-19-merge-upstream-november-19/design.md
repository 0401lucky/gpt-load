# 兼容合并设计

## 固定输入与变更边界

从 fork `72b24ef8765688577965a668ca1c6bdfcd5564c2` 创建集成分支 `merge/upstream-20261119`，以 `git merge --no-ff --no-commit e4a6ddec49327b2245c3bf5dc6479277d6821180` 接入全部 50 个上游提交。主会话拥有分支、Git 索引、提交和最终 main 快进；实现代理只修改明确归属的文件，不提交、不推送、不回滚其他代理变更。

当前行为缺口是上游 50 个提交尚未进入 fork。变更仅覆盖固定上游差异及必需的 fork 兼容修复。预合并结果、已有定制和风险点见 `research/recon.md`。

## 数据库合同

上游 `0031_model_price_reference` 映射为 fork `0034_model_price_reference`。保持已发布 `0001`–`0033` 生产迁移 blob 和注册顺序，保留 `ValidateCurrent0027` 及 fork 原有 0031 客户端 IP / 0032 优先级 / 0033 请求体记录归属。

只调整新迁移文件名、ID、导出函数、注册及直接调用者；保留上游内部 `modelPrice0031`、SQLite 临时表、索引和外键 DDL 名称。新迁移测试按语义 ID 查找边界，不能沿用上游硬编码下标。完整链断言更新为 34 项。

扩展现有真实临时 SQLite 升级测试，覆盖 33→34 及较早 fork 前缀，核验旧账本严格前缀、已有价格/业务记录/身份数据和请求正文保留、价格关联列默认 NULL、外键与索引可用，以及关闭重开/重复执行。MySQL/PostgreSQL 保持现有方言与中断恢复校验；无 opt-in DSN 时记录未实测，不访问生产库。

## 连接测试与前端合同

保持 `upstream_status_code`、`error_summary`、`retry_after_seconds` 的 nullable 字段语义，接纳上游可省略的 `error_detail`；保留 outcome/reason/protocol/restore proof 合同和批量测试所有权守卫。

诊断投影在执行阶段已知秘密仍在作用域时完成，不能退化为仅遮盖 API key：凭据 JSON 各字段、认证请求头、代理账号密码、编码变体仍须遮盖。上游详情的安全摘要和 HTTP/provider code 可保留，不返回原始响应体、响应头、transport/preparation 内部错误或堆栈。复用现有脱敏边界，补充两个字段集共存的回归。

classic/modern 通过现有 DTO、资源 projector 和 view 类型传播字段；界面保留 structured 诊断，并能展示上游错误详情与 reason fallback。三语言同步；不新增前端测试框架。处理 Prettier 3.10 对 fork 新增文件的格式要求时仅改必要格式。

## 自动合并区域复核

- 网关上下文恢复、WebSocket 和 advisor 定价更新与 fork 冷却/可信周期合同共存；调度等待到期不重置额度窗口。
- 捐献服务 DI、运行时恢复、鉴权、凭据备注和参数规则指纹继续保留；检查上游对参数规则新增方法是否与指纹边界兼容。
- 保留 fork main GHCR workflow 和 Compose 仓库目标；接纳上游 CI 分片/并行、音频和新订阅目录及 Docker runtime 更新。
- 接纳固定目标的 Bifrost 1.15.2、CPA v8.0.77、前端精确锁文件；不额外升级依赖。

## 执行协作与验证

实施阶段由独立 Trellis 实现代理分别拥有 storage 兼容、control/locale 诊断兼容、frontend 兼容；同一工作区共享改动，Git 操作和共享验证由主会话协调。最终 Trellis 检查代理审阅完整范围并修复机械性缺陷。

本机已有 Go 1.27.3、Node v25.6.0、corepack pnpm 11.17.0，未发现 make。可在 PowerShell 执行 Makefile 的逐项等价门禁。遵循上游新门禁次序：Go 检查与前端静态检查可独立并行，Vite 会清空嵌入 dist，因此必须等 Go 编译/测试结束后再运行 Vite build 和最终 Go build。依赖安装使用精确锁文件。

直接相关测试失败先修复；怀疑基线/平台问题时在仓库外创建固定起点的基线 worktree，以同样工具链复现具体测试，不能直接引用旧会话的 Windows 失败结论。独立 CPA module 额外 tidy/vet 及相关执行回归，无本地 race。

## 回退与发布边界

未提交 merge 可通过 Git 的 merge abort 退回起点，前提是先保存本任务证据且保留原有未跟踪文件。验证通过后创建双亲 merge，再将本地 main 快进。生产升级或回退不在本次范围；不执行真实数据库迁移或远端发布。
