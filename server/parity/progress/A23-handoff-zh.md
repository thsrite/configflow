# A23 IPv6 readiness — 进行中

分支 `go-port-a23`，Paseo worktree `/Users/sunbroke/.paseo/worktrees/302rx1dg/go-port-a23`，从协调者指定 `go-r17-base-v2@673851659` 新建（含 R14 与复核后的 R16）。没有叠入 A22；A22 正由独立 Opus 复核。全部测试固定指定第一台测试服务器，通过 RT_HOST 传入；不在本机跑测试、无子 agent、未 push、无设备访问。

## 已完成

- 阅读 A23 三个 Agent Python 模块、HTTP routes、A23 progress/files，R10/R11/R16 复核与 R14 交接；先前会话已读 AGENT-BRIEF/PORTING/WORKFLOW/Go style/O3/N7-N8。
- `internal/agents/ipv6` 原生公开 DTO、保存 YAML IPv6 配置解析、ULA 范围检查和掩码归一化、有限 TCP 候选选择。使用 yamlx、C18 默认模板，无私有 YAML/parser。
- `TestConfigurationAndTargetsPython`：真实 Python configuration/_probe_targets 正常与代表错误12行、坏 node 行隔离、IPv4/禁用/间接节点过滤；`TestSavedIPv6PrefixNormalization` 固定 R11 所需 canonical prefix。定向 race通过：`ok configflow/internal/agents/ipv6 1.344s`。新 fuzz 目标已写，最终跑60秒。

## 协调者授权的短设计（需实现并写 O3 Clarified）

1. setup session → Agent operation → RouterOS operation → repo。新增 LevelAgentOperation；把 LevelSetupSession 从19降到18，Agent用19，Router用20。表驱动测试正反向，更新C36注释/测试。
2. `routers.RouterOperationLocks` 复用现有 routeLocks，按规范化 type+endpoint 排序去重，组内同级获取合法但外部自嵌套仍违规。半途失败/取消逆序全释放；覆盖去重、顺序、取消、两goroutine反序并发无死锁。
3. 允许薄导出 R16 clientList 的受限连接表读取，保留坏行complete=false；允许 API agents 的 contentChecks.checker getter，由server接给A23，不能复制A13缓存。

## 接下来（顺序）

- 实现上述锁组/受限读取/内容checker getter及测试；`design/o3-concurrency.md` 加 Clarified in A23。
- readiness owner：Agent/native repo adapter、受限Agent transport、R11 InspectIPv6Readiness + R16 FromConfig/ListDevices、status私有缓存、prepare锁/身份/草稿双重证明、probe。
- fresh连接 tuple证据：observe/watch、源/目标/端口/host/协议/实际代理IPv6传输一一对应；复用受限ROS读取；坏行不拖垮健康来源、不把不完整baseline当空表。
- verification settings/result/start/enroll、8 slots/2 workers、真实Runtime tracked jobs、每Agent锁、过期/陈旧报告及未知字段保留、IdleRounds与0/5s调度；复用A14 receipts/content checker及C16 validate_health。
- 五方法HTTP与注册：显式profile多来源一致、method白名单（Python HEAD返回404，绝不写）、4096长度、no-store、鉴权；server接线+路由清单。
- 真实Python/Flask正常与代表错误、状态/存储/Agent原始请求/证据对照；真实Runtime断连继续与关机零写、跨设备凭据、坏报告/客户端/路由器隔离。只跑定向检查，完整gate归协调者。

## 约定与注意

- `routers.InspectIPv6Readiness` 是R11真实owner；配置中的 fake_ipv6_range 必须先归一化。`routers.FromConfig` 是R16已审查的坏行隔离入口。
- A13 checker 目前私有于 api/agents/content_wire.go；需薄 getter给server，不能让native IPv6包import API形成环。
- Go原生canonical digest代替Python JSON字节hash，check_token/fingerprints是不透明证明；对照使用各侧真实签发的token，不写Pythonhash仿真。
- 系统密钥整份脱敏；用户/peer凭据在诊断及公开显示元数据里先收集再投影，结构键/操作字段不能被短密码破坏；不要复制私有脱敏器。
- 尚未完成完整移植，不标ported。
