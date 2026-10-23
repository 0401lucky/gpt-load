# Journal - 0401lucky (Part 1)

> AI development session journal
> Started: 2026-09-14

---



## Session 1: 密钥捐献与社区永久额度奖励：实现、验收与归档
<!-- trellis-session: v=2 fp=67e52bfc69807014 -->

**Date**: 2026-09-15
**Task**: 密钥捐献与社区永久额度奖励：实现、验收与归档
**Branch**: `feat/key-donation-rewards`

### Summary

完成两个仓库的密钥捐献、永久奖励和管理页面，经真实数据库与浏览器验收后，按用户确认提交并归档四个任务。

### Main Changes

- 实现gpt-load受限接收、指定key校验、幂等回执及长期来源。
- 实现new-api全局防重、永久奖励事务、批次恢复及七语捐献/管理页面。
- 完成四组工作提交和完整任务树归档，保留45份原始验证日志及有效上下文。

### Git Commits

| Hash | Message |
|------|---------|
| `59a2d958506f86e3d8c80fa4512c705dd8d050d1` | feat(donation): 实现受限密钥接收与长期来源记录 |
| `7b6a3c7b434b72a777db5b5b8855cf777ef53dec` | feat(donation): 实现永久奖励账本与批次恢复 |
| `68079045ee2dd424884498d6225e982eae5325e4` | feat(web): 增加捐献页面与活动管理 |
| `a9cd302775552f2a6abd7b115f3f7a1206f3ac9e` | docs(donation): 记录规划与跨仓库验收 |

### Testing

- [OK] gpt-load完整make check在Linux LF验证副本通过；源码哈希无漂移。
- [OK] 真实SQLite、MySQL5.7/8.4、PostgreSQL9.6/17矩阵及相关后端测试/vet通过。
- [OK] 前端24用例、typecheck、变更范围lint/格式/版权、build:check及最终桌面/移动真实联调通过。

### Status

[OK] **Completed**

### Next Steps

- 本地范围完成；生产配置、真实上游凭据及外部发布另行授权。全仓历史检查问题与旧预览目录清理拒绝已记录。


## Session 2: gpt-load 主分支镜像自动构建与发布
<!-- trellis-session: v=2 fp=6afb9d535efd6d39 -->

**Date**: 2026-09-15
**Task**: gpt-load 主分支镜像自动构建与发布
**Branch**: `main`

### Summary

完成本fork main到GHCR的自动构建、双架构原生运行验证、digest推广及Compose更新说明，首次真实发布和匿名读取均通过。

### Main Changes

- 新增ghcr-main workflow和固定digest推广脚本，保留上游Release边界与最小GITHUB_TOKEN权限。
- Compose默认采用ghcr.io/0401lucky/gpt-load:latest，可用GPT_LOAD_IMAGE固定镜像；三语文档、测试和长期规范同步。
- 保留远端main新增上游修复，工作提交已推送main；任务已归档到tasks/archive/2026-09/09-15-ghcr-main-images。

### Git Commits

| Hash | Message |
|------|---------|
| `7a522cb7ffaf22a7bc53e166de0ef1ffefa75e13` | ci(images): 自动发布并验证主分支 GHCR 镜像 |

### Testing

- [OK] 最新85811f21基线的完整make check退出0；actionlint、Compose、12类推广故障分支及独立复核通过。
- [OK] GitHub运行34913061296的build、amd64/arm64原生verify、promote全部success。
- [OK] 三个标签同指向sha256:0c7153b38654bd7d42dbb6789a01cd5544b772005df9555a3311de69fb512b10；匿名manifest/config与18个layer请求全部200。

### Status

[OK] **Completed**

### Next Steps

- 服务器可在原Compose项目中pull并up；保留数据库与加密材料。本轮未操作服务器，main流水线未执行漏洞扫描。


