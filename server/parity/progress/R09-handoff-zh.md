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
| WORKFLOW 第四条／A24 F2／C32 high | Detach 后、每个 R09 perform 前、每个 manifest 提交前检查 ctx.Err；真实 Runtime.Start/BindRequest/Shutdown 覆盖 shutdown 后零请求／零 manifest 提交、空恢复、最后一次快照／删除归属证明后 shutdown；断开请求仍完成写入与恢复。 |

R09 不新增锁、后台线程或循环；L1/L2 和 lock-before-validation 的外层 HTTP／发布职责留在 R12/R14/R15。

## 问题及与 Python 的差异

- 中：Python 在预览后出现同名 foreign 对象／前插 53 端口规则时，apply 的保留检查仍可能通过。Go 写前拒绝，要求重新预览。
- 中：Python 删除前从新读取的行建立 expected、最终直接刷新 config，可能删除或吸收中途外部修改。Go 对照已记录的证明，保留被改对象。
- 低：Python 回滚只按 ID 重映射，可能跨 RouterOS 表混淆；Go 按 path+ID。新增 TestR09TableLocalIDsSurviveRestore 在四张表使用重复旧 ID，真实 Python 复现错误，Go 恢复与公开 VerifyDNSGuard 均通过；`.32` race 1.568s。
- 低：畸形／重复 manifest 身份明确拒绝，恢复保留未知字段。
- 低：接口引用增加资源预算；空／错型动作、错型 include 保持未知。
- 仅格式：Go 使用紧凑 JSON，Python requests 有空格／不同键顺序；解析后相同，原始字节分别检查。
- 中（已加发布防线）：未登记且 comment=`cf-dnsguard:hijack` 的 foreign NAT，包括 disabled=true 的旧 53 规则，Python 不阻断。Go 在构建和执行前阻断并给中文说明；正常 manifest 归属对象通过，撤销仍可进行。不论当前 failover 开关都保护该保留 comment，避免以后开启时继承冲突。

## 待 Opus 裁定：Netwatch 运行时脚本的归属

具体场景：某 NAT 未登记在 manifest 中，`disabled=true`，comment 却为 `cf-dnsguard:hijack`。Netwatch 的 up 动作执行 `enable [find comment="cf-dnsguard:hijack"]`，会启用该 foreign NAT；down 动作同样可能停用它。选择器不检查 chain、端口、ID 或完整配置。**Python 和 Go 当前生成的脚本一致。** API 发布的 ID+配置归属证明不能证明运行时脚本的安全。暂按潜在高严重度提交复核，不宣称已修复脚本合同。

本次小修仅阻断预览／写前快照已存在的同 comment foreign NAT。它不会停用已运行的旧 Netwatch，也不能消除写前检查之后或发布之后出现 foreign 对象的时间窗；这一限制必须由后续运行时设计处理。

| 候选修法 | 删除重建 | 回滚 | ID 复用／其他边界 |
|---|---|---|---|
| 脚本按受管 ID 绑定 | RouterOS 删除重建会分配新 ID，旧脚本漏控新规则；预览时新 ID 尚未知。 | 补偿重新创建得到新 ID，必须同步脚本与 manifest；恢复旧脚本可能指向失效 ID。 | 旧 ID 被 foreign 规则复用时，单凭 ID 仍会误操作，需另行配置或身份校验。 |
| comment 增加 manifest 指纹或 router id 等标识 | router id 在同设备上不区分新旧代；manifest 指纹若含对象 ID，会在重建后变化；不含 ID 则不能区分同配置的 foreign 副本。 | 需明确恢复哪一代标识，并同时恢复规则和脚本；部分补偿可能留下混合代。 | ID 复用配合旧标识残留、复制同标识仍有误认可能；额外标识是候选身份合同，不能天然当作归属证明。 |
| 每次发布后取得 ID，再重写脚本写入 ID | 新建后可获 ID，但引入脚本更新阶段和暂时不一致窗口；无变化发布也重写会破坏现有增量合同。 | journal 必须记录脚本重写，补偿重建后重新绑定实际新 ID，不能机械恢复旧脚本文本。 | 仍需防止发布后的 ID 复用，并验证完整配置；脚本重写请求失败／结果不确定时须定义安全状态。 |

