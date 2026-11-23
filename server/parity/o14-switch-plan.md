# O14：切换与发布计划

- 起草：fresh Opus，2026-11-22（会话日期 2026-11-21），分支 `go-audit-o14`，基于 `go` = `2f47d56bb`。
- 2026-11-23 同步修订（分支 `go-o14-checklist`，基于 `go@595972901`）：对齐 batch-20..44（面板与 Agent 双镜像无 Python：面板 `tini` + Go 自管 Sub-Store + `cmd/configflow-autoupdate`，Agent `1.9.3-go` + s6-overlay）、维护者裁定 D1–D14（含 D5 保持自动同步开启）、测试去 Python 化裁定 C 与 O17 `runtime-state`（v1→v2 迁移前强制备份与恢复标记 fail closed）。完整前置条件与切换提交删除清单统一收口在 [O14-CHECKLIST.md](O14-CHECKLIST.md)。
- 只写文档，没有运行任何测试，也没有连接生产或实验网。
- 依据：[O14-CHECKLIST](O14-CHECKLIST.md)、[progress/o14-switch.md](progress/o14-switch.md)、
  [o13-completeness](o13-completeness.md)、[o12-release-notes](o12-release-notes.md)（下文用 O12-Axx/Bxx/Mxx 引用其条目）、
  [lab/L1](lab/L1.md)、[lab/L3](lab/L3.md)、WORKFLOW「Switch checklist」「VM lab tests」「Production snapshot refresh」。
- O12 引用：最终对齐的是 `go-audit-o12`@17e877158 的终稿草案。它的第 1 节 U01–U15、2.1 节 P01–P08、第 3 节 D01–D08
  是表格编号，附录 A 保留 A01–A20 的详细做法；本文写「O12-Axx」时指附录 A，并在关键处括注 U/P 编号。O12 第 4 节是本文的
  摘要版，两者冲突时以本文为准，并回写 O12。
- 文中只用角色名：**生产面板主机**（运行 ConfigFlow 容器的 NAS）、**主路由**（RouterOS）、**DNS HA 对**（两台 OxiDNS +
  keepalived VIP）、**旁路网关**（Mihomo Agent）、**PVE 实验宿主**。不写真实地址、主机名或凭据。

**基本原则（维护者 2026-11-16 决定）：Python 已永久退休，切换后不回滚到 Python。** 停损 = 停止发布、让设备继续运行
已部署的配置、必要时把切换前备份的数据恢复到 **Go**、逐项修复后再发布。O12-A20 的 null 清理只作为极端情况的参考，
不是本计划的一部分。

---

## A）维护者自己的生产切换

### A.0 前置条件（全部满足才定切换窗口）

