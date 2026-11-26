# A18 DNS HA 移植交接

状态：`verified`（Opus HR 复核见 review-a18.md）；L3 实机仍待协调者执行。

分支 `go-port-a18`，基于指定的 batch-6（A16/A17 集成与 N8 第 2 批）；未 push。
主体提交 `133d5ec0f`；离线证明/基线关闭 `cc146fbc2`，故障/Runtime/集成回归与修正 `82c3fab94`；状态/归属/L3 计划为独立文档提交。

## 已实现

- HA 归一化、同空间/IPv4/VRID/现有组冲突、上游回环/DNS follow auto 拒绝、RouterOS 停止清理警告、双端预检、Docker 宿主/boot/安装身份与协议实时证明。
- 四条现有 HTTP 路由（鉴权、silent JSON、409 blockers/can_force、PUT 202、live environment 的有界白名单），字段及中文提示与真实 Flask 对照。
- 私有 payload/bundle、双端持久 journal、任务摘要、备用优先顺序、实际 VIP 持有者最后更新；两次双端 VIP DNS 确认；原持有者暂存后再次确认再激活；让位撤销与选举稳定确认。
- keepalived 备机→主机应用、反序撤销旧 spec/首次移除、保存失败撤销、leftover 清理、强制停用且保留部署历史。
- `deploy.SetPairPublisher`、A01/A15 三个 HA hooks、A07 `SetHARules`、A16 `SetPairAdvance`、0/2 秒 boot loop；`compat.go` 已删，纯 registry predicates 归入 `ha.go`。
- A14 最终生成用 `Manager.HAPair` 验证两端最终适配相同，preview 保存两个 target identities；不再无条件返回 A18 暂缺拒绝。
- A16 两个真实 rule-sync worker 推进同一个配对任务；A17 真实 DNS follow owner 向两端各发布一次并同步草稿；手动 OxiDNS rules 走真实 receipt/snapshot/PrepareCandidate(forceRefresh) 且 reload_only。

## 设计决定与行为差异

- D-A18-1 已裁定 A：keepalived 阶段 HA 锁忙保留 `applying`，下一轮重试，已加故障回归。Python 在此处会把整个 enable 记失败。这一项需收入 O12。
- 取消：浏览器断连不会取消已受理工作；真正全局 Runtime 的关机取消后，每个 repo/file/device 提交点均停止写入，不记晚到 failed、不做新 undo，保留 journal 让重启继续。已受理上传和让位只 poll/read-back，不重传。
- 损坏的交接 proof 标志（例如字符串 `handoff_required` / `released`）不能被 R5 容错读成 false 而绕过门禁：拒绝继续核心写入；已有交接保留 unknown 与配对预留，等待修复。有效 Python 日志的 true/false/null 不变。
- 修复本次实现中的关机问题：选举等待收到 Runtime 的 Stopping 时会早于 ctx 取消返回 `context.Canceled`；现在直接保留 applying，不进入失败/撤销处理。负控能复现晚写 failed，修复后的真实 Runtime 回归通过。
- payload 和 journal 的未来未知字段在 materialization/read-modify-write 中保留；显式 null 与空数组按 Python 的边界输出。
- 静态帮助示例沿用 Python 原文，所有新设备/凭据/域名测试均为合成数据。
- 未加依赖；未运行实机/PVE/生产连接。未改变前端入口，原 Python 操作说明仍适用。

## 自查发现

| 级别 | 已关闭问题 | 证据 |
|---|---|---|
| high | 关机 Stopping 先到、ctx 尚未取消时，将已应用 keepalived 的配对误记 failed | 修复前负控明确失败；`TestStopSignalDuringElectionDoesNotFailAppliedPair` race 通过，重启继续且不重传 |
| high | 损坏的交接 proof 类型经容错解码成 false，可能绕过需要交接的门禁 | `TestMalformedHandoffFlagsCannotBypassVIPGate` 两个子例；拒绝核心写入且保留预留 |
| medium | 读取 affected routers 时未按 `routers.items` 取数据；旧 payload 未来字段可能在 materialization 丢失 | `TestAffectedRouters...`、`TestMaterializationPreserves...` race 通过 |

