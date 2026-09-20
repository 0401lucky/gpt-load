# 技术设计：为上游凭据增加备注字段

## 1. 边界与总体形状

改动面被两条已确认的决策**收窄**为：

- **D1 逐个编辑** → 不碰录入解析（`group_write.go:365-371`）、不碰导出格式、不做内联编辑。
- **D2 只做 modern** → classic 前端**零改动**，但后端必须保证新字段**不进入经典响应**（见 §3，这是本设计最关键的一条）。

最终形状：**数据库加一列 → 更新接口多认一个可选字段 → modern 专用投影多暴露一个字段 → modern 编辑面板加一个输入框**。

## 2. 数据层

### 2.1 模型字段

`internal/storage/models/group.go` 的 `Credential`（:60-75）新增：

```go
Note string `gorm:"type:varchar(2048);not null;default:''"`
```

**选型理由**：`not null default ''` 而非可空 `*string`。
- 本功能不需要区分「从未设置」与「设为空」，两者在 UI 上都是「没有备注」。
- 免去所有读取路径的 NULL 处理，查询与比较更简单。
- 有现成同构先例：`internal/storage/models/donation.go:102` 的 `Note string`。

**列名**：与 GORM 蛇形一致，**不写** `column:`（与 `WeightManual` 同风格；只有不一致的 `ProxyConfig` 才写）。

### 2.2 迁移 `0020`

新建 `internal/storage/migrations/0020_credential_note.go`，照 `0014_affinity_kind.go` 的最简结构：

- `const ID0020 = "0020_credential_note"`
- `Up0020`：先 `ValidateRecoverable0020` 门禁 → `HasColumn` 判重 → 执行
  `ALTER TABLE credentials ADD COLUMN note VARCHAR(2048) NOT NULL DEFAULT ''`
  → `return Validate0020(db)`
- `Validate0020`：列存在 + 类型/非空/默认值符合预期
- `ValidateRecoverable0020`：表在 + 列不在 → `nil`（容忍 MySQL DDL 隐式提交造成的中断）

**不需要** `SchemaModels` / `TableNames`（那是建表型迁移才导出的符号）。

在 `internal/storage/migration.go:112` 之后追加注册表条目（必须位于链尾，ID 连续为 `0020`）。

**方言可行性**：`ADD COLUMN ... NOT NULL DEFAULT ''` 在 SQLite（非 NULL 默认值合法）、MySQL 8、PostgreSQL 15 上均受支持，且都**只改元数据、不改写既有行**（PG 11+ 起支持非易变默认值的快速加列）。既有迁移 `0005`（加可空列）与 `0014` 已验证该路径在三种方言下可用。

## 3. 响应分层（关键设计）

### 3.1 约束

`web/src/frontends/classic/app/resources/projector.ts:134-144` 的 `assertNoSecretLikeFields` 对**白名单外的任何字段**一律 `invalidResponse()`。而 classic 与 modern 的凭据读接口共用同一份 `CredentialItemResponse` 序列化。**因此：只要 `note` 出现在经典响应里，classic 凭据页立刻整页报错。**

### 3.2 方案：沿用 `WeightManual` 的 `json:"-"` 套路

| 层 | 处理 |
|---|---|
| `CredentialItemResponse`（`credentials.go:76-100`） | 新增 `Note string \`json:"-"\`` —— **字段存在但不序列化**，`mapCredentialItem` 正常填充 |
| `ModernCredentialItem`（`modern_credentials.go:14-18`） | 嵌入后再暴露 `Note string \`json:"note"\``，经 modern 通道下发 |
| classic 前端 | **零改动** —— `note` 从不进入它读到的响应 |

这正是 `WeightManual`（经典侧 `json:"-"`）与 `weight_manual`（modern 侧）的既有模式，属于**已验证的套路**，不是新发明。

### 3.3 写入后的回填

modern 的 `updateCredential`（`modern/api/credential-actions.ts:23-38`）打的是**共用**的 `PUT /api/groups/:group_id/credentials/:credential_id`，其响应是经典格式（**不含 `note`**）。因此现代化前端需照 `weight_manual` 的既有做法，用 patch 值回填：

```ts
return Object.hasOwn(patch, 'note') ? { ...row, note: patch.note ?? '' } : row
```

这与现有 `weight_manual` 的处理完全同构。

## 4. 接口契约

### 4.1 请求

