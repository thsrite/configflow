# batch-20 复核（O9 第二轮 low 修复、缺失 Agent 404、M18 损坏状态）

复核人：fresh Opus（没有写过被审代码）。分支 `go-opus-review-batch-20`。
范围：`9d187b0c1`（A：O9R2 L1/L2/L3/L4/L6）、`8acf1fc5b`（B：缺失 Agent 返回 404）、`b10b23ba0` + `b091a1827`（C：M18）。
`#40` 镜像那组提交已另行复核，不在范围内。测试只在 `.32` / `.64` 上用 `rt` 跑；没有连接生产，也没有写入真实主机名、IP 或 token。

## 1. 结论

| 级别 | 数量 | 处理 |
|---|---|---|
| high | 0 | — |
| medium | 2 | 已在本分支修复并补测试（B20-M1、B20-M2） |
| low | 3 | B20-L1 已修（B 组引入的回退）；L2、L3 记录 |
| 范围外 | 1 | `51f743c75`（#40）改了 `jsonstore/stat_linux.go`，mosdns 的 5 个 pyref 元数据 hash 失配，预检失败；已刷新（条目零差异，见第 5 节） |

除 B20-M1 收窄了 L3 已批准的保存校验（在简报授权范围内，保留了拒绝换行注入的目的），没有改变其他已批准行为。

## 2. 发现

| ID | 级别 | 位置 | 问题 | 处理 |
|---|---|---|---|---|
| B20-M1 | **medium** | `internal/api/nodes/validate.go` | L3 的保存校验对 `params` 递归字符串值、YAML/JSON `proxy_string` 的参数值一律拒绝 `\n`/`\r`/`\t`。Mihomo 合法的多行参数（`ca-str`、内联证书或私钥 PEM 等）因此无法新建或编辑，旧数据也无法再保存（PUT 会 400）。生成侧（Surge/Loon）本身已经把这些字符折成空格，保存时的整体拒绝没有带来额外的安全收益 | **已修**：`name`/`type`/`server`/`port` 和 URI 分享链接（含 `%0A` 解码）保持严格；结构化 `params` 与 YAML/JSON `proxy_string` 的参数值允许 TAB/LF/CR，只拒绝其余 C0/DEL/C1/NEL/U+2028/U+2029。Mihomo YAML 输出不经过 Surge/Loon 的净化函数，多行值原样保留；Surge/Loon 单行文本继续由生成侧防御兜底（`convert.TestO9R2ClientNodeLineInjectionDefense` 已经覆盖 params 中的 `\r\n`）。选这个方案是因为改动最小：不需要维护“哪些 params 会拼进 Surge/Loon”的字段表，也不需要新增跳过并告警的生成路径 |
| B20-M2 | **medium** | `tools/proddiff/run.py` `strip_discovery_store_state` | M18 的 proddiff 例外除了健康基线 `ok/""`，还会剥离 `damaged` + 非空消息。生产快照上如果 Go 把某个流量文件判为损坏（无论是真实损坏还是 Go 检测误报），差分会被静默吞掉，而误报正好会引导用户点「清除并恢复」删除数据 | **已修**：只剥离 `store_state == "ok"` 且 `store_message == ""`；`damaged` 保留为差异。`test_proddiff.py` 把 damaged 从“放过”改为“必须报差异”。configdiff 和 discovery difftest 的例外本来就只放过 `ok/""`，不用改 |
| B20-L1 | low | `internal/deploy/orchestrate.go:257` | B 把 `RunDirectOperation` 对缺失 Agent 的返回从 `{"success":false,"message":"Agent not found"}` 结果改成 `ErrAgentNotFound` 错误。发布成功后对旧 Agent（< 1.1.0-go）补发重启时，如果 Agent 恰好在这之间被删除，现在走 `fail(e)`，返回 500「Deployment preparation failed」，原来是整体成功，`restart` 字段里带失败对象 | **已修**（协调者要求）：该调用点把 `ErrAgentNotFound` 映射回原来的 `restart` 失败对象 `{"success":false,"message":"Agent not found"}`，整体仍为 200 成功。回归 `api/agents.TestLegacyRestartAfterAgentDeletedKeepsPublishResult`（Surge、Agent 1.0.0，假 Agent 收到 `/api/config/update` 后从注册表删除自己）；旧代码下该测试得到 500「Deployment preparation failed」 |
| B20-L2 | low | `internal/agents/trafficstore/store.go` `InspectStatus` | 只读检测只检查当日桶头（计数器、`hosts` 类型），某个 host 条目损坏只有在一次上报失败后才会进入 `damagedCache`；之后若一次不涉及该 host 的上报写入成功，签名变化使缓存失效，状态又回到 `ok`，直到下一次涉及坏 host 的上报失败。界面可能在 `damaged`/`ok` 之间来回切换 | 检测条件是写入失败条件的子集，不会误报损坏（不会诱导误删）。可接受；如要稳定显示，可在写入成功后保留“该日该 host 损坏”的标记。不影响已批准的 fail-closed 行为 |
| B20-L3 | low | `internal/agents/client.go` `ErrTotalTimeout` | L1 的总时长超时错误文本是英文 `agent request exceeded total duration limit`，经 `remoteConnection` 进入用户可见的诊断详情；单次读超时显示的是 Go 的 `i/o timeout` 原文。两者都不是 Python requests 的原文，Python 也没有总时长上限 | 可接受（新行为，原来也不对齐）。如需统一，可在 `messages.go` 加中文常量 |

