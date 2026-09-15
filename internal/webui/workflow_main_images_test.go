package webui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestMainImageWorkflowGatesChannelsOnVerifiedSourceBuild(t *testing.T) {
	content := readRepositoryFile(t, ".github/workflows/ghcr-main.yml")
	events := workflowTopLevelBlock(t, content, "on")
	if !strings.Contains(events, "push:\n    branches: [main]") ||
		!strings.Contains(events, "  workflow_dispatch:") {
		t.Fatalf("main image workflow lacks main push/manual triggers:\n%s", events)
	}
	for _, match := range regexp.MustCompile(`(?m)^  ([a-z_]+):`).FindAllStringSubmatch(events, -1) {
		if match[1] != "push" && match[1] != "workflow_dispatch" {
			t.Fatalf("main image workflow accepts unexpected event %q", match[1])
		}
	}
	if got := strings.TrimSpace(workflowTopLevelBlock(t, content, "permissions")); got != "permissions:\n  contents: read" {
		t.Fatalf("main image workflow default permissions = %q", got)
	}
	if !strings.Contains(content, "IMAGE_REPOSITORY: ghcr.io/0401lucky/gpt-load") {
		t.Fatal("main image workflow does not target this fork's GHCR repository")
	}
	for _, forbidden := range []string{"tbphp/", "DOCKERHUB", "self-hosted", "continue-on-error:", "always()"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("main image workflow contains %q", forbidden)
		}
	}
	immutableAction := regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	for _, match := range regexp.MustCompile(`(?m)^\s*uses:\s+([^#\s]+)`).FindAllStringSubmatch(content, -1) {
		if !immutableAction.MatchString(match[1]) {
			t.Fatalf("main image workflow has mutable action %q", match[1])
		}
	}
	for _, name := range []string{"build", "verify", "promote"} {
		job := workflowJobBlock(t, content, name)
		for _, required := range []string{
			"github.repository == '0401lucky/gpt-load' &&",
			"github.ref == 'refs/heads/main' &&",
			"(github.event_name == 'push' || github.event_name == 'workflow_dispatch')",
			"persist-credentials: false",
			"password: ${{ secrets.GITHUB_TOKEN }}",
		} {
			if !strings.Contains(job, required) {
				t.Fatalf("main image job %s lacks publication boundary %q", name, required)
			}
		}
		wantPackages := "write"
		if name == "verify" {
			wantPackages = "read"
		}
		if !strings.Contains(job, "packages: "+wantPackages) {
			t.Fatalf("main image job %s lacks packages: %s", name, wantPackages)
		}
	}

	build := workflowJobBlock(t, content, "build")
	buildStep := workflowStepBlock(t, build, "Build and push commit image")
	for _, required := range []string{
		"context: .", "target: source-build", "platforms: linux/amd64,linux/arm64", "push: true",
		"tags: ${{ env.IMAGE_REPOSITORY }}:sha-${{ github.sha }}",
		"org.opencontainers.image.revision=${{ github.sha }}",
		"org.opencontainers.image.version=${{ steps.metadata.outputs.version }}",
		"VERSION=${{ steps.metadata.outputs.version }}",
	} {
		if !strings.Contains(buildStep, required) {
			t.Fatalf("main source build lacks %q", required)
		}
	}
	if strings.Contains(buildStep, ":latest") || strings.Contains(buildStep, ":main") {
		t.Fatal("main source build publishes channels before verification")
	}
	verify := workflowJobBlock(t, content, "verify")
	for _, required := range []string{
		"needs: build", "runner: ubuntu-24.04\n", "runner: ubuntu-24.04-arm\n",
		"RELEASE_SMOKE_SOURCE_IMAGE: ${{ env.IMAGE_REPOSITORY }}@${{ needs.build.outputs.digest }}",
		"RELEASE_VERSION: ${{ needs.build.outputs.version }}",
		"RELEASE_SMOKE_SKIP_SCAN: \"true\"",
		"bash .github/scripts/release-docker-smoke.sh",
		"release-verify-image-revision.sh", "release-image-version.sh", "release-image-digest.sh",
	} {
		if !strings.Contains(verify, required) {
			t.Fatalf("main image verification lacks %q", required)
		}
	}
	promote := workflowJobBlock(t, content, "promote")
	for _, required := range []string{
		"needs: [build, verify]", "group: ghcr-main-image-channels", "cancel-in-progress: false",
		"VERIFIED_DIGEST: ${{ needs.build.outputs.digest }}",
		"IMAGE_VERSION: ${{ needs.build.outputs.version }}",
		"bash .github/scripts/main-promote-image-channels.sh",
	} {
		if !strings.Contains(promote, required) {
			t.Fatalf("main channel promotion lacks %q", required)
		}
	}
}

