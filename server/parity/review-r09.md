# R09 HR 复核（强制 DNS / DNS guard、IPv4 同口转发提示）

复核者：新的 Opus 会话（未参与移植；R09 由 Codex 移植）。分支 `go-opus-review-r09`，
从 `go-port-r09@5eddbd69` 分出，基底 `go@895f5e21`。只用 loopback fake 和合成数据，
未接触生产、真实路由器、DNS 或 PVE；未 push，未开子 agent。

范围：完整读了 `backend/routers/dns_guard.py`（589）、`forward_hint.py`（184）、
`ipv6_policy.py` 的求值器部分，以及调用方 `report.py`、`release.py`、`routes/routers.py`
的发布与 manifest 保存路径；对照 Go 的 `dns_guard_{model,spec,plan,write,messages}.go`、
`forward_hint{,_messages}.go`、`ipv6_policy_eval.go`，以及被复用的共享驱动
`perform`/`verifyChange`/`checkWritten`/`ownershipSnapshot`/`stableROSRow`/`comparable`、
R08 的 `Manifests()`。PR #572 语义（只增删有变化的对象、预览逐行说明）按 Python 源码核对。

## 结论

**0 条 high，1 条 medium（已修），5 条 low（1 条为 Netwatch 归属裁定，其余记录在案、列入 L5 或交给 R10）。**
未达到「≥ 2 条 high」的回退门槛。最终门禁在 `.64` 通过（见文末），R09 标记为 `verified`；L5 仍待做。

## 发现

### 中

- **F1 — 内部回滚只完成一部分时，已恢复的对象失去归属（Python 原有 bug，已修）。**
  场景：增量发布删除旧对象后创建新对象失败，回滚把部分旧对象重新创建（新 ID），但另一步补偿
  也失败。`ApplyDNSGuard` 只在回滚**全部**成功时才调用 `remapDNSGuard`，于是 manifest 仍指向
  已删除的旧 ID；已恢复的放行地址、劫持规则、Netwatch 带着保留的列表名 / `cf-dnsguard:hijack`
  注释却没有归属证明。R12 照常保存这份 manifest（Python `routes/routers.py` 发布后无条件保存
  `driver.dns_guard_manifest`），之后每次预览都被 ConfigFlow 自己的对象阻断（「地址列表
  「cf-dnsguard-bypass」已被其他配置使用」，以及移植者新增的同注释 NAT 阻断），只能到路由器上
  手工清理；已恢复的 Netwatch 变成「外部对象」后仍按 comment 启停劫持。
  证据：`TestR09ReviewPartialRollbackKeepsRestoredObjectsOwned`——放行地址从 .50 改为 .60，
  新 Netwatch 与补偿重建的旧 Netwatch 都被拒绝。修复前在 `.64`：
  `r09_review_test.go:70: restored member still recorded under its deleted ID *67`。
  修复：回滚失败但已有成功恢复的对象时，同样按 path+ID 重映射这些对象（重新回读其配置），
  恢复失败的对象仍按原记录保留；重映射本身失败时把两个错误合并为回滚错误；`remapDNSGuard`
  在提交前检查 `ctx.Err()`，关机后不提交。测试同时验证下一次预览（新设置与原设置）无阻断、
  可以继续发布。提交 `b3e93b5c`。
  未改 `RestoreDNSGuard`（发布后其他阶段失败时的整体回滚）：那时 driver 上已是新 manifest，
  它拥有本次新建的对象；部分失败时无论保留新 manifest 还是换成重映射后的旧 manifest，都会丢掉
  一部分对象的归属，两份 manifest 合并又会出现同一 role 两个对象。保持 Python 行为（保留新
  manifest），在下文「部分成功后的状态」说明。

### 低

- **F2 — Netwatch 运行时脚本按 comment 选择 NAT（必查项裁定，见下一节）。** 移植者补的发布前
  阻断判定为充分；它同时就是每次检查时的持续检测。剩余风险只有「发布之后才出现的同注释外部
  规则，在下一次检查前会被 Netwatch 启停」，判为低。三个候选方案都不采用。没有改脚本；新增
  `TestR09ReviewPostPublishCopyIsDetected`、`TestR09ReviewRestoredHijacksAreNotForeign`
  固定现有行为（提交 `b3e93b5c`）。

