# A18 HR 复核（DNS HA：keepalived 主备、VIP 交接、配对发布、A16/A17 接线）

独立 Opus 复核，非作者；分支 `go-opus-review-a18`，起点 `go-port-a18@ff3ecc008`（作者 Codex，基于本地 batch-6：
go + A16/A17 + N8-8a + N8-7b + O3-6b）。无子 agent、无 push、无生产/真实 Agent/PVE 访问；测试固定在 `.32`，
新增用例只用合成数据和回环 fake。

对照：Python `backend/agents/oxidns_ha.py`、`oxidns_ha_handoff.py`、`oxidns_ha_publish.py`（全文），
`routes/agents.py` 192–215 / 1233–1269 / 1556–1600 / 2224–2248；Agent 侧 `go-agent/routes/oxidns_ha*.go`（只读，
确认协议与暴露字段）；设计 `a16-a18-sync-follow-ha.md` §2.5.4–2.8、§3.3–3.7、§4.1–4.2、§6.8、§7–8；作者交接
`progress/A18-handoff-zh.md`；问题类来自 review-a16/a17/r04/r05/r06/a19/a24。

## 结论

- **high：0 条。未达到回退门槛（2 条 high）。**
- medium 1 条（Go 独有，A14/A18 接缝），已修，`467178838`。
- 继承自 Python 的活性缺口 1 条，需要维护者决定是否改行为（见 §2 O-1），本分支未改。
- 其余逐项核对未发现问题：主备顺序与撤销、配对发布顺序与失败保留、交接两次确认/超时/释放、D-A18-1、
  关机与断连、A16/A17 接线、脱敏、12 条 baseline。
- 状态：`verified`（L3 实机仍待协调者安排）。

## 1. 发现

### medium

- **M1 — 首次开启进行中，向任一成员发布返回 500 `deploy: DNS HA pair publication is not available (A18)`（已修，
  `467178838`）。**
  首次开启时 journal 已写到两台记录上（active），但两台的 `oxidns_ha.enabled` 要等 keepalived 阶段最后一次提交
  才写。此时 `deploy.Publish` 先调用 A18 pair publisher：`HAPair` 为空 → `handled=false`；随后 A14 为「A18 尚未
  接入」留下的保护 `pairRequired(t)`（`enabled` 或 `HAActive`）仍然生效 → `ErrPairPublisherMissing`（500，英文内部
  文本）。Python `publish()` 中 `pair()` 为 None → `_publish_deployment` → `begin_deployment` 的 HA conflict：
  409 `DNS 主备正在同步，请等待两台发布完成` + `ha_publication`。用户在开启过程（可能因设备未确认长时间停在
  unknown）中手动推送配置、发布已冻结的最终配置都会遇到。设备侧无影响（失败方向是拒绝）。
  证据：`TestPublishDuringFirstEnableAnswersPythonConflict`（两台各推一次，断言 409、中文文本、`ha_publication.id`
  等于开启 journal，且无上传）。修复前两台均失败：`got error deploy: DNS HA pair publication is not available (A18)`。
  修复：只有未安装 pair publisher 时才 fail closed；已安装但未接手的 Agent 交给 `PublishDirect`，由 A01 预留给出
  与 Python 相同的冲突。A16（HA journal active → `advancePair`，不发布）和 A17（自己先 `BeginDeployment`，被同一
  冲突拒绝）不经过此分支，已核对。

### 备注（不算发现）

- 开启时 `PrepareContent` 用了 `PreferCached:true, StoreDownloads:true`，Python `_prepare_transactional_content`
  默认均为 False。两项只作用于 Mihomo provider 下载，OxiDNS 配对的下载列表为空，行为无差异；未改。
- 日志：Python `upstream_findings` 解析失败时记一条 warning，Go 只给用户 warning 不记日志；`%T` 与
  `type(error).__name__` 文本不同。均不涉及敏感值，未改。

## 2. 需要维护者决定（继承自 Python，未改）

**O-1 — 交接中途一台成员永久失联时，配对管理永久卡在 `unknown`，无法停用 HA，也无法向存活成员发布。**

- 情形 A（新持有者在确认后宕机）：两次确认后 `confirmed=true`，30 s 超时不再检查；原持有者上传/激活前
  `ensure` 读视图，目标不可达 → `HandoffPending{unknown}`，每轮如此。
- 情形 B（原持有者在让位后宕机，最常见的「主节点宕机」）：未确认 → 30 s 超时 → `_fail` → `release` 读原持有者
  → 不可达 → `HandoffPending` → journal `unknown`（保留 `handoff_failure`），之后每轮重复。