## 3. 逐项结论

### A. O9R2 low 修复（`9d187b0c1`）

- **L1 总时长上限**。Agent：`Connect + 5 × Read`（都为零时 10 分钟）；路由器：`6 × timeout`（≤ 0 时 10 分钟）。
  requests 的读超时按单次等待计算，Agent/设备必须在 `Read` 内发出首字节，所以合法操作的耗时是“连接 + 等首字节 + 传输”，总上限比这多出 ≥ 3 × Read 的余量。逐项核对：
  升级 `Seconds(200)` → 1200 s；域名探测 `Seconds(200)` → 1200 s；区域检测 `Seconds(120)` → 720 s；大配置发布 `Pair(10, 120)` → 610 s；rule-sync 快照 `Pair(3, 90)` → 453 s；回程路由 `Seconds(60)` → 360 s；
  日志 `Seconds(5)` → 30 s（局域网内读取日志正文远小于此）；部署默认 `Pair(5, 30)` → 155 s；HA `Pair(5, n)`。
  路由器：RouterOS REST 10 s → 60 s，ping 4 s → 24 s；iKuai 按规则量放大的回滚超时同样乘 6。设备在超时内必须开始应答（看门狗按每次等待计时），剩余的 ≥ 4 × timeout 只用于传输；路由器读取都经过过滤、分页或脚本分块，局域网内的传输远低于这个余量。
  所有 Agent 调用都整体缓冲（`io.ReadAll`），没有流式/长连接调用会被总时长截断。
  超时错误类不变：Agent 仍是 `*TransportError`（调用方的 `IsTransportError` 分支和中文消息不变），设备仍是 `ReadTimeout` 类（「无法连接 RouterOS：ReadTimeout」）。
  规则 4b：`RemoteCall` 和各入口对非 GET 调用 `lifecycle.Detach`；总时长 context 只在 `Do` 内部派生，客户端断开不取消（`TestO9R2NonGETSurvivesClientDisconnect`）。
  没有新的裸 `go`，也没有新增并发豁免。
- **L2 不跟随重定向**。`CallAgent` 加了 `NoRedirects`，3xx 原样返回。`DeploymentRequestModel` 把 ≥ 300 判为失败，缺少消息时沿用 Python 已有的 `Agent returned HTTP <code>` 文本；`RemoteCall`/`FeatureRequestModel` 对非 200 本来就按失败处理；dnsha `call` 对非 200 返回「<名称>：HTTP <code>」类中文提示。`dnsha/wire_test.go` 的断言从 `NoRedirects == false` 改为 `true`，与 monitor/gateway 调用一致，合理。
- **L3**：见 B20-M1。名称/服务器等身份字段与 URI 的严格校验、生成侧净化（Surge/Loon 的节点行、组成员、WireGuard 段）都正确；Mihomo 生成路径不调用净化函数。
- **L4** Agent 模块 `toolchain go1.26.11`：正确。**L6** `installer.ShlexQuote` 取代私有副本：输出一致（`o9r2_l6_test.go`）。

### B. 缺失 Agent 返回 404（`8acf1fc5b`）

- 判定来自注册表读取（`ReadAgent` 返回 nil），存在的 Agent 不受影响（`upgrades_test.go` 有 existing-Agent 500 负控）。
- HA 钩子：`ServiceOperationJSON` 对缺失 Agent 的 `s.get` 返回 `nil, nil`，`active`/`haEnabled` 对 nil 为 false，随后执行 `op()`，`ErrAgentNotFound` 经 `deploy.RunDetached` 原样返回，路由层 `errors.Is` 命中，返回 404。
- Flask 差分：8 条路由都显式断言 Python 500 → Go 404，其他字段不变。例外精确。
- 其他调用方：`RemoteCall` 本身没改；`SetLoggingModel` 只被路由调用；`RunServiceOperation` 还被发布后重启调用，见 B20-L1。

### C. M18（`b10b23ba0`、`b091a1827`）

- 检测只读：`InspectStatus` 只做 `stat` + 必要时读取，不写文件；写入路径的 fail-closed（拒写 500、原文件字节不动）没有变化。
- 缓存失效：健康状态复用 `statusCache`（按签名）；损坏状态存进独立的有界 LRU（按签名 + 日期，当日桶损坏时日期变化即失效）；写入成功后签名变化；`Clear` 调用 `damagedPop`。`rememberDamaged` 写入前重新比对签名，解锁期间文件被改写也不会缓存过期状态。全部在全局 `mu` 下操作，并发安全。
- 检测条件是写入失败条件的子集：计数器只在存在时校验（写入时缺失同样失败），`hosts` 缺失或不是对象写入也失败。所以不会把能写入的文件报成损坏（见 B20-L2）。
- 字段只增不改：`store_state`、`store_message` 追加在 `AgentStatus` 末尾，原有字段的顺序和值不变。
- 前端遵守 DESIGN.md：低饱和危险色提示条 `role="alert"`，`confirmDanger` 二次确认，手机端按钮 `min-h-11`，有损坏时配置区保持展开，并且隐藏重复的图标清除按钮；DESIGN.md 与 `doc/manual/domain-discovery.md`、截图都已同步。
- 差分例外：discovery difftest 和 configdiff 只放过 `ok/""`；proddiff 已按 B20-M2 收紧。

