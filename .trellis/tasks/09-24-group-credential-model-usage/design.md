# 技术设计：按 (凭据, 上游模型) 的额度窗口内 token 用量矩阵

## 1. 设计目标

1. **不重造用量记账**：`usage_stats` 已按 `(时间桶, 凭据, 模型)` 聚合了全部 token 字段，直接复用。
2. **不引入数据库迁移**：窗口锚点（每个 `(凭据, 模型)` 的重置时刻）落在既有运行时检查点里，而不是新表。
3. **不改变任何路由/选取行为**：本功能纯读，不参与调度决策。
4. **两套前端共用同一份数据**：一个只读接口，classic 与 modern 各自投影。

## 2. 落点与边界

### 2.1 数据层（只读，无迁移）

- **复用 `usage_stats`**（`internal/storage/models/request.go` 的 `UsageStat`）。唯一索引已含 `credential_id` 与 `model`，无需新增索引。
- **窗口锚点**：新增每凭据的 `ModelCycle` 记录（见 3.1），落在 `internal/state` 的运行时状态与检查点里，**不进数据库**。理由见 §6 权衡。

### 2.2 接口层

- 新增只读接口 `GET /api/groups/:id/model-usage`（控制面，`internal/control`）。
- 复用 `requestlog` 包做聚合，但**不能直接用 `QueryCredentialWindowUsage`**：它是**按单个凭据、单一时间窗**的，而矩阵每行有各自的窗口起点，且需要按模型拆分（见 3.3）。

### 2.3 前端

- modern：分组详情页新增矩阵区块。
- classic：分组详情页新增同构区块；**必须同步登记投影白名单与三语言文案**（classic 对白名单外字段硬失败）。
- 两侧共用同一个接口响应结构体 → 所有字段都必须同时被两套投影接受。

### 2.4 明确不碰

- `internal/health`（判定层）、`internal/execution`（执行器）、`internal/scheduler`（选取）—— 本功能不改任何判定或选取逻辑。
- `rate_limit_reset_hint_enabled` 的解析逻辑本身。
- `CredentialWindowUsage.vue`（订阅类渠道的百分比额度窗口）语义。

## 3. 数据流与契约

### 3.1 窗口锚点的状态模型（新增）

在 `internal/state` 的每凭据状态上，新增一个按模型的「周期记录」：

```go
// CredentialEntry 新增字段
ModelCycleStarts map[string]time.Time // 模型 → 当前周期的起点（上一次已发生的重置时刻）
ModelNextResets  map[string]time.Time // 模型 → 下一次重置时刻（来自最近一次 429 文案）
```

写入时机：**在设置模型冷却的那一刻**（`SetModelCooldown` 的调用方已经拿到 `until`，那就是上游给出的重置时刻）。

```
SetModelCooldown(ref, model, until, now):
    // until 之前的那次重置 = 本周期起点；若上次的 NextReset 已过期，它就成为新的 CycleStart
    if prev, ok := ModelNextResets[model]; ok && !prev.After(now) {
        ModelCycleStarts[model] = prev
    }
    ModelNextResets[model] = until
```

- **边界语义**：`Now < NextReset` → 本周期尚未结束；`Now >= NextReset` → 该重置时刻已发生，成为新的周期起点。
- **有界性**：两个 map 每个模型至多一条。清理规则（`pruneModelCycles`，与 `pruneModelCooldowns` **完全独立**）：某模型最近一次已知重置早于 `now - 48h` → 该模型的 `NextReset` 与 `CycleStart` 一起删除；只要该模型的 `NextReset` 仍然新鲜，其 `CycleStart` 即使超过 48h 也不清理（避免误伤长周期）。上限 = 凭据数 × 模型数。
- **持久化**：并入 `internal/state/runtime_checkpoint.go` 的既有检查点（与 `ModelCooldowns` 并列）。旧检查点缺这两个字段时按空处理 —— **格式向后兼容**。注意：当前检查点恢复路径会 `pruneModelCooldowns`，新字段**不得**沿用该 prune（否则锚点立即丢失），需独立清理。

### 3.2 窗口口径（R1 / R5 / R6）

设 `now` 为请求时刻：

| 情况 | `window_start` | `window_source` |
|---|---|---|
| 该模型有 `CycleStart`（上一次已发生的重置） | 该时刻 | `"reset"` |
| 该模型无 `CycleStart`，但 `NextReset <= now`（重置已发生） | `NextReset` | `"reset"` |
| 该模型**首次冷却、尚无任何已发生的重置**（只有 `NextReset > now`） | `now - 24h` | `"fallback_24h"`，**同时**下发 `cooldown_until_ms` |
| 该模型从未冷却过（无任何记录） | `now - 24h` | `"fallback_24h"` |

