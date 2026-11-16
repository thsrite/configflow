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

继续provider下载、真实HTTP HEAD副作用、单飞/30秒复用/取消，以及其他业务文件。当前并非整个5181个设计用例完成，也未做最终全量。
