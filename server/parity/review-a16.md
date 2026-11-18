# A16 HR 复核（规则自动同步：两条路由、projection / isolation / candidate / snapshot / worker）

独立 Opus 复核，非作者；分支 `go-opus-review-a16`，起点 `go-port-a16@985c071c2`（作者 Codex，叠在
`go-opus-review-a14@53d43b577` 上，A14 尚未进 go）。无子 agent、无 push、无生产/真实 Agent/PVE 访问；测试只经
`rt` 固定在 `.32` 执行，新增用例只用合成数据和回环 fake。

对照：Python `backend/agents/rule_sync.py`、`rule_sync_projection.py`、`rule_sync_snapshot.py`、`rule_sync_store.py`、
`routes/agents.py` 1727–1748、`routes/tasks.py _agent_rows`、`routes/rules.py _pinned_withheld`、
`oxidns_ha_publish.pair/active`、`app.py start_agent_rule_sync`；设计 `a16-a18-sync-follow-ha.md` §2.2–2.8、3.1、3.4–3.7、
4.5；`o3-concurrency.md`（owner / Runtime / 取消）；作者交接 `progress/A16-handoff-zh.md`；`review-a17.md` 的问题类。

## 结论

- **high：0 条。未达到回退门槛（2 条 high）。**
- medium 1 条（候选生成被错误地强制下载，越过「规则库」关闭的作用域），已修。
- low 1 条（R5 假值 `enabled` 进入规则签名），已修；测试覆盖补充 5 项。
- 作者修的 3 个 Python 问题（`_publish` 已提交仍标 failed、GET 期间重绑定后仍 POST、HA enable 并发）均确认正确且完整。
- 共享最终提交窗口（O3-6c）初次 HR 时登记为待复验；现已在 batch-5 集成树接入并通过 .32 复验，见第 4 节和交回报告末尾。

## 1. 发现

### medium

- **M1 — 结构发布把状态里的 `force_refresh` 传给候选生成（已修，`83fe462db`）。**
  Go `worker.publish` 以 `value(old.ForceRefresh)` 调用 `PrepareCandidate`；Python `_publish` 调 `self.prepare(agent, source,
  baseline, settings, deployment_id)`，从不传 `force_refresh`（只有 A18 的规则任务对 OxiDNS 显式强制）。Go 的 `failure()`
  每次失败都写 `force_refresh: true`，离线时 running 也会补上，防抖状态再把它带下去，所以**任何一次失败之后**的下一次
  结构发布都被强制：
  1. OxiDNS 只开「设备规则」、关闭「规则库」时，`rules/rule_*` 规则文件不再按原字节固定，而是重新物化/下载——设备拿到了
     用户没有选择自动同步的规则库新内容（Python `test_prepare_candidate_library_off_never_refetches_remote_rule` 守的正是这一条）；
  2. 打开规则库时关闭未变文件的复用，每次都重新拉取全部规则集；
  3. Mihomo 的 forced 路径是用 OxiDNS 的 `RulesetDownloads` 从 Mihomo 来源算出来的（Python 的 Mihomo `downloads` 恒为空）。
  证据：`TestStructurePublicationIgnoresStoredForceRefresh`（先写入一次失败留下的状态，再做设备规则结构修改；修复前失败
  `stored force_refresh reached candidate preparation (Python passes none)`，修复后发布 succeeded、prepare 只调一次且 force=false）。
  修复：worker 固定传 `false`；`PrepareCandidate` 只对 OxiDNS 计算 forced 路径（保留给 A18）。`isolation` 只校验角色，
  拦不住这一越界，所以必须在候选生成处修。

### low

