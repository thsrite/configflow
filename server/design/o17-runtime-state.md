# O17 ② Agent 运行状态移出配置文档 + ① 后台循环改变化通知 — 设计（已裁定）

分支 `go-o17-runtime-state`（= `go@ce10c8375`），2026-11-23。对应 `parity/o17-idiomatic-go.md` 1.1、1.4 与 2.0 表。
O18 T1d（batch-42）合入后 rebase 再实现；本文只依赖 T1b 之后的 `model.Agent` 普通结构体形状，不依赖 `model` 旧机制。

命名：Agent 包里已有内存心跳 `agents.RuntimeState`，为避免混淆，本文把**要落盘的运行状态和历史**叫 **Agent 状态（agent state）**，
表名 `agent_state`；内存心跳仍叫 live/heartbeat。

---

## 0. 结论先行

1. **存储：同一个 `configflow.sqlite3` 里新建表 `agent_state(agent_id, member, value, revision)`**，与配置行在**同一个 SQLite
   事务、同一把 L3 锁**里提交；`store.Head` 增加 `State`（`meta.state_generation`），只改状态行时配置的 `Generation` 不变。
   不用按 Agent 的 JSON 文件、也不用第二个 SQLite 文件（理由见 2.2：守卫需要跨「配置 + 状态」的原子读写；第二个文件让备份/恢复要同时管两个文件）。
2. **内存模型不拆**：`model.Agent` 保持现在的全部成员，作为「配置 + 状态」的合并视图；拆分/合并只发生在 repo 的行编解码这一层。
   约 300 处读写方一行不改，API、Agent 协议、前端的字段和值不变（只是来源变了）。
3. **缓存默认安全**：`Head` 相等比较默认包含 `State`，所以现有按 head 作键的缓存行为不变（不会读到旧状态）；只读配置的缓存
   **逐个显式**改成 `SameConfig`，这一步才拿到「状态变化不让缓存失效」的收益。
4. **迁移**：`storage_version 1 → 2`，打开时在 `.config.lock` 下、Startup 之前、先在线备份到 `DATA_DIR/backups/`，再用**一个 SQLite 事务**
   把 `system.agents` 里的状态成员原字节搬进 `agent_state`。断电 = 要么 v1 原样、要么 v2 完整；重复打开 = 不再迁移。
   不做「新旧两处双写，确认后再删」：两份真值会冲突，备份文件就是「保留的旧值」（2.5）。
5. **导出/备份**：完整导出与 WebDAV 备份不再含状态成员；导入旧文件（含状态成员）时照迁移规则搬进状态表，兼容 Python 时期备份。
   代价：用新备份恢复后每台 Agent 需要推送一次（需裁定 D3）。
6. **① 循环**：repo 提交后按「配置变了 / 状态变了」通知订阅者，心跳在线/版本变化也通知；`dns-follow`、`oxidns-ha-publish`、
   `ipv6-verification-scheduler` 绑定 `Wake`，有进行中的工作时保持现在的 5/2/5 s，空闲时 600 s 兜底；删除 `IdleRounds`。
   `agent-rule-sync`、`scheduled-refresh`、`router-rule-sync` 同批改为「按到期 + 唤醒」（需裁定 D6）。

---

## 1. 范围

**包括**：`system.agents` 里每个 Agent 的运行状态/历史成员（第 2.1 表）；`store` 表结构、Head、迁移框架；repo 事务提交、
导出/导入、提交钩子；按 head 作键的缓存逐个归类；P7/P8/P9（必做）与 P10/P1/P2（建议同批）的唤醒改造；删除 `IdleRounds`。

**不包括**：
- 路由器记录里的 `last_release`、`rule_sync` 回执（回执已是文件；`last_release` 同类问题，本设计的机制可复用，切换后另起任务）。
- `system.agent_registration_sessions`（短期会话，导出时本来就清掉）。
- `sub-store-idle`（1.3，与通知机制无关，建议单独小任务）、时间类型统一（1.10）。
- 把 `model.Agent` 拆成配置/状态两个 Go 类型（见 6.3「后续」）。

---

## 2. 设计

### 2.1 成员归类（逐个）

「写入方」只列主要写入路径；频率是一次普通发布/操作里的量级。

