# O17「地道 Go」复查（2026-11-23）

分支 `go-audit-o17`（= `go@e19aa9431`）。只审计、出清单，不改产品代码。范围：`server/internal`、`server/cmd`、
`backend/agents/go-agent` 的非测试代码；**`internal/model` 及其编解码不在本次范围**（O18 T1d 正在复核）。
已经在 O18 清单里的项目（A1–A10、B1–B5、C1–C18）不重复列，只在相关处引用。

## 结论（先看这里）

**共 30 项：第 1 类「照 Python 搬来的结构、流程和标准库替代品」13 项，第 2 类「O18 没搜到的 Python 行为照搬」8 项，
第 3 类「规模、重复、死代码、命名」9 项。** 没有发现会写坏设备或数据的问题；静态检查报出的几处「值算了没用」都是
多余代码，不是逻辑错误（逐条核过，见 3.2）。

总的判断：

- 代码已经基本是 Go 的写法，**剩下的 Python 痕迹集中在三处结构上**：
  ① 后台循环靠「每 2–5 秒看一眼有没有变化」而不是「变了就通知」（`IdleRounds`，1.1）；
  ② Agent 的任务状态和历史（部署、规则同步、DNS 联动、HA、IPv6 校验）写在配置文档里，每次状态变化都算一次配置提交（1.4）；
  ③ 一些业务层还在拿「字典」干活：OxiDNS 整个包在无类型的 YAML 节点上做 1282 次 `get(...)`（1.7），
  RouterOS 行是 `map[string]any`（1.8），6 个 CRUD 接口的请求体是 `map[string]json.RawMessage`（1.6）。
  这三处都是 L/M 级改动，碰存储布局或设备写入路径，**建议切换后做**。
- **切换前值得做的都是零外部影响的清理**：死代码（服务端约 180 个生产不可达函数 + 34 个放在非测试文件里的测试钩子；
  go-agent 约 40 个函数、近千行）、Python 异常名、`X`/`XContext` 双份 API、重复的小工具函数。
  另有 3 处很小的「Python 怪行为」（`true` 被当成 IP `0.0.0.1`、「空」按 Python 真假判断、提示里出现 `None`）
  和 `splitlines` 断行规则会改对外行为，需要你裁定。
- 一个和 O18 结论不一致的地方：O18 A6 把 `convert/ipaddr.go` 列为「必须保留的边界」，**但它现在整文件 9 个函数都没人调用**
  （staticcheck U1000 + deadcode 一致），可以直接删（3.1）。

### 切换前建议做的清单（零或极小外部影响）

| 批 | 内容 | 编号 | 外部影响 | 量 |
|---|---|---|---|---|
| 前-1 | 删死代码（服务端 + go-agent），测试专用钩子移到 `export_test.go`，修 staticcheck 报出的多余赋值/空分支/双重取反 | 3.1、3.2、3.3、3.6 | 无 | M |
| 前-2 | Python 异常名清理：`health.ValueError`/`OSError`、`health/check.ValueError`、`metrics.ValueError`+`RetentionValueError`、`agents.PermissionError`、`trafficstore.TypeError/AttributeError/KeyError`、`refresh.LookupError`、installer/upgrades/health 里的 `valueError()`/`typeError()`、`ErrorClass()` 日志标签和死掉的 `rulefetch.errorClassOf` | 3.9、2.5 | 无（状态码和中文文案不变；日志里 `exception_type=` 的值会变） | S–M |
| 前-3 | 重复小工具合一：`ptr`×7（Go 1.26 的 `new(表达式)`，代码库已在使用）、`asciiDigits`×5、`hasControl`×4、`pathExists`×4、`contains`×5、sha256 `digest`×5、吞错误的 `raw(v)`×6、版本比较×2（其中一份用 `math/big` 模拟 Python 无限整数）、`utilx.StrToBool(any)`、5 份 `isoformat`、MCP 布尔参数不再转 `True`/`False` | 3.5、2.6、2.8、1.12、2.2 | 无（输出格式保持） | S |
| 前-4（需裁定） | 3 处 Python 怪行为：IPv4 字段接受 `true`/数字（`IPv4Address(True)`）、Python 真假判断决定「空」、提示文字里的 `None`；以及 4 份 `str.splitlines` 合成一份（按 `\n`/`\r\n`/`\r` 断行） | 2.1、2.3、1.11 | 只影响畸形输入和一条提示文字 | S |
| 前-5（可选） | 原子写文件统一成一个函数（服务端约 20 份、go-agent 11 份；有的 fsync、有的不 fsync，`jsonstore` 就不 fsync） | 3.4 | 无（耐久性变好） | M |

**切换后**按收益排序：后台循环改通知驱动（1.1–1.3、1.10）→ 运行时状态移出配置文档（1.4）→ CRUD 请求体类型化（1.6、2.4）→
OxiDNS 类型化（1.7）→ RouterOS/iKuai 行类型化（1.8，要实验室）→ 显式依赖组装（1.5）→ 拆 `routers` 大包（3.7）→ go-agent 风格（3.8）。
详见第 5 节。

---

## 1. 方法

- 静态检查（.32）：`staticcheck -checks "U1000,S1*,SA4*,SA6*,SA9*" ./...`、`deadcode -test=false ./cmd/...`（服务端）和
  `deadcode -test=false . ./...`（go-agent）。服务端 staticcheck 127 条（含测试文件），deadcode 255 条（去掉 model 后 215 条）；
  go-agent staticcheck 25 条、deadcode 42 条。
- 后台循环：对照 `server/design/o3-concurrency.md` 表 2.4，逐个读注册处（`lifecycle.RegisterPeriodic`）和每轮做的事。
- 结构：按包统计行数/文件数、`map[string]any`/`json.RawMessage` 字典、对 `any` 做取值的辅助函数、YAML 节点导航调用次数、
  全局钩子（`Wire`/`Register`/`Set*`）、重名辅助函数。
- 第 2 类：搜 Python 特征写法（`"True"/"None"` 字面量、JSON 真假表 `"", null, false, 0, [], {}`、`'%s'` 风格、
  `float64` 秒、`str.splitlines` 断行表、`isoformat` 规则「微秒为 0 时省略」、Python 异常类名），再读上下文确认。
- 生产快照、设备一律没碰；本文没有真实主机名/地址。

---

## 2. 第 1 类：照 Python 搬来的结构、流程和标准库替代品（13 项）

### 2.0 后台循环逐个判断（对照 o3 表 2.4）

