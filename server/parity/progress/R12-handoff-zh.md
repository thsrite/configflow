# R12 中文交接

R12 已移植，状态 `ported`，等待新的 Opus HR 复核。分支 `go-port-r12`，从
`go-opus-review-r11` 建立；实现首提交 `c145c31d8`，后续修正和验收见本分支 HEAD。
工作目录仍是 `go-port-r10`。未 push、未开子 agent；所有设备和 Agent 用 loopback fake，
fixture 只用合成数据；未访问生产、真实路由器、DNS、PVE。所有测试/构建/格式化/vet 均经 rt。

## 已实现

- `Inspect`：显式 profile、同一次 driver/read-proof scope；R08 policy、R09 guard/forward hint、
  R10 Fake-IP/rule-set、R11 IPv6 DNS/PD 接线；DNS HA；一个 IPv6 host 请求供两功能共用；
  ReturnStatus/跨所有 profile 的 return cleanup。
- `BuildPlan`：typed ops/checks；四个基础阶段及六个条件阶段；iKuai IP groups/端口分流迁移和
  退回静态路由；ROS policy 撤回前 prepare_static/NAT 保护；NAT list/removals-first；DHCP、
  RA DNS、guard、PD、return cleanup 的顺序、跳过和回读要求。构建不改变 inspection 的 policy op。
- 指纹按设计用 canonical native JSON，递归排序、不转 float、不含密码；同进程/跨进程稳定，
  evidence 变化会失效。警告和 advisory 不进入指纹；不追 Python JSON 格式的旧摘要。
- `Execute`/`Verify`：分阶段 pre-check、回读、无写入阶段仍验证、RouterOS 首个 DHCP op 的临时
  proof、batch 逐条完成记录、错误携带 undo 的部分成功、逆序恢复及逐条回滚结果。
- Return allocation 严格握手、Agent 选择 table/priority/单 bit mark、协议 1/2、mark/mask 校验；
  A01 manager adapter 保留原始 previous spec，传输失败保留补偿，补偿回复丢失后 GET 确认。
  GET 必须证明删除或完整 spec（含未知字段），不能把读失败/外部改动当成功。
- `Publish`/`StartPublish`：新鲜配置重建+全记录比较+指纹门禁；一次 driver；L2；失败和五类 manifest
  同一次 publication；三态 untouched/replace/clear；last_release 脱敏；capture/通知；tracked Spawn，
  驱动/L2/L1 都释放后才交付结果，拒收也释放 L1。
- repo 仅加 `JSONUpdate`/`RouterPublicationJSON`/`SaveRouterPublicationJSON` native 边界，内部仍交给
  原 `SaveRouterPublication` 的连接 CAS、revision 和单事务；不另建存储路径。adapter 本身 Detach
  后检查取消。Router 保留原记录快照用于 CAS，缺省 verify_tls 等运行时默认不会变成虚构的存储编辑。

无新增 HTTP 路由。R13 pending、R14 rule sync、R15 HTTP handler、A23 host wiring 仍由各自模块负责。
`IPv6HostRequest` 未接线时 fail closed；`CaptureManual` 未接线返回 ErrNotPorted，按设计记录警告，
不伪造自动同步 baseline。R15 必须在 404/校验前拿 L1，再把 held ctx/unlock 交给 StartPublish；
Publish 只拿 L2，driver/return agent 不重入 L1。已通过 Paseo 与 6e5c7760 对齐，不新增 Keyed。

## D-R12-3 与待集成 AST patch（重要）

协调者已明确保留 D-R12-3。Execute 在 Runtime 取消边界返回 Aborted、不再补偿，Publish 保留 Result。
只有 `Result.Aborted && Result.Wrote`，才进入
`routers:saveAbortedPublication:ctxx.Uninterruptible`，仅一次本地 publication：失败记录、五类显式
manifest 更新、必要 revision。设备 I/O、补偿、后续设备读均在区域外并响应取消；写设备前取消零提交。
普通事务在 repo 的取消门禁被拒、尚未 commit 时，才允许进入这一次本地记录事务。
两份设计都已写 `Clarified in R12`。

