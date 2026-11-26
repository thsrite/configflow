# HR 复核：O18 T1d（model 包外 DATA_DIR / 设备回复记录改普通结构体，删除旧 R5 机制）

分支 batch-42（= go-o18-t1d），复核 2026-11-23，测试在 .32/.64。按 WORKFLOW「Leaner verification」：不重跑作者全套对照，
跑受影响包 `-race`、difftest/proddiff 门禁阶段、作者的 5 个 fuzz 各 120 s、preflight；每个修复各做一个负向对照。

## 整理

- **rebase 到 go**（go 已合入 batch-40 metrics/rankings 事务、R13 公共后缀表、batch-41 jsondecode v2 + 仓库哨兵 + ValidationError 改名）：
  只有两处冲突——`behaviour-changes.md`（两节都保留：O18 T8 在前、O18 T1d 在后）和 17 个 pyref 二进制 golden（取 go 的录制，复核末尾重录）。
  go 新增的代码没有用到被删的 model 旧 API（`DecodeRecord/EncodeRecord/SetMemberJSON/ClearMember/Raw/Map/Issues/JSONHasContent/JSONSwitchEnabled`），
  rebase 后两套 tag `go vet` 直接通过，无需迁移。`deploy/agents` 里剩下的 `MemberJSON` 是 Agent 回复类型自己的方法，与 model 无关。
- **每个提交可编译**：rebase 后逐提交在 .64 上 `go vet ./...` + `-tags configflow_proddiff,configflow_difftest`：
  原 health、substore…、remaining views、delete model、model tests 五个提交测试编译失败（作者只说了后三个）。非交互重排：
  - health 提交并入 2c3e43b1b 中 `health/**/_test.go` 的改动；
  - substore/subs/refresh/taskstatus/setup 提交并入 notifications/server/setup/substore/provider/refresh 的测试改动（`setup/review_c36_test.go` 除外，它依赖 `_revision→int64`）；
  - remaining views 提交并入 `model/content_test.go`、`switch_test.go` 的删除（该提交删了 `content.go`/`switch.go`）；
  - delete model 提交吸收「model tests drop the R5 codec cases」与「tests follow the plain DATA_DIR records」的其余部分（含后者里的 `health/latency.go` 产品改动）。
  重排后 13 个作者提交每个都通过两套 vet；末端树与重排前逐字节相同（`git diff` 为空）。

## 发现

### 高
无。

### 中

**M1 旧订阅缓存的 metadata 没有 `url` 成员时，配置向导报「订阅链接已变化，请重新读取订阅」（已修，dc73bb17e）**
`setup/generation_adapter.go` 改成把 `json(cache.Metadata.URL)` 与订阅 URL 比较；`URL` 是 `*string`，成员缺失与 `null` 都读作 nil，
于是缺 `url` 的旧缓存被拒绝。Python 是 `cache.get('metadata', {}).get('url', <当前 url>)`（缺失不检查），T1d 前 Go 也先查成员是否存在。
修复：`decodeCacheData` 记录 `CacheMetadata.URLAbsent`（metadata 是对象但没有 `url`），向导跳过比较；`null` 仍是已存的 URL（与 Python 一样拒绝）。
`cloneCache` 保留该标记（值拷贝）。测试 `setup.TestReviewO18T1dLegacyCacheWithoutURL`（真实 repo + 缓存文件，缺失/相同/不同/null 四例）、
`substore.TestReviewO18T1dMetadataURLAbsent`。负向对照：去掉 `!cache.Metadata.URLAbsent` → 缺失例报「订阅链接已变化」失败。

