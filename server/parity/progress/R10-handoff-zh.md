# R10 IPv6 移植交接

## 范围与状态

分支 `go-port-r10`，已无冲突 rebase 本地 `go@f90a156eb`（O3-6a）。
实现提交：`55a022ad4`、`99c4b1cc3`、`165cb81fd`、`96e8b4326`；
O3-6a 跟进：`f03376cca`、`151c7b3a0`。状态 **ported，待独立 Opus HR 复核**。
只访问测试服务器上的 loopback fake；没有访问生产、真实路由器、DNS、PVE，没有 push/子 agent。

完整移植 `backend/routers/ipv6_policy.py`、`ipv6_fakeip.py`、`ipv6_routes.py` 的模块功能，
没有新 HTTP 路由、后台任务或外部依赖。R12 的整体 inspection/发布阶段/持久化调用与 R11 的
readiness 读表服务仍属于各自模块；这里只提供实际可调用的契约，不用假 handler 宣称 HTTP 差分。
没有新增用户功能/操作入口，故无需修改 doc 截图。

## 实现和后续调用契约

- `EvaluatePolicyChain(snapshot, table, chain, packet) PolicyDecision`：扩展 R09 共用求值器；
  raw/mangle/nat/filter、完整中文证据、nat 的 translate/translation、256 分支/12 层跳转，
  原三值 matcher 与接口列表 256/12 预算不变。R09 `CheckForwardHint` 改调用新签名，仅取 Decision。
- `PolicyEnvironmentFromSnapshot(tables, errors)` 保留未读/读失败与成功空表的区别；
  `PolicyChecks` 输出控制报文、节点/源地址候选、客户端 IPv4/IPv6 Fake-IP 去返程、NAT 与汇总。
  只使用供应的字面地址/端口，不解析域名，不声称连通性。
- R11：`PolicyTablePaths()` 返回独立 path map；使用上述 environment 转换（error key 不能变成空表）；
  `IPv6LAN`、`IPv6Gateway` 共享 LAN/下一跳规则。LLA 优先、无 LLA 时唯一 ULA，已证明的旧 ULA 可保留；
  只看 Agent 上游接口地址；不从 interfaces inventory 猜测。已带 zone 的主机地址候选拒绝，
  避免 Python 拼出双 `%` 下一跳。R11 仍负责 readiness 的全表/按引用列表读取和 Agent 协议适配。
- `IPv6FakeIPSettings(*model.Profile)`、`IPv6RoutesSettings(*model.Profile)`；只经 yamlx Parse/Node 读取。
  top-level ipv6 默认开；Fake-IP 两个 IPv6 模式均发布路由，只有 dns.ipv6 开时 TUN/缺失 range 才阻断。
  model 的 `mihomo.custom_config` R5 issue 会报错，不能回退成关闭后撤路。
- R12：对适用 router 调 `IPv6FakeIPRelevant` / `IPv6RoutesRelevant` / `IPv6RoutesNeedsHost`，
  用同一份 Agent IPv6 报告构建 `IPv6Host`；调用 `InspectIPv6FakeIP` / `InspectIPv6Routes`，
  放入 `RouterState.IPv6FakeIPPlan` / `IPv6RoutesPlan`。`BuildReport` 已直接接线两个完整 section。
  `IPv6FakeIPReplace` / `IPv6RoutesReplace` 实现现有 Op，driver.Apply 的四种 replace/restore handler 已注册。
  R12 保持原阶段顺序，分别调用 `VerifyIPv6FakeIP` / `VerifyIPv6Routes`；
  Fake-IP blockers 阻断整次发布，routes.Skip 只跳过 IPv6 规则集阶段，且不添加该阶段 check；
  两种同口转发 warning 要像 Python 一样去重。rules.Auto 是禁止自动同步的原因，空字符串才允许。
- `Manifests().IPv6FakeIP` / `.IPv6Routes` 分开携带 Set 和独立 raw Value。
  未赋值保持 unset；明确撤回输出 set + null。R12 失败后也保存实际 driver manifest；不能把 unset 当 clear。
  调用旧 routers.Router 时 raw Extra 会完整保留错误类型的 manifest 并拒绝；
  R12 从 model.Router 转入时仍必须按 N8-4f 检查 `model.Issues` 对应 ownership 字段，不能丢掉 R5 诊断。
- R10 无新锁；继续每次发布独占一份 driver，由 R12/R14 持有 L1/L2；O3-6b 负责 JobLock 接线。
  没有改 repo hooks 或另建锁系统。O3-6a 合入后 R10 四个新写 owner 使用 ctxx.Detach；无文件/持久叶资源，
  不新增叶锁。共享 R08/R09 owner 保持现有转发，O3-6b 继续负责 router JobLock。

