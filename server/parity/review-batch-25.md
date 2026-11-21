# batch-25 HR 复核（网络与 HA）

范围：go@cc303329a 之上的 go-lab-fixes-agent（L5-D1、L3-D1、L2-D3、Agent 1.9.1-go、routes 竞争修复、gate agent 阶段）
与 go-lab-fixes-panel（L3-D2、L3-D3、L5-D2、L5-D3、O-1）。复核者：Opus 5.5，2026-11-22。

## 结论

无高、中问题。两处低级问题已修，另记三项低风险说明（不改）。

| 级别 | 项 | 处理 |
|---|---|---|
| 低 | O-1：超时后若无法结算（失联成员或原持有者的子发布无法证实、让位无法撤销），后台每 2 s 一轮都写一条 WARNING「OxiDNS pair publication stopped: member connection lost」，两台都离线时每小时约 1800 行 | 修：只在第一次停止时记录（消息已以 `msgMemberLost` 开头或 `HandoffFailure` 已记为它时不再记录）；新增 `TestLostMembersBothOfflineKeepPairUnknown`（两台都离线：保持 unknown、消息为失联文案、强制停用仍被拒；原持有者回来后 failed）。该用例跑出超时后 ≥9 轮只有一行日志 |
| 低 | L5-D3：`planOps` 插在 `saveAbortedPublication` 的文档注释与函数之间，后者丢了注释（R12 唯一本地提交例外的说明） | 修：各自注释 |
| 低（说明） | L3-D1：pidfile 兜底只核对 `/proc/<pid>/comm == keepalived`。keepalived 崩溃留下陈旧 pidfile 且 PID 恰被另一个 keepalived（例如同机容器里的）复用时会误发 SIGTERM | 不改：需要「崩溃 + 陈旧 pidfile + PID 复用 + 复用者恰为 keepalived」同时发生；若要收紧，可再比较 `/proc/<pid>/ns/mnt` 与本进程一致 |
| 低（说明） | O-1 文档写「两台都离线时撤销让位失败，仍保持 unknown」。实际先命中的是原持有者已提交子发布无法证实 → `unknown`「…已停止…；已提交的设备结果仍待确认…」，还没走到撤销让位。对用户结局相同（unknown、停用被拒，原持有者回来后 failed），新测试按实际行为断言 | 不改行为；文档措辞可在下次 HANDOFF 时顺带改 |
| 低（说明） | L5-D2：网段取自路由器包含 Mihomo 网关的最长直连路由，Agent 的 apply 校验看的是 Agent 网卡前缀。Agent 掩码比路由器宽（例如 Agent /16、路由器 /24，管理地址在路由器另一 /24）时，面板阻止而 Agent 原本可能放行 | 不改：这种配置下回流下一跳本就依赖路由器跨网段应答 ARP，阻止并给出修改提示是合理的。/32、多网段、IPv6 见下 |

## 逐项核对

### Agent

**L5-D1 回流认领**（`return_routing_pending.go`、`return_routing_owned.go`、`return_routing_safety.go`）
- 认领范围只限 `inet configflow_return` 表内：链名必须以 `cf_<本机 token>_` 开头；规则必须在这样的链里且 comment 以 `configflow-return:<token>:` 开头；表本身只有在带本机标签或含有本机 token 对象时才收。其他任何对象（别人的链、无标签规则、set/map/flowtable、他人标签的表、空且无标签的表）都会拒绝整个认领，不写任何东西。非 inet 族的同名表不属于受管范围，既不认领也不删。
- 删除按记录的 handle 精确删除，删前再次核对 handle；表从不删除。所以认领的对象只可能是带 128 位 token 的对象，部分写入时不会删到无法证明的东西。
- 路由与 ip rule 不参与 token 认领，仍由 `checkReturnResources` 对目标表/优先级里未记录的条目拒绝（与文档一致）；`addOwnedReturn` 每写一条就回读并入账。
- Agent 重装（新 token、无状态）或状态文件被复制到另一台：旧 token 的链名前缀不匹配 → 拒绝；复制过去的非 pending 状态在新机器上找不到记录对象只会重新创建，不会接管别人的对象。
- 旧格式（表/链带 comment）记录：比较两侧都去掉表/链 comment，`ownedMatchesSpec` 仍成立，开机不重放（有测试）。handle 比较保留。
- 预览路径（`selectReturnAllocation`）只在克隆上认领，不写状态。

