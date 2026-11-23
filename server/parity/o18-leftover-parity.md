# O18 残留的「逐字节 / 逐顺序对标 Python」机制审计（2026-11-21）

分支 `go-audit-o18`（= `go@99daa1a8f`）。只读审计，不改产品代码。范围：`server/internal`、`server/cmd`，不含 `_test.go`
和测试工具（`testjson`、`testgolden`、`*test` 包、difftest/proddiff）。生产快照只在测试服务器 .32 上的私有副本里做只读分析，
临时探针已删除，本文只有路径模式和计数。

## 结论（先看这里）

**共 33 项残留：A 类（外部边界，必须保留）10 项，B 类（读旧数据需要）5 项，C 类（只为字节/顺序/Python 内部行为一致，可删）18 项。**

**model 层可以「直接去掉」，生产数据支持这么做。** 生产快照（43 行，539 KB，2 个配置空间，2271 条记录，9592 个叶子成员）里：

- Go 结构体不认识的成员：**1 个**（`system.agents[].has_update`，是 Python 接口给响应算出来的字段，被误写进了存储，没有用处）。
- 类型错误的字段：**0 个**（解码诊断 0 条）。
- 叶子成员「Go 按类型重新编码」和「库里的原字节」不同：**0 个**。也就是说，保留原字节在这份数据上什么也没保住。
- 真正会变的只有两件事，都没有语义影响：约 530 条记录的成员顺序和结构体声明顺序不同（第一次写入后顺序改变）；
  40 个指针字段存的是 `null`（都在部署/IPv6 校验历史和 HA 状态里，读取方 `null` 和缺失同样处理）。
- 代价（同一份生产文档，.32）：model 解码 **60.6 ms、71 MB（每字节输入 132 B）**；同样的字节用 `encoding/json` 解成通用
  `any` 只要 **4.7 ms、2.1 MB（4 B/B）**；model 编码 9.2 ms、13 MB（24 B/B）。类型化结构体解码通常比解成 `any` 还省。

**推荐：普通结构体 + 存储解码时的宽松叶子选项，不保留未知字段。** 细节见第 2 节。另外有两点必须保留：
用户数据作键的有序映射（部署历史等）要保持插入顺序；`Dynamic` 这类无 schema 的 JSON 保持原样存储。

**建议删除的 C 类任务（按收益排序）：**

