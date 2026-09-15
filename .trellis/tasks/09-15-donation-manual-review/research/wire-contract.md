# 实施级线协议冻结（由 design.md 派生）

本文件把 [design.md](../design.md) 的契约落到字段级细节，已于 2026-09-15 按两端实现和 Codex 真实进程复验同步。语义仍以 design.md 为准；浏览器验收状态另见复核记录。

## 1. 模式

- `validation_mode` / `effective_mode` 取值：`auto` | `manual_review`。
- gpt-load `GET /capabilities` 新增 `features: ["manual_review_v1"]` 与独立 `review_limits` 对象；`protocol_version` 仍为 `"1"`，原 `limits`、`input_types` 不变。
- gpt-load `GET /groups` 每项新增 `can_manual_review: bool`、`manual_target_revision: string`、`manual_unavailable_reason: string`；原 `can_probe` / `target_revision` / `unavailable_reason` 语义与算法不变。
- 批次创建请求新增可选字段 `validation_mode`。调用方向旧接收端发送 auto 时省略它；接收端对缺省/显式 auto 都使用冻结的旧 comparator 计算摘要，不能仅依赖 omitempty。

## 2. gpt-load 接收端路由

全部挂在 `/integrations/donations/v1`，沿用既有 Bearer 认证与 `response.SuccessI18n` 包装（`code`/`data`）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/batches/:batch_id/items/:item_id/review-context` | 审核上下文 |
| POST | `/batches/:batch_id/items/:item_id/review-actions` | 提交审核动作 |
| GET | `/review-actions/:action_id` | 查询动作 |
| POST | `/batches/:batch_id/items/:item_id/tests` | 发起真实调用 |
| GET | `/tests/:test_id` | 查询测试元数据 |

### 2.1 review-context 响应 `data`

```json
{
  "batch_id": "uuid",
  "item_id": "uuid",
  "group_id": 4,
  "state": "pending_review",
  "effective_mode": "manual_review",
  "item_revision": 3,
  "review_target_revision": "64-hex",
  "expires_at_ms": 1758000000000,
  "can_review": true,
  "can_reject": true,
  "can_test": true,
  "unavailable_reason": "",
  "review_action": "approve",
  "test_models": ["model-a", "model-b"],
  "review_limits": {
    "max_prompt_bytes": 16384,
    "max_request_bytes": 65536,
    "default_output_tokens": 1024,
    "max_output_tokens": 4096,
    "max_response_bytes": 131072,
    "max_event_bytes": 65536,
    "total_timeout_seconds": 120,
    "first_byte_timeout_seconds": 30,
    "idle_timeout_seconds": 20,
    "max_note_bytes": 2048
  }
}
```

- `unavailable_reason` 说明当前不可用能力，不能代替三个独立布尔字段。取值包括 `staging_expired`、`item_state`、`target_changed`、`target_unavailable`、`test_running`、`resource_busy`、`model_unavailable`。
- `test_models` 取自原组当前执行器支持的文本模型；为空时 `can_test=false`、`unavailable_reason="model_unavailable"`，但 `can_review` 仍可为 true。
- `effective_mode` 始终表示条目的真实存储态。可转审的旧自动项仍返回 auto，直到 enter_review 生效后才是 manual_review。
- `review_action` 表示主动作：enter_review（旧 auto 项转审）或 approve（已在待审）；不可审核时为空。`can_reject` 独立表达拒绝能力，目标变化/删除/下线时仍可拒绝有效待审项，不能根据 can_review 隐藏拒绝。

### 2.2 review-actions 请求 / 响应

请求体（严格白名单）：

```json
{
  "action_id": "uuid-v4",
  "kind": "enter_review",
  "actor": "user:123",
  "expected_item_revision": 3,
  "review_target_revision": "64-hex",
  "note": "可选，UTF-8 ≤ 2048 字节；reject 必填非空"
}
```

- `actor` 是 new-api 侧认证得到的操作者标识（1–128 字节可见 ASCII，无控制字符）。gpt-load 不把它当作身份来源，只作为审计事实记录；集成 token 是全命令共用的，不能代表操作者。
- `kind` ∈ `enter_review` | `approve` | `reject`。
- `Idempotency-Key` 头必须等于 `action_id`。
- `enter_review` 和 `approve` 都必须携带管理员所见的 64 位小写十六进制人工目标；转审比对当前目标，approve 比对冻结目标。`reject` 不携带 review_target_revision。

响应 `data`（动作查询返回同一形状）：

```json
{
  "action_id": "uuid-v4",
  "batch_id": "uuid",
  "item_id": "uuid",
  "kind": "approve",
  "expected_item_revision": 3,
  "review_target_revision": "64-hex",
  "outcome": "applied",
  "reason_code": "",
  "effect_revision": 4,
  "applied_at_ms": 1757900000000
}
```

- `outcome` ∈ `applied` | `rejected`。`rejected` 表示命令**未应用**的业务拒绝，不是捐献项被拒收。
- 业务拒绝码闭集：`revision_mismatch`、`mode_conflict`、`target_changed`、`target_unavailable`、`staging_expired`、`item_state`、`already_reviewed`、`not_reviewable`、`test_running`、`resource_busy`、`duplicate_pending`。
- 鉴权失败、JSON 非法、基础设施错误不写动作行，按既有错误码返回。
- 命令 kind 的 approve/reject 与 item.review_decision 的 approved/rejected 不同。回执 reviewed_at_ms 等于该动作 applied_at_ms；effect_revision 大于 expected revision 且不大于完整 item revision。

### 2.3 tests

请求体：

```json
{
  "test_id": "uuid-v4",
  "actor": "user:123",
  "expected_item_revision": 3,
  "review_target_revision": "64-hex",
  "model": "z-ai/glm-5.3-flash",
  "prompt": "非空 UTF-8",
  "system_prompt": "可选",
  "max_output_tokens": 1024,
  "stream": false
}
```

- `Idempotency-Key` 头必须等于 `test_id`；actor 由 new-api 认证层生成，并纳入测试请求摘要。
- `prompt + system_prompt` 合计 ≤ 16384 字节；完整请求体 ≤ 65536 字节。

非流式响应 `data`：

```json
{
  "test_id": "uuid-v4",
  "batch_id": "uuid",
  "item_id": "uuid",
  "model": "model-a",
  "stream": false,
  "state": "succeeded",
  "reason_code": "",
  "status_code": 200,
  "start_revision": 3,
  "target_revision": "64-hex",
  "started_at_ms": 1757900000000,
  "finished_at_ms": 1757900001000,
  "output_bytes": 42,
  "text": "脱敏后的回复正文",
  "usage": {"input_tokens": 12, "output_tokens": 34}
}
```

- 终态 state 为 succeeded/failed/cancelled/interrupted；GET 或相同 ID 的 POST 元数据重放也可能返回 running。running 重放不会再次调用上游。
- 正文只属于首次调用的即时响应，结果以 state/reason_code 为准；有部分正文不能当作 succeeded。
- `GET /tests/:test_id` **不返回 `text`**，只返回元数据（含 `state`、`reason_code`、`status_code`、时间、`output_bytes`、`usage`）。

流式响应：`Content-Type: text/event-stream`，事件序列固定为一次 `meta` → N 次 `delta` → 一次 `done`：

```
event: meta
data: {"test_id":"...","batch_id":"...","item_id":"...","model":"...","stream":true,"state":"running","reason_code":"","status_code":0,"start_revision":3,"target_revision":"...","started_at_ms":1757900000000,"finished_at_ms":0,"output_bytes":0}

