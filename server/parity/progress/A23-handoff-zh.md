# A23 IPv6 就绪、客户端证据与验证交接

状态：`ported`；功能与定向检查完成，待新的 Opus 独立 HR 复核。

分支 `go-port-a23`，基底为协调者指定 `go-r17-base-v2@673851659`（含 R14 与复核后的 R16）。没有重复叠入 A22。提交：`3bd37be66` 保存配置/候选投影；`dd58e8664` 主体 owner/接线；最后修正及测试提交在文末记录。未 push、无子 agent；仅在任务固定的第一台测试服务器运行，网络全部为 loopback fake，未访问生产或实验设备。源码、夹具与本报告使用合成标识。

## 实现范围

- `internal/agents/ipv6`：原生 JSON DTO/不可变快照、yamlx 配置解析、ULA 掩码、最多3个保存的直接 TCP 候选；复用 C18 默认模板与 C16 URL 校验/默认值。JSONx 只留在 registry/repo/converter/health 兼容适配层。
- readiness/status：共享 R11 RouterOS 检查、R16 routers.FromConfig/clients.ListDevices、完整 host 证据、草稿/部署差异、私有 `.ipv6-readiness/<sha256(id)>.json` 原子缓存、重查失败保留旧结果、每次重新读取 A13 publication 状态。native canonical digest 按 D2 代替 Python 字节 hash。
- prepare：明确 profile/网关用途，Agent 操作锁、R14 job 锁和共享 router 锁组；重查 token/interface、再读草稿/目标身份后才调用 Agent prepare。只补主机，不写 ROS；保持 tun_recovery 提示和 changed_routes=0。
- verification：settings/result/start/enroll/tick，8 slots/2 workers、按 Agent 串行、Runtime tracked jobs、0/5秒 scheduler、IdleRounds、一次发布只自动验收一次。复用 A14 真 Receipts 与 A13 同一 content checker；使用最终 adapted SHA，不把上传 YAML SHA 当作运行版本。
- 客户端证据：观察先于请求、基线/最多256行/30秒窗口/2秒读取；完整源/目的地址端口、host、TCP、代理链、IPv6 节点传输、ROS 包计数相关。请求 ID 不一致或身份变化不成立证据；不把 HTTP 成功或 watcher 中任意 socket 算作全链路通过。
- 历史兼容：model.Map[json.RawMessage] 保留报告顺序及未知字段；坏 report/router/node/连接行不隐藏健康项；不完整 baseline 保留未知。result GET 的陈旧/过期覆盖只在返回副本，不写旧记录。
- 两级脱敏：系统密钥整份；用户凭据只在诊断和明确凭据字段；完整 peer 与捕获快照先收集再投影，凭据被并发替换后仍不泄漏旧值。状态、ID、scope、check_token 不受短用户密码破坏。

## 共享接口与并发裁定

- O3 `Clarified in A23` 已写：setup session18 → LevelAgentOperation19 → router/device20 → repo30。C36 LevelSetupSession 从19下调18，表驱动覆盖正反向。
- `routers.RouterOperationLocks` 复用 routeLocks，规范化 type+endpoint 排序去重。组内固定顺序、检查原始祖先，返回 context 仍记录20；包外嵌套同级继续报违规。半途失败/取消逆序释放。
- `RouterOS.ReadConnections` 是 R16 clientList 的受限读取：只能 TCP connection 表/必要字段/元组过滤，不导出任意表读取，不放宽共有写驱动。
- `api/agents.ContentChecker` 是原 Manager-owned getter；server 注入 A23，不另建比较缓存。
- `server.wireIPv6` 必须留在 A17/A18 等所有 Manager-init 注册之后；它注册 A14 enrollment、最后的 scheduler hook，并把 R13 `routers.IPv6HostRequest` 接到 `Service.Host`。Host 是 router 已持锁后的叶读取，不反取 Agent 锁。
- 服务 API：Status/Prepare/Probe、VerificationSettings/Result/Start、Enroll/Tick、Host；`*ipv6.Error` 保持 Code/Status/Message。外部公开响应调用 Public；R13 Host 自身完成捕获凭据脱敏。
- A01 history=false 有未读取哨兵，不能直接编码为 native JSON；A23 需要 deploy/report 历史，走真实 history=true。settings 的 capability 预检与保存同属一个写 owner，断连不会在预检阶段中断提交。

