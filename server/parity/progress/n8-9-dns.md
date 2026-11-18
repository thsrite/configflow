# N8-9 DNS 迁移

分支：`go-n8-9-dns`，基点 `go@03257978f`。仅在 .32 使用 rt 做定向检查，不跑完整门禁。

## 范围与边界

- MosDNS：typed model 设置、规则布局、生成器、设置 HTTP DTO 与 typed 仓储写入。
- OxiDNS：内部 native YAML + typed 仓储/metadata；移除 Python truthiness、私有 JSON/标量 codec。
- `dnsha` 和 Agent 业务不修改。协调者批准 `oxidns/legacy.go` 只保留转码，旧 `NewProfile(*jsonx.Object)`、`ToJSONX` 等待 N8-10 迁移 `agents/rulesync/adapter.go` 与 `api/agents/client_tools_adapter.go` 后删除；N8-11 仍须检查这两个调用方。
- 不把 model.Raw 重新编码后当作消费模型。原始 bytes/revision 由仓储 R1–R9 保留；消费使用共享 R5 typed 字段。

## 验证记录

实施中，最终命令与结果随提交补齐。MosDNS 新 typed 生成、共享 R5 消费与不修改源对象的定向用例已在 .32 通过；既有参考语料需要以 typed/R5 投影的真实 Python 输入复验。没有改 baseline。
