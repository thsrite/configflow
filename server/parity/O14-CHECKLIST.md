# O14 切换清单（前置条件、删除清单、发布步骤与文档审计）

- 基线：`go@6c1c933a1`（2026-11-25 更新，原建表 `go@595972901`），去 Python 化切换预演分支 `go-switch-rehearsal@ce55d9afd`。
- 配套计划：[o14-switch-plan.md](o14-switch-plan.md)（维护者生产切换步骤、公开发布说明、L6 演练脚本）、[o12-release-notes.md](o12-release-notes.md)（行为变化与公开升级说明）、[no-python-tests.md](../design/no-python-tests.md)（裁定 C 测试去 Python 化方案）、[o10-pve-baseline.md](o10-pve-baseline.md)（最终 PVE CT 240 性能验收）、[o14-configdiff-final.md](o14-configdiff-final.md)（P9d 最终归档）、[L6.md](lab/L6.md)（实验网 L6 切换演练与 24h 浸泡）。
- 本文汇总 `HANDOFF.md` 与各设计文档中散落的所有切换前置条件、切换提交删除清单、发布与回滚步骤，以及 O14 相关文档的矛盾审计。文中不出现任何真实主机名、IP、token 或域名。

---

## 1）切换前置条件总表（全部满足方可定切换窗口）

状态口径：**已满足**（已合入 `go` 并通过门禁）、**进行中**（正在执行）、**待切换窗口**（由维护者在发版/切换窗口操作核验）。

