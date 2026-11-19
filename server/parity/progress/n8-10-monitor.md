# N8-10 monitor S pass

状态：实现及 batch-11 新基底定向检查完成；未标记 verified，待独立复核及协调者 batch gate。
分支 `go-n8-10-monitor`，先接纳 core-2 HR tip `70cea1eb6`，后按协调通知
rebase 到 batch-11 `origin/go@ae00b71f1`（Go 1.27.4、json-case/jsondecode.Select）。
迁移主提交现为 `2a2f65a62`、边界/测试提交 `065da003b`，包注释 `600ef8553`；
接纳 jsondecode 与轻量记录修复为 `fcb0d7124`；最终验证记录见本报告所在提交。无 push。

## 完成边界

生产 owner 使用 `Registry.ReadAgent` / `model.Agent` 的原始 member、`model.Fields` 精确键名和
原生 DTO，所有产品 JSON 解码统一走 jsondecode.Unmarshal。实现包含 cache / rule update / Mihomo panel、版本检查缓存、OxiDNS metrics、
dashboard 状态文件/CAS/发布检查点、overview/query/client/ranking、排行 SQLite 和后台 collector。
主要类型是 `Observations`、`MetricNumber`、`MetricSummary`、`DashboardState`、
`PendingPublication`、`Snapshot`、`Rankings`、`CacheResult`、`RuleTask`。
未知响应字段只在协议允许的位置作为 RawMessage 保留；统计状态及已知业务字段逐项验证。

生产主体没有 jsonx import、PythonClass、AttributeError/TypeError、对象 truthiness 或异常仿真。
9 个 `*_legacy.go`（1004 行）保留原导出签名及转码，供后续 API/upgrades/rulesync/dnsha 等
consumer 的 S pass 使用；`FromAgents` 直接实现 typed registry，不做整个 Agent 的 jsonx 往返。
HA rules 的旧回调签名暂在 codec 边界转码，没有改 core 公共 API。
未迁移的用户文本/Unicode 数字输入兼容 helper 留在 `input_text.go`、panel 查询参数及既有表，属于 N10；
机器版本入口独立 ASCII 校验。以 batch-11 为基准，整个 monitor 产品代码 6325 → 7811 行（含适配，新增显式
DTO、严格协议验证与 ctx 叶存储）；32 个产品 Go 文件，原 15 个。行数为信息，无预算变更。

版本下载复用 native rulefetch.Download/localhttp；只投影实际消费的三个 SystemConfig 设置。
没有扩大下载/Agent 响应上限、排行行数/样本数、采集预算或 TTL，没有升级依赖或改 go.mod。
本包没有原 allocation 阈值测试；未修改 core 的热点阈值或期望。

## 状态、协议和恢复

- Agent 回包以精确 member 解析，拒绝真实/escaped/nested duplicate、大小写别名授权及非 boolean
  协议标志；合法未知字段按该 DTO 原合同处理。N8-1 非有限回包数字逐值 null 修复保持，
  原 PanelDifferential NaN 比较没有放宽。
- 原始身份字段必须是 JSON string，不依赖 R5 投影。pending checkpoint 同样校验原始
  profile/service/deployment/status/manual_rollback，不能由错型或大小写别名解除检查点。
- 写操作 Detach 浏览器取消但保留 Runtime shutdown；设备发起/回包、统计文件 mkdir/temp/replace、
  SQLite BEGIN/语句/COMMIT 和共享版本/排行缓存发布均检查 ctx。SQLite 取消会回滚；
  maintenance watermark 仅成功提交后更新。真实 Runtime 回归覆盖断线完成、shutdown 后不写、
  已停止 generation 不再发请求；没有修改生产超时来迁就 fake。
- 缓存深拷贝排行 rows、数值/share、blocked tags 和 RawMessage；回归使用真实生产 TTL，
  修改第一次公开结果后再次读取，确认命中缓存且值未污染。
- 脱敏复用 redact，后台 panic 不将任意载荷写入日志。

协调者已批准的差异都登记 `behaviour-changes.md`：

