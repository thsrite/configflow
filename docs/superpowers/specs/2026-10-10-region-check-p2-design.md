# 域名发现 P2 设计：区域限制检测

在 P0（被动发现）和 P1（直连 / 代理探测）的基础上，回答两个问题：

1. **常见服务解锁**：ChatGPT、Claude、Netflix、YouTube Premium 等，在各个策略组和地区节点下能不能用、显示为哪个地区。
2. **任意域名的地区差异**：发现列表里的某个域名，换不同地区的节点访问，结果是否不同（疑似区域限制）。

检测结果可以直接转成规则：把服务或域名指定走某个策略组。

## 1. 经指定节点发起完整请求

P1 的延迟测试只返回延迟，拿不到响应内容和跳转地址。P2 在 **推送给 Agent 的 Mihomo 配置** 里注入一个只监听本机的入口：

```yaml
proxy-groups:
  - name: ConfigFlow-Region-Probe
    type: select
    proxies: [DIRECT, <当前配置的所有策略组>]
    include-all-proxies: true
    hidden: true
listeners:
  - name: configflow-region-probe
    type: mixed
    listen: 127.0.0.1
    port: 17999        # 可在设置中修改
    proxy: ConfigFlow-Region-Probe
```

- Agent 先通过控制接口把这个组切到目标（某个节点、某个策略组或 DIRECT），再经 `127.0.0.1:17999` 发请求。这个组不被任何规则引用，不影响真实流量。
- 已验证（mihomo v1.19.32）：
  - `include-all-proxies` 会自动收进所有节点，`hidden` 在面板中隐藏；
  - 切换返回 204，经入口的请求日志为 `using ConfigFlow-Region-Probe[节点]`；
  - mixed-port 照常走规则。
- **只在开启「区域检测」的配置空间、且只在推送给 Agent 的配置里注入**；手机等客户端订阅的配置不变。开启后需要部署一次。
- 选择策略组作为目标时，测的是它当前选中的节点，也就是用户真实访问时走的路径。

## 2. Agent（1.6.0-go）

- `GET /api/region-check/targets`：读取本机 Mihomo 配置里 `configflow-region-probe` 入口的地址，返回探测组可选的目标（名称、类型、是否存活）。入口未部署时返回 409。
- `POST /api/region-check`：针对**一个目标**执行一组请求：
  ```json
  {"target": "🇺🇸 美国 01", "timeout_ms": 8000,
   "requests": [{"id": "openai-compliance", "url": "https://api.openai.com/compliance/cookie_requirements",
                 "headers": {"Authorization": "Bearer null"},
                 "markers": {"unsupported": "unsupported_country"}}]}
  ```
  - 切换探测组 → 并发（≤ 4）执行请求 → 切回原选择。同一 Agent 内用互斥锁串行，避免两个检测互相切组。
  - 请求最多跟随 5 次跳转，记录跳转链；响应体最多读 2 MB，只返回**标记匹配结果**（正则的第一个捕获组或整段匹配），不回传正文。
  - 限制：requests ≤ 12、markers ≤ 8、URL 只允许 http/https、header 白名单（Authorization、Accept-Language、Cookie、Referer、Origin）。
  - 返回每个请求的 `status`、`final_url`、`redirects`、`markers`、`error`（timeout / tls / connect / other）和 `elapsed_ms`。

服务的检测逻辑放在服务端，Agent 只是通用的「经指定节点取页面并匹配标记」执行器。服务调整检测方式时只需升级服务端。

## 3. 服务检测（服务端定义）

每个服务给出：请求列表、判定函数、地区来源、用于生成规则的域名列表。判定结果为 `available`（可用，附地区）、`partial`（部分可用，如 Netflix 仅自制剧）、`blocked`（附原因）或 `unknown`（响应不符合已知模式，附证据）。**宁可 unknown，不猜。**

| 服务 | 请求与判定 | 地区 |
|---|---|---|
| ChatGPT | `api.openai.com/compliance/cookie_requirements` 含 `unsupported_country` → 不支持；`ios.chat.openai.com` 含 `VPN` → 检测到代理；前者 200 → 可用（`ios` 返回 `"type":"dc"` 时提示 App 可能受限） | `chatgpt.com/cdn-cgi/trace` 的 `loc` |
| Claude | `claude.ai` 跳转到 `app-unavailable-in-region` → 不支持；200 → 可用；403（Cloudflare 质询）→ 未知 | `claude.ai/cdn-cgi/trace` 的 `loc` |
| Netflix | 非自制剧 `title/81280792`、`title/70143836` 任一 200 → 完整解锁；都 404 时自制剧 `title/80018499` 200 → 仅自制剧；403 → 不可用 | 跳转路径 `/xx-yy/title` 中的 `xx`，无前缀为 US |
| YouTube Premium | 页面含 `Premium is not available in your country` → 不可用；能取到 `INNERTUBE_CONTEXT_GL` → 可用 | `INNERTUBE_CONTEXT_GL` |
| 出口 IP | `www.cloudflare.com/cdn-cgi/trace` | `loc`、`ip`（每个目标都测，用来标注节点的实际出口） |

