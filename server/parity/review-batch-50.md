# batch-50 HR 复核（O17 ② Agent 运行状态移出配置 + ① 后台循环改通知驱动）

复核者：新的 Opus 会话，没有参与实现（作者另一个 Opus，分支 `go-o17-runtime-state`，基于 `go@1e43a4766`，
提交 `9ac42933f`..`165fa44ce`）。只用合成数据和 loopback；没有接触生产；没有推送；没有开子 agent。
测试只在 .32 / .64 跑；空闲 CPU 在 PVE .6 CT 240 上用离线快照副本测（快照内容没有进入代码、测试或本文）。

## 结论

**0 条 high，2 条 medium（1 条已修复，1 条需要裁定），4 条 low（不改代码，记录在案）。**
迁移、同事务提交、恢复标记的主路径和循环唤醒都核对过，没有发现会丢配置或让面板起不来的问题。

修复与新增用例：

| 提交 | 内容 |
|---|---|
| `2cf0cfab7` | M1 修复：升级前备份按内容指纹校验，复用内容相同的旧备份，备份后 fsync 目录 |
| `42f3eb4c4` | 复核用例（repo）：大库迁移逐字节相同、磁盘满、备份目录只读、反复失败只留一份备份、外来备份不被复用、状态/配置提交并发 |
| `0748c0138` | 复核用例（refresh）：按下次到期休眠、配置提交立即唤醒 |
| `ee3190d96` | 磁盘满用例的子进程登记（pyref B-037） |
| 本文所在提交 | 本报告 |

## 1. 迁移（v1 → v2）

核对项与结果：

- **顺序**：`openLocked` 在 `.config.lock` 下、`Check` 之后、任何读取之前调用 `upgradeLocked`；先备份并校验，失败返回
  `msgUpgradeBackupFailed`（拒绝启动、数据库未改），再在 `store.Upgrade` 的一个 `BEGIN IMMEDIATE` 事务里建表、改写 `agents`
  行、插入状态行、写 `storage_version=2` 与 `state_generation`，最后提交。任何错误（含提交前的 ctx 取消）都整体回滚。
- **原子性**：journal_mode=DELETE + 单事务。作者已有：每个状态行前注入错误（14 个点）、子进程在第 0/7/13 行处 SIGKILL（断言留下
  hot journal、重开后 v1 指纹不变、再次启动升级成功）。复核补充：
  - `TestReviewB50DiskFullDuringUpgrade`：子进程设 `RLIMIT_FSIZE` = v1 文件大小并忽略 SIGXFSZ，备份能写下（同大小），升级让库从
    315 392 字节长到 434 176 字节而失败 → 拒绝启动、提示含「数据库未改动」、指纹与版本不变；去掉限制后再启动成功，合并视图不变。
  - `TestReviewB50ReadOnlyBackupDirectoryRefusesStart`：`backups/` 只读 → 拒绝启动、v1 不变；改回权限后升级成功。
- **合并视图逐字节相同**：`TestReviewB50UpgradeLargeRegistryKeepsTheView` 用 5 个 profile、60 台 mihomo（每台 5–24 条部署历史、
  规则同步设置与状态、升级、IPv6、Python 指标、未声明成员夹在状态成员中间、Python `", "`/`": "` 分隔符）、16 台 OxiDNS 组成 8 对 HA
  （日志为 `"applying"`/`7`/`[]`/`null`/进行中/成功/失败/`{}`）、DNS 联动绑定 + 状态（含字符串/数字损坏值）+ job，以及无 id、
  重复 id、空 id、数字 id、`rule_sync` 非对象、`rule_sync.state` 为文本、带引号的 id、非对象条目（`7`/`null`/`"text"`/`[]`）。
  断言：升级后完整类型化文档（`model.Marshal(整份 Document)`）与 v1 读出的逐字节相同；重开后仍相同；每个状态行都是 v1 行里
  某个成员的原字节；配置行再拆分为空操作。
