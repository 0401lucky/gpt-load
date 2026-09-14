# 本地提交分组（已获确认）

2026-09-14。实现与本地验收已经完成；用户随后明确回复“可以提交归档了”，已授权按本计划执行。以下按实际目标仓库依序提交，完整机器可读清单见 [commit-plan.json](commit-plan.json)，验收见 [final-acceptance.md](final-acceptance.md)。完成哈希记入会话日志，未推送或发布。

项目 `.trellis/workflow.md` Phase 3.4 第5步明确要求：**“Present the plan once, ask for one-shot confirmation”**。用户确认后才依序创建以下工作提交，之后进行四个已完成任务的归档及会话日志提交；不做amend或推送。

既有四个任务目录最初未跟踪。本计划明确将它们的既定规划和本轮续写、研究、验收一起版本化，不新建替代任务。45份验证日志共约605KiB，均为本次本地测试证据；因仓库默认忽略`*.log`，只对下列明确列出的日志逐一强制加入，不扩大为其它被忽略文件。

## 1. gpt-load：feat(donation): 实现受限密钥接收与长期来源记录

工作目录：`D:/code/Claude code program/gpt-load`；44个文件。

```text
.env.example
.trellis/spec/backend/database-guidelines.md
.trellis/spec/backend/directory-structure.md
.trellis/spec/backend/donation-caller-contract.md
.trellis/spec/backend/donation-integration.md
.trellis/spec/backend/index.md
README.md
README_CN.md
README_JP.md
internal/container/container.go
internal/control/access_keys.go
internal/control/auth.go
internal/control/bootstrap.go
internal/control/donation_batches.go
internal/control/donation_catalog.go
internal/control/donation_catalog_test.go
internal/control/donation_concurrency_test.go
internal/control/donation_database_integration_test.go
internal/control/donation_http.go
internal/control/donation_http_test.go
internal/control/donation_probe_integration_test.go
internal/control/donation_recovery_test.go
internal/control/donation_resources.go
internal/control/donation_worker.go
internal/control/donations_test.go
internal/control/group_write.go
internal/control/http_routes.go
internal/control/runtime.go
internal/control/server.go
internal/control/service.go
internal/platform/config/config.go
internal/platform/config/donation_test.go
internal/platform/errors/errors.go
internal/platform/httproute/donation_test.go
internal/platform/httproute/registry.go
internal/platform/i18n/locales/en-US.go
internal/platform/i18n/locales/ja-JP.go
internal/platform/i18n/locales/zh-CN.go
internal/storage/database_integration_test.go
internal/storage/db_test.go
internal/storage/migration.go
internal/storage/migration_test.go
internal/storage/migrations/0015_donation_intake.go
internal/storage/models/donation.go
```

## 2. new-api：feat(donation): 实现永久奖励账本与批次恢复

工作目录：`D:/code/Claude code program/new-api`；18个文件。

```text
controller/donation.go
controller/donation_integration_test.go
main.go
model/donation.go
model/donation_batch.go
model/donation_retention_test.go
model/donation_reward.go
model/donation_secret.go
model/donation_test.go
model/main.go
router/api-router.go
router/donation-router.go
service/authz/authz_test.go
service/authz/resources_donation.go
service/donation.go
service/donation_client.go
service/donation_test.go
service/text_quota_test.go
```

## 3. new-api：feat(web): 增加捐献页面与活动管理

工作目录：`D:/code/Claude code program/new-api`；46个文件。