| 循环 | 现在 | 每轮做什么 | 判断 | 建议 |
|---|---|---|---|---|
| P1 `scheduled-refresh` | 每 30 s | 读全部任务行，挑到期的订阅/规则源刷新 | 按时钟到期，本质是定时任务 | `Interval` 已经是函数：返回「离最近一个到期还有多久」（上限 30 s），配置改动时 `Wake`（1.2） |
| P2 `router-rule-sync` | 每 30 s | 读全部配置空间 + 每台 RouterOS 的回执文件，到期才同步 | 同上；每 30 s 读一遍回执文件 | 同上（1.2） |
| P3 `domain-discovery-auto` | 每 600 s | 逐个配置空间做自动发现 | 周期本来就长，开销小 | 保持 |
| P4 `oxidns-ranking-history` | 每 60 s | 向 OxiDNS 拉排行写 SQLite | 依赖设备数据，必须轮询 | 保持 |
| P5 `metrics-history-flush` | 每 30 s + `Wake` | 把内存里的指标落盘 | 已经是「有脏数据才写」+ 可唤醒 | 保持 |
| P6 `sub-store-idle` | 每 5 s | 读状态文件、读模式，必要时执行一次 `status` 命令，判断是否空闲停机 | Python 多进程留下的「状态文件 + 持久化单调时钟」 | 改内存空闲计时器（1.3） |
| P7 `dns-follow` | 每 5 s + `IdleRounds` | 先比较配置代号，没变就跳过；600 s 强制重看一次 | Python `idle_rounds.py` 的轮询式跳过 | 提交通知 + 心跳事件唤醒（1.1） |
| P8 `oxidns-ha-publish` | 每 2 s + `IdleRounds` | 同上，有未完成的 HA 发布时每轮都跑 | 同上 | 同上（1.1） |
| P9 `ipv6-verification-scheduler` | 每 5 s + `IdleRounds` | 同上 | 同上 | 同上（1.1） |
| P10 `agent-rule-sync` | 每 5 s（首个请求后） | 读全部 Agent，轮转地派 ≤2 个 worker；worker 自己按签名/`RetryAt` 判断 | 远端规则内容会变，需要定时；但「每 5 s 派活再让 worker 判断不用做」是 Python 的形状 | 每台 Agent 计算下次到期时间，按到期唤醒；配置改动/心跳上线时 `Wake`（1.1） |
| P11–P21 | `Spawn` / 定时器 | — | 已是事件驱动 | 保持 |

### 1.1 后台循环用「轮询 + 比代号」代替「变化通知」

> **已完成（分支 go-o17-runtime-state，设计 `design/o17-runtime-state.md` 2.6）**：`IdleRounds` 删除，P7–P10 改用 `agents.Waker`（`repo.OnCommit` + Agent 上线唤醒）。

- **位置**：`internal/agents/idle_rounds.go`（`IdleRounds`）；使用方 `agents/dnsfollow/round.go:17`、`agents/dnsha/publication.go:576`、
  `agents/ipv6/wire.go:218`；`agents/rulesync/service.go:211`（P10，没用 IdleRounds）。
- **现状**：这几个循环每 2–5 秒醒一次，先读配置库的 head 跟上次比，没变就返回；`IdleRounds` 的时钟是 `func() float64`
  秒（Python `time.monotonic`），按仓库对象弱引用建表（照 Python `WeakKeyDictionary`），但进程里只有一个仓库。
- **为什么算问题**：这是 Python 多进程下只能靠轮询的做法。Go 是单进程，`lifecycle.Periodic` 已经有 `Wake *Signal`
  （metrics flush 在用），完全可以「提交时通知」。现在的空转本身不贵（HANDOFF：空闲 CPU 1.9% vs Python 1.5%），
  但逻辑分散：每个循环都要自己判断「这轮算不算空闲」，判断错了就会 600 s 才反应。
- **建议改法**：`repo` 提交成功后 `Notify` 一个进程级 `lifecycle.Signal`（或按订阅者各一个）；心跳状态变化（上线/离线/版本变化）
  也通知；循环 `Wake` 绑定它，`Interval` 改成长兜底（600 s，即现在的 `RecheckSeconds`）。删掉 `IdleRounds`。
- **影响面**：不改 API/协议/设备写入；反应只会更快。风险是漏掉某个唤醒来源（比如只靠设备回报推进的 HA 步骤），
  所以 HA 发布进行中时保留短间隔。
- **工作量**：M。**优先级**：中（切换后第一批）。

### 1.2 定时任务固定 30 秒扫描，而不是按到期时间睡

> **已完成（同上）**：P1/P2 按最近到期时间休眠（1 s–10 分钟），配置提交唤醒。

- **位置**：`internal/refresh/rows.go:212`、`internal/routers/rule_sync_tasks.go:38` / `rule_sync.go:486`。
- **现状**：每 30 s 读全部任务（路由器同步还要读每台的回执文件），再判断有没有到期。
- **为什么算问题**：照搬 Python 调度器写法；`Periodic.Interval` 本来就是 `func(ctx) time.Duration`，可以直接返回下次到期。
- **建议改法**：每轮结束算「最近到期时间 − 现在」（夹在 1 s～30 s 或更长），任务配置修改、立即执行时 `Wake`。
- **影响面**：无外部变化；到期任务的启动误差从最多 30 s 变成秒级（更准）。
- **工作量**：S。**优先级**：低。

### 1.3 Sub-Store 空闲停机靠 5 秒轮询 + 状态文件里的单调时钟

- **位置**：`internal/substore/runtime.go:505`（`Tick`）、`:623`（`idlePeriodic`）、`runtime_state.go`（`LastUsed float64`）、
  `env.go:37–51`（`Monotonic()` 是 Python `time.monotonic` 的秒数）。
- **现状**：每 5 s 读状态文件、读模式，状态为空时还要跑一次进程管理命令；「上次使用时间」以 CLOCK_MONOTONIC 秒写进文件。
- **为什么算问题**：这是 Python 多 worker 之间共享状态的做法。Go 单进程里「上次使用」放内存就够，重启后本来就该重新判断。
- **建议改法**：内存里记上次使用时间，用一个可重置的定时器（每次转换请求后 `Reset(idleSeconds)`），到点再拿 `work` 锁停机；
  状态文件只保留需要跨重启的「failed + 错误信息」。
- **影响面**：无外部变化（`/api/sub-store/status` 的 `idle_seconds` 等字段照旧）。
- **工作量**：S–M。**优先级**：低。

### 1.4 Agent 的任务状态和历史写在配置文档里

> **已完成（同上，存储版本 2）**：状态成员存进同库 `agent_state` 表，与配置同事务；导出/备份只含配置，恢复后按恢复标记 fail closed。

- **位置**：`model.Agent` 的 `Deployments`、`LatestDeployment`、`LatestUpgrade`、`DNSFollowState`、`OxiDNSHAState`、
  `IPv6Verifications`、规则同步状态（`agents/rulesync/service.go:295 saveState` 等写入方）。
