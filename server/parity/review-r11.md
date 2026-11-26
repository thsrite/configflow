# R11 HR 复核（IPv6 PD guard、IPv6 DNS、RouterOS IPv6 readiness）

复核者：新的 Opus 会话（未参与移植；R11 由 Codex 移植）。分支 `go-opus-review-r11`，从 `go-port-r11@4cd388b9`
分出，基于 R10 复核分支 `go-opus-review-r10@f368fa1b`（R10 合入 go 后再 rebase）。只用 loopback fake 和合成数据，
未接触生产、真实路由器、DNS 或 PVE；未 push，未开子 agent。

范围：完整读了 `backend/routers/ipv6_pd_guard.py`（635）、`ipv6_dns.py`（232）、`ipv6_readiness.py`（294）、
`testdata/cf-pd-watch-prod.rsc`，以及 `test_router_ipv6_pd_guard{,_integration}.py`、`test_router_ipv6_dns*.py`、
`ipv6_fakeip.hairpin`、`plan.py`/`release.py` 中两个 stage 的接线和 A23 对 `fake_ipv6_range` 的归一化。对照 Go 的
`ipv6_pd_{model,plan,write,messages}.go`、`ipv6_pd_watch.rsc`、`ipv6_dns{,_messages}.go`、`ipv6_readiness{,_messages}.go`、
`ipv6_report.go`，以及共享驱动改动（`routeros.go` 的 `payload`/`payloadError`/single-row 地址列表/`Apply` 入口取消、
`forward_hint.go` 的快照求值、`Manifests()`、`routerfake` 的 `/ip/dns`）。

## 结论

**0 条 high，1 条 medium（已修），1 条 low（已修），另有 4 条 low 记录在案（不改代码）。**
未达到「≥ 2 条 high」的回退门槛。模板 45 行与真实 Python 逐字节一致，DNS 与 readiness 的移植没有发现功能差异；
问题集中在 PD 归属证明的范围。移植者的 80 个 Python 序列全部复跑通过，我另加的 8 个 PD 序列修复前全部与 Python 不同，
修复后完全一致（结果、manifest、设备表、写入序列）。

## 发现

### 中

- **F1 — hook 所在 dhcp-client 的整行配置被当成归属证明：用户改了这个客户端后，保护既不能更新也不能撤回（Go 引入，已修）。**
  场景：ConfigFlow 安装 cf-pd 后，manifest 的 hook 条目保存了 WAN dhcp-client 除运行字段外的**全部配置**，此后每次预览都要求
  逐字段相同。这个客户端是用户自己的对象，ConfigFlow 只拥有它 `script` 字段里的那一行。下列常见操作都会让预览报
  「IPv6 前缀变化保护的受管对象已发生外部变化，保留现场，请核对」，开关开着不能更新，关掉也不能撤回：
  按 readiness 的 DNS 提示关掉 `use-peer-dns`；换 ISP 后改 `pool-name`（此时旧脚本仍监控已不存在的池，**保护静默失效**）；
  把前缀换到另一个客户端（禁用旧客户端，Python 专门为此清理旧 hook 的分支在 Go 里根本走不到）；手动清掉或共用 hook 行。
  手动删掉脚本和定时任务也解不开，因为 hook 证明还在；只能删 WAN 的 DHCPv6 客户端。Python 只按 ID 认 hook，
  钩子内容由 `_hook_state` 决定是否写入，以上情形都能正常更新或撤回。
  证据：`TestR11ReviewPythonProbes`（同一 loopback fake 分别跑真实 Python 和 Go）修复前 8/8 不同，例如
  `review_pool_renamed: $.manifest.spec.pool Python=pool-next Go=pool-example`，
  `review_client_setting_withdraw: tables $./ipv6/dhcp-client[0].script Python= Go=:if ($"pd-valid" = 1) …`，
  `review_client_moved: $.manifest.resources[2].id Python=*C1 Go=*C0`。
  修复：`pdOwned` 对 hook 只按 ID 认（仍拒绝动态客户端），不再比较记录的客户端配置，也去掉 legacy hook 的模板要求；
  每次写 hook 仍由 `pdHookState` 把关，并用预览时的 `expected` 快照防止并发修改。脚本和定时任务的完整配置证明不变
  （它们是 ConfigFlow 自己创建或接管的对象）。behaviour-changes 已补充说明。提交 `9591b838b`。

### 低

- **F2 — readiness fingerprint 的输入包含设备密码（Python 同，已修）。** 这个 fingerprint 会返回给浏览器
  （A23 的 `routers[].fingerprint`），它是对整个路由器记录（含 `password`）做的无盐 SHA-256，拿到它的人可以离线猜密码。
  修复：只对去掉密码的副本求摘要；其他路由器设置或路由变化仍会改变摘要
  （`TestR11ReviewFingerprintExcludesPassword`）。fingerprint 在 Go 里本来就是不透明值，切换时需要重新检查，已登记。提交 `4be368394`。

