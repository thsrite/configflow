# A22 网关安装交接

分支 `go-port-a22`。按协调者通知，以 `git rebase --onto go-opus-review-a21 26a8a649c go-port-a22` 换到 A21 HR 复核基线 `663d271bb`，没有重复引入旧 A21 提交。实现检查点 `b057c148b`；本轮补充边界验证与复核规则接入。只使用合成数据；未 push、未开子 agent、未访问真实设备。状态 `ported`，最终定向 vet/race/fuzz 全通过，等待新的 Opus HR 复核。

## 实现范围

- `backend/agents/gateway_installation.py` 全流程：私有持久会话、创建/幂等重放/新 revision 复用、自动主机地址固定、限定登记、两份显式凭据原子绑定、网络确认、Mihomo→OxiDNS 顺序发布、固定任务 ID 恢复、显式失败重试、过期 capability、只读状态/最新安装。
- `backend/routes/gateway_installations.py` 七个方法及 GET 隐含 HEAD：管理员 JWT / installer capability 边界、16 KiB 声明与实际长度限制、空体 `{}`、忽略媒体类型、no-store、脚本文本/referrer-policy；所有路由已标 done。
- 新 owner 在 `internal/gateway/installation`；HTTP 只做翻译。复用 installer 的完整网关脚本、RootPrivileges、短链 SQLite；onboarding.AssertReady 注入目标设备地址；A14 PrepareContent/Push、C17 CommitProviderBundle；A20 ParseSpec/Bridge；A01 freshness 与 registry 事务。
- registry 新增可信 `RegisterInstallationAgent`：恢复凭据时在**同一 L3 事务**验证完整身份及 gateway_installation_id，避免读取旧身份后凭据被换绑的 TOCTOU。外来 capability 或无 marker 的独立 Agent 仍必须持有现有 token；已有 registry/RegisterAgent 路径继续复用同一实现。
- server 注册 A01 InstallationProfile/InstallationRegister 与 C36 ExternalOwners.Installation 的 Latest、HasInstallation、GatewayStatus。使用 Register 合并，保留其他 owner；错误保留 Cause、status/message，取消错误不伪装成设备失败。C36 自己继续持有唯一 LevelSetupSession 锁，A22 的只读 callbacks 不另锁 setup session。
- native JSON DTO；会话未知成员保留；落盘 0700 目录/0600 文件、temp→fsync→rename。jsonx 仅限 adapter/wire 连接尚未迁移的 repo/registry/convert API，不增加 Python 动态形状或异常仿真。无新依赖。

## 并发与 HR 清单

| 检查类 | A22 处理与证据 |
|---|---|
| L1 验证顺序 | publish 先校验 body，再 TryLock 会话 publication lease；busy 返回原状态，HTTP 仍沿 Python 固定 202。lease 贯穿 worker，失败/Spawn 拒绝均释放；不创建 flock。TestPublishLeaseAndRevisionGuard |
| L2 / L3 顺序 | create 锁用 LevelSetupSession 前序，安装 session 用 LevelDevice；setup owner 的 LevelSetupSession→device→repo 顺序不变。发布 worker 不跨设备上传持有 session 锁；双凭据一次 repo 事务，先全部证明再更新，零半绑定；lockx.Violations==0 |
| 断连与关机 | 写 owner Detach、入口/取锁后/文件 replace/设备与事务前检查 ctx。Spawn 受真实 Runtime 管理；上传前关机后 DB fingerprint、会话原字节不再变、零上传；同一位置浏览器断开仍完成双核；等待 session 锁后取消不写 |
| 写入身份与不确定结果（R04/R05） | 每次服务前、检测后、准备后重查 profile revision、host/name/port/service/shell/profile 与 task ID。BeginDeployment/Push/activate/get 均复用 A01/A14；uncertain 上传查询同 ID，首核失败不开始后核，后核重试保留首核成功 ID |
| shared client / timeout / raw bytes（R06） | 不更改共享 transport。真实 HTTP 接收器验证 GET 无 body、Bearer、manifest 身份与 X-Activate；不覆盖 timeout；status 保留 A20 的 connect/read、禁代理/重定向与响应上限。归档由 agentfake 自验大小/digest，配置文件与真实 Python 逐文件语义比较 |
| 错误类型（R05 / A21 F1） | Installation.Error 保留 Cause/HTTP status/code；AssertReady 的用户 YAML/OxiDNS 错误映射 400，基础设施/取消不冒充400；故障不吞成成功，remedy 只在本轮准备错误有时保留 |
| 凭据（A17 / A21 F4） | 系统密钥（含所有 Agent token、退役/规则/订阅/注册密钥）整份脱敏；从完整文档收集的用户凭据只用于 message/error/rollback_error 等诊断。先收集再投影，跨 Agent 的私有密码及编码引用不落日志/会话诊断/响应；短密码 linux 不破坏 name/network/状态键。本次管理员安装命令只保留自己的 capability |
| 无效证明 / 设备 ID 复用（R05） | ID/token/服务证明必须非空；existing marker 与完整身份在事务内验证；两份 proof 及 busy guard 全通过才换绑。没有设备 undo/补偿写；该清单类由已有 A14/A20 owner 负责 |
| 非法容器 / YAML 图（R04/A19） | 新代码没有任意 map 值可比较断言；native JSON 拒绝错类型；YAML 走已有 yamlx/onboarding/convert，不复制 alias 遍历器。session 有限长读取、typed corruption fail closed |
| 索引与批量（R05） | 双凭据绑定对 registry 建 ID 与 name/host 索引，不逐项 JSON round trip 扫整个表；无新的设备批量写协议 |

