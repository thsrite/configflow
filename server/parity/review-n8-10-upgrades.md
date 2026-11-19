# N8-10 upgrades S pass HR 复核（fresh Opus）

复核对象：`go-n8-10-upgrades@1051ddfc5`，只看 `f8df41fb1..1051ddfc5`（作者 Codex 4a68384a）。复核分支
`go-opus-review-n8-10-upgrades`。对照 Python（`backend/agents/upgrades.py`、`address_follow.py`、`manager.py`）和
迁移前 Go 阅读代码。定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4），没有重跑作者整套，也没有跑完整门禁。
探针和回归只用合成数据（192.0.2.0/24、假 token）。

## 结论

high 0，medium 0，low 3（2 已修、1 记录）。L1 按协调者裁定 (b) 已修；L2 归入 N8-11。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| R1 | low | 已修 | **Agent 的 `version` 为显式 null 时，Start 报“Agent 操作记录损坏（version）”（500）。** 注册（`DecodeRegistrationInput` → `setRegistrationMembers`）会原样保存 Agent 发来的 `"version": null`。Python 用 `agent.get('version', '')` 得到 None，照常升级。`version` 是 Agent 上报的字段，不是升级证据，也不在裁定 2 的范围（latest_upgrade / 升级守卫）内；而且报错让管理员去离线改 SQLite，实际上一次心跳就能修好。修复：`StartModel` 把 null 当作缺失（previous_version 为空串）；数字、数组等仍按损坏拒绝（不扩大接受面）。回归 `TestReviewN810NullAgentVersionStillStartsUpgrade`（null 能升级；`1.5`、`["1.0"]` 仍是 `CorruptStateError{version}`）。负控：临时恢复旧判断后 null 子测试失败（报“操作记录损坏（version）”），还原后通过。behaviour-changes 已补一行。 |
| L1 | low | 已裁定并修复 | **HA 字段损坏时，只要上报的地址与记录不同，每次心跳都返回 500。** `FollowModel` 在节流和后台任务之前同步调用 `memberHA`，返回的 `CorruptStateError` 会一路传到 heartbeat 的 500。探针结果：`oxidns_ha={"enabled":1}` 加新地址，心跳 500，正文是“Agent 操作记录损坏（oxidns_ha.enabled）…”；不带 host 时仍是 200。心跳状态在调用 hook 前已经记录，设备仍显示在线。但这段时间 Agent 收不到 `domain_discovery_enabled`、`traffic_history_enabled` 和 `confirmed_host`（Go Agent 只在 200 时应用这些值），而且每次心跳都会产生一条 500。Python 遇到 truthy 非 dict 的 `oxidns_ha` 也会 500（迁移前 Go 一致），但 `{"enabled":1}`、`oxidns_ha=false`、`oxidns_ha_publication.status` 不是字符串这几类，Python 是继续跟随或当作成员处理，不会 500；所以本轮把心跳 500 扩大到了新的输入类别。不跟随这个方向是对的（作者自查的“坏 enabled 不能解锁地址修改”成立）。建议二选一：(a) 维持现状（心跳 500 是可见的损坏信号）；(b) 在 `FollowModel` 里，遇到 `CorruptStateError` 时按地址节流记一条 warning、跳过跟随，心跳照常返回 200（错误只记日志、不返回，符合风格要求）。地址对话框 GET/PUT 继续 500。**裁定：选 (b)。** 已修：`FollowModel` 遇到 `CorruptStateError` 时不跟随、不写库、不接触设备，按（Agent，损坏字段）每 10 分钟（`CorruptWarnInterval`）记一条经 `redact.DiagnosticText` 的 warning（消息常量 `msgFollowCorrupt`），返回 nil，心跳照常 200；其他错误仍向上返回。`ResetAddressState` 同时清空告警节流。回归 `TestReviewN810CorruptHAFollowKeepsHeartbeat`（`{"enabled":1}` + 新地址连续三次心跳都是 200，`domain_discovery_enabled` 照常下发，地址不变，fingerprint 不变，未接触设备，告警只有一行；地址对话框 GET/PUT 都是 500 并带损坏字段名）和 `TestReviewN810CorruptFollowWarningInterval`（表驱动：间隔内不重复，换字段单独告警，到间隔后再告警）。负控：临时恢复 `return err` 后心跳 500，测试失败，还原后通过。behaviour-changes 已登记（相对 Python：不跟随 + 告警）。 |
| L2 | low | 记录（N8-11） | **core 的 typed hook 只能新增，不能撤销。** `RegisterHooks` 只覆盖非 nil 字段，typed 又优先于旧 hook。所以在 `upgrades.Wire()` 之后再注册旧 `AddressFollow`/`AddressConfirmed` 的调用方会被静默忽略，只有 `ResetHooks` 能清掉。目前生产代码只有 `Wire` 注册（旧 hook 没有生产调用方），测试 `TestHeartbeatReportedHostUsesAddressFollowHooks` 所在环境不调用 Wire，不受影响。`agents/doc.go` 的 hook 清单还没写 typed 变体。N8-11 删除旧 hook 时一并处理即可。 |