| # | 任务 | 收益 | 量 |
|---|---|---|---|
| T1a | model 换普通结构体·第一步：`Document/Profile/Shared/System` + `repo/model_rows.go` 改成逐行直接解码/编码，加入宽松叶子解码选项（C1、C2） | 冷加载和写入的 model 成本降一个数量级（60 ms/71 MB → 估计 ≤ 5 ms/≤ 5 MB）；O10 R5（编解码降放大）不用再做 | M |
| T1b | 第二步：Agent、部署、HA、IPv6 校验、规则同步记录，以及 agents/* 里依赖「原值」的判断（C1、C4） | `/api/agents`、通知、心跳、rule-sync worker 的解码/拷贝成本主体；O10 R1 的浅拷贝变安全，`copyAgent` 可改成普通克隆 | L |
| T1c | 第三步：路由器、设置、资源、策略记录及其调用方（C1、C4） | 路由器读路由、生成器的 `resolveConfig` 成本；删掉 `SetJSON`/`Clear` 大部分调用 | M |
| T1d | 第四步：model 包以外 25 个文件里用 `model.DecodeRecord` 的记录（DATA_DIR 文件、Agent 回复）（C3） | 删掉 model 的最后用户后，可以删 `record.go/json.go/fields.go/map.go/member.go/switch.go/content.go` 约 2000 行和 3766 行测试 | M |
| T2 | `provider.configDigest` 不再对整份配置做规范化 JSON 摘要，改用 (repo head, 配置空间, 资源 id) 作合并请求的键（C5） | 每次 Mihomo/provider 请求少一次整份配置编码 + 重排键 | S |
| T3 | `encoding/json/v2` 替换 `jsondecode` 内部实现（大小写精确匹配是 v2 默认行为）（C18） | 删约 400 行自写的成员筛选/重写；148 个使用方 API 不变 | S–M |
| T4 | `repo.HelperError` 的 Python 异常类名（`ValueError/TypeError/KeyError/AttributeError`）换成哨兵错误（C8） | 去掉 36 处按类名字符串判断，符合「不模拟 Python 异常」 | M |
| T5 | `taskstatus/isotime.go`（338 行 CPython `fromisoformat` 移植）换成几种 `time.Parse` 布局（C9） | 删 338 行；需先确认库里时间戳只有 `isoformat()` 写出的几种形式 | S–M |
| T6 | 发给 Go Agent 和 Sub-Store 的请求体不再模仿 `requests` 的 `json=`（ASCII 转义、`", "` 分隔），删掉 `jsonresp.EncodeDevice*`（C6、C7） | 少一遍逐字符转义；代码更直 | S |
| T7 | 小清扫：`pythonFloat`/CPython `resolve`/`expanduser`（C10）、`Path.exists` 3.11 和 `UnicodeDecodeError`（C12）、预览 `Identities` 有序编解码（C13）、MosDNS `!!set/!!omap` 特例（C14）、HTTP 层 Werkzeug 怪癖（C15）、JWT `int()`（C17） | 每项几十到三百行，合计约 900 行 | S |
| T8 | metrics/rankings 里模拟的 Python 异常文本、`sqlite3` 隐式事务、CPython 半偶舍入（C11） | 删掉模拟层，改用普通 `database/sql` 事务 | M |
| T9 | 可选：诊断脱敏和日志搜索里的 CPython Unicode 边角（C16） | 收益低，只在碰到这些文件时顺手做；脱敏范围不能变窄 | M |

依赖：T1a → T1b/T1c（可以并行）→ T1d。T1 和 O10 的 R1/R2（读方借用快照）冲突面很大，**建议 T1a 先于 R1/R2 落地**，
或者 R1/R2 先做「借用」，T1 再换类型（借用 API 不受影响）。T2–T9 相互独立。

**T2–T4 完成情况（2026-11-23，分支 go-o18-rest）：**

- T2：batch-34（R8/R9）没有改 `configDigest`。现在 `repo.ConfigAt` 返回解析配置时的已提交 head，
  订阅/聚合 provider 下载把它交给 `provider.Serve`；合并键是 (head, 配置空间, 种类, 资源 id, format/regex/exclude_regex)。
  在写事务里读到的工作副本没有 head，单独渲染、不合并。
- T3：`jsondecode` 改为 `encoding/json/v2` + `json.DefaultOptionsV1()` + `MatchCaseInsensitiveNames(false)`，
  删掉按类型反射重写成员的实现（396 → 211 行，Select 不变）。重复成员仍取最后一个完整值：只有 jsontext 报告重复时，
  才用每次调用的 v2 unmarshaler 把发往普通结构体/map 的对象缩成最后一组成员；自定义 codec 和 `json.RawMessage`
  照旧收到原始字节（model.Fields 和 Agent 协议照旧拒绝重复）。**没有改成 v2 默认的「拒绝重复」**：那会让现在接受的
  请求变成 400。唯一差异：畸形 struct 标签不再按 v1 的「前缀 + 大小写不敏感」读取（代码库里没有）。
- T4：`HelperError` 和类名字符串删除；仍在产生的两处 KeyError 换成 `*MissingError{Kind: ErrUnknownRootKind/ErrMissingLibraryRule}`（文本不变）。
  `repo.IsValueError` 换成哨兵 `repo.ErrRejected` + `errors.Is`（16 处），`upgrades.IsValueError` → `IsRefused`，
  `isPythonValueError` → `isRefusal`。状态码与中文文案不变。`installer.ValueError`/`TypeError` 别名删除（用 `ValidationError`/`RenderError`），`agents.ValueError` 改名 `agents.ValidationError`（batch-41 复核）。

---

## 1. 方法

1. grep `Python|CPython|PyYAML|json.dumps|ensure_ascii|byte-identical|original bytes|byte for byte|stored order|member order`，
   排除测试后命中 422 行。大多数只是「从哪段 Python 移植来」的说明，不是对标机制，逐个排除。
2. 重点读：`model`（全部）、`repo`（model_rows、section_snapshots、merge_nodes、lock、errors）、`store`、`jsonresp`、
   `jsonstore`、`jsondecode`、`yamlx`、`yamlbridge`、`deploy`、`provider`、`devhttp`、`agents`（remote、registry、metrics、
   monitor）、`health`、`notifications`、`taskstatus`、`auth`、`httpx`、`api/agents`、`api/logs`。
3. 调用面统计（`internal`+`cmd`，不含测试和 model 包自身）：`MemberJSON` 354 处 / 75 文件，`SetJSON` 105 / 41，
   `.Clear("…")` 22 / 13，`Fields` 增删查 86 / 24，`model.DecodeRecord` 56 / 25，`model.Map[...]` 55，
   `JSONSwitchEnabled` 6，`model.Marshal` 168 / 80。model 里声明 `raw Raw` 的记录类型约 100 个。
4. 生产快照：在 .32 上把 `configflow.sqlite3` 复制到私有目录（0600），写临时测试 `repo/zz_o18probe_test.go`（未提交，已删，
   服务器副本也已删），用 `store.Load` + `decodeModelRows` 解码，再用反射遍历每条记录的 `Raw`：未知成员、类型错误、
   叶子成员「原字节 vs 按类型重编码」、成员顺序、缺失的已声明成员，最后做一次整文档往返和性能测量。

## 2. model 层：能不能「直接去掉」

### 2.1 现在保留了什么

`internal/model`（`record.go` 545 行、`json.go` 552、`map.go` 268、`fields.go` 154、`member.go` 118、`switch.go` 101、
`int.go` 98、`dynamic.go` 55、`textlines.go` 53、`content.go` 47、`string.go` 35，共约 2000 行；测试 3766 行）对每条记录保留：

| 机制 | 做什么（通俗） | 为什么当初存在 |
|---|---|---|
| ① 原字节和顺序（`Raw.source`、每个成员前后的空白片段） | 没改过的成员原样写回，连空格和顺序都不变 | 「和 Python 写的库逐字节一致」，让对照测试比字节 |
| ② 类型错误保原值（R5） | 字段类型不对时解成零值，但写回时仍写原来的错值 | 不丢 Python 库里的「脏数据」 |
| ③ 每成员 baseline | 解码时立即把每个成员再编码一次，写回时比较「有没有改」 | 为 ①② 判断「未改动」；嵌套 d 层的子树被重复编码 d 次，这是 147 B/B 放大的直接原因 |
| ④ 未知成员（`Fields`、无 `Fields` 的记录也在 `Raw` 里保留） | 结构体不认识的成员原样带着 | Python 用 dict，什么都存得下 |
| ⑤ `null` 与缺失区分、`ClearMember`（Python 的 `pop`） | 指针字段存 `null` 和不存是两种状态 | Python `get(k, 缺省)` 对 `null` 和缺失给出不同结果 |
| ⑥ 宽松标量（`Int` 接受 `60.0`/`"60"`，`String` 接受数字） | 读 Python 写的「数字写法不同」的值 | 读旧数据 |
| ⑦ `MemberJSON`/`JSONSwitchEnabled`/`JSONHasContent` | 调用方读「这个成员在库里的原 JSON」再判断（是不是原始字符串、是不是 Python 意义上的真值） | 配合 ②⑥：防止「宽松解出来的值」被当成真值使用 |
| ⑧ `Dynamic.SetNode` 等值保字节 | 树没变就保留原字节 | 同 ① |
| ⑨ `repo/model_rows.go` | 读：把各行拼成整份 JSON 文档再整体解析；写：整份编码再拆回行，空白不同但值相同的行回填原字节 | 同 ① |

### 2.2 生产快照的结果

| 检查 | 结果 |
|---|---|
| 规模 | 43 行，539 KB，2 个配置空间，2271 条记录，9592 个叶子成员 |
| 解码诊断（类型错误，机制 ②） | **0** |
| 未知成员（机制 ④） | **1**：`system.agents[].*` 下 1 个（`has_update`）。根成员只有 `schema_version`（已知） |
| 叶子成员原字节 ≠ 按类型重编码（机制 ①③⑥） | **0**（含数字写法、数字当文本、`null` 变零值、转义差异各类均为 0） |
| 指针字段存 `null`（机制 ⑤） | 40 个，全部在 `domain_discovery.history[]`、`agents[].deployments{*}`、`ipv6_verifications{*}`、`dns_follow_state`、`*ha_publication` 里；带非零缺省值的开关类字段（`enabled`、`library_enabled` 等）**没有**存 `null` 的 |
| 成员顺序和结构体声明顺序不同 | 约 530 条记录（`rule_configs[]` 94、`proxy_groups[]` 60、部署历史 61、规则库 44、`proxies_order[]` 230 等） |
| 已声明但缺失的成员 | 很多（例如 `rule_configs[].url` 94 条都没有、部署历史里 `rollback_*` 61 条都没有）。普通结构体如果不加 `omitzero`，第一次写入会给每条记录补上一堆零值成员 |
| 当前实现整文档往返 | 0 行变化 |
| 性能 | model 解码 60.6 ms / 71 MB（132 B/B）；`encoding/json` → `any` 4.7 ms / 2.1 MB（4 B/B）；model 编码 9.2 ms / 13 MB（24 B/B） |

### 2.3 Python 自己怎么处理「类型错误 / 未知成员」（`backend/common/config_repository.py`）

- Python 不做类型化解码：库里是什么，内存里的 dict 就是什么。没有「错类型 → 缺省值」这一步，所以也谈不上「覆盖」。
- 读和每次写都跑 `_validate_document`（第 2416 行起）：根版本号、三个节必须是对象、`system` 只允许
  `system_config/backup/agents/_revision`（多一个键就拒绝，第 2430 行）、各资源列表必须是带字符串 id 的对象等。
  **所以凡是被校验的路径，Python 库里根本存不了错类型或未知成员。**
- 写入时：`update_system_transaction` 只重建 `system_config/backup/agents`（第 1572 行），`system` 顶层的其他键会被丢掉；
  `_update_profile_locked` 只把更新里带着的 `PROFILE_FIELDS` 键整体替换（第 1291 行），配置空间里其他成员不动；
  共享资源同理（`SHARED_FIELDS`）。嵌套在未校验路径里的错类型值或未知成员，Python 会一直带着，除非那个键被整体重写
  （配置空间和共享资源的保存通常是前端把整节发回来，此时存的就是前端回传的值）。
- 结论：**Python 不是「覆盖」，而是「原样带着」**。所以 Go 去掉 ② 严格说不是「对齐」，而是一处有意的行为变化：
  只影响库里本来就有脏数据的情况，而生产快照里这种情况是 0；切换后 Go 是唯一的写入方，只会写类型正确的值。

### 2.4 推荐方案：普通结构体 + 存储解码的宽松叶子选项

1. **记录用普通 Go 结构体**，编解码用 `encoding/json/v2`（Go 1.27 已正式提供，测试服务器上 `go doc encoding/json/v2` 可用；
   model 现在已经在用同一套里的 `jsontext`）。所有字段加 `omitzero`，避免第一次写入补出零值成员。
   **带非零缺省值的字段必须是指针**（Python 非测试代码里有 165 个键用 `get(key, 非 None 缺省)`，最常见 `enabled` 142 处、
   `library_enabled` 26 处）；现在的 model 已经基本这样声明。`nil` 同时表示「缺失」和「`null`」，读取方一律取缺省值。
   与 Python 的差异：`enabled: null` 在 Python 里是「关」，这里会变成「开」；生产里没有这种值，列为行为变化。
2. **读存储时用宽松选项**（`json.WithUnmarshalers`），只作用于 bool/string/整数/浮点叶子：类型不对就跳过这个值、取零值、
   记下 JSON 路径，加载完打一次警告；整数接受 `60.0`、`6e1` 这类整数写法（替代 ⑥ 的 `Int`/`String` 自定义类型）。
   API 请求体不用这个选项，照常严格校验。这样一个坏值只影响它自己，不会让整行或整份文档读不出来。
3. **不保留未知成员。** 加载时如果发现未知成员，打一次警告（只记路径）；测试里用 `RejectUnknownMembers(true)` 解码前端的
   典型请求，防止前端新增字段而后端漏声明，导致字段被悄悄丢掉。可选的保守做法：只在「前端整对象回写」的 5 类条目
   （`proxy_groups[]`、`rule_configs[]`、`subscriptions[]`、`nodes[]`、`subscription_aggregations[]`）上加一个
   `json:",unknown"` 的 `map[string]jsontext.Value`，为空时没有成本。我倾向于不加，靠测试守住。
4. **必须保留的两点：**
   - 用户数据作键的对象（`agents[].deployments{*}`、`ipv6_verifications{*}`、`device_notes`、`client_fixed_ips`、profiles 节顺序）
     保持插入顺序：历史保留「最近 N 条」和界面显示都依赖这个顺序。用一个简单的有序映射（键值切片），不带 baseline 和原字节。
   - `Dynamic`（`profile.oxidns`、`routers.items[].dns_guard_state.resources[].config`）保持为 `jsontext.Value`/`json.RawMessage` 原样存，
     去掉 `SetNode` 的「等值保字节」比较即可。
5. **`repo/model_rows.go` 改成逐行编解码**：每个存储行直接解码进对应字段，写入时逐字段编码后和库里的行比较，
   不同才写。不再拼整份文档、不再回填原字节。切换后第一次写入会把各行改写成 Go 的规范格式（一次性），之后稳定。
6. 读取方（354 处 `MemberJSON`、6 处 `JSONSwitchEnabled`）改成直接读类型化字段。这些判断大多是在防「宽松解出来的值」
   （例如 `AgentEnabledModel` 区分 `enabled` 缺失和 `null`、`storedText` 只认原始 JSON 字符串、心跳把上报值和库里的原 JSON
   比较）。宽松选项把错类型变成零值并告警之后，这些防线就不需要了。

**删掉的影响**：只在库里有脏数据、或有 Go 未声明的成员时，行为才和 Python 不同（脏值变零值、未知成员丢弃）。生产快照里
分别是 0 和 1（1 个是无用字段）。导入 Python 时代的导出文件走 `import_conversion` 的显式转换，不依赖 ④。
**收益**：冷加载/写入/任何「整条编码」的路径成本降一个数量级；model 核心约 2000 行和 3766 行测试可删；
`SetJSON`/`Clear`/`MemberJSON` 约 480 个调用点变成普通字段读写；O10 的 R5 不用做，R1 的浅拷贝也不再需要
「Raw 写时复制」这个前提。

## 3. 残留清单

### A 类：外部边界，必须保留（10 项）

| # | 位置 | 做什么 | 为什么保留 | 代价 |
|---|---|---|---|---|
| A1 | `devhttp`、`rulefetch/transport.go` | 失败信息里带 `requests`/`urllib3` 的异常类名（「无法连接 RouterOS：ReadTimeout」） | 是中文用户文案的一部分，文案按规定逐字一致 | 一张映射表，可忽略 |
| A2 | `deploy/core.go` `Fingerprint` | 部署内容指纹 `v1:`：固定字段顺序、紧凑 UTF-8、把 ` / ` 还原成原字符 | 指纹存在部署回执里，和 Agent 上报比对；改了会在切换时让所有 Agent 一次性显示不一致/重推。注意 `RulesFingerprint` 已经有意让 Python 时代的就绪回执失效 | 几行 |
| A3 | `devhttp.EncodeJSON`、`routers/ikuai.go` `IkuaiRequestBytes` | 发给 RouterOS/iKuai 的 JSON 把非 ASCII 转成 `\uXXXX`；按 `json.dumps` 的格式估算请求大小 | 写到路由器的语义：旧固件对原始 UTF-8 的处理没测过；分批上限是按这种长度在真机上测出来的 | 一次转义 |
| A4 | `rules/regex.go`（967 行） | 把 Python 正则语法翻译给 RE2/regexp2 | 用户存的过滤/重命名正则就是 Python 语法 | 已是最小实现 |
| A5 | `subs/psl.go` | 固定版本的公共后缀表（tldextract） | 决定域名归组结果 | — |
| A6 | `netipx`（CPython 3.11 地址分类表）、`convert/ipaddr.go` | 私有/全局地址判断、EasyTier 地址严格解析 | 校验必须接受/拒绝同样的输入 | — |
| A7 | `jsondecode`（语义部分） | 成员名大小写精确匹配 | 防 `Password` 之类大小写别名覆盖字段；和 Python `dict.get` 一致。实现可换 v2，见 C18 | — |
| A8 | `logx` | Python 模块名做 logger 名、级别号和行格式 | 日志页按行格式高亮/筛选 | 无 |
| A9 | `setup/presets.go`、`tasks`、`routersplit/messages.go` 等 | 列表按 Python 里的顺序输出 | 列表顺序本身是 API 返回值 | 无 |
| A10 | `installer/docker_ha_guide.go`、`docker_ha_compose_text.go` | 生成的命令/脚本里含 Python 片段 | 这些文本在用户主机上执行，是生成内容本身 | 无 |

### B 类：读旧数据需要（5 项）

| # | 位置 | 做什么 | 建议 |
|---|---|---|---|
| B1 | `store`（`rows.go` 插入序映射、`position` 列、字段值不解码） | 按行存，每行带位置 | 保留：表结构要求 `position`；顺序本身没有含义，Go 写什么顺序都行。代价很小 |
| B2 | `jsonresp.DecodeStored` / `NormalizeNonFinite` | 读 Python `json.dump` 写出的 `NaN/Infinity` | 保留（metrics、通知、任务状态等旧文件可能有）；Go 不会再写出这种值 |
| B3 | `model.Int`/`model.String` 的宽松解码 | 读 `60.0`、`"60"`、数字当文本 | 生产 0 例。保留「宽松读」，改成 2.4 第 2 点的解码选项，删掉自定义类型和写回原值 |
| B4 | `model.Dynamic` | 无 schema 的 OxiDNS 设置、DNS 守护资源配置原样存 | 保留为原始 JSON；删掉 `SetNode` 等值保字节（归入 C1） |
| B5 | `agents/runtime_state.go` `readLegacyRows`、`agents/metrics/history.go` 旧文件加载、`deploy/previews.go` 读旧预览元数据 | 读 Python 时代的旧行/旧文件 | 保留「读」；其中模拟 Python 报错文本和写回格式的部分归入 C11、C13 |

### C 类：可删（18 项）

| # | 位置 | 做什么 | 为什么当初存在 | 删掉的影响 | 代价/收益 | 建议 |
|---|---|---|---|---|---|---|
| C1 | `model` 核心（2.1 的 ①②③④⑤⑧） | 原字节、顺序、baseline、未知成员、错值保留、`null`/缺失区分 | 逐字节对标 Python 存储 | 见 2.4：只影响脏数据/未声明成员，生产 0/1 | 解码 132 B/B、60 ms；约 2000 行 + 3766 行测试 | T1a–T1c |
| C2 | `repo/model_rows.go` | 行 → 整份 JSON → 再解析；写回时整份编码再拆行并回填原字节 | 同上 | 第一次写入各行变成 Go 格式（一次性） | 每次冷加载/事务多一遍整文档编码+解析 | T1a |
| C3 | model 外 25 个文件的 `model.DecodeRecord`/`EncodeRecord`（`health/check`、`health/latency.go`、`notifications/types.go`、`refresh/types.go`、`substore/records.go`、`taskstatus`、`subs/records.go`、`agents/monitor/*`、`agents/trafficstore` 等，56 处） | DATA_DIR 文件和 Agent 回复也按「保原字节和顺序」读写 | 同上 | 文件第一次重写后格式变化；Agent 回复里需要透传给前端的部分改用 `jsontext.Value` | 中等 | T1d |
| C4 | 调用方的「原值」判断：`MemberJSON` 354 处、`JSONSwitchEnabled` 6 处、`JSONHasContent`、`storedText`/`originalText`/`storedAbsent` 等，以及心跳「上报值与库里原 JSON 比较」（`agents/registry.go:178`） | 读库里的原 JSON 判断是否是原始字符串/Python 真值 | 配合 ②⑥ 防宽松值被误用 | 宽松值变零值并告警后，这些判断不再需要；心跳改为比较类型化值 | 75 个文件 | 随 T1b/T1c |
| C5 | `provider/response.go` `configDigest` | 对整份配置做 `sort_keys` 规范化 JSON 再 sha256，当作合并请求的键 | 照搬 Python 的 `json.dumps(sort_keys=True)` | 只是进程内的键，换成 (head, 配置空间, 资源 id) 无外部影响 | 每次生成器请求一整份配置的编码+重排 | T2 |
| C6 | `jsonresp.EncodeDevice`、`EncodeDeviceUTF8`（`agents/remote.go`、`agents/monitor/*`、`agents/upgrades/*`、`api/agents/upgrades.go`，9 处） | 发给 Agent 的 JSON 模仿 `requests` 的 `json=`：ASCII 转义、`", "` 分隔 | 和 Python 发出的字节一致 | Go Agent 用 `encoding/json` 解析，不对请求体签名或算哈希（已查 go-agent），格式无关 | 逐字符转义 | T6 |
| C7 | `substore/httpclient.go` | 请求体同上，默认 `python-requests` User-Agent | 同上 | Sub-Store 解析 JSON，不看 UA | 小 | T6 |
| C8 | `repo/errors.go` `HelperError.Class`（`ValueError/TypeError/KeyError/AttributeError`）、`IsValueError`、`api/agents/docker_ha.go` `isPythonValueError` | 用 Python 异常类名字符串分类错误 | 照搬 Python 的 `except ValueError` | 无外部影响（状态码和中文文案不变） | 36 处判断；违反「不模拟 Python 异常」 | T4 |
| C9 | `taskstatus/isotime.go`（338 行，26 个调用方） | 移植 CPython 3.11 C 实现的 `datetime.fromisoformat` | 读 Python 写的时间戳 | 只要覆盖 `isoformat()` 实际写出的几种形式（有无微秒、`Z`/`+HH:MM`）即可；`fromisoformat` 接受的周日期等形式没人写 | 删 338 行 | T5（先用快照确认时间戳形式） |
| C10 | `repo/lock.go` `pythonFloat`（CPython `float()` 语法 + Unicode 数字）、`resolve`/`realpath`（CPython 3.11 `Path.resolve`）、`repo/repository.go` `expandUser` | 解析锁超时环境变量、规范化路径 | 照搬 Python | 环境变量写全角数字之类不再被接受；路径用 `filepath.EvalSymlinks`/`os.UserHomeDir` | 约 150 行 | T7 |
| C11 | `agents/metrics/history.go`、`traffic.go`、`agents/monitor/rankings.go` | 模拟 `OverflowError("Python int too large…")`、`strerror` 文本、naive/aware 时间比较、`sqlite3` 3.11 隐式事务、CPython 半偶舍入 | 对照测试比 Python 的异常和数值 | 只影响畸形输入的内部报错文本 | 中等 | T8 |
| C12 | `agents/upgrades/artifacts.go`（`Path.exists/is_file` 3.11 语义）、`substore/env.go`（`UnicodeDecodeError`） | 模拟 Python 文件判断和解码错误 | 同上 | 无外部影响 | 小 | T7 |
| C13 | `deploy/previews.go` `Identities` | 为 Python 写的预览元数据保持成员顺序的手写编解码 | 读写 Python 预览文件 | 预览是短期文件；顺序无语义 | 小 | T7 |
| C14 | `mosdns/generate.go` `UnsupportedYAMLError` 的 `!!set/!!omap/!!pairs` 特例 | 因为「Python 用 yaml.Dumper 写，Go 写不出同样字节」而拒绝 | 字节对标 | 保留「递归别名拒绝」（安全），删掉按 PyYAML 输出决定的特例；注释里还提到已删除的 jsonx | 小 | T7 |
| C15 | HTTP 层 Werkzeug 怪癖：`auth/auth.go` 公开路径允许末尾 `\n`（`re.match` + `$` 的怪癖）、`httpx/ctx.go` `currentURL` 按「Python 在此 500」处理、`api/agents/downloads_range.go`（300 行）Latin-1 空白和 Werkzeug Range/ETag 解析 | 模拟 Werkzeug 对畸形请求的行为 | 对照测试 | 畸形请求的结果变成标准库行为；Range 正常请求结果不变（可用 `http.ServeContent` 承担） | 约 350 行 | T7（Range 部分要保留 206/416 状态码和中文提示） |
| C16 | `agents/diag/diag.go`（Python Unicode `\s`/`\b`，`IGNORECASE` 下 U+0130）、`api/logs/logs.go`（`pyLower`、universal newlines） | 诊断文本脱敏和日志搜索按 CPython 的 Unicode 规则匹配 | 对照测试 | CPython Unicode 边角已裁定不追；但脱敏范围不能变窄 | 约 300 行 | T9（可选） |
| C17 | `auth/jwt.go` `claimInt` | 按 Python `int()` 解析 JWT 数字声明 | 切换时 Python 签发的令牌仍有效 | 令牌过期一轮后不再需要 | 小 | T7 |
| C18 | `jsondecode`（实现部分，396 行） | 自写成员筛选/重写，实现大小写精确匹配和「重复成员取最后一个完整值」 | encoding/json v1 默认大小写不敏感，且重复成员会合并 | v2 默认就是大小写精确；重复成员 v2 默认拒绝（Python `json.loads` 取最后一个），畸形请求会变成拒绝——状态码要和现在一致 | 删约 400 行；148 个使用方 API 不变 | T3 |

### 不算残留、但审计中确认过的

- `yamlx.Marshal` 的 `prepareStrings`/`preserveScalarTag`/`copyGraph`/`checkGraph`：是 yaml.v3 的正确性补丁（输出必须能读回同样的值、
  YAML 1.1 读者把 `y/no` 当布尔），不是对标 PyYAML。它们是 O10 R9 的性能对象（`preserveScalarTag` 对每个非字符串标量做一次
  Marshal+Unmarshal 探测），按 R9 处理即可。
- `yamlx.Edit` 保留注释：用户编辑自定义配置时保留自己的注释，是功能。
- `repo/merge_nodes.go` 三方合并、`section_snapshots.go`：并发编辑保护是功能；在 JSON 成员上做合并本身没问题，
  T1 之后成本跟着降。
- `notifications` 的 `RouterDigest`、确认指纹、`subs/host_extract.go` 摘要：已经是 Go 自己定义的规范化 JSON（D2），不对标 Python。
- `jsonresp.EncodeUTF8`（pushdiff）：为字节预算保留 U+2028 原字符，是 Go 自己的理由。
- `jsonstore.Write`：已经是原生 `encoding/json`，注释里 `ensure_ascii=False` 只是说明。

## 4. 复现

探针没有提交（按规定不提交读快照的代码）。要复现，在任务分支里临时放一个 `repo` 包内部测试：`store.New(path).Load()` →
`decodeModelRows` → 用反射遍历每条记录未导出的 `raw.members`（`name/known/invalid/value/baseline`）和 `Map` 的
`state.entries`，按「未知 / 错类型 / 原字节≠baseline（按 null→零值、数字写法、数字↔文本、只差转义/空白分类）/ 顺序 / 缺失」计数，
只输出路径模式（下标换成 `[]`、映射键换成 `{*}`）和个数。在测试服务器上对快照的私有副本运行：

```bash
RT_HOST=sunbroke@192.168.0.32 rt -e O18_DB=<私有副本路径> -- bash -lc 'go test -count=1 -run TestO18Probe -v ./internal/repo'
```

## 5. T8 结果（C11，2026-11-23）

- `monitor/rankings.go`：去掉模拟 `sqlite3` 隐式事务的 `rankConn.inTx`；改动数据的操作（存槽、整理）用 `database/sql` 显式事务
  （`_txlock=immediate`），只读操作不开事务。
- `metrics/traffic.go`：手写 `BEGIN IMMEDIATE`/`COMMIT`/`ROLLBACK` 换成 `sql.Tx`；删掉 `SQLiteError`（`sqlite3_errmsg` 文本）、
  `OSError`/`makedirs`（`[Errno N] …` 文本）、「cannot rollback」文本匹配和「ROLLBACK 失败顶替原错误」；计数溢出/无效改成哨兵错误
  （中文常量）。`metrics/history.go`：去掉 `OverflowError` 的三种 Python 文本，时间窗越界统一为 `ErrHoursOutOfRange`（仍是 500）；
  `strerror` 的 ESTALE 特例删除；`TypeError`/`AttributeError` 换成哨兵错误（中文文案不变）。
- 舍入：`isoUTC`（流量查询的 `start`）和 `fromTimestampMS`（看板的 `collected_at`）对外可见，半偶舍入在 1/128 s 之类的平局
  或远期毫秒值上与常规舍入结果不同，按「保留对外数值」保留 `math.RoundToEven`，注释改为说明原因。
- naive/aware 时间：只有手改文件才会出现带时区的时间戳，跳过它们是存储读取规则，不改。
- 生产快照私有副本往返：改动前后同一组操作（查询全部聚合、保存、+8 天整理/裁剪、保留、删除；排行查询、整理、存槽、重复存槽）
  的结果哈希和行数完全一致，`integrity_check` 正常。