| 边界 | 原始样本绑定 | 拒绝与恢复 |
|---|---|---|
| 非 object HA publication | a07 archive + 6 条完整 row SHA | 原有 native 500，中文说明 OxiDNS HA 发布记录损坏；不发设备请求、不写；`dnsha/monitor_repair_test.go` 证明现有 HA Disable 清除坏字段后规则更新成功 |
| pending 对应部署容器/status 损坏 | a06 archive + 88 条完整 row SHA | DASHBOARD_STATE_ERROR/500，不清 pending、不写统计边界；四类坏记录经现有显式 reset 开新周期后 dashboard/统计恢复 |
| 状态文件 duplicate / 回包错型或 duplicate | 3 条 state_files、3 条 responses | 明确损坏/INVALID_RESPONSE；另 1 条 responses 绑定 N8-1 null 修复，未改原档案 |
| 机器版本 ASCII/64 字节边界 | 1 条 Unicode helper、3 条原生任意精度 helper、8 条公开 current-version | INVALID_RESPONSE/502，不 fetch、不覆盖 cache；短 release/alpha 行为保持 |
| 原生指标整数 | reference 2 条 + review 13 条 + overview 1 条 | 真 Python 业务仅关闭解释器 int digit cap 重跑，精确计数；保留有限范围与原样本上限 |
| 非标准 JSON 存储身份摘要 | review 17 条 target | 裸 NaN/Infinity 不产生摘要；设备 HTTP 仍遵循上面的 N8-1 修复 |

原 a06/a07 archive、原 expected、原 pyref entries 和旧 generator 都没有修改。新增 fixture
绑定 archive SHA 与完整原行 SHA，只有完全匹配的行选择 native 期望；有改行不命中的负控。
`gen_n810_monitor_versions.py` 使用独立 ASCII/任意精度策略；`gen_n810_metric_numbers.py`
运行真实 Python metrics，仅关闭 int digit cap，不读取 Go 输出。历史 shape-free private
public_upstreams helper 的不可达动态形状、dashboard_rankings 私有 helper 的部分 state 入参只在
test-only adapter 中保留；生产状态仍严格 DecodeState，生产上游投影来自
`MetricSummary`，复用同一 identity/display primitive。

首次加载 StateError 时，现有 UI 不显示 reset 按钮；协调者批准本轮不改 UI。
`doc/manual/oxidns-panel.md` 已说明管理员带管理认证调用
`POST /api/agents/{agent_id}/oxidns/dashboard/reset`、body `{}` 的恢复方法。
后续 UI 项：首次加载 StateError 时提供恢复入口（由协调者记入 remaining-work）。

## 验证

全部经 rt 在 .32/.64 执行；未在 Mac 跑测试、未跑完整门禁。
core-2 基底完整 monitor race PASS 447.797s，修复组 PASS 25.375s。
以下为 batch-11 新基底检查；不是全仓门禁。新基底 monitor 第一轮所列筛选 race PASS
80.521s；轻量记录修复后同组加 LegacyScalar 回归 PASS 82.428s。
新基底 gofmt -l 全仓无输出、go vet ./... 静默通过；七个直接调用方完整 race 全 PASS：
API 680.396s、dnsha 559.718s、rulesync 204.413s、ipv6 29.668s、gateway/host 11.392s、
gateway/installation 104.887s、server 25.287s。两台均使用 Go 1.27.4。

### 最终产品/调用方检查

```sh
# .64，新基底 monitor 定向 race（原语料及新边界）
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "Reference|Review|Corpus|Golden|Native|Damaged|PendingCheckpoint|CoreVersionCache|LegacyScalar" ./internal/agents/monitor'

# .64 格式与全模块 vet
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/monitor/*.go; test -z "$(gofmt -l .)"; GOMAXPROCS=4 go vet -p 4 ./...'

# .32，七个直接调用方的完整 race
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m ./internal/api/agents ./internal/agents/dnsha ./internal/agents/rulesync ./internal/agents/ipv6 ./internal/gateway/host ./internal/gateway/installation ./internal/server'
```

此前最终完整 monitor run 暴露了 13 条 review 指标的 CPython digit-cap 和 17 条非法存储
JSON 摘要旧期望；相同计数规则也传播到 1 条 overview。新增完整行 SHA 的 canonical
需与 Go 一样转义 U+2028/U+2029；校正独立生成器后逐字节复现。排行私有 helper 的
部分状态入参投影已移到 test-only adapter，不再误走生产文件 schema 验证。原期望不变，
以上全部失败组先单独 race 验证，再运行完整 monitor race 并通过。

