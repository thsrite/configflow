# C31 聚合路由与设备备注移植交接

分支：`go-port-c31`。最终变基到本地 `go` 的 `8bf1b189`（包含 A11、N8-3、O3-4）。状态：**ported，等待独立复核**。没有推送、没有子 agent，没有移动 `go-fix-importer-bounds` 或修改 `go-port-a11` 分支。

## 完成范围

11/11 方法完成，剩余 0。注册在 `internal/api/aggregations`，生成器在 `internal/aggregations`，共享备注在 `internal/devicenotes`。

| 方法 | 路径 |
|---|---|
| GET、POST | `/api/aggregations` |
| GET、PUT、DELETE | `/api/aggregations/<agg_id>` |
| GET | `/api/aggregations/<agg_id>/count` |
| GET | `/api/aggregations/<agg_id>/preview` |
| GET | `/api/aggregations/<agg_id>/provider` |
| GET | `/api/aggregations/<resource_id>/health-impact` |
| POST | `/api/aggregations/reorder` |
| GET | `/api/profiles/<profile_id>/aggregations/<agg_id>/provider` |

完整读取了两份 Python 源码及三份指定测试。生成器使用八个有界 worker、配置顺序汇总、冻结的 main、一次性 URI 转换、暂存缓存更新和现有 provider bundle 的整体校验/提交。请求取消不终止 Python 原本会完成的工作。登录规则沿用 C30：全局 provider 路径先要求 JWT，配置空间路径再检查 URL token。

备注实现覆盖规范化、旧策略名称回退、空名隐藏、删除行后的名称保留、revision、准备与保存策略；只改名称不重建设置。repo/oxidns 都切到共享实现。最终检查 agents/routers/deploy，没有剩余私有备注实现或旧调用点。

A11 已合入，因此 C31 差分接上真实 `deploy.Locations`，删除了全部 A11 位置文案延期比较。proddiff 的冻结 provider 输入也已移除，两端独立生成；原 Agent bundle 的 partial 已提升为完整成功。没有改 A11 的实现或分支。所有 manifest 的大小和 SHA256 都先对各自实际交付字节校验，main/provider YAML 再按结构比较，其他文件保持字节比较。

## 发现与修复（按严重度）

- **高，已关闭，移植审计问题**：组合 handler 将带 JSON 的隐式 HEAD 落入 POST/PUT。增加明确方法检查，保持 Python 的 500/不修改目录合同；真实 Flask HEAD 差分覆盖有无请求体、存在/不存在对象、读取与 provider 入口，逐次比较状态。
- **中，已关闭，Python 可靠性问题**：预览在发布后重新读取文件，可能混用另一次发布的节点和本次统计。Go 使用本次已校验快照；替换已发布文件的回归证明结果不受影响。
- **中，已关闭，Python 可靠性问题**：源拉取失败且节点缓存类型损坏时，Python 可吞掉线程异常，成功返回空 provider。Go 拒绝生成并保留旧发布内容。真实 Python 对比明确断言这个命名差异和干运行的零写入。
- **中，已关闭，验收覆盖问题**：proddiff 读取不存在的 `aggregations` 而非 `subscription_aggregations`，样本数误报 0。HTTP/direct selector 均修正，增加枚举回归；现在聚合输出 2/2 成功。
- **低，已关闭，Python 输入问题**：干运行将字符串订阅选择器拆为字符 ID。Go 用原生字符串数组解码并拒绝错误类型；命名差分保留精确拒绝及所有存储比较。正常公共保存合同不变。

没有未关闭的 C31 功能问题。正常输入的状态、JSON 字段、中文消息、生成内容与存储效果一致。上述 Python 修正见 `behaviour-changes.md`。原生 JSON 键顺序/YAML 排版不要求字节相同；只有格式正确的动态时间串、生成的身份/排序 UUID 和原生 regex 诊断后缀做窄处理，并有负向控制。没有修改 proddiff 差异基线或 normalization 规则。

## 覆盖证据

