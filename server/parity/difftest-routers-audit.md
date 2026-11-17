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
