# R17 客户端固定 IP 与客户端设备 HTTP 交接

状态：**ported，待新的 Opus HR 独立复核**。无未移植功能；未 push。分支 `go-port-r17`，临时基底 `go-r17-base-v2@673851659`，包含 R14 与复核后的 R16。已执行 `git rebase --onto go-r17-base-v2 go-r17-base go-port-r17`，旧 R16 未重复带入。按协调者通知才换到 go；当前未收到该通知。

已有提交：`8f05e4f0e`（typed DHCP drivers）、`07644e197`（service/HTTP）。后续提交包含边界对照、Runtime/事务回归和本交接；最终提交号见交回消息与分支 log。

## 产品实现与依赖

- `routers/fixed_ip_{model,read,ros_write,ikuai_write,messages}.go`：typed binding/operation/snapshot/state；RouterOS DHCP 全表、最具体可用 scope、动态转静态、普通新增；iKuai 严格 static_ 视图、完整 form、DNS/hostname 为空以继承；归属删除、逆序补偿、删除恢复。随机 marker 使用 crypto/rand，RouterOS 注释追加 32hex，iKuai tag 为 cfip_ + 10hex。不存在、重复 ID、空 scope、外部字段改动不能被当成归属证明。
- `clients/fixed*.go`：精确请求键/类型、1..64、IP/MAC/同路由器 MAC 去重，发现来源再验证；签名预览、执行时重建所有证据；跨路由器批次、metadata CAS、错误反向补偿；删除恢复后按条件保存新 ID。未知 profile 字段保留。唯一 legacy jsonx 事务接缝集中在 `fixed_repo.go`；业务与 token 均 native JSON。
- `api/clientdevices`：正式注册 GET/PUT `/api/client-devices`、GET discovery、POST fixed-ip preview/apply，共五条。auth、no-store、精确 profile/Agent pinning、HEAD 只读；A01 manager、A13 `deploy.PublicDeployment`、C36 真实 `setupflow.ProfileSessions`、C06 `auth.Current().SecretKey`。正式 server wiring 与 routes.tsv 已更新。
- R16 discovery/cache/service 原接口复用；app-owned cache，固定 IP 写前/写后 invalidation，EnrichFixedIP 使用同一 reader。坏发现行不隐藏健康设备，无法证明完整写入证据时只将固定 IP 控件禁用。公开最终结果统一使用共享 redact 与全仓 token hook；原始存储/私有比较证据保留。
- `routerdoc.ValidateBindings` 已在 R01 实现且 `repo/validation.go` 已调用，本任务没有重新写 validator。
- 不新增依赖、不启动 goroutine 或周期任务；不改前端入口、安装/操作说明。原 Python 页面操作入口与存储合同保持。

## 锁与生命周期

复用 R14 `routers.JobLock` 的同物理连接 L1 和 R06 `routeLocks` L2，不另建 flock/锁。`RouterOperationLocks` 先 try 整组；遇忙释放所有已取 L2，再等待单个空出的锁并重试整组。全体 L1 先取，连接克隆去重，避免同级持锁阻塞。typed context 保留锁检查链。该 helper 未改 R06 的单设备入口。

R12 的 device transport/Driver、稳定字段、错误/拒绝分类、读回证明被复用；固定 IP 是 Python 独立的 DHCP 事务，不调用路由发布计划以修改无关路由或 publication。所有写 owner Detach 浏览器断连，仍响应 Runtime 关机；每个本地事务入口及真实提交点检查 ctx。

R17 iKuai 使用共用私有 `writeForm`（从原 Write 抽出编码/错误分类），传递已 Detach 的 Runtime context。旧公共 `Write` 仍按原合同 WithoutCancel；没有悄悄修改其他 HR 模块的单操作语义。原始请求与默认生产 10 秒超时通过实测，fake 没有改参数。

此基底尚无 O3-6b 的统一 RouterJobLock，仍调用 R14 临时唯一入口；协调者集成时应统一替换，不保留两套锁。未增加不可中断提交，未扩展仅 R12 获批的 D-R12-3。关机若发生在设备已写之后，停止后续补偿和 metadata 提交，可能保留带唯一 marker 的设备租约；下次发现/预览人工确认，不能伪装成成功。

## Python / Flask 边界证据与归一化

