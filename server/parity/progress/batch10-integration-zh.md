# batch-10 集成

基底 go@1fd89e1c1；私有 worktree `~/program/configflow-go-batch-10`，分支 batch-10。
依次 rebase C37/A24 `4b8ec848f`（2 提交）和 N8-10 core-1 `e871b2fe3`（5 提交），源分支未移动，未 push。

## 当前状态

- C37/A24 范围：lease 在 L3 前获取，StageCache 在 DB 事务中写 invalidation receipt；DB 提交后发布；清理先于 lease 释放。
- 指定 core-1 的五个提交没有修改 `api/agents/client_tools_adapter.go`，因此没有该文件代码冲突。逐行对照后，此文件完整等于 C37/A24 版本，diff 仅三项 import 和 applyClientIP。
- 唯一冲突是 12 份 MosDNS/OxiDNS pyref 二进制元数据。确认共 18,444 条输入/输出及 version 与两边完全一致，保留 batch-9 transcript；在集成树运行真实 Python record/check 刷新来源 SHA，未改变业务答案。
- record：MosDNS 53.386s / OxiDNS 43.535s；check：59.570s / 52.833s。另核对完整非 metadata payload SHA 全部不变。

## N8-8d 与运行时集成候选

已叠 N8-8d `d41dfdaac`（4 提交）；其 5 份 MosDNS transcript 冲突同样仅来源元数据，18,093 条答案完全一致，保留前两分支的组合。设计/说明自动合并保留双方。
C37/A24 与 core-1、N8-8d 均没有实际重叠的 Agent adapter 改动；此文件仍逐字等于 C37/A24 的版本。

发现原始 core-1 心跳分配上升。固定测试服务器、GOMAXPROCS=4、benchtime=3x、count=3，同一实际 warmed repo/public owner fixture：

| 路径 | 基底 | 直接叠 core-1 | 最终候选 |
| --- | --- | --- | --- |
| UpdateHeartbeat | 6,730–7,248 B / 134–136 allocs | 28,090–30,362 B / 413–415 | 8,341–9,029 B / 177–180 |
| ObserveHeartbeat | 基底尚无此入口 | 20,960–21,354 B / 318–319 | 7,434–7,952 B / 162–166 |
| HA guards | 0 B / 0 | warm 后 0 B / 0 | 0 B / 0 |

修复只在运行时保存七个独立 RawMessage，DTO 解码一次，已发布值不可变，部分更新沿用旧值；读取 overlay 仍经共享 model codec 恢复 R2/R5 证据。标准 JSON string/bool 的 MemberJSON 与 Marshal 使用相同 encoder，跳过已由 encoder 保证的重复整图检查。版本比较保留 exact-token 快路和 escaped-string 值比较。未恢复动态对象状态机，未借用仓库 snapshot，未新增持久化格式。

协调者裁定接受候选（公共入口约基底 1.3 倍，每次多几 KB），明确停止继续优化。新增真实 owner 分配回归：公共入口上限 10,840 B/216 allocs；native 上限 9,600 B/200 allocs，按候选最大值向上约 20%。定量上限仅用于与基准相同的 non-race build；race instrumentation 的编码/SQLite 额外分配不可与 non-race 混比。首次直接在 race 跑此上限出现 17,634/15,539 B 的工具形态误判，已拆为 !race 测试，所有语义/并发测试仍在 race 覆盖。Runtime 表示改动需 fresh Opus 只审此集成提交。
O10 后续保留两项：MemberJSON 标量编码、typed View 固定读取成本；本批不再试探调整。

新增 raw snapshot 回归验证数值字面量、嵌套坏类型、显式 null、部分更新继承、入参/诊断副本修改隔离、凭据不进入 live state。原真实 Runtime 关机/断开、删除竞态、版本类型/存在性回归保留。

## 候选定向验证

命令均在 rt / GOMAXPROCS=4 / -p 4：

- non-race `go test -count=1 -v -run "^TestBatch10HeartbeatAllocationCeilings$" ./internal/agents`：PASS 6.045s；public 8,201–8,234 B/176，native 7,279–7,308 B/162。
- `go test -race -count=1 -timeout=10m -run "Batch10|MemberJSON|MemberLayout|^TestHeartbeat" ./internal/agents ./internal/model ./internal/api/agents`：agents 5.040s、model 1.017s、API 73.039s，全部 PASS。
- 较宽的 Heartbeat/RuntimeState/Typed/ReviewN810/删除竞态 race 全部语义测试通过；该次包退出 1 仅因误将上述 non-race 定量上限放入 race（已说明并拆开），没有掩盖产品失败。
- `go test -count=1 -run "^TestAuditRepository$" ./tools/pyref`：PASS 0.777s。
- 真实负控：远端仅临时替换回叠入后原始 `9193c4376` 的六个产品文件（trap 恢复），同一新分配回归得到 public 28,084 B/412、native 20,856 B/317，均明确因 allocation regression 失败；控制脚本输出 NEGATIVE CONTROL PASS，无编译/超时假阳性，原文件已恢复。
- 共享模型 scalar fast-path 独立回归比较声明字段的完整 Marshal 证据，覆盖原始 null、错类型、escaped 文本、bool、赋值和 Clear。

## 下一步

完成候选定向 race、真实 Python 来源指纹刷新并单独提交给 fresh Opus；最终树跑四组分配、预检和完整 compare。
所有检查固定测试服务器，经 rt 运行；不跑完整 core。