- **现状**：心跳已经移到内存（`agents/runtime_state.go`），但部署历史、规则同步/DNS 联动/HA/IPv6 校验的运行状态仍在 Agent
  记录里，每次状态变化都是一次配置事务，配置 head 跟着变。
- **为什么算问题**：这是 Python 「一切都放进一份 JSON 文档」的结构（HANDOFF 2026-11-21 维护者点名的「histories inside Agent
  records」）。后果：运行状态一变，1.1 的「配置没变就跳过」、Agent 视图缓存、生成器缓存都失效；配置文档和备份/导出里混着运行时噪音；
  `saveState` 还要靠「序列化成 JSON 再比较」来避免无意义写入（1.9）。
- **建议改法**：运行状态和历史放到独立的存储（DATA_DIR 下一个 SQLite 表或按 Agent 的 JSON 文件，和 `deploy` 回执同类），
  配置文档只留用户配置；读接口照旧把两者合并输出。一次性迁移：启动时把文档里的状态搬出去。
- **影响面**：API 输出不变；**存储布局变化**（需要迁移和回退方案；WebDAV 备份内容会少这些状态）。
- **工作量**：L。**优先级**：中（切换后，等 O18 T1d 落地再做）。

### 1.5 全局钩子组装：30 个 `Wire`、102 个包级函数变量

- **位置**：`internal/server/server.go:114–147`（`wirePackages`，顺序敏感，注释里写着「必须最后注册」）；
  非测试代码里 79 个 `Set*/Reset*/Register*` 导出函数、102 个包级 `var x = func…`；服务定位器 `repo.GetRepository(ctx)`、`agents.Get(ctx)`。
- **现状**：包之间互相需要时，不是在构造时传依赖，而是启动时往对方的全局变量里塞函数（例如 `routers.IPv6HostRequest = func…`、
  `repo.RegisterHooks(...)`、`tasks.SetRowBuilder(...)`）。
- **为什么算问题**：这是 Python「晚导入/猴子补丁」解决循环导入的方式。Go 里会带来：初始化顺序依赖、测试之间共享可变全局状态
  （违反 AGENTS.md 测试规则，所以才有一堆 `Reset*`）、看代码不知道某个钩子是谁填的。
- **建议改法**：在 `server` 里显式构造服务对象（`repo` → `agents.Manager` → `deploy` → …），通过接口参数传给需要的包；
  钩子改成构造参数。可以按子系统逐步换，`wirePackages` 一项一项变少。
- **影响面**：无外部变化。
- **工作量**：L。**优先级**：低（切换后，和 3.7 一起做最划算）。

### 1.6 CRUD 接口把请求体当字典处理

- **位置**：`api/rules`（`type record map[string]json.RawMessage`，`service.go`/`compose.go` 里几十处 `body["dns"]`、`rule["policy"]`）、
  `api/aggregations`、`api/subscriptions`（`object`）、`api/nodes`（`record`）、`api/proxygroups`、`api/rulelibrary`（`= model.Fields`）、`mcp/invoker.go`。
- **现状**：先把 JSON 解成「成员名 → 原始字节」，再一个个 `textValue(item["x"])`、`notFalse(item["enabled"])` 取值和判断。
- **为什么算问题**：照搬 Flask `request.json` 字典；类型检查散在各处，同一字段不同接口判断不一致的风险高，
  `notFalse`/`exactTrue` 这类辅助函数就是 Python 真假判断（与 2.4 同源）。
- **建议改法**：每个接口定义请求结构体，需要区分「没传」和「传了零值」的用指针或 `omitzero` + `jsontext.Value`；
  错误文案仍用现有常量。
- **影响面**：正常请求不变；畸形请求的 400 文案可能变（要逐个接口对照现有测试）。
- **工作量**：M（6 个包，每个 S–M）。**优先级**：中（切换后）。

### 1.7 OxiDNS 整个包在无类型 YAML 节点上做「字典导航」

- **位置**：`internal/oxidns/`（16.6k 行，36 个文件）；`node.go` 里的 `get/items/str/isNone/isMap/isSeq/val`，
  全包调用 `get(` 1282 次、`isMap(` 223 次、`isStr(` 196 次；`node.go:16` 注释「pointer identity is object identity」（Python 对象语义）；
  `native.go:408` `if !(!isNone(args))`（`if not args` 的机械翻译）。
- **现状**：设置、生成、校验、编辑全部在 `*yaml.Node` 树上按名字取值。
- **为什么算问题**：这是业务层最大的一块「dict 传来传去」。需要保留用户手写 YAML（注释、未知插件）的只是编辑器和原生配置透传两处，
  其余（设置、规则 DNS、上游、HA）都是我们自己的 schema。
- **建议改法**：ConfigFlow 自己拥有的部分定义结构体（`yaml.v3` 解码），只在「保留用户 YAML」的边界用 `yaml.Node`；
  先从 `settings.go`、`ruledns*.go`、`upstreams.go` 开始。
- **影响面**：生成结果必须结构一致（用现有 golden/configdiff 守住）；不改 API。
- **工作量**：L。**优先级**：中（切换后；收益是可维护性，不是性能）。

### 1.8 RouterOS / iKuai 行是 `map[string]any`

- **位置**：`internal/routers/rosnat.go:19`（`type Row map[string]any`）、`routeros.go:675 rosText`、`:708 rosTruthy`、
  `ikuai.go`（27 处 `map[string]any`）、`ikuai_apply.go`（12 处）、`ikuai_dhcp.go:188 truthyValue`、`clients_ros.go:55 clientTrue`。
- **现状**：设备返回的每一行用 `any` 存，再用 `rosText`（`string/json.Number/int/int64/bool` 都转成文本）和 `rosTruthy` 取值。
- **为什么算问题**：RouterOS REST 只返回字符串，`map[string]string` 就够；iKuai 的接口有固定字段，可以用结构体。`any` 让每个取值点都要防类型。
- **建议改法**：ROS 行改 `map[string]string`；iKuai 按接口定义结构体（`json.Number` 保留数字）。
- **影响面**：**写设备路径**，要在 PVE 实验室回归；协议和写入语义不能变（O18 A3 的 `\uXXXX` 转义保持）。
- **工作量**：M。**优先级**：低（切换后，跟实验室批次走）。

### 1.9 用「序列化成 JSON 再比较/摘要」判断有没有变化

- **位置**：`bytesEqualJSON`/`CanonicalDigest`/`canonical*JSON` 共 75 处，`routers` 15、`setup` 4、`oxidns` 3、`agents/rulesync` 3、
  `agents/dnsfollow` 3、`deploy` 2 等；例如 `agents/rulesync/service.go:308–318`（注释还写着「包括未知成员」，O18 T1 之后已不存在）。