## Python 与 Go 测试映射

按 Leaner verification 合并行为族，没有把 54 个 Python 函数及其参数排列机械复制。

| Python 行为族 | Go 证据 |
|---|---|
| test_gateway_installation.py：创建、私有权限、精确 profile、重复请求、新 revision 与 target/takeover | TestCreationPrivacyReuseAndValidation、TestReprepareReusesInstalledPairAndKeepsScript、TestFullTUNTakeoverNeedsDistinctCapability；FlaskHTTP 对照幂等创建/完整文档/原会话，满短链503 |
| 注册密钥绕过仅限本能力、已有凭据恢复、丢 session receipt、外来能力、两证明原子性、自动地址 | TestRegistrationCapabilitiesAndAtomicEnrollment、TestAutoHostPinSurvivesMissingReceipt；真实 Flask /api/agents/register 与 enroll，完整文档及 session 对照 |
| 网络确认、地址/网卡/唯一路由一致性、不隐式 apply | TestNetworkUsesRealOwnerAndNeverApplies；FlaskHTTP 正常 auto_network、未绑定冲突，真实 A20 transport/raw GET；不改 profile/revision |
| 按序发布、首核失败、后核重试、响应丢失、固定 ID、lease、配置/目标变化 | TestRealOrderedPublicationFailureAndResume、TestPublishLeaseAndRevisionGuard；TestPythonPublicationStateAndGeneratedArtifacts 比较正常/首核失败/丢上传响应；FlaskHTTP 真实 publish202→等待双方空闲→published 与陈旧 revision409 |
| converter/bundle/receipt | PythonPublication 比较 TAR 的文件集合、manifest身份/顺序/role、X-Activate/Bearer 与每个 YAML 语义；FlaskHTTP 发布前全量文档/会话对照，发布后比较 profile/shared、双身份、config_revision、latest ID/status 与 applied ID。A14 的 opaque fingerprint/事件历史仍由 A14 原有 corpus 负责，不在 A22 再实现 normalizer |
| expired/script/binary/invalid请求/auth | private Script 与短链冻结内容相同；FlaskHTTP 匿名401、错误capability401、坏JSON/非对象/错类型/缺字段400、超限413、未知会话404、短链满503、HEAD无写、no-store/referrer-policy。脚本生成与真实安装执行继续由 A08–A10 测试负责 |
| 断连/关机/重启 freshness | TestRuntimeWriteOwnersShutdownAndDisconnect、TestSessionWaitRechecksShutdown、TestPublicationWorkerKeepsRuntimeAfterDisconnectAndShutdown；server 测试重置 A01 Runtime 后只信 freshness，不信持久 online；冻结 script 与 abandon dry-run 用坏 legacy DB 负例证明不初始化 registry |
| remedy、原子文件、未知字段、脱敏、输入故障 | publish 保留 PreparationError.Remedy、失败任务镜像入session；TestRecordUnknownFieldsAndMalformedOwnership、FuzzInstallationRecord、TestCrossAgentCredentialsRemovedBeforeSessionProjection、TestReviewedA21ValidationClassAndTwoLevelRedaction |
| test_router_split_regression.py | 三个基础配置/普通单服务安装兼容族归 A19/A21/A08 原 owner；A22 复用它们。完整 TUN 会话/脚本/接管变化由 TestFullTUNTakeoverNeedsDistinctCapability 固定；RouterOS 端到端由 routers/L4/L5 负责 |
| test_setup_flow.py 的 A22 外部 owner | TestSetupInstallationServerRegistrationUsesRealOwner：缺 owner503 负例→NewApp真注册→HasInstallation阻止误删→真实 capability登记/绑定→已发布证据→A20 GatewayStatus→C36 check200 |

## 差异与后续实验

