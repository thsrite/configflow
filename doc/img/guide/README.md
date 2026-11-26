# 安装指南截图说明

下表的 JPEG 来自当前仓库的真实 Vue 前端与 Go 面板（`server/cmd/configflow`）。截图在测试服务器运行，使用独立临时数据目录和合成演示数据；没有读取正式配置，没有使用 API mock，没有真实 Agent、路由器或高可用组。图中地址、名称、端口和目录只是演示填写，不代表设备已安装、已发布或已配对。

| 图片 | 场景 | 需要注意 |
| --- | --- | --- |
| `panel-header-docs.jpg` | 管理面板顶栏 | 右侧依次为深浅外观、系统设置、通知中心与“查看文档”（纸张图标，新标签页打开使用手册） |
| `first-run-router.jpg` | 首次向导默认的 Mihomo + OxiDNS 分流 | 尚未创建方案 |
| `first-run-gateway.jpg` | 改选 Linux 网关 | 第 5 步随之变成客户端设置 |
| `panel-service-settings.jpg` | 服务域名、访问令牌所在位置 | 示例地址使用 RFC 5737 文档地址（`192.0.2.x`）；独立开发服务不支持容器 Web 监听端口设置，这不是官方容器里的端口状态 |
| `agent-shell-single-default.jpg` | 普通 Shell 单独 Mihomo | 两个勾选均保留默认关闭；名称和端口是示例 |
| `agent-shell-single-choices.jpg` | 补装 Mihomo 核心并跳过管理 IPv4 转发 | 两项由截图脚本手动勾选，不是默认状态 |
| `agent-shell-aio.jpg` | Shell 同机 Mihomo + OxiDNS | 安装核心默认未勾选 |
| `agent-docker-single.jpg` | Docker 单独 Mihomo | 高级配置展开，显示默认 bridge |
| `agent-docker-aio.jpg` | Docker All-in-One | 两项 Agent 端口；未创建容器 |
| `oxidns-shell-ha-prepare.jpg` | Shell OxiDNS HA 准备入口 | 安装核心默认勾选；未执行安装 |
| `oxidns-docker-ha-prepare.jpg` | Docker HA 准备环境 | 默认 Host + 独立 HA 容器，使用 RFC 5737 服务地址 |
| `oxidns-docker-ha-host.jpg` | Host 主机字段 | 主机地址使用 RFC 5737 文档地址，目录是演示填写 |
| `oxidns-docker-ha-macvlan.jpg` | macvlan 主机字段 | 容器地址、网段和网关使用 RFC 5737 文档地址，网卡是演示填写 |
| `oxidns-ha-pair-empty.jpg` | 没有设备时的验证与配对页 | 如实保留空列表／等待状态；不是高可用验证成功 |
| `agents-backup-restored-desktop-light.jpg` | 从不含运行记录的备份恢复后的 Agent 页（桌面浅色） | 通过 `/api/config/import` 导入两台合成演示 Agent（RFC 5737 地址），触发顶部提示横幅与卡片「待重新发布」标记 |
| `agents-backup-restored-mobile-dark.jpg` | 从不含运行记录的备份恢复后的 Agent 页（手机 390px 深色） | 同上，深色主题与 390px 手机视口 |
| `aggregation-subscription-cache.png` | 聚合预览中查看单个订阅 | 只读隔离数据里的订阅缓存；来源设为「仅手动」，没有请求上游 |
| `client-devices-side-entries.png` | 客户端设备的旁路 DNS 入口提醒 | 使用 RFC 5737 网段占位和合成设备 |
| `generate-result-replay.png` | 默认静止外观下，生成步骤与配置文字依次展开 | 最终配置已返回，展示回放过程；并非后端实时阶段进度，可点「立即显示」跳过 |

