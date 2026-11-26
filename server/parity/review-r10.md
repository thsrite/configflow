# R10 HR 复核（IPv6 policy、IPv6 Fake-IP、IPv6 规则集路由）

复核者：新的 Opus 会话（未参与移植；R10 由 Codex 移植）。分支 `go-opus-review-r10`，
从 `go-port-r10@3d2354f7` 分出，基底 `go@f90a156e`。只用 loopback fake 和合成数据，
未接触生产、真实路由器、DNS 或 PVE；未 push，未开子 agent。

范围：完整读了 `backend/routers/ipv6_policy.py`（529）、`ipv6_fakeip.py`（348）、`ipv6_routes.py`（370），
以及它们依赖的 `ipv6_readiness.py`（`_lan`/`_gateway`/`_same_gateway`/`enabled`/`yes`）、
`report.py` 的两个 IPv6 段、`plan.py` 的阶段接线、`routeros_batch.restore_routes` 和
Agent 的 IPv6 地址上报（`go-agent/routes/ipv6_readiness.go`）。对照 Go 的
`ipv6_{model,plan,write,policy_checks,policy_eval,report,messages,policy_messages}.go`，以及被改动的共享驱动
`routeros.go`（`request`、`perform`、`rosCheck`）、`routeros_batch.go`（`consoleAdd`、`addRoutes`、
`deleteRoutes`、`restoreRemoved`）、`forward_hint.go`、`Manifests()`、`routerfake`。

## 结论

**0 条 high，1 条 medium（已修），1 条 low（已修），另有 3 条 low 记录在案（不改代码）。**
未达到「≥ 2 条 high」的回退门槛。移植质量高：Python 对照矩阵（68 路由序列、330 条 policy 全证据、
14 组 settings）本轮全部复跑通过；我另加的 24 组路由序列和 15 组 policy_checks 输入，
除下面两条外与真实 Python 完全一致（结果、设备表、写入序列）。

## 发现

### 中

- **F1 — 补偿 DELETE 的回复丢失后，Fake-IP 回滚不再恢复旧路由，网段失去路由（Go 引入，已修）。**
  场景：已发布的 Fake-IP 路由 `*65` 指向 `fe80::4`；Mihomo 换了 LLA，发布替换：删 `*65`、建 `*66`。
  `*66` 未生效 → 回滚，顺序为「删 `*66`、恢复 `*65`」。删 `*66` 的回复丢失（或之后的回读失败），
  但设备实际已删除。移植者为保护替代路由加了「回滚中出现失败后，Fake-IP 跳过后续所有步骤」，
  于是旧路由不再恢复，设备上 Fake-IP 网段**没有任何路由**，客户端拿到的 IPv6 Fake-IP 全部不通，
  直到下一次发布。Python 不跳过：恢复步骤照常执行（`*66` 已不在，恢复不冲突），旧路由回来。
  证据：`TestR10ReviewLostDeleteDeviceMatchesPython` 用同一 loopback fake 跑真实 Python 与 Go，
  修复前 `.32`：`R10 device tables differ: $./ipv6/route lengths Python=1 Go=0`；
  `TestR10ReviewFakeRollbackRestoresAfterLostDelete` 修复前 `previous Fake-IP route not restored: []`。
  修复：补偿 DELETE 失败后，用一次 `GET /ipv6/route/<id>` 证明该 ID 已不存在时按成功处理
  （`ipv6RouteGone`，读失败不算证明）；真正失败（路由仍在、被改动、读不到）仍然跳过，保持移植者
  「不删仍能保护流量的替代路由」的设计。修复后旧路由以新 ID 恢复并按恢复响应重映射进 manifest
  （比 Python 好：Python 此时整体报回滚错误、manifest 仍指向已删除的旧 ID）。
  规则集路由走同一函数，单条补偿同样受益；批量补偿（`restoreDeleted`）本来就按回读判断。
  提交 `33df00c2f`。

### 低

