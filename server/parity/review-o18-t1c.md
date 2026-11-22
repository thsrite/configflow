# HR 复核：O18 T1c（路由器、设置、资源、策略记录改普通结构体）+ C10

分支 batch-38（= go-o18-t1c，go@9d9e35d89 之上 23fe17a97 为止 16 个提交），复核 2026-11-23，测试在 .32/.64。
按 WORKFLOW「Leaner verification」：不重跑作者全套对照；跑本复核修复涉及的包与直接调用方 `-race`、相关 difftest（真 Python）、preflight；
每个修复各做一个负向对照。

## 发现

### 高

**H1 config_token 存成非文本时，保存其它设置会把它删掉 → 登录关闭时变成匿名访问（已修，054f2e41f）**
T1c 把 `config_token` 非文本读作空 + `system.system_config.config_token` 告警，`SystemSettingsDamaged` 认这条（失败即关）。但
`system_config` 是一行：保存服务器地址、GitHub 代理、订阅聚合开关等任何其它成员都会按读出的值重写整行，`config_token`（空、omitzero）
被省略 → 告警消失 → `ValidateSystemTokenOrJWT` 在登录关闭时直接放行。Python 保存时原样保留 `42`（永远对不上请求，等于失败即关）；
T1c 前 Go 的 R5 记录保留原字节。`keepDamagedSettings` 只保护「没有改动设置」的事务，显式保存其它成员不在保护内。
修复：`repo.keepDamagedConfigToken`（commitStore 里，keepUnchangedRows 之后）——基线有该告警且新值没有设置令牌时，把存储里的原值放回
重写的 `system_config` 行。只有设置新令牌才修复；`DELETE /api/config-token` 本来就是三方合并里的 no-op，不修复（与 T1c 前相同）。
测试 `repo.TestReviewO18T1cSettingsSaveKeepsDamagedConfigToken`（`42`/`{}`/`["x"]`/`true`）。负向对照：去掉调用 → 失败。

**H2 路由器规则集选择存成非列表 → 读作空选择 → 下次发布撤掉全部路由/策略（已修，498c89695）**
`ip_rule_sets`、`ros_policy_rule_sets` 存成 `"abc"`、`5`、`{}` 时普通记录读作 nil，编码成 `null`，`idList(null)` 为合法空选择；
`ValidateForDevice` 通过，发布计划删除该路由器上本功能管理的全部静态路由/策略路由（自动规则同步也走这条路）。T1c 前 `validateIdentity`
检查的是存储原值，会按「字段类型无效」拒绝；Python 对字符串逐字符迭代、找不到规则集，不会变成空选择。
根因：生产路径的 `routerFromModel` 把 `r.source` 设为**类型化编码**，`validateIdentity` 在普通记录上基本恒通过（见 M3）。
修复：新存储标签 `stored:"selection"`——非列表（`null` 除外）读作 `[DamagedText]` + 告警，写回保持损坏；`idList` 拒绝 `DamagedText`。
`setup` 的「选择为空则自动填充」也因此不会覆盖损坏值（与 Python 真值判断一致）。
测试 `routers.TestReviewO18T1cDamagedSelectionFailsClosed`（经 `model.DecodeStored` + `routerFromModel` 的生产路径，含写回再读）。负向对照：去掉标签 → 失败。

### 中

**M1 守卫开关存成 Python 为真的非布尔值（`"x"`、`1`）读作关 → 下次发布撤下 DNS 守卫 / IPv6 前缀保护（已修，498c89695）**
`dns_guard.enabled`、`ipv6_pd_guard.enabled` 是 gate，错类型读作关；但对守卫来说「关」= 发布时撤销设备上的守卫（安全功能被悄悄关掉）。
T1c 前：守卫有告警且已部署状态存在时拒绝设备管理。T1c 只在「开关为真 + 其它成员坏」时整份保留，测试 B 组把 `{"enabled":"x"}` + 状态记成「接受」。
修复：`enabledGuardIssue` 在开关本身错类型时按原值真值决定——为真则整份保留（`Stored`），`ValidateForDevice` 按「已保存的守卫设置无效」拒绝；
为假（`0`、`""`）读作关、可以撤下（Python 同为关）；`null` 照旧为关；未声明成员照旧丢弃不触发保留。
改了 2 条按旧语义断言的测试（`TestN89StoredRouterReadsWrongTypesLeniently`、`TestX1RouterDecodeFallsBackPerMember`）与
`TestN89MalformedActiveGuardBlocksDeviceAccess` 的一行。测试 `routers.TestReviewO18T1cDamagedGuardSwitchFailsClosed`。负向对照：恒返回 false → 失败。
（`manage_return_routing` 错类型同样读作关、发布会清理回流——这与 T1c 前一致，发布计划会显示清理项，属 T1b gate 裁定，未改，见 L3。）

**M2 真 Python 下 `model.TestHRPythonWriters` 失败（已修，812c605c9）**
c40b21303 把 Agent 记录登记进交叉核对后，Python HR 写入的 Agent 行与 Go 编码在 T1b 已认可的差异上不等（`dns_follow.upstream_index:0`
始终写、`null` 丢弃），嵌套比较与 Python 精确审计都失败；go@9d9e35d89 上通过，作者树 23fe17a97 上失败（临时 worktree 复现）。
作者「difftest 全过」未覆盖带 `CONFIGFLOW_PY` 的 model 包。修复只改测试：比较时忽略源里没有、Go 补写的 `0`/`[]`（不忽略 `false`，以免掩盖默认开的开关），
精确审计从 Go 编码形态开始（同 `TestTypesPythonRoundTrip`）。