## 安全与行为修正

均登记于 `behaviour-changes.md`，不放宽普通差分：

1. 删除规则集路由时对每条完整 stable config 证明归属；每个 remove 批次前再证一次，
   不因 Python 只核对 id/destination 而删除预览之后修改/复用的对象。
2. 写前出现同目标外部路由时停止创建，包含共享 console/scoped single 的最终读表守卫。
   不从一个旧的空预览推断现在仍空，不盲目重试不确定 PUT。
3. 回滚只用成功恢复响应证明的新 ID，取消 Python `_rebuild` 按相同配置查找并收养对象的路径。
   新对象在创建响应/console 证明时保留完整 birth config；若补偿 DELETE 拒绝或回读失败，
   将这些本次已证明的新 ID 与原配置登记为恢复 receipt（即使现在缺失也只是 stale），
   不在失败之后重新读取/收养当前配置。下一次预览能核对/修复残留，不留下明确已知 ID 的孤儿。
   部分成功恢复也更新新 ID；重映射只改 ID，保持原 stable config，避免刷新时顺手认可外部修改。
   规则集整体回滚不完整时保留两代原始证明（缺失 ID 仍是 stale，绝不成为删除/收养的依据）；
   Fake-IP 整体回滚未恢复旧对象时保持当前已发布证明。失败恢复后跳过后续替代路由删除以保留保护。
4. 非空、格式有效且唯一的 *hex ID、manifest self-consistency 在对象已不见时也必须成立；
   缺失 resources、错误类型、空 ID、重复 ID 不能成为空 ownership。动态/connect 路由不接管。
5. 已带 zone 的 Agent 地址不再被拼接第二个 zone。路由前缀用原生 netipx 拒绝 scope-id，
   不复用策略 matcher 的去 zone 规则而把 /64 误变为 /128；接口地址 evidence 则保留 zone、
   host bits 并规范化 /064。无有效候选时使用已有保守 blocker/skip。
6. host 错误复用 agents/diag 的共享凭据处理并传入实际 Agent token/router password；
   username=api、rest-api 诊断保留。无新私有脱敏算法，也不修改归属行里的值。
7. 借用规则集路由的 verify 同时要求 active；Python 只比 stable config 会放过运行时失活。
   两个 route feature 的 borrowed 活动状态有独立 race 回归。
8. 无效/缺失 range 的 Python 内部 ipaddress 异常文字改为明确中文；业务中文文案保持一致。
   YAML duplicate/alias/depth 等沿用 N7 已接受政策，没有额外的 YAML/JSON 模拟器。

残余边界（与 R04–R09 相同）：关机 drain 取消后禁止继续补偿写，可能留下尚未登记的现场；
无可信新 ID 的丢失回复不会猜测归属，也不会删除现场。这些失败明确返回错误，交 R12 保存已有证明、
提示重新检查；需在 L5/O14 验证停机与恢复流程。

## 验收与覆盖

`TestR10PythonBoundary`：68 个合成状态序列，真实 Python public inspect/apply/restore/verify 通过同一
loopback RouterOS fake；严格比较 plan/check/report（含 null/[]/missing）、undo、错误类别/业务中文、
有序设备表和全部实际写入请求（console script 完整文本也比较）。POST /print 是只读查询，
Go 多出来的每批归属证明查询单独排除于“写入序列”，不排除任何实际 mutation。
覆盖：增量/幂等/借用/冲突/ULA 保留和 LLA 优先/失联/无 TUN/两模式、关闭撤回、换下一跳与回滚、
lifetime 与地址 host bits/actual-interface、拒绝/丢失回复/新路由 inactive，以及 1/3/4/100/101/201/501/4096 条。

`TestR10PolicyPythonBoundary`：330 个真实 Python 全证据对照；312 个 family × table × action × known/unknown/false
组合，另 18 组完整 policy_checks（策略路由、raw/mangle、srcnat/dstnat、候选降级、scope cap、路由分裂等）。
R09 既有 matcher/jump/list/budget 差分继续运行；没有把 unknown 当作 allow。

`TestR10SettingsAndYAMLBytesPython`：14 组真实 Python settings 与保存 YAML UTF-8 字节对照，
含 CRLF、注释、引号、共享 alias、默认值、模式和 TUN。**这三个 Python 模块不生成 YAML**；
测试逐字节比较它们消费的保存 YAML，且确认 model 和 settings 调用不重写它。
仅对缺失 range 的 Python 内部异常文字单列断言（两端必须拒绝，其余 settings 字段仍严格比较）。

