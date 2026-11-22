# batch-36 复核 — 面板镜像去 Python（nopy-panel-image）

复核人：Opus 5.5（2026-11-23）。分支 `batch-36`，已 rebase 到 `go@2c592d7d7`（无冲突）。
作者提交 7 个（`915706db7`…`242b9e6e1`），复核修复 3 个（见下）。

## 发现

### 高

无。

### 中

- **M1 关停顺序：服务在 after-drain 钩子之前等待，可能吃掉最后一次指标落盘的时间**（已修，`94a8fb6eb`）。
  `SpawnService` 的服务在 drain 结束时取消，原实现在 after-drain 钩子**之前**用总 grace 等它。Sub-Store 忽略 SIGTERM 时
  监督者要 5 s 才 SIGKILL：drain 用满 6 s + 0.5 s + 5 s > 8 s，总 grace 先到期，`r.hook` 遇到已取消的 ctx 立即返回，
  metrics flush #2（`metrics-history-flush-last`）拿到已取消的 ctx，最多 30 分钟的指标历史丢失；close 钩子也只剩竞速。
  改为：after-drain 钩子 → 等服务（最多到 grace−0.5 s）→ close 钩子。Sub-Store 不依赖 after-drain 钩子，drain 期间仍在服务，
  原「drain 期间 Sub-Store 仍可用」的目的不变。新增 `TestSpawnServiceDoesNotStarveHooks`；`o3-concurrency.md` 表格同步。
- **M2 Sub-Store 输出脱敏不够**（已修，`5cb99742c`）。`weburl.SafeForLog` 只去掉账号、片段和敏感查询键，
  **路径里的令牌**（`/link/<token>`、`/s/<token>` 一类，机场很常见）原样保留；节点链接（`vmess://<base64 JSON>`、
  整串 base64 的 `ss://`）主机部分就是凭据；Surge/Loon 行 `password=…`、JSON 节点 `"uuid":"…"` 没有 URL 形式。
  这些现在进 `app.log`（日志页可见，旧镜像只在容器 stdout）。改为 Sub-Store 输出专用的 `redactProcessLine`：
  http(s) 只留协议+主机（有路径/查询时写 `/[REDACTED]`），其他协议只留协议名，凭据键值去值。表驱动测试覆盖
  query token、路径 token、userinfo、片段、ss/vmess、Surge 行、JSON。
- **M3（作者报告的旧问题）模板订阅占位地址让按需 Sub-Store 永不休眠**（已修，`8bfbe21e9`）。
  `substore.SourceURLConfigured`：地址中没有一行是带主机的绝对 URL 即视为未配置。定时刷新不调度（和空地址一样，不重试）；
  任务行不可运行、无下次时间，`run_hint`=「地址无效，请检查是否为完整的 http(s) 地址」（复用现有文案；空地址仍是「没有订阅地址」）；
  `POST /api/tasks/run` 同句 400；`Client.fetchSubscription` 在碰 Sub-Store 前返回 `InvalidURL`（其他拉取入口也不唤醒）。
  这是对 Python 的有意偏离，记入 `behaviour-changes.md`；refresh difftest 对模板行加了期望差异（`placeholderTemplateRow`）。
  单测：`TestSourceURLConfigured`、`TestPlaceholderSubscriptionDoesNotWakeSubStore`、`TestPlaceholderSubscriptionURLIsNotConfigured`。
  镜像实测：全新数据目录启动 100 s，无 node 进程、日志无订阅失败，任务行与手动运行文案如上。

### 低

- **L1 首次 spawn 失败后立即重试会被当成已在运行**（已修，并入 `5cb99742c`）。原来 `first <- err` 先于 `finish(run)`，
  调用方拿到错误立刻再 `Start` 时 `s.run` 仍在，直接返回 nil，随后等就绪超时。改为先结束本周期再回报错误。
- **L2 Go 进程被 SIGKILL/崩溃时 Sub-Store 的去向**（未改，记录）。镜像内：Go 退出 → tini（PID 1）退出 → PID 命名空间销毁，
  内核杀掉 node；容器由重启策略拉起时是新命名空间，不会有旧进程占 3001 端口。镜像外直接跑二进制且存在 `/opt/sub-store` 时
  node 会成孤儿继续占端口（下次 `isReady` 会把它当作运行中，但不受监督、空闲也停不掉）。没加 `Pdeathsig`：它绑定创建线程，
  Go 运行时可能退出线程导致误杀，且面板只在镜像里托管 Sub-Store。
