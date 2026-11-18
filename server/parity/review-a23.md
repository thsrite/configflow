# A23 HR 复核：IPv6 就绪、客户端证据与验收

复核者：Opus（不是作者）。分支 `go-opus-review-a23`，从 `go-port-a23@d21921782` 分出（基底 go-r17-base-v2：go + R14 + 复核后的 R16，都还没进 go）。

结论：**3 条发现：1 条 medium、2 条 low，0 条 high**。全部已修复，每条都有修复前失败的回归测试。没有达到回退门槛（需要 ≥2 条 high）。A23 状态改为 `verified`；L4 实机项见文末。

## 方法

- 对照 Python `agents/ipv6_readiness.py`、`ipv6_verification.py`、`ipv6_client_evidence.py`、`routes/ipv6_readiness.py` 和 `routes/routers.py::_ipv6_host`，逐个函数读了 Go 的 `internal/agents/ipv6`、`internal/api/ipv6`、`routers/operation_group.go`、`routers/connection_reader.go`、`lockx/order.go`，以及 server 接线。
- 按问题类别逐项核对：R16（坏行隔离、跨设备凭据）、R10/R11（IPv6 归属、未知读取）、R14 M1/M2（lifecycle 竞态、时间预算）、A21 F4（两级脱敏）、A22 F1/F3（类型异常的 Agent 记录）、四条常设检查（测试替身不覆盖生产参数、原始请求字节、共享驱动改动、Detach 后提交）。
- 按「精简验证」规则，没有重跑作者的完整对照套件。只跑了自己修复对应的测试和受影响的包，外加抽查作者的 Python/Flask 对照组。

## 发现

### F1，medium，已修复：enroll 把 Agent 的发布历史按 ID 字母序重排

- `Enroll` 把 `deployments` 解成 Go map 后再编码，键被排序。A14 的 `PrepareDeployment` 按插入顺序只保留最新 30 条（`keys[:len-30]` 删掉最前面的；Python 也是 `list(deployments)[:-30]`）。发布 ID 是随机的 uuid4 hex，所以每次发布启用 IPv6 的配置（默认模板就是 `ipv6: true`）以后，历史就按字母序排列了。下一次裁剪会删掉随机的条目，可能包括最近的几次，而真正最旧的反而留下；Agent 页面的发布历史顺序也会乱。
- Python 直接改原来的 dict，顺序不变。
- 修复：`deployments` 改用 `model.Map[json.RawMessage]` 读写，保留顺序和未知字段。
- 测试 `TestEnrollKeepsDeploymentHistoryOrder`。修复前：`deployment history reordered: [a-newest m-middle z-older]`。
- 其他写入点已核对：`ipv6_verifications` 本来就用 `model.Map`；`update()` 只会重排 Agent 记录顶层的键，嵌套的原始值保持原样。顶层键的顺序没有任何读取方依赖，HTTP 出口也会重新排序。

### F2，low，已修复：prepare 失败的消息没有用捕获时的凭据种子脱敏

- 作者的说法是：凭据被并发替换以后，旧值也不会泄漏。status、probe、Host、验收结果都是这样做的，但 `Prepare` 里 Agent 返回的 `PREPARE_FAILED` 文字（`主机配置补齐失败，请重新检查：<Agent 消息>`）只经过 HTTP 层的 `Public`，而 `Public` 用的是**当前**的种子。如果另一台设备的密码或 token 在 prepare 请求期间（最长 45 秒）被替换，而 Agent 的错误消息里又带出了旧值，旧值就会原样返回。
- 修复：`Prepare` 的 owner 在返回错误前调用 `capturedError(config, a, err)`，种子包含快照里的全部设备配置，和 status/probe 一致。
- 测试 `TestPrepareFailureUsesCapturedCredentials`。修复前，返回的消息里有 `synthetic-peer-old-token synthetic-peer-old-password`。已登记 behaviour-changes。

### F3，low，已修复：网关地址换一种写法时，会被当成验收客户端的源地址候选

- Python `_policy_context` 先比较两边的原始文本，再比较解析后的地址（`address != source`）。Go 只比较了原始文本，所以 `2001:db8::1` 和 `2001:db8:0::1` 会把网关自己的地址作为 `source_candidate` 交给 R11 的策略检查。
- 修复：同时比较解析后的地址；网关地址解析失败时，行为和 Python 一样（不排除）。
- 测试 `TestPolicyCandidateExcludesGatewayAddressSpelling`，修复前失败。

## 重点项核对（无发现）

