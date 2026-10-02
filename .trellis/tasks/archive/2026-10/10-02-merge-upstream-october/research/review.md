# 独立兼容性审查（2026-10-02）

## 审查结论

检查子代理以 `c79bca8f` 和 `addd6987` 为双亲基准，读取任务 PRD、设计、执行计划和 `check.jsonl` 规范，并检查当前工作树。未发现需要修改行为的合并兼容性缺陷；修复一处 Go 格式门禁问题。此记录不代替主会话的最终完整门禁、暂存审查与双亲提交校验。

## 已修复

- `internal/subscription/runtime/runtime_test.go`：`completeTestRuntimeImplementations` 的多返回值复合字面量不符合当前 Go 1.27 的 gofmt 排版。按主会话扩展的文件授权执行定向 `gofmt -w`，只改变缩进。随后 `go test -count=1 ./internal/subscription/runtime` 通过（1.036s），`gofmt -l` 该文件零输出。

## 16 个冲突文件核对

| 文件 | 独立检查结果 |
| --- | --- |
| `README.md`、`README_CN.md`、`README_JP.md` | 捐献与分组冷却说明保留，上游空响应重试、并发与实时语音说明合并；人工审核迁移链接使用 fork 的 0019。 |
| `internal/platform/config/config.go` | 对上游差异仅为既有捐献 token 字段、加载与校验。上游 Live 环境配置函数完整，捐献校验函数独立闭合。 |
| `internal/state/runtime_settings.go` | 上游新增配置完整；冷却常量、默认 false、基线解析、分组继承/覆盖保留，并仍不在 `IsRuntimeSettingKey` 的全局白名单内。 |
| `internal/state/snapshot.go` | `GroupView` 与 `Compile` 同时复制冷却、空响应重试、Live 和并发设置。 |
| `internal/control/group_detail.go` | effective DTO 字段和赋值同时包含新旧设置；原有数字配置嵌入结构保留。 |
| `internal/control/modern_credentials.go` | 列表和详情均显式构造 Note 与 RPMPeakHour；共享 `CredentialItemResponse` 两字段仍为 `json:"-"`。 |
| `internal/parameteroverride/rules.go` | `Fingerprint()` 和 `ConfiguredFields()` 分别闭合；前者保持长度前缀及 JSON pointer 分段编码，后者保持匹配规则及稳定排序；上游协议支持与后续方法未损坏。 |
| `internal/storage/database_integration_test.go`、`internal/storage/db_test.go` | 27 条完整链包含原 fork 18–24 与新增 25–27，新增 RPM 表及并发/输出列断言保留。 |
| classic `api/control/types.ts`、`app/resources/groups.ts` | 两套 DTO、严格字段白名单和投影均保留冷却与重试开关，以及并发数和 Live 模式校验。 |
| classic `features/groups/settings/group-settings-patch.ts` | clone/normalize/dirty/patch 使用完整集合，显式 false 不会丢失，恢复继承以删除覆盖键表达。 |
| classic `features/groups/settings/GroupSettingsTab.vue` | 两个开关的 computed、toggle、set 和模板行完整，无共用括号导致的逻辑嵌套。 |
| modern `api/group-detail.ts` | 共享 runtimeSwitches 包含两个开关，读取、必填检查及高级设置读写循环保持一致；凭据解析同时保留备注与 RPM。 |

对 `internal`、`web/src` 和三份 README 的冲突标记搜索无匹配。检查时 Git 索引中的原冲突仍由主会话统一暂存解决，子代理没有暂存、提交或切换分支。

## 迁移与测试的独立证据

- 用 Git 读取 HEAD 中的所有既有迁移生产文件，与工作树逐个比较，仅标准化 CRLF/LF；原 24 条全部一致。
- 新迁移与固定上游逐文件比较，差异只有公开编号/导出符号、ID 和一行来源注释。`rpmStat0022`、旧版本私有验证器、SQL、索引及约束名称没有随编号批量改写。
- `migration.go` 对 HEAD 的差异只有最后追加的三项；原 Validate/ValidateCurrent/ValidateRecoverable 注册值保留。
- RPM、并发和输出时间测试通过语义 ID 定位前置 schema 与 DDL 中断点；本次受上游修改影响的 custom AccessKey 和 model cooldown 测试也改用语义 ID。旧链内未受影响的固定下标仍指向相同迁移，无需扩大改动。
- `TestForkMigrationUpgradePreservesPublishedLedgerAndData` 先执行真实旧 24 条迁移，确认新表/列不存在，再插入 11 张表的合成数据。保存每张表的所有旧列，并在升级和再次迁移后逐列比对；同时比较完整账本、NULL 默认值和空 RPM 历史。测试覆盖备注、冷却 overrides、凭据身份/版本、捐献回执和审核/重试记录、全部 token 缓存分项，未用放宽断言或跳过方式绕过升级问题。

## 自动合并路径

- 终态日志仍由 `requestRecorder.bindUsage` 将 token/价格绑定到唯一一次 attempt。`requestlog.mapEvent` 按该组/凭据/上游模型写入，并校验 attribution 匹配恰好一次 attempt；每次尝试统计与 RPM 分开写入，新增 retry/输出时点没有把全部尝试重复汇入矩阵。
- 用量矩阵源文件保留 fork 原实现：单一读快照中合并完整小时桶与不重叠精确首尾明细，使用 `upstream_model`，保留不完整窗口标志与缓存 token 分项。现有 `group_model_usage_test.go`、mapper/worker 归属和幂等测试覆盖相应路径。
- 冷却判断仍接收分组 `RateLimitResetHintEnabled`，上游空响应重试采用独立无冷却效果的分类；新旧选项共存的控制面测试已同时验证持久化、effective 和运行快照。
- 凭据备注回归实际插入 RPM 样本，验证 modern 列表仍显示峰值 9、classic 列表/详情不泄漏 Note/RPM，并覆盖备注清空、显式 null、不相关字段更新与字符上限。
- Cline/Gemini embeddings 的新增适配保留；既有连接探测继续只使用指定凭据、生成安全诊断且不改变运行态。`Fingerprint()` 仍由捐献人工审核目标签名调用，参数改变撤销恢复资格的现有回归保留。
- 容器 `newHTTPRegistry` 保留 `DonationHTTPModule()`，与上游 RPM、Live DI 同时装配；捐献资源去重和普通凭据导入路径保留原 fork 增量。

## 验证与尚未覆盖范围

- 本审查直接执行：定向 gofmt 检查/修复、`go test -count=1 ./internal/subscription/runtime`，通过。
- 已读取实现证据：`storage-verification.md` 中 storage 全套和专项通过；`backend-verification.md` 中 config/state/parameteroverride/control/health/requestlog 通过、两个扩展兼容测试通过；`frontend-verification.md` 中 ESLint/样式检查、Prettier、vue-tsc/tsc 和 Vite build 全部通过。遵照派发要求没有重复全套门禁。
- 主会话已确认 root tidy/vet 与 CPA tidy/vet 通过，正在汇总完整测试 JSON；最终是否全绿及基线逐用例比较由主会话写入最终验证记录。
- 真实 MySQL/PostgreSQL 未设置 opt-in DSN，不能宣称已实测；对应代码和外部测试均保留。Windows 历史权限/网络 deadline 与 CRLF 失败须按本轮原始基线核对，不能只比较失败包名。
- 未发现需要产品决策而未修复的代码问题。AC1/AC7 的最终 main 历史、merge commit、原未跟踪工作保护，以及完整门禁最后结果仍属于主会话交付步骤。
