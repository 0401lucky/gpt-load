# 存储兼容合并记录

## 合并契约

- 原有 `0001–0027` 身份与生产迁移源码冻结；对固定起点 `dbb2b42b0bca07a83c7b0668ed5bfa549074409a` 做 LF 归一化逐文件比对，27/27 完全一致。
- 上游 `a5c691bb0559f856ebb587879d82e341ff480202` 新增迁移顺延如下：

| 上游 | fork |
| --- | --- |
| 0025_proxy_catalog | 0028_proxy_catalog |
| 0026_credential_names | 0029_credential_names |
| 0027_remove_request_log_output_timing | 0030_remove_request_log_output_timing |
| 0028_request_log_client_ip | 0031_request_log_client_ip |
| 0029_group_priority | 0032_group_priority |

- 五个新增文件逐字比对上游：除来源注释、ID/导出符号映射，以及清理迁移中的旧 `Validate0024` / `ValidateRecoverable0024` / `ValidateCurrent0024` 映射到 `0027` 外，源码完全一致。冻结局部类型、约束和索引名保留上游原名。
- `0030` 内定义 `ValidateCurrent0027` 并挂到旧 `0027` 注册项；只在账本存在 `0030`（或 MySQL 的 `0030#building`）时接受已删除计时列，无依据缺列仍拒绝启动。
- `credentials.name` 与 fork `credentials.note` 共存；运行时模型保留两字段。组优先级与客户端 IP 按上游迁移默认值演进。

## 测试兼容

- 新迁移测试、旧输出计时测试的截断边界全部使用 `migrationIndexByID`，避免重叠编号静默测错迁移。
- 完整注册表、账本列表更新为 32 条，外部数据库 schema 断言包含 `name` / `note` / `priority` / `client_ip`，移除已淘汰计时列要求。
- 旧 schema fixture 显式忽略尚不存在的 `Name` / `Priority` / `ClientIP`；移除已删除运行时计时字段的无效 Omit。
- `TestForkMigrationUpgradePreservesPublishedLedgerAndData` 覆盖真实 SQLite 24→32 与 27→32，建立旧账本及业务行后连续执行两次完整升级，逐列比较旧账本、组、凭据身份/密文/备注/时间、访问密钥、用量缓存分项、请求和请求尝试、捐献批次/资源/人工审核/重试回执。仅排除上游明确删除的输出计时列；27 条旧库包含非空计时数据，确认删除列不会级联丢失请求尝试。

## 验证与预算

- 首轮与 JSON 复验仅失败 `TestMigrationValidationLimitsMetadataQueries`：初始化 1131 次 SQL，超过上游新增 SQLite 上限 1000；重启 249 次，低于 400。
- fork 额外保留捐献接收、人工审核与凭据备注三个迁移，包含额外建表与 DDL。仅把 SQLite 初始化预算设为 1200，较实测留约 6% 余量；重启上限 400、MySQL 查询约束及其他方言预算不变。
- 最终完整 `go test -json -count=1 ./internal/storage/...` 通过：storage 75.219s、dbtx 3.670s、migrations 25.422s、models 0.989s。JSON 原始输出位于本机临时目录 `gpt-load-storage-final-20261008.jsonl`。
- `go vet ./internal/storage/...`、`gofmt -l internal/storage`（无输出）、storage 范围 `git diff --check` 通过；storage 无冲突标记或过期上游迁移 ID 引用。
- 无 `GPT_LOAD_DATABASE_TEST_DSN`；真实 MySQL/PostgreSQL 用例按既有 opt-in 规则跳过，不能声称已实测这两个服务。

## 范围与升级提醒

只修改 `internal/storage/**` 和本记录；未执行 git add/commit/push，也未连接用户数据库。升级会按上游意图删除旧 `first_output_ms` / `last_output_ms`，部署前需备份数据库与配套密钥；旧二进制回退需使用升级前数据库备份。