- **F2 — 现有正确但为 dynamic/connect 的 Fake-IP 路由，阻断文案与 Python 不同（已修）。**
  Python `_correct` 不看 dynamic/connect，先把它认作可借用，再以
  「现有 IPv6 Fake-IP 路由不能确认为普通静态路由，请先核对」阻断；Go 的 `ipv6Correct` 含这两个标志，
  直接落到通用的「已有 IPv6 Fake-IP 路由错误、停用或与其他路由冲突；不会自动覆盖」，
  `check.borrowed` 也从对象变成 null。两边都阻断、零写入，只是中文文案和 check 不一致（违反
  「中文消息逐字节一致」）。证据：`TestR10ReviewPythonProbes/fake/review_foreign_{dynamic,connect}`
  修复前 `$.results[0].blockers[0] Python=现有 IPv6 Fake-IP 路由不能确认为普通静态路由… Go=已有 IPv6 Fake-IP 路由错误…`。
  修复：检查 Fake-IP 候选时用与 Python `_correct` 同义的 `ipv6Routes`（不含 dynamic/connect），
  原有「不能确认为普通静态路由」分支随之可达；回读、`good`、新建校验仍用更严的 `ipv6Correct`
  （这些位置的对象不可能是 dynamic，行为不变）。规则集路由 Python 本来就把这两个标志算作冲突，一致。
  提交 `33df00c2f`。

- **F3 — 规则集回滚在第一处真实失败后，跳过剩余的新路由删除（设计取舍，记录）。**
  首次发布 5 条后回读失败 → 回滚删除 5 条新路由；若第 1 条删除被拒绝，Go 不再尝试删除其余 4 条，
  把它们按创建证明登记进 manifest（`recoverIPv6Created`）；Python 会逐条尝试。Go 的结果安全：
  多留的路由是 ConfigFlow 自己证明过的、指向 Mihomo，下一次预览会把它们当受管对象处理（撤回或保留），
  不会成为孤儿；行为已登记在 behaviour-changes「失败恢复后不继续删除仍能保护流量的替代路由」。不改。

- **F4 — 共享 `restoreRemoved` 的单条回退改为 `perform`+`verifyChange`，R04 调用方的非设备错误文案多了前缀（记录）。**
  原 `restoreSingleChange` 对 ctx 取消等非 `RouterError` 返回原始文本，新闭包统一包成
  「…回滚未完成：<原文>」。设备写入序列完全相同，只影响关机取消时的错误文本；R04 的
  回滚测试在合并树上照常通过。符合第三条常设检查（已在合并树跑 R04–R10 全部 routers 测试）。不改。

- **F5 — 关机打断发布：已落地的新路由未登记归属（与 R04–R09 相同的残余，记录）。**
  `TestR10RuntimeCancellationBoundaries/shutdown_after_final_read` 固定了现状：关机取消后不再补偿、
  不提交 manifest，已创建的 `cf-rule6:`/`cf-fakeip6:` 路由在下次预览时表现为外部正确路由
  （borrowed，不会被删除）。与 R09 F6 同属 O14 切换计划（关机前等待发布结束）。不改。

## 重点核对（未发现其他问题）

1. **共享多表求值器。** 已按 R09 F5 原位扩展：`PolicySnapshot` 按表保存（nil=未读到，空表=已读为空），
   `EvaluatePolicyChain(s, table, chain, p) PolicyDecision{Decision, Evidence, Translation}`；
   nat 表的 masquerade/src-nat/dst-nat/netmap/redirect 给出 `translate`+`translation`，其他表一律 unknown；
   未读表的证据按地址族带 `ipv4_` 前缀；中文证据全在 `ipv6_policy_messages.go`。逐行对照 Python
   `evaluate_chain`：跳过 log/passthrough、jump 环检测（含栈内链）、return 回到调用链、未知分支只在两路结论
   相同且为 allow/drop 时合并、证据去重保序、`dict.fromkeys` 等价的 `uniqueStrings`、评论按字符截 180。
   证据切片每次克隆，无别名。R09 唯一调用方 `CheckForwardHint` 只取 `.Decision`，`s == nil`、filter 为 nil、
   空/错型 action、跳转目标三种情况的结论与改前相同，`TestR09SharedEvaluatorPython` 继续通过。
   **三值 fail-closed**：未知表名、未读表、未知选择器、错型 action、预算耗尽、路由规则未读都给 unknown；
   `policy_checks` 中 unknown 不会变成 allow（`traffic`/`nat` 汇总 unknown 优先于 translate/allow）。
   `_path` 的阶段顺序、目的 NAT 在非 srcnat 处出现即 unknown、`nat_decision or 'unknown'`、
   WAN 唯一出口、节点/客户端只用提供的字面地址（不解析域名）、最多 3×3 组合、候选源地址 drop → warning
   都与 Python 一致；`packetDetails` 的字段顺序逐类核对过。我另加的 15 组输入全部一致。
