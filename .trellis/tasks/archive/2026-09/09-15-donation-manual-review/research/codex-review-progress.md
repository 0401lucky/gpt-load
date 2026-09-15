# Codex 复核进行中

> 2026-09-15 续审更新：下文为中断时的历史记录。原 invalid_receipt 已修复，最终双进程/三库及 Linux make check 已通过；浏览器访问被安全策略阻断。最新状态、准确证据和续接入口见 [codex-resume-verification.md](codex-resume-verification.md)，不能继续按下方旧二进制/旧待办判断。

用户要求复核 DeepSeek 实现，完成后保留任务，不归档、不提交、不推送、不部署。任务保持 in_progress。

## 分工

- `/root/review_intake`：gpt-load 产品文件和测试；报告 codex-intake-review.md。
- `/root/review_caller`：new-api 后端与模型/服务测试；报告 codex-caller-review.md。**不编辑 controller/donation_integration_test.go**。
- `/root/review_frontend`：new-api/web；报告 codex-frontend-review.md。
- 主会话：上述跨端契约协调、controller/donation_integration_test.go 的真实两进程/浏览器 fixture、隔离 Linux make check、规范和最终交付。

## 已确认并交各责任方修复

- 前端多发 action_id/test_id 被真实 controller 拒绝400，测试遗漏 expected revision/target。
- review-context 虚报 effective_mode 使转审按钮判断错误；已统一恢复真实模式，另给 review_action/can_reject，离线或目标变化仍可拒绝。
- 过期由 new-api 本地时间释放 owner，缺完整人工回执/entry 绑定、等版本矛盾和模式降级保护。
- 接收端人工路径仍受 probe_unavailable 阻断，审批缺恢复屏障，测试与拒绝未互斥、库存/owner 检查缺失。
- 测试虚拟凭据 ID 原来落在正式 signed ID 范围；改为上半区奇偶分区及边界保护。
- 测试持久化失败仍成功、时间不一致、SSE 过早200、重放/取消/超时/元数据不完整、终态revision遗漏。
- PostgreSQL fixture 漏清新表，遗留schema全局索引造成后续表实际缺唯一约束；已隔离schema并补实际重复INSERT断言。
- 旧捐献schema升级未覆盖、冷热队列及待审UI收敛缺失；补相应测试。
- 原子审核 intent 冻结、跨管理员UUID碰撞、本人拒绝原因、最近20条安全审核/测试元数据展示。

已协调的实现选择：同一个远端动作/test UUID 在 new-api 中数据库唯一，跨actor复用冲突；不为了这点新增客户端 request_key 协议。现有JSON严格白名单不放宽。manual认证模板收紧，旧auto资格保持兼容。

## 主会话验证设施

- Windows 工作目录：`C:/Users/lucky0401/AppData/Local/Temp/donation-codex-review-05f242e3363842ad99c18d03b5bcd987`。
- Linux 持久验证副本：`/home/lucky0401/.cache/donation-codex-review-05f242e3363842ad99c18d03b5bcd987/{gpt-load,new-api}`。
- 不使用 WSL /tmp 保留源码，当前环境会回收它；现副本在独立用户缓存目录，有 .codex-task 标记。
- 同步脚本：Windows目录的 sync-snapshot.py（用 WSL python3 执行，--include-dist复制new-api生成资源，--check校验归一化源哈希）；Linux根下source-manifest.json。
- Linux构建脚本：Windows目录 build-linux.sh；工具复用 `/home/lucky0401/.cache/donation-tools-20260914/{go,node,bin}`，Go1.27.0/Node24.11.0，GOCACHE同缓存，GOMODCACHE为/mnt/c/Users/lucky0401/go/pkg/mod；没有改全局配置。
- Linux初步两二进制构建已通过，完整make check尚待最终代码同步后运行。
- Windows目录已有gpt-load.exe/new-api.exe，**最后一轮已运行测试时它们只构建至14:09/14:10，早于接收端后续统一审核时间修复，必须重建后复验**。

## 实际联调进展（尚未通过整轮）

- 主会话扩展原 controller/donation_integration_test.go，新增 TestDonationManualReviewIntegrationRealServices 和必要fake/故障注入/只读管理员fixture。
- 编译通过；未配置binary时的skip仅代表编译，未计为验收。
- 前两轮真实进程到非流式测试、权限、重复test通过；流式失败来自Gemini fake缺独立STOP+usage尾帧。已按仓库真实SDK fixture修复测试，不放宽生产成功标准。
- 第三轮流式也通过，到approve返回applied，但本地batch待审90秒未结算。旧binary早于接收端审核时间一致性修复，当前先重建再判断；不能据此放宽时间/回执校验。
- 日志在Windows目录 manual-integration-windows.log、manual-integration-windows-2.log、manual-integration-windows-3.log。
- BrowserFixture已增加manual活动、正常/拒绝/可停止的合成key、只读管理员；浏览器验收尚未开始。

## 待完成

1. 使用两端最终源码重建，跑新人工联调和原 TestDonationIntegrationRealServices；如仍失败，核对完整远端回执与本地last_error。
2. 同步最终代码到Linux副本，运行完整make check；结束核对源哈希。
3. 启动最新嵌入UI的BrowserFixture，验收普通用户、管理员、只读管理员及停止/拒绝/转审等关键流程。
4. 接收三个审查报告；核对三库最终证据，完成规范/线协议同步和总体复核记录。
5. 保留in_progress，不archive/finish/commit/push。

当前报告不能当作全部验收通过。实际数据库和前端测试证据分别由责任代理记录，主会话需在交付前汇总。
