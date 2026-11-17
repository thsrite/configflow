# Config differential Step B 进度

分支 `go-difftest-config-b`；依据 O6 审过的 `difftest-config.md`。只编辑 tools/difftest/testdata、进度与发现清单；未读 Go 业务实现，未派子 agent。**任务进行中，未交最终验收。**

## 批次 1：框架、O6 横切起步、auth.py

- 真进程：Python `-m backend.app` 和 Go `cmd/configflow`；测试 overlay 只在公开入口导出实际路由，未禁用后台循环/未替换业务逻辑。
- Python 默认空配置→备份 importer→独立副本，固定 TZ/JWT/工作目录/日志路径约定；auto_backup 关闭、无定时刷新源。X08 启动孤儿缓存清理及 DB/文件核对通过。
- 已编码 X02 每非全局代表的 path/query/header profile-before-auth、全局蓝图对照；三认证环境的默认/显式 Mihomo D1–D8；X03 外部内部调用头；T29 双向签发、无 exp、字符串 exp/iat、未来 nbf/iat、HS512/RS256/none、错 key、随机 key 重启失效。
- X01 当前实现 profile 描述的 raw/percent/double-percent，以及导出的脱敏/原始 send_file 两条形态。原始备份 direct_passthrough 保留全文，不能错断言为全局脱敏。其余蓝图、深度、native/editor/content 豁免尚待补齐。
- `backend/routes/auth.py` R011–R013 的所有 S/F/B 编号已编码（含中文账号、8192密码、错误 method、三类异常内容类型、A1–A7 和 B90 JWT）。未用宽泛错误 mask：只忽略明确 CPython 异常尾文，中文前缀仍检。
- DB/文件比较器已接入 `writes=True`；本批 auth 无配置写。后续每个写场景须实际启用并校验相应行/文件。

最近迭代（.63，2026-11-17）：

```text
~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups precedence,jwt,auth-environments,redaction,startup,auth-routes
self-check: 9 tests OK
compared=73, go-unimplemented=125, different=0, design_errors=[]
request_variants=198, named_matrix_cases=45, seconds=17.37
categories（请求变体）：S=11 F=58 B=6 T29=11 D8=12 X01=9 X02=81 X03=9 X08=1
```

125 个“Go 尚未实现”是已编码且 Python 前提验证过的具体变体，非失败也非通过；具体 method/template 与编号见 `tools/configdiff/testdata/results.json`。45 是去重 R 编号，包含在三认证环境重复运行的拉取场景；不得与 198 个变体或 Step A 5697 设计数混用。当前未发现实际业务差异；Allow 无序列表是明确 HTTP 语义比较，缺方法的篡改自检必报错。

## 继续顺序

1. 补全 X01 各蓝图/敏感键/深度/native/editor/生成 content 豁免，以及 X08 旧 node_test_dns 启动迁移边界；T27 实际 tick 单列 .32/隔离 fake。
2. 按路由文件补设置、profiles、共享资源、配置生成/OxiDNS/MosDNS、rules/groups、异步和其他文件；每例对应矩阵编号，todo 路由也编具体 fixture。
3. H helper guards、T/G 子变体须逐项参数化/审可达性；不可 HTTP 达的 OS/内部 guard 按 O6 标归属，不拿双方 500 算成功。
4. 每批写后 DB/文件实测、比较器 mutation；差异列 findings，由协调人派移植 agent。
5. 全量 .32 运行、rebase go、干净工作区、不推送后才最终交回。

## 批次 2：A1–A7 全展开，X01 多蓝图与 native 豁免，X08 迁移

- 从矩阵明确的 A1–A7 范围生成 **206 路由 × 7 = 1442** 个实际鉴权请求（有效 config_token 已配置，证明其不能越权管理 API）。这类输入有意使用缺失业务对象 ID，因为应在对象查找之前拒绝；不算该对象/校验业务分支覆盖。
- X01 增加节点、订阅、规则、组四种文本的原文/单次编码/多次编码/plus 编码；任意层敏感键删除、126–129 深度边界与保留非秘密 sibling；aggregation/library/ruleset/custom/MosDNS/domain/tasks/settings/notifications/auth/logs 代表。OxiDNS 原生 API 凭据通过所有 editor/render/common/native/template/save 默认与显式 profile 入口测试；proxy editor 的例外值也经真实写入及 DB/文件比较。
- 原始 config export 是 send_file/direct_passthrough，确实保留可还原全文；脱敏 export 和普通 JSON 则须清洗。两者明确分开断言。
- X08 旧测速 DNS：合成旧 endpoint + 保留的 `https://dns.example/dns-query`，Python 清掉旧 endpoint/Agent 元数据并记录通知；Go 保留旧数据。按 O6 的退休结论标 **O12-separate**，完整 DB/文件观察共 13 处差异均落入显式允许的迁移路径；任何其他路径仍报真正 difference。详见 findings。
- 实际路由未注册且 routes.tsv=todo 才标“Go 尚未实现”，不再向 Go 发无意义请求；declared done 却未注册会报 wiring 差异。注册后原用例自动比较。
- 新增 ZIP/tar/binary 篡改自检，合计 **10 项自检**；case ID 与其矩阵 route 也逐项校验。

.63 完整现有批次运行（54.84 秒）及 X08 完整观察补跑（1.42 秒）：

```text
~/cf-test/.venv/bin/python server/tools/configdiff/run.py
compared=632 go-unimplemented=1058 O12-separate=1 different=0 design_errors=[]
request_variants=1691 named_matrix_cases=1480
去重矩阵 R 类别：S=10 F=1465 B=5
请求变体类别：S=11 F=1500 B=6 X01=59 X02=81 X03=9 X08=2 T29=11 D8=12
self-check: 10 tests OK
```

仍未完成：X01 artifact/content 与 profile export 补充、极深解码/循环容器的 HTTP 可达性说明；统计类固定数字响应没有用户文本回显，不能硬造 X01 成功。X08/T27 实际 tick/退避/单飞尚待 fake 单列。所有 H/G/T 和其他路由文件的合法业务场景仍待推进；以上数字不代表 1480 个独立业务功能或整个矩阵通过。

## 批次 3：settings.py 同步管理与备份参数校验

- 增加 server-domain、config-token、rule-fetch-proxy、subscription-aggregation 的合法形态、错型/空值、中文/长值、随机 token、旧 capability 脱敏，以及保存失败。通过在两侧实际 SQLite 表安装拒写 trigger 注入失败；完整行、revision、其他 profile、文件仍比较，失败不假定一定回滚。
- 随机 config_token 验证 32 位字母数字并将双射贯穿响应及后续 DB；内部异常策略继承已有身份对应。空域名的启动自动 origin 验证 host 相同、port 指向各自真实监听，再建立单射，不按任意 URL 放行。
- backup/config 完整/部分/掩码/清空/保留边界与 now/test 的所有请求体验证已编码；WebDAV、自动备份、task 回执仍是后续专用批次。
- 无内置 Sub-Store 环境只编码 mode 非法/外部 409 和 update 无 bundle 409；未访问真实 GitHub。web-port 只编码 O6 允许的校验/保留端口/不支持部署分支；没有把 unsupported 503 算成改端口成功。
- X01 增加 profile export 的普通 JSON 脱敏检查。生成 artifact content 豁免仍待后续。

.63 重跑本批和 redaction-other-blueprints（22.07 秒）：

```text
~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups settings-sync,settings-token,settings-backup-validation,redaction-other-blueprints
current: compared=104 go-unimplemented=28 different=0 design_errors=[]
cumulative: compared=731 go-unimplemented=1080 O12-separate=1
request_variants=1812 named_matrix_cases=1589
去重 R 类别：S=40 F=1523 B=26；self-check: 10 tests OK
```

本批 120 个新请求变体中 99 个比较、21 个 Go 未实现。R020-B01/B02 的 token 保存和管理鉴权已测，配置拉取配对留生成器批次；R020-B90/T28 import/reset 轮换未完成。R198 MITM 成功/失败 release 查询、R199 真进程切换、R200 更新生命周期、R205/206 Nginx/原生监听部署实验仍按 O6 分流/部分覆盖，未伪造覆盖。

## 批次 4：回环 DAV 与开启自动备份

- stateful DAV fake 记录 HEAD/PROPFIND/MKCOL/PUT/DELETE、请求路径/Basic/正文与最终远端对象；两端每场景重置相同初态。R016/R017 增加 18 个具体成功/目录创建/编码/上传失败/列表失败/保留场景，全部 Python 参考前提有效，当前路由均未接通，标 Go-unimplemented。
- Python PUT 正文须严格等于其当前完整 export；保留用例确认旧备份删除而 unrelated.json 保留、keep_latest=0 不删。每个 upload 对应 final task receipt。动态文件名验证 TZ 下格式和当前时间窗再对应，消息中的清理数量不能被对应掩盖。
- R015-S90 开启 auto_backup 的即时写入本身比较通过；等待去抖后的 T16 单列完整协议/DB/文件观察。两侧独立回环 DAV endpoint 用固定 fixture 身份双射，避免并发 worker 无法归属。
- **实际差异，.32 已复核**：Python 约 1 秒后 HEAD/HEAD/PUT 上传并写 task-status.json event/success；Go 6 秒内无 DAV 请求、无上传对象、无回执。DB 配置提交一致。未改 Go；见 findings 的 `T16 / enable-immediate-final`，由协调者派移植 agent。
- duration 比较器只接受非负整数且不超过实测运行期限；新自检验证负数/超窗/bool/字符串与邻近字段篡改必报。DAV 协议方法/Basic/上传 JSON mutation 同样必报；共 12 项自检。

命令/最后输出（非最终全量）：

```text
.63: ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups settings-webdav
current go-unimplemented=18 design_errors=[] seconds=18.25
.32: ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups settings-auto-backup
current compared=1 different=1 design_errors=[] seconds=11.72
cumulative compared=732 go-unimplemented=1098 different=1 O12-separate=1
request_variants=1832 named_matrix_cases=1608（S47 F1528 B33）
```

未完成：DAV DELETE 单项失败、MKCOL 的 now 分支、缺配置失败回执与 TZ 跨日子变体；T16 其他提交/合并/无变化/失败不触发、T17 恢复；其余路由文件/H/G/T。此批不等于完整 DAV 验收。

## 批次 5：mosdns.py 设置、nodes.py 同步管理

已 rebase 到本地 go（e6c87cc1）；之前四批新提交为 005650ac / f1e33286 / 6f76697e / c2d4682b。重跑已有 1832 请求后结论未变。

- MosDNS 增加 155 请求：六组 legacy GET/POST、whole-editor、保存失败/错误内容类型/非法 profile、P4–P6、数值 spelling。`true→1`、`3.9→3`、空白/下划线数字、1e400/inf/null 默认、interval=0→300 通过公开 GET 验证。全部 155 比较通过。
- 每次保存比较所有 DB 行和文件；profile 时间仅在明确 metadata 路径验证窗口/格式，revision 和未知字段照比。default 与 other 均有合法的合成规则引用，不以缺引用的 500 冒充 selector 成功。
- 矩阵的 legacy MosDNS P3 需要更正：这六组设置没有显式 `/profiles/<id>/mosdns/<setting>` 公开别名，因此对应 GET B02 / POST B04 不虚构请求；已测 P4–P6，P3 记不可适用。
- nodes.py 增加 79 请求：CRUD 的显式/随机 ID、结构化节点、中文/扩展字段、错型/重复/坏订阅引用、整对象替换；reorder top/bottom/旧对象不覆写；无引用与 group+aggregation 普通引用的 preview→各端自己的 confirmation→原子解绑删除；空/错型/缺 ID/缺确认/过期确认；latency/parse-lines 无 worker 的校验与空批次。
- **新差异** R103-F14：`POST /api/nodes/reorder` JSON `[1]`，Python 500，Go 404；内部异常英文允许不同，但 status 必须记录。未修改 Go。其他 78 个 node 请求比较通过。
- 引用 blocker、跨 profile 修改使确认过期、Sub-Store/probe 协议、DNS 和完整 history 仍待下一批，不能算该路由全覆盖。

```text
.63 run.py --groups mosdns-settings,mosdns-numeric
current compared=155 different=0 design_errors=[] seconds=14.63
.63 run.py --groups nodes-crud,nodes-reorder,nodes-deletion,nodes-validation
current compared=78 different=1 design_errors=[] seconds=6.21
cumulative compared=965 go-unimplemented=1098 different=2 O12-separate=1
request_variants=2066 named_matrix_cases=1818（S92 F1614 B112）；12 self-checks OK
```

仍继续 Step B；尚未做最终 .32 全量。剩余大项仍是 profiles/import、rules/groups/library、config/generated/artifact、OxiDNS helper、tasks/domain/setup 和 H/G/T 展开。

## 批次 6：node-probe 进程与 DNS 公开协议

- 新增 78 请求，全部 Flask↔Go 比较通过。fake 是通过 `CONFIGFLOW_NODE_PROBE_BIN` 启动的真实可执行子进程，记录 stdin，返回 version=4；没有 Go helper 替代、没有 DNS/代理实际出网。默认测试目标固定合成域名。
- R098 全部/按名/按来源 key→落盘→R097 历史；非零 1/3、版本错、坏 JSON、错型/缺结果、latency=0/bool/null 无错误；缺组件、无效目标设置、21 节点和同名歧义。
- R100/101 系统/自定义/旧数组、去重与端口规范化；R099 成功地址去重、Fake-IP/坏 IP/空成功/坏 latency、版本/退出失败。完整 stdin、状态/消息、结果、文件/历史均比较。
- H082–H098 共 17 个 helper 配方以 31 个实际输入展开：type/空值、unspecified/multicast、端口/Unicode 数字、URL 空白/query/fragment/反斜线、scheme/凭据/hostname/IPv6、非法 percent、DoT path、列表 5 项、custom 空和 bootstrap 缺失。没有仅发一个无效 body 就声称覆盖整组。
- 矩阵 H104 中被合成替换成 `192.0.2.10/15` 的 Fake-IP 条件不准确；Python 实际检查 `198.18.0.0/15`，fixture 使用该保留测试网段中的 `198.18.0.1`。
- probe stdin 篡改自检新增，确认换 server、丢调用都报差；共 13 self-checks。findings 标题去除空 variant 的尾空白。

```text
.63 run.py --groups nodes-probe,nodes-dns-settings,nodes-dns-guards,nodes-dns-probe
current compared=78 different=0 design_errors=[] seconds=28.18
cumulative compared=1043 go-unimplemented=1098 different=2 O12-separate=1
request_variants=2144 named_matrix_cases=1849（S104 F1630 B115）
另有 H 去重17；R 数不包含 H/T/X。13 self-checks OK
```

仍未完成：probe timeout/busy、20节点成功/5转换上限、14条 history、DNS OS 启动失败与 timeout、H099+、Sub-Store 协议；其余路由与 artifact 内容豁免待继续。此批不是完整 PROBE/T13/T14 验收。

## 批次 7：artifact/content 豁免与冻结读取

- R062/R126 五种服务各自真实生成，X01 OxiDNS native 的 content 含合成内部能力时保持全文；R063 download/file、未知 action、坏/缺/过期/坏 metadata/hash/非 manual、文件缺失/越界读取。31 个请求均完成 Python 前提检查，Go 路由尚未接通；不是 31 个比较通过。
- 新 artifact 比较器先从实际私有 record/tar 验证 archive SHA、逐成员 SHA/size、config-version 前缀、manifest/公开响应对应、成员闭包/路径/类型/权限、TTL。只有验证成立后才把派生 hash/size 投影成关系，继续比较所有解析后成员、原始 source/其他 metadata、未知字段/文件及普通生成文件；不 mask 整个 content、记录或 archive。
- epoch 窗口新增 mutation（旧值/越窗/bool/string 必拒）；artifact mutation 验证：改字节且重算所有 digest 仍因成员内容不同而失败；只破坏 archive digest 也失败。共 15 self-checks。
- 增加前置未实现写入的依赖保护：当缺失 Go 路由确实改变了 Python DB/文件后，本 Pair 内后续已接通路由标 `go-unimplemented-dependency`，避免拿已经分叉的状态制造假差异；路由接通并从新 Pair 运行后自动恢复比较。当前 31 项均是直接未实现，无依赖计数。

```text
.63 run.py --groups generated-artifacts,generated-frozen-reads
current go-unimplemented=31 design_errors=[] seconds=3.93
cumulative compared=1043 go-unimplemented=1129 different=2 O12-separate=1
request_variants=2175 named_matrix_cases=1877（S116 F1645 B116）；H去重17
15 self-checks OK
```

未完成：artifact 24份/容量/TTL驱逐、附加文件读取、配置改动后冻结不变、Agent hosts/dialers 心跳报告；X01 循环容器不能由 JSON 输入表达，解码 RecursionError 的公开可达夹具还未证明，不算已覆盖。T27 实际 scheduler tick、X04/X05/X06/X07其余变体，以及其他路由文件继续推进。

## 批次 8：真实 scheduler tick、HTTP 壳与端到端反例

- T27 在未改生命周期的两端实际等首轮 30秒 tick：9 个公开创建的订阅涵盖无缓存到期、新鲜任务、新鲜同 URL 缓存、缓存 URL 不匹配、失败3次退避1200秒、已超过退避、退避被60秒 interval截断、interval=0、停用。只拉取预期4源，每源一次。回环源以任务回执作完成屏障，固定落盘顺序，使 health .bak 真正是两侧相同的上一次提交；没有 mask 备份内容。
- 缓存生成 node ID 验格式/全局唯一后建立单射，URL 与 transport-cache SHA 路径按对应 fake endpoint 验证后映射；所有其余节点、缓存/健康/任务字段、DB、备份完整比较。JSON .bak 按 JSON 比，坏结构文件保留原始字节比较；二进制发现只在报告层变成长度/SHA/prefix，不改变比较。
- **.32 复核差异** T27 real-tick/due-and-backoff：两端到期请求、缓存、健康 history/.bak 和 task receipts 一致，唯 Go 缺 notifications.json 中 `task:subscription:backoff` 的失败通知。见 findings，未改业务代码。
- X06 新增54请求：每蓝图代表 HEAD、五种拉取默认/显式 HEAD、登录开关/Origin/凭据组合 OPTIONS、401/404 CORS、尾斜杠/未知路由、五类 `%2F` 路径。29比较通过、25尚未接通。Python HEAD **不总等于GET200**：`request.method == GET / elif POST` 的handler对HEAD返回None→500；profiles HEAD落创建分支→201；OxiDNS settings HEAD→400。按实际源码分支设前提，不把这些当夹具失败，也不虚构成功GET。
- 机械反例：新增真实公开Go进程→回环转发→故意把 `/api/auth/status` 的 `authEnabled` 从true改false→正常 Pair.request 必须报 `body.authEnabled` 差异。每次运行都验证；不把故意破坏算业务finding，summary保存 `end_to_end_mutation_check=true`。
- 16个比较器自检（新增损坏JSON备份/原始字节与可序列化证据、NaN/孤立代理项证据），均通过。

```text
.32 run.py --groups scheduler-tick
current compared=9 different=1 design_errors=[] seconds=37.89
.63 run.py --groups http-head,http-options-routing
current compared=29 go-unimplemented=25 different=0 design_errors=[] seconds=25.71
cumulative compared=1081 go-unimplemented=1154 different=3 O12-separate=1
request_variants=2239 named_matrix_cases=1877（S116 F1645 B116），H去重17
end_to_end_mutation_check=true
```

T27 剩余 URL规则源、失败重试实际请求、与manual同key单飞；跨进程flock按O6留Go单元测试。X06全部provider有效资源的HEAD副作用仍待provider批次。任务仍进行中，其他H/G/T及业务路由未完成。

## 批次 9：profiles.py 元数据/独立导入与全表 P1/P2

- profiles 新增109请求：显式/自动ID创建、名称缺省、clone_from/clone、元数据部分更新/错型、默认切换/重复、删除默认/非默认及new_default优先级、Agent用途与改绑确认/忙、独立profile导入及公开export读回；不存在/非法profile与未登录404优先。当前全部Go未接通，Python前提全部有效。
- 全矩阵明确标 P1–2 的 **138路由 ×2 =276** 请求已展开，逐例验证精确错误消息和拒绝优先级，写后比较也启用：68比较通过、208未接通。输入里的业务对象ID有意无效，因为必须先拒绝profile；这不算对象handler的业务验证覆盖。
- 新建profile metadata时间/ID、profile行key和删除/导入恢复目录的nonce有明确对应；目录时间校验窗口，目标profile前缀必须一致。备份目录里的 SQLite按逻辑行比较，物理页不作oracle；新增嵌套SQLite备份改行篡改自检，共17自检。
- 参考清单两处纠正：R116-F08 truthy数组在 `data.pop('clone_from',None)` 就TypeError→500，尚未到repo400；R123-F12 的 body.clone_from 不参与clone固定source路径，不能按“不存在clone_from”构造该分支（源不存在已有F13/路径P覆盖）。未虚造400。

```text
.63 run.py --groups profiles-metadata,profiles-missing,profiles-deletion,profiles-bindings,profiles-independent-import
current go-unimplemented=109 design_errors=[] seconds=16.07
.63 run.py --groups profile-guards-all
current compared=68 go-unimplemented=208 design_errors=[] seconds=3.87
cumulative compared=1149 go-unimplemented=1471 different=3 O12-separate=1
request_variants=2624 named_matrix_cases=2213（S139 F1941 B133），H去重17
17 self-checks + end-to-end wrong-Go-response check OK
```

尚缺profiles的client import、所有clone引用/恢复快照细节、删除router busy、legacy隐式Agent/HA busy/并发；导入全库及token生命周期另批。其余路由/H/G/T继续，未做最终全量。

## 批次 10：logs.py、notifications.py、stats.py

- logs 51请求全部比较通过：UTC/Asia-Shanghai固定10006行，大小写搜索/等级、非正数全量/10000上限/默认、Unicode数字/+号/下划线/巨整数、中文/坏UTF8忽略、非法正则、真实清空后逐端读文件验证、缺文件、目录代替文件的I/O错误、同一隔离CWD里的fallback。不能把通用文件比较排除app.log当日志清空证明，专门比较实际字节。
- 比较器修正：无时区字符串按Pair设置的TZ解释，而非测试机宿主TZ；创建/修改时间仍验证窗口和格式，固定fixture mtime仍精确比较。新增时区mutation自检，共18项。
- notifications 20请求：13个read/参数/版本场景比较通过，7个GET场景待接通。覆盖2000/2001项、键1024/1025、值100/101、未知/旧版本、已读后新版本不得被旧确认清除；目录文件I/O和坏task counter造成ValueError→503。生产通知来源的失败回执差异仍保留T27，不用合成通知fixture冒充生产钩子通过。
- stats 12请求全部比较通过：首次、30分钟节流不改原文件（逐端前后读）、超过间隔追加、48保留、空计数、坏时间、坏历史恢复、文件不能替换仍成功、坏历史计数→500且观察可能已写入的历史。不预设所有500都不写文件。

```text
.63 run.py --groups logs-fixed,logs-fallback
current compared=51 different=0 design_errors=[] seconds=5.26
.63 run.py --groups notifications-validation,notifications-versions
current compared=13 go-unimplemented=7 design_errors=[] seconds=5.87
.63 run.py --groups stats-history
current compared=12 different=0 design_errors=[] seconds=9.79
cumulative compared=1225 go-unimplemented=1478 different=3 O12-separate=1
request_variants=2707 named_matrix_cases=2263（S155 F1955 B153），H去重17
18 self-checks + end-to-end wrong-Go-response check OK
```

未完成：日志真实事件序列/10MiB轮转、stat权限分支、notifications完整生产来源；stats P3–6和并发。后续继续config/shared/provider/client-import/OxiDNS/helper和长流程；当前不是最终全量验收。

## 批次 11：X04 JSON 文法/数字/Content-Type

- 102请求：login忽略字段中的NaN/Infinity/大整数/孤立代理项、重复key最后覆盖、charset/merge-patch/BOM/空body/plain；server-domain/config-token 字符串校验接受的JSON spellings；backup三个端点 strict-int 与 tasks/interval 的float/exponent/bool/string/越64位/NaN；silent解析入口的Content-Type/BOM。
- 有可能产生两端不同写入的每份输入用全新Pair/seed，避免一个错误提交造成后续generation差异。非写入login每份JWT单独验证claims/签名再对应。
- **新增三项实际差异**：X04 new_domain/literal/`"\\ud800"` 与 token/literal/`"\\ud800"`：Python SQLite UTF-8拒绝，500且未写；Go替换为U+FFFD，200并写入。X04 POST /api/nodes/parse-lines/content-type/bom：Python200空results，Go400“lines必须是字符串数组”。均保留实际status/body/DB值于findings，未改Go。

```text
.63 run.py --groups json-decoder,json-settings-intervals,json-silent-content-types
current compared=65 different=3 go-unimplemented=34 design_errors=[] seconds=35.52
cumulative compared=1290 go-unimplemented=1512 different=6 O12-separate=1
request_variants=2809 named_matrix_cases=2263（S155 F1955 B153），H去重17
18 self-checks + end-to-end wrong-Go-response check OK
```

X04仍缺OxiDNS范围型各字段、setup step、domain days、其余非silent handler；未声称整个X04已完成。接着推进config生成/拉取和其他业务文件。

## 批次 12：generate.py 与所有 config 拉取认证环境

- 生成接口：默认/显式profile五种服务的结构化preview、下载S01–S03、空资源、Unicode节点/组、特殊base_url、坏JSON/数组、POST Range/If-None-Match。YAML逐结构；Surge/Loon解析section/key/逗号字段，保留section/entry/rule/comment顺序，不按整段文本掩盖变化。新增INI换策略/换规则顺序与header长度/格式/时间window篡改自检，共20项。
- 下载验证每侧实际Content-Length与body相符、ETag quoting、Last-Modified时间窗；动态opaque header仅验证后映射，存在性及其他头仍精确比较，派生文件内容/权限仍比较。
- **B90参考前提更正**：generate下载路由仅POST，实测带Range和If-None-Match:*仍200完整下载。不能把GET send_file的206/304直接写成这几条POST的期望。真正GET条件下载在export/artifact批次继续；不把POST200冒充206成功。
- D1–D7三环境从2条Mihomo扩至全部11条拉取（含Loon filename alias），330变体待接通；9个管理X03对照比较通过。native OxiDNS、特殊Unicode query token/拉取日志不泄漏、相等8192/8193 token、Loon三类filename清洗/UTF-8转义、X03拉取/rule-proxy伪头也编码。
- 内部伪头在rule-proxy三认证环境9例比较通过。其他新生成/拉取用例当前未接通，不算比较通过。公开Host固定panel.example以验证默认host来源，不让两端监听端口制造假差异。

```text
.63 run.py --groups auth-environments
current go-unimplemented=330 compared=9 design_errors=[] seconds=6.76
.63 run.py --groups generate-previews,generate-downloads,generate-unicode,config-pull-credentials,internal-header-pulls,config-loon-names
current go-unimplemented=197 compared=9 design_errors=[] seconds=50.56
.63 补跑 generate-downloads,generate-unicode,config-pull-credentials（含POST条件头与日志验证）
current go-unimplemented=139 design_errors=[] seconds=35.56
cumulative compared=1299 go-unimplemented=1989 different=6 O12-separate=1
request_variants=3295 named_matrix_cases=2503（S234 F2032 B237），H去重17
20 self-checks + end-to-end wrong-Go-response check OK
```

仍缺生成器远程规则/缓存/取消、可见provider URL的转发头与HTTP1.0无Host、完整G配方、P3–6业务隔离、GET条件下载和ETag跨请求语义；Surge/Loon复杂语法以已生成的合法文本为范围，不声称通用INI解析器。下一批OxiDNS规范化/编辑器、其他业务文件和H继续。

## 批次 13：OxiDNS settings/CAS 与逐字段规范化

- settings 两种路由的强/弱/star/无头/stale/多值 If-Match、原生/模板草稿、精确 JSON SHA-256 ETag、publish_guard 对实际 SQLite revision、跨实现 Python ETag 写入及同时写入的 200/409。ETag 不掩码；新增错误 SHA 与错误 revision 自检，共21项。
- editor 七端点的请求外壳与 H369 空变化；H502–554 可经 HTTP 到达的规范化错误逐项，30个整数项各9种错误拼写、布尔/数组/插件分节/相对路径/API/TLS/动态学习/ROS 参数。每份只改目标字段，核对 Python 精确错误消息，避免错误分支被前置校验遮挡。
- 30个整数项 × min/max × 两别名 =120个独立进程/数据目录用例，验证真正接受上下界。远端4线程只并行隔离 fixture，不派 agent；端口分配和 Recorder 加锁。
- 保留 O6 原 verdict，在清单末尾明确记录源代码/实测修正：空changes、HEAD handler、POST下载条件头、无显式profile的legacy MosDNS、不可经JSON达到的H508等，不把不可达项冒充已测。

```text
.63 run.py --groups oxidns-settings,oxidns-wrappers,oxidns-normalization-guards,oxidns-concurrent-etag
current go-unimplemented=986 design_errors=[] seconds=20.45
.63 run.py --groups oxidns-integer-boundaries
current go-unimplemented=120 design_errors=[] seconds=35.55
cumulative compared=1299 go-unimplemented=3095 different=6 O12-separate=1
request_variants=4401 named_matrix_cases=2571（S250 F2070 B251），H去重70
21 self-checks + end-to-end wrong-Go-response check OK
```

当前这些 OxiDNS 路由仍未接通，以上是有效 Python 用例与自动激活的 Go 对照，不能计作比较通过。编辑器成功形态、native/global/plugin 全量校验与其他 H/G/T 继续，未做最终全量。

## 批次 14：config export/import/reset、T28 和恢复快照

- 125请求：原始/脱敏导出、真实206四字节片段/416/条件头；当前schema/原版/schema2/空集合/Unicode/未知字段/两profile共享资源导入，重启后公开export读回；非对象、坏JSON、26个仓库H项、真实SQLite拒写；重复全库reset与忽略坏body。五种路由器已发布ownership分别阻止import/reset，均409精确中文，未接触任何设备。
- 每次成功import/reset验证新增且仅一个SQLite快照，其逻辑行必须逐字段等于本端请求前全库（包括meta/revisions/positions）。然后跨实现比较所有快照、目录、文件与当前全库。增加快照revision篡改/缺快照自检；206片段与204/304空body自检，总23项。
- T28启用/关闭登录 × 缺能力/碰撞/自带有效能力：retired合并去重，当前rule-proxy到参数400、退役能力401（含碰撞时旧config_token仍必须拒绝）、config-token不得嵌入新/旧能力。依赖尚未接通的import实际改了Python库时，后续已注册路由标go-unimplemented-dependency，不能制造状态不同的假差异。
- 清单oracle补充：有效当前schema能力会保留，缺失/碰撞/reset才生成新值；BytesIO导出没有自动ETag/Last-Modified，If-None-Match:*与If-Modified-Since未来值实测仍200。保留O6原结论与编号，明确修正。

```text
.63 run.py --groups config-export,config-import,config-import-guards,config-capability-lifecycle,config-reset,config-protected-router-import
current go-unimplemented=97 go-unimplemented-dependency=28 design_errors=[] seconds=34.69
cumulative compared=1299 go-unimplemented=3192 dependency=28 different=6 O12-separate=1
request_variants=4526 named_matrix_cases=2598（S260 F2078 B260），H去重96
23 self-checks + end-to-end wrong-Go-response check OK
```

当前本批待Go接通，未计比较通过。T28生成器内嵌/包manifest能力、并发router锁、旧格式资源冲突重命名及部分repo OS异常继续；不是完整T28或仓库H验收。继续custom config、共享资源和其他业务文件。

## 批次 15：五种 custom config、Mihomo 基础表单与 EasyTier

- 488请求：五编辑器默认/保存/清空/64KiB/中文、Surge部分字段合并与null、MosDNS坏YAML允许保存、OxiDNS独立extension不覆盖native、坏JSON/数组/SQLite拒写，以及query/header/default选择和其他profile不变。
- Mihomo/EasyTier拒绝项逐字段展开：基础布尔/整数范围/枚举/文本列表、IPv6、fake-IP/sniffer/controller/DNS循环/Agent保留端口；EasyTier每个text/secret/bool/int/list字段、长值与列表数量、网络/URI/地址/keepalive/启用组合。325拒绝变体逐端验证精确中文及Python DB/文件全不变，覆盖56个新增H编号。
- 72个基础表单合法形态：布尔两值、整数min/max、全部枚举（含YAML on/off兼容）、空列表/null、仅本机controller空密码；respect-rules=true同时提供合法proxy-server-nameserver前提，不把前置拒绝当成功。所有成功保存的publish_guard独立核对实际profile+shared revision；增加错profile/service/revision篡改自检，共24项。
- custom editor没有显式profile URL，矩阵的P3槽不适用；P4–6按各行原编号验证。H289内部LinuxGatewayError被H290同一catch转换，H319内部ValueError被H320捕获，单HTTP不能分别观测；未把相同响应重复宣称覆盖。EasyTier生成期H341–343另随G配方继续。

