# A17 HR 复核（DNS 联动：四条路由、DNSFollowRow、runner、boot loop、恢复与重试）

独立 Opus 复核，非作者；分支 `go-opus-review-a17`，起点 `go-port-a17@1560b84b0`（作者 Codex，叠在
`go-opus-review-a14@b6ce4ac29` 上，A14 尚未进 go）。无子 agent、无 push、无生产/真实 Agent/PVE 访问；
测试只经 `rt` 固定在 `.32` 执行，新增用例只用合成数据和回环 fake。

对照：Python `backend/agents/dns_follow.py`（全文）、`routes/agents.py` 162–191、`routes/tasks.py`
`_agent_rows`/`run_task`、`agents/manager.py record_deployment`；设计 `a16-a18-sync-follow-ha.md`
§2.3/2.4/2.5.3/2.7/2.8/3.2/3.4/3.6/3.7/4.4/6.8；作者交接 `progress/A17-handoff-zh.md`。

## 结论

- **high：0 条。未达到回退门槛（2 条 high）。**
- medium 2 条，都在「草稿已写入、状态未写完」这一段，都会让绑定永久失效（下一次 Mihomo 发布报
  「绑定的上游已被编辑或移动」，只能手动重新绑定）；Python 都没有这个问题。已修。
- low 3 条（凭据脱敏同类残留 1、设备回复类型容忍 1、用户可见文本 1），已修；测试覆盖补充 2 项。
- 共享最终提交窗口（O3-6c）登记为**待复验**，见第 3 节。

## 1. 发现

### medium

- **M1 — 重复 finalize 永远认不出自己写过的草稿（已修，`2a14b8ea7`）。**
  `finalize` 用 `equalJSON(src.Member("oxidns"), candidate)` 判断「草稿已等于候选」，但 `candidate` 是
  `MarshalJSON()` 返回的 `[]byte`，`json.Marshal([]byte)` 输出 base64 字符串，跟 `json.RawMessage` 永远不等。
  设计 §3.7 明列的恢复行「finalize 草稿写完、状态还没写 succeeded → 重启后再 finalize：草稿等于候选 → 不写，
  签名重绑幂等 → succeeded」因此不成立：重启后草稿摘要≠原始摘要，判成「发布期间 OxiDNS 草稿有新修改」，
  状态写 `succeeded … 已保留草稿` + `draft_synced:false`，并且**不重绑签名**；下一次 Mihomo 发布即报
  `绑定的上游已被编辑或移动…`，绑定永久失效。触发：关机（SIGTERM 收尾取消）或进程被杀落在草稿提交与签名/
  状态提交之间；同一路径也被签名提交的瞬时 repo 错误触发（下一轮 resume 重做 finalize）。
  证据：`TestShutdownAfterDraftCommitResumesWithoutSecondUpload`（真实全局 Runtime：在草稿提交钩子里停机，
  断言无晚到提交；重建 Runtime/Manager/owner 后一轮必须 `succeeded`、`draft_synced:true`、签名已重绑、上传仍为 2 次）。
  修复前失败：`"succeeded" "Mihomo 与 OxiDNS 均已发布成功；发布期间 OxiDNS 草稿有新修改，已保留草稿；请核对后再发布"`。
  修复：候选按 `json.RawMessage` 比较。其余 `equalJSON` 调用均为结构体/map，已逐个核对。

- **M2 — 草稿提交后的签名重绑重跑完整归属检查，来源一动就留下旧签名（已修，`64c9ceafc`）。**
  Go 在草稿事务里已经重查归属（作者对 Python 竞态的修复，保留），但随后的签名重绑事务又跑一遍完整
  `finalOwnership`（要求 Mihomo latest 仍是本次来源且 succeeded）。草稿已经写入候选地址后，只要这两个本地提交
  之间 Mihomo 开始新发布（用户手动发布或 A16 规则同步自动发布），重绑被拒：状态记 `failed … Mihomo 当前发布
  已改变或回滚…`（实际 OxiDNS 已发布成功、草稿已同步），签名停在旧地址，之后同 M1 永久失效。Python 的 `bind`
  只认 binding id。
  证据：`TestSourceMovingAfterDraftWriteStillRebindsSignature`（用 repo `ConfigurationCommitted` 钩子在草稿
  提交后立刻让 Mihomo 预留新发布）。修复前：`draft "udp://127.0.0.1:2053" written but binding signature not
  rebound (state "failed" …)`。修复：重绑只要求当前 binding id 仍等于 job 的 binding id（与 Python 一致）；
  草稿事务内的归属重查不变。