`TestR10RawWireAndProductionTimeouts`：生产 devhttp，原始 PUT /ipv6/route JSON、完整 /execute JSON、
/remove JSON、Basic auth、Content-Type、GET/DELETE 空 body 与头、IPv6 列表 `%20`/中文/斜杠查询编码；
每次 Do 都记录实际 10s 参数而不覆盖。既有 R06/R08 真正 10s silent-peer 回归由完整 gate 重跑。

`TestR10RuntimeCancellationBoundaries`：真实 Runtime.Start/BindRequest/Shutdown，12 组 fake/routes 的
断开仍完成、关机后的 apply/restore/empty restore 拒绝、读 snapshot 后关机、最终回读后关机。
设备状态及 manifest 都在提交点断言；没有用手造 canceled context 冒充 Runtime 关机。

`TestR10OwnershipAndFreshnessGuards`、`TestR10PartialRollbackKeepsTrustedRestoredIDs`、
`TestR10RollbackNeverAdoptsMatchingForeignRoute`、`TestR10RollbackRemapDoesNotBlessExternalEdit`、
`TestR10HostDiagnosticsRedactCredentials`、`TestR10ManifestSpecAndUnknownFields`、
`TestR10RestoreFailureKeepsPublishedOwnership`、
`TestR10FailedApplyRetainsProvenCreationWhenDeleteRefused`、
`TestR10BorrowedRouteMustRemainActive`：外部编辑、ID reuse、connect/dynamic、malformed manifest、
R5 fallback、部分恢复、新 ID、同配置外部路由与脱敏。`TestR10ManifestCopies` 防输入/输出别名。

repo 的五个 `TestR10...`：86 个子场景，五种 writer 的 IPv6 ownership 丢失保护、
publication remap/显式 clear/并发 metadata、连接变更拒绝/凭据修复/克隆/删除保护，两种 manifest 均覆盖。

`TestR10IPv6AddressMathPython`：10 组真实 Python interface stable 证据（ULA/LLA/zone/mapped/host bits/
/064/invalid），另断言 scoped route prefix 不被静默截断、zone 主机不产出双 scope。

新 fuzz：`FuzzR10IPv6Input`；共享 `FuzzR09PolicyEvaluator` 等一并纳入 gate。
`scripts/r10-mutation-gate.py`：result/journal/request/device-table 四种编译成功的破坏均被真实 Python 边界捕获；
编译失败不算检测，finally 还原源码；另加 recovery 第五类破坏，明确证明补偿 DELETE 拒绝后的归属回归能失败。

逐个 Python named test 的映射与 R11/R12/R14 边界见 [../r10-test-map.md](../r10-test-map.md)。

## 复核问题类别逐项规避

- R06 F1（生产 timeout 被假替身覆盖）：R10 记录参数并转发原值；全部 I/O 复用 rosTimeout，最终共享真 watchdog 回归。
- R06 F2 / R07 F1（脱敏错把 username 当 secret、复制 helper、合并树破坏）：复用 safeDeviceText/agents diag；
  不将 username 加入 secret；独立 raw/secret 用例及完整 gate 覆盖 R04–R09/devhttp/routersplit。
- R06 F3（nil body→null）：只用现有 request，原始字节强制 GET/DELETE 空 body/无 Content-Type。
- R06 F4（非字符串 interface 变可写）：下一跳只来自 typed Agent host 与 RouterOS 明确 LAN 字段；
  不依赖 rosText 把任意数当成合法 ID；manifest 的身份/网段严格验证。
- R06 F5（动态 dict 合并/JSON round-trip 建类型）：新 plan/check/report/op 用 Go structs；
  只有保留未知 manifest 字段的 raw patch 使用 RawMessage map，不建立 Python dict/exception 兼容层。
- R07 F2（range 身份与 CIDR 混用）：R10 仅接受 IPv6 network/host；matcher 的地址区间只三值匹配，不拿区间做 ownership key。
- R07 F3（平方扫描）：owned/foreign/当前行按 ID/CIDR 建索引；100 条 console、200 query terms、500 remove 复用；4096 条边界差分。
- R07 F4（别名）：driver manifests、配置行、输入/输出读取均独立；proof 原文保留，Manifests 返回 clone。
- R07 F5（只比数量）：逐 ID/完整 stable 配置、active 检查和目标集合证明；重复 ID 拒绝，不能只靠长度通过。
- R08 F1（局部读不到就当删除）：IPv6 route manifest 使用完整 routes snapshot，回滚不按目标局部搜索收养；
  缺失旧 ID 仅可按本次已证 journal 恢复。
