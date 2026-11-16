# C30 对抗性复核

状态：进行中；分支 `go-review-c30`；无子 agent、无推送、无生产访问。

## 已发现并修复（按严重度排序）

- 中：`auth.ValidateTokenOrJWT` 用普通字符串相等/map 查找比较公共、内部及退休能力 token，没有常量时间保证。改为 SHA-256 固定长度摘要 + `subtle.ConstantTimeCompare`，内部 token 全表比较，无提前退出。Python 同样缺少此保证；这是安全加固，不改变授权集合。
- 中：旧缓存 `nodes` 是对象而非数组时，缓存预览及订阅列表均返回 500（Python 200）。只在两个只读摘要入口忽略非数组 nodes，继续读取 count/updated_at；provider 渲染仍严格解码，不能静默发布空数据。新增 non-list 缓存差分，修复前稳定失败、修复后通过。
- 中：非对象的非空 `/fetch` 请求体被当成空对象执行，发生未请求的下载/节点导入（Python 写入前拒绝）；两个 reorder 入口把同类输入报为 404 而不是 500。现在在下载和写事务前拒绝，保留原有 500 响应类别及订阅中文错误，Go 使用自己的英文对象校验提示。新增 12 个 Flask 差分请求，比较状态和全部持久化结果，只归一化该类 Python 内部英文异常。
- 中：离线拉取回退到 `nodes: null` 的旧缓存时，adapter 将 nil slice 编成 null 后断言为数组，触发请求 panic/500；Python 返回空节点数组/200。现在在 adapter 边界保留空数组；新增 preview/import 两个差分，覆盖响应和 revision，修复前稳定复现 panic。
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

剩余：全量 .32 验收、proddiff、rebase 和最终报告。
