# A21 gateway onboarding 交接

状态：独立部分已实现并完成定向验收，HR 未复核；已 rebase batch-4（含 batch-1/O3-6c），等待 C34/C36 合入后最终接线，暂不可标 ported 或合入。分支 `go-port-a21` 从 `batch-1@778d8f8e2` 建立。

## Clarified in A21

- DNSLoop 等 C34 复核合入后 rebase 接线，不 cherry-pick、不重写；保留 TODO(C34)。
- C36 尚未实现 `adopt_owned_write`。A21 保留 hook 注册点。没有未完成 session 的普通 onboarding 保存照常；存在未完成首次使用 session，或现有 `setupflow.ProfileSessions` reader 读取失败/损坏，在 profile 提交前返回 503「等待首次使用向导模块」，DB、文件、revision 均不变。GET、preview 不受此写入就绪检查影响。不计算 digest、不制造替代状态。
- C36 实现 hook 后，须补 Python `_revision+1` 条件、会话锁及 ownership digest 对照；不是 A21 的临时实现范围。
- 所有测试只经 rt，合成数据；定向检查后交回，完整门禁由协调者批次执行。

## 外部 owner 衔接

- C34：共享 DNSLoop，等待合入。
- C36：setup flow `adopt_owned_write`，等待实现。

## 验证

测试服务器：192.168.0.64。未跑完整 gate、全量测试或 proddiff；由协调者批次执行。实际命令（从本 worktree）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/gateway/onboarding internal/api/gatewayonboarding internal/oxidns/upstreams.go internal/oxidns/references.go && gofmt -l internal/ tools/ cmd/ && go vet ./... && CONFIGFLOW_DIFF=1 go test -p 4 -race -count=1 -timeout 15m ./internal/gateway/onboarding ./internal/api/gatewayonboarding ./internal/oxidns ./internal/api/oxidns ./internal/api/agents ./internal/api/proxygroups ./internal/api/rulelibrary ./internal/api/rules ./internal/convert ./internal/deploy ./internal/ippresence ./internal/routers ./internal/server && CONFIGFLOW_DIFF=1 go test -count=1 -timeout 10m ./internal/api/oxidns/difftest && go test -run "^$" -fuzz "^FuzzGatewayOnboarding$" -fuzztime 60s -parallel 2 ./internal/gateway/onboarding'
```

`gofmt -l` 无输出，`go vet ./...` 通过；13 个改动包/直接调用方 race 均通过。最后输出：

```text
ok configflow/internal/oxidns                 766.032s
ok configflow/internal/api/agents            259.085s
ok configflow/internal/ippresence             874.622s
ok configflow/internal/routers                821.764s
ok configflow/internal/server                   1.833s
ok configflow/internal/api/oxidns/difftest       10.319s
PASS
ok configflow/internal/gateway/onboarding       61.022s
```

收口时修复了 Go string 解码 null 会保留默认值的问题（default_forward=null 按 Python 拒绝）；另拒绝把损坏的非对象 Mihomo 段静默当空对象修复。对这些实际改动的包定向重跑 race/Python 对照与新 fuzz，未重复引擎/其他直接调用方的大套件。随后按 C36 请求公开同一份 DNS leaf，做该抽取的定向验证。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/gateway/onboarding && gofmt -l internal/ tools/ cmd/ && go vet ./internal/gateway/onboarding ./internal/api/gatewayonboarding ./internal/server && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout 3m ./internal/gateway/onboarding ./internal/api/gatewayonboarding ./internal/server && go test -run "^$" -fuzz "^FuzzGatewayOnboarding$" -fuzztime 60s -parallel 2 ./internal/gateway/onboarding'
```

最终 leaf 抽取结果见文末。另将 HTTP 中文常量移到 messages.go 后，`gofmt -l internal/ tools/ cmd/` 无输出、`go vet ./internal/api/gatewayonboarding` 通过。

Python HTTP 对照复用 A20 的 Flask oracle；SQLite 字段/位置由独立短生命周期 sqlite3 reader 读取，所有真实 Python 调用都只在 rt 测试服务器上。所有数据为合成。

## 当前实现