`routers/r17_boundary_test.go` 驱动真实 Python RouterOS/iKuai 模块，双方使用仓库 routerfake 和真实 HTTP。15 组：RouterOS 8、iKuai 7，正常新增/转换、MAC 冲突、组播 MAC、无效 scope、歧义、显式拒绝、丢失新增回包。比较 preview、公开 binding 和最终完整设备表，普通中文消息严格比较；内部传输异常只比较失败/效果，不仿造 requests 异常文本。

`api/clientdevices/boundary_test.go` + `testdata/r17_http_reference.py` 使用真实 Flask blueprint/repository/vendor driver 对生产 Go HTTP handlers（无 business stub）。15 次请求包含列表、发现、HEAD、改名、旧行冲突、添加/删除预览与执行、坏 token、找不到 Agent、非对象 PUT、缺字段 preview；比较状态、JSON、no-store、每步完整 profile/revision、最终设备表。

只有以下精确归一化：成功 preview root 的 token 单独验证签名/有效性并要求存在；随机 cf-client:<name>:32hex 注释及其派生 hostname 的 nonce；先验证 RFC3339 的 root updated_at 与 discovery checked_at。其余字段/类型/数组顺序保持严格，created_at 不忽略。`clients/fixed_test.go` 用真实 itsdangerous 双向解码 Go/Python token，涵盖无压缩/压缩、中文、未来时间、600 秒可用、601 秒拒绝。

Python 测试映射（按行为类别，不声称逐个函数回放）：

| Python 文件 | Go 边界与附加回归 |
|---|---|
| `test_client_fixed_ip.py`（17） | Flask oracle 与 HTTP guards；token 双向互验/过期；真实 Runtime 写前、I/O 中、写后断连与关机；批次第二步失败/metadata 并发编辑逆向补偿；删除恢复新 ID；克隆连接去重、多设备混合厂商锁；1..64/重复 MAC；真实 discovery source。原有 clone/export/generation 不改，沿用 repo/生成器已验证合同。 |
| `test_routeros_fixed_ip.py`（23） | 8 组真实 Python vendor 对照，新增/转换→删除→恢复→撤销完整生命周期，raw Basic/bodyless GET/DELETE/make-static bytes，完整稳定字段/丢回包补偿/空 scope 拒绝。 |
| `test_ikuai_fixed_ip.py`（14） | 7 组真实 Python vendor 对照，完整生命周期，static_ raw form bytes、生产超时、运行中 Runtime 取消真实 add HTTP。 |
| `test_client_devices.py`（11，HTTP 半部） | 15 请求 Flask HTTP oracle、鉴权/无副作用 HEAD、Agent profile pin、UDP protocol/tag collision 的真实 GET/PUT 保留旁路；业务半部复用 R16 clients 已有测试。 |

## HR 问题类别逐项自查

- R04/R06 原始 HTTP/超时：两厂商真实 wire 检查，GET/DELETE 零字节、JSON header/Basic、make-static/static_ 的精确 bytes；transport wrapper 只观察生产 10 秒参数。
- R05 共享 client 隐式行为：没有复制 transport、login/session/redirect/TLS/proxy 逻辑；只抽出原 Write form 层，旧入口语义不变，运行原 iKuai/R05/R06/R12 回归。
- R05 错误与 uncertain write：重用 RouterError/KindRejected；拒绝不补偿，不确定写入先读回并仅清理唯一 tag/新 ID 的本次对象，完整字段不同则保留并报告。中文集中在 messages 文件；HTTP 按 Settings400/Conflict409/Router502 映射。
- R05 缺失不得匹配/ID reuse/undo：完整 expected 和非空 scope；删除/补偿前读回 ID、MAC、IP、scope、comment 与全部稳定字段。恢复先排除 ID 被重用及 IP/MAC/tag 冲突，不覆盖外部对象。
- R05 表处理：快照逐表读取；没有对每项重新 JSON 编解码整表。批次只对选中设备串行做独立安全校验，没有并发共享 driver。
- A19 图/边界/空意图：本模块只处理有界 JSON；DNS 策略编辑直接用 C26/R16，未复制 YAML 遍历或协议引用扫描；设备失败不变成空成功集合。
- R12 预览时效/共有对象：执行时重读 setup、完整 router record、发现来源、私有 DHCP before-image；持锁重建 digest；事务还检查 saved device/binding/router CAS。普通错误逆序补偿；外部编辑保留。未修改 IPv6 manifest 或 Agent 共有回流对象。
- R13/X1 与 R16：路由器仅用统一 R5 FromConfig 解码；健康发现行留存；最终投影跨设备凭据脱敏，未知 type 不接设备。没有单独容错 decoder。
- A24/C32/R12 关机：真实 Runtime 测试，不拿普通 cancelled context 充数；设备 I/O 能被 Runtime 关机取消，browser disconnect 仍完成；每个 DB commit point 检查 ctx；关机后无补偿 I/O/metadata 提交。
- token 可解码信息：签名不是加密，改用 native 完整 operation 摘要，私有 client-id 不进 token；专门回归改变私有字段后仍拒绝陈旧 token，正常转换保留原值。

