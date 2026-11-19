# N8-10 agents core-2

状态：进行中。分支 go-n8-10-core-2，基于已交回的 core-1 tip
7a3f1da4ebd606389c0142cf6f9134bc970fe0b5。core-1 报告见 n8-10-core.md；
其 HR 复核结论 verified、无 high；复核报告见 review-n8-10-core.md。
已 cherry-pick ec7c71865 / e871b2fe3 为 c2bd2fd6b / 3dd984086，保留原始
stored identity string 证明和 heartbeat version/config_version 原始 JSON 比较修复。

## 当前顺序

1. 先将 O9-H2 admission / proof matching 迁到 typed claim / model；旧 Record 仅在
   registration_legacy.go 转码。创建／消费同事务、hash-only、403、可选 proof 消费、
   existing Agent token 重注册均不得弱化。Profile existence 用缓存 typed View。
2. 注册／编辑／改绑／删除主体迁到 model.Agent transaction；保留部署、升级、DNS/HA
   busy guards 和 runtime overlay，不把 R5 零值当 identity proof。
3. deployments、remote/client、diagnostics/events/messages/version/time 的动态核心；
   ipv6、rulesync、metrics/traffic 残留 adapter / 私有 scalar helper。monitor/upgrades/
   installer/api-agents 的 S pass 仍是原计划后续步骤，只做必要接线。

## 验证约束

只在 .32/.64 经 rt 做定向 race、相关 Corpus|Reference|Golden / differential、新 fuzz 60s、
gofmt/vet；不跑完整门禁。本机只编辑／git／轻量阅读。不得改旧期望迁就实现，
R5/D2 新期望必须携带 exact raw 输入；设备请求比较真实 body/header 字节。

## 已实现的第一块

- registration.go 不再导入 jsonx。准入接受 typed registrationClaim，验证、profile
  existence、same-transaction one-use consumption 只有一个实现；旧 Record 入口在
  registration_legacy.go 只做 exact-key DTO 转码。profile existence 用 cached typed View，
  不复制／解码配置。
- O9-H2 发号逻辑不变：32-byte 随机值、持久化 SHA-256、24h、100 个服务身份及淘汰规则。
  消费仍修改外层注册事务的 system working copy，失败不会消费；hash 恒时比较。
  existing-token re-registration、JWT+有效 proof、existing token+有效 proof、匿名 403 及
  并发／重启／shutdown／disconnect／备份拒绝复活，原 O9 回归全部保留并通过。
- 不把 R5 string/zero 投影当凭据身份；original stored string token 类型单独检查，
  optional null/错类型不能成为 proof。protocol 数字 port 接受 8080 / 8080.0 / 8.08e3，
  不接受 JSON true 代替端口 1（旧 jsonx 的 Python bool/int 相等），新增真实 HTTP 403
  + proof 未消费 + numeric port 1 成功的独立回归。
- encoding/json 的 case-insensitive tag 匹配不能改协议字段名：先投影精确键名，
  Name/PORT/Profile_ID 等未知别名不覆盖 name/port/profile_id。新增 exact-input table 和
  fuzz：未知 case alias 不改变 claim，且 request 输入不变。

这只是 core-2 第一块；manager 主体、deployments/remote/diagnostics 等仍待迁移。
没有修改旧 corpus/baseline/期望；没有新依赖、没有 push、没有复核 agent。

## 定向证据（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/agents/registration.go internal/agents/registration_legacy.go internal/agents/registration_native_test.go && GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents && GOMAXPROCS=4 go test -race -p 4 -count=1 -run "RegistrationClaim|RegistrationSessionR5" ./internal/agents && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "^TestO9" ./internal/api/agents'

Exit 0：agents 1.022s；API O9 77.821s（此前两次 O9 分别 73.284s / 88.967s）。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/registration.go internal/agents/registration_fuzz_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzRegistrationClaimExactFields$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0，gofmt/vet 静默；fuzz 320,785 executions，PASS 61.012s。

## core-1 HR 接纳及 L4

- M1/L1 修复完整接纳，没有改原复核期望。后续所有权字段必须读取 original JSON
  token，不能用 R5 text projection 作为证明。
- L4：HA guard allocation bound 同时断言 B/op 和 allocs/op；0 与 2MiB
  captured_source 的阈值均不随 payload 增长。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/registry_native_test.go; GOMAXPROCS=4 go test -race -p 4 -count=1 -run "Prepared.*Stored|Heartbeat.*Stored|Malformed.*Disable|TypedHAEnablePresenceAndAllocationBound|N810" ./internal/agents ./internal/agents/dnsha'

Exit 0：agents 18.756s，dnsha 2.511s。