- **L1 — 规则签名只把 JSON `false` 当作停用（已修，`83fe462db`）。**
  Python `_source_rule_state` / `_rule_state` 用 `item.get('enabled', True)` 的真值，`null`、`0`、`""` 都算停用，生成器同样如此；
  Go `active()` 只认 `!!bool false`。对存了假值 `enabled` 的规则（R5 数据），修改它在 Go 判成 structure（Python none），
  触发一次无意义的只重载发布，OxiDNS 原生模式还会进入原生投影并可能报出 Python 不会报的拒绝。修复：改用已导出但未被
  使用的 `oxidns.RuleEnabled`（与生成器同一规则）。证据：`TestFalsyEnabledRuleIsNotAnEffectiveChange`（修复前 3 个值都报
  `kind structure, Python none`）以及真实 Python 预言机新增的 6 个 `falsy-enabled` 用例（修复前 Mihomo 3 个失败）。

### 测试覆盖补充（无代码缺陷）

- **T1 — 指定上游 withheld 没有被 Python 差分覆盖。** `TestProjectionAgainstPython` 只比较 withheld 的长度，且 oracle 没有
  任何指定上游用例（结果恒为空）。`testdata/reference.py` 新增 template/native × library/rules/both 的 withheld 用例，测试改为
  逐个比较 id。修复前后均通过（实现无误）。
- **T2 — 设计 §3.4 要求的 `TestMain` 锁序断言缺失。** 新增 `main_test.go`（`lockx.Violations()==0`），整包 race 通过。
- **T3 — 作者的真实 Runtime 关机用例只覆盖 Mihomo 内容刷新。** 新增 `TestShutdownDuringStructurePublicationConverges`：
  结构发布读取设备快照时停机 → 停机后 DB 字节不变（不写失败记录、不写状态）；新 Runtime + 新 owner 一轮后 A01 把中断的
  准备记为 failed，状态 `failed 规则热加载未完成…` 带 retry_at，候选从未上传。
- **T4 — R5 形状。** `TestWrongTypeRuleSyncMembersReadAsAbsent`：`rule_sync` 为 null/列表/字符串、`settings` 为列表、`state`
  为 null/列表/成员错类型时，Status、worker 一轮、Configure 都不报错。
- **T5 — 路由差分边界。** Flask 差分新增 text/plain、多余成员、null 成员、字符串成员、空 query `?` 五个场景，逐次比较 DB/文件。

## 2. 已核对、无问题的部分

- **两条路由**：`auth.Require`；PUT 先查 query/长度（400 `规则同步设置无效`）→ 404 → 服务类型 400 → body 400 → HA 开启中 400 →
  配对事务；非 JSON 内容类型按 `get_json(silent=True)` 的 None 处理；`no-store`；不信任 `X-ConfigFlow-Profile`；状态只公开
  status/message/updated_at 并按 leader 凭据脱敏。Configure 经 `RunDetached`，断连后完成、关机后不写（作者用例）。
- **作者修的 Python 问题**：
  1. `_publish` 失败记录改为 A01 `OnlyUnsubmitted`，且只在 ctx 未取消时写；所有错误出口都经同一 defer；已提交任务交给轮询，
     重启后由 A01 中断分类收敛（T3 证明）。正确且完整。
  2. OxiDNS GET 状态后重新绑定：首次 POST 与 `RULE_UPDATE_TARGET_UNKNOWN` 回退 POST 前都重查 `unchanged`；Mihomo 每个
     provider PUT 前也重查；`resumeNative` 无 POST。没有遗漏的设备写点。
  3. HA enable 并发：单机 `persistSync` 与配对 `configurePair` 都在事务内重查 `pairEnabling`（配对还比较 `oxidns_ha` 不变）。
- **worker 逐行对照 `_run/_refresh/_publish/_state`**：顺序、离线 pending 保留 native_task/key/force_refresh、版本门槛、未完成部署的
  running/pending、自身部署成功后重读、`resuming`、比较记忆（inputs、600 s、时钟回拨、unsafe/none/withheld）、防抖 5 s、
  native 任务续接（运行中不重发、库关闭只推进基线、成功+同 key+unchanged 才推进、失败 retry）、`_refresh` 编辑期间重查投影、
  `_publish` 预留/拒绝/结果映射。task-status 只在进入 succeeded/failed 时记一次。
