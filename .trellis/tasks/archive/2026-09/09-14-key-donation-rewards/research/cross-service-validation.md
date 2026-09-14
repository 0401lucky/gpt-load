# 真实双服务捐献联调验证

日期：2026-09-14。验证代码位于 `D:/code/Claude code program/new-api/controller/donation_integration_test.go`，测试名 `TestDonationIntegrationRealServices`。本文件只记录本地验证；未操作生产配置、账号或真实上游。

## 隔离与执行方式

- 测试以显式环境变量 `DONATION_TEST_GPT_LOAD_BIN`、`DONATION_TEST_NEW_API_BIN` 接收新构建的真实服务二进制；未同时提供时明确 skip，skip 不计作联调通过。
- 两个子进程各自使用 `t.TempDir()` 下的工作目录、SQLite 数据库、日志、密钥材料与临时文件。启动环境使用最小 allowlist，显式清空生产数据库/Redis设置，不读取仓库 `.env`。
- gpt-load 使用 `HOST=127.0.0.1`；new-api 使用本次补足的 `HTTP_LISTEN_HOST=127.0.0.1`。测试在同端口的 `127.0.0.2` 成功绑定，核对实际进程未使用通配监听。
- 客户端 transport 拒绝非 loopback 目标与跳转；上游和服务间故障代理只绑定 loopback。子进程环境代理指向本地拒绝服务器，关闭模型目录同步、任务插件等无关外部任务，并断言没有外部代理请求。
- 使用真实 setup、注册、登录、管理和捐献 HTTP 接口，后台恢复由实际 worker 执行。不通过数据库注入用户、奖励或接收结果。
- 合成 Gemini 上游按提交 key 返回成功、401/`UNAUTHENTICATED` 或 429/`RESOURCE_EXHAUSTED`，并记录调用凭据及模型路径。库存里刻意保留工作 key，以验证失败 key 不借用库存完成探测；组配置显式 `proxy.mode=direct` 指向本地上游。
- 服务间代理先将请求交给真实 gpt-load，读取其持久化回执后丢弃一次 HTTP 响应，临时阻断原批次查询；在两个进程重启后恢复查询。代理不伪造接收结果。

实际执行命令（PowerShell，工作目录 `D:/code/Claude code program/new-api`）：

```powershell
$env:GOFLAGS = '-p=2'
$env:GOMAXPROCS = '2'
$env:DONATION_TEST_GPT_LOAD_BIN = 'D:/code/Claude code program/gpt-load/tmp/donation-e2e/gpt-load.exe'
$env:DONATION_TEST_NEW_API_BIN = 'C:/Users/lucky0401/AppData/Local/Temp/donation-integration-binaries-20260914/new-api-current.exe'
go test ./controller -run '^TestDonationIntegrationRealServices$' -count=1 -v -timeout 10m
```

## 已通过的观察断言

1. 普通账号经实际注册登录；仅 Cookie/匿名请求、普通用户管理接口、跨账号批次读与 retry 被拒绝，额外指定 user/group/reward 字段被拒绝。集成 token 不可访问 gpt-load 管理面。
2. 管理员从真实目录配置普通 Gemini 分组与空库存组；活动只需名称、分组和整数奖励。
3. LF/CRLF、空行、首尾空白、大小写差异、批次内重复和格式错误保留正确行号。有效、401、429、已有库存和重复项独立显示，只有成功新增项增加永久余额。
4. 同一用户多个合格 key 分别奖励；同请求重放保持批次/明细 ID，内容变化冲突；跨用户、活动、分组和请求 ID 的重复不再奖励。
5. 两个普通用户并发争用同 key，且活动金额不同；合计只有一份接收和奖励，赢家收到其活动冻结金额，真实上游仅校验一次。
6. 429 的有界自动尝试结束后，显式 retry 无需重新提交明文；动作重放不重新探测已接收/确定无效项，明细标识不变。
7. 未确认回执不发奖；gpt-load 已接收而 new-api 未确认时，两个进程中断重启，instance/source、原明细、远端凭据和接收时间保持；实际后台 worker 仅查询原批次补发一次。
8. 校验暂停期间关闭活动并改变金额、禁用用户；新批次拒绝，原项保留原金额和版本，禁用期间奖励暂停，重新启用后自动续办一次。
9. 已奖励凭据禁用、删除和删除目标组后，钱包不追扣、历史和唯一奖励记录仍可查询；同 key 在另一组不再奖励，管理员组合筛选仍定位原记录。
10. 钱包的临时额度和实际 Checkin 历史均保持零；捐献响应、审计/普通日志 API、两个进程的日志文件不出现原始合成 key、集成 token 或登录会话凭据。