```text
.63 run.py --groups custom-config-basic,custom-config-selectors,custom-config-failures,mihomo-base-guards,mihomo-easytier-guards
current go-unimplemented=416 design_errors=[]（该五组最终记录；合法形态另组）
.63 run.py --groups mihomo-base-accepted
current go-unimplemented=72 design_errors=[] seconds=4.66
cumulative compared=1299 go-unimplemented=3680 dependency=28 different=6 O12-separate=1
request_variants=5014 named_matrix_cases=2686（S284 F2096 B306），H去重152
24 self-checks + end-to-end wrong-Go-response check OK
```

首轮合法形态中respect-rules缺前提产生DesignError，已补前提并完整替换该组结果；没有列为业务差异。当前本批Go仍未接通。继续四种客户端导入和其他业务文件，未做最终全量。

## 批次 16：四种客户端导入

- 41请求：Mihomo/Surge/Loon/Shadowrocket各dry-run+持久导入+重启export读回。dry-run全库和所有文件严格不变；持久导入核对一个节点/组/规则、名称、每类随机ID格式与唯一性；未来Go接通后ID双射贯穿响应和数据库引用，时间只开放已知metadata字段。
- R155外壳校验全部、重复profile ID、中文/64KiB内容及不支持节点warning。H268–282共15个解析分支实际HTTP逐项触发：警告分支同时带有效规则，避免被“无实体”后置拒绝遮挡；错误消息或warnings中的具体原因必须出现。完整响应继续比较，不掩盖整个warnings数组。
- F18原“默认订阅不存在409”与Python不符：build_import先过滤不存在ID，仍201。保留原编号并在清单errata说明，测试实际导入后行和文件；未虚构409。

```text
.63 run.py --groups client-import-syntax,client-import-validation,client-import-warnings
current go-unimplemented=41 design_errors=[] seconds=11.48
.63 run.py --groups client-import-syntax（补充随机ID自检断言）
current go-unimplemented=12 design_errors=[] seconds=8.52
cumulative compared=1299 go-unimplemented=3721 dependency=28 different=6 O12-separate=1
request_variants=5055 named_matrix_cases=2708（S292 F2107 B309），H去重167
24 self-checks + end-to-end wrong-Go-response check OK
```

客户端复杂provider/规则库复用、各协议参数完整形态随G导入配方继续，当前四种基本语法不是整个导入器全部形态验收。继续subscriptions文件的有效业务/缓存/失败/副作用，尚未最终全量。

## 批次 17：subscriptions CRUD、缓存视图和清理

- 111请求，109比较通过、2差异：创建/替换/多行URL/健康默认/派生字段剥离、54个独立进程拒绝输入与正则语法、缓存raw/筛选计数/旧count-only/无metadata/URL不匹配/空nodes、健康历史、改URL清task/相同URL保留task、删除cache/health/task且保留其他项、三种引用保护及用途、旧/新重排。每次比较全库行和所有普通文件；不只GET回显。无真实上游请求。
- **新增差异 R217-S02**：同一URL、完全相同订阅对象PUT，Python shared/subscriptions行revision保留2，Go变5；HTTP和行value相同。fixture本身是全新Pair，task/health/cache写在外部文件，不曾改SQL，下一次真正URL变化后版本重新一致，未产生后续假差异。
- **新增差异 R223-F12**：reorder body `[1]`，Python500“订阅操作失败”，Go404“以下订阅 id 不存在: ['subscriptions 必须是对象数组']”。状态和消息保留于findings，未改Go。
- 清单更正：interval上限为604800，原2592000应拒绝；PUT路径强制ID，F17“重复body ID”和F19泛化“删字段仍引用409”未找到可达HTTP输入，不把前置400冒充repo409。

```text
.63 run.py --groups subscriptions-crud,subscriptions-validation,subscriptions-caches,subscriptions-cleanup,subscriptions-references,subscriptions-reorder
current compared=108 different=2 design_errors=[] seconds=30.52
补跑 subscriptions-crud（含604800正例）：compared=18 design_errors=[] seconds=4.47
cumulative compared=1408 go-unimplemented=3721 dependency=28 different=8 O12-separate=1
request_variants=5166 named_matrix_cases=2779（S315 F2137 B327），H去重167
24 self-checks + end-to-end wrong-Go-response check OK
```

继续本文件实际fetch/Sub-Store/provider/单飞/缓存回落和其他文件；以上不是subscriptions全覆盖，未做最终全量。

## 批次 18：手动订阅 fetch 和 Sub-Store 请求协议

- 15请求，10比较通过、5差异。真实回环上游：preview仍写cache/health/task但SQL不导入；非preview导入、同名不替换已有节点、正则后空集合但原始缓存不空、userinfo正文同请求；rendered YAML直用；Sub-Store本地已下载正文转换；URL fragment跳过直连，转换失败后direct fallback。缓存原始ID格式/唯一性、时间、SQLite与全文件都比较。
- fake Sub-Store严格校验POST `/api/subs` payload（name/url/#noFlow/noFlow/local/content）、GET `/download/<temp>/ClashMeta?noFlow=true`、DELETE `/api/sub/<temp>`及实际源/转换/清理顺序。临时名称先证明格式与三处一致，再替换这三个指定字段；结束时fake内存中不得残留临时订阅。上游UA、请求次数也比较。增加漏DELETE/错误正文/noFlow、cache身份、通知evidence、单边SQL提交等篡改自检，共29项。
- 新增R218-F11：数组body使Python500且无网络/写入，Go200并发上游请求、导入2节点及写cache/health/task/新generation。比较器现在把“旧Python commit对应新Go commit”作为普通业务差异报告，禁止重绑身份；之前会抛双射断言，已以单边commit自检修正。
- R218-F12坏缓存节点+已有全局节点、F13真实SQLite拒写：Python500 JSON“订阅操作失败”，Go500 HTML。S03 stale cache回落与F09无cache的HTTP403，以及F12，均再现T27同类缺notifications.json。不是五种独立根因，findings按五个具体用例保留。
- O6允许内部英文异常措辞变化：仅固定fake403场景把可选`status=403`内部尾字段规范化；异常类型、整个中文前后缀、错误HTTP状态和其他内容仍严格比较，错误500/中文篡改自检必报。未掩码整个错误对象。通知evidence必须对应本端真实task的run_count/last_run_at/status，未凭空映射时间串。

```text
.63 run.py --groups subscription-fetch-direct,subscription-fetch-failures,subscription-fetch-substore
current compared=10 different=5 design_errors=[] seconds=22.04
cumulative compared=1418 go-unimplemented=3721 dependency=28 different=13 O12-separate=1
request_variants=5181 named_matrix_cases=2794（S321 F2143 B330），H去重167
29 self-checks + end-to-end wrong-Go-response check OK
```

继续provider下载、真实HTTP HEAD副作用、单飞/30秒复用/取消，以及其他业务文件。5181是已编码请求变体，不代表5697项设计清单已完成，也未做最终全量。

## 批次 19：subscription provider 格式、HEAD、时区和元数据

- 43请求（含4个T04准备PUT），28比较通过、15差异：默认/显式profile两入口的YAML、Surge、Loon、include/exclude筛选、空结果、unknown format、无URL缓存、无缓存/空缓存/非法filters/不存在对象；UTC与Asia/Shanghai，以及URL profile覆盖query/header。准备PUT只是令interval=60走首次拉取，不代表整个T04配方完成。
- 真实HEAD首次请求确实执行上游GET并写raw cache/health/served文件，响应body为空；比较实际副作用。Surge/Loon文本逐行、YAML逐结构比较外，新增独立注释元数据观察：Generated是本地ISO时间后接字面Z，文件名时间戳也按本端TZ校验；只把验证过的时间数字和文件名对应，其他注释全部比较。served YAML文件中的注释也观察，不能被通用YAML解析丢掉。
- **15个差异是同一遗漏的不同场景**：Go两入口的YAML响应和served副本无Python的注释（实际Go comments=[]），包括Generated时间；YAML数据、文件名、上游次数和HEAD副作用相同。不是15种独立根因。保留R154/R220的S01/S06/B03/B90/B91以及R154-B04的具体证据。
- 实际路由manifest表明R154已接通，尽管routes.tsv仍todo，本工具自动比较而未继续跳过。新增last-good/stale/warning标头比较与元数据/错误时间/错误subscription文件名自检，共31项。

```text
.63 run.py --groups subscription-provider-formats,subscription-provider-head,subscription-provider-validation,subscription-provider-timezone,subscription-provider-selector
current compared=28 different=15 design_errors=[] seconds=27.12
cumulative compared=1446 go-unimplemented=3721 dependency=28 different=28 O12-separate=1
request_variants=5224 named_matrix_cases=2823（S331 F2151 B341），H去重167
31 self-checks + end-to-end wrong-Go-response check OK
```

继续12秒last-good回退/后台收尾、并发单飞/格式隔离、merged provider和引用图错误；未做最终全量。

## 批次 20：T06 真实12秒回退、后台完成和20并发客户端

- 5个序列记录（其中2个准备PUT）；并发子例实际每端20个HTTP客户端，逐个比较全部响应，不把第一个响应当整批。使用公开`CONFIGFLOW_HTTP_THREADS=32`允许20请求在fake屏障处同时等待，生产生命周期及所有后台钩子保留。两端均只一次source GET、一条health历史，同一实现内20个响应body相同；全库/所有文件继续比较。
- 12秒计时器未替换：已有last-good时在11.0–13.8秒窗口返回旧完整字节与`X-ConfigFlow-Provider:last-good`，随后放行上游，观察fresh served文件和cache，确认备份及全部普通文件连续150ms稳定、无临时文件后再比较；下一次poll无额外source请求且返回新内容。
- 整个生命周期只剩已知注释遗漏（3个序列实例），未发现单飞/回退时限/后台收尾的新根因。曾捕获的emoji原文检查、`.yaml.bak`文本解析、原子写入过渡态均为测试端假差异，已分别改为YAML语义、YAML备份解析及外部可见收尾屏障并重跑；不掩盖最终残留临时文件或缺备份。
- 增加最后一个并发响应故意改401、少一个响应、回退超时/未完成/重复source请求、YAML备份语义与改节点自检，共34项。served备份中的已知注释也独立观察，不因YAML解析而漏掉。

```text
.63 run.py --groups provider-last-good-lifecycle
current compared=1 different=2 design_errors=[] seconds=27.71
.63 run.py --groups provider-concurrent-clients
current compared=1 different=1 design_errors=[] seconds=5.67
cumulative compared=1448 go-unimplemented=3721 dependency=28 different=31 O12-separate=1
request_variants=5229 named_matrix_cases=2823（S331 F2151 B341），H去重167
34 self-checks + end-to-end wrong-Go-response check OK
```

T06还需无last-good时等待、Surge/Loon不回退；T07键隔离、T05断连仍提交及其他业务文件继续。当前5229是记录变体数，20客户端批次另明确标`http_clients_per_backend=20`，不是把该批只发一次HTTP。未做最终全量。

## 批次 21：merged subscription provider

- 17请求，7比较通过、10差异：多个来源合并、禁用跳过、空启用集合、同名同/不同定义但无拓扑的200、严格拓扑下跨feed冲突400、坏dialer400、HTTP403旧缓存回落、非法provider片段禁止降级、真实HEAD拉取/写cache以及两种TZ。HEAD合并缓存按实际转换结果保存，不错误套用manual fetch的随机node ID前提；完整行/文件比较仍开启。
- merged注释新增独立验证：主Generated、Subscriptions/Total Proxies，以及每个已知订阅名/节点数/本地更新时间；仅证明为该条生成时间的后缀可对应，名称、计数及其他内容严格比较。35项自检含改订阅节点数/总数/时间的反例。
- 9个实例仍是Go省略注释的已知根因。**新差异 R222-F11**：两端均400且均拒绝用旧cache掩盖非法源；Python消息保留`订阅「订阅甲🧪」准备失败：`、内容来源、行列和原始合成错误行，Go只保留内部`Provider 代理必须是含非空 name 的对象\n字段：proxies[0]`。完整合成结果见findings。
- Python参考前提修正：无chain/dialer时同名甚至不同定义可以200；F09/B01需以合法dialer激活严格名称校验，已分别验证成功/失败形态并记清单errata。未把前置guard或错误fixture计业务失败。

```text
.63 run.py --groups merged-provider-data,merged-provider-remote,merged-provider-timezone
current compared=7 different=9 design_errors=[] seconds=16.28（加入额外合法形态前）
.63 run.py --groups merged-provider-data
current compared=3 different=6 design_errors=[] seconds=10.59
cumulative compared=1455 go-unimplemented=3721 dependency=28 different=41 O12-separate=1
request_variants=5246 named_matrix_cases=2834（S335 F2154 B345），H去重167
35 self-checks + end-to-end wrong-Go-response check OK
```

继续剩余鉴权环境、provider拓扑/G配方、tasks和其他业务文件；仍有明确未覆盖项，不是最终全量交回。

## 批次 22：tasks 同步列表、间隔及run参数拒绝

- 38请求，当前tasks三路由尚未接通，均保留有效Python前提和自动激活。订阅/URL规则/内容规则间隔、604800实际最大值及原2592000拒绝、监控保存四档间隔，验证实际SQL字段；内容规则虽能改间隔却不列为可运行URL任务，返回task=null。
- run参数、未知category、空resource、缺对象、domain profile非法/不存在、未启用自动探测、无URL/content源、metrics带冒号均核对精确中文或id消息，覆盖O6 F90/F91。固定第三次失败回执先设3600秒间隔，GET必须产生固定1200秒重试deadline；固定fixture时间没有被窗口掩盖。
- 系统metrics下一次运行是启动/执行基线+当前间隔，增加精确路径的future_times偏移窗口；只允许这个确定间隔，不扩大所有时间窗口。Python前提也验证ISO秒格式/对应间隔，新增错误deadline篡改自检，共36项。

```text
.63 run.py --groups tasks-intervals,tasks-run-validation,tasks-backoff-rows
current go-unimplemented=38 design_errors=[] seconds=6.06
cumulative compared=1455 go-unimplemented=3759 dependency=28 different=41 O12-separate=1
request_variants=5284 named_matrix_cases=2860（S340 F2168 B352），H去重167
36 self-checks + end-to-end wrong-Go-response check OK
```

202后台执行、busy释放/重复运行、真实fake任务、Agent/router任务行及interval并发继续；当前不算tasks整体通过，未做最终全量。

## 批次 23：tasks 202后的最终回执与占用释放

- 10请求：未配置WebDAV依然202，随后精确失败消息、计数和manual回执落盘，重复请求再次202并失败；无Agent监控保存202后success，再次运行计数递增。配置SQL全行不变，回执及全部普通文件照常比较。
- 订阅和URL规则各用真实loopback源屏障：先202并在公开tasks行确认running，再同key409；两端都收到busy响应后才放行源，等最终回执和running=false，再202重复运行证明锁释放。完整source方法/路径/鉴权/UA与每端请求数比较，busy不得额外拉取；订阅只写原始cache，不把manual任务误当成SQL节点导入。
- 新增回执反例自检：计数、结果、trigger、错误、失败次数和连续失败数任一改错均失败，共37项。任务完成屏障不靠固定sleep把202当完成；Go将来若先接通POST而GET仍缺失，不把缺GET误计POST差异，用后续重复POST独立验证锁释放。Go当前未接通，10项均只计真实Python有效参考，不计通过。

```text
.63 run.py --groups tasks-asynchronous,tasks-source-busy
current go-unimplemented=10 design_errors=[] seconds=7.61
cumulative compared=1455 go-unimplemented=3769 dependency=28 different=41 O12-separate=1
request_variants=5294 named_matrix_cases=2865（S344 F2169 B352），H去重167
37 self-checks + end-to-end wrong-Go-response check OK
```

tasks的domain/DNS/router任务及配置化WebDAV任务仍待fake；断连后继续执行另列待做。T18仅完成这里列出的子序列，未宣称整个跨域任务协议通过，未做最终全量。

## 批次 24：策略组来源、健康检查、预览、引用与listener

- 176请求，当前相关Go路由均未接通。默认与显式profile入口都实际创建manual/subscription/aggregation/include/follow/chain六类来源；url-test/fallback/load-balance默认健康字段、chain清除普通字段、中文emoji、列表空/非空。精确SQL存储与HTTP返回一致；无测试端健康默认注入。
- PUT部分合并、select转url-test默认初始化、delete_fields、改chain；同名/改ID/不存在/错引用/同时赋值删除拒绝。batch-health仅改选中URL，其余字段不动，一个未知ID整批行/revision不变。重排新旧格式仅改顺序，不采纳展示字段；坏body[1]真实Python500。
- preview从固定cache与已导入节点组合，验证中文、反引号桶顺序、单独exclude、聚合、按源去重与默认按名去重；坏源/错型/坏正则均独立用例。include/follow/rule/chain引用均阻止删除或禁用，完整事务不落盘；改名同步rule.policy和listener.proxy，删除只解除固定策略，注释与listener.rule保留。
- H225–229/H231/H233–239/H242–247共19个去重helper，另H248–253共6个坏listener草稿分支，各用PUT/DELETE触发并证明无行/revision变化；对应R159-F16/R158-F12也有独立编号。保留未改旧错型filter的兼容写入。H230/H232有前置形状guard，未虚计覆盖；其他拓扑/DNS/helper和selector边界继续。

```text
.63 run.py --groups groups-create,groups-reject,groups-update,groups-batch-health,groups-reorder,groups-preview,groups-references,groups-candidate-guards,groups-listener-guards
current go-unimplemented=176 design_errors=[] seconds=32.2
cumulative compared=1455 go-unimplemented=3945 dependency=28 different=41 O12-separate=1
request_variants=5470 named_matrix_cases=2980（S379 F2228 B373），H去重192
37 self-checks + end-to-end wrong-Go-response check OK
```

无新差异；全部176项是已验证Python前提且等待Go接通，不计比较通过。未做最终全量。

## 批次 25：共享规则库、草稿CAS、匿名正文和缓存/连通性

- 126新增变体（CAS变体每端实际2客户端）：content text/YAML/空/中文、URL text和MRS envelope原始二进制、剥离无关来源及derived字段、创建/改名/改内容/改URL/删除的数据库与全文件；保留update_interval；不存在/重复ID/错型/无效来源/下载失败。MRS这里仅验证此HTTP路由的压缩信封与原字节保存，不宣称目的核心trie解码。
- draft完整声明SHA revision按Python规范独立计算；精确版本写入正文及stable cache，旧版本409，非content拒绝，字段集合/错型全覆盖；两个相同revision且相同新正文的并发请求，恰好200+409，最终SQL/cache再次比较。usage_count跨两独立profile；匿名默认/URL profile正文均原始UTF-8+CRLF字节比较，空正文和URL源错误独立。
- 检查接口返回修复建议与errors而不写库；classical手写内容不被自动改写。库重排忽略旧展示字段，公共引用阻止删除。proxy_domains trim/清空/特殊字符与错误对象按真实Python行为。
- fake源扩展HEAD、按路径响应与302，test-single精确200才available，bulk HEAD按<400且失败自动禁用；content跳过、空库、全成功保留偏好。批量cache混合源、旧有效cache在403时保留且enabled不变，manual task/notification最终文件完整比较。仅异步results按唯一id排序、并发source交互按完整请求记录排序，缺项/重复/改结果/HEAD改GET有自检，38项。
- 回归6个已有subscription-fetch-direct，证实共用fake GET与观察器保持比较通过；无新业务差异。Go规则库相关路由尚未接通，126新增项全部只计有效Python参考，未来自动比较。

```text
.63 run.py --groups library-crud,library-drafts,library-check-content,library-public-content,library-remote,library-proxy-domains,library-reorder,library-network-checks,library-cache,library-concurrent-draft,subscription-fetch-direct
current go-unimplemented=126 compared=6 design_errors=[] seconds=33.79
cumulative compared=1455 go-unimplemented=4071 dependency=28 different=41 O12-separate=1
request_variants=5596 named_matrix_cases=3100（S414 F2281 B405），H去重192
38 self-checks + end-to-end wrong-Go-response check OK
```

规则库native跨profile重建/磁盘失败与不确定提交/下载期间CAS等分支仍待补，其他业务文件继续，未做最终全量。

## 批次 26：profile规则、规则集、批量操作与并发ID

- 123新增变体：普通DOMAIN/IP-CIDR/MATCH（UDP fallback与ntp_direct）、规则库引用strip、中文IDNA/emoji remark/逗号regex；默认与显式profile创建/读取，空列表。错类型/空值/坏域名CIDR/缺或错library/MATCH错值/重复ID均独立请求，写后数据库与文件继续比较。
- 部分PUT、规则值/policy更新、dns=default、空remark和不可更改路径ID；旧无效值的无关字段仍可编辑。专用rule-sets强制itemType，普通规则ID不能借此更新/删除；不存在和重复删除按真实404。
- batch逐行验证，第二行无效全库不动；ID分配跳过已有rule_1，两个真实并发客户端均成功且分别得到rule_2/rule_3，SQL行/文件与全部响应再比较，未用客户端完成先后作为非确定对应关系。
- group_name trim、清空、8193中文字符、缺/错成员及不存在成员原子拒绝；bulk policy保留省略label、显式清空label，不改未选成员，201条仍接受。新旧reorder仅改顺序；重复ID400、整数未知ID404、不可hash列表ID500分别验证，未错误统一成400。

```text
.63 run.py --groups rules-create,rules-update,rules-sets,rules-batch,rules-labels-policy,rules-reorder,rules-concurrent-batch
current go-unimplemented=123 design_errors=[] seconds=14.31
cumulative compared=1455 go-unimplemented=4194 dependency=28 different=41 O12-separate=1
request_variants=5719 named_matrix_cases=3218（S448 F2341 B429），H去重192
38 self-checks + end-to-end wrong-Go-response check OK
```

Go相关路由尚未接通，123项均只计有效Python参考。5719是请求/序列变体数量，不是5697设计条目已完成；R/H/G/T等仍有未实现项。DNS/native联动、compose、匹配/重复扫描、local缓存入口及其他业务文件继续，未做最终全量。

## 批次 27：规则DNS template/native写入与选择清理

- 53新增变体：template和由Python生成的有效native配置都逐一写direct/proxy/follow/default/refused/nxdomain/nodata/blackhole，读回精确state/route；IP规则明确unsupported，skip只略过该成员。200条接受，201条在事务前拒绝；缺/空routes、错型choice、未知rule、混合不可转换项全库/revision不变。
- 删除ordinary rule清理全部selector/follow/action/parked，native同时重建正文；MosDNS ordinary/ruleset选择也在同一事务清理。已用于MosDNS或显式OxiDNS处理的规则不能改不支持类型，显式dns=default可原子清路由并改类型。
- 无法安全管理的native图严格DNS写409且无落盘；非严格policy写仍保存并给warning，native原文保留；不可解析native草稿不阻止删rule且保留坏草稿。删除native固定相同Host，生成基址不因两端loopback端口而产生假差异。

```text
.63 run.py --groups rule-dns-choices,rule-dns-selectors-types,rule-dns-native-locked
current go-unimplemented=53 design_errors=[] seconds=12.41
.63 run.py --groups rule-dns-selectors-types（固定Host后）
current go-unimplemented=10 design_errors=[] seconds=5.95
cumulative compared=1455 go-unimplemented=4247 dependency=28 different=41 O12-separate=1
request_variants=5772 named_matrix_cases=3240（S455 F2351 B434），H去重192
38 self-checks + end-to-end wrong-Go-response check OK
```

Go尚未接通，未计比较通过。指定上游catalog/Agent绑定、disabled parked/native严格更多分支和DNS跟随组变更仍待补；未做最终全量。

## 批次 28：compose跨共享库/profile原子写入

- 33请求：values domain/IP/逗号regex、引用库、新内容库和fake URL库、top/bottom、中文组名；200行/50库恰好接受，重复paste去重且已有value跳过。校验所有生成ID的资源前缀+12位hex且唯一，才建立单射；新库cache路径由已验证声明独立算SHA，只对应正确的by-id/by-source路径，内容/错路径继续严格比较。
- 非对象、policy/position/text/行数/逐行field_errors/全注释/不支持DNS、库数量/重复/错型/kind/新库对象与空名/无来源/同名/不存在policy或library/已引用library、下载失败逐项核对；失败前后完整数据库完全相同。native DNS拒绝时，新建shared库及profile同时回滚。
- 真实文件路径故障：把by-id目标目录置为合成普通文件，验证DB提交仍成功且精确warning为`规则集已保存，但本地缓存失败，发布时会重新获取`；不把post-commit缓存失败误作整笔回滚。全部普通文件继续比较，没有忽略故障夹具。
- 新增组合ID/derived cache自检：错前缀/大写/重复ID/错结果形态拒绝，正确不同ID的相同内容可对应，故意改Go缓存正文仍失败；URL声明改变必须改变cache路径。39项自检。

```text
.63 run.py --groups rules-compose-success,rules-compose-reject,rules-compose-remote
current go-unimplemented=33 design_errors=[] seconds=15.84
cumulative compared=1455 go-unimplemented=4280 dependency=28 different=41 O12-separate=1
request_variants=5805 named_matrix_cases=3272（S461 F2372 B439），H去重192
39 self-checks + end-to-end wrong-Go-response check OK
```

Go尚未接通，未计通过。native成功compose更多形态和helper/G配方仍需推进，未做最终全量。

## 批次 29：匿名规则文件、匹配和重复扫描

- 52请求：默认和显式profile local入口均匿名验证内容首次建cache/已有cache、中文名、无catalog旧名称cache、路径特殊字符不逃逸、空文件、Mihomo classical交付转换且raw cache不变、非法UTF-8原字节回退，以及文件路径为目录时500。所有请求继续SQL/文件比较。
- fake URL来源：已有验证cache不再下载；既有failed task使其带stale/Warning；无cache首次真实GET落盘；非法源422、HTTP403无cache502并比较manual/pull回执与notification。比较器增加真实`X-ConfigFlow-Cache`头，不因其他provider头名称不同而漏掉。
- match覆盖域名/IPv4/IPv6/未命中/根尾点/disabled与缺参数、空值、坏query、非对象/数字query和非法remote规则422。中文query实测400，ASCII punycode可查询但不自动转存量Unicode值，已补oracle说明，未把错误Python前提记录成Go差异。
- duplicates覆盖无重复、同库内部、跨direct/library且policy冲突、REGEX大小写敏感、domain大小写合并与稳定tie；远端非法导致查重500、远端403仅列failed_rulesets仍200。完整响应包括行号、来源、计数与排序均比较。
- elapsed_time仅在这些具体路径以非负有限毫秒、两位小数精度且不超过真实请求窗口验证；没有把所有数值或时间泛化mask。增加负数/布尔/字符串/NaN/Infinity/超时/过多小数反例，40项自检。

```text
.63 run.py --groups rule-local-content,rule-local-remote,rule-match,rule-duplicates,rule-index-remote-errors
current go-unimplemented=52 design_errors=[] seconds=34.45
cumulative compared=1455 go-unimplemented=4332 dependency=28 different=41 O12-separate=1
request_variants=5857 named_matrix_cases=3320（S480 F2387 B453），H去重192
40 self-checks + end-to-end wrong-Go-response check OK
```

Go相关路由仍未接通，未计通过。大规模扫描取消客户端/日志协议、更多matcher/helper和生成器G形态继续，未做最终全量。

## 批次 30：G01–G05节点入口、客户端协议及组形态

- 76新增变体：G01结构化/JSON/YAML三入口×Mihomo/Surge/Loon，外层名称覆盖内层；G03 Surge 13协议（含WireGuard/wg、tuic/token与v5、TLS/WS/Snell obfs）；G04 Loon 12协议（含SSR、WG双栈/DNS/keepalive、TLS/WS）。所有preview完整文本结构及SQL/文件继续比较，不只检查200。
- G05六种组×三客户端，含manual/include的精确顺序、健康参数、follow与chain；Mihomo可生成chain，Surge/Loon明确400含代理链消息。这里是HTTP生成合同，未宣称真实目标内核已经接受或设备已生效。
- G02七种URI和base64列表×三客户端。fake Sub-Store核对每个完整create/download/delete循环、随机临时名称格式与唯一性、固定noFlow声明、完整URI query和值、清理无泄露；循环数量也保留比较。新增错query/漏DELETE/错清理目标反例，共41项自检。
- Python生成链有两层：Mihomo/预检使用fake扁平结果，而Surge/Loon最终节点仍走本地parse_uri_list；因此Surge SSR/hysteria、Loon hysteria、两者raw base64列表按Python省略协议处理，不能强行要求渲染fake返回的SS。SSR输入使用完整base64 padding，避免把坏fixture当目标不支持。已回归旧subscription-fetch-substore两项仍比较通过。

```text
.63 run.py --groups generator-node-inputs,generator-client-protocols,generator-group-shapes
current go-unimplemented=52 design_errors=[] seconds=51.39
.63 run.py --groups generator-uri-inputs,subscription-fetch-substore
current go-unimplemented=24 compared=2 design_errors=[] seconds=40.76
cumulative compared=1455 go-unimplemented=4408 dependency=28 different=41 O12-separate=1
request_variants=5933 named_matrix_cases=3320（S480 F2387 B453），H去重192，G去重5
41 self-checks + end-to-end wrong-Go-response check OK
```

全部新增generator路由当前Go未接通，未计比较通过。G06–G16及各族更多交叉分支继续，未做最终全量。

## 批次 31：G06完整规则词表与K46错误分支

- G06三份公开preview（Mihomo/Surge/Loon），每份包含全部37种Python RULE_TYPES，提供合法域名/IDNA、双栈CIDR、端口与UID/DSCP范围、逻辑嵌套、regex逗号负载、provider/sub-rule引用和MATCH UDP/NTP参数。词表从允许的Python源AST读取，仅检查配方没有漏掉新类型；没有读取或调用Go业务。Mihomo逐类型确认都在输出，其他客户端保留其映射/忽略注释及规则顺序，再完整结构比较；这是3个请求，不虚计为111个请求。
- 34个HTTP错误输入到达25个K46 helper分支（H918–943，除H921），通过R183的同一normalize_rule_config调用链校验精确消息，失败后全库/revision不变。包含真实escaped lone surrogate触发IDNA UnicodeError、IPv4/6缺prefix、前导零prefix/zone、范围组数/长度/逆序、regex过深RecursionError、logic深度34、正负不配平、NOT双子项、MATCH/SUB-RULE禁嵌、换行与错型等。
- H921的空/非文本CIDR在该REST调用链先被单行文本/空值guard拦截，没有伪造为已到达；仍留待其他允许的公共驱动或标不可达审计。所有新增路由仍Go未接通。

```text
.63 run.py --groups generator-rule-types,rule-syntax-guards
current go-unimplemented=37 design_errors=[] seconds=6.69
cumulative compared=1455 go-unimplemented=4445 dependency=28 different=41 O12-separate=1
request_variants=5970 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重6
41 self-checks + end-to-end wrong-Go-response check OK
```

G07–G16和其他helper、业务路由、长流程继续，未做最终全量。

## 批次 32：G07规则源格式、G09 DNS转换与原始正文比较

- 69新增变体：G09 Clash text/YAML、List和原生MosDNS各mixed/domain/ip，核对精确full/domain/keyword/regexp、IPv4/6和正则中逗号/DIRECT/no-resolve不误裁；空IP部分必须为`# no ip rules`。此公开rule-proxy已接通，13项实际Flask↔Go比较通过。
- G07合法domain/ipcidr/classical与text/yaml、domain/ipcidr MRS声明的五服务preview，普通文本/YAML的两DNS完整ZIP；原始cache已准备且fake证明没有再下载。MRS交付回调两类型均422（实际Go比较通过），启用domain MRS的MosDNS ZIP500/OxiDNS ZIP400且明确不支持MRS，不能把preview200误报为DNS二进制支持。
- MRS合成输入只覆盖Python验证的压缩信封与opaque body保存，尚未证明目的Mihomo核心trie能加载；这项仍部分覆盖，需要合法真实核心生成的MRS fixture/目的核心验收。其他请求均为合成text/YAML，不访问外网。
- 发现通用文本结构比较会抹掉原文CRLF差异，已收紧原文合同：匿名library/local成功正文启用精确字节比较，`shared/rules/**/*.list`及备份作为raw source bytes比较；生成器YAML/INI/归档结构仍按约定语义比较。新增CRLF→LF故意错误自检，42项；回归相关42个已有原文请求，不改变其未实现状态。

```text
.63 run.py --groups generator-dns-conversion,generator-ruleset-formats,library-public-content,rule-local-content,rule-local-remote
current compared=15 go-unimplemented=96 design_errors=[] seconds=35.41
cumulative compared=1470 go-unimplemented=4499 dependency=28 different=41 O12-separate=1
request_variants=6039 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重8
42 self-checks + end-to-end wrong-Go-response check OK
```

