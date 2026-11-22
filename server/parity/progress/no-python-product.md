# 去掉产品里剩余的 Python（no-python-product）

分支 `go-agent-no-python`（基于 go@c02c189f7）。维护者已同意。

## 1. 盘点：用户机器上仍会运行的 Python

只列 Go 面板、Go Agent、两个 Docker 镜像以及面板生成给用户执行的内容；随 `backend/` 一起删除的 Python 面板不算。

| # | 文件 / 位置 | 谁执行 | 在哪执行 | 做什么 | 状态 |
|---|---|---|---|---|---|
| 1 | `docker_ha_compose_text.go` 的 `composeBootstrap`（安装包里的 `bootstrap.py`），入口 `python3 /run/configflow-compose/bootstrap.py` | Compose 安装包（`install_method=compose`，页面不提供，仅接口）的 agent 服务 | 用户 Docker 宿主机，Agent 容器内 | 核对准备记录、首次登记、离线重启核对身份，再 exec `/entrypoint.sh` | **已改**：`configflow-agent ha-bootstrap` |
| 2 | `docker_ha_enrollment.go` 的 `bootstrapSource`（`docker run --entrypoint python3 … -`） | 备用安装脚本（页面「备用安装脚本」/`install_method=script`） | 宿主机临时 Agent 容器 | 首次登记并写身份文件 | **已改**：`ha-enroll`（设置经 stdin） |
| 3 | `docker_ha_guide.go` 的 `enrollmentProof`（`python3 -c`） | 备用安装脚本重复执行 | 临时 Agent 容器 | 核对本次安装的身份与标记 | **已改**：`ha-enroll-check URL` |
| 4 | `docker_ha_guide.go` 的 `migrationCheck`（`docker exec -i 旧容器 python3 -c`） | 迁移脚本 | **旧**容器内 | 核对旧容器名称/地址/端口/HA/服务类型，输出 runtime.env | **已改**：`ha-migration-check`，在新镜像临时容器里只读挂载旧卷执行 |
| 5 | `docker_ha_guide.go` 的 `inspectionCommand`（`docker exec 旧容器 python3 -c`） | 页面「只读检查命令」 | 旧容器内 | 打印旧 Agent 名称/端口/类型/配置路径 | **已改**：`ha-inspect`，同上 |
| 6 | `ha-startup-checks.sh` 的 `cf_check_dns_port`（`--entrypoint python3 … -c socket`） | 备用安装脚本（host 模式） | 宿主机网络空间的临时容器 | 检查 UDP/TCP 53 可用 | **已改**：`ha-port-check` |
| 7 | `docker/docker-compose-compact.py`（+ 它 import 的 `docker-compose-install.py`），入口 `docker-agent-entrypoint-service.sh` 在 `SERVICE_TYPE=oxidns-ha-host/oxidns-ha-macvlan` 时 `exec python3 …compact.py` | **页面默认的「精简 Compose」**（`compose_compact`） | Agent 容器内，每次启动 | 校验安装归属与宿主身份、首次登记、写 Agent/OxiDNS/HA 配置与标记、再进入普通服务入口 | 未改（量大，待定） |
| 8 | `docker/docker-compose-install.py`（`prepare` / `run`），`docker_ha_compose_direct.go` 的 `ComposeRuntime` | `compose_direct`（仅接口） | init 容器与 Agent 容器 | 同上（direct 版本：init 准备、run 登记） | 未改（与 7 共用代码） |
| 9 | `docker/docker-agent-entrypoint-service.sh` 内联 `PYCONFIG` / `PYSUPERVISOR` | 所有 Docker Agent（普通 Docker、AIO、HA） | Agent 容器每次启动 | 用环境变量生成 `config-<服务>.json`（保留已登记身份）；改写 supervisor 核心启动命令 | 未改 |
| 10 | `docker/docker-ha-companion-entrypoint.sh` 内联 `PYCOMPANION` | Docker HA host 模式的 HA 辅助容器 | 辅助容器启动 | 校验只读挂载与安装证明（精简 Compose 的 `.configflow-compose-compact.json`），等待主容器就绪 | 未改 |
| 11 | **supervisord**（Alpine `supervisor` 包本身是 Python，`Dockerfile.agent` 还装 `py3-pip` + `setuptools<81` 供 `pkg_resources`） | Agent 镜像 PID 1；Agent 代码 44 处 `supervisorctl` | Agent 容器 | 管理 agent/mihomo/mosdns/oxidns/keepalived | 未改（量最大） |
| 12 | **supervisord**（`docker/Dockerfile` runtime 阶段 `apk add supervisor`） | 面板镜像 PID 1 | 面板容器 | 管理 Go server 与按需启停的 Sub-Store | 未改 |
| 13 | `docker/auto_update.py`（runtime 中为 `/usr/local/bin/configflow-auto-update.py`） | `configflow-start.sh` 在 `CONFIGFLOW_AUTO_UPDATE=1` 时调用（autoupdate 镜像默认开启）；镜像构建阶段 `--precompress` / `--record-image` | autoupdate 镜像容器启动（用户机器）；构建机 | 拉分支源码、按源码指纹只重建变化的组件（Go server/前端/Agent/node-probe）并安装，失败回退；构建时预压缩前端、记录源码指纹 | 未改 |

