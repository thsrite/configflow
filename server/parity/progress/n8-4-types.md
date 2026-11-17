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