| `mihomo-base-settings-overview.png` | Mihomo 基础配置的基础参数属性表，所有值平铺 | 初始模板；控制面板标出「仍为默认密码」的提醒；为拍下整张表，临时取消了编辑窗口的高度限制 |
| `mihomo-base-settings-controller.png` | 控制面板分组，打开「监听范围」小窗 | 密码为演示填写，默认隐藏 |
| `mihomo-base-settings-port-conflict.png` | 两个端口相同 | 由截图脚本故意填写冲突端口，用于展示保存被阻止 |
| `mihomo-base-settings-dns.png` | DNS 上游切换到「国内外分开解析」，打开「默认 DNS」列表 | 只改草稿，未发布 |
| `mihomo-base-settings-sniffer.png` | 域名嗅探分组：HTTP / TLS / QUIC 端口与开关 | 脚本把 TLS 端口改为 443, 8443-8445，只改草稿 |
| `mihomo-base-settings-managed.png` | 接管与 DNS 监听（只读） | 未启用 Linux 网关、非向导配置空间 |
| `mihomo-base-settings-yaml-error.png` | YAML 无法解析时隐藏属性表，只显示原因 | 脚本在编辑器中输入错误 YAML |
| `mihomo-base-settings-mobile-dark.png` | 手机 390px 深色 | 分组名在上，格子两列 |
| `mihomo-easytier-settings.png` | EasyTier 组网卡片：生成方式、网络、监听三组 | 示例网络 home-lab、文档用地址（203.0.113.10、2001:db8::10），只保存草稿 |
| `mihomo-easytier-settings-mobile-dark.png` | EasyTier 组网卡片，手机 390px 深色 | 同上 |

JPEG 由 [capture-guide.mjs](../../../scripts/docs/capture-guide.mjs) 采集，质量 82，由浏览器直接输出。机器可读记录见 [capture-report.json](capture-report.json)。

客户端设备的旁路入口 PNG 由 [device_policy_side_entries_browser.mjs](../../../scripts/tests/device_policy_side_entries_browser.mjs) 单独采集。脚本使用真实 Vue 前端、隔离 Go 面板和合成设备，同时核对测速入口隐藏、DNS 策略可编辑及手机页无横向滚动。在测试服务器构建前端后，设置 `CF_PLAYWRIGHT` 为下方安装的 Playwright `index.mjs` 路径，再运行该脚本。

聚合缓存预览、Mihomo 基础参数、EasyTier、Mihomo IPv6 模式和生成结果回放的既有截图来自切换前的独立浏览器验收。采集脚本和记录可在切换候选提交 `7feb6e125` 中查阅。

## 在测试服务器重现

从任务 worktree 运行，不在本机启动服务：

```bash
cf-test typecheck
rt --no-sync -- npm install --prefix /tmp/configflow-guide-browser --no-save --package-lock=false playwright@1.64.1
rt --no-sync -- /tmp/configflow-guide-browser/node_modules/.bin/playwright install chromium
rt --no-sync -e PLAYWRIGHT_MODULE=/tmp/configflow-guide-browser/node_modules/playwright/index.mjs --back doc/img/guide -- node scripts/docs/capture-guide.mjs
```

只重拍部分图片时加 `-e CAPTURE_ONLY=名称[,名称]`（不含 `.jpg`），脚本只写这些图片，并在 `capture-report.json` 中更新或追加对应条目、记 `recapturedAt`。2026-11-21 `oxidns-docker-ha-prepare.jpg` 按此重拍（镜像输入框改为默认留空、与面板版本配套）；2026-11-24 切换为 Go 面板采集并重拍了 `panel-header-docs.jpg`（顶栏新增通知中心图标）与 `panel-service-settings.jpg`（改用 RFC 5737 示例地址），同时补截了 `agents-backup-restored-desktop-light.jpg` 与 `agents-backup-restored-mobile-dark.jpg`。

2026-11-24 脱敏更新重拍了 `oxidns-docker-ha-prepare.jpg`、`oxidns-docker-ha-host.jpg` 和 `oxidns-docker-ha-macvlan.jpg`，服务地址、主机地址和网段均使用 RFC 5737 文档地址。

第一条命令同步代码并准备共用依赖；后续命令不再同步，避免把本机的依赖软链接覆盖到远端。Playwright 安装在测试服务器的独立临时目录，不修改项目依赖；`1.64.1` 是本批图片实际使用的版本。脚本优先使用 `PLAYWRIGHT_MODULE`，未指定时尝试加载当前项目可解析的 `playwright`。后端直接从 `server/cmd/configflow` 构建并启动 Go 面板（无需 Python），也可通过 `DOCS_SERVER_BIN` 指定已构建的二进制路径。

不再需要这套截图依赖时，可在测试服务器删除本次创建的独立目录：

```bash
rt --no-sync -- rm -rf /tmp/configflow-guide-browser
```

前端 Vite 与 Go 面板默认均在 `0.0.0.0` 上自动选择空闲端口，也可通过 `DOCS_WEB_PORT`、`DOCS_API_PORT` 指定固定端口（启动前检查端口空闲）。脚本仅操作表单与合成备份导入，不执行设备安装／发布／配对按钮。结束或出错时关闭浏览器、前后端服务，并删除本次临时数据与临时二进制。
