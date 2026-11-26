# A21 gateway onboarding 交接

分支 `go-port-a21`；已 rebase 到 `go@c5f5ff349`（batch-5，含 batch-1、O3-6c、C34 与 C36）。自有实现与最终接线已完成，本轮定向检查通过，等待新的 Opus HR 复核。未 push、未开子 agent、未访问生产或实验设备。所有夹具均为合成，所有测试只在 rt 测试服务器运行。

## 已完成的边界

- 原生 request/state/report/publish guard，配置文档未知段用 `json.RawMessage` 保留；已知资源通过 model 读取。`jsonx` 只在尚未迁移的 repo/convert/routersplit adapter 边界。
- 复用 A19 editor/selection/overlap、A20 setupflow reader、routersplit marker/full TUN、C18 generator、OxiDNS normalize/common/validation/reference。不复制业务 parser、安装状态或 setup ownership。
- 候选仅替换 Mihomo/OxiDNS 两段；原生配置保留/显式转换备份、fallback 恢复、API 认证清理、query recorder、策略可达性、DIRECT overlap、动态/外部引用拒绝均有 Python 边界对照。保存前对同一锁内快照生成并验证两个服务，revision 与 marker 冲突零写入。
- owner 使用 Detach，保留 Runtime generation，在入口、事务前、回调开始/结束及 adoption 前检查 ctx。repo 继续使用 O3-6c 的 CommitContext 最终 COMMIT 闸门，没有 Uninterruptible 扩张。
- 三条 HTTP 方法及隐含 HEAD 已注册；鉴权、no-store、实际/声明 256 KiB body 上限、BindSilent。按 Python 的 raw json.loads 契约忽略媒体类型。HEAD 只读、畸形/非对象/非有限 JSON 不写入。
- 从同一配置快照收集当前及退役 rule token、subscription token、Agent token、fetch proxy；统一 credentials/redact 处理环境注册密钥与编码引用。Report 不返回 candidate 或私有备份。

## Clarified 与最终衔接

- C34 已合入：`onboarding.CheckDNSLoop` 仅薄接 `gateway.MihomoDNSLoop`，没有第二份 DNS parser。生产 HTTP、默认 Prepare/AssertReady 和 C36 DNS callback 使用同一 checker；代码中已无 TODO(C34)。公开低层 PrepareMihomoDNS 的 nil checker 仍 fail-closed。
- HTTP/C36 的准备没有目标主机地址，沿用 Python 的空 local_addresses；A22 安装时通过 Options.LocalAddresses 注入真实目标地址。不能拿面板主机地址替代目标设备。
- server `wireSetupOnboarding` 注册 `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)`，以及 `setup.RegisterExternalOwners` 的 DNSOwner。LocalUpstreams、PrepareMihomoDNS、默认 DNS/目标/Nameservers 均复用 A21 公共 leaf/常量。Register 合并非 nil owner，保留 C32 ReadRouteContent；不使用 Set 覆盖全部 owner。
- adoption 的 before/after 是完整存储 profile，不是 hydrated/shared snapshot。repo L3 已释放，且保存 revision 严格等于 before+1、ctx 检查通过才调用。C36 自己 Detach，使用唯一 `LevelSetupSession=19` 操作锁，在拿锁后和 rename 前检查同一 generation。A21 不另造 session 锁；顺序 setup-session→device/profile operation→repo。
- 对照确认：Python 对相同表单重复保存仍 revision+1，仍调用 adoption。真正的反例是 before 快照后出现一次 metadata 写入，保存完成时相差 2；此时双方均跳过 adoption，session 字节/证明不变。测试使用现有的 takeover/session-reader seam 插入真实 repo 写入，不增加产品 hook。
- 生产注册后 C36 prepare 与嵌入 onboarding save 不再返回缺 owner 的 503。独立 package 未注册 owner 的 fail-closed 防线保留；它不再是生产差分豁免。C36 自有 reader、digest、锁和 rename 只调用已合入 owner。
- 无 mutation gate；Python 只比较正常边界及每类错误代表，不做形状×引擎穷举。完整 gate 由协调者批次执行。

## 关键边界测试

