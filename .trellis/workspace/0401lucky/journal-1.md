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
