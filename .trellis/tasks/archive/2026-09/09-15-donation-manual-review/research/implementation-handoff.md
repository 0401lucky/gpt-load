# 实施交接（DeepSeek → Codex 审查）

- 日期：2026-09-15
- 任务：`.trellis/tasks/09-15-donation-manual-review`（保持 `in_progress`，等待 Codex 审查）
- 状态：**两仓库均已实现；验证部分完成**。SQLite / MySQL 8.4.11 / PostgreSQL 15.19 三库验证在 gpt-load 侧全部通过；new-api 侧 PostgreSQL 存在一处**未修复的真实缺陷**（见 §4.4），因此 **new-api 不能声称三库兼容**。

## 0. 阅读顺序

1. 本文件
2. [design.md](../design.md)（契约权威）
3. [wire-contract.md](wire-contract.md)（字段级冻结，本轮新增；§8.4 记录了两处跨端修正）
4. [prd.md](../prd.md)、[implement.md](../implement.md)

## 1. gpt-load（接收端）——已实现并完成三库验证

### 1.1 改动清单

新增：

| 文件 | 作用 |
| --- | --- |
| `internal/storage/migrations/0016_donation_manual_review.go` | 冻结迁移 0016：批次 `validation_mode`、条目人工投影字段、状态 CHECK 扩集、`donation_review_actions` 与 `donation_test_attempts` 两表；三引擎分支 |
| `internal/storage/migrations/0016_donation_manual_review_test.go` | 含真实旧捐献数据的升级 + 重复迁移 |
| `internal/control/donation_review.go` | review-context、`enter_review`/`approve`/`reject`、动作查询、幂等与业务拒绝台账 |
| `internal/control/donation_test_call.go` | 测试预约/租约、单 key 真实调用、终态持久化、冷对账清理 |
| `internal/control/donation_stream.go` | SSE 增量解码、字段白名单、跨片段秘密脱敏、有界输出 |
| `internal/control/donation_manual_review_test.go` | 11 个人工审核契约测试 |

修改：`internal/storage/models/donation.go`、`internal/control/{donation_batches,donation_catalog,donation_worker,donation_http,http_routes,server,service}.go`、`internal/parameteroverride/rules.go`（新增 `Rules.Fingerprint()`）、`internal/platform/errors/errors.go`、三份后端 locale、三语 README、`internal/storage/{migration.go,migration_test.go,db_test.go,database_integration_test.go}`、`internal/control/donation_database_integration_test.go`（迁移清单随 0016 同步）。

### 1.2 关键实现决定

1. **摘要兼容**：`DonationBatchRequest.ValidationMode` 带 `omitempty`，auto（缺省或显式）序列化后不出现该键，旧 `donation-batch/v1` 摘要逐字节不变；人工走 `donation-batch-manual/v1` 独立域。
2. **item_revision**：人工批次暂存即 1，legacy auto 保持 0；等值比较 + 效果递增，旧 0 版本可显式转审，已版本化项不能被 0/低版本覆盖。
3. **`staging_expires_at_ms`**：复用既有 `expires_at_ms` 作为唯一暂存截止并在回执中导出，不新增重复列。**这是本轮唯一的语义判断，请重点确认。**
4. **备注**：自由文本只落在 `donation_review_actions.note`；`items.reason_code` 保持闭集。
5. **测试虚拟凭据 ID**：`^uint(0)/2 - attemptID`，与正式 ID、自动 probe ID（`^uint(0) - itemID`）可证明不相交。
6. **脱敏**：`donationRedactor` 恒定回退 `maxSecret-1` 字节再输出，覆盖跨网络块/跨 SSE 帧/跨 delta 的回显。
7. **冷热分离**：`pending_review` 不进 `DrainDonationWork`；`ReconcileDonationReviews` 每 60 秒只清理，不 probe。

### 1.3 本轮修复的真实缺陷