## 4. 修复与测试

| 文件 | 内容 |
|---|---|
| `internal/api/nodes/validate.go` | `paramControlRune`、`hasParamControl`：参数值允许 TAB/LF/CR，URI 来源仍严格 |
| `internal/api/nodes/o9r2_l3_test.go` | 新增拒绝：params 中的 NUL、嵌套 ESC，YAML 中的 NEL，YAML `name` 中的换行；新增接受：结构化 `ca-str` PEM + 多行密码、YAML 块标量 `ca-str`；PEM 存储后逐字节读回 |
| `internal/api/subscriptions/difftest/o9r2_l3_test.go` | 原来的 `params.password` CRLF 拒绝用例改为 U+2028；新增负控：多行 params 的 POST/PUT 与 Flask 逐项一致 |
| `tools/proddiff/run.py`、`test_proddiff.py` | 只剥离 `ok/""`；`damaged` 必须报差异 |
| `parity/behaviour-changes.md` | O9R2-L3、M18 两节同步 |

负控：把 `validate.go` 临时换回 `HEAD` 版本（`.64`），`TestO9R2NodeControlCharValidation` 失败：
`status 400, want 200; body: {"message":"节点字段「params.ca-str」不能包含换行、回车或控制字符","success":false}`。

## 5. 运行记录

提交见交回消息。均在测试服务器上执行。

1. 定向测试（`.64`）：`cd server && go test -count=1 -run TestO9R2 ./internal/api/nodes/ ./internal/convert/` → `ok`；负控见第 4 节。
2. `.64`：`gofmt -l .` 无输出；`go vet ./...` 通过；
   `go test -race -count=1 -p 6 -timeout=60m ./internal/api/nodes/ ./internal/convert/ ./internal/agents/ ./internal/agents/dnsha/ ./internal/agents/trafficstore/ ./internal/devhttp/ ./internal/discovery/ ./internal/api/agents/ ./internal/gateway/installation/ ./internal/deploy/`
   → 全部 `ok`（nodes 31 s、convert 767 s、agents 825 s、dnsha 451 s、trafficstore 1.8 s、devhttp 9.5 s、discovery 19.8 s、api/agents 625 s、installation 92 s、deploy 461 s）。
3. `.64`：`CONFIGFLOW_DIFF=1 go test -count=1 -timeout=60m ./internal/api/subscriptions/difftest/ ./internal/api/config/difftest/ ./internal/api/agents/difftest/ ./internal/discovery/difftest/ ./internal/agents/dnsha/difftest/`
   → `ok` 74.7 s / 63.3 s / 251.6 s / 9.6 s / 23.0 s。
4. `.64`（仓库根目录）：`~/cf-test/.venv/bin/python -m pytest -q server/tools/proddiff/test_proddiff.py` → `60 passed, 109 subtests passed`；
   `cd server/tools/configdiff && pytest -q test_compare.py` → `157 passed, 8 subtests passed`。
5. 前端（`.64`）：`cf-test typecheck` → 退出码 0；`cf-test frontend tests/domain-discovery-loading.test.ts tests/domain-discovery-phone.test.ts` → `31 passed`。
6. 完整预检（`.32`，空闲）：第一次 `PREFLIGHT FAIL tests`，只有 `internal/mosdns` 的 5 个 pyref 报 `source/input/venv hash mismatch`。
   `PYREF_MODE=record` 显示唯一变化是 `PYREF source server/internal/jsonstore/stat_linux.go`（`51f743c75` 把 `Sec*1e9+Nsec` 改成 `Nano()`），
   `TestCorpus` 0 differ、`TestRecordedPythonTests` 0 differ，条目数不变；随后 `PYREF_MODE=record PYREF_ACCEPT=1` 刷新，`go test ./internal/mosdns/` → `ok`。
   重新预检：`bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-opus-review-batch-20 -p 8` → 两组 vet 通过，`PREFLIGHT PASS`。

7. B20-L1 修复（`.64`）：`gofmt -l internal/deploy internal/api/agents` 无输出，`go vet` 通过；`go test -race -count=1 -p 6 -timeout=60m ./internal/deploy/ ./internal/api/agents/`、`CONFIGFLOW_DIFF=1 go test ./internal/api/agents/difftest/`（`.32`，负载约 15）→ `ok deploy 797.8s`、`ok api/agents 1182.2s`、`ok api/agents/difftest 209.1s`。负控：临时换回 `HEAD` 的 `orchestrate.go`，新测试 `status 500, want 200`。

没有运行完整门禁 `gate.sh`（按简报，由协调者在批次树上跑）。