| 成员 | 归类 | 谁写 | 多频繁 | 谁读 |
|---|---|---|---|---|
| `deployments` | **状态** | `agents` 部署状态机（reserve/record/actions/prepared）、`deploy` 发布、`dnsha` 成对发布 | 每次发布约 6–10 次迁移（preparing→uploading→verifying→ready→activating→succeeded/failed） | Agent 页、`deploy`、`dnsfollow`、`dnsha`、`gateway/installation`、`monitor`、通知 |
| `latest_deployment` | **状态** | 同上（与 `deployments[id]` 同事务） | 同上 | 同上 + `clientdevices`、`upgrades`、`rulesync` |
| `applied_content` | **状态** | 部署成功（`deployment_record.go`） | 每次成功发布 1 次 | `contentsync`（待推送）、`rulesync` 基线、`dnsfollow`、`dnsha` |
| `config_version` | **状态** | 部署成功、心跳上报变化、远程操作 | 发布 1 次；心跳只在变化时 | Agent 页 |
| `config_revision`、`pushed_at` | **状态** | 部署成功 | 发布 1 次 | Agent 页、`ipv6`、`dnsha`、`gateway` |
| `version` | **状态** | 注册（默认 `1.0.0`）、心跳上报变化 | 升级后 1 次 | Agent 页、升级、能力判断 |
| `latest_upgrade` | **状态** | `upgrades`（A15） | 每次升级若干次 | Agent 页、升级忙锁、`dnsha` |
| `rule_sync.state` | **状态**（`rule_sync.settings` 留在配置） | `rulesync` worker `saveState`；「强制刷新」写 `force_refresh` | 规则源变化时每台数次；失败重试 | Agent 页、`rulesync` |
| `dns_follow_state` | **状态** | `dnsfollow` 每轮/每次发布 | 联动发布时数次 | Agent 页、编辑忙锁、部署预留 |
| `dns_follow_job` | **状态** | `dnsfollow` | 联动发布时 1–2 次 | `dnsfollow` |
| `oxidns_ha_state` | **状态** | `dnsha` keepalived 配置步骤 | HA 启用/停用时 | Agent 页、`dnsha` |
| `oxidns_ha_publication`（发布日志，含 `handoff` 让位、`auto_complete`） | **状态** | `dnsha` 每 2 s 推进 | 成对发布期间每步 1 次（十几次） | 配对锁（编辑/部署/换绑/规则同步）、Agent 页、`monitor`、`upgrades` |
| `ipv6_verifications` | **状态** | `ipv6` 校验 job | 每次发布后 1 次 + 手动 | Agent 页、IPv6 卡片 |
| `status`、`last_heartbeat` | **状态（遗留，冻结）** | Python 时期写；Go 不再写（在线状态在内存） | 0 | 内存心跳缺失时的回退显示 |
| `service_status`、`system_metrics`、`cpu`、`memory`、`disk`、`network` | **状态（遗留，冻结）** | Python 时期写；Go 不写 | 0 | 回退显示 |
| `id`、`name`、`host`、`port`、`token`、`service_type`、`profile_id`、`deployment_method`、`enabled`、`created_at`、`updated_at`、`logging_config`、`gateway_installation_id`、`docker_ha_installation_id`、`domain_discovery_enabled`、`traffic_history_enabled`、`traffic_history_since` | 配置 | 用户编辑、注册、换绑、开关 | 低 | 全部 |
| `rule_sync.settings`、`dns_follow`（绑定）、`oxidns_ha`（设置）、`ipv6_verification_settings` | 配置 | 用户 | 低 | 全部 |
| `node_test_dns_endpoints` | 配置（遗留，只读） | 无（功能已退役） | 0 | 旧数据保留 |

**归类准则（D2 裁定，硬要求）**：用户在页面设置的意图/开关一律留在配置——`enabled`、`oxidns_ha`（启用与配对设置）、
`rule_sync.settings`、`dns_follow` 绑定、`ipv6_verification_settings`、域名发现/流量历史开关等；只有设备回报或后台任务产生的
过程与结果进状态表。上表「谁写」列即依据：凡是写入方是用户请求、且值表达用户选择的，归配置。`force_refresh` 虽由用户点击写入，
但它是一次性的操作请求、被 worker 消费，不是持续的设置，所以归状态。新增的 `restored` 标记（2.5.1）由导入写入、由发布清除，归状态。

**边界项（D2 已按下列建议裁定）**：

- `dns_follow.upstream_signature`、`dns_follow.after_deployment_id`：后台联动成功后写回绑定（「上次已应用的上游签名」），语义偏状态，
  但嵌在绑定对象里且只在上游 DNS 真变化时写（很少）。**建议留在配置**：拆嵌套对象的复杂度不值得；代价是联动成功时多一次配置提交
  （会唤醒一次自己的循环，无害）。
- `dnsha` 发布完成时写 `oxidns_ha`（启用意图落成设置）：这是后台任务替用户把意图写成配置，**留在配置**，与日志同事务（见 2.3）。
- `updated_at`：只在配置编辑/换绑/注册时改（已核对：状态迁移不改它），留在配置，值不变。
- `rule_sync.state.force_refresh`：用户点「强制刷新」写入，但它是一次性操作请求，被 worker 消费，**归状态**。

