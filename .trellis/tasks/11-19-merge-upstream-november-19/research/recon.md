# 2026-11-19 上游合并证据

## 输入与规模

- fork：`72b24ef8765688577965a668ca1c6bdfcd5564c2`。
- upstream：`e4a6ddec49327b2245c3bf5dc6479277d6821180`。
- merge base：`dd33ae390c23575988b04b3f551873ae70f3eba0`。
- `git rev-list --left-right --count HEAD...upstream/main`：48 / 50。
- 上游相对共同祖先：497 files changed, 27668 insertions, 3129 deletions。
- `git merge-tree --write-tree --name-only HEAD upstream/main` 产生预合并 tree `4af3fd08e991d6a2df558cbec18c54d717799d04`，未修改工作区。

## 文本冲突

1. `internal/control/credential_probe.go`
2. `internal/control/credential_probe_test.go`
3. `internal/platform/i18n/locales/en-US.go`
4. `internal/platform/i18n/locales/ja-JP.go`
5. `internal/platform/i18n/locales/zh-CN.go`
6. `internal/storage/database_integration_test.go`
7. `internal/storage/db_test.go`
8. `internal/storage/migration.go`
9. `internal/storage/migration_test.go`
10. `web/src/frontends/classic/features/groups/credentials/CredentialTestDialog.vue`

## 必需的语义兼容

- 现有 fork 链尾 0033；上游新增 `0031_model_price_reference.go` 与 fork `0031_request_log_client_ip` 导出符号重名，必须追加为 0034。旧链禁止重排；新迁移测试含上游下标，需按语义 ID 适配。
- fork `CredentialProbeResponse` 已有三项 structured 诊断；上游 #944 新增 `error_detail` 和 `credentialProbeErrorDetail`。两套字段并存，安全摘要不得弱化完整已知秘密遮盖。
- fork `internal/parameteroverride/rules.go` 有 compiled rule fingerprint；上游有新增方法，自动合并后检查签名与有效配置隔离仍正确。
- 上游涉及 container/control bootstrap、gateway handler/reason、config、state snapshot、web 两套前端资源和设置的自动合并；重点核对捐献、备注、模型用量和冷却周期。
- 上游 `Makefile` 新版并行 Go 与前端静态检查，Vite build 放在 Go 编译完成之后；本地等价执行必须保留此依赖。

## 已有参考

- `.trellis/spec/backend/database-guidelines.md`：迁移编号冻结、完整链断言、ID 定位和真实升级回归。
- `.trellis/tasks/archive/2026-10/10-21-merge-upstream-october-21/design.md`：冷却/可信周期、diagnostics、前端批量测试及迁移顺延合同。
- `.trellis/tasks/archive/2026-10/10-23-merge-upstream-main/implement.md`：上次门禁出现 Windows/环境失败；本轮不可沿用该结论，必须具体复现。

## 工作区与环境

- 初始 tracked clean；未跟踪 `.playwright-mcp/`、`.trellis/tasks/09-28-credential-probe-real-chat/` 保留。
- Go 1.27.3 Windows amd64、Node v25.6.0、pnpm 11.17.0 可用；PATH 中无 make。
- 生产库不测试；MySQL/PostgreSQL 无明确测试 DSN 时仅使用既有 opt-in 和方言合同测试。