func TestMainImagePromotionChecksRemoteHeadAndUsesVerifiedDigest(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	const repository = "ghcr.io/0401lucky/gpt-load"
	const version = "2.0.0-dev.17.g111111111111"
	revision := strings.Repeat("1", 40)
	digest := "sha256:" + strings.Repeat("a", 64)
	previousDigest := "sha256:" + strings.Repeat("b", 64)
	source := repository + "@" + digest
	type imageRecord struct {
		Digest      string `json:"digest"`
		Revision    string `json:"revision"`
		ARMRevision string `json:"arm_revision"`
		Version     string `json:"version"`
	}
	for _, testCase := range []struct {
		name           string
		repository     string
		ref            string
		event          string
		remoteHead     string
		remoteFailure  bool
		inspectFailure bool
		badARMRevision bool
		badVersion     bool
		badChannel     bool
		wantError      bool
		wantWrite      bool
	}{
		{name: "current_digest_ignores_rebuilt_commit_tag", remoteHead: revision, wantWrite: true},
		{name: "manual_main", event: "workflow_dispatch", remoteHead: revision, wantWrite: true},
		{name: "stale_run", remoteHead: strings.Repeat("2", 40)},
		{name: "remote_failure", remoteFailure: true, wantError: true},
		{name: "malformed_remote_head", remoteHead: "null", wantError: true},
		{name: "inspection_failure", remoteHead: revision, inspectFailure: true, wantError: true},
		{name: "arm_revision_mismatch", remoteHead: revision, badARMRevision: true, wantError: true},
		{name: "version_mismatch", remoteHead: revision, badVersion: true, wantError: true},
		{name: "foreign_repository", repository: "other/gpt-load", remoteHead: revision, wantError: true},
		{name: "other_branch", ref: "refs/heads/topic", remoteHead: revision, wantError: true},
		{name: "pull_request", event: "pull_request", remoteHead: revision, wantError: true},
		{name: "incorrect_promoted_digest", remoteHead: revision, badChannel: true, wantError: true, wantWrite: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixtureDir := t.TempDir()
			fakeBin := filepath.Join(fixtureDir, "bin")
			if err := os.Mkdir(fakeBin, 0o700); err != nil {
				t.Fatalf("create fake bin: %v", err)
			}
			// 一个本地 registry/remote 替身，记录真实脚本的调用顺序和写入结果。
			fake := `#!/usr/bin/env python3
import json
import os
import sys

args = sys.argv[1:]
with open(os.environ["FAKE_EVENTS"], "a", encoding="utf-8") as events:
    if os.path.basename(sys.argv[0]) == "gh":
        expected = ["api", "--hostname", "github.com", "repos/0401lucky/gpt-load/git/ref/heads/main",
                    "--header", "Cache-Control: no-cache", "--jq", ".object.sha"]
        assert args == expected, args
        events.write("remote-main\n")
        if os.environ["FAKE_REMOTE_FAILURE"] == "true":
            raise SystemExit(1)
        print(os.environ["FAKE_REMOTE_HEAD"])
        raise SystemExit(0)
    events.write(" ".join(args) + "\n")

with open(os.environ["FAKE_REGISTRY"], encoding="utf-8") as source:
    state = json.load(source)
if args[:3] == ["buildx", "imagetools", "inspect"]:
    if os.environ["FAKE_INSPECT_FAILURE"] == "true":
        print("registry unavailable", file=sys.stderr)
        raise SystemExit(1)
    record = state[args[3]]
    if args[4:] == ["--format", "{{.Manifest.Digest}}"]:
        print(record["digest"])
    else:
        assert args[4:] == ["--format", "{{json .}}"], args
        print(json.dumps({"image": {
            platform: {"config": {"Labels": {
                "org.opencontainers.image.revision": image_revision,
                "org.opencontainers.image.version": record["version"],
            }}}
            for platform, image_revision in [
                ("linux/amd64", record["revision"]), ("linux/arm64", record["arm_revision"])
            ]
        }}))
elif args[:3] == ["buildx", "imagetools", "create"]:
    assert args[3:7] == ["--tag", "ghcr.io/0401lucky/gpt-load:latest",
                         "--tag", "ghcr.io/0401lucky/gpt-load:main"], args
    assert len(args) == 8, args
    record = dict(state[args[-1]])
    if os.environ["FAKE_BAD_CHANNEL"] == "true":
        record["digest"] = "sha256:" + "c" * 64
    for channel in (args[4], args[6]):
        state[channel] = record
    with open(os.environ["FAKE_REGISTRY"], "w", encoding="utf-8") as destination:
        json.dump(state, destination)
else:
    raise AssertionError(args)
`
			for _, name := range []string{"docker", "gh"} {
				if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(fake), 0o700); err != nil {
					t.Fatalf("write fake %s: %v", name, err)
				}
			}
			candidate := imageRecord{Digest: digest, Revision: revision, ARMRevision: revision, Version: version}
			if testCase.badARMRevision {
				candidate.ARMRevision = strings.Repeat("3", 40)
			}
			if testCase.badVersion {
				candidate.Version = "2.0.0-dev.16.g222222222222"
			}
			previous := imageRecord{Digest: previousDigest, Revision: strings.Repeat("2", 40)}
			state := map[string]imageRecord{
				source:                          candidate,
				repository + ":sha-" + revision: previous,
				repository + ":latest":          previous,
				repository + ":main":            previous,
			}
			statePath := filepath.Join(fixtureDir, "registry.json")
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatalf("marshal registry: %v", err)
			}
			if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
				t.Fatalf("write registry: %v", err)
			}
			eventsPath := filepath.Join(fixtureDir, "events")
			outputPath := filepath.Join(fixtureDir, "github-output")
			for _, path := range []string{eventsPath, outputPath} {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("create fixture output: %v", err)
				}
			}
			boundary := map[string]string{
				"GITHUB_REPOSITORY": "0401lucky/gpt-load",
				"GITHUB_REF":        "refs/heads/main",
				"GITHUB_EVENT_NAME": "push",
			}
			for key, override := range map[string]string{
				"GITHUB_REPOSITORY": testCase.repository,
				"GITHUB_REF":        testCase.ref,
				"GITHUB_EVENT_NAME": testCase.event,
			} {
				if override != "" {
					boundary[key] = override
				}
			}
			command := exec.Command("bash", ".github/scripts/main-promote-image-channels.sh")
			command.Dir = repositoryRoot
			command.Env = append(os.Environ(),
				"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_REGISTRY="+statePath, "FAKE_EVENTS="+eventsPath,
				"FAKE_REMOTE_HEAD="+testCase.remoteHead,
				"FAKE_REMOTE_FAILURE="+strconv.FormatBool(testCase.remoteFailure),
				"FAKE_INSPECT_FAILURE="+strconv.FormatBool(testCase.inspectFailure),
				"FAKE_BAD_CHANNEL="+strconv.FormatBool(testCase.badChannel),
				"GITHUB_SHA="+revision, "VERIFIED_DIGEST="+digest,
				"IMAGE_VERSION="+version, "GITHUB_OUTPUT="+outputPath,
			)
			for key, value := range boundary {
				command.Env = append(command.Env, key+"="+value)
			}
			output, runErr := command.CombinedOutput()
			if (runErr != nil) != testCase.wantError {
				t.Fatalf("promotion error = %v, want error %v\n%s", runErr, testCase.wantError, output)
			}
			encoded, err = os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read registry: %v", err)
			}
			if err := json.Unmarshal(encoded, &state); err != nil {
				t.Fatalf("decode registry: %v", err)
			}
			wantDigest := previousDigest
			if testCase.wantWrite {
				wantDigest = digest
				if testCase.badChannel {
					wantDigest = "sha256:" + strings.Repeat("c", 64)
				}
			}
			for _, channel := range []string{":latest", ":main"} {
				if got := state[repository+channel].Digest; got != wantDigest {
					t.Fatalf("channel %s digest = %q, want %q", channel, got, wantDigest)
				}
			}
			eventData, err := os.ReadFile(eventsPath)
			if err != nil {
				t.Fatalf("read events: %v", err)
			}
			events := strings.Split(strings.TrimSpace(string(eventData)), "\n")
			writeCount := 0
			for index, event := range events {
				if !strings.HasPrefix(event, "buildx imagetools create ") {
					continue
				}
				writeCount++
				if index == 0 || events[index-1] != "remote-main" || !strings.HasSuffix(event, " "+source) {
					t.Fatalf("promotion did not check remote main immediately before writing verified digest: %s", eventData)
				}
			}
			if (writeCount == 1) != testCase.wantWrite || writeCount > 1 {
				t.Fatalf("promotion writes = %d, want write %v", writeCount, testCase.wantWrite)
			}
			outputs, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("read promotion outputs: %v", err)
			}
			if testCase.wantError {
				if strings.Contains(string(outputs), "promoted=true") {
					t.Fatalf("failed promotion reported success: %s", outputs)
				}
			} else if want := "promoted=" + strconv.FormatBool(testCase.wantWrite) + "\n"; string(outputs) != want {
				t.Fatalf("promotion outputs = %q, want %q", outputs, want)
			}
		})
	}
}