本批除上述15个rule-proxy请求外，生成/原文路由仍Go未接通。原文比较收紧尚未对所有旧组全量重跑，留待最终.32全量验证；未做最终交回。

## 批次 33：G08 Mihomo DNS与EasyTier公开订阅输出

- 10请求：顶层/dns.ipv6均分别true/false并核对精确布尔类型，hosts、proxy-server-nameserver保留。EasyTier subscription/agent/all三scope×keepalive开关，公开订阅输出只在subscription/all加入outbound；agent-only不泄入订阅规则。
- 核对network-secret在生成配置中保留、MagicDNS policy、overlay CIDR和域名规则必须排在普通规则之前；keepalive仅开启时生成fallback组，URL/interval精确。另有全局disabled省略与dialer-proxy指向降低后的公开代理链名称。
- 本批仅证明公开subscription目标的scope行为；for_agent=true版本门禁/真正Agent推送仍待fake Agent序列，不能把agent-only在订阅中省略当成Agent目标成功下发。没有实际加入虚拟网或发keepalive网络请求。

```text
.63 run.py --groups generator-mihomo-dns,generator-easytier
current go-unimplemented=10 design_errors=[] seconds=12.17
cumulative compared=1470 go-unimplemented=4509 dependency=28 different=41 O12-separate=1
request_variants=6049 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重9
42 self-checks + end-to-end wrong-Go-response check OK
```

Go相关preview仍未接通，未计通过。G10–G16、Agent目标及其他未完成项继续，未做最终全量。

## 批次 34：G15客户端地址与能力矩阵

- 96请求：Mihomo/Surge/Loon各覆盖server_domain有/无、config_token有/无、base_url显式/省略、转发头有/无、default/other路径。显式other路径与冲突default header证明路径优先；比较完整主机、带前缀路径、query和生成正文，未掩码URL。
- 订阅provider/policy-path及Surge managed-config仅按配置携带pull token；匿名规则URL不带管理员JWT或其他能力。Mihomo规则走rules/local，Surge/Loon内联规则走rule-library/content。使用真实原始节点缓存及内联规则，fake源断言零外网下载。
- 原生启动会自动补齐空server_domain。未设域名的fixture在两边启动后以外部SQLite事务清空该字段，并改变generation/commit使公开读取失效旧缓存；没有修改生产入口或循环。数据库其他行和revision照常比较，不能通过后续归一化隐藏启动差异。
- **G15仍部分覆盖**：DNS preview YAML只含本地文件引用，下载地址在交付准备中形成；不能以preview200声称验证了DNS下载URL。MosDNS/OxiDNS URL及rule_proxy_token待fake Agent交付序列。

```text
.63 run.py --groups generator-url-capabilities
current go-unimplemented=96 design_errors=[] seconds=6.45
cumulative compared=1470 go-unimplemented=4605 dependency=28 different=41 O12-separate=1
request_variants=6145 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重10
42 self-checks + end-to-end wrong-Go-response check OK
```

本批生成preview仍Go未接通；无新业务差异。继续备份及其余生成器/业务/长流程，未做最终全量。

## 批次 35：G14三种备份格式完整恢复往返

- 12请求，当前schema、原版单配置、schema2多profile均通过公开import → 重启 → export → 将公开原始导出再次import → 重启 → export。不是启动时pre-SQLite迁移。
- 每种包含真实共享节点（合成凭据）、订阅、内联规则库、profile本地策略及MosDNS选择；多profile的不同资源ID和本地引用都精确保留。原始custom_config及两类能力不可丢失，shared表不重复资源。严格HTTP、数据库行/字段/revision、恢复前快照和文件比较沿用已自检比较器。
- 比较每轮导出完整正文，而非只验证success；Python前提额外验证当前schema、精确profile/resource闭包、所有引用及源正文。Go相关import/export仍未接通。

```text
.63 run.py --groups backup-format-roundtrips
current go-unimplemented=12 design_errors=[] seconds=8.85
cumulative compared=1470 go-unimplemented=4617 dependency=28 different=41 O12-separate=1
request_variants=6157 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重11
42 self-checks + end-to-end wrong-Go-response check OK
```

继续未完成的生成器与业务分支，未做最终全量。

## 批次 36：G16订阅hosts、DNS策略和artifact报告

- 13场景26个preview/artifact请求：无hosts、域名映射、双栈IP映射、手写hosts优先、只匹配显示名不匹配server、环、DNS关闭、缺proxy nameserver、已有DNS策略冲突、whitelist覆盖、双源冲突、已知旧/新内核。比较生成hosts/policy/filter、完整subscription_hosts报告及artifact全部成员/源快照/manifest，沿用故意出错自检过的artifact比较器。
- 域名映射经过自己的DNS listener，并将真实目标加入fake-ip-filter；IP映射无需proxy DNS策略。未知版本不凭seed.version判老；旧/新版本通过真正Agent Bearer认证的traffic-report公开上报，2个请求实际Flask↔Go比较通过，随后报告精确列出旧内核、保留最低版本说明。
- 核心版本前置上报在config R路由外，runner仅明确允许G16引用这一条已在routes.tsv声明的协议路由；未放开任意未知路由或改变config路由总数。上报JSON行文件中的status.at验证后才按实际请求时间比较；其他字段及文件完整比较。

```text
.63 run.py --groups subscription-hosts-reports
current compared=2 go-unimplemented=26 design_errors=[] seconds=15.04
cumulative compared=1472 go-unimplemented=4643 dependency=28 different=41 O12-separate=1
request_variants=6185 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重12
42 self-checks + end-to-end wrong-Go-response check OK
```

Go生成器尚未接通；这些用例未计为Go通过。继续G10–G13及其余业务/长流程，未做最终全量。

## 批次 37：G11 OxiDNS通用控件输出

- 198请求，11控件×关闭/合法defaults/自定义×默认/scoped×草稿render/保存settings/已保存preview。覆盖主缓存和探测缓存、API、log、query recorder、IP selector、动态域名及学习、AdGuard、ROS地址表、ROS路由、rule_update。
- 核对主缓存short_circuit=true与探测缓存false、lazy TTL=0必须省略、dump放state、API基础认证/TLS资源路径和生成正文里的合成凭据、日志rotation、学习插件独立开关、RouterOS TLS/async布尔及规则download→reload→cron执行引用。保存的SHA ETag、publish guard、完整DB及文件沿用既有严格比较；本批验证的合成API/ROS凭据在render/preview中保留，内部规则能力URL另由G15使用真实ZIP配置验证。
- 这是生成形态验收，不启动DNS核心、不会连接RouterOS或执行cron；不存在将预览当成运行时功能通过的情况。Go相关路由全未接通。

```text
.63 run.py --groups oxidns-control-output
current go-unimplemented=198 design_errors=[] seconds=66.75
cumulative compared=1472 go-unimplemented=4841 dependency=28 different=41 O12-separate=1
request_variants=6383 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重13
42 self-checks + end-to-end wrong-Go-response check OK
```

继续原生附件/草稿编辑/交付图及其他剩余清单，未做最终全量。

## 批次 38：G10原生include、附件原文与运行文件隔离

- 43请求：native文本/base64、template自定义provider、三层include和合法父路径、API TLS/WebUI/opaque binary资源闭包、cache/query/learned运行文件，默认/scoped均preview+完整ZIP+artifact。include按shared→first→middle→last展开，只导入插件，保留主runtime=3，附加文件原文留在包内。
- 新增managed-assets原始字节比较器，ZIP附件和artifact解包的附件均逐字节比较，config.yaml仍由生成结构比较器比较；CRLF、中文、尾空格、NUL/无效UTF-8都不能被文本归一化吞掉。故意CRLF改LF、删成员、加成员、重复成员均被自检拦住，比较器自检共43项。
- 公共preview可200但缺资源/运行输出与附件冲突的完整ZIP必须400；state/cache.dump、queries.db、learned.txt无法声明成可部署附件。生成前外部准备的运行文件保持原样，不能进入ZIP/artifact或被生成覆盖。
- TLS/opaque资源仅证明路径闭包和打包原文，使用合成占位内容；没有启动核心、TLS握手或声称opaque数据是可加载的核心数据库。Go相关路由仍未接通。

```text
.63 run.py --groups oxidns-managed-assets,oxidns-asset-guards
current go-unimplemented=43 design_errors=[] seconds=18.52
cumulative compared=1472 go-unimplemented=4884 dependency=28 different=41 O12-separate=1
request_variants=6426 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重14
43 self-checks + end-to-end wrong-Go-response check OK
```

继续G12/G13及其余业务/长流程；未做最终全量。

## 批次 39：G12三方草稿、重命名和标准插件恢复

- 44请求，默认/scoped：template→editor native+baseline；无变化保留中文备注原文字节；手改cache和未知插件/字段与模板worker/TTL独立合并，保留false/0/null；同时修改worker的冲突默认current与显式template resolution分别验证精确冲突路径/choice，更新baseline后重预览不重复变更。
- tag-only重命名remote_dns自动改goto，显式重命名cache且修改size会改明确executor引用；合成认证凭据保持精确。另验自定义username文本含旧tag时400并指出位置，而不是猜测改写。标准cache删除后断开自有边、停用并暂存参数；standard_add恢复原size和开关；错型/重复/未知intent及无效resolutions均400。
- 每次只编辑草稿时Python前提比较完整数据库必须等于最初快照；只有显式save才提交。设备DNS策略的中文/emoji备注往返无损。save/read/原始导出保留native与secret-bearing baseline；desensitize导出清空native/baseline/附件，沿用SHA ETag、全DB与文件比较。

```text
.63 run.py --groups oxidns-editor-roundtrips
current go-unimplemented=44 design_errors=[] seconds=4.64
cumulative compared=1472 go-unimplemented=4928 dependency=28 different=41 O12-separate=1
request_variants=6470 named_matrix_cases=3320（S480 F2387 B453），H去重217，G去重15
43 self-checks + end-to-end wrong-Go-response check OK
```

Go相关editor路由仍未接通。G13及更多helper错误分支、聚合/域名发现/setup等继续，未做最终全量。

## 批次 40：聚合CRUD、用途、计数与排序

- 57请求覆盖R001–R006/R009/R010：列表空/旧统计，生成agg_8hex身份与默认health、订阅/手动/混合/空源、Unicode，PUT整对象替换且路径ID优先/统计清除；不存在读/改404、删除幂等、引用自1/2空间阻止删除409、缺引用/非法health400、SQLite写失败500。
- cache-only count覆盖真实nodes、旧count-only、空、同名不去重、停用手动节点剔除、坏count触发500。排序支持top/bottom/旧对象只取顺序；重复/错型/未知ID404、数组body500，旧对象中的坏引用和名字不会被写回。
- 真实HTTP确认并修正R002-F09/F13 oracle：此创建链未校验空/重复名称或存储regex，因此200保存；清单保留编号并注明Python来源，不人为要求Go400。
- 新增agg身份比较的坏格式/重复使用自检，Go复用ID记录差异而非设计错误；共44项自检。其他HTTP/全DB/revision/文件严格比较照常。Go聚合路由仍未接通。

```text
.63 run.py --groups aggregation-crud,aggregation-validation,aggregation-count-order
current go-unimplemented=57 design_errors=[]
cumulative compared=1472 go-unimplemented=4985 dependency=28 different=41 O12-separate=1
request_variants=6527 named_matrix_cases=3369（S497 F2407 B465），H去重217，G去重15
44 self-checks + end-to-end wrong-Go-response check OK
```

继续聚合交付/预览与域名发现等剩余路由；未做最终全量。

## 批次 41：聚合provider、preview和真实源刷新

- 53请求，默认/scoped(other与冲突default header)的YAML、Surge、Loon、include/exclude regex、空集合、Unicode、手动节点与订阅同名去重；验证实际节点行数，Surge/Loon使用可输出的SS fixture，不能把空文本200当转换成功。
- 非法过滤分别到达不支持YAML筛选与文本regex编译两条分支；未认证401、missing404、disabled403、缓存拨号图错误400、provider目标路径为目录500。preview核对实际生成文件后的nodes/count，空/缺失/非法图/磁盘失败分别验证。
- 真正Source HTTP首次拉取、403旧缓存降级、HEAD首次拉取均有精确请求次数断言。HEAD body为空但source cache及profile provider文件已生成；preview也覆盖403降级。定时源间隔通过已接通的公开订阅PUT在启动后开启，7个前置请求实际Flask↔Go通过；没有绕开X08默认fixture不启可刷新源约定。
- 当前URL来源信息完备的cache不在交付请求按时间刷新；403降级用缺URL来源信息的旧cache，确实触发网络请求。全部缓存/回执/health/文件原文与DB比较保留，不将源失败吞成未发生IO。Go聚合交付仍未接通。

```text
.63 run.py --groups aggregation-provider-formats,aggregation-provider-refresh
current compared=7 go-unimplemented=46 design_errors=[] seconds=49.30
cumulative compared=1479 go-unimplemented=5031 dependency=28 different=41 O12-separate=1
request_variants=6580 named_matrix_cases=3405（S509 F2422 B474），H去重217，G去重15
44 self-checks + end-to-end wrong-Go-response check OK
```

聚合慢源多源完成次序、不能从公共边界单独触达的读取配置异常等仍待补齐/可达性审计；域名发现等继续。未做最终全量。

## 批次 42：域名发现设置/忽略/备注与OxiDNS真实下载地址

- 85个域名发现请求：默认/已选择library与用途、失效引用、auto开关/80/90/95及float90.0、端口边界/占用/错型、false/0/空容器等falsy值清默认库、非法目标与类型；ignore去重/删除幂等/2000容量、原始中文400与punycode200；备注修剪/修改/删除、200字符Unicode与2000已有项可更新。H045–051/H054–056逐分支到达，10个新增H编号。
- 修正测试seed中无效的auto_probe_enabled为实际auto_probe=false/auto_apply=false，并为ignore/note写后的profile.updated_at加入已有时间窗口比较。先前31条fixture/时间噪声已重跑消除；未提交为业务发现。其余旧组的seed变化留待最终全量重跑。
- 新增**真实差异1项**：R058-B03 / stored-legacy-extension，Python200后删除已存储domain_discovery.synthetic_legacy_extension，Go200仍保留该对象；其他响应/DB行一致，合成值仅flag=false和空列表。已写findings，未改Go。84条实际比较通过。
- G15新增OxiDNS的32个地址/能力组合，读真实ZIP config.yaml，精确验证rule_proxy_token而不是config_token/JWT；完整ZIP含源规则，与fake零外网请求/DB/文件比较。普通preview会把能力URL替换成[REDACTED]，不能用它验私有URL。本轮G15共128请求仍Go未接通；MosDNS中间下载URL依然部分覆盖，待可观察性审计。

```text
.63 run.py --groups discovery-settings,discovery-ignored,discovery-notes,generator-url-capabilities
current compared=84 different=1 go-unimplemented=128 design_errors=[] seconds=50.15
cumulative compared=1563 go-unimplemented=5063 dependency=28 different=42 O12-separate=1
request_variants=6697 named_matrix_cases=3455（S526 F2442 B487），H去重227，G去重15
44 self-checks + end-to-end wrong-Go-response check OK
```

继续adopt/undo/初始化、实际Agent报告与job、G13及其他未完成项；未做最终全量。

## 批次 43：域名采纳/撤销、容量和历史

- 41个真实Flask↔Go请求全部通过：DOMAIN-SUFFIX direct、DOMAIN proxy、重复/已覆盖跳过、punycode、500条上限、1000历史淘汰最旧记录、manual/auto/中文legacy来源读取；undo两个目标和不存在值，保留原注释/其他行。每次缓存原文字节必须等于提交的library.content；other空间完整行不变。
- 缺items/空/501/非对象项、IP/中文原文/非法type/target、未配置可写库、undo非法值/目标均精确分支，失败前后Python全库不变并与Go全库/文件比较。H060–063/H065新增5个编号。
- 容量fixture首次暴露Linux单个argv上限；harness现把seed选项写独立临时JSON文件，seed脚本兼容原有JSON参数。真实1000条history fixture证明该通道可用，不改业务入口或DATA_DIR比较范围。
- `applied_at`为秒级，普通连续写入之间等到两端前次完成之后的下一秒，避免时间戳平局在不同进程形成不同排序。混合direct/proxy单列，固定Python hash seed=0且不排序结果，HTTP added与DB history原始顺序都比较通过。未固定seed首轮出现过相反顺序，Python源码的set遍历导致跨hash环境不稳定；findings新增明确oracle限制，其他hash环境的顺序保证仍部分覆盖，不列Go回归。

```text
.63 run.py --groups discovery-adopt-undo,discovery-write-guards,discovery-mixed-order
current compared=41 design_errors=[] seconds=25.57
cumulative compared=1604 go-unimplemented=5063 dependency=28 different=42 O12-separate=1
request_variants=6738 named_matrix_cases=3480（S535 F2451 B494），H去重232，G去重15
44 self-checks + end-to-end wrong-Go-response check OK
```

默认规则库写前改变的CAS竞争、规则库初始化、实际Agent报告/job及G13等继续，未做最终全量。

## 批次 44：域名规则库初始化与ID格式校验

- 15请求：direct/proxy/both、空targets默认两者、重复target、重复初始化、名称碰撞加-2、引用位于MATCH前；坏targets/未知policy及H052/H053无任何DB/revision写入，6个失败输入实际比较通过。
- 真实Python确认R056-S04不是幂等：重复调用分配新库/引用且切换默认指针。重复target被接受，分配多份库/引用，较早同target库未被选中且不写缓存。清单已保留编号更正。每次Python新库/规则UUID4、数量、唯一性、选中库确定性cache路径及未选中库无cache均独立验证。
- 新增ID格式/重复分配故意错误自检，45项。**9条差异记录首先被格式校验拦住**：Python library是UUID4 hex，reference是带横线UUID4；观测到的Go library为hex32且不全满足UUID4位，reference为无横线hex32。未为此放宽格式或直接mask ID；findings保留完整两侧ID、DB、文件差异。是否把资源ID当作opaque而允许此形态差异需审查，不能把格式失败后的其他字段归一化称为通过；未改Go。

```text
.63 run.py --groups discovery-init,discovery-init-guards
current compared=6 different=9 design_errors=[] seconds=13.49
cumulative compared=1610 go-unimplemented=5063 dependency=28 different=51 O12-separate=1
request_variants=6753 named_matrix_cases=3488（S539 F2453 B496），H去重234，G去重15
45 self-checks + end-to-end wrong-Go-response check OK
```

继续实际Agent报告/job、区域路由、G13与其他剩余清单；未做最终全量。

## 批次 45：真实Agent上报与域名读取筛选

- 32个真实Flask↔Go请求全部通过：合成Agent向两端发相同鉴权heartbeat/traffic-report（含rule_provider确定性hash响应与traffic JSON写入）；启用状态、旧/未知版本、独立开关、重启丢失内存heartbeat后状态分别验证，未用仅种DB冒充已上报。
- incoming协议写后已比较后，向两端放入相同合成旧日桶，用三个当前域名和一个3日前域名明确区分1/7天；uncovered/failing/all/ignored、指定/未知Agent、days错值/0/2/空格1/1_0/-1/Arabic7、非法view的精确集合均比较。固定旧日期字段不忽略，当前上报时间只按明确字段/时间窗口比较。
- query profile=other覆盖default header，Agent及region记录不跨空间；无Agent/无报告/无区域结果默认数组和对象形态完整比较。T20仅明确允许这两条公共协议前置路由，不增加config R路由数。

```text
.63 run.py --groups discovery-reported-selectors,discovery-agent-versions
current compared=32 design_errors=[] seconds=9.22
cumulative compared=1642 go-unimplemented=5063 dependency=28 different=51 O12-separate=1
request_variants=6785 named_matrix_cases=3507（S549 F2453 B505），H去重234，G去重15
45 self-checks + end-to-end wrong-Go-response check OK
```

T20尚未完成：这里是Agent→面板的上报和读取，面板→fake Agent的probe/website/region请求、批次/错误/超时、job进度及自动化仍待后续；G13和其余helper/路由继续，未做最终全量。

## 批次 46：异步预检任务与严格生命周期比较

- 新增33个实际Flask↔Go请求全部通过：probe无Agent/缺默认代理、region未开启/无Agent均202后失败；完整最终job对象、准确中文message、0进度/空errors、先后时间、同ID稳定、跨profile404、重启后404与不存在ID。另覆盖probe空/错型/IP/中文/101项、region10域名上限/无服务无域名、website30上限/无Agent/有真实heartbeat但无近期报告。
- 202可能已经完成，也可能仍running。只有在独立验证该预检状态的全部9字段、时间及错误内容后，才投影最初响应的status/message/finished_at三个时序字段；终态仍完整比较，不掩盖错误数组。新增故意错误自检，46项通过；一般成功/部分失败任务不用这个限定为零进度的比较器。
- **修正批次44判断**：不要求随机hex ID满足UUID4版本位；这不是额外业务约定。仍验证资源前缀、lowercase hex/带横线UUID形状、唯一性与缓存/引用关系。重跑15条初始化后，9条规则引用ID的有/无横线形态差异仍保留，未改Go。

```text
.63 run.py --groups discovery-init,discovery-init-guards,discovery-job-preflight,discovery-job-guards
current compared=39 different=9 design_errors=[] seconds=34.77
cumulative compared=1675 go-unimplemented=5063 dependency=28 different=51 O12-separate=1
request_variants=6818 named_matrix_cases=3526（S550 F2469 B507），H去重234，G去重15
46 self-checks + end-to-end wrong-Go-response check OK
```

预检不等于fake Agent成功流程：实际出站协议、busy/进度/部分失败、region/website、自动化继续；未做最终全量。

## 批次 47：真实出站Agent协议、busy与失败批次

- 41个实际Flask↔Go请求全部通过。两端各提供独立的127.0.0.1 HTTP fake Agent；先真实heartbeat，再POST probe。fake首请求保持未响应时验证202、GET running 0/N、同profile busy保留job、异profile busy隐藏job及region/probe共用锁（含H071）。随后释放并轮询终态。
- 独立预期逐字段检查POST /api/domain-probe、Bearer合成token、Content-Type、DIRECT/代理🧪路径、attempts=3、timeout_ms=5000；每批18目标+百度/gtestatic两个对照，1/19/100域名和重复域名。对照不落盘，manual probe不自动采纳。失败直连+成功代理的固定统计须得到needs_proxy/85。
- 首批分别404/500/坏JSON/success=false/返回项不足，任务最终done且errors保留、下一批仍成功落盘。最终job/完整DB/每个文件/原始请求序列均比较，未使用固定相等的stub比较器。
- 两端分配端口只在精确DB Agent.port路径验证各自预先分配的整数；不mask其他数字或URL。新增端口错值/错型/错路径自检以及progress/ID/terminal变动/协议字段破坏自检，48项通过。首个fixture尝试读取未存储的history键已改为允许默认空值，未记为业务差异。

```text
.63 run.py --groups discovery-agent-probe
current compared=41 design_errors=[] seconds=17.38
cumulative compared=1716 go-unimplemented=5063 dependency=28 different=51 O12-separate=1
request_variants=6859 named_matrix_cases=3534（S552 F2473 B509），H去重235，G去重15
48 self-checks + end-to-end wrong-Go-response check OK
```

T19/T20仍部分覆盖：目前一个Agent、probe；多Agent/3 hosts选择、断连、真实超时、region/website与自动化待续。未做最终全量。

## 批次 48：区域检测Agent请求序列与目标列表错误

- 30请求全部实际比较通过。先GET /api/region-check/targets，再POST /api/region-check；Bearer/方法/路径/完整请求描述（URL、headers、markers）、target、timeout_ms=8000逐字段验证。按Python协议定义生成预期请求描述，不读Go；fake固定返回合成HTTP观察值，不代替面板做结果判定。
- DIRECT/优先代理组/首个存活香港节点共3目标；GLOBAL、内部检测组、重复地区及离线节点排除。services开/关、10域名与12请求分段；HTTP451、目标内单页error、整个代理target失败后其他target继续。终态errors、regions完整文件/DB与R053公开摘要全部比较。
- 404不支持/409缺检测入口/坏JSON/success=false/500目标列表，分别202后failed且精确中文message、零进度、无结果写入；H075有独立实际请求。错误fixture初次误写404 feature名称，已按Python公开结果修正重跑，不列业务差异。
- 更正上批文字：对照目标为百度与**gstatic**。T20仍没有调用真实公网，这里比较的是面板→fake Agent协议与面板判定。

```text
.63 run.py --groups discovery-agent-region,discovery-region-target-errors
current compared=28 design_errors=[] seconds=15.82
.63 run.py --groups discovery-region-target-errors（补H075）
current compared=10 design_errors=[] seconds=11.91
cumulative compared=1746 go-unimplemented=5063 dependency=28 different=51 O12-separate=1
request_variants=6889 named_matrix_cases=3540（S555 F2474 B511），H去重236，G去重15
48 self-checks + end-to-end wrong-Go-response check OK
```

继续website/区域采纳/自动化、多Agent及超时/断连；未做最终全量。

## 批次 49：区域服务/域名采纳与复用规则库

- 27请求，25通过、2条已有ID形态问题的新路由记录。OpenAI/Claude/Netflix/YouTube精确域名集合、指定中文emoji策略组、域名大小写/尾点/punycode、已有专用库复用/重复采纳、禁用reference补新reference、新库/新引用及插在默认代理前，均比较完整DB、history(source=region)、缓存原文字节及另一profile不变。
- 重复采纳的Python oracle更正：added=[]/skipped保留、内容/引用/history不变，但共享事务仍推进shared revision与generation一次；没有把no-op误断言为完全无提交。已重跑，全修订字段仍与Go严格比较。已有启用reference在MATCH后的形态，Python不会搬动它；保留实际行为。
- kind非法/未知service/IP/中文原文/未知策略组失败必须无部分写入；H078–081各自真实请求。新分配ID沿用独立格式/唯一性/派生cache映射校验。
- **R055-S02 new/domain与R055-B01 disabled/domain**：Python ruleset_带横线UUID、Go ruleset_hex32，映射校验拒绝并保留后续完整原始差异；同批其他25请求通过。属于已报初始化ID形态类别，不新增根因，不修改业务代码。

```text
.63 run.py --groups discovery-region-route,discovery-region-route-guards
current compared=25 different=2 design_errors=[] seconds=17.53
cumulative compared=1771 go-unimplemented=5063 dependency=28 different=53 O12-separate=1
request_variants=6916 named_matrix_cases=3548（S557 F2478 B513），H去重240，G去重15
48 self-checks + end-to-end wrong-Go-response check OK
```

继续website、多Agent/超时/断连/自动化、G13及其余路由/helper；未做最终全量。

## 批次 50：网站识别真实流量前置与页面结果

- 51请求全部实际比较通过。真实heartbeat/traffic-report先产生记录，面板经fake Agent的POST /api/region-check访问主域、www及采集子域，逐字段核对target/8000ms/marker描述与候选去重。PSL已识别的example.com/org取api子域，未知后缀的30个保留.example host按原样输入；所有真实网络仅loopback。
- 1/2/30域名、punycode、同profile busy、最终GET done；og:site_name优先、HTML实体/属性/description、登录与Powered by标题噪声、中文emoji及200码点截断，精确合成名称断言。challenge/无名称与RPC404都留下准确errors；首个RPC失败后下一域名仍成功，个别缺最近采集记录跳过而非整批拒绝。
- regions.websites只按真实采集host缓存，www成功候选保留正确host；final_url只保存hostname，不含合成private查询值。完整任务/DB/files依然严格比较，没有用单次202冒充完成。
- 夹具修正已重跑：未知.test不按根域归组；最初两个meta/1场景重名触发inventory重复键检查，现variant包含R编号。不把夹具错误计业务失败。

```text
.63 run.py --groups discovery-agent-website
current compared=51 design_errors=[] seconds=20.99
cumulative compared=1822 go-unimplemented=5063 dependency=28 different=53 O12-separate=1
request_variants=6967 named_matrix_cases=3554（S560 F2479 B515），H去重240，G去重15
48 self-checks + end-to-end wrong-Go-response check OK
```

T19/T20仍有多Agent、3 hosts、跨空间website busy、真实超时/断连；T21自动化及G13/其余路由helper继续，未做最终全量。

## 批次 51：G13订阅交付图、原始缓存与同名来源

- 77新增请求：33实际比较通过、14条已知缺少Provider注释记录、30生成入口Go尚未实现。两个源的机场内Relay命名空间/跨源策略组、精确filter选Relay、过滤掉Relay后从raw cache复制inline、两源hosts、fresh/expired/interval=0均验证。当前URL匹配的完整缓存三种年龄均零源HTTP请求，原始nodes/未过滤Relay/hosts/provenance保持原文数据不变。
- 四类输出：Mihomo main、Surge main、Loon main以及各自Provider；另验证冻结artifact完整文件。Surge/Loon含dialer的源明确400拒绝，不静默删掉dialer；不带dialer的另一源200。两端Provider先于未接通的预览运行，避免把可比较Provider隐藏在依赖skip后。
- 同名不同定义/相同定义的无chain legacy来源均保留两条；激活chain后merged Provider400。**普通main preview与manual artifact仍200，只引用HTTP Provider URL，并不在这里提前做merged Provider碰撞验证**。已按Python实际入口分开断言，没有把假设的400硬塞给Go。
- 扩展metadata校验为精确subscription ID，原默认sub-a不变；sub-b错误ID/过期时间故意自检通过，49项。补齐G13后G01–G16都有实际请求，不能据此声称其余R/H/T已完成。
- 新增14差异记录全部为旧类：12单源+2merged的Go响应/served文件缺少注释，正文节点/引用、DB、源请求和其他文件无新差异。未改Go。夹具修正了单IP hosts输出应为scalar、Surge main以资源URL而非源名称呈现、错写merged路径等前提，重跑后design_errors为空。

```text
.63 run.py --groups delivery-graph
current compared=32 different=12 go-unimplemented=24 design_errors=[] seconds=11.02
.63 run.py --groups delivery-graph-collisions
current compared=1 different=2 go-unimplemented=6 design_errors=[] seconds=4.96
cumulative compared=1855 go-unimplemented=5093 dependency=28 different=67 O12-separate=1
request_variants=7044 named_matrix_cases=3554（S560 F2479 B515），H去重240，G去重16
49 self-checks + end-to-end wrong-Go-response check OK
```

继续discovery命名helper/多Agent/断连/自动化及其余R/H/T；未做最终全量。

## 批次 52：域名发现命名helper与终态时间顺序

- 新增21请求实际通过：H057–059/H066–070/H073–074/H076–077逐分支独立公开请求（12个新增H编号；H070细分4种输入），包含2000忽略容量、31网站容量、真实heartbeat但无报告、异步前置失败、region11域名及无服务/域名；另补R054-B02 punycode成功的完整Agent请求/任务/regions文件序列。
- 通用成功任务比较器现在拒绝finished_at早于started_at，即使两者都落在允许窗口；已有自检加入故意倒序终态。T20最终poll状态码记录实际HTTP值，未从success字段推定。预检共用同一时区解析，不改变字段格式要求。
- 重跑region与四种预检job全流程；新增punycode最初与普通单域名variant重名，现统一包含R编号，inventory重复键检查已通过。没有新增业务差异。

```text
.63 run.py --groups discovery-named-branches,discovery-agent-region,discovery-job-preflight
current compared=59 design_errors=[] seconds=30.84
cumulative compared=1876 go-unimplemented=5093 dependency=28 different=67 O12-separate=1
request_variants=7065 named_matrix_cases=3555（S560 F2479 B516），H去重252，G去重16
49 self-checks + end-to-end wrong-Go-response check OK
```

H064共享规则库在读取后被改的竞态、H072同步自动job失败仍待独立到达；多Agent/断连/自动化及其余R/H/T继续，未做最终全量。

## 批次 53：多Agent、主域最多3 hosts与故障隔离

- 14新增请求全部实际比较通过。两台Agent各向两端发送真实heartbeat/traffic-report，共4个具体host，流量10/8/6/1；输入example.com只选择前三个，a/c交给采集它们的第一Agent，b交给第二Agent，d不进入任何RPC/结果文件。
- 每台Agent在两端均有独立loopback端口、独立token；逐设备保留请求顺序，严格比较其Bearer、18+2协议字段与目标，而不是给所有Agent共用一个不能辨认身份的fake。Python跨Agent迭代来自set，跨设备调用先后不排序冒充业务顺序保证；每个被标识设备的序列/次数及全局0/3→3/3均验证。
- 第二Agent500后，最终job done/errors保留该Agent错误；第一Agent的probes文件完整保留，失败Agent不产生probe结果。DB/两个traffic文件/probe文件及R052 running完整比较。已用不同token重跑，无新差异。

```text
.63 run.py --groups discovery-multiple-agents
current compared=14 design_errors=[] seconds=6.78
cumulative compared=1890 go-unimplemented=5093 dependency=28 different=67 O12-separate=1
request_variants=7079 named_matrix_cases=3556（S561 F2479 B516），H去重252，G去重16
49 self-checks + end-to-end wrong-Go-response check OK
```

