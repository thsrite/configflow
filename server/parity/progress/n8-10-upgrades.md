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

实现提交：773af91d3。补充实现/验证中：
- 已批准两处 Python bug：worker final必须同一回包的id/update/status；空/缺失/错型arch
  在reservation前409、不遗留queued。5条真实Python独立对照已冻结，每行SHA绑定。
- 已确认单Agent坏string旧404、pair-list旧200统一改明确CorruptStateError500；int500保持。
  协调者再次明确批准，列表页须继续200并保留健康条目（已补测试，待验证）。
- 原A15 archive SHA未改；38条native字段/凭据拒绝逐行绑定，负控不让修改输入使用豁免。
  非字符串error的人工dict repr转native JSON诊断已批准；实际HTTP原始8条记录（含正常
  update对照）来自真实Python，不读取Go响应。7条完整native HTTP结果与两棵完整存储
  子树由Python policy独立推导，保留原130-request表和原core-2十条策略。
- .32包级race完整 PASS60.267s（其后仅新增更早守卫验证、GETbootstrap校验、API明确损坏
  诊断及额外Runtime/列表回归；最终增量仍在验证）。HTTP差分初次发现混合地址数组的
  typed []string会丢全部条目，已改逐项原始string过滤；重新差分执行中。
- 文档已补具体SQLite离线修复字段/事务/代际及后果；没有新增产品恢复API或UI。
