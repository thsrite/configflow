# R16 客户端设备与发现交接

## 范围

分支 `go-port-r16`，基于协调者指定的 `go`。状态 `ported`，实现与本文同提交，等待独立复核。
移植 `common/client_devices.py`、`client_discovery.py`、`client_discovery_cache.py`、
`routers/ros_clients.py`、`routers/ikuai_clients.py`。不注册 R17 的五条 HTTP 路由，
不实现 DHCP 固定 IP 的预览、写入及发现能力补充；对应扩展点已留给 R17。

## 已完成

- `internal/clients`：typed 设备列表、单行乐观修改、名称与策略合并；名称修改不碰 OxiDNS；
  复用 C26/C31 的校验/策略编辑。保留不认识的配置及固定 IP 字段，移除空记录只删本地固定 IP 记录。
- `internal/routers`：RouterOS ARP/DHCP/LAN 判定、32 台上限/6 worker 主动确认、权限降级、
  ARP→ICMP 回退及复读；iKuai 终端/租约分页、续租年龄和 LAN 掩码读取。
- profile 独立的内存缓存：设置（含凭据）的摘要、60 秒 stale 标记、128 项 LRU、
  Flight 合并、刷新全失败保留最后有效快照、失效中的旧读取不能回填新一代。
- 请求断开由 owner Detach；保留 Runtime 关停取消；工作全部 Spawn 跟踪。
  设备请求自身有时限；缓存不在 L5 锁内做 I/O，无新增文件锁/后台循环。
- 无新增第三方依赖，无生产网络访问，无配置入口变更。

## R17 接口

- `clients.ListDevices(config json.RawMessage)` 可供 Agent readiness 与 HTTP 共用。
- `clients.NormalizeChange(Change)` 校验地址/名称/模式，HTTP 必须先验证完整键集合及字段类型。
- `clients.UpdateDevice(ctx, config, change, baseURL)` 返回完整替换文档；必须在 repo 事务回调内
  读取当前配置并调用，提交时沿用回调 ctx。`errors.Is(err, clients.ErrDeviceChanged)` → 409，
  `oxidns.SettingsError` → 400。owner 已 Detach，repo 对最终提交继续检查关停取消。
- 每个 app 创建一个 `clients.NewDiscoveryCache(nil, nil)`（nil 使用 lifecycle/ClockFrom）；
  测试可注入 Runtime 与 Clock。`Invalidate(profileID)` 供固定 IP apply 前后调用。
- `clients.NewDiscoveryService()` 使用真实驱动；HTTP 传当前 profile 的 AgentHosts/ProfileID/
  Refresh/Cache。`GatewayHost(ctx, agentID)` 接 A01 运行态，返回当前 Agent host。
- `DiscoveryService.Enrich(ctx, reader, reading, routerID)` 留给 R17 固定 IP 适配器；
  同一 reader 实例和 reading，填写各 Sighting.FixedIP。固定 IP 读取失败应只禁用控件，不能让发现失败。
- `routers.ClientReader` 由 RouterOS/Ikuai 实现；`routers.FromConfig` 复用已有容错 reader；
  `Router.StoredRecord` 供签名保留原有字段与旧类型；没有复制 routersOf。
- HEAD 必须走 GET 的只读分支。测试适配器已断言 HEAD 不写配置；正式路由由 R17 注册后再复核。

## 对照与覆盖

`internal/clients/boundary_test.go` + `testdata/r16_reference.py` 执行真实 Flask handler、
真实 Python vendor driver 与 Go driver；两边连同一个合成 loopback HTTP fake。
Go 侧是仅供测试的 HTTP 翻译器，**不代表 R17 的生产路由已经实现**。

对照字段/值/状态/中文消息/Cache-Control；每次请求后比较 profile，最终比较 SQLite fields 的
所有键/值/revision 与用户文件。native_config/template_base_config 按 YAML 结构比较；
checked_at/updated_at 的实际时刻不跨进程比较，SQLite updated_at 验证为合法时间，revision 严格比较。
用户文件逐字比较，只排除 SQLite 文件及确切 `.lock` 后缀。

- `test_client_devices.py`：设备读写、名称独立、策略新增/移除、冲突、地址/名称/模式错误、
  被手改策略拒绝但可改名称、未知字段/固定 IP 保留。auth/profile pinning/发布与请求 JSON 形状属 R17/A13。
- `test_client_discovery.py`：RouterOS 与 iKuai 正常、必需表失败、可选表降级、权限/静默/探测错误、
  LAN fallback/无 LAN、unsupported/partial、主路由自身、缺失/外部/IPv6、最具体覆盖网段；
  Go 专项覆盖并发超时/panic 隔离、Agent 排除、缓存后最新名称与 Agent host、cap/priority、分页和期限。
  已 verified 的基础驱动登录/错误码/会话用例不复制，定向 routers race 包含原有验证。
- `test_client_discovery_cache.py`：6 类行为均有 Go 定时/并发/复制/隔离/失效回归；
  Flask 对照覆盖普通缓存与显式刷新。没有自动后台刷新（Python 实际实现亦没有）。
- `test_oxidns_device_policy_side_entries.py`：共用 C26 ReadPolicies/ApplyPolicies，
  `side_entries` 不丢弃；本模块未修改共用策略实现。
- negative control：正常 vendor 场景必须实际返回 ok 且有发现设备；比较器必须能发现业务字段差异。
- 每个新设备读客户端都检查原始请求字节/头；GET 必须无 body，ping/call 必须有正确 JSON 字段和 Content-Type。