- **为什么算问题**：Python 比较 dict 的习惯。Go 结构体可以直接 `==`（可比较时）或写 `Equal` 方法；每次比较都多两次编码。
- **建议改法**：只在摘要要落盘/发给设备的地方保留规范化 JSON（O18 A2 的部署指纹）；进程内比较改结构体比较。
- **影响面**：无外部变化。
- **工作量**：M（逐处换）。**优先级**：低。

### 1.10 时间用 `float64` 秒传来传去

- **位置**：19 个文件，15 个可注入的 `func() float64` 时钟字段（`agents/dnsha`、`dnsfollow`、`rulesync`、`ipv6`、`monitor`、
  `metrics/traffic.go`、`substore/env.go`、`auth/jwt.go`、`installer/docker_ha_enrollment.go` 等）；例如 `rulesync/worker.go` 的
  `nowSeconds(s.now())` 与 `RetryAt` 比较。
- **为什么算问题**：照搬 `time.time()`/`time.monotonic()`。代码库已有 `clock.Clock` + `lifecycle.ClockFrom(ctx)`，现在是三套时钟并存。
- **建议改法**：内部一律 `time.Time`/`time.Duration`，时钟统一用 `lifecycle.ClockFrom(ctx)`；只在存储/协议字段（如 JWT `exp`、
  存储里的 `retry_at` 秒数）做一次转换。
- **影响面**：无外部变化（存储字段格式保持）。
- **工作量**：M。**优先级**：低（和 1.1 一起做）。

### 1.11 `str.splitlines` 被移植了 4 份

- **位置**：`rulefetch/text.go:28`、`oxidns/settings.go:517`（注释「is str.splitlines」）、`agents/diag/diag.go:297`
  （O18 C16 已提到）、`redact/redact.go:571`；`installer/templates.go:104` 转调 diag 那份。
- **现状**：都按 Python 的 Unicode 断行表（`\v`、`\f`、`\x1c–\x1e`、U+0085、U+2028/2029）切行。
- **为什么算问题**：Python 标准库替代品，而且 4 份写法各不相同。`rulefetch` 用它算编辑器里的行号偏移，但前端编辑器只认
  `\n`/`\r\n`/`\r`，含这些罕见字符时行号反而对不上。
- **建议改法**：一个共享函数，只按 `\n`、`\r\n`、`\r` 断行（或 Go 1.24 `strings.Lines` + 处理 `\r`）。
- **影响面**：只影响含罕见控制字符的文本；脱敏范围不能变窄（redact 那份要单独确认）。需要裁定（放在前-4）。
- **工作量**：S。**优先级**：中。

### 1.12 Python `datetime.isoformat` 规则写了 5 份

- **位置**：`agents/time.go:18`、`agents/metrics/history.go:198`（外加 `naiveNow` 把本地时间当 UTC，模拟 Python naive datetime）、
  `api/logs/logs.go:287`、`api/stats/stats.go:88`（`isoformatZ`）、`substore/text.go:67`（`+00:00`）。
- **现状**：都实现「微秒为 0 时不输出小数部分」这个 Python 规则，时区写法各不同。
- **为什么算问题**：标准库替代品 + 重复。
- **建议改法**：一个 `timefmt` 小包提供 2–3 个函数（本地无时区、UTC `+00:00`、UTC `Z`），**输出格式保持不变**（它们是 API 值和存储值）。
  `naiveNow` 那种「本地时间冒充 UTC」改成显式本地时区。
- **影响面**：无（格式不变）。
- **工作量**：S。**优先级**：中（切换前可做）。

### 1.13 `weburl` 手写了 `urllib.parse.quote` 和 requests 的 `requote_uri`

- **位置**：`internal/weburl/weburl.go:142–270`（`Quote`、`Unquote`、`UnquotePlus`、`EncodeQuery`、`Prepare`、`escapeStrayPercent`、`escapeExcept`）。
- **现状**：照 Python 规则决定哪些字符要转义。`Parse` 的 WHATWG 清理（去控制字符等）是合理的产品规则，可以保留。
- **为什么算问题**：`net/url` 的 `PathEscape`/`QueryEscape`/`url.Values` 已覆盖大部分；手写规则维护成本高。
- **建议改法**：`Quote`/`Unquote`/`EncodeQuery` 换标准库；`Prepare` 只保留真正需要的「已转义的不重复转义」。
- **影响面**：**订阅/规则下载 URL 的转义细节可能变**，少数机场对 URL 很挑剔，需要用现有用例对比。
- **工作量**：S–M。**优先级**：低（切换后）。

---

## 3. 第 2 类：O18 关键词没搜到的 Python 行为照搬（8 项）

### 2.1 IPv4 字段接受 `true`/`false`/整数，「空」按 Python 真假判断

- **位置**：`internal/installer/guide_native.go:432–462`（`decodeIPv4Value`）、`:463`（`emptyInspectionName`）。
- **现状**：JSON `true` → `"0.0.0.1"`，`false` → `"0.0.0.0"`，整数 → 按 32 位整数转 IPv4 —— 这是 Python `ipaddress.IPv4Address(True)`
  的行为；`emptyInspectionName` 把 `""、null、false、0、0.0、[]、{}` 都当「没填」（Python `if not value`）。没有任何注释说明来自 Python。
- **为什么算问题**：典型的「把 Python 的意外行为当功能保留」。没有用户会把 IP 填成 `true`。
- **建议改法**：IP 只接受字符串；其它类型按现有「地址无效」文案返回 400。「空」只认缺失、`null`、`""`。
- **影响面**：畸形请求从「生成 0.0.0.1」变成 400（**改对外行为，需裁定**）。
- **工作量**：S。**优先级**：中（前-4）。

### 2.2 MCP 安装工具把布尔参数转成 `True`/`False`

- **位置**：`internal/mcp/tools.go:536–550`。
- **现状**：MCP 调用安装脚本接口时，布尔参数被写成 Python `str(True)` 的 `"True"`/`"False"` 再放进查询串。
- **为什么算问题**：Python 字符串化的照搬；我们自己的接口解析这个参数时只需要 `true`/`false`。
- **建议改法**：传 `true`/`false`。已确认目标接口用 `utilx.StrToBool`（`api/agents/installer.go:490–496`）按大小写不敏感比较 `true`，两种写法结果相同。
- **影响面**：无（接口两种都认）；可以不等裁定，随前-3 做。
- **工作量**：S。**优先级**：低。

### 2.3 用户看得到的 `None`

- **位置**：`internal/health/check/region.go:246`（`formatStatus` 无状态码时返回 `"None"`，出现在「HTTP None」证据文字里，
  `:260`、`:681`）；`internal/api/agents/installer.go:228`（`pyOptional`，日志里打印 `None`）。
- **为什么算问题**：Python `f"{None}"` 的照搬；中文界面里出现 `HTTP None`。
- **建议改法**：区域检测证据里改成「HTTP 无响应」之类（新增文案常量）；日志里的 `pyOptional` 改成 `-`。
- **影响面**：一条用户可见文字变化（**需裁定**），日志格式变化（无消费者）。
- **工作量**：S。**优先级**：低。