- **F3 — 暂停期间重建劫持规则，新规则以启用状态创建，DNS 被送往无应答的 OxiDNS（Python 同）。**
  场景：OxiDNS 停止应答，Netwatch 已把劫持对暂停（`disabled=true`）。此时发布一次只改劫持规则、
  不改 Netwatch 的变更（改 LAN 接口，或被外部 53 规则压到下面后需要重新置顶）：增量计划保留
  Netwatch，删除旧劫持、以 `disabled=false` 新建劫持。Netwatch 的状态仍是 down，不会再次触发
  down-script，新劫持一直生效，局域网中硬编码 DNS 的设备查询全部送到无应答的 OxiDNS，直到 OxiDNS
  恢复。证据：`DNSGuardResources` 固定 `disabled=false`；`BuildDNSGuard` 只按值比较保留
  Netwatch，与当前暂停状态无关。未修：可行修法是「需要重建劫持且 failover 开启时，Netwatch 也一并
  重建」（预览会如实列出 Netwatch 替换），但它依赖 RouterOS「新建 Netwatch 首次判定 down 时会
  执行 down-script」，这必须在 CHR 上确认，不能凭 fake 改变真实设备行为。已加入 L5-C。

- **F4 — 发布进行中 Netwatch 恰好暂停新建的劫持，会被当成外部修改（Python 同）。**
  down-script 在 down 后延迟 15 秒执行 `disable [find comment=...]`，若正好落在发布的几秒内：
  新劫持的 `verifyChange`（共享 `checkWritten` 比较 `disabled`）或最终回读（比较完整
  `ownershipSnapshot`）失败 → 回滚；补偿 DELETE 的 expected 也是 `disabled=false`，被判为
  「操作期间被修改」→ 回滚不完整，留下带保留注释的孤儿劫持，下一次预览被阻断。触发需要 down
  事件与发布在 15 秒窗口内重合，概率低；正确修法要让共享 `perform`/`checkWritten` 理解「受管
  暂停字段」，属于共享驱动路径（第三条常设检查），收益不足以在 R09 改。记录，加入 L5-C。

- **F5 — 共享求值器 `ipv6_policy_eval.go` 的接口只够 R09 用，R10 必须先扩展。**
  现状：`PolicySnapshot` 只有一张 `Filter`，`EvaluatePolicyChain(s, chain, p) string` 写死 filter 表、
  只返回结论。Python `evaluate_chain(snapshot, family, table, chain, packet)` 被 R10 的
  `policy_checks` 用于 raw/mangle/nat/filter 四张表、两个地址族，返回 `decision`（含 nat 表的
  `translate` 和 `translation`）与中文 `evidence`，并区分「表未读到」（`_errors`）。建议 R10：
  `PolicySnapshot` 改为按表保存（nil = 未读到，空表 = 已读且为空，沿用现有约定），新增
  `EvaluatePolicyChain(s, table, chain, p) PolicyDecision{Decision, Evidence, Translation}`，
  evidence 的中文文案放进 messages 常量；`MatchPolicyRule` 的三值语义、预算（分支 256、跳转 12、
  接口列表 256/12）保持不变。唯一调用方是 `forward_hint.go` 一处和 R09 测试，改签名代价小。
  R09 本身的结论与 Python 一致（真实 Python 三值对照已覆盖），不需要现在改。另：IPv6 地址列表
  按名读取用 `url.QueryEscape`（空格编码为 `+`），Python 用 `quote(safe='')`（`%20`）；R09 只走
  IPv4 路径，R10 接手 IPv6 时需要用原始请求字节核对。