## Session 3: 合并上游 main 并重编号 donation 迁移
<!-- trellis-session: v=2 fp=bfdce0b8945d4262 -->

**Date**: 2026-09-20
**Task**: 合并上游 main 并重编号 donation 迁移
**Branch**: `main`

### Summary

合并 upstream/main 28 个提交。上游新增 0015-0017 与 fork 的 donation 迁移（原 0015/0016）编号撞车，donation 顺延为 0018/0019。因 schema_migrations 按数组下标严格比对 ID，升级路径定为「删账本两条旧记录 → 新版本重放 0015-0019」；幂等性经 PostgreSQL/MySQL/SQLite 三库演练及生产同构复刻验证，含 439 条资源与持久身份的生产库升级后数据零丢失、identity 不变。修复 operation_index_migration_test 对 len(migrations)-1 的隐式依赖（原会让两个子测试静默改测其他迁移却仍通过）。镜像已由 CI build/verify/promote 产出。生产 compose 已锁定到 locked-20260920，待按 runbook 执行升级。

### Git Commits

| Hash | Message |
|------|---------|
| `b2aba7d6` | merge: 合并上游 main 至 b211e7c8，donation 迁移顺延为 0018/0019 |
| `364988a4` | chore(spec): 同步迁移重编号并归档合并任务产物 |

### Status

[OK] **Completed**


## Session 4: 凭据备注字段：门禁验证、端到端实测、spec 同步与提交
<!-- trellis-session: v=2 fp=ac153a091dc59976 -->

**Date**: 2026-09-20
**Task**: 凭据备注字段：门禁验证、端到端实测、spec 同步与提交
**Branch**: `feat/credential-note`

### Summary

对 09-20-credential-note 的实现做独立验证并提交。三处高危点均正确落地：CredentialItemResponse.Note 标 json:"-"、normalizeCredentialUpdate 的「至少一字段」判定已含 Note、updates map 仅在 Note.Set 时写。门禁全绿（go build/vet、storage+control 测试、前端 type-check/lint/build）。上一会话的基线文件随临时目录丢失，改用 git archive HEAD 在仓库外取纯净副本重新取基线，失败包集合与改动后完全一致（catalog/container/gateway/platform-authkey/webui，均为既有 Windows 环境失败），无新增回归。端到端实测（真实服务 + 真实浏览器）覆盖 AC1–AC5：真实 SQLite 账本 20 条且末条为 0020、列为 VARCHAR(2048) NOT NULL DEFAULT ''；只传 note 可写入、不传不改动、空串与 null 均清空、2048/2049 rune 边界正确且被拒请求不部分改写；modern 面板保存后整页刷新仍保持；classic 页面渲染出两条凭据且控制台 0 错误。spec 更新三份：迁移导出符号按建表型/加列型分类（0005/0014/0020 均不导出 SchemaModels/TableNames/ValidateCurrent）、新增 classic/modern 共用 DTO 的 json:"-" 契约、修正 data-layer 对 assertNoSecretLikeFields 的表述。trellis-check 子代理独立核查未发现需修复缺陷。两处值得留存的发现：(1) projector.ts:141-142 中 secretLikeField.test 之后紧跟的 invalidResponse() 是无条件执行的，正则不构成放行条件 —— 白名单外任何字段都会抛错，故 json:"-" 是承重设计；(2) 本机 Git Bash 下 grep -c $'\r' 判定行尾会假阳性（返回行数），须用字节计数，经 git cat-file 确认暂存 blob 均为纯 LF。

### Git Commits

| Hash | Message |
|------|---------|
| `4d721020` | feat(credentials): 为上游凭据增加可编辑备注字段 |

### Status

[OK] **Completed**

### Next Steps

- 推分支并观察 CI（本机无 make，gofmt/prettier 因 CRLF 检出假阳性；MySQL/PostgreSQL 路径本地无 DSN 只能由 database-contract 矩阵覆盖）。上线走常规 upgrade.sh：0020 追加在链尾，无需修账本、无需停机。


