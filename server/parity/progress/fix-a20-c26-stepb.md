# routers Step B：A20 / C26 修复

- 分支：`go-fix-a20-c26-stepb`，基底 `batch-4@3b8d2c03b`。
- 输入证据：`go-difftest-routers-b@d5019c4f9:server/parity/difftest-routers-findings.md`。
- 仅修改 Go；Python、比较器、R15 HTTP 状态映射和 R17 路由注册均未修改。
- 测试固定 `RT_HOST=sunbroke@192.168.0.32`；无完整门禁、生产操作或 push。

## medium：A20 单个坏报告只影响自己的网关

Python `sanitize_forward_filter` 对 `state`、`sources[*].kind`、`remedies[*].source`
直接做 frozenset membership，数组/对象会抛出 TypeError，打断整个网关列表。
基底 Go 的 sanitizer 已避免 panic，但会丢弃异常证据，把坏网关继续显示成 `persisted`。

本次在共享 `host.Bridge.Forwarding` owner 中、清洗前校验报告标签的原生 JSON 类型。
坏项单独返回 `error`、固定中文格式错误说明和 `can_persist:false`；不回显报告，健康项保持正常。
缺少报告或 null 仍兼容旧 Agent，正常报告仍由原白名单清洗。

`TestStepBA20MalformedReportOnlyAffectsItsGateway` 通过授权 HTTP 和两个 Agent，覆盖三类字段的
数组/对象，断言两个 ID 均保留、坏项错误、健康项 persisted、零设备写入、异常对象内合成 secret 不泄漏。
修改前六项均失败，典型输出为 `malformed report not isolated as error`（坏项实际 persisted）。

提交：`e4e190e96`。

## medium：A20 HEAD 只读（基底已有修复，本次补强回归）

实际 Go 修复已在基底的 `56587a0b8`，没有重复改产品分派。
原修复前失败证据见 `review-a20.md` M1：
`HEAD wrote to the Agent: [POST /api/linux-gateway/forwarding]`。
真实 Flask 对照 `TestA20PythonHEADForwardingWriteIsFixed` 继续断言旧 Python 发出 forwarding POST，
而 Go 只发 GET，完整存储快照不变。

本次增强 `TestReviewA20HeadGatewayForwardingIsReadOnly`：空、合法、畸形和非对象 body 均按 GET
返回相同状态和全部 headers、无 body；SQLite 指纹（含 generation/revision）、显式 profile revision、
普通文件内容/模式/mtime 不变；BeforeCommit 拒绝钩子断言没有进入保存提交；POST 正控仍实际发出一次设备写入。
文件夹中放置真实合成 YAML 哨兵，避免文件检查空跑。

提交：`e4e190e96`。

## low：C26 URI 协议不再被当成插件引用

原扫描器把 `udp://…` 中的 `udp` 当成同名插件引用；如果主监听 tag=udp，独立旁路链被错误地
判成可达 sequence_main。ReadPolicies 不可编辑，ApplyPolicies 拒绝合法修改。

共享引用扫描使用 `net/url` 识别紧接 `://` 的有效 scheme，忽略该协议 token，继续扫描字符串后面的
真实引用；`$udp`、`jump udp`、同一字符串 URI 后面的真实引用仍受保护，不全局忽略名为 udp 的插件。

`TestStepBC26ProtocolSchemeIsNotReference` 校验 scheme 和真实引用对照；
`TestStepBC26IndependentSidePolicyLifecycle` 通过 PrepareEditor/ReadPolicies/ApplyPolicies 运行添加直连、
改为智能、撤销的全流程，断言 side_entries、旁路插件、上游、注释和输入草稿均保留。
负控加入真实 `$udp` 边后，读取和保存仍拒绝。修改前 scheme 三项误报引用，完整流程以
`处理链 side_forward 会间接调用主处理链 sequence_main` 失败；修改后通过。

提交：`c36bce347`。

## 定向检查

修复前（预期 FAIL；HEAD 已有修复的增强回归通过）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -w internal/api/agents/review_a20_test.go internal/oxidns/review_stepb_test.go && go test -count=1 -run "TestStepB|TestReviewA20Head" ./internal/api/agents ./internal/oxidns'
```

修复后（全部 PASS）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/agents/review_a20_test.go -- bash -lc 'cd server && gofmt -w internal/api/agents/review_a20_test.go && go test -race -count=1 ./internal/gateway/host ./internal/api/agents && go test -race -count=1 -run "TestStepB|TestC22C26ReviewReferences|TestReviewPublicEditorsPreserveYAML|TestReviewMergeEffectiveFields" ./internal/oxidns && CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON=$HOME/cf-test/.venv/bin/python go test -race -count=1 -run TestA20PythonHEADForwardingWriteIsFixed ./internal/api/linuxgateway/difftest && go vet ./...'
```

末行摘要：

```text
ok configflow/internal/gateway/host 8.624s
ok configflow/internal/api/agents 356.054s
ok configflow/internal/oxidns 1.515s
ok configflow/internal/api/linuxgateway/difftest 2.855s
go vet ./...: exit 0，无输出
```

HEAD 增强回归另做一次历史负控：远端临时把唯一分派条件恢复成 `56587a0b8^` 的
`if c.Request.Method == http.MethodGet {`，只跑：

```sh
go test -count=1 -run '^TestReviewA20HeadGatewayForwardingIsReadOnly$' ./internal/api/agents
```

通过 `RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc ...` 执行，shell EXIT trap 无论结果如何
恢复原文件。预期 FAIL，实测 `review_a20_test.go:112: status 400, want 200; body:`，用时 0.184s。
这是已有修复的单项历史负控，不是本次新改产品分派；原版发送设备 POST 的证据还由真实 Flask 对照独立断言。

随后同步最终代码并再跑新增回归，确认历史条件没有遗留：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/gateway/host/filter.go internal/gateway/host/forwarding.go internal/api/agents/review_a20_test.go internal/oxidns/references.go internal/oxidns/review_stepb_test.go)" && go test -race -count=1 -run "TestStepB|TestReviewA20Head" ./internal/api/agents ./internal/oxidns'
```

末行：`ok configflow/internal/api/agents 5.288s`、`ok configflow/internal/oxidns 1.147s`。
`gofmt -l` 无输出。未运行完整门禁；集成门禁由协调者执行。

已完成，未解决项：无。本次批准的 Python bug 差异已登记到 `behaviour-changes.md`；
R15/R17 未移植的 HTTP/注册工作仍归原任务，本分支未扩展范围。
