# 实施与验证结果

## 实现完成

- requestlog 新增 GroupModelWindowUsage 精确窗口汇总，完整小时和首尾明细同一快照、互斥合并；边界小时去重后按最多 200 个区间分批查询。
- control 的模型用量输出 data_complete 和截至观测时刻的统计，保留现有行筛选/排序/500 行上限。
- 连接测试新增 upstream_status_code、error_summary、retry_after_seconds；已知凭据、header、proxy 先脱敏后投影，安全诊断同步进入控制日志。
- 两套界面、DTO/projector/手工父组件投影和三语言同步。
- 更新数据库窗口与错误诊断规范。无迁移，无生产操作，无真实上游探测。

## 审查与回归

独立 trellis-check 未发现需修复的产品问题，追加以下检查：

- classic_api_contract_test.go 登记用量响应/行/分页字段白名单，覆盖 data_complete。
- 精确整点跨日、明细过期但完整小时仍齐全、全部缓存 token 分项。

已通过的关键场景还包括：

- 09:43 起点后短时耗尽的 112 次请求 / 15,662,497 tokens。
- 同小时边界去重、当前小时新模型、窗口不完整、候选过滤、500 行截断。
- 批量查询数量和同一读取快照；非法/不安全时间范围。
- 429 正文、Retry-After 秒数/日期、内部失败安全回退、非标准凭据/代理认证脱敏。
- 原有测试不改变 disabled/cooldown/blacklisted 状态及显式恢复证明合同。

## 已执行门禁

| 项目 | 结果 |
|---|---|
| pnpm frozen install | 通过 |
| pnpm run lint（含 modern 样式检查） | 通过 |
| pnpm run build（含两套前端类型检查） | 通过 |
| go vet ./... | 通过 |
| go build | 通过 |
| git diff --check | 通过 |
| 修改 Go 文件 gofmt | 通过 |
| go test -count=1 ./internal/control ./internal/requestlog（审查新增测试后复验） | 通过：control 12.833s，requestlog 6.729s |
| 全仓 Prettier --end-of-line auto | 通过；原 format 命令受工作区既有 CRLF 影响 |
| go mod tidy -diff | 原命令仅 go.sum 行尾差异；临时 LF mod/sum 副本通过，未改依赖文件 |
| 全量 go test -count=1 . ./internal/... | 已执行，以下既有 Windows 环境失败 |

本机没有 make，按 Makefile 执行等价步骤。不能声称原始 make check 全绿。未做浏览器视觉验证，UI 通过静态/类型/构建与 Go 契约检查。

## 范围外失败

以下相关源码与 HEAD 无差异；检查代理抽取代表测试单独复现，未修改无关模块：

- catalog：TestCacheReplaceErrorAndRollbackFailureRetainsPreviousBackupPath、TestCacheRollbackReplaceFailureKeepsRecoverablePreviousBackup。错误文案用 %q 转义 Windows 路径，测试按未转义路径匹配。
- catalog：TestCodexModelCatalogSnapshotDigest。JSON CRLF 影响摘要；归一 LF 后摘要精确匹配期望。
- 根包：TestSystemOutboundProxyProviderUsesEnvironmentAndLatestGlobalSnapshot。测试先设置 HTTPS_PROXY 再清空 https_proxy，Windows 环境变量大小写不敏感。
- webui：TestReleaseWorkflowJobsDownstreamOfSkippableGatesOverrideDefaultCondition（已单独复现）、TestReleaseSemverComparatorOrdersChannelCandidates、TestReleaseImageChannelPromotionIsMonotonicAndPreservesLatest、TestReleaseImageDigestReturnsAbsentOnlyForMissingManifest 等。已有 CRLF、Bash 接收 Windows 路径、python3 Store 执行别名问题。
- 原始 gofmt -l . 遍历到既有 tmp/baseline-* 目录，并受 CRLF 干扰；实际修改的 Go 文件格式清洁。

## 交付边界

最初实施阶段仅本地提交。用户随后明确授权推送 GitHub main 并部署到原服务器，交付范围相应扩展。

## 发布与部署（已完成）

- 修复提交 de2346468246ea4e4bc34f19008fe91eb833c9aa 已快进并推送 origin/main。
- Main images：https://github.com/0401lucky/gpt-load/actions/runs/36428540054。
- 升级前安全备份：/root/backups/gptload2-before-cline-fix-20260928-212629，包含 SQLite 在线一致性备份（quick_check=ok）、encryption.key、compose、.env 和脱敏基线，目录 0700、文件 0600。
- 升级前：13 分组 / 521 凭据 / 24 条迁移；旧镜像 sha-3f3c7476c89085edd9b180b0c8dc75aeac67d740。
- Main images 的 build、verify amd64、verify arm64、promote 全部成功。
- 2026-09-28 21:34（UTC+08:00）通过既有 upgrade.sh 升级为 2.0.0-dev.12.gde2346468246，镜像 digest sha256:eb4e97689e950a77c6552f1c08c0c30b4588dc40b186e7cec2e7fd25a79d54c4。
- compose 回滚点：/root/backups/gptload2-compose.bak-upgrade-20260928-213458；升级日志位于上述备份目录的 upgrade.log。
- 容器 healthy / RestartCount=0，OCI revision 与目标提交一致。升级后 13 分组 / 521 凭据 / 24 条迁移，捐献集成身份不变。
- 公网首页和 /health 均 200，内网 3002/3012 的 /health 均 200。服务器 urllib 默认 UA 一度收到 403，改常规 User-Agent 后服务器和本机公网访问都返回 200，无需改 Cloudflare。
- 用量接口耗时约 353ms，445=15,827,754 / 476=15,662,497 / 447=15,148,818 tokens，分别与精确日志查询一致；475 仍为真实本地零用量。上述行 data_complete=true。
- 对 476 发送一次最小连接测试：HTTP 429，摘要 Daily free limit reached ... Try again in 12h 5m，retry_after_seconds=null（上游只在正文提供时间），can_restore=false；测试前后所有该模型冷却截止时间一致。
- 最后五分钟被动观察为 17 个成功业务请求、1 个 canceled；未额外制造业务流量。
