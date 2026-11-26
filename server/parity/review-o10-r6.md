# O10-R6 HR 复核：路由器读路由、凭据按代缓存、Agent 视图（2026-11-22）

复核对象：`batch-28`（= `go-perf-r6`，基于 `go@4e478d651`），`5166d774c..12ae4ac24` 共 5 个提交。
方法：通读 `repo/config_credentials.go`、`repo/resolve.go`（`resolveConfig`、`copyOxiDNSAgents`、`hydratedRuleset`）、
`repo/config_borrow.go`、`redact/redact.go`（`CollectCredentials` 收集模式）、`routers/{management,inspect,pending,workspace,agent_return,publish}.go`、
`api/routers/{handlers,workspace,adapters}.go`、`repo/router_settings.go`。作者的 routerdiff/configdiff/difftest 只读不重跑
（WORKFLOW「Leaner verification」），补了针对性测试，修复配负向对照。

## 结论

缓存键、同代性、写事务不缓存、有界、脱敏集合等价都成立；路由器记录与写入内容不变。发现 1 个中危（已修）、4 条低危/说明。

## 中

### M1 policy-settings 的 ruleset 行在成员存成数字时不再报错（已修，`a197c79c7`）

`workspaceRuleSets` 只在 `rule_configs` 下有解码 issue 时退回严格解码。但记录解码器把数字标量当文本接受且**不记 issue**
（`{"policy":5}` → `Policy="5"`，`Issues` 为空；`true`/对象才记 issue）。改前对编码视图做 `jsondecode` 严格解码，
`policy`（以及 `id`、`name`、`library_rule_id`、`itemType`）是数字时报 `cannot unmarshal number into … of type string`；
R6 后同一份存储数据返回 200、`policy:"5"`。数字 `policy` 能通过存储校验（`UpdateSharedAndProfiles` 可写入，Python 导入的数据也可能有），
而且改前任何一条规则（不只是 ruleset）的这几个成员是数字都会让读取失败。

修复：`textRuleMembers` 逐条检查这 5 个成员的存储字节，只要不是字符串或 null 就走原来的严格解码（错误文本相同）。
测试 `routers.TestO10R6ReviewWorkspaceRuleSetsMatchEncoded`：普通、null、数字/布尔 policy、非 ruleset 规则的数字 policy/name、
无关成员错型（`enabled:"yes"`，两边都成功），逐条比较行和错误文本。负向对照：修复前 `policy_number` 失败
（`error <nil>, before json: cannot unmarshal number into Go struct field .rule_configs.0.policy of type string`）。

注：Python 在这里原样返回 `5`；改前 Go 报错本身就是既有差异，不属于 R6。本修复只恢复 R6 之前的 Go 行为。

## 低 / 说明

- **L1 `pendingTargets` 预筛选与解析是两次读取。** 先用借用的已存配置空间判断有没有路由器链接这个 Agent，再 `ConfigWithOxiDNSAgents`；
  两次之间提交恰好新增链接时，这一轮会漏掉该配置空间。改前配置空间列表、Agent、每个配置本来就是多次独立读取，下一次请求即恢复；
  不涉及脱敏。不改。
- **L2 路由器响应的 Agent 凭据仍按当前 head 读取。** `public()` 的 seeds 里 `AgentRecordCredentials` 读当前代，`cfg.Credentials` 是
  `Records` 那一代（同一次借用，见下）。这与 R6 之前完全相同（改前 `Records` 与 seeds 也是两次读取），OxiDNS 投影不含 token，
  路由器响应里也没有 Agent 私有成员；记为既有、非 R6 引入。
- **L3 缓存不按 `collect` 区分。** `Repository.ConfigCredentials` 接受任意 `collect`，缓存却只按（文档代, 配置空间）键；目前唯一调用方
  `redact.collectedCredentials`，文档注释已写明“collect 只依赖输入”。若将来有第二种 collect 会串值，记为约定。
