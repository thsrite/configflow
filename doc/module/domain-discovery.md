# 域名发现

域名发现从部署了 Agent 的 Mihomo 收集真实访问过的域名，找出**没有被任何规则覆盖**（落到兜底 `MATCH`）的域名，以及**直连失败**的域名，一键加入默认的直连 / 代理规则集。

入口：左侧导航「当前配置 → 域名发现」。数据和写入都属于当前配置空间。

## 使用步骤

1. **开启采集**：在「采集设备」里打开 Mihomo Agent 的开关（需要 Agent 1.4.0-go 及以上）。Agent 会在下次心跳（约 30 秒内）开始采集，首次上报约 15 秒后，之后每 5 分钟一次。
2. **设置默认规则集**：在「默认规则集」里选择两个内容型规则集（classical 或 domain 格式），或点「一键创建」生成 `域名发现-直连` / `域名发现-代理`，它们会自动加到兜底规则之前。
3. **部署一次配置**：让规则集进入 Agent 上的 Mihomo 配置。
4. **处理域名**：在列表里对域名点「加入代理」或「加入直连」，也可以展开只处理某个子域，或忽略不关心的域名。

部署过默认规则集之后，再加入的域名**不需要重新部署**：Agent 下次上报时发现规则集内容有变化，会让 Mihomo 立即重新拉取对应的 rule-provider。

## 列表说明

| 标签 | 含义 |
|---|---|
| 未覆盖 | 有连接命中了兜底规则 `MATCH` |
| 直连失败 | 直连失败次数 ≥ 3，且失败占「成功连接 + 失败」的一半以上 |
| 待部署 | 当前配置已有规则能匹配它，但 Agent 上的 Mihomo 还在用旧配置 |
| 已忽略 | 在忽略列表中，默认不显示 |

- 域名按主域合并（`a.b.example.co.uk` 归到 `example.co.uk`），使用 tldextract 自带的公共后缀列表，不联网。
- 「加入代理 / 直连」默认写 `DOMAIN-SUFFIX`；展开后的「仅此子域」写 `DOMAIN`。已被规则集里同名或上级后缀覆盖的会跳过。
- 失败类型：超时、重置、拒绝、断开、解析失败（DNS）、其他。
- 嗅探覆盖率 = 有域名的连接 / 全部连接。偏低时检查 Mihomo 的 `sniffer` 是否开启。

## 数据与隐私

- 默认关闭，按 Agent 单独开启；关闭后 Agent 立即停止采集。
- 只保存域名、端口、命中规则、策略、出口类型和计数，不保存网址路径、客户端 IP、进程名。
- 按天保存在 `data/traffic/<agent_id>.json`，保留 7 天，每天最多 5000 个域名。可在页面上一键清除；删除 Agent 时一并删除。

## 实现要点

- Agent 读取本机 Mihomo 配置里的 `external-controller` 与 `secret`，监听在 `0.0.0.0` / `::` 时改为访问 `127.0.0.1`。
- 两个采集器：每 2 秒轮询 `/connections`，连接从快照中消失即计为结束；订阅 `/logs?level=warning` 解析拨号失败，同一连接 30 秒内的重试只计一次。存活不到 2 秒的成功连接可能漏采。
- 上报接口 `POST /api/agents/<id>/traffic-report` 使用 Agent token，请求体上限 256 KB，字段严格校验；开关通过心跳响应的 `domain_discovery_enabled` 下发。
- 相关接口：`/api/domain-discovery/{domains,settings,rulesets/init,apply,ignore}`，`/api/agents/<id>/domain-discovery`（开关）与 `/api/agents/<id>/domain-discovery/clear`（清除数据）。
