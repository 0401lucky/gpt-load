# 后端兼容合并结果

## 改动边界

- 负责 `internal/control/**`；不改 storage、state、web，不操作 Git 索引或提交。
- 凭据冲突位于 `normalizeCredentialUpdate`：合并 `name` 与 `note` 的更新存在性判断，保留备注返回值及所有错误分支的返回形状。
- 手工改动：`credential_mutations.go`、新增 `credential_metadata_merge_test.go`、`donation_catalog.go`、`donation_catalog_test.go`。

## 凭据兼容

- `name` 保持上游的 trim、255 rune、非 null、归一化后不含控制字符；`note` 保持 2048 rune、不 trim、null/空串清空及未传保留语义。
- 事务同时写两个独立列；上游更新名称的快速路径允许同时持久化备注。仅更新备注的 exact registry restore 路径也保留模型冷却与可信周期。
- `CredentialUpdateRequest`、集合/详情投影、modern DTO 自动合并正确：classic 返回 name，不返回 note；modern 同时返回两者。已通知前端代理保留 classic 更新响应未携带的 note。
- 新回归覆盖独立/组合写入、独立/组合清空、中文长度边界、无效组合整笔拒绝、四个 classic/modern 列表/详情接口，以及密文、secret version、身份、配置、健康、模型冷却、周期起点和下一重置时刻不变。

## 自动合并审查与额外修复

- 发现 fork 捐献路径 `buildDonationTarget` 仍直接解密组代理，未展开上游新增 `proxy_id` 引用。`state.Compile` 因而使代理组的自动探测和人工审核均变为 `target_unavailable`。
- 在同一读快照/事务中改用 `s.resolvedProxyOverride(ctx, tx, ...)`，与上游普通凭据探测保持一致。
- 修复前运行新增回归，明确失败于 `CanProbe=false`、`CanManualReview=false`；修复后通过。覆盖实际 probe AttemptSpec 的代理地址、接受捐献、代理改名不改变目标、代理地址修改改变自动/人工目标修订、旧修订被拒，以及停用代理继承直连。
- 捐献路由/独立鉴权、库存去重与接收事务未被上游覆盖。`group_create` 的代理集中引用仍与既有库存登记共存。
- `credential_probe.go` 的自动合并仅替换代理解析入口，保留现有安全诊断与恢复凭证逻辑。
- `settings.go` 追加集中代理解析；既有分组专属 `rate_limit_reset_hint_enabled` 仍只在分组 effective/override 合同中，未被意外提升为全局配置。模型用量控制器无上游覆盖。
- `request_logs.go` 接纳上游首响应计时、客户端 IP 和凭据别名投影；fork 用量入口及返回字段独立保留。

## 验证

- `go test -count=1 ./internal/control -run 'TestCredential(Name|Note)|TestSubscriptionName|TestCredentialNameAndNoteUpdatesPreserveIdentityHealthAndModelCycles'`：通过，4.569s。
- `go test -count=1 ./internal/control -run '^TestDonationCatalogResolvesManagedProxyAndBindsEffectiveTarget$'`：修复前失败，修复后通过，5.515s。
- `go test -count=1 ./internal/control`：全部通过，72.978s；无新增或环境失败。日志：`C:/Users/lucky0401/AppData/Local/Temp/gpt-load-upstream-control-tests.log`。
- 4 个手改 Go 文件已经 gofmt；`git diff --check -- internal/control` 通过；control 中没有冲突标记。

## 交接

- 未执行 git add/commit；`credential_mutations.go` 索引仍显示 UU，由主会话完成冲突确认和暂存。
- 根模块完整门禁、独立 CPA module、外部数据库及迁移升级验证由主会话/存储代理汇总。本次 control 检查没有额外连接外部数据库。
