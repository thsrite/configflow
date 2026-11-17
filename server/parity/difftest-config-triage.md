# Config Step B：67 条 open 分诊

分诊基线：`go@97580425b`；原始证据为 [results.json](../tools/configdiff/testdata/results.json) 的 `finding_reviews[status=open]`，共 **67 个唯一变体**，最后实跑基线均为 `a08c4cc27`、测试服务器 .64。完整响应/SQL/文件差异保存在该 JSON 和 [findings](difftest-config-findings.md)。本轮仅阅读源码、既有合成证据和在途分支，不重跑业务测试，不修改业务、夹具、原始 verdict 或 accepted/closed 状态。阅读 Go 业务源码是本次分诊的明确要求，发生在 Step B 盲测交回之后。

严重度标准：

- **P1**：可突破既定安全/隔离边界，或直接破坏发布、网络和数据完整性，需要优先处置。
- **P2**：正常功能、错误分类、修订号/冲突判断或必要诊断不正确；也包括会误派业务缺陷的关键验收前提问题。
- **P3**：未证明功能失败的冗余数据、协议实现形式或比较器过严，允许随所属模块收尾。

判定严格采用四类：**Go bug / Go 更正确 / 两边都不对 / 测试夹具问题**。`Go 更正确`需要证明 Python 的缺陷或安全问题，不能把 Go 的非 200 本身当作证据。本批唯一变体分为 **Go bug 49、测试夹具问题 17、两边都不对 1、Go 更正确 0**；原先257条 accepted 不在本次范围。这里是派单建议，行为变化仍需协调者批准，不能据此直接把原始 finding 关闭。

合并为 **11 张工单**。H133 两个变体同时含“警告丢失”和“原文改写”两个根因，分别进入 CFG-02/CFG-03，因此工单关联数为69，唯一变体仍是67。附录逐行列出全部身份、判定和实际输出摘要；每行继承所指工单的源码依据、owner、状态及验收要求。

## Owner 状态与在途工作核对

模块编号依据 [FILES](FILES.md)、[config 模块表及正文](PROGRESS-config.md)、[agents 模块表](PROGRESS-agents.md)。旧模块表与后文有冲突时用更新的独立复核正文；协调者此次提供的在途状态优先于旧表。

