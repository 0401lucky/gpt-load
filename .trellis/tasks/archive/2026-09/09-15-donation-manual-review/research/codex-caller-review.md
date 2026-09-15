# Codex 复核：new-api 后端

- 日期：2026-09-15。
- 范围：`../new-api` 的 donation model/service/controller/router 及现有测试；未修改前端、gpt-load 产品代码或两服务集成测试。
- 基线：new-api `68079045e`。Go 实际版本 `go1.25.5 windows/amd64`，模块声明 Go 1.25.4。
- 结论：本范围发现的确定缺陷已修复，定向静态检查、后端测试、真实三库及 MySQL affected-rows 变体验证通过。两服务真实进程/浏览器验收由主会话持有，本报告不代替那些验收。
- 所有测试只用本地隔离数据库、合成输入与 loopback HTTP。保留任务 `in_progress`，未提交、推送、部署或归档。

## 1. 已修复的问题

### PostgreSQL 失败的实际根因

`newDonationFixture` 的清理列表遗漏新增 `DonationReviewAction` / `DonationTestAttempt`。旧测试留下 `dv_69d03160_donation_review_actions` 和 `dv_69d03160_donation_test_attempts`，以及 PostgreSQL schema 全局的两个显式索引名。后续随机表前缀的 `CREATE UNIQUE INDEX IF NOT EXISTS` 因同名索引而不创建索引。

复核不是只检查 DDL 文本：查询 `pg_indexes` 确认后续 `dv_4d82dfc7_*` 表只有主键/普通索引，没有两个复合唯一索引；补充真实重复 INSERT 后，两个唯一性断言也失败。因此不能把原症状描述成仅“重复无效 DDL、唯一性无影响”。

修复限定在 fixture：完整清理新增表；PostgreSQL 每个 fixture 使用独立生成的 schema，并通过 DSN `search_path` 使两个连接均固定在该 schema 内。未删除此前留下的测试表，未通过重命名/删除生产索引规避错误。

### 旧捐献历史升级验证原来缺失

原 `upgrade=true` 只创建 User/TopUp/Checkin，不能证明本次捐献表升级。现在冻结任务前 Campaign/Batch/Item 字段与索引，创建真实旧捐献表并保存秘密、连接、活动快照、批次摘要、已奖励项、资源归属、Reward、Retry 和事件，再运行新迁移与重复迁移。

断言包括旧数据/JSON 原样保留、auto 回填、旧请求重放、旧奖励不能再次到账、原两条奖励唯一约束仍拒绝重复 INSERT。SQLite 备份测试改为对比备份前后完整奖励数量，覆盖新旧奖励一起保留。

### 动作身份、互斥与目标绑定

- 原本只有 actor/action 的复合唯一性，但内部写入按 action_id 单独找行；另一管理员复用 UUID 可能取错记录。主会话确认采用最小兼容方案：action_id / test_id 在本地账本全局唯一，跨 actor 复用返回冲突，不新增另一套前端请求 ID。数据库索引裁决并发，不依赖先查再插。
- 动作执行结果校验原 batch/item/kind/expected revision/target、受控 outcome/reason、effect revision 和源时间；内部更新还绑定具体行 ID、actor 和原意图。
- pending 意图未确定前，不能另开相反决定或测试。MySQL 上在 item 行锁后用 current read 查询已有意图/测试，避免早先 REPEATABLE READ 快照漏掉刚提交的赢家。
- `enter_review` 与 `approve` 均携带管理员所见的人工目标修订；转审结果不再修改原意图的 comparator。动作先查幂等，重放不会因为自己的成功改变 revision 而失败。
- 审核和测试均向接收端上送由服务端认证取得的 `actor=user:<id>`。

### 完整回执与独立发奖防线

- 增加 batch.validation_mode 和 item.entry_action_id，严格区分冻结提交模式与后续人工转审。
- review-context 的 effective_mode 恢复条目真实模式；增加 review_action / can_reject，识别合法 auto 转审，不伪装 manual。
- 完整回执是 item 状态与 revision 的唯一推进来源；部分 context/action 响应不再单独抬高 revision。正版本拒绝同版本矛盾、忽略旧版本；旧 auto 的持续 0 版本推进保留。
- 已进入人工的项不能被高版本 auto 回执降级。发奖还查询独立已应用动作事实，避免只靠一个可损坏的模式字段。
- 人工 committing/accepted/rejected 必须匹配本地原决定、目标、源 reviewed_at_ms、动作 ID 和有效 revision。accepted 先于动作查询时，可在完整绑定下确认原 pending approve；不能根据裸 accepted/测试成功/HTTP 200 发奖。
- `CreditDonationReward` 再次验证同一批准与接收事实，保留原永久唯一奖励、账号暂停、事务回滚与缓存规则。

### 冷对账与归属释放

- 原冷对账以 `Summary.Processing == 0` 返回，导致只有 pending_review 的批次根本没有 GET。现将 PendingReview 纳入读取条件。
- 删除调用方仅凭本地 TTL 就改终态并释放 owner 的路径；404、超时或本地截止不能证明接收端没有先批准/接收。只有可信远端终态才能释放未取得归属。
- 修复 rejected 回执释放资源后又被旧 resource 副本写回的风险，改为在同一已锁定资源副本中更新。
- 热/冷队列均在实际开工前逐批取租约；领取重查队列条件，新动作唤醒不被旧租约结果覆盖。

