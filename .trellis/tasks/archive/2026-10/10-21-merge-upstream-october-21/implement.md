# 执行计划

- [x] 取得任务创建授权，加载项目规范，固定 fork/upstream 输入并只读预合并。
- [x] 完成 PRD 收敛、兼容设计、研究资料及真实上下文清单。
- [x] 展示最终规划摘要，用户回复「可以」，授权开始本地实现与验证。
- [x] 执行 `task.py start` 激活实现阶段；续接会话恢复当前任务指针，保留 `in_progress` 状态。
- [x] 核验 HEAD 与原有未跟踪文件；建立 `merge/upstream-20261021` 并发起不自动提交的 merge，固定 MERGE_HEAD 为 `41c82ac0`。
- [x] storage implement 子代理独占 `internal/storage/**`：解决 4 处冲突，顺延 0033、校准 fixture/语义边界、补旧 32→33 升级和幂等验证。研究输出 `research/storage.md`。
- [x] core implement 子代理独占 `internal/control/**`、`internal/gateway/**`、`internal/health/**`、`internal/state/**`，审查直接关联的 `internal/requestlog/**`：融合 quota cooldown 与可信周期，解决 10 处冲突，保留捐献恢复，补交叉回归；修复凭据重建时保留状态未同步调度投影的问题。
- [x] web implement 子代理独占 `web/**`：解决 2 处冲突，保留备注/别名、批量/单项探测及诊断，审查两套 UI，前端静态门禁通过。研究输出 `research/frontend.md`。
- [x] 主会话解决剩余 `internal/subscription/runtime/runtime_test.go` 冲突；审查 `main*`、`update*`、依赖、`third_party/cpaembedded/**`、发布脚本和 CI 自动合并，补 fork main 开发版本拒绝自更新回归。
- [x] 全部子代理完成后，主会话确认无冲突标记及索引未解决项；trellis-check 全范围审查通过，无新增阻断问题/额外源码修改，并重跑 lint/styles 与 type-check 通过。
- [x] 完整执行 Makefile 等价门禁：frozen pnpm install、1372 个实际源码 gofmt、根 tidy/vet、前端 lint/styles/format/build/type-check、Go build、完整根测试和 diff 检查。根测试为 65 pass / 8 skip / 5 fail，既有环境失败见本轮基线。
- [x] CPA 独立 module tidy/vet 和完整 `go test -count=1 ./...` 通过；开发版本 CLI 实际拒绝自更新，fork main 版本回归确认不会下载。
- [x] 在仓库外固定 `216024cc` 的本轮基线用相同 Go/Windows 环境完整复现，顶层 23 个、含子测试 82 个失败完全一致，新增/消除失败集合为空；格式与 snapshot 换行问题在不改变 Git blob 的前提下修复。
- [x] 更新数据库追加0033、两套冷却与可信周期、重建调度投影和前端单/批请求所有权 specs；写 `research/verification.md`、`research/baseline.md` 与 `research/upgrade-notes.md`，对照验收条件复核。
- [x] 形成本地双亲 merge `0aee0a00`，main 快进并核验双方 ancestry、上游待合入0、33条链、旧32个迁移blob与95个原有文件；未 push 或部署。
- [x] 按 trellis-finish-work 归档至 `archive/2026-10/10-21-merge-upstream-october-21`，记录会话并交付结果、工作提交 `0aee0a00` 及验证限制；归档/日志与工作合并分开提交。

## 协作约束

每个子代理 dispatch 以 `Active task: .trellis/tasks/10-21-merge-upstream-october-21` 开头；按 native 注入或子侧加载 fallback 读取 artifacts 和 manifests。代理不得再 spawn implement/check，且不得操作 Git 索引、分支或提交。告知各代理不是单独工作，不得回滚他人改动。跨 owner 需要修改时报告主会话并协调。

## 高风险审查点

- 已发布迁移/清理列的注册和完整链断言；禁止按重叠数字全局替换。
- `SetQuotaCooldown` 与 fork reset 的原子性、恢复代次及 checkpoint；新增字段的 clone/preserve/projection 触点。
- 捐献后台恢复、新 CPA ModelCatalogUpdater 的构造与生命周期。
- mobile settings 的备注、名称单独保存的 note 缓存、batchCount 与可选 row 的单/批模式边界。
- 根与嵌入 CPA SDK 版本一致、开发版 update 守卫、GHCR fork 配置与发布脚本。

## 回退点

实现开始前记录固定输入和工作区；merge 提交前保持集成分支不提交，若需要回退只操作本轮合并且先核验无用户新改动。不得 reset/清理原有文件。任何生产升级/外部发布需要另外授权。
