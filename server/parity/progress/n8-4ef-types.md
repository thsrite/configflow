# N8-4e/f — HR stored model types

状态：N8-4e 实现中，N8-4f 待开始；分支 `go-n8-4ef`，从 `go@ae2e0f59` 创建。
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
| DNSFollowBinding/State/Job | agents/dns_follow.py、manager.py；candidate_settings 是完整 OxiDNS Dynamic；binding ID/签名/部署指针均不重新计算；配置/重试的替换/删除行为留给消费者。 |
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