每条路由成功/失败的 Flask↔Go 对比包含响应、导出的数据库文档和 JSON/YAML 文件；未知字段、统计字段剥离、跨配置空间删除保护、旧式重排、认证、格式筛选、错误 provider、全局链激活、重复/保留名称、取消、合并请求和 last-good 均覆盖。main/provider 只做一次 opaque 转换；完整应用注册回归检查 11 方法和冲突路径。

真实 Python provider corpus 比较选择顺序、缓存筛选、正则、main 冻结、引用目标 relay/Static/DIRECT，以及 YAML 易混淆标量和未知协议字段。设备备注对比包含所有公共操作、非法与满容量输入、名称 revision 和只改名的设置对象身份。三份指定 Python 测试的逐项映射在 PROGRESS-config 的 C31 节；C34/A14 的请求包装仍由其所属模块负责，C31 的 bundle 生成已使用真实实现。

## 最终门禁与输出

主要最终门禁：**192.168.0.64**（新 Hyper-V Arch）。自然共享负载，开始 `/proc/loadavg` 为 `26.96 9.50 3.76 28/662 281284`，结束为 `5.81 7.46 6.99 1/415 428643`。未做人工压力实验，未提高 timeout。

实际执行的一条 fail-fast `rt` 命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc '
set -e
cd server
cat /proc/loadavg
test -z "$(gofmt -l internal/ tools/ cmd/)"
go vet ./...
printf "gofmt/vet PASS\n"
if ! CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/aggregations/... ./internal/api/aggregations/... ./internal/devicenotes/... ./internal/repo/... ./internal/oxidns/... ./internal/convert/... ./internal/provider/... ./internal/server/... ./tools/proddiff/generator/ > /tmp/c31-final64-race.log 2>&1; then tail -20 /tmp/c31-final64-race.log; exit 1; fi
tail -14 /tmp/c31-final64-race.log
if ! go test -count=1 ./... > /tmp/c31-final64-full.log 2>&1; then tail -25 /tmp/c31-final64-full.log; exit 1; fi
tail -5 /tmp/c31-final64-full.log
for d in $(go list ./... | rg difftest); do
 if ! CONFIGFLOW_DIFF=1 go test -count=1 "$d" > /tmp/c31-final64-one-diff.log 2>&1; then cat /tmp/c31-final64-one-diff.log; exit 1; fi
 cat /tmp/c31-final64-one-diff.log
done
for target in "FuzzStoredNotes ./internal/devicenotes/" "FuzzAggregationDocument ./internal/aggregations/"; do
 set -- $target
 if ! go test -run "^$" -fuzz="^$1$" -fuzztime=60s -parallel=4 "$2" > /tmp/c31-final64-fuzz-$1.log 2>&1; then tail -20 /tmp/c31-final64-fuzz-$1.log; exit 1; fi
 tail -3 /tmp/c31-final64-fuzz-$1.log
done
cd ..
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
if ! server/scripts/proddiff.sh > /tmp/c31-final64-proddiff.log 2>&1; then tail -6 /tmp/c31-final64-proddiff.log; exit 1; fi
tail -1 /tmp/c31-final64-proddiff.log
cat /proc/loadavg
printf "C31_FINAL64_PASS\n"
'
```

相关包 race 全绿：

```text
ok configflow/internal/aggregations                  1.700s
ok configflow/internal/api/aggregations              1.838s
ok configflow/internal/api/aggregations/difftest     70.150s
ok configflow/internal/devicenotes                   1.029s
ok configflow/internal/devicenotes/difftest           2.889s
ok configflow/internal/repo                        102.580s
ok configflow/internal/oxidns                      103.729s
ok configflow/internal/convert                      89.132s
ok configflow/internal/provider                    165.839s
ok configflow/internal/server                       1.383s
ok configflow/tools/proddiff/generator               1.151s
```

全量普通测试 80 个含测试包通过，其余为无测试包，退出 0。全部 12 个 difftest 启用后均通过：agents 109.684s、aggregations 15.023s、subscriptions 46.609s、devicenotes 0.372s、discovery 1.427s、health 13.262s、httpx 0.985s、mosdns 1.004s、refresh 2.963s、subs 1.055s、substore 4.449s、webdav 13.585s。

```text
FuzzStoredNotes:         2,309,628 executions; PASS; 61.008s
FuzzAggregationDocument:  51,472 executions; PASS; 61.023s
Ran 34 tests ... OK
baseline: new=0 stale=0
5.81 7.46 6.99 1/415 428643
C31_FINAL64_PASS
```

proddiff：aggregation-provider `cases=2 success=2`，agent-deployment-bundle `cases=3 success=3`，没有 partial；`coverage_regressions=0 unmapped_routes=0 self_check=PASS`。仅提高真实观测到的路由/成功输出覆盖下限。

最后补的损坏缓存用例只有测试与差异说明变化，没有新增运行时修改。在 **.32** 完整重跑 C31 differential race，并再次检查 gofmt/vet：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/aggregations/difftest/diff_test.go -- bash -lc 'cd server && gofmt -w internal/api/aggregations/difftest/diff_test.go && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/aggregations/difftest/ > /tmp/c31-final-cache-diff.log 2>&1; code=$?; tail -5 /tmp/c31-final-cache-diff.log; exit $code'
```

