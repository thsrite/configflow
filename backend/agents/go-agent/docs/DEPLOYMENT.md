# 部署指南

事务发布支持 Mihomo、MosDNS、OxiDNS，以及通过 Shell 安装或 Docker 运行的 Go Agent。ConfigFlow 与 Agent 都应更新到支持发布协议 1 的版本；旧版 `agent.sh` 不支持该协议，界面会要求升级，发布不会退回逐文件覆盖方式。

## 完整发布流程

```text
ConfigFlow：固定配置快照 → 验证与准备完整资源 → tar.gz 打包 → 发送
Go Agent：接收 → 独立暂存区解包 → 文件清单校验 → 配置校验 → 准备就绪
激活：磁盘检查 → 停服务 → 完整备份 → 替换 → 启动 → 健康检查 → succeeded
失败：恢复原文件、权限和属主 → 恢复原运行状态 → 检查回滚结果
```

ConfigFlow 准备主配置、规则、Provider 和受管自定义文件。必需下载失败会停止准备；文本与二进制资源都按原始字节处理，合法空规则可以存在。发布包清单包含发布 ID、目标 Agent、服务类型、路径、大小和 SHA-256；Agent 同时检查压缩包摘要、文件摘要及路径边界。

预检查在旧服务运行时完成。Mihomo 使用目标机器实际安装的同一个二进制执行 `-t`；MosDNS 检查 YAML 和受管文件引用，再由启动后的 DNS 检查验证运行结果。OxiDNS 使用实际核心执行原生 `check`，验证资源引用和配置目录一致性，并在激活前复查；动态 state 数据不会被发布覆盖。备份在停服务后执行，避免运行中的 Provider 更新造成备份不一致。备份失败不会继续替换。

启动后的检查既看服务管理器状态，也检查实际功能。Mihomo 会检查可用的控制接口、声明的 Provider，以及配置启用的代理监听端口；MosDNS 和 OxiDNS 执行 DNS 查询；OxiDNS 配置 API 时还检查其就绪端点，并保留认证和 TLS 校验。默认健康检查窗口为 30 秒，需要连续通过检查才确认成功。

只有 `succeeded` 表示本次文件已安装且服务检查通过，ConfigFlow 才据此确认本次发布的配置版本。`receiving`、`verifying`、`ready` 和 HTTP 202 都只是中间状态。连接超时后按同一发布 ID 查询结果；不要把未收到响应当成已回滚。

## 前端暂存、应用与结果

Agent 管理中的“推送配置”默认立即激活，并显示准备、校验、备份、替换、启动检查及回滚阶段。

通过后端发布接口 `POST /api/agents/<agent_id>/push-config` 传入 `restart: false` 时，只上传并校验文件，停在 `ready`，正式文件和当前服务保持原状。前端显示“已校验，待激活”和“应用并启动”；点击后重新核对暂存文件，再进入停服、备份、替换流程。暂存不是一次成功发布。

| 状态 | 含义与处理 |
| --- | --- |
| `succeeded` | 发布成功，启动检查通过 |
| `failed` | 本次发布失败；查看失败阶段和原因 |
| `rolled_back` | 已恢复发布前的文件及相应运行状态；可能由发布失败或显式手动回滚触发 |
| `recovery_pending` | 旧版本已恢复旧文件，等待人工执行完整恢复并检查服务 |
| `rollback_failed` | 回滚尚未完成，需要处理；界面提供“查询恢复状态” |
| 结果待确认 | 通信暂时失败，前端继续查询同一发布 ID |

成功发布后仍可显式手动回滚当前生效版本。该操作要求完整备份，先检查当前受管文件未被外部改写，再恢复上一版本并确认服务健康；不能选择任意历史事务覆盖当前配置。原生 OxiDNS 的工作目录校验和运行状态文件保护继续生效。

同一服务的发布、服务重启、Agent 更新和卸载会互斥。存在未解决的恢复事务时，这些变更操作被阻止。

## Shell 安装与升级

在 ConfigFlow 的 Agent 管理生成 Go Agent 安装命令（`agent_type=go`），按提示选择安装或升级。脚本支持 Linux amd64、arm64、armv7，自动识别 systemd 或 OpenRC。Mihomo/MosDNS/OxiDNS 核心及对应系统服务需提前安装。OxiDNS 安装前还会用 `-validate-service` 核对核心 `-c`、`-d`/`WorkingDirectory` 与 Agent 配置目录，校验失败不替换旧 Agent。

Shell 命令安装的仍是 Go 单二进制。发布运行时不需要安装 Python、tar/gzip、Redis 或数据库；安装阶段下载二进制需要 wget 或 curl。二进制暂存在 `/usr/local/bin` 内的唯一临时文件，适用于 `/tmp` 挂载为 `noexec` 的主机，并通过同一文件系统内的重命名替换。

重新安装时，相同 Agent 名称和服务类型会继承原注册 ID 与 Token，也会从旧版 `/opt/configflow-agent/config.json` 迁移身份。安装器只检查已有未完成事务；需要恢复时保留现场并提示先人工处理，不会自动停核心或恢复文件。自定义服务生命周期需明确提供停止、启动和状态命令，不能只提供任意重启命令。

