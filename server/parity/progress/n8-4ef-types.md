# N8-4e/f — HR stored model types

状态：N8-4e/f 均已 ported，两轮完整门禁通过，待 Opus HR 复核；分支 `go-n8-4ef`，从 `go@ae2e0f59` 创建。
仅类型和编解码；不迁移业务调用方，不改变文件 I/O、路径、归档或身份算法。无新依赖。

## N8-4e 字段来源与决定

所有路径相对 backend/。每条记录独立 Fields + raw Raw，沿用 N8-3 编解码。

| 类型 | Python 读写来源与边界 |
|---|---|
| Agent | agents/manager.py（注册/重新注册/更新/部署历史写入/心跳持久化版本）、runtime_state.py（运行时覆盖）、address_follow.py（更新 host）、traffic_history.py（enabled/since）、gateway_installation.py 和 docker_ha_enrollment.py（安装身份）；routes/agents.py、routes/tasks.py；common/config_repository.py（历史过滤、复制、导入导出、重新绑定）。不新增持久化 heartbeat/metrics 写入。has_update 等派生遗留成员由 Fields 保留。 |
| AgentMetrics/CPU/Memory/Disk/Network | runtime_state.py RUNTIME_FIELDS 与 go-agent/metrics.go 固定协议字段；保留历史内嵌样本，外部 metrics 数据库不纳入。整数计数用 Int，百分比用 float64。 |
| LegacyDNSEndpoint | agents/node_test_dns_cleanup.py 仍读取 endpoint/profile_id/status/installed/plugins；plugins 是开放 OxiDNS 子树。只保存，不运行迁移。 |
| Deployment、OperationEvent、DeploymentDNSCheck | agents/manager.py 所有 begin/record/get/push/upload/rollback 分支、operation_events.py、diagnostics.py、deployment_messages.py、deployment_locations.py；错误详情是字符串，health 无固定结构而用 RawMessage。submitted 是提交前记录；_preparation_running 是内存状态，不存储；不复制远端 was_running/backup 等 Agent 自有 journal。events 保留原顺序，不截断、不归一化。 |
| PreparedContent、AppliedContent | agents/manager.py、deployment_bundle.py、deployment_locations.py、mihomo_resources.py、generated_preview.py、config_generator.py、rule_sync_store.advance_content；files 是有序摘要 Map，可被历史裁剪；applied 仅由业务确认后提升，codec 不推断状态。 |
| DeploymentFile/Manifest、OxiDNSValidationReceipt、AgentRuleSyncReceipt | agents/deployment_bundle.py、rule_sync.py（reload_only）、oxidns_validation_receipt.py、rule_sync_store.py、rule_sync_snapshot.py；固定外部 receipt envelope 纳入。source 是去除 agents 后的捕获配置，用 RawMessage，避免递归；manifest 与 deploy DTO 独立。归档字节与 SHA 不变，不重新打包。 |
| AgentUpgrade、UpgradeArtifact | agents/upgrades.py（_save、提交与轮询/恢复）、manager.py；按 update_id 保留所有权，started_at/updated_at/recovery_reported_at 为浮点 epoch，agent_updated_at 为字符串；没有新增 upgrade 历史 map。 |
| IPv6VerificationSettings/Verification/Check | agents/ipv6_verification.py、ipv6_readiness.py；保留 nullable config_revision/deployment_id/client_fingerprint、epoch 与字符串时间和所有 scope/check 内容。7 条保留上限、过期/恢复逻辑不在 codec。 |
| RuleSyncSettings、AgentRuleSync、RuleSyncState | agents/rule_sync.py、rule_sync_projection.py、rule_sync_snapshot.py、rule_sync_store.py、common/rule_dns.py、routes/rules.py、routes/agents.py、routes/tasks.py、app.py；native_task 是字符串任务 ID。key 是已存身份，noticed_at/retry_at 是浮点 epoch。 |
| DNSFollowBinding/State/Job | agents/dns_follow.py、manager.py；candidate_settings 是完整 OxiDNS Dynamic；binding ID/签名/部署指针均不重新计算；首次绑定的 after_deployment_id 可由 Python 写成 null，用 *string，另有 Python null 回归样例；配置/重试的替换/删除行为留给消费者。 |
| HASettings、HAState、HAIdentity、HAEnable、HAJournal、HAHandoff/Failure、HARouter、HAMember、HAPublicationSummary | agents/oxidns_ha.py、oxidns_ha_publish.py、oxidns_ha_handoff.py、idle_rounds.py、manager.py，以及 routers/dhcp_dns.py、rule_sync.py 的消费者；HAState.members 是 leftover 清理用 ID 列表，与 journal.members 对象列表分开。handoff_required 用指针保留 presence；enable/active_agent_id 原 null 保留；摘要不是完整 journal，不引入递归 Agent 图。 |
| HABundle、HAPayload | agents/oxidns_ha_publish.py _write_payload/_read_payload 所有调用；外部固定 envelope 纳入，source_snapshot/resource_config 用 RawMessage；无文件 I/O/路径变更。 |

