# 调研：凭据备注字段的实现链路参考

> 来源：2026-09-20 代码调研（只读）。记录实现「Credential 备注字段」所需的完整文件链路与风险锚点。
> 结论性的需求与验收见 `../prd.md`；设计决策见 `../design.md`。

## 1. 后端改动清单（按数据流向）

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `internal/storage/models/group.go:60-75` | `Credential` 加 `Note string`（放 `ProxyConfig` :71 之后） |
| 2 | 新建 `internal/storage/migrations/0020_credential_note.go` | `ID0020` + `Up0020` / `Validate0020` / `ValidateRecoverable0020`，照 `0014_affinity_kind.go` |
| 3 | `internal/storage/migration.go:112` 后 | 注册表追加一条（链尾，ID 必须连续为 `0020`；自校验在 :161-172） |
| 4 | `internal/control/credentials.go:33-37` | `CredentialUpdateRequest` 加 `Note optionalField[string] \`json:"note"\`` |
| 5 | `internal/control/credential_mutations.go:22-52` | `normalizeCredentialUpdate`：**「至少一字段」判定（:26-28）必须带上 `Note`**；加 2048 rune 长度校验 |
| 6 | `internal/control/credential_mutations.go:175-196` | `updates` map：仅 `Note.Set` 时写入 |
| 7 | `internal/control/credentials.go:76-100` + `credential_mutations.go:453-493` | `CredentialItemResponse` 加 `Note string \`json:"-"\``；`mapCredentialItem` 填值 |
| 8 | `internal/control/modern_credentials.go:14-18` | `ModernCredentialItem` 加 `Note string \`json:"note"\`` |

**不需要改**：`credential_mutations.go:198-217` 的运行时注册表回调（note 不参与调度）、`RevealGroupCredential`（`credential_mutations.go:111`，有显式 Select 白名单，不含 note 无影响）、`migrations/0001_initial.go`（**冻结基线，绝对不要动**）。

## 2. 前端改动清单（仅 modern）

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `web/src/frontends/modern/api/group-detail.ts` | `CredentialRow` 加 `note: string`；`readCredential`（:227 起）用 `text(row.note ?? '')` |
| 2 | `web/src/frontends/modern/api/credential-actions.ts:23-38` | patch 类型加 `note?: string \| null`；回填照 `weight_manual` 写法 |
| 3 | `web/src/frontends/modern/features/groups/CredentialDetailPanel.vue` | 加输入框；接入 `dirty`（:53-60）与 `save()`（:97-131） |
| 4 | `web/src/frontends/modern/i18n/locales/{zh-CN,en-US,ja-JP}/credential-cards.ts` | 三语新增 key |

**classic 前端零改动** —— 前提是 `Note` 保持 `json:"-"`（见 §4）。

可用组件：`modern/components/ui/AppTextField.vue`、`AppField.vue`、`AppFormSection.vue`。

## 3. 可照搬的既有先例

### 3.1 `WeightManual`（**响应分层的样板**）

```
models/group.go:70                                  字段定义
credentials.go:88-89                                Weight 经典可见 / WeightManual 标 json:"-"
credential_collection_helpers.go:144-145            行级填充
modern_credentials.go:17                            ModernCredentialItem 暴露 weight_manual
modern/api/group-detail.ts:240-245                  readCredential 容忍 undefined
modern/api/credential-actions.ts:23-38              patch 回填（绕过响应缺失）
modern/features/groups/CredentialDetailPanel.vue    表单与 save
```

**关键**：经典响应**不含** `weight_manual`，modern 靠 patch 回填。`note` 完全照此办理。

### 3.2 `ProxyConfig`（**加列迁移的样板**）

```
models/group.go:71
migrations/0005_proxy_config.go                      ALTER TABLE 加列（groups 与 credentials 同时加）
control/proxy_config.go:17 / :254                    校验与视图构造
classic/.../GroupCredentialRecord.vue:333-339        ProxyConfigEditor
modern/.../CredentialDetailPanel.vue:104-110         表单
```

注意 0005 加的是**可空 text**；本次要加 `NOT NULL DEFAULT ''`，`0005` 与 `0014` 的 `Up` 结构都可参照，**不要照搬 0018/0019**（那是 donation 的复杂迁移）。

## 4. 已确认的关键约束（务必遵守）

1. **classic 白名单会拒绝任何未知字段**
   `web/src/frontends/classic/app/resources/projector.ts:134-144`：
   ```ts
   for (const field of Object.keys(record)) {
     if (allowed.has(field)) continue
     if (secretLikeField.test(field)) invalidResponse()
     invalidResponse()        // 白名单外一律拒绝
   }
   ```
   → `note` **绝不能**出现在经典响应里，必须 `json:"-"`。

2. **`optionalField[T]` 提供三态**（`internal/control/group_write.go:81-113`）
   `Set=false`（未传）/ `Null=true`（显式 null）/ `Set=true,Value=x`（含空串）。
   → 「未传不清除、传空串清空」可直接表达，无需额外协议。

3. **modern 写接口就是共用路径**
   `credential-actions.ts:23-38` 打 `PUT /api/groups/{group}/credentials/{id}`，响应为经典格式，故靠 patch 回填。

4. **迁移必须追加在链尾**
   `internal/storage/migration.go:161-172` 校验 ID 连续且等于数组下标；当前末条 `ID0019`，新条必须是 `0020`。

5. **凭据录入是按行的**（本次不改，仅记录）
   `internal/control/group_write.go:365-371` 的 `credentialInputEntries` 按 `\n` 切分，`maxCredentialLines = 5000`；每行可为纯 key 或 JSON 对象。D1 已排除改动此路径。

## 5. 测试参照

- 迁移测试：`migrations/0005_proxy_config_test.go`、`migrations/0014_*` 同类；内存库用 `internal/testutil/sqlitetest` 的 `OpenMigrated(t)`。
- 接口测试：`internal/control/credential_api_foundation_test.go`、`classic_api_contract_test.go`。
- **必须显式断言**「经典响应不含 `note` 键」——这是 AC5 的核心证据。
