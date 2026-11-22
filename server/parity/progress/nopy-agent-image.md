# Agent 镜像去掉 Python（nopy-agent-image）

分支 `go-nopy-agent-image`（基于 batch-35 的 84bd921f2）。盘点见 `no-python-product.md` 第 7–11 项，设计决定由协调者确认（2026-11-22）：
`service_manager` 保留 `"supervisor"` 协议值；核心日志 stdout/stderr 合并；host 模式辅助容器用 `ha-companion-serve` 控制套接字；
旧面板脚本/旧部署用 `supervisorctl`、`python3` 过渡兼容脚本（下一个大版本删除）；startsecs/startretries 用 finish 脚本复现。

状态：**完成**，Agent 1.9.3-go。行为变化写在 `behaviour-changes.md` 末节；文档 `doc/guide/oxidns-ha.md`、`doc/manual/agents.md#logs`。

## 1. 服务对照表（Supervisor → s6-overlay 3.2.3.2）

s6 定义由 `configflow-agent docker-config` 在每次容器启动时从 `svc.Specs` 生成（`/etc/s6-overlay/s6-rc.d`、user bundle），
运行命令由 `configflow-agent svc-exec`（记录启动时间、需要时合并 stderr、`exec`）启动，退出由 `svc-finish` 处理。

| 程序 | 启动命令 | 开机启动 | 自动重启 / startsecs / startretries | 停止信号与等待 | 环境、用户 | 日志与轮转 |
|---|---|---|---|---|---|---|
| `agent-mihomo` / `agent-mosdns` / `agent-oxidns` | 旧：`configflow-agent -config=/opt/configflow-agent/config-<核心>.json`；新：相同 | 旧：入口脚本 sed 改 `autostart`；新：启用的核心连同其 Agent 写进 user bundle | 旧：autorestart=true，默认 startsecs=1、startretries=3；新：s6 总是拉起，finish 复现 1 秒/3 次后 FATAL | 旧：TERM，默认 10 秒后 KILL；新：TERM，`timeout-kill` 10000 | 旧：继承 supervisord（容器环境），root；新：`with-contenv`（容器环境；ha-compact 已去掉 INSTALL_KEY），root | 旧/新：容器标准输出（docker logs） |
| `mihomo` | 旧：`mihomo -d /etc/mihomo`，单服务模式由 PYSUPERVISOR 改为 `-d <目录> -f <配置>`；新：`svc.CoreArgv` 写入 `/run/configflow-svc/mihomo.argv`（JSON 数组，路径可含空格和 `%`），AIO 用默认 | 同上 | 旧：startsecs=3、startretries=3；新：相同（finish） | 旧：TERM/10 秒；新：TERM/`timeout-kill` 10000 | 同上 | 旧：`/var/log/supervisor/mihomo.log` 与 `.err.log` 各 10MB×3；新：stdout+stderr 合并 → `mihomo-log`（s6-log `n3 s10485760`）→ `/var/log/configflow/mihomo/current` |
| `mosdns` | 旧：`mosdns start -c <配置> -d <目录>`（单服务模式改写）；新：同上，argv 文件 | 同上 | 3 秒 / 3 次 | TERM / 10 秒 | 同上 | 旧：stdout 走 Supervisor AUTO 子日志，stderr `/etc/mosdns/mosdns.err.log` 10MB×3（在数据卷里）；新：合并 → `/var/log/configflow/mosdns/current` 10MB×3 |
| `oxidns` | 旧：`oxidns start -c <配置> -d <目录>`；新：同上 | 同上 | 3 秒 / 3 次 | TERM / 10 秒 | 同上 | 旧：`/var/log/supervisor/oxidns.log`、`.err.log` 10MB×3；新：合并 → `/var/log/configflow/oxidns/current` |
| `configflow-keepalived`（macvlan 主容器、host 模式辅助容器） | `keepalived -P --dont-fork --log-console --use-file=/var/lib/configflow-ha/keepalived.conf --pid=… --vrrp_pid=…`，不变 | 旧/新：不开机启动，只由 Agent 按 HA 记录启动 | 旧：startsecs=1、startretries=3；新：相同 | 旧：TERM，15 秒，stopasgroup；新：TERM，`timeout-kill` 15000（keepalived 自己结束 VRRP 子进程） | root，容器环境 | 容器标准输出 |
| `ha-release`（macvlan 主容器）/ `ha-companion-release`（辅助容器） | 新增 oneshot：down 时 `configflow-agent svc stop configflow-keepalived` | 随容器 | — | 依赖 `agent-oxidns`，所以容器停止时**先**停 keepalived，再停 Agent、核心（还原 priority 110 > 99 > 10） | root | — |
| `ha-companion-serve`（辅助容器） | 新增：`configflow-agent ha-companion-serve`，监听 `/var/lib/configflow-ha/run/control.sock`（0600）；替代旧辅助容器的 supervisord 套接字 `supervisor.sock` | 开机启动 | 1 秒 / 3 次 | TERM / 5 秒 | root | 容器标准输出 |
| supervisord 本身 | 旧：PID 1，`/var/log/supervisor/supervisord.log` 50MB×10；新：s6-overlay `/init` 为 PID 1，输出在 docker logs（`S6_VERBOSITY=1`，阶段 2 失败即退出 `S6_BEHAVIOUR_IF_STAGE2_FAILS=2`，收尾等待 `S6_KILL_GRACETIME=1000`） | | | 旧：docker stop 时按 priority 逆序；新：s6-rc 依赖逆序 | | |

