# N8-10 upgrades S pass

状态：实现中；已编译 upgrades 与 api/agents，尚未完成定向验证，未宣称 verified。
分支 go-n8-10-upgrades，起点 installer@6d84df68c（等待 installer HR 修复后 rebase）。

范围：upgrades 升级/地址/artifact 主体迁至 typed model 与原生协议，移除 jsonx/pystr/PythonClass。
保留 protocol-1 report、fresh heartbeat 回滚证明、protocol-2 worker+identity/version/migration
联合证明、durable reservation/ownership、deadline unknown 的恢复语义。API 接原生入口；
旧输入形状只在 *_test.go 转码；没有改依赖/工具链/阈值，不 push。

已完成：StartModel/PollModel/ReportLegacyJSON、ArtifactsModel；FollowModel/ConfirmedModel/
DescribeModel/ChangeModel；原始 JSON string 身份、exact-key jsondecode/duplicate拒绝、
原生 netip、shared redact、typed registry/EditAgent；设备 GET 无 body、JSON用 shared
EncodeDevice；write owner Detach，关键 I/O/事务/notice 前检查 ctx。

协调裁定：core Hooks 新增 AddressFollowTyped/AddressConfirmedTyped，typed 优先且不能双触发；
旧 hook 留 N8-11。坏 stored latest_upgrade/守卫非 null 错型用 CorruptStateError/500 拒绝，
不自动清；本轮不新增 Repair API/UI，文档给管理员离线修复并测试修复后恢复。
待补：typed hook优先回归/原分配阈值；损坏记录/no-write/恢复；Runtime断线/关机；
旧原语料逐行 SHA 绑定 native 错误；HTTP differential；新 fuzz60s；gofmt/vet/race/
Corpus|Reference|Golden/pyref audit；最终报告及rebase。所有检查经 rt 在 .32/.64 执行。

已执行：rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -p 4 -run "^$"
./internal/agents/upgrades ./internal/api/agents'：.32 PASS 0.004s/0.012s（编译）。
完整定向包测试仍在执行；尚未标记通过。不存在半迁移的第二套产品状态机。