- **L3 autoupdate 把仓库/分支传给 git 时没有 `--`**（未改，信息）。值来自容器环境变量（运维者自己控制），不是越权面；
  构建会执行仓库的 npm 脚本/Go 构建，这正是功能本身，与 Python 一致。只用配置的仓库与分支：GitHub API 解析分支头、
  codeload 按 sha 下载、git 源 `ls-remote` + `fetch refs/heads/<branch>` + `checkout <sha>`，不运行仓库里其他脚本。

## 逐项核对（无问题）

- **进程组清理**：`Setpgid`；停止时 `Cancel` 发 SIGTERM 给整组，`WaitDelay` 5 s 后杀 leader、关管道；`Wait` 返回后每条退出路径
  （停止、崩溃、关停）都再向整组发 SIGKILL 清孙进程。退避 1→2→…→30 s，运行 ≥30 s 后复位；退避中 `Stop` 立即返回；
  退避期间 spawn 失败继续退避。
- **按需/常驻与 5 分钟空闲**：`Runtime` 逻辑没动，只把 supervisorctl 换成 `Process`；状态词不变。按需启动的并发由 work 租约 +
  control 租约串行（`ensureRunning` 在 control 内），`Supervisor.Start` 本身也幂等。冒烟：空闲 5 分钟后停止、再次按需启动。
- **在线更新后的重启**：`restartSubStore` 在 Maintenance 的独占 work 租约内调 `Restart`（Stop 等组退出 → Start），每次 spawn 都重新
  `ActiveBundle()`，所以新 bundle 生效；原来已停时 restart 会启动它（与 `supervisorctl restart` 一致），Maintenance 结束写 `LastUsed`，
  按需模式随后照常空闲停止。
- **SpawnService**：不计入 drain/`WaitIdle`，ctx 随 jobs 取消；`-race` 下 lifecycle 与 substore 测试通过，关停后 `Start` 返回
  `ErrProcessUnavailable` 且不留进程，无 goroutine 泄漏（`pending` 在服务结束时归还）。
- **tini -g + exec configflow**：SIGTERM 发给 configflow 所在进程组（含启动阶段的 autoupdate 构建），Sub-Store 在自己的组里，
  drain 后由 Go 停。Go 退出容器即退出，依赖重启策略：`docker/docker-compose.yml`、`doc/guide/install.md` 的 compose 与两条
  `docker run` 示例都有 `unless-stopped`，安装文档第 308、347 行有提醒（NAS 在设置表中核对）。
- **autoupdate 指纹**：在 .32 上对本树 server/frontend/go-agent/node-probe 用 `go:docker/auto_update.py` 的 `server_tree_hash`/`tree_hash`
  与 Go `--record-image` 对比：四项 **EQUAL**。替换 `/usr/local/bin/configflow` 用同目录临时文件 + rename（原子）；构建失败、
  安装失败都回到上一份 manifest 或镜像副本；`--precompress`/`--record-image` 在构建阶段由构建机平台的那份执行。

## 验证（全部在 192.168.0.32）

- `gofmt -l .` 无输出；`go vet` substore/lifecycle/refresh 通过。
- `go test -race -count=1 ./internal/substore/ ./internal/lifecycle/... ./internal/api/settings/ ./internal/autoupdate/ ./cmd/configflow-autoupdate/ ./internal/refresh/`：全部 ok。
- `CONFIGFLOW_DIFF=1 go test -tags configflow_difftest` refresh、substore、tasks、subs、provider、api/subscriptions、api/aggregations、mcp、health、api/config：全部 ok（refresh 加期望差异后）。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-36 -p 8`：`PREFLIGHT PASS`。
- `server/scripts/panel-image-smoke.sh cf-b36:runtime`：ALL PASS（含 5 分钟空闲停止、kill -9 重启、docker stop）。
- `server/scripts/autoupdate-image-smoke.sh cf-b36:autoupdate`：ALL PASS。

## 提交

- `94a8fb6eb` go(lifecycle): join services after the after-drain hooks
- `5cb99742c` go(substore): stronger redaction of Sub-Store output; clear failed start
- `8bfbe21e9` go(refresh): a placeholder subscription address is not configured
