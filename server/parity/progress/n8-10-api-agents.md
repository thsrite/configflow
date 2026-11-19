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
