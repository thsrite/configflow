# R11 Go 移植交接

状态：产品 **ported**，等待新的 Opus HR 复核、合并树门禁和 L5。分支 `go-port-r11`，从
`go-opus-review-r10@f368fa1b9` 分出。产品提交 `1c8e874c2`、`214ed3bc4`、`d214fb515`。
当前本地 `go@a9d6ce86c` 尚不包含 R10 review 分支，尚未满足「R10 合入后 rebase go」条件；
协调者已收到通知，后续按 WORKFLOW 的 batch merge 规则变基，不复制别的分支文件。
仅合成数据与 loopback；测试全部通过 rt。不 push、不派子 agent，不触碰生产、真实路由器、DNS 或 PVE。

## 范围和对接

完整覆盖 `ipv6_pd_guard.py`、`ipv6_dns.py`、RouterOS `ipv6_readiness.py`，无本模块功能遗漏；
没有新增 HTTP 路由。R12 的 inspect/stage/release/日志与跨阶段回滚、R14 自动同步、A23 HTTP/缓存/
check_token/prepare/probe 仍由原模块实现，不能把本模块 package 测试算作它们的验收。
逐文件测试归属见 [r11-test-map.md](../r11-test-map.md)。

- `InspectIPv6PDGuard(ctx, r *Router, gatewayHost string, d *RouterOS) (*IPv6PDGuardPlan, error)`；
  `IPv6PDGuardRelevant`；`VerifyIPv6PDGuard`；`ApplyIPv6PDGuard`/`RestoreIPv6PDGuard`。
- `InspectIPv6DNS(ctx, desired *Desired, scope DNSCheck, d *RouterOS) (*IPv6DNSPlan, error)`；
  `VerifyIPv6DNS`；`ApplyIPv6DNS`。scope 是 R12 已确认的 DHCP 目标；预览允许 DHCP pending，
  写入前必须 `VerifyDNS` 成功，且完整 LAN/ND 证据仍相同。
- `InspectIPv6Readiness(ctx, d, r, agent *model.Agent, host IPv6Host, configuration IPv6ReadinessConfiguration,
  dnsTarget, policyContext)` 是 A23 的只读 reader。`FakeIPv6Route *bool` 保留未提供时依 used 的默认；
  不解析域名。旧 additive API `AddMissingIPv6Route`/`UndoIPv6ReadinessRoute` 也已保留，但普通报告只提示发布。
- 两个 plan 存到 `RouterState.IPv6DNSPlan/IPv6PDGuardPlan`；report parts 自动合并。Ops 实现 `Op`，
  check/evidence 强类型；nil check 相当于 Python 的空检查。公共报告没有 ops/proof，返回切片副本。
- 三个 `RouterOS.Apply` kind 已注册；DNS 返回已有 `ros_restore`，PD 返回 `ipv6_pd_guard_restore`。
  `Manifests().IPv6PDGuard` 区分 Set 与显式 null、返回副本；`SetIPv6PDGuardManifest` 不持久化，
  R12 用现有 `SaveRouterPublication` 的 IPv6PDGuard slot 提交。
- 已与 O3-6b 约定：R11 无自己的 Keyed/JobLock，底层设备方法不取 L1；R12/A23 上层 owner 获取唯一
  `RouterJobLock` → `RouterOperationLock`(L2)，传 held ctx，不能重入。6b 与本分支都补了
  RouterOS.Apply 的 Detach 后取消检查，后续变基保留一次即可。
- R12 从 model.Router 转入时必须继续检查 `model.Issues` 对应 ownership/settings 路径，不能将 R5
  fallback 当成已关闭；R11 同时严格解码 raw manifest 的 version/spec/resources/role/path/ID/config。

## 行为和安全

