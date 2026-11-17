# C31 对抗式复核

状态：**`verified`，0 个未决发现**。分支 `go-review-c31`，从作者的 `go-port-c31@1aa5c0cf` 分出；复核者不是作者。无子 agent、无推送、无生产访问，只用合成测试数据；所有测试通过 `rt` 在测试服务器执行。

## 发现与修复

- **F1，高，已修复：handler/生成器脱离了 Runtime 关机取消。** 三个目录 handler 与 `aggregations.Generate` 使用 `context.WithoutCancel`；客户端断开时继续工作是正确的，但关机时源 I/O 不会收到取消，甚至已关闭 Runtime 的目录请求仍能提交写入。handler 现在原样传递 context；目录读取/保存/追加在 `aggregations.LoadCatalog/SaveCatalog/AppendCatalog` owner 边界调用 `lifecycle.Detach`，沿用 repo 的共享事务和请求 baseline，没有复制校验/合并逻辑。`Generate` 同样使用 `lifecycle.Detach`，worker 全部收拢后检查关机取消，持久化路径在准备 bundle 后再次检查，避免把取消后的缓存回退当成成功产物。已经开始的 bundle 原子提交继续采用共享 provider 的提交合同。
  - 回归：`TestReviewCatalogCancellation` 的四个写方法分别验证取消请求仍写入、关闭 Runtime 后拒绝写入且 DB head 不变；`TestReviewGeneratorKeepsShutdownCancellation` 验证 source I/O 收到取消、所有 worker 收拢、结果为 `context.Canceled`；`TestReviewDeliveryShutdownDoesNotPublish` 走 preview/provider 真实 HTTP、prepare 和发布调用链，验证源 I/O 取消、500、旧 provider 字节不变。既有 `TestRouteFallbackCoalescesAndFinishesAfterCancellation` 继续覆盖合并请求、取消 waiter 和后台完成。
- **F2，低，已修复：设备名称 revision 的 Unicode 分隔符编码不兼容。** `encoding/json` 即使关闭 HTML 转义，仍把 U+2028/U+2029 编成转义串；Python 持久化摘要输入保留 UTF-8，因此同一份有效备注得到不同 revision。现在只还原编码器产生的两个分隔符转义，其他 JSON 转义成对保留，字面 `\u2028` 文本不会改变。
  - 回归：真实 Python `TestDeviceNotesBoundary` 新增包含两个真实分隔符和字面反斜杠文本的配置，覆盖全部六类操作及摘要；`FuzzStoredNotes` 增加该种子，并验证 JSON round trip 后 revision 一致。

两项均先在未修复代码上稳定失败，再修改运行时实现。未修复输出（`.64`）：

```text
--- FAIL: TestReviewGeneratorKeepsShutdownCancellation
    Runtime shutdown did not reach source I/O
--- FAIL: TestReviewCatalogCancellation/shutdown/{POST,PUT,DELETE,reorder}
    shutdown wrote catalog: status=200 ... before/after head changed
--- FAIL: TestDeviceNotesBoundary/policy_names_revision/5
    Python=4aa480f7... Go=52fe7978...
```

## 已完成审计

- 完整对照 `backend/routes/aggregations.py`、`backend/common/device_notes.py` 和全部 C31 Go 实现；核对 AGENT-BRIEF、PORTING 4b、WORKFLOW 收尾规则、C31 handoff/进度、C30/A11/R06 发现类别、O3 2.5/O3-4 Detach、N7/N8 D1 与 alias 预算。
- 11 个方法的功能、状态/JSON/中文消息、HEAD 方法闸门、重排、共享资源与 profile 组合、删除保护均有真实 Flask 差分。完整应用注册测试同时加载所有模块，静态 `/reorder`、动态资源、health-impact 和 profile provider 没有注册冲突。全局 JWT hook 在资源解析前执行；profile provider 先解析 profile，再验证 URL token/JWT，与 Python 的 hook/handler 顺序一致；失败鉴权不进入生成/缓存。
- provider/preview 返回本次验证后的不可变结果，不重新读取已替换文件；并发有界八个 worker，全部 join 后按配置顺序汇总；缓存更新经过整个 bundle 验证后才提交。损坏缓存拒绝发布，last-good 保留。沿用作者已登记的 Python 修正，不新增未登记行为差异。
- 全仓搜索设备备注字段、规范化/读取/revision/保存名称：只有 `internal/devicenotes` 持有实现；`repo/compat_oxidns.go`、`oxidns/devicenotes.go`、`oxidns/devicepolicy.go` 是委托边界；agents/routers/deploy 没有遗留私有备注实现。路由和生成器复用 repo、health/check、rules.ResolveNewOrder、subs、refresh、provider、convert/deploy，没有另写共享业务副本。
- C31 新逻辑使用原生 JSON/类型和已有 regex/YAML facade；旧 jsonx 只留在 adapter 协作边界。无 Python 运行时模拟、无新依赖、无 `yamlx.Clone` 复制用户值。provider 输入继续由共享解析器执行 D1/alias 预算；输出 emitter 输入是 Go 自身生成的 JSON，无用户 YAML 节点复制。
- proddiff 两端各自生成 main/provider 和 Agent bundle，没有冻结输入或跨端产物注入；各自对实际交付字节验证 manifest size/SHA256 后比较 YAML 结构，其他成员按字节比较。已有负向控制改 provider port 必须报差异，没有扩大 normalization 或修改 baseline。最终计数与 partial 检查见下。
- 参照 A11/R06 检查了共享 helper、校验多分支、YAML 值语义、错误链、超时 seam 和 wire 合同。C31 不新增设备 HTTP client；拉取沿用 shared refresh/Sub-Store 超时，没有用 fake 缩短运行时参数。无新增用户功能/操作入口，因此无需 doc 截图变更。

