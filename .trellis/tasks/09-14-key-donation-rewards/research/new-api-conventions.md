# new-api 子任务的仓库边界与验证约定

本文件给托管在 gpt-load Trellis 目录下的 new-api 子任务提供上下文。目标代码位于 `D:/code/Claude code program/new-api`；实施前仍需读取该仓库的 `AGENTS.md`，前端还需读取 `web/AGENTS.md` 和 `.agents/skills/shadcn-ui/SKILL.md`。本摘要不替代原规则。

## 后端

- Go/Gin/GORM，业务分别位于 router、middleware、controller、model、service；用户鉴权沿用现有 token/session 与 authz，不能根据前端用户 ID 发奖。
- JSON 编解码使用 `common.Marshal/Unmarshal` 等包装，账务金额使用现有严格额度换算与上界校验，不裸 float→int 转换。
- 奖励唯一记录与 `User.Quota` 增量在同一事务；遵守现有缓存/预扣语义，不通过覆盖余额修复重试问题。表结构需要兼容 SQLite、MySQL >= 5.7.8、PostgreSQL >= 9.6。
- 数据库行为改动须用真实三数据库验证，新建、存量升级、重复迁移和唯一约束/并发事务都要覆盖；记录版本、命令、结果。无法执行时写明阻塞，不能称数据库兼容性已验证。
- 后端测试遵守 new-api 的 testify require/assert 约定，围绕契约集中覆盖，不按每个调用层机械复制测试。gpt-load 的“不用 testify”规则不能误套到这里。
- 涉及认证/凭据流程实施前读取适用 OWASP 指南，记录实际控制与验证；本次只规划，不宣称完成安全认证审计。
- 不能因新增捐献功能修改无关项目身份、版权或品牌。

## 前端

- React 19 + TypeScript + TanStack Router/Query + Zustand + Base UI，包管理使用 Bun。gpt-load 的 Vue、pnpm 和不写前端测试规则不适用于这里。
- 顶部导航从 `web/src/hooks/use-top-nav-links.ts` 和 HeaderNavModules 产生，公共页、登录后页与移动端使用场景都需验证。
- i18n 沿用现有英文源文案 key 与 flat locale 文件；风险小字也必须翻译，避免只更新中文或新增另一套语言键规范。
- UI 先检索共享组件，复用 `@/components/data-table`、dialog、empty/loading/error state、现有选择器与表单组件。新 feature 承载业务，但不重新实现已有通用交互。
- 沿用统一 `api` 客户端和最终业务 success 校验；跨用户切换时不能展示前一个用户的捐献记录或缓存 key。
- 新交互使用 Vitest/React Testing Library 验证，测试放模块 `__tests__/`。检查多行混合结果、分组加载失败、权限、永久奖励显示及提交前的小字可见性。

## 执行与校验

- 根 Go 包按影响运行 `go test ./model ./controller ./middleware ./service/...`，再按实际改动补齐必要包与迁移验证；不需要改独立 relaykit。
- 在 new-api/web 运行 `bun run typecheck`、`bun run lint`、受影响测试、`bun run build:check`；格式与版权检查按仓库现有脚本执行。
- 所有命令注明目标工作目录，new-api 测试不能被 gpt-load 的 `make check` 替代。跨端假上游联调由父任务统一验收。
- 本次观察到的六个 `clover-*.png` 是用户已有未跟踪文件，不能删除、重写或顺手提交。

## 续接时补充核对（2026-09-14）

- `common/crypto.go` 已有 `GenerateHMACWithKey(key []byte, data string)`，可复用算法包装并传入独立、持久的捐献秘密；不要使用绑定 `CryptoSecret` 的 `GenerateHMAC` 使历史去重随会话配置变化。
- `model/password_crypto.go` 展示“单独的内部表 + 唯一 slot + 冲突后读取已持久材料”的多副本秘密存储模式。捐献模块应使用独立材料，并在已有业务历史却丢失/损坏密钥时拒绝静默重新生成。
- `middleware/audit.go` 的自动管理审计只记录路由、路径参数、操作者和成功状态，不记录请求体；应继续使用标准审计边界，不能添加原始连接配置或 key 文本。
- `model/gorm_logger.go` 普通日志默认参数化，并收敛数据库驱动错误中的数据值；DEBUG 模式可输出参数。新的私密材料存取还需保证不被调试 SQL 或自行拼装的错误暴露。
- `model/topup.go:creditTopUpQuota` 支持事务内原子安全增额；`model/user_cache.go:syncCreditUserQuotaCache` 只在首次提交后同步增量，保留已预扣余额。恢复不能再次套用缓存增量或覆盖钱包余额。
- 前端使用现有英文源字符串 + flat locale 契约（本任务已确认）；web/AGENTS.md 中层级键名的旧示例不应引入另一套命名。
- intake 的真实 MySQL 测试发现 `OnConflict{DoNothing:true}` 在 clientFoundRows=true 时也可能返回 RowsAffected=1，不能据此判定首次创建或发奖资格。gpt-load 已改为 INSERT 冲突回滚后新读取核对摘要；new-api 的资源/批次/奖励幂等也应避免依赖该歧义，并在支持的 DSN 语义下验证。
- 本地 web 起初缺少 Node 24.10.0 和 node_modules；已安装该项目版本，按 `bun install --frozen-lockfile` 补齐现有依赖，不更改锁文件或全局 Node 默认版本。
- `web/src/lib/http-client.ts` 的 GET 去重包含 session SID；401 拦截器会刷新认证后用当前 token 重放请求。捐献输入/连接秘密写入需绑定发起时的用户与会话，在账号切换时中止旧请求并丢弃旧结果，不能把原 key 改由另一账号重发；可结合受会话约束的预刷新与 skipAuthRefresh/AbortSignal。
- `web/scripts/format-with-protected-headers.mjs --check` 实际会先快照所有文件、剥离版权头、执行 write，再恢复快照。它不是只读检查，不能与页面编辑或会生成 routeTree 的构建并行，否则会覆盖新写入。避免直接全仓 format 修复既有 98 个基线问题；本次触及文件需满足格式与版权要求。
- `web/rsbuild.config.ts` 的实际入口与别名均指向 `web/src`；`web/default` 的旧副本也会被格式脚本扫描，但不是当前构建入口，不应借本次功能批量修改。