继续断连、并发poll、跨空间website busy、真实超时与T21自动化及剩余清单；未做最终全量。

## 批次 54：客户端断连后完成与并发job隔离

- 4新增命名请求/序列实际比较通过。使用真实HTTPConnection发送POST后不调用getresponse、不读取响应；待fake Agent确认收到并保持未响应时，关闭浏览器连接。两端均能在公开domains.probe_job读到仍running的任务，释放Agent后完整终态/一次RPC/probes文件/DB正确，不因浏览器连接消失而取消任务。
- R052-B02各端并发12次GET，交错owner与foreign query并保持default header；逐响应比较body/header/status，owner始终同一running ID，foreign始终404且仅准确的not-found消息，不泄露进度或错误。poll前后全DB/文件不变，随后断连任务正常完成。
- 通用完成观察器可用清单R编号记录完整序列；这里只把已发生的完整断连序列记为R051-B03，没有把它当作其他写接口的T05已覆盖。

```text
.63 run.py --groups discovery-client-disconnect
current compared=4 design_errors=[] seconds=4.73
cumulative compared=1894 go-unimplemented=5093 dependency=28 different=67 O12-separate=1
request_variants=7083 named_matrix_cases=3558（S561 F2479 B518），H去重252，G去重16
49 self-checks + end-to-end wrong-Go-response check OK
```

仍待website跨空间busy、真实Agent超时、T21及其他T05入口与剩余R/H；未做最终全量。

## 批次 55：T21阈值、真实失败流量与同步job失败

- 新增29请求/序列：9条真实incoming协议前置已与Go比较通过；16条task入口未接通，4条后续已接通读取受该入口依赖。Python完整前提通过：80采纳85分，90不采纳85分，第二次真实traffic-report加入失败流量得到99分并在95阈值采纳；只写指定proxy规则库与source=auto history，另一profile全行/修订不变。
- fake Agent返回长度正确但缺paths的控制结果，使同步_start_job(wait=True)失败；经真实POST /api/tasks/run到达**H072**，不是拿异步失败冒充。job保留准确安全中文message、0/1进度、无probe半成品；task-status记录failed/count/error并释放锁。H072实际入口记录为tasks/run，未虚构发了POST probe。
- Go tasks/run当前未接通。通用harness新增显式runtime_writes：即使在途job/lock尚未落盘，未接通启动也阻断后续状态对比；接通后自动启用。最初8条“Go没有这个job”是该依赖缺失造成的测试噪声，已修正重跑消除，未作为业务发现提交。
- 完成观察器在源端继续验证终态/协议/DB/files前提，未接通时只跳过不存在的Go任务。新增故意错误自检：unwired与依赖只poll源端，真正wired时故意破坏Go最终message必须报different；使用真实SQLite/file比较函数，50项自检通过。来源端H072/95/80等计数不是声称Go实现已通过。
- 9组回归涵盖probe/region/website/多Agent/断连/预检/命名helper/region错误，188实际请求比较通过，无新业务差异。

```text
.63 run.py --groups discovery-automatic-thresholds,discovery-client-disconnect,discovery-agent-probe,discovery-agent-region,discovery-agent-website,discovery-multiple-agents,discovery-job-preflight,discovery-named-branches,discovery-region-target-errors
current compared=188 go-unimplemented=16 dependency=4 design_errors=[] seconds=86.87
cumulative compared=1903 go-unimplemented=5109 dependency=32 different=67 O12-separate=1
request_variants=7112 named_matrix_cases=3558（S561 F2479 B518），H去重253，G去重16
50 self-checks + end-to-end wrong-Go-response check OK
```

T21仍待忽略/已覆盖/3天重探/30天复检/人工apply竞争；还需website跨空间busy、真实Agent超时、H064以及剩余R/H/T，未做最终全量。

## 批次 56：T21安静轮次、3/30天边界与人工采纳重叠

- 47新增请求/序列：18条incoming前置实际比较通过；24条task入口尚未接通、5条依赖。六类安静轮次：ignored、已覆盖、新鲜probe、3天减1小时probe、30天减1小时history、最近一次recheck在30天减1小时。Python必须零Agent请求、全DB（含generation/commit）不变、原probe字节不变，同时任务成功回执且锁释放。
- 3天加1小时重新经真实fake Agent探测并采纳；30天加1小时复检proxy历史，原applied_at保持固定旧值，只更新recheck，准确记录both_ok/直连可访问/建议移除，未自动undo。边界两侧相隔2小时，不用宽泛4天/31天样例冒充准确阈值。
- 自动探测的RPC挂起期间，经公开apply采纳manual.example.net，再undo旧undo.example；放行后自动加入example.com。两条应留history（manual/auto）、两个目标规则及已撤销项准确保留，不恢复过期配置，也不丢人工写入。这里只证明后台自动任务与人工操作重叠，不代替T03同时SQL写入/CAS竞争。
- 首个真实80阈值启动/同key409分别补上R227-S03/B01编号。共享GET tasks动态时间/600秒deadline比较供全部自动化场景未来接通后使用；固定旧probe/history日期不mask。quiet的意外RPC有故意错误自检，50项通过。
- 上述完整自动化仍是Python参考前提已证明，Go任务入口未接通；没有把这些序列记成Go实现通过，没有新业务差异。

```text
.63 run.py --groups discovery-automatic-thresholds,discovery-automatic-quiet,discovery-automatic-aging
current compared=27 go-unimplemented=40 dependency=9 design_errors=[] seconds=25.62
cumulative compared=1921 go-unimplemented=5133 dependency=37 different=67 O12-separate=1
request_variants=7159 named_matrix_cases=3560（S562 F2479 B519），H去重253，G去重16
.63 python -m unittest discover -s server/tools/configdiff -p test_compare.py
Ran 50 tests in 0.775s; OK
```

继续website跨空间busy、真实Agent超时、H064、P边界及其余R/H/T；未做最终全量。

## 批次 57：GET入口P3–P6与不同空间的实际内容

- 新增137个读取变体，54实际比较通过、83入口尚未接通；新增101个原清单B编号。inventory仅从已审表中提取明确P3–P6编号，检查范围一一对应；不创造编号来填覆盖数。统计/dependencies等需专用观察的入口不借此覆盖。
- 逐入口验证X-ConfigFlow-Profile：URL覆盖query/header、query profile覆盖profile_id、header生效、无selector稳定默认。清单明确无URL profile的P3按query覆盖header测试，显式URL始终优先；原编号/数量不变。
- 另一空间有不同Mihomo原文、策略组、MATCH规则、ignored列表；不仅查头，完整响应参与差分。Mihomo原文、discovery ignored和Agent绑定列表另作独立准确值检查。Agent列表只验证持久绑定归属，未用种DB冒充在线/heartbeat状态。
- 每次源端读取全DB/revision/commit必须不变，生成读取允许的文件副作用仍按完整文件比较。设置/列表/拉取使用真实有效空配置；动态业务对象模板明确用不存在ID检查选择器，此P覆盖不替代各路由独立的成功/对象失败业务fixture，也没有全表发坏JSON。
- custom-config只有生成的default_config按YAML/INI语义比较，用户编辑config原文字节继续严格；保留字段类型，不把null当空文本。新增默认模板变化/编辑注释或CRLF丢失/类型错误故意自检，51项通过，无新业务差异。

```text
.63 run.py --groups profile-read-selectors
current compared=54 go-unimplemented=83 design_errors=[] seconds=53.98
cumulative compared=1975 go-unimplemented=5216 dependency=37 different=67 O12-separate=1
request_variants=7296 named_matrix_cases=3661（S562 F2479 B620），H去重253，G去重16
.63 python -m unittest discover -s server/tools/configdiff -p test_compare.py
Ran 51 tests; OK
```

继续写入入口P边界、custom-config业务用例、website跨空间busy/真实Agent超时、H064与其余R/H/T；未做最终全量。

## 批次 58：补custom-config P3与在线Agent无推送观察

- 复核已有custom-config基本/合并/64KiB/坏JSON/存储故障/legacy native隔离用例后，只补缺口，未重复宣称全部新实现。五种编辑器各新增P3有效写入：query default覆盖header other，仍精确检查选中字段及另一profile不变；原P4–P6保持原编号/variant。
- 每种编辑器启动独立fake Agent，真实授权heartbeat证明在线后再保存草稿。fake记录GET/POST/PUT/PATCH/DELETE/HEAD全部方法，不漏掉PUT发布。最后读取等待6秒，覆盖Python rule-sync的5秒扫描周期；源端整个fixture零Agent HTTP请求/错误，说明默认后台约定下保存草稿不发布。这里是有界观察窗，不宣称无限时长或启用其他同步功能后的行为。
- Agent端口分别分配，两端只在DB Agent.port与公开Mihomo reserved_ports[0].port精确映射已分配整数；owner/其他字段和未知端口继续比较。生成default_config复用语义投影，编辑原文保留；新增真实fake各HTTP方法与故意Go PUT破坏无请求期望的自检，52项通过。
- 20新增变体：5条X08在线前置实际Go比较通过、15编辑/读取入口尚未接通；新增5个原P3写入B编号。原失败/完整DB/files/publish guard检查重跑通过，无新业务差异。在线heartbeat作为X08唯一明确的非config公开协议前置，不改变228路由分母。

```text
.63 run.py --groups custom-config-basic,custom-config-selectors,custom-config-failures
current compared=5 go-unimplemented=106 design_errors=[] seconds=57.71
cumulative compared=1980 go-unimplemented=5231 dependency=37 different=67 O12-separate=1
request_variants=7316 named_matrix_cases=3666（S562 F2479 B625），H去重253，G去重16
52 self-checks + end-to-end wrong-Go-response check OK
```

继续其余写入P边界、website跨空间busy/真实Agent超时、H064及其余R/H/T；仍未最终全量。

## 批次 59：内部异常英文例外不能吞掉中文

- 收紧明确标注的internal_errors字段：只允许已验证prefix之后的纯ASCII异常措辞变化；tail出现中文/emoji等非ASCII时回到完整字符串比较，中文中的数字变化也必须报差异。固定prefix仍逐字节检查，未知字段/状态/DB/files始终不受这个例外影响。
- 新增故意中文替代提示、中文数字变化、同文正例等自检，52项全部通过。本批新增请求0，历史记录仍7316 variants（R3666/H253/G16），在重新执行前不把历史67项差异视为新规则下的全量结论。

```text
.63 python -m unittest discover -s server/tools/configdiff -p test_compare.py
Ran 52 tests in 1.354s; OK
```

开始在.32跑当前全部group，审核早期批次与seed/比较器变更的兼容性；仍有未实现的设计条目，这不是最终验收，结果与修正将在后续批次提交。

## 批次 60：.32 当前全部 group 审计

- 在.32重新执行全部已注册group，覆盖早期批次与当前seed、严格中文异常比较器的组合。52项比较器自检及故意错误Go响应检查通过；无用例设计错误；新旧67项差异的case/variant集合一致，没有新增业务差异。
- 当前7316请求变体：实际比较通过1980，Go尚未实现5231，依赖未实现37，业务差异67，O12单列1。原矩阵去重R3666（S562/F2479/B625）、H253、G16；这些是已有实现范围，仍有设计条目未完成，不作为Step B最终验收。

```text
RT_HOST=sunbroke@192.168.0.32 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc '~/cf-test/.venv/bin/python server/tools/configdiff/run.py > /tmp/configdiff-config-all-audit.log 2>&1; rc=$?; tail -10 /tmp/configdiff-config-all-audit.log | head -9; exit $rc'
last request: R192-B08 / P6/all-config-read-selectors / go-unimplemented
current compared=1980 go-unimplemented=5231 dependency=37 different=67 O12-separate=1
design_errors=[] seconds=1252.99 end_to_end_mutation_check=true
exit=1（既有67项差异保留为失败；未实现项不算失败）
```

继续rule-proxy、其余写入P边界、setup/native OxiDNS与剩余H/T；最终还需在.32完整重跑。

## 批次 61：MosDNS rule-proxy 真实上游、认证、缓存和限制

- R087/R138新增204请求变体，194实际比较通过、10差异；新增原R编号66（S22/F32/B12）及H106–H114九个helper编号。三种认证环境分别验证JWT、当前内部能力的query/Bearer角色、退役优先级、超长凭据、公开pull token；有效JWT优先于退役query的真实Python行为也保留。
- 四种输入（classical/YAML/list/native）分别验证domain/ip/混合，含正则中的逗号、原生注释、BOM/中文、坏UTF-8的声明与未声明路径、空过滤结果；精确5MiB通过，Content-Length超限与无长度的实际流超限均400。fake支持显式/省略长度，不重复Content-Length；首部与流超限独立真实客户端验证。
- 声明源缓存零下载、失败回执的stale头、首次刷新持久缓存、无缓存下载失败502、source_id引用/不存在/变更/歧义、MRS及声明类型错误、URL语法、非2xx与重定向环；每次完整SQL配置不变，并比较缓存、task-status和通知文件。S91由HTTP代理fake记录改写后的absolute URI请求行、Host及鉴权字段，禁止外部DNS/网络与凭据泄漏。
- inline跨profile相对URL、query相对URL、禁用共享源均不走快捷解析；绝对自身API带query由Python进程内分发。清单更正：重定向超限实际400；内部能力会自动生成，C1指未配置管理/公开pull且未提供内部能力；library_enabled是共享源enabled解析所得，不能直接存该引用字段。
- AUTO(T16/17)负向观察：显式仅允许127.0.0.1已分配DAV端口的备份fixture，启动安静期6秒后逐端点读取、再观察6秒，Python与Go均零DAV请求/对象且SQL不变。默认fixture仍强制auto_backup=false，不能借新选项接外网。新增源HTTP framing/代理字段破坏/意外备份请求与对象/备份fixture边界自检，55项通过。
- 新发现10条：两个入口各有F13声明内容无效、B04坏UTF8、S90无缓存503的通知缺失（6）；B91自身绝对URL带query Python200而Go502（2）；F16真实task-status路径目录冲突Python500而Go400（2）。已写findings，未改Go业务代码。真实Agent超时、其他helper故障/重试等仍待后续，不把本批当所有H-FETCH完备。

```text
.63 run.py --groups rule-proxy-credentials,rule-proxy-conversions,rule-proxy-local-cache,rule-proxy-guards,rule-proxy-declarations,rule-proxy-size-limits,rule-proxy-transport,rule-proxy-processing-failure,rule-proxy-no-backup
.63 run.py --groups rule-proxy-no-backup（显式fixture修正后2 compared；40.65s）
.63 run.py --groups rule-proxy-local-cache（补无缓存503，16 compared /4 different；16.36s）
all rule-proxy groups: compared=194 different=10 design_errors=[]
cumulative variants=7520 compared=2174 go-unimplemented=5231 dependency=37 different=77 O12-separate=1
R去重3732（S584 F2511 B637），H262，G16；55 self-checks + wrong-Go-response check OK
```

继续OxiDNS各原编号、其余写入P边界与剩余H/T；不是最终.32验收。

## 批次 62：OxiDNS各编辑路由原编号、结构差异与P选择

- 对R106–R114/R139–R147中现有G11/G12未按原R编号记录的路径补公开HTTP用例，不用新增编号替换原设计。common读取实际native值/缺失能力原因；common修改cache/upstream/规则路由/AdGuard、同值幂等、中文设备备注保留、空列表、未知键/自定义链拒绝。
- editor的template/native/空模板、原文注释和附件；render template/native/Host默认根、空附件、原生草稿中手写能力URL完整渲染（不把能力当普通metadata脱敏）；validate完整/不完整/引用错误/未知插件逐路径诊断。仅render生成YAML按结构比较，编辑native_config、附件及baseline仍逐字节比较；新增故意生成值/类型破坏自检，56项通过。
- template-preview无冲突/current/template选择、手写未知类型字段/中文附件保留、错型resolutions/template参数、清空仍被手写插件引用文件拒绝。native apply完整编辑/tag引用同步、standard_add恢复、删除受管cache、各错型/覆盖/动态引用/共享引用/新增同时删除依赖；references精确到候选文档数组索引，message位置后缀也逐字节检查。中文/emoji重命名tag按ASCII命名约束拒绝400。
- 35个有效草稿P3–P6与5个settings有效写入P选择；另2个settings B02检验未知metadata和能力的编辑器豁免。每次草稿操作全DB/revision/commit不变；settings写入检查实际选中字段、另一空间完整行及ETag/publish_guard，随后完整DB/files差分。
- 逐端点确认F11边界并补清单errata：重复插件可保留为草稿/common投影及无关scalar修改，不统一误断言400；validate错误是200 envelope，未知插件仅warning、valid=true但仍需目标核心确认。没有Go业务改动，所有本批路由尚未接通，Go连接后自动执行。

```text
.63 run.py --groups oxidns-named-common-reads,oxidns-named-common-applies,oxidns-named-editor-render-validate,oxidns-named-template-previews,oxidns-named-native-applies,oxidns-named-structure,oxidns-named-selectors,oxidns-named-stored-settings
.63 run.py --groups oxidns-named-editor-render-validate（补手写能力URL的有效native字段，源端前提通过）
56 comparator self-checks + end-to-end wrong-Go-response check OK
```

覆盖精确计数见同提交results.json；继续setup状态机、helper专用分支与其余T。尚非最终验收。

本批212请求变体均go-unimplemented，新增原矩阵编号172（S44/F54/B74）；累计7732 variants，R去重3904（S628/F2565/B711），H262、G16。累计compared2174 /go-unimplemented5443 /dependency37 /different77 /O12-separate1；design_errors=[]。

## 批次 63：setup创建/重入/输入边界与真实chunked客户端

- router/gateway新session、相同id/name/goal重入（SQL及所有文件完全不变）、改目标/名409、64个中文/emoji码点、首次生成43字符public config_token、各参数/JSON/32KiB Content-Length边界、预先占用setup_派生profile id。H1012/1013/1015各发独立请求验证准确错误分支。
- 创建响应与setup-sessions私有JSON文件完整相等，新profile的SQL名称/派生id/初始进度一致；完整SQL、文件内容和模式参与Go差分。随机public token只允许新生成token_urlsafe(32)形态且不能为已知旧角色，显式绑定两个新能力；内部能力与所有其他字段不放宽。
- 修正初次进度误记：首轮R208-B90实际413、退出2，不能记为成功。真实客户端自检证明线上发送chunked且无Content-Length；另用真实Waitress的WSGI回显自检证明其解码后补上CONTENT_LENGTH，所以公开生产入口在_body处拒绝413。保留生产HTTP入口，不改用直接Flask client来迁就原清单。B90实际发送>32KiB有效JSON并检查413及零SQL/文件变更。
- 初始session观察器故意破坏profile/goal/类型/进度/资源归属必须失败；chunked自检逐块读取并验证完整正文、TE与无CL；Waitress长度重建自检独立验证外部传输与路由观察的区别。结果与计数待本批修正后重跑确认。

继续setup列表/读取/进度、来源/prepare/check/finish/abandon与相关H；并发和Agent协议尚未由本批覆盖，未最终验收。

```text
.63 run.py --groups setup-create,setup-create-guards（修正后）
current go-unimplemented=26 design_errors=[] seconds=8.72
cumulative variants=7758 compared=2174 go-unimplemented=5469 dependency=37 different=77 O12-separate=1
R去重3918（S631 F2573 B714），H265，G16；新增原R编号14（S3/F8/B3）
59 comparator self-checks + wrong-Go-response check OK；exit=0
```

## 批次 64：setup列表、兼容session、进度和并发创建

- 75新增请求/序列变体均Go尚未实现，新增原R编号42（S12/F21/B9）、H编号7。R207初次/已有设备/明确dismissed/已有session、禁用资源和非链接订阅过滤、中文名称；已有设备仅需持久绑定而不依赖在线，不伪造heartbeat覆盖在线分支。
- R209未准备/已准备兼容记录、旧Linux gateway takeover推导、损坏JSON/错型500、非法/不存在session、profile删除404、中文备注。兼容session由Python在启动前创建，完整SQL与私有JSON复制给两端；prepared兼容快照只验证读取兼容，不冒充完成实际安装/发布/check流程。路由器引用fixture明确disabled/auto_sync=false，不接触真实设备。
- R210合法step/0/空对象、manual/managed、当前profile router、真正abandoned记录继续后去标记；各字段/类型/越权router/gateway目标错误、Content-Length及真实chunked超32KiB、profile缺失/非法session。成功progress只写session，明确全配置SQL不变；响应与原子session文件相等。
- R211前置错误：坏JSON/超限/未知operation/abandon参数/check与finish未准备/完成后abandon；session查找先于大正文校验，不存在session带超限坏JSON仍404；profile已删除404。H1010/1011/1014/1045–1048分别独立发请求验证准确消息，未借用编号填数。
- Stable只读观察器分别捕获每端before/after的完整SQL及文件，在同一端内逐值比较，不采用跨端身份/时间归一化。因此读请求或拒绝写入偷偷改commit_id、updated_at、文件即使仍在允许时间窗内也能报差异；故意改这三类值的自检通过，60项自检全部通过。
- R208-B03真正两个HTTP线程同时向一个session发创建（barrier同步），必须两次200且完整响应相等、session文件一致、只增加一个profile section和一次generation；Go接通后比较两次原始响应及完整最终DB/files，不按完成顺序排序掩盖错误。

```text
.63 run.py --groups setup-create,setup-create-guards,setup-list,setup-read,setup-progress,setup-action-guards
.63 run.py --groups setup-concurrent-create
current concurrent: go-unimplemented=1 design_errors=[] seconds=2.93
cumulative variants=7833 compared=2174 go-unimplemented=5544 dependency=37 different=77 O12-separate=1
R去重3960（S643 F2594 B723），H272，G16
60 comparator self-checks + wrong-Go-response check OK；exit=0
```

R208创建、R207列表、R209读取、R210进度的所有原R编号已有至少一个有效变体；不代表每个多形态完全穷尽。下一步R211 source/prepare/router-draft/check/finish/abandon、相关H与并发/断连；最终还需.32全量。

## 批次 65：setup来源选择和H1023–H1029

- 52新增请求变体均Go尚未实现；新增原R编号5（S3/F1/B1）、H编号7。现有订阅/节点分别去重保序，step/max_step=2、SQL不改而session原子更新；HTTP URL trim、覆盖已有选择、重复/变更URL只保留一个setup_sub固定id，精确检查名称、enabled、86400秒刷新间隔和清空node_ids，中文名称保留。
- HTTP来源用真实回环fake并比较完整请求记录，在source接口返回边界两端均不应立即下载；新源会成为后台调度输入，不宣称长期零请求。fake保持活到两端服务关闭，不可能误发到外网；后续自动刷新轮次仍属于T27。
- 已准备后禁止改来源、两个ID数组错型/非文本/101项、URL错型/8193字符/协议/host/port/括号、已删除或停用资源、内联占位订阅、完全空选择，逐个真实发送R211-F17和对应H编号，精确消息/400或409，Stable验证拒绝操作每端SQL/files完全不变。

```text
.63 run.py --groups setup-source,setup-source-guards
current go-unimplemented=52 design_errors=[] seconds=8.49；exit=0
cumulative variants=7885 compared=2174 go-unimplemented=5596 dependency=37 different=77 O12-separate=1
R去重3965（S646 F2595 B724），H279，G16
60 comparator self-checks + wrong-Go-response check OK
```

继续prepare、router-draft、check/finish/abandon及其余H/T，尚未最终验收。

## 批次 66：setup准备方案、DNS边界与真实冲突副作用

- 新增128请求变体均Go尚未实现，原R编号7（S04–07/F18–20）及H编号19。四种DNS/连接漏网组合×router/gateway，每种再重复一次；额外TLS/YAML DNS输入、query_recorder=false、复用已存密码而省略controller_secret、选中内容型域名库并去重引用。逐项检查真实SQL规则/MATCH策略/OxiDNS参数、TUN生成字段（含UDP/TCP两条hijack）、session文件/解锁进度、profile/shared revision与两种ownership digest；响应不泄漏控制器密码。只比较生成计划，不执行TUN/主机网络操作。
- H1033–H1042、H1030–H1032、H952–H957：未知/错型字段、DNS/traffic值、query recorder类型、域名列表结构/100上限/重复两侧/不可用项、DNS字节上限/YAML/16上游/协议/IPv6/回环/未指定/多播/链路本地/53或1053/认证/代理/缺DoH路径、被删除资源/旧订阅URL缓存/无可用缓存节点，精确状态和原始消息，失败的早期校验以Stable验证全存储不变。
- 通过公开scoped OxiDNS设置接口模拟其他页面编辑，再真实prepare冲突。发现Python先初始化共享内置规则库再返回409：精确检查新共享行/revision+1，同时profile和session原样保留；第二次同样冲突则全存储不变。已补清单errata，不能用“失败总无写入”误判。所有能初始化内置库的fixture设置回环拒绝代理，完整记录GET/HEAD/CONNECT及意外写入；不解析或访问真实源站。
- fake曾漏记CONNECT，现补齐并显式拒绝隧道（不伪装支持TLS）。S91重测实际序列为原站CONNECT拒绝后镜像GET，新增两条线级差异：Python CONNECT HTTP/1.0且无Host/User-Agent，Go HTTP/1.1并有标准字段；次数、目标、鉴权、最终200正文与存储相同。findings保留原始差异并明确待兼容性审核，不据此要求Go模仿Python传输栈，没有掩码吞掉这些字段。只有上述精确字段组合才显示该说明；其他业务差异不会归入。
- 增加fake对CONNECT/POST/PUT/PATCH/DELETE/OPTIONS实际记录的自检，61项通过。清单输入“DNS YAML原文”在prepare被strip首尾空白，检查持久化实际规范值而非假设保留末尾换行。

```text
.63 run.py --groups setup-prepare-plans,setup-prepare-forms,setup-prepare-guards,rule-proxy-transport
current go-unimplemented=128 different=2 design_errors=[] seconds=27.65
cumulative variants=8013 compared=2172 go-unimplemented=5724 dependency=37 different=79 O12-separate=1
R去重3972（S650 F2598 B724），H298，G16
61 comparator self-checks + wrong-Go-response check OK；exit=1（两条保留的CONNECT线级差异）
```

继续router-draft/check/finish/abandon、H1043/1044确定性竞争及其余H/T。go分支已前进到afca350b，下一步rebase并重跑现有全部group更新接通状态；仍非最终验收。

## 批次 67：rebase后端口分配器修正（无新增R/H）

- 66批已rebase到go afca350b，批次66的新head为107b3747；范围检查107个改动路径均在授权目录。随后.63全量跑到7081变体时，有36个组出现同一基础设施错误：could not allocate a distinct loopback port。该次用时1392.09s、退出2，不能作为有效全量。中间结果已保留在本机/tmp/configdiff-rebase-port-exhaustion-{results.json,findings.md}，恢复此前已提交报告后按组验证修复。
- 原因：.63的ip_local_port_range是43255–47350（4096个），harness把整个运行中每个已关闭fixture的端口永远加入全局used集合，最终与内核回收分配冲突。现在每个Pair记录自己的端口租约，等待全部进程退出后释放；restart期间保留原租约直到整个fixture结束，重复close不释放其他owner后来取得的租约。独立Waitress回显自检也明确释放自己的租约。
- 复用自检模拟内核反复返回同一个空闲端口，5000个生命周期必须成功；活动/重启保留时重复申请必须拒绝，另一owner持有时旧fixture重复close不可误释放，process.wait时租约仍保留。62项自检通过。未修改服务端口范围或Go业务代码。
- 重跑precedence、137个profile读取选择器、真实自动备份，夹具错误0。最新go上的T16 event备份现在通过，已从findings移除；其他组的累计记录仍含此前基线，必须再做同一HEAD全量刷新。

```text
.63 run.py --groups precedence,profile-read-selectors,settings-auto-backup
exit=0 design_errors=[]
.63 python -m unittest discover -s server/tools/configdiff -p test_compare.py
62 self-checks OK
```

下一步先验证工作区尚未注册的setup_lifecycle.py（router-draft/abandon）及setup.call的capture扩展，再启动当前全部group的新全量；当前报告不宣称Step B完成或新Go全量通过。

## 批次 68：setup私有路由草稿、放弃与恢复

- 新增78请求变体，均Go尚未实现；原R去重新增5（S4/F1），H新增7。iKuai/RouterOS私有草稿的新增、保留密码编辑、中文特殊名、撤回与重新生成身份；只写session，不提前创建profile router或接触设备。完整SQL与session不可变字段逐端检查，密码仅在私有文件存在，公开响应只有has_password。身份捕获器拒绝Go格式错误、编辑变号、撤回后复用旧号，不以重新绑定吞掉差异；新增故意出错自检。
- H1049–H1054逐个HTTP请求覆盖阶段/目标限制、参数key集合、错型/强制id、过早绑定Agent/auto-sync、各路由器连接字段校验和新空间策略路由限制。H987完成后拒绝放弃，拒绝请求逐端SQL/files严格不变。
- 放弃预览与实际执行分别覆盖未使用、默认空间、已绑定Agent、已有路由、已有安装记录；核对真实profile/session删除或保留、全局dismissal，以及拥有订阅/cache清理。其他空间仍引用的订阅及缓存必须保留，其他空间SQL原样。T23通过公开GET核对列表、suggested与abandoned/reopen状态。
- 真实公开序列先dry_run显示可删，再通过公开rebind让Agent占用，confirm必须重算并保留，随后PATCH恢复。覆盖用户预览与确认之间的竞态；没有声称确定性覆盖abandon函数内部plan与delete事务之间的极短竞态。安装占用只用合成持久记录，不冒充完成安装或安装检查。

```text
.63 run.py --groups setup-router-drafts,setup-router-draft-guards,setup-abandon,setup-abandon-shared-source,setup-abandon-binding-race
current go-unimplemented=78 design_errors=[] seconds=18.83；exit=0
cumulative variants=8091 compared=2173 go-unimplemented=5802 dependency=37 different=78 O12-separate=1
R去重3977（S654 F2599 B724），H305，G16
63 comparator self-checks + wrong-Go-response check OK
```

下一步重新全量刷新当前go基线，再继续check/finish与余下H/T；仍未完成Step B，最终.32全量尚待执行。

## 批次 69：setup安装检查、完成门禁及并发核心改动

- 187新增请求/序列变体：163 Go尚未实现、24受前置未接通setup/rebind写入阻断的心跳依赖。新增原R编号6（S2/F2/B2）、H17、T22；和此前清单编号去重合计R3983（S656/F2601/B726）、H322。此处是覆盖并集，不冒称当前全部组已在同一HEAD完成：批次68全量仍在.63运行，本批独立报告暂存/tmp/configdiff-setup-completion.json，下一批再合并刷新统一results.json。
- router/gateway分别真实调用create→source→prepare→rebind→两Agent公开心跳→check→finish及重复检查/重复完成。发布收据由两端相同的合成持久记录提供，仅作为安装完成的边界输入，不执行发布、安装、转发或TUN。两端用实际HTTP GET /api/linux-gateway/status、Bearer设备token、Accept application/json、空正文核对协议；OxiDNS和路由器哨兵不应收到请求。SQL、session、安装记录、私有草稿凭据/身份完整比较。
- H988–H999逐个覆盖完整TUN转发当前值/持久化/旧网关记录/配置/运行、选择性网关健康，安装记录缺失/阶段/目标/版本/设备身份，缺少核心、离线/禁用/旧Agent。H998另测404、401/403、坏JSON/非对象、512KiB上限、400/409/422/500消息映射和中文1000字符截断。所有拒绝检查分别验证两端全SQL/files原样。
- H1000/H1003/H1004/H1008类型与manual确认/managed绑定门禁；显式make_default才改变默认空间，未请求时不改SQL。私有路由草稿只有成功check后才关联两台真实绑定的fake Agent并成为一个profile router，auto-sync保持关闭。再次check和manual finish允许仅路由器变更导致的revision前进，验证B04的核心fingerprint边界。B91 check保留abandoned标记。
- H1009用真实并发HTTP：fake Agent挂起状态响应时，另一公开scoped OxiDNS设置请求成功修改核心；释放后finish返回409，session未被写成完成。再次check要求重新发布。不是修改Python函数或调快定时器。
- 新协议比较器对真实fake HTTP采样并故意改错方法、路径、认证、正文、Accept、Content-Type、目标Agent；64项自检通过。首轮发现测试草稿误带不支持的enabled字段400，以及尚未接通Go侧不存在session文件的夹具假设；均按Python修正后整批重跑，无设计错误，没有把夹具错误记成业务差异。

```text
.32 run.py --groups setup-completion-lifecycle,setup-completion-status-guards,setup-completion-publication-guards,setup-completion-protocol-guards,setup-completion-finish-guards,setup-completion-concurrent-core-edit,setup-completion-attach-router
current go-unimplemented=163 dependency=24 request_variants=187
design_errors=[] seconds=32.27；exit=0
64 comparator self-checks + wrong-Go-response check OK
```