## 执行结果

- 环境：`go version go1.25.5 windows/amd64`，所有 Go 命令使用 `GOFLAGS=-p=2`、`GOMAXPROCS=2`。
- 最新 new-api 构建命令（new-api 根目录）：`go build -o 'C:/Users/lucky0401/AppData/Local/Temp/donation-integration-binaries-20260914/new-api-current.exe' .`，退出码 0。gpt-load 二进制由主会话从最终 intake 源码和刚构建的 UI 资产提供。
- `gofmt -l controller/donation_integration_test.go` 无输出；`go vet ./controller` 首次与收尾复查均通过。未设 binary 环境变量的定向测试能编译并明确 skip，该结果不算联调通过。
- 首次真实运行完成两个服务启动、回环地址绑定验证、setup、注册和登录，9.91 秒后在 fixture 配置处失败。已直接修正测试的两个问题：gpt-load 的模型 HTTP 输入要求 `alias_enabled:false`；new-api 的非敏感 session-hint Cookie 值 `1` 不能被当作秘密检查，只有 HttpOnly 刷新凭据加入秘密名单。未将这些误报归为生产凭据泄漏。
- 第二次运行完成关键奖励、并发、恢复、暂停和删除后历史检查，52.99 秒后仅在末尾 Checkin fixture 路径失败；将误写的 `/api/user/self/checkin` 修为真实 `/api/user/checkin`。
- 首次全套通过为 47.86 秒。实施方随后确认秘密材料 NULL 校验和提示文案已稳定，因此重新构建最终二进制，再执行上述精确命令，**全部通过**：`--- PASS: TestDonationIntegrationRealServices (47.45s)`，`ok github.com/QuantumNous/new-api/controller 47.696s`，退出码 0。包含清理阶段的全部进程日志扫描、子进程退出和外部请求拒绝检查。
- 本联调没有发现需要改动生产代码的问题；所有测试修复都限定在独占的 `controller/donation_integration_test.go`。生产 `HTTP_LISTEN_HOST` 支持由后端实施方添加，实际回环绑定已验证。

二进制证据（均为本地未发布的开发构建）：

| 二进制 | 构建时间（Asia/Shanghai） | SHA-256 |
| --- | --- | --- |
| `gpt-load.exe` | 2026-09-14 15:19:40 | `8B2E07135FBBADCA89324E08DE69383807D1B554B36EC2CA79BAE5258DC4841E` |
| `new-api-current.exe` | 2026-09-14 15:34:26 | `A47718179B71D72E6A20360D787CC211FF0DD8120051EA310DD753F3FEF3FD7E` |

## 本文件不替代的验证

- SQLite/MySQL/PostgreSQL 新建、存量升级、重复迁移、事务失败和真实数据库唯一约束矩阵由后端专项测试负责。
- 跨自然日的签到清理、主库奖励事务中间断点、Redis 预扣缓存语义、目标配置变化后再验证、普通日志清理后的业务历史需结合后端/intake 定向测试证据；此 HTTP harness 不通过改库或修改系统时间制造这些状态。
- 页面、浏览器缓存、移动端和风险提示的验证由前端及父任务负责；本文件不作全站 OWASP 合规结论。