## Session 5: 合并上游 main 33 提交：迁移编号 B 方案（上游顺延为链尾）
<!-- trellis-session: v=2 fp=2f7f3d32551184b4 -->

**Date**: 2026-09-23
**Task**: 合并上游 main 33 提交：迁移编号 B 方案（上游顺延为链尾）
**Branch**: `main`

### Summary

合并 upstream/main 33 个提交（342 文件）进 fork main。两侧迁移编号再次撞车，改选 B 方案：fork 冻结 0018_donation_intake/0019_donation_manual_review/0020_credential_note，上游新增 4 条顺延为 0021_auto_model/0022_auto_decision_attribution/0023_client_model_overrides/0024_request_audit，使生产账本恢复纯追加语义，升级退化为 pull + up -d。冲突 5 个文件全在 internal/storage；另发现上游 3 个新测试硬编码注册表下标（顺延后会失败或静默测错迁移），按 fork 既有范式改为按 ID 定位 helper。用生产库快照（312MB、账本 20 条）在服务器 /tmp 一次性容器中演练：20→24 且前 20 行逐行未变、486 资源与身份 UUID 未变、二次启动幂等、空库 24 条。spec 已把编号策略改为「fork 冻结 + 上游顺延到链尾」。本地未 push、生产未升级。

### Main Changes

- git merge --no-ff upstream/main 至 0af4e399：保留 fork 三条迁移编号，上游 4 条迁移文件与导出符号顺延为 0021-0024，DDL 级内部名（auto_decision_usage_stats_0019、chk_auto_decision_usage_0019_* 等）保持上游原样。
- 解决 5 个 internal/storage 冲突：注册表 24 条、migration_test/db_test/database_integration_test 链式断言同步、operation_index_migration_test 保留按 ID 定位实现。
- 上游 3 个新测试的固定下标改为 autoDecisionAttributionMigrationIndex/clientModelOverrideMigrationIndex/requestAuditMigrationIndex，避免静默测错迁移。
- spec 更新：database-guidelines.md 编号约定改为 fork 冻结、上游顺延到链尾，并新增 DDL 内部名保留与绝对下标两条规则；归档 PRD/设计/执行计划/演练证据/生产升级说明。

### Git Commits

| Hash | Message |
|------|---------|
| `0af4e399` | merge: 合并上游 main 至 f2840466（上游新增迁移顺延为 0021-0024） |
| `9f4e7a57` | chore(spec): 冻结 fork 迁移编号并记录上游顺延约定 |

### Testing

- [OK] [OK] go build ./... 与 go vet ./... 退出 0；go test ./internal/storage/... 全通过。
- [OK] [OK] 全量 go test . ./internal/... 与 59d75050 纯净副本基线逐包对比：失败包集合一致（catalog/container/gateway/platform-authkey/webui）；唯一新增 TestCodexModelCatalogSnapshotDigest 已定位为 Windows core.autocrlf 检出 CRLF 的伪失败（blob 级 sha256 = 期望值，LF 内容下测试通过）。
- [OK] [OK] 前端 type-check / lint / build 全部退出 0（固定 pnpm 11.17.0）。
- [OK] [OK] 生产库快照演练：账本 20→24 前 20 行未变、业务计数与 instance_id/source_id 未变、三张新表与新列落库、二次启动幂等、空库全新安装 24 条。

### Status

[OK] **Completed**

### Next Steps

- push main 触发出 ghcr-main 镜像流水线（外部发布需用户确认）。
- 生产升级按 research/production-upgrade-note.md 走常规 pull + up -d：无需改账本、无需停机；回滚仍需卷快照与旧镜像成对。
- trellis-check 子代理本轮因 spawn_agent 持续不可用未能派发，交叉核查（blob 级比对、注册表、索引 helper 等价性）由主会话完成，证据在 rehearsal-evidence 与 baseline-tests。


