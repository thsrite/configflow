# 配置文件详解

ConfigFlow Agent 支持多种配置方式，可以适应不同的部署环境和需求。

## 配置加载顺序

Agent 启动时按以下顺序查找配置文件：

1. 命令行参数指定的配置文件：`-config=/path/to/config.json`
2. 自动检测配置文件（按优先级排序）：
   - `/opt/configflow-agent/config-mihomo.json`（Mihomo 专用配置）
   - `/opt/configflow-agent/config-mosdns.json`（MosDNS 专用配置）
   - `/opt/configflow-agent/config-oxidns.json`（OxiDNS 专用配置）
   - `/opt/configflow-agent/config.json`（通用配置）
3. 未设置 `AGENT_DIR` 且上述目录没有配置时，以相同文件名顺序兼容 `/opt/sublink-agent/`。

设置 `AGENT_DIR` 时，只在指定目录自动查找；显式 `-config` 始终优先。

## 基础配置字段

### server_url
- **类型**：字符串
- **必须**：是
- **说明**：中心服务器的 URL 地址
- **示例**：`"http://192.168.1.100:8080"`

### agent_name
- **类型**：字符串
- **必须**：是
- **说明**：Agent 的名称，在中心服务器上用于标识该 Agent
- **示例**：`"mihomo-agent-01"`

### agent_host
- **类型**：字符串
- **必须**：是
- **说明**：Agent 监听的主机地址
- **示例**：`"0.0.0.0"`（监听所有接口）或 `"127.0.0.1"`（仅本地）

### agent_port
- **类型**：整数
- **必须**：是
- **说明**：Agent 监听的端口号
- **示例**：`8080`

### agent_ip
- **类型**：字符串
- **必须**：否
- **说明**：Agent 注册地址；原生 AIO 的 TUN 自动重定向和选择性网关也用它排除本机 DNS，应填写目标 Linux 的 LAN IPv4。留空时，注册自动选择可用本机地址，发布则重新获取全部符合条件的本机 IPv4，为每个地址补充精确 `/32` DNS 排除项，覆盖多网卡和同网卡多个地址。自动地址不会写死到配置中，已注册 Agent 无需重新注册；显式填写时仍以该地址为准。
- **校验**：DNS 排除的自动探测跳过回环、停用、点对点及常见 TUN / Docker 虚拟接口；注册地址探测仍兼容只有 PPP / TAP 地址的机器。没有有效 IPv4 或显式填写了无效地址时，DNS 排除校验会失败并提示处理。激活已准备的配置之前，还会检查当前地址是否已被候选配置覆盖；地址变化导致漏排除时需重新发布，原配置和运行服务保持不变。
- **示例**：`"192.168.1.10"`

### service_type
- **类型**：字符串
- **必须**：是
- **说明**：服务类型，目前支持 `mihomo`、`mosdns` 和 `oxidns`
- **示例**：`"mihomo"`

### service_name
- **类型**：字符串
- **必须**：是
- **说明**：服务名称，Docker 镜像中必须与 s6 管理的程序名一致（mihomo、mosdns、oxidns）
- **示例**：`"mihomo"`

### config_path
- **类型**：字符串
- **必须**：是
- **说明**：被管理服务的配置文件路径
- **示例**：`"/etc/mihomo/config.yaml"`

### restart_command
- **类型**：字符串
- **必须**：是
- **说明**：重启服务的命令
- **示例**：`"/usr/local/bin/configflow-agent svc restart mihomo"`（Docker 镜像）

### heartbeat_interval
- **类型**：整数
- **必须**：否
- **默认值**：30
- **说明**：心跳间隔（秒）
- **示例**：`60`

### agent_id
- **类型**：字符串
- **必须**：否
- **说明**：Agent ID，注册成功后自动生成
- **示例**：`"agent-1234567890"`

### token
- **类型**：字符串
- **必须**：否
- **说明**：认证 Token，注册成功后自动生成
- **示例**：`"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`

## 事务发布配置

事务发布支持通过 Shell 安装的 Go Agent 和 Docker 内的 Go Agent。旧版 `agent.sh` 不支持发布包协议，需在 Agent 管理重新生成 Go Agent 安装命令升级；“Shell 安装”不等于运行旧版 Shell Agent。

