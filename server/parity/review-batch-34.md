# HR 复核 batch-34（O10 R11 分配上限门禁 + R8 订阅节点缓存 + R9 YAML/规则整编码清扫）

范围：`go@c02c189f7` 之上 13 个提交（6f7f5805f … 60ffc5712），作者 Opus。复核者：Opus 5.5，2026-11-22。
协调裁定照办：R9 未达 ≤30 MB 接受、不加生成结果缓存；`refresh/cache_text.go` 视为订阅源解析缓存，重点核对失效条件。

## 结论

无高级问题。两处中级问题已修（缓存保留内存），一处中级门禁绕过已补；另补一条端到端失效测试（含负对照）。
作者进度报告里「rebase 后 .64 preflight 只有 dnsha 一处失败」不准确：那次运行磁盘写满后崩溃，见第 5 节。

| 级别 | 项 | 处理 |
|---|---|---|
| 中 | 4 张「按输入复用」表（`provider.parsedProviders`/`parsedSubscriptions`、`aggregations.sourceNodeMemo`、`convert.subscriptionCaches`）的上限按**键文本长度**计（各 8 MB，单条 ≤2 MB），但值是解析后的图：实测 0.5 MB、2000 节点文本在两张解析表里常驻约 20 MB（每表约 ×20），聚合节点 ×16，订阅缓存解码 ×14。最坏可常驻数百 MB；订阅内容变化后旧条目要等表满才清 | 修（00c50b36a）：改按「估算常驻字节 = 文本长度 × 实测倍数」计，每表 32 MB，单条 ≤ 一半。生产快照订阅缓存最大 154 KB、合计 0.73 MB，全部仍命中。新增 `TestO10ParsedMemoBoundedByRetainedEstimate` |
| 中 | `refresh.cacheTexts` 以共享 `*NodeCache` 指针为键、满 64 条才清空：每次缓存文件变化产生的新记录都留一条，旧记录（解码后的全部节点）连同渲染文本一起被保留 | 修（3e2c25885）：按订阅 ID 存一条 `{record, text}`，只在记录指针相同时命中；文件变化即替换，旧记录可回收。测试补「换记录不命中旧文本、条目数为 1」 |
| 中 | R11 源码门禁只看实参自身类型，以下写法都漏报：`json.Marshal(map[string]any{"config": c})`、带导出记录字段的响应 struct、内嵌记录、`model.Marshal(any(c))`、把 `[]any{a}` 交给 `any` 参数的通用助手、`c.MarshalJSON` 方法值；`yamlx.Marshal`/`yaml.v3` 编码器未列入 | 修（4cacca083）：沿复合字面量（map 键值、切片元素、导出/内嵌字段）和接口转换查找记录；结构体类型按 JSON 会编码的字段（导出或内嵌）递归，自带 `MarshalJSON` 的类型除外；方法值与 YAML 编码器纳入。现有代码无新命中；`Detects` 加 7 个正例 3 个反例；真实树负对照（在 `internal/api/agents` 临时加 `json.Marshal(map[string]any{"agent": record})`）门禁失败，已还原 |
| 低（说明） | R8 文件身份 (size, mtime ns, dev, inode)：写入一律经 `writeAtomicContext`（临时文件 + rename，新 inode，旧文件仍在时新 inode 不可能复用旧号），删除走 `memoForget`。剩余理论窗口只有**外部进程**原地截断重写、同大小、同一内核时间戳粒度内（或手动 `touch -d` 回原 mtime）且两次写之间有读——ConfigFlow 自身不这样写 | 不改。已用端到端测试覆盖「同一秒、同大小、原子改写」 |
| 低（说明） | R11 分配上限在预热后测，测的是复用表命中路径；冷路径（表未命中、文件刚变）的整份编码回归不在上限里 | 不改：门禁（类型感知）管冷热两路的写法；上限是稳定态预算 |
| 低（说明） | 门禁仍按包划定范围（`internal/api/**` + 注册循环的包）；路由调用的其他包里的整份编码（如 `aggregations`、`convert` 内部）不在门禁内 | 不改：由 R11 上限兜底测量路由；扩大范围会把生成器内部的合法整图编码全部拉进豁免表 |

## 1. 缓存正确性

