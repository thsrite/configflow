# R15 路由器 HTTP / 策略工作区交接

状态：**ported，待 fresh Opus HR 独立复核**。分支 `go-port-r15`；按协调者指定从 `go-opus-review-r14@a08a120b2` 新建。未 push，无功能删减。尚未收到 batch-6 rebase 通知，因此保留指定基底；统一 O3-6b RouterJobLock 进入后应替换临时入口。第一步提交 `f7931b2a4`；最终提交号见交回消息和分支 log。

## 实现与依赖

21 method/path 全部注册，routes.tsv 标 done：routers CRUD/probe/report/plan/apply、手动 routes 列表/plan/apply、policy 查看、policy-settings GET/PUT/preview、rule-sync GET/PUT/POST、gateway-forwarding GET/POST、agents upstream-routers GET。正式 server 注册已接入。

- `internal/routers/management*.go`：创建事务、设置/删除 C03 CAS、仅 last_probe replacement、R06 手动路由 owner、原子工作区和同步设置；写 owner 自己 Detach，adapter admission 也保护写入前的 metadata preflight。
- `internal/routers/workspace.go`：typed workspace、C22/C31 来源策略/命名、native revision、原子 save。完整 stored record 做 CAS；last_probe 不改变 draft revision；names_revision 独立保护来源名称。legacy C03/C22 接缝集中在 management_repo.go，不复制 validators。
- `internal/api/routers`：HTTP shape/status/header/auth 与协作者连接。R12 Inspect/BuildPlan/Publish、R13 app-owned PendingCache、R14 StartJob/receipt、A01 ManagerAgentSource、A20 host.Bridge、C22 DNS generation、C31 names 与 shared redact 全部真实接线。
- core API 只暴露所需的原生记录/工作区/management 方法；不创建第二个发布执行器、驱动、设备锁或调度器。R15 不直接修改 manifests，设备发布及 publication 留给 R12。
- 同空间公开响应的凭据种子来自完整 config；global upstream 的种子来自它可展示的全部配置空间。Management 日志/诊断也保护 peer 和旧凭据；公开 projection 与私有 CAS/存储分开。

无新依赖，无新的后台 goroutine/周期任务，无 flock。没有变更前端或新增操作入口；现有 Python UI/操作文档继续适用。

## Step B 与 O6 处理

- **HEAD_CREATES_ROUTER**：collection 只有 POST 进入创建分支。合法写 body 的 HEAD 不改 DB/revision、不返回 body。
- **HEAD_PERSISTS_FORWARDING**：只有 POST 调用 PersistForwarding；HEAD 执行 GET。测试比较 metadata/JSON/YAML 文件并断言没有 Agent POST；相同 body 的 POST 正控制确实走真实 wire，body bytes 为 `{}`。
- **坏 verinfo / Location**：复用 R05 KindInvalidResponse，probe 捕获全部 RouterError 并保存失败，仅改 last_probe、revision 恰好 +1。真实 Flask 在两类输入上 HTML500且保留旧 probe；Go 为结构化502并保存失败。没有将坏响应当成空成功。
- **坏 Agent 报告隔离**：复用 A20 已修的 Bridge。异常 forward_filter.state[] 只令该条 error，健康网关继续显示；带 peer token 的 router ref 与坏报告凭据不外露。
- **O6 / HANDOFF 3c**：当前 httpx 已是自定义的 Flask 静态段优先/method fallback matcher，不使用 ServeMux 注册这些规则。实际 Werkzeug match 与 Go endpoint 选择证明：gateway-installations/upstream 优先 session handler，i/status 优先短链，POST Mihomo rule-targets 回退 proxy；GET 仍到专用 rule_targets。

该临时基底尚未注册 A22 gateway-installations，Go 测试按其真实 Flask pattern/endpoint 加入匹配合同，其余 handlers 为现有生产注册。完整当前 server 注册通过；**包含 A22 的批次仍需最后一次完整注册复验**。没有用产品占位 handler 冒充 A22。

## HTTP / Python / 存储边界

46 次实际 Flask↔Go HTTP 请求：主序列31、来源/命名/DNS preview8、健康 forwarding3、手动同步4。真实 Flask blueprint、真实 Python repository/vendor driver，生产 Go handlers 与仓库 loopback routerfake；无业务 stub、无覆盖生产 timeout。每步比较状态、JSON、no-store、完整 default profile、system/shared、所有 JSON/YAML 文件，最后比较完整设备表。

