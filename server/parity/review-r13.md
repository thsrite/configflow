# R13 HR 复核（pending reminders：AgentRouters / PendingCache / Summarize / C07 适配）

复核者：新的 Opus 会话（未参与移植；R13 由 Codex 移植，提交 `8ea43fd91`、`b78af2a08`）。分支 `go-opus-review-r13`，
从 `go-port-r13@b78af2a08` 分出，叠在 R12 复核分支 `go-opus-review-r12@7fa73f99` 之上。只用 loopback fake 和合成数据，
未接触生产、真实路由器、Agent、DNS 或 PVE；未 push，未开子 agent；只在 .32 跑定向检查，没有跑 gate 或全仓 race。

范围：完整读了 Python `backend/routers/pending.py`（260）、`routes/upstream_router_pending.py`、`notifications.record_router`
与 `test_upstream_router_pending.py`（17 组），对照 Go 的 `pending.go`、`pending_cache.go`、`pending_summary.go`、
`pending_messages.go`、`wire.go` 中的 Summarize hook 与 `recordPendingNotice`，以及它们依赖的 `lockx.Flight`、
`lifecycle.Runtime.Spawn`/`Detach`、`notifications.RecordRouter`/`SetSources`、R12 的 `Inspect`/`BuildPlan`/
`ReportParts` 和 `releaseRoutersOf`/`releaseRouterRecord`。按 D-R12-1，inspection 服务与 return advisory 已归 R12（已复核），
本次只看它们被 pending 调用的方式。

## 结论

**0 条 high，1 条 medium（已修），1 条 low（已修），2 条 low 记录在案（文档已补）；另有 1 项待 batch-1 合入后复验、
1 项 R12 同类问题交协调者。** 未达到「≥ 2 条 high」的回退门槛。

共享 Flight 生命周期、代际/Clear/淘汰后的迟到结果、LRU64、8 worker、25 s 共享预算、35 s 等待、unknown 不复用、
refresh 合并、深拷贝、显式 profile scope、请求断开（进入前与读取中）后任务完成、关机后不提交缓存与通知（在 R13 自己的
提交点上）、凭据不进公开结果、摘要与 Python 逐分支一致，均已核对。问题集中在「从存储读取关联路由器」这一步。

修复提交：`9573b1705`（F1、F2 及测试、behaviour-changes）；本报告所在提交（报告、PROGRESS）。

## 发现

### 中

- **F1 — 任一配置空间里有一条类型不符的路由器记录，所有 Agent 的上游路由提醒整体报错（Go 引入，已修）。**
  `pendingTargets` 对每个配置空间调用 `releaseRoutersOf`，用严格的 `[]*Router` 解码全部路由器；只要有一条记录某个字段类型
  不符（例如 `rule_sync_interval: "sixty"`），整个请求返回 `json: cannot unmarshal …`，与这个 Agent 无关的路由器也一样。
  仓库本身接受这种记录（加载时只记一条「配置字段类型不匹配，已按缺省值解码」警告），所以数据确实可能存在（旧版本写入、
  备份恢复、手工编辑）。Python 的 `routers_of` 读原始 dict：不关联的记录根本不看；关联的记录只影响它自己那一行
  （「a slow or failing router never hides the others」）。
  证据：`TestR13ReviewMalformedRouterIsolated`（关联/不关联两个子用例）在 `b78af2a08` 上都失败：
  `one malformed router record hid every reminder: json: cannot unmarshal string into Go struct field plain.rule_sync_interval of type int`。
  修复：`pendingLinkedRecords` 按 Python `routers_of` 的规则从原始 `routers.items` 取记录（`routers` 缺失/非对象、`items`
  非列表 → 无路由器；非对象项跳过；只有字符串 `proxy_gateway_agent_id` 等于 Agent ID 才算关联），只对关联的记录做类型化解码；
  解码失败的关联记录保留其字符串 id/name/type，读作 `unknown`（`读取失败：内部错误`，不发设备请求，日志只记路由器 ID），
  签名使用原始记录字节。正常记录的签名仍是 `releaseRouterRecord` 的原始字节，与修复前一致。
  同时消除了一个潜在 panic：`items` 中的 `null` 原来会成为 nil `*Router` 并在 `r.ProxyGatewayAgentID` 处解引用。

### 低（已修）

- **F2 — 空 Agent ID 会匹配所有未设代理网关的路由器，并真的去读设备（Go 引入，「缺失不得算匹配」一类）。**
  Go 的类型化记录把缺失和空串都读成 `""`，`r.ProxyGatewayAgentID == req.AgentID` 在 AgentID 为空时把所有未关联路由器当成
  关联目标。Flask 路由不会传空 ID，但 `AgentRouters` 是 R15 之外也可调用的产品入口。
  证据：`TestR13ReviewEmptyAgentMatchesNothing` 修复前返回一行 `profile plain`，状态 `blocked`（并向 fake 设备发了 GET）。
  修复：空 Agent ID 不关联任何路由器（也不调用驱动）。
  顺带：路由器类型不受支持（`NewDriver` 返回 nil）时原来静默返回 unknown，现在与 Python 的 `logger.exception` 对应记一条
  只含路由器 ID 的警告。