## 最终门禁

最终固定主机 **192.168.0.32**。以下为实际 fail-fast 批次；完整原始日志留在该测试服务器 `/tmp/review-c31-final-*.log`。此前 `.64` 专项 race 全部通过（aggregation 1.838s、API 6.709s、C31 differential 155.513s、notes 1.046s、notes differential 4.147s），高共享负载下没有提高测试 timeout。格式闸门第一次在任何测试前发现一个新测试文件未格式化，修正后重新执行整个最终批次。

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/aggregations/review_test.go --back server/internal/devicenotes/notes_test.go -- bash -lc '
set -e
cd server
gofmt -w internal/api/aggregations/review_test.go internal/devicenotes/notes_test.go
test -z "$(gofmt -l internal/ tools/ cmd/)"
go vet ./...
printf "FORMAT_VET_PASS\n"
if ! CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/aggregations/... ./internal/api/aggregations/... ./internal/devicenotes/... ./internal/provider/... > /tmp/review-c31-final-race.log 2>&1; then tail -30 /tmp/review-c31-final-race.log; exit 1; fi
tail -8 /tmp/review-c31-final-race.log
printf "RACE_PASS\n"
if ! go test -count=1 ./... > /tmp/review-c31-final-full.log 2>&1; then tail -35 /tmp/review-c31-final-full.log; exit 1; fi
tail -7 /tmp/review-c31-final-full.log
printf "FULL_PASS\n"
for d in $(go list ./... | rg difftest); do
 if ! CONFIGFLOW_DIFF=1 go test -count=1 "$d" > /tmp/review-c31-final-one-diff.log 2>&1; then tail -30 /tmp/review-c31-final-one-diff.log; exit 1; fi
 cat /tmp/review-c31-final-one-diff.log
done
printf "ALL_DIFF_PASS\n"
for target in "FuzzStoredNotes ./internal/devicenotes/" "FuzzAggregationDocument ./internal/aggregations/"; do
 set -- $target
 if ! go test -run "^$" -fuzz="^$1$" -fuzztime=60s -parallel=4 "$2" > /tmp/review-c31-final-fuzz-$1.log 2>&1; then tail -20 /tmp/review-c31-final-fuzz-$1.log; exit 1; fi
 tail -3 /tmp/review-c31-final-fuzz-$1.log
done
printf "FUZZ_PASS\n"
cd ..
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py > /tmp/review-c31-final-harness.log 2>&1
tail -4 /tmp/review-c31-final-harness.log
if ! server/scripts/proddiff.sh > /tmp/review-c31-final-proddiff.log 2>&1; then rg "^self-check:|^baseline:|^proddiff:" /tmp/review-c31-final-proddiff.log; exit 1; fi
rg "^self-check:|^baseline:" /tmp/review-c31-final-proddiff.log
printf "C31_REVIEW_FINAL_PASS\n"
'
```

批次退出 0，gofmt/vet 无输出。最终关键输出：

```text
FORMAT_VET_PASS
ok configflow/internal/aggregations                 1.453s
ok configflow/internal/api/aggregations             3.203s
ok configflow/internal/api/aggregations/difftest    41.670s
ok configflow/internal/devicenotes                  1.012s
ok configflow/internal/devicenotes/difftest          2.423s
ok configflow/internal/provider                  192.207s
RACE_PASS
FULL_PASS                         # 80 个含测试包，退出 0
ALL_DIFF_PASS                     # 12/12 启用的 difftest 包
FuzzStoredNotes:         111701 executions; PASS; 61.032s
FuzzAggregationDocument: 74987 executions; PASS; 61.054s
Ran 34 tests ... OK
baseline: new=0 stale=0
C31_REVIEW_FINAL_PASS
```

独立差分各包用时：agents 115.644s、aggregations 25.898s、subscriptions 101.842s、devicenotes 1.045s、discovery 4.190s、health 15.100s、httpx 2.605s、mosdns 3.103s、refresh 5.384s、subs 2.888s、substore 9.677s、webdav 28.030s，全部 `ok`。

随后通过同一主机的 `rt --no-sync` 读取 `/tmp/review-c31-final-proddiff.log` 的 JSON summary，并严格断言两行的 `(cases,success)` 分别为 `(2,2)`、`(3,3)`、不含 `partial` 字段，同时四个回归计数为 0：

```text
aggregation-provider:    cases=2 success=2 both-error=0 compared=2
agent-deployment-bundle: cases=3 success=3 both-error=0 compared=3
new=0 stale=0 coverage_regressions=0 unmapped_routes=0 self_check=PASS
```

最后只整理新增 HTTP 测试文件的 import 分组，再次运行：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/aggregations/review_test.go -- bash -lc 'cd server && gofmt -w internal/api/aggregations/review_test.go && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... && go test -count=1 ./internal/aggregations ./internal/api/aggregations -run TestReview && printf "C31_REVIEW_STYLE_PASS\n"'
```

该次也退出 0：aggregations 0.036s、API 0.343s，最后输出 `C31_REVIEW_STYLE_PASS`；gofmt/vet 无输出。

## 提交与交回

运行时/测试/进度/本报告形成独立复核提交，使用明确路径提交，最后 `git rebase go`。最新 `go` 已合入作者 C31，原版与复核起点的运行时代码完全相同，仅多了两份流程文档更新；变基会跳过已合入的作者提交。复核分支 `go-review-c31`；提交 ID 见交回消息及分支 HEAD。新增测试文件为 `aggregations/review_test.go`（61 行）和 `api/aggregations/review_test.go`（153 行）。没有未移植功能、没有未关闭 C31 问题，无推送。
