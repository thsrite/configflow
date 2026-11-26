# Sublink Agent

Sublink Agent 是一个用于远程管理和控制 Mihomo 和 MosDNS 服务的代理程序。它提供了一个 RESTful API 接口，允许中心服务器远程更新配置、重启服务、查看日志等操作。

## 文档目录

- [README](README.md) - 项目说明文档
- [架构设计](docs/ARCHITECTURE.md) - 系统架构和设计原则
- [配置说明](docs/CONFIG.md) - 配置文件详细说明
- [API 接口](docs/API.md) - API 接口文档
- [部署指南](docs/DEPLOYMENT.md) - 部署和安装说明

## 功能特性

- **多服务支持**：同时支持 Mihomo 和 MosDNS 服务管理
- **自动注册**：首次运行时自动向中心服务器注册
- **心跳机制**：定期向中心服务器发送心跳包，报告服务状态
- **配置更新**：通过 API 接口远程更新服务配置
- **服务重启**：支持远程重启服务
- **日志查看**：可以远程获取服务运行日志
- **Docker 化部署**：提供完整的 Docker 部署方案
- **安全认证**：使用 Token 机制保证 API 调用的安全性

## 磁盘自动清理

Agent 启动后及每 10 分钟执行一次清理，部署结束时也会回收暂存文件：

- 仅删除 `succeeded`、`failed`、`rolled_back` 部署的 `stage/` 和 `bundle.tar.gz`。保留状态、清单和回滚备份；未完成、待激活、回滚失败的事务不清理。
- 清理与部署、重启、升级共用服务操作锁，服务忙时延后处理。
- `/var/log/configflow-agent.log` 超过 4 MiB 时原地压缩为最新 256 KiB，避免 OpenRC 持有旧日志描述符。与 copytruncate 类似，压缩瞬间的并发日志可能丢失。不存在的日志、容器输出和 systemd journal 不处理。
- 常规心跳与指标日志默认关闭，可用 `CONFIGFLOW_AGENT_DEBUG=1` 临时启用。
- 不删除业务配置、GeoIP 数据、内核备份或系统文件。

也可手动运行（不会注册、监听端口或重启代理核心）：

```sh
configflow-agent -config /opt/configflow-agent/config-mihomo.json -cleanup
```

## 项目结构

```
.
├── api.go              # API 服务实现
├── client.go           # 客户端注册和心跳功能
├── config.go           # 配置文件加载和保存
├── docker-compose.yaml # Docker Compose 配置
├── docker-entrypoint.sh# Docker 入口点脚本
├── Dockerfile          # Docker 构建文件
├── go.mod              # Go 模块定义
├── go.sum              # Go 模块校验和
├── heartbeat.go        # 心跳循环实现
├── main.go             # 程序入口
├── Makefile            # 构建和管理脚本
├── system.go           # 系统相关工具函数
├── svc/                # Docker 镜像的进程控制（s6）与服务定义
├── dockerimage/        # 容器启动准备（docker-config）
├── ARCHITECTURE.md     # 架构设计文档
├── CONFIG.md           # 配置说明文档
├── API.md              # API 接口文档
├── DEPLOYMENT.md       # 部署指南文档
└── README.md           # 项目说明文档
```

## 工作原理

Sublink Agent 作为一个独立的服务运行，主要完成以下任务：

1. **初始化阶段**：
   - 加载本地配置文件
   - 如果未注册，则向中心服务器发起注册请求
   - 注册成功后获得 Agent ID 和 Token

2. **运行阶段**：
   - 启动 HTTP API 服务监听指定端口
   - 启动后台心跳循环，定期向中心服务器发送心跳包
   - 等待并处理来自中心服务器的指令

3. **API 接口**：
   - `/health`：健康检查接口
   - `/api/config/update`：更新服务配置
   - `/api/restart`：重启服务
   - `/api/logs`：获取服务日志
   - `/api/uninstall`：卸载服务

## 配置说明

配置文件采用 JSON 格式，主要包含以下字段：