- **A17 问题类逐项**：M1 类（`[]byte` 被编码成 base64）——包内所有比较都走 `json.RawMessage`/`CanonicalDigest`/`yamlx.Equal`，无
  `[]byte` 直接 `json.Marshal`；M2 类（提交后重复归属检查）——推进基线与发布后都没有二次归属检查，与 Python 一致；L1 类（他人
  凭据进记录）——失败记录、状态失败文本都是常量，设备返回的 native 消息经本机凭据 + 外部密钥脱敏，日志只记错误类型。
- **关机 / 断连 / I/O 取消**：每个设备写（provider PUT、native POST、`deploy.Publish`）、`AdvanceContent`、状态/设置事务、
  task-status 前都查 `ctx.Err()`；worker 失败路径在 `held.Err()!=nil` 时不写；`FinishDeploymentPreparation` 只释放内存锁。
- **snapshot**：AgentOrigin、Bearer、GET 空 body、`Accept: application/gzip`、(3,90)、无代理无重定向、压缩/展开/成员数/路径/角色/
  摘要/主配置版本逐层校验，不落盘（作者 60 s fuzz）。超限优先于读错误，与 Python 顺序一致。
- **projection / isolation / candidate**：对照 Python 逐段核对 `_rule_dns`、`_merge_items`、`_hydrate`、`_native_projection`、
  `_sequence_skeleton`、`_check_helpers`、`_isolate_oxidns_document`、`isolate_rule_bundle`、`prepare_candidate`（除 M1 外一致）。
- **C07 row**：字段、`blocked→skipped`、succeeded/failed 覆盖历史、running、pending 的 state_message、名称缺省与 `_agent_rows` 一致。
- **C32 PinnedWithheld**：只注册非 nil 钩子；按实际默认配置空间解析、复用同一投影、只对开启同步且有基线的 OxiDNS 计算。
- **R5 / null**：`settings` 严格 `is True`；状态成员 null/错类型读作缺失（T4）；`deployment_id` null 等价缺失；版本缺失/不可解析即阻止。

## 3. 其他观察（不改）

- `saveState` 用未脱敏的新消息和库里已脱敏的旧消息比较；若某条无时间字段的状态消息会被脱敏改写，就会每轮重写一次。目前
  这类状态的消息都是常量（会变的设备消息都带 `retry_at`），没有实际触发路径。
- Python `_state` 判断是否记日志时用不带「；指定上游需手动发布」的消息比较，有 notice 时每次状态改写都会重复记日志；Go 用
  带 notice 的消息比较，只在真正变化时记——仅日志差异。
- `rule_sync.state` 被存成列表时，Python GET 返回 `state: {}`，Go 返回「等待检查规则变化」；只在数据损坏时出现。
- HA 配对的规则同步（`_refresh`/`_publish` → 配对发布）在 A18 注册 pair publisher 之前按 A14 默认 fail closed，会周期性
  记为 failed；属 A18 依赖，不在本次范围。

## 4. 集成复验（初次 HR 待办，现已完成）

- **O3-6c 共享最终提交窗口**：`notifications.TaskStatus(...).Record` 无 ctx，SQLite 最终提交前的 ctx 检查由共享层负责。A14 与
  batch-4 合入 go 后 rebase，把 `saveState` 的 task-status 写换成 `RecordContext(owner ctx)`，在合并树补跑：本包定向 race（含
  `TestMain` 锁序）、关机后 task-status 不写、`TestRuleSyncHTTPAgainstFlask`，并确认 CFG-11 行 revision 保真。
- A14 合入后同时复验 T3（依赖 A14 的 `PublishDirect`/A01 中断分类）与 M1 用例。

