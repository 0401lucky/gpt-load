# 分组级：按上游 429 报错里的重置时间冷却单个模型凭据

## Goal

让 cline 分组在某个 key 的某个模型被上游限流时，把这个 (key, 模型) 组合冷却到上游给出的真实重置时间，而不是现在的固定 1 分钟；同一个 key 的其他模型、以及分组内其他 key 不受影响。当某个模型在所有 key 上都处于冷却时，请求不再打到上游，直接返回 429，并明确说明「该模型暂无可用账号」及预计恢复时间。

## Background

### 业务背景

- 上游 `https://api.cline.bot/api/v1`（分组 `cline`，group_id=4，45 个凭据、4 个模型）的免费额度按 **key × 模型** 每天计算：同一个 key 在 `deepseek-v4.1-flash` 上额度耗尽，不影响它在 `glm-5.3-flash` 上的额度。
- 额度耗尽时上游返回 429，响应体写明重置时间，例如 `Error 429: Daily free limit reached on model z-ai/glm-5.3-flash. Try again in 3h 18m`。
- 目前只有 cline 分组的上游返回这种带模型名与重置时间的 429，因此本功能设计为分组级可选开关，默认关闭。

### 生产证据（2026-09-23 ~ 09-24，容器 f7f31f1d）

- 请求 `bb9afc25-16db-48f7-b3ed-3ac0092493c8`：第 1 次尝试用凭据 482 调 `z-ai/glm-5.3-flash` 得到 429，判定 `failure_scope=model`、`effect=cooldown_model`、`rule_id=rate_limit.model.default_cooldown`，`cooldown_until_ms` 距该次尝试 **60 秒**；第 2 次换凭据 447 成功。说明「按 (key, 模型) 冷却 + 换 key 重试」已工作，但上游写的 `4h 17m` 被忽略。
- 另有请求 `attempts=3`、`err=upstream_rate_limited`、`err_msg` 为上述 429 原文：1 分钟冷却使额度耗尽的 key 快速回到轮询，单个请求连抽 3 个耗尽 key 后把上游原始 429 透传给了 new-api。
- 分组列表接口 `credential_counts` 已含 `model_cooldown` 计数；管理界面访问历史中 `/api/groups/4/settings` 与 `/api/modern/groups/4/credentials` 并存，即 modern 设置页仍在读 classic 端点字段。

### 代码现状（对应上游 #599 引入的按模型冷却）

- 按 (凭据, 上游模型) 冷却的状态已存在：`internal/state/model_cooldown.go`（内存 + 检查点持久化）、`internal/scheduler/model_cooldown.go`（候选过滤）、`internal/control/model_cooldown.go`（管理侧展示与手动恢复）。
- 冷却键是**实际发给上游的模型名**（上游模型 ID），既不是客户端模型名，也不是报错文本里的模型名（生产上客户端 `deepseek-v4.1-flash` 实际发上游的是 `cline-free/deepseek-v4.1-flash`）。
- 限流判定见 `internal/health/execution_judge.go:582-637`：证据 `RetryAfter > 0` 时采用它（`rate_limit.retry_after`），否则只认 `Retry-After` 响应头（`internal/health/ratelimit.go`，上限 1 小时），都没有才退回默认冷却。
- API key 通道的默认冷却固定 1 分钟（`internal/gateway/handler.go:45`）。判断 Cline 未返回 `Retry-After` 头的依据：命中规则是 `rate_limit.model.default_cooldown` 而非 `rate_limit.retry_after`，且后者只在证据与响应头皆空时才走到。
- 全冷却出口：`internal/gateway/handler.go:1524-1528` → `internal/gateway/reason.go:39`，429 + code `upstream_rate_limited` + 文案 "Upstream rate limit exceeded."，`Retry-After` 为最早恢复时间。
- 每个请求最多尝试「系统设置 `retry_count` + 1」次，默认 3 次；分组级 `retry_count` 被忽略。
- 冷却状态在正常重启/升级时经检查点文件恢复，异常崩溃会丢失，代价是每个耗尽的 (key, 模型) 多试错一次。

## Requirements

- **R1 分组级开关**：新增分组级布尔配置 `rate_limit_reset_hint_enabled`，默认关闭，未设置时继承系统级默认（关闭）。两套管理界面（classic 与 modern）的分组设置页都要显示且能切换它，读写往返不丢值。
- **R2 冷却时长解析**：开关开启时，上游 429 若带可解析的「稍后重试」时长（如 `Try again in 3h 18m`），把该 (凭据, 上游模型) 的冷却时长设为「解析值 + 1 分钟余量」，并钳制到 [1 分钟, 24 小时]；判定规则标记为 `rate_limit.reset_hint`。冷却只作用于该凭据的该模型。
- **R3 失败回退**：开关关闭，或文本缺失/解析失败/超出上下限时，行为与现在完全一致（默认 1 分钟冷却），不产生新副作用。
- **R4 全冷却时的对外报错**：某个模型在分组内所有可用凭据上都处于冷却时，不发起尝试，返回 429、`Retry-After` 为最早恢复时间，响应文案明确表达「该模型暂无可用账号」并给出预计恢复时间。`code` 保持 `upstream_rate_limited` 不变，仅更换文案。该分支不依赖分组开关，对所有分组一致生效。
- **R5 可见性与恢复**：冷却中的 (凭据, 模型) 仍可通过现有管理接口查看，并可沿用现有手动恢复能力立即解除冷却。