| 风险/行为 | 测试与证据 |
|---|---|
| 真实 server 注册 | server.TestSetupServerOwnerRegistrationRemovesUnavailable：未注册 DNS 的 prepare 503/零 DB 提交；NewApp 真接线后 prepare 200、补 nameserver、真实 adoption 更新两种 proof；重复保存仍 +1/再 adoption，没有 no-op 假实现 |
| revision+1 分支 | api/gatewayonboarding/difftest.TestFlaskAdoptionRevisionCondition：观察真实 Flask route 对真实 C36 owner 的调用；普通/重复保存各一次，before 后真实 metadata 写入造成 +2 时零调用。Go 对应 +2 session 原字节不变 |
| ownership 身份 | server 对实际保存的 Go profile 调真实 Python owned_digest(base=True/False)，与 C36 session 两个 proof 一致；不用结构化 YAML 替换参与 fingerprint 的存储文本 |
| DNS 解析/安装闸门 | TestRealDNSLoopPreparation：正常远端、loopback、指定 IPv6 目标主机的 fallback，与 Python _prepare_mihomo_dns 比较拒绝及中文消息；AssertReady 默认实际 checker，指定目标 IPv4 回环拒绝且不改文档 |
| HTTP DNS 回环 | TestProductionCheckerRejectsLoopWithoutWrites：真实 Register；preview 200 ready=false/blockers，save400，完整文档/revision/业务文件不变 |
| Runtime | TestOnboardingRuntimeShutdownAndDisconnect：关机前、生成期间、共享最终 COMMIT 前关机均不落盘；请求入口/最终提交点断开均完整提交。Fingerprint、文档/revision、业务文件检查；最终屏障恰好触发一次 |
| Flask 边界 | TestFlaskHTTP：三方法正常/错误，响应、Chinese messages、完整 export、物理 SQLite 字段/位置和业务文件；testServer 使用真实 C34 checker/C36 hook，不再注入 noLoop |
| HEAD | TestHEADIsReadOnlyAndMatchesGET、真实 Flask TestFlaskHEADExpectedReadOnlyDifference：显式记录 Python HEAD 写入 bug，Go DB/文件/revision 不变 |
| 合成配置与生成 | TestPythonPreparation：template/native/转换备份、双服务生成结构、state/ready、full TUN、策略/选择/认证/recorder/fallback 等行为族；不替代真正设备操作 |
| YAML/JSON/泄密 | 40 层 alias、自引用、重复键；form 类型/非有限 body；显式 null 开关不当作有效证明，retired token、环境注册密钥与编码引用脱敏；FuzzGatewayOnboarding |

Python 指纹作用于每一方各自保存的原始 YAML 文本；两端允许格式不同，不能直接把不同文本的 SHA 当成语义差异。server 指纹测试把同一个实际 Go 存储 profile 原样交 Python，严格比对两个 digest。

## 差异、发现和后续边界

- 已裁定差异：HEAD 只读修复；标准 JSON 拒绝 NaN/Infinity。已覆盖的正常 HTTP、候选/双服务生成、存储及 adoption 条件无未解释差异。
- 本轮无新增产品 high/medium finding。测试初稿把重复保存误当成无 revision 增量，以及把带 blockers 的 preview 误断言为400；读真实 repo/route 后修正测试，未据此修改业务语义。
- A22 发布/设备安装、真实网络 lab 行为由其 owner 验证；C36 Installation/VerifyRouter 的其他缺省503仍归对应外部 owner，不在 A21 的 DNS/adopt 注册范围。
- 仍需新的 Opus HR 复核和协调者批次完整 gate；作者不标 verified。
- N8-5 后续桥接清理：onboarding/adapter.go 的 legacy generator/router-split 桥，service.go 的 GetPublishSnapshot/GetProfile/UpdateProfileTransaction callback；不要重写原生 request/report 和 DNS 图算法。

## 变基与提交

从 batch-1 开工，先 rebase batch-4，再 rebase batch-5；只保留 A21 的实现，不重复引入旧 A20 历史。最终变基冲突保留 C34 路由/文档、A20 路由映射和已复核 OxiDNS 的 URI scheme 引用排除逻辑，同时添加 A21 的 AdditionalFields 遍历。

当前基础提交：`e858cb2e9`（共享 OxiDNS upstream/reference leaf）、`d7dca4f07`（A21 主实现）、`3b1e23e43`（此前 checkpoint）、`8f5af0070`（O3-6c 合并态回归）。最终 C34/C36 接线提交另见本分支随后提交。

## 定向检查

本轮固定 `.32`，无完整 gate/proddiff。最终命令退出0：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/gatewayonboarding/routes_test.go -- bash -lc 'cd server && gofmt -w internal/api/gatewayonboarding/routes_test.go && gofmt -l internal/ tools/ cmd/ && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 5m ./internal/gateway/onboarding ./internal/api/gatewayonboarding/... ./internal/setup ./internal/api/setup ./internal/server && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout 3m -run "DNSLoop|Reference" ./internal/gateway ./internal/oxidns && go test -run "^$" -fuzz "^FuzzGatewayOnboarding$" -fuzztime 60s -parallel 2 ./internal/gateway/onboarding'
```

gofmt -l 无输出，go vet ./... 通过。此前同一轮已用 rt gofmt -w 格式化全部改动 Go 文件并 --back 取回；没有本机运行 Go 工具。最终输出：

```text
ok configflow/internal/gateway/onboarding                  28.086s
ok configflow/internal/api/gatewayonboarding                7.658s
ok configflow/internal/api/gatewayonboarding/difftest        9.124s
ok configflow/internal/setup                              53.701s
?  configflow/internal/api/setup                          [no test files]
ok configflow/internal/server                             8.771s
ok configflow/internal/gateway                             1.216s
ok configflow/internal/oxidns                              3.027s
fuzz: elapsed: 1m1s, execs: 95537, new interesting: 169 (total: 172)
PASS
ok configflow/internal/gateway/onboarding                 61.019s
```

Gateway/OxiDNS 行仅为 DNSLoop/Reference 定向回归，不冒称重跑两个完整引擎。较早 `.64` 上13个改动包/直接调用方 race、相关 difftest 与60秒 fuzz 已通过，但不把那批历史结果当作 batch-5 新树的通过证明。