无已知未关闭的功能发现。独立 HR 审核和实际网络连续性尚未验收。

## Python 覆盖映射

| Python 文件 | 边界与 Go 证据 |
|---|---|
| `test_oxidns_ha.py` | `TestValidationAgainstPython` 的请求/环境/health/listener oracle；开启/双端提交/停用、undo/leftover、错误回复、回环上游、affected routers、全局 Runtime 测试。DHCP/guard/路由器实际写入仍归 R01/R10/R12。 |
| `test_oxidns_ha_publish.py` | 任一成员发起、immutable source 只准备一次、standby 失败/回滚/unknown/健康、staged 激活重读 holder、重复 ID、配对预留/服务锁、rules receipt 强制只重载、A16 双 worker、idle/锁序与恢复。 |
| `test_oxidns_vip_handoff.py` | 两次新鲜确认、VIP DNS 健康门禁、丢 yield/release 应答、清理未确认保留锁、停机/重启、超时及所有权；Flask/Go reconfigure/lost-yield 的真实设备边界对照。 |
| `test_oxidns_ha_docker.py`, `test_oxidns_ha_identity.py`, `test_oxidns_ha_compact_identity.py` | Python 实时能力/容器/来源/host/boot identity 拒绝 oracle，live environment 白名单；Docker 安装脚本本身留在已验收 A09/A10，真实跨宿主网络留 L3。 |
| `test_oxidns_ha_unchanged.py` | 单设备 hooks 保留原路径；A01 原默认历史 HA 读取回归；新旧字段/显式 null/unknown 数据保留；路由器 guard/DHCP 脚本归其原模块。 |
| `test_oxidns_ha_guards.py` | A07 rules 信封、强制停用保留历史、启用中 journal/规则原生修改 guard 的共享 A01/API 复验。 |
| A09 deferred environment case | live environment 与三个协议字段的鉴权、过滤/有界、无额外 payload，Flask/Go 比较。 |

## HR 问题类逐项

| 问题类 | 避免方式 |
|---|---|
| R04/R05 共享客户端与原始请求 | 复用 `Manager.Call`，GET/DELETE 无 JSON null body，Bearer 与 Latin1Headers，(5,15)/(5,30)/(5,90) 原参数断言；测试只把目标路由到 loopback，不缩短生产超时。 |
| 拒绝类型与中文文本 | `Error` → `deploy.Refusal` 链；A07 映射 `HABusyError`；messages.go 集中字符串；正常路径和每类错误与真实 Python 比较。 |
| 缺失身份不能匹配 | journal/payload/operation/child ID 均须非空；交接删除只认自己的 operation；设备 identity 用原值比较（数值语义，8082 与 8082.0 相等），丢 host 会停止。 |
| ID 定向撤销 | 按已尝试设备反序恢复该设备原 spec；首次 enable 失败删除自己的配置；交接 DELETE 携带原 operation_id 并 GET 复核。 |
| read-back 与别名 | 双成员有界处理；已验证快照交 A16 解析；YAML 只沿 Node 读取，无 Clone/展开；监听检查支持 DocumentNode、非映射拒绝与 Unicode 空白。 |
| 断连与关机 | `RunDetached`+L1 try leases；每个提交点检查 owner ctx；真实全局 Runtime 覆盖 accepted upload/yield/两次 keepalived apply/最终 SQLite 提交；断连正控完成一次。 |
| A16/A17 复核遗留 | 发布 options 的 sources 为 RawMessage，未把 []byte 当 JSON 值；复用 A16 snapshot/isolator；A17 发布后签名/草稿仍由其 owner 管；错误/摘要用所有 Agent tokens 脱敏。 |
| 锁序与闲置 | pair/member 多 key 全有或全无；按 DataDir 共享；锁忙先于校验；TestMain 断言 lockx.Violations()==0；IdleRounds 无任务不写入。 |