建议 L5 仅在获授权的隔离 CHR 实验网执行：建立一条未登记、disabled=true、同 comment 的 NAT，触发 up/down 并观察其 enabled 状态及实际命中计数；分别覆盖普通 53 规则和非 53／非 dstnat 规则。验证发布前阻断、预览后插入与发布后插入的差别；受管 NAT 删除重建、回滚中新 ID、foreign ID 复用、Netwatch 正在执行时更新脚本、重启及网络丢失后的恢复。各修法还需验证无变化发布保持增量行为、暂停状态保持及失败补偿后仅操作已证明归属的对象。本次不操作真实 RouterOS。

完整 Python 测试逐项映射、文件行数、接口与精确命令见 [R09-dns-guard.md](R09-dns-guard.md)。
R14 baseline、R12/R15 真实 HTTP 发布／指纹／规则同步长流程是其消费者的测试范围，没有在 R09 伪造接口冒充完成。

## 验证结果

测试机固定 `.32`，命令均通过 rt；日志不包含生产值。

最终 R08+C32 合并树改用 `.64`，已 rebase `go@e980d402`，R09 代码截至 `cf5a015b`。执行：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GATE_LOGS=/tmp/configflow-r09-integrated-20261118 server/internal/routers/testdata/r09_gates.sh'
```

wrapper 调共享 gate.sh，参数 `-p 6`，三个 fuzz 各 60 秒，最后四类编译变异；日志在 `.64:/tmp/configflow-r09-integrated-20261118`。首次排队期间 go 新增第四条常设检查，故仅终止了本任务等待槽位的 gate（尚未执行任何检查，rt143），补 shutdown 提交点防线后重新启动；最终结果待结束后填写。集成后的真实 Python／报告／manifest 定向 race 已通过 24.885s；gofmt、routers vet 和更严格组合用例 race 通过 1.124s。

- 前两轮：R09 Python 对照、安全测试、routers/routersplit/devhttp race、全量、12 difftest、三个 60 秒 fuzz、四类编译变异通过。
- 合入 N7-3c 的一轮：84 包全量通过，routers race 506.150s；proddiff `new=0 stale=0`。
- ae2e0f59 + R09：完整 `-race` **84 包通过**；routers 567.091s、routersplit 23.165s、devhttp 3.764s。日志 `.32:/tmp/configflow-r09-final/race.log`。
- proddiff 已知例外：`new=3 stale=0`，全部为 aggregation provider；原日志 `.32:/tmp/configflow-r09-final/proddiff.log`。
  协调者在未加 R09 的 `go@df6928ae`、`.64` 复现相同三条，并指示 R09 照常交回，另派 `go-fix-agg-provider-load` 排查。
  具体是全局 provider 的 `body.proxies` 长度、全局及 profile provider 的 `header.X-ConfigFlow-Provider` 缺失。未改 baseline。
- 最终变基到 `go@9a208c6c` 后：两种 tag 的 build/vet/lifecycle 全过；12 difftest 全过；三个 60 秒 fuzz 全过（60.305/61.226/61.308s）；四类编译变异均检出。补跑命令退出 0，日志 `.32:/tmp/configflow-r09-remaining/`。
- 最终 `go@9a208c6c` 变基后再次执行 `rt --no-sync -- bash -lc 'cd server && go test -race -count=1 -timeout=10m -run "^TestR09" ./internal/routers'`，通过：`ok configflow/internal/routers 41.458s`。
- 同 comment foreign NAT 防线补充后，`.32` 执行下列命令退出 0；gofmt 无输出、vet 通过，末行 `ok configflow/internal/routers 36.374s`。新测试 `TestR09ForeignHijackCommentBlocksPublish` 对照真实 Python，另在 `TestR09LateForeignObjectsAreNeverClaimed/hijack_comment_collision` 验证晚插入导致写前拒绝。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers/dns_guard_messages.go internal/routers/dns_guard_plan.go internal/routers/dns_guard_write.go internal/routers/r09_safety_test.go)" && go vet ./internal/routers/... && go test -race -count=1 -timeout=10m -run "^TestR09" ./internal/routers'
```

