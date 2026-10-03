# 待确认的本地提交

单个提交：`fix(usage): 隔离模型冷却与额度周期并提示用量缺失`

包含本任务代码、测试、文档和本任务目录：

```text
.trellis/spec/backend/database-guidelines.md
.trellis/tasks/10-03-model-usage-cycle-fix/
README.md
README_CN.md
README_JP.md
internal/control/group_model_usage.go
internal/control/group_model_usage_test.go
internal/execution/bifrost/cline_test.go
internal/gateway/handler.go
internal/gateway/handler_test.go
internal/health/decision.go
internal/health/decision_test.go
internal/health/execution_judge.go
internal/health/model_cooldown_test.go
internal/health/reset_hint.go
internal/health/reset_hint_test.go
internal/requestlog/group_model_usage_test.go
internal/state/model_cooldown.go
internal/state/model_cycle_test.go
internal/state/runtime_checkpoint.go
web/src/frontends/classic/api/control/types.ts
web/src/frontends/classic/app/resources/group-model-usage.ts
web/src/frontends/classic/features/groups/usage/GroupModelUsageTab.vue
web/src/frontends/classic/i18n/locales/en-US/group.ts
web/src/frontends/classic/i18n/locales/ja-JP/group.ts
web/src/frontends/classic/i18n/locales/zh-CN/group.ts
web/src/frontends/modern/api/group-model-usage.ts
web/src/frontends/modern/features/groups/GroupModelUsageMatrix.vue
web/src/frontends/modern/i18n/locales/group-detail.ts
```

不包含既有 `.playwright-mcp/`、`.trellis/tasks/09-28-credential-probe-real-chat/` 或 ignored 临时文件。

验证和限制详见 research/validation.md。该计划只提交本地分支，不 push、不发布镜像、不部署服务器。
