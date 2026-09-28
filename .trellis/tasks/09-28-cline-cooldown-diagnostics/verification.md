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

代码与任务记录仅本地提交，不推送、不部署。服务器现行镜像仍为旧版本，因此线上页面需后续部署才会生效；既有冷却及历史数据均未改变。