规则：状态成员的清单只在一个地方定义（`model` 里的 `AgentStateMembers`，含 `rule_sync.state` 这一个嵌套路径），repo 编解码、
迁移、导出、WebDAV 判定都引用它；测试断言清单覆盖了 `model.Agent` 的每个成员（新增成员必须归类，否则测试失败）。

### 2.2 存储位置与格式

```sql
CREATE TABLE agent_state (
  agent_id TEXT NOT NULL,
  member   TEXT NOT NULL,          -- 'deployments' / 'latest_deployment' / … / 'rule_sync.state'
  value    TEXT NOT NULL,          -- 该成员的 JSON 原字节（与现在 agents 行里的子串逐字节相同）
  revision INTEGER NOT NULL,       -- 同 fields.revision：行每变一次 +1
  PRIMARY KEY (agent_id, member)
) WITHOUT ROWID;
-- meta: state_generation（状态行有变化的提交 +1），storage_version = 2
```

为什么是同库独立表：

| 方案 | 守卫的原子性 | 崩溃一致性 | 改动量 | 结论 |
|---|---|---|---|---|
| **同库 `agent_state` 表** | 一个 SQLite 事务同时读写配置行与状态行 | SQLite 保证 | store + repo 编解码 | **采用** |
| 每个 Agent 一个 JSON 文件（像回执） | 做不到：部署预留要「读配置 + 读状态 + 写状态」、编辑要「读状态 + 写配置」、HA 完成要「写日志 + 写配置」 | 需要两阶段提交/补偿，新增崩溃窗口 | 大，且要新锁层级 | 否 |
| 第二个 SQLite 文件（ATTACH） | 本库是 `journal_mode=DELETE`，ATTACH 跨库提交靠 super-journal 可以原子，但每个连接都要 ATTACH | 备份/恢复/导入快照要同时处理两个文件且保持同一时刻，冷备份漏一个就不一致 | 中 | 否：没有比同库表多出任何好处 |

需要跨「配置 + 状态」原子的地方（全部已在一个 `EditAgents`/`UpdateSystem` 回调里，拆分后仍是一个事务，**写入方代码不变**）：
部署预留（读 HA 设置/启用/服务 + 读未完成部署/升级/联动忙 → 写 `preparing`）；Agent 编辑与换绑（读忙状态 → 写配置）；
`dnsha` 完成（写日志 + 写 `oxidns_ha`）；`dnsfollow` 成功（写状态 + 写 `upstream_signature`）；删除 Agent（删配置 + 删全部状态行）；
重新注册（新记录继承旧记录的历史）。「部署成功 + 配置版本记录」（`deployments`、`latest_deployment`、`applied_content`、
`config_version/revision`、`pushed_at`）现在全部归状态，同一次状态提交，天然原子。

### 2.3 接口（Go）

```go
// package store
type Head struct {
	StoreID    string
	Generation int64 // 配置行变化的提交才 +1（现语义）
	CommitID   string
	State      int64 // agent_state 行变化的提交 +1
}
// SameConfig 只比较配置部分；给逐个证明「不读状态成员」的缓存用。
func (h Head) SameConfig(o Head) bool

type StateRow struct{ AgentID, Member string; Value json.RawMessage }
// Commit 在一个事务里写配置差异和状态差异；只写字节变化的行；两部分各自决定是否推进代号。
func (c *Conn) Commit(target *Document, state []StateRow /* 完整目标集 */) (Head, Change, error)

type Change struct{ Config, State bool }

// 迁移框架（o3 3.7 已规定，首次实现）
type Migration struct {
	From, To int
	Up       func(ctx context.Context, c *Conn) error
}
```

```go
// package model
// AgentStateMembers 是归为「Agent 状态」的成员路径（唯一清单）。
var AgentStateMembers = []string{"deployments", "latest_deployment", "applied_content", "config_version",
	"config_revision", "pushed_at", "version", "latest_upgrade", "rule_sync.state", "dns_follow_state",
	"dns_follow_job", "oxidns_ha_state", "oxidns_ha_publication", "ipv6_verifications",
	"status", "last_heartbeat", "service_status", "system_metrics", "cpu", "memory", "disk", "network",
	"restored"} // restored：恢复标记（2.5.1），只存在于状态表
```

