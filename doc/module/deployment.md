# Docker / NAS 部署指南

本文适用于普通 Docker 主机。RouterOS Container 请使用 [专门安装指南](../../docs/CONFIGFLOW_AGENT_ROUTEROS_INSTALL.md)。

镜像包含前端、Python 后端、Sub-Store 和节点测速组件。准备好 Docker 与 Compose 后，按下面四步操作。

## 1. 准备目录和账号

```bash
mkdir -p configflow
cd configflow
```

在这个目录创建 `compose.yaml`。将下面密码和密钥替换为自己的随机值，密钥建议至少 32 位；用户名也可修改。

```yaml
services:
  config-flow:
    image: thsrite/config-flow:latest
    container_name: config-flow
    ports:
      - "5010:80"
    volumes:
      - ./data:/data
    environment:
      DATA_DIR: /data
      TZ: Asia/Shanghai
      ADMIN_USERNAME: admin
      ADMIN_PASSWORD: "replace-with-a-strong-password"
      JWT_SECRET_KEY: "replace-with-at-least-32-random-characters"
    restart: unless-stopped
```

`5010` 是浏览器访问的端口，右侧 `80` 保持不变。端口冲突时只改左侧，例如 `5011:80`。

`./data` 相对于部署文件所在目录解析。NAS 用户可以换成明确的绝对路径，例如 `/volume1/docker/configflow/data:/data`，路径必须按自己的设备填写。

## 2. 启动并打开页面

在部署文件所在目录执行：

```bash
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
```

| 部署位置 | 访问地址 |
| --- | --- |
| 当前电脑 | `http://localhost:5010` |
| NAS / 服务器 | `http://设备IP:5010`，例如 `http://192.168.1.10:5010` |

使用部署文件中的账号和密码登录。只有用户名、密码都设置完整才开启登录验证；本例已开启。

## 3. 填写设备能访问的地址

进入「系统 → 系统设置」：

