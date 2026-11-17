# C27 对抗性复核

范围：`backend/common/rule_dns.py` 的全部 885 行，对照 `oxidns/ruledns.go`、
`ruledns_state.go`、`ruledns_apply.go` 及原生 generation/routing 协作者。
当前为检查点；最终验收和并发/事务检查尚未完成，状态保持 ported。

## 问题和修复

- **中：返回 helper 与调用方共享可变节点。** `upstreamChain` 直接把 settings 的
  `cache_lazy_ttl` YAML 节点放进返回的 cache args。编辑返回插件的 scalar.Value 会修改
  调用方配置；其他调用读取该配置时也可能发生 race。Python 的整数是值，不能发生这类
  输入反向修改。改成复制标量；`TestC27ReviewHelperOwnership` 在原代码上稳定失败。
- **低：读模型丢失可空元数据。** 缺少 service_type 的旧 Agent、name 为 null 的 Agent，
  Python 输出 JSON null，Go 输出空字符串。协调者批准按 N8 可空类型规则把
  `RuleDNSAgent.ID/Name/ServiceType` 改成 `*string`，JSON 字段名不变；空字符串仍是
  空字符串，返回指针不指向调用方 YAML。C32 尚未开工，无现有调用方需迁移。
  新 Python 参考在原代码上复现两条差异；另补 null ID 和空字符串边界。
- **低：元组键拼接。** validator issue 去重键从 Path/Code/Message 用 NUL 拼接改成
  `validationKey` 结构体。现有 validator Code 是固定常量，尚无可达碰撞证据；这是原生化
  清理，不夸大成功能或安全缺陷。

## 新边界语料

`testdata/review_c27_gen.py` 独立构造输入，直接调用真实 Python 公共入口；不读取 Go
实现来生成期望，也不 mock routing/generation。451 条参考、93 条预期拒绝，额外比较
305 份完整最终 OxiDNS 配置（解析后比较）；保留作者 282 条/148 份为补充。

- template/native、aggregate 开关，与显式 action/default/follow 的交叉组合。
- 同事务启停/库停用/删除/重排/策略拒绝/组默认改动、rule→ruleset、classical 内容和行为改变。
- 多步 park、重新启用、跟随恢复、改类型和删动作序列。
- 修改 owned helper、main 自定义步骤、重复 tag、保留旧无关 validator 错误；strict/lenient、
  no-op、无关 metadata 改动、删除与跟随写入。
- 已有 linked pin 保留、新 linked/stale pin 在批量第二条失败、整批不可部分提交。
- 规则库 inline/remote/disabled 源 hydration、旧源字段清除、未知字段保留，然后真正 DNS
  写入和最终 generation；空 profile fallback。
- catalog 的重复 tag、错误 plugin 类型/args、原始 index 绑定、完整 entry 去重、
  同地址不同属性、parked fallback 不应成为候选。
- group 递归 2/63/64/65 层，旧 Agent 可空元数据，active/inactive follow replacement。

比较成功结构、中文错误、typed lock usages、参数调用后状态和生成结果；无新 waiver。
静态函数/行覆盖放在 `testdata/review_c27.coverage.json`，用于继续核查未覆盖分支。

## 已执行检查

基线 `.32` `rt -- server/scripts/proddiff.sh`：exit 0，四项自检 PASS，65 GET / 66
method-path / 282 次比较，5 个既有全局差异，`new=0 stale=0 coverage_regressions=0`。
OxiDNS main/Agent/files/downloads 各 2 成功。没有把未实现 C31 等路径算作成功。

`.63`：作者 `go test -count=1 -run "^TestRuleDNS" ./internal/oxidns/` 通过 1.313s。
新参考生成输出：`rows=451 generated=305 rejections=93`；可空字段修复后的新边界及作者
边界通过 3.265s。helper ownership 修复后，新旧边界再次通过 3.144s；gofmt 无输出，
`go vet ./...` 通过。

后续：并发只读快照/返回值隔离，真实 repo 事务中的取消等待、串行合并与冲突回滚；
生产分支覆盖核对，最终 `.32` gofmt/vet/race/full suite/proddiff，rebase go，干净提交交回。