```go
// package repo（内部）
// splitAgents 把编码好的 agents 行拆成「配置行字节 + 状态行」；mergeAgents 反向。
// 只拆有可证明 id（agentIDProven）且 id 首次出现的条目；其余条目（无 id、重复 id、非对象）原样留在配置行。
func splitAgents(row json.RawMessage) (config json.RawMessage, state []store.StateRow, err error)
func mergeAgents(config json.RawMessage, state []store.StateRow) (json.RawMessage, error)

// 提交后通知（在释放 L3 之后调用，回调不得阻塞）；repo 不能 import lifecycle，所以是回调而不是 Signal。
type ChangeKind uint8 // ChangeConfig | ChangeState
func (r *Repository) OnCommit(kinds ChangeKind, fn func()) (cancel func())
```

读路径：`readEntry` 加载 `fields` 与 `agent_state`，合并后再走现有的 `system.agents` 解码（**同一套成员解码器**，所以 T1b 的
`stored:"gate"`、`failClosed`、`secret` 规则对搬出去的成员照样生效：存成 `"applying"` 的 HA 日志仍读作进行中、锁仍在）。
写路径：事务提交时现有逻辑编码 `system.agents` → `splitAgents` → `store.Commit`。删除 Agent 时状态目标集里没有它 → 行被删。

钩子：`ConfigurationCommittedRecords`（WebDAV 等）只在 `Change.Config` 时调用；`OnCommit` 按种类调用。

### 2.4 缓存与派生数据的影响

默认：所有 `head ==` 比较包含 `State`，行为与现在相同（状态变化也让它们失效）。逐个改成 `SameConfig` 的条件是**能证明缓存值不读
任何状态成员**，每个改动配一条「改状态后缓存仍命中、改配置后失效」的测试。

| 缓存 | 读状态成员吗 | 键 | 说明 |
|---|---|---|---|
| repo 文档缓存、`agentsCache`（借用的注册表） | 是（合并视图） | 完整 Head | 不变 |
| R4 Agent 视图缓存 `api/agents/agent_view_cache.go` | 是（部署、升级、HA 摘要） | 完整 Head + 心跳指纹 | 不变；收益来自「状态提交不再让其它配置缓存失效」 |
| R6 Agent 凭据 `AgentRecordCredentialsOf` / `GenerationCredentials` | 是（历史里可能带凭据，`dns_follow_job.candidate_settings`、HA 日志捕获的配置） | 完整 Head | 不变；状态行仍参与凭据收集（同一代） |
| 配置凭据 `ConfigCredentials` / `ConfigWithCredentials` | `ConfigWithOxiDNSAgents` 投影目前复制整个 `rule_sync`（含 `state`） | → `SameConfig` | 先把投影改成只复制 `rule_sync.settings`（核对 OxiDNS/路由器读取方只用 settings），再换键 |
| 通知：路由器签名 `notifications.routerSignatures` | 同上（经 `ConfigWithOxiDNSAgents` 编码） | → `SameConfig` | 现在每次规则同步状态变化都会改路由器签名并重算；改后不会。需核对「待发布」提示不依赖它（预期是修正） |
| 通知：部署失败等 Agent 类通知 | 是 | 完整 Head | 不变 |
| `provider/response.go`、`api/subscriptions`、`api/aggregations`、生成器缓存 | 否 | → `SameConfig` | 逐个确认后换 |
| `rulesync` comparison（head + 回执 + 规则目录） | 读 `applied_content` | 完整 Head | 不变 |

WebDAV 自动备份：现在靠 `webdav/changes.go` 的 `agentRuntimeFields` 清单过滤，清单漏了 `rule_sync.state`、`dns_follow_state`、
`dns_follow_job`、`oxidns_ha_*`、`ipv6_verifications`，所以这些状态变化**会触发自动备份**（HA 成对发布一次就排十几次备份，
被 1 s 去抖合并）。改后：只有 `Change.Config` 才进入判断，清单删掉（行为变化登记 behaviour-changes，是修正）。

### 2.5 迁移（v1 → v2）

触发：`repo.OpenDefault` 打开 `storage_version = 1` 的库（Python 写的、或现在的 Go 写的）。顺序（o3 3.7）：

1. 持有 `.config.lock`、任何 Startup 之前。
2. `store.BackupTo` → `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3`，`PRAGMA integrity_check` = ok。**备份失败则拒绝启动**并给中文提示
   （磁盘满/目录不可写），不在没有备份的情况下改库（D4）。
3. 一个 `BEGIN IMMEDIATE` 事务：建表；读 `system.agents` 行；`splitAgents`；写状态行（revision 1）；重写 agents 行；
   `state_generation = 1`；`generation` 不变（配置语义没变，避免下游误以为配置被改）；`storage_version = 2`；提交。
4. 记一条 INFO 日志：迁移了几个 Agent、几行、备份文件名（不含值）。