`internal/control/credentials.go:33-37` 的 `CredentialUpdateRequest` 新增：

```go
Note optionalField[string] `json:"note"`
```

`optionalField[T]`（`group_write.go:81-113`）提供所需的三态，**已确认**：

| 客户端发送 | `Set` | `Null` | 语义 |
|---|---|---|---|
| 不带 `note` 键 | false | — | **不改动**已有备注 |
| `"note": null` | true | true | 按「清空」处理（与空串等价） |
| `"note": "账号A"` | true | false | 设置该值 |
| `"note": ""` | true | false | 清空 |

### 4.2 校验

`normalizeCredentialUpdate`（`credential_mutations.go:22-52`）需两处调整：

1. **「至少传一个字段」的判定必须带上 `Note`**（:26-28）。否则只传 `note` 会被误判为 `ErrBadRequest`。
2. 新增长度校验：上限 **2048 个字符**（按 rune 计，与 `varchar(2048)` 的字符语义一致，中文备注友好）。超限返回 **既有** `ErrValidation`，不新增错误码。

不做字符集限制（允许任意 UTF-8，便于写中文账号名）。

### 4.3 落库

`credential_mutations.go:175-196` 的 `updates` map 中，**仅在 `Note.Set` 为真时**写入：

- `Note.Set && !Note.Null` → `updates["note"] = Note.Value`
- `Note.Set && Note.Null` → `updates["note"] = ""`

**未传时不进 map**，从而天然满足 R2「未传不清除」。

### 4.4 不参与的东西

备注**不进**运行时凭据注册表（`credential_mutations.go:198-217` 的回调只灌 `status` / `weight_manual` / `proxy`），**不参与**指纹计算、调度与健康判定。

## 5. 前端（modern）

| 文件 | 改动 |
|---|---|
| `modern/api/group-detail.ts` | `CredentialRow` 加 `note: string`；`readCredential` 用 `text(row.note ?? '')` 解析 |
| `modern/api/credential-actions.ts` | patch 类型加 `note?: string \| null`；`updateCredential` 按 §3.3 回填 |
| `modern/features/groups/CredentialDetailPanel.vue` | 加备注输入框（用 `AppTextField` / `AppField`），纳入既有 `dirty` 判定（:53-60）与 `save()`（:97-131） |
| `modern/i18n/locales/{zh-CN,en-US,ja-JP}/credential-cards.ts` | 新增 label / placeholder / 帮助文案，三语同步 |

空备注在前端以空串表示，不显示为「null」。

## 6. 兼容性与迁移

- **既有凭据**：新列默认空串，语义为「无备注」，UI 显示为空 —— 无需数据回填。
- **经典前端**：因 §3.2 的分层，**完全不受影响**，无需改白名单（这点与初始调研的担忧相反，是 `json:"-"` 方案带来的收益）。
- **API 兼容**：请求侧新增字段为**可选**，旧客户端不传即行为不变；响应侧经典格式**逐字节不变**。
- **订阅类凭据**：同表共用该列，但本次不做额外语义（Out of Scope）。

## 7. 权衡与备选方案

| 方案 | 否决理由 |
|---|---|
| 让 `note` 进经典响应、同时给 classic 白名单加 `note` | 违反 D2（classic 零改动），且把 classic 拖进变更面 |
| 可空 `*string` 列 | 需要区分「未设」与「空」的场景不存在，徒增 NULL 处理 |
| 独立 `credential_notes` 表 | 一对一字段不值得建表，徒增 join |
| 复用 `ProxyConfig` 式的 `optionalField` + 结构体 | 备注是纯文本，不需要结构化 |
| 把备注塞进 `Data` 密文里 | 会使备注不可查询、且污染凭据指纹语义 |

## 8. 运维与回滚

- **迁移只加列**，不改写数据，回滚风险低；但**已升级的库不能直接换回旧镜像**（旧注册表无 `0020`，账本下标不匹配 —— 与 2026-09-20 那次同因）。生产升级仍走既有流程与 `upgrade.sh`。
- **回滚方式**：`DROP COLUMN note`（SQLite 3.35+ / MySQL 8 / PG 均支持）或恢复卷快照。因该列不参与任何关键逻辑，即使暂时留着也**不影响服务运行**，回滚不紧急。
- **旧数据**：升级后所有既有凭据的 `note` 为空串，行为与升级前完全一致。