1. `app_errors.ParseDBError(nil)` 返回**带类型的 nil** `*APIError`，被当作非 nil 返回后调用 `Error()` 会 panic，导致测试终态写入静默失败。已改为显式判空并把写入失败上报日志（租约仍是耐久兜底）。
2. 非流式测试响应因 `json:"-"` 丢失回复正文，已改为 `omitempty` 并且元数据查询不填充。
3. **只有真实数据库才能暴露的问题（MySQL/PostgreSQL）**：`Migrator().AddColumn` 不带出列的 CHECK 子句，导致 `chk_donation_item_revision`／`chk_donation_item_review_decision` 从未创建；已改为显式 `ALTER TABLE ADD CONSTRAINT`。
4. 三引擎的 CHECK 文本渲染差异：MySQL 返回 `_utf8mb4\'queued\'`（带字符集引导符与**反斜杠转义引号**），PostgreSQL 把 `IN (...)` 重写为 `= ANY (ARRAY[...])` 并追加 `::character varying`。原先的整串比对在真实引擎上必然失败，已改为可移植的**状态字面量逐一存在性**校验。

### 1.4 已执行的验证（本机 Windows / Go 1.27 / Docker）

容器：`gl-verify-mysql`（MySQL **8.4.11**，127.0.0.1:13306）、`gl-verify-pg`（PostgreSQL **15.19**，127.0.0.1:15433）。**两个容器仍在运行**，便于 Codex 复跑。

```
go build ./...                                                    → 通过
go vet ./internal/control/ ./internal/platform/... ./internal/storage/... → 通过
go test ./internal/control/ -run 'TestDonation' -count=1          → ok
go test ./internal/control/ ./internal/storage/... ./internal/platform/... ./internal/parameteroverride/ -count=1
                                                                  → 除既有 authkey 失败外全部 ok
go test ./internal/storage/migrations/ -run 'TestDonationManualReviewMigrationUpgradesLegacyHistory' -v
                                                                  → PASS

GPT_LOAD_DATABASE_TEST_DSN='mysql://root:verifypass@127.0.0.1:13306/gptload'
  go test ./internal/control -run '^TestExternalDatabase' -count=1  → ok 48.2s
  go test ./internal/storage -run '^TestExternalDatabase' -count=1  → ok 254.6s
GPT_LOAD_DATABASE_TEST_DSN='postgres://postgres:verifypass@127.0.0.1:15433/gptload'
  go test ./internal/control -run '^TestExternalDatabase' -count=1  → ok 22.3s
  go test ./internal/storage -run '^TestExternalDatabase' -count=1  → ok 19.9s
```

含新建、含真实旧捐献数据的升级、重复迁移、并发迁移、生命周期、独立连接竞争。

### 1.5 gpt-load 新增测试覆盖（`donation_manual_review_test.go`）

| 测试 | 验收点 |
| --- | --- |
| `...AdvertiseManualReviewAndKeepAutoDigestFrozen` | AC1：能力协商、原 limits 不变、auto 摘要冻结、人工域独立 |
| `...ManualBatchStagesWithoutProbeOrPoolEntry` | AC2：只加密暂存，无 probe、无凭据、无资源取得 |
| `...ManualApproveAcceptsOnceAndIgnoresLateDecisions` | AC2/AC5：批准一次入库；重放同结果；迟到相反决定被拒 |
| `...ManualRejectClearsStagingAndKeepsHistory` | AC2/AC6：清密文、闭环 reason、保留批次与截止、不建凭据 |
| `...ReviewRefusesStaleRevisionAndChangedTarget` | AC11 |
| `...EnterReviewPreservesLegacyAutoHistory` | AC9：保留批次/摘要/Attempts/截止；旧 worker 不再回收 |
| `...ExpiredReviewCannotBeApproved` | AC10 |
| `...TestUsesOnlyTheStagedKeyAndReplaysWithoutSecondCall` | AC3/AC4：只用本条目 key、可停重放、元数据不含正文 |
| `...TestFailsClosedWhenNoChatModelIsSupported` | 无模型不上上游 |
| `...StreamRedactsKeyEchoedAcrossChunks` | AC7/AC8 |
| `...LeaseBlocksConcurrentReviews` | AC5/AC9：废弃租约收敛 `interrupted`，不重试 |

### 1.6 gpt-load 未执行 / 未验证

