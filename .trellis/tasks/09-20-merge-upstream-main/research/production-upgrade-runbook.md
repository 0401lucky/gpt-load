# 生产升级手册：gptload2 迁移重编号

> 目标主机：`yunyou-99`（64.83.25.99，SSH 端口 **43477**，用户 `root`）
> 目标服务：`/root/apps/gptload2`，容器 `gpt-load-2`，镜像 `ghcr.io/0401lucky/gpt-load:main`
> 背景：合并上游后，donation 迁移由 `0015`/`0016` 顺延为 `0018`/`0019`；上游新增 `0015`/`0016`/`0017`。
> 账本按数组下标严格比对迁移 ID，**不做账本修正直接换镜像会启动失败**。

## 0. 为什么必须多做一步

升级前账本（16 条）：

```
14 0014_affinity_kind
15 0015_donation_intake          ← 与新版注册表第 15 位语义不同
16 0016_donation_manual_review
```

新版注册表（19 条）：

```
15 0015_group_usage_index
16 0016_credential_quota_history
17 0017_request_log_operation_index
18 0018_donation_intake
19 0019_donation_manual_review
```

因此需先删除账本中两条旧 donation 记录，让系统按正确顺序重放 `0015`–`0019`。`0018`/`0019` 的 `Up` 函数均为幂等（逐列/逐约束判存 + `AutoMigrate`），在已存在的 schema 上重跑是空操作——**该性质已经三库演练验证**，见 `migration-upgrade-rehearsal.md`。

## 1. 前置检查

```bash
# 确认当前版本与容器状态
docker inspect gpt-load-2 --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}'

# 确认磁盘余量（备份需要约 170MB + 卷快照）
df -h / | tail -1

# 记录升级前账本（用于升级后比对）
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect(f'file:{V}?mode=ro', uri=True)
rows = con.execute('SELECT id FROM schema_migrations ORDER BY id').fetchall()
print('ledger_total =', len(rows))
for i, r in enumerate(rows, 1):
    print(f'  {i} {r[0]}')
PY
```

**预期**：`ledger_total = 16`，第 15、16 条分别为 `0015_donation_intake`、`0016_donation_manual_review`。
**若不符，停止升级**，先查清实际状态。

同时记录业务数据基线（升级后须完全一致）：

```bash
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect(f'file:{V}?mode=ro', uri=True)
for t in ['donation_identities','donation_batches','donation_items',
          'donation_resources','donation_review_actions','donation_test_attempts']:
    print(t, con.execute(f'SELECT COUNT(*) FROM {t}').fetchone()[0])
print('instance_id/source_id =',
      con.execute('SELECT instance_id, source_id FROM donation_identities').fetchall())
PY
```

**记录 2026-09-20 排查时的实测值供对照**（实际以升级当刻为准）：
身份 1 / 批次 30 / 项 36 / **资源 439** / 审核动作 23 / 测试尝试 24，
`instance_id=c1e34efe-1f8b-45b7-897f-168d7e1ab926`，`source_id=49193b3f-900b-446e-9f6b-a426d1014dc7`。

## 2. 备份（不可跳过）

⚠️ **`encryption.key` 必须与数据库一同备份**。它在 `gptload2-data` 卷内，与 `gpt-load.db` 同目录。缺了它，即使迁移成功，服务也会因无法解密既有凭据而拒绝启动（`DONATION_UNAVAILABLE`）——本次演练已实证，见 `migration-upgrade-rehearsal.md` §5.1。

```bash
# 先停容器，确保 WAL 落盘、文件一致
cd /root/apps/gptload2
docker compose stop gpt-load

# 确认已停止
docker inspect gpt-load-2 --format '{{.State.Status}}'   # 期望 exited

# 整卷备份（含 db + wal + shm + encryption.key）
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p /root/backups
tar -czf "/root/backups/gptload2-data.bak-${TS}.tar.gz" \
    -C /var/lib/docker/volumes/gptload2-data _data
ls -lh "/root/backups/gptload2-data.bak-${TS}.tar.gz"

# 同时留一份 compose 与 .env
cp /root/apps/gptload2/docker-compose.yml "/root/backups/gptload2-compose.bak-${TS}"
cp /root/apps/gptload2/.env                "/root/backups/gptload2.env.bak-${TS}"

# 校验备份内含 encryption.key
tar -tzf "/root/backups/gptload2-data.bak-${TS}.tar.gz" | grep -E 'encryption.key|gpt-load.db'
```

**预期**：列表中出现 `_data/encryption.key` 与 `_data/gpt-load.db`。
**记下 `${TS}`**，回滚时要用。

## 3. 修正账本

```bash
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect(V)
cur = con.execute(
    "DELETE FROM schema_migrations WHERE id IN ('0015_donation_intake','0016_donation_manual_review')")
print('deleted =', cur.rowcount)
con.commit()
rows = con.execute('SELECT id FROM schema_migrations ORDER BY id').fetchall()
print('ledger_total =', len(rows))
print('last three =', [r[0] for r in rows[-3:]])
con.close()
PY
```

