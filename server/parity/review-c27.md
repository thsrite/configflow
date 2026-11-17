# C27 对抗性复核

范围：`backend/common/rule_dns.py` 的全部 885 行，对照 `oxidns/ruledns.go`、
`ruledns_state.go`、`ruledns_apply.go` 及原生 generation/routing 协作者。
当前为检查点；并发/事务与 D2 安全处理已通过 targeted race，最终验收未完成，状态保持 ported。

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
- **中：过期 pin 被静默归到另一个来源。** 合法 upstream tag 含 U+2028/U+2029 时，
  Go 原生 JSON 哈希与 Python 旧 EntryKey 不同；旧 fallback pin 无 hash 命中后按形状
  回退成 remote。标识变化由维护者 D2 接受，**猜错 source 不接受**。保留 Go 原生
  JSON 编码，不加 adapter；对含这两个字符、无任何原生 source hash 命中的 entry，
  `pinnedSource` 返回未知，`parseExec` 返回明确的过期/恢复提示。共享 recognizer 保持
  native 图锁定；严格写入 typed 400/409，宽松 warning，原 settings/source/YAML 不变。
  协调者批准 template 刷新重选、native 编辑器重建分流的保守恢复路径，已通知 C29。
- **低：人为类型哨兵。** 删除 `behaviorOf` 通过 bool-map 拼 `\x00` 区分非字符串的
  写法，改为 native YAML Node 的类型/值谓词；不改变规则可转换性和 IP 分类。

## 新边界语料

`testdata/review_c27_gen.py` 独立构造输入，直接调用真实 Python 公共入口；不读取 Go
实现来生成期望，也不 mock routing/generation。505 条参考：469 条严格比较、93 条普通
预期拒绝，额外严格比较 314 份完整最终 OxiDNS 配置；36 条显式 D2 断言。Python 对
D2 中 18 次旧标识写入生成了配置，Go 按批准决定安全拒绝；不把它们算作成功生成。
保留作者 282 条/148 份为补充。

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

比较成功结构、中文错误、typed lock usages、参数调用后状态和生成结果。
36 条 D2 不是整行豁免：12 catalog 仅允许已列出的 key 值改变，其他字段严格相同；
12 旧 choice 必须 typed stale/400；6 旧 native read 必须准确的锁定理由、无猜测 route，
并且原交付结构仍与 Python 完全相同；6 重建必须 typed lock/409，另测 lenient warning
和完整 settings/YAML 不变。字面 `\u2028` 控制组仍严格比较；新 Go 标识的 6 个组合
必须可读、保留 source 并可继续写入。依据/恢复路径写入 [行为差异表](behaviour-changes.md)。
静态函数/行覆盖放在 `testdata/review_c27.coverage.json`，用于继续核查未覆盖分支。

## 已执行检查

基线 `.32` `rt -- server/scripts/proddiff.sh`：exit 0，四项自检 PASS，65 GET / 66
method-path / 282 次比较，5 个既有全局差异，`new=0 stale=0 coverage_regressions=0`。
OxiDNS main/Agent/files/downloads 各 2 成功。没有把未实现 C31 等路径算作成功。

`.63`：作者 `go test -count=1 -run "^TestRuleDNS" ./internal/oxidns/` 通过 1.313s。
新参考生成输出：`rows=451 generated=305 rejections=93`；可空字段修复后的新边界及作者
边界通过 3.265s。helper ownership 修复后，新旧边界再次通过 3.144s；gofmt 无输出，
`go vet ./...` 通过。

## 并发、取消和事务契约

`review_c27_transactions_test.go` 在真实临时 repo 上验证 C32 的调用方式，不是替 C32
实现路由：先 `context.WithoutCancel(requestCtx)` 再等仓库锁，Scope 不丢失；在 updater
提供的完整 resolved snapshot 上构造 before/after，成功后才安装返回 Settings。

- 同一 template/native Profile 上 8 个 goroutine 各做 4 次 read/apply；直接修改返回的
  Agent name 指针、额外嵌套 scalar、Agent 副本，原共享快照保持字节不变。
- 第一个事务持锁，取消后的普通 ctx 确定返回 context.Canceled；detached ctx 等锁后成功，
  既保留前一写者的字段，也提交自己的 DNS 选择。
- 12 个并发 canceled-but-detached 事务：全部新增规则和 direct/proxy 选择都保留，
  revision 恰好增加 12，另一个 profile 完整不变。
- 严格 native routing conflict：新增规则、设置、revision、时间戳整笔回滚，typed usages 保留。
- C27 不存 ctx、不启动 worker、不直接写 repo；C32/C29 仍须在 HTTP 层执行上述 detachment，
  本复核不以纯函数安全替代尚未实现的 route 4b 验证。

`.63` `go test -race -count=1 -run "TestC27Review|^TestRuleDNS" ./internal/oxidns/`
通过 35.416s；包括所有新旧边界、并发/事务、D2 显式结果与旧 native 交付结构。

## 生产覆盖

`.32` 只读 snapshot mount、仅 loopback namespace；复制到临时 DATA_DIR。辅助
`review_c27_snapshot.py` 调用真实生成/文件/下载、公共读模型和 no-op 写入，不导出配置、
response、ID 或异常文本，仅导出静态行号和计数：[coverage](review-c27-production.coverage.json)。
这不是框架全部请求的 trace，也不声称 C32 路由已存在。

两份 profile：rule_actions 总计 2，follow 54，parked 0；main/files/downloads/read/no-op 各
2 成功，显式 route 读 94 次。`upstream_chain`、`helper_plugins`、`_pinned`、`_source`
无命中：生产快照没有覆盖 pinned helper 的构造/识别；写入只覆盖 no-op，故不能用 proddiff
成功代替 strict/lenient、pin、冲突、source hydration、多步 park/restore 和取消/并发边界。

后续：mutation 验证，最终 `.32` gofmt/vet/race/full suite/proddiff，rebase go，干净提交交回。

## Go 原生化边界

C27 新增 API 是 typed view/options/assignment/update、`*yaml.Node` 的开放上游数据和
普通 Go error，没有新的 Python 异常层或 `any` 调用形状。此次删除了 NUL 类型哨兵，
组合 issue key 用结构体，字符串/正则继续使用 Go 原生库；EntryKey 保持设计 D2 的
原生 JSON 编码。未添加依赖或私有共享 helper 副本。

既有 `adapter.go` 的 `NewProfile`/`splitRules`/`renderMembers`/`classicalWithoutIP`
仍连接旧 repo/rules/ippresence 接口，涉及 `jsonx`；核心图 recognizer 用内部 `!reason`
编码 invalid route。这些既有结构由 N8/S-oxidns 的设计轮次处理，本次不重写跨包接口或
整个 routing engine。固定 error/changed/settings 的公开 C27 契约保留，唯一导出字段
类型调整是已获批准的 Agent 可空字符串。

复验命令：

```sh
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/oxidns/testdata/review_c27.jsonl.gz --back server/internal/oxidns/testdata/review_c27.coverage.json -- bash -lc 'PYTHONPATH=$PWD ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/review_c27_gen.py'
RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/)" && go vet ./... && go test -count=1 -run "TestC27Review|^TestRuleDNS" ./internal/oxidns/'
```

最后输出 `ok configflow/internal/oxidns 3.977s`，exit 0。