## Session 6: 修复 Cline 模型用量与测试诊断并上线
<!-- trellis-session: v=2 fp=d84f80b4ef798465 -->

**Date**: 2026-09-28
**Task**: 修复 Cline 模型用量与测试诊断并上线
**Branch**: `main`

### Summary

补齐分组模型用量窗口边界，连接测试显示脱敏上游详情；推送 main 并部署 yunyou-99 gpt-load-2 dev.12，线上样本与日志一致。

### Main Changes

- 批量精确边界统计、data_complete、两套测试弹窗诊断和三语同步

### Git Commits

| Hash | Message |
|------|---------|
| `de234646` | fix: 修复模型用量窗口漏算并补全连接测试诊断 |

### Testing

- [OK] control/requestlog 完整测试、前端 lint/type/build、Go vet/build 通过；全量门禁既有 Windows 环境失败详见任务 verification.md
- [OK] Main images #12 全绿；公网200、容器healthy、cac8为15,662,497 tokens、真实测试返回429原文且不改冷却

### Status

[OK] **Completed**

### Next Steps

- 部署完成；遇新上游额度问题可根据测试错误详情继续判断


## Session 7: 兼容合并上游 44 个提交并验证迁移追加升级
<!-- trellis-session: v=2 fp=c01298d5e2a8df0f -->

**Date**: 2026-10-02
**Task**: 兼容合并上游 44 个提交并验证迁移追加升级
**Branch**: `main`

### Summary

将上游 addd6987 的 44 个提交兼容合并进本地 main，保留捐献、凭据备注、分组模型冷却、用量矩阵和连接诊断。16 个文本冲突已解决，上游三条迁移顺延为 0025-0027，旧 24 条冻结。本地 main 已更新，上游落后数为 0；未 push 或部署。

### Main Changes

- 独立实现及检查代理复核前后端契约与隐藏迁移注册表冲突，补充新旧设置和备注/RPM 共存回归。
- 数据库 spec 同步 27 条迁移链及 RPM 留存规则；完整任务与验证记录已归档。

### Git Commits

| Hash | Message |
|------|---------|
| `37ce4c93b2ac1802169313ef07cc86ebe28b97ba` | merge: 兼容合并上游 main 至 addd6987（迁移顺延为 0025-0027） |

### Testing

- [OK] 前端 install/lint/format/type-check/build、Go build/vet/tidy、CPA module tidy/vet、1257 个 Go 源码格式检查通过。
- [OK] SQLite 新库及 24→27 升级、11 张表数据和旧账本不变、重复迁移幂等测试通过；相关后端回归通过。
- [OK] 整仓 Go 测试执行完毕；5 个 Windows 失败包同条件 LF 基线复测为完全相同的 23 个顶层失败测试，无新增失败；真实 MySQL/PostgreSQL 未实测。

### Status

[OK] **Completed**


## Session 8: 模型用量周期修复与缺失提示
<!-- trellis-session: v=2 fp=c3e16c6c523e9bea -->

**Date**: 2026-10-03
**Task**: 模型用量周期修复与缺失提示
**Branch**: `main`

### Summary

分离默认冷却与可信额度重置，旧检查点回退24h并保留健康状态，两套UI暴露缺失计数。相关回归与构建通过；Windows完整门禁限制已记录，TCP失败在原版复现。用户已授权推送main并部署gpt-load-2，部署待镜像构建完成。

### Git Commits

| Hash | Message |
|------|---------|
| `42adbdaf` | fix(usage): 隔离模型冷却与额度周期并提示用量缺失 |

### Status

[OK] **Completed**


## Session 9: 兼容合并上游 main 至 a5c691bb
<!-- trellis-session: v=2 fp=604e55d148155cba -->

**Date**: 2026-10-08
**Task**: 兼容合并上游 main 至 a5c691bb
**Branch**: `main`

### Summary

