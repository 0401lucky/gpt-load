# 捐献页面的本地浏览器验证夹具

日期：2026-09-14。最终桌面/移动端及管理页面已由主会话验收通过，结果见本文末节。夹具代码位于 `D:/code/Claude code program/new-api/controller/donation_integration_test.go` 的 `TestDonationBrowserFixture`，复用已经通过的真实进程、HTTP 和假 Gemini helper。

## 启动

先由主会话完成 `new-api/web` 的 UI 构建，再从 new-api 根构建嵌入最新 `web/dist` 的 new-api 二进制。下面的 `<最新 UI 二进制绝对路径>` 必须替换为该文件；夹具不会自行修改或构建 web。

PowerShell，工作目录 `D:/code/Claude code program/new-api`：

```powershell
$env:GOFLAGS = '-p=2'
$env:GOMAXPROCS = '2'
$env:DONATION_BROWSER_FIXTURE = '1'
$env:DONATION_BROWSER_FIXTURE_DIR = Join-Path ([System.IO.Path]::GetTempPath()) ('donation-browser-' + [guid]::NewGuid().ToString('N'))
$env:DONATION_TEST_GPT_LOAD_BIN = 'D:/code/Claude code program/gpt-load/tmp/donation-e2e/gpt-load.exe'
$env:DONATION_TEST_NEW_API_BIN = '<最新 UI 二进制绝对路径>'
go test ./controller -run '^TestDonationBrowserFixture$' -count=1 -v -timeout 2h
```

该命令会持续运行。目录必须是**尚不存在的新绝对路径**，父目录须存在；已有目录直接拒绝，不覆盖或重用已有数据库。未设置 `DONATION_BROWSER_FIXTURE=1` 时，正常 `go test` 会明确 skip 此入口。

就绪时仅输出：

```text
Donation browser fixture ready; private metadata: <fixture_dir>\metadata.json
```

元数据先写临时文件再原子改名，出现 `metadata.json` 表示账号、连接、分组、活动和普通用户活动读取已全部验证成功。文件以 0600、目录以 0700 创建，放在本次私有夹具目录；内容只包含本地合成账号/凭据，没有读取真实服务秘密。日志不打印元数据全文。

## 元数据与可用数据

| 字段 | 用途 |
| --- | --- |
| `base_url` | 浏览器访问的实际 new-api 地址，固定 loopback、随机空闲端口。 |
| `accounts` | `browserroot`（root）、`browser_one`、`browser_two`，包含合成密码和真实本地用户 ID；从正常登录页面登录。 |
| `campaign` | 已开启的 Gemini 活动及 ID/group_id，`reward_quota=500000`，便于观察永久余额变化。 |
| `empty_group_id` | 另外创建并删除种子凭据的空组，可用于验证管理员分组选择和新活动保存。 |
| `gpt_load` | 实际 gpt-load 地址、服务间代理地址，以及仅本夹具有效的合成管理/集成 token，供连接表单验证。 |
| `keys` | 下表样例及预组合的 `mixed_desktop_text`。 |
| `controls` | 停止文件和解除限流文件的绝对路径。 |
| `processes` | 本次 runner、gpt-load、new-api 的 PID，便于核对生命周期。 |
| `shutdown_deadline_at_ms` | 若 Go 测试设置了截止时间，则在其前 30 秒走有序清理，避免超时 panic 留下子进程。 |

账号最初没有捐献记录。三个不同的有效 key 可分别留给桌面、移动端及账号切换检查；同一个 key 后续重复提交只显示重复，避免把测试输入耗尽误判成 UI 故障。

| key 字段 | 假上游与预期 |
| --- | --- |
| `valid_desktop` / `valid_mobile` / `valid_other_user` | 不同合成 key，真实 Gemini generation HTTP 返回 200；首次新增后各发一笔永久奖励。 |
| `invalid` | 返回 401 / UNAUTHENTICATED，明细无效且不奖励。 |
| `inventory` | 已由真实 gpt-load 管理接口导入，显示已有资源且不奖励。 |
| `rate_limited` | 返回 429 / RESOURCE_EXHAUSTED；出现待重试，耗尽自动次数后可操作重试。 |
| `mixed_desktop_text` | 五行：有效、无效、库存、重复有效、429；包含 LF/CRLF。只能给成功新增项奖励，不能展示整批全成功。 |

读取元数据示例（使用就绪日志中的实际路径）：

```powershell
$donationBrowserMeta = Get-Content -LiteralPath '<fixture_dir>/metadata.json' -Raw | ConvertFrom-Json
# 仅取所需字段用于浏览器，不把账号/token 或元数据全文写入任务日志。
$donationBrowserMeta.base_url
```

## 解除限流及停止

解除样例 key 的限流后，可在页面上对原批次重试，观察原明细继续接收和到账：

```powershell
[System.IO.File]::WriteAllText($donationBrowserMeta.controls.resolve_rate_limit_file, '')
```

夹具会在下一次 250ms 文件轮询时将该 key 的上游结果切换为 200，并打印一条不含 key 的确认。这个操作不会重新提交批次或伪造奖励，实际 gpt-load/new-api worker 仍负责处理。

