# 执行计划：为上游凭据增加备注字段

> 前置：工作区干净；从 `main` 切出工作分支。
> 每阶段结束都是一个可回退检查点。**实现顺序刻意从数据层向 UI 层推进**，每层都可独立编译与测试。

## 阶段 0：准备与基线

- [ ] 0.1 确认干净起点并切分支
  ```bash
  git status --short          # 应为空（.playwright-mcp/ 除外）
  git switch -c feat/credential-note
  ```
- [ ] 0.2 记录基线（确认既有测试本来就是绿的）
  ```bash
  go build ./... && go test ./internal/storage/... ./internal/control/... -count=1
  ```
  **验证**：全绿。若有既存失败，先记录、不得归因于本次改动。

## 阶段 1：数据层

- [ ] 1.1 `internal/storage/models/group.go` 的 `Credential` 增加
  `Note string \`gorm:"type:varchar(2048);not null;default:''"\``
  （放在 `ProxyConfig` 之后，与同类可编辑字段为邻）
- [ ] 1.2 新建 `internal/storage/migrations/0020_credential_note.go`，照 `0014_affinity_kind.go` 结构导出
  `ID0020` / `Up0020` / `Validate0020` / `ValidateRecoverable0020`
- [ ] 1.3 新建 `internal/storage/migrations/0020_credential_note_test.go`，覆盖：
  - 空库跑到 `0020` 后列存在且类型/默认值正确
  - **既有行不被改写**（升级前后行数、指纹、密文一致）
  - 重复执行 `Up0020` 为 no-op
  - `ValidateRecoverable0020` 在「表在但列不在」时返回 nil
- [ ] 1.4 `internal/storage/migration.go` 注册表追加 `ID0020` 条目（链尾）
- [ ] 1.5 **门禁**
  ```bash
  go build ./... && go vet ./...
  go test ./internal/storage/... -count=1
  ```
  **验证**：迁移注册表自校验通过（ID 连续、三个函数指针非 nil）；三种方言的迁移测试全绿。

## 阶段 2：接口层

- [ ] 2.1 `internal/control/credentials.go` 的 `CredentialUpdateRequest` 增加
  `Note optionalField[string] \`json:"note"\``
- [ ] 2.2 `internal/control/credentials.go` 的 `CredentialItemResponse` 增加
  `Note string \`json:"-"\``，并在 `mapCredentialItem`（`credential_mutations.go`）中填 `Note: row.Note`
- [ ] 2.3 `internal/control/credential_mutations.go` 的 `normalizeCredentialUpdate`：
  - **「至少传一个字段」判定必须带上 `Note`**（:26-28）—— 漏改会导致只传 note 被判 `ErrBadRequest`
  - 增加 rune 长度上限 2048 校验，超限返回既有 `ErrValidation`
- [ ] 2.4 同文件 `updates` map（:175-196）：仅在 `Note.Set` 时写入；`Note.Null` 与空串等价为 `""`
- [ ] 2.5 **确认 modern 通道**：`internal/control/modern_credentials.go` 的 `ModernCredentialItem` 增加
  `Note string \`json:"note"\``；核对列表与详情两条 modern 读路径都经过该投影
- [ ] 2.6 补测试：
  - 传 `note` 能写入并能读回
  - **不传 `note` 时既有备注保持不变**（R2 的核心）
  - 传空串 / `null` 能清空
  - 超长返回既有校验错误
  - **经典响应不含 `note` 键**（保护 classic 的关键断言，务必显式覆盖）
- [ ] 2.7 **门禁**
  ```bash
  go build ./... && go vet ./...
  go test ./internal/control/... -count=1
  ```

## 阶段 3：前端 modern

- [ ] 3.1 `modern/api/group-detail.ts`：`CredentialRow` 加 `note: string`；`readCredential` 用 `text(row.note ?? '')` 解析
- [ ] 3.2 `modern/api/credential-actions.ts`：patch 类型加 `note?: string | null`；`updateCredential` 照 `weight_manual` 的既有方式用 patch 回填
- [ ] 3.3 `modern/features/groups/CredentialDetailPanel.vue`：加备注输入框，接入既有 `dirty`（:53-60）与 `save()`（:97-131）
- [ ] 3.4 `modern/i18n/locales/{zh-CN,en-US,ja-JP}/credential-cards.ts`：新增 label / placeholder / 帮助文案
- [ ] 3.5 **门禁**
  ```bash
  pnpm --dir web run type-check
  pnpm --dir web run lint
  pnpm --dir web run build
  ```
  **验证**：三语 key 齐备；`check:styles` 通过。

## 阶段 4：全量与端到端

- [ ] 4.1 全量门禁（本地 CRLF 会导致 gofmt/format/tidy 误报，**以 CI 为准**）
  ```bash
  go build ./... && go vet ./...
  go test . ./internal/... -count=1
  ```
  **验证**：失败包集合与合并前一致（`internal/webui` 等的既有失败不算新增）。
- [ ] 4.2 与合并前对照失败集合（沿用 2026-09-20 的方法）
  ```bash
  go test . ./internal/... 2>&1 | grep "^FAIL" | awk '{print $2}' | sort -u > /tmp/after.txt
  comm -13 /tmp/before.txt /tmp/after.txt   # 应为空
  ```
- [ ] 4.3 端到端（本地跑起服务，用真实 HTTP 验证）
  - 既有凭据的 `note` 为空、行为不变
  - 写入备注 → 刷新页面仍在
  - 不传 `note` 的更新请求不清除已有备注
- [ ] 4.4 推分支 → 观察 CI（`make check` 由 CI 判定）

## 风险文件

| 文件 | 风险 |
|---|---|
| `credential_mutations.go:22-52` | 漏改「至少一字段」判定 → 只传 note 被拒。**最容易踩** |
| `credentials.go:76-100` | 忘记给 `Note` 加 `json:"-"` → **classic 凭据页整页报错**（本任务最高危回归） |
| `credential_mutations.go:175-196` | 无条件写 note → 未传时误清空既有备注 |
| `group.go:60-75` | 误改 `migrations/0001_initial.go` 的冻结基线 |

## 回退点

| 位置 | 回退动作 |
|---|---|
| 阶段 1 后 | `git reset --hard <阶段 0 提交>` |
| 阶段 2 后 | 同上 |
| 阶段 3 后 | 同上 |
| 生产（若已发布） | `note` 列不参与任何关键逻辑，可保留不回滚；彻底回滚用 `DROP COLUMN note` 或卷快照 |

## 完成判据

`prd.md` 的 `AC1`–`AC7` 全部满足；`AC5`（classic 不报错）必须有显式断言或实测覆盖，不得靠推理结论。
