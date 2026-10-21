# 本轮固定起点对照

## 同条件复现

使用 `git -c core.autocrlf=false worktree add --detach` 在仓库外建立固定 `216024cc31c5de4c31a11777c41746bf9ac752d3` 基线，复制合并前保留的内嵌 UI 产物，仅运行只读测试。基线与合并版本都在 Windows amd64、Go 1.27.3、相同进程环境下执行 `go test -json -count=1 . ./internal/...`。

| 结果 | 基线 | 合并版本 |
| --- | --- | --- |
| 包通过 | 64 | 65 |
| 无测试/跳过 | 8 | 8 |
| 包失败 | 5 | 5 |
| 顶层失败测试 | 23 | 23 |
| 含子测试失败 | 82 | 82 |

包含包与子测试名的完整失败集合：新增 `[]`，消除 `[]`。新增的通过包为上游 `internal/execution/responsemodel`。两份标准错误文件均为空。独立 CPA 基线与最终合并版本均为 2 包通过。

## 相同的 23 个顶层失败

- catalog：`TestCacheReplaceErrorAndRollbackFailureRetainsPreviousBackupPath`、`TestCacheRollbackReplaceFailureKeepsRecoverablePreviousBackup`。
- container：`TestSystemOutboundProxyProviderUsesEnvironmentAndLatestGlobalSnapshot`。
- gateway：`TestBufferedWriteDeadlineStopsRealTCPSlowReader`。
- platform/authkey：`TestResolveLogsPathWithoutMaterialOnlyWhenCreated`。
- webui：`TestWindowsSmokeRecovery`、`TestDatabaseShardsCoverEveryExternalTestExactlyOnce`、`TestMySQLShardsRunConcurrentlyAndPropagateFailures`、`TestDockerSmokeCancellationCleansOnlyOwnedResources`、`TestDockerSmokeDiscoversLoopbackPortOnEveryContainerStart`、`TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest`、`TestGoFormattingScriptsFailClosed`、`TestReleaseWorkflowUsesTagOnlyTriggerAndStrictSemverGuard`、`TestReleasePublicationStateClassifiesFreshConsistentConflictAndPartial`、`TestReleaseWorkflowUsesTrustedCurrentRunChecksumForExistingRelease`、`TestReleaseWorkflowKeepsUntrustedImageRevisionInsideJQComparison`、`TestReleaseImageVersionAcceptsOnlyMatchingStrictSemverLabels`、`TestReleaseWorkflowRetriesStaleRenderHealthVersion`、`TestReleaseImageDigestFailsClosedOnOperationalInspectionErrors`、`TestReleaseSemverComparatorOrdersChannelCandidates`、`TestReleaseImageChannelPromotionIsMonotonicAndPreservesLatest`、`TestReleaseImageDigestReturnsAbsentOnlyForMissingManifest`、`TestReleaseWorkflowReusesCIVerdictOnlyOnProvenSuccess`。

## 错误信息核对

- catalog 和 authkey 在两边相同源码行失败：Windows 反斜杠路径经引号转义，原路径字符串无法匹配日志。临时目录随机后缀不同，失败语义相同。
- container 两边均收到 `Mode:direct` / `ProxyID:0` / `Source:default`，不满足当前环境代理预期。
- gateway 两边在 `stream_integration_test.go:780` 收到 `writeUpstreamResponse() error = nil`，未达到真实 TCP 慢读端的写入时限条件。
- webui 逐子测试核对为相同的 Bash 无法读取 Windows 路径/环境、直接执行 `.sh` 的 Win32 格式错误、Python 命令不可用、PowerShell temporary recovery fixture 失败。Render 和 shard 脚本的 exit 127、release promotion 缺环境字段在两边相同。
- 没有以旧任务日志或上次 Windows 结论替代本轮验证；没有为通过而删测试、加 Skip、改变安全约束或放宽时限。

## 其他首次失败与修复

- CPA 新版本 client JSON 的工作树 CRLF 导致 `TestClientSnapshot` digest 失败；只转 LF 后源码与固定上游 blob 相同，SHA256 正确，独立 CPA 完整测试最终通过。
- Python release CLI 默认 GBK 的两项 UnicodeDecodeError 在固定旧版本同条件重现；两边进程级 UTF-8 模式通过，最终 release fake suite 17 项通过。

## 原始证据

`%TEMP%/gpt-load-merge-20261021-b0fc72ed/` 下保存完整根/CPA 两边 JSONL、stderr、`failure-comparison.json`、`baseline-python-tests-gbk.log` 与 `python-tests-utf8.log`；基线 worktree 为该目录下 `baseline/`。