**当前树没有 O3-6c 的三个 beginCommitBatch，也没有 canonical batch_paths_test.go。** 按协调者最新
裁定，扩展以 [r12-batch-allowlist.patch](r12-batch-allowlist.patch) 随 R12 提交，针对
`go-o3-6c@426f6edd0` 的 canonical `server/internal/repo/batch_paths_test.go`：

- 同一个测试扫描 repo/routers 两包、两种 callee，四条 package:FuncDecl:callee 各恰好一次。
- ctxx 导入别名解析；routers 的 Uninterruptible SelectorExpr（含取函数值）全部纳入检查。
- 未登记位置和方法接收者不会冒充获批函数；每条必须出现在设计。

只在 .64 临时目录对该稳定源文本验证了 patch 可应用和 gofmt；**没有在当前树运行四条 AST 白名单，
不能声称它已通过。** O3-6c/R10/R11 正式合入后，rebase 到 go、把 patch 应用进同一个 canonical 测试、
跑绿；若本 agent 已交回，协调者在批次树执行。当前 local go 仍没有 R10/R11，故未提前变基或复制公共产品。

O14 后续单列：R04–R11 F6——若关机打断的是下层 owner、它尚未把已落地对象登记进自己的 manifest，
应按同一模式统一补齐。R12 保存所有现有可信证明，不猜新 ID、不把未确认对象收养为本方资源。

## 验收与命令

按 `go@5264ba80e` Leaner verification 收口：保留已有 22 组完整 Python 对照；没有变异门禁，
没有 gate/proddiff/全仓 race。其后仅保留已经写完的正常和各阶段失败代表，以及本地提交/取消边界。

| Python 规格 | Go 边界验收 |
|---|---|
| test_router_release.py | TestR12PythonWholePlanAndExecution：两 vendor 22 组完整 plan、执行 log/rollback、Agent calls、写序列、最终表 |
| test_routeros_release_safety.py | 同上的 conflict/拒绝/后阶段失败；TestR12RouterOSRawStageRequests；共享 R04–R11 定向回归，继续使用原 ID/外改/丢 ack 保护 |
| test_router_return_routing.py | 同上协议 1/2、off、回滚；TestR12LostAgentRepliesAreCompensatedAndConfirmed；StartPublish/明确 profile 清理；HTTP route 专属断言留 R15 |
| test_router_return_allocation.py | TestR12AllocationStrictness、mark/mask/readback、raw A01；握手只相信 Agent 分配，无本地 fallback |
| test_ros_return_advisory.py | TestR12AdvisoryWholePlansPython：正常、有警告、遮蔽等代表；不变更 device/evidence；不扩输入组合矩阵 |
| IPv6/PD/DNS integration 的 release 部分 | TestR12FullInspectionStagesPython：9 个全链路代表，逐阶段失败回滚、PD/RA 跳过；rule-sync 半部留 R14 |
| ROS policy replacement | TestR12PolicyReplacementPython：5 个 install/withdraw/准备失败/blocked 代表；完整计划及实际请求/表、重复 BuildPlan 不变 |
| Runtime/local publication | TestR12RuntimePublicationBoundaries、ThreeStatesAndConnectionCAS、LegacyRouterPublicationPreservesAbsentDefaults、StartPublish 等真实 Runtime/SQLite 回归 |

原始协议：RouterOS literal body/header/Basic auth、GET/DELETE 无 body/Content-Type、补偿 DELETE；
超时记录器原样透传，全部仍是生产 10s。A01 GET/PUT/DELETE 原始 body/header/Bearer，previous spec
不丢未知字段；生产调用没有另造 HTTP 传输。JSON 对照按功能规范解析比较，字节格式另由 raw wire 断言。

最后主要命令和输出（都在前台跟到结束）：

