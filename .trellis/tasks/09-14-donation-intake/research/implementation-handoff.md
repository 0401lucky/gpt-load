# gpt-load 捐献接收实现交接

实现日期：2026-09-14。本文件记录 T1 的代码契约；父任务与状态清单由主会话维护。代码位于 gpt-load，未修改 new-api、用户截图或生产配置，未提交 Git commit。

## 配置与身份

- 新增静态配置 `DONATION_INTEGRATION_TOKEN`；环境变量和既有 `.env` 加载方式均可使用。默认空值关闭集成 HTTP 入口和捐献 worker，不回退为管理认证。
- 非空 token 必须是 32–256 字节的可见 ASCII，并与 `AUTH_KEY` 不同。启动检查和 AccessKey 凭据准备路径阻止 token 与数据面 AccessKey 碰撞。变更后重启。
- 数据库 `donation_identities` 持久保存实例 UUID、来源 UUID 和 HMAC 密钥校验值。来源由已认证的服务端决定，HTTP 请求不能自报来源。token 或 URL 变化不改变这两个 UUID。
- 指纹使用现有加密服务的独立 HMAC 能力，输入域为 `gpt-load/donation-resource/v1` 加规范化 key；不含组、渠道、用户或请求 ID。加密主密钥变化导致校验值不匹配时拒绝继续使用集成，避免历史去重悄悄失效。
- `.env.example` 和三份 README 已同步配置及运行说明。

## 远端接口的实际形状

独立 namespace：`/integrations/donations/v1`；`OwnerDonation` 配 `AuthDonation`，在统一路由注册器和 container 装配。集成 token 不能认证普通 `/api` 路由。所有接口禁止额外查询参数，响应设置 `Cache-Control: no-store`，保持 gpt-load 的 `{code:0,message,data}` / 字符串错误码包装。

`GET /capabilities` 的 data：

```json
{
  "protocol_version": "1",
  "instance_id": "数据库持久 UUID v4",
  "source_id": "数据库持久 UUID v4",
  "input_types": ["api_key"],
  "limits": {
    "max_items": 100,
    "max_key_bytes": 4096,
    "max_body_bytes": 1048576,
    "staging_retention_seconds": 604800
  }
}
```

- `GET /groups` 的 data 是数组，字段为 `id/name/channel_id/connection_type/enabled/can_probe/target_revision/unavailable_reason`。不返回 params 或任何凭据。
- `POST /batches` 请求保持父契约的 `batch_id/group_id/target_revision/items[{item_id,key}]`；`Idempotency-Key` 必须等于 `batch_id`，使用规范的小写 UUID v4。
- `GET /batches/:batch_id` 按认证来源读取原批次；不推动校验或重试。
- `POST /batches/:batch_id/retry` 是父契约补足的恢复入口。请求 `{item_ids?: string[]}`；省略或空数组选择本批次所有可重试项，携带新的 UUID v4 `Idempotency-Key`。来源、动作键和请求摘要持久去重，重放旧动作不能重置后来消耗的预算。

批次 data 包含 `batch_id/group_id/target_revision/created_at_ms/state/items`。批次 `state` 为 `processing` 或 `completed`；包含 `retry_pending` 的批次仍为 `processing`，调用方应依据逐项状态展示。

逐项 data：`item_id/state/reason_code/credential_id/accepted_at_ms/retryable`。只有 `accepted` 项才返回非空凭据 ID 与接收时间；其他状态返回 null，特别是 `existing` 不透露其他来源的原凭据关联。新接口按 item_id 关联，调用方不应依赖数组顺序。

结构错误、重复 item_id、非法 UUID 为请求错误；单项 key 格式错误独立成为 `invalid`。同批相同 key 的后续行是 `existing/duplicate_item`。单批数量与 HTTP 报文边界是工程限制，不累计用户或活动领奖数量。

## 校验、接收与恢复

1. 初次受理核对目标 revision；保存原组名称、渠道和目标 revision 快照。所有合格格式条目先用现有 AES-GCM 加密暂存，事务提交后才返回已受理。
2. worker 每秒或受理唤醒处理最多 32 个候选，逐条独立执行。网络探测不持数据库事务或控制面写锁。
3. 通过 `credentialProbeExecutor` 向生产 Executor 传入唯一的暂存密文 CredentialRef。暂存不进入 CredentialRegistry，探测不调用调度器，也不切换库存 key。测试用高位临时凭据 ID 避免 SDK 缓存与正式凭据冲突，调用后退休相关临时 SDK 状态。
4. 目标使用现有 `buildGroupValidationTarget` 的协议、模型回退、渠道、上游配置和代理/请求头。空库存组可接收；禁用、删除、订阅/非普通 key、无法形成 probe、静态认证覆盖的组不可接收。
5. 初次请求 revision 不符返回 `DONATION_TARGET_CHANGED`。暂存后按同一组 ID 的当前有效配置重新解析 probe，提交前再次比对实际 probe revision；期间变化变为 `retry_pending/target_changed`，重新校验后才能接收。组改名不改变 revision。
6. 凭据、全局资源 acquisition 和明细 `committing` 在同一控制事务提交。完成运行时恢复和发布后才持久标记 `accepted`。
7. 重启或提交后响应丢失时，`committing` 只恢复运行时及原回执，不再探测、插入凭据或更换明细 ID。`accepted_at_ms` 保持原事务中的接收时间。