- **F3 — 受管脚本或定时任务被手动改过后，Go 既不按模板修复，也不撤回（设计取舍，记录）。** Python 只要名字对得上就仍算受管：
  开关开着时按模板改回（例如重新启用被停用的 cf-pd-poll、把 hold 改回 3600），关掉时只要还是监控脚本就删除。Go 用完整配置证明，
  任何外改（包括只改 comment）都以 `messagePDOwnership` 阻断两条路径，提示里不说是哪个对象。用户可以手动删掉这个对象来恢复
  （对象不存在时，开着会重装，关掉会清记录），所以不会卡死；这条已作为「外改保留现场」登记在 behaviour-changes，O12 汇总时
  请维护者确认。风险点：定时任务证明里有 RouterOS 自己生成的 `start-date`，若 RouterOS 升级后改了日期格式会误判为外改（列入 L5）。

- **F4 — 补偿 PATCH 或 RESTORE 的回复丢失时，不用 GET 确认（记录）。** R10 F1 的模式只用在补偿 DELETE 上（`pdGone`），
  readiness 撤销同样用了。补偿 PATCH（还原 hook 或脚本内容）的回复丢失时，会报「回滚未完成」，但 manifest 按已证明的部分
  正确保存，下一次预览按实际状态处理，不会误写。补偿 RESTORE 的回复丢失时拿不到可信的新 ID，按原设计保留现场。只影响报错文字，不改。

- **F5 — 撤回后的回读比 Python 严格，极少数时序下会触发整个 stage 回滚（记录）。** 第二次清理之后，如果 hook 循环里正在运行的
  一次脚本又写出 `cf-pd-last`，Go 回读会报失败，R12 随后回滚这个 stage，重新安装；Python 则报成功，留下一条状态记录。窗口只有
  毫秒级，结果是安全的（用户重新发布即可）。列入 L5 时序验证。

- **F6 — 关机打断写入，已落地的写入不补偿（与 R04–R10 相同的残余，记录）。** `TestR11RuntimeOwners` 已固定：关机后不再写设备、
  不提交 manifest。已创建的 cf-pd 对象下次预览时按模板认作「沿用」或「接管」，不会成为危险孤儿。归 O14 drain 处理。

## 重点核对（未发现其他问题）

1. **PD 前缀变化。** 重启后前缀变化、旧前缀弃用（preferred 0s / valid 10m / 3600 s 后清理）、A→B→A 时复活当前前缀并重新通告、
   pd-valid 的 10×1s 钩子加 30 s 定时任务兜底（覆盖 hook 漏触发），都在脚本里实现。`ipv6_pd_watch.rsc` 与
   `cf-pd-watch-prod.rsc` 只差两行参数占位符；`TestR11TemplateBytes` 对 3 组参数（含中文名）与真实 Python `watch_source`
   逐字节比较。`pdName` 比 Python 的 `NAME` 更严（另拒控制字符），替换只做一遍，参数值不会二次展开。
   派生（唯一客户端、池、多 LAN 跟 Mihomo、/64、advertise）、状态判定、install/withdraw 的变更序列、items/message/summary
   与 Python 逐项一致；改池或改 LAN 时删除 `cf-pd-last` 让脚本重新记录，这一点与 Python 相同。
2. **IPv6 DNS 与 R09 DNS guard。** R11 只 PATCH `/ipv6/nd` 的 `advertise-dns=no`，不读不写 NAT 劫持规则和 Netwatch；
   DNS guard 只处理 IPv4（Python `dns_guard.py` 不涉及 IPv6）。两者的先后、跳过和回滚都在 R12 的 stage 接线里，不在本模块。
   专用 ND 优先于 `all`、共享 `all` 下发 DNS 时阻断并给出可复制命令、DHCP 预览允许 pending 而写入前必须 `VerifyDNS`、
   最后一次事务读取严格校验 flags、两种 DNS 字段都保留，均与 Python 一致或更严（已登记）。
3. **readiness 三值逻辑。** 读表失败时生成 `read_*` 的 unknown 检查项，wan、ra、dns 按依赖表改成 unknown；只要有 unknown，
   总状态至少是 warning，`can_prepare` 恒为 false。Fake-IP 路由在读表不全或下一跳不唯一时只给 warning、不给 plan；
   policy 和同口转发提示复用 R10 的快照求值器，读失败的表保持 nil（unknown），不会重读或当成空表。
   `fake_ipv6_range` 由 A23 按 Python 先归一化（`str(IPv6Network(strict=False))`），A23 移植时必须保留这一步，
   否则非规范写法会被判成「已有路由未能确认」。
4. **增量、预览与执行一致、回滚、部分成功。** PD 在写入前重新规划，并与预览的 changes、spec、owned 做 DeepEqual 比较；
   每个写入带 `expected` 快照。回滚逐步继续，已证明的恢复按 `(path, ID)` 重映射；创建证明只在补偿失败时保留。
   补偿 DELETE 的回复丢失时，只有 GET 返回 404 才算成功（R10 F1）；读失败不算证明（`TestR11PDLostCompensationDelete`）。
   PATCH 和 RESTORE 的情况见 F4。
