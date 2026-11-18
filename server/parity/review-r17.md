# R17 HR 独立复核（客户端固定 IP 与客户端设备 HTTP 路由）

复核者：新的 Opus 会话，没有参与移植（R17 由 Codex 移植，提交 `8f05e4f0e`、`07644e197`、`6f35de742`）。
分支 `go-opus-review-r17`，从 `go-port-r17@6f35de742` 分出；基底是临时分支 `go-r17-base-v2`
（go + R14 作者版 + 复核后的 R16）。只用 loopback fake 和合成数据，没有接触生产、真实路由器、Agent、DNS 或 PVE；
没有 push，没有开子 agent。只在指定测试服务器（下文 `$R17_TEST_HOST`）跑定向检查，没有跑完整 gate、proddiff 或全仓 race。

范围：完整读了 Python `backend/common/client_fixed_ip.py`、`backend/routers/routeros_fixed_ip.py`、
`backend/routers/ikuai_fixed_ip.py`、`backend/routes/client_devices.py`，以及它们用到的 `client_devices.normalize_change`、
`device_notes.normalize_name`、`route_editor.router_operation_lock`、`RouterOSDriver._list`。对照的 Go 代码：
`routers/fixed_ip_*.go`、`routers/ikuai.go` 的 `writeForm` 拆分、`clients/fixed*.go`、`api/clientdevices`、
`server.go` 注册，以及依赖的 `lockx.Keyed`/`Mutex`、`routers.JobLock`（R14 临时入口）、`routeLocks`（R06）、
`repo/router_guard.go` 的 JobLock 钩子、`clients/discovery.go` 的 Enrich 与最终脱敏、`repo` 的 compat 视图与事务。

## 结论

**0 条 high，0 条 medium，2 条 low（均已修复）**；另有 4 条 low 记录在案（不改代码），1 项待 batch-6 合入后复验。
没有达到「≥ 2 条 high」的回退门槛。R17 可以设为 `verified`（O3-6b 一项合入后复验）。

修复提交：`59c948366`（L1、L2 的修复和三个复核用例）；报告和 PROGRESS 在随后的文档提交里。

逐项核对后确认没问题的部分：

- **驱动层与 Python 逐分支一致。** RouterOS：全表快照、重复/无效 ID 拒绝、最具体 scope、动态转静态与普通新增、
  唯一 32hex marker、丢回包只按「新 ID + 完整标识」认领本次对象、补偿前读回完整稳定字段、注释只认原值或本次值、
  删除结果未知时重读一次、恢复时先排除 ID 复用和 IP/MAC 冲突。iKuai：`static_` 视图、完整字段白名单、
  `cfip_` 10hex tag 认领、DNS/hostname 留空、拒绝（`KindRejected`）不补偿、删除不确定时按 ID 重读、恢复时排除 IP/MAC/tag 冲突。
  Go 额外要求 RouterOS server、iKuai interface/gateway 非空才认定归属（已登记），只会更保守。
- **中文消息逐字一致。** 用脚本提取四个 Python 文件和 Go 三个 messages 文件里的全部中文字符串比对，
  差异只有 `%s` 与 f-string 占位、Go 把 RouterOS 转换摘要拼成一个常量，以及 Go 新增的 fail-closed 文案。
- **锁。** L1 用 R14 的 `routers.JobLock`（`syncJobs`，名为 `router-job`），按 type+去尾斜线 endpoint 去重；
  L2 用 R06 的 `routeLocks`，`RouterOperationLocks` 先整组 try，忙时全部释放、只阻塞等待那一把、拿到后立即放开再整组重试，
  不会在持有一把 L2 时阻塞等另一把，也不会死锁；10 分钟上限后返回 409。**R17 没有新建锁表**（R17 的全部改动里没有新增
  `NewKeyed`/`NewLeafKeyed`/`NewMutex`/`sync.Mutex`/flock）。metadata 提交不改路由器记录，不会触发 `routerGuard` 的 JobLock 钩子自锁。
- **token。** itsdangerous 兼容的 HMAC-SHA1/zlib/时间戳，先验签再解压，解压上限 8 MiB，未来时间戳拒绝（与 itsdangerous 2.x 相同）。
  payload 只放 intent 和每台设备操作的摘要，before-image 不进可解码 token；执行时持锁重建并逐项比对摘要。
- **HTTP。** 五条路由都经 `auth.Require`；状态码映射 Settings→400、Conflict/DeviceChanged→409、其他 RouterError→502，
  与 Flask 的异常类别一致（RouterOS 不符合条件是 `RouterError`→502，iKuai 是 `RouterConflictError`→409，Go 相同）。
  响应全部 no-store。HEAD 走 GET 分支，不进入写事务（作者用例断言 revision 不变）。公开投影（state、discovery、
  preview actions、错误消息）都过共享 `redact`，preview token 本身不过脱敏（避免把 token 里恰好等于密码的子串改坏）。
- **compat 视图。** 预览/执行读的 `GetCompatConfig` 与事务回调里的视图都是 `legacyConfig(..., agents=true)`，
  CAS 比较的设备行、绑定和路由器记录两边来源一致，不会因视图差异把每次执行都判成「被其他页面修改」。
