# batch-7 之后的剩余工作盘点

## 口径与结论

本次只读源码、Git 和文档，不运行应用、测试、构建或门禁；只新增本文。
基线固定为 `go@03257978feaf11b368b69cef52726953583695fd`，batch-7 固定为
`bb6cb1b00001039868650b1c9f74a8c70497caad`。该 tip 比初次盘点的
`d80e7a24ac7b88840a0deb9e43c31302a97c89de` 只多集成报告/三份golden来源metadata，
Go源码和routes.tsv完全相同，故下列d80计数和路由差集也适用于bb6。后者尚未合入；下文“batch-7 后”
指这份树成功完成集成门禁并合入后的预期状态，不把候选分支通过当作 go 已通过。
范围以 [HANDOFF 最新节](../HANDOFF.md) 为准，历史“missing/待接线”须对照现码，不能直接相加。

主要余量：**MCP 6 个未注册入口；3 个已注册但成功功能延期的 artifact 入口；N8-8d、
N8-9 余下消费者组、N8-10、N8-11；共享 jsonstore 关机提交窗口；C37/A24 DB/cache
崩溃一致性；发行镜像、最终审计和 L2–L6 实机验收。C07 没有四条路由待移植。**

规模是规划估计，不是承诺工期：S ≤1 agent 日，M 1–2 日，L 3–4 日；L 可拆分。
共用同一 owner 的多条路由/观察项合并估计，不按 HTTP 动词重复累加。
“已合入”仅指指定 go 的 Git/文件证据；旧进度页未更新的验收描述另列，未重跑其测试。

## 1. 路由差集：入口缺失与成功路径缺失分开

### 1.1 全量集合与减法

对 `backend/routes/**/*.py` 的 Blueprint、import alias、route/get/post 装饰器做静态枚举，
再加 `routes.tsv` 中的 MCP。372 个 `.route` method/path，加 notifications 的 get/post
两项，得到 **374 个 backend/routes 入口**；TSV 再含 **6 个 MCP**，合计 **380**，
没有发现 Python 路由在 TSV 外另漏列。HEAD/OPTIONS 的框架隐式注册不另计；
显式 GET/POST/PUT/PATCH/DELETE 才是本表统计单位。

| 集合 | TSV 账面 | 实际接线核对 | 依赖/余量 |
| --- | --- | --- | --- |
| go 基线 | done322 / todo55 / wip3 | 另有 Linux gateway7 条已接线、artifact3 条已接线但503，因此已注册332、缺48 | 不是“58 条未注册” |
| batch-7 新补 | 42 条由 todo→done | A18 4、A22 7、A23 5、R15 21、R17 5；R14/R16 是无独立路由的 owner | 共依赖本批门禁，验收 S |
| batch-7 后 | done364 / todo13 / wip3 | **374 已注册；6 未注册；已注册中3条只有延期503** | MCP 共 L；artifact 共 M |

接线证据：`internal/server/server.go` 的 routePackages/lateRoutePackages；batch-7 同文件
新增 clientdevices、routers、ipv6，agents.Register 新增 dnsha/installation 子入口。
`internal/api/linuxgateway/routes.go:Register` 的循环展开7个旧入口，不能因 TSV 仍 todo
将它们误算成缺失；`internal/api/generate/artifacts.go` 的注册也不能冒充成功实现。

batch-7 实际补齐的模块如下，均非“有意不做”：

| 模块 | 方法数/范围 | batch-7 证据 | 本批后还需什么/大小 |
| --- | --- | --- | --- |
| A18 | 4：oxidns-ha GET/PUT/DELETE、oxidns-ha-environment GET | `8492fec8a`、`5dcf32b07`；agents/dnsha 与 api/agents | L3；产品移植不再排队 |
| A22 | 7：gateway-installations 列表/创建、会话状态/network/publish/script/enroll | `875f388c5`、`6df5d8aed`、`524ceec4a` | L4；路由优先级已集成 |
| A23 | 5：ipv6-readiness 状态、operation、verification-settings GET/PUT、verify | `59ceeb810`、`dd042046a`；api/ipv6 | L4 |
| R15 | 21：routers CRUD/probe/report/plan/apply/policy/工作区/routes/rule-sync/forwarding、Agent upstream-routers | `25ba18894`、`d59c21c46`、`220203172` | L5 |
| R17 | 5：client-devices GET/PUT/discovery、fixed-ip preview/apply | `fd6e6a6ea`、`2257fb540`、`69aaa3285` | L5 |

### 1.2 真正未注册：逐条列出

| 方法 | 路由 | 所属模块 | 为什么缺 | 依赖 | 规模 |
| --- | --- | --- | --- | --- | --- |
| POST | `/mcp` | MCP，旧 packages #39；不在78个 C/A/R 子模块内 | MCP transport/auth/invoker/tools 尚未移植；batch-7 无注册 | 全部被调用 API/资源操作、认证和 profile 规则稳定 | 共 L |
| POST | `/mcp/` | MCP #39 | 同上，尾斜杠兼容入口 | 同上 | 同一 L |
| GET | `/mcp` | MCP #39 | Python 显式405入口也须保留，不能用 Go 未匹配404替代 | POST transport 同批实现 | 同一 L |
| DELETE | `/mcp` | MCP #39 | 同上，无状态 transport 拒绝分支未移植 | 同上 | 同一 L |
| GET | `/mcp/` | MCP #39 | 同上 | 同上 | 同一 L |
| DELETE | `/mcp/` | MCP #39 | 同上 | 同上 | 同一 L |