- `gateway/onboarding` 使用原生表单、状态、报告、发布 guard；未知配置段以 `json.RawMessage` 保留，已知资源通过 `model.Profile/Shared/System` 读取。R5 的 nil fallback 不单独当作 enablement 或 ownership 证明：显式 null/错误开关 fail-closed；C36 hook 需要可验证的整数 revision。
- 复用 A19 editor/selection/overlap，A20 的 setupflow reader，routersplit marker/完整 TUN，C18 Mihomo generator，C22–C25 OxiDNS normalize/common/validation/reference engine。`jsonx` 仅留在 adapter 与尚未迁移的 repo/convert/routersplit 边界。
- OxiDNS 仅新增共享 upstream reader 的导出入口，以及允许额外引用字段的 `ReferenceOptions`；既有 `ReferenceLocations` 默认语义不变。A21 的动态引用证明还包含嵌套 tag，复用同一遍历，不复制引用 parser。
- 候选仅替换 Mihomo/OxiDNS 两段；保存前再次比较 config revision 与 marker 状态，并在同一 repo 事务生成/验证两份服务配置。Report 不返回 candidate 或原生备份。管理 API 的启用/半份认证清理、fallback 恢复、显式模板转换保留全部备份、query recorder、策略可达性及 DIRECT overlap 均有边界对照。
- save owner Detach 后检查 ctx，在事务前、回调开头、回调交回前、C36 hook 前再次检查。真实 Runtime 覆盖先关机、事务内生成时关机、请求断开继续完成。未改共享 repo/leaf 提交结构；batch-4 合入后沿用 O3-6c 的最终 CommitContext 检查，合并态验证见文末。
- HTTP 注册 3 个方法入口，auth.Require，HEAD 只读；256 KiB 实际/声明长度限制，按 Python 的 raw json.loads 契约忽略媒体类型，统一走 BindSilent。畸形/非对象/non-finite body 回归均检查无写入；GET/HEAD headers 一致，响应 no-store。
- 脱敏从同一个配置快照显式取当前/退役 rule token、Agent token、subscription token 与 fetch proxy，再由共享 CollectCredentials/SanitizeWithSecrets 处理命名凭据、环境 registration key、编码引用。这样不会因公共投影先丢弃敏感字段而遗漏旧 token。

## Python 测试映射（按行为边界，不做形状 × 引擎穷举）

遵循 `WORKFLOW@5264ba80` Leaner verification，无变异门禁；保留正常路径与每类错误代表。

| Python `test_gateway_onboarding.py` 测试族 | Go 边界/契约 |
|---|---|
| 双服务预览、原生配置保留/恢复备用、外部/动态引用、显式转换备份 | `TestPythonPreparation` template/native/convert；比较候选、两个实际 generator 的结构化输出、state 与 AssertReady |
| 直连 DNS、表单错误、域名选择/改名/MRS/停用、策略预条件 | 同一真实 Python corpus 的 dns/form/choices；`TestFormShapesAndLimits`、`TestUnavailableSwitchCannotProveSourceReadiness` |
| API 清理/保留认证、原生 API、新旧 recorder、默认 DNS、DIRECT overlap、state 警告 | 同一 corpus 的 api/native-api/recorder/default/direct-ip-overlap/actionable-warnings |
| scoped preview/save、拒绝保存、revision guard、鉴权 | `api/gatewayonboarding/difftest.TestFlaskHTTP`（复用 A20 Flask oracle，标准 difftest 目录纳入批次门禁），每次比较 HTTP、实际 SQLite 字段/位置、完整 DB 文档和业务文件；`TestConcurrentExpectedRevisionAndStaleGuard`；HTTP auth/method matrix |
| saved readiness 与 full_tun/editor/marker | corpus full_tun/full-tun-saved/full-tun-invalid，`AssertReady`；DNSLoop 主机地址/所有上游字段的最终对照待 C34 接线后完成 |
| 首次使用 session/takeover 与 adopt | `TestSetupReadinessContract`、`TestAdopterHookReceivesOneCommittedRevision`、HTTP 503/Flask 显式预期依赖差异；真实 C36 ownership 更新留给 C36 |
| provider 必须配置 token / opaque native backup 编辑器往返 | 复用 A19/A20 既有 provider HTTP 和 C29 authenticated editor 边界；A21 仅调用共享配置验证，不另做该入口 |
| 新增安全回归 | `TestOnboardingRuntimeShutdownAndDisconnect`；HEAD/Bind/auth；原生 40 层 alias/自引用/重复键；`FuzzGatewayOnboarding` |

## 尚未闭合（不能标 ported）

1. C34 合入后 rebase，生产 `Register` 与 A22 的 `AssertReady` 统一接共享 DNSLoop；补齐生产 checker 注入并更新 TODO（公共 helper 的 nil 拒绝保护仍保留），补实际循环/非循环代表与主机地址对照。当前非回环独立 corpus 与 HTTP 测试显式注入纯 seam，不证明 DNSLoop 已接线。
2. batch-4（含 batch-1/O3-6c）已 rebase；共享最终提交入口的合并态验证见文末。
3. C36 拥有 session 文件锁、digest、adopt 的真实实现；合入后由 A21 按 C36 交接清单负责 server 的 SetAdoptOwnedWrite(setup.AdoptOwnedWrite) 和 DNS callbacks 注册，并补 server 级回归证明注册后 503 消失。当前裁定的 503 只是安全衔接，不是永久替代状态。
4. 独立 Opus HR 复核、协调者批次完整 gate、A22 安装/设备执行及 lab 行为不在本次已验证范围。

