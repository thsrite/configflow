# R10 IPv6 移植交接

任务：移植 ipv6_policy / ipv6_fakeip / ipv6_routes，依现有 R08/R09 owner 和 R12 typed operation 契约；只用合成数据与 loopback fake，测试全部经 rt。不 push，不开子 agent，不访问生产/设备/PVE。

状态：开发中。已完整阅读 Python 三模块、readiness 下一跳 helper、RouterOS transaction/batch 调用链及四份相关测试；按 review-r09 F5 扩展共享 evaluator（表、证据、NAT translation），保留 R09 结论。

下一步：完成 typed settings/manifest/plan/write/verify、报告接线；真实 Python 边界差分、raw request 与 YAML 字节比较、Runtime 取消测试；rebase go，定向合并树测试和 .64 gate -p 6；登记行为修正、测试映射和逐类复核规避说明。

已完成（待最终门禁/复核）：多表 evaluator + 全部 policy_checks；typed YAML settings、IPv6 下一跳/证据；Fake-IP 与规则集 inspect/apply/restore/verify；共享驱动注册、Manifests 与 BuildReport 接线。真实 Python 路由差分（含批量、失联/拒绝、回滚）、330 条完整规则证据差分、14 组 settings/YAML 原始字节、Runtime 取消/逐提交点及归属防护测试已通过。

最近定向验证：
`RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/routers -- bash -lc 'cd server && gofmt -w internal/routers/ipv6* && go test -p 6 -count=1 -run "TestR10|TestR09SharedEvaluatorPython" ./internal/routers && go vet ./internal/routers/...'`
→ `ok configflow/internal/routers 10.377s`，vet 无输出。

行为修正草录（最终写入 behaviour-changes）：每批删除前核对完整 manifest；添加前拒绝预览之后出现的外部同目标路由；回滚只按可信响应 ID 重映射、不按相同配置收养；部分成功恢复仍登记新 ID；回滚重映射保留旧配置证明、不把随后外部编辑更新为受管配置。R10 无 YAML 生成器，YAML 只读；验收比较 Python 返回的原保存 YAML 字节（CRLF/注释/引号/alias）和 settings 完整结果，原文不改。

仍需：增加部分失败/规模与模型边界检查；更新逐文件测试映射、复核类别规避、L5 项；共享 R04–R09 合并树测试与最终 .64 gate -p 6；rebase 最新本地 go；最终中文交回。

补充验证已通过：完整 BuildReport 差分（missing/synced）、501/4096 规模差分、公共 host 错误的共享脱敏（保留 username/api）；IPv6 仓库边界 5 writers × 6 类破坏 × 两种 manifest，加修复/克隆/关闭保护。`.64`：routers `9.568s`，repo `21.118s`；`python3 scripts/r10-mutation-gate.py` 四类 purpose-break（result/journal/request/device-table）均 `DETECTED (compiled, real Python boundary mismatch)`，最终 `4/4 detected; sources restored`。