- **幂等/重复启动**：作者用例覆盖重开 3 次指纹不变。
- **损坏条目**：fail closed 的 HA 日志（字符串 `"applying"`）迁移后仍读作进行中（作者用例）；上面的大库用例覆盖其余损坏形态。

### M1（medium，已修复 `2cf0cfab7`）升级失败时每次启动都写一份完整备份；备份校验偏弱；备份目录未 fsync

- 触发：升级因确定性原因失败（例如某个手工编辑过的库让 `targetState` 报错），容器按 `restart: unless-stopped` 反复重启。
  每次启动 `upgradeBackup` 都新建 `configflow-v1-<时间>.sqlite3`，生产库几 MB 到几十 MB，几小时就能写满数据盘，最后连面板之外的
  服务一起受影响。作者自己的 `TestO17UpgradeStoppedAtEveryRow` 实际就留下了 14 份备份。
- 校验只做 `quick_check` + `storage_version`，不能证明备份内容就是源库（设计写的是 integrity_check）。
- 备份文件 fsync 了，但 `backups/` 目录没有 fsync：升级提交后紧接着断电，目录项可能丢，留下 v2 库却没有 v1 备份。
- 修复：先算源库内容指纹（`store.Fingerprint`：meta、sections、fields、agent_state 全部行），已有同版本备份若通过 Check 且指纹相同
  就复用；新备份必须通过 Check、版本相同且指纹与源库相同，否则删掉并拒绝启动；写完 `fsx.SyncDir(backups)`。
- 用例：`TestReviewB50FailingStartsReuseOneBackup`（5 次失败启动只有 1 份备份，仍是 v1）、`TestReviewB50ForeignBackupIsNotReused`
  （内容不同的旧备份不会被当成本库备份）。原有 O17 用例全部通过。

### L1（low，记录）同一条目里重复的成员名会让升级失败

`staterows.Split` 把对象里的每个状态成员都搬成一行；手工编辑出的重复键（例如两个 `"deployments"`）会产生重复状态行，
`targetState` 报「Agent 状态行 …重复」→ 升级失败、拒绝启动（库不变，有备份）。Python `json.dumps` 和 Go 编码器都不会写出重复键，
只有手改数据库才会出现；修复 M1 之后反复重启也不会再堆备份。建议切换后若有人报这类错误，按提示从备份恢复或手工去重。

## 2. 恢复标记 fail closed

逐条核对「恢复了不含运行记录的备份之后，自动把旧 DNS/规则推到设备」的路径：

| 路径 | 结论 |
|---|---|
| `dns-follow` 循环 | 每个绑定在任何设备 I/O 之前检查源和目标的标记，拒绝并写 `failed` + 提示；恢复后 `dns_follow_state` 为空，不会走 `resume`。拒绝写状态有去重（`AppendOperationEvent` 同内容不追加、`saveState` 相等不提交），不会自我唤醒成忙循环。 |
| `oxidns-ha-publish` 循环 | 只推进已有日志的成对发布；恢复后没有日志，无事可做。已开启 HA 的配对由 `HAEnabledModel` 锁编辑/换绑/删除并强制成对发布。 |
| `agent-rule-sync` | worker 在选出 HA leader 之后、任何比较/推送之前检查标记 → `blocked` + 提示；`advancePair` 只对有进行中日志的成员。保存状态同样去重。 |
| `router-rule-sync` | 不在本设计范围（路由器回执是数据目录文件，不在备份里；行为与 Python 相同）。 |
| `ipv6-verification-scheduler` | 只做校验，不写设备配置。 |
| 旧式推送 `PushLegacyConfigModel` | 用户显式操作，成功后清除标记（出口之一）。 |
| MCP | 编辑走 `EditAgent`（被拒），发布走同一部署路径（出口）。 |
| 其它写 Agent 的 `EditAgents` 调用方 | `deploy/adapter`（发布本身）、`installer/capability`（新建 Docker HA 成员）、`gateway/installation`（绑定安装记录），都不改受保护字段。 |