A11/A12/A13/A16–18：现有 agents.Record、deploy.* 与 routers.* DTO 不迁移，model 不导入这些包。
A12 diff、A13 公共投影/缓存与 preview、安装记录不纳入。D2 身份迁移属于各消费者；
codec 不计算 fingerprint/digest/SHA。A11 v1 内容 fingerprint 的明确兼容合约保留。
D-A17-1 已裁定 B（切换后一次重新绑定），D-A18-1 已裁定 A（忙锁下一轮重试），本步不改变这些行为。

## 测试与覆盖

每个新增 record 加入 storedRecordTypes：逐字段 no-op 字节相同、只改目标成员、错误类型 R5、
错误片段原样保留、指针删除、Python JSON 写入→Go 写回→Python 读取；各类型加入 FuzzStoredTypes 种子。
修正测试夹具的根成员定位，避免同名嵌套字段让错误注入落在子记录上；基础 codec 无改动。
hr_interchange.py 另调用真实 Python append_event/_record_leftover，用合成数据验证 HA leftover IDs、
running native task、未提交候选、HA handoff 等缺失快照形状；不连接设备。

快照：仅测试服务器 `~/cf-prod-snapshot/20261116/data/`。SQLite mode=ro backup 到 0700 测试目录，
读取私有副本的原始字段 bytes；外部 JSON 仅只读复制到私有 rows。每个实际嵌套记录独立往返和编辑，
再做 Python 双向。只输出 schema/计数，所有私有数据自动删除，不回传/提交生产数据。
真实覆盖与合成覆盖最终分列补充；无 Python 功能差异。

### N8-4e 已完成（提交前检查）

39 个新增 record；34 个在真实 SQLite/外部文件有记录，以下 5 个没有真实对象：
LegacyDNSEndpoint、IPv6VerificationSettings、DNSFollowJob、HAHandoff、HAHandoffFailure。
5 个均有逐字段合成/Python 双向/fuzz 种子覆盖；DNSFollowJob/HAHandoff 另有 Python HR 场景。

真实覆盖（含 latest 副本，不能和独立历史数混用）：Agent 3；Deployment 64；PreparedContent 63；
AppliedContent 3；OxiDNSValidationReceipt 32；AgentUpgrade 3/UpgradeArtifact 9；OperationEvent 887；
DeploymentDNSCheck 308；IPv6Verification 7/Check 301；AgentRuleSync 3/Settings 5/State 2；
DNSFollowBinding/State 各 1；HAJournal/HASettings/HAState/HAEnable 各 2，HAIdentity 4；
外部 AgentRuleSyncReceipt 7、HAPayload/HABundle 各 1，DeploymentManifest 8/File 303。
输出含 schema/counts，不含值；源快照未修改，私有副本已随测试清理。