- **锁组 API `RouterOperationLocks`**：按「类型 + 去掉末尾 `/` 的 endpoint」排序去重，和 `RouterOperationLock`、`JobLock` 的键一致，所以同一 endpoint 的克隆连接只锁一次。Prepare 里的 JobLock 去重也是必需的，否则同一个键 TryLock 第二次会失败。组内每次取锁都检查原始的 `parent` 祖先链，不看兄弟锁；排序保证组内不会死锁，`TestRouterOperationGroupOppositeOrder` 已覆盖输入顺序相反的并发获取。半途失败或取消时 `release()` 逆序释放全部已拿到的锁，取锁成功后还会再检查一次 `parent.Err()`。checker 没有特殊放行：调用方已经持有 20 级锁时，每个组成员都照样记违规。返回的 context 只记录最后一把锁，但级别相同、一起释放，不会扩大放行范围。组成员是按顺序逐个取的，写操作每把锁最多各等 10 分钟（上限是 N×10 分钟），和 Python 逐把 `router_operation_lock` 相同。
- **级别**：setup 18 → Agent 19 → router 20 → repo 30。仓库中只有 `setup/store.go` 和 `agents/ipv6` 使用 18/19。Agent 锁（`agent-ipv6`）的所有持有路径（Status、Prepare、验收 worker）都不会反向去取 18，也不会再次取 19。R13 `Host` 在路由器锁下运行，不取 Agent 锁。Prepare 先取 L1 try 锁，再取 20 级阻塞锁，和 R14 sync 一样。
- **8 slots / 2 workers**：预约失败或 Spawn 失败时 defer 释放 slot；worker 的 recover、释放 slot 和 failRun 的顺序正确。超过 5 分钟的旧 run 被新 run 取代以后，旧 worker 在 Agent 锁上串行；它最后写入时会核对 run_id，不会覆盖新 run。
- **Runtime**：`lifecycle.Spawn` 总是 `WithoutCancel`，所以调度器那一轮结束不会取消 worker（已确认 periodic round 的 context 不会传给 worker）。Status/Prepare/Start/settings PUT 都在 RunDetached 中执行，并在 rename、repo 回调前后和设备请求前检查 ctx。关机后 worker 不写入，run 按 5 分钟期限显示中断，和 Python 一致。
- **两级脱敏**：系统密钥（规则代理 token、Agent token、注册密钥）对整个响应生效。用户凭据只作用于诊断字段（label/message/error/reason/details/command/conflicts/evidence/source_note）和以凭据命名的键。`check_token`、id、status、scope、run_id、各类 fingerprint、prepare_interface、interface_index 都不受短密码影响；前端 prepare 只用 `prepare_interface` 和 `check_token`。短密码会把诊断文字，以及 `plan.evidence` 的显示整串替换掉，这和 A21 F4 的折中相同，只影响显示，没有功能依赖。没有发现其他泄漏途径：验收报告在写入前已用捕获的种子投影，HTTP 出口再用当前种子投影一次。
- **坏行与旧字段**：类型异常的 Agent 行（`ipv6_verifications`、`deployments`、`latest_deployment` 不是对象，某条 run 是字符串）会被跳过或当作空值，不会 panic，也不影响其他 Agent。Python 在这些情况下多数会 AttributeError 500；Go 更宽松，没有误把它们当成通过的证据。连接表坏行由 R16 reader 隔离；基线不完整时不用来证明连接是新增的。
- **客户端证据**：和 Python `evaluate`/`_tracked` 的元组、主机名、DIRECT/REJECT 链、IPv6 节点传输、包计数条件逐项一致。旧版 RouterOS 的「地址:端口」合并列查询也一致。Go 的基线要求读取完整，比 Python 更保守。

## 观察（不是发现）

- 持久化证据和 runtime 详情中的非字符串值显示为 `null`/`true`，Python 显示 `None`/`True`。不仿真 Python repr，已登记 behaviour-changes。
- 类型异常的 `ipv6_verifications`（例如数组）会在下一次预约时被新的 map 覆盖；Python 在这种情况下 500 且不写入。
- `params` 不是对象的节点，Go 仍然用节点本身做 probe 候选，Python 会跳过这个节点。不存在 server/port 时两者结果相同，影响可以忽略。

## 需要 L4 实机验证（隔离实验网 + lab RouterOS CHR）

1. 真实 Mihomo Agent 主机的 GET 就绪检查、prepare（转发/RA 持久化）以及重启后的恢复，包括 `tun_recovery` 提示。
2. 关联 lab ROS 的 R11 就绪检查和指纹：ROS 端改了路由后，prepare 返回 `CHECK_CHANGED`。
3. 客户端链路验收：用真实的检测客户端 Agent 发出新请求，`/ip(v6)/firewall/connection` 的元组和包计数要能对上，Mihomo 代理链要能确认 IPv6 节点传输；同时验证 fasttrack 开/关两种情况，以及旧版 RouterOS 的「地址:端口」合并列。
4. 发布后的自动验收：A14 enroll → 5 秒调度器 → 只启动一次；快照缺失时返回可见的失败。验证超过 30 条历史后裁剪的是最旧的记录（F1）。
5. 运营商网络恢复和 PD 前缀变化以后重新检查：检查要变成 stale，或给出新的结论。
6. 同一台 ROS 有两条克隆连接时，prepare 和 R14 规则同步并发：应返回 409 BUSY，或串行执行，不会死锁。

## 定向检查（.32，`RT_HOST=sunbroke@192.168.0.32`）

```sh
rt -- bash -lc 'cd server && gofmt -l internal/agents/ipv6; go test -race -count=1 -timeout=5m ./internal/agents/ipv6 ./internal/api/ipv6 ./internal/server && go test -race -count=1 -timeout=3m ./internal/routers -run "TestOperationKeys|TestRouterOperationGroup" && go vet ./...'
```
末行：gofmt 无输出；`ok agents/ipv6 31.282s`、`ok api/ipv6 3.819s`、`ok server 6.153s`、`ok routers 1.068s`；vet 通过。

Python/Flask 抽查：
```sh
rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/ipv6 && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/agents/ipv6 -run "Python|Flask"'
```
末行：`ok api/ipv6 5.678s`；`TestConfigurationAndTargetsPython`、`TestTupleEvidenceAgainstPython`、`TestAgentTransportAgainstPython` PASS。

## 提交

- `b2d7080fc`：F1 + F2（附测试）。
- 下一个提交：F3（附测试）、本报告、PROGRESS、behaviour-changes。