**M2 订阅缓存节点列表里的 `null` 元素不再让列表算损坏 → 发布/provider 不再失败即关（已修，dc73bb17e）**
宽松解码把 `nodes: [{...}, null]` 的 `null` 读成空节点且没有 issue，`NodesDamaged()` 为假；T1d 前（R5）整个 `nodes` 有 issue、发布拒绝，
Python 发布时对 `None` 节点出错。修复：`decodeCacheData` 对节点列表中不是对象、且还没有 issue 的元素补 `nodes[i]` 类型 issue。
`LoadCacheSummary` 本来就把非对象元素当作没有可用节点，不变。测试 `substore.TestReviewO18T1dNodesDamaged`（15 例，含 `[null]`、带空白的 `null`、
节点成员错类型不算损坏）。负向对照：不追加这些 issue → `[{"name":"a"},null]` 例失败。

**M3 `custom_config` 存成非文本时读作空，生成与 IPv6 部署照常进行（T1c 起既有；按协调裁定失败即关，已修，23377dc48）**
T1d 删掉的 `routers/ipv6_model.go` 「基础 YAML 必须是文本」检查自 T1c（Profile 改普通结构体）起不可达，但有安全意义：
`custom_config` 为对象/布尔/数字时读作 `""`（数字读作其文本），IPv6 部署按空基础 YAML 计算（可能撤下本功能管理的 IPv6 设置），
Mihomo/Surge/Loon/MosDNS 生成按空自定义配置继续；Python 出错（`.strip()` / `yaml.safe_load` 的 AttributeError）。
修复（裁定：fail closed）：四个引擎的 `custom_config` 标 `stored:"restrict" falsy:"empty"`。按 Python 的 `custom_config or ''`：
Python 为假的非文本（`[]`、`{}`、`false`、`0`；`null` 照旧）读作空基础并报 issue；其余非文本读作 `model.DamagedText` 并报 issue
（repo 按成员解码时记告警），写回保持损坏。读取方遇到 `DamagedText` 拒绝：
Mihomo/Surge/Loon/MosDNS 生成（「<引擎>.custom_config 不是文本，请在配置页重新填写后保存」，生成接口 500）、
IPv6 部署与期望状态的 fake-ip 网段（「基础 YAML 必须是文本」，原文案恢复）、监听器引用改写、域名发现与网关安装的端口检查。
主路由分流状态视图（`routersplit.baseNode`）对它与不可解析的 YAML 一样读作无基础（只是诊断视图），未改。OxiDNS 没有同类成员；
MosDNS `custom_hosts` 是 hosts 文本而非基础配置，未纳入。
MosDNS 真 Python 语料：Python 出错、Go 以该文案拒绝的行登记为已接受（两边都失败；Python 产出配置的行不接受）。
pyref：oxidns 8 个与 mosdns 4 个 golden 只有 metadata 变化；mosdns `TestCorpus` 的 oracle 条目因派生输入带上新的读取值（`"\x00damaged"`/`""`）
重录（删 630、增 683，均为真 Python 输出，不记录 Go 输出）；sources.json 为三个触及该文案的 mosdns 测试声明 `msgCustomConfigNotText`。
随之更新两条按 T1c「数字读作其文本」断言的 convert 测试（`TestTypedClientSharedCustomConfigText`、`TestNativeMihomoStoredMemberFallbacks`）。
测试：`model.TestReviewO18T1dDamagedCustomConfig`（四引擎，真值/假值/null/文本，写回再读）、convert/mosdns/routers/subs/discovery 的
`TestReviewO18T1dDamagedCustomConfig*`。负向对照：去掉 `stored:"restrict"`、去掉 `falsy:"empty"`、逐个关掉 8 处拒绝 → 对应测试全部失败。
网关安装端口检查（`installation.Service.checkPorts`）没有单测（需要整套 Service），由代码审读确认。

### 低

- **L1 `Truthy`/`Switch` 把下溢到 0.0 的数（`1e-400`）当真（已修，a3cd64660）**：新 `truthy` 沿用 `JSONSwitchEnabled` 的「数字位全为 0」判断，
  `JSONHasContent` 原先用 float 值（`1e-400` 为假），Python `bool(1e-400)` 为假。改为 `strconv.ParseFloat(...) != 0`（上溢 ±Inf 为真，整数字面量不会舍成 0）。
  存储开关遇到 `1e-400` 也随之读作关（与 Python 一致）。测试 `model.TestReviewO18T1dTruthMatchesPython` 逐项核对 null/false/0/-0/0.0/0e5/""/[]/{}/
  `{"a":null}`/`[0]`/`"0"`/大整数/上溢/下溢，以及 `Switch` 缺失=默认、`enabled:null`=关。