`.64` 检查：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/model/agent*.go internal/model/types_test.go internal/model/hr_test.go && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data go test -count=1 -v ./internal/model > /tmp/n84e-model.log && go test -p=4 -run "^$" ./... > /tmp/n84e-compile.log'
```

model 全部通过（3.470 s；最后重跑含外部嵌套检查也通过）；全包编译通过。
repo/model_roundtrip_test.go 仅把 Agent 测试夹具写入改为类型化解码，无业务调用方迁移。
最终 race/difftest/proddiff/fuzz 门禁在 N8-4f 后统一跑。

## N8-4f 字段来源与决定

25 个新增 record，profile.routers 为 Routers{Items []Router}。

| 类型 | Python 来源与决定 |
|---|---|
| Routers、Router、RouterDNSGuard、RouterPDGuard | routers/store.py EDITABLE_FIELDS/normalize/public_view，routes/routers.py 的创建/编辑/探测/发布，common/config_repository.py ROUTER_IDENTITY_FIELDS/CONNECTION_FIELDS/MANIFEST_FIELDS/SaveRouterPublication/导入导出；routers/dhcp_dns.py/desired.py 消费。verify_tls、failover、enabled 等用指针，加载不填默认、不规范化重叠规则集；has_password 只在公开视图。 |
| RouterProbe、RouterRelease | routes/routers.py + routers/base.py RouterInfo；探测成功 vendor/version/model/capabilities，失败 message；release 的 at/ok/fingerprint/failed_stage/message。没有新增持久化 release running 标志。 |
| RouterResource、ROSPolicyState/Spec | routers/ros_policy.py、ros_policy_write.py、ros_policy_view.py、rule_sync.py/rule_sync_write.py；spec.sources/destinations/gateway/naming_version/policy_version；resource.path/id/config。ROSPolicyState 无顶层 version。Config 必须保留完整稳定厂商配置，含未知属性/复合值，用 Dynamic。 |
| DNSGuardState/Spec/Resource | routers/dns_guard.py 所有 build/apply/verify/restore 分支；version/spec/resources；role/path/id/config 与 agent/interface/network/bypass/block_dot/failover/exempt。routers/forward_hint.py 是只读提示，不引入持久化 proof 或 fingerprint 成员。 |
| IPv6FakeIPState/Spec、IPv6RoutesState | routers/ipv6_fakeip.py、ipv6_routes.py 的 build/apply/_remapped/_rebuild/verify；fakeIP spec.cidr/gateway，routes 顶层 gateway，resources 使用完整 RouterResource；不做 CIDR 规范化。 |
| IPv6PDGuardState/Spec、PDGuardResource | routers/ipv6_pd_guard.py，version/spec/resources，spec.lan/pool/client/client_interface/hold；resource 只有 role/path/id，没有 config。沿用手工安装与自有对象的区别留给业务，不自动认领。 |
| RouterRuleSyncReceipt、RouterRuleSyncBaseline、RouterSyncLog/Rule/Desired/CIDR/NAT | routers/rule_sync.py、rule_sync_store.py、rule_sync_rules.py、rule_sync_write.py、routeros_nat_list.nat_resources；顶层状态/时间/计数/log/rules/baseline；baseline.control/rules/desired/static_resources/policy/nat_list；rule 包含 classification 的 version/IPv4/IPv6/counts/name/source_type/library_id；desired 兼容初始 compute_desired 全字段与后续缩减字段；NAT 是 rule/src 完整资源证明。外部 envelope 不进入 SQLite/export；路径和 digest 算法不变。 |

衔接：R04 review F7 要求完整复合配置值，F9 要求缺失 proof 拒绝；R05 不按同名/无 ID 对象
自动认领；R06 的手工编辑 evidence/revision 不成为配置 manifest；R07 NAT plan/evidence 不新增
第六种 Router manifest；R08/R09 未移植，类型不借用 pending DTO，也不改共享 operation 类型。
R12 Manifest.Set 为运行时意图；SaveRouterPublication 在持有 job lock 下才可替换/清除，失败或回滚
后仍保存实际 ownership，此处 codec 只保留字节，绝不把 R5 回退空值当已验证无资源。

按协调裁定，在 n7-n8.md §2.3 与 r12-release.md “Clarified in N8-4f” 写明：
不扩展基础 codec 的缺失/null/清除 API；读到的 nil 不代表清除；原 null 再赋 nil 仍为 null；
N8-5/R12 显式携带写入意图；设备操作前必须完整验证 ownership，缺失或格式错就拒绝。

### N8-4f 首轮验证

`.64` model（含 Python 与私有快照）通过，3.862 s；全包编译通过。
每个新类型都有独立逐字段字节往返、单成员编辑、R5、Python 双向和 fuzz 种子。
HR Python 夹具调用真实 _resource/config_row 和 classify 产生完整资源与分类基线，
另覆盖五种 manifest、复合 Dynamic config、PD ID-only 证明、null 和无/false presence。
这只是存储协议测试，不声称验证设备状态机或已经移植 R08/R09/R12。

快照覆盖：25 个 Router 新 record 中 9 个有真实记录：Routers=2、Router=1、RouterDNSGuard=1、
RouterProbe=1、RouterRelease=1、DNSGuardState/Spec 各 1、DNSGuardResource=10、外部 RouterRuleSyncReceipt=1。
其余 16 个仅合成/Python 双向覆盖：RouterPDGuard、RouterResource、ROSPolicyState/Spec、
IPv6FakeIPState/Spec、IPv6RoutesState、IPv6PDGuardState/Spec、PDGuardResource、RouterSyncLog/Rule、
RouterRuleSyncBaseline、RouterSyncDesired、RouterCIDR、RouterSyncNAT。
真实 router receipt 仅状态元数据，没有 baseline，不能声称有真实有效 baseline 往返覆盖。

完整门禁结果见下文；真实快照与合成覆盖分别列出，不扩大覆盖声明。

## 完整门禁（首次最终运行）

2026-11-18，`.64`，`/tmp/gate.gj2ylx`：**GATE PASS**。基点 `go@8c5d2835`。
39+25=64 个新增 record、305+123=428 个声明字段；没有新增业务调用或身份算法。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$PWD CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data; server/scripts/gate.sh -p 4 FuzzRecordRoundTrip:./internal/model FuzzDocumentRoundTrip:./internal/model FuzzReviewRecordEdits:./internal/model FuzzStoredTypes:./internal/model FuzzRawDocument:./internal/store FuzzConvertMosDNSRules:./internal/deploy FuzzSourceExcerpt:./internal/deploy FuzzOpenArchive:./internal/deploy FuzzConfig:./internal/deploy/pushdiff FuzzRuleEntries:./internal/deploy/pushdiff FuzzNodeItems:./internal/deploy/pushdiff FuzzCredentialSpans:./internal/deploy/pushdiff'
```