- DNS 本身不中断：VRRP 仍会让存活一方持有 VIP（让位只降低优先级）；问题只在管理面：journal active →
  `Disable` 返回「主备配置发布尚未结束…」，推送返回 409，删除 Agent 也被 A01 拒绝。失联设备恢复后会自动收敛；
  若设备彻底报废，只能手工改库。
- 建议（需决定）：允许 `DELETE …/oxidns-ha?force=1` 在「journal 为 unknown 且至少一台成员离线 ≥ N 分钟」时
  清掉 journal 与两端 HA 字段（保留部署历史，返回人工清理提示），与现有 force 语义一致；或给交接后段加总超时
  后转 `failed`。两者都改变 Python 行为，需登记 O12。L3 第 12 项可先实测失联时长与恢复路径。

## 3. 复核范围逐项

### keepalived 主备、VIP 交接与恢复

- 开启：HA 锁 → 再次 normalize/上游检查/双端能力与环境证明/双端 preview → 先备机（secondary, 100）后主机
  （primary, 150）PUT → 双端记录一次事务写 `oxidns_ha` + `state=succeeded`（身份/组复查）→ settle ≤10 s。
  失败：反序撤销（重配=重放各自旧 spec，首次=DELETE），撤销失败 → 首次写 leftover（只写 state.members，不写
  `oxidns_ha`，路由器不发 VIP），重配写 `failed`（路由器停发 VIP）。与 Python `_configure_keepalived/_fail/_undo`
  一致；Go 在旧 peer 已不存在时记撤销错误而非 KeyError 500，属改进。
- 停用：pair 键 → HA 锁 → active 拒绝 → 先删 secondary → 有错且非 force：`_record_error(failed=false)` + `can_force`；
  force 或成功：pop 三个字段（leftover 不清已加入新组的成员），历史保留。一致。
- 交接：`ensure` 先持久化意图再 POST；GET 再 POST；记录 operation_id 必须等于 job id；双端视图唯一持有者 +
  keepalived/healthy/vip_healthy；两次连续确认；失败视图清零；30 s 未确认超时；`release` 只删自己的 operation，
  删后 GET 复核；`finish` 等主机重新持有 ≤30 s；`settle_failure` 双端健康时等正常选举。逐行对照一致；Go 另加
  operation/source/target 一致性与损坏标志拒绝（作者自查 high，保留）。

### 配对发布：两端一致性与半途失败

- 唯一持有者最后更新，无持有者/双持有者在任何核心写入前拒绝；备机发布结果未知/pending/不健康时主机不动；
  需要交接时原持有者先 staged、交接再确认后才激活；reload_only 无交接。
- 「一台新、一台旧」：Python 设计在主机发布失败（Agent 自行回滚）时就是备机新、主机旧，journal `failed`，下一次
  从任一成员发布会重发两台。Go 完全保持该行为，没有新增窗口；VIP 在失败后经 `release`+`settle_failure` 回到主机
  （旧配置）。这一点只能靠 L3 实测确认客户端无感（见 L3 第 3、11 项）。
- 面板中途停机/被杀：journal 与 payload 在盘上，已提交 child 只轮询不重传（A14 同 ID+digest 回放），让位丢应答
  先 GET 再 POST。作者真实 Runtime 用例覆盖，抽查通过。

### A16 / A17 接线

- A16：`rulesync.SetPairAdvance` → `ForManager(m).Advance`；worker 对 `HAActive` 只推进 journal，对已配对只让 leader
  运行；配对 rules 作业 receipt 基线 + `FetchSnapshot` + `PrepareCandidate(library, forceRefresh)`，reload_only。
- A17：DNS 联动对 HA 成员走 `deploy.Publish` → pair publisher，要求两端 `applied_content.fingerprint` 相同；联动先
  `BeginDeployment`，开启中被 HA conflict 拒绝（M1 修复不影响）。作者 `ha_integration_test` 覆盖，抽读通过。
- A07 `monitor.SetHARules`、A01/A15 三个 hook、A14 `GenerateFinal` 双端最终配置相等检查均已核对与 Python 一致。

### D-A18-1（keepalived 阶段 HA 锁忙）

- 忙 → `errHALockBusy` → `step` 返回 nil，journal 保持 `keepalived/applying`，下一轮（2 s 或任一状态 GET）从头重跑
  `_step`（已提交 child 只轮询，不重传；不写设备）。作者 `TestKeepalivedBusyRetriesNextRound` 抽跑通过。