**R8 substore（`cache_memo.go`）**：键 = 路径；命中需 `(size, mtime, dev, ino)` 完全相同；只在「读前 stat == 读后 stat」时存；失败与空文件不存；删除、孤儿清理、文件消失时 `memoForget`。`LoadCache` 返回副本（`cloneCache` 复制所有可改的 struct/slice；`Raw`/`Fields` 是写时复制——`ClearMember` 等先 `slices.Clone`），`LoadCacheShared` 返回只读共享记录，唯一使用者 `refresh.loadNodeCache` 只读。`saveCache` 写前 `LoadCache` 读旧值，所以 ConfigFlow 自己的两次写之间总有一次读，A→B→C 的 inode 复用 ABA 也会先被 B 的 stat 刷掉。
多订阅共用文件：`cacheName` 把 `/`、`\` 换成 `_`，`a/b` 与 `a_b` 本来就共用一个文件（既有行为，未变）；memo 按路径、view 按 ID + 记录指针，不会串。

**cache_text**：文本只依赖 `cache.Nodes`（`NodeProxy` 只在有 `proxy_string` 时用 converter/ctx，这类记录被排除）；`sharedNodeLoader` 用函数指针比对排除测试 loader；URL 不符的缓存在 lookup 之前已置 nil；错误不缓存；ConfigShapes 校验只在未命中时跑——命中意味着同一记录此前已通过。指针何时更换：substore memo 换代（文件身份变）→ `LoadCacheShared` 返回新 `*Cache` → `loadNodeCache` 重建 view。修复后按订阅 ID 一条。

**按输入复用的键是否覆盖全部输入**：
- `readProviderNodesFor`/`ParseNodes`：键 = 文本；`name/path/kind/sourceLabel` 只进错误，错误不缓存；`FixProxyNode` 只看节点。共享返回（own=false）的两处（ParseNodes 内部立即 `Encode` 拷贝、PrepareBundle 只 `Get`）都只读。
- `sourceNodes`：`proxies` 恒为 `ParseNodes(fetched.Text)`，`ProxyNodes` 纯函数（不生成 ID；随机 ID 在复用之后按副本补）；调用方改 `SubscriptionID/Name/ID` 是副本上的值字段。
- `decodedSubscriptionCache`：键 = 完整编码字节。
- 过滤/重命名正则、聚合设置、订阅 UA 都在这些表**之后**应用（`FilterNodes`、聚合组装、FetchSource 的 transport），不进键也不该进键。端到端测试证实（见第 2 节）。
- `yamlx` 探针表：键 (tag, value, flow, str)；旧实现的探针节点也只带这几项（锚点/注释先清掉，引号风格先去掉；`TaggedStyle` 已在时结果相同）。

**并发**：所有表都在互斥锁内读写；共享值只读。受影响包 `-race` 通过（见验证）。

## 2. 生成语义不变

configdiff 14 组作者报告与基线差异清单逐行相同；我抽查了与订阅/provider 相关的几组（见验证）。
mihomo/surge/loon 主配置只以 URL 引用订阅（`/api/profiles/<p>/subscriptions/<id>/proxies?format=…`），节点内容出现在该路由和聚合 provider。新增 `TestO10ReuseTablesFollowInputChanges`（e70432e66，完整 NewApp + R11 夹具）：四条路由各读两遍（第二遍走复用表），然后依次
1. 同一秒内以**同样大小**改写节点缓存（改一个节点名，字节数不变，测试断言文件大小相同）；
2. 给订阅设 `regex_filter`，再去掉；
3. 改聚合成员，再改聚合 `regex_filter`；
每一步下一次读取都必须反映变化。负对照：让 `cacheTexts` 忽略记录指针 → 第 1、2 步四条路由全部失败；让 substore memo 忽略文件身份 → 同样失败。两处均已还原。

## 3. writeJSONString / Marshal 规划

- `writeJSONString`：与 `encoding/json`（`SetEscapeHTML(false)`）逐字节对照——0x00–0xFF 每个码点和每个单字节、`"`/`\\`、`<>&`、U+2028/2029、非法 UTF-8（逐字节 U+FFFD）、`\b`/`\f` 短转义；`FuzzWriteJSONString` 同样对照且检查输出是合法 UTF-8。HTML 字符不转义，与改前一致。该工具链下 encoding/json 对非法字节写 U+FFFD 字符本身、`\b`/`\f` 用短转义；即使将来 encoding/json 改变，`writeJSONString` 的输出也不变（不再依赖它），两种写法都是同值 JSON。
- Marshal 规划：`marshalReference` 保留旧的「拷贝 → 原地准备 → 校验 → 输出」，`TestMarshalPlanMatchesReference`（yamlx 语料 + review 语料 + 边界样例）与 `FuzzMarshalPlan` 比字节、错误与否，并检查输入图未被改。规划按「首次到达时的 block/flow 上下文、每节点一次」与旧遍历一致，键的注释合并先记入计划再由该键自身的 visit 继续；未改动的图跳过 `checkGraph`，因 `encodeGraph` 已做 `validateNode`。

## 4. R11