- **F6 — 关机发生在发布中途：补偿同样无法执行，留下部分写入（设计取舍，所有路由器写入共有）。**
  `lifecycle.Detach` 保留关机取消，第四条常设检查要求关机后不再落写；补偿也是写入，且共享
  `request` 使用同一 ctx，因此关机打断的发布不会回滚，新建对象未登记归属。这与 R04–R08 一致，
  不是 R09 引入的；记录给 O14 切换计划（关机前等待发布结束，或发布后检查「强制 DNS」是否被阻断）。

## Netwatch 运行时脚本归属：裁定

现状：up/down 脚本执行 `/ip firewall nat enable|disable [find comment="cf-dnsguard:hijack"]`，
down 脚本另用 `[/tool netwatch get [find name="cf-dnsguard"] status]` 二次确认；劫持和 DoT 规则用
`src-address-list=!cf-dnsguard-bypass`。也就是说运行时有三个**按名字选择**的入口：劫持注释、
Netwatch 名、放行地址列表名。ConfigFlow 的归属证明（ID + 完整配置）只在 API 写入时生效，运行时
脚本不认 manifest。

### 1. 发布前阻断是否充分

移植者的阻断（`guardForeignHijack`）在 `BuildDNSGuard` 和 `ApplyDNSGuard` 写前快照里，对**任何**
未被 manifest 证明的、注释等于 `cf-dnsguard:hijack` 的 NAT 行生效（不限 chain、端口、动作、
disabled），failover 关闭时也生效；撤销（spec 为 nil）不受阻断。放行列表名和 Netwatch 名的
外部占用在 Python 中已经是 blocker，Go 写前再查一次。逐个场景核对：

| 场景 | 结果 |
|---|---|
| disabled 的旧 53 规则带同注释 | 阻断（不看 disabled）。移植者测试 `TestR09ForeignHijackCommentBlocksPublish` 覆盖 4 种组合，并对照真实 Python 证明原来放行。 |
| 预览之后、发布之前插入 | 写前快照再查，零写入拒绝（`TestR09LateForeignObjectsAreNeverClaimed/hijack_comment_collision`）。发布时 R12 还会重建计划并比对 fingerprint。 |
| 发布之后外部复制规则（WinBox 复制保留注释）或把外部规则改成同注释 | 运行时会被启停，直到下一次检查；下一次检查（预览、同步状态）立即变为 unknown + 中文说明，零写入；撤销仍可执行并删除 Netwatch。新增 `TestR09ReviewPostPublishCopyIsDetected` 固定这一点（enabled/disabled 两种）。 |
| 发布之后外部改写**受管**劫持 | `guardOwned` 按完整配置判为外部修改，预览报错保留现场；运行时它仍是我们的规则，被启停符合预期。 |
| 受管劫持被手工删除后重建（含 WinBox undo） | 删除的 ID 不再被证明 → 重建的同注释规则是外部对象 → 阻断，提示改注释。保守但正确：无法证明它就是原规则。 |
| 回滚成功 | 恢复的劫持新 ID 进入重映射后的 manifest，不会被当成外部（新增 `TestR09ReviewRestoredHijacksAreNotForeign`；边界用例 `disable_reject_address` 对照 Python 也覆盖）。 |
| 回滚部分失败 | **原来有漏洞**：已恢复的对象不再登记，被自己的阻断挡住 → F1 已修。仍未登记的只有「新建后补偿删除失败」的对象，这本身是需要人工核对的回滚错误。 |
| ID 复用 | 证明要求 ID **和**完整配置都一致；配置不同按外部修改处理。配置完全相同的复用对象在功能上等同受管规则，被启停无害。RouterOS 正常不复用 `.id`，L5 补测重启后 ID 是否保持。 |
| failover 关闭 | 没有 Netwatch 也阻断：过度保守，但避免以后开启 failover 时继承冲突；可接受，已在 O12 列为行为变化。 |
| 同名 Netwatch / 放行列表外部成员（同一类按名选择） | 发布前 blocker，写前再拒绝；发布后出现的同样在下一次检查时成为 blocker。外部同名 Netwatch 会让 down 脚本的 `get [find name=...]` 出错而不暂停，属于同一残余窗口。 |
| 已由 Python 发布、路由器上已有同注释外部规则 | 升级后第一次检查即变为 unknown 并说明原因。这是用户可见的行为变化，列入 O12 台账（原来显示「一致」）。 |

