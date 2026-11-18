# Routers Step B 可达性审计记录

状态：进行中，等待逐条补齐及独立复核。这里的提议不等于矩阵条目已完成。
只依据 Python、真实公开进程及合成持久数据；未读取 Go 业务实现。

## R002-B02：省略 profile_id 的旧 Agent

提议：把“运行时缺字段回退”与“旧格式导入迁移后读取”分开。不能把缺字段
Agent 直接装入正常 HTTP 成功夹具，再声称验证了运行时回退。

- `backend/common/config_repository.py:2390`：旧格式导入会补 `profile_id=default`。
- `backend/common/config_repository.py:2487`：当前文档要求每个 Agent 的
  `profile_id` 存在于 profiles 中，缺失或空绑定不符合要求。
- `backend/common/config_repository.py:897`：公开进程首次读取存储文档时也执行该校验。
- `backend/routes/client_devices.py:49`：运行时 `agent.get('profile_id') or default`
  回退位于上述存储加载校验之后，不能通过修改导入夹具绕过前置条件。

实际边界证据（测试服务器 .32）：

1. `test_seed.SeedTests.test_missing_agent_profile_does_not_reach_public_http_fixture`
   调用真实 Python 公开 import API；缺字段 Agent 被 `ProfileValidationError` 拒绝，
   提示“绑定的配置不存在”。没有启动 HTTP handler，不计为 HTTP 覆盖。
2. `test_boundary.BoundaryTests.test_stored_legacy_missing_profile_is_rejected_before_handlers`
   先公开导入合法合成库，再停止 Python 公开进程；仅在该独立 DATA_DIR 的 SQLite
   行中删除一个 Agent 的 profile_id，重新执行真实 `python -m backend.app`。
   进程启动失败，日志包含相同校验提示。没有替换仓库、AgentManager 或路由函数。

合法 alpha/beta Agent 的 query 固定空间、响应头保留请求空间、失效 header 优先
返回 404、HEAD 忽略 query agent 等属于独立可达分支，已经另写 HTTP 配方。
旧格式迁移后的 Agent 归属仍需补一份真实公开导入配方；本条保持待审计，不能标完成。

## R012：被前置 blocker 拦下的计划

`backend/routers/plan.py:527` 在未关联网关或其他早期 blocker 成立时返回空 stages、
空 script、空 fingerprint，并且 empty=true。该返回不是可确认发布的有效指纹。
`unlinked_inspection` 明确检查三个空值及 blocker；不能拿十六位 hash 的正常成功
约束套在这个分支上，也不能把 empty=true 当作已同步或可以发布。

## R011-S02：真实 netmask 后缀导致 Python 补偿残留

维护者 2026-11-17 明确要求同时保留两种独立变体。默认设备 fake 继续显示真实
netmask 后缀。公开 Python plan 为无 blocker 的待发布计划；确认发布后返回 502，
`result.failed_stage=routes`，回滚条目 failed，设备仍有新建的 Fake-IP 静态路由。

根因依据：`backend/routers/ikuai.py:639` 的 `_confirm_added` 及 `:692` 的 `_revert_add`
通过 `_matches` 逐字段对比原始 netmask，未剥离显示后缀；`:315` 的 read_state 也会
把带后缀的网段当解析失败略过。`_route_payload` 和 R06 手工编辑器虽会剥后缀，
这些发布确认/撤销路径没有使用同样的规范化。这里只记录，不修改实现。

- 默认变体 `release-ik-suffix`：锁定真实边界及至少 medium 的 Python bug，交 R05。
  Go 同例记录 HTTP 实际结果及失败后剩余新增静态路由数；复制 Python 的补偿残留
  必须被比较器判为 Go 安全性失败，不能因两端一致而通过。
- 独立变体 `release-ik-plain`：仅对本世界关闭后缀，保留 runtime status 等其他字段，
  覆盖静态成功发布及 empty 计划再次确认。默认 fake 没有放宽。
- 当 Go 成功或失败补偿干净、与 Python bug 结果不同，保留全部差异并单独记
  reference-bug-observation，待实际结果复核，不能自动计为差分通过。
- 当前公开 Go 未注册本领域 HTTP 路由，故 R05 实际行为暂时只能记 Go 尚未实现；
  不允许为了补结果直接调用 Go 私有 helper。

### L5 待实机核对（只列计划）

在维护者安排的隔离 iKuai 实验 VM 中记录固件版本、`static_rt show` 原始响应中
netmask 的类型和完整后缀；分别核对读取已有路由和新增后的回读。再受控注入确认
失败，比较发布前后完整设备表，确认新建路由撤销、foreign 路由保留、回滚日志
与设备终态一致。不得用测试 fake 推断固件格式或实际补偿效果；本轮不连接任何
PVE、生产、家庭设备。该项同步列入主矩阵 L5-A 的执行变体表。

## 执行配方与矩阵编号复查

逐项核对成功/失败编号，避免“请求真的执行了”被误算成其他含义的覆盖：

- R001 全局 P1/P2 的原 F08/F09 已被 O6 划掉，实际显式 profile 校验绑定 F13/F14；旧项最终按 superseded 审计。
- R006 同 router 同 MAC 多 IP 是 F15，profile 不符是 F16。
- R023 关闭/无 baseline 是 F12，paused 是 F13，running/queued 是 F14。
- R019 空列表只绑定 B01；S01/S02 另用真实可编辑路由读取证明。
- R020/R021 的 ROS add/update/delete 是 S01–S03，iKuai 是 S04–S06，no-op 是 S07。
- R010 空对象幂等是 B04；B01 另有持久 manifest/id 不被未知字段覆盖的五种真实写入。
- R011 empty 发布是 S16；S04 保留给真正 return protocol2。

