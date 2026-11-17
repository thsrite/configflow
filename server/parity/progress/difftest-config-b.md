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