```json
{
  "server_url": "http://your-server.com",  // 中心服务器地址
  "agent_name": "mihomo-agent",            // Agent 名称
  "agent_host": "0.0.0.0",                 // Agent 监听地址
  "agent_port": 8080,                      // Agent 监听端口
  "agent_ip": "192.168.1.100",             // Agent 外网IP（可选）
  "service_type": "mihomo",                // 服务类型（mihomo 或 mosdns）
  "service_name": "mihomo",                // 服务名称
  "config_path": "/etc/mihomo/config.yaml",// 服务配置文件路径
  "restart_command": "/usr/local/bin/configflow-agent svc restart mihomo", // 重启命令（Docker 镜像）
  "heartbeat_interval": 60                 // 心跳间隔（秒）
}
```

对于 MosDNS 服务，还支持额外的特殊功能字段：

```json
{
  // ... 基础配置字段 ...
  "directories": [                         // 需要创建的目录列表
    "rules",
    "geodata"
  ],
  "ruleset_downloads": [                   // 规则集下载配置
    {
      "name": "geosite",                   // 规则集名称
      "url": "https://example.com/geosite.dat", // 下载地址
      "local_path": "/etc/mosdns/geosite.dat"   // 保存路径
    }
  ]
}
```

## 部署方式

### Docker 部署（推荐）

1. 克隆项目代码：
   ```bash
   git clone <repository-url>
   cd go-agent
   ```

2. 修改 [docker-compose.yaml](file:///vol1/1000/docker/go-agent/docker-compose.yaml) 文件中的环境变量：
   ```yaml
   environment:
     - SERVER_URL=http://your-server-address:port  # 修改为中心服务器地址
     # 其他配置...
   ```

3. 启动服务：
   ```bash
   docker-compose up -d
   ```

### 手动部署

1. 编译程序：
   ```bash
   go build -o sublink-agent .
   ```

2. 创建配置文件（参考上面的配置说明）

3. 运行程序：
   ```bash
   ./sublink-agent -config=/path/to/config.json
   ```

### 使用 Makefile

项目提供了 Makefile 来简化构建和管理：

```bash
# 格式化代码
make fmt

# 构建应用
make build

# 运行应用
make run

# 构建 Docker 镜像
make docker-build

# 启动 Docker Compose
make compose-up
```

## API 接口说明

所有 API 接口都需要在请求头中包含 Authorization 字段：
```
Authorization: Bearer <your-token>
```

### 健康检查
```
GET /health
```

### 更新配置
```
POST /api/config/update
Content-Type: application/json

{
  "config": "服务配置内容"
}
```

对于 MosDNS，还可以包含特殊字段：
```json
{
  "config": "服务配置内容",
  "directories": ["rules", "geodata"],
  "ruleset_downloads": [
    {
      "name": "geosite",
      "url": "https://example.com/geosite.dat",
      "local_path": "/etc/mosdns/geosite.dat"
    }
  ]
}
```

### 重启服务
```
POST /api/restart
```

### 获取日志
```
GET /api/logs?lines=100
```

### 卸载服务
```
POST /api/uninstall
```

## 安全性

1. 所有 API 接口都通过 Token 进行身份验证
2. 通信建议使用 HTTPS 加密传输
3. Agent 以非 root 用户运行，提高系统安全性
4. 配置文件权限严格控制

## 故障排除

1. **无法注册到中心服务器**：
   - 检查 [SERVER_URL](file:///vol1/1000/docker/go-agent/config.go#L12-L12) 配置是否正确
   - 检查网络连接是否正常
   - 检查中心服务器是否正常运行

2. **心跳失败**：
   - 检查网络连接
   - 检查 Token 是否有效
   - 查看日志获取详细错误信息

3. **API 调用失败**：
   - 检查 Token 是否正确
   - 检查请求格式是否正确
   - 查看日志获取详细错误信息

## 许可证

[MIT License](LICENSE)
## Shell 网页一键更新

标准 systemd/OpenRC 安装可在主服务更新后，从 Agent 页面完成程序更新、旧配置迁移和恢复服务安装，无需重新运行安装命令。页面持续查询任务，确认目标版本就绪后才显示成功；失败恢复旧版本。Docker 点击“更新”打开镜像更新指南，提供 Compose / Docker Run 操作步骤和可复制命令，保留数据卷重建容器后刷新列表确认版本。

详见 [在线升级方案与首次旧协议限制](docs/ONLINE_UPGRADE.md) 和 [真实验收记录](docs/ONLINE_UPGRADE_ACCEPTANCE.json)。
