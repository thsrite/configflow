# N8-9 DNS 迁移

分支：`go-n8-9-dns`，基点 `go@03257978f`。仅在 .32 使用 rt 做定向检查，不跑完整门禁。

## 范围与边界

- MosDNS：typed model 设置、规则布局、生成器、设置 HTTP DTO 与 typed 仓储写入。
- OxiDNS：内部 native YAML + typed 仓储/metadata；移除 Python truthiness、私有 JSON/标量 codec。
- `dnsha` 和 Agent 业务不修改。协调者批准 `oxidns/legacy.go` 只保留转码，旧 `NewProfile(*jsonx.Object)`、`ToJSONX` 等待 N8-10 迁移 `agents/rulesync/adapter.go` 与 `api/agents/client_tools_adapter.go` 后删除；N8-11 仍须检查这两个调用方。
- 不把 model.Raw 重新编码后当作消费模型。原始 bytes/revision 由仓储 R1–R9 保留；消费使用共享 R5 typed 字段。

## 验证记录

实施中，最终命令与结果随提交补齐。MosDNS 新 typed 生成、共享 R5 消费与不修改源对象的定向用例已在 .32 通过；既有参考语料需要以 typed/R5 投影的真实 Python 输入复验。没有改 baseline。

## 已完成的定向检查（实施阶段）

- `.32`：MosDNS typed generation / R5，OxiDNS whole-draft 事务和 If-Match，C27 非字符串展示名称，ZIP shutdown / disconnect；新 MosDNS owner 的真实 Runtime 断连、已关机、事务锁等待取消，定向 race 通过。
- ZIP 原录制 86 条全部通过，包含匹配缓存回退和文件副作用；未放宽断言。
- `yamlx.CopyGraph` 经协调者批准，直接复用 Edit 的 copyGraph；共享关系、别名预算、源不变、并发编辑与放大负控 race 通过，未改 Clone 架构门。
- CopyGraph 物理图基准（100 次）：深度 8 / 64 / 256 分别为 5,064 / 41,256 / 164,456 B/op，38 / 268 / 1,040 allocs/op，与物理图规模成比例。
- OxiDNS `Generate → ippresence → rulefetch / RulesetFormat` 基准（真实离线 hook，128 条规则，3 次）：无无关字段 5,919,816 B/op、22,495 allocs/op；增加 2 MiB 无关字段 5,932,349 B/op、22,497 allocs/op。构建 profile 在计时之外，生成链没有整配置解码。基点数字和最终复验待补。
- Native Reader / R5 的旧异常形状与极值语料仍在逐项核对；不能把它们直接视为已通过。当前没有改 proddiff/configdiff baseline。