**唯一出口不会被锁死**：部署预留不看标记；普通部署和成对发布的子部署都经 `RecordDeploymentModel` → `applyDeploymentUpdate`，
`succeeded` 且是最新部署时清除标记（同一事务）。不改 `HAActiveModel` 的做法是对的：若把带标记的成员读作「发布进行中」，
`haDeploymentConflict` 会拒绝成对发布自己。停用 HA 只看 `oxidns_ha` 配置，也不受标记影响。

### M2（medium，需要裁定，未改代码）启用失败遗留的 keepalived 只记在 `oxidns_ha_state` 里，新格式备份恢复后无法清理

- 启用 HA 时先对两台设备 `PUT` keepalived，再在一个事务里写 `oxidns_ha`（配置）和 `oxidns_ha_state`（状态）。启用失败且回滚
  设备也失败时，`recordLeftover` 只写 `oxidns_ha_state = {status:"failed", members:[两台]}`，`oxidns_ha` 不写——这是「设备上还有
  keepalived/VIP」的唯一记录，`停用 DNS 高可用` 靠它的 `members` 找到两台去清理（`configure.go` 停用分支）。
- D3 之后导出不含 `oxidns_ha_state`。只要遗留没清理，之后任何时候做的备份恢复后：两台是普通 OxiDNS（带恢复标记），
  停用返回成功但什么都不清理；标记的出口「重新发布」对其中一台单独进行，设备上的 keepalived 仍在、可能持有 VIP。
  Python 时期完整备份带这个成员，恢复后能清理，所以这是 D3 新引入的缺口。（启用过程中、提交前的那几十秒里两者都还没写，
  Python 也一样，不算新问题。）
- 触发要两个条件：启用失败且设备回滚失败（少见）+ 用新格式备份恢复。后果是设备上残留 VRRP 实例，可能与新的 HA 配置或别的设备抢 VIP。
- 建议（需协调裁定）：a) 导出时保留 `oxidns_ha_state`（或只保留 `status=failed` 的 `members`），导入时照常进状态表——它记录的是
  「设备上有什么」，与恢复后需要清理的东西一一对应，体量很小；b) 带标记的 OxiDNS 发布前查询设备 keepalived，有则拒绝并提示先停用；
  c) 接受，在 `doc/` 恢复说明里写明「备份前先处理 DNS 高可用的失败提示」。倾向 a)。

## 3. 状态与配置同一事务、同一把锁

- 所有写入都经 `transact` → `transactLocked`（L3 `.config.lock` + 一个 SQLite 事务），`commitStore` 里 `splitRows` 后一次
  `CommitContext` 同时写配置差异和状态差异；读路径 `readEntry`/`agentRecords` 在同一个读事务里读 `fields` 与 `agent_state`。
  repo 之外没有直接读 `system.agents` 行或打开 store 的代码。
- 通知：`transact` 在释放锁之后，按 `Head` 比较得出种类，只有配置变化才调 `ConfigurationCommittedRecords`（WebDAV），
  `notifyCommit` 对每次有写入的提交都调用。没有发现「只写状态却不通知」的路径（唯一的写入口就是 `transact`；首次创建与
  升级发生在启动阶段，那时还没有订阅者）。
- 代号/Revision：状态行变化只推进 `Head.State`；`applySystemIn` 用 `agentStateOnly` 判断，只改状态成员时 `_revision` 不变。
  直接 `Revision++` 的其它路径（默认 profile、profile/shared 编辑、导入）都是配置编辑。ETag/缓存默认按完整 `Head`（含 State）比较，
  只会多失效、不会读到旧状态（设计 2.4 的 `SameConfig` 收益本批没有取，见 L4）。