### 2.4 网关配置按 Python 真假判断 JSON/YAML 值

- **位置**：`internal/gateway/adapter.go:105`（`jsonTruthy`：`null/false/""/[]/{}` 为假）、`gateway/node.go:52`（`truthy`）、
  `gateway/config.go:16`（每个成员带 `truthy` 标志）、`gateway/apply.go:135/142/226`、`gateway/overlap.go:178`。
- **为什么算问题**：Python `if cfg.get("tun"):` 的照搬。O18 T1 之后配置已是类型化结构体，字段类型确定，不需要「值是不是真」这种跨类型判断。
- **建议改法**：T1d 落地后，`profile_id`/`library_rule_id` 等直接按类型判断（字符串非空、布尔为真）；生成 Mihomo 配置时对
  `tun`/`dns` 段的判断改成「段存在且是映射」。
- **影响面**：只影响存储里类型不对的脏数据（O18 已统计生产 0 例）；生成结果需用网关 golden 守住。
- **工作量**：M。**优先级**：低（切换后，依赖 T1d）。

### 2.5 日志里的 Python 异常类名（`ErrorClass()`）

- **位置**：`weburl/logsafe.go:206–231`（`ClassError` 接口，日志写 `exception_type=<类名>`）；实现方 `refresh/interval.go:37`
  （返回 `"ValueError"`）、`refresh/scheduled.go:41/49/57`（`LookupError` 等）、`refresh/errors.go:127`、`rulefetch/errors.go:87`
  （`"UnicodeEncodeError"`）、`:98`（`"OSError"`）、`rulefetch/transport.go:50`、`rulefetch/fetch.go:294`、`gateway/gwbase/gwbase.go:23`。
  `rulefetch/errors.go:14 errorClassOf`（60 行，把 Go 错误映射回 `FileNotFoundError`、`ProfileNotFound`、`OperationalError` 等 Python 名）**已经没人调用**。
- **为什么算问题**：AGENTS.md「不模拟 Python 异常」；日志里出现 `UnicodeEncodeError` 会误导排查。
- **建议改法**：删 `errorClassOf`；`exception_type` 改成 Go 侧稳定的错误种类名（如 `interval`、`task-busy`、`credential-encoding`），
  或直接去掉 `ErrorClass` 改用哨兵错误名。注意 O18 A1（`devhttp` 用户文案里的 `ReadTimeout` 等）是用户文案，**不在此列**。
- **影响面**：只有日志文字。
- **工作量**：S。**优先级**：中（前-2）。

### 2.6 版本比较用 `math/big` 模拟 Python 无限整数，且有两份

- **位置**：`internal/substore/env.go:199`（`compareVersions(a, b []*big.Int)`）、`internal/agents/version.go:46`（`CompareVersions`）。
- **为什么算问题**：版本号段不会超过 int64；用 `big.Int` 是为了和 Python `int()` 完全一致。两份实现规则也不同。
- **建议改法**：一份共享函数，段按 `uint64` 解析，溢出按「无效版本」处理。
- **影响面**：无（实际版本号都很短）。
- **工作量**：S。**优先级**：中（前-3）。

### 2.7 logger 名是 Python 模块路径

- **位置**：`logx.Get("backend.routes.agents")` 等，70 个 logger 里约 65 个是 `backend.*`；`logx/logx.go:8` 注释要求这样命名。
- **现状**：O18 A8 保留了行格式（日志页按格式高亮/筛选），这一点不动。但 logger 名本身没有消费者：前端和 go-agent 都没有按
  `backend.` 前缀筛选（已 grep 确认）。
- **为什么算问题**：Python 已退役，日志里写 `backend.utils.sub_store_runtime` 会让人去找不存在的文件。
- **建议改法**：logger 名改成 Go 包路径（`agents/rulesync`、`substore` 等）；行格式和级别名保持。
- **影响面**：日志页看到的名字变化（无程序依赖）。**需裁定**（属于 O18 A8 的一部分）。
- **工作量**：S。**优先级**：低。

### 2.8 `utilx` 是 `backend/common/utils.py` 的整包移植

- **位置**：`internal/utilx/utils.go`（62 行，6 处调用）：`StrToBool(v any)`（`str(s).lower() == "true"`）、`GenerateRandomToken`
  （出错就 panic，违反「请求路径不 panic」）、`GetLocalIP`。
- **为什么算问题**：按 Python 模块拆出来的杂物包；`StrToBool(any)` 是 PORTING 明令禁止的「模仿 Python 调用形状的 `any` 参数」。
- **建议改法**：`StrToBool` 的 4 个调用方都在 `api/agents/installer.go`，参数本来就是查询串字符串，改成 `strings.EqualFold(v, "true")`；`GenerateRandomToken` 去掉 panic、改为返回 error
  （字母表和长度不变，不要换成 `crypto/rand.Text()`，那会改字母表）；`GetLocalIP` 移到唯一用它的 `server` 包。删掉 `utilx`。
- **影响面**：无（令牌字母表和长度保持）。
- **工作量**：S。**优先级**：中（前-3）。

---

## 4. 第 3 类：规模、重复、死代码、命名（9 项）

### 3.1 服务端死代码

- **数量**（`deadcode -test=false ./cmd/...`，去掉 model 后 215 条）：约 34 条是放在非测试文件里的测试钩子
  （`Reset*`、`Set*ForTest`、`SetNow`、`SetDefault`、`LockForTest` 等）；**约 180 条是生产代码到不了的函数**，按包：
  `agents/metrics` 18、`health/check` 17、`oxidns` 16、`convert` 16、`agents` 11、`rules` 10、`repo` 10、`agents/monitor` 7、
  `agents/trafficstore` 6、`routers` 5、其余 30 个包各 1–4 个。
- **值得点名的**：
  - `convert/ipaddr.go` 整文件 9 个函数（195 行）没人用 —— O18 A6 说要保留，实际已被替代。`convert/exports_oxidns.go ParseIPAddress` 同。
  - `rulefetch/errors.go errorClassOf`（见 2.5）、`rulesetfmt.URLValueError` + `invalidURL`。
  - `agents/metrics`：`History.Load/Flush/PendingCount/DeleteAgentHistory/RetainAgents/replace/flushAgents`、
    `TrafficHistory.Flush/Delete/RetainAgents/connect/exec/query` —— 只剩测试在调，生产走 `*Context` 版本（见 3.6）。
  - `health/check`：`ProbeURL`、`PathHealthy`、`DomainSuggestion`、`ProbeStore.SaveEntries/Delete/Path`、`RegionStore.UpdateData/Delete/Path`、
    `Suggestion`/`HostProbe` 的 JSON 方法。
  - `rules`：`CheckFilters`、`CheckCandidates`、`MatchIP`、`MatchQuery`、`IPAddressVersion`、`IsCandidateValidationError`。
  - `repo`：`LoadConfig`、`DefaultOxidnsSettings`、`UsagesOf`、`SanitizationTokens`、`AgentHistoryJSON`、`profileOperationLockPath`。
  - `oxidns`：`ApplyNativeSettings`、`FinishRuleUpdates`、`PolicyDNSRoutes`、`CommonFields`、`NormalizeName/Notes/ClientRange`、`PrefixEnd`。
  - `httpx.HTML`、`httpx.Stream`、`yamlx.Unmarshal`、`yamlx.Clone`、`jsonstore.Store.Write/Remove`、`logx.IsDebugEnabled/DebugLog`。
