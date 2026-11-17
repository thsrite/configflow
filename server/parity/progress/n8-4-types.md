# N8-4a–d — typed stored documents

状态：N8-4a 已完成并通过定向测试，分支 go-n8-4；不 push。N8-4e/f（Agent、Routers）不在范围。
只定义 model 和编解码；repo 业务调用方迁移属于 N8-5。

字段依据：逐项核对 backend 非测试 Python 的字典访问、创建/更新、迁移和生成器；
生产快照只经 .64 的只读 SQLite 备份核对字段名/类型，不复制到本机、不输出值。

- Node：routes/nodes.py、routes/subscriptions.py、utils/subscription_parser.py、
  utils/sub_store_client.py、converters/{mihomo,surge,loon}.py、utils/dialer_references.py。
  params 是开放引擎字段，Python 整体展开，所以用 Dynamic；稳定资源引用是 ResourceRef。
  remark 来自真实行；protocol 是 routes/proxy_groups.py 的候选节点类型后备字段。
  utils/config_importer.py 的节点/订阅/规则源写入也已核对；未知引擎字段保留 Raw。源/缓存派生展示字段不成为存储 schema。
- Subscription/Aggregation：routes/{subscriptions,aggregations}.py、utils/subscription_{filter,refresh,hosts}.py、
  utils/health_check.py。health_check 是 ProviderHealthCheck；统计响应字段排除，旧健康 URL 保留。
- RuleLibraryItem：routes/rule_library.py、utils/{rule_utils,scheduled_refresh}.py、
  common/config_repository.py。source_type/behavior/format/update_interval 保持可选，不在加载时填默认。

所有 record 有自己的 raw Raw 和 Fields；非零默认值用指针。只使用 Int、明确记录/列表、
Map、Dynamic，不使用 any 作为存储成员；空容器、R1–R9 沿用 N8-3。
新增 types_test.go：每种嵌套类型独立字节往返、逐字段改动隔离、逐字段 R5、指针删除；
Python json 双向和真实快照行往返（环境开关），FuzzStoredTypes 按所有类型加种子。
已有 repo TestStoredRoundTrip 的 test-only 桥适配类型，业务代码不改。

快照 a 覆盖：nodes=5、subscriptions=2、aggregations=1、rule_library=44；
health_check=3，ResourceRef 快照未出现，用合成/双向夹具覆盖。
.64 定向结果：model 0.134s / repo 25.401s，Python 和私有快照启用，PASS。
测试入口纠正：直接调用记录 UnmarshalJSON 保留外层空白；列表通过所属 Shared 编码，
并由既有 repo 行测试验证字节，而非把无来源信息的裸 Go slice 当作存储行。
一次 SSH 没收到远端退出状态，未计为通过；重跑启用 SSH keepalive，日志 /tmp/n84-a-repo.log。

下一步：b → c → d → rebase go → 完整门禁。
命令：`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server; go test ./internal/model ./internal/repo'`。

N8-4a 提交：281003c9。
N8-4b：ProxyGroup/ProxyChain/RuleConfig 已实现。
字段来源：routes/{proxy_groups,rules}.py、utils/{dialer_references,proxy_chains,group_candidates,health_check}.py、
converters/{mihomo,surge,loon,common}.py、common/config_repository.py（验证、迁移、hydrate）。
普通来源列表用指针表达可删除字段，以支持 Python 在 chain 转换中删除成员；健康字段指针支持 delete_fields。
utils/config_importer.py 的 MIHOMO_GROUP_PASSTHROUGH 及生成器 key 循环全部类型化：
max-failed-times、disable-udp、hidden、icon、exclude-filter、exclude-type 等不遗漏到 Raw。
排序引用共享 ResourceRef；代理链只保存 entry/exit 引用，不引入递归类型；legacy 来源保留具体字段。
规则的 groupId/groupLabel/groupPolicy/isExpandedGroupItem/isFirstInGroup/isLastInGroup/uniqueId
来自快照的持久化编辑器元数据，即使当前 Python 只透传，也明确定义并保留原大小写。
快照覆盖：60 ProxyGroup、94 RuleConfig、256 proxies_order 引用；chain 未出现，以合成夹具覆盖。
N8-4b .64 定向验证：model 0.223s / repo 28.280s，Python/快照启用，PASS。

N8-4b 提交：9855a052。
N8-4c：Mihomo/Surge/Loon/MosDNS、EasyTier（全部 34 个 FIELDS 选项）、DomainDiscovery/history/recheck、
DeviceNotes=Map[string]、ClientFixedIPs=Map[ClientFixedIP]，OxiDNS=Dynamic。
字段来源：common/{config,config_repository,mihomo_easytier,device_notes,client_fixed_ip}.py、
routes/{config,mosdns}.py、converters/{mihomo,surge,loon,mosdns}.py、utils/domain_discovery_service.py。
custom_config 保持字符串：存储的是可编辑文本（包括未通过 YAML 验证的草稿），本步不解析或填默认；
Node 树由消费者调用 yamlx 创建。MosDNS matches 的 string/list 用 TextLines 明确联合表示，保留形态。
旧 api_addr 明确可选；默认地址在 Python 的读/写端还不一致，本步不抹平默认。
快照覆盖：各引擎 2 行、EasyTier/Options/Keepalive 各 2、MosDNSMatch=4、DiscoveryHistory=10、
DiscoveryRecheck=6、DeviceNotes=14；ClientFixedIPs 真实行是空对象（1 行），无真实 binding，
ClientFixedIP 用逐字段合成/Python 双向验证，不能声称真实记录覆盖。
N8-4c .64：model/repo 0.300s/28.759s PASS；增强后的原始 SQLite 嵌套记录逐个往返+改动隔离
model 0.320s PASS。私有测试先检查原始 value 文本，再做 Python json 重写后的双向互读。
新类型未新增运行依赖；未迁移业务调用方，无 Python 功能差异。