结论：**对 ConfigFlow 能控制的写入路径，阻断是充分的**；它在每次检查都会重新执行，因此也是持续检测。
剩下的窗口是「发布之后、下一次检查之前，有人新建了带保留注释 / 名称的对象」，这类对象只能由用户
复制受管规则或手工使用保留名称产生，下一次检查即被指出。判为 low，不再加代码。

### 2. 三个方案的推荐：都不采用，维持 comment 选择器 + 现有阻断

- **方案一（脚本按受管 ID 绑定）——不推荐。** 劫持 ID 在建 Netwatch 时其实已知（Netwatch 最后创建），
  但：①劫持一旦重建，Netwatch 必须一起重建，预览里 Netwatch 的脚本值无法事先写出，`guardValidateOp`
  的「创建值必须等于由 spec 推出的值」会被打破；②回滚按 journal 恢复旧 Netwatch，其脚本里是旧劫持
  ID，而恢复出来的劫持是新 ID → Netwatch 静默地什么都不控制，OxiDNS 故障时不再暂停，整个局域网
  DNS 中断。这是把「可能误启停一条带保留注释的外部规则」换成「可能静默失去故障保护」，失败方向更糟；
  ③若 RouterOS 在重启等情况下变更 `.id`，manifest 只会让预览报错（fail closed），ID 绑定的脚本却
  会静默失效（fail open）。迁移：所有已部署路由器第一次发布时 Netwatch 替换一次。
- **方案三（每次发布后取得 ID 再改写脚本）——不推荐。** 共享 `perform` 已支持带 journal 的 PATCH，
  可以实现，但继承方案一的全部 ID 问题；回滚时还必须在重映射之后再改写一次脚本，这一步失败同样是
  静默失去保护；无变化发布也要碰 Netwatch，破坏 #572「只动有变化的对象」。迁移同方案一。
- **方案二（comment 加标识）——三者中风险最小，但收益几乎为零。** 规则和脚本带同一标识，journal
  恢复时两者一起恢复，不受 ID 变化、重启影响。但现实中的外部同注释规则来自 WinBox 复制（标识
  一并复制）或遗留的旧发布（已被名称冲突挡住），标识区分不了；随机标识还会让预览无法确定。迁移：
  所有已部署路由器第一次发布时劫持对 + Netwatch 全部替换，有短暂无劫持窗口；若当时处于暂停，
  还会触发 F3。为一个已被检测覆盖的低风险窗口付出全量迁移，不划算。
- **如果以后一定要加强运行时隔离**（例如维护者要求外部复制的规则绝不被启停），顺序是方案二
  （标识由劫持规则的全部配置推导，确定且可预览）优先，并且必须先在 CHR 上验证；方案一、三不考虑。

### 3. 代码修改

本轮只修 F1（它直接破坏了阻断的前提：ConfigFlow 恢复的对象被当成外部对象），并补两条测试固定
「发布后复制会被检测」「回滚恢复的劫持仍属 ConfigFlow」。生成的脚本文本与 Python 保持一致，
已部署路由器零迁移。

## 其余重点核对（未发现问题）

- **增量 diff。** 保留判定 = 受管（ID + 完整配置证明）且值与新 spec 一致，劫持另要求前面没有其他
  启用的 53 dst-nat；failover 时劫持的 `disabled` 不参与比较，暂停的劫持保持原 ID 和暂停状态
  （`TestR09IncrementalKeepsPausedIDs` 确认 NAT/filter 零写入）。删除集合 = 受管 − 保留，按创建
  逆序；创建集合 = spec 推出的对象 − 保留，按安全顺序（放行、hairpin、劫持置顶、DoT 置顶、Netwatch）。
  回滚 remap 按 path+ID（修复 Python 跨表 ID 混淆），`place-before` 只在同表内重映射。晚碰撞（预览后
  新增保留名、同注释、更靠前的 53 规则、受管对象移走/复制/改动）都在写前拒绝；删除前再次读取并
  按记录的证明比较，不再用新读到的行充当 expected（修复 Python 吸收中途修改）。
