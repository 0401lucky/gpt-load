# 执行计划

## 0. 规划与批准

- [x] 固定本地/上游 SHA，确认 33/44 提交差距、560 文件增量和 16 文本冲突。
- [x] 读取迁移冻结、运行时配置作用域、前端投影和质量规范。
- [x] 用户同意创建任务；完成 PRD、设计、执行计划与研究记录。
- [x] PRD 完成收敛重写并通读；实现与检查 JSONL 已填入实际规范/研究路径。
- [x] 主会话展示最终规划；用户在后续消息回复「开始吧」，批准该方案实施。
- [ ] `task.py start` 后按阶段指引派发 `trellis-implement`，原生注入优先，子代理按任务路径读取上下文兜底。

## 1. 实施前基线与集成准备

- [ ] 确认 HEAD、上游固定 SHA 与工作区仍匹配；记录原有两个未跟踪目录并保护。
- [ ] 在原始树或仓库外固定起点副本采集测试基线、工具版本和检出换行方式，保存具体测试结果到 `research/baseline.md`。避免在根仓库内放额外 Go module 副本污染扫描。
- [ ] 主会话建立集成分支，并执行 `git merge --no-ff --no-commit addd69870cb58108c584d69219c44e23d4bdf3db`。不使用 ours/theirs 全局冲突策略。

## 2. 分区解决兼容问题

子代理可独立编辑，各自只能修改分配范围；主会话统一管理 Git 暂存/合并提交。派发前说明共享工作区，禁止撤销其他代理或用户改动。

- [ ] 存储实现：负责 `internal/storage/**` 的迁移顺延、27 条注册表、链断言和 24→27 升级/幂等回归。
- [ ] 后端实现：负责 `internal/platform/config/**`、`internal/state/**`、`internal/control/**`、`internal/parameteroverride/**` 及相关网关/健康/用量适配，保留 fork 功能与上游新字段；调整范围前与主会话协调。
- [ ] 前端实现：负责 `web/**` 与三份 README，同步 classic/modern DTO、投影、设置保存、备注及上游字段。
- [ ] 主会话复核自动合并的配置、路由、容器装配、CI、依赖和独立 CPA module，协调跨分区契约。
- [ ] 全部实现结束后，检查索引冲突及标记，检查新旧迁移符号引用，确认旧 24 条注册项没有变化。

## 3. 定向验证与完整门禁

- [ ] 先运行 `go test -count=1 ./internal/storage/...`，证明空库 27、旧库 24→27、数据/身份/旧账本不变与重复迁移幂等。
- [ ] 运行相关 control/state/health/gateway/parameteroverride/requestlog 测试；针对真实发现添加最小回归，覆盖捐献、备注、冷却、用量和探测配置指纹。
- [ ] 独立检查子代理按 PRD AC1–AC7 复核差异和证据；修复后只重跑受影响检查。
- [ ] 执行 `make check`；若仍无 make，逐项执行下列等价命令并记录每项退出码：

```text
corepack pnpm --dir web install --frozen-lockfile
gofmt -l .
go mod tidy -diff
go vet ./...
corepack pnpm --dir web run lint
corepack pnpm --dir web run format
corepack pnpm --dir web run build
go build -o <本轮忽略目录或仓库外输出路径>/gpt-load.exe .
go test -count=1 . ./internal/...
git --no-pager diff --check
```

`gofmt -l` 要求零输出；前端 build 已含 type-check。不为执行门禁修改全局工具配置。

- [ ] 在 `third_party/cpaembedded` 执行 `go mod tidy -diff`、`go vet ./...`；如涉及需要定向验证的适配行为，再运行相关已有单元测试。
- [ ] 失败按用例与消息比对本轮基线；新增失败追因、修复和复验。不能只比较失败包名称。
- [ ] 证据写入 `research/verification.md`，清楚区分通过、基线环境失败和无法验证部分。

## 4. 本地合并交付

- [ ] 主会话更新确有变化的数据库/配置规范，记录迁移编号映射与简洁升级/回退说明。
- [ ] 审查暂存 diff，排除用户原有未跟踪文件和敏感数据，创建可追溯双亲 merge commit。
- [ ] 将本地 `main` 快进到已验证结果，检查 `git merge-base --is-ancestor` 对两个固定起点均成功，以及 `git rev-list --count HEAD..addd6987` 为 0。
- [ ] 保存提交 SHA、最终工作区状态、验证结果与未验证范围，按 Trellis 完成阶段归档记录。
- [ ] 向用户交付；不执行 push、镜像发布或生产操作。