### 低（记录，不改代码，已补 behaviour-changes）

- **F3 — 读取边界的计时与 Python 不同，原交接未登记。** 第一个调用者（Python 的 owner）在 Go 里同样最多等 35 s 后返回
  `ErrPendingTimeout`（Python 的 owner 不设上限，直接执行 `load`）；25 s 预算到期时仍在进行的读取会被取消（Python 的线程在
  后台继续跑完再丢弃结果）。读取本身 25 s 内结束、通知写入很快，正常情况下 owner 不会碰到 35 s；两处都比 Python 更有界。
- **F4 — C07 Summarize hook 对无法解码的 JSON 生成一条 unknown 的「上游路由发布」回执（文案
  `无法解析上游路由差异报告，请重新检查。`）。** Python 的 `summarize` 要么容忍该形状，要么抛错被 `safely` 吞掉、不写回执。
  只有 Go 自己序列化的 Report/Plan 会进入 hook，实际不可达。我加了 `TestR13ReviewNoticeMatchesReminder`：用真实
  Inspect/BuildPlan 结果经 hook 写出的回执文字等于原生摘要的 `有 N 项配置差异待发布。`（通过，作为回归保护）。

## 待复验（batch-1 合入后在共享树上做）

- **P1 — 通知写入的最后一段取消窗口。** `recordPendingNotice` 在 Detach 之后、调用 `notifications.RecordRouter` 之前检查
  `ctx.Err()`；当前基线的 `RecordRouter` 进入 `notifications.Transaction(dataDir, fn)`，没有 ctx，在等待通知 guard、
  Rename 之前不再看取消。因此「检查通过 → 关机 → 等到 guard 后提交」这一窗口在当前树上存在。作者与前协调者的裁定是由 C07
  batch-1 的 `TransactionContext`（leaf 锁内与 Rename 前检查）统一修复，R13 不复制。batch-1/batch-4 合入 go 后，rebase
  本分支并补一条真实 Runtime 用例：先占住通知事务（另一个 `Transaction` 阻塞在 fn 内），让 `AgentRouters` 的 owner 走到
  `RecordRouter`，再 `Shutdown`，然后放开 guard，断言 `notifications.json` 没有本次回执、缓存未提交；同一用例的断开分支
  断言回执照常写入。O3-6b 的 Wire hook 合并（`routers.Wire` 同时注册 RedactRouters 与 Summarize）在同一次复验中确认。

## 交协调者（R12 同类问题，不在本分支修）

- **X1 — `releaseRoutersOf` 的严格解码在 R12 里还有两处。** `returnCleanup`（`inspect.go`，遍历所有配置空间找共用回流的
  路由器）和 `Publish`（`publish.go`，在目标配置空间中按 ID 找路由器）都用它：同一配置空间或任一配置空间里一条类型不符的
  记录，会让回流清理检查（Agent 有生效的受管回流时）或另一台正常路由器的发布直接失败；`items` 中的 `null` 会 panic。
  Python 两处都按原始 dict 读取。建议由 `routers.Router` 的所有者（R01/n8 类型化模型规则）让 `Router.UnmarshalJSON`
  按 R5 缺省值回退并记录问题（与 `model.Router` 一致），而不是在每个调用点各写一份容错；R14/R15/R16 接入前完成。

## 重点核对（未发现其他问题）

1. **Flight 与代际。** Flight 的 key 是缓存内全局递增的 serial，只在 entry 不处于 pending 时分配，所以同一代际只会启动一次；
   提交时在缓存锁内同时检查 `entries[agent] == entry`、`entry.serial == serial`、`work.Err()`，Clear、淘汰、新签名之后的
   迟到结果不会覆盖新条目。fn 的 defer 在 Flight `finish` 之前清 pending，`finish` 前到达的读者加入同一 call；
   Spawn 失败（关机）时 waiter 若已断开导致 pending 残留，下一次读取用同一 serial 重新启动，能自愈（作者用例覆盖重试）。
   锁顺序：`PendingCache.mu` → `lifecycle.defaultMu` → `Runtime.mu`，后两者内部不回调缓存，无环。
2. **资源上限。** 每次读取最多 8 个 tracked worker，jobs 预先入队、results 按目标数缓冲，超时或关机后 worker 不阻塞、
   不取新任务；计时器都 Stop；缓存最多 64 个 Agent，Flight call 完成即删除；没有目标的 Agent 不建条目。
   签名在不同 scope 之间来回切换会让同一 Agent 先后启动多次读取（每次仍各自有界），与 Python 相同。