### Flask HTTP 差分

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && GOMAXPROCS=4 go test -p 4 -count=1 -timeout=20m -run "Test(PanelDifferential|OxidnsDifferential|RankingCollectorDifferential)AgainstFlask$" ./internal/api/agents/difftest'
```

core-2 基底 PASS，71.808s；batch-11 新基底 PASS，74.160s。原差分逻辑不变。新增 `TestNativeMonitorHTTPWireBytes` 对真实 httptest +
agents.HTTPClient 比较六条请求的原始 URI/method/Auth/Accept/Content-Type/body bytes，覆盖
cache GET/POST、rules GET/POST、panel delay GET、selector PUT（含 Unicode 路径/名称）。

### Fuzz 与独立期望复现

```sh
# .64，新基底三个 target 各运行 60s；cache 使用共享字节解码/BOM 种子
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzMonitorCacheProtocol$" -fuzztime=60s -parallel=2 ./internal/agents/monitor'
# 同参数分别替换为 FuzzDashboardStateRoundTrip、FuzzTypedMetricsSnapshot
```

首轮全部 PASS：cache 57,570 execs / 61.011s，state 895,260 / 61.015s，metrics 152,321 /
61.014s。core-2 基底最终 cache 重跑 PASS 61.014s / 33,817 execs；generator 逐字节
复现 PASS。batch-11 新基底三个 target 各 60s 全 PASS：cache 43,983 execs /
61.014s，state 809,450 / 61.012s，metrics 51,089 / 61.013s。

Python 测试覆盖映射：

| Python 测试 | 保留的独立检查/补充边界 |
|---|---|
| test_oxidns_monitor.py、test_agent_light_reads.py | a06 reference/review、HTTP OxidnsDifferential；原始身份/精确键名、query/client 协议回归 |
| test_oxidns_metrics.py | a06 metrics/baseline（含原 700 组 baseline）、真实 Python no-digit-cap 绑定、typed metrics fuzz |
| test_oxidns_dashboard.py | a06 state/overview/400 flows 2846 steps、state roundtrip fuzz、真实 Runtime 文件与缓存边界 |
| test_oxidns_publication_stats.py | publication/reference、pending 88 行绑定、四种损坏与五种原始身份恢复回归 |
| test_oxidns_ranking_history.py | 原 ranking/collector/history corpus、SQLite rollback/无 shutdown 创建、RankingCollectorDifferential |
| test_oxidns_cache.py、test_oxidns_rule_update.py | a07 reference/review、normal/错误类协议回归、真实 HTTP 字节、HA Disable 恢复直接调用方回归 |
| test_mihomo_panel.py | a07 reference/review、PanelDifferential、Unicode 空白无效响应与版本边界/cache 回归 |

不更改已有超时、采集节拍、paused/timeout budget、查询缓存上限及核心分配阈值。
合并注意：保持 batch-11 ae00b71f1 基点；剩余兼容 codec 随各 consumer 的后续 S pass 删除；本轮尚未
独立复核，batch gate 由协调者运行。

## batch-11 接纳与轻量记录边界

新基底全部产品 JSON 解码已接入 jsondecode；model.Fields/自定义 codec 保留 duplicate
拒绝与未知字段策略，未用普通 struct 解码绕过原始成员证明。没有改 jsondecode 或 core。
`.64` 高负载的旧基底调用方 run 只有 rulesync 的
`TestStructurePublicationPinsNonRuleConfiguration` 失败；在新基底 `.32` 安静环境复现，
确认是 monitor 的 legacy AgentOrigin 将整条轻量记录编码，遇到不可序列化的
`repo.AgentHistoryNotRead`。改为只投影 host/port；版本能力同样只投影 version。
字段原始类型与 case alias 仍拒绝，原记录/历史标记不改，也没有将未读历史用作授权证明。
新增 `TestLegacyScalarReadsDoNotSerializeUnreadHistory` 与原 rulesync 失败用例分别 PASS
1.022s / 10.391s。未改 rulesync 的旧测试或期望，调试改动已撤回。