| owner | 本次采用状态 | 派单注意 |
|---|---|---|
| C14 远端规则/内容 | **verified**；共享 reader 的 N7-3d 仍处复核/合并验收管线 | CFG-01/03/10。不能把 C14 已 verified 当作没有回归。 |
| C32 规则路由 | **verified**，[正文](PROGRESS-config.md#c32--rule-routes)覆盖21条路由；旧表仍写 missing，已过期 | CFG-02/11，回交 C32 修复/复核，不派给 C11。 |
| C28 MosDNS 路由 | **verified** | CFG-09；协作 C14 错误类型、C15 refresh、C02 taskstatus。 |
| C35 WebDAV | **verified** | CFG-05 是 difftest 比较器工单，不要求 WebDAV 业务改回空 XML。 |
| C37 域名发现 | **verified** | CFG-06 第三种行为待批准，修改范围应在 caller 去重。 |
| C07 logs/tasks/notifications/listener | **在移植中**（主线 partial）；`95eb529e` / `go-port-c07`，检查时分支头 `d38311548` | CFG-07 交现有 agent；该分支的 logs 改动是 clear 的 owner/取消处理，未修复 exists/EACCES。 |
| C33 规则库/策略组路由 | **合并验证中**；主线旧表 missing 与本快照尚未注册路由一致，但不能据此另派一次完整移植 | CFG-08 交现有 C33 合并验证加 difftest 维护；`go-port-c33@a4445071c` / `go-review-c33@0ed0be176` 已有公共 content 路由。 |
| A17 DNS follow | **还没移植**（主线 missing） | CFG-04 是不完整 seed/未实现后台依赖的测试工单，不派成 C32 或 C07 回归。 |

在途交叉核对：

- **C06/C11**：`a9cc8a3a` / `go-review-c06-c11`，检查时分支头 `5c014b1f6`。C06 复核已明确 listener 实现归 C07；本批没有可由 C06/C11 统一吸收的工单。特别是 logs EACCES 属 C07，local-rule delivery warning 属 C32。
- **O3-6c**：`6e5c7760` / `go-o3-6c@217805649`，在途。它改了 repo/store、refresh/taskstatus、rulefetch/cache 等生命周期边界，CFG-09/11 修复后需同该分支联验；本分诊没有把生命周期改动当作这两类问题已修复的证据。
- **N7-3d 复核**：`a5567935` / `go-review-n7-3d@cbce7eaa8`。[该分支 `server/parity/review-n7-3d.md` F5，约第38行]明确是 **A12 push-diff 的 `rule_entries` 展示逐行扫描**；另有 matcher 的原因链修复。其 diff 修改 `deploy/pushdiff/lists.go`、`rules/text.go`、`rules/matcher.go`，**没有修改**本批入口 `api/rules/read_adapter.go` 或 `rulefetch/content.go`。因此不是 H128–H133 的现成修复。分支报告自称实现复核6项全闭、相关验证通过、完整 gate 因排队截止未运行；主线仍不能标 verified。CFG-03 可请该复核者协作物理行/原文保真，但 CFG-02 的丢弃 warning 必须由 C32 修复。

<a id="cfg-01"></a>

## CFG-01 · 原始内部 API URL 经 dot-normalization 后丢失 redirect 限制

**Go bug · P1 · owner C14（verified）；4变体。** 接口：`GET /api/mosdns/rule-proxy` 及 profile 别名；对应附录01–04。

- **实际证据**：合成请求为配置的 server origin 上 `/api/../redirect`，第一跳经传输规范化到 `/redirect`，返回指向跨 origin 或同 origin 非 `/api/` 的302。Python **400** `Invalid remote URL`，被禁止的目标请求数 **0**；Go **200**，被禁止的目标请求数 **1**。目标都是 loopback fake；这不是生产 SSRF 已被利用的声明。
- **Python**：[rule_fetch.py:71](../../backend/utils/rule_fetch.py#L71)先据原始路径判内部；[139](../../backend/utils/rule_fetch.py#L139)记住每跳 internal，[180](../../backend/utils/rule_fetch.py#L180)拒绝离开同 origin API 边界。[local_http.py:109](../../backend/utils/local_http.py#L109)还在请求准备后重查 API 前缀，不把规范化后的非 API 路径当本地 handler。
- **Go**：[urls.go:12](../internal/rulefetch/urls.go#L12)在分类前用 `weburl.Prepare` 消除 dot segment；[fetch.go:139](../internal/rulefetch/fetch.go#L139)因此把 `/redirect` 判为 external；[305](../internal/rulefetch/fetch.go#L305)、[369](../internal/rulefetch/fetch.go#L369)后者只对 internal 分支做 redirect 限制。复现夹具：[rule_fetch_edges.py:75](../tools/configdiff/cases/rule_fetch_edges.py#L75)。
- **派单/验收**：保持 native URL 解析，保留原始内部分类引出的安全限制，或更早拒绝这类歧义路径；两种目标、两个别名均不得访问目标，正常外部源/合法内部重定向仍可用。不能为 parity 仿造 Python URL parser。**本批方向与任务消息的举例相反：是 Python 拒绝、Go 放行，不能登记为“Go 更安全”。**

<a id="cfg-02"></a>

## CFG-02 · Mihomo 本地规则交付丢弃人工审阅 warning

**Go bug · P2 · owner C32（verified）；22变体。** H128–H133，两个 local-rule 前缀；附录05–26，其中H133再关联CFG-03。

- **实际证据**：两端200。Python每次有1条 WARNING，消息为 `Rule '<合成规则名>': 1 entries need manual review (lines [0])`；Go未观察到该条。不能扩大为“Go没有任何日志”。20条的返回原文一致；另2条H133还有原文差异。
- **Python**：[rules.py:723](../../backend/routes/rules.py#L723)调用归一化、保留响应原文；[735](../../backend/routes/rules.py#L735)对返回的 warnings 记录数量和行号。各拒绝形态来源：[ruleset_content.py:227](../../backend/utils/ruleset_content.py#L227)。
- **Go**：[read_adapter.go:157](../internal/api/rules/read_adapter.go#L157)调用同一交付流程，但[162](../internal/api/rules/read_adapter.go#L162)把返回的 warnings 接成 `_`。底层[content.go:239](../internal/rulefetch/content.go#L239)已在拒绝时返回 line=0 的 warning。故至少多数形态不是 reader 没产 warning，而是 route 丢弃它；H133底层未判拒绝另见CFG-03。
- **派单/验收**：恢复一次且仅一次的可审计警告，保留规则名/数量/行号及日志脱敏；22向量逐一重跑。若要把旧英文消息改成集中定义的中文消息，另列明确行为变化，不能悄悄删掉警告。原始比较器：[rule_delivery_yaml.py:12](../tools/configdiff/cases/rule_delivery_yaml.py#L12)、[45](../tools/configdiff/cases/rule_delivery_yaml.py#L45)。

<a id="cfg-03"></a>

## CFG-03 · 物理跨行的 flow scalar 被当成单行后改写原文

**Go bug · P2 · owner C14（verified），协作 N7-3d 复核；2变体，与CFG-02交叉。** H133两个前缀。

- **实际证据**：原文 `{payload: [+.example.com], extra: "first\n second"}\n`。Python200并逐字返回原文；Go200把它重新输出为block YAML，`extra`值折叠为一行。此处是已有规则的交付保真，并非新生成配置允许的 N7-4 layout 变化。
- **Python**：[ruleset_content.py:263](../../backend/utils/ruleset_content.py#L263)递归检查所有mapping，尤其[275](../../backend/utils/ruleset_content.py#L275)使用原始start/end行跨度拒绝物理跨行，即使解析后的值已没有换行。
- **Go**：[content.go:273](../internal/rulefetch/content.go#L273)检查node style和解码后 `Value` 内是否有换行，[291](../internal/rulefetch/content.go#L291)再调用editor；被折叠的 quoted scalar 不满足该拒绝条件，因此原文字节丢失。
- **派单/验收**：用已有原生词法/源跨度能力判断物理跨行，拒绝有歧义的改写并保留原文，不复制PyYAML。两个H133先恢复原文字节，再由CFG-02补足警告；单独修任一票不能关闭整个mixed finding。N7-3d F5 的 A12展示扫描不经过这个入口，不能直接视作覆盖。

<a id="cfg-04"></a>

## CFG-04 · 不完整 DNS-follow seed 与未实现后台模块被算成规则接口回归

**测试夹具问题 · P2 · owner A17（还没移植），协作 configdiff/C32；2变体。** R188-S01、H911。

- **实际证据**：GET两端200、拒绝绑定的PUT两端400且业务消息一致。差异已在第一个请求前存在：Python写入 `dns_follow_state.status=failed`、task-status、通知，以及system revision/generation；Go没有。每端请求前后 `Stable` 证明自身未写入，不能称GET导致写入。
- **Python/seed**：[rule_dns_edges.py:134](../tools/configdiff/cases/rule_dns_edges.py#L134)仅种 `dns_follow={upstream_tag, upstream_index}`，缺完整绑定身份/源Agent等生命周期前提；[agent_manager.py:18](../../backend/common/agent_manager.py#L18)启动DNS-follow，[dns_follow.py:681](../../backend/agents/dns_follow.py#L681)将非预期异常转换成通用failed状态。真实运行的失败记录不等于这个不完整seed是合法联动流程。
- **Go**：A17没有完整实现；现有[agents/hooks.go:247](../internal/agents/hooks.go#L247)只声明初始化hook，[singleton.go:62](../internal/agents/singleton.go#L62)调用已注册hooks；[server.go:81](../internal/server/server.go#L81)没有DNS-follow owner接线。不是C32已实现read/guard错误，也不是通知模块单独漏通知。
- **派单/验收**：拆开规则catalog/已绑定guard和DNS-follow启动联验。前者使用契约完整seed；后者声明A17未实现依赖，接通后再以完整binding和fake Agent做成功/失败/重试断言。保留每端只读检查；不得全局忽略agents、通知、task-status或revision，也不得要求Go复制不完整seed触发的Python异常。

<a id="cfg-05"></a>

## CFG-05 · PROPFIND 空正文与显式 allprop 被作字节差异

**测试夹具问题 · P3 · owner C35（verified），执行由configdiff维护；10变体。** R016的常规及本地日界保留用例。

- **实际证据**：两端200；差异仅同一个PROPFIND请求正文：Python空，Go `<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>`。认证、请求路径/顺序、上传、DELETE保留集及SQL/files其余比较通过。
- **Python**：[webdav_backup.py:75](../../backend/utils/webdav_backup.py#L75)使用webdav3 Client，[141](../../backend/utils/webdav_backup.py#L141)调用 `client.list`，既有fake transcript记录其空正文；repo自身不手写其XML。
- **Go**：[webdav/client.go:192](../internal/webdav/client.go#L192)显式发送allprop。[RFC4918 §9.1](https://www.rfc-editor.org/rfc/rfc4918#section-9.1)要求服务端把空PROPFIND正文视作allprop，故没有证据说明Go需改成Python字节形式。
- **派单/验收**：只对相同PROPFIND语义把这两种形态比较为等价；继续严格比较Depth、URL、认证、返回资源、清理集合和其他XML属性。负控必须拒绝propname/prop选择、错误namespace和非法XML。真实非兼容DAV服务器另做联测，不能从本fake推断所有服务器都兼容。

<a id="cfg-06"></a>

## CFG-06 · 重复 targets 的初始化在两端都产生冗余资源

**两边都不对 · P3 · owner C37（verified）；1变体。** R056-S01 `duplicate-targets/0`。

- **实际证据**：两端200；Go额外生成空的by-id `.list.bak`（长度0、mode0600），Python没有。除此之外既有DB/响应比较相同。
- **Python**：[domain_discovery_service.py:255](../../backend/utils/domain_discovery_service.py#L255)按原始targets逐个append库项，但 `created[target]` 覆盖前项；[272](../../backend/utils/domain_discovery_service.py#L272)仅对dict值保存一次，[279](../../backend/utils/domain_discovery_service.py#L279)又按原始targets插入重复引用。
- **Go**：[discovery/settings.go:192](../internal/discovery/settings.go#L192)同样重复创建并覆盖 `created[t]`，[207](../internal/discovery/settings.go#L207)却按targets重复保存最后一个item，生成额外备份；[221](../internal/discovery/settings.go#L221)也重复插入引用。缓存保存入口[bridge.go:121](../internal/discovery/bridge.go#L121)。
- **判定依据**：额外空备份是报告实际观察；两端共同的孤立库项/重复引用是上述源码的直接推导，本轮没有新增运行证明其用户影响。只令Go少写一次文件仍会保留共同缺陷。
- **第三种行为/验收**：建议进入事务前对targets保持首次顺序去重，200仅创建一套direct/proxy库项、引用及缓存，无孤立项和空备份；也可由协调者选择明确400拒绝重复，但需批准后登记行为变化，并更新参考期望。不要把本票简化成在Go复制Python的冗余DB结果。

<a id="cfg-07"></a>

## CFG-07 · logs/info 把 EACCES 当成文件不存在

**Go bug · P2 · owner C07（在移植中，95eb529e）；1变体。** R075-F08。

- **实际证据**：相同的不可遍历私有日志目录，数据库仍可读；Python500 `success=false`、error为指定路径的Permission denied，Go200 `success=true, exists=false, path=<指定路径>`，权限和指定路径的故障观察也失败。
- **Python**：[logs.py:27](../../backend/routes/logs.py#L27)的Path.exists遇权限错误会传播，[125](../../backend/routes/logs.py#L125)/[147](../../backend/routes/logs.py#L147)转500。
- **Go**：[logs.go:36](../internal/api/logs/logs.go#L36)把所有Stat错误压成false；[55](../internal/api/logs/logs.go#L55)可能错误回退，[216](../internal/api/logs/logs.go#L216)又返回不存在的成功结果。不是stat本身没报错，而是错误被吞。
- **派单/验收**：用native `errors.Is(err, fs.ErrNotExist)`区分缺失和权限/其他I/O错误；保留正常fallback约定，EACCES返回失败，既定中文/错误出口与源行为核对。复用[log_stat_fault.py:56](../tools/configdiff/cases/log_stat_fault.py#L56)的非root真实故障，不用单纯chmod文件替代目录遍历拒绝。C07分支当前clearLogs的owner修正不含此处，挂现有任务而非另派C06。

<a id="cfg-08"></a>

## CFG-08 · C33 content 路由未合入，间接调用被误计成业务差异

**测试夹具问题 · P2 · owner C33（合并验证中），协作configdiff/C28；2变体。** R087/R138-B91 absolute-query。

- **实际证据**：相同origin、已引用的inline规则content URL加 `?x=1`；Python200 `full:inline.example`，Go502 `RULE_SOURCE_UNAVAILABLE`。未观察到“Go以安全规则明确拒绝query”的响应。
- **Python**：[mosdns.py:540](../../backend/routes/mosdns.py#L540)inline fast path不命中后走通用fetch，[rule_fetch.py:161](../../backend/utils/rule_fetch.py#L161)可转本地HTTP；[local_http.py:103](../../backend/utils/local_http.py#L103)使用新client和明确headers，不继承外层cookie/internal token。公共content入口[rule_library.py:302](../../backend/routes/rule_library.py#L302)本来就匿名，不是因query而获得额外权限。
- **Go**：[ruleproxy.go:147](../internal/mosdns/ruleproxy.go#L147)同样只禁用inline fast path；[fetch.go:314](../internal/rulefetch/fetch.go#L314)仍允许合法内部callback。[server.go:58](../internal/server/server.go#L58)此快照没有C33 Register；[ruleproxy.go:62](../internal/mosdns/ruleproxy.go#L62)把非2xx变HTTPError，再由[302](../internal/mosdns/ruleproxy.go#L302)转换502。**路由缺失→嵌套404→502是静态调用链推断，本轮没有另抓嵌套响应。**
- **在途Go位置**：`go-port-c33@a4445071c:server/internal/api/rulelibrary/routes.go:123` 已注册两个公共content前缀，`server/internal/server/server.go:65` 已接线，尚未在本证据基线出现。主线文档也有[明确C33 deferred](PROGRESS-config.md#c33--rule-library-and-proxy-group-routes)。
- **派单/验收**：先标识C33 manifest未接通的隐式依赖，不能当成已完成C28安全拒绝；C33合入后自动启用重跑这2例。若仍502再转为Go bug并定位callback，不能预先宣布关闭。覆盖无query/带query、两个prefix、profile边界和普通鉴权的反例，不为绿灯绕过middleware；**不把这2例批准为“Go拒绝Python安全漏洞”。**

**处理结果更新：closed（Go9c88c4390，.64实跑）。** C33合入后，原fixture/比较器不变，R087-B91与R138-B91的absolute-query均200/200，正文均严格为`full:inline.example`；完整响应/SQL/files及external fake零请求检查通过。所在组20变体全部compared、design_errors=[]，140.10秒exit0。归因C33内容路由及server接线提交4786d7f86；原502不再出现，无须登记“Go更正确”的行为变化。此结果仅更新CFG-08，前文67条分诊表和owner汇总保留当时的快照；最新证据见[修复进度第三批](progress/fix-cfg-harness.md#第三批c33合入后cfg-08最终复验)和findings。

<a id="cfg-09"></a>

## CFG-09 · 写 task receipt 的 rename 错误被当成非法 URL

**Go bug · P2 · owner C28（verified），协作 C14/C15/C02、O3-6c；2变体。** R087/R138-F16。

- **实际证据**：有效规则源已成功下载一次，但 `task-status.json` 是目录，无法被receipt原子替换。Python500 `Failed to process rule proxy request`；Go400 `Invalid remote URL`。上游请求次数无差异，不能称Go提前校验了坏URL。
- **Python**：[mosdns.py:552](../../backend/routes/mosdns.py#L552)触发refresh，存储异常由[592](../../backend/routes/mosdns.py#L592)通用处理返回500。故障输入[rule_proxy.py:273](../tools/configdiff/cases/rule_proxy.py#L273)真实创建目录冲突，无业务monkeypatch。
- **Go**：[refresh/scheduled.go:548](../internal/refresh/scheduled.go#L548)写成功receipt并传播错误；[taskstatus.go:305](../internal/taskstatus/taskstatus.go#L305)调用[131](../internal/taskstatus/taskstatus.go#L131)的 `os.Rename`。其native错误是 `*os.LinkError`；[ruleproxy.go:214](../internal/mosdns/ruleproxy.go#L214)只识别PathError等存储类型，未覆盖LinkError；[transport.go:71](../internal/rulefetch/transport.go#L71)对未识别错误兜底成ValueError，最终[ruleproxy.go:203](../internal/mosdns/ruleproxy.go#L203)返回400。该链路与实际下载成功后400一致，属于强静态定位，修复时仍应保留原始error类型回归。
- **派单/验收**：以native errors.Is/As或明确存储错误分类保留rename原因，不新增Python异常模拟；URL错误仍400，receipt目录/权限/rename I/O仍500，不把已下载缓存或事务结果强行假定全部回滚。O3-6c正改这些叶子持久化/取消边界，应联验，不能等其合入后未经复验就关闭。

<a id="cfg-10"></a>

## CFG-10 · CONNECT 版本/Host/User-Agent 被作固定 Python 线格式

**测试夹具问题 · P3 · owner C14（verified），执行由configdiff维护；3变体。** H123及R087/R138-S91。

- **实际证据**：H123两端最终502，只有CONNECT User-Agent为空 vs `Go-http-client/1.1`；S91两端最终200及镜像正文一致，CONNECT为Python HTTP/1.0、无Host/UA，Go HTTP/1.1、有目标authority对应Host/原生UA。其他调用顺序、结果及存储比较相同。
- **Python**：[rule_fetch.py:169](../../backend/utils/rule_fetch.py#L169)使用Requests session和代理；CONNECT由其依赖栈产生，不是业务手写协议。Go [transport.go:268](../internal/rulefetch/transport.go#L268)构造正常目标GET，[275](../internal/rulefetch/transport.go#L275)把proxy交给native transport；CONNECT由net/http生成，不等于外层GET的User-Agent约定。
- **依据/派单**：[RFC9110 §10.1.5](https://www.rfc-editor.org/rfc/rfc9110#section-10.1.5)的User-Agent描述的是实现标识，不要求不同客户端取同一值；迁移规则也要求使用Go原生HTTP栈。这里没有证明Go请求了不同目标、漏代理认证、绕过代理或改变最终结果，不应为了Python历史CONNECT字节形式新增仿真transport。
- **验收**：仅把这3个有限CONNECT向量的版本/实现头列作原生传输观察；继续检查authority/Host一致、TLS验证、代理认证、不直连回退、原站/镜像顺序与次数、响应/文件/SQL。保留错误目标/代理身份和额外请求的负控；如维护者要求写入行为变化台账，再追加精确声明，不能全局忽略HTTP头。

<a id="cfg-11"></a>

## CFG-11 · 规则写入链整配置回转导致未变化 DNS 字段 revision 被改

**Go bug · P2 · owner C32（verified），协作 C03/C04、O3-6c；20变体。** R180/R184及T25，含selectors/native与不存在ID。

- **实际证据**：普通场景Python的mosdns/oxidns字段revision仍1，Go变成5–11；其他profile选择有Python3→Go6，native已有值有Python2→Go5。字段解析值多数相同；T25仅mosdns 1→10且双端fake Agent零RPC。404用例仍有同样的积累差异，**不能据最终404这一请求就断言DELETE handler写了SQL**：它之前已有本组create/update等请求。
- **Python**：[rules.py:203](../../backend/routes/rules.py#L203)保留before/实际修改；[217](../../backend/routes/rules.py#L217)只清理引用。[config_store.py:315](../../backend/common/config_store.py#L315)按真实行position/text差异upsert，未改字段不改其revision；字段内键序也是存储约定。
- **Go**：[api/rules/adapter.go:68](../internal/api/rules/adapter.go#L68)把整个profile从有序legacy对象转成native map，再[86](../internal/api/rules/adapter.go#L86)完整转回并 `raw.Update(next)`；[service.go:194](../internal/api/rules/service.go#L194)又对两个DNS对象重设列表。下游[repo/model_rows.go:141](../internal/repo/model_rows.go#L141)只在JSON token字节未变时保留原始行。**完整map序列化造成键序/词法变化是最强静态候选，当前差异报告不保存原始SQLite value文本，未凭此宣称已逐请求证明唯一根因。**
- **派单/验收**：对每个流程找到第一个未变化字段revision发生变化的请求，记录该字段前后原始value字节、position、revision；在C32 adapter保留未改字段原始表示，只提交真正变动字段，保持DNS/native重建的原子性。不要全局让store忽略词法变化或给比较器屏蔽revision。404前后自身必须不写，成功清理确有变动的字段仍应递增。
- **mixed差异**：R180-B01/F08/F09、R184-F08另有HTML→JSON错误出口，已属Step B #3批准形式，不要求改回HTML；R184-S02/native的文本布局须单独按N7-4 typed-node等价核对，不能因伴随layout就忽略revision。20条都属于同一写入链排查票，而非20张DELETE票。

## 定向复验入口

修复/批准后使用标准configdiff runner与`native_network.sh`，在.32或.64冻结对应Go提交再运行。下表是 `run.py --groups` 的参数索引，**本轮只读分诊未执行这些命令**；CFG-04/08尤其应等依赖接通后再声称双端验收通过。

| 工单 | `--groups` 值 |
|---|---|
| CFG-01 | `rule-fetch-real-internal-redirect-escape-after-dot-normalization` |
| CFG-02、03 | `rule-delivery-yaml-helpers` |
| CFG-04 | `rule-dns-extra-guard-branches` |
| CFG-05 | `settings-webdav,webdav-local-day-retention-boundary` |
| CFG-06 | `discovery-init` |
| CFG-07 | `logs-real-stat-permission` |
| CFG-08 | `rule-proxy-local-cache` |
| CFG-09 | `rule-proxy-processing-failure` |
| CFG-10 | `rule-fetch-exhausted-candidates,rule-proxy-transport` |
| CFG-11 | `rules-sets,profile-rule-group-write-selectors,rule-dns-selectors-types,rule-dns-native-locked,rules-update,writes-real-online-Agent-no-push-or-activate` |

## 逐变体对账（67条，不修改原始 open）

下表的编号只用于分诊，不替代原清单case ID；同case不同variant/route仍各占一行。`Py/Go`为实跑HTTP状态。源码位置、owner状态、严重度和验收要求继承“工单”列；H133的第二工单用于其独立原文保真问题。

<!-- OPEN_VARIANTS_BEGIN -->
| # | 原case / variant | 路由 | Py/Go | 实际差异摘要 | 判定 | 工单 / owner / 严重度 |
|---:|---|---|---|---|---|---|
| 1 | `H120` / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin | `GET /api/mosdns/rule-proxy` | 400/200 | 禁止落点访问 Py0/Go1；400 Invalid remote URL / 200 text | Go bug | [CFG-01](#cfg-01) C14/P1 |
| 2 | `H120` / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api | `GET /api/mosdns/rule-proxy` | 400/200 | 禁止落点访问 Py0/Go1；400 Invalid remote URL / 200 text | Go bug | [CFG-01](#cfg-01) C14/P1 |
| 3 | `H120` / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 400/200 | 禁止落点访问 Py0/Go1；400 Invalid remote URL / 200 text | Go bug | [CFG-01](#cfg-01) C14/P1 |
| 4 | `H120` / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 400/200 | 禁止落点访问 Py0/Go1；400 Invalid remote URL / 200 text | Go bug | [CFG-01](#cfg-01) C14/P1 |
| 5 | `H128` / sequence-root | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 6 | `H128` / sequence-root | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 7 | `H129` / duplicate-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 8 | `H129` / mapping-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 9 | `H129` / missing-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 10 | `H129` / duplicate-payload | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 11 | `H129` / mapping-payload | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 12 | `H129` / missing-payload | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 13 | `H130` / alias | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 14 | `H130` / anchor | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 15 | `H130` / alias | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 16 | `H130` / anchor | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 17 | `H131` / block-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 18 | `H131` / nested-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 19 | `H131` / non-string-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 20 | `H131` / block-entry | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 21 | `H131` / nested-entry | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 22 | `H131` / non-string-entry | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 23 | `H132` / duplicate-extra-flow-key | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 24 | `H132` / duplicate-extra-flow-key | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0 | Go bug | [CFG-02](#cfg-02) C32/P2 |
| 25 | `H133` / multiline-extra-flow-value | `GET /api/profiles/<profile_id>/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0；原文字节也改变，另见CFG-03 | Go bug | [CFG-02](#cfg-02) C32/P2；[CFG-03](#cfg-03) C14/P2 |
| 26 | `H133` / multiline-extra-flow-value | `GET /api/rules/local/<name>` | 200/200 | manual-review WARNING Py1/Go0；原文字节也改变，另见CFG-03 | Go bug | [CFG-02](#cfg-02) C32/P2；[CFG-03](#cfg-03) C14/P2 |
| 27 | `H911` / actual-public-catalog-key-is-bound-to-DNS-follow | `PUT /api/rules/dns` | 400/400 | 请求结果一致；启动failed状态/task-status/通知 Py有/Go无；system revision/generation随之不同 | 测试夹具问题 | [CFG-04](#cfg-04) A17/P2 |
| 28 | `R188-S01` / real-DNS-follow-binding-catalog | `GET /api/rules/dns` | 200/200 | 请求结果一致；启动failed状态/task-status/通知 Py有/Go无；system revision/generation随之不同 | 测试夹具问题 | [CFG-04](#cfg-04) A17/P2 |
| 29 | `R016-B02` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 30 | `R016-B03` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 31 | `R016-B90` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 32 | `R016-B91` / Asia/Shanghai/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 33 | `R016-B91` / Etc/GMT+12/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 34 | `R016-B91` / Etc/GMT-14/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 35 | `R016-F16` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 36 | `R016-S01` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 37 | `R016-S02` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 38 | `R016-S03` / ∅ | `POST /api/backup/now` | 200/200 | PROPFIND body Py空/Go显式allprop；其余比较相同 | 测试夹具问题 | [CFG-05](#cfg-05) C35/P3 |
| 39 | `R056-S01` / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | 200/200 | Go多一个0-byte、0600的by-id .list.bak；源码另见共同重复创建/引用问题 | 两边都不对 | [CFG-06](#cfg-06) C37/P3 |
| 40 | `R075-F08` / real-stat-EACCES-with-database-still-readable | `GET /api/logs/info` | 500/200 | Py success=false + Permission denied；Go success=true + exists=false | Go bug | [CFG-07](#cfg-07) C07/P2 |
| 41 | `R087-B91` / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | 200/502 | Py full:inline.example；Go RULE_SOURCE_UNAVAILABLE；主线C33路由缺失 | 测试夹具问题 | [CFG-08](#cfg-08) C33/P2 |
| 42 | `R138-B91` / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 200/502 | Py full:inline.example；Go RULE_SOURCE_UNAVAILABLE；主线C33路由缺失 | 测试夹具问题 | [CFG-08](#cfg-08) C33/P2 |
| 43 | `R087-F16` / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | 500/400 | receipt目录冲突；Py处理失败500 / Go Invalid remote URL 400 | Go bug | [CFG-09](#cfg-09) C28/P2 |
| 44 | `R138-F16` / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 500/400 | receipt目录冲突；Py处理失败500 / Go Invalid remote URL 400 | Go bug | [CFG-09](#cfg-09) C28/P2 |
| 45 | `H123` / original-and-mirror-both-fail-through-loopback-proxy | `GET /api/mosdns/rule-proxy` | 502/502 | CONNECT UA Py空/Go原生UA；最终状态/其余内容相同 | 测试夹具问题 | [CFG-10](#cfg-10) C14/P3 |
| 46 | `R087-S91` / ∅ | `GET /api/mosdns/rule-proxy` | 200/200 | CONNECT UA Py空/Go原生UA；版本1.0/1.1，Host缺失/目标authority；最终状态/其余内容相同 | 测试夹具问题 | [CFG-10](#cfg-10) C14/P3 |
| 47 | `R138-S91` / ∅ | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | 200/200 | CONNECT UA Py空/Go原生UA；版本1.0/1.1，Host缺失/目标authority；最终状态/其余内容相同 | 测试夹具问题 | [CFG-10](#cfg-10) C14/P3 |
| 48 | `R180-B01` / ∅ | `DELETE /api/rule-sets/<rule_set_id>` | 404/404 | default/mosdns revision 1/9；default/oxidns revision 1/9；另有已批准JSON错误壳 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 49 | `R180-B02` / P3/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 50 | `R180-B03` / P4/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 51 | `R180-B04` / P5/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | other/mosdns revision 3/6；other/oxidns revision 3/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 52 | `R180-B05` / P6/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 53 | `R180-F08` / ∅ | `DELETE /api/rule-sets/<rule_set_id>` | 404/404 | default/mosdns revision 1/9；default/oxidns revision 1/9；另有已批准JSON错误壳 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 54 | `R180-F09` / ∅ | `DELETE /api/rule-sets/<rule_set_id>` | 404/404 | default/mosdns revision 1/9；default/oxidns revision 1/9；另有已批准JSON错误壳 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 55 | `R180-S01` / ∅ | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | default/mosdns revision 1/9；default/oxidns revision 1/9 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 56 | `R180-S01` / mosdns-selector | `DELETE /api/rule-sets/<rule_set_id>` | 200/200 | default/oxidns revision 1/5 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 57 | `R184-B01` / ∅ | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/5；default/oxidns revision 2/5 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 58 | `R184-B02` / P3/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 59 | `R184-B03` / P4/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 60 | `R184-B04` / P5/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | 200/200 | other/mosdns revision 3/6；other/oxidns revision 3/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 61 | `R184-B05` / P6/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/6；default/oxidns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 62 | `R184-F08` / ∅ | `DELETE /api/rules/<rule_id>` | 404/404 | default/mosdns revision 1/11；default/oxidns revision 1/11；另有已批准JSON错误壳 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 63 | `R184-S01` / ∅ | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/11；default/oxidns revision 1/11 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 64 | `R184-S01` / dns-cleanup/False | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/6 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 65 | `R184-S01` / mosdns-selector | `DELETE /api/rules/<rule_id>` | 200/200 | default/oxidns revision 1/5 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 66 | `R184-S02` / dns-cleanup/True | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/7；另有native文本布局差异，独立核对 | Go bug | [CFG-11](#cfg-11) C32/P2 |
| 67 | `T25` / rules/operation-5 | `DELETE /api/rules/<rule_id>` | 200/200 | default/mosdns revision 1/10 | Go bug | [CFG-11](#cfg-11) C32/P2 |
<!-- OPEN_VARIANTS_END -->

## 按 owner 派单汇总

只计主责工单，不把协作模块重复加单。C14/C32等verified模块应按回归重开检查；C07/C33挂现有任务；A17为夹具/未实现依赖验收，不要求临时补一个空后台实现。

| 主责 owner | 状态 | 工单数 | 最高严重度 | 工单 |
|---|---|---:|---|---|
| C14 | verified；N7-3d仍在复核管线 | 3 | P1 | CFG-01、03、10 |
| C32 | verified | 2 | P2 | CFG-02、11 |
| C28 | verified | 1 | P2 | CFG-09 |
| C07 | 在移植中，95eb529e | 1 | P2 | CFG-07 |
| C33 | 合并验证中 | 1 | P2 | CFG-08 |
| A17 | 还没移植；测试依赖处理 | 1 | P2 | CFG-04 |
| C35 | verified；测试比较器处理 | 1 | P3 | CFG-05 |
| C37 | verified；第三种行为待批准 | 1 | P3 | CFG-06 |
| **合计** | **不含重复协作派单** | **11** | **P1** | **6张Go bug票、4张测试夹具票、1张两边都不对票** |

优先次序：先CFG-01；C32并行处理CFG-02/11，C14与N7复核者协作CFG-03；C07接CFG-07，C28/O3-6c接CFG-09；CFG-04/08先改验收前提并等依赖接通；CFG-05/10限定修比较器；CFG-06先裁定去重还是拒绝，再改行为台账/代码/期望。此文档未宣称任何工单已经修复或新近实跑关闭。