- **建议改法**：逐包删除；只被测试调用的函数连同那几个测试一起删（如果测试在验证仍有价值的行为，就改成调生产入口）；
  测试钩子移到 `export_test.go`（同包测试可见，生产二进制里消失）。删前用 `deadcode -test` 再确认没有被测试以外的工具引用。
- **影响面**：无。**工作量**：M（量大但机械）。**优先级**：高（前-1）。

### 3.2 staticcheck 报出的多余代码（非测试 40 条左右）

- U1000 未使用：`agents/dnsha/adapter.go:91 owned`、`dnsha/payload.go:8 payloadFields`、`monitor/messages.go:10–11` 两个文案常量、
  `monitor/metrics.go:63 fieldNames`、`monitor/panel.go:142 argPresent`、`convert/messages.go:175–179` 5 个常量、
  `convert/mihomo_generate.go:271 fieldString`、`gateway/node.go:17 tagOpaque`、`gateway/installation/wire.go:38 asString`、
  `mcp/crud.go:12 collections`、`oxidns/node.go:381/383 containsRune/trimSpace`、`oxidns/ruledns.go:532 itoa`、
  `provider/errors.go:68`、`refresh/timing.go:84`、`repo/files.go:141 writeJSON`、`repo/profiles.go:17 metadataKeys`、
  `rules/groups.go:47 contains`、`rules/ruleutils.go:233`、`substore/cache.go:47/81`、`weblistener/weblistener.go:95`、
  `api/rulelibrary/model.go:104 success`、`installer/templates.go:117 codePoints`。
- SA4006（算了没用的值，逐条看过都是多余赋值，不是漏判断）：`oxidns/zone.go:597/629/684/696/762`（解析 zone 行时 TTL/class 的中间值）、
  `routers/routeros.go:1426 beforeRows`、`refresh/source.go:182 raw`、`importer/text.go:255 stem`、`installer/docker_ha_compose.go:171 err`、
  `agents/rulesync/signatures.go:31 fields`、`weblistener/weblistener.go:410 ctx`、`httpx/response.go:315`（第一次 `ToLower` 结果被覆盖）。
- SA4013 双重取反：`oxidns/native.go:408`、`:566`（`if !(!isNone(args))`）。SA9003 空分支：`convert/mihomo_generate.go:503`。
  SA4017：`oxidns/constraints_global.go:59`（需要确认那一行的意图后再改）。
- **建议改法**：一次清理提交；SA4006 里 `installer/docker_ha_compose.go:171` 要确认是不是本该检查的 `err`（核对后看起来是被后面覆盖，但值得作者再看一眼）。
- **影响面**：无。**工作量**：S。**优先级**：高（前-1）。

### 3.3 go-agent 死代码（约 40 个函数，近千行）

- **位置**：`routes/config.go` 103–850 行之间 14 个函数（`handleMosdnsFeatures`、`handleMihomoFeatures`、`downloadRulesets/Providers`、
  `writeConfig`、`backupConfig`、`handleConfigUpdate(Async)`、`copyFile/copyDir/cleanOldBackups`…，旧的「整包配置更新」流程，约 650 行）；
  `routes/update.go` 8 个（`downloadFile`、`verifyMD5`、`restartAgentWithCheck`、`rollbackAndRestart`…，旧升级流程，约 170 行）；
  `routes/utils.go` 8 个（旧 URL 脱敏，约 100 行）；`system.go:142/157`；`routes/aio_forwarding.go:186–188`；`routes/logs.go:244`；
  `routes/dns_watchdog.go:249`；`routes/return_routing_manage.go:168`；`routes/oxidns_source_cache.go:73`；`preflight/types.go:72/78`。
- **建议改法**：删除；下一个 Agent 版本带上。
- **影响面**：无（不可达）。**工作量**：S。**优先级**：中（前-1，若切换前本来就要发 Agent 版本）。

### 3.4 原子写文件实现了约 20 份

- **位置**：服务端 `os.Rename` + 临时文件的写法出现在 24 个文件（`store`、`repo/files.go`、`repo/profiles.go`、`jsonstore`、
  `taskstatus`、`notifications/store.go`、`deploy/storage.go`、`weblistener`、`setup/store.go`、`substore/*`、`autoupdate/*`、
  `rulefetch/publication.go`、`agents/metrics/history.go`、`agents/monitor/state.go`、`agents/ipv6/readiness.go`、
  `gateway/installation/store.go`、`routers/rule_sync_store.go` …）；go-agent 11 个文件。
- **现状**：有的 fsync 文件再改名、有的还 fsync 目录、有的完全不 fsync（`jsonstore`、`repo/profiles.go`、`autoupdate`、`ipv6/readiness.go` 等 11 个文件里没有 `Sync()`）。
- **为什么算问题**：重复实现；耐久性不一致 —— 断电时不 fsync 的那几份可能留下空文件或旧内容。
- **建议改法**：一个 `internal/fsx.WriteFile(ctx, path, data, perm)`（临时文件 → 写 → fsync → close → rename → fsync 目录），全部换用；
  go-agent 自己一份。
- **影响面**：无外部变化；写入略慢（多一次 fsync），对这些低频文件无感。
- **工作量**：M。**优先级**：中（前-5 可选；否则切换后第一批）。

### 3.5 重复的小工具函数