2. **IPv6 Fake-IP。** 三个 Python 模块不生成 YAML（只读保存的 Mihomo YAML），移植者已逐字节断言保存 YAML
   不被改写。settings：顶层 `ipv6` 默认开、两种 DNS IPv6 模式都发布、`dns.ipv6` 开时才要求 range 与 TUN、
   range 必须在 `fc00::/7` 内（`IsPrivate` 且前缀 ≥ 7，等价 `subnet_of`）、`strict=False` 掩码、
   `dns` 非对象/`ipv6` 非布尔报错；合并键和别名经 `yamlx.Get` 跟随。YAML 1.1 的 `yes/on` 按 N7 已接受政策
   视为字符串。地址段与 ROS 实际地址重叠 → blocker；更具体的启用路由、同网段停用路由 → 冲突。
   与 IPv4 Fake-IP 无交互（不进入 IPv4 NAT/回程规划），policy_checks 的 v4/v6 Fake-IP 各自取网段。
   畸形 manifest（错版本、resources 非数组、多于 1 个、spec 与 resource 不一致、空/非法/重复 ID）全部拒绝。
3. **IPv6 规则集路由。** 增量：保留（唯一受管、指向当前下一跳）、借用（唯一外部正确路由，交出受管）、
   冲突（ROS 自身地址、外部错误/停用/多条）、替换（下一跳变化，先加后删）、撤回（不再需要；输入不完整时保留）。
   预览与执行一致：执行前按 scope 重算证据并比对，`owned` 逐项相等，adds 必须在 scope 内且下一跳一致，
   deletes/retained 必须正好划分 owned。归属：ID + 完整 stable 配置 + 非 dynamic/connect；每个 remove
   批次前重新按 ID 读取并比对证明；新增前 scoped/console 读表拒绝新出现的外部同目标路由。
   ID 复用、外部改注释/下一跳/动态标志、同配置的外部路由均不会被收养。我另加的 101 条替换、
   101 条替换后回滚、101 条回滚、外部删除后收缩，设备表与写入序列都与 Python 一致。
4. **回滚和部分成功。** 移植者的修复完整：只用恢复响应证明的新 ID 重映射（不按配置猜测）；
   部分成功也重映射；补偿 DELETE 被拒绝时用创建证明登记新对象；整体回滚不完整时保存两代证明；
   Fake-IP 旧对象未恢复时保留当前已发布证明。唯一缺口是 F1（把「已删除但回复丢失」当失败），已修，
   并在真实 RouterOS fake 上与 Python 对照验证。
5. **写 owner。** 四个 owner（Apply/Restore × 两功能）入口 `ctxx.Detach` 后立即 `ctx.Err()`；每个设备写入
   前（`perform` 前显式检查 + 共享 `request` 入口检查）、manifest 提交前（`remapIPv6`、
   `recoverIPv6Created`、两个 Apply 的最终提交）都检查。`TestR10RuntimeCancellationBoundaries` 用真实
   `Runtime.Start/BindRequest/Shutdown` 覆盖断开仍完成、关机后零 I/O、读快照后/最终回读后关机不提交。
   F1 新增的只是一次只读 GET，经共享 `request`，关机后不执行。
6. **凭据和地址脱敏。** host 错误走共享 `agents/diag.FailureText`，传入 Agent token 与路由器密码；
   username、`rest-api` 诊断保留（`TestR10HostDiagnosticsRedactCredentials`）。R10 不写日志，
   不新增私有脱敏。Agent 上报的地址由 `net.ParseIP(...).String()` 产生，不带 zone。
7. **RouterOS 请求原始字节。** `TestR10RawWireAndProductionTimeouts` 用生产 `devhttp` 断言 PUT JSON、
   完整 `/execute` 脚本、`/remove` 体、GET/DELETE 无体无 Content-Type、Basic auth、IPv6 地址列表查询
   `%20`/`%2F`/UTF-8 编码；超时记录器只记录不覆盖（10 s）。`forward_hint.go` 的 IPv6 查询改为
   `quote(safe='')` 同义编码，只影响 IPv6 分支（R09 F5 的遗留项）。

## 四条常设检查