- **L2 `dns.use-hosts: null` / `enable: null` 读作 `false`（已修，3512dc758）**：`readDNSSettings` 用 `json.Unmarshal(null, &bool)`，不报错、得 false，
  DNS 报告误报 `use_hosts_off`（Python `is False`）。null 现读作缺失。测试 `subs.TestReviewO18T1dDNSSwitchNullIsAbsent`。
- **L3 保留为 JSON 的文本带 HTML 转义写入（已修，b03d6ec37）**：通知 `profile_id/profile_name`（T1d 起为 RawMessage，原先由 model 编码器写）
  和 Sub-Store `state.json` 的 `error` 用 `json.Marshal` 编码，`&<>` 写成 `&` 等；语义相同、字节与 Python 不同。改用 `jsonresp.Encode` / `model.Marshal`。
  测试 `notifications.TestReviewO18T1dStoredTextWithoutHTMLEscapes`。
- **L4 删掉 `model.Issues` 分支后留下的死代码（已清，bbb33945a）**：`health.hasFieldIssue`、`messageMalformedManifest`、`messageIPv6YAMLType`。
  `server/o10_r11_gate_test.go` 的 `o10Encoders` 里 `model.EncodeRecord` 一项已不存在（无害，未改）。
- **L5 其它核对结论（未改）**：
  - `Switch` 对非法 JSON 一律为真，`JSONSwitchEnabled` 按首字符判断（`nul` 为假）；输入都来自已解码数据，不可达。
  - Loon `reality-opts`：旧代码只数未声明成员，新代码「对象有任何成员」即真，更接近 Python `bool(params.get('reality-opts'))`
    （Python 对非空字符串也为真，Go 只认对象；不可能的输入）。
  - 区域/探测文件里有非法 UTF-8 时整份读作空、下次写入重置——与 T1d 前相同（已在 go 基线上实测），Python 读文件 `UnicodeDecodeError` 同样得空。
    区域文件「条目不是对象」照旧失败即关（作者测试 + 本复核核对）。
  - 向导会话 `prepared: null` 重写为 `false`（T1c 存储开关规则，读取语义相同）；`profile_revision: "7"`、`owned_digest: 5`、`node_ids: [1]` 与 T1d 前一样接受。

## 逐类核对（问题 1）

| 类别 | 结论 |
|---|---|
| 通知 | 文件形状错/非对象条目拒绝（`decodeStore`）；错类型成员读作缺失只影响显示收据；`RouterCheck.empty` 按成员数与旧 Raw 等价。L3 已修。 |
| 订阅缓存、hosts/dialers 元数据 | `NodesDamaged` 覆盖非列表/非对象元素（M2 补 null）；metadata 非对象或 url 非文本不认领（`TestReviewO18T1dDamagedMetadataNeverClaims` 12 例）；`dialers` 错类型仍算已存决定（provider 与 subs 两处）；hosts 无 entries 对象不建索引。M1 已修。 |
| 健康历史 / 下载回执 / 任务状态 | 按条目重写，其余条目原样保留；回执 `text/fetched_at` 必须是文本才用；`last_run_at` 非文本不当时间（`text:"strict"`）；计数器仍原样 JSON。 |
| Sub-Store 状态与发布信息 | `last_used` 为 null/非数字停止空闲处理（同前）；发布信息字段错类型报错，未声明成员忽略。 |
| 向导会话 | 任何声明成员错类型整份 `ErrCorruptRecord`（`TestReviewO18T1dCorruptSessionRefused` 14 例），非对象拒绝。 |
| 节点测速 | url/unified_delay/dns 列表错类型或缺失不续接（`TestReviewO18T1dDamagedSettingsNotContinued` 9 例）；`Extensions` 只在 `LatencyEntry`。 |
| 探测/区域存储 | 区域条目非对象 `UpdateData` 拒绝且文件不改（作者测试）；探测文件是缓存，损坏读空（同前）。 |
| 概览历史 / 客户端选项 / routersplit / MosDNS·OxiDNS 投影 / ipv6 映射 | 均为只读视图或缓存；客户端开关只认 JSON 布尔；routersplit 会话 `gate` 开关错类型读作关，与 T1d 前 R5 零值一致。 |

