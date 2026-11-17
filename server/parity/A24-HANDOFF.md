# A24 交回：client tools、OxiDNS 设备策略、域名发现开关

状态：**ported，8/8 个方法完成，剩余 0，未关闭问题 0**。分支 `go-port-a24`，最终验证基线 `go=d3124353`，包含 C31 verified、O3 BindRequest/Detach、N7-3a/N7-3b、A12 和 R07。未推送；独立复核后再设 verified。最终提交号见交回消息。

## 完成范围

| 方法 | `/api/agents/<agent_id>/` 后缀 |
|---|---|
| GET、PUT | client-notes |
| GET | mihomo/rule-targets |
| POST | mihomo/rule-targets/apply |
| PUT | domain-discovery |
| POST | domain-discovery/clear |
| GET、PUT | oxidns/device-policies |

全部要求管理员 JWT，Agent bearer 不能替代。配置空间取设备绑定值，忽略全局 UI 选择。GET/HEAD 只读。备注按 previous_name 比较后提交；策略保存在事务内重查绑定、settings revision 和 notes revision。纯改名保留 OxiDNS 设置、native draft 和 settings revision。IP/domain 规则复用 C09/C37；清理按 Python 顺序只删 traffic、probes、regions，保留 traffic_history。开关响应保留实时心跳覆盖。

C37 的最小接口已按裁定补齐：`ApplyOptions.BeforeWrite` 在共享事务内、库内容修改前运行，错误会终止事务，缓存和历史也不写；回调拿到事务 context，可绕过请求缓存重查绑定和默认规则集。`QueryRuleset(json.RawMessage, id)` 返回具体的 `*RulesetInfo`，复用原实现。已有调用方式保持兼容，PROGRESS-config C37 已加 Clarified in A24。

同包 `publicAgent(*agents.Record) *agents.Record` 供 A13 复用；委托现有 deployment、upgrade、HA 脱敏函数，去掉 token、私有比较内容、DNS follow job 和未读取历史哨兵，保留未来公共字段。最初单独提交 **78b796af** 已发给协调者；最终变基后该 helper 提交是 **1af82bee**。PROGRESS-agents A13 已注明不要另写。

写入口由 owner 调用 `lifecycle.Detach`，handler 传原 context。C37 下游历史和缓存 adapter 也保留 O3 停机取消父 context。没有新增后台 goroutine、依赖或私有算法副本；新增 JSONx 使用限于 adapter。backend/frontend 未改。

新增文件行数：`client_tools.go` 358；`client_tools_adapter.go` 392；`messages.go` 17；`client_tools_diff_test.go` 704；`public_agent_adapter.go` 36；`public_agent_test.go` 122；`discovery/query.go` 17；`discovery/a24_test.go` 171；真实 Flask 参照脚本 `client_tools.py` 50、`public_agent.py` 13。另修改 manager、路由注册、共享 deployment 序列化及 C37 事务桥接。

## 边界验收与测试映射

`TestA24FlaskBoundaries` 在同一份合成 SQLite/DATA_DIR 的两个副本上运行 Go HTTP 和真实 Flask，每次请求后比较状态码、JSON、no-store 头、完整数据库文档及全部 JSON/YAML 文件。规则缓存、native 策略草稿由真实 Python 库生成，不冻结 Go 输出当期望值。A07 catch-all 与字面量 rule-targets 同时注册并验证派发。Python 网络调用明确禁止；仅用合成数据。

`TestPublicAgentPython` 逐字段对比真实 `_public_agent` 的 10 组完整/未读历史组合，包含 token、私有部署内容和凭据证明、DNS job、空/非空 HA、upgrade、未来字段及空/null 情况；原记录不变，秘密标记不出现在输出中。

并发编辑用例证明两名编辑者只有一人提交、另一人冲突；纯改名的 settings revision 不变。取消请求后，备注、策略、开关、清理及 IP/domain 写操作仍完成。陈旧绑定/默认规则集被拒绝且无写入；开关保留版本、状态和实时指标。C37 拒绝守卫时比较完整数据库以及缓存路径、内容和 mtime；通过守卫与不带守卫的响应、库和历史一致。

`backend/test_client_tools.py` 的全部 15 个测试函数按行为映射在 PROGRESS-agents A24：HTTP 情况由本模块差分覆盖；legacy fallback、隐藏/删除名称及容量沿用 C31 的真实 Python `TestDeviceNotesBoundary`、`TestLegacyNamesAndPolicyPlan`；clone 沿用仓库的 `TestProfileCloneDoesNotCopyRouterAccess`（检查保留 device_notes），本任务不改 clone/import 实现。另覆盖 `test_oxidns_device_policy.py` 的 API 绑定、陈旧编辑、重复网段和管理员权限；实时开关对应 `test_agent_runtime_state.py`；A05 已有域名上报测试不变。没有遗留路由或功能。

