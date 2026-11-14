# Go Agent 构建和部署指南

## 构建 Go Agent 二进制

### 1. 构建所有平台的二进制文件

```bash
cd backend/agents/go-agent
make build-all
```

这将在 `dist/` 目录下生成：
- `sublink-agent-linux-amd64` (Linux x86_64)
- `sublink-agent-linux-arm64` (Linux ARM64)
- `sublink-agent-linux-armv7` (Linux ARMv7)

### 2. 复制到静态资源目录

```bash
cp dist/sublink-agent-* ../../static/agents/
```

## 使用方式

### API 调用

#### 默认（Go 版本）
```bash
curl "http://your-server/api/agents/install-script?name=MyAgent&type=mihomo"
```

旧版纯脚本 Agent（`agent_type=shell`）已停止提供，请求会返回 400。已安装旧版的设备运行上面的安装命令即可原地升级为 Go Agent。

### 安装脚本参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `name` | Agent 名称 | My Agent |
| `type` | 服务类型 (mihomo/mosdns) | mihomo |
| `port` | Agent 端口 | 8080 |
| `agent_ip` | Agent IP（可选） | 自动检测 |
| `config_path` | 配置文件路径 | /etc/{type}/config.yaml |
| `restart_command` | 重启命令 | systemctl restart {type} |

## Docker 构建

### 构建多架构镜像

```bash
# 首先构建 Go Agent 二进制
cd backend/agents/go-agent
make build-all

# 返回项目根目录
cd ../../..

# 构建 Docker 镜像（多架构支持）
docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
  -t your-registry/sublink-agent:latest \
  -f Dockerfile.agent \
  --push .
```

### 运行 Docker Agent

```bash
docker run -d \
  --name sublink-agent \
  -e SERVER_URL=http://your-server:5001 \
  -e AGENT_NAME=my-agent \
  -e SERVICE_TYPE=mihomo \
  -e AGENT_TYPE=go \
  -p 8080:8080 \
  your-registry/sublink-agent:latest
```

## 版本对比

| 特性 | Go 版本 | Shell 版本 |
|------|---------|------------|
| **性能** | ⭐⭐⭐⭐⭐ 高 | ⭐⭐⭐ 中 |
| **内存占用** | ⭐⭐⭐⭐⭐ 低 (~10MB) | ⭐⭐⭐ 中 (~50MB) |
| **依赖** | 无依赖（单二进制） | 需要 bash、curl、socat/nc |
| **兼容性** | Linux (amd64/arm64/armv7) | 所有 Linux 发行版 |
| **启动速度** | ⭐⭐⭐⭐⭐ 快 | ⭐⭐⭐ 中 |
| **推荐使用** | ✅ 默认推荐 | 🔧 特殊环境备用 |

## 故障排查

### 二进制文件不存在

如果访问 `/static/agents/sublink-agent-linux-amd64` 返回 404：

1. 确认已构建二进制文件：
   ```bash
   ls backend/static/agents/
   ```

2. 如果不存在，执行构建：
   ```bash
   cd backend/agents/go-agent
   make build-all
   cp dist/* ../../static/agents/
   ```

### 权限问题

确保二进制文件有执行权限：
```bash
chmod +x backend/static/agents/sublink-agent-*
```

### 测试下载

```bash
curl -O http://your-server/static/agents/sublink-agent-linux-amd64
file sublink-agent-linux-amd64
# 应该显示: ELF 64-bit LSB executable
```

## 更新日志

### v2.0.0 (Go 版本)
- ✨ 使用 Go 重写，性能提升 5 倍
- ✨ 单二进制部署，无依赖
- ✨ 多架构支持 (amd64/arm64/armv7)
- ✨ 内存占用降低 80%
- 🔧 保留 Shell 版本兼容性

### v1.0.0 (Shell 版本)
- Shell 脚本实现
- 依赖 bash、curl、socat/nc