- R08 F2（失败回退成空）：table nil/空明确；错误 manifest、YAML R5 issue 及 read error 都返回 blocker/skip，不生成撤回证据。
- R08 F3（空列表/presence）：Check/Op 强类型，数组初始化为 []，enabled borrowed:null 与 withdrawal removed:[] 用 MarshalJSON 明确；Python 全字段差分。
- R08 F4（空名字当角色）：无空角色映射；空 LAN 不生成下一跳/同口提示的肯定结论。
- R08 F5（丢失回读错误消息）：使用共享 checkWritten/verifyChange，inactive/拒绝/失联路径真实 Python 差分；新增中文集中 messages。
- R08 F6（Detach 文档仍承诺无限补偿）：owner 掉请求取消、保留关机取消；文档明确 drain 以后不能补偿，R12 保存现场。
- R09 F1（部分恢复丢归属）：成功部分也 remap，保持原配置证明；专门失败恢复用例。
- R09 F2–F4（Netwatch comment/暂停竞争）：R10 不写防火墙/Netwatch，只读 hairpin；不扩张 comment 归属。
- R09 F5（求值器只够 filter）：原位扩展按表、完整证据和 translate，R09 只取 Decision 保持原行为；IPv6 query 原始 `%20` 已核。
- R09 F6（关机中断）：同上第四常设检查；无 ctx leaf 写经 request 再次检查，manifest 提交前也检查。
- A24 F1（凭据在旧/未知 metadata 中泄露）：不公开 Agent/host metadata，host error 使用共享脱敏并保留普通 username；
  明确测试 token/password 无法逃逸，归属数据不脱敏。
- A24 F2（快速 owner 忽略 shutdown）：每个写 owner Detach 后立即 ctx.Err，每次 device request/manifest 更新前再检查；真实 Runtime 测试。
- N8-4ef high（R5 空值误作 ownership）：raw manifest 严格解析，typed profile 使用 Issues；R12 adapter 必须保留 model.Router issue。
- N8-4ef presence/clear：Set 与 Value 独立，未读 nil 不成为清除意图；资源未知字段和 manifest 未来字段保留，重映射不丢。
- O3/N7/N8：不新增 goroutine/flock/锁；纯读可取消，owner detach；yamlx 唯一入口，typed model.Profile/model.Agent，原生 JSON/RawMessage。

## O3-6a 的 JobLock 边界

读完 `o3-6-locks.md` 后，四个 R10 写 owner 改用 ctxx.Detach；真实 Runtime 取消父级不变。
没有本地文件/叶存储，不新增叶锁，也不对设备 I/O 使用 Uninterruptible。
共享 R08/R09 的 owner 模式保留；O3-6b 再接 routers JobLock。

仓库测试允许修改连接的场景均显式装入非阻塞合成 JobLock hook，模拟 caller 已持有锁；
没有依赖默认 hook 成功。`TestR10MissingJobLockRefusesProtectedTransactions` 确认两个 IPv6 manifest
在无 hook 时的普通凭据更新均 ErrInUse（对应 409），DB 指纹不变。生产在 O3-6b 前的 fail-closed
限制保留；R12/R15 接线不得绕过。

## 精确命令与结果

本机未运行测试/构建/lint。最新 O3-6a 合并树定向：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/routers/ipv6_write.go --back server/internal/repo/r10_ipv6_safety_test.go -- bash -lc 'cd server && gofmt -w internal/routers/ipv6_write.go internal/repo/r10_ipv6_safety_test.go && go test -p 6 -count=1 -run "TestR10|TestR09SharedEvaluatorPython" ./internal/routers ./internal/repo && go vet ./internal/routers/... ./internal/repo/...'
# ok configflow/internal/routers 11.532s
# ok configflow/internal/repo 29.859s
# 相关 vet 无输出
```

补偿失败修正之后（ctxx/lifecycle 仅有等价转发差异）的变异验证：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && python3 scripts/r10-mutation-gate.py'
# result / journal / request / device-table / recovery: DETECTED
# R10 mutation gate: 5/5 detected; sources restored
```