- 豁免表中标 `debt:` 的读路径（17 条）：`agents/ipv6/service.go:Host`、`api/aggregations/adapter.go:resourceConfig`、`api/rules/adapter.go:loadProfile`、`api/rulelibrary/adapter.go:sharedConfig`、`api/proxygroups/adapter.go:sharedConfig`、`api/proxygroups/adapter.go:load`、`agents/dnsfollow/adapter.go:secrets`、`agents/ipv6/adapter.go:agentDocument`、`api/agents/client_tools_adapter.go:clientRuleDeps`、`…:clientTargetsPayload`、`api/agents/public_agent_adapter.go:publicAgentModel`、`api/clientdevices/handlers.go:config`、`api/config/custom.go:readCustom`、`routers/management.go:Encoded`、`routers/pending.go:pendingTargets`（O10 路由，在上限内）、`routers/rule_sync_adapter.go:syncConfig`、`routers/workspace.go:encodedRuleSets`（O10 路由，在上限内）。豁免表有「过期条目」检查。
- `go list -e -export` 失败：命令非零 → `t.Fatalf`；单个包编译失败时导出数据为空 → 导入器报错 → 类型检查 `t.Fatalf`；检查的包少于 30 → Fatal；缺 go 命令 → Fatal（skipaudit 要求）。不会静默通过。
- 上限测试：19 个标签都必须有请求组且预热返回 200 非空；上限 ≈ 实测 ×1.5；作者负对照（`GET /api/routers` 加一次 Config 深拷贝 → 4.2 MB > 0.3）合理，未重复。绕过方式见上表第 3、5 行。

## 5. dnsha `member_lost_test.go:118 handoff never confirmed`

不是时序抖动，也不是产品问题。.64 上那次 preflight 日志（`~/.cache/rt-tmp/preflight-…go-perf-r11.log`）显示：失败的是 `TestLostMembersBothOfflineKeepPairUnknown`，日志 `OxiDNS pair publication stopped (*store.Error)` → 发布以「主备发布处理失败」结束，所以 12 轮都等不到交接；同一次运行随后 `link: … no space left on device`、`compile: … no space left on device`，最后 `SIGBUS` 崩溃——磁盘已满，SQLite 提交失败。作者报告「只有 dnsha 一处失败」不准确：该次全量运行中途崩溃，大量包没跑。
复现核对：.32 上在两个核上加 6 个满载进程、`GOMAXPROCS=2` 跑相关用例 15 次全通过；给每个 Agent 请求加 0.2 s / 1.5 s 延迟，交接都在第 2 轮确认（12 轮余量充足）。用例本身没有真实时钟依赖（`Options.Now` 固定，心跳新鲜度 120 s）。
处理（5004d402e）：不放宽断言，只让失败信息带上最后的 status/phase/message，下次能直接区分「存储失败」与「交接不动」。产品行为（存储错误使本次主备发布失败、需重发）是既有设计，不改。

## 验证

全部在测试服务器上（HEAD = 5004d402e + 本报告）。

- .32（负载 11–13）：`gofmt -l` 无输出；`go vet ./...` 通过。
- .32 `go test -race -count=1 -p 6` 于 refresh、provider、aggregations、convert、server、substore、api/aggregations、api/subscriptions（含子包）：全部 `ok`，无 DATA RACE。convert 第一次在负载 12 下撞默认 10 分钟超时，`-timeout 45m` 单独重跑 `ok configflow/internal/convert 744.898s`（负载 14–18，只是慢）。
- .32 `go test -race -run 'TestLostMember|TestLostMembersBoth' ./internal/agents/dnsha`：`ok 25.589s`。
- .32 difftest（`-tags configflow_difftest`）api/aggregations、api/subscriptions、provider、refresh、substore：全部 `ok`。
- 新测试 `TestO10ReuseTablesFollowInputChanges` 通过；两个负对照（文本表忽略记录、substore 忽略文件身份）均失败，已还原。门禁真实树负对照失败，已还原。
- .64 configdiff 抽查 4 组（delivery-graph、provider-downloaded-shape-guards、generate-downloads、subscriptions-validation，265 次比较）：17 处差异（3 accepted / 14 open）。在基线 `c02c189f7` 上同跑 delivery-graph + generate-downloads：14 处 open 差异集合相同（G13 六处 artifact `input_digest`、R064/R066/R072/R127/R129/R135-F09 与 R068/R131-F10 的 500→400），本批未新增；H1061（YAML 诊断位置）为既有已记录差异。
- .32 `/tmp/preflight.sh`（vet 两种 tag + 全量非 race，`-p 8`）：`PREFLIGHT PASS`，前后 `/home` 剩余 177/176 GB。