仍需managed finish设备报告分支、安装超时/断连/同session串行、H1001/1002/1005–1007/1055和余下helpers。T22目前验证上述fake发布边界状态机，不代表真实安装发布或实验网验收完成。

## 批次 70：规则和策略组成功写入的空间选择

- 19条写入路由、70个原B编号：显式profile URL优先于query/header，profile优先于profile_id，header回退和默认空间回退。真实执行策略组创建/删除/修改/健康URL/重排，以及规则/规则集创建删除修改、批量输入、compose、DNS、标签、策略和重排；每个变体独立干净fixture，成功200且核对实际目标空间业务字段，未用坏body提前拒绝替代选择验证。
- 新增逐端隔离比较器：只允许选中空间增加一次profile revision和一次generation，其他空间、共享和系统的每行值/position/revision以及所有文件严格不变；不采用跨端时间或身份归一化。跨端再完整比较DB/files/响应，compose随机身份依既有严格双射捕获。故意改错外部空间、系统行、revision、generation和section顺序均能检出，65项自检通过。
- 70变体均Go尚未实现；覆盖并集原R4053（S656/F2601/B796）、H322。独立报告暂存/tmp/configdiff-profile-writes.json，统一results.json待正在运行的.63旧组全量返回后合并刷新，未宣称新HEAD全量通过。

```text
.32 run.py --groups profile-rule-group-write-selectors
current go-unimplemented=70 request_variants=70 design_errors=[] seconds=91.87；exit=0
65 comparator self-checks + wrong-Go-response check OK
```

剩余生成/发现/设置等其他写入P3–P6及其业务分支继续实现；最终.32全量仍待执行。

## 批次 71：setup同session串行、客户端断连和生产超时

- 22新增请求/序列变体（16 Go尚未实现、6心跳依赖），新增原B编号2：R211-B01/B02。覆盖并集原R4055（S656/F2601/B798）、H322；独立报告/tmp/configdiff-setup-concurrency.json待与统一全量合并。
- 真实check在Agent响应屏障中挂起，再提交同session PATCH；独立auth状态请求仍可处理，PATCH须等待屏障释放后才返回。核对check的step=4、随后PATCH的step=2/max_step=4、单次Agent请求及最终session，全SQL不改。不是用单线程服务器饥饿冒充session锁。
- 浏览器发送check、确定Agent已收到请求后，不读响应直接关闭socket，再释放Agent。必须仍写成已检查step/max_step=4并保留安装收据，随后新浏览器GET读回成功。原响应在断连后不可见，明确只比较协议、真实落盘和后续公开响应，不伪造响应相等。
- 使用生产45秒Agent读取超时，fake仅延迟46秒；浏览器单例请求预算60秒，以免原默认30秒先截断。返回502准确中文，实际耗时必须44–50秒、一次准确状态请求、无OxiDNS请求、无SQL/files变化。新增耗时/意外规则拉取故意出错自检；66项通过。
- 首次长请求被严格只读断言拒绝，诊断确认30秒扫描后新增notifications.json和task-status.json（内置rule_source刷新）。未掩码这些文件；长超时fixture在Python备份阶段用相同名称、不同合成内容、interval=0占用内置库名，使未选择的内置URL无需新建。真实创建内置库仍由setup-prepare-*独立覆盖。核实长请求所有库均content/interval=0，整个45秒期间回环上游0请求。源码和生产时钟/循环均未改。

```text
.32 run.py --groups setup-same-session-serial,setup-browser-disconnect,setup-agent-status-timeout
current go-unimplemented=16 dependency=6 request_variants=22 design_errors=[] seconds=61.83；exit=0
.32 python -m unittest discover -s server/tools/configdiff -p test_compare.py
66 self-checks OK
```

剩余setup managed报告门禁、其他H/helper、其余profile选择和长流程继续；仍非最终验收。

## 批次 72：发现设置、忽略列表和网站备注的空间选择

- R049/R050/R058/R061各P3–P6，共16个新增原B编号，全部实际Flask↔Go比较通过。每个变体从独立同seed开始，分别真正删除/增加ignore、修改confidence=95、保存中文emoji备注；验证响应profile头与实际目标空间值，沿用逐端隔离比较器，其他空间/共享/系统行、所有文件原样，只前进一次目标revision/generation。
- 覆盖并集原R4071（S656/F2601/B814）、H322；独立报告/tmp/configdiff-profile-discovery.json待与统一全量合并。.63正在运行的批次68旧组全量尚未返回，不把单组16通过写成新HEAD全量通过。

```text
.32 run.py --groups profile-discovery-write-selectors
current compared=16 request_variants=16 design_errors=[] seconds=44.90；exit=0
66 comparator self-checks + wrong-Go-response check OK
```

接着补config.py live pull的有名场景及剩余helpers/长流程。

## 批次 73：端口租约修复后有效全量刷新

- .63对批次68已有的全部8091变体完整运行，design_errors=[]，无端口耗尽；耗时1632.85s、退出1（保留84差异变体），不是基础设施失败。生产Go基线为该次构建的afca350b；运行期间集成go已前进至e5903ffb，尚未再rebase，不能声称最新go全量。新批69–72独立验证报告尚未加入本次旧组结果。
- 5733 Go尚未实现、2236相比通过、37前置依赖、84不同、1 O12单列。原R3977（S654/F2599/B724）、H305、G16；这是本次全量本身，不是包含69–72新用例的覆盖并集。先前失败端口审计的7081中间结果已被本次有效结果替换。
- 当前主要差异：provider/merged缺生成注释和落盘注释；WebDAV新客户端的PROPFIND正文/方法序列及错误消息（10变体）；规则代理缺通知、内部内容分派、receipt错误分类、CONNECT标准传输字段；订阅失败通知/500外壳、重复写revision；发现库ID形态、旧扩展字段清理；孤立surrogate接受并写U+FFFD；scheduler失败通知。findings保留每个路由/编号/原始合成结果，没有改Go或加基线豁免；CONNECT传输观察仍单独解释待兼容性审核。

```text
RT_HOST=sunbroke@192.168.0.63 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc '~/cf-test/.venv/bin/python server/tools/configdiff/run.py > /tmp/configdiff-current-all.log 2>&1; rc=$?; tail -6 /tmp/configdiff-current-all.log | head -5; exit $rc'
current compared=2236 go-unimplemented=5733 dependency=37 different=84 O12-separate=1
request_variants=8091 design_errors=[] seconds=1632.85；exit=1
63 comparator self-checks + wrong-Go-response check OK（该次开始时的框架）
```

下一步把新组通过常规runner合并刷新到统一结果；仍有大量matrix/H条目和最终.32全量，Step B未完成。

## 批次 74：config.py live pull有名输入、鉴权优先级和生成拒绝

- 119请求变体均Go尚未实现，新增原R编号88（S22/F33/B33）；覆盖并集原R4159（S678/F2634/B847）、H322。独立报告/tmp/configdiff-pull-routes.json，待常规runner合并统一结果。
- 所有11个live pull入口逐条发送proxy Host/Proto与默认Host；Mihomo额外query base_url和GET JSON body base_url。空持久origin是明确兼容SQLite边界fixture，两端用相同generation/合法commit_id刷新，不调用Go cache API。provider缓存和真实回环规则源保持存活，实际生成URL必须全以对应origin开头，精确比较完整输出及零下载/零存储写入。中文emoji profile、空资源与IPv6 Host逐入口。
- 未登录带missing/invalid profile各入口404/400先于401；无效query token命中各入口F08。Mihomo/Surge/Loon的真实无效策略引用400中文逐字；OxiDNS坏native YAML400，Mihomo基础YAML未捕获异常500。没有拿别的异常充作尚不可达的MosDNS StrategyReferenceError或OxiDNS非ValueError分支。
- MosDNS YAML只引用本地规则文件，不输出下载URL；两次错误的“应有API URL”夹具假设已按Python源码修正并在matrix errata明确origin不可观察部分。最终forms整组55变体design_errors=[]；保留完整本地路径结构/响应/SQL/files比较，未伪造Host效果。B91目前只有IPv6 Host；逗号串和HTTP/1.0无Host待补。
- SourceStable组合真实源请求与逐端只读快照，故意改Go存储和意外上游请求均能检出；67项自检通过。

```text
.32 run.py --groups config-pull-named-forms,config-pull-named-guards,config-pull-named-failures（首轮forms夹具错误，不作通过）
.63 run.py --groups config-pull-named-forms（修正后）
current go-unimplemented=55；独立累计=119 request_variants design_errors=[] seconds=4.58；exit=0
.32 runner self-checks: Ran 67 tests / OK
```

继续B90日志/HEAD、多值Forwarded/无Host及其余未覆盖分支；当前既不是所有子形态覆盖，也不是最终.32全量。

## 批次 75：DNS worker严格协议、持久错误和生产子进程超时

- 33新变体全部实际Flask↔Go比较通过；新增H099–H105七编号、原R编号3（B2/F1）、T14。覆盖并集原R4162（S678/F2635/B849）、H329。独立报告/tmp/configdiff-dns-protocol.json待合并统一结果。
- 真实可执行fake记录stdin：execve格式错误（可发现且仍有执行权限，确保命中OSError而非missing guard）、退出1/3、协议版本3、success非bool/addresses非list或元素非string、latency负数/bool/string、成功但空地址或Fake-IP /15两端、坏JSON/非对象/缺version或dns_check/非法IP。逐个H独立请求，准确503安全中文，不回显stderr，SQL/files不改。
- system空servers、自定义多个上游去重保序、端口前导0和默认53规范化、bootstrap顺序；返回真实IP去重/latency整数；配置型DNS失败各code均HTTP200 success=false，未知code降为dns并使用安全中文。无效持久DNS设置GET400且不自动修复。
- T14真实sleeping子进程被生产10秒deadline终止，HTTP200 timeout结构无伪造latency；每端耗时必须9–13秒，stdin只有一次，SQL/files不改。四上游含DoH/DoT及bootstrap、2048字符DoH合法；全部fake worker不发DNS/HTTP流量。没有调快生产时钟。超时窗口故意改快/慢/非法值能检出，68自检通过。

```text
.32 run.py --groups dns-worker-helper-failures,dns-worker-boundary-inputs,dns-worker-returned-failures
compared=30 design_errors=[] seconds=83.00；exit=0
.32 run.py --groups dns-worker-production-deadline,dns-worker-success-forms
compared=3 独立累计=33 design_errors=[] seconds=30.48；exit=0
68 comparator self-checks + wrong-Go-response check OK
```

T14仍不代表真实DNS网络路径或实际代理RTT通过；T13节点并发busy/上限转换、其余helpers/长流程继续。

## 批次 76：统一报告合并69–75批

- 通过常规runner在.63重跑此前独立验证的新组并更新统一results.json：先414变体（368 Go尚未实现、30心跳前置依赖、16相比通过），design_errors=[]、180.27s、exit0；再33 DNS worker变体全部相比通过，design_errors=[]。没有手工拼接或把测试尚未运行的条目登记通过。
- 统一累计8538变体：compared2285、Go尚未实现6101、dependency67、different84、O12-separate1；原R4162（S678/F2635/B849）、H329、G16。所有此报告的Go生产代码基线仍afca350b，新集成go尚待rebase/重跑；新OxiDNS helper工作区不在此统计中。
- 相比73批有效全量，新增447真实变体；84差异与findings条目未被豁免或丢弃，已完成的新组不再只存在/tmp独立报告。68 comparator self-checks及故意错误Go响应检出通过。

```text
.63 run.py --groups setup-completion-lifecycle,setup-completion-status-guards,setup-completion-publication-guards,setup-completion-protocol-guards,setup-completion-finish-guards,setup-completion-concurrent-core-edit,setup-completion-attach-router,profile-rule-group-write-selectors,setup-same-session-serial,setup-browser-disconnect,setup-agent-status-timeout,profile-discovery-write-selectors,config-pull-named-forms,config-pull-named-guards,config-pull-named-failures
current go-unimplemented=368 dependency=30 compared=16 design_errors=[] seconds=180.27；exit=0
.63 run.py --groups dns-worker-helper-failures,dns-worker-boundary-inputs,dns-worker-returned-failures,dns-worker-production-deadline,dns-worker-success-forms
current compared=33 design_errors=[]；exit=0
```

仍余原R368编号及H783编号，另有已具名但多子形态/长流程部分覆盖；不把变体总数当作清单已完成。最终rebase与.32全量尚待执行。

## 批次 77：H555–H639原生插件静态约束

- 174新增HTTP变体覆盖85原H编号：H555–H639，每个编号独立公开POST /api/oxidns/validate。首轮85配方在.32通过Python前提校验，再按循环字段/条件展开到174变体在.63通过；全部Go尚未实现，接通后自动比较。统一累计8712变体，原R4162（S678/F2635/B849）、H414、G16；different仍84。
- 正则/域名/Cron分段与范围、IP/CIDR、模板占位符（url/body/content_type/headers/query/form/script args/env）、Provider/DNS code/mark/random/env/表达式/TTL/EDNS0、black_hole互斥与null warning、缓存与TTL、探测方法、hosts/redirect/domain/zone/AdGuard/geosite、nft双地址族字段、RouterOS双插件及持久地址/CA/超时/默认路由警告、HTTP body三种互斥/头/模板/下载目标、时间段与定时任务。所有字段家族单独变体；临界数值及显式null保留类型。
- 必须出现准确path+中文message+severity+source=config.yaml且code为非空；完整HTTP还逐字段比较所有issue及code，不掩码。valid须等于全部issues是否无error，明确不把warning当拒绝。有些schema层与helper层同路径同消息会在Python公开响应去重；本组只验证去重后的可观察结果，不宣称能辨认是哪一层生成了重复诊断。
- 每次验证逐端DB/files原样，真实回环Source记录所有HTTP方法且期望0请求；仅静态草稿，不执行DNS核心、脚本、RouterOS或nft操作。相对路径位于隔离cwd；规则include只登记目标核心需检查的warning，没有读实际zone文件。沿用已有SourceStable故意出错自检，68项通过。

```text
.32 run.py --groups oxidns-plugin-helper-constraints（85原配方）
go-unimplemented=85 design_errors=[] seconds=8.03；exit=0
.63 run.py --groups oxidns-plugin-helper-constraints（174多字段/多形态变体）
go-unimplemented=174 design_errors=[] seconds=5.17；exit=0
.63同组常规runner合并统一results.json：design_errors=[]；exit=0
```

继续zone记录、引用/quick语法及剩余helpers；当前H仍差698编号，最终.32全量未执行。

## 批次 78：zone/RDATA、RFC3597、占位符与AdGuard细分

- 56新增变体全部Go尚未实现：37 zone错误输入、3有效极值/占位符/RFC3597对照、16 AdGuard错误或跳过warning；新增H640–H663的24场景编号，包含下述2个指定return只能部分覆盖。统一累计8768变体，原R4162、H438、G16；compared2285、dependency67、different84、O12-separate1不变，Go尚未实现6331。
- 源自公开validate的精确路径/消息/严重性/完整issues/code、valid/core_check_required和全SQL/files，所有fake HTTP请求必须0。$ORIGIN/$TTL/$INCLUDE/$GENERATE参数与范围、quoted/omitted/relative owner、缺type/RDATA、placeholder位置、RFC3597长度/hex/类型、RDATA引用、空或多条记录；include不读文件，generate不展开大量记录。合法最大TTL4294967295、{qname}/{qclass}与RFC3597应valid=true且issues=[]。
- AdGuard补Hosts/URL/cosmetic/unsupported modifiers的warning、空pattern/正则/域名、denyallow/dnstype缺值或无效、反向查找/正则语法、内部pipe等，不能把warning统统改成error。
- 部分覆盖明确记入matrix errata：H642/H652超限TTL在dnspython解析时已被拒绝，公开结果为通用H662；专用>U32 return不可观察。H664固定schema格式名不能由HTTP构造，仍未计入H438。注释单串实际走H651，首轮错误地预期H663已修正；没有伪造该输入会触发0记录门禁。

```text
.63 run.py --groups oxidns-zone-helper-records,oxidns-adguard-helper-rules
首轮comment-only前提错误，不作通过；AdGuard16变体design正确
.63 run.py --groups oxidns-zone-helper-records（修正注释分类后）
current go-unimplemented=40 design_errors=[] seconds=3.48；exit=0
独立新增合计56；68 comparator self-checks + wrong-Go-response check OK
```

继续引用/quick语法与剩余helpers；H仍差674编号，另有明确不可观察分支和多子形态部分覆盖，未完成Step B。

## 批次 79：引用目标能力、quick参数和依赖环

- 78批已全部rebase到go5535931d，重写后head1c4133b4，工作区干净、116改动路径范围检查全部授权。该Go基线全量正在.63前台运行，尚无最终结果；没有读Go业务实现。遵守新AGENT-BRIEF的等待规则，不在等待命令时结束任务。
- 84新增变体覆盖H665–H698的34编号及5种G15合法quick边界，均Go尚未实现。.32独立报告/tmp/configdiff-oxidns-reference.json；覆盖并集原R4162、H472、G16，统一results.json仍待新全量完成后合并。
- 完整校验重复tag、空/不存在引用、未知plugin warning、executor/provider类型与domain/ip能力、必须为sequence/dynamic_domain_set、udp/http/fallback/prefer双栈入口，快捷matcher/executor种类、schema转换后的路径折叠、空/额外参数、cache/short_circuit/sleep/EDNS0/time/rate_limiter/ipset/nftset/download/upgrade/reject/mark等逐参数错误；直接自环和两节点依赖环精确定位。
- 未识别表达式/插件能力保留warning与target-core要求。合法sleep0、EDNS0 0/65535、命名reject码、mark 0/4294967295、时间段均valid=true且issues=[]，不执行插件。每次SQL/files不改、回环fake HTTP0请求。首轮错误假设空sleep走fallback文本；实际format_error已返回时长诊断，已修正并整组重跑，未当Go差异。

```text
.32 run.py --groups oxidns-reference-helper-constraints
current go-unimplemented=84 request_variants=84 design_errors=[] seconds=9.52；exit=0
68 comparator self-checks + wrong-Go-response check OK
```

继续schema格式/类型分支与剩余helpers；H仍差640编号，另有partial/不可观察分支，最终.32全量未执行。

## 批次 80：schema诊断、include来源与改绑事务门禁

- 新增68请求变体：39 schema、29 REBIND，均Go尚未实现，design_errors=[]。新增H25编号（schema21含H719部分观察、REBIND4）及原R121-F17；覆盖并集原R4163（S678/F2636/B849）、H497、G16，累计已验证变体并集8920。统一报告仍待go5535931d全量结束后由常规runner合并，未手工拼报告。
- H699–H720除H704的类型严格性、动态占位符warning、enum/min/max/exclusiveMinimum、长度/条目上下限、required、额外字段拒绝或warning、插件未知/缺args、准确include来源。完整issues/code/source/order及valid仍精确比较，SQL/files不变、回环HTTP0次。H704不可观察、H719前置校验已拒绝的部分范围已写入matrix errata。
- 本组先后发现三种字段选择错误（ECS、HTTP URL、cron时区实际不走目标schema格式）；按完整schema目录核对后改用forward/time字段，最终39变体完整重跑通过。此前exit2前提错误不记通过、不登记为Go差异。
- H742–H745逐条件：同空间、列表缺失/null/空/错误类型/非法元素/重复、确认集合缺失或额外；部署/升级/HA非终态和未知状态。R121-F17补HA忙拒绝；HA的rolled_back仍忙，不能与普通部署的终态集合混淆。拒绝后全部DB/files原样，Agent指向可记录回环fake且请求必须0。沿用SourceStable故意存储/请求错误自检。

```text
.32 run.py --groups oxidns-schema-helper-constraints
go-unimplemented=39 design_errors=[] seconds=3.66；exit=0
.32 run.py --groups profile-rebind-helper-guards
go-unimplemented=29 design_errors=[] seconds=16.76；exit=0
68 comparator self-checks + wrong-Go-response check OK
独立报告/tmp/configdiff-oxidns-schema.json、/tmp/configdiff-rebind-guards.json
```

继续节点worker和其余helpers；H仍差615编号，另有已标partial和不可观察分支，最终.32全量未执行。

## 批次 81：节点worker真实协议错误与设置边界

- 29新增变体在.32实际Flask↔Go比较全部通过，新增H721–725/H728–736共14编号。覆盖并集原R4163、H511、G16，已验证变体并集8949；统一报告待正在运行的新基线全量完成后合并。
- 节点测试URL的协议/host/凭据/IPv6错误、timeout上下界与非整数；无效持久DNS设置原样报错、不修复。组件缺失、可发现且有执行权限但execve格式错误、退出3与其他非零、协议version、results非数组/缺少或错关联名、latency零/负/bool/float/string、null且无error、坏JSON/缺version/results/name/非对象。HTTP503逐字安全中文，stdout错误不泄漏凭据。
- WorkerStable同时比较实际子进程stdin和每端SQL/files前后相等，失败不写latency历史。新增故意改Go存储、移除worker节点的自检，69项通过；首轮新自检的测试替身缺root导致异常，已修正后整组重新运行，无业务用例误报。

```text
.32 run.py --groups node-helper-worker-failures,node-helper-settings-guards
compared=29 request_variants=29 design_errors=[] seconds=39.00；exit=0
69 comparator self-checks + wrong-Go-response check OK
独立报告/tmp/configdiff-node-helpers.json
```

H726体积、H727生产超时、H737忙锁及候选上限仍待补；本组不宣称真实节点RTT或DNS网络通过。

## 批次 82：完整导入的共享目录与资源闭包

- 96新增HTTP变体全部Go尚未实现，新增H746–760共15编号（H757/H758为明确的前置拒绝部分观察）。覆盖并集原R4163、H526、G16，已验证变体并集9045；统一报告仍待新基线全量结束后合并。
- 完整current备份分别破坏Agent及四类共享目录的list/item/id/重复ID，每个目录均有独立请求；聚合nodes/subscriptions引用的列表与元素类型、重复、排序引用空ID、各类资源不存在、chain双边的形态/引用类型/id/额外字段/缺组、source错误类型、raw dialer空值/错型/缺目标。精确中文、409携带具体profile前缀，所有SQL/files原样、回环HTTP0次。
- H757/H758指定的后置return被validate_shapes抢先拒绝，matrix errata明确公开400与部分范围。首轮chain另一端用DIRECT却未共享节点解析，提前409；改为存在的group引用，仅让目标错误先于后续拓扑检查，96变体整组重跑design_errors=[]。没有将fixture错误登记为Go差异。

```text
.32 run.py --groups repository-resource-closure-guards
go-unimplemented=96 request_variants=96 design_errors=[] seconds=4.13；exit=0
已有存储/意外请求故意错误自检通过；独立报告/tmp/configdiff-repository-resources.json
```

继续生产超时恢复、其余helper/路由与长流程；当前H仍差586编号，未完成Step B。

## 批次 83：节点体积、候选限额与实际生产超时恢复

- 中断后核对并在.63整组重跑未提交草稿：7变体全部实际相比通过；新增H726/H727/H738–741共6编号及T13恢复子形态。覆盖并集原R4163、H532、G16，已验证变体并集9052。H738前置路由拒绝导致内部return仅部分观察，已同步matrix errata。
- 实际序列化worker stdin超过8MiB拒绝400且不启动worker；21节点、6个需Sub-Store转换的URI、同名节点及names/keys同时指定在任何转换/worker启动前拒绝。拒绝及超时完整SQL/files原样，stdin调用数精确。
- timeout设置为合法下界500ms，生产worker预算为500ms+5s，真实sleep子进程被终止；比较每端5–8秒窗口、安全503和零持久写入。随后空选择测速成功，证明锁释放，并比较实际生成的空历史文件；先前错误的恢复请求只读假设已撤销，没有掩码新增文件。新增故意过快/过慢/非法计时自检，70项通过。
- go5535931d的前台.63全量在中断期间完成：8768变体、design_errors=[]、1787.60s，compared2285/unimplemented6331/dependency67/different84/O12-separate1。原结果保存/tmp/configdiff-rebased-5535931d-results.json；修复分支尚未合入go，待合入后重跑findings并通过常规runner更新统一报告。

```text
RT_HOST=sunbroke@192.168.0.63 rt --back server/tools/configdiff/testdata/node-bounds-draft.json --back server/tools/configdiff/testdata/node-bounds-draft.md -- bash -lc '~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups node-helper-candidate-guards,node-helper-payload-limit,node-helper-production-deadline --output server/tools/configdiff/testdata/node-bounds-draft.json --findings server/tools/configdiff/testdata/node-bounds-draft.md > /tmp/configdiff-node-bounds-63.log 2>&1; rc=$?; tail -7 /tmp/configdiff-node-bounds-63.log | head -6; exit $rc'
H741 compared；H726 compared；H727 compared；T13 compared
compared=7 request_variants=7 design_errors=[] seconds=21.11；exit=0
70 comparator self-checks + wrong-Go-response check OK
独立报告/tmp/configdiff-node-bounds.json（临时draft文件移出worktree）
```

继续H737并发busy、生成选择器、其余helper与长流程；最终.32全量未执行，Step B未完成。

## 批次 84：生成器profile选择、节点并发busy及差异复验状态

- 50个原清单P3–P6编号新增实际HTTP变体，覆盖默认/scoped五客户端下载与预览，共20路由。全部Go尚未实现；新增H737与T13并发恢复2变体在.63实际相比通过。覆盖并集原R4213（S678/F2636/B899）、H533、G16，已验证变体并集9104。
- 两profile以不同Mihomo端口、Surge/Loon DNS配置、MosDNS日志路径、OxiDNS native tag使选择结果可观察。精确比较URL/query/header/default所选profile、完整生成文本/ZIP、每次SQL全库不变、只允许选中profile的指定生成文件及其正确前驱.bak改变；其他空间和文件必须不变，回环上游HTTP0次。比较器故意错误自检覆盖数据库/foreign file/输出/备份/写错profile；Surge/Loon格式也有对照。
- 夹具修正：MosDNS实际重建plugins，不能用custom plugin tag作持久标记，改为实际保留的log_file；主文件与.bak的同源内容检查统一使用原生成路径选择解析格式，全部50变体最终整组重跑design_errors=[]。中间前提错误均未计通过或Go差异。
- H737通过真实外部子进程门闩：第一个公开测速进入worker后保持在途，第二请求409且不写SQL/files，释放后第一请求200；随后新公开请求200，锁已释放。精确比较worker stdin、第一批响应和完整历史文件，已验证窗口内的备份tested_at使用单独精确时间路径规则，其他字段未归一化。新增门闩未进入/第一批错误/并发写入故意错误自检。
- findings复验状态现在由runner记录：只有本轮实际compared才closed，still different为open，未实现/依赖为blocked，未跑为pending；保留既有差异原始记录和最后复验baseline。支持--baseline显式标明生产源码版本，历史状态不会随无关组更新。自检故意用跳过/未跑验证不能误关；修复合入go后据此重新核对既有84差异。

```text
.63 run.py --groups profile-generator-selectors --baseline 5535931d
go-unimplemented=50 request_variants=50 design_errors=[] seconds=25.70；exit=0
.63 run.py --groups node-helper-concurrent-busy --baseline 5535931d
H737 compared；T13 compared；design_errors=[]；exit=0
73 comparator/report self-checks + wrong-Go-response check OK
独立报告/tmp/configdiff-generator-selectors.json、/tmp/configdiff-node-concurrency.json
```

原R仍差317编号、H仍差579编号，另有多子形态/长流程partial；继续补，不将变体数当完成率。

## 批次 85：go5535931d统一报告与复验基线

- 常规runner在.63重新执行79–84批11个组，336变体全部design_errors=[]：实际相比通过38、Go尚未实现298，91.62s、exit0。统一results.json现9104变体：compared2323/unimplemented6629/dependency67/different84/O12-separate1；原R4213（S678/F2636/B899）、H533、G16。此前独立报告均已真正运行合并，没有手工拼接。
- 既有84差异及1个O12单列项进入findings复验表，当前均pending；这11个新组不是其原始group，不能据此标closed。原始错误结果和具体Python/Go差异完整保留；待修复合入go后直接重跑相关原group。当前go仍5535931d，尚未合入修复。

```text
.63 run.py --groups oxidns-reference-helper-constraints,oxidns-schema-helper-constraints,profile-rebind-helper-guards,node-helper-worker-failures,node-helper-settings-guards,repository-resource-closure-guards,node-helper-candidate-guards,node-helper-payload-limit,node-helper-production-deadline,profile-generator-selectors,node-helper-concurrent-busy --baseline 5535931d
current: go-unimplemented=298 compared=38；design_errors=[] seconds=91.62；exit=0
cumulative: go-unimplemented=6629 compared=2323 dependency=67 different=84 O12-separate=1
73 self-checks + deliberate wrong-Go-response detection OK
```

继续剩余清单；此为迭代报告，最终.32全量未执行。

## 批次 86：只读查询选择器与provider三环境认证

- 新增148已验证变体：查询/统计选择器16（4相比通过、12 Go尚未实现），provider认证132（48相比通过、66 Go尚未实现、18已知provider元数据差异）。新增原R31编号；覆盖并集原R4244（S678/F2651/B915）、H533、G16，统一9252变体。provider的18差异已写入findings，保留原始比较而非仅存临时报告。
- 查重/匹配用两空间不同域名和规则ID明确观察选择结果及顺序，正则预览明确使用共享候选；统计用真实近期样本走生产30分钟节流，返回两空间不同规则数，同时SQL/files及两份历史精确不变。回环上游0请求。
- 默认/scoped聚合和单订阅逐条跑登录、关闭登录但设置config_token、完全开放三环境D1–D8；开放D1/D2/D3/D6/D7实际200，当前/退役内部能力query始终401，配JWT或伪造internal header也拒绝。拒绝时全SQL/files不变；允许时比较缓存、served文件和所有metadata，来源HTTP0次。能力含+，必须query编码；初稿未编码导致测试了另一字符串，已修正整组重跑。沿用严格Metadata比较器检查文件名身份/时间窗，避免秒边界被误报，同时不吞掉注释差异。

```text
.63 run.py --groups profile-query-selectors --baseline 5535931d
compared=4 go-unimplemented=12 design_errors=[] seconds=6.52；exit=0
.63 run.py --groups provider-auth-environments --baseline 5535931d
compared=48 go-unimplemented=66 different=18 design_errors=[] seconds=20.33；exit=1（真实已知元数据差异）
随后常规runner重跑两个组写入统一results/findings；73项自检通过
```

协调者已通知最新go含findings修复/C35/N8-2；本批提交后立即rebase并逐组复验，closed须带修复提交，accepted须链接behaviour-changes条目，未解决继续open。

## 批次 87：rebase至集成修复并记录修复归因

- 86个提交已成功rebase至go aeff9d75，重写后的86批head1626cfdc，rebase后工作区干净；未读Go业务源码。86批人工并集统计少算一个F编号，以实际统一报告为准：原R4245（S678/F2652/B915）、H533、9252变体；新增原R32，剩余原R285编号。
- 已在.63前台启动全部26个既有差异group复验，基线aeff9d75，覆盖102 different（含新增provider认证18）及O12单列项。此处不预报结果，待命令完成后更新证据。
- 修复归因从config-findings-fixes.md及git提交标题读取，只在实际closed后添加修复提交；未跑/跳过/仍不同不产生修复结论。provider元数据7b904725、准备上下文cb0cc9fa、通知8f3d36ad、同值PUT e9187b9f、发现UUID 041ab5a6、C35三提交已建对应。refresh_finding_reviews仅重渲染已经记录的复验和归因，不执行或拼接用例、不修改原始case结果与差异。

```text
git rebase go
Successfully rebased and updated refs/heads/go-difftest-config-b.
.32 python -m unittest -v test_compare
Ran 74 tests in 2.521s / OK
.63 run.py --groups delivery-graph,delivery-graph-collisions,discovery-init,discovery-region-route,discovery-settings,json-settings-intervals,merged-provider-data,merged-provider-remote,merged-provider-timezone,provider-auth-environments,provider-concurrent-clients,provider-last-good-lifecycle,rule-proxy-declarations,rule-proxy-local-cache,rule-proxy-processing-failure,rule-proxy-transport,scheduler-tick,settings-webdav,startup-migration,subscription-fetch-failures,subscription-provider-formats,subscription-provider-head,subscription-provider-selector,subscription-provider-timezone,subscription-provider-validation,subscriptions-cleanup --baseline aeff9d75
（仍在运行；最终证据待写入下一批）
```

## 批次 88：全部既有findings复验、精确接受决定及helper关联

