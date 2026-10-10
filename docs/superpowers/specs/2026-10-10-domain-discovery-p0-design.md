# 域名发现 P0 设计：未覆盖域名与直连失败

## 1. 目标与非目标

**目标**
- 从部署了 Agent 的 mihomo 实例收集「真实访问过的域名 + 命中的规则 + 出口 + 是否直连失败」。
- 在 ConfigFlow 里列出**没有被任何规则覆盖（落到 MATCH）**的域名，按「直连失败」优先排序。
- 一键把域名（或其主域）写入**默认直连 / 默认代理规则集**（每个配置空间各指定一个）；也可以忽略。

**非目标（留给 P1/P2）**
- 主动探测（直连 vs 代理对比）、置信度打分、自动生效。
- 区域限制检测。
- Surge / Loon / 手机端流量。
- 保存 URL 路径、客户端 IP 等明细。

## 2. 数据流

```
mihomo (external-controller)
   │  GET /connections（轮询快照）
   │  GET /logs?level=warning（HTTP 流）
   ▼
go-agent: traffic 模块 —— 本地聚合 5 分钟窗口
   │  POST /api/agents/<id>/traffic-report（Bearer agent token）
   ▼
ConfigFlow 后端: TrafficStore（按 Agent、按天分桶，保留 7 天）
   │  GET /api/domain-discovery/domains（按配置空间合并 + 归类）
   ▼
前端「域名发现」页 ──► POST /api/domain-discovery/apply → rule_configs
```

开关由服务端控制：Agent 记录上的 `domain_discovery_enabled`，通过**心跳响应**下发（旧 Agent 忽略多出的字段，不需要改心跳请求体）。默认关闭，因为这本质上是上网记录。

## 3. Agent 侧（go-agent 新增 `domain_discovery.go`）

### 3.1 启用条件
- `cfg.ServiceType == "mihomo"`，且最近一次心跳响应里 `domain_discovery_enabled == true`。
- 关闭后立即停止采集、丢弃未上报的窗口。

### 3.2 定位控制接口
不新增 Agent 配置项，直接解析本机 mihomo 配置（`cfg.ConfigPath`，已有 `gopkg.in/yaml.v3`）：
- `external-controller`：`0.0.0.0` / `::` / 空主机改写为 `127.0.0.1`；为空则状态 `no_controller`。
- `secret`：作为 `Authorization: Bearer <secret>`。
- 每次 Agent 下发新配置或采集连续失败时重新解析（用户可能改了端口或 secret）。

模板里的 `log-level` 可能是 `warning`，不影响：`/connections` 与日志级别无关，`/logs?level=warning` 正好覆盖拨号错误。

### 3.3 采集器 A：连接（`/connections`）
- 每 2 秒拉一次快照，维护 `id → 连接` 的表；某个 id 从快照中消失，即视为连接结束，取其最后一次的值落入聚合。
- 取字段：`metadata.host`（为空则用 `metadata.sniffHost`）、`metadata.destinationPort`、`metadata.network`、`rule`、`rulePayload`、`chains`、`upload`、`download`、`start`。
  - `chains[0]` 为实际出口（节点名或 `DIRECT` / `REJECT`），`chains[len-1]` 为规则指向的策略。
- 没有域名的连接（嗅探失败、直连 IP）只计数，不入明细，用于展示「嗅探覆盖率」。
- 过滤：私有/保留 IP、`.lan` / `.local` / `.arpa`、目标为 fake-ip 段且无域名的连接。
- 已知局限：存活不到 2 秒的连接可能漏采。P0 做的是统计意义上的发现，可以接受；失败连接由采集器 B 兜底。

### 3.4 采集器 B：拨号失败（`/logs?level=warning`）
- 非 WebSocket 请求时 mihomo 以分块 JSON 流返回日志，一行一个 `{"type","payload"}`；断开后指数退避重连（1s→60s）。
- 只解析拨号失败行，提取：网络类型、出口代理、规则、目标 `host:port`、错误文本。日志格式随 mihomo 版本略有差异，正则要宽松，并用真实版本的日志样本做单测。
- 实测（v1.19.32）：DNS 解析失败同样以 warning 记录为 `dns resolve failed`；同一连接会重试拨号，按「源地址 + 目标」30 秒内去重。
- 错误文本归类为：`timeout` / `reset` / `refused` / `eof` / `dns` / `other`。

### 3.5 本地聚合与上报
- 聚合键：`(host, rule, outlet_kind, policy)`；`outlet_kind` ∈ `direct` / `proxy` / `reject`。
- 每 300 秒发出一个窗口；单窗口最多 2000 个键，超出时按 `fail + conns` 保留前 2000，其余并入 `dropped`。
- 内存上限由键数量上限保证；`/connections` 跟踪表最多 1 万条，超出时丢弃最旧的条目。
- 上报失败时保留最多 3 个窗口重试，再多就丢弃最旧的。

上报体（`POST /api/agents/<id>/traffic-report`）：