## Acceptance Criteria

- [ ] AC1 全局默认关闭：未在任何分组设置该开关时，`GET /api/groups/:id/settings` 的 `effective` 中该键为 `false`，`overrides` 为空。
- [ ] AC2 分组级开启：置为 `true` 后，classic 与 modern 两套分组设置页都能正确显示为「开」，且「开启 → 保存 → 重新读取仍为 true」往返成功，classic 分组页不报错。
- [ ] AC3 解析生效：开关开启时，对含 `Try again in 3h 18m` 的上游 429，冷却截止时间约等于「尝试时刻 + 3h19m」，判定规则为 `rate_limit.reset_hint`，且只写入该 (凭据, 上游模型) 的冷却。
- [ ] AC4 同 key 其他模型不受影响：某凭据在模型 A 上被冷却后，针对模型 B 的请求仍可选中该凭据（自动化测试覆盖）。
- [ ] AC5 解析失败回退：文本不可解析、结果为 0、或超出 24 小时上限时，冷却回退为默认 1 分钟（超限时钳制为 24 小时），行为与开关关闭时一致。
- [ ] AC6 开关关闭无副作用：开关关闭时，同一上游 429 的冷却时长与判定规则与改动前完全一致（默认 1 分钟、`rate_limit.model.default_cooldown`）。
- [ ] AC7 全冷却出口文案：构造「某模型全部凭据冷却」场景，返回 429、`Retry-After` 等于最早恢复时间、响应体含模型名与「无可用账号」语义，且请求未打到上游。
- [ ] AC8 手动恢复仍可用：冷却期间经现有恢复接口清除该凭据的模型冷却后，下一次请求可立即选中该 (凭据, 模型)。
- [ ] AC9 兼容与门禁：不新增数据库迁移（配置存于分组 `overrides`）；SQLite/MySQL/PostgreSQL 行为一致；`make check` 通过。

## Out of Scope

- 不改动「按 (凭据, 模型) 冷却」机制本身，也不关闭它；开关只决定是否解析上游报错文本里的重置时间。
- 不做按分组的可配置解析模式、正则或时长上下限（上下限与余量为固定内置值）。
- 不改「无候选凭据」分支的 503 `no_available_candidate` 文案。
- 不改动最大尝试次数或系统级 `retry_count` 语义。
- 不覆盖「200 + SSE 错误事件」形式的流式限流（生产实测 Cline 直接返回 HTTP 429，未观测到该形态）。
- 不含服务器部署与生产配置变更（部署与开启开关由用户另行决定）。

## Technical Notes

- 见 `design.md`（落点、契约、解析规则、权衡）与 `implement.md`（实施顺序、验证命令、回滚点）。
- 关键规范：新增分组配置键不需要迁移（`groups.overrides` 是 JSON 列）；classic 前端对字段有严格白名单，漏登记会让分组页整页报错；两套前端保存时都整块替换 `overrides`。门禁为 `make check`（`.trellis/spec/backend/quality-guidelines.md`）；`internal/health` 不得 import `storage`/`control`/`gorm`（`.trellis/spec/backend/directory-structure.md`）。
- 运维影响：开关置 `true` 后回退到不认识该键的旧镜像，旧版本会拒绝加载配置，回退前必须先关闭开关。

## Decisions

- D1 全冷却出口采用方案 A（用户确认）：保持 429 与 `Retry-After`，仅更换文案为「该模型暂无可用账号 + 预计恢复时间」，`code` 保持 `upstream_rate_limited`。
- D2 开关在两套界面都显示（用户确认）：classic 与 modern 设置页都登记并渲染该键。
- D3 解析触发锚点固定为 `try again in`，时长支持 `d/w/h/m/s`；余量 1 分钟，上限 24 小时，下限 1 分钟（低风险细节，实现期如需调整会在验收前说明）。
- D4 出口文案采用英文网关风格：`No available account for model <model>. Earliest recovery in about <duration>.`；模型名不可得时使用不含模型名的退化措辞。
- D5 开关是**分组专属键**，不登记进 `IsRuntimeSettingKey`（用户确认，实现期发现并决策）：全局 PUT（`internal/control/settings.go:258`）与全局 `overrides` 投影（`:383`）都按该判定放行，登记进去会让一次全局写入就把该键推给前端；而 classic（`app/resources/settings.ts:230`）与 modern（`api/settings.ts:171`）都对未知 `overrides` 键硬失败，**两套前端的全局设置页会整页报错**。改为照 `parameter_overrides` 先例收口后，全局写入被拒（端到端实测 `Input validation failed`），分组读写与继承语义不受影响（分组写路径不经过该判定）。系统级仍保留默认值与继承基准。守护写法见 `internal/state/runtime_settings_test.go` 的反向断言。