## 发现与修复（均已关闭）

- **中：400 被映射成 500。** C09 的旧校验错误需要转换成 HTTP 层识别的 repo 校验错误，现与 Python 的 400 一致。
- **中：停机取消信号丢失。** owner 和 C37 adapter 使用 lifecycle.Detach，避免下游 WithoutCancel 抹掉 O3 的取消父 context。
- **中：空 HA 对象错误聚合。** 共享 deploy.PublicDeployment 原本将空对象当活动发布，覆盖 status/message/success；改为非空才聚合，与 Python 一致，并补真实参照回归。
- **中：旧 Agent 名称类型收缩。** Python 注册/编辑允许任意 JSON 名称；peers 不能强制转字符串。用 json.RawMessage 保留非空值，空值回退 ID；覆盖数字、null、布尔、空字符串/容器、对象和数组。
- **低：只读摘要混用快照。** rule-targets 从同一份配置快照生成；写守卫有意通过 repo 新读，避免请求缓存陈旧。

没有新增 Python bug 修正或故意功能差异，也未新增行为变更/基线例外。沿用既有格式类：native_config/template_base_config 的 YAML 按解析结构比较；其 revision 哈希包含实际 YAML 字符串，因此逐端验证响应 revision 确实对应该端已存设置，再供该端后续请求使用。没有忽略语义字段；只规范化不确定的 created_at/updated_at/applied_at 时间。

## 最终命令与结果

服务器均为 **192.168.0.64**。最终完整门禁开始 loadavg：`4.82 4.91 5.83`。没有人工压力实验。下面命令均退出 0。

最终变基后的格式和全仓 vet（无输出）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/agents/{public_agent_adapter,public_agent_test,client_tools,client_tools_adapter,client_tools_diff_test,messages,agents}.go internal/agents/manager.go internal/discovery/{write,bridge,query,a24_test}.go internal/deploy/adapter.go)" && go vet ./...'
```

N7-3b 基线完整门禁：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cat /proc/loadavg; cd server; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/agents/... ./internal/discovery/... ./internal/agents ./internal/deploy; go test -count=1 ./...; for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done; cd ..; server/scripts/proddiff.sh'
```

相关包 race：

```text
ok  	configflow/internal/api/agents	99.942s
ok  	configflow/internal/api/agents/difftest	188.201s
ok  	configflow/internal/discovery	8.590s
ok  	configflow/internal/discovery/difftest	8.126s
ok  	configflow/internal/agents	148.136s
ok  	configflow/internal/deploy	19.071s
```

普通全量的 94 个包条目全部通过（含仅编译、无测试文件的包）；12 个启用真实 Python 的 difftest 全绿，最后输出：

```text
ok  	configflow/internal/api/agents/difftest	111.202s
ok  	configflow/internal/api/aggregations/difftest	18.364s
ok  	configflow/internal/api/subscriptions/difftest	55.041s
ok  	configflow/internal/devicenotes/difftest	0.422s
ok  	configflow/internal/discovery/difftest	1.642s
ok  	configflow/internal/health/difftest	13.529s
ok  	configflow/internal/httpx/difftest	1.162s
ok  	configflow/internal/mosdns/difftest	2.999s
ok  	configflow/internal/refresh/difftest	3.651s
ok  	configflow/internal/subs/difftest	1.330s
ok  	configflow/internal/substore/difftest	4.608s
ok  	configflow/internal/webdav/difftest	13.784s
baseline: new=0 stale=0
```

proddiff：75 条 GET、2831 个案例；self-check PASS、coverage_regressions=0、unmapped_routes=0；11 项已有基线差异不变，**new=0、stale=0**。aggregation-provider 2/2、agent-deployment-bundle 3/3 继续一致。隔离运行的 loadavg 起止：`7.70 10.31 8.88` / `6.91 9.93 8.79`。

解析目标 `FuzzClientConnectionEntries` 已在先前完整门禁中跑 60 秒；ParseApplyItems 和该 fuzz 目标未被 N7-3b 改动。确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cat /proc/loadavg; cd server; go test -count=1 ./...; for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done; go test ./internal/api/agents -run "^$" -fuzz "^FuzzClientConnectionEntries$" -fuzztime=60s -parallel=2'
```

```text
fuzz: elapsed: 1m0s, execs: 502767, new interesting: 274 (total: 279)
PASS
ok  configflow/internal/api/agents  61.020s
```

已完成 git rebase go；工作区保持干净，不 push，不开子 agent。分支最终提交号在交回消息中提供。
