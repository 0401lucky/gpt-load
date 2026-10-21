# 前端兼容合并与验证

## 范围与结果

固定 fork 起点 `216024cc31c5de4c31a11777c41746bf9ac752d3`，固定上游目标 `41c82ac02be5c26bd8851256236036d404e4829b`。前端 owner 仅处理 `web/**` 与本报告；未操作 Git 索引、分支、提交或任务状态。

两个 modern 文本冲突已在工作树解决，保留先前 implementer 的兼容实现并完成复核：

- `CredentialDetailPanel.vue`：备注位于上游移动端 settings 分区，与权重及代理表单共存；dirty 包含备注和别名草稿。名称与设置保存共用请求所有权、独立 AbortController、当前身份及卸载守卫，设置保存不能绕过 nameDirty/namePending。刷新只在表单干净且没有写入请求时接纳数据。
- classic 更新响应缺少 modern `note`；名称单独保存向列表回填持久备注，详情缓存仅改 name/label，保留备注草稿。设置保存按提交值回填备注；`updateCredential` 对显式 `note:null`/空串均回填空串。列表中的 `GroupCredentials.vue` 名称保存入口继续保留当前持久备注。
- `CredentialTestDialog.vue`：上游 optional row/batchCount 合同与 fork 单项 HTTP 状态、错误摘要、重试等待详情共存。单项捕获 group/credential/proof 后请求；没有 row 或恢复证明时不发请求。批量入口仅 emit start，单项结果才显示恢复按钮。模型、协议或对象变化及卸载都会废弃在途请求，旧响应不写回结果。

`GroupDetailView.vue` 保留先前移动兼容修复：模型用量矩阵与紧凑布局总览放在同一分区，桌面继续可见，紧凑布局的凭据和设置分区不重复显示矩阵。刷新、更新时间和删除后的缓存清理仍覆盖 model usage。

## 自动合并复核

- classic 凭据投影白名单、DTO、父级结果裁剪及对话框完整保留 `upstream_status_code`、`error_summary`、`retry_after_seconds`；restore_proof 只在父级请求状态持有，不渲染到 DOM。
- 两套前端批量测试均使用共享 `useCredentialTestBatch`，当前轮次控制器拒绝旧回写，停止统一标记取消，重测只重排未通过目标，分组切换清理报告。批量 API key 测试入口、进度和单项诊断没有互相覆盖。
- fork `rate_limit_reset_hint_enabled` 在 classic 白名单、两套 DTO、投影、patch 克隆及 UI 开关中保留；modern runtimeSwitches 仍登记。上游 error_rules 有独立投影、编辑器和保存路径，两者共存。
- classic 模型用量 tab/route/query key/资源/失效计划保持，modern 矩阵保留 reset/fallback_24h、dataComplete 与 missing/partial 提示；显示冷却与统计窗口来自 API 的独立字段。
- 请求日志新增 request_body 投影扩展保留既有字段和严格白名单；访问密钥记录开关及登录提示已在两套 UI 接纳。
- `web/src` 没有 donation 管理入口，捐献独立鉴权与生命周期的验证归后端 owner；未为本轮合并添加新入口。

## 工具与门禁

Node `25.6.0`。固定 pnpm `11.17.0` 位于临时安装目录：

```text
C:/Users/lucky0401/AppData/Local/Temp/gpt-load-pnpm-11.17.0-20261021/node_modules/pnpm/bin/pnpm.cjs
C:/Users/lucky0401/AppData/Local/Temp/gpt-load-pnpm-11.17.0-20261021/node_modules/.bin/pnpm.cmd
```

每次命令仅将该 `.bin` 前置到进程 PATH，确保脚本内部的 `pnpm run` 同样使用固定版本。未更改全局配置。

| 检查 | 结果 |
| --- | --- |
| `pnpm --dir web install --frozen-lockfile` | 通过，Already up to date；pnpm v11.17.0 |
| `pnpm --dir web run lint` | 通过；ESLint `--max-warnings=0` 及新版视觉变量/公共控件/响应断点检查 |
| `pnpm --dir web run format` | 最终通过；All matched files use Prettier code style |
| `pnpm --dir web run build` | 通过，含 `vue-tsc --noEmit -p tsconfig.app.json` 与 `tsc --noEmit -p tsconfig.node.json`；Vite 8.3.4，4004 modules，34.51s |
| `git -c core.autocrlf=false diff --check -- web` | 通过 |
| `web/src` 行首冲突标记扫描 | 无匹配；Git 索引解决由主会话统一处理 |

首轮 format 报 273 个文件，原因主要为本机 `core.autocrlf=true` 检出的 CRLF。经主会话授权，仅将 276 个已跟踪 web 文本文件 CRLF 转为 LF，二进制和未跟踪/忽略内容未处理；与索引比较没有增加无关内容差异。之后仅 `CredentialDetailPanel.vue` 剩一处实际折行差异，使用仓库 Prettier 定向修复。未运行全目录 format:write。构建产生既有忽略目录 `internal/webui/dist`，供主会话后端门禁使用。

## 三语言与验证限制

使用临时 TypeScript AST 检查比较 11 个改动 namespace：classic access-keys/core/group/import/monitor，modern credential-cards/group-detail/access-keys/logs，以及共享 credential-test-messages/error-rule-messages。本次新增键集合均一致；10 个 namespace 全量一致。

另外对所有 7 个本轮改动的 modern 分语言模块（access-keys、credential-cards、group-create、group-detail、logs、request-redaction、ui）及三份根 locale 的键集合复核，全部一致。

classic/monitor 日文已有三项缺键：`monitor.logs.drawer.affinityPromptCacheKey`、`affinityPromptPrefix`、`affinityResponseContinuity`。对固定 HEAD 与 MERGE_HEAD 的源码读取确认两者均已有同样缺口，本次合并没有新增该差异；按聚焦范围记录，不夹带修复。

按项目规范未新增前端测试框架。没有运行浏览器 E2E、移动端实际点击、真实上游探测或生产数据操作；前端结论基于源码合同复核和上述静态门禁。后端契约回归与整体质量门禁由主会话汇总。
