# 执行计划

- [x] 获取双方 remote，锁定 fork 起点和上游目标；只读预合并定位 10 个冲突。
- [x] 阅读迁移冻结、连接测试诊断和相关前端规范及此前合并设计。
- [x] 编写 PRD、design、implement 和真实 spec/research 上下文。
- [x] 向用户展示最终方案并取得实施确认（用户回复“可以”），再 `task.py start`。
- [x] 主会话建立集成分支，执行固定目标 `--no-ff --no-commit` 合并，保留初始未跟踪目录。
- [x] storage 实现代理：追加 0034、解注册表/链式断言冲突、适配新迁移测试、扩展真实 fork 旧链升级验证。
- [x] control 兼容：实现代理连续限流后由主会话接手，兼容双方诊断字段和安全证据，合并探测测试与后端三语言文案，保留恢复/fallback/捐献边界；定向回归通过。
- [x] frontend 实现代理：解决 classic 对话框冲突并检查 classic/modern 自动合并的 DTO/projector/view/i18n；保留单项和批量诊断及竞态守卫，完整静态门禁通过。
- [x] 主会话与检查代理复核 497 个上游路径、40 个双方交叉路径、冻结迁移 blob、GHCR/Compose 目标和双方历史。
- [x] 完成定向 storage、control、gateway/state/health/requestlog 冷却与用量、donation 等必要回归；独立 CPA module tidy/vet/完整测试。
- [x] 执行 Linux `make check PNPM=pnpm`：精确依赖、gofmt、根 module tidy/vet、75 个 Go 测试包、前端 lint/styles/format/type-check、等待 Go 结束后 Vite build、最终 Go build 与 diff --check 全通过。
- [x] 修复网关准备失败顺序和诊断百分号编码大小写缺陷；Windows 原有失败同环境基线复现，并用 Linux 完整验收补齐证据。
- [x] 主会话更新必要 spec 和最终验证记录并审阅范围。
- [ ] 创建真实 merge commit，快进本地 main 并核验双方父提交和上游祖先关系。
- [ ] Trellis finish-work 归档任务、记录 journal，交付 commit 和验证结果。远端发布最后另行确认。

## Git 与文件所有权

- 主会话：所有 Git mutation、任务文档、spec/journal、全仓最终门禁和 main 快进。
- storage agent：`internal/storage/**`；禁止改已发布 0001–0033 生产迁移源码。
- control agent：`internal/control/credential_probe*`、`internal/platform/i18n/locales/{zh-CN,en-US,ja-JP}.go`；其他必要后端文件先向主会话报告。
- frontend agent：`web/**`，仅本次合并兼容与必要格式修复。
- 检查 agent：最终审查，可修复范围内机械性缺陷；不可自行扩大产品/权限/发布范围。

## 关键风险与回退点

注册表冲突不能用一侧覆盖；0034 测试按 ID 定位。任何已发布迁移 blob 变化立即排查。敏感诊断回归必须证明已知秘密不会进入 response/log。Vite build 不与 Go 编译并行。任何生产/远端发布操作不执行。

## 验证环境记录

- 原工作区遗留 ignored `tmp/baseline-59d75050/` 会被 `gofmt -l .` 扫描；该目录完全保留。Windows 检查在仓库外干净 worktree `C:/Users/lucky0401/AppData/Local/Temp/gpt-load-20261119-validation` 执行，最终完整门禁在 WSL 独立 clone `/home/lucky0401/.cache/gpt-load-20261119-resume/workspace` 执行。
- Windows checkout 的 CRLF 仅归一为 LF，不改全局 Git 配置；旧 33 个生产迁移已按 Git blob/源码字节核验未变。CPA 两个模型目录 JSON 归一后重新验证固定 SHA256，无数据或摘要常量变更。
- pnpm 11 lifecycle 的嵌套 `pnpm` 会误命中全局 10.32.1；使用 TEMP 的 `pnpm.cmd` shim 并仅为命令进程 prepend PATH，精确依赖及完整 lint/format/type-check 通过。
- 完整 Go 测试进程选 Git Bash，并清空真实 live/external 数据库 opt-in；不访问生产上游或生产数据库。CPA 独立 module tidy/vet/完整测试已通过。
- 最终 Linux 验收 tree 为 `525d32260a943ea8ca0585eb8e995307c5af4aa8`；完整 make check 通过，CPA tidy/vet/完整测试通过。便携 Compose 仅用于真实离线配置解析。提交前核对交付源码与该 tree 一致，后续只更新任务记录。
- 具体命令、修复与基线/外部数据库验证边界见 `validation.md`。
