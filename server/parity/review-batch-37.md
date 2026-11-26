# batch-37 复核：Agent 镜像去 Python（nopy-agent-image，Agent 1.9.3-go）

复核者：Opus 5.5，2026-11-23。分支 batch-37（go@2c592d7d7 之上 fb58e9711…ce8fe09aa），复核修正提交 8d80c5c8e、88daf48f4 与本文档。
范围：`svc`、`dockerimage`、`dockerha` 新子命令、Agent 中所有原 `supervisorctl` 调用点、镜像与入口脚本、兼容脚本、面板侧
`ha-startup-checks.sh`/`compose_direct`、`service_manager` 同义词。

## 发现

### 中

1. **重试间隔变短，坏状态更快 FATAL**（已修，8d80c5c8e）。原 Supervisor 在 STARTING 期内退出后按 1、2、3 秒递增退避，
   第 4 次失败才 FATAL；s6 版 finish 只计数，s6 自身最短 1 秒就重启，FATAL 窗口从约 6 秒（加 4 次运行时间）缩到约 3 秒。
   短暂阻塞（端口尚未释放、依赖短暂不可用）在原来能恢复、在新镜像会 FATAL，DNS 停到有人手动 start。修正：finish 在第 n 次失败后
   等 n 秒（每 250 ms 查一次 `wantedup`，收到停止即结束），服务目录写 `timeout-finish` 15000；`S6.Start` 的等待上限加上退避总和。
   实验 A 新增断言：坏配置 FATAL 用时 ≥ 6 秒（实测 10 秒）。
2. **keepalived 进程组清理缺失**（已修，8d80c5c8e）。原配置 `stopasgroup/killasgroup=true`；s6 的 `timeout-kill` 只对主进程发
   SIGKILL，主进程被杀（stop 超时、OOM、kill -9）时 VRRP 子进程可能成为孤儿并继续持有 VIP，而 s6 已把程序视为停止。修正：`Exec`
   记录 PID（程序由 s6-supervise 以 setsid 启动，PID 即进程组号），keepalived 的 finish 扫描 `/proc`，只对该进程组内名为 `keepalived`
   的进程先 SIGTERM（让其释放地址）、3 秒后 SIGKILL；其他程序（尤其 Agent，可能有分离的升级子进程）不做。实验 D 新增断言：
   kill -9 后所有 keepalived 进程都在新实例的进程组里。
3. **停用时 STARTING/BACKOFF 的 keepalived 不会被停止**（已修，88daf48f4）。`haStopService`/`haDaemonAlive` 只把 RUNNING 当作在运行；
   keepalived 处于 BACKOFF（崩溃重启中）或 STARTING（启动 1 秒内）时停用跳过停止，`haVerifyReleased` 也放行，而 s6 随后会自行拉起它。
   这是原 Supervisor 代码就有的判断（不是本批引入），但与 review-batch-25「停用必须确认 keepalived 已停且 VIP 不在本机」相悖，顺带收紧：
   容器管理下只有 STOPPED/FATAL 算已停，读不到状态算仍在运行（停止会失败、停用拒绝）。停用路径之前的 `haPlatform` 已要求状态可读，
   所以正常停用不受影响。测试 `TestHAContainerStopsKeepalivedUnlessDown`。

### 低

4. **辅助容器控制套接字被第二个辅助容器抢占**（已修，8d80c5c8e）。`Listen` 原来无条件删除已有套接字再监听；同一 HA 卷上误起第二个
   辅助容器时，前一个的 keepalived 就再也控制不到。现在先试连，连得上就拒绝启动（旧 supervisord 也会拒绝已被占用的套接字）。
5. **`/sbin/routel`（iproute2 自带）是 Python 脚本**，镜像里唯一引用 Python 的文件；Agent 不调用，执行时由兼容 `python3` 中文拒绝。不改。
6. **仓库里旧的 Python 验收脚本** `backend/agents/go-agent/integration/acceptance.py`、`aio_acceptance.py` 仍用
   `supervisorctl … pid/…`，兼容脚本不支持这些用法；它们已无人引用（Python 退役），不改，删除时一并处理。