- 基线aeff9d75。26组首先在.63执行：486变体，348相比通过/114 Go尚未实现/23严格差异/O12单列1，409.76s；provider-auth的全会话文件名身份绑定发生非单射夹具错误，缺21变体，此轮不当完整通过。SSH banner一度超时后恢复回传；按brief回退.32完整同组507变体复验，design_errors=[]、396.83s、exit1，369相比通过/114 Go尚未实现/23严格差异/O12单列1。
- 已修正provider-auth的文件名绑定作用域：每个响应采用独立比较policy，仍严格验证同一订阅ID、文件名格式和真实时间窗，不要求多个响应的秒级时间戳形成全会话一一映射。保持Metadata对正文/served及.bak注释逐行比较。.63修正后provider-auth与原有profile/node/client组281变体通过前提校验（95相比通过/186 Go尚未实现），51.72s；随后常规runner合并统一报告的同组281变体design_errors=[]、38.18s、exit0。
- **findings最终复验表：closed81、accepted5、open18、O12-separate1、pending0**。81条closed均附修复提交；accepted精确链接behaviour-changes中的Step B #1/#2/#3、R058-B03：两个孤立surrogate、保留遗留扩展、fetch坏JSON的同状态JSON迁移、fetch非对象400。原始case仍保留different及全差异，只将明确批准且全部差异精确吻合的项从当前失败退出码中分出。额外字段、错误success/中文/状态、额外SQL或文件写入、错route/variant会重开open；未整体忽略HTML、message或存储。
- 仍open：规则代理内部内容dispatch两项200→502、receipt目录错误两项500→400、CONNECT传输字段两项；R222-F11缺A11位置/摘录一项；WebDAV七项PROPFIND正文及507/403/连接失败提示三项；R056-S01 duplicate-targets新多出空缓存.bak一项。UUID形态差异本身已在11配方全部消失（041ab5a6），但duplicate-targets该用例因新文件差异保持open，未用修复ID来关闭新问题。O12旧DNS清理仍单列，无behaviour-changes明确接受条目，未擅自accepted。
- 原R请求现在可附source-audited helper_cases：关联16个此前已有真实HTTP用例对应的H编号；重新执行后H549。没有新增HTTP或伪造通过状态；一个请求同时覆盖路由槽与源码helper槽，重复标签按集合计一次。H770使用truthy数组的clone入口，不能误拿R116-F08的早期list.pop崩溃；H773/H774并未错误关联到clone或import-client wrapper。
- 统一仍9252变体，原R4245（S678/F2652/B915）、H549、G16。cumulative compared2454/unimplemented6707/dependency67/different23/O12单列1，其中严格different有5项accepted、18open。当前仅新Go基线的选定组复验，不宣称整个9252已经新基线全量。

```text
.32 run.py --groups <87批同26组> --baseline aeff9d75
current compared=369 go-unimplemented=114 different=23 O12-separate=1
request_variants=9252 design_errors=[] seconds=396.83；exit=1（保留真实差异）
.63 refresh_finding_reviews.py
recorded_rechecks: closed81 open18 accepted5 separate1；actual_case_results_unchanged=9252
.63 run.py --groups provider-auth-environments,profiles-metadata,profiles-deletion,profiles-bindings,profiles-independent-import,profiles-missing,nodes-deletion,client-import-validation --baseline aeff9d75
current compared=95 go-unimplemented=186；design_errors=[] seconds=38.18；exit=0
78 comparator/report/approval self-checks + deliberately wrong-Go-response detection OK
```

原R仍差285编号、H仍差563编号，另有已明确partial/不可观察分支和长流程。继续清单；未改Go业务、未推送、未派子agent，最终.32全量仍待完成清单后执行。

## 批次 89：helper关联纠正、repository公开门禁与归档回调

- 又一次rebase到go0efddadf（相对aeff9d75只有流程说明提交）。核对wrapper源码发现R137-F08早于H805拒绝，撤销该H关联并在matrix说明；改以现有“先删other后删唯一default”的R117-F08关联真正H779。两组实际重跑后关联修正已进入统一结果，H总数净不变，未保留虚假覆盖。H773/H774/H776/H783的公开驱动限制已单列，不以wrapper相似错误计数。R123-F08与R116-F08的data.pop/metadata区别也明确更正。
- 21新增repository变体覆盖metadata name/update/字符串类型分支、独立import移除仍被规则引用的策略组H785、无目录资源但主缓存或仅.bak已存在的H794；准确400/409、全SQL/files不变、真实回环HTTP0次。新增H2，累计9273变体/H551。独立41变体含重跑原有profile组，design_errors=[]、15.73s，常规runner已合并。
- 13新增归档变体覆盖MosDNS回调H003–H008、H014，及同样输入穿过OxiDNS错误映射H024；同一次HTTP附H012准备失败包装、H015下载失败包装标签，新增H10。跨空间、同源回调循环、缺url、非法part、未知callback、快照不存在库；MRS拒绝在HTTP下载前。所有错误源均被已声明的回环代理围住，实际请求必须0，未访问panel.example/source.example；500/400准确中文及全SQL/files不变。Go尚未实现，接通后自动比较。
- 统一9286变体、原R4245（S678/F2652/B915）、H561、G16。新增34实际变体均Go尚未实现；findings复验状态仍closed81/accepted5/open18/O12单列1。README补baseline、accepted/open退出码与helper标签不增加请求数的约定。

```text
.63 run.py --groups profiles-deletion,profiles-independent-import,repository-public-metadata-guards,repository-removed-policy-guard,repository-existing-cache-guard --baseline 0efddadf
go-unimplemented=41 design_errors=[]；exit=0（其中新增21）
.63 run.py --groups archive-callback-helper-guards --baseline 0efddadf
go-unimplemented=13 design_errors=[]；exit=0
两组均经常规runner写入统一results；78项自检及故意错误Go响应检出通过
独立报告/tmp/configdiff-repository-public.json、/tmp/configdiff-archive-callbacks.json
```

继续profile剩余边界、归档/依赖与其余helpers、长流程；原R仍差285、H仍差551，最终.32全量未执行。

## 批次 90：profile引用/导出、URL选择器和真实并发写入

- 新增15变体，原R新增14编号（S2/B12），现统一9301变体、原R4259（S680/F2652/B927）、H561、G16；本批全部Go尚未实现，接通后自动比较。旧schema2导入用R028-S03现有编号独立变体，未虚增原编号。
- 禁用Agent仍算用途；旧无profile_id输入通过真实完整schema2公开import后查看默认绑定并改到other，保留禁用且无推送。初版直接修改schema5为缺绑定被Python拒绝500，已撤销该非法前提；matrix明确R121-B01内部fallback仅部分观察。固定IP记录补齐归属字段；clone引用不复制共享资源，清空router/fixed-IP设备归属；source URL覆盖query/header，旧profile所有行保持原样。
- 单profile导出验证整串能力/Agent token脱敏为[REDACTED]、顶层及嵌套未知字段（false/空数组）、中文emoji和default.json附件名。初版误以为逐token替换，已按Python整串替换修正并重跑。删除/切默认/独立import的P3实际写入指定空间；删除tombstone只绑定新目录名，旧导出内容时间戳和未修改SQL字段保持逐字比较。
- R119-B03：两独立HTTP客户端同时修改name/description，验证最终两个字段及revision/generation+2、foreign行不动；每端两个完整响应必须准确满足Python的两种合法串行顺序之一（含字段集/ID/旧created_at/新updated_at窗口），不要求独立竞态选同一先后。headers/status及最终全SQL/files仍严格比较；故意丢更新、两个全新响应、额外字段、坏时间/ID/status自检均能拒绝。
- R121-B02：两客户端同时用同一确认集合改绑，一方200另一方409准确中文，只一次system revision/generation推进；完整winner/loser响应按状态对应、全SQL/files、profile/shared不变及Agent HTTP0次。未用假的锁或直接业务函数驱动。

```text
.63 run.py --groups profile-binding-legacy-edges,profile-clone-reference-edges,profile-export-edges --baseline 0efddadf
go-unimplemented=10 design_errors=[] seconds=7.17；exit=0；随后常规runner合并
.63 run.py --groups profile-export-edges,profile-metadata-write-selectors,profile-concurrent-metadata,profile-concurrent-rebind --baseline 0efddadf
go-unimplemented=8 design_errors=[]；exit=0；统一新增5（导出3为重跑）
79 comparator/report/approval self-checks + deliberately wrong-Go-response detection OK
```

原R仍差271编号、H仍差551编号，已标partial/不可观察分支另计；继续依赖/监听器与其余清单，最终.32全量未执行。

## 批次 91：隔离网络中的依赖状态、可信HTTPS与缓存

- 6新增原R编号（R198 S01/S02/S03/B01/B02/F08）在.32实际Flask↔Go比较通过，覆盖本地运行版本、refresh=1/true强制查询、refresh=TRUE沿用缓存、已最新、503时保留上次release并给check_error。全SQL/files保持原样。统一9307变体、原R4265（S683/F2653/B929）、H561、G16；findings既有closed81/accepted5/open18/O12单列1不变。
- 新native_network.sh只在原生Linux创建仅lo的网络命名空间，然后降回原用户跑测试；不改变宿主网络/服务。命名空间无外网路由，GitHub CONNECT无论目标都不出进程；fake内生成一日CA证书，Python与Go通过正常信任配置验证TLS，未关闭证书验证。WSL/非隔离环境在启动TLS fixture前拒绝，README的最终all命令已改用.32 wrapper。工具链缓存需在外部先就绪，命名空间内不下载依赖。
- 比较Sub-Store真实HTTP GET的method/path/auth/原始body hex，以及CONNECT精确目标和GitHub API明示Accept/User-Agent、请求数；健康API的默认传输User-Agent无接口约定，不声称是业务字段。native源录包开关默认关闭，不改变旧fixtures；新增真实非UTF8 GET body录包自检，以及方法/目标/正文/UA/次数/存储故意错误自检。共81自检通过。

```text
RT_HOST=sunbroke@192.168.0.32 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-status-protocol --baseline 0efddadf > /tmp/configdiff-dependency-status-consolidated.log 2>&1; rc=$?; tail -9 /tmp/configdiff-dependency-status-consolidated.log | head -8; exit $rc'
R198-S01 compared；R198-S02 compared；R198-S03 compared；R198-B02 compared；R198-B01 compared；R198-F08 compared
compared=6 design_errors=[]；exit=0（独立首次4.44s，随后常规runner合并）
81 self-checks + deliberate wrong-Go-response detection OK
```

继续内置Sub-Store模式/升级和监听器等剩余项；原R仍差265、H仍差551，最终.32全量未执行。

## 批次 92：内置依赖运行模式与本地状态边界

- 6新增变体在.32隔离网络实际相比通过；新增原R5编号，统一9313变体、原R4270（S685/F2655/B930）、H561、G16。R198-S01第二种有效形态复用编号，没有多算。R199 on_demand/always/重复always验证响应、system mode落库及每次revision/generation+1、所有foreign行不变；控制器status失败409验证全SQL/files无变更。
- fake supervisor默认只允许精确的只读status参数，其他命令以97拒绝，绝不落到宿主supervisor。实际健康HTTP次数、方法、路径、auth/body/TE和CLI未知动作均比较；X08启动monitor最多一次只读status的时序不形成虚假差异，过量status仍拒绝。所有控制临时文件放在owned TMPDIR。
- R198离线但builtin VERSION可读仍返回当前版本，VERSION为目录的真实OSError状态安全回退；运行Node的--version得到runtime。可信TLS release查询、健康API、全SQL/files仍严格比较。新增mode comparator故意restart、额外健康请求、过量status和失败落库自检，总82项通过。

```text
RT_HOST=sunbroke@192.168.0.32 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-managed-modes,dependency-builtin-status --baseline 0efddadf > /tmp/configdiff-dependency-managed-consolidated.log 2>&1; rc=$?; tail -9 /tmp/configdiff-dependency-managed-consolidated.log | head -8; exit $rc'
R199-S01/S02/B01/F13、R198-S01/F09：compared=6
82 self-checks + deliberate wrong-Go-response detection OK
design_errors=[] seconds=13.54；exit=0
独立原始报告/tmp/configdiff-dependency-managed.json
```

原R仍差260、H仍差551及已列partial/长流程；继续升级/监听器等，最终.32全量未执行。

## 批次 93：真实可信下载、Node 语法检查与异步升级终态

- rebase至go045f2485。5新增变体相比通过，统一9318变体、原R4273（S686/F2657/B930）、H564、G16；R200-S01/F09/F10（后者两种形态）和H033缺asset。R200-F09/F10实际请求附H041/H040来源标签。已最新/503/无asset在worker前409，SQL/files不动且健康HTTP次数为1/0/0，TLS API请求精确一次。
- R200-S01走真实可信TLS GitHub API和512KiB JavaScript下载、真实node --check；fake supervisor只有本fixture显式开启的restart能力，使用真实子进程读取各端DATA_DIR VERSION，再向带合成Bearer的fixture专用回环控制地址通知版本。未执行bundle、未重启宿主服务、未关闭证书校验。
- POST202后逐端轮询公开dependencies至worker终态且maintenance已退出，才重置共享fake运行另一端；中途状态/target非法、轮询非200、12秒无终态均留下失败证据。比较完整终态JSON和声明响应头，finished_at仅允许真实运行时间窗内有限数字（故意旧时间/NaN/字符串会失败）。逐次轮询数可以因调度不同，但扣除本fixture发起的轮询后严格要求三个业务健康请求，所有健康method/path/auth/body/TE仍精确；API/download/CLI次数和内容逐项比较。
- 全SQL保持不变，最终只能多出runtime VERSION和bundle，临时下载/回滚残留会失败；两端全文件比较加上最终两文件原始字节SHA256，不吞换行或尾部字节差异。终态/协议/写入比较器有故意错版本、重复restart、少下载、数据库写入和坏文件digest的自检，共83项。真实Node服务启动/端口接管仍属L1部分覆盖，未由fake冒充。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-update-preflight,dependency-update-success --baseline 045f2485
R200-F09/F10(two variants)、H033、R200-S01：compared=5
design_errors=[]；exit=0；统一报告常规runner合并完成
83 self-checks + deliberate wrong-Go-response detection OK
独立原始报告/tmp/configdiff-dependency-update.json
```

原R仍差257、H仍差548及partial/长流程；继续下载中冲突/升级回滚和其余清单，最终.32全量未执行。

## 批次 94：真实在途更新冲突与下载验证失败

- 新增6变体，原R新增4（R200-F08/B01/F12、R199-F12）、H新增2（H035/H036）；统一9324变体、原R4277（S686/F2660/B931）、H566、G16。升级相关四组共11变体常规runner重跑，全部compared、design_errors=[]、34.51s。
- TLS bundle响应暂停时先确认真实第一个POST202及worker已进入下载门，再发第二个POST或mode切换；准确409与中文比较，拒绝请求前后该端全SQL/files不变。释放后轮询第一个worker真正完成，再比较一份下载、一次restart和完整终态/落盘；没有直接操作业务锁。首次没进门/拒绝请求有写入的故意错误会被自检捕获。
- 真正下载19字节和SHA256不匹配各自产生failed终态；两个业务健康请求（另加实际轮询）、无restart、runtime不留下文件，所有SQL和原文件不动，消息逐字相比。202异步入口统一runtime_writes=True，避免仅内存状态变化被误认为后续Go独立可比。
- F12设置回环外部服务但无builtin，仍按同一Python可用性分支拒绝，HTTP/TLS0次；matrix明确F11/F12等价前置判定。L1真实进程接管仍partial，未扩大fake结论。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-update-preflight,dependency-update-success,dependency-update-conflicts,dependency-update-invalid-downloads --baseline 045f2485
current compared=11 design_errors=[] seconds=34.51；exit=0
83 self-checks + deliberate wrong-Go-response detection OK
独立原始报告/tmp/configdiff-dependency-conflicts.json、/tmp/configdiff-dependency-invalid.json
```

原R仍差253、H仍差546及partial/长流程；继续升级回滚和其余清单，最终.32全量未执行。

## 批次 95：重启错误与旧在线 bundle 的真实文件回滚

- H034新增4变体：fake supervisor首次restart分别stdout诊断/stderr诊断/空诊断后exit1，及已有2.41.0在线bundle时同样失败。实际worker进行两次restart、第二次向回环fake恢复原VERSION，公开轮询得到精确failed+已回滚中文消息。四变体均实际相比通过。
- 有旧在线版本时，真实runtime目录先包含VERSION/bundle和未知JSON状态；生产更新代码自己copytree备份并在失败时替换恢复，测试不替它回滚。最终全SQL/文件、原bundle/VERSION字节digest及未知状态均与请求前相同，没有.download.js/.bak残留。没有旧在线版本时恢复builtin2.40.0，runtime不残留文件。每端restart拒绝标记置于owned TMPDIR，不进入DATA_DIR或宿主supervisor。
- 升级相关四组（成功、并发、下载校验、回滚）合计10变体复跑，design_errors=[]、37.89s；83项自检和故意错误Go响应检出通过。统一9328变体、原R仍4277（S686/F2660/B931）、H567、G16，既有findings状态未新增差异。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-update-success,dependency-update-conflicts,dependency-update-invalid-downloads,dependency-update-restart-rollback --baseline 045f2485
current compared=10 design_errors=[] seconds=37.89；exit=0
独立原始报告/tmp/configdiff-dependency-rollback.json
```

原R仍差253、H仍差545及partial/长流程；继续真实就绪超时/回滚失败及其余清单，最终.32全量未执行。

## 批次 96：节点引用 blocker、非法拨号与跨空间确认失效

- 新增9变体、原R6（R093-F10/F11、R094-F12、R095-F14/B02、R096-B02）及H2（H796/H799同一次HTTP关联），统一9337变体、原R4283（S686/F2664/B933）、H569、G16。
- 节点仅被代理链或聚合引用时，单删均409；链的删除预览必须明确给出blocker，提交当前有效confirmation也不能绕过。替换链节点为不存在raw dialer目标在repository闭包校验409，修改前后全SQL/files严格不变；6变体实际Flask↔Go相比通过。
- 先预览聚合引用节点，再公开创建other空间节点来源组，新的预览必须含other profile用途且confirmation不同，旧confirmation删除准确409、存储不变。创建组尚未接通Go，后两个读取/删除用例按真实前置依赖标记，不能把Go没创建的引用当业务差异。
- .63独立首次与两次常规runner均跑完，6compared/1Go尚未实现/2依赖未实现、design_errors=[]；83自检与故意错误Go响应检出通过。所有输入来自有效Python seed和公开HTTP，不手改坏库伪造“合法现态”。

```text
.63 ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups node-reference-blockers,node-cross-profile-confirmation --baseline 045f2485
current compared=6 go-unimplemented=1 go-unimplemented-dependency=2 design_errors=[]；exit=0
独立原始报告/tmp/configdiff-node-reference.json
```

原R仍差247、H仍差543及partial/长流程；依赖超时独立报告已通过、下一批合并，最终.32全量未执行。

## 批次 97：未缩短的启动/回滚超时及真实JavaScript错误

- H038/H039分别真实等待40秒启动失败后成功回滚、40秒启动加30秒回滚仍失败，两端顺序运行共230.10s；均compared。callback只控制回环服务的运行版本/503，不改业务计时器、锁或后台函数。比较两次restart及控制版本序列，扣除实际公开轮询后业务健康请求准确43/72次，40/70秒最小经过时间必须满足；完整终态、失败原因、生命周期和所有SQL/files均比较。
- H037使用512KiB真正非法JavaScript、正确SHA256，生产下载/大小/digest门禁通过后真实Node编译器exit1；清临时文件、无restart、原数据不动、完整错误前缀/诊断相比。stdin检查wrapper仅固定诊断源而保留真实编译器结果，生产传入的精确文件路径/下载字节digest/exit1也逐项比较；matrix明确原生绝对文件路径诊断仍partial。未执行bundle。
- 六个升级组13变体常规runner复跑，compared13、design_errors=[]、264.87s，83项自检和故意错误Go响应检出通过；自检含过早结束、错Node返回码/字节digest。统一9340变体，原R4283（S686/F2664/B933）、H572、G16，无新增findings。原R仍差247、H仍差540及partial/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups dependency-update-readiness-timeouts,dependency-update-javascript-guard,dependency-update-success,dependency-update-conflicts,dependency-update-invalid-downloads,dependency-update-restart-rollback --baseline 045f2485
current compared=13 design_errors=[] seconds=264.87；exit=0
独立原始报告/tmp/configdiff-dependency-readiness.json、/tmp/configdiff-dependency-javascript.json
```

继续Web监听器及其余清单，最终.32全量未执行。

## 批次 98：Web完整前端公开边界及已撤回的API-only证据

- 首次Web草稿把API-only Go与有Nginx配置的Python相比，产生13个无效different。核对允许读取的公开cmd/configflow/main.go发现Go需CONFIGFLOW_FRONTEND_DIR启用原生listener，已明确撤回全部13条：统一results/findings恢复至此前有效版本后重新执行正确夹具；未把无效different改成accepted/closed或覆盖通过。原始无效报告仅存/tmp/configdiff-web-api-only-invalid-*，真实Nginx草稿另存/tmp/configdiff-web-nginx-overlay-draft.py，未提交。
- 维护者明确选择：Go取代Nginx；R206-F17/F18/F19的Nginx专属故障partial，Go验证原生监听失败后配置不变、清楚中文、旧端口可用，不比较Nginx错误文本。真实Nginx+Flask叠加归.32容器实验/O9/O14，不在Step B做。此明确新增授权已写behaviour-changes.md和matrix，未扩展成其他差异的全局豁免。
- 新Pair选项仅从公开环境启用真正Go frontend，预置各端独立监听端口和同样合成index；其他Pair保持API-only。Python前端HTTP peer为回环fake，私有/etc/nginx只提供状态文件/模板存在性，全部新用例都在任何Nginx命令前返回；当前代码不启动真实Nginx，也不声称覆盖成功换端口或真实重载。
- R205-S01、R206-S02、R206-F14三新增原编号实际相比通过。状态完整字段准确；同端口noop及占用拒绝前后各端全SQL/files严格不变；旧HTTP listener200、能力为32位hex并保持同值，配置文件未变、占用服务没有收到HTTP。各端port只按实际分配的两个值检查精确body/file路径，其他端口字段未mask。H1105由同次真实占用请求关联。
- native_network.sh增加private mount namespace，owned临时目录绑定/etc/nginx、退出卸载删除；使用mount表和source/target inode校验安全边界，降权后运行测试。第一次以mountinfo根路径前缀判定失败（/tmp可为独立挂载），已改为精确mountpoint+inode校验；两次fixture错误不计业务结果。读取/对比类新增健康能力丢失、错误HTTP、意外请求、SQL/文件写入故意错误自检，现84项通过。
- 统一9343变体、原R4286（S688/F2665/B933）、H573、G16；findings仍closed81/accepted5/open18/O12单列1，没有上述无效13条。原R仍差244、H仍差539，另有明确partial/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups web-listener-complete-readonly,web-listener-complete-occupied,dependency-status-protocol,dependency-managed-modes,dependency-builtin-status --baseline 045f2485
current compared=15 design_errors=[] seconds=21.93；exit=0
84 self-checks + deliberate wrong-Go-response detection OK
独立有效原始报告/tmp/configdiff-web-complete.json
```

继续Web成功换端口/原生失败、其余路由及helper清单；最终.32全量尚未执行。未读Go业务实现、未推送、未派子agent。

98批提交后rebase至go7521bb5c；behaviour-changes.md仅append冲突，保留集成分支N2原生代理决定和本批维护者Web决定两节。随后同5组15变体在.32重新执行：compared15、design_errors=[]、33.96s、exit0，84自检通过；统一报告更新该5组基线，未把其余未重跑组冒充新基线全量。

## 批次 99：成功换端口的公开结果与受限Python前端peer

- R206-S01新增1变体实际相比通过。Go始终是真正公开frontend/native listener；Python受限CLI peer只接受精确`-c /etc/nginx/nginx.conf -t`与`-s reload`，通过带合成Bearer的回环controller录制check/reload。controller验证metadata字段、32hex健康能力、精确生成body（监听端口/IPv6/upstream实际端口），再切换双栈HTTP fake。没有启动真实Nginx或执行shell。private mount namespace中将peer临时绑定到Nginx可执行路径，退出卸载；宿主二进制/配置/服务不变。
- 真实API返回200及保存的目标port；双栈新端口200、相同新健康token且相对旧值轮换、页面字节与合成frontend index相同、旧端口不再接入。全SQL不动，仅web-listener.json更新（600），其余DATA_DIR文件精确不变；Python生成配置与实际peer服务能力/上游字段完整对应。Go不模拟Nginx文件，按维护者原生替代决定比较公开结果和保存状态；真实Nginx叠加仍O9/O14容器部分覆盖。
- Source新增显式loopback host/port选项以构建双栈peer，默认行为不变，非回环host在绑定前拒绝。新Switch比较器故意错SQL、无新监听、坏/未轮换token、单栈、错误页面、旧端口残留或错CLI序列均失败。现86项自检通过（含下条精确JSON迁移门禁）。
- 四组60变体（Web成功/状态/占用与settings-sync）复跑：59compared、R228-F01严格different1。该POST版本接口405从HTML转JSON，精确命中已批准Step B #3；只接受同405、准确公开message和success=false、只有content-type/body两条差异。Allow、状态、group/route/case/variant、额外JSON、SQL/files差异会重开；为该请求增Stable独立SQL/files不变观察，保留完整原始diff，未全局忽略错误页。findings现closed81/accepted6/open18/O12单列1。
- H1088/H1104/H1109以实际重跑的settings校验/保留端口/无部署三个Python分支关联，新增H3不增加请求。统一9344变体、原R4287（S689/F2665/B933）、H576、G16；原R仍差243、H仍差536，另有partial/不可达槽及长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups web-listener-complete-switch,web-listener-complete-readonly,web-listener-complete-occupied,settings-sync --baseline 7521bb5c
current compared=59 different=1；accepted=1 open=0；design_errors=[] seconds=11.49；exit=0
86 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-web-switch.json
```

继续原生监听失败边界、MosDNS/发现选择器和其余清单；最终.32全量未执行。未派子agent/未推送/未读Go业务实现。

## 批次 100：MosDNS完整编辑器选择、失效规则及实际提交失败

- 8新增变体、原R5（R090-B05/B06/B07/F09/F10）；P4 query优先、P5 header选择other、P6默认各通过一次真实完整编辑器事务。验证selected revision/generation各+1、所有foreign行/文件不变；目标local_dns/cache_size确实更新、未知嵌套false/空数组保留、api_addr移除，返回publish_guard准确对应目标和shared revision。R090-B04无公开URL alias仍N/A，没有虚构路由。
- direct/proxy的rule/ruleset四种不存在引用均409，准确配置空间前缀/中文/usages和全SQL/files相比，不因失败留下默认化设置。第一次草稿少了Python repository添加的“配置 默认配置:”消息前缀而触发参考前提错误，已修正后四种实际重跑通过，未计作业务差异。
- SQLite BEFORE触发器拒绝真实editor commit得到500，两端数据和文件在各自前后严格不变。仅HTML→JSON公共500错误精确符合Step B #3，链接accepted；其余状态、存储变化、额外字段/头继续open。未把“存储冲突”改成永远相等的比较；保留两条严格原始diff。86项自检通过，包括该精确接受门禁的额外SQL/file/Allow/status反例。
- .63常规runner：compared7、严格different1/accepted1/open0、design_errors=[]。统一9352变体、原R4292（S689/F2667/B936）、H576、G16；findings closed81/accepted7/open18/O12单列1。原R仍差238、H仍差536及partial/不可达槽/长流程。

```text
.63 ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups mosdns-whole-editor-selectors,mosdns-whole-editor-failures --baseline 7521bb5c
current compared=7 different=1 accepted=1 open=0 design_errors=[]；exit=0
86 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-mosdns-edges.json
```

继续余下helpers、生成/发现选择器和长流程；最终.32全量未执行。

## 批次 101：下载代理校验、无Location和耗尽镜像

- 新增20变体：H115九种非字符串值、H116三个path/query/fragment、H117五种URL解析错误，公开R202统一400且全SQL/files/HTTP0次；H116到H117的异常包装以同次HTTP关联。H119两种profile路径的302无Location真实HTTP1次后400，完整协议及存储不变。H123通过回环HTTP proxy使GitHub原地址CONNECT503、镜像HTTP503，实际耗尽两个候选并得到502/RULE_SOURCE_UNAVAILABLE/no-store；未访问GitHub、mirror.example或生产网络。
- 新增H6（115/116/117/119/121/123）；H121来自实际重跑原有R087/R138四跳loop请求。62变体（含原guards42）.63复跑：compared61、H123 different1、design_errors=[]。H123仅CONNECT User-Agent：Python空、Go Go-http-client/1.1；这是既有CONNECT传输差异的另一个实例，仍open，不自行accepted。两端HTTP502及业务响应、SQL/文件其余均一致。
- source审计纠正：R087/R138-F15的size限制在mosdns._fetch_remote_content外层chunks里，不经过request_rule(max_bytes)的H122，未错误贴标签。H118的未知method同样不是public route POST错误能驱动；当前config callers固定get/head。matrix已写部分覆盖/跨领域调用边界，而不是用相似错误冒充私有分支。
- SourceStable现在显式支持expected请求数参数，仍组合原有完整Stable与Exchange，非恒等比较；86项原有SQL/files/协议/错误接受篡改自检通过。统一9372变体、原R4292（S689/F2667/B936）、H582、G16；findings closed81/accepted7/open19/O12单列1。原R仍差238、H仍差530及partial/长流程。

```text
.63 ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-fetch-proxy-normalizer-guards,rule-fetch-redirect-without-location,rule-fetch-exhausted-candidates,rule-proxy-guards --baseline 7521bb5c
current compared=61 different=1 open=1 design_errors=[]；exit=1（保留真实CONNECT差异）
86 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-rule-fetch-edges.json
```

继续规则内容helper、其余清单；最终.32全量未执行。

## 批次 102：规则内容检查/metadata/追加的可达helper分支

- 38新增实际HTTP变体覆盖H124–H127、H134–H140、H146–H149；检查路径逐字保留content/changes并精确验证200中的errors/warnings：Domain误填Classical、坏domain/CIDR/歧义外部语法、YAML根/重复payload/错类型/别名序列/锚点/块文本/嵌套复杂值等。append_lines路径则对同类payload结构返回400通用提示；不是把两个不同分支的错误合并。
- 创建入口R165还验证5种非法metadata、2种内容错误、5种非文本content；失败全SQL/files/源HTTP0次，均在缓存和提交前拒绝。H134由此真正到达inspect_domain_content，不能拿R171提前校验错误代替。
- helper标签新增case+variant+route精确键：重跑R087/R138声明组后仅text/F13附H124/H145、仅text/B04附H143。错variant/route不贴标签，Go尚未实现状态不改，新增对应自检；同次原请求不增加HTTP数。新增H17，统一9410变体、原R仍4292（S689/F2667/B936）、H599、G16。
- .63四组60变体：22compared（原声明组）、38Go尚未实现（新内容路由组），design_errors=[]、86自检和故意错误Go响应检出通过。新用例参考端均执行了准确分支及持久化不变前提，Go路由接通后自动比较；不计作通过。findings仍closed81/accepted7/open19/O12单列1。

```text
.63 ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-content-inspection-helpers,rule-content-metadata-helpers,rule-content-append-helpers,rule-proxy-declarations --baseline 7521bb5c
current compared=22 go-unimplemented=38 design_errors=[]；exit=0
86 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-rule-content-helpers.json（首次不含后补H134五变体，统一报告含完整60）
```

原R仍差238、H仍差513，另有已列partial/防御性不可达槽；继续GET拉取日志/HEAD、其余helper与长流程，最终.32全量未执行。

## 批次 103：11条config拉取的HEAD与真实GET日志