```sh
rt -- bash -lc 'cd server && go vet ./internal/routers/... ./internal/repo && go test -race -p 4 -count=1 -timeout 25m ./internal/routers ./internal/repo -run "TestR12|TestR0[789]|TestR1[01]|TestGroup|TestRoute|TestRouterOS|TestOwnerLifetime|TestMissingJobLock|Test.*Publication|TestR10IPv6|TestR11PD|FuzzR12ReturnHandshake"'
# .64: routers 188.076s; repo 79.536s，全部通过

rt -- bash -lc 'cd server && go vet ./... && go test -race -p 4 -count=1 ./internal/routers ./internal/repo -run "TestR12Runtime|TestR12Publication|TestR12Legacy|TestR12ReturnCleanup|TestR12StartPublish|TestR12FingerprintAcross|TestOwnerLifetime|TestMissingJobLock" -timeout 6m && go test ./internal/routers -run "^$" -fuzz "^FuzzR12ReturnHandshake$" -fuzztime=60s -parallel=4'
# .64: 全仓 vet clean; routers 4.477s; repo 2.008s; fuzz PASS 61.031s / 884644 次

rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers internal/repo/profiles.go)" && go vet ./internal/routers/... ./internal/repo && go test -race -count=1 ./internal/routers -run "TestR12StartPublish|TestR12Lost|TestR12Runtime|TestR12Legacy" -timeout 4m'
# .64: gofmt 无输出；vet clean；routers 3.303s
```

最后一条是在补偿 GET 的完整 unknown-field proof 和 StartPublish 测试进程隔离之后运行；未为不变代码
重复全套对照。较早 .32 raw RouterOS、三态/CAS/profile 定向分别 0.058s、0.752s 通过。
完整门禁、最终 O3-6b/6c 集成树和 L5 都没有冒领完成。

## 复核类别与限制

- R04/R05：不另写设备 journal/证明/批量恢复；使用共享 Driver/BatchDriver 的可信 undo/ID、拒绝、
  unknown-write、动态对象、外改检查。部分成功即时记录；回滚逐条结果，失败不假装全部成功。
- R06 F1/F3：生产超时原样透传和原始无体读删；F2/F4/F5：共享 diag/routeSafeError、用户名保留，
  typed DTO/严格字段；不增加 Python 兼容异常或 JSON roundtrip 业务实现。
- R07 F1–F5：共享脱敏不复制；CIDR/identity 用原 helper；单次 ReuseAddressReads，构建不改 shared ops；
  原 list/group set 验证和范围索引照用；shared report/driver 在 R03–R11 同树定向通过。
- R08 F1/F2：恢复/未知和空集合不混淆，复用原 proof；F3：prepare_static 非 nil 空表 presence；
  F4/F5：不造空 role、不丢 verifier 错误；F6：Runtime 边界与唯一已批准的本地记录例外。
- R09 F1–F6：不重做 remap/多表 evaluator；所有 verify 错误触发补偿，部分失败保留 owner 的 manifest；
  DNS guard 运行时脚本隔离及 O14 遗留不声称已在 R12 修复。
- R10 F1–F5/L5-D：使用已有 DELETE→GET404 补偿证明；Agent 补偿新增 GET+完整 spec 证明；原动态/
  connect 文案和外部对象保护不放宽，不猜新 ID。固件 ID/404/PUT-response stable 字段仍需 L5-D。
- R11 F1/F2：不把 WAN dhcp-client 整行视为 CF 所有；底层 hook owner 按 client ID，仅 hook 行更新，
  继续跑复核探针；指纹不纳入密码。下层 runtime proof 尚未登记的问题归 O14。
- A24 F1/F2：日志/last_release 及 Agent 错误共享脱敏；Detach 后和提交前取消检查；真实 Runtime
  证明写前零提交、写后恰好一次 revision+失败记录+manifest、无设备补偿，重启预览识别归属。
- N8-4ef/N8-5：model.IssueUnder 校验 retained proof；raw unknown 字段和整数保留；Set=false 不清理，
  显式 clear 才删除，不从 fallback/nil 猜写意图；native repo adapter 只在既有 legacy 边界解码。
- O3：无新锁表、flock、裸 go；L1 由 R15 先取，L2 owner Detach，held ctx 下传，Spawn 追踪。
  唯一 Uninterruptible 的语义已测，静态白名单待集成 patch 已明确列出。

L5 继续验证实际流量、RA/DHCP 续租、PD 重启/前缀轮换和脚本运行、设备稳定字段/ID/丢回复；fake
只证明协议和补偿决策。交给新 Opus 复核，再由协调者在合并树 gate/L5 验收。