源码：`backend/mcp_server/{server,auth,invoker,tools}.py`，server.py:136–169；
[packages.md #39](packages.md)。本批后没有其他已证实的“完全未注册”Python 路由。
这6条没有维护者批准的删除决定，必须移植；MCP 的工具业务也不能只实现一个空 transport。

### 1.3 已注册、成功路径仍缺：逐条列出

| 方法 | 路由 | owner / 状态 | 阻塞原因及需要补的工作 | 依赖 / 规模 |
| --- | --- | --- | --- | --- |
| POST | `/api/generate/<service>/artifact` | C34 入口 → A14 frozen release owner，wip | 合法服务仍固定503；接真实冻结候选生成、预览归属/过期、文件读取协议 | A14/A21、C34/configgen；三条共 M |
| POST | `/api/profiles/<profile_id>/generate/<service>/artifact` | C34 同上 | 同上，并保留 profile pinning | 同一 M |
| GET | `/api/generate/artifacts/<preview_id>/<action>` | C34 同上 | download/file 固定503；未知action仍404。需真实成功交付与权限/过期/路径校验 | 同一 M |

证据：base 与 batch-7 的 `internal/api/generate/artifacts.go` 都是延期占位，
[review-c34.md](review-c34.md) 第7项明确不宣称成功实现；A14 已合入不代表这三入口已接线。
batch-7 `proddiff-baseline.txt` **仍有34条精确 deferred→A14/A21**，全部属于 artifact
比较，指34个字段/状态差异而不是34条路由。接线后必须删除并确认 stale=0，不能保留豁免。

### 1.4 账面错标和有意不移植

- Linux gateway 的7个 `/api/custom-config/linux-gateway*` 方法已由 A20 注册；TSV todo
  是旧账。显式 profile 对应别名也在该 Register 中，C34 已覆盖分派。登记修正 S，依赖盘点/合并确认。
- 本次差集没有“有意不做”的 HTTP 路由。维护者不移植的是 pre-SQLite 自动迁移、Python
  runtime tuning、无调用代码和一次性 DNS 清理；C07 废弃3项 nginx 机制也不是删 API。
  这些决定保持，见 [packages.md Not ported](packages.md)；不能把 MCP 偷算进例外。

## 2. C07 的“9中5”到底欠哪四项

**这是汇总表过时，不是今天还缺4个 handler。**
[PROGRESS-config C07](PROGRESS-config.md#c07--logs-stats-tasks-notifications-web-listener)
正文明确“All nine routes now done”，`internal/server/server.go` 始终注册 tasks/notifications。
历史四项是：

| 原来未就绪的方法 | 今天入口状态 | 原阻塞/完成证据 | batch-7 后余量 |
| --- | --- | --- | --- |
| GET `/api/tasks` | 已注册；真实 slot 不齐时503，不能装空行 | C07 62129a294；基线只缺 R14 router_rule_sync | R14 真行到位后验收，不再是移植任务；S（与下行共用） |
| POST `/api/tasks/run` | 已注册；ready任务可运行、已知未移植503、未知400 | C07 owner/runner readiness；R14 提供真实 runner | 同上 |
| PUT `/api/tasks/interval` | 已注册；对应 ready owner 可提交，不能提交后再500 | C07 owned interval + display-order修复 a208172e1 | 无新增产品实现 |
| GET `/api/notifications` | 已注册；缺slot503；背景刷新不注销未观察到的旧通知 | ca6ca9b86；C07独立复核 | 同上 |

另5条原已做：GET `/api/logs/info`、GET `/api/logs/tail`、POST `/api/logs/clear`、
GET `/api/stats/overview`、POST `/api/notifications/read`；九条 TSV 当前均 done。

- A16/A17 已在 go 通过 agents.SetRuleSyncRow/SetDNSFollowRow 提供 agents 槽。
  A04 metrics、C15、C37、C35 已有各自真实 owner，不存在四个未移植 C07 子包。
- batch-7 `routers/rule_sync_tasks.go` 注册 SlotRouters rows/runner，`server` 接真实 R14。
  `a65fbccf4` 统一 R14/R15/R17 与 repo 的 RouterJobLock；没有另建假 slot。
- batch-7 已删 **11条 deferred→R14** 及 **12条 deferred→A18** baseline，基线分别还有11/12条。
  最终 compare/core 仍需协调者完成；不能把本盘点当门禁结果。
- 剩余是 **S 文档/验收登记**：修正 C07 summary 的 partial、5/9，以及仍说 queue900s 的历史
  全门禁备注；核对最终集成证据。C07正文的共享窗口大部已由 O3-6c解决，现仍实际开放的
  jsonstore Context 缺口是 N8-9 health F1，不能混成“C07四路由未做”。

## 3. N7/N8 状态与余量

按 [design/n7-n8.md §4](../design/n7-n8.md#4-buildable-steps) 的现有表逐项核对。
表里的 N8-9 写“6×S/M”，但实际枚举 **7组**，下表按7组展开，不丢 health/discovery。
已合入的证明优先使用本次 go 可达提交/现存文件；作者报告中的 rebase 前 hash 仅作历史。

### 3.1 基础步骤和 N8-8

| 步骤 | 状态 | 证据 | 尚欠 / 依赖 / 规模 |
| --- | --- | --- | --- |
| N7-1/2/3/4（N8前提） | 已合入 | yamlx native API；N7-2 `75e280b93`，N7-3/4各progress；pyyaml imports=0 | 老编辑器CRLF遗留已关闭；旧 API 删除不再单开实现任务 |
| N8-1 JSON bridge/response | 已合入 | `5535931dc`；httpx.writeJSON、canonjson | 旧 c.JSON* 使用者/最终删除仍归 N8-11 |
| N8-2 store raw rows | 已合入 | `76ecdf4df`、store/document.go；review-n8-2 | 无本步骤产品余量 |
| N8-3 model/codec | 已合入 | `fa237d77b`、model/record.go/document.go；review-n8-3 | 后续消费者守 R1–R9 |
| N8-4a shared resources | 已合入 | `a39922020`，model/resources.go | 无类型移植余量 |
| N8-4b groups/rules | 已合入 | `266ccc55b`，model/policies.go | 同上 |
| N8-4c profile settings | 已合入 | `0a67dfe7c`，model/settings.go | OxiDNS Dynamic 是设计选择，不算漏字段 |
| N8-4d system/backup | 已合入 | `ae2e0f59b`，model/system.go等类型 | 无类型移植余量 |
| N8-4e Agent journals | 已合入 | `b8a5f8360`、`1325af1cf`、`f147cf212` | HR字段/归属证明继续由消费者校验 |
| N8-4f router manifests | 已合入 | `2116844b0`、同上review | 同上 |
| N8-5a transactions/read/merge | 已合入 | `7670997b1`、`509eb407d`、`518cb9618`；repo/transaction.go/read.go | legacy facade待所有调用方迁移后删除 |
| N8-5b profiles/shared/library | 已合入 | `d8745f920`、`579573df8`、`ed300fec0` | 同上 |
| N8-5c guard/import/reset/system | 已合入 | `4e679dd99`–`0dfad03c6`；已删 compat_*.go/pyvalue.go | 不能把 repo/legacy.go 尚在误说成5c未移植 |
| N8-6 HTTP binding | 已合入 | `1aa4d2e76`–`64815165b`；httpx/bind.go/number.go | DTO逐调用方切换继续；旧动态入口归 N8-11 |
| N8-7 early consumers（两批） | 已合入 | `0189d6c1b`、`7ab70e1a3`；native tasks/notifications/stats等 | 相邻owner未typed前的薄legacy仍待删；共享ctx F1见§4 |
| N8-8a common/nodes/dialers/EasyTier | 已合入 | `7cb1b5aa2`、`47ef82ca8`、`aca7cc04d`、`b26b4af5f`；共享标量、provider修复与复核亦入batch-6 | 剩余单一legacy入口等消费者完成再删 |
| N8-8b Mihomo/env/downloads/base | 在 batch-7 | `bd8cdfc3b`、`c199cc0de`、`cb136c8b7`；review-n8-8b | S 批次验收；依赖8a/shared codecs，产品实现已交付 |
| N8-8c Surge/Loon | 在 batch-7；独立复核结案证据待核 | `712a37936`–`77bbb0212`；progress/n8-8c，pending_shared0 | M 独立复核/验收；依赖8a/8b；无旧的“两个正例skip”余量 |
| N8-8d provider delivery/response/hooks、MosDNS/OxiDNS exports | 未开始主迁移，已有native YAML/拓扑基础 | provider/{delivery,response,errors,hooks}.go 仍以jsonx为业务输入；5个非测试文件import | M；依赖8a/b/c、typed repo、N8-9 rules/subs/生成器接口，和consumer协调删除legacy |
| N8-11 delete gate | 未开始最终删除，前提部分完成 | jsonx/pystr仍存在；repo/legacy.go、httpx.Ctx.JSON/JSONSilent/JSONObject仍在 | 最终删除S；**前置迁移余量不是S**，依赖8d/9/10及所有外围调用方 |

进度文档仍有过时的“ported待review”或临时hash：N8-4a–d、N8-6等合入事实不等于已有
可追溯的新复核记录。O13需对齐验收登记；本次不凭旧措辞宣布已合入代码重新 missing。

### 3.2 N8-9 七组

| 组 | 状态 | 证据 | 下一步 / 依赖 / 规模 |
| --- | --- | --- | --- |
| subs/substore | 未开始 S pass（已建空任务分支） | `go-n8-9-subs@03257978f` 无实现增量；subs10/substore9个文件仍import jsonx | M：parser/filter/hosts/chains/cache/client/runtime typed；依赖repo、yamlx及provider/convert协议，替换health/discovery协作者 |
| rules/rulefetch/refresh/rulesetfmt | 部分完成 | rulesetfmt typed入口 `af957c225` 已入8a；其余规则/下载/调度仍8/8/6个文件import jsonx；`go-n8-9-rules`还在基线 | L（拆3×S/M）；完成typed规则API、fetch/refresh、剩余rulesetfmt调用方和legacy删除；保留缓存声明/ctx协议 |
| mosdns | 未开始 S pass（功能已移植） | mosdns仍6个文件import jsonx、5个import pystr；Node writer并不等于typed业务 | M；依赖rules/rulefetch、typed model/8d exports；`go-n8-9-dns`当前无增量 |
| oxidns adapter removal | 部分完成：原生内核已存在，typed adapter未删除 | 已完成S-oxidns/N7；oxidns/adapter.go仍接jsonx，3个文件import | M；依赖typed repo/8d、规则/生成器消费者；不能重写已验证的Node内核 |
| health(+check) | 在 batch-7，核心完成、跨模块F1明确未做 | `96777211a`、`a6850d650`、`0f595aab7`；各只有legacy文件import jsonx | S批次验收 + **M F1**：jsonstore最终Write/Remove ctx，并接discovery两adapter；其他旧协作者由subs/rules/agents迁移后删除 |
| discovery | 在 batch-7，核心完成、协作者迁移部分完成 | `10c34d4ba`、`a7dc8a3fe`、`9deb29e4e`、`cebf31b3f`；仅legacy.go import | M 独立复核证据待核；F1与health共M；legacy中的rules/rulefetch/health/Agent回调分别等相邻组/N8-10，不能今天删 |
| routersplit/routers | 部分完成：HR功能已typed，但S pass未完成 | routers4/routersplit3个文件仍import jsonx；routersplit仍pystr/PythonClass/dictGet | M；依赖rules/8d、A18/A23/R14–17集成和typed Agent；独立Opus HR复核 |

批次验收S不按行重复累计；F1是同一个跨模块任务。新建空分支不是“已经开始实现”的证据。

### 3.3 N8-10 五步

这五步的功能移植多数已 verified；这里盘点的是 **S pass**，不能把 A01/Axx 完成当成 N8-10 已完成。

| 步骤 | 状态 | 文件证据 | 依赖 / 下一步 / 规模 |
| --- | --- | --- | --- |
| agents core | 未开始主迁移，已有typed记录/部分native子包 | agents17个文件import jsonx；dnsfollow/dnsha/ipv6/rulesync/metrics/traffic还有adapter | M；repo model API、N8-9 health/discovery、A14/18/22/23稳定；保留live heartbeat overlay、凭据/部署证明 |
| monitor | 未开始 S pass | agents/monitor11个文件import jsonx，仍PythonClass | M；agents core、native localhttp/cache，typed状态/缓存/指标 |
| upgrades | 未开始 S pass | agents/upgrades6个文件import jsonx、1个pystr | M；agents core/installer/receipt协议；保持protocol-1和恢复语义 |
| installer | 未开始 S pass（YAML writer和L1修复已完成） | installer10个文件import jsonx、6个pystr，yaml_adapter.go | M；agents/core、typed settings、N8-8生成出口；迁移renderer/command/capability，不误删shell业务 |
| api/agents | 未开始主迁移，已有局部native DTO | api/agents10个文件import jsonx、4个pystr，content/client/public adapters | M；前四步、deploy、N8-9/8d、A24；切换c.Bind/typed服务并保留各场景HTTP状态/HEAD |

五步均要求Opus HR review；同时处理 deploy、gateway/setup、configexport、其他 api/* 等仍调用
legacy facade 的邻居，或明确分配补充consumer任务，不能仅清掉表中五包便宣布 N8-11 达标。

### 3.4 N8-11 当前删除门槛：按包计数

统计为**静态文本口径**，可复算：固定ref上 `git grep -I -n -E`，路径 `server/**/*.go`。
下表排除 `_test.go`、`/testdata/`、`/difftest/`；仍包括非 `_test.go` 的工具/测试helper，
故不称作生产二进制调用数。I=精确导入路径的文件数，Q=`\bjsonx\.`/`\bpystr\.`
文本出现次数；L-F/L-L=大小写不敏感 legacy 命中的文件/行数（含注释和字符串）。
**legacy协议/数据兼容本身不是应删的功能**，这些命中须判别，不是177个迁移adapter。

| 快照 | jsonx I / Q | pystr I / Q | legacy F / L |
| --- | --- | --- | --- |
| go 03257978f | 237 / 4690 | 36 / 124 | 160 / 906 |
| batch-7 d80e7a24a | 233 / 4320 | 30 / 95 | 177 / 1232 |

batch-7 新增薄适配器使 legacy 文本增加，不代表退回动态业务。需要最终删除的是例如
`convert/legacy.go`（含8b/c旧签名和窄投影）、`convert/mihomoenv/legacy.go`、
`provider` 动态层、`repo/legacy.go`、`health{,/check}/legacy.go`、`discovery/legacy.go`、
`rulesetfmt/legacy.go`、early-consumer兼容wrapper和httpx旧请求入口。

| 包（server/相对路径） | jsonx I | jsonx Q | pystr I | pystr Q | legacy F | legacy L |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| internal/agents | 17 | 530 | 0 | 0 | 8 | 54 |
| internal/agents/dnsfollow | 1 | 18 | 0 | 0 | 2 | 4 |
| internal/agents/dnsha | 1 | 22 | 0 | 0 | 1 | 1 |
| internal/agents/ipv6 | 1 | 8 | 0 | 0 | 2 | 10 |
| internal/agents/metrics | 6 | 124 | 0 | 1 | 2 | 44 |
| internal/agents/monitor | 11 | 302 | 0 | 0 | 2 | 2 |
| internal/agents/rulesync | 1 | 26 | 0 | 0 | 1 | 1 |
| internal/agents/trafficstore | 1 | 77 | 0 | 0 | 0 | 0 |
| internal/agents/upgrades | 6 | 187 | 1 | 1 | 1 | 3 |
| internal/aggregations | 1 | 33 | 0 | 0 | 1 | 10 |
| internal/api/agents | 10 | 149 | 4 | 16 | 5 | 9 |
| internal/api/aggregations | 1 | 14 | 0 | 0 | 1 | 8 |
| internal/api/auth | 1 | 2 | 0 | 0 | 0 | 0 |
| internal/api/clientdevices | 1 | 5 | 0 | 0 | 0 | 0 |
| internal/api/config | 2 | 25 | 0 | 0 | 1 | 3 |
| internal/api/generate | 1 | 3 | 0 | 0 | 0 | 0 |
| internal/api/linuxgateway | 0 | 0 | 0 | 0 | 1 | 1 |
| internal/api/mosdns | 1 | 21 | 0 | 0 | 1 | 1 |
| internal/api/nodes | 2 | 23 | 0 | 0 | 1 | 1 |
| internal/api/profiles | 1 | 28 | 0 | 0 | 1 | 2 |
| internal/api/proxygroups | 1 | 16 | 0 | 0 | 1 | 18 |
| internal/api/routers | 1 | 3 | 0 | 0 | 1 | 1 |
| internal/api/rulelibrary | 2 | 24 | 0 | 0 | 3 | 21 |
| internal/api/rules | 3 | 43 | 0 | 0 | 4 | 12 |
| internal/api/settings | 5 | 49 | 0 | 0 | 3 | 7 |
| internal/api/subscriptions | 1 | 18 | 0 | 0 | 1 | 27 |
| internal/api/tasks | 1 | 1 | 0 | 0 | 0 | 0 |
| internal/auth | 1 | 6 | 0 | 0 | 0 | 0 |
| internal/clients | 1 | 3 | 0 | 0 | 2 | 2 |
| internal/configexport | 1 | 6 | 0 | 0 | 1 | 2 |
| internal/configgen | 1 | 9 | 0 | 0 | 1 | 1 |
| internal/convert | 3 | 174 | 2 | 3 | 5 | 244 |
| internal/convert/mihomoenv | 1 | 4 | 0 | 0 | 1 | 1 |
| internal/deploy | 3 | 45 | 0 | 0 | 5 | 28 |
| internal/devicenotes | 0 | 0 | 0 | 0 | 2 | 6 |
| internal/discovery | 1 | 16 | 0 | 0 | 4 | 76 |
| internal/gateway | 3 | 36 | 0 | 0 | 1 | 1 |
| internal/gateway/draft | 1 | 12 | 0 | 0 | 2 | 30 |
| internal/gateway/host | 2 | 12 | 0 | 0 | 3 | 19 |
| internal/gateway/installation | 1 | 14 | 0 | 0 | 3 | 8 |
| internal/gateway/onboarding | 2 | 11 | 0 | 0 | 4 | 19 |
| internal/health | 1 | 59 | 0 | 0 | 2 | 58 |
| internal/health/check | 1 | 42 | 0 | 0 | 2 | 67 |
| internal/httpx | 5 | 17 | 0 | 0 | 3 | 6 |
| internal/httpx/httpxtest | 1 | 6 | 0 | 0 | 0 | 0 |
| internal/installer | 10 | 241 | 6 | 26 | 2 | 3 |
| internal/installer/installertest | 0 | 0 | 0 | 0 | 1 | 2 |
| internal/ippresence | 2 | 22 | 1 | 2 | 1 | 1 |
| internal/jsonresp | 0 | 0 | 0 | 0 | 2 | 3 |
| internal/jsonx | 0 | 3 | 0 | 0 | 1 | 5 |
| internal/model | 0 | 0 | 0 | 0 | 4 | 16 |
| internal/mosdns | 6 | 113 | 5 | 23 | 3 | 4 |
| internal/notifications | 1 | 25 | 0 | 0 | 3 | 30 |
| internal/oxidns | 3 | 17 | 0 | 0 | 10 | 41 |
| internal/provider | 5 | 105 | 1 | 2 | 1 | 1 |
| internal/pystr | 1 | 2 | 0 | 0 | 0 | 0 |
| internal/redact | 0 | 0 | 0 | 0 | 2 | 4 |
| internal/refresh | 6 | 120 | 1 | 1 | 0 | 0 |
| internal/repo | 18 | 427 | 1 | 1 | 13 | 147 |
| internal/routers | 4 | 22 | 0 | 0 | 19 | 66 |
| internal/routersplit | 3 | 51 | 1 | 1 | 1 | 25 |
| internal/rulefetch | 8 | 108 | 6 | 13 | 1 | 1 |
| internal/rules | 8 | 154 | 0 | 0 | 4 | 15 |
| internal/rulesetfmt | 1 | 4 | 0 | 0 | 2 | 5 |
| internal/setup | 10 | 45 | 0 | 0 | 8 | 17 |
| internal/setupflow | 1 | 4 | 0 | 0 | 0 | 0 |
| internal/subs | 10 | 415 | 0 | 0 | 2 | 5 |
| internal/substore | 9 | 117 | 1 | 5 | 2 | 2 |
| internal/tasks | 1 | 16 | 0 | 0 | 1 | 11 |
| internal/taskstatus | 1 | 7 | 0 | 0 | 3 | 6 |
| internal/webdav | 1 | 6 | 0 | 0 | 1 | 1 |
| internal/weblistener | 1 | 6 | 0 | 0 | 2 | 6 |
| internal/yamlbridge | 2 | 4 | 0 | 0 | 2 | 3 |
| internal/yamlx/yamlxtest | 7 | 29 | 0 | 0 | 0 | 0 |
| tools/faults/fixture | 1 | 12 | 0 | 0 | 0 | 0 |
| tools/proddiff/baseline | 1 | 4 | 0 | 0 | 0 | 0 |
| tools/proddiff/generator | 2 | 9 | 0 | 0 | 1 | 4 |
| tools/yamldiagnosticoracle | 1 | 9 | 0 | 0 | 0 | 0 |

测试/差分/夹具口径另有 jsonx **503个导入文件、8063个Q**，pystr **1个导入文件、1个Q**，
legacy **605个文本命中行**；不能只迁移产品入口，忽略测试/工具的最后依赖。
N8-11另外的grep阻塞（同一非测试口径，命中行，不是函数数）：PythonClass48、
raise(38、catch(12、dictGet(6；pyyaml imports0。逐包归属由上面的8/9/10和§4消费者任务负责。
N8-11的结束条件是实际依赖和仿真层清零、round-trip/HTTP/生成器门禁通过及Opus O7，
不是对 legacy 一词盲目做文本删除。

## 4. review / PROGRESS / HANDOFF 的后续与 open 项归并

只把仍有现实工作或必须裁定的项列为开放；过时的“等某模块”见4.2，避免重复派单。
来源是当前 go 所有 review-*.md、三份 PROGRESS、HANDOFF最新节，再叠加 batch-7 新增review/progress。
下表同一主题跨文档归并；“lab”不等于模块代码未 verified，O12也不等于要恢复Python bug。

### 4.1 仍开放或需最终验收

| 项目 | 证据/当前事实 | 需要做什么 | 依赖 | 规模 |
| --- | --- | --- | --- | --- |
| MCP #39 | routes.tsv6条todo；backend/mcp_server，无Go owner | transport/auth/tools/invoker及权限/profile/事务边界完整移植 | 路由域/资源API稳定、typed repo | L |
| C34 artifacts | artifacts.go固定503；34条deferred→A14/A21 | 真正冻结、保存/读取/下载；删除精确baseline；正常/错误/权限/存储验收 | A14/A21/C34，8b/c | M |
| 共享jsonstore Context F1 | batch-7 review-n8-9-health.md:50起 | WriteContext/RemoveContext；入口和rename/unlink检查；签名模式提交后验证完成；health三个ctx入口、discovery两adapter贯通；真实Runtime屏障 | batch-7 health/discovery；共享API协调 | M |
| C37/A24 DB→rule cache崩溃间隙 | review-c33.md:121；discovery/write.go先editLibrary再saveRule；api/agents/client_tools_adapter.go先事务再SaveRuleToLocal | 复用持久失效/发布协议，重启不交付过期cache；不是再加一次ctx.Err即可 | C33/rules/rulefetch、N8-9与N8-10协作 | M |
| N8-8c / N8-9 discovery独立复核 | 两份handoff仍写ready for review；本次未见各自独立review结案，8b及health有明确review报告 | 核对是否有未登记独立审阅；没有则由非作者复核，不能以批次gate代替 | 两分支实现已交付、协调者验收记录 | 各M（若已有证据则S补登记） |
| N8其余迁移/外围调用方 | §3和逐包表 | 完成8d、9、10，删除repo/httpx/early wrappers以及configgen/setup/gateway等邻接adapter | 相邻包无冲突、独立review | L（多任务，见各行） |
| O10重复解码/分配 | HANDOFF batch-6 e5性能修复；batch7-integration-zh.md | 128节点+512规则当前约76–77MB/次，老Go9.8MB；看pprof继续降重复decode，不以少输出/漏校验换性能 | 稳定batch-7、代表性负载 | M |
| A17 taskRow重复读取 | review-a17.md:132起：每行重读全部Agent | O10量化 O(任务行×Agent) 凭据收集，再决定一次snapshot传递 | C07 rows/typed Agent服务 | S |
| A22 public重复ExportAll | batch-7 review-a22.md:79起，每deployment额外两次全导出 | O10量化轮询分配，再缩窄/复用同代凭据快照 | A22、typed repo | S |
| A17 Step B差分覆盖 | review-a17.md:132–138：7个live场景未覆盖失败/重试/停用/rollback_failed | 非作者补黑盒场景；单元测试已有覆盖不等于此义务关闭 | A17/A18集成、difftest owner | M |
| O3-8 HR故障目录扩展 | progress/o3-8-faults.md末尾 | A14/A16–18/A22/R12/R14/R17设备场景扩展catalog；现16场景只覆盖通用故障，不冒称HR全覆盖；新增目录属后续扩展，不当作现16项验收缺失 | 这些owner和L2–L5 | M（可并入lab验收） |
| L1未覆盖/回归补验 | lab/L1.md、review-l1-installer.md:169起 | S4不开转发路径；真实Agent回滚/SSH断连/硬重启、host53 readiness、最小系统VIP、Compose2.20首次安装、过期链接；dash0.5.13有适用发行版再验 | 已修D1/D3–D8、真实实验guest | M |
| L1 D2 Agent自定义生命周期 | lab/L1.md D2；l1-installer-fixes明确排除 | service_manager=command新安装触发旧迁移失败，Agent bootstrap/migration owner修复并真安装验收 | Go Agent发布线（不是server迁移） | M |
| L1 D9 Agent文案 | lab/L1.md D9，Agent main.go:176仍显示OxiDNS，排除在installer修复外 | MosDNS validate-service错误显示OxiDNS，Agent owner改文案 | Agent发布/回归 | S |
| L1观察O1 | lab/L1.md：HA macvlan首次心跳后暂现409，调用方未定位 | L3配对时定位/验证，不据此猜测修改协议 | A18/L3 | S |
| L1观察O3/O4/O5/O6 | lab/L1.md观察节：同宿主macvlan地址池冲突、普通compose53占用、prepare_dns_ha无卸载菜单、公网地址探测 | O12/O9评估提示/风险；它们与Python相同且不是新移植失败。O2旧protocol终态空消息已知保留 | O9/O12，安装/Agent owner | S（裁定；改产品另估） |
| L2发布/包加载 | review-a11.md L2清单、A14设计 | 当前及老protocol真实core加载/reload、stage→ready→activate、失败回滚/recover、丢应答、AIO DNS、Geo权限/复用、Docker路径、进程死点 | A11/A14已verified，实验网 | L |
| L3规则同步/DNS联动/HA | review-a16/a17/a18；batch-7 lab/L3.md（计划，非执行报告） | provider/paths/旧Agent回退；监听变化；VIP迁移、poweroff、handoff丢应答/SIGTERM/kill9、pair一新一旧、真实DNS连续性 | batch-7 A18+go A16/A17、L2 | L |
| A18 O-1卡unknown | review-a18 O-1；design/a16-a18-sync-follow-ha.md:943起已覆盖旧“待决定” | **已决定，切换后实现**：journal unknown且成员离线≥10分钟，显式确认“强制停用”清journal/停HA；不是自动超时。列入O12，L3量化恢复 | 维护者已裁定；A18/UI/L3 | M（切换后，不冒称本批已修） |
| L4网关/IPv6 | review-a19/a21 + batch-7 review-a22/a23 | TUN/nft/route-address-set刷新、转发/RA持久化、安装capability/顺序发布、C36真实owned proofs；ROS客户端元组/fasttrack/PD变化/自动验收与clone锁 | A19–A23、R11、L2/L3、实验CHR | L |
| L5路由器 | review-r04…r17、PROGRESS-routers各L5清单 | 真固件格式/权限/批处理/ID/回读与补偿；NAT/policy/DNSguard/Netwatch/IPv6/PD；R12/14关机/基线，R15状态流程，R17租约固定/认领/混合批补偿 | R04–R17全部集成、隔离CHR+iKuai | L（按L5-A…等分批） |
| L6浸泡/切换演练 | WORKFLOW L6 | 24h RSS/goroutine/fd/内存稳定性及O14完整演练，形成真实结果 | O10、L2–L5和切换计划 | L（含24h观测） |
| O12行为变化发布说明 | behaviour-changes.md、各review/PROGRESS、O14-CHECKLIST.md与progress/o14-switch.md | 合并Python bug修正、严格类型/HEAD/凭据保护、writer/ID变化；D2使IPv6 verify报告/已忽略通知重置一次；维护者确认用户影响 | 完整功能树及N8固定输出 | M |
| 发布镜像/entrypoint #40 | packages #40 todo；docker/Dockerfile/supervisord仍Python面板链路 | Go server多架构发行、静态资源/Agent/探测组件、env/监听/关停、时区/CA、卷/备份/升级/拒旧目录验证；去掉Python/nginx运行依赖 | 功能完整、N8、O14；不能从Go Agent构建阶段推断Go面板镜像完成 | L |
| 最终源账本/验收同步 | C07表、Linux gateway7 todo、重复A/R summary、N8-4/6旧review措辞 | 合并后修表、清重复、核对独立复核证明；不要把历史“missing”重新派成产品移植 | batch-7门禁、O13；仅登记不是代跑测试 | S |
| O9/O13/O14/O15/O17 | WORKFLOW（下节） | 安全/完整性/发布/架构/体积审计仍须形成成果；不能用模块review替代 | 见§5 | 各见§5 |

### 4.2 历史后续：已关闭、已被迁移覆盖，或仅需核对证据

| 老记录 | 现在的处理/证据 | 剩余依赖 / 规模 |
| --- | --- | --- |
| C07 History计数、假slot、web listener、四条wip | C07正文/review已修，九route done；只剩本批真实R14 readiness验收 | §2，S登记 |
| C11旧CRLF/flow/tags/null-item注释 | progress/n7-2.md:73明确关闭；yamlx.Edit与N7-2复核已合入 | 无，不再排“CRLF重现Python坏输出” |
| C10/C14旧yamlx/PyYAML限制、C17 flattenMapping/两条allowlist | N7-3/4 native替换；C17正文已写closed | 无，新的N8消费者迁移见§3 |
| C05/C18/C19/C20 deferred S-convert | 8a已入go、8b/c在batch-7；仍有legacy facade等8d/9/10 | 不重复移植生成器；删除依赖见§3 |
| C18 controllerSecret helper等C36 | C36/A21已入go；8b原生base设置取代旧动态层 | O17检查死adapter即可，S |
| C29/C32/C33/C34等待O3-6c的ctx提交/ForgetContext | O3-6c和后续已入go，N8-5保留守卫；不重复当作C07/四模块open | 仍开放的F1/DB-cache间隙已独立列上表 |
| A13旧24条deferred→A14 PrepareFunc | A14已真实接线；本次baseline无这24条；余34条全部是另外的artifact deferral | 不能混同，artifact见§1.3 |
| A18 12条与R14 11条baseline | batch-7已删除；A18 R14真实owner都在 | 最终compare需0new/stale，S验收 |
| R14/R15/R17 JobLock与A22 route-priority复验 | `a65fbccf4`单一锁、`524ceec4a`真实注册，batch7-integration已列结果 | S批次门禁；非新代码欠账 |
| C36外部A21/A22/router owner | A21已接go；A22/R15/R17等在batch-7；d80e7a24a用真实owner重做abandon差分 | S批次验收；不能沿用“永远503”断言 |
| O3-8等batch-6 R14 M1再验 | `749f8257c`，batch7-integration:54、95起，16/16真实binary场景已复验，drain→stopJobs取消顺序有结果 | 不再派重复复验；最终整批core依然待协调者 |
| A01 proxy loopback初始差异 | PROGRESS-agents明确A07 review ce75e46a关闭 | 无；下一条redirect独立看待 |
| A01 redirect reqproxy.ForRedirect | PROGRESS-config:2422仍写应替换；agents/client.go仅NoRedirects分支，普通重定向未见对应hook | N8-10/O9确认行为与规则，S；不是把已关闭loopback问题重开 |
| C12 recursive-alias fallback/private urlsplit、EasyTier URL尾换行 | 8a原生StructuredProxy/EasyTier与N7 writer替换旧实现；仍保留的仿真由8d/9/10/11清理 | O17静态扫旧helper；S，非重做已批准边界 |
| C03旧测试flock race | N8-5/O3已改事务/锁测试；历史单次race不当作今天产品失败 | 合并门禁若再现才修；不凭旧日志报告新失败 |
| A05 traffic_compat私有IP/大小写helper | 该adapter尚在，PROGRESS-agents:498标TODO | 纳N8-10 core/metrics，S；不复制新的Python语义层 |
| A08/A09 URL尾换行、httpx Range、L1 D1/D3–D8 | 61f82de3、3adcdfbea及L1 installer复核已修；原L1报告“全待分派”是历史 | 只保留上表实机补验与D2/D9 |
| C35 importer bounds、C14 URL/reader强化 | review-c35后续已完成；安全强化交O9复查 | 无重复产品任务；O9范围不漏 |
| C37 CFG-06重复调用复用binding建议 | 单次重复target已修；跨调用分配新库仍按Python，复用只是Future decision | 切换后可选S设计+M实现；依赖定义重命名/删除/手改有效性 |

### 4.3 明确切换后/不在本轮parity的增强

| 项目 | 既有决定与范围 | 依赖 / 规模 |
| --- | --- | --- |
| Surge/Loon手工节点走Sub-Store | packages Follow-ups；现在保留支持协议/未知scheme丢弃行为，之后换Sub-Store并删内置parser | parity完成、Sub-Store能力/协议验收；M |
| C15精准timer替代30s轮询 | packages Follow-ups；next_run显示/触发时机有可见变化，不能N8偷偷改 | parity完成，lifecycle共享timer与配置唤醒；M。原文“keep flock”被O3单实例/lockx规则取代，不新加flock |
| A18 O-1强制停用 | 已裁定但明确after switch | 见4.1，共同M，不重复累计 |
| 新协议/CPython专属异常文本、pre-SQLite自动迁移 | 不属于补parity的任务；按维护者既定不移植/原生规则 | 无，除非维护者改变范围 |

## 5. WORKFLOW O9/O10/O12/O13/O14/O15/O16/O17 的触发与距离

以 [WORKFLOW完整任务表](../WORKFLOW.md#complete-opus-task-list-with-triggers-maintainer-2026-11-16--the-full-list-no-per-phase-asking)
为准，不根据早期HANDOFF“等维护者同意lab”重复请求批准；隔离L1–L6已被批准，生产切换仍须维护者决定。

| 项目 | 原触发条件 | batch-7 后距触发/真正阻塞 | 下一成果 / 依赖 | 规模 |
| --- | --- | --- | --- | --- |
| O9安全 | O3后；切换前完整树再审 | **首轮已可触发**：O3-1…6已合、7/8本批；最终审须含MCP/artifact/发行树。未见独立O9结案报告 | auth/session/CSRF、静态文件/路径、RouterOS/安装shell注入、脱敏、依赖、redirect；HR fresh Opus | L |
| O10性能/内存 | config+agents端到端；soak在lab | **batch-7合入后可开展主体**；现有128节点基准是局部证据，不是O10结案。24h部分仍等实验部署 | RSS/idle CPU、生成器与/api/agents分配、重复decode、goroutine/fd；L6执行soak | L |
| O12行为变化 | 切换前 | **汇总可立即开始**；终稿等N8/MCP/artifact语义和实机结论稳定 | behaviour-changes逐条用户影响/批准与发布说明；D2重置/全量重推、Python bug修正和after-switch项明确区分 | M |
| O13完整性 | 切换前，独立未移植过该代码的Opus | **现在可预审，尚不能clean**：MCP6、artifact3、#40与N8/外围清理；还需逐frontend调用、后台task、setting键审计。78模块verified≠全产品完成 | 路由/功能/任务/设置/工具/入口完整审计，修TSV/PROGRESS旧账；fresh Opus | L |
| O14切换/公开发行 | O13 clean | **已有局部清单，尚未触发最终验收**：O14-CHECKLIST.md、progress/o14-switch.md只记录模块切换影响；O13未clean，O9/O10/O12、L2–L6与发行镜像还须收敛 | NAS切换/stop-loss、只用Go全量重推、数据库/备份/legacy拒绝、多架构/时区/CA、他人升级文档；维护者决定实际切换时间 | M |
| O15架构文档 | 切换前 | **可先写框架**；当前及本批未见server/ARCHITECTURE.md，最终接口等N8-8d/9/10/11稳定 | 包/数据流/锁任务/路由生成器扩展指南；不沿用已删除adapter架构 | M |
| O16 L1–L6 | 各批前置verified | **L1做过，但有补验/D2/D9；L2现可排；L3/L4/L5本批合入后各前置到齐；L6等前述与O14草案**。只有L3计划文件，无L2–L6执行结案证据 | 各lab实测报告、故障结果与清理记录；Opus在授权隔离实验网执行 | 各批L；补验M |
| O17代码体积 | 切换前、O10后 | **最终审还等O10**；现在可先算行数。native core已瘦但legacy adapter仍在，直接Go/Python比值会混淆过渡双层 | 按替代模块统计产品行；>约2.5×由非作者Codex找重复/死adapter/过宽类型，列无行为变化简化任务；经正常batch gate | M（审计），后续简化各S/M |

O14清单还明确需要规则cache预热、全Agent一次全量推送并逐台确认core与content sync。
`progress/o14-switch.md` 中“紧急切回Python”的null互操作风险是历史记录，不能直接作为
现计划：WORKFLOW要求Python永久退休、stop-loss不回退Python，O14应据此重写处置步骤。
这不是要求补做Python兼容层。

## 6. 建议排程与交付检查

1. **S**：协调者完成当前固定batch-7的preflight/compare/core并合入；同步TSV及重复summary，
   不将本报告静态盘点视作测试。O3-8集成16/16已做，不重复派。
2. **M**：优先共享jsonstore F1（有明确关机窗口）及C37/A24持久失效协议；和rules/health/discovery owner串行协商。
3. **并行多个M/L**：N8-8d、N8-9其余组、N8-10分包推进；协调者按同包无开放分支安排，
   只留下单一薄adapter，避免增加第二份业务；MCP/artifact与typed核心边界同步排期。
4. **S（前提很大）**：N8-11在真实所有调用方完成后删除，连同测试/工具依赖与旧c.JSON*。
5. **L/M**：O9/O10/O12/O13/O15、L2–L5和#40可先预审/准备，再据稳定树验收；
   O17在O10后，O14在O13 clean后，L6与最终切换验收收口。切换后增强保持单列。

本次只提交 `server/parity/remaining-work.md`，未修改代码、测试、TSV或任何状态账本，未访问生产。