- gofmt -l internal/ tools/ cmd/ 无输出；go vet ./... 通过。
- 两个 build tags（configflow_proddiff/configflow_difftest）全包 build、lifecycle/cmd vet、lifecycle tests 通过。
- 全量 -race -p=4 -count=1：84 个有测试的包全部通过；model 69.375 s，repo 328.797 s。
  按新版 AGENT-BRIEF，全量 race 已运行全部测试，不另跑重复的非 race 全量。
- 12 个 difftest 包全部通过（CONFIGFLOW_DIFF=1）。
- 上述 12 个 fuzz 目标各 60 秒通过，实际各约 61 秒。
- proddiff：2831 cases，462 comparisons，11 既有 unique differences，new=0 stale=0。
- TestTypesPrivateSnapshot/TestStoredRoundTrip 剩余私有测试文件计数均为 0。

最后输出：

```text
FuzzOpenArchive ok configflow/internal/deploy 61.127s
FuzzConfig ok configflow/internal/deploy/pushdiff 61.126s
FuzzRuleEntries ok configflow/internal/deploy/pushdiff 61.152s
FuzzNodeItems ok configflow/internal/deploy/pushdiff 61.088s
FuzzCredentialSpans ok configflow/internal/deploy/pushdiff 61.101s
GATE PASS
```

此前 `/tmp/gate.GdAo2M` 在可空 after_deployment_id 修正前启动，proddiff 已通过但整轮主动停止，
不算通过；修正另在 `.32` model/Python/私有快照通过（18.605 s），并已包含于以上 .64 完整通过。

