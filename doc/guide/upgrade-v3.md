---
outline: 2
---

[文档首页](../README.md) · [安装管理面板](install.md) · [系统设置](../manual/system-settings.md)

# v3.0.0 升级说明（Python → Go）

`v3.0.0`（预发布 `v3.0.0-rc.1`）将 ConfigFlow 后端从 Python 重写为 Go，发行镜像彻底移除了 Python、Nginx 与 `supervisord`。已有 SQLite 数据继续使用，前端功能、Agent 日常功能和全部 API 均保留。

- **最后一个 Python 版本**：`v2.2.0`（镜像 `thsrite/config-flow:2.2.0`，长期保留）。
- **版本标签策略**：Go 版本使用具体版本号（如 `3.0.0-rc.1` / `3.0.0`、`3.0.0-autoupdate`）和跟随 3.x 正式版的主版本号标签 `3`（`3-autoupdate`；Agent 镜像为 `thsrite/config-flow-agent:3`）。面板与 Agent 的浮动标签 `latest`、`autoupdate` 暂时冻结在 Python 时期（面板 `v2.2.0`），Go 版本稳定一段时间后才会切换过去；在此之前跟随 `latest` 的部署（包括 Watchtower 等自动更新）不会被升级到 v3。升级到 Go 版本请把镜像改为 `3` 或具体版本号。

<a id="requirements"></a>
## 最低环境要求与架构支持

### 架构支持

| 组件 / 镜像 | 支持架构 | 说明 |
| --- | --- | --- |
| 面板运行镜像（`thsrite/config-flow:<版本>`） | `linux/amd64`、`linux/arm64` | 不提供 `linux/arm/v7` 面板镜像 |
| 面板自动更新镜像（`thsrite/config-flow:<版本>-autoupdate`） | `linux/amd64`、`linux/arm64` | 容器启动时按源码指纹增量重建 Go 服务、前端、Agent 与测速组件 |
| Docker Agent 镜像（`thsrite/config-flow-agent:<版本>`） | `linux/amd64`、`linux/arm64` | 与面板按同一版本号同步发布，自 `1.9.3-go` 起无 Python、改用 s6-overlay |
| Linux Shell Agent 二进制（内置于面板自动下发） | `linux/amd64`、`linux/arm64`、`linux/armv7` | 仍支持 32 位 ARMv7 路由器或单板机 |

### 环境与配置要求

- **容器运行环境**：Docker Engine（使用 Compose 部署建议 Docker Compose v2；OxiDNS Docker 高可用「精简 Compose」需要 Docker Compose ≥ 2.20）。
- **优雅停机宽限期**：建议设置 `stop_grace_period: 10s`（或 `docker stop -t 10`）。Go 服务默认在 8 秒内完成请求排空与监控历史落盘，不要用 `kill -9` 强杀。
- **数据目录与 `DATA_DIR`**：`DATA_DIR` 必须与挂载卷路径一致（默认 `/data`）。旧 Python 镜像曾忽略自定义 `DATA_DIR` 并固定读写 `/data`，Go 镜像会严格按 `DATA_DIR` 读写；若此前写过其他值导致首启显示空配置，改回 `/data` 即可，原数据不受影响。
- **单进程实例锁**：同一 `DATA_DIR` 只允许运行一个 ConfigFlow 服务进程；第二个进程等待 30 秒仍拿不到锁会退出。
- **正式签名密钥**：请配置随机、至少 32 字节的 `JWT_SECRET_KEY`（例如 `openssl rand -hex 32`）并长期保存。未设置、使用占位文字或不足 32 字节时，每次重启都会随机生成临时密钥，导致登录会话和尚未确认的固定 IP 预览失效（即使未开启登录也建议设置）。
- **已有 SQLite 数据库前提**：数据目录必须已包含 `configflow.sqlite3`。如果仍停留在早期只有 `config.json` / `system.json` 的旧版目录，Go 镜像会拒绝启动并输出中文提示（配置数据未改动，仅新建 `app.log`）；请先用 `thsrite/config-flow:2.2.0` 启动一次完成迁移，再升级到 v3.0.0。
- **配套 Agent 版本**：开启登录后新装 Agent 需 `1.9.0-go` 及以上；Docker 高可用只读检查与迁移脚本需 `1.9.2-go` 及以上；无 Python / s6-overlay Docker 镜像需 **`1.9.3-go`** 及以上（随 v3.0.0 面板内置与同步发布）。已注册的旧 Agent 凭自身 token 仍可继续工作，无需为此强制先升级。