- **上限**：没有计数上限，但不会活锁。HA 锁只被 `Configure`、`Disable`、`configureKeepalived` 持有，三者都是有界
  的设备调用（preview 30 s、apply/remove 90 s、settle 10 s），且全部 try-lock。最坏情况是另一组 HA 的停用
  （2×90 s）期间本组每 2 s 空转一次（约 5 次设备读 + 1 次 journal 写）。同一组的停用先拿 pair 键，与推进互斥；
  同一组重复开启只读后返回摘要。用户持续点击「开启」才会一直占锁，不现实。结论：不需要上限，已登记 O12 即可。

### 真实 Runtime 关机与断连

- 所有入口经 `RunDetached`；断连不取消；关机 Stopping 时 settle 等待返回且不记失败（作者自查修复）；排空截止后
  每个提交点（repo、payload 文件、设备写）先查 `ctx.Err()`。抽跑 `TestStoppedRuntimeRefusesEveryHAWrite`、
  `TestDisconnectedRequestCompletesEnableAndPair`、`TestShutdownAtAcceptedDeviceWriteResumesJournal`、
  `TestStopSignalDuringElectionDoesNotFailAppliedPair`（race）通过。
- 已知（Python 相同，设计 §3.7 已列）：停用在「设备已删除 keepalived、记录尚未删除」之间被杀，记录仍是开启，
  路由器继续发 VIP 而无人持有，直到再执行一次停用。属于 A24 规则「关机后不落写」的代价，L3 第 6 项要实测。

### 12 条 deferred → A18 baseline

- 删除的 12 条：`GET …/oxidns/rules/update` 5 条、`/api/logs/info|tail` 6 条后果、`generate-config`
  `a18-ha-refusal` 1 条。`proddiff-normalize.json` 在 A18 提交中未改，没有新增吞差异的规则。
- 本复核跑了一次完整 compare（`.32`，见 §5）：`baseline: new=21 stale=0`，21 条中**没有** rules/update 和
  generate-config；rules/update 按原比较器逐字段严格比较（作者负控 task.status 改值可检出）。
- generate-config 在离线快照里两边都因连接不到 Agent 失败（400），由 A14 已批准的「同一目标传输错误」规则
  归一；这证明 A18 拒绝已消失、走到与 Python 相同的位置，但不证明 HA 最终配置成功路径——该路径由
  `TestFinalGenerationRequiresMatchingPeerAndPinsBothIdentities` 与 Flask difftest 覆盖。
- 21 条新差异与 A18 无关：
  - 10 条 `/api/logs/info|tail`：Go 多出 35 条 `Exception on GET /api/…/subscriptions/<id>/proxies`、
    `…/aggregations/<id>/provider` 的 `context canceled`，对应本次运行 Go 侧 131 次传输重试、loadavg 12–17
    （复核期间同机还在跑测试）。按 BRIEF 属高负载差异，需安静时重跑确认。
  - 11 条 `/api/tasks`、`/api/notifications`：`missing_slots: ["router_rule_sync"]`，路由器域行提供者未注册
    （C07 readiness `7dc4a8c15` 早于 A18；A18 提交未触及 tasks/notifications）。
  - 建议协调者在 batch 门禁里确认这两组在 batch-6 基线上是否同样出现。

### 脱敏

- 面板从不持有 keepalived 认证口令：spec 只有 `safety_version/vip/vrid/priority/self_ip/peer_ip`，Agent 的 VRRP
  配置不用 auth_pass；Agent 本地所有权 token 不出现在 `GET /api/oxidns-ha` 的 `managed`（Agent 代码已核对）。
- 设备文本进入用户视图前均经 `s.safe`（全部 Agent token + URL 凭据）：设备错误、preview/environment blockers、
  回环上游、成员发布失败、409 body 与 blockers 再脱敏一次；handoff 文本是常量；日志只有 `%T` 和常量。

## 4. 问题类核对（R04/R05/R06/A19/A24）

| 类 | 结果 |
|---|---|
| 测试替身不覆盖生产参数 | fixture 只改目标地址，不改 (5,15)/(5,30)/(5,90) 超时 |
| 原始请求比较 | `TestRawClientRequestsAndMalformedReplies`、`TestRawKeepalivedSpecProtocol` 比较方法/路径/body 字节/头 |
| 缺失身份不能匹配 | journal/payload/operation/child id 空值即不匹配；identity 前后比较保持 Python 相等语义 |
| ID 定向撤销 | 只撤已尝试成员；只删自己的 operation |
| 共享驱动改动 | M1 改 `deploy.Publish`；`internal/deploy` 全包 race 跑过（唯一失败见 §5，与改动无关） |
| 关机后不落写 | 见 §3 |
| 锁序 | 全部 try-lock，`TestMain` 断言 `lockx.Violations()==0` |

## 5. 运行记录（固定 `.32`，未跑完整 gate）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'server/scripts/proddiff.sh'
# baseline: new=21 stale=0 （见 §3，均非 A18；self-check PASS）

