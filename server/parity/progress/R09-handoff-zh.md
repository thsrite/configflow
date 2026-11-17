# R09 交接

状态：`ported`（2026-11-18），等待新的 Opus HR 复核；不标 `verified`。

## 完成内容

- 移植 DNS guard 与 IPv4 同口转发提示，覆盖 upstream PR #572 / `2a8cb64c` 的增量发布。
- 逐对象预览保留／新增／更新／移除；只替换变化对象，未变化的 NAT/filter ID 与 Netwatch 暂停状态保留。
- 校验直连 LAN、OxiDNS 单机／HA VIP 与两台真实主机、代理目的网段冲突；生成放行、hairpin、劫持、853 与 Netwatch 对象。
- 根据 manifest 的完整稳定配置及 ID 证明归属；写前重验、回读、逆序 journal、恢复锚点及按表重映射 ID；拒绝与不确定写入分开处理。
- 写入口与恢复入口使用 lifecycle.Detach；每次设备 I/O 保留生产用的 10 秒超时。
- 同口提示只读，不进入操作、checks、evidence 或 fingerprint。共享求值器在 `ipv6_policy_eval.go`，R10 扩展该文件。
- 真实 Python 对照：计划、写入、journal、manifest、错误、最终设备表；提示的全部读取；三值求值结果。另测原始请求字节／headers、真实超时、取消、外部修改及未知字段保留。

无生产／真实路由器／真实 DNS／PVE 操作，无 push，无子 agent，无新依赖。

## HR 清单逐项

| 来源 | R09 如何避免 |
|---|---|
| R04 F1 异常或多余 ID | 复用 perform 的新鲜合法 ID／scope 证明；异常 manifest/操作先拒绝，入口 fuzz。 |
| R04 F2 中文错误 | 提示集中在 *_messages.go，Python 对照逐字比较。 |
| R04 F3 浏览器取消 | 写入、回读、补偿在 detached context 完成，生产超时不变；取消测试覆盖。 |
| R04 F4 清理与批处理 | 最多 64 条放行及固定规则，按 Python 逐对象写；复用既有驱动清理，不另造批处理。 |
| R04 F5 动态对象 | 共享 payload 拒绝接管／删除动态对象。 |
| R04 F6 journal | 破坏性请求前记录，逆序恢复，锚点及新 ID 回填；丢失 DELETE 回复实际补偿。 |
| R04 F7 复合字段 | 原始行用 sameRow；稳定行用共享 canonical 比较，不对原始数组／map 做 maps.Equal。 |
| R04 F8 大小限额 | 沿用设置上限及共享批量限制，不新增无界 console 批次。 |
| R04 F9 缺少 expected | manifest 全快照、即时删除前及最终回读再次证明；评论／名称不是所有权。 |
| R04 F10 私有副本 | 复用 stableROSRow、ownershipSnapshot、perform、地址读取及 netipx；求值器明确共享。 |
| R05 1 共享 HTTP 隐式行为 | 不改 HTTP 路径；原始 body/header 与既有驱动回归覆盖。 |
| R05 2 写错误类型 | 写失败均为中文 RouterError／回滚错误；不把普通错误文本当发布成功。 |
| R05 3 缺失身份误匹配 | ID、role、path、config 必须存在有效；重复证明拒绝，不能靠空值相等认领。 |
| R05 4 ID 复用后的 undo | 每次破坏性恢复核对 expected 全快照，改过的对象保留现场。 |
| R05 5 二次扫描 | 归属读取一次建索引；按 ID 的网络回读仅在协议要求处使用。 |
| R05 6 拒绝／不确定 | 4xx 拒绝不产生对象；transport/5xx 未证明的新对象保留；删除前已有恢复记录。 |
| R06 F1 替身遮盖生产参数 | wrapper 只记录超时，不改值；另用真实 devhttp 等待 11 秒的设备证明约 10 秒超时。 |
| R06 F2 脱敏过宽 | 沿用共享 internal/redact，只遮密码／token，不遮用户名。 |
| R06 F3 原始请求 | GET/DELETE 无 body/Content-Type；Go/Python 的原始 PUT 字节和 Basic auth 分别有精确期望。 |
| R06 F4/F5 动态类型／dict 合并 | 不改 iKuai；新计划、操作、检查、journal 均用原生 Go 类型。 |
| R07 F1 共享路径回归 | 不改 request/redact；在合并树运行 R04–R07、routersplit、devhttp。 |
| R07 F2 错用网络语义 | 成员身份使用 CIDR 解析，不把地址范围悄悄变成 CIDR 键。 |
| R07 F3 规模复杂度 | 归属建索引，网络折叠复用 netipx；规则／接口引用有预算。 |
| R07 F4 缓存别名 | 写前失效地址读取；复用拥有独立副本的共享读取器。 |
| R07 F5 长度代替集合 | manifest 角色逐项按顺序比较，不以长度相同作为证明。 |
| A19 F1 alias/图爆炸 | R09 不解析 YAML；规则分支 256、跳转 12，接口引用也有预算与循环检查。 |
| A19 F2 错误形状静默成功 | typed JSON 验证；无快照为 blocker／unknown；空或错误 action/include 不默认放行／空列表。 |
| A19 F3 解析器不一致 | 不新增 YAML/classical 解析器；D1、alias 预算、ErrSyntax 才可回退的规则保持在共享 yamlx。 |