<a id="storage-v2"></a>
## 存储格式 v2：单向升级与自动备份恢复

v3.0.0 将 Agent 的运行记录（部署历史、已应用内容、规则同步进度、DNS 联动与高可用状态、IPv6 校验记录等）从配置文档拆入同一 SQLite 数据库的独立表 `agent_state`，存储版本由 `v1` 升至 `v2`。

**升级到 v3.0.0 后，不能回退到 `v2.2.0` 及以前版本。** `v2.2.0`（Python 版）与较早的 Go 版本无法打开 `v2` 格式数据库。

### 升级前自动备份位置

首次打开 `v1` 格式的 `configflow.sqlite3` 时，Go 服务会在改库前先完成在线备份并通过 `PRAGMA integrity_check` 校验，备份保存在数据目录下：

```text
<DATA_DIR>/backups/configflow-v1-<UTC时间戳>.sqlite3
```

若备份失败（例如磁盘空间不足或 `backups` 目录不可写），面板会**拒绝启动**并记录中文原因，原数据库保持不变。

### 出现异常时的恢复方法

设备侧（Mihomo、OxiDNS、路由器）不依赖面板运行，遇到异常时先**停止发布或停掉面板容器**，再按以下方式恢复到 Go 版本：

1. 正常停止面板容器（`docker compose stop` 或 `docker stop config-flow`，等待排空，不要 `kill -9`）。
2. 复制一份当前异常的 `DATA_DIR` 另存留证，避免覆盖原备份。
3. 恢复数据：
   - **整目录恢复（推荐）**：清空 `DATA_DIR` 后，用升级前手动冷备份的整个数据目录还原。
   - **仅回退数据库到迁移前状态**：移除当前的 `configflow.sqlite3` 及可能存在的 `-wal`、`-shm` 文件，将 `<DATA_DIR>/backups/configflow-v1-<UTC时间戳>.sqlite3` 复制回 `<DATA_DIR>/configflow.sqlite3`（保留原属主与权限）。
4. 使用修复后的 Go 镜像重新启动容器。面板会重新执行原子 `v1 → v2` 迁移，之后按需复核或重新推送已变更的 Agent。

> 不要把已经升级为 `v2` 的 `DATA_DIR` 重新挂载给 `v2.2.0` 容器。

<a id="backup-restore"></a>
## 完整导出与 WebDAV 备份变化

从 v3.0.0 起，**「导出全部数据」与 WebDAV 备份只包含配置，不再包含 Agent 运行记录**（发布历史、已应用内容、规则同步/DNS 联动/DNS 高可用进度、IPv6 检查结果）。同时，这些运行状态的变化不再触发 WebDAV 自动备份（仅配置修改触发）。

使用不含运行记录的新格式备份导入恢复后，系统会按安全默认（fail closed）为每台导入的 Agent 加上恢复标记，防止在运行状态未知的情况下误操作设备：

- Agent 页顶部显示横幅：**「运行记录未包含在备份里，各 Agent 需要重新发布一次」**，每张 Agent 卡片标出 **「待重新发布：运行记录未包含在备份里」**。
- 在重新发布成功之前：
  - 受保护字段（地址、端口、服务类型、绑定配置空间、启用开关）禁止修改，也禁止换绑（返回 409）；
  - 规则自动同步与 DNS 联动暂停自动推送；
  - 已开启 DNS 高可用的配对仍保持锁定，发布照常按成对流程进行；
  - 删除 Agent 不受限制。
- **解除方式**：对每台普通 Agent 成功执行一次 **推送**（DNS 高可用成员走成对发布），成功后自动清除标记并恢复正常。

> 导入 `v2.2.0` 时期含运行记录的旧格式备份时，原有运行记录仍会一并导入，不会触发上述锁定标记。

<a id="docker-agent"></a>
## Docker Agent 使用与面板同版本的镜像标签

v3.0.0 起，面板镜像与 Docker Agent 镜像按**同一版本号**同步发布：

