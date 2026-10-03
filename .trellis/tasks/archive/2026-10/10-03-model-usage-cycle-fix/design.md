# 技术方案

health.Decision 增加独立可选的额度重置时刻/来源。默认冷却和 Retry-After 只影响调度，不能凭 CooldownUntil 或时长推断周期。当前可信来源限于明确 reset hint。

gateway 向 state 分别传递冷却与可信额度证据。普通 SetModelCooldown 只更新冷却，可信证据原子更新周期；不被“冷却未延长”提前返回吞掉。保留身份代次校验、冷却不缩短、重复通知不截断周期。

检查点新增周期版本/来源标识。旧 ModelCycleStarts/ModelNextResets 无来源证明，恢复时降级为空；保留 ModelCooldowns、黑名单及失败计数。页面退回 fallback_24h，后续可信证据建立周期。历史请求和 usage_stats 不改。旧状态不能按时长猜测恢复。

复用 UsageAggregate 的 UsageMissingCount/PartialCount，矩阵 API 新增计数，贯通小时桶与边界日志。data_complete 继续代表日志保留完整性，另行提示 token 缺失。两套 UI 保留已知 token 合计，全部缺失显示未知/缺失。失败也可能 missing，文案不能暗示所有缺失请求都产生消耗。

检查 buffered capture、协议转换与 OpenAI ExtractUsage，以本地脱敏 fixture 覆盖合法 usage、缺失、无效字段及转换丢失。数据库 missing 无法还原上游原始响应。仅凭可复现证据修解析；无法复现记录限制，不推测补账。

部署另行确认。升级后旧周期暂退近 24h，不能准确重建长周期；数据库历史不变。回滚旧代码将重新引入周期污染。