## 行为差异与剩余事项

差异已登记 `behaviour-changes.md`：D2 native 摘要/旧 preview 重做；token 解压有界且不含 before-image；空 scope 不能证明归属；HEAD Agent pin 修复；公开 credential redaction；Runtime 停止规则。未发现需要协调者裁定的开放阻塞；无功能删减。

fresh Opus 重点：共享 Write 拆分的取消边界、跨设备锁/克隆连接、丢回包只清本次对象、恢复 ID metadata CAS、native 摘要与私有证据、不扩展 R12 不可中断例外。实机 RouterOS 动态转静态及 iKuai static_ 固件字段/会话行为、进程强杀后 orphan marker 恢复属于 L5。完整 gate/proddiff 与独立 Step B 未覆盖项由协调者批次处理；没有访问生产/真实家中设备。R13 已移交的间歇测试未处理。

## 验证命令与输出

所有命令使用会话指定的固定测试服务器，`R17_TEST_HOST` 为该 SSH 目标（本交接不写基础设施地址）。仅合成数据与 loopback fake。本机只编辑/git/轻量读取。

1. 格式、全包 vet，clients/HTTP/routerdoc/server 定向 race，相关 routers 回归及新 fuzz：

```sh
RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/clients ./internal/api/clientdevices ./internal/routerdoc ./internal/server -timeout 8m && go test -race -count=1 ./internal/routers -run "TestR17|TestClient|TestR06|TestR12Runtime|TestR14Runtime|TestR14JobAndScheduler|TestOwnerLifetime" -timeout 8m && go test ./internal/clients -run "^$" -fuzz "^FuzzR17FixedRequest$" -fuzztime=60s -parallel=2'
```

末行：clients `31.679s`，clientdevices `11.395s`，routerdoc 无测试，server `5.362s`；routers `41.541s`；fuzz `599296` execs、`PASS`、clients `61.015s`。格式/vet 无输出且退出 0。

2. 增加私有证据摘要后重跑格式/vet 与其调用者：clients `31.165s`，clientdevices `10.203s`，server `5.294s`，全部 race 通过。

3. iKuai shutdown 写请求与共享 Write 拆分后的定向验证：

```sh
RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/routers -run "TestR17|TestIkuai|TestR05|TestR06|TestR12" -timeout 8m'
```

末行 `ok configflow/internal/routers 180.862s`。包含真实 Runtime 阻塞 iKuai add HTTP 的 disconnect/shutdown 对照，未缩短生产超时。

4. 最终共享路径修改后的格式/vet 与所有直接 HTTP/service 调用者复验：

```sh
RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/clients ./internal/api/clientdevices ./internal/server -timeout 8m'
```

格式/vet 无输出；末行 clients `32.056s`、clientdevices `10.073s`、server `5.316s`，退出 0。新 fuzz 的输入规范化未被后续摘要/transport 修改，再跑同一 fuzz 无新增验证价值。

核心产品非测试行数（含空行/注释）：routers fixed_ip*.go 1391、clients fixed*.go（不含测试）846、HTTP handlers/messages 429，共 2666；另有 shared ikuai form 抽取、fake make-static 和 server 注册的小改动。未为了行数压缩逻辑。

格式统一在远端 gofmt 后用 --back 逐一取回本任务文件；未取回其他包目录。未跑 full gate、全仓 race 或 proddiff。