`.32` O3-6a 合并树共享路径 **全部通过，rt 退出 0**：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal tools cmd && go vet ./... && go test -race -p 4 -count=1 -timeout=40m ./internal/routers/... ./internal/routersplit/... ./internal/devhttp/... ./internal/repo/... && go test -p 4 -run "^$" -fuzz "^FuzzR10IPv6Input$" -fuzztime=60s -parallel=2 ./internal/routers && go test -p 4 -run "^$" -fuzz "^FuzzR09PolicyEvaluator$" -fuzztime=60s -parallel=2 ./internal/routers'
# gofmt 无输出；go vet ./... clean
# race: routers 471.428s；routersplit 17.040s；devhttp 3.596s；repo 214.788s
# FuzzR10IPv6Input: 124205 execs, PASS, ok 61.040s
# FuzzR09PolicyEvaluator: 694886 execs, PASS, ok 61.057s
```

最终 `.64` gate：**未执行检查，排队超 15 分钟，按 AGENT-BRIEF 停止并交回**。
UTC 21:50:51 开始等槽，22:06:02 仍未获槽；随后终止，只清理本任务等待进程树（1 root/3 processes）。
没有改槽位数、没有绕过调度器、没有把中止算 PASS。完整门禁由协调者 cf-gate 执行。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GATE_LOGS=$HOME/.cache/r10-o36a-final server/scripts/gate.sh -p 6 FuzzR10IPv6Input:./internal/routers FuzzR09PolicyEvaluator:./internal/routers FuzzR09DNSGuardManifest:./internal/routers FuzzR09DNSGuardOperation:./internal/routers FuzzR08PolicyInput:./internal/routers FuzzR07DecodeAddressRow:./internal/routers FuzzFakeIPYAML:./internal/routers'
```

旧基底与自查修正前的门禁已停止，确认仅本任务的进程树退出，不算最终 PASS。
本分支未完成全量 race、两个 build tag、完整 proddiff、启用的全部 HTTP difftest 和其余五个 fuzz；
当前新基底上的完整门禁无 PASS 声明。定向 race 已覆盖全部 R04–R10 共享驱动测试。

## 未运行的 L5 / 后续验证

只做 loopback，不声称真实转发/固件验证。L5-D（合并树统一 L5）需验证：真实 CHR scoped LLA/ULA
解析与已有 ULA 保留、主机多 NIC/多个 LLA、同口 forward invalid/ICMPv6 redirect、客户端两 IPv6 模式/TUN
真实答案、默认路由与策略路由/IPv6 NAT 对报文的实际影响、active/invalid 邻居收敛；501/4096/20k 规模下
真实 10s budget、100 条 console/32KiB/500 remove 的固件限制；失联/ID reuse/外部编辑和部分回滚；
关机 drain 截断时 R12 的 manifest/错误保存与重启恢复。R11 readiness 读取范围及 R12/R14 orchestration
须在它们落地后加入对应黑盒 HTTP/发布集成测试；本模块无生产启用动作。

## 文件规模

新增运行时代码：ipv6_model.go 391、ipv6_plan.go 660、ipv6_write.go 761、
ipv6_policy_checks.go 544、ipv6_policy_messages.go 73、ipv6_messages.go 82、ipv6_report.go 24。
共享 ipv6_policy_eval.go 扩展至 550 行；已有共享驱动/forward_hint/Manifests/Report 原位接线。
新增测试：ipv6_boundary_test.go 394、ipv6_policy_boundary_test.go 149、ipv6_safety_test.go 597、
ipv6_settings_test.go 117、repo/r10_ipv6_safety_test.go 174；Python 四个合成 reference 共 93 行；
变异验证脚本 40 行。没有临时 debug/testdata 或生产数据。


## 交回结论

实现完成，工作树交回前清理并提交；R10 保持 `ported`，**不标 verified**。
没有待实现的三个模块功能；自查发现的归属/部分成功恢复问题均已修复并有回归。
自查风险等级（非 Opus 独立定级）：中——删除完整 proof、按配置收养/部分回滚丢归属、补偿拒绝后孤儿；
低——scope 前缀截断、borrowed active 验证、native 内部错误文案；凭据与 shutdown 采用共享安全政策。
所有这些项目已修复；未决的是完整 gate、独立 Opus HR 复核、R11/R12/R14 自身集成与 L5 实机。

最后一个新增测试只触及 test/doc，运行时代码与上述 `.32` 完整相关包 race 的 `151c7b3a0` 相同：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/ipv6_safety_test.go -- bash -lc 'cd server && gofmt -w internal/routers/ipv6_safety_test.go && go test -race -p 4 -count=1 -run "^TestR10BorrowedRouteMustRemainActive$" ./internal/routers'
# ok configflow/internal/routers 1.067s
```

新 R11 注意：readiness 的全表/按引用列表读取仍由 R11 完成；如果要复用已经读取的 snapshot 来给出 hairpin，
应抽取 CheckForwardHint 的纯 snapshot 部分，保持错误表为 unknown，不能因重读成功而掩盖本次 snapshot 的错误。
本模块没有伪装完成这条 R11 集成路径。
