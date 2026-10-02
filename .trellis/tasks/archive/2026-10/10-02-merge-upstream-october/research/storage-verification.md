# 存储兼容合并验证（2026-10-02）

## 实现范围

- 上游 `0022_rpm_stats.go`、`0023_access_key_concurrency.go`、`0024_request_log_output_timing.go` 分别顺延为 `0025_rpm_stats.go`、`0026_access_key_concurrency.go`、`0027_request_log_output_timing.go`。
- 仅重命名文件、ID、导出函数与相关引用，添加来源注释；保留 `rpmStat0022`、`validateOutputTiming0024` 等私有名称，以及原表名、索引、约束和 SQL。
- `migration.go` 保留既有 24 项，追加 25–27。`migration_test.go`、`db_test.go`、`database_integration_test.go` 更新完整 27 条链断言，并保留 fork 18–20 及原 21–24 的 ID。
- RPM、并发、输出时间测试按迁移 ID 查找边界。上游本次修改到的自定义 AccessKey、模型冷却迁移测试也改用语义 ID；保留上游为旧 schema 写入增加的 `Omit` 字段。
- `fork_upgrade_migration_test.go` 新增真实 SQLite 24→27 回归：旧链建库，填充 11 张表（分组、凭据、AccessKey、日志、用量、捐献身份/批次/项目/资源/审核动作/重试），保存所有旧列值及完整账本，升级后只追加三条账本，原数据逐列相同；再次迁移仍相同。特别覆盖备注、冷却配置、身份/版本、密文、回执、审核记录和全部缓存 token 分项。旧请求的输出时点及旧 AccessKey 并发上限仍为 NULL，新 RPM 表为空。

## 验证证据

1. `go test -count=1 ./internal/storage/...`：最终全部通过。

   ```text
   ok gpt-load/internal/storage            24.332s
   ok gpt-load/internal/storage/dbtx        0.302s
   ok gpt-load/internal/storage/migrations  7.228s
   ok gpt-load/internal/storage/models      0.935s
   ```

2. 专项命令通过（`gpt-load/internal/storage 3.347s`）：

   ```text
   go test -count=1 ./internal/storage -run 'TestForkMigrationUpgradePreservesPublishedLedgerAndData|TestRPMMigration|TestConcurrencyMigration|TestOutputTimingMigration|TestMigrationRegistryContainsOrderedMigrations|TestAutoMigrateCreatesUsageJournalAndMigrationLedger' -v
   ```

   包含新库、旧库升级、并发列 DDL 中断、输出时点仅首列/全部 DDL 中断恢复、负值约束拒绝、第二次启动幂等。

3. 对 `HEAD` 的 24 个已发布迁移生产源文件逐个比较（仅标准化 CRLF/LF），全部一致。
4. 三个新增迁移分别与 `addd6987` 上游源文件比较，去掉来源注释并映射导出符号/ID 后逐字一致，确认没有改变内部 DDL 或私有模型。
5. 本次手工修改的 13 个 Go 文件定向 `gofmt -l` 无输出；`git diff --check -- internal/storage` 通过；storage 范围没有冲突标记。整个 `gofmt -l internal/storage` 仍列出未手工修改文件；逐文件先标准化 CRLF 为 LF 再送入 gofmt，确认全部仅由检出换行方式造成，未重写冻结迁移或无关文件。主会话需在完整门禁证据中如实记录该环境差异。

## 边界与交接

- 没有设置 `GPT_LOAD_DATABASE_TEST_DSN`，按项目约定跳过真实 MySQL/PostgreSQL opt-in 测试；保留全部方言分支和外部测试，不声明已实测网络数据库。
- 本次 storage 全套测试没有 Windows 环境失败，无需豁免或放宽断言。
- 子代理未执行 Git 暂存、提交、切换分支或合并；两处冲突索引、三个旧上游文件删除和三个新文件需由主会话统一暂存。实际文件内容已无冲突。
- 主会话继续完整 Go/前端门禁、最终差异检查与本地 merge commit。升级后回退须使用升级前数据库备份配合旧版本，不可用旧二进制直接打开 27 条账本。