| # | 前置条件 | 判定方法（命令 / 文件） | 当前状态（2026-11-25） | 负责人 |
|---|---|---|---|---|
| P1 | **O13 完整性终审**：`routes.tsv` 380/380 全覆盖，无功能缺口 | `server/parity/o13-completeness.md` 结论 0 缺口 | **已满足**（`65d03d17c`） | 协调者 |
| P2 | **#40 面板发行镜像（无 Python / 无 supervisord）**：`tini` 作 PID 1，Go 自管内置 Sub-Store（独立进程组、退避重启、空闲休眠、脱敏日志），`cmd/configflow-autoupdate` 取代 `auto_update.py`；内置 `ca-certificates`、`tzdata`（默认 `TZ=Asia/Shanghai`）、版本号注入（非 `dev`）、legacy-only 拒绝启动 | `docker/Dockerfile`、`server/parity/progress/nopy-panel-image.md`；测试服务器运行 `server/scripts/panel-image-smoke.sh` 与 `server/scripts/autoupdate-image-smoke.sh` 均 `ALL PASS` | **已满足**（batch-20 + batch-36 已合并；L6 最终候选镜像 `3.0.0-rc.l6-6c77892c7` 冒烟与实机切换通过） | 协调者 |
| P2b | **Agent 发行镜像（无 Python / s6-overlay）**：s6-overlay v3.2.3.2 取代 supervisord，无 `python3`；Docker HA 引导/迁移/检查/伴随容器改由 Agent 子命令实现；`service_manager` 新写 `"s6"`，旧值 `"supervisor"` 读为同义词（下个大版本移除）；保留 `docker/compat/{supervisorctl,python3}` 过渡脚本（下个大版本移除）；Agent 版本 ≥ **`1.9.3-go`** | `docker/Dockerfile.agent`、`backend/agents/go-agent/upgrade/migration.go`（`Version = "1.9.3-go"`）、`server/internal/agents/version_changelog.go`（`LatestAgentVersion = "1.9.3-go"`）、`server/parity/progress/nopy-agent-image.md`；`server/parity/lab/nopy-agent-image-lab.sh` A–H + D2 与 L6 §5.6 全通过 | **已满足**（batch-35 + batch-37 已合并；L6 §5.6 原地升级与故障演练通过） | 协调者 |
| P3 | **O9 安全复核两轮**：全树无未关闭 high/medium；二轮 L1/L2/L3/L4/L6 已修，L5（`config_token` = MCP 权限）保持 | `server/parity/review-o9-round1.md`、`server/parity/review-o9-round2.md` | **已满足**（batch-20 已合并） | 协调者 |
| P4 | **O10 性能与内存验收**：正式测量机（PVE 实验宿主独立性能 CT，同机同快照）上 **Go ≤ Python**：空闲 CPU（平均、10 s 窗口中位、p10）、RSS、全部测量路由中位延迟；R11 分配上限、R13 PSL trie、R12 GC 策略（`GOGC=100`，沿用容器内存 ×0.8 软上限）已落地；派生缓存负载后存活堆增量目标 ≤ 10 MB；最终发布候选代码上复测通过 | `server/parity/o10-pve-baseline.md`、`server/parity/o10-r13-r12.md`；PVE CT 240 同机同快照 Go×2 + Py×1 + 方案 B Go-1 实测 | **已满足**（`8aa0000d2` / `e93955be3`：空闲 CPU `0.369%` vs `1.507%`、空闲 RSS `54.0` vs `90.1 MB`、存活堆增量 `+5.97 MB`、19/19 路由快 1.46×–24.66×，`mihomo` 21.86 MB / `provider` 24.54 MB 均低于 30 MB 目标） | 协调者 |
| P5 | **O12 行为变化台账定稿**：M01–M19 维护者逐条裁定；其中 M15/M16/M18 切换前已关闭；M07 按已知限制切换后实现；已同步无 Python 镜像、O17 runtime-state 及 `doc/guide/upgrade-v3.md` | `server/parity/o12-release-notes.md`、`doc/guide/upgrade-v3.md`；已核查无过时 `supervisord`/`auto_update.py` 表述 | **已满足**（`d8a803340` 同步，2026-11-25 终审无过时项） | 协调者 / 维护者 |
| P6 | **O15 架构文档**：`server/ARCHITECTURE.md` 完备并反映最新存储、组合根、后台任务与 `third_party/yamlv3` 结构 | `server/ARCHITECTURE.md` | **已满足**（`e93955be3` 同步完成） | 协调者 |
| P6b | **O18 遗留对齐清理**：T1a–T1d（`model` 改为普通结构体、`encoding/json/v2`）、T2/T3/T4/T8 及小清理全部合入 | `server/HANDOFF.md`（batch-42 合入，O18 COMPLETE） | **已满足**（batch-42 已合并） | 协调者 |
| P6c | **O17 地道 Go 结构与清理**（维护者 2026-11-23 裁定全部在切换前完成）：1.6 CRUD 请求体类型化、runtime-state (`storage_version` 1→2 + `agent_state` + `restored` fail closed + 变化通知)、1.7 OxiDNS、1.8 RouterOS/iKuai、剩余 `model.Fields`、3.7 路由器分层（R 线）、1.5 组合根去全局状态（W0–W11）、小清理、前-1 死代码与 UI 占位脱敏 | `server/parity/o17-idiomatic-go.md`、`server/HANDOFF.md`；各子分支均通过跨模型独立复核与全量门禁 | **已满足**（`81a07ae8a` 全部收口，O17 ALL DONE） | 协调者 |
| P7 | **实验网 L1–L5 全部通过、缺陷修复合入**：安装器/升级（L1）、发布/激活/回滚/恢复（L2）、规则同步/DNS 跟随/DNS HA（L3）、Linux 网关/IPv6（L4）、RouterOS/iKuai（L5），以及 HA 10 分钟成员失联自动补完 | `server/parity/lab/L1.md`–`L5.md`；缺陷修复 batch-24/25/31 已合并 | **已满足**（L1–L5 通过；HA 自动补完与 s6 双宿 HA 已在 L6 真机复验通过） | 协调者 |
| P8 | **实验网 L6（最终候选代码）**：24 h 浸泡（RSS/CPU/线程/fd 稳定）+ O14 C 节切换演练（含 O17 v1→v2 迁移与 `restored` 恢复锁定、s6 Agent 1.9.3-go 双宿 HA 切换、F5/F6/F9/F12/重启、HA 10 分钟失联自动补完、D5 开启自动同步下的首启观察） | `server/parity/lab/L6.md` 结论全部通过、无未关闭缺陷 | **进行中**（全部切换、停损、s6 Docker HA、O17 迁移与 `restored`、HA 自动补完实机演练已执行完毕；24 h `go-24h` 浸泡进行中） | 协调者（执行）/ 维护者（已批 D9） |
| P9 | **切换前 1 周内最后一次完整 Python 对比门禁**：在最终切换候选树上跑通完整 Python 对比门禁（Python vs Go `proddiff` `new=0`、difftest 24 包、synthetic live、core race、Agent、fuzz、frontend） | `server/scripts/gate.sh` 全绿 + `proddiff` 摘要 `new=0 stale=0` | **已满足**（`8aa0000d2` / `6c1c933a1`：`.64` FULL core + `.32` compare `proddiff` 2,831 用例 `new=0 stale=0`、difftest 24 包 214/214、LIVE 46/46、pyref check、前端 3,469/3,469 全绿） | 协调者 |
| P9b | **WP9 `proddiff` Go-vs-Go + Go 比较器就绪**：`PRODDIFF_MODE=go PRODDIFF_COMPARATOR=go` 脱离 Python 运行，且与 Python 比较器 0 差异 | `server/parity/progress/nopy-proddiff-go.md`；`proddiff.sh` Go-vs-Go 通过 | **已满足**（已合并；切换提交已改默认并删 Python 侧） | 协调者 |
| P9c | **去 Python 化门禁预演与 `skipaudit` 清零**：按裁定 C 备妥切换提交；`server/internal/skipaudit/allowed.txt` 清空为 **0 条**；**两次连续完整无 Python 门禁通过** | `go-switch-rehearsal@ce55d9afd`：`.py` 文件 `0`、`skipaudit` 白名单 `0`；`.64` 两次连续 `GATE_ONLY=all GATE_FULL=1` PASS 日志（`rehearsal-ce55d9afd-pass1` & `pass2`） | **已满足**（2026-11-25 连续两轮 `EXIT=0`、`GATE STAGE PASS: all`、`proddiff Go-vs-Go new=0 stale=0`、前端 3,469/3,469） | 协调者 |
| P9d | **configdiff 最终归档**：在切换候选树上最后运行一次 `configdiff all`，归档结果到 `server/parity/`，确认 closed findings 均有 Go 回归测试 | `server/parity/o14-configdiff-final.md`（私有原始结果留存 `.32`） | **已满足**（`e93955be3`，10,651 请求变体完整归档，0 未解释业务回归） | 协调者 |
| P10 | **前端清理遗留无效 API 辅助函数**：删除 `frontend/src/api/index.ts` 中未使用的 `generateDockerCompose` / `generateDockerRun` | `grep -rn "generateDockerCompose\|generateDockerRun" frontend/` 无输出，`npm test` 通过 | **已满足**（`81a07ae8a` 已合并） | 协调者 |
| P11 | **CI 工作流切换准备**：`.github/workflows/checks.yml` 去掉 Python pytest 分片并换成 Go server；`.github/workflows/release.yml` 适配预发布（`v*-*` 时 `prerelease: true` 且不写 `latest`） | `release.yml` 已合入 `go`（`d8a803340`）；`checks.yml` 已在切换提交 `go-switch-rehearsal@ce55d9afd` 就绪，合入 `main` 后观察首轮 CI 全绿 | **已就绪（随切换提交合入 `main` 生效）** | 协调者 / 维护者 |
| P12 | **「最后一个 Python 版本」标签确认与长期保留**（裁定 D2）：以 Go 合入 `main` 前的最后一次 Python 发布为准（当前最新 tag 为 `v2.2.0`），长期保留对应镜像标签并写入公开升级说明 | `git tag --sort=-v:refname \| head -n 5`；`docker buildx imagetools inspect thsrite/config-flow:2.2.0` | **已满足（发版前复核最新 tag 是否仍为 `v2.2.0`）** | 维护者 |
| P13 | **配套 Docker Agent 镜像（≥ `1.9.3-go`）先于或同时于 Go 面板镜像发布**：同一 `v*` 标签下 `docker-agent.yml` 推出 `thsrite/config-flow-agent:<版本>`（`linux/amd64` + `linux/arm64`），且 Go 面板镜像构建注入同一标签（`version.AgentImage`） | `docker buildx imagetools inspect thsrite/config-flow-agent:3.0.0-rc.1`；L6 已实机验证面板注入配套 `1.9.3-go` 标签 | **CI 与实验网均已就绪（打 tag 时逐项核验）** | 维护者 |
| P14 | **候选镜像与浮动标签开关核验**（裁定 D3/D4）：切换镜像 = 公开发布候选镜像（同一 digest），架构仅 `linux/amd64` + `linux/arm64`，版本号非 `dev`；约 7 天观察期结束前仓库变量 `PUBLISH_LATEST` **不得为 `true`** | 容器启动横幅版本号；GitHub Actions 仓库变量 `PUBLISH_LATEST != true` | **待切换窗口（发版时核验）** | 维护者 |
| P15 | **生产切换窗口确定**（裁定 D1）：L6 24h 浸泡完成后，由维护者选定低峰 60 分钟窗口 | 维护者确认 | **待切换窗口（L6 24h 浸泡完成后定）** | 维护者 |

