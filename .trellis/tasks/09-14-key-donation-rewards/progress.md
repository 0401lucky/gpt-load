# 本地实施进度

## 授权与现场（2026-09-14）

- 用户明确授权按最终规划完成本地实现、定向测试、必要修复与跨仓库联调；不重复创建任务或讨论已定产品规则。生产操作和外部发布不在本次范围。
- 已读取父 PRD、design、implement、integration-contract、codebase-findings、new-api-conventions，及 intake 的规划和 implement/check 上下文清单。
- gpt-load 基线 `57965b94`，开始前仅四个未跟踪的任务目录；new-api 基线 `bc223d567`，开始前仅六张 `clover-*.png`。未发现已有捐献代码，两端已有文件均保留。
- gpt-load 本地分支：`feat/key-donation-rewards`。任务目录仍统一位于 gpt-load；后续 new-api 的代码、Git 和测试在其目标仓库执行。
- 已运行 `task.py start .trellis/tasks/09-14-donation-intake`，状态由 planning 进入 in_progress。实现交由 Trellis implement 子代理，主会话负责协调、验证环境与进度。

## 执行状态

| 阶段 | 状态 | 证据 |
| --- | --- | --- |
| gpt-load 接收与校验 | 通过 | 独立复核、真实 MySQL/PG、定向包和 Linux LF worktree 完整 make check 均通过 |
| new-api 奖励后端 | 通过 | 全包检查、四版本真实数据库矩阵、独立源码复核通过 |
| new-api 页面 | 通过 | 24项交互、完整typecheck、变更范围lint/格式/版权和最终build:check均通过；独立复核通过 |
| 跨仓库最终验收 | 本地验收通过 | 真实双服务专项47.45秒、最终桌面/移动/管理员浏览器验收通过；真实数据库与秘密保护证据完整 |

## 验证环境

