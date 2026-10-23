# 交付记录

## 兼容合并

- 用户指定兼容合并 `upstream/main`，核定新提交仅 `dd33ae3`（两个订阅卡片余额显示表达式）。
- 用户在规划摘要后继续安排线上 Gemini 排查，前面的兼容合并范围保持不变；本会话将两项工作分别处理。
- 实现 agent 已执行真实 Git merge，无冲突，保留上游提交祖先关系。除两个 `n(Math.ceil(balance))` 表达式外没有产品代码修改。
- 定向及全量 ESLint、前端类型检查、现代样式检查、Vite 构建、Go vet、Go build 和差异检查通过。
- Windows 无 make，检查 agent 以等价命令验证。裸 pnpm 命中系统 10.32.1，与项目锁定的 11.17.0 不一致，嵌套脚本失败后使用显式 corepack 调用各阶段；未更改全局工具。
- 原始 Prettier/gofmt/tidy 检查受已有 CRLF 检出影响：Prettier `--end-of-line auto` 检查通过，tidy 的 go.sum 差异逐行忽略换行后相同。未格式化全仓库或重写依赖文件。
- 唯一一轮 `go test -count=1 . ./internal/...` exit 1：`catalog`、`container`、`gateway`、`platform/authkey`、`webui` 共 5 个包、57 个顶层测试失败。包括 Windows 路径/日志断言、模型目录摘要、环境代理、慢 TCP reader deadline、bash/符号链接/发布环境及工作流解析等；这些包及其 .github/Dockerfile/go.mod/go.sum 输入相对 HEAD 未变。未运行原 HEAD baseline，故不声称所有失败均已在原版本复现；未修复无关后端代码。完整测试名和错误摘要见 ignored `tmp/upstream-merge-quality-20261023.md`。
- 产品变更已提交为真实 merge `a3c71d5c`，父提交包含 `dd33ae3`；没有推送或部署。

## 规格审阅

余额修复沿用现有显示和本地化模式，没有新增接口、字段、迁移、基础设施或工程约定，无需修改项目 spec。Windows 的本次工具版本和换行符问题属于环境事实，不作为跨项目规则写入。

## 用户追加的线上 Gemini 诊断

- 对 yunyou-99 的 gpt-load-2 只读查询及少量串行微型推理请求，未修改部署、凭据或分组配置。
- 采样前 24 小时 gemini-3.8-flash：39 次请求，11 次成功、17 次上游 503、8 次 120 秒超时、3 次无效请求格式。
- 直连 Google 官方域名可以复现相同 503。相同 Key 调用其他模型成功，同一 Key 的 3.8 Flash 先 503、稍后也成功，支持间歇性上游问题而非普遍 Key 失效。
- 现有默认策略 `upstream.host_error.response_retry` 的 effect 为 `skip_group`，只在本次请求 iterator 内跳过 Gemini 分组，不会冷却或拉黑全部 Key；该策略使同组多 Key 没有在当前请求内恢复的机会。
- 建议为客户端添加有上限的退避重试，或经用户确认后仅在 Gemini 分组试验针对 `503 + high demand` 的 `next_candidate / none` 规则；该生产规则本次未应用。
- 详细证据与配置草案保存在本机 ignored `tmp/diagnostics/gemini-503-20261023.md` 和 `gemini-high-demand-rule-proposal.json`，无明文凭据。未通过另一网络出口复测，不能确定 Google 全球/地区资源池的具体责任范围。
