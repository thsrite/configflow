# C30 对抗性复核

状态：`verified`，没有未关闭的 C30 发现；分支 `go-review-c30`。无子 agent、无推送、无生产访问；全部测试在测试服务器上运行。

## 已发现并修复（按严重度排序）

- 中：`auth.ValidateTokenOrJWT` 用普通字符串相等/map 查找比较公共、内部及退休能力 token，没有常量时间保证。改为 SHA-256 固定长度摘要 + `subtle.ConstantTimeCompare`，内部 token 全表比较，无提前退出。Python 同样缺少此保证；这是安全加固，不改变授权集合。
- 中：旧缓存 `nodes` 是对象而非数组时，缓存预览及订阅列表均返回 500（Python 200）。只在两个只读摘要入口忽略非数组 nodes，继续读取 count/updated_at；provider 渲染仍严格解码，不能静默发布空数据。新增 non-list 缓存差分，修复前稳定失败、修复后通过。
- 中：非对象的非空 `/fetch` 请求体被当成空对象执行，发生未请求的下载/节点导入（Python 写入前拒绝）；两个 reorder 入口把同类输入报为 404 而不是 500。现在在下载和写事务前拒绝，保留原有 500 响应类别及订阅中文错误，Go 使用自己的英文对象校验提示。新增 12 个 Flask 差分请求，比较状态和全部持久化结果，只归一化该类 Python 内部英文异常。
- 中：离线拉取回退到 `nodes: null` 的旧缓存时，adapter 将 nil slice 编成 null 后断言为数组，触发请求 panic/500；Python 返回空节点数组/200。现在在 adapter 边界保留空数组；新增 preview/import 两个差分，覆盖响应和 revision，修复前稳定复现 panic。
- 中：UTF-8 BOM 请求体通过公共 JSON 解析后，C30 再次解码原始字节时未剥除 BOM，导致有效对象被当成非对象。节点/订阅的原生解码入口及可选请求体检测现在统一去掉 UTF-8 BOM；新增 8 个 CRUD/reorder/parse/fetch 差分请求通过。
- 新增 23 个 Flask↔Go URL 边界请求，覆盖大小写、空值、重复参数、百分号编码、路径穿越、尾斜杠、额外参数和显式 profile 优先级，全部一致。重复 token 取首值，额外无关参数被忽略，与 Flask 一致。
- 补过期 JWT、畸形 Bearer、退休 token 与 config token 碰撞、token 轮换、错误响应一致及日志不含凭据检查。
- `TestEveryGlobalRouteRequiresJWT` 对 25 个全局 method/path 逐一带有效 URL token、不给 JWT，Flask/Go 均返回 401；包括保留的 5 个 C16 延迟方法。profile alias 的 token-only 成功/失败另测，覆盖全部 26 个注册方法。

## 已完成检查

- 完整阅读 AGENT-BRIEF；核对 PORTING、WORKFLOW parity/idiomatic Go、C30 handoff/test mapping。
- 逐分支对照 subscriptions/nodes/node_deletion 的路由、共享资源写入、cache preview、provider 输出、删除规划及调用的鉴权和 provider snapshot/bundle 边界。
- 全局订阅 provider 路径必须 JWT，profile alias 经 handler 校验系统 config token；`_is_global_request` 只选择空间。config token 是系统级能力，并非每个 profile 独立 token；显式 profile 路径优先于 query/header。
- 删除执行在 repo 写事务内重算规划、比较确认摘要、检查 blockers，随后更新聚合/策略组/节点并一次提交。专项测试注入 Store.BeforeCommit 失败后，对比 DB fingerprint 及重新打开仓库的完整文档（含 revision/两处 profile/聚合），确认完全回滚；并发修改受影响策略组后旧确认返回 409；取消请求的有效删除仍完成。

## 当前命令

```sh
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/api/subscriptions/security_test.go -- bash -lc 'cd server && gofmt -w internal/api/subscriptions/security_test.go && go vet ./... && go test -count=1 ./internal/auth/... ./internal/api/subscriptions/... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest -run "TestAdversarialProviderURLs|TestProviderSecurity"'
```

退出 0：auth 0.626s，subscriptions 0.353s，focused differential 1.674s；vet 无输出。

补充 .63 验证（均退出 0）：

- `go vet ./...` 无输出。
- `go test -race -count=1 ./internal/api/nodes` → `ok configflow/internal/api/nodes 8.906s`。
- `CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest/` → `ok configflow/internal/api/subscriptions/difftest 63.728s`。
- `TestMissingAggregationDependencyFailsClosed` 验证真实缺省 C31 hook：激活图依赖聚合时，单订阅/合并均 500，完整 DB/JSON/YAML 快照未变化。
- A11 默认 Locations 仅改变错误文本，StrategyReferenceError/400 不变；既有形状、循环、保留名、跨 feed staging 差分继续通过，没有返回错误数据。
- C30 生产代码 jsonx 仅在两个 adapter.go；RawMessage 保存未知字段，json/yaml/crypto 为 Go 原生库。确认摘要的排序 JSON/分隔符以及中文错误中的 ID 列表是已有外部协议，没有新增 Python 运行时模拟、依赖或下游实现副本。
- flight key 是 kind/profile/resource/format/filter/config 摘要，不存请求 token；鉴权发生在 cache/flight 访问前。