### low

- **L1 — 预留后失败的 child 记录残留其他 Agent 凭据（与作者修过的任务行同类，已修，`a265d0d5d`）。**
  轮次失败路径把原始 `e.Error()` 作为 `error_details` 交给 `RecordDeployment(OnlyUnsubmitted)`；A01 只用目标
  自己的 token 脱敏（Python `record_deployment` 同样只用本 Agent token），公共 Agent 视图 `publicAgent` 也只
  收集该 Agent 自己的凭据。错误文本里带的来源 Mihomo（或 HA peer）凭据会原样落到 OxiDNS 的发布记录并经
  `GET /api/agents` 返回。修复：写 child 前用全部 Agent 凭据（`s.safe`）脱敏。
  证据：`TestPreparationFailureRecordHidesOtherAgentCredentials`（经 `deploy.SetPairPublisher` 注入含来源 token
  的预留后拒绝；修复前目标记录含来源 token）。其余出口逐一核对：409/任务 runner（`FailureMessage` 全量）、
  state/events（事务内全量）、options（`publicOptions` 全量）、日志 label、task row（作者已修）、Retry 视图
  （只含常量文本和 ID），未发现其他残留。

- **L2 — DNS 回复整体解码，单个成员类型错就退化成「联动处理异常」（R04-F1 类，已修，`64c9ceafc`/`49bbe1c0d`）。**
  `deviceReply` 用标准结构体解码，`host: 53`、`enabled: "yes"`、`agent_id: 7`、`success: 1` 任一出现都让
  整个回复解码失败 → 非用户错误 → `联动处理异常，请检查 Agent 连接后重试`。Python 对应给出
  `Mihomo 返回的 DNS 监听地址无效`、`Mihomo DNS 尚未确认启用…`，`success: 1` 按真值继续。修复：逐成员解码，
  类型不对按缺失处理；`host: ""` 与 Python 一样落到 `必须为明确的 IP 地址`（原来报「地址无效」）。
  证据：`TestMalformedListenerReplyKeepsSpecificRefusal`（7 个子例，修复前 5 个失败）。

- **L3 — 设备名和来源名的缺省值与 Python 不同（已修，`49bbe1c0d`）。**
  Python `_request` 用 `agent.get("name", "Agent")`，options 来源名用 `agent.get("name", agent["id"])`；Go 统一
  用「名字为空则用 ID」。记录无 name 时连接失败文本应为 `Agent 无法连接…`，存了空名字的来源在 options 里
  应为 `""`。证据：`TestDeviceAndSourceNamesFollowStoredRecord`。日志 label 和任务行仍是 `name or id`（与 Python 同）。

### 测试覆盖补充（无代码缺陷）

- **T1 — 原始 DNS 请求的 query 未被任何测试断言（常设清单第 2 条）。** 两个 fake 都忽略 `deployment_id`，
  发错部署 ID 也能通过。新增 `TestDNSRequestsCarryExactDeploymentQuery`，逐条比对
  `GET /api/dns/listener?deployment_id=<来源>`、`GET /api/dns/peer`、`GET /api/dns/active?deployment_id=<基准>`。
- **T2 — 设计 §3.4 要求的 `TestMain` 锁序断言缺失。** 新增 `main_test.go`（`lockx.Violations()==0`），全包 race 通过。
- 另加回归：`TestCorruptJobCandidateNeverReplacesDraft`（job 缺失/空/非对象的 `candidate_settings` 不会替换草稿：
  profile 校验拒绝，报告保留草稿；null 情形与 Python 相同，不算发现）。

## 2. 已核对、无问题的部分

- 四条路由：全部 `auth.Require`；锁先于校验（PUT `[]` 在锁被占时得 409 忙）；PUT 用 silent JSON；retry 202；
  用户错误 409 + 全量脱敏；HEAD 只读。与 Flask 的差分 7 场景在 .32 通过。
- DNSFollowRow / runner：行字段、`can_run`、`run_hint`、`state_message`、历史覆盖与 `_agent_rows` 一致；runner
  的 ValueError → `tasks.Invalid`（409）。注意 `agent` 任务槽要等 A16 的规则同步行才完整（作者没有伪造）。