```text
web/src/components/layout/__tests__/donation-navigation.test.tsx
web/src/components/layout/components/app-header.tsx
web/src/components/layout/components/public-header.tsx
web/src/components/layout/components/top-nav.tsx
web/src/features/donations/__tests__/fixtures.tsx
web/src/features/donations/__tests__/management.test.tsx
web/src/features/donations/__tests__/session-requests.test.ts
web/src/features/donations/__tests__/submission-recovery.test.tsx
web/src/features/donations/__tests__/submission.test.tsx
web/src/features/donations/api.ts
web/src/features/donations/components/batch-history.tsx
web/src/features/donations/components/batch-results.tsx
web/src/features/donations/components/campaign-form.tsx
web/src/features/donations/components/connection-form.tsx
web/src/features/donations/components/donation-layout.tsx
web/src/features/donations/components/item-results.tsx
web/src/features/donations/components/record-detail.tsx
web/src/features/donations/components/record-filters.tsx
web/src/features/donations/components/risk-notice.tsx
web/src/features/donations/components/submission-form.tsx
web/src/features/donations/hooks/use-donation-session.ts
web/src/features/donations/hooks/use-donation-submission.ts
web/src/features/donations/index.tsx
web/src/features/donations/lib/access.ts
web/src/features/donations/lib/labels.ts
web/src/features/donations/lib/schema.ts
web/src/features/donations/records.tsx
web/src/features/donations/settings.tsx
web/src/features/donations/types.ts
web/src/features/system-settings/maintenance/config.ts
web/src/features/system-settings/maintenance/header-navigation-section.tsx
web/src/hooks/use-top-nav-links.ts
web/src/i18n/locales/en.json
web/src/i18n/locales/fr.json
web/src/i18n/locales/ja.json
web/src/i18n/locales/ru.json
web/src/i18n/locales/vi.json
web/src/i18n/locales/zh-TW.json
web/src/i18n/locales/zh.json
web/src/i18n/static-keys.ts
web/src/lib/admin-permissions.ts
web/src/lib/nav-modules.ts
web/src/routeTree.gen.ts
web/src/routes/_authenticated/donations/index.tsx
web/src/routes/_authenticated/donations/records.tsx
web/src/routes/_authenticated/donations/settings.tsx
```

## 4. gpt-load：docs(donation): 记录规划与跨仓库验收

工作目录：`D:/code/Claude code program/gpt-load`；89个文件。

