# N8-4a–d — typed stored documents

本文记录 a–d 交付时的范围和验证。后续 Agent/Router HR 类型、字段来源、快照与合成覆盖、
衔接裁定和门禁记录见 [n8-4ef-types.md](n8-4ef-types.md)。

状态：**ported，最终门禁全部 PASS，待独立复核**；分支 `go-n8-4`，基于本地 `go`，不 push。
范围仅 model 类型、编解码和对应测试；N8-4e/f（Agents、Routers）保持 RawMessage，
repo 业务调用方迁移留给 N8-5。无新依赖，无 Python 功能差异。

## 分步提交

| 步骤 | 提交 | 类型文件 |
|---|---|---|
| N8-4a | 68392670 | resources.go |
| N8-4b | 617bb6ff | policies.go |
| N8-4c | 83eb9234 | settings.go、textlines.go |
| N8-4d | 本提交（包含最终验收记录） | system.go |

最终 rebase 基点：`go` 038b52ae。最终门禁运行树基于 d7c481c7；最后 rebase 只多 lab/L1.md 文档，全部 Go/Python 运行代码和测试字节相同，结果适用。

## 字段来源（非测试 Python 读写、创建/更新、迁移、导入与生成器）

所有路径相对 `backend/`。未知成员通过每条记录的 `Fields` 和 `raw Raw` 原位保留；
存储数值使用 `Int`，非零默认值和需要删除的可选成员用指针，用户键映射用 `Map`。
加载不填默认、不做业务规范化，编解码沿用 N8-3 R1–R9。

| 类型 | 字段依据与决定 |
|---|---|
| Node | routes/nodes.py、routes/subscriptions.py、routes/proxy_groups.py（protocol 后备值）、utils/subscription_parser.py、utils/sub_store_client.py、utils/config_importer.py、utils/dialer_references.py、converters/{mihomo,surge,loon}.py；remark 来自真实行；params 是 Python 整体展开的开放 Mihomo 引擎子树，用 Dynamic；dialer_ref 用 ResourceRef。来源展示元数据、临时代理链标记不成为存储字段。 |
| Subscription、Aggregation | routes/{subscriptions,aggregations}.py、utils/subscription_{filter,refresh,hosts}.py、utils/config_importer.py、utils/health_check.py；health_check 用 ProviderHealthCheck，旧 health_check_url 保留；流量/缓存/节点统计响应字段排除。 |
| RuleLibraryItem | routes/rule_library.py、utils/{rule_utils,scheduled_refresh,config_importer}.py、common/{config_repository,setup_presets}.py；来源类型、behavior、format、update_interval 可选，保留缺省。 |
| ProxyGroup、ProxyChain | routes/proxy_groups.py、utils/{dialer_references,proxy_chains,group_candidates,health_check,config_importer}.py、converters/{mihomo,surge,loon}.py、common/config_repository.py；核对 MIHOMO_GROUP_PASSTHROUGH 和生成器 key 循环，包含 max-failed-times、disable-udp、hidden、icon、exclude-filter、exclude-type；可删除来源列表用指针，健康字段指针支持 delete_fields。链仅有 entry/exit 稳定引用，不引入递归存储类型；proxies_order 共用 ResourceRef。 |
| RuleConfig | routes/rules.py、common/{config_repository,rule_dns}.py、converters/{common,mihomo,surge,loon,mosdns}.py、utils/config_importer.py；包括规则/规则集、MATCH UDP/NTP、legacy/hydrate 来源。快照中的 groupId/groupLabel/groupPolicy/isExpandedGroupItem/isFirstInGroup/isLastInGroup/uniqueId 编辑器元数据按原大小写类型化。 |
| MihomoSettings、EasyTierSettings/Keepalive/Options | routes/config.py、common/{config,config_repository,mihomo_easytier}.py、converters/mihomo.py；EasyTierOptions 覆盖全部 34 个 FIELDS 选项。custom_config 是存储的可编辑 YAML 文本，保留字符串，包括无效草稿；消费者用 yamlx 创建 Node，本层不解析。 |
| SurgeSettings/SurgeSmartGroup、LoonSettings | routes/config.py、common/config_repository.py、converters/{surge,loon}.py；保留客户端文本和 smart_groups 的 group_id/policy_priority。 |
| MosDNSSettings/MosDNSMatch、TextLines | routes/mosdns.py、common/{config,config_repository}.py、converters/mosdns.py；matches 的 string/list 用 TextLines 明确联合表示，并保留 null；旧 api_addr 显式可选。读/写端地址默认不一致，本步不抹平默认。 |
| DomainDiscovery、DiscoveryHistory/Recheck | utils/domain_discovery_service.py 的 _SETTING_DEFAULTS、history 写入和 recheck 写入；regional_rulesets、website_notes 为 Map[string]，confidence/verdict/recheck 可为 null。 |
| DeviceNotes、ClientFixedIPs/ClientFixedIP | common/{device_notes,client_fixed_ip}.py；DeviceNotes 实际值是字符串，故用 Map[string]；ClientFixedIP 按 BINDING_FIELDS 定义，映射键为 IPv4 文本，不规范化或改写归属标记。 |
| OxiDNS | 按任务要求保持 Dynamic。 |
| SystemConfig、NodeTestDNSConfig/ManagedDNSBinding | common/{config,config_repository,auth,config_export,setup_flow,setup_abandon}.py、routes/settings.py、utils/{node_dns,sub_store_runtime,sub_store_client,rule_fetch}.py、agents/{node_test_dns_cleanup,metrics_flush,traffic_history}.py；sub_store_url 当前改由环境变量指定，但快照仍有存储字段，保留类型；managed 已不能新保存，但启动清理读取 endpoint_id，本层保留、不迁移。 |
| Backup | routes/settings.py、utils/webdav_backup.py 的文本字段和 RETENTION_LIMITS；密码在 model 原样保存，脱敏属于 HTTP。 |

