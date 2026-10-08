# 最终验证记录（2026-10-08）

## 固定输入与边界

- 本地合并提交：`b2cbb156b347495d822a607590f4633c153de4ae`；main已快进，两个输入均为祖先，上游待合并数0。原有两个未跟踪目录保留。
- fork：`dbb2b42b0bca07a83c7b0668ed5bfa549074409a`。
- upstream：`a5c691bb0559f856ebb587879d82e341ff480202`，17 个新增提交、338 个上游变更文件。
- 14 个文本冲突均已解决；额外修复捐献代理目录引用解析和 modern 名称单独保存时备注缓存丢失。
- 本轮未 push、发布镜像、部署或使用生产数据库/真实上游凭据。

## 门禁

本机没有 make，逐项执行 Makefile 等价步骤：

| 检查 | 结果 |
| --- | --- |
| pnpm frozen install | 通过，11.17.0 |
| Go 源码格式 | 通过，1318 个现存且未忽略的 Go 文件，无格式差异 |
| 根 module go mod tidy -diff | 通过 |
| 根 module go vet ./... | 通过 |
| 前端 lint / styles | 通过 |
| 前端 format | 通过 |
| 前端 build（含 vue-tsc / tsc） | 通过，3918 modules |
| go build | 通过，临时目录内 gpt-load-upstream-20261008.exe |
| go test -json -count=1 . ./internal/... | 完整执行：64 包通过、8 包无测试/跳过、5 包失败；见本轮基线 |
| CPA 独立 module tidy -diff / vet | 均通过 |
| git diff --check | 通过 |

不能将本轮门禁描述为全绿；Windows 的既有环境/平台失败仍存在。源码格式检查按 Git 跟踪/未忽略的现存源码执行，避开仓库中原有忽略目录里的历史副本。

## 兼容专项

- 存储完整测试通过，24→32 和27→32真实SQLite升级、数据保存与重复执行均通过。旧27个生产迁移源码不变，新5个迁移除编号/导出引用/来源注释外与上游一致；详见 `storage.md`。
- control 完整测试通过，包含 name/note 组合写入、4个读接口差异、Unicode边界、无效更新原子拒绝、健康/身份/可信周期不变。
- 捐献代理回归修复前复现失败，修复后验证实际解析URL、代理改名不改修订、改URL改变自动/人工修订、旧修订拒绝与停用回退；详见 `backend.md`。
- 两套前端保留模型用量、缺失用量提示、冷却设置和连接诊断。modern 别名/备注/代理可共存；详见 `frontend.md`。
- gateway 中 `SetModelCooldownWithReset` 与可信额度周期保留，优先级重试来自上游；模型用量的完整小时桶+边界明细逻辑不变。GHCR workflow和捐献独立鉴权保留。
- 最终跨范围复核见 `review.md`。

## 本轮基线与环境

在仓库外 `%TEMP%/gpt-load-upstream-20261008-dbb2b42b` 使用 `git -c core.autocrlf=false worktree add --detach` 固定旧版本；保留合并前嵌入UI产物，使用同一Go和Windows环境执行完整测试。

基线结果为62包通过、8包无测试/跳过、6包失败，24个顶层失败测试；合并结果为64/8/5，23个顶层失败。比较包括子测试的失败集合，合并结果没有新增失败；基线额外出现一次Bifrost首包超时用例失败，合并版本通过。

既有失败集中于：catalog/authkey 的Windows路径转义断言、container的环境代理预期、gateway真实TCP慢读端时限，以及webui中Bash/Windows路径、Python命令与脚本执行环境。逐用例和消息核对记录见 `baseline.md`。

初始gofmt的13个文件及前端format的5个文件仅CRLF不同；逐字证明LF内容等于HEAD后归一，未产生额外Git内容差异。pnpm内部裸命令最初命中10.32.1，使用临时corepack shim和进程PATH解决，未改全局配置。

原始日志在 `%TEMP%/gpt-load-upstream-20261008-{baseline,merged}-tests.jsonl`，标准错误文件为空。

## 未验证范围

- 未配置外部数据库DSN，真实MySQL/PostgreSQL未实测，保留方言实现与opt-in用例。
- 未执行浏览器交互测试、真实上游请求、镜像发布或部署。
- Windows既有失败未通过跳过用例、放宽语义断言或无关生产改动掩盖。