3. **25 s / 35 s。** 25 s 计时器在启动 worker 之前创建，到期后取消剩余读取、非阻塞收齐已完成结果，未完成的报
   `读取超时（25 秒）…`（`%g` 与 Python `{:g}` 一致）；等待者各自 35 s，返回 `ErrPendingTimeout`（R15 映射 504）。测试用
   注入时钟推进真实的 25/35 s，没有缩短生产参数（R06 常设检查项 1）。
4. **断开与关机。** `AgentRouters` 与 `Read` 只用 `Detach` 后的上下文判定准入，请求在进入前或读取中断开都会照常完成读取、
   写通知、提交缓存（作者的真实 Runtime 用例三种模式）；关机后 Spawn 被拒，Flight work 被取消，设备读取取消，
   缓存提交与每条通知前都检查 `work.Err()`（第四条常设检查项；leaf 窗口见 P1）。
5. **摘要。** `_stage_word`/`_change_word`/`_blocked_reasons`/`summarize` 逐分支对照：只有四个 key 看 `enabled is False`；
   Fake-IP 状态词；规则集路由与前缀保护去掉 synced/borrowed 后的集合比较；`ipv6_dns` 固定「待关闭」；`return_cleanup`
   固定「待移除」；冲突原因只取 routes/nat 两节；阻断原因去空白项；`（另有 N 项）`。原生投影里 ReportPart 全是值类型
   （已核对 R09–R11 的 Section 类型），与 JSON 路径一致；作者的 8 组真实 Python 对照通过。
6. **签名与 scope。** 签名覆盖 scope（nil 与显式值不同）、关联配置空间的 id/name/revision（含共享修订）、关联路由器原始记录
   和 Python 规定的 8 个 Agent 字段；只存 SHA-256 摘要。显式空 profile 与不存在的 profile 都拒绝，不回退。
7. **凭据。** 公开结果只有 id/name/type/profile 与摘要；RouterError 文案经 `diag.FailureText` 去掉密码（用户名保留），
   其他错误只给 `读取失败：内部错误`，日志经 `diag.EventMessage`，panic 不记内容；设备调用全部为 GET。
8. **C07 适配。** `notifications.SetSources` 只安装非 nil 字段，`routers.Wire` 已在 `server.wirePackages` 中；
   hook 的数字经 jsonx Number 保持 `3` 而不是 `3.0`；`RecordRouter` 传显式 profile，router 用原始存储字节。

## 需要 L5 实机验证

1. 同一实验 Agent 关联一台 RouterOS CHR 与一台 iKuai：提醒的状态、计数和阶段状态词与上游路由页「预览发布」一致
   （pending / synced / blocked 各一次）。
2. 让其中一台丢包不可达：25 s 后该行为读取超时，另一台正常；读取进行中再发一个普通请求和一个 `refresh=1` 请求，都加入同一次读取。
3. 同一台 iKuai 在两个配置空间里以克隆 ID 关联同一 Agent：两路并发登录读取都成功（确认 iKuai 不会因同账号并发登录踢掉前一个会话）；
   25 s 取消的 iKuai 读取不残留会话。
4. batch-1 合入后：读取中向面板发 SIGTERM，宽限期内退出，重启后通知列表里没有关机之后写入的回执（P1）。

## 验证命令与输出（均在测试服务器 .32，RT_HOST 固定）

```sh
# 作者基线
rt -- bash -lc 'cd server && go vet ./internal/routers && go test -count=1 -race ./internal/routers -run "TestR13" -timeout 10m -v'
# 10 个顶层用例 PASS；ok configflow/internal/routers 7.163s

# 反向对照：新测试 + b78af2a08 的 pending.go
rt -- bash -lc 'cd server && go test -count=1 ./internal/routers -run "TestR13Review" -v'
# TestR13ReviewMalformedRouterIsolated/linked=false、/linked=true FAIL（json: cannot unmarshal …）
# TestR13ReviewEmptyAgentMatchesNothing FAIL（读出 profile plain 一行）；TestR13ReviewNoticeMatchesReminder PASS

# 修复后
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers)" && go vet ./internal/routers ./internal/notifications && go test -race -count=1 ./internal/routers -run "TestR13|TestR12Review|TestR12Runtime" -timeout 10m && go test -race -count=1 ./internal/notifications -run "TestRouterNotices|TestSameRouterID|TestRouterRead|TestObservationCollection" -timeout 5m'
# gofmt-clean；ok configflow/internal/routers 13.194s；ok configflow/internal/notifications 4.588s
```

未跑完整 gate、proddiff、全仓 race（按简报由协调者在批次树执行）。

## 提交

- `9573b1705` go(routers): R13 review — isolate undecodable router records, empty Agent links nothing
- 本报告所在提交：本报告、PROGRESS R13 状态。