按 PROGRESS 中 Python 测试函数计数（不把参数展开项冒充函数数）：

| Python 测试文件 | R16 边界职责 | 已有验证复用 / 后续模块 |
|---|---|---|
| test_client_devices.py（11） | 7 个读写/冲突/校验/手改保护场景 | 1 个旧名称修复复用 C31；3 个 auth/Agent profile/发布场景属 R17/A13 |
| test_client_discovery.py（35） | 28 个发现/状态/时间/缓存功能场景，以等价边界组合覆盖 | 5 个基础驱动协议/登录/错误场景复用 R04/R05；2 个 Agent profile/固定 IP 写入口属 R17 |
| test_client_discovery_cache.py（6） | 6 类全部覆盖 | 无丢弃 |

## 差异与 findings

- `difftest-routers-findings.md` 无明确归属 R16 的待修项；native-side reference collision 属 C26/R17，
  不在此改动 C26；R17 应继续跟踪该项。
- Python 内部异常的类名不模拟；意外 reader panic 安全返回“读取失败”，日志不含异常 payload。
- RouterOS duration 超出 int64 返回未知（nil），不仿真 Python 任意精度整数，避免溢出。
- R17 的 `client_fixed_ip.enrich` 尚未实现；Python 对照明确只禁用这一补充读取，不忽略比较结果中的其他字段。
- 没有新发现的产品功能差异或未决设计问题；等待独立复核。

## 验证

仅定向检查，固定指定测试服务器；不运行完整 gate、不 push。
主机使用用户指定的主测试服务器。以下 `$R16_TEST_HOST` 为该服务器的 SSH 目标；
不在仓库写入真实主机名/IP。所有命令均从本 worktree 执行。

1. 首轮 `gofmt`、`go vet ./...` 通过。组合 race 命令中的 routers 全量包通过：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/clients internal/routers/client_messages.go internal/routers/clients_ikuai.go internal/routers/clients_ros.go internal/routers/clients_reader.go internal/routers/clients_test.go && go vet ./... && go test -race -count=1 ./internal/clients ./internal/routers ./internal/routerdoc && go test ./internal/routers -run "^$" -fuzz "^FuzzClientDuration$" -fuzztime=60s -parallel=4'
   ```

   routers 末行 `ok configflow/internal/routers 533.967s`；routerdoc `[no test files]`。
   **该组合命令总退出码为 1**：新增 partial 客户端夹具遗漏 username（不是产品失败），因此
   末尾 fuzz 未执行；已修复夹具，clients 完整 race 及 fuzz 在下一条重跑通过。
   此后未修改 routers 产品代码；只追加 ICMP 专项测试。

2. 修正后最终 clients race、60 秒 fuzz、原 Python 回归（退出码 0）：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/clients internal/routers/client_messages.go internal/routers/clients_ikuai.go internal/routers/clients_ros.go internal/routers/clients_reader.go internal/routers/clients_test.go && test -z "$(gofmt -l internal/clients internal/routers/clients*.go internal/routers/client_messages.go)" && go test -race -count=1 ./internal/clients && go test ./internal/routers -run "^$" -fuzz "^FuzzClientDuration$" -fuzztime=60s -parallel=4 && cd .. && PYTHONPATH=. ~/cf-test/.venv/bin/python -m pytest backend/test_client_devices.py backend/test_client_discovery.py backend/test_client_discovery_cache.py -q -p no:cacheprovider'
   ```

   末行：`ok configflow/internal/clients 28.110s`；fuzz `execs: 767788`、`PASS`、
   `ok configflow/internal/routers 61.036s`；Python `71 passed in 24.24s`。

3. 最终整树 gofmt/vet（退出码 0；`--back` 仅取回本任务新增源文件）：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt --no-sync --back server/internal/clients --back server/internal/routers/client_messages.go --back server/internal/routers/clients_ikuai.go --back server/internal/routers/clients_ros.go --back server/internal/routers/clients_reader.go --back server/internal/routers/clients_test.go -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && echo "gofmt and go vet: PASS"'
   ```

   末行 `gofmt and go vet: PASS`。

4. 最后追加的 ARP ping 拒绝→ICMP 静默→ARP 复读 reachable/failed/stale 三条回归，
   连同最终 gofmt/vet（退出码 0）：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt --back server/internal/clients/cache_test.go --back server/internal/routers/clients_test.go -- bash -lc 'cd server && gofmt -w internal/clients/cache_test.go internal/routers/clients_test.go && go test -race -count=1 ./internal/routers -run "^TestClientICMPFallbackAndReread$" && go vet ./... && test -z "$(gofmt -l .)"'
   ```

   末行 `ok configflow/internal/routers 1.091s`。

## 新增文件

| 文件 | 行数 |
|---|---:|
| internal/clients/devices.go | 315 |
| internal/clients/discovery.go | 401 |
| internal/clients/cache.go | 224 |
| internal/clients/messages.go | 12 |
| internal/clients/boundary_test.go | 522 |
| internal/clients/cache_test.go | 173 |
| internal/clients/discovery_test.go | 262 |
| internal/clients/testdata/r16_reference.py | 63 |
| internal/routers/clients_reader.go | 30 |
| internal/routers/clients_ros.go | 404 |
| internal/routers/clients_ikuai.go | 163 |
| internal/routers/client_messages.go | 15 |
| internal/routers/clients_test.go | 223 |

## 下一步

本任务实现与验证已完成，没有未提交的半成品。交协调者安排独立 Codex 复核；
后续 R17 接正式路由、profile/auth/请求形状与 fixed-IP enrich。