## 离线快照 A18 定向证明

保持源快照只读、namespace 仅 loopback，复用原 proddiff 的迁移和比较规则。新增 `--a18-proof` 只执行 HA rules/update、generate-config 及日志后果，未运行完整 gate。

命令（`A18_RT_HOST` 为会话指定固定测试目标）：

```sh
RT_HOST="$A18_RT_HOST" rt -- bash -lc 'server/scripts/proddiff.sh --a18-proof'
```

原基线在场时结果：`A18 targeted snapshot: cases=6 new=0 stale=12 remaining=0 self-check=PASS`。
已删除全部 12 条 `deferred → A18`（含 `a18-ha-refusal`），其他基线行未变。负控改动 task.status 可被原比较器检出。F1 已 closed。

## 实机边界

自动化使用 loopback Agent 接收器与模拟 VIP segment，不声称验证真实 keepalived/L2 或 DNS 不间断。
L3 操作清单见 [L3.md](../lab/L3.md)，需独立 HR 复核后再执行。

## 最终运行记录

以下均固定本会话指定的测试服务器，不运行完整 gate。命令中的 `A18_RT_HOST` 代指该固定目标（展开后与实际执行一致，文档不保存真实主机地址）。所有命令从本 worktree 执行；Python 对照启用 `CONFIGFLOW_DIFF=1`。未跑本机测试。

```sh
RT_HOST="$A18_RT_HOST" rt -e CONFIGFLOW_DIFF=1 -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -race -p4 -count=1 -timeout=25m ./internal/agents/dnsha/...'
```

A18 root 644.308s 中仅 `TestHandoffTimeoutReleasesOwnYieldWithoutUpdatingHolder` 失败：fixture 把 VIP DNS 保持 unhealthy，Python 正确行为为 unknown/等待，原测试却要求 failed。已在超时前恢复模拟健康，再验证超时撤销；未改产品实现以迎合该断言。HTTP 12 场景全通过 `ok .../dnsha/difftest 111.886s`，无 race 报告。

```sh
RT_HOST="$A18_RT_HOST" rt -e CONFIGFLOW_DIFF=1 -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -race -count=1 -timeout=5m -run "TestHandoffTimeout|TestMaterializationPreserves|TestAffectedRouters|TestRawClient" ./internal/agents/dnsha; go test -race -count=1 -timeout=8m -run "TestHAHTTPAgainstFlask/(enable|reconfigure|lost-yield)$" ./internal/agents/dnsha/difftest'
```

结果 `ok .../dnsha 40.073s`、`ok .../dnsha/difftest 85.914s`。成功场景新增了交付包各两份、最终配置生成成功的非空断言，以免差分两边同时失败而假通过。

共享回归：`agentfake` race 1.094s、`monitor` race 396.003s 均通过。曾将整包 `internal/agents` 纳入回归，运行 954s 仍停在无关 Python reference 大矩阵，主动终止自有进程；不声称整包通过，以下收窄到实际改动调用方。

最终第一组检查（同一 foreground 命令，`set -euo pipefail; cd server`）：