## C36 已对齐的准确接口

C36 owner `89c495b3`（`go-port-c36`，worktree 名 `go-review-c34`）已确认：编排放 `internal/setup`，只读 reader 留在 `internal/setupflow` 叶层，server wiring 调用 `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)`，不形成 import cycle，不复制 reader。

```go
type AdoptOwnedWrite func(context.Context, *repo.Repository, string, json.RawMessage, json.RawMessage) error
func SetAdoptOwnedWrite(fn AdoptOwnedWrite) func()
```

参数为同一 Runtime generation 的 detached ctx、同一 repo、profileID、完整存储 before/after profile JSON（非 hydrated shared/system snapshot）。注册器 mutex 仅保护函数指针；调用前已释放。保存成功、repo L3 已释放，且明确的 `saved._revision == before._revision+1` 时调用；没有 session 锁，也没有其他 A21 锁。nil 包括没有匹配 session/owned_digest 的正常 no-op；错误保留链向上传播，已提交的 profile 不回滚，与 Python 顺序相同。C36 owner 仍需自己 Detach，在 session 操作锁后和 rename 前检查 ctx，不能另起 Background generation。已由 C36 确认会覆盖 `+1`、stored-profile digest 与 Runtime 契约。

锁术语补充：C36 已确认 Python session 锁跨 repo/Agent I/O，其编排用同一 keyed session 操作锁（L2 → L3），纯 file replace 才是叶提交区。A21 hook 不指定 L4；它只保证调用时没有 L3/session 锁，避免 L3 → session 的反向等待。不可中断区仅覆盖实际原子提交，不覆盖整段编排。


## C36 纯功能复用

C36 另需的 `_local_upstreams` 与 `_prepare_mihomo_dns` 不复制：

```go
func LocalUpstreams(value string) (*yamlx.Node, error)
func PrepareMihomoDNS(document *yamlx.Node, localAddresses []netip.Addr, check DNSLoopChecker) (*yamlx.Node, error)
func DefaultMihomoNameservers() []string
```

Mihomo leaf 不 I/O、不写 repo；输入是 caller 独占、经 yamlx 解析的临时 Node，原地补齐 nameserver 并保留其他字段/alias graph；nil 对应空白源文本的初始模板。失败丢弃临时 Node。常量 `DefaultLocalDNS`、`DNSTarget`、`DefaultMihomoPrimaryDNS`、`DefaultMihomoSecondaryDNS` 由 A21 拥有，默认列表返回新 slice。C36 已收到准确签名，checker 直接适配它基线上的 C34 共享函数；A21 自身仍等 C34 合入后接线。


## 与 Python 的差异及发现

- 已裁定差异：HEAD 只读修正；C36 暂缺 owner 的 503 安全衔接。除此之外，已覆盖的正常 HTTP、双服务候选/生成结果、存储结果无差异。
- 请求 JSON 遵守 N8-6 的原生标准 JSON 契约：NaN/Infinity 不模拟 Python json.loads 的非标准接受行为，返回 400 无写入。Go 回归覆盖，不新增 Python 形状矩阵。
- 作者自查修复：域名候选独立 DTO，避免多出 IP editor 的 selected 字段；显式 null 开关/行为不用于有效来源证明；退役凭据种子在投影前收集；default_forward=null 拒绝；损坏的非对象 Mihomo 段不被修成可保存的空配置。
- 仍未完成/未复核：C34 checker 接线，C36 真实 adoption owner/server 注册及成功回归，以及独立 Opus HR 复核。O3-6c 合并态复验见文末。
- N8-5 后续迁移位置：`onboarding/adapter.go` 的 legacy repo/generator/router-split 桥、`service.go` 的 GetPublishSnapshot/GetProfile/UpdateProfileTransaction callback；业务 request/report、DNS/原生图算法不应随之重写。

## 最终 leaf / difftest 定向复验