PD 模板的 45 行 source 与真实 Python 逐字节一致，包含当前前缀复活、10×1s pd-valid 钩子、30s
scheduler、旧前缀弃用/到期清理。派生唯一 PD client/池/LAN；共享池按 Mihomo LAN 选择并提示其他 LAN。
完整手动安装只沿用；确认的部分更新按原规格管理整套安装，共用 hook 保留，rollback helper 不碰。
撤回先清 hook、删 poll/watch，再清 runtime state，并二次 sweep；回读还发现 sweep 之后的新 runtime ID。

新 PD manifest 追加完整 stable config；旧 ID-only 仍可读，但必须能认出模板，不能仅靠保留名称。
错版本/表/角色/ID、重复角色/名称、损坏 spec/config 都 fail-closed；未知 document/spec/resource 字段
在更新和重映射中保留。稳定快照排除 run counters、lease timers，保留实际配置，恢复证明不从新读取刷新。

补偿 DELETE 报错只在一次新 GET 返回 404 时按已删除处理（R10 F1）；读取失败不能作为不存在证据。
PD 逐步继续已证明的回滚，按 `(path, ID)` 收集恢复 ID，部分失败也保存；PUT 已证明的新 ID 即使后续
验证 GET 失败也与原配置证明一起保留。内部/整 stage 补偿失败保留尚未补偿的创建证明，成功还原的 hook
不继续登记为新安装；下一次预览可修复未完成安装，而不是收养同名外部对象。未知 PUT 无可信 ID 时保留现场。

DNS 只 PATCH `advertise-dns=no`；专用 ND 优先于 all，不改共享通告。all 下发 DNS 时给复制设置的手动命令。
最后事务读取仍严格验证 dynamic/invalid/disabled（包括 "1" 与 null/unknown），完整配置变化拒写；undo
也不能覆盖外改。DHCP、LAN 地址、RA lifetime/flags、DNS server 两种字段全部保留。

readiness 一次读取路由/RA/DHCP/策略；只读被规则引用的地址列表，复用 R10 policy/forward snapshot，
失败表保持 unknown，不通过重读或空表回退掩盖。没有设备写入和 DNS 查询。返回的 fingerprint 对路由
顺序/lease timer 稳定，真实路由/连接/设置变化会改变它。

明确的 Python 安全修正和原生格式差异均在 [behaviour-changes.md](../behaviour-changes.md)：
完整 PD 证明、部分恢复/回复丢失、late runtime state、scoped 地址列表、完整 readiness undo 证明；
内部 ND list 使用 native JSON 文本、opaque fingerprint 用 native stable JSON，首次切换须重新检查。
没有仿真 Python repr、hash 或异常类。中文业务消息保持一致；raw 请求使用项目既有 native JSON 编码。

## 四条常设项及历次复核类别

- R06 F1：`r06TimeoutRecorder` 记录并透传真实 10s，不覆盖 timeout；F3：原始 GET/DELETE 空 body、无
  Content-Type；PUT/PATCH/script/hook/RA body 与 Basic auth 有原始字节断言，模板字节另对真实 Python。
- R06 F2 / R07 F1 / A24 F1：不新增私有脱敏；设备错误复用 `safeDeviceText`/`internal/redact`；普通
  username=api/rest-api 保留，含已知密码的字段按共享规则隐藏；proof 和设备配置不被脱敏改写。
- R06 F4/F5：ND 接口/ID/flags/server list 严格类型，所有 plan/check/report/op 用 Go structs，无动态 dict
  形状或 JSON round-trip 建类型；opaque future 字段才使用 RawMessage patch。
- R07 F2：网段用 netip/netipx，不拿 range 当身份；F3：readiness 复用快照，只查引用的列表；PD 核心三个
  roles，state 地址列表按 ID/CAS 与 list scope，不反复读取全 GeoIP；F4：public report、manifest、scope
  切片复制；F5：逐对象完整配置证明与唯一性，不只比较数量。
- R08 F1/F2：缺失表/错误 proof 不当空证据；不按配置猜 owner ID。F3：数组初始化、optional check 用指针
  明确，真实 Python 全计划/报告差分；F4：空/危险 LAN 不写脚本；F5：共享 verifyChange 的错误不丢；
  F6：关机取消后补偿也停止，不声称无限补偿。
