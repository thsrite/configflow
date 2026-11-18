# R13 中文交接（进行中）

分支 go-port-r13，从 go-port-r12@574c3e896 建立；R12 目前由 Opus 66383b28 在 go-opus-review-r12 复核，
有修复再变基。当前 R12 交接和 D-R12-1 明确 inspection/advisory 已由 R12 负责，R13 复用，不另写一份。

实现 pending 摘要、跨配置空间目标/签名、64-Agent LRU 无 TTL 缓存、Flight 合并 refresh、25 秒/8 worker
读取预算、35 秒等待预算和 C07 summary/RecordRouter 衔接。所有后台读取经过 lifecycle Spawner，
不写设备；unknown 不复用、迟到旧 generation 不覆盖新结果、Clear/eviction 不复活旧代；结果深拷贝。

C07 边界：现有 Sources.Summarize/RecordRouter 仍为 legacy jsonx API。只在既有 routers/wire.go 隔离
转换，其余 pending 产品用 native DTO；不新增 RecordRouterJSON。前协调者已明确 TransactionContext、
MarkReadContext 和 RecordRouter 的 leaf 取消修复在 batch-1/go-review-c07，禁止重复实现。等正式合入
再变基验证；N8-7 第二批（097eba4e）迁 notifications 后统一清理适配。tasks 的 router slot 留 R14，
不填空 task rows 假装已就绪。A23 未实现：R12 IPv6HostRequest 缺省 fail closed，R13 只展示实际 R12
报告，不造 readiness 或伪装 synced；A23 后续 owner 接线。

已跑 .32 定向：编译通过；TestR13 摘要真实 Python 正常/错误类代表、并发/cache 和实际 loopback
AgentRouters/通知检查通过0.993s。正在补最后的 shutdown-before-memory-commit 与错误隔离负控、定向 race。
未做 gate/变异，未 push、未开子 agent、未访问真实设备；所有检查仅 rt。