门禁期间 go 合入 O3-5 运行时代码，随后 rebase 并重新跑整合门禁，结果见末节。

### Dynamic 编辑夹具补强（仅测试与包注释）

复查发现旧的 typeJSON(Dynamic, changed) 对两个 changed 值生成了同一个对象，不能证明开放
resource.config 等成员的编辑。因此让 changed=true 生成不同的树，并新增“目标成员 bytes 必须
实际改变”的断言；所有 90 个业务 record（含 a–d）的逐字段编辑都受此断言约束。
没有更改任何运行时声明或 codec。包注释也明确包含固定外部 envelope，并说明不验证 ownership。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$PWD/.. CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data go test -race -count=1 ./internal/model && go test -run "^$" -fuzz "^FuzzStoredTypes$" -fuzztime=60s -parallel=2 ./internal/model'
```

`ok configflow/internal/model 67.793s`；加强后的 FuzzStoredTypes 60 秒通过（60.600s）。
此增量仅测改动的 model 包，不在 gate 外另跑全量 race/proddiff。

整合复验在 `.64` 的 `/tmp/gate.Y5dzX7` 运行，基点 go@9a208c6c；结果如下。


## 最终整合验收与交回

2026-11-18，`.64` `/tmp/gate.Y5dzX7`：**GATE PASS**，命令与上述完整 gate 命令相同（-p 4，12 个 fuzz 各 60 秒）。

- gofmt 无输出、go vet ./...、两个 build tags 的 build/vet/lifecycle tests 全部通过。
- 全量 -race -p=4 -count=1：**86 个有测试的包全部通过**；model **33.514 s**，repo **378.782 s**。
  此轮覆盖 rebase 后的 O3-5 运行时代码；按 AGENT-BRIEF 不再重复跑非 race 全量。
- **12 个 difftest 包全部通过**，包括 Agent/aggregation/subscription 的 Python 边界。
- **12 个 fuzz 各 60 秒全部通过**，包含 4 个 model 目标和 store/deploy/pushdiff。
- proddiff：**2831 cases、462 comparisons、11 既有 differences、new=0 stale=0**。
- 两类私有快照测试剩余文件计数均为 **0**；未取回或提交任何生产数据。

最后输出：

```text
FuzzStoredTypes ok configflow/internal/model 61.050s
FuzzRawDocument ok configflow/internal/store 61.062s
FuzzConvertMosDNSRules ok configflow/internal/deploy 61.170s
FuzzSourceExcerpt ok configflow/internal/deploy 61.084s
FuzzOpenArchive ok configflow/internal/deploy 61.054s
FuzzConfig ok configflow/internal/deploy/pushdiff 61.144s
FuzzRuleEntries ok configflow/internal/deploy/pushdiff 61.108s
FuzzNodeItems ok configflow/internal/deploy/pushdiff 61.228s
FuzzCredentialSpans ok configflow/internal/deploy/pushdiff 61.096s
GATE PASS
```

最终 rebase：`go@4d8a665a`。从门禁基点 9a208c6c 到该基点仅增加 r12-release.md 文档，
无运行时代码差异；已同时保留新的 lifecycle.Detach 约定及 Clarified in N8-4f。
门禁运行副本与最终分支的本任务差异仅为上述 Dynamic 测试补强、包注释和验收报告；
测试补强已在 .32 单包 race/Python/私有快照和 60 秒 fuzz 验证，运行时类型/codec 完全相同。

分支 `go-n8-4ef`，分步提交：N8-4e `d3d51e4b`；N8-4f 为本提交。
无待修复问题，无已知 Python 功能差异，无新依赖；未迁移调用方、未 push，等待 Opus HR 复核。
已修正的建模/测试发现：HA leftover members 是 ID 列表；首次 follow 的 after_deployment_id 可 null；
R5 注入必须定位根成员；Dynamic 的修改夹具必须真正改变值。ownership 的有效性仍由消费者验证。
