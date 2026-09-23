# 迁移演练证据（B 方案，零停机路径）

> 执行时间：2026-09-23
> 被测提交：`0af4e399`（`merge: 合并上游 main 至 f2840466（上游新增迁移顺延为 0021-0024）`）
> 被测产物：`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` 得到的二进制，版本号 `2.0.0-dev+merge0af4e399`
> 靶子：**生产库的只读快照**（SQLite，312 MB，含 `encryption.key` 与 `models.dev.catalog.json`）
> 目的：验证 PRD 的 AC5（生产账本纯追加、数据零丢失）与 AC6 相关部分（幂等、空库）

## 0. 方法

1. 在 `yunyou-99` 上以 `sqlite3` 的 online backup API 从生产卷（`/var/lib/docker/volumes/gptload2-data/_data`）导出一致性快照到 `/tmp/gl-rehearsal/data/gpt-load.db`（WAL 已并入），连同 `encryption.key`、`models.dev.catalog.json` 一并复制；**全程未触碰生产数据与服务**。
2. 上传新二进制到 `/tmp/gl-rehearsal/bin-gpt-load`，用一次性容器运行：

```bash
docker run -d --name gl-reh-1 --network none \
  -v /tmp/gl-rehearsal/data:/app/data \
  -v /tmp/gl-rehearsal/bin-gpt-load:/app-bin/gpt-load:ro \
  --env-file /root/apps/gptload2/.env \
  -e DATA_DIR=/app/data -e HOST=0.0.0.0 -e PORT=3001 \
  alpine:3.24 /app-bin/gpt-load
```

   `--network none` 是刻意的：演练不需要外网，且可确保不会用生产凭据对真实上游发起调用（日志中的 `models_dev_catalog_sync failed` 即为预期的无网结果）。

3. 升级**没有**做任何账本修正（这正是 B 方案要证明的点）。

## 1. 生产库快照升级（最高优先级）

| 阶段 | 账本 | donation_resources | batches | items | groups | request_logs |
|---|---|---|---|---|---|---|
| 升级前（快照） | **20** | 486 | 36 | 62 | 13 | 86728 |
| 新二进制启动后 | **24** | 486 | 36 | 62 | 13 | 86322 |

账本终态（逐行核对）：

```
01–17  0001_initial … 0017_request_log_operation_index   （与升级前逐行一致）
18     0018_donation_intake                                （升级前已有，未动）
19     0019_donation_manual_review                         （升级前已有，未动）
20     0020_credential_note                                （升级前已有，未动）
21     0021_auto_model                                    ← 本次追加
22     0022_auto_decision_attribution                     ← 本次追加
23     0023_client_model_overrides                        ← 本次追加
24     0024_request_audit                                 ← 本次追加
```

**前 20 行逐行未变**（ID 字符串与顺序完全一致），新增 4 条落在尾部。

持久身份未变：`instance_id=c1e34efe-1f8b-45b7-897f-168d7e1ab926`、`source_id=49193b3f-900b-446e-9f6b-a426d1014dc7`。

上游 4 条迁移真实落库（不只是账本记账）：

- `auto_decision_usage_stats` 表存在；
- `client_model_overrides` 表存在；
- `request_audit_usages` 表存在（列为 `request_id, sequence, completed_at_ms, access_key_id, group_id, channel_id, credential_id, model …`）；
- `request_logs` 新增列存在（`auto_decision`、`decision_model`、`decision_costing_*` 等）。

启动日志关键行：

```
msg="database migration completed"  event=startup.database_migrate
msg="initial control state ensured" event=startup.bootstrap_complete
msg="credential registry loaded"    event=startup.credential_registry_load credentials=484
msg="GPT-Load 2.0 server started"   event=startup.ready version=2.0.0-dev+merge0af4e399
```

> `request_logs` 由 86728 降到 86322 是**服务自身运行 25 秒期间的日志保留策略**造成的（运行时清理，与迁移无关），业务表计数与身份 UUID 全部未变。

## 2. 幂等复核

对已升级到 24 条的快照**再次启动**新二进制：

- `database migration completed`，`startup.ready`，**无 error 级日志**；
- 账本仍 **24 条**且与首次升级后完全一致；
- `donation_resources=486`、`batches=36`、`items=62`、`groups=13`，身份 UUID 未变。

## 3. 空库全新安装

空 `DATA_DIR`（无 `encryption.key`）启动新二进制：

- `database migration completed` + `startup.ready`；
- 账本 **24 条**，末四条为 `0021_auto_model`、`0022_auto_decision_attribution`、`0023_client_model_overrides`、`0024_request_audit`；
- 30 张表；`DATA_DIR` 生成 `encryption.key`、`gpt-load.db`、`runtime-state.checkpoint.json`。

## 4. 未覆盖项

- 未在 PostgreSQL / MySQL 上重跑本轮演练（本轮改动只涉及编号与测试定位，方言路径与上游一致，且 `database-contract` 矩阵由 CI 覆盖）。
- 生产服务本身未升级；本文件只证明「同一份生产数据 + 新二进制」可平滑升级。
- `--network none` 下上游目录同步失败属预期，未验证联网启动路径（该路径与本次改动无关）。

## 5. 现场清理

演练产物（快照 + 二进制）留在服务器 `/tmp/gl-rehearsal`，任务收尾时删除；快照含生产密文与 `encryption.key`，不得外传或长期留存。
