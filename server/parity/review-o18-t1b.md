# HR 复核：O18 T1b（Agent/部署/HA/IPv6/规则同步/DNS 联动/注册会话记录改普通结构体）

分支 batch-32（= go-o18-t1b，go@44424c19f 之上 8a3fcdfe7…1eb67c83d），复核 2026-11-22，测试在 .32/.64。
按 WORKFLOW「Leaner verification」：不重跑作者全套对照；跑自己修复涉及的包、受影响包 `-race`、相关 difftest、真 Python CrossCheck、
preflight；可能空过的守卫各做一个负向对照。

## 协调裁定的实现（a1a54b509、d4faa2d47）

**会触发设备写入或自动推送的开关，库里存成非布尔值时读作「关」并告警**（`model` 里以结构体标签 `stored:"gate"` 标注，
`storedUnmarshalers` 的布尔规则按解码器的根类型 + JSON Pointer 找到字段标签；`DecodeStoredMember`/`SetStoredMember` 直接解码单个
成员时也认标签）。逐个列出：

| 成员 | 触发的写入/推送 | Python | Go（复核后） |
|---|---|---|---|
| `agents[].enabled` | 对该 Agent 的一切自动推送（规则同步、DNS 联动、HA、IPv6 校验）的总开关 | 多处 `get('enabled', True)` 真值，少数 `is False` | 错类型 = 关 |
| `agents[].domain_discovery_enabled` | 生成配置里的区域探测监听、心跳回复让 Agent 开启域名发现 | `bool(get(...))` | 错类型 = 关 |
| `agents[].traffic_history_enabled` | 心跳回复让 Agent 开始记录流量历史 | `bool(get(...))` | 错类型 = 关 |
| `rule_sync.settings.library/rules`（含 HA 日志的 `rule_sync_settings`） | 规则同步 worker 自动下发规则库/设备规则 | `is True` | 错类型 = 关（与 Python 一致） |
| `rule_sync.state.force_refresh` | 强制重新下发全部规则文件 | 真值 | 错类型 = 关 |
| `dns_follow_state.retry_requested` | DNS 联动重新发布 | 真值 | 错类型 = 关 |
| `oxidns_ha.enabled` | HA 成员配置/keepalived（同时是编辑锁） | `is True` | 错类型 = 关（与 Python 一致） |
| `oxidns_ha_publication.activate` | 发布时激活（X-Activate） | 真值 | 错类型 = 关（只上传不激活，状态 ready） |

保留真值语义的（不是写入开关）：部署记录的 `success/manual_rollback/pending/submitted/previous_running/rollback_rejected/
ipv6_verification_requested`、`applied_content` 的 `reload_only/final_config`、HA 交接状态 `handoff_required`（取真 = 先交接再激活，
更保守；读作关反而会两台同时激活）、`handoff.confirmed/released`、`identities{*}.enabled`（身份快照比对）、旧版测速端点 `installed`。
`null` 照旧读作关（T1a 裁定）。

**HA `enable` 存成任意对象视为启用意图：评估后保留。** 唯一写 keepalived 的路径是 `step` 末尾的 `configureKeepalived`，
它先经 `validated()` → `normalize()`：对端必须是存在的 Agent、VIP/VRID 合法、上游检查与预览通过，否则报错、不发请求。
`{}` 或缺 `peer_id` 的残缺意图在这里失败（发布失败，不写设备）；能通过校验的意图就是 `start_enable` 写下的完整请求，Python 同样会执行。
锁定配对（`HAEnablingModel`）是 fail closed，保留。已写入 behaviour-changes。

## 发现

### 高

**H1 存成非对象的 `oxidns_ha_publication` 读作「没有」，解开 HA 配对锁（已修，a1a54b509）**
作者的 fail-closed 修复（da30e4565）只覆盖「对象但内容不对」：`{}`、`status` 错类型都算进行中。但通用规则
「指针记录存成别的 JSON 类型 → nil」让 `"applying"`、`1`、`[]` 这类值读成 nil → `HAActiveModel` 返回 false →
配对的编辑/部署/换绑/规则同步锁全部解开（Python 遇到非对象直接抛异常，等于拒绝）。修复：记录类型可实现 `failClosed`，
存成非对象时读作空记录（+ 告警）而不是 nil；只给 `HAJournal` 用。`null` 仍是没有。
测试 `model.TestReviewO18T1bDamagedHAJournalFailsClosed`；负向对照：关掉 `failClosed` 分支后失败。

