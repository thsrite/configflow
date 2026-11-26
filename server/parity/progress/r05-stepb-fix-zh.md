# R05 Step B 修复交接

分支 go-fix-r05-stepb，从 batch-4@3b8d2c03b 建立。证据：go-difftest-routers-b@d5019c4f9 的
parity/difftest-routers-findings.md，仅处理其 R05 medium 与两个 low。

当前实现：static_rt 掩码显示后缀统一由 ikuaiRouteMask 处理，网络解析/添加确认/按实际 ID 补偿共用；
不修改默认 fake，测试包装保持 Step B 的 `(synthetic display)` 后缀，plain 只作显式成功对照。
名称/其他配置证明不放宽，外改 gateway 不删除。异常 verinfo 和损坏 Location 返回
RouterError{Kind:KindInvalidResponse}，中文消息集中 messages.go，不泄漏响应内容。
R15 尚未移植；它必须捕获这类错误、持有自己的 owner 锁保存 last_probe，再返回失败 HTTP。
测试用实际 repo SaveRouterSettings 证明消费契约，不冒称已实现 HTTP 探测 owner。

修复前 .32 定向 TestR05StepB 失败：suffix 与 suffix/batch 发布读不回；lost_add_reply 和 later_refusal
补偿拒绝、各残留 1 行；verinfo scalar/array 误报未知版本，invalid Location 缺少新的明确错误类别/诊断。
Go 旧实现本来已不会像 Python 那样崩溃，low 的修复是明确诊断类型与 R15 契约。
修复后同组通过 0.024s；加真实 Python 后缀残留对照、现有 corpus 差异精确断言及 repo 消费契约后，
定向通过 1.437s。后续 .32 gofmt/vet、R05 与 R03–R12 可用共用路径 race、TestR03Corpus 全绿，见下。
没有 gate、push、真实设备或子 agent。L5 新项已写入 PROGRESS R05。

## 验收对应与 R15 交接

- `TestR05StepBStaticMaskPublishAndUndo`：默认后缀和 plain 两种呈现，单项/批次发布成功；
  ReadState 包含该路由，随后 undo 删除实际 ID、无残留。
- `TestR05StepBStaticMaskCompensation`：新增回复丢失、后续第二条新增被拒时，补偿能清理已添加路由；
  设备上的 gateway 被外改则拒绝删除，仍保留现场。匹配只忽略 static_rt.netmask 的显示后缀，
  对 NAT/端口分流及其他字段没有放宽；不按 CIDR 猜测归属。
- `TestR05StepBPythonBugEvidence`：真实 Python 在同一 loopback 后缀 fake 上发布失败、补偿失败、残留 1 行；
  Go 发布成功并完成同一个 always_rollback 请求、最终 0 行。
- `TestR05StepBProbeInvalidResponses`：非空标量/数组 verinfo 及损坏 IPv6 Location，均为
  `*RouterError{Kind: KindInvalidResponse}`，中文消息固定、不暴露凭据、不写设备。
  每项接实际 SaveRouterSettings：只替换本 router.last_probe，保留未知字段与另一个 router，revision 恰好 +1。
- 现有 `TestIkuaiBoundaryCorpus/read_state_details` 不再把漏读后缀路由当正确结果：精确断言新增识别 ID=3、
  active:3 的目的网段，然后比较所有其他报告、请求与表。损坏端口/IPv6 Location 的诊断差异明确登记。
- `TestR05StepBRawStaticRequests` 检查真实 HTTP add/del 请求原始 body、路径、POST 和 Content-Type，
  明确 wire 上发送 plain mask，补偿发送真实 row ID。

R15 接线要求：探测入口按自己的 L1 owner 契约拿锁、Detach；捕获 errors.As(*RouterError)，包括新 Kind，
写 `last_probe={at:<本次请求时间>,ok:false,message:<安全中文>}`，照常保存一次 revision。
先保存，再映射该设备错误为结构化失败（Python 正常失败为 502）；不能因 KindInvalidResponse 落入通用 500，
不能沿用旧的成功探测。HTTP 状态和路由实现仍由 R15 决定，本分支没有新增 HTTP 或替代保存 owner。

batch-4 当前包含 R03–R11 驱动和 `TestR12TypedNATContract`；尚无 R12 Inspect/Execute/Publish 产品。
定向 regex 含 TestR12，会验证当前可用契约；不能把这次证据称为尚未集成的完整 R12 服务验收。

## 命令

均固定 `RT_HOST=sunbroke@192.168.0.32`，前台跑。

```sh
# 未改产品时：FAIL（0.019s），含上文每类问题；plain 对照与外改保护通过。
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/r05_stepb_test.go -- bash -lc 'cd server && gofmt -w internal/routers/r05_stepb_test.go && go test -count=1 ./internal/routers -run TestR05StepB -timeout 3m'

# 修复后 + 真实 Python / repo 消费契约：ok configflow/internal/routers 1.437s
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/r05_stepb_test.go --back server/internal/routers/ikuai_cases_test.go -- bash -lc 'cd server && gofmt -w internal/routers/r05_stepb_test.go internal/routers/ikuai_cases_test.go && go test -count=1 ./internal/routers -run "TestR05StepB|TestIkuaiBoundaryCorpus/(read_state_details|redirect_location_10|fixed_unparseable_redirect_explained)$" -timeout 3m'

# 最终共用路径检查：gofmt clean、go vet ./... 通过；
# ok configflow/internal/routers 414.294s
# ok configflow/internal/devhttp 3.445s
# ok configflow/internal/routers 4.974s
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/ikuai_apply.go -- bash -lc 'cd server && gofmt -w internal/routers/ikuai_apply.go && test -z "$(gofmt -l internal/routers/ikuai*.go internal/routers/model.go internal/routers/messages.go internal/routers/r05_stepb_test.go)" && go vet ./... && go test -race -count=1 ./internal/routers -run "TestR0[4-9]|TestR1[012]|TestIkuai|TestRouterOS|TestRoute|TestGroup|TestLayout|TestReport|TestNAT|TestAddress" -timeout 25m && go test -race -count=1 ./internal/devhttp -timeout 3m && go test -count=1 ./internal/routers -run "^TestR03Corpus$" -timeout 3m'
```

最终新增原始请求用例及 capture seam 后，单独重验所改 R05 组（未重复整套 race）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/r05_stepb_test.go -- bash -lc 'cd server && gofmt -w internal/routers/r05_stepb_test.go && test -z "$(gofmt -l internal/routers/ikuai*.go internal/routers/model.go internal/routers/messages.go internal/routers/r05_stepb_test.go)" && go vet ./internal/routers && go test -race -count=1 ./internal/routers -run "^TestR05StepB" -timeout 3m'
# ok configflow/internal/routers 2.194s
```

没有未修复的 R05 Step B finding；HTTP 的状态码及真实 handler 保存链仍归 R15，真实固件归 L5。
原始黑盒清单未修改；没有把 Go 尚未注册的 HTTP 路由计作已通过。
交回提交即本文件所在提交；基线保留 batch-4，未擅自变基到其他未集成模块。