1. **`make check`**：未执行。POSIX Makefile + `gofmt -l .` 门禁；本机 `core.autocrlf=true` 使工作区全为 CRLF，`gofmt -l .` 会列出所有文件（与本次改动无关）。已对全部新增/修改文件做 LF 归一后单独 `gofmt -l`，结果干净；完整门禁需 Linux 环境。
2. **MySQL 5.7 / 8.0.16–8.0.18 分支未实测**：后者走 `DROP CHECK` 语法分支，本机只有 8.4（走 `DROP CONSTRAINT`）。另注意 MySQL 5.7 会**静默忽略** CHECK 约束——这是 0015 起就存在的项目级前提，非本次引入。
3. **两服务真实进程联调未执行**（见 §4.5）。

### 1.7 需要 Codex 重点检查

1. `0016` 的 SQLite 表重建（列定义须先于表约束；索引/触发器/`sqlite_sequence` 恢复）。
2. `donationManualRevision` 的签名范围（故意不含组名）。
3. `donationRedactor.drain()` 在匹配跨越 `limit` 时的剩余缓冲处理。
4. `ApplyDonationReviewAction` 的事务边界与「先在事务外发上游、再短 cleanup 写终态」的顺序。
5. 人工 `approve` 与 `completeDonationProbe` 正式接收路径的一致性，以及「未伪造 probe passed」。
6. §1.2.3 的 `staging_expires_at_ms` 语义判断。

## 2. new-api 后端——已实现

新增 `model/donation_review.go`、`service/donation_review.go`；修改 `model/{donation,donation_batch,donation_reward}.go`、`service/{donation,donation_client}.go`、`controller/donation.go`、`router/donation-router.go`、`service/authz/resources_donation.go` 及对应测试。

要点：模式枚举与增量回填、`donation_records.review`/`.test` 权限、pending 意图 + 原 UUID 对账、受控测试代理（含跨分片脱敏与协议白名单）、发奖双重条件、本人可见 `review_note` 投影、冷/热队列拆分、`Summary.PendingReview/Rejected`。

**实施者自述的已知取舍（请 Codex 复核）**：本地 `expected_item_revision` 不匹配时直接 409 而不落动作行；idle/first-byte 超时未在 new-api 侧独立强制；未改后端 locale（现有 donation 接口本就全为硬编码英文，新增键会成为死键）。

## 3. new-api 前端——已实现

新增 `components/record-review.tsx`、`components/record-test.tsx`、`hooks/use-donation-test.ts`、`__tests__/review.test.tsx`；修改 21 个文件（含七语 locale）。

实施者报告的命令与结论：`bun run typecheck` 通过；`bunx vitest run src/features/donations/__tests__` → 5 files / 39 tests passed；`oxlint` 0 error；`oxfmt --check` 通过；`bun run build:check` EXIT=0。**这些结论由实施者报告，本轮未由我复跑。**

## 4. 验证结论汇总

### 4.1 通过

| 范围 | 结果 |
| --- | --- |
| gpt-load `go build` / `go vet` / 受影响包单测 | 通过 |
| gpt-load SQLite 全量（含 0016 升级 + 重复迁移） | 通过 |
| gpt-load MySQL 8.4.11 外部库（control + storage） | 通过 |
| gpt-load PostgreSQL 15.19 外部库（control + storage） | 通过 |
| new-api `go build` / `TestDonation`（model+service）/ `service/authz` | 通过 |
| new-api SQLite + MySQL 8.4.11 的 `TestDonationDatabase` | 通过 |

### 4.2 既有失败（与本任务无关，已用干净基线对照证明）

`go test ./...` 在本机另有多处失败，均在**不含本次改动的 HEAD 干净工作树**（`git worktree add --detach HEAD`）上同样失败：`internal/catalog`（2 例）、`internal/container`（1 例）、`internal/gateway`（2 例）、`internal/platform/authkey`（1 例）、`internal/installer`（setup 失败）。按要求未做顺带修改。

### 4.3 未执行

- `make check`（Linux 门禁，原因见 §1.6.1）。
- 两服务真实进程联调 `TestDonationIntegrationRealServices`（需两端隔离构建产物）。
- 本地浏览器界面验收（管理员 / 普通用户 / 只读管理员三种权限）。
- new-api 前端命令本轮未由我复跑（见 §3）。

### 4.4 ❗ new-api 在 PostgreSQL 上的未修复缺陷

`TEST_POSTGRES_DSN` 下 `TestDonationDatabase/postgres`（upgrade_false 与 upgrade_true 两个子用例）失败：