---

## 2）各模块交接项（生产切换必看）

- [ ] **A14 / 设计 §7.1（一次性 `changed` 全量推送）**：Go 的 YAML 字节格式、tar/gzip 与 D2 provider 路径与 Python 不同，切换后每台 Agent 会一次性显示 `changed`。必须逐台全量发布，确认核心加载成功、部署状态 `succeeded`、content sync 变为 `synced`。不要改指纹或清状态消掉 `changed`，也不要把 `proddiff` 的测试副本格式迁移用于生产（见 [proddiff README](../tools/proddiff/README.md)）。
- [ ] **O9-H2 / L2 / P13（Agent ≥ `1.9.3-go` 镜像同发）**：同一 `v*` 标签下先确认 `thsrite/config-flow-agent:<版本>`（amd64 + arm64）已可拉取，再切换/公布面板镜像。不要改用旧 `latest`（旧入口不识别一次性注册凭据，开启登录后新装 403）。1.9.3-go 起 Docker 镜像改用 s6-overlay、无 Python，`service_manager` 新写 `"s6"`、旧 `"supervisor"` 读为同义词；旧 Compose/脚本的 `supervisorctl`/`python3` 兼容层与 `"supervisor"` 同义词计划在下个大版本移除。
- [ ] **O17 runtime-state（v1→v2 迁移前强制备份与恢复标记 fail closed）**：
  - 首次打开 `storage_version = 1`（Python 或旧 Go）的 `configflow.sqlite3` 时，Go 在 `.config.lock` 下先在线备份至 `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3` 并校验 `PRAGMA integrity_check = ok`，**备份失败则拒绝启动**，再以单事务将 Agent 运行状态/历史迁入 `agent_state` 表并升至 `storage_version = 2`（不改变 `generation`）。
  - 完整导出（`/api/config/export`）与 WebDAV 备份不再含 Agent 运行状态。使用**不含状态的新格式备份**恢复后，导入事务会给每台 Agent 写入 `restored` 标记并按 **fail closed** 锁定（HA 配对锁生效、受保护字段编辑与换绑 409、规则同步与 DNS 联动暂停自动推送），界面顶部横幅与卡片徽标提示「运行记录未包含在备份里，各 Agent 需要重新发布一次」；**唯一解除方式是对该 Agent 完成一次成功部署（HA 成员走成对发布）**。
