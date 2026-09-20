# 执行计划：合并上游并重编号 donation 迁移

> 执行前置：`git status` 干净（仅允许 `.playwright-mcp/` 未跟踪）；已切到 `main`。
> 每个阶段结束都是一个可回退的检查点。

## 阶段 0：准备与基线

- [ ] 0.1 记录基线
  ```bash
  git log --oneline -1                      # 期望 56c19ba6
  git rev-parse upstream/main               # 期望 b211e7c8
  ```
- [ ] 0.2 建工作分支，避免直接污染 `main`
  ```bash
  git switch -c merge/upstream-20260920
  ```
- [ ] 0.3 记录升级前测试基线（确认既有测试本来就是绿的）
  ```bash
  go build ./... && go test ./internal/storage/... ./internal/control/...
  ```
  **验证**：全绿。若有既存失败，先记录、不得归因于本次改动。

## 阶段 1：合并上游

- [ ] 1.1 执行合并（预期 4 个文件冲突）
  ```bash
  git merge upstream/main
  ```
  **验证**：冲突文件恰为 `internal/storage/{migration.go,migration_test.go,db_test.go,database_integration_test.go}`。
- [ ] 1.2 解决 4 处文本冲突：以上游 0015/0016/0017 为准，本地 donation 条目**暂不恢复**（留待阶段 2 以新编号写入）。
- [ ] 1.3 确认自动合并文件无异常
  ```bash
  git diff --stat HEAD~1 2>/dev/null || git status --short
  ```
  重点核对 `internal/control/http_routes.go`、三语 README、`Dockerfile`、`.github/workflows/release.yml`。
- [ ] 1.4 提交合并结果（**先不合入 donation 条目，允许此刻编译失败**，作为阶段性检查点）
  ```bash
  git add -A && git commit -m "merge: 合并上游 main 至 b211e7c8"
  ```

## 阶段 2：迁移重编号

- [ ] 2.1 重命名文件
  ```bash
  git mv internal/storage/migrations/0015_donation_intake.go         internal/storage/migrations/0018_donation_intake.go
  git mv internal/storage/migrations/0016_donation_manual_review.go  internal/storage/migrations/0019_donation_manual_review.go
  git mv internal/storage/migrations/0016_donation_manual_review_test.go internal/storage/migrations/0019_donation_manual_review_test.go
  ```
- [ ] 2.2 替换包内标识符：`0015`→`0018`、`0016`→`0019`（含 `ID00NN`/`Up00NN`/`Validate*`/`SchemaModels*`/`TableNames*` 与约 40 处私有符号、临时表名 `donation_items__0016`）
- [ ] 2.3 修正常量值字符串：`ID0018 = "0018_donation_intake"`、`ID0019 = "0019_donation_manual_review"`
- [ ] 2.4 残留复查
  ```bash
  grep -n "0015\|0016" internal/storage/migrations/0018_donation_intake.go internal/storage/migrations/0019_donation_manual_review.go
  ```
  **验证**：残留项均为有意保留或已解释，无遗漏标识符。
- [ ] 2.5 注册表与引用点同步（6 个文件，见 `design.md` §1.4）
- [ ] 2.6 编译门禁
  ```bash
  go build ./... && go vet ./...
  ```
  **验证**：无重复声明错误，无未定义符号。**此门禁不过则不得进入阶段 3。**

## 阶段 3：迁移兼容性演练（核心验证）

> 靶子状态：既有库账本 = 16 条（`0001`–`0014` + 旧 donation 两条）。

- [ ] 3.1 记录靶子升级前基线（表计数 + 身份 UUID）
- [ ] 3.2 **PostgreSQL** 演练（`gl-verify-pg`，库 `gptload`，用户 `postgres`）
  ```sql
  DELETE FROM schema_migrations WHERE id IN ('0015_donation_intake','0016_donation_manual_review');
  ```
  跑新代码迁移 → 断言账本 19 条且顺序正确 → 断言数据零丢失。
- [ ] 3.3 **MySQL** 演练（`gl-verify-mysql`，库 `gptload`，root/`verifypass`）
  同 3.2。
- [ ] 3.4 **SQLite** 演练（与生产同构，最高优先级）
  构造 16 条账本的 SQLite 库 → 同 3.2 流程。
- [ ] 3.5 幂等反证（AC5）：对已升级完成的库**再启动一次**，断言迁移不报错、数据不被改写。
- [ ] 3.6 生产副本预演：把生产库的 `/tmp` 副本（只读排查时的做法）升级一遍，比对 `design.md` §3.2 的计数与身份。**全程不触碰生产卷。**
- [ ] 3.7 记录演练证据到 `research/` 目录。

**门禁**：3.2–3.4 三库全绿 + 3.5 幂等成立，方可继续。任一失败 → 回到 `design.md` §2 重新论证。

## 阶段 4：全量回归

- [ ] 4.1 donation 既有测试
  ```bash
  go test ./internal/control/ -run 'Donation' -count=1
  go test ./internal/storage/... -count=1
  ```
- [ ] 4.2 合并副作用排查
  ```bash
  go test ./internal/webui/ -count=1
  ```
  重点：上游 `workflow_self_hosted_test.go` 与本地 `workflow_main_images_test.go` 断言是否互斥。
- [ ] 4.3 全量门禁
  ```bash
  make check
  ```
  **验证**：gofmt / `go mod tidy -diff` / vet / 前端 lint+format+build / build / 全量 test / `git diff --check` 全绿。

## 阶段 5：交付镜像

- [ ] 5.1 合回 `main` 并推送
  ```bash
  git switch main && git merge --no-ff merge/upstream-20260920 && git push origin main
  ```
- [ ] 5.2 观察 `ghcr-main.yml` 三段流水线（build → verify → promote）
  ```bash
  gh run list --workflow=ghcr-main.yml --limit 3
  gh run watch <run-id>
  ```
  **验证**：三段全绿，promote 产出 `:main` / `:latest` 滚动 tag。
- [ ] 5.3 核对镜像可匿名拉取且版本号指向本次合并提交

## 阶段 6：升级手册

- [ ] 6.1 编写生产升级手册，至少含：
  - 前置备份命令（**须覆盖 `gpt-load.db` + WAL/SHM + `encryption.key`**）
  - 停机方式（`stop` 而非 `restart`，确保 WAL 落盘）
  - 账本修复 SQL
  - 升级与端到端验证命令
  - L1/L2/L3 三级回滚点，并**显式说明 L1 必须与卷快照配套**
- [ ] 6.2 手册中的每条命令在本地演练环境验证可用（不照抄未验证的命令）

## 回退点

| 位置 | 回退动作 |
|---|---|
| 阶段 1 后 | `git merge --abort` 或 `git reset --hard 56c19ba6` |
| 阶段 2 后 | `git reset --hard <阶段 1 提交>` |
| 阶段 3 失败 | 靶子库均为验证环境，可直接重建；不涉及生产 |
| 阶段 5 后 | 镜像层不删旧 digest，`rollback-<date>` tag 保留；生产升级未执行，随时可停 |

## 完成判据

`AC1`–`AC8` 全部满足，且阶段 3 的三库演练证据已落盘 `research/`。
