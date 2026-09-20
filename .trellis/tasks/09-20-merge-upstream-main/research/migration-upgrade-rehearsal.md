# 迁移兼容性演练证据

> 执行时间：2026-09-20
> 被测提交：`b2aba7d6`（`merge: 合并上游 main 至 b211e7c8，donation 迁移顺延为 0018/0019`）
> 被测产物：新版本二进制（由该提交构建）与旧版本二进制（由 `56c19ba6` 构建）
> 目的：验证 AC4（账本平滑升级 + 数据零丢失）与 AC5（幂等）

## 演练方法

生产库状态 = **账本 16 条**（`0001`–`0014` + `0015_donation_intake` + `0016_donation_manual_review`）。

升级动作 = `DELETE FROM schema_migrations WHERE id IN ('0015_donation_intake','0016_donation_manual_review')`，随后用新版本启动，由系统按正确顺序重放 `0015`–`0019`。

## 1. PostgreSQL 15（容器 `gl-verify-pg`，库 `gptload`）

| 阶段 | 账本 | donation_resources |
|---|---|---|
| 升级前 | 16 | 1 |
| 删账本后 | 14 | 1 |
| **新版本启动后** | **19** | **1** |

日志：
```
level=info msg="database migration completed" event=startup.database_migrate
```

账本终态（逐条核对，顺序正确）：
```
15 0015_group_usage_index
16 0016_credential_quota_history
17 0017_request_log_operation_index
18 0018_donation_intake
19 0019_donation_manual_review
```

## 2. MySQL 8.4（容器 `gl-verify-mysql`，库 `gptload`）

| 阶段 | 账本 | donation_resources | credentials |
|---|---|---|---|
| 升级前 | 16 | 1 | 3 |
| 删账本后 | 14 | 1 | 3 |
| **新版本启动后** | **19** | **1** | 3 |

日志同样出现 `database migration completed`。MySQL 方言走 `<id>#building` 恢复标记路径，本次未触发恢复分支。

## 3. SQLite（生产同构，最高优先级）

生产即 SQLite，故用**旧版本二进制完整复刻**生产状态，再以新版本升级，全程复用同一个 `DATA_DIR`（即同一把 `encryption.key`）。

| 阶段 | 账本 | identity |
|---|---|---|
| 旧版本建库并启动 | 16 | `73662d48-7f5c-4623-beeb-53c105f5b13d` / `6c84260f-93d4-4267-9cf5-e17459dd0aa1` |
| 备份 + 删账本 | 14 | 同上 |
| **新版本启动** | **19** | **完全未变** |

新版本启动日志（关键三行）：
```
level=info msg="database migration completed" event=startup.database_migrate
level=info msg="initial control state ensured" event=startup.bootstrap_complete
level=info msg="GPT-Load 2.0 server started" event=startup.ready version=2.0.0-dev
```

donation 7 张表齐全：`donation_batches`、`donation_identities`、`donation_items`、`donation_resources`、`donation_retries`、`donation_review_actions`、`donation_test_attempts`。

## 4. 幂等反证（AC5）

对已升级完成的 SQLite 库**再次启动新版本**：

- `database migration completed` —— 无错误
- 账本仍 19 条，identity 未变
- `bootstrap_complete` 与 `startup.ready` 正常

PG 库同样做了二次启动，账本 19 条、`donation_resources` 保持 1，数据未重复。

底层依据：`internal/storage/migrations/0019_donation_manual_review_test.go` 已断言「二次 `Up0019` 为 no-op、数据不重复不丢失」，本次演练在完整 19 条注册表上复现了该性质。

## 5. 过程中发现的两个关键约束

### 5.1 `encryption.key` 必须与数据库一同保留（升级硬约束）

在 PG / MySQL 演练中，使用**全新 `DATA_DIR`**（因而生成了新的 `encryption.key`）时，迁移虽然成功，但服务在 bootstrap 阶段拒绝启动：

```
error="bootstrap initial state: ensure initial control state: Donation integration is unavailable"
```

根因在 `internal/control/donation_resources.go:87`：

```go
plaintext, err := s.encryption.Decrypt(row.Data)
if err != nil {
    return app_errors.ErrDonationUnavailable
}
```

`backfillDonationInventory` 会解密库中所有 API-key 凭据；密钥不匹配即导致启动失败。

**对生产升级的意义**：SQLite 部署下 `encryption.key` 与 `gpt-load.db` 同处 `gptload2-data` 卷，只要整卷备份/还原即自然满足。**若只备份 db 文件而遗漏 `encryption.key`，即使迁移成功服务也起不来**。升级手册须显式列出该文件。

### 5.2 上游测试对「最后一条迁移」的隐式假设

`internal/storage/operation_index_migration_test.go` 原以 `migrations[:len(migrations)-1]` 表达「0017 之前的迁移前缀」。donation 迁移追加到末尾后，该表达式变为「0019 之前」：

- `unexpected index definition` / `unexpected index direction` 两个子测试**直接失败**
- `existing` / `interrupted` 两个子测试**静默改测 0019 却仍然通过** —— 比失败更危险

已改为按 ID 定位 0017 下标（`operationIndexMigrationIndex`），使测试不再依赖链尾位置。

## 6. 未验证项

- **生产库真实数据**（439 条资源 / 36 个捐献项）未直接参与演练；SQLite 演练用的是旧版本新建的同构库。生产副本预演未执行。
- 生产为 SQLite，演练同样在 SQLite 上完成，方言与路径一致；PG / MySQL 演练用于确认三库行为一致，不代表生产会走这两个方言。
- 上游新增的 `0015_group_usage_index` / `0016_credential_quota_history` / `0017_request_log_operation_index` 在演练库上均为**全新执行**，其自身的既有测试由 `make check` 覆盖。
