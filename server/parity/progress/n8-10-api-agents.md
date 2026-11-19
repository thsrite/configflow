# N8-10 api/agents S pass

状态：实现中；分支 go-n8-10-api-agents，基底 upgrades@1051ddfc5（installer HR@f8df41fb1）。
不 push，不跑完整门禁；monitor 等协调者通知后 rebase。

## 已完成

- installer HTTP 参数用 native bounded binder / jsondecode exact-key，保留端口原始整数类型，
  null boolean、quoted/float/overflow port 拒绝；AIO、单服务、Docker 直接调用 typed renderer。
  保留 renderer 准入后才 mint capability 的顺序、原有 body limit / HEAD / 中文。
- Docker HA guide/enroll 改 native JSON boundary，不经 jsonx codec。
- 新增原生安装参数回归和 FuzzN810InstallCommandBinding；未改变旧 corpus/pyref entries。

## 已批准的公共接口和行为裁定（待后续提交）

- core Hooks.InstallationProfileTyped 参数依次 ctx、data(raw JSON)、id、token、fallback host；
  返回 profileID、updated raw JSON、error。InstallationRegisterTyped 参数依次 ctx、manager、
  id、token、data、existing token；返回 raw result、error。沿 gateway owner 原生边界，
  有 typed 同一事件不触发旧 hook，身份只认原始 JSON string。
- downloads_date 改 http.ParseTime，标准三种日期保持；无效日期包括人工超长日期忽略条件，
  旧500转通常200。旧行完整SHA绑定并登记 behaviour-changes；Range仍既有416裁定。

## 验证（仅测试服务器）

.64：`rt -- bash -lc 'cd server && gofmt -w internal/api/agents/body.go
internal/api/agents/installer.go internal/api/agents/docker_ha.go && GOMAXPROCS=4
 go test -p 4 -count=1 -timeout=10m -run "N810|Install|DockerHA|Compose|CoreInstall"
 ./internal/api/agents'`：PASS 8.320s。

.64：`rt --back server/internal/api/agents/n810_installer_binding_test.go
--back server/internal/api/agents/docker_ha.go -- bash -lc 'cd server && gofmt -w
internal/api/agents/n810_installer_binding_test.go internal/api/agents/docker_ha.go &&
GOMAXPROCS=4 go vet -p 4 ./internal/api/agents && GOMAXPROCS=4 go test -race -p 4
-count=1 -timeout=15m -run "N810Install|Install|DockerHA|Compose|CoreInstall|O9"
./internal/api/agents'`：vet静默，race PASS92.292s。

## 下一步

接 typed registry/heartbeat、content/public/client adapters、remote operation、下载日期/Range、
metrics/monitor（新基底后）；列清 deploy/gateway/setup/configexport 等 N8-11 余留消费方。
最终 gofmt、vet ./...、改动包/直接调用方 race、Corpus|Reference|Golden、api difftest、
TestAuditRepository、新 fuzz 60s；报告不把待集成/HR 说成 verified。

## 实现检查点（待本批提交与最终验证）

- 注册/手动创建/编辑/心跳接 ReadAgent/RegisterIdentity/AgentEdits/ObserveHeartbeat，
  请求走 Bind/jsondecode；typed installation hooks 与 gateway owner 已接通，原始string
  id/token、不同参数顺序和typed只触发一次有回归。
- public/content/client adapters 改 model/repo.Config/UpdateProfile/UpdateShared；
  IPTargets/AppendIP、native OxiDNS profile；新增只读 DeviceNotesJSON accessor。
  原始JSON凭据快照必须在删除私有任务树前交给redact；A13旧回归已发现并纠正一个漏接。
- approved RunServiceOperation / HAServiceOperationTyped / ServiceOperationJSON 接 native raw
  envelope，复用原有Detach、HA租约和冲突；占用成员租约restart/uninstall不发请求有回归。
- 下载日期已用 http.ParseTime，396条旧参考完整SHA绑定独立Python政策；pyref
  record→check PASS0.331s/0.346s，entries/input/venv与1051ddfc5逐项相同。
- 心跳四条旧500完整真实Python结果绑定native400；字节/版本/无符号数范围有测试与fuzz。
- 初轮 API安装+O9 race92.292s通过；注册/心跳/公开投影/client/content等宽race276.575s
  除新policy测试误把原生map交给旧WantJSON导致的两处断言桥错误外均通过；桥已修正。
  其后窄组发现metrics.RawJSON接线为[]byte(base64)及A13凭据快照缺失，已修复，复验中。

协调者批准不扩A05 engine，以下逐项归「N8-10 补充：metrics/trafficstore native 服务
（A05 engine）」，必须在N8-11删除facade前完成，不声称api/agents整包全native：

- traffic.go：readLegacyTrafficBody、trafficstore.CleanReport/AddReport；旧domain-discovery
  报告引擎只接jsonx，保留现有adapter，不新增第二引擎。
- traffic.go：metrics.CleanTrafficReport、TrafficHistory.Query、RetentionDays/SetRetentionDays；
  清洗、SQL聚合、保留期owner仍legacy，HTTP注册身份已typed。
- metrics.go 与traffic stats/trend：History.GetMetrics/GetMetricsSummary/GetTrafficStats/
  GetTrafficTrend仍返回旧owner的opaque结果；HTTP envelope/registry/query计数已native。

其余邻居扫描及最终精确命令见收口后更新；monitor等batch-12的新go，upgrades HR要求
方便检查点rebase到82adf451c（尚待本批提交）。

检查点补充：.64 native 注册版本授权/无写入、手动默认、A13 secret收集及HA成员busy租约
窄组 PASS（API1.527s，dnsha0.168s）；gofmt 已取回。宽HTTP差分已执行并显式保留失败：
主要为人工旧解释器诊断、native Unicode query/default、Docker非规范IP回落、下载日期
政策与少量参数诊断比较尚待接新政策；content两组通过。发现并修复 registration truthy
非string host 被接受、Docker action错型提前500（应由installer owner返回400）的接线问题。
现准备提交可编译检查点，再rebase upgradesHR@82adf451c，然后rebase --onto origin/go
f8df41fb1；新go=5c5998e86，monitor随后进入。不会以这些尚未收口的比较宣称完成。
