# C32 规则路由边界验收

范围来自已审的 `difftest-config.md` R151–153、R178–195（规则相关行）；不替代该清单。Go 包为 `internal/api/rules`，真实 Flask oracle 为其 `difftest/testdata/reference.py`。全部数据合成；下载及代理只走 loopback。

每次请求比较状态、完整 JSON/原文、Content-Type、profile、stale/Warning 标头，并比较 SQLite `sections`/`fields` 的全部行（含位置和值）、repository export、全部 JSON/YAML/list 文件。成功、失败和只读请求都比较持久化状态。Mihomo 标记交付的 YAML 比结构，未标记内容和缓存比原始文本（Python 以 bytes 读取，避免 read_text 的换行改写）。

只归一化有效的运行时间、计时数值，以及按响应位置配对的一一对应随机 ID 及其派生缓存名/native tag；引用、顺序、字段存在性、布尔值、错误和所有其他状态均保留。使用 httpxtest.MigratedAPIError 验证已批准的 HTML→JSON 错误迁移，要求相同状态和原公开描述。测试期间曾真实抓到 Agent 名称丢失和行/文件名差异，比较并非恒等通过。

| 方法/路径 | 成功 | 失败/边界 |
|---|---|---|
| GET /api/rules | 完整列表、hydrate、profile 隔离 | 401；HEAD 只读 |
| POST /api/rules | 普通规则、未知字段保留 | 非对象、类型/值、逗号、鉴权 |
| GET /api/profiles/<id>/rules | 显式 profile 列表 | 401；注册无冲突 |
| POST /api/profiles/<id>/rules | IP 规则 | 无效值；profile 隔离 |
| GET /api/rule-sets | 仅规则集 | 401；HEAD 只读 |
| POST /api/rule-sets | 引用共享库、剥离伪造来源 | 无效库 |
| PUT /api/rules/<id> | 元数据、policy、显式 DNS、follow、启停 | 未知 ID、语法、MosDNS/OxiDNS 类型保护、自定义 native |
| DELETE /api/rules/<id> | 清 selectors/actions/follow/parked/native | 不存在；不写其他 profile |
| PUT /api/rule-sets/<id> | 元数据 | 普通 rule 的 ID 返回404 |
| DELETE /api/rule-sets/<id> | 清库引用/native | 普通 rule 的 ID 返回404 |
| POST /api/rules/batch | trim、服务器分配 ID、250 条 | 空列表、类型/值；原子性 |
| POST /api/rules/compose | values/library/new_library、服务器 ID、DNS、重复跳过 | 400 field_errors、409 已引用/custom native、缓存失败提交保留；并发/取消 |
| POST /api/rules/group-name | 多行原子修改、trim | 成员消失；不改引用 |
| PUT /api/rules/policy | follow 重建、显式 DNS 保留、250 条无上限 | 不存在策略/ID、custom native 宽松保存 |
| POST /api/rules/reorder | ID及旧对象数组、top/bottom | 空/错误形状、null/数字/重复/未知ID、未知position；不覆盖展示字段 |
| GET /api/rules/dns | template/native/custom、Agent legacy 名称 | 401；HEAD 不写入 |
| PUT /api/rules/dns | direct/proxy/follow/default/actions、disabled parked、skip | 不可转换、未知/绑定 upstream、custom native 409 |
| GET /api/rules/local/<name> | 匿名缓存/inline、legacy 缓存、Mihomo 交付 | 404/422/502；stale、原始缓存不变 |
| GET /api/profiles/<id>/rules/local/<name> | 显式 profile 匿名交付 | 404/422/502；不请求真实设备 |
| POST /api/rules/match-test | 直接/延迟 ruleset、通配符、尾点/DNS labels、所有11种行分隔 | 非法查询、无效来源422、500非法body；直接命中不下载后续源 |
| POST /api/rules/find-duplicates | 同源/跨源、稳定排序、完整出现列表、2万条密集重复 | 空源标记失败；401；中央凭据脱敏 |

## Python 测试归属

下表按原始函数分组映射，不把参数化后的请求数量当作函数数；C32 没有删除相关功能。其他模块路由由原 owner 的验收覆盖，C32 不重复实现。

| Python 文件（函数数） | C32 Go 边界/回归及共享 owner |
|---|---|
| test_domain_wildcard_inputs.py (5) | TestMatchingWildcardQuerySpellings、TestAcceptedSourceLineBoundaries、TestMarkedDeliveryDoesNotAlterSource；DNS label 校验复用 rules；生成下载/Agent delivery 由 C18/A11 + C14 既有验收。 |
| test_mihomo_classical_delivery.py (13) | TestMarkedDeliveryDoesNotAlterSource、TestRemoteProviderValidationAndCache、TestPublicProvidersNormalizeOnlyDelivery、TestComposeSourcesDNSAndErrors；MRS/非UTF8交付单测。create/update/draft/check/append 路由属于 C33；生成器与 DNS 转换属于 C18/C28/A11、C14，沿用其真实 Python corpus。 |
| test_profile_rule_routes.py (29) | TestEveryRouteSuccessAndFailureWithStorage、TestTemplateAndNativeDNSWrites、TestCustomNativeRoutingPreservesCommittedMetadata、TestMosDNSGuardAndCleanup、TestLargePolicyGroupAndDNSLimit；其中 proxy-group 重命名/共享聚合来源、library has_ip_part/toggle、scoped rule proxy 共5个测试函数归 C33/C31/C28，非 C32 路由。 |
| test_rule_compose_route.py (24) | TestComposeSourcesDNSAndErrors、TestTemplateAndNativeDNSWrites、TestPinnedUpstreamChoicesAndLinkedRejection、TestDNSAgentReadModelLegacyNames；cache失败、DNS回滚、取消与并发另有 native HTTP/owner 测试。malformed rule_action import 属 C02；pinned_withheld 的 projection 字段明确 deferred→A16，hook nil/error/true/false/no-device 均有测试，未跳过其他字段。 |
| test_rule_duplicates.py (7) | TestDuplicateScanKeepsAllOccurrencesAndPriorities、TestDuplicateLargeRepeatedSourceIsComplete、TestDuplicateResponseUsesCentralRedaction、全路由差分；compact首拼写、REGEX敏感、稳定排序、空/失败源、返回全部行号及profile优先级。源码只加载一次，cached/inline源不重复解析配置。 |
| test_rule_fetch_proxy_routes.py (7) | C32 的 provider 与 matcher 下载由 TestSourceEntryPointsUsePersistedProxy 检查 raw method/body、URL、Proxy-Authorization；URL验证/缓存失败由真实回环差分。其他 rule-library/proxy设置/MosDNS转换入口归 C33/C06/C28；请求策略复用 C14。 |
| test_rule_validation_routes.py (8) | TestSyntaxValidationAcrossWriteEntries（create/edit/batch/compose）、TestComposeSourcesDNSAndErrors、YAML policy原子拒绝、逗号payload和MATCH；库create/edit/draft/check归C33，不复制其校验。 |

Fuzz：`FuzzPastedRules`，覆盖用户粘贴规则的逐行解析、逗号 payload、逻辑规则及多种换行。YAML 通过 C14/yamlx，D1重复键与alias recursion不得回退或留下写入；不新增私有 YAML 解析器。