只作说明，不算发现：

- `latest_upgrade` 内的 `events`/`error`/`message`/`artifacts` 错型不会被 `readState` 拒绝：解码时按缺省值处理（R5 回落），下次保存会被清理或覆盖。这与 Python 一致（`clean_events` 对非 list 返回 []，error/message 每次重写），而且它们只是展示用的日志，不是所有权或成功证明，不在裁定 2 的拒绝范围内。
- worker 回包 `"error": null` 现在存成 `"null"`（Python 存 `"None"`），属于裁定 4 的 native JSON 诊断文本。真实 Go Agent 的 `upgrade/worker.go` 用 `error,omitempty` 字符串，不会发 null。

## 逐项核对

1. **裁定 1（core typed hooks）**：`Hooks` 新增 `AddressFollowTyped`/`AddressConfirmedTyped`（`*model.Agent`），`RegisterHooks` 按非 nil 合并。只有 `api/agents/agents.go` 的 heartbeat 调用 hook（core 包内没有调用点）：先选 typed、否则旧 hook，follow 和 confirmed 各自只调用一次；只有当某一边缺 typed 而有旧 hook 时，才把 `model.Agent` 编码成 jsonx（生产环境 `Wire` 两个 typed 都注册，不走这条路）。heartbeat 改为 `ReadAgent` + `agentTokenMatches`（原始 JSON string token、非空、常量时间比较），缺失 Agent 时返回 401，与原来一致。偏好开关 `heartbeatFeature` 用 `JSONHasContent`，等价于原来的 `Truthy` + `service_type=="mihomo"`（错型 service_type 回落为空，结果 false，同 Python）。作者回归 `TestN810TypedHeartbeatHooksTakePrecedence`（旧 hook 被调用就 Fatal，typed 恰好一次）、`TestN810TypedAddressSnapshotPreservesHeartbeatPreferences` 覆盖了这些；分配阈值由作者在 .64 实测原阈值通过，复核没有重跑。见 L2。
2. **裁定 2（损坏状态）**：`readState`/`optionalObject` 对 present、非 null、错型的 `latest_upgrade`（含 id/版本/时间/protocol/status 子字段）、`latest_deployment.status`、`oxidns_ha_publication.status`、`oxidns_ha.enabled` 返回 `CorruptStateError`。这些检查都发生在写入之前（Start 的 reservation 回调、saveModel、Poll），不写库，也不自动清除。HTTP 由 `upgradeStateErrors` 统一转成 500 和中文字段说明；update/upgrade/report/address 都套了这一层，self-read 在鉴权通过后才读到损坏信息。复核探针：`{"status":5}`、`update_id:7` 两种子字段损坏，GET /api/agents 都是 200，单 Agent Poll 是 500 并给出对应字段名。作者测试 `TestN810CorruptUpgradeDoesNotBreakAgentList` 覆盖 `"abc"`/`5`/pair-list：列表和 summary 都是 200，另一台 Agent 名称正常，Poll 500，fingerprint 不变。`TestN810CorruptGuardsRefuseWithoutWriteAndRecover` 证明按文档删掉坏成员后可以重新升级。没有新增 HTTP/UI 恢复入口，也没有死代码修复函数（`Clear` 只出现在测试里）。`doc/manual/agents.md` 写明了离线修复步骤（备份、`fields` 行、generation/commit_id），以及删除后重新登记的后果。38 条 native 拒绝行逐行 SHA 绑定原 A15 行，原 archive 的 SHA 也固定；复核抽查了 Python 返回 200 的 4 行（terminal 的 `protocol:true`、超大整数和 bool 的 `started_at`），都属于“present 错型”这一类，在批准范围内。
3. **裁定 3A（同一 worker 三项全匹配）**：`pollAgentModel` 只有在**同一个** `decodeWorker` 结果里 `update_id==state.UpdateID`、`agent_id==agentID`、`status∈Stages` 时才生成 worker patch；protocol 2 的成功还要求 `worker.Status=="succeeded"`，再加上 upgrade-info 的 agent_id/白名单 version/target/`migration_ready` 为原始 `true`。agent_id 错误时 worker 为 nil，所以即使 upgrade-info 完全正确也不会成功。`decodeWorker`/`decodeInfo` 都先经 `model.Fields` 拒绝重复键，键名精确匹配。`TestN810PythonBugBoundary` 的 pending 行要求状态保持不变、无写入，并且先校验了 Python 确实误判为 succeeded（正控）。
4. **裁定 3B（architecture）**：hasInfo 时 architecture 只取原始 string；缺失、空串、非字符串都得到空串，`available.Get("")` 不命中，于是在 `EditAgents` reservation 之前返回 409 `msgArtifactsNotReady`。boundary 回归同时检查了 POST 未发送、fingerprint 不变。
5. **裁定 4（非字符串 error）**：worker 和 report 的 error 用 `rawText`：字符串取原文，其他取原始 JSON 文本，全部经 `saveModel` 的 `redact.DiagnosticText(…, secrets, 2048)` 处理；API 层仍用 `UpgradeErrorMessage` 做中文包装。没有使用自写脱敏。
6. **常设规则**：Agent JSON 经 `jsondecode`/`model.Fields`，键名精确。版本走 `versionPattern`（ASCII，64 字节）。存储身份（id/token/host/update_id）都要求原始 JSON string（`originalString`）。时间值经 float64 解码，NaN/Inf/溢出都拒绝（超大整数报解码错误）。protocol-1 bootstrap self-read、report（`rolling_back` + update_id 绑定）、fresh heartbeat 回滚确认、过期 unknown 继续查询，都与 Python 结构逐行对照过，一致。所有写入都经过 `lifecycle.Detach` + `tx.Err()`，地址后台任务用 `SpawnerFrom(ctx)`。upgrades 不调用 installer renderer（grep 确认）。原 A15 archive、pyref、既有测试的期望都没有被改动；作者对旧测试只改了 seam 签名（换成测试内 codec），`Usable(c.GetString(...))` 对非字符串输入得到 `""`，与 Python 非 str 返回 None 等价。behaviour-changes 登记齐全（R1 已补）。报告的延后清单（旧 hook 留到 N8-11、API 其余 binder 下一轮、恢复 UI 列为后续项）与代码一致。
7. **作者自查（坏 `oxidns_ha.enabled`）**：`memberHA` 对 `true` 判为成员，`false`/`null`/缺失判为非成员，其他值一律 `CorruptStateError`。`ChangeModel`/`DescribeModel` 在接触设备之前返回 500；`FollowModel` 按 L1 裁定不跟随、只告警。`TestN810DamagedHAFlagCannotUnlockAddressChange` 断言了未接触设备、无写入，以及修复后恢复。

## 复核运行（.64）

- `go test -race -p 4 -count=1 ./internal/agents/upgrades`：PASS 61.8s（含新回归）；L1 修复后复跑 PASS 57.7s。
- L1 修复后 `go test -race -p 4 -count=1 -run "N810|Upgrade|Address|Heartbeat" ./internal/api/agents`：PASS 250.4s；`go vet` 两包、`gofmt -l` 无输出。
- `go test -race -p 4 -count=1 -run "N810|Upgrade|HeartbeatReportedHost" ./internal/api/agents`：PASS 119.3s。
- `CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -run "Upgrades.*DifferentialAgainstFlask|N810UpgradesHTTPPolicy" ./internal/api/agents/difftest`：PASS 22.8s。
- `gofmt -l`、`go vet ./internal/agents/upgrades`：无输出。临时探针（坏子字段列表、坏 HA 心跳、version null/数字）跑完已删除。
