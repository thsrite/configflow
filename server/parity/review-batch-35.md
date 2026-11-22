# batch-35 HR 复核：Docker HA 里的 Python 改为 Go Agent 子命令（Agent 1.9.2-go）

复核对象：`c02c189f7..d98d903e0`（cc4973ca0…d98d903e0，7 个提交）。复核修复：`9cc48f457`、`96166f892`，本文件与实验脚本 C 场景在随后的提交里。
全部验证在 192.168.0.32 上运行（开跑前 `df -h ~`：.32 剩 176G，.64 剩 164G）。

## 结论

子命令与原 Python 在安全相关的地方一致，部分更严格；发现 2 个中等问题、若干低风险问题，中等问题和一处低风险问题已在本分支修复。

## 发现

### 中 1：本机有旧版同名镜像时，`ha-*` 子命令会被旧 Agent 当成普通启动（已修，96166f892）

生成脚本只用 `docker image inspect IMAGE || docker pull IMAGE`。面板没有固定配套镜像时（分支、autoupdate 构建）用浮动的 `thsrite/config-flow-agent:latest`，
迁移的主机上通常已经有旧容器用的旧 `latest`，所以不会拉新镜像。1.9.2-go 之前的 Agent 用 `flag.Parse` 解析参数，遇到 `ha-inspect` 这类位置参数就停止解析并忽略，
然后照常启动：

- `ha-migration-check` / `ha-inspect`：旧容器的数据卷里有 `config-oxidns.json`，旧 Agent 带着原身份在 `--network none` 的临时容器里常驻运行，
  `cf_runtime_env=$(…)` 和只读检查一直卡住（数据卷只读，没有写坏数据）。
- `ha-enroll` / `ha-port-check`：没有配置文件，旧 Agent 报“Agent 配置加载失败”退出；53 端口检查会被误报为端口被占用。

Python 版本没有这个问题，因为所有镜像都带 python3。实验 C 用真实的旧 Agent 二进制复现：`docker run … cf-nopy-agent:old ha-inspect` 输出
`Using config file: /opt/configflow-agent/config.json`，也就是照常启动了。

修法：安装脚本和迁移脚本在拉取那一行之后、第一次用子命令之前，先运行
`docker run --rm --network none --entrypoint configflow-agent IMAGE -version`，要求版本 ≥ 1.9.2-go（awk 按数字逐段比较，每段必须是纯数字），
否则提示 `所选 Agent 镜像缺少 Docker 高可用命令（需要 1.9.2-go 或更新）；请先用 docker pull 拉取该镜像的新版本后重试。` 并退出，此前没有任何改动。
只读检查改成 `if 探测; then ha-inspect; else 提示; fi`，不会关闭用户的终端。所有 Go Agent 的 `-version` 都会立即退出；更早的版本不认识这个参数，也会立即失败。
测试：`TestHAAgentReadyComparesVersions`（用真实 shell 跑 9 种输出）、`TestOldAgentImageStopsNewInstallBeforeChanges`（host 和 macvlan 都在改动前停止）、
`TestInspectionSkipsOldAgentImage`，`TestUnsupportedMigrationAbortsBeforeStopOrDataChange` 增加 `old_agent`；实验 C 在真实 Docker 上验证：拒绝旧镜像，接受新镜像。

### 中 2：`NoPythonBaseline` 会把真实差异也还原掉（已修，96166f892）

负向对照（修复前）：

- 只读检查去掉 `:ro`（变成可写挂载）：`strings.TrimSuffix(name, ":ro")` 照样还原成 `docker exec NAME python3 -c …`，和 Python 比较完全相同，差异被掩盖。
- 迁移检查运行的镜像和前一行拉取的镜像不同：正则里检查镜像没有分组，改错镜像同样会被还原掉。

修法：只读检查必须带 `:ro`，迁移检查用的镜像必须就是拉取的镜像，否则原样保留，让比较报出差异；新增的版本探测只在“紧跟同一镜像的拉取”或“包住同一镜像的 ha-inspect”时才去掉。
`TestNoPythonBaselineKeepsRealDifferences` 有 8 个负向对照（可写挂载、联网、镜像不一致、探测不在拉取之后或跟在另一个镜像的拉取之后、核对命令联网）。
仍然的盲区：只读检查用哪个镜像，Python 侧没有对应内容可比，由 `docker_ha_guide_test.go` 直接断言为 `DockerAgentImage()`。

### 低 3：`ha-migration-check` 接受 `null` 和尾随数据（已修，9cc48f457）

`json.Decode` 解析到 `[]any` 时会接受 `null`（容器没有任何环境变量）和 `[] ]`。Python 用 `json.load` 加 `isinstance(list)`，这两种都拒绝，提示“无法读取原容器环境配置”。
现在要求非 nil 且后面没有其他内容；补了 3 个测试用例。

### 低 4：文档写“迁移脚本会先拉取新镜像”，实际只在本机没有时拉取（已改）

`doc/guide/oxidns-ha.md` 已更正，并说明镜像过旧时的提示和处理方法。

### 低 5（记录，不改）：`ha-enroll` 的重定向处理与 Python 不同，Go 更安全

Python 的 `urlopen` 遇到 POST 的 301/302/303 会改成 GET，并把 `Authorization` 带到任何主机；遇到 307/308 报 HTTPError。
Go 默认客户端遇到 301/302/303 同样改成 GET，跨主机时去掉 `Authorization`；遇到 307/308 会重发 POST（只有同主机才保留 `Authorization`）。
登记接口只接受 POST，实际效果相当；凭据不会因为重定向发到其他主机。已写入 behaviour-changes。`ha-bootstrap` 和 Python 一样不跟随重定向、不走代理。