- **关机与断连。** owner 先 `Detach`，设备 I/O 用分离后的 ctx（关机取消、断连不取消）；每次设备调用前、metadata 事务前和
  事务回调里都检查 ctx；关机后不做补偿 I/O、不提交 metadata。R14 M1（lifecycle 的 job ctx 与 Detach 看到关机的时刻不同）的
  修复不在本基底，但 R17 的 Apply 不在 Spawn job 里运行，检查的是自己分离后的 ctx，不受该竞态影响。
- **坏记录与脱敏（R16 规则）。** Enrich 只在读取失败时禁用固定 IP 控件，发现结果照常；RouterOS `list` 遇非对象行整表失败
  （写路径 fail closed，正确）；Enrich 的原因文本在 `Discover` 末尾统一脱敏，跨设备密码引用不会经固定 IP 原因泄露。

## 发现

### 低（已修）

- **L1 — `PUT /api/client-devices` 一次违反两条规则时，报的不是 Flask 报的那一条（Go 引入）。**
  Flask `normalize_change` 的顺序是地址 → 名称 → 原名称 → 两个模式；Go 先对所有字段做 JSON 类型检查，再做值检查。
  例如 `ip:"bad"` 同时 `mode:5`，Flask 报「请输入有效的客户端 IPv4…」，Go 报「DNS 策略只能是…」；
  `mode:5` 同时 `previous_name:5`，Flask 报名称错误，Go 报模式错误。状态码相同（400），文案不同。
  证据：`internal/api/clientdevices/review_r17_test.go` `TestReviewR17ValidationOrderMatchesFlask`（真实 Flask oracle）
  前两个子例修复前失败：`$.message: Go=DNS 策略只能是默认、智能分流或直连。 Python=请输入有效的客户端 IPv4、IPv6 或 CIDR 网段…`、
  `Go=DNS 策略只能是… Python=设备备注最多 80 个字符…`。
  修复：`deviceChange` 按 Flask 顺序逐字段检查，类型错误算作该字段的值错误，最后仍由 `clients.NormalizeChange` 规范化。

- **L2 — 固定 IP 预览请求：非字符串 `profile_id` 返回 400，行内名称类型错误先于身份错误报出（Go 引入）。**
  Flask `normalize_request` 不检查 `profile_id` 类型，随后 `body['profile_id'] != profile_id` → 409「配置空间已切换，请重新载入。」；
  Go 解码失败返回 400「请选择 1 至 64 台设备…」。同一行 `ip` 无效且 `name` 不是字符串时，Flask 报身份错误，
  Go 报名称错误；同 IP 重复的第二行 `name:null` 也一样。
  证据：同一用例的三个 preview 子例修复前失败（`status Go=400 … Python=409`；两次 `Go=设备备注最多 80 个字符… Python=固定 IP 只支持有 MAC…`）。
  第六个子例（apply 的非字符串 `profile_id`）是对照，修复前后都通过。
  修复：`NormalizeFixedIPRequest` 返回未解码的 `profile_id`，由 handler 与 apply 一样按「非字符串或不相等 → 409」处理；
  名称在身份和重复检查之后再解码。用例同时断言校验失败不访问路由器。

### 低（记录，不改代码）

- **L3 — 删除已落到设备、metadata 提交前进程被杀或关机，会留下指向不存在租约的绑定。** 之后「删除固定 IP」预览报
  「静态租约已不存在…不能删除」。与 Python 相同；恢复方法是重新「固定 IP」（新增会覆盖旧绑定，CAS 只比较提交前的值），
  或清空该设备的名称和策略（R16 的 `UpdateDevice` 会一并移除绑定）。新增方向的对称情况（设备已写、metadata 未提交）
  靠 `cf-client:` 前缀在发现里显示为「已归属」，可以直接删除。建议 L5 实测一次并写进操作说明。
- **L4 — `client_fixed_ips` 里只要有一条坏记录，所有固定 IP 保存都会失败。** Go 把整张表经结构体重写，坏记录变成空绑定，
  被 `routerdoc.ValidateBindings` 拒绝，本次设备写入随后被补偿；Python 原样写回坏记录，同样被 `validate_bindings` 拒绝。
  两边都 fail closed，不会写错设备；正常保存路径已有校验，坏记录只可能来自外部导入。如需隔离，应由 repo 导入校验处理。
- **L5 — 配置向导会话里 `completed`/`abandoned` 不是布尔值时，预览/执行返回 500。** Python 按真值判断。
  会话文件只由 C36 写，值总是布尔；这里是 fail closed。
- **L6 — 等待 L2 有 10 分钟上限。** Python 的 `router_operation_lock` 无限等待；Go 超时返回 409「此路由器正在发布或同步，请稍后重试」，
  与 R06 已获批的行为一致，不另登记。

## 待 batch-6 合入后复验（O3-6b RouterJobLock）

本分支 `Apply` 的 L1 用 R14 的临时 `routers.JobLock`（返回 `bool`），与 `wireRuleSync` 注册的 repo 钩子是同一张表；
R12 发布在本基底还没有 L1，靠 L2 与固定 IP 串行。batch-6 合入时：