- [ ] **N8-7 / N8-9（通知重现与规则缓存迁移）**：
  - D2 原生 notification fingerprint 会让仍存在的问题对应的已忽略通知重新出现一次，查看后再忽略即可，不触发自动同步/发布。
  - 保留旧远程规则缓存文件，不手工改名或删除；首次全量推送前通过规则库缓存刷新入口预热，由 Go 校验后迁入新文件名。
- [ ] **D5（首启保持自动同步开启）**：维护者已裁定切换首启不暂停 Agent/路由器自动同步。因此必须在影子启动与 L6 演练中确认生成结果与当前设备一致；首启后立即观察第一轮自动同步结果，发现异常按 [o14-switch-plan.md](o14-switch-plan.md) A.5 停损。

---

## 3）切换提交删除清单与 `proddiff` 切默认值位置（裁定 C）

> 裁定 C（维护者 2026-11-22）：先在切换候选树上跑完最后一次带全部 Python 对比的完整门禁；**合并到 `main` 的切换提交**同步删除 `backend/`（Python 侧）与所有 Python 依赖测试、工具和门禁阶段，之后门禁只保留 `core race + Agent 模块 + fuzz + frontend + proddiff Go-vs-Go`。以下路径均已在 `go@595972901` 通过 `git ls-files` 逐项核实。

### 3.1 仓库删除清单（`git ls-files` 核实结果）

