# C30 对抗性复核

状态：进行中；分支 `go-review-c30`；无子 agent、无推送、无生产访问。

## 已发现并修复

- 中：`auth.ValidateTokenOrJWT` 用普通字符串相等/map 查找比较公共、内部及退休能力 token，没有常量时间保证。改为 SHA-256 固定长度摘要 + `subtle.ConstantTimeCompare`，内部 token 全表比较，无提前退出。Python 同样缺少此保证；这是安全加固，不改变授权集合。
- 新增 23 个 Flask↔Go URL 边界请求，覆盖大小写、空值、重复参数、百分号编码、路径穿越、尾斜杠、额外参数和显式 profile 优先级，全部一致。重复 token 取首值，额外无关参数被忽略，与 Flask 一致。
- 补过期 JWT、畸形 Bearer、退休 token 与 config token 碰撞、token 轮换、错误响应一致及日志不含凭据检查。

## 已完成检查

- 完整阅读 AGENT-BRIEF；核对 PORTING、WORKFLOW parity/idiomatic Go、C30 handoff/test mapping。
- 逐分支对照 subscriptions/nodes/node_deletion 的路由、共享资源写入、cache preview、provider 输出、删除规划及调用的鉴权和 provider snapshot/bundle 边界。
- 全局订阅 provider 路径必须 JWT，profile alias 经 handler 校验系统 config token；`_is_global_request` 只选择空间。config token 是系统级能力，并非每个 profile 独立 token；显式 profile 路径优先于 query/header。
- 删除执行在 repo 写事务内重算规划、比较确认摘要、检查 blockers，随后更新聚合/策略组/节点并一次提交。待补删除专用故障注入/并发边界。

## 当前命令

```sh
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/api/subscriptions/security_test.go -- bash -lc 'cd server && gofmt -w internal/api/subscriptions/security_test.go && go vet ./... && go test -count=1 ./internal/auth/... ./internal/api/subscriptions/... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest -run "TestAdversarialProviderURLs|TestProviderSecurity"'
```

退出 0：auth 0.626s，subscriptions 0.353s，focused differential 1.674s；vet 无输出。

剩余：缓存边界、删除故障/并发、A11/C31 失败关闭；全量 .32 验收、proddiff、rebase 和最终报告。