- R021/R022/R023/R024/R025/R026/R029/R030/R031/R032/R034-B90共11新增原编号，遍历所有真实pull模板。每个场景先GET，再HEAD，再GET作为有序日志屏障；比较本端HEAD与GET的Content-Type/Disposition/cache/ETag/Last-Modified/Vary/profile/Content-Length一致、HEAD无body，只有两条准确INFO拉取事件、没有HEAD事件，query中的能力值与已知capabilities不出现在新增日志中。完整SQL/files前后不变，未来Go接通后同样实测。
- 两条GET使用不同受控User-Agent标记；等后一个GET事件到达再判HEAD缺席，不用固定日志或单次睡眠猜测。文件logger只解析时间/包装格式以外的完整中文消息与severity，JSON/plain双格式保留事件字段；非法UTF-8、缺失事件、额外HEAD事件、错误headers/status或capability泄露的故意错误自检均能失败。共87项自检通过。
- .63本次SSH端口连接超时255，测试未成功启动/回传，已明确按brief回退.32，不计测试失败。.32独立和常规runner均跑完11变体，全部Go尚未实现、design_errors=[]；Python真实GET/HEAD/日志/存储前提全部通过，未把未接通视为相比通过。X07仍只有部分事件覆盖，轮转/完整生命周期后续继续。
- 统一9421变体、原R4303（S689/F2667/B947）、H599、G16；findings仍closed81/accepted7/open19/O12单列1。原R仍差227、H仍差513及partial/N/A/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups config-pull-head-and-real-logs --baseline 7521bb5c
current go-unimplemented=11 design_errors=[]；exit=0
87 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-pull-head-log.json
```

继续真实日志warning/tail、I/O失败与其余清单；最终.32全量未执行。未读Go业务、未派子agent、未推送。

## 批次 104：缺失拉取路径的WARNING、真实tail与stat权限错误

- R030-B93访问不存在的/api/config/mihomo/x也实际写一条WARNING“配置被拉取”事件，HTTP404、来源与独立User-Agent准确、query/capabilities不进日志，SQL/files不变。runtime_writes显式标记日志前置影响；Go该拉取路由未接通，所以随后已接通的R076-B91正确标依赖未实现，不能把缺失前置日志当tail bug。
- R076-B91从服务自身生成的文件取真实warning，level正则与中文search都需匹配。User-Agent刻意不含WARNING，避免过滤因客户端词语偶然命中。每端原始rows必须准确来自自己文件的过滤结果、filtered_lines严格保留，total_lines只有验证等于本端实际总行数后才映射；log_file按实际指派路径单射比较。完整中文事件/severity继续比，额外/漏掉/错误过滤行、坏总数、能力泄露的自检能失败，不用固定日志。
- R075-F08在与数据库分离的owned日志目录施加真实chmod000，driver和两端进程均为普通用户；验证stat确实EACCES、权限没有被悄悄改掉、SQL/files不变。**新增open：Python500/permission denied，Go200/success=true/exists=false**，把权限错误误当缺失。路径/原因检查加上全部响应字段保留证据，未用API错误JSON迁移来接受状态变化。该差异已在.32独立与统一报告重跑稳定复现。
- .32两组3新增变体：Go尚未实现1、依赖未实现1、different/open1、design_errors=[]，89项自检和故意错误Go响应检出通过。统一9424变体、原R4306（S689/F2668/B949）、H599、G16；findings closed81/accepted7/open20/O12单列1。原R仍差224、H仍差513及partial/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups config-pull-warning-and-real-tail,logs-real-stat-permission --baseline 7521bb5c
current go-unimplemented=1 go-unimplemented-dependency=1 different=1 open=1 design_errors=[] seconds=6.51；exit=1
89 self-checks + deliberately wrong-Go-response detection OK
独立原始报告/tmp/configdiff-real-log-edges.json（首轮UA标记后已移除warning词；统一报告为修正后）
```

.63仍因SSH连接超时按brief使用.32。继续缓存I/O失败与剩余清单，最终.32全量未执行。

## 批次 105：真实规则缓存权限失败与前缀保存失败

- 新增7变体、5个原R编号（R165-F16/R166-F10/R167-F14/R169-F18/R174-F10）。普通用户将真实缓存文件设0444、父目录0500，分别执行创建、删除、更新和draft保存；确认错误后权限仍在、旧缓存原字节及全部文件没有被覆盖。创建的失败要求完整数据库不变；删除/更新/draft按Python真实顺序验证已提交的一次shared事务（generation/revision/commit ID与准确规则列表），其余行/元数据不变，未把源端的提交后缓存错误误写成回滚。
- draft先走公开GET取得各端自己的revision；未接通的前置会阻止后续已接通路由被误比较。GitHub前缀保存用SQLite触发器拒绝实际写入，核对500/ConfigStorageError及全SQL/files不变，再公开GET确认旧前缀仍在，未泄漏未提交内存状态。
- .32独立草稿及正式runner均7个Go尚未实现、design_errors=[]、90项篡改自检通过；不计为Go通过。统一9431变体、原R4311（S689/F2673/B949）、H599、G16；findings仍closed81/accepted7/open20/O12单列1。原R仍差219、H仍差513及partial/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-library-real-cache-permissions,rule-library-proxy-save-failure --baseline 7521bb5c
current go-unimplemented=7 design_errors=[] seconds=9.57；exit=0
90 self-checks + deliberately wrong-Go-response detection OK
独立草稿已归档/tmp/configdiff-rule-cache-faults-draft.json，未提交临时报告
```

继续独立登录日志和并发规则写入；最终.32全量未执行。未读Go业务、未派子agent、未推送。

## 批次 106：已接通登录路由的真实WARNING与tail

- R011-F03实际错误密码返回401后，观察每端新增文件日志必须只有一条完整中文登录失败WARNING、真实用户和来源。检查正确/错误密码、JWT签名密钥、双方JWT及seed能力值均不出现。R076-B91随后按warning和中文search过滤真实文件；本端原始行/总数/过滤数先严格自证，再仅投影日志时间/包装，完整中文message与WARNING词不变。两端已接通，因此这两条均真正compared，不依赖config拉取TODO。
- 复跑原pull warning/tail与11条HEAD，确认通用marker参数没有破坏原语义。15变体：compared2、Go尚未实现12、依赖未实现1、design_errors=[]；91项自检通过，新增auth marker/来源/等级/泄密篡改全部检出。
- 统一9433变体、原R仍4311（S689/F2673/B949）、H599/G16；findings仍closed81/accepted7/open20/O12单列1。X07仍部分覆盖，尚不能以两类日志宣称完整生命周期或轮转通过。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups auth-warning-and-real-tail,config-pull-warning-and-real-tail,config-pull-head-and-real-logs --baseline 7521bb5c
current compared=2 go-unimplemented=12 go-unimplemented-dependency=1 design_errors=[]；exit=0
91 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-auth-log-draft.json
```

继续并发规则写入和剩余条目；最终.32全量未执行。

## 批次 107：下载中的公开编辑冲突与两个远端写者

- R167-F12真实暂扣/slow下载，让另一个公开PUT先改名并提交；随后释放下载，原编辑409。R167-B03先让第一个远端编辑到达HTTP gate，再由第二个远端编辑下载/提交，最后释放第一个，只允许一个200、一个409与一次generation/shared revision变化。没有直接调用业务helper或用假的revision替代并发。
- 比较两位客户端的完整响应与headers、精确源GET序列、完整SQL及文件。源端Conflict是Flask HTML409，检查其中准确中文；未假设JSON。row.revision使用真实新generation；Python改名会重写原缓存并创建.bak，被拒远端编辑仍留下其声明绑定的预写cache，两种现象按source顺序逐字验证，未隐藏残留文件。
- 92项自检通过：gate未到达、超时、双成功/丢提交、非法commit ID、错误/缺失被拒缓存、HTTP序列及第二个客户端响应的篡改均能失败。.32草稿及正式各2变体、Go尚未实现2、design_errors=[]；统一9435变体、原R4313（S689/F2674/B950）、H599/G16；findings仍closed81/accepted7/open20/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-library-concurrent-source-edit --baseline 7521bb5c
current go-unimplemented=2 design_errors=[]；exit=0
92 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-rule-concurrency-draft.json
```

原R仍差217、H仍差513及partial/N/A/长流程。继续parse-lines与其余条目；最终.32全量未执行。

## 批次 108：parse-lines的Sub-Store协议与逐项失败

- R102-S01/S02/B03/B90/F13共9新变体，真实Surge Snell/Loon行、中文emoji名称、首尾空白trim经同一个回环fake转换。逐次验证随机临时名称格式/唯一性、POST创建的noFlow字段、GET content解码后精确输入及ClashMeta路径、DELETE清理，无遗留临时订阅；完整SQL/files保持不变。复用已有严格NodeExchange与篡改自检，不直接调用converter。
- HTML、空proxies、坏YAML、下载503均返回HTTP200中的逐项中文error，且下载失败照样DELETE。独立不可用回环runtime场景含两种合法文本和空白行，准确得到3个逐项error，无持久化写入。fake只证明协议与结果处理；真实Sub-Store对这些客户端语法的转换正确性仍属于对应集成边界。
- .32独立及正式均compared9、design_errors=[]，92项自检通过，无新差异。统一9444变体、原R4318（S691/F2675/B952）、H599/G16；findings仍closed81/accepted7/open20/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups nodes-parse-lines-converter,nodes-parse-lines-unavailable --baseline 7521bb5c
current compared=9 design_errors=[]；exit=0
92 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-node-lines-draft.json
```

原R仍差212、H仍差513及partial/N/A/长流程。继续node边界与剩余条目；最终.32全量未执行。

## 批次 109：rebase新启动入口并复验全部历史findings

- 已rebase到go `3c8a1760`。behaviour-changes追加冲突同时保留integration的N7/O3等决定和维护者的Go取代Nginx决定，未覆盖任何一方。公开cmd入口拆成service.go后，旧main.go文本overlay正确停在构建前；改用公开命令已有的`configflow_proddiff` tag / `PRODDIFF_ROUTES`清单入口，无需打开或改写任何业务文件。teardown给公开8秒drain另留2秒flush/exit时间，没有缩短或停用生产后台循环。
- .32原30组、570变体重新执行，覆盖109项历史review；compared427、Go尚未实现114、严格different28（accepted7/open21）、O12单列1、design_errors=[]，342.20秒、exit1。全部review的checked_baseline确实为3c8a1760，没有给未执行条目改基线。
- **T27 real-tick/due-and-backoff重新open**：Python四个到期源各实际GET一次，Go在真实40秒观察窗内零GET，缓存/任务/健康/通知未产生对应更新。单独再跑scheduler-tick + 已接通auth真实日志后仍复现49条严格diff；11compared/1open、design_errors=[]、46.50秒。未读Go业务定位或自行修改实现；按新基线保留完整合成证据。原closed81变为closed80，accepted7、open21、O12单列1；其他既有open仍保持open，已接受项仍精确指向批准条目。
- 统一覆盖数未变：9444变体、原R4318（S691/F2675/B952）、H599/G16。构建前停止的首轮不算业务执行，修正公开入口后重新完整跑了所有30组。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups <全部30个finding所属组，见/tmp/configdiff-findings-rebased.log> --baseline 3c8a1760
current compared=427 go-unimplemented=114 different=28 accepted=7 open=21 O12-separate=1 design_errors=[] seconds=342.20；exit=1
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups scheduler-tick,auth-warning-and-real-tail --baseline 3c8a1760
current compared=11 different=1 open=1 design_errors=[] seconds=46.50；exit=1
比较器篡改自检 + deliberately wrong-Go-response detection OK
```

.63不可达继续按brief回退.32。余下node边界和OxiDNS helper草稿继续，最终.32完整all仍未执行。

## 批次 110：节点来源身份、真实批次上限与损坏历史

- 新增6个HTTP变体：R097-B01的坏JSON/数组root/旧method记录均返回空results、原文件与全SQL不变；R098-B01同中文emoji显示名的两个手动节点按两个来源key独立测速；R098-B02真实20个结构化节点与5个并发Sub-Store转换边界，检查全量结果、每项一次history、完整落盘results与响应相同、SQL无修改。
- 五转换逐个证明随机临时名唯一、create/download/content/delete精确且无泄漏，只在每条cycle通过后按输入关联不同cycle的交错。原worker stdin继续完整比较所有节点/认证/DNS参数；没有删掉协议记录或把不同内容当成随机值。新增篡改自检覆盖缺少cleanup、重复cycle、先用后建、错误方法/路径/noFlow及非对象create body；非法Go协议成为真实diff而非比较器异常。93项自检通过。
- 原H737/H741真实busy/六转换拒绝请求改用R098-F12/F17主编号，再关联同次H标签；旧group复跑替换旧结果，不增加请求或丢失H覆盖。连同其余candidate guards与锁释放复验共12变体，.32草稿及正式均compared12、design_errors=[]，没有新差异。
- 统一9450变体、原R4323（S691/F2677/B955）、H599/G16；findings保持closed80/accepted7/open21/O12单列1。原R仍差207、H仍差513及partial/N/A/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups node-history-corruption,node-selection-bounds,node-five-conversion-boundary,node-helper-candidate-guards,node-helper-concurrent-busy --baseline 3c8a1760
current compared=12 design_errors=[]；exit=0
93 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-node-bounds-draft.json
```

继续OxiDNS common/client策略helper与规则交付YAML分支；最终.32全量未执行。

## 批次 111：OxiDNS常用参数类型与客户端策略guard

- 68新变体（含2个真实editor准备请求），通过默认/scoped公开API覆盖28个新增H：TLS对象、严格bool/int、block_qtypes数组及成员、文本、dynamic route、AdGuard URL/mode、未知字段排序、fallback/local upstream坏YAML/错shape/缺地址，分别验证准确中文400，全SQL/files不变。fallback的前置解析H353/354和local后置解析H372/373各用独立输入，未把相似消息当同一分支。
- editor settings中的device_policies覆盖非字符串地址、多重slash、非法/超限prefix、IPv4-mapped小于96、组播/unspecified、列表及513条上限、row额外字段、映射后的重复地址、长/控制字符备注、未知mode；H416–422的异常再由同次请求关联H423。route+case精确helper映射只接editor两种路径，render等相似路由不贴标签，state保持TODO，自检验证这一边界。公开响应完整相比，不删除诊断示例地址或其他字段。
- .32草稿和正式均Go尚未实现68、design_errors=[]；Python参考前提全部成立，93项自检和故意错误Go响应检出通过。统一9518变体、原R4323（S691/F2677/B955）、H627/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups oxidns-common-type-guards,oxidns-client-policy-guards --baseline 3c8a1760
current go-unimplemented=68 design_errors=[]；exit=0
93 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-oxidns-common-guards-draft.json
```

原R仍差207、H仍差485及partial/N/A/长流程。继续真正Mihomo规则交付的YAML guard；最终.32全量未执行。

## 批次 112：真正local-rule交付的Mihomo YAML guard

- H128–133从R192/R153公开GET的target=mihomo到达，纠正Step A把这些helper挂到R087的入口说明。22新变体用真实内容型库与既存历史cache，分别检验sequence root、缺/错型/重复payload、锚点/别名、数字/嵌套/块条目、flow额外键重复及额外值跨行。响应必须逐字节保留原文、不能悄悄修复；全部SQL/文件保持精确不变。
- 每次从服务自己的新增文件日志观察准确一条WARNING（1 entries / line 0），没有用固定日志代替。消息与等级继续比，时间/包装沿用既有X07解析；新增自检能发现丢失/额外事件、WARN替代WARNING、消息变化和源cache被改写，94项自检通过。
- .32草稿及正式均22个Go尚未实现、design_errors=[]；不计Go通过。统一9540变体、原R仍4323（S691/F2677/B955）、H633/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-delivery-yaml-helpers --baseline 3c8a1760
current go-unimplemented=22 design_errors=[]；exit=0
94 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-rule-delivery-yaml-draft.json
```

原R仍差207、H仍差479及partial/N/A/长流程。继续profile选择与其余条目；最终.32全量未执行。

## 批次 113：异步发现与website准入的真实profile归属

- R051/R054/R060各补P4–P6，共9个原R边界编号、18新HTTP变体（含6次foreign poll和3次真实心跳）。probe默认空间缺代理/另一空间缺Agent，region默认关闭/另一空间开启但缺Agent；不同终态中文错误证明实际读取了所选空间。Completion轮询现在显式带所选owner，完整job身份/生命周期/终态继续验证，另一空间query即使带原owner header也404。前后全SQL/files精确不变。
- website用已心跳默认空间Agent与另一空间无Agent，两个空间分别到“缺采集记录”或“缺Agent”的准入分支，核对准确中文、X-ConfigFlow-Profile、零Agent HTTP及存储不变。此选择器不声称启动worker；成功worker仍由独立S/T组测。R051/R054/R060的P3无公开URL别名，明确N/A，不用query优先重复或不存在URL404冒充。
- 旧preflight18变体回归复跑，证明默认owner行为和重启后job丢失不受影响。.32草稿及正式共36compared、design_errors=[]，94项自检通过，无新差异。统一9558变体、原R4332（S691/F2677/B964）、H633/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups discovery-job-profile-selectors,discovery-website-profile-selectors,discovery-job-preflight --baseline 3c8a1760
current compared=36 design_errors=[]；exit=0
94 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-discovery-profiles-draft.json
```

原R仍差198（含已审计N/A槽）、H仍差479及partial/长流程。继续同步发现写入与其他条目；最终.32全量未执行。

## 批次 114：同步发现的成功跨空间写入与库/cache隔离

- R046/R055/R056/R059各补P4–P6，共12新增原R边界及HTTP变体。两个空间有独立direct/proxy/regional库与策略组：apply写所选库和history，undo仅去目标行保留注释，region写所选策略库，init实际分配新库/引用并更换所选指针。每个改变的库cache字节等于新提交content，完整SQL/files跨实现继续比。
- ForeignRows在各端独立禁止其他profile、system、无关shared字段、sections和其他meta变化；位置/revision/updated_at均不使用跨端时间许可。新增篡改自检发现foreign更新时间、nodes、system、section与schema变动，95项自检通过。UUID/cache派生比较器支持显式owner，默认行为由旧init9变体回归证明；不是忽略other空间的引用/文件。
- .32草稿及正式21变体：20compared、既有duplicate-targets多余.bak严格different/open1、design_errors=[]。12新变体全部compared，无新差异。统一9570变体、原R4344（S691/F2677/B976）、H633/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups discovery-shared-write-profile-selectors,discovery-init --baseline 3c8a1760
current compared=20 different=1 open=1 design_errors=[] seconds=22.17；exit=1
95 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-discovery-shared-profiles-draft.json
```

原R仍差186（含已审计N/A槽）、H仍差479及partial/长流程。继续artifact冻结/驱逐等，最终.32全量未执行。

## 批次 115：冻结artifact成员、Unicode附件、TTL和真实profile

- 先rebase到go `21dfb92c`，追加冲突保留A11原生归档决定与维护者的Web决定。已读更新后的brief：.63停用，.32/.64等价；README和native fixture提示同步，两台均可跑命名空间和最终门禁。生产循环与网络隔离逻辑未变。
- 14新变体补13个原R编号：R062/R126的TTL、Unicode附件与profile优先级，R063真实附加文件、改配置后的旧成员及P4–P6。两空间worker_threads=2/5，manifest/source/公开profile_id和header必须对应所选空间，附件raw bytes与路径/size/hash对应。通过公开POST把live设置改为worker9后，旧file必须逐字节等于本端最初验证的bundle成员，且读取全SQL/文件不变。主YAML跨端仍结构比较；不拿每次重组ZIP的容器时间戳冒充冻结内容。manual预览是全局admin record，换合法profile上下文仍读同一旧成员，按Python实际合同验证。
- 旧frozen错误组13变体复跑，真实错误关联H026–H029（非法ID、过期/错Agent、metadata坏、archive digest坏），新增H4。96项自检通过，故意改旧成员/存储可检出。草稿首轮把保存确认响应当设置对象，已改为核对真实SQL中的新配置；修正后独立及正式.32共27个Go尚未实现、design_errors=[]，未将其计通过。
- T27在.64独立及正式复验仍9compared/1open、49条diff、design_errors=[]：四个到期源Python实际GET，Go零GET，和.32相同。已将这一finding实际复验基线更新21dfb92c，其余review保留各自真实基线，未外推“全量新基线通过”。findings仍closed80/accepted7/open21/O12单列1。
- 统一9584变体、原R4357（S692/F2677/B988）、H637/G16。原R仍差173（含N/A）、H仍差475及partial/长流程。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups artifact-extra-frozen-profile-lifecycle,generated-frozen-reads --baseline 21dfb92c
current go-unimplemented=27 design_errors=[] seconds=6.45；exit=0
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups scheduler-tick --baseline 21dfb92c
current compared=9 different=1 open=1 design_errors=[]；exit=1
96 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-artifact-frozen-draft.json、/tmp/configdiff-scheduler-native64-draft.json
```

继续artifact驱逐/错误落盘及剩余条目；最终完整all仍未执行。

## 批次 116：真实第25份预览驱逐与私有目录权限

- R062-B01/R126-B01分别实际创建25份预览，逐次要求目录集合精确为最新24个真实ID；第25份后以公开download确认第一份409明确要求重新生成，再读最后一份成员仍200/原始字节，SQL和文件不变。每次只短暂停20ms分隔真实filesystem mtime，未改生产TTL、计数上限或业务函数。
- 完整artifact比较器校验每份真实bundle/metadata/成员及保留集合，未来Go接通后多留/误删/重渲染都能发现。现增加私有root与preview目录700验证及投影，和原file/member mode一起比较；已有篡改自检新增root或folder变755均必须失败，96项自检通过。
- 新增54变体、2原R编号，连同上批27变体回归共81。 .32草稿及正式均Go尚未实现81、design_errors=[]；Python参考前提真实执行，不计为Go通过。统一9638变体、原R4359（S692/F2677/B990）、H637/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups artifact-real-count-eviction,artifact-extra-frozen-profile-lifecycle,generated-frozen-reads --baseline 21dfb92c
current go-unimplemented=81 design_errors=[] seconds=13.73；exit=0
96 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-artifact-eviction-draft.json
```

原R仍差171（含N/A）、H仍差475及partial/长流程。继续真实preview写失败和生成错误等，最终完整all仍未执行。

## 批次 117：子生成错误传递与真实预览保存权限失败

- R062/R126-F09分别先向同一公开child生成路由发送相同输入，再发送artifact，要求各端自己的完整status/headers/JSON错误精确原样传递，全SQL/files无改动；若child尚未注册，明确作为依赖未实现，不把child的404当artifact缺陷。输入[1]的Python生成错误在写文件前发生，未伪造helper返回。
- R062/R126-F11在一次成功预览后，将真实preview root chmod0500，普通UID必须确实不能新建预览。失败后fault模式仍在，恢复权限后验证旧private record/archive全原始SHA、SQL全行不变，生成阶段只按Python顺序添加真实前输出.bak；没有错误地断言整个失败无文件副作用。用首次真实ID重新validate所有保留预览及完整ordinary/private内容，只有与既有artifact比较器相同的runtime lock边界被排除。
- 97项自检通过，fault未生效/偷偷chmod、旧私有字节被改、SQL/输出残留、错误包装或丢字段都可检出。.32草稿及正式6新变体均Go尚未实现、design_errors=[]，不计Go通过。统一9644变体、原R4363（S692/F2681/B990）、H637/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups artifact-child-error-propagation,artifact-real-save-permissions --baseline 21dfb92c
current go-unimplemented=6 design_errors=[] seconds=6.93；exit=0
97 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-artifact-failures-draft.json
```

原R仍差167（含N/A）、H仍差475及partial/长流程。继续真实体积上限与其他条目；最终完整all仍未执行。

## 批次 118：不修改生产常量的真实512MiB metadata上限

- R062-F10/R126-F10和H025真实到达：先通过公开R044保存>512MiB的ASCII前置注释，R072普通Surge生成必须200且正文<64KiB，再分别默认/scoped artifact。注释被生成器忽略、仍保留在source metadata，两次均500；准确中文上限诊断必须出现在本次真实response/log，DB完整不变、不创建private root，仅按真实顺序新增生成文件前版本.bak。没有减小MAX_BYTES、猴补serializer或用别的500充数。
- 大fixture/request spool放owned磁盘目录，默认Pair行为不改；前后完整SQL值/文件仍比。首次控制字符seed方案在.64内存不足、同方案在.32无法在ready期限启动Go，均发生在业务请求前，未进入统一报告/不算业务失败；已撤回该草稿，改为小库启动后公开保存ASCII内容，不保留readiness超时修改。失败报告仅在/tmp，.64遗留owned目录已清理。
- 修正方案在.32独立114.60秒及正式77.92秒完成，均4个Go尚未实现、design_errors=[]（含两个真实公开前置），未计为Go通过。独立user scope限制14GiB/无swap，正式journal峰值8.5GiB；目录退出已清空。README给出完整all同样的资源命令。
- 巨大字符串只有在完整比较已产生diff后，才像原bytes证据一样记录长度/完整UTF8-surrogatepass SHA/首尾hex，防止未来路由接通后将半GB错误样本写入git。新增自检证明末尾/Unicode/surrogate改动保留different及不等SHA，报告可安全JSON编码且有界；99项自检通过。这一报告修改不改变任何比较判定。
- 统一9648变体、原R4365（S692/F2683/B990）、H638/G16；findings仍closed80/accepted7/open21/O12单列1。原R仍差165（含N/A）、H仍差474及partial/长流程。

```text
.32 native_network.sh env XDG_RUNTIME_DIR=/run/user/1000 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus systemd-run --user --scope --quiet --collect -p MemoryMax=14G -p MemorySwapMax=0 ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups artifact-real-metadata-size-boundary --baseline 21dfb92c
current go-unimplemented=4 design_errors=[] seconds=77.92；exit=0
98 self-checks + deliberately wrong-Go-response detection OK（正式大夹具运行）
.64 native_network.sh bash -lc 'cd server/tools/configdiff && ~/cf-test/.venv/bin/python -m unittest -v test_compare'
Ran 99 tests in 3.841s / OK（其后增加有界证据自检）
独立有效报告/tmp/configdiff-artifact-size-draft.json
```

继续token轮换与版本等边界，最终完整all仍未执行。

## 批次 119：公开token保存后import/reset的隔离边界与默认构建版本

- R020-B90增加真实公开setter前置，再测试缺内部能力/能力冲突/有效当前能力三种import、登录及认证关闭；轮换/保留按T28实际合同验证，随后当前/退役能力不可保存为public token，拒绝时各端完整SQL/files不变。另有公开保存→reset→拒绝新/旧内部能力的独立序列。新前置放独立groups，原不依赖setter的import/reset组保留并回归，不能为新增前置牺牲独立路径。
- R228-B90检查默认未注入公开构建的精确version字段，完整值仍比，无shape投影屏蔽dev差异。发布时Python Git tag替换/Go注入的值约定仍列O12/O14部分覆盖，不把默认dev/dev外推为发布注入通过。
- 新增45变体、2原R编号；.64草稿和正式141变体：63compared、Go尚未实现19、依赖未实现58、严格different1/accepted1（既有version405），open0、design_errors=[]。99项自检及故意错误Go响应检出通过；未实现和依赖均不计Go通过。
- 统一9693变体、原R4367（S692/F2683/B992）、H638/G16；findings仍closed80/accepted7/open21/O12单列1。原R仍差163（含N/A）、H仍差474及partial/长流程。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups config-capability-lifecycle,config-reset,config-token-import-boundary,config-token-reset-boundary,settings-sync --baseline 21dfb92c
current compared=63 go-unimplemented=19 go-unimplemented-dependency=58 different=1 accepted=1 open=0 design_errors=[] seconds=22.39；exit=0
99 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-config-capability-edges-draft.json
```

继续生成与其他原路由槽、helper；最终完整all仍未执行。

## 批次 120：artifact 的 hosts 报告、其他服务null与metadata脱敏

- 两种artifact路由各13种Mihomo hosts/DNS场景，含真实公开Agent核心版本上报，逐项断言ignored原因、合并结果、旧核心提示与生成配置；其他四服务在两种路由均要求两个字段为null。缓存不含链式节点时dialer报告必须null，非空dialer报告仍待补。保留原preview/G16及Agent前置，artifact主编号改为R062/R126-S90并关联G16。
- R062/R126-B90各4种编码：真实附加文件名包含能力，公开path只能精确[REDACTED]，content保留实际生成rule-proxy URL；完整归档路径/正文、SQL/files继续比较。专用比较器先核对完整manifest metadata，只恢复已证明脱敏的单字段以复用通用校验；漏脱敏、误脱敏兄弟、改size/hash/content/profile都有篡改自检。
- .64草稿及正式57变体：2compared、55Go尚未实现、open0、design_errors=[]，100项自检+故意错误Go响应检出。净增29变体、4原R编号。统一9722变体、原R4371（S694/F2683/B994）、H638/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups subscription-hosts-reports,artifact-redaction-metadata-boundary --baseline 21dfb92c
current compared=2 go-unimplemented=55 design_errors=[] seconds=25.06；exit=0
100 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-config-artifact-{reports,redaction}-draft.json
```

原R仍差159（含N/A）、H仍差474及partial/长流程。继续生成错误/离线缓存；最终完整all仍未执行。

## 批次 121：生成失败、离线规则缓存与converter防御分支

- 默认/scoped各客户端实际缺策略引用→400，预览非对象→500、OxiDNS下载非对象→500、MosDNS非对象一般异常→500，非法原生YAML→400；均要求完整SQL/files不变。三个文本下载器在普通UID下真实generated目录0500触发EACCES，比较故障是否生效/权限是否被偷偷改动、全行和文件未留下半成品；新增故意篡改检查，101项自检通过。
- 10种下载S04均使用真实启用远端声明+有效by-id规则缓存，对应源真实返回503但必须零请求；MosDNS/OxiDNS ZIP必须含缓存的真实转换内容。仅允许选定文本生成文件及精确前版本.bak变化，完整SQL/其他文件不变，完整下载结构和headers继续比较。
- 公开preview到达H152、H155–165、H167–169和H171–174，共19个新helper编号，精确中文错误/500 YAML上下文和只读状态验证，两种路由各执行。H014既有MRS前置现在以R068-F08命名并新增scoped R131-F08，保留H014/H015关联，13种旧callback回归保留。
- .64正式生成/归档60变体均Go尚未实现、design_errors=[]，26.82秒；.32正式helper42变体均Go尚未实现、design_errors=[]，不计Go通过。合计净增89变体、48个原R编号、19个H。统一9811变体、原R4419（S704/F2721/B994）、H657/G16；findings仍closed80/accepted7/open21/O12单列1。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups generator-real-failures,generator-offline-valid-cache,archive-callback-helper-guards --baseline 21dfb92c
current go-unimplemented=60 design_errors=[] seconds=26.82；exit=0
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups generator-oxidns-guard-branches,generator-mihomo-guard-branches --baseline 21dfb92c
current go-unimplemented=42 design_errors=[]；exit=0
101 self-checks + deliberately wrong-Go-response detection OK
独立报告/tmp/configdiff-config-generator-{edges,guards}-draft.json
```

原R仍差111（含N/A）、H仍差455及partial/长流程；继续DNS/native、provider与剩余路由。最终完整all仍未执行。

## 批次 122：新go基线复验、DNS/native原子边界与provider具名失败

- 已rebase到go 6b50151c；.64实际复验全部30个既有finding groups，571变体、447.94秒，design_errors=[]。T27仍49处差异，不把生命周期重构当预期；R222-F11实际compared关闭。两项通知evidence跨秒出现假差异，补入已有capture_notice：先核对各端自己run_count/last_run_at/status、通知evidence和checked_at/last_seen_at，再建立对应，所有其他字段严格比较。两组42变体实际复跑后，两条时间假证据关闭，内部callback的2个真实差异仍保留。既有finding为closed81/accepted7/open20/O12单列1，全部有真实最后复验基线，不把未跑的条目更新成最新。
- 13个新原R用例：新增group改变following DNS在自定义native中失败时整事务不动；手写domain_set确实使用普通规则原子/规则集路径时，删除400准确中文且不删SQL/文件；显式IP DNS不可转换、显式DNS的native严格更新冲突；禁用library仍hydrate显示、native禁用规则暂存refused；显式DNS不随policy变化、真实recognizable native重排。再用公开草稿/真实catalog绑定覆盖H904–907/H910/H911/H914，并将已实际经过的R189及strict失败关联H909/H912/H913/H917，共11新H。
- provider补14原R：有效JWT混入当前/退役能力仍401；真实active panel chain分别拒绝Surge/Loon；坏缓存拨号图400不可降级；真实last-good输出路径被目录占用→500。后两种500仅完整HTML→JSON错误边界的精确两处差异按已批准Step B #3标accepted，保留raw证据；任何额外status/字段/header/SQL/files变化均不接受，专用篡改自检通过。
- S04首次草稿证明“当前URL缓存”不再网络请求，已撤回其错误前提；改成无URL来源证明的legacy缓存才实际发出一次403并降级。缓存固定updated_at保持严格输入，不当成当前时刻。两端仅英文异常多status=403的后缀按O6作精确可选归一，其余comment/源地址/错误类/状态码/数据仍比；新增改变status/message/count注释的负向自检。103项自检+故意错误Go响应检出。
- .32正式86变体：15compared、69Go尚未实现、strict different2/accepted2/open0，design_errors=[]，61.95秒。净增46变体、27原R、11H。统一9857变体、原R4446（S706/F2741/B999）、H668/G16；findings合计closed81/accepted9/open20/O12单列1。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups [上列30个既有finding groups] --baseline 6b50151c
current compared=427 go-unimplemented=114 different=29 accepted=7 open=22 O12-separate=1 design_errors=[] seconds=447.94；exit=1
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-proxy-local-cache,rule-proxy-declarations --baseline 6b50151c
current compared=40 different=2 design_errors=[] seconds=51.14；exit=1（只余已知内部callback差异）
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-dns-group-strict-boundaries,rule-dns-native-write-boundaries,rule-dns-disabled-reads,rule-dns-explicit-and-reorder,rule-dns-extra-guard-branches,rule-dns-choices,provider-named-reserved-auth,provider-real-stale-cache,provider-named-graph-and-file-failures --baseline 6b50151c
current compared=15 go-unimplemented=69 different=2 accepted=2 open=0 design_errors=[]；exit=0
103 self-checks + deliberately wrong-Go-response detection OK
```

原R仍差84（含N/A）、H仍差444及partial/长流程；继续剩余import/native、任务与断连等。最终完整all仍未执行。

## 批次 123：完整导入闭包、原生YAML/附件真实限额与已核对的helper标签

- 完整import新增31变体：R028-F10当前schema的profile key/内容ID不一致、R028-F12 Agent引用不存在profile，以及H855/H859–863/H870–874的组名/跟随/排序/规则类型/规则库/DNS选择/Action/Surge引用错误。公开HTTP精确中文400/409，完整SQL/files及零fake出站；不是给同一错误批量套H编号。
- 原生编辑器default/scoped新增92变体，实际到达H469–496：安全键/重复键、真实UTF8 2MiB、深度34/50001节点、循环alias、binary/非有限数、主文件/metadata/state覆盖、路径重复/层级冲突、真实32MiB字符上限/16MiB字节上限/65MiB总量、513次include展开、include类型/路径/循环/缺失/UTF8/根类型和完整schema错误。生产常量未修改，全部拒绝保持完整SQL/files不变。未知插件的首份草稿只产生warning，已撤回其错误前提；改用已知cache插件错型size实际到达error，两个空间均重新跑通。
- H497/H498必须用template自定义插件→完整ZIP资源materializer才可到达；native schema会先拒绝，4个新变体证明真实后续guard并关联H023。既有G10完整ZIP缺资源/运行数据重合、R180/R184手写provider删除和G13两客户端拨号拒绝重新执行后关联H023/H374/H500/H501/H170/H175。再核对并实际复跑native apply的字段/standard_add/rename/动态引用/依赖guard，关联8个真实H；未命中的重复防御分支不借相似文本计覆盖。
- .32正式207变体：44compared、163Go尚未实现、design_errors=[]，47.75秒；.64随后named-native-applies正式均Go尚未实现。净增127请求变体、2原R和55H；103自检与故意错误Go响应检出通过。统一9984变体、原R4448（S706/F2743/B999）、H723/G16；findings不变closed81/accepted9/open20/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups repository-complete-import-edge-invariants,oxidns-native-yaml-attachment-real-limits,oxidns-template-package-resource-guards,oxidns-asset-guards,delivery-graph,rule-dns-native-write-boundaries --baseline 6b50151c
current compared=44 go-unimplemented=163 design_errors=[] seconds=47.75；exit=0
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups oxidns-named-native-applies --baseline 6b50151c
current Go尚未实现、design_errors=[]；exit=0
103 self-checks + deliberately wrong-Go-response detection OK
独立有效报告/tmp/configdiff-config-{import-edges,native-guards,resource-refs,helper-graph-assets,native-applies-labels}-draft.json
```