## 删掉的 `model.Issues` 分支（问题 2）

- 不可达且无安全意义：节点测速「节点 name 必须是字符串」（T1c 宽松身份裁定）、convert/Mihomo 端口原值回读（类型化编码与 `IntValue` 相同）、
  Sub-Store `ProxyNodes` 原值回填（T1c 已接受）、`routers.Inspect` 归属记录检查（T1c 起损坏清单整份保留，`ValidateForDevice` 失败即关）。
- 不可达但有安全意义：IPv6「基础 YAML 必须是文本」→ M3（已按裁定修复）。
- 恢复的两处正确：Agent 注册表行按需加载用 `DecodeStoredMember` 的 issue 告警；节点测速 DNS 用 `StoredIssues`（事务内取基线 issue）
  查 `system.system_config` 类型 issue，与 repo 的 `systemSettingsIssue` 同一路径。

## 编码边界（问题 3）

`TestReviewO18T1dEncoderBoundary`：不做 HTML 转义、NaN/±Inf 拒绝、30 位整数原样、非法 UTF-8 文本替换为 U+FFFD（与 T1c 一致）、
经 `Fields` 拼出超过 10000 层报 `ErrJSON`、`OrderedMap` 自引用报 `ErrJSON`（不再栈溢出）、`Fields` 重复成员名拒绝、成员名按解码后的文本保存（token 失效修复）。
`TestReviewO18T1dLenientInvalidUTF8`：宽松读出的值都能写回。U+2028 被 `scalarJSON` 转义为 ` ` 是 T1d 前既有行为，未改。

## 兼容（问题 4）

8 份旧缓存补 `metadata.fetched_at: null`：只有作者探针的「解码→写出」会补；Go 只在刷新时用新 metadata 重写缓存，没有读取方区分缺失与 null
（`saveCache` 只用新 metadata 的 fetched_at；refresh 视图为 `*string`）。`LatencyEntry.Extensions`（`json:",embed"`）全树唯一。

## 提交（batch-42，rebase 到 go@f90d05448 后；作者 13 个提交重排为 161f9f470..898b83d02）

- dc73bb17e go(substore,setup): legacy cache without metadata.url / null node element（M1、M2）
- a3cd64660 go(model): Truthy/Switch underflow（L1，含编码边界测试）
- 3512dc758 go(subs): dns null switch（L2）
- b03d6ec37 go(notifications,substore): no HTML escapes（L3）
- bbb33945a go(health,routers): dead helpers/messages（L4）
- d036ba7c6 go(refresh,health): review tests（metadata 认领、测速设置）
- 81f939840 go(pyref): 17 个 golden 只重录 metadata
- cf8e7ea5f、6bc0b0197 本报告与 behaviour-changes 补充
- 3173110d3 go(server): R11 编码器清单删去已不存在的 `model.EncodeRecord`（取自作者的 rebase）
- 23377dc48 go(model,convert,mosdns,routers,subs,discovery,gateway): M3 custom_config 失败即关

## 验证（末端树）

