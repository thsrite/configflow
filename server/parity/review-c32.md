# C32 对抗式复核

状态：**verified，0 个未决发现**。两项发现（1 高、1 中）均已修复。分支 `go-review-c32`，从作者
`go-port-c32@c3e4e9ac` 分出；起始工作区干净，复核者未参与移植。
无子 agent、无 push、无生产或设备访问；所有输入合成，所有测试通过 `rt`。

## 发现与修复

- **F1，高，已修：Runtime 关机取消后仍可写入。** owner 虽然调用
  `lifecycle.Detach`，却不检查保留下来的关机取消。共享 repo 的旧锁在
  lock 可用和 Done 同时就绪时可能选择 lock，不能代替 owner 的取消检查。
  停止 Runtime 后，create、mutate、delete、rename、policy、reorder、batch、DNS、
  compose 九类 owner 均实测仍可提交；这与 C31 的关机边界属于同类问题。
  现在 owner 在 Detach 后检查取消，profile/共享组合事务在进入和编辑完成时检查取消。
  Local/Match/FindDuplicates 同样拒绝已停止的 generation；下载结束检查取消，
  取消错误不会再进入镜像回退或缓存成功路径。没有改写 repo、C14 或 lifecycle。
  已进入原子提交的事务仍遵循共享 repo 的提交合同。
  - 回归 `TestReviewWriteCancellation`：九类写入各验普通请求取消仍提交，
    Runtime 关闭后重复 16 次均返回 `context.Canceled`、DB head 不变。
  - `TestReviewStoppedReadOwners`：三个缓存/扫描 owner 在关机后拒绝执行。
    `TestReviewShutdownDuringDownload`：网络成功响应与关机竞争时，match 来源不落缓存、
    不访问镜像；compose 不写共享库/profile/DNS。完整 export 与缓存均检查。
- **F2，中，已修：batch 把 JSON null 作为空字符串接受。** `[]string` 解码不会拒绝
  null 元素，`{"rule_type":"MATCH","domains":[null],"policy":"DIRECT"}`
  在 Go 返回 200 并写入空 MATCH，Python 返回 400 且无写入；
  DOMAIN 的混合数组则返回不同中文消息。改用 `[]*string` 在语法校验前逐项拒绝 nil，
  继续使用共享 C09 语法校验。真实 Flask 回归 `TestReviewBatchNullValues`
  比较 status、完整 JSON（包括“请提供规则值列表”）、SQLite sections/fields、
  export 和 JSON/YAML/list 文件，不放宽 normalization。

修复前输出（.64）：

```text
TestReviewBatchNullValues/MATCH: Python=400 Go=200
TestReviewBatchNullValues/DOMAIN: Python message=请提供规则值列表
                                Go message=第 2 行：规则值为空。
TestReviewStoppedRuntimeRejectsWrites/{create,mutate,delete,rename,policy,reorder,batch,compose}:
    attempt=0 err=<nil> changed=true
TestReviewStoppedRuntimeRejectsWrites/dns: attempt=1 err=<nil> changed=true
```

## 已完成审计与补充验证

- 完整读取 `backend/routes/rules.py`，核对 profile/ruleset 别名、rule_syntax、reorder、
  matcher、rule DNS、repository 和 rule fetch/cache 调用链；对照 C32 全部 Go 文件。
  已读 AGENT-BRIEF、PORTING 顶部、WORKFLOW、C32-HANDOFF、difftest-c32、
  PROGRESS-config C32（R01/A16 合同）以及 review-c30/c31/a12 的问题类别。
  HANDOFF 3c 的 ServeMux 动态/静态冲突由完整 `server.NewApp` 注册测试覆盖。
- 21 方法的成功/失败、中文、JSON、状态及落盘均有真实 Flask 差分。
  新增 `TestReviewAuthenticationAndProviderTokens`：两个 provider 对缺省/正确配置 token/
  错误 token/当前及退役内部 token/空 token、错误 JWT 和内部 Bearer 保持匿名 200；
  其他 19 个 method/path 的无 JWT、仅配置 token、错误 token、当前/退役内部 token 均 401。
  token 不授予这些管理路由权限；有效 JWT 的成功矩阵继续由作者测试覆盖。
- 组合事务提交失败另经 `TestReviewComposeCommitFailureIsAtomic` 在真实 SQLite
  `BeforeCommit` 注入失败，并重新打开 repo 检查共享库/profile/DNS 整体未变。
  作者原有缓存写失败测试继续证明“已保存 + warning”，不回滚成功保存。
  并发去重、DNS native/custom/template、多模式/删除保护和超限分支继续执行。
- 新增 `TestReviewSourceTimeoutAndMirror`：match 来源下载的连接和读取超时均为
  30 秒，GET 保持原始内容；普通 HTTP 失败才回退镜像，内容校验错误不回退；
  镜像字节只落实际镜像的声明缓存，不污染主来源。真实 HTTP 的无效来源 422、
  普通失败空结果及 lazy short-circuit 继续由 `TestRemoteProviderAndLazyMatching` 对照 Python。
  30 秒沿用 Python requests 的连接/读取超时语义，没有改为整段下载总时限。
