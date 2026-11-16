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