5. **写入 owner 与锁。** 五个 owner（PD 的 Apply/Restore、DNS 的 Apply、readiness 的 Add/Undo）在 `ctxx.Detach` 后立即检查
   `ctx.Err()`，每次设备写入前和 manifest 提交前（`commitPD`）再查一次。`RouterOS.Apply` 入口也补了检查。
   `TestR11RuntimeOwners` 用真实 `Runtime.Start/BindRequest/Shutdown` 覆盖 18 种组合。新文件没有 goroutine、mutex、
   flock 或自己的锁，与 O3-6b 的约定一致：上层统一取 `RouterJobLock`，底层不重入。
6. **原始字节与测试替身。** `TestR11RawWireAndProductionTimeouts` 用生产 `devhttp` 断言 script/scheduler/hook/ND 的 JSON
   原始字节、GET/DELETE 无 body 且无 Content-Type、Basic auth、带中文和空格的地址列表查询编码；超时记录器只记录真实的 10 s，
   不改写。JSON 用 Go 原生编码（非 ASCII 不转义），这是项目已接受的格式差异。共享驱动改动方面，`/ipv6/firewall/address-list`
   现在归入 single-row 和 scoped 路径，目前只有 R11 写这张表；**R14 若写 IPv6 地址列表，需要知道这一点。**
7. **凭据和地址脱敏。** 设备错误走共享的 `safeDeviceText`/`internal/redact`，用户名保留
   （`TestR11DeviceErrorsRedactOnlyCredentials`）；R11 不写日志。唯一的密码外泄途径是 F2，已修。

## 四条常设检查

① 测试替身不覆盖生产参数（10 s 透传）；② 原始请求字节有专门断言（见上第 6 项）；③ 共享驱动改动只对 R11 的路径生效，
本轮在本分支上用 race 跑了 R09–R11 的 routers 测试和 repo 测试，全部通过；④ Detach 后检查 ctx.Err（见上第 5 项）。

## 需要 L5 实机验证（隔离 CHR，加入 L5-D）

1. 真实重启和 PD 切换：前缀 A→B 时弃用 A，A→B→A 时复活 A 并重新通告；pd-valid 的实际取值，10×1s 钩子在 LAN 地址更新较晚时
   是否赶得上；scheduler 的 startup 兜底。
2. 重启后 `/system/script`、`/system/scheduler`、`/ipv6/dhcp-client` 的 `.id` 是否保持不变（Go 和 Python 都按 ID 认归属）。
3. 定时任务的 `start-date`，以及脚本 source、policy 在 PUT 回复、GET 回读和 RouterOS 升级前后是否保持一致（F3 的误判风险）。
4. 所有 PD DELETE 端点（script、scheduler、address-list、nd/prefix）的回复丢失后，GET 是否返回 404。
5. 撤回时 hook 循环与第二次清理之间的时序（F5）。
6. RA `advertise-dns` 为 yes/self/no 时的实际表现，以及 dns 和 dns-servers 两种字段的形状（字符串或列表）。
7. 沿用 R10 L5-D：恢复对象的 proof，创建响应和后续 GET 的 stable 字段是否一致；O14 drain 后关机的残留核对。

## 验证命令与输出

修复前（`.32`，新探针，运行代码为移植者的 `4cd388b9`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -count=1 -run "TestR11ReviewPythonProbes" ./internal/routers'
# --- FAIL: …/pd/review_client_setting_update   result: $.results[2].blockers lengths Python=0 Go=1
# --- FAIL: …/pd/review_pool_renamed            result: $.manifest.spec.pool Python=pool-next Go=pool-example
# --- FAIL: …/pd/review_client_moved            result: $.manifest.resources[2].id Python=*C1 Go=*C0
# --- FAIL: …/pd/review_client_setting_withdraw / client_moved_withdraw / hook_cleared_withdraw /
#           hook_shared_update / hook_foreign_withdraw（共 8/8 与 Python 不同）
```

修复后（定向检查；按协调者的新流程不跑完整 gate）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/routers; go vet ./internal/routers && go test -count=1 -run "^TestR11" ./internal/routers'
# ok  configflow/internal/routers 11.656s（含 TestR11PythonBoundary 80 个序列和 TestR11Review*）
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal tools cmd)" && go vet ./internal/routers/... ./internal/repo/... && go test -race -p 6 -count=1 -run "^TestR11|^FuzzR11|^TestR10|^TestR09" ./internal/routers ./internal/repo && go test -p 6 -run "^$" -fuzz "^FuzzR11PDInput$" -fuzztime=60s -parallel=2 ./internal/routers'
# gofmt 无输出；vet clean
# ok configflow/internal/routers 92.262s；ok configflow/internal/repo 72.013s
# FuzzR11PDInput：307313 execs，PASS，61.027s
```

改动的函数（`pdOwned`、`InspectIPv6Readiness` 的摘要）在 R11 之外没有调用方，没有改共享驱动。

R11 状态：本复核把 R11 设为 `verified`（L5 仍待做）。
