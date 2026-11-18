# R13 中文交接

状态：ported，待新的 Opus HR 复核。分支 `go-port-r13`，从 `go-port-r12@574c3e896` 建立，
已叠入 R12 复核分支的产品修复 `92b5c81c1`；交回前同步复核报告及白名单补丁修正 `7fa73f99d`。
R10–R12、O3-6b/6c、C07 batch-1 尚未正式进入当前基线，正式集成后再 rebase 到 go。

## 实现及对外入口

按 D-R12-1，inspection/advisory 已归 R12；R13 仅移植 `pending.py`，复用 Inspect/BuildPlan，
不另造检查服务。产品入口 `AgentRouters(ctx, PendingRequest)` 与 `Summarize(report, plan)`，
返回 typed PendingResponse/PendingSummary。`SummarizeJSON` 是既有 C07 回调的 typed JSON 边界，
异常数据不伪装 synced。`PendingRequest` 支持显式 profile、AgentSource、Driver factory、独立 PendingCache。
生产默认 cache 使用 lifecycle.DefaultSpawner 与 clock.Real；测试注入真实 Runtime 和 fake clock，预算不缩短。

- 目标保持 profile/router 顺序；显式 profile 空值或缺失分别拒绝，不回退；克隆 ID 按 profile 隔离。
- cache 签名包含 scope、关联 profile revision/name、完整 router 记录和 Python 规定的 Agent 字段。
  只存内部 SHA-256 digest 与安全的结果 DTO；无 TTL、64 Agent LRU、unknown 不复用。
  refresh 与并发查询合并，旧 generation、Clear、eviction 的迟到结果不能覆盖新条目；返回值深拷贝。
- 每次读取共享 25 秒、最多 8 worker；按目标顺序返回，单项错误不隐藏其他结果；等待共享 owner 最多 35 秒，
  ErrPendingTimeout 由 R15 映射为既有 504。请求断开只停止等待，owner 继续读取和通知；Runtime 关机取消设备读取，
  禁止新的 cache/通知提交。入口取目标快照也 Detach，已断开的请求仍完成共享 owner。
- pending 操作数、阶段顺序、阻塞/跳过与中文短状态直接来自同一个 R12 plan。
  成功读取后以明确 profile 调 C07 RecordRouter；超时和错误不创建成功通知。设备路径只读，实际 fake 调用全部为 GET。
- Wire 注册 Sources.Summarize，同时保留原有 repo redaction hook。未新增依赖、Keyed、锁文件或 YAML 路径。

## 边界与后续 owner

C07 当前接口仍为 legacy jsonx；仅在既有 `routers/wire.go` 转换，pending 业务使用 native DTO。
前协调者明确 TransactionContext、MarkReadContext 和 RecordRouter 的 leaf 取消修复已在
`batch-1`/`go-review-c07`，不得重复实现；本分支没有新增 RecordRouterJSON 或私有 transactionContext。
当前 Runtime 用例证明进入通知前拒绝关机，不冒称已验证 C07 内部等锁/ Rename 窗口。
正式集成 batch-1 后须补一次共享树检查。N8-7 第二批（097eba4e）迁 notifications 时统一移除 legacy 适配；
现有 typed summary/原生 report-plan 已足以承接，不需要先开新的并行写入 API。

A23 未移植：R12 IPv6HostRequest 缺省 fail closed，R13 仅展示实际 report，不造替代状态或 readiness。
R14 拥有 task slot/自动同步，不填空 task rows 伪装 ready；R15 拥有 upstream-routers HTTP、鉴权、no-store、504。
R12 的 canonical AST 批处理白名单仍随其待集成 patch，由 O3-6c 正式集成时应用；本模块没有新增例外。

## 验证（仅 rt，loopback fake / 合成数据）

按 Leaner verification，真实 Python 对照 8 组摘要正常路径及错误类别代表，不扩组合，不做变异或完整 gate。
`test_upstream_router_pending.py` 的摘要由 TestR13SummariesPython，service/scope/cache/通知由
TestR13AgentRoutersScopeCacheAndNotifications，错误隔离和 25 秒/8 worker 由对应 service 用例，
缓存合并/重试/容量/深拷贝/代际由 TestR13Cache*，请求断开与关机由真实 Runtime 用例覆盖。
`test_ros_return_advisory.py` 与 IPv6 integration 的 inspection 部分已归 R12；HTTP 鉴权留 R15，sync 留 R14。

最后两条实跑命令均在 .32，前台完成：

```sh
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers/pending*.go internal/routers/r13_*test.go internal/routers/wire.go)" && go vet ./... && go test -race -count=1 ./internal/routers -run "TestR13|TestR12Review" -timeout 5m && go test -race -count=1 ./internal/notifications -run "TestRouterNotices|TestSameRouterID|TestRouterRead|TestObservationCollection" -timeout 3m'
# ok configflow/internal/routers 7.451s
# ok configflow/internal/notifications 4.894s

rt --back server/internal/routers/pending.go --back server/internal/routers/r13_service_test.go -- bash -lc 'cd server && gofmt -w internal/routers/pending.go internal/routers/r13_service_test.go && test -z "$(gofmt -l internal/routers/pending*.go internal/routers/r13_*test.go internal/routers/wire.go)" && go vet ./... && go test -race -count=1 ./internal/routers -run "TestR13|TestR12Review" -timeout 5m'
# ok configflow/internal/routers 7.272s
```

第二条覆盖最后增加的「请求进入前已经断开」回归与入口快照 Detach。无 gofmt 输出，vet 通过。
另 .32 非 race TestR13 0.993s；初次编译和摘要/cache 定向通过。一次早期 .64 race 的结果因工具输出丢失未计入验收。
之后叠入 `7fa73f99d` 只改 R12 文档及待集成 patch，不改产品/测试，未为纯文档重复跑测试。

## 差异及风险

无新增 high/medium 待修发现。Python 内部异常类名改为清楚的 `读取失败：内部错误`；错误凭据走 diag 脱敏，
panic 不输出 payload；原生 canonical JSON 只影响临时 cache identity。细项记入 behaviour-changes.md。
R12 T27 的显式 profile 修复被完整复用；本模块不新增设备写入/补偿或归属推断。

复核重点：共享 Flight 生命周期、超时后迟到结果、cache generation、profile scope、Wire/C07 通知调用；
正式合并树应重点复验 C07 取消叶边界和 O3-6b Wire hook 合并。未 push、未开子 agent、未访问真实设备。