- boot loop：`AtBoot`、首轮 0、5 秒；`IdleRounds` 只在无错误时 `Done`；忙锁轮次不记空闲；Manager 关闭时移除
  owner；同一 DataDir 跨 Manager 互斥。
- 轮次逻辑逐行对照 `_round/_run/_resume/_finalize/_check_ownership/_save_state`：顺序、空闲判定
  （written / stored_only / FollowError）、去重条件、`WaitingForTarget` 只写一次、提交后才记 taskstatus 与日志、
  `rollback_failed` 非终态且禁止重试、丢应答只 poll 不重传。
- 关机：所有写 owner 走 `deploy.RunDetached`；每个提交点前查 `ctx.Err()`；失败路径先查取消再记录；
  作者的 Runtime 用例 + 新增 M1 用例覆盖 I/O 中、上传被接受后、草稿提交后三处停机。
- R5 / null：存储里 `upstream_index`/`upstream_tag` null 或错类型不当作 0；binding `id`/source/profile 为空即
  「绑定已改变」；`dns/active` 的 `deployment_id` 缺失不当作匹配（Python `None == None` 会通过，Go 拒绝，属 R05-3 修正）；
  `attempt: null` 重试按 0 处理（Python 500）；`dns_follow_state` 为 null/非对象时写入新状态（Python AttributeError）。

## 3. 待复验（不计入本次发现）

- **O3-6c 共享最终提交窗口**：`taskstatus.Store.Record` 无 ctx（A14 父分支还没有 `RecordContext`），SQLite
  最终提交前的 ctx 检查由共享层负责。A14 与 batch-4 合入 go 后 rebase，把 `recordOutcome` 换成
  `RecordContext(owner ctx)`，在合并树上补跑：本包定向 race、关机后 taskstatus 不写、live difftest。
- A14 合入后同时复验本分支新增的 M1/M2/L1 用例（它们依赖 A14 的 `PublishDirect`/`SetPairPublisher` 行为）。

## 4. 需要 L3 实机验证的项目（设计 §6.7 第 9、10 项细化）

1. 真实 Mihomo 改 `dns.listen` 后重新发布 → OxiDNS 上游跟随，期间 dig 循环无解析中断；端口/地址不变时只记成功不上传。
2. 同一网络空间（AIO）`auto` 连接：`0.0.0.0`/`::` 监听换成回环；跨设备 `network` 连接，含 IPv6 地址。
3. 面板 SIGTERM / `kill -9` 分别落在：读监听地址时、上传被接受后、**草稿已写入后（M1）**；重启后各自按 §3.7 收敛，
   设备不重复上传，绑定之后仍能继续跟随。
4. 上传应答丢失（实验网 nft 丢包）→ 状态 publishing，恢复网络后只 poll；OxiDNS 回滚失败 → 不允许重试，恢复后结束。
5. 旧版 Agent（无 `dns/*` 接口）→ `请升级 Agent 至 1.8.24-go…`。
6. HA 主备目标的联动（依赖 A18 的 pair publisher）留到 A18 复核后的 L3。

## 5. 运行记录（全部 `RT_HOST=sunbroke@192.168.0.32`）

```sh
rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/agents/dnsfollow internal/api/agents)"; go vet ./internal/agents/dnsfollow/... ./internal/api/agents ./internal/server; go test -race -p 4 -count=1 -timeout=20m ./internal/agents/dnsfollow/...; go test -race -count=1 -run TestDNSFollow ./internal/api/agents; go test -count=1 -run "^$" ./internal/server'
```

```
ok  	configflow/internal/agents/dnsfollow	555.914s
ok  	configflow/internal/agents/dnsfollow/difftest	95.263s
ok  	configflow/internal/api/agents	1.378s
ok  	configflow/internal/server	0.007s [no tests to run]
```

修复前失败输出见第 1 节各条。未跑完整 gate / 全仓 race（按协调者要求）。

## 6. 其他观察（不改）

- `taskRow` 每一行都重新读取全部 Agent 收集凭据，任务页是 O(行数×Agent 数) 次读取；Agent 少时无影响，
  如以后任务页变慢可改为每次 rows 调用只读一次（需要 C07 的 RowsFunc 传入上下文）。
- live 差分只有 7 个场景，没有覆盖发布失败、重试、停用、`rollback_failed` 的路由流（由单元测试覆盖）；黑盒
  Step B 差分仍需非移植 Codex 补齐这些行。