合入17个上游提交，冻结旧27条迁移并追加0028-0032；保留备注、捐献、模型用量与可信周期，修复捐献代理引用及别名保存备注缓存。构建、静态检查、storage/control与CPA验证通过；完整Go测试64包通过，23个Windows基线失败已同条件复现，无新增失败。已合入本地main，未push或部署。

### Git Commits

| Hash | Message |
|------|---------|
| `b2cbb156` | merge: 兼容合并上游 main 至 a5c691bb（迁移顺延为 0028-0032） |

### Status

[OK] **Completed**


## Session 10: 兼容合并上游 main 至 41c82ac0
<!-- trellis-session: v=2 fp=37ed7c53b894a8e8 -->

**Date**: 2026-10-21
**Task**: 兼容合并上游 main 至 41c82ac0
**Branch**: `main`

### Summary

合入41个上游提交并解决17处冲突，冻结旧32条迁移且追加0033；保留捐献、备注/别名、模型用量、可信周期、诊断及fork GHCR。前端/构建/SQLite/CPA门禁通过，23个Windows顶层基线失败与旧版相同，无新增失败。已合入本地main，未push或部署。

### Main Changes

兼容合并固定上游41c82ac0的41个提交，解决17处冲突并形成保留双方历史的本地merge。保留捐献独立鉴权与恢复、备注/别名、模型用量、可信额度周期、连接诊断和fork GHCR配置。冻结旧32条迁移、追加0033，验证真实SQLite 24/27/32→33和重启幂等。

前端冻结安装/lint/styles/format/build、根tidy/vet/build、CPA独立tidy/vet/full tests、核心交叉回归通过。根完整测试65pass/8skip/5fail，23顶层/82含子测试失败与本轮固定216024cc同条件Windows基线完全一致，无新增失败。没有push或部署；真实MySQL/PostgreSQL与浏览器E2E未实测。

实现中修复ReplaceCredentials使用原始entries同步调度的遗漏；spec记录恢复代次和可信周期、0033请求体合同以及单/批前端所有权。用户原有95个文件与32个旧迁移blob、fork发布配置核对不变。详细证据在任务archive的research目录。


### Git Commits

| Hash | Message |
|------|---------|
| `0aee0a0037baaf72ee6cfe0311e33e65d15f3d81` | merge: 兼容合并上游 main 至 41c82ac0（请求体迁移顺延为 0033） |

### Status

[OK] **Completed**


## Session 11: 合并上游余额显示修复并诊断 Gemini 503
<!-- trellis-session: v=2 fp=da707f288175fe6a -->

**Date**: 2026-10-23
**Task**: 合并上游余额显示修复并诊断 Gemini 503
**Branch**: `merge/upstream-20261023`

### Summary

真实合并 dd33ae3，完成前端与编译验证；线上直连复现 Gemini 503，记录调度策略及改善草案，未修改生产配置。

### Main Changes

- 上游两个订阅凭据卡片统一将正余额向上取整；保留 fork 提交及原有未跟踪目录。
- Gemini 首次采样前24小时39次请求：11成功、17次503、8超时、3请求格式错误。直连Google两枚Key复现503，相同Key其他模型及稍后3.8 Flash可成功；默认skip_group只作用当前请求。
- 诊断证据：tmp/diagnostics/gemini-503-20261023.md；针对high demand的分组规则草案未应用，API Key未写入本机或报告。

### Git Commits

| Hash | Message |
|------|---------|
| `a3c71d5c` | merge: 兼容合并上游 main 至 dd33ae3（余额显示向上取整） |

### Testing

- [OK] ESLint、现代样式、Vue/TS类型检查、Vite、Go vet、Go build及diff检查通过。
- [OK] Windows完整Go测试5包57个顶层测试未通过；未做baseline复测，不声称make check全绿。报告：tmp/upstream-merge-quality-20261023.md。

### Status

[OK] **Completed**