所有条目仍处于变体审计中；修正编号不等于宣告整个条目已完成。

### D26 DHCP delete ID wire type

The matrix's D26 shorthand says `del{id:int}`. The checked-in real 4.0.314
capture (backend/testdata/ikuai_fixed_ip_40314.json, request11) and its asserting
Python test backend/test_ikuai_fixed_ip.py:67,84 instead show `{"id":"1"}`.
The public Python adapter also sends a string (ikuai_fixed_ip.py:144). The fake
now requires a positive decimal string for dhcp_static del, compares it to the
integer show ID and actually removes that row. Actual HTTP contract regression
checks integer refusal and string deletion. This is a fixture/matrix correction,
not a finding against either product; other iKuai mutation forms are unchanged.

### H0032/H0033: storage validation can be reached from public fixed-IP apply

These guards cannot be dismissed just because there is no direct binding editor.
A valid public-import fixture with2048 existing bindings plus an ordinary preview/
add reaches the2049 limit only when saving after the device write. A fake DHCP
lease whose otherwise valid RouterOS ID exceeds512 characters likewise converts
and reads back, then binding value validation refuses storage. Both recipes now
pass through real R006→R005:502 generic storage-save message, exact DB unchanged,
new reservation compensated; dynamic undo removes the static lease and warns of
possible renewal/IP change. No private Python/Go helper is invoked. This proves
reachability for the count and ID-length variants only, not all H0033 conjuncts.

## X06 / R025-B90：HEAD 不是 GET 的可靠替身

公开 Python 路由表允许 HEAD，但集合与 forwarding 的 GET/POST 共用函数
仅以 request.method == 'GET' 进入读取分支。真实公开 HTTP 已证明：
HEAD 无 body 均400；合法 body 分别新增路由器（revision +1）、向 Agent POST
持久化 forwarding。故矩阵 O6 中“R025 HEAD 读取每台 Agent 状态”的前提
不成立。保留原矩阵，增加4个独立世界变体锁定实际行为，按两条 Python bug
报告 R15/A20。正常 GET 读取仍单独执行；不能让 fake 重写 HEAD 或虚构 GET。
Go 应执行只读 HEAD；matching reference mutation 是失败，不是兼容性成功。

## X06 / X09：OPTIONS 与路由重叠的真实前置条件

Python 全局 before_request_auth 同样检查 OPTIONS。合成合法空间无 bearer
返回401；已登录返回自动 OPTIONS200。非法/不存在 profile 对非全局领域路由
仍在鉴权前400/404；全局 R001 未登录401，不进入 handler 显式 profile 校验。
`OPTIONS /api/routers/gateway-forwarding` 的 Allow 包含 GET/HEAD/POST/OPTIONS
以及重叠 `/<router_id>` 的 PUT/DELETE。PUT/DELETE 本身先取 job 锁，再返回
路由器不存在404。错误方法/尾斜杠未登录也先401；登录后405/404。
Allow 仅按方法排列顺序归一化，保留大小写、重复、缺失和未知方法。负面比较器
测试证明缺方法、重复、大小写和替换方法仍失败，未忽略整个响应头。

策略查看 HEAD 的设备读取前提是有记录的 manifest resources；空 manifest 不读
任何设备。policy-view 世界用公开导入的6条合成资源证明5张表各实际GET1次；
同名 foreign 行不计 owned。GET 另验证记录顺序、source/destination 角色、外部
改名/改地址、缺失、runtime 字段不改变归属状态、未知配置字段改变归属但不泄露、
fib 空flag显示true、beta隔离、同空间 source_agents 和真实设备失败502。

## H0154–H0161 / H0491–H0492：校验器的真实公开入口

全仓 Python 调用点查找证明：DNS guard 的 normalize_settings 只有
backend/routers/store.py:105 调用；PD 的 normalize_settings 只有同文件:114
调用。H 表的路由范围列漏掉了 R008/R010，不能把该列当作“report/plan 必能
触发该异常”的证明。创建、编辑正常公开 HTTP 可以直接进入这些校验器；本轮
给已有真实错误配方补准确的 H 编号，并增加缺失的条件输入，不调用私有函数。

已分别执行结构错误、三个布尔字段、各类非字符串 bypass 成员、空/空白/中文/
全角/IPv6/错误 prefix 地址、65 项（去重前）上限、DNS 目标缺失、iKuai 不支持
开启、PD 结构/开关/iKuai。共同校验同时覆盖创建和已有 iKuai/RouterOS 的编辑。
H0161 缺 DNS 目标只在 RouterOS 可达：iKuai 已被前面的 H0160 厂商检查拦下。
政策集合的重叠检查同理：iKuai 先返回“不支持按来源策略路由”，另有公开反例
锁定这个前置条件，不伪造一个能走到后面检查的 iKuai 成功夹具。

14 条显式审计覆盖子校验与父级 catch/rethrow；每个声明都列出具体 world:case。
固定输入位置或字段循环变化会让一致性自检失败，不能因为某个 H 有任意一条
400 就自动通过。正常64项、稳定去重、空值恢复默认是独立成功控制；未把这些
设置成功请求当作 PD 事件脚本/真实 DNS 转发的覆盖。

## H0069/H0070：备注加载校验与写入上限须分开