```json
{
  "schema": 1,
  "status": "running",            // running | no_controller | auth_failed | unreachable
  "mihomo_version": "v1.19.x",
  "window_start": "2026-10-10T10:00:00Z",
  "window_end":   "2026-10-10T10:05:00Z",
  "ip_only_conns": 37,
  "dropped": 0,
  "items": [
    {
      "host": "example.com",
      "port": 443,
      "network": "tcp",
      "rule": "Match",
      "rule_payload": "",
      "policy": "🐟 漏网之鱼",
      "outlet": "direct",
      "conns": 12,
      "fails": 9,
      "fail_kinds": {"timeout": 7, "reset": 2},
      "zero_dl": 3,                // 存活 ≥3s 但下行为 0 的连接数，仅作辅助信号
      "up": 10240, "down": 2048
    }
  ]
}
```

`status` 不是 `running` 时 `items` 为空，但仍然上报，便于前端展示 Agent 为何没有数据。

## 4. 服务端

### 4.1 上报接口
- `POST /api/agents/<agent_id>/traffic-report`：鉴权方式与心跳一致（常量时间比较 Bearer token）。
- 请求体上限 256 KB；严格校验字段白名单、类型、长度（host ≤ 253，策略名 ≤ 128，items ≤ 2000）；host 用现有的 `is_valid_domain` 校验。
- 只接受 `service_type == mihomo` 且开关已开启的 Agent，否则返回 409 并带上 `enabled:false`。
- 心跳响应追加 `domain_discovery_enabled`（`routes/agents.py` 的 `agent_heartbeat`）。

### 4.2 存储：`backend/agents/traffic_store.py`
仿照 `metrics_history.py`，每个 Agent 一个文件：`data/traffic/<agent_id>.json`。

```json
{
  "status": {"value": "running", "at": "...", "mihomo_version": "..."},
  "days": {
    "2026-10-10": {
      "ip_only_conns": 0,
      "hosts": {
        "example.com": {
          "first_seen": "...", "last_seen": "...",
          "by_route": {
            "Match|direct|🐟 漏网之鱼": {"conns": 0, "fails": 0, "fail_kinds": {}, "zero_dl": 0, "up": 0, "down": 0}
          }
        }
      }
    }
  }
}
```

- 保留 7 天，写入时顺带清理过期数据；每天最多 5000 个 host，超出时淘汰连接数最少的。
- 删除 Agent 时一并删除其文件；关闭开关时提供「清除已采集数据」操作。

### 4.3 查询与归类：`GET /api/domain-discovery/domains`
参数：`days`（1 / 7）、`agent_id`（可选）、`view`（`uncovered` / `failing` / `all`）。

1. 取当前配置空间（`resolve_profile_id`）下所有 Agent 的数据并合并。
2. **按主域归并**：用 `tldextract`（`suffix_list_urls=()`，只用包内自带的公共后缀列表，不联网）求出可注册域，如 `a.b.example.co.uk` → `example.co.uk`；前端以主域为一行，可展开看各子域。
3. **归类**（同一域名可同时属于多类）：
   - `uncovered`：存在 `rule == "Match"` 的连接。
   - `failing`：直连 `fails / (conns + fails) ≥ 0.5`，并且 `fails ≥ 3`（失败的拨号不会出现在连接列表里，所以分母是成功 + 失败）。
   - `pending`：当前 `rule_configs` 已能匹配该域名（规则刚加、还没部署），显示「已有规则，待部署生效」。
   - `ignored`：命中忽略列表，默认不显示。
4. 排序：`failing` 优先，然后按失败次数、连接数排列。

`pending` 判定复用 `/rules/match-test` 的逐条匹配逻辑：抽成 `backend/utils/rule_matcher.py` 中的 `RuleConfigMatcher`，规则集内容按需加载并缓存；域名发现只读本地缓存，不发网络请求。

返回示例（按主域）：

```json
{
  "agents": [{"id": "...", "name": "...", "status": "running", "last_report": "..."}],
  "sniff_coverage": 0.93,
  "items": [{
    "domain": "example.com",
    "tags": ["uncovered", "failing"],
    "conns": 40, "fails": 31, "fail_kinds": {"timeout": 25, "reset": 6},
    "routes": [{"rule": "Match", "policy": "🐟 漏网之鱼", "outlet": "direct", "conns": 40}],
    "hosts": [{"host": "www.example.com", "conns": 30, "fails": 25}],
    "agents": ["home-gw"],
    "last_seen": "...",
    "pending_rule": null
  }]
}
```

### 4.4 默认规则集与操作接口
每个配置空间在 `domain_discovery` 字段（新增的配置空间字段，随导入导出）里保存：`direct_ruleset`、`proxy_ruleset`（规则库条目 ID）和 `ignored`。