**L3-D1 keepalived**（`oxidns_ha_resources.go`）
- 停用：原本未运行时先停（配置还在），失败即返回「配置未删除，可重试」；原本运行时删配置后重启。`haVerifyReleased` 在 systemd / Supervisor 路径同样适用：`haDaemonAlive` 对非 OpenRC 等于 `haServiceRunning`，VIP 检查读本机网卡，最多 10×0.5 s。正常情况首轮即通过、不等待。
- 校验失败时状态保持 `Pending`，再次停用时文件已删 → 跳过文件步骤、再停一次、再校验，可重试。
- 开启失败回滚：`wasRunning` 是本次开启前的实时运行状态（不是首次记录的 `Service.WasRunning`），更新已启用的 HA 失败时不会误停 keepalived；未运行时先停再恢复文件，再按原状态恢复服务。
- pidfile 兜底：见上表低（说明）。`pid <= 1` 拒绝；SIGTERM 后最多等 5 s，再 zap。

**routes 竞争修复**：`serviceOperationHandoff` 只在拿到 flock 后 Load、释放前 Add(1)，不参与任何判断，生产上只是两次原子操作；`forwardFilterInspect` 提前取值，无行为变化。

**gate.sh agent 阶段**：`run_core` 在 server race 之后调用 `run_agent || exit 1`，`all`/`core`/`agent` 都会运行；`run_agent` 内每一步都有显式 `|| fail`（`||` 上下文里 `set -e` 失效，但这里不依赖它），gofmt 有输出、vet（含 `halab nftlab` 标签）、race 任一失败都使门禁失败。

**L2-D3**：连接正常时满 60 s 再读 `/version`，断开重连立即读，无问题。

### 面板

**O-1**（`dnsha/publication.go`）
- `unreachable_since`：第一轮传输错误/状态不可用时记下并随 unknown 一起保存；任何一轮成员有应答（成功、交接等待、用户错误）即清除。`EncodeRecord` 对新增的 nil 指针不输出、清除时移除，存储兼容；汇总投影不含它，接口无变化。
- `settleChildren` 只对「已提交、未终结、非 ready」的子发布调用 `ReadDeploymentModel`：只读 GET（manual_rollback 的既有补发逻辑除外），离线成员走 transport → pending，不报错、不写存储，不会重复下发。
- 超时后 `fail()`：失联成员已提交且未证实的子发布 → 保持 unknown（文档所述「不另加时限」）；否则撤销原持有者让位并等选举（`HandoffTimeout`），最终 failed。两台都离线见上表。
- 测试覆盖「10 分钟内回来」（`returns-in-time`：succeeded 且计时清除）。

**L3-D2**：`recovery()` 对未终结成员查询；离线 → pending、不改存储；Agent 仍在回滚时返回 `rolling_back`/pending，不会被判终态。配对日志已终结后 GET 的补查同理。

**L3-D3**：只对 `*dnsha.Error` 换成 `FailureMessage` 脱敏文案，其他错误文案不变。

**L5-D3**：`StartPublish` 的工作 context 由 `workContext(detach=true)` 派生，请求断开不取消发布（与原 httpx 行为一致，`disconnect` 子测试未改）；关停时 Runtime 取消并等待跟踪任务，中止记录在 `Shutdown` 返回前提交。`Applied` 只在 `finished(…, ok=true)`（设备回复证实）时递增，撤销用的 effectful 失败不计。

**L5-D2**：只在读到直连路由（主表、启用、connect 且 active）且有网段包含 Mihomo 网关时判断。多网段取最长前缀；/32 直连路由不可能包含网关以外的地址，不会误判；上游与网关都要求 IPv4（IPv6 早已由 `messageInvalidUpstream` 阻止），iKuai 无直连路由证据时不判断。

## 提交

- `32683d15f` go(dnsha): log the lost-member stop once and cover both members offline (HR batch-25)
- `f513cea5a` go(routers): give planOps its own doc comment and restore saveAbortedPublication's (HR batch-25)
- `44fc064d6` go(pyref): rebase 到 batch-24 后 mosdns golden 的 agentha.go 源元数据刷新（条目不变；oxidns check 干净）

rebase：`git rebase go`（cc303329a），behaviour-changes.md 冲突两边保留；mosdns pyref golden 二进制冲突取 go 版本后 record+accept，再 check 通过。

## 验证

见交回消息（同一树上运行）。