## 共享文件与接线

仅最小修改 `desired.go`、`model.go`、`report.go`、`routeros.go` 的字段／manifest set 标志。
ReportParts 链式保留前驱，R08 的 policy report 保留。R09 直接读取 `Desired.ROSPolicy.CIDRs`，已删除临时 `DesiredDNSGuard.PolicyCIDRs`；同口提示直接使用 `state.ROSPolicyPlan.Items`，不再往返报告 JSON。只有启用且无 blocker 的 policy plan 提供提示目标。
filter 优先读取非 nil 的 R08 snapshot，其次才是 DNS guard snapshot；空列表是有效证据，缺少 filter 时发起新读取，不误退回另一份快照。真实 Python 对照覆盖启用／禁用／被阻断，以及两种快照、优先级、空／缺少 filter。
R08 的 DNS manifest 标志和 setter 各保留一份，`Manifests()` 完整保留；合并后 R09 对 `routeros.go` 没有额外 diff。R12 获取 `Manifests().DNSGuard` 的值与 set 标志（包括显式 null），保存成功或补偿后的 manifest；`DNSGuardPublication()` 保留为便捷接口。组合测试验证 BuildReport 同时保留两个可发布计划，DNS 冲突直接读取原生 policy CIDR，恢复 DNS 不覆盖 Policy manifest，返回副本不别名共享证明。
C32 的 ComputeDesired／RulesetContent context 首参及已有调用方 ctx 完整保留；R09 没有这两个函数的新增调用点。R09 和 R08 共享写入／回滚路径均用 lifecycle.Detach。
跟进 `go@f73983fb` 第四条：R09 自有 owner 在 Detach 后、每个 perform／manifest 提交前显式检查 ctx.Err，避免零 I/O 恢复或最终回读后的快速提交越过 shutdown。测试使用真实 Runtime 的 Start/BindRequest/Shutdown，没有替换取消源、超时或生产 HTTP。
`.64` 定向验证：gofmt 无输出、routers vet 通过，runtime／shutdown／请求断开测试 race 通过 2.043s。删除前探针按共享驱动真实的 Netwatch 集合回读定位，不假设存在单行端点；原先一次全部 R09 运行的失败来自该探针，最终 gate 将重新运行全套。
R12/R15 把 ForwardHint 放到 report/warnings，不能作为 fingerprint 输入。

## L5 与交回

L5 尚未执行：真实 CHR 接受 payload/scripts、UDP/TCP DNS/hairpin/853、HA VIP 与真实主机豁免、15 秒故障确认、恢复 conntrack 清理、重启撤销、链路丢失、规则顺序及同口命令的实际流量效果。
只能由协调者在隔离实验网安排，不能把 loopback fake 通过当实流证明。

分支 `go-port-r09`，已 rebase 到包含 R08+C32 与并行化测试／N7-4 writer 的 `go@e980d402`。未 push，临时基线 worktree 已清理；最终提交及门禁结果将在完整 gate 结束后记录。

独立验收已完成，R08+C32 已整合；最终门禁固定 `.64`、`gate.sh -p 6`，完成后才交回。历史 aggregation proddiff 例外已由独立修复合入 go，最终门禁仍须证明 new=0 stale=0；旧失败保留作为历史记录。L5 和 Netwatch 运行时归属裁定不作通过宣称。
