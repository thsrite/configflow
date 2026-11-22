# nopy-panel-image — 面板镜像去掉 Python（2026-11-22/23）

分支 `go-nopy-panel-image`（基于 `go@488bdd3b2`）。对应 no-python-product 盘点表第 12 项（面板镜像 supervisord）
和第 13 项（`docker/auto_update.py`）。Agent 镜像的 supervisord（第 11 项）不在本任务范围。

## 改动

### 1. Go 服务自己托管 Sub-Store

- 原来：`substore.Runtime.runCommand` 和 `Dependencies.restartSubStore` 调 `supervisorctl -c
  /etc/supervisor/conf.d/supervisord.conf <status|start|stop|restart> sub-store`。两处全部替换，仓库里
  面板侧再没有 supervisorctl 调用（`installer` 里的 supervisord 文本属于 HA 辅助容器/Agent 镜像，未动）。
- `internal/substore/process.go`：`Process` 接口（`Status/Start/Stop/Restart`）和 `Supervisor` 实现，挂在
  `Env.Process`（`NewEnv` 默认装上）。状态词沿用 supervisord 的（`STOPPED/STARTING/RUNNING/BACKOFF/STOPPING`），
  Runtime 的判断逻辑不变；「常驻/按需」模式、5 分钟空闲停止、work/control 租约都没改。
  - 启动：`node <Env.ActiveBundle()>/sub-store.bundle.js`（即原 `sub-store-start.sh`：镜像版和在线更新版取较新者，
    先建 `SUB_STORE_DATA_BASE_PATH`），`Setpgid` 独立进程组；第一个进程同步启动，失败直接返回调用方。
  - 崩溃：1 s 起倍增、最长 30 s 的退避重启；连续运行 ≥30 s 后退避复位；退避中 `Start` 不打断退避。
  - 停止：`exec.Cmd.Cancel` 向进程组发 SIGTERM，`WaitDelay`=5 s 后 SIGKILL；进程退出后再向进程组补一次 SIGKILL 清理残留子进程。
  - 输出：stdout→INFO、stderr→WARNING 写入 logx（日志名 `sub-store`，进 `app.log` 和控制台），逐行，单行上限 4 KiB，
    URL 经 `weburl.SafeForLog` 脱敏。
  - 监督 goroutine 走 `lifecycle.SpawnService`（新增，见下），不是裸 `go`。
- `internal/lifecycle`：新增 `SpawnService`（Runtime 方法 + 默认 runtime 函数）。与 `Spawn` 同样的准入；**不计入 drain 和
  `WaitIdle`**，上下文随 jobs 一起在 drain 结束时取消，Shutdown 在 after-drain 钩子之前等它结束（受总 grace 约束）。
  这样 `docker stop` 时 Sub-Store 能服务完正在收尾的请求再停，和旧 supervisord（先停 configflow、后停 sub-store）一致，
  而且空闲面板关停不必等满 6 s。`o3-concurrency.md` 已补说明。

### 2. 镜像

- runtime 阶段：`apk add ca-certificates tzdata tini nodejs`，去掉 `supervisor`；app 阶段去掉 `python3`。
- `ENTRYPOINT ["/sbin/tini", "-g", "--"]`，CMD 仍是 `configflow-start.sh`，脚本最后 `exec /usr/local/bin/configflow`。
- **为什么用 tini，不让 Go 当 PID 1**：PID 1 要回收所有孤儿进程，只能 `wait4(-1)`，会和 `os/exec` 对自己子进程的
  `Wait` 抢同一个 pid（偶发 ECHILD、丢退出码），Go 运行时没有安全的做法；tini 只回收孤儿、转发信号，Go 服务仍是普通进程，
  启动脚本里 autoupdate 的构建阶段（Go 还没起来）也有人收尸和转发信号。`-g` 让 `docker stop` 的 SIGTERM 同时到达启动阶段的
  构建子进程；Sub-Store 在自己的进程组里，不会被提前打断。
- 删除 `docker/supervisord.conf`、`docker/sub-store-start.sh`、`docker/auto_update.py`；workflow 的 paths 同步。
- healthcheck 脚本本来就只用 busybox `wget`，不变。

### 3. autoupdate（Go）

- `internal/autoupdate` + `cmd/configflow-autoupdate`，独立小程序（不做成 `configflow` 子命令：它要替换
  `/usr/local/bin/configflow` 本身，且构建阶段要按**构建机平台**运行，独立程序更简单）。
- 行为逐项对应 `auto_update.py`：GitHub API / git 源解析分支头，codeload 包或 git 浅拉（失败回退完整 fetch），按源码指纹
  只重建 server/前端/Agent/node-probe，缺工具链不混版本（`ErrMissingToolchain`），安装到镜像位置，失败回退上一次构建或镜像版，
  Python 时代 manifest 忽略，SQLite 之前的旧数据跳过，prune 规则、缓存目录名、manifest 字段都相同。