`config_repository.py:2563` 在公开 import/load 时先执行 normalize_notes。因此
非对象或 canonical 地址重复的旧记录不能直接用作正常 HTTP 成功夹具；
`test_seed` 新增真实公开 import 拒绝证据（含 IPv4-mapped 别名重复）。
这些加载前 guard 仍保留待审计，不能仅凭一次 HTTP400 关闭全部变体。

条数上限则存在正常公开入口：导入恰好2048条有效备注，R003 新增第2049条
返回400且 DB/文件/设备不变；现有条目改名成功，删除一条后新增成功。另用
真实 R006→R005 建立 DHCP reservation，再 R003 改名/删备注：改名不改变
OxiDNS 或 binding，删备注仅删本地 binding，不向设备发请求，不删除租约。

只改备注还有直接影响编辑器的分支：先通过 R003 建 direct 策略，再改备注。
OxiDNS/native_config/旧兼容策略名不变；旧 names_revision 的 legacy R015
保存409，未携带 sources 的现代 R015 使用原 revision 仍200且保留新备注。
清空备注写空字符串遮盖旧名，最后移除策略时再删除空备注，无幽灵设备行。

## H0080–H0084：存量策略与备注的加载边界不同

公开 repository import 校验 OxiDNS 外层对象、规则引用与 DNS 动作，不调用
normalize_policies；真实导入后启动普通 Python 进程可到达这些 guard。新增65个
非法策略世界，每个分别用 R002 GET、R003 PUT 和 R014 GET 验证相同的精确400
以及完整 DB/文件/设备不变：外层类型/513条、行类型/未知键、canonical重复、
name类型/81字符/全部C0控制、mode缺失/非法。不是以 helper 调用替代 HTTP。

另有512条成功控制，和旧 DEL 名称/IPv4-mapped/IPv6/CIDR/空备注遮盖的成功控制。
策略旧名中的 DEL 被丢弃，但 normalize_name 拒绝 DEL；两者不能混为一个规则。
只改名称会保存 canonical note，同时保持旧策略行与 native_config 原样。
H0080–H0084 各有完整显式变体声明；没有据此宣称 R016 生成或所有策略图已覆盖。

## store.normalize / manifest 保护的双向控制

23条新增显式审计覆盖 H1234-H1242、H1245/H1248-H1255、H1289-H1293
（已有的 DNS/PD 包装审计不重复计数）。创建/编辑的非对象 body、silent JSON
失败，两个已有厂商的字段类型/循环成员、所有7个合法 interval 和13个非法
形态都实际经过公开 HTTP。iKuai 先在厂商分支拒绝非空 policy selection；
此类反例移除 H1241 附着，不能把它误当作已命中后续 overlap guard。

五类非空 manifest 均拒绝改 type、endpoint，并拒绝删除；注入 id/manifest
字段不能绕过保护。另建五条真正空 manifest 记录，公开改 endpoint/type、
删除全部成功；不靠私有写入清理非空 manifest。空/缺失凭据可公开修复；
已有密码上的6种非字符串值按 Python 保留，全部不向设备发请求。

## T03 缓存新增实际证据（仍等待完整条目审计）

- 同时发送两次 refresh=1，第一笔在真实设备 ARP GET 被 gate 挡住。客户端
  确认两组请求头已通过 TCP，观察两个 future 尚未结束，再释放 gate；仅一组
  设备读取，两端自己的 owner/waiter 响应完全一致。之后独立 refresh 仍重读。
- 公开 import 建129个真实独立 profile，分别核对名称隔离；128条填满后触碰
  alpha，插入第129条淘汰 beta 而保留 alpha，再检查后续 LRU 次序。134条
  请求耗时72.66s；LRU用例不强行要求旧快照仍fresh，stale由独立时序验证。
- 真实等待各端自己的首个请求结束61s：默认读与 refresh=true 均只标 stale，
  无设备 I/O、checked_at不变；refresh=1重读清 stale；正常进程重启后默认读
  再次读取设备。耗时66.29s，不改时钟/不覆写生产超时。

以上是可执行的 T03 变体；本次未将 T03 自动关闭。还须整合既有全失败保好
快照/partial 替换/普通重复读的明确声明，并核对签名变更控制。

T03 现在已完成显式审计：161条具名前提全部由实际公开 HTTP 满足，包含9条
凭据/TLS/no-op 签名控制（保存错误密码后设备真实401；恢复与 TLS 变化重读；
仅profile revision增加不重读）。`audits.py` 中 T03 只代表本条变体完备，
不是整个 R004/helper 子树或 Go 领域的验收通过。详细命令和165条组合输出见
progress；H0030/H0031 的 OS 资源故障仍单列待补。

## Native side / legacy node-test 的公开上下文

23个旁路/旧 node-test 输入分别经 GET、真实 direct 保存、只改备注三个步骤。
包括节点测试 DNS 列表与 custom.servers 中的无效项、直接/不同/缺省53端口、
三个 wildcard 形式与真实 profile 绑定 Agent 记录、UDP/TCP 共享监听、HTTP/TLS/
多entry、非sequence入口、嵌套sequence/fallback、插件外/foreign引用、无forward
和共享forward。H0090–H0095 六条显式审计已完成；旁路未知字段保持，备注编辑
保留整份 native 字节。不据此推断实际 DNS socket/网络行为（仍属 L5）。

