# A22 HR 复核（网关安装）

复核者：新的 Opus（非作者），2026-11-18。分支 `go-opus-review-a22`，起点 `go-port-a22@5f7d7d03`
（作者 Codex，叠在 A21 复核 `go-opus-review-a21@663d271b` 上）。无子 agent、无 push、未访问生产/路由器/PVE；
测试只经 `rt` 固定 `RT_HOST=sunbroke@192.168.0.32` 跑，数据全部合成。

**结论：high 0 条，medium 2 条，low 2 条；全部已修复并带测试，0 未决。未达到「≥2 条 high」回退门槛。**
A22 状态改为 `verified`（待协调者批次 gate）。

## 方法

- Python：`backend/agents/gateway_installation.py`、`backend/routes/gateway_installations.py`、
  `routes/agents.register_agent` 的安装分支、`manager.register_agent/begin_deployment/_get_deployment/update_agent`、
  `setup_completion.check_installation/_check_full_tun`、`setup_abandon`、`linux_gateway._spec`、
  `test_gateway_installation.py` 的 54 个用例名与关键用例正文。
- Go：`internal/gateway/installation` 全部产品文件、`agents.registerAgent/RegisterInstallationAgent`、
  `api/agents` 注册路由与七个安装路由、`server/setup.go` 的 C36 接线、`setup` 的 Installation 消费方、
  `lockx` 级别、`lifecycle.Spawn`、`installer.Store.SaveInstallScript`、`deploy.IsPreparationFailure`。
- 抽查作者证据（不重跑作者全套）：外来 capability、两证明原子性、自动地址固定、Runtime 关机/断连、
  顺序发布/固定 ID/重试、C36 server 注册。每条发现先写测试，确认修复前失败。

## 发现

### F1，medium，已修复：一条类型异常的 Agent 记录让所有安装登记和状态读取 500

- Go 用 typed struct 解码 Agent 记录（`Port int`、`Latest task{Message string, HTTPStatus int …}`）。
  `PUT /api/agents/<id>`（Python 与 Go 都不校验类型）可以把 `port` 存成文本；旧记录也可能有。
  `Register` 解码**全部** Agent 做 name/host/marker 查找，任一无关记录的类型不符就让每次安装登记 500；
  已绑定 Agent 的类型不符让 GET/latest/publish/C36 check 500。
- Python 只做 `dict.get` 相等比较：无关记录不影响；绑定记录被视为 `target_changed`，发布 409「设备身份…已改变」。
  另外注册数据 `service_type` 非字符串时 Python 是 403「安装凭据只能注册…」，Go 因整体解码失败给 409。
- 修复：`agentFields` 按成员取值，类型不符即「不匹配值」（端口接受 Python 视为相等的整数值浮点）；
  注册数据同样处理。测试 `TestWronglyTypedAgentMembersFollowPythonEquality`（修复前：
  `json: cannot unmarshal string into Go struct field agent.port`）。

### F4，medium，已修复：准备阶段的 Python ValueError 在会话里变成通用「检查设备」文案

- Python `run_publication` 对 `InstallationError` 和**所有** `ValueError` 写 `str(exc)`；策略引用、基础设置、
  远程规则内容校验（`RemoteRuleValidationError`）、配置空间校验都是 ValueError，向导直接显示要修什么。
- Go 只认 `*Error` 和 `*deploy.PreparationError`，其余显示「发布暂未完成，请检查设备状态后继续…」，误导用户去查设备。
- 修复：`failureMessage` 复用 A14 的共享分类 `deploy.IsPreparationFailure`，不另写一份。
  测试 `TestPublicationFailureNamesPythonValueErrors`（含对照：该错误不是 `*Error`/`PreparationError`）。

### F2，low，已修复：enroll 的「目标名称和地址已被另一台 Agent 使用」漏判

- Python 用 `any(other …)`；Go 用 name/host → id 的 last-wins 索引。管理员编辑可造成同名同址的两条记录，
  当重复记录排在被证明的 Agent 之前时 Go 放行绑定。
- 修复：索引保留全部持有者。测试 `TestEnrollRefusesEarlierDuplicateTarget`（修复前放行），并确认拒绝时注册表与会话零写入。

### F3，low，已修复：C36 接线对 Agent 原始 JSON 做 typed 解码

- `Latest` 把公开视图解到 `setup.InstallationState`（`Enabled *bool`），`GatewayStatus` 把 A20 状态回复解到
  `setup.GatewayStatus`（`Current string`、`Error string` …）。文本 `enabled`、对象 `managed.error`、数字
  `ipv4_forwarding.current` 都让 setup check 500。Python 用 `is False`、真值、`is True`、`== '1'`，给对应 409。
- 修复：`installationState`/`gatewayStatus` 按 check 自己的判定读取；不可读的 `managed`（真值非对象）按 A20
  `HasRecord` 视为仍有转发记录（Python 在此 AttributeError 500，已登记 behaviour-changes）。
  测试 `TestInstallationStateReadsRawAgentMembersLikePython`、`TestGatewayStatusReadsAgentReplyLikePython`（各含 typed 解码失败对照）。

## 核对通过的项目（无发现）