| 函数 | 份数 | 位置 | 换成 |
|---|---|---|---|
| `ptr[T]` | 7 | `deploy/publish.go`、`discovery/model.go`、`deploy/pushdiff/types.go`、`agents/{dnsfollow,dnsha,rulesync,ipv6}` | Go 1.26 `new(表达式)`（代码库已在使用） |
| `asciiDigits` | 5 | `netipx`、`rules/syntax.go`、`clientrange`、`installer/scalar.go`、`agents/monitor/panel.go` | 一份共享 |
| `hasControl` | 4 | `weburl`、`oxidns/settings.go`、`installer/templates.go`、`agents/monitor/monitor.go` | 一份共享（先确认四份的字符范围是否一致） |
| `pathExists` | 4 | `repo`、`agents/upgrades`、`api/logs`、`api/agents/downloads.go` | `os.Stat` + `errors.Is(fs.ErrNotExist)`，不吞权限错误 |
| `contains` | 5 | `oxidns`、`discovery`、`rules`（死）、`agents/monitor`、`api/proxygroups` | `slices.Contains` |
| sha256 十六进制 `digest` | 5 | `gateway/draft`、`deploy/core.go`、`agents/dnsha`、`agents/rulesync`、`agents/ipv6` | 一份共享 |
| `raw(v any) json.RawMessage`（**吞掉编码错误**） | 6 | `gateway/draft`、`gateway/installation/record.go`、`importer/parse.go`、`agents/dnsha/adapter.go`、`agents/rulesync/adapter.go`、`agents/ipv6` | 有类型的编码函数并返回 error，或在确定不会失败处用 `must` 并注释原因 |
| `isoformat` | 5 | 见 1.12 | `timefmt` |
| `splitLines` | 4 | 见 1.11 | 一份 |
| 版本比较 | 2 | 见 2.6 | 一份 |
| `readBody` | 6 | `api/{rules,aggregations,proxygroups,agents,subscriptions,rulelibrary}` | 随 1.6 类型化后自然合并 |

- **影响面**：无。**工作量**：S。**优先级**：中（前-3）。

### 3.6 `X` 和 `XContext` 两套 API（40 对）

- **位置**：`Commit/CommitContext`、`Flush/FlushContext`、`Load`、`Read`、`Write`、`Delete`、`Remove`、`RetainAgents`、`DeleteAgentHistory`、
  `SaveEntries`、`UpdateData`、`Forget`、`Maintain`、`Query`、`HasAny`、`writeAtomic`、`writeJSON` …（`agents/metrics`、`health/check`、
  `jsonstore`、`taskstatus`、`repo` 等）。
- **现状**：照 Python 同步 API 的形状保留了不带 `ctx` 的版本，内部转调 `XContext(context.Background())`；deadcode 显示其中很多不带 ctx 的版本已经只有测试在用。
- **为什么算问题**：AGENTS.md 要求 I/O 函数第一个参数是 `ctx`；两套 API 让调用方可能绕开取消和超时。
- **建议改法**：只留带 `ctx` 的版本，并去掉 `Context` 后缀（`Flush(ctx)`）；测试改传 `t.Context()`。
- **影响面**：无。**工作量**：S–M。**优先级**：高（前-1，和死代码一起）。

### 3.7 `routers` 是一个 3.4 万行、101 个文件的大包

- **位置**：`internal/routers/`（RouterOS、iKuai、规则同步、DNS 守护、IPv6/PD、NAT 列表、固定 IP、地址列表、回程路由、客户端读取全在一个包）。
- **为什么算问题**：和「按 Python 模块拆得太碎」相反，这里是太大：私有辅助函数（`rosText`、`firstNonEmpty`…）被所有功能共享，改一处影响面看不清；编译和测试也慢。
- **建议改法**：按设备驱动和功能拆子包（`routers/routeros`、`routers/ikuai`、`routers/rulesync`、`routers/dnsguard`、`routers/ipv6`），
  驱动对外暴露小接口。和 1.8 一起做。
- **影响面**：无。**工作量**：L。**优先级**：低（切换后）。

### 3.8 go-agent 的旧式写法

- 703 处 `interface{}`（服务端只剩 2 处）—— `gofmt -r 'interface{} -> any'` 一次搞定。
- 273 处标准库 `log.Print*`，没有级别；服务端已统一 `logx`。go-agent 是独立模块，AGENTS.md 的服务端风格不强制，但至少要有级别和脱敏。
- 556 处 `map[string]interface{}`，多数是响应体和 `nft -j` 的 JSON。`nft` 那部分是外部格式，可以保留；响应体建议改结构体（**协议字段不能变**）。
- `go.mod` 是 `go 1.26.0`，服务端 `1.27.0`。
- **影响面**：无（协议不变）。**工作量**：`any` 替换 S；日志 M；响应结构体 M。**优先级**：低（切换后，跟 Agent 版本走）。

### 3.9 剩余的 Python 风格错误命名（任务点名 + 搜到的）

| 名字 | 位置 | 建议 |
|---|---|---|
| `health.ValueError`、`valueError()`、`isValueError` | `health/runner.go:198`、`health/dns.go:46`、`health/latency.go:69` | `health.ErrInvalid` 哨兵或 `*health.ValidationError` |
| `health.OSError` | `health/runner.go:43` | 直接包装原错误（`fmt.Errorf("...: %w")`），调用方 `errors.As(*fs.PathError)` |
| `health/check.ValueError`、`valueError()` | `health/check/healthcheck.go:36/41`（30 余处调用） | `check.ValidationError` |
| `metrics.ValueError`、`RetentionValueError` | `agents/metrics/traffic_native.go:156/161` | `ErrRetentionInvalid` 哨兵 |
| `agents.PermissionError` | `agents/manager.go:38`（`Error()` 返回 `Unauthorized`）、`api/agents/agents.go:141` | `agents.ErrUnauthorized` 哨兵 |
| installer `valueError()`/`typeError()` | `installer/doc.go:16/23`（108 处调用；类型已改成 `ValidationError`/`RenderError`，只剩函数名） | 改名 `invalid()`/`renderFailed()` |
| upgrades `valueError()` | `agents/upgrades/errors.go:14` | 同上 |
| `trafficstore.TypeError`、`AttributeError`、`KeyError` | `agents/trafficstore/errors.go`（`AttributeError` 只剩类型定义；`KeyError.Error()` 返回 `'key'`，是 Python `repr` 格式） | `ErrBadReport`/`*MissingFieldError`；删 `AttributeError` |
| `refresh.LookupError`、`isOSError`、`isRepositoryOrOSError` | `refresh/scheduled.go:52`、`refresh/errors.go:91/97` | `ErrSourceGone` 哨兵；`errors.As(*fs.PathError)` |
| `substore.osError`、`isOSError` | `substore/text.go:45`、`substore/cache.go:41`（死） | 删/改名 |
| `api/nodes.isValueError`、`api/notifications.isOSOrValueError` | `api/nodes/latency.go:26`、`api/notifications/notifications.go:40` | 按哨兵判断 |
| `rulesetfmt.URLValueError` | `rulesetfmt/rulesetfmt.go:142`（死） | 删 |
| `oxidns.requestTypeError` | `oxidns/stored_projection.go:63` | 改名 |
| `ErrorClass()` 标签 | 见 2.5 | 见 2.5 |

- **影响面**：无（状态码、中文文案不变）。**工作量**：S–M。**优先级**：高（前-2）。

---

## 5. 建议分批

