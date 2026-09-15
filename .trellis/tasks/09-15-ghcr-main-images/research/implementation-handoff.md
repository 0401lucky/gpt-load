# Implementation handoff

## Implemented scope

- `.github/workflows/ghcr-main.yml`: only `0401lucky/gpt-load`, `refs/heads/main`, push/manual events. SHA-pinned Actions and GitHub-hosted Ubuntu runners. Source-build publishes `ghcr.io/0401lucky/gpt-load:sha-<full-commit>` for linux/amd64 and linux/arm64; version is `2.0.0-dev.<run-number>.g<12-character-sha>`.
- The verify matrix pulls the build output digest and reuses the existing digest, revision, version, and Docker runtime smoke helpers. Each target architecture has its own native runner. Promotion depends on all verification jobs succeeding.
- `.github/scripts/main-promote-image-channels.sh`: rejects other repositories/refs/events, rechecks the verified digest and labels, then queries remote main immediately before one `imagetools create` invocation for latest/main. Stale runs report `promoted=false`; API/inspection failures stop before channel writes. Both channel digests are checked after publication.
- Only build/promote jobs grant `packages: write`; verification grants `packages: read`. Registry/API authentication uses `GITHUB_TOKEN`. The promotion job shares a fixed concurrency group with cancellation disabled.
- `docker-compose.yml` defaults to `${GPT_LOAD_IMAGE:-ghcr.io/0401lucky/gpt-load:latest}`; `.env.example` exposes the optional override. Ports, runtime configuration, mounts, project/data volume semantics, and keys retain their existing behavior.
- `README.md`, `README_CN.md`, `README_JP.md`: fork clone/default image, update within the existing project, paired database/key backup, commit/digest pinning, mutable commit rebuild tags, and upstream Release distinction. The existing 1.x migration prohibition remains.
- `internal/webui/container_contract_test.go`: updated default-image contract and real Compose override/project-data tests. `internal/webui/workflow_main_images_test.go`: publication dependencies/permissions and local fake registry/remote failure coverage.

No existing CI/Release workflow, shared release helper, Dockerfile, business Go code, dependency, production service, or unrelated file was changed. Scripts are called explicitly with `bash`, so the new script does not require an executable-bit change on Windows.

## Verification by implementer

- Regression first: the new default/override tests failed against the original tbphp image, and the workflow contract failed because the workflow did not yet exist.
- Windows: `go test ./internal/webui -run '^(TestCompose|TestMainImageWorkflow|TestNetworkConfiguration)' -count=1` passed (`4.332s`). This runs real `docker compose config`, including the unchanged port and volume contracts.
- actionlint 1.7.12: new workflow passed.
- WSL `bash -n .github/scripts/main-promote-image-channels.sh`: passed.
- `gofmt` and `git diff --check`: passed; Git emitted only the existing Windows checkout line-ending conversion notices.
- Linux LF clone: `go test ./internal/webui -run '^(TestMainImage|TestReleaseImageVersionAcceptsOnlyMatchingStrictSemverLabels)' -count=1 -v` passed (`1.790s`), including all 12 promotion success/manual/stale/failure cases and six existing strict-version helper cases. No source fixes were required. Use `wsl -d Ubuntu --exec bash <env.sh> ...` to pass regex arguments without the default zsh expanding them.

## Remaining parent-owned evidence

- Complete `make check` and independent review on the same LF source snapshot.
- Actual source image build/runtime checks, followed by the first GitHub workflow run and remote manifest/digest inspection.
- GHCR package visibility and anonymous pull behavior must be verified after first publication; repository visibility alone does not establish package visibility.
- Runtime smoke is configured for both native architectures, but this handoff does not claim those image runs have already happened. This workflow explicitly skips vulnerability scanning and records that fact in its summaries.
- Registry updates to two tags are not a transaction. A push or post-check failure can leave a partial channel update; any digest written by this script is nevertheless the previously verified candidate. A successful rerun on the current main can reconcile the channels. SHA tags may change on rebuild; digest references remain the exact deployment pin.

The parent synchronized the nine candidate source files to the LF verification clone before these Linux tests. Source remains frozen; any fixes from later full checks/review will be reported before changing it. Git commit/push, package publication, and deployment were not performed by this implementer.
