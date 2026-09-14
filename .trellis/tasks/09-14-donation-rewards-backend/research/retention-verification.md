# 备份恢复与永久额度保留补充验证

2026-09-14，父任务收尾核对。新增代码仅为 new-api 的 `model/donation_retention_test.go`，没有修改后端生产源码。

## 真实 SQLite 备份恢复

`TestDonationSQLiteBackupRetainsIdentityAndRewards` 使用 SQLite **3.50.4**，先通过真实模型事务接收并结算一笔固定奖励，再执行 SQLite `VACUUM INTO`，将包含已提交 WAL 数据的独立备份复制到另一临时目录，以独立数据库连接重新迁移和打开 DonationStore。

断言恢复后 key 指纹一致、连接凭据仍能解密、原用户/明细/远端凭据/永久奖励可查询；修改恢复库内的连接地址和 token 后再次打开 Store，另一用户提交同 key 仍为 duplicate。原明细重奖返回 false，余额保持125，奖励流水仍为1。没有通过内存复用或重建空账本代替实际备份恢复。

范围是包含独立秘密材料及业务表的完整本地主库备份；不表示已演练生产两端备份编排、灾备系统或线上凭据变更。

## 签到过期与记录清理

`TestDonationPermanentRewardSurvivesCheckinRetention` 在同版本真实 SQLite 上，将隔离夹具的 Checkin 表映射为既有固定表名，设置已过期的合成签到记录，然后用真实捐献事务发放永久额度。

实际调用 `GetActiveTemporaryQuota`，过期桶被排除且桶内原余额9不变。随后仅删除该合成用户的过期临时桶，永久钱包仍为125、捐献流水仍为25，重放不再次发奖。使用历史时间数据，不修改系统时钟。当前仓库按查询时间过滤过期桶，不依赖一个独立的签到清理定时任务；测试验证了该真实读取路径和记录物理清理后的账本保留。

## 命令与结果

工作目录均为 `D:/code/Claude code program/new-api`，`GOFLAGS=-p=2`、`GOMAXPROCS=2`。

- `go test ./model -run '^TestDonation(SQLiteBackup|PermanentReward)' -count=1 -v`：两项通过，package 0.533s，退出码0；日志 [database-retention.log](database-retention.log)。
- `go test ./model -count=1`：model 全包通过，8.539s，退出码0；日志 [model-retention-regression.log](model-retention-regression.log)。
- `go vet ./model`：退出码0。
- 新增文件已 `gofmt`。没有重跑已完成的网络数据库矩阵，也没有将本轮未提供 DSN 的 skip 记为 MySQL/PostgreSQL 验收；其真实版本与通过证据沿用 [check-handoff.md](check-handoff.md)。
