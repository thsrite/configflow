# batch-6 两条 difftest 的集成收尾

分支 `go-fix-batch6-logs`，接续已交回的 a16a9fc99；修复提交 `259efd4da`。本轮只改测试，不改产品逻辑、基线、比较规则或生产参数。完整 GATE_ONLY=compare 固定 .32，core 由协调者在 .64 跑。

## 通知：记录字段顺序与显示条目顺序分开验证

来源是 batch-6 的 `92249a92c`（N8-7 native incident store）：`notifications/store.go` 的 upsert 把 observation/已有记录投影为原生 `map[string]json.RawMessage`，再 `json.Marshal(patch/fields)`，记录内部的键按 Go 原生方式排序。旧版 jsonx.Clone/Update 保留 Python 风格的字段插入顺序。后续 `21ed8dddd` 的 R5 修复没有引入这一转换。

这是 JSON 对象字段的格式差异，不是通知条目排序变化。前端 `NotificationCenter.vue` 按 API 的 items 数组展示，并按字段名读 title/message/status_label；没有依赖记录内部的对象成员顺序。原生 `notifications.Items` 仍使用保序 model.Map；SnapshotItems 按时间/严重程度稳定排序，同时间的插入顺序仍有意义。PORTING 的功能等价与既有 Go 原生 JSON 格式政策允许字段布局不同，不应把 Python 的字段插入顺序移植回产品。

测试修复：保留原来的时间/fingerprint/evidence 掩码；之后用 encoding/json + UseNumber 比较完整结构，同时**单独保留 items 的插入键序列**。这样没有忽略显示顺序，也没有忽略数组顺序、字段缺失、消息、类型或大整数差异。新增正负控证明只有记录字段重排能相等；条目重排、数组重排、真实消息变化、字段丢失、整数精度和数字/字符串变化都会报差异。

定向 race 暴露并修掉原测试的 30s 墙钟依赖：真实 scheduled-refresh 驱动在首轮后会刷新从默认配置带进来的无关来源，增加 task:subscription 的时间证据与规则缓存文件。A15 测的是升级/地址操作，不是 C15 自动下载。两边相同的 seed 现在明确用现有配置 interval=0/update_interval=0 关闭自动来源更新；没有改计时器、运行模式、注册 provider、设备响应或显式升级/地址 worker。最终 race 持续 43.390s（超过首轮时间）仍完整比较 130 个请求、Agent 请求、Agent 存储、通知和文件并通过；没有扩大掩码吞掉任务证据/文件差异。

## setup：A21 已接线，prepare 应比较真实 Python 的 400

来源是 `ded312332`（A21 DNS/setup ownership wiring）：server.wireSetupOnboarding 注册真实 DNSOwner 及 C36 AdoptOwnedWrite。旧 difftest 把 prepare 与 abandon 一起视作“A22 未移植，必须 503”，这个前提已经过时。

实际调用顺序：C36 prepare 校验表单、确认 A21 DNS owner、检查 local DNS/选项，再读来源。测试选中的订阅没有缓存节点，因此 resourceNodes 返回 400「当前没有可用节点，请先读取订阅或选择支持的节点」。Python setup_flow.prepare 的同一顺序也在 available_sources 阶段返回相同 400。这里还没有节点可供 converter 做 R5 校验，A16/A17 也没有介入；不是 R5 抢在 readiness 之前拒绝请求。

测试改为真实 Flask↔Go 比较 prepare 的 400、完整 body/headers/DB/文件，另断言失败没有任何状态改动。abandon 仍需要 A22 HasInstallation，继续严格断言缺 owner 的 503 与零写入。

A21 gateway-onboarding 的 readiness 是另一个条件：缺 adopter 且存在未完成 session 时阻止保存；没有 session、已完成、已放弃都不应被全局阻止；reader 损坏或类型错误按可能未完成 fail-closed。既有 TestSetupReadinessContract 全部保留，并加一例未完成 session + 非法表单，证明 503 在表单校验之前且无 DB/文件/revision 写入。没有放宽 503 或 reader。

## .32 定向检查

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 --back server/internal/api/agents/difftest/upgrades_diff_test.go --back server/internal/api/agents/difftest/notification_comparison_test.go --back server/internal/api/config/difftest/setup_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/agents/difftest/upgrades_diff_test.go internal/api/agents/difftest/notification_comparison_test.go internal/api/config/difftest/setup_test.go; go vet ./internal/api/agents/difftest ./internal/api/config/difftest; go test -race -count=1 -timeout=15m -run "^(TestNotificationStoreComparisonPreservesDisplayAndData|TestUpgradesDifferentialAgainstFlask|TestSetupFlaskHTTPStateAndAuthentication)$" ./internal/api/agents/difftest ./internal/api/config/difftest; go test -race -count=1 -timeout=10m -run "^(TestSetupReadinessContract|TestPrepareFieldsMatchRealPythonAndRetainOwnership)$" ./internal/gateway/onboarding ./internal/setup'
```

```text
ok configflow/internal/api/agents/difftest 43.390s
ok configflow/internal/api/config/difftest 5.903s
ok configflow/internal/gateway/onboarding 4.440s
ok configflow/internal/setup 9.171s
```

新增 readiness 表单优先级例子后，再跑该测试 race：`ok configflow/internal/gateway/onboarding 4.610s`。

中间失败已定位：第一次字段布局修复后，live race 超过 30s，引入上述独立 scheduler 来源状态/文件污染；两次捕获都不是消息/条目顺序问题。没有在污染结果上增加忽略项，而是修正 seed 的自动更新配置，然后定向 race 通过。

## 完整 compare

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e GATE_ONLY=compare -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/batch6-logs-followup-complete --back server/internal/gateway/onboarding/service_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/gateway/onboarding/service_test.go; test -z "$(gofmt -l internal/api/agents/difftest/notification_comparison_test.go internal/api/agents/difftest/upgrades_diff_test.go internal/api/config/difftest/setup_test.go internal/gateway/onboarding/service_test.go)"; go test -race -count=1 -timeout=5m -run "^TestSetupReadinessContract$" ./internal/gateway/onboarding; cd ..; cat /proc/loadavg; server/scripts/gate.sh -p 4'
```

完整阶段 **PASS，退出码 0**。宿主机起始 load `0.65 0.63 2.29`，最后 `1.96 2.09 2.49`；proddiff 内部记录 start `0.67 0.63 2.28` / end `3.24 1.71 2.46`。

```text
baseline: new=0 stale=0
transport_retries: python=0 go=0
self_check=PASS coverage_regressions=0 unmapped_routes=0
ok configflow/internal/api/agents/difftest 311.957s
ok configflow/internal/api/config/difftest 65.219s
# 全部 21 个 difftest 包均 ok
SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips; repo Python subtest and WebDAV Python exec verified
GATE STAGE PASS: compare
```

Synthetic live 实际执行 `go test -json -race -count=1`，真实 Python、snapshot unset，manifest 核验 23 个必跑条目全部执行，未跳过。没有只报 proddiff 通过；后续 difftest 和 synthetic live 都完整跑完。

本轮关闭两条由 batch-6 引入的测试集成问题，并修正 A15 夹具的独立 scheduler 污染；产品输出的 JSON 对象字段格式仍遵循既定原生 Go 政策，用户可见的通知顺序和 setup/503 逻辑未改变。没有新增功能差异、Python bug 修复或延期项。分支不推送，工作区在文档提交后保持干净。