2026-11-18 集成补记（作者 Codex，独立 HR 结论不变）：`go-a16-integrate` 已叠到 `batch-5@0d43e3f1`，没有重复旧 A14 提交；终态写入传 owner ctx 给共享 `RecordContext`。真实 Runtime 在 task-status 锁等待期间关机后不写任务状态/notifications；断连后成功和失败终态均完整记录一次。负控旧 Record 两个 shutdown 子例失败。完整 rulesync race（含 M1/T3/Python）172.908s；20 场景 Flask HTTP/DB/文件差分16.187s；O3-6c taskstatus/repo、CFG-11 行 revision、server race 与 vet/gofmt 均通过。A16 可标 `verified`；L3 实机依赖仍留给后续批次。确切命令与原始最后输出见 [A16-handoff-zh.md](progress/A16-handoff-zh.md) 的 batch-6 集成增量。

## 5. 需要 L3 实机验证的项目（设计 §6.7 细化）

1. Mihomo 只改规则库内容：真实 HTTP rule-provider 按名热刷新，快照校验后基线推进；非 HTTP provider 走只重载发布。
2. OxiDNS 内容变化：`rules/update` 带 paths 的选择性下载；旧 Agent 不支持 paths 时回退全量；`RULE_UPDATE_TARGET_UNKNOWN` 回退。
3. 结构变化（新规则/新规则集/DNS 指定上游）：`reload_only` 包只改规则相关文件和插件，节点/DNS 草稿/订阅字节不变；
   未发布的指定上游被保留并在状态中提示「指定上游需手动发布」。
4. **M1 回归**：设备只开「设备规则」时，先制造一次失败（例如短暂断网让快照读取失败），恢复后做一次结构修改——设备上
   规则库文件字节必须保持不变。
5. 面板 SIGTERM / `kill -9` 分别落在：provider 刷新中、native 任务受理后、结构发布读快照时（T3）、上传被接受后；重启后
   不重复 POST/上传，状态收敛。
6. 设备离线、Agent 版本低于 1.8.30-go、设备上手动下载正在运行时的状态与重试节奏（5 s / 60 s / 600 s）。
7. HA 主备的 leader 选择与配对规则发布依赖 A18，留到 A18 复核后的 L3。

## 6. 运行记录（全部固定在 .32，`RT_HOST=sunbroke@192.168.0.32`）

修复前失败（`83fe462db` 之前的工作树）：

```
--- FAIL: TestProjectionAgainstPython/mihomo/falsy-enabled/None      kind structure want none（0、'' 同）
--- FAIL: TestStructurePublicationIgnoresStoredForceRefresh          stored force_refresh reached candidate preparation (Python passes none)
--- FAIL: TestFalsyEnabledRuleIsNotAnEffectiveChange                 enabled <nil>/0/"": kind structure, Python none
```

最终（`a36476ea2`，开始 load 0.86，结束 1.32）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/agents/rulesync internal/api/agents)"; go vet ./internal/agents/rulesync/... ./internal/api/agents/... ./internal/server; go test -race -p 4 -count=1 -timeout=20m ./internal/agents/rulesync; go test -race -count=1 -run "^TestRuleSyncHTTPAgainstFlask$" ./internal/api/agents/difftest; go test -race -count=1 ./internal/server'
```

```
ok  	configflow/internal/agents/rulesync	87.674s
ok  	configflow/internal/api/agents/difftest	8.506s
ok  	configflow/internal/server	1.434s
```

未跑完整 gate / 全仓 race / proddiff（按协调者要求）；解析代码未改，作者的 60 s 快照 fuzz 仍有效。

## 7. 提交

```
83fe462db go(rulesync): keep stored force_refresh out of candidate preparation; falsy enabled excludes rules
55b171f23 go(rulesync): assert lock order and prove shutdown during structural publication
82e334ae9 go(rulesync): extend the Flask route differential with body and query edge cases
a36476ea2 go(rulesync): R5 regression for wrong-type rule_sync members
```
