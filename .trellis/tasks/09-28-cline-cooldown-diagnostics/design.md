# 技术设计

## 1. 边界与总体方案

本任务修复管理端取数和测试诊断，不修改 gateway/scheduler/state 的调度状态机。无数据库迁移，不触碰服务器生产状态。两个工作包按顺序实施以避免共同 DTO、契约测试和翻译文件冲突，统一验证后交付。

## 2. 模型用量的窗口计算

以每行 (credential_id, upstream_model) 为键，保留 groupModelUsageWindow 的既有起点选择规则：已发生的重置锚点优先，否则近 24h。本任务的“完整”只表示本项目在该推导窗口内留存的用量齐全，不代表供应商窗口必然准确。

### 2.1 互斥区间

对于 [start, observedAt)：

- 起始边界：[start, min(ceilHour(start), observedAt))。
- 完整小时：[ceilHour(start), floorHour(observedAt))，仅非空时使用 usage_stats。
- 结束边界：[max(floorHour(observedAt), 起始边界结束), observedAt)。

边界使用 request_logs 精确 completed_at_ms；完整桶使用 usage_stats。三者互斥，同小时首尾不会重复。对整点空边界不发查询。复用 internal/requestlog/credential_window_usage.go 的算法原则及 UsageAggregate 求和、整数校验和 status 成功定义，不按 HTTP 200 自创成功口径。

### 2.2 批量读取和一致性

- 在 requestlog 层新增/调整分组窗口读取入口，接收 group、观测时刻、按凭据/模型的窗口信息或等价表示；不能引用 control/state 类型，控制面负责转换。
- 小时聚合与边界明细在一个 dbtx.ReadSnapshot 中读取。控制面先捕获一次 runtime cycles 和 observedAt，随后计算，避免各行时钟漂移。
- 批量读取组内候选 usage_stats（包括当前小时和窗口起点所在小时用于发现行），与周期/冷却项取并集；无锚点的新模型用兜底窗口。按窗口裁剪后仍按原有“有用量或正在冷却”规则过滤。
- 将各行的边界小时去重并合并为查询区间，按组和时间索引批量读取所需明细，仅选择聚合必需的时间、归属、status 和 token 字段，不读取 payload/error_summary。
- 不对每个 key 调单凭据接口。常量次查询或按数据库参数上限分批读取边界区间；查询数不得线性依赖 key 数。优先一次小时读取、一次边界读取及既有完整性检查。
- 保留行数上限、排序、总行数、当前凭据过滤及按上游模型归属的既有契约。

### 2.3 留存与接口

- 新增每行 data_complete: boolean。存在需要补齐的边界且起点超出 requestLogWindowRetained 的可信范围时为 false；保留可确认的聚合与明细，不把缺失边界补成猜测值。
- 短窗口若边界明细已过期且候选桶显示存在用量，保留不完整行，不能因合计为 0 而静默隐藏。
- window_start_ms 与 window_source 不变。顶层 counted_from_ms / counted_to_ms 表示各行查询窗口的整体覆盖范围（最早起点至 observedAt），不再对用户宣称只有完整小时；每行真实起点仍由 window_start_ms 表达。
- classic/modern DTO、projector、帮助文案与不完整标识同步。正常行保持现有展示；缺失行显示“统计不完整”，说明已知数值不代表全部用量。
- 旧的“小时边界时间误差意味着约 4% token 误差”假设废止，更新 backend/database-guidelines.md。

## 3. 连接测试诊断

### 3.1 响应契约

CredentialProbeResponse 新增：

| 字段 | 类型 | 语义 |
|---|---|---|
| upstream_status_code | integer 或 null | 实际上游 HTTP 状态；没有响应时 null，不能拿管理接口 200 代替 |
| error_summary | string 或 null | 脱敏、限长的可展示错误摘要，成功或无可用摘要时 null |
| retry_after_seconds | integer 或 null | 明确 Retry-After / ErrorEvidence.RetryAfter 提供的正数建议，未知则 null |

正文中的 Try again in 通过 error_summary 原样（脱敏后）展示即可，不额外解析成数值：health.ParseResetHint 已混入 1 分钟余量和 24h 钳制，不适合冒充供应商原始重试时间。若有 Retry-After 日期，使用一次捕获的测试时刻换算，保持安全整数。

outcome/reason、can_restore、restore_proof 及自动协议回退不变。限流仍为 inconclusive，因为不能仅凭 429 判定凭据无效；具体摘要让用户理解是额度耗尽。

### 3.2 安全投影

- 执行适配器已有 errorMessageSummary：ExtractErrorMessage -> known-secret redaction -> 归一化 -> rune 限长，优先复用该已脱敏 ErrorEvidence。
- 控制面新增诊断投影不能假设所有执行器都已过滤；在 Probe 内仍持有凭据/已配置 header/proxy 的阶段完成已知秘密脱敏，出该阶段只携带安全诊断字段，不延长明文凭据生命周期。
- 复用 platform/redact，拒绝原始 HTML、原始 JSON、控制字符和内部错误堆栈；限长沿用 execution.MaxErrorSummaryLength。已知非标准 key、Bearer、代理认证等均纳入回归测试。
- 只展示来自有效 AttemptResult 的可用上游错误；准备失败/内部异常仍经现有错误出口安全回退。
- probe_completed 控制日志可附相同安全诊断字段，不能记录原始响应头、凭据或完整 body。

### 3.3 前端

classic 的 api/control/types.ts、app/resources/credentials.ts 白名单及 projectCredentialTestResult，modern 的 api/credential-actions.ts 同步更新；对应 CredentialTestDialog 使用文本插值显示详情并允许长行换行，不使用 v-html。三语言提供字段标签、无细节回退和重试提示文案。旧分类继续显示，新增详情仅在有值时出现。

## 4. 兼容与回滚

- 后端和两套前端一起提交，classic 对新增未登记字段会拒绝投影，必须由契约测试覆盖。
- 无数据库或检查点格式变化，回滚只涉及代码/镜像，原始历史数据始终保留。
- 不扩大探测权限，不让测试触发冷却写入或自动恢复；已有显式恢复按钮保持原合同。
- 实施完成只交付本地验证结果。发布与线上部署不在当前授权内。

## 5. 验证

覆盖 PRD AC1–AC5。批量窗口测试须覆盖同小时耗尽、首尾重叠、多 key 多模型、当前小时新行、留存不完整及查询数量。探测测试覆盖 429 正文、Retry-After 秒/日期、401、超时、成功、非法结果、敏感回显和无状态变更。最终 make check，不新增前端测试框架或 race 门槛。
