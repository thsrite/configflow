# batch-10 集成

基底 go@1fd89e1c1；私有 worktree `~/program/configflow-go-batch-10`，分支 batch-10。
依次 rebase C37/A24 `4b8ec848f`（2 提交）和 N8-10 core-1 `e871b2fe3`（5 提交），源分支未移动，未 push。

## 当前状态

- C37/A24 范围：lease 在 L3 前获取，StageCache 在 DB 事务中写 invalidation receipt；DB 提交后发布；清理先于 lease 释放。
- 指定 core-1 的五个提交没有修改 `api/agents/client_tools_adapter.go`，因此没有该文件代码冲突。逐行对照后，此文件完整等于 C37/A24 版本，diff 仅三项 import 和 applyClientIP。
- 唯一冲突是 12 份 MosDNS/OxiDNS pyref 二进制元数据。确认共 18,444 条输入/输出及 version 与两边完全一致，保留 batch-9 transcript；在集成树运行真实 Python record/check 刷新来源 SHA，未改变业务答案。
- record：MosDNS 53.386s / OxiDNS 43.535s；check：59.570s / 52.833s。另核对完整非 metadata payload SHA 全部不变。

## 下一步

叠已批准的 N8-8d `d41dfdaac`，在最终树检查 typed registry 心跳/HA、原三组分配，预检和完整 compare。
所有检查固定测试服务器，经 rt 运行；不跑完整 core。