## 快照覆盖与隔离

仅测试服务器只读打开 `~/cf-prod-snapshot/20261116/data/configflow.sqlite3`，通过 SQLite
backup 创建私有副本；只在副本读取，原始 value 文本直接进入类型测试。
私有源/期望/输出位于测试自己的 0700 目录，结束自动删除；副本 SQLite 在导出行后即删除。
无生产值、地址/用户键或数据文件提交/取回本机，输出只含 schema 名、类型和计数。

- Node=5，Subscription=2，Aggregation=1，RuleLibraryItem=44，ProviderHealthCheck=3。
- ProxyGroup=60，RuleConfig=94，ResourceRef=256（排序引用）。
- Mihomo/Surge/Loon/MosDNS/DomainDiscovery 各 2 条；EasyTier/Options/Keepalive 各 2，
  MosDNSMatch=4，DiscoveryHistory=10，DiscoveryRecheck=6。
- DeviceNotes=2 行、14 个名字；ClientFixedIPs=1 个真实空对象。
- SystemConfig/Backup/NodeTestDNSConfig/ManagedDNSBinding 各 1 条，managed 是空对象。
- 26 个新增 record 中 23 个有真实记录（包含空 managed）；ClientFixedIP、ProxyChain、
  SurgeSmartGroup 无真实条目，仅有空容器或字段未出现，用合成/Python 双向覆盖，
  不声称有真实 binding/chain/smart-group 覆盖。

## 验证边界

`internal/model/types_test.go`：26 个 record、260 个声明字段，每种嵌套类型独立检查，
避免父记录 Raw 掩盖子类型缺陷。原始未改字节、逐字段修改值的独立 JSON 语义断言、
其他成员逐字节不变、R5 类型错误/无值诊断及原片段保留、指针置 nil 删除。
Dynamic 是任意合法 JSON 子树，不对其合法 JSON 形状报类型错；TextLines 另测文本/列表/null。
快照每个实际嵌套记录再次独立 no-op 和修改，输出分类计数；再用 Python json 写出、Go
读取写回、Python 读取做结构比较。所有 record 均加入 `FuzzStoredTypes` 种子。

`internal/repo/model_roundtrip_test.go`：仅 test-only 桥适配具体类型；保留真实 Python
ConfigStore/ProfileRepository 双向检查。`TestStoredRoundTrip` 继续验证原始 SQLite 43 行、
2 profile、no-op 不提交、单成员编辑、未知字段、R5、generation/fingerprint 和仅触及行的 revision。
始终运行合成 fixture（12 行）及完整 Go repo（27 行），最终环境还运行 Python 写入 fixture。

Python 测试覆盖：本步不迁移业务函数或逐例翻译 Python 测试；原有 repo/store 边界测试继续
覆盖 test_config_store.py、test_config_repository.py、test_config_repository_cache.py、
test_config_storage_migration.py、test_config_import.py；功能差异由全部 difftest/proddiff 兜底。
没有跳过任何已有用例、采样真实行、修改基线或绕过加载校验。
N8-3 已记录的 legacy 加载校验限制仍原样保留，由 N8-5 分离加载与写入验证。

## 最终门禁（全部 PASS）

最终树包含 O3/R07/C31/N7-3a/推送差异的 go 合并更新，不以旧基点的预检代替。
命令在前台跑，全部检查退出码；未修改任何基线、采样、超时断言或用例集合。