## HTTP 与比较证据

五方法登记于 `internal/api/ipv6` 和 routes.tsv。显式 header/query/body profile 必须一致；body≤4096、对象校验、鉴权和 no-store；HEAD 逐路径保持 Python404，零设备 I/O/写入。

- `TestRoutesAgainstFlask`：真实 backend.app/Flask，正常 status/cache/prepare（各侧真实签发 token）/probe/settings GET+PUT/result，以及鉴权、profile、JSON、类型、缺字段、陈旧 token、外部目标、修订号、HEAD、未知操作、长度错误。仅归一化 opaque token/时间；业务字段、中文、状态、Cache-Control严格对照。
- `TestConfigurationAndTargetsPython`：12个真实 Python config/候选输出；`TestTupleEvidenceAgainstPython` 正向与端口/host/直连/IPv6 socket/不完整请求负对照。
- `TestAgentTransportAgainstPython`：真实 requests 与 Go HTTPClient 连接同一 loopback receiver，正常、404、403、坏 JSON、非对象、422、500、超限、连接中断的 code/status/中文 message 对照。另有 raw GET/POST bytes/headers 及3/45/512KiB/NoProxy/NoRedirects原值检查；capabilities2/3原值检查，fake不覆盖生产参数。
- real Runtime 测读取缓存、prepare设备写、verification/shutdown、settings capability预检与提交：断连继续、关机零后续写、已停 owner 拒绝；队列8/并发2、完整 fanout join、过期/late result guard、旧字段保留。
- 真 Receipts.Capture/Load 证明发布快照、最终 SHA、草稿后改 IPv6 off；调度器无需前端即可启动一次、missing snapshot可见失败且不无限重试；只对新且合法 IPv6 发布 enroll。
- server 注册测试证明五方法、HEAD、R13 host owner 和最后的 scheduler 接线。锁组覆盖去重/乱序同序/半途取消/反序并发及外部同级仍违规。

## 行为差异与后续

没有省略 A23 功能。D2 证明值迁移、坏节点/坏计数/不完整连接表的保守证据、脱敏与关机改正在 behaviour-changes.md 登记。用户操作说明补入 doc/ipv6-readiness.md；UI/截图入口沿用。

待协调者安排：临时 R14/R16 基底合入 go 后按通知 rebase；新的 Opus HR 复核；完整 batch gate/proddiff；L4 实验网主机重启、ROS 实流与运营商网络恢复。这里只证明 loopback 协议/相关性与真实 Runtime 生命周期，不把它们写成真实端到端网络验收。

## 定向命令与末行

`A23_TEST_HOST` 在执行环境固定为任务授权的第一台测试服务器 SSH 目标，报告不写主机明文。下面所有命令均通过 rt 远端执行，本机只编辑、git 与轻量读取。

```sh
RT_HOST="$A23_TEST_HOST" rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=3m ./internal/agents/ipv6 ./internal/api/ipv6 ./internal/server && go test -race -count=1 -timeout=3m ./internal/routers -run "TestOperationKeys|TestRouterOperationGroup|TestClient|TestR11Readiness|TestR12IPv6Host" && go test -race -count=1 -timeout=3m ./internal/lockx ./internal/api/setup ./internal/setupflow && go vet ./... && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzSavedIPv6Configuration$" -fuzztime=60s -parallel=4 ./internal/agents/ipv6'
```