```sh
RT_HOST="$A18_RT_HOST" rt -e CONFIGFLOW_DIFF=1 -e GOMAXPROCS=4 --back server/internal/agents/dnsha -- bash -lc '
set -euo pipefail
cd server
gofmt -w internal/agents/dnsha
test -z "$(gofmt -l internal/agents/dnsha internal/agents/ha.go internal/agents/messages.go internal/agents/dnsfollow/ha_integration_test.go internal/agents/agentfake/ha.go internal/agents/monitor/monitor.go internal/deploy/orchestrate.go internal/deploy/messages.go internal/api/agents/dnsha.go internal/api/agents/agents.go internal/server/server.go)"
go vet ./...
go test -race -count=1 -timeout=5m -run "TestStopSignalDuringElection|TestRawKeepalivedSpecProtocol" ./internal/agents/dnsha
go test -race -count=1 -timeout=8m -run "TestHistoricalHAPairRead|TestAgentJSONAndDNSHARequestsLikePython|TestRegistrationKeepsManagedFieldsAndPreservesToken|TestProfileRebindingRejectedForEveryUnfinishedDeployment|TestManualRollbackOwnershipAndOperationGuards" ./internal/agents
go test -race -count=1 -timeout=8m -run "TestDNSFollowPublishesBothHAMembers" ./internal/agents/dnsfollow
go test -race -count=1 -timeout=8m -run "Test.*(Generate|Finalize|HA|Pair)|TestShutdown|TestDisconnected|TestDeployHTTP|TestRuleUpdateRoute" ./internal/deploy ./internal/api/agents
go test -race -count=1 -timeout=8m -run "^Test(PanelDifferentialAgainstFlask|UpgradesDifferentialAgainstFlask)$" ./internal/api/agents/difftest
go test -race -count=1 -timeout=3m ./internal/server
go test -count=1 -run "^$" -fuzz "^FuzzHAListener$" -fuzztime=60s -parallel=4 ./internal/agents/dnsha
'
```

格式检查、`go vet ./...` 通过。已执行并通过：dnsha 18.573s、agents 94.871s、dnsfollow 143.046s、deploy 32.632s、api/agents 35.426s。随后 A15 differential 唯一失败是无关演示订阅后台任务的 `notifications.evidence` 时间戳相差一秒，130 个 HTTP/Agent 请求及存储比较均一致。定向重跑确认相同原因；改用 repository 默认配置工厂，保留相同 Agent 种子及 Runtime，避免无关演示订阅任务参与。未隐藏通知字段，也未改产品代码。

A15 原来排除整个 `.oxidns-ha-publications` 目录的 deferred 规则已收窄为仅 `/lock` 文件：O3 规定 Go 使用 L1 lease，无需复制 Python flock。其余 HA payload/journal 继续比较。

强制规则测试初稿错误地要求手动发布直接重新抓取共享库；真实 Python `read_rule_source` 和 Go 均使用调度缓存。现由实际 refresh owner 更新缓存后，验证 URL/源声明不变仍交付新规则字节，真实 Python `prepare_candidate(force_refresh=True)` 同时证明相同结果。这里没有修改产品的缓存语义。

关机负控：修复前 `TestStopSignalDuringElectionDoesNotFailAppliedPair` 1.82s 报 `stop signal wrote a late failed journal`；修复后包括原始协议测试在上述 race 中通过。


测试说明：初次 race 的 A18 fixture 不必要地加载了首次启动演示模板，导致每次 journal 保存都复制大量无关规则，累计超时；未视为功能缺陷。已改用真实 repository 的默认配置工厂与最小合成数据，完整 HTTP 成功生成另有真实 Flask/Go 和离线快照证明。生产 HTTP 超时、包大小、解析预算均未缩短或覆盖。后续定向运行的完整日志保留于测试服务器的本任务缓存目录，文档仅记录合成用例与汇总。


最终补充检查（同一固定目标；未运行全量门禁）：