- **双凭据与 token 恢复**：capability 先在 `RegistrationProfile` 校验（全局注册密钥只对返回 profile 的安装请求豁免）；
  `Register` 在 session 锁内重读、校验、固定自动地址（含丢失 receipt 时按 marker 主机）；`RegisterInstallationAgent`
  在同一 L3 事务里要求 marker == 本 session 且 name/host/port/service_type/deployment_method/profile_id 全等，才用存量
  token；否则仍需 Bearer。注册 JSON 里的 `gateway_installation_id` 不被读取。无 marker 的独立 Agent、外来 capability、
  身份变化（含管理员改 profile/端口）都拿不到 token。找不到让 token 被错误恢复或被其他 Agent 认领的路径。
- **enroll 原子性**：两份证明、busy、bound/registered、重复目标全部在一个 `UpdateAgents` 回调中先验证、后改写，
  回调内 ctx 检查后才改；失败零写入。
- **固定 ID 发布**：Mihomo 成功（`succeeded` 且无 http_status）后才 `BeginDeployment` OxiDNS；`retry_failed` 只替换终态 ID；
  `existing` 预约只读回原任务；准备失败记录 failed 并镜像到会话；中断的准备由 A14 `_get_deployment` 规则收敛为 failed。
- **锁顺序**：`creates`（LevelSetupSession）→ `sessions`（LevelDevice）→ repo；publication lease 为 LevelJob TryLock；
  worker 不持 session 锁做设备上传；C36 的三个 callback 不取安装锁。与 setup-session → device → repo 一致。
- **Runtime**：所有写 owner Detach 后入口、取锁后、rename/事务前检查 ctx；worker 由默认 Runtime 跟踪；
  关机后零写入、零上传，断连后完整发布；session 等待后关机不写。作者测试用真实 `Runtime.Start/BindRequest/Shutdown`。
- **脱敏**：脚本/短链只含本 session 的 capability 与地址；公开视图 `agents` 不含 token；系统密钥整份脱敏、
  用户凭据只用于 message/error/rollback_error；持久化进度先脱敏再写盘；remedy 只含 kind/github。
- **C36 注册**：`installation.Wire` + `wireSetupInstallation` 通过 `RegisterExternalOwners` 合并，保留 DNS/C32 owner；
  `HasInstallation` 不初始化 manager；错误保留 Cause、status/message，取消不伪装。

## 观察（不是发现）

- `public()` 每次都做 1 次 `secrets` + 每个 deployment 2 次完整 `ExportAll` 收集凭据；状态轮询频繁时是额外开销，
  正确性无影响，可交 O10 评估。
- Python `_server_url` 接受 `http://host/?`（空查询）并生成坏链接；Go 拒绝。属 Python 缺陷修复，影响可忽略，未单列。

## 需要 L4 实机验证

1. Debian 12 / Alpine 3.24 访客：手动网络与 `auto_network` 各一次，用短链命令以 root 与 sudo 运行（curl 与 BusyBox wget），
   面板设置 `AGENT_REGISTRATION_KEY` 时两核仍可经 capability 登记；enroll 后注册表带正确 marker。
2. 丢失登记响应 / 重跑安装命令：同一 ID 与 token 被恢复，无重复 Agent；从另一个 IP 用同一 auto capability 登记被拒。
3. 真实顺序发布：Mihomo 健康检查通过后才上传 OxiDNS；让 Mihomo 失败（主机上端口被占）时 OxiDNS 零上传；
   `retry_failed` 保留成功的 Mihomo 任务 ID、不重传。
4. 发布进行中重启面板容器：会话停在 publishing；显式点击继续发布后沿用同一任务 ID，不重复上传。
5. 网络确认：多网卡 / 两条默认路由主机被拒；整个流程不改主机 IP、默认路由，不隐式开启转发。
6. 完整 TUN 档案：安装脚本持久化 IPv4 转发并启用 TUN，C36 check 用真实状态通过；选择性接管在显式 apply 后
   managed active+healthy 才通过。
7. 配置改动后重新准备：capability/脚本不变，设备沿用，发布新 revision。
8. capability 过期（调整访客时钟或等待）：script/enroll/register 401，管理员发布仍可继续。

## 定向检查（.32）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/gateway/installation internal/server && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p 2 -count=1 -timeout=20m ./internal/gateway/installation/... ./internal/server'
```

gofmt 无输出，`go vet ./...` 通过。输出尾部：

```text
ok  	configflow/internal/gateway/installation	117.369s
ok  	configflow/internal/gateway/installation/difftest	19.118s
ok  	configflow/internal/server	17.866s
```

修复前对照：把 `adapter.go`/`register.go` 换回 `5f7d7d03` 版本后，F1、F2 两个新测试均失败
（`cannot unmarshal string into Go struct field agent.port`；enroll 未拒绝）；F3、F4 测试内含 typed 解码 / 旧分类的对照断言。
未跑完整 gate / proddiff / fuzz（未改记录解析；按 2026-11-18 规则由协调者批次执行）。

修复提交：`60df17ed6`（F1、F2，F3 的 Latest 部分）、`9ac80102f`（F3 GatewayStatus）、`24b2e097f`（F4），文档提交见 PROGRESS。