```
model/donation_test.go:105 / :431
Should be empty, but was [
  CREATE UNIQUE INDEX IF NOT EXISTS "idx_donation_review_actor" ON "dv_XXXXXXXX_donation_review_actions" ("actor_id","action_id")
  CREATE UNIQUE INDEX IF NOT EXISTS "idx_donation_test_actor"  ON "dv_XXXXXXXX_donation_test_attempts"  ("actor_id","test_id")
]
```

即第二次 `MigrateDonations` 仍会重发这两条 DDL。已定位到的事实：

- 两个索引在模型标签中带**显式名字**（`uniqueIndex:idx_donation_review_actor`），因此不受 `NamingStrategy.TablePrefix` 影响，在 PostgreSQL 中成为**schema 全局、不带前缀**的索引名；同 schema 内多个带前缀的测试表共用同一索引名，`CREATE ... IF NOT EXISTS` 对其余表是 no-op。
- 实测 `Migrator().HasIndex` 对这两个名字返回 `false`，而对结构相同的既有 `idx_donation_user_request`、`idx_donation_retry_request` 返回 `true`；`ParseIndexes()` 的键名两侧一致。
- 因此这是一个**测试可复现、根因尚未完全确定**的问题。因为语句带 `IF NOT EXISTS`，实际影响是每次启动重发一次无效 DDL，不是数据损坏；但它**破坏了「第二次迁移 schema 稳定」这一验收要求**。
- **已尝试但未完成**：多次探针定位 GORM 名称解析差异后中止，避免在未确认根因前改动索引定义造成更坏后果（例如误删/重建索引）。**建议由 Codex 接续定位**，最小线索是「显式命名的复合唯一索引 + TablePrefix」这一组合。

SQLite 与 MySQL 的同一断言通过。

### 4.5 跨端一致性（已按真实代码核对）

- gpt-load `review-context` 的 `state`、`effective_mode`、`unavailable_reason`、`test_models` 均落在 new-api 的校验闭集内；本轮据此修正了两处：
  1. 不可审核分支原先返回契约外的 `not_reviewable` → 已改为 `item_state`（`not_reviewable` 仅保留在**动作拒绝**闭集中，那里是合法的）。
  2. `effective_mode` 在「旧 auto 项可转审」时原先返回 `auto`，会被 new-api 判为 `invalid_response`，导致转审入口不可用 → 现改为**可审核时返回本审核所依据的模式 `manual_review`**，并新增 `review_action` 字段（`enter_review`/`approve`）显式表达当前可用动作；条目**存储态与回执仍如实返回 `auto`**。这是跨端契约的实质修正，请 Codex 确认语义是否可接受。
- 业务拒绝一律 HTTP 200 + `outcome:"rejected"`，不使用 409 表达业务拒绝（409 会被 new-api 视为「未执行」并保留 pending 意图重放）。**唯一例外**：测试接口的 `ErrDonationTestUnavailable/Conflict/Busy` 仍返回 409，因为那时确实没有发起上游调用；请确认这是否与 new-api 的测试重试预期一致。
- 回执 item 字段（`item_revision`/`effective_mode`/`review_target_revision`/`review_action_id`/`review_decision`/`reviewed_at_ms`/`staging_expires_at_ms`）与 `review_limits` 双端一致。

## 5. 剩余问题汇总

1. **new-api PostgreSQL 缺陷（§4.4）**——阻塞 new-api 的三库声明。
2. **两服务真实进程联调与浏览器验收未执行**（§4.3）。
3. **`make check` 未执行**（§1.6.1）。
4. **测试接口的 409 语义**待确认（§4.5）。
5. `ParseDBError` 的 typed-nil 陷阱本轮只修了 gpt-load 自身路径，建议排查 new-api 是否有同类写法。

## 6. 边界声明

- 未连接或修改生产、未使用真实上游 key、未部署、未 commit、未 push。
- 未归档任务、未执行 `task.py archive`/`finish`、未标记 completed、未清空任务指针。
- 保留 `gpt-load/.playwright-mcp/` 与 new-api 既有 `clover-*.png`，未回滚他人改动。
- 为三库验证启动的两个本地容器 `gl-verify-mysql`、`gl-verify-pg` 仍在运行，供 Codex 复跑；不需要时可 `docker rm -f` 删除。