**H2 会触发设备写入的开关错类型按真值取开（按裁定修改，a1a54b509）**
见上表。改前 `"false"`、`"no"`、`1` 存在 `rule_sync.settings.rules` 等成员里会让 worker 自动下发；Python 对其中几项用 `is True`。
测试 `model.TestReviewO18T1bWriteGatesOfAnotherTypeReadOff`（`DecodeStored` 单条/整份 System、存储行 `DecodeStoredMember("agents")`、
`json.Unmarshal`、单成员解码；非 gate 开关仍为真值；真布尔与缺失不变）。负向对照：`writeGate` 恒为 false 时失败。
随之改了 4 个按旧真值断言的测试（agents_view、registry_native、rulesync `AgentSettings`、api/agents 心跳偏好），
o10t2 参考语料 1027 行哈希变化：把两处修复同时关掉、用旧语料跑 `TestO10T2ReferenceCorpus` 为 0 差异，即变化全部来自这两处。
pyref 13 个文件只重录 metadata（逐个解压比对，只有 `metadata` 键变化；`PYREF_MODE=check` 通过）。

### 中

**M1 作者交回时真 Python 对照有 4 个失败（已修，6925c2502、00d6ec022）**
带 difftest 环境（同 gate-live.py：`CONFIGFLOW_DIFF=1` + 真 Python）时，作者树 1eb67c83d 上
`repo.TestCrossCheckOperations`、`TestCrossCheckInterchange`、`TestCrossCheckC04Operations` 与 `api/agents.TestPublicAgentPython` 失败
（用临时 worktree 在作者提交上复现，与本复核修改无关）。逐条看：
- 一处是**真实接口差异**：`/api/profiles` 列表和绑定视图里，没有 `port` 的 Agent 在 T1b 后显示 `port: 0`，Python 与改前 Go 是 `null`。
  已修（`agentBinding`：端口为 0 即缺失，输出 `null`；Go 不会存 0 端口），加普通单测 `repo.TestReviewO18T1bBindingMissingPortIsNull`。
- 其余都是已认可的编解码差异：Python 保留了 T1b 丢弃的未声明成员（`z`、`future`）、`null`、空文本（`name:""`、`token:""`）、
  错类型值（`deployments:[…]`、`ipv6_verifications:[]`、`members:["a"]`）、以及始终写的 `active_agent_id:null`/`started_at:0`。
  CrossCheck 夹具受 `n89_cache_reference_test` 的哈希保护不能改，改为在对照层把 Python 记录里的**存储 Agent**（系统记录的 `agents`、
  `agents` 行文本、`get_agents` 结果；绑定视图不动，`"<not read>"` 历史标记保留）按 Go 编解码规范化后再比——只抹掉 Go 按裁定无法表示的部分，
  Go 写入逻辑的差异仍会暴露。负向对照：撤掉 `port:null` 修复后 Operations 与 C04 都在 `list_profiles` 步骤失败。
  `TestPublicAgentPython` 改为让 Python 投影 Go 存储后的记录（空的 HA 摘要两边按 `{}` 对齐，见 L6）。
- 另有两条 n8-11 的 Agent `enabled` 真值回归测试（notifications、routers）按 gate 裁定改为 `1`/`"yes"` = 关。

### 低（记录，未改）

- **L1 单行存储放大宽松改写。** `system.agents` 是一行；任何 Agent 写入（包括心跳上报版本变化）都会把所有 Agent 按宽松读出的值重新编码。
  逐项看安全相关字段，改写后的值都是 fail closed 或与 Python 语义相同：损坏的 `token`（对象/列表）→ 删除 → 空令牌不鉴权通过
  （`ReadAgentByToken` 要求非空）、`CallAgent` 不发请求；gate 开关 → `false`；非对象 HA 日志 → `{}`（仍锁定）；
  `latest_upgrade.status`/部署 `status` 错类型 → `""`，不算忙（Python `in (...)` 同样为假）。未知成员丢弃是已认可的裁定。