- PinnedWithheld 仅在 A16 hook 缺失/失败时省略字段，保存保留，warning 为固定常量，
  不附带 error、规则内容、URL 或凭据；已有 nil/error/true/false/no-device 测试断言
  warning 数量与无泄漏。A16 的唯一单字段延期合同保留，不新增未实现功能。
- C27 `RuleDNSAgent.Name` 的 RawMessage 修复保留数字/数组/null 和独立字节，
  原 C27 PROGRESS 已登记，C32 live diff 与 C27 legacy tests 覆盖。
  HEAD DNS 意外写入以及 HEAD 列表 500 的 Python bug 修正已在
  `behaviour-changes.md` 登记，真实 Python 旧写入和 Go 全状态不变均有断言。
- R01 `ComputeDesired(ctx, ...)` reader 固定 offline：cache → inline → empty，
  在任何下载或镜像前返回；拒绝所有出站的 transport 断言请求数为零，
  完整缺缓存 plan/blocking warning 与 Python 对照。rebase 保留集成分支字段。
- 无新增 Python 模拟或依赖；JSON 用原生 RawMessage，旧 jsonx 仅在接口 adapter。
  语法/排序/matcher 复用 C09，校验/抓取/缓存/交付复用 C14/rulefetch/refresh，
  DNS 与 native routing 复用 C27；没有这些业务逻辑的私有副本。
  handler 无 WithoutCancel，owner 继续 Detach；无新增后台 goroutine。
  用户 YAML 沿用共享 facade 的 D1/alias 预算，未用 yamlx.Clone 复制用户值。
  未改生产基线或扩大比较豁免，无新增用户操作入口，无需 doc 截图变更。

## 验证命令与输出

主机固定 **192.168.0.64**。复现、定向修复验证、格式化、race 均仅在远端执行。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/api/rules -- bash -lc 'cd server && gofmt -w internal/api/rules && go test -count=1 -run TestReview ./internal/api/rules && CONFIGFLOW_DIFF=1 go test -count=1 -run TestReview -v ./internal/api/rules/difftest'
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && go vet ./internal/api/rules/... && CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rules/...'
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/api/rules/difftest/review_test.go -- bash -lc 'cd server && gofmt -w internal/api/rules/difftest/review_test.go && CONFIGFLOW_DIFF=1 go test -count=1 -run TestReviewAuthenticationAndProviderTokens ./internal/api/rules/difftest'
```

定向修复验证退出 0：

```text
ok configflow/internal/api/rules 0.967s
TestReviewBatchNullValues: PASS (Python=400 Go=400)
TestReviewAuthenticationAndProviderTokens: PASS
ok configflow/internal/api/rules/difftest 2.471s
```

改动包 vet/race 退出 0（最新 token 矩阵扩展在后一条定向命令执行）：

```text
ok configflow/internal/api/rules 28.148s
ok configflow/internal/api/rules/difftest 123.063s
```

扩展后的当前/退役内部 token 矩阵：`ok configflow/internal/api/rules/difftest 3.059s`。
之后给 compose 远程准备/缓存边界补同样的关机检查，追加：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/api/rules/compose_adapter.go -- bash -lc 'cd server && gofmt -w internal/api/rules/compose_adapter.go && go test -race -count=1 -run TestReview ./internal/api/rules'
```

退出 0：`ok configflow/internal/api/rules 13.700s`。

变基到 `go@64815165`，仅 behaviour-changes 文末冲突，保留 N8-6 与 C32 两段。
变基前后的 C32 运行时及测试、desired.go 无差异；复核提交变为 `a18a6d8a`。

最终门禁原先按下面的直接命令进入脚本队列：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/tmp/c32-review-final-gate -- bash -lc 'server/scripts/gate.sh -p 6 FuzzPastedRules:./internal/api/rules'
```

20 秒轮询多次错过释放槽位，约 21 分钟后仍未开始任何格式/测试检查。
终止该等待脚本（退出 143，不是测试失败），改为阻塞等待既有 slot 2，
取得后立即解锁并执行未修改的原 gate.sh；并发上限仍为 2。
最初进入执行阶段的命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/tmp/c32-review-final-gate -- bash -lc 'mkdir -p "$HOME/rt/.gate-slots"; exec 9>"$HOME/rt/.gate-slots/2"; echo "C32: blocking until existing gate slot 2 is released"; flock 9; flock -u 9; exec 9>&-; exec server/scripts/gate.sh -p 6 FuzzPastedRules:./internal/api/rules'
```

02:00:29 取得 slot 2，load 26.14 / 19.08 / 15.17。gofmt/vet、
双 tag 检查通过，proddiff 23 自检 PASS、new=0/stale=0、coverage_regressions=0、
unmapped_routes=0。该轮 race 尚未完成时，go 已合入 R08 与 A24；为验证完整合并树，
终止本 worktree 的门禁进程组（退出 255，不计作通过或测试失败）。

