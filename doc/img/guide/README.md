# 安装指南截图说明

这些图片来自当前仓库的真实 Vue 前端与 Flask API。截图在测试服务器运行，使用独立临时数据库；没有读取正式配置，没有使用 API mock，没有真实 Agent、路由器或高可用组。图中地址、名称、端口和目录只是演示填写，不代表设备已安装、已发布或已配对。

| 图片 | 场景 | 需要注意 |
| --- | --- | --- |
| `panel-header-docs.jpg` | 管理面板顶栏 | 右侧纸张图标为“查看文档”，在新标签页打开使用手册 |
| `first-run-router.jpg` | 首次向导默认的 Mihomo + OxiDNS 分流 | 尚未创建方案 |
| `first-run-gateway.jpg` | 改选 Linux 网关 | 第 5 步随之变成客户端设置 |
| `panel-service-settings.jpg` | 服务域名、访问令牌所在位置 | 独立开发服务不支持容器 Web 监听端口设置；这不是官方容器里的端口状态 |
| `agent-shell-single-default.jpg` | 普通 Shell 单独 Mihomo | 两个勾选均保留默认关闭；名称和端口是示例 |
| `agent-shell-single-choices.jpg` | 补装 Mihomo 核心并跳过管理 IPv4 转发 | 两项由截图脚本手动勾选，不是默认状态 |
| `agent-shell-aio.jpg` | Shell 同机 Mihomo + OxiDNS | 安装核心默认未勾选 |
| `agent-docker-single.jpg` | Docker 单独 Mihomo | 高级配置展开，显示默认 bridge |
| `agent-docker-aio.jpg` | Docker All-in-One | 两项 Agent 端口；未创建容器 |
| `oxidns-shell-ha-prepare.jpg` | Shell OxiDNS HA 准备入口 | 安装核心默认勾选；未执行安装 |
| `oxidns-docker-ha-prepare.jpg` | Docker HA 准备环境 | 默认 Host + 独立 HA 容器 |
| `oxidns-docker-ha-host.jpg` | Host 主机字段 | 主机地址、目录是演示填写 |
| `oxidns-docker-ha-macvlan.jpg` | macvlan 主机字段 | 独立容器地址、网卡、网段和网关是演示填写 |
| `oxidns-ha-pair-empty.jpg` | 没有设备时的验证与配对页 | 如实保留空列表／等待状态；不是高可用验证成功 |
| `aggregation-subscription-cache.png` | 聚合预览中查看单个订阅 | 只读隔离数据里的订阅缓存；来源设为「仅手动」，没有请求上游 |

采集脚本：[capture-guide.mjs](../../../scripts/docs/capture-guide.mjs)。机器可读记录见 [capture-report.json](capture-report.json)。图片为浏览器直接输出的 JPEG，质量 82，未经后期拼接或生成。

聚合缓存预览的 PNG 由 [aggregation_cache_preview_browser.mjs](../../../scripts/tests/aggregation_cache_preview_browser.mjs) 单独采集，使用真实页面和 API、临时数据及演示节点；脚本验证预览不产生上游请求或拉取记录，并检查来源缓存更新、缓存缺失和手机布局。准备好下述依赖后，在测试服务器运行 `rt --no-sync --back doc/img/guide/aggregation-subscription-cache.png -- node scripts/tests/aggregation_cache_preview_browser.mjs`。

## 在测试服务器重现

从任务 worktree 运行，不在本机启动服务：

```bash
cf-test typecheck
rt --no-sync -- npm install --prefix /tmp/configflow-guide-browser --no-save --package-lock=false playwright@1.64.1
rt --no-sync -- /tmp/configflow-guide-browser/node_modules/.bin/playwright install chromium
rt --no-sync -e PLAYWRIGHT_MODULE=/tmp/configflow-guide-browser/node_modules/playwright/index.mjs --back doc/img/guide -- node scripts/docs/capture-guide.mjs
```

第一条命令同步代码并准备共用依赖；后续命令不再同步，避免把本机的依赖软链接覆盖到远端。Playwright 安装在测试服务器的独立临时目录，不修改项目依赖；`1.64.1` 是本批图片实际使用的版本。脚本优先使用 `PLAYWRIGHT_MODULE`，未指定时尝试加载当前项目可解析的 `playwright`。Python 使用测试服务器的共用环境，也可通过 `DOCS_PYTHON` 指定路径。

不再需要这套截图依赖时，可在测试服务器删除本次创建的独立目录：

```bash
rt --no-sync -- rm -rf /tmp/configflow-guide-browser
```

默认前端 `15671`、API `15672`，启动前检查端口空闲；可通过 `DOCS_WEB_PORT`、`DOCS_API_PORT` 修改。脚本仅操作表单，不执行安装／发布／配对按钮。结束或出错时关闭浏览器、前后端服务，并删除本次临时数据。