Gemini、Disney+ 暂不纳入：前者社区脚本使用的页面标记在 49 上实测已不出现，后者需要多步 token 流程，没有可靠的判断依据时宁可不做。

## 4. 任意域名的地区差异

对发现列表中的域名，经每个目标请求 `https://host/`（端口规则同 P1），比较各目标的结果：

- 单个目标判为**受限**：状态 451；或 403 / 200 且正文命中受限关键词（`not available in your (country|region)`、`unavailable in your (country|region|location)`、`not available in your location`、`地区不可用`、`所在的地区` 等）；或最终 URL 含 `unavailable` / `region` / `country` 且原始 URL 不含。
- 汇总：既有受限又有正常 → **疑似区域限制**（列出可用地区）；全部受限 → **各地区都受限**；全部正常 → **无地区差异**；都失败 → **无法访问**。
- 这是启发式判断，界面上明确标注「疑似」，并展示每个目标的状态码、最终地址和命中的关键词。

## 5. 目标选择

由服务端根据 `targets` 接口的返回挑选：

- **当前路径**：当前配置里的策略组，排除 DIRECT / REJECT 和探测组，最多 4 个，优先默认代理规则集指向的组。
- **地区代表节点**：按节点名识别地区（与前端 `regions.ts` 相同的规则，并补充 CA、AU、FR、NL、IN、MY、TH、VN、PH、TR、AR、BR 等），每个地区取第一个存活的节点，最多 8 个地区。
- 实际地区以 Cloudflare trace 的 `loc` 为准；与名称不符时在界面上标出。

## 6. 存储、任务与规则

- 结果：`data/regions/<agent_id>.json`，保存 `services: {目标: {服务: 结果}}`、`domains: {host: {目标: 结果}}`、`exits: {目标: {loc, ip}}` 和时间，保留 30 天。
- 任务：复用 P1 的后台任务机制（同一时间一个），`POST /api/domain-discovery/region/check {"services": true, "domains": [...]}`，按目标逐个调用 Agent 并报告进度；`GET /api/domain-discovery/region` 返回最近一次的矩阵。
- **指定走向**：`POST /api/domain-discovery/region/route {"kind": "service" | "domain", "value", "policy"}`
  - 为策略组自动创建并引用规则集「域名发现-<策略组>」，放在默认代理规则集之前（没有时放在 MATCH 之前）；
  - 服务写入其内置域名列表（如 ChatGPT：`openai.com`、`chatgpt.com`、`oaistatic.com`、`oaiusercontent.com`），域名写 `DOMAIN-SUFFIX`；
  - 记入历史，可撤销；规则集变化同样由 Agent 自动刷新。

## 7. 设置

`domain_discovery` 新增 `region_check_enabled`（默认关）、`region_probe_port`（默认 17999，1024–65535）、`regional_rulesets {策略组: 规则库 ID}`。

## 8. 前端

域名发现页新增「地区限制」视图：

- 顶部：开关、入口端口、部署状态提示（「需要部署一次」），以及「检测服务」按钮和进度；
- 服务矩阵：行为服务，列为目标（策略组 + 地区节点，列头带实际出口地区）；单元格显示 可用·US / 仅自制剧 / 不可用 / 未知，悬停显示依据；每行可「指定走向」选择策略组；
- 域名列表的行操作新增「检测地区差异」，展开后显示各目标的状态码、最终地址和判定。

## 9. 测试

- Go：请求校验、header 白名单、跳转与正文上限、标记匹配、组切换与恢复、互斥、入口未部署时返回 409，以及对假 Mihomo 和假站点的端到端测试。
- Python：各服务判定（用实测与构造的响应作为夹具）、通用地区差异判定、目标挑选、配置注入只在开启且是 Agent 配置时发生、指定走向与撤销。
- 实机（49）：注入入口后切换节点，经入口访问 trace 和各服务；49 的网关会按域名从不同地区出口，用它产生不同的结果。
- 局限：没有真实的多地区节点，「可用」与「不可用」两类结论不能逐一实测，判定依据来自公开的检测方法并用夹具覆盖。界面给出证据，方便人工复核。

## 10. 实现记录

- 49 实机：A 的配置由 ConfigFlow 生成，注入的 `include-all` 选择组与本机入口能被 mihomo v1.19.32 正常加载。服务检测与域名检测都经各目标发出（日志里是 `ConfigFlow-Region-Probe[节点]`），结束后探测组恢复为 DIRECT。
- 名为「香港」「美国」的节点在 49 上实际都从 JP 出口（49 的网关按域名分流），验证了必须用 trace 标注实际出口。
- 不存在的节点（JP，指向不可达地址）各项均为「未知」，出口显示连接失败，不影响其他目标。
- Claude 在 49 上返回 Cloudflare 质询（403），判定为「未知」而不是「不可用」。
- 判定函数统一把出错的请求视为没有状态码，避免带着默认状态码的错误结果被当成成功。
