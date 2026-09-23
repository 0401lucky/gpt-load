# 生产升级说明：gptload2（本次无需改账本、无需停机）

> 目标：`/root/apps/gptload2`，容器 `gpt-load-2`
> 前置：合并提交 `0af4e399` 已由 `ghcr-main.yml` 产出镜像（本轮交付后另行触发）
> 与 2026-09-20 那次的关键差别：**账本是纯追加**，不需要删账本行、不需要停容器做手术

## 1. 升级步骤

```bash
cd /root/apps/gptload2

# 记录升级前基线（账本 20 条 + 业务计数 + 身份 UUID）
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect('file:%s?mode=ro' % V, uri=True)
print('ledger_total =', con.execute('SELECT COUNT(*) FROM schema_migrations').fetchone()[0])
for t in ['donation_resources','donation_batches','donation_items','groups','request_logs']:
    print(t, con.execute('SELECT COUNT(*) FROM %s' % t).fetchone()[0])
print('identity =', con.execute('SELECT instance_id, source_id FROM donation_identities').fetchall())
PY

docker compose pull gpt-load
docker compose up -d gpt-load
docker logs gpt-load-2 2>&1 | grep -E "database migration completed|bootstrap_complete|startup.ready|startup.failed"
```

**预期日志**：`database migration completed`、`initial control state ensured`、`GPT-Load 2.0 server started`。

## 2. 升级后验证

```bash
python3 - <<'PY'
import sqlite3
V = '/var/lib/docker/volumes/gptload2-data/_data/gpt-load.db'
con = sqlite3.connect('file:%s?mode=ro' % V, uri=True)
rows = [r[0] for r in con.execute('SELECT id FROM schema_migrations ORDER BY id')]
print('ledger_total =', len(rows))
print('last four =', rows[-4:])
print('first twenty unchanged =', rows[:20])   # 应与升级前逐行一致
PY
```

必须满足：

- 账本 **24 条**，末四条为 `0021_auto_model`、`0022_auto_decision_attribution`、`0023_client_model_overrides`、`0024_request_audit`；
- **前 20 行与升级前逐行一致**（`0018_donation_intake`/`0019_donation_manual_review`/`0020_credential_note` 位置不变）；
- 业务计数与 `instance_id` / `source_id` 与第 1 步基线一致；
- 容器 `healthy`、`RestartCount` 不增长；
- 集成端点仍 200：`curl -H "Authorization: Bearer $DONATION_INTEGRATION_TOKEN" http://127.0.0.1:3002/integrations/donations/v1/capabilities`。

## 3. 回滚

与 2026-09-20 同样的硬约束：**换回旧镜像之前，账本必须先退回 20 条**（旧版本注册表只有 20 条，24 条的账本会让它拒绝启动）。因此回滚仍为「恢复卷快照 + 旧镜像」成对操作：

```bash
cd /root/apps/gptload2
docker compose stop gpt-load
docker run --rm -v gptload2-data:/data -v /root/backups:/backup alpine \
  sh -c "rm -rf /data/* && tar -xzf /backup/gptload2-data.bak-<TS>.tar.gz -C /data --strip-components=1"
# 再切回旧镜像 tag（升级前用 docker tag 固定一个 rollback-<date>）
docker compose up -d gpt-load
```

升级前的镜像 tag 建议：`docker tag ghcr.io/0401lucky/gpt-load:main ghcr.io/0401lucky/gpt-load:rollback-$(date +%Y%m%d)`。

## 4. 演练依据

- 生产库快照（312 MB，账本 20 条）+ 新二进制的升级、幂等、空库三条路径的实测结果见 `rehearsal-evidence-2026-09-23.md`。
- 演练在 `yunyou-99` 的 `/tmp/gl-rehearsal` 完成，**生产数据与服务全程未被写入**。