- 面板镜像：`thsrite/config-flow:<版本>`（例如 `3.0.0-rc.1` 或 `3.0.0`）
- 配套 Agent 镜像：`thsrite/config-flow-agent:<版本>`（与面板同一版本号）

使用时请注意：

1. **新装 Docker Agent**：请使用 v3.0.0 面板重新生成部署命令。面板生成的 `docker run`、Compose 与 OxiDNS 高可用文件会自动填入与面板同版本的 `thsrite/config-flow-agent:<版本>`。手写 Compose 请同步修改为同一版本号；开启登录后，旧版 `latest` 镜像不识别一次性注册凭据，新装会被 403 拒绝。
2. **更新已有 Docker Agent（重要）**：「Docker Agent 更新步骤」对话框的目标镜像默认填与面板配套的版本（例如 `thsrite/config-flow-agent:3.0.0`）。已有 Compose 文件或命令里写的是 `latest` 的，**请改为 `3` 或与面板相同的版本号**再拉取重建：`latest` 在冻结期内仍是 Python 时期的 Agent。
3. **无 Python 与 s6-overlay 变化**：Agent `1.9.3-go` 镜像已移除 Python 与 `supervisord`，改用 `s6-overlay` 管理进程；核心标准输出与错误输出合并写入 `/var/log/configflow/<核心>/current`（Agent 日志来源不再单列“错误输出”）。Host 模式 OxiDNS 高可用的辅助容器须与主容器使用**同一镜像**一起重建。

<a id="steps"></a>
## 升级步骤

1. **确认已完成 SQLite 迁移**：若数据目录尚无 `configflow.sqlite3`，或很久未升级，先换用 `thsrite/config-flow:2.2.0` 启动一次完成旧数据迁移和一次性清理，确认页面正常。
2. **停机与备份**：等待正在进行的 Agent 发布/回滚、HA 同步、路由器发布和固定 IP 操作结束；在「系统设置」导出一次全部数据；停止旧容器并**整体备份 `DATA_DIR`**（不要在运行中单独复制数据库文件）。
3. **核对部署配置**：
   - 设置 ≥32 字节随机 `JWT_SECRET_KEY`；
   - 确认 `DATA_DIR=/data` 与卷挂载一致，`stop_grace_period: 10s`；
   - 如信任私有/自签 CA，改用 `SSL_CERT_FILE` 或 `SSL_CERT_DIR`（`REQUESTS_CA_BUNDLE` 不再生效）；
   - 移除已废弃的 Python 调优变量（`CONFIGFLOW_HTTP_THREADS`、`CONFIGFLOW_MEMORY_TUNING`、`MALLOC_ARENA_MAX`、`MALLOC_TRIM_THRESHOLD_`），需要限制内存上限时改用 `GOMEMLIMIT` 或 `CONFIGFLOW_MEMORY_LIMIT_RATIO`；
   - 若曾定制容器内 Nginx 配置，请移到容器外的反向代理。
4. **切换镜像并启动**：将镜像换为 `thsrite/config-flow:3.0.0`（预发布为 `3.0.0-rc.1`；`autoupdate` 用户需拉取新版 Go `autoupdate` 镜像，如 `3.0.0-autoupdate`），重建容器并检查启动日志无异常报错。
5. **预热规则缓存并逐台全量推送**：
   - 保留原有远程规则缓存文件，不要手工改名或删除；先在规则库刷新预热缓存；
   - **逐台对所有 Agent 执行一次全量推送**（因 Go 原生 YAML/tar/gzip 排版变化，每台 Agent 会一次性显示 `changed`，推送并确认核心加载成功后变为 `synced`；OxiDNS 旧 ready 未激活候选需重新推送并校验）。
6. **按需完成一次性重做项**：
   - **DNS 联动（DNS 跟随）**：升级后首次 Mihomo 发布可能提示「绑定的上游已被编辑或移动」，在 OxiDNS 的 DNS 联动设置中重新选择原绑定上游并保存一次即可；
   - **IPv6**：重新检查就绪状态并重做一次验收；
   - **路由器**：自动规则同步如因基准变化暂停，手动「预览发布」一次建立新基准；路由器检查提醒显示「已过期」时重新检查一次即可；
   - **未完成操作**：重新发起尚未确认的固定 IP 预览、放弃并重开进行中的设置向导、刷新升级前打开的浏览器编辑页；
   - **通知**：已忽略通知中仍存在的问题会重新出现一次，确认后再忽略即可，无需为此重新推送。