| 字段 | 用途 |
| --- | --- |
| `service_manager` | `systemd`、`openrc`、`s6`（Docker 镜像；1.9.3-go 之前的镜像写 `supervisor`，读取时视为同义词，计划下个大版本删除）或 `command` |
| `service_unit` | 被管理的核心服务名称；systemd 可用 `mihomo` 或 `mihomo.service` |
| `service_binary` | 与运行核心相同的可执行程序，Mihomo 用它执行 `-t`，OxiDNS 用它执行原生 `check` |
| `stop_command` / `start_command` / `status_command` | `command` 管理器必需的三个明确动作；不从重启命令推断 |
| `health_url` | 可选 HTTP 健康检查地址，未配置时从 Mihomo 控制接口或 OxiDNS API 配置发现探测目标 |
| `health_dns_address` | 可选 DNS 探测地址，例如 `127.0.0.1:53` |
| `health_dns_name` | DNS 探测域名，默认 `example.com`；可改为当前配置中可稳定解析的域名 |
| `deployment_health_timeout` | 启动后检查超时秒数，默认 30 |

原生安装示例：

```json
{
  "service_manager": "systemd",
  "service_unit": "mihomo",
  "service_binary": "/usr/local/bin/mihomo",
  "deployment_health_timeout": 30
}
```

发布包、暂存区、备份、发布清单位于 `config_path` 同目录的 `.configflow-deployments/`，需要保留和持久化整个服务目录。不要在存在未完成事务时移动配置目录或删除该目录。

Docker 入口会填写 `s6` 管理器。可通过 `MIHOMO_HEALTH_URL`、`MIHOMO_HEALTH_DNS_ADDRESS`、`MIHOMO_HEALTH_DNS_NAME` 或对应的 `MOSDNS_`、`OXIDNS_` 变量覆盖探测目标；`DEPLOYMENT_HEALTH_TIMEOUT` 设置等待秒数。

OxiDNS 的 `config_path` 必须与核心实际加载的配置一致；systemd 的 `-d`/`WorkingDirectory` 也必须等于配置所在目录。安装器通过 `-validate-service` 在替换 Agent 前检查该条件，推送和手动回滚继续校验。`state/` 中的动态域名、查询记录和缓存属于运行数据，不能作为受管配置覆盖；原生配置检查使用隔离目录。

### 部署检查与人工恢复

```sh
# 只读检查部署记录，不停止核心、不改写文件、不注册、不监听 HTTP。
configflow-agent -config /opt/configflow-agent/config-mihomo.json -check-deployments

# 人工恢复：包含服务停止、恢复和检查，不注册、不监听 HTTP。
configflow-agent -config /opt/configflow-agent/config-mihomo.json -recover-and-exit

# 旧参数兼容：已弃用，现等同 -check-deployments，仅检查，不恢复。
configflow-agent -config /opt/configflow-agent/config-mihomo.json -recover-only
```

正常 Agent 启动只读检查未完成事务，保留备份和原有状态，记录需要人工处理的提示并继续提供经过鉴权的状态、日志查询；不会停止核心、恢复文件或完成回滚。未解决事务会阻止新的发布、服务重启、Agent 更新和卸载。Agent 配置加载或首次注册自身失败仍可能导致 Agent 无法启动。

