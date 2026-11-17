# O3-3 实现说明：启动与停止

Go 后端要求一个数据目录只由一个服务进程使用。main 先调用共享的
`repo.ResolveDataDir` 展开用户目录并解析符号链接，再取得
`DATA_DIR/.configflow-server.lock`，并同时持有 Python 的四把调度器生命周期锁。
如果前一个 Go 或 Python 服务尚未退出，最多等待 30 秒，然后以状态码 1 退出，提示：

> 数据目录 … 正被另一个 ConfigFlow 服务使用，本进程退出

停止旧服务后再启动即可。锁由内核随进程退出释放；锁文件会保留，文件中的 PID、版本和
启动时间仅供诊断，不决定目录是否被占用。

拿到锁后，服务依次准备配置、清理孤立订阅缓存、初始化 Agent 管理器、加载监控历史，
全部完成后才监听。规则同步循环等第一个通过配置空间解析和鉴权门禁的请求到来后启动；
允许公开访问的 Agent 心跳也会触发它，被拒绝的请求不会触发。

收到 SIGTERM 或 SIGINT 后，服务立即保存一次监控历史，停止接受新连接，并等待现有请求、
后台工作完成。`CONFIGFLOW_SHUTDOWN_GRACE_SECONDS` 默认是 8 秒；到宽限期前 2 秒时取消
剩余后台工作，再等待至多 0.5 秒，最后保存新增历史、关闭资源并释放实例锁。
空闲时会提前结束，镜像仍保持原来的默认 10 秒停止时限。
第一次保存监控历史期间再次收到 SIGTERM 或 SIGINT 会被忽略（与 Python 相同），保存完成后再次收到会立即强制退出。
等待数据目录（最多 30 秒）期间收到停止信号会立即结束等待并以状态码 0 退出，不打开存储。

请求在进入 httpx 时绑定到当前 lifecycle 代际：owner 从请求上 `lifecycle.Detach` 出来的工作不受客户端断开影响，
但会在宽限期前 2 秒随后台工作一起被取消；关机钩子和 Drain 里的 Detach 以整体宽限期为界。

Go 不接受 legacy 存储，也不再执行旧版一次性 DNS 清理迁移。切换前须先升级到最后一个
Python 版本并启动一次，完成旧数据迁移，再使用 Go 后端。

接口和阶段约定见 [o3-concurrency.md](o3-concurrency.md)。Startup 注册在 Wire 重复注册时
保留原位置，`lifecycle.RunStartup` 先执行一次性工作，监听后才由 Start 启动周期循环。
httpx 的通知位于 profile hook 和 auth gate 之后；鉴权门禁拒绝时不启动首请求循环。

HTTP Drain 并行等待 API/web 的 `http.Server.Shutdown` 并回收控制协程。到截止时间后，
PhaseClose 会关闭剩余连接、刷出日志、释放实例锁。SQLite 连接由各操作的 owner 关闭，
目前没有需要伪造 Close 方法的常驻全局数据库对象。
