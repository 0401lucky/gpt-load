# 本轮同条件基线对照

固定基线 dbb2b42b，合并目标 a5c691bb；Go 1.27.0 / Windows amd64。两边都执行 go test -json -count=1 . ./internal/...。

## 统计

- baseline: {'pass': 62, 'skip': 8, 'fail': 6}；顶层失败 24。
- merged: {'pass': 64, 'skip': 8, 'fail': 5}；顶层失败 23。

包含子测试的新增失败集合：[]。

基线额外失败为 TestRuntimeFirstByteAndStreamIdleTimeouts/stream_first_byte_requires_a_complete_data_event；合并版本通过。

## 两边共有的顶层失败

- `gpt-load/internal/catalog` / `TestCacheReplaceErrorAndRollbackFailureRetainsPreviousBackupPath`
- `gpt-load/internal/catalog` / `TestCacheRollbackReplaceFailureKeepsRecoverablePreviousBackup`
- `gpt-load/internal/container` / `TestSystemOutboundProxyProviderUsesEnvironmentAndLatestGlobalSnapshot`
- `gpt-load/internal/gateway` / `TestBufferedWriteDeadlineStopsRealTCPSlowReader`
- `gpt-load/internal/platform/authkey` / `TestResolveLogsPathWithoutMaterialOnlyWhenCreated`
- `gpt-load/internal/webui` / `TestDatabaseShardsCoverEveryExternalTestExactlyOnce`
- `gpt-load/internal/webui` / `TestDockerSmokeCancellationCleansOnlyOwnedResources`
- `gpt-load/internal/webui` / `TestDockerSmokeDiscoversLoopbackPortOnEveryContainerStart`
- `gpt-load/internal/webui` / `TestGoFormattingScriptsFailClosed`
- `gpt-load/internal/webui` / `TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest`
- `gpt-load/internal/webui` / `TestMySQLShardsRunConcurrentlyAndPropagateFailures`
- `gpt-load/internal/webui` / `TestReleaseImageChannelPromotionIsMonotonicAndPreservesLatest`
- `gpt-load/internal/webui` / `TestReleaseImageDigestFailsClosedOnOperationalInspectionErrors`
- `gpt-load/internal/webui` / `TestReleaseImageDigestReturnsAbsentOnlyForMissingManifest`
- `gpt-load/internal/webui` / `TestReleaseImageVersionAcceptsOnlyMatchingStrictSemverLabels`
- `gpt-load/internal/webui` / `TestReleasePublicationStateClassifiesFreshConsistentConflictAndPartial`
- `gpt-load/internal/webui` / `TestReleaseSemverComparatorOrdersChannelCandidates`
- `gpt-load/internal/webui` / `TestReleaseWorkflowKeepsUntrustedImageRevisionInsideJQComparison`
- `gpt-load/internal/webui` / `TestReleaseWorkflowRetriesStaleRenderHealthVersion`
- `gpt-load/internal/webui` / `TestReleaseWorkflowReusesCIVerdictOnlyOnProvenSuccess`
- `gpt-load/internal/webui` / `TestReleaseWorkflowUsesTagOnlyTriggerAndStrictSemverGuard`
- `gpt-load/internal/webui` / `TestReleaseWorkflowUsesTrustedCurrentRunChecksumForExistingRelease`
- `gpt-load/internal/webui` / `TestWindowsSmokeRecovery`

## 消息核对

- catalog 两例在相同 cache_test.go 断言失败，日志中的 Windows 反斜杠经过引号转义后无法与原路径匹配。
- authkey 在相同 authkey_test.go:85 断言路径未匹配，日志本身只含测试路径、无凭据。
- container 两边均得到 Mode:direct / Source:default；合并输出多出上游 ProxyID:0 字段但失败语义相同。
- gateway 两边均在 stream_integration_test.go:780 收到 writeUpstreamResponse() error=nil。
- webui 多数为 Bash 不识别 Windows 路径（exit 127），另有直接执行 .sh 的 Win32 格式错误、Python 命令 exit 9009，以及相同 promotion/Windows smoke 子进程失败。
- 首轮 MySQL shards 的一个子进程输出受并发 WSL 启动影响，已在两边顺序定向复验该测试，均复现 exit 127、Bash 不识别 Windows 临时脚本路径；原始日志为临时目录的 baseline-shards.jsonl / merged-shards.jsonl。

## 原始证据

- `%TEMP%/gpt-load-upstream-20261008-baseline-tests.jsonl`
- `%TEMP%/gpt-load-upstream-20261008-merged-tests.jsonl`
- `%TEMP%/gpt-load-upstream-20261008-baseline-shards.jsonl`
- `%TEMP%/gpt-load-upstream-20261008-merged-shards.jsonl`

基线工作树使用仓库外独立 LF checkout，合并前嵌入UI产物已复制；未使用之前任务的结论替代本轮复现。