性质：
- **原子**：步骤 3 是单事务，断电/`kill -9` 后要么 v1 原样（下次启动重做，备份文件按时间戳另存一份）要么 v2 完整。
- **幂等/可重入**：只在版本为 1 时运行；`splitAgents(mergeAgents(x)) == x`、对已拆分的数据再拆是空操作（单测 + fuzz）。
- **不丢数据**：状态成员按**原字节**搬（不经过类型解码，错类型值也原样保留）；无 id / 重复 id / 非对象条目整条留在配置行；
  `agents` 行不是数组或不是合法 JSON → 不搬、原样保留、仍升版本并告警（读法与现在相同）；`rule_sync` 不是对象 → 整个留在配置行。
- **旧值保留**：不在配置行里保留副本（两份真值会在下一次写时分叉，且旧 Go 代码已被版本号挡住，副本没有读者）。旧值保留在
  步骤 2 的备份文件里，保留到下一次迁移（o3 3.7）；切换计划 A.3 的冷备份另外有一份。
- **兼容**：
  - Python 时期数据：就是 v1，正常迁移。Python 读不了 v2（`StoreUnsupported`），与 o14 切换计划「数据只恢复到 Go、不退回 Python」一致。
  - 旧 Go 二进制：看到 v2 拒绝打开（现有 `StoreUnsupported` 文案），不会把状态当成「没有」再写回。生产上还没有 Go，影响只在测试/实验室。
  - 旧 Agent：协议不变，无影响。遗留 `runtime.sqlite3` 导入走 `EditAgents`，自动落到新位置。
- **导入（ImportAllJSON / 首次向导导入 / WebDAV 恢复）**：文档里的 agents 若带状态成员（旧备份、Python 导出），按同一 `splitAgents`
  搬进状态表并**替换**这些 Agent 的状态行；不带（新格式导出）则按 2.5.1 处理。导入前的快照（`snapshot`）用 `BackupTo`，已含两张表。
- **导出**（D3，已裁定）：完整导出（下载、WebDAV）只含配置，不含状态成员；脱敏导出本来就把 agents 清空。「诊断导出」本批不做。

#### 2.5.1 用不含状态的备份恢复之后（D3 条件，fail closed）

问题：备份里没有部署历史、`applied_content`、HA 发布日志/`oxidns_ha_state`、联动状态。设备上实际跑的可能是任何时刻的配置，
尤其是备份恰好在「HA 启用进行中」或「HA 停用进行中」时生成：配置里的 `oxidns_ha` 还没落成（或还没撤掉），而设备上 keepalived
已经配好（或还在）。如果把「没有日志」读成「没有进行中的发布」，配对锁会解开，可能对一台仍持有 VIP 的成员单独发布或改地址。

规则：
1. **恢复标记**：导入一个不含状态成员的文档（新格式备份）时，在同一事务里给每个导入的、有可证明 id 的 Agent 写一行状态
   `restored`（`{"at": <ISO 时间>}`）。导入旧格式（带状态成员）不写标记——那是现在的行为，状态随备份恢复。
2. **标记存在时 = 运行状态未知，按 fail closed**：
   - HA 配对：已开启的 `oxidns_ha` 设置随备份恢复，`HAEnabledModel` 照旧锁住编辑、换绑、删除并强制成对发布；不改 `HAActiveModel`
     （实现时发现：把带标记的成员读作「发布进行中」会让成对发布自己的冲突检查也拒绝，唯一出口被锁死，见第 7 节）。
   - 所有带标记的 Agent：受保护字段编辑与换绑返回 409 `运行记录未包含在备份里，请先重新发布此 Agent`；规则同步、DNS 联动
     不以它为源或目标自动推送（没有基线，不自动写设备），状态显示 `blocked` + 同一提示。
   - **唯一出口是重新发布**：一次成功的部署（普通 Agent）或成功的成对发布（HA 成员，按 `oxidns_ha` 当前配置走成对流程，
     未启用 HA 的 OxiDNS 走普通发布）在同一事务里删除标记。删除 Agent 同时删除标记。不提供「忽略」按钮（本批）。
3. **界面提示**：Agent 列表/详情响应新增只读成员 `state_restored_at`（有标记时出现，否则不出现——
   不带标记时响应与现在逐字节相同）；Agent 页顶部横幅与卡片徽标显示「运行记录未包含在备份里，各 Agent 需要重新发布一次」。
   导入接口成功消息追加同一句（仅在写了标记时）。`doc/` 的备份恢复说明与截图同步。
4. 已登记为有意变化：新格式导出不含状态；恢复后需重新发布；恢复标记的锁定行为。

### 2.6 ① 后台循环：变化通知

通知来源：