状态查询（`configflow-agent svc status`、Agent 内部）：`s6-svstat -o up,wantedup,pid,updownfor` + FATAL 标记 →
RUNNING / STARTING（启动期内）/ BACKOFF（等待重启）/ STOPPING / STOPPED / FATAL，输出行与 supervisorctl 相同格式（`oxidns RUNNING pid 12, uptime 0:01:02`）。

## 2. 改动

- `backend/agents/go-agent/svc`（新）：`Manager` 接口（Start/Stop/Restart/Status，可替换实现），`S6` 实现、`Remote`（辅助容器套接字）实现、
  `ParseSvstat`、finish 逻辑、`Setup`（s6-rc 源与 bundle）、子命令 `svc`、`svc-exec`、`svc-finish`、`ha-companion-serve`。
- `backend/agents/go-agent/dockerimage`（新）：`docker-config agent|services|companion`（替代 PYCONFIG、PYSUPERVISOR、autostart 改写）。
- `backend/agents/go-agent/dockerha`：`ha-compact`（替代 docker-compose-compact.py）、`ha-compose prepare|run`（替代 docker-compose-install.py）、
  `ha-companion-check`（替代 PYCOMPANION）；登记步骤抽成 `enrollAgent`（安装包/Compose 两种措辞）；owner 摘要规范形式 `bindingOwner`。
- Agent 里所有 `supervisorctl` 调用改走 `svc`：`routes/deployment_service.go`、`oxidns_ha_container.go`、`oxidns_ha_resources.go`、
  `logs.go`、`log_sources.go`、`restart.go`、`uninstall.go`、`update.go`、`system.go`。删除 `supervisord.conf`、`supervisor/*.conf`。
- 镜像：`docker/Dockerfile.agent`（s6-overlay、无 Python）、入口脚本、辅助容器入口、`docker/compat/{supervisorctl,python3}`；删除
  `docker/docker-compose-{compact,install}.py`、`docker/supervisor-ha-companion.conf`。面板镜像的 `docker/supervisord.conf` 未动（盘点第 12 项）。
- 面板：`ha-startup-checks.sh` 用 `configflow-agent svc status oxidns`，探测最低版本 1.9.3-go；`compose_direct` 入口 `configflow-agent ha-compose`；
  `NoPythonBaseline` 还原这两处；`l1-ha-deltas.json`、L1 容器测试（真实 Agent 二进制）、O9 入口测试（真实 docker-config）。
- 版本 1.9.3-go（照 9d4a3cdcb / c1459430c）：`upgrade.Version`、`LatestAgentVersion` + 更新日志、pure_test、proddiff、configdiff。

## 3. 验证（测试服务器）

见交回报告末尾的命令与结果；实验：`server/parity/lab/nopy-agent-image-lab.sh BASE_TREE`（BASE_TREE = `git archive 84bd921f2 docker backend/agents/go-agent server`）。

| 场景 | 结果 |
|---|---|
| 镜像内容 | 无 python3/py3-*/supervisor 包、无 `/usr/lib/python3*`；`/usr/bin/python3` 只是 shell 兼容脚本；镜像里其余文件不调用 python |
| A 单 OxiDNS | RUNNING；kill -9 核心/Agent 自动拉起（新 PID）；svc stop 保持停止、start、restart；supervisorctl 兼容 status/stop/start，其余用法与 python3 中文拒绝；坏配置约 5 秒 FATAL 且不再拉起，修好后 start 清除；docker stop 约 1.2 秒、退出码 0、无僵尸；docker start 恢复 |
| B MosDNS Agent API | `/api/restart` 重启核心；日志来源只有合并的 `current`；`/api/logs` 读取；真实部署 succeeded（核心经 svc 停止并启动，配置替换），数据卷里不再写 `mosdns.err.log` |
| C AIO / 自定义配置 / 旧 CMD | AIO 两核心两 Agent RUNNING、oxidns 已定义未启动、默认 argv；`CONFIG_PATH` 含空格传到核心 argv 与 Agent 配置；`/usr/bin/supervisord …` 作为 CMD 仍启动 s6 |
| D macvlan HA | Agent 经 API 应用 HA：本容器 keepalived RUNNING、持有 VIP；kill -9 拉起；docker stop 顺序 keepalived（优雅释放）→ Agent → 核心；docker start 由 HA 记录恢复；DELETE 停止并释放 VIP |
| E host 模式 HA（共享沙箱网络命名空间） | 辅助容器证据、0600 控制套接字、keepalived 等待；主 Agent 环境就绪；应用后 keepalived 在辅助容器运行（主容器内不运行）、VIP 存在；辅助容器重启后 Agent 经新套接字恢复；DELETE 停止 |
| F 精简 Compose（macvlan） | 新镜像首装（ha-compact 登记、服务 RUNNING、进程环境无 INSTALL_KEY、有 HA 环境）、停面板离线重建；旧 Python 镜像首装后同一 compose 换新镜像照常启动 |
| F2 精简 Compose host 模式 | ha-compact 认领、辅助容器（installation 身份）等待并发布证据；两容器重建后再次通过 |
| G 旧面板 compose_direct | 旧镜像（Python）准备+登记；换新镜像后经 python3 兼容脚本转 `ha-compose`，离线核对通过 |
| G2 本面板 compose_direct | 新镜像 init（只读根文件系统）`ha-compose prepare`、Agent `ha-compose run` 登记；离线重建通过 |
| H 迁移检查 | 新镜像 `ha-migration-check` 只读挂载旧（Supervisor 时代）容器卷，输出运行环境 |
| Python 差分 | `go test -tags pyref -run Pyref ./dockerha`（原脚本在同一夹具）：compact 10 场景 + 互相启动、direct 6 场景，消息与文件一致 |

真机（两台不同宿主）HA 切换留给 L6。
