# 上游兼容合并设计

## 边界与交付

从本地 `c79bca8f` 建立 `merge/upstream-20261002` 集成分支，向它合入固定目标 `addd6987`，保留 merge 的双亲关系。主会话发起合并并协调 Git 索引；实现子代理按不重叠文件范围修复，检查子代理随后复核。验证完成后记录 merge commit，将本地 `main` 快进至已验证结果。若 `main` 被其他工作推进，先检查新变更，不覆盖或强制重置。

任务文档随本轮交付记录；暂存只选本轮文件，不使用会收入已有未跟踪目录的全量暂存。用户已在最终规划展示后明确批准实施，可按本设计创建集成分支并合并。

## 迁移映射与持久化契约

| 上游新增迁移 | fork 新 ID / 文件前缀 | 作用 |
|---|---|---|
| `0022_rpm_stats` | `0025_rpm_stats` | RPM 历史统计表 |
| `0023_access_key_concurrency` | `0026_access_key_concurrency` | AccessKey 可空并发上限 |
| `0024_request_log_output_timing` | `0027_request_log_output_timing` | 请求日志首末输出时间 |

旧链 `0001`–`0024` 完整冻结。旧链尾仍是 `0021_auto_model`、`0022_auto_decision_attribution`、`0023_client_model_overrides`、`0024_request_audit`，随后才是新三条。

只调整新迁移文件名、ID 字符串、导出符号与相应注册/测试引用；不改变内部冻结模型、约束、索引、表名和 DDL 标识。旧迁移 `ID0022` 等仍然有效，不能对整个仓库做无差别替换。

预演的 `migration.go` 没有文本冲突，但合并后只剩 24 个注册项，新旧迁移还导出同名函数。必须显式追加 25–27 三项、保留原注册项的各验证函数，并修订完整链断言。

新增和受上游修改影响的迁移测试按语义 ID 定位旧 schema 边界，沿用 `operationIndexMigrationIndex` 等先例；审查 `migrations[:N]`、`migrations[N]`、`len(migrations)-1` 以及旧 schema fixture 的 `Omit` 字段。

验证使用本地临时 SQLite 和合成数据：先用旧 24 条迁移建库、填充捐献/备注/身份等样本，保存完整账本与已有数据；跑新注册表后仅新增三行且数据不变；重复运行无变化。保持 MySQL/PostgreSQL 方言和恢复验证器逻辑，按现有测试支持检查；没有可用真实外部数据库时明确该范围未实测。

## 冲突解决

| 范围 | 解决策略 |
|---|---|
| 三份 README | 保留 fork 冷却开关说明，并接纳上游新配置说明，三语言结构一致 |
| `platform/config/config.go`、`state/runtime_settings.go`、`state/snapshot.go` | 合并字段集合、默认值、继承和快照赋值；冷却开关保持分组专属，上游并发、语音与重试设置保持上游作用域 |
| `control/group_detail.go` | `effective` 同时投影冷却开关、空响应重试、语音与并发字段 |
| `control/modern_credentials.go` | 同时保留 `Note` 与 `RPMPeakHour`，集合/详情构造均赋值；备注不泄漏到 classic 共用 DTO 的响应 |
| `parameteroverride/rules.go` | 保留 `Fingerprint()` 与新增 `ConfiguredFields()` 两个独立方法，各自具备完整函数边界，保留新增协议支持 |
| classic 类型、资源、设置 patch 与组件 | DTO、严格白名单、运行时投影、clone/dirty/保存和开关展示同步；新旧配置可同时编辑和继承 |
| modern `api/group-detail.ts` | 合并 switches/数值配置及解析保存路径，避免保存时丢弃旧开关 |
| storage 两个冲突测试及自动合并文件 | 按 27 条最终链同步，不接受文本自动合并作为正确性证明 |

## 自动合并区域复核

1. 用量归属：网关终态请求日志与用量矩阵取数、跨窗口聚合及上游多次尝试记账同时检查，避免重试重复计数或窗口漏算。
2. Cline 与探测：上游新增 Cline 模块/执行适配不能使已有连接诊断和参数指纹失效；复用已存在的可重现测试，不扩展未完成的真实对话任务。
3. 凭据：modern 备注与 RPM 展示同时保留；classic/modern 投影分别复核，敏感字段规则不放宽。
4. 捐献：路由/容器依赖装配、凭据写入和迁移在上游更新后保留既有幂等、审核与来源契约。
5. 依赖与容器：接纳上游锁文件和 CPA/Bifrost 升级，独立验证 CPA module；保留 `.github/workflows/ghcr-main.yml`，不触发发布。

## 验证与风险处理

- 完整门禁来自 `Makefile`；本机当前没有 `make`，可按顺序执行等价命令。Go `1.27.0` 与 Node `25.6.0` 已可用。
- 历史有 Windows CRLF、鉴权文件权限与内嵌 UI 相关失败，但不能沿用历史结论。以本轮固定起点、同工具版本和相同检出方式采集基线，再比较具体失败用例和消息。必要时在仓库外受控 LF checkout 验证，不更改全局 Git 配置。
- 不新增本地 race、前端测试框架或发布端到端门禁；前端由 lint、format、type-check/build 和 Go 契约测试覆盖。
- 对确实存在的兼容错误添加能复现行为的回归测试；不以跳过测试或放宽断言换取通过。
- 升级后旧二进制不认识 27 条迁移链，回退应使用升级前数据库备份与旧版本配对；本轮只提供说明，不执行部署。
- 集成期间需要取消时先保存本轮工作并确认工作区，再在本轮合并仍未提交且没有外部写入时使用 `git merge --abort`；保留分支供检查，不采用 `reset --hard`、`clean` 或删除用户文件。