核心启动不依赖 ConfigFlow Agent 或部署恢复服务。安装及升级会清理旧版本生成的启动恢复依赖：

- systemd 清除核心及 Agent unit 中对对应 `configflow-recover-mihomo`、`configflow-recover-mosdns`、`configflow-recover-oxidns` 的依赖，移除不再包含其他设置的 `configflow-recovery.conf` 和对应恢复服务文件，并刷新服务管理器；保留用户的其他 unit 设置。
- OpenRC 移除核心 `/etc/conf.d/<service>` 中 ConfigFlow recovery 标记区域和对应恢复服务，保留已有其他设置。
- 卸载也兼容清理旧恢复依赖，保留核心配置与发布备份。

原生安装默认配置目录是 `/opt/configflow-agent`，默认 Agent API 端口为 8080，心跳间隔 30 秒。安装时可指定端口和核心配置路径，服务生命周期及健康检查字段见 [配置说明](CONFIG.md)。

## Docker 部署与升级

使用项目的 [AIO Compose 示例](../../../../docker/docker-compose-aio.yml)，或 Agent 管理生成的 Compose 配置；也可参考 [Mihomo 单服务示例](../../../../docker/docker-compose-mihomo.yaml) 和 [MosDNS 单服务示例](../../../../docker/docker-compose-mosdns.yaml)。需要已安装 Docker，使用 Compose 文件时还需 Docker Compose。

Docker 镜像包含 Go Agent、Mihomo/MosDNS/OxiDNS 核心和 s6-overlay（Agent 1.9.3-go 起不含 Python），不需要为事务发布额外安装软件包。入口按配置生成 s6 服务后启动 s6；存在未完成部署时，Agent 只读检查并记录提示，不会执行自动恢复或阻止其他核心启动。已有事务的服务目录会保留现场，不用默认配置覆盖缺失的受管配置。

保留完整服务目录和 Agent 身份目录：

```yaml
volumes:
  - ./agent_data/agent:/opt/configflow-agent
  - ./agent_data/mihomo:/etc/mihomo
  - ./agent_data/mosdns:/etc/mosdns
  - ./agent_data/oxidns:/etc/oxidns
```

单服务部署只需挂载对应服务目录和 Agent 目录。服务目录包含 `.configflow-deployments/`，必须与配置一起持久化；只挂载 `config.yaml` 不满足发布和恢复要求。

| 设置 | 默认或示例行为 |
| --- | --- |
| `SERVICE_TYPE=mihomo`、`mosdns` 或 `oxidns` | 单服务模式，Mihomo/MosDNS Agent API 默认 8080，OxiDNS 默认 8082 |
| `ENABLE_MIHOMO` / `ENABLE_MOSDNS` / `ENABLE_OXIDNS` | AIO 模式下显式设为 `true` 才启用对应服务 |
| `AGENT_MIHOMO_PORT` / `AGENT_MOSDNS_PORT` / `AGENT_OXIDNS_PORT` | AIO 默认 8080 / 8081 / 8082 |
| `SERVER_URL` | 必填 ConfigFlow 服务地址 |
| `AGENT_IP` | 可选，填写可被 ConfigFlow 访问的 Agent 地址 |
| `HEARTBEAT_INTERVAL` | Dockerfile 默认 30 秒；部分 Compose 示例显式设为 60 秒 |
| `DEPLOYMENT_HEALTH_TIMEOUT` | 默认 30 秒 |
| `MIHOMO_HEALTH_*` / `MOSDNS_HEALTH_*` / `OXIDNS_HEALTH_*` | 覆盖 HTTP/DNS 探测目标，字段见配置说明 |

AIO 同时启用 Mihomo 和一个 DNS 核心时，新建的默认配置让 MosDNS/OxiDNS 使用 53、Mihomo DNS 使用 1053。MosDNS 与 OxiDNS 默认端口相同，入口要求二者只启用一个。代理端口、控制接口和 DNS 端口应按配置映射；9090 是 Mihomo 控制接口端口。

在 Compose 文件所在目录启动和查看日志：

```sh
docker compose up -d
docker compose logs --tail=100
```

升级时更新 Agent 镜像并重新创建容器，保留上述数据卷及启动环境。源代码自建镜像使用仓库内 `docker/Dockerfile.agent`；无需在运行容器内安装 Go 编译器。

## 磁盘空间与备份保留

每个服务的 `.configflow-deployments/<发布ID>/` 可包含发布包、暂存文件、有效清单、`state.json` 和 `backup/`。事务记录在替换前落盘，备份保留原有文件内容、传统 Unix 权限位（mode）及属主、属组（UID/GID）；回滚也会删除本次新增的受管文件。

当前文件替换与恢复仅支持上述传统权限元数据，不保留额外 ACL、SELinux 标签等扩展权限属性。依赖这些属性才能访问配置或规则的安装方式尚未验证，也不属于当前事务发布的支持范围；必须先适配并验证扩展属性的复制与恢复，再启用事务发布。

