# Routers 领域黑盒差分覆盖清单（Step A，已经 O6 Opus 审核，见文末“O6 review”）

本文件只设计覆盖，不写测试代码，不代表已执行或通过。按 `FILES.md` routers归属52个Python文件筛选 `routes.tsv`，共有 **26条method+模板**。只从Python、路由/文件表、`PROGRESS-routers.md`模块/文件清单和前端API推导，不读取 `server/internal/**` 实现，不参与领域移植。先交Opus对照Python审，审过才进入Step B。**[O6]** 已审：Step B按本文件连同“[O6] 横切补充用例”“[O6] 逐路由补充用例”“[O6] 设备协议与fake合约补充”三节实现，前提条件见文末O6 review。

## 用例和比较规则

- `Rnnn-Sxx/Fxx/Bxx`各是一份独立输入/请求序列配方；共享A/P身份输入按每条适用路由展开计数。H是逐源码错误/诊断guard的必展开配方，D设备协议、G生成形态、T长序列另计。循环字段、复合guard各条件及各vendor分别参数化，清单条目数不等于最终HTTP请求数。错误降级、502里的补偿结果、200里的blocked/unknown均必须精确比，不能算发布成功。
- status、JSON类型/键/值/有序数组、中文消息逐字节，`Cache-Control`/`X-ConfigFlow-Profile`/Content-Type等标头也比。Python内部英文异常措辞只比拒绝/status/字段形态，不搬CPython异常文案。序列中的第二请求必须用各端第一次返回的revision/fingerprint/token，不能把Python guard直接拿给Go。
- 比完整SQLite快照、文件、fake设备表及出站HTTP transcript；每次写都验未关联profile/设备/foreign对象不变。不能只比浏览器200，更不能把device 2xx/`code=0`当回读已验证。每阶段的稳定字段、规则顺序、ownership、补偿日志都保留；只排除源码明确不影响配置的运行计数/时间，不能mask整份plan/manifest/report/errors。
- UUID、ROS `.id`、iKuai数字ID、操作tag、时间及签名token先验证格式/唯一性/时序/约束，再建Python↔Go双射并贯穿DB/设备/响应。fingerprint/revision/hash各端验证与自身内容一致，再用于后续调用；不能删除这些字段使冲突用例失效。固定生成命令/脚本逐字节及有序命令/quote比，YAML按解析结构比。
- **[O6]** Python端一律用`python -m backend.app`（waitress，`LOG_LEVEL`≠DEBUG）启动，不用Flask test client：routers规则自动同步循环只在这种启动方式下存在（X07）。生产在Python前有Nginx，长请求与请求体上限按X10。
- **[O6]** 出站设备请求体按解析后的JSON值比较（键序无关），不按字节：R04已接受Go用紧凑JSON（requests用`", "`分隔）。R04“Accepted differences”与Python崩溃类（500/IndexError，Go fail closed）单独归类记O12，不算通过也不算Go缺陷（X12）。`*.lock`与`.scheduler.lock`按精确glob排除出文件树比较，`router-rule-sync/<digest>.json`文件名必须一致（X04）。
- Step B所有测试/服务/构建只在测试服务器；用两个独立DATA_DIR，fake RouterOS/iKuai/Agent均在隔离loopback。不得接生产、PVE或家庭设备。真设备L5仅列计划，交协调人依WORKFLOW经维护者安排后执行。浏览器取消/慢设备也需验证后台与锁结局。

## 共享身份、数据与输入 fixture

| 代码 | 输入与需要分别实例化的变体 | Python依据 |
|---|---|---|
| A1–A7 | 所有26管理路由开启认证时各测：无Authorization、错签JWT、过期JWT、空claims JWT、Bearer双空格、8193字符token、只有Agent token。query config_token/设备密码不能越权；认证关闭环境分别重放合法成功请求 | [routes/auth.py:35](../../backend/routes/auth.py#L35)、[common/auth.py:23](../../backend/common/auth.py#L23) |
| P1/P2 | 当前profile不存在404、非法profile ID400；query profile优先profile_id/header再default。alpha/beta同名同IDrouter各自隔离。R001属agents全局路由，只有其显式query profile筛选；R002/R004 query agent绑定profile有特殊优先，单独覆盖。**[O6]** 更正：`routers`与`client_devices`蓝图不是全局的，before_request在**登录鉴权之前**解析profile（路径/`?profile=`/`?profile_id=`/`X-ConfigFlow-Profile`），未登录也先得404 `Profile not found: <id>`（英文，errorhandler）或400；R001挂在全局`agents`蓝图上，header与`profile_id`完全忽略、响应不带`X-ConfigFlow-Profile`，只有handler里的显式`?profile=`在鉴权之后生效。详见X02 | [profile_context.py:53](../../backend/common/profile_context.py#L53)、[client_devices.py:39](../../backend/routes/client_devices.py#L39)、[pending.py:165](../../backend/routers/pending.py#L165) |
| ROUTER | alpha中r-ros `{name:"路由器🧪",type:"routeros",endpoint:"http://127.0.0.1:<fake-port>",username:"test-admin",password:"synthetic-secret",proxy_gateway_agent_id:"m-a",ip_rule_sets:[],ros_policy_rule_sets:[],manage_return_routing:false}`；r-ik仅type/port改iKuai。HTTP/HTTPS、verify_tls true/false、凭据特殊字符分别测；panel存device密码但任何公开响应不得回显 | [store.py:54](../../backend/routers/store.py#L54) |
| PROFILE/AGENT | alpha默认、beta非默认；两profile各有synthetic Mihomo/OxiDNS，含offline/disabled/旧version/跨绑定；OxiDNS主备HA VIP合成地址、部分offline、配对不全与publication未完。Agent出站fake独立Bearer并记录身份；forwarding按router link授权，不强制Agent绑定当前空间 | [routes/routers.py:230](../../backend/routes/routers.py#L230)、[dhcp_dns.py:13](../../backend/routers/dhcp_dns.py#L13) |
| DEVICE | ARP/lease/terminal分别用合成LAN、MAC `02:00:00:00:00:21`，scope一/零/多匹配、disabled范围、静态foreign租约、动态租约、客户端先有name/mode；两端fake初始字节/顺序一致，模拟ROS运行字段和iKuai完整表单默认值 | [routeros_fixed_ip.py:75](../../backend/routers/routeros_fixed_ip.py#L75)、[ikuai_fixed_ip.py:45](../../backend/routers/ikuai_fixed_ip.py#L45) |
| RULES | inline/library/cache缺失/URL假下载，IPv4/IPv6/IP范围、纯domain/classical/ipcidr、重复和最具体source策略；fake-ip IPv4用合成范围，IPv6 fake-ip必须合成ULA（如`fd42:cf::/48`）且TUN按配置开启。涉及`is_global`成功分支由测试服务器生成**仅作为隔离规则文本的合成全局分类地址**，不把192.0.2.0/24、2001:db8::/32被过滤误作公网成功；地址不作网络目标、不访问公网、不写报告，仍逐叶比较原值 | [desired.py:88](../../backend/routers/desired.py#L88)、[rule_sync_rules.py:34](../../backend/routers/rule_sync_rules.py#L34)、[ipv6_fakeip.py:30](../../backend/routers/ipv6_fakeip.py#L30) |
| ADDR **[O6]** | 地址分类边界：发现用的LAN接口在没有DHCP server时只认RFC1918静态地址（10/8、172.16/12、192.168/16），只用TEST-NET的LAN会得“无LAN接口”；DHCP/路由下一跳校验用unspecified/loopback/multicast/255.255.255.255；规则IP用`is_global`；IPv6 link-local下一跳fe80::/10、fake-ip ULA fc00::/7；Mihomo未设fake-ip-range时用默认`198.18.0.1/16`。每类至少一个RFC1918变体和一个文档网段变体，见X11 | [ros_clients.py:31](../../backend/routers/ros_clients.py#L31)、[desired.py:16](../../backend/routers/desired.py#L16)、[dhcp_dns.py:44](../../backend/routers/dhcp_dns.py#L44) |
| SECRET **[O6]** | 种子里放：一个规则源URL指向面板自身、带内部rule_proxy_token；`system_config.rule_fetch_proxy`开/关两种（自动同步下载走该代理，fake代理记录请求）；Agent token为长随机串（避免短token巧合命中整串脱敏）；fake设备在错误文本里回显路由器密码的变体。见X01 | [config_export.py:130](../../backend/common/config_export.py#L130)、[rule_sync.py:132](../../backend/routers/rule_sync.py#L132) |
| RECEIPT **[O6]** | 两端DATA_DIR各放同内容的`router-rule-sync/<digest([profile,router]).json>`种子：baseline就绪且`checked_at`早于间隔（到期）、未到期、`running`（模拟重启遗留）、`queued`、`paused`、`failed`、坏JSON、JSON数组。用于不等15分钟也能驱动定时同步，见X07/T26 | [rule_sync_store.py:24](../../backend/routers/rule_sync_store.py#L24)、[rule_sync.py:374](../../backend/routers/rule_sync.py#L374) |
| V | body逐字段缺/空白/空对象/数组/null/错型、重复/超长、中文emoji、引号/反斜杠/美元/分号/换行/NUL；Content-Type/坏JSON，query重复值按Python取值行为。表中“合法/非法”必须套H guard给精确字段，不能只拿一个坏JSON代替整个模块 | [route_editor.py:76](../../backend/routers/route_editor.py#L76)、[store.py:54](../../backend/routers/store.py#L54) |

## 写入后比较代码

先后拍`configflow.sqlite3`的`sections(scope,owner,position)`、`fields(scope,owner,field,position,value,revision)`、`meta(key,value)`，JSON结构及有意义的顺序都比。配置的profile revision/config_revision、updated_at及事务一致性不能只靠GET推断；仅运行期cache/receipt不应误改草稿revision。config提交auto_backup可能合并触发WebDAV后台备份，测试只用synthetic WebDAV fake。**[O6]** 精确规则：凡写profile文档的请求都会`profile._revision += 1`（`config_revision`与`GET /api/profiles`的`revision`随之变化，Agent列表据此显示“待更新”），开启auto_backup时每次都安排一次WebDAV上传（1秒去抖）。写profile的有：R003、R005、R008、R009、R010、R011（含empty发布写last_release、失败发布写manifest）、R015、R017（**探测失败也写last_probe**）、R024、自动同步改manifest时；不写的有：R001/R012/R018（只写通知存储）、receipt与规则缓存文件。跨域后果见X08。来源 [config_store.py:38](../../backend/common/config_store.py#L38)、[config_repository.py:1010](../../backend/common/config_repository.py#L1010)。

| 代码 | DB/文件/后台/fake设备观察 |
|---|---|
| ROUTER | 对应profile `routers.items`单条及顺序，password保留/变更、last_probe的失效条件；delete仅删record不设备卸载。expected_router/连接字段与job_lock再检，跨profile同endpoint仍共享锁；不得客户端注入manifest/id。来源 [store.py:54](../../backend/routers/store.py#L54)、[config_repository.py:1321](../../backend/common/config_repository.py#L1321)。 |
| PROBE | last_probe.at/ok/version/model/capabilities/message持久化，probe失败也记录；无设备写/Agent推送；只读账号probe成功不能标已有写权限。来源 [routes/routers.py:208](../../backend/routes/routers.py#L208)。 |
| REPORT / PLAN / POLICY-READ / POLICY-PREVIEW | 路由器/Agent只读transcript，plan/checks/evidence及资源顺序。report/plan/pending会安全记录路由通知，允许notifications/task聚合副作用，不修改device/publication；policy preview可能生成规则内容/缓存，DNS预览不推Agent。来源 [routes/routers.py:388](../../backend/routes/routers.py#L388)、[routes/routers.py:646](../../backend/routes/routers.py#L646)。 |
| RELEASE | router last_release和ros_policy_state/dns_guard_state/ipv6_fakeip_state/ipv6_routes_state/ipv6_pd_guard_state；操作完成及补偿后新ID/稳定config/归属都保存，其他并发普通字段保留；设备所有相关表与Agent回流状態，发布log/rollback/failed_stage。成功含empty可建立router-rule-sync baseline、刷新任务通知；设备成功后receipt I/O失败不能改报失败，DB publication失败不能反称设备未改。来源 [routes/routers.py:437](../../backend/routes/routers.py#L437)、[release.py:290](../../backend/routers/release.py#L290)、[config_repository.py:1341](../../backend/common/config_repository.py#L1341)。 |
| WORKSPACE / CLIENT | profile routers/oxidns/device_notes及client_fixed_ips按动作原子写；只改name不重写DNS策略；清本地设备记录不删除router静态租约，不自动publish/push。names_revision/previous_name/previous_mode与锁内profile检查。来源 [policy_workspace.py:37](../../backend/routers/policy_workspace.py#L37)、[client_devices.py:40](../../backend/common/client_devices.py#L40)。 |
| FIXED / FIXED-PREVIEW | preview只签600秒token不写device/binding；apply逐设备写DHCP→所有成功后原子存client_fixed_ips，仅自有marker/binding；失败逆序undo，delete回滚重建后新ID同步binding。apply开始/结束invalidate discovery并验证在途旧读取不得回填。设备未保存不允许固定；名字/DNS策略保持，真实DHCP续租另L5。来源 [client_fixed_ip.py:152](../../backend/common/client_fixed_ip.py#L152)、[client_fixed_ip.py:171](../../backend/common/client_fixed_ip.py#L171)。 |
| DEVICE-ROUTE | device静态路由全表含高级字段/foreign配置、readonly/revision、返回entry与changed。只编辑设备，不改profile路由选择/规则/manifest，不触发Agent推送或自动发布；失败补偿不覆盖外部写。来源 [route_editor.py:153](../../backend/routers/route_editor.py#L153)、[routeros_routes.py:221](../../backend/routers/routeros_routes.py#L221)、[ikuai_routes.py:178](../../backend/routers/ikuai_routes.py#L178)。 |
| SYNC | profile只存auto_rule_sync/rule_sync_interval及成功/回滚manifest；`router-rule-sync/<digest>.json` receipt与`.lock`、baseline/control/rules/desired/static_resources/nat_list/policy、status/log最后20条，规则缓存、task-status计数/trigger/duration、device delta。run loop/POST worker需轮询结局，非只202；中断running→paused，下载失败failed可重试，非下载控制/归属错误paused需人工。来源 [rule_sync_store.py:16](../../backend/routers/rule_sync_store.py#L16)、[rule_sync.py:241](../../backend/routers/rule_sync.py#L241)。 |
| DISCOVERY / PENDING | 每app/Agent/profile缓存signature/checked_at/cached/stale/refresh_error、并发共用和失效；不把router offline等价saved client offline。DISCOVERY全新读取失败保留旧好快照；PENDING unknown不能复用。PENDING安全通知可写，但无设备配置写。来源 [client_discovery_cache.py:34](../../backend/common/client_discovery_cache.py#L34)、[pending.py:199](../../backend/routers/pending.py#L199)。 |
| FORWARD | 当前profile linked refs授权、fake Mihomo `linux-gateway/status`与POST `forwarding`前后及超时再查；不写router DHCP/NAT/profile draft。是否真的sysctl持久化需L5-H及agents L4，fake200仅证协议。来源 [routes/routers.py:132](../../backend/routes/routers.py#L132)、[agents/linux_gateway.py:625](../../backend/agents/linux_gateway.py#L625)。 |

## 每条路由覆盖矩阵

每条method+模板一行。来源列给实际handler全范围，所有S/F/B无独立行号的项以该handler及后文H逐guard为依据；D/G/T展开设备、生成与长序列变体。共享身份见A/P源码。L5列只列fake不能证明的行为，纯鉴权/校验/协议/状态和DB仍须先在fake差分。

| ID / method / route | 成功S | 失败/降级F | 边界B | 写后比较 / 长流程 | 实验网L5 | Python |
|---|---|---|---|---|---|---|
| **R001** `GET /api/agents/<agent_id>/upstream-routers` | **S01** 无linked router空列表<br>**S02** 同一Agent跨alpha/beta的路由器按profile/router顺序<br>**S03** profile=alpha仅alpha<br>**S04** refresh=1<br>**S05** refresh=true<br>**S06** 未变signature复用cached=true<br>**S07** 目标Agent已删但仍有linked记录按Python报告 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** ~~P1空间不存在404~~ **[O6]** 更正：本路由在全局agents蓝图，header/`profile_id`/default的P1不适用（header指向不存在空间仍200，见F91）；只有显式`?profile=`（F14）<br>**F09** ~~P2空间ID非法400~~ **[O6]** 同上，只有F13<br>**F10** 总等待FutureTimeout→504<br>**F11** 单RouterError→200该路由unknown而其他不丢<br>**F12** 单路由25秒未完→unknown<br>**F13** 显式profile非法→400<br>**F14** 显式profile不存在→404<br>**F15** 内部解析异常只该路由unknown | **B01** unknown不能缓存为synced<br>**B02** 重复并发共用一次读取<br>**B03** profile/router/凭据/关联Agent变化使签名失效<br>**B04** 活动sidebar不能覆盖显式profile<br>**B05** 中文emoji路由/空间名<br>**B06** 空Agent或非Mihomo按实际linked记录不凭空404<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | PENDING；T01 | 需实验网 **L5-A,L5-B,L5-C**；见L5表 | [upstream_router_pending.py:L13–27](../../backend/routes/upstream_router_pending.py#L13) |
| **R002** `GET /api/client-devices` | **S01** 默认profile读设备名+策略<br>**S02** query profile=beta<br>**S03** query agent绑定OxiDNS的profile优先<br>**S04** 无策略但有备注<br>**S05** 有固定IP binding与Agent deployment/HA摘要 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** agent不存在→404<br>**F11** agent不是OxiDNS→404<br>**F12** 配置政策不可编辑以editable=false/reason返回不崩溃 | **B01** 空设备/空Agent列表<br>**B02** 旧Agent省略profile走稳定default<br>**B03** IPv4/IPv6/CIDR设备合并不重复<br>**B04** 中文emoji名字<br>**B05** 公开返回不得泄露token/native secret<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | CLIENT | —（本路由只管草稿/校验；不据此证明真设备行为） | [client_devices.py:L55–78](../../backend/routes/client_devices.py#L55) |
| **R003** `PUT /api/client-devices` | **S01** 新命名mode=""<br>**S02** 模式smart<br>**S03** 模式direct<br>**S04** smart→direct<br>**S05** 清mode保留name<br>**S06** 清name保留mode<br>**S07** 清name和mode删本地行/binding但不删设备租约<br>**S08** 只改名不改OxiDNS | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 非对象/字段缺失或多余/base_url非字符串→400<br>**F11** body profile_id与选中空间不符→409<br>**F12** 非法IP/范围/名字/模式→400<br>**F13** previous_name过时→409<br>**F14** previous_mode过时→409<br>**F15** native DNS策略不能安全改写→400<br>**F16** 事务/并发资源变更拒绝且不半存 | **B01** 中文emoji及换行/quote按normalize_name<br>**B02** 名称80字符与81字符<br>**B03** IPv6/CIDR canonical<br>**B04** 空previous值表示新行<br>**B05** 两不同IP并发均保留<br>**B06** query agent不能改变PUT的profile作用域<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | CLIENT；T02 | —（本路由只管草稿/校验；不据此证明真设备行为） | [client_devices.py:L55–78](../../backend/routes/client_devices.py#L55) |
| **R004** `GET /api/client-devices/discovery` | **S01** RouterOS ARP+lease+probe正常<br>**S02** IKuai terminal+lease正常<br>**S03** 双vendor合并<br>**S04** 无router→unsupported<br>**S05** 一台失败→partial<br>**S06** 全失败→failed<br>**S07** 缓存未刷新<br>**S08** refresh=1强制<br>**S09** 旧好快照+全刷新失败→cached且refresh_error<br>**S10** 保存名称变化立即反映 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** agent缺失/错服务→404<br>**F11** RouterOS必需ARP失败→该源失败<br>**F12** 可选表失败→notes/未知而不误offline<br>**F13** 20秒deadline后慢源超时<br>**F14** 一个解析异常不隐藏其余<br>**F15** 固定IP enrich失败仅禁用控件 | **B01** 空表<br>**B02** 多路由同IP冲突与better优先级<br>**B03** MAC大小写/分隔符/非法值<br>**B04** 基础设施IP排除<br>**B05** 仅失败ARP无MAC不列新设备<br>**B06** 最具体CIDR模式<br>**B07** cache60秒stale不自动刷新<br>**B08** 128profile与+1淘汰<br>**B09** 并发refresh共用/写固定IP前后invalidate<br>**B10** alpha/beta隔离、query/header/default或本路由特例的profile选择 | DISCOVERY；T03,T04 | 需实验网 **L5-F**；见L5表 | [client_devices.py:L83–100](../../backend/routes/client_devices.py#L83) |
| **R005** `POST /api/client-devices/fixed-ip/apply` | **S01** RouterOS动态make-static<br>**S02** RouterOS无租约静态新增<br>**S03** iKuai静态新增<br>**S04** RouterOS删除自有预留<br>**S05** iKuai删除自有预留<br>**S06** 一次多router混合vendor<br>**S07** 本地binding缺失但设备保留cf-client自有标记，重新预览后删除成功 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 非对象/字段不精确→400<br>**F11** body profile不符→409<br>**F12** token缺/空/错型/>1000000→409<br>**F13** 签名错/过期600秒/跨profile→409<br>**F14** setup未完成→409<br>**F15** job_lock占用→409<br>**F16** 等待锁期间router变化→409<br>**F17** 重新plan与签名operations不一致→409<br>**F18** 设备未先保存name或mode→409<br>**F19** 快照/IP/MAC/server/scope变化→409/502按adapter<br>**F20** 第N项写入/读回失败→502且逆序补偿<br>**F21** 事务保存时设备/binding/router变化→409并补偿<br>**F22** 数据库保存异常→502并补偿<br>**F23** 补偿失败→502明确回滚未完成<br>**F24** 共同层无自有binding→409，ROS租约changed/foreign→502，iKuai→409<br>**F25** 恢复租约新ID保存失败→502<br>**F26** 后续设备失败时已删预留重建成功，仍返回原失败并保存恢复的新ID binding | **B01** 一个与64设备<br>**B02** 原始token不能跨项目复用<br>**B03** 删除响应丢失后回读已不存在不重复创建<br>**B04** 同token重放必须重查不重造身份<br>**B05** 外部改动不覆盖/不猜删<br>**B06** 中文emoji备注与唯一operation后缀<br>**B07** 临界600秒与超期<br>**B08** 开始/结束invalidate防旧discovery回填<br>**B09** alpha/beta隔离、query/header/default或本路由特例的profile选择 | FIXED；T05,T06,T07 | 需实验网 **L5-F,L5-G**；见L5表 | [client_devices.py:L128–153](../../backend/routes/client_devices.py#L128) |
| **R006** `POST /api/client-devices/fixed-ip/preview` | **S01** RouterOS动态转静态预览<br>**S02** RouterOS ARP设备新增静态预览<br>**S03** iKuai新增静态分配预览<br>**S04** 两vendor各删除自有预留<br>**S05** 1..64台多路由按输入顺序actions | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** body字段缺/多/错型→400<br>**F11** operation非法→400<br>**F12** devices空/>64→400<br>**F13** row不是精确ip/mac/router_id/name→400<br>**F14** 非法/重复IPv4或无MAC/router→400<br>**F15** 同router同MAC多IP→400<br>**F16** profile不符→409<br>**F17** setup进行中→409<br>**F18** 来源router不存在/不支持→409<br>**F19** add的MAC/IP非该router所见→409<br>**F20** read-only/禁用/模糊DHCP范围：ROS→502、iKuai→409<br>**F21** 外部静态租约或租约冲突：ROS→502、iKuai→409<br>**F22** delete无自有binding→409<br>**F23** 设备读取失败→502 | **B01** MAC归一<br>**B02** 多设备重复IP跨router仍拒<br>**B03** 空name合法<br>**B04** 1/64/65<br>**B05** token内容不包含密码，operations连接只存digest<br>**B06** 预览不保存binding/不改路由器<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | FIXED-PREVIEW；T05 | 需实验网 **L5-F,L5-G**；见L5表 | [client_devices.py:L105–123](../../backend/routes/client_devices.py#L105) |
| **R007** `GET /api/routers` | **S01** 默认alpha列表<br>**S02** query profile=beta<br>**S03** 旧record默认开关/interval归一<br>**S04** 静态/policy旧重叠选择只公开静态差集 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 非法已存record仅保留dict按routers_of规则<br>**F11** 读取存储异常不返回伪空成功 | **B01** 空列表<br>**B02** 中文emoji名称<br>**B03** has_password正确且password不返回<br>**B04** manifests不进public_view<br>**B05** 未知配置字段不泄漏；DB保持原样<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | ROUTER | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L102–112](../../backend/routes/routers.py#L102) |
| **R008** `POST /api/routers` | **S01** RouterOS最小合法凭据<br>**S02** IKuai4最小合法凭据<br>**S03** TLS开关false<br>**S04** 选择静态rule sets<br>**S05** RouterOS策略rule sets与return开关<br>**S06** DNS guard合法开启<br>**S07** PD guard合法开启<br>**S08** RouterOS auto-sync合法间隔 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** body非对象/坏JSON→400<br>**F11** 缺name/type/endpoint/username/password各拒400<br>**F12** type非routeros/ikuai→400<br>**F13** URL无scheme/无host/有path→400<br>**F14** manage_return非bool→400<br>**F15** auto_sync非bool或iKuai开启→400<br>**F16** interval非整数/不在枚举→400<br>**F17** ip_rule_sets非list/含空或非str→400<br>**F18** policy_rule_sets非list/非法项→400<br>**F19** IKuai选择policy→400<br>**F20** 静态和policy交集→400<br>**F21** DNS guard未知字段/非bool/非法IPv4/>64/非ROS/无DNS Agent→400<br>**F22** PD guard未知字段/非bool/非ROS→400<br>**F23** 配置事务busy/冲突不半写 | **B01** name中文emoji/超长不凭空加上限<br>**B02** username/password特殊字符<br>**B03** URL IPv6/自定义端口/尾slash<br>**B04** URL query/fragment/userinfo按Python接受边界核对而非预设拒绝<br>**B05** 重复rule IDs去重<br>**B06** body伪造id/manifests/last_release不得注入<br>**B07** 空list与null按normalize<br>**B08** alpha/beta隔离、query/header/default或本路由特例的profile选择 | ROUTER；T08 | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L102–112](../../backend/routes/routers.py#L102) |
| **R009** `DELETE /api/routers/<router_id>` | **S01** 无任何active manifest删除record且不写设备 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 找不到router→404<br>**F11** 发布/同步job忙→409<br>**F12** 等待router锁后record变化→409<br>**F13** ros_policy_state存在→409<br>**F14** ipv6_fakeip_state存在→409<br>**F15** ipv6_routes_state存在→409<br>**F16** dns_guard_state存在→409<br>**F17** ipv6_pd_guard_state存在→409<br>**F18** repository并发expected不符→409（**[O6]** ProfileInUse经errorhandler，响应多一个`usages:[]`键）| **B01** 重复删除404<br>**B02** 删record不卸载设备/不清外部对象<br>**B03** 与其他profile同ID隔离<br>**B04** rule-sync运行/固定IP同连接互斥<br>**B05** alpha/beta隔离、query/header/default或本路由特例的profile选择 | ROUTER；T08 | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L160–202](../../backend/routes/routers.py#L160) |
| **R010** `PUT /api/routers/<router_id>` | **S01** 只改name<br>**S02** 只改username/password<br>**S03** 空password保留旧secret<br>**S04** 改endpoint/type且无manifest<br>**S05** 只改interval<br>**S06** 规范化旧重叠选择不把policy清空变静态 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 找不到404<br>**F11** job锁忙409<br>**F12** 锁内record变化409<br>**F13** 所有normalize拒绝分支同POST<br>**F14** 有active policy/DNS/PD/fakeIP/IPv6 routes manifest改type/endpoint分别400<br>**F15** repository expected冲突409（**[O6]** 同R009 F18，带`usages:[]`）| **B01** 未知字段不覆盖manifest/id<br>**B02** password非str按Python保留<br>**B03** 连接凭据任一变更清last_probe，改name保留<br>**B04** 空对象幂等<br>**B05** 其他profile不可改<br>**B06** 并发两字段/删除与发布冲突不丢改动<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | ROUTER；T08 | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L160–202](../../backend/routes/routers.py#L160) |
| **R011** `POST /api/routers/<router_id>/apply` | **S01** RouterOS基础静态路由发布<br>**S02** IKuai目的静态/端口分流两路径<br>**S03** 启return protocol1<br>**S04** 启return protocol2<br>**S05** 关闭return最后清理<br>**S06** DHCP DNS单Agent<br>**S07** DHCP DNS HA VIP<br>**S08** ROS来源policy创建<br>**S09** policy撤回迁静态先保护NAT<br>**S10** NAT list迁移与增删<br>**S11** DNS guard开/更新/撤回<br>**S12** IPv6 fakeIP开/撤回<br>**S13** IPv6公共routes增删<br>**S14** IPv6 DNS关闭通告<br>**S15** PD guard装/更新/撤回<br>**S16** empty计划记录last_release及baseline | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 找不到→404<br>**F11** job忙→409<br>**F12** body非空标量/数组导致属性异常→500<br>**F13** 等待期间record变化409<br>**F14** plan blockers409<br>**F15** fingerprint缺/错/过期409<br>**F16** device/Agent读取RouterError→502<br>**F17** 每stage写入/回读失败→502且逆序撤已完成项<br>**F18** 本阶段不确定写入需携undo/保留未知现场<br>**F19** 回滚失败502明确未完成<br>**F20** DB publication提交失败仍保留真实设备结果不能宣称未写（**[O6]** 更正：Python此处抛ProfileInUse，不被`except RouterError`捕获→errorhandler 409 `{success:false,message:'路由器连接已变化，无法保存发布回读记录',usages:[]}`，响应不含result，last_release不写；只在持有job锁期间连接字段被改时触发，HTTP基本不可达→审计N/A，Go行为记O12）<br>**F21** receipt写失败不把成功手动发布改失败 | **B01** 空{}或null缺fingerprint409<br>**B02** 合法fingerprint后的额外键按Python<br>**B03** 同fingerprint并发串行后重建计划<br>**B04** 计数器变化不误stale而稳定字段/顺序变化必须stale<br>**B05** 中文emoji规则/提示<br>**B06** 中途Agent/配置变更不能改目标<br>**B07** RouterOS新增无可信ID不猜删<br>**B08** fixture只读driver分支外部不可达标审计<br>**B09** alpha/beta隔离、query/header/default或本路由特例的profile选择 | RELEASE；T09,T10,T11,T12,T13,T14 | 需实验网 **L5-A,L5-B,L5-C,L5-D,L5-E**；见L5表 | [routers.py:L437–518](../../backend/routes/routers.py#L437) |
| **R012** `POST /api/routers/<router_id>/plan` | **S01** ROS静态+NAT基础计划<br>**S02** IKuai静态或端口分流/group计划<br>**S03** 带return与cleanup计划<br>**S04** DHCP单Agent/HA VIP<br>**S05** ROS策略bundle与迁移<br>**S06** DNS guard<br>**S07** IPv6 fakeIP<br>**S08** IPv6规则routes<br>**S09** IPv6 DNS及fix_command<br>**S10** PD guard安装/沿用/关闭<br>**S11** 无差异empty加sync_baseline<br>**S12** 阻止/跳过项仍HTTP200完整plan | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 找不到404<br>**F11** 设备列表/格式/认证失败→502<br>**F12** 规则缺失或不能读取按desired warning与policy blocker，不猜空撤回<br>**F13** NAT出口/规则证据无法证明→blockers<br>**F14** Agent回流offline/unavailable→blockers | **B01** null/空/额外body被忽略<br>**B02** 中文emoji/特殊字符脚本quote<br>**B03** 100/101路由与大配置<br>**B04** fingerprint随稳定证据变化且计数器不变<br>**B05** preview不得写设备或保存publication，但会写通知<br>**B06** 重复预览同快照一致<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | PLAN；T09,T11,T12,T14 | 需实验网 **L5-A,L5-B,L5-C,L5-D,L5-E**；见L5表 | [routers.py:L408–431](../../backend/routes/routers.py#L408) |
| **R013** `GET /api/routers/<router_id>/policy` | **S01** ROS已发布policy对象<br>**S02** 未发布时对象清单<br>**S03** 当前profile OxiDNS source_agents | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** IKuai→400<br>**F12** 等待锁后router变化409<br>**F13** 读取对象/manifest校验失败502 | **B01** 空资源<br>**B02** foreign同名对象不冒充owned<br>**B03** 源OxiDNS同空间/default回退<br>**B04** 未知native字段保留指纹<br>**B05** Cache-Control no-store<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | POLICY-READ；T11 | 需实验网 **L5-B**；见L5表 | [routers.py:L556–591](../../backend/routes/routers.py#L556) |
| **R014** `GET /api/routers/<router_id>/policy-settings` | **S01** workspace含revision/names_revision/config_revision<br>**S02** profile内OxiDNS targets<br>**S03** Mihomo gateways按Python候选列表 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** IKuai400<br>**F12** OxidnsSettings/ProfileRepository错误400 | **B01** 空rule set/源设备<br>**B02** 有policy不可编辑reason<br>**B03** 改备注不影响draft_revision，策略/router改动影响；last_probe不影响draft_revision<br>**B04** no-store与profile header<br>**B05** alpha/beta隔离、query/header/default或本路由特例的profile选择 | WORKSPACE | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L597–641](../../backend/routes/routers.py#L597) |
| **R015** `PUT /api/routers/<router_id>/policy-settings` | **S01** 新格式{revision,router:{ip_rule_sets,ros_policy_rule_sets,proxy_gateway_agent_id},base_url}<br>**S02** 旧格式加policies/names_revision原子保存<br>**S03** 同值幂等 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** IKuai400<br>**F12** job忙409<br>**F13** 非对象/键集合不精确/router字段不精确/base_url错型400<br>**F14** 锁内record变更409<br>**F15** revision过时409<br>**F16** 旧格式names_revision过时409<br>**F17** normalize/settings/policies/资源引用校验400<br>**F18** 保存中冲突不可半存（**[O6]** 事务里的ProfileInUse被`except ProfileRepositoryError`捕获→**400**，不是409） | **B01** 空policies清空<br>**B02** 中文emoji源名字<br>**B03** 只发router不受纯备注变更阻挡<br>**B04** last_probe不使revision失效而publication变化使失效<br>**B05** 仅draft无router写入/Agent推送<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | WORKSPACE；T02,T08 | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L597–641](../../backend/routes/routers.py#L597) |
| **R016** `POST /api/routers/<router_id>/policy-settings/preview` | **S01** targets=[]仅router plan且dns_content空<br>**S02** 单OxiDNS target<br>**S03** 多同profile OxiDNS target<br>**S04** 合法target即使offline也按Python输出状态 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** body非对象/targets非list或含非str/重复/base_url错型400<br>**F11** router缺404<br>**F12** IKuai400<br>**F13** revision缺/错409<br>**F14** target缺失/服务错/改绑409<br>**F15** 生成期间config_revision变化409<br>**F16** RouterError/ValueError生成校验失败→400，其他未捕获异常→500（**[O6]** ProfileInUse/ProfileValidationError是ValueError→400；ProfileNotFound是KeyError→errorhandler 404；`yaml.YAMLError`→HTML 500） | **B01** 空target不生成DNS<br>**B02** 未知body字段按Python接受<br>**B03** 中文emojitarget名<br>**B04** secret脱敏但dns_content结构仍要比<br>**B05** 只预览不推router或Agent<br>**B06** profile scope不可跨空间<br>**B07** alpha/beta隔离、query/header/default或本路由特例的profile选择 | POLICY-PREVIEW；T11 | 需实验网 **L5-B,L5-C**；见L5表 | [routers.py:L646–699](../../backend/routes/routers.py#L646) |
| **R017** `POST /api/routers/<router_id>/probe` | **S01** RouterOS7.1+ resource+可读能力<br>**S02** iKuai4.x sysstat+可读能力<br>**S03** 部分能力读失败仍probe成功但capabilities缺项 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** job忙409<br>**F12** 认证401/403或网络/JSON/版本错误→probe.ok=false且HTTP502<br>**F13** probe子能力错误只省略capability | **B01** body忽略<br>**B02** 只读账号可probe成功不代表可写<br>**B03** 密码不入probe/public/log<br>**B04** version和model中文<br>**B05** 写last_probe失败真实device只读<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | PROBE；T15 | 需实验网 **L5-A**；见L5表 | [routers.py:L208–227](../../backend/routes/routers.py#L208) |
| **R018** `GET /api/routers/<router_id>/report` | **S01** ROS routes/NAT差异<br>**S02** iKuai routes+policy/group差异<br>**S03** 全synced<br>**S04** missing/pending/conflict/unknown<br>**S05** DNS DHCP/HA<br>**S06** DNS guard/IPv6各report<br>**S07** return disabled/unlinked/offline/unavailable/ok<br>**S08** forward hint accept/drop/unknown | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** device RouterError→502<br>**F12** DHCP读取失败写dns_error/unknown<br>**F13** 规则content缺失静态warning/policy blocker<br>**F14** 可选mangle/policy证据缺失不当安全；未知matcher保持unknown | **B01** foreign路由/重叠CIDR/默认路由/blackhole/多路径<br>**B02** 空规则和空设备表<br>**B03** 中文emoji备注<br>**B04** 通知runtime变化允许而不能写设备<br>**B05** counter与运行期lifetime变化；同端口转发hint不进fingerprint<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | REPORT；T11,T12,T14 | 需实验网 **L5-A,L5-B,L5-C,L5-D,L5-E**；见L5表 | [routers.py:L388–403](../../backend/routes/routers.py#L388) |
| **R019** `GET /api/routers/<router_id>/routes` | **S01** ROS普通静态main可编辑<br>**S02** iKuai4完整表单可编辑<br>**S03** 动态/ECMP/VRF等只读行仍返回 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 缺router404<br>**F11** 上游认证/网络/坏列表502<br>**F12** 丢ID/重复ID导致read_routes整批拒绝502<br>**F13** IKuai分页缺行/total变更/无有效ID502 | **B01** 空路由<br>**B02** ROS readonly未知字段/布尔不可识别/网关接口或递归<br>**B03** IKuai字段缺失或新增readonly<br>**B04** 备注中文emoji/512字符<br>**B05** revision基于稳定snapshot，不忽略未知字段；ROS runtime counters不影响<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | DEVICE-ROUTE；T16 | 需实验网 **L5-A**；见L5表 | [routers.py:L540–551](../../backend/routes/routers.py#L540) |
| **R020** `POST /api/routers/<router_id>/routes/apply` | **S01** ROS add<br>**S02** ROS update<br>**S03** ROS delete<br>**S04** iKuai add<br>**S05** iKuai update<br>**S06** iKuai delete<br>**S07** 相同值empty no-op<br>**S08** cf管理备注warning | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router不存在404<br>**F11** body/action/字段错误400<br>**F12** id缺失/超128/控制字符400<br>**F13** revision非64小写hex400<br>**F14** add携id/revision400<br>**F15** delete携values400<br>**F16** values缺字段/额外字段400<br>**F17** cidr/gateway无效/非IPv4或不可用单播400<br>**F18** enabled非bool400<br>**F19** comment非str/>512/含NUL400<br>**F20** 锁后router变化409<br>**F21** 原路由消失/revision变/readonly409<br>**F22** 主表相同cidr重复409<br>**F23** device读取/写/回读错误502<br>**F24** job锁占用409<br>**F25** fingerprint缺/错409<br>**F26** 外部写入期间变化只补偿可证明自身对象<br>**F27** 补偿失败502明确回滚未完成 | **B01** CIDR主机位归一<br>**B02** gateway边界<br>**B03** comment512与513/emoji/特殊quote/换行<br>**B04** 空与非空未知字段<br>**B05** 外部改变高级字段必须冲突<br>**B06** update保留advanced配置<br>**B07** add丢ID不猜删；delete回滚ID可变化<br>**B08** 重复apply按新snapshot/fingerprint检查<br>**B09** alpha/beta隔离、query/header/default或本路由特例的profile选择 | DEVICE-ROUTE；T16 | 需实验网 **L5-A**；见L5表 | [routers.py:L711–753](../../backend/routes/routers.py#L711) |
| **R021** `POST /api/routers/<router_id>/routes/plan` | **S01** ROS add<br>**S02** ROS update<br>**S03** ROS delete<br>**S04** iKuai add<br>**S05** iKuai update<br>**S06** iKuai delete<br>**S07** 相同值empty no-op<br>**S08** cf管理备注warning | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router不存在404<br>**F11** body/action/字段错误400<br>**F12** id缺失/超128/控制字符400<br>**F13** revision非64小写hex400<br>**F14** add携id/revision400<br>**F15** delete携values400<br>**F16** values缺字段/额外字段400<br>**F17** cidr/gateway无效/非IPv4或不可用单播400<br>**F18** enabled非bool400<br>**F19** comment非str/>512/含NUL400<br>**F20** 锁后router变化409<br>**F21** 原路由消失/revision变/readonly409<br>**F22** 主表相同cidr重复409<br>**F23** device读取/写/回读错误502 | **B01** CIDR主机位归一<br>**B02** gateway边界<br>**B03** comment512与513/emoji/特殊quote/换行<br>**B04** 空与非空未知字段<br>**B05** 外部改变高级字段必须冲突<br>**B06** update保留advanced配置<br>**B07** 预览不写device/DB<br>**B08** alpha/beta隔离、query/header/default或本路由特例的profile选择 | DEVICE-ROUTE | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L704–753](../../backend/routes/routers.py#L704) |
| **R022** `GET /api/routers/<router_id>/rule-sync` | **S01** 未启waiting<br>**S02** 已启baseline ready<br>**S03** queued/running/synced/failed/paused状态与最近20log | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** IKuai400<br>**F12** 坏receipt读为paused不当空synced | **B01** 空rules<br>**B02** version/synced_version仅12前缀但内容hash验证<br>**B03** 公开不泄露baseline/password<br>**B04** no-store<br>**B05** alpha/beta隔离、query/header/default或本路由特例的profile选择 | SYNC | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L772–820](../../backend/routes/routers.py#L772) |
| **R023** `POST /api/routers/<router_id>/rule-sync` | **S01** 已启且baseline就绪启动202<br>**S02** 失败下载后允许重试<br>**S03** 完成synced后再检查无delta | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** router缺404<br>**F11** IKuai400<br>**F12** 关闭或无baseline409<br>**F13** paused409<br>**F14** running/queued409<br>**F15** job并发RouterError→409，启动/存储未捕获异常→500且释放worker<br>**F16** 后台下载失败为failed不当已同步<br>**F17** 后台控制/manifest变化paused<br>**F18** 不确定写/回滚失败paused待人工 | **B01** body按Python忽略<br>**B02** 同时两次start只一job<br>**B03** 进程重启running须人工确认<br>**B04** 共享endpoint跨profile互斥<br>**B05** 手动empty apply可重建baseline恢复paused<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | SYNC；T17,T18,T19 | 需实验网 **L5-B,L5-I**；见L5表 | [routers.py:L772–820](../../backend/routes/routers.py#L772) |
| **R024** `PUT /api/routers/<router_id>/rule-sync` | **S01** 开关true<br>**S02** 开关false<br>**S03** 有效interval=15<br>**S04** 有效interval=30<br>**S05** 有效interval=60<br>**S06** 有效interval=180<br>**S07** 有效interval=360<br>**S08** 有效interval=720<br>**S09** 有效interval=1440 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 缺router404<br>**F11** IKuai400<br>**F12** job锁占用409<br>**F13** 非对象/键不精确400<br>**F14** bool错/interval非int或枚举外400<br>**F15** 锁内router变化409<br>**F16** 事务中router已删400 | **B01** interval bool不能当int<br>**B02** 关闭不删除设备规则/baseline<br>**B03** 同值写与未完成后台竞争<br>**B04** 未知字段拒绝<br>**B05** no-store<br>**B06** alpha/beta隔离、query/header/default或本路由特例的profile选择 | SYNC；T08,T18 | —（本路由只管草稿/校验；不据此证明真设备行为） | [routers.py:L772–820](../../backend/routes/routers.py#L772) |
| **R025** `GET /api/routers/gateway-forwarding` | **S01** 已关联Mihomo forwarding状态<br>**S02** 多router共享Agent聚合refs<br>**S03** 无linked空列表<br>**S04** 部分不可达entry保留错误<br>**S05** Agent本身绑定beta但当前alpha router明确关联，仍按link授权 | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** Agent不存在/错服务/离线/Docker/低版本以entry问题呈现<br>**F11** 目标GET非JSON/连接超时降级不能伪ok | **B01** current profile授权；其他profile关联不算本域授权<br>**B02** 同Agent去重维持router序<br>**B03** no-store<br>**B04** GET不写OS<br>**B05** alpha/beta隔离、query/header/default或本路由特例的profile选择 | FORWARD；T20 | 需实验网 **L5-H**；见L5表 | [routers.py:L132–154](../../backend/routes/routers.py#L132) |
| **R026** `POST /api/routers/gateway-forwarding` | **S01** 精确{agent_id}当前profile linked网关开启并持久化<br>**S02** 已经开启幂等<br>**S03** Agent绑定另一profile但当前profile有router link允许persist | **F01** A1<br>**F02** A2<br>**F03** A3<br>**F04** A4<br>**F05** A5<br>**F06** A6<br>**F07** A7<br>**F08** P1空间不存在404<br>**F09** P2空间ID非法400<br>**F10** 非对象/字段多缺/agent_id非str400<br>**F11** 未linked当前profile404<br>**F12** Agent不存在/错服务/离线/Docker/低版本/forwarding布局guard MonitorError按status<br>**F13** device超时/redirect/过大/坏响应按MonitorError不泛500 | **B01** 空agent_id404<br>**B02** 只有其他profile有router link不能授权；当前profile的link可指向另一profile Agent；多router同Agent一次操作<br>**B03** 先应用后断连需再GET确认；OS状态/重启保持fake无法证明<br>**B04** alpha/beta隔离、query/header/default或本路由特例的profile选择 | FORWARD；T20 | 需实验网 **L5-H**；见L5表 | [routers.py:L132–154](../../backend/routes/routers.py#L132) |

## 设备协议：请求序列、分批、回读与补偿

fake为**有状态设备HTTP服务器**，两种vendor分别维护有序表、完整配置字段、运行字段、ID分配、session/cookie、权限、引用计数、journal及并发外部写。固定seed以外不从Go生成期望。每操作至少注入：收到前拒绝、应用一部分、全应用但丢响应、回读不一致、补偿再次失败；记录每次method/path/headers/JSON/timeout与实际设备状态。`/execute`只实现本清单所需脚本合约/预录输出，不冒充全功能RouterOS解释器；脚本文本仍独立逐字节比较，语法/权限/运行实际留L5。

以下ROS路径均在`<endpoint>/rest`下，Basic auth来自面板保存账号；iKuai都是POST `/Action/call`，表里的`func/action/param`指其JSON三字段。不能把两种vendor的字符串布尔、数字ID、空对象/列表或只读字段统一归一掉。

| 协议 / 操作 | 面板发出什么 | 设备返回/读回/分批 | 失败、补偿与fake行为 | Python |
|---|---|---|---|---|
| D01 ROS HTTP/权限 | 所有GET/PUT/PATCH/DELETE默认10秒、无redirect、Session.trust_env=false、verify_tls按开关；写body JSON | GET表必须list[dict]，单行dict；204空对象；2xx JSON按后续契约校验 | 401认证；403权限提示；GET404 MissingResource；DELETE404补偿视已无；写5xx/断连/非JSON为UncertainWrite，GET为RouterError；4xx明确拒绝；500含not enough permissions脚本可安全fallback。fake无write账号、TLS坏证书/开关、3xx带secret Location、半包、接收后断连均测 | [backend/routers/routeros.py:83](../../backend/routers/routeros.py#L83) |
| D02 ROS probe | GET /system/resource；GET /ip/route、/ip/firewall/nat、/ip/firewall/address-list?list=cf-probe | resource version>=7.1/model，子表可读才列对应capabilities；无写 | 旧/坏version、resource非dict失败；子表403/404只省capability，probe成功不证明写权限。HTTP服务REST版本差别需L5-A | [backend/routers/routeros.py:162](../../backend/routers/routeros.py#L162) |
| D03 ROS inspect | GET /ip/route、/ip/firewall/nat、/ip/firewall/mangle；仅被NAT/Mangle引用与自有列表按D08读取；有out-interface条件才增读/routing/rule、/ip/vrf、/interface/list及member等证据 | route稳定证据/immediate-gw/active用于真实出口判断，NAT所有matcher与顺序；mangle等不可读仍unknown | fake main/VRF/policy/recursive/ECMP/invalid/inactive、interface-list include/exclude、动态list/DNS名、否定matcher/未知字段，不把不可证明转not_required；foreign大表不应无条件全下载 | [backend/routers/routeros.py:182](../../backend/routers/routeros.py#L182) |
| D04 ROS 单条静态路由 | 先全GET /ip/route；新增PUT /ip/route {dst-address,gateway,disabled,comment,routing-table:main}；改PATCH /ip/route/*ID四编辑字段；删DELETE /ip/route/*ID | 新建要可信新*.hex ID，后全表回读精确稳定snapshot，update保留distance/scope/pref-src等；delete证目标不存在 | 并发revision/高级字段变化409；unknown字段/动态/VRF/ECMP/递归/接口网关只读。失败fresh读，只删除证实本次新ID、只还原仍等于本次after的字段；delete重建需未被占用且唯一回读，丢新增ID不能按同cidr猜删 | [backend/routers/routeros_routes.py:221](../../backend/routers/routeros_routes.py#L221) |
| D05 ROS 批量静态/IPv6路由 | 连续同kind >=4启batch，最多100且script<=32KiB，重复目的/ID打断；POST /ip/route/print或/ipv6/route/print {.query:[dst-address=...,#OR]}最多200terms；POST /execute {script,as-string:""}；remove用POST <path>/remove {.id:逗号ID}最多500 | ret cf-id:index:*ID连续前缀、cf-err及cf-done；回读验证新ID不在before、scope/值正确；批删后按ID读确认已消失的逐个记undo | /execute确定拒绝安全单条fallback；已接受后超时无可信ID保留现场、明确rollback未完成；脚本前缀部分成功/缺cf-done/乱序重复或旧ID/移出scope/回读失败分别测；只重试读取最多3次间隔1秒，不盲重试未知写 | [backend/routers/routeros_batch.py:42](../../backend/routers/routeros_batch.py#L42) |
| D06 ROS 传统NAT免转换 | PUT /ip/firewall/nat {chain:srcnat,action:accept,disabled:false,src-address?,dst-address,comment,place-before?,out-interface?}；已有accept PATCH comment+按目的新建；批量通过/execute（100/32KiB） | 全NAT回读值、优先位置、ID；NAT transient invalid允许8轮0.25秒settle；顺序undo中保留next place-before | 未知NAT matcher必须保留，readonly .id/dynamic/invalid/bytes/packets/creation-time不得原样写回；每个source×dest组合失败只补偿能证实归属的项；丢ack/规则被移动/外部修改或原邻居消失不粗暴还原 | [backend/routers/routeros.py:697](../../backend/routers/routeros.py#L697) |
| D07 ROS NAT列表及旧规则迁移 | 准备cf-mihomo-nat-src/dst address-list成员与首位srcnat accept规则，NAT comment=cf-mihomo-nat；读取NAT/Mangle/filter/raw排他引用；旧cf-mihomo逐目的规则在新保护已验证后分批remove（500） | 一条精确shape规则、source排除网关、dest成员集合与comment、无额外引用；list delta独立改成员，不每CIDR添NAT | 自有依据是唯一规则精确shape+list+comment而非manifest；foreign成员保留/有不兼容成员或共享引用拒；迁移后回读失败恢复旧NAT原顺序。自动sync不做legacy迁移；8×0.25秒invalid settle与外部修改补偿独立测 | [backend/routers/routeros_nat_list.py:1](../../backend/routers/routeros_nat_list.py#L1) |
| D08 ROS scoped列表/摘要协议 | GET /ip/firewall/address-list?list=<编码名>；POST /execute scripts返回cf-row/cf-rows（每页2000）、cf-digest（每500行分片MD5后二次MD5）、cf-names；失联member按ID GET，>20个fallback整表 | digest计数与hash精确匹配才复用已知成员；字段按稳定key/ID顺序，runtime creation-time/timeout不入；成员被移list仍定位 | fake list名含quote/$/换行/分号，值含分隔符触发REST fallback；total变动/缺marker/digest变化/无法解析不能把错缓存当证据；cf-names失败拒，不忽略冲突。只伪造摘要会测试失效，必须从fake表真实计算或预录自洽状态 | [backend/routers/ros_address_lists.py:35](../../backend/routers/ros_address_lists.py#L35) |
| D09 ROS 来源策略bundle | GET /routing/table、/ip/firewall/mangle、/ip/route、/ip/firewall/nat、/routing/rule、/ip/firewall/filter/raw、/ip/vrf、/routing/settings及D08；创建PUT /ip/firewall/address-list {list,address,comment,disabled:false}、/routing/table {name,fib:true}、/ip/route {dst-address:0.0.0.0/0,gateway:Mihomo@main,routing-table:name,comment,disabled:false}；可选NAT accept {src/dst-address-list,out-interface}；Mangle私有chain return(bypass)→mark-routing {new-routing-mark,passthrough:false}，最后prerouting jump {src/dst-address-list,dst-address-type:!local,jump-target}，common comment/disabled；激活入口后才撤旧bundle | policy-rules查找顺序（无字段再查resource区分<7.22）；ownership精确manifest稳定config+ID；route egress与prepare_static/NAT先读回，外部引用检测 | unsupported顺序/同名foreign/来源重叠最具体策略/直连覆盖/fasttrack/VRF/raw未知均按guard；地址批200/16KiB、remove500。失败逆journal恢复、重建ID remap和依赖顺序；恢复失败保留manifest现场，不只name/comment认领 | [backend/routers/ros_policy_write.py:639](../../backend/routers/ros_policy_write.py#L639) |
| D10 ROS DHCP DNS | GET /ip/dhcp-server/{network,lease,option,option/sets,matcher}及/ip/dhcp-server；唯一enabled scope含Mihomo；PATCH network/*ID {dns-server:单目标,dns-none:false} | 完整network属性作为expected，servers/options/matchers/租约override可阻止；写前再查scope/路由与NAT，写后checked_dns_target+DNS明确单目标 | RADIUS/option6/custom option-set/静态lease DNS等override、scope重叠/变化、坏IP、readonly字段/未知option格式逐支；失败PATCH只恢复已写字段，外部snapshot变化拒覆盖 | [backend/routers/routeros.py:293](../../backend/routers/routeros.py#L293) |
| D11 ROS DNS guard/Netwatch | 按D10确定LAN与OxiDNS/VIP；先PUT bypass list/hairpin srcnat UDP+TCP，再首位dstnat53双规则，可选filter853 reject TCP/drop UDP，最后netwatch tcp-conn53 interval10s timeout2s startup30s和up/down scripts | manifest中的role/path/id/config与设备exact值/顺序；hairpin/bypass先于hijack；读NAT/filter/netwatch/list；failover自行disabled变化可接受 | fake OxiDNS offline/Netwatch无API/foreign同名/手动改matcher/前方53规则/单项写错及rollback。down确认15s才pause并清相关conntrack；up enable并保护exempt；fake能核文本/模拟status，真实Netwatch脚本、conntrack与DNS连通只能L5-C | [backend/routers/dns_guard.py:246](../../backend/routers/dns_guard.py#L246) |
| D12 ROS IPv6 Fake-IP | GET /ipv6/route、/ipv6/address、/ip/address及必要filter/list诊断；结合Mihomo Agent IPv6证据，PUT /ipv6/route {dst-address:合成ULA,gateway:<IPv6%interface>,...}或DELETE已核自有ID | host地址保留host bits、LAN/interface及稳定route；owned manifest与borrowed手工/legacy区分；dns.ipv6关但top ipv6仍开可仍需路由 | ULA/TUN/YAML无效、下一跳不确定、同前缀冲突/多目标、foreign路由被改不接管；失败逆undo；valid/preferred剩余lifetime不是stale原因，interface改必须stale；实际ND/TUN流量L5-D | [backend/routers/ipv6_fakeip.py:30](../../backend/routers/ipv6_fakeip.py#L30) |
| D13 ROS IPv6公网规则路由 | 所选静态rule sets的合成global分类IPv6按Mihomo IPv6下一跳生成/ipv6/route；多条复用D05批量create/remove；关闭只撤本manifest自有 | snapshot所有选中routes、与fakeIP不混IPv4 NAT/return；owned/borrowed/conflict/skip稳定；回滚manifest新ID | 下一跳或归属不明只跳过该阶段，不猜路由/不阻整次其他安全stage；invalid多余route/readbackchanged逐项验证；真实IPv6出口/递归/邻居解析L5-D | [backend/routers/ipv6_routes.py:263](../../backend/routers/ipv6_routes.py#L263) |
| D14 ROS IPv6 DNS/RA | 先重查DHCP仍明确OxiDNS，再GET /ip/address与/ipv6/nd；仅专用LAN enabled/static ND条PATCH /ipv6/nd/*ID {advertise-dns:no} | readback所有ND项与scope；不修改地址/默认网关通告；共享all行不能直接关，返回带完整保留参数的fix_command | dynamic/invalid/未知bool/同接口多行/共享all未覆盖/跨LAN/defaults不明只skip该stage。写前/后DHCP切换或ND高级字段变化拒并补偿；客户端真实RDNSS缓存/RA优先级L5-D | [backend/routers/ipv6_dns.py:148](../../backend/routers/ipv6_dns.py#L148) |
| D15 ROS IPv6 PD guard | GET /ipv6/address、/ip/address、/ipv6/dhcp-client、/system/script、/system/scheduler、/ipv6/firewall/address-list?list=cf-pd-last、/ipv6/nd/prefix；PUT/PATCH watch script{name,source,policy:read/write/test,...}/poll scheduler与client script HOOK；disable逆序clearhook→scheduler→script→runtime | 唯一pool/LAN/prefix-request client；可沿用manual完全模板、不写不认领；rewritten才manifest；撤回后再次扫运行state避免并发残留 | 多pool/client/LAN、foreign同名/既有script其他内容、老HOOK迁移、shared HOOK只沿用不删、shutdown race/undo失败；counter/lease状态不误判，保持只写allowlist。30秒scheduler、10×1s PD hook、旧前缀deprecate/恢复/过期的真脚本效果L5-E | [backend/routers/ipv6_pd_guard.py:338](../../backend/routers/ipv6_pd_guard.py#L338) |
| D16 ROS 发现/主动探测 | 必读GET /ip/arp，可选/ip/address、/ip/dhcp-server、/ip/dhcp-server/lease，必要/interface/list/member。最多32候选、6workers：POST /ping {address,count:"2",interval:"300ms",arp-ping:"yes",interface}，失败可fallback ICMP | HTTP200 list中received整数>0有reply；ARP状态/租约fresh20min与renew margin5min，saved IP优先；表总budget/每probe最多4秒 | 500 not enough permissions→PingNotPermitted只notes不误offline；ARP失败/静默、低budget、>32上限保留recent，optional表缺使未出现unknown；无LAN接口拒该源。真ICMP/ARP和租约时序L5-F | [backend/routers/ros_clients.py:170](../../backend/routers/ros_clients.py#L170) |
| D17 ROS 固定IP | 读lease/server/network/address+来源ARP；动态POST /ip/dhcp-server/lease/make-static {.id}→GET原ID→PATCH comment加唯一cf-client后缀；无租约PUT lease {mac-address,address,server,comment,disabled:false}；delete DELETE *ID | 租约字段/identity一致，可信新ID或unique operation suffix恢复归属；readback比所有稳定字段；delete只自有完整binding | make-static丢ack重查同ID，转换后字段变/错目标保留现场；新增丢ack可unique suffix找回再补偿但仍报失败；动态转换补偿移除静态预留并提示续租IP可能变，不伪“恢复原动态行”。删除rollback重建换ID只在未占用时，DB同步新binding。真正DHCP续租L5-G | [backend/routers/routeros_fixed_ip.py:237](../../backend/routers/routeros_fixed_ip.py#L237) |
| D18 iKuai HTTP/login/session | POST /Action/login {username,passwd:MD5(password),pass:base64("salt_11"+password)}；cookie session，/Action/call {func_name,action,param}；默认10秒，无redirect/trust_env，TLS开关 | 登录有code只code=0成功（Result=10000仍可失败），无code旧Result登录之后call必须4.x code0/results dict；1008重新登录并原请求重发一次 | 1001/1002/1003/1008明确_Rejected；其他code/details逐字段报错；3.x Data/Result格式拒；200空body是超大拒绝提示；非JSON/HTML/TLS/ConnectTimeout/ReadTimeout/ConnectionError区别，redirect仅origin不泄漏Location凭据。写超时不可自动当未执行重发。**[O6]** 写入的确定/不确定划分与ROS不同：只有HTTP 200+JSON且`code≠0`是确定拒绝（`_Rejected`，不补偿）；HTTP非200（**含4xx**）、非JSON、空200（超大请求被丢）、超时、断连都按“回复丢失”处理，undo带`confirmed:false`，靠唯一tagname回找。1008只在写入上重登重发一次，fake对回1008的写**不得**落表 | [backend/routers/ikuai.py:202](../../backend/routers/ikuai.py#L202) |
| D19 iKuai probe/完整分页读取 | show sysstat {TYPE:verinfo}；probe各static_rt/nat_rule/route_object/stream_ipport {TYPE:"total,data",limit:"0,1"}。inspect _list_all每页{TYPE:"total,data",limit:"offset,1000"}；active static_rt_table同样TYPE total,data且ids=false；dhcp_static才使用static_total/static_data前缀 | version4.x；full total为非负int且分页不变、每页恰好应有数、正整数唯一id；route_object可ids=false但实际写需有效ID；group引用gid/addr真实一致 | 0/999/1000/1001/2000/最后页、total bool/负数/变化/重复ID/短页；只读未知表optional按函数降级不吞必需列表；fake不能只固定第一页，页间并发修改必须触发保护 | [backend/routers/ikuai.py:270](../../backend/routers/ikuai.py#L270) |
| D20 iKuai 单条路由/发布批次 | static_rt add/edit/del；add payload enabled yes/no,tagname(手动RT+13hex),ip_type:"4",interface:auto,dst_addr,gateway,netmask,prio,comment；edit完整ROUTE_FIELDS，del {id:int}；先fresh列表、后完整回读 | 数字ID与string API ID映射，tag<=15唯一；netmask显示后缀去掉、status runtime不写；release batch静态快照仅本阶段，连续new routes确认后才能后续delete/edit，rollback末尾一起确认 | _Rejected不当未知补偿；丢ack按唯一tag确认，改/删undo完整snapshot且外部变化保留。新增后读回失败/重复cidr/新增identity被占用/回滚改ID逐项。静态route和active table不一致可warning不伪内核已清 | [backend/routers/ikuai_routes.py:178](../../backend/routers/ikuai_routes.py#L178) |
| D21 iKuai IP分组 | route_object add/edit {id?,group_name,type:0,group_value:[{ip,comment}]}、del{id}；每组<=2000且编码后<=92KiB，名字cf_pa/pm/nat_N，最多16组引用、index<=9999 | 首条member comment角色marker才owned，tagname设备会丢不作依据；用户其他条notes保留，重读gid/ref_count与被引用完整表；group拆分按实际请求字节 | foreign同名预留号不改/不引用；丢marker但本role受管规则引用可重新标记，标记修复不反向撤；隐藏ACL/流控等ref_count额外引用拒改/删；partial group writes及失联对账，undo按digest不猜删；恢复gid变更需rebind规则引用 | [backend/routers/ikuai.py:913](../../backend/routers/ikuai.py#L913) |
| D22 iKuai 端口分流策略 | stream_ipport add/edit完整POLICY_FIELDS：enabled yes,type:1,nexthop,prio:POLICY_PRIORITY,protocol:any,src_addr custom:[Mihomo] inv:1,dst_addr custom或object refs inv:0,time每日全日及interface/iface_band/mode/area/ports等；del{id} | 来源排除网关避免DIRECT回环，自动与手动迁移规则分开；建立/读回policy成功才删旧静态；读group object refs/gid/名字和所有匹配器 | 外部规则先匹配/未知字段/同名不安全及partial增改、被覆盖`time`/ports、restore gid替换/字节过大逐项；失败undo完整旧form和引用，不把空{}当[]或只比dst | [backend/routers/ikuai.py:724](../../backend/routers/ikuai.py#L724) |
| D23 iKuai NAT免转换 | nat_rule add/edit action=filter,protocol:any,iinterface/ointerface:any,src_addr/dst_addr {custom,object},ports结构,nat_addr/nat_port,inv flags,tagname/comment及原表单其他字段；del{id} | 免转换src/dst/group集合、enabled/引用/优先条件；用户foreign group引用保留；undo回旧行 | 编码先%再空白与引号/$/反引号/&/;/反斜杠/竖线/单引号，comment换行%0A；200 code0但设备丢字段仍回读失败；完整payload不得误抹unknown条件，跨action补偿与partial分组状态比 | [backend/routers/ikuai.py:707](../../backend/routers/ikuai.py#L707) |
| D24 iKuai DHCP DNS | show dhcp_server/dhcp_static等完整表；唯一scope；dhcp_server edit精确DHCP_FIELDS，dns1=dns2=同一OxiDNS/VIP，不留空继承；恢复也发完整before form | DHCP_OPTIONS整数/各opt值、range/interface/lease/gateway/wins/domain等原样保留；静态binding独立DNS阻止；写后attributes==payload且blockers空 | scope缺/多/变化、未知字段、bool冒int、表单缺字段、lease override逐支；本次写失败尝试精确before恢复与回读，restore失败RouterRollbackError；不能只PATCH DNS两项导致厂商默认重置 | [backend/routers/ikuai.py:375](../../backend/routers/ikuai.py#L375) |
| D25 iKuai 客户端发现 | show monitor_lanip与dhcp_lease分页{TYPE:"data,total",limit:"offset,1000"}，最多10页；show lan {TYPE:"data,total",limit:"0,1000"}读ip_mask；补读fixedIP适配器 | terminal按2min内流量在线，lease与hostname/MAC补全、LAN范围/own识别；优先展示已保存设备 | monitor必需失败该源fail，lease或LAN失败notes/未知；一万条上限/total/page错型/空pages/重复IP/无MAC/坏IP都测；真实在线不由fake证明，L5-F | [backend/routers/ikuai_clients.py:33](../../backend/routers/ikuai_clients.py#L33) |
| D26 iKuai 固定IP | show dhcp_static（TYPE static_total,static_data）+dhcp_server及monitor/lease来源；dhcp_static add {interface,gateway,ip_addr,mac,dns1:"",dns2:"",enabled:"yes",hostname:"",comment,tagname:"cfip_"+10hex}；del{id:int}；恢复add完整before | 顶层rowid不作为可靠results；全static表按unique tag和known_ids定位，再按ID精确回读DNS等；delete仅cf-client自有，restore后绑定新ID | 丢add ack尝试找唯一tag后undo；tag存在但字段外改保留现场；delete ack丢且readback已无可成功；IP/MAC/tag被占用不能恢复；只读runtime termname/ip_addr_int/hostname按来源校验，不能写不明字段。L5-G真实DHCP | [backend/routers/ikuai_fixed_ip.py:165](../../backend/routers/ikuai_fixed_ip.py#L165) |
| D27 跨域Agent回流 | Mihomo GET /api/return-routing 15秒，PUT /api/return-routing {router_ip,table,priority,mark,safety_version,proxy_dst?(protocol1)} 60秒，DELETE同路径 query restore_legacy 60秒；Bearer本Agent token | status safety_version/protocols、managed spec/active/routes/rule/iptables与安全allocation；proto1按目的/2按router，返回并回读；发布第一stage先保护、关闭最后cleanup | Agent offline/缺hostport/404旧版/不安全blockers/表号或mark占用/协议或分配变化拒；clear后后续失败可恢复previous spec；其他profile仍用同Agent则不能清。fake记录顺序与旧新配置，真实nft/iprule回流L5-B且依agents L4 | [backend/routers/release.py:29](../../backend/routers/release.py#L29) |
| D28 自动同步规则源 | 先成功手动发布baseline；POST rule-sync启动后fake URL GET/本地inline缓存；解析后只受允许IP delta写ROS，继承D05/D07/D09/D13 | 单源<=4MiB、总<=16MiB、<=100000行、<=128选择、每族有效IP<=20000；classical/domain/ipcidr逐形态，版本hash/域名计数/ignored/invalid；domain-only变更可只cache | 空/HTML/非UTF8/无效IP或未知type、selected missing/disabled/unpublished source、IP全部消失check_updates均拒；下载失败failed可重试，control/ownership变paused（**[O6]** 更正：超过4MiB或30秒总时限抛的是RouterError不是DownloadError→**paused**；HTTP错误码、连接错误、`utf-8-sig`解码失败、内容校验失败→failed。下载经`request_rule`，`system_config.rule_fetch_proxy`存在时走代理，超时(5,10)；同一轮按(url, system_config)缓存最多16MiB）；不能顺便push主配置、改DHCP/guard/return或迁旧NAT。伪download不证明真实负载/内核增删L5-I | [backend/routers/rule_sync_rules.py:145](../../backend/routers/rule_sync_rules.py#L145) |
| D29 关联代理网关forwarding | GET列表读Agent /api/linux-gateway/status；POST持久化在旧版先status检查layout，新版可直接POST /api/linux-gateway/forwarding 空{}，Bearer，(3,45)秒/512KiB，无redirect/proxy | GET entry与refs、forwarding enabled/persistent/safe evidence，POST后返回该项；当前profile router关联授权，不以Agent绑定profile拒绝已有router link授权 | 错服务/离线/Docker/低版、未确认layout状态、已生效未持久、外部冲突、应用后丢response按MonitorError；fake持内存enabled/persistent可对账；真实重启sysctl与router同链路流量L5-H | [backend/agents/linux_gateway.py:625](../../backend/agents/linux_gateway.py#L625) |
| D30 Mihomo IPv6主机证据 | router inspect按profile核Agent后GET /api/ipv6-readiness，Bearer，(3,45)秒，读取interface/index、addresses/routes与published/client能力证据 | 同时fakeIP和公网routes一次preview复用同一host读取；profile/service/enabled要匹配，error反映到stage unknown/skip/blockers | 低协议404/坏格式/超大/地址动态变化与loopback/link-local scope；host不属于本空间不可请求。fake证明payload归并/序列，不证明真ND、PD、RA或客户实流，见L5-D/E | [backend/routes/routers.py:339](../../backend/routes/routers.py#L339) |

设备写入字段附表（D20–D24的“完整form”必须逐字段检查）：

| func / 对象 | 写入字段与类型 | Python |
|---|---|---|
| iKuai static_rt | `id`仅edit/del正整数；`enabled,tagname,ip_type,interface,dst_addr,gateway,netmask,comment`为str，`prio`为int。edit保留全form，runtime `status`不写 | [ikuai.py:40](../../backend/routers/ikuai.py#L40) |
| iKuai nat_rule | `id?,enabled,tagname,comment,action,protocol,iinterface,ointerface,src_addr,dst_addr,src_port,dst_port,nat_addr,nat_port,src_addr_inv,dst_addr_inv`；四地址/端口字段是`{custom:[],object:[]}`，object refs有`gid/gp_name`，inv是数字0/1。其他原字段按NAT_FIELDS原样还原 | [ikuai.py:41](../../backend/routers/ikuai.py#L41) |
| iKuai stream_ipport | `id?,enabled,tagname,interface,src_addr,dst_addr,iface_band,comment,time,prio,mode,type,nexthop,area_code,src_port,dst_port,protocol,dst_type,src_addr_inv,dst_addr_inv`；src/dst/ports/time是custom/object结构。自动规则type=1、prio=0、src_inv=1、dst_inv=0；新规则time custom为weekly1234567/00:00–23:59 | [ikuai.py:45](../../backend/routers/ikuai.py#L45)、[base.py:321](../../backend/routers/base.py#L321) |
| iKuai dhcp_server | `id,enabled,tagname,interface,phy_ifnames,addr_pool,exclude_pool,netmask,gateway,next_server,dns1,dns2,domain,wins1,wins2,lease,delay,check_addr_valid,check_relay_only`；每个n∈15/28/43/60/66/67/80/119/125/128/138/121还有`opt_type<n>`及`opt<n>`，type/lease/delay/check为整数。改DNS保留其余完整值 | [ikuai.py:62](../../backend/routers/ikuai.py#L62) |
| iKuai dhcp_static | `id?,enabled,tagname,interface,mac,gateway,dns1,dns2,ip_addr,comment`；新增另发hostname空值，runtime termname/ip_addr_int/hostname读回校验按adapter允许范围，不能复制未验证字段 | [ikuai.py:65](../../backend/routers/ikuai.py#L65)、[ikuai_fixed_ip.py:165](../../backend/routers/ikuai_fixed_ip.py#L165) |
| ROS PD资源 | 新script `{name,source,policy,comment}`；scheduler `{name,interval:30s,start-time:startup,policy,on-event,comment,disabled:false}`；hook只PATCH dhcp-client `{script:HOOK}`。update只写源码明确字段；delete用已核归属ID，不能带counter/lease runtime | [ipv6_pd_guard.py:338](../../backend/routers/ipv6_pd_guard.py#L338) |

所有D适用操作再分别模拟：401/403/404/405/409/422/500/503/504、iKuai code1001/1002/1003/1008/未知code、3xx、空body/非JSON/数组/丢必需键、Content-Type不符但JSON有效、header后慢body、连接拒绝/超时/半包、收到后应用再断连。**状态码不是统一成功标准**：ROS单行/print/execute、iKuai show/write/readback与Agent返回各自判断。fake每次补偿前插外部写、ID复用、禁用/动态/invalid变化、同目的新route、同名foreign、reference新增；必须能拒绝不合法写入或只读字段，不能自动忽略一切readonly让错误payload也成功。

读表顺序/批次边界影响安全：记录并断言“替代路径与NAT保护已回读→撤旧策略/路由”“DHCP前再次验证routes/NAT→改DNS”“RA前复核DHCP”“hairpin/bypass在hijack前”“group存在且已回读→rule引用→旧group不再引用才删除”。iKuai整请求常规<=96KiB，group更严92KiB；restore含>1000文字entries时timeout=10+0.003×src/dst custom条目数秒，不硬套10秒误超时。每一batch的部分完成与下一batch停止/回退必须落入result.log/rollback和fake表。


## 生成计划、脚本与修复命令形态

以下从公开R入口观察plan/report/DNS预览和fake捕获的设备请求体。两种实际driver均supports_write=true、继承的render_script为空，**正常顶层plan.script必须是空字符串**，不以私有替身driver硬造一个HTTP成功脚本分支；manual-driver仅列不可HTTP达审计哨兵。批量/Netwatch/PD的实际script字段与修复命令必须完整比，不能忽略“脚本太长”的内容。

- **G01** ROS空/基础计划：stages有序key/title/manual/enabled/note/ops、checks/fingerprint/blockers/warnings/empty、script=""；有/无gateway和blocked早返回形态分别。 来源 [backend/routers/plan.py:505](../../backend/routers/plan.py#L505)。
- **G02** iKuai manage_return=false静态计划与true端口分流计划；新建policy后删旧static；auto/手工迁移add-only分组、空与多千条cidr分别。 来源 [backend/routers/plan.py:270](../../backend/routers/plan.py#L270)。
- **G03** return protocol1(proxy_dst含被跳过目的的原保护交集)/protocol2(按router)两spec，自动table/priority/mark、冲突提示、关闭后cleanup最后stage；不带管理开关的disabled stage不能含写。 来源 [backend/routers/plan.py:647](../../backend/routers/plan.py#L647)。
- **G04** 静态route missing/adoptable/synced/conflict/extra、cf管理备注与foreign保留；目的prefix重叠/同网段多下一跳/active残留warning；单路由preview before/after/empty/fingerprint。 来源 [backend/routers/route_editor.py:119](../../backend/routers/route_editor.py#L119)。
- **G05** ROS route批量/execute：1/3条单REST，4/100/101多batch，32KiB边界、重复dst/ID打断；cf-id/err/done序号、UTF8字节预算、disabled true/false转yes/no、$和换行quote。 来源 [backend/routers/routeros_batch.py:75](../../backend/routers/routeros_batch.py#L75)。
- **G06** ROS NAT source fragments×dest笛卡尔展开、place-before、out-interface；32KiB/100规则脚本，NAT settle与partial新ID列表，不把foreach次序重排。 来源 [backend/routers/routeros_batch.py:525](../../backend/routers/routeros_batch.py#L525)。
- **G07** ROS NAT-list目标/源列表集合、首位一条规则、legacy迁移计划的警告和先新后旧顺序；只增/只减/mixed同批次、rollback原规则排序。 来源 [backend/routers/routeros_nat_list.py:1](../../backend/routers/routeros_nat_list.py#L1)。
- **G08** scoped address-list脚本：2000行分页、cf-row/cf-rows含字段计数和分隔符处理、500行MD5分片cf-digest、cf-names去重输出、REST fallback，中文/quote/$/换行list名。 来源 [backend/routers/ros_address_lists.py:43](../../backend/routers/ros_address_lists.py#L43)。
- **G09** ROS policy private名称版本、source最具体smart/direct减法、destination去重、table/mark/entry/private-chain/NAT/list完整bundle；跨run新旧资源隔离、迁静态prepare_static与static_nat_checks先于撤回。 来源 [backend/routers/ros_policy.py:1](../../backend/routers/ros_policy.py#L1)。
- **G10** ROS地址批量脚本16KiB/200成员，cf-done:失败数:首错，500 member remove；空集合/一/200/201/最大字节与+1，特殊字符quote/转义不可截断。 来源 [backend/routers/ros_policy_write.py:149](../../backend/routers/ros_policy_write.py#L149)。
- **G11** iKuai route/NAT/policy/group所有request JSON先encode_value；%先编码、comment CR/LF→%0A且strip，其他whitespace→%20，"`$&;\|'逐项；readback decode一次，不能重复unquote把literal %变掉。 来源 [backend/routers/ikuai.py:69](../../backend/routers/ikuai.py#L69)。
- **G12** iKuai group首member marker与用户notes，2000条/92KiB分片、最多16refs、reserved index与foreign group；完整payload中type=0和空custom/object结构；恢复原member顺序/notes。 来源 [backend/routers/ikuai_groups.py:87](../../backend/routers/ikuai_groups.py#L87)。
- **G13** DHCP DNS ROS dns-server明确值/dns-none=false，对比iKuai dns1=dns2目标的全表单；single/HA VIP、当前空/双/继承值、option override阻止，其他DHCP字段逐叶保留。 来源 [backend/routers/dhcp_dns.py:110](../../backend/routers/dhcp_dns.py#L110)。
- **G14** DNS guard bypass/hairpin UDP+TCP/hijack UDP+TCP、block_dot on/off、failover on/off的每种组合；resources role/path/values及manifest、启/更新/撤回计划，HA VIP+两真实host豁免。 来源 [backend/routers/dns_guard.py:246](../../backend/routers/dns_guard.py#L246)。
- **G15** Netwatch up/down脚本：down delay15s再查down才disable；up enable，conntrack只清对应53旧流，已过期remove on-error继续，exempt /30及更细最多64host；interval10s/timeout2s/startup30s逐字节。 来源 [backend/routers/dns_guard.py:293](../../backend/routers/dns_guard.py#L293)。
- **G16** IPv6 Fake-IP开/关/dns.ipv6模式切换，ULA range+TUN、下一跳ULA%LAN与link-local%LAN两形态（新建优先唯一LLA，已证旧ULA可保留）、owned/borrowed/冲突；host/interface稳定证据与remaining lifetime区别。 来源 [backend/routers/ipv6_fakeip.py:30](../../backend/routers/ipv6_fakeip.py#L30)。
- **G17** IPv6规则集公网routes按选中规则、最小聚合与owner resources；owned增删/borrowed沿用/next hop未知skip/关闭withdraw。与IPv4 NAT/return检查彻底分开。 来源 [backend/routers/ipv6_routes.py:263](../../backend/routers/ipv6_routes.py#L263)。
- **G18** IPv6 DNS report.fix_command：只共享all时生成专用LAN /ipv6 nd add advertise-dns=no并保留其余可复制属性；有专用LAN/无安全override时空；接口空格/quote/$特殊字符CLI转义。 来源 [backend/routers/ipv6_dns.py:136](../../backend/routers/ipv6_dns.py#L136)。
- **G19** forward_hint fix_command：IPv4 /ip firewall filter、IPv6 /ipv6 firewall filter 两family，same-port明确drop才给命中规则前accept命令（可含place-before）；ok/unknown命令空。只读提示不能自动执行或纳入发布fingerprint。 来源 [backend/routers/forward_hint.py:90](../../backend/routers/forward_hint.py#L90)。
- **G20** PD watch_source：合成LAN/pool/hold3600插值、当前/旧prefix保存、重新成为当前时恢复preferred、旧prefix preferred=0、配置valid-lifetime=10m与脚本hold=3600清理门禁分开验证，实际RA重发寿命留L5、日志及清理；完整source不可只比参数hash。 来源 [backend/routers/ipv6_pd_guard.py:66](../../backend/routers/ipv6_pd_guard.py#L66)。
- **G21** PD scheduler/hook：name cf-pd-watch/cf-pd-poll、30秒scheduler/on-event/policy、pd-valid=1后10×1秒hook、旧HOOK升级/shared HOOK沿用/foreign script阻止；关开撤回步骤与残留清理。 来源 [backend/routers/ipv6_pd_guard.py:338](../../backend/routers/ipv6_pd_guard.py#L338)。
- **G22** policy-settings/preview：空targets dns_content=""，非空为清洗secret后的OxiDNS YAML，native/template可编辑形态与完整validation；targets顺序/元数据、router_plan、config/oxidns/draft revision；不因DNS预览text忽略router plan。 来源 [backend/routes/routers.py:646](../../backend/routes/routers.py#L646)。
- **G23** router_split全TUN与legacy marker、rule_ip_presence缓存/源选择属于本领域helper但公开生成入口在config/agents；将config/Agent生成/导出公开请求作为跨域补测，检查marker合法/无效/过滤TUN冲突/生成缺TUN以及不凭空补partial。此组不增加26路由分母；无法从本领域入口触发的guard只能标N/A并交对应域。 来源 [backend/common/router_split.py:95](../../backend/common/router_split.py#L95)。
- **G24** 固定IP预览actions/token与设备创建文本：ROS cf-client comment唯一operation后缀、iKuai cfip_≤15 tag；操作描述中文、MAC/IP/vendor/action序；600秒签名payload只含intent/operations和connection digest，不能含device密码，后续verify不可mask。 来源 [backend/common/client_fixed_ip.py:152](../../backend/common/client_fixed_ip.py#L152)。

## 长时间运行、轮询、并发与上传下载边界

本领域26路由没有SSE、WebSocket、文件上传下载端点。长操作仍是有限JSON响应，不能把单次返回视为已验证设备安全：一次apply内部多设备/多表/多batch/Agent串行I/O；rule-sync先202后worker；discovery/pending有并行读取/cache；Netwatch/PD脚本由设备未来事件运行。需要公开进程+有状态fake+轮询/重启、设备transcript与最终DB/文件观察。核心的日志/Agent流式接口不属于这26条，不以设备REST fake宣称覆盖其帧协议。

- **T01** pending缓存序列：首次→cached→refresh=1/true→单源unknown后必须重读；router/profile revision/Agent相关字段变化失效，64Agent/65淘汰；8并发workers、总25秒、共用Future35秒超时504；浏览器断开后thread结果不污染新signature。 来源 [backend/routers/pending.py:199](../../backend/routers/pending.py#L199)。
- **T02** 设备草稿：GET previous/name/mode→两页面同时改同IP冲突409、不同IP保留；旧policy workspace同时names_revision/revision双guard；只改备注不改OxiDNS、不自动推，清行留router预留。事务失败两字段不能半存。 来源 [backend/common/client_devices.py:40](../../backend/common/client_devices.py#L40)。
- **T03** discovery缓存：默认重复读、60秒仅标stale、refresh=1、refresh=true不强制（Python只认1）、128profile/129淘汰；同时两refresh共用一次请求，全部fresh源失败保旧快照并携refresh_error；部分成功替换后其余saved IP应unknown。 来源 [backend/common/client_discovery_cache.py:34](../../backend/common/client_discovery_cache.py#L34)。
- **T04** discovery并行/证据：慢源20秒不挡其他源，必需ARP vs optional lease/server/address失败，ROS32候选saved优先/6workers/4秒预算与no-test账号fallback；IKuai2min流量阈值、saved CIDR最具体策略、Infrastructure排除与坏MAC/重复IP合并。 来源 [backend/common/client_discovery.py:44](../../backend/common/client_discovery.py#L44)。
- **T05** 固定IP token：preview动作1/64/65、签名篡改/跨profile/600秒临界/过期/百万长度边界；HTTP apply只收profile_id/token，等待锁后重新plan同operations，来源IP/MAC/DHCP server和connection任一变就拒；不得由Go私有函数造token或假成功。 来源 [backend/common/client_fixed_ip.py:171](../../backend/common/client_fixed_ip.py#L171)。
- **T06** 固定IP多项事务：第N台写失败、全部写后DB设备/名字/binding变更冲突；逆序undo所有已完成项，外改自身before/after字段时保留，delete undo重建新ID同步binding；真实dynamic undo移除预留并提示续租影响，不能伪还原动态行。 来源 [backend/common/client_fixed_ip.py:200](../../backend/common/client_fixed_ip.py#L200)。
- **T07** 固定IP模糊结果：ROS make-static/PUT/PATCH/delete接收后丢ack，iKuai add/tag查回；与外部同MAC/IP写并发，读回失败/再次补偿超时/恢复binding提交失败；apply前后invalidate cache防在途discovery旧结果回填。 来源 [backend/routers/routeros_fixed_ip.py:237](../../backend/routers/routeros_fixed_ip.py#L237)。
- **T08** 锁/持久化：same endpoint跨profile/router共用job文件锁和operation锁，手动apply/probe/edit/delete/rule-sync/fixed-IP竞争；锁后reset_context再查record、DB commit guard；等待后record改/删要409不覆盖。轻量草稿GET不可误持写锁。 来源 [backend/routers/rule_sync_store.py:52](../../backend/routers/rule_sync_store.py#L52)。
- **T09** 完整发布阶段：return→routes→IPv6 fakeIP→IPv6 routes→NAT→DHCP→IPv6 DNS→DNS guard→PD→return cleanup；只出现适用stage。对每stage首/中/末请求注入失败，后stage不得继续，每已完成undo逆序且记录failed_stage/log/rollback；empty也记录last_release与baseline。 来源 [backend/routers/release.py:297](../../backend/routers/release.py#L297)。
- **T10** 批次边界与未知归属：ROS3/4/100/101、32KiB、200queryterms、500remove；地址199/200/201、16KiB、500remove；iKuai1000分页、2000group、92/96KiB、16refs；各批n项部分成功/ret marker丢失/回读短暂失败，fallback仅明确未运行时允许。 来源 [backend/routers/routeros_batch.py:102](../../backend/routers/routeros_batch.py#L102)。
- **T11** 策略迁移：private bundle新写和证明→入口激活→旧入口先撤→反序旧依赖；静态替代先NAT/路径回读→撤policy；前序fasttrack/raw/VRF/lookup order不确定阻止，foreign引用增加不误删。失败重新建ID时list/table/route/rule引用remap自洽。 来源 [backend/routers/ros_policy_write.py:639](../../backend/routers/ros_policy_write.py#L639)。
- **T12** NAT路径：缺保护/无需保护/无法证明三类，out-interface/list、否定地址、跳转chain、未知matcher、conntrack动态信息分别；legacy NAT合并先保护再remove旧，invalid最多8轮0.25秒；forward hint只提示，counter跳变不改fingerprint、stable matcher或顺序变化必须改。 来源 [backend/routers/routeros_nat_list.py:1](../../backend/routers/routeros_nat_list.py#L1)。
- **T13** 发布回滚与DB：本stage补偿+此前stage撤回都观察，补偿再次失败显式未完成；device成功但save_router_publication失败、manifest写失败、receipt写失败、webdav/notification故障区分。重开进程查看persistent manifests与receipt，不假设所有写天然事务。 来源 [backend/routes/routers.py:437](../../backend/routes/routers.py#L437)。
- **T14** DNS/IPv6事件：DHCP前即时再证route/NAT/scope，RA前再查DHCP；OxiDNS主备VIP/失联/配对变化，DNS guard启用/停机15秒pause/恢复；PD换prefix/回到旧prefix/hold过期/撤回时脚本仍在跑。fake证明协议/顺序，真正未来脚本和实流留L5。 来源 [backend/routers/release.py:363](../../backend/routers/release.py#L363)。
- **T15** probe权限矩阵：ROS只读用户resource和capability成功但apply写403；缺test的ping/缺execute的batch两者分别降级；iKuai login Result10000+非零code、3.x格式、session1008一次重登；last_probe失效只跟connection变，不跟纯name。 来源 [backend/routers/routeros.py:162](../../backend/routers/routeros.py#L162)。
- **T16** 手工路由多请求：GET revision→plan fingerprint→apply，对ROS/IKuai各add/update/delete/no-op；外部改任一高级字段/同目的新路由/ID复用，preview或写前409；写后读回错只保守补偿本次变更，不修改manifest/profile选择。 来源 [backend/routers/route_editor.py:119](../../backend/routers/route_editor.py#L119)。
- **T17** auto rule-sync生命周期：成功manual/empty apply建baseline→开启→POST202 queued→running→synced；同一全局worker竞争、timer与手动job互斥、入队后disable/删router需failed释放而非永久loading；重启running→paused，重新manual确认才resume。**[O6]** 精确时机：`running`只在下一次定时轮（启动后30秒起每30秒，X07）或任何一次`sync`开始时才改成paused；在那之前POST返回409并带陈旧的running消息。孤儿`queued`不转paused而是照常执行。 来源 [backend/routers/rule_sync.py:404](../../backend/routers/rule_sync.py#L404)。
- **T18** auto delta控制：rule源内容变化IP增删/domain-only/忽略项，控制settings/Agent绑定/HA/guard/策略manifest变动，来源缺失/selected引用变化要paused；只能已登记路径小范围delta，不自动迁NAT/主表policy、不改DHCP/guard/PD/return；disabled保留baseline与device。 来源 [backend/routers/rule_sync_write.py:34](../../backend/routers/rule_sync_write.py#L34)。
- **T19** auto sync故障：4MiB/16MiB/100k lines/20k per family/128源边界，URL下载UTF8/HTML/空/未知type、全IP消失、来源domain↔IP；下载故障failed可重试，ownership/控制/写未知或rollback失败paused（**[O6]** 大小/时间超限属paused，见D28更正）。设备已核对但cache落盘失败不能反向恢复错误基准；task-status计一次。 来源 [backend/routers/rule_sync.py:241](../../backend/routers/rule_sync.py#L241)。
- **T20** gateway-forwarding：当前profile多router同Agent合并refs、只有其他profile有link不授权，但Agent绑定另profile且当前有link允许；offline/Docker/低版/foreign/symlink/modified hook；GET列表读取status；旧版persist前先status检查layout、新版可直接POST forwarding空{}；之后GET对账，已应用后丢response不能说未变化。fake不证明重启persist或RouterOS流量。 来源 [backend/agents/linux_gateway.py:652](../../backend/agents/linux_gateway.py#L652)。
- **T21** 持久化故障注入：read-only/full disk/rename/fsync/SQLite busy与进程kill，baseline/lock/manifest/publication/client_fixed_ips存储逐点；不写Go私有状态；从公开进程加载隔离Python兼容seed或权限/磁盘故障触发。不能由HTTP达的guard审计N/A并说明。 来源 [backend/routers/rule_sync_store.py:32](../../backend/routers/rule_sync_store.py#L32)。
- **T22** 生成比较器敏感性：故意改plan一叶、device method/auth/body/readonly字段、script一字符或顺序、DB绑定/manifest ID、回滚条目、hash/失败status，各自必须在同一生产比较器报差异；不把所有error或script mask掉。 来源 [backend/routers/plan.py:498](../../backend/routers/plan.py#L498)。
- **T23** 跨域全TUN/legacy：配置生成→router report/plan→Agent发布→router同步提醒，marker失效/TUN过滤/未真正发布不能变synced；其他domain负责公开入口，这里列联测链与helper guard，不混入本域路由分母。 来源 [backend/common/router_split.py:162](../../backend/common/router_split.py#L162)。
- **T24** 假设备与L5对照：同一合成场景在fake仅记录协议/表状态、在L5真实CHR/iKuai按包流/重启/事件验收；发现fake允许firmware禁止的payload回补fake fixture，而不是读取Go实现改oracle。无Lab证据的实流/脚本/性能结论始终部分覆盖。 来源 [backend/routers/base.py:273](../../backend/routers/base.py#L273)。

## [O6] 横切补充用例（X01–X14）

Step A原稿把handler和设备驱动读得很细，漏掉的主要在handler之外：Flask应用壳层（before/after_request、errorhandler、Werkzeug路由与解析）、锁的获取顺序、进程启动方式决定的后台循环、只在内存里的状态，以及每次写profile带来的跨域后果。X01–X06与config/agents两份清单的同名项同源，这里只写routers特有的触发点和期望；每个X在“适用范围”列出的路由上展开，计入该路由用例数。期望值一律以Python实际结果为准，状态码/消息只是提示。

| 代码 | 输入与断言 | 适用范围 | 来源 |
|---|---|---|---|
| X01 全局JSON脱敏（routers部分） | after_request对每个JSON响应重写：任何字符串含已持久化Agent token或当前/退役rule_proxy_token（原文、`%`/`+`编码）就整串变`[REDACTED]`。routers特有：(a) **路由器密码不在脱敏集合里**，只靠`public_view`不返回密码、`last_release.message`经`event_message(..., (password,))`；fake设备在错误文本里回显密码时，`result.log[].summary`、R017 `probe.message`、R019–R021、R004 notes按Python原样返回（行为差异只记O9/O12）；(b) 规则源URL指向面板自身且带内部token时，rule-sync的`message`/`log`、desired warnings里含URL的整串变`[REDACTED]`；R016的`dns_content`已在对象层先脱敏再dump；(c) 种子token必须是长随机串 | 全部26条 | [profile_context.py:104](../../backend/common/profile_context.py#L104)、[config_export.py:130](../../backend/common/config_export.py#L130)、[routes/routers.py:486](../../backend/routes/routers.py#L486) |
| X02 profile解析先于鉴权、蓝图范围 | before_request顺序：profile解析→登录鉴权→Agent规则同步循环启动。(a) R002–R026：启用登录且不带Authorization，路径/`?profile=`/`?profile_id=`/`X-ConfigFlow-Profile`指向不存在空间→404 `Profile not found: <id>`，非法id→400，换成存在的空间→401；`?profile=`空串→400。(b) R001在全局`agents`蓝图：header与`profile_id`被忽略（指向不存在空间仍200）、响应无`X-ConfigFlow-Profile`；`?profile=`在鉴权之后由handler校验。(c) R002/R004带`?agent=`：header/query仍先被before_request校验（侧栏header已失效+agent有效→404），响应头`X-ConfigFlow-Profile`是请求解析出的空间，不是body里Agent绑定的空间 | R001–R026（F90/F91） | [profile_context.py:94](../../backend/common/profile_context.py#L94)、[routes/__init__.py:97](../../backend/routes/__init__.py#L97)、[routes/auth.py:35](../../backend/routes/auth.py#L35)、[client_devices.py:39](../../backend/routes/client_devices.py#L39) |
| X03 错误形态 | errorhandler：ProfileInUse→409 `{success:false,message,usages:[…]}`（无用途时`usages:[]`）；ProfileValidationError/其他ProfileRepositoryError→400；ProfileNotFound→404英文。ProfileInUse/ProfileValidationError同时是ValueError、ProfileNotFound是KeyError，被handler自己的`except`拦下时形态不同（R015→400、R016→400或404）。未捕获异常→Werkzeug HTML 500（`text/html`）：R008/R010 endpoint `http://[::1`（urlparse ValueError）、R011 body为非空数组/字符串、R011 `execute`内非RouterError异常、R016 YAML异常、R022 receipt规则记录缺`version`。比status、Content-Type，不比HTML正文 | R008–R011、R015、R016、R022 | [routes/__init__.py:102](../../backend/routes/__init__.py#L102)、[config_repository.py:53](../../backend/common/config_repository.py#L53)、[release.py:406](../../backend/routers/release.py#L406) |
| X04 锁的获取顺序与锁文件 | `_manual_sync_lock`（R017、R011、R020）和`_router_settings_lock`（R009、R010、R015 PUT、R024 PUT）在**404/类型/body校验之前**非阻塞取job_lock：锁被占（同一type+endpoint连接上有发布/同步/探测在跑）→409 `此路由器正在发布或同步，请稍后重试`，即使body错误或路由器是iKuai（R024）。路由器不存在时锁键为digest(profile, router_id)，`DATA_DIR/router-rule-sync/<digest>.lock`在404请求上也会被创建。进程内`router_operation_lock`是阻塞且无上限的RLock：R013 GET、R021预览会一直等正在运行的发布，等完再按record变化409。占锁方法：每端各自用慢fake设备跑一个发布，同时发第二个请求。文件树比较按精确glob排除`*.lock`与`.scheduler.lock`，但`<digest>.json` receipt文件名必须一致（切换时Go要读到Python留下的baseline，O14） | R009–R011、R013、R015、R017、R020、R021、R023、R024 | [routes/routers.py:37](../../backend/routes/routers.py#L37)、[routes/routers.py:50](../../backend/routes/routers.py#L50)、[rule_sync_store.py:52](../../backend/routers/rule_sync_store.py#L52)、[route_editor.py:30](../../backend/routers/route_editor.py#L30) |
| X05 宽松解析 | `store.normalize`：`name/type/endpoint/username`用`str()`强转（`123`→`"123"`、`["a"]`→`"['a']"`、`true`→`"True"`都接受）；`verify_tls`只有字面`false`才关（`"false"`、`0`、`null`都算开）；`proxy_gateway_agent_id/dns_agent_id` `str()`强转（`null`→`""`）；`ip_rule_sets`/`ros_policy_rule_sets`为假值（`null`、`""`、`0`、`false`、`{}`）当`[]`；`rule_sync_interval`必须是精确int（`true`、`60.0`、`6e1`拒）；urlparse不校验端口（`http://h:99999`、`http://h:abc`、带userinfo/query/fragment都存下，后续设备调用才失败），`http://[::1`→HTML 500；非字符串password被忽略（新建时→`请填写密码`）。`get_json(silent=True)`：Content-Type不对或坏JSON都当None（R008→`路由器参数必须是对象`；R011→fingerprint空→**先完整读设备再409**；R012/R017/R023忽略）。query：R001 `refresh`认`1`/`true`，R004只认`1`。BOM/NaN/大整数同config X04 | R003、R005、R006、R008、R010、R011、R015、R016、R020、R021、R024、R001、R004 | [store.py:71](../../backend/routers/store.py#L71)、[store.py:75](../../backend/routers/store.py#L75)、[store.py:130](../../backend/routers/store.py#L130)、[routes/routers.py:444](../../backend/routes/routers.py#L444) |
| X06 HEAD/OPTIONS | HEAD对GET路由**执行handler**只不发body：R004会读设备并向LAN主动`/ping`；R018读设备并写路由通知；R001并行读设备、写通知、填缓存；R013持操作锁读设备；R019读设备；R025逐个调用Agent status。R002的HEAD走`_profile_id(by_agent=False)`（`request.method == 'GET'`为假）：忽略`?agent=`，GET对不存在Agent是404而HEAD是200。OPTIONS同config X06（before_request在OPTIONS上也执行） | R001、R002、R004、R007、R013、R014、R018、R019、R022、R025 | [client_devices.py:61](../../backend/routes/client_devices.py#L61)、[routes/routers.py:388](../../backend/routes/routers.py#L388) |
| X07 规则自动同步循环 | 只在`python -m backend.app`下启动；flock `.scheduler.lock`；启动后30秒首轮、之后每30秒一轮。一轮里对每个profile的RouterOS路由器：未开启、无baseline、paused跳过；`checked_at`早于`rule_sync_interval`分钟，或status为running/queued时执行`sync`；`sync`开头把running改paused。定时轮与手动POST共用非阻塞`_worker`：轮次进行中POST→409 `已有规则同步任务正在运行，请稍后重试`。用RECEIPT种子驱动，等≤60秒后轮询GET，比较status/message/设备表/task-status，不比轮次的精确时刻（节奏差异记O12） | R022–R024、T17–T19、T26 | [app.py:185](../../backend/app.py#L185)、[rule_sync.py:374](../../backend/routers/rule_sync.py#L374)、[rule_sync.py:448](../../backend/routers/rule_sync.py#L448) |
| X08 写入后的跨域后果 | (a) `config_revision`变化（“写入后比较代码”所列写入）：`GET /api/profiles[].revision`、该空间Agent的“待更新”、网关安装会话`config_changed`、网关引导的expected revision、IPv6验收报告失效、R016生成期间被并发探测改动→409 `生成预览期间配置已更新，请重新预览。`；(b) auto_backup开启时上述每次写入各安排一次WebDAV上传（fake WebDAV比PUT次数与内容）；(c) 通知/任务：R001/R012/R018写路由检查通知，R011成功/失败`refresh_tasks`，自动同步写task-status `router_rule_sync:<profile>:<router>`，比较`GET /api/notifications`、`GET /api/tasks`；(d) 自动同步成功后写共享规则缓存`rules/<file>`（内容不变时只touch mtime，否则连`.bak`一起换），规则库的缓存时间和生成配置随之变；(e) 删除配置空间（config R117）与导入配置（config R028）经过`_router_commit_guard`：有manifest的路由器→409带router usages，路由器job锁被占→409，这两条要加routers种子；(f) Mihomo Agent改host/删除会改变R001缓存键、`desired.proxy_gateway`与发布fingerprint | R001、R003、R005、R008–R012、R015–R018、R023、R024；config R028/R117 | [config_repository.py:1010](../../backend/common/config_repository.py#L1010)、[config_repository.py:1086](../../backend/common/config_repository.py#L1086)、[webdav_backup.py:158](../../backend/utils/webdav_backup.py#L158)、[notifications.py:177](../../backend/notifications.py#L177)、[rule_utils.py:107](../../backend/utils/rule_utils.py#L107)、[config_repository.py:944](../../backend/common/config_repository.py#L944) |
| X09 路由重叠 | `GET /api/agents/{gateway-installations,i,download}/upstream-routers`进不了R001（分别进网关安装状态、短链、二进制下载）；Go `ServeMux`里`GET /api/agents/gateway-installations/{session_id}`与`GET /api/agents/{agent_id}/upstream-routers`互相重叠又互不更具体，注册即panic，需自定义分派（同agents X04）。`PUT/DELETE /api/routers/gateway-forwarding`落到R010/R009（先取job锁，再404 `路由器不存在`）；`GET/POST /api/routers/<id>`、`GET /api/routers/<id>/apply`→405；尾斜杠`/api/routers/`。比status、`Allow`与副作用 | R001、R009、R010、R025、R026 | [upstream_router_pending.py:11](../../backend/routes/upstream_router_pending.py#L11)、[routes/routers.py:157](../../backend/routes/routers.py#L157) |
| X10 Nginx与长请求 | 前端超时：apply 300秒（=Nginx `proxy_read_timeout 300s`）、plan/report/preview 90秒、probe/routes 60秒。Python（waitress）在客户端断开或Nginx 504后照常跑完：R011、R005、R020、R023的设备/DB/receipt/通知终态必须与不断开时一致；Go不得因请求context取消而半途停（R04 F3只修了驱动层，handler、DB保存与通知阶段也要验）。请求体上限1MiB：R005 token 1 000 001字符的body仍<1MiB→Python 409，≥1MiB→Nginx HTML 413 | R005、R011、R016、R020、R023 | [nginx.conf:121](../../docker/nginx.conf#L121)、[api/index.ts:359](../../frontend/src/api/index.ts#L359) |
| X11 地址分类边界 | 见fixture ADDR：发现（RFC1918回退）、DHCP/路由下一跳、`is_global`规则IP、fe80::/10下一跳、fc00::/7 ULA、默认fake-ip-range。每个分类判断取边界内外各一例，且不只用TEST-NET（同agents X14） | R004–R006、R011、R012、R018、R020、R021、R023 | [ros_clients.py:52](../../backend/routers/ros_clients.py#L52)、[route_editor.py:84](../../backend/routers/route_editor.py#L84)、[rule_sync_rules.py:99](../../backend/routers/rule_sync_rules.py#L99) |
| X12 Python崩溃类与已接受差异 | `execute`只捕获ReleaseError/RouterError/RuntimeError/RequestException：设备回了结构异常但合法的JSON（如ROS `/execute`多报`cf-id`、数组值比较）时Python抛IndexError/KeyError/TypeError→HTML 500，**不回滚、不写last_release、锁释放**；Go按R04 F1/F7 fail closed并回滚。R04“Accepted differences”（紧凑JSON、多字段不一致时报首个按键排序、PUT回动态行的undo、F8回滚重建脚本按32KiB切分）。每例记录Python结果与Go既定行为进O12台账，不算通过也不算失败 | R011、R020、R023 | [release.py:406](../../backend/routers/release.py#L406)、[review-r04.md](review-r04.md) |
| X13 fake按端隔离 | 每端各一套有状态RouterOS/iKuai/Agent/规则URL/WebDAV fake（各自network namespace、相同地址），种子逐字节相同；**禁止两端共用一个fake**，否则两端写入交错、ID分配错位。ROS `.id`、iKuai数字ID、tagname随机后缀按端建立双射 | 全部设备路由 | [routes/routers.py:84](../../backend/routes/routers.py#L84) |
| X14 进程内存状态 | 发现缓存在`current_app.extensions`、上游提醒缓存在模块变量、操作锁在进程内，重启即清空。固定IP token用`JWT_SECRET_KEY`+salt `client-fixed-ip-v1`的itsdangerous签名：未设该环境变量时每次启动随机→重启后旧预览409；两端配同一密钥时，Python签发的token拿到Go是否认可只是切换兼容性检查（600秒窗口，结论记O14），不作差分项 | R001、R004、R005、R006、T27 | [client_fixed_ip.py:55](../../backend/common/client_fixed_ip.py#L55)、[client_devices.py:12](../../backend/routes/client_devices.py#L12) |

### [O6] 补充长序列

- **T25 重启与中断**：在R011发布、R005固定IP、R023同步各自中途对两端分别SIGTERM一次、`kill -9`一次。重启后比较设备表、manifest（Python在保存前被杀就没有manifest，下一次plan必须把本次新建对象当foreign/未知而不是owned）、receipt（running留到下一轮才paused，X07）、`client_fixed_ips`、通知。Python没有重启恢复逻辑，Go若有恢复动作要记O12。 来源 [routes/routers.py:481](../../backend/routes/routers.py#L481)、[rule_sync.py:257](../../backend/routers/rule_sync.py#L257)。
- **T26 定时同步全路径（RECEIPT种子）**：到期/未到期、开关关闭、paused跳过、running→paused、孤儿queued照常执行、failed到下一间隔重试、下载超限→paused、HTTP错误/非UTF-8/校验失败→failed、`rule_fetch_proxy`开启时经fake代理下载、同一轮两个路由器共用同一URL只下载一次。每轮后比task-status、通知、receipt、规则缓存mtime。 来源 [rule_sync.py:374](../../backend/routers/rule_sync.py#L374)、[rule_sync.py:151](../../backend/routers/rule_sync.py#L151)。
- **T27 R001线程里的profile回落**：R001在线程池里调用`_inspect`，`_return_cleanup`的`resolve_profile_id()`没有请求上下文→总是默认空间。种子：默认空间alpha有路由器X（`manage_return_routing:true`，网关m-a），克隆出的beta里同ID路由器X（`false`，同网关），fake m-a报告受管回流active。R001（不带profile）对beta条目把alpha的X误当“自己”排除→生成回流清理、显示`待移除`；R012/R018在beta上下文里则因alpha仍在用而只给warning。两端逐字段比。 来源 [routes/routers.py:372](../../backend/routes/routers.py#L372)、[pending.py:138](../../backend/routers/pending.py#L138)。

## [O6] 逐路由补充用例

编号沿用各路由的S/F/B，从90开始，原有编号不变。F90在R002–R026上都指X02(a)：未登录+不存在/非法profile→404/400先于401，下表不再逐条重复，只列各路由特有项。

| R | [O6] 补充 | Python |
|---|---|---|
| R001 | **F90** 未登录+`?profile=missing`→401（全局蓝图，鉴权先于handler校验）<br>**F91** header `X-ConfigFlow-Profile: missing`→200且响应无该头<br>**B90** `?profile=`空串→400<br>**B91** agent_id取`gateway-installations`/`i`/`download`时进别的handler（X09）<br>**B92** HEAD照样并行读设备、写通知、填缓存（X06）<br>**B93** 探测写last_probe后缓存键（含路由器记录与profile `revision`）失效，下一次`cached:false`<br>**S90** T27克隆空间回流清理差异 | [upstream_router_pending.py:20](../../backend/routes/upstream_router_pending.py#L20)、[pending.py:181](../../backend/routers/pending.py#L181) |
| R002 | **B90** `?agent=`有效但header指向已删空间→404（X02c）<br>**B91** `?agent=`在beta、header为alpha：body `profile_id`=beta，响应头=alpha<br>**B92** HEAD忽略`?agent=`（X06） | [client_devices.py:39](../../backend/routes/client_devices.py#L39) |
| R003 | **S90** 保存后`GET /api/profiles` revision+1、auto_backup上传一次（X08）<br>**F91** 事务ProfileInUse/ProfileValidationError不被handler捕获→errorhandler 409/400（X03） | [client_devices.py:73](../../backend/routes/client_devices.py#L73) |
| R004 | **B90** HEAD触发主动`/ping`（X06）<br>**B91** 无DHCP server时LAN为RFC1918→可发现，为TEST-NET→该源无LAN接口（X11）<br>**B92** `refresh=true`不强制刷新 | [client_devices.py:100](../../backend/routes/client_devices.py#L100)、[ros_clients.py:59](../../backend/routers/ros_clients.py#L59) |
| R005 | **B90** token恰1 000 000字符与1 000 001字符（body<1MiB，后者409 `缺少固定 IP 预览确认，请重新预览。`）；body≥1MiB经Nginx→413（X10）<br>**B91** 发起apply后客户端断开，终态与不断开一致（X10）<br>**B92** 未设`JWT_SECRET_KEY`时重启后旧token→409（X14）<br>**S90** 成功写`client_fixed_ips`→revision+1、备份一次 | [client_fixed_ip.py:164](../../backend/common/client_fixed_ip.py#L164) |
| R006 | **B90** devices里同一IP在不同router_id也拒（`seen`按IP）；name经`normalize_name`但不要求已保存（保存检查在apply） | [client_fixed_ip.py:68](../../backend/common/client_fixed_ip.py#L68) |
| R007 | **B90** 经X05宽松创建的记录（`name:"True"`、`verify_tls`由`"false"`变true、端口非法）原样列出 | [store.py:36](../../backend/routers/store.py#L36) |
| R008 | **B90** X05每个强转变体各一例，比存储值与public_view<br>**F91** endpoint `http://[::1`→HTML 500，无写入<br>**B91** Content-Type `text/plain`的合法JSON→400 `路由器参数必须是对象`<br>**S90** revision+1与备份（X08） | [store.py:54](../../backend/routers/store.py#L54)、[routes/routers.py:108](../../backend/routes/routers.py#L108) |
| R009 | **F91** 同一连接正在发布时DELETE→409，先于manifest检查（X04）<br>**B90** `DELETE /api/routers/gateway-forwarding`→404且留下锁文件（X09） | [routes/routers.py:157](../../backend/routes/routers.py#L157) |
| R010 | **F91** 锁被占+非法body→409而不是400<br>**F92** endpoint `http://[::1`→HTML 500<br>**B90** 只改`verify_tls`/password也清last_probe；`verify_tls:"false"`等于不变 | [routes/routers.py:194](../../backend/routes/routers.py#L194) |
| R011 | **F91** X12：设备回结构异常的JSON使Python在`execute`内抛非RouterError→HTML 500、不回滚、不写last_release<br>**B90** `last_release.message`经`event_message`：URL→`[下载地址已隐藏]`、路由器密码→`[REDACTED]`、去控制字符、超1024字节截断加`\n[输出过长，已截断]`；同一响应的`result.log[].summary`不经过它<br>**B91** 客户端断开/Nginx 504后照常完成（X10）<br>**B92** Content-Type不对→fingerprint为空，仍先完整读设备再409（看fake transcript）<br>**S90** empty发布写last_release→revision+1、备份、`refresh_tasks`<br>**S91** receipt目录只读时`_capture_sync`失败被吞，发布仍200 | [routes/routers.py:444](../../backend/routes/routers.py#L444)、[routes/routers.py:486](../../backend/routes/routers.py#L486)、[operation_events.py:13](../../backend/agents/operation_events.py#L13)、[routes/routers.py:63](../../backend/routes/routers.py#L63) |
| R012 | **B90** `sync_baseline`只在`empty`且无blocker时出现<br>**B91** 预览只写通知，不改revision、不触发备份 | [routes/routers.py:425](../../backend/routes/routers.py#L425) |
| R013 | **B90** HEAD也取操作锁并读设备<br>**B91** 发布进行中发GET：阻塞到发布结束，再按record是否变化200/409 | [routes/routers.py:569](../../backend/routes/routers.py#L569) |
| R014 | **B90** `gateways`列出所有空间的Mihomo Agent，`source_agents`只列本空间OxiDNS | [policy_workspace.py:24](../../backend/routers/policy_workspace.py#L24) |
| R015 | **F91** 锁被占+非法body→409（X04）<br>**F92** 事务ProfileInUse→400（见F18更正） | [routes/routers.py:613](../../backend/routes/routers.py#L613) |
| R016 | **F91** body非法且路由器不存在→400（body校验在查找之前）<br>**F92** ProfileNotFound→404、ProfileValidationError→400、YAML异常→HTML 500（X03）<br>**F93** 生成期间另一请求探测同空间路由器→409 `生成预览期间配置已更新，请重新预览。`（X08a） | [routes/routers.py:658](../../backend/routes/routers.py#L658)、[routes/routers.py:686](../../backend/routes/routers.py#L686) |
| R017 | **S90** 探测失败（502）也写last_probe→revision+1、备份一次（X08）<br>**B90** 前端发`null` body与不带body各一例<br>**B91** 不存在的路由器：先取锁（建锁文件）再404 | [routes/routers.py:208](../../backend/routes/routers.py#L208) |
| R018 | **B90** HEAD写路由检查通知（X06） | [routes/routers.py:402](../../backend/routes/routers.py#L402) |
| R019 | **B90** HEAD读设备（X06） | [routes/routers.py:540](../../backend/routes/routers.py#L540) |
| R020 | **F91** 锁被占时先于body校验返回409（X04）<br>**B90** 客户端断开后照常完成（X10） | [routes/routers.py:711](../../backend/routes/routers.py#L711) |
| R021 | **B90** 发布进行中预览：阻塞等待操作锁（X04） | [routes/routers.py:726](../../backend/routes/routers.py#L726) |
| R022 | **B90** receipt是JSON数组→当`{}`读，显示waiting而不是paused<br>**F91** receipt的rules记录缺`version`→HTML 500（X03） | [rule_sync_store.py:32](../../backend/routers/rule_sync_store.py#L32)、[rule_sync_store.py:88](../../backend/routers/rule_sync_store.py#L88) |
| R023 | **F91** 重启遗留running：下一轮之前POST→409带陈旧消息，之后paused（X07）<br>**F92** 定时轮进行中POST→409 `已有规则同步任务正在运行，请稍后重试`<br>**B90** 下载超限→paused而非failed（D28更正） | [routes/routers.py:809](../../backend/routes/routers.py#L809)、[rule_sync.py:405](../../backend/routers/rule_sync.py#L405) |
| R024 | **F91** iKuai路由器+锁被占→409而不是400（X04）<br>**S90** 开关/间隔写入→revision+1、备份 | [routes/routers.py:784](../../backend/routes/routers.py#L784) |
| R025 | **B90** HEAD逐个调用Agent status（X06） | [routes/routers.py:139](../../backend/routes/routers.py#L139) |
| R026 | **B90** `PUT/DELETE /api/routers/gateway-forwarding`落到R010/R009（X09） | [routes/routers.py:157](../../backend/routes/routers.py#L157) |

## [O6] 设备协议与fake合约补充

D表已覆盖每条写路径；这里补的是fake必须照真设备实现、否则相应Python分支永远走不到的行为（来源均为驱动源码里记录的4.0.314/7.24.5实测）。

- **iKuai fake**：所有回复带`Result:10000`，成败只看`code`；重复tagname的add回`code:3001`（`_add`靠它证明归属）；`route_object`丢tagname、空地址字段回`{}`；`static_rt`读回`netmask`带显示后缀、有运行字段`status`；请求体超过约96KiB回空200且**不落表**；会话可在批次中途失效回1008（被拒的写不落表）；group `ref_count`含隐藏引用；`dhcp_static`用`static_total/static_data`前缀分页。 来源 [ikuai.py:247](../../backend/routers/ikuai.py#L247)、[ikuai.py:661](../../backend/routers/ikuai.py#L661)、[ikuai.py:31](../../backend/routers/ikuai.py#L31)。
- **RouterOS fake**：`.id`为递增`*hex`、默认不复用（复用只作注入）；NAT新增后短暂`invalid=true`；`/execute`带`as-string`回`{"ret": "..."}`；缺权限回500 `{"detail":"not enough permissions (9)"}`；ARP ping缺interface回400；`print`的`.query`按OR语义；`remove`逗号ID可按设备自己的顺序处理（R04 F6）；动态/只读行拒PATCH/DELETE。fake的`/execute`必须真正解析本清单用到的脚本行并改表，预录输出只能用于注入异常。 来源 [routeros.py:133](../../backend/routers/routeros.py#L133)、[routeros_batch.py:139](../../backend/routers/routeros_batch.py#L139)。
- **规则下载fake（D28补充）**：HTTP源可回慢body（逼近30秒总时限）、>4MiB、错误码、非UTF-8、BOM（`utf-8-sig`接受）；`rule_fetch_proxy`开启时fake代理必须收到请求，URL里的凭据不得出现在任何消息里。 来源 [rule_sync.py:132](../../backend/routers/rule_sync.py#L132)。

## Python逐错误、只读与诊断分支 H

每个H定位一个源码错误/降级/阻止位置。用对应R合法fixture起步，单独让guard成立（循环字段与复合条件逐项变体），比较外部status/中文message、report/plan unknown或readonly reason，以及device/DB/补偿结局。K键指文件，`Kxx:行号 函数`指精确分支；不把return None/False统一当HTTP400。

这是源码分支配方与审计索引，**不是已可执行或已通过的测试数**：纯内部参数、抽象driver、已不可达manual分支、只由config/agents公开入口触发的router_split，以及只能OS/文件故障触发的guard，都保留界限。Step B须由Opus审每条从公开入口与隔离seed是否可达；不能调用Go私有helper填覆盖，不能构造“双方都500”冒充合法成功。模板字符串中的RouterOS脚本运行期guard由G/D/L5验证，不凭Python AST索引声称穷尽设备解释器。

H中动态error变量、message/key/MESSAGES以相同行源码和Python oracle解析，不能mask。截长表达式保留原行；英文内部exception措辞依比较规则处理，中文提示必须保留。测试辅助`routeros_test_helpers.py`不作为生产HTTP错误入口。


### K01 [backend/agents/linux_gateway.py](../../backend/agents/linux_gateway.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0001 / R025/R026（仅forwarding可达helper） | K01:57 `_at_least` / 校验/诊断降级 | `not isinstance(version, str) or not re.fullmatch('[0-9]+\\.[0-9]+\\.[0-9]+(?:-go)?', version)` → `False` |
| H0002 / R025/R026（仅forwarding可达helper） | K01:60 `_at_least` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError, AttributeError)` → `except (ValueError, TypeError, AttributeError):     return False` |
| H0003 / R025/R026（仅forwarding可达helper） | K01:61 `_at_least` / 校验/诊断降级 | `捕获 (ValueError, TypeError, AttributeError)` → `False` |
| H0004 / R025/R026（仅forwarding可达helper） | K01:338 `_require_forwarding_layout` / 抛出 | `not supports_standalone_forwarding(agent) and _forwarding_upgrade_needed(read_status(), agent)` → `_error('UPGRADE_REQUIRED', 409, STANDALONE_UPGRADE)` |
| H0005 / R025/R026（仅forwarding可达helper） | K01:395 `_request` / 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_RESPONSE_BYTES` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据过大。')` |
| H0006 / R025/R026（仅forwarding可达helper） | K01:399 `_request` / 捕获后的失败/降级/补偿 | `捕获 MonitorError` → `except MonitorError:     raise` |
| H0007 / R025/R026（仅forwarding可达helper） | K01:400 `_request` / 抛出 | `捕获 MonitorError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0008 / R025/R026（仅forwarding可达helper） | K01:401 `_request` / 捕获后的失败/降级/补偿 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `except (requests.RequestException, KeyError, TypeError, ValueError):     if operation in ('apply', 'remove'):         message = '未能确认网关操作结果，请先刷新设备网络状态，勿重复应用。'     elif operation == 'forwarding':         message = '未能确认转发持久化结果，请先刷新设备状态，勿重复操作。'     else:         message = '暂时无法连接 Agent，请检查设备连接。'     raise _error('AGENT_UNREACHABLE', 502, message) from None` |
| H0009 / R025/R026（仅forwarding可达helper） | K01:408 `_request` / 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `_error('AGENT_UNREACHABLE', 502, message)` |
| H0010 / R025/R026（仅forwarding可达helper） | K01:411 `_request` / 抛出 | `status == 404` → `_error('UPGRADE_REQUIRED', 409, f'设备尚未提供网关接口，请更新到包含本功能的 {minimum} 或更新版本。')` |
| H0011 / R025/R026（仅forwarding可达helper） | K01:413 `_request` / 抛出 | `status in (401, 403)` → `_error('AGENT_AUTH_FAILED', 502, 'Agent 认证失败，请检查设备注册状态。')` |
| H0012 / R025/R026（仅forwarding可达helper） | K01:416 `_request` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, UnicodeError)` → `except (ValueError, UnicodeError):     raise _error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据无法解析，请刷新设备状态。') from None` |
| H0013 / R025/R026（仅forwarding可达helper） | K01:417 `_request` / 抛出 | `捕获 (ValueError, UnicodeError)` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据无法解析，请刷新设备状态。')` |
| H0014 / R025/R026（仅forwarding可达helper） | K01:419 `_request` / 抛出 | `not isinstance(data, dict)` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据格式异常。')` |
| H0015 / R025/R026（仅forwarding可达helper） | K01:424 `_request` / 抛出 | `status >= 400` → `_error('GATEWAY_ERROR', status if status in (400, 409, 422) else 502, message[:1000])` |
| H0016 / R025/R026（仅forwarding可达helper） | K01:536 `sanitize_forward_filter` / 校验/诊断降级 | `not isinstance(value, dict) or value.get('state') not in FORWARD_FILTER_STATES` → `None` |
| H0017 / R025/R026（仅forwarding可达helper） | K01:643 `gateway_forwarding_entry` / 捕获后的失败/降级/补偿 | `捕获 MonitorError` → `except MonitorError as error:     if error.code == 'UPGRADE_REQUIRED':         return _gateway_entry(agent_id, agent, routers, 'upgrade_required', GATEWAY_FORWARDING_MESSAGES['upgrade_required'])     return _gateway_entry(agent_id, agent, routers, 'error', error.message)` |
| H0018 / R025/R026（仅forwarding可达helper） | K01:655 `persist_gateway_forwarding` / 抛出 | `not agent` → `_error('AGENT_NOT_FOUND', 404, GATEWAY_FORWARDING_MESSAGES['missing'])` |
| H0019 / R025/R026（仅forwarding可达helper） | K01:657 `persist_gateway_forwarding` / 抛出 | `agent.get('service_type') != 'mihomo'` → `_error('WRONG_SERVICE', 400, GATEWAY_FORWARDING_MESSAGES['not_mihomo'])` |
| H0020 / R025/R026（仅forwarding可达helper） | K01:659 `persist_gateway_forwarding` / 抛出 | `agent.get('deployment_method') == 'docker'` → `_error('UNSUPPORTED_ENVIRONMENT', 409, GATEWAY_FORWARDING_MESSAGES['docker'])` |
| H0021 / R025/R026（仅forwarding可达helper） | K01:661 `persist_gateway_forwarding` / 抛出 | `not is_online(agent)` → `_error('AGENT_OFFLINE', 409, 'Agent 离线，无法开启转发。')` |
| H0022 / R025/R026（仅forwarding可达helper） | K01:663 `persist_gateway_forwarding` / 抛出 | `not supports_forwarding(agent)` → `_error('UPGRADE_REQUIRED', 409, GATEWAY_FORWARDING_MESSAGES['upgrade_required'])` |

### K02 [backend/common/client_devices.py](../../backend/common/client_devices.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0023 / R002/R003/R014–R016（设备草稿） | K02:36 `normalize_change` / 抛出 | `mode not in MODES or previous_mode not in MODES` → `OxidnsSettingsError('DNS 策略只能是默认、智能分流或直连。')` |
| H0024 / R002/R003/R014–R016（设备草稿） | K02:45 `update_device` / 抛出 | `(current['name'], current['mode']) != (previous_name, previous_mode)` → `DeviceChanged('此设备已在其他页面修改，请重新载入后再保存。')` |

### K03 [backend/common/client_discovery.py](../../backend/common/client_discovery.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0025 / R004/R005/R006 | K03:58 `_read_all` / 捕获后的失败/降级/补偿 | `for (router, future) in zip(routers, futures) / 捕获 RouterError` → `except RouterError as error:     results.append((router, None, str(error)))` |
| H0026 / R004/R005/R006 | K03:60 `_read_all` / 捕获后的失败/降级/补偿 | `for (router, future) in zip(routers, futures) / 捕获 Exception` → `except Exception as error:     logger.exception('Client discovery failed for router %s', router.get('id'))     results.append((router, None, f'读取失败：{error.__class__.__name__}'))` |
| H0027 / R004/R005/R006 | K03:79 `_endpoint_ip` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return ''` |
| H0028 / R004/R005/R006 | K03:86 `_network` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return None` |
| H0029 / R004/R005/R006 | K03:87 `_network` / 校验/诊断降级 | `捕获 ValueError` → `None` |

### K04 [backend/common/client_discovery_cache.py](../../backend/common/client_discovery_cache.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0030 / R004（并发/refresh） | K04:76 `DiscoveryCache.read` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception as failure:     with self._lock:         entry.pending = None         pending.set_exception(failure)     raise` |
| H0031 / R004（并发/refresh） | K04:80 `DiscoveryCache.read` / 抛出 | `捕获 Exception` → `重新抛出当前错误；同时核对已写入/补偿结果` |

### K05 [backend/common/client_fixed_ip.py](../../backend/common/client_fixed_ip.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0032 / R005/R006（固定IP） | K05:27 `validate_bindings` / 抛出 | `not isinstance(value, dict) or len(value) > 2048` → `OxidnsSettingsError('客户端固定 IP 记录必须为对象，最多 2048 条。')` |
| H0033 / R005/R006（固定IP） | K05:38 `validate_bindings` / 抛出 | `for (ip, binding) in value.items() / not isinstance(binding, dict) or set(binding) - set(BINDING_FIELDS) or (not required <= set(binding)) or (not ipv4(ip)) or (binding.get('ip') != ip) or (binding.get('vendor') not in ('routeros', 'ikuai')) or (not all((isinstance(v, str) and len(v) <= 512 and (not any((ord(c) < 32 for c in v))) for v in binding.values()))) or (not binding['router_id']) or (not binding['id']) or (not normalize_mac(binding['mac'])) or (binding['mac'] != normalize_mac(binding['mac'])) or (not binding['comment'].startswith('cf-client:'))` → `OxidnsSettingsError('客户端固定 IP 记录缺少有效 IP、MAC、路由器或归属标记。')` |
| H0034 / R005/R006（固定IP） | K05:48 `adapter` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError('此来源路由器不支持固定 IP。')` |
| H0035 / R005/R006（固定IP） | K05:65 `_guard_setup` / 抛出 | `any((not item.get('completed') and (not item.get('abandoned')) for item in profile_sessions(get_repository(), profile_id)))` → `RouterConflictError('配置向导进行中，只允许命名；请完成向导后再固定或删除固定 IP。')` |
| H0036 / R005/R006（固定IP） | K05:72 `normalize_request` / 抛出 | `not isinstance(body, dict) or set(body) != {'profile_id', 'operation', 'devices'} or body.get('operation') not in ('add', 'delete') or (not isinstance(body.get('devices'), list)) or (not 1 <= len(body['devices']) <= 64)` → `OxidnsSettingsError('请选择 1 至 64 台设备预览固定 IP 操作。')` |
| H0037 / R005/R006（固定IP） | K05:76 `normalize_request` / 抛出 | `for row in body['devices'] / not isinstance(row, dict) or set(row) != {'ip', 'mac', 'router_id', 'name'}` → `OxidnsSettingsError('固定 IP 请求需要 IP、MAC、来源路由器和设备名称。')` |
| H0038 / R005/R006（固定IP） | K05:80 `normalize_request` / 抛出 | `for row in body['devices'] / not ip or not mac or (not isinstance(router_id, str)) or (not router_id) or (ip in seen)` → `OxidnsSettingsError('固定 IP 只支持有 MAC 和明确来源路由器的单个 IPv4，且不能重复选择。')` |
| H0039 / R005/R006（固定IP） | K05:82 `normalize_request` / 抛出 | `for row in body['devices'] / (router_id, mac) in seen_macs` → `OxidnsSettingsError('同一路由器上的同一 MAC 只能选择一个固定 IP。')` |
| H0040 / R005/R006（固定IP） | K05:102 `_router` / 抛出 | `not router or router.get('type') not in ('routeros', 'ikuai')` → `RouterConflictError('发现这台设备的路由器已不存在或不支持写入，请刷新设备列表。')` |
| H0041 / R005/R006（固定IP） | K05:112 `_plan` / 抛出 | `for item in intent['devices'] / not getattr(driver, 'supports_write', False)` → `RouterError('此来源路由器不支持写入 DHCP 静态租约。')` |
| H0042 / R005/R006（固定IP） | K05:116 `_plan` / 抛出 | `intent['operation'] == 'add' / not _seen(driver, router['type'], item['ip'], item['mac'])` → `RouterConflictError(f"{item['ip']} 的 MAC 或来源路由器已变化，请重新发现设备。")` |
| H0043 / R005/R006（固定IP） | K05:126 `_plan` / 抛出 | `not (intent['operation'] == 'add') / not binding or binding.get('router_id') != item['router_id'] or binding.get('mac') != item['mac']` → `RouterConflictError('没有这台设备由 ConfigFlow 创建的固定 IP 记录，不会删除其他静态租约。')` |
| H0044 / R005/R006（固定IP） | K05:165 `apply` / 抛出 | `not isinstance(token, str) or not token or len(token) > 1000000` → `RouterConflictError('缺少固定 IP 预览确认，请重新预览。')` |
| H0045 / R005/R006（固定IP） | K05:168 `apply` / 捕获后的失败/降级/补偿 | `捕获 BadSignature` → `except BadSignature:     raise RouterConflictError('固定 IP 预览无效或已过期，请重新预览确认。') from None` |
| H0046 / R005/R006（固定IP） | K05:169 `apply` / 抛出 | `捕获 BadSignature` → `RouterConflictError('固定 IP 预览无效或已过期，请重新预览确认。')` |
| H0047 / R005/R006（固定IP） | K05:171 `apply` / 抛出 | `approved.get('profile_id') != profile_id` → `RouterConflictError('配置空间已切换，请重新预览。')` |
| H0048 / R005/R006（固定IP） | K05:184 `apply` / 抛出 | `for router_id in sorted({item['router_id'] for item in intent['devices']}) / not stack.enter_context(job_lock(profile_id, router_id, router))` → `RouterConflictError('此路由器正在发布或同步，请稍后重新预览。')` |
| H0049 / R005/R006（固定IP） | K05:191 `apply` / 抛出 | `for item in intent['devices'] / _router(initial, item['router_id']) != _router(config, item['router_id'])` → `RouterConflictError('路由器连接在等待期间发生变化，请重新预览。')` |
| H0050 / R005/R006（固定IP） | K05:195 `apply` / 抛出 | `operations != approved['operations']` → `RouterConflictError('路由器或 DHCP 绑定在预览后发生变化，请重新预览确认。')` |
| H0051 / R005/R006（固定IP） | K05:199 `apply` / 抛出 | `for item in intent['devices'] / item['ip'] not in before_rows` → `RouterConflictError('请先保存设备名称或 DNS 策略，再固定 IP。')` |
| H0052 / R005/R006（固定IP） | K05:219 `apply.save` / 抛出 | `for item in intent['devices'] / rows.get(ip) != before_rows.get(ip) or bindings(current).get(ip) != before_bindings.get(ip) or _router(current, item['router_id']) != _router(config, item['router_id'])` → `RouterConflictError('设备或路由器设置已被其他页面修改，已撤销固定 IP 操作，请刷新。')` |
| H0053 / R005/R006（固定IP） | K05:228 `apply` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception as error:     (failures, restored) = ([], {})     for (item, module, driver, receipt) in reversed(completed):         try:             if intent['operation'] == 'add':                 module.rollback(driver, receipt)             else:                 binding = module.restore_deleted(driver, receipt)                 if binding:                     restored[item['ip']] = _public_binding(binding, item['router_id'])         except Exception as undo_error:             failures.append(f"{item['ip']}：{undo_error}")     if restored:          def save_restored(current):             updated = deepcopy(bindings(current))             present = {row['ip'] for row in list_devices(current)['devices']}     …` |
| H0054 / R005/R006（固定IP） | K05:238 `apply` / 捕获后的失败/降级/补偿 | `for (item, module, driver, receipt) in reversed(completed) / 捕获 Exception` → `except Exception as undo_error:     failures.append(f"{item['ip']}：{undo_error}")` |
| H0055 / R005/R006（固定IP） | K05:239 `apply` / 阻止/只读原因 | `for (item, module, driver, receipt) in reversed(completed) / 捕获 Exception` → `failures.append(f"{item['ip']}：{undo_error}")` |
| H0056 / R005/R006（固定IP） | K05:251 `apply` / 捕获后的失败/降级/补偿 | `restored / 捕获 Exception` → `except Exception as restore_error:     failures.append(f'恢复绑定标识保存失败：{restore_error}')` |
| H0057 / R005/R006（固定IP） | K05:252 `apply` / 阻止/只读原因 | `restored / 捕获 Exception` → `failures.append(f'恢复绑定标识保存失败：{restore_error}')` |
| H0058 / R005/R006（固定IP） | K05:254 `apply` / 抛出 | `捕获 Exception / failures` → `RouterRollbackError(f'{error}；回滚未完成，请检查路由器：' + '；'.join(failures))` |
| H0059 / R005/R006（固定IP） | K05:260 `apply` / 抛出 | `捕获 Exception / isinstance(error, (RouterError, OxidnsSettingsError))` → `type(error)(str(error) + note)` |
| H0060 / R005/R006（固定IP） | K05:261 `apply` / 抛出 | `捕获 Exception` → `RouterError('固定 IP 记录保存失败，已撤销本次路由器变更，请重试。' + note)` |
| H0061 / R005/R006（固定IP） | K05:268 `enrich` / 抛出 | `not getattr(driver, 'supports_write', False)` → `RouterError('此来源路由器不支持写入。')` |
| H0062 / R005/R006（固定IP） | K05:278 `enrich` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     for sighting in reading.sightings.values():         sighting.fixed_ip = {'eligible': False, 'reason': str(error), 'status': 'none', 'owned': False, 'router_id': router_id}` |

### K06 [backend/common/config_repository.py](../../backend/common/config_repository.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0063 / R009/R010/R011/R015/R024（并发/manifest保存） | K06:969 `ProfileRepository._router_commit_guard` / 抛出 | `for (field, label) in (('ros_policy_state', '策略路由'), ('dns_guard_state', '强制 DNS 规则'), ('ipv6_fakeip_state', 'IPv6 Fake-IP 路由'), ('ipv6_routes_state', 'IPv6 规则集路由'), ('ipv6_pd_guard_state', 'IPv6 前缀变化保护')) / old.get(field) and (identity_changed or (field_changed and key != policy_router))` → `ProfileInUse(f'路由器仍有已发布的{label}；请先发布并核对撤回，再修改类型、地址或删除发布记录', [{'kind': 'router', 'id': router_id, 'name': old.get('name', router_id), 'profile_id': profile_id, 'profile_name': previous['profiles'][profile_id]['name']}])` |
| H0064 / R009/R010/R011/R015/R024（并发/manifest保存） | K06:987 `ProfileRepository._router_commit_guard` / 抛出 | `for ((profile_id, router_id), router) in changed.items() / not jobs.enter_context(job_lock(profile_id, router_id, router))` → `ProfileInUse('路由器正在发布或同步，不能替换连接或删除记录，请稍后重试')` |
| H0065 / R009/R010/R011/R015/R024（并发/manifest保存） | K06:1329 `ProfileRepository.save_router_settings` / 抛出 | `current != expected_router` → `ProfileInUse('路由器设置已变化，请刷新后重试')` |
| H0066 / R009/R010/R011/R015/R024（并发/manifest保存） | K06:1331 `ProfileRepository.save_router_settings` / 抛出 | `replacement is not None and replacement.get('id') != router_id` → `ProfileValidationError('不能修改路由器 ID')` |
| H0067 / R009/R010/R011/R015/R024（并发/manifest保存） | K06:1357 `ProfileRepository.save_router_publication` / 抛出 | `current is None or any((current.get(field) != expected_router.get(field) for field in ROUTER_CONNECTION_FIELDS))` → `ProfileInUse('路由器连接已变化，无法保存发布回读记录')` |

### K07 [backend/common/device_notes.py](../../backend/common/device_notes.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0068 / R002/R003/R014–R016（设备草稿） | K07:20 `normalize_name` / 抛出 | `not isinstance(value, str) or len(value) > 80 or any((ord(c) < 32 or ord(c) == 127 for c in value))` → `OxidnsSettingsError('设备备注最多 80 个字符，不能包含换行或控制字符。')` |
| H0069 / R002/R003/R014–R016（设备草稿） | K07:26 `normalize_notes` / 抛出 | `not isinstance(value, dict) or len(value) > MAX_NOTES` → `OxidnsSettingsError(f'设备备注必须为对象，最多 {MAX_NOTES} 条。')` |
| H0070 / R002/R003/R014–R016（设备草稿） | K07:31 `normalize_notes` / 抛出 | `for (address, name) in value.items() / ip in result` → `OxidnsSettingsError(f'设备备注地址重复：{ip}')` |

### K08 [backend/common/oxidns_device_policy.py](../../backend/common/oxidns_device_policy.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0071 / R002/R003/R014–R016（设备草稿） | K08:25 `normalize_client_range` / 抛出 | `not isinstance(raw, str)` → `ValueError()` |
| H0072 / R002/R003/R014–R016（设备草稿） | K08:28 `normalize_client_range` / 抛出 | `len(parts) > 2 or not re.fullmatch('[0-9a-fA-F:.]+', parts[0])` → `ValueError()` |
| H0073 / R002/R003/R014–R016（设备草稿） | K08:33 `normalize_client_range` / 抛出 | `len(parts) == 2 / not re.fullmatch('[0-9]{1,3}', parts[1])` → `ValueError()` |
| H0074 / R002/R003/R014–R016（设备草稿） | K08:36 `normalize_client_range` / 抛出 | `not 0 <= prefix <= address.max_prefixlen` → `ValueError()` |
| H0075 / R002/R003/R014–R016（设备草稿） | K08:40 `normalize_client_range` / 抛出 | `isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped / prefix < 96` → `ValueError()` |
| H0076 / R002/R003/R014–R016（设备草稿） | K08:44 `normalize_client_range` / 抛出 | `network.is_multicast` → `ValueError()` |
| H0077 / R002/R003/R014–R016（设备草稿） | K08:47 `normalize_client_range` / 抛出 | `prefix == address.max_prefixlen / address.is_unspecified` → `ValueError()` |
| H0078 / R002/R003/R014–R016（设备草稿） | K08:50 `normalize_client_range` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     raise OxidnsSettingsError('请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。') from None` |
| H0079 / R002/R003/R014–R016（设备草稿） | K08:51 `normalize_client_range` / 抛出 | `捕获 ValueError` → `OxidnsSettingsError('请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。')` |
| H0080 / R002/R003/R014–R016（设备草稿） | K08:56 `normalize_policies` / 抛出 | `not isinstance(value, list) or len(value) > 512` → `OxidnsSettingsError('设备策略必须为列表，最多 512 条策略。')` |
| H0081 / R002/R003/R014–R016（设备草稿） | K08:60 `normalize_policies` / 抛出 | `for row in value / not isinstance(row, dict) or set(row) - {'ip', 'name', 'mode'}` → `OxidnsSettingsError('设备策略仅支持 IP / 网段、备注和策略。')` |
| H0082 / R002/R003/R014–R016（设备草稿） | K08:63 `normalize_policies` / 抛出 | `for row in value / ip in seen` → `OxidnsSettingsError(f'客户端 IP / 网段重复：{ip}')` |
| H0083 / R002/R003/R014–R016（设备草稿） | K08:67 `normalize_policies` / 抛出 | `for row in value / not isinstance(name, str) or len(name) > 80 or any((ord(c) < 32 for c in name))` → `OxidnsSettingsError('设备备注最多 80 个字符，不能包含换行或控制字符。')` |
| H0084 / R002/R003/R014–R016（设备草稿） | K08:69 `normalize_policies` / 抛出 | `for row in value / mode not in ('smart', 'direct')` → `OxidnsSettingsError('设备策略必须为智能分流或直连。')` |
| H0085 / R002/R003/R014–R016（设备草稿） | K08:83 `_policy_plugin` / 校验/诊断降级 | `not direct` → `None` |
| H0086 / R002/R003/R014–R016（设备草稿） | K08:103 `_refuse` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `OxidnsSettingsError(f'{reason}，无法安全插入设备策略；{fix}。')` |
| H0087 / R002/R003/R014–R016（设备草稿） | K08:148 `_listen_port` / 校验/诊断降级 | `not isinstance(listen, str) or '${' in listen` → `None` |
| H0088 / R002/R003/R014–R016（设备草稿） | K08:157 `_server_entries` / 校验/诊断降级 | `not isinstance(args, dict)` → `None` |
| H0089 / R002/R003/R014–R016（设备草稿） | K08:161 `_server_entries` / 校验/诊断降级 | `plugin.get('type') == 'http_server' / not isinstance(items, list) or not items or any((not isinstance(item, dict) for item in items))` → `None` |
| H0090 / R002/R003/R014–R016（设备草稿） | K08:220 `entry_context` / 捕获后的失败/降级/补偿 | `for value in values / 捕获 ValueError` → `except ValueError:     continue` |
| H0091 / R002/R003/R014–R016（设备草稿） | K08:240 `_legacy_node_test` / 校验/诊断降级 | `not any((port == want and (host == ip or (host in ('', '0.0.0.0', '::') and ip in context['hosts'])) for (ip, want) in context.get('addresses', ())))` → `False` |
| H0092 / R002/R003/R014–R016（设备草稿） | K08:243 `_legacy_node_test` / 校验/诊断降级 | `len(entries) != 1 or any((p.get('type') not in ('udp_server', 'tcp_server') for (p, _) in servers))` → `False` |
| H0093 / R002/R003/R014–R016（设备草稿） | K08:246 `_legacy_node_test` / 校验/诊断降级 | `plugins[entry].get('type') != 'sequence'` → `False` |
| H0094 / R002/R003/R014–R016（设备草稿） | K08:248 `_legacy_node_test` / 校验/诊断降级 | `any((plugins[tag].get('type') in ('sequence', 'fallback') for tag in reach - {entry}))` → `False` |
| H0095 / R002/R003/R014–R016（设备草稿） | K08:258 `_legacy_node_test` / 校验/诊断降级 | `owners(entry) - names` → `False` |
| H0096 / R002/R003/R014–R016（设备草稿） | K08:329 `_patch` / 抛出 | `old / owned != {POLICY_TAG: old, DIRECT_TAG: DIRECT} or len(hooks) != 1` → `OxidnsSettingsError('设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。')` |
| H0097 / R002/R003/R014–R016（设备草稿） | K08:333 `_patch` / 抛出 | `old / index != _prefix_end(args)` → `OxidnsSettingsError('设备策略的位置已被修改，请先恢复到分流规则之前。')` |
| H0098 / R002/R003/R014–R016（设备草稿） | K08:335 `_patch` / 抛出 | `not (old) / owned or hooks` → `OxidnsSettingsError('完整参数中存在同名设备策略插件，请先处理命名冲突。')` |
| H0099 / R002/R003/R014–R016（设备草稿） | K08:340 `_patch` / 抛出 | `POLICY_TAG in text or DIRECT_TAG in text` → `OxidnsSettingsError('设备策略插件存在额外引用，请先在完整参数中处理。')` |
| H0100 / R002/R003/R014–R016（设备草稿） | K08:370 `read_policies` / 捕获后的失败/降级/补偿 | `捕获 OxidnsSettingsError` → `except OxidnsSettingsError as exc:     return {'policies': rows, 'editable': False, 'reason': str(exc), 'side_entries': []}` |
| H0101 / R002/R003/R014–R016（设备草稿） | K08:371 `read_policies` / 错误/未知结果 | `捕获 OxidnsSettingsError` → `{'policies': rows, 'editable': False, 'reason': str(exc), 'side_entries': []}` |

### K09 [backend/common/router_split.py](../../backend/common/router_split.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0102 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:102 `full_tun_document` / 抛出 | `not isinstance(tun, dict)` → `LinuxGatewayError('Mihomo tun 必须是对象')` |
| H0103 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:104 `full_tun_document` / 抛出 | `for key in tun_filters(tun)` → `LinuxGatewayError(f'主路由分流的完整 TUN 与现有 tun.{key} 过滤冲突：未进入 TUN 的连接会在主路由与 Linux 之间回环。请先在 Mihomo 基础 YAML 中移除该过滤')` |
| H0104 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:119 `_load_base` / 抛出 | `not isinstance(text, str)` → `LinuxGatewayError('Mihomo 基础配置必须为 YAML 文本')` |
| H0105 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:125 `_load_base` / 捕获后的失败/降级/补偿 | `捕获 (yaml.YAMLError, RecursionError)` → `except (yaml.YAMLError, RecursionError) as exc:     raise LinuxGatewayError('Mihomo 基础 YAML 无法解析，请先修正基础配置') from exc` |
| H0106 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:126 `_load_base` / 抛出 | `捕获 (yaml.YAMLError, RecursionError)` → `LinuxGatewayError('Mihomo 基础 YAML 无法解析，请先修正基础配置')` |
| H0107 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:128 `_load_base` / 抛出 | `not isinstance(document, dict)` → `LinuxGatewayError('Mihomo 基础配置必须是 YAML 对象')` |
| H0108 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:143 `_full_tun_shape` / 校验/诊断降级 | `not isinstance(doc, dict) or GATEWAY_MARKER in doc` → `False` |
| H0109 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:146 `_full_tun_shape` / 校验/诊断降级 | `not isinstance(tun, dict)` → `False` |
| H0110 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:166 `check_generation` / 抛出 | `state == 'invalid'` → `RouterSplitError(MESSAGES['ROUTER_SPLIT_INVALID'], 'ROUTER_SPLIT_INVALID')` |
| H0111 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:168 `check_generation` / 抛出 | `state == 'valid' and _gateway_enabled(config)` → `RouterSplitError(MESSAGES['ROUTER_SPLIT_FULL_TUN'], 'ROUTER_SPLIT_FULL_TUN')` |
| H0112 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:173 `require_full_tun_generated` / 抛出 | `not _full_tun_shape(main)` → `ValueError(GENERATED_TUN_MISSING)` |
| H0113 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:176 `require_full_tun_generated` / 抛出 | `filters` → `ValueError(GENERATED_TUN_FILTERED.format(keys='、tun.'.join(filters)))` |
| H0114 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:210 `_base_document` / 捕获后的失败/降级/补偿 | `捕获 (yaml.YAMLError, RecursionError)` → `except (yaml.YAMLError, RecursionError):     return None` |
| H0115 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:211 `_base_document` / 校验/诊断降级 | `捕获 (yaml.YAMLError, RecursionError)` → `None` |
| H0116 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:224 `legacy_source` / 校验/诊断降级 | `read_marker(config)[0] != 'absent'` → `None` |
| H0117 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:228 `legacy_source` / 校验/诊断降级 | `context.get('has_gateway_session')` → `None` |
| H0118 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K09:235 `publish_legacy_source` / 校验/诊断降级 | `read_marker(config)[0] != 'absent' or not profile_id` → `None` |

### K10 [backend/common/rule_ip_presence.py](../../backend/common/rule_ip_presence.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0119 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:53 `classical_ip_lines` / 校验/诊断降级 | `source_type == 'content' / not isinstance(text, str)` → `None` |
| H0120 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:59 `classical_ip_lines` / 校验/诊断降级 | `source_type != 'url' or not item.get('url')` → `None` |
| H0121 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:64 `classical_ip_lines` / 校验/诊断降级 | `ruleset_format(item, config) == 'mrs'` → `None` |
| H0122 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:67 `classical_ip_lines` / 捕获后的失败/降级/补偿 | `捕获 (OSError, TypeError, ValueError)` → `except (OSError, TypeError, ValueError):     return None` |
| H0123 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:68 `classical_ip_lines` / 校验/诊断降级 | `捕获 (OSError, TypeError, ValueError)` → `None` |
| H0124 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:74 `classical_ip_lines` / 校验/诊断降级 | `data is None` → `None` |
| H0125 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:77 `classical_ip_lines` / 捕获后的失败/降级/补偿 | `捕获 UnicodeDecodeError` → `except UnicodeDecodeError:     return None` |
| H0126 / 跨域G23：配置/Agent公开生成入口；不计26路由 | K10:78 `classical_ip_lines` / 校验/诊断降级 | `捕获 UnicodeDecodeError` → `None` |

### K11 [backend/routers/base.py](../../backend/routers/base.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0127 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K11:99 `StaticRouteIndex.__init__` / 捕获后的失败/降级/补偿 | `for route in routes / 捕获 ValueError` → `except ValueError:     continue` |
| H0128 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K11:278 `RouterDriver.probe` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `NotImplementedError` |
| H0129 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K11:281 `RouterDriver.read_state` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `NotImplementedError` |
| H0130 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K11:285 `RouterDriver.read_dhcp_dns` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'{self.vendor} 不支持 DHCP DNS 下发')` |
| H0131 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K11:289 `RouterDriver.apply` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'{self.vendor} 不支持直接写入')` |

### K12 [backend/routers/desired.py](../../backend/routers/desired.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0132 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:23 `rule_source_issue` / 诊断 | `item is None` → `'所选规则集已不存在'` |
| H0133 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:25 `rule_source_issue` / 诊断 | `item.get('enabled') is False` → `'规则集在策略规则中已停用'` |
| H0134 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:27 `rule_source_issue` / 诊断 | `(item.get('library_rule_id') or require_library) and library_rule is None` → `'共享规则库条目已不存在'` |
| H0135 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:29 `rule_source_issue` / 诊断 | `item.get('library_enabled') is False or (library_rule is not None and library_rule.get('enabled') is False)` → `'共享规则库条目已停用'` |
| H0136 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:40 `fake_ip_network` / 捕获后的失败/降级/补偿 | `custom / 捕获 yaml.YAMLError` → `except yaml.YAMLError:     parsed = {}` |
| H0137 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:47 `fake_ip_network` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return ipaddress.IPv4Network(DEFAULT_FAKE_IP_RANGE, strict=False)` |
| H0138 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:73 `_public_networks` / 捕获后的失败/降级/补偿 | `for raw in lines.splitlines() / 捕获 ValueError` → `except ValueError:     continue` |
| H0139 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K12:141 `compute_desired` / 捕获后的失败/降级/补偿 | `for rule_id in ip_rule_sets or [] / 捕获 Exception` → `except Exception:     content = ''` |

### K13 [backend/routers/dhcp_dns.py](../../backend/routers/dhcp_dns.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0140 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:26 `dns_agent_settings` / 抛出 | `not (agent.get('status') != 'online') / not isinstance(host, str)` → `ValueError` |
| H0141 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:29 `dns_agent_settings` / 抛出 | `not (agent.get('status') != 'online') / address.is_unspecified or address.is_loopback or address.is_multicast or (int(address) == 4294967295)` → `ValueError` |
| H0142 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:31 `dns_agent_settings` / 捕获后的失败/降级/补偿 | `not (agent.get('status') != 'online') / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     settings['dns_error'] = '所选 DNS Agent 的主机地址必须是客户端可访问的 IPv4 地址'` |
| H0143 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:40 `_client_ipv4` / 抛出 | `not isinstance(value, str)` → `ValueError` |
| H0144 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:42 `_client_ipv4` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     return ''` |
| H0145 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:85 `matching_networks` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('无法确定 DHCP DNS 下发范围：mihomo 网关不是有效 IPv4 地址')` |
| H0146 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:86 `matching_networks` / 抛出 | `捕获 (ValueError, TypeError)` → `RouterError('无法确定 DHCP DNS 下发范围：mihomo 网关不是有效 IPv4 地址')` |
| H0147 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:93 `matching_networks` / 抛出 | `for entry in state.entries / not entry.id or entry.id in seen` → `RouterError('DHCP 配置缺少唯一标识，无法安全下发 DNS')` |
| H0148 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:97 `matching_networks` / 捕获后的失败/降级/补偿 | `for entry in state.entries / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     detail = '；'.join(entry.blockers)     raise RouterError('DHCP 配置包含无法识别的网段，无法安全确定 DNS 下发范围' + (f'：{detail}' if detail else ''))` |
| H0149 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:99 `matching_networks` / 抛出 | `for entry in state.entries / 捕获 (ValueError, TypeError)` → `RouterError('DHCP 配置包含无法识别的网段，无法安全确定 DNS 下发范围' + (f'：{detail}' if detail else ''))` |
| H0150 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:122 `build_dns_report` / 捕获后的失败/降级/补偿 | `not (state is None) / 捕获 RouterError` → `except RouterError as exc:     error = str(exc)` |
| H0151 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:148 `checked_dns_target` / 抛出 | `len(matches) != 1 or matches[0].id != check['id'] or matches[0].network != check['network']` → `RouterError('回读校验失败：DHCP DNS 的目标网段发生变化')` |
| H0152 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:151 `checked_dns_target` / 抛出 | `entry.blockers` → `RouterError('回读校验失败：' + '；'.join(entry.blockers))` |
| H0153 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K13:158 `verify_dns` / 抛出 | `entry.dns != check['dns'] or not entry.dns_explicit` → `RouterError('回读校验失败：DHCP DNS 与所选服务器不一致')` |

### K14 [backend/routers/dns_guard.py](../../backend/routers/dns_guard.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0154 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:53 `normalize_settings` / 抛出 | `not isinstance(value, dict) or set(value) - set(DEFAULTS)` → `SettingsError('强制 DNS 设置格式无效')` |
| H0155 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:57 `normalize_settings` / 抛出 | `for key in ('enabled', 'block_dot', 'failover') / type(settings[key]) is not bool` → `SettingsError('强制 DNS 的开关必须为布尔值')` |
| H0156 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:60 `normalize_settings` / 抛出 | `not isinstance(bypass, list) or any((not isinstance(item, str) for item in bypass))` → `SettingsError('放行设备必须是 IPv4 地址或网段列表')` |
| H0157 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:65 `normalize_settings` / 捕获后的失败/降级/补偿 | `for item in bypass / 捕获 ValueError` → `except ValueError:     raise SettingsError(f'放行设备「{item}」不是有效的 IPv4 地址或网段') from None` |
| H0158 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:66 `normalize_settings` / 抛出 | `for item in bypass / 捕获 ValueError` → `SettingsError(f'放行设备「{item}」不是有效的 IPv4 地址或网段')` |
| H0159 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:68 `normalize_settings` / 抛出 | `len(networks) > MAX_BYPASS` → `SettingsError(f'放行设备最多 {MAX_BYPASS} 项')` |
| H0160 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:72 `normalize_settings` / 抛出 | `settings['enabled'] / router_type != 'routeros'` → `SettingsError('强制局域网 DNS 走 OxiDNS 仅支持 RouterOS')` |
| H0161 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:74 `normalize_settings` / 抛出 | `settings['enabled'] / not dns_agent_id` → `SettingsError('请先选择 DHCP DNS 服务器（OxiDNS Agent），强制 DNS 使用同一台设备')` |
| H0162 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:97 `snapshot` / 捕获后的失败/降级/补偿 | `for path in PATHS / 捕获 RouterError` → `except RouterError:     if path != NETWATCH:         raise     live[path] = None` |
| H0163 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:99 `snapshot` / 抛出 | `捕获 RouterError / path != NETWATCH` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0164 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:130 `owned_entries` / 抛出 | `for entry in (previous or {}).get('resources', []) / rows is None` → `RouterError('无法读取受管的 Netwatch，不能确认 DNS 劫持对象的归属')` |
| H0165 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:133 `owned_entries` / 抛出 | `for entry in (previous or {}).get('resources', []) / len(matches) > 1` → `RouterError('DNS 劫持对象 ID 不唯一，停止发布')` |
| H0166 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:138 `owned_entries` / 抛出 | `for entry in (previous or {}).get('resources', []) / _comparable_config(entry['role'], stable(entry['path'], matches[0]), failover) != _comparable_config(entry['role'], entry['config'], failover)` → `RouterError(f"DNS 劫持的受管对象被外部修改，保留现场，请先核对：{entry['path']}/{entry['id']}")` |
| H0167 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:148 `_connected` / 抛出 | `not isinstance(rows, list)` → `RouterError('无法读取路由表，不能确定 OxiDNS 所在的局域网接口')` |
| H0168 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:157 `_connected` / 捕获后的失败/降级/补偿 | `for row in rows / 捕获 ValueError` → `except ValueError:     continue` |
| H0169 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:162 `_connected` / 抛出 | `not candidates` → `RouterError('OxiDNS 不在路由器的直连网段内，不能确定劫持所用的局域网接口')` |
| H0170 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:166 `_connected` / 抛出 | `len(selected) != 1` → `RouterError('OxiDNS 所在网段存在多条直连路由，不能确定唯一的局域网接口')` |
| H0171 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:171 `_connected` / 抛出 | `not interface or immediate != interface` → `RouterError('OxiDNS 所在网段的直连路由没有给出唯一接口')` |
| H0172 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:188 `_ha_desired_spec` / 捕获后的失败/降级/补偿 | `捕获 (KeyError, TypeError, ValueError)` → `except (KeyError, TypeError, ValueError):     raise RouterError('无法确定 OxiDNS 高可用的虚拟 IP 和主机地址') from None` |
| H0173 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:189 `_ha_desired_spec` / 抛出 | `捕获 (KeyError, TypeError, ValueError)` → `RouterError('无法确定 OxiDNS 高可用的虚拟 IP 和主机地址')` |
| H0174 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:194 `_ha_desired_spec` / 捕获后的失败/降级/补偿 | `for host in hosts / 捕获 RouterError` → `except RouterError:     same = False` |
| H0175 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:197 `_ha_desired_spec` / 抛出 | `for host in hosts / not same` → `RouterError(f'OxiDNS 高可用主机 {host} 必须与虚拟 IP {vip} 在同一直连网段 {network}')` |
| H0176 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:202 `_ha_desired_spec` / 抛出 | `for address in [vip, *hosts] / any((address in ipaddress.IPv4Network(cidr, strict=False) for cidr in targets))` → `RouterError(f'OxiDNS 地址 {address} 落在代理目的网段内，劫持后的查询会被送往 Mihomo，请检查网段设置')` |
| H0177 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:206 `_ha_desired_spec` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     pass` |
| H0178 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:223 `desired_spec` / 抛出 | `desired.get('dns_error')` → `RouterError('强制 DNS 依赖 DHCP DNS 设置：' + desired['dns_error'])` |
| H0179 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:228 `desired_spec` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     raise RouterError('无法确定 OxiDNS Agent 的 IPv4 地址') from None` |
| H0180 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:229 `desired_spec` / 抛出 | `捕获 ValueError` → `RouterError('无法确定 OxiDNS Agent 的 IPv4 地址')` |
| H0181 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:234 `desired_spec` / 抛出 | `any((agent in ipaddress.IPv4Network(cidr, strict=False) for cidr in targets))` → `RouterError('OxiDNS 地址落在代理目的网段内，劫持后的查询会被送往 Mihomo，请检查网段设置')` |
| H0182 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:238 `desired_spec` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     pass` |
| H0183 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:332 `_hijack_on_top` / 校验/诊断降级 | `for other in nat / other.get('chain') == 'dstnat' and str(other.get('dst-port', '')) == '53' and (str(other.get('disabled')).lower() not in ('true', 'yes')) and ((NAT, str(other.get('.id'))) not in owned_ids)` → `False` |
| H0184 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:376 `build` / 抛出 | `live is None` → `RouterError('无法读取强制 DNS 相关的防火墙配置')` |
| H0185 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:379 `build` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     result.update(status='unknown', message=str(error), blockers=[str(error)])     return result` |
| H0186 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:386 `build` / 阻止/只读原因 | `spec and spec['failover'] and (live[NETWATCH] is None)` → `result['blockers'].append('路由器不支持 Netwatch 或账号无法读取，请关闭「OxiDNS 故障时自动暂停」')` |
| H0187 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:390 `build` / 阻止/只读原因 | `for (path, key, name, label) in ((ADDRESSES, 'list', BYPASS_LIST, '地址列表'), (NETWATCH, 'name', NETWATCH_NAME, 'Netwatch')) / foreign and spec` → `result['blockers'].append(f'{label}「{name}」已被其他配置使用，请先改名或删除这些对象')` |
| H0188 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:481 `_rollback` / 捕获后的失败/降级/补偿 | `for change in changes / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0189 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:482 `_rollback` / 阻止/只读原因 | `for change in changes / 捕获 RouterError` → `failures.append(str(error))` |
| H0190 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:484 `_rollback` / 抛出 | `failures` → `RouterRollbackError('强制 DNS 回滚未完成：' + '；'.join(failures))` |
| H0191 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:509 `apply` / 抛出 | `[entry['id'] for entry in owned] != [entry['id'] for entry in op.get('owned') or []]` → `RouterError('强制 DNS 的受管对象在预览后发生变化，请重新预览')` |
| H0192 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:518 `apply` / 抛出 | `item['role'] in keep / row is None or not _matches(item, row, failover)` → `RouterError('强制 DNS 的受管对象在预览后发生变化，请重新预览')` |
| H0193 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:540 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         remap = _rollback(driver, list(reversed(undo)))     except RouterRollbackError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     driver.dns_guard_manifest = _remapped(previous, remap, driver)     raise` |
| H0194 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:543 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterRollbackError` → `except RouterRollbackError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0195 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:544 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterRollbackError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0196 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:546 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0197 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:564 `verify` / 抛出 | `roles != check['roles'] or (not check['roles'] and manifest)` → `RouterError('回读校验失败：强制 DNS 的受管对象与计划不一致')` |
| H0198 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K14:573 `verify` / 抛出 | `for entry in hijacks / earlier` → `RouterError('回读校验失败：强制 DNS 的劫持规则前面还有其他 53 端口规则')` |

### K15 [backend/routers/forward_hint.py](../../backend/routers/forward_hint.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0199 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:99 `check` / 错误/未知结果 | `not lan or not targets` → `{'status': 'unknown', 'command': '', 'rule': None}` |
| H0200 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:104 `check` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return {'status': 'unknown', 'command': '', 'rule': None}` |
| H0201 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:105 `check` / 错误/未知结果 | `捕获 RouterError` → `{'status': 'unknown', 'command': '', 'rule': None}` |
| H0202 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:155 `ipv4` / 校验/诊断降级 | `router.get('type') != 'routeros' or router.get('manage_return_routing') is True or driver is None` → `None` |
| H0203 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:164 `ipv4` / 校验/诊断降级 | `not targets` → `None` |
| H0204 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:168 `ipv4` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return None` |
| H0205 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:169 `ipv4` / 校验/诊断降级 | `捕获 ValueError` → `None` |
| H0206 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:171 `ipv4` / 错误/未知结果 | `not lan` → `{'status': 'unknown', 'fix_command': '', 'warning': IPV4_UNKNOWN + '无法确定 Mihomo 所在的 LAN 口。'}` |
| H0207 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:178 `ipv4` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception:     result = {'status': 'unknown'}` |
| H0208 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K15:183 `ipv4` / 错误/未知结果 | `result['status'] == 'unknown'` → `{'status': 'unknown', 'fix_command': '', 'warning': IPV4_UNKNOWN + '无法读取 RouterOS IPv4 防火墙。'}` |

### K16 [backend/routers/ikuai.py](../../backend/routers/ikuai.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0209 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:119 `_origin` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return ''` |
| H0210 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:130 `_http_error` / 捕获后的失败/降级/补偿 | `status in (301, 302, 303, 307, 308) / 捕获 ValueError` → `except ValueError:     target = ''` |
| H0211 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:133 `_http_error` / 诊断 | `status in (301, 302, 303, 307, 308) / target and target != _origin(url)` → `f'iKuai 要求跳转到 {target}{code}。请核对浏览器打开的管理地址，在「编辑路由器」中修改地址后重试。'` |
| H0212 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:135 `_http_error` / 诊断 | `status in (301, 302, 303, 307, 308)` → `f'iKuai 将请求转到了其他页面{code}。请在浏览器打开管理页面，核对最终地址的协议和端口；若地址无误，请检查管理入口或反向代理的跳转设置。'` |
| H0213 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:152 `_http_error` / 诊断 | `依赖/文件/事务故障；按函数前置条件` → `f'iKuai {reason}{code}。'` |
| H0214 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:169 `_call_error` / 诊断 | `code == 1001` → `f'iKuai 账号没有{feature}的访问权限（错误码 1001）。请在 iKuai 的用户组中授予读取权限，或改用管理员账号。'` |
| H0215 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:172 `_call_error` / 诊断 | `code == 1002` → `f'iKuai 账号没有{feature}的执行权限（错误码 1002）。请在 iKuai 的用户组中授予修改权限，或改用管理员账号。'` |
| H0216 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:175 `_call_error` / 诊断 | `code == 1003` → `f"iKuai 不提供 {func_name} 接口（错误码 1003{('：' + message if message else '')}）。当前固件可能不支持这项功能，请确认 iKuai 版本。"` |
| H0217 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:178 `_call_error` / 诊断 | `code == 1008` → `'iKuai 登录会话已失效（错误码 1008），重新登录后仍未恢复，请稍后重试。'` |
| H0218 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:180 `_call_error` / 诊断 | `code is None` → `f'iKuai 调用 {func_name} 返回格式无法识别'` |
| H0219 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:181 `_call_error` / 诊断 | `依赖/文件/事务故障；按函数前置条件` → `f"iKuai 调用 {func_name} 失败：{message or '未知错误'}（错误码 {code}）"` |
| H0220 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:208 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 requests.exceptions.SSLError` → `except requests.exceptions.SSLError:     raise RouterError('iKuai HTTPS 连接失败：请检查证书是否可信、是否过期，以及管理地址和 HTTPS 端口是否正确。') from None` |
| H0221 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:209 `IKuaiDriver._post` / 抛出 | `捕获 requests.exceptions.SSLError` → `RouterError('iKuai HTTPS 连接失败：请检查证书是否可信、是否过期，以及管理地址和 HTTPS 端口是否正确。')` |
| H0222 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:211 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 requests.ConnectTimeout` → `except requests.ConnectTimeout:     raise RouterError(f'连接 iKuai 超时（{TIMEOUT} 秒）：请检查管理地址、端口，以及 ConfigFlow 所在主机到路由器的网络是否畅通。') from None` |
| H0223 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:212 `IKuaiDriver._post` / 抛出 | `捕获 requests.ConnectTimeout` → `RouterError(f'连接 iKuai 超时（{TIMEOUT} 秒）：请检查管理地址、端口，以及 ConfigFlow 所在主机到路由器的网络是否畅通。')` |
| H0224 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:214 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 requests.Timeout` → `except requests.Timeout:     raise RouterError(f'等待 iKuai 响应超时（{timeout:g} 秒）：请检查网络连接和路由器负载，稍后重试。') from None` |
| H0225 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:215 `IKuaiDriver._post` / 抛出 | `捕获 requests.Timeout` → `RouterError(f'等待 iKuai 响应超时（{timeout:g} 秒）：请检查网络连接和路由器负载，稍后重试。')` |
| H0226 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:216 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 requests.ConnectionError` → `except requests.ConnectionError:     raise RouterError('无法连接 iKuai 管理服务：请检查地址、端口和域名解析，确认管理服务已开启，并允许 ConfigFlow 所在主机访问。') from None` |
| H0227 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:217 `IKuaiDriver._post` / 抛出 | `捕获 requests.ConnectionError` → `RouterError('无法连接 iKuai 管理服务：请检查地址、端口和域名解析，确认管理服务已开启，并允许 ConfigFlow 所在主机访问。')` |
| H0228 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:219 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 requests.RequestException` → `except requests.RequestException:     raise RouterError('与 iKuai 通信失败：请检查管理地址、端口和网络连接，稍后重试。') from None` |
| H0229 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:220 `IKuaiDriver._post` / 抛出 | `捕获 requests.RequestException` → `RouterError('与 iKuai 通信失败：请检查管理地址、端口和网络连接，稍后重试。')` |
| H0230 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:222 `IKuaiDriver._post` / 抛出 | `response.status_code != 200` → `RouterError(_http_error(response, url))` |
| H0231 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:225 `IKuaiDriver._post` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     content = getattr(response, 'content', None)     if isinstance(content, bytes) and (not content.strip()):         raise RouterError('iKuai 没有返回任何内容：请求可能过大或被 iKuai 拒绝') from None     raise RouterError('iKuai 返回的不是 JSON，请确认地址指向管理页面') from None` |
| H0232 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:229 `IKuaiDriver._post` / 抛出 | `捕获 ValueError / isinstance(content, bytes) and (not content.strip())` → `RouterError('iKuai 没有返回任何内容：请求可能过大或被 iKuai 拒绝')` |
| H0233 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:230 `IKuaiDriver._post` / 抛出 | `捕获 ValueError` → `RouterError('iKuai 返回的不是 JSON，请确认地址指向管理页面')` |
| H0234 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:232 `IKuaiDriver._post` / 抛出 | `not isinstance(data, dict)` → `RouterError('iKuai 返回格式无法识别')` |
| H0235 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:251 `IKuaiDriver._login` / 抛出 | `failed` → `RouterError(f'iKuai 登录失败：{detail}{code}')` |
| H0236 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:267 `IKuaiDriver.call` / 抛出 | `'code' not in data and ('Data' in data or 'Result' in data)` → `RouterError('检测到 iKuai 3.x 的响应格式，只支持 iKuai 4.x')` |
| H0237 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:268 `IKuaiDriver.call` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(_call_error(func_name, data))` |
| H0238 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:282 `IKuaiDriver._list_all` / 抛出 | `type(count) is not int or count < 0 or (not isinstance(page, list)) or (total is not None and count != total) or (len(page) != min(PAGE_SIZE, max(0, count - offset)))` → `RouterError(incomplete)` |
| H0239 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:287 `IKuaiDriver._list_all` / 抛出 | `not ids / not isinstance(row, dict)` → `RouterError(incomplete)` |
| H0240 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:290 `IKuaiDriver._list_all` / 抛出 | `not (not ids) / not isinstance(row, dict) or type(row.get('id')) is not int or row['id'] < 1 or (row['id'] in seen)` → `RouterError(f'iKuai {label}列表缺少唯一有效 ID，无法安全编辑')` |
| H0241 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:303 `IKuaiDriver.probe` / 抛出 | `not version.startswith('4.')` → `RouterError(f"只支持 iKuai 4.x，当前版本 {version or '未知'}")` |
| H0242 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:310 `IKuaiDriver.probe` / 捕获后的失败/降级/补偿 | `for (func, capability) in (('static_rt', 'routes'), ('nat_rule', 'nat_bypass'), ('route_object', 'address_sets'), ('stream_ipport', 'policy_routes')) / 捕获 RouterError` → `except RouterError:     pass` |
| H0243 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:320 `IKuaiDriver.read_state` / 捕获后的失败/降级/补偿 | `for row in self._list_all('static_rt') / 捕获 ValueError` → `except ValueError:     continue` |
| H0244 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:362 `IKuaiDriver._active_routes` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return None` |
| H0245 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:363 `IKuaiDriver._active_routes` / 校验/诊断降级 | `捕获 RouterError` → `None` |
| H0246 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:368 `IKuaiDriver._active_routes` / 捕获后的失败/降级/补偿 | `for row in rows / 捕获 ValueError` → `except ValueError:     continue` |
| H0247 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:396 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / missing or unknown` → `blockers.append('iKuai DHCP 字段与已验证的 4.x 格式不兼容，无法保留完整配置' + f"（缺少：{','.join(sorted(missing)) or '无'}；未知：{','.join(sorted(unknown)) or '无'}）")` |
| H0248 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:405 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for key in DHCP_FIELDS / not valid` → `blockers.append(f'iKuai DHCP 字段 {key} 类型无法识别')` |
| H0249 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:407 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / row.get('enabled') not in ('yes', 'no')` → `blockers.append('iKuai DHCP 启用状态无法识别')` |
| H0250 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:409 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / row.get('status') != 1 or type(row.get('status')) is not int` → `blockers.append('iKuai DHCP 服务未生效或无法确认运行状态')` |
| H0251 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:411 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / row.get('check_relay_only') != 0` → `blockers.append('iKuai DHCP 仅响应中继请求，无法确认该网段客户端的 DNS 覆盖')` |
| H0252 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:413 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / row.get('phy_ifnames') != 'all'` → `blockers.append('iKuai DHCP 限制了物理接口，无法确认完整网段的 DNS 覆盖')` |
| H0253 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:420 `IKuaiDriver.read_dhcp_dns` / 抛出 | `for row in rows / end not in subnet or start > end or ipaddress.IPv4Address(row.get('gateway')) not in subnet` → `ValueError` |
| H0254 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:422 `IKuaiDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for row in rows / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     blockers.append('iKuai DHCP 地址池、掩码或网关无法确定唯一网段')` |
| H0255 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:423 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in rows / 捕获 (ValueError, TypeError)` → `blockers.append('iKuai DHCP 地址池、掩码或网关无法确定唯一网段')` |
| H0256 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:433 `IKuaiDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for field in ('dns1', 'dns2') / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     blockers.append('iKuai DHCP DNS 包含无法识别的自动或继承配置')` |
| H0257 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:434 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for field in ('dns1', 'dns2') / 捕获 (ValueError, TypeError)` → `blockers.append('iKuai DHCP DNS 包含无法识别的自动或继承配置')` |
| H0258 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:440 `IKuaiDriver.read_dhcp_dns` / 抛出 | `for binding in bindings / not isinstance(binding.get('ip_addr'), str)` → `ValueError` |
| H0259 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:442 `IKuaiDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for binding in bindings / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     blockers.append('iKuai 静态 DHCP 绑定缺少有效地址，无法排除 DNS 覆盖')     continue` |
| H0260 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:443 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for binding in bindings / 捕获 (ValueError, TypeError)` → `blockers.append('iKuai 静态 DHCP 绑定缺少有效地址，无法排除 DNS 覆盖')` |
| H0261 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:449 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for binding in bindings / binding.get('enabled') != 'yes' or not all((key in binding for key in DHCP_STATIC_FIELDS)) or any((not isinstance(binding.get(key), str) for key in DHCP_STATIC_FIELDS if key != 'id'))` → `blockers.append('iKuai 静态 DHCP 绑定信息不完整，无法排除 DNS 覆盖')` |
| H0262 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:451 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `not (binding.get('enabled') != 'yes' or not all((key in binding for key in DHCP_STATIC_FIELDS)) or any((not isinstance(binding.get(key), str) for key in DHCP_STATIC_FIELDS if key != 'id'))) / binding['dns1'] or binding['dns2']` → `blockers.append('该网段存在静态 DHCP 绑定的独立 DNS，请先移除绑定中的 DNS 覆盖')` |
| H0263 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:453 `IKuaiDriver.read_dhcp_dns` / 阻止/只读原因 | `for binding in bindings / binding.keys() - set(DHCP_STATIC_FIELDS) - {'termname', 'hostname', 'ip_addr_int'}` → `blockers.append('iKuai 静态 DHCP 绑定含未验证字段，无法排除 DNS 覆盖')` |
| H0264 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:466 `IKuaiDriver._dhcp_entry` / 抛出 | `len(matches) != 1` → `RouterError('iKuai DHCP 目标配置不存在或标识不唯一')` |
| H0265 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:473 `IKuaiDriver._dhcp_edit_verified` / 抛出 | `actual.attributes != payload or actual.blockers` → `RouterError('iKuai DHCP DNS 回读校验失败：DNS 或其他 DHCP 配置与提交值不一致')` |
| H0266 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:479 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_restore' / not isinstance(payload, dict) or set(payload) != set(DHCP_FIELDS)` → `RouterError('iKuai DHCP DNS 撤销记录不完整')` |
| H0267 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:490 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_update' / not isinstance(scope, dict) or any((not isinstance(scope.get(key), str) or not scope[key] for key in ('id', 'network', 'gateway')))` → `RouterError('iKuai DHCP DNS 操作缺少已预览网段')` |
| H0268 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:493 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_update' / entry.id != str(op.get('id')) or entry.attributes != op.get('expected')` → `RouterError('iKuai DHCP 配置已变化，请重新预览')` |
| H0269 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:498 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_update' / not isinstance(target, list) or len(target) != 1 or (not isinstance(target[0], str))` → `ValueError` |
| H0270 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:501 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_update' / address.is_unspecified or address.is_multicast or address.is_loopback or (int(address) == 4294967295)` → `ValueError` |
| H0271 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:502 `IKuaiDriver._dhcp_apply` / 捕获后的失败/降级/补偿 | `op['kind'] == 'dhcp_dns_update' / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('iKuai DHCP DNS 需要单个有效 IPv4 地址') from None` |
| H0272 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:503 `IKuaiDriver._dhcp_apply` / 抛出 | `op['kind'] == 'dhcp_dns_update' / 捕获 (ValueError, TypeError)` → `RouterError('iKuai DHCP DNS 需要单个有效 IPv4 地址')` |
| H0273 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:511 `IKuaiDriver._dhcp_apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         try:             actual = self._dhcp_entry(self.read_dhcp_dns(), payload['id'])             changed = actual.attributes != before         except RouterError:             changed = True         if changed:             self._dhcp_edit_verified(before)     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；DHCP DNS 局部撤销失败：{rollback_error}') from None     raise` |
| H0274 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:516 `IKuaiDriver._dhcp_apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError:     changed = True` |
| H0275 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:522 `IKuaiDriver._dhcp_apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；DHCP DNS 局部撤销失败：{rollback_error}') from None` |
| H0276 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:523 `IKuaiDriver._dhcp_apply` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；DHCP DNS 局部撤销失败：{rollback_error}')` |
| H0277 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:524 `IKuaiDriver._dhcp_apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0278 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:542 `IKuaiDriver._write` / 抛出 | `data.get('code') != 0 / data.get('code') in (1001, 1002, 1003, 1008)` → `_Rejected(_call_error(func_name, data))` |
| H0279 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:545 `IKuaiDriver._write` / 抛出 | `data.get('code') != 0` → `_Rejected(f"iKuai {func_name} {action} 失败：{data.get('message') or data.get('code')}" + (f'（{details}）' if details else ''))` |
| H0280 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:585 `IKuaiDriver._static_write` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0281 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:586 `IKuaiDriver._static_write` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0282 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:587 `IKuaiDriver._static_write` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     if batch is not None:         batch['rows'] = None     if undo is not None:         error.undo = {**undo, 'confirmed': False}     raise` |
| H0283 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:593 `IKuaiDriver._static_write` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0284 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:611 `IKuaiDriver._row` / 抛出 | `row is None` → `RouterError(f'iKuai {func_name} 中找不到 id={row_id}，可能已被手动删除')` |
| H0285 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:618 `IKuaiDriver._find_id` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'iKuai {func_name} 已提交但读不回新对象')` |
| H0286 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:644 `IKuaiDriver._confirm_added` / 抛出 | `missing` → `RouterError('iKuai static_rt 已提交但读不回新对象：' + '、'.join((undo['cidr'] for undo in missing)))` |
| H0287 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:652 `IKuaiDriver._new_id` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     error.undo = undo     raise` |
| H0288 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:654 `IKuaiDriver._new_id` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0289 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:659 `IKuaiDriver._new_id` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `error` |
| H0290 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:675 `IKuaiDriver._add` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0291 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:676 `IKuaiDriver._add` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0292 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:677 `IKuaiDriver._add` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     error.undo = {**undo, 'confirmed': False}     raise` |
| H0293 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:679 `IKuaiDriver._add` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0294 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:689 `IKuaiDriver._revert_add` / 抛出 | `not tagged / op['confirmed']` → `RouterError(f"iKuai {func_name} 中找不到本次新增的对象（名称 {match['tagname']}），可能已被手动修改或删除，请检查")` |
| H0295 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:693 `IKuaiDriver._revert_add` / 抛出 | `len(tagged) != 1 or not self._matches(tagged[0], match)` → `RouterError(f"iKuai {func_name} 中名称为 {match['tagname']} 的对象已被修改，未自动删除，请检查")` |
| H0296 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:760 `IKuaiDriver._edit` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0297 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:761 `IKuaiDriver._edit` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0298 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:762 `IKuaiDriver._edit` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     error.undo = undo     raise` |
| H0299 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:764 `IKuaiDriver._edit` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0300 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:769 `IKuaiDriver._target_refs` / 校验/诊断降级 | `'groups' not in op` → `None` |
| H0301 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:827 `IKuaiDriver._group_role` / 抛出 | `role not in ikuai_groups.ROLES or not ikuai_groups.owned_role(op.get('name'), ikuai_groups.ROLES[role])` → `RouterError('iKuai IP 分组操作无效，请重新预览')` |
| H0302 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:835 `IKuaiDriver._owned_group` / 抛出 | `len(found) != 1 or (found[0]['role'] != role and (not (unmarked and found[0]['role'] is None)))` → `RouterError(f"iKuai IP 分组「{op['name']}」不存在或不属于 ConfigFlow，请重新预览")` |
| H0303 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:837 `IKuaiDriver._owned_group` / 抛出 | `found[0]['id'] is None` → `RouterError(f"iKuai IP 分组「{op['name']}」缺少有效 ID，无法安全编辑")` |
| H0304 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:854 `IKuaiDriver._group_write` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0305 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:855 `IKuaiDriver._group_write` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0306 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:856 `IKuaiDriver._group_write` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     if undo is not None:         error.undo = undo     raise` |
| H0307 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:860 `IKuaiDriver._group_write` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0308 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:881 `IKuaiDriver._cf_refs` / 抛出 | `missing` → `RouterError(f"iKuai IP 分组 {'、'.join(missing)} 已提交但读不回预期内容，无法引用")` |
| H0309 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:910 `IKuaiDriver._rebind` / 抛出 | `not (len(found) == 1) / current is None` → `RouterError(f"iKuai IP 分组 {ref['gp_name']} 不存在，无法恢复规则引用")` |
| H0310 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:919 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_add' / self._named_groups(name)` → `RouterError(f'iKuai IP 分组「{name}」已存在，请重新预览')` |
| H0311 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:932 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_update' and op.get('mark') / ikuai_groups.digest(group['values']) != op['expected']` → `RouterError(f'iKuai IP 分组「{name}」在预览后发生变化，请重新预览')` |
| H0312 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:939 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_update' / ikuai_groups.digest(group['values']) != op['expected']` → `RouterError(f'iKuai IP 分组「{name}」在预览后发生变化，请重新预览')` |
| H0313 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:952 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_delete' / ikuai_groups.digest(group['values']) != op['expected']` → `RouterError(f'iKuai IP 分组「{name}」在预览后发生变化，请重新预览')` |
| H0314 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:955 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_delete' / users` → `RouterError(f"iKuai IP 分组「{name}」仍被{'、'.join(users)}引用，未删除")` |
| H0315 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:965 `IKuaiDriver._apply_group` / 抛出 | `not found / op['confirmed']` → `RouterError(f'iKuai 中找不到本次新增的 IP 分组「{name}」，可能已被手动修改或删除，请检查')` |
| H0316 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:970 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_undo' / len(found) != 1 or group['role'] != role or ikuai_groups.digest(group['values']) != op['expected']` → `RouterError(f'iKuai IP 分组「{name}」已被修改，未自动删除，请检查')` |
| H0317 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:972 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_undo' / group['id'] is None` → `RouterError(f'iKuai IP 分组「{name}」缺少有效 ID，未自动删除，请检查')` |
| H0318 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:975 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_undo' / users` → `RouterError(f"iKuai IP 分组「{name}」仍被{'、'.join(users)}引用，未自动删除")` |
| H0319 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:985 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_restore' / current != op['expected']` → `RouterError(f'iKuai IP 分组「{name}」已被修改，保留现状，请检查')` |
| H0320 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:999 `IKuaiDriver._apply_group` / 抛出 | `kind == 'ikuai_group_readd' / found` → `RouterError(f'iKuai IP 分组「{name}」已被其他配置占用，无法恢复，请检查')` |
| H0321 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:1006 `IKuaiDriver._apply_group` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'不支持的操作 {kind}')` |
| H0322 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:1022 `IKuaiDriver._add_route` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     error.undo = undo     raise` |
| H0323 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:1024 `IKuaiDriver._add_route` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0324 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:1092 `IKuaiDriver.apply` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'不支持的操作 {kind}')` |
| H0325 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K16:1142 `oversized_requests` / 阻止/只读原因 | `for op in ops / size > MAX_REQUEST_BYTES` → `blockers.append(f'iKuai {label}「{name}」需要一次写入 {count} {unit}，请求约 {size // 1024} KB，超过 iKuai 单次请求上限（约 {MAX_REQUEST_BYTES // 1024} KB），发布会被拒绝。请减少代理网段（例如少选规则集）后重试')` |

### K17 [backend/routers/ikuai_clients.py](../../backend/routers/ikuai_clients.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0326 / R004/R005/R006 | K17:43 `rows` / 捕获后的失败/降级/补偿 | `for page in range(MAX_PAGES) / 捕获 (TypeError, ValueError)` → `except (TypeError, ValueError):     total = None` |
| H0327 / R004/R005/R006 | K17:66 `parse_terminal` / 校验/诊断降级 | `not ip` → `None` |
| H0328 / R004/R005/R006 | K17:79 `parse_lease` / 校验/诊断降级 | `not ip` → `None` |
| H0329 / R004/R005/R006 | K17:93 `lan_addresses` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return None` |
| H0330 / R004/R005/R006 | K17:94 `lan_addresses` / 校验/诊断降级 | `捕获 RouterError` → `None` |
| H0331 / R004/R005/R006 | K17:101 `lan_addresses` / 捕获后的失败/降级/补偿 | `for item in text.replace(',', ' ').split() / 捕获 ValueError` → `except ValueError:     continue` |
| H0332 / R004/R005/R006 | K17:119 `read` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     reading.notes.append(f'无法读取 DHCP 租约：{error}')     leases = []` |

### K18 [backend/routers/ikuai_fixed_ip.py](../../backend/routers/ikuai_fixed_ip.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0333 / R005/R006（固定IP） | K18:32 `_fields` / 抛出 | `set(DHCP_STATIC_FIELDS) - set(row) or set(row) - FIELDS - RUNTIME or type(row.get('id')) is not int or (row['id'] < 1) or any((not isinstance(row.get(key), str) for key in set(DHCP_STATIC_FIELDS) - {'id'})) or (row['enabled'] not in ('yes', 'no')) or (not ipv4(row['ip_addr'])) or (not _mac(row['mac']))` → `RouterError('iKuai 静态 DHCP 配置字段不完整或与已验证格式不同，无法安全修改。')` |
| H0334 / R005/R006（固定IP） | K18:65 `_scopes` / 捕获后的失败/降级/补偿 | `for row in snapshot['servers'] / 捕获 (ValueError, TypeError, KeyError)` → `except (ValueError, TypeError, KeyError):     continue` |
| H0335 / R005/R006（固定IP） | K18:115 `preview` / 抛出 | `not state['eligible']` → `RouterConflictError(state['reason'])` |
| H0336 / R005/R006（固定IP） | K18:119 `preview` / 抛出 | `len(name) > 80 or any((ord(char) < 32 for char in name))` → `RouterError('设备名称不能超过 80 字符或包含控制字符')` |
| H0337 / R005/R006（固定IP） | K18:140 `_remove` / 抛出 | `not binding.get('comment', '').startswith(PREFIX) or not _matches(current, binding) or _fields(current) != expected` → `RouterConflictError('iKuai 固定 IP 已被其他操作修改，保留现状，不会删除。')` |
| H0338 / R005/R006（固定IP） | K18:143 `_remove` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0339 / R005/R006（固定IP） | K18:144 `_remove` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0340 / R005/R006（固定IP） | K18:145 `_remove` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     if _row(driver, binding['id']) is None:         return     raise` |
| H0341 / R005/R006（固定IP） | K18:148 `_remove` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0342 / R005/R006（固定IP） | K18:150 `_remove` / 抛出 | `_row(driver, binding['id']) is not None` → `RouterError('iKuai 静态分配删除后仍存在，回读校验失败。')` |
| H0343 / R005/R006（固定IP） | K18:161 `_new_row` / 校验/诊断降级 | `len(matches) != 1 or matches[0]['id'] in known_ids or _fields(matches[0]) != _expected(payload)` → `None` |
| H0344 / R005/R006（固定IP） | K18:167 `apply` / 抛出 | `preview(driver, op.get('ip'), op.get('mac'), op.get('name')) != op` → `RouterConflictError('iKuai DHCP 设置在预览后发生变化，请重新预览。')` |
| H0345 / R005/R006（固定IP） | K18:179 `apply` / 抛出 | `acquired is None` → `RouterError('iKuai 新增静态分配回读不一致，无法核对 DNS、IP 或归属。')` |
| H0346 / R005/R006（固定IP） | K18:182 `apply` / 抛出 | `actual is None or _fields(actual) != _expected(payload)` → `RouterError('iKuai 静态分配按 ID 回读校验失败。')` |
| H0347 / R005/R006（固定IP） | K18:184 `apply` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0348 / R005/R006（固定IP） | K18:185 `apply` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0349 / R005/R006（固定IP） | K18:186 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         acquired = acquired or _new_row(driver, known_ids, payload)         if acquired is not None:             _remove(driver, _binding(acquired), _expected(payload))         else:             rows = _static(driver)             if any((row['tagname'] == payload['tagname'] for row in rows)):                 raise RouterError('新绑定已发生外部变化，无法安全撤销，保留现场')     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；iKuai 固定 IP 回滚未完成：{rollback_error}') from error     raise` |
| H0350 / R005/R006（固定IP） | K18:194 `apply` / 抛出 | `not (acquired is not None) / any((row['tagname'] == payload['tagname'] for row in rows))` → `RouterError('新绑定已发生外部变化，无法安全撤销，保留现场')` |
| H0351 / R005/R006（固定IP） | K18:195 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；iKuai 固定 IP 回滚未完成：{rollback_error}') from error` |
| H0352 / R005/R006（固定IP） | K18:196 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；iKuai 固定 IP 回滚未完成：{rollback_error}')` |
| H0353 / R005/R006（固定IP） | K18:197 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0354 / R005/R006（固定IP） | K18:202 `rollback` / 抛出 | `not isinstance(binding.get('undo'), dict)` → `RouterError('iKuai 固定 IP 回滚缺少本次写入证据。')` |
| H0355 / R005/R006（固定IP） | K18:208 `preview_delete` / 抛出 | `binding.get('vendor') != 'ikuai' or not binding.get('comment', '').startswith(PREFIX)` → `RouterConflictError('只能删除 ConfigFlow 创建的静态分配。')` |
| H0356 / R005/R006（固定IP） | K18:211 `preview_delete` / 抛出 | `not _matches(current, binding)` → `RouterConflictError('iKuai 静态分配已不存在或 IP、MAC、归属发生变化，不能删除。')` |
| H0357 / R005/R006（固定IP） | K18:219 `delete` / 抛出 | `'expected' in binding and op != binding` → `RouterConflictError('iKuai 静态分配在预览后发生变化，请重新预览。')` |
| H0358 / R005/R006（固定IP） | K18:222 `delete` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0359 / R005/R006（固定IP） | K18:223 `delete` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0360 / R005/R006（固定IP） | K18:224 `delete` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         if _row(driver, op['id']) is None:             return op     except RouterError as read_error:         raise RouterRollbackError(f'{error}；iKuai 删除结果无法确认，请检查路由器：{read_error}') from error     raise` |
| H0361 / R005/R006（固定IP） | K18:230 `delete` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as read_error:     raise RouterRollbackError(f'{error}；iKuai 删除结果无法确认，请检查路由器：{read_error}') from error` |
| H0362 / R005/R006（固定IP） | K18:231 `delete` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；iKuai 删除结果无法确认，请检查路由器：{read_error}')` |
| H0363 / R005/R006（固定IP） | K18:232 `delete` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0364 / R005/R006（固定IP） | K18:238 `restore_deleted` / 抛出 | `receipt.get('kind') != 'delete' or not receipt.get('comment', '').startswith(PREFIX)` → `RouterError('iKuai 删除回滚缺少归属证据。')` |
| H0365 / R005/R006（固定IP） | K18:244 `restore_deleted` / 抛出 | `current` → `RouterError('iKuai 原静态分配 ID 已被占用，保留现状。')` |
| H0366 / R005/R006（固定IP） | K18:247 `restore_deleted` / 抛出 | `any((ipv4(row['ip_addr']) == receipt['ip'] or _mac(row['mac']) == receipt['mac'] or row['tagname'] == receipt['expected']['tagname'] for row in rows))` → `RouterError('iKuai 删除回滚时 IP、MAC 或名称已被占用，保留现状。')` |
| H0367 / R005/R006（固定IP） | K18:251 `restore_deleted` / 捕获后的失败/降级/补偿 | `捕获 _Rejected` → `except _Rejected:     raise` |
| H0368 / R005/R006（固定IP） | K18:252 `restore_deleted` / 抛出 | `捕获 _Rejected` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0369 / R005/R006（固定IP） | K18:253 `restore_deleted` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     restored = _new_row(driver, known_ids, receipt['expected'])     if restored is not None:         return _binding(restored)     raise` |
| H0370 / R005/R006（固定IP） | K18:257 `restore_deleted` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0371 / R005/R006（固定IP） | K18:260 `restore_deleted` / 抛出 | `restored is None` → `RouterRollbackError('iKuai 删除回滚后无法核对原静态分配。')` |

### K19 [backend/routers/ikuai_groups.py](../../backend/routers/ikuai_groups.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0372 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:79 `canon` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return text` |
| H0373 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:460 `GroupPlanner.family` / 阻止/只读原因 | `result['error']` → `self.blockers.append(result['error'])` |
| H0374 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:576 `GroupPlanner.migration_warning` / 校验/诊断降级 | `not self.migrations` → `None` |
| H0375 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:646 `verify` / 抛出 | `gstate is None` → `RouterError('回读校验失败：无法读取 iKuai IP 分组')` |
| H0376 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:656 `verify` / 抛出 | `for (name, expected) in stage_checks['groups'].items() / len(found) != 1 or not found[0]['role'] or found[0].get('adopted') or (digest(found[0]['values']) != expected)` → `RouterError(f'回读校验失败：IP 分组「{name}」内容与计划不一致')` |
| H0377 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:666 `verify` / 抛出 | `for check in stage_checks['rules'] / not any((_rule_matches(groups, rule, check) for rule in candidates))` → `RouterError(f"回读校验失败：{label}「{check['comment']}」没有按计划只引用 ConfigFlow 的 IP 分组")` |
| H0378 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:669 `verify` / 抛出 | `for name in stage_checks['removed'] / any((group['role'] for group in by_name.get(name, [])))` → `RouterError(f'回读校验失败：应删除的 IP 分组「{name}」仍然存在')` |
| H0379 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:674 `_rule_matches` / 校验/诊断降级 | `rule['custom']` → `False` |
| H0380 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:680 `_rule_matches` / 校验/诊断降级 | `kind == 'owned' / ref.get('gp_name') != group['name']` → `False` |
| H0381 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K19:683 `_rule_matches` / 校验/诊断降级 | `not (kind == 'owned') / kind == 'stale' or not check.get('keep_foreign')` → `False` |

### K20 [backend/routers/ikuai_routes.py](../../backend/routers/ikuai_routes.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0382 / R019/R020/R021 | K20:28 `_entry` / 阻止/只读原因 | `missing or unknown` → `reasons.append(f"路由字段与已验证的 iKuai 4.x 格式不兼容，无法保留完整配置（缺少：{','.join(sorted(missing)) or '无'}；未知：{','.join(sorted(unknown)) or '无'}）")` |
| H0383 / R019/R020/R021 | K20:39 `_entry` / 阻止/只读原因 | `for key in ROUTE_FIELDS / not valid` → `reasons.append(f'路由字段 {key} 类型无法识别')` |
| H0384 / R019/R020/R021 | K20:41 `_entry` / 阻止/只读原因 | `snapshot.get('ip_type') != '4'` → `reasons.append('仅支持普通 IPv4 静态路由')` |
| H0385 / R019/R020/R021 | K20:43 `_entry` / 阻止/只读原因 | `snapshot.get('enabled') not in ('yes', 'no')` → `reasons.append('路由启用状态无法识别')` |
| H0386 / R019/R020/R021 | K20:45 `_entry` / 阻止/只读原因 | `not isinstance(snapshot.get('interface'), str) or not snapshot['interface']` → `reasons.append('路由接口不完整')` |
| H0387 / R019/R020/R021 | K20:47 `_entry` / 阻止/只读原因 | `not isinstance(snapshot.get('tagname'), str) or not 1 <= len(snapshot['tagname']) <= 15` → `reasons.append('路由名称无法按原表单保留')` |
| H0388 / R019/R020/R021 | K20:52 `_entry` / 阻止/只读原因 | `not isinstance(ipaddress.ip_network(cidr), ipaddress.IPv4Network)` → `reasons.append('仅支持普通 IPv4 静态路由')` |
| H0389 / R019/R020/R021 | K20:53 `_entry` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     reasons.append('路由目的地址或掩码无法识别')` |
| H0390 / R019/R020/R021 | K20:54 `_entry` / 阻止/只读原因 | `捕获 ValueError` → `reasons.append('路由目的地址或掩码无法识别')` |
| H0391 / R019/R020/R021 | K20:58 `_entry` / 抛出 | `not isinstance(gateway, str)` → `ValueError` |
| H0392 / R019/R020/R021 | K20:60 `_entry` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     reasons.append('仅支持下一跳为 IPv4 地址的静态路由')` |
| H0393 / R019/R020/R021 | K20:61 `_entry` / 阻止/只读原因 | `捕获 (ValueError, TypeError)` → `reasons.append('仅支持下一跳为 IPv4 地址的静态路由')` |
| H0394 / R019/R020/R021 | K20:79 `_values` / 抛出 | `not isinstance(values, dict) or set(values) != {'cidr', 'gateway', 'enabled', 'comment'} or (not isinstance(values['cidr'], str)) or (not isinstance(values['gateway'], str)) or (type(values['enabled']) is not bool) or (not isinstance(values['comment'], str))` → `RouterError('静态路由参数格式无效')` |
| H0395 / R019/R020/R021 | K20:83 `_values` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     raise RouterError('静态路由目的网段和下一跳必须是 IPv4') from None` |
| H0396 / R019/R020/R021 | K20:84 `_values` / 抛出 | `捕获 ValueError` → `RouterError('静态路由目的网段和下一跳必须是 IPv4')` |
| H0397 / R019/R020/R021 | K20:95 `_no_duplicate` / 抛出 | `any((entry['cidr'] == cidr and entry['id'] != own_id for entry in entries))` → `RouterConflictError(f'已存在目的网段 {cidr} 的静态路由，请先处理重复配置')` |
| H0398 / R019/R020/R021 | K20:105 `_new_entry` / 校验/诊断降级 | `len(tagged) != 1 or tagged[0]['id'] in before_ids or (not tagged[0]['editable'])` → `None` |
| H0399 / R019/R020/R021 | K20:113 `_verify_record` / 抛出 | `not _snapshot_equal(entry, snapshot)` → `RouterError('静态路由回读与本次提交不一致')` |
| H0400 / R019/R020/R021 | K20:123 `_restore_update` / 抛出 | `not _snapshot_equal(current, desired)` → `RouterError('路由已被其他操作修改或删除，保留现状，未覆盖外部变更')` |
| H0401 / R019/R020/R021 | K20:127 `_restore_update` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     _verify_record(driver, before['id'], before['snapshot'])     return` |
| H0402 / R019/R020/R021 | K20:140 `_restore_delete` / 抛出 | `current is not None` → `RouterError('原路由 ID 已发生变化，保留现状，未覆盖外部变更')` |
| H0403 / R019/R020/R021 | K20:145 `_restore_delete` / 抛出 | `any((entry['snapshot'].get('tagname') == payload['tagname'] for entry in latest))` → `RouterError('原路由名称已被其他配置占用，未自动重建')` |
| H0404 / R019/R020/R021 | K20:149 `_restore_delete` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     if _new_entry(read_routes(driver), before_ids, payload) is None:         raise     return` |
| H0405 / R019/R020/R021 | K20:151 `_restore_delete` / 抛出 | `捕获 RouterError / _new_entry(read_routes(driver), before_ids, payload) is None` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0406 / R019/R020/R021 | K20:154 `_restore_delete` / 抛出 | `_new_entry(read_routes(driver), before_ids, payload) is None` → `RouterError('删除失败后的原路由重建无法精确确认，请检查路由列表')` |
| H0407 / R019/R020/R021 | K20:166 `_restore_add` / 抛出 | `entry is None` → `RouterError('无法唯一确认本次新增路由，已保留所有配置，未按网段或网关猜测删除')` |
| H0408 / R019/R020/R021 | K20:169 `_restore_add` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     if _find(read_routes(driver), entry['id']) is not None:         raise     return` |
| H0409 / R019/R020/R021 | K20:171 `_restore_add` / 抛出 | `捕获 RouterError / _find(read_routes(driver), entry['id']) is not None` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0410 / R019/R020/R021 | K20:174 `_restore_add` / 抛出 | `_find(read_routes(driver), entry['id']) is not None` → `RouterError('本次新增路由撤销后仍然存在')` |
| H0411 / R019/R020/R021 | K20:180 `mutate_route` / 抛出 | `action not in ('add', 'update', 'delete')` → `RouterError('不支持的静态路由操作')` |
| H0412 / R019/R020/R021 | K20:184 `mutate_route` / 抛出 | `action == 'add' and expected is not None` → `RouterError('新增路由不能指定旧记录')` |
| H0413 / R019/R020/R021 | K20:187 `mutate_route` / 抛出 | `action != 'add' and (not isinstance(expected, dict) or not all((key in expected for key in ENTRY_IDENTITY)))` → `RouterError('缺少完整旧路由快照，请刷新列表后重试')` |
| H0414 / R019/R020/R021 | K20:193 `mutate_route` / 抛出 | `action != 'add' / before is None or any((before[key] != expected[key] for key in ENTRY_IDENTITY))` → `RouterConflictError('静态路由在读取后发生变化，请刷新列表后重试')` |
| H0415 / R019/R020/R021 | K20:195 `mutate_route` / 抛出 | `action != 'add' / not before['editable']` → `RouterConflictError(before['reason'] or '这条路由暂不支持编辑')` |
| H0416 / R019/R020/R021 | K20:203 `mutate_route` / 抛出 | `action == 'add' / any((entry['snapshot'].get('tagname') == tagname for entry in previous))` → `RouterConflictError('新路由名称已被占用，请重试')` |
| H0417 / R019/R020/R021 | K20:217 `mutate_route` / 抛出 | `action == 'delete' / _find(read_routes(driver), before['id']) is not None` → `RouterError('路由删除后回读仍然存在')` |
| H0418 / R019/R020/R021 | K20:224 `mutate_route` / 抛出 | `entry is None or (action == 'update' and (not _snapshot_equal(entry, payload)))` → `RouterError('静态路由回读与本次提交不一致')` |
| H0419 / R019/R020/R021 | K20:227 `mutate_route` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         if action == 'add':             _restore_add(driver, previous, payload)         elif action == 'update':             _restore_update(driver, before, payload)         else:             _restore_delete(driver, before)     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；安全补偿未完成：{rollback_error}') from None     raise RouterError(f'{error}；已确认未保留本次变更') from None` |
| H0420 / R019/R020/R021 | K20:235 `mutate_route` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；安全补偿未完成：{rollback_error}') from None` |
| H0421 / R019/R020/R021 | K20:236 `mutate_route` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；安全补偿未完成：{rollback_error}')` |
| H0422 / R019/R020/R021 | K20:237 `mutate_route` / 抛出 | `捕获 RouterError` → `RouterError(f'{error}；已确认未保留本次变更')` |

### K21 [backend/routers/ipv6_dns.py](../../backend/routers/ipv6_dns.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0423 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:24 `_flag` / 校验/诊断降级 | `value in ('false', 'no', '0')` → `False` |
| H0424 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:27 `_flag` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'IPv6 DNS 下发的 {key} 状态无法识别，请检查路由器')` |
| H0425 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:41 `_dns_servers` / 抛出 | `not (isinstance(value, str)) / not (isinstance(value, list) and all((isinstance(item, str) for item in value)))` → `RouterError('IPv6 DNS 下发的服务器列表无法识别，请检查路由器')` |
| H0426 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:44 `_dns_servers` / 捕获后的失败/降级/补偿 | `for key in ('dns', 'dns-servers') / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('IPv6 DNS 下发包含无法识别的服务器地址，请检查路由器') from None` |
| H0427 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:45 `_dns_servers` / 抛出 | `for key in ('dns', 'dns-servers') / 捕获 (ValueError, TypeError)` → `RouterError('IPv6 DNS 下发包含无法识别的服务器地址，请检查路由器')` |
| H0428 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:54 `_scope` / 抛出 | `desired.get('dns_error') or not target or (not gateway)` → `RouterError('请先选择可用的 OxiDNS 和 Mihomo 网关，再检查 IPv6 DNS 下发')` |
| H0429 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:57 `_scope` / 抛出 | `not isinstance(scope, dict) or not scope.get('id') or (not scope.get('network')) or (scope.get('gateway') != gateway) or (scope.get('dns') != [target])` → `RouterError('尚未确认唯一的 DHCP DNS 目标网段，不能调整 IPv6 DNS 下发')` |
| H0430 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:61 `_scope` / 抛出 | `ipaddress.IPv4Address(gateway) not in network` → `ValueError` |
| H0431 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:62 `_scope` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('DHCP DNS 目标网段与 Mihomo 网关不匹配，请重新预览') from None` |
| H0432 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:63 `_scope` / 抛出 | `捕获 (ValueError, TypeError)` → `RouterError('DHCP DNS 目标网段与 Mihomo 网关不匹配，请重新预览')` |
| H0433 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:70 `_read` / 抛出 | `not isinstance(addresses, list) or any((not isinstance(row, dict) for row in addresses))` → `RouterError('无法读取路由器 LAN 地址，不能确定 IPv6 DNS 下发范围')` |
| H0434 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:76 `_read` / 捕获后的失败/降级/补偿 | `for row in addresses / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('路由器包含无法识别的 LAN 地址，不能确认 IPv6 DNS 下发范围') from None` |
| H0435 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:77 `_read` / 抛出 | `for row in addresses / 捕获 (ValueError, TypeError)` → `RouterError('路由器包含无法识别的 LAN 地址，不能确认 IPv6 DNS 下发范围')` |
| H0436 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:81 `_read` / 抛出 | `for row in addresses / _flag(row, 'invalid')` → `RouterError('Mihomo 所在的路由器 LAN 地址无效，请先检查地址配置')` |
| H0437 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:84 `_read` / 抛出 | `for row in addresses / not isinstance(interface, str) or not interface` → `RouterError('Mihomo 所在的路由器 LAN 地址缺少接口信息')` |
| H0438 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:87 `_read` / 抛出 | `not isinstance(interface, str) or not interface or interface == 'all'` → `RouterError('无法确认 Mihomo 所在的唯一 LAN，请先检查路由器地址配置')` |
| H0439 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:90 `_read` / 抛出 | `not isinstance(rows, list) or any((not isinstance(row, dict) for row in rows))` → `RouterError('无法读取 IPv6 DNS 下发设置，请检查路由器连接和权限')` |
| H0440 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:92 `_read` / 抛出 | `any((not isinstance(row.get('interface'), str) or not row['interface'] for row in rows))` → `RouterError('IPv6 通告条目缺少接口信息，不能确定 DNS 下发范围')` |
| H0441 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:96 `_read` / 抛出 | `any((not isinstance(row_id, str) or not row_id for row_id in ids)) or len(ids) != len(set(ids))` → `RouterError('IPv6 通告条目缺少唯一标识，无法安全调整 DNS 下发')` |
| H0442 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:99 `_read` / 抛出 | `for row in relevant / _flag(row, 'dynamic') or _flag(row, 'invalid')` → `RouterError('该 LAN 的 IPv6 通告是动态或无效条目，请先在路由器上处理')` |
| H0443 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:102 `_read` / 抛出 | `for row in relevant / row.get('advertise-dns') not in ('yes', 'self', 'no')` → `RouterError('IPv6 DNS 下发方式无法识别，请先在路由器上核对')` |
| H0444 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:120 `_constraints` / 抛出 | `len([row for row in rows if row['interface'] == interface]) > 1` → `RouterError('该 LAN 有多个 IPv6 通告条目，无法确认应修改哪一条，请先在路由器上整理')` |
| H0445 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:122 `_constraints` / 抛出 | `len([row for row in rows if row['interface'] == 'all']) > 1` → `RouterError('发现多个共享 IPv6 通告条目，无法确认 DNS 下发范围，请先在路由器上整理')` |
| H0446 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:125 `_constraints` / 抛出 | `any((row['interface'] == 'all' and row['advertise-dns'] != 'no' for row in _effective(rows, interface)))` → `RouterError('该 LAN 使用共享的 all 通告下发 DNS；直接关闭会影响其他接口，请先为该 LAN 单独设置 IPv6 通告')` |
| H0447 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:193 `inspect` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     plan.update(status='blocked', message=str(error), blockers=[str(error)], ops=[], check={})     for item in plan['items']:         item.update(status='blocked', detail=str(error))` |
| H0448 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:212 `apply` / 抛出 | `interface != op.get('interface') or _entries(driver, rows) != op.get('expected_rows')` → `RouterError('LAN 或 IPv6 通告设置在预览后发生变化，请重新预览')` |
| H0449 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:218 `apply` / 抛出 | `len(selected) != 1 or _flag(selected[0], 'disabled') or selected[0]['advertise-dns'] not in ('yes', 'self') or (op.get('values') != {'advertise-dns': 'no'}) or (driver._ownership_snapshot(ND, selected[0]) != op.get('expected'))` → `RouterError('IPv6 DNS 下发的目标或修改内容已变化，请重新预览')` |
| H0450 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:230 `verify` / 抛出 | `interface != check['interface'] or _entries(driver, rows) != check['entries']` → `RouterError('回读校验失败：IPv6 DNS 下发未按计划关闭，或 LAN 的其他通告设置发生变化')` |
| H0451 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K21:232 `verify` / 抛出 | `any((row['advertise-dns'] != 'no' for row in _effective(rows, interface)))` → `RouterError('回读校验失败：该 LAN 仍在下发 IPv6 DNS')` |

### K22 [backend/routers/ipv6_fakeip.py](../../backend/routers/ipv6_fakeip.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0452 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:46 `settings` / 抛出 | `not isinstance(document, dict)` → `ValueError('基础 YAML 必须是对象')` |
| H0453 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:49 `settings` / 抛出 | `not isinstance(dns, dict)` → `ValueError('dns 必须是对象')` |
| H0454 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:52 `settings` / 抛出 | `for (name, value) in [('ipv6', document.get('ipv6', True)), ('dns.ipv6', dns.get('ipv6', False))] / type(value) is not bool` → `ValueError(name + ' 必须填写 true 或 false')` |
| H0455 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:61 `settings` / 抛出 | `not network.subnet_of(ipaddress.IPv6Network('fc00::/7'))` → `ValueError('IPv6 Fake-IP 网段必须是 ULA 地址，不能覆盖公网或链路本地地址')` |
| H0456 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:66 `settings` / 抛出 | `clients and (not result['tun_enabled'])` → `ValueError('IPv6 Fake-IP 需要 Mihomo 开启 TUN；请先检查设备配置')` |
| H0457 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:67 `settings` / 捕获后的失败/降级/补偿 | `捕获 (yaml.YAMLError, ValueError, TypeError, AttributeError)` → `except (yaml.YAMLError, ValueError, TypeError, AttributeError) as error:     result['error'] = 'IPv6 Fake-IP 配置无法确认，请检查 YAML、fake-ip-range6 和 TUN 设置：' + str(error)` |
| H0458 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:85 `stable` / 捕获后的失败/降级/补偿 | `'address' in row / 捕获 (TypeError, ValueError)` → `except (TypeError, ValueError):     values['address'] = str(row['address'])` |
| H0459 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:116 `owned_entries` / 抛出 | `not isinstance(previous, dict) or previous.get('version') != VERSION or (not isinstance(previous.get('resources'), list)) or (len(previous['resources']) > 1) or (not isinstance(previous.get('spec'), dict))` → `RouterError('IPv6 Fake-IP 管理记录无效，保留现有路由，请核对归属')` |
| H0460 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:121 `owned_entries` / 抛出 | `for entry in previous['resources'] / not isinstance(entry, dict) or not isinstance(entry.get('id'), str) or (not isinstance(entry.get('config'), dict)) or (entry.get('path') != ROUTES)` → `RouterError('IPv6 Fake-IP 管理记录缺少完整对象信息，保留现有路由')` |
| H0461 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:124 `owned_entries` / 抛出 | `for entry in previous['resources'] / len(rows) > 1` → `RouterError('IPv6 Fake-IP 路由对象 ID 不唯一，停止发布')` |
| H0462 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:130 `owned_entries` / 抛出 | `for entry in previous['resources'] / str(_network(entry['config'].get('dst-address'))) != spec.get('cidr') or entry['config'].get('gateway') != spec.get('gateway')` → `RouterError('IPv6 Fake-IP 管理记录与记录中的路由配置不一致，保留现场，请核对')` |
| H0463 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:132 `owned_entries` / 抛出 | `for entry in previous['resources'] / stable(rows[0]) != entry['config']` → `RouterError('IPv6 Fake-IP 受管路由被外部修改，保留现场，请先核对')` |
| H0464 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:134 `owned_entries` / 抛出 | `for entry in previous['resources'] / yes(rows[0].get('dynamic')) or yes(rows[0].get('connect'))` → `RouterError('IPv6 Fake-IP 管理记录指向动态路由，停止发布')` |
| H0465 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:180 `inspect` / 阻止/只读原因 | `setting['error']` → `plan['blockers'].append(setting['error'])` |
| H0466 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:185 `inspect` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     plan['blockers'].append(str(error))     plan['status'] = 'unknown'     return plan` |
| H0467 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:186 `inspect` / 阻止/只读原因 | `捕获 RouterError` → `plan['blockers'].append(str(error))` |
| H0468 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:196 `inspect` / 阻止/只读原因 | `setting['enabled'] / not agent or agent.get('service_type') != 'mihomo' or agent.get('enabled') is False` → `plan['blockers'].append('请关联本配置空间中启用的 Mihomo 代理网关')` |
| H0469 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:198 `inspect` / 阻止/只读原因 | `not (not agent or agent.get('service_type') != 'mihomo' or agent.get('enabled') is False) / host.get('error')` → `plan['blockers'].append('无法读取 Mihomo 的 IPv6 地址：' + str(host['error']))` |
| H0470 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:204 `inspect` / 阻止/只读原因 | `setting['enabled'] / not gateway` → `plan['blockers'].append('未确认 Mihomo 唯一、稳定的同网段 IPv6 下一跳，请先检查设备 IPv6 网络')` |
| H0471 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:212 `inspect` / 阻止/只读原因 | `setting['enabled'] / any(((network := _network(row.get('address'))) and network.version == 6 and network.overlaps(target) for row in live['addresses']))` → `plan['blockers'].append('IPv6 Fake-IP 与 ROS 的内网实际地址重叠，请先调整网段')` |
| H0472 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:219 `inspect` / 阻止/只读原因 | `len(candidates) == 1 and gateway and _correct(candidates[0], spec) / not borrowed['id'] or yes(candidates[0].get('dynamic')) or yes(candidates[0].get('connect'))` → `plan['blockers'].append('现有 IPv6 Fake-IP 路由不能确认为普通静态路由，请先核对')` |
| H0473 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:221 `inspect` / 阻止/只读原因 | `candidates / not (len(candidates) == 1 and gateway and _correct(candidates[0], spec))` → `plan['blockers'].append('已有 IPv6 Fake-IP 路由错误、停用或与其他路由冲突；不会自动覆盖')` |
| H0474 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:224 `inspect` / 阻止/只读原因 | `setting['enabled'] / owned and borrowed` → `plan['blockers'].append('受管路由与其他 IPv6 Fake-IP 路由重复，请先核对')` |
| H0475 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:267 `_rollback` / 捕获后的失败/降级/补偿 | `for change in changes / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0476 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:268 `_rollback` / 阻止/只读原因 | `for change in changes / 捕获 RouterError` → `failures.append(str(error))` |
| H0477 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:270 `_rollback` / 抛出 | `failures` → `RouterRollbackError('IPv6 Fake-IP 路由回滚未完成：' + '；'.join(failures))` |
| H0478 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:287 `apply` / 抛出 | `evidence(live) != op.get('expected')` → `RouterError('IPv6 路由或内网地址在预览后变化，请重新预览')` |
| H0479 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:291 `apply` / 抛出 | `owned != op.get('owned')` → `RouterError('IPv6 Fake-IP 对象归属已变化，请重新预览')` |
| H0480 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:298 `apply` / 抛出 | `for entry in owned / stable(row) != entry['config']` → `RouterError('IPv6 Fake-IP 受管路由在操作期间变化，保留现场')` |
| H0481 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:306 `apply` / 抛出 | `op.get('spec') / _target_rows({'routes': driver._list(ROUTES)}, op['spec']['cidr'])` → `RouterError('IPv6 Fake-IP 目标已出现其他路由，停止添加')` |
| H0482 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:311 `apply` / 抛出 | `op.get('spec') / not _correct(row, op['spec'])` → `RouterError('新建 IPv6 Fake-IP 路由尚未生效，已停止发布')` |
| H0483 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:315 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         remap = _rollback(driver, list(reversed(undo)))     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     driver.ipv6_fakeip_manifest = _remapped(previous, remap, driver)     raise` |
| H0484 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:318 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0485 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:319 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0486 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:321 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0487 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:338 `verify` / 抛出 | `not spec / manifest or owned or any((str(row.get('.id')) in check.get('removed', []) for row in live['routes']))` → `RouterError('回读校验失败：IPv6 Fake-IP 受管路由未按计划撤回')` |
| H0488 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:342 `verify` / 抛出 | `len(rows) != 1 or not _correct(rows[0], spec)` → `RouterError('回读校验失败：IPv6 Fake-IP 路由未生效或存在冲突')` |
| H0489 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:346 `verify` / 抛出 | `borrowed / str(rows[0].get('.id')) != borrowed['id'] or stable(rows[0]) != borrowed['config']` → `RouterError('回读校验失败：沿用的 IPv6 Fake-IP 路由已变化')` |
| H0490 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K22:348 `verify` / 抛出 | `not (borrowed) / len(owned) != 1 or str(rows[0].get('.id')) != owned[0]['id'] or (manifest or {}).get('spec') != spec` → `RouterError('回读校验失败：IPv6 Fake-IP 路由缺少匹配的 ConfigFlow 管理记录')` |

### K23 [backend/routers/ipv6_pd_guard.py](../../backend/routers/ipv6_pd_guard.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0491 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:120 `normalize_settings` / 抛出 | `not isinstance(value, dict) or set(value) - set(DEFAULTS) or type(value.get('enabled', False)) is not bool` → `SettingsError('IPv6 前缀变化保护的开关必须为布尔值')` |
| H0492 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:123 `normalize_settings` / 抛出 | `settings['enabled'] and router_type != 'routeros'` → `SettingsError('IPv6 前缀变化保护仅支持 RouterOS')` |
| H0493 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:184 `_rows` / 抛出 | `any((not isinstance(row.get('.id'), str) or not row['.id'] for row in rows))` → `RouterError(f'RouterOS {path} 的对象缺少 ID，不能确认前缀保护的安装状态')` |
| H0494 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:202 `derive` / 抛出 | `not clients` → `RouterError('没有启用并请求前缀（request 含 prefix）的 IPv6 DHCP 客户端，前缀保护不适用')` |
| H0495 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:205 `derive` / 抛出 | `len(clients) > 1` → `RouterError(f'有多个启用的 IPv6 DHCP 客户端在请求前缀（{names}），无法确定监控哪一个')` |
| H0496 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:209 `derive` / 抛出 | `not pool` → `RouterError(f"IPv6 DHCP 客户端（{client.get('interface')}）没有设置 pool-name，无法确定前缀来源")` |
| H0497 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:211 `derive` / 抛出 | `not NAME.fullmatch(pool)` → `RouterError(f'地址池名称「{pool}」含有脚本无法安全使用的字符')` |
| H0498 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:215 `derive` / 抛出 | `not rows` → `RouterError(f'地址池 {pool} 没有分配给任何 LAN（/ipv6 address 中没有启用的 from-pool={pool}）')` |
| H0499 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:217 `derive` / 抛出 | `'' in interfaces` → `RouterError(f'地址池 {pool} 的 LAN 地址缺少接口信息')` |
| H0500 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:226 `derive` / 抛出 | `not (len(interfaces) == 1) / mihomo not in interfaces` → `RouterError(f"地址池 {pool} 分给了多个 LAN（{'、'.join(interfaces)}），且 Mihomo 网关不在其中任何一个，无法确定监控哪一个")` |
| H0501 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:232 `derive` / 抛出 | `not NAME.fullmatch(lan)` → `RouterError(f'接口名称「{lan}」含有脚本无法安全使用的字符')` |
| H0502 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:235 `derive` / 抛出 | `len(selected) != 1` → `RouterError(f'{lan} 上有多条来自地址池 {pool} 的地址，脚本无法确定当前前缀')` |
| H0503 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:239 `derive` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     raise RouterError(f'{lan} 的 IPv6 地址无法识别') from None` |
| H0504 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:240 `derive` / 抛出 | `捕获 ValueError` → `RouterError(f'{lan} 的 IPv6 地址无法识别')` |
| H0505 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:242 `derive` / 抛出 | `length != 64` → `RouterError(f'{lan} 的 IPv6 地址前缀长度为 /{length}，前缀保护只支持 /64')` |
| H0506 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:244 `derive` / 抛出 | `not yes(address.get('advertise', 'false'))` → `RouterError(f'{lan} 的 IPv6 地址没有开启 advertise，客户端不通过 RA 获得前缀，前缀保护没有作用')` |
| H0507 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:259 `_script_state` / 错误/未知结果 | `not _is_watch(source)` → `('conflict', f'已有同名脚本 {WATCH}，但内容不是前缀保护脚本，不能覆盖；请改名或删除后重新检查')` |
| H0508 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:280 `_scheduler_state` / 错误/未知结果 | `WATCH not in str(row.get('on-event', ''))` → `('conflict', f'已有同名定时任务 {POLL}，但不是运行 {WATCH}，不能覆盖；请改名或删除后重新检查')` |
| H0509 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:305 `_hook_state` / 错误/未知结果 | `依赖/文件/事务故障；按函数前置条件` → `('conflict', f"dhcp-client（{client.get('interface')}）的 script 已有其他内容，不能覆盖。可把下面这行加到原内容末尾后重新检查：{HOOK}")` |
| H0510 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:315 `_owned` / 抛出 | `not isinstance(previous, dict) or previous.get('version') != VERSION or (not isinstance(previous.get('resources'), list))` → `RouterError('IPv6 前缀变化保护的发布记录无效，保留现场，请核对')` |
| H0511 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:320 `_owned` / 抛出 | `for entry in previous['resources'] / not isinstance(entry, dict) or entry.get('role') not in ('script', 'scheduler', 'hook') or (not isinstance(entry.get('id'), str))` → `RouterError('IPv6 前缀变化保护的发布记录缺少对象信息，保留现场，请核对')` |
| H0512 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:378 `_withdraw_changes` / 阻止/只读原因 | `scheduler is not None / WATCH not in str(scheduler.get('on-event', ''))` → `blockers.append(f'定时任务 {POLL} 已被改成其他内容，不能确认可以删除，请先核对')` |
| H0513 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:383 `_withdraw_changes` / 阻止/只读原因 | `script is not None / not _is_watch(script.get('source'))` → `blockers.append(f'脚本 {WATCH} 已被改成其他内容，不能确认可以删除，请先核对')` |
| H0514 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:416 `inspect` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     plan.update(status='blocked', message=str(error), blockers=[str(error)], ops=[], check={})     for item in plan['items']:         if item['status'] not in ('borrowed', 'synced', 'conflict'):             item['status'] = 'blocked'             item['detail'] = (item['detail'].replace('发布时', '') + '；本次跳过，不写入').lstrip('；，')` |
| H0515 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:456 `_inspect_enabled` / 抛出 | `conflicts` → `RouterError('；'.join(conflicts))` |
| H0516 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:510 `_inspect_withdraw` / 抛出 | `blockers` → `RouterError('；'.join(blockers))` |
| H0517 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:543 `_rollback` / 捕获后的失败/降级/补偿 | `for change in changes / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0518 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:544 `_rollback` / 阻止/只读原因 | `for change in changes / 捕获 RouterError` → `failures.append(str(error))` |
| H0519 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:546 `_rollback` / 抛出 | `failures` → `RouterRollbackError('IPv6 前缀变化保护回滚未完成：' + '；'.join(failures))` |
| H0520 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:563 `apply` / 抛出 | `fresh['blockers'] or fresh_op is None or fresh_op['changes'] != op.get('changes') or (fresh_op.get('spec') != op.get('spec'))` → `RouterError('IPv6 前缀变化保护的路由器配置在预览后发生变化，请重新预览')` |
| H0521 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:591 `apply` / 抛出 | `op.get('enabled') and roles / not all((ids[role] for role in roles))` → `RouterError('写入后没有找到前缀保护的脚本或定时任务')` |
| H0522 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:597 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         remap = _rollback(driver, list(reversed(undo)))     except RouterRollbackError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     driver.ipv6_pd_guard_manifest = _remapped(previous, remap)     raise` |
| H0523 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:600 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterRollbackError` → `except RouterRollbackError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0524 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:601 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterRollbackError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0525 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:603 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0526 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:622 `verify` / 抛出 | `check.get('withdraw') / manifest or present & {tuple(item) for item in check.get('removed') or []}` → `RouterError('回读校验失败：IPv6 前缀变化保护未按计划撤回')` |
| H0527 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:625 `verify` / 抛出 | `derive(live, check.get('gateway') or '', driver)['spec'] != check['spec']` → `RouterError('回读校验失败：IPv6 前缀变化保护监控的 LAN 或 dhcp-client 已变化')` |
| H0528 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:630 `verify` / 抛出 | `_script_state(script, check['spec'])[0] != 'ok' or _scheduler_state(scheduler)[0] != 'ok' or _hook_state(client)[0] not in ('ok', 'shared')` → `RouterError('回读校验失败：IPv6 前缀变化保护的脚本、定时任务或钩子与模板不一致')` |
| H0529 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K23:635 `verify` / 抛出 | `sorted(owned) != sorted(check.get('owned') or []) or any((row['.id'] != current[role]['.id'] for (role, row) in owned.items()))` → `RouterError('回读校验失败：IPv6 前缀变化保护的管理记录与路由器对象不一致')` |

### K24 [backend/routers/ipv6_policy.py](../../backend/routers/ipv6_policy.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0530 / R011/R012/R018（IPv6与包路径诊断） | K24:40 `_network` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     return None` |
| H0531 / R011/R012/R018（IPv6与包路径诊断） | K24:41 `_network` / 校验/诊断降级 | `捕获 (ValueError, TypeError)` → `None` |
| H0532 / R011/R012/R018（IPv6与包路径诊断） | K24:62 `_address_match` / 校验/诊断降级 | `source is None` → `None` |
| H0533 / R011/R012/R018（IPv6与包路径诊断） | K24:79 `_address_match` / 捕获后的失败/降级/补偿 | `'-' in expected / 捕获 ValueError` → `except ValueError:     result = None` |
| H0534 / R011/R012/R018（IPv6与包路径诊断） | K24:89 `_number_match` / 校验/诊断降级 | `actual is None` → `None` |
| H0535 / R011/R012/R018（IPv6与包路径诊断） | K24:100 `_number_match` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     return None` |
| H0536 / R011/R012/R018（IPv6与包路径诊断） | K24:101 `_number_match` / 校验/诊断降级 | `捕获 (ValueError, TypeError)` → `None` |
| H0537 / R011/R012/R018（IPv6与包路径诊断） | K24:106 `_string_match` / 校验/诊断降级 | `actual is None` → `None` |
| H0538 / R011/R012/R018（IPv6与包路径诊断） | K24:116 `_interface_member` / 校验/诊断降级 | `interface is None` → `None` |
| H0539 / R011/R012/R018（IPv6与包路径诊断） | K24:118 `_interface_member` / 校验/诊断降级 | `not interface` → `False` |
| H0540 / R011/R012/R018（IPv6与包路径诊断） | K24:122 `_interface_member` / 校验/诊断降级 | `name == 'none'` → `False` |
| H0541 / R011/R012/R018（IPv6与包路径诊断） | K24:125 `_interface_member` / 校验/诊断降级 | `name in ('dynamic', 'static') / 'interfaces' in snapshot.get('_errors', ()) or 'interfaces' not in snapshot` → `None` |
| H0542 / R011/R012/R018（IPv6与包路径诊断） | K24:128 `_interface_member` / 校验/诊断降级 | `name in ('dynamic', 'static') / item is None` → `None` |
| H0543 / R011/R012/R018（IPv6与包路径诊断） | K24:131 `_interface_member` / 校验/诊断降级 | `name in visited or any((k in snapshot.get('_errors', ()) or k not in snapshot for k in ('interface_lists', 'interface_members')))` → `None` |
| H0544 / R011/R012/R018（IPv6与包路径诊断） | K24:134 `_interface_member` / 校验/诊断降级 | `len(definitions) != 1` → `None` |
| H0545 / R011/R012/R018（IPv6与包路径诊断） | K24:151 `_list_match` / 校验/诊断降级 | `key not in snapshot or key in snapshot.get('_errors', ()) or actual is None` → `None` |
| H0546 / R011/R012/R018（IPv6与包路径诊断） | K24:196 `_rule_match` / 捕获后的失败/降级/补偿 | `key == 'hop-limit' and packet.get('hop-limit') is not None / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     result = None` |
| H0547 / R011/R012/R018（IPv6与包路径诊断） | K24:358 `_fake_gateway_matches` / 校验/诊断降级 | `not candidates` → `False` |

### K25 [backend/routers/ipv6_readiness.py](../../backend/routers/ipv6_readiness.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0548 / R011/R012/R018（IPv6与包路径诊断） | K25:40 `_network` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     return None` |
| H0549 / R011/R012/R018（IPv6与包路径诊断） | K25:41 `_network` / 校验/诊断降级 | `捕获 (ValueError, TypeError)` → `None` |
| H0550 / R011/R012/R018（IPv6与包路径诊断） | K25:47 `_lan` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return None` |
| H0551 / R011/R012/R018（IPv6与包路径诊断） | K25:48 `_lan` / 校验/诊断降级 | `捕获 ValueError` → `None` |
| H0552 / R011/R012/R018（IPv6与包路径诊断） | K25:60 `_gateway` / 校验/诊断降级 | `not lan` → `None` |
| H0553 / R011/R012/R018（IPv6与包路径诊断） | K25:71 `_gateway` / 捕获后的失败/降级/补偿 | `for row in host.get('addresses', []) / 捕获 (ValueError, KeyError, TypeError)` → `except (ValueError, KeyError, TypeError):     continue` |
| H0554 / R011/R012/R018（IPv6与包路径诊断） | K25:126 `_SnapshotReads._list` / 抛出 | `key is None or key in self.errors or key not in self.snapshot` → `RouterError('未读取 ' + path)` |
| H0555 / R011/R012/R018（IPv6与包路径诊断） | K25:141 `_forward_hint` / 校验/诊断降级 | `not lan or configuration.get('ipv6') is not True` → `None` |
| H0556 / R011/R012/R018（IPv6与包路径诊断） | K25:145 `_forward_hint` / 校验/诊断降级 | `not warning` → `None` |
| H0557 / R011/R012/R018（IPv6与包路径诊断） | K25:157 `inspect` / 捕获后的失败/降级/补偿 | `for (key, path) in TABLES.items() / 捕获 RouterError` → `except RouterError:     errors[key] = '读取失败，请检查 RouterOS IPv6 支持与账号权限'` |
| H0558 / R011/R012/R018（IPv6与包路径诊断） | K25:170 `inspect` / 捕获后的失败/降级/补偿 | `for prefix in ('', 'ipv4_') / 捕获 RouterError` → `except RouterError:     errors[key] = '引用的地址列表未完整读取，无法确认规则匹配'` |
| H0559 / R011/R012/R018（IPv6与包路径诊断） | K25:174 `inspect` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     errors['dns'] = '无法读取 ROS DNS'` |
| H0560 / R011/R012/R018（IPv6与包路径诊断） | K25:260 `add_missing_route` / 抛出 | `before` → `RouterError('IPv6 路由已变化，请重新检查后再补齐')` |
| H0561 / R011/R012/R018（IPv6与包路径诊断） | K25:266 `add_missing_route` / 抛出 | `len(rows) != 1 or not _owned(rows[0], values) or (not yes(rows[0].get('active')))` → `RouterError('新增 IPv6 路由未能通过读回验证')` |
| H0562 / R011/R012/R018（IPv6与包路径诊断） | K25:268 `add_missing_route` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception as error:     try:         rows = [r for r in driver._list(ROUTES) if r.get('comment') == values['comment']]         if len(rows) == 1 and _owned(rows[0], values):             undo_route(driver, {'id': rows[0]['.id'], 'values': values})     except RouterError:         raise RouterError('IPv6 路由添加结果或撤销结果未能确认，请刷新路由器检查，勿重复添加') from None     raise RouterError(str(error)) from None` |
| H0563 / R011/R012/R018（IPv6与包路径诊断） | K25:275 `add_missing_route` / 捕获后的失败/降级/补偿 | `捕获 Exception / 捕获 RouterError` → `except RouterError:     raise RouterError('IPv6 路由添加结果或撤销结果未能确认，请刷新路由器检查，勿重复添加') from None` |
| H0564 / R011/R012/R018（IPv6与包路径诊断） | K25:276 `add_missing_route` / 抛出 | `捕获 Exception / 捕获 RouterError` → `RouterError('IPv6 路由添加结果或撤销结果未能确认，请刷新路由器检查，勿重复添加')` |
| H0565 / R011/R012/R018（IPv6与包路径诊断） | K25:277 `add_missing_route` / 抛出 | `捕获 Exception` → `RouterError(str(error))` |
| H0566 / R011/R012/R018（IPv6与包路径诊断） | K25:290 `undo_route` / 抛出 | `not _owned(row, receipt['values'])` → `RouterError('新增路由已被其他操作修改，未自动删除，请人工核对')` |

### K26 [backend/routers/ipv6_routes.py](../../backend/routers/ipv6_routes.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0567 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:42 `settings` / 抛出 | `not isinstance(document, dict)` → `ValueError('基础 YAML 必须是对象')` |
| H0568 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:45 `settings` / 抛出 | `type(value) is not bool` → `ValueError('ipv6 必须填写 true 或 false')` |
| H0569 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:48 `settings` / 捕获后的失败/降级/补偿 | `捕获 (yaml.YAMLError, ValueError, TypeError, AttributeError)` → `except (yaml.YAMLError, ValueError, TypeError, AttributeError) as error:     result['error'] = 'Mihomo 的 IPv6 设置无法确认，请检查基础 YAML：' + str(error)` |
| H0570 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:56 `relevant` / 校验/诊断降级 | `router.get('type') != 'routeros'` → `False` |
| H0571 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:79 `owned_entries` / 抛出 | `not isinstance(previous, dict) or previous.get('version') != VERSION or (not isinstance(previous.get('resources'), list)) or (not isinstance(previous.get('gateway'), str))` → `RouterError('IPv6 规则集路由的管理记录无效，保留现有路由，请核对归属')` |
| H0572 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:87 `owned_entries` / 抛出 | `for entry in previous['resources'] / not isinstance(entry, dict) or not isinstance(entry.get('id'), str) or entry.get('path') != ROUTES or (not isinstance(entry.get('config'), dict)) or (_key(entry['config'].get('dst-address')) is None)` → `RouterError('IPv6 规则集路由的管理记录缺少完整对象信息，保留现有路由')` |
| H0573 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:89 `owned_entries` / 抛出 | `for entry in previous['resources'] / entry['id'] in seen or len(rows.get(entry['id'], [])) > 1` → `RouterError('IPv6 规则集路由对象 ID 不唯一，保留现有路由')` |
| H0574 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:95 `owned_entries` / 抛出 | `for entry in previous['resources'] / stable(row) != entry['config']` → `RouterError(f"IPv6 规则集受管路由 {entry['config'].get('dst-address')} 被外部修改，保留现场；请改回原值或删除这条路由后重新预览")` |
| H0575 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:98 `owned_entries` / 抛出 | `for entry in previous['resources'] / yes(row.get('dynamic')) or yes(row.get('connect'))` → `RouterError('IPv6 规则集路由的管理记录指向动态路由，保留现有路由')` |
| H0576 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:146 `inspect` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return skipped(str(error))` |
| H0577 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:261 `_rebuild` / 校验/诊断降级 | `not previous` → `None` |
| H0578 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:264 `_rebuild` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return copy.deepcopy(previous)` |
| H0579 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:285 `_rollback` / 抛出 | `problems` → `RouterRollbackError('IPv6 规则集路由回滚未完成：' + problems[0])` |
| H0580 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:312 `apply` / 抛出 | `evidence(_scoped(live, scope, {entry['id'] for entry in op['owned']})) != op['expected']` → `RouterError('IPv6 路由或内网地址在预览后变化，请重新预览')` |
| H0581 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:315 `apply` / 抛出 | `owned_entries(previous, live) != op['owned']` → `RouterError('IPv6 规则集路由的归属已变化，请重新预览')` |
| H0582 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:327 `apply` / 抛出 | `str(row.get('.id')) in added / not _correct(row, {'cidr': _key(row.get('dst-address')), 'gateway': op['gateway']})` → `RouterError(f"新建 IPv6 路由 {row.get('dst-address')} 尚未生效，已停止发布")` |
| H0583 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:330 `apply` / 抛出 | `len(resources) != len(op['retained']) + len(op['adds'])` → `RouterError('RouterOS 回读校验失败：新建的 IPv6 路由数量与计划不一致')` |
| H0584 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:333 `apply` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, RuntimeError)` → `except (RouterError, RuntimeError) as error:     try:         _rollback(driver, list(reversed(undos)))     except RouterError as rollback_error:         driver.ipv6_routes_manifest = _rebuild(driver, previous)         raise RouterRollbackError(f'{error}；{rollback_error}') from error     driver.ipv6_routes_manifest = _rebuild(driver, previous)     raise` |
| H0585 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:336 `apply` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, RuntimeError) / 捕获 RouterError` → `except RouterError as rollback_error:     driver.ipv6_routes_manifest = _rebuild(driver, previous)     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0586 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:338 `apply` / 抛出 | `捕获 (RouterError, RuntimeError) / 捕获 RouterError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0587 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:340 `apply` / 抛出 | `捕获 (RouterError, RuntimeError)` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0588 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:357 `verify` / 抛出 | `manifest and len(owned) != len(manifest['resources'])` → `RouterError('回读校验失败：IPv6 规则集受管路由缺失')` |
| H0589 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:365 `verify` / 抛出 | `for cidr in check['active'] / len(candidates) != 1 or not _correct(candidates[0], {'cidr': cidr, 'gateway': check['gateway']})` → `RouterError(f"回读校验失败：IPv6 路由 {cidr} 没有唯一生效的受管路由指向 {check['gateway']}")` |
| H0590 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:368 `verify` / 抛出 | `for item in check['borrowed'] / item['id'] not in rows or stable(rows[item['id']]) != item['config']` → `RouterError('回读校验失败：沿用的 IPv6 路由已变化')` |
| H0591 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K26:370 `verify` / 抛出 | `set(check['removed']) & set(rows)` → `RouterError('回读校验失败：应撤回的 IPv6 规则集路由仍然存在')` |

### K27 [backend/routers/lan_clients.py](../../backend/routers/lan_clients.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0592 / R004/R005/R006 | K27:68 `ipv4` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return ''` |
| H0593 / R004/R005/R006 | K27:79 `normalize_mac` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return ''` |

### K28 [backend/routers/pending.py](../../backend/routers/pending.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0594 / R001 | K28:149 `_read_all` / 捕获后的失败/降级/补偿 | `not (not future.done() or future.cancelled()) / 捕获 RouterError` → `except RouterError as error:     result = _unknown(str(error) or '读取路由器失败')` |
| H0595 / R001 | K28:151 `_read_all` / 捕获后的失败/降级/补偿 | `not (not future.done() or future.cancelled()) / 捕获 Exception` → `except Exception as error:     logger.exception('Upstream router status failed for router %s', router.get('id'))     result = _unknown(f'读取失败：{error.__class__.__name__}')` |
| H0596 / R001 | K28:234 `PendingCache.read` / 捕获后的失败/降级/补偿 | `捕获 BaseException` → `except BaseException as failure:     with self._lock:         entry.pending = None     pending.set_exception(failure)     raise` |
| H0597 / R001 | K28:238 `PendingCache.read` / 抛出 | `捕获 BaseException` → `重新抛出当前错误；同时核对已写入/补偿结果` |

### K29 [backend/routers/plan.py](../../backend/routers/plan.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0598 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:63 `router_ip` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return None` |
| H0599 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:64 `router_ip` / 校验/诊断降级 | `捕获 ValueError` → `None` |
| H0600 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:81 `return_allocation` / 校验/诊断降级 | `not isinstance(allocation, dict) or type(allocation.get('version')) is not int or allocation['version'] != RETURN_ALLOCATION_VERSION or (type(allocation.get('reused')) is not bool) or (type(allocation.get('table')) is not int) or (not 1 <= allocation['table'] <= 252) or (type(allocation.get('priority')) is not int) or (not 1 <= allocation['priority'] <= 32765)` → `None` |
| H0601 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:84 `return_allocation` / 校验/诊断降级 | `not isinstance(mark, str) or not re.fullmatch('0[xX][0-9a-fA-F]{1,8}', mark)` → `None` |
| H0602 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:87 `return_allocation` / 校验/诊断降级 | `number == 0 or number & number - 1` → `None` |
| H0603 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:193 `_retired_policy_ops` / 阻止/只读原因 | `lost` → `blockers.append(f"{'、'.join(sorted(lost))} 存在路由冲突，改回静态路由前请先处理，否则删除端口分流后这些网段将无法转发")` |
| H0604 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:235 `_policy_op` / 校验/诊断降级 | `policy_route_ok(rule, gateway) and set(rule.dst) == set(dst)` → `None` |
| H0605 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:250 `_grouped_policy_op` / 校验/诊断降级 | `rule is not None and policy_route_ok(rule, gateway) and (not planner.needs_rewrite(key, role, keep_foreign))` → `None` |
| H0606 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:512 `build_plan` / 阻止/只读原因 | `not gateway` → `blockers.append('未关联 mihomo Agent，无法确定代理路由的下一跳')` |
| H0607 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:516 `build_plan` / 捕获后的失败/降级/补偿 | `not (not gateway) / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     blockers.append('代理路由的下一跳不是有效 IPv4 地址，请检查关联 Agent 的主机地址')` |
| H0608 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:517 `build_plan` / 阻止/只读原因 | `not (not gateway) / 捕获 (ValueError, TypeError)` → `blockers.append('代理路由的下一跳不是有效 IPv4 地址，请检查关联 Agent 的主机地址')` |
| H0609 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:521 `build_plan` / 阻止/只读原因 | `manage_return / not upstream` → `blockers.append('路由器地址不是 IPv4，无法确定回流规则的下一跳')` |
| H0610 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:523 `build_plan` / 阻止/只读原因 | `manage_return / return_routing.get('status') != 'ok'` → `blockers.append(f"无法读取 mihomo Agent 的回流规则状态：{return_routing.get('message') or '未知错误'}")` |
| H0611 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:525 `build_plan` / 阻止/只读原因 | `manage_return / not (return_routing.get('status') != 'ok')` → `blockers.extend(return_safety_blockers(return_routing))` |
| H0612 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:555 `build_plan` / 阻止/只读原因 | `ros_policy is not None` → `blockers.extend(ros_policy['blockers'])` |
| H0613 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:567 `build_plan` / 阻止/只读原因 | `ros_policy is not None / any((replaced(net) for net in _networks(skipped)))` → `blockers.append('替代静态路由存在冲突，不能撤销仍在生效的来源策略，请先处理冲突')` |
| H0614 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:580 `build_plan` / 阻止/只读原因 | `manage_dns` → `blockers.extend(dns['blockers'])` |
| H0615 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:582 `build_plan` / 阻止/只读原因 | `manage_dns / skipped` → `blockers.append('存在代理路由冲突，请先处理冲突再下发 DHCP DNS，避免客户端获取 DNS 后无法访问代理网段')` |
| H0616 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:594 `build_plan` / 阻止/只读原因 | `依赖/文件/事务故障；按函数前置条件` → `blockers.extend((f"{item['cidr']}：无法确认是否需要 NAT 免转换。{item['detail']}" for item in nat_items if item['status'] == 'unknown'))` |
| H0617 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:598 `build_plan` / 阻止/只读原因 | `依赖/文件/事务故障；按函数前置条件` → `blockers.extend(listed.get('blockers') or [])` |
| H0618 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:602 `build_plan` / 阻止/只读原因 | `router.get('type') == 'ikuai'` → `blockers.extend(oversized_requests(route_ops + nat_ops))` |
| H0619 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:605 `build_plan` / 阻止/只读原因 | `router.get('type') == 'routeros' and state.nat_context is not None` → `blockers.extend(planned_bypass_conflicts(nat_ops, state, gateway))` |
| H0620 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:622 `build_plan` / 阻止/只读原因 | `nat_ops / interface is None` → `blockers.append('无法安全准备替代路径的 LAN NAT 保护：' + reason)` |
| H0621 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:628 `build_plan` / 阻止/只读原因 | `for preparation in nat_ops / preparation['kind'] == 'nat_list_sync'` → `blockers.extend([problem] if problem else [])` |
| H0622 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:659 `build_plan` / 阻止/只读原因 | `guard is not None` → `blockers.extend(guard['blockers'])` |
| H0623 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:663 `build_plan` / 阻止/只读原因 | `guard is not None / skipped and guard['spec']` → `blockers.append('存在代理路由冲突，请先处理冲突再启用强制 DNS，避免被劫持的设备拿到 fake-ip 后无法访问')` |
| H0624 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:684 `build_plan` / 阻止/只读原因 | `ipv6 is not None` → `blockers.extend(ipv6['blockers'])` |
| H0625 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K29:712 `build_plan` / 阻止/只读原因 | `ipv6_dns is not None / ipv6_dns['ops'] and (not manage_dns or dns['status'] not in {'synced', 'pending'})` → `blockers.append('IPv6 DNS 下发缺少已确认的 OxiDNS DHCP 目标，请重新预览')` |

### K30 [backend/routers/policy_workspace.py](../../backend/routers/policy_workspace.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0626 / R014/R015/R016 | K30:41 `save_workspace` / 抛出 | `not router or router.get('type') != 'routeros' or body.get('revision') != draft_revision(config, router) or ('policies' in body and body.get('names_revision') != policy_names_revision(config))` → `RouterConflictError('配置已在其他页面更新，请重新载入后再保存。')` |

### K31 [backend/routers/release.py](../../backend/routers/release.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0627 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:46 `_check_return_allocation` / 抛出 | `blockers` → `RouterError('；'.join(blockers))` |
| H0628 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:49 `_check_return_allocation` / 抛出 | `any((allocation[key] != spec.get(key) for key in RETURN_ALLOCATION_FIELDS))` → `RouterError('回流资源分配在预览后发生变化，请重新预览后发布')` |
| H0629 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:51 `_check_return_allocation` / 抛出 | `(spec.get('safety_version') or 0) > return_protocol(status)` → `RouterError('mihomo Agent 不支持按来源标记的回流规则，请重新预览后发布')` |
| H0630 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:60 `_apply` / 抛出 | `kind in {'return_routing_apply', 'return_routing_remove'} and agent is None` → `RouterError('未启用 Agent 回流配置，无法执行回流操作')` |
| H0631 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:65 `_apply` / 抛出 | `kind == 'return_routing_apply' / type(spec.get('safety_version')) is not int or spec['safety_version'] not in RETURN_PROTOCOLS or spec.get('adopt_legacy')` → `RouterError('回流操作缺少安全握手或包含手工规则接管，请重新预览')` |
| H0632 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:76 `_apply` / 抛出 | `kind == 'return_routing_remove' / blockers` → `RouterError('；'.join(blockers))` |
| H0633 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:102 `verify_nat` / 抛出 | `uncovered` → `RouterError(f"回读校验失败：{', '.join(uncovered)} 没有被受管 NAT 免转换规则覆盖")` |
| H0634 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:112 `verify_nat` / 抛出 | `for check in checks.get('nat_not_required') or [] / assessment['status'] != 'not_required'` → `RouterError(f"回读校验失败：{check['cidr']} 无法继续确认无需 NAT 免转换。{assessment['detail']}")` |
| H0635 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:121 `_return_rule_matches.number` / 抛出 | `not isinstance(value, str) or not value` → `ValueError('invalid policy rule number')` |
| H0636 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:129 `_return_rule_matches` / 捕获后的失败/降级/补偿 | `捕获 (TypeError, ValueError, AttributeError)` → `except (TypeError, ValueError, AttributeError):     return False` |
| H0637 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:130 `_return_rule_matches` / 校验/诊断降级 | `捕获 (TypeError, ValueError, AttributeError)` → `False` |
| H0638 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:163 `_verify` / 抛出 | `stage == 'return_cleanup' / managed.get('active') is True or managed.get('table_present') is True` → `ReleaseError('回读校验失败：Agent 上仍有受管的回流规则')` |
| H0639 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:171 `_verify` / 抛出 | `stage == 'return_routing' / not managed.get('table_present')` → `ReleaseError('Agent 上没有找到 configflow_return 表')` |
| H0640 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:174 `_verify` / 抛出 | `stage == 'return_routing' / set(actual.get('proxy_dst') or []) != set(expected.get('proxy_dst') or [])` → `ReleaseError('Agent 上的回流网段与计划不一致')` |
| H0641 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:177 `_verify` / 抛出 | `stage == 'return_routing' / any((actual.get(key) != expected[key] for key in ('router_ip', 'mark', 'table', 'priority', 'safety_version')))` → `ReleaseError('Agent 上的回流路由或安全握手与计划不一致')` |
| H0642 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:179 `_verify` / 抛出 | `stage == 'return_routing' / not any((_return_rule_matches(rule, expected) for rule in status.get('fwmark_rules') or []))` → `ReleaseError('没有找到与计划一致的回流策略路由（ip rule）')` |
| H0643 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:190 `_verify` / 抛出 | `for check in checks['routes'] / not candidates or not all((is_managed(route.comment) for route in candidates))` → `ReleaseError(f"回读校验失败：{check['cidr']} 没有有效且无冲突的受管主路由指向 {check['gateway']}")` |
| H0644 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:196 `_verify` / 抛出 | `for check in checks.get('policy_routes') or [] / rule is None or not policy_route_ok(rule, check['gateway']) or {canon(cidr) for cidr in rule.dst} != {canon(cidr) for cidr in check['dst']}` → `ReleaseError(f"回读校验失败：策略路由「{check['comment']}」没有按计划排除 {check['gateway']} 并转发到 {check['gateway']}")` |
| H0645 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:200 `_verify` / 抛出 | `stage == 'routes' / remaining` → `ReleaseError('回读校验失败：应删除的路由仍然存在')` |
| H0646 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:204 `_verify` / 抛出 | `for check in checks.get('restored_routes') or [] / not any((route.dst == check['cidr'] and route.gateway == check['gateway'] and route.enabled for route in state.routes))` → `ReleaseError(f"回读校验失败：没有找到恢复的静态路由 {check['cidr']} → {check['gateway']}")` |
| H0647 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:206 `_verify` / 抛出 | `stage == 'routes' / {rule.id for rule in state.policy_routes or []} & set(checks.get('removed_policy_routes') or [])` → `ReleaseError('回读校验失败：应删除的端口分流规则仍然存在')` |
| H0648 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:241 `_rollback` / 捕获后的失败/降级/补偿 | `end - index > 1 / 捕获 (RouterError, RuntimeError, requests.RequestException)` → `except (RouterError, RuntimeError, requests.RequestException) as error:     errors = [str(error)] * len(run)` |
| H0649 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:256 `_rollback` / 捕获后的失败/降级/补偿 | `for (stage, op, undo) in entries[index:end] / 捕获 (RouterError, RuntimeError, requests.RequestException)` → `except (RouterError, RuntimeError, requests.RequestException) as error:     results.append({'stage': stage, 'summary': f"撤销失败：{op['summary']}（{error}）", 'status': 'failed'})` |
| H0650 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:259 `_rollback` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, RuntimeError, requests.RequestException)` → `except (RouterError, RuntimeError, requests.RequestException) as error:     results.append({'stage': done[0][0], 'summary': f'撤销失败：{error}', 'status': 'failed'})` |
| H0651 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:281 `_apply_stage` / 捕获后的失败/降级/补偿 | `not (end - index > 1) / 捕获 RouterError` → `except RouterError as error:     if getattr(error, 'undo', None):         finished(ops[index], error.undo, ok=False)     raise` |
| H0652 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:285 `_apply_stage` / 抛出 | `not (end - index > 1) / 捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0653 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:308 `_execute` / 抛出 | `plan.get('blockers')` → `ReleaseError('发布计划存在阻止项：' + '；'.join(plan['blockers']))` |
| H0654 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:313 `_execute` / 抛出 | `stage.get('enabled') is False / stage['ops']` → `ReleaseError('未启用的发布阶段包含写入操作，请重新预览')` |
| H0655 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:377 `_execute` / 抛出 | `current == 'ipv6_dns' / not plan.get('checks', {}).get('dhcp_dns')` → `ReleaseError('IPv6 DNS 下发缺少已确认的 OxiDNS DHCP 目标，请重新预览')` |
| H0656 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:406 `_execute` / 捕获后的失败/降级/补偿 | `捕获 (ReleaseError, RouterError, RuntimeError, requests.RequestException)` → `except (ReleaseError, RouterError, RuntimeError, requests.RequestException) as error:     record({'stage': current, 'summary': str(error) or error.__class__.__name__, 'status': 'failed'})     rollback = []     if isinstance(error, RouterRollbackError):         rollback.append({'stage': current, 'summary': str(error), 'status': 'failed'})     rollback.extend(_rollback(done, driver, agent))     return {'ok': False, 'failed_stage': current, 'log': log, 'rollback': rollback}` |
| H0657 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K31:412 `_execute` / 错误/未知结果 | `捕获 (ReleaseError, RouterError, RuntimeError, requests.RequestException)` → `{'ok': False, 'failed_stage': current, 'log': log, 'rollback': rollback}` |

### K32 [backend/routers/report.py](../../backend/routers/report.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0658 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K32:37 `_networks` / 捕获后的失败/降级/补偿 | `for value in values / 捕获 ValueError` → `except ValueError:     continue` |

### K33 [backend/routers/ros_address_lists.py](../../backend/routers/ros_address_lists.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0659 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K33:79 `_decode` / 校验/诊断降级 | `not count.isdigit() or len(pairs) != int(count) or any((not sep for (_, sep, _) in pairs))` → `None` |
| H0660 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K33:148 `router_digests` / 抛出 | `len(digests) != len(names)` → `RouterError('无法计算地址列表摘要：' + (output.strip()[:200] or 'RouterOS 无输出'))` |
| H0661 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K33:273 `locate` / 捕获后的失败/降级/补偿 | `for row_id in ids / 捕获 MissingResource` → `except MissingResource:     continue` |
| H0662 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K33:296 `occupied_names` / 抛出 | `'cf-names:' not in output` → `RouterError('无法读取已占用的地址列表名称：' + (output.strip()[:200] or 'RouterOS 无输出'))` |

### K34 [backend/routers/ros_clients.py](../../backend/routers/ros_clients.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0663 / R004/R005/R006 | K34:44 `duration` / 校验/诊断降级 | `not text or _DURATION.sub('', text)` → `None` |
| H0664 / R004/R005/R006 | K34:55 `_private` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return False` |
| H0665 / R004/R005/R006 | K34:56 `_private` / 校验/诊断降级 | `捕获 ValueError` → `False` |
| H0666 / R004/R005/R006 | K34:82 `lan_networks` / 捕获后的失败/降级/补偿 | `for row in addresses / 捕获 ValueError` → `except ValueError:     continue` |
| H0667 / R004/R005/R006 | K34:198 `confirm.check` / 捕获后的失败/降级/补偿 | `interface / 捕获 PingNotPermitted` → `except PingNotPermitted:     raise` |
| H0668 / R004/R005/R006 | K34:199 `confirm.check` / 抛出 | `interface / 捕获 PingNotPermitted` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0669 / R004/R005/R006 | K34:200 `confirm.check` / 捕获后的失败/降级/补偿 | `interface / 捕获 RouterError` → `except RouterError:     pass` |
| H0670 / R004/R005/R006 | K34:206 `confirm.check` / 捕获后的失败/降级/补偿 | `捕获 PingNotPermitted` → `except PingNotPermitted:     denied.set()` |
| H0671 / R004/R005/R006 | K34:208 `confirm.check` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     pass` |
| H0672 / R004/R005/R006 | K34:225 `confirm` / 捕获后的失败/降级/补偿 | `silent / 捕获 RouterError` → `except RouterError:     now = {}` |
| H0673 / R004/R005/R006 | K34:254 `read.listing` / 抛出 | `left < 1` → `RouterError('路由器响应太慢，读取时间已用完')` |
| H0674 / R004/R005/R006 | K34:262 `read.optional` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     reading.notes.append(f'无法读取{label}：{error}')     return ([], False)` |
| H0675 / R004/R005/R006 | K34:276 `read` / 捕获后的失败/降级/补偿 | `not interfaces / 捕获 RouterError` → `except RouterError:     members = []` |
| H0676 / R004/R005/R006 | K34:282 `read` / 抛出 | `not interfaces` → `RouterError('无法确定局域网接口：没有启用的 DHCP 服务，也没有配置私有地址的接口')` |

### K35 [backend/routers/ros_policy.py](../../backend/routers/ros_policy.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0677 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:76 `snapshot` / 抛出 | `not isinstance(settings, dict)` → `RouterError('无法读取 ROS 路由查找顺序')` |
| H0678 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:85 `snapshot` / 捕获后的失败/降级/补偿 | `'policy-rules' not in settings / 捕获 RouterError` → `except RouterError:     resource = None` |
| H0679 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:96 `policy_rules_missing_message` / 诊断 | `match and (int(match.group(1)), int(match.group(2))) >= (7, 22)` → `f'RouterOS {version} 未返回路由查找顺序（/routing/settings policy-rules），无法确认 Mangle 优先级'` |
| H0680 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:97 `policy_rules_missing_message` / 诊断 | `依赖/文件/事务故障；按函数前置条件` → `f"策略路由需要 RouterOS 7.22 或更高版本（当前 {version or '版本未知'}），请升级 RouterOS 后再发布"` |
| H0681 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:103 `check_policy_order` / 抛出 | `len(settings) == 1 and 'policy-rules' not in settings[0]` → `RouterError(policy_rules_missing_message(settings[0].get('routeros-version')))` |
| H0682 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:110 `check_policy_order` / 抛出 | `not isinstance(order, list) or any((not isinstance(item, str) for item in order)) or len(order) != len(set(order)) or (not {'mangle', 'main'}.issubset(order)) or set(order) - {'mangle', 'vrf-lookup', 'vrf-unreach', 'local', 'user', 'main'}` → `RouterError('无法确认 ROS 路由查找顺序中的 Mangle 优先级')` |
| H0683 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:112 `check_policy_order` / 抛出 | `any((order.index(item) < order.index('mangle') for item in ('main', 'user') if item in order))` → `RouterError('ROS 路由查找顺序将主表或用户路由规则置于 Mangle 之前，请先核对策略优先级')` |
| H0684 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:120 `check_route_egress` / 抛出 | `not isinstance(row, dict) or not enabled(row) or str(row.get('active', '')).lower() not in ('true', 'yes') or (str(row.get('immediate-gw') or '') != f'{gateway}%{interface}') or any((str(row.get(key, '')).lower() in ('true', 'yes') for key in ('invalid', 'blackhole', 'ecmp')))` → `RouterError('独立路由表的实际下一跳或出口与预览不一致，停止启用策略')` |
| H0685 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:173 `smart_sources` / 抛出 | `len(networks) > 4096` → `RouterError('OxiDNS 来源例外产生过多网段，请简化客户端设备的 DNS 策略')` |
| H0686 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:200 `inspect_desired` / 抛出 | `not policies['editable']` → `RouterError(policies['reason'])` |
| H0687 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:203 `inspect_desired` / 抛出 | `not result['sources']` → `RouterError('请先在「当前配置 → 客户端设备」中把至少一个 IPv4 地址或网段设为智能分流')` |
| H0688 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:204 `inspect_desired` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, OxidnsSettingsError, ValueError)` → `except (RouterError, OxidnsSettingsError, ValueError) as error:     result['error'] = str(error)` |
| H0689 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:220 `owned_rows` / 抛出 | `for entry in (previous or {}).get('resources', []) / len(matches) > 1` → `RouterError('策略路由对象 ID 不唯一，停止发布')` |
| H0690 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:223 `owned_rows` / 抛出 | `matches / config_row(matches[0]) != config_row(entry['config'])` → `RouterError('策略路由受管对象被外部修改，保留现场，请先核对：' + entry['path'] + '/' + entry['id'])` |
| H0691 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:247 `check_references` / 抛出 | `key in {'table', 'routing-table', 'new-routing-mark', 'routing-mark', 'src-address-list', 'dst-address-list', 'jump-target', 'chain', 'list', 'name'} / str(value).lstrip('!') in names` → `RouterError('其他配置正在使用本功能的路由表、地址列表或链，保留这些对象：' + str(value))` |
| H0692 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:249 `check_references` / 抛出 | `for (key, value) in row.items() / key == 'gateway' and any(('@' + name in str(value) for name in names))` → `RouterError('其他路由引用了本功能的路由表，不能自动切换')` |
| H0693 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:282 `bundle_health` / 抛出 | `for path in (MANGLE, NAT) / len(present) == len(expected) and present != expected` → `RouterError('受管策略规则的顺序被修改，请先核对 Mangle / NAT')` |
| H0694 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:288 `bundle_health` / 抛出 | `for entry in resources / str(row.get('invalid', '')).lower() in ('true', 'yes', '1')` → `RouterError('受管策略对象尚未生效：' + entry['path'] + '/' + entry['id'])` |
| H0695 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:290 `bundle_health` / 抛出 | `for entry in resources / entry['path'] == ROUTES and str(row.get('active', '')).lower() not in ('true', 'yes')` → `RouterError('独立路由表的下一跳未生效，请检查 Mihomo 网关和直连路由')` |
| H0696 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:298 `bundle_health` / 抛出 | `entry['path'] == NAT / any((enabled(other) and other.get('chain') == 'srcnat' and (other.get('action') not in {'accept', 'log', 'passthrough', 'return'}) for other in before))` → `RouterError('策略 NAT 免转换规则之前出现转换规则，请先核对规则顺序')` |
| H0697 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:323 `_nat_migrations` / 抛出 | `for key in ('dst-address', 'dst-address-list') / str(value).startswith('!')` → `RouterError('受管 NAT 的反向目的条件无法安全拆分，请先核对：' + row['.id'])` |
| H0698 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:331 `_nat_migrations` / 抛出 | `for value in values / not parsed` → `RouterError('受管 NAT 的目的地址无法安全拆分，请先核对：' + row['.id'])` |
| H0699 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:342 `_nat_migrations` / 抛出 | `for row in live[NAT] / row.get('dst-address-list')` → `RouterError('列表型 NAT 免转换无法自动拆分并保持后续列表更新语义，请先核对：' + row['.id'])` |
| H0700 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:344 `_nat_migrations` / 抛出 | `for row in live[NAT] / str(row.get('dynamic', 'false')).lower() in ('true', 'yes', '1')` → `RouterError('动态 NAT 免转换规则不能迁移：' + row['.id'])` |
| H0701 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:359 `build` / 阻止/只读原因 | `request.get('error')` → `result['blockers'].append(request['error'])` |
| H0702 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:361 `build` / 阻止/只读原因 | `live is None` → `result['blockers'].append('无法读取 ROS 策略路由配置')` |
| H0703 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:370 `build` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     result['blockers'].append(str(error))     return result` |
| H0704 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:371 `build` / 阻止/只读原因 | `捕获 RouterError` → `result['blockers'].append(str(error))` |
| H0705 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:377 `build` / 阻止/只读原因 | `request['selected'] / any((enabled(row) and row.get('action') == 'fasttrack-connection' for row in live[FILTER]))` → `result['blockers'].append('存在启用的 FastTrack，需先让策略流量避开 FastTrack；本功能不会修改现有防火墙规则')` |
| H0706 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:380 `build` / 阻止/只读原因 | `request['selected'] / any((enabled(row) and row.get('chain') == 'prerouting' and (row.get('action') in {'mark-routing', 'route', 'jump', 'accept', 'return'}) for row in other_mangle))` → `result['blockers'].append('已有 prerouting 分流或提前终止规则，无法确认新策略的优先级；请先核对现有 Mangle')` |
| H0707 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:382 `build` / 阻止/只读原因 | `request['selected'] / any((enabled(row) and row.get('name') != 'main' for row in live[VRFS]))` → `result['blockers'].append('存在非 main VRF，无法安全确定策略路由作用范围')` |
| H0708 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:398 `build` / 捕获后的失败/降级/补偿 | `for row in live[ROUTES] / 捕获 (KeyError, ValueError)` → `except (KeyError, ValueError):     continue` |
| H0709 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:412 `build` / 阻止/只读原因 | `for row in live[ROUTES] / is_managed(row.get('comment')) and (not str(row.get('comment', '')).startswith(PREFIX)) and (row.get('gateway') == desired.get('proxy_gateway')) and policy_only_overlap(row['dst-address']) and (str(row.get('dynamic', 'false')).lower() not in ('true', 'yes', '1'))` → `result['blockers'].append('受管主表路由无法安全迁移，请先核对：' + row['.id'])` |
| H0710 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:419 `build` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     result['blockers'].append(str(error))     return result` |
| H0711 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:420 `build` / 阻止/只读原因 | `捕获 RouterError` → `result['blockers'].append(str(error))` |
| H0712 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:428 `build` / 阻止/只读原因 | `request['selected'] and (not spec['destinations'])` → `result['blockers'].append('选中的策略路由规则集没有可用的真实 IPv4 网段')` |
| H0713 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:446 `build` / 阻止/只读原因 | `request['selected'] and spec['destinations'] / interface is None` → `result['blockers'].append('无法确认 Mihomo 的 LAN 出口：' + reason)` |
| H0714 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:450 `build` / 阻止/只读原因 | `for cidr in spec['destinations'] / assessment['status'] == 'unknown'` → `result['blockers'].append(f"{cidr}：{assessment['detail']}")` |
| H0715 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K35:482 `build` / 阻止/只读原因 | `request['selected']` → `result['blockers'].append('没有可用的策略资源名称')` |

### K36 [backend/routers/ros_policy_write.py](../../backend/routers/ros_policy_write.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0716 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:31 `_read` / 捕获后的失败/降级/补偿 | `捕获 MissingResource` → `except MissingResource:     return None` |
| H0717 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:32 `_read` / 校验/诊断降级 | `捕获 MissingResource` → `None` |
| H0718 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:33 `_read` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError('策略路由对象回读格式不完整：' + path)` |
| H0719 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:39 `_guard` / 抛出 | `row is None or config_row(row) != config_row(item['config'])` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + item['path'] + '/' + item['id'])` |
| H0720 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:47 `_guard_in` / 抛出 | `row is None or config_row(row) != config_row(item['config'])` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + item['path'] + '/' + item['id'])` |
| H0721 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:53 `_writable` / 抛出 | `str(row.get('dynamic', 'false')).lower() in ('true', 'yes')` → `RouterError('动态策略对象不能修改')` |
| H0722 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:84 `_put` / 抛出 | `not isinstance(row, dict) or not row.get('.id')` → `UncertainWrite('策略对象已提交但未返回对象 ID')` |
| H0723 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:85 `_put` / 捕获后的失败/降级/补偿 | `捕获 UncertainWrite` → `except UncertainWrite:     candidates = [row for row in driver._list(path) if row.get('.id') not in ids[path] and _matches_values(path, row, values)]     if len(candidates) != 1:         raise RouterRollbackError('策略对象写入结果不明确，未删除任何不明归属的对象，请检查路由器') from None     journal.append({'action': 'delete', **_resource(path, candidates[0])})     raise` |
| H0724 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:89 `_put` / 抛出 | `捕获 UncertainWrite / len(candidates) != 1` → `RouterRollbackError('策略对象写入结果不明确，未删除任何不明归属的对象，请检查路由器')` |
| H0725 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:91 `_put` / 抛出 | `捕获 UncertainWrite` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0726 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:104 `_confirm` / 抛出 | `actual is None or not _matches_values(path, actual, values)` → `RouterError('策略路由回读与提交内容不一致：' + path + detail)` |
| H0727 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:108 `_confirm` / 抛出 | `path == ROUTES and values.get('routing-table', 'main') != 'main' and (str(actual.get('active', '')).lower() not in ('true', 'yes'))` → `RouterError('独立路由表的下一跳未生效，不启用 Mangle')` |
| H0728 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:113 `_confirm` / 抛出 | `path == NAT and values.get('place-before') / values['place-before'] not in order or order.index(row_id) >= order.index(values['place-before'])` → `RouterError('策略 NAT 免转换规则位置不正确')` |
| H0729 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:143 `_address_rows` / 捕获后的失败/降级/补偿 | `for attempt in range(attempts) / 捕获 RouterError` → `except RouterError:     if attempt + 1 == attempts:         raise     time.sleep(1)` |
| H0730 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:145 `_address_rows` / 抛出 | `捕获 RouterError / attempt + 1 == attempts` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0731 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:175 `_run_address_batches` / 抛出 | `for batch in batches / not count.strip().isdigit()` → `RouterError('批量写入地址列表未执行完成：' + (output.strip()[:200] or 'RouterOS 无输出'))` |
| H0732 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:190 `_settled_address_rows` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterRollbackError('地址列表批量写入结果不明确，未删除任何不明归属的对象，请检查路由器：' + name)` |
| H0733 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:225 `_create_addresses` / 抛出 | `for (shared, members) in groups.items() / not name` → `RouterError('地址列表条目缺少列表名')` |
| H0734 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:233 `_create_addresses` / 捕获后的失败/降级/补偿 | `for (shared, members) in groups.items() / 捕获 UncertainWrite` → `except UncertainWrite as error:     (failure, rows) = (error, _settled_address_rows(driver, name))` |
| H0735 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:235 `_create_addresses` / 捕获后的失败/降级/补偿 | `for (shared, members) in groups.items() / 捕获 RouterError` → `except RouterError as error:     (failure, rows) = (error, _address_rows(driver, name, attempts=3))` |
| H0736 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:258 `_create_addresses` / 抛出 | `for (shared, members) in groups.items() / failure is not None` → `failure` |
| H0737 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:260 `_create_addresses` / 抛出 | `for (shared, members) in groups.items() / unknown` → `RouterError(f'批量写入期间地址列表 {name} 出现未提交的条目，保留这些对象，请检查路由器')` |
| H0738 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:266 `_create_addresses` / 抛出 | `for (shared, members) in groups.items() / failed or missing` → `RouterError(f"地址列表 {name} 有 {max(failed, len(missing))} 条写入失败（{'、'.join(missing[:5])}）{detail}")` |
| H0739 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:300 `_delete_addresses` / 抛出 | `for item in items / row is None or config_row(row) != config_row(item['config'])` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + item['path'] + '/' + item['id'])` |
| H0740 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:306 `_delete_addresses` / 抛出 | `set(_live_addresses(driver, items)) & {item['id'] for item in items}` → `RouterError('策略路由回读校验失败：对象仍然存在')` |
| H0741 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:321 `_delete` / 抛出 | `_read(driver, item['path'], item['id']) is not None` → `RouterError('策略路由回读校验失败：对象仍然存在')` |
| H0742 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:333 `_delete_routes` / 抛出 | `for item in items / row is None or config_row(row) != config_row(item['config'])` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + item['path'] + '/' + item['id'])` |
| H0743 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:335 `_delete_routes` / 捕获后的失败/降级/补偿 | `for item in items / 捕获 RouterError` → `except RouterError as error:     failure = error     break` |
| H0744 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:343 `_delete_routes` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     for row in _query(driver, '.id', removing):         driver._request('DELETE', ROUTES + '/' + quote(row['.id'], safe='*'))` |
| H0745 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:349 `_delete_routes` / 抛出 | `removing and _query(driver, '.id', removing)` → `RouterError('策略路由回读校验失败：对象仍然存在')` |
| H0746 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:351 `_delete_routes` / 抛出 | `failure is not None` → `failure` |
| H0747 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:362 `_restore_routes` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     for step in steps:         try:             _restore_step(driver, step, ids, remapped)         except RouterError as error:             failures.append(str(error))     return` |
| H0748 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:366 `_restore_routes` / 捕获后的失败/降级/补偿 | `for step in steps / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0749 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:367 `_restore_routes` / 阻止/只读原因 | `for step in steps / 捕获 RouterError` → `failures.append(str(error))` |
| H0750 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:382 `_restore_routes` / 捕获后的失败/降级/补偿 | `for start in range(0, len(batched), ROUTE_BATCH) / 捕获 RouterError` → `except RouterError as error:     for (index, _) in chunk:         errors[index] = str(error)     continue` |
| H0751 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:401 `_restore_routes` / 捕获后的失败/降级/补偿 | `for (position, (index, values)) in enumerate(chunk[:count]) / 捕获 RouterError` → `except RouterError as error:     errors[index] = str(error)` |
| H0752 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:412 `_restore_routes` / 捕获后的失败/降级/补偿 | `for (index, values) in sorted(single) / 捕获 RouterError` → `except RouterError as error:     errors[index] = str(error)` |
| H0753 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:417 `_restore_routes` / 捕获后的失败/降级/补偿 | `created / 捕获 RouterError` → `except RouterError as error:     rows = None     for (index, *_) in created:         errors[index] = str(error)` |
| H0754 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:424 `_restore_routes` / 捕获后的失败/降级/补偿 | `for (index, values, row_id, undo) in created if rows is not None else [] / 捕获 RouterError` → `except RouterError as error:     errors[index] = str(error)` |
| H0755 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:426 `_restore_routes` / 阻止/只读原因 | `依赖/文件/事务故障；按函数前置条件` → `failures.extend((errors[index] for index in sorted(errors)))` |
| H0756 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:441 `_restore_step` / 抛出 | `step['action'] == 'delete' / _read(driver, step['path'], step['id']) is not None` → `RouterError('回滚删除后对象仍存在')` |
| H0757 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:451 `_restore_step` / 抛出 | `before / _read(driver, step['path'], values['place-before']) is None` → `RouterError('原规则位置已改变，不能安全恢复顺序')` |
| H0758 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:465 `_restore_delete_addresses` / 抛出 | `set(_live_addresses(driver, steps)) & set(removing)` → `RouterError('回滚删除后对象仍存在')` |
| H0759 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:467 `_restore_delete_addresses` / 抛出 | `changed` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + ADDRESSES + '/' + changed[0]['id'])` |
| H0760 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:476 `_restore_sweep` / 抛出 | `{row['.id'] for row in _address_rows(driver, step['list'], attempts=3)} & set(removing)` → `RouterError('回滚删除后对象仍存在')` |
| H0761 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:496 `_restore_addresses` / 抛出 | `changed` → `RouterError('策略路由对象在操作期间发生变化，保留现场：' + ADDRESSES + '/' + changed[0]['id'])` |
| H0762 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:514 `run_steps` / 捕获后的失败/降级/补偿 | `is_address and action in ('delete', 'restore', 'sweep') / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0763 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:515 `run_steps` / 阻止/只读原因 | `is_address and action in ('delete', 'restore', 'sweep') / 捕获 RouterError` → `failures.append(str(error))` |
| H0764 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:529 `run_steps` / 阻止/只读原因 | `action == 'operation' / not failures` → `failures.extend((error for error in driver.restore_many(undos, stop_on_failure=True) if error and error != routeros_batch.SKIPPED))` |
| H0765 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:531 `run_steps` / 捕获后的失败/降级/补偿 | `not failures / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0766 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:532 `run_steps` / 阻止/只读原因 | `not failures / 捕获 RouterError` → `failures.append(str(error))` |
| H0767 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:543 `run_steps` / 捕获后的失败/降级/补偿 | `end - index >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0768 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:544 `run_steps` / 阻止/只读原因 | `end - index >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `failures.append(str(error))` |
| H0769 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:554 `run_steps` / 捕获后的失败/降级/补偿 | `end - index >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0770 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:555 `run_steps` / 阻止/只读原因 | `end - index >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `failures.append(str(error))` |
| H0771 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:562 `run_steps` / 捕获后的失败/降级/补偿 | `for ((action, is_address), run) in groupby(steps, key=lambda step: (step['action'], step.get('path') == ADDRESSES)) / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0772 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:563 `run_steps` / 阻止/只读原因 | `for ((action, is_address), run) in groupby(steps, key=lambda step: (step['action'], step.get('path') == ADDRESSES)) / 捕获 RouterError` → `failures.append(str(error))` |
| H0773 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:583 `restore` / 抛出 | `retained / any(((item['path'], item['id']) not in present_keys \| missing_allowed for item in retained))` → `RouterError('保留的策略对象缺失，无法安全回滚')` |
| H0774 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:586 `restore` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     failures.append(str(error))     delete_safe = False` |
| H0775 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:587 `restore` / 阻止/只读原因 | `捕获 RouterError` → `failures.append(str(error))` |
| H0776 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:603 `restore` / 抛出 | `failures` → `RouterRollbackError('策略路由回滚未完成：' + '；'.join(failures))` |
| H0777 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:635 `_verify_replacement_nat` / 抛出 | `for check in op['static_checks'] / interface is None or not preserves_sources(rows, sources, destinations, interface, context)` → `RouterError('替代静态 NAT 无法证明覆盖原策略的来源、目的及出口，保留原来源策略：' + check['cidr'] + ('；' + reason if reason else ''))` |
| H0778 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:646 `apply` / 抛出 | `actual != expected` → `RouterError('策略路由配置在预览后发生变化，请重新预览')` |
| H0779 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:664 `apply` / 抛出 | `for check in op['static_checks'] / not candidates or not all((is_managed(route.comment) for route in candidates))` → `RouterError('替代静态路由尚未生效，保留原来源策略：' + check['cidr'])` |
| H0780 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:708 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     undo = {'steps': list(reversed(journal)), 'previous': op.get('previous')}     try:         restore(driver, undo)     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     raise` |
| H0781 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:712 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0782 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:713 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0783 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:714 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0784 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:725 `verify` / 抛出 | `check['spec'] is None / owned or owned_rows(check.get('previous'), live)` → `RouterError('策略路由撤销后仍有受管对象')` |
| H0785 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:739 `verify` / 抛出 | `not manifest or manifest.get('spec') != check['spec'] or len(owned) != len(manifest['resources'])` → `RouterError('策略路由回读校验失败：来源、目的或转发资源不完整')` |
| H0786 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:745 `verify` / 抛出 | `any((enabled(row) and row.get('action') == 'fasttrack-connection' for row in live[FILTER]))` → `RouterError('发布期间出现 FastTrack，无法确认来源策略继续生效')` |
| H0787 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:754 `verify` / 抛出 | `any((enabled(row) and row.get('chain') == 'prerouting' and (row.get('action') in {'mark-routing', 'route', 'jump', 'accept', 'return'}) for row in state.nat_context['mangle']))` → `RouterError('发布期间出现其他 prerouting 分流或终止规则，请重新预览')` |
| H0788 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:759 `verify` / 抛出 | `interface is None` → `RouterError('回读无法确认策略下一跳出口：' + reason)` |
| H0789 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:761 `verify` / 抛出 | `check['spec'].get('egress_interface', interface) != interface` → `RouterError('策略下一跳出口在发布期间发生变化')` |
| H0790 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:764 `verify` / 抛出 | `check['spec']['nat_interface'] / interface != check['spec']['nat_interface']` → `RouterError('策略下一跳出口在发布期间发生变化')` |
| H0791 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:770 `verify` / 抛出 | `for cidr in check['spec']['destinations'] / assessment['status'] != 'not_required'` → `RouterError('策略流量不再能确认无需 NAT 免转换：' + assessment['detail'])` |
| H0792 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:778 `verify` / 抛出 | `for migration in check.get('migrations', []) / (migration['path'], migration['id']) in present` → `RouterError('应迁移的受管路由或 NAT 免转换仍然存在')` |
| H0793 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K36:782 `verify` / 抛出 | `for replacement in check.get('replacements', []) / not any((_matches(row, expected) for row in live[replacement['path']]))` → `RouterError('迁移后应保留的 NAT 目的范围不完整')` |

### K37 [backend/routers/ros_return_advisory.py](../../backend/routers/ros_return_advisory.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0794 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:112 `_possible` / 校验/诊断降级 | `row.get('src-address-type') == 'local' or row.get('dst-address-type') == 'local'` → `False` |
| H0795 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:115 `_possible` / 校验/诊断降级 | `for (direction, networks) in (('src', sources), ('dst', destinations)) / not _address_match(row.get(direction + '-address'), networks)` → `False` |
| H0796 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:117 `_possible` / 校验/诊断降级 | `for (direction, networks) in (('src', sources), ('dst', destinations)) / not _list_match(row.get(direction + '-address-list'), networks, address_sets)` → `False` |
| H0797 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:121 `_possible` / 校验/诊断降级 | `for (suffix, is_list) in (('interface', False), ('interface-list', True)) / _selector_matches(row.get(f'{direction}-{suffix}'), interface, context, is_list) is False` → `False` |
| H0798 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:135 `_covers_invalid` / 校验/诊断降级 | `not _matches_state(row, 'invalid') or any((value for (key, value) in row.items() if key not in METADATA \| SCOPE \| {'connection-state'}))` → `False` |
| H0799 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:145 `_covers_invalid` / 校验/诊断降级 | `for suffix in ('address', 'address-list') / values is None or not networks` → `False` |
| H0800 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:148 `_covers_invalid` / 校验/诊断降级 | `for suffix in ('address', 'address-list') / values and (not parsed)` → `False` |
| H0801 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:151 `_covers_invalid` / 校验/诊断降级 | `negate / any((net.overlaps(target) for net in parsed for target in networks))` → `False` |
| H0802 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:153 `_covers_invalid` / 校验/诊断降级 | `not (negate) / not all((any((target.subnet_of(net) for net in parsed)) for target in networks))` → `False` |
| H0803 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:155 `_covers_invalid` / 校验/诊断降级 | `any((row.get(key) for key in ('src-address-type', 'dst-address-type')))` → `False` |
| H0804 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:183 `warnings_for` / 捕获后的失败/降级/补偿 | `context.get(key) is None / 捕获 RouterError` → `except RouterError:     pass` |
| H0805 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K37:185 `warnings_for` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, ValueError, KeyError)` → `except (RouterError, ValueError, KeyError):     return []` |

### K38 [backend/routers/ros_values.py](../../backend/routers/ros_values.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0806 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K38:25 `comparable_values` / 捕获后的失败/降级/补偿 | `path == '/ip/route' and isinstance(result.get('gateway'), str) / 捕获 ValueError` → `except ValueError:     pass` |

### K39 [backend/routers/route_editor.py](../../backend/routers/route_editor.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0807 / R019/R020/R021 | K39:60 `_adapter` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError('该路由器暂不支持手工路由管理')` |
| H0808 / R019/R020/R021 | K39:69 `read_routes` / 抛出 | `not isinstance(rows, list) or any((not isinstance(row, dict) or not isinstance(row.get('id'), str) or (not row['id']) or (not isinstance(row.get('snapshot'), dict)) for row in rows)) or len({row['id'] for row in rows}) != len(rows)` → `RouterError('路由列表缺少完整、唯一的对象信息，停止操作')` |
| H0809 / R019/R020/R021 | K39:76 `_values` / 抛出 | `not isinstance(raw, dict) or raw.keys() != fields` → `RouteEditValidationError('请提供网段、下一跳、启用状态和备注，不能包含其他字段')` |
| H0810 / R019/R020/R021 | K39:78 `_values` / 抛出 | `not isinstance(raw['cidr'], str) or not isinstance(raw['gateway'], str)` → `RouteEditValidationError('网段和下一跳必须为 IPv4 地址文本')` |
| H0811 / R019/R020/R021 | K39:82 `_values` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouteEditValidationError('网段必须为有效 IPv4 网段，下一跳必须为单个 IPv4 地址') from None` |
| H0812 / R019/R020/R021 | K39:83 `_values` / 抛出 | `捕获 (ValueError, TypeError)` → `RouteEditValidationError('网段必须为有效 IPv4 网段，下一跳必须为单个 IPv4 地址')` |
| H0813 / R019/R020/R021 | K39:85 `_values` / 抛出 | `gateway.is_unspecified or gateway.is_multicast or gateway.is_loopback or (int(gateway) == 4294967295)` → `RouteEditValidationError('下一跳必须为可用的单播 IPv4 地址')` |
| H0814 / R019/R020/R021 | K39:87 `_values` / 抛出 | `type(raw['enabled']) is not bool` → `RouteEditValidationError('启用状态必须为 true 或 false')` |
| H0815 / R019/R020/R021 | K39:89 `_values` / 抛出 | `not isinstance(raw['comment'], str) or len(raw['comment']) > 512 or '\x00' in raw['comment']` → `RouteEditValidationError('备注最多 512 个字符，且不能包含空字符')` |
| H0816 / R019/R020/R021 | K39:97 `normalize_request` / 抛出 | `not isinstance(payload, dict) or set(payload) - {'action', 'id', 'revision', 'values', 'fingerprint'}` → `RouteEditValidationError('路由操作参数格式错误')` |
| H0817 / R019/R020/R021 | K39:100 `normalize_request` / 抛出 | `action not in ('add', 'update', 'delete')` → `RouteEditValidationError('只支持添加、编辑或删除单条路由')` |
| H0818 / R019/R020/R021 | K39:106 `normalize_request` / 抛出 | `action != 'add' / not isinstance(row_id, str) or not row_id or len(row_id) > 128 or any((ord(char) < 32 for char in row_id))` → `RouteEditValidationError('请选择有效的路由记录')` |
| H0819 / R019/R020/R021 | K39:108 `normalize_request` / 抛出 | `action != 'add' / not isinstance(revision, str) or len(revision) != 64 or any((c not in '0123456789abcdef' for c in revision))` → `RouteEditValidationError('缺少路由原始版本，请刷新列表后重试')` |
| H0820 / R019/R020/R021 | K39:111 `normalize_request` / 抛出 | `not (action != 'add') / payload.get('id') or payload.get('revision')` → `RouteEditValidationError('新增路由不能指定已有路由的身份')` |
| H0821 / R019/R020/R021 | K39:115 `normalize_request` / 抛出 | `not (action != 'delete') / payload.get('values') is not None` → `RouteEditValidationError('删除操作不能同时修改路由字段')` |
| H0822 / R019/R020/R021 | K39:126 `build_route_plan` / 抛出 | `action != 'add' / current is None` → `RouteEditConflict('该路由已不存在，请刷新列表后重试')` |
| H0823 / R019/R020/R021 | K39:128 `build_route_plan` / 抛出 | `action != 'add' / public_route(current)['revision'] != intent['revision']` → `RouteEditConflict('路由已被其他操作修改，请刷新列表后重新预览')` |
| H0824 / R019/R020/R021 | K39:130 `build_route_plan` / 抛出 | `action != 'add' / not current['editable']` → `RouteEditConflict(current.get('reason') or '该路由为只读，不能修改或删除')` |
| H0825 / R019/R020/R021 | K39:137 `build_route_plan` / 抛出 | `values is not None / duplicate` → `RouteEditConflict(f"主路由表中已存在 {values['cidr']}，请编辑已有路由，避免重复添加")` |

### K40 [backend/routers/routeros.py](../../backend/routers/routeros.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0826 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:89 `RouterOSDriver._request` / 捕获后的失败/降级/补偿 | `捕获 requests.RequestException` → `except requests.RequestException as error:     error_type = RouterError if method == 'GET' else UncertainWrite     raise error_type(f'无法连接 RouterOS：{error.__class__.__name__}') from None` |
| H0827 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:91 `RouterOSDriver._request` / 抛出 | `捕获 requests.RequestException` → `error_type(f'无法连接 RouterOS：{error.__class__.__name__}')` |
| H0828 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:93 `RouterOSDriver._request` / 抛出 | `response.status_code == 401` → `RouterError('RouterOS 认证失败，请核对 ConfigFlow 中的用户名、密码和账号允许的来源地址；在 WinBox → System → Users / Groups 检查用户组的 api、rest-api 权限')` |
| H0829 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:96 `RouterOSDriver._request` / 抛出 | `response.status_code == 403` → `RouterError('RouterOS 拒绝访问，请先确认所用 HTTP/HTTPS 服务允许 REST API。' + PERMISSION_HELP)` |
| H0830 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:98 `RouterOSDriver._request` / 抛出 | `response.status_code == 404 and method == 'GET'` → `MissingResource('RouterOS 未提供 REST API（HTTPS 需要 7.1+ 并启用 www-ssl；HTTP 需要 7.9+ 并启用 www）')` |
| H0831 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:104 `RouterOSDriver._request` / 捕获后的失败/降级/补偿 | `not 200 <= response.status_code < 300 / 捕获 ValueError` → `except ValueError:     body = {}` |
| H0832 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:112 `RouterOSDriver._request` / 抛出 | `not 200 <= response.status_code < 300` → `error_type(message)` |
| H0833 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:117 `RouterOSDriver._request` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     error_type = RouterError if method == 'GET' else UncertainWrite     raise error_type('RouterOS 返回的不是 JSON') from None` |
| H0834 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:119 `RouterOSDriver._request` / 抛出 | `捕获 ValueError` → `error_type('RouterOS 返回的不是 JSON')` |
| H0835 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:128 `RouterOSDriver._list` / 抛出 | `not isinstance(rows, list) or any((not isinstance(row, dict) for row in rows))` → `RouterError(f'RouterOS {path} 返回的列表格式无法识别')` |
| H0836 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:148 `RouterOSDriver.ping` / 捕获后的失败/降级/补偿 | `捕获 requests.RequestException` → `except requests.RequestException as error:     raise RouterError(f'RouterOS ping 未完成：{error.__class__.__name__}') from None` |
| H0837 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:149 `RouterOSDriver.ping` / 抛出 | `捕获 requests.RequestException` → `RouterError(f'RouterOS ping 未完成：{error.__class__.__name__}')` |
| H0838 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:152 `RouterOSDriver.ping` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     body = None` |
| H0839 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:159 `RouterOSDriver.ping` / 抛出 | `re.search('\\bnot enough permissions\\b', detail, re.IGNORECASE)` → `PingNotPermitted(f'RouterOS 账号缺少 test 权限（{detail}）')` |
| H0840 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:160 `RouterOSDriver.ping` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'RouterOS ping 返回 HTTP {response.status_code}' + (f'（{detail}）' if detail else ''))` |
| H0841 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:165 `RouterOSDriver.probe` / 抛出 | `not isinstance(resource, dict)` → `RouterError('RouterOS 系统信息格式无法识别')` |
| H0842 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:169 `RouterOSDriver.probe` / 抛出 | `not match or (int(match.group(1)), int(match.group(2))) < (7, 1)` → `RouterError(f"需要 RouterOS 7.1+，当前版本 {version or '未知'}")` |
| H0843 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:177 `RouterOSDriver.probe` / 捕获后的失败/降级/补偿 | `for (path, capability) in (('/ip/route', 'routes'), ('/ip/firewall/nat', 'nat_bypass'), ('/ip/firewall/address-list?list=cf-probe', 'address_sets')) / 捕获 RouterError` → `except RouterError:     pass` |
| H0844 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:191 `RouterOSDriver.read_state` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     mangle = None` |
| H0845 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:239 `RouterOSDriver.read_state` / 捕获后的失败/降级/补偿 | `for (key, path) in (('routing_rules', '/routing/rule'), ('mangle', MANGLE), ('vrfs', '/ip/vrf')) / 捕获 RouterError` → `except RouterError:     pass` |
| H0846 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:246 `RouterOSDriver.read_state` / 捕获后的失败/降级/补偿 | `for (key, path) in (('interface_lists', '/interface/list'), ('interface_members', '/interface/list/member')) / 捕获 RouterError` → `except RouterError:     pass` |
| H0847 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:253 `RouterOSDriver.read_state` / 捕获后的失败/降级/补偿 | `for row in routes / 捕获 ValueError` → `except ValueError:     continue` |
| H0848 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:262 `RouterOSDriver.read_state` / 捕获后的失败/降级/补偿 | `for row in routes / 捕获 (KeyError, TypeError, ValueError)` → `except (KeyError, TypeError, ValueError):     distance = None` |
| H0849 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:309 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `not active_servers` → `global_blockers.append('未发现启用的 DHCP 服务，无法确认网段 DNS 会被下发')` |
| H0850 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:313 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for server in active_servers / not server.get('name') or _truthy(server.get('invalid'))` → `global_blockers.append(f'{label} 信息不完整或无效，无法确认 DNS 下发')` |
| H0851 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:316 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for server in active_servers / radius not in ('no', 'false', 'accounting')` → `global_blockers.append(f'{label} 启用了 RADIUS 或其状态未知，可能覆盖 DHCP DNS')` |
| H0852 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:317 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for server in active_servers` → `global_blockers.extend(_dhcp_overrides(server, label, options, option_sets))` |
| H0853 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:323 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for matcher in matchers` → `global_blockers.extend(_dhcp_overrides({'dhcp-option-set': matcher.get('option-set', '')}, label, options, option_sets))` |
| H0854 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:337 `RouterOSDriver.read_dhcp_dns` / 抛出 | `for row in networks / not row_id or row_id in seen_ids` → `RouterError('RouterOS DHCP 网段缺少唯一对象 ID')` |
| H0855 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:341 `RouterOSDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for row in networks / 捕获 ValueError` → `except ValueError:     raise RouterError('RouterOS DHCP 网段地址无法识别') from None` |
| H0856 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:342 `RouterOSDriver.read_dhcp_dns` / 抛出 | `for row in networks / 捕获 ValueError` → `RouterError('RouterOS DHCP 网段地址无法识别')` |
| H0857 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:346 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in networks / _truthy(row.get('dynamic')) or _truthy(row.get('invalid'))` → `blockers.append('该 DHCP 网段为动态或无效对象，不能自动修改 DNS')` |
| H0858 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:347 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in networks` → `blockers.extend(_dhcp_overrides(row, f'DHCP 网段 {network}', options, option_sets))` |
| H0859 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:353 `RouterOSDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for lease in override_leases / 捕获 ValueError` → `except ValueError:     address = None` |
| H0860 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:359 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for lease in override_leases / _truthy(lease.get('radius'))` → `blockers.append(f'{label} 由 RADIUS 管理，可能覆盖 DHCP DNS')` |
| H0861 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:360 `RouterOSDriver.read_dhcp_dns` / 阻止/只读原因 | `for row in networks / for lease in override_leases` → `blockers.extend(_dhcp_overrides(lease, label, options, option_sets))` |
| H0862 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:363 `RouterOSDriver.read_dhcp_dns` / 捕获后的失败/降级/补偿 | `for row in networks / 捕获 ValueError` → `except ValueError:     raise RouterError(f'RouterOS DHCP 网段 {network} 的 DNS 地址无法识别') from None` |
| H0863 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:364 `RouterOSDriver.read_dhcp_dns` / 抛出 | `for row in networks / 捕获 ValueError` → `RouterError(f'RouterOS DHCP 网段 {network} 的 DNS 地址无法识别')` |
| H0864 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:396 `RouterOSDriver._find` / 捕获后的失败/降级/补偿 | `捕获 MissingResource` → `except MissingResource:     return None` |
| H0865 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:397 `RouterOSDriver._find` / 校验/诊断降级 | `捕获 MissingResource` → `None` |
| H0866 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:399 `RouterOSDriver._find` / 抛出 | `not isinstance(row, dict) or str(row.get('.id')) != str(row_id)` → `RouterError(f'RouterOS {path} 返回的对象格式无法识别')` |
| H0867 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:418 `RouterOSDriver._row` / 抛出 | `row is None` → `RouterError(f'RouterOS {path} 中找不到 id={row_id}，可能已被手动删除')` |
| H0868 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:424 `RouterOSDriver._payload` / 抛出 | `_truthy(row.get('dynamic'))` → `RouterError('RouterOS 动态对象不能接管、修改或删除')` |
| H0869 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:428 `RouterOSDriver._payload` / 抛出 | `path == '/ipv6/nd' / _flag(row, 'dynamic') or _flag(row, 'invalid')` → `RouterError('动态或无效的 IPv6 通告不能修改，请重新预览')` |
| H0870 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:473 `RouterOSDriver._perform` / 抛出 | `current is not None / self._ownership_snapshot(path, current) != change.get('expected')` → `RouterError('回滚对象已发生外部变化，保留现有配置')` |
| H0871 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:479 `RouterOSDriver._perform` / 抛出 | `method == 'RESTORE' / path in ROUTE_PATHS and any((str(row.get('.id')) not in change.get('known_ids', []) and row.get('dst-address') == values.get('dst-address') and (row.get('routing-table', 'main') == values.get('routing-table', 'main')) for row in rows))` → `RouterError('回滚期间已有其他程序新增同网段路由，保留现有配置')` |
| H0872 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:481 `RouterOSDriver._perform` / 抛出 | `method == 'RESTORE' / values.get('place-before') and (not any((row.get('.id') == values['place-before'] for row in rows)))` → `RouterError('原 NAT 规则位置已变化，无法安全恢复顺序')` |
| H0873 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:495 `RouterOSDriver._perform` / 抛出 | `method == 'PUT' / not isinstance(row_id, str) or not re.fullmatch('\\*[0-9a-fA-F]+', row_id) or row_id in before or (scoped and (not self._in_scope(path, row, values)))` → `UncertainWrite(f'RouterOS {path} 已提交但没有返回可信的新对象 ID')` |
| H0874 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:496 `RouterOSDriver._perform` / 捕获后的失败/降级/补偿 | `method == 'PUT' / 捕获 UncertainWrite` → `except UncertainWrite as error:     raise RouterRollbackError(f'{error}；无法确认新增对象归属，已保留现场，请检查路由器') from error` |
| H0875 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:499 `RouterOSDriver._perform` / 抛出 | `method == 'PUT' / 捕获 UncertainWrite` → `RouterRollbackError(f'{error}；无法确认新增对象归属，已保留现场，请检查路由器')` |
| H0876 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:521 `RouterOSDriver._perform` / 捕获后的失败/降级/补偿 | `restoring / 捕获 MissingResource` → `except MissingResource:     return {'path': path, 'id': row_id, 'absent': True}` |
| H0877 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:524 `RouterOSDriver._perform` / 抛出 | `restoring / not isinstance(current, dict) or current.get('.id') != row_id` → `RouterError('回滚对象无法完整回读，保留现有配置')` |
| H0878 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:527 `RouterOSDriver._perform` / 抛出 | `row is None and method == 'DELETE' / not (restoring)` → `RouterError(f'RouterOS {path} 中找不到 id={row_id}')` |
| H0879 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:529 `RouterOSDriver._perform` / 抛出 | `row is None` → `RouterError(f'RouterOS {path} 中找不到 id={row_id}')` |
| H0880 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:535 `RouterOSDriver._perform` / 抛出 | `'expected' in change and snapshot != change['expected']` → `RouterError('对象在操作期间发生外部变化，保留现有配置')` |
| H0881 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:537 `RouterOSDriver._perform` / 抛出 | `restoring and 'expected' not in change` → `RouterError('回滚缺少本次写入快照，无法确认对象归属')` |
| H0882 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:555 `RouterOSDriver._perform` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'不支持的 RouterOS 写入方法 {method}')` |
| H0883 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:571 `RouterOSDriver._check_written` / 抛出 | `check.get('absent') / row is not None` → `RouterError('RouterOS 回读校验失败：应删除的对象仍然存在')` |
| H0884 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:574 `RouterOSDriver._check_written` / 抛出 | `row is None` → `RouterError('RouterOS 回读校验失败：没有找到已写入的对象')` |
| H0885 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:579 `RouterOSDriver._check_written` / 抛出 | `key == 'place-before' / target is None or rows.index(row) >= rows.index(target)` → `RouterError('RouterOS 回读校验失败：NAT 规则顺序不正确')` |
| H0886 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:581 `RouterOSDriver._check_written` / 抛出 | `not (key == 'place-before') / _comparable(key, row.get(key, '')) != _comparable(key, value)` → `RouterError(f"RouterOS 回读校验失败：{check['id']} 的 {key} 与提交值不一致")` |
| H0887 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:589 `RouterOSDriver._restore` / 捕获后的失败/降级/补偿 | `for change in changes / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0888 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:590 `RouterOSDriver._restore` / 阻止/只读原因 | `for change in changes / 捕获 RouterError` → `failures.append(str(error))` |
| H0889 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:592 `RouterOSDriver._restore` / 抛出 | `failures` → `RouterRollbackError('RouterOS 回滚未完成：' + '；'.join(failures))` |
| H0890 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:612 `RouterOSDriver._transaction` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         self._restore(list(reversed(undo)))     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     raise` |
| H0891 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:615 `RouterOSDriver._transaction` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H0892 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:616 `RouterOSDriver._transaction` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H0893 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:617 `RouterOSDriver._transaction` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0894 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:667 `RouterOSDriver.apply` / 抛出 | `kind == 'dhcp_dns_update' / not isinstance(dns, list) or not 1 <= len(dns) <= 2 or any((not isinstance(value, str) for value in dns))` → `RouterError('DHCP DNS 必须包含一至两个 IPv4 地址')` |
| H0895 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:670 `RouterOSDriver.apply` / 捕获后的失败/降级/补偿 | `kind == 'dhcp_dns_update' / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     raise RouterError('DHCP DNS 地址不是有效 IPv4 地址') from None` |
| H0896 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:671 `RouterOSDriver.apply` / 抛出 | `kind == 'dhcp_dns_update' / 捕获 (ValueError, TypeError)` → `RouterError('DHCP DNS 地址不是有效 IPv4 地址')` |
| H0897 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:677 `RouterOSDriver.apply` / 抛出 | `kind == 'dhcp_dns_update' / not isinstance(scope, dict) or any((not isinstance(scope.get(key), str) or not scope[key] for key in ('id', 'network', 'gateway'))) or scope['id'] != str(op.get('id') or '')` → `RouterError('RouterOS DHCP DNS 缺少完整且一致的目标范围，请重新预览')` |
| H0898 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:685 `RouterOSDriver.apply` / 抛出 | `kind == 'dhcp_dns_update' / entry.attributes != op.get('expected')` → `RouterError('RouterOS DHCP 网段在预览后发生变化，请重新预览')` |
| H0899 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:701 `RouterOSDriver.apply` / 抛出 | `kind == 'nat_update' / 'expected' in op and original != op['expected']` → `RouterError('RouterOS NAT 规则在预览后发生变化，请重新预览')` |
| H0900 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:703 `RouterOSDriver.apply` / 抛出 | `kind == 'nat_update' / original.get('chain') != 'srcnat' or original.get('action') != 'accept'` → `RouterError('RouterOS 目标规则已不是 NAT 免转换规则，请重新预览')` |
| H0901 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:719 `RouterOSDriver.apply` / 抛出 | `kind == 'nat_add' / op['src'] and (not sources)` → `RouterError('RouterOS NAT 源地址范围无法识别')` |
| H0902 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:731 `RouterOSDriver.apply` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError(f'不支持的操作 {kind}')` |
| H0903 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:746 `_comparable` / 捕获后的失败/降级/补偿 | `key in ('src-address', 'dst-address', 'address') / 捕获 ValueError` → `except ValueError:     pass` |
| H0904 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:766 `_dhcp_names` / 抛出 | `not (isinstance(value, str)) / not (isinstance(value, list) and all((isinstance(item, str) for item in value)))` → `RouterError('RouterOS DHCP 选项或 DNS 列表格式无法识别')` |
| H0905 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:777 `_dhcp_network_attributes` / 抛出 | `not isinstance(suppression, bool) and suppression not in ('true', 'false', 'yes', 'no')` → `RouterError('RouterOS DHCP dns-none 状态无法识别')` |
| H0906 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:792 `_dhcp_overrides` / 阻止/只读原因 | `for name in _dhcp_names(row.get('dhcp-option-set', '')) / len(matches) != 1 or 'options' not in matches[0]` → `blockers.append(f'{label} 的选项集 {name} 无法完整读取，可能覆盖 DHCP DNS')` |
| H0907 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:798 `_dhcp_overrides` / 阻止/只读原因 | `for name in dict.fromkeys(references) / len(matches) != 1` → `blockers.append(f'{label} 的选项 {name} 无法唯一识别，可能覆盖 DHCP DNS')` |
| H0908 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:810 `_dhcp_overrides` / 捕获后的失败/降级/补偿 | `re.fullmatch('[0-9]+', raw_code) / 捕获 ValueError` → `except ValueError:     pass` |
| H0909 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:813 `_dhcp_overrides` / 阻止/只读原因 | `for name in dict.fromkeys(references) / code == 6` → `blockers.append(f'{label} 使用自定义 DNS 选项 6（{name}），不能只修改网段 DNS')` |
| H0910 / R011/R012/R017/R018/R019/R020/R021（双vendor或指定vendor） | K40:815 `_dhcp_overrides` / 阻止/只读原因 | `not (code == 6) / code is None or not 1 <= code <= 254` → `blockers.append(f'{label} 的选项 {name} 编号未知，可能覆盖 DHCP DNS')` |

### K41 [backend/routers/routeros_batch.py](../../backend/routers/routeros_batch.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0911 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:51 `_query` / 抛出 | `for start in range(0, len(values), QUERY_TERMS) / not isinstance(result, list) or any((not isinstance(row, dict) for row in result))` → `RouterError(f'RouterOS {path} 返回的列表格式无法识别')` |
| H0912 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:60 `_retry` / 捕获后的失败/降级/补偿 | `for attempt in range(attempts) / 捕获 RouterError` → `except RouterError:     if attempt + 1 == attempts:         raise     time.sleep(1)` |
| H0913 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:62 `_retry` / 抛出 | `捕获 RouterError / attempt + 1 == attempts` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0914 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:155 `_console_add` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return None` |
| H0915 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:156 `_console_add` / 校验/诊断降级 | `捕获 RouterError` → `None` |
| H0916 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:160 `_console_add` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     if _refused(error):         return None     raise RouterRollbackError(f'{error}；批量新增路由的结果未知，无法确认新增对象归属，已保留现场，请检查路由器') from error` |
| H0917 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:162 `_console_add` / 校验/诊断降级 | `捕获 RouterError / _refused(error)` → `None` |
| H0918 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:163 `_console_add` / 抛出 | `捕获 RouterError` → `RouterRollbackError(f'{error}；批量新增路由的结果未知，无法确认新增对象归属，已保留现场，请检查路由器')` |
| H0919 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:177 `_console_add` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     raise RouterRollbackError(f'{error}；批量新增的路由无法回读，未删除任何不明归属的对象，请检查路由器') from error` |
| H0920 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:178 `_console_add` / 抛出 | `捕获 RouterError` → `RouterRollbackError(f'{error}；批量新增的路由无法回读，未删除任何不明归属的对象，请检查路由器')` |
| H0921 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:186 `_console_add` / 捕获后的失败/降级/补偿 | `row is None and ROW_ID.fullmatch(row_id) / 捕获 RouterError` → `except RouterError as error:     failure = failure or (index, RouterRollbackError(f'{error}；无法确认新增对象归属，已保留现场，请检查路由器'))     continue` |
| H0922 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:216 `_add` / 捕获后的失败/降级/补偿 | `failure is None or index < failure[0] / 捕获 RouterError` → `except RouterError as error:     failure = (index, error)` |
| H0923 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:233 `_add` / 抛出 | `failure is not None / problems` → `RouterRollbackError(f'{error}；{problems[0]}')` |
| H0924 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:234 `_add` / 抛出 | `failure is not None` → `error` |
| H0925 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:236 `_add` / 抛出 | `added['unexplained']` → `RouterRollbackError(f'RouterOS {path} 批量写入未执行完成；无法确认新增对象归属，已保留现场，请检查路由器')` |
| H0926 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:244 `_delete` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return ops` |
| H0927 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:264 `_delete` / 捕获后的失败/降级/补偿 | `for start in range(0, len(ids), REMOVE_BATCH) / 捕获 RouterError` → `except RouterError as error:     failure = error     break` |
| H0928 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:269 `_delete` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     for (op, undo) in prepared:         record(op, undo)     raise` |
| H0929 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:273 `_delete` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0930 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:284 `_delete` / 抛出 | `failure is not None` → `failure` |
| H0931 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:305 `restore_routes` / 阻止/只读原因 | `stop_on_failure and any((error for error in errors))` → `errors.extend([SKIPPED] * (len(undos) - index))` |
| H0932 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:319 `restore_routes` / 阻止/只读原因 | `method and len(group) >= BATCH_MIN and (not (stop_on_failure and method == 'RESTORE'))` → `errors.extend(_restore_deleted(driver, changes, stop_on_failure, run_path) if method == 'DELETE' else _restore_removed(driver, changes, run_path))` |
| H0933 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:324 `restore_routes` / 阻止/只读原因 | `for undo in group / stop_on_failure and any((error for error in errors))` → `errors.append(SKIPPED)` |
| H0934 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:326 `restore_routes` / 阻止/只读原因 | `not (method and len(group) >= BATCH_MIN and (not (stop_on_failure and method == 'RESTORE'))) / for undo in group` → `errors.append(_single(lambda : driver.apply(undo)))` |
| H0935 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:334 `_single` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return str(error)` |
| H0936 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:352 `_restore_deleted` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     rows = None` |
| H0937 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:364 `_restore_deleted.flush` / 捕获后的失败/降级/补偿 | `for start in range(0, len(ids), REMOVE_BATCH) / 捕获 RouterError` → `except RouterError:     break` |
| H0938 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:368 `_restore_deleted.flush` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     remaining = set(ids)` |
| H0939 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:406 `_restore_removed` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     for index in range(len(changes)):         single(index)     return errors` |
| H0940 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:420 `_restore_removed.flush` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     rows = None` |
| H0941 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:438 `_restore_removed.flush` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     for index in adding:         errors[index] = _rollback_failed(str(error))     return` |
| H0942 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:456 `_restore_removed.flush` / 捕获后的失败/降级/补偿 | `for (position, index) in enumerate(adding[:added['count']]) / 捕获 RouterError` → `except RouterError as error:     errors[index] = _rollback_failed(str(error))` |
| H0943 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:471 `_restore_removed` / 捕获后的失败/降级/补偿 | `current is not None / 捕获 RouterError` → `except RouterError as error:     errors[index] = _rollback_failed(str(error))` |
| H0944 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:511 `put_nat` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     return 0` |
| H0945 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:512 `put_nat` / 校验/诊断降级 | `捕获 RouterError` → `0` |
| H0946 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:541 `_nat_batch` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     if _refused(error):         return None     raise RouterRollbackError(f'{error}；批量新增 NAT 规则的结果未知，无法确认新增对象归属，已保留现场，请检查路由器') from error` |
| H0947 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:543 `_nat_batch` / 校验/诊断降级 | `捕获 RouterError / _refused(error)` → `None` |
| H0948 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:544 `_nat_batch` / 抛出 | `捕获 RouterError` → `RouterRollbackError(f'{error}；批量新增 NAT 规则的结果未知，无法确认新增对象归属，已保留现场，请检查路由器')` |
| H0949 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:562 `_nat_batch` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     raise RouterRollbackError(f'{error}；批量新增的 NAT 规则无法回读，未删除任何不明归属的对象，请检查路由器') from error` |
| H0950 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:563 `_nat_batch` / 抛出 | `捕获 RouterError` → `RouterRollbackError(f'{error}；批量新增的 NAT 规则无法回读，未删除任何不明归属的对象，请检查路由器')` |
| H0951 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:581 `_nat_batch` / 捕获后的失败/降级/补偿 | `for (index, row_id) in enumerate(ids) / 捕获 RouterError` → `except RouterError:     valid = False` |
| H0952 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:594 `_nat_batch` / 捕获后的失败/降级/补偿 | `for (row_id, expected) in owned / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H0953 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:595 `_nat_batch` / 阻止/只读原因 | `for (row_id, expected) in owned / 捕获 RouterError` → `failures.append(str(error))` |
| H0954 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K41:597 `_nat_batch` / 抛出 | `unproven or failures` → `RouterRollbackError('；'.join(failures + ([f'RouterOS {NAT} 批量写入结果无法确认，未删除任何不明归属的对象，请检查路由器'] if unproven else [])))` |

### K42 [backend/routers/routeros_fixed_ip.py](../../backend/routers/routeros_fixed_ip.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0955 / R005/R006（固定IP） | K42:32 `_id` / 抛出 | `not isinstance(value, str) or not ID.fullmatch(value)` → `RouterError('RouterOS DHCP 租约缺少有效对象 ID')` |
| H0956 / R005/R006（固定IP） | K42:68 `_row` / 捕获后的失败/降级/补偿 | `捕获 MissingResource` → `except MissingResource:     return None` |
| H0957 / R005/R006（固定IP） | K42:69 `_row` / 校验/诊断降级 | `捕获 MissingResource` → `None` |
| H0958 / R005/R006（固定IP） | K42:71 `_row` / 抛出 | `not isinstance(row, dict) or row.get('.id') != row_id` → `RouterError('RouterOS DHCP 租约按 ID 回读格式无法识别')` |
| H0959 / R005/R006（固定IP） | K42:83 `read` / 抛出 | `for row in result['leases'] / row_id in seen` → `RouterError('RouterOS DHCP 租约 ID 不唯一')` |
| H0960 / R005/R006（固定IP） | K42:104 `_scopes` / 捕获后的失败/降级/补偿 | `for own in snapshot['addresses'] / 捕获 ValueError` → `except ValueError:     continue` |
| H0961 / R005/R006（固定IP） | K42:113 `_scopes` / 捕获后的失败/降级/补偿 | `for network in snapshot['networks'] / 捕获 ValueError` → `except ValueError:     continue` |
| H0962 / R005/R006（固定IP） | K42:182 `preview` / 抛出 | `not state['eligible']` → `RouterError(state['reason'])` |
| H0963 / R005/R006（固定IP） | K42:186 `preview` / 抛出 | `any((ord(char) < 32 for char in name))` → `RouterError('设备名称不能包含控制字符')` |
| H0964 / R005/R006（固定IP） | K42:208 `_check` / 抛出 | `not _matches(row, binding) or _truthy(row.get('disabled')) != disabled` → `RouterError('RouterOS 固定 IP 回读校验失败：ID、IP、MAC、服务、启用状态或归属标记不一致')` |
| H0965 / R005/R006（固定IP） | K42:216 `_remove` / 抛出 | `not _matches(row, binding) or (expected is not None and _fields(row) != expected)` → `RouterError('RouterOS 租约已发生外部变化，保留现有配置')` |
| H0966 / R005/R006（固定IP） | K42:220 `_remove` / 捕获后的失败/降级/补偿 | `捕获 UncertainWrite` → `except UncertainWrite as error:     failure = error` |
| H0967 / R005/R006（固定IP） | K42:225 `_remove` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     readback_error = error     try:         remaining = _row(driver, binding['id'])     except RouterError as retry_error:         raise RouterRollbackError(f'RouterOS 删除结果未知，无法确认租约是否仍存在；请检查路由器：{retry_error}') from error` |
| H0968 / R005/R006（固定IP） | K42:231 `_remove` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as retry_error:     raise RouterRollbackError(f'RouterOS 删除结果未知，无法确认租约是否仍存在；请检查路由器：{retry_error}') from error` |
| H0969 / R005/R006（固定IP） | K42:232 `_remove` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'RouterOS 删除结果未知，无法确认租约是否仍存在；请检查路由器：{retry_error}')` |
| H0970 / R005/R006（固定IP） | K42:234 `_remove` / 抛出 | `remaining is not None` → `failure or readback_error or RouterError('RouterOS 删除回读校验失败：静态租约仍存在')` |
| H0971 / R005/R006（固定IP） | K42:241 `apply` / 抛出 | `latest != op` → `RouterError('RouterOS DHCP 配置或租约在预览后发生变化，请重新预览')` |
| H0972 / R005/R006（固定IP） | K42:256 `apply` / 捕获后的失败/降级/补偿 | `op['kind'] == 'make_static' / 捕获 UncertainWrite` → `except UncertainWrite:     conversion_pending = True     row = _row(driver, op['id'])     if row is None or _truthy(row.get('dynamic')):         raise     acquired = _binding(row)     acquired_fields = deepcopy(original)     raise` |
| H0973 / R005/R006（固定IP） | K42:261 `apply` / 抛出 | `捕获 UncertainWrite / row is None or _truthy(row.get('dynamic'))` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0974 / R005/R006（固定IP） | K42:264 `apply` / 抛出 | `op['kind'] == 'make_static' / 捕获 UncertainWrite` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0975 / R005/R006（固定IP） | K42:267 `apply` / 抛出 | `op['kind'] == 'make_static' / row is None or _truthy(row.get('dynamic'))` → `RouterError('RouterOS 未将动态租约转为静态')` |
| H0976 / R005/R006（固定IP） | K42:271 `apply` / 抛出 | `op['kind'] == 'make_static' / _fields(row) != original` → `RouterRollbackError('RouterOS 转换期间租约配置已变化，已保留现场，请检查路由器')` |
| H0977 / R005/R006（固定IP） | K42:276 `apply` / 抛出 | `op['kind'] == 'make_static' / any((acquired[key] != expected_binding[key] for key in ('ip', 'mac', 'server')))` → `RouterRollbackError('RouterOS 转换后的租约与预览目标不一致，已保留现场，请检查路由器')` |
| H0978 / R005/R006（固定IP） | K42:286 `apply` / 抛出 | `not (op['kind'] == 'make_static') / not isinstance(new_id, str) or not ID.fullmatch(new_id) or new_id in known_ids` → `UncertainWrite('RouterOS 新建租约未返回可信的新对象 ID')` |
| H0979 / R005/R006（固定IP） | K42:287 `apply` / 捕获后的失败/降级/补偿 | `not (op['kind'] == 'make_static') / 捕获 UncertainWrite` → `except UncertainWrite as error:     try:         candidates = [row for row in driver._list(DHCP_LEASES) if row.get('.id') not in known_ids and _matches(row, {**expected_binding, 'id': row.get('.id')})]         if len(candidates) != 1:             raise RouterError('无法唯一确认本次新增租约')         recovered = _row(driver, _id(candidates[0].get('.id')))         if recovered is None or _fields(recovered) != _fields(candidates[0]):             raise RouterError('新增租约在恢复回读时发生变化')         acquired = _binding(recovered)         acquired_fields = _fields(recovered)         expected_binding['id'] = acquired['id']     except RouterError as recovery_error:         raise RouterRollbackError(f'{error}；{recovery_error}，保留现场，请检查路由器…` |
| H0980 / R005/R006（固定IP） | K42:293 `apply` / 抛出 | `捕获 UncertainWrite / len(candidates) != 1` → `RouterError('无法唯一确认本次新增租约')` |
| H0981 / R005/R006（固定IP） | K42:296 `apply` / 抛出 | `捕获 UncertainWrite / recovered is None or _fields(recovered) != _fields(candidates[0])` → `RouterError('新增租约在恢复回读时发生变化')` |
| H0982 / R005/R006（固定IP） | K42:300 `apply` / 捕获后的失败/降级/补偿 | `捕获 UncertainWrite / 捕获 RouterError` → `except RouterError as recovery_error:     raise RouterRollbackError(f'{error}；{recovery_error}，保留现场，请检查路由器') from error` |
| H0983 / R005/R006（固定IP） | K42:301 `apply` / 抛出 | `捕获 UncertainWrite / 捕获 RouterError` → `RouterRollbackError(f'{error}；{recovery_error}，保留现场，请检查路由器')` |
| H0984 / R005/R006（固定IP） | K42:302 `apply` / 抛出 | `not (op['kind'] == 'make_static') / 捕获 UncertainWrite` → `error` |
| H0985 / R005/R006（固定IP） | K42:309 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     compensated = False     if acquired or conversion_pending:         try:             current = _row(driver, (acquired or expected_binding)['id'])             if current is not None and (not _truthy(current.get('dynamic'))):                 expected = deepcopy(acquired_fields if acquired_fields is not None else original)                 allowed_comments = {str(expected.get('comment') or ''), expected_binding['comment']}                 expected['comment'] = str(current.get('comment') or '')                 if any((_identity(current)[key] != expected_binding[key] for key in ('ip', 'mac', 'server'))) or str(current.get('comment') or '') not in allowed_comments or _fields(current)…` |
| H0986 / R005/R006（固定IP） | K42:323 `apply` / 抛出 | `current is not None and (not _truthy(current.get('dynamic'))) / any((_identity(current)[key] != expected_binding[key] for key in ('ip', 'mac', 'server'))) or str(current.get('comment') or '') not in allowed_comments or _fields(current) != expected` → `RouterError('回滚目标已发生外部变化，保留现有配置')` |
| H0987 / R005/R006（固定IP） | K42:326 `apply` / 捕获后的失败/降级/补偿 | `acquired or conversion_pending / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；RouterOS 固定 IP 回滚未完成：{rollback_error}') from error` |
| H0988 / R005/R006（固定IP） | K42:327 `apply` / 抛出 | `acquired or conversion_pending / 捕获 RouterError` → `RouterRollbackError(f'{error}；RouterOS 固定 IP 回滚未完成：{rollback_error}')` |
| H0989 / R005/R006（固定IP） | K42:329 `apply` / 抛出 | `捕获 RouterError / compensated and op['kind'] == 'make_static'` → `RouterError(f'{error}；{ROLLBACK_NOTE}')` |
| H0990 / R005/R006（固定IP） | K42:330 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H0991 / R005/R006（固定IP） | K42:340 `rollback` / 抛出 | `not str(binding.get('comment') or '').startswith(PREFIX) or not isinstance(binding.get('undo'), dict)` → `RouterError('RouterOS 固定 IP 回滚缺少本次写入的归属证据')` |
| H0992 / R005/R006（固定IP） | K42:346 `preview_delete` / 抛出 | `binding.get('vendor') != 'routeros' or not str(binding.get('comment') or '').startswith(PREFIX)` → `RouterError('只能删除 ConfigFlow 创建的静态租约')` |
| H0993 / R005/R006（固定IP） | K42:349 `preview_delete` / 抛出 | `not _matches(row, binding)` → `RouterError('静态租约已不存在或归属、IP、MAC 发生变化，不能删除')` |
| H0994 / R005/R006（固定IP） | K42:358 `delete` / 抛出 | `'expected' in binding and op != binding` → `RouterError('RouterOS 静态租约在预览后发生变化，请重新预览')` |
| H0995 / R005/R006（固定IP） | K42:366 `restore_deleted` / 抛出 | `receipt.get('kind') != 'delete' or not str(receipt.get('comment') or '').startswith(PREFIX)` → `RouterError('RouterOS 删除回滚缺少归属证据')` |
| H0996 / R005/R006（固定IP） | K42:371 `restore_deleted` / 抛出 | `current is not None` → `RouterError('RouterOS 删除回滚的对象 ID 已被占用，保留现有配置')` |
| H0997 / R005/R006（固定IP） | K42:374 `restore_deleted` / 抛出 | `for row in driver._list(DHCP_LEASES) / ipv4(row.get('address')) == receipt['ip'] or _mac(row.get('mac-address')) == receipt['mac']` → `RouterError('RouterOS 删除回滚时 IP 或 MAC 已出现其他租约，保留现有配置')` |
| H0998 / R005/R006（固定IP） | K42:381 `restore_deleted` / 抛出 | `_fields(row) != receipt['expected']` → `RouterRollbackError('RouterOS 删除回滚回读失败，原租约字段未完整恢复')` |

### K43 [backend/routers/routeros_nat.py](../../backend/routers/routeros_nat.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H0999 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:38 `_network` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return None` |
| H1000 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:39 `_network` / 校验/诊断降级 | `捕获 ValueError` → `None` |
| H1001 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:46 `_interface` / 校验/诊断降级 | `not value or any((character in value for character in (',', ';', '%', '/', '@')))` → `None` |
| H1002 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:49 `_interface` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return value` |
| H1003 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:89 `RouteEgressIndex.select` / 捕获后的失败/降级/补偿 | `for (index, (row, network)) in enumerate(self.main) / 捕获 (KeyError, TypeError, ValueError)` → `except (KeyError, TypeError, ValueError):     distance = None` |
| H1004 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:115 `_egress` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return (None, '代理网段或旁路下一跳不是有效 IPv4 地址')` |
| H1005 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:166 `_egress` / 捕获后的失败/降级/补偿 | `for entry in selection.selected / 捕获 ValueError` → `except ValueError:     return (None, '代理路由的实际下一跳无法识别')` |
| H1006 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:212 `_list_contains` / 校验/诊断降级 | `name == 'none'` → `False` |
| H1007 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:214 `_list_contains` / 校验/诊断降级 | `name in ('static', 'dynamic')` → `None` |
| H1008 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:217 `_list_contains` / 校验/诊断降级 | `not isinstance(definitions, list) or not isinstance(members, list)` → `None` |
| H1009 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:219 `_list_contains` / 校验/诊断降级 | `any((not isinstance(row, dict) for row in definitions + members))` → `None` |
| H1010 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:221 `_list_contains` / 校验/诊断降级 | `any((_enabled(row) and (not row.get('list') or not row.get('interface')) for row in members))` → `None` |
| H1011 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:224 `_list_contains` / 校验/诊断降级 | `len(matching) != 1 or matching[0].get('include') or matching[0].get('exclude')` → `None` |
| H1012 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:228 `_list_contains` / 校验/诊断降级 | `any((_enabled(row) and row.get('list') == name and (row.get('interface') == 'all' or str(row.get('interface') or '').startswith('all-')) for row in members))` → `None` |
| H1013 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:240 `_selector_matches` / 校验/诊断降级 | `not name or name.startswith('!')` → `None` |
| H1014 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:242 `_selector_matches` / 校验/诊断降级 | `interface is None and name not in ('all', 'none')` → `None` |
| H1015 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:265 `assess_nat` / 错误/未知结果 | `not isinstance(context, dict)` → `{'status': 'unknown', 'detail': '缺少 RouterOS NAT 判断依据，无法确认是否需要免 NAT'}` |
| H1016 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:268 `assess_nat` / 错误/未知结果 | `not isinstance(rows, list) or any((not isinstance(row, dict) for row in rows))` → `{'status': 'unknown', 'detail': '无法读取完整 NAT 规则，无法确认是否需要免 NAT'}` |
| H1017 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:270 `assess_nat` / 错误/未知结果 | `any((_enabled(row) and (not row.get('chain')) for row in rows))` → `{'status': 'unknown', 'detail': '启用的 NAT 规则缺少链信息，无法排除源地址转换'}` |
| H1018 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:302 `assess_nat` / 错误/未知结果 | `unknown` → `{'status': 'unknown', 'detail': unknown[0]}` |
| H1019 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:313 `_address_hits` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     return None` |
| H1020 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:314 `_address_hits` / 校验/诊断降级 | `捕获 (ValueError, TypeError)` → `None` |
| H1021 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:323 `_address_hits` / 校验/诊断降级 | `not isinstance(name, str) or name.startswith('!') or name not in lists` → `None` |
| H1022 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:336 `_address_hits` / 捕获后的失败/降级/补偿 | `name not in cache / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     cache[name] = None` |
| H1023 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:339 `_address_hits` / 校验/诊断降级 | `cache[name] is None` → `None` |
| H1024 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:377 `preserves_sources` / 校验/诊断降级 | `for row in rows / action != 'accept'` → `False` |
| H1025 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:440 `assess_bypass` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return {'status': 'unknown', 'detail': '无法确认 NAT 免转换的客户端范围', 'rule_ids': []}` |
| H1026 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:441 `assess_bypass` / 错误/未知结果 | `捕获 ValueError` → `{'status': 'unknown', 'detail': '无法确认 NAT 免转换的客户端范围', 'rule_ids': []}` |
| H1027 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K43:469 `assess_bypass` / 错误/未知结果 | `for row in rows / _address_hits(selectors, 'dst-address', destination, context, cache) != []` → `{'status': 'unknown', 'rule_ids': [], 'detail': '已有免转换规则的顺序或来源、地址列表、接口、协议等条件不能保证客户端流量均免转换；请先检查现有规则'}` |

### K44 [backend/routers/routeros_nat_list.py](../../backend/routers/routeros_nat_list.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1028 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:70 `_key` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     return text` |
| H1029 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:180 `_members` / 阻止/只读原因 | `for row in rows / _truthy(row.get('dynamic')) or row.get('timeout')` → `problems.append(f'地址列表 {name} 含动态条目，可能被其他规则写入，无法安全维护')` |
| H1030 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:182 `_members` / 阻止/只读原因 | `not (_truthy(row.get('dynamic')) or row.get('timeout')) / not _enabled(row)` → `problems.append(f"地址列表 {name} 含停用条目 {row.get('address')}，请启用或删除后重新预览")` |
| H1031 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:236 `_legacy` / 捕获后的失败/降级/补偿 | `for (key, group) in groups.items() / 捕获 ValueError` → `except ValueError:     continue` |
| H1032 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:272 `still_routed` / 校验/诊断降级 | `not isinstance(rows, list) or any((not isinstance(row, dict) for row in rows))` → `None` |
| H1033 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:338 `plan` / 校验/诊断降级 | `found['rule'] is None and (not found['problem']) and (not missing) and (not legacy)` → `None` |
| H1034 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:342 `plan` / 阻止/只读原因 | `lists is None` → `result['blockers'].append('无法读取 NAT 免转换地址列表，请重新预览')` |
| H1035 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:347 `plan` / 阻止/只读原因 | `lists.get('mangle_refs') is None` → `result['blockers'].append('无法读取 Mangle，不能确认 NAT 免转换地址列表未被其他规则引用')` |
| H1036 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:349 `plan` / 阻止/只读原因 | `lists.get('firewall_refs') is None` → `result['blockers'].append('无法读取防火墙 filter 或 raw 规则，不能确认 NAT 免转换地址列表未被其他规则引用')` |
| H1037 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:351 `plan` / 阻止/只读原因 | `found['problem']` → `result['blockers'].append(found['problem'])` |
| H1038 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:355 `plan` / 阻止/只读原因 | `依赖/文件/事务故障；按函数前置条件` → `result['blockers'].extend(problems + more)` |
| H1039 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:358 `plan` / 阻止/只读原因 | `for (name, foreign) in ((SRC_LIST, src_foreign), (DST_LIST, dst_foreign)) / foreign and rule is None` → `result['blockers'].append(f'地址列表 {name} 已存在非 ConfigFlow 条目，ConfigFlow 不会接管同名列表')` |
| H1040 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:553 `policy_migrations` / 抛出 | `for item in addresses / not networks` → `RouterError('受管 NAT 的目的地址无法安全拆分，请先核对：' + str(item.get('.id')))` |
| H1041 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:557 `policy_migrations` / 抛出 | `for item in addresses / item.get('comment') != COMMENT or _truthy(item.get('dynamic'))` → `RouterError('列表型 NAT 免转换中有非 ConfigFlow 条目与策略目的重叠，无法自动拆分，请先核对：' + str(item.get('.id')))` |
| H1042 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:587 `state_lists` / 捕获后的失败/降级/补偿 | `src or dst or any((candidate(row) for row in nats)) / 捕获 RouterError` → `except RouterError:     firewall = None` |
| H1043 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:608 `lists_of` / 捕获后的失败/降级/补偿 | `state.nat_lists is None and read is not None / 捕获 RouterError` → `except RouterError:     state.nat_lists = None` |
| H1044 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:647 `nat_resources` / 抛出 | `found['problem']` → `RouterError(found['problem'])` |
| H1045 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:649 `nat_resources` / 校验/诊断降级 | `found['rule'] is None` → `None` |
| H1046 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:669 `member_update` / 校验/诊断降级 | `not ops or not enrolled or any((op['kind'] != 'nat_list_sync' for op in ops))` → `None` |
| H1047 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:676 `member_update` / 校验/诊断降级 | `for op in ops / op['rule'] is None or op['replace_rule'] or op['withdraw'] or op['legacy'] or op.get('stale') or op['src_add'] or op['src_remove'] or (op['rule']['id'] != enrolled['rule']['id']) or (_config(op['rule']['config']) != _config(enrolled['rule']['config'])) or (op['out_interface'] != str(enrolled['rule']['config'].get('out-interface') or ''))` → `None` |
| H1048 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:701 `_check_unchanged` / 抛出 | `foreign` → `RouterError(f'其他防火墙规则 {path}/{row_id} 正在引用 {name}，未做任何修改')` |
| H1049 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:704 `_check_unchanged` / 抛出 | `found['problem']` → `RouterError(found['problem'])` |
| H1050 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:707 `_check_unchanged` / 抛出 | `op['rule'] is None / found['rule'] is not None` → `RouterError(CHANGED)` |
| H1051 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:710 `_check_unchanged` / 抛出 | `not (op['rule'] is None) / found['rule'] is None or str(found['rule']['.id']) != op['rule']['id'] or _config(found['rule']) != _config(op['rule']['config'])` → `RouterError(CHANGED)` |
| H1052 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:715 `_check_unchanged` / 抛出 | `for item in op['legacy'] + op.get('stale', []) / row is None or _config(row) != _config(item['config'])` → `RouterError(CHANGED)` |
| H1053 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:720 `_check_unchanged` / 抛出 | `for (kind, name) in (('src', SRC_LIST), ('dst', DST_LIST)) / problems` → `RouterError(problems[0])` |
| H1054 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:722 `_check_unchanged` / 抛出 | `for (kind, name) in (('src', SRC_LIST), ('dst', DST_LIST)) / others and op['rule'] is None` → `RouterError(f'地址列表 {name} 已存在非 ConfigFlow 条目，ConfigFlow 不会接管同名列表')` |
| H1055 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:726 `_check_unchanged` / 抛出 | `for item in op[kind + '_remove'] / row is None or _config(row) != _config(item['config'])` → `RouterError(CHANGED)` |
| H1056 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:729 `_check_unchanged` / 抛出 | `for (kind, name) in (('src', SRC_LIST), ('dst', DST_LIST)) / any((_key(value) in present for value in op[kind + '_add']))` → `RouterError(CHANGED)` |
| H1057 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:739 `_settle` / 抛出 | `for attempt in range(SETTLE_READS) / row is None` → `RouterError('新的 NAT 免转换规则回读时不存在，已回滚')` |
| H1058 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:744 `_settle` / 抛出 | `依赖/文件/事务故障；按函数前置条件` → `RouterError('新的 NAT 免转换规则未生效（invalid），已回滚')` |
| H1059 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:770 `_prove` / 抛出 | `found['problem'] or found['rule'] is None or str(found['rule']['.id']) != rule_id or (not preserves_sources(rows, sources, destinations, op['out_interface'] or None, context))` → `RouterError('新的 NAT 免转换规则回读后无法证明覆盖全部客户端与目的网段，已回滚' + (f"：{found['problem']}" if found['problem'] else ''))` |
| H1060 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:788 `delete_rules` / 抛出 | `for item in items / row is None or _config(row) != _config(item['config'])` → `RouterError(f"NAT 规则在操作期间发生变化，保留现场：{NAT}/{item['id']}")` |
| H1061 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:805 `delete_rules` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     present = {str(row.get('.id')) for row in driver._list(NAT)}     for row_id in ids:         if row_id in present:             driver._request('DELETE', NAT + '/' + quote(row_id, safe='*'))` |
| H1062 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:812 `delete_rules` / 抛出 | `deleting & {str(row.get('.id')) for row in driver._list(NAT)}` → `RouterError('旧的 NAT 免转换规则删除后仍然存在')` |
| H1063 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:838 `restore_rules` / 阻止/只读原因 | `current is not None / _config(current) != _config(step['config'])` → `failures.append(f"NAT 规则在操作期间发生变化，保留现场：{NAT}/{step['id']}")` |
| H1064 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:852 `restore_rules` / 阻止/只读原因 | `for step in batched / values.get('place-before') and values['place-before'] not in by_id` → `failures.append('原规则位置已改变，不能安全恢复顺序')` |
| H1065 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:860 `restore_rules` / 捕获后的失败/降级/补偿 | `len(pending) >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))     return` |
| H1066 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:861 `restore_rules` / 阻止/只读原因 | `len(pending) >= routeros_batch.BATCH_MIN / 捕获 RouterError` → `failures.append(str(error))` |
| H1067 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:871 `restore_rules` / 捕获后的失败/降级/补偿 | `for step in [step for (step, _) in pending[handled:]] + [step for step in steps if 'order' not in step] / 捕获 RouterError` → `except RouterError as error:     failures.append(str(error))` |
| H1068 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:872 `restore_rules` / 阻止/只读原因 | `for step in [step for (step, _) in pending[handled:]] + [step for step in steps if 'order' not in step] / 捕获 RouterError` → `failures.append(str(error))` |
| H1069 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:931 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         restore(driver, {'kind': 'nat_list_restore', 'steps': list(reversed(journal)), 'keep': keep})     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；{rollback_error}') from error     raise` |
| H1070 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:934 `apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；{rollback_error}') from error` |
| H1071 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:935 `apply` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；{rollback_error}')` |
| H1072 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:936 `apply` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H1073 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:959 `restore` / 阻止/只读原因 | `foreign` → `failures.append(f'其他防火墙规则 {foreign[0][0]}/{foreign[0][1]} 正在引用 {foreign[0][2]}，保留这些对象')` |
| H1074 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:960 `restore` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     delete_safe = False     failures.append(str(error))` |
| H1075 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:962 `restore` / 阻止/只读原因 | `捕获 RouterError` → `failures.append(str(error))` |
| H1076 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:972 `restore` / 阻止/只读原因 | `kept` → `failures.append('原有免转换对象未能全部恢复，已保留新的 NAT 免转换规则和地址列表条目，请检查后处理')` |
| H1077 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:974 `restore` / 抛出 | `failures` → `RouterRollbackError('NAT 免转换回滚未完成：' + '；'.join(failures))` |
| H1078 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:985 `verify` / 抛出 | `lists is None` → `RouterError('回读校验失败：无法读取 NAT 免转换地址列表')` |
| H1079 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:989 `verify` / 抛出 | `lists.get('mangle_refs') is None` → `RouterError('回读校验失败：无法读取 Mangle，不能确认 NAT 免转换地址列表未被其他规则引用')` |
| H1080 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:991 `verify` / 抛出 | `lists.get('firewall_refs') is None` → `RouterError('回读校验失败：无法读取防火墙 filter 或 raw 规则，不能确认 NAT 免转换地址列表未被其他规则引用')` |
| H1081 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:996 `verify` / 抛出 | `check['withdraw'] / found['rule'] is not None or any((candidate(row) for row in rows))` → `RouterError('回读校验失败：应删除的 NAT 免转换规则仍然存在')` |
| H1082 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:999 `verify` / 抛出 | `not (check['withdraw']) / found['problem']` → `RouterError('回读校验失败：' + found['problem'])` |
| H1083 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:1001 `verify` / 抛出 | `not (check['withdraw']) / check['destinations'] and found['rule'] is None` → `RouterError('回读校验失败：没有找到 NAT 免转换规则')` |
| H1084 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:1003 `verify` / 抛出 | `not (check['withdraw']) / found['rule'] is not None and found['out_interface'] != check['out_interface']` → `RouterError('回读校验失败：NAT 免转换规则的出口限制与计划不一致')` |
| H1085 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:1005 `verify` / 抛出 | `set(check['removed']) & present` → `RouterError('回读校验失败：应删除的旧 NAT 免转换规则仍然存在')` |
| H1086 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:1011 `verify` / 抛出 | `for (kind, name) in (('src', SRC_LIST), ('dst', DST_LIST)) / ours != expected` → `RouterError(f'回读校验失败：地址列表 {name} 的 ConfigFlow 条目与计划不一致')` |
| H1087 / R011/R012/R018/R016（发布/plan/report，按模块特性seed） | K44:1016 `verify` / 抛出 | `not check['withdraw'] / any((_key(value) not in present_keys for value in wanted_values))` → `RouterError(f'回读校验失败：地址列表 {name} 缺少计划中的网段')` |

### K45 [backend/routers/routeros_routes.py](../../backend/routers/routeros_routes.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1088 / R019/R020/R021 | K45:49 `_entry` / 阻止/只读原因 | `not isinstance(row_id, str) or not re.fullmatch('\\*[0-9a-fA-F]+', row_id)` → `reasons.append('路由缺少有效对象 ID')` |
| H1089 / R019/R020/R021 | K45:51 `_entry` / 阻止/只读原因 | `not (not isinstance(row_id, str) or not re.fullmatch('\\*[0-9a-fA-F]+', row_id)) / row_id in duplicate_ids` → `reasons.append('路由对象 ID 不唯一')` |
| H1090 / R019/R020/R021 | K45:55 `_entry` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     reasons.append('仅支持普通 IPv4 目标网段')` |
| H1091 / R019/R020/R021 | K45:56 `_entry` / 阻止/只读原因 | `捕获 ValueError` → `reasons.append('仅支持普通 IPv4 目标网段')` |
| H1092 / R019/R020/R021 | K45:60 `_entry` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     reasons.append('仅支持单个 IPv4 下一跳；接口、递归和多路径路由只读')` |
| H1093 / R019/R020/R021 | K45:61 `_entry` / 阻止/只读原因 | `捕获 ValueError` → `reasons.append('仅支持单个 IPv4 下一跳；接口、递归和多路径路由只读')` |
| H1094 / R019/R020/R021 | K45:64 `_entry` / 阻止/只读原因 | `table != 'main'` → `reasons.append('仅支持 main 主路由表')` |
| H1095 / R019/R020/R021 | K45:67 `_entry` / 阻止/只读原因 | `for name in sorted(FLAGS & row.keys()) / _flag(row[name]) is None` → `reasons.append(f'{name} 状态无法识别')` |
| H1096 / R019/R020/R021 | K45:69 `_entry` / 阻止/只读原因 | `_flag(row.get('dynamic', 'false'))` → `reasons.append('动态路由只读')` |
| H1097 / R019/R020/R021 | K45:71 `_entry` / 阻止/只读原因 | `'static' in row and _flag(row['static']) is False` → `reasons.append('非静态路由只读')` |
| H1098 / R019/R020/R021 | K45:73 `_entry` / 阻止/只读原因 | `_flag(row.get('connect', 'false'))` → `reasons.append('直连路由只读')` |
| H1099 / R019/R020/R021 | K45:75 `_entry` / 阻止/只读原因 | `any((_flag(row.get(key, 'false')) for key in SPECIAL_FLAGS - {'connect'}))` → `reasons.append('ECMP、无效或特殊路由只读')` |
| H1100 / R019/R020/R021 | K45:77 `_entry` / 阻止/只读原因 | `row.get('vrf-interface')` → `reasons.append('绑定 VRF 接口的路由只读')` |
| H1101 / R019/R020/R021 | K45:80 `_entry` / 阻止/只读原因 | `unknown` → `reasons.append('包含无法安全恢复的字段：' + '、'.join(unknown))` |
| H1102 / R019/R020/R021 | K45:83 `_entry` / 阻止/只读原因 | `any((not isinstance(value, (str, bool, int)) or isinstance(value, float) for (key, value) in row.items() if key not in RUNTIME_FIELDS))` → `reasons.append('路由配置字段格式无法识别')` |
| H1103 / R019/R020/R021 | K45:86 `_entry` / 阻止/只读原因 | `not isinstance(comment, str)` → `reasons.append('路由备注格式无法识别')` |
| H1104 / R019/R020/R021 | K45:120 `_canonical` / 捕获后的失败/降级/补偿 | `key == 'dst-address' / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     pass` |
| H1105 / R019/R020/R021 | K45:134 `_find` / 抛出 | `len(found) > 1` → `RouterError('RouterOS 路由 ID 不唯一，请刷新后重试')` |
| H1106 / R019/R020/R021 | K45:142 `_no_duplicate` / 抛出 | `any((item['table'] == 'main' and item['cidr'] == cidr and (item['id'] != exclude_id) for item in _entries(rows)))` → `error_type(f'main 主表已存在目标 {cidr} 的路由，请刷新后检查')` |
| H1107 / R019/R020/R021 | K45:149 `_values` / 抛出 | `not isinstance(values, dict) or set(values) != {'cidr', 'gateway', 'enabled', 'comment'} or type(values['enabled']) is not bool or any((not isinstance(values[key], str) for key in ('cidr', 'gateway', 'comment')))` → `RouterError('静态路由参数格式无效')` |
| H1108 / R019/R020/R021 | K45:153 `_values` / 捕获后的失败/降级/补偿 | `捕获 ValueError` → `except ValueError:     raise RouterError('静态路由需要 IPv4 目标网段和单个 IPv4 下一跳') from None` |
| H1109 / R019/R020/R021 | K45:154 `_values` / 抛出 | `捕获 ValueError` → `RouterError('静态路由需要 IPv4 目标网段和单个 IPv4 下一跳')` |
| H1110 / R019/R020/R021 | K45:169 `_restoration_write` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as caught:     error = caught` |
| H1111 / R019/R020/R021 | K45:173 `_restoration_write` / 抛出 | `not verify(rows, response)` → `RouterError(f"恢复后回读不一致{(f'（{error}）' if error else '')}")` |
| H1112 / R019/R020/R021 | K45:184 `_compensate` / 抛出 | `action == 'add' / not row_id` → `RouterError('新增响应没有可信对象 ID，无法确认归属；保留现有路由，请刷新检查')` |
| H1113 / R019/R020/R021 | K45:189 `_compensate` / 抛出 | `action == 'add' / not _same(current, after_snapshot)` → `RouterError('新增路由存在外部变化，未删除该对象')` |
| H1114 / R019/R020/R021 | K45:198 `_compensate` / 抛出 | `action == 'update' / current is None or not _same(current, after_snapshot)` → `RouterError('目标路由存在外部变化，未覆盖或重建该对象')` |
| H1115 / R019/R020/R021 | K45:206 `_compensate` / 抛出 | `current is not None` → `RouterError('原路由 ID 已发生外部变化，未覆盖该对象')` |
| H1116 / R019/R020/R021 | K45:223 `mutate_route` / 抛出 | `action not in ('add', 'update', 'delete')` → `RouterError('不支持的静态路由操作')` |
| H1117 / R019/R020/R021 | K45:231 `mutate_route` / 抛出 | `action == 'add' / expected is not None` → `RouterError('新增静态路由不应包含旧对象')` |
| H1118 / R019/R020/R021 | K45:236 `mutate_route` / 抛出 | `not (action == 'add') / not isinstance(expected, dict) or not isinstance(expected.get('snapshot'), dict)` → `RouterConflictError('缺少原始路由快照，请刷新后重试')` |
| H1119 / R019/R020/R021 | K45:239 `mutate_route` / 抛出 | `not (action == 'add') / before is None or before != expected` → `RouterConflictError('路由在读取后发生变化，请刷新后重试')` |
| H1120 / R019/R020/R021 | K45:241 `mutate_route` / 抛出 | `not (action == 'add') / not before['editable']` → `RouterConflictError(before['reason'] or '该路由只读')` |
| H1121 / R019/R020/R021 | K45:254 `mutate_route` / 抛出 | `action == 'add' / not isinstance(candidate_id, str) or not re.fullmatch('\\*[0-9a-fA-F]+', candidate_id) or any((row.get('.id') == candidate_id for row in before_rows))` → `UncertainWrite('RouterOS 新增响应没有可信的新对象 ID')` |
| H1122 / R019/R020/R021 | K45:260 `mutate_route` / 抛出 | `action == 'add' / any((_canonical(after_snapshot).get(key) != _canonical(payload).get(key) for key in payload))` → `RouterError('RouterOS 新增响应与提交内容不一致')` |
| H1123 / R019/R020/R021 | K45:269 `mutate_route` / 抛出 | `action == 'delete' / current is not None` → `RouterError('删除后回读仍发现原路由')` |
| H1124 / R019/R020/R021 | K45:272 `mutate_route` / 抛出 | `current is None or not _same(current, after_snapshot)` → `RouterError('写入后完整回读与预期不一致，可能发生了外部修改')` |
| H1125 / R019/R020/R021 | K45:275 `mutate_route` / 抛出 | `not entry['editable']` → `RouterError('写入后的路由无法继续安全编辑：' + entry['reason'])` |
| H1126 / R019/R020/R021 | K45:278 `mutate_route` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     try:         _compensate(driver, action, before_rows, before, row_id, after_snapshot)     except RouterError as rollback_error:         raise RouterRollbackError(f'{error}；回滚未完成：{rollback_error}') from error     raise` |
| H1127 / R019/R020/R021 | K45:281 `mutate_route` / 捕获后的失败/降级/补偿 | `捕获 RouterError / 捕获 RouterError` → `except RouterError as rollback_error:     raise RouterRollbackError(f'{error}；回滚未完成：{rollback_error}') from error` |
| H1128 / R019/R020/R021 | K45:282 `mutate_route` / 抛出 | `捕获 RouterError / 捕获 RouterError` → `RouterRollbackError(f'{error}；回滚未完成：{rollback_error}')` |
| H1129 / R019/R020/R021 | K45:283 `mutate_route` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |

### K46 [backend/routers/rule_sync.py](../../backend/routers/rule_sync.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1130 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:93 `capture_manual` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, OSError, ValueError, RuntimeError)` → `except (RouterError, OSError, ValueError, RuntimeError) as error:     store.write(profile_id, router['id'], {'status': 'waiting', 'message': '本次发布未建立自动同步基准：' + (str(error) if isinstance(error, RouterError) else '同步记录无法保存，请重新预览发布'), 'checked_at': timestamp})` |
| H1131 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:106 `_capture` / 抛出 | `live_router is None or controls(current, live_router) != signature` → `RouterError('发布期间设置有变化，请重新手动预览发布后建立基准')` |
| H1132 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:109 `_capture` / 抛出 | `pinned['proxy_cidrs'] != desired['proxy_cidrs']` → `RouterError('发布期间规则缓存已变化，请重新手动预览发布后建立基准')` |
| H1133 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:117 `_capture` / 抛出 | `router.get('ros_policy_rule_sets') / not policy or set(destinations) != set(policy['spec']['destinations'])` → `RouterError('已发布策略与规则内容不一致，无法建立自动同步基准')` |
| H1134 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:140 `Downloads.__call__` / 捕获后的失败/降级/补偿 | `key in self.items / 捕获 RemoteRuleValidationError` → `except RemoteRuleValidationError as error:     raise DownloadError(str(error)) from None` |
| H1135 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:141 `Downloads.__call__` / 抛出 | `key in self.items / 捕获 RemoteRuleValidationError` → `DownloadError(str(error))` |
| H1136 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:151 `Downloads.__call__` / 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_BYTES or time.monotonic() > deadline` → `RouterError('远程规则下载超过大小或时间限制')` |
| H1137 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:154 `Downloads.__call__` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError:     raise` |
| H1138 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:155 `Downloads.__call__` / 抛出 | `捕获 RouterError` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H1139 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:156 `Downloads.__call__` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception as error:     raise DownloadError('远程规则下载失败：' + safe_exception_details(error)) from None` |
| H1140 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:158 `Downloads.__call__` / 抛出 | `捕获 Exception` → `DownloadError('远程规则下载失败：' + safe_exception_details(error))` |
| H1141 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:162 `Downloads.__call__` / 捕获后的失败/降级/补偿 | `捕获 RemoteRuleValidationError` → `except RemoteRuleValidationError as error:     raise DownloadError(str(error)) from None` |
| H1142 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:163 `Downloads.__call__` / 抛出 | `捕获 RemoteRuleValidationError` → `DownloadError(str(error))` |
| H1143 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:228 `paused_message` / 诊断 | `not reason` → `f'同步已暂停。{RESUME_HINT}'` |
| H1144 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:236 `needs_baseline` / 校验/诊断降级 | `router.get('type') != 'routeros' or not router.get('auto_rule_sync')` → `False` |
| H1145 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:248 `sync` / 抛出 | `not acquired` → `RouterError('此路由器正在发布或同步，请稍后再试')` |
| H1146 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:252 `sync` / 抛出 | `router is None or router.get('type') != 'routeros' or (not router.get('auto_rule_sync'))` → `RouterError('请先开启此路由器的远程规则自动同步')` |
| H1147 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:256 `sync` / 抛出 | `not baseline` → `RouterError('请先手动预览并成功发布一次，建立同步基准')` |
| H1148 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:261 `sync` / 抛出 | `receipt.get('status') == 'paused'` → `RouterError('上次同步待人工确认，请先手动预览发布以重新建立基准')` |
| H1149 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:268 `sync` / 抛出 | `controls(config, router) != baseline['control']` → `RouterError('来源、规则选择或网关设置尚未发布，请手动预览发布')` |
| H1150 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:270 `sync` / 抛出 | `router.get('ros_policy_state') != baseline.get('policy')` → `RouterError('策略发布记录已变化，请重新手动预览发布')` |
| H1151 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:290 `sync.ensure_current` / 抛出 | `item is None or not item.get('auto_rule_sync') or controls(latest, item) != baseline['control'] or (item.get('ros_policy_state') != router.get('ros_policy_state')) or (item.get('ipv6_routes_state') != router.get('ipv6_routes_state'))` → `RouterError('同步期间配置已变化，停止应用，请重新手动预览发布')` |
| H1152 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:297 `sync.ensure_current` / 抛出 | `source.get('source_type') == 'content' / hashlib.sha256(source.get('content', '').encode()).hexdigest() != records[key]['version']` → `RouterError('本地规则内容在同步期间被修改，停止应用')` |
| H1153 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:304 `sync` / 抛出 | `{item['cidr'] for item in plan['checks']['routes']} != {item['cidr'] for item in desired['proxy_cidrs']}` → `RouterError('目的网段存在冲突或无法完整核对，请手动预览发布')` |
| H1154 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:306 `sync` / 抛出 | `(state.ipv6_fakeip_plan or {}).get('blockers') or (state.ipv6_fakeip_plan or {}).get('ops')` → `RouterError('IPv6 Fake-IP 配置有差异或未能确认，请先手动预览发布')` |
| H1155 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:310 `sync` / 抛出 | `(state.ipv6_dns_plan or {}).get('ops') and (not (state.ipv6_dns_plan or {}).get('blockers'))` → `RouterError('IPv6 DNS 下发有待关闭的差异，请先手动预览发布')` |
| H1156 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:312 `sync` / 抛出 | `(state.ipv6_pd_guard_plan or {}).get('ops') and (not (state.ipv6_pd_guard_plan or {}).get('blockers'))` → `RouterError('IPv6 前缀变化保护有待安装、更新或撤回的差异，请先手动预览发布')` |
| H1157 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:330 `sync` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception as error:     rollback_failed = isinstance(error, RouterRollbackError)     if driver is not None and writes_started:         try:             _save_manifest(profile_id, router, driver)             if receipt.get('status') != 'synced':                 baseline = {**baseline, 'static_resources': static_resources(driver, baseline['desired']), 'policy': copy.deepcopy(getattr(driver, 'ros_policy_manifest', None))}                 receipt['baseline'] = baseline         except Exception:             rollback_failed = True     from backend.utils.url_utils import safe_exception_details     message = str(error) if isinstance(error, RouterError) else safe_exception_details(error)     receipt.update(sta…` |
| H1158 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:342 `sync` / 捕获后的失败/降级/补偿 | `driver is not None and writes_started / 捕获 Exception` → `except Exception:     rollback_failed = True` |
| H1159 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:390 `run_round` / 捕获后的失败/降级/补偿 | `for router in routers_of(config) / 捕获 (ValueError, TypeError)` → `except (ValueError, TypeError):     elapsed = float('inf')` |
| H1160 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:396 `run_round` / 捕获后的失败/降级/补偿 | `for router in routers_of(config) / 捕获 RouterError` → `except RouterError as error:     from backend.utils.logger import get_logger, log_text     get_logger(__name__).info('主路由 %s 本轮远程规则同步已跳过：%s', log_text(router.get('name') or router['id']), error)` |
| H1161 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:406 `start_job` / 抛出 | `not _worker.acquire(blocking=False)` → `RouterError('已有规则同步任务正在运行，请稍后重试')` |
| H1162 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:411 `start_job` / 抛出 | `not acquired` → `RouterError('此路由器正在发布或同步，请稍后重试')` |
| H1163 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:414 `start_job` / 抛出 | `not router or not router.get('auto_rule_sync') or (not receipt.get('baseline'))` → `RouterError('请开启自动同步，并先手动预览发布一次')` |
| H1164 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:416 `start_job` / 抛出 | `receipt.get('status') == 'paused'` → `RouterError(paused_message(receipt))` |
| H1165 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:418 `start_job` / 抛出 | `receipt.get('status') in ('running', 'queued')` → `RouterError('上次同步尚未完成，请稍后重试')` |
| H1166 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:421 `start_job` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception:     _worker.release()     raise` |
| H1167 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:423 `start_job` / 抛出 | `捕获 Exception` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H1168 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:428 `start_job.run` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     receipt = store.read(profile_id, router_id)     if receipt.get('status') == 'queued':         receipt.update(status='failed', message=str(error), checked_at=now())         store.write(profile_id, router_id, receipt)     from backend.utils.logger import get_logger, log_text     get_logger(__name__).warning('主路由 %s 手动触发的远程规则同步未执行：%s', log_text((router or {}).get('name') or router_id), error)` |
| H1169 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:443 `start_job` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception:     _worker.release()     raise` |
| H1170 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:445 `start_job` / 抛出 | `捕获 Exception` → `重新抛出当前错误；同时核对已写入/补偿结果` |
| H1171 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:462 `start_loop.run` / 捕获后的失败/降级/补偿 | `捕获 BlockingIOError` → `except BlockingIOError:     return` |
| H1172 / R011/R022/R023/R024（建立baseline→触发/轮询） | K46:470 `start_loop.run` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception:     logger.exception('Router rule synchronization round failed')` |

### K47 [backend/routers/rule_sync_rules.py](../../backend/routers/rule_sync_rules.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1173 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:36 `classify` / 抛出 | `not isinstance(content, str) or len(content.encode('utf-8')) > MAX_BYTES` → `RouterError('规则内容过大或不是文本（最多 4 MiB）')` |
| H1174 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:39 `classify` / 抛出 | `not text or text.startswith(('<', '{', '['))` → `RouterError('规则内容为空或不是支持的规则格式')` |
| H1175 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:41 `classify` / 抛出 | `behavior not in ('classical', 'ipcidr', 'domain')` → `RouterError('自动同步暂不支持此规则格式')` |
| H1176 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:46 `classify` / 捕获后的失败/降级/补偿 | `any((re.match('^payload\\s*:', line) for line in lines)) / 捕获 yaml.YAMLError` → `except yaml.YAMLError:     raise RouterError('规则 YAML 无法解析') from None` |
| H1177 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:47 `classify` / 抛出 | `any((re.match('^payload\\s*:', line) for line in lines)) / 捕获 yaml.YAMLError` → `RouterError('规则 YAML 无法解析')` |
| H1178 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:49 `classify` / 抛出 | `any((re.match('^payload\\s*:', line) for line in lines)) / not isinstance(document, dict) or not isinstance(document.get('payload'), list)` → `RouterError('规则 YAML 必须包含 payload 列表')` |
| H1179 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:52 `classify` / 抛出 | `len(lines) > MAX_LINES` → `RouterError('规则条目超过 100000 条，停止自动同步')` |
| H1180 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:78 `classify` / 捕获后的失败/降级/补偿 | `not kind and (not is_domain) / 捕获 ValueError` → `except ValueError:     is_domain = bool(re.fullmatch('(?:\\+\\.\|\\*\\.\|\\.)?(?:[\\w-]+\\.)+[\\w-]+\\.?', value)) and (not bool(re.fullmatch('[\\d.]+', value)))` |
| H1181 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:85 `classify` / 抛出 | `kind in {'IP-CIDR', 'IP-CIDR6'} / (kind == 'IP-CIDR6') != (network.version == 6)` → `ValueError()` |
| H1182 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:86 `classify` / 捕获后的失败/降级/补偿 | `kind in {'IP-CIDR', 'IP-CIDR6'} / 捕获 ValueError` → `except ValueError:     network = None` |
| H1183 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:96 `classify` / 捕获后的失败/降级/补偿 | `not kind and (not is_domain) / 捕获 ValueError` → `except ValueError:     network = None` |
| H1184 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:102 `classify` / 抛出 | `isinstance(network, ipaddress.IPv4Network) and network.prefixlen > 0 and network.is_global / len(networks) > MAX_IPS` → `RouterError('有效 IPv4 超过 20000 条，请使用手动发布')` |
| H1185 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:106 `classify` / 抛出 | `network.version == 6 and network.prefixlen > 0 and network.is_global / len(networks6) > MAX_IPS` → `RouterError('有效 IPv6 超过 20000 条，请使用手动发布')` |
| H1186 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:117 `classify` / 抛出 | `not total` → `RouterError('规则不包含任何有效条目')` |
| H1187 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:120 `classify` / 抛出 | `invalid` → `RouterError(f"{detail}：{invalid} 条（第 {'、'.join(map(str, examples))} 行）")` |
| H1188 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:129 `selected_sources` / 抛出 | `len(selected) > 128` → `RouterError('自动同步每台路由器最多支持 128 个规则集')` |
| H1189 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:138 `selected_sources` / 抛出 | `for rule_id in selected / issue` → `RouterError(f'{issue}，请重新手动预览发布')` |
| H1190 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:141 `selected_sources` / 抛出 | `not result` → `RouterError('没有选择需要同步的 IP 规则集')` |
| H1191 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:162 `load_sources` / 抛出 | `not (download is not None and source.get('source_type', 'url') == 'url') / len(raw) > MAX_BYTES` → `RouterError(f'规则集「{name}」超过 4 MiB，停止自动同步')` |
| H1192 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:164 `load_sources` / 捕获后的失败/降级/补偿 | `not (download is not None and source.get('source_type', 'url') == 'url') / 捕获 FileNotFoundError` → `except FileNotFoundError:     fetched[key] = source.get('content', '') if source.get('source_type') == 'content' else ''` |
| H1193 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:166 `load_sources` / 捕获后的失败/降级/补偿 | `not (download is not None and source.get('source_type', 'url') == 'url') / 捕获 UnicodeError` → `except UnicodeError:     raise RouterError(f'规则集「{name}」不是 UTF-8 文本') from None` |
| H1194 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:167 `load_sources` / 抛出 | `not (download is not None and source.get('source_type', 'url') == 'url') / 捕获 UnicodeError` → `RouterError(f'规则集「{name}」不是 UTF-8 文本')` |
| H1195 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:170 `load_sources` / 抛出 | `key not in fetched / size > 16 * 1024 * 1024` → `RouterError('所选规则内容总量超过 16 MiB，停止自动同步')` |
| H1196 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:177 `load_sources` / 捕获后的失败/降级/补偿 | `source.get('source_type', 'url') == 'url' / 捕获 RemoteRuleValidationError` → `except RemoteRuleValidationError as error:     raise RouterError(str(error)) from None` |
| H1197 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:178 `load_sources` / 抛出 | `source.get('source_type', 'url') == 'url' / 捕获 RemoteRuleValidationError` → `RouterError(str(error))` |
| H1198 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:180 `load_sources` / 捕获后的失败/降级/补偿 | `for (rule_id, (item, source)) in sources.items() / 捕获 RouterError` → `except RouterError as error:     raise RouterError(f'规则集「{name}」：{error}') from None` |
| H1199 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:181 `load_sources` / 抛出 | `for (rule_id, (item, source)) in sources.items() / 捕获 RouterError` → `RouterError(f'规则集「{name}」：{error}')` |
| H1200 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:186 `load_sources` / 抛出 | `for (rule_id, (item, source)) in sources.items() / sum((value['ip_count'] for value in records.values())) > MAX_IPS` → `RouterError('所选规则集的有效 IPv4 合计超过 20000 条，请使用手动发布')` |
| H1201 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:188 `load_sources` / 抛出 | `for (rule_id, (item, source)) in sources.items() / sum((value['ip6_count'] for value in records.values())) > MAX_IPS` → `RouterError('所选规则集的有效 IPv6 合计超过 20000 条，请使用手动发布')` |
| H1202 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:207 `check_updates` / 抛出 | `for (rule_id, record) in current.items() / old is None` → `RouterError('规则集选择有变化，请先手动预览发布')` |
| H1203 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:209 `check_updates` / 抛出 | `for (rule_id, record) in current.items() / record['source_type'] != 'url' and record['version'] != old['version']` → `RouterError(f"本地规则集「{record['name']}」已修改，请手动预览发布")` |
| H1204 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:211 `check_updates` / 抛出 | `for (rule_id, record) in current.items() / old['ip_count'] > 0 and record['ip_count'] == 0` → `RouterError(f"规则集「{record['name']}」的有效 IPv4 从 {old['ip_count']} 条变为 0，保留 ROS 原状态，请人工确认")` |
| H1205 / R011/R022/R023/R024（建立baseline→触发/轮询） | K47:214 `check_updates` / 抛出 | `for (rule_id, record) in current.items() / old.get('ip6_count', 0) > 0 and record['ip6_count'] == 0` → `RouterError(f"规则集「{record['name']}」的有效 IPv6 从 {old['ip6_count']} 条变为 0，保留 ROS 原状态，请人工确认")` |

### K48 [backend/routers/rule_sync_store.py](../../backend/routers/rule_sync_store.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1206 / R011/R022/R023/R024（建立baseline→触发/轮询） | K48:28 `read` / 捕获后的失败/降级/补偿 | `捕获 FileNotFoundError` → `except FileNotFoundError:     return {}` |
| H1207 / R011/R022/R023/R024（建立baseline→触发/轮询） | K48:30 `read` / 捕获后的失败/降级/补偿 | `捕获 (ValueError, OSError)` → `except (ValueError, OSError):     return {'status': 'paused', 'message': '同步记录无法读取，请手动预览发布后重新建立基准'}` |
| H1208 / R011/R022/R023/R024（建立baseline→触发/轮询） | K48:31 `read` / 错误/未知结果 | `捕获 (ValueError, OSError)` → `{'status': 'paused', 'message': '同步记录无法读取，请手动预览发布后重新建立基准'}` |
| H1209 / R011/R022/R023/R024（建立baseline→触发/轮询） | K48:68 `job_lock` / 捕获后的失败/降级/补偿 | `捕获 BlockingIOError` → `except BlockingIOError:     yield False     return` |

### K49 [backend/routers/rule_sync_write.py](../../backend/routers/rule_sync_write.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1210 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:25 `static_resources` / 抛出 | `row.get('routing-table', 'main') == 'main' and row.get('dst-address') in wanted and (row.get('gateway') == desired['proxy_gateway']) and is_managed(row.get('comment')) / not entry['editable'] or not entry['enabled']` → `RouterError('受管静态路由含特殊字段或已停用，不能自动维护')` |
| H1211 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:28 `static_resources` / 抛出 | `{item['config'].get('dst-address') for item in result} != wanted` → `RouterError('静态路由尚未完整发布或包含未受管的路由，请先手动预览发布')` |
| H1212 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:30 `static_resources` / 抛出 | `len(result) != len(wanted)` → `RouterError('受管静态路由存在重复，无法建立自动同步基准')` |
| H1213 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:36 `restrict_plan` / 抛出 | `plan['blockers']` → `RouterError('；'.join(plan['blockers']))` |
| H1214 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:44 `restrict_plan` / 抛出 | `merged or stale` → `RouterError(f"路由器上还有旧的逐条 NAT 免转换规则（{'，'.join(detail)}），自动同步不会迁移，请手动预览并发布一次完成迁移")` |
| H1215 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:53 `restrict_plan` / 抛出 | `for stage in plan['stages'] / stage['key'] != 'routes' and stage['ops']` → `RouterError(f"本次更新还需要修改「{stage['title']}」，请手动预览发布")` |
| H1216 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:61 `restrict_plan` / 抛出 | `op['kind'] == 'nat_list_sync' / nat is None` → `RouterError(f"本次更新还需要修改「{STAGE_TITLES['nat_bypass']}」，请手动预览发布")` |
| H1217 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:64 `restrict_plan` / 抛出 | `op['kind'] == 'ros_policy_replace' / op.get('migrations')` → `RouterError('更新需要迁移主表路由，请手动预览发布')` |
| H1218 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:67 `restrict_plan` / 抛出 | `op['kind'] == 'ros_policy_replace' / not previous or not op.get('spec')` → `RouterError('策略路由尚未建立或需要撤销，请手动预览发布')` |
| H1219 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:71 `restrict_plan` / 抛出 | `op['kind'] == 'ros_policy_replace' / old != new` → `RouterError('来源、网关或 NAT 设置有变化，请手动预览发布')` |
| H1220 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:79 `restrict_plan` / 抛出 | `not (op['kind'] == 'route_delete') / not (op['kind'] == 'route_add')` → `RouterError('更新涉及接管或修改已有路由，请手动预览发布')` |
| H1221 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:121 `_apply_delta` / 抛出 | `nat is not None / foreign` → `RouterError(f'其他防火墙规则 {path}/{row_id} 正在引用 {name}，自动同步不会修改，请手动预览发布')` |
| H1222 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:125 `_apply_delta` / 抛出 | `nat is not None / sources != {item['id'] for item in enrolled['src']}` → `RouterError('NAT 免转换源列表与同步基准不一致，请手动预览发布')` |
| H1223 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:134 `_apply_delta` / 抛出 | `any((row.get('routing-table', 'main') == 'main' and row.get('dst-address') in wanted and (row['.id'] not in known_static) for row in live[ROUTES]))` → `RouterError('目的网段已有未登记的静态路由，自动同步不会接管，请手动预览发布')` |
| H1224 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:138 `_apply_delta` / 抛出 | `previous / len(owned) != len(previous['resources'])` → `RouterError('已发布的策略对象缺失，请手动预览发布')` |
| H1225 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:157 `_apply_delta` / 抛出 | `check and previous / not entry` → `RouterError('策略入口的目的地址列表无法识别')` |
| H1226 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:183 `_apply_delta` / 抛出 | `for (path, values) in additions / any((row.get('routing-table', 'main') == 'main' and row.get('dst-address') == values['dst-address'] for row in driver._scoped(ROUTES, values)))` → `RouterError('写入前发现同网段路由，保留手工对象，请重新预览')` |
| H1227 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:245 `_apply_delta` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, RuntimeError)` → `except (RouterError, RuntimeError) as error:     problems = []     if undo6 is not None:         from backend.routers import ipv6_routes         try:             ipv6_routes.restore(driver, undo6)         except RouterError as rollback_error:             problems.append(str(rollback_error))     try:         restore(driver, {'steps': list(reversed(journal)), 'previous': previous, 'retained': (previous or {}).get('resources', []) + retained_nat})     except RouterError as rollback_error:         problems.append(str(rollback_error))     if problems:         raise RouterRollbackError(f"{error}；{'；'.join(problems)}") from error     raise` |
| H1228 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:251 `_apply_delta` / 捕获后的失败/降级/补偿 | `undo6 is not None / 捕获 RouterError` → `except RouterError as rollback_error:     problems.append(str(rollback_error))` |
| H1229 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:252 `_apply_delta` / 阻止/只读原因 | `undo6 is not None / 捕获 RouterError` → `problems.append(str(rollback_error))` |
| H1230 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:256 `_apply_delta` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, RuntimeError) / 捕获 RouterError` → `except RouterError as rollback_error:     problems.append(str(rollback_error))` |
| H1231 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:257 `_apply_delta` / 阻止/只读原因 | `捕获 (RouterError, RuntimeError) / 捕获 RouterError` → `problems.append(str(rollback_error))` |
| H1232 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:259 `_apply_delta` / 抛出 | `捕获 (RouterError, RuntimeError) / problems` → `RouterRollbackError(f"{error}；{'；'.join(problems)}")` |
| H1233 / R011/R022/R023/R024（建立baseline→触发/轮询） | K49:260 `_apply_delta` / 抛出 | `捕获 (RouterError, RuntimeError)` → `重新抛出当前错误；同时核对已写入/补偿结果` |

### K50 [backend/routers/store.py](../../backend/routers/store.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1234 / R007–R010/R015/R024 | K50:56 `normalize` / 抛出 | `not isinstance(payload, dict)` → `RouterValidationError('路由器参数必须是对象')` |
| H1235 / R007–R010/R015/R024 | K50:78 `normalize` / 抛出 | `not isinstance(manage_return, bool)` → `RouterValidationError('自动配置回流必须为布尔值')` |
| H1236 / R007–R010/R015/R024 | K50:83 `normalize` / 抛出 | `type(auto_sync) is not bool or (auto_sync and router['type'] != 'routeros')` → `RouterValidationError('远程规则自动同步仅支持 RouterOS，开关必须为布尔值')` |
| H1237 / R007–R010/R015/R024 | K50:85 `normalize` / 抛出 | `type(interval) is not int or interval not in (15, 30, 60, 180, 360, 720, 1440)` → `RouterValidationError('请选择有效的规则检查间隔')` |
| H1238 / R007–R010/R015/R024 | K50:91 `normalize` / 抛出 | `not isinstance(rule_sets, list) or any((not isinstance(item, str) or not item for item in rule_sets))` → `RouterValidationError('IP 路由规则集必须是规则集 ID 列表')` |
| H1239 / R007–R010/R015/R024 | K50:95 `normalize` / 抛出 | `not isinstance(policy_sets, list) or any((not isinstance(item, str) or not item for item in policy_sets))` → `RouterValidationError('策略路由规则集必须是规则集 ID 列表')` |
| H1240 / R007–R010/R015/R024 | K50:97 `normalize` / 抛出 | `policy_sets and router['type'] != 'routeros'` → `RouterValidationError('按来源策略路由仅支持 RouterOS')` |
| H1241 / R007–R010/R015/R024 | K50:99 `normalize` / 抛出 | `set(policy_sets).intersection(rule_sets)` → `RouterValidationError('同一规则集只能选择静态路由或策略路由中的一种方式')` |
| H1242 / R007–R010/R015/R024 | K50:102 `normalize` / 抛出 | `existing and existing.get('ros_policy_state') and any((router.get(key) != existing.get(key) for key in ('type', 'endpoint')))` → `RouterValidationError('请先清空策略路由选择并发布撤销，再修改路由器类型或管理地址')` |
| H1243 / R007–R010/R015/R024 | K50:106 `normalize` / 捕获后的失败/降级/补偿 | `捕获 SettingsError` → `except SettingsError as error:     raise RouterValidationError(str(error)) from None` |
| H1244 / R007–R010/R015/R024 | K50:107 `normalize` / 抛出 | `捕获 SettingsError` → `RouterValidationError(str(error))` |
| H1245 / R007–R010/R015/R024 | K50:111 `normalize` / 抛出 | `existing and existing.get('dns_guard_state') and any((router.get(key) != existing.get(key) for key in ('type', 'endpoint')))` → `RouterValidationError('请先关闭强制 DNS 并发布撤销，再修改路由器类型或管理地址')` |
| H1246 / R007–R010/R015/R024 | K50:115 `normalize` / 捕获后的失败/降级/补偿 | `捕获 ipv6_pd_guard.SettingsError` → `except ipv6_pd_guard.SettingsError as error:     raise RouterValidationError(str(error)) from None` |
| H1247 / R007–R010/R015/R024 | K50:116 `normalize` / 抛出 | `捕获 ipv6_pd_guard.SettingsError` → `RouterValidationError(str(error))` |
| H1248 / R007–R010/R015/R024 | K50:120 `normalize` / 抛出 | `existing and existing.get('ipv6_pd_guard_state') and any((router.get(key) != existing.get(key) for key in ('type', 'endpoint')))` → `RouterValidationError('请先关闭 IPv6 前缀变化保护并发布撤回，再修改路由器类型或管理地址')` |
| H1249 / R007–R010/R015/R024 | K50:122 `normalize` / 抛出 | `existing and existing.get('ipv6_fakeip_state') and any((router.get(key) != existing.get(key) for key in ('type', 'endpoint')))` → `RouterValidationError('请先关闭 IPv6 Fake-IP 并发布撤销，再修改路由器类型或管理地址')` |
| H1250 / R007–R010/R015/R024 | K50:124 `normalize` / 抛出 | `existing and existing.get('ipv6_routes_state') and any((router.get(key) != existing.get(key) for key in ('type', 'endpoint')))` → `RouterValidationError('请先撤回 IPv6 规则集路由并发布，再修改路由器类型或管理地址')` |
| H1251 / R007–R010/R015/R024 | K50:127 `normalize` / 抛出 | `not router['name']` → `RouterValidationError('请填写名称')` |
| H1252 / R007–R010/R015/R024 | K50:129 `normalize` / 抛出 | `router['type'] not in ROUTER_TYPES` → `RouterValidationError('路由器类型只支持 iKuai 4.x 或 RouterOS')` |
| H1253 / R007–R010/R015/R024 | K50:132 `normalize` / 抛出 | `parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.path not in ('', '/')` → `RouterValidationError('管理地址格式应为 http(s)://主机[:端口]')` |
| H1254 / R007–R010/R015/R024 | K50:134 `normalize` / 抛出 | `not router['username']` → `RouterValidationError('请填写用户名')` |
| H1255 / R007–R010/R015/R024 | K50:136 `normalize` / 抛出 | `not router.get('password')` → `RouterValidationError('请填写密码')` |

### K51 [backend/routes/client_devices.py](../../backend/routes/client_devices.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1256 / R002–R006 | K51:48 `_profile_id` / 错误/未知结果 | `agent_id is not None and by_agent / not agent or agent.get('service_type') != 'oxidns'` → `(None, _response({'success': False, 'message': '这台 OxiDNS Agent 已不存在，请返回 Agent 页刷新。'}, 404))` |
| H1257 / R002–R006 | K51:69 `client_devices` / 错误/未知结果 | `request.method == 'PUT' / not isinstance(body, dict) or set(body) != keys or (not isinstance(body['base_url'], str))` → `_response({'success': False, 'message': '客户端设备请求格式无效。'}, 400)` |
| H1258 / R002–R006 | K51:71 `client_devices` / 错误/未知结果 | `request.method == 'PUT' / body['profile_id'] != profile_id` → `_response({'success': False, 'message': '配置空间已切换，请重新载入。'}, 409)` |
| H1259 / R002–R006 | K51:75 `client_devices` / 捕获后的失败/降级/补偿 | `捕获 DeviceChanged` → `except DeviceChanged as error:     return _response({'success': False, 'message': str(error)}, 409)` |
| H1260 / R002–R006 | K51:76 `client_devices` / 错误/未知结果 | `捕获 DeviceChanged` → `_response({'success': False, 'message': str(error)}, 409)` |
| H1261 / R002–R006 | K51:77 `client_devices` / 捕获后的失败/降级/补偿 | `捕获 OxidnsSettingsError` → `except OxidnsSettingsError as error:     return _response({'success': False, 'message': str(error)}, 400)` |
| H1262 / R002–R006 | K51:78 `client_devices` / 错误/未知结果 | `捕获 OxidnsSettingsError` → `_response({'success': False, 'message': str(error)}, 400)` |
| H1263 / R002–R006 | K51:116 `preview_fixed_ip` / 抛出 | `body['profile_id'] != profile_id` → `RouterConflictError('配置空间已切换，请重新载入。')` |
| H1264 / R002–R006 | K51:118 `preview_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 OxidnsSettingsError` → `except OxidnsSettingsError as error:     return _response({'success': False, 'message': str(error)}, 400)` |
| H1265 / R002–R006 | K51:119 `preview_fixed_ip` / 错误/未知结果 | `捕获 OxidnsSettingsError` → `_response({'success': False, 'message': str(error)}, 400)` |
| H1266 / R002–R006 | K51:120 `preview_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 RouterConflictError` → `except RouterConflictError as error:     return _response({'success': False, 'message': str(error)}, 409)` |
| H1267 / R002–R006 | K51:121 `preview_fixed_ip` / 错误/未知结果 | `捕获 RouterConflictError` → `_response({'success': False, 'message': str(error)}, 409)` |
| H1268 / R002–R006 | K51:122 `preview_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return _response({'success': False, 'message': str(error)}, 502)` |
| H1269 / R002–R006 | K51:123 `preview_fixed_ip` / 错误/未知结果 | `捕获 RouterError` → `_response({'success': False, 'message': str(error)}, 502)` |
| H1270 / R002–R006 | K51:137 `apply_fixed_ip` / 抛出 | `not isinstance(body, dict) or set(body) != {'profile_id', 'token'}` → `OxidnsSettingsError('固定 IP 确认请求格式无效。')` |
| H1271 / R002–R006 | K51:139 `apply_fixed_ip` / 抛出 | `body['profile_id'] != profile_id` → `RouterConflictError('配置空间已切换，请重新载入。')` |
| H1272 / R002–R006 | K51:148 `apply_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 OxidnsSettingsError` → `except OxidnsSettingsError as error:     return _response({'success': False, 'message': str(error)}, 400)` |
| H1273 / R002–R006 | K51:149 `apply_fixed_ip` / 错误/未知结果 | `捕获 OxidnsSettingsError` → `_response({'success': False, 'message': str(error)}, 400)` |
| H1274 / R002–R006 | K51:150 `apply_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 RouterConflictError` → `except RouterConflictError as error:     return _response({'success': False, 'message': str(error)}, 409)` |
| H1275 / R002–R006 | K51:151 `apply_fixed_ip` / 错误/未知结果 | `捕获 RouterConflictError` → `_response({'success': False, 'message': str(error)}, 409)` |
| H1276 / R002–R006 | K51:152 `apply_fixed_ip` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return _response({'success': False, 'message': str(error)}, 502)` |
| H1277 / R002–R006 | K51:153 `apply_fixed_ip` / 错误/未知结果 | `捕获 RouterError` → `_response({'success': False, 'message': str(error)}, 502)` |

### K52 [backend/routes/routers.py](../../backend/routes/routers.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1278 / R011/R012/R018（inspect/release/锁辅助） | K52:45 `_manual_sync_lock.wrapped` / 错误/未知结果 | `not acquired` → `(jsonify(success=False, message='此路由器正在发布或同步，请稍后重试'), 409)` |
| H1279 / R011/R012/R018（inspect/release/锁辅助） | K52:72 `_capture_sync` / 捕获后的失败/降级/补偿 | `捕获 Exception` → `except Exception:     from backend.utils.logger import get_logger     get_logger(__name__).exception('Could not record remote rule synchronization baseline')` |
| H1280 / R011/R012/R018（inspect/release/锁辅助） | K52:85 `_not_found` / 错误/未知结果 | `依赖/文件/事务故障；按函数前置条件` → `(jsonify({'success': False, 'message': '路由器不存在'}), 404)` |
| H1281 / R011/R012/R018（inspect/release/锁辅助） | K52:95 `_agent` / 校验/诊断降级 | `not agent_id` → `None` |
| H1282 / R007/R008 | K52:108 `list_or_create_routers` / 捕获后的失败/降级/补偿 | `捕获 RouterValidationError` → `except RouterValidationError as error:     return (jsonify({'success': False, 'message': str(error)}), 400)` |
| H1283 / R007/R008 | K52:109 `list_or_create_routers` / 错误/未知结果 | `捕获 RouterValidationError` → `(jsonify({'success': False, 'message': str(error)}), 400)` |
| H1284 / R025/R026 | K52:144 `gateway_forwarding` / 错误/未知结果 | `not isinstance(body, dict) or set(body) != {'agent_id'} or (not isinstance(body['agent_id'], str))` → `_no_store(jsonify({'success': False, 'code': 'INVALID_REQUEST', 'message': '请求只能包含 agent_id。'}), 400)` |
| H1285 / R025/R026 | K52:147 `gateway_forwarding` / 错误/未知结果 | `agent_id not in linked` → `_no_store(jsonify({'success': False, 'code': 'NOT_LINKED', 'message': '这台 Agent 不是当前配置空间中任何路由器的代理网关。'}), 404)` |
| H1286 / R025/R026 | K52:151 `gateway_forwarding` / 捕获后的失败/降级/补偿 | `捕获 MonitorError` → `except MonitorError as error:     return _no_store(jsonify({'success': False, 'code': error.code, 'message': error.message}), error.status)` |
| H1287 / R025/R026 | K52:152 `gateway_forwarding` / 错误/未知结果 | `捕获 MonitorError` → `_no_store(jsonify({'success': False, 'code': error.code, 'message': error.message}), error.status)` |
| H1288 / R009/R010 | K52:171 `update_or_delete_router` / 错误/未知结果 | `current != existing` → `(jsonify(success=False, message='路由器设置已变化，请刷新后重试'), 409)` |
| H1289 / R009/R010 | K52:176 `update_or_delete_router` / 错误/未知结果 | `request.method == 'DELETE' / existing.get('ros_policy_state')` → `(jsonify(success=False, message='请先清空策略路由选择并发布撤回，再删除路由器记录'), 409)` |
| H1290 / R009/R010 | K52:178 `update_or_delete_router` / 错误/未知结果 | `request.method == 'DELETE' / existing.get('ipv6_fakeip_state')` → `(jsonify(success=False, message='请先关闭 IPv6 Fake-IP 并发布撤销，再删除路由器记录'), 409)` |
| H1291 / R009/R010 | K52:180 `update_or_delete_router` / 错误/未知结果 | `request.method == 'DELETE' / existing.get('ipv6_routes_state')` → `(jsonify(success=False, message='请先关闭 Mihomo IPv6 或取消相关规则集，并发布撤回 IPv6 规则集路由，再删除路由器记录'), 409)` |
| H1292 / R009/R010 | K52:182 `update_or_delete_router` / 错误/未知结果 | `request.method == 'DELETE' / existing.get('dns_guard_state')` → `(jsonify(success=False, message='请先关闭强制 DNS 并发布撤销，再删除路由器记录'), 409)` |
| H1293 / R009/R010 | K52:184 `update_or_delete_router` / 错误/未知结果 | `request.method == 'DELETE' / existing.get('ipv6_pd_guard_state')` → `(jsonify(success=False, message='请先关闭 IPv6 前缀变化保护并发布撤回，再删除路由器记录'), 409)` |
| H1294 / R009/R010 | K52:191 `update_or_delete_router` / 捕获后的失败/降级/补偿 | `捕获 RouterValidationError` → `except RouterValidationError as error:     return (jsonify({'success': False, 'message': str(error)}), 400)` |
| H1295 / R009/R010 | K52:192 `update_or_delete_router` / 错误/未知结果 | `捕获 RouterValidationError` → `(jsonify({'success': False, 'message': str(error)}), 400)` |
| H1296 / R017 | K52:218 `probe_router` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     probe.update(ok=False, message=str(error))` |
| H1297 / R011/R012/R018（inspect/release/锁辅助） | K52:276 `_inspect_state` / 捕获后的失败/降级/补偿 | `router['type'] == 'routeros' and (policy_sets or router.get('ros_policy_state')) / 捕获 RouterError` → `except RouterError:     owned = []` |
| H1298 / R011/R012/R018（inspect/release/锁辅助） | K52:307 `_inspect_state` / 捕获后的失败/降级/补偿 | `dns_id and (not desired['dns_error']) / 捕获 RouterError` → `except RouterError as error:     desired['dns_error'] = f'无法读取 DHCP DNS 配置：{error}'` |
| H1299 / R011/R012/R018（inspect/release/锁辅助） | K52:350 `_ipv6_host` / 捕获后的失败/降级/补偿 | `捕获 MonitorError` → `except MonitorError as error:     return {'error': str(error)}` |
| H1300 / R011/R012/R018（inspect/release/锁辅助） | K52:361 `_return_cleanup` / 校验/诊断降级 | `agent is None or agent.get('status') != 'online' or (not agent.get('host')) or (not agent.get('port'))` → `None` |
| H1301 / R011/R012/R018（inspect/release/锁辅助） | K52:365 `_return_cleanup` / 校验/诊断降级 | `status.get('status') != 'ok' or not managed.get('spec')` → `None` |
| H1302 / R011/R012/R018（inspect/release/锁辅助） | K52:369 `_return_cleanup` / 校验/诊断降级 | `managed.get('active') is not True and safety.get('ok') is True and (not safety.get('blockers'))` → `None` |
| H1303 / R018 | K52:396 `router_report` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1304 / R018 | K52:397 `router_report` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1305 / R012 | K52:418 `router_plan` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1306 / R012 | K52:419 `router_plan` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1307 / R011 | K52:451 `router_apply` / 错误/未知结果 | `find_router(config, router_id) != router` → `(jsonify({'success': False, 'message': '路由器设置在等待发布期间发生变化，请重新预览'}), 409)` |
| H1308 / R011 | K52:458 `router_apply` / 错误/未知结果 | `plan['blockers']` → `(jsonify({'success': False, 'message': '；'.join(plan['blockers']), 'plan': plan}), 409)` |
| H1309 / R011 | K52:461 `router_apply` / 错误/未知结果 | `not expected or plan['fingerprint'] != expected` → `(jsonify({'success': False, 'message': '路由器、Agent 或期望状态在预览后发生了变化，请重新预览', 'plan': plan}), 409)` |
| H1310 / R011 | K52:505 `router_apply` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     logger.warning('主路由 %s 发布中止：%s', _label(router), error)     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1311 / R011 | K52:507 `router_apply` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1312 / R019 | K52:548 `list_device_routes` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1313 / R019 | K52:549 `list_device_routes` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1314 / R013 | K52:566 `list_device_policy` / 错误/未知结果 | `router['type'] != 'routeros'` → `(jsonify({'success': False, 'message': '策略路由查看仅支持 RouterOS'}), 400)` |
| H1315 / R013 | K52:573 `list_device_policy` / 抛出 | `current != router` → `RouterConflictError('路由器设置已变化，请刷新后重试')` |
| H1316 / R013 | K52:585 `list_device_policy` / 捕获后的失败/降级/补偿 | `捕获 RouterConflictError` → `except RouterConflictError as error:     return (jsonify({'success': False, 'message': str(error)}), 409)` |
| H1317 / R013 | K52:586 `list_device_policy` / 错误/未知结果 | `捕获 RouterConflictError` → `(jsonify({'success': False, 'message': str(error)}), 409)` |
| H1318 / R013 | K52:587 `list_device_policy` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1319 / R013 | K52:588 `list_device_policy` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1320 / R014/R015 | K52:610 `policy_settings` / 错误/未知结果 | `router.get('type') != 'routeros'` → `(jsonify(success=False, message='策略路由设置仅支持 RouterOS'), 400)` |
| H1321 / R014/R015 | K52:619 `policy_settings` / 错误/未知结果 | `request.method == 'PUT' / not isinstance(body, dict) or set(body) not in ({'revision', 'router', 'base_url'}, {'revision', 'names_revision', 'router', 'policies', 'base_url'}) or (not isinstance(body['router'], dict)) or (set(body['router']) != set(FIELDS)) or (not isinstance(body['base_url'], str))` → `(jsonify(success=False, message='策略路由设置格式无效'), 400)` |
| H1322 / R014/R015 | K52:623 `policy_settings` / 抛出 | `request.method == 'PUT' / find_router(get_config(profile_id), router_id) != router` → `RouterConflictError('路由器设置已变化，请刷新后重试')` |
| H1323 / R014/R015 | K52:638 `policy_settings` / 捕获后的失败/降级/补偿 | `捕获 RouterConflictError` → `except RouterConflictError as error:     return (jsonify(success=False, message=str(error)), 409)` |
| H1324 / R014/R015 | K52:639 `policy_settings` / 错误/未知结果 | `捕获 RouterConflictError` → `(jsonify(success=False, message=str(error)), 409)` |
| H1325 / R014/R015 | K52:640 `policy_settings` / 捕获后的失败/降级/补偿 | `捕获 (RouterValidationError, OxidnsSettingsError, ProfileRepositoryError)` → `except (RouterValidationError, OxidnsSettingsError, ProfileRepositoryError) as error:     return (jsonify(success=False, message=str(error)), 400)` |
| H1326 / R014/R015 | K52:641 `policy_settings` / 错误/未知结果 | `捕获 (RouterValidationError, OxidnsSettingsError, ProfileRepositoryError)` → `(jsonify(success=False, message=str(error)), 400)` |
| H1327 / R016 | K52:661 `preview_policy_settings` / 错误/未知结果 | `not isinstance(body, dict) or not isinstance(body.get('targets'), list) or any((not isinstance(value, str) for value in body['targets'])) or (len(set(body['targets'])) != len(body['targets'])) or (not isinstance(body.get('base_url', ''), str))` → `(jsonify(success=False, message='发布预览参数无效'), 400)` |
| H1328 / R016 | K52:669 `preview_policy_settings` / 错误/未知结果 | `router.get('type') != 'routeros'` → `(jsonify(success=False, message='策略路由设置仅支持 RouterOS'), 400)` |
| H1329 / R016 | K52:671 `preview_policy_settings` / 错误/未知结果 | `body.get('revision') != draft_revision(config, router)` → `(jsonify(success=False, message='配置已更新，请重新载入后预览。'), 409)` |
| H1330 / R016 | K52:676 `preview_policy_settings` / 错误/未知结果 | `any((target not in peers for target in body['targets']))` → `(jsonify(success=False, message='OxiDNS 发布目标或绑定已变化，请重新载入。'), 409)` |
| H1331 / R016 | K52:687 `preview_policy_settings` / 错误/未知结果 | `repository.config_revision(profile_id) != config_revision` → `(jsonify(success=False, message='生成预览期间配置已更新，请重新预览。'), 409)` |
| H1332 / R016 | K52:696 `preview_policy_settings` / 捕获后的失败/降级/补偿 | `捕获 (RouterError, ValueError)` → `except (RouterError, ValueError) as error:     return (jsonify(success=False, message=str(error)), 400)` |
| H1333 / R016 | K52:697 `preview_policy_settings` / 错误/未知结果 | `捕获 (RouterError, ValueError)` → `(jsonify(success=False, message=str(error)), 400)` |
| H1334 / R011/R012/R018（inspect/release/锁辅助） | K52:722 `_device_route_change` / 捕获后的失败/降级/补偿 | `捕获 RouteEditValidationError` → `except RouteEditValidationError as error:     return (jsonify({'success': False, 'message': str(error)}), 400)` |
| H1335 / R011/R012/R018（inspect/release/锁辅助） | K52:723 `_device_route_change` / 错误/未知结果 | `捕获 RouteEditValidationError` → `(jsonify({'success': False, 'message': str(error)}), 400)` |
| H1336 / R011/R012/R018（inspect/release/锁辅助） | K52:730 `_device_route_change` / 抛出 | `current != router` → `RouterConflictError('路由器设置已变化，请刷新后重试')` |
| H1337 / R011/R012/R018（inspect/release/锁辅助） | K52:738 `_device_route_change` / 错误/未知结果 | `not isinstance(fingerprint, str) or fingerprint != plan['fingerprint']` → `(jsonify({'success': False, 'message': '操作内容或路由状态已变化，请重新预览后确认'}), 409)` |
| H1338 / R011/R012/R018（inspect/release/锁辅助） | K52:743 `_device_route_change` / 捕获后的失败/降级/补偿 | `捕获 RouteEditValidationError` → `except RouteEditValidationError as error:     return (jsonify({'success': False, 'message': str(error)}), 400)` |
| H1339 / R011/R012/R018（inspect/release/锁辅助） | K52:744 `_device_route_change` / 错误/未知结果 | `捕获 RouteEditValidationError` → `(jsonify({'success': False, 'message': str(error)}), 400)` |
| H1340 / R011/R012/R018（inspect/release/锁辅助） | K52:745 `_device_route_change` / 捕获后的失败/降级/补偿 | `捕获 RouterConflictError` → `except RouterConflictError as error:     return (jsonify({'success': False, 'message': str(error)}), 409)` |
| H1341 / R011/R012/R018（inspect/release/锁辅助） | K52:746 `_device_route_change` / 错误/未知结果 | `捕获 RouterConflictError` → `(jsonify({'success': False, 'message': str(error)}), 409)` |
| H1342 / R011/R012/R018（inspect/release/锁辅助） | K52:747 `_device_route_change` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     if apply:         logger.warning('主路由 %s 静态路由变更失败：%s', _label(router), error)     return (jsonify({'success': False, 'message': str(error)}), 502)` |
| H1343 / R011/R012/R018（inspect/release/锁辅助） | K52:750 `_device_route_change` / 错误/未知结果 | `捕获 RouterError` → `(jsonify({'success': False, 'message': str(error)}), 502)` |
| H1344 / R011/R012/R018（inspect/release/锁辅助） | K52:764 `_return_routing` / 捕获后的失败/降级/补偿 | `捕获 (RuntimeError, requests.RequestException)` → `except (RuntimeError, requests.RequestException) as error:     return {'status': 'unavailable', 'message': str(error) or error.__class__.__name__}` |
| H1345 / R022/R023/R024 | K52:782 `router_rule_sync` / 错误/未知结果 | `router.get('type') != 'routeros'` → `(jsonify(success=False, message='远程规则自动同步仅支持 RouterOS'), 400)` |
| H1346 / R022/R023/R024 | K52:787 `router_rule_sync` / 错误/未知结果 | `request.method == 'PUT' / not isinstance(body, dict) or set(body) != {'auto_rule_sync', 'rule_sync_interval'}` → `(jsonify(success=False, message='请提供同步开关和检查间隔'), 400)` |
| H1347 / R022/R023/R024 | K52:791 `router_rule_sync` / 抛出 | `request.method == 'PUT' / find_router(get_config(profile_id), router_id) != router` → `RouterConflictError('路由器设置已变化，请刷新后重试')` |
| H1348 / R022/R023/R024 | K52:795 `router_rule_sync.mutate` / 抛出 | `request.method == 'PUT' / current is None` → `RouterValidationError('路由器已不存在')` |
| H1349 / R022/R023/R024 | K52:804 `router_rule_sync` / 错误/未知结果 | `request.method == 'POST' / not router.get('auto_rule_sync') or not receipt.get('baseline')` → `(jsonify(success=False, message='请开启自动同步，并先手动预览发布一次'), 409)` |
| H1350 / R022/R023/R024 | K52:807 `router_rule_sync` / 错误/未知结果 | `request.method == 'POST' / receipt.get('status') == 'paused'` → `(jsonify(success=False, message=paused_message(receipt)), 409)` |
| H1351 / R022/R023/R024 | K52:809 `router_rule_sync` / 错误/未知结果 | `request.method == 'POST' / receipt.get('status') in ('running', 'queued')` → `(jsonify(success=False, message=receipt.get('message') or '同步尚未就绪'), 409)` |
| H1352 / R022/R023/R024 | K52:817 `router_rule_sync` / 捕获后的失败/降级/补偿 | `捕获 RouterValidationError` → `except RouterValidationError as error:     return (jsonify(success=False, message=str(error)), 400)` |
| H1353 / R022/R023/R024 | K52:818 `router_rule_sync` / 错误/未知结果 | `捕获 RouterValidationError` → `(jsonify(success=False, message=str(error)), 400)` |
| H1354 / R022/R023/R024 | K52:819 `router_rule_sync` / 捕获后的失败/降级/补偿 | `捕获 RouterError` → `except RouterError as error:     return (jsonify(success=False, message=str(error)), 409)` |
| H1355 / R022/R023/R024 | K52:820 `router_rule_sync` / 错误/未知结果 | `捕获 RouterError` → `(jsonify(success=False, message=str(error)), 409)` |

### K53 [backend/routes/upstream_router_pending.py](../../backend/routes/upstream_router_pending.py#L1)

| H / 公开驱动 | 精确源码位置 / 类型 | 错误输入、故障或诊断guard → 结果 |
|---|---|---|
| H1356 / R001 | K53:24 `upstream_router_pending` / 捕获后的失败/降级/补偿 | `捕获 FutureTimeout` → `except FutureTimeout:     (response, status) = (jsonify({'success': False, 'message': '读取上游路由状态超时，请稍后重试。'}), 504)` |

## 实验网 L5：RouterOS CHR 与 iKuai 虚拟机

下表L5-A～I是**同一L5批次的行为子项**，不是新增L批次。主矩阵每个相关入口逐条引用；fake先完成权限/校验/请求/表状态/回滚比较，以下实机结论保留部分覆盖。由协调人按WORKFLOW安排获准的隔离CHR/iKuai VM，并配实验Mihomo/OxiDNS/独立客户端；从.32驱动，本步骤不访问或创建实验网。

| L5子项 | 必须真设备验证的行为 | fake无法证明什么 / 怎么验 | Python依据 |
|---|---|---|---|
| L5-A REST/API与手工静态路由 | **[O6]** 连同[review-r04.md](review-r04.md)“L5 lab items”1–9（HTTP/HTTPS自签证书、账号组缺api/rest-api/test/执行权限、100条/32KiB控制台批与64kB上限、200项OR查询与500 ID remove的处理顺序、NAT place-before与invalid settle、拔管理链路丢回复、默认字段、DHCP DNS option 6各层、非Latin-1凭据）一起执行。CHR REST版本、HTTP/HTTPS服务、read/write/api/rest-api/test权限、/execute权限与单条fallback；iKuai4 session/表单/只读字段、分页/编码、大body；静态路由add/edit/delete/反向恢复 | fake不会证明firmware能解析/接受同一payload或CLI、真实ID分配/默认字段/异步active收敛。真VM配置有高级/动态/VRF/递归/ECMP/foreign行，检验只读保护、丢ack/外部写补偿、重启保持、实际选路；抓管理HTTP与完整导出/路由表 | [routeros.py:83](../../backend/routers/routeros.py#L83)、[ikuai.py:202](../../backend/routers/ikuai.py#L202)、[routeros_routes.py:221](../../backend/routers/routeros_routes.py#L221) |
| L5-B 静态/来源policy/NAT/回流 | ROS7.22+ lookup order、Mangle/table/mark/私有chain、fasttrack/raw/VRF、同端口转发、NAT exemption与legacy→列表迁移；iKuai static→端口分流、IP groups及真实ref_count；Mihomo DIRECT/proxy回流 | fake表一致不能证明包路径无回环、未被前序规则/fasttrack影响、源IP未被NAT、隐藏ACL引用或gid恢复正常。独立客户端与Mihomo自身DIRECT对同合成目的分流、双向抓包、切换/回滚/故障注入，foreign规则不变 | [ros_policy.py:97](../../backend/routers/ros_policy.py#L97)、[routeros_nat.py:1](../../backend/routers/routeros_nat.py#L1)、[ikuai_groups.py:1](../../backend/routers/ikuai_groups.py#L1)、[release.py:29](../../backend/routers/release.py#L29) |
| L5-C DHCP DNS/HA VIP/强制DNS | ROS/iKuai各单scope DHCP DNS、option6/RADIUS/静态override；OxiDNS/HA VIP续租下发；ROS hairpin/dstnat53、DoT/DoQ853、Netwatch tcp health及15s故障确认/恢复conntrack | fake不能证明真实客户端续租/RDNSS优先、TCP/UDP DNS解析和回包、真实DNS故障/主备切换无中断、conntrack特定流刷新或豁免未误伤。用独立客户端硬编码resolver、续租、切VIP与停DNS、长UDP会话/旁路白名单逐项抓包，回滚/重启再验 | [dhcp_dns.py:110](../../backend/routers/dhcp_dns.py#L110)、[dns_guard.py:246](../../backend/routers/dns_guard.py#L246)、[ikuai.py:475](../../backend/routers/ikuai.py#L475) |
| L5-D IPv6 routes/RA/包路径 | CHR+Mihomo IPv6 Fake-IP ULA/TUN、global规则路由、link-local%接口下一跳、邻居发现；同LAN专用/共享ND DNS通告、IPv6同端口filter提示 | fake不会证明真实ND、L2/L3 scope、RA/RDNSS/客户端缓存、prefix剩余lifetime造成的变化、IPv6实际无泄漏/无回环。独立客户端按published链路发TCP/UDP/IPv6 DNS，切dns.ipv6/关top ipv6/撤回并抓包；iKuai只IPv4，不能误称实现IPv6转发 | [ipv6_fakeip.py:1](../../backend/routers/ipv6_fakeip.py#L1)、[ipv6_routes.py:1](../../backend/routers/ipv6_routes.py#L1)、[ipv6_dns.py:148](../../backend/routers/ipv6_dns.py#L148)、[ipv6_policy.py:1](../../backend/routers/ipv6_policy.py#L1) |
| L5-E PD guard事件脚本 | CHR DHCPv6-PD真实重编号、watch script/poll scheduler/10×1s hook、旧前缀preferred=0且hold=3600脚本清理/重新成为当前、切LAN/pool、禁用清理、borrowed/shared内容保护 | fake只能比模板和模拟事件输出，无法证明RouterOS脚本语法、policy权限、hook上下文、并发scheduler以及实际客户端地址迁移。实验PD服务器反复换/回退prefix，观察配置preferred=0/valid-lifetime=10m、实际RA重发与客户端寿命，和hold=3600脚本删除的分别作用、重启与撤回并发，不借改时钟跳过真实结论 | [ipv6_pd_guard.py:66](../../backend/routers/ipv6_pd_guard.py#L66)、[ipv6_pd_guard.py:338](../../backend/routers/ipv6_pd_guard.py#L338) |
| L5-F 客户端发现/在线证据 | CHR ARP状态、DHCP lease freshness/renew margin、ARP ping→ICMP与缺test、静态IP/手机休眠；iKuai monitor_lanip真实2分钟流量、lease/LAN、跨router同IP冲突 | fake数值一致不证明某设备真的在线/离线，尤其静默host/邻居cache和抓取时序。真客户端进出网络/睡眠/防火墙禁ICMP、lease renew、失联某router，查recent/unknown与offline分界、源/MAC归属、慢大表预算 | [ros_clients.py:99](../../backend/routers/ros_clients.py#L99)、[ikuai_clients.py:106](../../backend/routers/ikuai_clients.py#L106) |
| L5-G 固定IP/多设备补偿 | CHR make-static、无lease新预留、iKuai dhcp_static、真实scope/IP/MAC冲突及删除/重建；动态转换失败移除静态后的实际续租影响 | fake不能证明DHCP守护进程采用预留、client lease改变、设备固件hostname/runtime字段或恢复新ID实际可用。两vendor真实客户端预览→固定→续租→删除→续租，批次中断/失联/DB拒绝触发逆序补偿，确认foreign静态/DNS override不被接管 | [routeros_fixed_ip.py:237](../../backend/routers/routeros_fixed_ip.py#L237)、[ikuai_fixed_ip.py:165](../../backend/routers/ikuai_fixed_ip.py#L165)、[client_fixed_ip.py:200](../../backend/common/client_fixed_ip.py#L200) |
| L5-H 关联Linux网关转发 | router→Mihomo实验Linux guest实际IPv4 forwarding、持久sysctl、systemd/OpenRC hook、foreign/symlink保护、重启/撤网关仍保持 | fake报告current/persisted不能证明OS、启动顺序或路由器流量生效。与agents L4联测，但本域需确认仅当前profile的linked proxy gateway授权、相同Agent多router不会多写，router客户端双向连接和重启前后抓包 | [routes/routers.py:132](../../backend/routes/routers.py#L132)、[agents/linux_gateway.py:652](../../backend/agents/linux_gateway.py#L652) |
| L5-I 自动规则delta/规模与故障 | CHR真实4MiB/20k IP/多batch更新、NAT列表/来源policy/IPv6 enrolled routes；固件长列表/批量语法、临时invalid settle/内核active残留、断线回滚和ID重建 | fake不能证明真实API字节/负载限额、秒级runtime收敛、丢ack后的firmware现场与流量不中断。先manual baseline，再以隔离规则源变IP/domain、停止响应/限权/外部改，核receipt paused/failed/synced和真实路由、流量；规则同步不允许顺手改未登记保护 | [rule_sync.py:241](../../backend/routers/rule_sync.py#L241)、[rule_sync_write.py:98](../../backend/routers/rule_sync_write.py#L98)、[routeros_batch.py:139](../../backend/routers/routeros_batch.py#L139) |

## 前端与跨域入口核对

- 前端routerApi按当前profile明确header；plan→确认fingerprint→apply；单路由GET revision→plan→apply、workspace旧/新格式、rule-sync202轮询必须分别走真公开入口。来源 [api/index.ts:350](../../frontend/src/api/index.ts#L350)、[api/index.ts:371](../../frontend/src/api/index.ts#L371)、[api/index.ts:617](../../frontend/src/api/index.ts#L617)。
- 上游提醒用`/agents/<id>/upstream-routers?profile=...&refresh=1`，belongs routers而非agents领域；默认可跨profile聚合，显式scope不得落回全局。来源 [api/upstreamRouterPending.ts:46](../../frontend/src/api/upstreamRouterPending.ts#L46)。
- RouterOS IPv6 readiness私有诊断以及router_split/rule_ip_presence还被agents/config公开入口使用；领域helper的分支列H/G23/T23，但不新增本域路由分母。跨域Agent回流、Mihomo主机IPv6与OS转发所需fake合约已列D27/D29/D30，不读Go实现或调用其私有helper。
- 两vendor只有有限REST/API，不提供本领域SSE/WebSocket；安装/下载和Agent升级也不在这26条。生成文本测试要保留plan空script、嵌套source/up/down/hook/fix_command及出站execute scripts各自形态，不能只看plan.script为空便声称没有脚本要比较。


## 汇总与部分覆盖清单

- **路由总数26**（routes.routers20、routes.client_devices5、routes.upstream_router_pending1），主矩阵method+模板去重与FILES/routes.tsv完全一致。
- **路由场景756**：成功S 155、失败/降级F 428、边界B 173，共享A/P已按每条展开。
- 另 **H源码分支/诊断配方1356、D设备协议30、G生成形态组24、T长序列24**，合计 **2190个清单条目**。H含审计N/A候选，D/G/T需按子变体参数化；不是已实现/已通过数，不宣称独立HTTP请求数或不重复的分支覆盖率。
- **[O6]** 审核新增：逐路由补充88条（列出的63条+R002–R026各一条F90），横切X01–X14共14项，长序列T25–T27共3条，fixture ADDR/SECRET/RECEIPT共3项，fake合约3项；合计 **2301个清单条目**。明细见文末O6 review。
- **15条路由标需L5**；纯验证/草稿/HTTP协议不因此推迟。单轮请求/响应仅部分覆盖 **21条**，逐条如下；其余入口也必须按写后比较码验DB事务/文件/隔离，不能只比200。

| R | method / route | 部分覆盖原因 / 补测 | L5必须部分 |
|---|---|---|---|
| R001 | `GET /api/agents/<agent_id>/upstream-routers` | 并行读取、signature缓存/unknown不复用与通知；T01/D03，真实device状态需L5。 | L5-A,L5-B,L5-C |
| R003 | `PUT /api/client-devices` | previous值与原子name/policy写、移除设备不删预留；T02/DB观察。 | —；此入口只验草稿/存储/并发 |
| R004 | `GET /api/client-devices/discovery` | 多router并行/timeout/cache/presence证据，T03/T04；fake不能证明在线。 | L5-F |
| R005 | `POST /api/client-devices/fixed-ip/apply` | 签名guard→设备多项写→DB→逆序补偿与新ID，T05–T07；需真续租。 | L5-F,L5-G |
| R006 | `POST /api/client-devices/fixed-ip/preview` | 完整设备fresh snapshot/签名与apply连续链，T05；preview本身不证明固IP有效。 | L5-F,L5-G |
| R008 | `POST /api/routers` | same endpoint锁、DB expected比较、manifest保护与竞争，T08。 | —；此入口只验草稿/存储/并发 |
| R009 | `DELETE /api/routers/<router_id>` | same endpoint锁、DB expected比较、manifest保护与竞争，T08。 | —；此入口只验草稿/存储/并发 |
| R010 | `PUT /api/routers/<router_id>` | same endpoint锁、DB expected比较、manifest保护与竞争，T08。 | —；此入口只验草稿/存储/并发 |
| R011 | `POST /api/routers/<router_id>/apply` | 多stage/多设备/批次、unknown写和补偿、manifest与baseline，T09–T14；实流/脚本L5。 | L5-A,L5-B,L5-C,L5-D,L5-E |
| R012 | `POST /api/routers/<router_id>/plan` | 只读证据/通知和generated文本，后续apply才证明可接受；G01–G23/T09–T14。 | L5-A,L5-B,L5-C,L5-D,L5-E |
| R013 | `GET /api/routers/<router_id>/policy` | 归属/foreign/动态字段、lookup与前后并发，T11；不能证明真包路径。 | L5-B |
| R015 | `PUT /api/routers/<router_id>/policy-settings` | 旧新格式及双revision原子事务/并发，不直接发布；T02/T08。 | —；此入口只验草稿/存储/并发 |
| R016 | `POST /api/routers/<router_id>/policy-settings/preview` | 生成期间config_revision与targets绑定、DNS YAML和router plan，T11；不是已发布。 | L5-B,L5-C |
| R017 | `POST /api/routers/<router_id>/probe` | 有读权限不等于可写，last_probe持久与真实服务/版本兼容，T15。 | L5-A |
| R018 | `GET /api/routers/<router_id>/report` | NAT/回流/IPv6/guard多表证据、unknown/notifications；T11/T12/T14；不证明流量。 | L5-A,L5-B,L5-C,L5-D,L5-E |
| R019 | `GET /api/routers/<router_id>/routes` | GET→preview→apply、外部写/丢ack/保守补偿，T16，L5-A真正选路。 | L5-A |
| R020 | `POST /api/routers/<router_id>/routes/apply` | GET→preview→apply、外部写/丢ack/保守补偿，T16，L5-A真正选路。 | L5-A |
| R023 | `POST /api/routers/<router_id>/rule-sync` | 202后台worker/timer、receipt与cache/manifest落盘、重启恢复和允许delta范围，T17–T19。 | L5-B,L5-I |
| R024 | `PUT /api/routers/<router_id>/rule-sync` | 202后台worker/timer、receipt与cache/manifest落盘、重启恢复和允许delta范围，T17–T19。 | —；此入口只验草稿/存储/并发 |
| R025 | `GET /api/routers/gateway-forwarding` | 多router授权refs与按版本status检查/POST→回读，T20；重启持久和真实流量依L5-H。 | L5-H |
| R026 | `POST /api/routers/gateway-forwarding` | 多router授权refs与按版本status检查/POST→回读，T20；重启持久和真实流量依L5-H。 | L5-H |

本步骤未跑测试/构建/lint/dev server，未连接真设备或实验网，未修改Go或Python实现。所有错误/差异后续交移植agent处理；清单先由Opus审可达性、遗漏与协议细节，再进入Step B。

## O6 review

Reviewer: Opus (O6, 2026-11-17). Basis: Python source on this branch (`backend/`, `docker/nginx.conf`), `routes.tsv`, `FILES.md`, `frontend/src/api` and `review-r04.md`. No Go implementation code was read and nothing was run; this is a document review.

### Verdict

**Approved with additions.** Step B may start from this matrix as amended, with three preconditions that must exist before any device case runs: one stateful fake set per server (X13), Python started as `python -m backend.app` so the routers rule-sync loop exists (X07, with RECEIPT seeds to drive it), and a way to hold a router's job lock on each server while a second request is sent (X04, a slow fake reply is enough). Without them the lock, auto-sync and compensation cases fail or pass for the wrong reason.

The device-protocol part is the strongest section of the three domain matrices so far. D01–D30 name every write path, what is read back, how it is batched and what proves ownership before a delete; the batch limits match the constants in `routeros_batch.py`, `ros_address_lists.py`, `ros_policy_write.py` and `ikuai.py`; the stage order in T09 matches `plan.py`; and the fake-versus-L5 split is drawn in the right places. The H index is accurate: of 1356 rows, all point inside the cited function (18 sit in nested closures and are labelled with the outer function, which is fine), 30 randomly sampled rows were read by hand and all match, and only two rows had a corrupted literal.

The misses are of the same kind the config and agents reviews found: outside the handlers. They are in the Flask shell (profile before auth, error handlers, HEAD), in the order in which locks are taken relative to validation, in the process start mode, and in what every profile write does to other domains.

### Most important misses (now added)

1. **Locks are taken before validation** (X04; R009 F91, R010 F91, R015 F91, R020 F91, R024 F91). `_manual_sync_lock` and `_router_settings_lock` acquire the non-blocking job lock before the 404, router-type and body checks. A malformed PUT, or a rule-sync PUT on an iKuai router, returns 409 while another operation runs on the same connection, not 400. A lock file is created even for a 404. The in-process operation lock is blocking with no limit, so R013 and R021 wait for a running apply.
2. **Every profile write has cross-domain effects** (X08, "写入后比较代码"). Each write bumps `profile._revision`. That changes `config_revision`, so Agents in the profile show "待更新", gateway installation sessions report `config_changed`, onboarding and IPv6 verification revisions go stale, and R016 returns 409 if a probe lands during generation. With auto-backup on, it also schedules a WebDAV upload. This includes a **failed probe**, because it still writes `last_probe`. Auto-sync rewrites shared rule caches. Profile delete and config import are blocked by routers that have manifests, and those config cases had no router seeds.
3. **Profile before auth, and R001 is global** (X02; P1/P2 and R001 F08/F09 corrected). `routers` and `client_devices` resolve the profile before login. R001 sits on the global `agents` blueprint: it ignores the header, sends no `X-ConfigFlow-Profile`, and only validates `?profile=` after auth. With `?agent=`, the header is still validated first, and the response header names the request's profile, not the Agent's.
4. **Auto-sync timing depends on how Python is started** (X07, T26; T17 corrected). The loop exists only under `python -m backend.app`. Its first round runs 30 s after start. A `running` receipt turns `paused` only when a round or sync runs, and until then POST returns 409 with the stale message. An orphaned `queued` receipt runs normally. Without seeded receipts nothing is due for at least 15 minutes.
5. **Download limits pause, they do not fail** (D28/T19 corrected). Exceeding 4 MiB or 30 s raises `RouterError`, which pauses the sync. HTTP, decode and validation errors raise `DownloadError`, which fails it so it is retried. Downloads go through `rule_fetch_proxy` when one is set.
6. **iKuai treats more write failures as uncertain than RouterOS does** (D18 corrected). Only HTTP 200 with JSON `code≠0` is a definite refusal. Any non-200 (including 4xx), non-JSON, an empty 200 (an oversized body) or a transport error leaves an unconfirmed undo, which is found again by its unique tagname. The fake must not apply a write that it answers with 1008. Further fake-fidelity requirements were added: duplicate tagname → 3001, `Result:10000` on every reply, `{}` empty address fields, and RouterOS `invalid` settle / `not enough permissions (9)`.
7. **Error shapes and Python crashes** (X03, X12; R009/R010/R011/R015/R016 corrected). `ProfileInUse` goes through the errorhandler as 409 with `usages`. Handlers that catch `ValueError` or `ProfileRepositoryError` turn the same error into 400. R011 F20 is a 409 with no result, not a 502, and is not reachable over HTTP. Inside `execute`, a non-RouterError (for example a malformed device reply) is an HTML 500 with **no rollback and no `last_release`**. Go fails closed there (R04 F1/F7), so these cases go to the O12 ledger instead of counting as passes or failures.
8. **Lax parsing in `store.normalize`** (X05). Covers `str()` coercion of name/type/endpoint/username, `verify_tls` off only for a literal `false`, falsy rule-set lists, unchecked ports, and `http://[::1` → HTML 500. With a wrong Content-Type, R011 still reads the whole device before returning 409.
9. **HEAD has side effects** (X06). It actively pings the LAN (R004), writes notifications (R018, R001), takes the operation lock (R013) and calls Agents (R025). R002's HEAD ignores `?agent=`.
10. **Thread-context profile fallback in R001** (T27). `_return_cleanup` calls `resolve_profile_id()` in a worker thread, so it always gets the default profile. With a cloned profile that has the same router ID, R001 shows a return-routing removal that R012 suppresses for the same router.
11. **Route overlaps** (X09). `GET /api/agents/gateway-installations/{session_id}` and `GET /api/agents/{agent_id}/upstream-routers` overlap with neither more specific, so Go's `ServeMux` panics when both are registered. `PUT/DELETE /api/routers/gateway-forwarding` reach R010/R009.
12. **Long requests behind Nginx** (X10). The 300 s apply timeout equals Nginx's `proxy_read_timeout`. Python finishes after a client disconnect or Nginx 504, so the final state must match an uninterrupted run, including the DB and notification steps after the driver (R04 F3 fixed only the driver).
13. **Redaction does not cover router passwords** (X01, R011 B90). `last_release.message` goes through `event_message` (URL hiding, password, 1024-byte truncation), but `result.log` summaries in the same response do not. Device text that echoes the password is returned verbatim; any change goes to O9/O12.
14. **Corrupted source facts.** H0078/H0079 had the example network in a Chinese message replaced by `[合成LAN fixture]`; the literal `192.168.50.0/24` is restored.

### Classes from earlier O6 reviews, checked here

| Class | Applies? | Where |
|---|---|---|
| Profile resolution before auth | yes, 25 routes; R001 is the global exception | X02, F90 |
| Global JSON token redaction | yes, but router passwords are outside it | X01 |
| Startup / first-request background loops | yes: routers rule-sync loop only under `python -m backend.app`; Agent rule-sync starts on the first authenticated router request | X07, config X08, agents X08 |
| Nginx in front of Flask | yes: 300 s apply timeout, 1 MiB body | X10 |
| Lax Python parsing | yes | X05 |
| HEAD runs handlers | yes, with device and Agent I/O | X06 |
| Route-pattern overlaps | yes | X09 |
| Cross-domain side effects (WebDAV backup, notifications, tasks) | yes, plus config_revision consumers, rule caches and profile delete/import | X08 |
| Source text rewritten by placeholder substitution | yes, one message (2 rows) | H0078/H0079 |

### Counts

| Category | Count |
|---|---|
| Per-route additions (`S/F/B 9x`) | 88: 63 listed (S 8, F 18, B 37) + F90 on R002–R026 (25) |
| Cross-cutting items | 14 (X01–X14) |
| Long sequences | 3 (T25–T27) |
| Fixtures | 3 (ADDR, SECRET, RECEIPT) |
| Fake-contract items | 3 (iKuai, RouterOS, rule download) |
| **Additions total** | **111** |

Corrections: **16**, touching 44 places (25 of them links).

| What | Count |
|---|---|
| Status and comparison rules: title; Python start mode; JSON-value body comparison, crash class, lock-file glob | 2 |
| Fixture P1/P2: profile before auth, R001 global | 1 |
| Write-code introduction: exact revision/backup rule | 1 |
| Route cells: R001 F08/F09; R009 F18 and R010 F15 (`usages`); R011 F20; R015 F18; R016 F16 | 5 |
| Device/sequence: D18 iKuai certainty; D28/T19 download limits; T17 running→paused timing | 3 |
| L5-A: run review-r04 L5 items 1–9 | 1 |
| H0078/H0079 literal restored (1 entry, 2 rows) | 1 |
| Links: 11 anchors off by 1–4 lines (20 occurrences) and 5 wrong rows in the iKuai field table | 2 |

Totals after review: 2190 original entries + 111 additions = **2301** named entries (design counts, not request counts).

### Link spot check

- All 173 distinct non-H links were printed and read. 16 were wrong: 11 pointed 1–4 lines above the cited definition (`common/client_devices.py:39`, `config_repository.py:1006`, `profile_context.py:52`, `ipv6_dns.py:147`, `ipv6_fakeip.py:29`, `pending.py:164`, `route_editor.py:117`, `routeros_routes.py:220`, `rule_sync_store.py:49`, `store.py:53`, `ikuai.py:66`), and the five rows of the iKuai write-field table cited the neighbouring constant (`ROUTE_FIELDS` is L40, `NAT_FIELDS` L41, `POLICY_FIELDS` L45, `DHCP_FIELDS` L62, `DHCP_STATIC_FIELDS` L65). All 16 are fixed.
- All 1356 H rows were checked by script: the cited line lies inside the named function (18 are inside nested closures). 30 rows sampled with a fixed seed were read by hand; all 30 point at the cited guard and carry its message. A script compared every Chinese literal in the H rows with the source file. Apart from multi-line string concatenations, it found only H0078/H0079.
- All 121 distinct links on added or edited lines were printed and read; 11 that pointed at a neighbouring line were corrected before commit.

### Fake versus lab: what the fake proves, and what only L5 can

The fake proves the protocol: methods, paths and bodies; batch boundaries; read-back and ownership checks; and the compensation decisions after an injected refusal, a lost reply, an external write or ID reuse. It proves these only if the fake reproduces the firmware facts listed in "[O6] 设备协议与fake合约补充". Without those facts the ownership-by-tagname and settle branches are never taken. The fake cannot prove that firmware accepts the payload or script, how the device assigns IDs and fills defaults, how RouterOS processes the IDs in a `remove`, that a Netwatch or PD script really runs, or that traffic and DHCP renewals behave as intended. Those stay in L5-A…I. L5-A now also includes the nine R04 lab items.

### Not testable by HTTP differential, and where it goes instead

| Item | Goes to |
|---|---|
| Firmware acceptance of payloads and console scripts, ID assignment, defaults, `remove` processing order, 64 kB console limit, lost replies on a real link, non-Latin-1 credentials | **L5-A/B** (with review-r04 L5 items 1–9) |
| Real packet paths (policy, NAT, return routing, fasttrack/raw), DHCP renewals and RDNSS, Netwatch failover and conntrack flush, PD renumbering scripts, real discovery presence, real auto-sync scale | **L5-B…I** |
| Nginx-plus-Flask composite: 300 s apply timeout, 1 MiB body, disconnect after Nginx 504 (X10) | container test on .32 with the production `nginx.conf` in front of Python; Go's choice per difference → **O12** |
| `kill -9` mid-release, lock files across processes, scheduler flock (T25) | fault injection on .32 against each server separately; Go unit tests for lock semantics |
| Python crash class and R04 accepted differences (X12) | **O12** ledger, case by case |
| Router passwords echoed in responses, unauthenticated profile-existence probing (X01, X02) | **O9** security review (behaviour must match Python unless O12 records a change) |
| Waitress thread exhaustion while requests block on the operation lock (R013/R021 waiting on a long apply) | **O10**, soak in **L6** |
| Receipt digests and `client_fixed_ips` carried over at the switch; `JWT_SECRET_KEY` continuity for fixed-IP tokens (X14) | **O14** switch plan, **L6** rehearsal; production-data differential must include `router-rule-sync/` |
| Profile delete and config import blocked by routers with manifests (X08e) | config Step B, cases R028 and R117, using this matrix's router seeds |

### Notes for Step B

- Start Python with `python -m backend.app` under waitress. Give both servers the same `TZ`, `JWT_SECRET_KEY`, `ADMIN_*`, `CONFIGFLOW_HTTP_THREADS` and working directory, separate `DATA_DIR`s, and RECEIPT seeds with identical content.
- Run one fake set per server, in separate network namespaces at identical addresses: RouterOS, iKuai, Mihomo/OxiDNS Agents, the rule URL server and its proxy, and WebDAV. Keep fake Agents heartbeating both servers (agents X07).
- Hold a job lock by giving one request a slow fake reply. Send the second request only after the first has reached the device (check the fake's transcript), not after a fixed sleep.
- Compare device request bodies as JSON values; exclude `*.lock` and `.scheduler.lock` by exact glob; never mask `fingerprint`, `revision` or `token`. Map IDs by bijection.
- Keep `auto_backup` off except in the X08 fixture. With it on, compare the WebDAV PUT count after the 1 s debounce.
- Before reporting a difference that involves timing, check X07 (which round ran, and when) and X04 (who held the lock).
