# Agents 领域黑盒差分覆盖清单（Step A，已经 O6 Opus 审核，见文末“O6 review”）

只设计覆盖，不写测试、不运行服务。本文件基于 `FILES.md` agents 归属的69个Python文件和 `routes.tsv`，不读取 `server/internal/**` Go实现。`PROGRESS-agents.md`只用模块表识别A01–A24及HR边界。清单审过后才能进入Step B。

## 范围、计数和比较约定

- **120条 method+路由模板**，主矩阵每条一行。动态 `<operation>`、`<path:resource>` 的所有合法形态分别列为S场景；显式profile别名不能借默认入口的成功代替。`profiles/<id>/agents/rebind`由config路由文件拥有，已在config清单，不重复计入120；面板→Agent私有协议请求也不加入面板路由分母。
- `Rnnn-Sxx/Fxx/Bxx`是独立输入/请求序列配方；F包含拒绝与错误降级，200里的部分失败/unknown不算成功完成。`Hnnn`逐行列Python错误/诊断/校验guard，作为主表的必展开分支，不是已完成测试。循环字段、布尔guard各条件、协议/架构和下述协议失败变体均在Step B逐项参数化；清单数不等于最终HTTP请求数。不可通过HTTP触发的纯内部参数、模板约定、OS启动故障只能标审计哨兵或隔离故障注入，不能伪造Go helper调用充覆盖。
- 比status、JSON键/类型/值、中文消息、Content-Type/Disposition、Cache-Control、X-ConfigFlow-Profile、Referrer-Policy、nosniff及实际出站HTTP transcript。错误消息可能有凭据时以合成secret验证精确脱敏。Python内部英文exception措辞只比拒绝、status和结果形态，不复制实现怪癖。
- YAML/Compose按解析后的结构与有序规则比；Surge文本按section/key/规则顺序；Shell命令比quote后的参数、命令顺序、guard和副作用，不能仅删空白或hash整个脚本。ZIP/tar解包比成员/role/内容/大小/hash/引用闭包。固定模板和脚本短链快照要逐字节核对，不能用“语义相同”放过凭据、引号或注册目标的改变。
- UUID/token/time做受约束双射：先验证格式/唯一性/TTL/时序，再在两端响应、DB、文件路径和出站头中一贯关联；revision、ETag、内容hash各端验证与其内容自洽，继续用各端返回guard发下一请求。禁止mask整个content、events、errors、metrics、logs、manifest或未知状态。
- **[O6]** Python端一律用`python -m backend.app`（waitress，`LOG_LEVEL`≠DEBUG）启动，不用Flask test client或调试服务器；X15那部分再在前面加生产镜像的nginx.conf。详见X08/X15。
- 所有Step B运行只在测试服务器；真实网络栈、假服务namespace、容器和脚本执行只用原生.32。初始seed两份独立DATA_DIR，无真实家庭设备地址/密钥/快照。需要非回环LAN IPv4校验的API，以192.0.2.0/24和198.51.100.0/24在**隔离namespace的lo**上分配合成地址并绑定fake，网络出口关闭；不为绕过校验修改服务代码。设备实际接管只由实验网L1–L4另验。

## Fixture 与共享身份/输入场景

| 代码 | 输入、变体与预期 | Python来源 |
|---|---|---|
| SEED | alpha默认、beta非默认两个profile；Agent m-a/o-a/o-b分别Mihomo/OxiDNS主备，另有Surge、MosDNS、旧protocol1、Docker、禁用与低版本Agent。每个有不同合成token、host、port；profile header故意与Agent绑定相冲突。不得由前端活动profile覆盖Agent绑定 | [manager.py:171](../../backend/agents/manager.py#L171)、[routes/agents.py:1729](../../backend/routes/agents.py#L1729) |
| REG | `{name:"agent-a",host:"192.0.2.10",port:18080,service_type:"mihomo",profile_id:"alpha",deployment_method:"shell",version:"1.8.60-go"}`；普通Agent显示名另测中文emoji，但installer名称受限。首次响应id/token作后续认证；重注册必须携此token，新token不能任意注入 | [routes/agents.py:547](../../backend/routes/agents.py#L547)、[manager.py:171](../../backend/agents/manager.py#L171) |
| A1–A7 | 开启管理员认证时每条管理API分别无header、错签名JWT、过期JWT、空claims、Bearer双空格、8193字符token、只有Agent token。比较before_request的401与消息；query config_token不能越权。认证关闭环境重放合法成功体，公开路由不误加JWT。**[O6]** 消息有两套：before_request拒绝用`请先登录后再操作。`/`登录已失效，请重新登录。`；被白名单放过后再进`require_auth`的（R079管理员分支、公开精确路径落到`/<agent_id>`）用`缺少有效的登录凭据…`/`登录已过期…`+`token_expired`/`登录凭据无效…`+`token_invalid`。见X03 | [routes/auth.py:35](../../backend/routes/auth.py#L35)、[common/auth.py:131](../../backend/common/auth.py#L131) |
| C1–C7 | Agent自身API：无Bearer、错误token、其他Agent token、管理员JWT、双空格Bearer、8193字符Bearer、非ASCII凭据，均按Agent认证边界拒绝；禁用管理员认证也不能绕过。config拉取例外只收query token；upgrade GET允许自己的token或管理员JWT，分别测，不套“一律拒JWT” | [routes/agents.py:653](../../backend/routes/agents.py#L653)、[routes/agents.py:885](../../backend/routes/agents.py#L885)、[routes/agents.py:1197](../../backend/routes/agents.py#L1197) |
| P1–P5 | profile不存在404、id格式非法400；URL显式profile优先query/header；普通Agent API忽略sidebar选择而用绑定profile；IPv6要求header/query/body明确且一致，Linux网关则header优先query（现有Python行为）。写入并发改绑必须在锁内再核对。**[O6]** 更正：`agents`蓝图是全局的，`/api/agents/**`完全忽略header/query中的profile（不存在也不404，响应也没有`X-ConfigFlow-Profile`头），只有IPv6、Linux网关操作、Docker HA guide在handler里显式读；`custom_config`（R104–R110）和带路径id的`profiles`（R111–R120）在**登录鉴权之前**解析profile，未登录也先得404/400。见X02 | [profile_context.py:53](../../backend/common/profile_context.py#L53)、[routes/ipv6_readiness.py:30](../../backend/routes/ipv6_readiness.py#L30)、[routes/linux_gateway_agents.py:52](../../backend/routes/linux_gateway_agents.py#L52) |
| V | 涉及body字段逐测缺失、空字符串/空白、null、空数组/对象、错误类型、未知字段、重复ID、上限与上限+1、中文emoji、引号/反斜杠/美元/反引号/换行；实际是否拒绝由Python和H guard决定。Content-Length缺失/谎报、chunked超限分开测 | [routes/agents.py:668](../../backend/routes/agents.py#L668)、[routes/gateway_installations.py:11](../../backend/routes/gateway_installations.py#L11) |
| HEARTBEAT | `{version:"1.8.60-go",config_version:"abcd1234",service_status:"running",host:"192.0.2.10",system_metrics:{collected_at:"2026-01-01T00:00:00Z",cpu:{usage_percent:25,core_count:2},memory:{total:1024,used:256,available:768,used_percent:25},disk:{total:4096,used:1024,free:3072,used_percent:25},network:{bytes_sent:100,bytes_recv:200,speed_sent:0,speed_recv:0}}}`；{}也合法 | [routes/agents.py:45](../../backend/routes/agents.py#L45) |
| TRAFFIC | domain report `{schema:1,status:"running",mihomo_version:"synthetic",window_start:"2026-01-01T00:00:00Z",window_end:"2026-01-01T00:05:00Z",ip_only_conns:0,dropped:0,items:[]}`；增量history `{schema:2,usage:[],domains:[]}`，非空项按clean_report字段附计数/整点hour/chain。顶层坏report拒绝；域名单项坏数据只dropped，history坏项整批拒绝 | [traffic_store.py:124](../../backend/agents/traffic_store.py#L124)、[traffic_history.py:160](../../backend/agents/traffic_history.py#L160) |
| NETWORK | `{interface:"eth0",address:"192.0.2.10",lan_cidr:"192.0.2.0/24",upstream_gateway:"192.0.2.1"}`；HA peer=192.0.2.11、VIP=192.0.2.53，VRID固定合成值。所有IP仅fake namespace可达，profile规则/代理节点完全合成 | [agents/linux_gateway.py:224](../../backend/agents/linux_gateway.py#L224)、[oxidns_ha.py:87](../../backend/agents/oxidns_ha.py#L87) |

认证例外：register和install-script/core-install-script/network-probe/binary/短链公开；安装enroll和script自行校验限时capability；心跳、两个流量上报、config pull和upgrade接口自行授权；linux-gateway rule/source provider用JWT或config_token，但internal rule-proxy capability不得作为公开凭据。实际白名单匹配、路径非法时认证优先级也对比，不能因为handler没有装饰器就假定匿名开放。

## 写入后比较代码

每次写操作先后拍全库，而非只GET：`configflow.sqlite3` 的 `sections(scope,owner,position)`、`fields(scope,owner,field,position,value,revision)`、`meta(key,value)`。JSON value结构比较、未知字段不丢、其他Agent/profile行不变、revision随实际变更一致。随机store/commit标识做双射但顺序、事务完整性不忽略。配置提交若auto_backup开启还可能安排合并WebDAV备份；runtime-only心跳/监控不能触发配置备份（**[O6]** 精确规则：auto_backup开启时，Agent非运行字段的任何变化都会安排备份，包括注册、开关、rule_sync/dns_follow/oxidns_ha及其state；见X10）。来源 [config_store.py:38](../../backend/common/config_store.py#L38)、[config_repository.py:1010](../../backend/common/config_repository.py#L1010)。

| 代码 | DB、文件、任务和Agent副作用比较 |
|---|---|
| DB0 | 无配置写入与推送；只允许对应读取所需cache/日志，若是带后台工作的GET改用专用代码。 |
| REG / DELETE | system.agents相应记录及创建、绑定、版本、开关；注册token仅首次返回，重注册保留历史/开关；删除同时按Python清runtime、metrics、traffic/probes/regions，不能顺便删除其他设备。拒绝注册还会日志/通知；各密钥不入公开日志。来源 [manager.py:171](../../backend/agents/manager.py#L171)、[routes/agents.py:538](../../backend/routes/agents.py#L538)。。**[O6]** 更正“重注册保留开关”：重注册整条重建记录，`enabled`重置为true，缺version写`1.0.0`，只保留`id/token/created_at/config_version/deployments/latest_deployment/latest_upgrade/domain_discovery_enabled/traffic_history_*/config_revision/pushed_at/applied_content/dns_follow*/rule_sync/oxidns_ha*/ipv6_verification*/gateway_installation_id`，其余字段（如`logging_config`）丢失；R002成功体里的token被全局钩子换成`[REDACTED]`（X01） |
| HEARTBEAT / ADDRESS | 心跳status/time/metrics主要内存；version/config_version变化才持久化，metrics追加内存历史；host不直接写成心跳字段，而先capability探活再后台更新；手动地址先写设备再写面板。比较后台确认、节流、通知和路由器提示，不能只比200。来源 [manager.py:430](../../backend/agents/manager.py#L430)、[address_follow.py:49](../../backend/agents/address_follow.py#L49)。。**[O6]** 心跳version变化会提交配置但不触发WebDAV备份（version属运行字段）；在线只看内存里的最后心跳≤120秒（X07），fake必须两端各自心跳 |
| METRICS / TRAFFIC / TRAFFIC-HISTORY | `metrics/<id>_metrics.jsonl`，内存4000点/24h和flush时序；`metrics/traffic_history.sqlite3` 的 traffic_usage/traffic_targets行，schema1→2转换与保留30/90/180/365天；域名报告traffic/<id>.json与probes/regions目录，增量重复上报按Python累加不伪造去重。历史缓存读取不写system.agents的system_metrics。来源 [metrics_history.py:1](../../backend/agents/metrics_history.py#L1)、[traffic_history.py:61](../../backend/agents/traffic_history.py#L61)。 |
| LOG / LIFECYCLE / PANEL / CACHE / RULE-UPDATE | fake出站method/path/header/body/timeout/次数与设备fake状态，path/source优先级、日志与错误脱敏；logging_config仅上游success才写本地；缓存清理的部分flushed不能当回滚；core规则热更任务需要poll到完成；Mihomo runtime mode/selector不误写profile草稿。来源 [manager.py:1343](../../backend/agents/manager.py#L1343)、[mihomo_panel.py:318](../../backend/agents/mihomo_panel.py#L318)、[oxidns_cache.py:79](../../backend/agents/oxidns_cache.py#L79)。 |
| DEPLOY | system.agents.deployments/latest_deployment、prepared_content/applied_content/config_version/config_revision/pushed_at与events；`.agent-rule-sync` receipt+bundle，preparation锁、HA payload，订阅/规则/provider缓存；manifest/profile/service/digest与提交同一快照，主配置/resource/prepared摘要不泄漏。failed/rollback_failed/unknown不得更新成功基准或解除错误锁；上传前必须先持久submitted，查询的终态失败仍HTTP200。来源 [manager.py:491](../../backend/agents/manager.py#L491)、[manager.py:556](../../backend/agents/manager.py#L556)、[rule_sync_store.py:16](../../backend/agents/rule_sync_store.py#L16)。 |
| PREVIEW / PULL / SYNC | PREVIEW `.generated-previews/<id>/record.json`/bundle.tar.gz与file/download字节，TTL3600/24份/512MiB；最终生成会落订阅缓存并对目标final-preview，但不创部署任务。PULL content/md5/version与绑定profile匹配，不自动push。SYNC计算后台缓存与checking/ready/unknown、分页version、secret片段/差异项正确，不能保存新applied基准。来源 [generated_preview.py:67](../../backend/agents/generated_preview.py#L67)、[routes/agents.py:1554](../../backend/routes/agents.py#L1554)、[content_sync.py:1](../../backend/agents/content_sync.py#L1)。 |
| UPGRADE | latest_upgrade的update_id/protocol/artifacts/phase/events/error/timestamps，正确绑定agent和架构；stages顺序、600秒后确认门禁；下载包SHA/size从允许binary实际计算，提交结果未知保留原ID，不重复POST新任务。来源 [upgrades.py:175](../../backend/agents/upgrades.py#L175)、[upgrades.py:244](../../backend/agents/upgrades.py#L244)。 |
| MONITOR | `dns-monitor/<hash>.json` 的generation/target/baseline/carried/pending，`metrics/oxidns_rankings.sqlite3` 的period/row/slot；reset只改面板统计起点不删核心query记录；发布checkpoint失败不阻止发布。HTTP结果中的整数/计数/来源字段及回退cache必须单独验证。来源 [oxidns_dashboard_state.py:111](../../backend/agents/oxidns_dashboard_state.py#L111)、[oxidns_publication_stats.py:91](../../backend/agents/oxidns_publication_stats.py#L91)。 |
| RULESYNC / FOLLOW / HA / HA-ENV | agents中rule_sync.settings/state、dns_follow binding/state/job、oxidns_ha pair/state/publication；`.agent-rule-sync` snapshot/receipt/bundle与`.oxidns-ha-publications` payload/bundle，task-status按完成事件记一次；native DNS草稿与被固定上游不得无授权覆盖。HA须比两个成员状态、VIP handoff顺序和leftover，不把任一端200当双端一致。HA-ENV只读证据不可修改pair。来源 [rule_sync.py:125](../../backend/agents/rule_sync.py#L125)、[dns_follow.py:208](../../backend/agents/dns_follow.py#L208)、[oxidns_ha_publish.py:176](../../backend/agents/oxidns_ha_publish.py#L176)。 |
| SCRIPT / COMPOSE / BINARY / INSTALL-SNAPSHOT | 只读render不安装、不写设备；快照写`install-commands/snapshots.sqlite3`的token/script/expires_at/generator，生成指纹变化返回保护脚本；路径权限与24h过期、上限、并发INSERT。binary只允许amd64/arm64/armv7固定文件；不把源码模板内容或实际生产密钥提交清单。来源 [install_command_store.py:42](../../backend/agents/install_command_store.py#L42)、[routes/agents.py:329](../../backend/routes/agents.py#L329)。 |
| HA-INSTALL / INSTALL | `docker-ha-installations`或`gateway-installations`的session/index/token/registered/agent_ids/network/phase/deployments；签发不创Agent，enroll才创/复用身份；24h凭据与首次注册限制、幂等、原身份不可覆盖、短链快照。INSTALL create显式use_as_server_domain可能先写system，即使后续验证失败也须比真实副作用；publish202背景依次推进两个核心，失败保存任务ID供继续。来源 [docker_ha_enrollment.py:29](../../backend/agents/docker_ha_enrollment.py#L29)、[gateway_installation.py:155](../../backend/agents/gateway_installation.py#L155)。 |
| GATEWAY-DRAFT / ONBOARD / GATEWAY-PROVIDER / GATEWAY-HOST | profile.mihomo/oxidns原子改动、restore_point及preview_token/revision，preview不落草稿；只保存不操作主机。HOST记录fake实际network/persist/TUN操作与回读，应用必须是Agent绑定profile；provider输出和规则引用闭包。只读取原转发状态不能证明真网络已生效。来源 [routes/linux_gateway.py:88](../../backend/routes/linux_gateway.py#L88)、[routes/gateway_onboarding.py:38](../../backend/routes/gateway_onboarding.py#L38)、[agents/linux_gateway.py:350](../../backend/agents/linux_gateway.py#L350)。 |
| IPV6 | `.ipv6-readiness/<hash>.json`检查缓存与agents.ipv6_verifications/settings、绑定profile/config_revision/check_token/client身份与published快照；prepare有实际OS变更的可能，超时不能宣布无效果；verify多请求后台报告、实际客户端证据关联不能由fake断言实流。来源 [ipv6_readiness.py:362](../../backend/agents/ipv6_readiness.py#L362)、[ipv6_verification.py:419](../../backend/agents/ipv6_verification.py#L419)。 |
| NOTES / POLICY / RULE-ACTION / DISCOVERY-DATA | 绑定profile device_notes/OxiDNS device_policies/rule-library.content/缓存/history；previous_name、notes_revision、策略revision与Agent绑定在锁内核对；只保存draft，无Controller写入或自动push；清采集仅删traffic/probes/regions不改开关。来源 [routes/client_tools.py:38](../../backend/routes/client_tools.py#L38)、[routes/agents.py:2326](../../backend/routes/agents.py#L2326)。 |

## 每条路由覆盖矩阵

A/C/P来源见共享表；其余单项L链接指确切分支，未单列L的以来源列完整handler范围及H逐guard明细为准。HR列仅标**fake无法证明**、必须实验网验证的部分；同条路由的输入校验/协议/状态机/DB仍先用fake完整比较。

| ID / method / route | 成功S | 失败/降级F | 边界B | 写后比较 / 长流程 | HR实验网 | Python |
|---|---|---|---|---|---|---|
| **R001** `GET /api/agents` | **S01** 默认列表包含content_sync与更新状态<br>**S02** summary=1轻量列表<br>**S03** history=0省略历史 | **F01–07** A1–7<br>**F08** content comparison profile不可用→unknown而非崩溃 [L241](../../backend/routes/agents.py#L241) | **B01** 空列表<br>**B02** 中文emoji名字<br>**B03** history省略的字段不能误输出null | SYNC；SYNC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L955–989](../../backend/routes/agents.py#L955) |
| **R002** `POST /api/agents` | **S01** 最小{name,host,port,service_type}新Agent（**[O6]** 成功体`data.token`是`"[REDACTED]"`，见S90）<br>**S02** 指定profile_id=beta | **F01–07** A1–7<br>**F08** 坏JSON→500 [L989](../../backend/routes/agents.py#L989)<br>**F09** 非对象→500 [L989](../../backend/routes/agents.py#L989)<br>**F10** AgentDeploymentConflict→409 [L987](../../backend/routes/agents.py#L987)<br>**F11** 重复身份不带原token→500 [L989](../../backend/routes/agents.py#L989)<br>**F12** 不存在profile→repository错误 | **B01** 省略name走Unnamed Agent<br>**B02** 中文emoji名称<br>**B03** 扩展字段不得注入token/deployments | REG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L955–989](../../backend/routes/agents.py#L955) |
| **R003** `DELETE /api/agents/<agent_id>` | **S01** 无在途工作Agent删除并清附属数据 | **F01–07** A1–7<br>**F08** 不存在→404 [L1027](../../backend/routes/agents.py#L1027)<br>**F09** 升级忙→409 [L1029](../../backend/routes/agents.py#L1029)<br>**F10** DNS-follow忙→409 [L1029](../../backend/routes/agents.py#L1029)<br>**F11** HA启用/忙→409 [L1029](../../backend/routes/agents.py#L1029)<br>**F12** 文件/存储异常→500 [L1031](../../backend/routes/agents.py#L1031) | **B01** 重复删404<br>**B02** 不删除其他Agent历史<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DELETE；REG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L994–1031](../../backend/routes/agents.py#L994) |
| **R004** `GET /api/agents/<agent_id>` | **S01** 已有Agent含历史与content_sync | **F01–07** A1–7<br>**F08** Agent不存在→404 [L1003](../../backend/routes/agents.py#L1003) | **B01** 中文emoji<br>**B02** profile_unavailable→unknown<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | SYNC；SYNC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L994–1031](../../backend/routes/agents.py#L994) |
| **R005** `PUT /api/agents/<agent_id>` | **S01** 改name/host/port<br>**S02** 改profile绑定<br>**S03** enabled开关 | **F01–07** A1–7<br>**F08** 不存在→404 [L1012](../../backend/routes/agents.py#L1012)<br>**F09** 坏JSON/非对象→500 [L1016](../../backend/routes/agents.py#L1016)<br>**F10** 升级忙→409 [L1014](../../backend/routes/agents.py#L1014)<br>**F11** DNS-follow忙→409 [L1014](../../backend/routes/agents.py#L1014)<br>**F12** HA已启/忙→409 [L1014](../../backend/routes/agents.py#L1014)<br>**F13** 未完成deployment改绑定→409 [L1014](../../backend/routes/agents.py#L1014) | **B01** 空对象幂等<br>**B02** 未知字段按白名单过滤<br>**B03** 并发两不同字段保留<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | REG；REG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L994–1031](../../backend/routes/agents.py#L994) |
| **R006** `GET /api/agents/<agent_id>/address` | **S01** 新Agent可写地址<br>**S02** 旧接口404回退<br>**S03** 环境变量模式只读 | **F01–07** A1–7<br>**F08** 不存在→404 [L1042](../../backend/routes/agents.py#L1042)<br>**F09** 连接失败展示不可管理状态 | **B01** 当前旧地址不可达<br>**B02** 中文名称<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | ADDRESS；ADDRESS | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1036–1047](../../backend/routes/agents.py#L1036) |
| **R007** `PUT /api/agents/<agent_id>/address` | **S01** 新Agent写入host并更新面板<br>**S02** 旧Agent仅改面板<br>**S03** 相同host幂等 | **F01–07** A1–7<br>**F08** 不存在→404 [L1042](../../backend/routes/agents.py#L1042)<br>**F09** 缺/非法host→400<br>**F10** HA启用或忙→409<br>**F11** 同名同host冲突→409<br>**F12** 连接失败→502<br>**F13** 凭据不匹配→502<br>**F14** env模式→409<br>**F15** 设备拒绝host→400/502<br>**F16** 设备成功后DB冲突→409 | **B01** 文档IPv4边界<br>**B02** IPv6/域名/回环/多播按usable拒绝<br>**B03** 先设备再面板的次序<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | ADDRESS；ADDRESS | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1036–1047](../../backend/routes/agents.py#L1036) |
| **R008** `GET /api/agents/<agent_id>/client-notes` | **S01** mihomo备注<br>**S02** oxidns备注 | **F01–07** A1–7<br>**F08** 无效Agent/服务→400 [L19](../../backend/routes/client_tools.py#L19)<br>**F09** profile不存在→repository | **B01** 中文emoji<br>**B02** 空备注集合<br>**B03** header beta不改变alpha绑定<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | NOTES | —（无主机应用；跨域真实性仍见协议限制） | [client_tools.py:L40–63](../../backend/routes/client_tools.py#L40) |
| **R009** `PUT /api/agents/<agent_id>/client-notes` | **S01** 新增备注<br>**S02** 更新备注<br>**S03** 空name删除 | **F01–07** A1–7<br>**F08** body字段集合不精确→400 [L47](../../backend/routes/client_tools.py#L47)<br>**F09** profile_id不匹配→409 [L49](../../backend/routes/client_tools.py#L49)<br>**F10** IP/range/name无效→400 [L63](../../backend/routes/client_tools.py#L63)<br>**F11** previous_name过时→409 [L56](../../backend/routes/client_tools.py#L56)<br>**F12** 写入前改绑→409 [L25](../../backend/routes/client_tools.py#L25) | **B01** 80字符/控制符<br>**B02** 同IP规范化冲突<br>**B03** 同时两次修改只一方成功<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | NOTES | —（无主机应用；跨域真实性仍见协议限制） | [client_tools.py:L40–63](../../backend/routes/client_tools.py#L40) |
| **R010** `GET /api/agents/<agent_id>/config` | **S01** token匹配本Agent的mihomo<br>**S02** mosdns<br>**S03** oxidns<br>**S04** surge配置 | **F01** Q1仅管理员JWT而无query token401<br>**F02** Q2 query token为其他Agent凭据401<br>**F03** 缺/空/超长token→401 [L892](../../backend/routes/agents.py#L892)<br>**F04** 错token→401 [L898](../../backend/routes/agents.py#L898)<br>**F05** 其他Agent token→401 [L898](../../backend/routes/agents.py#L898)<br>**F06** 仅Bearer不代替query→401 [L892](../../backend/routes/agents.py#L892)<br>**F07** 策略引用无效→400 [L914](../../backend/routes/agents.py#L914)<br>**F08** 绑定profile失效→409 [L916](../../backend/routes/agents.py#L916)<br>**F09** 其他生成错误→500 [L918](../../backend/routes/agents.py#L918) | **B01** 中文emoji配置<br>**B02** md5与content自洽且version前缀<br>**B03** 空规则集合<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | PULL；PULL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L886–918](../../backend/routes/agents.py#L886) |
| **R011** `GET /api/agents/<agent_id>/config/logging` | **S01** enabled=true<br>**S02** enabled=false | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** upstream非200→500（sources404除外）<br>**F10** 坏JSON/错型结果→500<br>**F11** 连接失败/超时→500 | **B01** 空列表与旧版本<br>**B02** P4 sidebar/query另一profile不能越过Agent绑定 | LOG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1144–1151](../../backend/routes/agents.py#L1144) |
| **R012** `POST /api/agents/<agent_id>/config/logging` | **S01** enabled=true<br>**S02** enabled=false<br>**S03** {}默认true | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** 上游失败不写本地→500<br>**F10** 上游JSON错→500<br>**F11** 坏请求JSON→500 [L1167](../../backend/routes/agents.py#L1167) | **B01** enabled=null/数字按Python透传，不凭空加bool校验<br>**B02** 成功才写本地enabled<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | LOG+REG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1156–1167](../../backend/routes/agents.py#L1156) |
| **R013** `GET /api/agents/<agent_id>/deployments/<deployment_id>` | **S01** preparing/uploading/ready/activating状态<br>**S02** 成功终态200<br>**S03** failed/rolled_back/rollback_failed终态仍200<br>**S04** lost upload response后对账 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** deployment不存在→404<br>**F10** profile/service归属改变→409<br>**F11** 暂时网络错误保留pending<br>**F12** 上传在途404不误判最终失败<br>**F13** 无上传在途且Agent无journal→failed记录 | **B01** 旧终态回执不可覆盖新latest<br>**B02** 过时事件单调合并<br>**B03** 超过30条历史裁剪<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | DEPLOY；DEPLOY | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L1981–1985](../../backend/routes/agents.py#L1981) |
| **R014** `POST /api/agents/<agent_id>/deployments/<deployment_id>/activate` | **S01** ready任务激活<br>**S02** OxiDNS已成功任务重复请求<br>**S03** 冻结generation候选激活 | **F01–07** A1–7<br>**F08** Agent/deployment不存在→404<br>**F09** profile/service错→409<br>**F10** 未ready/已终态/升级或HA锁冲突→409<br>**F11** validation receipt无效→拒绝<br>**F12** 上游明确拒绝→原status<br>**F13** 超时后unknown待对账 | **B01** 同ID并发只一份效果<br>**B02** 只精确busy409重试<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DEPLOY；DEPLOY | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L1990–1991](../../backend/routes/agents.py#L1990) |
| **R015** `POST /api/agents/<agent_id>/deployments/<deployment_id>/recover` | **S01** rollback_failed请求恢复<br>**S02** 恢复进行中继续轮询 | **F01–07** A1–7<br>**F08** 不存在→404<br>**F09** profile/service归属错→409<br>**F10** 非可恢复状态→409<br>**F11** 上游拒绝保留rollback_failed<br>**F12** 网络结果未知保留锁 | **B01** 同deployment重试不新建事务<br>**B02** 恢复失败不声称原配置已恢复<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DEPLOY；DEPLOY | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L2002–2004](../../backend/routes/agents.py#L2002) |
| **R016** `POST /api/agents/<agent_id>/deployments/<deployment_id>/rollback` | **S01** 成功任务手工回滚<br>**S02** 旧Agent明确拒绝保留原成功基准 | **F01–07** A1–7<br>**F08** Agent/deployment不存在→404<br>**F09** profile/service错→409<br>**F10** 不允许状态或后续发布已替代→409<br>**F11** 上游404/405/409拒绝取消manual标记<br>**F12** 响应丢失保留manual_rollback并对账<br>**F13** rollback_failed保持锁 | **B01** 重复回滚<br>**B02** 轮询旧succeeded不能取消已接收的回滚<br>**B03** 不回退新发布<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | DEPLOY；DEPLOY | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L1996–1997](../../backend/routes/agents.py#L1996) |
| **R017** `DELETE /api/agents/<agent_id>/dns-follow` | **S01** 无在途工作解除绑定<br>**S02** 已经无绑定幂等 | **F01–07** A1–7<br>**F08** 目标无效→409<br>**F09** 发布或HA在途→409 | **B01** 中文emoji上游tag<br>**B02** 同profile多个OxiDNS不能各自竞争写草稿<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | FOLLOW；FOLLOW | 需实验网 L3；见L表的不可fake部分 | [agents.py:L164–177](../../backend/routes/agents.py#L164) |
| **R018** `GET /api/agents/<agent_id>/dns-follow` | **S01** 上游列表+binding/state<br>**S02** 无绑定 | **F01–07** A1–7<br>**F08** 目标不存在/停用/非OxiDNS→409 [L177](../../backend/routes/agents.py#L177)<br>**F09** native draft无效→409 [L177](../../backend/routes/agents.py#L177) | **B01** 中文emoji上游tag<br>**B02** 同profile多个OxiDNS不能各自竞争写草稿<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | FOLLOW；FOLLOW | 需实验网 L3；见L表的不可fake部分 | [agents.py:L164–177](../../backend/routes/agents.py#L164) |
| **R019** `PUT /api/agents/<agent_id>/dns-follow` | **S01** connection=network+address<br>**S02** connection=auto同网络命名空间<br>**S03** 重新绑定到另一个可用上游 | **F01–07** A1–7<br>**F08** 非对象/额外字段→409 [L177](../../backend/routes/agents.py#L177)<br>**F09** 缺源/停用/错类型→409<br>**F10** upstream tag/index/connection非法→409<br>**F11** HA要求network→409<br>**F12** 网络host含scheme/端口/回环→409<br>**F13** auto无法证同namespace→409<br>**F14** 上游已被规则固定→409<br>**F15** 目标profile已有绑定→409<br>**F16** 写入前绑定改变→409<br>**F17** DNS-follow或HA执行中→409 | **B01** 中文emoji上游tag<br>**B02** 同profile多个OxiDNS不能各自竞争写草稿<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | FOLLOW；FOLLOW | 需实验网 L3；见L表的不可fake部分 | [agents.py:L164–177](../../backend/routes/agents.py#L164) |
| **R020** `POST /api/agents/<agent_id>/dns-follow/retry` | **S01** 已确认failed状态重试→202 | **F01–07** A1–7<br>**F08** 未绑定/目标不存在→409 [L189](../../backend/routes/agents.py#L189)<br>**F09** waiting/running不允许重复启动→409<br>**F10** 上次发布unknown→409<br>**F11** HA锁冲突→409 | **B01** 持久job恢复后重试<br>**B02** 保留人工新草稿<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | FOLLOW；FOLLOW | 需实验网 L3；见L表的不可fake部分 | [agents.py:L182–189](../../backend/routes/agents.py#L182) |
| **R021** `PUT /api/agents/<agent_id>/domain-discovery` | **S01** enabled=true<br>**S02** enabled=false | **F01–07** A1–7<br>**F08** 非对象/缺enabled/非bool→400 [L757](../../backend/routes/agents.py#L757)<br>**F09** 非mihomo→400 [L761](../../backend/routes/agents.py#L761)<br>**F10** 部署冲突→409 [L763](../../backend/routes/agents.py#L763)<br>**F11** 不存在→404 [L765](../../backend/routes/agents.py#L765) | **B01** 重注册保留管理员开关<br>**B02** 心跳同步返回开关<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | REG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L753–766](../../backend/routes/agents.py#L753) |
| **R022** `POST /api/agents/<agent_id>/domain-discovery/clear` | **S01** 清traffic/probes/regions | **F01–07** A1–7<br>**F08** 不存在→404 [L774](../../backend/routes/agents.py#L774)<br>**F09** 删除文件故障按Python失败 | **B01** 不改变开关或其他Agent数据<br>**B02** P4 sidebar/query另一profile不能越过Agent绑定 | DISCOVERY-DATA | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L771–778](../../backend/routes/agents.py#L771) |
| **R023** `POST /api/agents/<agent_id>/generate-config` | **S01** Mihomo目标适配最终包<br>**S02** MosDNS最终包<br>**S03** OxiDNS验证包<br>**S04** Surge单文件<br>**S05** HA主备一致冻结 | **F01–07** A1–7<br>**F08** Agent不存在→404 [L1563](../../backend/routes/agents.py#L1563)<br>**F09** 非对象/expected_profile或service不匹配→409 [L1568](../../backend/routes/agents.py#L1568)<br>**F10** 生成时revision/目标变化→409 preview_changed [L1594](../../backend/routes/agents.py#L1594)<br>**F11** 能力缺失/final-preview失败→400 [L1596](../../backend/routes/agents.py#L1596)<br>**F12** HA两端需不同配置→400 [L1586](../../backend/routes/agents.py#L1586)<br>**F13** 规则/资源下载或语法失败→400 [L1596](../../backend/routes/agents.py#L1596) | **B01** 生成可写订阅缓存但不占用部署<br>**B02** 中文emoji附件<br>**B03** TTL/容量上限<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | PREVIEW；PREVIEW | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L1556–1596](../../backend/routes/agents.py#L1556) |
| **R024** `GET /api/agents/<agent_id>/generated-config/<preview_id>/download` | **S01** 有效预览ZIP<br>**S02** 配置变化后仍下载原冻结字节 | **F01–07** A1–7<br>**F08** preview格式错/缺失/过期→409 [L1610](../../backend/routes/agents.py#L1610)<br>**F09** 其他Agent预览→409<br>**F10** bundle损坏→409 | **B01** 成员集合和摘要<br>**B02** 24份驱逐<br>**B03** 不重新生成<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | PREVIEW；PREVIEW | 需实验网 L2；见L表的不可fake部分 | [agents.py:L1601–1610](../../backend/routes/agents.py#L1601) |
| **R025** `GET /api/agents/<agent_id>/generated-config/<preview_id>/file` | **S01** path=config.yaml<br>**S02** provider/rule/custom/resource逐成员<br>**S03** Surge.conf | **F01–07** A1–7<br>**F08** preview无效/过期/归属错→409 [L1630](../../backend/routes/agents.py#L1630)<br>**F09** 缺path→404 [L1628](../../backend/routes/agents.py#L1628)<br>**F10** 不存在文件→404 [L1628](../../backend/routes/agents.py#L1628) | **B01** ../与编码路径不能越目录<br>**B02** 二进制Geo文件按字节比<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | PREVIEW；PREVIEW | 需实验网 L2；见L表的不可fake部分 | [agents.py:L1615–1630](../../backend/routes/agents.py#L1615) |
| **R026** `POST /api/agents/<agent_id>/heartbeat` | **S01** {}有效心跳<br>**S02** 全version/config_version/status/metrics<br>**S03** host变化触发后台验证<br>**S04** 仅runtime字段变化 | **F01–07** C1–7<br>**F08** 认证失败→401 [L698](../../backend/routes/agents.py#L698)<br>**F09** Body读入失败→400/413 [L700](../../backend/routes/agents.py#L700)<br>**F10** 未知字段→400 [L704](../../backend/routes/agents.py#L704)<br>**F11** 文本错型/超长→400 [L704](../../backend/routes/agents.py#L704)<br>**F12** metrics不合法→400 [L704](../../backend/routes/agents.py#L704)<br>**F13** 认证后并发删Agent→404 [L723](../../backend/routes/agents.py#L723)<br>**F14** 持久化异常→500 [L725](../../backend/routes/agents.py#L725) | **B01** 32KiB及+1含chunked<br>**B02** metrics16KiB及+1<br>**B03** 百分比0/100<br>**B04** uint64最大值<br>**B05** version128/status64/host64<br>**B06** NaN/Inf拒绝<br>**B07** P4 sidebar/query另一profile不能越过Agent绑定 | HEARTBEAT；HEARTBEAT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L692–725](../../backend/routes/agents.py#L692) |
| **R027** `GET /api/agents/<agent_id>/ipv6-readiness` | **S01** status缓存可复用<br>**S02** refresh=1强制重查<br>**S03** refresh=true重查<br>**S04** 配置/Agent/router变动使key失效重查 | **F01–07** A1–7<br>**F08** operation/method不支持→404 [L23](../../backend/routes/ipv6_readiness.py#L23)<br>**F09** body超4096→400 [L25](../../backend/routes/ipv6_readiness.py#L25)<br>**F10** 没有明确profile→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F11** 多个profile selector不一致→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F12** Agent不存在→404<br>**F13** profile不匹配/禁用→409<br>**F14** 非mihomo→400<br>**F15** 旧协议→409<br>**F16** 上游认证/格式/超时→502 | **B01** header/query/body均一致<br>**B02** 空报告not_applicable<br>**B03** router/client证据改变使缓存无效<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | IPV6；IPV6 | 需实验网 L4；见L表的不可fake部分 | [ipv6_readiness.py:L18–63](../../backend/routes/ipv6_readiness.py#L18) |
| **R028** `POST /api/agents/<agent_id>/ipv6-readiness/<operation>` | **S01** operation=prepare check_token+接口<br>**S02** operation=probe仅已保存节点<br>**S03** operation=verify已保存deployment+revision | **F01–07** A1–7<br>**F08** 未知operation/method→404<br>**F09** 非对象/超4096→400<br>**F10** 缺/冲突profile→400<br>**F11** prepare缺check_token/interface→400<br>**F12** check_token过期/草稿/设备/router变动→409<br>**F13** prepare不满足条件→409<br>**F14** probe额外payload→400 [L55](../../backend/routes/ipv6_readiness.py#L55)<br>**F15** IPv6配置关闭→409<br>**F16** verify目标/版本/客户端无效→409<br>**F17** 队列busy→409<br>**F18** 网络失败→502 | **B01** 与配置发布交错不得证明未发布draft已生效<br>**B02** 客户端必须独立Linux；断连后验收完成<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定<br>**B04** P5明确profile与绑定/selector优先级 | IPV6；IPV6 | 需实验网 L4；见L表的不可fake部分 | [ipv6_readiness.py:L18–63](../../backend/routes/ipv6_readiness.py#L18) |
| **R029** `GET /api/agents/<agent_id>/ipv6-readiness/verification-settings` | **S01** 读取已保存client/test_url<br>**S02** 无client时空设置+候选能力 | **F01–07** A1–7<br>**F08** body超4096→400 [L25](../../backend/routes/ipv6_readiness.py#L25)<br>**F09** 没有明确profile→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F10** 多个profile selector不一致→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F11** Agent不存在→404<br>**F12** profile不匹配/禁用→409<br>**F13** 非mihomo→400<br>**F14** 旧协议→409<br>**F15** 上游认证/格式/超时→502 | **B01** header/query/body均一致<br>**B02** 空报告not_applicable<br>**B03** router/client证据改变使缓存无效<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | IPV6；IPV6 | 需实验网 L4；见L表的不可fake部分 | [ipv6_readiness.py:L18–63](../../backend/routes/ipv6_readiness.py#L18) |
| **R030** `PUT /api/agents/<agent_id>/ipv6-readiness/verification-settings` | **S01** verification-settings保存client_agent_id/test_url | **F01–07** A1–7<br>**F08** operation/method不支持→404<br>**F09** body错/超4096→400<br>**F10** 缺/冲突profile→400<br>**F11** client不是独立同profile可用Agent→409<br>**F12** URL非法/过长→400<br>**F13** busy验收中→409<br>**F14** 绑定改变→409 | **B01** 清空可选客户端<br>**B02** 中文URL路径与特殊字符<br>**B03** 保存后旧报告失效<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | IPV6；IPV6 | 需实验网 L4；见L表的不可fake部分 | [ipv6_readiness.py:L18–63](../../backend/routes/ipv6_readiness.py#L18) |
| **R031** `GET /api/agents/<agent_id>/ipv6-readiness/verify` | **S01** 指定deployment_id报告<br>**S02** 省略id取当前报告<br>**S03** IPv6未启not_applicable<br>**S04** running/succeeded/failed各报告 | **F01–07** A1–7<br>**F08** operation/method不支持→404 [L23](../../backend/routes/ipv6_readiness.py#L23)<br>**F09** body超4096→400 [L25](../../backend/routes/ipv6_readiness.py#L25)<br>**F10** 没有明确profile→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F11** 多个profile selector不一致→400 [L33](../../backend/routes/ipv6_readiness.py#L33)<br>**F12** Agent不存在→404<br>**F13** profile不匹配/禁用→409<br>**F14** 非mihomo→400<br>**F15** 旧协议→409<br>**F16** 上游认证/格式/超时→502 | **B01** header/query/body均一致<br>**B02** 空报告not_applicable<br>**B03** router/client证据改变使缓存无效<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | IPV6；IPV6 | 需实验网 L4；见L表的不可fake部分 | [ipv6_readiness.py:L18–63](../../backend/routes/ipv6_readiness.py#L18) |
| **R032** `GET /api/agents/<agent_id>/linux-gateway/<operation>` | **S01** operation=status只读当前主机检测 | **F01–07** A1–7<br>**F08** 缺显式profile→400 [L54](../../backend/routes/linux_gateway_agents.py#L54)<br>**F09** 操作/method不支持→404<br>**F10** Agent不存在→404<br>**F11** 绑定profile不符→409<br>**F12** 非mihomo→400<br>**F13** Docker/低版本→409<br>**F14** 上游404→409<br>**F15** 401/403→502<br>**F16** 过大/坏JSON/错型→502<br>**F17** 网络超时→502 | **B01** header优先于query的Python现状<br>**B02** bound profile不可受sidebar误改<br>**B03** foreign/symlink转发状态<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | GATEWAY-HOST；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway_agents.py:L48–74](../../backend/routes/linux_gateway_agents.py#L48) |
| **R033** `POST /api/agents/<agent_id>/linux-gateway/<operation>` | **S01** operation=preview<br>**S02** operation=apply<br>**S03** operation=remove<br>**S04** operation=forwarding prepare_tun=true<br>**S05** forwarding空body代理网关 | **F01–07** A1–7<br>**F08** 缺profile→400<br>**F09** body>4096含chunked→400<br>**F10** 坏JSON/非对象→400<br>**F11** 未知operation/method→404<br>**F12** 不存在Agent→404<br>**F13** 绑定改变→409<br>**F14** 非mihomo→400<br>**F15** Docker/低版本→409<br>**F16** 网络spec非法→400<br>**F17** apply未启draft→409<br>**F18** 破坏fullTUN/legacy接管→409<br>**F19** forwarding自定义字段→400<br>**F20** 未持久转发移除被阻止→409<br>**F21** Agent错误/超时→502或400/409/422透传 | **B01** 网段/8与/30<br>**B02** gateway与address不同且同网段<br>**B03** 服务成功后真正转发仅L4证明<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定<br>**B05** P5明确profile与绑定/selector优先级 | GATEWAY-HOST；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway_agents.py:L48–74](../../backend/routes/linux_gateway_agents.py#L48) |
| **R034** `GET /api/agents/<agent_id>/logs` | **S01** 默认lines=100<br>**S02** source日志源<br>**S03** log_path自定义路径 | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** upstream非200→500（sources404除外）<br>**F10** 坏JSON/错型结果→500<br>**F11** 连接失败/超时→500 | **B01** 中文emoji与凭据脱敏<br>**B02** source优先于log_path<br>**B03** lines非法文本回默认<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | LOG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1077–1090](../../backend/routes/agents.py#L1077) |
| **R035** `POST /api/agents/<agent_id>/logs/clear` | **S01** source指定清理<br>**S02** log_path指定清理 | **F01–07** A1–7<br>**F08** 缺source/path→400 [L1115](../../backend/routes/agents.py#L1115)<br>**F09** upstream失败→500 [L1119](../../backend/routes/agents.py#L1119)<br>**F10** 非对象/坏JSON→500 [L1121](../../backend/routes/agents.py#L1121)<br>**F11** Agent不存在→500<br>**F12** 超时→500 | **B01** 两个参数同时给时source优先<br>**B02** 中文路径和单引号<br>**B03** 重复清理<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | LOG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1107–1121](../../backend/routes/agents.py#L1107) |
| **R036** `GET /api/agents/<agent_id>/logs/sources` | **S01** 已解析sources<br>**S02** 旧Agent404→legacy=true空sources | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** upstream非200→500（sources404除外）<br>**F10** 坏JSON/错型结果→500<br>**F11** 连接失败/超时→500 | **B01** 空列表与旧版本<br>**B02** P4 sidebar/query另一profile不能越过Agent绑定 | LOG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1095–1102](../../backend/routes/agents.py#L1095) |
| **R037** `POST /api/agents/<agent_id>/logs/validate` | **S01** path存在可读<br>**S02** path不存在的结构化结果 | **F01–07** A1–7<br>**F08** 缺path→400 [L1133](../../backend/routes/agents.py#L1133)<br>**F09** 非对象/坏JSON→500 [L1139](../../backend/routes/agents.py#L1139)<br>**F10** Agent不存在/upstream失败→500<br>**F11** 超时→500 | **B01** 中文emoji路径<br>**B02** ../路径由fake拒绝并比错误<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | LOG；LOG | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1126–1139](../../backend/routes/agents.py#L1126) |
| **R038** `GET /api/agents/<agent_id>/metrics` | **S01** 心跳最新system_metrics<br>**S02** 无metrics时{} | **F01–07** A1–7<br>**F08** 不存在→404 [L2017](../../backend/routes/agents.py#L2017)<br>**F09** 错误持久结构→500 [L2032](../../backend/routes/agents.py#L2032) | **B01** collected_at缺失为null<br>**B02** 浮点与大整数精度<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | METRICS；METRICS | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2009–2032](../../backend/routes/agents.py#L2009) |
| **R039** `GET /api/agents/<agent_id>/metrics/history` | **S01** 默认hours=24<br>**S02** hours=1<br>**S03** 无历史[] | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 历史读取/计算错误→500 | **B01** hours=0/负数/非法文本<br>**B02** interval=0/负数<br>**B03** 未知period按Python；跨重启计数器重置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | METRICS；METRICS | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2037–2065](../../backend/routes/agents.py#L2037) |
| **R040** `GET /api/agents/<agent_id>/metrics/summary` | **S01** 默认hours=1<br>**S02** hours=24<br>**S03** 无历史摘要 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 历史读取/计算错误→500 | **B01** hours=0/负数/非法文本<br>**B02** interval=0/负数<br>**B03** 未知period按Python；跨重启计数器重置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | METRICS；METRICS | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2070–2097](../../backend/routes/agents.py#L2070) |
| **R041** `DELETE /api/agents/<agent_id>/mihomo/<path:resource>` | **S01** resource=proxy 以合法name/provider/query<br>**S02** resource=connections 以合法name/provider/query<br>**S03** resource=connection 以合法name/provider/query | **F01–07** A1–7<br>**F08** resource/method不在白名单→404<br>**F09** Agent不存在→404<br>**F10** 服务不是mihomo→400<br>**F11** 版本低于资源门槛→409<br>**F12** name/provider缺失/超512/控制符→400<br>**F13** 测速url/timeout非法→400<br>**F14** mode或selector body非法→400<br>**F15** 日志after/level非法→400<br>**F16** upstream401/403→502<br>**F17** 超16MiB/坏JSON/不合法上游结构→502<br>**F18** upstream404旧接口→409<br>**F19** core408/504→504<br>**F20** 其他core4xx→400/5xx→502<br>**F21** 网络失败/超时→502 | **B01** 中文emoji/斜杠名称单segment编码<br>**B02** logs after18位边界<br>**B03** mode三枚举<br>**B04** 测速500/30000ms边界<br>**B05** 响应slim只最新history而非全mask<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | PANEL；PANEL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L2201–2213](../../backend/routes/agents.py#L2201) |
| **R042** `GET /api/agents/<agent_id>/mihomo/<path:resource>` | **S01** resource=version<br>**S02** resource=configs<br>**S03** resource=proxies<br>**S04** resource=proxy/delay 以合法name/provider/query<br>**S05** resource=group/delay 以合法name/provider/query<br>**S06** resource=connections 以合法name/provider/query<br>**S07** resource=flow<br>**S08** resource=logs<br>**S09** resource=rules<br>**S10** resource=providers/proxies<br>**S11** resource=providers/proxy/healthcheck 以合法name/provider/query<br>**S12** resource=providers/proxy/delay 以合法name/provider/query<br>**S13** resource=providers/rules<br>**S14** resource=core/update | **F01–07** A1–7<br>**F08** resource/method不在白名单→404<br>**F09** Agent不存在→404<br>**F10** 服务不是mihomo→400<br>**F11** 版本低于资源门槛→409<br>**F12** name/provider缺失/超512/控制符→400<br>**F13** 测速url/timeout非法→400<br>**F14** mode或selector body非法→400<br>**F15** 日志after/level非法→400<br>**F16** upstream401/403→502<br>**F17** 超16MiB/坏JSON/不合法上游结构→502<br>**F18** upstream404旧接口→409<br>**F19** core408/504→504<br>**F20** 其他core4xx→400/5xx→502<br>**F21** 网络失败/超时→502 | **B01** 中文emoji/斜杠名称单segment编码<br>**B02** logs after18位边界<br>**B03** mode三枚举<br>**B04** 测速500/30000ms边界<br>**B05** 响应slim只最新history而非全mask<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | PANEL；PANEL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L2201–2213](../../backend/routes/agents.py#L2201) |
| **R043** `PATCH /api/agents/<agent_id>/mihomo/<path:resource>` | **S01** resource=configs | **F01–07** A1–7<br>**F08** resource/method不在白名单→404<br>**F09** Agent不存在→404<br>**F10** 服务不是mihomo→400<br>**F11** 版本低于资源门槛→409<br>**F12** name/provider缺失/超512/控制符→400<br>**F13** 测速url/timeout非法→400<br>**F14** mode或selector body非法→400<br>**F15** 日志after/level非法→400<br>**F16** upstream401/403→502<br>**F17** 超16MiB/坏JSON/不合法上游结构→502<br>**F18** upstream404旧接口→409<br>**F19** core408/504→504<br>**F20** 其他core4xx→400/5xx→502<br>**F21** 网络失败/超时→502 | **B01** 中文emoji/斜杠名称单segment编码<br>**B02** logs after18位边界<br>**B03** mode三枚举<br>**B04** 测速500/30000ms边界<br>**B05** 响应slim只最新history而非全mask<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | PANEL；PANEL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L2201–2213](../../backend/routes/agents.py#L2201) |
| **R044** `POST /api/agents/<agent_id>/mihomo/<path:resource>` | **S01** resource=cache/fakeip/flush<br>**S02** resource=cache/dns/flush<br>**S03** resource=upgrade | **F01–07** A1–7<br>**F08** resource/method不在白名单→404<br>**F09** Agent不存在→404<br>**F10** 服务不是mihomo→400<br>**F11** 版本低于资源门槛→409<br>**F12** name/provider缺失/超512/控制符→400<br>**F13** 测速url/timeout非法→400<br>**F14** mode或selector body非法→400<br>**F15** 日志after/level非法→400<br>**F16** upstream401/403→502<br>**F17** 超16MiB/坏JSON/不合法上游结构→502<br>**F18** upstream404旧接口→409<br>**F19** core408/504→504<br>**F20** 其他core4xx→400/5xx→502<br>**F21** 网络失败/超时→502 | **B01** 中文emoji/斜杠名称单segment编码<br>**B02** logs after18位边界<br>**B03** mode三枚举<br>**B04** 测速500/30000ms边界<br>**B05** 响应slim只最新history而非全mask<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | PANEL；PANEL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L2201–2213](../../backend/routes/agents.py#L2201) |
| **R045** `PUT /api/agents/<agent_id>/mihomo/<path:resource>` | **S01** resource=proxy 以合法name/provider/query<br>**S02** resource=providers/proxy 以合法name/provider/query<br>**S03** resource=providers/rule<br>**S04** resource=configs/reload | **F01–07** A1–7<br>**F08** resource/method不在白名单→404<br>**F09** Agent不存在→404<br>**F10** 服务不是mihomo→400<br>**F11** 版本低于资源门槛→409<br>**F12** name/provider缺失/超512/控制符→400<br>**F13** 测速url/timeout非法→400<br>**F14** mode或selector body非法→400<br>**F15** 日志after/level非法→400<br>**F16** upstream401/403→502<br>**F17** 超16MiB/坏JSON/不合法上游结构→502<br>**F18** upstream404旧接口→409<br>**F19** core408/504→504<br>**F20** 其他core4xx→400/5xx→502<br>**F21** 网络失败/超时→502 | **B01** 中文emoji/斜杠名称单segment编码<br>**B02** logs after18位边界<br>**B03** mode三枚举<br>**B04** 测速500/30000ms边界<br>**B05** 响应slim只最新history而非全mask<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | PANEL；PANEL | 需实验网 L2；见L表的不可fake部分 | [agents.py:L2201–2213](../../backend/routes/agents.py#L2201) |
| **R046** `GET /api/agents/<agent_id>/mihomo/rule-targets` | **S01** 显示有效direct/proxy/ip规则集 | **F01–07** A1–7<br>**F08** 非mihomo/不存在→400 [L19](../../backend/routes/client_tools.py#L19)<br>**F09** profile失效→repository | **B01** 空候选<br>**B02** 共享规则库启用gate<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DB0 | —（无主机应用；跨域真实性仍见协议限制） | [client_tools.py:L68–75](../../backend/routes/client_tools.py#L68) |
| **R047** `POST /api/agents/<agent_id>/mihomo/rule-targets/apply` | **S01** DOMAIN direct<br>**S02** DOMAIN-SUFFIX proxy<br>**S03** IP-CIDR<br>**S04** IP-CIDR6 | **F01–07** A1–7<br>**F08** body缺/额外字段→400 [L84](../../backend/routes/client_tools.py#L84)<br>**F09** profile错→409 [L86](../../backend/routes/client_tools.py#L86)<br>**F10** target/ruleset_id无效→400 [L88](../../backend/routes/client_tools.py#L88)<br>**F11** value/type非法→400<br>**F12** 默认规则集变化→409 [L102](../../backend/routes/client_tools.py#L102)<br>**F13** 执行前改绑→409 [L25](../../backend/routes/client_tools.py#L25) | **B01** 重复值不重复追加<br>**B02** 中文IDNA<br>**B03** 不控制器写入只保存规则<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | RULE-ACTION | —（无主机应用；跨域真实性仍见协议限制） | [client_tools.py:L80–105](../../backend/routes/client_tools.py#L80) |
| **R048** `DELETE /api/agents/<agent_id>/oxidns-ha` | **S01** 两端在线撤销HA恢复原服务状态<br>**S02** force=true离线端清面板记录 | **F01–07** A1–7<br>**F08** publication未完成→409<br>**F09** Agent不存在→409<br>**F10** 默认离线不能清理→409+can_force<br>**F11** 撤销/恢复失败保留leftover→409 | **B01** pair两端共享rule-sync设置<br>**B02** force不等于设备已清理<br>**B03** VIP/VRID边界<br>**B04** 真实双主不能由fake证明<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | HA；HA | 需实验网 L3；见L表的不可fake部分 | [agents.py:L194–213](../../backend/routes/agents.py#L194) |
| **R049** `GET /api/agents/<agent_id>/oxidns-ha` | **S01** 健康主备状态<br>**S02** 无HA状态<br>**S03** 一个成员失联的降级状态 | **F01–07** A1–7<br>**F08** Agent不存在→409 [L213](../../backend/routes/agents.py#L213)<br>**F09** 实时状态失败以成员problem展示 | **B01** pair两端共享rule-sync设置<br>**B02** force不等于设备已清理<br>**B03** VIP/VRID边界<br>**B04** 真实双主不能由fake证明<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | HA；HA | 需实验网 L3；见L表的不可fake部分 | [agents.py:L194–213](../../backend/routes/agents.py#L194) |
| **R050** `PUT /api/agents/<agent_id>/oxidns-ha` | **S01** 兩OxiDNS同profile启HA<br>**S02** 重复相同pair配置<br>**S03** 修改VIP/VRID的受控流程 | **F01–07** A1–7<br>**F08** 非对象/额外字段→409 [L213](../../backend/routes/agents.py#L213)<br>**F09** 非OxiDNS/对端不存在/自配对→409<br>**F10** 停用成员→409<br>**F11** 跨profile→409<br>**F12** 主机相同/非法IPv4→409<br>**F13** VIP非法或等同成员或其他组占用→409<br>**F14** VRID非1..255→409<br>**F15** 协议不支持→409<br>**F16** 同宿主/容器hostidentity未证实→409<br>**F17** 上游loopback阻止→409+blockers<br>**F18** router关联阻止→409<br>**F19** 配对变化/升级/部署忙→409<br>**F20** 预检/应用失败回滚→409 | **B01** pair两端共享rule-sync设置<br>**B02** force不等于设备已清理<br>**B03** VIP/VRID边界<br>**B04** 真实双主不能由fake证明<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | HA；HA | 需实验网 L3；见L表的不可fake部分 | [agents.py:L194–213](../../backend/routes/agents.py#L194) |
| **R051** `GET /api/agents/<agent_id>/oxidns-ha-environment` | **S01** native环境<br>**S02** host容器环境<br>**S03** macvlan环境 | **F01–07** A1–7<br>**F08** 不存在/非OxiDNS→404 [L1240](../../backend/routes/agents.py#L1240)<br>**F09** capabilities无法连接→502 [L1244](../../backend/routes/agents.py#L1244)<br>**F10** 非对象或success=false→502 [L1246](../../backend/routes/agents.py#L1246) | **B01** 协议数组剔除bool/非法元素<br>**B02** 64hex host_id/boot_id<br>**B03** blockers最多20且每项500字符<br>**B04** token脱敏<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | HA-ENV；HA | 需实验网 L1、L3；见L表的不可fake部分 | [agents.py:L1235–1268](../../backend/routes/agents.py#L1235) |
| **R052** `GET /api/agents/<agent_id>/oxidns/cache` | **S01** 返回设备caches tag列表 | **F01–07** A1–7<br>**F08** query或body超16KiB→400 [L2262](../../backend/routes/agents.py#L2262)<br>**F09** GET有body→400 [L2267](../../backend/routes/agents.py#L2267)<br>**F10** 不存在→404<br>**F11** 非OxiDNS→400<br>**F12** 版本不足→409<br>**F13** 响应错型/重定向/超16KiB→502<br>**F14** 网络/Agent认证失败→502 | **B01** 0/64个tag<br>**B02** 标签64字符<br>**B03** 中文/斜杠tag拒绝<br>**B04** 仅当前设备<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | CACHE；CACHE | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2254–2283](../../backend/routes/agents.py#L2254) |
| **R053** `POST /api/agents/<agent_id>/oxidns/cache/flush` | **S01** tags=[cache-a]<br>**S02** 多个tag按请求顺序全部flushed | **F01–07** A1–7<br>**F08** query/超16KiB→400<br>**F09** 非JSON/坏JSON→400<br>**F10** 非对象/多字段→400<br>**F11** tags空/重复/非字符串/超64→400<br>**F12** tag语法非法→400<br>**F13** 未知缓存→400/409按上游<br>**F14** SERVICE_BUSY/CORE_BUSY→409/503<br>**F15** 上游部分成功保留flushed<br>**F16** flushed不等于请求→502<br>**F17** 超时→502 | **B01** 64标签<br>**B02** 长度64<br>**B03** 已清理子集不可伪装回滚<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | CACHE；CACHE | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2254–2283](../../backend/routes/agents.py#L2254) |
| **R054** `GET /api/agents/<agent_id>/oxidns/clients` | **S01** recorder选择<br>**S02** 有效since_ms/until_ms/search查询 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 错误service→400<br>**F10** 版本不足→409<br>**F11** 上游401/403→502<br>**F12** redirect→502<br>**F13** 超过5MiB→502<br>**F14** 非JSON/非对象→502<br>**F15** core未配API/recorder/TLS不支持→409<br>**F16** core鉴权/连接失败→502<br>**F17** 未知字段/重复query参数→400<br>**F18** 网络超时→502 | **B01** 最多CLIENT_SAMPLE_LIMIT<br>**B02** IP归一<br>**B03** 空列表<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | MONITOR；MONITOR | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2310–2311](../../backend/routes/agents.py#L2310) |
| **R055** `POST /api/agents/<agent_id>/oxidns/dashboard/reset` | **S01** {}重置统计起点<br>**S02** recorder指定重置 | **F01–07** A1–7<br>**F08** query参数→400 [L2295](../../backend/routes/agents.py#L2295)<br>**F09** Content-Length或chunked>2048→400 [L2300](../../backend/routes/agents.py#L2300)<br>**F10** 非JSON/坏JSON→400 [L2304](../../backend/routes/agents.py#L2304)<br>**F11** body非法/recorder未知→400<br>**F12** device或snapshot变化→409<br>**F13** dashboard持久化故障→503<br>**F14** Agent不存在→404<br>**F15** 错误service→400<br>**F16** 版本不足→409<br>**F17** 上游401/403→502<br>**F18** redirect→502<br>**F19** 超过5MiB→502<br>**F20** 非JSON/非对象→502<br>**F21** core未配API/recorder/TLS不支持→409<br>**F22** core鉴权/连接失败→502<br>**F23** 未知字段/重复query参数→400<br>**F24** 网络超时→502 | **B01** 不清核心查询记录<br>**B02** 重置后overview进入新轮次<br>**B03** 并发reset不混基准<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | MONITOR；MONITOR | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2288–2305](../../backend/routes/agents.py#L2288) |
| **R056** `GET /api/agents/<agent_id>/oxidns/device-policies` | **S01** 绑定profile设备策略/notes/revisions<br>**S02** 同profile peers | **F01–07** A1–7<br>**F08** Agent不存在→404 [L2343](../../backend/routes/agents.py#L2343)<br>**F09** 非OxiDNS→400 [L2345](../../backend/routes/agents.py#L2345)<br>**F10** 旧设置非法→400 [L2384](../../backend/routes/agents.py#L2384) | **B01** sidebar header不影响<br>**B02** draft与deployment字段区分<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | POLICY | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2328–2384](../../backend/routes/agents.py#L2328) |
| **R057** `PUT /api/agents/<agent_id>/oxidns/device-policies` | **S01** 合法完整policies<br>**S02** 空列表清策略 | **F01–07** A1–7<br>**F08** 非对象/多字段/base_url错型→400 [L2352](../../backend/routes/agents.py#L2352)<br>**F09** profile错→409 [L2354](../../backend/routes/agents.py#L2354)<br>**F10** revision过时→409 [L2363](../../backend/routes/agents.py#L2363)<br>**F11** notes_revision过时→409 [L2363](../../backend/routes/agents.py#L2363)<br>**F12** 写锁内Agent改绑/服务改变→409 [L2363](../../backend/routes/agents.py#L2363)<br>**F13** 策略/备注/native引用校验失败→400 [L2384](../../backend/routes/agents.py#L2384) | **B01** 中文emoji备注<br>**B02** 多个设备共享profile互相可见<br>**B03** 仅draft不自动发布<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | POLICY；POLICY | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2328–2384](../../backend/routes/agents.py#L2328) |
| **R058** `GET /api/agents/<agent_id>/oxidns/overview` | **S01** 默认overview<br>**S02** range=round<br>**S03** 24h<br>**S04** 7d<br>**S05** 30d<br>**S06** 指定recorder<br>**S07** 有发布checkpoint新轮次 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 错误service→400<br>**F10** 版本不足→409<br>**F11** 上游401/403→502<br>**F12** redirect→502<br>**F13** 超过5MiB→502<br>**F14** 非JSON/非对象→502<br>**F15** core未配API/recorder/TLS不支持→409<br>**F16** core鉴权/连接失败→502<br>**F17** 未知字段/重复query参数→400<br>**F18** 网络超时→502<br>**F19** snapshot变化→409<br>**F20** dashboard存储失败→503/冲突409按MonitorError | **B01** 排名60秒复用和失败180秒退避<br>**B02** 空recorder<br>**B03** 大于JS精确整数范围被滤<br>**B04** 中文emoji标签<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | MONITOR；MONITOR | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2218–2219](../../backend/routes/agents.py#L2218) |
| **R059** `GET /api/agents/<agent_id>/oxidns/queries` | **S01** 默认limit50<br>**S02** limit100<br>**S03** cursor翻页<br>**S04** qname/qtype/client_ip/rcode/status/matcher_tag筛选<br>**S05** since_ms/until_ms区间 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 错误service→400<br>**F10** 版本不足→409<br>**F11** 上游401/403→502<br>**F12** redirect→502<br>**F13** 超过5MiB→502<br>**F14** 非JSON/非对象→502<br>**F15** core未配API/recorder/TLS不支持→409<br>**F16** core鉴权/连接失败→502<br>**F17** 未知字段/重复query参数→400<br>**F18** 网络超时→502 | **B01** qtype大写<br>**B02** rcode名映射Display文本<br>**B03** IPv6clientIP<br>**B04** 游标边界<br>**B05** 来源enrichment字段保留<br>**B06** P4 sidebar/query另一profile不能越过Agent绑定 | MONITOR；MONITOR | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2316–2317](../../backend/routes/agents.py#L2316) |
| **R060** `GET /api/agents/<agent_id>/oxidns/queries/<record_id>` | **S01** 合法record_id详情<br>**S02** recorder参数 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 错误service→400<br>**F10** 版本不足→409<br>**F11** 上游401/403→502<br>**F12** redirect→502<br>**F13** 超过5MiB→502<br>**F14** 非JSON/非对象→502<br>**F15** core未配API/recorder/TLS不支持→409<br>**F16** core鉴权/连接失败→502<br>**F17** 未知字段/重复query参数→400<br>**F18** 网络超时→502<br>**F19** record_id格式/范围错→400<br>**F20** 记录不存在→404 | **B01** 记录过期<br>**B02** 未知额外字段不泄漏；详情与列表来源一致<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | MONITOR；MONITOR | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2322–2323](../../backend/routes/agents.py#L2322) |
| **R061** `GET /api/agents/<agent_id>/oxidns/rules/update` | **S01** idle/running/succeeded/failed task状态轮询 | **F01–07** A1–7<br>**F08** 任意query→400 [L2232](../../backend/routes/agents.py#L2232)<br>**F09** 大于1024含chunked→400<br>**F10** GET有body→400 | **B01** task计数整数/上限及done总数一致<br>**B02** POST仅固定操作不接受paths/coreURL，内部rule-sync可用paths<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | RULE-UPDATE；RULE-UPDATE | 需实验网 L3；见L表的不可fake部分 | [agents.py:L2224–2248](../../backend/routes/agents.py#L2224) |
| **R062** `POST /api/agents/<agent_id>/oxidns/rules/update` | **S01** {}触发更新202<br>**S02** 无body也可触发<br>**S03** HA成对顺序更新 | **F01–07** A1–7<br>**F08** query/超1024→400<br>**F09** body不是{}→400<br>**F10** 不支持service/版本→400/409<br>**F11** busy→409<br>**F12** 下载/重载失败保留task code<br>**F13** 上游不合法task/非JSON/过大→502<br>**F14** 超时→502（不声称操作未执行） | **B01** task计数整数/上限及done总数一致<br>**B02** POST仅固定操作不接受paths/coreURL，内部rule-sync可用paths<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | RULE-UPDATE；RULE-UPDATE | 需实验网 L3；见L表的不可fake部分 | [agents.py:L2224–2248](../../backend/routes/agents.py#L2224) |
| **R063** `POST /api/agents/<agent_id>/push-config` | **S01** Mihomo完整包activate=true<br>**S02** MosDNS完整包<br>**S03** OxiDNS含validation receipt<br>**S04** Surge旧JSON路径<br>**S05** restart=false→ready<br>**S06** generation_id冻结产物<br>**S07** 显式publish_guard<br>**S08** 兼容expected_oxidns_revision<br>**S09** 相同deployment_id重试 | **F01–07** A1–7<br>**F08** 不存在Agent→404 [L1767](../../backend/routes/agents.py#L1767)<br>**F09** 非对象→400 [L1770](../../backend/routes/agents.py#L1770)<br>**F10** generation与deployment不匹配→409 [L1938](../../backend/routes/agents.py#L1938)<br>**F11** guard字段缺失/错型→400 [L1803](../../backend/routes/agents.py#L1803)<br>**F12** legacy revision非法/过期→409 [L1798](../../backend/routes/agents.py#L1798)<br>**F13** profile失效→409 [L1811](../../backend/routes/agents.py#L1811)<br>**F14** profile/service/config revision改变→409 [L1818](../../backend/routes/agents.py#L1818)<br>**F15** OxiDNS草稿digest改变→409 draft_changed [L1824](../../backend/routes/agents.py#L1824)<br>**F16** 不支持service→400 [L1841](../../backend/routes/agents.py#L1841)<br>**F17** restart非bool→400 [L1844](../../backend/routes/agents.py#L1844)<br>**F18** 非法deployment_id→400 [L1950](../../backend/routes/agents.py#L1950)<br>**F19** 已有未结束工作/锁→409 [L1849](../../backend/routes/agents.py#L1849)<br>**F20** 准备时目标改绑→409 [L1858](../../backend/routes/agents.py#L1858)<br>**F21** 准备/图/归档/验证错误→400 [L1950](../../backend/routes/agents.py#L1950)<br>**F22** 其他准备错误→500 [L1960](../../backend/routes/agents.py#L1960) | **B01** 80字符deployment_id<br>**B02** 中文emoji文件内容<br>**B03** 客户端断开后继续<br>**B04** 丢上传响应→unknown并轮询<br>**B05** 不重复下载已准备订阅<br>**B06** prepared进度节流<br>**B07** P4 sidebar/query另一profile不能越过Agent绑定 | DEPLOY；DEPLOY | 需实验网 L2、L3；见L表的不可fake部分 | [agents.py:L1751–1963](../../backend/routes/agents.py#L1751) |
| **R064** `GET /api/agents/<agent_id>/push-diff` | **S01** 首次checking后ready<br>**S02** 无baseline<br>**S03** 已同步无差异<br>**S04** 规则/配置/订阅节点变化<br>**S05** profile不可用→unknown | **F01–07** A1–7<br>**F08** Agent不存在→404 [L1646](../../backend/routes/agents.py#L1646)<br>**F09** 后台准备失败返回结构化unknown/原因 | **B01** 结构化secret片段和storedtoken脱敏<br>**B02** 中文emoji<br>**B03** 不改变最后发布基准<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | SYNC；SYNC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1635–1666](../../backend/routes/agents.py#L1635) |
| **R065** `GET /api/agents/<agent_id>/push-diff/entries` | **S01** added明细<br>**S02** removed明细<br>**S03** modified明细<br>**S04** q大小写不敏感<br>**S05** offset/limit分页<br>**S06** version过时→stale | **F01–07** A1–7<br>**F08** offset非ASCII数字/越界→400 [L1701](../../backend/routes/agents.py#L1701)<br>**F09** limit0/201→400 [L1701](../../backend/routes/agents.py#L1701)<br>**F10** 缺version/file或超长→400 [L1701](../../backend/routes/agents.py#L1701)<br>**F11** q超MAX_QUERY_CHARS→400 [L1701](../../backend/routes/agents.py#L1701)<br>**F12** change非法→400 [L1701](../../backend/routes/agents.py#L1701)<br>**F13** Agent不存在→404 [L1704](../../backend/routes/agents.py#L1704)<br>**F14** 无文件明细→404 [L1719](../../backend/routes/agents.py#L1719) | **B01** limit200<br>**B02** offset10000000<br>**B03** 中文emoji搜索<br>**B04** 当前comparison改变不能混页<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | SYNC；SYNC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1671–1724](../../backend/routes/agents.py#L1671) |
| **R066** `POST /api/agents/<agent_id>/restart` | **S01** fake返回success=true | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** upgrade忙→500<br>**F10** HA锁拒绝→结果映射<br>**F11** 上游HTTP失败→500<br>**F12** success=false→500<br>**F13** 坏JSON/错型→500<br>**F14** 连接失败→500 | **B01** 先POST再观察心跳而非HTTP200即认重启完成<br>**B02** P4 sidebar/query另一profile不能越过Agent绑定 | LIFECYCLE；LIFECYCLE | 需实验网 L2；见L表的不可fake部分 | [agents.py:L1052–1059](../../backend/routes/agents.py#L1052) |
| **R067** `GET /api/agents/<agent_id>/rule-sync` | **S01** Mihomo状态<br>**S02** OxiDNS状态<br>**S03** HA主备leader状态 | **F01–07** A1–7<br>**F08** Agent不存在→404 [L1744](../../backend/routes/agents.py#L1744)<br>**F09** 错误服务→400 [L1746](../../backend/routes/agents.py#L1746) | **B01** 同设置幂等<br>**B02** bound profile忽略全局选择<br>**B03** 同步循环仅规则允许变化不热改主配置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | RULESYNC；RULESYNC | 需实验网 L3；见L表的不可fake部分 | [agents.py:L1729–1746](../../backend/routes/agents.py#L1729) |
| **R068** `PUT /api/agents/<agent_id>/rule-sync` | **S01** library=true/rules=false<br>**S02** library=false/rules=true<br>**S03** 两者true<br>**S04** 两者false<br>**S05** HA成对保存 | **F01–07** A1–7<br>**F08** 任何query→400 [L1736](../../backend/routes/agents.py#L1736)<br>**F09** body>1024→400 [L1736](../../backend/routes/agents.py#L1736)<br>**F10** 非对象/缺字段/未知字段/非bool→400 [L1746](../../backend/routes/agents.py#L1746)<br>**F11** HA开启中→400 [L1746](../../backend/routes/agents.py#L1746)<br>**F12** 配对并发改变→400 [L1746](../../backend/routes/agents.py#L1746)<br>**F13** 不存在→404 [L1744](../../backend/routes/agents.py#L1744)<br>**F14** 不支持服务→400 [L1746](../../backend/routes/agents.py#L1746) | **B01** 同设置幂等<br>**B02** bound profile忽略全局选择<br>**B03** 同步循环仅规则允许变化不热改主配置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | RULESYNC；RULESYNC | 需实验网 L3；见L表的不可fake部分 | [agents.py:L1729–1746](../../backend/routes/agents.py#L1729) |
| **R069** `GET /api/agents/<agent_id>/status` | **S01** online<br>**S02** offline<br>**S03** unknown状态 | **F01–07** A1–7<br>**F08** 不存在→404 [L1072](../../backend/routes/agents.py#L1072) | **B01** 超过心跳过期窗口<br>**B02** GET不主动请求Agent<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DB0 | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L1064–1072](../../backend/routes/agents.py#L1064) |
| **R070** `DELETE /api/agents/<agent_id>/traffic-history` | **S01** 删除记录不改enabled | **F01–07** A1–7<br>**F08** 不存在→404<br>**F09** 非mihomo→400 | **B01** 重复清空<br>**B02** 只删目标Agent<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | TRAFFIC-HISTORY；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L864–870](../../backend/routes/agents.py#L864) |
| **R071** `GET /api/agents/<agent_id>/traffic-history` | **S01** range=24h<br>**S02** range=7d<br>**S03** range=30d<br>**S04** by=source<br>**S05** by=destination<br>**S06** by=chain<br>**S07** by=outbound<br>**S08** by=group | **F01–07** A1–7<br>**F08** 不存在→404 [L816](../../backend/routes/agents.py#L816)<br>**F09** 非mihomo→400 [L818](../../backend/routes/agents.py#L818)<br>**F10** 无效range→400 [L831](../../backend/routes/agents.py#L831)<br>**F11** 无效by→400 [L831](../../backend/routes/agents.py#L831) | **B01** 空数据<br>**B02** 中文emoji链组名<br>**B03** 保留窗口边界<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | TRAFFIC-HISTORY；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L824–834](../../backend/routes/agents.py#L824) |
| **R072** `POST /api/agents/<agent_id>/traffic-history` | **S01** schema1聚合增量<br>**S02** schema2完整链路增量 | **F01–07** C1–7<br>**F08** 认证失败→401 [L787](../../backend/routes/agents.py#L787)<br>**F09** 超过4MiB→413<br>**F10** 非JSON/坏JSON→400<br>**F11** 关闭或非mihomo→409 [L792](../../backend/routes/agents.py#L792)<br>**F12** clean_report失败→400 [L795](../../backend/routes/agents.py#L795) | **B01** 20000项和+1<br>**B02** 未来/过旧hour<br>**B03** 相同增量重复上报按Python累加，无report id不假定去重<br>**B04** 空usage/domains列表<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | TRAFFIC-HISTORY；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L782–797](../../backend/routes/agents.py#L782) |
| **R073** `PUT /api/agents/<agent_id>/traffic-history` | **S01** enabled=true初始化since<br>**S02** enabled=false保留<br>**S03** enabled=false+clear=true删除 | **F01–07** A1–7<br>**F08** 非对象→400 [L844](../../backend/routes/agents.py#L844)<br>**F09** enabled缺失/非bool→400 [L844](../../backend/routes/agents.py#L844)<br>**F10** clear非bool→400 [L844](../../backend/routes/agents.py#L844)<br>**F11** 不存在→404<br>**F12** 非mihomo→400<br>**F13** 部署冲突→409 [L854](../../backend/routes/agents.py#L854) | **B01** 重复开启since是否保留<br>**B02** enabled=true且clear=true不清<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | TRAFFIC-HISTORY；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L839–859](../../backend/routes/agents.py#L839) |
| **R074** `POST /api/agents/<agent_id>/traffic-report` | **S01** schema1 domain report<br>**S02** running/no_controller/auth_failed/unreachable状态分别上报 | **F01–07** C1–7<br>**F08** Agent认证失败→401 [L734](../../backend/routes/agents.py#L734)<br>**F09** 超过256KiB→413<br>**F10** 非JSON/坏JSON→400<br>**F11** 域名发现关闭或非mihomo→409 [L739](../../backend/routes/agents.py#L739)<br>**F12** clean_report失败→400 [L742](../../backend/routes/agents.py#L742) | **B01** 2000项和+1<br>**B02** 非负/越界计数<br>**B03** 重复report按现有语义合并<br>**B04** 空items<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | TRAFFIC；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L729–748](../../backend/routes/agents.py#L729) |
| **R075** `GET /api/agents/<agent_id>/traffic/stats` | **S01** period=total<br>**S02** period=today<br>**S03** period=week<br>**S04** period=hours_24 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 历史读取/计算错误→500 | **B01** hours=0/负数/非法文本<br>**B02** interval=0/负数<br>**B03** 未知period按Python；跨重启计数器重置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | METRICS；METRICS | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2102–2129](../../backend/routes/agents.py#L2102) |
| **R076** `GET /api/agents/<agent_id>/traffic/trend` | **S01** 默认24h/5min<br>**S02** hours=1 interval=1<br>**S03** 无历史 | **F01–07** A1–7<br>**F08** Agent不存在→404<br>**F09** 历史读取/计算错误→500 | **B01** hours=0/负数/非法文本<br>**B02** interval=0/负数<br>**B03** 未知period按Python；跨重启计数器重置<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | METRICS；METRICS | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2134–2168](../../backend/routes/agents.py#L2134) |
| **R077** `POST /api/agents/<agent_id>/uninstall` | **S01** 远端200接受后删本地Agent | **F01–07** A1–7<br>**F08** Agent不存在→500<br>**F09** 升级忙→500<br>**F10** HA启用/忙→拒绝<br>**F11** upstream非200→500<br>**F12** 坏JSON→500<br>**F13** 超时→500<br>**F14** 远端已接受但本地delete冲突→409或500按Python | **B01** 远端200且success=false仍以Python现有接受逻辑比较；列为行为审查，不静默修正<br>**B02** 附属历史清理范围<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | DELETE；LIFECYCLE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1172–1182](../../backend/routes/agents.py#L1172) |
| **R078** `POST /api/agents/<agent_id>/update` | **S01** protocol2 Shell升级→202<br>**S02** upgrade-info404且已知Shell走protocol1→202 | **F01–07** A1–7<br>**F08** 不存在→409 [L1194](../../backend/routes/agents.py#L1194)<br>**F09** Docker→409<br>**F10** 能力连接失败/身份或安装方式不符→409<br>**F11** 二进制缺失/架构不匹配→409<br>**F12** HA在途→409<br>**F13** 已有升级/回滚失败→409<br>**F14** 未结束deployment→409<br>**F15** 远端拒绝→409 [L1192](../../backend/routes/agents.py#L1192)<br>**F16** 提交超时→202 checking而非新ID | **B01** amd64/arm64/armv7<br>**B02** 旧新版本对比<br>**B03** 并发start只保留一个update_id<br>**B04** binary大小和sha256<br>**B05** P4 sidebar/query另一profile不能越过Agent绑定 | UPGRADE；UPGRADE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1187–1194](../../backend/routes/agents.py#L1187) |
| **R079** `GET /api/agents/<agent_id>/upgrade` | **S01** 管理员protocol2 poll<br>**S02** 管理员protocol1 poll<br>**S03** 自身Bearer直接读pending plan无递归<br>**S04** 已终态读回 | **F01** U1无凭据401<br>**F02** U2其他Agent token401<br>**F03** U3管理员过期JWT401<br>**F04** U4禁用管理员认证时按require_auth现状允许管理员分支<br>**F05** Agent token不匹配转管理员auth拒绝<br>**F06** 无任务→404 [L1211](../../backend/routes/agents.py#L1211)<br>**F07** 错identity/update_id/非法阶段不终结<br>**F08** 超时保留旧state或unknown | **B01** 600秒后有旧版本新心跳→failed可重试<br>**B02** worker succeeded但migration_ready=false→checking<br>**B03** out-of-order events不倒退<br>**B04** P4 sidebar/query另一profile不能越过Agent绑定 | UPGRADE；UPGRADE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1198–1212](../../backend/routes/agents.py#L1198) |
| **R080** `POST /api/agents/<agent_id>/upgrade/report` | **S01** protocol1 rolling_back对应update_id | **F01–07** C1–7<br>**F08** 错Agent/Bearer→401 [L1222](../../backend/routes/agents.py#L1222)<br>**F09** 无Content-Length→413 [L1224](../../backend/routes/agents.py#L1224)<br>**F10** 大于4096→413 [L1224](../../backend/routes/agents.py#L1224)<br>**F11** 非对象/错误protocol/update_id/status→400 [L1228](../../backend/routes/agents.py#L1228)（**[O6]** 坏JSON与非JSON Content-Type不进这个分支：分别是Werkzeug HTML 400/415，见F90/F91） | **B01** 回滚报告后需旧版本新心跳才rolled_back<br>**B02** 重复报告与终态不可倒退<br>**B03** P4 sidebar/query另一profile不能越过Agent绑定 | UPGRADE；UPGRADE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1216–1228](../../backend/routes/agents.py#L1216) |
| **R081** `POST /api/agents/core-install-command` | **S01** target=both<br>**S02** target=mihomo<br>**S03** target=oxidns | **F01–07** A1–7<br>**F08** body>1024→413<br>**F09** 坏JSON→400<br>**F10** body非对象/字段非target/目标不支持→400 [L371](../../backend/routes/agents.py#L371)<br>**F11** 生成失败→500<br>**F12** 快照已满→503<br>**F13** 保存失败→500 | **B01** shebang和marker保留后插set -- --core<br>**B02** 下载相同字节 | INSTALL-SNAPSHOT；SCRIPT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L365–378](../../backend/routes/agents.py#L365) |
| **R082** `GET /api/agents/core-install-script` | **S01** 固定双核心脚本 | **F01** 缺模板OSError/ValueError→500 [L254](../../backend/routes/agents.py#L254)<br>**F02** 模板约定变动RuntimeError→500 | **B01** 不含面板token<br>**B02** 响应nosniff/no-store/下载名 | SCRIPT；SCRIPT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L247–260](../../backend/routes/agents.py#L247) |
| **R083** `GET /api/agents/docker-agent-compose` | **S01** host+Mihomo<br>**S02** bridge+Mihomo<br>**S03** 单MosDNS<br>**S04** 单OxiDNS<br>**S05** Mihomo+MosDNS<br>**S06** Mihomo+OxiDNS<br>**S07** HA host OxiDNS<br>**S08** HA macvlan OxiDNS | **F01–07** A1–7<br>**F08** 名字无效→400<br>**F09** MosDNS与OxiDNS同时启用→400<br>**F10** HA混合Mihomo→400<br>**F11** HA bridge→400<br>**F12** HA固定IP/子网/网卡/网关无效→400<br>**F13** host identity路径无效→400<br>**F14** 端口无效→400<br>**F15** 其他生成异常→500 | **B01** 布尔文本归一<br>**B02** 空data_dir<br>**B03** 路径空格单引号美元符号；未限制普通network_mode按Python接受/拒绝不擅加 | COMPOSE；COMPOSE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1272–1302](../../backend/routes/agents.py#L1272) |
| **R084** `GET /api/agents/docker-agent-run` | **S01** host+Mihomo<br>**S02** bridge+Mihomo<br>**S03** 单MosDNS<br>**S04** 单OxiDNS<br>**S05** Mihomo+MosDNS<br>**S06** Mihomo+OxiDNS<br>**S07** HA host OxiDNS<br>**S08** HA macvlan OxiDNS | **F01–07** A1–7<br>**F08** 名字无效→400<br>**F09** MosDNS与OxiDNS同时启用→400<br>**F10** HA混合Mihomo→400<br>**F11** HA bridge→400<br>**F12** HA固定IP/子网/网卡/网关无效→400<br>**F13** host identity路径无效→400<br>**F14** 端口无效→400<br>**F15** 其他生成异常→500 | **B01** 布尔文本归一<br>**B02** 空data_dir<br>**B03** 路径空格单引号美元符号；未限制普通network_mode按Python接受/拒绝不擅加 | COMPOSE；COMPOSE | 需实验网 L1；见L表的不可fake部分 | [agents.py:L1307–1337](../../backend/routes/agents.py#L1307) |
| **R085** `POST /api/agents/docker-oxidns-ha-enroll/<session_id>` | **S01** 空{}+限定安装Bearer首次注册<br>**S02** 同凭据丢响应后重放返回同身份 | **F01** body不是{}→400 [L64](../../backend/routes/docker_ha_installation.py#L64)<br>**F02** 坏/过大body→400/413<br>**F03** session/token无效/过期→401<br>**F04** 已有同名同host不能覆盖身份→409<br>**F05** 登记目标改变→409 | **B01** JWT/全局registration_key不能替代capability<br>**B02** secret不进入公开guide日志 | HA-INSTALL；HA-INSTALL | 需实验网 L1；见L表的不可fake部分 | [docker_ha_installation.py:L61–67](../../backend/routes/docker_ha_installation.py#L61) |
| **R086** `POST /api/agents/docker-oxidns-ha-guide` | **S01** action=inspect<br>**S02** action=verify合法VIP<br>**S03** action=check预检报告<br>**S04** new script<br>**S05** new compose bundle<br>**S06** new compose_direct<br>**S07** new compose_compact<br>**S08** migrate script | **F01–07** A1–7<br>**F08** 坏JSON/非对象→400 [L24](../../backend/routes/docker_ha_installation.py#L24)<br>**F09** 非check body>16KiB→413<br>**F10** check>128KiB→413<br>**F11** action/mode/install_method不支持→400<br>**F12** Compose migrate拒绝→400<br>**F13** image/server_url/port非法→400<br>**F14** 路径/mount冲突→400<br>**F15** 预检输出非法→400<br>**F16** 签发读取旧坏会话→409 | **B01** 中文展示名与受限container name区分<br>**B02** bind/volume<br>**B03** host/macvlan<br>**B04** token24h<br>**B05** 相同目标签发去重<br>**B06** 大报告check专门放宽 | HA-INSTALL；HA-INSTALL | 需实验网 L1；见L表的不可fake部分 | [docker_ha_installation.py:L46–57](../../backend/routes/docker_ha_installation.py#L46) |
| **R087** `GET /api/agents/download/<filename>` | **S01** linux-amd64<br>**S02** linux-arm64<br>**S03** linux-armv7 | **F01** 非允许filename→404 [L939](../../backend/routes/agents.py#L939)<br>**F02** 缺文件→404 [L944](../../backend/routes/agents.py#L944)<br>**F03** 读异常→500 [L950](../../backend/routes/agents.py#L950) | **B01** 环境静态目录不存在回退bundled<br>**B02** ../和编码斜杠不越目录<br>**B03** 按字节/hash比 | BINARY；BINARY | 需实验网 L1；见L表的不可fake部分 | [agents.py:L922–950](../../backend/routes/agents.py#L922) |
| **R088** `GET /api/agents/gateway-forwarding` | **S01** 全profile router引用的proxy gateways<br>**S02** 没有引用返回[] | **F01–07** A1–7<br>**F08** 每台不可达/低版本/非Shell状态单独展示，不把整组成功当设备可用 | **B01** 多个router引用同Agent合并<br>**B02** 不依赖sidebarprofile | GATEWAY-HOST；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway_agents.py:L24–43](../../backend/routes/linux_gateway_agents.py#L24) |
| **R089** `POST /api/agents/gateway-forwarding` | **S01** {agent_id:agent-a}已被router引用并持久转发 | **F01–07** A1–7<br>**F08** body非对象/额外字段→400 [L33](../../backend/routes/linux_gateway_agents.py#L33)<br>**F09** 非linked Agent→404 [L37](../../backend/routes/linux_gateway_agents.py#L37)<br>**F10** 非mihomo/缺失/离线/Docker/低版本→MonitorError<br>**F11** upstream失败/超时→MonitorError | **B01** 一个Agent多个profile引用<br>**B02** 调用后状态复读 | GATEWAY-HOST；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway_agents.py:L24–43](../../backend/routes/linux_gateway_agents.py#L24) |
| **R090** `GET /api/agents/gateway-installations` | **S01** profile_id指定最新session<br>**S02** 无session返回null | **F01–07** A1–7<br>**F08** profile非法/不存在→repository错误<br>**F09** 坏持久安装记录→409 | **B01** 已过期和target_changed状态<br>**B02** 只读不签发新凭据 | INSTALL；INSTALL | 需实验网 L1、L4；见L表的不可fake部分 | [gateway_installations.py:L41–44](../../backend/routes/gateway_installations.py#L41) |
| **R091** `POST /api/agents/gateway-installations` | **S01** 手动network+selective<br>**S02** auto_network自动识别<br>**S03** full_tun安装<br>**S04** 相同目标参数幂等返回同session<br>**S05** 目标相同仅revision变化复用设备 | **F01–07** A1–7<br>**F08** body>16KiB→413<br>**F09** 坏JSON/非对象/额外字段→400<br>**F10** profile/config_revision无效→400<br>**F11** 名称超48或中文emoji→400<br>**F12** server_url回环/凭据/query等非法→400<br>**F13** 手动/auto参数混用→400<br>**F14** 端口同值/53/1053/范围错→400<br>**F15** Mihomo端口冲突→400<br>**F16** 保存配置已变化→409<br>**F17** 本地binary/checksum缺失→400<br>**F18** 短链已满→503 | **B01** use_as_server_domain=true会写全局域名<br>**B02** 24小时过期<br>**B03** 同session并发锁<br>**B04** 写system后后续验证失败不得假定全无副作用 | INSTALL；INSTALL | 需实验网 L1、L4；见L表的不可fake部分 | [gateway_installations.py:L41–44](../../backend/routes/gateway_installations.py#L41) |
| **R092** `GET /api/agents/gateway-installations/<session_id>` | **S01** waiting<br>**S02** enrolled<br>**S03** publishing<br>**S04** publish_failed<br>**S05** published | **F01–07** A1–7<br>**F08** session不存在/非法→404<br>**F09** 坏安装记录→409 | **B01** config_changed/target_changed/expired必须逐字段比<br>**B02** 命令里capability有意管理员可见但不入日志 | INSTALL；INSTALL | 需实验网 L1、L4；见L表的不可fake部分 | [gateway_installations.py:L49–50](../../backend/routes/gateway_installations.py#L49) |
| **R093** `POST /api/agents/gateway-installations/<session_id>/enroll` | **S01** 限定Bearer+两个Agent身份关联<br>**S02** 同session重复登记保持ID | **F01** 大body/坏JSON→400/413<br>**F02** capability缺/错/过期→401<br>**F03** 非精确双身份→400<br>**F04** 两Agent未通过限定注册→409<br>**F05** Agent token错误→401<br>**F06** 身份/profile/service/地址变动→409<br>**F07** 非Shell→409<br>**F08** 其他发布/升级在途→409 | **B01** 两个Agent同host但不同port<br>**B02** JWT/registration key不能代替capability | INSTALL；INSTALL | 需实验网 L1、L4；见L表的不可fake部分 | [gateway_installations.py:L80–82](../../backend/routes/gateway_installations.py#L80) |
| **R094** `POST /api/agents/gateway-installations/<session_id>/network` | **S01** auto_network=true重新检测<br>**S02** network完整手动spec | **F01–07** A1–7<br>**F08** body无效→400<br>**F09** 未登记或正在publishing→409<br>**F10** status无检测/多默认路由→409<br>**F11** host与登记不一致→409<br>**F12** 网卡/地址/默认路由不匹配→409<br>**F13** 现有网关active未撤销→409 | **B01** 不会重写主机IP或默认路由<br>**B02** 准备失败保留旧参数 | INSTALL；INSTALL | 需实验网 L4；见L表的不可fake部分 | [gateway_installations.py:L55–56](../../backend/routes/gateway_installations.py#L55) |
| **R095** `POST /api/agents/gateway-installations/<session_id>/publish` | **S01** retry_failed=false首次启动→202<br>**S02** 上次失败retry_failed=true继续<br>**S03** 已保存任务ID继续确认 | **F01–07** A1–7<br>**F08** 非对象/未知参数→400<br>**F09** 配置revision过期→409<br>**F10** 双Agent未登记/离线/停用→409<br>**F11** 已有别的部署→409<br>**F12** 上次failed未确认retry→409<br>**F13** 准备失败持久publish_failed<br>**F14** 等待任务超时保留ID而非重复创任务 | **B01** 两核心依次发布和进度轮询<br>**B02** 自动network确认后才允许接管<br>**B03** 并发点继续不双worker | INSTALL+DEPLOY；INSTALL | 需实验网 L2、L4；见L表的不可fake部分 | [gateway_installations.py:L61–63](../../backend/routes/gateway_installations.py#L61) |
| **R096** `GET /api/agents/gateway-installations/<session_id>/script` | **S01** session+query token取冻结安装脚本 | **F01** 错session→404<br>**F02** token缺失/错误/过期→401<br>**F03** 保存配置变化/目标变更→409 | **B01** Referrer-Policy:no-referrer<br>**B02** 下载字节与面板预览一致<br>**B03** 全局JWT不替代安装凭据 | INSTALL；INSTALL | 需实验网 L1、L4；见L表的不可fake部分 | [gateway_installations.py:L67–76](../../backend/routes/gateway_installations.py#L67) |
| **R097** `GET /api/agents/gateway-network-probe.sh` | **S01** 公开只读detect-gateway-network.sh | **F01** 文件读取错误→503 [L272](../../backend/routes/agents.py#L272) | **B01** 固定下载名/no-store/nosniff<br>**B02** 无凭据<br>**B03** 不执行脚本 | SCRIPT；SCRIPT | 需实验网 L1、L4；见L表的不可fake部分 | [agents.py:L264–278](../../backend/routes/agents.py#L264) |
| **R098** `GET /api/agents/i/<token>` | **S01** 有效短链取保存脚本<br>**S02** 指纹变化返回过时保护脚本STALE_SCRIPT | **F01** token格式错/不存在/过期→404 [L396](../../backend/routes/agents.py#L396)<br>**F02** 存储读取异常→503 [L394](../../backend/routes/agents.py#L394) | **B01** TTL边界<br>**B02** 响应nosniff/no-store<br>**B03** 重复下载不重渲染 | INSTALL-SNAPSHOT；SCRIPT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L382–397](../../backend/routes/agents.py#L382) |
| **R099** `POST /api/agents/install-command` | **S01** 合法单服务参数快照<br>**S02** 合法aio参数快照 | **F01–07** A1–7<br>**F08** body>32KiB→413<br>**F09** 非JSON/坏JSON→400<br>**F10** 未知字段/错类型/文本>4096/NUL→400 [L356](../../backend/routes/agents.py#L356)<br>**F11** 生成失败沿用400/500<br>**F12** 存储已满→503 [L334](../../backend/routes/agents.py#L334)<br>**F13** 落盘/SQLite错误→500 [L337](../../backend/routes/agents.py#L337) | **B01** script与短链下载逐字节一致<br>**B02** 过期链接/版本更新指纹<br>**B03** 并发持久上限 | INSTALL-SNAPSHOT；SCRIPT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L348–360](../../backend/routes/agents.py#L348) |
| **R100** `GET /api/agents/install-script` | **S01** Mihomo单服务<br>**S02** MosDNS单服务<br>**S03** OxiDNS单服务<br>**S04** type=aio双服务<br>**S05** install_core=true Mihomo<br>**S06** install_core=true OxiDNS<br>**S07** install_core=true aio<br>**S08** Mihomo ipv4_forwarding省略/true/false<br>**S09** OxiDNS prepare_dns_ha=true | **F01** 无效agent_type/shell已移除→400 [L517](../../backend/routes/agents.py#L517)<br>**F02** install_core/prepare_dns_ha/ipv4_forwarding非truefalse→400<br>**F03** 服务不支持该选项→400<br>**F04** 名字非法→400<br>**F05** 端口越界/与DNS端口冲突→400；**[O6]** 更正：install_core=false时非数字port被`type=int`静默换成8080并200，见F90<br>**F06** 生命周期配置不完整→400<br>**F07** 模板缺失→500 [L527](../../backend/routes/agents.py#L527)<br>**F08** 其他生成异常→500 [L535](../../backend/routes/agents.py#L535) | **B01** server_url显式/请求根/完整Forwarded头<br>**B02** 单引号美元反引号换行路径正确quote<br>**B03** 无auth公开入口 | SCRIPT；SCRIPT | 需实验网 L1；见L表的不可fake部分 | [agents.py:L401–402](../../backend/routes/agents.py#L401) |
| **R101** `GET /api/agents/latest-version` | **S01** 可用静态release版本 | **F01–07** A1–7<br>**F08** 版本读取异常→500 [L2182](../../backend/routes/agents.py#L2182) | **B01** semver带-go前缀/缺补丁的兼容比较 | DB0 | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L2173–2182](../../backend/routes/agents.py#L2173) |
| **R102** `POST /api/agents/register` | **S01** 首次注册返回一次性token<br>**S02** 同name/host携原Bearer重注册不再回token<br>**S03** installation_id/token限定注册<br>**S04** 空host采用remote_addr<br>**S05** Docker/回环host用X-Forwarded-For首地址（**[O6]** 只有`172.17–31.x`、`127.`开头、`localhost`、空host会被替换，`::1`和`172.16.x`不替换，见B91） | **F01** JSON null→400 [L558](../../backend/routes/agents.py#L558)<br>**F02** 坏JSON→500 [L650](../../backend/routes/agents.py#L650)<br>**F03** 非对象→500 [L650](../../backend/routes/agents.py#L650)<br>**F04** 配置registration key但缺失→403 [L580](../../backend/routes/agents.py#L580)<br>**F05** key错误→403 [L580](../../backend/routes/agents.py#L580)<br>**F06** 重注册无原token→401 [L643](../../backend/routes/agents.py#L643)<br>**F07** 重注册错token→401 [L643](../../backend/routes/agents.py#L643)<br>**F08** 安装capability失效→InstallationError [L571](../../backend/routes/agents.py#L571)<br>**F09** 冲突发布/升级/DNS-follow/HA→409 [L637](../../backend/routes/agents.py#L637)<br>**F10** profile无效/不存在→500 [L650](../../backend/routes/agents.py#L650) | **B01** 32KiB以上注册当前无route显式上限不擅加<br>**B02** 中文emoji/控制字符日志截断<br>**B03** 并发同身份仅生成一个id/token<br>**B04** 注册密钥不入DB或日志 | REG；REG | 需实验网 L1；见L表的不可fake部分 | [agents.py:L548–650](../../backend/routes/agents.py#L548) |
| **R103** `PUT /api/agents/traffic-history/retention` | **S01** days=30<br>**S02** days=90<br>**S03** days=180<br>**S04** days=365 | **F01–07** A1–7<br>**F08** 非对象/缺days→400 [L881](../../backend/routes/agents.py#L881)<br>**F09** days非法/非int/bool→400 [L881](../../backend/routes/agents.py#L881) | **B01** 裁剪窗口边界<br>**B02** 跨多个Agent全局设置 | TRAFFIC-HISTORY；TRAFFIC | —（无主机应用；跨域真实性仍见协议限制） | [agents.py:L875–882](../../backend/routes/agents.py#L875) |
| **R104** `GET /api/custom-config/linux-gateway` | **S01** 默认disabled与候选<br>**S02** 已启selective<br>**S03** legacy/full_tun接管状态<br>**S04** 已有restore_point | **F01–07** A1–7<br>**F08** 无效draft→400 [L171](../../backend/routes/linux_gateway.py#L171)<br>**F09** RouterSplitError→对应code/status [L169](../../backend/routes/linux_gateway.py#L169)<br>**F10** profile不存在→404<br>**F11** P1不存在profile→404<br>**F12** P2非法profile id→400 | **B01** 默认header/query/显式URL选择优先<br>**B02** 中文emoji规则名<br>**B03** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R105** `POST /api/custom-config/linux-gateway` | **S01** 完整settings启selective并保存restore point<br>**S02** 关闭且转发安全<br>**S03** 精确preview_token/revision保存<br>**S04** 旧整对象body兼容保存 | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R106** `POST /api/custom-config/linux-gateway/preview` | **S01** enabled=true有效选规则的preview<br>**S02** enabled=false合法关网关的preview | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(preview只读)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R107** `POST /api/custom-config/linux-gateway/restore` | **S01** 使用本次restore preview的revision/token恢复原始Mihomo/OxiDNS | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(restore)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R108** `POST /api/custom-config/linux-gateway/restore/preview` | **S01** 存在restore_point的恢复preview，返回新preview_token和changes不写 | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(preview只读)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R109** `GET /api/custom-config/linux-gateway/rules/<path:rule_id>` | **S01** 有效已选规则YAML<br>**S02** JWT<br>**S03** query config_token<br>**S04** 认证关闭且无token | **F01** D1缺JWT/query token401<br>**F02** D2错误query token401<br>**F03** D3 internal ruleproxy capability401<br>**F04** D4退役capability401<br>**F05** 授权失败→401<br>**F06** 未知/不可转换/禁用/无缓存规则→503<br>**F07** 内部ruleproxy capability不可认证<br>**F08** P1不存在profile→404<br>**F09** P2非法profile id→400 | **B01** path:rule_id含特殊编码<br>**B02** 空payload<br>**B03** 两profile相同ID隔离<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-PROVIDER；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L176–187](../../backend/routes/linux_gateway.py#L176) |
| **R110** `GET /api/custom-config/linux-gateway/source/<path:rule_id>` | **S01** 有效已选规则YAML<br>**S02** JWT<br>**S03** query config_token<br>**S04** 认证关闭且无token | **F01** D1缺JWT/query token401<br>**F02** D2错误query token401<br>**F03** D3 internal ruleproxy capability401<br>**F04** D4退役capability401<br>**F05** 授权失败→401<br>**F06** 未知/不可转换/禁用/无缓存规则→503<br>**F07** 内部ruleproxy capability不可认证<br>**F08** P1不存在profile→404<br>**F09** P2非法profile id→400 | **B01** path:rule_id含特殊编码<br>**B02** 空payload<br>**B03** 两profile相同ID隔离<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-PROVIDER；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L192–203](../../backend/routes/linux_gateway.py#L192) |
| **R111** `GET /api/profiles/<profile_id>/custom-config/linux-gateway` | **S01** 默认disabled与候选<br>**S02** 已启selective<br>**S03** legacy/full_tun接管状态<br>**S04** 已有restore_point | **F01–07** A1–7<br>**F08** 无效draft→400 [L171](../../backend/routes/linux_gateway.py#L171)<br>**F09** RouterSplitError→对应code/status [L169](../../backend/routes/linux_gateway.py#L169)<br>**F10** profile不存在→404<br>**F11** P1不存在profile→404<br>**F12** P2非法profile id→400 | **B01** 默认header/query/显式URL选择优先<br>**B02** 中文emoji规则名<br>**B03** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R112** `POST /api/profiles/<profile_id>/custom-config/linux-gateway` | **S01** 完整settings启selective并保存restore point<br>**S02** 关闭且转发安全<br>**S03** 精确preview_token/revision保存<br>**S04** 旧整对象body兼容保存 | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R113** `POST /api/profiles/<profile_id>/custom-config/linux-gateway/preview` | **S01** enabled=true有效选规则的preview<br>**S02** enabled=false合法关网关的preview | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(preview只读)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R114** `POST /api/profiles/<profile_id>/custom-config/linux-gateway/restore` | **S01** 使用本次restore preview的revision/token恢复原始Mihomo/OxiDNS | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(restore)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R115** `POST /api/profiles/<profile_id>/custom-config/linux-gateway/restore/preview` | **S01** 存在restore_point的恢复preview，返回新preview_token和changes不写 | **F01–07** A1–7<br>**F08** 非对象→400 [L113](../../backend/routes/linux_gateway.py#L113)<br>**F09** profile不存在→404<br>**F10** settings字段/规则引用无效→400<br>**F11** revision过时→409<br>**F12** preview_token缺/错→409<br>**F13** 无restore point→400<br>**F14** full_tun禁止启selective或破坏性恢复→409<br>**F15** legacy没有保留fullTUN→409<br>**F16** 转发不可确认/未持久化→409<br>**F17** 并发接管marker变化→409<br>**F18** P1不存在profile→404<br>**F19** P2非法profile id→400 | **B01** 空rule_set_ids<br>**B02** 无关字段不丢<br>**B03** 必须检查跨mihomo/oxidns原子提交<br>**B04** save/restore的token不能互用<br>**B05** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-DRAFT(preview只读)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L88–171](../../backend/routes/linux_gateway.py#L88) |
| **R116** `GET /api/profiles/<profile_id>/gateway-onboarding` | **S01** takeover=selective<br>**S02** takeover=full_tun<br>**S03** 旧legacy引导状态 | **F01–07** A1–7<br>**F08** profile不存在→404<br>**F09** 坏native/base设置→400<br>**F10** P1不存在profile→404<br>**F11** P2非法profile id→400 | **B01** 空域名/IP规则选择<br>**B02** 中文emoji<br>**B03** 保存生成两个publish_guard使用相同revision<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | ONBOARD；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [gateway_onboarding.py:L38–91](../../backend/routes/gateway_onboarding.py#L38) |
| **R117** `POST /api/profiles/<profile_id>/gateway-onboarding` | **S01** ready=true原子保存两个核心draft及publish_guards<br>**S02** 已有setup会话采纳owned write<br>**S03** 已准备配置幂等再次保存 | **F01–07** A1–7<br>**F08** body>256KiB含chunked→400<br>**F09** 坏JSON/非对象→400<br>**F10** settings/native转换未确认→400<br>**F11** ready=false保存拒绝→400 [L89](../../backend/routes/gateway_onboarding.py#L89)<br>**F12** config_revision过期→409 [L63](../../backend/routes/gateway_onboarding.py#L63)<br>**F13** 接管marker或gateway启用状态并发变动→409 [L69](../../backend/routes/gateway_onboarding.py#L69)<br>**F14** P1不存在profile→404<br>**F15** P2非法profile id→400 | **B01** 空域名/IP规则选择<br>**B02** 中文emoji<br>**B03** 保存生成两个publish_guard使用相同revision<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | ONBOARD；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [gateway_onboarding.py:L38–91](../../backend/routes/gateway_onboarding.py#L38) |
| **R118** `POST /api/profiles/<profile_id>/gateway-onboarding/preview` | **S01** ready=true预览只读<br>**S02** ready=false返回blockers<br>**S03** selective/full_tun各合法预览 | **F01–07** A1–7<br>**F08** body>256KiB含chunked→400<br>**F09** 坏JSON/非对象→400<br>**F10** settings/native转换未确认→400<br>**F11** ready=false保存拒绝→400 [L89](../../backend/routes/gateway_onboarding.py#L89)<br>**F12** config_revision过期→409 [L63](../../backend/routes/gateway_onboarding.py#L63)<br>**F13** 接管marker或gateway启用状态并发变动→409 [L69](../../backend/routes/gateway_onboarding.py#L69)<br>**F14** P1不存在profile→404<br>**F15** P2非法profile id→400 | **B01** 空域名/IP规则选择<br>**B02** 中文emoji<br>**B03** 保存生成两个publish_guard使用相同revision<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | ONBOARD(preview只读)；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [gateway_onboarding.py:L38–91](../../backend/routes/gateway_onboarding.py#L38) |
| **R119** `GET /api/profiles/<profile_id>/linux-gateway/rules/<path:rule_id>` | **S01** 有效已选规则YAML<br>**S02** JWT<br>**S03** query config_token<br>**S04** 认证关闭且无token | **F01** D1缺JWT/query token401<br>**F02** D2错误query token401<br>**F03** D3 internal ruleproxy capability401<br>**F04** D4退役capability401<br>**F05** 授权失败→401<br>**F06** 未知/不可转换/禁用/无缓存规则→503<br>**F07** 内部ruleproxy capability不可认证<br>**F08** P1不存在profile→404<br>**F09** P2非法profile id→400 | **B01** path:rule_id含特殊编码<br>**B02** 空payload<br>**B03** 两profile相同ID隔离<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-PROVIDER；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L176–187](../../backend/routes/linux_gateway.py#L176) |
| **R120** `GET /api/profiles/<profile_id>/linux-gateway/source/<path:rule_id>` | **S01** 有效已选规则YAML<br>**S02** JWT<br>**S03** query config_token<br>**S04** 认证关闭且无token | **F01** D1缺JWT/query token401<br>**F02** D2错误query token401<br>**F03** D3 internal ruleproxy capability401<br>**F04** D4退役capability401<br>**F05** 授权失败→401<br>**F06** 未知/不可转换/禁用/无缓存规则→503<br>**F07** 内部ruleproxy capability不可认证<br>**F08** P1不存在profile→404<br>**F09** P2非法profile id→400 | **B01** path:rule_id含特殊编码<br>**B02** 空payload<br>**B03** 两profile相同ID隔离<br>**B04** P3 URL/profile query/header选择与另一profile隔离 | GATEWAY-PROVIDER；GATEWAY | 需实验网 L4；见L表的不可fake部分 | [linux_gateway.py:L192–203](../../backend/routes/linux_gateway.py#L192) |

## Agent 双向 HTTP 协议与 fake Agent 合约

这里定义的是Python面板可观察的协议，不读取Go Agent/Go面板实现。fake是**有状态HTTP服务器+主动上报客户端**，按(id,token,profile,service,deployment/update)记录收发、接收时间、串行顺序和持久journal；允许在“接收前/已接收/已应用/已持久回执”四个点挂起或丢响应。每条交互的success和failure fixture都要比较两端发出的HTTP、面板DB和fake状态，而非只比较面板给浏览器的响应。

所有管理出站使用注册Agent地址/port和它的Bearer token，不接受浏览器传任意core地址/secret。不同桥接的redirect、代理和成功JSON检查规则并不相同，必须逐条保留；不能把一个通用fake“200 success=true”套给所有接口。流量/监控真实采集和设备脚本执行不能由fake证明。

| 协议ID / 方向 | 发出请求与合法返回 | 失败、超时、fake必须模拟 | Python来源 |
|---|---|---|---|
| W01 Agent→面板 注册 | POST `/api/agents/register`，REG字段，首次可带registration_key；限定安装则installation_id/token；重注册原Bearer。首次回`success,id,status,is_new,token`，重注册不回token | key403、旧身份凭据401、部署/DNS/升级锁409、坏JSON500、登记capability403/401/409；首响应丢失后使用已保存身份重放不能造第二Agent；同名host与不同名/host分开 | [routes/agents.py:547](../../backend/routes/agents.py#L547)、[manager.py:171](../../backend/agents/manager.py#L171) |
| W02 Agent→面板 心跳 | POST `/<id>/heartbeat` Bearer+HEARTBEAT；回success/domain_discovery_enabled/traffic_history_enabled/reads_host，可有confirmed_host | 32KiB body/16KiB metrics、unknown字段、bool冒整数、NaN/超uint64、删除竞争；host上报触发W03异步验证；fake主动按旧/新版本与断连重试上报，面板runtime变化不写配置、version变化才写 | [routes/agents.py:691](../../backend/routes/agents.py#L691)、[manager.py:430](../../backend/agents/manager.py#L430) |
| W03 面板→Agent 地址验证 | 自动GET `/api/capabilities`携原token并以HTTP200证明可达（Python不再核验body中的agent_id）；手动GET `/api/agent-address`读取可管理模式，POST `{host}`后才保存面板地址；旧接口404再用capabilities证身份。每次VERIFY_TIMEOUT=4秒 | 非本token无法得到200、env绑定拒改、旧Agent无地址接口、设备已写但DB冲突、请求丢失/不可达；fake两地址监听并能关闭旧端口、确认写入后心跳。真正DHCP换址需L1 | [address_follow.py:67](../../backend/agents/address_follow.py#L67)、[address_follow.py:137](../../backend/agents/address_follow.py#L137)、[address_follow.py:180](../../backend/agents/address_follow.py#L180) |
| W04 Agent→面板 拉配置 | GET `/<id>/config?token=...`，回success/profile_id/content/md5/version；md5=内容UTF8摘要，version为生成器定义前缀 | 仅Bearer/JWT不代替query；其他id token、profile失效409、生成错误400/500；Mihomo/MosDNS/OxiDNS/Surge各输出；fake必须实际解析内容而非只看success | [routes/agents.py:885](../../backend/routes/agents.py#L885)、[config_generator.py:1](../../backend/agents/config_generator.py#L1) |
| W05 面板→Agent 能力与最终预览 | GET `/api/capabilities` 5秒，需deployment_protocols=[1]及匹配service/config_path；最终生成另需final_config_protocols=[1]，POST `/api/config/final-preview` `{content}` 30秒回success=true+文本content；reload_only另查reload_only_protocols | 旧协议/错误service/config_path、capability错型/非JSON、final-preview失败、两HA目标适配字节不同；fake改写config并验证冻结后再次发布不重渲染；resource摘要不混profile基准 | [generated_preview.py:40](../../backend/agents/generated_preview.py#L40)、[manager.py:1067](../../backend/agents/manager.py#L1067) |
| W06 面板→Agent 事务上传 | POST `/api/deployments` 原始gzip包，头`X-Deployment-ID/X-Content-SHA256/X-Activate`和Bearer；(10,120)秒。返回success/status/deployment_id，fake校验包digest与manifest/profile/service/files再写journal | capability失败发生在提交前→failed；接收后丢响应→unknown待W07确认；同ID同内容幂等、同ID异内容409；只有精确409 message=`another service operation is in progress`在2秒/最多21次重试，其他409及网络错误不能证明未接收。注入非法/列表JSON响应、空status | [manager.py:705](../../backend/agents/manager.py#L705)、[manager.py:1050](../../backend/agents/manager.py#L1050) |
| W07 面板→Agent 事务状态/激活 | GET `/api/deployments/<id>`；POST `/<id>/activate`，默认(5,30)秒；fake依次ready→applying/verifying→succeeded或failed/rolled_back/rollback_failed，并带events/DNS checks | 上传在途GET404不能误判失败；无在途且journal缺失才失败；activate回包丢失仍查询同ID；旧回执/错profile/service/乱序事件不能改latest/applied；OxiDNS缺服务端验证receipt不得发activate，Agent不能伪造receipt | [manager.py:746](../../backend/agents/manager.py#L746)、[manager.py:808](../../backend/agents/manager.py#L808)、[oxidns_validation_receipt.py:34](../../backend/agents/oxidns_validation_receipt.py#L34) |
| W08 面板→Agent 回滚/恢复 | POST `/api/deployments/<id>/rollback`或`recover`，默认(5,30)秒，fake将本任务原配置恢复并在journal记manual_rollback/recovery阶段 | 明确400/404/405/409拒绝保留原成功基准；接收后丢响应不能因旧succeeded取消manual rollback；rollback_failed保持阻塞，恢复未知仍待确认；同id重复命令幂等且绝不回滚后续新部署 | [manager.py:860](../../backend/agents/manager.py#L860)、[manager.py:920](../../backend/agents/manager.py#L920) |
| W09 面板→Surge旧配置 | POST `/api/config/update` UTF8 JSON `{config,md5,restart_after_update?}` 10秒；success=true才记version/pushed_at。Agent>=1.1.0-go自重启，否则另POST restart；restart=false两者都不发 | 200错型JSON、success=false、4xx/5xx、超时均不能改发布成功基准；fake核验MD5、中文/emoji和自重启字段有无，不把事务包投给Surge | [routes/agents.py:1825](../../backend/routes/agents.py#L1825)、[manager.py:1158](../../backend/agents/manager.py#L1158) |
| W10 面板→Agent 生命周期 | POST `/api/restart`、`/api/uninstall`，Bearer，10秒；restart需JSON success，uninstall的Python逻辑为HTTP200可解析JSON即接受并本地删除 | fake模拟200 success=false、坏JSON、非200、超时、升级/HA锁拒绝；精确比当前Python接受策略，不用fake200宣称服务真的重启或已卸载。实际服务/数据保留需L1/L2 | [manager.py:1265](../../backend/agents/manager.py#L1265)、[manager.py:1455](../../backend/agents/manager.py#L1455) |
| W11 面板→Agent 升级提交 | GET `/api/upgrade-info` 5秒，返回agent_id/deployment_method=shell/architecture/version/migration_ready；protocol2 POST `/api/update` `{version,update_id,sha256,size}`。404旧Shell回protocol1 `{version,update_id}`，所有允许架构artifact预先齐全 | 非Shell/Docker/错身份/缺artifact拒绝；提交非200/202→failed，连接异常→checking保留同ID；fake记录SHA/size并模拟下载/校验/切换/迁移成功或回滚阶段；包URL不是浏览器任意传入字段 | [upgrades.py:121](../../backend/agents/upgrades.py#L121)、[upgrades.py:175](../../backend/agents/upgrades.py#L175) |
| W12 双向升级对账 | 面板GET `/api/update`与upgrade-info，必须update_id/agent_id/targetVersion/migration_ready都一致才成功；Agent GET面板`/<id>/upgrade`取自己的legacy计划，POST `upgrade/report`只许当前protocol1 rolling_back | worker succeeded但迁移未就绪→checking；10分钟后仍无法确认→unknown，只有deadline后旧版本新心跳可判failed；旧版本新心跳确认recovery_reported_at后才rolled_back；模拟旧事件/同id错agent/任务消失/重启 | [upgrades.py:244](../../backend/agents/upgrades.py#L244)、[routes/agents.py:1197](../../backend/routes/agents.py#L1197) |
| W13 面板→Agent 日志 | GET `/api/logs?lines=...&source=...`或log_path（source优先），GET logs/sources，POST logs/clear `{source}`或`{log_path}`，POST logs/validate `{path}`；均5秒，JSON success/logs/error_logs/sources/valid等按fixture | sources404专门legacy=true成功，其余非200/错JSON通常面板500；fake模拟路径不存在/不可读/超时、部分日志与token/密码回显；面板必须脱敏。日志源/文件真实读权限与rotation需L1/L2 | [manager.py:1343](../../backend/agents/manager.py#L1343)、[routes/agents.py:1075](../../backend/routes/agents.py#L1075) |
| W14 面板→Agent logging | GET/POST `/api/config/logging`，POST `{enabled}`，5秒；仅上游JSON success=true才写本地logging_config | fake成功、失败、错型、超时、enabled=null/数值按Python透传；不能把本地已写但设备未接受判成功 | [manager.py:1580](../../backend/agents/manager.py#L1580) |
| W15 Agent→面板 指标/流量 | 指标在heartbeat；domain POST traffic-report（256KiB/2000项），回rule_providers；历史POST traffic-history（4MiB/20000项）schema1/2。fake主动发空/正常/坏report并轮询metrics/summary/history/trend | 无本AgentBearer401、功能关闭409、顶层格式400；domain坏明细只drop，history坏项拒整批；计数累加、counter reset、future/expired hour丢弃、flush故障后重试、重启恢复。真实网卡/核心采样需真Agent联测 | [routes/agents.py:728](../../backend/routes/agents.py#L728)、[routes/agents.py:781](../../backend/routes/agents.py#L781)、[metrics_flush.py:1](../../backend/agents/metrics_flush.py#L1) |
| W16 面板→Agent Mihomo桥 | 按mihomo_panel.ROUTES逐resource映射GET/PUT/PATCH/DELETE/POST到`/api/mihomo/...`，路径name/provider逐segment编码；只允许mode/selector受限body。timeout连接3秒；普通读20秒、delay/healthcheck/provider40秒、reload70秒、upgrade200秒 | 无允许resource404，不发请求；fake模拟控制器未配、核心401/错误/超时、Agent401、非JSON、重定向、>16MiB；保留slim字段/最新history。`core/update`先查version再查下载源，release与alpha、缓存/强制、较新运行版不降级 | [mihomo_panel.py:23](../../backend/agents/mihomo_panel.py#L23)、[mihomo_panel.py:298](../../backend/agents/mihomo_panel.py#L298)、[mihomo_panel.py:318](../../backend/agents/mihomo_panel.py#L318) |
| W17 面板→Agent OxiDNS监控 | GET `/api/oxidns/{overview,rankings,clients,queries,queries/<id>}`，(3,12)秒，无redirect/proxy继承，<=5MiB。overview需health/capabilities/Prometheus文本metrics、labels；dashboard另需instance_id/64hex source/runtime_revision/collected_at_ms；query列表发`X-ConfigFlow-Query-Sources:1` | fake模拟core未开API/recorder/TLS不支持409、record不存在404、非法查询400、认证/连接/坏数据502、来源snapshot变化409。rankings复用60秒/失败退避180秒；reset只改面板baseline，checkpoint采集失败不能阻止发布 | [oxidns_monitor.py:132](../../backend/agents/oxidns_monitor.py#L132)、[oxidns_monitor.py:245](../../backend/agents/oxidns_monitor.py#L245)、[oxidns_monitor.py:560](../../backend/agents/oxidns_monitor.py#L560) |
| W18 面板→Agent cache与规则热更 | cache GET `/api/oxidns/cache`或POST cache/flush `{tags}`，(3,40)秒、16KiB响应；rule update GET/POST `/api/oxidns/rules/update`，(3,15)秒，task的id/status/phase/counts必须自洽。公开POST只允许{}，内部rule-sync新版本可发paths | fake tag无效/未知/重复、partial flushed、SERVICE_BUSY/CORE_BUSY、下载失败/超时/部分重载、task计数倒退或假succeeded；不允许错tag或未部署path。200/202不同语义，不能盲目重试超时造成重复更新；HA配对进入成对流程 | [oxidns_cache.py:79](../../backend/agents/oxidns_cache.py#L79)、[oxidns_rule_update.py:110](../../backend/agents/oxidns_rule_update.py#L110) |
| W19 面板→Agent DNS-follow | target GET `/api/dns/peer`证peer_agent_id/service_type（auto）；source GET `/api/dns/listener?deployment_id=...`需success/agent_id/deployment_id/enabled/host/port；后续OxiDNS通过W06–W08事务发布 | 默认(5,30)秒；fake同namespace证明缺失、错发布id、关闭DNS、回环监听跨主机、上游被移动/固定/草稿新改、两端改绑、unknown子发布；仅源成功新部署后推进。真实解析不中断与同namespace证据需L3 | [dns_follow.py:162](../../backend/agents/dns_follow.py#L162)、[dns_follow.py:450](../../backend/agents/dns_follow.py#L450)、[dns_follow.py:531](../../backend/agents/dns_follow.py#L531) |
| W20 面板→Agent DNS HA | GET `/api/oxidns-ha` (5,15)，POST `/preview` (5,30)，PUT规范spec/DELETE (5,90)；成功是状态对象，可不含success。pair capabilities含HA/container/handoff协议与host身份；handoff GET/POST/DELETE `/api/oxidns-ha/handoff`，写body operation_id，(5,15) | fake两成员独立状态机、同hostid/bootid、foreign配置、VIP holder/健康、预检blocker、应用失败撤销、撤销失败leftover、丢响应unknown；迁移30秒门禁+多轮DNS确认前禁止更新旧主。真实VRRP/VIP单主与故障切换必须L3 | [manager.py:1732](../../backend/agents/manager.py#L1732)、[oxidns_ha.py:389](../../backend/agents/oxidns_ha.py#L389)、[oxidns_ha_handoff.py:82](../../backend/agents/oxidns_ha_handoff.py#L82) |
| W21 面板→Agent Linux网关 | `/api/linux-gateway/{status,preview,apply,remove,forwarding}`按OPERATIONS method；写network/profile或受限prepare_tun，(3,45)秒、512KiB响应、不follow redirect/proxy。status需managed/detection/ipv4_forwarding/tun等供guard用 | fake foreign/symlink/no route/multiple routes、未启TUN、转发未持久、低版本、端口/网卡/spec不符、部分应用后断连；400/409/422透传其余502。主机真实iptables/nft/ip rule/TUN/sysctl及重启保持只能L4 | [agents/linux_gateway.py:350](../../backend/agents/linux_gateway.py#L350)、[agents/linux_gateway.py:382](../../backend/agents/linux_gateway.py#L382) |
| W22 面板→Agent IPv6 | GET `/api/ipv6-readiness`，POST `/prepare` `{interface,interface_index}`，`/probe` `{host,port}`，`/verify`运行快照payload；client POST `/verify-client` `{test_url,request_id,fake_ipv4_range?,fake_ipv6_range?}`，source并发`/verify-connections`关联host/request_id；(3,45)秒 | fake真实tuple关联/观察先于发送、source/client改绑、无证据、结果错型、超大、旧协议404、未知prepare结果，published revision与check_token门禁；capabilities(2,3)秒需platform=linux与client协议。假证据只能验证归并逻辑，IPv6实流、防泄漏、RouterOS入站/回流需L4 | [ipv6_readiness.py:110](../../backend/agents/ipv6_readiness.py#L110)、[ipv6_verification.py:306](../../backend/agents/ipv6_verification.py#L306) |
| W23 安装器→面板 双向凭据 | 短链GET /api/agents/i/token返回冻结脚本；gateway script用query限时token，注册携installation_id/token，enroll用限定Bearer绑定双Agent；Docker HA enroll空{}+限定Bearer，返回只该目标注册身份 | 24h、已过期/错目标/他端token、原Agent已有同名host、丢enroll响应重放不创第二身份、升级script指纹保护；GET脚本不执行/不注册，生成guide不创Agent。fake安装客户端只模拟请求，真正安装/断电/卷持久化L1 | [routes/gateway_installations.py:66](../../backend/routes/gateway_installations.py#L66)、[gateway_installation.py:321](../../backend/agents/gateway_installation.py#L321)、[docker_ha_enrollment.py:78](../../backend/agents/docker_ha_enrollment.py#L78) |
| W24 跨域邻接协议 | manager还定义POST domain-probe 200秒、GET region-check/targets 15秒、POST region-check 120秒，以及return-routing GET15/PUT60/DELETE60秒（params restore_legacy） | 由config域名发现/routers等公开路由触发的请求不增加agents120分母，但fake必须共享同Agent身份与协议错误行为；404版本提示、region409未部署检测入口、HA/return-routing不回显密钥。其真实设备效果留相应领域实验网 | [manager.py:1694](../../backend/agents/manager.py#L1694)、[manager.py:1719](../../backend/agents/manager.py#L1719) |

Fake故障参数对W03–W24各适用动作逐一展开：连接拒绝、连接超时、头后慢body/读超时、chunked超限、半包EOF、3xx到另地址、401/403/404/409/422/500/503/504、Content-Type不匹配但JSON可解析、空body/数组/非JSON、成功JSON缺必需键、错误回显synthetic secret。是否跟随redirect/继承proxy、是否接受无success/无status，按对应Python桥精确断言，不能预先统一。W06–W08精确busy重试与W11结果未知不重提分别测试。身份/主配置/manifest的变动安排在capabilities、下载、写journal、确认前各屏障点，避免“全部提前校验”的弱fake。

## 安装脚本、Compose 与发布生成文本形态

以下每项均通过上表公开HTTP渲染/下载，禁止以调用Go内部生成函数代替。每个合法替代项要单独实例化，生成文本中的synthetic secret允许只在私有fixture与响应内，不在报告/日志回显。

- **G01 原生单服务**：GET install-script：Mihomo、MosDNS、OxiDNS各一份；默认路径与自定义config_path；systemd、OpenRC、command三生命周期，command必须stop/start/status齐全。比较下载URL/架构分支、单位名、配置/日志/凭据文件路径与权限、注册/启动顺序、失败guard。 来源 [backend/agents/go_install_script.py:90](../../backend/agents/go_install_script.py#L90)。
- **G02 原生安装核心**：单Mihomo/OxiDNS install_core=true分别；不安装核心false；不兼容自定义路径/管理器/命令拒绝。比较core下载架构/版本/校验值、默认DNS端口冲突、TUN/forwarding准备顺序。 来源 [backend/agents/go_install_script.py:142](../../backend/agents/go_install_script.py#L142)。
- **G03 Mihomo独立转发**：ipv4_forwarding省略、true、false各一例；仅单Mihomo且允许的管理方式生效；比较sysctl持久片段、启动hook、原值恢复与异常停止策略，不执行到本机。 来源 [backend/agents/go_install_script.py:163](../../backend/agents/go_install_script.py#L163)。
- **G04 OxiDNS单服务准备HA**：prepare_dns_ha=false/true，systemd/OpenRC分开，command禁止；比较keepalived准备/身份文件/权限和拒绝覆盖foreign资源的guard，真正服务生效L1/L3。 来源 [backend/agents/go_install_script.py:125](../../backend/agents/go_install_script.py#L125)。
- **G05 AIO双服务**：type=aio，install_core false/true，独立端口/配置目录/service unit/binary；比较共享二进制两个Agent身份、服务顺序、停止回滚、可选管理forwarding/TUN及安装capability嵌入。 来源 [backend/agents/go_install_script.py:321](../../backend/agents/go_install_script.py#L321)。
- **G06 固定核心脚本**：core-install-script默认与core-install-command target both/mihomo/oxidns各一份；shebang/marker后固定set -- --core不能丢位置，脚本文本与短链下载完全一致。 来源 [backend/routes/agents.py:363](../../backend/routes/agents.py#L363)。
- **G07 普通Docker Compose**：host/bridge × 单Mihomo、单MosDNS、单OxiDNS、Mihomo+MosDNS、Mihomo+OxiDNS；MosDNS+OxiDNS拒绝。比services/env/volumes/ports UDP+TCP/cap_add/network/persistence，不只验证YAML能parse。 来源 [backend/agents/install_script.py:78](../../backend/agents/install_script.py#L78)。
- **G08 普通Docker Run**：G07各合法服务组合在run入口各一份；比较argv中-v/-e/-p/--network/容器名、服务端口、路径shell quote；字符空格/单引号/美元/反引号与换行有正确转义或按Python拒绝。 来源 [backend/agents/install_script.py:252](../../backend/agents/install_script.py#L252)。
- **G09 HA Host Compose/Run**：仅OxiDNS+固定IPv4+主机身份路径；比较独立ha-companion、host网络、权限/健康探测、持久HA配置挂载；两端host identity不能相同。 来源 [backend/agents/install_script.py:30](../../backend/agents/install_script.py#L30)。
- **G10 HA Macvlan Compose/Run**：固定子网/parent/地址/可选gateway；比较IPAM/ipv4_address/NET_ADMIN+NET_RAW、HA网络标识，不应给HA companion套bridge。parent不存在/地址不在网段拒绝。 来源 [backend/agents/install_script.py:30](../../backend/agents/install_script.py#L30)。
- **G11 HA guide inspect/check/verify**：inspect生成只读命令，check输入输出报告text解析，verify只向指定合成VIP渲染查询命令；逐mode和返回command_blocks次序，不签发无关凭据。 来源 [backend/agents/docker_ha_guide.py:377](../../backend/agents/docker_ha_guide.py#L377)。
- **G12 HA新装script**：host/macvlan，bind目录/identity已有文件或显式创建；比较bootstrap命令的限定enrollment、compose预览、logs/inspection/identity块，不允许直接Compose up跳过首次安全注册。 来源 [backend/agents/docker_ha_guide.py:125](../../backend/agents/docker_ha_guide.py#L125)。
- **G13 HA migrate**：旧单OxiDNS默认启动容器，agent/oxidns bind与命名卷形态分别；备份目录独立、原卷不丢、旧容器与HA记录预检、rollback_command精确复原、raw env引号/换行、保留原身份。真实mount核对与失败恢复L1。 来源 [backend/agents/docker_ha_guide.py:248](../../backend/agents/docker_ha_guide.py#L248)。
- **G14 Compose bundle**：install_method=compose host/macvlan分别，content_base64解码ZIP；比成员集合、UTF8内容、Unix权限600/700、compose与prepare/settings/bootstrap/readme引用闭包、prepare_command/start_command。ZIP时间戳不能代替内容比较。 来源 [backend/agents/docker_ha_compose.py:286](../../backend/agents/docker_ha_compose.py#L286)。
- **G15 Compose direct**：compose_direct，identity file与docker-volume两模式、host/macvlan分别；比一次性init服务read_only/network none、依赖条件、固定共享身份卷、enrollment与应用目录owner摘要；已有身份文件在应用目录内必须拒绝。 来源 [backend/agents/docker_ha_compose_direct.py:15](../../backend/agents/docker_ha_compose_direct.py#L15)。
- **G16 Compose compact**：compose_compact host/macvlan，普通data_dir与含冒号需long-bind语法的路径；比INSTALL_KEY=session.token、host只读boot_id/netns证据、companion INSTALL_SCOPE、日志轮换和grace_period；不应继承无关legacy identity选项。 来源 [backend/agents/docker_ha_compact.py:119](../../backend/agents/docker_ha_compact.py#L119)。
- **G17 短链快照/过时保护**：POST install/core-install-command得到script后GET i/token逐字节；修改生成器指纹fixture后仍200返回STALE_SCRIPT而非重新生成敏感脚本；过期404、128快照和129拒绝、512KiB脚本上限，SQLite只存自己的snapshot。 来源 [backend/agents/install_command_store.py:75](../../backend/agents/install_command_store.py#L75)。
- **G18 网关安装脚本**：gateway-installations：selective/full_tun、auto_network/手动network、amd64/arm64；比较预验证binary SHA/core资源checksum、固定双Agent端口、临时安装token、重放身份及IPv4转发/TUN差异。生成不向主机应用。 来源 [backend/agents/gateway_install_script.py:95](../../backend/agents/gateway_install_script.py#L95)。
- **G19 网络探测文本**：GET gateway-network-probe.sh的固定只读文本、下载名、nosniff；成功生成与源文件不可读503各例，实际网卡/多路由探测结果由L1/L4提供。 来源 [backend/routes/agents.py:263](../../backend/routes/agents.py#L263)。
- **G20 Agent发布完整包**：Mihomo/MosDNS/OxiDNS各一份，config/provider/rule/custom/resource所有role；安全相对path、role校验、64MiB单文件/256MiB总包/4096成员边界；ZIP/tar摘要校验和不能含state运行文件。 来源 [backend/agents/deployment_bundle.py:40](../../backend/agents/deployment_bundle.py#L40)。
- **G21 最终冻结包与Surge**：final-preview会改config内容的fake，HA两端一致与不一致；生成后读download/file，再相同generation_id发布，不能重新转换；Surge无transaction bundle但仍冻结content，md5自洽。 来源 [backend/routes/agents.py:1554](../../backend/routes/agents.py#L1554)。
- **G22 规则自动热更包**：Mihomo reload_only与OxiDNS selective paths两种，library/rules两个开关四组合；主配置/provider/resource/用户新增草稿不能被允许外变化带入；pin withheld明确提示。 来源 [backend/agents/rule_sync.py:202](../../backend/agents/rule_sync.py#L202)。
- **G23 IPv6/网关生成关联**：onboarding selective/full_tun、native/template转换确认、空/多ruleset；生成config里fakeIP、DNS、TUN与managed gateway标记要与Agent应用spec同revision；schema/文本相同不证明实流通。 来源 [backend/common/gateway_onboarding.py:1](../../backend/common/gateway_onboarding.py#L1)。
- **G24 升级binary文本元数据**：三架构固定download允许文件，upgrade artifact读取内容变化/同stat重新读/缺文件；sha256/size与fake收到payload一致；恶意filename/目录链接/替换读取只限隔离fixture，不能读真实binary或敏感文件。 来源 [backend/agents/upgrades.py:45](../../backend/agents/upgrades.py#L45)。

## 流式/长运行、轮询、上传下载与多请求序列

原有短请求框架无法观察后台状态、两个设备先后应用、响应丢失后的真实接受情况、artifact内部或OS效果。下面T序列补充每行路由；不改Go业务实现来等待/注入，仅使用公开HTTP、独立DATA_DIR、fake屏障、进程重启和外部存储观察。

- **T01 三类身份与优先级**：管理员JWT、Agent token、限时installation capability三套不互换；全局auth开/关、public whitelist、旧register重复身份与alias路径非法的先后顺序。不得凭请求来源可信跳过认证。 来源 [backend/routes/auth.py:35](../../backend/routes/auth.py#L35)。
- **T02 注册与删除竞争**：两个同名同host首次注册，丢第一次响应后使用原token重放；心跳读取Agent后删除，runtime deletion mark不得复活旧身份；新增另名同host按Python独立注册。 来源 [backend/agents/manager.py:171](../../backend/agents/manager.py#L171)。
- **T03 runtime持久化与退出flush**：高频metrics心跳只追加内存，版本变化才写agents；任务metrics_history_flush、traffic/ranking flush及SIGTERM保存，写失败后内存保留再重试，重启读回半行JSONL容错。遗弃的LEGACY_DIRS迁移不计范围。 来源 [backend/agents/metrics_history.py:1](../../backend/agents/metrics_history.py#L1)。
- **T04 地址follow**：旧host与新host fake服务各自有可控身份，120秒节流、4秒verify、same-name collision、HA成员只通知不自动改、关闭旧endpoint/重启后地址保留。真实DHCP和namespace证明L1。 来源 [backend/agents/address_follow.py:49](../../backend/agents/address_follow.py#L49)。
- **T05 预览/缓存完整性**：generate→读取每成员→下载→使用generation发布；TTL/24份/512MiB淘汰、bad JSON/sha/wrong Agent、服务或revision改动→preview_changed；生成订阅缓存后后续content-sync应稳定。 来源 [backend/agents/generated_preview.py:67](../../backend/agents/generated_preview.py#L67)。
- **T06 上传未知与幂等**：fake在收到包并持journal后断连接，面板保留submitted+ID→unknown；重试相同ID/sha只对账，错误sha409；GET上传前404与POST完成后的并发404不得误失败。 来源 [backend/agents/manager.py:705](../../backend/agents/manager.py#L705)。
- **T07 生命周期对账**：ready→activate与成功/failed/rolled_back/rollback_failed各路径；每个动作明确拒绝、接受后丢响应、重放；旧poll不能改新latest；只精确busy409有限重试，Agent token/body里的错误被脱敏。 来源 [backend/agents/manager.py:742](../../backend/agents/manager.py#L742)。
- **T08 服务端receipt**：OxiDNSvalidate实际打包字节，改变主文件/附件/manifest/sha/已载规则fingerprint后拒绝激活；fake伪造receipt无效；GET终态失败仍200且message/error/issues/events都比较。 来源 [backend/agents/oxidns_validation_receipt.py:34](../../backend/agents/oxidns_validation_receipt.py#L34)。
- **T09 跨目标与取消**：在下载、reservation、capabilities、上传、activate各点改Agent host/profile/service/token/enabled/草稿revision；拒绝旧操作但保留新内容。浏览器断连不能中止已承诺的写入、worker与锁释放。 来源 [backend/routes/agents.py:1749](../../backend/routes/agents.py#L1749)。
- **T10 HA成对发布与迁移**：在两个fake上分别持有VIP、无人持/双持/健康失败，先从/备用适配和发布、再handoff多轮DNS确认、再原主；每一阶段连接丢失/重启持久payload恢复，unknown不继续危险步骤。30秒迁移超时、cleanup结果不确定、rollback-leftover逐例。 来源 [backend/agents/oxidns_ha_handoff.py:82](../../backend/agents/oxidns_ha_handoff.py#L82)。
- **T11 HA开启/停用/force**：同profile独立hostidentity，network host/macvlan/native；preview有blocker不得写；一端apply失败按previous恢复，恢复失败保留leftover；离线force只清面板不伪称设备已撤销。真实VIP故障切换L3。 来源 [backend/agents/oxidns_ha.py:389](../../backend/agents/oxidns_ha.py#L389)。
- **T12 规则同步四组合**：library/rules各true/false，修改允许部分、主配置变化、无baseline、文件删改、过期receipt、pinned upstream withheld；timer tick能启动真实公开服务循环，用短轮询等结果，不直接调用Go worker；HA只leader提交并同步peer状态。 来源 [backend/agents/rule_sync.py:125](../../backend/agents/rule_sync.py#L125)。
- **T13 DNS follow状态机**：先建立两端成功发布baseline，源下一成功部署触发；auto与network，source rollback/新发布替代、target草稿/上游位置/规则文件变化、首次无baseline、子任务unknown/pending、failed后retry、publish后revision再变。等待/冲突不能覆写人工draft。 来源 [backend/agents/dns_follow.py:531](../../backend/agents/dns_follow.py#L531)。
- **T14 升级双协议**：protocol2每阶段、protocol1计划自取/回报、旧回执乱序、same-id错agent、artifact stat内容变化、提交丢响应、migration未就绪、rollback_failed、600秒unknown和旧版新心跳解锁；重启后任务继续同ID。 来源 [backend/agents/upgrades.py:175](../../backend/agents/upgrades.py#L175)。
- **T15 日志轮询与流式边界**：/logs与/mihomo/logs是面板有限HTTP响应，连续after游标读增量；空结果、rotation、截断、丢连接、source优先级、旧sources404；真实Agent消费Mihomo日志SSE/核心query来源流的帧协议Python并未定义，fake只比较面板到Agent REST，核心流正确性留真Agent集成，不能声称测过WebSocket/SSE。 来源 [backend/agents/mihomo_panel.py:109](../../backend/agents/mihomo_panel.py#L109)。
- **T16 OxiDNS统计发布连续性**：overview→reset→rankings reuse/backoff；两不同recorder和Agent隔离；checkpoint旧current期间新发布/回滚/失败时baseline不重复carry；snapshot/revision/core instance变动；历史slot不重复记，采集失败不阻止发布。 来源 [backend/agents/oxidns_publication_stats.py:91](../../backend/agents/oxidns_publication_stats.py#L91)。
- **T17 热更与清cache部分失败**：POST固定更新后poll running/downloading/reloading到终态，下载超时可能仍在设备执行；partial reloaded/partial flushed必须向前端保留，不自动假回滚；HA两个成员逐步结果与busy冲突。 来源 [backend/agents/oxidns_rule_update.py:110](../../backend/agents/oxidns_rule_update.py#L110)。
- **T18 安装能力生命周期**：generate不创Agent，下载→register→enroll→query waiting/enrolled/publishing/published；24h过期、重复enroll/丢响应、错目标/改绑、全局key与安装token不混、128快照边界和生成指纹变化保护。 来源 [backend/agents/gateway_installation.py:155](../../backend/agents/gateway_installation.py#L155)。
- **T19 网关安装继续发布**：顺序双核心发布+网络验证，发布失败保留deployment ID；retry_failed显式确认、超时再继续仅对账；auto多默认路由阻止，不重写主机IP或网关；同时点击继续只有一worker。 来源 [backend/agents/gateway_installation.py:629](../../backend/agents/gateway_installation.py#L629)。
- **T20 网关draft与restore**：GET revision→preview→save；共享资源或其他页面改配置时409；restore必须精确action/revision/token，保存原始Mihomo/OxiDNS一次，非法快照拒绝，不在响应泄露原文secret；full_tun/legacy接管门禁由fake转发证据支持。 来源 [backend/common/linux_gateway_draft.py:30](../../backend/common/linux_gateway_draft.py#L30)。
- **T21 OS副作用与结果未知**：Linux apply/remove/forwarding、IPv6 prepare在接受后丢响应，面板必须提示重新检查，不声称无变化；fake持managed/foreign/symlink及forwarding/TUN状态，后续status能对账。真实nft/sysctl/路由/重启只能L4。 来源 [backend/agents/linux_gateway.py:382](../../backend/agents/linux_gateway.py#L382)。
- **T22 IPv6端到端证据归并**：source+独立client+router观察fake，先启watch再发新request_id，旧connection ID排除、tuple/source请求不匹配不能pass；客户端/路由策略/部署快照在中途变化报告作废；只配置检查不能替代published实流。 来源 [backend/agents/ipv6_verification.py:306](../../backend/agents/ipv6_verification.py#L306)。
- **T23 并发文件/DB失败**：read-only/full disk/rename/fsync/SQLite busy与外部commit，在session、bundle receipt、snapshot、升级reserve、部署record各点注入；精确区分事务未提交与设备已变更但回执失败；持久锁跨进程验证，不拿全局500当全无副作用。 来源 [backend/agents/rule_sync_store.py:16](../../backend/agents/rule_sync_store.py#L16)。
- **T24 比较器可失败**：故意篡改fake收到的method/auth/payload、HTTP status、manifest成员/sha、配置叶子、DB的绑定/状态、出站调用顺序各一份，必须由生产同一比较器报差异；忽略/归一清单只能精确路径，不全mask。 来源 [backend/routes/agents.py:1751](../../backend/routes/agents.py#L1751)。

## [O6] 横切补充用例（X01–X15）

Step A原稿逐个handler读源码，漏掉的主要在handler之外：Flask应用壳层（before/after_request钩子、蓝图全局性、Werkzeug路由与解析）、生产前置Nginx、进程启动的后台循环和只在内存里的状态。每个X在“适用范围”列出的路由上展开，计入该路由的用例数；期望值一律以Python实际结果为准，这里写的状态码/消息只是提示。

| 代码 | 输入与断言 | 适用范围 | 来源 |
|---|---|---|---|
| X01 全局JSON脱敏钩子（agents部分） | after_request对每个JSON响应重写：任何字符串只要含**任一已持久化Agent token**或当前/退役rule_proxy_token（原文、`%`/`+`编码、多重编码）整串变`[REDACTED]`；键`rule_proxy_token`/`retired_rule_proxy_tokens`/`rule_fetch_proxy`任意深度删除。脱敏集合在响应时从已提交文档读取，所以**同一请求刚生成的token也会被脱敏**，刚删除Agent的token则不再脱敏。agents豁免只有：`agents.register_agent`与`agents.docker_oxidns_ha_enroll`在2xx且`success is True`时的**顶层**`token`；`agents.get_agent_config`与`agents.generate_final_agent_config`在200且success时的`content`。由此：R002管理员手动添加的成功体`data.token`是`"[REDACTED]"`；R079 Agent分支返回的原始latest_upgrade同样过钩子；push-diff/entries先在handler里用`without_stored`做片段级替换，钩子不再整串清空；Agent日志先`redact_secrets_in_text`逐行替换再过钩子。fixture把token放进Agent名、日志行、mihomo桥返回的provider URL、OxiDNS查询记录、错误消息、DNS-follow/HA失败文本，逐个比较。 | R002、R034、R042、R054、R059、R060、R064、R065、R079、R085、R102（首注册/重注册/失败体）、R019/R050失败体、R023 | [profile_context.py:104](../../backend/common/profile_context.py#L104)、[profile_context.py:164](../../backend/common/profile_context.py#L164)、[config_export.py:137](../../backend/common/config_export.py#L137)、[config_repository.py:1797](../../backend/common/config_repository.py#L1797) |
| X02 profile解析先于鉴权，以及agents蓝图的全局性 | before_request顺序：profile解析→登录鉴权→rule-sync启动。(a) `agents`蓝图是全局的：`/api/agents/**`忽略`X-ConfigFlow-Profile`/`?profile=`，即使指向不存在的profile也不404，响应也没有`X-ConfigFlow-Profile`头；(b) 例外是handler自己读header的：IPv6（R027–R031，ProfileNotFound不是MonitorError→全局404、无no-store）、Linux网关操作（R032/R033）、Docker HA guide（R086，ProfileNotFound是KeyError→全局404；ProfileValidationError是ValueError→_respond 400+no-store）；(c) `custom_config`蓝图（R104–R110）非全局：未登录+不存在profile（header/`profile`/`profile_id`）→404 `Profile not found: <id>`，非法id→400，**先于401**，包括公开provider R109/R110；(d) `profiles`蓝图带路径id（R111–R120）同样404/400先于401，成功响应带`X-ConfigFlow-Profile`。 | (a) 每个agents路由抽一条代表 + R001/R004/R010/R026；(b) R027、R031、R032、R033、R086；(c)(d) R104–R120 | [routes/__init__.py:113](../../backend/routes/__init__.py#L113)、[profile_context.py:32](../../backend/common/profile_context.py#L32)、[profile_context.py:90](../../backend/common/profile_context.py#L90)、[docker_ha_installation.py:51](../../backend/routes/docker_ha_installation.py#L51) |
| X03 两套鉴权消息与按endpoint/方法的白名单 | before_request拒绝用`请先登录后再操作。`/`登录已失效，请重新登录。`；被白名单放过、再进`require_auth`的路由用另一套：`缺少有效的登录凭据，请重新登录。`、过期`登录已过期，请重新登录。`+`error:"token_expired"`、无效`登录凭据无效，请重新登录。原始错误：<PyJWT文本>`+`error:"token_invalid"`（英文detail只比有无）。走第二套的有：R079管理员分支、`GET /api/agents/register`等公开精确路径落到`/<agent_id>`（R004 handler，require_auth 401）。同一路径按方法不同鉴权：`POST /<id>/traffic-history`只认Agent token，GET/PUT/DELETE只认JWT（互换各一例）。公开精确路径表里还有不存在的`/api/agents/docker-compose`、`docker-run`、`docker-mihomo-compose`、`docker-mihomo-run`、`docker-mosdns-compose`、`docker-mosdns-run`：开启登录时匿名访问→404，而其他不存在的`/api/agents/xxx`→401。认证关闭时Agent自身路由仍要Agent token，R079无凭据走管理员分支并发出站请求。`AGENT_REGISTRATION_KEY`、`ADMIN_*`、`JWT_SECRET_KEY`两端相同。 | R004、R026、R071–R073、R079、R102、上述6个公开不存在路径、2个随机不存在路径 | [routes/auth.py:35](../../backend/routes/auth.py#L35)、[common/auth.py:123](../../backend/common/auth.py#L123)、[routes/agents.py:1197](../../backend/routes/agents.py#L1197) |
| X04 路由优先级与方法回落 | Werkzeug对静态段在前的规则优先，且路径匹配但方法不符时继续找其他规则；Go `ServeMux`对重叠模式的规则不同（可能注册即冲突）。逐条比状态、handler副作用与`Allow`：`GET /api/agents/i/{status,config,logs,metrics,upgrade,address}`→短链文本404；`GET /download/config`→`Invalid filename` 404，`POST /download/heartbeat`→落到agent_id=`download`的心跳401；`GET /gateway-installations/status`→会话查找；`POST /docker-oxidns-ha-enroll/{restart,update,heartbeat}`→enroll；`POST /gateway-installations/ipv6-readiness/enroll`、`GET /gateway-installations/deployments/script`→gateway路由；`GET /api/agents/{register,latest-version,install-command}`、`DELETE /api/agents/install-script`→`/<agent_id>` handler（404 Agent not found）；`POST /api/agents/latest-version`→405；`POST /<id>/mihomo/rule-targets`、`GET /<id>/mihomo/rule-targets/apply`→桥接白名单404；`POST /<id>/ipv6-readiness/{status,verification-settings}`→`NOT_ALLOWED` 404；`POST /<id>/oxidns/cache`→405；`<path:resource>`/`<path:rule_id>`含`..`、`%2F`、`//`、尾斜杠；agent_id/session_id/preview_id/token含`%2F`。 | R004、R026、R042–R047、R052、R085、R087、R092–R096、R098、R028/R029、R109/R110 | [routes/agents.py:381](../../backend/routes/agents.py#L381)、[routes/agents.py:921](../../backend/routes/agents.py#L921)、[routes/agents.py:2199](../../backend/routes/agents.py#L2199)、[ipv6_readiness.py:13](../../backend/routes/ipv6_readiness.py#L13) |
| X05 Agent与安装器请求体解析 | `_read_json_body`顺序：Agent认证401→Content-Length或实际字节超限413（`Request body too large`）→`request.is_json`（`application/json`与`application/*+json`）否则400→`json.loads(bytes)`。`json.loads(bytes)`接受UTF-8 BOM与UTF-16/32、`NaN`/`Infinity`、任意大整数、重复键（后者生效）；深嵌套抛RecursionError不在捕获列表：心跳被外层`except Exception`变500 JSON，traffic-report/traffic-history未包裹→Werkzeug HTML 500。指标：非百分比字段必须是int（`1024.0`、`1e3`、`true`拒绝），百分比字段接受float。register用`get_json(force=True)`：不看Content-Type，坏JSON→500（消息是Werkzeug 400文本），`null`→400，数组→500，字段类型不校验（port字符串、version整数/null照存，之后R001完整列表在`has_update`抛异常→Python 500 HTML）。upgrade/report：无Content-Length→413，坏JSON→HTML 400，text/plain→HTML 415。gateway安装`_body`空体当`{}`，Docker HA enroll空体→400且不看Content-Type。query整数：`request.args.get(..., type=int)`非法值**静默用默认值**（install-script的port/deployment_health_timeout、docker compose/run端口），`int()`接受` 80`、`+80`、`8_0`、全角/阿拉伯数字；push-diff/entries要求`isascii() and isdigit()`。每类在出现它的入口各一例。 | R002、R005、R026、R072、R074、R080、R083、R084、R085、R093、R100、R102、R065、R001 | [routes/agents.py:668](../../backend/routes/agents.py#L668)、[routes/agents.py:86](../../backend/routes/agents.py#L86)、[routes/agents.py:558](../../backend/routes/agents.py#L558)、[routes/agents.py:1224](../../backend/routes/agents.py#L1224)、[gateway_installations.py:11](../../backend/routes/gateway_installations.py#L11)、[docker_ha_installation.py:14](../../backend/routes/docker_ha_installation.py#L14) |
| X06 面板地址与客户端IP的来源（每条路由不同） | 生成内容里的面板URL：R010配置拉取=`request.url_root`（只看Host）；R100 install-script=参数`server_url`＞同时存在的`X-Forwarded-Proto`+`X-Forwarded-Host`＞url_root（只给其一被忽略）；R063 push-config=body`base_url`＞`X-Forwarded-Proto`、`X-Forwarded-Host`**各自独立**回落到scheme/host；R023 generate-config=body`base_url`＞host_url；R050 HA PUT=host_url；R116–R118、R105–R108=url_root；R091 gateway安装=body`server_url`校验后与系统`server_domain`比较（不一致409 `server_url_mismatch`，可选写入server_domain）。R102注册host：为空、`127.`开头、`localhost`、或`172.17–31.x.x`时改用`X-Forwarded-For`第一个元素（去空格），否则remote_addr；`::1`、`172.16.x`、`172.32.x`不替换。waitress `clear_untrusted_proxy_headers=False`，头原样进Flask。变体：Host带端口/IPv6字面量/缺失、XFH含逗号列表、只给XFP、body base_url含换行/引号/`javascript:`。值进入生成文本后逐字节比；是否应信任这些头由O9决定，改动记O12。 | R010、R023、R050、R063、R091、R100、R102、R105、R116 | [routes/agents.py:455](../../backend/routes/agents.py#L455)、[routes/agents.py:587](../../backend/routes/agents.py#L587)、[routes/agents.py:903](../../backend/routes/agents.py#L903)、[routes/agents.py:1571](../../backend/routes/agents.py#L1571)、[routes/agents.py:1785](../../backend/routes/agents.py#L1785)、[gateway_installation.py:101](../../backend/agents/gateway_installation.py#L101)、[http_server.py:30](../../backend/http_server.py#L30) |
| X07 在线状态只在内存（fake必须两端各自心跳） | 在线=`last_heartbeat`距今≤120秒（`HEARTBEAT_FRESH_SECONDS`）；心跳只写内存，version/config_version变化才写配置。注册写`status:"online"`与`last_heartbeat=datetime.now().isoformat()`（**本地naive时间**，微秒为0时无小数），所以新注册Agent不心跳也在线120秒。面板重启后所有Agent离线直到下次心跳。种子`last_heartbeat`若在未来，`heartbeat_fresh`得到负年龄→Python永远在线（overlay会忽略未来值，但文档值仍被用）。发布、HA、IPv6、转发、网关安装继续等都用`is_usable`/`is_online`门禁。用例：(a) 注册后不心跳，t=60s在线、t=130s离线；(b) 两端重启后离线，心跳后恢复；(c) 未来时间种子；(d) 删除后到达的心跳不复活；(e) 启动时导入旧`runtime.sqlite3`（种该文件：启动后文件被删、较新版本写入配置、Agent保持在线）——FILES.md未把它列为not-ported，Go没移植则记O12；(f) `last_heartbeat`字符串格式在TZ=UTC与Asia/Shanghai下逐字符比。Harness规则：任何超过约100秒的用例都要让两端的fake持续心跳；判断离线差异前先核对两端最后心跳时间。 | 全部需要Agent在线的用例的前提；R001、R004、R026、R063、R095单列 | [manager.py:45](../../backend/agents/manager.py#L45)、[manager.py:233](../../backend/agents/manager.py#L233)、[manager.py:48](../../backend/agents/manager.py#L48)、[runtime_state.py:163](../../backend/agents/runtime_state.py#L163) |
| X08 agents后台循环与启动方式 | 模块导入即启动：DNS联动（每5秒）、HA发布（每2秒）、IPv6验收调度（每5秒）。**第一个通过profile解析和登录鉴权的请求**才启动Agent规则同步循环（每5秒，同时写上线/离线日志`已恢复在线`/`已离线：超过 2 分钟没有心跳`）；被401/404拒绝的请求不启动它。只有`python -m backend.app`服务进程才有：metrics flush（30秒tick，默认1800秒间隔）、`metrics_flush.load_history()`、SIGTERM保存、OxiDNS排行历史（60秒一轮）。每次心跳可能起地址核实线程；网关安装publish起worker线程；配置提交可能起WebDAV自动备份线程。循环用`IdleRounds`：配置head不变时最多跳过600秒。Harness规则：(a) Python端只用`python -m backend.app`（waitress，`LOG_LEVEL`≠DEBUG）启动，不用Flask test client或调试服务器——否则没有metrics历史加载、没有SIGTERM保存，chunked与线程模型也不同；(b) 启动后先发一个已认证的无副作用请求，让Python的规则同步循环启动；“种一台开启rule_sync的Agent、不发任何请求”作为探针，Go若在启动时就同步则记O12；(c) 写后比较前轮询到两端静止；(d) fake按“请求触发/循环触发”标记transcript。 | 全部用例的环境前提；R067/R068、R017–R020、R048–R050、R031、R038–R040单列 | [agent_manager.py:18](../../backend/common/agent_manager.py#L18)、[app.py:111](../../backend/app.py#L111)、[app.py:182](../../backend/app.py#L182)、[rule_sync.py:605](../../backend/agents/rule_sync.py#L605)、[presence_log.py:20](../../backend/agents/presence_log.py#L20)、[idle_rounds.py:18](../../backend/agents/idle_rounds.py#L18) |
| X09 Agent版本门槛 | 每个门槛取min-1、min、min+1（含/不含`-go`）三例：Mihomo桥1.8.15-go（及`RESOURCE_MIN_VERSION`逐资源）、Linux网关1.8.25/转发1.8.40/独立转发1.8.41、traffic-history 1.8.51、全链路1.8.52（这两个**额外要求以`-go`结尾**）、排行历史1.8.55、规则热更1.8.30/paths 1.8.35、规则同步1.8.30、OxiDNS监控1.7.0/clients 1.7.1/dashboard 1.8.14、cache 1.8.36、域名发现1.4.0/探测1.5.0/区域1.6.0、自重启1.1.0-go、`LATEST_AGENT_VERSION`=1.8.61-go（R001 `has_update`、R101）。`compare_versions`怪癖：后缀被丢弃（`1.8.61-rc1`==`1.8.61`）、长度补0（`1.8`==`1.8.0`）、非数字段按字符串比（`v1.8`）、`int()`接受空白和Unicode数字；缺失version的默认值各处不同（`'0'`、`'0.0.0'`、注册写`'1.0.0'`）。Go的最新版本常量必须等于随包发布的Agent二进制版本。 | R001、R026（version变化即生效）、R032/R033、R042–R045、R052/R053、R054/R058、R061/R062、R067/R068、R071–R073、R078、R101 | [version.py:548](../../backend/agents/version.py#L548)、[mihomo_panel.py:17](../../backend/agents/mihomo_panel.py#L17)、[linux_gateway.py:14](../../backend/agents/linux_gateway.py#L14)、[traffic_history.py:111](../../backend/agents/traffic_history.py#L111) |
| X10 写入后跨领域副作用 | (a) 通知（`GET /api/notifications`）：每台Agent的发布状态（`deployment:<id>`）、地址变化/恢复（`agent-address:<id>`）、注册被拒（`agent-registration`）；(b) 任务页（`GET /api/tasks`）的规则同步、DNS联动、metrics flush行；(c) WebDAV自动备份：配置提交且auto_backup开启时，只要Agent的**非运行字段**变化就安排备份——运行字段仅`last_heartbeat,status,version,service_status,config_version,system_metrics,cpu,memory,disk,network,updated_at,deployments,latest_deployment,latest_upgrade,config_revision,pushed_at,applied_content`。所以注册/重注册（host/name/enabled）、开关、rule_sync/dns_follow/oxidns_ha及其state、logging_config、ipv6设置的写入**会**上传；心跳只改version时提交配置但不上传。用一个带fake WebDAV、auto_backup开启的专用fixture跑这些写入，比较上传次数与内容。(d) app.log里的注册、发布、离线事件行（同config X07规则）。 | R002、R003、R005、R007、R012、R019、R021、R026、R050、R063、R068、R073、R102 | [notifications.py:72](../../backend/notifications.py#L72)、[notifications.py:108](../../backend/notifications.py#L108)、[notifications.py:129](../../backend/notifications.py#L129)、[webdav_backup.py:20](../../backend/utils/webdav_backup.py#L20)、[config_repository.py:1010](../../backend/common/config_repository.py#L1010) |
| X11 HEAD/OPTIONS与下载头 | HEAD对每个GET路由执行handler只是不发body：R010会生成配置并可能写订阅缓存；R013、R079管理员分支、R042、R049、R051会向fake发出站请求并可能写DB；R058/R054写排行与dashboard状态；R027写`.ipv6-readiness`缓存；R001/R064触发内容比较后台计算；R098/R096读快照。比较副作用与头，不比body。OPTIONS：`CORS(app)`全开，开启登录时预检会被before_request 401（同config X06）。下载：R087/R024/R025走`send_file`：Range→206、If-None-Match/If-Modified-Since→304、`Accept-Ranges`、`Last-Modified`；ETag值是Werkzeug实现细节，只比有无与状态码，差异记O12。 | 上列GET路由；OPTIONS取R026、R100、R004 | [app.py:34](../../backend/app.py#L34)、[routes/agents.py:946](../../backend/routes/agents.py#L946)、[routes/agents.py:1606](../../backend/routes/agents.py#L1606) |
| X12 并发与锁（waitress 8线程） | 同一Agent两个并发push-config→一个成功、一个409（`Agent already has an unfinished deployment`或`Deployment preparation already running`，以Python为准）；同deployment_id重放→返回已有记录；push与upgrade/start交错；push与带profile变化的重注册交错；delete与heartbeat交错；HA开启双击；网关安装publish双击；规则同步循环与手动push交错；部署历史只保留30条。准备锁是`.agent-deployment-locks/<sha256>`的`flock`，进程死亡即释放：`kill -9`后重启，再轮询`preparing`记录的结果。长请求（Mihomo核心升级200秒、域名探测200秒、上传120秒+busy重试）占满8个线程时其余请求排队——不是差分项，交O10。 | R003、R014–R016、R026、R050、R063、R078、R095、R102 | [manager.py:465](../../backend/agents/manager.py#L465)、[manager.py:491](../../backend/agents/manager.py#L491)、[http_server.py:21](../../backend/http_server.py#L21) |
| X13 fake Agent按端隔离 | 两端各自一套fake（分别在两个network namespace里，地址相同，所以种子DB逐字节相同）；**禁止两端共用一个fake**——它分不清是哪个面板发来的请求，两端的部署/升级副作用会混在一起。注册产生的随机id/token每端不同，fake按端保存身份并用各自凭据心跳、上报、拉配置；比较时用id/token双射。fake的时钟、deployment journal、update_id确定化；每端transcript单独记录、成对比较。 | 全部W协议用例的前提 | [manager.py:171](../../backend/agents/manager.py#L171)、[manager.py:705](../../backend/agents/manager.py#L705) |
| X14 地址分类边界（文档网段陷阱） | 校验器用Python `ipaddress`属性：`is_reserved`（240.0.0.0/4）、`is_link_local`、`is_multicast`、`is_unspecified`、广播`255.255.255.255`、Docker网段（注册用字符串解析`172.17–31`，地址跟随用`172.16.0.0/12且第二段17–31`，两处实现不同）。Go `netip`没有`IsReserved`，`IsPrivate`也不含192.0.2.0/24。每个校验器取：`240.0.0.1`、`255.255.255.255`、`169.254.1.1`、`224.0.0.1`、`0.0.0.0`、`127.0.0.2`、`172.16.0.1`、`172.17.0.1`、`172.31.255.254`、`172.32.0.1`、`::ffff:192.0.2.10`、`192.168.001.1`（前导0）、带zone的IPv6。fixture只用192.0.2.0/24、198.51.100.0/24时这些分支一条都不会走到。 | R007、R026（host上报）、R033、R050、R086、R091、R094、R102 | [address_follow.py:36](../../backend/agents/address_follow.py#L36)、[gateway_installation.py:299](../../backend/agents/gateway_installation.py#L299)、[install_script.py:30](../../backend/agents/install_script.py#L30)、[docker_ha_guide.py:389](../../backend/agents/docker_ha_guide.py#L389)、[linux_gateway.py:240](../../backend/agents/linux_gateway.py#L240) |
| X15 生产前置Nginx | 生产所有`/api/`请求先经Nginx：`client_max_body_size`默认1MiB（只有OxiDNS settings/render是96MiB）→traffic-history的4MiB上限实际不可达，1MiB以上由Nginx回HTML 413；请求体被缓冲，chunked到Flask时已带Content-Length（R080的“无Content-Length→413”在生产不可达）；`Host $host`去掉端口，`X-Forwarded-Host $host`、`X-Forwarded-Proto $scheme`被强制覆盖，`X-Forwarded-For`在客户端值后追加；`proxy_read_timeout 300s`；JSON响应gzip。Go直接对外服务、不再有Nginx，所以Go应匹配的是“Nginx+Flask”组合。Step B对本行用生产镜像的nginx.conf在.32容器里前置Python；与裸waitress结果不同的地方逐条列出，Go选择哪一边记O12（1MiB上限、Host端口是用户可见的）。 | R026、R072、R074、R080、R102（XFF）、R100/R063（XFH/XFP）、R010（Host端口）、一个>300秒的长请求 | [docker/nginx.conf:108](../../docker/nginx.conf#L108)、[docker/nginx.conf:92](../../docker/nginx.conf#L92) |

### [O6] 补充长序列

- **T25 凭据重放与过期**：Agent token没有轮换和过期，抓到的心跳可无限重放（O9记录，不是差分项）。差分项：删除后旧token对心跳/拉配置/上报/upgrade都401；删除后同name/host再注册得到新id/token，旧token仍401；另一端（Go/Python）发的token在本端401；首注册响应丢失后用同name/host且不带token重放→401+注册被拒通知（不是第二条记录）；安装capability过期后重放注册；退役rule_proxy_token用于linux-gateway provider→401。 来源 [manager.py:196](../../backend/agents/manager.py#L196)、[routes/agents.py:641](../../backend/routes/agents.py#L641)。
- **T26 面板重启时丢失的内存状态**：在每个流程中途重启两端（SIGTERM一次、`kill -9`一次），重启后比较：在线状态（X07）、地址跟随的120秒节流与待解决通知（`_attempts`/`_reported`）、`_preparing_deployments`与flock、上线/离线日志的已知集合（重启后不重复报“恢复在线”）、OxiDNS排行60秒复用缓存、内容比较缓存、metrics内存历史（SIGTERM会保存，kill -9丢最后一段）。 来源 [address_follow.py:30](../../backend/agents/address_follow.py#L30)、[presence_log.py:1](../../backend/agents/presence_log.py#L1)、[metrics_flush.py:196](../../backend/agents/metrics_flush.py#L196)。

## [O6] 逐路由补充用例

编号沿用各路由的S/F/B，从90开始，原有编号不变。

| 路由 | 新增用例 | 来源 |
|---|---|---|
| R001 | **B90** 经R102注册version=123/null的Agent后，完整列表`has_update`抛异常→Python 500 HTML；`summary=1`仍200<br>**B91** AGENTS_STATIC_DIR放三架构合成ELF（魔数`\x7fELF`）→`upgrade_available`真；非ELF或>128MiB→假<br>**B92** 状态按X07新鲜度：注册后t=130s无心跳→offline；未来时间种子→online | [routes/agents.py:970](../../backend/routes/agents.py#L970)、[upgrades.py:49](../../backend/agents/upgrades.py#L49) |
| R002 | **S90** 成功体`data.token`为`"[REDACTED]"`（X01），DB里的token能用于心跳<br>**F90** Content-Type text/plain→`request.json`抛415被捕获→500 JSON | [routes/agents.py:980](../../backend/routes/agents.py#L980) |
| R003 | **S90** 删除后：runtime清除、旧token心跳401、metrics历史与traffic_history行删除、traffic/probes/regions删除；该token不再在其他响应里被脱敏 | [manager.py:400](../../backend/agents/manager.py#L400)、[routes/agents.py:1022](../../backend/routes/agents.py#L1022) |
| R004 | **B90** `X-ConfigFlow-Profile`指向不存在profile→仍200，响应无该头（X02a） | [profile_context.py:32](../../backend/common/profile_context.py#L32) |
| R005 | **F90** text/plain→500 JSON（同R002 F90）<br>**B90** auto_backup开启时改name/host/enabled安排WebDAV上传（X10） | [routes/agents.py:1005](../../backend/routes/agents.py#L1005) |
| R010 | **B90** Host带/不带端口、IPv6字面量、缺失→生成内容里的面板URL随之变化；X-Forwarded-*不影响<br>**B91** HEAD同样生成（X11）<br>**B92** 种子两台Agent token相同→第二台401（`get_agent_by_token`取第一条）<br>**B93** query token含非ASCII/空格（无Bearer字符集限制）→401 `Invalid token` | [routes/agents.py:886](../../backend/routes/agents.py#L886)、[manager.py:315](../../backend/agents/manager.py#L315) |
| R013 | **B90** HEAD触发出站`GET /api/deployments/<id>`和记录更新 | [manager.py:746](../../backend/agents/manager.py#L746) |
| R023 | **B90** body `base_url`任意字符串（换行、引号、`javascript:`）原样进入生成记录；未给时用host_url | [routes/agents.py:1571](../../backend/routes/agents.py#L1571) |
| R026 | **F90** 深嵌套JSON→RecursionError被外层捕获→500 JSON<br>**F91** text/plain且≤32KiB→400 `Invalid heartbeat data`；>32KiB仍先413<br>**B90** 非百分比指标`1024.0`/`1e3`/`true`拒绝，百分比`25.5`接受<br>**B91** UTF-8 BOM、UTF-16 body被接受<br>**B92** 重复键后者生效<br>**B93** 只有version变化时提交配置但不安排WebDAV上传；只有service_status/metrics变化不提交 | [routes/agents.py:86](../../backend/routes/agents.py#L86)、[routes/agents.py:668](../../backend/routes/agents.py#L668)、[manager.py:452](../../backend/agents/manager.py#L452) |
| R034 | **B90** 日志行含本/他Agent token与rule_proxy_token（原文、`%`、`+`编码）→行内替换，替换不净的行整行`[REDACTED]`，不是整串清空 | [config_export.py:137](../../backend/common/config_export.py#L137) |
| R042 | **B90** HEAD也发出站请求<br>**B91** resource含`..`、`%2F`、`//`、尾斜杠，白名单按Werkzeug解码后的路径判断 | [mihomo_panel.py:298](../../backend/agents/mihomo_panel.py#L298) |
| R044 | **B90** `POST /<id>/mihomo/rule-targets`落到桥接→白名单404，不调用client_tools | [routes/agents.py:2199](../../backend/routes/agents.py#L2199) |
| R047 | **B90** `GET /<id>/mihomo/rule-targets/apply`落到桥接GET→白名单404 | [routes/client_tools.py:78](../../backend/routes/client_tools.py#L78) |
| R052 | **B90** `POST /<id>/oxidns/cache`→405与`Allow` | [routes/agents.py:2251](../../backend/routes/agents.py#L2251) |
| R058 | **B90** HEAD写排行/dashboard状态 | [oxidns_monitor.py:488](../../backend/agents/oxidns_monitor.py#L488) |
| R063 | **B90** base_url：body值；只给XFH；只给XFP；都不给（X06）<br>**B91** 同一Agent两个并发push→一个409（X12）<br>**B92** 第31个部署后最旧记录被裁掉 | [routes/agents.py:1785](../../backend/routes/agents.py#L1785)、[manager.py:539](../../backend/agents/manager.py#L539) |
| R065 | **B90** 值含rule_proxy_token→`without_stored`片段级`[REDACTED]`，钩子不再整串清空 | [routes/agents.py:1721](../../backend/routes/agents.py#L1721) |
| R072 | **F90** 深嵌套→未捕获RecursionError→Werkzeug HTML 500<br>**F91** text/plain且未超限→400<br>**B90** >1MiB经生产Nginx→Nginx HTML 413（X15） | [routes/agents.py:782](../../backend/routes/agents.py#L782) |
| R074 | **F90** 深嵌套→HTML 500<br>**F91** text/plain→400 | [routes/agents.py:729](../../backend/routes/agents.py#L729) |
| R079 | **S90** Agent分支返回原始`latest_upgrade`（未经`public_upgrade_state`），再过X01钩子<br>**F90** 管理员分支的401消息来自require_auth（`token_expired`/`token_invalid`），不是before_request那套<br>**F91** 认证关闭且无凭据→管理员poll并发出站请求 | [routes/agents.py:1198](../../backend/routes/agents.py#L1198)、[common/auth.py:123](../../backend/common/auth.py#L123) |
| R080 | **F90** Content-Length正确的坏JSON→Werkzeug HTML 400<br>**F91** text/plain→HTML 415<br>**F92** chunked：裸waitress与经Nginx各一例，以Python为准 | [routes/agents.py:1224](../../backend/routes/agents.py#L1224) |
| R083 | **B90** 端口`type=int`：非法值静默默认、` 80`/`+80`/`8_0`被接受<br>**B91** `enable_*`经`str_to_bool`的宽松取值 | [routes/agents.py:1278](../../backend/routes/agents.py#L1278) |
| R084 | **B90** 同R083 B90<br>**B91** 同R083 B91 | [routes/agents.py:1313](../../backend/routes/agents.py#L1313) |
| R085 | **F90** 空body→400 `安装请求需要 JSON 对象。`（gateway enroll空body视为`{}`）<br>**S90** body `{}`配text/plain也接受<br>**S91** 成功体顶层token不脱敏；失败体中出现的token被脱敏 | [docker_ha_installation.py:14](../../backend/routes/docker_ha_installation.py#L14) |
| R086 | **F90** generate+new且`X-ConfigFlow-Profile`不存在→全局404 `Profile not found`、无no-store<br>**F91** 非法profile id→400+no-store | [docker_ha_installation.py:51](../../backend/routes/docker_ha_installation.py#L51) |
| R087 | **B90** Range→206、If-None-Match/If-Modified-Since→304，只比状态与头的有无<br>**B91** HEAD | [routes/agents.py:946](../../backend/routes/agents.py#L946) |
| R091 | **B90** `server_url`与系统server_domain不一致→409 `server_url_mismatch`<br>**B91** `http://127.1`、`http://[::1]`、`http://0`等以Python判定为准 | [gateway_installation.py:101](../../backend/agents/gateway_installation.py#L101)、[gateway_installation.py:132](../../backend/agents/gateway_installation.py#L132) |
| R093 | **F90** 空body→视为`{}`→400 `请提交本次安装的两个 Agent 身份。` | [gateway_installations.py:16](../../backend/routes/gateway_installations.py#L16)、[gateway_installation.py:368](../../backend/agents/gateway_installation.py#L368) |
| R095 | **B90** 并发两次publish→只有一个worker（X12） | [gateway_installation.py:629](../../backend/agents/gateway_installation.py#L629) |
| R098 | **B90** `GET /api/agents/i/{status,config,logs,metrics,upgrade,address}`进入短链（文本404），不进入`/<agent_id>/…`（X04） | [routes/agents.py:381](../../backend/routes/agents.py#L381) |
| R100 | **F90** install_core=false时`port=abc`→静默8080并200（更正F05）<br>**B90** `deployment_health_timeout=abc`→30<br>**B91** `port= 80`/`+80`/`8_0`/全角数字被接受<br>**B92** server_url：显式参数＞XFP与XFH同时存在＞url_root；只给其一时忽略 | [routes/agents.py:406](../../backend/routes/agents.py#L406)、[routes/agents.py:455](../../backend/routes/agents.py#L455) |
| R102 | **F90** Content-Type缺失或text/plain也解析（`force=True`）<br>**F91** 深嵌套→500 JSON<br>**B90** version整数/null、port字符串照存（后果见R001 B90）<br>**B91** host替换规则（X06）：`::1`、`172.16.0.5`、`172.32.0.1`不替换；`172.17.0.1`、`172.31.255.1`、`127.0.0.2`、`localhost`、空→XFF第一个元素或remote_addr<br>**B92** 重注册把`enabled`重置为true、缺version写`1.0.0`、复制列表以外的字段（如`logging_config`）丢失<br>**B93** 设置AGENT_REGISTRATION_KEY时，带安装capability的注册不需要key<br>**B94** 被拒注册写`agent-registration`通知 | [routes/agents.py:547](../../backend/routes/agents.py#L547)、[manager.py:196](../../backend/agents/manager.py#L196)、[notifications.py:108](../../backend/notifications.py#L108) |
| R050 | **B90** HA PUT的base_url取host_url（Host头），进入HA发布载荷 | [routes/agents.py:200](../../backend/routes/agents.py#L200) |
| R104–R108、R111–R118 | 每条各两例：**F90** 未登录+不存在profile（R104–R108用header/`profile`/`profile_id`；R111–R118用路径）→404 `Profile not found: <id>`，先于401<br>**F91** 未登录+非法id→400 `Invalid profile id` | [routes/__init__.py:113](../../backend/routes/__init__.py#L113) |
| R109、R110、R119、R120 | 每条各两例：**F90** 公开provider+不存在profile→404，先于token校验<br>**F91** 非法id→400 | [routes/auth.py:63](../../backend/routes/auth.py#L63)、[profile_context.py:90](../../backend/common/profile_context.py#L90) |

## Python逐错误/诊断分支索引 H

每个H是一条源码错误位置的配方：用合法SEED和W合约起步，单独让所列guard成立，再通过所列公开请求驱动，比较其外部错误、降级、部分结果及副作用。源码键Kxx定位文件，`Kxx:行号`精确定位分支；完整handler范围已在R表提供。一个`return False/None`可能只过滤一条坏采样、标unknown或给UI capability，不一律映射HTTP400；最终行为按调用路由断言。循环字段及复合guard逐条件参数化。动态message变量和错误code要解析同文件MESSAGES/模板，中文提示不应被通用归一吞掉。

**可达性界限**：这里刻意保留模板约定、纯内部参数、文件/事务故障和回执竞争的审计配方；它们不自动等于可执行HTTP用例。不能在Step B调用Go私有函数来“覆盖”。Opus需决定从哪条公开动作+隔离fixture可触发；无法触发则标N/A（理由）或实验网，而不是把它算通过。旧LEGACY_DIRS采用被维护者取消，不测它；生成Python/Shell字符串内部运行期guard不由AST索引宣称穷尽，归G和L1实际执行审查。

源码中的示例地址在此索引被替成文档网段（**[O6]** 更正：条件里的字面量和用户可见消息里的示例不能替换——H0003、H0197的条件因此被改错，H0359、H0574、H0895、H0973的消息与Python不一致，已恢复源码原文；它们不是家庭网络地址），实际中文错误期望仍由各自Python oracle在合成fixture下产生；不把真实家庭IP、token、订阅域名写进文档。过长条件/表达式列摘要，完整内容以精确源行复核。

### K01 [backend/agents/address_follow.py](../../backend/agents/address_follow.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0001 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:37 校验/降级 | `not isinstance(value, str)` → `None` |
| H0002 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:41 校验/降级 | `捕获 ValueError` → `None` |
| H0003 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:45 校验/降级 | `address.is_loopback or address.is_link_local or address.is_multicast or address.is_unspecified or address.is_reserved or (address in ipaddress.IPv4Network('172.16.0.0/12') and 17 <= address.…（完整见源行）` → `None` |
| H0004 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:74 校验/降级 | `捕获 requests.exceptions.RequestException` → `False` |
| H0005 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:184 错误结果 | `host is None` → `({'success': False, 'message': '请输入这台设备的局域网 IPv4 地址'}, 400)` |
| H0006 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:187 错误结果 | `_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)` → `({'success': False, 'message': HA_EDIT}, 409)` |
| H0007 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:190 错误结果 | `any((other['id'] != agent_id and other.get('name') == agent.get('name') and (other.get('host') == host) for other in manager.get_all_agents()))` → `({'success': False, 'message': f'另一条同名的 Agent 记录已经在用 {host}'}, 409)` |
| H0008 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:196 错误结果 | `status is None` → `({'success': False, 'message': f"面板连不上 {host}:{agent.get('port')}，没有修改。请确认地址正确，且面板所在的机器能访问它。"}, 502)` |
| H0009 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:198 错误结果 | `status not in (200, 404)` → `({'success': False, 'message': f"{host}:{agent.get('port')} 有应答，但不是这台 Agent（凭据不匹配），没有修改。"}, 502)` |
| H0010 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:201 错误结果 | `mode == 'env'` → `({'success': False, 'message': message}, 409)` |
| H0011 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:206 错误结果 | `mode == 'agent' / status != 200` → `({'success': False, 'message': reply.get('message') or f"设备没有接受这个地址（{status or '无应答'}），没有修改。"}, 400 if status == 400 else 502)` |
| H0012 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:212 错误结果 | `host != old / not manager.update_agent(agent_id, {'host': host})` → `({'success': False, 'message': 'Agent not found'}, 404)` |
| H0013 / PUT /api/agents/<agent_id>/address（自动follow由heartbeat） | K01:214 错误结果 | `host != old / 捕获 AgentDeploymentConflict` → `({'success': False, 'message': str(conflict)}, 409)` |

### K02 [backend/agents/config_generator.py](../../backend/agents/config_generator.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0014 / R063 | K02:37 抛出 | `not (service_type == 'oxidns') / not (service_type == 'surge')` → `ValueError(f'Unsupported service type: {service_type}')` |

### K03 [backend/agents/content_sync.py](../../backend/agents/content_sync.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0015 / R065 | K03:384 抛出 | `change not in _CHANGES` → `KeyError(change)` |

### K04 [backend/agents/core_install_script.py](../../backend/agents/core_install_script.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0016 / R099 | K04:38 抛出 | `installer_forwarding / script.count(_CORE_FORWARDING_BLOCK) != 1` → `RuntimeError('install-cores.sh forwarding block changed; review the installer-managed text')` |

### K05 [backend/agents/deployment_bundle.py](../../backend/agents/deployment_bundle.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0017 / R063 | K05:75 诊断 | `isinstance(exc, requests.exceptions.Timeout)` → `'下载超时，请检查网络连接或稍后重试'` |
| H0018 / R063 | K05:77 诊断 | `isinstance(exc, requests.exceptions.ProxyError)` → `'代理连接失败，请检查规则下载代理是否可用'` |
| H0019 / R063 | K05:79 诊断 | `isinstance(exc, requests.exceptions.SSLError)` → `'无法建立 HTTPS 安全连接，请检查规则源及其证书'` |
| H0020 / R063 | K05:81 诊断 | `isinstance(exc, requests.exceptions.ConnectionError)` → `'连接失败，请检查规则下载地址及网络连接'` |
| H0021 / R063 | K05:83 诊断 | `isinstance(exc, requests.exceptions.HTTPError) and exc.response is not None` → `f'下载服务器返回错误（HTTP {exc.response.status_code}），请检查规则下载地址或稍后重试'` |
| H0022 / R063 | K05:84 诊断 | `外部依赖/文件/事务异常` → `'下载失败，请检查规则下载地址及网络连接'` |
| H0023 / R063 | K05:170 抛出 | `not isinstance(value, str) or not value or '\\' in value or any((ord(c) < 32 for c in value))` → `DeploymentPreparationError('Invalid deployment file path')` |
| H0024 / R063 | K05:175 抛出 | `path.is_absolute() or not path.parts or any((p in ('..', '.', '') for p in value.split('/')))` → `DeploymentPreparationError('Deployment files must use safe relative paths')` |
| H0025 / R063 | K05:177 抛出 | `value.startswith('.configflow') or value == 'manifest.json'` → `DeploymentPreparationError('Reserved deployment file path')` |
| H0026 / R063 | K05:204 抛出 | `not isinstance(manifest, dict) or type(manifest.get('protocol_version')) is not int or manifest['protocol_version'] != 1 or (manifest.get('service_type') not in ('mihomo', 'mosdns', 'oxidns'…（完整见源行）` → `DeploymentPreparationError('Invalid deployment content manifest')` |
| H0027 / R063 | K05:209 抛出 | `path != value or '\x7f' in path` → `DeploymentPreparationError('Manifest paths must be canonical relative paths')` |
| H0028 / R063 | K05:214 抛出 | `'/' in config_path` → `DeploymentPreparationError('Agent configuration filename must be a basename')` |
| H0029 / R063 | K05:217 抛出 | `not isinstance(entries, list) or not entries or len(entries) > MAX_FILES` → `DeploymentPreparationError('Manifest must contain a complete deployment file list')` |
| H0030 / R063 | K05:223 抛出 | `for item in entries / not isinstance(item, dict)` → `DeploymentPreparationError('Invalid deployment manifest file')` |
| H0031 / R063 | K05:229 抛出 | `for item in entries / not isinstance(role, str) or role not in ('config', 'provider', 'rule', 'custom') or type(size) is not int or (not 0 <= size <= MAX_FILE_BYTES) or (not isinstance(diges…（完整见源行）` → `DeploymentPreparationError('Invalid deployment manifest file metadata')` |
| H0032 / R063 | K05:231 抛出 | `for item in entries / path in paths` → `DeploymentPreparationError('Duplicate deployment manifest path')` |
| H0033 / R063 | K05:236 抛出 | `role == 'config' / path != config_path or size == 0` → `DeploymentPreparationError('Invalid main configuration manifest entry')` |
| H0034 / R063 | K05:239 抛出 | `not (role == 'config') / path == config_path` → `DeploymentPreparationError('Main configuration entry must have config role')` |
| H0035 / R063 | K05:241 抛出 | `for item in entries / path in canonical_paths` → `DeploymentPreparationError('Conflicting canonical deployment paths')` |
| H0036 / R063 | K05:245 抛出 | `for item in entries / total_size > MAX_TOTAL_BYTES` → `DeploymentPreparationError('Deployment exceeds size limit')` |
| H0037 / R063 | K05:248 抛出 | `config_count != 1` → `DeploymentPreparationError('Manifest must contain exactly one main configuration')` |
| H0038 / R063 | K05:274 抛出 | `'/api/' in path or path.startswith('/rules/local/') or path.startswith('/rule-library/content/')` → `DeploymentPreparationError('Rule callback is not part of this profile snapshot')` |
| H0039 / R063 | K05:280 抛出 | `not url` → `DeploymentPreparationError('Required rule has no source URL')` |
| H0040 / R063 | K05:289 抛出 | `for chunk in response.iter_content(chunk_size=64 * 1024) / len(value) > MAX_FILE_BYTES` → `DeploymentPreparationError('Rule exceeds deployment file size limit')` |
| H0041 / R063 | K05:295 抛出 | `捕获 Exception` → `RuleDownloadError(f'{_download_error_message(exc)}\n原始错误：{details}', remedy=_download_remedy(url, exc, config))` |
| H0042 / R063 | K05:305 抛出 | `depth > 3` → `DeploymentPreparationError('Recursive rule callback')` |
| H0043 / R063 | K05:314 抛出 | `internal and parsed.path.endswith(('/mosdns/rule-proxy', '/oxidns/rule-proxy')) / not original` → `DeploymentPreparationError('Rule conversion callback has no source')` |
| H0044 / R063 | K05:342 抛出 | `for source in (False, True) / 捕获 UnicodeError` → `DeploymentPreparationError('Gateway rules must be UTF-8 text')` |
| H0045 / R063 | K05:347 抛出 | `for source in (False, True) / 捕获 LinuxGatewayError` → `DeploymentPreparationError(str(exc))` |
| H0046 / R063 | K05:354 抛出 | `rule is not None / source == url` → `DeploymentPreparationError('Rule callback refers to itself')` |
| H0047 / R063 | K05:374 抛出 | `捕获 RemoteRuleValidationError` → `DeploymentPreparationError(str(exc))` |
| H0048 / R063 | K05:401 抛出 | `rule_type not in ('domain_set', 'ip_set')` → `DeploymentPreparationError('Unsupported MosDNS rule plugin type')` |
| H0049 / R063 | K05:418 抛出 | `捕获 UnicodeError` → `DeploymentPreparationError('MosDNS requires text rules; binary MRS is only supported by Mihomo')` |
| H0050 / R063 | K05:420 抛出 | `'\x00' in text` → `DeploymentPreparationError('MosDNS rule source is binary')` |
| H0051 / R063 | K05:428 抛出 | `yaml_payload / not isinstance(lines, list)` → `ValueError` |
| H0052 / R063 | K05:430 抛出 | `yaml_payload / 捕获 Exception` → `source_error('Invalid rule YAML payload', text, cause=exc, payload_index=-1 if not isinstance(exc, yaml.YAMLError) else None, source_label='转换前的规则内容')` |
| H0053 / R063 | K05:435 抛出 | `yaml_payload / invalid is not None` → `source_error('Invalid rule YAML payload', text, payload_index=invalid, source_label='转换前的规则内容')` |
| H0054 / R063 | K05:461 抛出 | `rule_type == 'ip_set' / 捕获 ValueError` → `invalid_line('Invalid IP network in MosDNS ip_set rule source', index)` |
| H0055 / R063 | K05:483 抛出 | `捕获 ValueError / not expression` → `invalid_line('Unrecognized MosDNS rule source content', index)` |
| H0056 / R063 | K05:495 抛出 | `local_dns_watchdog and service in ('mosdns', 'oxidns') / 捕获 ValueError` → `preparation_context(DeploymentPreparationError(str(exc)), name=service, path='config.yaml', kind='配置')` |
| H0057 / R063 | K05:508 抛出 | `service not in ('mihomo', 'mosdns', 'oxidns')` → `DeploymentPreparationError('Unsupported transactional deployment service')` |
| H0058 / R063 | K05:512 抛出 | `捕获 yaml.YAMLError` → `preparation_context(source_error('生成配置的 YAML 格式不正确', config_content, cause=exc), name=service, path='config.yaml', kind='配置')` |
| H0059 / R063 | K05:515 抛出 | `not isinstance(main, dict)` → `DeploymentPreparationError('Generated configuration must be a YAML mapping')` |
| H0060 / R063 | K05:517 抛出 | `service == 'oxidns' and main.get('include')` → `DeploymentPreparationError('OxiDNS deployment must inline included plugin definitions')` |
| H0061 / R063 | K05:526 抛出 | `for plugin in main.get('plugins', []) / not isinstance(args, dict) or not isinstance(args.get('files', []), list)` → `DeploymentPreparationError('MosDNS file references must be a list')` |
| H0062 / R063 | K05:534 抛出 | `for reference in args.get('files', []) / path in rule_types and rule_types[path] != rule_type` → `DeploymentPreparationError('MosDNS file is used by both domain_set and ip_set: ' + path)` |
| H0063 / R063 | K05:543 抛出 | `service == 'oxidns' and (path == 'state' or path.startswith('state/'))` → `DeploymentPreparationError('OxiDNS runtime state cannot be managed by a deployment')` |
| H0064 / R063 | K05:546 抛出 | `not isinstance(value, bytes)` → `DeploymentPreparationError('Missing deployment file content')` |
| H0065 / R063 | K05:553 抛出 | `len(value) > MAX_FILE_BYTES` → `DeploymentPreparationError('Deployment file exceeds size limit')` |
| H0066 / R063 | K05:555 抛出 | `path in files` → `DeploymentPreparationError('Duplicate deployment path: ' + path)` |
| H0067 / R063 | K05:557 抛出 | `len(files) >= MAX_FILES or sum((len(item[0]) for item in files.values())) + len(value) > MAX_TOTAL_BYTES` → `DeploymentPreparationError('Deployment exceeds size limit')` |
| H0068 / R063 | K05:567 抛出 | `捕获 DeploymentPreparationError` → `preparation_context(exc, name=name, path=path, kind=kind)` |
| H0069 / R063 | K05:582 抛出 | `for item in pinned_files / item.get('role') not in ('rule', 'custom', 'provider')` → `DeploymentPreparationError('Invalid pinned deployment file role')` |
| H0070 / R063 | K05:593 抛出 | `for (path, (content, role)) in files.items() / 捕获 (yaml.YAMLError, UnicodeError, RecursionError)` → `DeploymentPreparationError('Linux 网关无法检查订阅节点，请先修复 Provider YAML')` |
| H0071 / R063 | K05:595 抛出 | `for (path, (content, role)) in files.items() / _routing_mark_path(provider)` → `DeploymentPreparationError('Linux 网关与订阅节点中的 routing-mark 冲突，请移除后重新发布')` |
| H0072 / R063 | K05:607 抛出 | `for (name, definition) in main['rule-providers'].items() / not definition.get('path')` → `DeploymentPreparationError(f'Rule provider {name} has no bundled path')` |
| H0073 / R063 | K05:612 抛出 | `for (name, definition) in main['rule-providers'].items() / not definition.get('url')` → `DeploymentPreparationError(f'Rule provider {name} references an unbundled local file; use a managed rule source')` |
| H0074 / R063 | K05:697 抛出 | `捕获 RemoteRuleValidationError` → `preparation_context(DeploymentPreparationError(str(exc)), name=item.get('name', ''), path=item.get('local_path', ''))` |
| H0075 / R063 | K05:702 抛出 | `捕获 RuleDownloadError` → `RuleDownloadError(f'无法下载「{name}」规则集：{explanation}。本次发布已停止，设备仍使用原配置。' + f"\n文件：{failure_text(str(item.get('local_path', '')))}" + '\n修复：更换该规则的下载地址，或检查「GitHub 代理域名」和「系统设置 → 规则下载代理」。' + '\n也可手动下载后导入规则库的内容型规则，再重新发布。' + (f'\n{details}' if details else '')…（完整见源行）` |
| H0076 / R063 | K05:709 抛出 | `捕获 DeploymentPreparationError` → `preparation_context(exc, name=item.get('name', ''), path=item.get('local_path', ''))` |
| H0077 / R063 | K05:734 抛出 | `for (name, provider) in (main.get(section) or {}).items() / 'path' not in provider` → `DeploymentPreparationError(f'Provider {name} has no bundled path')` |
| H0078 / R063 | K05:737 抛出 | `for (name, provider) in (main.get(section) or {}).items() / path not in files` → `DeploymentPreparationError(f'Provider {name} has no bundled file')` |
| H0079 / R063 | K05:749 抛出 | `捕获 OxidnsSettingsError / hints` → `DeploymentPreparationError('OxiDNS 还在使用不可用的规则集，发布已停止，设备配置没有改动。' + ''.join((f'\n· {hint}' for hint in hints)))` |
| H0080 / R063 | K05:752 抛出 | `捕获 OxidnsSettingsError / missing` → `DeploymentPreparationError(f'OxiDNS 配置用到了文件 {missing[0]}，但附加文件里没有它，发布已停止，设备配置没有改动。\n请在 OxiDNS 设置的「附加文件」中添加它，或在 YAML 中删除这个引用。')` |
| H0081 / R063 | K05:755 抛出 | `service == 'oxidns' / 捕获 OxidnsSettingsError` → `DeploymentPreparationError(str(exc))` |
| H0082 / R063 | K05:763 抛出 | `for plugin in main.get('plugins', []) / not isinstance(paths, list)` → `DeploymentPreparationError('MosDNS file references must be a list')` |
| H0083 / R063 | K05:767 抛出 | `for (index, value) in enumerate(paths) / path not in files` → `DeploymentPreparationError('MosDNS references an unbundled file: ' + path)` |
| H0084 / R063 | K05:816 抛出 | `'/' in path` → `DeploymentPreparationError('Agent configuration filename must be a basename')` |
| H0085 / R063 | K05:822 抛出 | `any((item['path'] == path for item in manifest['files']))` → `DeploymentPreparationError('Agent configuration filename collides with a managed file')` |

### K06 [backend/agents/dns_follow.py](../../backend/agents/dns_follow.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0086 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:65 抛出 | `捕获 BlockingIOError` → `FollowError('DNS 联动正在处理，请稍后重试')` |
| H0087 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:76 抛出 | `not agent or agent.get('service_type') != service or (not agent.get('enabled', True))` → `FollowError(f'{service} Agent 不存在、已停用或类型不符')` |
| H0088 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:111 抛出 | `parsed.scheme not in ('udp', 'tcp') or not parsed.hostname or parsed.username or parsed.password or (parsed.path not in ('', '/')) or parsed.query or parsed.fragment` → `FollowError('仅普通 UDP / TCP DNS 上游可以跟随 Mihomo')` |
| H0089 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:119 抛出 | `外部依赖/文件/事务异常` → `FollowError('绑定的 OxiDNS 上游已移除，请重新选择绑定')` |
| H0090 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:137 抛出 | `not isinstance(value, str) or not value or len(value) > 253 or (value != value.strip())` → `FollowError('请填写 OxiDNS 可以访问的 Mihomo IP 或主机名，不含协议和端口')` |
| H0091 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:144 抛出 | `捕获 ValueError / not re.fullmatch('(?=.{1,253}\\Z)[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?', value) or any((not label or len(label) > 63 or label.startswith('-') or label.endswith('-') for l…（完整见源行）` → `FollowError('Mihomo 地址格式无效；请填写 IP 或主机名，不含协议和端口')` |
| H0092 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:147 抛出 | `ip.is_loopback or ip.is_unspecified or ip.is_multicast` → `FollowError('跨设备连接必须使用可访问的地址，不能使用回环或通配地址')` |
| H0093 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:166 抛出 | `捕获 requests.RequestException` → `FollowError(f"{agent.get('name', 'Agent')} 无法连接，请检查网络后重试")` |
| H0094 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:171 抛出 | `not result.get('success') or result.get('http_status')` → `FollowError(f"{agent.get('name', 'Agent')}：{message}")` |
| H0095 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:178 抛出 | `peer.get('peer_agent_id') != source['id'] or peer.get('service_type') != 'mihomo'` → `FollowError('无法确认这两个 Agent 共用网络空间；请选择网络地址连接')` |
| H0096 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:203 抛出 | `active(target)` → `FollowError('DNS 主备配置正在同步，请等待两台发布完成后修改联动')` |
| H0097 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:205 抛出 | `(target.get('dns_follow_state') or {}).get('status') in _RUNNING` → `FollowError('OxiDNS 联动发布尚未确认结束，请等待结果后再修改绑定')` |
| H0098 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:211 抛出 | `not isinstance(payload, dict) or set(payload) - {'source_agent_id', 'upstream_tag', 'upstream_index', 'connection', 'address'}` → `FollowError('DNS 绑定参数无效')` |
| H0099 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:218 抛出 | `not isinstance(payload.get('upstream_tag'), str) or type(payload.get('upstream_index')) is not int or payload['upstream_index'] < 0 or (payload.get('connection') not in ('auto', 'network'))` → `FollowError('请选择上游和连接方式')` |
| H0100 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:221 抛出 | `(target.get('oxidns_ha') or {}).get('enabled') is True and binding['connection'] != 'network'` → `FollowError('DNS 主备必须通过同一个 Mihomo 网络地址联动，请选择网络地址连接')` |
| H0101 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:234 抛出 | `index is not None` → `FollowError(f'该上游已被策略规则 #{index} 固定为指定上游，请先改选后再绑定')` |
| H0102 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:243 抛出 | `_profile(manager, current) != profile_id or not current_source or _profile(manager, current_source) != binding['source_profile_id']` → `FollowError('Agent 配置绑定已改变，请刷新后重新设置')` |
| H0103 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:248 抛出 | `for other in agents / other['id'] != target_id and (other.get('dns_follow') or {}).get('target_profile_id') == profile_id` → `FollowError('此 OxiDNS 配置已有联动绑定，请使用独立配置或先解除另一绑定')` |
| H0104 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:302 抛出 | `before_save is None and (state.get('status'), state.get('message')) == (status, message_safe) and all((key in state and state[key] == value for (key, value) in fields.items())) and (events =…（完整见源行）` → `_Unchanged()` |
| H0105 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:330 抛出 | `not binding or state.get('status') != 'failed'` → `FollowError('只有已确认失败的 DNS 联动可以重试；执行中的任务会继续确认结果')` |
| H0106 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:334 抛出 | `child and child.get('status') not in _TERMINAL` → `FollowError('OxiDNS 发布结果尚未确认，请先恢复 Agent 连接')` |
| H0107 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:421 抛出 | `for (current, previous, service) in ((current_source, source, 'mihomo'), (current_target, target, 'oxidns')) / not current or current.get('service_type') != service or (not current.get('enab…（完整见源行）` → `FollowError(f'{service} Agent 不存在、已停用或类型不符')` |
| H0108 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:423 抛出 | `for (current, previous, service) in ((current_source, source, 'mihomo'), (current_target, target, 'oxidns')) / any((current.get(key) != previous.get(key) for key in ('host', 'port', 'token')…（完整见源行）` → `FollowError('Agent 连接信息已改变，请重试 DNS 联动')` |
| H0109 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:427 抛出 | `_profile(manager, current_source) != binding['source_profile_id'] or _profile(manager, current_target) != binding['target_profile_id'] or current_target.get('dns_follow') != binding` → `FollowError('Agent 的配置绑定已改变，请重新设置 DNS 联动')` |
| H0110 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:431 抛出 | `latest.get('deployment_id') != source_id or latest.get('status') != 'succeeded' or latest.get('manual_rollback')` → `FollowError('Mihomo 已开始新的发布或回滚，请等待最新结果后重试')` |
| H0111 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:439 抛出 | `current_target.get('applied_content') != baseline or current_target.get('config_version') != target.get('config_version') or any((latest_target.get(key) != previous_target.get(key) for key i…（完整见源行）` → `FollowError('OxiDNS 发布基准已变化，请重试')` |
| H0112 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:453 抛出 | `result.get('agent_id') != source['id'] or result.get('deployment_id') != deployment_id or result.get('enabled') is not True` → `FollowError('Mihomo DNS 尚未确认启用，请重新发布后重试')` |
| H0113 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:456 抛出 | `type(port) is not int or not 1 <= port <= 65535 or (not isinstance(host, str))` → `FollowError('Mihomo 返回的 DNS 监听地址无效')` |
| H0114 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:460 抛出 | `捕获 ValueError` → `FollowError('Mihomo DNS 监听地址必须为明确的 IP 地址')` |
| H0115 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:467 抛出 | `not (binding['connection'] == 'auto') / ip.is_loopback` → `FollowError('Mihomo 仅监听回环地址，跨设备 OxiDNS 无法访问；请修改 dns.listen 后重新发布')` |
| H0116 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:485 抛出 | `(target.get('latest_deployment') or {}).get('deployment_id') != state['target_deployment_id'] or (target.get('applied_content') or {}).get('deployment_id') != state['target_deployment_id'] o…（完整见源行）` → `FollowError('OxiDNS 当前发布或绑定已改变，未覆盖草稿，请核对设备状态后重试')` |
| H0117 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:492 抛出 | `_profile(manager, source) != binding['source_profile_id'] or source_state.get('deployment_id') != state['source_deployment_id'] or source_state.get('status') != 'succeeded' or source_state.g…（完整见源行）` → `FollowError('Mihomo 当前发布已改变或回滚，OxiDNS 联动未确认一致，请核对后重试')` |
| H0118 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:497 抛出 | `_digest(config.get('oxidns', {})) != job['original_settings_digest']` → `FollowError('发布期间 OxiDNS 草稿有新修改，已保留草稿；请核对后再发布')` |
| H0119 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:518 抛出 | `not child_id or not job` → `FollowError('联动准备被中断，请重试')` |
| H0120 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:526 抛出 | `child.get('status') in ('failed', 'rolled_back', 'rollback_failed') or child.get('http_status') == 404 / not (child.get('status') == 'rollback_failed')` → `FollowError(child.get('error_details') or child.get('error') or child.get('message') or 'OxiDNS 发布失败')` |
| H0121 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:541 抛出 | `not baseline.get('fingerprint') or baseline.get('profile_id') != binding['target_profile_id'] or baseline.get('service_type') != 'oxidns'` → `FollowError('请先手动成功发布一次 OxiDNS，建立当前配置基准后重试')` |
| H0122 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:547 抛出 | `_digest(entry) != binding['upstream_signature']` → `FollowError('绑定的上游已被编辑或移动，请重新选择绑定，避免覆盖自定义内容')` |
| H0123 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:551 抛出 | `active.get('agent_id') != target_id or active.get('deployment_id') != baseline.get('deployment_id')` → `FollowError('OxiDNS 当前配置与最后成功发布不一致，请先手动发布并确认')` |
| H0124 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:558 抛出 | `not _matches_applied_bundle(prepared['bundle'], config, baseline)` → `FollowError(_drift_message(prepared['bundle'], config, baseline))` |
| H0125 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:575 抛出 | `resources(prepared['bundle']) != resources(candidate['bundle'])` → `FollowError('OxiDNS 规则文件在准备期间发生变化，请先手动发布后重试')` |
| H0126 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:581 抛出 | `not result.get('success') or result.get('existing')` → `FollowError(result.get('message') or 'OxiDNS 还有其他发布任务，请等待结果后重试')` |
| H0127 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:631 抛出 | `not agent or agent.get('service_type') != service or (not agent.get('enabled', True))` → `FollowError(f'{service} Agent 不存在、已停用或类型不符')` |
| H0128 / PUT /api/agents/<agent_id>/dns-follow → 源发布后tick | K06:656 抛出 | `for target in targets / _profile(manager, target) != binding['target_profile_id'] or _profile(manager, source) != binding['source_profile_id']` → `FollowError('Agent 的配置绑定已改变，请重新设置 DNS 联动')` |

### K07 [backend/agents/docker_ha_compact.py](../../backend/agents/docker_ha_compact.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0129 / R086 | K07:125 抛出 | `any((data == root or data.startswith(root + '/') for root in system_roots)) or data in ('/var', '/var/lib', '/opt', '/srv', '/home', '/root', '/tmp') or re.fullmatch('/volume[0-9]+(?:/docker…（完整见源行）` → `ValueError('请为应用选择专用持久子目录，不能读写挂载系统目录或共享根目录')` |
| H0130 / R086 | K07:135 抛出 | `enrollment / not re.fullmatch('[0-9a-f]{32}', session) or not re.fullmatch('[A-Za-z0-9_-]{43}', enrollment['token'])` → `ValueError('安装凭据格式无效')` |

### K08 [backend/agents/docker_ha_compose.py](../../backend/agents/docker_ha_compose.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0131 / R086 | K08:298 抛出 | `identity == data or any((identity == data + '/' + part or identity.startswith(data + '/' + part + '/') for part in ('agent', 'oxidns', 'ha', '.ha-compose', '.ha-install', 'compose.yaml', 'pr…（完整见源行）` → `ValueError('宿主身份文件不能放在本实例的受管数据子目录内，请选择独立持久文件')` |

### K09 [backend/agents/docker_ha_compose_direct.py](../../backend/agents/docker_ha_compose_direct.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0132 / R086 | K09:22 抛出 | `any((data == root or data.startswith(root + '/') for root in system_roots)) or data in ('/var', '/var/lib', '/opt', '/srv', '/home', '/root', '/tmp') or re.fullmatch('/volume[0-9]+(?:/docker…（完整见源行）` → `ValueError('请为应用选择专用持久子目录，不能读写挂载系统目录或共享根目录')` |
| H0133 / R086 | K09:27 抛出 | `identity_mode == 'file' / identity_path == data or identity_path.startswith(data + '/')` → `ValueError('单文件 Compose 的现有宿主身份文件须位于应用数据目录之外')` |

### K10 [backend/agents/docker_ha_enrollment.py](../../backend/agents/docker_ha_enrollment.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0134 / R086 | K10:52 抛出 | `index.exists() / not re.fullmatch('[a-f0-9]{32}', session_id)` → `EnrollmentError('安装记录无法读取，请保留现场并检查服务端数据。', 409)` |
| H0135 / R086 | K10:56 抛出 | `index.exists() / 捕获 (FileNotFoundError, ValueError)` → `EnrollmentError('安装记录无法读取，请保留现场并检查服务端数据。', 409)` |
| H0136 / R085 | K10:81 抛出 | `not re.fullmatch('[a-f0-9]{32}', session_id or '') or not isinstance(token, str) or (not re.fullmatch('[A-Za-z0-9_-]{43}', token))` → `EnrollmentError('安装凭据无效，请重新生成安装命令。', 401)` |
| H0137 / R085 | K10:86 抛出 | `捕获 (FileNotFoundError, ValueError)` → `EnrollmentError('安装凭据无效，请重新生成安装命令。', 401)` |
| H0138 / R085 | K10:89 抛出 | `time.time() >= session['expires'] or not hmac.compare_digest(hashlib.sha256(token.encode()).hexdigest(), session['token_hash'])` → `EnrollmentError('安装凭据无效或已过期，请重新生成安装命令。', 401)` |
| H0139 / R085 | K10:98 抛出 | `owned / any((owned.get(key) != value for (key, value) in expected.items()))` → `EnrollmentError('本次安装的设备信息已改变，请重新生成命令。', 409)` |
| H0140 / R085 | K10:101 抛出 | `any((a.get('name') == expected['name'] and a.get('host') == expected['host'] for a in agents))` → `EnrollmentError('该名称与地址已有 Agent；请使用旧容器迁移，不能创建或覆盖其身份。', 409)` |

### K11 [backend/agents/docker_ha_guide.py](../../backend/agents/docker_ha_guide.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0141 / R086 | K11:21 抛出 | `not isinstance(value, str) or not value.startswith('/') or value == '/' or (os.path.normpath(value) != value) or any((c in value for c in (',\x00\r\n' if allow_colon else ',:\x00\r\n')))` → `ValueError(label + '须为规范的绝对路径，不能包含' + ('逗号或换行' if allow_colon else '逗号、冒号或换行'))` |
| H0142 / R086 | K11:27 抛出 | `not isinstance(value, str) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', value)` → `ValueError(label + '只能包含字母、数字、点、下划线或短横线')` |
| H0143 / R086 | K11:33 抛出 | `not isinstance(value, dict) or value.get('type') not in ('bind', 'volume')` → `ValueError(label + '请选择 bind 目录或 Docker 命名卷')` |
| H0144 / R086 | K11:252 抛出 | `not isinstance(old_agent, str)` → `ValueError('请填写原 Agent 完整名称')` |
| H0145 / R086 | K11:262 抛出 | `for right in directories[i + 1:] / os.path.commonpath((left, right)) in (left, right)` → `ValueError('Agent、OxiDNS、HA 和备份目录须彼此独立，不能相同或互相包含')` |
| H0146 / R086 | K11:264 抛出 | `agent == core` → `ValueError('Agent 和 OxiDNS 必须使用不同的数据挂载')` |
| H0147 / R086 | K11:379 抛出 | `not isinstance(request, dict)` → `ValueError('请提交 JSON 对象')` |
| H0148 / R086 | K11:390 抛出 | `action == 'verify' / vip.is_unspecified or vip.is_loopback or vip.is_multicast or vip.is_reserved or vip.is_link_local` → `ValueError()` |
| H0149 / R086 | K11:392 抛出 | `action == 'verify' / 捕获 (ValueError, TypeError)` → `ValueError('请填写有效的虚拟 IPv4 地址')` |
| H0150 / R086 | K11:395 抛出 | `action != 'generate' or request.get('mode', 'new') not in ('new', 'migrate')` → `ValueError('安装步骤或模式无效')` |
| H0151 / R086 | K11:398 抛出 | `method not in ('script', 'compose', 'compose_direct', 'compose_compact')` → `ValueError('install_method 须为 script、compose、compose_direct 或 compose_compact')` |
| H0152 / R086 | K11:400 抛出 | `method in ('compose', 'compose_direct', 'compose_compact') and request.get('mode', 'new') != 'new'` → `ValueError('Compose 安装包仅支持新安装；已有容器请使用迁移脚本')` |
| H0153 / R086 | K11:403 抛出 | `not isinstance(image, str) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._:/@-]{0,254}', image)` → `ValueError('Docker 镜像名称无效')` |
| H0154 / R086 | K11:406 抛出 | `not isinstance(server, str) or not re.match('^https?://[^\\s]+$', server)` → `ValueError('请填写完整的 ConfigFlow HTTP(S) 地址')` |
| H0155 / R086 | K11:409 抛出 | `parsed.username or parsed.password or parsed.query or parsed.fragment or (not parsed.hostname)` → `ValueError('ConfigFlow 地址不能包含登录凭据、查询参数或片段')` |
| H0156 / R086 | K11:413 抛出 | `isinstance(port, bool) or not isinstance(port, int)` → `ValueError('Agent 端口须为整数')` |
| H0157 / R086 | K11:421 抛出 | `not isinstance(create_identity, bool)` → `ValueError('create_host_identity 须为布尔值')` |

### K12 [backend/agents/docker_ha_preflight.py](../../backend/agents/docker_ha_preflight.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0158 / R086 | K12:192 抛出 | `not isinstance(output, str)` → `ValueError('output 须为检查命令的文本输出')` |
| H0159 / R086 | K12:196 抛出 | `捕获 UnicodeError` → `ValueError('检查输出包含无效文本编码')` |
| H0160 / R086 | K12:198 抛出 | `size > MAX_OUTPUT_BYTES` → `ValueError('检查输出超过 96 KiB，请只粘贴本次检查结果')` |
| H0161 / R086 | K12:286 抛出 | `network_mode not in ('host', 'macvlan')` → `ValueError('network_mode 须为 host 或 macvlan')` |

### K13 [backend/agents/gateway_install_script.py](../../backend/agents/gateway_install_script.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0162 / R091 | K13:88 抛出 | `not isinstance(value, str) or any((ord(c) < 32 or ord(c) == 127 for c in value))` → `ValueError(f'{name} must be an HTTP(S) URL')` |
| H0163 / R091 | K13:91 抛出 | `parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.fragment` → `ValueError(f'{name} must be an HTTP(S) URL without credentials or fragment')` |
| H0164 / R091 | K13:126 抛出 | `agent_ip / 捕获 (ValueError, TypeError)` → `ValueError('Gateway Agent address must be an IPv4 address')` |
| H0165 / R091 | K13:128 抛出 | `agent_ip / address.is_loopback or address.is_multicast or address.is_unspecified or address.is_link_local` → `ValueError('Gateway Agent address must be a usable LAN IPv4 address')` |
| H0166 / R091 | K13:130 抛出 | `bool(enrollment_url) != bool(enrollment_token)` → `ValueError('Both enrollment_url and enrollment_token are required together')` |
| H0167 / R091 | K13:134 抛出 | `enrollment_url / urlsplit(enrollment_url)[:2] != urlsplit(server_url)[:2]` → `ValueError('Enrollment endpoint must use the ConfigFlow server origin')` |
| H0168 / R091 | K13:137 抛出 | `for token in (enrollment_token,) / token and (not isinstance(token, str) or not re.fullmatch('[A-Za-z0-9_.-]{16,512}', token))` → `ValueError('Invalid installation-session token')` |
| H0169 / R091 | K13:139 抛出 | `not isinstance(full_tun, bool)` → `ValueError('full_tun must be a boolean')` |
| H0170 / R091 | K13:142 抛出 | `not checksums` → `ValueError('ConfigFlow 尚未提供 Linux Agent 二进制，请先构建/部署最新版 Agent 后重试。')` |
| H0171 / R091 | K13:145 抛出 | `for (arch, digest) in checksums.items() / arch not in ('linux-amd64', 'linux-arm64') or not isinstance(digest, str) or (not re.fullmatch('[a-f0-9]{64}', digest))` → `ValueError('Invalid Agent architecture or SHA256 digest')` |

### K14 [backend/agents/gateway_installation.py](../../backend/agents/gateway_installation.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0172 / R091 | K14:54 抛出 | `not isinstance(session_id, str) or not re.fullmatch('[a-f0-9]{32}', session_id)` → `InstallationError('安装会话不存在。', 404)` |
| H0173 / R091 | K14:62 抛出 | `捕获 FileNotFoundError` → `InstallationError('安装会话不存在，请重新创建安装。', 404)` |
| H0174 / R091 | K14:64 抛出 | `not isinstance(value, dict) or value.get('id') != session_id` → `InstallationError('安装记录无法读取，请保留现场并检查服务端数据。', 409)` |
| H0175 / R091 | K14:98 抛出 | `not token or not _constant_time_ascii_equal(token, session['token']) or time.time() >= session['expires']` → `InstallationError('安装凭据无效或已过期，请在向导创建新的安装命令。', 401)` |
| H0176 / R091 | K14:103 抛出 | `not isinstance(value, str) or not value or len(value) > 2048 or any((ord(c) < 33 for c in value))` → `InstallationError('请填写 Linux 能访问的 ConfigFlow HTTP(S) 服务地址。')` |
| H0177 / R091 | K14:108 抛出 | `捕获 ValueError` → `InstallationError('ConfigFlow 服务地址无效。')` |
| H0178 / R091 | K14:110 抛出 | `parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment` → `InstallationError('服务地址必须是无账号、查询参数或片段的 HTTP(S) 地址。')` |
| H0179 / R091 | K14:112 抛出 | `parsed.hostname.lower() in ('localhost', '0.0.0.0', '::', '::1') or parsed.hostname.startswith('127.')` → `InstallationError('Linux 安装不能使用 localhost / 回环服务地址，请填写 ConfigFlow 的局域网地址或域名。')` |
| H0180 / R091 | K14:124 抛出 | `revision != session['config_revision']` → `InstallationError('配置已在其他页面修改，请重新准备安装并预览保存；已安装的设备会继续沿用。', 409)` |
| H0181 / R091 | K14:126 抛出 | `session.get('takeover') and session['takeover'] != _takeover(config)` → `InstallationError('接管方式已变化，请重新准备安装；已安装的设备会继续沿用。', 409)` |
| H0182 / R091 | K14:133 抛出 | `configured_url and configured_url != session['server_url']` → `InstallationError(f"系统设置中的服务地址是 {configured_url}，与这里填写的 {session['server_url']} 不一致。核心会使用系统服务地址持续下载规则，两者需相同。可将本次地址设为系统服务地址后继续，或在系统设置中修改。", 409, code='server_url_mismatch')` |
| H0183 / R091 | K14:151 抛出 | `for (service, port) in ports.items() / port in owners` → `InstallationError(f'{service} Agent 端口 {port} 与 Mihomo 基础配置的 {owners[port]} 冲突，请换一个 Agent 端口，或先修改 Mihomo 基础配置。')` |
| H0184 / R091 | K14:159 抛出 | `not isinstance(body, dict) or set(body) - allowed` → `InstallationError('安装参数无效。')` |
| H0185 / R091 | K14:163 抛出 | `type(body.get('config_revision')) is not int or body['config_revision'] < 0` → `InstallationError('请先预览并保存网关配置。')` |
| H0186 / R091 | K14:166 抛出 | `not isinstance(name, str) or not re.fullmatch('[A-Za-z0-9_-]{1,48}', name)` → `InstallationError('设备名称使用 1–48 个字母、数字、短横线或下划线。')` |
| H0187 / R091 | K14:169 抛出 | `type(auto_network) is not bool` → `InstallationError('auto_network 必须为布尔值。')` |
| H0188 / R091 | K14:172 抛出 | `auto_network / 'network' in body` → `InstallationError('自动识别网络时只需填写可选的 Agent IP。')` |
| H0189 / R091 | K14:175 抛出 | `auto_network / not isinstance(requested_ip, str)` → `InstallationError('Agent IP 必须是 IPv4 地址或留空。')` |
| H0190 / R091 | K14:180 抛出 | `not (auto_network) / 'agent_ip' in body` → `InstallationError('手动网络模式请使用完整网络参数。')` |
| H0191 / R091 | K14:184 抛出 | `not (auto_network) / 捕获 MonitorError` → `InstallationError(exc.message)` |
| H0192 / R091 | K14:187 抛出 | `any((type(port) is not int or not 1024 <= port <= 65535 or port == 1053 for port in ports.values())) or len(set(ports.values())) != 2` → `InstallationError('两个 Agent 需要不同的 1024–65535 端口，不能占用 Mihomo DNS 1053。')` |
| H0193 / R091 | K14:198 抛出 | `type(body.get('use_as_server_domain', False)) is not bool` → `InstallationError('use_as_server_domain 必须为布尔值。')` |
| H0194 / R091 | K14:247 抛出 | `session['phase'] == 'publishing'` → `InstallationError('发布进行中，请等待完成后再更新配置。', 409)` |
| H0195 / R091 | K14:279 抛出 | `捕获 InstallCommandStoreFull` → `InstallationError('可用安装链接已达上限，请使用已生成的命令或等待旧链接过期。', 503)` |
| H0196 / R091 | K14:296 抛出 | `not agent or agent.get('service_type') != service or agent.get('deployment_method') != 'shell' or (agent.get('host') != session['network']['address']) or (agent.get('port') != session['ports…（完整见源行）` → `InstallationError('设备身份、地址、服务类型或配置空间已改变，请检查本次安装目标。', 409)` |
| H0197 / R091 | K14:303 抛出 | `address.is_loopback or address.is_multicast or address.is_unspecified or address.is_link_local or (str(address) == '255.255.255.255')` → `ValueError`（**[O6]** 更正：原稿把源码里的网段/广播字面量替换成了文档地址，改变了条件含义） |
| H0198 / R091 | K14:305 抛出 | `捕获 (ValueError, TypeError)` → `InstallationError('安装主机需要可访问的 LAN IPv4 地址。')` |
| H0199 / R091 | K14:314 抛出 | `session.get('auto_network') and (not session['network']['address']) / len(owned_hosts) > 1` → `InstallationError('本次安装的两个 Agent 地址不一致，请检查安装主机。', 409)` |
| H0200 / R091 | K14:317 抛出 | `session.get('auto_network') and (not session['network']['address']) / owned_hosts and host not in owned_hosts` → `InstallationError('本次安装已在另一台主机登记，不能更换地址。', 409)` |
| H0201 / R102 | K14:326 抛出 | `service not in SERVICES` → `InstallationError('安装凭据只能注册本次指定的 Mihomo / OxiDNS。', 403)` |
| H0202 / R102 | K14:354 抛出 | `owned / session['agent_ids'].get(service, existing['id']) != existing['id']` → `InstallationError('本次安装已关联其他设备，不能替换。', 409)` |
| H0203 / R102 | K14:357 抛出 | `not (owned) / session['agent_ids'].get(service) and (not existing or existing['id'] != session['agent_ids'][service])` → `InstallationError('本次安装关联的设备已改变，请创建新的安装。', 409)` |
| H0204 / R093 | K14:369 抛出 | `not isinstance(body, dict) or set(body) != {'agents'} or (not isinstance(body['agents'], dict)) or (set(body['agents']) != set(SERVICES))` → `InstallationError('请提交本次安装的两个 Agent 身份。')` |
| H0205 / R093 | K14:374 抛出 | `not session['network']['address']` → `InstallationError('请先等待两个 Agent 通过本次安装命令登记。', 409)` |
| H0206 / R093 | K14:382 抛出 | `for service in SERVICES / not isinstance(proof, dict) or set(proof) != {'id', 'token'} or (not all((isinstance(v, str) for v in proof.values())))` → `InstallationError('Agent 身份凭据无效。', 401)` |
| H0207 / R093 | K14:385 抛出 | `for service in SERVICES / not agent or not _constant_time_ascii_equal(proof['token'], agent.get('token'))` → `InstallationError('无法验证本次安装的 Agent 身份。', 401)` |
| H0208 / R093 | K14:387 抛出 | `for service in SERVICES / agent.get('service_type') != service or agent.get('deployment_method') != 'shell'` → `InstallationError('本次安装只能关联对应的 Shell 核心 Agent。', 409)` |
| H0209 / R093 | K14:389 抛出 | `for service in SERVICES / session['agent_ids'].get(service, agent['id']) != agent['id']` → `InstallationError('本次安装已关联其他设备，不能替换。', 409)` |
| H0210 / R093 | K14:391 抛出 | `for service in SERVICES / session['registered'].get(service, agent['id']) != agent['id']` → `InstallationError('Agent 与本次安装的登记记录不一致。', 409)` |
| H0211 / R093 | K14:400 抛出 | `for service in SERVICES / session['agent_ids'].get(service) and changed` → `InstallationError('已关联设备的信息发生变化，请重新准备安装。', 409)` |
| H0212 / R093 | K14:403 抛出 | `for service in SERVICES / any((other['id'] != agent['id'] and other.get('name') == expected['name'] and (other.get('host') == expected['host']) for other in agents))` → `InstallationError('目标名称和地址已被另一台 Agent 使用，请更换设备名称。', 409)` |
| H0213 / R093 | K14:407 抛出 | `for service in SERVICES / changed and (_upgrade_busy(agent) or _dns_follow_busy(agents, agent['id']) or any((task.get('status') not in ('succeeded', 'failed', 'rolled_back') for task in agen…（完整见源行）` → `InstallationError('设备仍有未完成的发布或升级，请先完成或恢复后再关联。', 409)` |
| H0214 / R091 | K14:445 抛出 | `session['phase'] == 'published' / task.get('deployment_id') != expected or task.get('status') != 'succeeded'` → `InstallationError('设备配置在安装完成后已更改。', 409)` |
| H0215 / R095 | K14:492 抛出 | `repository.config_revision(session['profile_id']) != session['config_revision']` → `InstallationError('配置已修改：请重新准备安装，预览并保存后会继续沿用已安装的设备，无需重新运行安装命令。', 409)` |
| H0216 / R095 | K14:494 抛出 | `set(session['agent_ids']) != set(SERVICES)` → `InstallationError('请先完成两个核心和 Agent 的安装登记。', 409)` |
| H0217 / R095 | K14:500 抛出 | `for (service, agent_id) in session['agent_ids'].items() / not is_usable(agent)` → `InstallationError('安装设备离线或已禁用，请检查连接后继续。', 409)` |
| H0218 / R095 | K14:504 抛出 | `for (service, agent_id) in session['agent_ids'].items() / deployment_id in agent.get('deployments', {}) and (agent.get('latest_deployment') or {}).get('deployment_id') != deployment_id` → `InstallationError('设备已开始另一项发布，请重新准备本次安装。', 409)` |
| H0219 / R094 | K14:518 抛出 | `not status.get('success') or status.get('supported') is not True or status.get('detection_error')` → `InstallationError(status.get('detection_error') or status.get('unsupported_reason') or '暂时无法自动读取安装主机的网络，请检查 Agent 后重试。', 409)` |
| H0220 / R094 | K14:520 抛出 | `捕获 MonitorError` → `InstallationError(exc.message, exc.status)` |
| H0221 / R094 | K14:525 抛出 | `捕获 MonitorError` → `InstallationError('无法自动确认主机的 LAN 网络或唯一默认路由，请检查主机网络后重试。', 409)` |
| H0222 / R094 | K14:527 抛出 | `network['address'] != session['network']['address']` → `InstallationError('自动识别的 LAN 地址与 Agent 登记地址不同，请检查多网卡或默认路由后重试。', 409)` |
| H0223 / R094 | K14:538 抛出 | `not automatic and (not isinstance(body, dict) or set(body) != {'network'})` → `InstallationError('请选择自动检测，或提交完整网络参数。')` |
| H0224 / R094 | K14:544 抛出 | `session['phase'] not in ('enrolled', 'publish_failed', 'published')` → `InstallationError('请等待设备登记完成，发布过程中不能修改网络参数。', 409)` |
| H0225 / R094 | K14:549 抛出 | `not status.get('success') or status.get('supported') is not True or status.get('detection_error')` → `InstallationError(status.get('detection_error') or status.get('unsupported_reason') or '无法读取主机网络。', 409)` |
| H0226 / R094 | K14:552 抛出 | `not isinstance(detection, dict)` → `InstallationError('Agent 未返回有效网络检测结果，请重试。', 409)` |
| H0227 / R094 | K14:555 抛出 | `捕获 MonitorError` → `InstallationError('自动检测无法确认网络，请在高级选项填写实际网络参数。' if automatic else exc.message, 409 if automatic else exc.status)` |
| H0228 / R094 | K14:557 抛出 | `network['address'] != session['network']['address']` → `InstallationError('Linux IP 必须与本次已登记设备一致，不能切换目标主机。', 409)` |
| H0229 / R094 | K14:570 抛出 | `not matching_address or not matching_route` → `InstallationError('参数与主机实际网卡、地址或唯一默认路由不一致；向导不会修改主机 IP 或默认路由。', 409)` |
| H0230 / R094 | K14:580 抛出 | `(managed.get('active') or managed.get('pending')) and network != session['network'] / network != live` → `InstallationError('请先恢复客户端设置并撤销网关转发，再修改网络参数。', 409)` |
| H0231 / R095 | K14:598 抛出 | `not reservation.get('success')` → `InstallationError(reservation.get('message') or '设备正在执行其他任务。', 409)` |
| H0232 / R091 | K14:642 抛出 | `for service in SERVICES / task.get('status') in ('failed', 'rolled_back', 'rollback_failed')` → `InstallationError(f'{service} 发布未成功：' + str(task.get('error') or task.get('message') or task['status']), 409)` |
| H0233 / R091 | K14:644 抛出 | `for service in SERVICES / task.get('http_status') in (400, 401, 403, 404, 409, 422) and (not task.get('pending'))` → `InstallationError(task.get('message') or '设备拒绝发布，请检查后继续。', 409)` |
| H0234 / R091 | K14:646 抛出 | `for service in SERVICES / time.monotonic() >= deadline` → `InstallationError('等待发布结果超时，任务编号已保存；可点击继续发布确认结果，不会重复创建任务。', 409)` |
| H0235 / R095 | K14:679 抛出 | `not isinstance(body, dict) or set(body) - {'retry_failed'} or type(body.get('retry_failed', False)) is not bool` → `InstallationError('发布参数无效。')` |
| H0236 / R095 | K14:699 抛出 | `failed and (not body.get('retry_failed'))` → `InstallationError('上次发布失败，请确认错误已解决后选择重试发布。', 409)` |

### K15 [backend/agents/generated_preview.py](../../backend/agents/generated_preview.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0237 / R023 | K15:45 抛出 | `1 not in capability.get('final_config_protocols', []) or capability.get('service_type') != agent['service_type']` → `ValueError('请先更新目标 Agent，再生成最终下发配置。')` |
| H0238 / R023 | K15:56 抛出 | `rendered.get('success') is not True or not isinstance(rendered.get('content'), str)` → `ValueError(rendered.get('message') or '设备无法生成最终配置，请检查 Agent 状态。')` |
| H0239 / R023 | K15:84 抛出 | `incoming > MAX_BYTES` → `ValueError('生成结果超过临时存储上限，请减少配套文件后重试。')` |
| H0240 / R025 | K15:109 抛出 | `not isinstance(key, str) or not re.fullmatch('[a-f0-9]{32}', key)` → `PreviewChanged('生成结果无效，请重新生成。')` |
| H0241 / R025 | K15:114 抛出 | `record['agent_id'] != agent_id or record['expires_at'] <= time.time()` → `ValueError('stale')` |
| H0242 / R025 | K15:118 抛出 | `(meta := record.get('bundle_meta')) / hashlib.sha256(archive).hexdigest() != meta['sha256']` → `ValueError('digest')` |
| H0243 / R025 | K15:123 抛出 | `捕获 (OSError, ValueError, KeyError, TypeError)` → `PreviewChanged('生成结果已过期或不可用，请重新生成后推送。')` |
| H0244 / R025 | K15:131 抛出 | `捕获 ProfileRepositoryError` → `PreviewChanged('生成时的配置空间已不可用，请重新选择并生成。')` |
| H0245 / R025 | K15:133 抛出 | `revision != record['config_revision'] or input_digest(config) != record['input_digest']` → `PreviewChanged('配置已改变，请重新生成并检查后推送。')` |
| H0246 / R025 | K15:137 抛出 | `for (key, identity) in record['identities'].items() / not current or agent_identity(current) != identity` → `PreviewChanged('目标设备或绑定已改变，请重新生成并检查后推送。')` |

### K16 [backend/agents/go_install_script.py](../../backend/agents/go_install_script.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0247 / R099 | K16:61 抛出 | `not INSTALL_GO_AIO_FORWARDING.startswith(_AIO_FORWARDING_HEAD) or INSTALL_GO_AIO_FORWARDING.count(_AIO_FORWARDING_ROOT) != 6 or INSTALL_GO_AIO_FORWARDING.count(AIO_FORWARDING_HOOK_COMMENT) !…（完整见源行）` → `RuntimeError('aio-ipv4-forwarding.sh changed; review the single-service forwarding helpers')` |
| H0248 / R099 | K16:66 抛出 | `'$AGENT_ROOT' in text` → `RuntimeError('single-service forwarding helpers still reference the AIO root')` |
| H0249 / R099 | K16:123 抛出 | `service_type not in ('mihomo', 'mosdns', 'oxidns')` → `ValueError('Go Agent transactional deployment supports mihomo, mosdns and oxidns')` |
| H0250 / R099 | K16:125 抛出 | `not isinstance(install_core, bool)` → `ValueError('install_core 必须为 true 或 false')` |
| H0251 / R099 | K16:127 抛出 | `not isinstance(prepare_dns_ha, bool)` → `ValueError('prepare_dns_ha 必须为 true 或 false')` |
| H0252 / R099 | K16:129 抛出 | `prepare_dns_ha and service_type != 'oxidns'` → `ValueError('仅 OxiDNS 单服务 Go Agent 支持准备 DNS 高可用环境')` |
| H0253 / R099 | K16:131 抛出 | `not re.fullmatch('[A-Za-z0-9_-]+', agent_name)` → `ValueError('Invalid agent name: use letters, numbers, hyphens or underscores')` |
| H0254 / R099 | K16:133 抛出 | `not 1 <= int(agent_port) <= 65535` → `ValueError('Invalid Agent port')` |
| H0255 / R099 | K16:136 抛出 | `not re.fullmatch('[A-Za-z0-9_.@-]+', unit)` → `ValueError('Invalid service unit')` |
| H0256 / R099 | K16:140 抛出 | `service_manager not in ('', 'systemd', 'openrc', 'command')` → `ValueError('Shell installs require systemd, openrc or explicit command lifecycle')` |
| H0257 / R099 | K16:146 抛出 | `install_core / service_type not in ('mihomo', 'oxidns')` → `ValueError('仅单服务 Mihomo 或 OxiDNS Go Agent 支持同时安装核心')` |
| H0258 / R099 | K16:151 抛出 | `install_core / config_path not in (None, '', f'/etc/{service_type}/config.yaml') or unit != service_type or service_manager or (not standard_restart) or (service_binary not in ('', service_t…（完整见源行）` → `ValueError('同时安装核心需要默认配置路径、服务名、核心路径和自动识别服务管理器，不能使用自定义启停命令')` |
| H0259 / R099 | K16:153 抛出 | `install_core / isinstance(agent_port, bool) or not re.fullmatch('[0-9]+', str(agent_port))` → `ValueError('Invalid Agent port')` |
| H0260 / R099 | K16:155 抛出 | `install_core / int(agent_port) == (1053 if service_type == 'mihomo' else 53)` → `ValueError('Agent 端口不能与所安装核心的 DNS 监听端口相同')` |
| H0261 / R099 | K16:159 抛出 | `prepare_dns_ha and service_manager == 'command'` → `ValueError('DNS 高可用安装需要原生 systemd/OpenRC，不能使用自定义启停命令')` |
| H0262 / R099 | K16:161 抛出 | `service_manager == 'command' and (not all((stop_command, start_command, status_command)))` → `ValueError('Custom lifecycle requires stop_command, start_command and status_command; a restart command alone cannot support rollback')` |
| H0263 / R099 | K16:163 抛出 | `manage_ipv4_forwarding is not None and (not isinstance(manage_ipv4_forwarding, bool))` → `ValueError('manage_ipv4_forwarding 必须为 true 或 false')` |
| H0264 / R099 | K16:169 抛出 | `not (manage_ipv4_forwarding is None) / manage_ipv4_forwarding and (not forwarding_supported)` → `ValueError(STANDALONE_FORWARDING_UNSUPPORTED)` |
| H0265 / R099 | K16:354 抛出 | `not isinstance(install_core, bool)` → `ValueError('install_core 必须为 true 或 false')` |
| H0266 / R099 | K16:356 抛出 | `not isinstance(manage_ipv4_forwarding, bool)` → `ValueError('manage_ipv4_forwarding must be a boolean')` |
| H0267 / R099 | K16:358 抛出 | `not isinstance(manage_tun, bool)` → `ValueError('manage_tun must be a boolean')` |
| H0268 / R099 | K16:360 抛出 | `not isinstance(non_interactive, bool)` → `ValueError('non_interactive must be a boolean')` |
| H0269 / R099 | K16:362 抛出 | `not isinstance(tun_prepare_assume_enabled, bool)` → `ValueError('tun_prepare_assume_enabled must be a boolean')` |
| H0270 / R099 | K16:364 抛出 | `tun_prepare_assume_enabled and (not (manage_tun and verified_agent_path))` → `ValueError('assume-tun-enabled is only supported inside the gateway installer')` |
| H0271 / R099 | K16:366 抛出 | `verified_agent_path and verified_agent_path != '$GATEWAY_STAGE/configflow-agent'` → `ValueError('Preverified Agent binary is only supported inside the gateway installer')` |
| H0272 / R099 | K16:368 抛出 | `verified_agent_path and (not non_interactive)` → `ValueError('Preverified Agent requires non-interactive installation')` |
| H0273 / R099 | K16:370 抛出 | `bool(installation_id) != bool(installation_token)` → `ValueError('Installation id and token must be supplied together')` |
| H0274 / R099 | K16:372 抛出 | `installation_id and (not re.fullmatch('[A-Za-z0-9_-]{16,80}', installation_id))` → `ValueError('Invalid installation id')` |
| H0275 / R099 | K16:374 抛出 | `installation_token and (not re.fullmatch('[A-Za-z0-9_.-]{16,512}', installation_token))` → `ValueError('Invalid installation token')` |
| H0276 / R099 | K16:376 抛出 | `not isinstance(agent_name, str) or not re.fullmatch('[A-Za-z0-9_-]+', agent_name)` → `ValueError('Invalid agent name: use letters, numbers, hyphens or underscores')` |
| H0277 / R099 | K16:380 抛出 | `for port in (mihomo_agent_port, oxidns_agent_port) / isinstance(port, bool) or not re.fullmatch('[0-9]+', str(port)) or (not 1 <= int(port) <= 65535)` → `ValueError('Invalid Agent port')` |
| H0278 / R099 | K16:383 抛出 | `ports[0] == ports[1]` → `ValueError('Mihomo and OxiDNS Agent ports must be different')` |
| H0279 / R099 | K16:385 抛出 | `install_core and any((port in (53, 1053) for port in ports))` → `ValueError('两个 Agent 端口均不能使用核心 DNS 监听端口 53 或 1053')` |
| H0280 / R099 | K16:397 抛出 | `for value in (mihomo_config_path, oxidns_config_path) / not isinstance(value, str) or not value.startswith('/') or any((ord(c) < 32 or ord(c) == 127 for c in value)) or ('..' in PurePosixPat…（完整见源行）` → `ValueError('Core configuration paths must be absolute file paths without parent traversal')` |
| H0281 / R099 | K16:399 抛出 | `for value in (mihomo_config_path, oxidns_config_path) / inside_agent_directory(value)` → `ValueError('Core configuration paths must be outside the Agent directory')` |
| H0282 / R099 | K16:402 抛出 | `paths[0] == paths[1]` → `ValueError('Mihomo and OxiDNS configuration paths must be different')` |
| H0283 / R099 | K16:404 抛出 | `PurePosixPath(paths[0]).parent == PurePosixPath(paths[1]).parent` → `ValueError('Core configurations require separate directories for deployment journals')` |
| H0284 / R099 | K16:438 抛出 | `for (service, port, path, unit, binary) in zip(('mihomo', 'oxidns'), ports, paths, (mihomo_service_unit, oxidns_service_unit), (mihomo_service_binary, oxidns_service_binary)) / not isinstanc…（完整见源行）` → `ValueError('Invalid service unit')` |
| H0285 / R099 | K16:441 抛出 | `for (service, port, path, unit, binary) in zip(('mihomo', 'oxidns'), ports, paths, (mihomo_service_unit, oxidns_service_unit), (mihomo_service_binary, oxidns_service_binary)) / not unit or u…（完整见源行）` → `ValueError('Core service unit must not be a ConfigFlow Agent unit')` |
| H0286 / R099 | K16:443 抛出 | `for (service, port, path, unit, binary) in zip(('mihomo', 'oxidns'), ports, paths, (mihomo_service_unit, oxidns_service_unit), (mihomo_service_binary, oxidns_service_binary)) / not isinstanc…（完整见源行）` → `ValueError('Invalid core service binary')` |
| H0287 / R099 | K16:445 抛出 | `for (service, port, path, unit, binary) in zip(('mihomo', 'oxidns'), ports, paths, (mihomo_service_unit, oxidns_service_unit), (mihomo_service_binary, oxidns_service_binary)) / binary.starts…（完整见源行）` → `ValueError('Core service binaries must be outside the Agent directory')` |
| H0288 / R099 | K16:448 抛出 | `for (service, port, path, unit, binary) in zip(('mihomo', 'oxidns'), ports, paths, (mihomo_service_unit, oxidns_service_unit), (mihomo_service_binary, oxidns_service_binary)) / install_core …（完整见源行）` → `ValueError('同时安装两个核心需要默认配置路径、服务名和核心路径；自定义部署请取消同时安装核心')` |
| H0289 / R099 | K16:467 抛出 | `mihomo_service_unit.removesuffix('.service') == oxidns_service_unit.removesuffix('.service')` → `ValueError('Mihomo and OxiDNS service units must be different')` |

### K17 [backend/agents/install_command_store.py](../../backend/agents/install_command_store.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0290 / R099 | K17:77 抛出 | `not isinstance(script, str) or len(script.encode('utf-8')) > MAX_SCRIPT_BYTES` → `ValueError('安装脚本过大，请减少自定义参数后重试。')` |
| H0291 / R099 | K17:83 抛出 | `not (expires_at is None) / type(expires_at) is not int or not now < expires_at <= now + TTL_SECONDS` → `ValueError('安装链接有效期必须在未来 24 小时内。')` |
| H0292 / R099 | K17:86 抛出 | `connection.execute('SELECT COUNT(*) FROM snapshots').fetchone()[0] >= MAX_SNAPSHOTS` → `InstallCommandStoreFull()` |

### K18 [backend/agents/install_script.py](../../backend/agents/install_script.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0293 / R083 | K18:27 抛出 | `not re.match('^[a-zA-Z0-9_-]+$', name)` → `ValueError('Invalid agent name: only alphanumeric characters, hyphens, and underscores are allowed.')` |
| H0294 / R083 | K18:33 抛出 | `not enable_oxidns or enable_mihomo or enable_mosdns` → `ValueError('Docker DNS 高可用仅支持单独运行 OxiDNS')` |
| H0295 / R083 | K18:35 抛出 | `network_mode not in ('host', 'macvlan')` → `ValueError('Docker DNS 高可用需选择 Host 或 Macvlan，不支持 bridge')` |
| H0296 / R083 | K18:39 抛出 | `address.is_loopback or address.is_multicast or address.is_link_local or address.is_unspecified or address.is_reserved` → `ValueError()` |
| H0297 / R083 | K18:41 抛出 | `捕获 ValueError` → `ValueError('请填写有效的固定局域网 IPv4 地址；Host 模式填写宿主机地址，Macvlan 模式填写容器地址')` |
| H0298 / R083 | K18:45 抛出 | `not re.fullmatch('[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}', ha_parent)` → `ValueError('请填写有效的宿主机网卡名称，如 eth0 或 enp1s0')` |
| H0299 / R083 | K18:52 抛出 | `subnet.prefixlen > 30 or any((ip.is_loopback or ip.is_multicast or ip.is_link_local or ip.is_unspecified for ip in (subnet.network_address, address)))` → `ValueError()` |
| H0300 / R083 | K18:55 抛出 | `for ip in (address, gateway) / ip is not None and (ip not in subnet or ip in (subnet.network_address, subnet.broadcast_address))` → `ValueError()` |
| H0301 / R083 | K18:57 抛出 | `address == gateway` → `ValueError()` |
| H0302 / R083 | K18:59 抛出 | `捕获 ValueError` → `ValueError('请填写有效的 IPv4 局域网网段、容器固定地址和可选网关；容器地址须在该网段内且不能与网关相同')` |
| H0303 / R083 | K18:73 抛出 | `not isinstance(path, str) or not os.path.isabs(path) or any((char in path for char in ',:\x00\r\n'))` → `ValueError('宿主机身份文件须为绝对路径，且不能包含逗号、冒号或换行')` |
| H0304 / R083 | K18:104 抛出 | `enable_dns_ha / not isinstance(oxidns_port, int) or not 1024 <= oxidns_port <= 65535` → `ValueError('OxiDNS Agent 端口须为 1024–65535')` |
| H0305 / R083 | K18:107 抛出 | `enable_mosdns and enable_oxidns` → `ValueError('MosDNS 与 OxiDNS 默认都监听 53 端口，请使用独立容器或只启用一个 DNS 核心')` |
| H0306 / R083 | K18:277 抛出 | `enable_dns_ha / not isinstance(oxidns_port, int) or not 1024 <= oxidns_port <= 65535` → `ValueError('OxiDNS Agent 端口须为 1024–65535')` |
| H0307 / R083 | K18:281 抛出 | `enable_mosdns and enable_oxidns` → `ValueError('MosDNS 与 OxiDNS 默认都监听 53 端口，请使用独立容器或只启用一个 DNS 核心')` |

### K19 [backend/agents/ipv6_readiness.py](../../backend/agents/ipv6_readiness.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0308 / R028 | K19:67 抛出 | `not isinstance(document, dict)` → `ValueError('基础 YAML 必须是对象')` |
| H0309 / R028 | K19:70 抛出 | `not isinstance(dns, dict)` → `ValueError('dns 必须是对象')` |
| H0310 / R028 | K19:100 抛出 | `not agent` → `MonitorError('AGENT_NOT_FOUND', 404, '未找到这台 Agent')` |
| H0311 / R028 | K19:102 抛出 | `agent.get('profile_id') != profile_id` → `MonitorError('PROFILE_CHANGED', 409, '设备绑定的配置空间已变化，请重新打开设置')` |
| H0312 / R028 | K19:104 抛出 | `agent.get('service_type') != 'mihomo'` → `MonitorError('WRONG_SERVICE', 400, 'IPv6 检查仅适用于 Mihomo Agent')` |
| H0313 / R028 | K19:106 抛出 | `agent.get('enabled') is False` → `MonitorError('AGENT_DISABLED', 409, '这台设备已被禁用，请启用后重新检查')` |
| H0314 / R028 | K19:123 抛出 | `for part in response.iter_content(65536) / size > MAX_RESPONSE` → `MonitorError('INVALID_RESPONSE', 502, 'IPv6 检查结果过大')` |
| H0315 / R028 | K19:128 抛出 | `捕获 (requests.RequestException, KeyError, ValueError, TypeError)` → `MonitorError('AGENT_UNREACHABLE', 502, '未能确认 IPv6 配置操作结果，请重新检查设备状态' if operation == 'prepare' else '无法连接 Agent 进行 IPv6 检查')` |
| H0316 / R028 | K19:132 抛出 | `status == 404` → `MonitorError('UPGRADE_REQUIRED', 409, '请将 Mihomo Agent 更新到 1.8.50-go 或更新版本后检查 IPv6')` |
| H0317 / R028 | K19:134 抛出 | `status in (401, 403)` → `MonitorError('AGENT_AUTH_FAILED', 502, 'Agent 认证失败，请检查设备注册状态')` |
| H0318 / R028 | K19:138 抛出 | `捕获 (ValueError, UnicodeError)` → `MonitorError('INVALID_RESPONSE', 502, 'Agent 返回的 IPv6 检查结果无法解析')` |
| H0319 / R028 | K19:140 抛出 | `not isinstance(data, dict)` → `MonitorError('INVALID_RESPONSE', 502, 'Agent 返回的 IPv6 检查格式不正确')` |
| H0320 / R028 | K19:143 抛出 | `status >= 400 or data.get('success') is not True` → `MonitorError('IPV6_OPERATION_FAILED', status if status in (400, 409, 422) else 502, message)` |
| H0321 / R028 | K19:194 抛出 | `not isinstance(host.get('checks'), list) or not isinstance(host.get('persistence'), dict)` → `MonitorError('INVALID_RESPONSE', 502, 'Agent IPv6 状态缺少检查与持久化结果')` |
| H0322 / R028 | K19:382 抛出 | `online_status(agent) == 'offline'` → `MonitorError('AGENT_OFFLINE', 409, '设备离线，上线后重新检查')` |
| H0323 / R028 | K19:396 抛出 | `not isinstance(body, dict) or set(body) != {'interface', 'check_token'}` → `MonitorError('INVALID_REQUEST', 400, '请提供检查结果对应的网卡与 check_token')` |
| H0324 / R028 | K19:398 抛出 | `not isinstance(body['check_token'], str) or len(body['check_token']) != 64` → `MonitorError('INVALID_REQUEST', 400, '请先检查 IPv6 状态')` |
| H0325 / R028 | K19:406 抛出 | `for router in linked / not acquired` → `MonitorError('BUSY', 409, '路由器正在发布或同步，请稍后重试')` |
| H0326 / R028 | K19:410 抛出 | `body['check_token'] != report['check_token'] or body['interface'] != report['prepare_interface']` → `MonitorError('CHECK_CHANGED', 409, '设备、草稿或路由状态已变化，请重新检查后补齐')` |
| H0327 / R028 | K19:412 抛出 | `not report['can_prepare']` → `MonitorError('PREPARE_UNAVAILABLE', 409, '当前状态不满足自动补齐条件，请查看检查结果')` |
| H0328 / R028 | K19:417 抛出 | `_prepare_config(get_config(profile_id)) != _prepare_config(config) or any((current_agent.get(key) != agent.get(key) for key in PREPARE_AGENT_FIELDS))` → `MonitorError('CHECK_CHANGED', 409, '配置或设备信息已变化，请重新检查')` |
| H0329 / R028 | K19:423 抛出 | `捕获 MonitorError` → `MonitorError('PREPARE_FAILED', 502, '主机配置补齐失败，请重新检查：' + str(error))` |
| H0330 / R028 | K19:477 抛出 | `settings['errors'] or settings['ipv6'] is not True` → `MonitorError('IPV6_DISABLED', 409, '请先保存有效的 Mihomo IPv6 配置')` |

### K20 [backend/agents/ipv6_verification.py](../../backend/agents/ipv6_verification.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0331 / R028 | K20:117 抛出 | `not client or client.get('id') == source['id'] or _profile(manager, client) != _profile(manager, source) or (str(client.get('host', '')).lower() == str(source.get('host', '')).lower()) or (c…（完整见源行）` → `MonitorError('CLIENT_UNAVAILABLE', 409, '检测客户端必须是同一配置空间中另一台独立的 Linux 主机')` |
| H0332 / R028 | K20:119 抛出 | `client.get('enabled') is False or client.get('status') != 'online'` → `MonitorError('CLIENT_UNAVAILABLE', 409, '原检测客户端已离线或禁用，请恢复后重新验收')` |
| H0333 / R028 | K20:123 抛出 | `capabilities / cap.get('platform') != 'linux' or 1 not in cap.get('ipv6_client_probe_protocols', [])` → `MonitorError('CLIENT_UPGRADE_REQUIRED', 409, '请更新检测客户端 Agent，使其支持客户端链路验收')` |
| H0334 / R028 | K20:131 抛出 | `values is not None / set(values) != {'client_agent_id', 'test_url'} or any((not isinstance(v, str) for v in values.values()))` → `MonitorError('INVALID_REQUEST', 400, '请提供检测客户端和测试网址')` |
| H0335 / R028 | K20:134 抛出 | `values is not None / len(values['client_agent_id']) > 200 or len(values['test_url']) > 2048` → `MonitorError('INVALID_REQUEST', 400, '验收设置过长')` |
| H0336 / R028 | K20:139 抛出 | `values['test_url'] / 捕获 ValueError` → `MonitorError('INVALID_REQUEST', 400, str(error))` |
| H0337 / R028 | K20:145 抛出 | `values is not None / not current or _identity(current) != _identity(source)` → `MonitorError('PROFILE_CHANGED', 409, '设备信息已变化，请重新打开设置')` |
| H0338 / R028 | K20:147 抛出 | `values is not None / any((r.get('status') == 'running' for r in (current.get('ipv6_verifications') or {}).values()))` → `MonitorError('BUSY', 409, '验收正在执行，请结束后再修改验收设置')` |
| H0339 / R028 | K20:194 抛出 | `deployment_id / task.get('status') != 'succeeded' or task.get('manual_rollback') or task.get('profile_id') != _profile(manager, agent) or (task.get('service_type') != 'mihomo') or ((agent.ge…（完整见源行）` → `MonitorError('DEPLOYMENT_CHANGED', 409, '本次发布尚未成功，或已被其他发布替代')` |
| H0340 / R028 | K20:199 抛出 | `deployment_id / not receipt or receipt['manifest'].get('deployment_id') != deployment_id` → `MonitorError('SNAPSHOT_UNAVAILABLE', 409, '缺少本次发布的配置快照，无法确认验收对象')` |
| H0341 / R028 | K20:208 抛出 | `expected_revision is not None and (type(expected_revision) is not int or expected_revision != revision)` → `MonitorError('CONFIG_CHANGED', 409, '验收对应的配置版本已变化，请重新检查')` |
| H0342 / R028 | K20:338 抛出 | `_identity(manager.get_agent_by_id(client['id']) or {}) != _identity(client)` → `MonitorError('CLIENT_CHANGED', 409, '检测客户端在验收期间发生变化，结果已作废')` |
| H0343 / R028 | K20:356 抛出 | `_identity(manager.get_agent_by_id(source['id']) or {}) != _identity(source)` → `MonitorError('AGENT_CHANGED', 409, '设备绑定在验收前已变化')` |
| H0344 / R028 | K20:410 校验/降级 | `not report` → `False` |
| H0345 / R028 | K20:421 抛出 | `set(body) - {'deployment_id', 'expected_config_revision'}` → `MonitorError('INVALID_REQUEST', 400, '验收只使用已保存的目标与测试设置')` |
| H0346 / R028 | K20:424 抛出 | `deployment_id is not None and (not isinstance(deployment_id, str) or not 1 <= len(deployment_id) <= 128)` → `MonitorError('INVALID_REQUEST', 400, '发布标识无效')` |
| H0347 / R028 | K20:427 抛出 | `'expected_config_revision' in body and (type(body['expected_config_revision']) is not int or body['expected_config_revision'] < 0)` → `MonitorError('INVALID_REQUEST', 400, '配置版本无效')` |
| H0348 / R028 | K20:448 抛出 | `not _slots.acquire(blocking=False)` → `MonitorError('BUSY', 409, '验收队列繁忙，请稍后重试')` |
| H0349 / R028 | K20:463 抛出 | `not current or _findings(manager, current, report, agents)` → `MonitorError('AGENT_CHANGED', 409, '设备或验收设置已变化，请重新检查')` |
| H0350 / R028 | K20:471 抛出 | `any((r.get('status') == 'running' for r in runs.values()))` → `MonitorError('BUSY', 409, '这台设备正在验收，请等待当前结果')` |

### K21 [backend/agents/linux_gateway.py](../../backend/agents/linux_gateway.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0351 / R033 | K21:161 抛出 | `捕获 MonitorError` → `ValueError(f'无法确认设备上是否仍有旧版 Linux 网关记录，已停止发布：{error.message}')` |
| H0352 / R033 | K21:163 抛出 | `not keeps_forwarding(status)` → `ValueError(FORWARDING_NOT_KEPT.format(action='发布完整 TUN 后，'))` |
| H0353 / R033 | K21:171 抛出 | `捕获 MonitorError` → `ValueError(f'无法确认设备上是否仍有旧版 Linux 网关记录，已停止发布：{error.message}')` |
| H0354 / R033 | K21:173 抛出 | `not _record_kept(agent, status)` → `ValueError(PUBLISH_GATEWAY_OFF_NOT_KEPT)` |
| H0355 / R033 | K21:202 抛出 | `not supports_gateway(agent)` → `ValueError(f'Linux 网关需要 Mihomo Agent {MIN_AGENT_VERSION} 或更新版本，请先升级 Agent 后再发布。')` |
| H0356 / R033 | K21:226 抛出 | `not isinstance(body, dict) or set(body) != SPEC_FIELDS` → `_error('INVALID_REQUEST', 400, '请填写网卡、Linux IPv4、LAN 网段和主路由 IPv4。')` |
| H0357 / R033 | K21:228 抛出 | `any((not isinstance(v, str) or not v or len(v) > 64 for v in body.values()))` → `_error('INVALID_REQUEST', 400, '网关转发的网络参数必须是有效的短文本。')` |
| H0358 / R033 | K21:231 抛出 | `not re.fullmatch('[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}', interface) or interface == 'lo'` → `_error('INVALID_REQUEST', 400, '请选择实际的 LAN 网卡，不能使用回环网卡。')` |
| H0359 / R033 | K21:237 抛出 | `捕获 ValueError` → `_error('INVALID_REQUEST', 400, '需要合法 IPv4 地址和规范 LAN 网段，例如 192.168.1.0/24。')` |
| H0360 / R033 | K21:243 抛出 | `not 8 <= network.prefixlen <= 30 or any((value not in network or value in (network.network_address, network.broadcast_address) or value.is_multicast or value.is_unspecified or value.is_loopb…（完整见源行）` → `_error('INVALID_REQUEST', 400, 'Linux 和主路由需要不同的可用 IPv4 地址，且位于同一个 /8 至 /30 LAN 网段。')` |
| H0361 / R033 | K21:280 抛出 | `operation == 'remove' and body not in (None, {}) and (not confirmed)` → `_error('INVALID_REQUEST', 400, '撤销网关转发不接受自定义命令或网络参数。')` |
| H0362 / R033 | K21:284 抛出 | `operation == 'apply' / mode == 'full_tun'` → `_error('ROUTER_SPLIT_FULL_TUN', 409, MESSAGES['ROUTER_SPLIT_FULL_TUN'])` |
| H0363 / R033 | K21:286 抛出 | `operation == 'apply' / mode == 'invalid'` → `_error('ROUTER_SPLIT_INVALID', 409, MESSAGES['ROUTER_SPLIT_INVALID'])` |
| H0364 / R033 | K21:290 抛出 | `operation == 'remove' / mode == 'legacy' and (not confirmed)` → `_error('ROUTER_SPLIT_LEGACY_TEARDOWN', 409, MESSAGES['ROUTER_SPLIT_LEGACY_TEARDOWN'])` |
| H0365 / R033 | K21:294 抛出 | `mode == 'full_tun' / not supports_forwarding(agent)` → `_error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')` |
| H0366 / R033 | K21:298 抛出 | `mode == 'full_tun' / not keeps_forwarding(status)` → `_error('FORWARDING_NOT_PERSISTED', 409, FORWARDING_NOT_KEPT.format(action=''))` |
| H0367 / R033 | K21:312 抛出 | `not (confirmed) / not supports_forwarding(agent)` → `_error('UPGRADE_REQUIRED', 409, LEGACY_REMOVE_UPGRADE)` |
| H0368 / R033 | K21:314 抛出 | `not (confirmed) / not (not supports_forwarding(agent))` → `_error('FORWARDING_NOT_PERSISTED', 409, LEGACY_REMOVE_NOT_KEPT)` |
| H0369 / R033 | K21:317 抛出 | `not supports_forwarding(agent)` → `_error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')` |
| H0370 / R033 | K21:321 抛出 | `mode == 'full_tun' / body not in (None, {}) and (not isinstance(body, dict) or set(body) != {'prepare_tun'} or type(body['prepare_tun']) is not bool)` → `_error('INVALID_REQUEST', 400, '开启转发持久化只接受 prepare_tun 参数。')` |
| H0371 / R033 | K21:327 抛出 | `proxy_gateway_routers(get_config(profile_id), agent['id']) / body not in (None, {})` → `_error('INVALID_REQUEST', 400, '为代理网关开启转发持久化不接受额外参数。')` |
| H0372 / R033 | K21:330 抛出 | `not (mode == 'full_tun') / not (proxy_gateway_routers(get_config(profile_id), agent['id']))` → `_error('ROUTER_SPLIT_REQUIRED', 409, ROUTER_SPLIT_REQUIRED)` |
| H0373 / R033 | K21:338 抛出 | `not supports_standalone_forwarding(agent) and _forwarding_upgrade_needed(read_status(), agent)` → `_error('UPGRADE_REQUIRED', 409, STANDALONE_UPGRADE)` |
| H0374 / R033 | K21:343 抛出 | `agent.get('service_type') != 'mihomo'` → `_error('WRONG_SERVICE', 400, 'Linux 网关只能应用到 Mihomo Agent。')` |
| H0375 / R033 | K21:345 抛出 | `agent.get('deployment_method') == 'docker'` → `_error('UNSUPPORTED_ENVIRONMENT', 409, '自动网络配置适用于 Linux Shell 安装，请选择原生安装的 Mihomo Agent。')` |
| H0376 / R033 | K21:347 抛出 | `not supports_gateway(agent)` → `_error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {MIN_AGENT_VERSION} 或更新版本。')` |
| H0377 / R033 | K21:352 抛出 | `OPERATIONS.get(operation) != method` → `_error('NOT_ALLOWED', 404, '不支持这个网关操作。')` |
| H0378 / R033 | K21:355 抛出 | `not agent` → `_error('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')` |
| H0379 / R033 | K21:357 抛出 | `not profile_id or agent.get('profile_id') != profile_id` → `_error('PROFILE_CHANGED', 409, '设备绑定的配置空间已变化，请重新打开网关设置。')` |
| H0380 / R033 | K21:365 抛出 | `operation == 'apply' / (config.get('mihomo', {}).get('linux_gateway') or {}).get('enabled') is not True` → `_error('GATEWAY_DISABLED', 409, '请先启用并保存当前配置空间的 Linux 网关。')` |
| H0381 / R033 | K21:395 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_RESPONSE_BYTES` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据过大。')` |
| H0382 / R033 | K21:408 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `_error('AGENT_UNREACHABLE', 502, message)` |
| H0383 / R033 | K21:411 抛出 | `status == 404` → `_error('UPGRADE_REQUIRED', 409, f'设备尚未提供网关接口，请更新到包含本功能的 {minimum} 或更新版本。')` |
| H0384 / R033 | K21:413 抛出 | `status in (401, 403)` → `_error('AGENT_AUTH_FAILED', 502, 'Agent 认证失败，请检查设备注册状态。')` |
| H0385 / R033 | K21:417 抛出 | `捕获 (ValueError, UnicodeError)` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据无法解析，请刷新设备状态。')` |
| H0386 / R033 | K21:419 抛出 | `not isinstance(data, dict)` → `_error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据格式异常。')` |
| H0387 / R033 | K21:424 抛出 | `status >= 400` → `_error('GATEWAY_ERROR', status if status in (400, 409, 422) else 502, message[:1000])` |
| H0388 / R033 | K21:655 抛出 | `not agent` → `_error('AGENT_NOT_FOUND', 404, GATEWAY_FORWARDING_MESSAGES['missing'])` |
| H0389 / R033 | K21:657 抛出 | `agent.get('service_type') != 'mihomo'` → `_error('WRONG_SERVICE', 400, GATEWAY_FORWARDING_MESSAGES['not_mihomo'])` |
| H0390 / R033 | K21:659 抛出 | `agent.get('deployment_method') == 'docker'` → `_error('UNSUPPORTED_ENVIRONMENT', 409, GATEWAY_FORWARDING_MESSAGES['docker'])` |
| H0391 / R033 | K21:661 抛出 | `not is_online(agent)` → `_error('AGENT_OFFLINE', 409, 'Agent 离线，无法开启转发。')` |
| H0392 / R033 | K21:663 抛出 | `not supports_forwarding(agent)` → `_error('UPGRADE_REQUIRED', 409, GATEWAY_FORWARDING_MESSAGES['upgrade_required'])` |

### K22 [backend/agents/manager.py](../../backend/agents/manager.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0393 / R063 | K22:149 抛出 | `not saved` → `_NothingToSave()` |
| H0394 / R063 | K22:168 抛出 | `捕获 ProfileInUse` → `AgentDeploymentConflict(str(error))` |
| H0395 / R102 | K22:203 抛出 | `existing_agent / not _constant_time_ascii_equal(existing_token, existing_agent.get('token'))` → `PermissionError('Unauthorized')` |
| H0396 / R102 | K22:206 抛出 | `existing_agent / installation_id and existing_agent.get('gateway_installation_id') == installation_id and (requested_profile_id != existing_agent.get('profile_id'))` → `AgentDeploymentConflict('Installation Agent profile changed; prepare a new installation')` |
| H0397 / R102 | K22:211 抛出 | `existing_agent / requested_profile_id and requested_profile_id != (existing_agent.get('profile_id') or self.repository.default_profile_id()) and (_upgrade_busy(existing_agent) or any((task.g…（完整见源行）` → `AgentDeploymentConflict('Agent has an unfinished deployment or upgrade; recover it before changing profile')` |
| H0398 / R102 | K22:216 抛出 | `existing_agent / _dns_follow_busy(agents, existing_agent['id']) and (agent_data.get('service_type', 'mihomo') != existing_agent.get('service_type') or (requested_profile_id and requested_pro…（完整见源行）` → `AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再改变 Agent 注册信息')` |
| H0399 / R102 | K22:221 抛出 | `existing_agent / (_oxidns_ha_enabled(existing_agent) or _oxidns_ha_busy(existing_agent)) and (agent_data.get('service_type', 'mihomo') != existing_agent.get('service_type') or (requested_pro…（完整见源行）` → `AgentDeploymentConflict(OXIDNS_HA_LOCKED)` |
| H0400 / R005 | K22:339 抛出 | `for agent in agents / _upgrade_busy(agent)` → `AgentDeploymentConflict('Agent update is in progress; wait for confirmation before editing it')` |
| H0401 / R005 | K22:343 抛出 | `for agent in agents / _dns_follow_busy(agents, agent_id) and any((key in updates and updates[key] != agent.get(key) for key in ('host', 'port', 'enabled', 'service_type', 'profile_id')))` → `AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再修改 Agent')` |
| H0402 / R005 | K22:347 抛出 | `for agent in agents / (_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)) and any((key in updates and updates[key] != agent.get(key) for key in ('host', 'port', 'enabled', 'service_type', …（完整见源行）` → `AgentDeploymentConflict(OXIDNS_HA_LOCKED)` |
| H0403 / R005 | K22:353 抛出 | `for agent in agents / 'profile_id' in updates and updates['profile_id'] != (agent.get('profile_id') or self.repository.default_profile_id()) and any((task.get('status') not in ('succeeded', …（完整见源行）` → `AgentDeploymentConflict('Agent has an unfinished deployment; finish or recover it before changing profile')` |
| H0404 / R003 | K22:405 抛出 | `agent and _upgrade_busy(agent)` → `AgentDeploymentConflict('Agent 正在更新或等待恢复')` |
| H0405 / R003 | K22:407 抛出 | `_dns_follow_busy(agents, agent_id)` → `AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再删除 Agent')` |
| H0406 / R003 | K22:409 抛出 | `_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)` → `AgentDeploymentConflict(OXIDNS_HA_LOCKED)` |
| H0407 / R063 | K22:494 抛出 | `not isinstance(deployment_id, str) or not re.fullmatch('[A-Za-z0-9_-]{1,80}', deployment_id)` → `ValueError('Invalid deployment ID')` |
| H0408 / R063 | K22:498 错误结果 | `agent is None` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0409 / R063 | K22:505 错误结果 | `follow.get('status') in ('preparing', 'publishing') and follow.get('target_deployment_id') != deployment_id` → `{'success': False, 'message': 'DNS 联动发布正在执行，请等待结果后再发布 OxiDNS', 'http_status': 409}` |
| H0410 / R063 | K22:508 错误结果 | `any(((item.get('dns_follow') or {}).get('source_agent_id') == agent_id and (item.get('dns_follow_state') or {}).get('status') in ('preparing', 'publishing') for item in agents))` → `{'success': False, 'message': '绑定的 OxiDNS 正在同步，请等待结果后再发布 Mihomo', 'http_status': 409}` |
| H0411 / R063 | K22:510 错误结果 | `_upgrade_busy(agent)` → `{'success': False, 'message': 'Agent update is in progress', 'http_status': 409}` |
| H0412 / R063 | K22:517 错误结果 | `active` → `{'success': False, 'message': 'Agent already has an unfinished deployment', 'deployment_id': active['deployment_id'], 'status': active['status'], 'http_status': 409}` |
| H0413 / R063 | K22:528 错误结果 | `捕获 BlockingIOError` → `{'success': False, 'message': 'Deployment preparation already running', 'http_status': 409}` |
| H0414 / R063 | K22:566 错误结果 | `agent is None` → `{'success': False, 'message': 'Agent not found'}` |
| H0415 / R063 | K22:569 错误结果 | `state is None` → `{'success': False, 'message': 'Deployment not found', 'http_status': 404}` |
| H0416 / R013 | K22:749 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0417 / R013 | K22:752 错误结果 | `not state` → `{'success': False, 'message': 'Deployment not found', 'http_status': 404}` |
| H0418 / R013 | K22:754 错误结果 | `state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id())` → `{'success': False, 'message': 'Deployment belongs to a different profile', 'http_status': 409}` |
| H0419 / R013 | K22:758 错误结果 | `state.get('service_type') != agent.get('service_type', 'mihomo')` → `{**state, 'success': False, 'http_status': 409, 'message': 'Deployment belongs to a different service type'}` |
| H0420 / R013 | K22:772 错误结果 | `not state.get('submitted') / not state.get('submitted')` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'message': 'Preparation interrupted before upload; current service unchanged'}, only_unsubmitted=True)` |
| H0421 / R013 | K22:796 错误结果 | `result.get('http_status') == 404` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'message': 'Agent has no record of the uploaded deployment'})` |
| H0422 / R014 | K22:814 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0423 / R014 | K22:817 错误结果 | `state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id()) or state.get('service_type') != agent.get('service_type', 'mihomo')` → `{**state, 'success': False, 'http_status': 409, 'message': 'Deployment profile or service type changed; activation refused'}` |
| H0424 / R014 | K22:824 错误结果 | `state.get('status') == 'ready' and (not state.get('http_status')) / state.get('pending')` → `{**state, 'success': False, 'http_status': 409, 'message': '尚未确认 Agent 候选状态，请稍后重试激活'}` |
| H0425 / R014 | K22:836 错误结果 | `not receipt_matches(agent, state) / task.get('status') != 'ready' or task.get('sha256') != state.get('sha256')` → `{**task, 'success': False, 'http_status': 409, 'message': '候选状态已变化，请刷新后重试'}` |
| H0426 / R014 | K22:847 错误结果 | `state.get('http_status') or state.get('status') != 'ready'` → `{**state, 'success': False, 'message': 'Deployment must be ready before activation', 'http_status': 409}` |
| H0427 / R016 | K22:864 错误结果 | `_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent) or (agent.get('deployments', {}).get(deployment_id) or {}).get('ha_publication')` → `{'success': False, 'http_status': 409, 'rollback_rejected': True, 'rollback_message': '主备发布不能单独回滚一台；请将目标配置重新发布到主备', 'message': '主备发布不能单独回滚一台；请将目标配置重新发布到主备'}` |
| H0428 / R016 | K22:870 错误结果 | `agent is None` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0429 / R016 | K22:873 错误结果 | `not state` → `{'success': False, 'message': 'Deployment not found', 'http_status': 404}` |
| H0430 / R016 | K22:875 错误结果 | `_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)` → `{'success': False, 'http_status': 409, 'rollback_rejected': True, 'rollback_message': '请将目标配置重新发布到主备，不能单独回滚成员', 'message': '请将目标配置重新发布到主备，不能单独回滚成员'}` |
| H0431 / R016 | K22:879 错误结果 | `_upgrade_busy(agent)` → `{'success': False, 'message': 'Agent update is in progress', 'http_status': 409}` |
| H0432 / R016 | K22:881 错误结果 | `_dns_follow_busy(agents, agent_id)` → `{'success': False, 'message': 'DNS 联动发布尚未确认结束，请等待结果后再回滚', 'http_status': 409}` |
| H0433 / R016 | K22:885 错误结果 | `state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id()) or state.get('service_type', agent.get('service_type')) != agent.get('service_type') or (agent.ge…（完整见源行）` → `{'success': False, 'message': 'Only the latest deployment of this profile can be rolled back', 'http_status': 409}` |
| H0434 / R016 | K22:889 错误结果 | `state.get('status') != 'succeeded'` → `{'success': False, 'message': 'Only a successful deployment can be rolled back', 'http_status': 409}` |
| H0435 / R015 | K22:931 错误结果 | `agent is None` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0436 / R015 | K22:934 错误结果 | `not state` → `{'success': False, 'message': 'Deployment not found', 'http_status': 404}` |
| H0437 / R015 | K22:936 错误结果 | `_upgrade_busy(agent)` → `{'success': False, 'message': 'Agent update is in progress', 'http_status': 409}` |
| H0438 / R015 | K22:938 错误结果 | `state.get('status') != 'rollback_failed'` → `{'success': False, 'message': '只有回滚失败的发布需要重新恢复', 'http_status': 409}` |
| H0439 / R015 | K22:955 错误结果 | `捕获 requests.RequestException` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'unknown', 'message': 'Recovery outcome unknown; awaiting Agent status'})` |
| H0440 / R015 | K22:968 错误结果 | `result.get('http_status')` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'unknown', 'message': 'Recovery outcome unknown; awaiting Agent status'})` |
| H0441 / R015 | K22:970 错误结果 | `外部依赖/文件/事务异常` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'rolling_back'})` |
| H0442 / R063 | K22:1038 抛出 | `state['profile_id'] != prepared['profile_id'] or agent.get('service_type') != prepared['service_type']` → `ValueError('Deployment ownership changed during preparation')` |
| H0443 / R063 | K22:1054 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found', 'http_status': 404}` |
| H0444 / R063 | K22:1062 抛出 | `(agent.get('profile_id') or self.repository.default_profile_id()) != bundle.manifest['profile_id']` → `ValueError('Agent profile changed during preparation')` |
| H0445 / R063 | K22:1064 抛出 | `agent.get('service_type', 'mihomo') != bundle.manifest['service_type']` → `ValueError('Agent service type changed during preparation')` |
| H0446 / R063 | K22:1069 错误结果 | `1 not in capability.get('deployment_protocols', []) or capability.get('service_type') != agent.get('service_type')` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'http_status': 409, 'message': 'Agent does not support transactional deployments; upgrade the Shell-installed or Docker Agent first'})` |
| H0447 / R063 | K22:1073 错误结果 | `bundle.manifest.get('reload_only') and 1 not in capability.get('reload_only_protocols', [])` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'http_status': 409, 'message': '请升级 Agent 到 1.8.30-go 或更新版本后使用规则自动热加载'})` |
| H0448 / R063 | K22:1080 抛出 | `bundle.manifest.get('final_config') and (1 not in capability.get('final_config_protocols', []) or capability.get('config_path', 'config.yaml') != bundle.manifest['config_path'])` → `ValueError('目标设备配置已改变，请重新生成最终配置后推送。')` |
| H0449 / R063 | K22:1087 错误结果 | `previous.get('submitted') / previous.get('source_sha256', previous.get('sha256')) != bundle.sha256` → `{'success': False, 'deployment_id': deployment_id, 'http_status': 409, 'message': 'Deployment ID already belongs to different content'}` |
| H0450 / R063 | K22:1151 错误结果 | `捕获 ValueError` → `self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'failed_stage': 'preparing', 'message': details.splitlines()[0][:300] if details else '发布准备失败', 'error_details': details})` |
| H0451 / R063 | K22:1183 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0452 / R063 | K22:1188 错误结果 | `expected_profile_id is not None and (agent.get('profile_id') or self.repository.default_profile_id()) != expected_profile_id or (expected_service_type is not None and agent.get('service_type…（完整见源行）` → `{'success': False, 'message': '草稿已保存，但设备绑定已改变，请重新选择设备后发布。', 'http_status': 409}` |
| H0453 / R063 | K22:1194 错误结果 | `bundle is not None or agent.get('service_type', 'mihomo') in ('mihomo', 'mosdns', 'oxidns') / bundle is None` → `{'success': False, 'message': 'A complete deployment bundle is required', 'http_status': 409}` |
| H0454 / R063 | K22:1258 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': 'Unable to connect to Agent', 'error_details': failure_text(str(e), (agent.get('token'),))}` |
| H0455 / R066 | K22:1277 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0456 / R066 | K22:1280 错误结果 | `_upgrade_busy(agent)` → `{'success': False, 'message': 'Agent update is in progress'}` |
| H0457 / R066 | K22:1307 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': 'Unable to connect to Agent', 'error_details': failure_text(str(e), (agent.get('token'),))}` |
| H0458 / R063 | K22:1322 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0459 / R063 | K22:1338 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0460 / R063 | K22:1341 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0461 / R036 | K22:1351 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0462 / R036 | K22:1368 错误结果 | `外部依赖/文件/事务异常` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0463 / R036 | K22:1371 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0464 / R034 | K22:1388 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0465 / R034 | K22:1410 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0466 / R034 | K22:1413 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0467 / R035 | K22:1429 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0468 / R035 | K22:1446 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0469 / R035 | K22:1449 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0470 / R077 | K22:1467 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0471 / R077 | K22:1470 错误结果 | `_upgrade_busy(agent)` → `{'success': False, 'message': 'Agent update is in progress'}` |
| H0472 / R077 | K22:1490 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0473 / R077 | K22:1493 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0474 / R063 | K22:1513 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0475 / R063 | K22:1539 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0476 / R063 | K22:1542 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0477 / R037 | K22:1557 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0478 / R037 | K22:1575 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0479 / R037 | K22:1578 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0480 / R011 | K22:1592 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0481 / R011 | K22:1608 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0482 / R011 | K22:1611 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0483 / R012 | K22:1626 错误结果 | `not agent` → `{'success': False, 'message': 'Agent not found'}` |
| H0484 / R012 | K22:1655 错误结果 | `not (response.status_code == 200)` → `{'success': False, 'message': f'HTTP {response.status_code}'}` |
| H0485 / R012 | K22:1658 错误结果 | `捕获 requests.exceptions.RequestException` → `{'success': False, 'message': f'Connection error: {str(e)}'}` |
| H0486 / R021 | K22:1667 抛出 | `for agent in agents / enabled and agent.get('service_type') != 'mihomo'` → `ValueError('域名发现只支持 mihomo Agent')` |
| H0487 / R073 | K22:1683 抛出 | `for agent in agents / enabled and agent.get('service_type') != 'mihomo'` → `ValueError('流量记录只支持 mihomo Agent')` |
| H0488 / R063 | K22:1708 抛出 | `response.status_code == 404` → `RuntimeError(f"Agent {agent.get('name') or agent['id']} 不支持{feature}，请升级")` |
| H0489 / R063 | K22:1714 抛出 | `response.status_code == 409 and path.startswith('/api/region-check')` → `RuntimeError('Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置')` |
| H0490 / R063 | K22:1716 抛出 | `response.status_code != 200 or not body.get('success')` → `RuntimeError(body.get('message') or f'Agent 返回 HTTP {response.status_code}')` |
| H0491 / R063 | K22:1745 抛出 | `response.status_code == 404` → `RuntimeError(f"Agent {agent.get('name') or agent['id']} 不支持 DNS 高可用，请升级")` |
| H0492 / R063 | K22:1754 抛出 | `response.status_code != 200 or body.get('success') is False` → `RuntimeError(failure_text(str(message), (agent.get('token'),)))` |
| H0493 / R063 | K22:1777 抛出 | `method not in ('get', 'post', 'delete')` → `ValueError('Unsupported DNS handoff operation')` |

### K23 [backend/agents/metrics_flush.py](../../backend/agents/metrics_flush.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0494 / POST /api/agents/<agent_id>/heartbeat → metrics/任务读取 | K23:42 抛出 | `type(value) is not int or value not in INTERVAL_CHOICES` → `ValueError('保存间隔只能是 5、10、30 或 60 分钟')` |
| H0495 / POST /api/agents/<agent_id>/heartbeat → metrics/任务读取 | K23:236 抛出 | `外部依赖/文件/事务异常` → `SystemExit(0)` |

### K24 [backend/agents/metrics_history.py](../../backend/agents/metrics_history.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0496 / POST /api/agents/<agent_id>/heartbeat → metrics/任务读取 | K24:550 抛出 | `errors` → `MetricsFlushError(f"{len(errors)} 台 Agent 的监控历史写入失败（{'；'.join(reasons)}），数据仍在内存中，下次保存时重试。目录：{self.data_dir}")` |

### K25 [backend/agents/mihomo_panel.py](../../backend/agents/mihomo_panel.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0497 / R042 | K25:88 抛出 | `not agent` → `_fail('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')` |
| H0498 / R042 | K25:90 抛出 | `agent.get('service_type', 'mihomo') != 'mihomo'` → `_fail('WRONG_SERVICE', 400, 'Mihomo 面板仅适用于 Mihomo Agent。')` |
| H0499 / R042 | K25:97 抛出 | `not supported` → `_fail('UPGRADE_REQUIRED', 409, f'请将此 Agent 升级到 {min_version} 或更新版本后使用{feature}。')` |
| H0500 / R042 | K25:105 抛出 | `not isinstance(name, str) or not name or len(name) > MAX_NAME_LENGTH or any((ord(c) < 32 for c in name))` → `_fail('INVALID_REQUEST', 400)` |
| H0501 / R042 | K25:112 抛出 | `level not in LOG_LEVELS` → `_fail('INVALID_REQUEST', 400, '日志等级只能是 debug、info、warning 或 error。')` |
| H0502 / R042 | K25:115 抛出 | `not isinstance(after, str) or not after.isdigit() or len(after) > 18` → `_fail('INVALID_REQUEST', 400)` |
| H0503 / R042 | K25:126 抛出 | `not isinstance(test_url, str) or not test_url.startswith(('http://', 'https://')) or len(test_url) > 2048` → `_fail('INVALID_REQUEST', 400, '测速地址必须是 http(s) 链接。')` |
| H0504 / R042 | K25:130 抛出 | `捕获 (TypeError, ValueError)` → `_fail('INVALID_REQUEST', 400)` |
| H0505 / R042 | K25:132 抛出 | `not 500 <= timeout <= 30000` → `_fail('INVALID_REQUEST', 400, '测速超时需在 500–30000 毫秒之间。')` |
| H0506 / R042 | K25:143 抛出 | `resource == 'proxy' / not isinstance(body, dict) or set(body) != {'name'} or (not isinstance(body['name'], str)) or (not body['name']) or (len(body['name']) > MAX_NAME_LENGTH)` → `_fail('INVALID_REQUEST', 400, '请选择要切换的节点。')` |
| H0507 / R042 | K25:148 抛出 | `resource == 'configs' / not isinstance(body, dict) or set(body) != {'mode'} or body['mode'] not in MODES` → `_fail('INVALID_REQUEST', 400, '代理模式只能是 rule、global 或 direct。')` |
| H0508 / R042 | K25:236 抛出 | `not CORE_VERSION_PATTERN.fullmatch(version)` → `ValueError('version.txt is not a version')` |
| H0509 / R042 | K25:262 校验/降级 | `used != settings` → `False` |
| H0510 / R042 | K25:307 抛出 | `not isinstance(current, str) or not current` → `_fail('INVALID_RESPONSE')` |
| H0511 / R042 | K25:323 抛出 | `template is None` → `_fail('NOT_ALLOWED', 404, '面板不支持这个操作。')` |
| H0512 / R042 | K25:348 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_RESPONSE_BYTES` → `_fail('INVALID_RESPONSE')` |
| H0513 / R042 | K25:355 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `_fail('AGENT_UNREACHABLE')` |
| H0514 / R042 | K25:358 抛出 | `status in (401, 403)` → `_fail('AGENT_AUTH_FAILED')` |
| H0515 / R042 | K25:364 抛出 | `捕获 (ValueError, UnicodeError) / status == 404` → `_fail('UPGRADE_REQUIRED', 409, f'请将此 Agent 升级到 {MIN_AGENT_VERSION} 或更新版本后使用 Mihomo 面板。')` |
| H0516 / R042 | K25:366 抛出 | `捕获 (ValueError, UnicodeError) / status < 400` → `_fail('INVALID_RESPONSE')` |
| H0517 / R042 | K25:369 抛出 | `status >= 400 / isinstance(data, dict) and data.get('success') is False and (data.get('code') in MESSAGES)` → `_fail(data['code'], 409 if data['code'] == 'CONTROLLER_UNAVAILABLE' else 502)` |
| H0518 / R042 | K25:374 抛出 | `status >= 400` → `_fail('CORE_ERROR', 504 if status in (408, 504) else 400 if status < 500 else 502, message[:300])` |

### K26 [backend/agents/mihomo_resources.py](../../backend/agents/mihomo_resources.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0519 / R063 | K26:129 抛出 | `not isinstance(sources, dict)` → `DeploymentPreparationError('geox-url 必须是资源名称与下载地址的映射')` |
| H0520 / R063 | K26:140 抛出 | `for key in required_geo_resources(document, files) / not isinstance(url, str) or not url.strip()` → `RuleDownloadError(f'geox-url.{key} 未填写有效下载地址')` |
| H0521 / R063 | K26:147 抛出 | `for key in required_geo_resources(document, files) / not value or value.lstrip().lower().startswith((b'<!doctype html', b'<html'))` → `RuleDownloadError('下载结果为空或是网页，不是 Geo 数据文件')` |
| H0522 / R063 | K26:150 抛出 | `for key in required_geo_resources(document, files) / 捕获 RuleDownloadError` → `RuleDownloadError(failure_text(f'{label} 下载失败。本次发布已停止，设备仍使用原配置。\n原因：{reason}\n下载地址：{url}\n目标文件：{target}\n修复：更换 geox-url.{key}，或检查「GitHub 代理域名」和「系统设置 → 规则下载代理」。\n也可手动下载对应数据文件，放到 Agent 机器的 {target}，确保 Mihomo 可读取，再重新发布。' + (f'\n{details}' if details els…（完整见源行）` |
| H0523 / R063 | K26:159 抛出 | `for key in required_geo_resources(document, files) / len(value) > MAX_FILE_BYTES or len(files) >= MAX_FILES or sum((len(item[0]) for item in files.values())) + len(value) > MAX_TOTAL_BYTES` → `DeploymentPreparationError(f'{label} 超出发布文件大小限制')` |

### K27 [backend/agents/oxidns_cache.py](../../backend/agents/oxidns_cache.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0524 / R053 | K27:51 抛出 | `not agent` → `_fail('AGENT_NOT_FOUND', 404)` |
| H0525 / R053 | K27:53 抛出 | `agent.get('service_type') != 'oxidns'` → `_fail('WRONG_SERVICE', 400)` |
| H0526 / R053 | K27:59 抛出 | `not supported` → `_fail('UPGRADE_REQUIRED', 409)` |
| H0527 / R053 | K27:67 抛出 | `not isinstance(value, list) or len(value) > MAX_TAGS or any((not isinstance(tag, str) or not _TAG.fullmatch(tag) for tag in value)) or (len(set(value)) != len(value))` → `_fail('INVALID_RESPONSE')` |
| H0528 / R053 | K27:80 抛出 | `(method, tags is None) not in (('GET', True), ('POST', False)) or (tags is not None and (not valid_request_tags(tags)))` → `_fail('INVALID_REQUEST', 400)` |
| H0529 / R053 | K27:95 抛出 | `status in (401, 403)` → `_fail('AGENT_AUTH_FAILED')` |
| H0530 / R053 | K27:97 抛出 | `300 <= status < 400` → `_fail('INVALID_RESPONSE')` |
| H0531 / R053 | K27:102 抛出 | `for chunk in response.iter_content(4096) / size > MAX_RESPONSE_BYTES` → `_fail('INVALID_RESPONSE')` |
| H0532 / R053 | K27:107 抛出 | `捕获 (ValueError, UnicodeError)` → `_fail('UPGRADE_REQUIRED' if status == 404 else 'INVALID_RESPONSE', 409 if status == 404 else 502)` |
| H0533 / R053 | K27:112 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `_fail('AGENT_UNREACHABLE')` |
| H0534 / R053 | K27:114 抛出 | `not isinstance(data, dict)` → `_fail('INVALID_RESPONSE')` |
| H0535 / R053 | K27:118 抛出 | `status >= 400 or data.get('success') is False / status == 404 and code not in MESSAGES` → `_fail('UPGRADE_REQUIRED', 409)` |
| H0536 / R053 | K27:128 抛出 | `status != 200 or data.get('success') is not True` → `_fail('INVALID_RESPONSE')` |
| H0537 / R053 | K27:133 抛出 | `flushed != tags` → `_fail('INVALID_RESPONSE')` |

### K28 [backend/agents/oxidns_dashboard_state.py](../../backend/agents/oxidns_dashboard_state.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0538 / R058 | K28:47 校验/降级 | `not isinstance(value, dict)` → `False` |
| H0539 / R058 | K28:54 校验/降级 | `set(value) != fields` → `False` |
| H0540 / R058 | K28:56 校验/降级 | `value.get('schema') not in (1, 2) or type(value.get('schema')) is not int` → `False` |
| H0541 / R058 | K28:60 校验/降级 | `any((not isinstance(value.get(key), str) or not re.fullmatch(pattern, value[key]) for (key, pattern) in (('generation', '[0-9a-f]{32}'), ('target', '[0-9a-f]{64}'), ('source_revision', '[0-9…（完整见源行）` → `False` |
| H0542 / R058 | K28:64 校验/降级 | `type(value.get('started_at_ms')) is not int or not 0 <= value['started_at_ms'] <= 2 ** 53 - 1 or value.get('reason') not in _REASONS or (not isinstance(value.get('instance_id'), str)) or (no…（完整见源行）` → `False` |
| H0543 / R058 | K28:66 校验/降级 | `not validate_observations(value['baseline'])` → `False` |
| H0544 / R058 | K28:71 校验/降级 | `not validate_observations(value['carried']) or type(value['carried_partial']) is not bool or type(value['publication_count']) is not int or (not 0 <= value['publication_count'] <= 2 ** 53 - …（完整见源行）` → `False` |
| H0545 / R058 | K28:88 校验/降级 | `not isinstance(baseline, dict) or set(baseline) != set(_FIELDS)` → `False` |
| H0546 / R058 | K28:93 校验/降级 | `for (family, (labels, numbers)) in _FIELDS.items() / not isinstance(rows, list)` → `False` |
| H0547 / R058 | K28:96 校验/降级 | `for (family, (labels, numbers)) in _FIELDS.items() / total > MAX_ROWS` → `False` |
| H0548 / R058 | K28:100 校验/降级 | `for row in rows / not isinstance(row, dict) or set(row) - set(labels) - numbers` → `False` |
| H0549 / R058 | K28:102 校验/降级 | `for row in rows / any((not isinstance(row.get(key), str) or not 0 < len(row[key]) <= 16384 for key in labels))` → `False` |
| H0550 / R058 | K28:105 校验/降级 | `for row in rows / identity in seen or any((not _finite(row.get(key)) for key in numbers))` → `False` |
| H0551 / R058 | K28:122 抛出 | `not lock.acquire(timeout=LOCK_TIMEOUT)` → `DashboardStateError('DNS statistics state is busy')` |
| H0552 / R058 | K28:142 抛出 | `捕获 OSError / time.monotonic() >= deadline` → `DashboardStateError('DNS statistics state is busy')` |
| H0553 / R058 | K28:152 抛出 | `捕获 BlockingIOError / time.monotonic() >= deadline` → `DashboardStateError('DNS statistics state is busy')` |
| H0554 / R058 | K28:157 抛出 | `捕获 OSError` → `DashboardStateError('Cannot access DNS statistics state')` |
| H0555 / R058 | K28:178 抛出 | `捕获 OSError` → `DashboardStateError('Cannot read DNS statistics state')` |
| H0556 / R058 | K28:181 抛出 | `len(encoded) > MAX_STATE_BYTES` → `ValueError` |
| H0557 / R058 | K28:184 抛出 | `not validate_state(value)` → `ValueError` |
| H0558 / R058 | K28:186 抛出 | `捕获 (ValueError, UnicodeError, TypeError, OverflowError, RecursionError)` → `DashboardStateError('DNS statistics state is damaged')` |
| H0559 / R058 | K28:206 抛出 | `generation != expected` → `DashboardStateConflict()` |
| H0560 / R058 | K28:212 抛出 | `not validate_state(replacement)` → `DashboardStateError('Invalid DNS statistics state')` |
| H0561 / R058 | K28:216 抛出 | `len(encoded) > MAX_STATE_BYTES` → `DashboardStateError('DNS statistics state exceeds size limit')` |
| H0562 / R058 | K28:233 抛出 | `捕获 OSError` → `DashboardStateError('Cannot save DNS statistics state')` |

### K29 [backend/agents/oxidns_ha.py](../../backend/agents/oxidns_ha.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0563 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:55 抛出 | `捕获 BlockingIOError` → `HAError('DNS 高可用正在处理，请稍后重试')` |
| H0564 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:66 抛出 | `not isinstance(value, str)` → `ValueError` |
| H0565 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:90 抛出 | `not isinstance(request, dict) or set(request) - REQUEST_FIELDS` → `HAError('DNS 高可用参数无效')` |
| H0566 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:92 抛出 | `not agent or agent.get('service_type') != 'oxidns'` → `HAError('只有 OxiDNS Agent 可以开启 DNS 高可用')` |
| H0567 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:94 抛出 | `not isinstance(request.get('peer_id'), str) or not request['peer_id']` → `HAError('请选择另一台 OxiDNS 作为对端')` |
| H0568 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:96 抛出 | `not peer or peer.get('service_type') != 'oxidns'` → `HAError('对端 Agent 不存在或不是 OxiDNS')` |
| H0569 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:98 抛出 | `peer['id'] == agent['id']` → `HAError('对端不能是本机')` |
| H0570 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:101 抛出 | `for item in (agent, peer) / not item.get('enabled', True)` → `HAError(f'{_name(item)} 已停用，请先启用')` |
| H0571 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:103 抛出 | `agent.get('profile_id') != peer.get('profile_id')` → `HAError('两台 OxiDNS 必须使用同一个配置空间，才能给出相同的应答')` |
| H0572 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:106 抛出 | `not all(hosts)` → `HAError('两台 OxiDNS 的主机地址都必须是局域网 IPv4 地址')` |
| H0573 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:108 抛出 | `hosts[0] == hosts[1]` → `HAError('两台 OxiDNS 的主机地址不能相同')` |
| H0574 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:111 抛出 | `vip is None` → `HAError('虚拟 IP 必须是可用的单播 IPv4 地址，例如 192.168.1.53')` |
| H0575 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:113 抛出 | `vip in hosts` → `HAError('虚拟 IP 不能与任何一台 OxiDNS 的主机地址相同')` |
| H0576 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:118 抛出 | `not (vrid is None) / type(vrid) is not int or not 1 <= vrid <= 255` → `HAError('虚拟路由 ID（VRID）必须是 1 到 255 的整数')` |
| H0577 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:123 抛出 | `enabled(agent) or enabled(peer) / not _paired(agent, peer)` → `HAError(f'{_name(busy)} 已加入其他 DNS 高可用组，请先停用')` |
| H0578 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:130 抛出 | `for other in agents / other.get('host') == str(vip)` → `HAError(f'虚拟 IP {vip} 已是 Agent {_name(other)} 的主机地址')` |
| H0579 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:133 抛出 | `for other in agents / enabled(other) and other_ha.get('peer_id') in ids` → `HAError(f'{_name(other)} 的 DNS 高可用仍指向这两台之一，请先在该 Agent 上停用')` |
| H0580 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:135 抛出 | `for other in agents / enabled(other) and other_ha.get('vip') == str(vip)` → `HAError(f'虚拟 IP {vip} 已被另一组 DNS 高可用使用')` |
| H0581 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:148 抛出 | `捕获 requests.RequestException` → `HAError(f'{_name(agent)} 无法连接，请检查网络后重试')` |
| H0582 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:150 抛出 | `捕获 RuntimeError` → `HAError(f"{_name(agent)}：{failure_text(str(error), (agent.get('token'),))}")` |
| H0583 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:158 抛出 | `捕获 requests.RequestException` → `HAError(f'{_name(agent)} 无法连接，请检查网络后重试')` |
| H0584 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:160 抛出 | `not isinstance(capability, dict)` → `HAError(f'{_name(agent)} 的 DNS 高可用能力响应无效，请升级 Agent 后重试')` |
| H0585 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:163 抛出 | `not isinstance(protocols, list) or PROTOCOL not in protocols` → `HAError(f'{_name(agent)} 的 Agent 版本不支持 DNS 高可用，请先升级 Agent')` |
| H0586 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:190 抛出 | `for (member, capability, env, container) in zip(members, capabilities, environments, containers) / not isinstance(protocols, list) or PROTOCOL not in protocols or env.get('kind') != 'contain…（完整见源行）` → `HAError(f'{_name(member)} 的 Docker 高可用环境未就绪：{detail}', blockers=blockers)` |
| H0587 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:193 抛出 | `any((source not in ('file', 'docker-volume', 'installation') for source in sources))` → `HAError('宿主机身份来源无效，请升级两端 Agent 后重试')` |
| H0588 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:198 抛出 | `any((source in ('docker-volume', 'installation') for source in sources)) and (not all(valid_boot_ids))` → `HAError('Docker 安装身份需要确认两台独立宿主机的实时启动身份；请升级两端 Agent，并为容器只读挂载宿主 boot_id 后重试')` |
| H0589 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:201 抛出 | `all(valid_boot_ids) and boot_ids[0] == boot_ids[1]` → `HAError('两台 OxiDNS 实际位于同一台宿主机，无法提供主机故障接管；请部署到两台独立设备')` |
| H0590 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:207 抛出 | `any(containers) and (not all((isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) for value in host_ids)))` → `HAError('Docker DNS 高可用需要确认两台独立宿主机；请升级两端 Agent，并按部署说明提供宿主机身份')` |
| H0591 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:210 抛出 | `isinstance(host_ids[0], str) and re.fullmatch('[0-9a-f]{64}', host_ids[0]) and (host_ids[0] == host_ids[1])` → `HAError('两台 OxiDNS 实际位于同一台宿主机，无法提供主机故障接管；请部署到两台独立设备')` |
| H0592 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:386 抛出 | `外部依赖/文件/事务异常` → `HAError(message)` |
| H0593 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:401 抛出 | `blockers` → `HAError('无法开启 DNS 高可用：' + '；'.join(blockers), blockers=blockers)` |
| H0594 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:407 抛出 | `blockers` → `HAError('无法开启 DNS 高可用：' + '；'.join(blockers), blockers=blockers)` |
| H0595 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:427 抛出 | `blockers` → `HAError('无法开启 DNS 高可用：' + '；'.join(blockers), blockers=blockers)` |
| H0596 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:434 抛出 | `blockers` → `HAError('无法开启 DNS 高可用：' + '；'.join(blockers), blockers=blockers)` |
| H0597 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:452 抛出 | `for member in (agent, peer) / not stored or any((stored.get(key) != member.get(key) for key in ('host', 'port', 'service_type', 'profile_id', 'enabled')))` → `HAError('Agent 信息在开启过程中发生变化，请刷新后重试')` |
| H0598 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:454 抛出 | `for member in (agent, peer) / enabled(stored) and (stored['oxidns_ha'].get('group_id') != group_id or previous is None)` → `HAError(f'{_name(stored)} 已加入其他 DNS 高可用组，请刷新后重试')` |
| H0599 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:494 抛出 | `not acquired` → `HAError('主备配置正在处理，请等待完成后再停用高可用')` |
| H0600 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:510 抛出 | `active(agent)` → `HAError('主备配置发布尚未结束，请确认两台结果后再停用高可用')` |
| H0601 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:512 抛出 | `not agent` → `HAError('Agent 不存在')` |
| H0602 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:540 抛出 | `errors and (not force)` → `HAError(message + '。设备离线时可以强制清除记录，之后请在设备上手动删除 keepalived 中的 ConfigFlow 配置', can_force=True)` |
| H0603 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K29:605 抛出 | `not agent` → `HAError('Agent 不存在')` |

### K30 [backend/agents/oxidns_ha_handoff.py](../../backend/agents/oxidns_ha_handoff.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0604 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:22 抛出 | `捕获 Exception` → `HandoffPending('虚拟 IP 迁移操作结果待确认，尚未继续更新原主机', unknown=True)` |
| H0605 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:24 抛出 | `not isinstance(result, dict) or result.get('error') or result.get('success') is False` → `HandoffPending('无法确认虚拟 IP 迁移状态，尚未继续更新原主机', unknown=True)` |
| H0606 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:47 抛出 | `for member in members.values() / not isinstance(capability, dict)` → `ValueError('Agent 的 DNS 高可用能力响应无效，请升级 Agent 后重试')` |
| H0607 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:51 抛出 | `for member in members.values() / not isinstance(protocols, list) or 1 not in protocols` → `ValueError(f"{member.get('name') or member['id']} 的 Agent 不支持发布前迁移虚拟 IP，请先升级两台 Agent")` |
| H0608 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:55 抛出 | `paired / record and record.get('operation_id') != job['id']` → `ValueError('设备仍有其他虚拟 IP 迁移记录，请先确认上一任务的清理结果')` |
| H0609 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:67 抛出 | `for (ident, member) in members.items() / 捕获 Exception` → `HandoffPending('无法确认两台设备的虚拟 IP 状态，尚未继续更新原主机', unknown=True)` |
| H0610 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:71 抛出 | `for (ident, member) in members.items() / result.get('error') or not isinstance(runtime, dict) or managed.get('active') is not True or managed.get('pending')` → `HandoffPending('设备上的高可用状态尚未确认，尚未继续更新原主机', unknown=True)` |
| H0611 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:93 抛出 | `handoff.get('released')` → `ValueError('虚拟 IP 迁移已结束，未继续激活旧候选配置')` |
| H0612 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:98 抛出 | `not handoff.get('confirmed') and time.time() - handoff['requested_at'] >= TIMEOUT` → `ValueError('虚拟 IP 迁移或解析确认超时，原主机未更新')` |
| H0613 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:105 抛出 | `not record or record.get('operation_id') != handoff['operation_id'] or record.get('active') is not True` → `HandoffPending('虚拟 IP 让位记录尚未确认，原主机未更新', unknown=True)` |
| H0614 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:110 抛出 | `not _healthy_holder(views, target)` → `HandoffPending('等待已更新的设备独占虚拟 IP，并从两台确认虚拟 IP 的 DNS 解析')` |
| H0615 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:115 抛出 | `not handoff['confirmed']` → `HandoffPending('虚拟 IP 已迁移，正在再次确认 DNS 解析后更新原主机')` |
| H0616 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:125 抛出 | `not source` → `HandoffPending('原主机记录不可用，无法确认虚拟 IP 让位清理', unknown=True)` |
| H0617 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:131 抛出 | `record / record.get('operation_id') != handoff['operation_id']` → `HandoffPending('设备上的虚拟 IP 迁移记录已改变，需确认清理结果', unknown=True)` |
| H0618 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:135 抛出 | `record / record` → `HandoffPending('虚拟 IP 让位记录尚未清理，正在等待设备确认', unknown=True)` |
| H0619 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:148 抛出 | `not preferred or not _healthy_holder(_views(manager, members), preferred) / time.time() - job['handoff']['released_at'] >= TIMEOUT` → `ValueError('两台已更新，但恢复正常选举后的虚拟 IP 解析检查未通过')` |
| H0620 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:149 抛出 | `not preferred or not _healthy_holder(_views(manager, members), preferred)` → `HandoffPending('两台已更新，正在等待正常选举和虚拟 IP 解析恢复')` |
| H0621 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K30:167 抛出 | `all((view.get('healthy') is True and view.get('keepalived_running') is True for view in views.values())) / preferred and (not _healthy_holder(views, preferred))` → `HandoffPending('迁移已撤销，正在等待正常选举恢复后结束本次发布')` |

### K31 [backend/agents/oxidns_ha_publish.py](../../backend/agents/oxidns_ha_publish.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0622 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:46 错误结果 | `active(agent) and job.get('children', {}).get(agent.get('id')) != deployment_id` → `{'success': False, 'http_status': 409, 'message': 'DNS 主备正在同步，请等待两台发布完成', 'ha_publication': summary(job)}` |
| H0623 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:54 错误结果 | `not acquired` → `{'success': False, 'http_status': 409, 'message': 'DNS 主备正在处理，请等待两台确认完成'}` |
| H0624 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:60 错误结果 | `uninstall and (agent.get('oxidns_ha') or {}).get('enabled') is True` → `{'success': False, 'http_status': 409, 'message': '请先停用 DNS 高可用，再卸载成员'}` |
| H0625 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:74 抛出 | `not peer or other.get('enabled') is not True or (not ha.get('group_id')) or (other.get('group_id') != ha['group_id']) or (other.get('peer_id') != agent_id) or (peer.get('profile_id') != agen…（完整见源行）` → `ValueError('DNS 高可用配对不完整，请先修复主备关系')` |
| H0626 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:123 抛出 | `payload.get('id') != job['id']` → `ValueError('主备发布资料与当前任务不匹配，请重新发布')` |
| H0627 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:129 抛出 | `payload.get('bundle') / hashlib.sha256(archive).hexdigest() != meta['sha256']` → `ValueError('主备发布资料校验失败，请重新发布')` |
| H0628 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:152 抛出 | `for agent_id in order / not agent or (agent.get(FIELD) or {}).get('id') != job['id']` → `ValueError('主备发布记录已经改变，已停止继续操作')` |
| H0629 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:190 抛出 | `for member in members / not current or _identity(current) != _identity(member) or current.get('oxidns_ha') != member.get('oxidns_ha')` → `ValueError('主备设备信息已改变，请刷新后重试')` |
| H0630 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:192 抛出 | `for member in members / active(current)` → `ValueError('DNS 主备已有发布正在执行，请等待两台完成')` |
| H0631 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:194 抛出 | `for member in members / current.get('enabled') is False` → `ValueError('主备设备已停用，请先启用后发布')` |
| H0632 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:197 抛出 | `for member in members / upgrade and upgrade.get('status') not in CHILD_TERMINAL` → `ValueError('主备设备正在升级，请等待完成')` |
| H0633 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:201 抛出 | `for state in current.get('deployments', {}).values() / state.get('status') not in CHILD_TERMINAL and (not (current['id'] == owner_id and state.get('deployment_id') == deployment_id))` → `ValueError('主备设备已有未完成的发布，请先处理')` |
| H0634 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:212 抛出 | `for (agent_id, deployment_id) in job['children'].items() / not result.get('success')` → `ValueError(result.get('message') or '无法预留主备发布')` |
| H0635 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:222 抛出 | `active(agent) / existing.get('enable') != request or set(existing.get('children', {})) != {agent['id'], peer['id']}` → `ValueError('DNS 主备已有发布正在执行，请先等待完成')` |
| H0636 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:229 抛出 | `not acquired` → `ValueError('DNS 主备正在处理，请稍后重试')` |
| H0637 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:256 抛出 | `previous.get('children', {}).get(agent_id) == bundle.manifest['deployment_id'] / original is None or deployment_content_fingerprint(original.manifest) != deployment_content_fingerprint(bundl…（完整见源行）` → `ValueError('发布任务 ID 已用于其他内容，请使用新的发布任务')` |
| H0638 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:264 抛出 | `not acquired` → `ValueError('DNS 主备正在处理，请稍后重试')` |
| H0639 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:272 抛出 | `bundle.manifest.get('reload_only') or (follow.get('status') == 'publishing' and follow.get('target_deployment_id') == bundle.manifest['deployment_id']) / len(fingerprints) != 1 or not next(i…（完整见源行）` → `ValueError('主备配置基线不同，请先完整发布一次主备配置，再自动同步规则')` |
| H0640 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:321 抛出 | `any((not item or _identity(item) != job['identities'][key] for (key, item) in members.items()))` → `ValueError('主备设备信息在发布期间改变，已停止继续操作')` |
| H0641 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:329 抛出 | `捕获 Exception` → `HAStatusUnavailable('无法确认主备的 DNS 健康和虚拟 IP 状态')` |
| H0642 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:332 抛出 | `not isinstance(runtime, dict) or result.get('error')` → `HAStatusUnavailable('无法确认主备的 DNS 健康和虚拟 IP 状态')` |
| H0643 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:342 抛出 | `len(holders) != 1` → `ValueError('无法确认唯一持有虚拟 IP 的主机，未更新任何核心')` |
| H0644 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:367 抛出 | `外部依赖/文件/事务异常` → `ValueError('主备候选配置必须保留通配地址的 UDP 53 监听（如 0.0.0.0:53），未更新任何核心')` |
| H0645 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:397 抛出 | `bundle is None and job.get('kind') == 'rules' / baseline is None or baseline['manifest']['deployment_id'] != payload.get('baseline_deployment_id')` → `ValueError('主备规则更新基线已改变，请重新发布配置')` |
| H0646 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:503 抛出 | `members is None` → `ValueError('主备关系不可用')` |
| H0647 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:508 抛出 | `method == 'POST' and (not (active(agent) and job.get('kind') == 'rules')) / active(agent)` → `ValueError('DNS 主备正在发布，请等待两台完成后更新规则')` |
| H0648 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:512 抛出 | `method == 'POST' and (not (active(agent) and job.get('kind') == 'rules')) / baseline is None or len(fingerprints) != 1 or (not next(iter(fingerprints), None))` → `ValueError('请先完整发布相同的主备配置，再更新规则')` |
| H0649 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:515 抛出 | `method == 'POST' and (not (active(agent) and job.get('kind') == 'rules')) / not acquired` → `ValueError('DNS 主备正在处理，请稍后重试')` |
| H0650 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:619 错误结果 | `not acquired` → `{'success': False, 'http_status': 409, 'message': '主备发布正在处理，请稍后确认'}` |
| H0651 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:624 错误结果 | `job.get('status') != 'ready'` → `{'success': False, 'http_status': 409, 'message': '两台候选配置尚未就绪，请等待主备准备完成'}` |
| H0652 / PUT /api/agents/<agent_id>/oxidns-ha → 发布/轮询 | K31:628 错误结果 | `捕获 (ValueError, HAStatusUnavailable)` → `{'success': False, 'http_status': 409, 'message': '无法确认当前主备角色，未激活任何候选配置'}` |

### K32 [backend/agents/oxidns_metrics.py](../../backend/agents/oxidns_metrics.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0653 / R058 | K32:129 抛出 | `not isinstance(text, str)` → `ValueError('OxiDNS metrics must be text')` |
| H0654 / R058 | K32:131 抛出 | `len(text) > MAX_INPUT_BYTES` → `ValueError('OxiDNS metrics exceed size limit')` |
| H0655 / R058 | K32:134 抛出 | `len(text.encode('utf-8')) > MAX_INPUT_BYTES` → `ValueError('OxiDNS metrics exceed size limit')` |
| H0656 / R058 | K32:136 抛出 | `捕获 UnicodeEncodeError` → `ValueError('OxiDNS metrics contain invalid text')` |
| H0657 / R058 | K32:142 抛出 | `for raw in text.splitlines() / len(raw) > MAX_LINE_LENGTH` → `ValueError('OxiDNS metrics line exceeds size limit')` |
| H0658 / R058 | K32:148 抛出 | `for raw in text.splitlines() / sample_count > MAX_SAMPLES` → `ValueError('OxiDNS metrics exceed sample limit')` |

### K33 [backend/agents/oxidns_monitor.py](../../backend/agents/oxidns_monitor.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0659 / R058 | K33:98 抛出 | `not agent` → `MonitorError('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')` |
| H0660 / R058 | K33:100 抛出 | `agent.get('service_type') != 'oxidns'` → `MonitorError('WRONG_SERVICE', 400, 'DNS 监控仅适用于 OxiDNS Agent。')` |
| H0661 / R058 | K33:106 抛出 | `not supported` → `_upgrade_required(min_version)` |
| H0662 / R058 | K33:118 抛出 | `not isinstance(host, str) or not host or any((c in host for c in '/?#@\\'))` → `MonitorError('AGENT_UNREACHABLE')` |
| H0663 / R058 | K33:125 抛出 | `捕获 ValueError / not re.fullmatch('[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?', host)` → `MonitorError('AGENT_UNREACHABLE')` |
| H0664 / R058 | K33:128 抛出 | `isinstance(port, bool) or not isinstance(port, int) or (not 1 <= port <= 65535)` → `MonitorError('AGENT_UNREACHABLE')` |
| H0665 / R058 | K33:146 抛出 | `response.status_code in (401, 403)` → `MonitorError('AGENT_AUTH_FAILED')` |
| H0666 / R058 | K33:148 抛出 | `300 <= response.status_code < 400` → `MonitorError('INVALID_RESPONSE')` |
| H0667 / R058 | K33:153 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_RESPONSE_BYTES` → `MonitorError('INVALID_RESPONSE')` |
| H0668 / R058 | K33:159 抛出 | `捕获 (ValueError, UnicodeError) / response.status_code == 404` → `_upgrade_required(min_version)` |
| H0669 / R058 | K33:160 抛出 | `捕获 (ValueError, UnicodeError)` → `MonitorError('INVALID_RESPONSE')` |
| H0670 / R058 | K33:162 抛出 | `not isinstance(data, dict)` → `MonitorError('INVALID_RESPONSE')` |
| H0671 / R058 | K33:171 抛出 | `response.status_code >= 400 or data.get('success') is False or data.get('ok') is False / code == 'UPGRADE_REQUIRED'` → `_upgrade_required(min_version)` |
| H0672 / R058 | K33:172 抛出 | `response.status_code >= 400 or data.get('success') is False or data.get('ok') is False` → `MonitorError(code, status)` |
| H0673 / R058 | K33:176 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `MonitorError('AGENT_UNREACHABLE')` |
| H0674 / R058 | K33:204 抛出 | `for key in args / len(values) != 1 or (key == 'recorder' and (not _valid_recorder_tag(values[0]))) or (key == 'range' and values[0] not in RANKING_RANGES) or (key not in ('recorder', 'range'…（完整见源行）` → `MonitorError('INVALID_QUERY', 400)` |
| H0675 / R058 | K33:249 抛出 | `not isinstance(health, dict) or not isinstance(capabilities, dict) or (not isinstance(text, str))` → `MonitorError('INVALID_RESPONSE')` |
| H0676 / R058 | K33:252 抛出 | `not isinstance(recorders, list) or any((not isinstance(x, str) for x in recorders)) or len(recorders) > 64` → `MonitorError('INVALID_RESPONSE')` |
| H0677 / R058 | K33:256 抛出 | `捕获 (ValueError, TypeError)` → `MonitorError('INVALID_RESPONSE')` |
| H0678 / R058 | K33:261 抛出 | `not isinstance(labels, list) or len(labels) > CLIENT_SAMPLE_LIMIT` → `MonitorError('INVALID_RESPONSE')` |
| H0679 / R058 | K33:266 抛出 | `for item in labels / not isinstance(item, dict) or any((not isinstance(item.get(key), str) or len(item[key]) > 16384 for key in ('plugin_tag', 'upstream', 'address')))` → `MonitorError('INVALID_RESPONSE')` |
| H0680 / R058 | K33:269 抛出 | `for item in labels / identity in seen` → `MonitorError('INVALID_RESPONSE')` |
| H0681 / R058 | K33:279 抛出 | `dashboard and (any((not _valid_recorder_tag(tag) for tag in recorders)) or not isinstance(health.get('instance_id'), str) or (not re.fullmatch('[A-Za-z0-9_.:-]{1,128}', health['instance_id']…（完整见源行）` → `MonitorError('INVALID_RESPONSE')` |
| H0682 / R058 | K33:301 错误结果 | `外部依赖/文件/事务异常` → `{'available': False, 'success': False, 'code': code, 'reason': reason, 'sample_size': 0, 'rows': [], 'at_limit': False, **fields}` |
| H0683 / R058 | K33:307 抛出 | `not isinstance(value, dict)` → `MonitorError('INVALID_RESPONSE')` |
| H0684 / R058 | K33:313 抛出 | `type(sample) is not int or not 0 <= sample <= CLIENT_SAMPLE_LIMIT or (not isinstance(rows, list)) or (len(rows) > limit) or (type(at_limit) is not bool) or (at_limit != (sample == CLIENT_SAM…（完整见源行）` → `MonitorError('INVALID_RESPONSE')` |
| H0685 / R058 | K33:317 抛出 | `for row in rows / not isinstance(row, dict) or not isinstance(row.get('key'), str)` → `MonitorError('INVALID_RESPONSE')` |
| H0686 / R058 | K33:322 抛出 | `for row in rows / not key or len(key) > 1024 or any((ord(char) < 32 or 127 <= ord(char) <= 159 for char in key)) or (key in seen) or (type(count) is not int) or (not 0 < count <= sample * (1…（完整见源行）` → `MonitorError('INVALID_RESPONSE')` |
| H0687 / R058 | K33:326 抛出 | `kind == 'clients' / '%' in key` → `ValueError` |
| H0688 / R058 | K33:329 抛出 | `kind == 'clients' / 捕获 ValueError` → `MonitorError('INVALID_RESPONSE')` |
| H0689 / R058 | K33:336 抛出 | `total > sample * (1 if kind == 'clients' else 65535)` → `MonitorError('INVALID_RESPONSE')` |
| H0690 / R058 | K33:383 抛出 | `params.get('recorder') and params['recorder'] not in recorders` → `MonitorError('INVALID_QUERY', 400)` |
| H0691 / R058 | K33:395 抛出 | `data.get('success') is not True or not _valid_recorder_tag(recorder) or recorder not in recorders or (params.get('recorder', recorder) != recorder) or (type(data.get('collected_at_ms')) is n…（完整见源行）` → `MonitorError('INVALID_RESPONSE')` |
| H0692 / R058 | K33:399 抛出 | `data.get('instance_id') != state['instance_id'] or data.get('source_revision') != state['source_revision'] or data.get('runtime_revision') != state['runtime_revision'] or (data['collected_at…（完整见源行）` → `MonitorError('SNAPSHOT_CHANGED', 409)` |
| H0693 / R058 | K33:402 抛出 | `not isinstance(blocked, dict) or type(blocked.get('available')) is not bool` → `MonitorError('INVALID_RESPONSE')` |
| H0694 / R058 | K33:406 抛出 | `blocked['available'] / not isinstance(code, str) or not re.fullmatch('[a-z_]{1,64}', code)` → `MonitorError('INVALID_RESPONSE')` |
| H0695 / R058 | K33:411 抛出 | `'matcher_tag' in blocked / not _valid_recorder_tag(blocked['matcher_tag'])` → `MonitorError('INVALID_RESPONSE')` |
| H0696 / R058 | K33:417 抛出 | `'matcher_tags' in blocked / not isinstance(tags, list) or not 0 < len(tags) <= BLOCK_RANKING_MATCHER_LIMIT or (not all((_valid_recorder_tag(tag) for tag in tags)))` → `MonitorError('INVALID_RESPONSE')` |
| H0697 / R058 | K33:483 抛出 | `current is None or _target(current) != expected` → `MonitorError('DASHBOARD_CHANGED', 409)` |
| H0698 / R058 | K33:507 抛出 | `snapshot['collected_at_ms'] < state['started_at_ms']` → `MonitorError('SNAPSHOT_CHANGED', 409)` |
| H0699 / R058 | K33:541 抛出 | `捕获 oxidns_publication_stats.PublicationPending` → `MonitorError('SNAPSHOT_CHANGED', 409)` |
| H0700 / R058 | K33:543 抛出 | `捕获 DashboardStateError` → `MonitorError('DASHBOARD_STATE_ERROR', 500)` |
| H0701 / R058 | K33:545 抛出 | `捕获 DashboardStateConflict` → `MonitorError('DASHBOARD_CHANGED', 409)` |
| H0702 / R058 | K33:555 抛出 | `not isinstance(body, dict) or set(body) - {'recorder'} or ('recorder' in body and (not _valid_recorder_tag(body['recorder'])))` → `MonitorError('INVALID_QUERY', 400)` |
| H0703 / R058 | K33:567 抛出 | `for key in args / key not in allowed or len(values) != 1` → `MonitorError('INVALID_QUERY', 400)` |
| H0704 / R058 | K33:570 抛出 | `for key in args / not isinstance(value, str) or len(value.encode()) > 1024 or any((ord(c) < 32 for c in value))` → `MonitorError('INVALID_QUERY', 400)` |
| H0705 / R058 | K33:576 抛出 | `key in ('limit', 'since_ms', 'until_ms') / not re.fullmatch('[0-9]{1,16}', value)` → `MonitorError('INVALID_QUERY', 400)` |
| H0706 / R058 | K33:579 抛出 | `key in ('limit', 'since_ms', 'until_ms') / n > MAX_INTEGER or (key == 'limit' and (not 1 <= n <= 100))` → `MonitorError('INVALID_QUERY', 400)` |
| H0707 / R058 | K33:583 抛出 | `key == 'cursor' / not re.fullmatch('[0-9]{1,16}:[1-9][0-9]{0,15}', value) or any((int(x) > MAX_INTEGER for x in value.split(':')))` → `MonitorError('INVALID_QUERY', 400)` |
| H0708 / R058 | K33:585 抛出 | `not (key == 'cursor') / key == 'status' and value not in ('all', 'error', 'has_response', 'no_response')` → `MonitorError('INVALID_QUERY', 400)` |
| H0709 / R058 | K33:587 抛出 | `not (key == 'status' and value not in ('all', 'error', 'has_response', 'no_response')) / key in ('recorder', 'matcher_tag') and (len(value) > 256 or any((c in value for c in '/?#\\')))` → `MonitorError('INVALID_QUERY', 400)` |
| H0710 / R058 | K33:591 抛出 | `key == 'client_ip' / '%' in value` → `ValueError` |
| H0711 / R058 | K33:594 抛出 | `key == 'client_ip' / 捕获 ValueError` → `MonitorError('INVALID_QUERY', 400)` |
| H0712 / R058 | K33:597 抛出 | `key == 'qtype' / not re.fullmatch('[A-Za-z0-9_-]{1,32}', value)` → `MonitorError('INVALID_QUERY', 400)` |
| H0713 / R058 | K33:601 抛出 | `key == 'rcode' / not re.fullmatch('[A-Za-z0-9 _-]{1,64}', value)` → `MonitorError('INVALID_QUERY', 400)` |
| H0714 / R058 | K33:605 抛出 | `'since_ms' in result and 'until_ms' in result and (int(result['since_ms']) > int(result['until_ms']))` → `MonitorError('INVALID_QUERY', 400)` |
| H0715 / R058 | K33:613 抛出 | `not isinstance(value, dict) or type(value.get('id')) is not int or (not 0 < value['id'] <= MAX_INTEGER)` → `MonitorError('INVALID_RESPONSE')` |
| H0716 / R058 | K33:621 抛出 | `for key in ('created_at_ms', 'elapsed_ms') / _number(record.get(key)) is None` → `MonitorError('INVALID_RESPONSE')` |
| H0717 / R058 | K33:623 抛出 | `not isinstance(record.get('questions_json'), list) or not isinstance(record.get('answers_json'), list)` → `MonitorError('INVALID_RESPONSE')` |
| H0718 / R058 | K33:626 抛出 | `detail / not isinstance(value.get('steps'), list)` → `MonitorError('INVALID_RESPONSE')` |
| H0719 / R058 | K33:644 抛出 | `data.get('ok') is not True or not isinstance(records, list) or len(records) > 100` → `MonitorError('INVALID_RESPONSE')` |
| H0720 / R058 | K33:646 抛出 | `cursor is not None and (not isinstance(cursor, str) or not re.fullmatch('[0-9]{1,16}:[1-9][0-9]{0,15}', cursor))` → `MonitorError('INVALID_RESPONSE')` |
| H0721 / R058 | K33:654 抛出 | `not re.fullmatch('[1-9][0-9]{0,15}', record_id) or int(record_id) > MAX_INTEGER` → `MonitorError('INVALID_QUERY', 400)` |
| H0722 / R058 | K33:658 抛出 | `data.get('ok') is not True` → `MonitorError('INVALID_RESPONSE')` |
| H0723 / R058 | K33:669 抛出 | `for key in args / key not in allowed or len(values) != 1 or (not isinstance(values[0], str))` → `MonitorError('INVALID_QUERY', 400)` |
| H0724 / R058 | K33:674 抛出 | `for key in args / 捕获 UnicodeError` → `MonitorError('INVALID_QUERY', 400)` |
| H0725 / R058 | K33:676 抛出 | `for key in args / any((ord(char) < 32 or 127 <= ord(char) <= 159 for char in value))` → `MonitorError('INVALID_QUERY', 400)` |
| H0726 / R058 | K33:679 抛出 | `key == 'search' / len(encoded) > 64` → `MonitorError('INVALID_QUERY', 400)` |
| H0727 / R058 | K33:682 抛出 | `key == 'recorder' / not _valid_recorder_tag(value)` → `MonitorError('INVALID_QUERY', 400)` |
| H0728 / R058 | K33:684 抛出 | `not (key == 'recorder') / not re.fullmatch('[0-9]{1,16}', value) or int(value) > MAX_INTEGER` → `MonitorError('INVALID_QUERY', 400)` |
| H0729 / R058 | K33:689 抛出 | `'since_ms' in result and 'until_ms' in result and (int(result['since_ms']) > int(result['until_ms']))` → `MonitorError('INVALID_QUERY', 400)` |
| H0730 / R058 | K33:707 抛出 | `data.get('ok') is not True or not _valid_recorder_tag(recorder) or params.get('recorder', recorder) != recorder or (type(sample) is not int) or (not 0 <= sample <= CLIENT_SAMPLE_LIMIT) or (n…（完整见源行）` → `MonitorError('INVALID_RESPONSE')` |
| H0731 / R058 | K33:711 抛出 | `for row in rows / not isinstance(row, dict) or not isinstance(row.get('key'), str)` → `MonitorError('INVALID_RESPONSE')` |
| H0732 / R058 | K33:715 抛出 | `for row in rows / '%' in ip` → `ValueError` |
| H0733 / R058 | K33:718 抛出 | `for row in rows / 捕获 ValueError` → `MonitorError('INVALID_RESPONSE')` |
| H0734 / R058 | K33:720 抛出 | `for row in rows / ip in seen or type(count) is not int or (not 0 < count <= sample)` → `MonitorError('INVALID_RESPONSE')` |
| H0735 / R058 | K33:725 抛出 | `total != sample` → `MonitorError('INVALID_RESPONSE')` |

### K34 [backend/agents/oxidns_publication_stats.py](../../backend/agents/oxidns_publication_stats.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0736 / R058 | K34:58 抛出 | `pending and (not manual) and (previous['target'] == target) / snapshot['collected_at_ms'] < pending['collected_at_ms']` → `PublicationPending()` |
| H0737 / R058 | K34:61 抛出 | `status and status not in TERMINAL / reason` → `PublicationPending()` |
| H0738 / R058 | K34:116 抛出 | `snapshot['collected_at_ms'] < state['started_at_ms']` → `PublicationPending()` |
| H0739 / R058 | K34:118 抛出 | `state.get('pending') and state['pending']['deployment_id'] != deployment_id` → `PublicationPending()` |
| H0740 / R058 | K34:131 抛出 | `_task(manager.get_agent_by_id(agent_id), pending).get('status') not in ('uploading', 'ready')` → `DashboardStateConflict()` |

### K35 [backend/agents/oxidns_ranking_history.py](../../backend/agents/oxidns_ranking_history.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0741 / R058 | K35:266 抛出 | `data.get('success') is not True or data.get('recorder') != recorder` → `SlotIncomplete('unexpected answer')` |
| H0742 / R058 | K35:270 抛出 | `not isinstance(blocked, dict) or type(blocked.get('available')) is not bool` → `SlotIncomplete('unexpected answer')` |

### K36 [backend/agents/oxidns_rule_update.py](../../backend/agents/oxidns_rule_update.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0743 / R062 | K36:58 抛出 | `not agent` → `_fail('AGENT_NOT_FOUND', 404)` |
| H0744 / R062 | K36:60 抛出 | `agent.get('service_type') != 'oxidns'` → `_fail('WRONG_SERVICE', 400)` |
| H0745 / R062 | K36:66 抛出 | `not supported` → `_fail('UPGRADE_REQUIRED', 409)` |
| H0746 / R062 | K36:80 抛出 | `not isinstance(value, dict)` → `_fail('INVALID_RESPONSE')` |
| H0747 / R062 | K36:84 抛出 | `state not in ('idle', 'running', 'succeeded', 'failed') or phase not in ('idle', 'downloading', 'reloading', 'done')` → `_fail('INVALID_RESPONSE')` |
| H0748 / R062 | K36:87 抛出 | `not isinstance(task_id, str) or (state == 'idle' and task_id != '') or (state != 'idle' and (not re.fullmatch('[A-Za-z0-9_-]{1,96}', task_id)))` → `_fail('INVALID_RESPONSE')` |
| H0749 / R062 | K36:92 抛出 | `for key in ('downloaded', 'download_total', 'reloaded', 'reload_total') / type(count) is not int or not 0 <= count <= 1000000` → `_fail('INVALID_RESPONSE')` |
| H0750 / R062 | K36:95 抛出 | `result['downloaded'] > result['download_total'] or result['reloaded'] > result['reload_total']` → `_fail('INVALID_RESPONSE')` |
| H0751 / R062 | K36:98 抛出 | `state == 'succeeded' and (phase != 'done' or result['downloaded'] != result['download_total'] or result['reloaded'] != result['reload_total'])` → `_fail('INVALID_RESPONSE')` |
| H0752 / R062 | K36:110 抛出 | `method not in ('GET', 'POST') or (paths is not None and (method != 'POST' or not isinstance(paths, list) or (not 0 < len(paths) <= MAX_PATHS) or any((not isinstance(path, str) or not path fo…（完整见源行）` → `_fail('INVALID_REQUEST', 400)` |
| H0753 / R062 | K36:116 抛出 | `method == 'POST' and active(agent) and (not (paired and (agent.get(FIELD) or {}).get('kind') == 'rules'))` → `MonitorError('SERVICE_BUSY', 409, 'DNS 主备正在同步，请等待两台发布完成后更新规则')` |
| H0754 / R062 | K36:122 抛出 | `paired / 捕获 ValueError` → `MonitorError('SERVICE_BUSY', 409, str(error))` |
| H0755 / R062 | K36:124 抛出 | `paths is not None and (not supports_paths(agent))` → `_fail('UPGRADE_REQUIRED', 409)` |
| H0756 / R062 | K36:138 抛出 | `status in (401, 403)` → `_fail('AGENT_AUTH_FAILED')` |
| H0757 / R062 | K36:140 抛出 | `300 <= status < 400` → `_fail('INVALID_RESPONSE')` |
| H0758 / R062 | K36:145 抛出 | `for chunk in response.iter_content(4096) / size > MAX_RESPONSE_BYTES` → `_fail('INVALID_RESPONSE')` |
| H0759 / R062 | K36:150 抛出 | `捕获 (ValueError, UnicodeError)` → `_fail('UPGRADE_REQUIRED' if status == 404 else 'INVALID_RESPONSE', 409 if status == 404 else 502)` |
| H0760 / R062 | K36:155 抛出 | `捕获 (requests.RequestException, KeyError, TypeError, ValueError)` → `_fail('AGENT_UNREACHABLE')` |
| H0761 / R062 | K36:157 抛出 | `status == 404` → `_fail('UPGRADE_REQUIRED', 409)` |
| H0762 / R062 | K36:159 抛出 | `not isinstance(data, dict)` → `_fail('INVALID_RESPONSE')` |
| H0763 / R062 | K36:162 抛出 | `status >= 400 or data.get('success') is False` → `_fail(code if isinstance(code, str) and code in MESSAGES else 'RULE_UPDATE_FAILED', status if status in (400, 409, 503, 504) else 502)` |
| H0764 / R062 | K36:165 抛出 | `status not in (200, 202) or data.get('success') is not True` → `_fail('INVALID_RESPONSE')` |

### K37 [backend/agents/oxidns_validation_receipt.py](../../backend/agents/oxidns_validation_receipt.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0765 / R063 | K37:41 抛出 | `manifest['service_type'] != 'oxidns' or hashlib.sha256(bundle.archive).hexdigest() != bundle.sha256` → `DeploymentPreparationError('Invalid OxiDNS deployment archive digest')` |
| H0766 / R063 | K37:49 抛出 | `packed_manifest is None or json.load(packed_manifest) != manifest or (not member.isfile()) or (member.size != main['size'])` → `DeploymentPreparationError('OxiDNS deployment manifest does not match its archive')` |
| H0767 / R063 | K37:52 抛出 | `hashlib.sha256(content).hexdigest() != main['sha256']` → `DeploymentPreparationError('OxiDNS configuration digest does not match its manifest')` |
| H0768 / R063 | K37:55 抛出 | `捕获 (tarfile.TarError, KeyError, UnicodeError, json.JSONDecodeError, EOFError, OSError)` → `DeploymentPreparationError('Invalid OxiDNS deployment archive')` |
| H0769 / R063 | K37:59 抛出 | `document.get('include')` → `DeploymentPreparationError('OxiDNS 发布配置必须先展开 include 后重新推送')` |

### K38 [backend/agents/push_diff.py](../../backend/agents/push_diff.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0770 / R064 | K38:1159 抛出 | `not isinstance(proxies, list)` → `ValueError('not a node list')` |

### K39 [backend/agents/rule_sync.py](../../backend/agents/rule_sync.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0771 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:111 抛出 | `agent is None` → `KeyError('Agent not found')` |
| H0772 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:113 抛出 | `agent.get('service_type') not in ('mihomo', 'oxidns')` → `ValueError('规则自动同步仅支持 Mihomo 和 OxiDNS')` |
| H0773 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:129 抛出 | `not isinstance(settings, dict) or set(settings) != {'library', 'rules'} or any((type(value) is not bool for value in settings.values()))` → `ValueError('library 和 rules 必须分别为布尔值')` |
| H0774 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:135 抛出 | `publications.active(agent) and (agent.get(publications.FIELD) or {}).get('enable')` → `ValueError('DNS 高可用正在开启，请完成后再修改主备规则自动同步设置')` |
| H0775 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:143 抛出 | `for member in members / not current or current.get('oxidns_ha') != member.get('oxidns_ha')` → `ValueError('主备关系已改变，请刷新后重试')` |
| H0776 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:460 抛出 | `old_state.get('native_task') and agent['service_type'] == 'oxidns' / baseline is None` → `ValueError('Rule sync receipt is no longer available')` |
| H0777 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:494 抛出 | `baseline is None` → `ValueError('Rule sync receipt is no longer available')` |
| H0778 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K39:585 抛出 | `not _unchanged(self.manager, agent, settings, deployment_id=deployment_id)` → `ValueError('Rule sync target or settings changed during preparation')` |

### K40 [backend/agents/rule_sync_projection.py](../../backend/agents/rule_sync_projection.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0779 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:160 抛出 | `for item in config.get('rule_configs', []) / source is None` → `RuleSyncProjectionError('规则集引用的共享来源不存在，请先修复规则引用。')` |
| H0780 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:185 抛出 | `any((not before['capabilities'][key]['editable'] for key in keys))` → `RuleSyncProjectionError('OxiDNS 原生分流包含无法识别的自定义结构，无法自动应用规则结构；请手动检查并发布。')` |
| H0781 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:193 抛出 | `current_oxi.get('config_mode') == 'native' and (not same_document) / any((not state['capabilities'][key]['editable'] for key in keys))` → `RuleSyncProjectionError('OxiDNS 草稿分流无法唯一对应当前规则，请手动检查并发布。')` |
| H0782 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:215 抛出 | `any((set(group['provider']) - {'tag', 'type', 'args'} for group in old_groups))` → `RuleSyncProjectionError('OxiDNS 规则 Provider 包含额外原生参数，请手动检查并发布。')` |
| H0783 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:221 抛出 | `for tag in old_tags / _references({key: value for (key, value) in document.items() if key != 'plugins'}, tag) or _references(before['values'].get('custom_matches', []), tag) or any((_referen…（完整见源行）` → `RuleSyncProjectionError('OxiDNS 规则 Provider 被自定义插件共用，无法安全自动替换；请手动发布。')` |
| H0784 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:228 抛出 | `捕获 OxidnsSettingsError` → `RuleSyncProjectionError(f"OxiDNS 规则分流无法自动更新：{str(error).rstrip('。')}；请手动检查并发布。")` |
| H0785 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:238 抛出 | `for plugin in result.get('plugins', []) / empty_download or empty_reload` → `RuleSyncProjectionError('本次规则变更需要停用 OxiDNS 规则下载或重载服务，无法单独自动同步；请手动完整发布。')` |
| H0786 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:304 抛出 | `service_type != 'oxidns'` → `RuleSyncProjectionError('规则自动同步仅支持 Mihomo 和 OxiDNS。')` |
| H0787 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:429 抛出 | `hashlib.sha256(bundle.archive).hexdigest() != bundle.sha256` → `DeploymentPreparationError('Stored rule-sync deployment digest mismatch')` |
| H0788 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:435 抛出 | `for entry in bundle.manifest['files'] / not member.isfile() or member.size != entry['size']` → `DeploymentPreparationError('Stored rule-sync deployment file mismatch')` |
| H0789 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:438 抛出 | `for entry in bundle.manifest['files'] / hashlib.sha256(content).hexdigest() != entry['sha256']` → `DeploymentPreparationError('Stored rule-sync deployment file digest mismatch')` |
| H0790 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:445 抛出 | `not isinstance(plugin, dict) or plugin.get('type') != 'sequence' or (not isinstance(plugin.get('args'), list))` → `RuleSyncProjectionError('OxiDNS 主执行链无法安全隔离规则更新。')` |
| H0791 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:464 抛出 | `ip_indices / start == 0 or args[start - 1] != {'exec': 'jump local_probe'} or ip_indices != list(range(start, end + 1)) or (end + 1 >= len(args)) or (args[end + 1] not in ({'exec': 'drop_res…（完整见源行）` → `RuleSyncProjectionError('OxiDNS IP 分流区包含自定义结构，无法自动隔离。')` |
| H0792 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:484 抛出 | `tag == BLACKHOLE_TAG / plugin != BLACKHOLE_PLUGIN or type(plugin['args']['short_circuit']) is not bool` → `RuleSyncProjectionError('OxiDNS 候选配置的黑洞插件不是规则生成的格式。')` |
| H0793 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:487 抛出 | `not (tag == BLACKHOLE_TAG) / not (_canonical_chain(match[1], right) and match[1] in published and (tag.startswith(UPSTREAM_PREFIX) or {'exec': f'${tag}'} in right[UPSTREAM_PREFIX + match[1]]…（完整见源行）` → `RuleSyncProjectionError('OxiDNS 候选配置包含未发布的指定上游，请手动发布。')` |
| H0794 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:499 抛出 | `len(left) != len(previous.get('plugins', [])) or len(right) != len(candidate.get('plugins', []))` → `RuleSyncProjectionError('OxiDNS 插件标签重复，无法隔离规则更新。')` |
| H0795 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:504 抛出 | `_sequence_skeleton(left.get('sequence_main')) != _sequence_skeleton(right.get('sequence_main'))` → `RuleSyncProjectionError('OxiDNS 候选配置包含规则范围之外的执行链变化。')` |
| H0796 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:509 抛出 | `for tag in set(left) &#124; set(right) / left.get(tag) != right.get(tag)` → `RuleSyncProjectionError('OxiDNS 候选配置包含规则范围之外的插件变化。')` |
| H0797 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:513 抛出 | `for tag in ('rules_download', 'rules_reload') / (old is None) != (new is None)` → `RuleSyncProjectionError('自动同步不能启用或关闭 OxiDNS 下载和重载服务。')` |
| H0798 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:524 抛出 | `old is not None / old_meta != new_meta` → `RuleSyncProjectionError('OxiDNS 候选配置改变了下载和重载服务参数。')` |
| H0799 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:529 抛出 | `[tag for tag in left if not allowed(tag)] != [tag for tag in right if not allowed(tag)]` → `RuleSyncProjectionError('OxiDNS 候选配置改变了非规则插件顺序。')` |
| H0800 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:541 抛出 | `(old_paths &#124; new_paths) & other_paths` → `RuleSyncProjectionError('OxiDNS 规则文件被其他插件共用，无法单独自动更新。')` |
| H0801 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:545 抛出 | `'rules_download' in result_plugins / not isinstance(entry, dict)` → `RuleSyncProjectionError('OxiDNS 规则下载条目格式无效。')` |
| H0802 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:567 抛出 | `for field in ('agent_id', 'profile_id', 'service_type') / baseline_bundle.manifest.get(field) != candidate_bundle.manifest.get(field)` → `RuleSyncProjectionError('规则同步的设备或配置空间已经改变。')` |
| H0803 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:569 抛出 | `candidate_bundle.manifest.get('service_type') != service_type` → `RuleSyncProjectionError('规则同步服务类型不匹配。')` |
| H0804 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:592 抛出 | `not (service_type == 'mihomo') / not (service_type == 'oxidns')` → `RuleSyncProjectionError('规则自动同步仅支持 Mihomo 和 OxiDNS。')` |
| H0805 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:596 抛出 | `for path in paths / path in files` → `RuleSyncProjectionError('规则文件与非规则文件冲突，无法自动隔离。')` |
| H0806 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K40:599 抛出 | `for path in paths / value is None or value[1] != 'rule'` → `RuleSyncProjectionError('候选规则文件缺失或角色不符。')` |

### K41 [backend/agents/rule_sync_snapshot.py](../../backend/agents/rule_sync_snapshot.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0807 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:26 抛出 | `self.remaining < 0` → `ValueError('设备规则快照展开后超过大小限制')` |
| H0808 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:41 抛出 | `response.status_code != 200` → `ValueError('无法读取设备当前规则快照，请确认 Agent 已升级且没有其他操作正在进行')` |
| H0809 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:46 抛出 | `for chunk in response.iter_content(64 * 1024) / size > MAX_TOTAL_BYTES + 8 * 1024 * 1024` → `ValueError('设备规则快照超过大小限制')` |
| H0810 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:56 抛出 | `for member in archive / member.name in seen or not member.isfile() or len(seen) >= MAX_FILES + 1` → `ValueError('设备规则快照包含无效文件')` |
| H0811 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:60 抛出 | `member.name == 'manifest.json' / member.size > 2 * 1024 * 1024` → `ValueError('设备规则快照清单过大')` |
| H0812 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:66 抛出 | `for member in archive / member.name != 'files/' + path or path not in expected_files or (not 0 <= member.size <= MAX_FILE_BYTES)` → `ValueError('设备规则快照文件不属于上次发布')` |
| H0813 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:69 抛出 | `for member in archive / total > MAX_TOTAL_BYTES` → `ValueError('设备规则快照超过大小限制')` |
| H0814 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:72 抛出 | `not isinstance(manifest, dict) or set(files) != set(expected_files)` → `ValueError('设备规则快照文件不完整')` |
| H0815 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:75 抛出 | `for key in ('protocol_version', 'deployment_id', 'agent_id', 'profile_id', 'service_type', 'config_path') / manifest.get(key) != expected.get(key)` → `ValueError('设备配置已改变，请等待发布完成或重新建立同步基线')` |
| H0816 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:78 抛出 | `not isinstance(entries, list) or len(entries) != len(files)` → `ValueError('设备规则快照清单不完整')` |
| H0817 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:83 抛出 | `for entry in entries / path in checked or path not in files` → `ValueError('设备规则快照清单无效')` |
| H0818 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:88 抛出 | `for entry in entries / entry['role'] != role or type(entry['size']) is not int or entry['size'] != len(content) or (entry['sha256'] != hashlib.sha256(content).hexdigest())` → `ValueError('设备规则快照摘要不一致')` |
| H0819 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:93 抛出 | `confirmed_version not in (digest, digest[:8])` → `ValueError('设备主配置已改变，请先成功发布一次配置')` |
| H0820 / PUT /api/agents/<agent_id>/rule-sync → 后台tick | K41:98 抛出 | `捕获 (requests.RequestException, tarfile.TarError, OSError, KeyError, TypeError, json.JSONDecodeError, UnicodeError)` → `ValueError('无法验证设备当前规则快照，未继续自动应用')` |

### K42 [backend/agents/traffic_history.py](../../backend/agents/traffic_history.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0821 / R072 | K42:104 抛出 | `type(days) is not int or days not in RETENTION_CHOICES` → `ValueError('保留时长只能是 30、90、180 或 365 天')` |
| H0822 / R072 | K42:148 校验/降级 | `schema == 2 / not isinstance(layers, list) or len(layers) > CHAIN_MAX_LAYERS or any((not isinstance(name, str) or not name or len(name) > _MAX_TEXT['outbound'] or (CHAIN_SEPARATOR in name) f…（完整见源行）` → `None` |
| H0823 / R072 | K42:153 校验/降级 | `outbound is None or group is None` → `None` |
| H0824 / R072 | K42:166 校验/降级 | `not isinstance(report, dict) or report.get('schema') not in REPORT_SCHEMAS` → `None` |
| H0825 / R072 | K42:170 校验/降级 | `not isinstance(usage, list) or not isinstance(domains, list) or len(usage) + len(domains) > REPORT_MAX_ITEMS` → `None` |
| H0826 / R072 | K42:188 校验/降级 | `for item in usage / not isinstance(item, dict)` → `None` |
| H0827 / R072 | K42:192 校验/降级 | `for item in usage / None in fields` → `None` |
| H0828 / R072 | K42:201 校验/降级 | `for item in domains / not isinstance(item, dict)` → `None` |
| H0829 / R072 | K42:206 校验/降级 | `for item in domains / None in fields or fields[1] == UNATTRIBUTED` → `None` |

### K43 [backend/agents/traffic_store.py](../../backend/agents/traffic_store.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0830 / R074 | K43:88 校验/降级 | `not isinstance(value, str) or len(value) > max_length` → `False` |
| H0831 / R074 | K43:90 校验/降级 | `required and (not value)` → `False` |
| H0832 / R074 | K43:100 校验/降级 | `not isinstance(item, dict) or set(item) - _ITEM_FIELDS` → `False` |
| H0833 / R074 | K43:103 校验/降级 | `not isinstance(host, str) or host != host.lower() or is_valid_ip(host) or (not is_valid_domain(host))` → `False` |
| H0834 / R074 | K43:106 校验/降级 | `not isinstance(port, int) or isinstance(port, bool) or (not 0 <= port <= 65535)` → `False` |
| H0835 / R074 | K43:108 校验/降级 | `item.get('network', 'tcp') not in ('tcp', 'udp')` → `False` |
| H0836 / R074 | K43:110 校验/降级 | `item.get('outlet') not in OUTLETS` → `False` |
| H0837 / R074 | K43:115 校验/降级 | `not (_bounded_text(item.get('rule'), 64) and _bounded_text(item.get('rule_payload'), 256) and _bounded_text(item.get('policy'), 128, required=True))` → `False` |
| H0838 / R074 | K43:117 校验/降级 | `any((not _count(item.get(field, 0)) for field in _COUNT_FIELDS))` → `False` |
| H0839 / R074 | K43:120 校验/降级 | `not isinstance(fail_kinds, dict) or set(fail_kinds) - FAIL_KINDS` → `False` |
| H0840 / R074 | K43:131 校验/降级 | `not isinstance(payload, dict) or set(payload) - _REPORT_FIELDS` → `None` |
| H0841 / R074 | K43:133 校验/降级 | `payload.get('schema') != REPORT_SCHEMA or payload.get('status') not in REPORT_STATUSES` → `None` |
| H0842 / R074 | K43:136 校验/降级 | `for field in ('mihomo_version', 'window_start', 'window_end') / not _bounded_text(payload.get(field), 64)` → `None` |
| H0843 / R074 | K43:139 校验/降级 | `for field in ('ip_only_conns', 'dropped') / not _count(payload.get(field, 0))` → `None` |
| H0844 / R074 | K43:142 校验/降级 | `not isinstance(items, list) or len(items) > REPORT_MAX_ITEMS` → `None` |

### K44 [backend/agents/upgrade_messages.py](../../backend/agents/upgrade_messages.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0845 / POST /api/agents/<agent_id>/update → upgrade轮询 | K44:131 诊断 | `isinstance(message, str) and message.strip()` → `f'{explanation}\n原始错误：{message}'` |

### K45 [backend/agents/upgrades.py](../../backend/agents/upgrades.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0846 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:58 抛出 | `_artifact_signature(os.fstat(handle.fileno())) != expected` → `_ArtifactChanged` |
| H0847 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:71 抛出 | `捕获 FileNotFoundError` → `_ArtifactChanged` |
| H0848 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:73 抛出 | `expected != after_read or expected != after_path or size != expected[2]` → `_ArtifactChanged` |
| H0849 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:102 抛出 | `for attempt in range(2) / 捕获 _ArtifactChanged` → `OSError('Agent artifact changed while being read; please retry')` |
| H0850 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:137 抛出 | `state.get('update_id') != update_id` → `ValueError('Upgrade ownership changed')` |
| H0851 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:178 抛出 | `not agent` → `ValueError('Agent not found')` |
| H0852 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:180 抛出 | `agent.get('deployment_method') == 'docker'` → `ValueError('Docker Agent 请更新镜像并重建容器；网页二进制更新不适用于 Docker')` |
| H0853 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:189 抛出 | `捕获 (requests.RequestException, ValueError)` → `ValueError('无法连接 Agent 并确认更新能力')` |
| H0854 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:191 抛出 | `info and (info.get('agent_id') != agent_id or info.get('deployment_method') != 'shell')` → `ValueError('Agent 身份或安装方式与网页记录不符')` |
| H0855 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:193 抛出 | `not info and agent.get('deployment_method') != 'shell'` → `ValueError('旧 Agent 未报告 Shell 安装方式，无法安全选择更新流程')` |
| H0856 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:197 抛出 | `arch and arch not in available or (not info and set(available) != set(ARCHES))` → `ValueError('更新文件未准备就绪，请先更新 ConfigFlow 主服务镜像')` |
| H0857 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:209 抛出 | `active(current)` → `ValueError('DNS 主备正在同步，请等待两台发布完成后升级')` |
| H0858 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:212 抛出 | `previous.get('update_id') != (agent.get('latest_upgrade') or {}).get('update_id')` → `ValueError('当前更新任务已改变，请刷新后查看最新结果')` |
| H0859 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:214 抛出 | `previous and (previous.get('status') not in TERMINAL or previous.get('status') == 'rollback_failed')` → `ValueError('已有更新正在执行或等待恢复，请先查询结果')` |
| H0860 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:217 抛出 | `deployment and deployment.get('status') not in {'succeeded', 'failed', 'rolled_back'}` → `ValueError('存在未结束的配置发布，请先完成或恢复')` |
| H0861 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:247 抛出 | `not agent or not agent.get('latest_upgrade')` → `ValueError('No upgrade recorded')` |
| H0862 / POST /api/agents/<agent_id>/update → upgrade轮询 | K45:309 抛出 | `state.get('protocol') != 1 or payload.get('update_id') != state.get('update_id') or payload.get('status') != 'rolling_back'` → `ValueError('Invalid legacy upgrade report')` |

### K46 [backend/common/config_repository.py](../../backend/common/config_repository.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0863 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:969 抛出 | `for (field, label) in (('ros_policy_state', '策略路由'), ('dns_guard_state', '强制 DNS 规则'), ('ipv6_fakeip_state', 'IPv6 Fake-IP 路由'), ('ipv6_routes_state', 'IPv6 规则集路由'), ('ipv6_pd_guard_state', …（完整见源行）` → `ProfileInUse(f'路由器仍有已发布的{label}；请先发布并核对撤回，再修改类型、地址或删除发布记录', [{'kind': 'router', 'id': router_id, 'name': old.get('name', router_id), 'profile_id': profile_id, 'profile_name': previous['profiles'][profile_id]['name']}])` |
| H0864 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:987 抛出 | `for ((profile_id, router_id), router) in changed.items() / not jobs.enter_context(job_lock(profile_id, router_id, router))` → `ProfileInUse('路由器正在发布或同步，不能替换连接或删除记录，请稍后重试')` |
| H0865 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2418 抛出 | `document.get('schema_version') != self.SCHEMA_VERSION` → `ProfileValidationError('配置存储版本不匹配')` |
| H0866 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2421 抛出 | `for key in ('system', 'shared', 'profiles') / not isinstance(document.get(key), dict)` → `ProfileValidationError(f'缺少配置节：{key}')` |
| H0867 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2424 抛出 | `not isinstance(system.get('system_config'), dict)` → `ProfileValidationError('系统设置和备份设置必须是对象')` |
| H0868 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2427 抛出 | `default_id is not None and (not isinstance(default_id, str))` → `ProfileValidationError('默认配置空间 ID 必须是字符串')` |
| H0869 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2429 抛出 | `self._default_profile_id(document) not in document['profiles']` → `ProfileValidationError('必须保留默认配置')` |
| H0870 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2431 抛出 | `set(system) - self.SYSTEM_FIELDS` → `ProfileValidationError('system 节只接受全局设置、备份与 Agent')` |
| H0871 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2433 抛出 | `not isinstance(system.get('system_config'), dict) or not isinstance(system.get('backup'), dict)` → `ProfileValidationError('系统设置和备份设置必须是对象')` |
| H0872 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2448 抛出 | `捕获 ValueError` → `ProfileValidationError(str(error))` |
| H0873 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2453 抛出 | `捕获 DialerReferenceError` → `ProfileValidationError(str(error))` |
| H0874 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2456 抛出 | `for node in catalogs['nodes'].values() / 'dialer_ref' in node` → `ProfileValidationError('共享节点不能保存拨号引用，请在策略组中创建代理链')` |
| H0875 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2458 抛出 | `for node in catalogs['nodes'].values() / node.get('subscription_id') and node['subscription_id'] not in catalogs['subscriptions']` → `ProfileValidationError('节点引用的订阅不存在')` |
| H0876 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2461 抛出 | `for resource in catalogs['rule_library'].values() / 'policy' in resource or 'target' in resource` → `ProfileValidationError('规则仓库不包含目标策略，请在规则配置中设置')` |
| H0877 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2464 抛出 | `any((not isinstance(name, str) or not name for name in library_names)) or len(set(library_names)) != len(library_names)` → `ProfileValidationError('共享规则仓库名称必须非空且唯一')` |
| H0878 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2469 抛出 | `for value in _id_list(aggregation.get(field, []), f"聚合 {aggregation.get('name')} 的 {field}") / value not in catalogs[field] and (not (field == 'nodes' and value in BUILTIN_POLICIES))` → `ProfileValidationError(f"聚合 {aggregation.get('name')} 引用了不存在的资源：{value}")` |
| H0879 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2473 抛出 | `for (profile_id, profile) in document['profiles'].items() / not isinstance(profile, dict) or profile.get('id') != profile_id` → `ProfileValidationError('配置内容与 ID 不一致')` |
| H0880 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2481 抛出 | `for (profile_id, profile) in document['profiles'].items() / 捕获 DialerReferenceError` → `ProfileInUse(str(error))` |
| H0881 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2485 抛出 | `for (profile_id, profile) in document['profiles'].items() / 捕获 ProfileInUse` → `ProfileInUse(f"配置 {profile['name']}: {error}", usages)` |
| H0882 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2488 抛出 | `for agent in _items_by_id(system.get('agents'), 'Agent').values() / agent.get('profile_id') not in document['profiles']` → `ProfileValidationError(f"Agent {agent.get('name', agent['id'])} 绑定的配置不存在")` |
| H0883 / 关联公开路由（环境故障/不可达性待Opus判定） | K46:2491 抛出 | `for section in (system, shared, *document['profiles'].values()) / not isinstance(section.get('_revision'), int) or section['_revision'] < 0` → `ProfileValidationError('配置修订号无效')` |

### K47 [backend/common/device_notes.py](../../backend/common/device_notes.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0884 / R057 | K47:20 抛出 | `not isinstance(value, str) or len(value) > 80 or any((ord(c) < 32 or ord(c) == 127 for c in value))` → `OxidnsSettingsError('设备备注最多 80 个字符，不能包含换行或控制字符。')` |
| H0885 / R057 | K47:26 抛出 | `not isinstance(value, dict) or len(value) > MAX_NOTES` → `OxidnsSettingsError(f'设备备注必须为对象，最多 {MAX_NOTES} 条。')` |
| H0886 / R057 | K47:31 抛出 | `for (address, name) in value.items() / ip in result` → `OxidnsSettingsError(f'设备备注地址重复：{ip}')` |

### K48 [backend/common/gateway_onboarding.py](../../backend/common/gateway_onboarding.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0887 / R117 | K48:121 抛出 | `for tag in ('server_udp', 'server_tcp') / item and args(item).get('entry') != 'sequence_main'` → `OxidnsSettingsError('原生 DNS 监听入口使用自定义处理链，无法确认网关分流路径')` |
| H0888 / R117 | K48:128 抛出 | `proxy.get('type') != 'sequence' or not (fallback_enabled or (allow_disabled_fallback and fallback_disabled))` → `OxidnsSettingsError('原生 proxy_dns_seq 使用自定义处理链，无法确认 Mihomo 上游')` |
| H0889 / R117 | K48:136 抛出 | `fallback_enabled / fallback.get('type') != 'fallback' or args(fallback).get('primary') != 'remote_dns' or args(fallback).get('secondary') != 'fallback_dns'` → `OxidnsSettingsError('原生 fallback 使用自定义处理链，无法确认 Mihomo 代理与备用上游')` |
| H0890 / R117 | K48:140 抛出 | `fallback_enabled / secondary.get('type') != 'sequence' or secondary.get('args') != [{'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]` → `OxidnsSettingsError('原生 fallback_dns 使用自定义处理链，无法确认 Mihomo 上游')` |
| H0891 / R117 | K48:147 抛出 | `remote.get('type') != 'sequence' or not isinstance(steps, list) or steps[:2] != [{'exec': '$forward_remote'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}] or any((not isinstance(item, dic…（完整见源行）` → `OxidnsSettingsError('原生 remote_dns 使用自定义处理链，无法确认 Mihomo 上游')` |
| H0892 / R117 | K48:161 抛出 | `document.get('include')` → `OxidnsSettingsError('原生备用 DNS 含 include，无法安全准备网关上游')` |
| H0893 / R117 | K48:183 抛出 | `for tag in tags / not isinstance(item, dict) or item.get('tag') != tag or item.get('type') != kinds[tag] or (tag == 'fallback_dns' and args != secondary) or (tag != 'fallback_dns' and (not i…（完整见源行）` → `OxidnsSettingsError('原生备用 DNS 使用自定义插件，无法安全准备网关上游')` |
| H0894 / R117 | K48:206 抛出 | `dynamic_reference(document) or any((_references(remaining, tag) for tag in tags))` → `OxidnsSettingsError('原生备用 DNS 仍有自定义或动态引用，无法安全准备网关上游')` |
| H0895 / R117 | K48:220 抛出 | `not isinstance(value, str) or not value.strip() or len(value.encode('utf-8')) > 16384` → `GatewayOnboardingError('请填写直连 DNS，使用 IP 地址的 DoH / DoT，例如 https://223.5.5.5/dns-query')` |
| H0896 / R117 | K48:224 抛出 | `捕获 OxidnsSettingsError` → `GatewayOnboardingError(str(exc))` |
| H0897 / R117 | K48:226 抛出 | `len(upstreams) > 16` → `GatewayOnboardingError('直连 DNS 最多支持 16 个上游')` |
| H0898 / R117 | K48:236 抛出 | `for upstream in upstreams / parts.scheme not in ('https', 'tls') or address.version != 4 or address.is_loopback or address.is_unspecified or address.is_multicast or address.is_link_local or …（完整见源行）` → `ValueError` |
| H0899 / R117 | K48:238 抛出 | `for upstream in upstreams / parts.scheme == 'https' and (not parts.path)` → `ValueError` |
| H0900 / R117 | K48:240 抛出 | `for upstream in upstreams / 捕获 (ValueError, TypeError)` → `GatewayOnboardingError('直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口')` |
| H0901 / R117 | K48:246 抛出 | `not isinstance(value, dict) or set(value) - INPUT_FIELDS` → `GatewayOnboardingError('安装设置必须是 JSON 对象，且只能包含向导字段')` |
| H0902 / R117 | K48:253 抛出 | `for key in ('rule_set_ids', 'direct_rule_set_ids', 'proxy_rule_set_ids') / not isinstance(ids, list) or len(ids) > 1024 or any((not isinstance(item, str) or not item or len(item) > 256 or an…（完整见源行）` → `GatewayOnboardingError(f'{key} 必须为有效规则 ID 数组')` |
| H0903 / R117 | K48:256 抛出 | `set(result['direct_rule_set_ids']) & set(result['proxy_rule_set_ids'])` → `GatewayOnboardingError('同一域名规则集不能同时选择直连和代理')` |
| H0904 / R117 | K48:259 抛出 | `type(result.get('query_recorder', False)) is not bool` → `GatewayOnboardingError('query_recorder 必须为布尔值')` |
| H0905 / R117 | K48:261 抛出 | `type(result.get('confirm_template_conversion', False)) is not bool` → `GatewayOnboardingError('confirm_template_conversion 必须为布尔值')` |
| H0906 / R117 | K48:263 抛出 | `'expected_config_revision' in result and (type(result['expected_config_revision']) is not int or result['expected_config_revision'] < 0)` → `GatewayOnboardingError('配置版本无效，请刷新安装向导')` |
| H0907 / R117 | K48:265 抛出 | `result.get('default_forward', 'forward_remote') not in ('forward_remote', 'forward_local')` → `GatewayOnboardingError('请选择有效的未匹配域名 DNS')` |
| H0908 / R117 | K48:312 抛出 | `state == 'invalid'` → `GatewayOnboardingError(MESSAGES['ROUTER_SPLIT_INVALID'])` |
| H0909 / R117 | K48:316 抛出 | `捕获 LinuxGatewayError` → `GatewayOnboardingError(str(exc))` |
| H0910 / R117 | K48:441 抛出 | `捕获 (TypeError, ValueError)` → `GatewayOnboardingError('Linux 本机地址无效，请重新核对安装网络信息')` |
| H0911 / R117 | K48:444 抛出 | `not isinstance(source, str)` → `GatewayOnboardingError('Mihomo 基础配置必须为 YAML 文本')` |
| H0912 / R117 | K48:447 抛出 | `not isinstance(document, dict) or not isinstance(document.get('dns', {}), dict)` → `GatewayOnboardingError('Mihomo 基础配置及 dns 必须是 YAML 对象，请先修正基础配置')` |
| H0913 / R117 | K48:452 抛出 | `mihomo_dns_loop(dns, host_addresses)` → `GatewayOnboardingError('Mihomo DNS 上游不能指向本机 OxiDNS 或系统 DNS，否则会形成解析循环；请先修改基础配置')` |
| H0914 / R117 | K48:518 抛出 | `捕获 OxidnsSettingsError / not value.get('confirm_template_conversion')` → `GatewayOnboardingError('现有 OxiDNS 无法安全读取：' + str(exc) + '。请修正原生配置，或明确确认转换为模板。')` |
| H0915 / R117 | K48:531 抛出 | `for identity in value['direct_rule_set_ids'] + value['proxy_rule_set_ids'] / item is None or (not item['available'] and identity not in selected_before)` → `GatewayOnboardingError('所选域名规则集不可用：' + (item['reason'] if item else '不存在或不属于当前配置空间'))` |
| H0916 / R117 | K48:570 抛出 | `捕获 OxidnsSettingsError / not value.get('confirm_template_conversion')` → `GatewayOnboardingError('现有 OxiDNS 自定义结构无法安全更新：' + str(exc) + '。请在完整参数中调整，或明确确认转换为模板；不会自动覆盖原生配置。')` |
| H0917 / R117 | K48:592 抛出 | `not report['validation']['oxidns']['valid']` → `GatewayOnboardingError('OxiDNS 配置校验未通过，请根据 validation.oxidns.issues 修正后重试')` |
| H0918 / R117 | K48:597 抛出 | `not prerequisites['ready']` → `GatewayOnboardingError('当前配置没有被启用规则引用的可用代理出口；请先添加节点或订阅、配置策略组，并为需要代理的规则选择该策略')` |
| H0919 / R117 | K48:615 抛出 | `is_full_tun(config) / gateway_settings(config)['enabled']` → `GatewayOnboardingError('主路由分流不能同时启用 Linux 网关，请重新预览并保存')` |
| H0920 / R117 | K48:621 抛出 | `is_full_tun(config) / not is_full_tun_document(document)` → `GatewayOnboardingError('Mihomo 基础 YAML 缺少完整 TUN，请重新预览并保存')` |
| H0921 / R117 | K48:623 抛出 | `not (is_full_tun(config)) / not state['settings']['enabled']` → `GatewayOnboardingError('请先在安装向导预览并保存网关配置')` |
| H0922 / R117 | K48:629 抛出 | `any((type(values.get(key)) is not type(expected) or values.get(key) != expected for (key, expected) in required.items()))` → `GatewayOnboardingError('已保存的 OxiDNS 监听、IPv4 或默认分流不符合安装方案，请重新预览并保存')` |
| H0923 / R117 | K48:631 抛出 | `values.get('default_forward') not in ('forward_local', 'forward_remote')` → `GatewayOnboardingError('已保存的 OxiDNS 默认 DNS 无效，请重新预览并保存')` |
| H0924 / R117 | K48:637 抛出 | `for key in ('remote_dns', 'fallback_dns') / not upstreams or any((item.get('addr') not in (DNS_TARGET, 'udp://' + DNS_TARGET) or item.get('socks5') or item.get('outbound') for item in upstre…（完整见源行）` → `GatewayOnboardingError('OxiDNS 代理和备用上游必须均为 127.0.0.1:1053，请重新预览并保存')` |
| H0925 / R117 | K48:644 抛出 | `settings['config_mode'] == 'native' / any((not caps.get(field, {}).get('editable') for field in ('default_forward', 'direct_rulesets', 'proxy_rulesets')))` → `GatewayOnboardingError('OxiDNS 自定义分流结构无法确认，请回到安装向导重新预览')` |
| H0926 / R117 | K48:647 抛出 | `not validation['valid']` → `GatewayOnboardingError('已保存的 OxiDNS 配置校验未通过，请修正后重新准备安装')` |
| H0927 / R117 | K48:652 抛出 | `not document.get('dns', {}).get('nameserver')` → `GatewayOnboardingError('Mihomo 缺少 DNS 上游，请重新预览并保存网关配置')` |
| H0928 / R117 | K48:655 抛出 | `not prerequisites['ready']` → `GatewayOnboardingError('当前配置没有被启用规则引用的可用代理出口，请先完善节点和规则')` |
| H0929 / R117 | K48:660 抛出 | `捕获 (LinuxGatewayError, OxidnsSettingsError, ValueError, yaml.YAMLError)` → `GatewayOnboardingError(str(exc))` |

### K49 [backend/common/linux_gateway.py](../../backend/common/linux_gateway.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0930 / R105 | K49:38 抛出 | `not isinstance(value, dict)` → `LinuxGatewayError('Linux 网关设置必须是对象')` |
| H0931 / R105 | K49:41 抛出 | `type(enabled) is not bool` → `LinuxGatewayError('enabled 必须是布尔值')` |
| H0932 / R105 | K49:46 抛出 | `not isinstance(ids, list) or len(ids) > 1024 or any((not isinstance(item, str) or not item or len(item) > 256 or any((ord(c) < 32 or ord(c) == 127 for c in item)) for item in ids))` → `LinuxGatewayError('请选择有效的 IP 规则集，最多 1024 个')` |
| H0933 / R105 | K49:61 抛出 | `捕获 (yaml.YAMLError, RecursionError)` → `LinuxGatewayError('Mihomo 基础 YAML 无法解析，请先修正基础配置')` |
| H0934 / R105 | K49:63 抛出 | `not isinstance(document, dict)` → `LinuxGatewayError('Mihomo 基础配置必须是 YAML 对象')` |
| H0935 / R105 | K49:70 抛出 | `not isinstance(dns, dict)` → `LinuxGatewayError('Mihomo dns 必须是对象')` |
| H0936 / R105 | K49:77 抛出 | `network.version != 4 or not 8 <= network.prefixlen <= 30 or network.is_loopback or network.is_multicast or network.is_link_local or network.network_address.is_unspecified` → `ValueError` |
| H0937 / R105 | K49:79 抛出 | `捕获 (ValueError, TypeError)` → `LinuxGatewayError('fake-ip-range 必须是有效 IPv4 网段（/8 至 /30），不能是回环、多播或链路本地地址')` |
| H0938 / R105 | K49:116 抛出 | `not isinstance(content, str) or len(content.encode('utf-8')) > MAX_CONTENT_BYTES` → `LinuxGatewayError('IP 规则内容必须为 UTF-8 文本且不超过 16 MiB')` |
| H0939 / R105 | K49:118 抛出 | `'\x00' in content or '�' in content` → `LinuxGatewayError('IP 规则不是有效文本，暂不支持二进制规则')` |
| H0940 / R105 | K49:124 抛出 | `re.search('^\\s*payload\\s*:', stripped, re.M) or stripped.startswith(('[', '- ', '---')) / 捕获 (yaml.YAMLError, RecursionError)` → `LinuxGatewayError('IP 规则集 YAML 语法错误')` |
| H0941 / R105 | K49:127 抛出 | `re.search('^\\s*payload\\s*:', stripped, re.M) or stripped.startswith(('[', '- ', '---')) / not isinstance(values, list) or any((not isinstance(item, str) for item in values))` → `LinuxGatewayError('IP 规则集 YAML 必须包含字符串数组 payload')` |
| H0942 / R105 | K49:131 抛出 | `len(values) > MAX_RULES` → `LinuxGatewayError('IP 规则数量超过 250000 条限制')` |
| H0943 / R105 | K49:138 抛出 | `behavior not in ('ipcidr', 'classical')` → `LinuxGatewayError('网关仅支持 IP-CIDR / Classical 规则集')` |
| H0944 / R105 | K49:149 抛出 | `behavior == 'classical' / len(parts) not in (2, 3) or (len(parts) == 3 and parts[2].lower() != 'no-resolve')` → `LinuxGatewayError('Classical 目的 IP 规则只能包含 IP-CIDR,网段[,no-resolve]')` |
| H0945 / R105 | K49:153 抛出 | `not (behavior == 'classical') / len(parts) != 1` → `LinuxGatewayError('IP-CIDR 规则集中只能填写 IP / CIDR，不能包含其他匹配条件')` |
| H0946 / R105 | K49:158 抛出 | `for line in _lines(content) / 捕获 ValueError` → `LinuxGatewayError('IP 规则包含无效 IP / CIDR')` |
| H0947 / R105 | K49:160 抛出 | `for line in _lines(content) / behavior == 'classical' and (parts[0].upper() == 'IP-CIDR6') != (network.version == 6)` → `LinuxGatewayError('IP-CIDR / IP-CIDR6 规则类型必须与目标 IP 版本一致')` |
| H0948 / R105 | K49:164 抛出 | `for line in _lines(content) / network.prefixlen < 8` → `LinuxGatewayError('Linux 网关只接受 /8 至 /32 的 IPv4 网段，请拆分或移除 /0、/1 等超大接管范围')` |
| H0949 / R105 | K49:168 抛出 | `any((network.prefixlen == 0 for network in collapsed))` → `LinuxGatewayError('所选 IP 规则覆盖全部 IPv4 地址，无法用于 Linux 网关')` |
| H0950 / R105 | K49:193 抛出 | `not content / not cached.is_file()` → `LinuxGatewayError('无法读取 IP 规则集，请先在规则库刷新并检查下载状态')` |
| H0951 / R105 | K49:384 抛出 | `conflict` → `LinuxGatewayError(f'Linux 网关与 {conflict} 冲突，请先移除 routing-mark')` |
| H0952 / R105 | K49:388 抛出 | `not isinstance(tun, dict)` → `LinuxGatewayError('Mihomo tun 必须是对象')` |
| H0953 / R105 | K49:391 抛出 | `for key in TUN_FILTER_KEYS / tun.get(key)` → `LinuxGatewayError(f'Linux 网关与现有 tun.{key} 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管')` |
| H0954 / R105 | K49:405 抛出 | `not isinstance(providers, dict)` → `LinuxGatewayError('Mihomo rule-providers 必须是对象')` |
| H0955 / R105 | K49:407 抛出 | `any((name.startswith(PROVIDER_PREFIX) for name in providers))` → `LinuxGatewayError('cf_gateway_ 是网关内部规则集前缀，请修改同名前缀的自定义规则集')` |
| H0956 / R105 | K49:415 抛出 | `settings['rule_set_ids'] and is_auth_enabled() and (not token)` → `LinuxGatewayError('已开启登录验证，请先在系统设置中配置订阅令牌（config_token），供 Mihomo 自动刷新网关规则')` |
| H0957 / R105 | K49:494 抛出 | `item is None / rule_id not in previous` → `LinuxGatewayError('所选规则集不存在或不属于当前配置空间')` |
| H0958 / R105 | K49:501 抛出 | `for rule_id in ids / behavior not in ('ipcidr', 'classical')` → `LinuxGatewayError('只能选择 IP-CIDR / Classical 规则集')` |
| H0959 / R105 | K49:505 抛出 | `for rule_id in ids / 'MRS' in _reason(config, item) and rule_id not in previous` → `LinuxGatewayError(_reason(config, item))` |

### K50 [backend/common/linux_gateway_draft.py](../../backend/common/linux_gateway_draft.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0960 / R105 | K50:32 抛出 | `type(expected) is not int or expected < 0` → `LinuxGatewayError('缺少有效配置版本，请重新打开 Linux 网关设置')` |
| H0961 / R105 | K50:34 抛出 | `expected != revision` → `ProfileInUse('配置或共享资源已更新，请重新打开 Linux 网关设置并预览')` |
| H0962 / R105 | K50:62 抛出 | `not point` → `LinuxGatewayError('没有可用的试用前草稿快照')` |
| H0963 / R105 | K50:74 抛出 | `RESTORE_KEY in config.get('mihomo', {}) / not restore_point(config)` → `LinuxGatewayError('原有网关恢复快照无效，请先检查配置备份')` |
| H0964 / R105 | K50:136 抛出 | `捕获 (ValueError, yaml.YAMLError, TypeError)` → `LinuxGatewayError(f'无法生成目标 Mihomo 配置：{exc}')` |
| H0965 / R105 | K50:165 抛出 | `not isinstance(token, str) or not re.fullmatch('[0-9a-f]{64}', token) or (not secrets.compare_digest(token, preview['preview_token']))` → `ProfileInUse('配置预览已变化或未确认，请重新预览后保存')` |

### K51 [backend/common/oxidns_device_policy.py](../../backend/common/oxidns_device_policy.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0966 / R058 | K51:25 抛出 | `not isinstance(raw, str)` → `ValueError()` |
| H0967 / R058 | K51:28 抛出 | `len(parts) > 2 or not re.fullmatch('[0-9a-fA-F:.]+', parts[0])` → `ValueError()` |
| H0968 / R058 | K51:33 抛出 | `len(parts) == 2 / not re.fullmatch('[0-9]{1,3}', parts[1])` → `ValueError()` |
| H0969 / R058 | K51:36 抛出 | `not 0 <= prefix <= address.max_prefixlen` → `ValueError()` |
| H0970 / R058 | K51:40 抛出 | `isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped / prefix < 96` → `ValueError()` |
| H0971 / R058 | K51:44 抛出 | `network.is_multicast` → `ValueError()` |
| H0972 / R058 | K51:47 抛出 | `prefix == address.max_prefixlen / address.is_unspecified` → `ValueError()` |
| H0973 / R058 | K51:51 抛出 | `捕获 ValueError` → `OxidnsSettingsError('请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。')` |
| H0974 / R058 | K51:56 抛出 | `not isinstance(value, list) or len(value) > 512` → `OxidnsSettingsError('设备策略必须为列表，最多 512 条策略。')` |
| H0975 / R058 | K51:60 抛出 | `for row in value / not isinstance(row, dict) or set(row) - {'ip', 'name', 'mode'}` → `OxidnsSettingsError('设备策略仅支持 IP / 网段、备注和策略。')` |
| H0976 / R058 | K51:63 抛出 | `for row in value / ip in seen` → `OxidnsSettingsError(f'客户端 IP / 网段重复：{ip}')` |
| H0977 / R058 | K51:67 抛出 | `for row in value / not isinstance(name, str) or len(name) > 80 or any((ord(c) < 32 for c in name))` → `OxidnsSettingsError('设备备注最多 80 个字符，不能包含换行或控制字符。')` |
| H0978 / R058 | K51:69 抛出 | `for row in value / mode not in ('smart', 'direct')` → `OxidnsSettingsError('设备策略必须为智能分流或直连。')` |
| H0979 / R058 | K51:103 抛出 | `外部依赖/文件/事务异常` → `OxidnsSettingsError(f'{reason}，无法安全插入设备策略；{fix}。')` |
| H0980 / R058 | K51:329 抛出 | `old / owned != {POLICY_TAG: old, DIRECT_TAG: DIRECT} or len(hooks) != 1` → `OxidnsSettingsError('设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。')` |
| H0981 / R058 | K51:333 抛出 | `old / index != _prefix_end(args)` → `OxidnsSettingsError('设备策略的位置已被修改，请先恢复到分流规则之前。')` |
| H0982 / R058 | K51:335 抛出 | `not (old) / owned or hooks` → `OxidnsSettingsError('完整参数中存在同名设备策略插件，请先处理命名冲突。')` |
| H0983 / R058 | K51:340 抛出 | `POLICY_TAG in text or DIRECT_TAG in text` → `OxidnsSettingsError('设备策略插件存在额外引用，请先在完整参数中处理。')` |

### K52 [backend/routes/agents.py](../../backend/routes/agents.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H0984 / 所属路由表的同名handler | K52:64 校验/降级 | `isinstance(value, bool) or not isinstance(value, (str, int, float))` → `None` |
| H0985 / 所属路由表的同名handler | K52:87 校验/降级 | `isinstance(value, bool) or not isinstance(value, (int, float)) or (not math.isfinite(value))` → `False` |
| H0986 / 所属路由表的同名handler | K52:91 校验/降级 | `not isinstance(value, int) or value < 0` → `False` |
| H0987 / 所属路由表的同名handler | K52:97 校验/降级 | `not isinstance(metrics, dict) or set(metrics) - (set(_METRIC_FIELDS) &#124; {'collected_at'})` → `False` |
| H0988 / 所属路由表的同名handler | K52:99 校验/降级 | `len(json.dumps(metrics, ensure_ascii=False).encode('utf-8')) > 16 * 1024` → `False` |
| H0989 / 所属路由表的同名handler | K52:102 校验/降级 | `collected_at is not None and (not isinstance(collected_at, str) or len(collected_at) > 64)` → `False` |
| H0990 / 所属路由表的同名handler | K52:108 校验/降级 | `for (section, allowed_fields) in _METRIC_FIELDS.items() / not isinstance(values, dict) or set(values) - allowed_fields` → `False` |
| H0991 / 所属路由表的同名handler | K52:110 校验/降级 | `for (section, allowed_fields) in _METRIC_FIELDS.items() / any((not _valid_metric_number(field, value) for (field, value) in values.items()))` → `False` |
| H0992 / 所属路由表的同名handler | K52:116 校验/降级 | `not isinstance(payload, dict) or set(payload) - _HEARTBEAT_FIELDS` → `False` |
| H0993 / 所属路由表的同名handler | K52:121 校验/降级 | `field in payload / not isinstance(value, str) or len(value) > max_length` → `False` |
| H0994 / R017 | K52:177 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': failure_text(str(error), secrets)}), 409)` |
| H0995 / R020 | K52:189 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': failure_text(str(error), secrets)}), 409)` |
| H0996 / R048 | K52:213 错误响应 | `捕获 ValueError` → `(jsonify(body), 409)` |
| H0997 / R082 | K52:254 错误结果 | `捕获 (OSError, ValueError)` → `(jsonify({'success': False, 'message': '核心安装脚本不可用，请更新 ConfigFlow 后重试。'}), 500)` |
| H0998 / R097 | K52:272 错误结果 | `捕获 OSError` → `(jsonify({'success': False, 'message': '网络检测脚本暂时不可用，请更新 ConfigFlow 后重试。'}), 503)` |
| H0999 / 所属路由表的同名handler | K52:304 抛出 | `agent_type == 'shell'` → `ValueError(LEGACY_SHELL_AGENT_REMOVED)` |
| H1000 / 所属路由表的同名handler | K52:306 抛出 | `agent_type != 'go'` → `ValueError('agent_type 只支持 go')` |
| H1001 / 所属路由表的同名handler | K52:311 抛出 | `not isinstance(payload, dict) or set(payload) - _INSTALL_COMMAND_FIELDS` → `ValueError('安装参数无效，请重新生成命令。')` |
| H1002 / 所属路由表的同名handler | K52:316 抛出 | `key in _INSTALL_COMMAND_BOOLEANS / not isinstance(value, bool)` → `ValueError(f'{key} 必须为 true 或 false')` |
| H1003 / 所属路由表的同名handler | K52:320 抛出 | `key in _INSTALL_COMMAND_NUMBERS / not isinstance(value, int) or isinstance(value, bool) or (not 1 <= value <= 65535)` → `ValueError(f'{key} 必须是 1 到 65535 之间的整数')` |
| H1004 / 所属路由表的同名handler | K52:323 抛出 | `not (key in _INSTALL_COMMAND_NUMBERS) / not isinstance(value, str) or len(value) > 4096 or '\x00' in value` → `ValueError(f'{key} 参数无效')` |
| H1005 / 所属路由表的同名handler | K52:334 错误结果 | `捕获 InstallCommandStoreFull` → `(jsonify({'success': False, 'message': '可用安装链接已达上限，请使用已生成的命令或等待旧链接过期。'}), 503)` |
| H1006 / 所属路由表的同名handler | K52:337 错误结果 | `捕获 (OSError, ValueError, sqlite3.Error)` → `(jsonify({'success': False, 'message': '无法保存安装命令，请检查数据目录后重试。'}), 500)` |
| H1007 / R099 | K52:356 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1008 / R081 | K52:371 错误结果 | `not isinstance(payload, dict) or set(payload) != {'target'} or payload['target'] not in ('both', 'mihomo', 'oxidns')` → `(jsonify({'success': False, 'message': '请选择 both、mihomo 或 oxidns 核心。'}), 400)` |
| H1009 / R098 | K52:394 错误响应 | `捕获 (OSError, sqlite3.Error)` → `('安装脚本暂时不可用，请稍后重试。\n', 503, headers)` |
| H1010 / R098 | K52:396 错误响应 | `script is None` → `('安装链接不存在或已过期，请在面板重新生成。\n', 404, headers)` |
| H1011 / 所属路由表的同名handler | K52:423 抛出 | `install_core_value not in ('true', 'false')` → `ValueError('install_core 必须为 true 或 false')` |
| H1012 / 所属路由表的同名handler | K52:427 抛出 | `prepare_ha_value not in (None, 'true', 'false')` → `ValueError('prepare_dns_ha 必须为 true 或 false')` |
| H1013 / 所属路由表的同名handler | K52:429 抛出 | `prepare_ha_value is not None and (service_type != 'oxidns' or agent_type != 'go')` → `ValueError('仅 OxiDNS 单服务 Go Agent 支持准备 DNS 高可用环境')` |
| H1014 / 所属路由表的同名handler | K52:433 抛出 | `install_core / service_type not in ('mihomo', 'oxidns', 'aio') or agent_type != 'go'` → `ValueError('仅 Mihomo、OxiDNS 或同机双服务 Go Agent 支持同时安装核心')` |
| H1015 / 所属路由表的同名handler | K52:441 抛出 | `forwarding_value not in (None, 'true', 'false')` → `ValueError('ipv4_forwarding 必须为 true 或 false')` |
| H1016 / 所属路由表的同名handler | K52:443 抛出 | `forwarding_value is not None and (service_type != 'mihomo' or agent_type != 'go')` → `ValueError('仅 Mihomo 单独安装（Go Agent）可设置 IPv4 转发')` |
| H1017 / 所属路由表的同名handler | K52:476 抛出 | `service_type == 'aio' / agent_type != 'go'` → `ValueError('Mihomo + OxiDNS 同机安装需要 Go Agent')` |
| H1018 / 所属路由表的同名handler | K52:517 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1019 / 所属路由表的同名handler | K52:527 错误结果 | `捕获 FileNotFoundError` → `(jsonify({'success': False, 'message': error_msg}), 500)` |
| H1020 / 所属路由表的同名handler | K52:535 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1021 / R102 | K52:558 错误结果 | `agent_data is None` → `(jsonify({'success': False, 'message': 'Invalid JSON data'}), 400)` |
| H1022 / R102 | K52:580 错误结果 | `registration_key and installation_profile is None / provided_key != registration_key` → `(jsonify({'success': False, 'message': 'Invalid registration key'}), 403)` |
| H1023 / R102 | K52:637 错误响应 | `捕获 AgentDeploymentConflict` → `(jsonify(success=False, message=str(exc)), 409)` |
| H1024 / R102 | K52:643 错误结果 | `捕获 PermissionError` → `(jsonify({'success': False, 'message': 'Unauthorized'}), 401)` |
| H1025 / R102 | K52:650 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1026 / 所属路由表的同名handler | K52:671 错误结果 | `request.content_length is not None and request.content_length > max_length` → `(None, (jsonify({'success': False, 'message': 'Request body too large'}), 413))` |
| H1027 / 所属路由表的同名handler | K52:674 错误结果 | `len(raw_body) > max_length` → `(None, (jsonify({'success': False, 'message': 'Request body too large'}), 413))` |
| H1028 / 所属路由表的同名handler | K52:676 错误结果 | `not request.is_json` → `(None, (jsonify({'success': False, 'message': invalid_message}), 400))` |
| H1029 / 所属路由表的同名handler | K52:680 错误结果 | `捕获 (json.JSONDecodeError, UnicodeDecodeError)` → `(None, (jsonify({'success': False, 'message': invalid_message}), 400))` |
| H1030 / R026 | K52:698 错误结果 | `agent is None` → `(jsonify({'success': False, 'message': 'Unauthorized'}), 401)` |
| H1031 / R026 | K52:704 错误结果 | `not _validate_heartbeat_payload(heartbeat_data)` → `(jsonify({'success': False, 'message': 'Invalid heartbeat data'}), 400)` |
| H1032 / R026 | K52:723 错误结果 | `外部依赖/文件/事务异常` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1033 / R026 | K52:725 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1034 / R074 | K52:734 错误结果 | `agent is None` → `(jsonify({'success': False, 'message': 'Unauthorized'}), 401)` |
| H1035 / R074 | K52:739 错误结果 | `not _domain_discovery_enabled(agent)` → `(jsonify({'success': False, 'enabled': False, 'message': 'Domain discovery disabled'}), 409)` |
| H1036 / R074 | K52:742 错误结果 | `report is None` → `(jsonify({'success': False, 'message': 'Invalid traffic report'}), 400)` |
| H1037 / R021 | K52:757 错误结果 | `not isinstance(payload, dict) or not isinstance(payload.get('enabled'), bool)` → `(jsonify({'success': False, 'message': 'enabled 必须是布尔值'}), 400)` |
| H1038 / R021 | K52:761 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1039 / R021 | K52:763 错误结果 | `捕获 AgentDeploymentConflict` → `(jsonify({'success': False, 'message': str(exc)}), 409)` |
| H1040 / R021 | K52:765 错误结果 | `agent is None` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1041 / R022 | K52:774 错误结果 | `not get_agent_manager().get_agent_by_id(agent_id, history=False)` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1042 / R072 | K52:787 错误结果 | `agent is None` → `(jsonify({'success': False, 'message': 'Unauthorized'}), 401)` |
| H1043 / R072 | K52:792 错误结果 | `not _traffic_history_enabled(agent)` → `(jsonify({'success': False, 'enabled': False, 'message': 'Traffic history disabled'}), 409)` |
| H1044 / R072 | K52:795 错误结果 | `cleaned is None` → `(jsonify({'success': False, 'message': 'Invalid traffic history report'}), 400)` |
| H1045 / 所属路由表的同名handler | K52:816 错误结果 | `not agent` → `(None, (jsonify({'success': False, 'message': 'Agent not found'}), 404))` |
| H1046 / 所属路由表的同名handler | K52:818 错误结果 | `agent.get('service_type', 'mihomo') != 'mihomo'` → `(None, (jsonify({'success': False, 'message': '流量记录只支持 mihomo Agent'}), 400))` |
| H1047 / R071 | K52:831 错误结果 | `range_key not in traffic_history.RANGES or by not in traffic_history.AGGREGATES` → `(jsonify({'success': False, 'message': 'range 或 by 参数无效'}), 400)` |
| H1048 / R073 | K52:844 错误结果 | `not isinstance(payload, dict) or not isinstance(payload.get('enabled'), bool) or (not isinstance(payload.get('clear', False), bool))` → `(jsonify({'success': False, 'message': 'enabled 必须是布尔值'}), 400)` |
| H1049 / R073 | K52:852 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1050 / R073 | K52:854 错误结果 | `捕获 AgentDeploymentConflict` → `(jsonify({'success': False, 'message': str(exc)}), 409)` |
| H1051 / R073 | K52:856 错误结果 | `agent is None` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1052 / R103 | K52:881 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1053 / R010 | K52:892 错误结果 | `not is_token_within_length(token)` → `(jsonify({'success': False, 'message': 'Token required'}), 401)` |
| H1054 / R010 | K52:898 错误结果 | `not agent or agent['id'] != agent_id` → `(jsonify({'success': False, 'message': 'Invalid token'}), 401)` |
| H1055 / R010 | K52:914 错误结果 | `捕获 StrategyReferenceError` → `(jsonify({'success': False, 'message': str(e)}), 400)` |
| H1056 / R010 | K52:916 错误结果 | `捕获 ProfileRepositoryError` → `(jsonify({'success': False, 'message': f'Agent profile unavailable: {e}'}), 409)` |
| H1057 / R010 | K52:918 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1058 / R087 | K52:939 错误结果 | `filename not in allowed_files` → `(jsonify({'success': False, 'message': 'Invalid filename'}), 404)` |
| H1059 / R087 | K52:944 错误结果 | `not os.path.exists(filepath)` → `(jsonify({'success': False, 'message': 'File not found'}), 404)` |
| H1060 / R087 | K52:950 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1061 / R001 | K52:987 错误结果 | `request.method == 'POST' / 捕获 AgentDeploymentConflict` → `(jsonify({'success': False, 'message': str(exc)}), 409)` |
| H1062 / R001 | K52:989 错误结果 | `request.method == 'POST' / 捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1063 / R003 | K52:1003 错误结果 | `request.method == 'GET' / not (agent)` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1064 / R003 | K52:1012 错误结果 | `request.method == 'PUT' / not (result)` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1065 / R003 | K52:1014 错误结果 | `request.method == 'PUT' / 捕获 AgentDeploymentConflict` → `(jsonify({'success': False, 'message': str(exc)}), 409)` |
| H1066 / R003 | K52:1016 错误结果 | `request.method == 'PUT' / 捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1067 / R003 | K52:1027 错误结果 | `request.method == 'DELETE' / not (result)` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1068 / R003 | K52:1029 错误结果 | `request.method == 'DELETE' / 捕获 AgentDeploymentConflict` → `(jsonify({'success': False, 'message': str(exc)}), 409)` |
| H1069 / R003 | K52:1031 错误结果 | `request.method == 'DELETE' / 捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1070 / R006 | K52:1042 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1071 / R066 | K52:1059 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1072 / R069 | K52:1072 错误结果 | `not (agent)` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1073 / R034 | K52:1090 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1074 / R036 | K52:1102 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1075 / R035 | K52:1115 错误结果 | `not log_path and (not source)` → `(jsonify({'success': False, 'message': '日志路径不能为空'}), 400)` |
| H1076 / R035 | K52:1121 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1077 / R037 | K52:1133 错误结果 | `not log_path` → `(jsonify({'success': False, 'message': '日志路径不能为空'}), 400)` |
| H1078 / R037 | K52:1139 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1079 / R011 | K52:1151 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1080 / R012 | K52:1167 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1081 / R077 | K52:1182 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1082 / R078 | K52:1194 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': upgrade_error_message(str(exc))}), 409)` |
| H1083 / R079 | K52:1211 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': upgrade_error_message(str(exc))}), 404)` |
| H1084 / R080 | K52:1222 错误结果 | `not agent or not provided or (not _constant_time_ascii_equal(provided, agent.get('token', '')))` → `(jsonify({'success': False}), 401)` |
| H1085 / R080 | K52:1224 错误结果 | `request.content_length is None or request.content_length > 4096` → `(jsonify({'success': False}), 413)` |
| H1086 / R080 | K52:1228 错误结果 | `捕获 (ValueError, TypeError, AttributeError)` → `(jsonify({'success': False, 'message': 'Agent 上报的更新结果格式无效。'}), 400)` |
| H1087 / R051 | K52:1240 错误结果 | `not agent or agent.get('service_type') != 'oxidns'` → `(jsonify({'success': False, 'message': 'OxiDNS Agent not found'}), 404)` |
| H1088 / R051 | K52:1244 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': '无法连接 Agent，请先检查容器日志和网络'}), 502)` |
| H1089 / R051 | K52:1246 错误结果 | `not isinstance(capability, dict) or capability.get('success') is False` → `(jsonify({'success': False, 'message': 'Agent 能力检查失败，请确认 Agent 已上线'}), 502)` |
| H1090 / R083 | K52:1300 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(e)}), 400)` |
| H1091 / R083 | K52:1302 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1092 / R084 | K52:1335 错误结果 | `捕获 ValueError` → `(jsonify({'success': False, 'message': str(e)}), 400)` |
| H1093 / R084 | K52:1337 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1094 / 所属路由表的同名handler | K52:1407 抛出 | `外部依赖/文件/事务异常` → `ConnectionError('本次发布已经尝试过下载这个订阅，不再重复下载')` |
| H1095 / 所属路由表的同名handler | K52:1438 抛出 | `service_type == 'mihomo' and read_marker(config_data)[0] != 'absent' / agent.get('deployment_method') != 'docker' and (not supports_forwarding(agent))` → `ValueError(f'主路由分流（完整 TUN）需要 Mihomo Agent {FORWARDING_MIN_AGENT_VERSION} 或更新版本；旧版本在移除旧网关记录时可能关闭 IPv4 转发。请先在 Agent 页更新。')` |
| H1096 / 所属路由表的同名handler | K52:1460 抛出 | `service_type == 'mihomo' / 捕获 yaml.YAMLError` → `_generated_config_failure(exc, content, 'Mihomo')` |
| H1097 / 所属路由表的同名handler | K52:1531 抛出 | `service_type == 'oxidns' / 捕获 (yaml.YAMLError, OxidnsValidationError)` → `_generated_config_failure(exc, config_content, 'OxiDNS')` |
| H1098 / 所属路由表的同名handler | K52:1535 抛出 | `not (service_type == 'mosdns') / not (service_type == 'oxidns')` → `ValueError('Unsupported service type')` |
| H1099 / R023 | K52:1563 错误响应 | `not agent` → `(jsonify(success=False, message='找不到这台 Agent，请刷新设备列表。'), 404)` |
| H1100 / R023 | K52:1568 错误响应 | `not isinstance(data, dict) or data.get('expected_profile_id') != profile_id or data.get('expected_service_type') != agent.get('service_type')` → `(jsonify(success=False, message='目标设备或绑定已改变，请刷新后重新生成。'), 409)` |
| H1101 / R023 | K52:1586 抛出 | `peer['id'] != agent_id / other.archive != prepared['bundle'].archive` → `ValueError('主备设备需要不同的最终配置，请先统一主备配置路径与设备设置。')` |
| H1102 / R023 | K52:1594 错误响应 | `捕获 previews.PreviewChanged` → `(jsonify(success=False, code='preview_changed', message=str(exc)), 409)` |
| H1103 / R023 | K52:1596 错误响应 | `捕获 Exception` → `(jsonify(success=False, message=failure_text(str(exc), (agent.get('token'),))), 400)` |
| H1104 / R024 | K52:1610 错误响应 | `捕获 previews.PreviewChanged` → `(jsonify(success=False, code='preview_changed', message=str(exc)), 409)` |
| H1105 / R025 | K52:1628 错误响应 | `捕获 KeyError` → `(jsonify(success=False, message='生成结果中没有这个文件。'), 404)` |
| H1106 / R025 | K52:1630 错误响应 | `捕获 previews.PreviewChanged` → `(jsonify(success=False, code='preview_changed', message=str(exc)), 409)` |
| H1107 / R064 | K52:1646 错误响应 | `not agent` → `(jsonify(success=False, message='找不到这台 Agent，请刷新设备列表。'), 404)` |
| H1108 / R065 | K52:1688 抛出 | `not (raw.isascii() and raw.isdigit()) or not low <= int(raw) <= high` → `ValueError(name)` |
| H1109 / R065 | K52:1697 抛出 | `not version or len(version) > 64 or (not path) or (len(path) > 1024) or (len(query) > MAX_QUERY_CHARS)` → `ValueError('arguments')` |
| H1110 / R065 | K52:1699 抛出 | `change not in ('added', 'removed', 'modified')` → `ValueError('change')` |
| H1111 / R065 | K52:1701 错误响应 | `捕获 ValueError` → `(jsonify(success=False, message='分页参数无效，请重新加载。'), 400)` |
| H1112 / R065 | K52:1704 错误响应 | `not agent` → `(jsonify(success=False, message='找不到这台 Agent，请刷新设备列表。'), 404)` |
| H1113 / R065 | K52:1719 错误响应 | `捕获 KeyError` → `(jsonify(success=False, message='这个文件没有可加载的明细，请重新加载。'), 404)` |
| H1114 / R067 | K52:1736 错误响应 | `request.method == 'PUT' / request.args or (request.content_length and request.content_length > 1024)` → `(jsonify(success=False, message='规则同步设置无效'), 400)` |
| H1115 / R067 | K52:1744 错误响应 | `捕获 KeyError` → `(jsonify(success=False, message='未找到这台 Agent'), 404)` |
| H1116 / R067 | K52:1746 错误响应 | `捕获 ValueError` → `(jsonify(success=False, message=str(error)), 400)` |
| H1117 / R063 | K52:1767 错误结果 | `not agent` → `(jsonify({'success': False, 'message': '找不到这台 Agent，请刷新设备列表。'}), 404)` |
| H1118 / R063 | K52:1770 错误结果 | `not isinstance(data, dict)` → `(jsonify({'success': False, 'message': '发布请求格式无效，请重新提交。'}), 400)` |
| H1119 / R063 | K52:1774 抛出 | `'generation_id' in data / data.get('deployment_id') not in (None, generated['deployment_id'])` → `previews.PreviewChanged('发布任务与生成结果不匹配，请重新生成。')` |
| H1120 / R063 | K52:1798 错误响应 | `legacy_revision_guard and (type(data['expected_config_revision']) is not int or data['expected_config_revision'] < 0)` → `(jsonify(success=False, message='配置已改变，请重新预览后发布。'), 409)` |
| H1121 / R063 | K52:1803 错误响应 | `guarded and (not isinstance(data.get('expected_profile_id'), str) or not isinstance(data.get('expected_service_type'), str) or type(data.get('expected_config_revision')) is not int or (data[…（完整见源行）` → `(jsonify(success=False, message='保存后的发布信息不完整，请重新保存草稿后发布。'), 400)` |
| H1122 / R063 | K52:1811 错误结果 | `捕获 ProfileRepositoryError` → `(jsonify({'success': False, 'message': f'Agent 绑定的配置空间不可用。原始错误：{exc}'}), 409)` |
| H1123 / R063 | K52:1813 错误响应 | `legacy_revision_guard and data['expected_config_revision'] != config_revision` → `(jsonify(success=False, message='配置已改变，请重新预览后发布。'), 409)` |
| H1124 / R063 | K52:1818 错误响应 | `guarded and (data['expected_profile_id'] != profile_id or data['expected_service_type'] != service_type or data['expected_config_revision'] != config_revision)` → `(jsonify(success=False, message='草稿已保存，但配置或设备绑定已改变，请重新载入并保存后发布。'), 409)` |
| H1125 / R063 | K52:1824 错误响应 | `'expected_oxidns_revision' in data or ('expected_profile_id' in data and (not guarded)) / service_type != 'oxidns' or data.get('expected_profile_id') != profile_id or data.get('expected_oxid…（完整见源行）` → `(jsonify(success=False, code='draft_changed', message='配置或设备绑定已改变，请重新载入后发布。'), 409)` |
| H1126 / R063 | K52:1841 错误结果 | `service_type not in ('mihomo', 'mosdns', 'oxidns')` → `(jsonify({'success': False, 'message': '当前服务类型不支持配置发布。'}), 400)` |
| H1127 / R063 | K52:1844 错误结果 | `not isinstance(activate, bool)` → `(jsonify({'success': False, 'message': '发布请求中的重启选项无效，请重新提交。'}), 400)` |
| H1128 / R063 | K52:1858 错误响应 | `guarded and isinstance(reservation, dict) and (reservation.get('profile_id') != profile_id or reservation.get('service_type') != service_type)` → `(jsonify(_public_deployment(result)), 409)` |
| H1129 / R063 | K52:1938 错误响应 | `捕获 previews.PreviewChanged` → `(jsonify(result), 409)` |
| H1130 / R063 | K52:1950 错误响应 | `捕获 (StrategyReferenceError, DeploymentPreparationError, ValueError)` → `(jsonify(_public_deployment(result)), 400)` |
| H1131 / R063 | K52:1960 错误响应 | `捕获 Exception` → `(jsonify(_public_deployment(result)), 500)` |
| H1132 / R038 | K52:2017 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1133 / R038 | K52:2032 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1134 / R039 | K52:2045 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1135 / R039 | K52:2065 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1136 / R040 | K52:2078 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1137 / R040 | K52:2097 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1138 / R075 | K52:2110 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1139 / R075 | K52:2129 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1140 / R076 | K52:2142 错误结果 | `not agent` → `(jsonify({'success': False, 'message': 'Agent not found'}), 404)` |
| H1141 / R076 | K52:2168 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1142 / R101 | K52:2182 错误结果 | `捕获 Exception` → `(jsonify({'success': False, 'message': str(e)}), 500)` |
| H1143 / R061 | K52:2232 抛出 | `request.args or (request.content_length or 0) > 1024` → `oxidns_rule_update._fail('INVALID_REQUEST', 400)` |
| H1144 / R061 | K52:2235 抛出 | `len(raw) > 1024 or (request.method == 'GET' and raw)` → `oxidns_rule_update._fail('INVALID_REQUEST', 400)` |
| H1145 / R061 | K52:2242 抛出 | `raw / not valid` → `oxidns_rule_update._fail('INVALID_REQUEST', 400)` |
| H1146 / R052 | K52:2262 抛出 | `request.args or (request.content_length or 0) > 16 * 1024` → `caches._fail('INVALID_REQUEST', 400)` |
| H1147 / R052 | K52:2267 抛出 | `request.method == 'GET' / raw` → `caches._fail('INVALID_REQUEST', 400)` |
| H1148 / R052 | K52:2274 抛出 | `not (request.method == 'GET') / not isinstance(body, dict) or set(body) != {'tags'} or (not caches.valid_request_tags(body['tags']))` → `caches._fail('INVALID_REQUEST', 400)` |
| H1149 / R055 | K52:2292 错误响应 | `外部依赖/文件/事务异常` → `(response, 400)` |
| H1150 / R056 | K52:2363 抛出 | `request.method == 'PUT' / not latest or latest.get('service_type') != 'oxidns' or (latest.get('profile_id') or get_default_profile_id()) != profile_id or (body.get('revision') != revision(co…（完整见源行）` → `PolicyConflict('配置或设备绑定已更新，请重新载入后再保存。')` |

### K53 [backend/routes/client_tools.py](../../backend/routes/client_tools.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1151 / 所属路由表的同名handler | K53:19 抛出 | `not agent or agent.get('service_type') not in services` → `ProfileValidationError('请选择有效的 Mihomo / OxiDNS 设备。')` |
| H1152 / 所属路由表的同名handler | K53:25 抛出 | `bound_profile(agent_id, services) != profile_id` → `ProfileInUse('设备绑定的配置空间已改变，请重新打开后再保存。')` |
| H1153 / R008 | K53:47 抛出 | `request.method == 'PUT' / not isinstance(body, dict) or set(body) != {'profile_id', 'ip', 'name', 'previous_name'}` → `ProfileValidationError('设备备注请求格式无效。')` |
| H1154 / R008 | K53:49 抛出 | `request.method == 'PUT' / body['profile_id'] != profile_id` → `ProfileInUse('设备绑定的配置空间已改变，请重新打开后再保存。')` |
| H1155 / R008 | K53:56 抛出 | `request.method == 'PUT' / read_notes(config).get(ip, '') != previous` → `ProfileInUse('此设备的备注已在其他页面修改，请重新打开后再保存。')` |
| H1156 / R008 | K53:63 抛出 | `捕获 OxidnsSettingsError` → `ProfileValidationError(str(exc))` |
| H1157 / R047 | K53:84 抛出 | `not isinstance(body, dict) or set(body) != {'profile_id', 'ruleset_id', 'target', 'value', 'rule_type'}` → `ProfileValidationError('连接规则请求格式无效。')` |
| H1158 / R047 | K53:86 抛出 | `body['profile_id'] != profile_id` → `ProfileInUse('设备绑定的配置空间已改变，请重新打开后再保存。')` |
| H1159 / R047 | K53:88 抛出 | `body['target'] not in ('direct', 'proxy') or not isinstance(body['ruleset_id'], str) or (not body['ruleset_id'])` → `ProfileValidationError('请选择有效的规则集和代理或直连目标。')` |
| H1160 / R047 | K53:102 抛出 | `settings.get(body['target'] + '_ruleset') != body['ruleset_id'] or (infos is not None and infos[body['target']]['id'] != body['ruleset_id'])` → `ProfileInUse('默认规则集已改变，请重新打开后再保存。')` |

### K54 [backend/routes/docker_ha_installation.py](../../backend/routes/docker_ha_installation.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1161 / 所属路由表的同名handler | K54:18 抛出 | `len(raw) > limit` → `enrollment.EnrollmentError('安装请求内容过大。', 413)` |
| H1162 / 所属路由表的同名handler | K54:23 抛出 | `捕获 (ValueError, UnicodeError, RecursionError) / len(raw) > 16384` → `enrollment.EnrollmentError('安装请求内容过大。', 413)` |
| H1163 / 所属路由表的同名handler | K54:24 抛出 | `捕获 (ValueError, UnicodeError, RecursionError)` → `ValueError('安装请求需要 JSON 对象。')` |
| H1164 / 所属路由表的同名handler | K54:26 抛出 | `len(raw) > 16384 and (not allow_check or not isinstance(value, dict) or value.get('action') != 'check')` → `enrollment.EnrollmentError('安装请求内容过大。', 413)` |
| H1165 / 所属路由表的同名handler | K54:28 抛出 | `not isinstance(value, dict)` → `ValueError('安装请求需要 JSON 对象。')` |
| H1166 / R085 | K54:64 抛出 | `_body() != {}` → `ValueError('安装登记无需提交设备信息，目标由生成命令时固定。')` |

### K55 [backend/routes/gateway_installations.py](../../backend/routes/gateway_installations.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1167 / 所属路由表的同名handler | K55:13 抛出 | `request.content_length is not None and request.content_length > 16384` → `installations.InstallationError('安装请求内容过大。', 413)` |
| H1168 / 所属路由表的同名handler | K55:17 抛出 | `len(raw) > 16384` → `installations.InstallationError('安装请求内容过大。', 413)` |
| H1169 / 所属路由表的同名handler | K55:21 抛出 | `捕获 (ValueError, UnicodeError)` → `installations.InstallationError('安装请求需要 JSON 对象。')` |
| H1170 / 所属路由表的同名handler | K55:23 抛出 | `not isinstance(value, dict)` → `installations.InstallationError('安装请求需要 JSON 对象。')` |

### K56 [backend/routes/gateway_onboarding.py](../../backend/routes/gateway_onboarding.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1171 / 所属路由表的同名handler | K56:24 抛出 | `request.content_length and request.content_length > 262144` → `GatewayOnboardingError('安装设置超过 256 KiB 限制')` |
| H1172 / 所属路由表的同名handler | K56:29 抛出 | `len(data) > 262144` → `GatewayOnboardingError('安装设置超过 256 KiB 限制')` |
| H1173 / 所属路由表的同名handler | K56:32 抛出 | `捕获 (ValueError, UnicodeDecodeError)` → `GatewayOnboardingError('安装设置必须是有效 JSON')` |
| H1174 / R116 | K56:63 抛出 | `expected is not None and expected != repository.config_revision(profile_id)` → `ProfileInUse('配置或共享资源已更新，请刷新安装向导并重新预览后保存')` |
| H1175 / R116 | K56:69 抛出 | `marker != read_marker(before)[0] or (takeover == 'full_tun' and marker == 'absent' and gateway_settings(current)['enabled'])` → `ProfileInUse('配置已更新，请刷新安装向导并重新预览后保存')` |
| H1176 / R116 | K56:72 抛出 | `not result['ready']` → `GatewayOnboardingBlocked(result)` |
| H1177 / R116 | K56:89 错误结果 | `捕获 GatewayOnboardingBlocked` → `_response({**exc.result, 'success': False, 'message': str(exc)}, 400)` |
| H1178 / R116 | K56:91 错误结果 | `捕获 GatewayOnboardingError` → `_response({'success': False, 'message': str(exc)}, 400)` |

### K57 [backend/routes/ipv6_readiness.py](../../backend/routes/ipv6_readiness.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1179 / R027 | K57:23 抛出 | `request.method not in methods.get(operation, ())` → `MonitorError('NOT_ALLOWED', 404, '不支持这个 IPv6 操作')` |
| H1180 / R027 | K57:25 抛出 | `request.content_length and request.content_length > 4096` → `MonitorError('INVALID_REQUEST', 400, 'IPv6 请求内容过大')` |
| H1181 / R027 | K57:28 抛出 | `not isinstance(body, dict)` → `MonitorError('INVALID_REQUEST', 400, 'IPv6 请求必须是 JSON 对象')` |
| H1182 / R027 | K57:33 抛出 | `not selected or any((not isinstance(value, str) or value != selected[0] for value in selected))` → `MonitorError('PROFILE_REQUIRED', 400, '请从同一个明确的配置空间打开 IPv6 设置')` |
| H1183 / R027 | K57:55 抛出 | `not (operation == 'verification-settings') / body` → `MonitorError('INVALID_REQUEST', 400, '连接检查只使用当前配置空间中已保存的节点')` |

### K58 [backend/routes/linux_gateway.py](../../backend/routes/linux_gateway.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1184 / 所属路由表的同名handler | K58:45 抛出 | `context is None and needs_context(current)` → `LinuxGatewayError('Linux 网关设置已在其他位置修改，请刷新后重试')` |
| H1185 / 所属路由表的同名handler | K58:49 抛出 | `mode == 'invalid'` → `RouterSplitError(MESSAGES['ROUTER_SPLIT_INVALID'], 'ROUTER_SPLIT_INVALID')` |
| H1186 / 所属路由表的同名handler | K58:52 抛出 | `mode == 'full_tun' / restore` → `RouterSplitError(_restore_blocked(current), 'ROUTER_SPLIT_RESTORE_BLOCKED')` |
| H1187 / 所属路由表的同名handler | K58:54 抛出 | `mode == 'full_tun' / after['enabled']` → `RouterSplitError(MESSAGES['ROUTER_SPLIT_FULL_TUN'], 'ROUTER_SPLIT_FULL_TUN')` |
| H1188 / 所属路由表的同名handler | K58:64 抛出 | `not base_is_full_tun(candidate) / restore` → `RouterSplitError(LEGACY_RESTORE_BLOCKED, 'ROUTER_SPLIT_RESTORE_BLOCKED')` |
| H1189 / 所属路由表的同名handler | K58:65 抛出 | `not base_is_full_tun(candidate)` → `RouterSplitError(MESSAGES['ROUTER_SPLIT_LEGACY'], 'ROUTER_SPLIT_LEGACY')` |
| H1190 / 所属路由表的同名handler | K58:68 抛出 | `refusal` → `RouterSplitError(refusal, 'ROUTER_SPLIT_FORWARDING_NOT_PERSISTED')` |
| H1191 / R104 | K58:103 抛出 | `'refusal' not in checked / not io` → `LinuxGatewayError('Linux 网关设置已在其他位置修改，请刷新后重试')` |
| H1192 / R104 | K58:113 抛出 | `request.method == 'POST' / not isinstance(value, dict)` → `LinuxGatewayError('Linux 网关设置必须是 JSON 对象')` |
| H1193 / R104 | K58:171 错误结果 | `捕获 LinuxGatewayError` → `(jsonify({'success': False, 'message': str(exc)}), 400)` |
| H1194 / R109 | K58:180 错误结果 | `not auth['valid']` → `(jsonify({'success': False, 'message': auth['message']}), 401)` |
| H1195 / R109 | K58:187 错误结果 | `捕获 LinuxGatewayError` → `(jsonify({'success': False, 'message': str(exc)}), 503)` |
| H1196 / R110 | K58:196 错误结果 | `not auth['valid']` → `(jsonify({'success': False, 'message': auth['message']}), 401)` |
| H1197 / R110 | K58:203 错误结果 | `捕获 LinuxGatewayError` → `(jsonify({'success': False, 'message': str(exc)}), 503)` |

### K59 [backend/routes/linux_gateway_agents.py](../../backend/routes/linux_gateway_agents.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1198 / R088 | K59:33 错误结果 | `not isinstance(body, dict) or set(body) != {'agent_id'} or (not isinstance(body['agent_id'], str))` → `_no_store(jsonify({'success': False, 'code': 'INVALID_REQUEST', 'message': '请求只能包含 agent_id。'}), 400)` |
| H1199 / R088 | K59:36 错误结果 | `agent_id not in linked` → `_no_store(jsonify({'success': False, 'code': 'NOT_LINKED', 'message': '这台 Agent 不是任何路由器的代理网关。'}), 404)` |
| H1200 / R088 | K59:41 错误结果 | `捕获 MonitorError` → `_no_store(jsonify({'success': False, 'code': error.code, 'message': error.message}), error.status)` |
| H1201 / R032 | K59:54 抛出 | `not selected` → `MonitorError('PROFILE_REQUIRED', 400, '请从一个明确的配置空间打开 Linux 网关设置。')` |
| H1202 / R032 | K59:59 抛出 | `request.method == 'POST' / request.content_length is not None and request.content_length > 4096` → `MonitorError('INVALID_REQUEST', 400, '网关请求内容过大。')` |
| H1203 / R032 | K59:62 抛出 | `request.method == 'POST' / len(raw) > 4096` → `MonitorError('INVALID_REQUEST', 400, '网关请求内容过大。')` |
| H1204 / R032 | K59:66 抛出 | `request.method == 'POST' / 捕获 (ValueError, UnicodeError)` → `MonitorError('INVALID_REQUEST', 400, '网关参数必须是 JSON 对象。')` |

### K60 [backend/utils/connection_rules.py](../../backend/utils/connection_rules.py#L1)

| H / 驱动 | 源行 / 类型 | 错误输入或故障guard → 结果 |
|---|---|---|
| H1205 / R047 | K60:38 抛出 | `rule_type != ('IP-CIDR' if network.version == 4 else 'IP-CIDR6')` → `ValueError()` |
| H1206 / R047 | K60:41 抛出 | `捕获 (ValueError, OxidnsSettingsError)` → `ProfileValidationError('请选择有效的目标 IP 或网段，IPv4 使用 IP-CIDR，IPv6 使用 IP-CIDR6。')` |
| H1207 / R047 | K60:58 抛出 | `info is None` → `ProfileValidationError('所选规则集不支持 IP、不可编辑或策略已改变，请重新打开后选择。')` |

## HR实验网边界（L1–L4）

每条HR相关路由已在主表标“需实验网Lx”；以下列明这些标记的**具体未被fake证明部分**。这份Step A不执行实验网、不启动PVE任务，交协调人依WORKFLOW由Opus安排；不要把fake可达性当真实健康/安全接管结论。

| 批次 | 需真实验证的项目 | fake能测/测不出的分界 | Python依据 |
|---|---|---|---|
| **L1** | A08–A10 installer、Go单服务/AIO/双核心安装、Docker host/bridge/macvlan、新装/迁移/Compose bundle/direct/compact、旧短链保护；A15升级/回滚protocol1/2与DHCP地址跟随 | fake可比脚本文本、凭据、HTTP与持久状态；不能证明Debian systemd/Alpine OpenRC真实service生命周期、二进制架构兼容、sh引用执行、卷归属/旧身份重启复用、Compose依赖/宿主namespace身份、升级worker迁移/掉电恢复、地址在真实网卡变化后可达 | [go_install_script.py:90](../../backend/agents/go_install_script.py#L90)、[docker_ha_guide.py:377](../../backend/agents/docker_ha_guide.py#L377)、[upgrades.py:175](../../backend/agents/upgrades.py#L175) |
| **L2** | A01/A11/A14 主配置与完整包publish/activate/rollback/recover；Mihomo/MosDNS/OxiDNS与Surge旧配置；final-preview目标适配/资源加载 | fake证明digest/状态机/接受未知/回执所有权/顺序；不能证明目标核心真正加载包、重启/热加载/DNS健康检查、不同配置路径/服务管理、原配置真实恢复、资源大小/权限/运行中状态与结果一致。**[O6]** Surge旧配置路径没有实验网载体（Surge只在macOS/iOS上运行），只能fake验证，真实设备交O14切换时在维护者自己的设备上确认 | [manager.py:1044](../../backend/agents/manager.py#L1044)、[deployment_bundle.py:1](../../backend/agents/deployment_bundle.py#L1)、[generated_preview.py:40](../../backend/agents/generated_preview.py#L40) |
| **L3** | A16规则热更、A17 DNS-follow、A18 DNS HA与VIP handoff/回滚/恢复、真实source rollback及两成员失败 | fake证明双成员协议、baseline/草稿保护、split-brain报告门禁与未知结果不推进；不能证明native/host/macvlan真实keepalived VRRP、两个独立宿主、VIP只持一端/主机故障接管、无DNS中断、upstream真实可达、缓存/热重载实时生效 | [rule_sync.py:202](../../backend/agents/rule_sync.py#L202)、[dns_follow.py:531](../../backend/agents/dns_follow.py#L531)、[oxidns_ha_handoff.py:82](../../backend/agents/oxidns_ha_handoff.py#L82) |
| **L4** | A19–A22 selective/full_tun/legacy Linux gateway、forwarding/TUN准备、安装双核心与撤销；A23 IPv6 readiness和独立client+实验RouterOS实流验收 | fake可核对spec/profile/check_token/revision、tuple证据归并、拒绝foreign对象；不能证明ip rule/route、nft/iptables、sysctl重启持久、TUN捕获、IPv6/PD/fakeIP端到端/回流及泄漏防护、手机/电脑链路。无client或无实流证据必须unknown，不能用配置布尔值补pass | [agents/linux_gateway.py:350](../../backend/agents/linux_gateway.py#L350)、[gateway_installation.py:629](../../backend/agents/gateway_installation.py#L629)、[ipv6_verification.py:306](../../backend/agents/ipv6_verification.py#L306) |

## 前端入口、跨域和框架核对

- 前端绑定设备、发布guard/generation_id/部署轮询、upgrade/query、DNS follow/HA、gateway/IPv6均与R表对应；必须使用捕获的profile和guard而非全局sidebar。来源 [api/index.ts:478](../../frontend/src/api/index.ts#L478)、[api/index.ts:533](../../frontend/src/api/index.ts#L533)、[api/agentRuleSync.ts:16](../../frontend/src/api/agentRuleSync.ts#L16)、[api/linuxGateway.ts:82](../../frontend/src/api/linuxGateway.ts#L82)、[api/gatewayOnboarding.ts:64](../../frontend/src/api/gatewayOnboarding.ts#L64)。
- **额外404核对项E01/E02**：前端API文件仍有 `/agents/docker-compose`、`/agents/docker-run`（[index.ts:556](../../frontend/src/api/index.ts#L556)），Python实际注册的是 `/agents/docker-agent-compose`、`/agents/docker-agent-run`（[agents.py:1270](../../backend/routes/agents.py#L1270)）。这两处不在routes.tsv 120条中；Step B可各加匿名/管理员请求404哨兵并交Opus确认入口是否在使用，不能擅自给Go补别名或用前端旧地址替代生成器成功测试。**[O6]** 补充：这两个路径在before_request的公开精确路径表里，所以开启登录时匿名请求得到404而不是401（同表还有`docker-mihomo-*`、`docker-mosdns-*`四个不存在的路径）；前端`generateDockerCompose/generateDockerRun`没有任何调用方，是死代码。见X03。
- metrics flush、自动rule-sync、DNS-follow、HA出版、IPv6 verify、OxiDNS ranking采集等循环需在Step B起真实公开服务进程，用synthetic fake和公开API触发/轮询，不直接调用Go worker；生命周期关闭/任务时间可用seed时间与进程退出观测。config领域tasks触发、routers回流和设备应用归相应跨域联测，未列出的私有Agent端点不能当本面板routes.tsv新路由。
- 前端实时日志和OxiDNS查询来源可能依赖Agent对核心SSE的订阅；Python面板边界只定义有限REST响应，本清单**没有读Go Agent实现，也不宣称覆盖内部SSE/WebSocket帧处理**。真实Agent收集、断流重连和核心版本兼容需独立集成/L2或对应域验证。
- Step B比较器需扩展原短请求框架以观测SQLite、目录、fake transcript、进程重启和background最终状态。仅有生产离线快照GET对比不足以证明这里的写入/rollback/installer/双端协议；故意破坏输入/结果的T24必须能报差异。

## 汇总与部分覆盖清单

- **路由120条**；R表method/template去重恰好120。
- **[O6]** 审核后另加逐路由用例105个（50条路由）、横切X01–X15与T25/T26共17项，合计 **3742** 条；更正17处。明细见文末“O6 review”。
- **路由场景2339个**：成功S 365、失败/降级F 1518、边界B 456。A/C身份场景已按适用路由展开。
- 附加 **H源码分支配方1207、W协议交互24、G生成形态组24、T长序列24、E额外404核对2**；合计清单条目 **3620**。H含不可HTTP达的审计哨兵，W/G/T有待参数化子变体；这些数字不代表实现/通过数或最终HTTP请求数。
- 主表 **79条路由标需L1–L4实验网**，具体行为按上节，不把纯校验也推迟到实验网。
- 需要额外长序列、协议观测或实验网才能完成外部验收的路由 **110条**，如下逐条列出；单轮请求/响应只能部分覆盖。其余路由仍需按写后比较代码做DB/文件观察，不因未列partial就声称所有OS失败已可达。

| R | method / route | 部分覆盖原因与补测 | 需实验网部分 |
|---|---|---|---|
| R001 | `GET /api/agents` | 后台content comparison、cache与分页version/token脱敏；checking后再读，不能mask整个diff。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R003 | `DELETE /api/agents/<agent_id>` | 注册/编辑/删除与持久身份、通知、在途锁、并发；需DB与多请求fixture。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R004 | `GET /api/agents/<agent_id>` | 后台content comparison、cache与分页version/token脱敏；checking后再读，不能mask整个diff。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R005 | `PUT /api/agents/<agent_id>` | 注册/编辑/删除与持久身份、通知、在途锁、并发；需DB与多请求fixture。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R006 | `GET /api/agents/<agent_id>/address` | 出站地址模式/探活/写设备+写面板的顺序，自动follow节流；W03/T04；实际换IP L1。 | L1，具体行为见L表 |
| R007 | `PUT /api/agents/<agent_id>/address` | 出站地址模式/探活/写设备+写面板的顺序，自动follow节流；W03/T04；实际换IP L1。 | L1，具体行为见L表 |
| R010 | `GET /api/agents/<agent_id>/config` | 生成输出含绑定profile/provider缓存；需摘要自洽与目标端解析，真实加载L2。 | L2，具体行为见L表 |
| R011 | `GET /api/agents/<agent_id>/config/logging` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R012 | `POST /api/agents/<agent_id>/config/logging` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R013 | `GET /api/agents/<agent_id>/deployments/<deployment_id>` | 持久journal、unknown对账/幂等/状态机、回滚及两端副作用；W05–W08/T06–T11，真核心L2/HA L3。 | L2、L3，具体行为见L表 |
| R014 | `POST /api/agents/<agent_id>/deployments/<deployment_id>/activate` | 持久journal、unknown对账/幂等/状态机、回滚及两端副作用；W05–W08/T06–T11，真核心L2/HA L3。 | L2、L3，具体行为见L表 |
| R015 | `POST /api/agents/<agent_id>/deployments/<deployment_id>/recover` | 持久journal、unknown对账/幂等/状态机、回滚及两端副作用；W05–W08/T06–T11，真核心L2/HA L3。 | L2、L3，具体行为见L表 |
| R016 | `POST /api/agents/<agent_id>/deployments/<deployment_id>/rollback` | 持久journal、unknown对账/幂等/状态机、回滚及两端副作用；W05–W08/T06–T11，真核心L2/HA L3。 | L2、L3，具体行为见L表 |
| R017 | `DELETE /api/agents/<agent_id>/dns-follow` | 源成功部署后异步子发布、未知任务/人工draft保护；W19/T13，实际DNS连通L3。 | L3，具体行为见L表 |
| R018 | `GET /api/agents/<agent_id>/dns-follow` | 源成功部署后异步子发布、未知任务/人工draft保护；W19/T13，实际DNS连通L3。 | L3，具体行为见L表 |
| R019 | `PUT /api/agents/<agent_id>/dns-follow` | 源成功部署后异步子发布、未知任务/人工draft保护；W19/T13，实际DNS连通L3。 | L3，具体行为见L表 |
| R020 | `POST /api/agents/<agent_id>/dns-follow/retry` | 源成功部署后异步子发布、未知任务/人工draft保护；W19/T13，实际DNS连通L3。 | L3，具体行为见L表 |
| R023 | `POST /api/agents/<agent_id>/generate-config` | 生成可写订阅缓存与目标final-preview，冻结文件/容量/TTL需多请求；W05/T05。 | L2、L3，具体行为见L表 |
| R024 | `GET /api/agents/<agent_id>/generated-config/<preview_id>/download` | 生成可写订阅缓存与目标final-preview，冻结文件/容量/TTL需多请求；W05/T05。 | L2，具体行为见L表 |
| R025 | `GET /api/agents/<agent_id>/generated-config/<preview_id>/file` | 生成可写订阅缓存与目标final-preview，冻结文件/容量/TTL需多请求；W05/T05。 | L2，具体行为见L表 |
| R026 | `POST /api/agents/<agent_id>/heartbeat` | 内存runtime、指标flush、异步地址核实与删Agent竞争；W02/W03、T02–T04。 | L1，具体行为见L表 |
| R027 | `GET /api/agents/<agent_id>/ipv6-readiness` | 显式profile与published/client/router证据，多请求验收；T22；fake不能证明实际IPv6链路L4。 | L4，具体行为见L表 |
| R028 | `POST /api/agents/<agent_id>/ipv6-readiness/<operation>` | 显式profile与published/client/router证据，多请求验收；T22；fake不能证明实际IPv6链路L4。 | L4，具体行为见L表 |
| R029 | `GET /api/agents/<agent_id>/ipv6-readiness/verification-settings` | 显式profile与published/client/router证据，多请求验收；T22；fake不能证明实际IPv6链路L4。 | L4，具体行为见L表 |
| R030 | `PUT /api/agents/<agent_id>/ipv6-readiness/verification-settings` | 显式profile与published/client/router证据，多请求验收；T22；fake不能证明实际IPv6链路L4。 | L4，具体行为见L表 |
| R031 | `GET /api/agents/<agent_id>/ipv6-readiness/verify` | 显式profile与published/client/router证据，多请求验收；T22；fake不能证明实际IPv6链路L4。 | L4，具体行为见L表 |
| R032 | `GET /api/agents/<agent_id>/linux-gateway/<operation>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R033 | `POST /api/agents/<agent_id>/linux-gateway/<operation>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R034 | `GET /api/agents/<agent_id>/logs` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R035 | `POST /api/agents/<agent_id>/logs/clear` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R036 | `GET /api/agents/<agent_id>/logs/sources` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R037 | `POST /api/agents/<agent_id>/logs/validate` | 外部日志文件/legacy sources与脱敏，需fake transcript；真实rotation与读权限另验。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R038 | `GET /api/agents/<agent_id>/metrics` | 心跳内存、定时落盘和重启恢复、计数器reset；T03与私有文件/DB观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R039 | `GET /api/agents/<agent_id>/metrics/history` | 心跳内存、定时落盘和重启恢复、计数器reset；T03与私有文件/DB观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R040 | `GET /api/agents/<agent_id>/metrics/summary` | 心跳内存、定时落盘和重启恢复、计数器reset；T03与私有文件/DB观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R041 | `DELETE /api/agents/<agent_id>/mihomo/<path:resource>` | 受限method/resource桥接和finite log poll，需出站/上游错误fake；真实core运行状态另验。 | L2，具体行为见L表 |
| R042 | `GET /api/agents/<agent_id>/mihomo/<path:resource>` | 受限method/resource桥接和finite log poll，需出站/上游错误fake；真实core运行状态另验。 | L2，具体行为见L表 |
| R043 | `PATCH /api/agents/<agent_id>/mihomo/<path:resource>` | 受限method/resource桥接和finite log poll，需出站/上游错误fake；真实core运行状态另验。 | L2，具体行为见L表 |
| R044 | `POST /api/agents/<agent_id>/mihomo/<path:resource>` | 受限method/resource桥接和finite log poll，需出站/上游错误fake；真实core运行状态另验。 | L2，具体行为见L表 |
| R045 | `PUT /api/agents/<agent_id>/mihomo/<path:resource>` | 受限method/resource桥接和finite log poll，需出站/上游错误fake；真实core运行状态另验。 | L2，具体行为见L表 |
| R048 | `DELETE /api/agents/<agent_id>/oxidns-ha` | 两个成员、VIP/handoff、失败撤销/unknown/leftover；W20/T10/T11，VRRP真实L3。 | L3，具体行为见L表 |
| R049 | `GET /api/agents/<agent_id>/oxidns-ha` | 两个成员、VIP/handoff、失败撤销/unknown/leftover；W20/T10/T11，VRRP真实L3。 | L3，具体行为见L表 |
| R050 | `PUT /api/agents/<agent_id>/oxidns-ha` | 两个成员、VIP/handoff、失败撤销/unknown/leftover；W20/T10/T11，VRRP真实L3。 | L3，具体行为见L表 |
| R051 | `GET /api/agents/<agent_id>/oxidns-ha-environment` | 两个成员、VIP/handoff、失败撤销/unknown/leftover；W20/T10/T11，VRRP真实L3。 | L1、L3，具体行为见L表 |
| R052 | `GET /api/agents/<agent_id>/oxidns/cache` | 设备cache标签校验/partial flush，需fake实际状态+transcript；真实cache效果集成验证。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R053 | `POST /api/agents/<agent_id>/oxidns/cache/flush` | 设备cache标签校验/partial flush，需fake实际状态+transcript；真实cache效果集成验证。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R054 | `GET /api/agents/<agent_id>/oxidns/clients` | query/rankings/指标快照、reset与发布统计跨请求状态、历史slot；W17/T16。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R055 | `POST /api/agents/<agent_id>/oxidns/dashboard/reset` | query/rankings/指标快照、reset与发布统计跨请求状态、历史slot；W17/T16。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R057 | `PUT /api/agents/<agent_id>/oxidns/device-policies` | 双revision和绑定并发保护、native DNS副作用需连续GET/PUT与存储观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R058 | `GET /api/agents/<agent_id>/oxidns/overview` | query/rankings/指标快照、reset与发布统计跨请求状态、历史slot；W17/T16。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R059 | `GET /api/agents/<agent_id>/oxidns/queries` | query/rankings/指标快照、reset与发布统计跨请求状态、历史slot；W17/T16。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R060 | `GET /api/agents/<agent_id>/oxidns/queries/<record_id>` | query/rankings/指标快照、reset与发布统计跨请求状态、历史slot；W17/T16。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R061 | `GET /api/agents/<agent_id>/oxidns/rules/update` | 202任务poll、选择性paths/partial reload、HA双端；W18/T17，真实热更L3。 | L3，具体行为见L表 |
| R062 | `POST /api/agents/<agent_id>/oxidns/rules/update` | 202任务poll、选择性paths/partial reload、HA双端；W18/T17，真实热更L3。 | L3，具体行为见L表 |
| R063 | `POST /api/agents/<agent_id>/push-config` | 持久journal、unknown对账/幂等/状态机、回滚及两端副作用；W05–W08/T06–T11，真核心L2/HA L3。 | L2、L3，具体行为见L表 |
| R064 | `GET /api/agents/<agent_id>/push-diff` | 后台content comparison、cache与分页version/token脱敏；checking后再读，不能mask整个diff。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R065 | `GET /api/agents/<agent_id>/push-diff/entries` | 后台content comparison、cache与分页version/token脱敏；checking后再读，不能mask整个diff。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R066 | `POST /api/agents/<agent_id>/restart` | 远端接收、进程重启/卸载与本地清理不是单响应能证明；W10与实验网。 | L2，具体行为见L表 |
| R067 | `GET /api/agents/<agent_id>/rule-sync` | 异步timer、receipt/source与允许变更范围、HA主备；T12，真实热加载L3。 | L3，具体行为见L表 |
| R068 | `PUT /api/agents/<agent_id>/rule-sync` | 异步timer、receipt/source与允许变更范围、HA主备；T12，真实热加载L3。 | L3，具体行为见L表 |
| R070 | `DELETE /api/agents/<agent_id>/traffic-history` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R071 | `GET /api/agents/<agent_id>/traffic-history` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R072 | `POST /api/agents/<agent_id>/traffic-history` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R073 | `PUT /api/agents/<agent_id>/traffic-history` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R074 | `POST /api/agents/<agent_id>/traffic-report` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R075 | `GET /api/agents/<agent_id>/traffic/stats` | 心跳内存、定时落盘和重启恢复、计数器reset；T03与私有文件/DB观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R076 | `GET /api/agents/<agent_id>/traffic/trend` | 心跳内存、定时落盘和重启恢复、计数器reset；T03与私有文件/DB观察。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R077 | `POST /api/agents/<agent_id>/uninstall` | 远端接收、进程重启/卸载与本地清理不是单响应能证明；W10与实验网。 | L1，具体行为见L表 |
| R078 | `POST /api/agents/<agent_id>/update` | protocol1/2独立worker与migration/心跳证据，已接受不等于已升级；W11/W12/T14，真实L1。 | L1，具体行为见L表 |
| R079 | `GET /api/agents/<agent_id>/upgrade` | protocol1/2独立worker与migration/心跳证据，已接受不等于已升级；W11/W12/T14，真实L1。 | L1，具体行为见L表 |
| R080 | `POST /api/agents/<agent_id>/upgrade/report` | protocol1/2独立worker与migration/心跳证据，已接受不等于已升级；W11/W12/T14，真实L1。 | L1，具体行为见L表 |
| R081 | `POST /api/agents/core-install-command` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1，具体行为见L表 |
| R082 | `GET /api/agents/core-install-script` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1，具体行为见L表 |
| R083 | `GET /api/agents/docker-agent-compose` | YAML/argv与环境/卷/网络，文本不能证明Docker实际安装；G07–G10/L1。 | L1，具体行为见L表 |
| R084 | `GET /api/agents/docker-agent-run` | YAML/argv与环境/卷/网络，文本不能证明Docker实际安装；G07–G10/L1。 | L1，具体行为见L表 |
| R085 | `POST /api/agents/docker-oxidns-ha-enroll/<session_id>` | 签发/过期/登记幂等和脚本/Compose包，需私有session/transcript；安装、身份/卷真实性L1。 | L1，具体行为见L表 |
| R086 | `POST /api/agents/docker-oxidns-ha-guide` | 签发/过期/登记幂等和脚本/Compose包，需私有session/transcript；安装、身份/卷真实性L1。 | L1，具体行为见L表 |
| R087 | `GET /api/agents/download/<filename>` | 二进制字节/SHA/架构下载和实际可执行性；fake元数据之外的启动兼容L1。 | L1，具体行为见L表 |
| R088 | `GET /api/agents/gateway-forwarding` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R089 | `POST /api/agents/gateway-forwarding` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R090 | `GET /api/agents/gateway-installations` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L1、L4，具体行为见L表 |
| R091 | `POST /api/agents/gateway-installations` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L1、L4，具体行为见L表 |
| R092 | `GET /api/agents/gateway-installations/<session_id>` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L1、L4，具体行为见L表 |
| R093 | `POST /api/agents/gateway-installations/<session_id>/enroll` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L1、L4，具体行为见L表 |
| R094 | `POST /api/agents/gateway-installations/<session_id>/network` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L4，具体行为见L表 |
| R095 | `POST /api/agents/gateway-installations/<session_id>/publish` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L2、L4，具体行为见L表 |
| R096 | `GET /api/agents/gateway-installations/<session_id>/script` | session→两个Agent→双核心发布→网络检查，后台及继续重试需T18/T19；实际L1/L2/L4。 | L1、L4，具体行为见L表 |
| R097 | `GET /api/agents/gateway-network-probe.sh` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1、L4，具体行为见L表 |
| R098 | `GET /api/agents/i/<token>` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1，具体行为见L表 |
| R099 | `POST /api/agents/install-command` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1，具体行为见L表 |
| R100 | `GET /api/agents/install-script` | 生成文本/短链/实际执行三边界；G01–G06/G17/G19；执行和service生命周期L1。 | L1，具体行为见L表 |
| R102 | `POST /api/agents/register` | 注册/编辑/删除与持久身份、通知、在途锁、并发；需DB与多请求fixture。 | L1，具体行为见L表 |
| R103 | `PUT /api/agents/traffic-history/retention` | 主动上报后的聚合/保留/落盘、开关与版本语义；W15/T03，不证明真实采样。 | 未强制指定L批次；实际Agent/Core语义另验 |
| R104 | `GET /api/custom-config/linux-gateway` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R105 | `POST /api/custom-config/linux-gateway` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R106 | `POST /api/custom-config/linux-gateway/preview` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R107 | `POST /api/custom-config/linux-gateway/restore` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R108 | `POST /api/custom-config/linux-gateway/restore/preview` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R109 | `GET /api/custom-config/linux-gateway/rules/<path:rule_id>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R110 | `GET /api/custom-config/linux-gateway/source/<path:rule_id>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R111 | `GET /api/profiles/<profile_id>/custom-config/linux-gateway` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R112 | `POST /api/profiles/<profile_id>/custom-config/linux-gateway` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R113 | `POST /api/profiles/<profile_id>/custom-config/linux-gateway/preview` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R114 | `POST /api/profiles/<profile_id>/custom-config/linux-gateway/restore` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R115 | `POST /api/profiles/<profile_id>/custom-config/linux-gateway/restore/preview` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R116 | `GET /api/profiles/<profile_id>/gateway-onboarding` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R117 | `POST /api/profiles/<profile_id>/gateway-onboarding` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R118 | `POST /api/profiles/<profile_id>/gateway-onboarding/preview` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R119 | `GET /api/profiles/<profile_id>/linux-gateway/rules/<path:rule_id>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |
| R120 | `GET /api/profiles/<profile_id>/linux-gateway/source/<path:rule_id>` | profile draft/restore preview门禁及OS操作两个边界，W21/T20/T21，真实网络L4。 | L4，具体行为见L表 |

所有实验网项目均只列计划，待协调人按已批准L1–L4条件执行；未接触PVE、生产设备、真实token或快照。此步骤没有测试/构建/lint/dev server，也不承诺分支实现已通过。交回后先由Opus逐条审核覆盖与可达性，再进入Step B；测试发现差异回移植agent，本清单作者不改Go实现。

## O6 review

Reviewer: Opus (O6, 2026-11-17). Basis: Python source on this branch (`backend/`, `docker/nginx.conf`, `docker/Dockerfile`), `routes.tsv`, `FILES.md` and `frontend/src/api`. No Go implementation code was read and nothing was run; this is a document review.

### Verdict

**Approved with additions.** Step B may start from this matrix as amended, with one precondition: the harness rules in X07 (both servers need their own heartbeats), X08 (start Python as the production process) and X13 (one fake Agent set per server) must exist before any W/T case runs, because without them most Agent cases fail or pass for the wrong reason.

The per-handler work is thorough. Almost every error return in `routes/agents.py` and the agents modules has an H row, the W protocol table matches the timeouts, headers and retry rules in `manager.py`, and the fake-versus-lab split in the L table is drawn in the right places. The misses are of the same kind the config review found: they sit outside the handlers — in the Flask application shell, in the production Nginx in front of Flask, in startup code and background loops, and in state that only lives in process memory.

As in the config review: the expected statuses and messages in the rows are hints, and the Python response is the oracle. What cannot correct itself is a scenario that is never sent, and the additions are all of that kind.

### Most important misses (now added)

1. **Global JSON redaction hook** (X01, R002 S90, R079 S90, R085 S91). Every JSON response is rewritten after the handler, and the redaction set includes every persisted Agent token, read after the commit. So the manual-add response (R002) returns `data.token: "[REDACTED]"`, and only two endpoints may return a top-level token. The original never mentioned the hook.
2. **Online state is memory-only, and the fake must heartbeat both servers** (X07, X13). An Agent is online for 120 s after its last heartbeat or registration, offline after any restart, and online forever if seeded with a future `last_heartbeat`. Publishing, HA, IPv6, forwarding and gateway installation all check it. Fake Agents shared between the two servers cannot tell which server sent a request; each server needs its own.
3. **Background loops and how Python is started** (X08). DNS follow, HA publication and IPv6 verification start on import. The Agent rule-sync loop starts only on the first request that passes profile resolution and authentication. Metrics flush, history loading and SIGTERM saving exist only under `python -m backend.app`. A harness built on a Flask test client would silently lack half of this.
4. **Production Nginx in front of Flask** (X15, R072 B90, R080 F92). Real Agents talk to Nginx: a 1 MiB body limit (the 4 MiB traffic-history limit is unreachable), chunked bodies turned into Content-Length, `Host` without port, forced `X-Forwarded-Host/Proto`. Go replaces Nginx, so the behaviour Go must match is Nginx plus Flask, not bare Flask. Where the two differ, Go's choice goes to O12.
5. **Profile resolution before authentication, and the agents blueprint being global** (X02, F90/F91 on 17 routes). `/api/agents/**` ignores profile headers completely. The linux-gateway `custom_config` routes and the `/api/profiles/<id>/…` routes resolve the profile before login, so an unauthenticated request with a missing profile gets 404, including on the public rule/source providers.
6. **Request-body and parameter parsing as a class** (X05, 29 per-route cases). Includes: the order 401 → 413 → Content-Type → JSON; `json.loads(bytes)` accepting a BOM, UTF-16 and NaN; deep nesting giving a JSON 500 on heartbeat but an HTML 500 on traffic reports; int versus float in metrics; `request.args.get(type=int)` silently using the default (an invalid installer port becomes 8080, so R100 F05 was wrong); registration accepting any field types, which later crashes the full Agent list in Python.
7. **Where the panel URL and client IP come from** (X06). Six routes derive it in five different ways (Host only; both `X-Forwarded-*` headers together; each header independently; a body `base_url`; `host_url`). The registration host substitution covers only 172.17–31, `127.`, `localhost` and an empty host. These values flow into generated scripts and configs.
8. **Routing precedence** (X04, R044/R047/R052/R098 B90). Werkzeug prefers static segments and falls through on method mismatch: `/api/agents/i/status` goes to the install link, and `POST /<id>/mihomo/rule-targets` goes to the Mihomo bridge. The overlapping patterns may not even register in Go's `ServeMux`.
9. **Side effects into other domains** (X10). Agent writes produce notifications and task rows. With auto-backup on, every change to a non-runtime Agent field uploads a backup — registration, switches, and rule-sync/DNS-follow/HA state included. That contradicts the original's "heartbeat/monitoring never backs up", which is only true for runtime fields.
10. **Corrupted source facts.** Two H conditions had their literals replaced by documentation addresses (`172.16.0.0/12` and `255.255.255.255` became `192.0.2.10…`), which changed what they test. Four user-visible messages had their example addresses replaced. All are restored. Re-registration resets `enabled` and drops unlisted fields (the REG code said switches are kept).

Smaller additions: version gates with their boundaries and `compare_versions` quirks (X09); HEAD running GET handlers that call Agents or write state, plus `send_file` conditional and Range headers (X11); concurrency and the `flock` preparation lock across `kill -9` (X12); address-classifier boundaries that the TEST-NET fixtures never reach (X14); credential replay (T25); restart in the middle of each flow (T26).

### Counts

Per-route additions (105 cases on 50 routes, all tagged `[O6]`, numbered S/F/B 90+ so existing numbers stay stable):

| Category | Added | Main items |
|---|---|---|
| Authentication / permissions | 39 | profile-before-auth F90/F91 on R104–R120 (34); R079 require_auth messages and auth-off admin poll (2); R102 registration key with installation capability (1); R010 duplicate-token and non-ASCII query token (2) |
| HTTP shell, routing and headers | 12 | HEAD with side effects (R010, R013, R042, R058); routing fall-through (R044, R047, R052, R098); `%2F`/`..` in `<path:resource>` (R042); `send_file` conditional/Range and HEAD (R087 ×2); profile header ignored on agents routes (R004) |
| Input parsing and validation | 29 | `_read_json_body` order, Content-Type, BOM/UTF-16, duplicate keys, deep nesting, int vs float (R026 ×5, R072 ×2, R074 ×2); `request.json` 415 caught as 500 (R002, R005); upgrade report HTML 400/415 and chunked (R080 ×3); enroll empty-body differences (R085 ×2, R093); profile errors inside handlers (R086 ×2); lax `type=int`/`int()`/`str_to_bool` (R100 ×3, R083 ×2, R084 ×2); registration force-JSON, nesting, untyped fields (R102 ×3) |
| Panel URL and client IP | 9 | Host/XFH/XFP/body base_url per route (R010, R023, R050, R063, R100); registration host substitution (R102); server_url validation and mismatch (R091 ×2); Nginx body limit (R072) |
| Side effects and in-memory state | 10 | freshness-based status and ELF artifact detection (R001 ×2); delete clean-up (R003); auto-backup on Agent writes (R005, R026); concurrent push and 30-record cap (R063 ×2); re-registration field reset and rejection notification (R102 ×2); concurrent gateway publish (R095) |
| Redaction and generated outputs | 6 | R002 redacted token; R034 log line redaction; R065 piecewise redaction; R079 raw upgrade record; R085 token exemption; R001 crash from an untyped version |

Cross-cutting additions: **17** — X01–X15, T25, T26.

Corrections: **17**, touching 20 rows or paragraphs.

| What | Count |
|---|---|
| Comparison/environment rules: title status; Python start mode (waitress, `python -m backend.app`); A1–A7 two message sets; P1–P5 global blueprint and profile-before-auth; backup rule in the write-code introduction | 5 |
| Write codes: REG re-registration resets `enabled` and drops fields; HEARTBEAT version commit without backup | 2 |
| Route rows: R002 S01 token is redacted; R100 F05 non-numeric port is not rejected; R080 F11 bad JSON and wrong Content-Type are Werkzeug HTML; R102 S05 host substitution rule | 4 |
| H rows: H0003 and H0197 conditions corrupted by address substitution; H0359, H0574, H0895, H0973 messages restored (1 entry, 4 rows); the substitution rule in the H introduction | 4 |
| Frontend/lab notes: E01/E02 anonymous 404 and dead frontend wrappers; L2 Surge has no lab host | 2 |

Totals after review: 3620 original entries + 105 per-route cases + 17 cross-cutting items = **3742** named entries (design counts, not request counts).

### Link spot check

- 40 randomly sampled H rows (seeded): all 40 point at the cited guard. The four my script could not match automatically were checked by hand, and two of them are the corrupted literals above.
- 20 randomly sampled `[Lnnn]` links in the route table: all 20 point at the cited return or raise.
- All 349 distinct links in the fixture, write-code, W, G and T sections were printed and read: 346 point at the cited definition or branch; 3 were off by one to four lines (`profile_context.py:52`, `config_repository.py:1006`, `deployment_bundle.py:39`) and are fixed.
- Every link added by this review was checked the same way.

### Not testable by HTTP differential, and where it goes instead

| Item | Goes to |
|---|---|
| Real service lifecycle, binaries on real architectures, Compose/Docker volumes, upgrade worker and power loss, DHCP renumbering (address follow) | **L1** |
| A core really loading a published bundle, reload or restart, DNS health checks, real rollback | **L2** |
| Real rule hot-reload, DNS follow without resolution gaps, keepalived VRRP and VIP failover on two hosts | **L3** |
| ip rule/route, nft/iptables, sysctl persistence over reboot, TUN capture, IPv6/PD end-to-end against a lab RouterOS | **L4** |
| Nginx-plus-Flask composite (X15): body limits, chunked buffering, Host/forwarded headers, 300 s proxy timeout | container test on .32 with the production `nginx.conf` in front of Python; each difference between bare Flask and the composite goes to the **O12** ledger with Go's choice |
| `flock` preparation lock across processes and after `kill -9`; scheduler idle rounds; restart in the middle of a flow (T26) | fault injection on .32 against each server separately, plus Go unit tests for the lock semantics |
| Credential model (static Agent tokens, no expiry or replay protection), trusting `X-Forwarded-*` and body `base_url`, user input in generated install scripts and Compose files | **O9** security review (behaviour must still match Python here; any change goes to O12) |
| Thread exhaustion under long requests, memory of in-process histories | **O10** (soak in **L6**) |
| Legacy `runtime.sqlite3` import, `LEGACY_DIRS`, the `node_test_dns_cleanup` migration | **O12** ledger if Go does not port them (only `node_test_dns_cleanup` and `LEGACY_DIRS` are recorded maintainer decisions) |
| `LATEST_AGENT_VERSION` equal to the shipped Agent binaries; JWT secret continuity; TZ and `last_heartbeat` format at the switch | **O14** release and switch plan, **L6** rehearsal |
| Surge devices (macOS/iOS only) | fake only; the maintainer confirms on their own device during **O14** |
| Agent-side streaming (Mihomo log stream, query sources) | not part of the panel protocol; real-Agent integration in **L2** |

### Notes for Step B

- Start Python only with `python -m backend.app` under waitress, then send one authenticated, side-effect-free request so its rule-sync loop starts (X08). Give both servers the same `TZ`, `JWT_SECRET_KEY`, `ADMIN_*`, `AGENT_REGISTRATION_KEY`, `AGENTS_STATIC_DIR` (with synthetic ELF-magic files for the three architectures), `CONFIGFLOW_HTTP_THREADS`, `LOG_FILE` and working directory.
- One fake Agent set per server, in separate network namespaces at identical addresses; keep every fake heartbeating both while a case runs and in any case longer than about 100 s (X07, X13).
- Keep `auto_backup` off except in the X10 fixture.
- Run the D-type provider cases (R109/R110/R119/R120) with login on, with login off plus a config token, and with both off.
- When a fixture needs a boundary address, use the X14 list, not only the TEST-NET ranges.
- Before reporting a difference that involves timing, check X07 (heartbeat age) and X08 (which loop runs, and when).