1. 「服务域名」填写完整地址，例如 `http://192.168.1.10:5010`。其他设备不能使用浏览器自己的 `localhost`。
2. 生成并保存「访问令牌」，之后通过页面复制配置 URL。
3. 按 [第一次使用](../../README.md#第一次使用) 添加资源并生成配置。

访问令牌同时授权 MCP 管理工具，含令牌的订阅链接需要像密码一样保管。

Sub-Store 默认按需启动，空闲约 5 分钟后停止；第一次转换需要等待就绪。在「第三方依赖」可检查、更新或切换为常驻模式。无需另外开放容器内部的 `3001` 端口。

## 4. 认识数据目录

当前配置格式为 schema 5，主要目录如下。部分目录按需创建，不要求首次启动就全部出现。

```text
data/
├── configflow.sqlite3   # 系统、共享资源和全部配置空间
├── config.json          # 仅升级前的旧格式副本（若存在），新版本不再写入
├── configflow.sqlite3.corrupt-*  # 数据库损坏时保留的原文件（仅在出现损坏时）
├── shared/              # 共享订阅和规则缓存
├── profiles/<id>/       # 每个配置的生成文件、Provider 和转换规则
├── sub-store/           # 内置 Sub-Store 数据及在线更新组件
├── migrations/          # 迁移/导入快照（含数据库备份）与删除配置的保留数据（含 deleted-profile.json）
└── app.log              # 服务日志，路径可由 LOG_FILE 调整
```

Agent 主机监控图表（CPU、内存、磁盘和流量，最近 24 小时）的数据先保存在内存中，每台 Agent 最多保留 4000 个点。「系统设置 → 定时任务 → 监控历史保存」按间隔把新增数据追加写入磁盘，可选 5、10、30 或 60 分钟，默认 30 分钟；文件明显大于内存数据时自动整理。`docker compose stop` / `restart` 等正常停止（包括 `LOG_LEVEL=DEBUG` 调试模式）会先保存再退出；意外断电或强制结束最多丢失一个间隔的数据。写入失败（如磁盘已满、没有权限）会显示在该任务的上次运行结果中。这些文件目前位于容器内的 `/opt/configflow/data/metrics/`（源码运行时为 `backend/data/metrics/`），不在 `/data` 中，重建容器后从空开始。删除 Agent 时同时删除它的监控历史；恢复备份或重置后不再存在的 Agent，其监控历史在下次保存时删除。

旧实例还可能保留 `system.json`、旧 `profiles/<id>/config.json` 等文件。不要只看根配置就清理旧数据；迁移和恢复需要保留完整目录。

## 常用操作

所有命令均在部署文件所在目录执行。

| 操作 | 命令 |
| --- | --- |
| 查看状态 | `docker compose ps` |
| 查看最近日志 | `docker compose logs --tail=100 config-flow` |
| 持续查看日志 | `docker compose logs -f config-flow`，按 Ctrl+C 退出查看 |
| 停止 | `docker compose stop` |
| 启动 | `docker compose start` |
| 重启 | `docker compose restart` |
| 修改环境变量后重新创建容器 | `docker compose up -d` |

`restart` 不会应用部署文件中新改的环境变量，也不会拉取新镜像。

## 备份与升级

1. 在「系统设置」导出全部数据，并记录当前镜像版本或摘要。
2. 停服后复制整个实际挂载的数据目录。服务运行时不要只复制 `configflow.sqlite3`，可能复制到写到一半的状态；运行中请使用导出或 WebDAV 备份。
3. 确认备份成功，再更新镜像。

本例使用相对路径 `./data`：

```bash
docker compose stop
cp -a ./data "./data-backup-$(date +%Y%m%d-%H%M%S)"
```

确认复制完成后执行：

```bash
docker compose pull
docker compose up -d
docker compose logs --tail=100 config-flow
```

`latest` 是可变化的标签，不代表已核实包含某个修复。重要实例应记录可回退的版本或摘要，并先用数据副本验证。

Agent 最近一次心跳的状态（在线时间、服务状态、当前监控指标）只保存在内存里，不写入配置，也不在 JSON 导出和 WebDAV 备份里。服务重启后，各 Agent 在下一次心跳（通常 30–60 秒内）前显示为离线；版本号在变化时已写入配置数据库，会随备份保留。监控历史曲线（最近 24 小时）按上文「监控历史保存」的间隔写入磁盘，不随每次心跳写入。旧版本留下的 `runtime.sqlite3` 会在升级后首次启动时读入并删除。

旧版多配置必须完整备份 `system.json`、`profiles/`、`.bak` 和缓存，不能只备份根 `config.json`。完整 JSON 导出也不包含主机上所有运行文件。迁移边界和恢复方法见 [升级、备份与恢复](../design/multi-config-management.md#升级备份与恢复)。

仓库的 Compose 文件位于 `docker/docker-compose.yml`；它的 `./data` 指向 `docker/` 下的数据目录。改用它之前，明确指定原数据路径。

## 不使用 Compose 时

以下是另一种独立部署方式，与上面的 Compose 二选一。先进入准备保存数据的目录，并替换三个变量：

```bash
CF_ADMIN_USER='admin'
CF_ADMIN_PASSWORD='replace-with-a-strong-password'
CF_JWT_SECRET='replace-with-at-least-32-random-characters'

docker run --pull=always -d \
  --name config-flow \
  -p 5010:80 \
  -v "$(pwd)/data:/data" \
  -e ADMIN_USERNAME="$CF_ADMIN_USER" \
  -e ADMIN_PASSWORD="$CF_ADMIN_PASSWORD" \
  -e JWT_SECRET_KEY="$CF_JWT_SECRET" \
  thsrite/config-flow:latest
```

后续重新创建容器时保留原账号、密钥、端口和绝对数据路径。Compose 会把这些设置保存到文件中，更便于维护。

## 常见问题

| 现象 | 检查方法 |
| --- | --- |
| 容器启动失败 | 查看日志，检查 `5010` 是否被占用以及挂载目录权限 |
| 本机能打开，其他设备打不开 | 使用主机实际 IP，检查主机防火墙和端口；容器内部 `5001` 不是本例对外端口 |
| 更新后配置变空 | 先检查 `docker inspect config-flow` 中的 Mounts 是否仍指向原数据目录，保留现有文件并查看迁移日志 |
| Sub-Store 显示休眠 | 按需模式下属正常状态；转换任务会唤醒，失败时在第三方依赖查看原因 |
| 旧版还有 sub-store 服务 | 当前镜像内置组件，旧独立服务及 `SUB_STORE_URL` 环境变量不再使用 |
| 需要详细日志或调整线程 | 参阅 [README 部署参数](../../README.md#部署参数)；`DEBUG` 会启用开发调试模式 |

## 实现参考

[部署示例](../../docker/docker-compose.yml)、[镜像构建](../../docker/Dockerfile)、[Nginx 端口与代理](../../docker/nginx.conf)、[配置存储](../../backend/common/config_repository.py)、[Sub-Store 运行模式](../../backend/utils/sub_store_runtime.py)。