已完成 .32 全量验收及最终 rebase；批准延后的 A11 诊断位置信息、C31 聚合 materialization 仍由各自模块完成。缺省 hook 不会吞掉校验错误或提交未通过校验的产物；保持 Python 的 provider last-good 响应机制。

## 最终验收命令（.32）

全部通过 `rt` 运行；测试输出日志只保留在测试服务器 `/tmp/review-c30-*.log`。

```sh
rt -- bash -lc 'set -e
cd server
gofmt -l internal/ tools/ design/ > /tmp/review-c30-format.log
cat /tmp/review-c30-format.log
test ! -s /tmp/review-c30-format.log
go vet ./... > /tmp/review-c30-vet.log 2>&1
cat /tmp/review-c30-vet.log
go test -race -count=1 ./internal/api/subscriptions/... ./internal/api/nodes/... ./internal/auth/... > /tmp/review-c30-race.log 2>&1
tail -6 /tmp/review-c30-race.log
go test -count=1 ./... > /tmp/review-c30-suite.log 2>&1
tail -8 /tmp/review-c30-suite.log
CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/... ./internal/api/nodes/... > /tmp/review-c30-diff.log 2>&1
tail -6 /tmp/review-c30-diff.log
cd ..
server/scripts/proddiff.sh > /tmp/review-c30-proddiff.log 2>&1
rg "^self-check:|^baseline:" /tmp/review-c30-proddiff.log
rg -o "\"coverage_regressions\": [0-9]+|\"new\": [0-9]+|\"stale\": [0-9]+" /tmp/review-c30-proddiff.log'
```

最终一轮包含全部修复，命令退出 0，gofmt/vet 无输出。scoped race 输出如下（difftest 在本次 race 调用中按既有环境开关跳过；下一步 HTTP 差分显式启用）：

```text
ok  configflow/internal/api/subscriptions          1.406s
ok  configflow/internal/api/subscriptions/difftest 1.028s
ok  configflow/internal/api/nodes                  4.922s
ok  configflow/internal/auth                      1.430s
```

全量 `go test -count=1 ./...` 退出 0，末尾：

```text
?   configflow/internal/tasks              [no test files]
ok  configflow/internal/taskstatus          0.022s
ok  configflow/internal/utilx               0.006s
ok  configflow/internal/version             0.005s
ok  configflow/internal/weblistener         0.012s
ok  configflow/internal/weburl              0.004s
ok  configflow/internal/yamlx               0.723s
?   configflow/tools/proddiff/generator     [no test files]
```

启用 HTTP 差分及 proddiff：

```text
ok  configflow/internal/api/subscriptions          0.029s
ok  configflow/internal/api/subscriptions/difftest 67.413s
ok  configflow/internal/api/nodes                  0.296s
self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS
baseline: new=0 stale=0
"coverage_regressions": 0
"new": 0
"stale": 0
```

生产快照比较覆盖 65 个 GET 模板及生成器，原有 5 类基线差异不变。此前一轮 proddiff 曾出现 5 个额外 Agent metrics summary 差异，路径仅为 `body.data.summary.data_points`、`cpu.avg`、`memory.avg`、`network_recv.avg`、`network_sent.avg`，无 C30 差异；最终复跑消失。疑似独立请求时钟窗口造成，未证明根因，未修改 baseline/normalize 掩盖它；若再次出现应交给 metrics/proddiff 所属模块排查。

## 提交与兼容性

最终测试代码：`3e088219`，基于 `go` 的 `e6c87cc1`；验收后 `git rebase go` 返回 `Current branch go-review-c30 is up to date.`。最终文档提交后再次确认工作区干净。

- `6c80c783`：常量时间 token 比较及 URL/轮换/JWT 边界。
- `2ef264ae`：旧缓存摘要与删除事务故障/并发检查。
- `82f78031`：响应/日志/served 文件凭据检查及复核记录。
- `37fa3455`：非对象 fetch/reorder 请求体拒绝。
- `627bed76`：全部全局路由 JWT 闸门差分。
- `729c2452`：null 缓存回退不触发 panic。
- `3e088219`：UTF-8 BOM 请求体。

新增测试文件 `internal/api/nodes/review_test.go`（120 行）；修改 subscriptions/auth 的原生边界及差分用例，生产代码净增 66 行。没有新增依赖，没有复制 Python 运行时或下游业务实现。外部功能/存储兼容性与 Python 一致；安全加固不改变授权集合，畸形请求的原生英文提示不追逐 Python 内部异常文本。没有新增操作入口，故无需用户文档截图变更。
