# 前端兼容合并与验证（2026-10-02）

## 改动边界

前端实现子代理负责 `web/**`、`README.md`、`README_CN.md`、`README_JP.md`。未执行 Git 暂存、提交、分支或合并操作；后端与迁移由其他代理负责。

手工解决 5 个前端文件与 3 个 README 的文本冲突：

- classic `api/control/types.ts`：运行时 overrides/effective 同时包含 `rate_limit_reset_hint_enabled` 和 `empty_response_retry`。
- classic `app/resources/groups.ts`：严格白名单、两个本地 DTO 与布尔投影同时保留两个开关；保留上游 `concurrency_limit`、`codex_live_mode` 的校验。
- classic `features/groups/settings/group-settings-patch.ts`：clone/normalize/dirty/patch 保留两个开关（包含显式 `false`），以及上游并发数与语音模式。
- classic `features/groups/settings/GroupSettingsTab.vue`：两个开关各有完整 computed 与 toggle/set 函数；两个既有模板行均保留，冷却开关仍为该设置块最后一行。
- modern `api/group-detail.ts`：`runtimeSwitches` 同时登记两个开关；读取、必填 effective 检查与 `GroupAdvancedPanel.vue` 的表单/保存循环使用相同枚举。
- README 三语言保留捐献接收/人工审核与冷却说明，修正人工审核迁移链接为 `0019_donation_manual_review.go`；同步空响应重试、并发上限与实时语音的当前配置语义。尊重上游移除旧 WS/Responses 细节的变化，移除过期的 Codex 跨连接限制及日文“尚未接入 WS”说法。日文补齐既有冷却开关及 WebSocket 开关说明。中文降级说明明确必须清除未知配置键，单纯设为 false 并不足够。

## 自动合并审查

- 同时被 fork 与 upstream 修改的前端文件共 12 个；除 5 个文本冲突外，逐项审查了 classic `GroupCredentialsTab.vue`、三语言 group locale、modern `CredentialDetailPanel.vue`、`credential-cards.ts`、`group-detail.ts` locales。
- classic 连接测试仍保留 `upstream_status_code`、`error_summary` 与 `retry_after_seconds`；未丢失 fork 诊断信息。
- modern `readCredential` 同时解析 `note` 和可选 `rpm_peak_hour`。备注输入、dirty、保存 patch 保留；API key / subscription 卡片保留 RPM 峰值，详情保留 RPM 趋势。
- `GroupDetailView.vue` 保留模型用量 query、刷新、删除缓存清理与矩阵。矩阵标识仍通过分页凭据获取映射，优先备注、其次脱敏标识、最后内部 ID；独立 identity query key 不混入凭据分页缓存。
- modern `GroupAdvancedPanel.vue` 的读取、快照、dirty、保存和模板由共享 `runtimeSwitches` / `runtimeNumbers` 派生；`codex_live_mode` 有独立完整路径。
- 两套全局设置均未登记 `rate_limit_reset_hint_enabled`；与后端确认该键仍为分组专属，modern Note/RPM 仍未暴露到 classic 共享 DTO。

## 环境问题与处理

1. `corepack pnpm --dir web install --frozen-lockfile` 成功，使用 pnpm 11.17.0。镜像下载有慢速警告，最终安装成功、锁文件不变。
2. 初次直接运行 lint/build 时，script 内裸 `pnpm` 被 PATH 上 pnpm 10.32.1 接管，报 `ERR_PNPM_UNSUPPORTED_ENGINE`（期望 11.17.0）。ESLint 本身已执行完成，失败发生在后续 `pnpm run check:styles`；build 失败发生在启动 type-check 之前。
3. 临时 shim 位于 `%TEMP%\gpt-load-october-corepack-shims`，用 `corepack enable pnpm --install-directory <该目录>` 创建。每个验证子进程仅设置：

```powershell
$env:Path = (Join-Path $env:TEMP 'gpt-load-october-corepack-shims') + ';' + $env:Path
```

未修改全局工具配置、PATH 或安装版本。

4. 原工作树 format 对 629 个文件报错。经主会话授权，对 `git ls-files -z -- web` 去重后的现存文本文件仅做 CRLF→LF（685 个文件），与项目 `.editorconfig` 一致；跳过二进制与非跟踪文件，未产生额外 Git 内容差异。
5. LF 后 format 只剩 `GroupSettingsTab.vue` 的长行排版，执行 `corepack pnpm --dir web exec prettier --write src/frontends/classic/features/groups/settings/GroupSettingsTab.vue` 定向修复，未变更行为。

## 最终验证结果

| 命令 | 退出码 | 证据 |
| --- | --- | --- |
| `corepack pnpm --dir web install --frozen-lockfile` | 0 | Lockfile passes supply-chain policies，Done in 42.2s using pnpm v11.17.0 |
| `corepack pnpm --dir web run lint`（上述临时 PATH） | 0 | ESLint 零 warning；`新版视觉变量、公共控件复用与响应断点检查通过` |
| `corepack pnpm --dir web run format` | 0 | `All matched files use Prettier code style!` |
| `corepack pnpm --dir web run build`（上述临时 PATH） | 0 | vue-tsc 与 tsc 均通过；Vite 8.3.0 转换 3863 modules，built in 12.74s |
| Node 原生 TS 导入与递归键集合断言 | 0 | classic group、modern group-detail/credential-cards 的 zh-CN/en-US/ja-JP 键集合完全相同 |
| `git -c core.safecrlf=false diff --check -- web README.md README_CN.md README_JP.md` | 0 | 无输出 |
| 用 `rg` 搜索本范围的 Git 冲突标记 | 1 | 无匹配，即范围内无冲突标记 |

构建输出为项目既有忽略目录 `internal/webui/dist`。构建仅给出 Vite 插件耗时提示，无编译警告/失败。构建/lint 后唯一源码改动为定向 Prettier 空白修正，因此只重跑格式检查；行为和已生成产物一致。

## 未验证范围

按项目规范未新增前端测试框架、未启动浏览器验收；前后端合同与功能回归由主会话的 Go 测试补足。未运行生产服务、发布、推送或数据库操作。