不在用户机器上运行、不算产品的：`backend/agents/go-agent/integration/*.py`（验收脚本）、各 `testdata/*.py`、`server/tools/*`、`installertest/gen_*.py`。
`netfilter.go` 只有注释提到 Python。

## 2. 改法

### 已完成（本分支）

- Agent 新包 `backend/agents/go-agent/dockerha`，`main.go` 在解析 Agent 参数前分派子命令：
  `ha-bootstrap`（`-settings/-agent-dir/-setup/-entrypoint` 默认是容器内路径）、`ha-enroll`、`ha-enroll-check URL`、
  `ha-migration-check NAME SERVER PORT`（环境 JSON 走 stdin）、`ha-inspect`、`ha-port-check`。
  中文提示（`messages.go`）、文件布局/权限/字段顺序、幂等与失败行为（登记前先占用标记、身份独占发布、临时文件清理、
  不跟随重定向、不走代理）照原 Python；表单驱动测试覆盖原脚本每个分支（141 个用例，含 exec 交接）。
- 面板生成内容改用子命令（见 behaviour-changes.md 本节）；迁移检查和只读检查在**目标镜像**的临时容器里
  `--volumes-from 旧容器:ro` 执行，所以旧镜像无需新程序；迁移脚本把拉取镜像挪到检查之前。
- 与 Python 参考的比较：`installertest.NoPythonBaseline` / `NoPythonBundle` 把新形式还原成旧 Python 形式（已批准差异，
  旧文本存 `installertest/no-python-legacy.json`）；`GuideBaseline`、A10 生成器回放、Flask 差分 inspect 都经过它。
- installer 里执行产品 Python 的 6 个测试（5 个 bootstrap + 1 个迁移检查）改为构建并运行 Go Agent（`agentBinary`），
  installer 包不再需要 python3；`productPython` 删除。skipaudit 白名单里本来就没有 installer 条目（这 6 个是缺 python3 即失败，
  不是 skip），无需收缩；`design/no-python-tests.md` 附录 B 的说明已更新。
- Agent 1.9.2-go（Agent `upgrade.Version`、面板 `LatestAgentVersion`、版本日志、proddiff/configdiff 例外）。
- 文档：`doc/guide/oxidns-ha.md`（迁移检查/只读检查、安装包、旧部署升级）。

### Dockerfile.agent 的 python3：本任务不能去掉

1–6 改完后，Agent 镜像仍因 7–11 需要 python3（supervisord 本身就是 Python）。已确认镜像内其它 python 调用点只有 7–11。

### 兼容 / 迁移路径

- 已用旧安装包部署的实例：`compose.yaml` 仍是 `python3 /run/configflow-compose/bootstrap.py`，`bootstrap.py` 在持久目录
  `.ha-compose/` 里。新镜像保留 python3，所以照常 `docker compose pull && up -d` 即可（实验 B 验证）。
- 将来镜像去掉 python3 前：把旧 `compose.yaml` 的两行入口改成 `configflow-agent` / `ha-bootstrap`，`ha-bootstrap` 读同一份
  `.ha-compose/enrollment.json`、`setup` 和标记，直接接受已有安装（实验 B 验证，含无 python3 镜像）。也可以在镜像里放一个
  只认 `/run/configflow-compose/bootstrap.py` 的极小 `python3` 兼容脚本（转 `configflow-agent ha-bootstrap`），用户无需改文件。
- 备用安装脚本/迁移脚本是一次性命令，重新生成即用新形式；新形式要求 Agent 镜像 ≥ 1.9.2-go（面板默认配套镜像）。

### 待决定（量大，未动）

见交回报告的提问。

## 3. 验证（2026-11-22，全部在 192.168.0.32；.64 磁盘已满未用）

- Agent 模块：`gofmt -l` 无输出、`go vet ./...`、`go test -race ./...` 全过；`GATE_ONLY=agent server/scripts/gate.sh -p 6` → `GATE STAGE PASS: agent`（dockerha 2.2 s）。
- `go test -race -timeout=60m ./internal/installer/` ok 491.9 s；`./internal/installer/installertest` ok；`./internal/api/agents/...` ok 494.5 s；`./internal/agents/` ok 502.8 s。
- Flask 差分（CONFIGFLOW_DIFF=1）：`TestDockerHADifferentialAgainstFlask` → 239 comparisons equal, 0 differ；`TestInstallerDifferentialAgainstFlask` 通过。
- `server/tools/proddiff/test_proddiff.py` 67 passed。`tools/pyref` 审计通过（登记 B-035）。
- `/tmp/preflight.sh`（vet 两组 tag + 全量非 race）→ `PREFLIGHT PASS`。
- 容器实验 `server/parity/lab/no-python-compose-lab.sh ~/.cache/cf-nopy-old`（旧树 = go@c02c189f7）→ `LAB PASS`：A 新安装包/无 python3 镜像（准备两次、首次登记、停面板离线重启、运行中再准备）；B 旧 bootstrap.py 安装包 → 升级镜像照常、改两行入口用 ha-bootstrap、无 python3 镜像照常。