7. `ha-companion-serve` 逐个处理请求：一次 start（最长 90 秒）期间，主 Agent 的状态查询（10 秒超时）会超时，环境检查短暂显示
   「受管 keepalived 程序尚未就绪」。只在辅助容器里 keepalived 启动很慢时出现，不影响正确性。不改。

## 逐项核对（无问题的部分）

- **服务对照表**：逐条对照 `go:` 中的 `supervisord.conf`、`supervisor/*.conf`、`supervisor-ha-companion.conf`。Agent 程序未写
  startsecs/startretries，Supervisor 默认 1 秒/3 次，与 `agentSpec` 一致；核心 3 秒/3 次；keepalived 1 秒/3 次、15 秒；stopwaitsecs
  默认 10 秒 = `timeout-kill` 10000。autostart：只有入口启用的核心及其 Agent 进 user bundle，keepalived 只定义不启用（Agent 依 journal 启动）。
  关停顺序：`ha-release` 依赖 `agent-oxidns`，s6-rc 逆依赖顺序先跑它（`svc stop configflow-keepalived` 等到停下）再停 Agent、核心，
  实验 D 以 s6-rc 日志行号验证。用户均为 root。日志：s6-log `n3 s10485760` = 10 MB × (当前 + 3 份)，与 maxbytes/backups 相同（合并输出已列入行为变化）。
- **环境变量**：`ha-compact` 以 `CompactEnvironment` 去掉 `INSTALL_KEY` 后 `exec /entrypoint.sh`，`/init` 及 `with-contenv` 看到的
  容器环境都不含它（实验 F 检查服务进程环境）；`ha-compose run` 去掉 `CONFIGFLOW_COMPOSE_INSTALL`，与原脚本相同。
- **状态解析**：`s6-svstat -o up,wantedup,pid,updownfor` 四个字段；up∧¬wanted → STOPPING，up 且未满 startsecs → STARTING，
  up → RUNNING，down 且有 FATAL 标记 → FATAL，down∧wanted（含 finish 期间）→ BACKOFF，其余 STOPPED。s6 的 finish 退出码 125 使
  s6-supervise 不再拉起（等同 `s6-svc -O`），与标记一致。旧状态词的使用点全部换成 `svc.State`：部署 `serviceRunning`（RUNNING / 过渡态 /
  其余为停）、心跳 `checkServiceWithContainer`（RUNNING、STARTING 为 active，与原逻辑同）、HA `haContainerState`、日志来源
  （核心 → `/var/log/configflow/<核心>/current`，Agent/keepalived → docker logs 提示）。原 `EXITED` 在 s6 下不存在，其语义并入 STOPPED/BACKOFF。
  Start/Stop/Restart 都有上限超时并带最后状态报错。
- **控制套接字**：只接受 status/start/stop/restart 四个命令（其余回 `unsupported command`），只控制 `configflow-keepalived`
  （Remote 端对其他名字返回 ErrUnknown）；目录 0700、套接字 0600、root；请求长度 64 字节、5 秒读超时。主 Agent 的
  `checkHACompanionPeer`（SO_PEERCRED：uid 0、PID 映射不到本命名空间）原样保留，只改路径。新 Agent + 旧辅助容器：连不上
  `control.sock` → 「无法连接独立 HA 辅助容器」，环境未就绪；旧 Agent + 新辅助容器：`supervisorctl -s` 失败 → 未就绪。两者都是拒绝，
  不会误操作。停用保证：`removeOxiDNSHA` 先 `haPlatform`（环境就绪、状态可读）→ 停止（见发现 3）→ `haVerifyReleased`（不在运行且 VIP
  不在本机）未改；实验 D/E 的 DELETE 验证 keepalived 停止、VIP 释放。