- **L4 `AGENT_REGISTRATION_KEY` 进入缓存值。** `collectedCredentials` 经 `CollectCredentials(data, nil)` 把当时的注册密钥并进记住的集合；
  运行中改环境变量只会让旧密钥在该代内继续被遮蔽（多遮），新密钥每次由 `CollectCredentials(nil, …)` 加入。无泄露。

## 逐项核对

### 1. repo 凭据缓存

- **键**：`weak.Pointer[model.Document]` + 配置空间 id。`resolveConfig` 只读 `document`（Profile、Shared 目录、System 的
  SystemConfig/Backup/Agents、RuleLibrary）和空 `ResourceRoots`，没有文件、环境或时钟输入，所以（文档, 配置空间）完全决定视图。
  共享文档不可变（R2）；`weak.Value()` 在旧文档回收后返回 nil，不存在地址复用的 ABA。同一代被重新读成另一个指针只会未命中。
- **同代**：`ConfigWithCredentials`、`ProfileWithCredentials` 在同一个 `borrow` 回调里取视图/记录和凭据；命中缓存时凭据来自同一文档指针。
  `Records`/`ConfigRecords` 各自一次调用，没有 R4 M1 那种两次独立读取。`safeText` 本来就是单独读取（改前也是），且只多遮。
- **写事务**：`borrow` 的 `shared=false`（事务内或 section updater 活动视图）时不取缓存、不写缓存，作者测试覆盖回滚后不污染。
- **有界**：只保留一代；每代 map 键是成功解析过的配置空间 id（错误不入缓存）。不同代读者交替只会互相替换（抖动）。
- **并发**：`configCredentialsFor` CAS 循环；map 读写在 `mu` 下；返回的切片只被 `NewSecrets`/`CollectCredentials` 复制，不原地改。
- **集合等价**：`CollectCredentials` 收集模式下 `discovered` 与 seeds 无关（`collectOnly` 分支不看 `s.secrets`），
  所以 `CollectCredentials(view, seeds) == NewSecrets(seeds, discovered(view), 注册密钥)`；值是解码后的字符串，`json.Marshal`
  与 `model.Marshal` 的 HTML 转义差异不影响。`allProfileSecrets` 从不带 Agent 的视图改成带 OxiDNS 投影的视图：投影成员是存储 Agent
  记录成员的子集，随后 `AgentRecordCredentials` 已全部加入，并集不变。

新测试（`redact/o10_r6_review_test.go`）：
- `TestO10R6ReviewPublicSecretsMatchEncodedView`：对两个配置空间，比较路由器响应实际使用的整套集合（外部 seeds + 存储 Agent 凭据 +
  视图凭据）与改前“对编码视图收集”；构造凭据只在被引用的共享节点里（只改节点、不改配置空间）、只在 Agent 的 `rule_sync` 嵌套成员里
  （OxiDNS 投影）、Agent 换 token + 换嵌套 key 之后；并断言旧值不再出现、未引用的配置空间不含节点凭据、投影本身不含 token。
- `TestO10R6ReviewCredentialsBelongToTheirRecords`：4 个读者与 40 次提交并发（`-race`），每个读者检查 `ConfigWithCredentials` 的值
  等于其视图编码收集结果、`ProfileWithCredentials` 的轮换值恰好等于借来记录里的值。
- 负向对照：把 `configCredentialsFor` 的键退化成“忽略代”，两条测试都失败（节点凭据缺失；`[synthetic-rot-0]` 对 `[synthetic-rot-1]`）；恢复后通过。

### 2. Records 不解析：逐路由字段来源

`resolveConfig` 对路由器成员只做 `slices.Clip`，水合只改 `rule_configs`，OxiDNS 投影只填 `Agents`，没有默认值填充进 `routers`。