### 切换前（全部不改 API/Agent 协议/写设备语义）

1. **前-1 死代码与双份 API**（3.1、3.2、3.3、3.6）：一位 agent，按包分提交；每个提交 `go vet` + 受影响包测试；go-agent 部分单独提交。M。
2. **前-2 Python 错误命名**（3.9、2.5）：一位 agent；`grep -rE 'ValueError|TypeError|KeyError|AttributeError|OSError|PermissionError|LookupError|ErrorClass'` 清零（model 和测试数据除外）可作为验收。S–M。
3. **前-3 重复小工具**（3.5、2.6、2.8、1.12、2.2）：可与前-2 同一位 agent。S。
4. **前-4 需裁定的小行为**（2.1、2.3、1.11）：请维护者逐条点头后做，写进 `behaviour-changes.md`。S。
5. **前-5（可选）原子写统一**（3.4）。M。

### 切换后（按收益/风险）

| 顺序 | 内容 | 编号 | 量 | 风险 |
|---|---|---|---|---|
| 1 | 后台循环改通知驱动、按到期调度、Sub-Store 空闲计时器、时间类型统一 | 1.1、1.2、1.3、1.10 | M | 低–中（漏唤醒源） |
| 2 | 原子写统一（如果切换前没做） | 3.4 | M | 低 |
| 3 | Agent 运行状态/历史移出配置文档 | 1.4 | L | 中（存储迁移） |
| 4 | CRUD 请求体类型化 + 网关真假判断 | 1.6、2.4、1.9 | M | 中（畸形请求文案） |
| 5 | OxiDNS 类型化 | 1.7 | L | 中（生成结果，用 golden 守） |
| 6 | URL 转义换标准库 | 1.13 | S–M | 中（机场 URL 兼容） |
| 7 | RouterOS/iKuai 行类型化 + 拆 `routers` 包 | 1.8、3.7 | L | 中（写设备，需 PVE 实验室） |
| 8 | 显式依赖组装替代全局钩子 | 1.5 | L | 低（纯内部，但面广） |
| 9 | go-agent 风格、logger 名 | 3.8、2.7 | S–M | 低 |

---

## 附：检查命令

```
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && staticcheck -checks "U1000,S1*,SA4*,SA6*,SA9*" ./... ; deadcode -test=false ./cmd/...'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd backend/agents/go-agent && staticcheck -checks "U1000,S1*,SA4*,SA6*,SA9*" ./... ; deadcode -test=false . ./...'
```

工具版本：staticcheck（honnef.co/go/tools v0.8.2）、deadcode（golang.org/x/tools v0.51.0），Go 1.27.3。
deadcode 以 `cmd/configflow`、`cmd/configflow-autoupdate` 为入口；只被测试调用的函数也算「不可达」，删之前按 3.1 的做法处理。

## 6. 进展

### 切换前清理第一部分（分支 `go-o17-pre`，2026-11-23）

已做：前-2（3.9、2.5，`ErrorClass` 标签改 Go 名，`rulefetch.errorClassOf` 移到测试）、前-3（`ptr`→`new(...)`，`pushdiff` 因参数名 `new`
保留自己的 `ptr`；`raw(v)`×4→`jsonresp.Value`（失败记日志）；`asciiDigits`→`textx.ASCIIDigits`；`isoformat`×5→`timefmt`；
版本比较→`version.CompareAgent`/`ParseRelease`，uint64，逐对对比旧实现的测试；`utilx` 删除；MCP 布尔）、前-5（`internal/fsx`，
服务端 18 处换用；`jsonstore` 因签名校验保留自己的 rename，补 fsync）、前-4 三项裁定（IPv4 字段只收字符串、「无 HTTP 响应」、
`textx.SplitLines`）、O18 A6 的 `convert/ipaddr.go` 删除、`docker_ha_compose.go` 漏判的 err。行为变化见 `behaviour-changes.md`。

未做（留给后续）：前-1 死代码（等 T1d）；`hasControl`×4、`pathExists`×4、`contains`×5、sha256 `digest`×5、`readBody`×6（不在本次指派范围）；
`metrics.naiveNow` 本地时间冒充 UTC（1.12 后半）；go-agent 的 11 份原子写；`store.Install`/`autoupdate.replaceFile`/Sub-Store 依赖目录
的整目录换名（不是「写字节」型原子写）。

### 1.5 + 3.7 设计（分支 `go-o17-wiring-plan`，2026-11-23）

方案见 `design/o17-wiring.md`（routers 拆 13 个包、显式组装、R1–R10 + W0–W11 共 22 步，裁定点 D1–D14）；逐项清单在
`design/o17-wiring/inventory.md`。等「运行状态收尾」合入后实施。

### 前-1 清理（分支 `go-o17-pre1-deadcode`，2026-11-24）

以交接树 `b082e2ca6` 为基线，完成 §3.1、§3.2、§3.3、§3.6。本轮删除
`convert.ParseIPAddress`，`convert/ipaddr.go` 已在前一轮删除。存储、Agent 监控和健康检查
统一使用带 `context.Context` 的入口。规则库提交批次在开始文件写入后仍完成已获准的写入和提交。

| 扫描 | 服务端前 → 后 | go-agent 前 → 后 |
|---|---:|---:|
| staticcheck，含测试 | 111 → 51 | 25 → 0 |
| staticcheck，非测试 | 60 → 0 | 23 → 0 |
| deadcode，默认生产入口 | 257 → 134 | 44 → 0 |

服务端剩余 134 项中，37 项是本轮范围外的 `internal/model`，3 项是 faultprobe 工具和构建标签
使用的入口，94 项由测试使用。可随所属包测试编译的钩子已移到 `export_test.go`。其他包的集成测试
仍需要的钩子继续由原包提供，例如 `agents.ResetRuntime`、`health.LockForTest` 和
`convert.ResetMihomoEnvironmentForTest`。51 条 staticcheck 告警均来自服务端测试文件。

`tools/pyref/sources.json` 随测试助手和删除的文件更新。15 份记录仅刷新源码路径和哈希，
逐条核对了精确输入、输出和 Python 依赖指纹。installer、mosdns、oxidns、routersplit 的清单入口
通过 `PYREF_MODE=check`。

验证均通过 `rt` 在 .64 执行。服务端 `gofmt -l internal/ tools/ cmd/` 无输出，
`go vet ./...` 和 `go build -tags <tag> ./...`（两个标签分别构建）通过。
`go test -race -count=1 -p 6 -timeout 12m` 覆盖改动 Go 文件的包及
`cmd/configflow`、`internal/configgen`、`internal/server`、`internal/testgolden`，
47 个包通过，3 个包无测试。go-agent 的格式检查、默认与 `halab nftlab` 标签 vet、
Linux 四架构交叉构建和 `go test -race -count=1 ./...` 均通过。