- **兼容脚本**：`supervisorctl` 去掉 `-c X`/`-cX` 后只认恰好两个参数的 status/start/stop/restart；`supervisorctl reread`、`status`（缺程序名）
  等都中文报错退出 2。旧面板 `ha-startup-checks.sh` 的 `supervisorctl -c /etc/supervisor/supervisord.conf status oxidns` 被覆盖，输出行以
  「oxidns 」开头、含 RUNNING/FATAL/STOPPED，旧脚本的 case 模式照常匹配；未知程序的提示以「oxidns：」开头，不会误匹配。`python3` 只认
  旧 compose_direct 的两种调用与旧 Compose 包的 `bootstrap.py`（`ha-bootstrap` 的默认路径与旧 bootstrap.py 相同）；`python3 -c …`、无参数等都拒绝。
- **`"supervisor"` 同义词**：读入规范化在 `LoadConfig`（root Config；routes.Config 由其复制）、`loadHAState`、`sameNamespaceDNSPeer`
  的对端配置；`upgrade.Config.Docker()` 两值都认。其余比较只认 `"s6"`，未发现直接读文件后比较 `service_manager` 的遗漏
  （`deployment_dns` 的对端只比 service_type）。面板侧：`server/internal` 非测试代码没有比较 `"supervisor"`（grep），前端只用
  auto/systemd/openrc/command 选项；Python 面板侧也无比较。
- **镜像**：s6-overlay 3.2.3.2，三个 tar 的 SHA256 与 GitHub 发布文件在测试服务器上重新下载核对一致；CI 只构建 amd64/arm64，
  Dockerfile 对其他架构直接失败。`/etc/s6-overlay/user-bundles.d/user/contents.d` 是 s6-overlay 自带布局（tar 里就有），
  `s6-rc.d` 在 tar 里为空，Setup 清空重建它不会删掉 overlay 自身定义。新镜像 `apk info` 无 python/py3-/supervisor、无 `/usr/lib/python3*`、
  无 pip；对镜像文件 `grep -rIl python` 只有兼容脚本、入口脚本注释、`/sbin/routel`（发现 5）和 openssl 配置注释。
- **二进制安装**：抽查 diff，systemd/OpenRC 分支在 `deployment_service`、`log_sources`、`oxidns_ha*`、`uninstall`、`restart` 中只是把
  `case "supervisor"` 换成 `svc.ContainerManager`；`update.go` 删去的 supervisorctl 分支所在的 `restartAgentWithCheck` 无调用方。
  `detectInitSystem` 先查 `/run/service/agent-*`，原生主机不存在该目录。

## 验证（测试服务器 192.168.0.32；路由 HA 测试在 192.168.0.64）

- `go test -race -count=3 ./svc/`（Agent 模块）→ ok。
- `go test -race -run 'HA|OxiDNSHA|Keepalived' ./routes/`（.64）→ ok。
- `GATE_ONLY=agent server/scripts/gate.sh -p 6`（含 8d80c5c8e）→ `GATE STAGE PASS: agent`（routes 171.3 s）。
- `go test -race -count=1 -timeout=60m ./internal/installer/... ./internal/api/agents/... ./internal/agents/` → 全部 ok（installer 524.1 s）。
- 实验 `server/parity/lab/nopy-agent-image-lab.sh ~/.cache/nopy-base`：`A D D2 E F` → `LAB PASS`；`A B C F2 G G2 H` → `LAB PASS`
  （坏配置 FATAL after 10 s；D：kill -9 后无旧进程组的 keepalived）。
- `CONFIGFLOW_DIFF=1 go test -run 'TestDockerHADifferentialAgainstFlask|TestInstallerDifferentialAgainstFlask' ./internal/api/agents/difftest/`
  → docker HA routes 239 comparisons equal, 0 differ；installer routes 293 comparisons equal, 0 differ。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-37` → `PREFLIGHT PASS`。
- 最终树（88daf48f4，.64）：Agent 模块 `gofmt -l` 无输出、`go vet ./...`、`go vet -tags "halab nftlab" ./...`、
  `go test -race -count=1 ./...` → 全部 ok（routes 191.6 s、svc 1.1 s）。