- **预览与执行一致。** 执行前 R12 按新读取重建计划并比对 fingerprint（Python `routes/routers.py`
  同样如此）；`ApplyDNSGuard` 另外要求实时受管列表与 `op.Owned` 逐项一致、`op.Create` 与
  `spec` 推出值减去 keep 后**完全相等**、keep 是新 roles 的子集、保留对象仍满足保留条件。因此执行
  删除的正是预览中 pending/remove 的对象，创建的正是 pending/add 的对象，`synced` 的对象零写入。
  `op.Delete` 只用于展示，执行时由 Owned − Keep 推出，二者同源。
- **回滚与部分成功。** 每个破坏性请求前写 journal；4xx 拒绝不产生对象，传输错误/5xx 的 PUT 保留
  现场并报回滚错误；丢失 DELETE 回复会实际补偿；ID 被复用或被改过的对象不删除。部分成功后的状态：
  内部回滚失败时 manifest = 原记录 + 已恢复对象的新 ID（F1 后）；外部 `dns_guard_restore` 失败时
  保持新 manifest（与 Python 一致，原因见 F1）；两种情况都返回回滚错误，R12 显示「回滚未完成」。
- **R08 原生接入。** `Manifests()` 同时返回 Policy 与 DNSGuard（含显式 null 的 Set 标志），返回副本；
  `natlist_read.go`、`routeros.go` 的 NAT 评估按 DNS guard manifest 识别受管 hairpin；ReportParts
  链式保留前驱；ForwardHint 直接用 `ROSPolicyPlan.Items`，优先 R08 的 filter 快照，空表视为证据、
  缺失才重新读取；DNS guard 冲突检查直接读 `Desired.ROSPolicy.CIDRs`。没有丢 Manifests。
- **Python 逐行对照。** 计划状态（add/pending/synced/remove、paused）、synced 判定、消息与摘要、
  `_connected` 最长前缀唯一性、HA 主机同网段与代理网段冲突、exempt 列表（≥/30 展开、去重、64 上限）、
  Netwatch 两段脚本的字节内容、ForwardHint 的目标收集/Fake-IP 优先/无目的选择器只测一个目标/
  首个接管规则（跳过 log、passthrough、动态、无 ID）/命令引号，均与 Python 一致。三值求值器的地址、
  范围、端口、协议、接口列表（含 include/exclude 和显式成员优先）、地址列表、ipsec-policy、
  hop-limit 与 Python 一致；Go 多出的接口列表预算与「空 / 错型 action 为 unknown」已在移植报告列明。
- **凭据与地址脱敏。** R09 不写日志；错误信息走共享 `request` 的 `internal/redact`（只遮密码/token）。
  写入 Netwatch 脚本的值只有 agent 与 exempt 地址：前者要求规范形式的 IPv4，后者经 `netip.ParseAddr`
  严格解析（不接受空白、前导零、zone），不可能注入脚本；接口名只进入 REST JSON 字段。用户消息里的
  地址是局域网地址，与 Python 一致。
- **四条常设检查。** ①超时：wrapper 只记录不改值，另有真实 11 秒无应答 PUT 证明 10 秒上限；
  ②原始请求字节与 headers 有精确断言；③未改共享驱动路径，F1 只改 R09 自己的文件，最终门禁在合并后
  的树上跑全部 routers/routersplit/devhttp；④Detach 后每个 perform 与 manifest 提交前检查
  `ctx.Err()`，真实 Runtime 测试覆盖；F1 新增的提交点走 `remapDNSGuard`，同样先检查 `ctx.Err()`。

## L5 补充项（加入 L5-C，只在隔离 CHR 实验网执行）

1. 未登记、带 `cf-dnsguard:hijack` 注释的 NAT（disabled 的 53 规则、非 dstnat/非 53 规则）在 Netwatch
   up/down 时是否被启停，命中计数是否变化（确认 F2 的风险模型）。