首次尝试用主监听 tag=udp 暴露了一个独立 Python bug：引用扫描在 forward
上游 `udp://` 里匹配到 udp，再沿该主监听器的 entry 到 sequence_main，误报
旁路间接进入主链。仅把该 tag 改为 client_dns_listener 就恢复合法旁路编辑。
因此普通 guard 输入使用中性 tag；原 udp 输入独立保留为 low 级参考缺陷，
实际 GET200 editable=false、PUT400 与 Go尚未实现均已进入 findings。没有改
设备 fake，没有删除失败输入，也没有把修复后的 Go 结果自动算作差分通过。

## G23：区分配置预览、Agent 最终配置和向导准备

`check_generation` 的无效标记与 Linux 网关冲突，在配置预览中由通用异常
处理返回500及准确消息。合法预览没有 success 字段，返回 content 等字段。
`require_full_tun_generated` 则由 Agent 最终配置生成调用，缺少完整 TUN 或
带过滤时返回400。不能把仅供预览的普通生成器当成该 guard 的覆盖。

H0110–H0113 的103条实际 HTTP 配方已满足四份显式审计：标记结构/边界、
开关冲突、完整 TUN 必需字段及所有过滤键。完整 TUN 的 Docker Agent 夹具
仅避开无关的原生主机转发查询，没有伪造业务结果；拒绝后无预览记录或设备
写入。Go 公开清单没有注册这两个外部入口，均单独计 Go尚未实现，不增加
26条领域路由分母。细节与首次测试假设修正见 progress。

`full_tun_document` / `_load_base` 不由上述普通预览调用；调用方是
setup_flow 与 gateway_onboarding。后续31条网关安装向导预览已满足 H0102–
H0107 的六份显式审计：非对象 TUN、全部过滤键、非文本基础配置、真正的
YAML 语法/构造器/递归错误、非对象根节点。三个对照通过两种服务的配置
校验，再被“没有代理出口”挡下；不能称为完整安装成功。

另外27条 Linux 网关编辑器 GET 验证 H0108/H0109/H0114/H0115：非对象
文档、旧网关标记、无效 TUN 形态、解析错误均诊断 tun_missing；完整 TUN
无问题，11种过滤键各诊断 tun_filtered。读取保留完整存储与设备状态。
这两批58次真实 HTTP 没有新 Python bug，Go 外部路由均尚未注册。旧版
setup/topology 识别和发布中的早退 guard 仍单列待补，未自动标成不可达。

## R025/R026：版本、布局、传输错误与实际写入结果

169条独立合成 HTTP 配方完成 H0001/H0004/H0005–H0022 与四个 API 父项的
24份显式审计。版本检查前只用真正的空心跳建立在线状态；缺设备、错服务、
Docker、离线、版本错型/格式/数值边界各自进入目标 guard，不能借早期失败
凑覆盖。旧版布局只读后拒绝、AIO 边界成功、新版跳过布局读取分别核对请求数。

设备回复使用真实 HTTP 字节/连接：非法 UTF-8、全部五种重定向不跟随、错误
文本回退与截断、512 KiB 精确边界、真实超时和丢回复。POST 失败分别保留
“设备没写”和“设备已持久化”的输入；随后自己的公开 GET 证明终态，禁止
重试 POST。H0016 的数组/对象不可哈希状态、source.kind、remedy.source
都保留原 Python500 与局部 traceback 证据，属于已记录的同一 medium bug。

H0002/H0003 仍待专门的可达性证明：普通格式错误在 try 之前返回，不能冒充
覆盖 compare_versions 的异常捕获。K01 的本域范围仅 forwarding；apply/remove
的 Agent 端点错误文本没有被这批配方虚假计入。实机转发效果仍属 L5-H。

## H1278：锁 guard 的实际调用方与可比较条件

真正调用此装饰器的是删除、连接设置修改、发布、policy-settings 写入、
探测、手工路由 apply、rule-sync 设置七类方法。R012 plan / R018 report
不因原 H 作用域简写就经过该 guard。每个目标与 vendor 用自己的真实探测
占锁：设备 gate 已到达才发非法 body，409先于 body 校验；释放后 join 原
探测，确认最后探测记录只写一次。另测 alpha/beta 指向同一物理管理地址。

15个隔离世界、30个请求满足显式 H1278 审计。只依赖本目标与占锁的 R017，
不再要求所有兄弟路由先实现。异步前序若提前返回错误，保留真实 Go 结果并
阻断后续，而不是当成 fake 未到达。这里证明 HTTP 的进程内非阻塞竞争，
不声称跨进程 flock、后台 scheduler 或 kill 恢复已经完成，后者仍待 T25。

## 客户端 wrapper 与 Agent pin 的方法差异

H1256 不是整个 R002–R006 都能进入：R002 仅 GET 根据 agent 查询选择绑定
空间，HEAD 忽略；R004 则 GET/HEAD 都保留绑定。R003 PUT 忽略 valid beta
Agent 与不存在 Agent 的 query，两次实际写入只影响 alpha 的本地备注，DNS、
固定 IP、router 及 beta 都保持。缺、空、错服务的读查询与 alpha/beta 正常
绑定分别作为拒绝/成功控制，不能拿非法旧 Agent 绑定绕过仓库加载校验。

H1257–H1260 与 R003-F10–F14 的 shape/profile/normalizer/stale guard 已显式
覆盖，包含21种结构拒绝、11种 profile 不匹配、127种字段错误、两个陈旧值
以及真实新行保存。H1261/H1262 的传递设置错误族仍单列待补，不据这些局部
guard 宣称整个错误 wrapper 已完成。