公开 C36 复用 leaf、将 HTTP 对照整理到标准 difftest 目录之后，精确命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/gateway/onboarding --back server/internal/api/gatewayonboarding -- bash -lc 'cd server && gofmt -w internal/gateway/onboarding internal/api/gatewayonboarding && gofmt -l internal/ tools/ cmd/ && go vet ./internal/gateway/onboarding ./internal/api/gatewayonboarding/... ./internal/server && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout 3m ./internal/gateway/onboarding ./internal/api/gatewayonboarding/... ./internal/server && go test -run "^$" -fuzz "^FuzzGatewayOnboarding$" -fuzztime 60s -parallel 2 ./internal/gateway/onboarding'
```

```text
ok configflow/internal/gateway/onboarding                  20.667s
ok configflow/internal/api/gatewayonboarding                5.839s
ok configflow/internal/api/gatewayonboarding/difftest        6.210s
ok configflow/internal/server                               2.075s
PASS
ok configflow/internal/gateway/onboarding                  61.027s
```

最终 fuzz 225 个缓存 baseline，15334 次执行，新发现 16 个 interesting inputs，无失败。早期完整直接调用方的命令展示省略了仅取回 gofmt 文件的 --back 参数；这里的最终命令包含它们。

共享 OxiDNS helper 提交：`7129d0ef4`。主实现提交见本分支随后的一条 `go(gateway): A21 ...` 提交。

## 提交与交回状态

- `7129d0ef4`：共享 OxiDNS upstream/reference 入口。
- `0ade86dd8`：A21 原生准备/状态/安装前验证、原子 owner、3 个 HTTP 方法入口、C36 hook/纯功能 API、定向测试与差异登记。
- 工作分支 `go-port-a21`，起点 `batch-1@778d8f8e2`；当前 `go@e0e9d9d45` 仍不包含 batch-1，按原裁定等其合入后再 rebase。C34 DNSLoop 也尚未合入 go。
- 这是依赖未解除时的可审阅 checkpoint，不是已完成/已验证的 A21。生产 Register 的 checker 仍为 TODO(C34)，准备/保存会拒绝绕过它；路由清单和模块状态保持 wip，不能直接合入或宣称成功闭环。
- 需要协调者在 batch-1、O3-6c、C34 合入后提供提交号；随后接线、做代表性实际 DNSLoop/owner 合并态验证，再正式交给新的 Opus HR 复核。


## batch-4 rebase 与共享入口衔接（2026-11-18）

- 已变基到本地 `go@fbbe382ec`（含协调者通知的 `c939a5f6`、batch-1 和 O3-6c）。原起点 batch-1 的 A20 提交已由合并树重写，因此只重放 A21 的三个提交，避免把旧 A20 再引入。behaviour-changes 文档冲突保留双方条目；未覆盖其他 owner 的实现。
- 变基后 A21 原提交依次为 `315c24784`、`c77a6624a`、`5b54fc8f0`，替代上文历史提交号。
- 自有生产调用链已核对：`Service.Execute` 的 detached Runtime ctx → `UpdateProfileTransaction` → typed `Repository.Update` → `transactLocked` → `commitStore(ctx, ...)` → `Store.CommitContext`，最终 COMMIT 前保留相同 generation 的取消检查。A21 无 taskstatus Record/Forget 或文件 replace 直写，不需要额外转换到 Context 入口，也没有新增 helper/屏蔽取消。
- `TestOnboardingRuntimeShutdownAndDisconnect` 增加 final COMMIT 屏障的两种真实 Runtime 场景；关机检查 SQLite Fingerprint、完整文档/revision 和业务文件均不变；请求断开仍提交。显式断言屏障恰好触发一次，避免假通过。
- DNSLoop 仍保留 TODO(C34)，等待 batch-5/C34 通知；不 cherry-pick。
- 最新衔接裁定：C36 先交自有部分，callback 未注册时 503；A21 最终负责 server 中 `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)` 与 DNS callbacks，按 C36 handoff 做，并补 server 级测试证明注册后不再 503。


本轮定向命令（.64，退出 0；无新 fuzz/产品逻辑改动，不重跑已完成的 fuzz）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/gateway/onboarding/service_test.go -- bash -lc 'cd server && gofmt -w internal/gateway/onboarding/service_test.go && gofmt -l internal/gateway/onboarding internal/api/gatewayonboarding && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 5m ./internal/gateway/onboarding ./internal/api/gatewayonboarding/... ./internal/server && go test -race -count=1 -timeout 3m -run "^TestO36cRepo(BeforeCommitRuntimeBoundary|LockWaitRuntimeBoundary|OwnerRuntimeMatrix)$" ./internal/repo'
```

`gofmt -l` 无输出，全仓 vet 通过；最后输出：

```text
ok configflow/internal/gateway/onboarding                 15.121s
ok configflow/internal/api/gatewayonboarding               3.978s
ok configflow/internal/api/gatewayonboarding/difftest       4.162s
ok configflow/internal/server                              1.512s
ok configflow/internal/repo                                2.662s
```

本轮未发现新增产品差异；原生 typed repo 兼容入口保留现有 A21 边界，不趁衔接扩展迁移。
