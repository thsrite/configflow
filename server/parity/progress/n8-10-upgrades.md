# N8-10 upgrades S pass

状态：主体实现与定向验证完成；收口提交与 installer HR tip 变基中，未宣称 verified。
分支 go-n8-10-upgrades；起点 installer@6d84df68c，按协调通知接 f8df41fb1。
未 push、未跑完整门禁；fresh HR / batch gate 由协调者安排。

## 完成边界与接口

- upgrades 全部产品文件无 jsonx / pystr / PythonClass；旧 Python 形状只有 *_test.go
  机械 codec，运行同一套 native owner，没有第二套状态机。Go 原生 netip、jsondecode、
  model、redact、jsonresp.EncodeDevice；存储修改用 EditAgents / EditAgent。
- `StartModel(ctx,m,id)` / `PollModel(ctx,m,id)` → `*model.AgentUpgrade`。
  `ReportLegacyJSON(ctx,m,id,raw)` 接 exact-key / duplicate拒绝的协议1报告。
  `StoredState(*model.Agent)` 校验原始存储证据，不发设备请求，供 legacy bootstrap 使用。
  `ArtifactsModel(ctx)` → `model.Map[model.UpgradeArtifact]`；`ArtifactDirectory(ctx)`。
- `FollowModel(ctx,m,*model.Agent,host)` / `ConfirmedModel(*model.Agent,host)`；
  `DescribeModel` → AddressDescription；`ChangeModel` 接 string → AddressResult/status。
  HTTP 更新、查询、自读/report、地址及列表 artifact 接入上述 native API；其余 API binder
  仍归 api/agents 下一轮 S pass，未宣称整个 API 包已 native。
- core 公共 API（协调者批准）：Hooks.AddressFollowTyped / AddressConfirmedTyped 接
  *model.Agent。HTTP heartbeat 已有 typed snapshot直接进入hook；typed优先、旧hook不双触发，
  没有新增 registry 查询或 jsonx 往返。旧 hook 留给 N8-11 删除。
- upgrades 没有 installer renderer 调用，不拼安装 shell，也不传 renderer 默认端口0。
  installer guide/enroll/native renderer 接口由其既有 API owner 使用，本轮不另造适配层。
  rebase 接纳 HR 的 shellWord/根目录别名/ss unknown 修复，不改其公共接口。

## 保留的协议与批准的变化

保留 protocol-1、durable ID、restart 后恢复、fresh authenticated heartbeat 回滚确认；
protocol-2 保持 worker + executable identity/version/migration 联合证明。丢应答保留同一ID，
queued acceptance不覆盖已观察到的更晚阶段；过期仍未知时继续查询，不假称回滚成功。
保留事件去重、旧 worker 时间戳拒绝、未知 stored 字段及原始类型证据。

协调者明确批准并登记 behaviour-changes：

1. stored latest_upgrade / latest_deployment / HA守卫 present、非null、类型不对：
   CorruptStateError/500、中文字段说明、不写、不自动清。单Agent string由原404、pair-list
   由原200改500，int原500保持；GET /api/agents 列表仍200，健康条目保留。
   文档 doc/manual/agents.md 给出具体SQLite字段、代际/commit_id事务、备份与后果；
   清除坏成员后重新升级成功有回归。本轮不新增恢复HTTP/UI或无调用方Repair API。
2. protocol-2 必须同一 worker 的 update_id + agent_id + status全部匹配，错agent_id即使
   另一条 upgrade-info 正确也不写成功/不解锁（Python原来能误判成功）。
3. 非空 upgrade-info 缺失/空/非字符串 architecture，在reservation前现有中文409拒绝；
   Python缺失/空/空数组先留queued再500，truthy numeric原来已409。
4. 机器版本ASCII 64字节白名单；身份只认original JSON string；protocol/时间拒绝bool、
   字符串冒充数值和溢出，不模拟CPython 4300位上限、异常文本、私有空白/IP表。
   人工error对象改native JSON诊断文本，正常string中文与redact保持。

原 A15 reference/review archive、原130-request表、core-2旧十行policy、原pyref entries均不改。
38条native字段/凭据拒绝逐行完整SHA绑定；5条真实Python升级bug对照绑定原输入/请求/
结果/存储。实际HTTP新archive有8条完整原行（含正常update对照），7条新HTTP结果及
2棵完整存储子树由独立Python policy生成，不读取Go响应；修改输入不命中、运行执行计数、
no-write fingerprint与请求trace都检查，未豁免未绑定字段。新文件见本分支 git diff --stat。

## 风险类与验证

- 身份/所有权：原始string token/id/version/update_id及exact-key；非bool readiness无证明。
- 并发/关机：所有写owner Detach，保留 originating Runtime；入场、I/O、事务、notice前检查。
  地址worker由SpawnerFrom(ctx)发起，不用Background丢generation。artifact读取/锁后/
  摘要缓存发布也检查ctx。真实Runtime覆盖断线完成、关机不reservation、不保存迟到accept、
  不发布摘要；随后合法重试恢复。共享原超时/限额不改。
- 脱敏/设备字节：只复用redact；真实loopback核对GET空body、POST body/header/path，
  update控制NoProxy由原proxy差分验证，普通地址探测保留其proxy路径。
- 原生地址数组逐项过滤原始string，夹杂数字不丢合法地址（首轮差分发现并修复）。
- Python覆盖：backend/test_agent_upgrades.py → upgrades_test/native_test；
  test_agent_upgrade_auth/messages.py → API upgrades_test + 130请求差分；
  test_agent_address_follow.py → address/tracked_address/API address_test + proxy差分；
  test_agent_artifacts.py → artifacts_test + Runtime摘要取消测试。Windows专属分支沿原说明不移植。

全部检查经 rt 在 .32/.64，未在Mac跑测试。变基后的最终命令/结果在末节补齐。

- .32 完整 upgrades `go test -race -p 4 -count=1 -timeout=20m ./internal/agents/upgrades`：
  PASS60.267s；后续 context/artifact/恢复/身份等相关race：upgrades10.496s、API28.853s PASS。
- .32 API `-race -run "Upgrade|Address|HeartbeatReportedHost|N810TypedHeartbeat|PublicAgent"`：
  PASS167.560s。最后跨包 Corpus|Reference|Golden|Heartbeat|Hook：agents214.450s、
  upgrades33.706s PASS；server编译/race启动1.106s（此regex无test）。API该轮唯一失败为
  新列表测试拿注册回包不存在的name比较；修为fixture真实名称后上述28.853s组通过。
- .64 `CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=15m -run
  "Upgrades.*DifferentialAgainstFlask|N810UpgradesHTTPPolicy" ./internal/api/agents/difftest`：
  PASS22.288s，130请求 + 6 proxy场景 + 设备请求/全部Agent/notice/DATA_DIR终态。
- .64 `go test -run '^$' -fuzz '^FuzzUpgradeNativeProtocol$' -fuzztime=60s -parallel=2
  ./internal/agents/upgrades`：PASS61.010s，639074 executions。
- .64 `go test -v -count=1 -run '^TestBatch10HeartbeatAllocationCeilings$' ./internal/agents`：
  原阈值PASS5.769s。public empty/2MiB 6808/6774 B、153alloc；native7197/7240 B、160/162alloc。
- 全仓gofmt -l静默、go vet -p 4 ./...静默；tools/pyref TestAuditRepository PASS0.820s，
  最后增量复跑PASS0.733s。go.mod/go.sum/Dockerfile/依赖/分配阈值均未改。