已通过末行：ipv6 `29.884s`、api/ipv6 `7.082s`、server `8.058s`、routers `1.274s`、lockx `5.601s`、setupflow `1.797s`；api/setup 无测试文件；vet退出0；fuzz `307891 execs`，PASS `61.063s`。命令前另跑全部拥有文件 `gofmt -l` 无输出（同轮检查）。最初完整 routers 定向包 race 也通过 `549.301s`，后续只复跑关联 reader/IPv6/锁组用例。

新增 Python transport/enroll/automatic tick 三例补跑：`CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=3m ./internal/agents/ipv6 -run "^(TestAgentTransportAgainstPython|TestEnrollOnlyValidNewIPv6Publications|TestSchedulerStartsCapturedPublicationOnce)$"`，PASS `5.975s`；同轮 vet/gofmt 通过。最后 settings preflight 归属修正：

```sh
RT_HOST="$A23_TEST_HOST" rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=3m ./internal/agents/ipv6 -run "^(TestSettingsCapabilityPreflightBelongsToWriteOwner|TestSettingsIndependentClientAndCapabilities|TestQueueBoundAndShutdownDoesNotFinishRun|TestAgentTransportAgainstPython|TestEnrollOnlyValidNewIPv6Publications|TestSchedulerStartsCapturedPublicationOnce)$" && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=3m ./internal/api/ipv6 && go vet ./...'
```

末行：ipv6 `7.647s`、api/ipv6 `6.289s`，退出0；同轮所有拥有文件 gofmt -l 无输出、全仓 vet 通过。设置能力预检中的 disconnect/shutdown 成对测试通过，GET 仍是可取消只读。

## HR 复核清单落点

| 已有问题类别 | A23 的处理与证据 |
| --- | --- |
| R04/R05：过期快照、跨 profile/设备、结果不明时误报成功 | prepare 两次身份/草稿证明、host/interface/token 与 router fingerprints；只调用既有 R11 检查，不写 ROS；Agent prepare 连接失败返回不确定操作结果。客户端/发布快照独立绑定。 |
| R06/R07：raw GET body、生产超时被 fake 隐藏、密码过滤误伤用户名 | loopback 原始 GET 无 body/无 JSON Content-Type、POST 精确 body；包装真实 HTTPClient 只断言3/45/512KiB/NoProxy/NoRedirects而不改参数；capabilities2/3原值断言。共享 redact，用户名不作为凭据。 |
| A19/A21：权限与配置证明、两级脱敏 | panel 不执行主机命令；prepare 需明确网关用途与 host.can_prepare，并受 setup18→Agent19→router20→repo30 保护；系统密钥全响应、用户凭据仅诊断，短密码保留状态/ID/token，捕获旧 peer 凭据也不泄露。 |
| A24/C32：Detach 后仍落库、关机继续写 | real Runtime Start/BindRequest/Shutdown；缓存 rename 前、repo callback 前/后、设备请求前检查取消；已停 Runtime 拒绝 settings/prepare；写任务断连后仍完成。 |
| R10/R11：IPv6 归属、未知读取、密码进入指纹 | 调用真实 routers InspectIPv6Readiness/RouterOS，不复制 plan/write/undo；保留 R11 去密码 fingerprint 与 unknown 读证据；R13 hook 接同一个受限 transport。 |
| R16：混合坏行隐藏健康来源、跨设备凭据 | routers.FromConfig/ListDevices、受限 clientList complete 标志；坏 baseline 不证明新增；坏 report 用 model.Map[RawMessage] 保留；完整 peer/captured seeds 先收集再投影。 |
| 锁与后台生命周期 | 锁组原锁排序去重、逆序释放、反序并发；全局8 slots/2 worker；workers/watchers 都由 Runtime 跟踪且 fanout 全部 join，panic 无敏感日志；调度器0/5秒、IdleRounds、Manager close 清理且换 Manager 后不会继续使用旧闭包。 |

L4 的真实主机重启、ROS 实流/运营商网络恢复仍由协调者安排新的 Opus 在隔离实验网执行；这里没有把 loopback 相关性测试写成真实端到端通过证明。