- R09 F1：部分恢复也重映射，且只保留原证明；F2–F4：PD runtime 确实按 cf-pd 保留名/注释工作，
  不能隔离发布后人为复制保留 namespace 的对象，这和旧脚本一致，纳入 L5；API 核心对象有完整证明、
  唯一保留名与写前核验。F5：按表读取保持 nil/empty 区别，直接复用 R10 evaluator；F6：同关机限制。
- R10 F1：见上述 GET 404 证明；F2：readiness 的 dynamic/connect route 判定复用严格既有 helper，R10
  中文候选逻辑不改；F3：PD 回滚继续所有已证步骤并登记未完成状态；F4：共享默认调用分支不变，已有
  R04–R10 race 通过；F5：已写设备但 drain 后取消的残留仍属 O14，未声称解决；L5-D 增补见下。
- A24 F2 / PORTING 4b：每个写 owner Detach 后立即 ctx.Err，每个设备/manifest commit 前再查；真实
  Runtime.Start/BindRequest/Shutdown 覆盖断开仍完成、关机前后、空 restore 与快照/最终读之后。
- N8-4ef：raw 严格 proof 解码与 model round trip；Set/null 意图独立，未知字段不丢；R12 要保留 Issues。
  N7/O3：R11 无 YAML parser/新依赖/goroutine/flock/独立锁，纯读取可取消，写 owner 用 ctxx。

## 验证

所有测试只跑合成 loopback，未执行真实 RouterOS 语言或真实 RA/DHCP 流量。80 个 Python 序列 +
3 组模板 source 原始字节；原始 devhttp 请求、18 个 Runtime 组合、归属/漂移/部分成功/metadata/SQLite
均通过。`r11-test-map.md` 标出每份 Python 文件哪些部分属于 R12/R14/A23。

1. 最新定向（.64，产品 d214fb515；gofmt 无输出，vet clean）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/routers/routeros.go --back server/internal/routers/ipv6_pd_model.go --back server/internal/routers/r11_safety_test.go -- bash -lc 'cd server && gofmt -w internal/routers/routeros.go internal/routers/ipv6_pd_model.go internal/routers/r11_safety_test.go && go test -race -p 6 -count=1 -run "^TestR11|^FuzzR11" ./internal/routers ./internal/repo && go vet ./internal/routers/... ./internal/repo/...'
# ok configflow/internal/routers 27.887s
# ok configflow/internal/repo 25.853s
```

2. 共享驱动全包（.32，运行代码=214ed3bc4；后续仅 R11 的更严格 ND/state 路径，旧调用分支不变）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal tools cmd && go vet ./... && go test -race -p 4 -count=1 -timeout=40m ./internal/routers/... ./internal/routersplit/... ./internal/devhttp/... ./internal/repo/... ./internal/model/... && go test -p 4 -run "^$" -fuzz "^FuzzR11PDInput$" -fuzztime=60s -parallel=2 ./internal/routers && go test -p 4 -run "^$" -fuzz "^FuzzR11DNSRow$" -fuzztime=60s -parallel=2 ./internal/routers'
# gofmt 无输出；全仓 vet clean
# routers 990.855s; routersplit 56.088s; devhttp 3.948s; repo 448.124s; model 48.181s
# FuzzR11PDInput: 306469 executions, PASS, 61.088s
# FuzzR11DNSRow: 260409 executions, PASS, 60.117s; rt exit 0
```