### 低 6（记录，不改）：Compose 安装包遇到旧镜像

`ha-bootstrap` 被旧 Agent 忽略后，旧 Agent 找不到配置文件就退出，容器反复重启，不会跳过归属校验去登记。安装包只能通过接口生成，不做探测。

### 低 7（记录，不改）：`--volumes-from 旧容器:ro` 的范围

迁移脚本先要求旧容器恰好有两个挂载（Agent 目录和 `/etc/oxidns`），所以只带过去这两个。只读检查不限制挂载数，会带上全部挂载，
但都是只读、`--network none`，容器里只运行本镜像的 `ha-inspect`，输出和 Python 一样只有 4 个字段，不含凭据。
`:ro` 挡不住对 docker.sock 之类套接字的连接，不过新程序不会用它。迁移检查输出原容器环境，这一点和 Python 相同。

## 核对通过的项目

1. **逐项对照原 Python**（`no-python-legacy.json`）：
   - `ha-bootstrap`：
     - 归属标记在发 HTTP 请求之前独占写入（`os.Link`，相当于 `os.link`），写入的 url 未经校验，与 Python 相同。
     - 身份文件用独占硬链接发布，绝不覆盖；回执用原子 rename；临时文件 0600 并 fsync，目录也 fsync；失败时删除临时文件。
     - `O_NOFOLLOW` 读文件，锁文件同样 `O_NOFOLLOW` 并检查是普通文件，再 flock。
     - 登记请求不走代理、不跟随重定向（`ErrUseLastResponse` 得到 3xx 响应，提示“首次登记被拒绝（HTTP 3xx）”，与 Python 的 NoRedirect 一致）；响应最多 16384 字节；`success is True` 判断一致。
     - 摘要按排序后的紧凑 JSON 计算，格式校验保证不需要转义。
     - 退出码与提示一致；非拒绝类错误统一映射为“无法安全保存安装身份”（Python 只映射 OSError，其他异常会打出回溯）。
     - exec 交接保留环境变量和参数。
   - `ha-enroll`：符号链接检查、归属标记判断（不是对象或不是 UTF-8 也按“另一份安装”处理）、提示文字、0600 且 `O_EXCL` 的写入顺序（标记在登记之后写，和原脚本一致）、重复执行时逐项比较身份，均一致。
   - `ha-enroll-check`、`ha-inspect`（键顺序、`ensure_ascii=False`、不输出凭据）、`ha-port-check`（不设 SO_REUSEADDR、不 listen）：一致。
   - `ha-migration-check`：环境变量改从 stdin 的 `.Config.Env` 读取，它就是 Python 版本在旧容器里看到的 `os.environ` 的来源。键的插入和覆盖顺序、TZ 默认值、`str(agent_port)` 对数字和字符串的处理、`health_url` 的真值判断，均一致。
   - 凭据只写进 0600 文件，不进 stdout/stderr；用户输入错误会带 `%v` 的路径只有 Go 内部错误，这些在 bootstrap 里都映射成固定提示。
2. **只读与挂载范围**：`--volumes-from X:ro` 让所有继承的挂载都只读，临时容器 `--network none --rm`。拉取镜像挪到检查之前后，拉取失败会因 `set -euo pipefail` 在停止旧容器之前就退出（检查之前只做过 inspect 类只读操作）。
3. **旧部署兼容**：新 `prepare.sh` 不删除 `.ha-compose/` 下的任何文件；旧安装目录的 `setup` 属于旧 owner，新安装包的 prepare.sh 会拒绝接管，不会碰到旧的 `bootstrap.py`；`composeManagedParts` 仍保留这个名字。实验 B 验证：旧 compose 加新镜像（带 python3）照常启动。
4. **NoPythonBaseline**：见中 2；Flask 差分在新增探测之后仍是 0 差异，说明探测只在预期位置被去掉。
5. **版本例外**：与 9d4a3cdcb 和 1.9.1-go 的做法一致，只改 proddiff 的 `GO_AGENT_LATEST` 和 has_update 区间 [1.8.61-go, 1.9.2-go)、configdiff T16/T25 的心跳版本，以及 `TestExistingAgentsCanUpdate` 增加 1.9.1-go；没有扩大例外范围。

## 验证（192.168.0.32，均在复核修复之后）

- `GATE_ONLY=agent server/scripts/gate.sh -p 6` → `GATE STAGE PASS: agent`（dockerha 1.668s，routes 170.689s）。
- `go vet ./... && go test -race -count=1 -timeout=60m ./internal/installer/... ./internal/api/agents/... ./internal/agents/` →
  installer ok 470.4s，installertest ok，api/agents ok 311.3s，api/agents/difftest ok，agents ok 383.5s。
- `CONFIGFLOW_DIFF=1 go test -tags configflow_difftest -run 'TestDockerHADifferentialAgainstFlask|TestInstallerDifferentialAgainstFlask'` →
  `docker HA routes: 239 comparisons equal, 0 differ`，`installer routes: 293 comparisons equal, 0 differ`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-35 -p 8` → `PREFLIGHT PASS`。
- `server/parity/lab/no-python-compose-lab.sh ~/.cache/cf-nopy-old`（旧树 = `git archive c02c189f7`）→
  `LAB PASS: A (new bundle, no python3), B (upgrade from the bootstrap.py bundle), C (image version probe)`。
