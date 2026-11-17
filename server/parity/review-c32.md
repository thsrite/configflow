# C32 对抗式复核

状态：复核修复完成，最终门禁待执行。分支 `go-review-c32`，从作者
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

完整最终 gate 结果待补。

## 提交与交回

独立复核提交使用明确路径；最后执行 `git rebase go`。分支 `go-review-c32`。
提交 ID 与最终门禁结果见后续记录及交回消息。不 push。
