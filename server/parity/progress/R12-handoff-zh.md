# R12 移植交接（进行中）

分支 go-port-r12，从 go-opus-review-r11 建立；未 push、未开子 agent。只用合成数据和 loopback。

已读 R12 设计、Python plan/release/advisory、调用路径、R09–R11 复核和锁设计。
实现 native typed plan/check/ops、return allocation/handshake、Inspect、BuildPlan、Execute/Verify、
逆序回滚和 Publish/StartPublish。共享路径保留原 API；repo 仅加显式三态 JSON 边界转接到原 publication 事务。
R15 在 404 前持 L1 并将 held ctx/unlock 交 StartPublish；Publish 只拿 L2，不新建 JobLock。
6e5c7760 已确认接口和 AST 机制。

协调者裁定 D-R12-3：Result.Aborted && Result.Wrote 后仅一次本地 publication（失败记录、五类
manifest、revision）显式 ctxx.Uninterruptible；设备 I/O 和补偿仍响应 Runtime 取消。唯一位置
routers:saveAbortedPublication:ctxx.Uninterruptible。待 O3-6c 正式合入后原位扩展同一 repo AST 白名单。
O14 后续：R04–R11 F6 的 handler 内部被关机打断、归属尚未登记问题仍需统一补齐。

已跑 .64 编译检查 routers/repo 通过。首轮 22 个真实 Python 完整计划/结果/写序列/最终表对照，
RouterOS 全部一致；iKuai 发现新 NAT group check 的 id:null 既有缺项，已修，待复跑。

待完成：更多完整集成/阶段故障、原始请求、真实 Runtime publication、指纹/脱敏/三态边界、
AST 白名单和设计更新、定向 race/vet、正式依赖集成后变基。尚未标 ported。

首轮修复后：.64 TestR12PythonWholePlanAndExecution（22 组）+ TestGroup 全绿17.668s。
TestR12RuntimePublicationBoundaries 三条真实 Runtime/SQLite 回归通过0.981s：断开请求正常完成；
写设备前关机零 revision/记录；Fake-IP owner 已完成后关机不再 I/O/补偿，失败记录+归属同一次
publication 落库，revision 精确+1，下一次预览认作 synced，L2 释放。尚需补 AST/全阶段矩阵。