| 项 | 要求 |
|---|---|
| 入口 | `clients/fixed.go` 的 `routers.JobLock(ctx, profile, id, r)` 改用 `RouterJobLock`（返回 `error`）；忙时仍报 `messageFixedBusy`（「…请稍后重新预览。」，与 Python `apply` 一致），取消错误原样返回 |
| 锁表 | 删除 `syncJobs` 后只剩一张 `router-job` 表；R17 不得再持有第二张 |
| 守卫 | `TestReviewR17RepositoryHookSharesJobLock`：经 `routers.Wire()` 注册的 repo 钩子持锁时，`Apply` 必须报忙且不访问设备，释放后执行成功。两张表并存时它会失败 |

合入后请复跑：`TestReviewR17RepositoryHookSharesJobLock`、`TestReviewR17ShutdownWhileWaitingForDeviceLock`、
`TestR17BatchLocksAndRollback`、`TestR17DistinctDeviceLocksAndMixedVendors`、`TestR17Runtime*`，以及 R14 复核的
`TestR14ReviewRepositoryHookSharesJobLock`。

## 新增复核用例

| 用例 | 内容 | 修复前 |
|---|---|---|
| `api/clientdevices` `TestReviewR17ValidationOrderMatchesFlask` | 6 个多重违规请求，与真实 Flask 比较状态和 JSON，并断言不访问路由器 | 5 失败、1 对照通过 |
| `clients` `TestReviewR17RepositoryHookSharesJobLock` | O3-6b 集成守卫（见上） | 通过（守卫） |
| `clients` `TestReviewR17ShutdownWhileWaitingForDeviceLock` | 他人持有 R06 L2 时在真实 Runtime 下启动 Apply，等待中关机：返回 `context.Canceled`，无设备调用、revision 不变；并断言确实在等待 | 通过（作者没有覆盖「等锁时关机」） |

## 抽查的作者证据

没有重跑作者的全部对照（按「Leaner verification」）；因为修改了 `NormalizeFixedIPRequest` 和 PUT 校验，
跑了这两个包及直接调用者的完整测试，其中包括作者的 15 请求 Flask HTTP 对照、15 组 Python vendor 对照（`TestR17RealPythonVendorBoundaries`）、
token 双向互验、真实 Runtime 断连/关机、批次补偿与删除恢复。读过作者对照的归一化：只放过成功预览的 token、
`cf-client:<name>:32hex` 的 nonce、RFC3339 时间戳和 `cfip_` tag，其余逐值比较，没有发现过宽的归一化。

## L5 实机验证项

1. RouterOS 7.x：`/ip/dhcp-server/lease/make-static` 后 `.id` 不变、除运行时字段外的可写字段不变，随后 PATCH comment 成功；
   丢回包（拔线/超时）后按 marker 认领并补偿；删除恢复时 PUT 完整 before-image 被固件接受。
2. iKuai 4.0.x：`static_` 视图字段集合与白名单一致；`cfip_` 10hex tag 不超长；`hostname` 留空时固件自动填写不影响归属；
   DNS 留空时客户端续租沿用网段 DNS；`add` 丢回包时按 tag 认领。
3. 进程强杀在「设备已写、metadata 未提交」两个方向的恢复（见 L3）：新增方向发现显示「已归属」可删除；删除方向重新固定后绑定被覆盖。
4. 跨路由器批次（RouterOS + iKuai 混合、同一设备的克隆记录）在真实设备上第二台失败时第一台被逆序补偿。
5. 登录开启的面板上，五条路由的鉴权、HEAD `?agent=` 绑定空间、no-store；大租约表（数百条）下发现 + Enrich 的耗时不超过 20 秒预算。

## 定向验证

主机为指定测试服务器，`$R17_TEST_HOST` 即该 SSH 目标。夹具全部合成，网络只连 loopback。

1. 修复前（复核用例）：

   ```sh
   RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && go vet ./internal/clients ./internal/api/clientdevices && go test -race -count=1 ./internal/clients -run "^TestReviewR17" -v && go test -race -count=1 ./internal/api/clientdevices -run "^TestReviewR17"'
   ```

   clients 两个用例 PASS（`2.601s`）；clientdevices `TestReviewR17ValidationOrderMatchesFlask` 5 个子例 FAIL（见 L1、L2）。

2. 修复后：

   ```sh
   RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/clients internal/api/clientdevices internal/routers)" && go vet ./... && go test -race -count=1 ./internal/clients ./internal/api/clientdevices ./internal/server -timeout 10m && go test -race -count=1 ./internal/routers -run "TestR17|TestR06|TestR14JobAndScheduler" -timeout 10m && go test ./internal/clients -run "^$" -fuzz "^FuzzR17FixedRequest$" -fuzztime=60s -parallel=2'
   ```

   退出 0：gofmt 无输出，`go vet ./...` 通过；clients `45.845s`、clientdevices `16.503s`、server `7.017s`、routers `52.667s`；
   fuzz `312810` execs、`PASS`、clients `60.118s`。