- `gofmt` 无输出；`go vet ./...` 与 `-tags configflow_proddiff,configflow_difftest`：VET-OK（.32）。
- `-race -count=1 -p 6 -timeout 60m`（.32）T1d 改动的 35 个包全部 ok，无 DATA RACE：agents 512.0 s、agents/dnsfollow 520.0 s、agents/dnsha 508.4 s、
  api/agents 485.1 s、convert 915.7 s、deploy 358.6 s、mosdns 288.2 s、oxidns 1089.2 s、provider 168.7 s、refresh 162.5 s、repo 489.8 s、routers 804.9 s、
  server 143.2 s（含 R11 `TestO10RouteAllocationCeilings` 与 view guard `TestO10WholeRecordEncodeBoundary`）、subs 183.7 s、substore 128.3 s 等。
- `GATE_ONLY=difftest gate.sh -p 6`（.64）：`DIFF PASS: 214 required entries ran and passed; 0 test skips`，`GATE STAGE PASS: difftest`。
- 真 Python 环境（CONFIGFLOW_PY 等同 gate-live）：model、repo、store、substore、notifications、setup、health/...、refresh、taskstatus、subs、convert、
  api/subscriptions、provider 全部 ok（.64）。
- `GATE_ONLY=proddiff gate.sh -p 6`（.64）：`baseline: new=0 stale=0`，`GATE STAGE PASS: proddiff`。
- fuzz 120 s（.64，并发）：FuzzStoredTypes、FuzzRecordRoundTrip、FuzzDocumentRoundTrip、FuzzMarshalOutputValid、FuzzSetupRecord 全部 ok。
- pyref：rebase 取 go 的 golden 后 mosdns/oxidns/routersplit/installer `check` 报 metadata 变化；`record` 后 17 个文件解压比对非 metadata 差异 0，
  再 `PYREF_MODE=check`：mosdns 44.5 s、oxidns 52.6 s、routersplit 2.2 s、installer 103.2 s ok。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-42 x 6`（.64）：`PREFLIGHT PASS`。
- 负向对照（.64）：撤掉 M1/M2/L1/L2/L3 各自的修复 → 对应复核测试全部失败。

## 第二轮：rebase 到 go@f90d05448 + M3 裁定（2026-11-23）

- 不换用作者的 go-o18-t1d（c31664d9c）：其树与本分支复核修复前的树只差 pyref metadata 与 `o10_r11_gate_test.go` 一行，后者取来单独提交（3173110d3）；
  本分支的逐提交可编译重排保留。f90d05448 只加文档，rebase 无冲突。
- M3 按裁定修复（23377dc48，见上文 M3）。

### 第二轮验证（树 23377dc48）
- `gofmt` 无输出；`go vet ./...` 与带 tag vet：VET-OK（.32）。
- `-race -p 6 -timeout 60m`（.32）：model、mosdns 338.6 s、oxidns 978.5 s、routers 726.5 s、routersplit、subs 210.9 s、discovery、gateway/...（含 installation 85.9 s）、
  api/generate、api/config、api/mosdns、repo 299.1 s、setup、importer、server 94.4 s、agents/ipv6 全部 ok；convert 首轮两条旧断言失败（见上），改后 `-race` ok 632.7 s。无 DATA RACE。
- `GATE_ONLY=proddiff gate.sh -p 6`（.64）：`baseline: new=0 stale=0`，`GATE STAGE PASS: proddiff`。
- `GATE_ONLY=difftest gate.sh -p 6`（.64）：`DIFF PASS: 214 required entries ran and passed; 0 test skips`，`GATE STAGE PASS: difftest`。
- 真 Python 环境：model、repo、mosdns、routers、subs、setup、importer ok（convert 同上两条旧断言，改后 ok）。
- `PYREF_MODE=check`（.64）：mosdns 44.4 s、oxidns 52.3 s、routersplit 2.2 s、installer 103.0 s ok。
- `/tmp/preflight.sh … x 6`（.64，末端树）：`PREFLIGHT PASS`。
- 负向对照（.64）：去掉 `stored:"restrict"`、去掉 `falsy:"empty"`、逐个关掉 mihomo/surge/loon/mosdns/IPv6/fake-ip/监听器/端口检查的拒绝 → 对应测试全部失败。
