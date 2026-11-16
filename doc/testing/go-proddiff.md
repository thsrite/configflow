# Go 生产快照结构对比

从仓库根目录运行（所有构建和测试均在测试服务器）：

```bash
rt -- server/scripts/proddiff.sh
rt -- server/scripts/proddiff.sh ~/cf-prod-snapshot/20261116/data
```

默认使用第二条命令的快照目录，Python 为 `~/cf-test/.venv/bin/python`。
可通过 `CONFIGFLOW_DIFF_PYTHON` 指定相同依赖环境的解释器。
退出码：0 表示没有新增差异和覆盖回退；1 表示发现回归；2 表示运行环境或框架失败。
基线差异消失时打印 `stale` 并提示更新，不阻止检查。

## 隔离与认证

脚本先构建 Go，然后进入新的 user/net/mount/PID namespace。
命名空间内只启用 lo；程序用接口 API 再次确认只有 loopback。
快照以 bind mount 挂为只读，程序验证只读挂载标志，并在运行前后对文件内容做摘要检查。
拒绝含符号链接的快照，防止复制或访问越出快照目录。

Python、Go 和比较程序都在同一个断网命名空间。两端各加载快照的一份临时副本；
无论正常完成还是异常退出都清理副本。SIGINT/SIGTERM 触发清理；PID namespace
确保服务退出后不会遗留子进程。直接 SIGKILL/主机断电无法执行文件清理，遗留副本仅会位于私有目录。
没有外部设备假服务，也不会放开网络。真实设备不可达时保留并比较原始 HTTP 失败响应；
报告另外统计两端连接失败数。HTTP 客户端超时为 20 秒，单边无响应额外判失败。

两端使用相同的 `ADMIN_USERNAME`、`ADMIN_PASSWORD`、`JWT_SECRET_KEY` 测试环境变量，
各自调用正常 `/api/auth/login` 获取 JWT。它们本来就支持环境变量账号；
不读取生产密码，不更改快照账号，也不添加认证后门。Agent 专用配置拉取使用副本里的 Agent token，
只在断网请求和服务器私有报告中使用。

Python 通过测试启动器在导入 app 之前禁用具名启动函数，正常加载持久历史。
Go 构建的是 `cmd/configflow`，使用临时 `-overlay` 文件和
`-tags configflow_proddiff`，仅替换 `lifecycle.Start` 为不启动循环的测试版本，
并从实际 Router 导出路由清单。临时文件带相同 build tag，不能被普通构建误用。
仓库中的 Go 业务源码没有改动。直接构建生产入口仍保留全部后台功能。

当前 Python 禁用 10 项：DNS follow、OxiDNS HA publication、IPv6 verification、
Agent rule sync、metrics flush、OxiDNS ranking、domain discovery、scheduled refresh、
router rule sync、Sub-Store idle monitor。
当前 Go 禁用 5 个已注册循环：metrics-history-flush、oxidns-ranking-history、
sub-store-idle、subscription-cache-prune、scheduled-refresh。
所有已知定时启动入口均已关闭；按请求执行的计算、连接尝试仍在隔离网络中运行。
Python 启动迁移和缓存整理仍正常执行，但只作用于临时副本。

## 覆盖与比较

实现位于 `server/tools/proddiff/`：这是离线验收工具，不是服务内部依赖。
采用 Python 是为了复用测试服务器已有的 SQLite、PyYAML 和 Flask 参考运行环境，
不引入 Go 业务依赖或新的依赖包。

- 从 `server/parity/routes.tsv` 选择所有 GET，不相信表中的 todo/done 状态。
  是否“Go 尚未实现”由测试构建实际注册的 method/template 决定；不会把业务 404 当作未实现。
- 每个配置空间均作为请求作用域，展开保存的 Agent、routers.items、订阅、聚合、规则库、
  部署记录、预览、安装会话、安装脚本 token。读取配置 SQLite 和快照元数据时只使用副本。
- 安装脚本和 Docker 命令按保存的 Agent 参数渲染；配置拉取包含 Mihomo、Surge、Loon、
  MosDNS、OxiDNS；生成、预览、导出、provider、Agent 配置均由各自公开入口覆盖。