event: delta
data: {"text":"脱敏增量"}

event: done
data: {"test_id":"...","batch_id":"...","item_id":"...","model":"...","stream":true,"start_revision":3,"target_revision":"...","started_at_ms":1757900000000,"state":"succeeded","reason_code":"","status_code":200,"finished_at_ms":1757900001000,"output_bytes":42,"usage":{"input_tokens":12,"output_tokens":34}}
```

- 每个事件 ≤ 65536 字节，累计响应正文 ≤ 131072 字节，超限以非成功结束。
- `usage` 字段可缺省；缺省时 new-api 不得伪造。
- 只有准备成功后才开始 SSE；相同 ID 的重放返回 JSON 元数据，包括 running，不伪造新正文。
- 取消由下游断开触发；成功须完整 done 且结果已经持久保存。取消/断流/持久失败可能没有可送达的 done，调用方此时只能标记非成功并读取元数据，不能重发模型请求。

### 2.4 错误码新增

gpt-load `internal/platform/errors`：

| 常量 | HTTP | code |
| --- | --- | --- |
| `ErrDonationReviewNotFound` | 404 | `DONATION_REVIEW_NOT_FOUND` |
| `ErrDonationReviewConflict` | 409 | `DONATION_REVIEW_CONFLICT` |
| `ErrDonationTestUnavailable` | 409 | `DONATION_TEST_UNAVAILABLE` |
| `ErrDonationTestConflict` | 409 | `DONATION_TEST_CONFLICT` |
| `ErrDonationTestBusy` | 409 | `DONATION_TEST_BUSY` |
| `ErrDonationItemNotFound` | 404 | `DONATION_ITEM_NOT_FOUND` |

## 3. new-api 路由

前缀 `/api/donations/admin`，全部 `AdminAuth` + 显式权限：

| 方法 | 路径 | 权限 |
| --- | --- | --- |
| GET | `/records/:item_id/review-context` | `donation_records.read` |
| POST | `/records/:item_id/review-actions` | `donation_records.read` + `donation_records.review` |
| GET | `/records/:item_id/review-actions/:action_id` | `donation_records.read` |
| POST | `/records/:item_id/tests` | `donation_records.read` + `donation_records.test` |
| GET | `/records/:item_id/tests/:test_id` | `donation_records.read` |

权限：`donation_records.review` 与 `donation_records.test` 为 `donation_records` 资源下与 `read` 并列的新动作，沿用 `service/authz` 的 `Permission{Resource, Action}` 结构；只读记录管理员默认不获得这两个动作。

浏览器请求与 gpt-load 请求是两种形状：审核 JSON 只允许 kind/expected_item_revision/review_target_revision/note；测试 JSON 只允许 expected_item_revision/review_target_revision/model/prompt/system_prompt/max_output_tokens/stream。UUID 只放 Idempotency-Key，actor 不允许从浏览器传入；转发时由 new-api 补齐 UUID 与认证 actor。action_id/test_id 在 new-api 各自全局唯一，跨 actor 复用冲突。

活动字段：`DonationCampaign` 新增 `validation_mode`（`auto` | `manual_review`，缺省/空写回 `auto`，未知值拒绝）。`DonationCampaignRevision` 快照随结构体序列化自然包含该字段，不重写历史行。

批次新增 `ValidationMode`，提交时从活动冻结。条目新增：`review_state`、`review_note`（仅本人可见的最终拒绝原因投影）、`reviewed_at_ms`、`item_revision`、`effective_mode`。

本人 `GET /api/donations/batches[/:id]` 的 item 投影包含安全模式、revision、审核状态/时间及 `review_note`（仅该项已 applied 的最终 reject 备注）；reason_code 保持闭集，不装自由文本。管理 record 另有 pending_review_action、latest_test 和各最近 20 条 recent_review_actions/recent_tests，无提示词或回复。

## 4. 状态映射（两端口径必须一致）

| gpt-load item.state | new-api item.state | 用户可见含义 |
| --- | --- | --- |
| `queued` / `validating` | 同名状态（发送未确认时另为 unconfirmed） | 自动检测中 |
| `pending_review` | `pending_review` | 等待人工审核 |
| `rejected` | `rejected` | 审核未通过（附拒绝原因） |
| `committing` | `committing` | 接收处理中，运行时待发布 |
| `accepted` | `accepted` | 已接收 |
| `existing` | `existing` | 已有库存；前端计入重复统计 |
| `invalid` | `invalid` | 无效；`reason_code=staging_expired` 时 UI 显示暂存过期 |
| `retry_pending` | `retry_pending` | 自动重试中或已耗尽 |

奖励保持 none/pending/paused/rewarded 等原有业务状态；账号暂停保留待奖励事实。只有匹配原 applied approve 且首次 accepted 的人工项可奖励；approved + existing 只记录审核动作及已有库存，不能新增奖励。

批次回执冻结 validation_mode，完整 items 带 effective_mode/item_revision/review_target_revision/entry_action_id/review_action_id/review_decision/reviewed_at_ms/staging_expires_at_ms。review_decision 只允许空或 approved/rejected；不能用 approve/reject 的命令值。new-api 同时绑定原动作、目标、时间和版本；完整回执可确认原 pending 动作，不能从裸 accepted 或测试成功推导批准。

## 5. 未变更的兼容要求

- 原 7 天暂存期（604800 秒）不变；转审、测试、刷新都不续期。
- 原 `limits`、`input_types=["api_key"]`、`protocol_version="1"` 不变；新能力通过 `features` 协商。
- 旧接收端缺 `item_revision` 时按 0 处理并沿用原单调推进；已观察正版本或人工事实的项禁止 0/低版本覆盖。
- 缺少 `manual_review_v1` 时 new-api 不得保存或提交 `manual_review` 活动。