> 说明：首次冷却时不可能存在「上一周期起点」——`CycleStart` 只能由**第二次**冷却迁移产生。因此 AC4 所述「冷却中行的起点为上一周期起点」只在已有至少一次重置历史时成立；首次冷却期间该行按兜底口径展示，但**必须同时给出恢复时刻**，`cooldown_until_ms` 不为 null 即为该信号。

- **`window_source` 必须下发**（R6）：UI 用它区分「精确窗口」与「近 24h 兜底」，不允许同一列标题掩盖两种口径。
- **小时对齐**：`usage_stats` 是小时桶。为了让「窗口内已用」的取数可复现，**按整点对齐**：`window_start` 向上取整到整点、`now` 向下取整到整点。实际参与统计的区间随响应下发（见 3.3 的 `counted_from_ms` / `counted_to_ms`），UI 在 tooltip 里说明「按整点统计」。单侧误差 ≤ 1 小时，对日额度（百万 token 量级）约 ≤ 4%。
  - 不采用 `request_logs` 做边界精确校正：该表默认只留 7 天，且逐行校正会退化成 N 次查询（矩阵行数为数百量级）。现有 `QueryCredentialWindowUsage` 的边界合并策略是**单凭据单窗口**场景的优化，不适用于矩阵。

### 3.3 接口契约

```
GET /api/groups/:id/model-usage
```

- 仅接受一个整数组 id；无其他必需参数（排序/筛选在前端做，见 §4）。

响应：

```json
{
  "observed_at_ms": 1790239000000,
  "counted_from_ms": 1790197200000,
  "counted_to_ms": 1790236800000,
  "pagination": { "page": 1, "page_size": 500, "total_items": 116, "total_pages": 1 },
  "items": [
    {
      "credential_id": 523,
      "model": "z-ai/glm-5.3-flash",
      "window_start_ms": 1790196000000,
      "window_source": "reset",
      "cooldown_until_ms": 1790238956357,
      "request_count": 176, "success_count": 172, "failure_count": 4,
      "uncached_input_tokens": 3367595,
      "cache_read_tokens": 0, "cache_write_5m_tokens": 0,
      "cache_write_1h_tokens": 0, "cache_write_unknown_tokens": 0,
      "output_tokens": 60559,
      "total_tokens": 3428154
    }
  ]
}
```

- `model` 必须是**上游模型 ID**（与冷却键一致，如 `z-ai/glm-5.3-flash`），不是客户端模型名（R2）。
- `cooldown_until_ms` 为 `null` 表示未在冷却；数据源为既有 `ModelCooldowns`（仅活跃项）。
- `total_tokens` = 全部输入类 token（uncached + cache_read + cache_write_*）+ output，与既有 `usage` 包的口径一致，**不要另立算法**（复用 `internal/usage` 的既有求和函数）。
- 行集：该组**当前有效凭据 × 该凭据在窗口内有用量或处于冷却的模型**的并集。窗口内既无用量又未冷却的组合不下发（避免空行撑爆矩阵）。
- 分页/上限（实现定稿）：单响应行数上限 **500**；超出时按 `total_tokens` 降序 → `credential_id` 升序 → `model` 升序截断。`pagination` **恒存在**，为 `{page, page_size, total_items, total_pages}`，其中 `total_items` 是**截断前**的行数（前端据此显示「仅显示用量最高的 N 行，共 M 行」）。
- 路由内部参数名为 `:group_id`（`/api/groups/:group_id/model-usage`）：同一位置已有其他 `/api/groups/:group_id/...` 路由，通配名必须一致否则注册期冲突。**客户端可见路径不变**，仍为 `/api/groups/{id}/model-usage`。

### 3.4 查询实现

一次查询拿到整组数据，在 Go 里按行归并：

1. 用既有 `usage_stats` 读取路径，按 `group_id` + `bucket_start_ms ∈ [counted_from, counted_to)` 一次取回全部行（该表已按 `(credential_id, bucket_start_ms)` 建索引）。cline 组 24h 约 2800 行量级，可接受。
2. 从 `state` 取该组的 `ModelCycleStarts` / `ModelNextResets` / `ModelCooldowns`。
3. 逐 `(credential_id, model)` 归并：`window_start` 按 3.2 计算，再对 `bucket_start_ms >= alignUp(window_start)` 的桶求和。
4. 组装响应并截断。

**不得**逐行发 SQL（N+1）。

## 4. 前端设计

### 4.1 共用