`-check-deployments` 对未完成、损坏或无法读取的部署记录只输出提示，仍返回成功；需查看输出，不能把退出码 0 当作部署已完成。Agent 配置无法读取或 JSON 无效时仍返回非零退出码。`-recover-only` 是该只读检查的兼容入口，不取得服务操作锁，也不恢复文件。实际恢复统一使用 `-recover-and-exit`，失败时返回非零退出码。执行人工恢复前先停止对应 Agent，成功后再启动；原生及 Docker 命令见 [部署指南](DEPLOYMENT.md#失败诊断与恢复)。旧版本留下的 `recovery_pending` 需显式执行 `-recover-and-exit` 验证服务健康，之后才转为 `rolled_back`。

Mihomo、MosDNS、OxiDNS 在 systemd、OpenRC 和 Docker 中启动均不依赖部署恢复；安装和升级会清理旧 ConfigFlow 启动恢复依赖。核心按自身服务配置启动，Agent 重启或容器重建不会自动修复未完成事务。推送期间的校验、备份、健康检查和失败回滚保持启用。成功发布后的显式手动回滚接口也保留；它只接受当前生效且备份完整、受管文件未被外部改写的发布，不代替中断事务恢复。

## MosDNS 旧版兼容配置字段

下面的 `directories` 和 `ruleset_downloads` 用于旧配置兼容。事务发布所需规则由 ConfigFlow 完整准备并放入发布包，不依靠这些字段在替换后补下载；新的发布入口以清单中的文件为准。


### directories
- **类型**：字符串数组
- **必须**：否
- **说明**：需要创建的目录列表，用于存放规则文件等
- **示例**：
  ```json
  "directories": [
    "rules",
    "geodata",
    "cache"
  ]
  ```

### ruleset_downloads
- **类型**：对象数组
- **必须**：否
- **说明**：规则集下载配置
- **字段**：
  - `name`：规则集名称
  - `url`：下载地址
  - `local_path`：本地保存路径

- **示例**：
  ```json
  "ruleset_downloads": [
    {
      "name": "geosite",
      "url": "https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
      "local_path": "/etc/mosdns/geosite.dat"
    },
    {
      "name": "geoip",
      "url": "https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geoip.dat",
      "local_path": "/etc/mosdns/geoip.dat"
    }
  ]
  ```

## 环境变量配置

在 Docker 部署中，可以通过环境变量来配置 Agent：

### 通用环境变量

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `SERVER_URL` | 中心服务器地址 | 无 |
| `HEARTBEAT_INTERVAL` | 心跳间隔（秒） | Dockerfile 设置 30；部分 Compose 示例显式覆盖为 60 |
| `AGENT_IP` | Agent 外网 IP | 自动检测 |

### Mihomo 相关环境变量

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `ENABLE_MIHOMO` | 是否启用 Mihomo | false |
| `AGENT_MIHOMO_NAME` | Mihomo Agent 名称 | mihomo-agent |
| `AGENT_MIHOMO_PORT` | Mihomo Agent 端口 | 8080 |

### MosDNS 相关环境变量

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `ENABLE_MOSDNS` | 是否启用 MosDNS | false |
| `AGENT_MOSDNS_NAME` | MosDNS Agent 名称 | mosdns-agent |
| `AGENT_MOSDNS_PORT` | MosDNS Agent 端口 | 8081 |

### OxiDNS 相关环境变量

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `ENABLE_OXIDNS` | 是否启用 OxiDNS | false |
| `AGENT_OXIDNS_NAME` | OxiDNS Agent 名称 | oxidns-agent |
| `AGENT_OXIDNS_PORT` | OxiDNS Agent 端口 | 8082 |

AIO 的 MosDNS 和 OxiDNS 默认都使用 DNS 端口 53，入口要求二者只启用一个；这与部署恢复检查无关。

## 配置示例

### Mihomo 配置示例

```json
{
  "server_url": "http://192.168.1.100:8080",
  "agent_name": "mihomo-agent-01",
  "agent_host": "0.0.0.0",
  "agent_port": 8080,
  "agent_ip": "203.0.113.1",
  "service_type": "mihomo",
  "service_name": "mihomo",
  "service_manager": "s6",
  "service_unit": "mihomo",
  "service_binary": "/usr/local/bin/mihomo",
  "config_path": "/etc/mihomo/config.yaml",
  "restart_command": "/usr/local/bin/configflow-agent svc restart mihomo",
  "heartbeat_interval": 60
}
```

### MosDNS 配置示例

```json
{
  "server_url": "http://192.168.1.100:8080",
  "agent_name": "mosdns-agent-01",
  "agent_host": "0.0.0.0",
  "agent_port": 8081,
  "agent_ip": "203.0.113.1",
  "service_type": "mosdns",
  "service_name": "mosdns",
  "service_manager": "s6",
  "service_unit": "mosdns",
  "service_binary": "/usr/local/bin/mosdns",
  "config_path": "/etc/mosdns/config.yaml",
  "restart_command": "/usr/local/bin/configflow-agent svc restart mosdns",
  "heartbeat_interval": 60,
  "directories": [
    "rules",
    "geodata"
  ],
  "ruleset_downloads": [
    {
      "name": "geosite",
      "url": "https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
      "local_path": "/etc/mosdns/geosite.dat"
    }
  ]
}
```

## 配置更新

配置文件会在以下情况下自动更新：

1. **注册成功后**：Agent 会将获得的 `agent_id` 和 `token` 保存到配置文件中
2. **API 更新**：通过 `/api/config/update` 接口更新服务配置时，会备份旧配置并写入新配置

配置文件更新时会自动创建备份，备份文件名为 `原文件名.backup.时间戳`。