覆盖创建/更新/删除、成功探测、报告/计划/确认、手动路由写入、策略对象读取、原子工作区与中文改名、选中 DNS 的生成、foreign target/坏 target 类型、坏源策略/陈旧 revision、规则同步设置、正常202及最终同步结果、健康网关持久化与失败、上游提醒。

另有两个已知 Python probe bug 的实证差异和5条真实 Flask route match。全部21入口鉴权，global upstream 的 `?profile=missing` 未登录仍401。普通 routers 显式无效 profile 遵循已有 C06 前置 profile 解析合同，没有擅改全局 guard。

精确比较例外（没有通配删除响应/文件）：

1. opaque native revisions/fingerprint/control 先检查十六进制形状再归一化；C31 names_revision 和内容版本仍严格比较。新建 router ID 仅接受 router_ + 12hex。
2. 验证 RFC3339/naive ISO 的已知 metadata 时钟；任务 last_duration_ms 验证非负数字。created_at 保持原值比较。
3. 仅 C22 的 oxidns.native_config、template_base_config 与 preview.dns_content 按完整 YAML 结构比较，数组顺序/值保留。
4. R14 私有 receipt `_rule_order` 验证唯一且覆盖规则集合，再由公开 rules 数组严格核对顺序；私有 receipt 缺失 added/removed/log 和 successful-delta Desired 的零值 optional 字段，按明确 0/[]/空字符串补齐。非缺省值不忽略。
5. 202 返回可以已排队、运行中或已完成，每个阶段中文消息必须吻合；仅当两端观察阶段不同才比较为合法异步阶段。随后真实等待两端 owner 完成，严格比较最终公开状态、DB、文件、设备。

## Runtime、锁与持久化保护

- 写入口 L1 先于 lookup/body validation，忙锁409优先；随后 R06 L2，锁后重读完整原始 record。R5 显示默认值不能抹掉 CAS 差异，专门测试两条不同坏字段值解码成相同默认值时仍在建 driver 前拒绝。
- `ManagementService` native transaction callback 接收仓库 held context，未知字段保留；提交点检查 ctx，C03 store 最后检查也保留。同步设置只写两项，probe 只写 last_probe。
- 真实 Runtime 在原始 HTTP transport 进入 App 前 BindRequest，避免误绑已经附着的 default Runtime。probe I/O 中断连继续、关机取消且不提交；publish 写前关机不写、不记成功；写后断连完成；写后关机停止设备 I/O，并由 R12 原 owner 保存唯一获批的中止 publication。fast create 在 Store.BeforeCommit 关机，最终零提交。
- 四种 batch HTTP（profile import、whole import、reset、delete profile）分别在已有归属和第一次设备写入尚未保存 publication 时拒绝，8次请求检查完整 export/fingerprint 不变。不是仅测私有锁 helper。
- 已有归属记录可以旋转 password，保留 manifest/未知字段，清旧 probe；新探测的真实 Basic auth 使用新 password。空 password 保留有效凭据和探测。删除/改管理地址被原有 guard 拦住，设置入口不访问设备。

## Python 测试映射

| Python / 延后项 | 边界证据与归属 |
|---|---|
| test_policy_workspace.py（17） | Flask 来源原子 save、中文改名/模板、DNS/foreign/坏类型 preview、stale revision、无设备 draft；C22 原生成/patch/alias 校验复用已有 owner，snapshot+revision 在本 API 固定并重查。 |
| test_ros_api_safety.py（7） | L1-before-validation、锁后 raw-record guard、真实 HTTP publication/runtime、C03 batch protections、归属删除/credential repair；设备行/manifest/补偿逻辑仍由已验证 R08/R12 执行。 |
| test_router_repository_safety.py（5个延后类别） | 8次跨 API batch 保护、第一次写后窗口、完整 export/fingerprint、真实 credential repair/new Basic auth；policy withdrawal 本体沿用 R08/R12，未复制另一执行路径。Python 跨进程 flock 改由 O3 单进程/共享 L1 合同承接。 |
| test_notifications.py confirmed-empty release、test_operation_logs.py | Flask 空计划确认写 last_release、现有失败清理经 R12 RefreshTasks/C07；日志沿用 R12 release owner，新 Management 诊断单独 peer-secret 测试；notifications/httpx/server 定向 race 通过。 |
| test_profile_clone_router_isolation.py route half | Router/profile选择由 C06/C03 与已有 C34 clone/export/import 承接；R15不补回 clone 丢掉的 routers，scope/foreign target 和 batch import 走真实 HTTP。 |
| test_router_gateway_forwarding.py / A20 | 真实 Flask GET/POST 成功/失败，raw path/header/body、HEAD零写及POST正控、坏报告保留健康条目，A20现有独立回归复跑。 |
| R01–R14 的 route halves | 主 Flask 序列、46个请求与 error/status/auth/context 测试调用实际 owner；原 driver/plan/rollback corpus 不复制；R06/R12 Runtime、R13 review、R14 hook 相关回归复跑。 |

