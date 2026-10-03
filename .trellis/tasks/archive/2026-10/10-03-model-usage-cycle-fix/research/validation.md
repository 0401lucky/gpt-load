# 验证记录

## 实现阶段

六个相关包的综合定向测试通过（实现代理日志 `/tmp/model-usage-focused.log`）。覆盖普通限流不重置周期、可信重置与重试缓冲分离、旧检查点回退、missing/partial 的小时桶和边界归并、Cline 包装非流式缺失/null/无效 usage。

非流式标准响应和 Responses/Anthropic/Gemini 转换原有测试通过，未复现解析缺陷，因此没有更改采集生产代码。生产 missing 的准确原因尚需额外上游响应证据。

## Windows TCP 测试基线

本次工作树的 gateway 全包出现 `TestBufferedWriteDeadlineStopsRealTCPSlowReader` 失败。为核实归因，创建独立 detached worktree `tmp/model-usage-head-baseline`，版本为未经修改的 HEAD `080a1531`，执行：

```text
go test ./internal/gateway -run '^TestBufferedWriteDeadlineStopsRealTCPSlowReader$' -count=1
--- FAIL: TestBufferedWriteDeadlineStopsRealTCPSlowReader (0.03s)
    stream_integration_test.go:780: writeUpstreamResponse() error = nil
FAIL gpt-load/internal/gateway 0.330s
```

基线同样失败，证明不是本次变更引入。该测试未被跳过或修改，完整门禁不能宣称全绿。

## 环境

Windows 无 make，使用 Makefile 等价检查。预存 ignored `tmp/baseline-59d75050` 的大量 CRLF 源码污染全目录 gofmt，保留原目录，另行验证受版本管理源码格式。全局 pnpm 与项目 engines 版本不一致，复核用项目指定 corepack pnpm 执行等价脚本。

## 独立复核结果

- ESLint、样式、类型、Prettier、tracked Go gofmt、go mod tidy -diff、go vet ./...、Vite 与 Go build、git diff --check 通过。
- health/state/control/requestlog/bifrost 相关测试通过。复核补充非未来重置被拒绝且不修改状态的测试，通过；纠正检查点回滚注释。
- 全量 `go test -count=1 . ./internal/...` 未全绿。日志 `D:/tmp/model-usage-check-full.log`。
- 除上述已证实的基线 TCP 失败，还有 catalog/authkey 的 Windows 路径转义断言、container 环境代理断言、webui 的 WSL 路径/Python3 Store 占位程序/符号链接权限相关失败。这些额外失败未独立复现基线，不宣称必属既存，也未扩修无关范围。
- 两套 UI 分组/非分组共用行模板，都具有缺失计数提示和全部缺失时的未知显示；保留已知 token 合计。

本地修复可审阅；未提交、未部署。独立基线 worktree 已删除，预存目录未动。