①测试替身不覆盖生产参数：超时记录器透传，batchMin/路由批量用生产默认值；②原始请求字节：见上第 7 项；
③共享驱动改动（`request` 入口 ctx 检查、`perform` 创建守卫与私有 `created`、`consoleAdd`/`addRoutes`/
`deleteRoutes` 的可选证明、`restoreRemoved` 的可选重映射）对其他调用方默认关闭；本轮在合并树上跑了
`./internal/routers/...` 全部 race（含 R04–R09 测试）；④Detach/ctx.Err：见上第 5 项。

## L5 补充（加入 L5-D，只在隔离 CHR 实验网执行）

1. 补偿 DELETE 回复丢失时（F1），实机上 `GET /ipv6/route/<id>` 是否返回 404（`KindMissingResource`）。
2. 回滚恢复出的路由，回读的 stable 配置是否与原证明完全一致（`remapIPv6` 保留原证明而不重读；
   若 RouterOS 恢复后多出/改变非运行字段，下一次预览会误报「被外部修改」）。
3. PUT 响应与随后 GET 的 stable 字段是否一致（`recoverIPv6Created` 用 PUT 响应作创建证明）。
4. dynamic/connect 的 IPv6 路由在 `/ipv6/route` 上的标志值（F2 的触发条件）。

## 验证命令与输出

修复前（`.32`，新测试，运行时代码为移植者的 `3d2354f7`）：

```sh
rt -- bash -lc 'cd server && go test -count=1 -run "TestR10ReviewPythonProbes" ./internal/routers'
# --- FAIL: TestR10ReviewPythonProbes/fake/review_foreign_dynamic
#     R10 result differs: $.results[0].blockers[0] Python=现有 IPv6 Fake-IP 路由不能确认为普通静态路由，请先核对 Go=已有 IPv6 Fake-IP 路由错误、停用或与其他路由冲突；不会自动覆盖
# --- FAIL: TestR10ReviewPythonProbes/fake/review_foreign_connect（同上）
rt -- bash -lc 'cd server && go test -count=1 -v -run "TestR10ReviewLostDelete|TestR10ReviewFakeRollback" ./internal/routers'
#     R10 device tables differ: $./ipv6/route lengths Python=1 Go=0
#     previous Fake-IP route not restored: []
```

修复后（`33df00c2f` + 探针 `62aac8870`）：

```sh
rt -- bash -lc 'cd server && go test -count=1 -run "TestR10" ./internal/routers'
# .32: ok  configflow/internal/routers 13.938s（含 TestR10PythonBoundary、全部 TestR10Review*）
rt -- bash -lc 'cd server && gofmt -l internal/routers && go vet ./internal/routers/... && go test -race -p 4 -count=1 -timeout=40m ./internal/routers/...'
# .64: gofmt 无输出，vet clean；ok configflow/internal/routers 633.612s
```

最终门禁（`.64`）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GATE_LOGS=$HOME/.cache/r10-review-gate server/scripts/gate.sh -p 6 FuzzR10IPv6Input:./internal/routers FuzzR09PolicyEvaluator:./internal/routers FuzzR09DNSGuardManifest:./internal/routers FuzzR09DNSGuardOperation:./internal/routers FuzzR08PolicyInput:./internal/routers FuzzR07DecodeAddressRow:./internal/routers FuzzFakeIPYAML:./internal/routers'
```

说明：排队约 19 分钟才获槽（06:35 → 06:54，服务器上同时有另外两个门禁）；我在第 15 分钟检查时它
已接近获槽，获槽后没有中止。本地 `rt` 会话随后被工具的后台时限切断，服务器上的 gate 继续跑完，
结果从 `GATE_LOGS` 读取（stdout 汇总行丢失，下列各阶段日志齐全；`fail` 会立即退出，后续阶段都跑了，
说明 gofmt、vet、两个 build tag 的构建/vet/lifecycle 测试、proddiff observer 自检均已通过）：

- 全量 `-race`：97 个包 ok，无 FAIL/panic；`internal/routers` 755.861s，`internal/routersplit` 26.531s。
- proddiff：rc 0，`baseline: new=0 stale=0`，self-check PASS。
- difftest：15 个包全部 ok（agents 332.810s、subscriptions 171.385s 等）。
- fuzz 60 s × 7：FuzzR10IPv6Input、FuzzR09PolicyEvaluator、FuzzR09DNSGuardManifest、FuzzR09DNSGuardOperation、
  FuzzR08PolicyInput、FuzzR07DecodeAddressRow、FuzzFakeIPYAML 全部 PASS。

R10 状态：本复核把 R10 设为 `verified`（L5-D 仍待做）。