## HR 问题类别自查

- R04/R06 raw wire/timeout：无新设备 client；生产 routerfake/10s 参数未覆盖，Agent forwarding 原始 path/header/body 有证据。共同底层 client/session/TLS/重定向行为复用原模块。
- R05 error class：RouterError/ValidationError/OxiDNS errors 分层映射；显式边界状态优先，report/plan 捕获 RouterError 为502，joint preview 为400，editor Conflict为409。未仿造 Python内部异常文本。
- R05 missing identity/ID reuse/undo：写入/补偿仍经已验证 R06/R12；原始 record CAS、非空 ID、活跃 manifest/delete guard 不放松为显示容错。不做每项全表 JSON round trip。
- A19 YAML graph/空意图：C22/C31 原生节点 owner 负责；本 API 不复制遍历/解析器；设备错误不会变成空成功计划。
- R12：只调用原 Publish，Err 优先于 Status；保留 R12 唯一写后中止提交，不扩大不可中断区；fresh preview/revision/target/连接 guard 保留。
- R13/R16类别：只用已有 R5 decoder，坏记录只影响自身，CAS 用完整原始数据；全局提醒的凭据种子覆盖全部展示空间。has_password 布尔 flag 是合法公开元数据，同名字符串仍脱敏。
- A24/C32：真实 Runtime、I/O响应关机、每个本地提交点取消检查；disconnect 不抛弃已经开始的 mutation preflight/owner。

行为差异已登记 behaviour-changes.md；没有新增依赖或未移植功能。剩余：fresh Opus、协调者批次 gate、A22完整注册复验、O3-6b统一入口与 L5实机。

## 检查命令与尾行

所有命令固定会话指定的测试服务器。`R15_TEST_HOST` 代表该 SSH 目标，本文件不写真实基础设施地址。本机只有编辑/git/轻量读取。格式均在远端 gofmt 后只 --back 本任务文件。

```sh
RT_HOST="$R15_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/api/routers ./internal/redact -timeout 8m'
```

完整 API/隐私回归末行：routers `23.256s`，redact `1.246s`，退出0。

```sh
RT_HOST="$R15_TEST_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/notifications ./internal/httpx ./internal/server -timeout 8m && go test -race -count=1 ./internal/api/agents -run "TestReviewA20|TestStepBA20|TestReviewA13" -timeout 5m && go test -race -count=1 ./internal/routers -run "TestR15|TestR06|TestR12Runtime|TestR13Review|TestR14ReviewRepositoryHookSharesJobLock" -timeout 8m && go test ./internal/api/routers -run "^$" -fuzz "^FuzzR15WorkspaceBoundary$" -fuzztime=60s -parallel=2'
```

格式/vet无输出；notifications `14.472s`，httpx `59.909s`，server `4.896s`，api/agents `28.578s`，routers `57.211s`；fuzz `259045` execs，`PASS`，`60.131s`。全部退出0，未跑完整 gate/full race/proddiff。

补齐 credential/batch HTTP：`go test -race -count=1 ./internal/api/routers -run "TestR15OwnedRecordCredentialRepair|TestR15PublishHTTP" -timeout 4m`，`9.404s`，退出0。

最后增加显式错误状态优先的 mapping 对照后，最终复验：

```sh
RT_HOST="$R15_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/api/routers/handlers.go internal/api/routers/errors_test.go && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/api/routers -timeout 8m && go test -race -count=1 ./internal/server -run TestNewAppRegistersImplementedRoutes -timeout 3m'
```

格式/vet无输出；完整 API `30.020s`，server 注册 `1.040s`，退出0。fuzz输入规范化未变，没有重复60秒运行。没有复制服务器 Go cache 的生成 corpus。

核心非测试产品（含空行/注释）约1.7k行：management/workspace及legacy接缝约717行，HTTP约1k行；另有 shared redact 的10行布尔 presence 修正与 server 注册。没有为行数压缩逻辑。