3. 变异（.64，恢复 ID/部分 undo 修正之后、最后 proof/ND 校验之前；这些变异锚点未变）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/routers/ipv6_pd_write.go --back server/internal/routers/r11_safety_test.go -- bash -lc 'cd server && gofmt -w internal/routers/ipv6_pd_write.go internal/routers/r11_safety_test.go && go test -race -p 6 -count=1 -run "^TestR11" ./internal/routers ./internal/repo && python3 scripts/r11-mutation-gate.py'
# routers 25.021s; repo 21.634s
# result/journal/request/device-table/recovery: DETECTED
# R11 mutation gate: 5/5 detected; sources restored; exit 0
```

4. 最新产品 d214fb515 的补跑（.32，等待 gate 时）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal tools cmd)" && go vet ./... && go test -p 4 -run "^$" -fuzz "^FuzzR11PDInput$" -fuzztime=60s -parallel=2 ./internal/routers && go test -p 4 -run "^$" -fuzz "^FuzzR11DNSRow$" -fuzztime=60s -parallel=2 ./internal/routers'
# gofmt/vet clean
# FuzzR11PDInput: 219613 executions, PASS, 61.040s
# FuzzR11DNSRow: 257393 executions, PASS, 60.140s; rt exit 0
```

最终 gate（.64，产品 d214fb515）：**排队满 900s 未取得 slot，前台 watcher 按要求终止自己的进程组，rt 退出 124**。
开始 UTC 00:27，截止 UTC 00:42；没有执行任何 gate 检查，不能声称完整 race/proddiff/difftest/build tags
在最新树通过。停止后只读核对自身 rt/bash/Python/gate 四个 PID 已全部退出，未改共享槽位或触碰其他任务。
实际 gate 命令为：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GATE_SLOTS=1 GATE_LOGS="$HOME/.cache/r11-final-gate" server/scripts/gate.sh -p 6 FuzzR11PDInput:./internal/routers FuzzR11DNSRow:./internal/routers FuzzR09PolicyEvaluator:./internal/routers FuzzR10IPv6Input:./internal/routers'
```

实际启动时外层加了仅转发 stdout 的前台 Python watchdog：`Popen(start_new_session=True)`，未见
`gate: slot ` 行满900s就SIGTERM自己的进程组、wait后退出124；没有进入后台或结束回合等待。
最后输出：

```text
R11_GATE_QUEUE_TIMEOUT: stopped own gate after 15 minutes without a slot
```

完整门禁由协调者 `cf-gate` 在 batch 合并树上跑。R10 review 尚未合入当前 go，按用户条件暂不变基；
合入后若 batch 改写过 R10 SHA，可用 `git rebase --onto go f368fa1b9 go-port-r11` 只重放本模块提交，
保留两侧文档；再验证共享驱动与 O3-6b 的组合。当前工作树没有未提交产品代码。

## 文件与后续

新增主要产品文件（行数）：ipv6_dns.go 472、ipv6_pd_model.go 525、ipv6_pd_plan.go 309、
ipv6_pd_write.go 450、ipv6_readiness.go 420、ipv6_pd_watch.rsc 45；messages 三份共 155。
修改 RouterOS shared Apply/payload/RESTORE guard 传递/single-row scope、Manifests、RouterState、report parts、
forward_hint 的纯快照求值提取。新增边界/安全测试分别 486/869 行，repo 测试 85 行，两个 Python
reference、mutation 脚本与映射；具体提交用 `git show --stat` 查看。

未决事项只有独立 Opus、batch 完整门禁、满足合入条件后的 rebase 和 L5。已修的 Python 安全问题为
medium（归属/部分恢复/撤回误报）及 low（不必要全表读取、内部原生格式）；当前无已知未修 high/medium。

L5（不在本轮执行）：实际重启/PD 切换/前缀复活、pd-valid 的 RouterOS 值及 10×1s 事件晚到、poll startup；
RA `yes/self/no` 与 dns/dns-servers 形状；所有 PD DELETE 资源端点实际 404；PUT/GET stable 字段一致性；
第二次 sweep 后残余运行时状态；script/scheduler/ND/address-list 的 ID 稳定与权限。还需沿用 R10 L5-D
对恢复 proof 和创建响应的验证，以及 O14 drain 后关机残留核对。未接触任何实验网或真实设备。