<a id="changes"></a>
## Python → Go 的主要用户可感知变化

### 镜像与运行架构

- 面板运行镜像切换为 `alpine:3.24` + `tini` + Go 单二进制（直接提供 Web/API/MCP 与预压缩前端），彻底移除 Python、Nginx 与 `supervisord`，镜像体积显著缩小。
- 内置 Sub-Store 由 Go 主服务直接托管并按需启停（意外退出自动退避重启，最长 30 秒），其日志经脱敏后直接写入 **系统 → 日志**（来源 `sub-store`）；模板占位订阅地址不再反复触发刷新和唤醒 Sub-Store。
- 后台任务循环改为变化驱动，配置或状态变更时立即响应，空闲时大幅降低资源占用。
- `autoupdate` 镜像保留并改由 Go 程序实现增量重建（支持 Go 服务、前端、Agent 与 `node-probe`）；遇到仅有旧版 JSON 配置文件的目录时自动跳过切换并提示先经 `v2.2.0` 迁移。

### 安全与访问边界

- **新装 Agent 首次注册授权**：开启面板登录后，手拼无授权命令不再可用；新装须使用面板生成的命令（内含 24 小时有效、用后即废的一次性凭据）、注册密钥或面板 JWT；旧版 Python 生成的安装链接失效，请重新生成并妥善保管。
- **同源 CORS 与鉴权顺序**：CORS 仅对同源 Origin 放行，请使用同源面板或同源反代路径；开启登录后受保护接口先鉴权再解析配置空间，未登录统一返回 401。
- **凭据与路径保护**：日志、公开 Agent/部署/路由器接口与 WebDAV 地址均补强凭据脱敏；WebDAV 编辑表单留空密码不再清空已保存密码，地址和备份路径拒绝 `.`/`..` 路径段；请求体及规则/订阅/Sub-Store 下载（含解压后 64 MiB）均设统一上限。

### 可靠性与缺陷修复

- **安装与高可用修复**：Shell 安装覆盖失败可自动回滚恢复原 Agent 与服务状态；OxiDNS 高可用发布中成员失联超 10 分钟自动停止，恢复健康后可自动补完被中断的发布；停用高可用确认释放 keepalived 与 VIP。
- **设备与内核兼容**：回流路由兼容 Debian 12（nftables 1.0.6）；Mihomo `alpha-*` 等非标准内核版本按“未知”处理，不再阻断 EasyTier 配置生成；不存在的 Agent 日志与操作路由由 500 修正为 404；HEAD 请求不再误写数据。
- **损坏数据 fail closed**：损坏的存储或统计文件（OxiDNS 统计快照、域名发现流量文件、升级/高可用记录、`config_token` 等）不再被静默当作空值覆盖，改为明确报错并保留原数据，可通过现有入口恢复（域名发现页已新增「清除并恢复」按钮）。

### 输入校验与兼容性提醒

- **YAML 与正则**：YAML 读写改用与 Mihomo 一致的 `yaml.v3`（拒绝重复 key、非法显式 tag 与超预算 alias），修复旧版把 `0o17`、`1e3` 等字符串误判类型的缺陷；面板内部少数生僻正则或国际化域名写法可能需按 Go 规则微调（下发给客户端的规则原样输出）。
- **严格输入类型**：接口中非有限数字（`NaN`/`Infinity`）会被拒绝，历史记录中的非有限数响应时转为 `null`；端口、版本号与查询整数仅接受标准 ASCII 数字。
- **强制 DNS 归属保护**：路由器上若存在未登记归属、注释为 `cf-dnsguard:hijack` 的外部 NAT 规则，升级后检查会阻断并提示，请人工核实并修改其注释后再预览发布。
- **环境代理规则**：遵循 Go `httpproxy` 规则——`localhost`/loopback 始终直连，空小写变量不再屏蔽大写变量，`.example.com` 仅匹配子域，`NO_PROXY` 需写 `host:port` 而非单独 `:port`。
- **已知限制**：OxiDNS 高可用若某成员永久离线导致发布日志停在未知状态，当前需按停用提示强制清除记录并到设备上手动清理 keepalived 配置；MCP 多步工具若返回 `-32603`，请先查询当前状态再决定是否重试。
