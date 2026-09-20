# 为上游凭据增加备注字段

## Goal

给 group 下的上游凭据（Credential）增加一个可编辑的**备注**字段，用来标注每个 key 属于哪个上游账号。

要解决的问题：一个 group 下常有几十到几百个 key（生产上 nvidia 组 300+），目前 Credential 模型**没有任何名称或备注字段**，只能靠 ID 和掩码区分，无法知道哪个 key 是哪个账号。

## Background（已确认事实，来自代码调研）

**数据层**
- `Credential` 模型：`internal/storage/models/group.go:60-75`，含 `Data`(密文) / `Fingerprint` / `Status` / `WeightManual` / `ProxyConfig` 等，**无任何备注字段**。
- 同构先例：可空用 `ProxyConfig *string`（`:71`）；非空 + 空串默认可参 `internal/storage/models/donation.go:102` 的 `Note string`。
- `migrations/0001_initial.go` 的 `initialCredential` 是**冻结基线，不可改**；`Validate0001` 只校验「应有列存在」，不拒绝多余列，故追加列安全。

**接口层**
- 单个凭据更新：`PUT /api/groups/:group_id/credentials/:credential_id`，注册于 `internal/control/http_routes.go:367-377`。
- 请求体 `CredentialUpdateRequest`：`internal/control/credentials.go:33-37`，三字段均用 `optionalField[T]`（区分「未传 / null / 有值」）。
- 校验 `normalizeCredentialUpdate`：`internal/control/credential_mutations.go:22-52`；**「三字段都没传」会判 `ErrBadRequest`**，新增字段必须同步该条件。
- 落库：`credential_mutations.go:175-196` 的 `updates` map。响应：`CredentialItemResponse`（`credentials.go:76-100`）+ `mapCredentialItem`。

**响应分层的现成套路（决定了本次不需要碰 classic）**
- `CredentialItemResponse.WeightManual` 标 `json:"-"`（`credentials.go:89`），只在 `internal/control/modern_credentials.go:14-18` 的 `ModernCredentialItem` 里以 `weight_manual` 暴露，经 `/api/modern/groups/:group_id/credentials` 下发。
- `classic/app/resources/projector.ts:134-144` 的 `assertNoSecretLikeFields` 会拒绝**白名单外的响应字段**。但因为 `json:"-"` 的字段根本不出现在经典响应里，**沿用该套路即可让 classic 完全不受影响**。

**前端**
- classic 与 modern 是**同一个 SPA**（`web/src/main.ts` 动态加载），构建只有一份 bundle，**路由地址相同**（`/groups/:id`）。
- `web/src/shared/frontend/preference.ts`：**默认 modern**，仅当 localStorage 显式设为 classic 且当前为 admin 时才走 classic。
- modern 凭据编辑面板：`modern/features/groups/CredentialDetailPanel.vue`，已有 weight + proxy 表单与 dirty/save 机制（`save()` :97-131、`dirty` :53-60），是备注输入框的落点。
- modern 可用表单组件：`modern/components/ui/AppTextField.vue`、`AppField.vue`、`AppFormSection.vue`。

**前端 API 层**
- modern 读取：`modern/api/group-detail.ts` 的 `CredentialRow` / `readCredential`。
- modern 写入：`modern/api/credential-actions.ts:23-38` 的 `updateCredential(client, group, id, patch)`。
- ⚠️ **待实现时确认**：该 patch 提交到哪个 URL（是否为共用的 `PUT /api/groups/:group_id/credentials/:credential_id`）。若为独立路径，需一并扩展。

**迁移约定**
- 新迁移追加链尾（当前末条 `ID0019`），ID 必须连续即 `0020`；`internal/storage/migration.go:161-172` 自我校验。
- 「只加一个带默认值的列」照 `migrations/0014_affinity_kind.go`：导出 `ID` / `Up` / `Validate` / `ValidateRecoverable` 四个符号，`Up` 中先判 `HasColumn` 再 `ALTER TABLE`；**不需要** `SchemaModels`/`TableNames`。

**i18n**
- 后端 `internal/platform/i18n/locales/{zh-CN,en-US,ja-JP}.go` 三份同步（点分小写、字母序）。
- modern 文案在 `modern/i18n/locales/*/credential-cards.ts`（根 key `credentialCards`），三语三份。

## Key Decisions（已与用户确认）

- **D1 录入方式 = 在 UI 逐个编辑**。不做列表内联编辑，不做「导出→编辑→导入」往返。因此**不改动录入解析**（`group_write.go:365-371` 的 `credentialInputEntries`）与导出格式。
- **D2 UI 范围 = 只做 modern**。classic 不做编辑入口，也不需要改白名单（因为新字段按 `json:"-"` 处理，不进经典响应）。

## Requirements

- **R1 持久化**：Credential 增加备注列，由新迁移 `0020` 落地；SQLite / MySQL / PostgreSQL 行为一致；**不改写既有凭据行**。
- **R2 可读写**：备注经既有凭据更新接口写入、经 modern 读取接口返回。**未传该字段时不得清除**已有备注（保持 `optionalField` 的「未传 ≠ 清空」语义）；传空串表示清空。
- **R3 不破坏既有契约**：classic 凭据页不因新字段报错；凭据的调度、权重、代理、状态、去重指纹等行为完全不变；备注**不参与**指纹计算与调度。
- **R4 前端可编辑**：modern 凭据编辑面板可查看与编辑备注，保存后立即可见，刷新保持。
- **R5 文案三语**：modern 新增文案覆盖 zh-CN / en-US / ja-JP；后端错误复用既有校验错误文案，不新增后端 key。
- **R6 校验**：备注设长度上限，超限返回既有校验错误类型（`ErrValidation`）。

## Acceptance Criteria

- [ ] **AC1** 迁移 `0020` 在三种数据库上可正常执行；对既有凭据表**无数据改写**（升级前后行数、指纹、密文不变）；二次启动为 no-op。
- [ ] **AC2** 通过 `PUT /api/groups/:group_id/credentials/:credential_id` 传 `note` 可写入；**不传 `note` 时既有备注保持不变**；传空串可清空。
- [ ] **AC3** 备注超长时返回既有校验错误（不新增错误码）。
- [ ] **AC4** modern 凭据编辑面板可查看、编辑、保存备注；保存后界面立即反映，刷新页面后仍保持。
- [ ] **AC5** classic 凭据页在新旧数据下均正常渲染，不出现 `invalidResponse` 类报错。
- [ ] **AC6** 既有凭据测试全绿；`make check` 在 CI 通过。
- [ ] **AC7** 三语文案齐备，无缺失 key。

## Out of Scope

- 备注的搜索、过滤、排序、批量编辑。
- 列表内联编辑与「导出→编辑→导入」往返（D1 已排除）。
- classic 前端的备注查看与编辑（D2 已排除）。
- 备注的审计历史与版本回溯。
- 订阅类凭据（OAuth JSON）的备注语义扩展。
- 备注参与去重或调度决策。