浏览器检查结束后，通过元数据指定的停止文件关闭：

```powershell
[System.IO.File]::WriteAllText($donationBrowserMeta.controls.stop_file, '')
```

runner 收到停止文件或 SIGINT/SIGTERM 后返回清理路径：只停止其直接启动的两个服务，等待退出，关闭本地假上游/代理，检查进程日志，然后验证已解析目录路径与随机所有权标记，最后删除这一个夹具目录。不会删除其他目录或按进程名称批量终止服务。日常使用已实测的停止文件路径。

保留 `-timeout 2h` 可给浏览器操作提供有界时间；需要显式不限时等待时可改为 `-timeout 0`，仍使用停止文件结束。服务意外退出会使夹具报错并清理剩余进程。

## 夹具自检证据

- 未启用模式时：`go test ./controller -run '^TestDonationBrowserFixture$' -count=1 -v` 编译成功并明确 skip。
- 短暂自检使用已有开发二进制，仅检查 helper、接口就绪和生命周期，没有据此宣称新 UI 已验收。
- 自检目录：`C:/Users/lucky0401/AppData/Local/Temp/donation-browser-selfcheck-67bbd3991525410681a5dd4a145a75d4`；元数据完整发布，实际 `/api/status` 返回 success，3 个合成账号、1 个启用活动正确。
- 实际创建解除限流文件，runner 输出 `rate-limited key is now available`；再创建 stop 文件，`TestDonationBrowserFixture` **PASS 65.11s**，package **65.360s**，退出码 0。
- 结束后独立检查该目录已删除，runner PID 32948、gpt-load PID 39880、new-api PID 35268 均已退出。此自检地址已关闭，不供后续浏览器使用。
- `gofmt -l controller/donation_integration_test.go` 无输出；`go vet ./controller` 通过。
- 原 `TestDonationIntegrationRealServices` 的所有断言保留；抽取共享构造器后再次真实运行，**PASS 46.83s**，package **47.055s**。此前后端正式 47.45 秒联调证据原样保留，生产源码未因夹具扩展改变。

## 实际 UI 验收

主会话已对翻译完成前的 preview 构建进行初步真实浏览器验证：登录返回 `/donations` 正常；桌面顶部入口可达；五行混合提交独立显示有效、无效、库存、重复、限流，首次接收1/5并到账$1；提交确认后输入清空。通过夹具解除429后在页面点击重试，原批次最终接收2/5并累计到账$2，没有重奖。桌面未出现页面级横向溢出，风险提示位于输入区之后、提交按钮之前。

上述是预览检查，尚不能代替最终七语言构建、移动端、管理员表单和跨账号页面缓存验收。预览截图位于 gpt-load 的 tmp/donation-ui-review/preview-desktop.png。

用户离开数小时后续接时，旧 preview runner 已因2h Go测试超时退出（日志 browser-preview.log）；主会话核对其两个子进程的parent PID与绝对可执行路径后，已停止仅这两个测试进程。最终浏览器夹具改用显式stop文件、当前活跃验收期间运行，避免机器长时间暂停导致测试alarm与清理定时器同时触发。最终结果另补在本节。

旧预览目录 `C:/Users/lucky0401/AppData/Local/Temp/donation-browser-preview-58e31616324441d69d57f25afcb622d0` 的递归清理请求被自动审批检查以 `blocked by policy` 拒绝；未改用其他手段绕过。目录保留，两个子进程已停止；其中仅有本次合成测试账号、key 与临时数据库，不是生产数据。

## 最终构建的真实浏览器验收

最终 `bun run build:check` 退出0（Rsbuild 32.3s），之后从 new-api 根目录执行 `go build -o tmp/donation-e2e/new-api-ui.exe .`，退出0。此后没有修改前端或后端生产源码。

| 二进制 | SHA-256 |
| --- | --- |
| gpt-load `tmp/donation-e2e/gpt-load.exe` | `8B2E07135FBBADCA89324E08DE69383807D1B554B36EC2CA79BAE5258DC4841E` |
| new-api `tmp/donation-e2e/new-api-ui.exe` | `4E6E2D08D25BDAF23A7152E615EDEC0849BB3ECF123CC653AF5E50785F247148` |

按上述启动方法运行 `TestDonationBrowserFixture`，本次使用 `-timeout 0`，以停止文件结束。独立 Playwright 浏览器连接 `http://127.0.0.1:50861`，使用中文界面，桌面视口1440×1000、移动视口390×844。真实两个 Go 进程、临时 SQLite、现有注册/登录/权限/后台 worker 和本地 Gemini HTTP 服务参与处理；没有用浏览器 mock 接收或奖励响应。

