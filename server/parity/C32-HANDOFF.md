# C32 规则路由交回

状态：**ported，待独立复核**。分支 go-port-c32，已变基到本地 go 0f943623。A24 分支未动，不 push、不派子 agent。

## 范围与完成情况

全部 **21/21 method** 完成，routes.tsv 已标 done：全局/显式 profile 规则列表与新增、规则和规则集编辑/删除、batch、compose、reorder、group-name、policy、GET/PUT dns、两个匿名 local provider、match-test、find-duplicates。完整应用注册无 ServeMux 冲突。

复用 C09 语法/排序/lazy matcher、C14 校验/下载/缓存/Mihomo交付、refresh任务状态、C27 DNS/native routing。核心为 native DTO/RawMessage；jsonx 只在存储及旧接口 adapter。中文提示集中 messages.go；owner 调用 lifecycle.Detach，无 handler WithoutCancel、无新后台 goroutine。共享库+profile+DNS 同一事务，提交后缓存失败只警告，保存不回滚。未加依赖。

A19 gateway 和 R01 默认来源读取已接线。经协调者裁定，ComputeDesired/RulesetContent 首参加 ctx，ContentLoader 闭包不变；R01 使用同一 source helper 的 offline 选项，顺序 cache → inline，绝不联网。缺缓存产生与 Python 一致的 blocking warning/不完整 plan。拒绝所有出站的 fake 断言零请求，真实 Python 对比完整 plan 和存储。

唯一延期：**deferred → A16**。A16 必须注册 agents.Hooks.PinnedWithheld。无符合条件的自动同步 OxiDNS 时确定 false；需投影但 hook 缺失/出错，单条脱敏 warning、省略该字段、保存保留。nil/error/true/false/no-device 均有测试；没有跳过其他字段。PROGRESS-agents A16 已注明。

## 发现的问题与差异

| 程度 | 问题及处理 |
|---|---|
| 中，已修 | C27 read-model 把历史数字/数组 Agent 名称丢为 null。Name 改成独立 json.RawMessage，数字、数组、null 与真实 Python 一致，返回字节隔离有回归；协调者批准，C27 PROGRESS 已说明。 |
| 中，Python bug 已修 | HEAD /api/rules/dns 携合法 JSON body 会写 DNS。Go 只允许 PUT 写，HEAD 按 GET；规则/规则集列表 HEAD 也修复 Python 的500。真实 Python 旧写入与 Go 全状态不变均有断言，记录于 behaviour-changes.md。 |
| 低，已修 | C32 重排不能套用其他路由的严格 OrderIDs，错误形状/旧对象数组/数字、null、重复、未知 ID、position 的结果均已差分对齐；排序算法仍复用共享实现。 |
| 依赖已补齐 | A19/R01 原预留 reader 未接线；C32 Wire 已完成。desired.go 仅新增 context import、两个首参和透传，未改变 Desired 字段。 |
| 边界已对齐 | C32 matcher 使用 C14 RuleLines 适配所有11种已接受行分隔；原始缓存及通用 C09 语义不改。 |

没有已知未修的 C32 实现问题。除上述 HEAD Python bug、安全 YAML facade 的既有全局裁定，以及 A16 单字段延期外，没有功能差异。生成 YAML 按结构比较，原始来源/缓存仍逐字比较。未添加生产基线或宽泛差异豁免。

## 验收范围

[完整21方法及7个Python测试文件映射](difftest-c32.md)。所有数据合成，网络仅 loopback fakes。每次请求后比较 SQLite sections/fields 全部行及位置/值、repository export、JSON/YAML/list 文件；成功、失败、只读请求均检查存储。随机 ID 按响应位置一一配对，并核对引用、派生 cache filename/native tag；有效运行时间与计时值归一化。标记的 Mihomo YAML 交付和 native_config 比结构，未标记交付与原始 cache 比字节文本。

覆盖 template/native/custom DNS、follow/policy、explicit/parked/actions、类型保护和删除清理、250条组策略/201条DNS拒绝、远程 compose 200/400/502、通配符和11种行分隔、2万条重复无截断、中央脱敏、原始代理 GET/空 body/Proxy-Authorization、owner取消与并发去重、D1/alias拒绝原子性、全应用注册冲突和解析 fuzz。

## 完整门禁（.64）