- **L2 注册会话 `host` 错类型读作空 = 不限主机。** `admitRegistrationClaim` 里 `session.Host != "" && …`；会话是 Go 自己写的一次性凭据、
  有 TTL、还要求凭据摘要匹配，实际只有手改库才会出现。如要收紧可把 `Host` 改成 `*string`，留给 T1c 一并处理。
- **L3 数字令牌。** 库里 `token: 7` 读作 `"7"`，`Bearer 7` 可以通过心跳鉴权（Python 用 `==` 比较不会相等）。Python 生成的令牌都是文本，
  只有手改库才会出现，且攻击者仍需知道该值；已在 behaviour-changes 中由作者列出（数字令牌）。
- **L4 `dns_follow_state` 存成非对象读作没有**，DNS 联动的「准备中/发布中」忙锁随之消失（Python 遇非对象抛异常）。
  该锁只防并发重复发布，不涉及跨设备安全；不改，必要时同样实现 `failClosed`。
- **L5 `DeploymentReply` 透传设备回复的未声明成员**（capabilities、content、listener 等）：与改前（`model.Fields` 保留未知成员）及 Python 相同；
  诊断文本成员（message/error/…/failed_stage）照旧经 `DiagnosticText` 脱敏，DNS 报告经 `SanitizeDNSReport`，HTTP 出口还有 token 脱敏。
  不进存储（`recordDeploymentReply` 只写类型化成员）。不是回归。
- **L6 空的 HA 摘要写成 `{"active_agent_id":null}`。** `active_agent_id` 始终写（作者裁定），所以存储/接口里原本是 `{}` 的
  `ha_publication`/`oxidns_ha_publication` 变成只有这一个成员的对象。Go 自己的投影正常；Python 把非空摘要当作发布并取 `status`，
  读 Go 写的库时会 KeyError（只有空摘要这种非正常数据才会出现，Python 已退役）。不改，记录。
- **L7 Agent `enabled` 列为 gate 是本复核的判断。** 它是对该设备一切自动推送的总开关，所以按裁定处理；代价是库里 `"yes"`/`1`
  会让 Agent 显示为停用（Python 多数位置按真值为启用）。若协调认为它不属于「写入开关」，去掉 `agents.go` 里 `Enabled` 的
  `stored:"gate"` 标签并恢复 4 处测试期望即可。

## 逐项结论

1. **Agent 协议**：注册（`DecodeRegistrationInput` 宽松 + `decodeRegistrationClaim` 对会话校验严格，`requireJSONObject` 拒绝重复成员）、
   心跳（令牌查找要求非空；`version`/`config_version` 只在文本且有变化时写）、部署回执（`DecodeDeploymentUpdate` 只取协议成员白名单，
   服务端持有的 deployment_id/准备内容/校验回执进不来；重复成员被 `ValidJSON` 拒绝）、设备回复（同改前，见 L5）、指纹 A2
   （`liveFingerprint` 改用 `EncodeMember`，成员集不变）。宽松处接受的值（`success:"yes"`、`manual_rollback:1`）与设备发真布尔是同一信任级别，
   改前 Go 只认字面 `true`，Python 按真值；不构成伪造面。
2. **H1/H2 同类**：见 H1、L1–L4。空令牌不放行（`ReadAgentByToken`、`CallAgent`、`remote_model` 三处均要求非空）；
   HA 日志状态零值 = 进行中（不是已完成）；升级/部署状态零值不算忙与 Python 一致；IPv6 校验只认具体状态值（`passed`/`succeeded`），零值不放行。
3. **OrderedMap / Number**：`Set/SetAll/Delete` 先复制条目切片再改（写时复制），解码结果 `slices.Clip`，借用浅拷贝上的修改不会写穿；
   `Get` 返回值内的切片/指针与原记录共享（与改前结构体 Map 相同，借用约定照旧）。`Clone` = 编码再按存储规则解码，所有已声明成员（含仅响应成员
   `restart/file_count/...`，它们有 json 标签）完整往返；`Number("")` 为 omitzero 不写。`-race` 覆盖 agents/api/agents/repo 的借用路径（见验证）。