- Windows 已有 Go、Bun、pnpm、Docker CLI 和 Ubuntu WSL。当前 PATH 未找到 make；Docker Linux daemon 尚未运行。正在准备本地隔离验证环境，未把这些初始状态视为最终阻塞。
- gpt-load 要求 Go 1.27.0，原生 Go 正在自动获取该 toolchain。真实 SQLite/MySQL/PostgreSQL 验证的版本、命令与结果将在执行后记录。
- Go 1.27.0 已就绪。GNU Make 4.4.1 位于系统临时目录 `donation-verification-20260914/make/tools/install/bin/make.exe`，通过 Git Bash 执行仓库原始 Makefile，未修改系统 PATH 或项目依赖。
- Docker 初始出现 WSL 数据盘 E_ACCESSDENIED；用户随后修复并确认可用。Docker Client/Server 均为 29.6.1，未进行系统权限修改。
- 已创建仅绑定 127.0.0.1、带 `codex.task=key-donation-rewards` 标签和 `--rm` 的本次专用容器，数据位于临时内存盘；不使用既有业务容器/数据。MySQL 8.4.11（23306）、MySQL 5.7.44（23307）、PostgreSQL 17.10（25432）、PostgreSQL 9.6.24（25433）均已通过真实版本查询。各实例内 `donation_newapi` 与 `donation_gptload` 数据库分离；版本查询不代表功能测试通过。
- new-api 现有迁移基线 `go test ./model -run '^TestMigrationSchemaStability$' -count=1 -v` 已分别在 SQLite + MySQL 8.4.11 + PG 17.10，以及 SQLite + MySQL 5.7.44 + PG 9.6.24 通过。使用现有 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN` 指向上述专用 new-api 库。这证明验证环境与现有迁移基线可用，尚不代表新增捐献模型/事务通过。
- 已读取 ASVS 5.0.0 和认证、会话、CSRF、REST 官方指南，控制映射写入 [security-controls.md](research/security-controls.md) 并加入 new-api 两个子任务上下文清单。
- new-api/web 环境缺少项目指定 Node 24.10.0 与依赖导致首次类型检查无法启动；已用 fnm 安装该版本并运行 `bun install --frozen-lockfile`，随后 `bun run typecheck` 基线通过。new-api Git 状态仍只有原有六张截图，锁文件及源代码未变化。

## intake 实施与复核结果

- 0015 迁移、独立集成鉴权/五个接口、加密暂存、单 key 真实 probe、批次/动作幂等、控制恢复屏障及长期资源/来源账本已实现；三份 README 和 .env.example 已同步。
- 已修复真实 MySQL 保留字查询、clientFoundRows 下误判新插入、重试 GORM 查询污染、控制操作未恢复时的提交/发布顺序，以及相关测试期望遗漏。
- 最终 Windows 定向 `control/config/httproute/storage/...` 全包、对应 go vet、改动 Go 文件 gofmt 和 diff 检查通过；生产 Bifrost + 本地 Gemini HTTP 测试断言实际提交 key，无库存回退。
- 最终 MySQL 8.4.11 READ COMMITTED 新路径全套、REPEATABLE READ 幂等专项，以及 PostgreSQL 17.10 全套通过；隔离数据库/schema 已清理。详细命令与边界见 [database-verification.md](../09-14-donation-intake/research/database-verification.md)。
- 原 Windows 工作区 `make check` 的 gofmt 因 927 份既有 CRLF Go 文件失败；未批量改用户文件或全局 Git 配置。建立包含当前改动的 LF 临时 worktree，并保存内容哈希以便最终核对。
- LF worktree 的格式、依赖、Go vet、前端 lint/format/类型与构建、Go build 已通过。递归 pnpm 误用旧版本已通过临时 PATH shim 修复。全量 Windows Go 测试遇到分页文件资源不足，以及既有 catalog Windows 路径断言与 python3 别名环境问题；已降低进程内 Go 并发，另准备 Linux 环境运行原始全量门禁。尚不声称完整 make check 已通过。
- 后续已完成 Linux 原始 `make check`，**退出码0、全流程通过**，含根包和 internal 全部测试及 diff 检查。使用 Go1.27.0、Node24.11.0、pnpm11.17.0、jq1.8.1、Docker CLI29.6.1/Compose5.3.0；工具置于本次用户缓存目录，未改系统全局配置。完整输出在 intake research/intake-make-check-linux.log。初次 Linux 运行仅缺 Docker/jq 导致脚本契约失败，补齐本地工具后复验通过；Windows 基线限制仍如上记录。

## rewards-backend 验收结果

- 连接/活动配置、独立秘密材料、稳定批次/明细/来源、全局去重、原请求恢复、永久奖励事务、账号暂停、查询与独立权限已实现；后端生产源码冻结。
- `go test ./model ./controller ./middleware ./service/... ./router . -count=1`、同范围 vet、改动 Go 格式及 diff 检查通过。实际 SQLite3.50.4、MySQL8.4.11/5.7.44、PostgreSQL17.10/9.6.24 覆盖新建/升级/重复迁移/并发/回滚/余额边界，并覆盖 MySQL 两种 clientFoundRows 语义。
- 独立完整源码复核通过，未发现剩余确认问题。旧 fixed-price fixture 缺少其实际查询的 Checkin 表已独立复现，修复仅补夹具；未变更既有计费生产逻辑。详细见 [check-handoff.md](../09-14-donation-rewards-backend/research/check-handoff.md)。
- 实际两个服务进程、真实登录/管理/捐献接口与 worker、临时 SQLite、本地假 Gemini HTTP 的全套最终通过47.45秒，包含并发、丢回执/两端重启、冻结奖励/禁用暂停、删除后历史、零 Checkin 写入与无 key/token 日志。二进制哈希和命令见 [cross-service-validation.md](research/cross-service-validation.md)。

## new-api 后续验证基线

- 开始后端前 Git 仍仅六张原有 clover 图片；已建立 new-api 本地 `feat/key-donation-rewards` 分支，启动已有 rewards-backend 子任务。
- 页面代码尚未修改前，`bun run format:check` 已有 98 个文件失败，`bun run copyright:check` 已有 22 个文件缺少头部；完整清单保存在 research/new-api-format-baseline.log / new-api-copyright-baseline.log。后续应保证本次修改文件通过，不能将既有问题误归于新增功能或静默批量重写无关文件。

## 前端收尾与现场保护

- 已实现捐献、管理配置、管理记录三个页面与顶部入口，七语言翻译完成；初版交互 4 个文件/19 项通过。独立复核正在收尾迟到 POST 响应的状态单调性，以及中文活动说明的 UTF-8 字节边界，并补回归测试。
- 真实浏览器 preview 已完成登录返回、五行混合提交、输入确认后清空和原批次重试：首次 1 个接收/$1，解除429后 2 个接收/$2，无重复奖励。最终构建的移动、管理员与账号切换验收仍待完成，未用 preview 代替最终验收。
- 已再次核对六张原有 `clover-*.png` 的 SHA-256，全部与开工前一致；未纳入暂存。完整 make check 使用的快照与当前 gpt-load 39 份代码/配置/README 的归一化哈希一致。
- 旧浏览器临时目录清理被自动审批检查以 `blocked by policy` 拒绝，未绕过；其两个服务进程已核对并停止，详细记录在 [browser-validation.md](research/browser-validation.md)。
- 已关闭四个本次专用数据库容器（核对任务标签和 autoRemove 后执行），保留全部真实数据库日志。未停止 Docker Desktop 或其他容器。
- 父任务补充了真实 SQLite 完整备份恢复后去重/奖励保留，以及过期签到桶查询与清理后永久额度保留的两项回归；两项、model 全包和 vet 均通过。生产源码未改，详见 [retention-verification.md](../09-14-donation-rewards-backend/research/retention-verification.md)。

## 最终本地交付

- 独立复核发现的迟到POST、新草稿被旧GET清空、UTF-8说明长度三类边界已修复，各有先失败后通过的回归。最终24用例、完整类型检查、38个TS/TSX范围lint、45个手工文件格式/版权检查和build:check均通过；独立检查再次通过。
- 全仓遗留239条lint error/65条warning、92个格式项、22个版权项均位于未修改文件，与本次变更交集为空；没有扩大范围重写旧代码。完整定位见 web 的 [check-handoff.md](../09-14-donation-web/research/check-handoff.md)。
- 已使用task.py start切回已有父任务，父任务为in_progress。最新UI嵌入Go二进制后，最终真实浏览器完成公共/登录/移动入口、中文R18、混合捐献与重试、新草稿保持、跨账号隔离、普通用户403、连接保存、空组活动、改名保持目标、记录组合筛选与详情。
- 浏览器普通账号和管理员持久存储扫描均无合成key/token/密码，相关API响应同样无秘密。最终夹具PASS 2305.78秒，包含真实进程日志检查；停止文件已使runner和两个服务退出，本次夹具目录已删除。详细证据见 [browser-validation.md](research/browser-validation.md)。
- 父PRD AC1–AC20的本地验收均完成，见 [final-acceptance.md](research/final-acceptance.md)。原有六张clover图片最终哈希再次一致；两个仓库git diff --check通过。
- 用户已回复“可以提交归档了”。按Trellis Phase3.4执行已确认分组，工作提交之后进行归档与日志提交；未推送。四个任务的实现和验证均已完成。

## 已完成的代码提交

| 仓库 | 提交 | 内容 |
| --- | --- | --- |
| gpt-load | `59a2d958` | 受限接收、指定key验证、长期来源和契约规范 |
| new-api | `7b6a3c7b` | 奖励后端、数据库/HTTP/保留性验证 |
| new-api | `68079045` | 捐献与管理页面、导航、七语和交互回归 |

文档工作提交、归档与会话日志按顺序继续；最终哈希记录在会话日志中。