```text
ok configflow/internal/api/aggregations/difftest 70.656s
```

前期全量发现的备注 golden 仅比较键顺序，已改为该 helper 的结构比较；首次 sustained aggregation fuzz 的 nil/空切片比较是假阳性，已改用 bytes.Equal 并加入空输入种子。两项修正后上述门禁全部通过。最初 .63 不可达及 rt 脚本更新期语法错误是环境问题，之后已重跑。

## 提交

- `403cd614` 共享备注验证与编辑计划。
- `8cdd6d1e` 真实 Python 备注对比及懒校验修正。
- `9bf1da8c` 11 路由、生成器、provider 接线与生产差分注册。
- `aa2d28c0` 原生选择器、缓存统计兼容、扩大边界覆盖。
- `580ec7f4` HEAD 方法闸门及真实 Flask 回归。
- `225d34ed` 接上 A11、去掉冻结输入、提升独立生产 bundle 覆盖。
- 后续收尾提交包含缓存失败关闭的测试、差异说明与本报告，见分支 HEAD。

最终执行 `git rebase go`，并确认工作区干净；不推送。移植完成，独立复核与合并交给协调者。

## 变基到 O3-4 后的组合复验

上面的含 fuzz 完整门禁在 `0b367ae5` 组合树运行。最终变基到 `8bf1b189` 后，又在 .64 执行以下命令（C31 运行时代码未再修改，解析器 fuzz 证据仍适用）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc '
set -e
cd server
test -z "$(gofmt -l internal/ tools/ cmd/)"
go vet ./...
printf "post-rebase gofmt/vet PASS\n"
if ! CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/aggregations/... ./internal/api/aggregations/... ./internal/devicenotes/... ./internal/repo/... ./internal/oxidns/... ./internal/convert/... ./internal/provider/... ./internal/server/... ./tools/proddiff/generator/ > /tmp/c31-rebase-race.log 2>&1; then tail -20 /tmp/c31-rebase-race.log; exit 1; fi
tail -12 /tmp/c31-rebase-race.log
if ! go test -count=1 ./... > /tmp/c31-rebase-full.log 2>&1; then tail -25 /tmp/c31-rebase-full.log; exit 1; fi
tail -3 /tmp/c31-rebase-full.log
for d in $(go list ./... | rg difftest); do
 if ! CONFIGFLOW_DIFF=1 go test -count=1 "$d" > /tmp/c31-rebase-one-diff.log 2>&1; then cat /tmp/c31-rebase-one-diff.log; exit 1; fi
 cat /tmp/c31-rebase-one-diff.log
done
cd ..
if ! server/scripts/proddiff.sh > /tmp/c31-rebase-proddiff.log 2>&1; then tail -5 /tmp/c31-rebase-proddiff.log; exit 1; fi
tail -1 /tmp/c31-rebase-proddiff.log
printf "C31_REBASE_PASS\n"
'
```

该轮退出 0：相关包 race 全绿（C31 differential 69.600s、repo 100.403s、OxiDNS 103.228s、provider 176.581s），全量普通测试通过，全部 12 个 differential 通过（C31 21.243s，subscriptions 56.465s），最后输出：

```text
baseline: new=0 stale=0
C31_REBASE_PASS
```