- 并发：`TestReviewB50StateAndConfigurationCommitsRace`（`-race`）4 组写状态 + 4 组写配置各 15 次，3 个读者不停读注册表并断言
  `latest_deployment` 总在 `deployments` 里；结束时配置代号、系统 `_revision`、`Head.State`、两类观察者计数都正好各 60。

## 4. 循环唤醒

逐个循环列出原触发条件和现在的唤醒源：

| 循环 | 原来 | 现在 | 缺口 |
|---|---|---|---|
| `dns-follow` | 每 5 s 比较 head（`IdleRounds`），600 s 兜底 | 配置+状态提交、Agent 上线；有进行中工作 5 s；空闲 600 s | 无。源 Agent 发布完成是状态提交；目标忙时 `waiting` 保持 5 s |
| `oxidns-ha-publish` | 每 2 s 比较 head，600 s 兜底 | 配置+状态提交、Agent 上线；有日志或等待丢失成员时 2 s | 无。成员掉线没有事件，但等待期间本来就是 2 s |
| `ipv6-verification-scheduler` | 每 5 s 比较 head | 配置+状态提交（部署完成触发校验）；运行中 5 s | 无 |
| `agent-rule-sync` | 每 5 s 无条件 | 配置+状态提交、Agent 上线、规则源刷新完成；有 worker/pending/running/未到期重试时 5 s；空闲 60 s | 不经提交改变的规则文件（例如 Agent 自己下载的远端内容）最长 60 s，设计已接受 |
| `scheduled-refresh` | 每 30 s | 到最近到期时间（1 s–10 min）；配置提交、手动执行后重算 | 见 L2 |
| `router-rule-sync` | 每 30 s | 到最近到期路由器（1 s–10 min）；配置提交 | 见 L3 |

Agent 上线来自 `observeHeartbeat`（释放 runtime 锁之后、只在离线→在线时），版本/配置版本变化写状态行 → 状态提交。
手动「立即执行」走 `runResource(trigger != "schedule")` 唤醒 P1；规则源刷新完成经 `refresh.OnRulesRefreshed` → `rulesync.WakeLoop`
（`server.wireLoopWakes`，进程内一次）。复核补充 `TestReviewB50RefreshPacesByNextDueAndWakesOnCommits`：一轮后暂停 ≤ 下次到期
（300 s 间隔），改成 2 h 后为 `MaxPause`，循环在长暂停中被一次配置提交立即唤醒、不需要推进时钟。

### L2（low，记录）P1：本轮跑完仍「立即到期」的资源最长等 10 分钟

`pace` 只用「尚未到期」的资源算下次时间。若某资源跑完后仍立即到期（没写回执、也没有缓存基线，`NextDue` 返回 now），
原来每 30 s 重试，现在最长 10 min。正常的成功/失败都写回执（失败按退避），所以只在异常路径出现；而原来的 30 s 无限重试本身也不理想。

### L3（low，记录）P2：发布后捕获基线只写回执文件，首次检查最长推迟 10 分钟

路由器发布成功后 `capture` 写回执（`synced`、无 `CheckedAt`），这不是仓库提交，不会唤醒 P2；下一次检查在原暂停结束时（≤ 10 min，
原来 ≤ 30 s）。刚捕获的基线就是设备现状，首轮检查本来就不会改动设备；之后按 15–1440 min 间隔进行，影响可忽略。
「暂停」状态只由重新发布解除，同理。

### L4（low，记录）`SameConfig` 缓存与离线 Agent

- 设计 2.4 的「逐个把只读配置的缓存改成 `SameConfig`」本批没做，全部按完整 Head，安全但没拿到这部分收益（不是缺陷）。
- 规则同步开着的 Agent 离线时状态是 `pending`，`inFlight` 让 `agent-rule-sync` 保持 5 s（与原来相同），这种情况下空闲收益为零。

