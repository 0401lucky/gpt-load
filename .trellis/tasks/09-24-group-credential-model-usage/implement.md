# 执行计划：按 (凭据, 上游模型) 的额度窗口内 token 用量矩阵

## 前置

- **依赖边界**：新增代码落在 `internal/state`（周期状态）、`internal/requestlog` 或聚合所在层、`internal/control`（只读接口）、`web/`。**不得**让 `internal/health` / `internal/scheduler` / `internal/telemetry` import `storage`/`control`/`gorm`（`dependency_test.go` 会红）。
- **改动前的固定动作**：grep 每个新键名与字段名（接口路径、JSON 字段、`ModelCycleStarts`/`ModelNextResets`、frontend query key），确认无重名（`.trellis/spec/guides/index.md` 的 Pre-Modification Rule）。
- **本任务不新增数据库迁移**。若实现中发现确实需要新表，**停下来回报**，不要自行加迁移 —— 本项目的迁移编号策略有额外约定。
- 复现基线：`internal/state/model_cooldown.go`、`internal/state/runtime_checkpoint.go`、`internal/requestlog/credential_window_usage.go`（**只作参考，不复用其单凭据单窗口形态**）、`internal/control/credentials.go`。

## 实施清单（按序）

1. **周期状态机（先写测试）**
   - `internal/state/`：`CredentialEntry` 新增 `ModelCycleStarts` / `ModelNextResets`；在 `SetModelCooldown` 的路径上按 design 3.1 的规则维护。
   - 新增清理函数（独立于 `pruneModelCooldowns`，**不要复用**它，否则锚点在冷却过期瞬间被删）。
   - 单测覆盖：首次冷却、连续两次冷却导致 `CycleStart` 迁移、冷却未过期时不迁移、清理规则、上限有界。
   - 验证：`go test ./internal/state/ -count=1`。

2. **检查点持久化**
   - `internal/state/runtime_checkpoint.go`：新增两个可选字段的保存与恢复；**恢复路径不得 pruned 掉锚点**。
   - 单测：保存 → 恢复后锚点保持；旧格式检查点（缺字段）按空处理不报错。
   - 验证：`go test ./internal/state/ -count=1`。

3. **聚合查询**
   - 实现 design 3.4：**一次**取回该组窗口内的 `usage_stats` 行，在 Go 里按 `(credential_id, model)` 归并；`window_start` 按 3.2 三态计算；整点对齐。
   - `total_tokens` **复用 `internal/usage` 的既有求和函数**，不要另写一份。
   - 禁止逐行发 SQL（N+1）。
   - 单测：三态口径各一例、整点对齐、行集筛选（无用量且未冷却不下发）、排序与截断。
   - 验证：`go test ./internal/control/ ./internal/requestlog/ -count=1`。

4. **控制面接口**
   - `GET /api/groups/:id/model-usage`，响应字段严格按 design 3.3；错误走既有 `writeServiceError` 出口。
   - 契约测试：字段集合与两套前端投影白名单一一对应（表驱动断言，防止漏登记）。
   - 验证：`go test ./internal/control/ -count=1`。

5. **modern 前端**
   - 资源层新增取数 + projector（**禁止 `as` 断言**）；query key 进 `query-keys.ts`。
   - 新增矩阵组件（`features/groups/`），实现排序、筛选、按模型折叠；口径列区分 `reset` / `fallback_24h`。
   - i18n 三语言同步。
   - 验证：`pnpm --dir web run lint` / `type-check` / `build`。

6. **classic 前端**
   - 投影白名单登记**每一个**响应字段（漏一个该区块整页报错）；DTO 同步。
   - 新增区块组件，与 modern 同构；i18n 三语言同步。
   - 验证：`pnpm --dir web run lint` / `type-check` / `build`。

7. **门禁与回归**
   - `make check`（本机无 `make`，按既有记忆逐步独立执行九条 recipe）。
   - 全量 `go test -count=1 . ./internal/...` 与未改动的基线 commit 做失败集合对照，证明新增失败为 0。
   - 本机为 Windows/CRLF 检出，`gofmt`/`prettier`/`tidy` 的失败先判定行尾假阳性，不凭本地结果改文件。

## 风险与回滚点

- **高危点 1：classic 投影白名单漏登记** → 分组详情页整页报错。实现后必须逐字段核对白名单，并用契约测试固定字段集合。
- **高危点 2：检查点恢复路径沿用 `pruneModelCooldowns`** → 锚点在冷却过期瞬间被删，全表退化为「近 24h」，功能静默失效。必须在单测里断言「冷却过期后锚点仍在」。
- **高危点 3：窗口起点按行计算被写成按组计算** → 所有行共用同一个窗口，数字失真但不报错。用「同一组内两个不同重置时刻的组合」做断言。
- **高危点 4：`usage_stats` 检索范围按 `now-24h` 截断，而某行 `window_start` 早于该范围** → 该行用量被系统性低估。需要按「本组最早 `window_start`」确定检索下界。
- **降级**：检查点丢失（崩溃）→ 锚点丢失 → 全表退化为「近 24h」口径。已写入 design §5，属可接受降级。
- **代码回滚**：无迁移，`git revert` 或换回旧镜像即可；**接口与前端必须整体回滚**（只回滚一半会让新区块遇到未知/缺失字段）。

## 交付后（不在本任务内执行）

- 由用户决定何时部署到 `yunyou-99` 的 gptload2；本任务不含服务器操作与生产配置变更。
- 部署后按 design §8 做只读核对：新接口结果与直接查库的 `usage_stats` 聚合逐项对齐。