| 来源 | 种类 | 发出点 |
|---|---|---|
| repo 提交（释放 L3 后） | `ChangeConfig` / `ChangeState` | `repo.OnCommit` |
| 心跳：首次在线、离线后恢复、`version`/`config_version` 变化 | `agents.OnPresence` | `observeHeartbeat`（释放 runtime 互斥锁之后） |
| 规则源刷新完成（规则缓存文件变化） | `refresh.OnRefreshed` | `refresh` worker 结束 |

注：离线是 120 s 不新鲜推断出来的，没有事件；依赖「成员掉线」的步骤（HA 等待丢失成员 `awaitingCompletion`）保留短间隔。

改造（`lifecycle.Periodic` 已有 `Wake *Signal` 且每轮重读 `Interval`）：

| 循环 | Wake 订阅 | 有进行中的工作 | 空闲 |
|---|---|---|---|
| P7 `dns-follow` | Config + State + Presence | 5 s（轮询设备上的部署） | 600 s |
| P8 `oxidns-ha-publish` | Config + State + Presence | 2 s（含 `awaitingCompletion`） | 600 s |
| P9 `ipv6-verification-scheduler` | Config + State | 5 s | 600 s |
| P10 `agent-rule-sync`（建议同批） | Config + State + Presence + Refreshed | 下一个 `RetryAt` / 进行中 worker：≤5 s | 60 s（远端规则内容的来源最难穷举，兜底取短） |
| P1 `scheduled-refresh`（建议同批，1.2） | Config（任务设置）+ 立即执行 | 到最近到期的时间，下限 1 s | 上限 600 s |
| P2 `router-rule-sync`（建议同批，1.2） | Config + 立即执行 | 同上 | 上限 600 s |

每个服务持有 `wake lifecycle.Signal` 和 `busy atomic.Bool`；`Round` 结束时设置 `busy`（= 现在传给 `IdleRounds.Done` 的 `!idle`），
`Interval` 读它。提交发生在一轮进行中 → `Signal` 留一个待处理通知 → 本轮结束立即再跑一轮（等价于 `IdleRounds` 的「开始前取 head」）。
注册与注销跟 Manager 生命周期走（`OnManagerInit` 注册 `OnCommit`，`OnClose` 取消），测试新建 Manager 不继承旧订阅。

删除：`agents/idle_rounds.go`（`IdleRounds`、`RecheckSeconds`、弱引用表、`float64` 秒时钟）、`dnsfollow`/`dnsha` Options 的 `Monotonic`。
600 s 兜底等于现在的 `RecheckSeconds`，所以最坏反应时间不变；有通知的情况从「≤5 s」变成「立即」。

唤醒风暴：HA 成对发布每 2 s 一次状态提交，会顺带唤醒 P7/P9 各一次；它们每轮只读借用的注册表（无设备 I/O 时 < 1 ms），可接受。
若实测有问题，再给 `Change` 加上变化的成员集合做过滤（本批不做）。

### 2.7 状态与并发

- 锁不变：状态写入与配置写入都在 L3 下、同一 SQLite 事务；L1/L2 顺序不变。状态提交仍是短事务，没有 I/O。
- 状态迁移表（部署、联动、HA、升级、规则同步）全部不变——只是落盘位置变了。崩溃恢复（o3 3.5「config」类状态）照旧：重启后由所属循环
  或下一次请求按记录的 ID 续上。
- `OnCommit` 回调在 L3 释放后同步调用，只做 `Signal.Notify`（非阻塞）；不得在回调里读写 repo。

### 2.8 对外不变 / 有意变化

不变：所有路由的响应字段与值（唯一新增：带恢复标记的 Agent 多出只读 `state_restored`/`state_restored_at`，见 2.5.1）（Agent 列表/详情、部署轮询、HA/联动/IPv6/规则同步/升级状态）、状态码、中文消息、Agent 协议、写设备语义、前端。

有意变化（登记 `behaviour-changes.md`）：
1. 存储布局 v2（Python 与旧 Go 不能打开）。
2. 完整导出/WebDAV 备份不含 Agent 状态成员（D3）。
3. 状态变化不再触发 WebDAV 自动备份。
4. 规则同步状态变化不再改变路由器签名（不再触发对应的重算）。
5. 用不含状态的备份恢复后，Agent 带恢复标记、按 fail closed 锁定，重新发布后解除（2.5.1）。
6. 后台循环有通知时立即反应（原来最多 2–5 s），空闲时不再每 2–5 s 读库。

---

## 3. 风险