后续补充已完成本领域 H1261/H1262 的显式传递族审计：每条348份具名前提，
包括请求 normalizer127、旧策略结构/名字/模式/重复65、旧 IP32、native 图/
归属/格式120、三个实际增长失败及真实保存成功对照。所有400都比较完整
存储与设备不变。32种旧 IP 场景还分别通过 R002/R003/R014 三条实际入口。

容量不是只测“坏数据读不进来”：从512条合法策略增至513被拒绝；2,048条
备注再新增时，分别测单独备注和同时修改 DNS 模式。后一种已准备的策略也
必须随备注失败一起撤销，不能留下半份草稿。H0080 现在要求实际增长配方。
这不扩大为共享 C22 所有内部设置 parser、SQL/事务错误或非法旧备注加载的
覆盖；这些边界仍有自己的条目和加载校验依据。

## 固定 IP 外层结构与空间 guard

H0036/H0037 的错误集合分别有29/11项，另有真实单设备和64设备 add/delete
成功对照。错型 devices 特别包含非空字符串和非空对象，不以同时命中“空集”
偷过类型检查。apply 精确 profile_id/token 两个字段；preview/apply 各有11种
profile 不匹配，包含数组/对象、大小写和空白，均在设备访问前原子拒绝。

12项 helper/API 审计已满足，实际正向64台混合两种设备的预览、应用、删除
仍走公开 HTTP。IP/MAC/name 的值校验、setup guard、签名 token、DHCP 状态
和补偿是独立条目，没有因为外层400/409就自动算作完成。

Fixed-IP field normalization (Step B, source-only oracle):
- Read all of `backend/common/client_fixed_ip.py`, including validation, setup,
  opening/planning, signed preview, apply/compensation and enrichment; read
  `backend/routers/lan_clients.py` in full. Added97 literal rejected HTTP vectors
  and12 device-backed positive previews in `fixed_normalization.py`.
- H0038 is not just an arbitrary400:48 independent field values, three duplicate
  IP contexts (including different routers and trimming), and twelve successes (including actual AF hex letter casing).
  H0039 requires canonical and three normalized same-router duplicate MACs plus
  a real same-MAC/different-router success (both devices actually see it).
- Names: every C0/DEL, wrong JSON types,81 ASCII/Unicode/space characters refuse;
  empty, whitespace-only,80 ASCII and80 codepoint Chinese/emoji previews work.
  `normalize_name` remains the source of truth; no copied normalizer computes
  reference values. The public preview token is inspected with its real library.
- H0592/H0593 use the public preview to reach actual IPv4/hex ValueError catches.
  Non-hex MAC segments have valid count/length, so their errors cannot be
  credited to the earlier segment shape check. No tests call private helpers.
- These previews are DB/device read-only, retain foreign reservations, and do
  not claim that apply, compensation, firmware or real DHCP traffic is covered.
  Native route absence still counts as Go 尚未实现, never as a passed comparison.
- Remaining comparison gap discovered while reading apply: ROS ownership
  comments gain a UUID suffix; iKuai adds a random cfip_ tag. A future alias
  must bind each marker to exact writes/readback/owned storage. Generic UUID
  replacement would hide an ownership defect and is explicitly not acceptable.

Cached response-clock provenance:
- Fresh declared response times are checked against each side's real HTTP send /
  response window, then paired only in comparison copies. Cached times now
  explicitly reference one earlier fresh body member of the same route/profile.
  They must equal that side's exact original text; an unrelated ISO-like field,
  changed precision, a refreshed time, a foreign profile/route, an undeclared
  source or a bad original native clock cannot be silently paired.
- Raw observations and Ref inputs remain unchanged. Python provenance is checked
  even when the native handler is absent. Comparator negative controls exercise
  the actual execute path, not just the standalone helper.
- Applied only to known cache lifecycles: discovery refresh=true non-refresh,
  renamed cached notes, credential-signature changes/no-op, actual129-profile
  LRU,61-second stale cache, failed refresh keeping good data, upstream cached
  read and unknown-result replacement. Persisted last_probe/receipts/tasks and
  unrelated response timestamps are still strict and remain separate work.

Fixed-IP setup guard, without the original unit test's monkeypatch:
- Canonical setup session/profile files are accepted by the actual public reader;
  real HTTP create/abandon establishes base fields, ownership permissions and
  why an abandoned session with routers retains its profile. Active/completed/
  abandoned/foreign cases are read from DATA_DIR, not injected profile_sessions.
- The completed record tests the archived flag that this guard reads. It does
  not prove the config domain's installation/check/finish state machine.
- Apply uses an actual own-side preview token from the abandoned state, then
  public PATCH /api/setup/<id> reopens it. Exact409 setup message precedes any
  driver I/O. GET/POST/PATCH setup bodies do not have success: HTTP prerequisite
  assertions use exact id/profile/completed and absence of abandoned instead.
- Native setup prerequisite absence is detected from the real manifest, so the
  dependent apply remains Go 尚未实现 until that public prerequisite exists.
  The static-file preview controls require only their actual fixed-IP route.
- Session updated_at bytes after a real PATCH still need the separate persisted
  clock/content binding work before native parity can be claimed.

Fixed-IP source and recovery:
- Stored routers are only structurally validated as an items list of legacy
  dictionaries; unsupported/missing JSON type fields can reach R006. Both add
  and delete reject409 before driver selection, including array/object types;
  missing/case/whitespace router identities have the same precise guard.