| 场景 | 实际结果 |
| --- | --- |
| 公共与登录入口 | 桌面公共顶栏显示捐献；公共移动菜单可进入，匿名访问返回 `/sign-in?redirect=%2Fdonations`。普通用户经真实登录后回到捐献页。应用内移动菜单可进入捐献并自动关闭。 |
| R18 与输入 | 中文原文逐字可见，位于输入之后、提交按钮之前。移动端自然分3行，字号12px、行高19.5px，实际合成背景计算对比度约5.85:1；桌面/移动均无页面级横向溢出。捐献提交没有确认弹窗或强制勾选。 |
| 五行混合提交 | `browser_one` 提交有效、401、库存、重复有效、429。回执确认后立即清空输入，初始排队不误报成功；随后1/5接收、1无效、2重复/已有、1待处理，永久到账$1。原行号1–5及掩码正确。 |
| 新草稿与旧查询 | 已确认混合批次仍在处理时，输入下一份合成草稿，再通过真实GET刷新原批次；新草稿保持不变。随后主动清空该草稿，避免把原文留在截图中。 |
| 原批次重试 | 上游429自动尝试用尽后，页面显示可重试及原因。在空输入状态解除夹具429并点击原批次重试，最终2/5接收、0处理中、永久累计$2，首个奖励没有重发。 |
| 移动端提交 | 同账号通过移动页提交另一条合格 key，确认后清空输入，最终1/1接收、永久到账$1；移动结果自动采用卡片排列。该账号共有三条已到账记录。 |
| 账号切换 | 经现有登出和真实登录切换到 `browser_two`，捐献页为空历史、空输入且没有前一账号结果。提交原已捐献 key 与另一条新 key，最终1/2接收、1重复、仅$1到账，归属 user_id=3；只有自己的这一批历史。 |
| 普通用户管理限制 | 普通用户无配置/记录入口；直接访问 `/donations/settings` 最终进入403，没有显示管理数据。 |
| 连接凭据 | root 登录后读取连接，token输入为空且类型为password。填入本夹具原集成token并保存成功，输入清空，实际响应不含token；Cache-Control为`no-store, no-cache, must-revalidate, private, max-age=0`。 |
| 活动与真实空组 | 在管理对话框创建 `gemini aistudio free key`，下拉实际列出 `browser-gemini` 与 `browser-empty`，选择空组ID=2、固定$2并开启。实际POST仅含name/description/group_id/reward_quota/enabled，服务端保存group_id=2/reward_quota=1000000。没有平台、上游、测试模型、奖励有效期或领奖上限字段。 |
| 活动改名 | 将新活动改名后实际PATCH成功，group_id、target_revision和reward_quota均与原保存值一致。实际接收路径与该组的对应关系另由真实双服务专项覆盖。 |
| 管理记录 | 页面显示8条真实明细及原用户、原行号、掩码、分组、凭据ID和奖励。组合筛选user_id=2/group_id=1/reward_state=rewarded后恰为3条，均属于browser_one。 |
| 管理详情 | 查看移动提交详情，能对应browser_one、group=1、credential_id=5、原明细UUID、唯一奖励UUID、$1与真实接收/奖励毫秒时间；处理历史包含排队、接收、发奖。响应无完整key/token。移动管理记录也能正常换行，无页面横向溢出。 |
| 持久存储 | 普通用户及管理员完成凭据保存/账号切换后，逐一扫描localStorage/sessionStorage内容，所有已知合成key、集成token、登录测试密码均未命中。IndexedDB数据库与CacheStorage均为空；localStorage仅有既有语言、状态、配置缓存和设备上报标记。 |
| 管理员本人页 | 从全站记录切回root的“我的捐献”，显示空历史，没有把他人的管理记录带入本人结果。 |

人工逐项验收通过。自动化观察中有一次重试等待超过30秒窗口，后续实际GET、DOM和截图确认了上述2/5、$2终态；另外修正了“普通登出后登录仍携带捐献redirect”的观察假设（既有登出后登录正常进入dashboard）及工具VM不提供URL全局对象的问题。这些没有通过改动业务代码掩盖。首次匿名刷新401是既有认证流程；对比度测量曾触发Canvas性能提示，最终应用页没有console error/warning。

最终截图在 gpt-load 的 `tmp/donation-ui-review/`：`final-desktop-before-submit.png`、`final-desktop-mixed.png`、`final-desktop-retry.png`、`final-mobile-before-submit.png`、`final-mobile-result.png`、`final-admin-campaign.png`、`final-admin-records.png`、`final-admin-detail.png`、`final-mobile-admin-records.png`。均已实际查看，不含明文key或token；这些本地临时图片不纳入代码提交。

## 最终夹具退出

- 通过本次元数据指定的stop文件关闭，`TestDonationBrowserFixture` **PASS 2305.78s**，package **2306.032s**，退出码0；包含真实子进程日志的秘密扫描、假上游调用检查和清理。完整输出在 [browser-final.log](browser-final.log)。
- 本次目录 `C:/Users/lucky0401/AppData/Local/Temp/donation-browser-final-167a933f382a46d1b966977b7cb90e69` 已由已验证的夹具生命周期删除。
- 独立检查：runner 44364、gpt-load 44456、new-api 45160 均已退出，目录不存在。本次临时地址不再提供服务。
- 旧preview目录仍按先前自动审批拒绝保留；没有以本次夹具清理绕过旧目录的拒绝。