实现内容为现历史 c9965a9f（原 0af66561）。协调者批准的最后 R01 context 补丁为 ccab8632（原09577cb1）；按明确裁定，完整 gate 继续跑完，补丁追加相关包 vet/race，不再重复整轮 gate。之后 rebase 的 go 9a208c6c → 5680fbb3 仅增加 HANDOFF/design 文档，没有运行代码变化。

确切命令：

~~~sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/tmp/c32-final-gate -- bash -lc 'cat /proc/loadavg; server/scripts/gate.sh -p 6 FuzzPastedRules:./internal/api/rules; result=$?; cat /proc/loadavg; exit $result'
~~~

提交队列时 loadavg 18.06 / 15.16 / 16.48，脚本等待已有两轮 gate；运行中记录过 16.06 / 15.76 / 15.85，proddiff 自报 start 15.33 / 15.59 / 15.81，end 14.65 / 15.46 / 15.75。最终退出0，结束 loadavg 25.59 / 24.32 / 21.56。

gofmt 无输出，vet、configflow_proddiff/configflow_difftest 双 tag build/vet/lifecycle 检查通过。全量 race：88个包的测试命令通过（difftest 实质执行在后续 CONFIGFLOW_DIFF=1 阶段）；关键包耗时 API rules 16.886s、oxidns 95.383s、routers 442.437s、routersplit 21.648s、server 1.542s。全部13个 live difftest 通过，C32 74.075s（以 diff.log 为准）。最后输出：

~~~text
== difftest
ok configflow/internal/api/agents/difftest 156.685s
ok configflow/internal/api/aggregations/difftest 45.340s
ok configflow/internal/api/rules/difftest 74.075s
ok configflow/internal/api/subscriptions/difftest 105.396s
ok configflow/internal/devicenotes/difftest 1.160s
ok configflow/internal/discovery/difftest 4.555s
ok configflow/internal/health/difftest 15.824s
ok configflow/internal/httpx/difftest 3.519s
ok configflow/internal/mosdns/difftest 4.123s
ok configflow/internal/refresh/difftest 5.836s
ok configflow/internal/subs/difftest 4.175s
ok configflow/internal/substore/difftest 15.041s
ok configflow/internal/webdav/difftest 32.336s
== fuzz (1 targets, 60s each, concurrent)
FuzzPastedRules ok configflow/internal/api/rules 61.064s
GATE PASS
~~~

proddiff：23个 self-check PASS，81个 GET、82个 method-path，计划 cases=2831，实际 compared=693；aggregation-provider 2/2、agent-deployment-bundle 3/3；existing differences=11，**new=0 stale=0 coverage_regressions=0 unmapped_routes=0**。两侧各30个 egress/connection failures 对称记录，未当成功。基线未改。末行：

~~~text
baseline: new=0 stale=0
~~~

FuzzPastedRules：60秒、90,402 execs、182个总interesting inputs，无失败。

## R01补丁追加门禁（.32）

确切命令（back 参数只取回格式化后的文件）：

~~~sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/rules --back server/internal/routers/desired.go --back server/internal/routers/r01_corpus_test.go --back server/internal/routers/n73_yaml_test.go --back server/internal/routers/n73_desired_boundary_test.go --back server/internal/routersplit/n73_plan_test.go -- bash -lc 'date -Is; cat /proc/loadavg; cd server && gofmt -w internal/api/rules internal/routers/desired.go internal/routers/r01_corpus_test.go internal/routers/n73_yaml_test.go internal/routers/n73_desired_boundary_test.go internal/routersplit/n73_plan_test.go && go vet ./internal/api/rules/... ./internal/routers/... ./internal/routersplit/... ./internal/server ./internal/gateway && CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 ./internal/api/rules/... ./internal/routers/... ./internal/routersplit/... ./internal/server ./internal/gateway > /tmp/c32-offline-race.log 2>&1; result=$?; tail -45 /tmp/c32-offline-race.log; cat /proc/loadavg; exit $result'
~~~

退出0。开始 loadavg 21.67 / 23.75 / 25.79，结束 3.08 / 12.39 / 20.44。gofmt/vet 通过，原始输出：

~~~text
ok configflow/internal/api/rules 37.281s
ok configflow/internal/api/rules/difftest 172.626s
ok configflow/internal/routers 399.537s
?  configflow/internal/routers/routerfake [no test files]
ok configflow/internal/routersplit 27.638s
ok configflow/internal/server 1.480s
ok configflow/internal/gateway 29.774s
~~~