- Source sightings and stored bindings are distinct checks: both vendors now
  have mismatched IP/MAC/empty and known-good sightings, a borrowed router MAC,
  plus stored wrong-router/wrong-MAC binding cases (no I/O).
- Missing local metadata is not always an error: actual owned static rows can
  be recovered by both adapters, previewed, then deleted with the public own
  token. Foreign rows are retained, names/DNS/router unchanged and binding map
  remains empty. No reservation creation or mocked adapter is used.
- Source-unreachable candidates: adapter's unsupported vendor (H0034), _plan's
  supports_write=false (H0041), enrich's supports_write=false (H0061). All real
  calls flow through _router/type filtering and DRIVERS containing only the two
  concrete classes, both class attributes supports_write=True, vendor constant.
  Account permissions fail at HTTP and do not flip that flag. Currently these
  three now have explicit source-proof classification; see the next section.


H0034/H0041/H0061 source-proof ledger:
- The full proof, source locations and real boundary controls are in
  tools/routerdiff/reachability.py. It covers _plan/apply/enrich callsites,
  _router and discovery type filters, open_driver/_driver, DRIVERS and both
  vendor/supports_write constants. Test monkeypatches cannot establish HTTP
  reachability. Device permissions are a distinct actual transport failure.
- All required real controls must complete before the ledger emits
  source-unreachable; otherwise it remains source-proof-controls-pending.
  These are not executed branches: execution_claim=false, no H execution tag
  added to control requests, and source_unreachable_entries counted separately
  from audited_entries. No matrix row or denominator is removed.
- Future changes to dispatch/driver capabilities require rechecking this proof.
  Source exclusions do not waive ordinary read/write permission or fixed-IP
  success/refusal/rollback cases and never establish native parity.

R001-F08/F09 O6 supersession:
- The approved row explicitly strikes these global P1/P2 entries out and points
  to F14/F13 instead. They now have review-superseded evidence (distinct from
  source-unreachable or executed coverage). Both missing/invalid header and
  profile_id return200, explicit profile errors remain404/400, and linked
  alpha/beta output with invalid header proves real scope rather than only an
  empty result. All named controls are required, execution_claim=false.
- review_superseded_entries stays separate; no row is removed, no native result
  changes classification because of this proof. Missing/bad controls remain
  pending, and every actual implemented HTTP request is still compared strictly.

H0002/H0003 _at_least outer catch:
- Built-in JSON string plus ASCII dotted regex; minimum arguments are three
  valid literal constants. compare_versions's only nontrivial conversion catches
  its own ValueError, including Python's integer-digit guard, and returns int.
  Outer TypeError/AttributeError cannot arise from the guarded string operations.
- Author source proof now in reachability.py with48 named real public controls,
  including a5000-digit zero-major below-minimum input. It is not an executed
  catch, and its H IDs are not attached to those HTTP request execution tags.
  Any ordinary invalid/old Agent version still has its exact real refusal test.
- These classifications are reviewable Step B author evidence; no Go package is
  marked verified and no Python exception is simulated or added to Go.

Persisted probe-clock/public-view boundary:
- R017 creates one probe dict, persists it on its selected router and returns it.
  A fresh response time is only pairable after exact own DB equality, own
  profile/router identity and actual HTTP-window validation. Invalid native
  evidence becomes a contract difference. Reference checks run even without Go.
- Compare copies project only proven routers.items[].last_probe.at and the same
  public_view slots in R007/R008/R010 and R014/R015 workspace.router. The Python
  workspace file was read fully; only these resource ID slots were added to
  generated-RouterIDs projection. source_agents IDs, arbitrary strings, raw
  files, messages, other clocks, hashes/revisions/fingerprints and raw Refs remain
  untouched. Known raw source must still match; external replacement stays strict.
- Real10-request readback/editor sequence plus17 fresh-probe/edit worlds prove
  each connection member clears last_probe, normalization/no-op/ignored passwords
  preserve it, and name edits preserve probe while invalidating the editor.
  Publication invalidation (remaining R015-B04 part) is not inferred.
- Existing104 probe protocol/TLS/IPv6 requests validate both success/failure
  persistence. Comparator negatives cover wrong/duplicate owner, stale/missing
  time, foreign profile, raw-file/message corruption and actual execute/list/
  workspace flow. Purpose-broken observations are oracle tests, not product bugs.

Native main-request failure classification:
- Actual worker exception class and actual child running/exit state establish a
  transport observation; never classify by a substring of exception prose.
  Only concrete request/join connection/read/protocol failures qualify.
  Unknown RPC/fixture errors and Python reference failures stay fixture failures.
- No complete HTTP observation means go=null plus typed transport details,
  different and blocked subsequent dependencies. No manufactured500/empty JSON,
  Go-unimplemented fallback or false parity. Immediate state evidence explicitly
  cannot prove quiescence, completed work or compensation, even after child exit.
- Real own-namespace -9 negative control uses an actually registered auth route;
  it is not a router case or product finding. Main HTTP polls/joins are covered;
  auxiliary setup and post-response settling failures are now also retained,
  with a completed main response kept separately and later dependencies blocked.

Probe fatal errors versus capability downgrades:
- Every capability loop member has actual peer refusal/HTTP/non-JSON/disconnect/
  timeout, plus ROS root/row guards and later null after valid row. Child errors
  keep200/success and only remove that capability; fatal resource/auth/version
  errors return502 and write failed probe metadata. All probes read device state.