| 风险 | 后果 | 对策 |
|---|---|---|
| 某个缓存按配置 head 作键却读了状态 | 显示旧状态、守卫读旧值 | 默认完整 Head；`SameConfig` 逐个开、逐个测；HA/联动/部署守卫都在写事务内读工作副本，不走缓存 |
| 合并读漏了 T1b 的 fail closed 规则 | 损坏的 HA 日志解锁配对 | 合并后走同一解码器；`TestReviewO18T1b*` 全部在 v2 上重跑；新增「损坏状态行」用例（非对象 HA 日志、`dns_follow_state` 字符串、gate 开关错类型） |
| 迁移中断/损坏 | 无法启动或丢历史 | 单事务 + 事先备份 + 故障注入（第 4 节） |
| 漏唤醒源 | 某步骤 600 s 才动 | 进行中保持短间隔；每个循环列出输入来源并对每个来源写「来源变化 → 下一轮立即运行」测试；P10 兜底 60 s |
| Python 对照测试（CrossCheck/Interchange、proddiff 基线迁移工具、n89 哈希语料）直接读 `agents` 行或把 Go 库交给 Python | 大量测试失败 | 测试侧加合并视图/降级副本（D5）；这是本批工作量最大的不确定项 |
| 唤醒过于频繁 | 空闲 CPU 回升 | o10perf 空闲 CPU 前后对比（PVE CT 由协调安排） |

---

## 4. 测试方案

迁移（`store`/`repo`，测试服务器）：
- **断电**：子进程执行迁移，在 `splitAgents` 写状态行的每个点之间设测试钩子暂停，父进程 `SIGKILL`；重开后断言「v1 字节逐行相同」或
  「v2 且合并视图 == 迁移前视图」，循环覆盖每个暂停点 + 随机点。
- **重复迁移**：v2 重开 N 次无变化（`generation`/`state_generation`/行 revision 不变）；`Up` 对已拆分数据再执行为空操作。
- **部分损坏**：agents 行非数组/非 JSON、条目非对象、无 id、重复 id、状态成员错类型、`rule_sync` 非对象、HA 日志为字符串/数字/`[]`、
  `dns_follow_state` 非对象 → 断言原字节保留、迁移前后 `DecodeStored` 结果与 API 投影相同、fail closed 守卫结论相同。
- **备份失败**：备份目录不可写/空间不足 → 拒绝启动、库仍是 v1。
- **fuzz**：`FuzzAgentStateSplit`（`mergeAgents(splitAgents(x))` 字节等于 x，且合并视图解码等于直接解码），60 s。
- **语料**：现有 Agent 存储语料（o10t2 参考语料、repo 夹具）全部迁移后对比合并视图；proddiff（gate 读快照副本）API 零新差异。

拆分后的读写：
- 状态提交不推进 `Generation`、配置提交不推进 `State`；混合提交两个都推进；删除 Agent 删除其状态行；重新注册继承历史。
- 并发：状态提交与配置编辑交错（`-race`），守卫读到一致结果（部署预留 vs Agent 编辑 vs HA 完成）。
- 导出不含状态成员；导入旧格式（含状态）后合并视图 == 导入文档；导入新格式后无状态。
- WebDAV：状态提交不排备份，配置提交排备份。
- 缓存：每个改成 `SameConfig` 的缓存一条正向/负向测试。

① 循环（`lifecycletest` 假时钟）：
- 提交后同一时刻跑一轮（不等 5 s）；空闲时 600 s 内不跑；进行中保持 5/2/5 s；一轮中途提交 → 结束后立即再跑一轮；心跳上线唤醒 HA 等待；
  `refresh` 完成唤醒 P10；P1/P2 在最近到期时刻运行。
- 负向对照：去掉某个 `OnCommit` 注册 → 对应测试失败。
- 全部受影响包 `-race`；相关 difftest（api/agents、dnsfollow、dnsha、ipv6、rulesync、repo CrossCheck）。

---

## 5. 工作量与拆分（每步可编译、测试通过、单独提交）

| 步 | 内容 | 量 |
|---|---|---|
| S1 | `store`：迁移框架（`Migration`、在线备份、版本检查）、`agent_state` 表、`Head.State`、`Commit` 同事务写两部分、`Change` | M |
| S2 | `model.AgentStateMembers` + 覆盖测试；repo `splitAgents`/`mergeAgents`、读写路径、v1→v2 迁移、故障注入与 fuzz | M |
| S3 | repo 导出/导入、恢复标记与 fail closed 锁（2.5.1）+ 界面横幅/`doc/`、提交钩子按种类、WebDAV 判定简化、`ConfigWithOxiDNSAgents` 投影只带 `rule_sync.settings` | M |
| S4 | 缓存逐个归类（`SameConfig`）+ 测试 | S |
| S5 | 测试适配：CrossCheck/Interchange 合并视图与降级副本、proddiff 基线迁移工具、受影响语料重录（只允许 metadata/存储布局变化） | M（不确定，见 D5） |
| S6 | ①：`repo.OnCommit`、`agents.OnPresence`、`refresh.OnRefreshed`；P7/P8/P9 改造并删除 `IdleRounds`；P10；P1/P2 | M |
| S7 | 文档：ARCHITECTURE 1.2/3.5/3.6/4.1、o3 表 2.4 与 3.5/3.7、behaviour-changes、o17 状态 | S |

