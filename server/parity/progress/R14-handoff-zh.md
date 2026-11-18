# R14 自动规则同步（实现中）

范围：四个 rule_sync Python 模块，复用 R12 Inspect/BuildPlan/Verify、R07/R08 写入与补偿、R10 IPv6 路由；无 HTTP（R15）。只使用 loopback routerfake，定向检查固定测试服务器；不 push。

已完成：原生规则分类/来源选择、共享 rulefetch 下载适配、原子回执、control 签名、手动 Capture、受限目的增删与全错误补偿、Sync/StartJob/30 秒调度、C07 rows/runner 和 Wire。真实 Runtime 的断开/关机基本用例通过。

当前提交：本文件所在提交（第一步）。无半写文件。

接下来：真实 Python 边界对照（分类、delta、service）、NAT/policy/IPv6 变更与失败恢复、Runtime 设备取消和 leaf 提交窗口、tasks/锁/调度与缓存复用测试；最终定向 race、vet、格式、fuzz；rebase go；标记 ported。

决策/依赖：本基线 go@c5f5ff349 尚无 O3-6b RouterJobLock。JobLock 临时集中在 rule_sync.go，repo hook/sync/R15 同用；待 O3-6b 合并应改为委托统一入口，移除 syncJobs。没有 flock。Capture 不再次拿 L1/L2。签名用 canonicalReleaseJSON（D2），新旧签名不同需重建基准。关机取消后不再写设备/本地，保留已写 running 回执供下一轮变 paused；未扩大 R12 的 D-R12-3 特例。

已跑：
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -w internal/routers/rule_sync*.go internal/routers/r14*.go internal/routers/wire.go && go test -count=1 ./internal/routers -run "TestR14" -timeout 5m'`
最后输出：`ok configflow/internal/routers 2.138s`。