- 默认规则集必须是**内容型**、`classical` 或 `domain` 格式的规则库条目；策略由它在策略规则里的引用决定。
- `GET/PUT /api/domain-discovery/settings`：读取 / 设置默认规则集，返回每个规则集是否在当前配置启用且位于兜底规则之前（`active`）。
- `POST /api/domain-discovery/rulesets/init` `{"proxy_policy": "PROXY", "targets": ["direct","proxy"]}`：在规则库新建 `域名发现-直连` / `域名发现-代理`（重名自动加后缀），以 `DIRECT` / 所选策略组引用到第一条 `MATCH` 之前，并设为默认。
- `POST /api/domain-discovery/apply` `{"items":[{"value":"example.com","rule_type":"DOMAIN-SUFFIX","target":"proxy"}]}`：
  - 按目标规则集格式追加行（classical → `DOMAIN-SUFFIX,x`；domain → `+.x` / `x`）；
  - 规则集里已有同值或上级 `DOMAIN-SUFFIX` 时跳过；
  - 规则集未启用或在兜底规则之后时返回 warning。
- `POST / DELETE /api/domain-discovery/ignore`：维护忽略列表。
- `PUT /api/agents/<id>/domain-discovery` `{"enabled": bool}`；`POST .../clear` 清除该 Agent 的数据。

### 4.5 免部署生效
上报响应带 `rule_providers: {规则集名: 内容 md5}`（只含已启用的默认规则集）。Agent 记住上次的摘要，变化时调用 Mihomo `PUT /providers/rules/<name>` 立即重新拉取。前提是规则集已经随配置部署过一次；Mihomo 返回 404 时等下次部署后再刷新。

## 5. 前端

新增页面 `/domain-discovery`「域名发现」，在导航「当前配置」分组里，位于「策略规则」之后。

- **采集设备**：列出当前配置下的 Mihomo Agent，显示采集状态、最近上报时间、Mihomo 版本，提供开关和「清除数据」。Agent 版本低于 1.4.0-go 时提示升级。开关放在这里而不是 Agent 页，便于就地操作。
- **默认规则集**：分别选择直连 / 代理规则集，标出「未在当前配置启用」等问题；缺少时提供「一键创建」，弹窗里选择代理策略组。
- **筛选**：`未覆盖` / `直连失败` / `全部` / `已忽略`，时间范围 24 小时 / 7 天，搜索，以及嗅探覆盖率。
- **列表**（按主域）：标签、连接数、直连失败（含错误类型）、当前走向（规则 → 策略 · 出口）、最近出现。行操作是「加入代理」「加入直连」「忽略」；展开后可对单个子域写 `DOMAIN`。支持多选批量操作。

## 6. 隐私与安全
- 默认关闭，按 Agent 单独开启；开启时提示会记录所访问的域名。
- 只存域名、端口、统计数，不存 URL 路径、客户端 IP、进程名。
- 7 天自动过期，可一键清除。
- 上报接口与心跳使用同一套 token 鉴权与请求体上限；查询接口走 `require_auth`。
- Agent 只访问本机的控制接口，secret 不上报。

## 7. 兼容性
- 旧 Agent：不认识心跳响应的新字段，不会上报；页面显示「Agent 版本过低」，需要提供最低版本号。
- 新 Agent + 旧服务端：上报返回 404 时进入休眠，每 30 分钟重试一次，不影响心跳。
- 心跳请求体不变（服务端对心跳字段做白名单校验，加字段会让旧服务端拒收整个心跳）。

## 8. 测试（在 10.0.0.49 上跑）
- Go：
  - 日志行解析，使用多个 mihomo 版本的真实样本；
  - `/connections` 快照差分（连接结束判定、无域名连接计数）；
  - 窗口上限与 `dropped`；
  - controller 地址改写与 secret；
  - 用假 controller（httptest）做端到端测试。
- Python：
  - 上报校验（越界、未知字段、超大请求体、错误 token）；
  - TrafficStore 分桶、过期清理、单日 host 上限；
  - 主域归并、归类与 `pending` 判定；
  - `apply` 的插入位置（MATCH 之前）、去重、策略校验；
  - `RuleConfigMatcher` 重构后 `/match-test` 行为不变。
- 集成：在 49 上起 mihomo + Agent，访问几个直连不通的域名，确认能被标为 `failing`。完成后清理。

## 9. 已验证（mihomo v1.19.32，10.0.0.49）
1. `/connections` 字段：`metadata.host`、`metadata.sniffHost`、`chains`（`[出口, …, 策略]`）、`rule`（兜底为 `Match`）、`rulePayload`。
2. 拨号失败：`[TCP] dial <策略> (match <规则>/<内容>) <源> --> <host>:<port> error: ...`，warning 级别，含 DNS 解析失败。
3. `/logs?level=warning` 在非 WebSocket 请求下按行流式返回 JSON。
4. 端到端：真实 mihomo + 后端 + Agent 采集代码，直连失败 → 列表标记 → 加入代理 → 上报触发 rule-provider 刷新 → mihomo 改为 `match RuleSet/域名发现-代理`，全程无需重新部署。

## 10. 任务拆分
1. 后端：规则匹配抽成 `RuleConfigMatcher`（按需加载并缓存规则集，可批量查询）+ 测试。
2. 后端：上报接口、TrafficStore、心跳响应字段、开关接口。
3. 后端：查询 / apply / ignore 接口。
4. Agent：controller 定位、两个采集器、聚合与上报，版本号升级。
5. 前端：域名发现页 + Agent 开关。
6. 文档：`doc/module/` 新增模块说明。