- Seven valid version/model controls, eight resource-root shapes and every JSON
  version-field kind distinguish source guards and preserve Chinese/emoji.
  TLS min/error controls and exact owned clocks/one revision protect persistence.
- H1296's110 required observations do not claim DB-save failures, non-RouterError
  crashes, all nested schemas, true permission acceptance or T15 completeness.
  R017-F12 remains a separate open audit. Device password echo remains the known
  X01/O9 policy observation; no message is masked or silently reclassified.
- elapsed_min now checks native too, using real monotonic request duration and
  original async start. It never compares arbitrary RTT equality. Timing-only
  differences still require lower-load/other-host recheck; slow in-budget and
  upper-bound device tests remain open. All new source controls ran on real
  Python; purpose-broken oracle tests are separate from product findings.

DHCP evidence guard increment (author audit, not native parity):
- Read RouterOS read_dhcp_dns, _dhcp_names/_dhcp_network_attributes/_dhcp_overrides,
  shared build_dns_report/matching_networks and the actual _inspect/plan/apply
  boundary.22 explicit H audits require named report/plan/confirmation variants.
- Server absence/disabled, missing identity/invalid flags/RADIUS, network IDs/
  address parsing/dynamic flags, malformed DNS and dns-none are separate inputs.
  Every names invocation position has wrong JSON kinds plus actual accepted lists.
  Each option6 layer and missing/ambiguous/incomplete set/option references is
  exercised; integer/string/bool/range/vendor-specific/5000-digit option codes
  are literal device evidence, never an application-helper substitute.
- Unknown lease scope is conservative. Outside/disabled lease, disabled server,
  ordinary dynamic renewal and disabled matcher are independent healthy controls.
  Report200 retains exact unknown diagnostic, plan200 retains exact blocker,
  apply409 refuses without a fingerprint and without changing device/storage.
  These evidence-only guards do not prove actual firmware DNS acceptance.
- Four abstract RouterDriver defaults (H0128–H0131) are source-unreachable:
  all application construction uses the two-class _driver map, both concrete
  classes override each method and never call super/base. Device error responses
  cannot change method dispatch. No production base instance/map/method mutation
  exists. Real both-vendor probe/read/selected-DHCP/successful-write controls are
  required by reachability.py; no control is tagged as executing a base error.
  This is separate source proof, not a test pass or package verification.

Untrusted nested probe schema / X12 increment:
- Actual iKuai sysstat code0/results object with truthy nonobject verinfo raises
  uncaught AttributeError before version validation. Five JSON kinds were proven
  by actual local-window Python traceback and HTML500, not exception emulation.
  Database/device state remains unchanged; each has a subsequent healthy probe.
  Six falsy values instead follow the ordinary502 version error and persist it.
- New low reference bug belongs to R05/R15. Missing Go R017 stays unimplemented;
  no native outcome is invented. Safe native structured rejection is separate
  O12 review with all differences retained, never a parity pass. Matching HTML
  failure/success-looking reply/device mutation/write intent is a safety failure.
- A Go fix may leave DB untouched or add only its own fresh failed last_probe,
  one profile revision/generation and a new commit ID. Foreign owners, other
  profile fields, stable meta, schema and files cannot change. Purpose-broken
  oracle tests check stale clock, corrupted router/settings, duplicate/bad rows,
  wrong store/generation/commit and foreign DB/file/device-write changes.
- This does not complete broad R017-F12 or X12. Other nested error schemas,
  Unicode credential transport and publication-time uncaught errors remain open.

Store commit identity comparator:
- Real normal Go POST /api/server-domain matched Python HTTP and stored config,
  with the sole difference at meta.commit_id. These are independently generated
  identities, not config content. Oracle control is outside routers coverage.
- StoreHeads binds only actual meta.commit_id after own store/generation/config
  provenance checks and equality of the complete projected database excluding
  that one ID. Config-table digest includes physical field revisions/positions,
  sections and root_order. Only independently validated router/probe projections
  precede candidate equality; no generic UUID/clock/string mask is introduced.
- Each side records raw content separately from comparison copies. Reusing a
  token for another generation/content, changing a known generation's token or
  content between requests, rewinding generation or changing store identity is
  a contract difference. Raw observations, files, arbitrary ID-looking fields,
  stable metadata, actual generations and revisions remain strict and intact.
- Python config_store permits metadata-only commits without changing head:
  raw config provenance excludes unrelated metadata from its digest, while those
  metadata rows stay fully compared. Legacy absent/null/empty commit IDs remain
  literal, never aliased; a subsequent real config commit needs a fresh token.
- Every real Pair enables this check, including Python-only requests while Go
  routers are absent. Real two-cycle native config write/read controls pass.
  Rotating only the native token in its OWN stopped disposable DB, then restarting
  and returning normal HTTP200, is detected as store-head corruption. This
  deliberate oracle fault is not a native finding or matrix recipe.

Router draft lifecycle and protected-field route audits:
-5 independent profile-selection worlds delete each vendor's draft record and
  verify a repeat404, exact remaining/empty lists and the protected same-ID
  foreign profile. Every mutation preserves full device tables and sends zero
  device HTTP; names/DNS/fixed bindings and foreign SQL stay unchanged. The
  target with5 empty manifests deletes, while a foreign active manifest stays.
- R009-F13–F17 each require their actual active refusal and separate empty
  deletion control. R010-F14 requires both endpoint/type edits for all5 active
  and empty manifest kinds; S04 covers all10 actual empty-manifest changes.