# M1 修复前（测试先行）
go test -count=1 -run TestPublishDuringFirstEnableAnswersPythonConflict ./internal/agents/dnsha
# FAIL … got error deploy: DNS HA pair publication is not available (A18)

RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/deploy internal/agents/dnsha)"; go vet ./internal/deploy ./internal/agents/dnsha ./internal/api/agents; GOMAXPROCS=4 go test -race -count=1 -timeout=20m -run "TestPublishDuringFirstEnable|TestKeepalivedBusy|TestInitialEnable|TestPairPublicationFromEitherMember|TestStoppedRuntime|TestDisconnected|TestShutdownAtAccepted|TestStopSignal|TestFinalGeneration" ./internal/agents/dnsha; GOMAXPROCS=4 go test -race -count=1 -timeout=15m -run "HA|Pair|Publish|Push" ./internal/api/agents'
# ok configflow/internal/agents/dnsha 222.585s
# ok configflow/internal/api/agents 41.319s

GOMAXPROCS=4 go test -race -count=1 ./internal/deploy
# 仅 TestReviewA14UnforcedProgressIsThrottled 失败（1 s 节流，-race 下两次 DB 写间隔超过 1 s）；
# 非 race `-count=3` 通过（ok 2.441s）。与 M1 改动无关（只改 Publish 的 fail-closed 分支），属 A14 测试的计时敏感，
# 交协调者在门禁里看是否复现。
```

## 6. L3 实机验证项（补充到 `lab/L3.md`，执行顺序建议 1→12）

见 [L3.md](lab/L3.md) 第 1–10 项（作者）与第 11–12 项（本复核新增）。重点量化项：

1. **开启**：两台新实验 OxiDNS（systemd + OpenRC）。dig 循环（每 100 ms，对 VIP 与两台实地址）全程记录；确认
   备机先 PUT、主机后 PUT，两端 `state=succeeded` 且 journal `succeeded` 前路由器视图不发 VIP。
2. **主节点宕机**：持有 VIP 的主机 `poweroff -f`（模拟断电，不是 systemctl stop）→ 记录 VIP 漂移到备机的时间与 dig
   失败次数；主机上电 → 抢占回主机的时间与失败次数；面板 GET 状态在三个阶段的 `health`（ok→degraded→ok）。
3. **完整配对发布（需交接）**：记录 VIP 从主机让给备机、两次确认、主机 staged→激活、撤销让位、主机重新持有的
   时间线，dig 失败数应为 0 或仅限测得的切换窗口；同时比较两台最终 `config.yaml` 摘要相同。
4. **丢应答**：在实验桥上用 nft 丢弃让位 POST、DELETE 的响应各一次 → journal `unknown` → 恢复后收敛，原持有者
   上传次数为 1。
5. **面板中途停机**：在让位已确认、原持有者 staged 未激活时 SIGTERM，再试 `kill -9`；重启后不重传、继续交接。
6. **停用中途被杀**：备机 DELETE 完成后 `kill -9` 面板 → 观察路由器视图仍发 VIP、主机 keepalived 状态、dig
   结果；再次停用后清理完成。记录这段时间客户端是否断解析（§3 已知风险）。
7. **keepalived 应用失败**：让主机 PUT 失败（改主机 keepalived 二进制权限）→ 备机被撤销；再让撤销也失败 → leftover，
   停用清理。
8. **D-A18-1**：journal 停在 keepalived 阶段时，并发对另一组 HA 执行停用（或在面板里人为占 HA 锁），确认本组
   保持 applying、每 2 s 重试、锁释放后成功，且设备 PUT 只发生一次/台。
9. **配对发布中途断电（备机）**：备机发布成功后、主机未更新前给备机断电 → journal unknown，主机不动；备机上电后
   收敛。
10. **配对发布中途断电（主机）**：主机 staged 上传中断电 → 期间 VIP 在备机，dig 不断；主机上电后收敛或按超时失败
    并恢复正常选举。
11. **主机发布失败后的「一新一旧」**：人为让主机候选激活失败（Agent 回滚）→ journal failed；分别在 VIP 位于主机/
    备机两种情况下做相同查询，确认差异只来自新旧配置本身、DNS 不中断；再发布一次两台一致。
12. **O-1 失联卡死**：交接确认后让新持有者永久断网（或让位后让原持有者断网）10 分钟，记录 journal 状态、停用/推送
    返回、VIP 与 dig；恢复网络后应自动收敛。结果供 O-1 决定。

所有对象必须是登记的新实验设备，不接触生产 DNS（WORKFLOW「VM lab tests」）。