- 非 GET 使用经过逐条选择的 `proddiff-readonly-post.json`。
  新增 POST 不会因为名字含 preview 或 generate 就自动执行。
  生成接口可在临时副本中保存生成产物，不能提交配置或应用设备变更。
  HA guide 只执行 `action=inspect`，避免签发新安装能力。
- JSON 深度比较，YAML 解析后比较；JSON 内的 content/config/compose 也解析配置内容。
  ZIP 按成员读取后做同样比较。文本统一 CRLF/CR 和末尾换行后逐行比较。
  状态码、Content-Type 独立比较。差异只输出模板、键路径、类型。
- `proddiff-normalize.json` 逐项声明精确路由、精确路径和原因，不支持通配符。
  当前仅处理独立进程的日志路径/统计和按请求计算的默认查询起点；其余内容继续比较。
- 每次运行都会篡改一份真实 Go 响应的状态，再交给同一个比较器。
  自检必须检测到该差异，否则整轮失败。另有合成测试覆盖深层值、缺键、类型、
  Content-Type、YAML 结构、文本换行、精确规范化、动态键脱敏和实体枚举。

没有保存实例的路径参数用合成 missing 参数请求错误分支，并在 coverage 中明确计数。
进程内探测 job、设备端 DNS query record 等无法从离线快照恢复，只覆盖错误分支。
现有预览也可能已过期。未接入 Go 的路由单独计数，不发出无意义的双端比较，
不把它们计为通过；当前数据无法证明这些尚未实现功能的结果一致。
POST 草稿输入来自快照；需要额外用户意图的接口可能走校验失败分支。
这是一份真实保存状态的回归检查，不替代覆盖每个写操作和每种请求分支的领域验收。

## 基线和私有报告

`server/parity/proddiff-baseline.txt` 每行包含 method/template、键路径、差异类型、原因或 open。
出现基线之外的新项目即失败；基线项目消失则提示更新。
`proddiff-coverage.txt` 记录之前实际比较的路由，防止路由移除后被“尚未实现”掩盖。
需要人工审阅后更新基线时执行：

```bash
rt --back server/parity/proddiff-baseline.txt \
   --back server/parity/proddiff-coverage.txt \
   -- server/scripts/proddiff.sh --record-baseline
```

此命令是显式重录模式，普通门禁不能加 `--record-baseline`。
取回的两个文件仅有路由模板和键路径/类型，不含数据值。

每次运行的报告位于测试服务器 `~/cf-prod-snapshot/runs/run-*/`，目录权限 700。
`summary.json`、`coverage.json`、`differences.tsv`、`report.md` 是脱敏结果；
`responses.jsonl` 和服务日志可能含真实请求参数、响应和凭据，只能留在该私有目录，
不能复制到仓库、交接回复或公开报告。框架错误详情也只写入私有 `error.txt`。
普通运行不会在项目中生成未跟踪文件。

当前基线按去重的模板/键路径计数：agents 的 value 差异 3 项，config 的 value 差异 2 项，
routers 的已比较差异 0 项（其路由尚未接入，不能理解为已验证）。
Agent 差异是三个设备日志/日志设置入口的断网失败消息文本；config 差异是独立进程日志行内容。
未修改这些业务行为。详情定位见基线文件。

## 第二阶段建议回放的写接口

需要同时检查 SQLite、文件和隔离假设备收到的请求，至少包括：

- `POST /api/profiles`、`POST /api/profiles/<profile_id>/clone`
- `POST /api/config/import`、`POST /api/subscriptions/<sub_id>/fetch`
- `POST /api/rules/compose`、`PUT /api/agents/<agent_id>`
- `POST /api/agents/<agent_id>/heartbeat`、`POST /api/agents/<agent_id>/push-config`
- `POST /api/routers/<router_id>/apply`、`POST /api/routers/<router_id>/routes/apply`
- 安装能力签发、安装登记、部署 activate/rollback/recover 和 settings 保存入口

上述建议也写在每轮私有 report.md 的末尾。第一版不执行这些写操作。