清理策略是保留当前处理的事务、当前生效清单指向的事务，以及另外最近两条已完成事务（`succeeded`、`failed`、`rolled_back`）。较旧的这些已完成事务可被清理，其发布包、暂存文件和备份随目录一起删除。`ready`、`rollback_failed` 等未解决事务不会被自动清理，因此这不是固定最多两份备份，也不是按天数保留。

磁盘空间需容纳压缩包、解包内容、旧文件备份以及替换时的临时副本，不能按固定 100 MB 估算。Agent 会在停服前检查备份与替换所需空间；运行期间空间耗尽仍会触发失败处理。需要长期留存的已完成备份应另行保存。

## 失败诊断与恢复

先在 Agent 卡片查看失败阶段、发布错误和回滚错误。需要更多信息时，读取该发布的 `state.json`，并查看 Agent 及核心日志。状态查询接口保留现有鉴权，不要把 Token 贴到日志或共享诊断记录中。

Agent 1.8.17-go 起，发布记录中的 `error_details` 和 `rollback_error_details` 保留校验、服务命令及健康探测返回的具体错误，单项最多 16 KiB，密码、Token 和 URL 凭据会隐藏。Mihomo 与 OxiDNS 包含原生校验输出；MosDNS 使用现有 YAML/插件检查及服务启动、健康探测结果，不新增原生校验步骤。OxiDNS 热重载失败且回退重启也失败时，两次原因一并记录。成功发布或成功完成回滚后会清除对应旧错误。旧版已丢弃的校验输出无法补回，需要更新 Agent 后的新发布才能记录详情；服务管理器未提供具体原因时，仍需查看核心日志。

- systemd Agent 日志：`journalctl -u configflow-agent --no-pager -n 100`。
- OpenRC Agent 日志：`/var/log/configflow-agent.log`。
- Docker 日志：`docker compose logs --tail=100`；核心日志在容器内 `/var/log/configflow/<核心>/current`。
- 发布状态：前端查询，或通过 ConfigFlow 的 `GET /api/agents/<agent_id>/deployments/<发布ID>` 获取。

普通 Agent 启动只读检查部署记录，发现未完成或损坏的事务时记录提示并继续提供状态、日志查询 API；不会停止核心、改写文件或自动回滚。核心按自身服务配置启动，是否可用取决于磁盘上的实际配置。未解决事务仍会阻止新的发布、服务重启、Agent 更新和卸载。

可单独执行只读检查，不注册、不监听 HTTP。未完成、损坏或无法读取的部署记录只会输出提示，命令仍返回成功；需查看输出判断是否有待处理事务。Agent 配置文件无法读取或 JSON 无效时仍返回非零退出码：

```sh
sudo /usr/local/bin/configflow-agent -config /opt/configflow-agent/config-mosdns.json -check-deployments
```

出现 `rollback_failed`、`recovery_pending` 或发布中断提示时：

1. 保留发布目录、备份和日志，按 `rollback_error` 检查磁盘空间、目录权限、文件属主、核心二进制、服务管理器和探测目标。
2. 修复具体原因。若备份缺失或校验值不符，重复重启不能重建备份；应先从独立可信备份修复文件，保留失败现场供核对。
3. 原生安装先停止 Agent，避免与服务操作并发，再执行已实现的恢复命令：

   ```sh
   sudo systemctl stop configflow-agent
   sudo /usr/local/bin/configflow-agent -config /opt/configflow-agent/config-mihomo.json -recover-and-exit
   ```

   OpenRC 用 `sudo rc-service configflow-agent stop` 停止 Agent；MosDNS 使用 `config-mosdns.json`，OxiDNS 使用 `config-oxidns.json`。只有恢复命令成功后，再使用对应服务管理器启动 Agent。
4. Docker 在修复持久化目录或部署设置后，停止对应 Agent 并显式恢复。下面以 MosDNS 为例，将 `容器名` 替换为实际容器名；Mihomo 使用 `agent-mihomo` 和 `config-mihomo.json`，OxiDNS 使用 `agent-oxidns` 和 `config-oxidns.json`：

   ```sh
   docker exec 容器名 configflow-agent svc stop agent-mosdns
   docker exec 容器名 /usr/local/bin/configflow-agent -config /opt/configflow-agent/config-mosdns.json -recover-and-exit
   # 上一条恢复命令成功后再启动 Agent。
   docker exec 容器名 configflow-agent svc start agent-mosdns
   ```
5. 在前端点击“查询恢复状态”，确认恢复结果和核心实际可用性后再发布。

`-recover-and-exit` 会按事务记录停止服务、恢复文件，并在原服务需要运行时启动及检查它；失败返回非零退出码。旧版本留下的 `recovery_pending` 也需用此命令完成服务检查。`-recover-only` 已弃用，现作为兼容入口仅执行与 `-check-deployments` 相同的只读检查，不再恢复文件或取得服务操作锁。重新启动 Agent 或容器不会代替人工恢复。不要手工把失败状态改成 `succeeded` 或删除未解决的事务目录来绕过恢复。
