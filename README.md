<div align="center">

# ConfigFlow

### 🚀 现代化代理配置管理平台

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Available-2496ED?logo=docker&logoColor=white)](https://hub.docker.com/r/thsrite/config-flow)
[![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white)](https://python.org)
[![Vue](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vue.js&logoColor=white)](https://vuejs.org)

**一站式管理订阅、节点、规则，一键生成 Mihomo / Surge / Loon / MosDNS 配置**

[功能特性](#-功能特性) •
[快速开始](#-快速开始) •
[升级与数据安全](#升级与数据安全) •
[文档](#-文档) •
[截图预览](#-截图预览)

</div>

---

## ✨ 功能特性

<table>
<tr>
<td width="50%">

### 📦 订阅管理
- 多订阅源支持（Mihomo/Surge/通用格式）
- 内置 [Sub-Store](https://github.com/sub-store-org/Sub-Store) 解析订阅和转换节点格式，支持在线检测与更新
- 支持 Base64、YAML、URI 多种格式

### 🌐 节点管理
- 可视化节点管理
- 支持 SS/SSR/VMess/Trojan/Hysteria/Hysteria2
- 手动添加与编辑

### 📋 规则仓库
- 集中管理规则集（rule-providers）
- 批量导入 YAML 格式规则
- 连通性测试与自动关闭

</td>
<td width="50%">

### 🎯 策略组配置
- 手动选择 / 自动测速 / 故障转移 / 负载均衡
- 多维度节点筛选；正则预览由后端统一校验，支持 `(?i)` 忽略大小写，非法表达式显示原因并保留上次预览结果
- 支持引用其他策略组

### ⚡ 配置生成
- 一键生成 Mihomo YAML 配置
- 一键生成 Surge 配置
- 一键生成 Loon 配置，iOS 上可通过 `loon://import?sub=` 一键导入
- 一键生成 MosDNS 配置
- 实时预览 & 配置导入导出
- 导入 Mihomo / Surge / Loon / Shadowrocket 配置文件为新的配置空间：节点、订阅、规则集并入共享资源（相同定义自动复用），策略组与规则按本项目结构转换
- 多配置共享订阅、节点、聚合和规则仓库；资源引用、策略组、规则目标、拨号代理与生成参数分别保存
- 全局服务、访问令牌、WebDAV 和全量备份统一放在「系统设置」，Agent 按绑定配置下发
- [多配置、数据迁移与备份说明](doc/design/multi-config-management.md)

### 界面与可访问性
- 深浅主题均使用玻璃面板与柔和辉光；表单保持清晰底色，浮层保持高不透明度
- 桌面固定导航，移动端保留分组导航与配置切换；总览指标在窄屏按两列排列
- 支持键盘跳转到主要内容、可见焦点与系统减少动效偏好；长弹窗在视口内滚动

### 🔌 MCP 服务
- 内置 MCP 服务端（`/mcp` 端点）
- 工具覆盖共享资源、独立配置、系统设置与 Agent 管理
- Claude Desktop / Claude Code 等客户端可直接接入

### 🤖 Agent 远程管理
- 一键生成安装脚本
- 自动注册与心跳监控
- 远程推送配置、重启、日志查看
- 卡片按注册的服务类型显示 Mihomo、Surge 或 MosDNS；切换绑定配置不会改变服务类型标签

</td>
</tr>
</table>

---

## 🚀 快速开始

### Docker Compose 部署（推荐）

```yaml
version: '3.8'
services:
  config-flow:
    image: thsrite/config-flow:latest
    ports:
      - "80:80"
    volumes:
      - ./data:/data
    environment:
      - ADMIN_USERNAME=admin
      - ADMIN_PASSWORD=your_password
      - JWT_SECRET_KEY=your-secret-key
    restart: unless-stopped
```

访问 `http://localhost` 即可使用

> 💡 **提示**：生产环境请务必修改默认密码和 JWT 密钥；镜像已内置 Sub-Store（订阅解析和节点格式转换），无需另外部署，可在「系统设置 → 第三方依赖」检测并在线更新
>
> 从旧版升级：compose 中的 `sub-store` 服务和 `SUB_STORE_URL` 环境变量已不再使用，可以删除。

## 升级与数据安全

**升级前请确认目标镜像已包含旧版多配置迁移修复，不要仅凭 `latest` 标签判断。** 本节描述当前源码的迁移行为，已发布镜像是否包含修复应以发布记录和镜像摘要为准。

### 升级前检查

1. 记录当前镜像版本或摘要，以及容器 `/data` 对应的宿主机目录。
2. 停止 ConfigFlow 服务后，备份完整数据目录，包括根 `config.json`、`system.json`、`profiles/`、所有 `.bak` 和缓存文件。**旧版多配置不能只备份根 `config.json`，它可能是更早遗留的数据。**
3. 使用原 Compose 文件和原数据挂载升级；重要实例建议先复制数据，在隔离副本上验证配置和 Agent 绑定。

```bash
docker compose pull
docker compose up -d
```

> 仓库内的 Compose 文件位于 `docker/docker-compose.yml`。其中 `./data:/data` 的宿主机路径相对于 Compose 文件所在目录解析；从根目录的旧 Compose 改用此文件时，可能挂载到另一个 `data` 目录。请保留原文件位置，或明确使用原数据目录的绝对路径。

### 迁移行为

| 原数据格式 | 升级处理 |
|---|---|
| 原版无版本号的单文件配置 | 资源转为共享资源，策略与生成参数进入默认配置 |
| schema 2 多配置目录：`system.json`＋`profiles/<id>/config.json` | 优先读取系统索引和各配置文件，忽略残留旧根配置；保留配置 ID、规则顺序、生成参数及 Agent 绑定 |
| 当前 schema 5 配置 | 直接读取，不因旧目录仍存在而重复迁移 |
| 不完整数据或不支持的中间格式 | 明确报错，不静默创建默认模板或删除不支持字段 |

同 ID、同定义的资源会合并；同 ID、不同定义的资源会分别保留并重写引用，避免跨配置覆盖。提交前会在 `migrations/<时间戳-标识>/migration/` 保存旧配置主文件和备份文件快照。

### 升级后配置变少怎么办

- **不要重置或清理旧文件。** 先核对 `/data` 的实际挂载、启动日志和升级前备份。
- 若此前的错误升级已经写入 schema 5，修复版不会自动用旧文件覆盖它，以免丢失升级后新增的数据。
- 应从升级前的完整快照创建副本，使用含修复的版本迁移；核对全部配置、资源和 Agent 绑定后，再停服切换数据目录。不要只修改 `schema_version`，也不要直接删除生产根配置。

详细支持范围及恢复约束见[多配置、数据迁移与备份说明](doc/design/multi-config-management.md#升级备份与恢复)。

---

## 📖 文档

详细使用文档请查看：**[ConfigFlow 使用指南](doc/README.md)**

---

## 📸 截图预览

以下截图来自当前版本的本地隔离演示环境，使用虚构配置空间、节点与策略数据；不代表真实线上监控或生产配置。

<div align="center">
<h3>数据统计总览</h3>
<img src="doc/img/dashboard-demo.png" alt="ConfigFlow 新版数据统计总览：演示配置空间、引用节点、配置健康与运行状态" title="数据统计总览（演示数据）" width="100%" />
<br/><br/>
<h3>当前配置的策略组</h3>
<img src="doc/img/policy-groups-demo.png" alt="ConfigFlow 新版策略组：日常工作演示配置中的手动选择、自动测速及共享节点引用" title="策略组管理（演示数据）" width="100%" />
</div>

---

## 🏗️ 技术栈

| 层级 | 技术 |
|------|------|
| **后端** | Python 3.11 • Flask • PyYAML |
| **前端** | Vue 3 • TypeScript • Tailwind CSS 4 • Reka UI • Vite |
| **订阅解析** | [Sub-Store](https://github.com/sub-store-org/Sub-Store) |
| **部署** | Docker • Nginx • Supervisor |

---

## 📁 项目结构

```
config-flow/
├── backend/                # Python Flask 后端
│   ├── app.py             # 主应用入口
│   ├── common/            # 公共模块 (认证、配置)
│   ├── routes/            # API 路由
│   ├── mcp_server/        # MCP 服务端 (对外暴露 /mcp)
│   ├── converters/        # 配置生成器 (Mihomo/Surge/MosDNS)
│   ├── agents/            # Agent 管理
│   └── utils/             # 工具函数
├── frontend/              # Vue 3 前端
│   └── src/
│       ├── views/         # 页面组件
│       ├── components/    # 公共组件
│       └── api/           # API 调用
├── doc/                   # 项目文档
└── docker/                # 容器构建与部署配置
    ├── Dockerfile
    ├── docker-compose.yml
    ├── nginx.conf
    └── supervisord.conf
```

---

## 📄 许可证

本项目基于 [MIT License](LICENSE) 开源

---

## ❤️ 赞助支持

如果这个项目对你有帮助，欢迎赞助支持开发者持续维护！

<a href="https://ifdian.net/order/create?user_id=1ed9d800ada811f0bdbe52540025c377&remark=&affiliate_code=" target="_blank">
  <img src="https://img.shields.io/badge/爱发电-赞助支持-ff69b4?style=for-the-badge&logo=heart" alt="爱发电赞助">
</a>

### 🏆 发电排行榜

感谢以下用户的赞助支持！

<table>
<tr>
<td align="center">
<img src="https://pic1.afdiancdn.com/default/avatar/avatar-purple.png?imageView2/1/w/50/h/50" width="50" height="50" style="border-radius: 50%;" /><br/>
<sub><b>爱发电用户_740f4</b></sub>
</td>
</tr>
</table>

---

<div align="center">

**⭐ 如果觉得有用，请给个 Star 支持一下！**

Made with ❤️ by [thsrite](https://github.com/thsrite)

</div>