原R仍差82（含N/A）、H仍差389及partial/长流程；继续任务、生成器断连与剩余guard。最终完整all仍未执行。

## 批次 124：真实Web状态落盘失败、任务并发、WebDAV时区与英文诊断纠正

- R206-F20在完整Go frontend/Python peer部署中，先预置实际锁文件，再将真实DATA_DIR chmod0500，避免提前失败在建锁。换端口最终保存失败503后，两端旧端口/双栈/原健康能力保持不变、新端口关闭、全部SQL/files原样；source还必须实际check→reload→rollback reload并恢复原Nginx配置。普通UID故障生效和目录模式保持都单独比较。原成功换端口同时回归；都compared。自检逐项翻转10个真实不变量必须检出。
- R226-B03两个真实并发HTTP客户端分别设interval=60、替换为新URL且interval=60，要求完整新对象、精确两个提交/修订、无丢URL/间隔、无外部请求/其他行变化；完整两响应/SQL/files自动跨端比较，105项自检通过。Go任务间隔未接通，实际Python前提通过，仅计未实现。
- R014-B90原清单错误：GET保存设置根本不发DAV；用fake证明零请求且全状态不变，协议序列仍由R016/R017承担。R016-F14三种不完整凭据400零网络但会新增准确的失败manual任务回执，首稿的“完全无文件变化”假设已撤回并修正，106项自检验证不得漏记回执或夹带别的状态变化。R016-F15实际MKCOL403→400到达H031，先前误设500已更正。
- R016-B91用Asia/Shanghai及+14/-12两端时区，不改时钟：上一本地日07:30/08:30的版本跨UTC午夜，但按同一本地日只保留最新；过期日、当天新上传、无关对象逐一核对。上传文件名按真实TZ/时间窗验证、Basic/方法/路径/完整上传内容/回执/files继续比较。仅PROPFIND空body→allprop XML这一已知未批准线级差异保留open。
- 依据矩阵开头已审核的O6“Python内部异常英文措辞只比拒绝/status/字段形态”，纠正DAV三个旧假差异及新增mkdir：只在精确body/回执/通知路径允许已证明的有限等价值（NotEnoughSpace↔HTTP507、ResponseErrorCode HTTP403↔HTTP403、NoConnection↔network request failed）。中文、目录、HTTP数字、额外英文/字段、头与副作用均不能被掩盖；107项自检含故意篡改这些字段。没有要求Go实现Python异常类，没有新增行为批准。三个旧英文差异和新增mkdir变体compared/closed；实际PROPFIND差异仍保留。
- 另补H151/H153/H154/H166两种preview：缺下载来源、不支持的单条规则、空转换结果、实际生成downloader错型timeout；8变体均Go尚未实现。
- .32正式Web/task/DAV共29变体：18compared、1Go尚未实现、10open strict差异（全部为已知PROPFIND模式的不同请求），design_errors=[]，65.99秒；.64随后生成guard8变体均TODO。净增18变体、5原R、7H。统一10002变体、原R4453（S706/F2745/B1002）、H730/G16；findings closed85/accepted9/open20/O12单列1。新mkdir关闭属于本批O6比较器纠正，绝不冒充Go业务修复。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups web-listener-real-save-failure,web-listener-complete-switch,tasks-interval-concurrent-URL-retention,webdav-named-preflight-and-directory-failure,webdav-local-day-retention-boundary,settings-webdav --baseline 6b50151c
current compared=18 go-unimplemented=1 different=10 accepted=0 open=10 design_errors=[] seconds=65.99；exit=1
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups generator-oxidns-selected-rule-guards --baseline 6b50151c
current go-unimplemented=8 design_errors=[]；exit=0
107 self-checks + deliberately wrong-Go-response detection OK
```

原R仍差77（含N/A）、H仍差382及partial/长流程；继续任务、断连与剩余guard。最终完整all仍未执行。

## 批次 125：设备任务拒绝、完整DAV后台完成与真实客户端断开

- R225-S03以实际设备绑定/状态和四次失败历史检查任务行；禁用的Agent规则同步保留历史、DNS跟随显示专用设备状态、router分钟间隔换算成秒且明确不可运行。R227-F14/F15分别验证未启用/无baseline路由器和无DNS绑定Agent的409准确中文；全SQL/files不变，fake零出站。
- R227-S04连续两次真实202→DAV HEAD/MKCOL/PUT→成功持久回执，完整上传导出JSON、认证/路径/请求序列、远端对象、文件和SQL比较；第二次证明锁释放。R227-B02在实际HEAD进入fake后阻塞响应，客户端不读任何响应即关闭socket，再释放DAV，必须完成上传与回执。没有给未观察的初始响应编造202；记录status=null和真实gate证据。
- fake DAV默认路径保持原语义，只给显式gate释放记录锁；新增自检证明等待响应期间可读取真实请求记录，避免观察器死锁。110项自检含故意错误Go响应，以及SQL变化、任务失败、远端对象变化的负向检查。
- 旧DAV英文诊断的closed条目明确附bd7363fd：这是O6比较器纠正提交，不冒充Go业务修复。共享fake变更后实际回归settings-webdav和目录失败；已知PROPFIND请求体差异继续open。
- .32正式6个新增请求变体均Go尚未实现，Python真实前提全部通过；净增5原R。累计10008变体、原R4458（S708/F2747/B1003）、H730/G16；findings closed85/accepted9/open20/O12单列1。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups tasks-device-status-and-retry-guards,tasks-real-WebDAV-completion,tasks-disconnected-WebDAV-completion,settings-webdav,webdav-named-preflight-and-directory-failure --baseline 6b50151c
110 self-checks + deliberately wrong-Go-response detection OK；design_errors=[]；exit=1（既有PROPFIND差异）
日志 /tmp/configdiff-batch125.log；独立有效草稿已移至本机/tmp，未提交草稿。
```

原R仍差72（含N/A）、H仍差382及partial/长流程；最终完整all仍未执行。

## 批次 126：ac1全量复验及已跑通的新分支（正式合并报告前）

- 已rebase到go ac1f2377；.32执行现有全部组，日志`/tmp/configdiff-all-ac1.log`，2995.27秒。实际10007变体：compared2786、Go未实现7034、依赖未实现100、strict different86、O12单列1；原R4458（S708/F2747/B1003）、H729/G16。现存85closed/9accepted/77open/O12单列1，所有既有finding均实际重跑。唯一design_error为H726的Go启动未就绪；原始全量不能算通过。长SSH连接后来reset，未自动取回报告，已用固定.32的rt --no-sync --back取回真实落盘结果，未拼接结果。
- H726随后在.64和.32分别实际compared、design_errors=[]（13.50/15.58秒），确认不是持续的跨实现差异；正式报告待下一次正常runner合并该组，保留原全量失败事实。
- 全量多出的57项中，35条是C35空密码行为在单一有状态序列中的传播、19条为已批准Step B #1/#3，三条为旧缓存固定时间误按当前时间检查。修正旧缓存时间规则；在R015-B01同时提交真实密码和keep_latest=0，使两端都发生真实行变化、后续基线相同，不吞掉S04原始密码/行revision差异。接受规则只匹配精确用例/路由/变体、status、完整差异字段和值，额外SQL/files/header一律保持open；C35精确记录本夹具8/6行revision及空/原密码。补篡改自检。已独立复跑backup：41compared+1严格差异/accepted1/open0；旧三项聚合缓存时间也实际compared。
- .64新规则库五变体：R167-S04在保留第二个真实下载项时禁用/再启用目标源，实际native YAML发生变化、无warning、组合和另一源不变；只有最后一个下载项时Python会因空download列表校验失败，先前“必然成功”前提撤回。R167-F15自定义native按non-strict返回200/warning并提交，不能误设成400；R176-F08非法HEAD地址实际ValueError逐项失败且自动禁用；R176-F10真实SQLite trigger整体保存500，SQL/files完全不动。均源前提通过、Go未接通。
- R227-S06实际202后保存waiting、attempt从2到3、清除旧job、准确事件/时间；来源没有成功发布，后台不应联系设备。第二次重试409且保持意图。只证明重试准入/持久化，后续HR发布仍partial。两变体源通过/Go未实现。
- Provider真实下载12种坏YAML/字段×默认/scoped/merged，连同前置写入72变体全部compared；覆盖H1057–1063。必须拒绝而不降级成旧节点，完整SQL、旧缓存/发布文件不变；只允许真实原始transport文件，其模式、正文、traffic、时刻单独核验。另H1087三形态和H1082–1084/H1071精确标签实际65变体compared；无效scheme、语法/引用/值错型、零更新无缓存均不是空响应伪覆盖。
- 10个default/scoped下载B04使用>1MiB真实大响应、小SO_RCVBUF，读取实际200/头和一个字节后shutdown真实socket；比较完整生成文件及其.bak和全部SQL/files，不给未读响应编造正文。20变体源实际通过/Go未接通，修正后复跑41.51秒。取消观察器每个布尔项/status/额外字段均有负向自检。
- R007-B01真实并发两源，第一源阻塞直到第二源正文已写出，再要求preview和真实provider均保留配置顺序；3变体compared。Source新增显式完成钩子，默认行为不变，另以真实两个HTTP客户端自检门的时序。R009-B01已有两空间引用中的真实中文emoji名称也实际compared。113项自检及故意错误Go响应检出。

这些有效草稿下一批通过普通runner合并，当前正式报告仍保留上述原始ac1全量状态；不手改case verdict。新覆盖17原R和12H，合并后预期剩55原R（含N/A）、370H及partial/长流程；不得把此估算当作正式运行计数。R103-F13/R163-F12的“坏现存拨号图→400”草稿撤回：schema5导入拒绝共享managed ref，手写坏raw参数也未触发预期guard；没有提交该草稿代码，也不计覆盖。继续找合法可达输入或逐条明确防御分支边界。

```text
.32 full all --baseline ac1f2377（14GiB MemoryMax/无scope swap）
remote SUMMARY: request_variants=10007 design_errors=[node-helper-payload-limit: 1 server did not become ready] seconds=2995.27
SSH returned255（长连接reset）；报告通过rt --no-sync --back取回，远端结果本身含上述design_error。
.64/.32 node-helper-payload-limit：各1compared，design_errors=[]；exit0
.64 provider-downloaded-shape-guards：72compared，design_errors=[]；exit0
.64 provider-manual-mode-missing-cache-guard,subscriptions-validation,provider-real-stale-cache：65compared；exit0
.64 generated-download-real-client-disconnect：20 Go未实现、design_errors=[]；exit0
.64 settings-backup-validation：41compared、1strict different/accepted1/open0；exit0
```

现有全量已完成一次，但新增内容及最终最新go完整门禁尚未完成，任务继续。

## 批次 127：正式合并新覆盖、精确批准归类与真实路由器发布互斥

- .64用普通runner正式运行20组、344变体：273compared、51Go未实现、20strict different/20accepted/open0、design_errors=[]，177.50秒。没有手工合并verdict；上一批新用例与H726缺失报告均由实际重跑进入统一JSON。
- C35空密码单例保留实际密码及backup行revision差异，按已批准C35精确accepted；不接受额外存储变化。其后真实B01同时恢复共同密码前提和改变keep_latest，34条传播差异实际closed。三条固定缓存日期假差异也实际closed。37条关闭均标3dc6e7bf并注明夹具修正，不冒充Go业务修复；所有closed都有提交依据。
- 19条原有批准类新发现只按有限用例集接受：nodes/subscriptions非法排序400精确中文与对应Python404/500、auth错误方法405、未知API404、失败HEAD的Content-Type；仍保留完整raw differences。额外头、SQL、文件或不同status/消息不能匹配。114项自检和故意错误Go响应检出。
- 此时统一10114变体、原R4475（S710/F2750/B1015）、H742/G16；累计compared2903、Go未实现7061、依赖未实现100、strict different49（accepted29/open20）、O12单列1。finding review为closed122/accepted29/open20/O12单列1。20个open均为先前真实差异，T27仍open，未因生命周期提交自行关闭。
- 再补R117-F13：真实公开router apply已进入fake RouterOS首个GET /rest/ip/route且阻塞响应时，另一真实HTTP客户端删除其profile必须409、准确中文、全SQL/files不变；放行401后apply必须502，完整次请求响应和唯一GET一并比较，不能模拟内存job锁或Go内部状态。普通runner在.32合并；Go profile入口仍未接通。新增115项自检验证次响应、设备写入和SQL变化不能被漏掉。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups node-helper-payload-limit,rule-library-native-warning-commit,rule-library-real-HEAD-exception,rule-library-bulk-enabled-save-failure,rule-library-two-source-native-enable-rebuild,tasks-DNS-follow-retry-intent,provider-downloaded-shape-guards,provider-manual-mode-missing-cache-guard,subscriptions-validation,provider-real-stale-cache,generated-download-real-client-disconnect,aggregation-out-of-order-source-completion,aggregation-validation,settings-backup-validation,aggregation-provider-refresh,auth-routes,http-head,http-options-routing,nodes-reorder,subscriptions-reorder --baseline ac1f2377
current compared273 / Go未实现51 / different20 / accepted20 / open0；design_errors=[]；seconds177.50；exit0
.32 --groups profile-delete-real-router-publication-busy --baseline ac1f2377
源真实发布阻塞前提与115项自检通过；Go未实现1；design_errors=[]；exit0
```

合并R117后10115变体、原R4476（S710/F2751/B1015），原R仍差54（含N/A）、H差370；最终最新go全量仍待完成。

## 批次 128：038b基线全部finding复验、跨空间忙碌、健康检查和真实归档限额

- 再次rebase到go 038b52ae；.64实际重跑全部40个finding组，744变体、308.10秒：622compared、72Go未实现、49strict different（29accepted/20open）、O12单列1、design_errors=[]，exit1只因20个已知真实差异。172个review全部checked_baseline=038b52ae，仍closed122/accepted29/open20/O12单列1。变基后的测试修正引用更新为当前可达提交9b421107（原bd7363fd）和5e5dd8ee（原3dc6e7bf），已通过115项自检；不是把业务修复提交改成测试提交。
- R060-F13为两个配置空间各配真实在线心跳和采集记录，再由默认空间启动受fake Agent响应门阻塞的网站识别；另一个空间的合法识别必须409、job=null、准确中文、不泄露首任务ID、没有额外RPC/SQL/files，首任务随后真实完成。7变体全部compared；避免被“另一空间无Agent/无记录”的早期400抢先。
- H240/H241经订阅和聚合POST/PUT四种公开入口驱动：null/数组/数值/字符串/false健康对象及未知字段，28变体全compared，准确中文400、全部行/文件不变、零出站。H344/H345只关联已确认进入node_deletion的缺/错/空ids和不存在节点；非对象body的wrapper错误不计helper。同组29变体实际compared，新加2H标签。
- .32正式前三组64compared、design_errors=[]，13.21秒；累计10150变体、原R4477（S710/F2752/B1015）、H746/G16。
- 归档新增真实H001/H002：规则库名称中的反斜杠、换行、../和/生成不安全本地路径，default/scoped完整MosDNS包500准确中文且零网络/原状态不动。H013使用2048个真实classical来源生成4096下载成员加主文件，触发文件顺序上限；H011使用4096条相邻路由交替规则产生4096个真实inline文件加主文件，触发add上限。未修改4096或256MiB生产常量；H011本批覆盖文件数条件，256MiB总字节条件仍未证明。
- 预设H010的custom_hosts同名冲突实际200，因为builtin tag保留名会改成custom_hosts_2；撤回错误500前提和H010/H016标签，改为G10正向控制，真实ZIP逐字核对两个不同文件。组改名后旧草稿残留曾触发duplicate inventory，改用全新报告路径后14变体source全部通过/Go未实现、design_errors=[]，13.56秒；再由普通runner正式合并。
- 再补7条pull F10：Python真实备份导入接受的non-text custom_config=1，使Loon/Surge/MosDNS GET实际500；default/scoped/filename别名逐条执行，完整SQL/files不变、零源访问。Go相关入口未接通；不用POST的坏JSON冒充GET生成失败。9.64秒有效草稿后正式合并。
- 当前10171变体、原R4484（S710/F2759/B1015）、H750/G16；累计compared2938、Go未实现7083、依赖未实现100、strict different49、O12单列1。原R仍差46（含N/A）、H差362及partial/长流程。新增源前提通过不计Go通过，最终最新go完整all仍未完成。

```text
.64 --groups [上列40个finding组] --baseline 038b52ae：622compared/72TODO/49different/accepted29/open20/O12=1；design_errors=[]；seconds308.10；exit1
.32 --groups discovery-website-foreign-profile-busy,provider-health-shared-write-guards,nodes-deletion --baseline 038b52ae：64compared；design_errors=[]；seconds13.21；exit0
.64 --groups archive-real-name-path-guards,archive-real-download-file-order-limit,archive-real-inline-file-add-limit --baseline 038b52ae：14TODO；design_errors=[]；exit0
.32 --groups config-pull-retained-non-text-settings-failure --baseline 038b52ae：7TODO；design_errors=[]；exit0
115 self-checks + deliberately wrong-Go-response detection OK；有效草稿移至/tmp/configdiff-batch128-evidence。
```

## 批次 129：路由器同步任务的真实排队、暂停回执与重试释放

- R227-S07使用启用自动同步的真实router记录、所选IP规则库/规则引用，以及合成历史runtime baseline（控制摘要对应尚未发布的旧设置）。真正POST任务202后，真实worker先写queued/running，再因未发布控制变化写paused；精确检查完整receipt/历史baseline/rules均保留、当前checked_at、task-status失败计数/原因、全SQL不变及零设备请求。初稿没有选择IP规则，提前命中另一个guard，已纠正并撤回该前提。
- 随后R227-F14重试准确409「同步已暂停：来源、规则选择或网关设置尚未发布，请手动预览发布」；若worker锁未释放，应得到另一条“已有任务运行”提示，不能通过。完整runtime文件、DB/通知/计时继续比较；116项自检逐项改变暂停证明、SQL和设备请求都必须检出。
- 这只覆盖任务准入、真实后台前置检查及持久化，不把合成baseline当成真实设备所有权证明；ROS实际同步写入/回读/补偿仍属于routers领域fake协议和L5实验网部分覆盖。
- .64独立及正式运行2变体，Python前提通过，Go任务入口未实现，design_errors=[]，exit0。统一10173变体、原R4485（S711/F2759/B1015）、H750/G16；Go未实现7085、依赖100，其他计数和findings不变。原R仍差45（含N/A）、H差362及partial/长流程，最终完整all未完成。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups tasks-router-sync-queued-pause-and-retry-guard --baseline 038b52ae
current Go未实现2；design_errors=[]；exit0
116 self-checks + deliberately wrong-Go-response detection OK
```

## 批次 130：真实 managed finish、设备拒绝/漂移和并发路由器编辑

- R211-S12及H1005/1006/1007均经Python真实公开finish走RouterOS driver、inspect和plan；不替换业务函数。仅private native namespace的lo短暂添加192.0.2.240/32，finally移除，宿主没有网络改动。两个fake Agent只提供HTTP状态，不做安装或网络操作；原completion helper默认127.0.0.1路径保持原行为。
- 逐项比较完整只读REST transcript、Basic认证、两条准确Mihomo GET及其头/正文、OxiDNS零请求、规则源零连接、全部DB/files。真实首读401必须502；真实gateway漂移使非空plan必须409；空plan才完成并返回准确客户端路由表。
- 并发项在首个REST读已进入且未返回时，由另一个公开PUT实际提交名称更新，finish必须409。只允许这次PUT的完整单一SQL事务，验证行revision、全局generation、唯一新commit和当前updated_at；额外行/文件仍严格比较。首个草稿漏算正常updated_at/commit变化，已更正并重新实际通过，未把草稿错误报成Go差异。
- .32独立组12变体及正式三组回归全部Python前提通过，Go未接通；118项比较器自检含故意设备写入、额外Agent请求、规则下载、次HTTP拒绝和每项事务证明篡改。draft移至/tmp，不提交临时报告。
- 正式10185变体，原R4486（S712/F2759/B1015）、H753/G16；累计compared2938/Go未实现7095/依赖102/严格差异49/O12单列1。findings仍closed122/accepted29/open20/O12单列1。原R差44（含N/A）、H差359及partial/长流程，最终all尚未完成。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups setup-completion-real-managed-router,setup-completion-lifecycle,setup-completion-attach-router --baseline 038b52ae
Python前提全部通过；Go未实现/依赖未实现；design_errors=[]；exit0
118 self-checks + deliberately wrong-Go-response detection OK
```

## 批次 131：保留基础YAML、长扫描真实断连和OxiDNS后置校验

- 向导H1016–1022共12个具体输入、首次/重复两形态：坏根对象、dns对象/回环、密码、tun类型、完整TUN和Linux网关过滤、1053/53/9199端口冲突。首次prepare确实先初始化内置规则库，完整SQL/files比较保留此副作用；重复拒绝必须全部精确不变。原始“GET已初始化”的草稿前提撤回。
- R190-B04实际60002行规则，在下载HTTP已进入且被门阻塞时shutdown真实浏览器socket，未读取首响应；放行后完整新缓存落盘，再实际扫描得到唯一重复域及准确计数，零第二次下载，配置不变。原先要求初次写入必有.bak的草稿撤回；依据原子写来源，首次只新增主缓存。119项自检逐项篡改断连证明、存储和网络请求均检出。不得把后续扫描200归给断开的首响应。
- OxiDNS default/scoped逐项驱动20个新增H：非法selectors/action/follow、自定义匹配、已有action与新selector冲突、停用parked与follow冲突、default/aggregate/position后置空值和template-preview参数。特意用null越过较早可选类型检查；aggregate配置两条真实相邻同策略规则，自定义执行顺序先具备真实custom match。缺这两个前提会被capability拒绝，早期拒绝的草稿不计后置覆盖。另经完整import驱动device_notes H346–348，含2049条真实备注和IPv4/mapped IPv6重复。
- .64独立及.32正式四组均通过Python前提；正式10267变体、原R4487（S712/F2759/B1016）、H783/G16；累计compared2938/Go未实现7177/依赖102/严格差异49/O12单列1，design_errors=[]。findings仍closed122/accepted29/open20/O12单列1。原R差43（含N/A）、H差329，最终最新go全量未完成。
- 另尝试R154/R220-F10、R222-F12的外部SQLite共享集合错型；实际先触发整体仓库/公开前置故障，得到HTML500，没进入目标JSON错误分支。该草稿代码已删、报告移/tmp、无H/R覆盖计数；不能据此编造路由内异常证据。继续按可达性审计，不注入内部函数异常。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups setup-prepare-retained-base-guards,rule-duplicates-real-browser-disconnect,oxidns-common-late-routing-guards,repository-complete-import-edge-invariants --baseline 038b52ae
Python前提全部通过；Go未实现；design_errors=[]；exit0
119 self-checks + deliberately wrong-Go-response detection OK
```

## 批次 132：真实EasyTier输出、保留listener与legacy导入前置形态

- H342/H343共8变体，真实Mihomo default/scoped生成与预览因名称冲突/无效dialer返回500，完整SQL/files与零出站验证。H254真实组改名触发roundtrip重复键拒绝；H255两个实际preview检查不存在的固定listener策略。原有精确变体复跑后补H289/H319/H969/H970/H439/H462/H224标签；不将Mihomo格式query的前置拒绝当成Surge/Loon正则helper覆盖。
- schema2导入42个输入在实际legacy迁移前覆盖H176–182/H818及H817/H819/H820/H821/H828/H829：7类元数据形态、每个源数组、原始拨号和remap失败。全部真实公开import400、所有原库/文件精确不动、零网络。同步撤回H757/H758的后置覆盖计数：两请求一直只到较早validate_shapes，现标实际H182并关联H853。H838/H856仅给实际repository健康错误调用链加标签。
- .32第一组正式8组489变体（compared39/Go未实现450）、182.58秒，design_errors=[]。后续legacy四组实际复跑正式合并，报告计数以下一行实际JSON为准；120项自检及故意错误Go响应检出。两次CLI选择器拼写错误只报unknown groups，修正后重跑，没有计入覆盖。
- legacy正式196变体（compared28/Go未实现168）、27.66秒。统一10320变体、原R4487（S712/F2759/B1016）、H809/G16；累计compared2938/Go未实现7230/依赖102/严格差异49/O12单列1。findings仍closed122/accepted29/open20/O12单列1。原R差43（含N/A）、H差303（含已明确跨域/防御分支），最新go最终all未完成。
- 明确范围审计：K06中输入校验/现有DNS结构准备/安装ready的一批helper由agents的gateway-onboarding和gateway-installations调用；R211只直接使用_local_upstreams及_prepare_mihomo_dns，latest(public)不执行_saved_snapshot。不能为H944–951、H958–967、H971–986伪造config向导驱动；H341 core_version也只在Agent部署入口传入，普通config生成不传。仍列partial/跨域，runtime未计通过；正在继续逐条审计剩余。

```text
.32 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups generator-easytier-live-topology-rejections,mihomo-base-guards,mihomo-easytier-guards,setup-prepare-retained-base-guards,oxidns-named-native-applies,oxidns-editor-roundtrips,groups-real-retained-and-generated-listener-guards,aggregation-provider-formats --baseline 038b52ae
current compared39 / Go未实现450；design_errors=[]；exit0
.32 --groups repository-legacy-pre-migration-shape-guards,repository-resource-closure-guards,groups-candidate-guards,provider-health-shared-write-guards --baseline 038b52ae
Python前提通过；compared/Go未实现；design_errors=[]；exit0
120 self-checks + deliberately wrong-Go-response detection OK
```

## 批次 133：df6928ae全部finding复验、OxiDNS后置生命周期和图拒绝

- rebase到go df6928ae；behaviour-changes的追加冲突保留集成N7说明和维护者批准的Web listener边界。更新工具自修正提交引用为当前可达e4ced9d6（原9b421107）和3ab6f5b4（原5e5dd8ee），不冒充业务修复。
- .64实际复跑全部40个finding组744变体、582.66秒：compared622/Go未实现72/严格差异49（accepted29/open20）/O12单列1、design_errors=[]，exit1仅因20个原有open。172个review全部checked_baseline=df6928ae，仍closed122/accepted29/open20/separate1。期间服务器过载使总耗时增加，未新增时序差异；已有open也在较早基线实际复现，未因慢机器自行关闭或接受。
- 新增6个定向组正式在.64运行134变体（全部Go未实现、Python前提通过），27.14秒；新组包括18类初始graph/updater拒绝及后续API、legacy-IP、缺URL/链、rename、设备策略和Hosts引用边界。native lifecycle的H437–468每项现都有真实公开输入，未替换任何业务函数。H467必须“删除+新增”才越过纯修复fallback；H468确实从standard_add恢复的暂存参数引入新悬空引用，再由最终图检查拒绝。
- H916真实editor→公开保存保留参数错误→规则DNS写入后移除旧指定上游helper，参数issue坐标发生变化并进入新增issue拒绝；全SQL/files不动。H499的template草稿实际200，因为转换器会重建api；已撤回，改用native common资源扫描后实际到达。每次References诊断要求准确中文加实际坐标，引用路径/tag/reason和所有字段继续跨端严格比；新增121项自检含坏坐标/错误消息/缺诊断的故意变异。
- 正式10449变体，原R4487（S712/F2759/B1016）、H865/G16；累计compared2938/Go未实现7359/依赖102/严格差异49/O12单列1。原R差43（含N/A）、H差247，矩阵新增逐条OxiDNS剩余9H的防御/跨域可达性说明，不把它们计为通过。其他剩余和最终all继续推进。
- 新规则：后续完整Go门禁使用server/scripts/gate.sh的两槽排队（.64加-p 4），高负载单次时序差异先复验。当前新增运行均为定向用例组；未另开全量race/proddiff。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups <全部40个finding组，JSON summary保留精确列表> --baseline df6928ae
current compared622 / Go未实现72 / different49 / accepted29 / open20 / O12单列1；design_errors=[]；seconds582.66；exit1
.64 --groups oxidns-native-common-graph-and-updater-guards,oxidns-retained-selected-source-missing-URL,oxidns-template-policy-and-retained-resource-guards,oxidns-explicit-native-lifecycle-guard-branches,rule-DNS-real-new-native-validation-issue,rule-dns-native-write-boundaries --baseline df6928ae
current Go未实现134；design_errors=[]；seconds27.14；exit0
121 self-checks + deliberately wrong-Go-response detection OK
```

## 批次 134：转发规则实际补下载与导入保留发布记录

- H1001/H1002真实prepare、规则库下载/绑定和公开profile import后移除自有缓存；check实际403，finish分别再次403拒绝、200补下载但要求重新检查。比较真实Agent/来源GET和完整session/SQL；后者仅允许准确600模式的原始规则缓存新增。合成发布回执仅是向导边界前提。
- H830四种legacy集合的null/object/非对象成员、H839共享节点三种形态、H846/H847真实resolved chain错误：停用节点、订阅节点、同节点、混普通来源、跨node/group同名、含链时引用停用普通组；精确关联H184/H257/H260–263。H765关联实际阻塞RouterOS首读期间删除profile的已有请求。
- H764五类保留路由器发布记录分别删除路由器/删回执/换类型/换endpoint，20请求409。Python先生成恢复快照再拒绝；原“文件完全不变”草稿前提撤回。现在要求恰好一份快照且每个逻辑SQL字段等于请求前状态，其他行/文件不变，零设备/源请求；快照路径/内容/模式继续跨端相比。故意破坏SQL、快照、旧文件、缺失/额外/错误路径均检出。
- .64正式五组55变体（Go未实现52/依赖3）、16.35秒，design_errors=[]，123项自检及故意错误Go响应检出。累计10503变体，原R4487（S712/F2759/B1016）、H879/G16；compared2938/Go未实现7410/依赖105/严格差异49/O12单列1。findings仍closed122/accepted29/open20/separate1。剩余原R43、H233含部分覆盖/跨域，最终all尚未完成。

```text
.64 native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups setup-finish-real-forwarded-rule-download-guards,repository-legacy-collections-and-shared-shape-errors,repository-resolved-chain-import-errors,repository-retained-router-publication-guards,profile-delete-real-router-publication-busy --baseline df6928ae
current Go未实现52 / 依赖3；design_errors=[]；seconds16.35；exit0
123 self-checks + deliberately wrong-Go-response detection OK
```