每轮最多 5 次探测，单次上下文上限 30 秒，租约 90 秒，退避为 1/2/4/8/16 秒。租约 token 对完成写入做代次隔离；过期不会自行释放已接收资格，也不能让旧 worker 覆盖新代次结果。

自动次数耗尽保存 `retry_pending/retry_exhausted` 和暂存密文；显式 retry 能在保留期内重新排队。已接收、提交中或确定无效项不因 retry 重跑。未接收项目的密文在受理七天后由 worker 清理为 `invalid/staging_expired`；关闭 worker 时保留数据，重新启用后继续检查过期。来源、请求摘要、明细和资源历史不清理。

## 数据与竞争语义

迁移：`0015_donation_intake`。使用迁移局部冻结模型并注册正常、当前与中断恢复校验；新增五表：

- `donation_identities`：稳定实例/来源与 HMAC 密钥校验。
- `donation_batches`：来源 + batch_id 唯一，原请求摘要和目标快照。
- `donation_items`：来源 + item_id 唯一，密文、状态、租约、重试预算与最终回执。
- `donation_resources`：全局 key 指纹主键、当前未决处理归属与永久首次 acquisition。
- `donation_retries`：来源 +动作幂等键唯一及请求摘要。

这些表没有指向组或正式凭据的级联外键。删除组、凭据以及压缩 ControlOperation 不清除原 acquisition 或来源。

普通管理导入也在原控制事务内锁定和登记全局资源指纹；已有库存保持 `origin=inventory`，不伪造捐献来源。为避免锁顺序造成问题，资源锁按指纹排序，正式凭据插入顺序保持原管理契约。启动 `EnsureInitialState` 回填当前所有普通 key 库存，使用 GORM 子查询兼容 MySQL 的保留字引用。

资源行使用唯一插入和数据库行锁，不能仅靠进程内 mutex 防并发。无效、未接收且耗尽或过期的尝试可释放未决占用；非空 acquisition 永不释放，之后同 key 跨组、来源、批次或凭据删除仍是已有资源。

**MySQL 方言事实**：仓库强制 `clientFoundRows=true`。`ON DUPLICATE KEY UPDATE id=id` 的 RowsAffected=1 不证明本次插入；batch/retry 不以此作资格判断，而是普通 INSERT，唯一冲突先完整回滚，再 fresh read 核对实际持久摘要。这个路径也避免在 PostgreSQL 已失败事务里继续 SELECT。明细被其他批次使用属于不同冲突，会回滚新批次并返回 409。

## 验证记录

- 已通过 `go test ./internal/control ./internal/platform/httproute ./internal/storage/... -run 'TestDonation|TestMigrationRegistry' -count=1`，覆盖混合多行、加密暂存、实际 Gemini HTTP、来源隔离、拒绝凭据泄露、恢复、删除后历史、代次隔离、管理导入竞争和失败项重试。
- 实际 Gemini HTTP 测试使用生产 Bifrost Runtime 和本地 `httptest` Gemini 服务器；断言请求 key 和模型路径，错误 key 不由库存 key 代替通过，未连接真实平台或使用真实 key。
- 两个独立 SQLite 数据库连接/Service 的资源竞争测试通过，不只依赖共享 Service.writeMu。
- 最终 control/config/httproute/storage 及全部子包测试、对应 go vet、改动 Go 格式和 diff 检查均通过；独立检查记录在 database-verification.md。
- 真实 MySQL/PostgreSQL 的新建、升级、回填、强制并发批次、事务故障及历史验证由检查代理记录在同目录 `database-verification.md`；初次运行实际发现并推动修复了 MySQL 保留字 JOIN 和 clientFoundRows 两项问题。
- 主会话已在保留当前代码的 LF Linux 隔离工作树运行完整 `make check`，退出码0，全部门禁通过；日志 intake-make-check-linux.log。原工作区存在既有 CRLF/Windows工具与路径断言限制，未批量修改用户文件或全局 Git 配置。已核对通过快照中的全部36份变更 Go/配置文件与当前源码一致。

## 后续边界

- T2/new-api 必须严格解析 gpt-load 包装与状态；仅可信 accepted 回执触发其永久奖励事务，并持久保存 instance/source/batch/item 引用。
- 完整双服务恢复和永久余额验收归父任务，不能用本接收端 fake 或单服务测试替代。
- 加密密钥与数据库需要共同备份；未有历史记录的上线前已彻底删除 key 无法追溯，沿用父 PRD 的已知边界。
- 生产配置、真实分组和外部发布均未执行。