| 类别 | 删除路径（精确路径） | `git ls-files` 实测文件数 | 说明 / 保留项 |
|---|---|---:|---|
| **Python 后端源码与测试** | `backend/`（**保留** `backend/agents/go-agent/` 与 `backend/node-probe/`） | **616** | 含 `backend/*.py`、`backend/{agents,common,converters,mcp_server,models,routers,routes,schemas,testdata,utils}/`、`config_template.json`、`conftest.py`、`requirements.txt` 等。**禁止删除** `backend/agents/go-agent/`、`backend/node-probe/` |
| **24 个 `*/difftest` 包** | `server/internal/agents/{dnsfollow,dnsha}/difftest`<br>`server/internal/api/{agents,aggregations,config,gatewayonboarding,linuxgateway,oxidns,rulelibrary,rules,subscriptions}/difftest`<br>`server/internal/{devicenotes,discovery,health,httpx,mcp,mosdns,provider,refresh,subs,substore,tasks,webdav}/difftest`<br>`server/internal/gateway/installation/difftest` | **24 个包**（全删） | 含各包 `testdata/`；共 214 个顶层测试（含 24 个仅校验比较器的 Go 负控） |
| **普通包 Python 参考脚本** | `server/internal/**/testdata/*.py` | **248** | 已提交语料（`*.json.gz` 等）**保留**，每个 `testdata/` 目录留一行来源说明 |
| **普通包 Python 依赖测试** | [no-python-tests.md 附录 B.2](../design/no-python-tests.md#b2-普通包里要删的测试34-个包126-个顶层测试) 所列 34 个包 126 个顶层测试 + 无调用方的 `xcPython`/`pythonSQLite`/`pythonTypes` 等 helper | 34 包 126 测试 | 规则：若测试同时含 Go 自身断言，优先只删 Python 对照部分、保留 Go 断言；纯 Python 互测整段删除。**保留**附录 B.3 的 18 个纯回放测试（已去哈希校验，不启 Python） |
| **Python 差分/预言机工具** | `server/tools/configdiff/`（230）、`server/tools/routerdiff/`（85）、`server/tools/yamlreaderoracle/`（1）、`server/tools/yamlwriteroracle/`（2）、`server/tools/yamldiagnosticoracle/`（1）、`server/tools/gatediscover/`（2）、`server/tools/pyref/`（10）、`server/tools/requirements-tools.txt`（1）、`server/tools/security_headers.py`（1） | **333** | `configdiff` 退役前先完成 P9d 归档；`tools/pyref` 的零 Python 审计能力按需精简保留或由 `skipaudit` 承担 |
| **`proddiff` Python 侧** | `server/tools/proddiff/{flask_worker.py,python_generators.py,run.py,generators.py,artifact_policy.py,content_policy.py,provider_policy.py,test_proddiff.py,test_artifact_policy.py,crosscheck.py}`（10）、`server/tools/proddiff/baseline/`（5）、`server/parity/proddiff-baseline.txt`（1） | **16** | **核实修正**：`no-python-tests.md` B.1 原列 `a13_proof.py`/`a18_proof.py` 当前树上已不存在（实测 0）；保留 `tools/proddiff/{comparator,generator,README.md}` 与 `parity/proddiff-baseline-go.txt` |
| **门禁与一次性 Python 脚本** | `server/scripts/{gate-live.py,gate-test-exec.sh,test-gate-live.py,review-n8-2-startup.py,a20-mutation-gate.py,r08-mutation-gate.py,r10-mutation-gate.py,r11-mutation-gate.py}` | **8** | `server/scripts/test-gate-routing.py` 去掉 difftest/live 路由与 python proddiff 用例（或改为纯 Go 测试） |
| **性能工具 Python 侧** | `server/tools/o10perf/perf.py`（及 `run.sh`/`pve.sh`）中的 `--servers python` 分支 | 部分修改 | P4 最终 PVE 验收完成后移除 Python 侧 |
| **`testgolden` 与 `skipaudit`** | `server/internal/testgolden/` 中的 record/check、`PYREF_*`、源码/venv 指纹、假解释器负控；清空 `server/internal/skipaudit/allowed.txt`（当前 90 条 → **0 条**） | 修改 + 清空 | 保留 `testgolden` 纯回放与「每条录制必须被消费」检查；`skipaudit` 必须通过且白名单为空 |
| **根目录 / Docker / CI** | `scripts/test-backend.sh`（1）、`docker/nginx.conf`（1）、`.github/ci/backend-durations.json`（1）；修改 `.github/workflows/checks.yml`（删 `backend-tests`/`backend` pytest 分片，新增 Go server 检查；**保留** `scripts/ci/shard_tests.py` 供 `frontend` job 使用）；更新 `AGENTS.md` 测试节 | **3 删除 + 2 修改** | `docker/nginx.conf` 经核实已无引用；`scripts/tests/*_lab.py` 为历史实验脚本，是否清理见第 5 节 C-4 |
| **测试服务器清理** | 退役 `~/cf-test/.venv`（与 Python 测试不再需要任何解释器） | 服务器环境 | 切换提交合入且首次无 Python 门禁通过后执行 |

### 3.2 `proddiff` 默认值改为 `go` / `go` 的精确位置

切换提交需把 `PRODDIFF_MODE` 与 `PRODDIFF_COMPARATOR` 默认值从 `python`/`python` 改为 `go`/`go`（并删除 `python` 分支）：

| 文件 | 当前位置（行号） | 切换提交改动 |
|---|---|---|
| `server/scripts/gate.sh` | L10 `proddiff_mode=${PRODDIFF_MODE:-python}`<br>L17 `proddiff_comparator=${PRODDIFF_COMPARATOR:-python}`<br>L11–L22、L169 | 默认值改为 `go` / `go`；移除 `python` 分支、`difftest`/`live` 阶段与 `PYREF_INPUT_FINGERPRINT`；`GATE_ONLY` 去掉 `difftest`，`compare` = `proddiff + frontend` |
| `server/scripts/proddiff.sh` | L5–L12 注释<br>L18 `mode=${PRODDIFF_MODE:-python}`<br>L19 `comparator=${PRODDIFF_COMPARATOR:-python}`<br>L31–L62、L107 | 默认值改为 `go` / `go`；移除 `python` 分支、`PYTHONPATH` 设置与工具 venv 构建逻辑 |
| `server/scripts/test-gate-routing.py` | L80、L104–L105、L247–L268 | 移除 `PRODDIFF_MODE=python` / `PRODDIFF_COMPARATOR=python` 及 `difftest`/`live` 路由用例，默认断言 `go`/`go` |
| `server/tools/proddiff/comparator/main.go` | L48–L50（校验 `PRODDIFF_MODE == "go"`） | 保留（或默认空值亦视为 `go`），确保不传 `PRODDIFF_MODE` 时也能直接运行 |
| `server/scripts/gate.md` | L40–L41 及相关阶段表 | 更新 `proddiff` 默认值说明与删去 `difftest`/`live` 后的门禁阶段说明 |
| `server/tools/proddiff/README.md` | L22–L23、L44–L48 | 更新为仅保留 Go-vs-Go + Go 比较器说明，删除 Python 参考模式说明 |

---

## 4）发布步骤、版本号与回滚方案

### 4.1 版本号与标签策略（裁定 D2、D3、D4）

- **最后一个 Python 版本**：`v2.2.0`（以 `go` 合入 `main` 前最后一个 Python 发布为准，长期保留 `thsrite/config-flow:2.2.0`，不覆写）。
- **Go 首发预发布（维护者切换 + 约 7 天观察期）**：主版本 +1，建议标签 **`v3.0.0-rc.1`**（按 `docker.yml` / `docker-agent.yml` 的 semver 规则）：
  - 面板 runtime：`thsrite/config-flow:3.0.0-rc.1`（`linux/amd64`、`linux/arm64`）
  - 面板 autoupdate：`thsrite/config-flow:3.0.0-rc.1-autoupdate`（`linux/amd64`、`linux/arm64`）
  - 配套 Agent 镜像：`thsrite/config-flow-agent:3.0.0-rc.1`（`linux/amd64`、`linux/arm64`，内置 Agent ≥ **`1.9.3-go`**）
  - **浮动标签不动**：仓库变量 `PUBLISH_LATEST` 保持非 `true`，且标签含 `-` 预发布后缀，`latest` 与 `autoupdate` 均保持指向最后一次 Python 构建。
- **正式公开发布（约 7 天观察期无阻断问题后）**：
  - 打正式标签 **`v3.0.0`**（先确认是否开启 `PUBLISH_LATEST=true` 同步移动浮动 `latest` 与 `autoupdate`；若想先发 `3.0.0` 再择日移动浮动标签，先保持 `PUBLISH_LATEST` 关闭，确认稳定后再开）。

### 4.2 面板镜像与 Agent 镜像发布顺序（P13 / B.2.1）

1. **合入 `main`（切换提交）**：在最终候选树跑完最后一次完整 Python 对比门禁（P9）与两次无 Python 门禁预演（P9c）后，合入 `main`（同一提交完成第 3 节删除清单、CI `checks.yml` 切换与 `proddiff` 默认值切换）。确认 `main` 上 `checks.yml` 全绿。
2. **打预发布标签 `v3.0.0-rc.1`**：同时触发 `docker-agent.yml`、`docker.yml`、`release.yml`。
3. **先验收 Agent 镜像（硬门槛）**：等 `docker-agent.yml` 的 `merge` 任务成功，执行：
   ```bash
   docker buildx imagetools inspect thsrite/config-flow-agent:3.0.0-rc.1
   ```
   确认 `linux/amd64` 与 `linux/arm64` 均已就绪。若构建失败，重跑 `docker-agent.yml`，**绝不**把面板或文档改成 `latest`。
4. **再验收面板镜像（runtime + autoupdate）**：等 `docker.yml` 完成，核验 `thsrite/config-flow:3.0.0-rc.1` 与 `3.0.0-rc.1-autoupdate` 的双架构 manifest 与 digest；在测试服务器做一次 [o14-switch-plan.md](o14-switch-plan.md) A.2 影子启动，确认启动横幅版本号非 `dev`、Agent 镜像注入为 `thsrite/config-flow-agent:3.0.0-rc.1`、无异常 ERROR。
5. **维护者生产切换**：按 [o14-switch-plan.md](o14-switch-plan.md) A.3（使用固定 digest）执行切换，进入约 7 天观察期（A.4）。
6. **公开发布 `v3.0.0`**：观察期通过后重复步骤 2–4 发布 `v3.0.0`，按裁定 D3 择机开启 `PUBLISH_LATEST=true` 移动浮动 `latest` / `autoupdate`，并发布包含 [o12-release-notes.md](o12-release-notes.md) 第 5 节的公开升级说明。

### 4.3 停损与回滚方案（不回滚 Python，含 runtime-state 备份恢复）

**总原则**：Python 已永久退休，**不回滚到 Python**；设备侧不依赖面板运行，出问题时先**停止对设备的写入**，再按场景恢复数据到 **Go** 或修复前进。

1. **场景 A：v1→v2 迁移阶段异常或升级后数据写坏 — 用 `DATA_DIR` 冷备份 / 迁移前强制备份回滚到 Go**
   - **两份可用备份**：
     1. 切换前整目录冷备份（A.3 第 1 步，含 `configflow.sqlite3` 及 `-wal`/`-shm`、规则缓存、Sub-Store、traffic/metrics）；
     2. O17 `runtime-state` 在首次打开 v1 数据库时自动生成的迁移前强制备份：`DATA_DIR/backups/configflow-v1-<UTC>.sqlite3`（已通过 `PRAGMA integrity_check = ok`）。
   - **回滚步骤（恢复到 Go）**：
     1. `docker compose stop` 停止 Go 容器（留足 ≥ 10 s 排空，不要 `kill -9`）。
     2. 复制当前异常的 `DATA_DIR` 另存留证（用于测试服务器复现，不覆盖原备份）。
     3. 若是整目录损坏或需完全回到切换前状态：清空 `DATA_DIR` 后恢复 **A.3 整目录冷备份**；若仅需回退 SQLite 到迁移前 v1 状态：移除当前 `configflow.sqlite3` 及 `-wal`/`-shm`，将 `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3` 复制回 `DATA_DIR/configflow.sqlite3`（保留属主权限）。
     4. 用修复后的 Go 镜像（或确认无存储格式差异的上一 Go 镜像）启动容器；Go 会重新执行原子 v1→v2 迁移（生成新的时间戳备份）。
     5. 重做切换后的手动操作，并按 A.3 第 5 步对已推送过的 Agent 复核/重推。
   - **严禁**：用 Python 容器挂载已经迁到 `storage_version = 2` 的 `DATA_DIR`（Python 无法读取 v2，且违背不回退原则）。仅在极端排查需要只读查看旧数据时，可在隔离网络里对**冷备份副本**只读查看（D10）。

2. **场景 B：用新格式 JSON 导出（`/api/config/export`）或 WebDAV 备份恢复（不含运行状态）**
   - 恢复完成后，导入事务自动给每台 Agent 写入 `restored` 恢复标记并进入 **fail closed** 锁定（HA 配对锁生效、编辑受保护字段/换绑返回 409、规则同步与 DNS 联动暂停自动推送）。
   - **解除流程**：核对界面顶部横幅提示 → 逐台对普通 Agent 执行一次完整发布（`POST /api/agents/<id>/push-config`），对 HA 成员按配对流程执行成对发布 → 每次成功部署会在同一事务中清除该 Agent 的 `restored` 标记并恢复正常。

3. **场景 C：新 Go 修复版引入回归**
   - 仅当两个 Go 版本之间**无存储格式变更**（例如同为 `storage_version = 2`）时，可直接回退到上一 Go 版本镜像 digest；若存在存储版本升级，则修复前进。

### 4.4 需同步更新的 `doc/` 说明与截图清单

按根目录 `AGENTS.md` 规则（功能或操作入口变更时必须同步更新 `doc/` 说明和截图），核查当前仓库状态如下：

| 文档 / 截图 | 当前状态 | 切换/发布前需同步内容 | 负责阶段 |
|---|---|---|---|
| `doc/guide/install.md` | 已更新无 Python / tini / Sub-Store / `SSL_CERT_FILE` / `DATA_DIR` / 旧版迁移提示 | 公开发布时补入**具体「最后一个 Python 版本号」（如 `2.2.0`）**与 Go 首发版本号升级指引；预发布观察期内说明如何固定版本标签拉取 | 发布前（维护者 / 协调者） |
| `doc/guide/agent-install.md` | 已更新 `1.9.0-go` 首次注册凭据与配套镜像标签 | 同步核对最低配套版本提升至 **`1.9.3-go`**（无 Python / s6-overlay / Docker HA 子命令）；确认截图/命令示例与实际一致 | 切换前 |
| `doc/guide/oxidns-ha.md` | 已更新 `1.9.2-go` / `1.9.3-go` s6-overlay、过渡脚本说明 | 核对是否有遗留 `Supervisor` 措辞（已确认仅在历史迁移上下文中出现，合理） | 已基本同步（发版前复核） |
| `doc/manual/agents.md` | 已更新 1.9.3-go s6-log 合并日志、`CorruptStateError` 恢复 | O17 `runtime-state` S3 合入时同步补充：**不含状态备份恢复后的 `restored` 锁定行为、顶部横幅与重新发布解除步骤**，并补新截图 | O17 S3（`go-o17-runtime-state`） |
| `doc/manual/system-settings.md` | 已更新 Go 自管 Sub-Store、legacy 升级提示、冷备份步骤 | O17 `runtime-state` S3 合入时同步更新「备份与升级」表格：**明确「导出全部数据」与 WebDAV 备份不再包含 Agent 运行状态/部署历史，恢复后需对各 Agent 重新发布一次**；补入 `DATA_DIR/backups/configflow-v1-*.sqlite3` 自动迁移备份说明 | O17 S3（`go-o17-runtime-state`） |
| `doc/manual/domain-discovery.md` + `doc/img/guide/domain-discovery-damaged-store*.png` | 已有损坏恢复 UI 截图（桌面 + 移动） | 已随 M18 同步，发版前抽查文案一致 | 已满足 |
| `doc/img/guide/oxidns-ha-auto-complete-waiting.png` | 已有 HA 自动补完等待态截图 | 已随 batch-31 同步 | 已满足 |
| **新增截图**（待 O17 S3 补拍） | 尚未拍摄 | 拍摄 Agent 页**恢复标记顶部横幅与卡片徽标**（桌面浅色 + 移动深色），嵌入 `doc/manual/agents.md` 与 `doc/manual/system-settings.md` | O17 S3（`go-o17-runtime-state`） |
| 历史设计/验证文档（`doc/design/*`、`doc/oxidns-validation-20261016.md`、`doc/client-fixed-ip-validation.md`、`doc/oxidns-device-policies.md`） | 引用旧 `backend/` 路径 | 属于历史设计/实验记录，不在用户手册导航中，可保留原貌或加注历史档案头（见第 5 节） | 可选（不阻塞） |

---

## 5）O14 相关文档矛盾与过时项审计

### 5.1 本轮已直接修正的过时/矛盾项

| # | 位置 | 原内容（过时/矛盾） | 已修正为 |
|---|---|---|---|
| F1 | `o14-switch-plan.md` A.0 P2 / S6 / F11 / B.2 / B.3、`o12-release-notes.md` §4.5 / §5 / D06 | 仍写「保留 `supervisord` 管 Sub-Store」「`docker/auto_update.py`」 | 对齐 batch-36/37：面板镜像无 Python/supervisord，用 `tini` 作 PID 1、Go 自管 Sub-Store、`cmd/configflow-autoupdate` 取代 `auto_update.py`；Agent 镜像（`1.9.3-go`）用 s6-overlay v3.2.3.2、无 Python |
| F2 | `o14-switch-plan.md` A.0 前置条件表 | 仍停留在 2026-11-22 早期状态（P2/P3/P5b/P7/P11 写「进行中/未开始」，P4 写绝对 146 MB 基线，缺 O18/O17/去 Python 测试） | 更新为 2026-11-23 最新状态并与本文第 1 节对齐 |
| F3 | `o14-switch-plan.md` A.1-5 / A.3 S8 / 第 7 步-4 | 正文仍建议「首启前先关掉自动同步」，与末尾维护者裁定 **D5（首启保持自动同步开启）** 矛盾 | 正文同步对齐 D5 裁定（不暂停自动同步，首启后立即观察首轮自动同步结果） |
| F4 | `o14-switch-plan.md` B.3.1 / `docker.yml` 注释示例 | 举例版本号写 `2.0.0-rc.1`，但仓库最新 Python tag 已是 `v2.2.0` | `o14-switch-plan.md` 与本文统一改为 `3.0.0-rc.1`（主版本 +1） |
| F5 | `o12-release-notes.md` M15 / M18 | M15 仍写「切换后小任务 / 进行中」、M18 仍写「进行中」 | 更新为已合入 batch-20 |
| F6 | `no-python-tests.md` B.1 删除清单路径偏差 | 列出 `server/tools/proddiff/a13_proof.py` 与 `a18_proof.py`（当前树已不存在），遗漏 `docker/nginx.conf`、`.github/ci/backend-durations.json`；`skipaudit` 记为 91 条 | 本文第 3 节以 `git ls-files` 实测为准修正（`skipaudit` 实测 90 条，补入 `docker/nginx.conf` 等） |

### 5.2 需协调者 / 维护者裁定的条目

| # | 冲突 / 未决点 | 现状说明 | 建议裁定 | 决策方 |
|---|---|---|---|---|
| **C-1** | **`GATE_NOPY=1` 两次连续通过 vs 裁定 C 的执行方式** | `HANDOFF.md`（L208）要求「两次连续 `GATE_NOPY=1` 门禁通过」；但那是旧 WP11（冻结全部测试）方案下的门禁开关，裁定 C 放弃了 WP1–WP8 转换、改为在切换提交里直接删除 Python 依赖测试。目前 `server/scripts/gate.sh` 尚无 `GATE_NOPY=1` 实现 | 明确 `GATE_NOPY=1` 在裁定 C 下的语义：在**预演分支**（按第 3 节删除清单删掉 `backend/` 与 Python 依赖测试，并将 `proddiff` 切到 `go`/`go`、`skipaudit` 清零）上连续两次跑通完整无 Python 门禁（`core race + Agent + fuzz + frontend + proddiff Go-vs-Go`），作为合入 `main` 的前置门禁 | 协调者<br>**已裁定（协调者 2026-11-23）**：按建议执行，预演分支上连续两次完整无 Python 门禁 = `GATE_NOPY` 验收。 |
| **C-2** | **`.github/workflows/release.yml` 预发布适配** | 当前 `release.yml` 硬编码 `prerelease: false` 并在正文写死 `docker pull thsrite/config-flow:latest`，打 `v3.0.0-rc.1` 时会误发正式 Release 并引导用户拉 `latest` | 切换/打 `v3.0.0-rc.1` 前修改 `release.yml`：标签含 `-` 时设 `prerelease: true`，且正文仅列具体版本号（`PUBLISH_LATEST=true` 正式版才附 `latest`） | 协调者 / 维护者<br>**已裁定（协调者 2026-11-23）**：发 rc 前按建议改 `release.yml`，归入发布准备。 |
| **C-3** | **O14 F-1（「Docker Agent 更新步骤」对话框默认镜像）是否仍延后** | `o14-switch-plan.md` F-1 记录该对话框仍默认 `thsrite/config-flow-agent:latest`，原裁定切换后改。但现在 Agent 1.9.3-go 已换成 s6-overlay，若公开用户看对话框拉 `latest` 在观察期内仍指向旧 Python 镜像 | 确认是否维持「切换后尽快改」；若维持，公开升级说明需明确提示已注册 Docker Agent 更新也应使用与面板同版本的镜像标签 | 维护者<br>**已裁定（维护者 2026-11-23）**：维持切换后尽快改；公开升级说明加提示「Docker Agent 用与面板同版本的镜像标签」。 |
| **C-4** | **`scripts/tests/*_lab.py`、`scripts/audit_oxidns_schema.py` 等根目录历史 Python 脚本** | 根目录 `scripts/` 下尚有约 30 个历史 Python 实验/校验脚本，不在 `backend/` 中，也未列入 WP0 附录 B 删除清单 | 建议在切换提交中**先不删**（避免扩大切换 diff，且部分是 PVE 实验参考），切换后再单开清理 PR 归档或删除 | 协调者<br>**已裁定（协调者 2026-11-23，依维护者「Python 全部去掉」）**：不采纳建议，`scripts/` 下历史 Python 脚本在切换提交里一并删除（先核实无 CI/文档引用）。 |
| **C-5** | **Go 首发具体版本号确认** | 仓库当前最新 tag 为 `v2.2.0`，按裁定 D3「新主版本号 + 预发布标签」，应为 `v3.0.0-rc.1` → `v3.0.0` | 请维护者最终确认 `v3.0.0-rc.1` / `v3.0.0` 及最后一个 Python 版本号 `v2.2.0` | 维护者<br>**已裁定（维护者 2026-11-23）**：`v3.0.0-rc.1` → `v3.0.0`；最后一个 Python 版本 `v2.2.0`。 |