4. **视图缓存（R4）**：键仍是 (head, 位置, id, 视图) + 心跳指纹；指纹成员 `liveMembers` 不变，取值从 `MemberJSON` 换成 `EncodeMember`（编码一致）。
   同代凭据：`GenerationCredentials` → `AgentRecordCredentialsOf` 仍只从借来的那一代记录收集；`sanitizationTokensModel` 改读类型化 `Token`。
   未知成员被丢弃后不再作为凭据来源（它们也不再出现在响应里）。
5. **-race**：见验证（作者 da30e4565 之后未跑 race；本复核在含修复的树上跑了全部受影响包）。
6. **存储兼容**：null→缺失 24 处可接受（读取方对 null/缺失同等处理；`active_agent_id` 始终写 null）。真 Python CrossCheck：作者树上失败（M1），
   修后 FreshInit/Operations/Interchange/Lock/WriterKilled/C04、`TestStoredRoundTrip`、`TestReviewN85PythonFullStorageDifferential` 全部通过。

## 提交（batch-32，1eb67c83d 之后）

- a1a54b509 go(model): O18 T1b review: device-write switches of another type read off; damaged HA journal stays locked
- d4faa2d47 go(pyref): O18 T1b review source metadata re-recorded (entries unchanged)
- 6925c2502 go(repo): O18 T1b review: a binding of an Agent without a port shows port null
- 00d6ec022 go(test): O18 T1b review: real-Python comparisons follow the accepted Agent codec
- 本报告单独提交

## 验证（树 00d6ec022；之后只有本报告）

- `gofmt -l internal tools cmd` 无输出；`go vet ./...` 与 `-tags configflow_proddiff,configflow_difftest` 通过（VET-OK）。
- `-race`（.32，含 a1a54b509 的树，`-timeout 60m -p 6`）：agents/...（含 dnsha、dnsfollow、ipv6、monitor 615.6 s、rulesync、upgrades）、
  api/agents 373.4 s、api/clientdevices、api/config、api/profiles、api/routers、configgen、deploy/... 337.9 s、gateway/installation/...、
  installer 508.6 s、jsondecode、model、oxidns 857.4 s、redact、repo 334.1 s、routersplit、webdav、discovery 全部 ok，无 DATA RACE；
  当时 notifications、routers 只各有一条 Agent `enabled` 真值回归测试失败（按裁定改期望，00d6ec022）。
  修后再跑 `-race`（.32）：repo 262.8 s、notifications 21.1 s、model 7.3 s、api/agents 307.2 s、agents 308.5 s、agents/rulesync 77.5 s、
  routers（`-run SwitchTruthiness`）全部 ok。作者 da30e4565 之后缺的 race 已由这两轮覆盖。
- difftest（.64，`-tags configflow_difftest`，环境同 gate-live.py，真 Python）：api/agents 106.7 s、api/agents/difftest 308.0 s、
  agents/dnsfollow 81.3 s、agents/dnsfollow/difftest 24.4 s、api/config/difftest 84.1 s、gateway/installation/difftest 11.4 s、
  api/linuxgateway/difftest 14.7 s、model、auth、repo 169.2 s（含全部 CrossCheck、`TestStoredRoundTrip`、
  `TestReviewN85PythonFullStorageDifferential`）全部 ok。
- pyref：mosdns、oxidns `PYREF_MODE=check` 通过（重录后再查）；13 个文件只有 `metadata` 变化。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-32 x 6`（.64，同机另有 installer -race，负载约 19）：vet 两套 tag 通过，
  `PREFLIGHT PASS`。
- 负向对照：`writeGate` 恒 false / 关掉 `failClosed` → 两条 model 复核测试失败、旧 o10t2 语料 0 差异；撤掉 `port:null` → CrossCheck
  Operations 与 C04 在 `list_profiles` 失败。