## 5. 空闲 CPU / RSS（PVE .6 CT 240，`pve.sh run --servers go`，30 min 空闲 + 路由负载，2026-11-23）

启动时 Go 在 CT 内把 v1 快照副本升级到 v2（真实形状的数据上跑了一次迁移），之后的数字：

| 空闲 | 本批 | go 77c4c4538（o10-pve-baseline 最后一节） |
|---|---:|---:|
| CPU 平均 | **0.42%** | 1.05% |
| 10 s 窗口中位 / p10 / p90 | **0.10 / 0.00 / 0.30%** | 0.40 / 0.20 / 0.60% |
| 5 分钟后稳态平均 | 0.20% | — |
| RSS 结束 / 最大 | **54 / 61 MB** | 90 / 129 MB |
| 分配速率 | 47 MB/min | 202 MB/min |
| GC 次数（30 min） | 213 | 262 |

尖峰：开头 70 s 内 9–13%（启动后第一轮各循环；goroutine 一度 140，随即回到 17–21），之后只有每 10 分钟一次 4–8% 的 10 s 窗口
（各循环 600 s 兜底同时到期）。两次测量之间还合入了其它批次，数字不能全归功于 O17，但没有唤醒风暴的迹象：稳态窗口中位 0.10%。
私有输出：.32 `~/cf-prod-snapshot/runs/pve-20261123-095551`（不提交）。

## 6. 验证（最后几行原样摘录）

```
# .64：受影响包 -race（store/repo/agents/refresh/routers/webdav/api/config/api/agents/server/lifecycle/model 共 29 个包）
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./... && go test -race -count=1 -p 6 ./internal/store/... ./internal/repo/... ./internal/agents/... ./internal/refresh/... ./internal/routers/... ./internal/webdav/... ./internal/api/config/... ./internal/api/agents/... ./internal/server/... ./internal/lifecycle/... ./internal/model/...'
VET-OK
29 × ok，0 FAIL
# 复核新增用例（-race）
--- PASS: TestReviewB50UpgradeLargeRegistryKeepsTheView / FailingStartsReuseOneBackup / ForeignBackupIsNotReused /
          ReadOnlyBackupDirectoryRefusesStart / DiskFullDuringUpgrade（v1 315392 → 434176 bytes）/ StateAndConfigurationCommitsRace
--- PASS: TestReviewB50RefreshPacesByNextDueAndWakesOnCommits
ok  configflow/internal/repo   ok  configflow/internal/refresh
# .64：fuzz
go test -run '^$' -fuzz '^FuzzSplitMerge$' -fuzztime 60s ./internal/repo/staterows/
fuzz: elapsed: 1m1s, execs: 384301 (0/sec), new interesting: 220 (total: 246)
ok  configflow/internal/repo/staterows 61.055s
# .32（.64 也跑过一次，同样通过）：CONFIGFLOW_DIFF difftest
GATE_ONLY=difftest scripts/gate.sh -p 8
DIFF PASS: 214 required entries ran and passed; 0 test skips
GATE STAGE PASS: difftest
# .32：proddiff
GATE_ONLY=proddiff scripts/gate.sh -p 8
self_check PASS, unique_differences 80, coverage_regressions 0
baseline: new=0 stale=0
GATE STAGE PASS: proddiff
# .32：preflight（vet 两个 tag + 全量非 race）
/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-50 -p 8
PREFLIGHT PASS
# .64：前端
cf-test typecheck → exit 0
cf-test frontend → Test Files 217 passed (217), Tests 3469 passed (3469)
# PVE .6 CT 240：空闲 CPU/RSS（见第 5 节）
RT_HOST=sunbroke@192.168.0.32 rt -- bash server/tools/o10perf/pve.sh run --servers go → pve-20261123-095551
```

第一次 preflight 失败在 `tools/pyref TestAuditRepository`（新用例的子进程未登记），`ee3190d96` 登记后重跑通过。