### 真实调用与流式结果

- `BeginTest` 返回 Created 标志；同 ID 的 running 重放也只返回元数据，不再次发起调用。
- 测试摘要使用持久私有 HMAC 材料，结构化绑定 prompt/system/output budget，避免固定公开 HMAC key 和未绑定输出预算。
- 非流式与流式均校验 test/batch/item/model/start revision/target、状态、时间、输出大小与安全原因；SSE 必须先 meta，之后 delta，最后 done。
- 原实现会在本地结果持久化之前写 succeeded done；现仅在完整协议、有效输出及元数据成功写入之后发送成功终态。
- 长连接独立执行首包/idle 限额，取消传播到接收端；请求总时限及下游写截止有界。测试 409 或断流不自动重发模型调用。
- 接收端对流式旧 ID 返回 JSON 元数据时，调用方保留元数据语义，不伪造新正文。
- 修复固定 holdback 截断恰好跨越秘密的情况：先在完整缓冲中确定匹配边界，再输出；加上已知集成 token，保持 UTF-8 边界。当前捐献 key 的精确脱敏仍由持有暂存 key 的接收端负责。

### 权限与记录投影

- POST 审核/测试同时要求 records.read 和各自独立写权限；actor 只从认证上下文取得。
- 有 records.read 的管理员可按 item 查询其他管理员的既有动作/测试元数据；不能冒用其 actor 提交新动作。
- admin record 提供 pending_review_action、latest_test，以及最近 20 条 recent_review_actions / recent_tests，通过现有安全 View 转换；不输出 prompt、digest、完整 key 或模型正文。
- 本人 batch 仍只投影最终已应用 reject 动作的备注，并按确切 review_action_id 关联；不会带出批准备注/管理历史。

## 2. 实际验证

| 命令/范围 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./model ./service ./controller ./router ./service/authz` | 通过 |
| 受影响 Go 文件 `gofmt -l`、`git diff --check` | 无输出/通过 |
| `go test -count=1 ./model ./service -run '^TestDonation' -timeout 10m` | model 2.458s / service 8.052s，通过 |
| `go test -count=1 ./service/authz` | 0.324s，通过 |
| `go test -count=1 ./model -run '^TestDonationDatabase$' -v -timeout 10m`，配置两个真实 DSN | 18.435s，通过；各引擎均执行 fresh / old donation schema upgrade / repeated migration |
| `TEST_MYSQL_DSN` 增加 `clientFoundRows=true`，`go test -count=1 ./model -run '^TestDonationDatabase$/mysql' -v -timeout 10m` | 8.974s，通过 |
| `go test -count=1 ./service -run '^TestDonationReviewReconcilesLostResponse$' -v -timeout 5m` | 0.973s，通过；包含 before_apply 和 after_apply，后者只有一次 POST |

真实版本：SQLite **3.50.4**、MySQL **8.4.11**、PostgreSQL **15.19**（Debian 15.19-1.pgdg13+2）。MySQL DSN 基础版未启用 clientFoundRows，另外显式启用 true 验证。没有声称实测 MySQL 5.7 / PostgreSQL 9.6。

矩阵证据：

- [codex-caller-database.log](codex-caller-database.log)
- [codex-caller-mysql-found-rows.log](codex-caller-mysql-found-rows.log)

新增真实行为回归：source-wide UUID 冲突、同 item 相反 pending 决定、双连接测试互斥、错误目标回执、本地 TTL/404 不释放 owner、合法 auto context/转审重放、模式降级拒绝、running test replay、输出预算幂等、跨 holdback 脱敏、缺 meta 拒绝、首包/idle 超时取消、非流式错模型拒绝、历史 20 条边界及秘密隔离、已应用动作丢响应后的唯一奖励恢复。

测试开发中修正过一个新 HTTP fixture：服务端须读完请求体后才能观测 HTTP 客户端关闭；现有测试消费请求体并有释放兜底，相关首包/idle 用例通过。没有把中途停止的那次运行记为通过。

## 3. OWASP 核对

已读取 OWASP ASVS **v5.0.0** 的 [V8 Authorization](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/en/0x17-V8-Authorization.md)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 与 [Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)。

本范围对应 v5.0.0-8.1.1/8.1.2、8.2.1/8.2.2/8.2.3、8.3.1：功能/数据/字段权限分离、在服务器校验权限、原 item 绑定、只读元数据、POST actor 来源、敏感字段不投影。服务调用沿原 HTTPS/受限 loopback、TLS 验证、无重定向、no-store 及关闭透明 POST 重放。未更改通用登录/会话机制，也不声称完成整站 ASVS 认证。

## 4. 主会话仍需汇总的边界

- 真实双进程联调和本地浏览器验收属于主会话/前端审查职责，本报告未把它们写成通过。
- receiver 的 reviewed_at_ms 必须与对应 action.applied_at_ms 完全相同；双方已协调接收端使用同一个动作时间，不放宽调用方绑定。
- 需同步设计/spec/wire：effective_mode 的真实含义、enter_review 必须绑定 target、can_reject、actor、source-wide UUID 唯一、完整回执/奖励约束、调用方不按 TTL 自行释放、bounded admin history。该文档范围由主会话持有。
- 生产 cline 当前可用性、生产迁移及发布未执行。两个原验证容器保留；不执行归档或 task finish。