**M3（未改，供裁定）生产路径上 `validateIdentity` 已基本恒通过，N89 身份测试只覆盖测试路径**
`routers.Router.UnmarshalJSON` 把 `source` 设为原始字节，`TestN89StoredIdentityCannotOpenDevice` 经它通过；生产的 `routerFromModel` 设的是类型化编码，
于是库里 `username/password/endpoint/type: 12` 读作文本 `"12"` 并用于连接设备，`true/[]/{}` 读作 `""`。这符合 T1c「文本接受数字原文」裁定，
不放宽安全（空用户名/密码只会登录失败，ID 为空另有拒绝），所以未改；但测试对生产是空过的。若要恢复「凭据/身份非文本即拒绝」，
可把这些成员标 `stored:"restrict"` 并在 `validateIdentity` 拒绝 `DamagedText`。

### 低

- **L1 C10 路径解析对「不存在」以外的读取错误直接报错（已修，b3a7840ea）**：ENOTDIR、EACCES 时 C10 版返回错误（变成内部错误），
  T1c 前（realpath 非严格）原样保留该段继续。改为任何 Lstat 错误都原样保留。测试 `repo.TestReviewO18C10UnreadableComponentKeptAsGiven`。
  锁超时 `strconv.ParseFloat` 与 `os.UserHomeDir` 改动核对无问题（只差 Unicode 数字/十六进制浮点与错误文本，已记 behaviour-changes）。
- **L2 订阅缓存孤儿清理的告警与快照来自两次读取**：`getShared` 先 `r.Shared` 再 `r.StoredIssues`，两次可能是不同代；窗口极小，
  且损坏只会让清理跳过或按另一代判断。未改。
- **L3 `manage_return_routing` 错类型读作关 → 发布清理回流**：与 T1c 前一致，T1b gate 裁定范围；发布前计划显示清理项。未改。
- **L4 `Node.ProtocolParams`（`json:",embed"`）接住所有节点的未声明成员**（不只是扁平节点），因此节点不会丢未声明成员；导出脱敏与测速遍历
  `EncodeMembers`/嵌入值，复制共享只读字节（注释已要求替换而非修改）。无问题。
- **L5 严格输入**：抽查 MosDNS 设置（`DecodeInputMember` 严格、非对象仍先按「必须是对象」）、EasyTier（原始 JSON 上 `NormalizeEasyTierJSON`）、
  自定义配置非字符串在解析前 400、订阅聚合开关按真值存；未发现放宽。

## 性能（问题 6）

- batch-34 未合入 go，batch-38 树里没有 R11 测试。复核把 batch-34 的 `o10_r11_alloc_test.go`/fixture/race 文件拷进两个临时 worktree
  （batch-38 23fe17a97 与 go 9d9e35d89），在 .64 同机先后跑 `TestO10RouteAllocationCeilings`（非 race）：
  - `GET /api/agents/<id>/rule-sync`：0.14 → 0.14 MB/次（上限 0.30）；`GET /api/routers/<id>/rule-sync` 0.15 → 0.13。19 条路由 T1c 都 ≤ go。
  - 两棵树都超的只有 `mihomo` 配置（go 29.2 / T1c 25.4，上限 13.9）和聚合 provider（144.5 / 133.2，上限 46.0）：这两个上限依赖 batch-34 自己的 R8/R9 优化。
  所以作者「batch-34 的 19 条上限测试全部通过」不可能是在单独的 T1c 树上测的（应是叠了 batch-34 的树）。合并 batch-38 与 batch-34 时
  （convert/rules 有冲突）需在合并树上重跑 R11。
- 生产快照上 rule-sync 4.8 → 5.9 MB 在合成 fixture 上不复现（持平），是数据相关的；延迟不变，不触碰 R11 上限。未用快照剖析。

## 提交（batch-38，23fe17a97 之后）

- 498c89695 go(model): O18 T1c review: damaged router guard switches and rule-set selections fail closed（H2、M1）
- 054f2e41f go(repo): O18 T1c review: a settings save keeps a damaged config_token until a token is set（H1）
- b3a7840ea go(repo): O18 C10 review: an unreadable path component is kept as given, as before C10（L1）
- 812c605c9 go(model): O18 T1c review: real-Python HR writer check accepts Agent always-written zeros…（M2）
- 本报告与 behaviour-changes 补充单独提交

## 验证

- `gofmt -l internal tools cmd` 无输出；`go vet ./...` 与 `-tags configflow_proddiff,configflow_difftest`：VET-OK（.32）。
- `-race -timeout 60m -p 6`（.32，含 3 个修复提交）：model 7.4 s、routers 555.7 s、repo 255.2 s、auth 6.9 s、api/settings 22.2 s、api/routers 40.6 s、
  setup 34.2 s、clients 22.9 s、agents/ipv6 18.2 s、mcp 6.3 s、substore 81.6 s、webdav 5.9 s、mosdns 221.7 s 全部 ok，无 DATA RACE。
- difftest（.64，`-tags configflow_difftest`，环境同 gate-live.py，真 Python）：repo 155.0 s（含 CrossCheck、`TestStoredRoundTrip`、N85）、routers 294.1 s、
  auth、api/config/difftest 80.4 s、httpx/difftest 28.4 s、api/rules/difftest 61.1 s、api/rulelibrary/difftest、api/subscriptions/difftest 100.8 s、
  mosdns/difftest、devicenotes/difftest、webdav/difftest、mcp/difftest、discovery/difftest、api/aggregations/difftest、substore/difftest 全部 ok；
  model 首轮 `TestHRPythonWriters` 失败（M2，作者树已失败），修后 `ok configflow/internal/model 0.720s`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-38 x 6`（.64，树 812c605c9）：`PREFLIGHT PASS`。
- 负向对照（.32）：去掉 `keepDamagedConfigToken` 调用、去掉 `stored:"selection"`、守卫开关真值判断恒 false → 三条复核测试全部失败。