> 2026-11-23 更新：完整前置条件总表（含 O18、O17、无 Python 面板/Agent 镜像、裁定 C 门禁预演、CI 切换、负责人与精确判定命令）已统一收口到 [O14-CHECKLIST.md 第 1 节](O14-CHECKLIST.md#1切换前置条件总表全部满足方可定切换窗口)。下表保留原 P1–P13 编号并同步最新状态。

| # | 条件 | 当前状态（2026-11-23） | 判定方式 |
|---|---|---|---|
| P1 | O13 完整性终审：0 缺口（#40 除外） | **已满足**（`65d03d17c`） | — |
| P2 | **#40 面板与 Agent 发布镜像（双镜像无 Python）**：面板用 `tini` 作 PID 1、Go 自管 Sub-Store、`cmd/configflow-autoupdate` 取代 `auto_update.py`（已移除 supervisord 与 nginx）；Agent 镜像（`1.9.3-go`）用 s6-overlay v3.2.3.2 取代 supervisord、`service_manager` 新写 `"s6"` 且 `"supervisor"` 读为同义词；`ca-certificates`、`tzdata`、`DATA_DIR=/data`、SIGTERM 排空、版本号注入 | **已满足**（batch-20/35/36/37 已合并） | `panel-image-smoke.sh`、`autoupdate-image-smoke.sh`、`nopy-agent-image-lab.sh` 全通过，见 A.2 影子启动 |
| P3 | O9 第二轮安全复核（全树）：无未关闭的 high/medium | **已满足**（batch-20 已合并） | `review-o9-round2.md` |
| P4 | O10 性能/内存：正式测量机（PVE 实验宿主独立性能 CT）同机同快照 Go ≤ Python（空闲 CPU 平均/10s 中位/p10、RSS、全部测量路由延迟），无 goroutine/fd 泄漏 | **进行中**（R1–R13 已合并；缓存预算 `go-perf-cache-budget` 进行中；O17 全合入后重跑最终 PVE 验收） | `o10-pve-baseline.md` + 最终 PVE 复测 + L6 24 h soak |
| P5 | O12 台账定稿：M01–M19 维护者逐条确认；M04/M11/M15/M16/M18 切换前关闭 | **基本满足**（M01–M19 已裁定，M15/M16/M18 已合入；O17 合入后同步终稿） | `o12-release-notes.md` |
| P5b | M16：Mihomo alpha 内核（实验网实测 `alpha-100ff73`）上报值按未知处理，不拒绝生成 | **已满足**（batch-22 已合并） | L2 实测 + 回归测试 |
| P6 | O15 `ARCHITECTURE.md`、O18 遗留清理、O17 地道 Go 清理（维护者 2026-11-23 裁定 O17 结构项全部在切换前完成） | O15/O18 **已满足**；O17 **进行中**（1.6 已合并；`runtime-state`、OxiDNS/ROS 类型化、小清理进行中；死代码待做） | 见 [O14-CHECKLIST.md](O14-CHECKLIST.md) P6/P6b/P6c |
| P7 | 实验网 L1–L5 全部通过，缺陷已修 | **已满足**（L1–L5 已通过，缺陷修复 batch-24/25/31 已合并） | `lab/L1.md`–`L5.md` |
| P8 | **L6**：最终候选代码上的 24 h soak + 本文 C 节切换演练（含 runtime-state v1→v2 迁移、s6 双宿 HA、HA 自动补完）通过 | **进行中**（准备分支 `go-l6-prep`，待 O17 合入后执行） | `lab/L6.md` |
| P9 | 切换前一周内刷新生产快照，在最终候选树跑完最后一次完整 Python 对比门禁（Python vs Go proddiff `new=0`）；裁定 C 删除清单、WP9 `proddiff` Go-vs-Go、两次无 Python 门禁预演（`GATE_NOPY=1`）、`skipaudit` 清零就绪 | WP9 **已满足**；最后一次 Python 对比门禁与无 Python 预演 **未满足** | 见 [O14-CHECKLIST.md](O14-CHECKLIST.md) P9–P9d |
| P10 | 「最后一个 Python 版本」标签确认与长期保留（裁定 D2：以合入 `main` 前最后一次 Python 发布 `v2.2.0` 为准） | **已满足（发版前复核）** | git tag + 镜像标签存在 |
| P11 | 小修复：缺失 Agent 的 logs/logs-sources/config-logging 由 500 改 404 | **已满足**（batch-20 已合并） | 已合并进 `go` |
| P12 | 切换用的镜像 = 公开发布候选镜像（同一 digest），版本号不是 `dev`，观察期内 `PUBLISH_LATEST != true` | **未满足（发版时核验）** | 启动横幅版本号 |
| P13 | **配套 Docker Agent 镜像（≥ `1.9.3-go`）先于或同时于 Go 面板镜像发布**：同一 `v*` 标签下 `docker-agent.yml` 已推出 `thsrite/config-flow-agent:<版本>`（amd64 + arm64），且 Go 面板镜像构建注入同一标签 | CI 已就绪（发版打 tag 时核验） | `docker buildx imagetools inspect thsrite/config-flow-agent:<版本>` 有两种架构；面板生成命令为 `:<版本>` |

### A.1 切换前一天：检查与准备（不停服务）

在 Python 面板上做，只读或只改配置：

1. **设备操作排空清单**：确认没有进行中的 Agent 发布/激活/回滚、gateway onboarding 会话、路由器发布、固定 IP 预览、
   DNS HA 配对任务、设置向导。未完成的先完成或取消（O12-A02、A07、N8-11 补充）。
2. **OxiDNS ready 但未激活的候选**：记下来，切换后需重新推送（O12-A05）。最好在 Python 下先激活或丢弃。
3. **逐项核对 O12 A 类前置项**：
   - A13：部署配置里已有 ≥32 字节随机 `JWT_SECRET_KEY`（生产已开登录；没有就现在生成并写入 compose，但先不重启）。
   - A17：WebDAV 地址/路径没有 `.`/`..` 段。
   - A18：容器环境代理变量没有依赖 Python 特殊写法。
   - A19：OxiDNS 动态设置 `custom_matches[].enabled` 为 bool，`ros_route.args.*` 为文本。
   - A16：主路由上没有未登记归属的 `cf-dnsguard:hijack` NAT 注释。
4. **记录切换前基线**（保存在维护者本机，不入库）：
   - `GET /api/agents` 的 JSON（每台 Agent 的状态、版本、content sync、当前部署 ID）。
   - 每台路由器的规则同步状态（`GET /api/routers/<id>/rule-sync`）和自动同步开关。
   - DNS 跟随绑定、IPv6 验收结论、已忽略通知列表（截图即可）。
   - 每台 Agent 设备上当前核心配置文件的 sha256（从设备上读，用于对照切换后是否真的换了配置）。
5. **自动任务（已裁定 D5）**：维护者决定切换首启**保持 Agent 与路由器的自动同步开启**（不在切换前手动关闭）。因此切换前必须通过影子启动（A.2）与 L6 演练确认生成结果与当前设备一致；首启后立即观察第一轮自动同步行为，出现异常按 A.5 停损。
6. **WebDAV 保留策略**：Go 首次写库后 1 s 会触发自动备份，并按 keep_latest/keep_daily 轮转。先把最近一份 Python 时期的
   WebDAV 备份复制到轮转目录之外（或临时调大 keep_latest），避免被 Go 的新备份挤掉。
7. 在测试服务器上用发布镜像对**刚刷新的快照副本**做一次影子启动（A.2），把首启日志作为对照。

### A.2 影子启动（测试服务器，无外网）

目的：在生产数据副本上提前看到首启行为，不碰生产。

- 用 proddiff 的网络隔离方式（netns 只有 lo，DNS 失败、TEST-NET 不可达）启动发布镜像，`DATA_DIR` 指向快照副本。
- 记录：启动耗时、启动横幅版本、WARNING/ERROR 日志逐条归类、首页/Agent 页/路由器页/规则库/订阅页是否 200、
  空闲 RSS、`configflow.sqlite3` 是否被写（预期：只有正常的元数据写入，没有 schema 变化）。
- 对每个 Agent 调 `GET /api/agents/<id>/push-diff`：预期每台都显示 `changed`（O14-CHECKLIST A14），差异只来自 YAML 排版、
  D2 provider 路径和资源标签；逐台人工抽查语义无变化（规则顺序、节点、DNS 设置）。
- 不通过则不定切换窗口。

### A.3 切换步骤（维护窗口，建议 60 分钟，低峰时段）

> 顺序原则：先面板，后设备；设备中先单台、后批量；DNS HA 对最后、按配对流程；路由器在 Agent 之后。
> 每一步失败都按 A.5 对应场景处理，不跳步。

**第 1 步：停 Python，冷备份（约 5 分钟）**
1. 再确认一次 A.1-1 排空清单。
2. `docker compose stop` 停 Python 容器（留 10 s 停止预算，不要 kill）。
3. 冷备份整个 `DATA_DIR`（tar，保留属主和权限；包括 `configflow.sqlite3-wal`/`-shm`、`runtime.sqlite3`、traffic/metrics 目录、
   规则缓存目录、Sub-Store 数据），同时保存一份 Python 最后的 `/api/config/export`（停机前导出），另存一份 compose 文件和 `docker inspect` 输出（含密钥，私下保存）。
   记录 Python 镜像 digest（只用于以后查看旧数据，不用于回滚）。
4. 在备份副本上跑 `PRAGMA integrity_check`，结果必须是 `ok`。
5. 主路由：导出一次配置（`/export` 文本 + 二进制备份）。iKuai 如有：导出备份。DNS HA 两台：各自复制当前 OxiDNS 与 keepalived 配置。

**第 2 步：换镜像，首次启动（约 5 分钟）**
0. 先确认 P13：`thsrite/config-flow-agent:<本次版本>` 已发布（两种架构）。不存在就不换面板镜像，等 `docker-agent.yml` 完成或手动重跑它。
1. compose 里换成 Go 镜像（固定 digest，不用 `latest`），确认 `JWT_SECRET_KEY`、`ADMIN_*`、`DATA_DIR`、端口和代理变量不变；
   删除 Go 不再读取的 Python 调优变量（`CONFIGFLOW_HTTP_THREADS`、`CONFIGFLOW_MEMORY_TUNING`、`MALLOC_*`，O12-D03）；
   `stop_grace_period` 不小于 10 s。
2. `docker compose up -d`。同一 `DATA_DIR` 只能有一个进程（Go 持实例锁，忙时 30 s 后退出 1）。

**第 3 步：首次启动检查清单（约 10 分钟）**

| # | 检查 | 预期 |
|---|---|---|
| S1 | 容器日志启动横幅 | 版本号 = 发布版本（不是 `dev`）；监听端口正确；登录已开启；**没有**「临时随机签名密钥」WARNING（有 → F4） |
| S2 | 没有「数据目录中有旧版配置文件…」拒绝 | 有 → A.5-F1 |
| S3 | WARNING/ERROR 日志 | 与 A.2 影子启动的分类一致，没有新类别 |
| S4 | `/health`、首页、登录 | 200；登录后会话正常 |
| S5 | 各页面能打开：概览、Agent、路由器、规则库、订阅、设置、通知、定时任务 | 无 500、无空白 |
| S6 | Sub-Store | 订阅页能列出订阅；需要时能由 Go 服务按需启动（日志来源 `sub-store`） |
| S7 | Agent 心跳 | 5 分钟内所有在线 Agent 重新上报，状态与基线一致（离线的保持离线） |
| S8 | 设备写入与自动同步（D5） | 观察第一轮自动同步结果：按 D5 保持开启，确认没有意外错误写入；若有异常立即按 F12 停止发布 |
| S9 | 资源 | RSS、CPU 与 A.2 / O10 同量级 |
| S10 | 设置导出 `/api/config/export` 与 v1→v2 迁移 | 能导出；确认 `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3` 迁移前强制备份已生成（O17 `runtime-state`）；新格式导出不再含 Agent 运行状态（仅配置部分与切换前一致） |

**第 4 步：规则缓存预热（约 5 分钟，O12-A03）**
1. 保留旧缓存文件，不手动改名或删除。
2. 通过规则库的缓存刷新入口（`POST /api/rule-library/cache`）以及各 profile 的正常生成预览，让 Go 校验并迁移旧缓存。
3. 抽查：订阅链接（`/api/config/<profile>/mihomo` 等）生成成功，规则集数量与基线一致；日志里没有「规则来源不可用」类错误。
4. 主路由上游离线时也能生成：这条在 L6 里验证过即可，生产只看结果。

**第 5 步：Agent 全量推送（逐台，O14-CHECKLIST A14 / O12-A04）**

顺序：先一台不承担 DNS 的旁路网关作试点 → 其余 Mihomo/MosDNS 单机 Agent → 最后 DNS HA 对。

对每台：
1. 看 `push-diff`，确认只有预期差异。
2. 推送整包（`POST /api/agents/<id>/push-config`）。
3. 逐项确认：整包已接收 → 核心加载成功（Agent 报告的部署状态 succeeded，不是只「已上传」）→ 部署记录状态成功 →
   content sync = `synced`。
4. 设备侧：核心进程在跑、配置文件 sha256 已变化、代理/DNS 实测可用（试点机多测几个站点）。
5. 不要用改 fingerprint、清状态的办法去掉 `changed`；proddiff 的测试副本格式迁移不能用在生产。
6. 试点机通过后观察 10 分钟，再继续其他机器。

**DNS HA 对**：按 HA 配对发布流程（备机先、确认健康 → 主机让位 → 主机更新 → 撤销让位），全程在一台客户端上
连续 `dig` VIP 记录丢包。规则热更新应为 reload_only，不重启核心。任一步失败按 A.5-F6。

**OxiDNS 旧 ready 候选**（O12-A05）：重新推送、校验后再激活。

**第 6 步：DNS 跟随绑定重绑（O12-A08，设计裁定 D-A17-1 → B）**
在 DNS 跟随设置里重新选择每个绑定、核对来源与目标后保存。之后改一次 Mihomo 监听（或手动触发一次跟随）确认能写入；
不重绑的绑定会因旧签名不匹配阻断跟随写入。

**第 7 步：路由器发布检查（O12-A09、A16、A06）**
1. 每台路由器：看规则同步状态。因旧 controls 不匹配而 paused 的，手动「预览发布」→ 核对差异（预期：资源标签/修订哈希变化，
   规则内容不变）→ 发布，建立新基准。
2. 强制 DNS 检查：若提示未登记的 `cf-dnsguard:hijack`，按 A16 人工处理，不按注释批量删。
3. IPv6：重新检查 readiness，需要时 prepare，再做一次验收（A06）。
4. 确认自动同步（Agent、路由器）处于开启状态，观察一个自动同步周期（路由器 30 s、Agent 变化驱动/兜底周期），确认空闲时不再产生意外写入。

**第 8 步：其他一次性事项**
- 固定 IP：重新载入来源设备并预览（A07）。
- 已忽略通知重新出现：检查后再忽略一次（A10），不因此重新推送。
- 浏览器里切换前打开的编辑页全部刷新（A11）。
- 手动触发一次 WebDAV 备份，确认成功；确认 A.1-6 保存的 Python 时期备份还在。
- 前端：删除未使用的 `generateDockerCompose`/`generateDockerRun`（WORKFLOW Switch checklist；应在发布树里已做，这里只核对）。

### A.4 验收清单（切换当天 + 观察 7 天）

切换当天，全部打勾才算完成：

- [ ] 首启检查 S1–S10 全部通过。
- [ ] 所有在线 Agent：部署 succeeded、content sync `synced`、设备侧配置已换、业务实测正常。
- [ ] DNS HA：VIP 由主机持有，两台最终配置摘要一致，切换期间 `dig` 丢包在 L6 演练的范围内。
- [ ] 主路由与其他路由器：规则同步状态正常（无 paused）、强制 DNS 一致、IPv6 验收通过（如使用）。
- [ ] DNS 跟随：全部绑定已重绑，并验证过一次跟随写入。
- [ ] 自动任务已恢复，空闲时没有意外写入。
- [ ] WebDAV 备份成功；冷备份已离机保存。
- [ ] 日志没有持续的 ERROR。

观察期（7 天，再决定公开发布）：

- [ ] 每天看一次：RSS/CPU（目标低于 Python 基线）、goroutine 数平稳、错误日志、Agent 在线数、规则同步无 paused。
- [ ] 订阅定时刷新、规则缓存刷新、domain discovery、OxiDNS 排行历史、metrics 落盘（30 min）都至少跑过一个周期。
- [ ] 至少一次真实的规则变更 → 自动同步到 Agent 和路由器。
- [ ] 至少一次 Agent 版本升级或重启后重新注册（已有 token）。
- [ ] 面板容器重启一次（SIGTERM），确认排空正常、重启后状态不丢。

### A.5 停损方案（不回滚 Python）

总原则：

1. **设备侧不依赖面板运行。** Agent、路由器、DNS 主机都继续运行已部署的配置；面板出问题时先**停止发布**
   （关掉自动同步、不再点推送），而不是急着换回旧面板。
2. **数据只恢复到 Go。** 需要恢复数据时，把 A.3 第 1 步的冷备份恢复到 `DATA_DIR`，仍用 Go 镜像启动
   （Go 能读 Python 写的数据，这是 proddiff 和 O13 已验证的）。恢复后切换后手动做过的改动要重做一遍。
3. **逐项修复。** 每个问题在 go 分支修复 → 正常批次门禁 → 出新镜像 → 换镜像，不在生产容器里打补丁。
4. 每次停损都记录到 `lab/L6.md` 的「生产切换记录」小节（或新的切换报告），作为公开发布前的问题清单。

| # | 故障场景 | 现象 | 处置 |
|---|---|---|---|
| F1 | 首启拒绝：旧版数据目录 | 日志「数据目录中有旧版配置文件…」，容器退出 | 配置数据未改动（只新建 `app.log`）。说明 `configflow.sqlite3` 不在预期位置（挂载路径错）或 Python 从未完成迁移。核对挂载；若确实缺库，用最后一个 Python 版本完成迁移后再换 Go（这是数据迁移，不是回滚运行） |
| F2 | 首启失败：实例锁忙 / 端口占用 / DB 非普通文件 | 退出码 1，日志写明原因 | 确认旧容器已停、只有一个进程；修正挂载或端口后重启 |
| F3 | 能启动，但某些页面 500 或数据显示不对 | 页面报错，日志有 ERROR | 停止发布；导出日志；判断是否有错误写入（对比冷备份导出）。没有写坏 → 保持运行，修 Go 后换镜像。写坏了 → F9 |
| F4 | 登录失效、会话频繁掉线 | 重启后要重新登录 | 检查 `JWT_SECRET_KEY` 是否 ≥32 字节、是否被识别为占位值（O12-A13）；修正后重启 |
| F5 | 单台 Agent 推送后核心加载失败 | 部署失败，Agent 自动回滚到上一部署 | 设备继续运行旧配置；暂停该 Agent 后续推送；从 `push-diff` 和 Agent 日志取差异；若为 Go 生成问题，暂停**所有**同类 Agent 推送，修 Go 后重推。不手动改设备配置 |
| F6 | DNS HA 推送中断或 VIP 异常 | `dig` 丢包、VIP 双持或无人持有 | 立即停止 HA 发布；若某成员永久离线导致 journal 卡在 unknown（O12-P08 / M07：离线 10 分钟强制停用**尚未实现**），按停用提示强制清除记录，再到该设备上手动删除 keepalived 里的 ConfigFlow 配置；确认至少一台持有 VIP 并能解析（必要时在该成员上手动启动 keepalived 恢复 VIP）；主路由上 DNS 兜底按现有 netwatch 策略生效。按 L3 第 6/12 项的恢复路径处理：失败方回滚旧 spec，未更新主机保留。修复后按配对流程重新发布 |
| F7 | 路由器发布结果不对 | 规则/NAT/路由与预期不符 | 关闭该路由器自动同步；用 Go 的预览查看差异；Go 有补偿/回滚的先用它，否则用第 1 步导出的路由器配置手动恢复受影响的 ConfigFlow 管理项（只恢复带 ConfigFlow 注释/归属的条目） |
| F8 | content sync 长时间不是 `synced` | Agent 页显示待同步 | 先看 Agent 是否在线、规则缓存是否预热（A.3 第 4 步）；重推一次；仍不行则记录并暂停该 Agent 的规则自动同步，设备继续运行已部署配置 |
| F9 | Go 写坏了数据 / v1→v2 迁移异常 | 导出与预期不符、配置丢失，或迁移前备份失败拒绝启动 | 停容器 → 复制当前 `DATA_DIR` 留证 → 恢复 A.3 冷备份（或仅回退数据库时恢复 `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3`）→ 用 Go 启动 → 重做切换后的手动操作 → 在测试服务器上用留证数据复现并修复。若使用**不含状态的新格式 JSON/WebDAV 备份**恢复，每台 Agent 会进入 `restored` fail closed 锁定，逐台成功发布（HA 走成对发布）后自动解除（见 [O14-CHECKLIST.md §4.3](O14-CHECKLIST.md)） |
| F10 | 内存/CPU 异常 | RSS 持续增长、CPU 高 | 采 pprof（heap/goroutine），需要时设 `GOMEMLIMIT` 临时限制并重启；作为 O10 回归处理 |
| F11 | Sub-Store 起不来 | 订阅页报错 | 订阅生成使用已有缓存；检查 `app.log` / 系统日志中来源为 `sub-store` 的日志（由 Go 直接托管，已无 supervisord）；属于镜像或托管问题，修 Go 后换镜像 |
| F12 | 自动同步在首启后意外写设备 | 首启后、手动推送前出现部署/apply 记录 | 立即关闭对应自动同步；对照 A.1-4 基线判断写入是否正确；不正确按 F5/F7 处理 |
| F13 | WebDAV 自动备份失败 | 备份任务报错 | 不影响运行；按 A17 修正路径；本地冷备份已在，修好后手动备份一次 |
| F14 | Agent 离线或无法重新注册 | 心跳中断 | 已有 Agent 用自身 token，不应受影响；检查面板地址/端口是否变化、登录 allowlist；新装 Agent 必须用 Go 面板生成的命令（A14/A15） |
| F15 | 某个操作因损坏/手工改坏的存储记录返回 400/409/500（fail closed） | 只影响该 Agent/设备/日期，其他照常 | 按 O12-P07 列出的现有恢复入口处理（重置统计、`POST /api/agents/<id>/domain-discovery/clear`、流量历史清空、停用 HA、修正错型 profile 绑定等）；不要为此恢复整份备份 |
| F16 | 修复版本本身有问题 | 新镜像引入回归 | 可退到**上一个 Go 版本**镜像，前提是两个版本之间没有存储格式变更（每次发布说明写明）；否则继续修复前进 |

**不做的事**：不启动 Python 容器操作同一 `DATA_DIR`；不在两个后端之间来回切；不为了消掉 `changed` 改数据库指纹。
如果维护者在极端情况下仍要求临时运行 Python 查看旧数据，只能在冷备份的**副本**上、隔离网络里只读查看。

---

## B）面向其他用户的公开发布

### B.1 旧数据目录与升级路径

- Go 启动时若 `DATA_DIR` 里有 `config.json`、`config.json.bak`、`system.json`、`system.json.bak` 或 `*/config.json(.bak)`，
  但没有 `configflow.sqlite3`，会拒绝启动并提示：
  「数据目录中有旧版配置文件（…），但没有 configflow.sqlite3。此版本不再迁移 SQLite 之前的旧格式：请先升级到最后一个
  Python 版本的 ConfigFlow 并启动一次完成迁移，再升级到此版本。配置数据未改动。」（`internal/repo/messages.go` `msgLegacyOnlyDataDir`）
  拒绝时只会在目录里新建日志 `app.log`（`DATA_DIR/app.log`），配置文件和数据不动；#40 复核前提示写的是「数据目录未改动」，已改。
- 已有 `configflow.sqlite3` 的用户直接升级即可；Go 不再补做 Python 的一次性清理（managed DNS / node-test DNS 等，O12-A01）。
- **升级指引**（写进 release notes 和 `doc/guide/install.md`）：
  1. 备份整个数据目录。
  2. 版本早于「最后一个 Python 版本」的，先换成该版本的镜像（固定标签，例如 `thsrite/config-flow:<最后 Python 版本>`），
     启动一次，确认页面正常、数据目录里出现 `configflow.sqlite3`。
  3. 等设备操作结束，停容器，换成 Go 版本标签，启动。
  4. 按 O12 公开升级说明做一次性操作（JWT 密钥、规则缓存预热、逐台全量推送、DNS 跟随重绑、路由器建立新基准等）。
- 「最后一个 Python 版本」需要在 Go 合入 main 前单独打标签并发布镜像，且长期保留该标签（D2）。

### B.2 镜像：标签、架构、时区、CA

| 项 | Python 时期 | Go 版本计划 | 备注 |
|---|---|---|---|
| 仓库 | `thsrite/config-flow` | 不变 | |
| 标签 | `latest`、语义化版本、`autoupdate` | 语义化版本（建议主版本 +1，见 D3）、`latest`、`autoupdate` | 建议 Go 首发先只推版本号标签 + 一个预发布标签，观察期后再移动 `latest`（D3） |
| 架构 | `linux/amd64`、`linux/arm64`（docker.yml） | 同上；Go 静态编译（CGO_ENABLED=0）便于加 `linux/arm/v7` | 是否加 armv7 由维护者决定（D4）；Sub-Store（Node）在 armv7 的可用性需先确认 |
| 基础镜像 | python:3.11-slim + nginx + supervisord | `alpine:3.24` + `tini`（PID 1）+ Go 单二进制 + Node.js（供内置 Sub-Store）；已彻底移除 Python、nginx、supervisord | batch-36 已落地 |
| 端口 | 容器内 80（nginx） | 不变（Go 内置 Edge 取代 nginx）；`CONFIGFLOW_WEB_PORT`/`BACKEND_*` 保持含义 | Host 网络模式用户核对端口 |
| 时区 | `TZ=Asia/Shanghai`，镜像有 tzdata | 保持默认 `TZ=Asia/Shanghai`；镜像必须带 tzdata（或 Go 编译时 `-tags timetzdata`），否则 `TZ` 无效、定时任务和日志时间变成 UTC | 影子启动检查日志时间 |
| CA | Python requests 用 certifi，部分读 `REQUESTS_CA_BUNDLE` | Go 用系统 CA（`ca-certificates`）；私有 CA 用 `SSL_CERT_FILE`/`SSL_CERT_DIR` 或挂载到系统证书目录 | **`REQUESTS_CA_BUNDLE` 不再生效**，release notes 必须写明（O12-M04） |
| 内存调优 | `MALLOC_ARENA_MAX` 等 | 不再读取；改为 `GOMEMLIMIT`、`CONFIGFLOW_MEMORY_LIMIT_RATIO` | 写入 release notes |
| 关停 | supervisord 停 Flask | `tini -g` 转发 SIGTERM，Go 排空默认 8 s（`CONFIGFLOW_SHUTDOWN_GRACE_SECONDS`）并在排空后停止 Sub-Store，compose 的 `stop_grace_period` 不能小于 10 s | |
| 版本号 | `VERSION` build-arg | 通过 ldflags 注入 `internal/version.Version`；`dev` 不得出现在发布镜像（O12-M11，A11 RulesFingerprint 依赖它） | |
| Agent 制品 | 镜像内 `/opt/configflow/static/agents` | 同一路径；必须是本次源码重建的 Agent 二进制，Agent 镜像同时发布（O12-A14） | docker-agent.yml 在同一 `v*` 标签上推 `config-flow-agent:<版本>` |
| Docker Agent 镜像标签 | 面板命令写死 `config-flow-agent:latest`；Agent 镜像只推 `latest`（main 推送、定时、手动） | `v*` 标签（含预发布）推 `config-flow-agent:<semver>`，不动 `latest`；`latest` 仍按原规则随 main 移动。Go 面板镜像构建时注入同一标签（`version.AgentImage`），生成的 Docker 命令/Compose/HA 用它；分支/本地/autoupdate 重建仍用 `latest` | **发布顺序：Agent 镜像必须先于或同时于面板镜像可拉取**（P13）。新 Agent 镜像配旧 Python 面板向后兼容，main 上 Agent `latest` 先变成新入口不影响 Python 用户 |

### B.2.1 发布步骤中的 Agent 镜像（L2 发现后补充）

1. 打 `v<版本>` 标签后，`docker.yml`（面板）与 `docker-agent.yml`（Agent）同时触发。**先等 `docker-agent.yml` 的 merge 任务成功**，
   用 `docker buildx imagetools inspect thsrite/config-flow-agent:<版本>` 确认 amd64 + arm64 都在，再公布面板版本。
   面板镜像先完成也不会出错，但在 Agent 标签可拉取之前，新装 Docker Agent 会因镜像不存在而失败。
2. Agent 任务失败（例如上游镜像拉取失败）时：重跑 `docker-agent.yml`（在该标签上 `workflow_dispatch` 或重新运行失败的任务），不要改面板命令去用 `latest`。
3. 公开说明写明：新装 Docker Agent 时请用面板生成的命令，镜像标签会自动与面板版本一致；手写 Compose 的用户把镜像改成同一版本号。

### B.3 autoupdate 镜像（维护者已决定保留）

> 来源冲突：O12 终稿草案 M14 / D06 仍**建议下线** autoupdate；但 HANDOFF 2026-11-22 记录的维护者决定是「保留，并让它也重建 Go server」。
> 本文按维护者决定写，O12 的 M14/D06 需要改成「保留」（见 D12）。

- Go 版 `autoupdate` 已由独立的 Go 程序 `cmd/configflow-autoupdate` 取代原 `docker/auto_update.py`（batch-36），容器内不含 Python，源码变化时按指纹只重建 Go server、前端、Agent 与 `node-probe`，工具链（Go、npm）保留在 autoupdate 层。
- **风险**：autoupdate 用户跟随分支。Go 合入 `main` 的那一刻，仍在旧 Python 版本、甚至仍是 legacy 目录的 autoupdate 用户会
  在下一次重启时直接跳到 Go，跳过「先用最后一个 Python 版本」。建议（D6）：
  1. Go 合入前，最后一个 Python 版本的 autoupdate 构建已经发布足够久（例如 2 周），让 autoupdate 用户先完成迁移；
  2. Go 版 auto_update 在切换代码前先检查数据目录：遇到 legacy-only 目录时**不切换**，继续运行当前镜像内的程序，
     并在日志里打印 B.1 的升级提示；
  3. release notes 单独一节说明 autoupdate 用户的行为。
- 构建失败时的行为保持现状：继续运行镜像里已有的版本，不留下半成品。

#### B.3.1 已在用 autoupdate 的用户（维护者 2026-11-22 采纳 #40 复核 M3）

旧 Python autoupdate 容器运行的是**镜像里固化的旧 `auto_update.py`**，上面第 2 条的 legacy 保护只在 Go 版脚本里，对它们不起作用。
旧脚本每次启动会安装 `main` 的 `backend/` Python 源码并重建前端；如果 Go 合入后 `backend/` 仍在，它们会继续跑冻结的 Python 后端
配上 Go 时期的前端，接口逐步失配。因此：

1. **Go 合入 `main` 的同一提交删除 `backend/` 的 Python 源码**（`backend/agents/go-agent`、`backend/node-probe` 这些 Go 组件保留）。
   旧脚本随后安装 `backend/` 或 `backend/requirements.txt` 时失败，按原逻辑记录 `update failed` 并退回它最后一次 Python 构建，
   不会出现半新半旧的组合，容器继续运行。
2. 这些用户需要**重新拉取 Go 版 autoupdate 镜像**才会进入 Go。CI（`docker.yml`）现在构建并发布 autoupdate（amd64 + arm64）：
   - 每个版本标签（含预发布）推 `<版本>-autoupdate`，例如 `3.0.0-rc.1-autoupdate`；
   - 浮动标签 `autoupdate`（旧用户正在跟的那个）与 `latest` 同一开关：`PUBLISH_LATEST=true` 且不是预发布才移动。
     观察期（D3）结束前它保持指向最后一次 Python 构建，旧用户不会在重启时被换成 Go；观察期后与 `latest` 一起移动。
3. 拉取 Go 版 autoupdate 镜像后，若数据目录仍是 legacy-only，Go 版脚本跳过更新、`-check` 拒绝启动并打印 B.1 提示；
   有 `/data/.auto-update/manifest.json`（Python 时期写的，没有 server 记录）时，Go 版脚本丢弃它并按新镜像重新构建，
   不会把镜像自带 server 与旧构建的前端/Agent 混用（#40 复核 M2）。
4. release notes 的 autoupdate 小节写明以上三点，以及：缓存仍在 `/data/.auto-update`（Go 工具链缓存约 1.5 GB）；
   自定义 `CONFIGFLOW_UPDATE_REPO` 等于在容器里执行该源码，必须是可信来源。

### B.4 升级注意事项（公开 release notes 的结构）

公开说明直接复用 [O12「面向公开用户的升级说明草稿」](o12-release-notes.md)，发布前补齐：

1. 版本号、最后一个 Python 版本号、升级窗口建议。
2. B.1 的升级路径和拒绝提示原文。
3. B.2 的镜像变化：nginx 移除（自定义 nginx 配置的用户需改用反向代理）、CA（`REQUESTS_CA_BUNDLE` 失效）、
   内存变量、停止宽限。另写两点（#40 复核 L1、L4）：
   - **`DATA_DIR` 必须指向挂载的数据卷**（默认 `/data`）。旧 Python 镜像的 supervisord 强制后端用 `/data`，容器里另设的
     `DATA_DIR` 实际被忽略；Go 镜像按它读写。设成别的路径会让首启看到空配置（原数据不受影响），改回 `/data` 即可。
   - 旧数据目录被拒绝启动时，目录里只会新建日志 `app.log`，配置数据不动；提示语为「…配置数据未改动。」
4. O12 A 类中适用于普通用户的条目（A01、A02、A03、A04、A05、A06、A07、A08、A09、A10、A11、A13、A14、A15、A16，
   以及按条件适用的 A12、A17、A18、A19）。A20 不写进公开说明（官方不支持退回 Python）。
5. M01–M19 中维护者确认后要公开的部分（例如 M02 CORS 同源、M04 CA、M07 HA 永久离线的人工处理、M12 MCP -32603 先查询再重试、
   M18 domain-discovery 损坏文件的清理入口）。
6. 「切换后每个 Agent 会显示一次 `changed`，这是预期的，请逐台推送」放在最显眼位置。
7. 「已忽略的通知会重新出现一次」「正在进行的设置向导需要重新开始」等提示。
8. 问题反馈渠道：附启动日志前请删除密码、token 和订阅链接。

---

## C）L6 切换演练脚本（实验网）

### C.0 范围与安全

- 只用 PVE 实验宿主上的隔离网桥 `cfgt`；所有 guest 在启动前登记到 owner 文件，结束后销毁或标为留用。不碰任何生产 guest。
- 使用生产快照副本时（WORKFLOW L6 已批准），**在任何设备可能被写入之前**：
  - 把每个路由器、Agent 的地址改指向实验 guest，或直接禁用；
  - 关闭 WebDAV 自动备份并清空 WebDAV 地址（否则会写到真实备份服务器）；
  - 禁用或改写全部订阅地址和定时刷新（否则会用真实 token 访问真实机场）；
  - 关闭通知外发（如有 webhook）。
  这些改动在 Python 面板上做（演练的起点是 Python），并记录做了哪些。
- 报告和截图里不出现快照中的真实地址、域名、token；只报告数量和结果。
- 本节为脚本草稿，执行前由协调者确认维护者的 go-ahead（D9）。

### C.1 实验拓扑

| guest | 角色 | 说明 |
|---|---|---|
| 面板 CT | 先跑最后一个 Python 版本镜像，再换 Go 发布镜像 | 与 L1 一样放在 cfgt 内，Docker 运行，挂载快照副本 |
| 旁路网关 ×2 | Mihomo Agent（systemd 一台、OpenRC 一台） | 一台作试点 |
| DNS HA 对 | 两台 OxiDNS + keepalived，VIP 在 cfgt 内 | 复用 L3 的搭建方法 |
| 实验 RouterOS CHR | 主路由角色 | 规则同步、强制 DNS、IPv6、DNS 跟随目标 |
| 实验 iKuai VM（可选） | 第二路由器 | 若快照里有 iKuai 设备 |
| 客户端 CT | 连续探测 | 跑 `dig` 循环和 `curl` 经代理循环 |

快照中多出来的设备一律禁用，只保留与上表一一对应的记录（改指向）。

### C.2 起点：用 Python 建立「切换前」状态

1. 面板 CT 用最后一个 Python 版本镜像 + 快照副本启动，完成 C.0 的改指向/禁用。
2. 在实验 guest 上安装 Agent（Python 面板生成的命令），对每台全量推送一次，让设备运行**Python 生成的配置**。
3. 建立 DNS HA 配对、DNS 跟随绑定、路由器规则同步基准、IPv6 验收、一个 OxiDNS ready 未激活候选、一条已忽略通知、
   一个未提交的固定 IP 预览、一个进行中的设置向导、WebDAV 地址指向实验内的 WebDAV（可选，用来测轮转）。
4. 记录 A.1-4 的全部基线。

### C.3 按 A.3 演练

严格按 A.3 第 1–8 步执行，每一步记录开始/结束时间和下表指标。再额外做：

- A.2 影子启动：在测试服务器上对同一快照副本先做一次，结果与实验面板首启对比。
- 首启时故意不关自动同步（第二轮演练），观察 F12：手动推送前是否有任何自动写入，写了什么。

### C.4 要观测的指标

| 类别 | 指标 | 采集方式 | 期望 |
|---|---|---|---|
| 面板 | 停机时长（Python 停止 → Go `/health` 200） | 时间戳 | ≤ 2 min |
| 面板 | 首启耗时、启动日志 WARNING/ERROR 数量与类别 | 容器日志 | 与影子启动一致 |
| 面板 | 首启后 10 min 内的 RSS、CPU、goroutine、fd | `docker stats`、`/proc`、pprof | 低于 Python 基线 |
| 数据 | `integrity_check`；切换前后导出结构差异 | sqlite、导出 JSON 比较 | ok；只有随机 ID/时间差异 |
| 自动任务 | 首启到手动推送之间的设备写入次数 | 部署记录、路由器 apply 记录 | 0（关自动同步时）；第二轮记录实际数字 |
| 规则缓存 | 预热前后可用规则集数量；旧缓存迁移数 | 生成结果、缓存目录 | 与基线一致；旧文件保留 |
| Agent | 每台 `changed` 是否出现、推送耗时、核心是否重启/仅重载、content sync 到 `synced` 的耗时 | Agent 页、设备日志 | 每台都 `changed` 一次；全部 synced |
| Agent | 推送期间代理连续性 | 客户端 `curl` 循环（每秒） | 只在核心重启的瞬间失败 |
| DNS HA | VIP 查询丢包数和最长中断 | 客户端 `dig` 循环（10 次/秒） | 在 L3 测得的范围内；规则热更新 0 丢包 |
| DNS HA | 两台最终配置摘要 | sha256 | 相同 |
| DNS 跟随 | 重绑前写入是否被阻断、重绑后跟随写入是否成功 | 面板 + 设备 | 阻断 → 成功 |
| 路由器 | paused 数量、预览差异条数、发布后规则条数 | 面板、RouterOS 导出比较 | 差异只在资源标签/哈希；规则数不变 |
| 路由器 | 强制 DNS、IPv6 验收结果 | 面板 | 与基线一致 |
| 一次性事项 | 通知重现数、固定 IP 预览 409、设置向导提示、OxiDNS 候选激活被拒 | 面板 | 都按 O12 描述出现 |
| 备份 | WebDAV 首次自动备份时间、轮转后旧备份是否保留 | WebDAV 目录 | 按 A.1-6 处理后旧备份仍在 |

### C.5 停损演练（A.5 抽样）

| 演练 | 做法 | 通过标准 |
|---|---|---|
| F5 | 给试点 Agent 推一个核心无法加载的配置（例如人为制造冲突端口） | Agent 自动回滚，设备继续服务，面板显示失败，不影响其他 Agent |
| F6 | DNS HA 发布中，在主机让位后 `kill -9` 面板容器，再启动 | 至少一台持有 VIP，`dig` 中断在可接受范围；重启后按同一任务继续或安全失败 |
| F9 | 推送完成后停容器，恢复冷备份到 `DATA_DIR`，Go 启动 | 启动正常；面板显示需要重新推送（因为恢复的是切换前的部署记录）；重推后收敛 |
| F12 | 第二轮不关自动同步 | 记录实际行为，决定 D5 |
| 重启 | 推送完成后 SIGTERM 重启面板 | 排空正常；状态不丢；不产生新的 `changed` |
| 24 h | 演练结束后保持运行 24 h（与 O10 soak 合并） | RSS/goroutine/fd 平稳，定时任务都跑过 |

### C.6 产出

- `server/parity/lab/L6.md`：拓扑、每一步耗时、C.4 指标表实测值、停损演练结果、缺陷 → 任务。
- 演练发现需要改 A 节步骤的，直接改本文件并在 L6.md 注明。
- 演练结束：guest 销毁或登记留用；快照副本删除。

---

## 切换后的后续事项

| # | 事项 | 来源 |
|---|---|---|
| F-1 | 「Docker Agent 更新步骤」对话框的目标镜像仍默认 `thsrite/config-flow-agent:latest`。切换后改为默认与面板配套的 `config-flow-agent:<版本>`（需要前端拿到面板注入的 `version.AgentImage`，涉及 `/api/version` 或新接口及其 Flask/proddiff 例外）。已注册 Agent 换镜像不依赖新入口，暂不阻塞切换 | 协调者裁定 2026-11-21（L2 发布阻断后续） |

## 需要维护者决定的事项

| # | 事项 | 建议 |
|---|---|---|
| D1 | 生产切换的时间窗口，以及 7 天观察期后再公开发布是否可以 | 低峰时段 60 min；观察 7 天再发布 |
| D2 | 「最后一个 Python 版本」的版本号与发布时间；该标签长期保留 | Go 合入 main 前单独打 tag 发布，至少提前 2 周 |
| D3 | Go 首发的版本号与标签策略：是否主版本 +1；`latest` 何时指向 Go；是否先用预发布标签 | 主版本 +1；先只推版本号 + 预发布标签，观察期结束再移动 `latest` |
| D4 | 是否增加 `linux/arm/v7` 镜像 | 先不加，除非 Sub-Store 在 armv7 上已确认可用 |
| D5 | 首启时是否先关掉 Agent 与路由器的自动同步，全量推送确认后再打开 | 关掉（L6 第二轮演练提供数据后最终确认） |
| D6 | autoupdate 用户跳过最后 Python 版本的保护：legacy-only 目录时不切换代码，并提前 2 周发布最后 Python 版本的 autoupdate 构建 | 同意两条都做（需要 #40 实现） |
| D7 | O12 M01–M19 每条的处理（接受 / 补 A 类操作 / 修复）；其中 M04（私有 CA、`REQUESTS_CA_BUNDLE` 失效）和 M11（发布版本号注入）直接影响镜像 | M04、M11 在 #40 里解决；其余按 O12 最终版逐条确认 |
| D8 | 默认时区继续用 `Asia/Shanghai` | 保持，避免现有用户定时任务时间变化 |
| D9 | L6 演练的 go-ahead：使用生产快照副本 + 实验 guest，按 C.0 改指向/禁用后执行 | WORKFLOW 已批准 L6；按 O14 表格的要求再确认一次 |
| D10 | 停损中「极端情况下临时运行 Python 查看旧数据」是否完全禁止 | 只允许在冷备份副本、隔离网络里只读查看 |
| D11 | DNS HA 对放在最后推送，且全量推送当天就做（不分多天） | 当天做完，避免面板长期处于部分 `changed` |
| D12 | O12 终稿草案 M14 建议下线 autoupdate，与 2026-11-22 记录的「保留并重建 Go server」冲突 | 以维护者决定为准（保留），请 O12 作者把 M14/D06 改为「保留」并引用 B.3 的 legacy 保护 |
| D13 | 是否接受 M07（HA 成员永久离线 10 分钟强制停用未实现）作为已知限制先切换 | 接受，生产切换不依赖它；F6 给出人工步骤；切换后尽快实现 |
| D14 | 是否为 M18 domain-discovery 损坏文件清理补一个 UI 入口 | 切换后小任务，不阻塞 |

## 维护者裁定（2026-11-22）

- D2：「最后一个 Python 版本」= Go 合入 main 前的最后一次 Python 发布（不提前单独打标签）；公开升级说明指向它，并长期保留该镜像标签。
- D3：Go 版用新的主版本号与预发布标签；观察期（约 7 天）结束前不改 `latest`。
- D4：面板镜像只发 amd64 + arm64，不加 armv7（Agent 二进制仍含 armv7）。
- D5：切换后**保持自动同步开启**（不在首启时暂停 Agent/路由器自动同步）。因此 A 节切换步骤要求：切换前用影子启动与 L6 演练确认生成结果与当前设备一致；首启后立即观察自动同步的第一轮结果，出现异常按停损 F 场景先停止发布。
- D6：autoupdate 遇只有旧版数据的目录不切换代码（#40 实现）。
- D7：O12 M01–M19 已逐条裁定（见 o12-release-notes.md 末尾）。
- D12：autoupdate 镜像保留并重建 Go server。
- D13：M07 作为已知限制先切换。
- D14：M18 损坏恢复入口切换前补上（进行中）。
- D1：切换窗口到时再定（L2–L6 全部通过后）。
- D8：默认时区保持 Asia/Shanghai。
- D9：同意 L6 使用生产快照副本（所有设备/备份/订阅先改指向实验机或禁用）。
- D10：同意，仅在冷备份副本、隔离环境用旧 Python 版只读查看，不是回滚。
- D11：同意，切换当天全量重推：试点机 → 其余 Agent → DNS HA 对最后按配对流程。
