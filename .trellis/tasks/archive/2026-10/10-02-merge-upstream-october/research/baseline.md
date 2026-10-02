# 同条件测试基线（2026-10-02）

- 原版本：`c79bca8f`；合并目标：`addd6987`。
- Go 1.27.0 windows/amd64，测试进程 GOMAXPROCS=4；均使用对应 Git 内容的 LF 工作副本。
- 完整测试与失败包复测均使用 `-count=1`，未用缓存结果替代。
- LF 复测范围：catalog、container、platform/authkey、gateway、webui。
- 两边失败集合完全相同，含子测试及包级结果；新增失败数 0，顶层失败测试 23。

## 相同的失败用例

### `gpt-load/internal/catalog`

- `TestCacheReplaceErrorAndRollbackFailureRetainsPreviousBackupPath`
- `TestCacheRollbackReplaceFailureKeepsRecoverablePreviousBackup`

### `gpt-load/internal/container`

- `TestSystemOutboundProxyProviderUsesEnvironmentAndLatestGlobalSnapshot`

### `gpt-load/internal/gateway`

- `TestBufferedWriteDeadlineStopsRealTCPSlowReader`

### `gpt-load/internal/platform/authkey`

- `TestResolveLogsPathWithoutMaterialOnlyWhenCreated`

### `gpt-load/internal/webui`

- `TestDatabaseShardsCoverEveryExternalTestExactlyOnce`
- `TestDockerSmokeCancellationCleansOnlyOwnedResources`
- `TestDockerSmokeDiscoversLoopbackPortOnEveryContainerStart`
- `TestGoFormattingScriptsFailClosed`
- `TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest`
- `TestMySQLShardsRunConcurrentlyAndPropagateFailures`
- `TestReleaseImageChannelPromotionIsMonotonicAndPreservesLatest`
- `TestReleaseImageDigestFailsClosedOnOperationalInspectionErrors`
- `TestReleaseImageDigestReturnsAbsentOnlyForMissingManifest`
- `TestReleaseImageVersionAcceptsOnlyMatchingStrictSemverLabels`
- `TestReleasePublicationStateClassifiesFreshConsistentConflictAndPartial`
- `TestReleaseSemverComparatorOrdersChannelCandidates`
- `TestReleaseWorkflowKeepsUntrustedImageRevisionInsideJQComparison`
- `TestReleaseWorkflowRetriesStaleRenderHealthVersion`
- `TestReleaseWorkflowReusesCIVerdictOnlyOnProvenSuccess`
- `TestReleaseWorkflowUsesTagOnlyTriggerAndStrictSemverGuard`
- `TestReleaseWorkflowUsesTrustedCurrentRunChecksumForExistingRelease`
- `TestWindowsSmokeRecovery`

## 证据解释

Windows 路径转义、环境代理、真 TCP 时限及脚本调用环境失败均已在原提交复现；详见 verification.md。原始 JSON 日志在本机临时目录保留，不提交测试运行产生的大量日志。