再变基到 `go@8d716ceb`：R08 的 `Desired.ROSPolicy` 及全部相关字段保留。
新增 R08 `InspectROSPolicyDesired` 调用点透传已有 ctx，作为正常签名迁移适配
合入 C32 原 context 接线提交，以免中间提交不能编译；没有改变 R08 算法。
其余 C32 runtime/tests 未变。发现仍为上面 F1/F2 两项，不把上游新增调用点算作原移植缺陷。

最终合并树使用全新的日志目录，完整重跑原脚本：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/tmp/c32-review-integrated-gate -- bash -lc 'mkdir -p "$HOME/rt/.gate-slots"; exec 9>"$HOME/rt/.gate-slots/2"; echo "C32: blocking until existing gate slot 2 is released"; flock 9; flock -u 9; exec 9>&-; exec server/scripts/gate.sh -p 6 FuzzPastedRules:./internal/api/rules'
```

02:06:09 立即取得 slot 2，load 19.04 / 23.40 / 18.74；02:27:01 结束，
load 31.82 / 20.29 / 22.38。**退出 0，GATE PASS**。

- gofmt 无输出，vet 与 configflow_proddiff/configflow_difftest 双 tag build/vet/lifecycle 均通过。
- 全量 race 的 91 个包测试命令均通过（live differential 在随后的显式启用阶段执行）。
  API rules 49.239s、oxidns 114.265s、routers 456.132s、server 1.425s。
- 全部 14 个 live differential 通过。
- FuzzPastedRules 跑满 60 秒，369,572 execs，263 总 interesting，退出 0，wall 61.064s。
- proddiff 23 自检 PASS，coverage_regressions=0、unmapped_routes=0、new=0、stale=0。
  无高负载单侧差异，不需要重跑或修改 baseline。

最终末尾原始输出：

```text
baseline: new=0 stale=0
== difftest
ok configflow/internal/api/agents/difftest 113.920s
ok configflow/internal/api/aggregations/difftest 19.081s
ok configflow/internal/api/rules/difftest 33.880s
ok configflow/internal/api/subscriptions/difftest 53.117s
ok configflow/internal/devicenotes/difftest 0.517s
ok configflow/internal/discovery/difftest 2.091s
ok configflow/internal/health/difftest 13.771s
ok configflow/internal/httpx/difftest 1.464s
ok configflow/internal/mosdns/difftest 1.865s
ok configflow/internal/provider/difftest 49.799s
ok configflow/internal/refresh/difftest 3.468s
ok configflow/internal/subs/difftest 1.373s
ok configflow/internal/substore/difftest 5.027s
ok configflow/internal/webdav/difftest 14.961s
== fuzz (1 targets, 60s each, concurrent)
FuzzPastedRules ok configflow/internal/api/rules 61.064s
GATE PASS
```

## 门禁后的变基增量验证

协调者将原版 C32 和 R08 ctx 透传以 ported 合入 `go@76f65828`。
复核分支变基后跳过已集成的作者提交，最终只保留独立复核修复及报告两个提交。
没有改动 R08 文件或丢弃其字段。差分 helper 仅补回 `t.Helper()`，不改变断言或输入。

随后变基到 `go@601a8121`（包含 N8-4e/f model 及已验证的并行测试更新），
同一 .64 追加全树格式/vet 和相关包启用 live differential 的 race：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; tail -4 /tmp/c32-review-integrated-gate/fuzz-FuzzPastedRules.log; rg "^ok.*(internal/api/rules[[:space:]]|internal/routers[[:space:]]|internal/oxidns[[:space:]]|internal/server[[:space:]])" /tmp/c32-review-integrated-gate/race.log; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rules/... ./internal/model ./internal/repo ./internal/server'
```

退出 0，gofmt/vet 无输出，末尾：

```text
ok configflow/internal/api/rules 50.714s
ok configflow/internal/api/rules/difftest 166.861s
ok configflow/internal/model 20.533s
ok configflow/internal/repo 125.254s
ok configflow/internal/server 1.759s
```

最后 `git rebase go` 到 `e980d402`（N7 writer 已由其作者独立完整验收）。
复核运行时代码不变；为覆盖 C32 调用共享 writer 的交付/DNS 以及完整接线，
追加以下命令，不重复全量 race/proddiff：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... && CONFIGFLOW_DIFF=1 go test -p 6 -count=1 ./internal/api/rules/... ./internal/server'
```

退出 0，gofmt/vet 无输出，全部 C32 live differential 和新复核用例仍通过：

```text
ok configflow/internal/api/rules 4.002s
ok configflow/internal/api/rules/difftest 56.612s
ok configflow/internal/server 0.053s
```

完整 gate 覆盖 R08/A24 集成树；末次增量检查覆盖新 model/writer 与 C32 的交互。

## 提交与交回

独立复核提交使用明确路径；最后执行 `git rebase go`。分支 `go-review-c32`。
最终基于 go@e980d402，只含 C32 独立复核的 13 个路径；完整记录与 PROGRESS 均为 verified。
修复提交和报告提交 ID 见最终交回消息。不 push；工作区干净。