合计 L（单人顺序做，约 4–6 个工作日的量；S5 取决于 D5）。HR：S1、S2、S6 需要对抗复核。

---

## 6. 裁定（协调，2026-11-23）

- **D1** 同意：同库新表 `agent_state`，与配置同事务同锁。
- **D2** 同意归类；硬要求：用户意图/开关留在配置，只有设备/后台产生的过程与结果进状态表（见 2.1 归类准则）。
- **D3** 同意：完整导出与 WebDAV 备份不含状态。条件：恢复后界面明确提示需要重新发布；HA 配对恢复后按 fail closed（2.5.1）；
  behaviour-changes 与 `doc/` 同步。
- **D4** 同意：迁移前强制备份，失败拒绝启动，中文提示说明原因与处理办法。
- **D5** 切换提交在所有清理之后才发生，本批必须保持现有门禁全绿。S5 用最省办法：比较整份存储文档的 Python 对照改为经合并视图
  （配置 + 状态）比较；不为将被删除的测试做深度重构。
- **D6** 同意：P10、P1/P2 同批（P10 兜底 60 s）；P6 单独小任务。
- 实现顺序：batch-42（T1d）合入后 rebase，按 S1–S7 频繁提交，每个提交可编译可测；迁移故障注入（中途断电、重复迁移、部分损坏、
  无 id/重复 id 条目）必须有。

### 6.3 后续（不在本批）

把 `model.Agent` 拆成 `AgentConfig` + `AgentState` 两个类型、写入方分别调用 `EditAgents`/`EditAgentState`：更地道，但要改约 300 处读写方，
且只在存储拆分稳定后才有意义。路由器 `last_release` 用同一机制搬出。

---

## 7. 实现记录（2026-11-23，分支 go-o17-runtime-state）

与上文设计不同或细化的地方（都按裁定的方向，影响写在 `parity/behaviour-changes.md` 的 O17 节）：

- **代码位置**：`store/state.go`（表、`Head.State`、`Upgrade`）；`repo/staterows`（拆分/合并，按原字节，`Split(Merge(x)) == x`）；
  `repo/agent_state.go`（读写挂接、`agentStateOnly`、恢复标记）；`repo/storage_upgrade.go`（备份 + 升级）；`repo/commit_watch.go`
  （`OnCommit`、`CommitWake`）；`agents/wake.go`（`Waker`、`OnPresence`）。`model.Agent` 增加唯一一个成员 `state_restored_at`。
- **升级时配置代号加一**（设计写的是不变）：升级改写了 `agents` 行的字节，按「行变就推进代号」的一般规则处理；启动阶段没有缓存，没有副作用。
- **系统 `_revision`**：`applySystemIn` 只在配置成员变化时推进（`agentStateOnly`），否则每次状态写入都会产生配置提交、方案失效。
- **HA 恢复后的 fail closed（2.5.1 细化）**：没有改 `HAActiveModel`。原因：若让带标记的 HA 成员读作「发布进行中」，成对发布自己的检查
  （`haDeploymentConflict` 等假定有日志）也会把唯一出口锁死。实际做法：已开启的 HA 设置随备份恢复，`HAEnabledModel` 继续锁住编辑/换绑/删除
  并强制成对发布；恢复标记另外阻止规则同步和 DNS 联动的自动推送，以及所有 Agent 的受保护编辑/换绑/注册变更；成功发布（成对发布的子部署
  也走同一记录路径）清除标记。删除 Agent 不受标记限制（HA 已开启的仍按原规则被 HA 锁住）。
- **恢复标记的界面字段**：只有 `state_restored_at`（没有另加布尔 `state_restored`），前端按它显示横幅与卡片标记。
- **诊断导出**：`repo.DiagnosticExportJSON` 已有（完整文档含状态），目前只给 Python 对照与诊断代码用，没有界面入口。
- **测试侧 Python 对照（D5）**：`repotest.PythonLayout`（交给 Python 前降级为 v1）、`DowngradeToV1`、`MergedLoad`、`MergedAgentsRow`；
  比较整份存储时屏蔽系统 `_revision`。门禁 difftest 阶段全绿（.64）。
- **P1/P2**：`refresh` 与 `routers` 在每轮结束时算下次到期，`repo.CommitWake` 跟随当前仓库注册配置提交唤醒；手动执行后唤醒 P1 重新计算。