R09 不新增锁、后台线程或循环；L1/L2 和 lock-before-validation 的外层 HTTP／发布职责留在 R12/R14/R15。

## 问题及与 Python 的差异

- 中：Python 在预览后出现同名 foreign 对象／前插 53 端口规则时，apply 的保留检查仍可能通过。Go 写前拒绝，要求重新预览。
- 中：Python 删除前从新读取的行建立 expected、最终直接刷新 config，可能删除或吸收中途外部修改。Go 对照已记录的证明，保留被改对象。
- 低：Python 回滚只按 ID 重映射，可能跨 RouterOS 表混淆；Go 按 path+ID。新增 TestR09TableLocalIDsSurviveRestore 在四张表使用重复旧 ID，真实 Python 复现错误，Go 恢复与公开 VerifyDNSGuard 均通过；`.32` race 1.568s。
- 低：畸形／重复 manifest 身份明确拒绝，恢复保留未知字段。
- 低：接口引用增加资源预算；空／错型动作、错型 include 保持未知。
- 仅格式：Go 使用紧凑 JSON，Python requests 有空格／不同键顺序；解析后相同，原始字节分别检查。

完整 Python 测试逐项映射、文件行数、接口与精确命令见 [R09-dns-guard.md](R09-dns-guard.md)。
R14 baseline、R12/R15 真实 HTTP 发布／指纹／规则同步长流程是其消费者的测试范围，没有在 R09 伪造接口冒充完成。

## 验证结果

测试机固定 `.32`，命令均通过 rt；日志不包含生产值。

- 前两轮：R09 Python 对照、安全测试、routers/routersplit/devhttp race、全量、12 difftest、三个 60 秒 fuzz、四类编译变异通过。
- 合入 N7-3c 的一轮：84 包全量通过，routers race 506.150s；proddiff `new=0 stale=0`。
- ae2e0f59 + R09：完整 `-race` **84 包通过**；routers 567.091s、routersplit 23.165s、devhttp 3.764s。日志 `.32:/tmp/configflow-r09-final/race.log`。
- proddiff 已知例外：`new=3 stale=0`，全部为 aggregation provider；原日志 `.32:/tmp/configflow-r09-final/proddiff.log`。
  协调者在未加 R09 的 `go@df6928ae`、`.64` 复现相同三条，并指示 R09 照常交回，另派 `go-fix-agg-provider-load` 排查。
  具体是全局 provider 的 `body.proxies` 长度、全局及 profile provider 的 `header.X-ConfigFlow-Provider` 缺失。未改 baseline。
- 最终变基到 `go@9a208c6c` 后：两种 tag 的 build/vet/lifecycle 全过；12 difftest 全过；三个 60 秒 fuzz 全过（60.305/61.226/61.308s）；四类编译变异均检出。补跑命令退出 0，日志 `.32:/tmp/configflow-r09-remaining/`。
- 最终 `go@9a208c6c` 变基后再次执行 `rt --no-sync -- bash -lc 'cd server && go test -race -count=1 -timeout=10m -run "^TestR09" ./internal/routers'`，通过：`ok configflow/internal/routers 41.458s`。

## 共享文件与接线

仅最小修改 `desired.go`、`model.go`、`report.go`、`routeros.go` 的字段／manifest set 标志。
ReportParts 链式保留前驱。R08/R12 提供 `DesiredDNSGuard.PolicyCIDRs`；R12 获取 `DNSGuardPublication()` 的值与 set 标志，保存成功或补偿后的 manifest。
R12/R15 把 ForwardHint 放到 report/warnings，不能作为 fingerprint 输入。

## L5 与交回

L5 尚未执行：真实 CHR 接受 payload/scripts、UDP/TCP DNS/hairpin/853、HA VIP 与真实主机豁免、15 秒故障确认、恢复 conntrack 清理、重启撤销、链路丢失、规则顺序及同口命令的实际流量效果。
只能由协调者在隔离实验网安排，不能把 loopback fake 通过当实流证明。

分支 `go-port-r09`，已 rebase 到 `go@9a208c6c`。实现提交：`54a3fd9f`（核心）、`63986dd9`（HA/解析门禁）、`9040581f`（未知选择器修正）；其后为门禁与文档提交。未 push，临时基线 worktree 已清理。

独立验收已完成。协调者追加最终集成要求：等待 aggregation-oracle 修复 `72d4b2c0` 和已 verified 的 R08 合入 go，再 rebase、去重 manifest 标志并保留 Manifests()；接入 R08 的策略目的 CIDR 与 filter 快照，使用共享 gate.sh 重跑最终门禁后才交回。R09 自有写／恢复已用 lifecycle.Detach。已知 proddiff 例外及 L5 不作通过宣称。