- 矩阵列：模型 / 凭据 / 本窗口已用 token / 请求数 / 窗口口径 / 冷却状态。
- **排序**（R7）：默认按总 token 降序；支持点击列头切换。
- **筛选**（R7）：至少「只看冷却中」开关；按模型分组折叠。
- `window_source == "fallback_24h"` 的行必须在口径列显式标注（R6），不能用同一个「本周期」措辞。

### 4.2 modern

- 新增组件放 `web/src/frontends/modern/features/groups/`（与既有 `Credential*` 组件同级）。
- 取数走资源层 + projector，禁止 `as` 断言；query key 进 `query-keys.ts`；失效在 `invalidation.ts` 登记（若本视图为纯只读且随分组详情刷新，可复用分组详情的既有失效计划）。
- 三语言文案：`i18n/locales/` 相应 namespace 三份同步。

### 4.3 classic

- **新增分组详情区块，需要走完整的「新增分组级设置键」以外的另一条路径**：这是**只读展示**，不涉及设置项白名单，但**响应投影白名单必须登记每一个字段**（classic 对白名单外字段硬失败）。
- 需要在 `app/resources/` 新增资源文件或在既有 `group-detail` 资源上扩展，并同步 `api/control/types.ts` 的 DTO。
- 三语言文案：`i18n/locales/{zh-CN,en-US,ja-JP}/` 三份同步。

## 5. 兼容与迁移

- **无数据库迁移**：不新增表、不新增列、不改 `schema_migrations`。账本保持 24 条，升级与回滚都不需要账本手术。
- **检查点格式**：`runtime_checkpoint.go` 新增两个可选字段。旧版本读到新检查点会忽略未知字段（按 JSON 解析语义），因此**回滚到旧镜像不会因检查点格式失败** —— 但窗口锚点会丢失，全部行退化为「近 24h」口径。这是可接受的降级。
- **接口是新增的**：classic 与 modern 的既有页面不会因为新端点而报错；但只要有一侧前端未同步投影白名单，该侧新增的矩阵区块会整页报错 —— 因此**后端接口与两套前端必须放同一个提交**（沿用上一个任务的既定做法）。

## 6. 权衡

| 选择 | 备选 | 理由 |
|---|---|---|
| 窗口锚点放运行时检查点 | 新表 + 迁移 | 免迁移。本项目档案里有多次上游迁移编号撞车的成本记录；锚点丢失只会让口径退化为「近 24h」，而迁移出错的代价是停机 + 修账本 |
| 小时桶 + 整点对齐 | `request_logs` 精确边界 | 矩阵行数数百，逐行精确校正会退化成 N+1；`request_logs` 默认只留 7 天。**且已核实 `usage_stats` 不受留存裁剪**：`internal/requestlog/retention.go` 的 `Sweep` 只删 `RequestLog`（`request_log_retention_days`）、`UsageAggregationJournal`（35 天）与 `CredentialQuotaHistory`（35 天），小时聚合**永久保留**（该文件 `:22-24` 的文档注释明写 "Hourly aggregates are retained indefinitely"）→ 任意窗口长度都能查到数据，无留存风险 |
| 单接口 + 两套前端各自投影 | 两套独立接口 | 数据完全一致，避免双份聚合逻辑；代价是两侧白名单都必须同步（已在 R8 接受） |
| 行集只含「窗口内有用量或冷却中」的组合 | 凭据 × 全模型笛卡尔积 | 44 凭据 × 4 模型 = 176 行里大量空行；笛卡尔积在凭据更多的组会爆 |
| 不做「剩余额度」推断 | 用触顶值估计额度 | 用户明确只要 token；推断值会误导，且需要额外记录触顶快照 |

## 7. 测试策略

- **`internal/state`**：周期状态机的边界 —— 首次冷却、连续两次冷却（`CycleStart` 迁移）、冷却未过期时不迁移、检查点保存/恢复后锚点保持、清理规则不误删、上限有界。
- **`internal/control`**（或聚合所在层）：窗口口径三态（`reset` / `fallback_24h` / 冷却中）各一例；整点对齐的正确性；行集筛选（无用量且未冷却的组合不下发）；截断与排序；`total_tokens` 与 `internal/usage` 既有口径一致。
- **契约**：响应字段与两套前端投影白名单一一对应（用表驱动断言字段集合，避免漏登记）。
- **门禁**：`make check`；本机为 CRLF 检出，格式类失败需按既有记忆判定归属。

## 8. 运维与回滚

- 无迁移 → 回滚只需换镜像，**不需要账本手术**（与上一个任务的回滚形态相同）。
- 新增接口与前端区块都是叠加式，不改变既有页面行为；本次**不开启任何开关、不改生产配置**。
- 部署后需在生产只读验证：新接口返回行数与 `usage_stats` 直接查库的结果一致（至少抽一个 `(凭据, 模型)` 组合逐项对齐）。
