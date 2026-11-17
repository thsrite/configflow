# Config 领域黑盒差分覆盖清单（Step A；O6 已审：有补充后通过，见文末“O6 review”）

此文是用例设计，不代表测试已实现或已通过。基准为本分支 Python 与 `routes.tsv`；只提交本文件。没有读取 `server/internal/**` 领域实现；允许的 `httpx/difftest/diff_test.go` 只用于了解启动/请求/比较框架。下一步先由独立 Opus 对照 Python 审核，再进入 Step B。

归属按 `FILES.md` 的 config 路由文件筛选，共 **228 条 method+模板**。其中 `profiles.py` 的代理改绑也保留：虽调用 agents manager，路由文件归 config。显式 profile 别名各占一行，绝不以一个默认入口的通过替代别名覆盖。`routes.tsv` 的 todo/done 不作为覆盖依据。所有源码链接定位本次清单基准；rebase 后若 Python 变化，需复核链接与条件。

## 用例和比较规则

- 每行 `Rnnn-Sxx/Fxx/Bxx` 分别是成功、失败/降级、边界场景；一个编号是一份独立 fixture/request 序列。`Hnnn` 是后文共享 helper 的逐分支配方，一个编号对应一个源码错误/诊断位置，不声称一次请求能穿过多个错误。循环校验器的每个字段、每个互斥条件仍须在 Step B 参数化；本次数量是**有名场景/配方数，不是最终 HTTP 请求数**。一个长流程可能发多次请求。
- 表格中的“helper/repo”必须展开对应 H 分支，不能只测一个无效 body。H 给出满足错误 guard 的输入条件；基于下列合法 fixture 一次只改一处，若前置校验先挡住则换该 helper 的指定公开入口，或通过允许的 Python 备份 fixture 种下旧数据。不得从 Go 业务函数导出期望值。不可由 HTTP 构造的运行环境异常列为故障注入或部分覆盖，不能拿双方报错充作成功。
- 对每次请求比较 status、Content-Type、Content-Disposition、Cache-Control、ETag、X-ConfigFlow-Profile、Location、Allow、CORS、stale/warning 标头与 JSON 结构/值。中文提示逐字节；Python 内部异常的英文措辞只比拒绝、status、字段形态。**[O6] 例外**：前端按字符串解析的英文/代码字段逐字节比较——`Profile not found: <id>`（[api/index.ts:78](../../frontend/src/api/index.ts#L78) 用 `startsWith` 与整串比较触发配置空间恢复）以及所有 `code` 字段（`oxidns_changed_elsewhere`、`node_deletion_changed`、`RULE_SOURCE_UNAVAILABLE`、`RULE_CONTENT_INVALID`、`ruleset_already_used` 等）。JSON 一律解析后比较（Python 全局钩子以 `sort_keys` 重新序列化，键序不承载语义），但 Werkzeug HTML 错误页（未知路径/方法、`request.json` 的 400/415、未捕获异常 500）只比 status 与“非 JSON”这一事实。YAML 解析后逐节点比值和有意义的数组顺序；Surge/Loon 按 section/key/有序规则语义比；规则源原文不做宽泛删行。ZIP/tar 解包比成员集合、路径、内容结构与引用闭包，不能只比压缩大小。
- 随机 id/token 必须校验格式、唯一性、引用和读回，再建立 Python↔Go 双射；同一双射贯穿响应、DB、文件名。时间校验格式、因果/窗口与相对顺序；固定 fixture 时间不忽略。ETag/revision/hash 不直接掩盖：每端自行验证与内容一致，继续用它做下一次写入。**[O6] 更正**：由存储内容确定性计算、不含随机量的校验值必须与 Python 字节相同，不能只做自洽检查——OxiDNS 设置 ETag 是 `sha256(json.dumps(oxidns, sort_keys=True, ensure_ascii=False, separators=(',',':')))`（[oxidns.py:15](../../backend/routes/oxidns.py#L15)），切换前打开的浏览器页会把 Python 发的 ETag 送给 Go；浮点、非 ASCII、大整数、键序的序列化差异都会造成误报 409。运行时间只限定非负且完成在期限内。禁止 mask 整个 content/config/日志数组/错误对象。
- 两个进程分别使用相同 seed 的独立 DATA_DIR；每例从干净副本开始，多请求例在本例延续。loopback 假 HTTP/DNS/Sub-Store/Agent/WebDAV 可记录请求和挂起点。所有运行、构建、故障注入都只在测试服务器，真网络栈/容器只在 .32。Step A 无测试执行、无构建、无生产快照访问。
- HTTP 用例必须保留身份校验优先级：~~`routes/auth.py:35` 的全局 before_request 先于 `require_auth` 和 `profile_context`~~。**[O6] 更正（原文错误）**：`install_profile_context` 在 `register_blueprints` 末尾注册（[routes/__init__.py:113](../../backend/routes/__init__.py#L113)），早于 `setup_before_request`（[app.py:107](../../backend/app.py#L107)），Flask 按注册顺序执行 before_request，所以**非全局端点先解析 profile、后鉴权**：未登录请求带不存在/非法的 profile（路径、`?profile=`、`?profile_id=` 或前端每次都带的 `X-ConfigFlow-Profile` 头）先得到 404 `Profile not found: <id>` / 400 `Invalid profile id`，而不是 401。`_is_global_request` 为真的蓝图（auth/settings/nodes/subscriptions/…，见 [profile_context.py:32](../../backend/common/profile_context.py#L32)）不解析 profile，仍先 401。全局钩子之后才轮到 `require_auth`。见 X02。默认 `/api/subscriptions/*/proxies`、`/api/aggregations/*/provider` 并不在全局公开名单；启用登录时仅 query token 仍 401。显式 profile provider 别名才被白名单放行后校验 URL token。`/api/rule-library/content/*` 与 `/api/rules/local/*` 无 auth 校验，成功匿名访问是特性，不虚构权限失败。

## 合法 fixture 与通用变体

以 `alpha`（默认）和 `beta` 两个 profile，各有独立配置、相同共享目录为起点；另设只引用 beta 的资源，写 alpha 后始终验证 beta 没变。所有地址只用 loopback 或文档网段，域名用 example.com；示例端口由服务器 fixture 分配并两端映射。

| fixture | 典型输入/合法形态 | Python 来源 |
|---|---|---|
| node-a | `{id:"node-a",name:"节点😀",type:"socks5",server:"127.0.0.1",port:18083,params:{username:"u",password:"synthetic"}}`；另用 `proxy_string` JSON、YAML、URI/base64 三种入口，每种各一例 | [mihomo.py:1112](../../backend/converters/mihomo.py#L1112)；[subscription_parser.py:61](../../backend/utils/subscription_parser.py#L61) |
| sub-a | `{id:"sub-a",name:"订阅😀",url:"http://127.0.0.1:18084/feed",enabled:true,interval:60}`；fake 返回两个节点和 Subscription-Userinfo；有新鲜、过期、旧无 metadata、URL 不匹配和坏内容缓存 | [subscriptions.py:48](../../backend/routes/subscriptions.py#L48)；[subscription_refresh.py:94](../../backend/utils/subscription_refresh.py#L94) |
| group-a | `{id:"group-a",name:"代理😀",type:"select",manual_nodes:["node-a"]}`；分别替换为 subscriptions、aggregations、include_groups、follow_group；chain 使用 `{type:"chain",chain:{entry:{type:"node",id:"node-a"},exit:{type:"group",id:"group-b"}}}` | [group_candidates.py:233](../../backend/utils/group_candidates.py#L233)；[dialer_references.py:185](../../backend/utils/dialer_references.py#L185) |
| lib-a / rule-a | `{id:"lib-a",name:"规则😀",source_type:"content",behavior:"domain",format:"text",content:"+.example.com\n"}`；直接 rule `{id:"rule-a",itemType:"rule",rule_type:"DOMAIN-SUFFIX",value:"example.com",policy:"DIRECT",enabled:true}`；ruleset 引用 `{id:"rs-a",itemType:"ruleset",library_rule_id:"lib-a",policy:"代理😀"}` | [rules.py:57](../../backend/routes/rules.py#L57)；[rule_library.py:99](../../backend/routes/rule_library.py#L99) |
| agg-a | `{name:"聚合😀",subscriptions:["sub-a"],nodes:["node-a"],enabled:true}`；创建后从响应关联生成 id，不强行假定固定 UUID | [aggregations.py:334](../../backend/routes/aggregations.py#L334) |
| OxiDNS | `{}` 补默认 template；native 最小 `{config_mode:"native",native_config:"plugins: []\n",additional_files:[]}`；有效复杂 native 使用 Python 生成模板再经 editor 获取，涵盖 route/upstream/cache/query recorder/动态集/AdGuard/RouterOS/update，每种独立启用一次 | [oxidns_settings.py:115](../../backend/common/oxidns_settings.py#L115)；[oxidns_editor.py:228](../../backend/common/oxidns_editor.py#L228) |
| import-client | Mihomo：`proxies: [{name: 节点😀,type: socks5,server: 127.0.0.1,port: 18083}]`；Surge/Shadowrocket：`[Proxy]` + `节点😀 = socks5, 127.0.0.1, 18083`；Loon 使用 `[Proxy]` 的 Loon socks5 语法。另加一个策略组和一条 DOMAIN-SUFFIX 规则 | [config_importer.py:884](../../backend/utils/config_importer.py#L884)；[profiles.py:40](../../backend/routes/profiles.py#L40) |
| setup | `id` 为32位小写 hex，`name:"向导😀"`，`goal:router/gateway`；已有 node-a 源；prepare 四组 dns_fallback/traffic_fallback；安装与路由器全部用回环 fake，绝不安装到实机 | [setup_flow.py:35](../../backend/common/setup_flow.py#L35)；[setup_flow.py:372](../../backend/common/setup_flow.py#L372) |

通用边界变体：名称分别用中文、emoji、引号/反斜杠/反引号/逗号/换行；文本用空、空白、null、错型、64KiB 长串；列表用空、重复、缺对象、最大值和最大值+1；路径用 `%2e%2e`、编码斜杠、非 ASCII 和控制字符。只在相关字段展开，不替 Python 发明校验上限。链、规则、DNS 等错误的具体条件由 H 分支列出。源码已有的真实示例地址在本表一律改成合成值。

## 共享身份与 profile 场景

| 代码 | 输入与断言 | 来源 |
|---|---|---|
| A1–A7 | 每条受保护路由分别：无 Authorization、伪造 JWT、过期 JWT、`Bearer  ` 双空格、8193 字符 Bearer、合法签名但空 claims、只有 query config_token。均比 401 JSON 与前置钩子的消息；启用管理员登录 | [auth.py:77](../../backend/routes/auth.py#L77)；[common/auth.py:24](../../backend/common/auth.py#L24)；[common/auth.py:131](../../backend/common/auth.py#L131) |
| D1–D7 | 自行授权的拉取分别：缺凭据、错 query token、过期 JWT、当前 internal capability query、退役 capability query、坏 Bearer、8193 长 query。均 401；current/retired capability 即使同时带有效 JWT 也不能冒充 config token。默认 provider 先受 A 保护，其 D 仅在通过前置钩子时验证。**[O6] 补充环境矩阵**（原文“均 401”只对“启用登录”或“设置了 config_token”成立）：三种环境各跑 D1–D7——(a) 启用登录；(b) 关闭登录但设 config_token；(c) 关闭登录且无 config_token。在 (c) 中 D1/D2/D3/D6/D7 均**放行 200**，而 D4/D5（当前或退役 rule_proxy_token 出现在 query 或 Bearer）仍 401，因为内部能力检查在“认证关闭直接放行”之前（[common/auth.py:98](../../backend/common/auth.py#L98)）。另加 D8 [O6]：伪造 `X-ConfigFlow-Internal` 头（随机值、空值）不得绕过任何检查（X03） | [common/auth.py:68](../../backend/common/auth.py#L68)；[config.py:25](../../backend/routes/config.py#L25) |
| P1–P6 | profile 不存在→404；profile id 非法→400；P3显式 URL alpha 覆盖 query beta/header beta（Step B明确：无URL profile的入口，用query alpha覆盖header beta验证同一优先级）；P4非显式 `profile=alpha` 优先于 `profile_id=beta`；P5 header beta 在无 query 时生效；P6无 selector 使用稳定默认。显式URL始终优先，不要求它被header覆盖。全局端点忽略 query/header selector，不能误切 profile | [profile_context.py:31](../../backend/common/profile_context.py#L31)；[profile_context.py:52](../../backend/common/profile_context.py#L52)；[config_repository.py:1856](../../backend/common/config_repository.py#L1856) |

A、D、P 在下表每个适用路由实际展开计数；公开无失败分支的 status/version 不拿 405 冒充业务失败，明确标为无业务失败。错误 method/CORS 属 HTTP 壳额外检查，不改变 routes.tsv 的路由分母。

## 写入后比较代码

每个代码都要求**先后快照全库**：按 `sections(scope,owner,position)`、`fields(scope,owner,field,position,value,revision)` 和 `meta(key,value)` 比较；JSON value 解析而非比序列化空白。`store_id/generation/commit_id` 分配值可双射，但单调性、变更字段 revision、未修改行、事务完整性必须验证。不得只 GET 读回。表结构来源 [config_store.py:38](../../backend/common/config_store.py#L38)，写入来源 [config_store.py:325](../../backend/common/config_store.py#L325)（[O6] 原 L367 在 commit 中段，改指 `commit`）。系统/共享 owner 为空，profile owner 为 id。

| 代码 | 具体行、文件和异步观察 |
|---|---|
| DB0 | 无配置写入；允许请求日志追加，但不得改 fields/meta 或生成物。若 GET 有特别副作用，以下专用代码覆盖它。 |
| SYS / SHARED / PROFILE | 只允许 system/shared/profile 指定字段行及关联 revision；未知字段不能丢、其他 profile 不变。PROFILE:* 检查所有该 profile 字段。`save_config_with_publish_guard` 的 expected_profile_id/service_type/config_revision 与已提交快照一致；保存草稿无 Agent 推送。来源 [config.py:236](../../backend/common/config.py#L236)（[O6] 原链接 L136 指向 `reset_config_context`，已改）、[config_repository.py:1314](../../backend/common/config_repository.py#L1314)（[O6] 原 L1265 落在 `delete_profile` 内，改指 `update_profile_transaction`）。 |
| SUB / SUBSTORE | shared.subscriptions/nodes 行、`shared/subscribes/*.json` 的 nodes/count/metadata(url、hosts、fetched_at)、`shared/subscription_health.json` 的 history/traffic、`task-status.json` 的订阅 key；URL 改变/删除清理行为逐项比。hosts 变化会 invalidate Agent content-sync 状态，但不是主动发布；fake 记录不得有 push。来源 [subscription_cache.py:80](../../backend/utils/subscription_cache.py#L80)、[subscription_cache.py:108](../../backend/utils/subscription_cache.py#L108)、[subscriptions.py:221](../../backend/routes/subscriptions.py#L221)。 |
| DELETE-NODE | 预览不写；确认后 shared.nodes/aggregations 和每个 profile.proxy_groups 中 manual_nodes/proxies/proxies_order 原子解绑，confirmation 仅锁定受影响引用，无关改动不应失效；blockers（链、直接节点 policy、raw dialer）拒绝并全库不变。来源 [node_deletion.py:8](../../backend/common/node_deletion.py#L8)、[config_repository.py:1483](../../backend/common/config_repository.py#L1483)。 |
| RULE / GROUP / COMPOSE | profile.rule_configs/proxy_groups/mihomo.custom_config/oxidns.native_config 和 RULE_DNS_FIELDS；MosDNS 选择、DNS follow、disabled 暂存一并比较。COMPOSE 另比 shared.rule_library 与规则缓存；warning 不丢，失败必须跨 shared/profile 原子回滚；pinned_withheld 通过 fake 自动同步设备观测，不能无条件 mask。来源 [rules.py:193](../../backend/routes/rules.py#L193)、[rules.py:378](../../backend/routes/rules.py#L378)、[proxy_groups.py:97](../../backend/routes/proxy_groups.py#L97)。 |
| LIBRARY / RULE-FILE | shared.rule_library 行、各 profile 的 hydrated 输出/原始引用，`shared/rules` 中按声明寻址缓存与旧名称缓存迁移/删除；task-status rule_source 项，validated cache 的内容/绑定声明；GET local/match/duplicate 可能下载和刷新缓存。只改偏好或只重命名不强迫下载。批量 test 会禁用失败项，cache 失败不禁用。来源 [rule_library.py:153](../../backend/routes/rule_library.py#L153)、[rule_utils.py:40](../../backend/utils/rule_utils.py#L40)、[rules.py:690](../../backend/routes/rules.py#L690)。 |
| PROVIDER | 上述订阅缓存/health + profile `providers/<agg-id>.yaml`、`providers/served/{subscription,aggregation}-<id>.yaml`、bundle 涉及的所有 provider 文件；适配后的图与主配置一致；失败不替换 last-good，DB 资源声明不被 GET 修改。比较 fake 请求次数/参数、并发归并、后台最终落盘。来源 [provider_response.py:68](../../backend/utils/provider_response.py#L68)、[provider_delivery.py:332](../../backend/utils/provider_delivery.py#L332)、[aggregations.py:473](../../backend/routes/aggregations.py#L473)。 |
| PULL / GENERATE / PREVIEW | 生成器可能预检 provider 并写缓存，必须使用 PROVIDER 检查；PULL 另外检查脱敏拉取日志。generate Mihomo 写 profile `generated/config.yaml`，Surge 写 `generated/config.conf`，Loon 写 `generated/loon.lcf`；MosDNS/OxiDNS ZIP 比 config 与规则/自定义文件闭包，运行 state/cache 不误打包。preview 不写这些 generated 文件；正文内部 capability 脱敏例外精确到端点。无 Agent publish。来源 [generate.py:92](../../backend/routes/generate.py#L92)、[config_repository.py:2057](../../backend/common/config_repository.py#L2057)、[profile_context.py:124](../../backend/common/profile_context.py#L124)。 |
| ARTIFACT / ARTIFACT-READ | 前述生成副作用 + DATA_DIR `.generated-previews/<id>/record.json`、`bundle.tar.gz`，manifest/files/path/role/size/sha256、过期时间、TTL/24份/512MiB 淘汰；读取不重渲染、不推送。来源 [generated_preview.py:67](../../backend/agents/generated_preview.py#L67)、[generate.py:36](../../backend/routes/generate.py#L36)。 |
| PROFILES / REBIND / IMPORT / IMPORT-ALL / RESET / EXPORT | PROFILES 对照 profile section 增删、顺序、默认指针和缓存目录；REBIND 仅 system.agents 中 profile_id/updated_at，部署历史不改，不发布。IMPORT dry_run 不写，实导入检查新增共享资源/引用去重；IMPORT-ALL/RESET 检查全部 scopes 和默认 profile、Agent/受保护 router guard；备份中内部 token 不泄漏，Web listener 部署设置不随导入覆盖。EXPORT 只导出不写。来源 [config_repository.py:1135](../../backend/common/config_repository.py#L1135)、[config_repository.py:2185](../../backend/common/config_repository.py#L2185)、[manager.py:366](../../backend/agents/manager.py#L366)。 |
| STAT / NOTICE / LOG | STAT 比 profile `stats_history.json` 的48样本/30分钟节流；NOTICE 比 DATA_DIR `notifications.json` 已读版本映射和新版本未读；LOG 比固定 fixture 日志字节、截断和路径/时间的关系，不 mask 整个数组。来源 [stats.py:37](../../backend/routes/stats.py#L37)、[notification_store.py:39](../../backend/utils/notification_store.py#L39)、[logs.py:154](../../backend/routes/logs.py#L154)。 |
| WEB / DEP / DAV / NET | WEB 比临时监听配置/保存端口、旧端口恢复、进程访问能力；DEP 比 `sub-store/runtime` bundle/VERSION/备份目录、运行状态与状态文件；DAV 比回环服务器收到的 MKCOL/PUT/DELETE、上传 JSON 的完整备份和保留集合，客户端配置不因“测试/备份”被写回；NET 比精确请求。失败日志不能含 synthetic 密码/token。来源 [web_listener.py:262](../../backend/utils/web_listener.py#L262)、[dependencies.py:273](../../backend/utils/dependencies.py#L273)、[webdav_backup.py:169](../../backend/utils/webdav_backup.py#L169)。 |
| PROBE / TASK | PROBE 比 shared `node_latency.json` 的 method/results/history（14条），system_config.node_test_dns_config 与兼容 node_test_dns；假 probe stdin/返回结构/退出码/启动次数，非实际公网测速。TASK 比 `task-status.json` 计数/trigger/结果、锁释放和最终缓存；interval 只改 subscription.interval 或 rule_library.update_interval，metrics interval 另存全局设置。来源 [node_latency.py:170](../../backend/utils/node_latency.py#L170)、[node_dns.py:137](../../backend/utils/node_dns.py#L137)、[tasks.py:278](../../backend/routes/tasks.py#L278)。 |
| SETUP | `setup-sessions/<hex>.json`、专属 setup profile、自动订阅 shared 行、system.config_token 和 dismissed 标志；prepare 的组/规则/Mihomo/OxiDNS 一次提交，session 只在对应阶段推进；check 会查询 fake Agent、可能关联 router draft；finish 默认空间/完成标记；abandon dry_run 不写，执行按占用原因保留或清理。来源 [setup.py:86](../../backend/routes/setup.py#L86)、[setup_abandon.py:61](../../backend/common/setup_abandon.py#L61)。 |
| DISCOVERY-WRITE / DISCOVERY-JOB | 写入 shared.rule_library.content + 缓存、profile.rule_configs 与 domain_discovery(history/ignored/website_notes/默认库)，新增规则位于 MATCH 前。job 比内存状态 running/done/failed、progress、probes/regions 按 Agent 的 JSON、history 与自动采纳；fake Agent 请求参数/次数、不会对另一 profile 执行。没有直接发布，后续 rule-sync 等跨域效果需分开接收。来源 [domain_discovery_service.py:560](../../backend/utils/domain_discovery_service.py#L560)、[domain_discovery_service.py:860](../../backend/utils/domain_discovery_service.py#L860)。 |

## 每条路由覆盖矩阵

下表来源列同时列 wrapper 和实际 Python handler。每个 S/F/B 的无单独 L 链接项以该 handler 的分支及后文 H 明细为依据；有 L 的项指向确切返回分支。A/D/P 的来源见上表。别名使用同样业务用例，但以路径中 alpha 强制覆盖相冲突的 query/header，再检查 beta 隔离。

| ID / method / route | 成功（S） | 失败与降级（F） | 边界（B） | 写后比较 / 长流程 | Python |
|---|---|---|---|---|---|
| **R001** `GET /api/aggregations` | **S01** 聚合列表<br>**S02** 空列表 | **F01–07** A1–7 | **B01** 包含旧统计字段时遵循列表原样返回 | DB0 | [backend/routes/aggregations.py:336](../../backend/routes/aggregations.py#L336) |
| **R002** `POST /api/aggregations` | **S01** 订阅源聚合<br>**S02** 手动节点聚合<br>**S03** 混合源聚合 | **F01–07** A1–7<br>**F08** 非对象→400 [L347](../../backend/routes/aggregations.py#L347)<br>**F09** Step B oracle更正：重复或空名称仍200并保存（未执行名称校验）<br>**F10** 不存在订阅→repo400<br>**F11** 不存在节点→repo400<br>**F12** 健康检查无效→repo400<br>**F13** Step B oracle更正：非法 regex_filter 仍200并保存（该创建链未校验正则） | **B01** 中文 emoji 名称<br>**B02** 统计字段删除<br>**B03** 空源列表是否允许按 repo | SHARED:subscription_aggregations；AUTO(T16/17) | [backend/routes/aggregations.py:336](../../backend/routes/aggregations.py#L336) |
| **R003** `DELETE /api/aggregations/<agg_id>` | **S01** 未引用聚合删除<br>**S02** 不存在幂等成功 | **F01–07** A1–7<br>**F08** 被 profile 策略组引用→repo409 | **B01** 重复删除不清无关文件 | SHARED:subscription_aggregations；AUTO(T16/17) | [backend/routes/aggregations.py:369](../../backend/routes/aggregations.py#L369) |
| **R004** `GET /api/aggregations/<agg_id>` | **S01** 已有聚合且统计字段剥除 | **F01–07** A1–7<br>**F08** 不存在→404 [L385](../../backend/routes/aggregations.py#L385) | **B01** 中文 emoji 与未知扩展字段 | DB0 | [backend/routes/aggregations.py:369](../../backend/routes/aggregations.py#L369) |
| **R005** `PUT /api/aggregations/<agg_id>` | **S01** 整对象替换但路径 id 优先 | **F01–07** A1–7<br>**F08** 非对象→400 [L394](../../backend/routes/aggregations.py#L394)<br>**F09** 不存在→404 [L412](../../backend/routes/aggregations.py#L412)<br>**F10** 坏引用→repo<br>**F11** 异常→500 [L417](../../backend/routes/aggregations.py#L417) | **B01** 传入错误 id 被覆盖<br>**B02** 统计字段清除 | SHARED:subscription_aggregations；AUTO(T16/17) | [backend/routes/aggregations.py:369](../../backend/routes/aggregations.py#L369) |
| **R006** `GET /api/aggregations/<agg_id>/count` | **S01** 从缓存加启用手动节点计数<br>**S02** 旧 count-only 缓存 | **F01–07** A1–7<br>**F08** 不存在→404 [L437](../../backend/routes/aggregations.py#L437)<br>**F09** 缓存结构异常→500 [L470](../../backend/routes/aggregations.py#L470) | **B01** 空源为 0<br>**B02** 同名按 Python 计数不额外去重 | DB0 | [backend/routes/aggregations.py:428](../../backend/routes/aggregations.py#L428) |
| **R007** `GET /api/aggregations/<agg_id>/preview` | **S01** 混合源生成后读取 YAML 节点<br>**S02** 远端失败缓存降级 | **F01–07** A1–7<br>**F08** 不存在→404 [L484](../../backend/routes/aggregations.py#L484)<br>**F09** 引用图无效→400 [L508](../../backend/routes/aggregations.py#L508)<br>**F10** 文件读取或渲染异常→500 [L511](../../backend/routes/aggregations.py#L511) | **B01** 慢源完成顺序不改变输出顺序<br>**B02** 空列表 | PROVIDER；长流程 PROVIDER | [backend/routes/aggregations.py:475](../../backend/routes/aggregations.py#L475) |
| **R008** `GET /api/aggregations/<agg_id>/provider` | **S01** 默认 YAML<br>**S02** format=surge<br>**S03** format=loon<br>**S04** Surge/Loon regex 排除<br>**S05** 失联源复用有效缓存 | **F01–07** A1–7<br>**F08** 认证失败→401 [L537](../../backend/routes/aggregations.py#L537)<br>**F09** 不存在→404 [L545](../../backend/routes/aggregations.py#L545)<br>**F10** 禁用→403 [L548](../../backend/routes/aggregations.py#L548)<br>**F11** 读取配置异常→500 [L551](../../backend/routes/aggregations.py#L551)<br>**F12** 非法 filters→400 [L558](../../backend/routes/aggregations.py#L558)<br>**F13** 引用图或代理链不支持→400 [L586](../../backend/routes/aggregations.py#L586)<br>**F14** 渲染异常→500 [L589](../../backend/routes/aggregations.py#L589)<br>**F15** D4/D5 内部能力与JWT混用依然拒绝；D1/D2仅query先被A拦截 | **B01** 空聚合 proxies=[]<br>**B02** 中文 emoji 名称<br>**B03** 同名节点去重<br>**B91 [O6]** HEAD 也会执行 handler（拉取上游、写缓存/健康记录）：比副作用而非只比空 body（X06） | PROVIDER；长流程 PROVIDER | [backend/routes/aggregations.py:515](../../backend/routes/aggregations.py#L515) |
| **R009** `GET /api/aggregations/<resource_id>/health-impact` | **S01** 一个 profile 引用<br>**S02** 多个 profile 引用<br>**S03** 无引用 | **F01–07** A1–7<br>**F08** 不存在按 Python 返回用途列表 | **B01** 中文 emoji 名称 | DB0 | [backend/routes/aggregations.py:53](../../backend/routes/aggregations.py#L53) |
| **R010** `POST /api/aggregations/reorder` | **S01** ids top<br>**S02** ids bottom<br>**S03** 旧 aggregations 数组 | **F01–07** A1–7<br>**F08** 重复 ID→404 [L607](../../backend/routes/aggregations.py#L607)<br>**F09** 非字符串 ID→404 [L607](../../backend/routes/aggregations.py#L607)<br>**F10** ids 错型→404 [L607](../../backend/routes/aggregations.py#L607)<br>**F11** 未知 ID→404 [L607](../../backend/routes/aggregations.py#L607)<br>**F12** 策略引用错→400 [L612](../../backend/routes/aggregations.py#L612)<br>**F13** 其他异常→500 [L617](../../backend/routes/aggregations.py#L617) | **B01** 空 ids<br>**B02** 旧对象字段不覆盖 | SHARED:subscription_aggregations；AUTO(T16/17) | [backend/routes/aggregations.py:598](../../backend/routes/aggregations.py#L598) |
| **R011** `POST /api/auth/login` | **S01** 正确 username/password→token、username、expiresIn=86400 | **F01** 认证关闭→400 [L21](../../backend/routes/auth.py#L21)<br>**F02** 用户名错误→401 [L26](../../backend/routes/auth.py#L26)<br>**F03** 密码错误→401 [L26](../../backend/routes/auth.py#L26)<br>**F04** 缺 username→401 [L26](../../backend/routes/auth.py#L26)<br>**F05** 空 password→401 [L26](../../backend/routes/auth.py#L26)<br>**F06** JSON 数组→500 [L28](../../backend/routes/auth.py#L28)<br>**F07** 坏 JSON→500 [L28](../../backend/routes/auth.py#L28)<br>**F08** 非 JSON Content-Type→500 [L28](../../backend/routes/auth.py#L28) | **B01** 中文 emoji 账号在独立环境可登录<br>**B02** 8192 字符密码精确比较 | LOG | [backend/routes/auth.py:16](../../backend/routes/auth.py#L16) |
| **R012** `GET /api/auth/status` | **S01** 认证开启→authEnabled=true<br>**S02** 仅用户名或密码为空→false | **F01** 公开入口无业务错误分支：不伪造失败，另测错误 method 的 405 | **B01** 带错误 JWT 仍返回状态 | DB0 | [backend/routes/auth.py:12](../../backend/routes/auth.py#L12) |
| **R013** `GET /api/auth/verify` | **S01** 有效 JWT→success=true | **F01–07** A1–7 | **B01** 关闭认证时错误 token 放行<br>**B90 [O6]** JWT 兼容见 T29：同一 `JWT_SECRET_KEY` 下 Python 签发的 token 在 Go 通过，反之亦然 | DB0 | [backend/routes/auth.py:32](../../backend/routes/auth.py#L32) |
| **R014** `GET /api/backup/config` | **S01** 无密码返回空<br>**S02** 有密码仅返回 ****** | **F01–07** A1–7 | **B01** 保留策略缺失及旧非法值归一<br>**B90 [O6]** 回环 DAV 逐请求比方法序列（HEAD/PROPFIND/MKCOL/PUT/DELETE）、Basic 认证头、中文路径段百分号编码与尾斜杠 | DB0 | [backend/routes/settings.py:201](../../backend/routes/settings.py#L201) |
| **R015** `POST /api/backup/config` | **S01** 完整 WebDAV 配置<br>**S02** 仅 keep_latest 部分更新<br>**S03** ****** 保留密码<br>**S04** 空密码清除<br>**S90 [O6]** 打开 auto_backup 本身就是 system 变更：已配置 WebDAV 时约 1 秒去抖后立即上传一次（[webdav_backup.py:221](../../backend/utils/webdav_backup.py#L221)） | **F01–07** A1–7<br>**F08** 空对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F09** 非对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F10** 只有未知字段→400 [L186](../../backend/routes/settings.py#L186)<br>**F11** 文本字段非字符串→400 [L188](../../backend/routes/settings.py#L188)<br>**F12** auto_backup=1→400 [L190](../../backend/routes/settings.py#L190)<br>**F13** keep_latest=-1→400 [L193](../../backend/routes/settings.py#L193)<br>**F14** keep_latest=1001→400 [L193](../../backend/routes/settings.py#L193)<br>**F15** keep_daily=3651→400 [L193](../../backend/routes/settings.py#L193)<br>**F16** 保留数 bool→400 [L193](../../backend/routes/settings.py#L193) | **B01** keep_latest=0<br>**B02** keep_latest=1000<br>**B03** keep_daily=3650<br>**B04** 中文 emoji 目录 | SYS:backup；AUTO(T16/17) | [backend/routes/settings.py:201](../../backend/routes/settings.py#L201) |
| **R016** `POST /api/backup/now` | **S01** 回环 PUT 当前完整备份<br>**S02** 复用 ****** 和保存的保留设置<br>**S03** 成功上传后清理旧备份 | **F01–07** A1–7<br>**F08** 非对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F09** 空对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F10** 未知字段→400 [L186](../../backend/routes/settings.py#L186)<br>**F11** 文本字段错型→400 [L188](../../backend/routes/settings.py#L188)<br>**F12** auto_backup 错型→400 [L190](../../backend/routes/settings.py#L190)<br>**F13** 保留设置越界→400 [L193](../../backend/routes/settings.py#L193)<br>**F14** 不完整配置→400 [L323](../../backend/routes/settings.py#L323)<br>**F15** HTTP 上传失败→500 [L331](../../backend/routes/settings.py#L331)<br>**F16** 清理异常保留已上传对象并比较结果 | **B01** keep_latest=0 不按数量清理<br>**B02** 同日备份与跨日保留<br>**B03** 特殊目录名<br>**B90 [O6]** 回环 DAV 逐请求比方法序列（HEAD/PROPFIND/MKCOL/PUT/DELETE）、Basic 认证头、中文路径段百分号编码与尾斜杠<br>**B91 [O6]** 备份文件名时间戳与 keep_daily 的“按天”在 TZ=Asia/Shanghai 下跨 UTC 午夜（X05） | DAV；长流程 DAV | [backend/routes/settings.py:307](../../backend/routes/settings.py#L307) |
| **R017** `POST /api/backup/test` | **S01** 回环 WebDAV 根与目录均存在<br>**S02** 缺多级目录逐级 MKCOL<br>**S03** ****** 使用保存密码 | **F01–07** A1–7<br>**F08** 非对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F09** 空对象→400 [L184](../../backend/routes/settings.py#L184)<br>**F10** 未知字段→400 [L186](../../backend/routes/settings.py#L186)<br>**F11** 文本字段错型→400 [L188](../../backend/routes/settings.py#L188)<br>**F12** auto_backup 错型→400 [L190](../../backend/routes/settings.py#L190)<br>**F13** 保留设置越界→400 [L193](../../backend/routes/settings.py#L193)<br>**F14** 缺地址→400 [L260](../../backend/routes/settings.py#L260)<br>**F15** 缺用户名→400 [L260](../../backend/routes/settings.py#L260)<br>**F16** 缺密码→400 [L260](../../backend/routes/settings.py#L260)<br>**F17** 根 check=false→400 [L274](../../backend/routes/settings.py#L274)<br>**F18** mkdir 失败→400 [L295](../../backend/routes/settings.py#L295)<br>**F19** 连接异常→500 [L302](../../backend/routes/settings.py#L302) | **B01** 中文 emoji 目录<br>**B02** 路径无前后斜杠自动补齐<br>**B90 [O6]** 回环 DAV 逐请求比方法序列（HEAD/PROPFIND/MKCOL/PUT/DELETE）、Basic 认证头、中文路径段百分号编码与尾斜杠 | DAV；长流程 DAV | [backend/routes/settings.py:242](../../backend/routes/settings.py#L242) |
| **R018** `DELETE /api/config-token` | **S01** 删除已有 token<br>**S02** 未设置时重复删除 | **F01–07** A1–7<br>**F08** 保存失败→500 [L167](../../backend/routes/settings.py#L167) | **B01** 删除后开启登录仍须 JWT | SYS:system_config.config_token；AUTO(T16/17) | [backend/routes/settings.py:109](../../backend/routes/settings.py#L109) |
| **R019** `GET /api/config-token` | **S01** 未设 token 返回空<br>**S02** 已设 token 返回管理员可见值 | **F01–07** A1–7 | **B01** 内部 capability 混入旧值时检查脱敏 | DB0 | [backend/routes/settings.py:109](../../backend/routes/settings.py#L109) |
| **R020** `POST /api/config-token` | **S01** token=synthetic-pull<br>**S02** generate=true 生成新 token<br>**S03** generate=true 优先于手填 token | **F01–07** A1–7<br>**F08** 缺 token→400 [L130](../../backend/routes/settings.py#L130)<br>**F09** 空白 token→400 [L130](../../backend/routes/settings.py#L130)<br>**F10** 含当前 rule_proxy_token→400 [L136](../../backend/routes/settings.py#L136)<br>**F11** 含退役 capability 的 URL 编码→400 [L136](../../backend/routes/settings.py#L136)<br>**F12** token 非字符串→500 [L154](../../backend/routes/settings.py#L154)<br>**F13** 写失败→500 [L154](../../backend/routes/settings.py#L154) | **B01** 中文 emoji token 保存与拉取配对<br>**B02** 8192 与 8193 长度的保存/认证差异<br>**B90 [O6]** 设置成功后再 import/reset 会轮换 rule_proxy_token；config_token 仍不得等于新/旧能力（T28） | SYS:system_config.config_token；AUTO(T16/17) | [backend/routes/settings.py:109](../../backend/routes/settings.py#L109) |
| **R021** `GET /api/config/<profile_id>/loon` | **S01** loon 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url<br>**B92 [O6]** Loon `Content-Disposition: inline; filename*=UTF-8''…`：名称全为非法字符/点→回退 profile_id→`loon`；空格、`%`、emoji 由 `quote` 编码（[config.py:105](../../backend/routes/config.py#L105)） | PULL；长流程 GEN-loon | [backend/routes/config.py:207](../../backend/routes/config.py#L207)<br>→ [backend/routes/config.py:112](../../backend/routes/config.py#L112) |
| **R022** `GET /api/config/<profile_id>/loon/<filename>` | **S01** loon 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url<br>**B92 [O6]** Loon `Content-Disposition: inline; filename*=UTF-8''…`：名称全为非法字符/点→回退 profile_id→`loon`；空格、`%`、emoji 由 `quote` 编码（[config.py:105](../../backend/routes/config.py#L105)） | PULL；长流程 GEN-loon | [backend/routes/config.py:207](../../backend/routes/config.py#L207)<br>→ [backend/routes/config.py:112](../../backend/routes/config.py#L112) |
| **R023** `GET /api/config/<profile_id>/mihomo` | **S01** mihomo 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** query base_url<br>**S06** GET JSON body base_url 兼容 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-mihomo | [backend/routes/config.py:178](../../backend/routes/config.py#L178)<br>→ [backend/routes/config.py:36](../../backend/routes/config.py#L36) |
| **R024** `GET /api/config/<profile_id>/mosdns` | **S01** mosdns 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-mosdns | [backend/routes/config.py:212](../../backend/routes/config.py#L212)<br>→ [backend/routes/config.py:144](../../backend/routes/config.py#L144) |
| **R025** `GET /api/config/<profile_id>/oxidns` | **S01** template 配置<br>**S02** native 配置<br>**S03** JWT<br>**S04** config_token query<br>**S05** 代理 Host/Proto | **F01–07** D1–7<br>**F08** 认证失败→401 [L189](../../backend/routes/config.py#L189)<br>**F09** ValueError→400 [L197](../../backend/routes/config.py#L197)<br>**F10** 其他异常→Flask500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文域名<br>**B02** 空规则集<br>**B03** token 必须不进入日志<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-oxidns | [backend/routes/config.py:184](../../backend/routes/config.py#L184) |
| **R026** `GET /api/config/<profile_id>/surge` | **S01** surge 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-surge | [backend/routes/config.py:201](../../backend/routes/config.py#L201)<br>→ [backend/routes/config.py:72](../../backend/routes/config.py#L72) |
| **R027** `GET /api/config/export` | **S01** 完整备份<br>**S02** desensitize=true<br>**S03** desensitize=TRUE | **F01–07** A1–7 | **B01** desensitize=1 不当 true<br>**B02** 含内部 capability 默认也过滤<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，`Range`→206 与 `If-None-Match`→304；比存在性与语义，不比 mtime 值，Go 若不支持则记入 O12 行为差异清单 | EXPORT | [backend/routes/config.py:239](../../backend/routes/config.py#L239) |
| **R028** `POST /api/config/import` | **S01** 当前 schema 完整备份<br>**S02** 原版单配置导入<br>**S03** schema2 多配置完整备份<br>**S90 [O6]** 导入/重置轮换 rule_proxy_token：旧值进入 retired（去重），旧能力访问 rule-proxy→401，新生成的 MosDNS/OxiDNS 内嵌新值（双射）（T28，[config_repository.py:1701](../../backend/common/config_repository.py#L1701)） | **F01–07** A1–7<br>**F08** 非对象→400 [L258](../../backend/routes/config.py#L258)<br>**F09** 不支持中间格式→400 [L266](../../backend/routes/config.py#L266)<br>**F10** schema/索引不一致→400 [L266](../../backend/routes/config.py#L266)<br>**F11** 缺默认 profile→400 [L266](../../backend/routes/config.py#L266)<br>**F12** 坏引用→400/409 按异常类<br>**F13** 正在发布或资源用途冲突→409 [L264](../../backend/routes/config.py#L264)<br>**F14** 存储错误→500 [L268](../../backend/routes/config.py#L268)<br>**F15** 坏 JSON→500 [L268](../../backend/routes/config.py#L268) | **B01** 空 shared 集合<br>**B02** 中文 emoji<br>**B03** 含未知字段保留<br>**B04** 多个 profile 引用同资源 | IMPORT-ALL；AUTO(T16/17) | [backend/routes/config.py:255](../../backend/routes/config.py#L255) |
| **R029** `GET /api/config/loon` | **S01** loon 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04–07** P3–6<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url<br>**B92 [O6]** Loon `Content-Disposition: inline; filename*=UTF-8''…`：名称全为非法字符/点→回退 profile_id→`loon`；空格、`%`、emoji 由 `quote` 编码（[config.py:105](../../backend/routes/config.py#L105)） | PULL；长流程 GEN-loon | [backend/routes/config.py:112](../../backend/routes/config.py#L112) |
| **R030** `GET /api/config/mihomo` | **S01** mihomo 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** query base_url<br>**S06** GET JSON body base_url 兼容 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04–07** P3–6<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url<br>**B93 [O6]** 不存在子路径 `/api/config/mihomo/x`：404/401 也写一条 WARNING 拉取日志（正则带 `(?:/[^/]+)?`，[config.py:216](../../backend/routes/config.py#L216)） | PULL；长流程 GEN-mihomo | [backend/routes/config.py:36](../../backend/routes/config.py#L36) |
| **R031** `GET /api/config/mosdns` | **S01** mosdns 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04–07** P3–6<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-mosdns | [backend/routes/config.py:144](../../backend/routes/config.py#L144) |
| **R032** `GET /api/config/oxidns` | **S01** template 配置<br>**S02** native 配置<br>**S03** JWT<br>**S04** config_token query<br>**S05** 代理 Host/Proto | **F01–07** D1–7<br>**F08** 认证失败→401 [L189](../../backend/routes/config.py#L189)<br>**F09** ValueError→400 [L197](../../backend/routes/config.py#L197)<br>**F10** 其他异常→Flask500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文域名<br>**B02** 空规则集<br>**B03** token 必须不进入日志<br>**B04–07** P3–6<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-oxidns | [backend/routes/config.py:184](../../backend/routes/config.py#L184) |
| **R033** `POST /api/config/reset` | **S01** 整个 schema 重置为默认<br>**S02** 再次重置<br>**S90 [O6]** 导入/重置轮换 rule_proxy_token：旧值进入 retired（去重），旧能力访问 rule-proxy→401，新生成的 MosDNS/OxiDNS 内嵌新值（双射）（T28，[config_repository.py:1701](../../backend/common/config_repository.py#L1701)） | **F01–07** A1–7<br>**F08** 运行中发布/受保护 router→409 [L282](../../backend/routes/config.py#L282)<br>**F09** 存储失败→500 [L284](../../backend/routes/config.py#L284) | **B01** 原有所有 profile/shared/system 均核对<br>**B02** 未知 body 被忽略 | RESET；AUTO(T16/17) | [backend/routes/config.py:273](../../backend/routes/config.py#L273) |
| **R034** `GET /api/config/surge` | **S01** surge 默认 profile 配置 JWT<br>**S02** config_token query 拉取<br>**S03** 认证关闭且无 config_token<br>**S04** 有反向代理 Host/Proto<br>**S05** 默认请求 host 生成 provider 地址 | **F01–07** D1–7<br>**F08** 认证失败→401<br>**F09** StrategyReferenceError→400<br>**F10** 生成异常→500<br>**F11–12** P1–2<br>**F90 [O6]** 未登录 + 不存在/非法 profile（路径段或 `?profile=`）→404/400 先于 401（X02） | **B01** 中文 emoji name<br>**B02** 空资源生成<br>**B03** 特殊 token 百分号编码<br>**B04–07** P3–6<br>**B90 [O6]** HEAD：与 GET 同 status/头、无 body；拉取日志只记 GET，HEAD 不记（[config.py:227](../../backend/routes/config.py#L227)）<br>**B91 [O6]** `X-Forwarded-Host/Proto` 多值逗号串、IPv6 `Host: [::1]:port`、HTTP/1.0 无 Host 原样拼入 base_url（waitress `clear_untrusted_proxy_headers=False`，[http_server.py:35](../../backend/http_server.py#L35)）；Mihomo 不读 X-Forwarded，只用 query/body base_url | PULL；长流程 GEN-surge | [backend/routes/config.py:72](../../backend/routes/config.py#L72) |
| **R035** `GET /api/custom-config/loon` | **S01** loon 默认配置<br>**S02** 已保存自定义文本 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 中文 emoji 与空配置<br>**B02–05** P3–6 | DB0；长流程 GEN-loon | [backend/routes/config.py:423](../../backend/routes/config.py#L423) |
| **R036** `POST /api/custom-config/loon` | **S01** config 文本<br>**S02** {} 保留原文 | **F01–07** A1–7<br>**F08** 坏 JSON→500 [L438](../../backend/routes/config.py#L438)<br>**F09** 保存异常→500 [L438](../../backend/routes/config.py#L438)<br>**F10–11** P1–2 | **B01** config 空字符串清空<br>**B02** 中文 emoji<br>**B03–06** P3–6 | PROFILE:loon；AUTO(T16/17) | [backend/routes/config.py:423](../../backend/routes/config.py#L423) |
| **R037** `GET /api/custom-config/mihomo` | **S01** mihomo 默认配置<br>**S02** 已保存自定义文本 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 中文 emoji 与空配置<br>**B02–05** P3–6 | DB0；长流程 GEN-mihomo | [backend/routes/config.py:314](../../backend/routes/config.py#L314) |
| **R038** `POST /api/custom-config/mihomo` | **S01** config YAML<br>**S02** config 空字符串恢复默认<br>**S03** easytier 合法<br>**S04** provider_update_interval=60 | **F01–07** A1–7<br>**F08** 刷新间隔非法→400 [L353](../../backend/routes/config.py#L353)<br>**F09** YAML 语法→400 [L375](../../backend/routes/config.py#L375)<br>**F10** IPv6 标志非法→400 [L370](../../backend/routes/config.py#L370)<br>**F11** 基础端口冲突→400 [L370](../../backend/routes/config.py#L370)<br>**F12** EasyTier 无效→400 [L381](../../backend/routes/config.py#L381)<br>**F13** 请求非对象/坏 JSON→500 [L386](../../backend/routes/config.py#L386)<br>**F14** save 异常→500 [L386](../../backend/routes/config.py#L386)<br>**F15–16** P1–2 | **B01** 空 config<br>**B02** 注释 YAML<br>**B03** 中文 emoji<br>**B04** 无关字段保存不触发基础配置旧值报错<br>**B05–08** P3–6 | PROFILE:mihomo；AUTO(T16/17) | [backend/routes/config.py:314](../../backend/routes/config.py#L314) |
| **R039** `GET /api/custom-config/mosdns` | **S01** mosdns 默认配置<br>**S02** 已保存自定义文本 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 中文 emoji 与空配置<br>**B02–05** P3–6 | DB0；长流程 GEN-mosdns | [backend/routes/config.py:442](../../backend/routes/config.py#L442) |
| **R040** `POST /api/custom-config/mosdns` | **S01** config 文本<br>**S02** {} 默认清空 | **F01–07** A1–7<br>**F08** 非对象→500 [L474](../../backend/routes/config.py#L474)<br>**F09** 坏 JSON→500 [L474](../../backend/routes/config.py#L474)<br>**F10** 保存异常→500 [L474](../../backend/routes/config.py#L474)<br>**F11–12** P1–2 | **B01** YAML 错文本可保存，生成时再校验<br>**B02–05** P3–6 | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/config.py:442](../../backend/routes/config.py#L442) |
| **R041** `GET /api/custom-config/oxidns` | **S01** oxidns 默认配置<br>**S02** 已保存自定义文本 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 中文 emoji 与空配置<br>**B02–05** P3–6 | DB0；长流程 GEN-oxidns | [backend/routes/config.py:479](../../backend/routes/config.py#L479) |
| **R042** `POST /api/custom-config/oxidns` | **S01** config YAML 文本<br>**S02** 空字符串 | **F01–07** A1–7<br>**F08** 非对象→400 [L488](../../backend/routes/config.py#L488)<br>**F09** config 缺失→400 [L488](../../backend/routes/config.py#L488)<br>**F10** 非文本→400 [L488](../../backend/routes/config.py#L488)<br>**F11** 归一失败→400 [L496](../../backend/routes/config.py#L496)<br>**F12–13** P1–2 | **B01** 中文 emoji<br>**B02** 扩展片段不应覆盖 native_config<br>**B03–06** P3–6 | PROFILE:oxidns；AUTO(T16/17) | [backend/routes/config.py:479](../../backend/routes/config.py#L479) |
| **R043** `GET /api/custom-config/surge` | **S01** surge 默认配置<br>**S02** 已保存自定义文本 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 中文 emoji 与空配置<br>**B02–05** P3–6 | DB0；长流程 GEN-surge | [backend/routes/config.py:391](../../backend/routes/config.py#L391) |
| **R044** `POST /api/custom-config/surge` | **S01** config 单字段<br>**S02** smart_groups 单字段<br>**S03** 两字段一起<br>**S04** {} 保持原值 | **F01–07** A1–7<br>**F08** 坏 JSON→500 [L418](../../backend/routes/config.py#L418)<br>**F09** 保存异常→500 [L418](../../backend/routes/config.py#L418)<br>**F10–11** P1–2 | **B01** smart_groups=[] 清空<br>**B02** config=null Python 可保存，生成另判<br>**B03–06** P3–6 | PROFILE:surge；AUTO(T16/17) | [backend/routes/config.py:391](../../backend/routes/config.py#L391) |
| **R045** `GET /api/domain-discovery/agents` | **S01** 合成启用域名发现的 Agent 状态<br>**S02** 无 Agent | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧版本/离线/禁用 Agent 的 supported 状态<br>**B02–05** P3–6 | DB0；长流程 DISCOVERY<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/domain_discovery.py:66](../../backend/routes/domain_discovery.py#L66) |
| **R046** `POST /api/domain-discovery/apply` | **S01** DOMAIN-SUFFIX direct<br>**S02** DOMAIN proxy<br>**S03** 多条 mixed<br>**S04** 已存在域名跳过 | **F01–07** A1–7<br>**F08** items 缺失/空→400<br>**F09** 501项→400<br>**F10** 项非对象→400<br>**F11** 域名无效→400<br>**F12** 类型非DOMAIN/DOMAIN-SUFFIX→400<br>**F13** target 无效→400<br>**F14** 默认库未设置→400<br>**F15** 默认库在写入前改变→400<br>**F16–17** P1–2 | **B01** 500 项<br>**B02** 中文 IDNA 域名<br>**B03** 重复采纳去重<br>**B04** history 最多1000<br>**B05–08** P3–6 | DISCOVERY-WRITE；AUTO(T16/17) | [backend/routes/domain_discovery.py:72](../../backend/routes/domain_discovery.py#L72) |
| **R047** `GET /api/domain-discovery/domains` | **S01** view=uncovered<br>**S02** view=failing<br>**S03** view=all<br>**S04** view=ignored<br>**S05** days=1<br>**S06** days=7<br>**S07** 指定 agent_id | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** days=abc 回7<br>**B02** days=0→1<br>**B03** days=2→7<br>**B04** 非法 view 回uncovered<br>**B05** 无报告→[]<br>**B06–09** P3–6<br>**B90 [O6]** days=" 1"、"1_0"、"-1"、"٧" 按 `int()` 宽松解析（X04） | DB0；长流程 DISCOVERY | [backend/routes/domain_discovery.py:49](../../backend/routes/domain_discovery.py#L49) |
| **R048** `GET /api/domain-discovery/history` | **S01** 有 manual 与 auto 历史<br>**S02** 空历史 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 1000 条截断<br>**B02** 中文 emoji 来源<br>**B03–06** P3–6 | DB0；长流程 DISCOVERY | [backend/routes/domain_discovery.py:92](../../backend/routes/domain_discovery.py#L92) |
| **R049** `DELETE /api/domain-discovery/ignore` | **S01** values=[example.com]<br>**S02** 重复值去重<br>**S03** 不存在值删除幂等 | **F01–07** A1–7<br>**F08** values 缺失/错型→400<br>**F09** 无效域名→400<br>**F10–11** P1–2 | **B01** 2000条<br>**B02** 中文域名<br>**B03** 空列表行为按 Python<br>**B04–07** P3–6 | PROFILE:domain_discovery.ignored；AUTO(T16/17) | [backend/routes/domain_discovery.py:105](../../backend/routes/domain_discovery.py#L105) |
| **R050** `POST /api/domain-discovery/ignore` | **S01** values=[example.com]<br>**S02** 重复值去重 | **F01–07** A1–7<br>**F08** values 缺失/错型→400<br>**F09** 无效域名→400<br>**F10** 超过2000条→400<br>**F11–12** P1–2 | **B01** 2000条<br>**B02** 中文域名<br>**B03** 空列表行为按 Python<br>**B04–07** P3–6 | PROFILE:domain_discovery.ignored；AUTO(T16/17) | [backend/routes/domain_discovery.py:98](../../backend/routes/domain_discovery.py#L98) |
| **R051** `POST /api/domain-discovery/probe` | **S01** domains=[example.com]→202<br>**S02** 多个域名 job | **F01–07** A1–7<br>**F08** domains 缺失/空→400<br>**F09** 非字符串或非法域名→400<br>**F10** 101项→400<br>**F11** 同profile已有任务→409 [L23](../../backend/routes/domain_discovery.py#L23)<br>**F12** 异profile已有任务→409 [L22](../../backend/routes/domain_discovery.py#L22)<br>**F13** 无合适Agent 作为异步job失败<br>**F14** 未设默认代理规则集作为异步job失败<br>**F15–16** P1–2 | **B01** 100域名<br>**B02** 同域名去重<br>**B03** 客户端中断仍完成<br>**B04–07** P3–6 | DISCOVERY-JOB；长流程 DISCOVERY<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/domain_discovery.py:112](../../backend/routes/domain_discovery.py#L112) |
| **R052** `GET /api/domain-discovery/probe/<job_id>` | **S01** running进度<br>**S02** done结果<br>**S03** failed结果 | **F01–07** A1–7<br>**F08** job不存在→404 [L126](../../backend/routes/domain_discovery.py#L126)<br>**F09** 其他profile的job→404 [L126](../../backend/routes/domain_discovery.py#L126)<br>**F10–11** P1–2 | **B01** 进程重启job丢失<br>**B02** 并发轮询不能泄漏别的profile<br>**B03–06** P3–6 | DB0；长流程 DISCOVERY | [backend/routes/domain_discovery.py:123](../../backend/routes/domain_discovery.py#L123) |
| **R053** `GET /api/domain-discovery/region` | **S01** 无记录默认状态<br>**S02** 有区域服务/域名记录 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 别的 profile Agent 记录不能混入<br>**B02–05** P3–6 | DB0；长流程 DISCOVERY | [backend/routes/domain_discovery.py:132](../../backend/routes/domain_discovery.py#L132) |
| **R054** `POST /api/domain-discovery/region/check` | **S01** services=true→202<br>**S02** services=false+domains→202 | **F01–07** A1–7<br>**F08** domains无效→400<br>**F09** 超MAX_DOMAINS_PER_CHECK→400<br>**F10** services=false且无domains→400<br>**F11** 同profile busy→409 [L23](../../backend/routes/domain_discovery.py#L23)<br>**F12** 异profile busy→409 [L22](../../backend/routes/domain_discovery.py#L22)<br>**F13** 未启区域检测作为job失败<br>**F14** 无Agent作为job失败<br>**F15** region_targets失败作为job失败<br>**F16–17** P1–2 | **B01** 一批12请求分段<br>**B02** 中文域名<br>**B03** 部分目标错误保留其他结果<br>**B04–07** P3–6 | DISCOVERY-JOB；长流程 DISCOVERY<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/domain_discovery.py:158](../../backend/routes/domain_discovery.py#L158) |
| **R055** `POST /api/domain-discovery/region/route` | **S01** kind=service 支持服务<br>**S02** kind=domain 域名转指定策略组 | **F01–07** A1–7<br>**F08** kind非法→400<br>**F09** 未知service→400<br>**F10** domain无效→400<br>**F11** policy不存在→400<br>**F12–13** P1–2 | **B01** 已有策略专用规则集复用<br>**B02** 中文emoji策略组<br>**B03–06** P3–6 | DISCOVERY-WRITE；AUTO(T16/17) | [backend/routes/domain_discovery.py:171](../../backend/routes/domain_discovery.py#L171) |
| **R056** `POST /api/domain-discovery/rulesets/init` | **S01** targets=[direct]<br>**S02** targets=[proxy]<br>**S03** targets=[direct,proxy]<br>**S04** Step B oracle更正：已创建再次调用仍新建库/引用，并切换默认选择 | **F01–07** A1–7<br>**F08** targets 非数组或含未知值→400（空列表按默认 direct+proxy）<br>**F09** proxy_policy 不存在→400<br>**F10–11** P1–2 | **B01** 中文 emoji 同名库避让<br>**B02** 引用插在 MATCH 前<br>**B03–06** P3–6 | DISCOVERY-WRITE；AUTO(T16/17) | [backend/routes/domain_discovery.py:40](../../backend/routes/domain_discovery.py#L40) |
| **R057** `GET /api/domain-discovery/settings` | **S01** 默认配置<br>**S02** 已有 direct/proxy library 与 usage | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 失效默认库条目显示 problem<br>**B02** 空策略列表<br>**B03–06** P3–6 | DB0 | [backend/routes/domain_discovery.py:28](../../backend/routes/domain_discovery.py#L28) |
| **R058** `PUT /api/domain-discovery/settings` | **S01** 设置两个内容型库<br>**S02** auto_probe/auto_apply 开关<br>**S03** 置信度80<br>**S04** 置信度90<br>**S05** 置信度95<br>**S06** region_probe_port=18082<br>**S07** null 清默认库 | **F01–07** A1–7<br>**F08** 非对象→400<br>**F09** 默认库不存在/不是内容型→400<br>**F10** auto_probe 非bool→400<br>**F11** auto_apply 非bool→400<br>**F12** 置信度无效→400<br>**F13** region_check_enabled 非bool→400<br>**F14** port 非整数/bool→400<br>**F15** port<1024→400<br>**F16** port>65535→400<br>**F17** port 已被 Mihomo 占用→400<br>**F18–19** P1–2 | **B01** 1024<br>**B02** 65535<br>**B03** 未知键忽略<br>**B04–07** P3–6 | PROFILE:domain_discovery；AUTO(T16/17) | [backend/routes/domain_discovery.py:34](../../backend/routes/domain_discovery.py#L34) |
| **R059** `POST /api/domain-discovery/undo` | **S01** 撤销 direct 行<br>**S02** 撤销 proxy 行<br>**S03** 撤销不存在行按 Python 幂等 | **F01–07** A1–7<br>**F08** value 非域名→400<br>**F09** target 非法→400<br>**F10–11** P1–2 | **B01** 只删除目标域名行保留注释和其他行<br>**B02–05** P3–6 | DISCOVERY-WRITE；AUTO(T16/17) | [backend/routes/domain_discovery.py:84](../../backend/routes/domain_discovery.py#L84) |
| **R060** `POST /api/domain-discovery/website/identify` | **S01** 最近有Agent采集域名→202<br>**S02** 多个域名识别 | **F01–07** A1–7<br>**F08** domains缺失/无效→400<br>**F09** 超批量限制→400<br>**F10** 无合适Agent→400 [L146](../../backend/routes/domain_discovery.py#L146)<br>**F11** 域名无最近采集记录→400<br>**F12** 同profile busy→409 [L23](../../backend/routes/domain_discovery.py#L23)<br>**F13** 异profile busy→409 [L22](../../backend/routes/domain_discovery.py#L22)<br>**F14–15** P1–2 | **B01** 中文IDNA<br>**B02** 空domains<br>**B03** 不同website标题编码<br>**B04–07** P3–6 | DISCOVERY-JOB；长流程 DISCOVERY<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/domain_discovery.py:138](../../backend/routes/domain_discovery.py#L138) |
| **R061** `PUT /api/domain-discovery/website/note` | **S01** 保存note<br>**S02** 空白note删除<br>**S03** 修改已有note | **F01–07** A1–7<br>**F08** 非对象→400<br>**F09** 无效domain→400<br>**F10** note非字符串→400<br>**F11** note201字符→400<br>**F12** 新note超过2000条→400<br>**F13–14** P1–2 | **B01** 200字符中文emoji<br>**B02** 2000已有项仍可更新<br>**B03–06** P3–6 | PROFILE:domain_discovery.website_notes；AUTO(T16/17) | [backend/routes/domain_discovery.py:152](../../backend/routes/domain_discovery.py#L152) |
| **R062** `POST /api/generate/<service>/artifact` | **S01** service=mihomo<br>**S02** service=surge<br>**S03** service=loon<br>**S04** service=mosdns<br>**S05** service=oxidns<br>**S90 [O6]** mihomo artifact 返回 `subscription_hosts`（含 `dns.applied`，以及从 fake Agent 心跳上报的旧 Mihomo 版本算出的 `dns.outdated_agents`）与 `subscription_dialers`；其他 service 两字段为 null（[generate.py:24](../../backend/routes/generate.py#L24)、[generate.py:76](../../backend/routes/generate.py#L76)） | **F01–07** A1–7<br>**F08** 不支持 service→400 [L49](../../backend/routes/generate.py#L49)<br>**F09** 子生成接口错误原样传播 [L52](../../backend/routes/generate.py#L52)<br>**F10** 产物过大→helper<br>**F11** 预览保存不可写→Flask500<br>**F12–13** P1–2 | **B01** 24 份旧预览驱逐<br>**B02** TTL 3600 秒<br>**B03** 中文 emoji 文件名<br>**B04–07** P3–6<br>**B90 [O6]** 产物正文含 rule-proxy 能力 URL：`content` 按端点豁免保持原文，响应中其他字段里的同一能力变成 `[REDACTED]`（X01） | ARTIFACT；长流程 ARTIFACT | [backend/routes/generate.py:38](../../backend/routes/generate.py#L38) |
| **R063** `GET /api/generate/artifacts/<preview_id>/<action>` | **S01** action=download ZIP<br>**S02** action=file path=config.yaml<br>**S03** action=file 附加文件 | **F01–07** A1–7<br>**F08** action 未知→404 [L89](../../backend/routes/generate.py#L89)<br>**F09** preview_id 非32位 hex→409<br>**F10** 不存在 preview→409<br>**F11** 过期→409<br>**F12** metadata 坏→409<br>**F13** bundle hash 错→409<br>**F14** 非 manual 预览→409<br>**F15** file path 缺失→404<br>**F16** file 不存在→404<br>**F17–18** P1–2 | **B01** ../ 路径不能读预览外文件<br>**B02** 下载后改配置再读仍是同份冻结字节<br>**B03–06** P3–6 | ARTIFACT-READ；长流程 ARTIFACT | [backend/routes/generate.py:83](../../backend/routes/generate.py#L83) |
| **R064** `POST /api/generate/loon` | **S01** 当前 profile loon 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05–08** P3–6<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:loon；长流程 GEN-loon | [backend/routes/generate.py:147](../../backend/routes/generate.py#L147) |
| **R065** `POST /api/generate/loon/preview` | **S01** loon preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04–07** P3–6 | PREVIEW:loon；长流程 GEN-loon | [backend/routes/generate.py:245](../../backend/routes/generate.py#L245) |
| **R066** `POST /api/generate/mihomo` | **S01** 当前 profile mihomo 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05–08** P3–6<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:mihomo；长流程 GEN-mihomo | [backend/routes/generate.py:94](../../backend/routes/generate.py#L94) |
| **R067** `POST /api/generate/mihomo/preview` | **S01** mihomo preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04–07** P3–6 | PREVIEW:mihomo；长流程 GEN-mihomo | [backend/routes/generate.py:204](../../backend/routes/generate.py#L204) |
| **R068** `POST /api/generate/mosdns` | **S01** 当前 profile mosdns 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** MosdnsArchiveError→500 [L195](../../backend/routes/generate.py#L195)<br>**F09** 一般异常含脱敏详情→500 [L199](../../backend/routes/generate.py#L199)<br>**F10** 坏 JSON→500<br>**F11–12** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05–08** P3–6<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:mosdns；长流程 GEN-mosdns | [backend/routes/generate.py:169](../../backend/routes/generate.py#L169) |
| **R069** `POST /api/generate/mosdns/preview` | **S01** mosdns preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** 其他异常→500 [L274](../../backend/routes/generate.py#L274)<br>**F09–10** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04–07** P3–6 | PREVIEW:mosdns；长流程 GEN-mosdns | [backend/routes/generate.py:262](../../backend/routes/generate.py#L262) |
| **R070** `POST /api/generate/oxidns` | **S01** 当前 profile oxidns 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** OxidnsSettingsError/ValueError→400 [L296](../../backend/routes/generate.py#L296)<br>**F09** 其他异常脱敏→500 [L300](../../backend/routes/generate.py#L300)<br>**F10–11** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05–08** P3–6<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:oxidns；长流程 GEN-oxidns | [backend/routes/generate.py:279](../../backend/routes/generate.py#L279) |
| **R071** `POST /api/generate/oxidns/preview` | **S01** oxidns preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** ValueError→400 [L318](../../backend/routes/generate.py#L318)<br>**F09** 其他异常脱敏→500 [L320](../../backend/routes/generate.py#L320)<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04–07** P3–6 | PREVIEW:oxidns；长流程 GEN-oxidns | [backend/routes/generate.py:305](../../backend/routes/generate.py#L305) |
| **R072** `POST /api/generate/surge` | **S01** 当前 profile surge 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05–08** P3–6<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:surge；长流程 GEN-surge | [backend/routes/generate.py:122](../../backend/routes/generate.py#L122) |
| **R073** `POST /api/generate/surge/preview` | **S01** surge preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04–07** P3–6 | PREVIEW:surge；长流程 GEN-surge | [backend/routes/generate.py:226](../../backend/routes/generate.py#L226) |
| **R074** `POST /api/logs/clear` | **S01** 清空已有日志<br>**S02** 不存在返回成功 | **F01–07** A1–7<br>**F08** 不可写→500 [L179](../../backend/routes/logs.py#L179) | **B01** 重复清空<br>**B92 [O6]** LOG_FILE 指向不存在文件时按 `./app.log`、`data/app.log`… 相对 CWD 回退（[logs.py:27](../../backend/routes/logs.py#L27)）；两端 CWD 相同后比较 | LOG；长流程 LOG | [backend/routes/logs.py:155](../../backend/routes/logs.py#L155) |
| **R075** `GET /api/logs/info` | **S01** 存在文件统计<br>**S02** 文件不存在 exists=false | **F01–07** A1–7<br>**F08** stat 权限异常→500 [L151](../../backend/routes/logs.py#L151) | **B01** 空文件 size=0<br>**B90 [O6]** `modified/created` 为无时区本地时间 isoformat（微秒为 0 时省略小数）；`path` 按 DATA_DIR/LOG_FILE 映射比较（X05）<br>**B92 [O6]** LOG_FILE 指向不存在文件时按 `./app.log`、`data/app.log`… 相对 CWD 回退（[logs.py:27](../../backend/routes/logs.py#L27)）；两端 CWD 相同后比较 | LOG；长流程 LOG | [backend/routes/logs.py:122](../../backend/routes/logs.py#L122) |
| **R076** `GET /api/logs/tail` | **S01** 默认末 100 行<br>**S02** search 大小写不敏感<br>**S03** level=warning<br>**S04** 文件缺失返回 success | **F01–07** A1–7<br>**F08** level=[ 正则错误→500 [L118](../../backend/routes/logs.py#L118)<br>**F09** 读取权限错误→500 [L118](../../backend/routes/logs.py#L118) | **B01** lines=0 全量<br>**B02** lines=-1 全量<br>**B03** lines=10001 截断 10000<br>**B04** lines=abc 回默认<br>**B05** 中文 emoji 和非法 UTF-8 忽略<br>**B90 [O6]** lines=" 5"、"+5"、"5_0"、"٥"（阿拉伯-印度数字）、"99999999999999999999" 按 Python `int()` 宽松解析（X04）<br>**B91 [O6]** 不用固定文件：先发一组请求让各端自己写日志，再 tail——等级词必须能被 `\bWARNING\b` 过滤命中，关键中文事件都在，密钥不出现（X07）<br>**B92 [O6]** LOG_FILE 指向不存在文件时按 `./app.log`、`data/app.log`… 相对 CWD 回退（[logs.py:27](../../backend/routes/logs.py#L27)）；两端 CWD 相同后比较 | LOG；长流程 LOG | [backend/routes/logs.py:46](../../backend/routes/logs.py#L46) |
| **R077** `GET /api/mosdns/api-settings` | **S01** 默认 api_enabled/api_address<br>**S02** 已保存 api_enabled/api_address | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:294](../../backend/routes/mosdns.py#L294) |
| **R078** `POST /api/mosdns/api-settings` | **S01** 合法 api_enabled/api_address<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:294](../../backend/routes/mosdns.py#L294) |
| **R079** `GET /api/mosdns/cache-settings` | **S01** 默认 cache_enabled/cache_size/cache_lazy_ttl/cache_dump_enabled/cache_dump_file/cache_dump_interval<br>**S02** 已保存 cache_enabled/cache_size/cache_lazy_ttl/cache_dump_enabled/cache_dump_file/cache_dump_interval | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:338](../../backend/routes/mosdns.py#L338) |
| **R080** `POST /api/mosdns/cache-settings` | **S01** 合法 cache_enabled/cache_size/cache_lazy_ttl/cache_dump_enabled/cache_dump_file/cache_dump_interval<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正）<br>**B90 [O6]** `to_int`：true→1、3.9→3、" 12 "→12、"1_000"→1000、1e400→OverflowError→默认、"inf"→默认、cache_dump_interval=0→300（[mosdns.py:67](../../backend/routes/mosdns.py#L67)，X04） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:338](../../backend/routes/mosdns.py#L338) |
| **R081** `GET /api/mosdns/custom-matches` | **S01** 默认 custom_matches 与 position<br>**S02** 已保存 custom_matches 与 position | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:164](../../backend/routes/mosdns.py#L164) |
| **R082** `POST /api/mosdns/custom-matches` | **S01** 合法 custom_matches 与 position<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:164](../../backend/routes/mosdns.py#L164) |
| **R083** `GET /api/mosdns/dns-servers` | **S01** 默认 local_dns/remote_dns/fallback_dns/default_forward/custom_hosts<br>**S02** 已保存 local_dns/remote_dns/fallback_dns/default_forward/custom_hosts | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:205](../../backend/routes/mosdns.py#L205) |
| **R084** `POST /api/mosdns/dns-servers` | **S01** 合法 local_dns/remote_dns/fallback_dns/default_forward/custom_hosts<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:205](../../backend/routes/mosdns.py#L205) |
| **R085** `GET /api/mosdns/log-settings` | **S01** 默认 log_enabled/log_level/log_file<br>**S02** 已保存 log_enabled/log_level/log_file | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:250](../../backend/routes/mosdns.py#L250) |
| **R086** `POST /api/mosdns/log-settings` | **S01** 合法 log_enabled/log_level/log_file<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:250](../../backend/routes/mosdns.py#L250) |
| **R087** `GET /api/mosdns/rule-proxy` | **S01** JWT 授权<br>**S02** 当前内部 token query 授权<br>**S03** config_token query 授权<br>**S04** 本 profile inline 内容不走网络<br>**S05** 声明 URL 验证缓存<br>**S06** 未声明 URL 下载<br>**S07** part=domain<br>**S08** part=ip<br>**S09** 无 part 混合<br>**S90 [O6]** 声明源有有效缓存且上次定时更新失败→直接返回缓存（不下载）+ `X-ConfigFlow-Rule-Cache: stale`；声明源无缓存且下载失败→502 `RULE_SOURCE_UNAVAILABLE`（[mosdns.py:556](../../backend/routes/mosdns.py#L556)、[scheduled_refresh.py:214](../../backend/utils/scheduled_refresh.py#L214)）<br>**S91 [O6]** `github_proxy_domain` 改写下载地址 + `rule_fetch_proxy` 作 HTTP 代理：fake 代理记录实际请求行与目标 | **F01** C1 未配置管理/公开拉取凭据且未提供内部能力或JWT仍401（内部能力由仓库自动生成，不能假设不存在）<br>**F02** C2当前rule_proxy_token放Bearer不授权<br>**F03** C3退役token即使配置认证关闭仍401<br>**F04** 无凭据→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F05** 退役能力→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F06** 内部 token 仅放 Bearer→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F07** 缺 url→400 [L534](../../backend/routes/mosdns.py#L534)<br>**F08** part 非法→400 [L537](../../backend/routes/mosdns.py#L537)<br>**F09** 非法远端 URL→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F10** 远端 non2xx→502 [L577](../../backend/routes/mosdns.py#L577)<br>**F11** 重定向过多→400 Invalid remote URL [L591](../../backend/routes/mosdns.py#L591)<br>**F12** 声明 MRS→422 [L588](../../backend/routes/mosdns.py#L588)<br>**F13** 声明内容校验失败→422 [L588](../../backend/routes/mosdns.py#L588)<br>**F14** Content-Length 超限→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F15** 实际流超限→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F16** 未知处理异常→500 [L594](../../backend/routes/mosdns.py#L594)<br>**F17–18** P1–2 | **B01** BOM 和中文域名<br>**B02** 5MiB 边界<br>**B03** 本地跨 profile URL 不误解析<br>**B04** 坏 UTF8 按来源路径处理<br>**B05–08** P3–6<br>**B90 [O6]** `source_id` 指向声明源 / 指向别的源 / 不存在<br>**B91 [O6]** inline 内容源被禁用（共享源 enabled=false，解析引用得到 `library_enabled=false`）或 URL 带 query 时不走 inline 快捷解析；相对 URL 随后400，绝对面板自身 API URL 由 request_local 进程内分发（Step B oracle 更正） | RULE-FILE；AUTO(T16/17)；长流程 RULE-PROXY<br>**[O6] 更正**：本端点不提交配置，AUTO(T16/17) 只断言**不**触发备份；只比较规则缓存与 task-status | [backend/routes/mosdns.py:493](../../backend/routes/mosdns.py#L493) |
| **R088** `GET /api/mosdns/rulesets` | **S01** 默认 direct_rulesets/proxy_rulesets/direct_rules/proxy_rules<br>**S02** 已保存 direct_rulesets/proxy_rulesets/direct_rules/proxy_rules | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 旧字段与空值按 Python 返回<br>**B02** P3无公开URL别名，N/A<br>**B03–05** P4–6（Step B oracle更正） | DB0 | [backend/routes/mosdns.py:121](../../backend/routes/mosdns.py#L121) |
| **R089** `POST /api/mosdns/rulesets` | **S01** 合法 direct_rulesets/proxy_rulesets/direct_rules/proxy_rules<br>**S02** 空对象恢复这些字段默认值 | **F01–07** A1–7<br>**F08** 数组请求按 Python 处理<br>**F09** 坏 JSON→500<br>**F10** 非 JSON Content-Type→500<br>**F11** 保存异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 文本<br>**B02** 空列表<br>**B03** 错型字段有些会被保存，须读回再生成验证<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:121](../../backend/routes/mosdns.py#L121) |
| **R090** `POST /api/mosdns/settings` | **S01** 完整编辑器 draft 一次提交<br>**S02** {} 默认值<br>**S03** 数字文本 cache 值归一 | **F01–07** A1–7<br>**F08** 非对象→400 [L93](../../backend/routes/mosdns.py#L93)<br>**F09** 无效资源引用→repo<br>**F10** 存储冲突→repo<br>**F11–12** P1–2 | **B01** 未知已存字段保留<br>**B02** api_addr 被移除<br>**B03** 负 dump interval 恢复300<br>**B04** P3无公开URL别名，N/A<br>**B05–07** P4–6（Step B oracle更正）<br>**B90 [O6]** `to_int`：true→1、3.9→3、" 12 "→12、"1_000"→1000、1e400→OverflowError→默认、"inf"→默认、cache_dump_interval=0→300（[mosdns.py:67](../../backend/routes/mosdns.py#L67)，X04） | PROFILE:mosdns；AUTO(T16/17) | [backend/routes/mosdns.py:89](../../backend/routes/mosdns.py#L89) |
| **R091** `GET /api/nodes` | **S01** 共享手动与订阅节点列表<br>**S02** 空列表 | **F01–07** A1–7 | **B01** 中文 emoji 与带引号名称 | DB0 | [backend/routes/nodes.py:14](../../backend/routes/nodes.py#L14) |
| **R092** `POST /api/nodes` | **S01** 显式 id 的 socks5 节点<br>**S02** 省略 id 自动生成<br>**S03** 结构化 mihomo 节点 | **F01–07** A1–7<br>**F08** 非对象→400 [L24](../../backend/routes/nodes.py#L24)<br>**F09** dialer_ref 即使 null→400 [L29](../../backend/routes/nodes.py#L29)<br>**F10** 重复 ID→repo<br>**F11** 节点所属订阅不存在→repo | **B01** 中文 emoji 名称<br>**B02** 未知扩展字段保留<br>**B03** 空对象由 repo 校验 | SHARED:nodes；AUTO(T16/17) | [backend/routes/nodes.py:14](../../backend/routes/nodes.py#L14) |
| **R093** `DELETE /api/nodes/<node_id>` | **S01** 无引用节点删除 | **F01–07** A1–7<br>**F08** 节点不存在→404 [L93](../../backend/routes/nodes.py#L93)<br>**F09** 普通组引用→repo409<br>**F10** 代理链引用→repo409<br>**F11** 聚合引用→repo409 | **B01** 重复删除仍 404 | SHARED:nodes；AUTO(T16/17) | [backend/routes/nodes.py:79](../../backend/routes/nodes.py#L79) |
| **R094** `PUT /api/nodes/<node_id>` | **S01** 替换已有节点保持 id<br>**S02** 省略 id 使用路径值 | **F01–07** A1–7<br>**F08** 非对象/null→400 [L86](../../backend/routes/nodes.py#L86)<br>**F09** dialer_ref→400 [L88](../../backend/routes/nodes.py#L88)<br>**F10** 节点不存在→404 [L93](../../backend/routes/nodes.py#L93)<br>**F11** 修改 ID→400 [L96](../../backend/routes/nodes.py#L96)<br>**F12** 被链引用的节点改坏→repo | **B01** 中文 emoji<br>**B02** 删除旧未知字段遵循整对象替换 | SHARED:nodes；AUTO(T16/17) | [backend/routes/nodes.py:79](../../backend/routes/nodes.py#L79) |
| **R095** `POST /api/nodes/delete` | **S01** 无引用 ids 的预览/确认删除<br>**S02** 普通组与聚合引用的预览/确认解绑 | **F01–07** A1–7<br>**F08** 非对象→400 [L63](../../backend/routes/nodes.py#L63)<br>**F09** ids 缺失或空→helper<br>**F10** 非字符串 ID→400（重复字符串 ID 按 set 去重）<br>**F11** 节点不存在→404 [L74](../../backend/routes/nodes.py#L74)<br>**F12** 缺 confirmation（仅 delete）→repo400<br>**F13** confirmation 过时（仅 delete）→409 [L72](../../backend/routes/nodes.py#L72)<br>**F14** 代理链 blocker（仅 delete）→repo409 | **B01** 批量 ids=[] 拒绝<br>**B02** 另 profile 新增引用使确认失效 | DELETE-NODE；AUTO(T16/17) | [backend/routes/nodes.py:58](../../backend/routes/nodes.py#L58) |
| **R096** `POST /api/nodes/delete-preview` | **S01** 无引用 ids 的预览/确认删除<br>**S02** 普通组与聚合引用的预览/确认解绑 | **F01–07** A1–7<br>**F08** 非对象→400 [L63](../../backend/routes/nodes.py#L63)<br>**F09** ids 缺失或空→helper<br>**F10** 非字符串 ID→400（重复字符串 ID 按 set 去重）<br>**F11** 节点不存在→404 [L74](../../backend/routes/nodes.py#L74) | **B01** 批量 ids=[] 拒绝<br>**B02** 另 profile 新增引用使确认失效 | DELETE-NODE；AUTO(T16/17) | [backend/routes/nodes.py:58](../../backend/routes/nodes.py#L58) |
| **R097** `GET /api/nodes/latency` | **S01** 无历史 results={}<br>**S02** 固定历史 results | **F01–07** A1–7 | **B01** 损坏 results 文件恢复为空 | PROBE；长流程 PROBE | [backend/routes/nodes.py:140](../../backend/routes/nodes.py#L140) |
| **R098** `POST /api/nodes/latency` | **S01** {} 全部<br>**S02** names 指定唯一名<br>**S03** keys 指定来源<br>**S04** keys=[] | **F01–07** A1–7<br>**F08** 非对象→400 [L151](../../backend/routes/nodes.py#L151)<br>**F09** names 非字符串数组→400 [L157](../../backend/routes/nodes.py#L157)<br>**F10** keys 非字符串数组→400 [L161](../../backend/routes/nodes.py#L161)<br>**F11** 同时 names/keys→400 [L163](../../backend/routes/nodes.py#L163)<br>**F12** 忙→409 [L168](../../backend/routes/nodes.py#L168)<br>**F13** 未装 probe→503 [L170](../../backend/routes/nodes.py#L170)<br>**F14** 无效设置→503 [L170](../../backend/routes/nodes.py#L170)<br>**F15** 过多节点→400 [L172](../../backend/routes/nodes.py#L172)<br>**F16** 同名未用 key→400 [L172](../../backend/routes/nodes.py#L172)<br>**F17** 过多 Sub-Store 转换→400 [L172](../../backend/routes/nodes.py#L172)<br>**F18** probe 超时/退出异常→503 [L170](../../backend/routes/nodes.py#L170) | **B01** 中文 emoji 和同名不同来源<br>**B02** 单批20节点和5个需转换节点的上限边界 | PROBE；长流程 PROBE | [backend/routes/nodes.py:140](../../backend/routes/nodes.py#L140) |
| **R099** `POST /api/nodes/latency/dns/check` | **S01** dns.mode=system 的固定 probe 结果<br>**S02** custom IP DNS 的固定结果 | **F01–07** A1–7<br>**F08** 缺 dns→400 [L211](../../backend/routes/nodes.py#L211)<br>**F09** 非对象→400 [L211](../../backend/routes/nodes.py#L211)<br>**F10** DNS 参数无效→400 [L211](../../backend/routes/nodes.py#L211)<br>**F11** probe 不可用→503 [L213](../../backend/routes/nodes.py#L213)<br>**F12** 结果结构错误→503 [L213](../../backend/routes/nodes.py#L213) | **B01** 空 servers 配 system<br>**B02** 多个 DNS 的稳定顺序 | PROBE；长流程 PROBE | [backend/routes/nodes.py:202](../../backend/routes/nodes.py#L202) |
| **R100** `GET /api/nodes/latency/settings` | **S01** 系统 DNS<br>**S02** 自定义 DNS | **F01–07** A1–7<br>**F08** 无效持久设置走默认还是错误按 helper | **B01** 旧 dns_servers 配置 | DB0 | [backend/routes/nodes.py:178](../../backend/routes/nodes.py#L178) |
| **R101** `POST /api/nodes/latency/settings` | **S01** dns.mode=system<br>**S02** dns.mode=custom 配 IP<br>**S03** 兼容 dns_servers 数组<br>**S04** dns_servers=[] 回 system | **F01–07** A1–7<br>**F08** 非对象→400 [L197](../../backend/routes/nodes.py#L197)<br>**F09** 缺 dns/dns_servers→400 [L197](../../backend/routes/nodes.py#L197)<br>**F10** 非法 mode→400 [L197](../../backend/routes/nodes.py#L197)<br>**F11** custom 空 servers→400 [L197](../../backend/routes/nodes.py#L197)<br>**F12** 域名 DoH 缺 bootstrap→400 [L197](../../backend/routes/nodes.py#L197)<br>**F13** 非法 DNS URL/端口→400 [L197](../../backend/routes/nodes.py#L197) | **B01** IPv4:端口<br>**B02** 重复 DNS 去重 | SYS:node_test_dns；AUTO(T16/17) | [backend/routes/nodes.py:178](../../backend/routes/nodes.py#L178) |
| **R102** `POST /api/nodes/parse-lines` | **S01** 有效 Surge snell 行<br>**S02** 有效 Loon 行<br>**S03** 空 lines=[] | **F01–07** A1–7<br>**F08** 非对象→400 [L45](../../backend/routes/nodes.py#L45)<br>**F09** lines 缺失→400 [L45](../../backend/routes/nodes.py#L45)<br>**F10** lines 非数组→400 [L45](../../backend/routes/nodes.py#L45)<br>**F11** 数组含数字→400 [L45](../../backend/routes/nodes.py#L45)<br>**F12** 501 行→400 [L47](../../backend/routes/nodes.py#L47)<br>**F13** 未知行返回 results[].error 而非 HTTP 失败 | **B01** 500 行<br>**B02** 空白行逐项 error<br>**B03** 中文 emoji 行名<br>**B90 [O6]** 两端设同一 `SUB_STORE_URL` 指向回环 fake Sub-Store，比临时订阅创建/下载/删除的请求序列；内置 runtime 不可用时的错误 | SUBSTORE；AUTO(T16/17)；长流程 PROBE | [backend/routes/nodes.py:38](../../backend/routes/nodes.py#L38) |
| **R103** `POST /api/nodes/reorder` | **S01** ids 移到 top<br>**S02** ids 移到 bottom<br>**S03** 旧 nodes 对象数组只取顺序 | **F01–07** A1–7<br>**F08** ids 非数组→404 [L122](../../backend/routes/nodes.py#L122)<br>**F09** 重复 ID→404 [L122](../../backend/routes/nodes.py#L122)<br>**F10** ID 非字符串→404 [L122](../../backend/routes/nodes.py#L122)<br>**F11** 缺旧对象数组→404 [L122](../../backend/routes/nodes.py#L122)<br>**F12** 未知 ID→404 [L122](../../backend/routes/nodes.py#L122)<br>**F13** 非法引用→400 [L129](../../backend/routes/nodes.py#L129)<br>**F14** 非对象请求→500 [L135](../../backend/routes/nodes.py#L135) | **B01** 空 ids 不变<br>**B02** position 非 top 按 bottom<br>**B03** 旧对象字段不得覆盖 | SHARED:nodes；AUTO(T16/17) | [backend/routes/nodes.py:113](../../backend/routes/nodes.py#L113) |
| **R104** `GET /api/notifications` | **S01** 空通知<br>**S02** 来自订阅失败、任务和配置变更的合成通知 | **F01–07** A1–7<br>**F08** OSError→503 [L17](../../backend/routes/notifications.py#L17)<br>**F09** ValueError→503 [L17](../../backend/routes/notifications.py#L17) | **B01** 已读版本与新版本的区别<br>**B02** 中文 emoji 名称 | NOTICE；长流程 NOTICE | [backend/routes/notifications.py:13](../../backend/routes/notifications.py#L13) |
| **R105** `POST /api/notifications/read` | **S01** versions={}<br>**S02** 已知 ID/版本标读<br>**S03** 未知 ID/版本按 Python 保存 | **F01–07** A1–7<br>**F08** 缺 versions→400 [L31](../../backend/routes/notifications.py#L31)<br>**F09** versions=[]→400 [L31](../../backend/routes/notifications.py#L31)<br>**F10** 2001 项→400 [L31](../../backend/routes/notifications.py#L31)<br>**F11** 键 1025 字符→400 [L31](../../backend/routes/notifications.py#L31)<br>**F12** 值非字符串→400 [L31](../../backend/routes/notifications.py#L31)<br>**F13** 值 101 字符→400 [L31](../../backend/routes/notifications.py#L31) | **B01** 2000 项<br>**B02** 键 1024 字符<br>**B03** 值 100 字符 | NOTICE | [backend/routes/notifications.py:25](../../backend/routes/notifications.py#L25) |
| **R106** `POST /api/oxidns/common-settings` | **S01** 默认 native 控件识别<br>**S02** 自定义 native 能力状态只读 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 缺标准插件给 capability 原因<br>**B02** 空 changes 不涉及<br>**B03–06** P3–6 | DB0 | [backend/routes/oxidns.py:157](../../backend/routes/oxidns.py#L157) |
| **R107** `POST /api/oxidns/common-settings/apply` | **S01** 改 cache<br>**S02** 改 upstream<br>**S03** 改路由<br>**S04** 改 AdGuard/设备策略<br>**S05** 非空同值 changes 幂等；{} 实际拒绝400，见 H369（Step B oracle 更正） | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** changes 非对象→helper<br>**F13** 未知控件键→helper<br>**F14** 无法识别共享处理链拒绝→helper<br>**F15–16** P1–2 | **B01** 中文 emoji 设备备注<br>**B02** 空列表清除<br>**B03** 仅返回草稿不能落库<br>**B04–07** P3–6 | DB0 | [backend/routes/oxidns.py:169](../../backend/routes/oxidns.py#L169) |
| **R108** `POST /api/oxidns/editor` | **S01** template 转 unified native 草稿<br>**S02** 已有 native 编辑器准备 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 空模板<br>**B02** 注释和额外文件语义保留<br>**B03–06** P3–6 | DB0 | [backend/routes/oxidns.py:132](../../backend/routes/oxidns.py#L132) |
| **R109** `POST /api/oxidns/native-settings/apply` | **S01** native_config 完整表单编辑<br>**S02** renames tag 联动<br>**S03** standard_add 标准功能新增<br>**S04** 安全删除未被引用插件 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** native_config 非文本→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F13** renames 非映射→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F14** standard_add 重复/错型→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F15** 重命名覆盖现有 tag→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F16** include/动态引用无法安全改→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F17** 删除后仍被引用→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F18** 新增依赖同时被删→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F19–20** P1–2 | **B01** 中文 emoji tag<br>**B02** 空 renames<br>**B03** references 字段必须逐路径比较<br>**B04–07** P3–6 | DB0 | [backend/routes/oxidns.py:181](../../backend/routes/oxidns.py#L181) |
| **R110** `POST /api/oxidns/render` | **S01** settings template 渲染<br>**S02** settings native 渲染<br>**S03** 省略 base_url 使用请求根 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 空附加文件列表<br>**B02** 含 capability 的 config 不被通用脱敏整串抹去<br>**B03–06** P3–6 | DB0 | [backend/routes/oxidns.py:84](../../backend/routes/oxidns.py#L84) |
| **R111** `GET /api/oxidns/settings` | **S01** 默认 template 设置与 ETag<br>**S02** 已有 native 不完整插件可编辑 | **F01–07** A1–7<br>**F08** 结构归一失败→400 [L79](../../backend/routes/oxidns.py#L79)<br>**F09–10** P1–2 | **B01** 空 native 文本<br>**B02** 额外未知字段与内部 capability 的编辑器豁免<br>**B03–06** P3–6<br>**B90 [O6]** ETag 必须与 Python 字节相同（含浮点、非 ASCII、大整数；上文规则更正）；用 Python 发出的 ETag 去 Go 写入也须成功 | DB0 | [backend/routes/oxidns.py:33](../../backend/routes/oxidns.py#L33) |
| **R112** `POST /api/oxidns/settings` | **S01** template 完整保存<br>**S02** native 不完整参数可保存草稿<br>**S03** 无 If-Match 兼容客户端<br>**S04** If-Match=*<br>**S05** If-Match 强 ETag<br>**S06** If-Match 弱 W/ETag | **F01–07** A1–7<br>**F08** 非对象→400 [L48](../../backend/routes/oxidns.py#L48)<br>**F09** 旧 ETag→409 [L70](../../backend/routes/oxidns.py#L70)<br>**F10** 参数归一校验失败→400 [L79](../../backend/routes/oxidns.py#L79)<br>**F11** 两请求持同 ETag 并发后一方→409 [L70](../../backend/routes/oxidns.py#L70)<br>**F12–13** P1–2 | **B01** 空对象补默认<br>**B02** 中文 emoji 自定义文本<br>**B03** whole-object 替换保留默认语义<br>**B04–07** P3–6<br>**B90 [O6]** ETag 必须与 Python 字节相同（含浮点、非 ASCII、大整数；上文规则更正）；用 Python 发出的 ETag 去 Go 写入也须成功<br>**B91 [O6]** `If-Match: "a", "b"` 多值列表按整串比较→409；`If-Match:  W/"…" ` 首尾空白被 strip | PROFILE:oxidns；AUTO(T16/17) | [backend/routes/oxidns.py:33](../../backend/routes/oxidns.py#L33) |
| **R113** `POST /api/oxidns/template-preview` | **S01** 当前 baseline 与模板无冲突<br>**S02** 冲突 resolutions=current<br>**S03** 冲突 resolutions=template | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** template_settings 错型→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F13** resolutions 无效→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F14** 清空仍被引用附件→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F15–16** P1–2 | **B01** 用户自定义未相关字段保留<br>**B02** 中文 emoji 附件名<br>**B03–06** P3–6 | DB0 | [backend/routes/oxidns.py:144](../../backend/routes/oxidns.py#L144) |
| **R114** `POST /api/oxidns/validate` | **S01** 合法 template 验证<br>**S02** 合法 native 验证<br>**S03** 不完整参数返回结构化诊断 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 深层错误路径与引用链<br>**B02** 未知插件诊断<br>**B03–06** P3–6 | DB0 | [backend/routes/oxidns.py:121](../../backend/routes/oxidns.py#L121) |
| **R115** `GET /api/profiles` | **S01** 所有 profile 元数据与默认标志<br>**S02** 单 profile | **F01–07** A1–7 | **B01** 名称中文 emoji 排序 | DB0 | [backend/routes/profiles.py:27](../../backend/routes/profiles.py#L27) |
| **R116** `POST /api/profiles` | **S01** 显式 id+name 创建<br>**S02** 省略 id 生成<br>**S03** 省略 name 使用id<br>**S04** clone_from 存在 profile | **F01–07** A1–7<br>**F08** truthy 非对象metadata先在 data.pop 触发 Flask500（Step B oracle 更正）<br>**F09** name空白→repo400<br>**F10** 无效id→repo400<br>**F11** 重复id→repo409<br>**F12** clone_from不存在→repo404 | **B01** 中文 emoji 名称<br>**B02** 超长 ID 65字符拒绝<br>**B03** 未知扩展 metadata | PROFILES；AUTO(T16/17) | [backend/routes/profiles.py:27](../../backend/routes/profiles.py#L27) |
| **R117** `DELETE /api/profiles/<profile_id>` | **S01** 非默认无用途 profile 删除<br>**S02** 默认 profile query new_default 切换后删<br>**S03** body new_default 替换 | **F01–07** A1–7<br>**F08** 唯一默认 profile→409<br>**F09** 有其他但未指定新默认→409<br>**F10** 新默认不存在→400<br>**F11** Agent 仍绑定→409<br>**F12** 不存在→404<br>**F13** router 正发布→409<br>**F14–15** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 新默认等于被删项按 Python 拒绝<br>**B02** query 优先于 body<br>**B03** P3 URL优先于query/header | PROFILES；AUTO(T16/17) | [backend/routes/profiles.py:80](../../backend/routes/profiles.py#L80) |
| **R118** `GET /api/profiles/<profile_id>` | **S01** 已有 profile 元数据 | **F01–07** A1–7<br>**F08** 不存在→404<br>**F09** ID 格式非法→400<br>**F10–11** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 中文 emoji name 不是 ID<br>**B02** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:80](../../backend/routes/profiles.py#L80) |
| **R119** `PUT /api/profiles/<profile_id>` | **S01** 仅 name<br>**S02** 仅 description<br>**S03** 两者一起 | **F01–07** A1–7<br>**F08** 未知字段→400<br>**F09** name 空白→400<br>**F10** name 非字符串→400<br>**F11** description 非字符串→400<br>**F12** 不存在→404<br>**F13–14** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 中文 emoji<br>**B02** 空 description 清除<br>**B03** 并发不同字段不丢失<br>**B04** P3 URL优先于query/header | PROFILES；AUTO(T16/17) | [backend/routes/profiles.py:80](../../backend/routes/profiles.py#L80) |
| **R120** `GET /api/profiles/<profile_id>/agents` | **S01** 显式绑定<br>**S02** 旧无 profile_id Agent 按默认绑定<br>**S03** 无绑定空列表 | **F01–07** A1–7<br>**F08** profile 不存在→404<br>**F09–10** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 禁用 Agent 仍算用途<br>**B02** 中文 emoji Agent 名称<br>**B03** P3 URL优先于query/header | DB0<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/profiles.py:120](../../backend/routes/profiles.py#L120) |
| **R121** `POST /api/profiles/<profile_id>/agents/rebind` | **S01** 确认全部 Agent 从 alpha 改到 beta | **F01–07** A1–7<br>**F08** 非对象→400 [L132](../../backend/routes/profiles.py#L132)<br>**F09** 目标缺失或非法→400<br>**F10** 目标不存在→404<br>**F11** 源目标相同→400<br>**F12** agent_ids 缺失/空→400<br>**F13** 重复或非字符串 ID→400<br>**F14** 确认集合已变化→409<br>**F15** 正在部署→409<br>**F16** 正在升级→409<br>**F17** HA publication 忙→409<br>**F18–19** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 旧默认隐式绑定也改显式<br>**B02** 并发改绑只一方成功<br>**B03** P3 URL优先于query/header | REBIND；AUTO(T16/17)；长流程 REBIND<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/profiles.py:126](../../backend/routes/profiles.py#L126) |
| **R122** `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider` | **S01** 默认 YAML<br>**S02** format=surge<br>**S03** format=loon<br>**S04** Surge/Loon regex 排除<br>**S05** 失联源复用有效缓存 | **F01–07** D1–7<br>**F08** 认证失败→401 [L537](../../backend/routes/aggregations.py#L537)<br>**F09** 不存在→404 [L545](../../backend/routes/aggregations.py#L545)<br>**F10** 禁用→403 [L548](../../backend/routes/aggregations.py#L548)<br>**F11** 读取配置异常→500 [L551](../../backend/routes/aggregations.py#L551)<br>**F12** 非法 filters→400 [L558](../../backend/routes/aggregations.py#L558)<br>**F13** 引用图或代理链不支持→400 [L586](../../backend/routes/aggregations.py#L586)<br>**F14** 渲染异常→500 [L589](../../backend/routes/aggregations.py#L589)<br>**F15–16** P1–2 | **B01** 空聚合 proxies=[]<br>**B02** 中文 emoji 名称<br>**B03** 同名节点去重<br>**B04** P3 URL优先于query/header<br>**B91 [O6]** HEAD 也会执行 handler（拉取上游、写缓存/健康记录）：比副作用而非只比空 body（X06） | PROVIDER；长流程 PROVIDER | [backend/routes/profiles.py:204](../../backend/routes/profiles.py#L204)<br>→ [backend/routes/aggregations.py:515](../../backend/routes/aggregations.py#L515) |
| **R123** `POST /api/profiles/<profile_id>/clone` | **S01** 克隆到显式 id/name<br>**S02** 省略 id 自动生成 | **F01–07** A1–7<br>**F08** truthy 非对象metadata先在 data.pop 触发 Flask500（Step B oracle 更正）<br>**F09** name空白→repo400<br>**F10** 无效id→repo400<br>**F11** 重复id→repo409<br>**F12** N/A：body.clone_from不参与固定source克隆，源不存在见F13（Step B oracle更正）<br>**F13** 源不存在→404<br>**F14–15** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 中文 emoji name<br>**B02** 共享引用仍为引用不复制资源<br>**B03** P3 URL优先于query/header | PROFILES；AUTO(T16/17) | [backend/routes/profiles.py:111](../../backend/routes/profiles.py#L111) |
| **R124** `POST /api/profiles/<profile_id>/default` | **S01** 切换到 beta<br>**S02** 已默认重复设置 | **F01–07** A1–7<br>**F08** 不存在→404<br>**F09** 非法 ID→400<br>**F10–11** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 不复制或改写 profile 内容<br>**B02** P3 URL优先于query/header | PROFILES；AUTO(T16/17) | [backend/routes/profiles.py:100](../../backend/routes/profiles.py#L100) |
| **R125** `GET /api/profiles/<profile_id>/export` | **S01** 导出单 profile 可再导入<br>**S02** 内部 token 脱敏 | **F01–07** A1–7<br>**F08** profile 不存在→404<br>**F09–10** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 未知扩展字段保留<br>**B02** 中文 emoji 名称<br>**B03** Content-Disposition profile-id.json<br>**B04** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:145](../../backend/routes/profiles.py#L145) |
| **R126** `POST /api/profiles/<profile_id>/generate/<service>/artifact` | **S01** service=mihomo<br>**S02** service=surge<br>**S03** service=loon<br>**S04** service=mosdns<br>**S05** service=oxidns<br>**S90 [O6]** mihomo artifact 返回 `subscription_hosts`（含 `dns.applied`，以及从 fake Agent 心跳上报的旧 Mihomo 版本算出的 `dns.outdated_agents`）与 `subscription_dialers`；其他 service 两字段为 null（[generate.py:24](../../backend/routes/generate.py#L24)、[generate.py:76](../../backend/routes/generate.py#L76)） | **F01–07** A1–7<br>**F08** 不支持 service→400 [L49](../../backend/routes/generate.py#L49)<br>**F09** 子生成接口错误原样传播 [L52](../../backend/routes/generate.py#L52)<br>**F10** 产物过大→helper<br>**F11** 预览保存不可写→Flask500<br>**F12–13** P1–2 | **B01** 24 份旧预览驱逐<br>**B02** TTL 3600 秒<br>**B03** 中文 emoji 文件名<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** 产物正文含 rule-proxy 能力 URL：`content` 按端点豁免保持原文，响应中其他字段里的同一能力变成 `[REDACTED]`（X01） | ARTIFACT；长流程 ARTIFACT | [backend/routes/profiles.py:16](../../backend/routes/profiles.py#L16)<br>→ [backend/routes/generate.py:38](../../backend/routes/generate.py#L38) |
| **R127** `POST /api/profiles/<profile_id>/generate/loon` | **S01** 当前 profile loon 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:loon；长流程 GEN-loon | [backend/routes/profiles.py:231](../../backend/routes/profiles.py#L231)<br>→ [backend/routes/generate.py:147](../../backend/routes/generate.py#L147) |
| **R128** `POST /api/profiles/<profile_id>/generate/loon/preview` | **S01** loon preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04** P3 URL优先于query/header | PREVIEW:loon；长流程 GEN-loon | [backend/routes/profiles.py:258](../../backend/routes/profiles.py#L258)<br>→ [backend/routes/generate.py:245](../../backend/routes/generate.py#L245) |
| **R129** `POST /api/profiles/<profile_id>/generate/mihomo` | **S01** 当前 profile mihomo 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:mihomo；长流程 GEN-mihomo | [backend/routes/profiles.py:217](../../backend/routes/profiles.py#L217)<br>→ [backend/routes/generate.py:94](../../backend/routes/generate.py#L94) |
| **R130** `POST /api/profiles/<profile_id>/generate/mihomo/preview` | **S01** mihomo preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04** P3 URL优先于query/header | PREVIEW:mihomo；长流程 GEN-mihomo | [backend/routes/profiles.py:244](../../backend/routes/profiles.py#L244)<br>→ [backend/routes/generate.py:204](../../backend/routes/generate.py#L204) |
| **R131** `POST /api/profiles/<profile_id>/generate/mosdns` | **S01** 当前 profile mosdns 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** MosdnsArchiveError→500 [L195](../../backend/routes/generate.py#L195)<br>**F09** 一般异常含脱敏详情→500 [L199](../../backend/routes/generate.py#L199)<br>**F10** 坏 JSON→500<br>**F11–12** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:mosdns；长流程 GEN-mosdns | [backend/routes/profiles.py:237](../../backend/routes/profiles.py#L237)<br>→ [backend/routes/generate.py:169](../../backend/routes/generate.py#L169) |
| **R132** `POST /api/profiles/<profile_id>/generate/mosdns/preview` | **S01** mosdns preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** 其他异常→500 [L274](../../backend/routes/generate.py#L274)<br>**F09–10** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04** P3 URL优先于query/header | PREVIEW:mosdns；长流程 GEN-mosdns | [backend/routes/profiles.py:264](../../backend/routes/profiles.py#L264)<br>→ [backend/routes/generate.py:262](../../backend/routes/generate.py#L262) |
| **R133** `POST /api/profiles/<profile_id>/generate/oxidns` | **S01** 当前 profile oxidns 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** OxidnsSettingsError/ValueError→400 [L296](../../backend/routes/generate.py#L296)<br>**F09** 其他异常脱敏→500 [L300](../../backend/routes/generate.py#L300)<br>**F10–11** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:oxidns；长流程 GEN-oxidns | [backend/routes/profiles.py:271](../../backend/routes/profiles.py#L271)<br>→ [backend/routes/generate.py:279](../../backend/routes/generate.py#L279) |
| **R134** `POST /api/profiles/<profile_id>/generate/oxidns/preview` | **S01** oxidns preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** ValueError→400 [L318](../../backend/routes/generate.py#L318)<br>**F09** 其他异常脱敏→500 [L320](../../backend/routes/generate.py#L320)<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04** P3 URL优先于query/header | PREVIEW:oxidns；长流程 GEN-oxidns | [backend/routes/profiles.py:278](../../backend/routes/profiles.py#L278)<br>→ [backend/routes/generate.py:305](../../backend/routes/generate.py#L305) |
| **R135** `POST /api/profiles/<profile_id>/generate/surge` | **S01** 当前 profile surge 下载<br>**S02** 显式 base_url<br>**S03** 省略 base_url<br>**S04** 有远程规则的离线有效缓存 | **F01–07** A1–7<br>**F08** 配置引用无效→400<br>**F09** 坏 JSON→500<br>**F10** 非对象→500<br>**F11** 生成/写文件异常→500<br>**F12–13** P1–2 | **B01** 中文 emoji 名称<br>**B02** 空资源<br>**B03** 特殊字符 URL<br>**B04** 客户端中断写入仍完成<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `send_file` 头：`Content-Disposition: attachment; filename=…`、按扩展名推断的 Content-Type（以测试服务器上 Python 实际值为准）、Last-Modified/ETag/Accept-Ranges 是否存在，POST 实测 `Range` 与 `If-None-Match:*` 均200；不套用GET的206/304。比存在性与语义，不比独立生成mtime；GET条件下载另测（Step B oracle更正） | GENERATE:surge；长流程 GEN-surge | [backend/routes/profiles.py:224](../../backend/routes/profiles.py#L224)<br>→ [backend/routes/generate.py:122](../../backend/routes/generate.py#L122) |
| **R136** `POST /api/profiles/<profile_id>/generate/surge/preview` | **S01** surge preview 有效配置<br>**S02** base_url 显式与省略 | **F01–07** A1–7<br>**F08** StrategyReferenceError→400<br>**F09** 其他异常→500<br>**F10–11** P1–2 | **B01** 空资源<br>**B02** 中文 emoji<br>**B03** 生成文本逐结构比较<br>**B04** P3 URL优先于query/header | PREVIEW:surge；长流程 GEN-surge | [backend/routes/profiles.py:251](../../backend/routes/profiles.py#L251)<br>→ [backend/routes/generate.py:226](../../backend/routes/generate.py#L226) |
| **R137** `POST /api/profiles/<profile_id>/import` | **S01** 当前 schema 独立 profile 导入<br>**S02** 导入后 GET 读回 | **F01–07** A1–7<br>**F08** 非对象→400 [L157](../../backend/routes/profiles.py#L157)<br>**F09** 包含共享资源副本→400<br>**F10** schema 错→400<br>**F11** revision 冲突→409<br>**F12** 资源不存在→409<br>**F13** profile 不存在→404<br>**F14–15** P1–2<br>**F90 [O6]** 未登录访问不存在的 `<profile_id>`→404 先于 401（路径 profile 先被解析，X02） | **B01** 空对象校验<br>**B02** 未知扩展字段存储保留<br>**B03** P3 URL优先于query/header | PROFILE:*；AUTO(T16/17) | [backend/routes/profiles.py:154](../../backend/routes/profiles.py#L154) |
| **R138** `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | **S01** JWT 授权<br>**S02** 当前内部 token query 授权<br>**S03** config_token query 授权<br>**S04** 本 profile inline 内容不走网络<br>**S05** 声明 URL 验证缓存<br>**S06** 未声明 URL 下载<br>**S07** part=domain<br>**S08** part=ip<br>**S09** 无 part 混合<br>**S90 [O6]** 声明源有有效缓存且上次定时更新失败→直接返回缓存（不下载）+ `X-ConfigFlow-Rule-Cache: stale`；声明源无缓存且下载失败→502 `RULE_SOURCE_UNAVAILABLE`（[mosdns.py:556](../../backend/routes/mosdns.py#L556)、[scheduled_refresh.py:214](../../backend/utils/scheduled_refresh.py#L214)）<br>**S91 [O6]** `github_proxy_domain` 改写下载地址 + `rule_fetch_proxy` 作 HTTP 代理：fake 代理记录实际请求行与目标 | **F01** C1 未配置管理/公开拉取凭据且未提供内部能力或JWT仍401（内部能力由仓库自动生成，不能假设不存在）<br>**F02** C2当前rule_proxy_token放Bearer不授权<br>**F03** C3退役token即使配置认证关闭仍401<br>**F04** 无凭据→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F05** 退役能力→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F06** 内部 token 仅放 Bearer→401 [L529](../../backend/routes/mosdns.py#L529)<br>**F07** 缺 url→400 [L534](../../backend/routes/mosdns.py#L534)<br>**F08** part 非法→400 [L537](../../backend/routes/mosdns.py#L537)<br>**F09** 非法远端 URL→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F10** 远端 non2xx→502 [L577](../../backend/routes/mosdns.py#L577)<br>**F11** 重定向过多→400 Invalid remote URL [L591](../../backend/routes/mosdns.py#L591)<br>**F12** 声明 MRS→422 [L588](../../backend/routes/mosdns.py#L588)<br>**F13** 声明内容校验失败→422 [L588](../../backend/routes/mosdns.py#L588)<br>**F14** Content-Length 超限→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F15** 实际流超限→400 [L591](../../backend/routes/mosdns.py#L591)<br>**F16** 未知处理异常→500 [L594](../../backend/routes/mosdns.py#L594)<br>**F17–18** P1–2 | **B01** BOM 和中文域名<br>**B02** 5MiB 边界<br>**B03** 本地跨 profile URL 不误解析<br>**B04** 坏 UTF8 按来源路径处理<br>**B05** P3 URL优先于query/header<br>**B90 [O6]** `source_id` 指向声明源 / 指向别的源 / 不存在<br>**B91 [O6]** inline 内容源被禁用（共享源 enabled=false，解析引用得到 `library_enabled=false`）或 URL 带 query 时不走 inline 快捷解析；相对 URL 随后400，绝对面板自身 API URL 由 request_local 进程内分发（Step B oracle 更正） | RULE-FILE；AUTO(T16/17)；长流程 RULE-PROXY<br>**[O6] 更正**：本端点不提交配置，AUTO(T16/17) 只断言**不**触发备份；只比较规则缓存与 task-status | [backend/routes/profiles.py:210](../../backend/routes/profiles.py#L210)<br>→ [backend/routes/mosdns.py:493](../../backend/routes/mosdns.py#L493) |
| **R139** `POST /api/profiles/<profile_id>/oxidns/common-settings` | **S01** 默认 native 控件识别<br>**S02** 自定义 native 能力状态只读 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 缺标准插件给 capability 原因<br>**B02** 空 changes 不涉及<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:320](../../backend/routes/profiles.py#L320)<br>→ [backend/routes/oxidns.py:157](../../backend/routes/oxidns.py#L157) |
| **R140** `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | **S01** 改 cache<br>**S02** 改 upstream<br>**S03** 改路由<br>**S04** 改 AdGuard/设备策略<br>**S05** 非空同值 changes 幂等；{} 实际拒绝400，见 H369（Step B oracle 更正） | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** changes 非对象→helper<br>**F13** 未知控件键→helper<br>**F14** 无法识别共享处理链拒绝→helper<br>**F15–16** P1–2 | **B01** 中文 emoji 设备备注<br>**B02** 空列表清除<br>**B03** 仅返回草稿不能落库<br>**B04** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:327](../../backend/routes/profiles.py#L327)<br>→ [backend/routes/oxidns.py:169](../../backend/routes/oxidns.py#L169) |
| **R141** `POST /api/profiles/<profile_id>/oxidns/editor` | **S01** template 转 unified native 草稿<br>**S02** 已有 native 编辑器准备 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 空模板<br>**B02** 注释和额外文件语义保留<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:306](../../backend/routes/profiles.py#L306)<br>→ [backend/routes/oxidns.py:132](../../backend/routes/oxidns.py#L132) |
| **R142** `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | **S01** native_config 完整表单编辑<br>**S02** renames tag 联动<br>**S03** standard_add 标准功能新增<br>**S04** 安全删除未被引用插件 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** native_config 非文本→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F13** renames 非映射→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F14** standard_add 重复/错型→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F15** 重命名覆盖现有 tag→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F16** include/动态引用无法安全改→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F17** 删除后仍被引用→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F18** 新增依赖同时被删→400 [L188](../../backend/routes/oxidns.py#L188)<br>**F19–20** P1–2 | **B01** 中文 emoji tag<br>**B02** 空 renames<br>**B03** references 字段必须逐路径比较<br>**B04** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:334](../../backend/routes/profiles.py#L334)<br>→ [backend/routes/oxidns.py:181](../../backend/routes/oxidns.py#L181) |
| **R143** `POST /api/profiles/<profile_id>/oxidns/render` | **S01** settings template 渲染<br>**S02** settings native 渲染<br>**S03** 省略 base_url 使用请求根 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 空附加文件列表<br>**B02** 含 capability 的 config 不被通用脱敏整串抹去<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:292](../../backend/routes/profiles.py#L292)<br>→ [backend/routes/oxidns.py:84](../../backend/routes/oxidns.py#L84) |
| **R144** `GET /api/profiles/<profile_id>/oxidns/settings` | **S01** 默认 template 设置与 ETag<br>**S02** 已有 native 不完整插件可编辑 | **F01–07** A1–7<br>**F08** 结构归一失败→400 [L79](../../backend/routes/oxidns.py#L79)<br>**F09–10** P1–2 | **B01** 空 native 文本<br>**B02** 额外未知字段与内部 capability 的编辑器豁免<br>**B03** P3 URL优先于query/header<br>**B90 [O6]** ETag 必须与 Python 字节相同（含浮点、非 ASCII、大整数；上文规则更正）；用 Python 发出的 ETag 去 Go 写入也须成功 | DB0 | [backend/routes/profiles.py:285](../../backend/routes/profiles.py#L285)<br>→ [backend/routes/oxidns.py:33](../../backend/routes/oxidns.py#L33) |
| **R145** `POST /api/profiles/<profile_id>/oxidns/settings` | **S01** template 完整保存<br>**S02** native 不完整参数可保存草稿<br>**S03** 无 If-Match 兼容客户端<br>**S04** If-Match=*<br>**S05** If-Match 强 ETag<br>**S06** If-Match 弱 W/ETag | **F01–07** A1–7<br>**F08** 非对象→400 [L48](../../backend/routes/oxidns.py#L48)<br>**F09** 旧 ETag→409 [L70](../../backend/routes/oxidns.py#L70)<br>**F10** 参数归一校验失败→400 [L79](../../backend/routes/oxidns.py#L79)<br>**F11** 两请求持同 ETag 并发后一方→409 [L70](../../backend/routes/oxidns.py#L70)<br>**F12–13** P1–2 | **B01** 空对象补默认<br>**B02** 中文 emoji 自定义文本<br>**B03** whole-object 替换保留默认语义<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** ETag 必须与 Python 字节相同（含浮点、非 ASCII、大整数；上文规则更正）；用 Python 发出的 ETag 去 Go 写入也须成功<br>**B91 [O6]** `If-Match: "a", "b"` 多值列表按整串比较→409；`If-Match:  W/"…" ` 首尾空白被 strip | PROFILE:oxidns；AUTO(T16/17) | [backend/routes/profiles.py:285](../../backend/routes/profiles.py#L285)<br>→ [backend/routes/oxidns.py:33](../../backend/routes/oxidns.py#L33) |
| **R146** `POST /api/profiles/<profile_id>/oxidns/template-preview` | **S01** 当前 baseline 与模板无冲突<br>**S02** 冲突 resolutions=current<br>**S03** 冲突 resolutions=template | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12** template_settings 错型→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F13** resolutions 无效→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F14** 清空仍被引用附件→400 [L152](../../backend/routes/oxidns.py#L152)<br>**F15–16** P1–2 | **B01** 用户自定义未相关字段保留<br>**B02** 中文 emoji 附件名<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:313](../../backend/routes/profiles.py#L313)<br>→ [backend/routes/oxidns.py:144](../../backend/routes/oxidns.py#L144) |
| **R147** `POST /api/profiles/<profile_id>/oxidns/validate` | **S01** 合法 template 验证<br>**S02** 合法 native 验证<br>**S03** 不完整参数返回结构化诊断 | **F01–07** A1–7<br>**F08** settings 缺失/非对象→400 [L106](../../backend/routes/oxidns.py#L106)<br>**F09** base_url 非字符串→400 [L109](../../backend/routes/oxidns.py#L109)<br>**F10** native YAML 非法→helper<br>**F11** 插件约束/引用非法→helper<br>**F12–13** P1–2 | **B01** 深层错误路径与引用链<br>**B02** 未知插件诊断<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:299](../../backend/routes/profiles.py#L299)<br>→ [backend/routes/oxidns.py:121](../../backend/routes/oxidns.py#L121) |
| **R148** `GET /api/profiles/<profile_id>/proxy-groups` | **S01** 普通来源组<br>**S02** chain 组<br>**S03** follower 组 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 空列表<br>**B02** 中文 emoji 名称<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:196](../../backend/routes/profiles.py#L196)<br>→ [backend/routes/proxy_groups.py:130](../../backend/routes/proxy_groups.py#L130) |
| **R149** `POST /api/profiles/<profile_id>/proxy-groups` | **S01** manual_nodes 来源组<br>**S02** subscriptions 来源组<br>**S03** aggregations 来源组<br>**S04** include_groups 来源组<br>**S05** follow_group 组<br>**S06** chain 前置和落地组 | **F01–07** A1–7<br>**F08** 非对象→400 [L140](../../backend/routes/proxy_groups.py#L140)<br>**F09** delete_fields 不适用于创建→400 [L142](../../backend/routes/proxy_groups.py#L142)<br>**F10** 无效候选引用→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F11** 候选数组错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F12** follow_group 错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F13** filter 非字符串→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F14** filter 正则错误→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F15** 无实际候选来源→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F16** proxies_order 不全/重复/错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F17** 重复/保留组名→repo<br>**F18** 链循环/自引用→helper<br>**F19** follow DNS native 冲突→helper<br>**F20–21** P1–2 | **B01** 中文 emoji 名称<br>**B02** 健康类型默认初始化<br>**B03** chain 无关字段清除<br>**B04** P3 URL优先于query/header | GROUP；AUTO(T16/17) | [backend/routes/profiles.py:196](../../backend/routes/profiles.py#L196)<br>→ [backend/routes/proxy_groups.py:130](../../backend/routes/proxy_groups.py#L130) |
| **R150** `GET /api/profiles/<profile_id>/rule-library/content/<rule_id>` | **S01** content 文本原样<br>**S02** 空 content 返回空 | **F01** 不存在→404 [L311](../../backend/routes/rule_library.py#L311)<br>**F02** URL 类型→400 [L314](../../backend/routes/rule_library.py#L314)<br>**F03** 配置异常→500 [L320](../../backend/routes/rule_library.py#L320)<br>**F04–05** P1–2 | **B01** 中文 emoji 与换行<br>**B02** 无需 token 公开访问<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:189](../../backend/routes/profiles.py#L189)<br>→ [backend/routes/rule_library.py:303](../../backend/routes/rule_library.py#L303) |
| **R151** `GET /api/profiles/<profile_id>/rules` | **S01** 普通规则与 hydrated ruleset<br>**S02** 相对 URL 拼接 server_domain | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 空 rule_configs<br>**B02** 禁用的 library_enabled 展示<br>**B03** P3 URL优先于query/header | DB0 | [backend/routes/profiles.py:175](../../backend/routes/profiles.py#L175)<br>→ [backend/routes/rules.py:93](../../backend/routes/rules.py#L93) |
| **R152** `POST /api/profiles/<profile_id>/rules` | **S01** DOMAIN 规则<br>**S02** IP-CIDR 规则<br>**S03** MATCH 带 udp_fallback/ntp_direct<br>**S04** ruleset 引用并剥离 source 字段 | **F01–07** A1–7<br>**F08** 非对象→400 [L61](../../backend/routes/rules.py#L61)<br>**F09** 未知 rule_type→400 [L81](../../backend/routes/rules.py#L81)<br>**F10** 空域名值→400 [L81](../../backend/routes/rules.py#L81)<br>**F11** 非法域名/CIDR→400 [L81](../../backend/routes/rules.py#L81)<br>**F12** ruleset 未给 library_rule_id→400 [L86](../../backend/routes/rules.py#L86)<br>**F13** 不存在 library id→400 [L86](../../backend/routes/rules.py#L86)<br>**F14** MATCH 非法 udp_fallback→400 [L68](../../backend/routes/rules.py#L68)<br>**F15** MATCH ntp_direct 非 bool→400 [L70](../../backend/routes/rules.py#L70)<br>**F16–17** P1–2 | **B01** 中文 emoji remark/group_name<br>**B02** 逗号型规则值<br>**B03** 重复 ID 不可落盘<br>**B04** P3 URL优先于query/header | PROFILE:rule_configs；AUTO(T16/17) | [backend/routes/profiles.py:175](../../backend/routes/profiles.py#L175)<br>→ [backend/routes/rules.py:93](../../backend/routes/rules.py#L93) |
| **R153** `GET /api/profiles/<profile_id>/rules/local/<name>` | **S01** 内容型缓存存在<br>**S02** 内容型缺缓存自动写入<br>**S03** URL 有效缓存<br>**S04** URL 刷新失败用旧验证缓存<br>**S05** 库无条目但旧缓存存在<br>**S06** target=mihomo 规范化 | **F01** 库和缓存均不存在→404 [L758](../../backend/routes/rules.py#L758)<br>**F02** 远端内容非法且无有效缓存→422 [L772](../../backend/routes/rules.py#L772)<br>**F03** 网络失败无缓存→502 [L775](../../backend/routes/rules.py#L775)<br>**F04** 读取异常→500 [L791](../../backend/routes/rules.py#L791)<br>**F05–06** P1–2 | **B01** 中文 emoji 名称<br>**B02** 路径特殊字符不能逃逸<br>**B03** 非 UTF8 目标归一失败时返回原字节<br>**B04** 空内容<br>**B05** P3 URL优先于query/header | RULE-FILE；AUTO(T16/17)；长流程 RULE-FILE | [backend/routes/profiles.py:181](../../backend/routes/profiles.py#L181)<br>→ [backend/routes/rules.py:691](../../backend/routes/rules.py#L691) |
| **R154** `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | **S01** 默认 Mihomo YAML<br>**S02** format=surge 文本<br>**S03** format=loon 文本<br>**S04** 远端失败返回节点缓存<br>**S05** Surge/Loon regex 筛选<br>**S06** 无 URL 读取缓存 | **F01–07** D1–7<br>**F08** 认证失败→401 [L487](../../backend/routes/subscriptions.py#L487)<br>**F09** 对象不存在→404 [L495](../../backend/routes/subscriptions.py#L495)<br>**F10** 读取配置异常→500 [L498](../../backend/routes/subscriptions.py#L498)<br>**F11** 非法 filters→400 [L505](../../backend/routes/subscriptions.py#L505)<br>**F12** 链用 Surge→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F13** 链用 Loon→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F14** 坏图不可回缓存→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F15** 无缓存→404 [L600](../../backend/routes/subscriptions.py#L600)<br>**F16** 缓存无节点→404 [L607](../../backend/routes/subscriptions.py#L607)<br>**F17** 其他异常→500 [L692](../../backend/routes/subscriptions.py#L692)<br>**F18–19** P1–2 | **B01** 筛选后空集合<br>**B02** 中文 emoji 节点名<br>**B03** format=unknown 按 YAML 但不触发默认 fallback<br>**B04** P3 URL优先于query/header<br>**B90 [O6]** YAML 头注释 `# Generated: <本地时间>Z`（本地时间后接字面 Z）和 `Content-Disposition` 文件名时间戳随 TZ 变化（X05）<br>**B91 [O6]** HEAD 也会执行 handler（拉取上游、写缓存/健康记录）：比副作用而非只比空 body（X06） | PROVIDER；长流程 PROVIDER | [backend/routes/profiles.py:166](../../backend/routes/profiles.py#L166)<br>→ [backend/routes/subscriptions.py:471](../../backend/routes/subscriptions.py#L471) |
| **R155** `POST /api/profiles/import-client` | **S01** client_type=mihomo dry_run=true<br>**S02** client_type=surge dry_run=true<br>**S03** client_type=loon dry_run=true<br>**S04** client_type=shadowrocket dry_run=true<br>**S05** client_type=mihomo 持久导入201<br>**S06** client_type=surge 持久导入201<br>**S07** client_type=loon 持久导入201<br>**S08** client_type=shadowrocket 持久导入201 | **F01–07** A1–7<br>**F08** 非对象→400 [L52](../../backend/routes/profiles.py#L52)<br>**F09** name 缺失→400 [L55](../../backend/routes/profiles.py#L55)<br>**F10** name 空白→400 [L55](../../backend/routes/profiles.py#L55)<br>**F11** 默认订阅 ID 非数组→400 [L58](../../backend/routes/profiles.py#L58)<br>**F12** 不支持 client_type→400 [L62](../../backend/routes/profiles.py#L62)<br>**F13** content 空→400 [L62](../../backend/routes/profiles.py#L62)<br>**F14** Mihomo 非对象→400 [L62](../../backend/routes/profiles.py#L62)<br>**F15** INI 无 Section→400 [L62](../../backend/routes/profiles.py#L62)<br>**F16** 无解析实体→400 [L62](../../backend/routes/profiles.py#L62)<br>**F17** 重复 id→repo409<br>**F18** 默认订阅不存在→repo409 | **B01** 中文 emoji 节点/组/规则<br>**B02** 超长导入文本<br>**B03** 不支持节点行产生 warnings 而非吞掉 | IMPORT；AUTO(T16/17) | [backend/routes/profiles.py:42](../../backend/routes/profiles.py#L42) |
| **R156** `GET /api/proxy-groups` | **S01** 普通来源组<br>**S02** chain 组<br>**S03** follower 组 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 空列表<br>**B02** 中文 emoji 名称<br>**B03–06** P3–6 | DB0 | [backend/routes/proxy_groups.py:130](../../backend/routes/proxy_groups.py#L130) |
| **R157** `POST /api/proxy-groups` | **S01** manual_nodes 来源组<br>**S02** subscriptions 来源组<br>**S03** aggregations 来源组<br>**S04** include_groups 来源组<br>**S05** follow_group 组<br>**S06** chain 前置和落地组 | **F01–07** A1–7<br>**F08** 非对象→400 [L140](../../backend/routes/proxy_groups.py#L140)<br>**F09** delete_fields 不适用于创建→400 [L142](../../backend/routes/proxy_groups.py#L142)<br>**F10** 无效候选引用→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F11** 候选数组错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F12** follow_group 错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F13** filter 非字符串→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F14** filter 正则错误→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F15** 无实际候选来源→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F16** proxies_order 不全/重复/错型→400 [L156](../../backend/routes/proxy_groups.py#L156)<br>**F17** 重复/保留组名→repo<br>**F18** 链循环/自引用→helper<br>**F19** follow DNS native 冲突→helper<br>**F20–21** P1–2 | **B01** 中文 emoji 名称<br>**B02** 健康类型默认初始化<br>**B03** chain 无关字段清除<br>**B04–07** P3–6 | GROUP；AUTO(T16/17) | [backend/routes/proxy_groups.py:130](../../backend/routes/proxy_groups.py#L130) |
| **R158** `DELETE /api/proxy-groups/<group_id>` | **S01** 无引用组删除<br>**S02** 解除 listener 固定策略 | **F01–07** A1–7<br>**F08** 不存在→404 [L223](../../backend/routes/proxy_groups.py#L223)<br>**F09** 被 rule 引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F10** 被 include/follow 引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F11** 被链引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F12** listener 重写失败→400 [L276](../../backend/routes/proxy_groups.py#L276)<br>**F13** DNS 跟随重建失败→helper<br>**F14–15** P1–2 | **B01** 空 listener_changes 不返回字段<br>**B02–05** P3–6 | GROUP；AUTO(T16/17) | [backend/routes/proxy_groups.py:206](../../backend/routes/proxy_groups.py#L206) |
| **R159** `PUT /api/proxy-groups/<group_id>` | **S01** 部分字段合并<br>**S02** delete_fields 清健康字段<br>**S03** 普通类型改 url-test 初始化<br>**S04** 改名联动 rule.policy 和 listener<br>**S05** 切换为 chain 清普通字段 | **F01–07** A1–7<br>**F08** 非对象→400 [L214](../../backend/routes/proxy_groups.py#L214)<br>**F09** 不存在→404 [L223](../../backend/routes/proxy_groups.py#L223)<br>**F10** 修改 ID→409 [L226](../../backend/routes/proxy_groups.py#L226)<br>**F11** 同名→409 [L245](../../backend/routes/proxy_groups.py#L245)<br>**F12** 删除/禁用被规则引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F13** 被 include/follow 引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F14** 被 dialer 引用→409 [L253](../../backend/routes/proxy_groups.py#L253)<br>**F15** 候选校验失败→400 [L276](../../backend/routes/proxy_groups.py#L276)<br>**F16** listener YAML 不可安全改→400 [L276](../../backend/routes/proxy_groups.py#L276)<br>**F17** delete_fields 同时赋值→helper<br>**F18** 跟随 DNS 严格更新失败→helper<br>**F19–20** P1–2 | **B01** 中文 emoji 新名<br>**B02** 未修改旧无效 filter 可否保留按 validator<br>**B03–06** P3–6 | GROUP；AUTO(T16/17) | [backend/routes/proxy_groups.py:206](../../backend/routes/proxy_groups.py#L206) |
| **R160** `PUT /api/proxy-groups/batch-health-url` | **S01** 多个 url-test/fallback/load-balance 组仅改 url | **F01–07** A1–7<br>**F08** 非对象→400 [L169](../../backend/routes/proxy_groups.py#L169)<br>**F09** ids 缺失/空→400 [L174](../../backend/routes/proxy_groups.py#L174)<br>**F10** 重复 id→400 [L174](../../backend/routes/proxy_groups.py#L174)<br>**F11** 空白或非字符串 id→400 [L174](../../backend/routes/proxy_groups.py#L174)<br>**F12** URL 无效→400 [L179](../../backend/routes/proxy_groups.py#L179)<br>**F13** 组不存在→404 [L187](../../backend/routes/proxy_groups.py#L187)<br>**F14** follow 组→400 [L189](../../backend/routes/proxy_groups.py#L189)<br>**F15** 非健康类型组→400 [L191](../../backend/routes/proxy_groups.py#L191)<br>**F16–17** P1–2 | **B01** 一个有效一个无效必须全回滚<br>**B02** 特殊编码 URL<br>**B03–06** P3–6 | PROFILE:proxy_groups.url；AUTO(T16/17) | [backend/routes/proxy_groups.py:162](../../backend/routes/proxy_groups.py#L162) |
| **R161** `GET /api/proxy-groups/health-defaults` | **S01** 组和 provider 默认健康检查 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** tolerance=50 以及布尔类型<br>**B02–05** P3–6 | DB0 | [backend/routes/proxy_groups.py:19](../../backend/routes/proxy_groups.py#L19) |
| **R162** `POST /api/proxy-groups/preview-regex` | **S01** subscription 包含 regex<br>**S02** aggregation 包含 regex<br>**S03** exclude_regex 单独使用<br>**S04** 反引号多桶排序<br>**S05** deduplicate_by_source=true | **F01–07** A1–7<br>**F08** 非对象→400 [L297](../../backend/routes/proxy_groups.py#L297)<br>**F09** regex 非文本→400 [L303](../../backend/routes/proxy_groups.py#L303)<br>**F10** exclude_regex 非文本→400 [L303](../../backend/routes/proxy_groups.py#L303)<br>**F11** source 非法→400 [L309](../../backend/routes/proxy_groups.py#L309)<br>**F12** 两种 regex 都空→400 [L312](../../backend/routes/proxy_groups.py#L312)<br>**F13** include 正则非法→400 [L319](../../backend/routes/proxy_groups.py#L319)<br>**F14** exclude 正则非法→400 [L323](../../backend/routes/proxy_groups.py#L323)<br>**F15** 聚合 filter 错误→400 [L402](../../backend/routes/proxy_groups.py#L402)<br>**F16** 其他异常→500 [L404](../../backend/routes/proxy_groups.py#L404)<br>**F17–18** P1–2 | **B01** 中文 emoji 节点名<br>**B02** 不存在或禁用来源被跳过<br>**B03** 同名跨源去重差别<br>**B04–07** P3–6 | DB0 | [backend/routes/proxy_groups.py:291](../../backend/routes/proxy_groups.py#L291) |
| **R163** `POST /api/proxy-groups/reorder` | **S01** ids top<br>**S02** ids bottom<br>**S03** 旧 groups 数组 | **F01–07** A1–7<br>**F08** 缺/错旧数组→404 [L422](../../backend/routes/proxy_groups.py#L422)<br>**F09** 重复或非字符串 id→404 [L422](../../backend/routes/proxy_groups.py#L422)<br>**F10** ids 非数组→404 [L422](../../backend/routes/proxy_groups.py#L422)<br>**F11** 未知 id→404 [L422](../../backend/routes/proxy_groups.py#L422)<br>**F12** 重排后 dialer 无效→400 [L430](../../backend/routes/proxy_groups.py#L430)<br>**F13** 其他异常→500 [L436](../../backend/routes/proxy_groups.py#L436)<br>**F14–15** P1–2 | **B01** ids=[]<br>**B02** 旧对象不覆盖<br>**B03–06** P3–6 | PROFILE:proxy_groups；AUTO(T16/17) | [backend/routes/proxy_groups.py:409](../../backend/routes/proxy_groups.py#L409) |
| **R164** `GET /api/rule-library` | **S01** domain/classical/ipcidr has_ip_part<br>**S02** 无库项 | **F01–07** A1–7 | **B01** 启用与禁用项<br>**B02** classical 已知 IP 部分 | DB0 | [backend/routes/rule_library.py:101](../../backend/routes/rule_library.py#L101) |
| **R165** `POST /api/rule-library` | **S01** content domain 文本<br>**S02** content YAML payload<br>**S03** url 文本<br>**S04** url MRS 声明 | **F01–07** A1–7<br>**F08** 非对象→400 [L116](../../backend/routes/rule_library.py#L116)<br>**F09** id 缺失→400 [L118](../../backend/routes/rule_library.py#L118)<br>**F10** id 空→400 [L118](../../backend/routes/rule_library.py#L118)<br>**F11** id 错型→400 [L118](../../backend/routes/rule_library.py#L118)<br>**F12** 重复 ID→409 [L120](../../backend/routes/rule_library.py#L120)<br>**F13** 非法域名内容→helper<br>**F14** 无效 source/url→400 [L141](../../backend/routes/rule_library.py#L141)<br>**F15** 下载失败→502 [L139](../../backend/routes/rule_library.py#L139)<br>**F16** 落盘失败→500 [L149](../../backend/routes/rule_library.py#L149)<br>**F17** 写结果无法确认→503 [L146](../../backend/routes/rule_library.py#L146) | **B01** 中文 emoji 名称<br>**B02** 剥 has_ip_part 与多余来源字段<br>**B03** 空内容是否有效按 validator | LIBRARY；AUTO(T16/17) | [backend/routes/rule_library.py:101](../../backend/routes/rule_library.py#L101) |
| **R166** `DELETE /api/rule-library/<rule_id>` | **S01** 无引用 source 删除与缓存清理 | **F01–07** A1–7<br>**F08** 不存在→404 [L194](../../backend/routes/rule_library.py#L194)<br>**F09** 仍被任一 profile 引用→repo409<br>**F10** 删除文件权限异常→Flask500 | **B01** 旧名称缓存已不存在忽略 | LIBRARY；AUTO(T16/17) | [backend/routes/rule_library.py:155](../../backend/routes/rule_library.py#L155) |
| **R167** `PUT /api/rule-library/<rule_id>` | **S01** 只改名称迁移旧缓存<br>**S02** 更改 URL 预下载<br>**S03** content 修改<br>**S04** 更新 enabled/behavior 重建 native | **F01–07** A1–7<br>**F08** 非对象→400 [L169](../../backend/routes/rule_library.py#L169)<br>**F09** 不存在→404 [L173](../../backend/routes/rule_library.py#L173)<br>**F10** 无有效源→400 [L185](../../backend/routes/rule_library.py#L185)<br>**F11** 远端失败→502 [L183](../../backend/routes/rule_library.py#L183)<br>**F12** 下载期间被修改→409 [L197](../../backend/routes/rule_library.py#L197)<br>**F13** 域名内容非法→helper<br>**F14** 写缓存失败→Flask500<br>**F15** native 重建异常按 helper | **B01** 保留原 update_interval<br>**B02** 中文 emoji 名称<br>**B03** 并发只一方更新成功 | LIBRARY；AUTO(T16/17) | [backend/routes/rule_library.py:155](../../backend/routes/rule_library.py#L155) |
| **R168** `GET /api/rule-library/<rule_id>/draft-content` | **S01** 读取 content rule 与 revision<br>**S02** 读取 URL rule 草稿元数据 | **F01–07** A1–7<br>**F08** 不存在→404 [L69](../../backend/routes/rule_library.py#L69) | **B01** usage_count 多 profile<br>**B02** 空内容 | DB0 | [backend/routes/rule_library.py:54](../../backend/routes/rule_library.py#L54) |
| **R169** `PUT /api/rule-library/<rule_id>/draft-content` | **S01** 精确 revision+content 保存 | **F01–07** A1–7<br>**F08** 非对象→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F09** 缺 revision→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F10** 缺 content→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F11** 额外字段→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F12** revision 非文本→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F13** content 非文本→400 [L78](../../backend/routes/rule_library.py#L78)<br>**F14** 规则不存在→404 [L94](../../backend/routes/rule_library.py#L94)<br>**F15** revision 旧→409 [L84](../../backend/routes/rule_library.py#L84)<br>**F16** URL 型不可手工改→400 [L86](../../backend/routes/rule_library.py#L86)<br>**F17** 内容验证失败→helper<br>**F18** DB 成功缓存写失败→Flask500 | **B01** 中文与空内容<br>**B02** 并发两个相同 revision 一个成功一个409 | LIBRARY；AUTO(T16/17) | [backend/routes/rule_library.py:54](../../backend/routes/rule_library.py#L54) |
| **R170** `POST /api/rule-library/cache` | **S01** content 与 URL 混合缓存<br>**S02** URL 刷新成功<br>**S03** 失败保留旧有效缓存 | **F01–07** A1–7<br>**F08** 缺 rule_ids→400 [L479](../../backend/routes/rule_library.py#L479)<br>**F09** rule_ids=[]→400 [L479](../../backend/routes/rule_library.py#L479)<br>**F10** 无选中资源→404 [L487](../../backend/routes/rule_library.py#L487)<br>**F11** 单项异常 results[].success=false [L507](../../backend/routes/rule_library.py#L507)<br>**F12** 错误 body→500 [L540](../../backend/routes/rule_library.py#L540) | **B01** 空 content 源<br>**B02** 多个异步结果按 id 对齐<br>**B03** 失败不改变 enabled | LIBRARY+TASK；AUTO(T16/17) | [backend/routes/rule_library.py:468](../../backend/routes/rule_library.py#L468) |
| **R171** `POST /api/rule-library/check-content` | **S01** domain 文本检查<br>**S02** classical 保持手写文本<br>**S03** append_lines 追加 | **F01–07** A1–7<br>**F08** 非对象/无 content→400 [L37](../../backend/routes/rule_library.py#L37)<br>**F09** content 非字符串→400 [L37](../../backend/routes/rule_library.py#L37)<br>**F10** append_lines 非数组→400 [L41](../../backend/routes/rule_library.py#L41)<br>**F11** append_lines 含非字符串→400 [L41](../../backend/routes/rule_library.py#L41)<br>**F12** 行内换行/回车→400 [L41](../../backend/routes/rule_library.py#L41)<br>**F13** 内容检查 errors 字段 | **B01** 空内容<br>**B02** 中文域名 IDNA<br>**B03** 空 append_lines | DB0 | [backend/routes/rule_library.py:32](../../backend/routes/rule_library.py#L32) |
| **R172** `GET /api/rule-library/content/<rule_id>` | **S01** content 文本原样<br>**S02** 空 content 返回空 | **F01** 不存在→404 [L311](../../backend/routes/rule_library.py#L311)<br>**F02** URL 类型→400 [L314](../../backend/routes/rule_library.py#L314)<br>**F03** 配置异常→500 [L320](../../backend/routes/rule_library.py#L320) | **B01** 中文 emoji 与换行<br>**B02** 无需 token 公开访问 | DB0 | [backend/routes/rule_library.py:303](../../backend/routes/rule_library.py#L303) |
| **R173** `GET /api/rule-library/proxy-domains` | **S01** 未设为空<br>**S02** 已设置代理前缀 | **F01–07** A1–7 | **B01** 特殊字符原样 | DB0 | [backend/routes/rule_library.py:325](../../backend/routes/rule_library.py#L325) |
| **R174** `POST /api/rule-library/proxy-domains` | **S01** proxy_domains=http://example.com/ trim<br>**S02** 空字符串清除 | **F01–07** A1–7<br>**F08** 非字符串→500 [L347](../../backend/routes/rule_library.py#L347)<br>**F09** 非对象→500 [L347](../../backend/routes/rule_library.py#L347)<br>**F10** 写失败→500 [L347](../../backend/routes/rule_library.py#L347) | **B01** 中文路径<br>**B02** {} 等价清空 | SYS:system_config.github_proxy_domain；AUTO(T16/17) | [backend/routes/rule_library.py:325](../../backend/routes/rule_library.py#L325) |
| **R175** `POST /api/rule-library/reorder` | **S01** ids top<br>**S02** ids bottom<br>**S03** 旧 rules 数组 | **F01–07** A1–7<br>**F08** 非法旧对象数组→404 [L292](../../backend/routes/rule_library.py#L292)<br>**F09** ids 非数组→404 [L292](../../backend/routes/rule_library.py#L292)<br>**F10** 重复或非字符串 id→404 [L292](../../backend/routes/rule_library.py#L292)<br>**F11** 未知 id→404 [L292](../../backend/routes/rule_library.py#L292)<br>**F12** 其他异常→500 [L299](../../backend/routes/rule_library.py#L299) | **B01** ids=[]<br>**B02** 旧展示字段不覆盖 | SHARED:rule_library；AUTO(T16/17) | [backend/routes/rule_library.py:281](../../backend/routes/rule_library.py#L281) |
| **R176** `POST /api/rule-library/test` | **S01** 多个 URL HEAD 检查<br>**S02** 无 URL/内容型跳过<br>**S03** 失败项自动 enabled=false | **F01–07** A1–7<br>**F08** HEAD 异常成为 results[].error [L424](../../backend/routes/rule_library.py#L424)<br>**F09** HTTP>=400 自动禁用<br>**F10** 整体异常→500 [L463](../../backend/routes/rule_library.py#L463) | **B01** 空库<br>**B02** 完成顺序不稳定按 id 对齐<br>**B03** 全成功不改变 enabled | LIBRARY+NET；AUTO(T16/17) | [backend/routes/rule_library.py:378](../../backend/routes/rule_library.py#L378) |
| **R177** `POST /api/rule-library/test-single` | **S01** 回环 GET 200 available=true<br>**S02** 远端404 返回 HTTP200 available=false | **F01–07** A1–7<br>**F08** URL 缺失→400 [L360](../../backend/routes/rule_library.py#L360)<br>**F09** URL 空→400 [L360](../../backend/routes/rule_library.py#L360)<br>**F10** 非法 scheme→500 [L373](../../backend/routes/rule_library.py#L373)<br>**F11** 连接失败→500 [L373](../../backend/routes/rule_library.py#L373) | **B01** 重定向<br>**B02** 中文编码路径 | NET | [backend/routes/rule_library.py:352](../../backend/routes/rule_library.py#L352) |
| **R178** `GET /api/rule-sets` | **S01** 只列 itemType=ruleset<br>**S02** 相对 URL 拼接 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 无 ruleset 时 []<br>**B02–05** P3–6 | DB0 | [backend/routes/rules.py:800](../../backend/routes/rules.py#L800) |
| **R179** `POST /api/rule-sets` | **S01** 引用 lib-a 强制 itemType=ruleset | **F01–07** A1–7<br>**F08** 非对象→Flask500 [L814](../../backend/routes/rules.py#L814)<br>**F09** library id 缺失→400 [L86](../../backend/routes/rules.py#L86)<br>**F10** library 不存在→400 [L86](../../backend/routes/rules.py#L86)<br>**F11** 重复 id→repo400<br>**F12–13** P1–2 | **B01** 客户端传 itemType=rule 被强制改写<br>**B02–05** P3–6 | PROFILE:rule_configs；AUTO(T16/17) | [backend/routes/rules.py:800](../../backend/routes/rules.py#L800) |
| **R180** `DELETE /api/rule-sets/<rule_set_id>` | **S01** 删除 ruleset 并清 native 资源与选择 | **F01–07** A1–7<br>**F08** 不存在→404 [L209](../../backend/routes/rules.py#L209)<br>**F09** 路径 ID 是普通 rule→404 [L209](../../backend/routes/rules.py#L209)<br>**F10** native 引用无法删除→400 [L140](../../backend/routes/rules.py#L140)<br>**F11–12** P1–2 | **B01** 重复删→404<br>**B02–05** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:824](../../backend/routes/rules.py#L824) |
| **R181** `PUT /api/rule-sets/<rule_set_id>` | **S01** 更新已有 ruleset 组合与 DNS | **F01–07** A1–7<br>**F08** 非对象→400 [L198](../../backend/routes/rules.py#L198)<br>**F09** 路径 ID 是普通 rule→404 [L209](../../backend/routes/rules.py#L209)<br>**F10** 缺/错 library→400 [L86](../../backend/routes/rules.py#L86)<br>**F11** DNS 更新错误→helper<br>**F12–13** P1–2 | **B01** 路径 id 保持稳定<br>**B02–05** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:824](../../backend/routes/rules.py#L824) |
| **R182** `GET /api/rules` | **S01** 普通规则与 hydrated ruleset<br>**S02** 相对 URL 拼接 server_domain | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 空 rule_configs<br>**B02** 禁用的 library_enabled 展示<br>**B03–06** P3–6 | DB0 | [backend/routes/rules.py:93](../../backend/routes/rules.py#L93) |
| **R183** `POST /api/rules` | **S01** DOMAIN 规则<br>**S02** IP-CIDR 规则<br>**S03** MATCH 带 udp_fallback/ntp_direct<br>**S04** ruleset 引用并剥离 source 字段 | **F01–07** A1–7<br>**F08** 非对象→400 [L61](../../backend/routes/rules.py#L61)<br>**F09** 未知 rule_type→400 [L81](../../backend/routes/rules.py#L81)<br>**F10** 空域名值→400 [L81](../../backend/routes/rules.py#L81)<br>**F11** 非法域名/CIDR→400 [L81](../../backend/routes/rules.py#L81)<br>**F12** ruleset 未给 library_rule_id→400 [L86](../../backend/routes/rules.py#L86)<br>**F13** 不存在 library id→400 [L86](../../backend/routes/rules.py#L86)<br>**F14** MATCH 非法 udp_fallback→400 [L68](../../backend/routes/rules.py#L68)<br>**F15** MATCH ntp_direct 非 bool→400 [L70](../../backend/routes/rules.py#L70)<br>**F16–17** P1–2 | **B01** 中文 emoji remark/group_name<br>**B02** 逗号型规则值<br>**B03** 重复 ID 不可落盘<br>**B04–07** P3–6 | PROFILE:rule_configs；AUTO(T16/17) | [backend/routes/rules.py:93](../../backend/routes/rules.py#L93) |
| **R184** `DELETE /api/rules/<rule_id>` | **S01** 删除普通规则并清两引擎选择<br>**S02** 删除 native 中的规则及 provider | **F01–07** A1–7<br>**F08** 不存在→404 [L209](../../backend/routes/rules.py#L209)<br>**F09** native 删除引用无法安全重写→400 [L140](../../backend/routes/rules.py#L140)<br>**F10–11** P1–2 | **B01** native YAML 坏草稿允许删规则但保留草稿<br>**B02–05** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:253](../../backend/routes/rules.py#L253) |
| **R185** `PUT /api/rules/<rule_id>` | **S01** 普通规则字段合并<br>**S02** 规则值或 policy 更新带 DNS 跟随<br>**S03** 显式 dns=default 清路由<br>**S04** ruleset 改 library 引用 | **F01–07** A1–7<br>**F08** 非对象→400 [L61](../../backend/routes/rules.py#L61)<br>**F09** 未知 rule_type→400 [L81](../../backend/routes/rules.py#L81)<br>**F10** 空域名值→400 [L81](../../backend/routes/rules.py#L81)<br>**F11** 非法域名/CIDR→400 [L81](../../backend/routes/rules.py#L81)<br>**F12** ruleset 未给 library_rule_id→400 [L86](../../backend/routes/rules.py#L86)<br>**F13** 不存在 library id→400 [L86](../../backend/routes/rules.py#L86)<br>**F14** MATCH 非法 udp_fallback→400 [L68](../../backend/routes/rules.py#L68)<br>**F15** MATCH ntp_direct 非 bool→400 [L70](../../backend/routes/rules.py#L70)<br>**F16** 不存在 rule→404 [L209](../../backend/routes/rules.py#L209)<br>**F17** MosDNS 已用规则改不支持类型→400 [L163](../../backend/routes/rules.py#L163)<br>**F18** OxiDNS 显式路由改不支持类型→400 [L167](../../backend/routes/rules.py#L167)<br>**F19** DNS 不可转换→helper<br>**F20** 自定义 native draft 严格更新失败→helper<br>**F21–22** P1–2 | **B01** 只改旧坏值以外字段仍保存<br>**B02** 空 remark<br>**B03** 请求 id 不得改存储 id<br>**B04–07** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:253](../../backend/routes/rules.py#L253) |
| **R186** `POST /api/rules/batch` | **S01** DOMAIN-SUFFIX domains 两项<br>**S02** IP-CIDR 批量<br>**S03** 已有 rule_1 时分配后续空闲 ID | **F01–07** A1–7<br>**F08** 非对象→400 [L653](../../backend/routes/rules.py#L653)<br>**F09** domains 缺失→400 [L657](../../backend/routes/rules.py#L657)<br>**F10** domains 空→400 [L657](../../backend/routes/rules.py#L657)<br>**F11** domains 含非字符串→400 [L657](../../backend/routes/rules.py#L657)<br>**F12** 非法 rule_type→400 [L661](../../backend/routes/rules.py#L661)<br>**F13** 第2项非法值→400 [L661](../../backend/routes/rules.py#L661)<br>**F14–15** P1–2 | **B01** 中文 IDNA 域名<br>**B02** 前后空白 trim<br>**B03** 并发分配 ID 不重复<br>**B04–07** P3–6 | PROFILE:rule_configs；AUTO(T16/17) | [backend/routes/rules.py:649](../../backend/routes/rules.py#L649) |
| **R187** `POST /api/rules/compose` | **S01** values 粘贴 DOMAIN-SUFFIX<br>**S02** values 混合 IP 和逗号参数<br>**S03** library 多条引用<br>**S04** new_library 内容型<br>**S05** new_library 回环 URL<br>**S06** position=bottom | **F01–07** A1–7<br>**F08** 非对象→400 [L393](../../backend/routes/rules.py#L393)<br>**F09** policy 缺失→400 [L402](../../backend/routes/rules.py#L402)<br>**F10** position 非法→400 [L405](../../backend/routes/rules.py#L405)<br>**F11** text 非字符串→400 [L413](../../backend/routes/rules.py#L413)<br>**F12** 201 行→400 [L316](../../backend/routes/rules.py#L316)<br>**F13** 值校验 field_errors→400 [L419](../../backend/routes/rules.py#L419)<br>**F14** 全空注释→400 [L421](../../backend/routes/rules.py#L421)<br>**F15** 非 DNS 类型指定 DNS→400 [L426](../../backend/routes/rules.py#L426)<br>**F16** library ids 0/51 条→400 [L432](../../backend/routes/rules.py#L432)<br>**F17** 重复 library id→400 [L432](../../backend/routes/rules.py#L432)<br>**F18** library id 非字符串→400 [L432](../../backend/routes/rules.py#L432)<br>**F19** 来源 kind 未知→400 [L436](../../backend/routes/rules.py#L436)<br>**F20** 新 library 非对象→400 [L356](../../backend/routes/rules.py#L356)<br>**F21** 新 library 空 name→400 [L359](../../backend/routes/rules.py#L359)<br>**F22** 下载失败→502 [L444](../../backend/routes/rules.py#L444)<br>**F23** 无有效来源→400 [L446](../../backend/routes/rules.py#L446)<br>**F24** 同名 library→400 [L452](../../backend/routes/rules.py#L452)<br>**F25** policy 不存在→400 [L459](../../backend/routes/rules.py#L459)<br>**F26** library 不存在→404 [L483](../../backend/routes/rules.py#L483)<br>**F27** 已引用 library→409 [L488](../../backend/routes/rules.py#L488)<br>**F28** DNS helper 拒绝原子回滚<br>**F29–30** P1–2 | **B01** 200 行<br>**B02** 50 个 library<br>**B03** 重复 values 跳过<br>**B04** 中文 emoji 组名<br>**B05** 缓存失败返回 warning 且 DB 提交成功 [L523](../../backend/routes/rules.py#L523)<br>**B06–09** P3–6 | COMPOSE；AUTO(T16/17) | [backend/routes/rules.py:380](../../backend/routes/rules.py#L380) |
| **R188** `GET /api/rules/dns` | **S01** template 路由状态<br>**S02** native 路由状态 | **F01–07** A1–7<br>**F08–09** P1–2 | **B01** 空规则集<br>**B02** disabled 暂存选择<br>**B03–06** P3–6 | DB0 | [backend/routes/rules.py:265](../../backend/routes/rules.py#L265) |
| **R189** `PUT /api/rules/dns` | **S01** routes={rule-a:direct}<br>**S02** follow 路由<br>**S03** default 清显式路由<br>**S04** skip_unsupported=true 跳过不可转换项 | **F01–07** A1–7<br>**F08** 非对象→400 [L278](../../backend/routes/rules.py#L278)<br>**F09** routes 缺失→400 [L278](../../backend/routes/rules.py#L278)<br>**F10** routes={}→400 [L278](../../backend/routes/rules.py#L278)<br>**F11** 201 条→400 [L280](../../backend/routes/rules.py#L280)<br>**F12** 非法 choice→helper<br>**F13** 未知 rule→helper<br>**F14** 自定义 native 不能安全修改→helper<br>**F15** 不可转换 rule 且不 skip→helper<br>**F16–17** P1–2 | **B01** 200 条<br>**B02** 混合支持与不支持规则<br>**B03–06** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:265](../../backend/routes/rules.py#L265) |
| **R190** `POST /api/rules/find-duplicates` | **S01** 无重复<br>**S02** 同一 ruleset 内重复<br>**S03** 跨直接规则和 ruleset 重复且策略冲突 | **F01–07** A1–7<br>**F08** 规则读取/解析异常→500 [L1107](../../backend/routes/rules.py#L1107)<br>**F09** 下载失败返回 failed_rulesets 而非全失败<br>**F10–11** P1–2 | **B01** REGEX 大小写不合并<br>**B02** 域名大小写合并<br>**B03** 稳定 tie 排序<br>**B04** 长规则集+取消客户端<br>**B05–08** P3–6 | RULE-FILE；AUTO(T16/17)；长流程 SCAN | [backend/routes/rules.py:987](../../backend/routes/rules.py#L987) |
| **R191** `POST /api/rules/group-name` | **S01** ids 单条改组名<br>**S02** 多条组名改为空解除 | **F01–07** A1–7<br>**F08** 非对象/缺 group_name→400 [L537](../../backend/routes/rules.py#L537)<br>**F09** ids 空→400 [L542](../../backend/routes/rules.py#L542)<br>**F10** 重复 id→400 [L542](../../backend/routes/rules.py#L542)<br>**F11** 非字符串 id→400 [L542](../../backend/routes/rules.py#L542)<br>**F12** 成员被删除→404 [L555](../../backend/routes/rules.py#L555)<br>**F13–14** P1–2 | **B01** 中文 emoji<br>**B02** 组名称空白 trim<br>**B03** 超长组名按 Python<br>**B04–07** P3–6 | PROFILE:rule_configs.group_name；AUTO(T16/17) | [backend/routes/rules.py:532](../../backend/routes/rules.py#L532) |
| **R192** `GET /api/rules/local/<name>` | **S01** 内容型缓存存在<br>**S02** 内容型缺缓存自动写入<br>**S03** URL 有效缓存<br>**S04** URL 刷新失败用旧验证缓存<br>**S05** 库无条目但旧缓存存在<br>**S06** target=mihomo 规范化 | **F01** 库和缓存均不存在→404 [L758](../../backend/routes/rules.py#L758)<br>**F02** 远端内容非法且无有效缓存→422 [L772](../../backend/routes/rules.py#L772)<br>**F03** 网络失败无缓存→502 [L775](../../backend/routes/rules.py#L775)<br>**F04** 读取异常→500 [L791](../../backend/routes/rules.py#L791)<br>**F05–06** P1–2 | **B01** 中文 emoji 名称<br>**B02** 路径特殊字符不能逃逸<br>**B03** 非 UTF8 目标归一失败时返回原字节<br>**B04** 空内容<br>**B05–08** P3–6 | RULE-FILE；AUTO(T16/17)；长流程 RULE-FILE | [backend/routes/rules.py:691](../../backend/routes/rules.py#L691) |
| **R193** `POST /api/rules/match-test` | **S01** 域名命中<br>**S02** IPv4 命中<br>**S03** IPv6 命中<br>**S04** 无匹配返回默认 | **F01–07** A1–7<br>**F08** query 缺失→400 [L900](../../backend/routes/rules.py#L900)<br>**F09** query 空白→400 [L900](../../backend/routes/rules.py#L900)<br>**F10** 非法 query→400 [L905](../../backend/routes/rules.py#L905)<br>**F11** 远端规则非法→422 [L920](../../backend/routes/rules.py#L920)<br>**F12** 非对象/数字 query→500 [L941](../../backend/routes/rules.py#L941)<br>**F13–14** P1–2 | **B01** DNS 尾点<br>**B02** 中文域名<br>**B03** 禁用项跳过<br>**B04–07** P3–6 | RULE-FILE；AUTO(T16/17) | [backend/routes/rules.py:887](../../backend/routes/rules.py#L887) |
| **R194** `PUT /api/rules/policy` | **S01** 内置 DIRECT<br>**S02** 存在组名<br>**S03** group_name 省略保留<br>**S04** group_name 空清除 | **F01–07** A1–7<br>**F08** 非对象→400 [L572](../../backend/routes/rules.py#L572)<br>**F09** ids 空→400 [L577](../../backend/routes/rules.py#L577)<br>**F10** 重复 id→400 [L577](../../backend/routes/rules.py#L577)<br>**F11** id 非字符串→400 [L577](../../backend/routes/rules.py#L577)<br>**F12** policy 缺失→400 [L581](../../backend/routes/rules.py#L581)<br>**F13** group_name 非文本→400 [L584](../../backend/routes/rules.py#L584)<br>**F14** policy 不存在→400 [L591](../../backend/routes/rules.py#L591)<br>**F15** 成员已删除→404 [L607](../../backend/routes/rules.py#L607)<br>**F16–17** P1–2 | **B01** 超过 200 条仍允许<br>**B02** 自定义 native 返回 warning<br>**B03** 显式 DNS 不跟随<br>**B04–07** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:561](../../backend/routes/rules.py#L561) |
| **R195** `POST /api/rules/reorder` | **S01** ids top<br>**S02** ids bottom<br>**S03** 旧 rule_configs 对象数组 | **F01–07** A1–7<br>**F08** 非对象/空 body→400 [L622](../../backend/routes/rules.py#L622)<br>**F09** 旧数组错型→400 [L627](../../backend/routes/rules.py#L627)<br>**F10** ids 非数组→400 [L630](../../backend/routes/rules.py#L630)<br>**F11** 未知 ID→404 [L638](../../backend/routes/rules.py#L638)<br>**F12** 重复 id 触发 repo 校验<br>**F13** 非字符串项按 Python 错误<br>**F14–15** P1–2 | **B01** ids=[] 不变<br>**B02** 旧对象不能覆盖字段<br>**B03** 顺序变更联动 native<br>**B04–07** P3–6 | RULE；AUTO(T16/17) | [backend/routes/rules.py:613](../../backend/routes/rules.py#L613) |
| **R196** `GET /api/server-domain` | **S01** 默认 server_domain<br>**S02** 已设置 server_domain | **F01–07** A1–7 | **B01** 中文路径与空域名 | DB0 | [backend/routes/settings.py:70](../../backend/routes/settings.py#L70) |
| **R197** `POST /api/server-domain` | **S01** new_domain=http://example.com:18080/ 并 trim | **F01–07** A1–7<br>**F08** 缺 new_domain→400 [L86](../../backend/routes/settings.py#L86)<br>**F09** 空白→400 [L86](../../backend/routes/settings.py#L86)<br>**F10** 数字→500 [L104](../../backend/routes/settings.py#L104)<br>**F11** 坏 JSON→500 [L104](../../backend/routes/settings.py#L104)<br>**F12** 保存异常→500 [L104](../../backend/routes/settings.py#L104) | **B01** 域名含中文 emoji 路径<br>**B02** 8193 字符文本不擅加长度限制 | SYS:system_config.server_domain；AUTO(T16/17) | [backend/routes/settings.py:70](../../backend/routes/settings.py#L70) |
| **R198** `GET /api/settings/dependencies` | **S01** 本地版本状态<br>**S02** refresh=1 强制检查<br>**S03** refresh=true 强制检查 | **F01–07** A1–7<br>**F08** release 查询失败作为状态中的 error<br>**F09** 本地文件不可读状态 | **B01** 无更新和已最新<br>**B02** refresh=TRUE 不强制 | DEP；长流程 DEP | [backend/routes/settings.py:372](../../backend/routes/settings.py#L372) |
| **R199** `POST /api/settings/dependencies/sub-store/mode` | **S01** mode=on_demand<br>**S02** mode=always | **F01–07** A1–7<br>**F08** 缺 mode→400 [L395](../../backend/routes/settings.py#L395)<br>**F09** 非法 mode→400 [L395](../../backend/routes/settings.py#L395)<br>**F10** 非对象→400 [L395](../../backend/routes/settings.py#L395)<br>**F11** 外部服务→409 [L401](../../backend/routes/settings.py#L401)<br>**F12** 更新或使用中冲突→409 [L401](../../backend/routes/settings.py#L401)<br>**F13** 服务控制失败→409 [L401](../../backend/routes/settings.py#L401) | **B01** 幂等设置同一 mode | DEP；长流程 DEP | [backend/routes/settings.py:391](../../backend/routes/settings.py#L391) |
| **R200** `POST /api/settings/dependencies/sub-store/update` | **S01** 有新版本→202 并轮询完成 | **F01–07** A1–7<br>**F08** 并发更新→409 [L386](../../backend/routes/settings.py#L386)<br>**F09** 当前最新版→409<br>**F10** 无法获取新版本→409<br>**F11** 无内置目录→409<br>**F12** 外部服务→409 | **B01** 重复请求不启动两个 worker | DEP；长流程 DEP | [backend/routes/settings.py:381](../../backend/routes/settings.py#L381) |
| **R201** `GET /api/settings/rule-fetch-proxy` | **S01** 未设置返回空<br>**S02** 已保存带凭据代理可供编辑 | **F01–07** A1–7 | **B01** 特殊用户名密码字符不会被通用脱敏误删 | DB0 | [backend/routes/settings.py:35](../../backend/routes/settings.py#L35) |
| **R202** `POST /api/settings/rule-fetch-proxy` | **S01** HTTP 代理 http://127.0.0.1:18081<br>**S02** HTTPS 代理 https://example.com:443<br>**S03** 空字符串清除<br>**S04** 前后空白裁剪 | **F01–07** A1–7<br>**F08** 缺字段→400 [L45](../../backend/routes/settings.py#L45)<br>**F09** 非对象→400 [L45](../../backend/routes/settings.py#L45)<br>**F10** socks5 scheme→400 [L53](../../backend/routes/settings.py#L53)<br>**F11** 坏端口→400 [L53](../../backend/routes/settings.py#L53)<br>**F12** 无主机→400 [L53](../../backend/routes/settings.py#L53)<br>**F13** 保存失败→500 [L62](../../backend/routes/settings.py#L62) | **B01** 中文凭据与百分号编码<br>**B02** 超长代理文本按 Python 接受或拒绝 | SYS:system_config.rule_fetch_proxy；AUTO(T16/17) | [backend/routes/settings.py:35](../../backend/routes/settings.py#L35) |
| **R203** `GET /api/settings/subscription-aggregation` | **S01** 默认 false<br>**S02** 已保存 true | **F01–07** A1–7 | **B01** 旧字符串值按保存形态返回 | DB0 | [backend/routes/settings.py:336](../../backend/routes/settings.py#L336) |
| **R204** `POST /api/settings/subscription-aggregation` | **S01** enabled=true<br>**S02** enabled=false<br>**S03** {} 默认 false<br>**S04** enabled=[] 按 Python 不做布尔转换 | **F01–07** A1–7<br>**F08** JSON 数组→500 [L367](../../backend/routes/settings.py#L367)<br>**F09** 坏 JSON→500 [L367](../../backend/routes/settings.py#L367)<br>**F10** 写失败→500 [L367](../../backend/routes/settings.py#L367) | **B01** enabled=null<br>**B02** 超长 enabled 文本 | SYS:system_config.subscription_aggregation_enabled；AUTO(T16/17) | [backend/routes/settings.py:336](../../backend/routes/settings.py#L336) |
| **R205** `GET /api/settings/web-port` | **S01** 独立临时部署已配置监听端口<br>**S02** 无 saved port 的默认状态 | **F01–07** A1–7<br>**F08** 配置文件不可读→503 [L30](../../backend/routes/settings.py#L30)<br>**F09** 保存端口损坏→helper | **B01** 端口 1 和 65535 的状态 | WEB；长流程 WEB | [backend/routes/settings.py:17](../../backend/routes/settings.py#L17) |
| **R206** `POST /api/settings/web-port` | **S01** 可修改部署 port=18080<br>**S02** 相同端口重复提交 | **F01–07** A1–7<br>**F08** body 缺 port→400 [L24](../../backend/routes/settings.py#L24)<br>**F09** port=字符串→400<br>**F10** port=true→400<br>**F11** port=0→400<br>**F12** port=65536→400<br>**F13** 保留端口→409<br>**F14** 端口占用→409<br>**F15** 并发持锁→409<br>**F16** 不支持修改的部署→helper<br>**F17** Nginx 检查失败→helper<br>**F18** 重载失败并回滚→helper<br>**F19** 回滚失败→helper<br>**F20** 目录权限→503 [L30](../../backend/routes/settings.py#L30) | **B01** port=1 权限行为<br>**B02** port=65535 | WEB；长流程 WEB | [backend/routes/settings.py:17](../../backend/routes/settings.py#L17) |
| **R207** `GET /api/setup` | **S01** 初次 suggested=true<br>**S02** 已有 session 列表<br>**S03** 明确 abandoned 不再提示<br>**S04** 已有 Agent 不提示 | **F01–07** A1–7 | **B01** 禁用资源不列为可选<br>**B02** 中文 emoji 名称 | SETUP；AUTO(T16/17)；长流程 SETUP<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/setup.py:50](../../backend/routes/setup.py#L50) |
| **R208** `POST /api/setup` | **S01** goal=router<br>**S02** goal=gateway<br>**S03** 相同 id/name/goal 幂等继续 | **F01–07** A1–7<br>**F08** body 超32KiB→413 [L14](../../backend/routes/setup.py#L14)<br>**F09** 非对象/坏JSON→400 [L17](../../backend/routes/setup.py#L17)<br>**F10** goal 非法→400<br>**F11** name 缺失/空→400<br>**F12** name 超64字符→400<br>**F13** session_id 非法→404<br>**F14** 同 session 改目标或名→409<br>**F15** profile-id 已占用→409 | **B01** 64字符中文 emoji 名称<br>**B02** 首次自动 config_token<br>**B03** 并发相同 session 只创建一个 profile<br>**B90 [O6]** `Transfer-Encoding: chunked` 在线上无 Content-Length 的 >32KiB body：生产 Waitress 解码后补 CONTENT_LENGTH，实际413；直接Flask WSGI缺长度语境不等同生产HTTP（Step B oracle更正，[setup.py:13](../../backend/routes/setup.py#L13)、[http_server.py:114](../../backend/http_server.py#L114)） | SETUP；AUTO(T16/17)；长流程 SETUP | [backend/routes/setup.py:50](../../backend/routes/setup.py#L50) |
| **R209** `GET /api/setup/<session_id>` | **S01** 读取未准备 session<br>**S02** 读取已准备 session 与 default_profile<br>**S03** 旧 prepared session 补 legacy_takeover | **F01–07** A1–7<br>**F08** session 不存在→404<br>**F09** profile 已删除→404 [L27](../../backend/routes/setup.py#L27)<br>**F10** session id 非法→404 | **B01** 损坏 session JSON<br>**B02** 中文 emoji | SETUP；AUTO(T16/17)；长流程 SETUP | [backend/routes/setup.py:72](../../backend/routes/setup.py#L72) |
| **R210** `PATCH /api/setup/<session_id>` | **S01** step 在已解锁范围内<br>**S02** router_mode=manual<br>**S03** router_mode=managed<br>**S04** router_id 当前 profile 内<br>**S05** abandoned 继续时移除标记 | **F01–07** A1–7<br>**F08** 32KiB 超限→413<br>**F09** 非对象→400<br>**F10** 额外字段→400<br>**F11** step 越过 max→400<br>**F12** step bool/非整数→400<br>**F13** 非法 router_mode→400<br>**F14** gateway 目标设置 router_mode→400<br>**F15** router 不在本 profile→409<br>**F16** session 或 profile 不存在→404 | **B01** step=0<br>**B02** 空对象幂等<br>**B90 [O6]** `Transfer-Encoding: chunked` 在线上无 Content-Length 的 >32KiB body：生产 Waitress 解码后补 CONTENT_LENGTH，实际413；直接Flask WSGI缺长度语境不等同生产HTTP（Step B oracle更正，[setup.py:13](../../backend/routes/setup.py#L13)、[http_server.py:114](../../backend/http_server.py#L114)） | SETUP；AUTO(T16/17)；长流程 SETUP | [backend/routes/setup.py:72](../../backend/routes/setup.py#L72) |
| **R211** `POST /api/setup/<session_id>/<operation>` | **S01** source HTTP URL<br>**S02** source 已有 subscriptions<br>**S03** source 已有 nodes<br>**S04** prepare dns_fallback=mihomo/traffic_fallback=proxy<br>**S05** prepare direct/direct<br>**S06** prepare mihomo/direct<br>**S07** prepare direct/proxy<br>**S08** router-draft 保存<br>**S09** router-draft router=null 撤回<br>**S10** check 假 Agent 通过<br>**S11** finish manual<br>**S12** finish managed<br>**S13** abandon dry_run=true<br>**S14** abandon 执行 | **F01–07** A1–7<br>**F08** 请求超限→413<br>**F09** 坏/非对象 JSON→400<br>**F10** session 不存在→404<br>**F11** profile 删除→404<br>**F12** operation 未知→404 [L124](../../backend/routes/setup.py#L124)<br>**F13** abandon 多余字段或 dry_run 非bool→400 [L97](../../backend/routes/setup.py#L97)<br>**F14** 未 prepared 的 check→409 [L112](../../backend/routes/setup.py#L112)<br>**F15** 未 prepared 的 finish→409 [L112](../../backend/routes/setup.py#L112)<br>**F16** 已完成 abandon→409<br>**F17** 来源格式/删除/停用→helper<br>**F18** prepare 字段或 DNS 参数错误→helper<br>**F19** prepare 空节点→helper<br>**F20** prepare revision 冲突→helper<br>**F21** router-draft 校验失败→helper<br>**F22** check Agent 离线/版本/转发/TUN错误→helper<br>**F23** finish 未确认/新 revision→helper | **B01** 断连后继续提交<br>**B02** 两个同 session 请求串行<br>**B03** 名称/备注中文 emoji<br>**B04** 只改 router 状态不当作核心配置改动<br>**B90 [O6]** `Transfer-Encoding: chunked` 在线上无 Content-Length 的 >32KiB body：生产 Waitress 解码后补 CONTENT_LENGTH，实际413；直接Flask WSGI缺长度语境不等同生产HTTP（Step B oracle更正，[setup.py:13](../../backend/routes/setup.py#L13)、[http_server.py:114](../../backend/http_server.py#L114)）<br>**B91 [O6]** `check` 不清除 abandoned 标记，其他操作会清除（[setup.py:102](../../backend/routes/setup.py#L102)）；`prepare` 用 `request.url_root`（只看 Host，不看 X-Forwarded-*） | SETUP；AUTO(T16/17)；长流程 SETUP<br>**[O6]** 需要 Agent 在线/版本时，fake Agent 先向两端各发相同心跳（在线状态只在内存，种 DB 不够） | [backend/routes/setup.py:88](../../backend/routes/setup.py#L88) |
| **R212** `GET /api/stats/overview` | **S01** 首次采样<br>**S02** 不到 30 分钟更新响应末样本但不落盘<br>**S03** 超过 30 分钟追加 | **F01–07** A1–7<br>**F08** 非法计数结构→500 [L109](../../backend/routes/stats.py#L109)<br>**F09** 历史不可读走空历史恢复<br>**F10** 历史不可写仍响应成功<br>**F11–12** P1–2 | **B01** 48→49 样本只保留 48<br>**B02** 空资源计数 0<br>**B03** 历史时间无效<br>**B04–07** P3–6 | STAT | [backend/routes/stats.py:84](../../backend/routes/stats.py#L84) |
| **R213** `GET /api/subscriptions` | **S01** 含原始缓存节点计数及筛选计数<br>**S02** 无缓存返回 null | **F01–07** A1–7 | **B01** 空订阅列表<br>**B02** 旧 count-only 缓存 | DB0 | [backend/routes/subscriptions.py:69](../../backend/routes/subscriptions.py#L69) |
| **R214** `POST /api/subscriptions` | **S01** 典型 {id:sub-a,name:测试订阅,url:回环URL}<br>**S02** 多行 URL<br>**S03** 健康检查默认值初始化 | **F01–07** A1–7<br>**F08** 非对象→repo400<br>**F09** name 缺失→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F10** name 空白→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F11** url 缺失→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F12** url 空白→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F13** name 非字符串→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F14** url 非字符串→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F15** 非法 regex_filter→400 [L56](../../backend/routes/subscriptions.py#L56)<br>**F16** 非法 interval→400 [L61](../../backend/routes/subscriptions.py#L61)<br>**F17** 重复 ID→repo400 | **B01** interval=0<br>**B02** interval=60<br>**B03** interval=2592000<br>**B04** DERIVED_FIELDS 回写剥除 | SHARED:subscriptions；AUTO(T16/17) | [backend/routes/subscriptions.py:69](../../backend/routes/subscriptions.py#L69) |
| **R215** `GET /api/subscriptions/<resource_id>/health-impact` | **S01** 引用一个 profile<br>**S02** 引用多个 profile<br>**S03** 没有引用的资源 | **F01–07** A1–7<br>**F08** 不存在资源按 Python 返回空用途而非假设 404 | **B01** 名称中文 emoji | DB0 | [backend/routes/subscriptions.py:36](../../backend/routes/subscriptions.py#L36) |
| **R216** `DELETE /api/subscriptions/<sub_id>` | **S01** 删除未引用订阅并清关联缓存/健康/task<br>**S02** 不存在订阅幂等成功 | **F01–07** A1–7<br>**F08** 被组引用→repo409<br>**F09** 被聚合引用→repo409<br>**F10** 被其他 profile 引用→repo409 | **B01** 重复删除<br>**B02** 无缓存文件 | SUB；AUTO(T16/17) | [backend/routes/subscriptions.py:115](../../backend/routes/subscriptions.py#L115) |
| **R217** `PUT /api/subscriptions/<sub_id>` | **S01** 整对象替换且 id 固定<br>**S02** URL 未变保留 task 历史<br>**S03** URL 改变清 task 历史 | **F01–07** A1–7<br>**F08** 非对象→repo400<br>**F09** name 缺失→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F10** name 空白→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F11** url 缺失→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F12** url 空白→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F13** name 非字符串→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F14** url 非字符串→400 [L53](../../backend/routes/subscriptions.py#L53)<br>**F15** 非法 regex_filter→400 [L56](../../backend/routes/subscriptions.py#L56)<br>**F16** 非法 interval→400 [L61](../../backend/routes/subscriptions.py#L61)<br>**F17** 重复 ID→repo400<br>**F18** 不存在→404 [L144](../../backend/routes/subscriptions.py#L144)<br>**F19** 仍被引用时删除字段→repo409 | **B01** 中文 emoji 名称<br>**B02** 派生字段剥除 | SUB；AUTO(T16/17) | [backend/routes/subscriptions.py:115](../../backend/routes/subscriptions.py#L115) |
| **R218** `POST /api/subscriptions/<sub_id>/fetch` | **S01** preview=true 新下载<br>**S02** preview=false 导入节点<br>**S03** 远端失败回退缓存<br>**S04** rendered_yaml 直接复用<br>**S05** Sub-Store 转换<br>**S06** direct_url_fallback | **F01–07** A1–7<br>**F08** 不存在→404 [L230](../../backend/routes/subscriptions.py#L230)<br>**F09** 网络失败且无缓存→500 [L272](../../backend/routes/subscriptions.py#L272)<br>**F10** 坏 JSON→Flask400<br>**F11** 非对象 body→Flask500<br>**F12** 导入节点结构异常→500 [L316](../../backend/routes/subscriptions.py#L316)<br>**F13** 写配置冲突→repo | **B01** 按名去重<br>**B02** 筛选后空节点<br>**B03** preview 仍写缓存与健康 | SUB；AUTO(T16/17)；长流程 SUB | [backend/routes/subscriptions.py:223](../../backend/routes/subscriptions.py#L223) |
| **R219** `GET /api/subscriptions/<sub_id>/nodes` | **S01** 默认返回已导入节点<br>**S02** source=cache 返回筛选后缓存<br>**S03** 无缓存 cache_available=false<br>**S04** 缓存 URL 不匹配视为无缓存 | **F01–07** A1–7<br>**F08** 不存在订阅→404 [L196](../../backend/routes/subscriptions.py#L196) | **B01** 空缓存 nodes=[] 仍 available<br>**B02** 旧缓存无 metadata 可读 | DB0 | [backend/routes/subscriptions.py:189](../../backend/routes/subscriptions.py#L189) |
| **R220** `GET /api/subscriptions/<sub_id>/proxies` | **S01** 默认 Mihomo YAML<br>**S02** format=surge 文本<br>**S03** format=loon 文本<br>**S04** 远端失败返回节点缓存<br>**S05** Surge/Loon regex 筛选<br>**S06** 无 URL 读取缓存 | **F01–07** A1–7<br>**F08** 认证失败→401 [L487](../../backend/routes/subscriptions.py#L487)<br>**F09** 对象不存在→404 [L495](../../backend/routes/subscriptions.py#L495)<br>**F10** 读取配置异常→500 [L498](../../backend/routes/subscriptions.py#L498)<br>**F11** 非法 filters→400 [L505](../../backend/routes/subscriptions.py#L505)<br>**F12** 链用 Surge→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F13** 链用 Loon→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F14** 坏图不可回缓存→400 [L690](../../backend/routes/subscriptions.py#L690)<br>**F15** 无缓存→404 [L600](../../backend/routes/subscriptions.py#L600)<br>**F16** 缓存无节点→404 [L607](../../backend/routes/subscriptions.py#L607)<br>**F17** 其他异常→500 [L692](../../backend/routes/subscriptions.py#L692)<br>**F18** D4/D5 内部能力与JWT混用依然拒绝；D1/D2仅query先被A拦截 | **B01** 筛选后空集合<br>**B02** 中文 emoji 节点名<br>**B03** format=unknown 按 YAML 但不触发默认 fallback<br>**B90 [O6]** YAML 头注释 `# Generated: <本地时间>Z`（本地时间后接字面 Z）和 `Content-Disposition` 文件名时间戳随 TZ 变化（X05）<br>**B91 [O6]** HEAD 也会执行 handler（拉取上游、写缓存/健康记录）：比副作用而非只比空 body（X06） | PROVIDER；长流程 PROVIDER | [backend/routes/subscriptions.py:471](../../backend/routes/subscriptions.py#L471) |
| **R221** `GET /api/subscriptions/health` | **S01** 有成功失败历史<br>**S02** 无历史项 | **F01–07** A1–7 | **B01** 空列表<br>**B02** 禁用源仍可见 | DB0 | [backend/routes/subscriptions.py:169](../../backend/routes/subscriptions.py#L169) |
| **R222** `GET /api/subscriptions/proxies` | **S01** 多个订阅合并 YAML<br>**S02** 禁用订阅跳过<br>**S03** 网络错误使用有效缓存<br>**S04** 无可用节点返回空 proxies | **F01–07** A1–7<br>**F08** 缺登录/错能力→401 [L341](../../backend/routes/subscriptions.py#L341)<br>**F09** 跨 feed 名称冲突→400 [L465](../../backend/routes/subscriptions.py#L465)<br>**F10** 坏拨号引用→400 [L465](../../backend/routes/subscriptions.py#L465)<br>**F11** 非法内容不可降级→400 [L465](../../backend/routes/subscriptions.py#L465)<br>**F12** 异常配置→500 [L467](../../backend/routes/subscriptions.py#L467)<br>**F13** D4/D5 内部能力与JWT混用依然拒绝；D1/D2仅query先被A拦截 | **B01** 中文 emoji 同名冲突<br>**B02** 空 enabled 集合<br>**B90 [O6]** YAML 头注释 `# Generated: <本地时间>Z`（本地时间后接字面 Z）和 `Content-Disposition` 文件名时间戳随 TZ 变化（X05）<br>**B91 [O6]** HEAD 也会执行 handler（拉取上游、写缓存/健康记录）：比副作用而非只比空 body（X06） | PROVIDER；长流程 PROVIDER | [backend/routes/subscriptions.py:326](../../backend/routes/subscriptions.py#L326) |
| **R223** `POST /api/subscriptions/reorder` | **S01** ids top<br>**S02** ids bottom<br>**S03** 旧 subscriptions 数组 | **F01–07** A1–7<br>**F08** 重复 ID→404 [L156](../../backend/routes/subscriptions.py#L156)<br>**F09** 非字符串 ID→404 [L156](../../backend/routes/subscriptions.py#L156)<br>**F10** ids 非数组→404 [L156](../../backend/routes/subscriptions.py#L156)<br>**F11** 未知 ID→404 [L156](../../backend/routes/subscriptions.py#L156)<br>**F12** 非对象→500 [L164](../../backend/routes/subscriptions.py#L164) | **B01** ids=[]<br>**B02** 旧派生字段不落库 | SHARED:subscriptions；AUTO(T16/17) | [backend/routes/subscriptions.py:149](../../backend/routes/subscriptions.py#L149) |
| **R224** `GET /api/subscriptions/test` | **S01** success 与 timestamp | **F01–07** A1–7 | **B01** timestamp 格式检查不整字段忽略 | DB0 | [backend/routes/subscriptions.py:320](../../backend/routes/subscriptions.py#L320) |
| **R225** `GET /api/tasks` | **S01** 空资源也包含系统任务<br>**S02** 订阅与 URL 规则任务<br>**S03** 回环 Agent/router 任务状态 | **F01–07** A1–7 | **B01** 关闭间隔 0<br>**B02** 失败连击与 next_run_at<br>**B03** 无 URL 规则跳过 | TASK；长流程 TASK | [backend/routes/tasks.py:265](../../backend/routes/tasks.py#L265) |
| **R226** `PUT /api/tasks/interval` | **S01** subscription:sub-a interval_seconds=60<br>**S02** rule_source:lib-a interval_seconds=0<br>**S03** metrics_history_flush 合法间隔 | **F01–07** A1–7<br>**F08** 缺 key→400 [L337](../../backend/routes/tasks.py#L337)<br>**F09** 额外字段→400 [L337](../../backend/routes/tasks.py#L337)<br>**F10** 缺 interval→400 [L337](../../backend/routes/tasks.py#L337)<br>**F11** 不支持 category→400 [L347](../../backend/routes/tasks.py#L347)<br>**F12** 非法刷新间隔→400 [L351](../../backend/routes/tasks.py#L351)<br>**F13** metrics 非法间隔→400 [L343](../../backend/routes/tasks.py#L343)<br>**F14** 不存在资源→404 [L364](../../backend/routes/tasks.py#L364) | **B01** 最大间隔 2592000<br>**B02** 59→400<br>**B03** 并发只改间隔保留 URL<br>**B90 [O6]** interval_seconds 为 60.0、6e1、true、"60"、2**63、NaN→按 `type(x) is int` 拒绝（X04）；`rule_source:<content 源>` 仍写 update_interval（无 URL 检查） | SHARED+TASK；AUTO(T16/17)；长流程 TASK | [backend/routes/tasks.py:331](../../backend/routes/tasks.py#L331) |
| **R227** `POST /api/tasks/run` | **S01** subscription:sub-a→202<br>**S02** rule_source:lib-a→202<br>**S03** domain_discovery:alpha→202<br>**S04** webdav_backup→202<br>**S05** metrics_history_flush→202<br>**S06** dns_follow:agent-a→202<br>**S07** router_rule_sync:alpha:router-a→202<br>**S90 [O6]** `webdav_backup` 未配置 WebDAV 仍 202，随后 task-status 记失败并释放锁，可再次运行 | **F01–07** A1–7<br>**F08** 缺 key→400 [L283](../../backend/routes/tasks.py#L283)<br>**F09** key 非字符串→400 [L283](../../backend/routes/tasks.py#L283)<br>**F10** 未知 category→400 [L319](../../backend/routes/tasks.py#L319)<br>**F11** 资源不存在→404 [L323](../../backend/routes/tasks.py#L323)<br>**F12** TaskBusy→409 [L321](../../backend/routes/tasks.py#L321)<br>**F13** 自动域名探测关闭→409 [L304](../../backend/routes/tasks.py#L304)<br>**F14** router Job 错误→409 [L295](../../backend/routes/tasks.py#L295)<br>**F15** ValueError→409 [L325](../../backend/routes/tasks.py#L325)<br>**F90 [O6]** `domain_discovery:<不存在 profile>`：ProfileNotFound 是 KeyError→被 `except LookupError` 捕获→404，消息只有 id（`str(error).strip("'")`），不是全局的 `Profile not found:`（[tasks.py:322](../../backend/routes/tasks.py#L322)、[config_repository.py:41](../../backend/common/config_repository.py#L41)）<br>**F91 [O6]** `domain_discovery:bad!id`→ProfileValidationError（ValueError）→409 `Invalid profile id`；`subscription:<无 URL>`、`rule_source:<content 源>`→409 `这个来源没有订阅地址，不需要更新`；`subscription:`（空资源）→404 | **B01** 同 key 并发只启动一份<br>**B02** 客户端断开任务仍完成<br>**B90 [O6]** `metrics_history_flush:x`→400（只认整串 key） | TASK；长流程 TASK | [backend/routes/tasks.py:280](../../backend/routes/tasks.py#L280) |
| **R228** `GET /api/version` | **S01** 公开版本字段 | **F01** 公开无业务失败分支：非法 method→405 | **B01** 错误 JWT 仍可访问<br>**B90 [O6]** Go 版本号来源（构建注入）与 Python `__version__` 不同：比字段形态；值的约定交 O12/O14，不能把差异直接忽略 | DB0 | [backend/routes/settings.py:171](../../backend/routes/settings.py#L171) |

## 共享 helper 错误分支明细

下列 H 明细是路由表的补充，不是测试实现或已达分支覆盖率。每项给出**公开驱动入口、Python 错误位置、输入触发 guard、错误/诊断结果**。输入从合法 fixture 起步，让所列 guard 为真；`捕获 X` 表示使用 loopback fake、坏文件或隔离环境制造 X。`for key ...` 必须逐 key 参数化，不能只测第一个 key。f-string 内 `{key}`/`{label}` 用该参数实例化的中文消息。诊断返回和累积项有时仍为 HTTP200，必须比 `valid/issues/references/results[]`，不能只检查 status。

所有存储写路由还需用 H-REPO 的通用文档校验和冲突分支：重复/空 ID、坏类型、失效共享引用、被使用资源、外部 commit 冲突、只读/损坏数据库。内部 API 参数（如不可调用 updater）、启动完整性检查、固定文件名等**无法以合法 HTTP 形态触发的 guard**只留作审计哨兵，不算实际可达错误已覆盖；Step B 要注明故障注入或 N/A，不允许伪造调用 Go helper。旧备份导入仍测试；被维护者取消的 pre-SQLite 启动迁移不在范围。H 行数是保守的分支配方数，包含这种哨兵；汇总单独给出，不冒充可执行 HTTP 用例数。

文件键 Kxx 定位到下列 Python 文件；源行 Kxx:nnn 是确切错误分支。驱动 Rnnn 对应上表完整 method/模板；匿名函数或环境哨兵按说明在可达性审核中展开。

### K38 [backend/utils/mosdns_archive.py](../../backend/utils/mosdns_archive.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H001 / ARCHIVE | R068 | K38:30 | `not isinstance(value, str) or not value or '\\' in value or any((ord(c) < 32 for c in value))` → `'规则文件路径无效'` |
| H002 / ARCHIVE | R068 | K38:35 | `path.is_absolute() or any((part in ('', '.', '..') for part in value.split('/')))` → `'规则文件必须使用安全的相对路径'` |
| H003 / ARCHIVE | R068 | K38:72 | `not path.startswith(prefix)` → `'规则回调引用了其他配置'` |
| H004 / ARCHIVE | R068 | K38:84 | `depth > 3` → `'规则回调存在循环引用'` |
| H005 / ARCHIVE | R068 | K38:86 | `not isinstance(url, str) or not url` → `'规则缺少下载地址'` |
| H006 / ARCHIVE | R068 | K38:97 | `part is not None and part not in RULE_PARTS` → `'规则转换类型无效'` |
| H007 / ARCHIVE | R068 | K38:104 | `library_id is None and name is None` → `'无法识别规则回调'` |
| H008 / ARCHIVE | R068 | K38:114 | `外部依赖/存储返回异常` → `'规则不在当前配置快照中'` |
| H009 / ARCHIVE | R068 | K38:128 | `not isinstance(content, (str, bytes))` → `'规则文件缺少内容'` |
| H010 / ARCHIVE | R068 | K38:134 | `path in files` → `'生成配置包含内容冲突的文件路径'` |
| H011 / ARCHIVE | R068 | K38:137 | `len(files) >= MAX_FILES or total > MAX_TOTAL_BYTES` → `'生成配置超过打包大小限制'` |
| H012 / ARCHIVE | R068 | K38:152 | `捕获 Exception` → `f'规则“{_label(item)}”准备失败：{detail}'` |
| H013 / ARCHIVE | R068 | K38:155 | `len(file_order) > MAX_FILES` → `'生成配置超过打包文件数量限制'` |
| H014 / ARCHIVE | R068 | K38:172 | `ruleset_format(declared, config) == 'mrs'` → `'OxiDNS/MosDNS 不支持 MRS 二进制规则，请使用该订阅的 Text 或 YAML 版本。'` |
| H015 / ARCHIVE | R068 | K38:183 | `捕获 Exception` → `f'规则“{_label(sources[source][0][0])}”下载失败：{detail}'` |
| H016 / ARCHIVE | R068 | K38:213 | `捕获 Exception` → `f'规则“{_label(item)}”转换失败：{detail}'` |
| H017 / ARCHIVE | R068 | K38:228 | `not isinstance(document, dict) or not isinstance(document.get('plugins', []), list)` → `'MosDNS 配置格式无效'` |
| H018 / ARCHIVE | R068 | K38:235 | `not isinstance(references, list)` → `'MosDNS 文件引用必须为列表'` |
| H019 / ARCHIVE | R068 | K38:237 | `any((_path(path) not in files for path in references))` → `'MosDNS 配置引用了未打包的规则文件'` |

### K41 [backend/utils/oxidns_archive.py](../../backend/utils/oxidns_archive.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H020 / ARCHIVE | R070 | K41:16 | `any((path.startswith('state/') for path in paths))` → `'OxiDNS 运行数据 state/ 不能包含在配置包中'` |
| H021 / ARCHIVE | R070 | K41:19 | `not isinstance(document, dict)` → `'OxiDNS 配置格式无效'` |
| H022 / ARCHIVE | R070 | K41:21 | `document.get('include')` → `'OxiDNS 完整配置包不支持外部 include'` |
| H023 / ARCHIVE | R070 | K41:26 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H024 / ARCHIVE | R070 | K41:28 | `捕获 MosdnsArchiveError` → `str(exc).replace('MosDNS', 'OxiDNS')` |

### K01 [backend/agents/generated_preview.py](../../backend/agents/generated_preview.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H025 / ARTIFACT | R062 | K01:84 | `incoming > MAX_BYTES` → `'生成结果超过临时存储上限，请减少配套文件后重试。'` |
| H026 / ARTIFACT | R063 | K01:109 | `not isinstance(key, str) or not re.fullmatch('[a-f0-9]{32}', key)` → `'生成结果无效，请重新生成。'` |
| H027 / ARTIFACT | R063 | K01:114 | `record['agent_id'] != agent_id or record['expires_at'] <= time.time()` → `'stale'` |
| H028 / ARTIFACT | R063 | K01:118 | `hashlib.sha256(archive).hexdigest() != meta['sha256']` → `'digest'` |
| H029 / ARTIFACT | R063 | K01:123 | `捕获 (OSError, ValueError, KeyError, TypeError)` → `'生成结果已过期或不可用，请重新生成后推送。'` |

### K54 [backend/utils/webdav_backup.py](../../backend/utils/webdav_backup.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H030 / DAV | R016 | K54:74 | `not url or not username or (not password)` → `'请填写完整的 WebDAV 配置'` |
| H031 / DAV | R016 | K54:90 | `捕获 Exception` → `f'无法创建目录 {current}：{describe_webdav_error(error)}'` |

### K32 [backend/utils/dependencies.py](../../backend/utils/dependencies.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H032 / DEP | R200 | K32:47 | `not raw.isascii() or not raw.isdecimal() or len(raw) > 5 or (not 1 <= int(raw) <= 65535)` → `'SUB_STORE_BACKEND_API_PORT must be an integer between 1 and 65535'` |
| H033 / DEP | R200 | K32:158 | `not asset` → `f'最新 release 中没有 {SUB_STORE_ASSET}'` |
| H034 / DEP | R200 | K32:222 | `result.returncode != 0` → `(result.stdout + result.stderr).strip() or 'supervisorctl restart 失败'` |
| H035 / DEP | R200 | K32:249 | `size < _MIN_BUNDLE_SIZE` → `f'下载的文件过小（{size} 字节），可能不是有效的 Sub-Store 后端'` |
| H036 / DEP | R200 | K32:251 | `release.get('sha256') and sha.hexdigest() != release['sha256']` → `'文件校验失败（SHA256 不匹配），请检查 GitHub 代理设置'` |
| H037 / DEP | R200 | K32:254 | `check.returncode != 0` → `f'下载的文件不是有效的 JavaScript：{check.stderr.strip()[:200]}'` |
| H038 / DEP | R200 | K32:309 | `外部依赖/存储返回异常` → `f'Sub-Store {target} 启动后未就绪'` |
| H039 / DEP | R200 | K32:325 | `not restored or (previous_version and parse_version(restored) != parse_version(previous_version))` → `'原版本仍未就绪'` |
| H040 / DEP | R200 | K32:343 | `not release` → `error or '无法获取最新版本'` |
| H041 / DEP | R200 | K32:346 | `current and (not is_newer(release['version'], current))` → `f'当前已是最新版本 {current}'` |

### K34 [backend/utils/domain_discovery_service.py](../../backend/utils/domain_discovery_service.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H042 / DISCOVERY | R051 | K34:125 | `library_item is None` → `'missing'` |
| H043 / DISCOVERY | R051 | K34:127 | `library_item.get('source_type', 'url') != 'content'` → `'not_content'` |
| H044 / DISCOVERY | R051 | K34:129 | `library_item.get('behavior', 'classical') not in WRITABLE_BEHAVIORS` → `'unsupported_behavior'` |
| H045 / DISCOVERY | R058 | K34:191 | `not isinstance(payload, dict)` → `'请求必须是 JSON 对象'` |
| H046 / DISCOVERY | R058 | K34:201 | `value is not None and ruleset_problem(library.get(value)) is not None` → `'默认规则集必须是内容型、classical 或 domain 格式的规则库条目'` |
| H047 / DISCOVERY | R058 | K34:206 | `not isinstance(payload[key], bool)` → `f'{key} 必须是布尔值'` |
| H048 / DISCOVERY | R058 | K34:210 | `payload['auto_apply_min_confidence'] not in CONFIDENCE_CHOICES` → `'自动采纳阈值只能是 80、90 或 95'` |
| H049 / DISCOVERY | R058 | K34:214 | `not isinstance(payload['region_check_enabled'], bool)` → `'region_check_enabled 必须是布尔值'` |
| H050 / DISCOVERY | R058 | K34:219 | `not isinstance(port, int) or isinstance(port, bool) or (not 1024 <= port <= 65535)` → `'区域检测入口端口必须在 1024-65535 之间'` |
| H051 / DISCOVERY | R058 | K34:221 | `port in _custom_config_ports(config)` → `f'端口 {port} 已被 Mihomo 配置中的入站占用'` |
| H052 / DISCOVERY | R056 | K34:248 | `not isinstance(targets, list) or any((target not in TARGETS for target in targets))` → `'targets 只能包含 direct / proxy'` |
| H053 / DISCOVERY | R056 | K34:251 | `'proxy' in targets and proxy_policy not in policy_names(config)` → `'请选择代理规则集使用的策略组'` |
| H054 / DISCOVERY | R061 | K34:434 | `not isinstance(payload, dict)` → `'请求必须是 JSON 对象'` |
| H055 / DISCOVERY | R061 | K34:438 | `not domain or not isinstance(note, str) or len(note) > 200` → `'需要有效域名，备注最多 200 字'` |
| H056 / DISCOVERY | R061 | K34:444 | `domain not in notes and len(notes) >= 2000` → `'备注数量已达 2000 条上限'` |
| H057 / DISCOVERY | R060 | K34:471 | `len(values) > WEBSITE_BATCH_LIMIT` → `f'一次最多识别 {WEBSITE_BATCH_LIMIT} 个域名'` |
| H058 / DISCOVERY | R060 | K34:476 | `not agents` → `f'需要开启域名发现且版本不低于 {MIN_REGION_AGENT_VERSION} 的 Agent'` |
| H059 / DISCOVERY | R060 | K34:493 | `not plans` → `f'{missing[0]} 没有可用 Agent 最近采集的记录'` |
| H060 / DISCOVERY | R046 | K34:538 | `not isinstance(items, list) or not items or len(items) > MAX_APPLY_ITEMS` → `f'items 必须是 1-{MAX_APPLY_ITEMS} 条'` |
| H061 / DISCOVERY | R046 | K34:542 | `not isinstance(item, dict)` → `'items 中每一项必须是对象'` |
| H062 / DISCOVERY | R046 | K34:547 | `value is None or rule_type not in APPLY_RULE_TYPES or target not in TARGETS` → `'每一项需要有效域名、DOMAIN-SUFFIX/DOMAIN 类型和 direct/proxy 目标'` |
| H063 / DISCOVERY | R046 | K34:570 | `info is None or info['problem']` → `f'还没有设置可写入的{target_label(target)}规则集'` |
| H064 / DISCOVERY | R046 | K34:582 | `ruleset_problem(item) is not None` → `'默认规则集已被修改，请刷新后重试'` |
| H065 / DISCOVERY | R059 | K34:663 | `value is None or not isinstance(target, str) or (not (target in TARGETS or target.startswith(POLICY_TARGET_PREFIX)))` → `'需要有效域名和规则集目标'` |
| H066 / DISCOVERY | R050 | K34:695 | `not cleaned or None in cleaned` → `'values 必须是有效域名列表'` |
| H067 / DISCOVERY | R050 | K34:702 | `len(current) > MAX_IGNORED` → `f'忽略列表最多 {MAX_IGNORED} 条'` |
| H068 / DISCOVERY | R051 | K34:739 | `not agents` → `f'没有可用于探测的 Agent：需要开启域名发现且 Agent 版本不低于 {MIN_PROBE_AGENT_VERSION}'` |
| H069 / DISCOVERY | R051 | K34:770 | `not proxy_path` → `'请先设置默认代理规则集，并在当前配置中启用（探测会经它指向的策略组访问）'` |
| H070 / DISCOVERY | R051 | K34:850 | `not cleaned or None in cleaned or len(cleaned) > PROBE_MAX_VALUES` → `f'domains 必须是 1-{PROBE_MAX_VALUES} 个有效域名'` |
| H071 / DISCOVERY | R051 | K34:865 | `busy` → `busy['id']` |
| H072 / DISCOVERY | R051 | K34:905 | `job['status'] == 'failed'` → `job['message']` |
| H073 / DISCOVERY | R054 | K34:1120 | `not settings['region_check_enabled']` → `'请先开启区域检测'` |
| H074 / DISCOVERY | R054 | K34:1123 | `not agents` → `f'没有可用于区域检测的 Agent：需要开启域名发现且 Agent 版本不低于 {MIN_REGION_AGENT_VERSION}'` |
| H075 / DISCOVERY | R054 | K34:1129 | `捕获 Exception` → `str(error)` |
| H076 / DISCOVERY | R054 | K34:1192 | `len(values) > MAX_DOMAINS_PER_CHECK` → `f'一次最多检测 {MAX_DOMAINS_PER_CHECK} 个域名'` |
| H077 / DISCOVERY | R054 | K34:1194 | `not services and (not values)` → `'请选择要检测的服务或域名'` |
| H078 / DISCOVERY | R055 | K34:1242 | `policy not in policy_names(config)` → `'请选择当前配置中的策略组'` |
| H079 / DISCOVERY | R055 | K34:1297 | `service is None` → `'未知的服务'` |
| H080 / DISCOVERY | R055 | K34:1302 | `cleaned is None` → `'需要有效域名'` |
| H081 / DISCOVERY | R055 | K34:1305 | `not (kind == 'domain')` → `'kind 只能是 service 或 domain'` |

### K39 [backend/utils/node_dns.py](../../backend/utils/node_dns.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H082 / DNS | R099 | K39:28 | `not isinstance(value, str)` → `message` |
| H083 / DNS | R099 | K39:33 | `address.is_unspecified or address.is_multicast` → `ValueError` |
| H084 / DNS | R099 | K39:35 | `separator and (not port.isascii() or not port.isdecimal() or (not 1 <= int(port) <= 65535))` → `ValueError` |
| H085 / DNS | R099 | K39:37 | `捕获 ValueError` → `message` |
| H086 / DNS | R099 | K39:43 | `not isinstance(value, str) or not value.strip()` → `DNS_ERROR` |
| H087 / DNS | R099 | K39:49 | `len(value) > 2048 or any((c.isspace() or ord(c) < 32 or ord(c) == 127 for c in value)) or any((c in value for c in ('?', '#', '\\')))` → `ValueError` |
| H088 / DNS | R099 | K39:52 | `parsed.scheme not in ('https', 'tls') or not parsed.hostname or parsed.username is not None or (parsed.password is not None)` → `ValueError` |
| H089 / DNS | R099 | K39:58 | `len(host) > 253 or not all((re.fullmatch('[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in host.removesuffix('.').split('.')))` → `ValueError` |
| H090 / DNS | R099 | K39:61 | `address.version != 4 or address.is_unspecified or address.is_multicast` → `ValueError` |
| H091 / DNS | R099 | K39:63 | `re.search('%(?![0-9a-fA-F]{2})', parsed.path)` → `ValueError` |
| H092 / DNS | R099 | K39:66 | `port is not None and (not 1 <= port <= 65535) or parsed.netloc.endswith(':')` → `ValueError` |
| H093 / DNS | R099 | K39:68 | `parsed.scheme == 'tls' and parsed.path` → `ValueError` |
| H094 / DNS | R099 | K39:74 | `捕获 (ValueError, UnicodeError)` → `DNS_ERROR` |
| H095 / DNS | R099 | K39:80 | `not isinstance(value, list) or len(value) > 4` → `message` |
| H096 / DNS | R101 | K39:101 | `not isinstance(value, dict) or value.get('mode') not in ('system', 'custom')` → `'请选择系统 DNS 或自定义 DNS'` |
| H097 / DNS | R101 | K39:108 | `not result['servers']` → `'自定义 DNS 至少需要一个服务器地址'` |
| H098 / DNS | R101 | K39:110 | `any((_needs_bootstrap(server) for server in result['servers'])) and (not result['bootstrap'])` → `'域名形式的 DoH / DoT 需要指定引导 DNS（IPv4 或 IPv4:端口）'` |
| H099 / DNS | R099 | K39:164 | `捕获 OSError` → `'测速组件无法启动，请检查可执行权限和 CPU 架构'` |
| H100 / DNS | R099 | K39:166 | `process.returncode` → `'DNS 检测组件异常退出，请检查组件版本后重试'` |
| H101 / DNS | R099 | K39:170 | `response['version'] != 4` → `'测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe'` |
| H102 / DNS | R099 | K39:175 | `type(result.get('success')) is not bool or not isinstance(result.get('addresses'), list) or (not all((isinstance(address, str) for address in result['addresses'])))` → `ValueError` |
| H103 / DNS | R099 | K39:179 | `latency is not None and (type(latency) is not int or latency < 0)` → `ValueError` |
| H104 / DNS | R099 | K39:181 | `result['success'] and (not addresses or any((ipaddress.ip_address(address) in ipaddress.ip_network('198.18.0.0/15') for address in addresses)))` → `ValueError` |
| H105 / DNS | R099 | K39:189 | `捕获 (ValueError, TypeError, KeyError, AttributeError)` → `'DNS 检测组件返回了无效结果，请检查组件版本'` |

### K44 [backend/utils/remote_rule_sources.py](../../backend/utils/remote_rule_sources.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H106 / FETCH | R087 | K44:30 | `source is None` → `'规则来源已不存在，请重新生成并发布配置。'` |
| H107 / FETCH | R087 | K44:32 | `source.get('source_type', 'url') != 'url'` → `'规则来源已改为手写内容，请重新生成并发布配置。'` |
| H108 / FETCH | R087 | K44:34 | `not _same_url(source.get('url'), url, config)` → `'规则订阅地址已变更，请重新生成并发布配置。'` |
| H109 / FETCH | R087 | K44:44 | `len(declarations) != 1` → `'同一订阅地址对应多个不同规则类型或格式，无法确定当前来源；请重新生成并发布配置。'` |

### K45 [backend/utils/rule_fetch.py](../../backend/utils/rule_fetch.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H110 / FETCH | R087 | K45:33 | `not isinstance(url, str) or any((ord(c) < 32 or ord(c) == 127 for c in url))` → `'Invalid HTTP/HTTPS URL'` |
| H111 / FETCH | R087 | K45:38 | `parsed.scheme not in ('http', 'https') or not parsed.hostname` → `ValueError` |
| H112 / FETCH | R087 | K45:40 | `port is not None and (not 1 <= port <= 65535)` → `ValueError` |
| H113 / FETCH | R087 | K45:42 | `any((c.isspace() for c in parsed.netloc)) or '\\' in parsed.netloc` → `ValueError` |
| H114 / FETCH | R087 | K45:44 | `捕获 (ValueError, UnicodeError)` → `'Invalid HTTP/HTTPS URL'` |
| H115 / FETCH | R087 | K45:50 | `not isinstance(value, str)` → `'规则下载代理必须是 HTTP 或 HTTPS 地址'` |
| H116 / FETCH | R087 | K45:57 | `parsed.path not in ('', '/') or parsed.query or parsed.fragment` → `ValueError` |
| H117 / FETCH | R087 | K45:59 | `捕获 ValueError` → `'规则下载代理必须是 HTTP 或 HTTPS 地址，不能包含路径、查询参数或片段'` |
| H118 / FETCH | R087 | K45:151 | `method not in ('get', 'head')` → `'Rule requests only support GET and HEAD'` |
| H119 / FETCH | R087 | K45:179 | `not location` → `'Redirect without Location'` |
| H120 / FETCH | R087 | K45:184 | `internal and (_origin(parsed) != _origin(target_parts) or not is_internal_rule_url(target, config_data))` → `'Internal API redirect leaves its origin'` |
| H121 / FETCH | R087 | K45:186 | `hop == max_redirects` → `'Too many redirects'` |
| H122 / FETCH | R087 | K45:292 | `len(body) > max_bytes` → `'Resource exceeds deployment file size limit'` |
| H123 / FETCH | R087 | K45:346 | `index + 1 == len(candidates)` → `attempts` |

### K47 [backend/utils/ruleset_content.py](../../backend/utils/ruleset_content.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H124 / FETCH | R087 | K47:149 | `',' in value` → `'Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。'` |
| H125 / FETCH | R087 | K47:151 | `not _valid_pattern(value)` → `'请填写域名或通配符，例如 example.com、+.example.com。'` |
| H126 / FETCH | R087 | K47:172 | `'%' in stripped or ('/' in stripped and cidr_error(stripped))` → `ValueError()` |
| H127 / FETCH | R087 | K47:181 | `外部依赖/存储返回异常` → `'无法确定此行的 Classical 语义，已保留原文；请改为明确的规则类型和值。'` |
| H128 / FETCH | R087 | K47:232 | `not isinstance(root, yaml.MappingNode)` → `'YAML 规则需要 payload 列表。'` |
| H129 / FETCH | R087 | K47:235 | `len(pairs) != 1 or not isinstance(pairs[0][1], yaml.SequenceNode)` → `'YAML 规则需要唯一的 payload 列表。'` |
| H130 / FETCH | R087 | K47:239 | `any((isinstance(token, (yaml.tokens.AnchorToken, yaml.tokens.AliasToken)) for token in tokens))` → `'请先展开 YAML 锚点或别名；Mihomo 下发保留了原文。'` |
| H131 / FETCH | R087 | K47:243 | `any((not isinstance(node, yaml.ScalarNode) or node.tag != 'tag:yaml.org,2002:str' or node.style in ('&#124;', '>') or (node.start_mark.line != node.end_mark.line) or ('\n' in node.va…（完整条件见源行）` → `'payload 每项应为一行文本；复杂 YAML 内容已保留原文。'` |
| H132 / FETCH | R087 | K47:269 | `len(keys) != len(node.value) or len(set(keys)) != len(keys)` → `'YAML 映射键重复或过于复杂，已保留原文。'` |
| H133 / FETCH | R087 | K47:276 | `node.style in ('&#124;', '>') or node.start_mark.line != node.end_mark.line` → `'多行 YAML 内容已保留原文，请展开为普通 payload 列表。'` |
| H134 / FETCH | R087 | K47:343 | `not isinstance(content, str)` → `'规则内容必须是文本。'` |
| H135 / FETCH | R087 | K47:361 | `not isinstance(root, yaml.MappingNode)` → `'YAML 规则需要 payload 列表。'` |
| H136 / FETCH | R087 | K47:364 | `len(pairs) != 1 or not isinstance(pairs[0][1], yaml.SequenceNode)` → `'YAML 规则需要唯一的 payload 列表。'` |
| H137 / FETCH | R087 | K47:367 | `payload.start_mark.index < key.end_mark.index` → `'请将 payload 的 YAML 别名展开后再编辑。'` |
| H138 / FETCH | R087 | K47:379 | `not isinstance(node, yaml.ScalarNode) or node.tag != 'tag:yaml.org,2002:str' or node.start_mark.index < payload.start_mark.index or (id(node) in seen) or (node.style in ('&#124;', '>…（完整条件见源行）` → `'payload 每项应为一行文本；请先展开 YAML 锚点、别名或多行内容。'` |
| H139 / FETCH | R087 | K47:444 | `error` → `error` |
| H140 / FETCH | R087 | K47:450 | `checked['errors']` → `f"规则内容第 {error['line']} 行：{error['message']}" if error['line'] else error['message']` |
| H141 / FETCH | R087 | K47:544 | `外部依赖/存储返回异常` → `rule` |
| H142 / FETCH | R087 | K47:600 | `metadata_error` → `rule` |
| H143 / FETCH | R087 | K47:625 | `捕获 UnicodeDecodeError` → `rule` |
| H144 / FETCH | R087 | K47:630 | `not isinstance(content, str)` → `rule` |
| H145 / FETCH | R087 | K47:653 | `errors` → `rule` |
| H146 / FETCH | R087 | K47:663 | `not isinstance(document, dict) or not isinstance(document.get('payload'), list)` → `ValueError()` |
| H147 / FETCH | R087 | K47:666 | `payload.start_mark.index < key.end_mark.index` → `ValueError()` |
| H148 / FETCH | R087 | K47:668 | `not all((isinstance(line, str) for line in document['payload']))` → `ValueError()` |
| H149 / FETCH | R087 | K47:671 | `捕获 (yaml.YAMLError, ValueError, StopIteration, AttributeError)` → `'规则集的 payload 格式无法读取，请先在规则仓库检查内容。'` |

### K28 [backend/converters/mosdns.py](../../backend/converters/mosdns.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H150 / GEN-D | R069 | K28:198 | `not isinstance(rule_proxy_token, str) or not rule_proxy_token` → `'MosDNS rule proxy requires a rule proxy token'` |

### K29 [backend/converters/oxidns.py](../../backend/converters/oxidns.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H151 / GEN-D | R071 | K29:132 | `not url` → `'已选择的规则集缺少下载来源'` |
| H152 / GEN-D | R071 | K29:205 | `for (number, raw) in enumerate(text.splitlines(keepends=True), 1)` → `f'自定义 Hosts 第 {number} 行格式无效；请使用“域名规则 IPv4/IPv6 [更多 IP]”，或“IP 域名 [别名]”，不能混合地址与别名'` |
| H153 / GEN-D | R071 | K29:237 | `kind not in DNS_RULE_TYPES` → `f'所选规则类型暂不能转换为 OxiDNS 规则：{kind}'` |
| H154 / GEN-D | R071 | K29:240 | `not converted or converted.startswith('#')` → `'所选单条规则无法转换为 DNS 规则'` |
| H155 / GEN-D | R071 | K29:260 | `捕获 yaml.YAMLError` → `'DNS 上游 YAML 语法无效'` |
| H156 / GEN-D | R071 | K29:263 | `not values or any((not isinstance(item, (str, dict)) or (isinstance(item, dict) and (not item.get('addr'))) for item in values))` → `'DNS 上游 YAML 每项需要 addr 地址'` |
| H157 / GEN-D | R071 | K29:266 | `not result or any((not isinstance(item.get('addr'), str) or not item['addr'].strip() for item in result))` → `'DNS 上游配置无效'` |
| H158 / GEN-D | R071 | K29:278 | `捕获 yaml.YAMLError` → `'DNS 上游 YAML 语法无效'` |
| H159 / GEN-D | R071 | K29:295 | `捕获 yaml.YAMLError` → `'OxiDNS 高级 YAML 语法无效'` |
| H160 / GEN-D | R071 | K29:297 | `not isinstance(custom, dict)` → `'OxiDNS 高级 YAML 顶层必须为对象'` |
| H161 / GEN-D | R071 | K29:299 | `custom.get('include')` → `'完整部署模板不支持外部 include；请将插件内联到高级 YAML'` |
| H162 / GEN-D | R071 | K29:302 | `not isinstance(extra_plugins, list)` → `'高级 YAML 的 plugins 必须为数组'` |
| H163 / GEN-D | R071 | K29:305 | `result.get('runtime') is not None and (not isinstance(result['runtime'], dict))` → `'高级 YAML runtime 必须为对象'` |
| H164 / GEN-D | R071 | K29:445 | `s['rule_update']['enabled'] and custom_updates` → `'已有自定义下载或重载插件，请在高级 YAML 中配置规则定时更新，不能自动创建第二条维护链'` |
| H165 / GEN-D | R071 | K29:447 | `s['rule_update']['enabled'] and (not downloads)` → `'定时更新需要至少一个规则订阅'` |
| H166 / GEN-D | R071 | K29:459 | `any((issue['severity'] == 'error' for issue in issues))` → `issues` |
| H167 / GEN-D | R071 | K29:479 | `not isinstance(plugin, dict)` → `'插件必须为对象'` |
| H168 / GEN-D | R071 | K29:482 | `not isinstance(tag, str) or len(tag) > 64 or (not _TAG.fullmatch(tag)) or tag.startswith(('qs.exec.', 'qs.match.', 'qs.cron.'))` → `'OxiDNS 插件 tag 必须为有效的 1–64 位 ASCII 标识'` |
| H169 / GEN-D | R071 | K29:484 | `tag in tags` → `f'OxiDNS 插件 tag 重复：{tag}'` |

### K26 [backend/converters/loon.py](../../backend/converters/loon.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H170 / GEN-L | R065 | K26:481 | `any((proxy.get('dialer-proxy') is not None for proxy in proxies))` → `'Loon 订阅暂不支持 dialer-proxy，不能静默移除拨号配置'` |

### K27 [backend/converters/mihomo.py](../../backend/converters/mihomo.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H171 / GEN-M | R067 | K27:34 | `'ipv6' in document and type(document['ipv6']) is not bool` → `'ipv6 必须为 true 或 false，不能是字符串或拼写错误的值'` |
| H172 / GEN-M | R067 | K27:37 | `isinstance(dns, dict) and 'ipv6' in dns and (type(dns['ipv6']) is not bool)` → `'dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值'` |
| H173 / GEN-M | R067 | K27:384 | `捕获 yaml.YAMLError` → `f'mihomo.custom_config YAML 解析失败: {e}'` |
| H174 / GEN-M | R067 | K27:394 | `not isinstance(mihomo_config, dict)` → `f'mihomo.custom_config 不是 mapping，得到 {type(mihomo_config).__name__}'` |

### K30 [backend/converters/surge.py](../../backend/converters/surge.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H175 / GEN-S | R073 | K30:465 | `any((proxy.get('dialer-proxy') is not None for proxy in proxies))` → `'Surge policy-path 暂不支持 dialer-proxy，不能静默移除拨号配置'` |

### K33 [backend/utils/dialer_references.py](../../backend/utils/dialer_references.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H176 / GROUP | R157 | K33:14 | `not isinstance(items, list)` → `f'{key} 必须是数组'` |
| H177 / GROUP | R157 | K33:17 | `not isinstance(obj, dict)` → `'资源必须是 JSON 对象'` |
| H178 / GROUP | R157 | K33:24 | `not isinstance(value, str) or not value.strip()` → `f'资源 {field} 必须是非空字符串'` |
| H179 / GROUP | R157 | K33:27 | `not isinstance(obj.get('params', {}), dict)` → `'节点 params 必须是对象，不能为 null'` |
| H180 / GROUP | R157 | K33:29 | `obj.get('proxy_string') is not None and (not isinstance(obj['proxy_string'], str))` → `'节点 proxy_string 必须是字符串'` |
| H181 / GROUP | R157 | K33:34 | `field in obj and (not isinstance(obj[field], list) or any((not isinstance(v, str) or not v.strip() for v in obj[field])))` → `f'策略组 {field} 必须是字符串数组'` |
| H182 / GROUP | R157 | K33:39 | `not isinstance(order, list) or any((not isinstance(i, dict) or not isinstance(i.get('type'), str) or (not isinstance(i.get('id'), str)) for i in order))` → `'策略组 proxies_order 必须是含 type/id 字符串的对象数组'` |
| H183 / GROUP | R157 | K33:53 | `len(set(ids)) != len(ids)` → `'拨号引用要求资源 ID 唯一'` |
| H184 / GROUP | R157 | K33:58 | `any((not isinstance(name, str) or not name.strip() for name in names)) or len(set(names)) != len(names) or any((name in {'DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'PASS-RULE', …（完整条件见源行）` → `'拨号代理要求代理/策略组名称非空、唯一且不与内置策略冲突'` |
| H185 / GROUP | R157 | K33:66 | `not isinstance(ref, dict) or set(ref) != {'type', 'id'} or ref.get('type') not in ('node', 'group') or (not isinstance(ref.get('id'), str)) or (not ref['id'].strip())` → `'dialer_ref 必须为 {type: node&#124;group, id: 非空字符串}'` |
| H186 / GROUP | R157 | K33:70 | `target is None or not target.get('enabled', True)` → `'拨号引用不存在、未启用或不在当前配置空间'` |
| H187 / GROUP | R157 | K33:72 | `ref['type'] == 'node' and target.get('id') == node.get('id')` → `'节点不能引用自身作为拨号代理'` |
| H188 / GROUP | R157 | K33:94 | `obj is None` → `'拨号依赖不存在'` |
| H189 / GROUP | R157 | K33:96 | `key[0] == 'node' and obj.get('subscription_id')` → `'稳定拨号引用仅支持手动节点，不能使用订阅节点'` |
| H190 / GROUP | R157 | K33:98 | `not obj.get('enabled', True) and key not in roots` → `'拨号依赖不存在或未启用'` |
| H191 / GROUP | R157 | K33:130 | `group.get('include_all') or group.get('include-all')` → `'拨号拓扑不支持 include-all 隐式全局来源，请显式选择来源'` |
| H192 / GROUP | R157 | K33:143 | `len(matches) != 1` → `'策略组 use 引用不存在或名称不唯一'` |
| H193 / GROUP | R157 | K33:150 | `id not in catalog or not catalog[id].get('enabled', True)` → `'拨号策略组来源不存在或未启用'` |
| H194 / GROUP | R157 | K33:153 | `not children` → `'拨号策略组不能为空'` |
| H195 / GROUP | R157 | K33:175 | `not isinstance(name, str) or not name.strip()` → `'原始 dialer-proxy 必须是非空名称'` |
| H196 / GROUP | R157 | K33:182 | `len(matches) != 1` → `f"原始 dialer-proxy '{name}' 不存在、未启用或名称不唯一，请改用稳定引用"` |
| H197 / GROUP | R157 | K33:195 | `key[0] == 'builtin'` → `'代理链落地组不能包含 DIRECT/REJECT 等无法施加前置代理的候选'` |
| H198 / GROUP | R157 | K33:198 | `not obj or not obj.get('enabled', True)` → `'代理链落地候选不存在或未启用'` |
| H199 / GROUP | R157 | K33:201 | `obj.get('subscription_id')` → `'订阅节点请通过策略组 Provider 使用'` |
| H200 / GROUP | R157 | K33:211 | `not graph[key]` → `'代理链落地聚合不能为空'` |
| H201 / GROUP | R157 | K33:237 | `not obj or not obj.get('enabled', True)` → `'拨号依赖不存在或未启用'` |
| H202 / GROUP | R157 | K33:250 | `not children` → `'拨号策略组聚合来源不能为空'` |
| H203 / GROUP | R157 | K33:272 | `key in visiting` → `'拨号代理依赖存在循环'` |
| H204 / GROUP | R157 | K33:309 | `not proxy and ('node', node.get('id')) in dependency_graph(profile)` → `'拨号拓扑参与节点转换失败，不能省略节点'` |
| H205 / GROUP | R157 | K33:324 | `捕获 re.error` → `'Provider 正则过滤无效或不受支持'` |
| H206 / GROUP | R157 | K33:344 | `rules and name.startswith(rules[0]['target'].rsplit('_', 2)[0] + '_')` → `'Provider 原始节点名称与代理链私有排序标记冲突'` |
| H207 / GROUP | R157 | K33:351 | `捕获 (re.error, IndexError)` → `'Provider 名称转换规则无效'` |
| H208 / GROUP | R157 | K33:367 | `len(set(names)) != len(names) or any((not n or n in builtins for n in names))` → `'生成的代理/策略组名称重复或与内置策略冲突'` |
| H209 / GROUP | R157 | K33:374 | `require_providers` → `'拨号拓扑缺少已验证的 Provider 内容'` |
| H210 / GROUP | R157 | K33:379 | `len(set(raw_names)) != len(raw_names)` → `'Provider 节点名称重复'` |
| H211 / GROUP | R157 | K33:383 | `not proxies` → `'拨号拓扑 Provider 过滤后不能为空'` |
| H212 / GROUP | R157 | K33:390 | `emitted_name in builtins or (emitted_name in available and override.get('additional-prefix'))` → `'Provider 节点名称与代理拓扑冲突'` |
| H213 / GROUP | R157 | K33:393 | `target is not None and (not isinstance(target, str) or target not in available)` → `f'Provider 节点「{emitted_name}」的 dialer-proxy「{target}」引用了不存在的代理或策略组；若它是机场订阅自带的中转分组，在「订阅来源」刷新该订阅后 ConfigFlow 会自动补上'` |
| H214 / GROUP | R157 | K33:403 | `len(children) != len(set(children))` → `'策略组候选名称重复'` |
| H215 / GROUP | R157 | K33:405 | `any((n not in available for n in children))` → `'拨号拓扑中的策略组引用了未生成的代理或策略组'` |
| H216 / GROUP | R157 | K33:414 | `not isinstance(target, str) or target not in available` → `'生成的 dialer-proxy 引用了不存在的代理或策略组'` |
| H217 / GROUP | R157 | K33:418 | `provider not in definitions` → `'策略组引用了未生成的 Provider'` |
| H218 / GROUP | R157 | K33:423 | `candidate['name'] in candidate_names` → `'策略组候选名称重复'` |
| H219 / GROUP | R157 | K33:439 | `key in groups and (not graph[key]) and (require_providers or not groups[key].get('use'))` → `'拨号策略组过滤后没有可用候选'` |
| H220 / GROUP | R157 | K33:445 | `any((g.get('type') == 'chain' and g.get('enabled', True) for g in profile.get('proxy_groups', [])))` → `f'{client} 暂不支持代理链，请使用 Mihomo 或禁用代理链'` |
| H221 / GROUP | R157 | K33:451 | `node.get('dialer_ref') is not None or raw_dialer(node) is not None` → `f'{client} 暂不支持 Mihomo 拨号代理拓扑，请使用 Mihomo 或显式移除拨号配置'` |
| H222 / GROUP | R157 | K33:455 | `proxy and proxy.get('dialer-proxy') is not None` → `f'{client} 暂不支持转换结果中的 dialer-proxy'` |

### K35 [backend/utils/group_candidates.py](../../backend/utils/group_candidates.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H223 / GROUP | R157 | K35:25 | `value is not None and (not isinstance(value, str))` → `'正则筛选必须是字符串或 null'` |
| H224 / GROUP | R157 | K35:29 | `捕获 re.error` → `'正则表达式无效'` |
| H225 / GROUP | R157 | K35:121 | `original is None or field not in original or type(original[field]) is not type(group[field]) or (original[field] != group[field])` → `f'{field} 必须是字符串或 null'` |
| H226 / GROUP | R157 | K35:126 | `捕获 re.error` → `f'{field} 正则表达式无效'` |
| H227 / GROUP | R157 | K35:239 | `field in group and (not isinstance(group[field], list))` → `f'{field} 必须是数组'` |
| H228 / GROUP | R157 | K35:241 | `group.get('follow_group') is not None and (not isinstance(group.get('follow_group'), str))` → `'follow_group 必须是 ID 字符串'` |
| H229 / GROUP | R157 | K35:250 | `source is None or source.get('type') == 'chain' or source.get('follow_group')` → `'跟随来源必须是有效启用的普通来源组'` |
| H230 / GROUP | R157 | K35:256 | `not isinstance(values, list)` → `'候选来源必须是 ID 数组'` |
| H231 / GROUP | R157 | K35:261 | `any((not isinstance(value, str) or value not in available for value in values))` → `'候选来源必须引用有效启用资源'` |
| H232 / GROUP | R157 | K35:264 | `not isinstance(order, list)` → `'proxies_order 必须是数组'` |
| H233 / GROUP | R157 | K35:271 | `not isinstance(item, dict) or set(item) != {'type', 'id'}` → `'排序项必须包含且仅包含 type 和 id'` |
| H234 / GROUP | R157 | K35:274 | `not isinstance(kind, str) or not isinstance(ident, str) or kind not in allowed or (ident not in allowed[kind])` → `'排序项必须与已选择的受支持来源一致'` |
| H235 / GROUP | R157 | K35:276 | `(kind, ident) in seen` → `'排序项不能重复'` |
| H236 / GROUP | R157 | K35:280 | `not required <= seen` → `'排序必须包含所有手动节点和引用策略组'` |
| H237 / GROUP | R157 | K35:288 | `not emitted.get('proxies') and (not emitted.get('use'))` → `'策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT'` |
| H238 / GROUP | R157 | K35:296 | `not isinstance(deletes, list) or any((not isinstance(key, str) or key not in HEALTH_FIELDS for key in deletes)) or len(set(deletes)) != len(deletes)` → `'delete_fields 必须是无重复的健康检查字段数组'` |
| H239 / GROUP | R157 | K35:298 | `any((key in patch for key in deletes))` → `'同一字段不能同时赋值和删除'` |

### K36 [backend/utils/health_check.py](../../backend/utils/health_check.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H240 / GROUP | R157 | K36:44 | `not isinstance(values, dict)` → `'健康检查必须是对象'` |
| H241 / GROUP | R157 | K36:47 | `provider and set(values) - allowed` → `'健康检查包含未知字段'` |
| H242 / GROUP | R157 | K36:52 | `type(value) is not bool` → `f'{key} 必须为布尔值'` |
| H243 / GROUP | R157 | K36:57 | `type(value) is not int or not minimum <= value <= maximum` → `f'{key} 必须为 {minimum}–{maximum} 的整数'` |
| H244 / GROUP | R157 | K36:60 | `not isinstance(value, str) or any((c.isspace() or ord(c) < 32 for c in value))` → `'探测 URL 必须是有效 HTTP(S) 地址'` |
| H245 / GROUP | R157 | K36:70 | `not valid` → `'探测 URL 必须是无凭据的 HTTP(S) 地址'` |
| H246 / GROUP | R157 | K36:73 | `not isinstance(value, str) or not re.fullmatch('[0-9]{3}(?:-[0-9]{3})?(?:/[0-9]{3}(?:-[0-9]{3})?)*', value)` → `'expected-status 格式应为 204 或 200-299/304'` |
| H247 / GROUP | R157 | K36:77 | `not all((100 <= n <= 599 for n in bounds)) or bounds[0] > bounds[-1]` → `'expected-status 范围必须在 100–599 内且递增'` |

### K37 [backend/utils/mihomo_listeners.py](../../backend/utils/mihomo_listeners.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H248 / GROUP | R157 | K37:21 | `not isinstance(listeners, list)` → `'Mihomo listeners 必须是列表，无法确认入口的固定策略引用'` |
| H249 / GROUP | R157 | K37:24 | `not isinstance(listener, dict)` → `f'Mihomo listeners[{index}] 必须是对象'` |
| H250 / GROUP | R157 | K37:27 | `target is not None and (not isinstance(target, str))` → `f'Mihomo listeners[{index}].proxy 必须是策略名称或留空'` |
| H251 / GROUP | R157 | K37:44 | `捕获 yaml.YAMLError` → `'Mihomo 基础配置无法解析，未修改策略组或入口引用'` |
| H252 / GROUP | R157 | K37:46 | `not isinstance(document, dict)` → `'Mihomo 基础配置必须是对象，未修改策略组或入口引用'` |
| H253 / GROUP | R157 | K37:53 | `isinstance(target, str) and '${' in target` → `f'Mihomo listeners[{index}].proxy 含未解析变量，无法安全联动此策略组；原配置已保留'` |
| H254 / GROUP | R157 | K37:72 | `捕获 (CommentedYamlError, ValueError, TypeError, RecursionError)` → `'无法安全保留 Mihomo 基础配置的注释与引用，原配置已保留'` |
| H255 / GROUP | R157 | K37:86 | `target and target not in available` → `f'Mihomo listeners[{index}].proxy 引用了不存在或未启用的策略「{target}」'` |

### K43 [backend/utils/proxy_chains.py](../../backend/utils/proxy_chains.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H256 / GROUP | R157 | K43:14 | `group.get('follow_group') in chains` → `'跟随模式不能引用代理链，请使用策略组引用'` |
| H257 / GROUP | R157 | K43:18 | `kind == 'group' and (id not in groups or not groups[id].get('enabled', True))` → `'策略组引用不存在或未启用'` |
| H258 / GROUP | R157 | K43:22 | `not isinstance(chain, dict) or set(chain) != {'entry', 'exit'}` → `'代理链 chain 必须包含 entry 和 exit'` |
| H259 / GROUP | R157 | K43:28 | `not isinstance(ref, dict) or set(ref) != {'type', 'id'} or ref.get('type') not in ('node', 'group') or (not isinstance(ref.get('id'), str)) or (not ref['id'].strip())` → `'代理链端点必须为 {type: node&#124;group, id: 非空字符串}'` |
| H260 / GROUP | R157 | K43:32 | `not target or ((endpoint == 'entry' or group.get('enabled', True)) and (not target.get('enabled', True)))` → `'代理链依赖不存在或未启用'` |
| H261 / GROUP | R157 | K43:34 | `ref['type'] == 'node' and target.get('subscription_id')` → `'代理链仅支持共享手动节点，订阅请通过策略组使用'` |
| H262 / GROUP | R157 | K43:36 | `chain['entry']['type'] == 'node' and chain['entry'] == chain['exit']` → `'代理链前置与落地不能是同一资源'` |
| H263 / GROUP | R157 | K43:39 | `any((group.get(k) for k in ('manual_nodes', 'include_groups', 'proxies_order', 'proxies', 'subscriptions', 'aggregations', 'use', 'follow_group', 'include_all', 'include-all')))` → `'代理链不能同时配置普通策略组来源'` |
| H264 / GROUP | R157 | K43:56 | `len(provider_names) != len(set(provider_names))` → `'订阅与聚合 Provider 名称必须唯一'` |
| H265 / GROUP | R157 | K43:62 | `original['id'] in seen or original['follow_group'] not in groups` → `'跟随策略组依赖不存在或存在循环'` |
| H266 / GROUP | R157 | K43:126 | `original['id'] in seen` → `'跟随策略组依赖存在循环'` |
| H267 / GROUP | R157 | K43:139 | `kind == 'builtin'` → `'代理链落地组不能包含 DIRECT/REJECT 等非代理候选'` |

### K31 [backend/utils/config_importer.py](../../backend/utils/config_importer.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H268 / IMPORT | R155 | K31:168 | `port is None or not 0 < port < 65536` → `f'端口无效：{value}'` |
| H269 / IMPORT | R155 | K31:201 | `not sep` → `'缺少 “=”'` |
| H270 / IMPORT | R155 | K31:270 | `捕获 yaml.YAMLError` → `f'YAML 解析失败：{error}'` |
| H271 / IMPORT | R155 | K31:272 | `not isinstance(data, dict)` → `'Mihomo 配置必须是 YAML 对象'` |
| H272 / IMPORT | R155 | K31:399 | `len(positional) < 2` → `'缺少服务器或端口'` |
| H273 / IMPORT | R155 | K31:492 | `not (proxy_type == 'anytls')` → `f'不支持的节点类型 {proxy_type}'` |
| H274 / IMPORT | R155 | K31:500 | `lines is None` → `f'未找到 [WireGuard {section}]'` |
| H275 / IMPORT | R155 | K31:553 | `transport not in ('', 'tcp')` → `f'不支持的传输方式 {transport}'` |
| H276 / IMPORT | R155 | K31:577 | `len(positional) < 2` → `'缺少服务器或端口'` |
| H277 / IMPORT | R155 | K31:648 | `not (proxy_type in ('http', 'https', 'socks5'))` → `f'不支持的节点类型 {proxy_type}'` |
| H278 / IMPORT | R155 | K31:756 | `not sections` → `'未找到任何 [Section]，请确认配置类型是否正确'` |
| H279 / IMPORT | R155 | K31:812 | `not sections` → `'未找到任何 [Section]，请确认配置类型是否正确'` |
| H280 / IMPORT | R155 | K31:888 | `client_type not in CLIENT_TYPES` → `f'不支持的配置类型：{client_type}'` |
| H281 / IMPORT | R155 | K31:890 | `not isinstance(content, str) or not content.strip()` → `'配置内容不能为空'` |
| H282 / IMPORT | R155 | K31:900 | `not plan['proxies'] and (not plan['providers']) and (not plan['groups']) and (not plan['rules'])` → `'未从配置中解析到节点、策略组或规则，请确认配置类型是否正确'` |

### K07 [backend/common/mihomo_base_settings.py](../../backend/common/mihomo_base_settings.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H283 / MBASE | R038 | K07:142 | `changed(path) and value is not _MISSING and (type(value) is not bool)` → `f'{label} 只能填写 true 或 false，不要加引号'` |
| H284 / MBASE | R038 | K07:146 | `changed(path) and value is not _MISSING and (type(value) is not int or not low <= value <= high)` → `f'{label} 需为 {low}–{high} 的整数'` |
| H285 / MBASE | R038 | K07:149 | `changed(('global-client-fingerprint',)) and _get(document, ('global-client-fingerprint',)) is not _MISSING` → `REMOVED_FINGERPRINT` |
| H286 / MBASE | R038 | K07:157 | `not isinstance(value, str) or value.strip().lower() not in options` → `f"{label} 只能是 {' / '.join(options)} 之一"` |
| H287 / MBASE | R038 | K07:162 | `changed(path) and value is not _MISSING and (value is not None) and (not isinstance(value, list) or not all((isinstance(item, str) and item.strip() for item in value)))` → `f'{label} 需为文本列表，每行一项，不能有空项'` |
| H288 / MBASE | R038 | K07:166 | `changed(path) and value is not _MISSING and (not isinstance(value, str) or not value.strip())` → `f'{label} 需为非空文本；不需要时删除该项'` |
| H289 / MBASE | R038 | K07:189 | `not isinstance(value, str)` → `''` |
| H290 / MBASE | R038 | K07:192 | `捕获 LinuxGatewayError` → `'dns.fake-ip-range 需为 IPv4 网段（/8 至 /30），例如 192.0.2.10/16，不能是回环、多播或链路本地地址'` |
| H291 / MBASE | R038 | K07:203 | `changed(('sniffer', 'sniff'))` → `'sniffer.sniff 需为对象，按协议 HTTP / TLS / QUIC 分别填写'` |
| H292 / MBASE | R038 | K07:208 | `changed(('sniffer', 'sniff'))` → `f'sniffer.sniff 只支持 HTTP / TLS / QUIC，「{key}」无法识别，Mihomo 会拒绝加载'` |
| H293 / MBASE | R038 | K07:214 | `not isinstance(ports, list) or not ports or any((sniff_port_error(port) for port in ports))` → `f'sniffer.sniff.{key}.ports：{SNIFF_PORTS_HINT}'` |
| H294 / MBASE | R038 | K07:217 | `override is not _MISSING and type(override) is not bool` → `f'sniffer.sniff.{key}.override-destination 只能填写 true 或 false，不要加引号'` |
| H295 / MBASE | R038 | K07:227 | `changed(('external-controller',)) and parsed is None` → `'external-controller 需写成「地址:端口」，例如 0.0.0.0:9090 或 127.0.0.1:9090，端口 1–65535'` |
| H296 / MBASE | R038 | K07:230 | `changed(('external-controller',)) and scope == 'other'` → `'external-controller 只能监听 0.0.0.0 / ::（局域网可访问）或 127.0.0.1 / ::1（仅本机）：Agent 经本机回环地址读取连接、流量并热重载配置，监听其他地址时这些功能不可用'` |
| H297 / MBASE | R038 | K07:236 | `changed(('secret',))` → `'控制面板密码（secret）必须是文本，纯数字等请加引号'` |
| H298 / MBASE | R038 | K07:242 | `secret is _MISSING or secret in (None, '')` → `'控制面板对局域网开放（external-controller 监听所有地址）时必须设置控制面板密码（secret）'` |
| H299 / MBASE | R038 | K07:245 | `error` → `error` |
| H300 / MBASE | R038 | K07:249 | `error` → `error` |
| H301 / MBASE | R038 | K07:261 | `not valid` → `'external-ui-url 需为 http(s) 下载地址，例如 https://example.com/source'` |
| H302 / MBASE | R038 | K07:270 | `not valid` → `'bind-address 只能填写 *（所有地址）或本机的一个 IP 地址'` |
| H303 / MBASE | R038 | K07:280 | `port in seen` → `f'端口 {port} 同时被 {seen[port]} 和 {owner} 使用，请改用不同端口'` |
| H304 / MBASE | R038 | K07:283 | `port in reserved` → `f'{owner} 使用端口 {port}，与{reserved[port]}冲突，请改用其他端口'` |
| H305 / MBASE | R038 | K07:294 | `loop` → `f'dns.{loop[0]} 中的 {loop[1]} 指向本机或系统 DNS，会形成解析循环；请改用外部 DNS，例如 https://192.0.2.10/dns-query'` |
| H306 / MBASE | R038 | K07:297 | `changed(('dns', 'nameserver')) and dns.get('enable') is True and (not dns.get('nameserver'))` → `'dns.nameserver 至少填写一个上游'` |
| H307 / MBASE | R038 | K07:300 | `(changed(('dns', 'respect-rules')) or changed(('dns', 'proxy-server-nameserver'))) and dns.get('respect-rules') is True and (not dns.get('proxy-server-nameserver'))` → `'开启 dns.respect-rules 时必须填写 dns.proxy-server-nameserver，否则 Mihomo 无法启动'` |

### K08 [backend/common/mihomo_easytier.py](../../backend/common/mihomo_easytier.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H308 / MBASE | R038 | K08:90 | `not isinstance(value, str)` → `f'{label} 必须是文本'` |
| H309 / MBASE | R038 | K08:93 | `required and (not value)` → `f'请填写{label}'` |
| H310 / MBASE | R038 | K08:95 | `len(value) > limit or any((ord(c) < 32 or ord(c) == 127 for c in value))` → `f'{label} 过长或包含控制字符'` |
| H311 / MBASE | R038 | K08:103 | `not isinstance(value, list) or len(value) > MAX_LIST` → `f'{label} 必须是列表，最多 {MAX_LIST} 项'` |
| H312 / MBASE | R038 | K08:110 | `'->' in value` → `f'{label}「{value}」：Mihomo 的 EasyTier 不支持「->」网段映射，只能原样填写网段'` |
| H313 / MBASE | R038 | K08:115 | `捕获 ValueError` → `f'{label}「{value}」不是有效的网段'` |
| H314 / MBASE | R038 | K08:117 | `network.version != 4` → `f'{label}「{value}」：EasyTier 虚拟网只支持 IPv4'` |
| H315 / MBASE | R038 | K08:124 | `not sep or scheme.lower() not in _PEER_SCHEMES or (not rest)` → `f"{label}「{value}」格式不对，应类似 tcp://203.0.113.10:11010（支持 {', '.join(_PEER_SCHEMES[:7])} 等）"` |
| H316 / MBASE | R038 | K08:133 | `type(value) is not bool` → `f'{label} 必须是开或关'` |
| H317 / MBASE | R038 | K08:137 | `type(value) is not int` → `f'{label} 必须是整数'` |
| H318 / MBASE | R038 | K08:140 | `not low <= value <= high` → `f'{label} 需在 {low} ~ {high} 之间'` |
| H319 / MBASE | R038 | K08:152 | `ipaddress.ip_address(item).version != 4` → `ValueError` |
| H320 / MBASE | R038 | K08:154 | `捕获 ValueError` → `f'exit-nodes「{item}」应填写出口节点的虚拟 IPv4 地址'` |
| H321 / MBASE | R038 | K08:158 | `value not in IP_VERSIONS` → `f"ip-version 只能是 {' / '.join(IP_VERSIONS)}"` |
| H322 / MBASE | R038 | K08:165 | `捕获 ValueError` → `f'ipv4「{text}」应类似 192.0.2.10 或 192.0.2.10/24'` |
| H323 / MBASE | R038 | K08:167 | `interface.version != 4` → `'ipv4 只能填写 IPv4 地址'` |
| H324 / MBASE | R038 | K08:169 | `key == 'hostname' and text and (not _HOSTNAME.match(text))` → `'hostname 只能包含字母、数字和连字符，不超过 63 个字符'` |
| H325 / MBASE | R038 | K08:178 | `not isinstance(value, dict)` → `'EasyTier 设置必须是对象'` |
| H326 / MBASE | R038 | K08:182 | `type(enabled) is not bool` → `'enabled 必须是布尔值'` |
| H327 / MBASE | R038 | K08:186 | `',' in name` → `'节点名称不能包含英文逗号，否则规则无法引用'` |
| H328 / MBASE | R038 | K08:190 | `apply_to not in APPLY_TO` → `'下发范围只能是 subscription、agent 或 all'` |
| H329 / MBASE | R038 | K08:195 | `type(magic_dns) is not bool` → `'magic_dns 必须是布尔值'` |
| H330 / MBASE | R038 | K08:200 | `not isinstance(keepalive, dict)` → `'keepalive 必须是对象'` |
| H331 / MBASE | R038 | K08:203 | `type(keep_enabled) is not bool` → `'keepalive.enabled 必须是布尔值'` |
| H332 / MBASE | R038 | K08:207 | `type(interval) is not int or not 10 <= interval <= 86400` → `'保活检测间隔需为 10 ~ 86400 秒的整数'` |
| H333 / MBASE | R038 | K08:209 | `url and (not re.match('^https?://\\S+$', url))` → `'保活检测地址应为 http:// 或 https:// 开头的网址'` |
| H334 / MBASE | R038 | K08:211 | `keep_enabled and (not url)` → `'开启保活时请填写检测地址，例如虚拟网内某台设备上的网页'` |
| H335 / MBASE | R038 | K08:216 | `not isinstance(options, dict)` → `'options 必须是对象'` |
| H336 / MBASE | R038 | K08:219 | `unknown` → `f"不认识的 EasyTier 参数：{', '.join(map(str, unknown))}"` |
| H337 / MBASE | R038 | K08:232 | `not cleaned.get('network-name')` → `'请填写 network-name（EasyTier 网络名）'` |
| H338 / MBASE | R038 | K08:234 | `cleaned.get('listeners') and cleaned.get('no-listener') is True` → `'填写了 listeners 时不能同时开启 no-listener'` |
| H339 / MBASE | R038 | K08:237 | `not cleaned.get('peers') and (not listening)` → `'不监听端口时至少要填写一个 peers（入网节点）'` |
| H340 / MBASE | R038 | K08:239 | `bool(cleaned.get('local-public-key')) and (not cleaned.get('local-private-key'))` → `'local-public-key 需要和 local-private-key 一起填写'` |
| H341 / MBASE | R038 | K08:281 | `parsed is not None and parsed < MIN_CORE_VERSION` → `f'设备上的 Mihomo {core_version} 不支持 EasyTier 节点（需要 v1.19.31 或更新），请先更新内核，或在 EasyTier 设置里不再下发到 Agent'` |
| H342 / MBASE | R038 | K08:295 | `clash` → `f'EasyTier 节点名「{clash[0]}」与已有节点或策略组重名，请在 EasyTier 设置里改名'` |
| H343 / MBASE | R038 | K08:298 | `dialer and dialer not in taken and (dialer not in ('DIRECT',))` → `f'EasyTier 的 dialer-proxy「{dialer}」不在生成的节点或策略组里'` |

### K09 [backend/common/node_deletion.py](../../backend/common/node_deletion.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H344 / NODE | R095 | K09:18 | `not isinstance(node_ids, list) or not node_ids or any((not isinstance(value, str) or not value.strip() for value in node_ids))` → `'请选择要删除的节点'` |
| H345 / NODE | R095 | K09:23 | `len(nodes) != len(ids)` → `'部分节点已不存在，请刷新节点列表'` |

### K05 [backend/common/device_notes.py](../../backend/common/device_notes.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H346 / OX-COMMON | R107 | K05:20 | `not isinstance(value, str) or len(value) > 80 or any((ord(c) < 32 or ord(c) == 127 for c in value))` → `'设备备注最多 80 个字符，不能包含换行或控制字符。'` |
| H347 / OX-COMMON | R107 | K05:26 | `not isinstance(value, dict) or len(value) > MAX_NOTES` → `f'设备备注必须为对象，最多 {MAX_NOTES} 条。'` |
| H348 / OX-COMMON | R107 | K05:31 | `ip in result` → `f'设备备注地址重复：{ip}'` |

### K10 [backend/common/oxidns_common.py](../../backend/common/oxidns_common.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H349 / OX-COMMON | R107 | K10:96 | `not isinstance(child, dict)` → `'此参数包含自定义值类型，请在完整参数中编辑'` |
| H350 / OX-COMMON | R107 | K10:168 | `not isinstance(self.plugins, list) or any((not isinstance(item, dict) or not isinstance(item.get('tag'), str) for item in self.plugins))` → `'插件结构尚未完成，请在完整参数或 YAML 中继续编辑'` |
| H351 / OX-COMMON | R107 | K10:228 | `plugin.get('type') != kind` → `f'插件「{tag}」已使用其他类型'` |
| H352 / OX-COMMON | R107 | K10:418 | `not capability['editable']` → `_FALLBACK_REASON` |
| H353 / OX-COMMON | R107 | K10:425 | `捕获 yaml.YAMLError` → `'DNS 上游 YAML 语法无效'` |
| H354 / OX-COMMON | R107 | K10:428 | `not isinstance(upstreams, list) or any((not isinstance(item, dict) for item in upstreams))` → `'DNS 上游必须为参数对象数组'` |
| H355 / OX-COMMON | R107 | K10:446 | `enabled and (not isinstance(upstreams, list) or not upstreams or any((not isinstance(item, dict) or not isinstance(item.get('addr'), str) or (not item['addr'].strip()) for item …（完整条件见源行）` → `'启用代理回退前请先填写备用 DNS 地址'` |
| H356 / OX-COMMON | R107 | K10:726 | `not isinstance(value, dict)` → `f'{field} 必须是 TLS 参数对象或 null'` |
| H357 / OX-COMMON | R107 | K10:729 | `type(value) is not bool` → `f'{field} 必须为布尔值'` |
| H358 / OX-COMMON | R107 | K10:732 | `type(value) is not int` → `f'{field} 必须为整数'` |
| H359 / OX-COMMON | R107 | K10:735 | `not isinstance(value, list)` → `f'{field} 必须为数组'` |
| H360 / OX-COMMON | R107 | K10:737 | `field == 'block_qtypes' and any((type(item) is not int for item in value))` → `'屏蔽查询类型必须为整数数组'` |
| H361 / OX-COMMON | R107 | K10:740 | `not isinstance(value, str)` → `f'{field} 必须为文本'` |
| H362 / OX-COMMON | R107 | K10:878 | `next_route not in ('direct', 'proxy')` → `'动态域名路由必须为 direct 或 proxy'` |
| H363 / OX-COMMON | R107 | K10:899 | `chain is None` → `'缺少动态学习的目标执行链，请在完整参数中配置'` |
| H364 / OX-COMMON | R107 | K10:940 | `not downloads` → `'规则定时更新需要当前配置引用至少一个可下载订阅'` |
| H365 / OX-COMMON | R107 | K10:944 | `not isinstance(downloader.get('args'), dict)` → `'下载插件使用自定义参数，请在完整参数中编辑'` |
| H366 / OX-COMMON | R107 | K10:1018 | `any((not isinstance(url, str) or not re.match('^https?://', url) for url in urls))` → `'AdGuard 订阅必须为 HTTP(S) URL'` |
| H367 / OX-COMMON | R107 | K10:1034 | `changes['adguard.mode'] not in ('nxdomain', 'nodata', 'null', 'refused')` → `'AdGuard 响应模式无效'` |
| H368 / OX-COMMON | R107 | K10:1059 | `downloader.get('type') != 'download' or not isinstance(downloader.get('args'), dict) or (not isinstance(downloader['args'].get('downloads'), list)) or (reload and (reload.get('t…（完整条件见源行）` → `'规则更新使用自定义下载/重载结构，请在完整参数中调整'` |
| H369 / OX-COMMON | R107 | K10:1079 | `not isinstance(changes, dict) or not changes` → `'changes 必须为非空参数对象'` |
| H370 / OX-COMMON | R107 | K10:1083 | `unknown` → `'不支持的常用参数：' + ', '.join(sorted(unknown))` |
| H371 / OX-COMMON | R107 | K10:1091 | `not capability.get('editable') and (not enabling_api)` → `f'{field}：' + capability.get('reason', '请在完整参数中编辑')` |
| H372 / OX-COMMON | R107 | K10:1129 | `捕获 yaml.YAMLError` → `'DNS 上游 YAML 语法无效'` |
| H373 / OX-COMMON | R107 | K10:1133 | `not isinstance(upstreams, list) or any((not isinstance(item, dict) for item in upstreams))` → `'DNS 上游必须为参数对象数组'` |

### K11 [backend/common/oxidns_common_routing.py](../../backend/common/oxidns_common_routing.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H374 / OX-COMMON | R107 | K11:571 | `result is None or _uses_rule(load_native_yaml(result['native_config']), config, identity)` → `'OxiDNS 还在用这条规则，而且那部分配置是手动写的，没法自动移除。请先在 OxiDNS 设置的 YAML 中删掉对它的引用，再删除。'` |
| H375 / OX-COMMON | R107 | K11:698 | `not isinstance(url, str) or not url` → `'已选择的规则集缺少下载来源。'` |
| H376 / OX-COMMON | R107 | K11:785 | `existing is not None and (not _same(existing, plugin)) and (not replaceable(tag))` → `f'已有同名插件 {tag}，无法创建 DNS 处理所需插件，请先在完整参数中改名'` |
| H377 / OX-COMMON | R107 | K11:851 | `download is None or download.get('type') != 'download' or (not isinstance(download.get('args'), dict)) or (not isinstance(download['args'].get('downloads'), list)) or (not reloa…（完整条件见源行）` → `'规则定时更新包含自定义结构，请先在完整参数中调整下载与重载任务。'` |
| H378 / OX-COMMON | R107 | K11:910 | `not isinstance(values[key], list) or any((not isinstance(value, str) for value in values[key]))` → `f'{key} 必须为规则 ID 列表'` |
| H379 / OX-COMMON | R107 | K11:915 | `key in changes and any((identity not in known or _suffix(known[identity]) != suffix for identity in values[key]))` → `_MISSING_RULE` |
| H380 / OX-COMMON | R107 | K11:919 | `not isinstance(actions, dict) or any((not isinstance(identity, str) for identity in actions))` → `'rule_actions 必须为以规则 ID 为键的对象'` |
| H381 / OX-COMMON | R107 | K11:921 | `any((identity not in known for identity in actions))` → `_MISSING_RULE` |
| H382 / OX-COMMON | R107 | K11:927 | `not isinstance(follow, list) or any((not isinstance(identity, str) for identity in follow))` → `'rule_dns_follow 必须为规则 ID 数组'` |
| H383 / OX-COMMON | R107 | K11:929 | `any((identity not in known for identity in follow))` → `_MISSING_RULE` |
| H384 / OX-COMMON | R107 | K11:933 | `set(values['direct_' + suffix]) & set(values['proxy_' + suffix])` → `'同一规则不能同时选择直连和代理。'` |
| H385 / OX-COMMON | R107 | K11:947 | `key in changes` → `'同一规则不能同时选择多种 DNS 处理。'` |
| H386 / OX-COMMON | R107 | K11:955 | `'rule_actions' in changes` → `'同一规则不能同时选择多种 DNS 处理。'` |
| H387 / OX-COMMON | R107 | K11:1007 | `identity in values['rule_actions']` → `f'规则 {identity} 已设置 DNS 处理「{label}」，不能同时跟随策略'` |
| H388 / OX-COMMON | R107 | K11:1009 | `identity in parked` → `f'规则 {identity} 已暂存停用前的 DNS 处理，不能同时跟随策略'` |
| H389 / OX-COMMON | R107 | K11:1015 | `not isinstance(value, list)` → `'自定义匹配必须为列表。'` |
| H390 / OX-COMMON | R107 | K11:1019 | `not isinstance(entry, dict)` → `'每条自定义匹配必须为对象。'` |
| H391 / OX-COMMON | R107 | K11:1021 | `entry.get('enabled') is False` → `'常用设置直接编辑执行中的匹配条目，不支持 enabled: false；请移除该条目或改为启用。'` |
| H392 / OX-COMMON | R107 | K11:1025 | `not isinstance(match, list) or not match or any((not isinstance(item, str) or not item.strip() for item in match)) or (not isinstance(entry.get('exec'), str)) or (not entry['exe…（完整条件见源行）` → `'自定义匹配需填写非空条件与执行动作。'` |
| H393 / OX-COMMON | R107 | K11:1033 | `not state['capabilities'][field]['editable']` → `state['capabilities'][field]['reason']` |
| H394 / OX-COMMON | R107 | K11:1036 | `position not in ('head', 'tail')` → `'自定义匹配位置必须为 head 或 tail。'` |
| H395 / OX-COMMON | R107 | K11:1071 | `aliased is not None` → `'执行链已重命名，请单独调整自定义匹配，再编辑其他分流设置。'` |
| H396 / OX-COMMON | R107 | K11:1076 | `not cap['editable']` → `cap['reason']` |
| H397 / OX-COMMON | R107 | K11:1078 | `rebuild and (not current['capabilities']['direct_rules']['editable'])` → `current['capabilities']['direct_rules']['reason']` |
| H398 / OX-COMMON | R107 | K11:1081 | `values['default_forward'] not in ('forward_local', 'forward_remote')` → `'默认转发目标无效。'` |
| H399 / OX-COMMON | R107 | K11:1090 | `_plugins(result).get(target, {}).get('type') != 'sequence'` → `'目标分流执行链未配置，请在完整参数中添加后再切换。'` |
| H400 / OX-COMMON | R107 | K11:1102 | `_plugins(result).get(helper, {}).get('type') != 'sequence'` → `f'分流执行链 {helper} 未配置，请在完整参数中添加后再调整。'` |
| H401 / OX-COMMON | R107 | K11:1114 | `'aggregate_rules' in changes and type(values['aggregate_rules']) is not bool` → `'合并相邻规则必须为布尔值。'` |
| H402 / OX-COMMON | R107 | K11:1116 | `'custom_match_position' in changes and values['custom_match_position'] not in ('head', 'tail')` → `'自定义匹配执行顺序必须为 head 或 tail。'` |
| H403 / OX-COMMON | R107 | K11:1184 | `available.get(target, {}).get('type') != 'sequence'` → `f'分流执行链 {target} 未配置，请在完整参数中添加后再调整。'` |

### K12 [backend/common/oxidns_common_updates.py](../../backend/common/oxidns_common_updates.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H404 / OX-COMMON | R107 | K12:130 | `changes.get('rule_update.enabled') is True` → `'规则更新流水线来自 include 文件或已重命名，请在完整参数或附加文件中调整'` |
| H405 / OX-COMMON | R107 | K12:154 | `changes.get('rule_update.enabled') is True` → `'已有自定义规则更新插件，请在完整参数中配置其定时任务，不能重复创建更新链'` |
| H406 / OX-COMMON | R107 | K12:162 | `not isinstance(download.get('args'), dict)` → `_error()` |
| H407 / OX-COMMON | R107 | K12:176 | `not isinstance(reload.get('args'), list)` → `_error()` |
| H408 / OX-COMMON | R107 | K12:193 | `cron.get('type') != 'cron' or not isinstance(cron.get('args'), dict) or (not isinstance(cron['args'].get('jobs'), list))` → `_error()` |
| H409 / OX-COMMON | R107 | K12:197 | `len(jobs) > 1 or any((job.get('name') != 'refresh_rules' or job.get('executors') != ['$rules_refresh'] for job in jobs))` → `_error()` |
| H410 / OX-COMMON | R107 | K12:225 | `ctx.plugin(tag).get('type') != _PIPELINE[tag]` → `_error()` |
| H411 / OX-COMMON | R107 | K12:228 | `refresh and refresh.get('args') not in (_REFRESH, _DOWNLOAD_ONLY, [])` → `_error()` |
| H412 / OX-COMMON | R107 | K12:232 | `_external_references(ctx, targets, jobs)` → `_error()` |
| H413 / OX-COMMON | R107 | K12:258 | `refresh and (refresh.get('type') != 'sequence' or refresh.get('args') not in (_REFRESH, _DOWNLOAD_ONLY))` → `_error()` |
| H414 / OX-COMMON | R107 | K12:263 | `item.get('tag') not in ('rules_reload', 'rules_refresh') and _references(item, 'rules_reload')` → `_error()` |
| H415 / OX-COMMON | R107 | K12:265 | `_references({key: value for (key, value) in document.items() if key != 'plugins'}, 'rules_reload')` → `_error()` |

### K13 [backend/common/oxidns_device_policy.py](../../backend/common/oxidns_device_policy.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H416 / OX-COMMON | R107 | K13:25 | `not isinstance(raw, str)` → `ValueError()` |
| H417 / OX-COMMON | R107 | K13:28 | `len(parts) > 2 or not re.fullmatch('[0-9a-fA-F:.]+', parts[0])` → `ValueError()` |
| H418 / OX-COMMON | R107 | K13:33 | `not re.fullmatch('[0-9]{1,3}', parts[1])` → `ValueError()` |
| H419 / OX-COMMON | R107 | K13:36 | `not 0 <= prefix <= address.max_prefixlen` → `ValueError()` |
| H420 / OX-COMMON | R107 | K13:40 | `prefix < 96` → `ValueError()` |
| H421 / OX-COMMON | R107 | K13:44 | `network.is_multicast` → `ValueError()` |
| H422 / OX-COMMON | R107 | K13:47 | `address.is_unspecified` → `ValueError()` |
| H423 / OX-COMMON | R107 | K13:51 | `捕获 ValueError` → `'请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.0.2.10/24。'` |
| H424 / OX-COMMON | R107 | K13:56 | `not isinstance(value, list) or len(value) > 512` → `'设备策略必须为列表，最多 512 条策略。'` |
| H425 / OX-COMMON | R107 | K13:60 | `not isinstance(row, dict) or set(row) - {'ip', 'name', 'mode'}` → `'设备策略仅支持 IP / 网段、备注和策略。'` |
| H426 / OX-COMMON | R107 | K13:63 | `ip in seen` → `f'客户端 IP / 网段重复：{ip}'` |
| H427 / OX-COMMON | R107 | K13:67 | `not isinstance(name, str) or len(name) > 80 or any((ord(c) < 32 for c in name))` → `'设备备注最多 80 个字符，不能包含换行或控制字符。'` |
| H428 / OX-COMMON | R107 | K13:69 | `mode not in ('smart', 'direct')` → `'设备策略必须为智能分流或直连。'` |
| H429 / OX-COMMON | R107 | K13:103 | `外部依赖/存储返回异常` → `f'{reason}，无法安全插入设备策略；{fix}。'` |
| H430 / OX-COMMON | R107 | K13:329 | `owned != {POLICY_TAG: old, DIRECT_TAG: DIRECT} or len(hooks) != 1` → `'设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。'` |
| H431 / OX-COMMON | R107 | K13:333 | `index != _prefix_end(args)` → `'设备策略的位置已被修改，请先恢复到分流规则之前。'` |
| H432 / OX-COMMON | R107 | K13:335 | `owned or hooks` → `'完整参数中存在同名设备策略插件，请先处理命名冲突。'` |
| H433 / OX-COMMON | R107 | K13:340 | `POLICY_TAG in text or DIRECT_TAG in text` → `'设备策略插件存在额外引用，请先在完整参数中处理。'` |

### K14 [backend/common/oxidns_editor.py](../../backend/common/oxidns_editor.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H434 / OX-COMMON | R113 | K14:263 | `choice == 'template'` → `f'{_label(path)}仍被完整配置引用，请先移除相关文件引用再清空内容'` |
| H435 / OX-COMMON | R113 | K14:272 | `not isinstance(proposed_inputs, dict)` → `'template_settings 必须为模板参数对象'` |
| H436 / OX-COMMON | R113 | K14:279 | `not isinstance(resolutions, dict) or any((not isinstance(path, str) or choice not in ('current', 'template') for (path, choice) in resolutions.items()))` → `'resolutions 必须按参数路径选择 current 或 template'` |

### K17 [backend/common/oxidns_native_apply.py](../../backend/common/oxidns_native_apply.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H437 / OX-NATIVE | R109 | K17:35 | `not isinstance(items, list) or any((not isinstance(item, dict) or not isinstance(item.get('tag'), str) for item in items))` → `'插件结构尚未完成，请先填写插件 tag 和参数'` |
| H438 / OX-NATIVE | R109 | K17:38 | `len(result) != len(items)` → `'插件 tag 重复，请先使用不同名称再联动修改'` |
| H439 / OX-NATIVE | R109 | K17:116 | `refs` → `'不能自动删除共享插件，请先在完整参数中调整引用'` |
| H440 / OX-NATIVE | R109 | K17:151 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H441 / OX-NATIVE | R109 | K17:171 | `candidate.get('include')` → `'配置包含 include，不能自动移除 Hosts 处理链'` |
| H442 / OX-NATIVE | R109 | K17:187 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H443 / OX-NATIVE | R109 | K17:215 | `candidate.get('include')` → `'配置包含 include，不能自动接入标准功能'` |
| H444 / OX-NATIVE | R109 | K17:220 | `refs` → `'不能自动接入共享或自定义处理链，请先调整引用'` |
| H445 / OX-NATIVE | R109 | K17:225 | `not isinstance(parked, dict)` → `'暂存参数结构无效，请先在完整参数中修正'` |
| H446 / OX-NATIVE | R109 | K17:255 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H447 / OX-NATIVE | R109 | K17:267 | `not available` → `'规则定时更新需要当前配置引用至少一个可下载订阅'` |
| H448 / OX-NATIVE | R109 | K17:270 | `refresh and refresh.get('args') not in (_REFRESH, _DOWNLOAD_ONLY)` → `'规则更新包含自定义步骤，不能自动接入定时任务'` |
| H449 / OX-NATIVE | R109 | K17:277 | `any((_path(item) in historical - desired for item in download['args']['downloads']))` → `'新增下载插件仍包含已撤回的受管资源，请先删除旧下载条目'` |
| H450 / OX-NATIVE | R109 | K17:300 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H451 / OX-NATIVE | R109 | K17:317 | `not capability['editable']` → `capability['reason']` |
| H452 / OX-NATIVE | R109 | K17:334 | `main[index + 1:index + 2] != [accept]` → `'Hosts 使用了自定义返回处理，请在完整参数中调整'` |
| H453 / OX-NATIVE | R109 | K17:346 | `not isinstance(native_config, str)` → `'native_config 必须为 YAML 文本'` |
| H454 / OX-NATIVE | R109 | K17:358 | `renames is not None and (not isinstance(renames, dict) or any((not isinstance(key, str) or not isinstance(value, str) for (key, value) in renames.items())))` → `'renames 必须为原 tag 到新 tag 的文本映射'` |
| H455 / OX-NATIVE | R109 | K17:361 | `standard_add is not None and (not isinstance(standard_add, list) or any((not isinstance(tag, str) for tag in standard_add)) or len(set(standard_add)) != len(standard_add))` → `'standard_add 必须为不重复的标准功能 tag 数组'` |
| H456 / OX-NATIVE | R109 | K17:364 | `any((tag not in _STANDARD or tag not in added or proposed[tag].get('type') != _TAG_TYPES.get(tag, 'sequence') for tag in standard_add))` → `'标准功能新增必须对应唯一、类型正确且尚不存在的标准插件'` |
| H457 / OX-NATIVE | R109 | K17:366 | `len(set(renames.values())) != len(renames) or any((tag not in removed or new not in added for (tag, new) in renames.items()))` → `'重命名映射必须对应唯一的原插件和新增 tag，不能覆盖现有插件'` |
| H458 / OX-NATIVE | R109 | K17:372 | `standard_add & set(renames.values())` → `'标准功能新增不能同时作为插件重命名目标'` |
| H459 / OX-NATIVE | R109 | K17:374 | `any((not new or len(new) > 64 or (not TAG.fullmatch(new)) or new.startswith(('qs.exec.', 'qs.match.', 'qs.cron.')) or (old[tag].get('type') != proposed[new].get('type')) for (ta…（完整条件见源行）` → `'重命名需要有效的非空 tag，并保持插件类型；请继续在完整参数中填写'` |
| H460 / OX-NATIVE | R109 | K17:376 | `removed and any((not name.strip() for name in added))` → `'请先填写非空的新插件 tag，再同步重命名或删除'` |
| H461 / OX-NATIVE | R109 | K17:380 | `dynamic or candidate.get('include')` → `'重命名前请先确认外部或环境变量中的引用'` |
| H462 / OX-NATIVE | R109 | K17:385 | `left` → `'存在无法安全自动重命名的自定义引用'` |
| H463 / OX-NATIVE | R109 | K17:390 | `not isinstance(disabled, dict)` → `'暂存参数结构无效，不能记录插件重命名'` |
| H464 / OX-NATIVE | R109 | K17:393 | `not isinstance(aliases, dict)` → `'插件重命名记录结构无效，请在完整参数中修正'` |
| H465 / OX-NATIVE | R109 | K17:444 | `removed & _addition_targets(additions)` → `'同一次修改不能删除标准功能的依赖并重新启用该功能，请分步调整'` |
| H466 / OX-NATIVE | R109 | K17:456 | `candidate.get('include')` → `'配置包含 include，不能自动修改其依赖链'` |
| H467 / OX-NATIVE | R109 | K17:478 | `not (removed and (not additions) and (not reference_locations(candidate, removed)))` → `str(exc)` |
| H468 / OX-NATIVE | R109 | K17:516 | `refs` → `'删除后仍有插件引用，请在完整参数中调整'` |

### K16 [backend/common/oxidns_native.py](../../backend/common/oxidns_native.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H469 / OX-NORM | R110 | K16:40 | `not isinstance(key, str) or key in ('__proto__', 'prototype', 'constructor')` → `'OxiDNS YAML 字段名必须为安全文本'` |
| H470 / OX-NORM | R110 | K16:42 | `key in result` → `'OxiDNS YAML 包含重复字段'` |
| H471 / OX-NORM | R110 | K16:67 | `not isinstance(text, str) or len(text.encode('utf-8')) > MAX_NATIVE_BYTES` → `f'OxiDNS {label}必须为不超过 2 MiB 的 YAML 文本'` |
| H472 / OX-NORM | R110 | K16:73 | `捕获 (yaml.YAMLError, RecursionError, ValueError)` → `f'OxiDNS {label} YAML 语法无效'` |
| H473 / OX-NORM | R110 | K16:81 | `count > 50000 or depth > 32` → `'OxiDNS YAML 超出结构大小限制'` |
| H474 / OX-NORM | R110 | K16:84 | `id(item) in active` → `'OxiDNS YAML 不支持循环别名'` |
| H475 / OX-NORM | R110 | K16:90 | `not (item is None or isinstance(item, (str, int, float, bool)))` → `'OxiDNS YAML 只支持普通对象、数组和标量'` |
| H476 / OX-NORM | R110 | K16:92 | `isinstance(item, float) and (not math.isfinite(item))` → `'OxiDNS YAML 数字必须为有限值'` |
| H477 / OX-NORM | R110 | K16:96 | `not isinstance(value, dict)` → `f'OxiDNS {label}顶层必须为对象'` |
| H478 / OX-NORM | R110 | K16:103 | `not isinstance(value, list) or len(value) > MAX_ADDITIONAL_FILES` → `'附加文件必须为数组，最多 256 个文件'` |
| H479 / OX-NORM | R110 | K16:107 | `not isinstance(item, dict)` → `'附加文件条目必须为对象'` |
| H480 / OX-NORM | R110 | K16:110 | `path in ('config.yaml', 'manifest.json', 'state') or path.startswith(('state/', '.configflow'))` → `'附加文件不能覆盖主配置、部署元数据或 state/ 运行数据'` |
| H481 / OX-NORM | R110 | K16:112 | `path in seen` → `'附加文件路径重复'` |
| H482 / OX-NORM | R110 | K16:114 | `any((path.startswith(previous + '/') or previous.startswith(path + '/') for previous in seen))` → `'附加文件路径存在文件与目录冲突'` |
| H483 / OX-NORM | R110 | K16:118 | `not isinstance(content, str) or encoding not in ('text', 'base64')` → `'附加文件内容必须为文本，编码为 text 或 base64'` |
| H484 / OX-NORM | R110 | K16:120 | `len(content) > MAX_ADDITIONAL_FILE_BYTES * 2` → `'单个附加文件不能超过 16 MiB'` |
| H485 / OX-NORM | R110 | K16:124 | `捕获 (binascii.Error, ValueError)` → `'附加文件 Base64 内容无效'` |
| H486 / OX-NORM | R110 | K16:126 | `len(data) > MAX_ADDITIONAL_FILE_BYTES` → `'单个附加文件不能超过 16 MiB'` |
| H487 / OX-NORM | R110 | K16:129 | `total > MAX_ADDITIONAL_TOTAL_BYTES` → `'附加文件总大小不能超过 64 MiB'` |
| H488 / OX-NORM | R110 | K16:151 | `len(parents) > 8 or count > 512` → `'OxiDNS include 超过 8 层或文件数量限制'` |
| H489 / OX-NORM | R110 | K16:154 | `not isinstance(includes, list) or any((not isinstance(item, str) for item in includes))` → `'OxiDNS include 必须为文件路径数组'` |
| H490 / OX-NORM | R110 | K16:157 | `not isinstance(plugins, list)` → `'OxiDNS plugins 必须为数组'` |
| H491 / OX-NORM | R110 | K16:162 | `not reference or reference.startswith('/') or '${' in reference or ('\\' in reference) or any((ord(c) < 32 for c in reference))` → `'include 必须引用附加文件中的相对路径，不能使用绝对路径或环境变量'` |
| H492 / OX-NORM | R110 | K16:167 | `path in parents or path == name` → `'OxiDNS include 存在循环引用'` |
| H493 / OX-NORM | R110 | K16:169 | `path not in assets` → `'OxiDNS include 文件未在附加文件中提供'` |
| H494 / OX-NORM | R110 | K16:173 | `捕获 UnicodeDecodeError` → `'OxiDNS include 文件必须为 UTF-8 YAML'` |
| H495 / OX-NORM | R110 | K16:175 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H496 / OX-NORM | R110 | K16:191 | `any((item['severity'] == 'error' for item in issues))` → `issues` |
| H497 / OX-NORM | R110 | K16:201 | `not isinstance(value, str) or not value` → `'OxiDNS 文件引用必须为非空文本'` |
| H498 / OX-NORM | R110 | K16:207 | `not isinstance(args['files'], list)` → `'OxiDNS 文件引用必须为列表'` |
| H499 / OX-NORM | R110 | K16:237 | `not isinstance(root, str) or not isinstance(index, str)` → `'OxiDNS WebUI 路径必须为文本'` |
| H500 / OX-NORM | R110 | K16:254 | `path is not None and path not in normalized` → `'OxiDNS 配置引用了未打包的资源文件：' + path` |
| H501 / OX-NORM | R110 | K16:264 | `path in normalized` → `'OxiDNS 运行数据不能与附加文件重合'` |

### K20 [backend/common/oxidns_settings.py](../../backend/common/oxidns_settings.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H502 / OX-NORM | R110 | K20:82 | `not isinstance(value, str) or not value or '\\' in value or any((ord(c) < 32 for c in value))` → `f'{label}无效'` |
| H503 / OX-NORM | R110 | K20:86 | `path.is_absolute() or any((part in ('', '.', '..') for part in normalized.split('/')))` → `f'{label}必须为安全的相对路径'` |
| H504 / OX-NORM | R110 | K20:101 | `not isinstance(value, dict) or any((not isinstance(identity, str) for identity in value))` → `f'{key} 必须为以规则 ID 为键的对象'` |
| H505 / OX-NORM | R110 | K20:106 | `not isinstance(follow, list) or any((not isinstance(identity, str) for identity in follow))` → `'rule_dns_follow 必须为规则 ID 数组'` |
| H506 / OX-NORM | R110 | K20:111 | `identity in settings['rule_actions']` → `f'规则 {identity} 已设置 DNS 处理「{label}」，不能同时跟随策略'` |
| H507 / OX-NORM | R110 | K20:113 | `identity in settings['rule_dns_parked']` → `f'规则 {identity} 已暂存停用前的 DNS 处理，不能同时跟随策略'` |
| H508 / OX-NORM | R110 | K20:120 | `not isinstance(value, dict)` → `'OxiDNS 设置必须是对象'` |
| H509 / OX-NORM | R110 | K20:125 | `settings['config_mode'] not in ('template', 'native')` → `'OxiDNS 配置模式必须为 template 或 native'` |
| H510 / OX-NORM | R110 | K20:127 | `not isinstance(settings['native_config'], str)` → `'OxiDNS 完整配置必须为 YAML 文本'` |
| H511 / OX-NORM | R110 | K20:145 | `isinstance(item, bool) or not isinstance(item, int) or (not minimum <= item <= maximum)` → `f'{key} 必须为 {minimum}–{maximum} 的整数'` |
| H512 / OX-NORM | R110 | K20:155 | `not isinstance(settings[key], bool)` → `f'{key} 必须为布尔值'` |
| H513 / OX-NORM | R110 | K20:158 | `not isinstance(settings[key], list) or any((not isinstance(v, str) for v in settings[key]))` → `f'{key} 必须为规则 ID 数组'` |
| H514 / OX-NORM | R110 | K20:162 | `set(settings[f'direct_{suffix}']) & set(settings[f'proxy_{suffix}'])` → `'同一规则不能同时分配给直连和代理'` |
| H515 / OX-NORM | R110 | K20:165 | `not isinstance(settings[key], str)` → `f'{key} 必须为文本'` |
| H516 / OX-NORM | R110 | K20:167 | `not settings['listen'].strip()` → `'DNS 监听地址不能为空'` |
| H517 / OX-NORM | R110 | K20:169 | `settings['default_forward'] not in ('forward_local', 'forward_remote')` → `'默认转发目标无效'` |
| H518 / OX-NORM | R110 | K20:171 | `settings['custom_match_position'] not in ('head', 'tail')` → `'自定义匹配执行顺序无效'` |
| H519 / OX-NORM | R110 | K20:173 | `not isinstance(settings['custom_matches'], list)` → `'自定义匹配必须为数组'` |
| H520 / OX-NORM | R110 | K20:176 | `not isinstance(entry, dict)` → `'自定义匹配条目必须为对象'` |
| H521 / OX-NORM | R110 | K20:183 | `not isinstance(matches, list) or not any((isinstance(m, str) and m.strip() for m in matches))` → `'启用的自定义匹配需要至少一个条件'` |
| H522 / OX-NORM | R110 | K20:185 | `any((not isinstance(m, str) for m in matches)) or not isinstance(entry.get('exec'), str) or (not entry['exec'].strip())` → `'自定义匹配条件和执行动作必须为非空文本'` |
| H523 / OX-NORM | R110 | K20:187 | `not isinstance(settings['block_qtypes'], list) or any((type(v) is not int or not 1 <= v <= 65535 for v in settings['block_qtypes']))` → `'屏蔽查询类型必须为 1–65535 的整数数组'` |
| H524 / OX-NORM | R110 | K20:194 | `not isinstance(section, dict)` → `f'{name} 必须为对象'` |
| H525 / OX-NORM | R110 | K20:196 | `name != 'log' and (not isinstance(section.get('enabled'), bool))` → `f'{name}.enabled 必须为布尔值'` |
| H526 / OX-NORM | R110 | K20:198 | `'args' in section and (not isinstance(section['args'], dict))` → `f'{name}.args 必须为对象'` |
| H527 / OX-NORM | R110 | K20:202 | `not isinstance(api.get(key), str)` → `f'api.{key} 必须为文本'` |
| H528 / OX-NORM | R110 | K20:205 | `not isinstance(api['listen'], str) or not api['listen'].strip()` → `'API 监听地址不能为空'` |
| H529 / OX-NORM | R110 | K20:207 | `bool(api['username']) != bool(api['password'])` → `'API 用户名和密码必须同时设置'` |
| H530 / OX-NORM | R110 | K20:209 | `not isinstance(api['ssl'], dict)` → `'API TLS 设置必须为对象'` |
| H531 / OX-NORM | R110 | K20:211 | `bool(api['ssl'].get('cert')) != bool(api['ssl'].get('key'))` → `'API TLS 证书和私钥必须同时设置'` |
| H532 / OX-NORM | R110 | K20:214 | `not isinstance(log['file'], str)` → `'日志文件路径必须为文本'` |
| H533 / OX-NORM | R110 | K20:216 | `log['level'] not in ('off', 'trace', 'debug', 'info', 'warn', 'error')` → `'日志级别无效'` |
| H534 / OX-NORM | R110 | K20:218 | `log['rotation'] not in ('never', 'minutely', 'hourly', 'daily', 'weekly')` → `'日志轮转方式无效'` |
| H535 / OX-NORM | R110 | K20:226 | `not safe_relative_path(recorder['args']['path']).startswith('state/')` → `'查询记录请放在 state/ 下，避免部署覆盖运行数据'` |
| H536 / OX-NORM | R110 | K20:230 | `selector['args'].get('selection_mode') not in ('first_success', 'best_within_budget', 'background')` → `'IP 优选模式无效'` |
| H537 / OX-NORM | R110 | K20:235 | `selector['args'].get('dnssec_policy') not in ('reorder_only', 'skip')` → `'IP 优选 DNSSEC 策略无效'` |
| H538 / OX-NORM | R110 | K20:238 | `not isinstance(methods, list) or not methods or any((not isinstance(method, str) for method in methods))` → `'IP 探测方式必须为非空文本数组'` |
| H539 / OX-NORM | R110 | K20:242 | `dynamic['route'] not in ('direct', 'proxy') or type(dynamic['learn']) is not bool` → `'动态域名集路由或学习设置无效'` |
| H540 / OX-NORM | R110 | K20:244 | `not safe_relative_path(dynamic['args']['path']).startswith('state/')` → `'动态规则请放在 state/ 下，避免部署覆盖运行数据'` |
| H541 / OX-NORM | R110 | K20:246 | `not isinstance(dynamic['learn_args'], dict)` → `'学习参数必须为对象'` |
| H542 / OX-NORM | R110 | K20:251 | `not isinstance(bootstrap, list) or any((not isinstance(item, str) for item in bootstrap))` → `'动态域名初始规则必须为文本数组'` |
| H543 / OX-NORM | R110 | K20:254 | `learn.get('phase') not in ('before', 'after') or learn.get('questions') not in ('first', 'all')` → `'动态学习阶段或问题范围无效'` |
| H544 / OX-NORM | R110 | K20:256 | `learn.get('rule_kind') not in ('full', 'domain') or learn.get('error_mode') not in ('continue', 'stop', 'fail')` → `'动态学习规则类型或错误行为无效'` |
| H545 / OX-NORM | R110 | K20:260 | `adguard['mode'] not in ('nxdomain', 'nodata', 'null', 'refused')` → `'AdGuard 拦截响应模式无效'` |
| H546 / OX-NORM | R110 | K20:262 | `not isinstance(adguard['content'], str) or not isinstance(adguard['urls'], list)` → `'AdGuard 规则必须为文本和订阅 URL 数组'` |
| H547 / OX-NORM | R110 | K20:264 | `not adguard['content'].strip() and (not adguard['urls'])` → `'启用 AdGuard 时请填写规则或订阅 URL'` |
| H548 / OX-NORM | R110 | K20:266 | `any((not isinstance(url, str) or not re.match('^https?://', url) for url in adguard['urls']))` → `'AdGuard 订阅必须为 HTTP(S) URL'` |
| H549 / OX-NORM | R110 | K20:272 | `any((not isinstance(args.get(key), str) or not args[key].strip() for key in ('address', 'username', 'password')))` → `f'{name} 需要 RouterOS 地址、用户名和密码'` |
| H550 / OX-NORM | R110 | K20:275 | `not isinstance(args['tls'], dict)` → `'RouterOS TLS 必须为对象，关闭时删除 tls 字段'` |
| H551 / OX-NORM | R110 | K20:277 | `args['tls'].get('ca') and args['tls'].get('insecure')` → `'RouterOS TLS 不能同时设置 CA 和跳过验证'` |
| H552 / OX-NORM | R110 | K20:281 | `args['min_ttl'] > args['max_ttl']` → `'RouterOS 最小 TTL 不能大于最大 TTL'` |
| H553 / OX-NORM | R110 | K20:283 | `name == 'ros_route' and (not args.get('routing_table') or not (args.get('gateway4') or args.get('gateway6')))` → `'RouterOS 路由需要已存在的路由表和至少一个网关'` |
| H554 / OX-NORM | R110 | K20:288 | `not isinstance(schedule, str) or len(schedule.split()) != 5` → `'规则更新需要五段 cron 表达式'` |

### K15 [backend/common/oxidns_global_constraints.py](../../backend/common/oxidns_global_constraints.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H555 / OX-VALID | R114 | K15:74 | `外部依赖/存储返回异常` → `{'path': path, 'message': message, 'code': code}` |

### K18 [backend/common/oxidns_plugin_constraints.py](../../backend/common/oxidns_plugin_constraints.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H556 / OX-VALID | R114 | K18:64 | `re.search('(?<!\\\\)\\\\[1-9]&#124;\\(\\?[=!]&#124;\\(\\?<[=!]', raw)` → `'OxiDNS 的正则不支持反向引用或前后查找'` |
| H557 / OX-VALID | R114 | K18:70 | `捕获 re.error` → `'正则表达式语法无效'` |
| H558 / OX-VALID | R114 | K18:83 | `not pattern.strip()` → `'正则表达式不能为空'` |
| H559 / OX-VALID | R114 | K18:88 | `not value.strip().rstrip('.')` → `'域名规则不能为空'` |
| H560 / OX-VALID | R114 | K18:98 | `len(parts) != 5` → `'必须使用 5 段 Cron 表达式（分 时 日 月 星期）'` |
| H561 / OX-VALID | R114 | K18:106 | `not segment` → `'Cron 字段中有空条目'` |
| H562 / OX-VALID | R114 | K18:110 | `any((n is None or n < (0 if segment.startswith('L-') else 1) for n in nums))` → `'Cron 月日字段超出 1–31'` |
| H563 / OX-VALID | R114 | K18:116 | `len(fragments) > 2 or (len(fragments) == 2 and _uint(fragments[1]) in (None, 0))` → `'Cron 步长必须是字段范围内的正整数'` |
| H564 / OX-VALID | R114 | K18:120 | `base == '?' and index not in (2, 4)` → `'Cron 的 ? 只能用于日或星期字段'` |
| H565 / OX-VALID | R114 | K18:124 | `len(bounds) > 2 or any((_uint(x, hi) is None or _uint(x, hi) < lo for x in bounds))` → `'Cron 字段超出允许范围'` |
| H566 / OX-VALID | R114 | K18:126 | `len(bounds) == 2 and _uint(bounds[0], hi) > _uint(bounds[1], hi)` → `'Cron 范围起点不能大于终点'` |
| H567 / OX-VALID | R114 | K18:140 | `外部依赖/存储返回异常` → `{'path': prefix + path, 'message': message, 'code': code, 'severity': severity}` |
| H568 / OX-VALID | R114 | K18:145 | `error` → `path` |
| H569 / OX-VALID | R114 | K18:153 | `捕获 ValueError` → `path` |
| H570 / OX-VALID | R114 | K18:163 | `_number(low) and _number(high) and (low > high)` → `[lower]` |
| H571 / OX-VALID | R114 | K18:174 | `not key` → `path` |
| H572 / OX-VALID | R114 | K18:176 | `'${' in re.sub('\\$\\{[^}]*\\}', '', stripped)` → `path` |
| H573 / OX-VALID | R114 | K18:181 | `not tokens` → `[]` |
| H574 / OX-VALID | R114 | K18:190 | `kind in ('question', 'reload_provider') and (not token.startswith('$') or not token[1:].strip())` → `[]` |
| H575 / OX-VALID | R114 | K18:193 | `not dns_code_valid(kind, token)` → `[]` |
| H576 / OX-VALID | R114 | K18:195 | `kind == 'mark' and _uint(token, U32) is None` → `[]` |
| H577 / OX-VALID | R114 | K18:202 | `not valid` → `[]` |
| H578 / OX-VALID | R114 | K18:206 | `not entries or all((not _str(x) for x in entries))` → `[]` |
| H579 / OX-VALID | R114 | K18:209 | `isinstance(entry, str) and (not entry.strip() or not re.split('[=:]', entry, maxsplit=1)[0].strip())` → `[j] if isinstance(args, list) else []` |
| H580 / OX-VALID | R114 | K18:213 | `not any((_str(x) for x in entries))` → `[]` |
| H581 / OX-VALID | R114 | K18:226 | `source not in sources and (not (source.startswith('$') and len(source) > 1))` → `[]` |
| H582 / OX-VALID | R114 | K18:228 | `op not in ('eq', 'prefix', 'suffix', 'contains', 'regexp', 'zl')` → `[]` |
| H583 / OX-VALID | R114 | K18:230 | `op == 'zl' and values or (op != 'zl' and (not values))` → `[]` |
| H584 / OX-VALID | R114 | K18:234 | `error` → `[]` |
| H585 / OX-VALID | R114 | K18:242 | `len(tokens) > 2 or any((_uint(x.strip(), U32) is None for x in tokens))` → `[]` |
| H586 / OX-VALID | R114 | K18:244 | `isinstance(args, dict) and (not any((args.get(k) is not None for k in ('fix', 'min', 'max'))))` → `[]` |
| H587 / OX-VALID | R114 | K18:247 | `any((_uint(token, 65535) is None for token in _tokens(args)))` → `[]` |
| H588 / OX-VALID | R114 | K18:255 | `'mode' in args and raw_mode is None` → `['mode']` |
| H589 / OX-VALID | R114 | K18:257 | `isinstance(mode, str) and mode not in ('nxdomain', 'nodata', 'null', 'custom', 'refused') and (not _deferred(mode))` → `['mode']` |
| H590 / OX-VALID | R114 | K18:263 | `mode == 'custom' and isinstance(ips, (list, type(None))) and (not ips)` → `['ips']` |
| H591 / OX-VALID | R114 | K18:265 | `mode is not None and mode != 'custom' and isinstance(ips, list) and ips` → `['ips']` |
| H592 / OX-VALID | R114 | K18:270 | `value not in ('true', 'false')` → `[]` |
| H593 / OX-VALID | R114 | K18:273 | `mode == 'custom' and (not tokens)` → `[]` |
| H594 / OX-VALID | R114 | K18:274 | `mode and mode != 'custom' and tokens` → `[]` |
| H595 / OX-VALID | R114 | K18:282 | `args.get('dump_file') is not None and args.get('dump_interval') == 0` → `['dump_interval']` |
| H596 / OX-VALID | R114 | K18:285 | `kind in ('prefer_ipv4', 'prefer_ipv6') and nullable_default('cache', True) is True and (args.get('cache_ttl') == 0)` → `['cache_ttl']` |
| H597 / OX-VALID | R114 | K18:292 | `not tokens` → `['probe_methods']` |
| H598 / OX-VALID | R114 | K18:297 | `token not in ('ping', 'none') and (not (match and _uint(match[1], 65535) not in (None, 0)))` → `['probe_methods']` |
| H599 / OX-VALID | R114 | K18:299 | `'none' in tokens and len(set(tokens)) > 1` → `['probe_methods']` |
| H600 / OX-VALID | R114 | K18:303 | `cache.get(key) == 0` → `['cache', key]` |
| H601 / OX-VALID | R114 | K18:314 | `kind == 'hosts' and len(tokens) < 2 or (kind == 'redirect' and len(tokens) != 2)` → `[key, j]` |
| H602 / OX-VALID | R114 | K18:319 | `error` → `[key, j]` |
| H603 / OX-VALID | R114 | K18:323 | `len(tokens[1].rstrip('.').encode()) > 253 or any((not part or len(part.encode()) > 63 for part in tokens[1].rstrip('.').split('.')))` → `[key, j]` |
| H604 / OX-VALID | R114 | K18:336 | `error` → `['rules', j]` |
| H605 / OX-VALID | R114 | K18:338 | `isinstance(raw, str) and re.search('(?m)^\\s*\\$(?:INCLUDE&#124;GENERATE)\\b', raw, re.I)` → `['rules', j]` |
| H606 / OX-VALID | R114 | K18:345 | `isinstance(raw, str) and re.search('(?m)^\\s*\\$(?:INCLUDE&#124;GENERATE)\\b', raw, re.I)` → `[key, j]` |
| H607 / OX-VALID | R114 | K18:351 | `message` → `['rules', j]` |
| H608 / OX-VALID | R114 | K18:356 | `not code.strip() or not attr.strip()` → `['selectors', j]` |
| H609 / OX-VALID | R114 | K18:368 | `isinstance(family, str) and family not in ('ip', 'ip6', 'inet')` → `['table_family' + suffix]` |
| H610 / OX-VALID | R114 | K18:370 | `isinstance(args.get(key + suffix), str) and (not args[key + suffix].strip())` → `[key + suffix]` |
| H611 / OX-VALID | R114 | K18:372 | `_number(mask) and mask > limit` → `['mask' + suffix]` |
| H612 / OX-VALID | R114 | K18:376 | `not any((_str(args.get(key)) for key in families))` → `[families[0]]` |
| H613 / OX-VALID | R114 | K18:378 | `isinstance(args.get('comment_prefix'), str) and any((c in args['comment_prefix'] for c in ';='))` → `['comment_prefix']` |
| H614 / OX-VALID | R114 | K18:381 | `isinstance(tls, dict) and tls.get('insecure') is True and _str(tls.get('ca'))` → `['tls', 'ca']` |
| H615 / OX-VALID | R114 | K18:383 | `seconds == 0` → `['wait_timeout']` |
| H616 / OX-VALID | R114 | K18:393 | `捕获 ValueError` → `['persistent', 'ips', j]` |
| H617 / OX-VALID | R114 | K18:395 | `not _str(args.get(families[0 if network.version == 4 else 1]))` → `['persistent', 'ips', j]` |
| H618 / OX-VALID | R114 | K18:397 | `kind == 'ros_route' and network.prefixlen == 0` → `['persistent', 'ips', j]` |
| H619 / OX-VALID | R114 | K18:401 | `_str(timeout) and (not _deferred(timeout)) and (_duration(timeout) is None)` → `['timeout']` |
| H620 / OX-VALID | R114 | K18:404 | `body_count > 1` → `['body']` |
| H621 / OX-VALID | R114 | K18:406 | `args.get('content_type') is not None and (args.get('json') is not None or args.get('form'))` → `['content_type']` |
| H622 / OX-VALID | R114 | K18:411 | `key == 'content_type' and ('\r' in value or '\n' in value)` → `[key]` |
| H623 / OX-VALID | R114 | K18:416 | `not valid` → `['url']` |
| H624 / OX-VALID | R114 | K18:422 | `key != 'query_params' and (not _str(name))` → `path` |
| H625 / OX-VALID | R114 | K18:423 | `key == 'headers' and isinstance(name, str) and (not re.fullmatch("[!#$%&'*+.^_&#96;&#124;~0-9A-Za-z-]+", name.strip()))` → `path` |
| H626 / OX-VALID | R114 | K18:424 | `isinstance(value, str) and (not value.strip())` → `path` |
| H627 / OX-VALID | R114 | K18:431 | `not _str(key)` → `['env', key]` |
| H628 / OX-VALID | R114 | K18:439 | `not filename` → `['downloads', j, 'filename']` |
| H629 / OX-VALID | R114 | K18:441 | `path in seen` → `['downloads', j]` |
| H630 / OX-VALID | R114 | K18:447 | `捕获 (ZoneInfoNotFoundError, ValueError)` → `['timezone']` |
| H631 / OX-VALID | R114 | K18:452 | `(start is None) != (end is None)` → `['periods', j]` |
| H632 / OX-VALID | R114 | K18:455 | `isinstance(value, str) and (not _deferred(value)) and (not re.fullmatch('(?:[01]\\d&#124;2[0-3]):[0-5]\\d', value.strip()))` → `['periods', j, key]` |
| H633 / OX-VALID | R114 | K18:457 | `isinstance(start, str) and isinstance(end, str) and (not _deferred(start)) and (start.strip() == end.strip())` → `['periods', j, 'end']` |
| H634 / OX-VALID | R114 | K18:459 | `start is None and (not period.get('weekdays')) and (not period.get('monthdays'))` → `['periods', j]` |
| H635 / OX-VALID | R114 | K18:463 | `isinstance(day, str) and (not _deferred(day)) and (day.strip().lower() not in ('mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'))` → `['periods', j, 'weekdays', k]` |
| H636 / OX-VALID | R114 | K18:469 | `name in names` → `['jobs', j, 'name']` |
| H637 / OX-VALID | R114 | K18:473 | `bool(_str(schedule)) == bool(_str(interval))` → `['jobs', j]` |
| H638 / OX-VALID | R114 | K18:476 | `error` → `['jobs', j, 'schedule']` |
| H639 / OX-VALID | R114 | K18:479 | `seconds is None or seconds < 60` → `['jobs', j, 'interval']` |
| H640 / OX-VALID | R114 | K18:499 | `raw.count('{qname}') > 1 or raw.count('{qclass}') > 1` → `'每条响应记录中的 {qname} 和 {qclass} 各只能出现一次'` |
| H641 / OX-VALID | R114 | K18:532 | `not origin.is_absolute()` → `'$ORIGIN 必须能解析为绝对域名'` |
| H642 / OX-VALID | R114 | K18:537 | `current_ttl > U32` → `'DNS 记录 TTL 超出 0–4294967295'` |
| H643 / OX-VALID | R114 | K18:542 | `len(values) not in (2, 3)` → `'$INCLUDE 需要文件路径和可选 origin'` |
| H644 / OX-VALID | R114 | K18:543 | `not os.path.isabs(values[1])` → `'内联 zone 中的 $INCLUDE 必须使用绝对路径'` |
| H645 / OX-VALID | R114 | K18:550 | `len(values) < 5` → `'$GENERATE 需要范围、名称、类型和 RDATA'` |
| H646 / OX-VALID | R114 | K18:552 | `not match or any((_uint(part, 9223372036854775807) is None for part in (match[1], match[2], match[3] or '1'))) or _uint(match[1]) > _uint(match[2]) or (_uint(match[3] or '1') == 0)` → `'$GENERATE 范围或步长无效'` |
| H647 / OX-VALID | R114 | K18:554 | `single and record_count > 1` → `'响应记录条目不能包含多条生成记录'` |
| H648 / OX-VALID | R114 | K18:556 | `values[0].startswith('$')` → `'不支持的 zone 指令或指令参数不完整'` |
| H649 / OX-VALID | R114 | K18:559 | `tokens[0].is_quoted_string()` → `'DNS 记录的域名不能放在引号内'` |
| H650 / OX-VALID | R114 | K18:562 | `current_owner is None` → `'省略记录名称前必须先定义名称'` |
| H651 / OX-VALID | R114 | K18:563 | `not current_owner.is_absolute()` → `'相对域名需要 $ORIGIN；否则名称必须以 . 结尾'` |
| H652 / OX-VALID | R114 | K18:576 | `ttl > U32` → `'DNS 记录 TTL 超出 0–4294967295'` |
| H653 / OX-VALID | R114 | K18:578 | `rtype is None` → `'DNS 记录缺少类型和 RDATA'` |
| H654 / OX-VALID | R114 | K18:582 | `'{qname}' in raw and (not original_tokens or original_tokens[0] != '{qname}')` → `'{qname} 只能作为未加引号的记录名称'` |
| H655 / OX-VALID | R114 | K18:584 | `'{qclass}' in raw and (not any((token == '{qclass}' for token in original_tokens[1:type_index])))` → `'{qclass} 只能作为类型前的未加引号的记录类别'` |
| H656 / OX-VALID | R114 | K18:587 | `not data_values` → `'DNS 记录缺少 RDATA'` |
| H657 / OX-VALID | R114 | K18:589 | `len(data_values) < 2 or not data_values[1].isdigit()` → `'RFC3597 记录需要长度和十六进制数据'` |
| H658 / OX-VALID | R114 | K18:591 | `_uint(data_values[1], 65535) is None or len(hexdata) != _uint(data_values[1], 65535) * 2 or re.search('[^0-9a-fA-F]', hexdata)` → `'RFC3597 数据长度或十六进制格式无效'` |
| H659 / OX-VALID | R114 | K18:594 | `rtype not in allowed_text` → `'该记录类型在 OxiDNS 中需要 RFC3597 格式：\\# 长度 十六进制数据'` |
| H660 / OX-VALID | R114 | K18:596 | `position >= len(data_values)` → `'DNS 记录的 RDATA 参数不足'` |
| H661 / OX-VALID | R114 | K18:598 | `not name.is_absolute()` → `'RDATA 中的相对域名需要 $ORIGIN；否则必须以 . 结尾'` |
| H662 / OX-VALID | R114 | K18:607 | `捕获 (ValueError, TypeError, dns.exception.DNSException)` → `'DNS 记录的名称、TTL、类型或 RDATA 格式无效'` |
| H663 / OX-VALID | R114 | K18:609 | `single and (record_count > 1 or (record_count != 1 and (not unresolved_include)))` → `'每个响应记录条目必须恰好包含一条 DNS 记录'` |
| H664 / OX-VALID | R114 | K18:619 | `外部依赖/存储返回异常` → `'Unsupported plugin validation format: ' + str(name)` |

### K19 [backend/common/oxidns_reference_validation.py](../../backend/common/oxidns_reference_validation.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H665 / OX-VALID | R114 | K19:51 | `外部依赖/存储返回异常` → `{'path': list(path), 'message': message, 'code': code, 'severity': severity}` |
| H666 / OX-VALID | R114 | K19:58 | `tag in registry` → `['plugins', index, 'tag']` |
| H667 / OX-VALID | R114 | K19:68 | `not tag` → `path` |
| H668 / OX-VALID | R114 | K19:72 | `target is None` → `path` |
| H669 / OX-VALID | R114 | K19:80 | `not schema` → `path` |
| H670 / OX-VALID | R114 | K19:83 | `schema.get('x-kind') != kind` → `path` |
| H671 / OX-VALID | R114 | K19:85 | `exact and target_type != exact` → `path` |
| H672 / OX-VALID | R114 | K19:87 | `capability == 'domain' and target_type not in _DOMAIN` → `path` |
| H673 / OX-VALID | R114 | K19:89 | `capability == 'ip' and target_type not in _IP` → `path` |
| H674 / OX-VALID | R114 | K19:102 | `for problem in validate_schema(args, schema, path)` → `{**problem, 'path': list(path)}` |
| H675 / OX-VALID | R114 | K19:108 | `for problem in collect_plugin_issues(pseudo, 0, document)` → `problem` |
| H676 / OX-VALID | R114 | K19:119 | `token.split('=', 1)[1] not in ('true', 'false')` → `path` |
| H677 / OX-VALID | R114 | K19:127 | `schema is None` → `path` |
| H678 / OX-VALID | R114 | K19:130 | `schema.get('x-kind') != expected` → `path` |
| H679 / OX-VALID | R114 | K19:133 | `kind not in _QUICK` → `path` |
| H680 / OX-VALID | R114 | K19:141 | `raw.strip()` → `path` |
| H681 / OX-VALID | R114 | K19:145 | `not raw.strip()` → `path` |
| H682 / OX-VALID | R114 | K19:158 | `any((item not in ('short_circuit', 'short_circuit=true', 'short_circuit=false') for item in tokens))` → `path` |
| H683 / OX-VALID | R114 | K19:173 | `not raw.strip() or error` → `path` |
| H684 / OX-VALID | R114 | K19:177 | `any((not _uint(token, 65535) for token in _tokens(raw)))` → `path` |
| H685 / OX-VALID | R114 | K19:184 | `len(tokens) != 1 or '-' not in tokens[0]` → `path` |
| H686 / OX-VALID | R114 | K19:196 | `key != 'qps' and (not _uint(tokens[index]))` → `ValueError()` |
| H687 / OX-VALID | R114 | K19:199 | `捕获 ValueError` → `path` |
| H688 / OX-VALID | R114 | K19:216 | `len(parts) != 3 or parts[1] not in ('inet', 'inet6') or (not _uint(parts[2], 255))` → `path` |
| H689 / OX-VALID | R114 | K19:228 | `len(parts) != 5 or parts[0] not in ('ip', 'ip6', 'inet') or (not parts[1]) or (not parts[2]) or (not _uint(parts[4], 255))` → `path` |
| H690 / OX-VALID | R114 | K19:231 | `parts[3] not in ('ipv4_addr', 'ipv6_addr')` → `path` |
| H691 / OX-VALID | R114 | K19:240 | `len(parts) < 2 or not parts[1].strip()` → `path` |
| H692 / OX-VALID | R114 | K19:252 | `not sep or key not in ('force', 'no_restart', 'github_token', 'bundle')` → `path` |
| H693 / OX-VALID | R114 | K19:255 | `value not in ('true', 'false')` → `path` |
| H694 / OX-VALID | R114 | K19:269 | `not raw.strip()` → `path` |
| H695 / OX-VALID | R114 | K19:280 | `not param or param.strip().startswith('$')` → `path` |
| H696 / OX-VALID | R114 | K19:287 | `tokens and (len(tokens) != 1 or not dns_code_valid('base-rcode', tokens[0]))` → `path` |
| H697 / OX-VALID | R114 | K19:292 | `not tokens or any((not _uint(token) for token in tokens))` → `path` |
| H698 / OX-VALID | R114 | K19:364 | `target in active` → `path` |

### K21 [backend/common/oxidns_validation.py](../../backend/common/oxidns_validation.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H699 / OX-VALID | R114 | K21:116 | `not isinstance(value, str) or '%' in value` → `ValueError()` |
| H700 / OX-VALID | R114 | K21:126 | `not isinstance(value, str) or any((c.isspace() for c in value))` → `ValueError()` |
| H701 / OX-VALID | R114 | K21:131 | `not parts.hostname or parts.scheme not in schemes` → `ValueError()` |
| H702 / OX-VALID | R114 | K21:135 | `捕获 (ValueError, TypeError)` → `'URL/上游地址无效或协议不受支持'` |
| H703 / OX-VALID | R114 | K21:143 | `捕获 (ZoneInfoNotFoundError, ValueError, TypeError, AttributeError)` → `'时区须为有效 IANA 时区，例如 Asia/Shanghai'` |
| H704 / OX-VALID | R114 | K21:152 | `外部依赖/存储返回异常` → `'Unsupported OxiDNS schema format: ' + name` |
| H705 / OX-VALID | R114 | K21:166 | `外部依赖/存储返回异常` → `{'path': list(at), 'message': message, 'code': code, 'severity': severity}` |
| H706 / OX-VALID | R114 | K21:171 | `isinstance(value, str) and ENV.search(value)` → `'含动态占位符，实际值需由目标环境解析；静态校验无法确认展开后的参数'` |
| H707 / OX-VALID | R114 | K21:191 | `kinds and (not any((_is_type(value, kind) for kind in (kinds if isinstance(kinds, list) else [kinds]))))` → `'参数类型无效，需要 ' + ' / '.join(kinds if isinstance(kinds, list) else [kinds])` |
| H708 / OX-VALID | R114 | K21:194 | `'enum' in schema and (not any((type(value) is type(option) and value == option for option in schema['enum'])))` → `'不在该参数允许的选项中'` |
| H709 / OX-VALID | R114 | K21:200 | `key in schema and fails(value, schema[key])` → `label + ' ' + str(schema[key])` |
| H710 / OX-VALID | R114 | K21:202 | `isinstance(value, str) and len(value) < schema.get('minLength', 0)` → `'文本长度不足'` |
| H711 / OX-VALID | R114 | K21:205 | `len(value) < schema.get('minItems', 0)` → `'至少需要 ' + str(schema['minItems']) + ' 个条目'` |
| H712 / OX-VALID | R114 | K21:207 | `'maxItems' in schema and len(value) > schema['maxItems']` → `'条目数量超过 ' + str(schema['maxItems'])` |
| H713 / OX-VALID | R114 | K21:214 | `key not in value` → `'缺少必填参数'` |
| H714 / OX-VALID | R114 | K21:221 | `schema.get('additionalProperties') is False` → `'官方核心不接受此对象中的未知字段'` |
| H715 / OX-VALID | R114 | K21:223 | `'properties' in schema` → `'扩展字段已保留，不在当前版本的校验目录中'` |
| H716 / OX-VALID | R114 | K21:227 | `error` → `error` |
| H717 / OX-VALID | R114 | K21:246 | `schema is None` → `{'path': [*path, 'type'], 'message': '插件未在当前校验目录中，发布时需由目标核心确认支持', 'code': 'unknown-plugin', 'severity': 'warning'}` |
| H718 / OX-VALID | R114 | K21:249 | `'args' not in plugin and (not schema.get('x-args-optional'))` → `{'path': [*path, 'args'], 'message': '缺少插件必填参数', 'code': 'required', 'severity': 'error'}` |
| H719 / OX-VALID | R114 | K21:273 | `any((item['severity'] == 'error' for item in issues))` → `issues` |
| H720 / OX-VALID | R114 | K21:291 | `for item in validate_schema(included, global_schema)` → `{**item, 'source': name}` |

### K40 [backend/utils/node_latency.py](../../backend/utils/node_latency.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H721 / PROBE | R098 | K40:52 | `parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password` → `ValueError` |
| H722 / PROBE | R098 | K40:56 | `not 500 <= timeout <= 10000` → `ValueError` |
| H723 / PROBE | R098 | K40:58 | `捕获 ValueError` → `'节点测速设置无效，请检查测试地址及超时（500–10000 毫秒）'` |
| H724 / PROBE | R098 | K40:62 | `捕获 ValueError` → `str(exc)` |
| H725 / PROBE | R098 | K40:126 | `外部依赖/存储返回异常` → `'独立测速组件未安装，请更新 ConfigFlow 镜像；源码部署请先构建 backend/node-probe'` |
| H726 / PROBE | R098 | K40:133 | `len(payload.encode()) > 8 * 1024 * 1024` → `'本批节点配置过大，请减少每批节点数'` |
| H727 / PROBE | R098 | K40:142 | `捕获 subprocess.TimeoutExpired` → `'测速组件执行超时，已结束本批测试，请重试'` |
| H728 / PROBE | R098 | K40:144 | `捕获 OSError` → `'测速组件无法启动，请检查可执行权限和 CPU 架构'` |
| H729 / PROBE | R098 | K40:147 | `process.returncode == 3` → `'节点测速 DNS 设置无效，请检查服务器地址及引导 DNS'` |
| H730 / PROBE | R098 | K40:149 | `process.returncode` → `'测速组件异常退出，请检查组件版本后重试'` |
| H731 / PROBE | R098 | K40:153 | `response['version'] != 4` → `'测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe'` |
| H732 / PROBE | R098 | K40:155 | `not isinstance(response['results'], list)` → `ValueError` |
| H733 / PROBE | R098 | K40:158 | `set(results) != {proxy['name'] for proxy in proxies}` → `ValueError` |
| H734 / PROBE | R098 | K40:162 | `latency is not None and (type(latency) is not int or latency < 1)` → `ValueError` |
| H735 / PROBE | R098 | K40:164 | `latency is None and (not entry.get('error'))` → `ValueError` |
| H736 / PROBE | R098 | K40:167 | `捕获 (ValueError, TypeError, KeyError)` → `'测速组件返回了无效结果，请检查组件版本'` |
| H737 / PROBE | R098 | K40:190 | `not _test_lock.acquire(blocking=False)` → `'已有节点测速正在进行，请稍后重试'` |
| H738 / PROBE | R098 | K40:200 | `names is not None and keys is not None` → `'names 与 keys 不能同时指定'` |
| H739 / PROBE | R098 | K40:204 | `len(requested) > MAX_NODES` → `f'单次最多测试 {MAX_NODES} 个节点，请分批测试'` |
| H740 / PROBE | R098 | K40:213 | `any((len(by_name.get(name, [])) > 1 for name in requested))` → `'存在同名节点，请使用 keys 指定节点来源后重试'` |
| H741 / PROBE | R098 | K40:219 | `sum((_needs_sub_store(candidates[key]) for key in wanted)) > MAX_CONVERSION_WORKERS` → `f'单次最多测试 {MAX_CONVERSION_WORKERS} 个需要 Sub-Store 转换的节点，请分批测试'` |

### K02 [backend/agents/manager.py](../../backend/agents/manager.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H742 / REBIND | R121 | K02:373 | `source_profile_id == target_profile_id` → `'请选择其他配置空间'` |
| H743 / REBIND | R121 | K02:377 | `not isinstance(expected_agent_ids, list) or not expected_agent_ids or any((not isinstance(item, str) or not item for item in expected_agent_ids)) or (len(set(expected_agent_ids)…（完整条件见源行）` → `'请提供已确认的 Agent ID 列表'` |
| H744 / REBIND | R121 | K02:385 | `{agent['id'] for agent in bound} != set(expected_agent_ids)` → `'Agent 绑定已变化，请刷新列表后重新确认'` |
| H745 / REBIND | R121 | K02:390 | `blocked` → `f"以下 Agent 正在发布、更新或等待恢复，请完成后再改绑：{'、'.join(blocked)}"` |

### K03 [backend/common/config_repository.py](../../backend/common/config_repository.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H746 / REPO | R028 | K03:214 | `not isinstance(items, list)` → `f'{label} 必须是列表'` |
| H747 / REPO | R028 | K03:218 | `not isinstance(item, dict) or not isinstance(item.get('id'), str) or (not item['id'])` → `f'{label} 中每一项必须有 ID'` |
| H748 / REPO | R028 | K03:220 | `item['id'] in result` → `f"{label} 存在重复 ID：{item['id']}"` |
| H749 / REPO | R028 | K03:226 | `not isinstance(values, list) or any((not isinstance(value, str) for value in values))` → `f'{label} 只能包含资源 ID，不能设置资源覆盖'` |
| H750 / REPO | R028 | K03:228 | `len(set(values)) != len(values)` → `f'{label} 含有重复引用'` |
| H751 / REPO | R028 | K03:253 | `not isinstance(value, str) or not value.strip()` → `'资源引用必须是非空 ID'` |
| H752 / REPO | R028 | K03:257 | `value not in catalogs[kind]` → `f'引用的共享资源不存在：{kind}/{value}'` |
| H753 / REPO | R028 | K03:264 | `not isinstance(chain, dict) or set(chain) != {'entry', 'exit'}` → `'代理链必须指定前置和落地引用'` |
| H754 / REPO | R028 | K03:270 | `not isinstance(reference, dict) or set(reference) != {'type', 'id'} or reference.get('type') not in ('node', 'group') or (not isinstance(reference.get('id'), str)) or (not refer…（完整条件见源行）` → `'代理链引用必须是 node/group type 与非空 id'` |
| H755 / REPO | R028 | K03:274 | `reference['id'] not in groups` → `f"代理链引用的策略组不存在：{reference['id']}"` |
| H756 / REPO | R028 | K03:281 | `group.get('source') is not None and (not isinstance(group['source'], str))` → `'策略组 source 必须是字符串'` |
| H757 / REPO | R028 | K03:289 | `not isinstance(order, list)` → `'策略组排序必须是数组'` |
| H758 / REPO | R028 | K03:292 | `not isinstance(item, dict)` → `'策略组排序项必须是 type/id 对象'` |
| H759 / REPO | R028 | K03:332 | `not isinstance(target, str) or not target.strip()` → `'原始 dialer-proxy 必须是非空名称'` |
| H760 / REPO | R028 | K03:337 | `len(node_targets) + len(groups_by_name.get(target, [])) != 1` → `f'原始拨号代理目标不存在或名称不唯一：{target}'` |
| H761 / REPO | R028 | K03:914 | `捕获 StoreConflict` → `'配置数据库在事务外被修改，已放弃本次写入，请重试'` |
| H762 / REPO | R028 | K03:916 | `捕获 StoreError` → `f'配置数据库内容无效：{error}'` |
| H763 / REPO | R028 | K03:938 | `not isinstance(routers, dict)` → `'路由器设置必须是对象'` |
| H764 / REPO | R028 | K03:969 | `old.get(field) and (identity_changed or (field_changed and key != policy_router))` → `f'路由器仍有已发布的{label}；请先发布并核对撤回，再修改类型、地址或删除发布记录'` |
| H765 / REPO | R028 | K03:987 | `not jobs.enter_context(job_lock(profile_id, router_id, router))` → `'路由器正在发布或同步，不能替换连接或删除记录，请稍后重试'` |
| H766 / REPO | R028 | K03:996 | `write` → `'不能在配置事务中再次开启写事务'` |
| H767 / REPO | R028 | K03:1034 | `getattr(self._local, 'document', None) is not None` → `'不能在配置事务中再次开启写事务'` |
| H768 / REPO | R028 | K03:1048 | `捕获 StoreConflict` → `'配置数据库在事务外被修改，已放弃本次写入，请重试'` |
| H769 / REPO | R028 | K03:1062 | `profile_id not in document['profiles']` → `profile_id` |
| H770 / REPO | R028 | K03:1137 | `not isinstance(metadata, dict)` → `'Profile metadata must be an object'` |
| H771 / REPO | R028 | K03:1141 | `not isinstance(name, str) or not name.strip()` → `'配置名称不能为空'` |
| H772 / REPO | R028 | K03:1144 | `profile_id in document['profiles']` → `profile_id` |
| H773 / REPO | R028 | K03:1162 | `not isinstance(metadata, dict)` → `'Profile metadata must be an object'` |
| H774 / REPO | R028 | K03:1166 | `not isinstance(name, str) or not name.strip()` → `'配置名称不能为空'` |
| H775 / REPO | R028 | K03:1169 | `profile_id in document['profiles']` → `profile_id` |
| H776 / REPO | R028 | K03:1180 | `key not in PROFILE_FIELDS` → `f'导入结果包含未知字段：{key}'` |
| H777 / REPO | R028 | K03:1191 | `not isinstance(updates, dict) or set(updates) - {'name', 'description'}` → `'只能修改配置名称和说明'` |
| H778 / REPO | R028 | K03:1196 | `not isinstance(value, str) or (key == 'name' and (not value.strip()))` → `'配置名称和说明必须是字符串，名称不能为空'` |
| H779 / REPO | R028 | K03:1232 | `len(document['profiles']) < 2` → `'只有一个配置空间时不能删除默认配置'` |
| H780 / REPO | R028 | K03:1234 | `not new_default_id` → `'删除默认配置时需要指定新的默认配置空间'` |
| H781 / REPO | R028 | K03:1236 | `new_default_id == profile_id or new_default_id not in document['profiles']` → `f'新的默认配置空间不存在：{new_default_id}'` |
| H782 / REPO | R028 | K03:1241 | `usages` → `f"配置「{profile['name']}」仍被以下 Agent 使用：{names}"` |
| H783 / REPO | R028 | K03:1283 | `not callable(updater)` → `'Updater must be callable'` |
| H784 / REPO | R028 | K03:1287 | `not isinstance(replacement, dict)` → `'Updater must return an object or None'` |
| H785 / REPO | R028 | K03:1304 | `rule.get('policy') in removed_names` → `f"配置 {current['name']} 的规则仍引用被删除策略组：{rule['policy']}"` |
| H786 / REPO | R028 | K03:1329 | `current != expected_router` → `'路由器设置已变化，请刷新后重试'` |
| H787 / REPO | R028 | K03:1331 | `replacement is not None and replacement.get('id') != router_id` → `'不能修改路由器 ID'` |
| H788 / REPO | R028 | K03:1357 | `current is None or any((current.get(field) != expected_router.get(field) for field in ROUTER_CONNECTION_FIELDS))` → `'路由器连接已变化，无法保存发布回读记录'` |
| H789 / REPO | R028 | K03:1374 | `not isinstance(fields, dict) or (baseline is not None and (not isinstance(baseline, dict)))` → `'Profile fields and baseline must be objects'` |
| H790 / REPO | R028 | K03:1376 | `set(fields) & {'resource_refs', 'node_dialers'}` → `'资源由策略组直接引用，拨号组合请使用代理链类型'` |
| H791 / REPO | R028 | K03:1379 | `baseline is None and '_revision' in fields and (fields['_revision'] != current['_revision'])` → `'配置已更新，请刷新后重试'` |
| H792 / REPO | R028 | K03:1409 | `not isinstance(content, (str, bytes))` → `'规则内容必须是文本或二进制规则数据'` |
| H793 / REPO | R028 | K03:1423 | `any((item['id'] == rule['id'] for item in shared['rule_library']))` → `'规则库 ID 已存在，请刷新后重试'` |
| H794 / REPO | R028 | K03:1428 | `path.exists() or path.with_name(path.name + '.bak').exists()` → `'规则缓存已存在，请使用新的规则 ID'` |
| H795 / REPO | R028 | K03:1447 | `preserve_cache` → `'规则保存结果待确认，请刷新列表核对，勿重复导入'` |
| H796 / REPO | R028 | K03:1464 | `usages` → `f"资源 {resource.get('name', resource['id'])} 仍被引用：" + '、'.join((u['name'] for u in usages))` |
| H797 / REPO | R028 | K03:1491 | `not isinstance(confirmation, str) or not re.fullmatch('[a-f0-9]{64}', confirmation)` → `'请先查看并确认删除影响'` |
| H798 / REPO | R028 | K03:1495 | `not secrets.compare_digest(confirmation, preview['confirmation'])` → `'节点或引用关系已变化，请重新查看删除影响并确认'` |
| H799 / REPO | R028 | K03:1497 | `preview['blockers']` → `'无法删除：' + '；'.join(preview['blockers'])` |
| H800 / REPO | R028 | K03:1535 | `not isinstance(snapshot, dict)` → `'Shared data must be an object'` |
| H801 / REPO | R028 | K03:1538 | `baseline is None and '_revision' in snapshot and (snapshot['_revision'] != current['_revision'])` → `'共享资源已更新，请刷新后重试'` |
| H802 / REPO | R028 | K03:1584 | `not isinstance(snapshot, dict)` → `'System data must be an object'` |
| H803 / REPO | R028 | K03:1587 | `baseline is None and '_revision' in snapshot and (snapshot['_revision'] != current['_revision'])` → `'系统设置已更新，请刷新后重试'` |
| H804 / REPO | R028 | K03:1600 | `resource_type not in SHARED_FIELDS` → `'未知资源类型'` |
| H805 / REPO | R028 | K03:1641 | `not isinstance(data, dict)` → `'Imported profile must be an object'` |
| H806 / REPO | R028 | K03:1644 | `not isinstance(data, dict) or any((key in data for key in SHARED_FIELDS))` → `'独立配置导入只接受引用，不接受共享资源副本'` |
| H807 / REPO | R028 | K03:1646 | `set(data) & {'resource_refs', 'node_dialers'}` → `'独立配置导入仅支持当前格式'` |
| H808 / REPO | R028 | K03:1649 | `'_revision' in data and data['_revision'] != current['_revision']` → `'配置已更新，请刷新后重试'` |
| H809 / REPO | R028 | K03:1858 | `not isinstance(profile_id, str) or not _PROFILE_ID.fullmatch(profile_id)` → `'Invalid profile id'` |
| H810 / REPO | R028 | K03:1869 | `捕获 ValueError` → `'Profile path escapes profile directory'` |
| H811 / REPO | R028 | K03:1874 | `not isinstance(relative_path, str) or not relative_path or '\x00' in relative_path` → `'Invalid profile relative path'` |
| H812 / REPO | R028 | K03:1880 | `捕获 ValueError` → `'Profile path escapes profile directory'` |
| H813 / REPO | R028 | K03:1937 | `remaining <= 0` → `'Timed out waiting for profile repository lock'` |
| H814 / REPO | R028 | K03:1999 | `written != expected` → `f'Incomplete write for {path}: {written} of {expected} bytes'` |
| H815 / REPO | R028 | K03:2042 | `backup_temp.stat().st_size != len(previous)` → `'incomplete backup write'` |
| H816 / REPO | R028 | K03:2059 | `Path(filename).name != filename or filename not in {'config.yaml', 'config.conf', 'loon.lcf'}` → `'Invalid generated filename'` |
| H817 / REPO | R028 | K03:2100 | `not isinstance(profile, dict)` → `'旧配置内容必须是对象'` |
| H818 / REPO | R028 | K03:2104 | `捕获 DialerReferenceError` → `str(error)` |
| H819 / REPO | R028 | K03:2116 | `subscription_id and (not isinstance(subscription_id, str) or subscription_id not in subscriptions)` → `f'旧配置 {profile_id} 的节点引用的订阅不存在'` |
| H820 / REPO | R028 | K03:2133 | `not isinstance(target, str) or not target.strip()` → `'原始 dialer-proxy 必须是非空名称'` |
| H821 / REPO | R028 | K03:2139 | `len(matches) != 1` → `f'旧配置 {profile_id} 的原始拨号目标不存在或名称不唯一：{target}'` |
| H822 / REPO | R028 | K03:2187 | `not isinstance(source, dict)` → `'导入内容必须是 JSON 对象'` |
| H823 / REPO | R028 | K03:2197 | `not isinstance(old_system, dict) or not isinstance(old_profiles, dict)` → `'旧多配置备份必须包含 system 索引和全部 profiles 配置内容'` |
| H824 / REPO | R028 | K03:2201 | `set(metadata) != set(old_profiles)` → `'旧多配置备份的索引与配置内容不一致'` |
| H825 / REPO | R028 | K03:2208 | `not (version is None and (not set(source) & {'profiles', 'shared', 'resource_refs', 'node_dialers'}))` → `'仅支持原版单配置、schema 2 完整多配置备份或当前格式；未修改原数据'` |
| H826 / REPO | R028 | K03:2220 | `not isinstance(settings, dict)` → `'系统设置必须是对象'` |
| H827 / REPO | R028 | K03:2263 | `not isinstance(raw_profile, dict)` → `'旧配置内容必须是对象'` |
| H828 / REPO | R028 | K03:2265 | `set(raw_profile) & {'resource_refs', 'node_dialers', 'schema_version', 'shared', 'profiles'}` → `'旧配置包含不支持的中间格式字段；未修改原数据'` |
| H829 / REPO | R028 | K03:2275 | `value not in remaps[kind]` → `f'旧配置 {profile_id} 引用的本地资源不存在：{kind}/{value}'` |
| H830 / REPO | R028 | K03:2281 | `not isinstance(resources, list) or any((not isinstance(item, dict) for item in resources))` → `f'{kind} 必须是资源对象数组'` |
| H831 / REPO | R028 | K03:2418 | `document.get('schema_version') != self.SCHEMA_VERSION` → `'配置存储版本不匹配'` |
| H832 / REPO | R028 | K03:2421 | `not isinstance(document.get(key), dict)` → `f'缺少配置节：{key}'` |
| H833 / REPO | R028 | K03:2424 | `not isinstance(system.get('system_config'), dict)` → `'系统设置和备份设置必须是对象'` |
| H834 / REPO | R028 | K03:2427 | `default_id is not None and (not isinstance(default_id, str))` → `'默认配置空间 ID 必须是字符串'` |
| H835 / REPO | R028 | K03:2429 | `self._default_profile_id(document) not in document['profiles']` → `'必须保留默认配置'` |
| H836 / REPO | R028 | K03:2431 | `set(system) - self.SYSTEM_FIELDS` → `'system 节只接受全局设置、备份与 Agent'` |
| H837 / REPO | R028 | K03:2433 | `not isinstance(system.get('system_config'), dict) or not isinstance(system.get('backup'), dict)` → `'系统设置和备份设置必须是对象'` |
| H838 / REPO | R028 | K03:2448 | `捕获 ValueError` → `str(error)` |
| H839 / REPO | R028 | K03:2453 | `捕获 DialerReferenceError` → `str(error)` |
| H840 / REPO | R028 | K03:2456 | `'dialer_ref' in node` → `'共享节点不能保存拨号引用，请在策略组中创建代理链'` |
| H841 / REPO | R028 | K03:2458 | `node.get('subscription_id') and node['subscription_id'] not in catalogs['subscriptions']` → `'节点引用的订阅不存在'` |
| H842 / REPO | R028 | K03:2461 | `'policy' in resource or 'target' in resource` → `'规则仓库不包含目标策略，请在规则配置中设置'` |
| H843 / REPO | R028 | K03:2464 | `any((not isinstance(name, str) or not name for name in library_names)) or len(set(library_names)) != len(library_names)` → `'共享规则仓库名称必须非空且唯一'` |
| H844 / REPO | R028 | K03:2469 | `value not in catalogs[field] and (not (field == 'nodes' and value in BUILTIN_POLICIES))` → `f"聚合 {aggregation.get('name')} 引用了不存在的资源：{value}"` |
| H845 / REPO | R028 | K03:2473 | `not isinstance(profile, dict) or profile.get('id') != profile_id` → `'配置内容与 ID 不一致'` |
| H846 / REPO | R028 | K03:2481 | `捕获 DialerReferenceError` → `str(error)` |
| H847 / REPO | R028 | K03:2485 | `捕获 ProfileInUse` → `f"配置 {profile['name']}: {error}"` |
| H848 / REPO | R028 | K03:2488 | `agent.get('profile_id') not in document['profiles']` → `f"Agent {agent.get('name', agent['id'])} 绑定的配置不存在"` |
| H849 / REPO | R028 | K03:2491 | `not isinstance(section.get('_revision'), int) or section['_revision'] < 0` → `'配置修订号无效'` |
| H850 / REPO | R028 | K03:2496 | `any((key in profile for key in SHARED_FIELDS))` → `'独立配置不能保存共享资源副本'` |
| H851 / REPO | R028 | K03:2498 | `not isinstance(profile.get('name'), str) or not profile['name'].strip()` → `'配置名称不能为空'` |
| H852 / REPO | R028 | K03:2500 | `set(profile) & {'resource_refs', 'node_dialers'}` → `'独立配置不接受资源白名单或节点拨号覆盖字段'` |
| H853 / REPO | R028 | K03:2505 | `捕获 DialerReferenceError` → `str(error)` |
| H854 / REPO | R028 | K03:2510 | `any((not isinstance(name, str) or not name for name in names))` → `'策略组名称必须非空'` |
| H855 / REPO | R028 | K03:2513 | `len(group_names) != len(groups) or group_names & BUILTIN_POLICIES` → `'策略组名称必须唯一，且不能使用 DIRECT 或 REJECT'` |
| H856 / REPO | R028 | K03:2519 | `捕获 ValueError` → `str(error)` |
| H857 / REPO | R028 | K03:2524 | `value not in catalogs[kind] and (not (kind == 'nodes' and value in BUILTIN_POLICIES))` → `f'{label} 引用了不存在的资源：{value}'` |
| H858 / REPO | R028 | K03:2529 | `value not in catalogs[legacy_kind] and (not (legacy_kind == 'nodes' and value in BUILTIN_POLICIES))` → `f'{label} 引用了不存在的资源：{value}'` |
| H859 / REPO | R028 | K03:2535 | `target not in groups or target == group['id']` → `f'{label} 引用了无效的策略组：{target}'` |
| H860 / REPO | R028 | K03:2537 | `group.get('follow_group') and groups[group['follow_group']].get('type') == 'chain'` → `'代理链请通过引用策略使用，不能作为跟随目标'` |
| H861 / REPO | R028 | K03:2545 | `available is None or value not in available or (kind == 'strategy' and value == group['id'])` → `f'{label} 的排序列表包含无效引用：{value}'` |
| H862 / REPO | R028 | K03:2549 | `rule.get('itemType') not in ('rule', 'ruleset')` → `'规则类型必须为 rule 或 ruleset'` |
| H863 / REPO | R028 | K03:2551 | `rule['itemType'] == 'ruleset' and rule.get('library_rule_id') not in catalogs['rule_library']` → `'规则集必须引用全局规则仓库，不能单独设置来源'` |
| H864 / REPO | R028 | K03:2554 | `not isinstance(profile.get(engine), dict)` → `f'{engine} 参数必须是对象'` |
| H865 / REPO | R028 | K03:2557 | `not isinstance(profile.get('loon', {}), dict)` → `'loon 参数必须是对象'` |
| H866 / REPO | R028 | K03:2560 | `not isinstance(profile.get('domain_discovery', {}), dict)` → `'domain_discovery 参数必须是对象'` |
| H867 / REPO | R028 | K03:2568 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H868 / REPO | R028 | K03:2572 | `not isinstance(routers, dict) or not isinstance(routers.get('items', []), list)` → `'routers 参数必须是包含 items 列表的对象'` |
| H869 / REPO | R028 | K03:2575 | `not isinstance(profile.get('oxidns', {}), dict)` → `'oxidns 参数必须是对象'` |
| H870 / REPO | R028 | K03:2582 | `value not in rules or rules[value]['itemType'] != expected` → `f'{label} {key} 仍引用不存在的规则 {value}，请先取消选择'` |
| H871 / REPO | R028 | K03:2588 | `not isinstance(value, (dict, list)) or any((not isinstance(identity, str) for identity in value))` → `f'OxiDNS {key} 必须以规则 ID 为键'` |
| H872 / REPO | R028 | K03:2591 | `identity not in rules` → `f'OxiDNS DNS 处理仍引用不存在的规则 {identity}，请先取消'` |
| H873 / REPO | R028 | K03:2601 | `捕获 OxidnsSettingsError` → `f'OxiDNS {key} 中规则 {identity} 的 DNS 处理无效：{exc}'` |
| H874 / REPO | R028 | K03:2604 | `setting.get('group_id') not in groups` → `'Surge Smart 设置引用的策略组不存在'` |

### K04 [backend/common/config_store.py](../../backend/common/config_store.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H875 / REPO | R028 | K04:106 | `not isinstance(section, dict)` → `f'配置节 {scope}/{owner} 必须是对象'` |
| H876 / REPO | R028 | K04:109 | `not isinstance(field, str)` → `f'配置节 {scope}/{owner} 的字段名必须是字符串'` |
| H877 / REPO | R028 | K04:116 | `not isinstance(document, dict)` → `'配置文档必须是对象'` |
| H878 / REPO | R028 | K04:123 | `not isinstance(key, str)` → `'配置文档的顶层键必须是字符串'` |
| H879 / REPO | R028 | K04:130 | `not isinstance(value, dict)` → `'profiles 必须是对象'` |
| H880 / REPO | R028 | K04:133 | `not isinstance(owner, str) or not owner` → `'配置空间 ID 必须是非空字符串'` |
| H881 / REPO | R028 | K04:147 | `(scope, owner) not in sections` → `f'字段 {scope}/{owner}/{field} 没有所属配置节'` |
| H882 / REPO | R028 | K04:156 | `len(set(root_order)) != len(root_order) or set(expected_root) != set(root)` → `'顶层键顺序与数据库内容不一致'` |
| H883 / REPO | R028 | K04:159 | `(scope in root_order) != ((scope, '') in sections)` → `f'配置节 {scope} 与顶层键顺序不一致'` |
| H884 / REPO | R028 | K04:162 | `profiles and PROFILES_KEY not in root_order` → `'配置空间与顶层键顺序不一致'` |
| H885 / REPO | R028 | K04:220 | `_is_corrupt(error)` → `f'配置数据库已损坏：{error}'` |
| H886 / REPO | R028 | K04:221 | `捕获 sqlite3.Error` → `f'配置数据库访问失败：{error}'` |
| H887 / REPO | R028 | K04:256 | `捕获 ValueError` → `f'数据库元信息无法解析：{error}'` |
| H888 / REPO | R028 | K04:262 | `not isinstance(store_id, str) or not isinstance(generation, int) or isinstance(generation, bool)` → `'数据库元信息缺少 store_id/generation'` |
| H889 / REPO | R028 | K04:264 | `commit_id is not None and (not isinstance(commit_id, str))` → `'数据库元信息 commit_id 无效'` |
| H890 / REPO | R028 | K04:289 | `not isinstance(root_order, list) or any((not isinstance(key, str) for key in root_order))` → `'数据库元信息 root_order 无效'` |
| H891 / REPO | R028 | K04:298 | `捕获 ValueError` → `f"字段 {'/'.join(key)} 不是有效 JSON：{error}"` |
| H892 / REPO | R028 | K04:342 | `head != base.head` → `'配置数据库在事务外被修改'` |
| H893 / REPO | R028 | K04:426 | `str(mode).lower() != 'delete'` → `f'无法设置数据库日志模式：{mode}'` |
| H894 / REPO | R028 | K04:444 | `result != [('ok',)]` → `f'新建数据库未通过完整性检查：{result}'` |
| H895 / REPO | R028 | K04:450 | `dumps(entry.document) != dumps(document)` → `'新建数据库的内容与迁移文档不一致'` |
| H896 / REPO | R028 | K04:483 | `result != [('ok',)]` → `'完整性检查失败：' + '; '.join((str(row[0]) for row in result[:5]))` |
| H897 / REPO | R028 | K04:487 | `missing` → `'缺少数据表：' + '、'.join(sorted(missing))` |
| H898 / REPO | R028 | K04:491 | `missing` → `'缺少数据库元信息：' + '、'.join(missing)` |
| H899 / REPO | R028 | K04:494 | `not isinstance(version, int) or isinstance(version, bool) or version < 1` → `f'数据库存储版本无效：{version!r}'` |
| H900 / REPO | R028 | K04:496 | `version > STORAGE_VERSION` → `f'数据库存储版本 {version} 高于当前支持的 {STORAGE_VERSION}'` |
| H901 / REPO | R028 | K04:501 | `mode != 'delete'` → `f'无法把数据库日志模式恢复为 DELETE：{mode}'` |
| H902 / REPO | R028 | K04:506 | `_is_corrupt(error)` → `str(error)` |

### K22 [backend/common/rule_dns.py](../../backend/common/rule_dns.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H903 / RULE | R187 | K22:100 | `外部依赖/存储返回异常` → `f'不支持的 DNS 处理：{route}'` |
| H904 / RULE | R187 | K22:107 | `not isinstance(value, dict) or value.get('action') not in allowed` → `'规则 DNS 处理无效：' + '、'.join(allowed)` |
| H905 / RULE | R187 | K22:111 | `set(value) != {'action'}` → `f'规则 DNS 处理「{ROUTE_LABELS[action]}」不能包含其他参数'` |
| H906 / RULE | R187 | K22:114 | `set(value) != {'action', 'source', 'upstream'} or value['source'] not in UPSTREAM_SOURCES` → `'指定上游需要 source（local、remote 或 fallback）和 upstream'` |
| H907 / RULE | R187 | K22:117 | `not isinstance(upstream, dict) or not isinstance(upstream.get('addr'), str) or (not upstream['addr'].strip())` → `'指定上游需要非空的 addr 地址'` |
| H908 / RULE | R187 | K22:174 | `route not in pinned` → `'指定上游缺少上游地址，请重新选择'` |
| H909 / RULE | R187 | K22:708 | `外部依赖/存储返回异常` → `f'不支持的 DNS 处理：{value}'` |
| H910 / RULE | R187 | K22:749 | `entry is None` → `_STALE_UPSTREAM` |
| H911 / RULE | R187 | K22:751 | `entry['linked']` → `_LINKED_UPSTREAM` |
| H912 / RULE | R187 | K22:787 | `identity not in rules` → `f'规则不存在：{identity}，请刷新后重试'` |
| H913 / RULE | R187 | K22:790 | `choice != 'default' and (not convertible(rules[identity]))` → `f'{kind} 规则不参与 DNS 分流，请把 DNS 设为默认上游'` |
| H914 / RULE | R187 | K22:804 | `strict` → `current['reason']` |
| H915 / RULE | R187 | K22:837 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H916 / RULE | R187 | K22:858 | `added` → `'写入后 OxiDNS 配置校验失败：' + sorted(added)[0][2]` |
| H917 / RULE | R187 | K22:861 | `strict` → `str(exc)` |

### K46 [backend/utils/rule_syntax.py](../../backend/utils/rule_syntax.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H918 / RULE | R187 | K46:35 | `not isinstance(value, str) or not value or looks_like_ip(value)` → `'不是有效域名；IP 地址或网段请使用 IP-CIDR。'` |
| H919 / RULE | R187 | K46:39 | `捕获 UnicodeError` → `'不是有效域名。'` |
| H920 / RULE | R187 | K46:43 | `len(ascii_value) > 253 or ascii_value.rsplit('.', 1)[-1].isdigit() or any((not re.fullmatch('[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?', part) for part in ascii_value.split(…（完整条件见源行）` → `'不是有效域名。'` |
| H921 / RULE | R187 | K46:50 | `not isinstance(value, str) or not value` → `'请填写有效 CIDR，例如 192.0.2.0/24 或 2001:db8::/32。'` |
| H922 / RULE | R187 | K46:55 | `捕获 ValueError` → `'不是有效 IP 网段；IPCIDR 只接受带前缀长度的 CIDR。'` |
| H923 / RULE | R187 | K46:56 | `'/' not in value` → `f'单个 IP 也需要前缀长度，请填写 {value}/{address.max_prefixlen}。'` |
| H924 / RULE | R187 | K46:59 | `not re.fullmatch('(?:0&#124;[1-9][0-9]*)', prefix) or '%' in address` → `'不是有效 IP 网段；请使用数字前缀长度。'` |
| H925 / RULE | R187 | K46:63 | `捕获 ValueError` → `'不是有效 IP 网段；请检查地址与前缀长度。'` |
| H926 / RULE | R187 | K46:81 | `len(segments) > 28` → `f'{label}最多支持 28 组范围。'` |
| H927 / RULE | R187 | K46:85 | `not match` → `f'{label}请填写数字或范围，以 / 分隔，例如 80/443/8000-9000。'` |
| H928 / RULE | R187 | K46:88 | `max(len(first_text), len(last_text)) > len(str(maximum))` → `f'{label}范围必须在 0–{maximum} 内。'` |
| H929 / RULE | R187 | K46:91 | `first > last or last > maximum` → `f'{label}范围必须在 0–{maximum} 内，且起点不大于终点。'` |
| H930 / RULE | R187 | K46:130 | `depth < 0` → `'正则表达式的括号不匹配。'` |
| H931 / RULE | R187 | K46:132 | `escaped or in_class or depth` → `'正则表达式含未闭合括号、字符组或末尾转义。'` |
| H932 / RULE | R187 | K46:136 | `捕获 RecursionError` → `'正则表达式嵌套过深，请简化表达式。'` |
| H933 / RULE | R187 | K46:148 | `depth > 32` → `'逻辑规则嵌套过深，请拆分规则。'` |
| H934 / RULE | R187 | K46:150 | `not value.startswith('(') or not value.endswith(')')` → `'逻辑规则应使用括号包裹条件，例如 ((DOMAIN,example.com),(NETWORK,tcp))。'` |
| H935 / RULE | R187 | K46:164 | `nesting < 0` → `'逻辑规则的括号不匹配。'` |
| H936 / RULE | R187 | K46:166 | `nesting` → `'逻辑规则的括号不匹配。'` |
| H937 / RULE | R187 | K46:168 | `not children or (kind == 'NOT' and len(children) != 1)` → `'逻辑规则需要有效的子条件；NOT 必须恰好包含一个条件。'` |
| H938 / RULE | R187 | K46:172 | `child_kind in ('MATCH', 'SUB-RULE')` → `f'逻辑规则不能包含 {child_kind}。'` |
| H939 / RULE | R187 | K46:184 | `not isinstance(value, str) or '\n' in value or '\r' in value` → `'规则值必须是单行文本。'` |
| H940 / RULE | R187 | K46:188 | `not value.strip()` → `'规则值为空。'` |
| H941 / RULE | R187 | K46:190 | `',' in value and kind not in COMMA_PAYLOAD_TYPES` → `'此规则值不能包含逗号；端口列表请使用 / 分隔，策略与参数应单独填写。'` |
| H942 / RULE | R187 | K46:204 | `kind in ('IP-ASN', 'SRC-IP-ASN') and (not re.fullmatch('[0-9]+', value))` → `'ASN 应为非负整数。'` |
| H943 / RULE | R187 | K46:206 | `kind == 'NETWORK' and value.lower() not in ('tcp', 'udp')` → `'NETWORK 只接受 tcp 或 udp。'` |

### K06 [backend/common/gateway_onboarding.py](../../backend/common/gateway_onboarding.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H944 / SETUP | R211 | K06:121 | `item and args(item).get('entry') != 'sequence_main'` → `'原生 DNS 监听入口使用自定义处理链，无法确认网关分流路径'` |
| H945 / SETUP | R211 | K06:128 | `proxy.get('type') != 'sequence' or not (fallback_enabled or (allow_disabled_fallback and fallback_disabled))` → `'原生 proxy_dns_seq 使用自定义处理链，无法确认 Mihomo 上游'` |
| H946 / SETUP | R211 | K06:136 | `fallback.get('type') != 'fallback' or args(fallback).get('primary') != 'remote_dns' or args(fallback).get('secondary') != 'fallback_dns'` → `'原生 fallback 使用自定义处理链，无法确认 Mihomo 代理与备用上游'` |
| H947 / SETUP | R211 | K06:140 | `secondary.get('type') != 'sequence' or secondary.get('args') != [{'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]` → `'原生 fallback_dns 使用自定义处理链，无法确认 Mihomo 上游'` |
| H948 / SETUP | R211 | K06:147 | `remote.get('type') != 'sequence' or not isinstance(steps, list) or steps[:2] != [{'exec': '$forward_remote'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}] or any((not isinsta…（完整条件见源行）` → `'原生 remote_dns 使用自定义处理链，无法确认 Mihomo 上游'` |
| H949 / SETUP | R211 | K06:161 | `document.get('include')` → `'原生备用 DNS 含 include，无法安全准备网关上游'` |
| H950 / SETUP | R211 | K06:183 | `not isinstance(item, dict) or item.get('tag') != tag or item.get('type') != kinds[tag] or (tag == 'fallback_dns' and args != secondary) or (tag != 'fallback_dns' and (not isinst…（完整条件见源行）` → `'原生备用 DNS 使用自定义插件，无法安全准备网关上游'` |
| H951 / SETUP | R211 | K06:206 | `dynamic_reference(document) or any((_references(remaining, tag) for tag in tags))` → `'原生备用 DNS 仍有自定义或动态引用，无法安全准备网关上游'` |
| H952 / SETUP | R211 | K06:220 | `not isinstance(value, str) or not value.strip() or len(value.encode('utf-8')) > 16384` → `'请填写直连 DNS，使用 IP 地址的 DoH / DoT，例如 https://192.0.2.10/dns-query'` |
| H953 / SETUP | R211 | K06:224 | `捕获 OxidnsSettingsError` → `str(exc)` |
| H954 / SETUP | R211 | K06:226 | `len(upstreams) > 16` → `'直连 DNS 最多支持 16 个上游'` |
| H955 / SETUP | R211 | K06:236 | `parts.scheme not in ('https', 'tls') or address.version != 4 or address.is_loopback or address.is_unspecified or address.is_multicast or address.is_link_local or parts.username …（完整条件见源行）` → `ValueError` |
| H956 / SETUP | R211 | K06:238 | `parts.scheme == 'https' and (not parts.path)` → `ValueError` |
| H957 / SETUP | R211 | K06:240 | `捕获 (ValueError, TypeError)` → `'直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口'` |
| H958 / SETUP | R211 | K06:246 | `not isinstance(value, dict) or set(value) - INPUT_FIELDS` → `'安装设置必须是 JSON 对象，且只能包含向导字段'` |
| H959 / SETUP | R211 | K06:253 | `not isinstance(ids, list) or len(ids) > 1024 or any((not isinstance(item, str) or not item or len(item) > 256 or any((ord(c) < 32 or ord(c) == 127 for c in item)) for item in ids))` → `f'{key} 必须为有效规则 ID 数组'` |
| H960 / SETUP | R211 | K06:256 | `set(result['direct_rule_set_ids']) & set(result['proxy_rule_set_ids'])` → `'同一域名规则集不能同时选择直连和代理'` |
| H961 / SETUP | R211 | K06:259 | `type(result.get('query_recorder', False)) is not bool` → `'query_recorder 必须为布尔值'` |
| H962 / SETUP | R211 | K06:261 | `type(result.get('confirm_template_conversion', False)) is not bool` → `'confirm_template_conversion 必须为布尔值'` |
| H963 / SETUP | R211 | K06:263 | `'expected_config_revision' in result and (type(result['expected_config_revision']) is not int or result['expected_config_revision'] < 0)` → `'配置版本无效，请刷新安装向导'` |
| H964 / SETUP | R211 | K06:265 | `result.get('default_forward', 'forward_remote') not in ('forward_remote', 'forward_local')` → `'请选择有效的未匹配域名 DNS'` |
| H965 / SETUP | R211 | K06:312 | `state == 'invalid'` → `MESSAGES['ROUTER_SPLIT_INVALID']` |
| H966 / SETUP | R211 | K06:316 | `捕获 LinuxGatewayError` → `str(exc)` |
| H967 / SETUP | R211 | K06:441 | `捕获 (TypeError, ValueError)` → `'Linux 本机地址无效，请重新核对安装网络信息'` |
| H968 / SETUP | R211 | K06:444 | `not isinstance(source, str)` → `'Mihomo 基础配置必须为 YAML 文本'` |
| H969 / SETUP | R211 | K06:447 | `not isinstance(document, dict) or not isinstance(document.get('dns', {}), dict)` → `'Mihomo 基础配置及 dns 必须是 YAML 对象，请先修正基础配置'` |
| H970 / SETUP | R211 | K06:452 | `mihomo_dns_loop(dns, host_addresses)` → `'Mihomo DNS 上游不能指向本机 OxiDNS 或系统 DNS，否则会形成解析循环；请先修改基础配置'` |
| H971 / SETUP | R211 | K06:518 | `not value.get('confirm_template_conversion')` → `'现有 OxiDNS 无法安全读取：' + str(exc) + '。请修正原生配置，或明确确认转换为模板。'` |
| H972 / SETUP | R211 | K06:531 | `item is None or (not item['available'] and identity not in selected_before)` → `'所选域名规则集不可用：' + (item['reason'] if item else '不存在或不属于当前配置空间')` |
| H973 / SETUP | R211 | K06:570 | `not value.get('confirm_template_conversion')` → `'现有 OxiDNS 自定义结构无法安全更新：' + str(exc) + '。请在完整参数中调整，或明确确认转换为模板；不会自动覆盖原生配置。'` |
| H974 / SETUP | R211 | K06:592 | `not report['validation']['oxidns']['valid']` → `'OxiDNS 配置校验未通过，请根据 validation.oxidns.issues 修正后重试'` |
| H975 / SETUP | R211 | K06:597 | `not prerequisites['ready']` → `'当前配置没有被启用规则引用的可用代理出口；请先添加节点或订阅、配置策略组，并为需要代理的规则选择该策略'` |
| H976 / SETUP | R211 | K06:615 | `gateway_settings(config)['enabled']` → `'主路由分流不能同时启用 Linux 网关，请重新预览并保存'` |
| H977 / SETUP | R211 | K06:621 | `not is_full_tun_document(document)` → `'Mihomo 基础 YAML 缺少完整 TUN，请重新预览并保存'` |
| H978 / SETUP | R211 | K06:623 | `not state['settings']['enabled']` → `'请先在安装向导预览并保存网关配置'` |
| H979 / SETUP | R211 | K06:629 | `any((type(values.get(key)) is not type(expected) or values.get(key) != expected for (key, expected) in required.items()))` → `'已保存的 OxiDNS 监听、IPv4 或默认分流不符合安装方案，请重新预览并保存'` |
| H980 / SETUP | R211 | K06:631 | `values.get('default_forward') not in ('forward_local', 'forward_remote')` → `'已保存的 OxiDNS 默认 DNS 无效，请重新预览并保存'` |
| H981 / SETUP | R211 | K06:637 | `not upstreams or any((item.get('addr') not in (DNS_TARGET, 'udp://' + DNS_TARGET) or item.get('socks5') or item.get('outbound') for item in upstreams))` → `'OxiDNS 代理和备用上游必须均为 127.0.0.1:1053，请重新预览并保存'` |
| H982 / SETUP | R211 | K06:644 | `any((not caps.get(field, {}).get('editable') for field in ('default_forward', 'direct_rulesets', 'proxy_rulesets')))` → `'OxiDNS 自定义分流结构无法确认，请回到安装向导重新预览'` |
| H983 / SETUP | R211 | K06:647 | `not validation['valid']` → `'已保存的 OxiDNS 配置校验未通过，请修正后重新准备安装'` |
| H984 / SETUP | R211 | K06:652 | `not document.get('dns', {}).get('nameserver')` → `'Mihomo 缺少 DNS 上游，请重新预览并保存网关配置'` |
| H985 / SETUP | R211 | K06:655 | `not prerequisites['ready']` → `'当前配置没有被启用规则引用的可用代理出口，请先完善节点和规则'` |
| H986 / SETUP | R211 | K06:660 | `捕获 (LinuxGatewayError, OxidnsSettingsError, ValueError, yaml.YAMLError)` → `str(exc)` |

### K23 [backend/common/setup_abandon.py](../../backend/common/setup_abandon.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H987 / SETUP | R211 | K23:24 | `session.get('completed')` → `'向导已完成，无需放弃'` |

### K24 [backend/common/setup_completion.py](../../backend/common/setup_completion.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H988 / SETUP | R211 | K24:62 | `not isinstance(forwarding, dict) or not isinstance(tun, dict)` → `'请将 Mihomo Agent 更新到 1.8.40-go 或更新版本后再检查'` |
| H989 / SETUP | R211 | K24:64 | `forwarding.get('current') != '1'` → `'Linux IPv4 转发未开启，请在安装与检查中点击「开启并持久化 IPv4 转发」'` |
| H990 / SETUP | R211 | K24:66 | `forwarding.get('persisted') is not True` → `'IPv4 转发尚未持久化，重启后可能失效；请在安装与检查中点击「开启并持久化 IPv4 转发」'` |
| H991 / SETUP | R211 | K24:69 | `managed.get('active') or managed.get('pending') or managed.get('error')` → `'设备上仍有 Linux 网关的转发记录，请先在配置生成页的 Linux 网关中撤销后再检查'` |
| H992 / SETUP | R211 | K24:71 | `status.get('enabled_in_config') or tun.get('gateway_marker')` → `'已发布的 Mihomo 仍是 Linux 网关配置，请重新发布'` |
| H993 / SETUP | R211 | K24:73 | `tun.get('configured') is not True` → `'已发布的 Mihomo 未启用完整 TUN，请检查基础 YAML 后重新发布'` |
| H994 / SETUP | R211 | K24:75 | `tun.get('present') is not True` → `f"Mihomo TUN（{tun.get('device') or 'Meta'}）未运行，请检查 Mihomo 日志"` |
| H995 / SETUP | R211 | K24:136 | `not gateway or gateway['phase'] != 'published' or gateway['target_changed'] or (gateway['config_changed'] and (not checked or checked['id'] != gateway['id'] or checked['fingerpr…（完整条件见源行）` → `'请先在安装向导完成 Mihomo、OxiDNS 的发布与检查；配置变化后需重新发布'` |
| H996 / SETUP | R211 | K24:139 | `not all((agents.get(service, {}).get('id') for service in ('mihomo', 'oxidns')))` → `'两个核心的设备尚未确认，请继续安装向导'` |
| H997 / SETUP | R211 | K24:144 | `any((agents[service].get('enabled') is False or agents[service].get('status') != 'online' for service in ('mihomo', 'oxidns')))` → `'安装设备离线或已禁用，请检查连接后再检查'` |
| H998 / SETUP | R211 | K24:149 | `捕获 MonitorError` → `error.message` |
| H999 / SETUP | R211 | K24:155 | `not managed.get('active') or managed.get('pending') or managed.get('healthy') is not True` → `'Linux 转发尚未启用或健康状态未确认，请继续安装向导'` |
| H1000 / SETUP | R211 | K24:180 | `type(make_default) is not bool` → `'请选择是否设为默认配置空间'` |
| H1001 / SETUP | R211 | K24:190 | `missing` → `f'主路由转发规则集「{names}」的 IP 段仍未下载，静态路由说明不完整。请检查 ConfigFlow 服务器能否访问规则地址后重新检查。'` |
| H1002 / SETUP | R211 | K24:195 | `shown_missing` → `f'主路由转发规则集「{names}」的 IP 段已下载，静态路由表有新增网段。请重新检查，按更新后的列表配置主路由后再确认。'` |
| H1003 / SETUP | R211 | K24:198 | `body.get('ack_routes') is not True or body.get('ack_dns') is not True` → `'请先按说明配置主路由的静态路由与 DNS，再确认已完成'` |
| H1004 / SETUP | R211 | K24:209 | `not router or router.get('proxy_gateway_agent_id') != pair['mihomo']['id'] or router.get('dns_agent_id') != pair['oxidns']['id']` → `'请关联本次安装的 Mihomo 和 OxiDNS 设备，再预览并发布路由器配置'` |
| H1005 / SETUP | R211 | K24:215 | `捕获 RouterError` → `str(error)` |
| H1006 / SETUP | R211 | K24:219 | `plan['blockers'] or not plan['empty']` → `'路由器配置尚未同步，请在差异报告中预览并完成发布'` |
| H1007 / SETUP | R211 | K24:222 | `next((item for item in latest if item['id'] == router_id), None) != router` → `'路由器设置在检查期间已变化，请重新检查'` |
| H1008 / SETUP | R211 | K24:225 | `not (mode == 'managed')` → `'请选择路由器配置方式'` |
| H1009 / SETUP | R211 | K24:230 | `core_fingerprint(current) != session['installation']['fingerprint']` → `'核心配置在检查期间已变化，请重新发布后检查'` |

### K25 [backend/common/setup_flow.py](../../backend/common/setup_flow.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1010 / SETUP | R211 | K25:37 | `not isinstance(session_id, str) or not re.fullmatch('[a-f0-9]{32}', session_id)` → `'向导记录不存在'` |
| H1011 / SETUP | R211 | K25:58 | `捕获 FileNotFoundError` → `'向导记录不存在，请重新开始'` |
| H1012 / SETUP | R208 | K25:87 | `goal not in ('router', 'gateway')` → `'请选择使用方式'` |
| H1013 / SETUP | R208 | K25:89 | `not isinstance(name, str) or not 1 <= len(name.strip()) <= 64` → `'配置名称需为 1–64 个字符'` |
| H1014 / SETUP | R208 | K25:95 | `session['goal'] != goal or session['name'] != name.strip()` → `'该向导已创建，请继续原向导或新建一次配置'` |
| H1015 / SETUP | R208 | K25:99 | `any((p['id'] == profile_id for p in repository.list_profiles()))` → `'配置名称标识冲突，请重新开始'` |
| H1016 / SETUP | R211 | K25:221 | `捕获 (LinuxGatewayError, GatewayOnboardingError)` → `str(error)` |
| H1017 / SETUP | R211 | K25:229 | `error` → `error` |
| H1018 / SETUP | R211 | K25:234 | `not custom_controller_secret(document)` → `'请设置控制面板密码：Mihomo 控制面板（external-controller）需要用它防止他人访问'` |
| H1019 / SETUP | R211 | K25:246 | `tun is not None and (not isinstance(tun, dict))` → `'Mihomo tun 必须是对象，请先修正基础配置'` |
| H1020 / SETUP | R211 | K25:251 | `捕获 LinuxGatewayError` → `str(error)` |
| H1021 / SETUP | R211 | K25:258 | `for key in tun_filters(tun)` → `f'Linux 网关与现有 tun.{key} 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管'` |
| H1022 / SETUP | R211 | K25:266 | `port in (reserved or {})` → `f'Mihomo 基础配置的 {key} 使用端口 {port}，与{reserved[port]}冲突；请先在 Mihomo 基础配置中改用其他端口'` |
| H1023 / SETUP | R211 | K25:310 | `session['prepared']` → `'方案已生成；修改来源请前往订阅来源或创建新的向导'` |
| H1024 / SETUP | R211 | K25:315 | `not isinstance(values, list) or len(values) > 100 or any((not isinstance(v, str) for v in values))` → `'请选择有效的订阅或节点'` |
| H1025 / SETUP | R211 | K25:318 | `not isinstance(url, str) or len(url) > 8192` → `'订阅链接格式不正确'` |
| H1026 / SETUP | R211 | K25:322 | `not _http_url(url)` → `'请输入 HTTP(S) 订阅链接'` |
| H1027 / SETUP | R211 | K25:339 | `set(values) - available` → `'所选来源已删除或停用，请刷新后重选'` |
| H1028 / SETUP | R211 | K25:341 | `set(subscription_ids) - {item['id'] for item in selectable_subscriptions(shared)}` → `'所选订阅未填写订阅链接，请改用订阅链接添加'` |
| H1029 / SETUP | R211 | K25:343 | `not subscription_ids and (not node_ids)` → `'请添加订阅链接或选择已有节点'` |
| H1030 / SETUP | R211 | K25:356 | `resource_id not in items` → `'所选来源已删除或停用，请返回检查节点来源'` |
| H1031 / SETUP | R211 | K25:360 | `cache.get('metadata', {}).get('url', items[resource_id]['url']) != items[resource_id]['url']` → `'订阅链接已变化，请重新读取订阅'` |
| H1032 / SETUP | R211 | K25:368 | `not available` → `'当前没有可用节点，请先读取订阅或选择支持的节点'` |
| H1033 / SETUP | R211 | K25:375 | `set(body) - {'dns_fallback', 'traffic_fallback', 'local_dns', 'domain_rules', 'template', 'query_recorder', 'controller_secret'}` → `'漏网处理参数无效'` |
| H1034 / SETUP | R211 | K25:377 | `body.get('template', 'basic') != 'basic'` → `'代理优先模式已移除，请刷新向导并分别选择 DNS 与连接的漏网处理'` |
| H1035 / SETUP | R211 | K25:381 | `dns_fallback not in ('mihomo', 'direct') or traffic_fallback not in ('proxy', 'direct')` → `'请选择有效的 DNS 与连接漏网处理方式'` |
| H1036 / SETUP | R211 | K25:386 | `type(query_recorder) is not bool` → `'DNS 查询记录开关必须为布尔值'` |
| H1037 / SETUP | R211 | K25:391 | `捕获 GatewayOnboardingError` → `str(error)` |
| H1038 / SETUP | R211 | K25:397 | `not isinstance(selections, dict) or set(selections) != {'direct', 'proxy'}` → `'请选择有效的直连和代理域名规则集'` |
| H1039 / SETUP | R211 | K25:401 | `not isinstance(values, list) or len(values) > 100 or any((not isinstance(value, str) or not value or len(value) > 256 for value in values))` → `'域名规则集必须是有效的选项列表'` |
| H1040 / SETUP | R211 | K25:404 | `set(selections['direct']) & set(selections['proxy'])` → `'同一个域名规则集不能同时设为直连和代理'` |
| H1041 / SETUP | R211 | K25:407 | `any((set(values) - available for values in selections.values()))` → `'所选域名规则集已删除、停用或不支持，请刷新后重选'` |
| H1042 / SETUP | R211 | K25:490 | `edited and (not unchanged)` → `'该空间已在其他页面修改，向导不会覆盖；请新建向导或前往配置生成继续'` |
| H1043 / SETUP | R211 | K25:496 | `not generated.get('proxy-groups') or not generated.get('rules')` → `'生成配置未包含有效代理策略，请检查来源'` |
| H1044 / SETUP | R211 | K25:500 | `current['_revision'] != stored['_revision']` → `'配置已更新，请刷新向导后重试'` |
| H1045 / SETUP | R210 | K25:531 | `set(body) - {'step', 'router_id', 'router_mode'}` → `'向导进度参数无效'` |
| H1046 / SETUP | R210 | K25:534 | `type(body['step']) is not int or not 0 <= body['step'] <= session['max_step']` → `'请先完成前面的步骤'` |
| H1047 / SETUP | R210 | K25:538 | `body['router_mode'] not in ('manual', 'managed') or session['goal'] != 'router'` → `'请选择有效的路由器配置方式'` |
| H1048 / SETUP | R210 | K25:543 | `not any((router['id'] == body['router_id'] for router in routers))` → `'请选择当前配置空间中的路由器'` |
| H1049 / SETUP | R211 | K25:551 | `session['goal'] != 'router' or not session['prepared'] or session['max_step'] != 3` → `'请在安装等待期间填写主路由信息；安装完成后使用路由器管理'` |
| H1050 / SETUP | R211 | K25:553 | `set(body) != {'router'}` → `'主路由连接参数无效'` |
| H1051 / SETUP | R211 | K25:560 | `not isinstance(payload, dict) or set(payload) - set((*EDITABLE_FIELDS, 'password'))` → `'主路由连接参数无效'` |
| H1052 / SETUP | R211 | K25:563 | `payload.get('proxy_gateway_agent_id') or payload.get('dns_agent_id') or payload.get('auto_rule_sync')` → `'安装完成后自动关联本次设备，暂不启用规则自动同步'` |
| H1053 / SETUP | R211 | K25:567 | `捕获 RouterValidationError` → `str(error)` |
| H1054 / SETUP | R211 | K25:572 | `draft['ros_policy_rule_sets']` → `'新配置空间还没有设为智能分流的客户端设备，暂不能使用策略路由；完成向导后先在「客户端设备」中设置，再到上游路由中启用'` |
| H1055 / SETUP | R211 | K25:597 | `existing != router` → `'主路由记录已在其他页面修改，请检查路由器设置'` |

### K42 [backend/utils/provider_delivery.py](../../backend/utils/provider_delivery.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1056 / SUB | R154 | K42:94 | `names.intersection(self.chain_names)` → `'Provider 节点名称与代理链出口冲突'` |
| H1057 / SUB | R154 | K42:122 | `not isinstance(proxies, list)` → `'Provider proxies 必须是数组'` |
| H1058 / SUB | R154 | K42:127 | `not isinstance(proxy, dict) or not isinstance(proxy.get('name'), str) or (not proxy['name'].strip())` → `'Provider 代理必须是含非空 name 的对象'` |
| H1059 / SUB | R154 | K42:130 | `'proxies' in proxy and (not isinstance(proxy['proxies'], list) or any((not isinstance(name, str) or not name.strip() for name in proxy['proxies'])))` → `'Provider proxies 引用必须是字符串数组'` |
| H1060 / SUB | R154 | K42:132 | `'type' in proxy and (not isinstance(proxy['type'], str))` → `'Provider type 必须是字符串'` |
| H1061 / SUB | R154 | K42:141 | `捕获 yaml.YAMLError` → `_provider_source_error(exc, text, source_label)` |
| H1062 / SUB | R154 | K42:144 | `not isinstance(data, dict)` → `'Provider 必须是 YAML 对象\n字段：文档根节点'` |
| H1063 / SUB | R154 | K42:147 | `捕获 DialerReferenceError` → `_provider_source_error(exc, text, source_label)` |
| H1064 / SUB | R154 | K42:171 | `捕获 StrategyReferenceError` → `exc` |
| H1065 / SUB | R154 | K42:218 | `not cache or not cache.get('nodes')` → `'订阅没有可用缓存或远程代理' + detail` |
| H1066 / SUB | R154 | K42:249 | `chains and any((proxies is None for proxies in collections))` → `DialerReferenceError('拨号拓扑不能使用未验证的 Provider URL fallback')` |
| H1067 / SUB | R154 | K42:262 | `捕获 DialerReferenceError` → `exc` |
| H1068 / SUB | R154 | K42:354 | `捕获 StrategyReferenceError` → `exc` |
| H1069 / SUB | R154 | K42:379 | `not ('/aggregations/' in path)` → `'拨号拓扑 Provider 必须使用当前配置空间的订阅或聚合来源'` |

### K48 [backend/utils/sub_store_client.py](../../backend/utils/sub_store_client.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1070 / SUB | R154 | K48:106 | `not _is_yaml_response(text)` → `f'订阅 URL 未直接返回有效的 proxies YAML。响应预览: {preview}'` |
| H1071 / SUB | R154 | K48:268 | `getattr(getattr(exc, 'response', None), 'status_code', None) in (403, 429)` → `None` |
| H1072 / SUB | R154 | K48:294 | `not _is_yaml_response(text)` → `f'Sub-Store 返回了非 YAML 内容（可能是 HTML 页面），请检查 Sub-Store URL 配置是否正确。响应预览: {preview}'` |
| H1073 / SUB | R154 | K48:311 | `direct_attempted` → `sub_store_error` |
| H1074 / SUB | R154 | K48:317 | `捕获 Exception` → `sub_store_error` |

### K49 [backend/utils/sub_store_runtime.py](../../backend/utils/sub_store_runtime.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1075 / SUB | R154 | K49:71 | `time.monotonic() >= deadline` → `'Sub-Store 正忙或正在更新，请稍后重试'` |
| H1076 / SUB | R154 | K49:97 | `捕获 (OSError, subprocess.TimeoutExpired)` → `'无法管理内置 Sub-Store，请检查容器服务状态'` |
| H1077 / SUB | R154 | K49:104 | `外部依赖/存储返回异常` → `'Sub-Store 服务操作失败，请检查容器日志'` |
| H1078 / SUB | R154 | K49:124 | `外部依赖/存储返回异常` → `'Sub-Store 启动后未就绪，请稍后重试或检查容器日志'` |
| H1079 / SUB | R154 | K49:163 | `mode not in ('on_demand', 'always')` → `'运行模式必须是 on_demand 或 always'` |
| H1080 / SUB | R154 | K49:165 | `not is_builtin_available()` → `'外部 Sub-Store 的运行模式由其部署环境管理'` |

### K50 [backend/utils/subscription_dialers.py](../../backend/utils/subscription_dialers.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1081 / SUB | R154 | K50:204 | `value in taken` → `f'名称「{value}」已被使用，与订阅「{name}」自动生成的中转分组冲突，请改名后重新生成'` |

### K51 [backend/utils/subscription_filter.py](../../backend/utils/subscription_filter.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1082 / SUB | R154 | K51:19 | `not isinstance(value, str)` → `'节点筛选正则必须是字符串'` |
| H1083 / SUB | R154 | K51:25 | `捕获 re.error` → `f'节点筛选正则无效: {exc}'` |

### K52 [backend/utils/subscription_refresh.py](../../backend/utils/subscription_refresh.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1084 / SUB | R154 | K52:17 | `type(value) is not int or (value != 0 and (not 60 <= value <= MAX_INTERVAL))` → `f'{label}必须为 0（关闭自动更新）或 60 ~ {MAX_INTERVAL} 秒的整数'` |
| H1085 / SUB | R154 | K52:84 | `not proxies` → `'订阅没有返回任何节点，已保留原有缓存'` |
| H1086 / SUB | R154 | K52:110 | `捕获 ValueError` → `str(exc)` |
| H1087 / SUB | R154 | K52:124 | `not usable` → `f"订阅「{sub.get('name', sub['id'])}」已关闭自动更新，且没有当前订阅地址的可用缓存。请先在「订阅来源」中手动更新，再重新发布或同步。"` |

### K53 [backend/utils/web_listener.py](../../backend/utils/web_listener.py#L1)

| H / 组 | 驱动 | 源行 | 错误输入条件 → 结果 |
|---|---|---|---|
| H1088 / WEB | R206 | K53:36 | `type(value) is not int or not 1 <= value <= 65535` → `'Web 端口须为 1–65535 的整数'` |
| H1089 / WEB | R206 | K53:51 | `捕获 BlockingIOError` → `'Web 端口正在更新，请稍后重试'` |
| H1090 / WEB | R206 | K53:61 | `len(raw) > 5 or not raw.isascii() or (not raw.isdecimal())` → `f'{name} 须为 1–65535 的整数'` |
| H1091 / WEB | R206 | K53:64 | `not 1 <= value <= 65535` → `f'{name} 须为 1–65535 的整数'` |
| H1092 / WEB | R206 | K53:77 | `捕获 (OSError, ValueError, KeyError, TypeError, WebListenerError)` → `'无法读取已保存的 Web 端口；可设置 CONFIGFLOW_WEB_PORT 恢复访问'` |
| H1093 / WEB | R206 | K53:89 | `捕获 OSError` → `'无法检查 IPv6 监听能力'` |
| H1094 / WEB | R206 | K53:96 | `not first.startswith(MARKER)` → `ValueError()` |
| H1095 / WEB | R206 | K53:100 | `type(active['ipv6']) is not bool or not isinstance(active['token'], str)` → `ValueError()` |
| H1096 / WEB | R206 | K53:103 | `捕获 (OSError, IndexError, ValueError, KeyError, TypeError, WebListenerError)` → `'Web 监听配置尚未初始化，请重启容器'` |
| H1097 / WEB | R206 | K53:110 | `active['port'] != port` → `'Web 监听配置与保存的端口不一致，请重启容器'` |
| H1098 / WEB | R206 | K53:160 | `backend_host not in ('0.0.0.0', '127.0.0.1')` → `'Docker 的 CONFIGFLOW_BACKEND_HOST 须为 127.0.0.1 或 0.0.0.0'` |
| H1099 / WEB | R206 | K53:164 | `backend_port != 5001` → `'自定义后端端口需要更新容器镜像'` |
| H1100 / WEB | R206 | K53:169 | `template.count('__CONFIGFLOW_LISTEN__') != 1 or template.count('__CONFIGFLOW_BACKEND_PORT__') != 1 or '__CONFIGFLOW_LISTENER_TOKEN__' not in template` → `'Web 监听模板不完整，请更新容器镜像'` |
| H1101 / WEB | R206 | K53:186 | `捕获 (OSError, subprocess.TimeoutExpired)` → `'无法执行 Nginx 配置检查或重载'` |
| H1102 / WEB | R206 | K53:190 | `result.returncode` → `'Nginx 配置检查或重载失败，请查看容器日志'` |
| H1103 / WEB | R206 | K53:197 | `backend_port == sub_store_port` → `'ConfigFlow 后端与 Sub-Store 须使用不同的端口'` |
| H1104 / WEB | R206 | K53:200 | `port in (backend_port, sub_store_port)` → `f'端口 {port} 已保留给 {service}，请选择其他端口'` |
| H1105 / WEB | R206 | K53:216 | `error.errno == errno.EADDRINUSE` → `f'端口 {port} 已被占用，请选择其他端口'` |
| H1106 / WEB | R206 | K53:217 | `捕获 OSError` → `f'无法监听端口 {port}，请检查权限和网络配置'` |
| H1107 / WEB | R206 | K53:241 | `time.monotonic() >= deadline` → `'Nginx 未能在新端口提供服务，已尝试恢复原端口'` |
| H1108 / WEB | R206 | K53:248 | `not _supported()` → `'缺少 Nginx 或 Web 监听模板'` |
| H1109 / WEB | R206 | K53:267 | `not _supported()` → `'当前部署不支持在 Web 中修改监听端口'` |
| H1110 / WEB | R206 | K53:271 | `not status['editable']` → `status['message']` |
| H1111 / WEB | R206 | K53:294 | `捕获 Exception` → `'更新失败且无法确认原端口已恢复；请设置 CONFIGFLOW_WEB_PORT 并重建容器恢复访问'` |
| H1112 / WEB | R206 | K53:297 | `捕获 Exception` → `'保存 Web 端口失败，已恢复原配置'` |

## [O6] 横切补充用例（X01–X08）

这些是 Step A 原稿没有覆盖的全局行为。每个 X 不只测一条路由：在“适用范围”列出的每个代表路由上展开，计入该路由的用例数。来源都在 Python 应用壳层（after_request/before_request、服务器选项、启动代码），逐个 handler 读时看不到。

| 代码 | 输入与断言 | 适用范围 | 来源 |
|---|---|---|---|
| X01 全局 JSON 脱敏钩子 | 每个 JSON 响应都在 after_request 里重写：任何字符串只要含当前/退役 rule_proxy_token 或任一 Agent token（原文、`%` 编码、多重编码、`+` 形式），整串换成 `[REDACTED]`；任意深度的键 `rule_proxy_token`/`retired_rule_proxy_tokens`/`rule_fetch_proxy` 被删除；容器深度 ≥128 或成环时该子树变成 `[REDACTED]`；解码 RecursionError 时整个 body 变成 `"[REDACTED]"`。fixture 把这些能力值放进用户可控字段（节点名、规则值、订阅 URL query、组名、描述、OxiDNS native 文本），比较每个回显它们的响应。豁免逐端点比较：OxiDNS render 的 `config`；OxiDNS settings/editor/template-preview/common-settings/apply/native-settings/apply 的 native 字段与 `values`；`settings.handle_rule_fetch_proxy` 的 `rule_fetch_proxy`；artifact 的 `content`（仅 200 且 success=true）。走 `jsonify_external` 预脱敏的端点结果须与全局钩子一致。 | 每个蓝图至少一个回显用户文本的 JSON 路由；全部豁免端点（R106–R114、R139–R147、R062/R126、R201）；R027 desensitize=true/false；R125 | [profile_context.py:104](../../backend/common/profile_context.py#L104)、[config_export.py:194](../../backend/common/config_export.py#L194)、[json_response.py:61](../../backend/common/json_response.py#L61) |
| X02 profile 解析先于鉴权 | 启用登录、不带 Authorization：(a) 非全局端点 + 不存在的 profile（路径、`?profile=`、`?profile_id=`、`X-ConfigFlow-Profile`）→404 `Profile not found: <id>`；(b) 非法 id→400 `Invalid profile id`；(c) 同一请求换成存在的 profile→401；(d) 全局蓝图（settings、nodes、subscriptions、rule_library、无路径 id 的 profiles、tasks、notifications、logs）即使 header 指向不存在的 profile 也只回 401。前端每个请求都带 `X-ConfigFlow-Profile`，所以 (a) 是真实形态（配置空间被删后仍打开的旧标签页）。 | 每个非全局蓝图一个代表：rules、rule-sets、proxy-groups、custom-config、mosdns、oxidns、generate、stats、domain-discovery、config 拉取，以及 `/api/profiles/<id>/…` 别名 | [routes/__init__.py:113](../../backend/routes/__init__.py#L113)、[app.py:107](../../backend/app.py#L107)、[profile_context.py:93](../../backend/common/profile_context.py#L93) |
| X03 进程内调用头 | `X-ConfigFlow-Internal` 取随机值、空值、超长值：不得跳过全局钩子、`require_auth`、`validate_token_or_jwt`、rule-proxy 鉴权；拉取日志照常记录。Go 不应存在任何可由外部 HTTP 触发的同类旁路。 | 一个受保护路由、一个拉取路由、R087 | [internal_call.py:16](../../backend/common/internal_call.py#L16)、[auth.py:38](../../backend/routes/auth.py#L38) |
| X04 JSON 与数字解析 | Python `json.loads` 接受 `NaN`/`Infinity`/`-Infinity`、任意大整数、孤立代理项 `"\ud800"`、重复键（后者覆盖）；`60` 是 int，`60.0`/`6e1` 是 float，`true` 是 int 的子类（只写 `isinstance(x, int)` 的校验会接受 true）。Go 若解到 `map[string]any` 会丢掉 int/float 的区别。query/body 字符串经 `int()` 时接受首尾空白、`+`、`_`、Unicode 数字。Content-Type：`application/json; charset=utf-8`、`application/merge-patch+json`（Flask 视为 JSON）、`text/plain`（`get_json(silent=True)` 得 None；`request.json` 得 415）、空 body、UTF-8 BOM 前缀。每类在每个出现的校验器上各一例，期望以 Python 实际结果为准。 | `type(x) is int` 校验：R015–R017 保留份数、R226、R210 step、OxiDNS 区间（H511 等）；`int()`：R076、R047、R080/R090；字符串校验：R020、R197、R214；所有非 silent 的 `request.json` 入口 | [settings.py:191](../../backend/routes/settings.py#L191)、[subscription_refresh.py:16](../../backend/utils/subscription_refresh.py#L16)、[mosdns.py:69](../../backend/routes/mosdns.py#L69)、[logs.py:58](../../backend/routes/logs.py#L58) |
| X05 时区与时间格式 | 两端各以 `TZ=UTC` 与 `TZ=Asia/Shanghai` 启动（生产镜像的时区来源交 O14）。本地时间：订阅 proxies 头注释 `<本地时间>Z`、下载文件名时间戳、logs/info 的 `modified/created`、WebDAV 备份文件名与 keep_daily 的日期、日志行 asctime。带 Z 的 UTC：tasks `_iso`（秒精度）、stats `at`（微秒；微秒为 0 时 Python 不输出小数部分）。格式逐字符比，值只比窗口。 | R220/R154/R222、R075、R016、R225、R212、X07 | [subscriptions.py:433](../../backend/routes/subscriptions.py#L433)、[tasks.py:27](../../backend/routes/tasks.py#L27)、[stats.py:56](../../backend/routes/stats.py#L56) |
| X06 HTTP 壳 | HEAD：Flask 对每个 GET 路由自动接受 HEAD 并**执行 handler**（provider 会拉上游、写缓存与健康记录，stats 会写历史），只是不发 body，所以要比副作用与头。OPTIONS：自动 OPTIONS + flask-cors，分别带/不带 `Origin`、启用/关闭登录，比 status（全局 before_request 在 OPTIONS 上同样执行）、`Allow`、`Access-Control-Allow-*`、`Vary`。401/404/500 错误响应上的 CORS 头。未知路径/方法的 Werkzeug HTML 404/405；`/api/subscriptions/` 尾斜杠→404，不重定向；路径参数里的 `%2F`（Werkzeug 先解码再路由→404；Go ServeMux 按转义路径匹配，可能落进路由）。 | 每个蓝图一个 GET 代表 + 全部 provider/拉取路由；OPTIONS 取一个受保护、一个公开路由；`%2F` 取 R192、R153、R172、R150、R063 | [app.py:34](../../backend/app.py#L34) |
| X07 日志内容 | 固定请求序列：登录失败、带 token 的配置拉取、带密码的 WebDAV 失败、带 token URL 的订阅失败、导入配置空间。然后各端 `GET /api/logs/tail`：同一事件都在，等级词能被 `\b<LEVEL>\b` 过滤命中，中文事件文本一致，token、密码、订阅链接不出现。asctime/logger 名的格式差异记入 O12，不算通过也不算失败。另测 10MiB×5 轮转（Python RotatingFileHandler）。 | R074–R076 | [app.py:55](../../backend/app.py#L55)、[config.py:224](../../backend/routes/config.py#L224) |
| X08 启动与后台循环 | `python -m backend.app` 会启动：首个请求触发的 Agent rule-sync 循环、scheduled_refresh（30 s 后首轮，之后每 30 s；无缓存的启用订阅和 URL 规则源立即到期）、域名发现自动循环（120 s 后每 600 s）、routers rule-sync、OxiDNS 排行（15 min）、metrics flush、Sub-Store 监视器；模块导入时还执行 `prune_orphan_caches`（删除已删订阅留下的 `shared/subscribes/*.json`）和 `node_test_dns_cleanup.migrate`。Harness 规则：(a) 不想被后台刷新的 fixture 设 interval=0 或带新鲜缓存；(b) 超过 30 s 的用例必须预期或固定这些写入，不能事后 mask；(c) fake 记录要区分请求触发与循环触发；(d) 启动用例：种孤儿缓存与旧 `node_test_dns` 后两端分别启动，比文件与 DB（Go 未移植 migrate 的部分记入 O12）。 | 全部用例的环境前提；启动用例单列 | [app.py:111](../../backend/app.py#L111)、[app.py:138](../../backend/app.py#L138)、[app.py:182](../../backend/app.py#L182)、[scheduled_refresh.py:319](../../backend/utils/scheduled_refresh.py#L319) |

## 流式、长时间、轮询、上传下载与并发序列

本领域228条中没有 SSE 或 WebSocket 协议路由；`logs/tail` 是普通 GET 轮询。参考 [routes/logs.py:45](../../backend/routes/logs.py#L45)、[routes/domain_discovery.py:121](../../backend/routes/domain_discovery.py#L121)。下载响应本身能做请求/响应对比，但耗时回退、后台完成、压缩包内文件完整性与客户端取消必须用下列序列；不能只读一次200/202。下列 T 是额外命名序列，每项内列的分支在 Step B 继续参数化。

- **T01 身份全局开关**：管理员用户名和密码均非空、仅一项非空、两项均空三环境；每个入口重放成功样例，公开拉取另外设/不设config_token，internal capability从不越权。 来源 [backend/common/auth.py:37](../../backend/common/auth.py#L37)。
- **T02 profile并发隔离**：alpha/beta并发写不同规则与设置，查询/header/URL互相冲突；验证每个scope的DB行及生成provider URL，无请求间profile串线。 来源 [backend/common/profile_context.py:52](../../backend/common/profile_context.py#L52)。
- **T03 数据库并发与三方合并**：两个HTTP请求在下载fake屏障处同时读同baseline，分别改不同字段→保留两者，同字段→Python冲突类和status；另以外部SQLite连接更改commit元数据，写入必须拒绝且无半成品。 来源 [backend/common/config_repository.py:1372](../../backend/common/config_repository.py#L1372)。
- **T04 显式revision冲突**：OxiDNS GET ETag→请求A写入→请求B旧If-Match写409；rule-library草稿同revision并发仅一方成功；nodes/delete-preview→目标或引用改变→delete旧confirmation409。各端自己使用自己的validator，不mask后继续写。 来源 [backend/routes/oxidns.py:56](../../backend/routes/oxidns.py#L56)。
- **T05 取消连接后仍提交**：在配置写入、compose、订阅fetch、generate、provider后台、setup prepare分别设置fake屏障，关闭浏览器HTTP连接后放行；最终DB/文件/任务记录与不断连请求一致，未启动额外推送。 来源 [backend/common/config_repository.py:995](../../backend/common/config_repository.py#L995)。
- **T06 provider单飞与超时回退**：同provider/profile/config并发20请求，仅一次source fetch；在12秒阈值外挂起，已有last-good则及时200+X-ConfigFlow-Provider:last-good；放行后最后文件更新。没有last-good时等待完成；format=surge/loon不走旧内容回退。 来源 [backend/utils/provider_response.py:68](../../backend/utils/provider_response.py#L68)。
- **T07 provider键隔离**：同id不同profile、format、regex/exclude_regex、config digest应各自flight；认证/不存在/禁用先拒绝，不能借旧内容绕过；旧flight完成不能删掉新flight。 来源 [backend/utils/provider_response.py:70](../../backend/utils/provider_response.py#L70)。
- **T08 ZIP与冻结artifact**：五种service分别generate artifact→file逐成员→download解包；原始配置改变后读取仍为同份冻结文件（manual读取不调用require_current）；无部署/无Agent推送；坏metadata/hash/过期/被驱逐均409，无重新渲染降级。 来源 [backend/agents/generated_preview.py:107](../../backend/agents/generated_preview.py#L107)。
- **T09 上传/导入边界**：config/import与profiles/import-client为JSON body而非multipart；逐类INI/YAML/旧完整备份/当前schema导入；大文本、空对象、坏JSON、Content-Type错误；dry_run全库不变，实导入后restart再GET验证。 来源 [backend/routes/profiles.py:40](../../backend/routes/profiles.py#L40)。
- **T10 长扫描与日志轮询**：duplicate scan挂慢源后取消客户端，结果/缓存仍完成；logs/tail以固定logfile轮询，每次检查tail/filter/total，轮换/截断/无文件；本领域没有SSE/WebSocket路由，不能用Agent日志接口冒充覆盖。 来源 [backend/routes/logs.py:45](../../backend/routes/logs.py#L45)。
- **T11 Sub-Store更新生命周期**：start202→dependencies refresh轮询running→success/failed；假release下载短文件、SHA错、JS语法错、重启未就绪、回滚成功、回滚失败各一子fixture；检查runtime/VERSION/备份清理，第二次启动409且无双worker。 来源 [backend/utils/dependencies.py:273](../../backend/utils/dependencies.py#L273)。
- **T12 Sub-Store按需进程**：on_demand第一次转换起进程、请求结束可闲置停机；always保持；maintenance遇活动租约拒绝；不可用服务命令、未就绪与外部mode保留错误状态。只临时进程或容器。 来源 [backend/utils/sub_store_runtime.py:61](../../backend/utils/sub_store_runtime.py#L61)。
- **T13 节点probe子进程**：fixture可执行文件记录stdin后给定JSON：正常、非0退出、超时、协议版本错、JSON错、少/错result项；20节点与5转换节点上限，同名必须keys；并发busy409后锁可恢复。真实代理RTT不以合成结果宣称通过。 来源 [backend/utils/node_latency.py:129](../../backend/utils/node_latency.py#L129)。
- **T14 DNS probe与设置**：system/custom，IPv4/端口、DoH/DoT带引导DNS；fake DNS/worker逐返回结果，4服务器与5服务器边界、2048字符与2049、无bootstrap、credential/query/fragment/IPv6拒绝。只loopback不对公共DNS查询。 来源 [backend/utils/node_dns.py:26](../../backend/utils/node_dns.py#L26)。
- **T15 WebDAV操作与保留**：回环DAV逐请求比认证/路径/上传JSON；MKCOL中间失败停止；上传失败不清旧文件；prune列表失败和逐DELETE失败只log、不把成功上传报失败；keep_latest/keep_daily并集，非备份文件绝不删除。 来源 [backend/utils/webdav_backup.py:135](../../backend/utils/webdav_backup.py#L135)。
- **T16 自动备份提交钩子**：auto_backup=true时改变system/shared/profile各提交一例并等去抖，只上传最终快照；短时间多次写合并；no-op/失败事务/runtime-only Agent更新不触发；待上传期间改配置再上传正确快照；禁用后不再上传。 来源 [backend/utils/webdav_backup.py:158](../../backend/utils/webdav_backup.py#L158)。
- **T17 自动备份失败与恢复**：DAV故障或worker启动失败不撤销已成功配置提交；记录task-status/log，不能无限重试；后续新配置提交可再次触发；各端最终备份状态/计数相符。 来源 [backend/utils/webdav_backup.py:219](../../backend/utils/webdav_backup.py#L219)。
- **T18 定时任务触发与轮询**：run202→tasks.running→task-status success/failed，每个category用fake；同key冲突409，错误不吞且锁释放；interval不篡改资源其他字段。不同category不能借别的任务回执冒充完成。 来源 [backend/routes/tasks.py:278](../../backend/routes/tasks.py#L278)。
- **T19 域名job进度**：probe/website/region start202→probe/<id> running→done/failed；done<=total单调；失败保留message/errors/finished_at；跨profile busy不回泄job，跨profile GET404；进程重启job消失404。 来源 [backend/utils/domain_discovery_service.py:860](../../backend/utils/domain_discovery_service.py#L860)。
- **T20 域名fake Agent与部分失败**：版本/启用/域名发现开关筛选，批次18+2对照、3 hosts、3 attempts；region每批12请求，website最多30域名；一个Agent/target失败保留其他成功，probes/regions文件与最终摘要对照。 来源 [backend/utils/domain_discovery_service.py:765](../../backend/utils/domain_discovery_service.py#L765)。
- **T21 域名自动化**：通过tasks/run触发auto_probe，阈值80/90/95、忽略/已覆盖/失败样本、3天重探与30天复检；fake响应固定，自动apply只能写指定规则库和当前profile的history；人工apply/undo并发不丢规则。 来源 [backend/utils/domain_discovery_service.py:926](../../backend/utils/domain_discovery_service.py#L926)。
- **T22 setup完整状态机**：create→source→prepare→fake安装检查→router-draft/check→finish，router/gateway各一例；故障逐步重试不重复创建资源；外部改核心配置产生409，单纯心跳/router回执不当作核心改动；finish前静态路由/DNS确认与managed差异报告门禁逐项。 来源 [backend/common/setup_completion.py:131](../../backend/common/setup_completion.py#L131)。
- **T23 setup放弃与竞争**：无占用删除专属profile和自建订阅；分别有Agent/router/installation/default时只标abandoned；dry_run不写；plan后产生占用时保留；订阅被另一profile引用时subscription_kept=true；继续PATCH重新打开。 来源 [backend/common/setup_abandon.py:22](../../backend/common/setup_abandon.py#L22)。
- **T24 监听端口迁移**：隔离临时部署中改端口后探活新端口，旧端口不再接新连接；占用/保留/锁竞争；写/检查/重载/新端口探活失败要恢复旧端口。Python Nginx-specific分支与Go原生监听需分别记录，不能把两个不同环境的503判成功。 来源 [backend/utils/web_listener.py:262](../../backend/utils/web_listener.py#L262)。
- **T25 来源图与发布副作用**：规则/组/库改动可能重建native DNS、改变pinned_withheld/content-sync失效；loopback Agent记录所有请求，保存/预览只产生Python实际会发的查询，不能偷发push/activate；后续agents rule-sync的真实效果留跨域联测。 来源 [backend/routes/rules.py:172](../../backend/routes/rules.py#L172)。
- **T26 导入与原子落盘故障**：read-only/full-disk/rename/fsync/SQLite外部commit故障分别检查事务与缓存文件，确定是否已提交后再返500/503，不假定所有500均无写入；create-rule不确定状态必须confirmation_required，compose缓存失败必须DB成功+warning。 来源 [backend/routes/rule_library.py:135](../../backend/routes/rule_library.py#L135)。

- **T27 [O6] 定时刷新调度**：种 task-status（`last_run_at` 很早；`last_status=failed` + `consecutive_failures=n`）和缓存（新鲜、过期、metadata.url 不匹配），等一个 tick（≤60 s）：只有到期的才拉取；失败退避 `300·2^(n-1)` 秒且不超过 interval；interval=0、订阅停用、规则源不是 URL 时从不拉取；与手动 run 同 key 冲突时只跑一份。跨进程 flock（`.task-scheduler.lock`）无法用两个 DATA_DIR 做差分，改为 Go 单元测试。 来源 [backend/utils/scheduled_refresh.py:107](../../backend/utils/scheduled_refresh.py#L107)、[backend/utils/scheduled_refresh.py:233](../../backend/utils/scheduled_refresh.py#L233)。
- **T28 [O6] rule_proxy_token 生命周期**：种 `config_token == rule_proxy_token` 的旧数据→启动时轮换，旧值进 retired；import、reset 各轮换一次；retired 去重；rule-proxy 接受当前值、拒绝退役值（关闭登录也拒绝）；新生成的 MosDNS/OxiDNS 内嵌当前值；config_token 不得包含当前或退役值。所有随机值走双射。 来源 [backend/common/config_repository.py:1767](../../backend/common/config_repository.py#L1767)。
- **T29 [O6] JWT 兼容**：两端设同一 `JWT_SECRET_KEY`：Python 登录签发的 token 在 Go 上通过，反之亦然（切换后会话不中断）；无 `exp` 的 token（PyJWT 默认不要求 exp）、`exp`/`iat` 为字符串、`nbf`/`iat` 在未来、`alg=none`、HS512、RS256、用别的密钥签名，按 PyJWT 2.8.0 实际结果比较；不设 `JWT_SECRET_KEY` 时每个进程随机，重启后旧 token 得 401。 来源 [backend/common/auth.py:13](../../backend/common/auth.py#L13)、[backend/common/auth.py:56](../../backend/common/auth.py#L56)。
- **T30 [O6] publish_guard 链**：custom-config mihomo/surge/mosdns、mosdns 的 rulesets/api/cache 设置、OxiDNS settings 保存后返回的 `publish_guard.expected_config_revision` 等于此后的 `config_revision`；再保存一次，旧 guard 即过期。config 侧只比 guard 与 revision 的关系；拿过期 guard 去 Agent 发布被拒，属于 agents 域差分，这里登记为跨域依赖。 来源 [backend/common/config.py:236](../../backend/common/config.py#L236)、[backend/routes/mosdns.py:112](../../backend/routes/mosdns.py#L112)。

所有配置写操作（包括下表 SYS/SHARED/PROFILE、导入、重置、改绑、setup）均继承 T16/T17 自动 WebDAV 备份观察：提交成功后且 `auto_backup` 开启、业务配置发生变化才安排，不由接口名称推断。应比较 event/manual trigger 与最终快照，配置失败不得安排备份。来源 [config_repository.py:1006](../../backend/common/config_repository.py#L1006)。大多数保存草稿不主动 Agent 推送；future rule-sync 的设备行为与保存HTTP响应是两个边界。

## 生成器成功形态补充

这些 G 场景通过主配置下载/preview/provider公开入口，每种目标分别执行，目标不支持的节点按Python保留warning/省略或拒绝。不直接调用任何Go生成器。文本格式不按空白字节判负，但字段/规则顺序与同名冲突必须比较。语法与来源是Python；不从Go成功结果反推fixture。

- **G01 节点三入口**：结构化node、proxy_string JSON、proxy_string YAML各独立；外层名覆盖内层名。来源 [backend/converters/mihomo.py:1112](../../backend/converters/mihomo.py#L1112)。
- **G02 URI/base64入口**：ss/ssr/vmess/vless/trojan/hysteria/hysteria2逐协议一个合成节点；Sub-Store fake转同一扁平结构。来源 [backend/utils/subscription_parser.py:61](../../backend/utils/subscription_parser.py#L61)。
- **G03 Surge节点**：ss、vmess、trojan、hysteria2、http、https、snell、tuic、tuic-v5、vless、anytls、wireguard/wg逐类型一例，包含TLS/WS等分支。来源 [backend/converters/surge.py:532](../../backend/converters/surge.py#L532)。
- **G04 Loon节点**：ss、ssr、vmess、vless、trojan、hysteria2、anytls、http/https/socks5、wireguard/wg逐类型一例。来源 [backend/converters/loon.py:604](../../backend/converters/loon.py#L604)。
- **G05 组类型**：select、url-test、fallback、load-balance、follow、chain分别选择有效来源，健康参数/过滤/排序/include联动。来源 [backend/utils/group_candidates.py:82](../../backend/utils/group_candidates.py#L82)。
- **G06 规则类型**：Python RULE_TYPES每个一个合法典型值；DOMAIN与中文IDNA、IP-CIDR4/6、端口列表、REGEX、AND/OR/NOT、MATCH的UDP/NTP。来源 [backend/utils/rule_syntax.py:179](../../backend/utils/rule_syntax.py#L179)。
- **G07 规则源格式**：domain/ipcidr/classical × text/yaml/mrs合法组合；MosDNS/OxiDNS不支持MRS要422/归档错误，不伪造成功。来源 [backend/utils/ruleset_content.py:1](../../backend/utils/ruleset_content.py#L1)。
- **G08 Mihomo DNS与EasyTier**：IPv6布尔、proxy-server-nameserver、hosts、EasyTier scope=subscription/agent/all，保活开关与代理链名引用。来源 [backend/common/mihomo_easytier.py:174](../../backend/common/mihomo_easytier.py#L174)。
- **G09 MosDNS转换**：Clash/List/YAML/原生mosdns，domain/ip/混合三种part，full/domain/keyword/regexp/IP-CIDR6逐项，正则逗号负载原样。来源 [backend/routes/mosdns.py:492](../../backend/routes/mosdns.py#L492)。
- **G10 OxiDNS模式**：template/native及include多文件，additional_files text/base64，插件引用、下载闭包和state运行文件隔离。来源 [backend/common/oxidns_native.py:143](../../backend/common/oxidns_native.py#L143)。
- **G11 OxiDNS通用控件**：缓存/API/log/查询记录/IP选择/动态域名/AdGuard/ROS地址表/ROS路由/rule_update逐项启用与关闭；启用时每项合法defaults与自定义参数。来源 [backend/common/oxidns_settings.py:115](../../backend/common/oxidns_settings.py#L115)。
- **G12 OxiDNS草稿编辑**：模板baseline三方合并、resolutions、rename、标准插件新增/删除、设备DNS策略与备注的无损往返；保存/渲染/导出的脱敏豁免范围不同。来源 [backend/common/oxidns_native_apply.py:341](../../backend/common/oxidns_native_apply.py#L341)。
- **G13 订阅交付图**：多个Provider、跨源拨号、过滤前原始缓存、同名不同源、hosts metadata以及来源interval新鲜/过期/0的四类输出。来源 [backend/utils/provider_delivery.py:175](../../backend/utils/provider_delivery.py#L175)。
- **G14 恢复备份**：当前schema、旧单配置、schema2完整多profile分别导入后导出；不移植pre-SQLite启动迁移，但保留这些用户导入能力。来源 [backend/common/config_repository.py:2185](../../backend/common/config_repository.py#L2185)。

- **G15 [O6] 生成物里的地址与能力**：每种输出（Mihomo rule-providers/proxy-providers、Surge/Loon RULE-SET 与 policy-path、MosDNS/OxiDNS 规则下载 URL）分别在 server_domain 已设/未设、请求 base_url 显式/省略、X-Forwarded-* 有/无、config_token 有/无、默认/显式 profile 下生成；比较 URL 的主机、路径（`/api/profiles/<id>/…` 还是默认路径），以及嵌入的是 config_token 还是 rule_proxy_token（双射后逐字节）。 来源 [backend/common/profile_context.py:22](../../backend/common/profile_context.py#L22)、[backend/converters/mosdns.py:197](../../backend/converters/mosdns.py#L197)。
- **G16 [O6] 订阅 hosts 报告**：订阅缓存 metadata 带 hosts 时，Mihomo 主配置的 hosts / proxy-server-nameserver-policy 输出，以及 artifact 的 `subscription_hosts` 报告（`dns.applied`、`outdated_agents`）；无 hosts、hosts 与节点名冲突、Agent 版本未知各一例。 来源 [backend/routes/generate.py:24](../../backend/routes/generate.py#L24)。

## 前端调用核对与框架差距

前端 `api/index.ts` 的订阅/节点/规则/组/生成/profile/OxiDNS入口均已按上表默认与显式profile路由展开，`If-Match`来自GET的ETag；artifact按id读文件和下载。来源 [api/index.ts:104](../../frontend/src/api/index.ts#L104)、[api/index.ts:233](../../frontend/src/api/index.ts#L233)、[api/index.ts:259](../../frontend/src/api/index.ts#L259)、[api/index.ts:386](../../frontend/src/api/index.ts#L386)。setup明确把source/prepare/check/router-draft/abandon/finish作为同一operation模板的不同用例，90秒等待不等于无后台效果；见 [api/setup.ts:51](../../frontend/src/api/setup.ts#L51)。通知已读有version，不以ID一概标读，见 [api/notifications.ts:28](../../frontend/src/api/notifications.ts#L28)。

**[O6] 前端调用补充**：(1) 请求拦截器给**每个**请求加 `X-ConfigFlow-Profile`（[api/index.ts:54](../../frontend/src/api/index.ts#L54)），包括全局端点，所以默认用例就要带这个头，X02 的“未登录 + 已删除 profile”是常见形态；(2) 响应拦截器按 `Profile not found: <id>` 整串判断是否恢复配置空间，按 401 且 token 未变判断登出；(3) OxiDNS 设置页读响应头 `etag` 并在 409 时看 `code === 'oxidns_changed_elsewhere'`（[OxidnsSettings.vue:420](../../frontend/src/components/generate/OxidnsSettings.vue#L420)）；(4) 生成页下载用 `responseType: 'blob'`，失败时从 blob 解析 JSON 的 `message`（[Generate.vue:1546](../../frontend/src/views/Generate.vue#L1546)），所以下载端点的错误体必须是 JSON 且带 message；(5) 依赖更新调用 `/settings/dependencies/${key}/update`（[api/index.ts:785](../../frontend/src/api/index.ts#L785)），Python 只有 `sub-store`，其他 key 是 Werkzeug 404。

已有 `internal/httpx/difftest/diff_test.go` 可复用双进程启动与身份请求结构，但现有短请求/大范围mask不能覆盖这里的DB文件、后台工作和多阶段读写；Step B需加公开HTTP序列、SQLite/目录观察和loopback fake记录，保持业务黑盒。`tools/proddiff/README.md` 的生产数据只读离线回归是补充，不替代本清单写入/并发/新对象成功路径；这一步不读取快照。最终比较器还需故意篡改status、JSON叶子、YAML叶子、文本规则行和DB行各一次，证明可失败，不能以宽泛normalization放过。

## 汇总与部分覆盖

- 路由总数：**228**；每条一行、无重复method/template。
- 路由命名场景：**4418**（S 702、F/降级 2763、B 953），A/D/P已按适用路由展开。
- 补充：**1112个H源码分支配方**（含不可HTTP到达的审计哨兵，不等于已可执行测试）、**26个T序列**、**14个G生成形态组**。合计清单条目 **5570**。循环字段、协议与子fixture展开后请求数更多，须在Step B报告真实执行/跳过数，不能用本设计数代替。
- 普通请求/响应框架只能部分覆盖的路由 **45条**，逐条如下；T/H给出补充计划。这里的“部分”表示当前框架能力，不是免测：扩展后应全测假环境可达分支；真实设备、安装成功、实际网络性能、Python特有Nginx流程和不可控OS故障仍不能据此宣称完整。

| 路由ID | 路由 | 部分覆盖原因与补充方法 |
|---|---|---|
| R007 | `GET /api/aggregations/<agg_id>/preview` | 同步生成provider并落盘，远端下载与原子图发布不能靠响应证明；T05/T07和目录记录。此入口不经过serve_provider，不能假定有12秒回退。 |
| R008 | `GET /api/aggregations/<agg_id>/provider` | 12秒fallback后后台继续/单飞/原子图发布；T06/T07和目录记录，单响应不能证明最终文件或并发隔离。 |
| R016 | `POST /api/backup/now` | 远端目录/上传内容/清理/备份task；T15–T17回环DAV，真实WebDAV实现兼容另做跨域联测。 |
| R017 | `POST /api/backup/test` | 远端目录/上传内容/清理/备份task；T15–T17回环DAV，真实WebDAV实现兼容另做跨域联测。 |
| R045 | `GET /api/domain-discovery/agents` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 **[O6] 前提**：Agent 在线状态与版本只在内存（心跳），种 DB 不够；fake Agent 先向两端发相同心跳，并在登记地址上提供 Agent API 供探测请求记录。 |
| R047 | `GET /api/domain-discovery/domains` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 |
| R048 | `GET /api/domain-discovery/history` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 |
| R051 | `POST /api/domain-discovery/probe` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 **[O6]** 同 R045 的心跳前提。 |
| R052 | `GET /api/domain-discovery/probe/<job_id>` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 |
| R053 | `GET /api/domain-discovery/region` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 |
| R054 | `POST /api/domain-discovery/region/check` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 **[O6]** 同 R045 的心跳前提。 |
| R060 | `POST /api/domain-discovery/website/identify` | 内存job/进度、Agent流量/版本/探测和后台落盘；T19–T21 fake与重启，真实服务可达性另验。 **[O6]** 同 R045 的心跳前提。 |
| R062 | `POST /api/generate/<service>/artifact` | 生成到冻结预览、TTL/容量/逐文件读取；T08观察私有文件；极限512MiB/OS故障需服务器限额fixture，单响应不代表覆盖。 **[O6] 可执行化**：TTL 3600 s 与 24 份/512MiB 淘汰不靠等待或真写 512MiB——在请求之间直接改 DATA_DIR `.generated-previews/<id>/record.json` 的过期时间、预置 24 份记录、用稀疏大文件撑容量；两端同样操作。 |
| R063 | `GET /api/generate/artifacts/<preview_id>/<action>` | 生成到冻结预览、TTL/容量/逐文件读取；T08观察私有文件；极限512MiB/OS故障需服务器限额fixture，单响应不代表覆盖。 **[O6]** 同 R062：过期/坏 metadata/hash 错通过直接改两端预览目录文件构造。 |
| R074 | `POST /api/logs/clear` | 轮询/rotation/写权限与请求自身日志影响；T10固定logfile，真实运行日志来源差异需逐项审阅而非全mask。 |
| R075 | `GET /api/logs/info` | 轮询/rotation/写权限与请求自身日志影响；T10固定logfile，真实运行日志来源差异需逐项审阅而非全mask。 |
| R076 | `GET /api/logs/tail` | 轮询/rotation/写权限与请求自身日志影响；T10固定logfile，真实运行日志来源差异需逐项审阅而非全mask。 |
| R087 | `GET /api/mosdns/rule-proxy` | 流式上游下载、大小限制、重定向与stale缓存；T15类回环分块fake+H-FETCH，真实外网不可测。 |
| R097 | `GET /api/nodes/latency` | 响应只读最近结果，需先驱动测速再核对持久history和旧method过滤；T13，真实代理RTT不由合成结果覆盖。 **[O6] 前提**：两端都必须认 `CONFIGFLOW_NODE_PROBE_BIN`（[node_latency.py:117](../../backend/utils/node_latency.py#L117)）；若 Go 改为进程内测速，则 T13 不是差分，改记 O12 并另写 Go 测试。 |
| R098 | `POST /api/nodes/latency` | 子进程、超时、锁、结果持久化；T13/T14 fake完整协议，真实代理链网络RTT与DNS不由此覆盖。 **[O6]** 同 R097。 |
| R099 | `POST /api/nodes/latency/dns/check` | 子进程、超时、锁、结果持久化；T13/T14 fake完整协议，真实代理链网络RTT与DNS不由此覆盖。 **[O6]** 同 R097。 |
| R102 | `POST /api/nodes/parse-lines` | 逐行调用Sub-Store转换，HTTP200还可能有results[].error；需fake验证行序、超时/转换失败与并发服务租约，不是node-probe测速。 **[O6]** 两端同一 `SUB_STORE_URL` 指向回环 fake（[sub_store_client.py:72](../../backend/utils/sub_store_client.py#L72)）。 |
| R104 | `GET /api/notifications` | 通知聚合依赖agents/routers任务源；本域可测合成事件/已读版本，跨域源的真实生命周期另验。 |
| R121 | `POST /api/profiles/<profile_id>/agents/rebind` | agents部署/升级/HA锁与绑定并发；REBIND源guard+T03/T25，真实在途部署跨域联验。 **[O6]** 同 R045 的心跳前提；Agent 记录用 Python 备份导入种入，不依赖 Go agents 路由是否已移植。 |
| R122 | `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider` | 12秒fallback后后台继续/单飞/原子图发布；T06/T07和目录记录，单响应不能证明最终文件或并发隔离。 |
| R126 | `POST /api/profiles/<profile_id>/generate/<service>/artifact` | 生成到冻结预览、TTL/容量/逐文件读取；T08观察私有文件；极限512MiB/OS故障需服务器限额fixture，单响应不代表覆盖。 **[O6]** 同 R062。 |
| R138 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 流式上游下载、大小限制、重定向与stale缓存；T15类回环分块fake+H-FETCH，真实外网不可测。 |
| R154 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | 12秒fallback后后台继续/单飞/原子图发布；T06/T07和目录记录，单响应不能证明最终文件或并发隔离。 |
| R190 | `POST /api/rules/find-duplicates` | 长源下载、取消与失败源；T10和目录观察，大规模压力另验。 |
| R198 | `GET /api/settings/dependencies` | 更新后台状态/本地bundle/真实进程不在单次响应；T11/T12 fake与临时容器；真实第三方release不可用于稳定断言。 **[O6] 执行前提**：release 查询硬编码 `https://api.github.com/…`（[dependencies.py:149](../../backend/utils/dependencies.py#L149)），只经 `rule_fetch_proxy` 代理；要做差分必须用回环 MITM 代理 + 测试 CA（Python `REQUESTS_CA_BUNDLE`，Go `SSL_CERT_FILE`），并设 `SUB_STORE_BUILTIN_DIR` 为临时目录。做不到就只测 409/外部服务分支，成功更新路径移到 Go 单元测试 + 容器实验。进程内 `_latest_cache` 有 TTL，每例重启进程。 |
| R199 | `POST /api/settings/dependencies/sub-store/mode` | 更新后台状态/本地bundle/真实进程不在单次响应；T11/T12 fake与临时容器；真实第三方release不可用于稳定断言。 **[O6]** 运行模式切换要真实 node 进程；harness 无内置 Sub-Store 时只有 RuntimeUnavailable→409 可差分，其余进容器实验。 |
| R200 | `POST /api/settings/dependencies/sub-store/update` | 更新后台状态/本地bundle/真实进程不在单次响应；T11/T12 fake与临时容器；真实第三方release不可用于稳定断言。 **[O6]** 同 R198 的 MITM 前提；否则只差分 409 分支。 |
| R205 | `GET /api/settings/web-port` | 监听/部署环境，响应不足证明新端口及回滚；T24，Nginx专有流程仅Python可测，Go原生服务另验，差异交Opus裁决。 **[O6] 计划不可执行，改分流**：差分 harness 用 `python -m backend.app` 启动，没有 Nginx/模板，Python 只能走“不支持/环境变量端口”分支（H1108–H1110），T24 的“改端口并探活”在 Python 侧根本到不了；Go 用原生监听，机制不同。HTTP 差分只做校验与不支持分支；真正的改端口/回滚 → Go 集成测试 + .32 容器实验（L 批次）；Nginx 流程退役记入 O12。 |
| R206 | `POST /api/settings/web-port` | 监听/部署环境，响应不足证明新端口及回滚；T24，Nginx专有流程仅Python可测，Go原生服务另验，差异交Opus裁决。 **[O6] 同 R205**：F13–F19 的 Nginx 检查/重载/回滚分支只在 Python Docker 镜像里可达，不进差分；Go 的端口占用/保留/回滚由 Go 集成测试覆盖，差异由 O12 登记。 |
| R207 | `GET /api/setup` | 跨多请求的安装/检查/路由器发布状态机；T22/T23 fake与持久记录；真实安装及设备生效只能由实验网验收。 |
| R208 | `POST /api/setup` | 跨多请求的安装/检查/路由器发布状态机；T22/T23 fake与持久记录；真实安装及设备生效只能由实验网验收。 |
| R209 | `GET /api/setup/<session_id>` | 跨多请求的安装/检查/路由器发布状态机；T22/T23 fake与持久记录；真实安装及设备生效只能由实验网验收。 |
| R210 | `PATCH /api/setup/<session_id>` | 跨多请求的安装/检查/路由器发布状态机；T22/T23 fake与持久记录；真实安装及设备生效只能由实验网验收。 |
| R211 | `POST /api/setup/<session_id>/<operation>` | 跨多请求的安装/检查/路由器发布状态机；T22/T23 fake与持久记录；真实安装及设备生效只能由实验网验收。 |
| R218 | `POST /api/subscriptions/<sub_id>/fetch` | 同步请求可能长拉取/缓存降级与取消；T05/T09、SUB文件观察，Sub-Store真实协议转换另以服务fixture补验。 |
| R220 | `GET /api/subscriptions/<sub_id>/proxies` | 12秒fallback后后台继续/单飞/原子图发布；T06/T07和目录记录，单响应不能证明最终文件或并发隔离。 |
| R222 | `GET /api/subscriptions/proxies` | 同步合并多个订阅，下载失败降级、跨feed引用验证与延迟缓存提交需fake和目录观察；不经过serve_provider，没有12秒回退。 |
| R225 | `GET /api/tasks` | 聚合跨域Agent/router/metrics任务状态及next_run_at；interval还有metrics全局设置边界。T18用fake和存储观察，真实跨域生命周期另验。 |
| R226 | `PUT /api/tasks/interval` | 聚合跨域Agent/router/metrics任务状态及next_run_at；interval还有metrics全局设置边界。T18用fake和存储观察，真实跨域生命周期另验。 |
| R227 | `POST /api/tasks/run` | 202只代表启动，含跨域router/Agent/metrics工作；T18轮询+fake，跨域真实操作不计完成覆盖。 |

其余普通路由的业务输入/输出和DB副作用可在公开HTTP+seed副本上对比；全局OS故障（磁盘耗尽/fsync结果不确定/进程创建失败/SQLite启动损坏）、H中的纯内部参数哨兵仍是全体写入路径的部分覆盖限制，不因未列在上表就认为已穷尽。这些项在Opus审核时决定可做的隔离故障注入和明确N/A；先确认Python分支可达，再实现Step B，不派子agent、不修改Go实现。

本次没有运行任何测试、构建、lint或dev server。仅核对文档路由集合、场景编号、源码行链接、git diff。待Opus审过本清单后再写测试代码。

## O6 review

Reviewer: Opus (O6, 2026-11-17). Basis: Python source on this branch (`backend/`), `routes.tsv`, `FILES.md`, `frontend/src/api` and the frontend components that read response fields. No Go implementation code was read. Nothing was run; this is a document review.

### Verdict

**Approved with additions.** Step B may start from this matrix as amended. The per-handler work is thorough: almost every `return …, 4xx/5xx` in the 19 config route files is referenced by a row, and the H recipes match their source lines. What the original missed sits outside the handlers — in the Flask application shell (before/after-request hooks, server options, startup code) and in background loops — plus one wrong precedence rule that would have made the authentication rows fail for the wrong reason. Those are now X01–X08, T27–T30, G15–G16 and the corrections below.

One general point for Step B: the expected statuses and messages written in the rows are hints. The oracle is the Python response, so a row whose expected status is wrong corrects itself in a differential run. What cannot correct itself is a scenario that is never sent. The additions below are all of that kind.

### Most important misses (now added)

1. **Precedence of profile resolution over authentication was stated backwards** (corrected rule, X02, F90 on 20 routes). `install_profile_context` is registered inside `register_blueprints`, before `setup_before_request`, so on every non-global endpoint an unauthenticated request with a missing or invalid profile gets 404/400, not 401. The frontend sends `X-ConfigFlow-Profile` on every request, so this is the normal shape after a profile is deleted.
2. **Global JSON redaction hook** (X01). Every JSON response is rewritten after the handler: strings containing any current or retired rule-proxy token or any Agent token (including URL-encoded forms) become `[REDACTED]`, three key names are removed at any depth, and there are endpoint-specific exemptions. The original only mentioned redaction for a few rows.
3. **Background loops started by `python -m backend.app`** (X08, T27). The subscription and rule-source scheduler fetches any enabled source without a cache 30 s after start; a first request also starts the Agent rule-sync loop. Without explicit harness rules these produce writes and fake-server requests that look like differences or get masked. The scheduler itself (due time, retry back-off, interval 0) had no test.
4. **Authentication environment for public pulls** (D-row correction, T29). "D1–D7 all 401" holds only with login on or a config token set. With both off, D1/D2/D3/D6/D7 succeed and only the internal capability is still refused. JWT compatibility across the switch (same `JWT_SECRET_KEY`, token without `exp`, foreign algorithms) was not covered.
5. **Python parsing leniency as a class** (X04). `type(x) is int` vs float `60.0`, `true` as int, `NaN`/`Infinity`/huge integers accepted by `json.loads`, `int()` accepting spaces/underscores/Unicode digits, `+json` content types. A Go decoder into `map[string]any` loses exactly these distinctions.
6. **Rule-proxy capability lifecycle** (T28, S90 on R028/R033). Import and reset rotate `rule_proxy_token` and retire the old one; previously generated MosDNS/OxiDNS configs then get 401 from rule-proxy.
7. **HTTP shell behaviour** (X06, B90/B91 on pulls, providers and downloads). HEAD runs the GET handler including its side effects; OPTIONS and CORS on error responses; `send_file` headers (Range/conditional requests); `%2F` in path parameters routes differently in Werkzeug and Go's ServeMux.
8. **Frontend contracts** (rule correction, B90 on OxiDNS settings). `Profile not found: <id>` and the `code` fields are parsed by the frontend and must be byte-equal; the OxiDNS ETag is deterministic and must equal Python's, not merely be self-consistent.
9. **Time zone** (X05). Several outputs use local time (one even appends a literal `Z` to local time); none of the rows fixed `TZ`.
10. **Executability of partial rows.** Web-port switching (R205/R206) cannot run on the Python side of the harness at all (no Nginx); the Sub-Store update (R198–R200) talks to a hard-coded GitHub API; Agent online state is memory-only, so seeding the DB is not enough. Each now names what it needs or where it moves.

### Counts

Per-route additions (112 cases on 65 routes, all tagged `[O6]`, numbered S/F/B 90+ so existing numbers stay stable):

| Category | Added | Main items |
|---|---|---|
| Authentication / permissions | 21 | F90 profile-before-auth on 11 pull routes and 9 `/api/profiles/<id>` routes; R013 JWT compatibility |
| HTTP shell and headers | 43 | HEAD on pulls (11) and providers (5); X-Forwarded/Host shapes (11); Loon filename (3); pull-log edge (1); `send_file` headers on downloads/export (11); version source (1) |
| Input parsing and validation | 14 | `int()`/`type is int` cases on R047, R076, R080, R090, R226; chunked body vs 413 on setup (3); If-Match lists (2); task keys (3); setup `check`/`url_root` (1) |
| Side effects after writes and background work | 11 | auto-backup on enabling auto_backup; failed WebDAV task run; capability rotation on import/reset (2) and config-token (1); backup date under TZ; log content and log-file fallback (5) |
| Generated outputs and frontend contracts | 11 | artifact reports and redaction exemption (4); OxiDNS ETag equality (4); provider header timestamps under TZ (3) |
| External services and long-running endpoints | 12 | rule-proxy stale cache, GitHub proxy rewrite, `source_id`, local-content fallback (8); WebDAV request sequence (3); Sub-Store fake for parse-lines (1) |

Cross-cutting additions: **15** — X01–X08, T27–T30, G15–G16, D8.

Corrections: **27**

| What | Count |
|---|---|
| Comparison rules: precedence of auth vs profile (wrong), D1–D7 environment ("all 401" wrong in one environment), ETag self-consistency too weak, English-message rule missing frontend-parsed strings | 4 |
| Write-code: R087/R138 do not commit configuration, so AUTO(T16/17) must assert *no* backup | 2 |
| Partial rows whose plan could not run as written (R198, R199, R200, R205, R206) | 5 |
| Partial rows that needed a stated precondition to run (R045, R051, R054, R060, R062, R063, R097, R098, R099, R102, R121, R126) | 12 |
| Source links pointing at the wrong code (`config.py:136`, `config_store.py:367`, `config_repository.py:1265`, `config_repository.py:1133`) | 4 |

Totals after review: 5570 original entries + 112 per-route cases + 15 cross-cutting items = **5697** named entries (still design counts, not request counts).

### Link spot check

- 60 randomly sampled row and H links (seeded sample across R rows and H001–H1112): all 60 point at the branch the row describes, at most one line off.
- All ~60 links in the fixture, identity and write-code tables were also checked: 4 pointed at unrelated code and are fixed above; about 12 point at the blank line just before the cited `def`, which is harmless and left as is.

### Not testable by HTTP differential, and where it goes instead

| Item | Goes to |
|---|---|
| Scheduler single-instance file lock across processes (T27) | Go unit test |
| Web-port change, Nginx check/reload/rollback (R205/R206) | Go integration test + container test on .32 in **L1**; the Nginx flow's retirement goes to the O12 ledger |
| Sub-Store online update success path and process restart (R198–R200), unless the MITM-proxy setup is built | Go unit test + container test on .32 (**L1**) |
| Real WebDAV servers (Nextcloud, Jianguoyun quirks), real node RTT and DNS probes | **L6** rehearsal with real services |
| What an Agent does with a `publish_guard`, rule-sync pushes after config writes (T25, T30) | agents-domain differential, then **L2** (publish) and **L3** (rule sync, DNS follow) |
| Setup `check`/`finish` against a real installed gateway and router draft | **L1** (install) and **L5** (router) |
| Session continuity (`JWT_SECRET_KEY`), production TZ, log line format at the switch | O14 switch plan and **L6** rehearsal; format differences into O12 |
| OS faults (disk full, fsync failure, rename failure) during commits (T26) | fault injection on .32 against each server separately, not a differential |
| Host-header values flowing into generated URLs (B91), HEAD triggering upstream fetches (X06), static file serving and CORS policy once Go replaces Nginx | O9 security review (behaviour must still match Python here; O9 decides whether to change it, and any change goes to O12) |

### Notes for Step B

- Run every case with login enabled unless the row says otherwise, and run the D rows in all three environments.
- Keep `auto_backup` off in the default fixture; turn it on only in T16/T17 and the rows that test it, otherwise every write uploads.
- Give both processes the same `TZ`, `JWT_SECRET_KEY`, `SUB_STORE_URL`, `CONFIGFLOW_NODE_PROBE_BIN`, `LOG_FILE` and working directory.
- Before claiming a difference from a background loop, check the X08 list.

## Step B oracle errata（2026-11-17，原 O6 verdict 保留）

以下是公开 Python 实测或 REST 可达性发现的 Step A 前提更正；不修改原 O6 审核结论，不新增或删除原编号。执行证据见 `progress/difftest-config-b.md`。

| 编号 | 更正与归属 |
|---|---|
| R107/R140-S05、H369 | 空 changes 返回400；幂等应使用非空同值 patch。错误配方与成功配方分开，不能用400冒充幂等成功。来源 `backend/common/oxidns_common.py:1079`。 |
| R116-F08 | truthy数组先调用列表.pop的两个参数导致TypeError，Flask500；没有到create_profile的object guard。来源 `backend/routes/profiles.py:33`。 |
| R123-F12 | clone路径把source固定传给repository，metadata里的clone_from不会被消费；此项没有所述REST错误分支，N/A。源不存在已有F13及P1。来源 `backend/routes/profiles.py:115`、`backend/common/config_repository.py:1186`。 |
| legacy MosDNS P3 | /api/mosdns的六组GET/POST和whole settings没有对应显式profile URL别名；P3相应slot为N/A，P4–P6照测，不虚构URL。来源 routes.tsv 与 `backend/routes/profiles.py`。 |
| generate B90 | 下载端点仅POST；Range与If-None-Match:*实测200完整正文。GET的206/304必须由GET端点验证，不把它当POST成功条件。来源 `backend/routes/generate.py:94` 等及批次12。 |
| H104 | Fake-IP实际网段是198.18.0.0/15；原表把代码片段改成文档IPv4后改变了语义，已还原保留测试网段。来源 `backend/utils/node_dns.py:181`。 |
| H508 | R110的settings非对象先被wrapper拒绝；repository也不允许存非对象oxidns。此helper的非对象guard在所列REST驱动下不可达，保留审计哨兵，不直接调用Go helper伪造覆盖。 |
| X06 HEAD | 自动HEAD会执行handler，但method仍为HEAD：显式GET/elif POST返回None→500；profiles落创建分支→201；OxiDNS settings走缺对象→400。测试比实际副作用，不一律假定GET200。来源各handler的request.method分支及批次8。 |

- **T28/R028-S90 的轮换条件**：当前 schema 备份带有有效且不等于 config_token 的 rule_proxy_token 时，`_ensure_rule_proxy_token` 保留它；不是每次 import 无条件轮换。缺少能力、与 config_token 相等或 reset 默认配置时生成新值。`_apply_import` 合并旧能力/retired 并去重。Step B 分别测试三种 import 与重复 reset，来源 [config_repository.py:1695](../../backend/common/config_repository.py#L1695)、[config_repository.py:1767](../../backend/common/config_repository.py#L1767)。
- **R027-B90 的内存导出条件语义**：BytesIO send_file 没有自动 ETag/Last-Modified；GET Range→206/416，但 If-None-Match:* 与未来 If-Modified-Since 均200。保留原 B90 编号，按实测头存在性和语义验证，不虚造304。
- **R155-F18**：不存在的 default_subscription_ids 在 `build_import` 开头被过滤，实测有效客户端配置仍201；不是409。保留编号作为防回归的原预期更正，验证过滤后导入和全库/文件。来源 [config_importer.py:946](../../backend/utils/config_importer.py#L946)。
- **无显式 profile URL 的 P3 槽**：custom-config 五编辑器同 legacy MosDNS 一样没有 `/api/profiles/<id>/custom-config/...` 别名，P3（URL覆盖query/header）不可应用；保留原槽并标 N/A，P4–6仍需测试。不能拿一个不存在URL的404充作URL选择成功。
- **R214-B03 / R226-B01 间隔上限**：本基准 `MAX_INTERVAL=604800`，2592000 被拒绝400；不能当合法最大值。Step B 分别覆盖604800接受/604801拒绝以及原2592000拒绝。来源 [subscription_refresh.py:11](../../backend/utils/subscription_refresh.py#L11)。
- **R217-F17/F19 的可达性**：PUT在校验name/url之后强制 `new_data['id']=sub_id`，因此body的重复id不能制造重复资源；少name/url先400，少id由路径补回，其他可选字段删除本身也不触发引用409。这两个泛化槽未找到满足前置条件的HTTP配方，标部分覆盖/N/A，不能重复F09/F11或虚造repo409。来源 [subscriptions.py:136](../../backend/routes/subscriptions.py#L136)。
- **R222-F09/B01 的前提**：名称冲突拒绝需要激活严格拨号拓扑校验（有dialer-proxy/chain/override）；没有这些时，Python允许跨feed同名节点，即使定义不同也返回200。Step B分别覆盖无拓扑的同名成功与含合法`dialer-proxy: DIRECT`时的同名400。来源 [dialer_references.py:355](../../backend/utils/dialer_references.py#L355)。merged写缓存也不分配随机node ID，不能复用manual fetch的ID格式前提，来源 [provider_delivery.py:276](../../backend/utils/provider_delivery.py#L276)。
- **R193-B02 的中文输入**：规则编辑允许IDNA域名，不代表match-test接受Unicode query；该入口`is_valid_domain(..., allow_dns_labels=True)`仍只接受ASCII标签，中文query实测400。转为ASCII punycode后可查询，但此matcher不将已存Unicode DOMAIN值转换成punycode，不能伪造命中成功。分别保留这两种输入，来源 [rule_matcher.py:24](../../backend/utils/rule_matcher.py#L24)、[rule_matcher.py:64](../../backend/utils/rule_matcher.py#L64)、[rule_matcher.py:155](../../backend/utils/rule_matcher.py#L155)。

### Step B 聚合创建 oracle 更正

`backend/routes/aggregations.py:347–362` 只检查对象、初始化health、分配ID并提交；`backend/common/config_repository.py:2465–2469` 校验资源引用，没有名称和聚合正则校验。真实HTTP的空名、重复名、`regex_filter="["` 均200并原样保存。R002-F09/F13保留原编号并测试此实际行为，不能人为要求Go返回400；健康检查/缺失引用仍400。

### Step B 域名输入与 OxiDNS 预览边界

域名发现的 `clean_domain`（`backend/utils/domain_discovery_service.py:526`）使用严格ASCII主机名校验：原始中文域名返回400，已转punycode的IDNA名称可用；R046/R049/R050的中文边界分别验证这两种形式。普通 OxiDNS preview（`backend/routes/generate.py:303–316`）会将内部规则能力URL脱敏；G15应读完整ZIP的 `config.yaml` 来验证地址/token，不能把preview中的 `[REDACTED]` 当成下载URL。MosDNS的下载URL只在内部准备阶段存在，仍待外部可观察性审计。

### Step B 规则库初始化 oracle 更正

`backend/utils/domain_discovery_service.py:244–293` 每次初始化都分配新的 library/reference ID，没有检查已存在默认库后复用。R056-S04实测重复调用继续增加资源，默认指针切到新库；重复targets也被接受，创建多份库/引用，只有最后选中的库写缓存，较早同target新库成为未引用项。保留这些实际行为作为对比，不能要求幂等。

### Step B OxiDNS 草稿与诊断边界更正

- R106–R114及其R139–R147 scoped对应项的F11不是统一的HTTP拒绝：重复plugin tag在editor/template-preview保留为可继续编辑草稿；common-settings可投影，common-settings/apply允许无关worker_threads修改并保留重复插件；render和native-settings/apply拒绝400；validate用200 envelope返回valid=false及逐路径issues。非法YAML在validate同样是200结构化诊断，其他这些操作400。依据[oxidns_editor.py:107](../../backend/common/oxidns_editor.py#L107)、[oxidns_settings.py:123](../../backend/common/oxidns_settings.py#L123)、[oxidns_validation.py:295](../../backend/common/oxidns_validation.py#L295)；Step B逐端点实际发送两种输入，不把可编辑草稿误当可发布配置。
- R114/R147-B02未知插件为warning，valid=true且core_check_required=true，不捏造为错误。依据[oxidns_validation.py:245](../../backend/common/oxidns_validation.py#L245)。
- R107/R140-S04的AdGuard修改有效；device_policies不是common changes键，中文设备备注以B01验证无关草稿修改仍完整保留，设备策略本身的完整编辑由editor/G12覆盖。依据[oxidns_common.py:77](../../backend/common/oxidns_common.py#L77)、[oxidns_common.py:1077](../../backend/common/oxidns_common.py#L1077)。
- R109/R142-F18的message会附带首个引用位置（例如`（plugins[7].args）`），同时references必须比较候选文档的准确路径、tag和reason，不只检查message固定前缀。依据[oxidns_native_apply.py:443](../../backend/common/oxidns_native_apply.py#L443)。

### Step B setup chunked 的生产入口更正

R208/R210/R211-B90保留原输入：线上使用真实chunked、无Content-Length、>32KiB有效JSON；期望按公开生产入口观察。backend.app调用Waitress（[http_server.py:114](../../backend/http_server.py#L114)），其WSGI environ将解码后的字节数提供为CONTENT_LENGTH，故setup._body实际返回413。Step B有原始HTTP收包自检及独立Waitress WSGI回显自检，既验证客户端无CL，也验证路由观察到的长度。原“不触发413”只适用于直接WSGI缺CONTENT_LENGTH的环境，不能据此改变测试服务器入口。

### Step B setup prepare 失败写入边界更正

R211-F20/H1042：首次prepare会先由resolve_domain_rules/ensure_builtin_library初始化共享内置规则库，再检查外部profile修改冲突；即使409，shared.rule_library/shared._revision可能已增加。须检查被编辑profile与session未覆盖，同时完整比较新增共享行；重复同样冲突在内置库已初始化后保持存储不变。不能统一断言失败全库回滚。依据[setup_flow.py:417](../../backend/common/setup_flow.py#L417)、[setup_flow.py:490](../../backend/common/setup_flow.py#L490)、[setup_presets.py:239](../../backend/common/setup_presets.py#L239)。Step B用回环拒绝代理隔离新内置源可能进入的后台下载，不访问其真实源站。