| 路由 | 读法 | 依赖解析视图的字段 |
|---|---|---|
| `/api/routers`、settings、probe、apply、rule-sync、policy、forwarding、job 冲突响应 | `Records`（已存 routers） | 无：响应只来自路由器记录 + 各自的 core 读取；`cfg` 只用于脱敏集合（类型改成 `*ConfigView` 后编译器保证没有其它用途） |
| report / plan（`inspect`） | `ConfigRecords` → `Encoded()` | `Inspect` 的 `Config` JSON、通知摘要：与改前 `json.Marshal(ConfigWithOxiDNSAgents)` 同字节（作者测试比较了字节） |
| policy-settings（workspace GET/PUT 后重读） | `ConfigRecords` → `ReadWorkspace(*model.Config)` | OxiDNS 策略/settings revision（`NewTypedProfile`，与生成器同一构造）、ruleset 行（M1 修后与改前逐条相同） |
| workspace preview | 自己的配置读取，凭据 `CollectCredentials(cfg, nil)` | 不变 |
| upstream-routers | `allProfileSecrets` + `pendingTargets` | 集合等价见上；预筛选见 L1 |

路由器记录：`TestO10R6ReviewStoredRoutersMatchEncodedConfig` 用含 `&`、`<>`、U+2028、中文、未知成员、错型 `port`/`rule_sync_interval`、
`last_probe` 的三台路由器，比较 `Records`/`ConfigRecords` 与 `routersOf(json.Marshal(视图))`：记录 `jsonEqual`、`View()` 字节相同、
`releaseRouterRecord` 语义相同且 `sameReleaseRouter` 为真、`WorkspaceRevision` 相同。`r.source` 字节在两条路径下确实不同
（HTML 转义），但它的使用者（`SaveRouterSettingsJSON` 的 `sameStoredObject`、`canonicalReleaseJSON`、`jsonEqual`、
重新 `json.Marshal` 的 revision）都按语义比较或重新编码，写入存储的替换值来自 `json.Marshal(next)`，不受影响。

### 3. ManagerAgentSource.Agent

`view := *record` 浅拷贝，`Record` 字段改前就是同一个借用记录。调用方（`inspect.go` 两处、`pending.go`、`rule_sync_controls.go` 两处）
只读 `ID/Host/Port/ProfileID/ServiceType/Status/Version/Enabled` 与 `DNS`；routers 包内没有对 `GatewayAgent` 嵌入记录的赋值、
append、`Fields.Set`/`SetJSON`。`dnsAgentMembers` 与 `DNSAgent` 的 7 个 json tag 一一对应；`jsondecode` 大小写敏感、忽略未知成员，
所以只解这 7 个成员与解整条记录的结果和错误类型相同（错误文本不含偏移）。

### 4. 写到路由器的内容

发布/规则同步/设置保存的输入是 `lockedFor` 用 `Records` 找到的记录，与改前 `jsonEqual`（上面的测试）；写路径本身未改。
作者的 routerdiff（约 6300 用例，除时间戳外 0 变化）与 configdiff 路由器组已覆盖，抽查了其报告与上述字节差异的使用点，未重跑。

## 提交

- `1dc29e4e7` M1 修复 + 复核测试（redact、routers）
- 本报告单独提交

## 验证

.64：
```
（rebase 前，1dc29e4e7 的同内容树）
gofmt -l . → 无输出；go vet ./... → VET-OK
go test -race -count=1 -p 6 redact routers api/routers repo api/agents
  ok redact 18.8s / routers 427.9s / api/routers 36.5s / repo 212.1s / api/agents 286.0s
CONFIGFLOW_DIFF=1 go test -tags configflow_difftest api/agents/difftest httpx/difftest api/oxidns/difftest
  ok 222.8s / 19.5s / 7.1s
（rebase 到 go@1ada5e9db 之后）
go test -race -count=1 -run TestO10R6 ./internal/redact ./internal/routers → ok / ok
/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-28 x 6 → PREFLIGHT PASS
```