**预期**：`deleted = 2`、`ledger_total = 14`、`last three = ['0012_access_key_mask_prefix', '0013_validation_protocol', '0014_affinity_kind']`。

## 4. 升级镜像并启动

```bash
cd /root/apps/gptload2
docker compose pull gpt-load
docker compose up -d gpt-load

# 观察迁移日志（关键三行）
docker logs gpt-load-2 2>&1 | grep -E "database migration completed|bootstrap_complete|startup.ready|startup.failed"
```

**预期**：
```
msg="database migration completed"       event=startup.database_migrate
msg="initial control state ensured"      event=startup.bootstrap_complete
msg="GPT-Load 2.0 server started"        event=startup.ready
```

**若出现 `startup.failed ... Donation integration is unavailable`**：
- 迁移本身大概率已成功（看 `database migration completed` 是否出现）；
- 优先检查 `.env` 的 `DONATION_INTEGRATION_TOKEN` 是否仍在、是否与升级前一致；
- 再检查第 2 步备份是否真的包含 `encryption.key`，以及是否误用了新生成的数据目录。

## 5. 升级后验证

```bash
# 容器健康
docker inspect gpt-load-2 --format '{{.State.Status}} {{.State.Health.Status}} {{.RestartCount}}'

# 账本应为 19 条且顺序正确
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect(f'file:{V}?mode=ro', uri=True)
rows = con.execute('SELECT id FROM schema_migrations ORDER BY id').fetchall()
print('ledger_total =', len(rows))
for i, r in enumerate(rows, 1):
    print(f'  {i} {r[0]}')
PY
```

**预期末四条**：`0016_credential_quota_history`、`0017_request_log_operation_index`、`0018_donation_intake`、`0019_donation_manual_review`。

**业务数据必须与第 1 步基线完全一致**（重跑第 1 步的数据比对脚本），尤其：
- `donation_resources` 应为 439（或升级当刻的实测值）
- `instance_id` / `source_id` **不得变化**（变了说明身份被重建，属严重问题）

```bash
# 集成端点连通性（token 在 .env 中）
set -a; . /root/apps/gptload2/.env; set +a
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer ${DONATION_INTEGRATION_TOKEN}" \
  http://127.0.0.1:3002/integrations/donations/v1/capabilities
# 期望 200（不带 token 应返回 401）

# nginx 前置入口
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:3012/health   # 期望 200

# 两个 new-api 的真实请求是否仍成功
docker logs --since 10m gpt-load-2 2>&1 | grep -c "status=success"
```

**幂等复核**（可选但推荐）：再执行一次 `docker compose up -d gpt-load` 重启，确认 `database migration completed` 无报错、账本仍 19 条、数据未变。

## 6. 回滚

⚠️ **镜像回滚必须与卷快照恢复配套**。账本已被改为 19 条，旧镜像（16 条注册表）同样会因下标不匹配而启动失败——**单独把镜像改回旧 tag 是起不来的**。

```bash
# 一步回滚：停容器 → 恢复卷 → 换回旧镜像
cd /root/apps/gptload2
docker compose stop gpt-load

# 恢复数据卷（用第 2 步记下的 TS）
docker run --rm -v gptload2-data:/data -v /root/backups:/backup alpine \
  sh -c "rm -rf /data/* && tar -xzf /backup/gptload2-data.bak-<TS>.tar.gz -C /data --strip-components=1"

# 恢复 compose 并改回旧镜像
cp /root/backups/gptload2-compose.bak-<TS> /root/apps/gptload2/docker-compose.yml
docker compose up -d gpt-load
```

**回滚的三个层级**：

| 层级 | 场景 | 动作 |
|---|---|---|
| L1 | 迁移成功但服务异常 | 恢复卷快照 + 旧镜像（必须成对，见上） |
| L2 | 迁移中途失败 | 同上 |
| L3 | 数据异常 | 卷快照即全量数据备份（含 439 条资源与身份 UUID） |

**建议**：升级前先给当前 `:main` digest 打一个可回滚的 tag，便于 L1 使用：
```bash
docker tag ghcr.io/0401lucky/gpt-load:main ghcr.io/0401lucky/gpt-load:rollback-$(date +%Y%m%d)
```

## 7. 演练依据与未覆盖项

- 三库（PostgreSQL 15 / MySQL 8.4 / SQLite）升级演练与幂等复核：见 `migration-upgrade-rehearsal.md`。
- SQLite 演练用**旧版本二进制新建的同构库**复刻生产状态（16 条账本 + 旧 donation schema），再以新版本升级；**生产库本身未参与演练**。
- 生产库的真实数据规模（439 条资源）大于演练库；`AutoMigrate` 与逐列判存均为幂等操作，不改写既有行，但**首次在生产执行时请严格按第 1 步记录基线、第 5 步逐项比对**。
- 停机窗口取决于第 2 步备份耗时（约 170MB 打包），建议低峰期执行。