2. 新建的 Netwatch 在目标已无应答时，首次判定 down 是否执行 down-script（F3 修法的前提）。
3. 暂停期间只改 LAN 接口的发布：新劫持是否保持启用、DNS 是否中断（F3 复现）。
4. down 事件与发布重合时的回读、补偿结果（F4）。
5. RouterOS 重启后 NAT / Netwatch / 地址列表的 `.id` 是否保持（manifest 与任何 ID 绑定方案的前提）。
6. 外部同名 Netwatch 存在时 down 脚本 `get [find name=...]` 的行为。

## 验证命令与输出

修复前（`.64`，仅回退 `dns_guard_write.go` 的修复，新测试保留）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -timeout=10m -run "^TestR09Review" ./internal/routers'
```

```text
--- FAIL: TestR09ReviewPartialRollbackKeepsRestoredObjectsOwned (0.03s)
    r09_review_test.go:70: restored member still recorded under its deleted ID *67
FAIL	configflow/internal/routers	0.137s
```

修复后（`.64`），包括全部移植者的 R09 测试与真实 Python 对照：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers/)" && go vet ./internal/routers/ && go test -race -count=1 -timeout=15m -run "^TestR09" ./internal/routers'
```

```text
ok  	configflow/internal/routers	30.327s
```

最终门禁（`.64`，`-p 6`，树 = 修复提交（变基前 `700250b9`）+ 本报告与台账的文档改动，基底 `go@895f5e21`；gate 排队约 10 分钟后拿到槽位 2/2。
rt 超过 10 分钟被工具转入后台，gate 用 setsid 在服务器上继续运行，我在前台用 ssh 轮询日志直到结束，期间没有再同步代码；
远端脚本在排队超过 15 分钟时会自动放弃）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc '
export LANG=en_US.UTF-8 GATE_LOGS="$HOME/rt-logs/r09-review-gate"
server/scripts/gate.sh -p 6 FuzzR09DNSGuardManifest:./internal/routers FuzzR09DNSGuardOperation:./internal/routers FuzzR09PolicyEvaluator:./internal/routers
PYTHONPATH=. "$HOME/cf-test/.venv/bin/python" server/internal/routers/testdata/r09_mutations.py
'
```

gofmt、vet、两种 build tag（构建 + lifecycle）通过；全量 `-race` **93 个测试包全部 ok**
（routers 361.151s、routersplit 13.766s、devhttp 3.477s、installer 565.160s）；proddiff 退出 0，
全部 self-check PASS，coverage_regressions=0，**new=0 stale=0**；**14 个 difftest 包全部 ok**；
四类编译变异全部检出。rt 退出 0，末尾输出：

```text
FuzzR09DNSGuardManifest ok  	configflow/internal/routers	60.132s
FuzzR09DNSGuardOperation ok  	configflow/internal/routers	61.047s
FuzzR09PolicyEvaluator ok  	configflow/internal/routers	61.178s
GATE PASS
gate rc=0
MUTATION_DETECTED result
MUTATION_DETECTED journal
MUTATION_DETECTED request
MUTATION_DETECTED table
R09_REVIEW_GATE_PASS
```

日志：`.64:~/rt-logs/r09-review-gate/`（gate.out、race.log、proddiff.log、diff.log、fuzz-*.log、mutations.log）。

### 变基补验

门禁后 `git rebase go`（`go@dda8973d`，新增的只有 A13 agents/deploy 改动）。
`git diff --exit-code <门禁树> HEAD -- server/internal/{routers,routersplit,devhttp,lifecycle}` 无输出，
routers 相关源码与门禁验证的树逐字一致。补跑（`.64`）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... && go build ./... && go test -race -count=1 -timeout=15m -run "^TestR09" ./internal/routers && echo R09_REVIEW_POST_REBASE_PASS'
```

```text
ok  	configflow/internal/routers	36.773s
R09_REVIEW_POST_REBASE_PASS
```

## 提交

- `b3e93b5c` go(routers): R09 review keep restored objects owned after partial rollback（F1 修复 + 三个复核测试）
- 本报告及 PROGRESS 更新为单独的文档提交。