```text
.trellis/tasks/09-14-donation-intake/check.jsonl
.trellis/tasks/09-14-donation-intake/design.md
.trellis/tasks/09-14-donation-intake/implement.jsonl
.trellis/tasks/09-14-donation-intake/implement.md
.trellis/tasks/09-14-donation-intake/prd.md
.trellis/tasks/09-14-donation-intake/research/database-verification.md
.trellis/tasks/09-14-donation-intake/research/implementation-handoff.md
.trellis/tasks/09-14-donation-intake/research/intake-e2e-build.log
.trellis/tasks/09-14-donation-intake/research/intake-focused-tests.log
.trellis/tasks/09-14-donation-intake/research/intake-make-check-linux.log
.trellis/tasks/09-14-donation-intake/task.json
.trellis/tasks/09-14-donation-rewards-backend/check.jsonl
.trellis/tasks/09-14-donation-rewards-backend/design.md
.trellis/tasks/09-14-donation-rewards-backend/implement.jsonl
.trellis/tasks/09-14-donation-rewards-backend/implement.md
.trellis/tasks/09-14-donation-rewards-backend/prd.md
.trellis/tasks/09-14-donation-rewards-backend/research/backend-final-test.log
.trellis/tasks/09-14-donation-rewards-backend/research/backend-test.log
.trellis/tasks/09-14-donation-rewards-backend/research/backend-vet.log
.trellis/tasks/09-14-donation-rewards-backend/research/check-handoff.md
.trellis/tasks/09-14-donation-rewards-backend/research/database-final-default.log
.trellis/tasks/09-14-donation-rewards-backend/research/database-minimum-final.log
.trellis/tasks/09-14-donation-rewards-backend/research/database-minimum.log
.trellis/tasks/09-14-donation-rewards-backend/research/database-modern.log
.trellis/tasks/09-14-donation-rewards-backend/research/database-retention.log
.trellis/tasks/09-14-donation-rewards-backend/research/fixed-price-fixture-before.log
.trellis/tasks/09-14-donation-rewards-backend/research/implementation-handoff.md
.trellis/tasks/09-14-donation-rewards-backend/research/model-retention-regression.log
.trellis/tasks/09-14-donation-rewards-backend/research/model-sqlite-test.log
.trellis/tasks/09-14-donation-rewards-backend/research/retention-verification.md
.trellis/tasks/09-14-donation-rewards-backend/research/service-final-test.log
.trellis/tasks/09-14-donation-rewards-backend/research/service-test.log
.trellis/tasks/09-14-donation-rewards-backend/task.json
.trellis/tasks/09-14-donation-web/check.jsonl
.trellis/tasks/09-14-donation-web/design.md
.trellis/tasks/09-14-donation-web/implement.jsonl
.trellis/tasks/09-14-donation-web/implement.md
.trellis/tasks/09-14-donation-web/prd.md
.trellis/tasks/09-14-donation-web/research/check-global-lint.log
.trellis/tasks/09-14-donation-web/research/check-handoff.md
.trellis/tasks/09-14-donation-web/research/check-tests.log
.trellis/tasks/09-14-donation-web/research/check-typecheck.log
.trellis/tasks/09-14-donation-web/research/copyright-check.log
.trellis/tasks/09-14-donation-web/research/draft-regression-after.log
.trellis/tasks/09-14-donation-web/research/draft-regression-before.log
.trellis/tasks/09-14-donation-web/research/final-build.log
.trellis/tasks/09-14-donation-web/research/final-lint.log
.trellis/tasks/09-14-donation-web/research/final-tests.log
.trellis/tasks/09-14-donation-web/research/final-typecheck.log
.trellis/tasks/09-14-donation-web/research/format-check.log
.trellis/tasks/09-14-donation-web/research/frontend-tests.log
.trellis/tasks/09-14-donation-web/research/i18n-keys.json
.trellis/tasks/09-14-donation-web/research/i18n-verification.md
.trellis/tasks/09-14-donation-web/research/implementation-handoff.md
.trellis/tasks/09-14-donation-web/research/initial-build.log
.trellis/tasks/09-14-donation-web/research/initial-typecheck.log
.trellis/tasks/09-14-donation-web/research/interaction-tests.log
.trellis/tasks/09-14-donation-web/research/lint.log
.trellis/tasks/09-14-donation-web/research/navigation-final-tests.log
.trellis/tasks/09-14-donation-web/research/navigation-typecheck.log
.trellis/tasks/09-14-donation-web/research/review-regressions-after.log
.trellis/tasks/09-14-donation-web/research/review-regressions-before.log
.trellis/tasks/09-14-donation-web/research/session-test.log
.trellis/tasks/09-14-donation-web/research/touched-format-copyright.json
.trellis/tasks/09-14-donation-web/research/touched-lint-fix.log
.trellis/tasks/09-14-donation-web/research/touched-lint.log
.trellis/tasks/09-14-donation-web/research/typecheck-with-tests.log
.trellis/tasks/09-14-donation-web/research/typecheck.log
.trellis/tasks/09-14-donation-web/task.json
.trellis/tasks/09-14-key-donation-rewards/check.jsonl
.trellis/tasks/09-14-key-donation-rewards/design.md
.trellis/tasks/09-14-key-donation-rewards/implement.jsonl
.trellis/tasks/09-14-key-donation-rewards/implement.md
.trellis/tasks/09-14-key-donation-rewards/prd.md
.trellis/tasks/09-14-key-donation-rewards/progress.md
.trellis/tasks/09-14-key-donation-rewards/research/browser-final.log
.trellis/tasks/09-14-key-donation-rewards/research/browser-preview.log
.trellis/tasks/09-14-key-donation-rewards/research/browser-validation.md
.trellis/tasks/09-14-key-donation-rewards/research/codebase-findings.md
.trellis/tasks/09-14-key-donation-rewards/research/commit-plan.json
.trellis/tasks/09-14-key-donation-rewards/research/commit-plan.md
.trellis/tasks/09-14-key-donation-rewards/research/cross-service-validation.md
.trellis/tasks/09-14-key-donation-rewards/research/final-acceptance.md
.trellis/tasks/09-14-key-donation-rewards/research/integration-contract.md
.trellis/tasks/09-14-key-donation-rewards/research/new-api-conventions.md
.trellis/tasks/09-14-key-donation-rewards/research/new-api-copyright-baseline.log
.trellis/tasks/09-14-key-donation-rewards/research/new-api-format-baseline.log
.trellis/tasks/09-14-key-donation-rewards/research/security-controls.md
.trellis/tasks/09-14-key-donation-rewards/task.json
```

## 不纳入任何提交

- new-api原有六张`clover-*.png`，全部哈希与开工前一致：

```text
clover-config-drawer.png
clover-dashboard-light.png
clover-pricing-dark.png
clover-pricing-light.png
clover-signin-dark.png
clover-signin-light.png
```

- gpt-load的`.playwright-mcp/`工具产物及`tmp/`截图/二进制。
- 仓库外的临时数据库、验证工具与LF检查工作区；旧preview目录按自动审批拒绝保持原状。
- 没有发现其它身份不明的业务源码改动。

## 工作提交之后

依次用Trellis归档三个已完成子任务和父任务，然后用工作提交哈希记录会话日志。归档与日志提交均放在上述工作提交之后；保留Git历史，不将本地验收表述为生产部署。