- 构建阶段：server-builder 额外按目标平台和**构建机平台**各编一份；app 阶段用构建机那份执行 `--precompress` 和
  `--record-image <root> /rootfs/app/.image-trees.json`。
- 指纹与 Python 逐字节一致（在 .64 上对真实仓库 server/frontend/go-agent/node-probe 四个目录对比：equal；另有固定值单测
  `TestTreeHashPinned`），已有 autoupdate 容器的构建缓存继续复用。
- 删除 `backend/test_node_probe_packaging.py`（它测的是 `auto_update.py`，用例已移植成 Go 测试）；删掉
  `backend/test_startup.py` 里读 `supervisord.conf` 的过时用例（它本来就断言早已不存在的 `program:flask`）。

## 测试与实测（全部在 .64）

- 单元测试（`process_test.go`，假子进程 = 测试二进制自身）：启动/状态/幂等/停止/日志与脱敏、崩溃重启与退避间隔
  （50→100→200→200 ms 上限）、退避期间停止立即返回、稳定运行后退避复位、忽略 SIGTERM 的进程组超时后整组 SIGKILL
  （含孙进程）、spawn 失败、面板关停：drain 期间 Sub-Store 仍在、之后停止并回收、关停后 Start 返回 `ErrProcessUnavailable`
  且不留进程、启动命令选较新 bundle 并建数据目录。`lifecycle/service_test.go`：服务不挡 drain、drain 之后取消、after-drain
  钩子之前 join；忽略取消的服务受 grace 约束。
- autoupdate 单测：指纹输入选择、固定值、预压缩（确定性、头部）、manifest、旧数据检测、node-probe（移植的 3 个 Python 用例）、
  Agent 三架构、安装/恢复、prune、tar 安全解压、本地 git 源全流程（不变→变→复用→缺 npm→构建失败→源不可达）。
- 面板镜像冒烟 `server/scripts/panel-image-smoke.sh cf-nopy:runtime`（合成数据）：ALL PASS——无 python/supervisord、PID 1
  为 tini；启动时 Sub-Store 不运行；订阅拉取（本地合成订阅服务器）与节点转换按需拉起 Sub-Store，独立进程组、父进程 configflow，
  输出进 app.log；`kill -9` 后约 1 s 拉起新进程且转换正常、无僵尸；tini 回收孤儿；空闲 5 分钟后停止、无僵尸、再次按需启动；
  `docker stop` 0.26 s 退出码 0，日志有「Sub-Store 已停止」。
- autoupdate 镜像冒烟 `server/scripts/autoupdate-image-smoke.sh cf-nopy:autoupdate`（本地 git 仓库作源，不连 GitHub/生产）：
  ALL PASS——与镜像同源时不重建；改 server+前端后重建两者（43 s），`/api/version`=`main-<sha>`，新前端已安装、已预压缩、
  正在提供，Agent 沿用镜像；同源重启复用；坏的 server 改动构建失败并保留上一版；`docker stop` 退出码 0。

## 发现

- （低，旧问题，非本次引入）新数据目录自带的模板订阅「subscribe」地址是占位文字，定时刷新失败后约每 5 分钟重试一次，每次都经过
  Sub-Store，刚好让按需模式的 Sub-Store 永远不进入休眠，并每 5 分钟写一条警告。冒烟脚本里把它改成本地合成订阅地址绕开。
  可考虑：刷新前先校验 URL（地址无效时不唤醒 Sub-Store），或让失败重试的间隔大于空闲时间。
- （信息）节点行转换有缓存，同一行第二次转换不经过 Sub-Store。

## 验证命令

在 .64 上运行：`gofmt -l .` 无输出；受影响包 `go test -race -count=1 ./internal/substore/ ./internal/lifecycle/...
./internal/api/settings/ ./internal/autoupdate/ ./cmd/configflow/ ./cmd/configflow-autoupdate/` 全部 ok；`/tmp/preflight.sh`
（两组 tag 的 vet + 完整非 race 测试，-p 8）结果为 PREFLIGHT PASS（第一次因 skipaudit 拦下 autoupdate 测试里“缺工具就跳过”而失败，修复后通过）。
前端没有改动（`git diff go -- frontend` 为空）。

## 提交

- `56847db2d` go(substore): supervise the bundled Sub-Store as a child process
- `6e77de713` go(autoupdate): port docker/auto_update.py to configflow-autoupdate
- `9c1fcbcdf` go(docker): panel image without Python or supervisord
- `3d2282028` go(docker): autoupdate image smoke test against a local git source
- `037544178` go(docs): panel image without Python: process model, Sub-Store logs, SpawnService
- `9c7495497` go(autoupdate): no tool-missing skips; bundled Go path is configuration