| 门禁 | 服务器 | 结果 |
|---|---|---|
| gofmt -l internal/ tools/ cmd/ | .32 | 无输出 |
| go vet ./... | .64 | PASS |
| model/repo -race | .64 | 3.406s / 272.770s，PASS |
| 原始行与 Python↔Go | .64 | model/repo 均 PASS；repo 11.855s；真实 43 行、2 profile |
| 全量 go test -count=1 ./... | .32 | **82 个包 ok，0 FAIL**；model 0.414s、repo 41.167s、installer 161.255s、oxidns 70.661s、routers 189.834s |
| 全部 difftest | .32 | **12/12 PASS**（含新合入 api/aggregations 与 devicenotes） |
| FuzzRecordRoundTrip | .32 | 60s，4,038,758 执行，PASS（总 60.082s） |
| FuzzDocumentRoundTrip | .32 | 60s，4,033,462 执行，PASS（总 60.343s） |
| FuzzReviewRecordEdits | .32 | 60s，3,713,085 执行，PASS（总 61.015s） |
| FuzzStoredTypes（新增，26 类型种子） | .32 | 60s，2,464,732 执行，PASS（总 61.012s） |
| scripts/proddiff.sh | .32 | **new=0 stale=0**，23 self-check PASS、2831 cases、435 comparisons；11 类既有差异不变，coverage_regressions=0、unmapped_routes=0 |
| 私有副本清理 | 两台 | n8-3-roundtrip-* 残留均 0；types 的 t.TempDir 自动清理，无数据取回 |

最终日志：.64 `/tmp/n84-final-race.fNjqSY/`；.32 `/tmp/n84-final-suite.k3b25C/`。
最终尾部输出摘录（计数/路径，不含存储值）：

```text
ok  configflow/internal/model  3.406s
ok  configflow/internal/repo   272.770s
rows=43 profiles=2: byte/no-op, typed edits, unknowns/R5 and Python↔Go passed
ok  configflow/internal/repo   11.855s
FINAL_RACE_PASS logs=/tmp/n84-final-race.fNjqSY

ok  configflow/internal/yamlx             1.298s
ok  configflow/internal/yamlx/yamlxtest   0.002s
ok  configflow/tools/proddiff/generator  0.010s
PASS full
ok  configflow/internal/webdav/difftest  22.214s
PASS difftest
PASS FuzzStoredTypes
baseline: new=0 stale=0
PASS proddiff
0
FINAL_SUITE_PASS logs=/tmp/n84-final-suite.k3b25C
```

精确命令（实际分阶段检查退出码、记录日志并打印 tail）：

```sh
# 从本 worktree 发起；两台服务器均设置以下环境后 cd server。
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc '
set -euo pipefail; umask 0022; unset TMPDIR
export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$PWD CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data
cd server
go vet ./...
go test -race -p=2 -count=1 -timeout=30m ./internal/model ./internal/repo
go test -v -count=1 -run "^TestStoredRoundTrip$|^TestTypesPythonRoundTrip$|^TestTypesPrivateSnapshot$" ./internal/model ./internal/repo
'

RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '
set -euo pipefail; umask 0022; unset TMPDIR
export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$PWD CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data
cd server
out=$(gofmt -l internal/ tools/ cmd/); test -z "$out"
go test -p=2 -count=1 -timeout=30m ./...
CONFIGFLOW_DIFF=1 go test -p=2 -count=1 -timeout=40m $(go list ./... | rg difftest)
for f in FuzzRecordRoundTrip FuzzDocumentRoundTrip FuzzReviewRecordEdits FuzzStoredTypes; do
  go test -run "^$" -fuzz "^$f\$" -fuzztime=60s -parallel=4 ./internal/model
done
scripts/proddiff.sh
'
```

## 交回

已完成 N8-4a、b、c、d，每步一个提交；不 push，不启动子 agent，不修改生产或 backend/frontend。
本次没有新增 Python 功能差异、已知未修复问题、依赖或 N8-5 业务调用方迁移。
审计时补齐导入/生成器 key 循环中的六个策略组透传字段、protocol 后备字段，以及可删除来源列表的指针；已并入各自步骤提交。
沿用 N8-3 已明确的 legacy 加载校验限制，非本步遗留回归。

新增文件行数：resources.go 124、policies.go 104、settings.go 277、system.go 77、textlines.go 53
（运行代码合计 635）；types_test.go 503、testdata/types_interchange.py 36（新增测试 539）。
同时更新 document.go、既有 repo 往返测试和本进度文档；未提交生产数据或临时调试文件。
后续：独立复核 N8-4a–d；N8-4e/f 与 N8-5 由协调者另行安排。
