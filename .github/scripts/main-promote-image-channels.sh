#!/usr/bin/env bash
set -euo pipefail

# 只能由本 fork 的 main 发布；独立校验调用边界，避免手动重跑其它分支。
[[ "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}" == "0401lucky/gpt-load" ]]
[[ "${GITHUB_REF:?GITHUB_REF is required}" == "refs/heads/main" ]]
case "${GITHUB_EVENT_NAME:?GITHUB_EVENT_NAME is required}" in
  push | workflow_dispatch) ;;
  *) exit 1 ;;
esac

revision="${GITHUB_SHA:?GITHUB_SHA is required}"
verified_digest="${VERIFIED_DIGEST:?VERIFIED_DIGEST is required}"
image_version="${IMAGE_VERSION:?IMAGE_VERSION is required}"
output_file="${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
[[ "${revision}" =~ ^[0-9a-f]{40}$ ]]
[[ "${verified_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "${image_version}" == 2.*-dev.* ]]

repository=ghcr.io/0401lucky/gpt-load
source="${repository}@${verified_digest}"
# 验证和推广只读取成功运行过的 digest；提交 tag 在重建时可能被覆盖。
test "$(bash .github/scripts/release-image-digest.sh "${source}")" = "${verified_digest}"
bash .github/scripts/release-verify-image-revision.sh "${source}" "${revision}" >/dev/null
test "$(bash .github/scripts/release-image-version.sh "${source}")" = "${image_version}"

# 调用方持有 workflow 的通道锁。所有耗时检查完成后、写入前再查询远端；
# API/网络失败或不可识别的响应直接失败，不能退回本地 checkout 的 main。
remote_revision="$(
  gh api --hostname github.com repos/0401lucky/gpt-load/git/ref/heads/main \
    --header 'Cache-Control: no-cache' --jq '.object.sha'
)"
[[ "${remote_revision}" =~ ^[0-9a-f]{40}$ ]]
if [[ "${remote_revision}" != "${revision}" ]]; then
  printf 'skip channel promotion: remote main is %s, candidate is %s\n' \
    "${remote_revision}" "${revision}"
  printf 'promoted=false\n' >>"${output_file}"
  exit 0
fi

docker buildx imagetools create \
  --tag "${repository}:latest" \
  --tag "${repository}:main" \
  "${source}"

for channel in latest main; do
  test "$(bash .github/scripts/release-image-digest.sh "${repository}:${channel}")" = "${verified_digest}"
done
printf 'promoted=true\n' >>"${output_file}"