- 没有丢弃 A22 功能。唯一 registry 增强是将 Python 的 capability marker 凭据恢复证明收紧到同一事务，防止并发换绑泄露；正常路径与 Python 相同。
- 沿用 O3 已接受的进程内锁/无 per-session flock、关机取消时保留 publishing 固定 ID 检查点；不在 shutdown 后写 publish_failed，不自动增加恢复操作。
- 损坏/错类型私有 session 统一409，读取有2 MiB上限；latest跳过无法安全解析的记录，但 HasInstallation 对仍能读到 profile_id 的记录保守计占用。Python 对部分损坏数据会抛500。未知正常成员继续原样保留。
- 矩阵 R096 F03 描述与 Python 实现不符：script 只验证本 session capability/expiry，保存配置变化不作废冻结安装脚本；新 revision 需要重新 prepare 才能 publish。按实际 Python 保持，无需重装设备。
- Python HTTP/worker oracle 的唯一外部 owner 替身是尚未接入的 A23 IPv6 enrollment no-op，与 Go 未注册状态一致；生成、bundle、发布、轮询、登记、文件与 repo 都是真 owner。所有 HTTP 均只发往独立 loopback 接收器。
- 后续新的 Opus 做 HR 复核。真实 Linux 安装、systemd/OpenRC、自检、关机/断电、IPv4持久转发、TUN/nft及LAN客户端行为属于 L4；没有在本轮操作实验或生产设备。完整 gate/proddiff 由协调者合并批次执行。

## 定向命令与结果

固定协调者指定的测试主机，通过 `RT_HOST` 环境传入；没有在代码/夹具/报告内写入真实设备地址或凭据。

最终通过的命令（测试主机固定为指定的第一台服务器，由 RT_HOST 传入）：

```sh
rt -- bash -lc 'cd server && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p 2 -count=1 -timeout=5m ./internal/gateway/installation/... ./internal/server'
rt -- bash -lc 'cd server && gofmt -l internal/gateway/installation internal/agents/manager.go internal/agents/messages.go internal/agents/upgrade_support.go internal/api/agents/agents.go internal/api/agents/gateway_installation.go internal/api/agents/messages.go internal/server/server.go internal/server/setup.go internal/server/setup_installation_test.go internal/server/setup_installation_reads_test.go && go test -race -p 2 -count=1 -timeout=5m -run "Regist|OneBusyAgent|ConstantTimeASCII" ./internal/agents ./internal/api/agents && CONFIGFLOW_DIFF=1 go test -race -p 2 -count=1 -timeout=3m -run "TestOnboardingRuntimeShutdownAndDisconnect|TestAssertReadyClassifiesValidationErrors|TestShortCredentialDoesNotCorruptWizardFields" ./internal/gateway/onboarding && go test -run "^$" -fuzz "^FuzzInstallationRecord$" -fuzztime=60s -parallel=2 ./internal/gateway/installation'
```

第一条实际执行还在检查前执行 gofmt -w，并用 rt --back 取回逐个归属文件；上面列出验证部分。gofmt -l 无输出、go vet ./... 无输出，退出码均0。尾部：

```text
ok configflow/internal/gateway/installation          111.937s
ok configflow/internal/gateway/installation/difftest 18.583s
ok configflow/internal/server                       18.194s
ok configflow/internal/agents                       75.609s
ok configflow/internal/api/agents                   40.198s
ok configflow/internal/gateway/onboarding            5.696s
fuzz: elapsed: 1m0s, execs: 589621, new interesting: 278 (total: 280)
PASS
ok configflow/internal/gateway/installation          61.021s
```

早期较宽的全 agents 包 race 达到10分钟总时限（当时停在既有 TestConfirmedAgentStageDiscardsOldUnknownTransportDetails，单项运行11秒），该轮不计全包通过，改为上述与修改直接相关的登记路径。另修复了 HTTP 差分测试把 Stop 当作等待：race 下宽限期会正常取消未完成发布；现在通过真实状态接口等待双方完成后才 Stop。产品关机宽限期没有改变。未运行完整 gate/proddiff。

## 新增文件行数

| 文件 | 行数 |
|---|---:|
| installation/adapter.go、messages.go、network.go | 384 / 85 / 206 |
| installation/publish.go、record.go、register.go | 363 / 124 / 199 |
| installation/service.go、store.go、wire.go | 450 / 163 / 63 |
| installation/service_test.go、python_test.go | 860 / 172 |
| installation/difftest/http_test.go | 430 |
| installation/testdata/flask_reference.py、publication.py | 68 / 40 |
| api/agents/gateway_installation.go | 124 |
| server/setup_installation_test.go、setup_installation_reads_test.go | 165 / 56 |

另薄改 registry/API/server 注册以及 parity 清单；无新依赖、无临时调试文件。