```sh
RT_HOST="$A18_RT_HOST" rt -e CONFIGFLOW_DIFF=1 -e GOMAXPROCS=4 --back server/internal/agents/dnsha/forced_rules_test.go --back server/internal/api/agents/difftest/upgrades_diff_test.go -- bash -lc '
set -euo pipefail
cd server
gofmt -w internal/agents/dnsha/forced_rules_test.go internal/api/agents/difftest/upgrades_diff_test.go
go vet ./...
go test -race -count=1 -timeout=5m -run "^TestManualPairRulesForcesUnchangedLibraryMaterialization$" ./internal/agents/dnsha
go test -race -count=1 -timeout=8m -run "^Test(PanelDifferentialAgainstFlask|UpgradesDifferentialAgainstFlask)$" ./internal/api/agents/difftest > "$HOME/.cache/a18-callers-last.log" 2>&1 || { rg -A4 "DIFF|differences|^ok|^FAIL|^--- FAIL" "$HOME/.cache/a18-callers-last.log"; exit 1; }
tail -1 "$HOME/.cache/a18-callers-last.log"
go test -race -count=1 -timeout=3m ./internal/server
go test -count=1 -run "^$" -fuzz "^FuzzHAListener$" -fuzztime=60s -parallel=4 ./internal/agents/dnsha
'
```

补充结果：强制规则 materialization（含真实 Python 校验已交付基线归档）race `ok .../dnsha 61.057s`。

A15 在清除无关订阅后进一步确认仅剩对象字段顺序差异：旧 helper 比较原始 JSON 字符串，N8 native 存储重排键后误报。现将既有时间掩码后的 JSON 解析并排序，字段和值全部继续严格比较。Panel differential 在两次组合运行中均未报失败；仅重跑修正的 A15，不重复其他已通过用例。

最后收窄命令：

```sh
RT_HOST="$A18_RT_HOST" rt -e CONFIGFLOW_DIFF=1 -e GOMAXPROCS=4 --back server/internal/api/agents/difftest/upgrades_diff_test.go -- bash -lc '
set -euo pipefail
cd server
gofmt -w internal/api/agents/difftest/upgrades_diff_test.go
go test -race -count=1 -timeout=8m -run "^TestUpgradesDifferentialAgainstFlask$" ./internal/api/agents/difftest > "$HOME/.cache/a18-upgrades-final.log" 2>&1 || { rg -A4 "DIFF|differences|^ok|^FAIL|^--- FAIL" "$HOME/.cache/a18-upgrades-final.log"; exit 1; }
tail -1 "$HOME/.cache/a18-upgrades-final.log"
go test -race -count=1 -timeout=3m ./internal/server
go test -count=1 -run "^$" -fuzz "^FuzzHAListener$" -fuzztime=60s -parallel=4 ./internal/agents/dnsha
test -z "$(gofmt -l internal/agents/dnsha internal/agents/dnsfollow/ha_integration_test.go internal/api/agents/difftest/upgrades_diff_test.go)"
'
```

最后收窄结果：A15 differential（130 请求、Agent trace、存储、通知、DATA_DIR）race `ok .../api/agents/difftest 31.027s`；启动接线 race `ok .../server 6.317s`。

Fuzz 最后输出：`fuzz: elapsed: 1m0s, execs: 160037 ... total: 265`，`PASS`，`ok .../dnsha 61.018s`。末尾 gofmt 断言发现 `publication.go` 排版待更新（此前测试均已通过）；在服务器执行最终统一格式化与 vet，不重复语义未改变的测试：

```sh
RT_HOST="$A18_RT_HOST" rt --back server/internal/agents/dnsha --back server/internal/api/agents/difftest/upgrades_diff_test.go -- bash -lc '
set -euo pipefail
cd server
gofmt -l internal/agents/dnsha internal/agents/dnsfollow/ha_integration_test.go internal/api/agents/difftest/upgrades_diff_test.go
gofmt -w internal/agents/dnsha internal/agents/dnsfollow/ha_integration_test.go internal/api/agents/difftest/upgrades_diff_test.go
test -z "$(gofmt -l internal/agents/dnsha internal/agents/dnsfollow/ha_integration_test.go internal/api/agents/difftest/upgrades_diff_test.go)"
go vet ./...
echo "A18 final formatting and vet: PASS"
'
```

最终格式化后 `gofmt -l` 无输出，`go vet ./...` 通过，尾行 `A18 final formatting and vet: PASS`。未运行完整 gate，后续由协调者在合并树执行。
