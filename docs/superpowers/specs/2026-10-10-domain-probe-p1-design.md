# 域名发现 P1 设计：主动探测、置信度与自动采纳

在 P0（被动发现未覆盖 / 直连失败的域名）基础上，用**主动探测**判断域名该走直连还是代理，给出带置信度的建议，可选自动采纳，并对已加入的域名定期复检。

## 1. 探测方式：借 Mihomo 自己的延迟测试

探测不在 Agent 主机上直接发请求（开启 tun 的主机上直连请求会被 Mihomo 截走），而是调用本机 Mihomo 控制接口：

```
GET /proxies/{DIRECT 或 策略组}/delay?url=<目标>&timeout=<ms>
```

- 直连路径用 `DIRECT`，代理路径用**默认代理规则集引用的策略组**，组内用当前选中的节点，与真实流量一致。
- 已验证（mihomo v1.19.32）：可达返回 `200 {"delay": n}`；超时 `504 Timeout`；DNS 等其他错误 `503`；任何 HTTP 响应都算可达。只会写入延迟历史，不改变组的选择。
- 目标 URL：按该域名流量里最常见的端口决定，443 → `https://host/`，80 → `http://host/`，其他 → `https://host:port/`。

## 2. Agent（1.5.0-go）

新增 `POST /api/domain-probe`（Agent token 鉴权）：

```json
{"targets": [{"host": "www.example.com", "url": "https://www.example.com/"}],
 "paths": ["DIRECT", "🚀 节点选择"], "attempts": 3, "timeout_ms": 5000}
```

- 上限：targets ≤ 20、paths ≤ 3、attempts 1–5、timeout 1000–10000 ms；host 必须是合法域名，URL 只允许 http/https 且主机与 host 一致。
- 每个「目标 × 路径」依次探测 `attempts` 次；前两次结果一致（都通或都不通）时提前结束。全局最多 6 个并发。
- 返回每个目标、每条路径的 `ok`、`total`、`delays`、`errors{timeout, error}`；控制接口不可用时返回 503。

## 3. 判定与置信度（服务端纯函数）

| 判定 | 条件 | 建议 | 置信度 |
|---|---|---|---|
| `needs_proxy` | 直连全失败，代理成功率 ≥ 50% | 代理 | 70；探测完全一致 +15；被动数据也是「直连失败」+15 |
| `direct_only` | 直连成功率 ≥ 50%，代理全失败 | 直连 | 70；完全一致 +15 |
| `both_ok` | 两条路径都 ≥ 50% | 直连延迟 ≤ 代理 × 1.5 时建议直连，否则建议代理 | 60 / 50 |
| `proxy_down` | 都失败，且代理路径对其他域名也全失败 | 无（提示检查节点） | — |
| `unreachable` | 都失败 | 无 | — |
| `flaky` | 其他 | 无 | — |

置信度上限 99。每条建议附带可读的理由列表。

## 4. 服务端

- **ProbeStore**：`data/probes/<agent_id>.json`，按 host 保存最近一次结果（含时间、路径名、各路径统计、判定），保留 30 天。
- **TrafficStore** 增加每个 host 的端口计数，用于选择探测 URL（旧数据缺省 443）。
- **探测任务**：`POST /api/domain-discovery/probe {"domains": [...]}` 创建后台任务，返回 `job_id`；`GET /api/domain-discovery/probe/<job_id>` 查进度。每个域名选「见过它、已开启、版本支持、最近在线」的 Agent，按 Agent 分批（每批 ≤ 20 个 host）调用。单个域名最多探测流量最多的 3 个子域。
- **列表**：`/domains` 的每个子域附带 `probe`，主域附带汇总的 `suggestion {target, confidence, verdict, reasons, checked_at}`（取置信度最高的子域）。
- **采纳**：沿用 `/apply`；每次写入都记录到配置空间 `domain_discovery.history`（值、类型、目标、规则集、来源 manual/auto、时间、当时的判定与置信度），最多保留 1000 条。
- **撤销**：`POST /api/domain-discovery/undo {"value", "target"}`：从规则集删除对应行，并删除这条历史记录。

## 5. 自动探测与自动采纳

配置空间设置（`domain_discovery`）：

- `auto_probe`（默认关）：后台每 10 分钟一轮。挑出未覆盖且未被忽略的域名中，从未探测或探测已超过 3 天的，按「直连失败优先、失败数、连接数」取前 20 个探测。
- `auto_apply`（默认关）+ `auto_apply_min_confidence`（默认 90）：探测后置信度达标、目标是直连或代理的建议自动写入默认规则集，来源记为 `auto`。
- **复检**：同一轮里，对加入超过 30 天、且 30 天内没复检过的历史记录重新探测。代理条目如果变成 `both_ok` 且直连不慢，或者两条路径都不通，就标记「建议移除」。只提示，不自动删除。

后台线程在应用启动时启动，用 `data/probes/.auto.lock` 文件锁保证只有一个进程运行。

## 6. 前端

- 列表新增「探测建议」列：`建议代理 92`、`建议直连 75`、`两边都不通` 等，悬停显示理由与探测时间。
- 行操作「探测」；批量操作「探测所选」「采纳建议」。探测中显示进度。
- 默认规则集卡片下新增「自动化」：自动探测开关、自动采纳开关和置信度阈值（80 / 90 / 95）。
- 新增「已加入」视图：历史记录（来源、时间、判定），复检建议移除的高亮显示，提供「撤销」。

## 7. 兼容性

- 探测需要 Agent 1.5.0-go；低版本 Agent 不参与探测，页面给出提示。
- 旧的 `domain_discovery` 设置缺少新字段时按默认值处理。

## 8. 测试

- Go：请求校验、提前结束、并发上限、对假控制接口的端到端。
- Python：判定与置信度表驱动测试、ProbeStore、任务调度（Agent 选择、分批、失败处理）、自动采纳阈值、撤销、复检标记。
- 实机（49）：两个 mihomo（B 作为可用 socks5 节点，A 用 hosts 把测试域名指向不可达地址模拟直连被封），验证 needs_proxy → 自动采纳 → 规则生效。

## 9. 实现记录

- 已在 49 上用两个 mihomo v1.19.32 实测：A 用 `nameserver-policy` 把测试域名指向不存在的 DNS（模拟污染），B 作为可用的 socks5 节点。流程为：真实流量直连失败 → 探测得到 `needs_proxy 99` → 采纳 → Agent 刷新 rule-provider → 同一请求命中 `RuleSet(域名发现-代理)` 并返回 200。
- 实测发现并修复 P0 遗留的竞态：规则集摘要原来按配置内容计算，而配置先提交、`/rules/local` 的缓存文件后写入。若上报恰好落在两者之间，Agent 会让 Mihomo 拉到旧文件，并记成「已刷新」。现在改为按实际下发的文件计算摘要。
- 不要用 Mihomo 的静态 `hosts` 模拟被墙：静态 hosts 对代理流量同样生效，会把解析后的 IP 发给代理节点，与真实的 DNS 污染不一致。
- 自动探测线程只在 `python -m backend.app` 作为服务进程运行时启动，用 `data/probes/.auto.lock` 文件锁保证单实例。