## 提交及合并

| 提交 | 内容 |
|---|---|
| 36105ec5 | A16类型化hook合同及延期记录 |
| b0941c89 | 首批16方法、owner/事务/DNS |
| c4a6db6c | compose事务及解析 |
| 8d68d209 | provider、match、compact duplicate scan |
| 71523fbd | 真实Flask/SQLite/文件差分、C27历史名称修正 |
| c3d5eecc | 重排/HEAD/交付/代理/恶意输入边界 |
| c9965a9f | A19 gateway source loader接线 |
| ccab8632 | R01 context/offline source接线及补充回归 |

最终文档提交在其后，最终 hash 见交回消息。合并顺序按协调者裁定：R08先合 → C32 → R09 rebase。若 desired.go 冲突，C32只保留ctx签名/透传，R08字段全部保留。

## 新增文件行数

目录 internal/api/rules：adapter.go 372，compose.go 407，compose_adapter.go 123，dns.go 149，messages.go 71，model.go 116，read.go 199，read_adapter.go 301，routes.go 201，service.go 457；共2396行非测试代码。

同包测试：boundary_test.go 63，compose_test.go 158，offline_test.go 80，read_test.go 282，registration_test.go 15，service_test.go 151。

difftest：cases_test.go 190，diff_test.go 514，dns_test.go 180，edge_test.go 147，offline_test.go 42，remote_compose_test.go 45，testdata/reference.py 87。

另增 internal/oxidns/ruledns_legacy_test.go 45、internal/server/rules_wire_test.go 27。共享包仅新增薄getter/line framing入口及协作接线，不复制算法。

## 新文件行数

最后变基时 go 又合入独立验证过的 provider 慢渲染修复 0f943623，因此在完整 gate 的13个 live differential 之外，追加了新增的第14个 provider/difftest 及 tag 回归；没有重复整轮 gate。确切命令及结果如下：

~~~sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./internal/api/rules/... ./internal/server ./internal/provider/... && CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/provider/difftest && go test -tags=configflow_proddiff -p 6 -count=1 ./cmd/configflow -run TestProddiffWaitsForFreshProvider && go test -p 6 -count=1 ./internal/api/rules ./internal/server'
~~~

~~~text
ok configflow/internal/provider/difftest 51.543s
ok configflow/cmd/configflow 0.070s
ok configflow/internal/api/rules 1.414s
ok configflow/internal/server 0.035s
~~~

退出0。最后合并顺序保持 R08 → C32 → R09；如 R08 在 desired.go 的新增字段与签名改动冲突，只保留本分支 ctx 签名/透传，保留 R08 全部字段。

路径前缀为 server/internal/api/rules/；计入空行和注释，无行数预算。

| 文件 | 行数 |
|---|---:|
| adapter.go | 372 |
| compose.go | 407 |
| compose_adapter.go | 123 |
| dns.go | 149 |
| messages.go | 71 |
| model.go | 116 |
| read.go | 199 |
| read_adapter.go | 301 |
| routes.go | 201 |
| service.go | 457 |
| boundary_test.go | 63 |
| compose_test.go | 158 |
| offline_test.go | 80 |
| read_test.go | 282 |
| registration_test.go | 15 |
| service_test.go | 151 |
| difftest/cases_test.go | 190 |
| difftest/diff_test.go | 514 |
| difftest/dns_test.go | 180 |
| difftest/edge_test.go | 147 |
| difftest/offline_test.go | 42 |
| difftest/remote_compose_test.go | 45 |
| difftest/testdata/reference.py | 87 |

另新增 internal/oxidns/ruledns_legacy_test.go（45）、internal/server/rules_wire_test.go（27）。改动的共享接口/调用点：agents/hooks.go、rules/syntax.go、rulefetch/content.go、oxidns/ruledns_state.go、routers/desired.go 及4个显式ctx测试调用点、server/server.go。核心10文件2396行，C32 native测试749行，live differential含Python1205行。Python/backend/frontend没有改动。

最终 git 检查：git rebase go 成功；git diff --check 无输出；工作区干净。报告提交为 docs-only，准确最终tip见 git log -1（本报告不自引用自身hash）。不 push。