- R010-B01 now requires all5 fields with null/object/list input, plus forged id,
  probe/release and unknown values. Fifteen accepted real writes preserve the
  original id/manifest and prove absent forged metadata in actual SQL.
- Busy mutation route audits use real public probes inside entered device gates;
  malformed bodies get409 first, and each probe finishes after gate release.
  No elapsed-sleep surrogate/private lock is used. Cross-profile shared endpoint
  is an extra R010 control, not proof of waiting-lock/repository races. R009 F12/
  F18/B04 and R010 F15/B06 remain open.

Client-device OxiDNS revision comparison:
- The maintainer-authorized S-oxidns change uses a Go-native canonical hash, so
  R002 GET/R003 PUT hashes cannot be compared as literal Python encodings.
  PolicyRevisions validates SHA256 text shape and the observed deterministic,
  collision-free relation with the OWN effective profile's complete stored
  oxidns settings. It never computes either returned hash or calls a helper.
- Unchanged settings must retain a token; different observed settings cannot
  reuse it. Names/bindings/router/probe/profile counters are not dependencies.
  Own-input provenance preserves numeric representation distinctions, including
  signed zero. Pinned Agent responses use their actual body profile, which can
  differ from the selected request/header scope.
- Pairing requires equal settings AND the entire response except that exact
  oxidns_revision member, after independent clock/resource projections. Other
  response/storage differences are retained. Raw responses/own Ref values never
  change. Invalid/stale/reused native tokens are contract differences; bad
  reference invariants stay fixture errors even when Go is absent.
- HEAD/OPTIONS have no observable body revision and are not token-binding
  events; their actual header/body/storage checks remain in the normal runner.
  Initial HEAD fixture assumption was corrected, not added as a product finding.
- Narrow scope: workspace revision/names_revision/oxidns_revision, plan
  fingerprints and signed fixed-IP previews remain later work. This client
  binding adds no matrix coverage or native domain parity claim by itself.

Workspace revision extension (R014 GET/R015 PUT):
- Bind three explicit members together: workspace.oxidns_revision to full stored
  settings (same token namespace as R002/R003), workspace.revision to full stored
  router excluding ONLY last_probe plus those settings, workspace.names_revision
  to actual ordered public policy IP/name rows. Do not mirror a hash algorithm.
- Own raw dependency history remains separate from resource/clock comparison
  copies. A generated router ID must first be established by RouterIDs in both
  storage/public views; then workspace scope equality can use that alias. Probe
  metadata is excluded by source, while every other private router member stays
  a dependency. The independent probe-clock/storage contract remains enforced.
- All sibling tokens must validate before aliasing; a different valid native
  hash cannot block comparison of the other two, nor can one hash hide wrong
  settings/router/names/profile/content. Reference checks stay enabled without
  native handlers. Only body-bearing actual GET/PUT methods create bindings.
- Integrated execute oracle writes both real own-side raw optimistic tokens
  back to its same side; projection strings never reach a request. Deliberately
  changed tokens on unchanged inputs, malformed/missing fields, wrong owner,
  corrupted private setting and names fail. Actual reference probe/name/router
  edit workflows validate intended invalidation relations.
- These controlled workflows use settled storage for response provenance.
  Deliberately overlapping config writers still need explicit response/storage
  linearization evidence before T02/T08 can be closed; no universal concurrent
  snapshot claim is made. Signed previews, plan fingerprints, YAML semantics,
  runtime publication/receipt clocks and ownership nonces remain pending.

iKuai complete DHCP evidence / shared scope guards:
- Actual637-request run covers20 new explicit audits H0247–H0263 and H0147–H0149.
  Complete firmware4.x forms use43 known fields and2 recognized runtime fields;
  every full-form field is missing/null independently and every17 integer field
  rejects bool. Extra wrong kinds and ordered/combined diagnostics remain visible.
- Enabled/runtime status (including bool-vs-int1), relay and physical interface
  restrictions are distinct. Both start/end parsers, each explicit pool disjunct,
  invalid masks/gateways, singleton and prefix-mask controls are actual device
  replies. Invalid networks preserve their precise blockers through shared scope.
- Both DNS slots have valid/invalid/empty/duplicate/inherited/explicit targets;
  two bad slots deduplicate their diagnostic. Static bindings cover every early
  address type/syntax guard, each reachable missing/type field, independent DNS,
  unknown metadata and allowed runtime fields. Disabled/outside rows do not
  block the target LAN. Static id type is intentionally only a reader control;
  no assertion says this malformed id is accepted by the fixed-IP writer.
- Shared matching sees actual missing/duplicate enabled IDs and unknown subnet
  strings. RouterOS rejects malformed IDs/subnets before shared matching; iKuai
  can emit an empty network and reach real ValueError. No private model/catch
  simulation or fake exception is used to fabricate an alternative TypeError.
- All report200/plan200/confirmation409 messages and full DB/device invariants
  passed. No DNS delivery/write/readback/compensation or firmware acceptance is
  inferred. Default static_rt netmask display suffix remains enabled unchanged.

DHCP write evidence extension (Step B): H0264/H0265/H0268/H0273–H0279 now
require the explicitly named public release worlds in audits.py and an actual
successful full-form DHCP edit. Peer-gated external domain drift refuses without
DHCP writes; full readback and clean/failed compensation retain real device state.
A forged private operation is never submitted. Plain-mask controls are separately
named; the default realistic static-mask suffix and its known Python bug remain.
These10 entries have39 actual synthetic HTTP observations in the focused .64 run;
native routers registration remains absent, so they do not claim Go parity.
