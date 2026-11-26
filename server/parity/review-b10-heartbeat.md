# Batch-10 心跳分配修复复核（3380a1c28）

复核者：fresh Opus（未参与该代码）。范围：仅 `git show 3380a1c28`（父提交 9193c4376）。

## 结论

修复方向正确，分配阈值和负控有效；但 DTO 改造引入了一处语义回归（键名大小写折叠），已修复。修复后公共入口分配进一步降到约 6.8 KB/154 allocs（原修复 8.2 KB/176），native 入口 7.2 KB/161。

## 发现

### M1（medium，已修）：心跳成员按大小写不敏感匹配

`UpdateHeartbeat` 先把 `jsonx.Object` 编码，再用 `json.Unmarshal` 解进 `heartbeatPayload`；`recordFields` 也用结构体解 `system_metrics` 的四个分量。encoding/json 的结构体字段匹配不区分大小写，同名键出现多次时取最后一个。修复前的实现（`model.Agent` 解码 + `MemberJSON`、`model.Fields.Get`）按精确键名读取，和 Python 的 `heartbeat.get("version")` 一致。

后果：已认证的 Agent 发送 `{"Version":"x"}` 或 `{"CONFIG_VERSION":"x"}` 会改写存储的版本（`config_version` 会影响下发判断）；`{"version":"a","VERSION":"b"}` 会取 b；`system_metrics` 中的 `CPU`/`Disk` 会覆盖实时的 `cpu`/`disk`。

修复：
- 新增 `heartbeatFromObject`，用 `Object.Get` 按精确键名取四个成员，并逐个 `json.Marshal`，得到的字节和整体编码时一致（同样做 HTML 转义，数字写法不变）。
- `recordFields` 只在样本以 `{` 开头时用 `model.Fields` 解码，再按精确名 `Get`（返回独立副本）。

测试：`TestReviewB10HeartbeatMembersMatchExactNames` 在 3380a1c28 上 4 个子项失败，在修复前的 9193c4376 上通过。`TestReviewB10PublicHeartbeatBytesMatchModelPath` 对字符串（含 `<>&`、引号、反斜杠、U+2028、非 BMP 字符）、null、bool、多种数字写法、对象和数组，逐字节比较新入口与旧路径（`agentModel` + `reportedHeartbeat`）的结果。

### L1（low，已修）：`batch10_alloc_norace_test.go` 未经 gofmt

导入块用了空格缩进，`gofmt -l` 会列出这个文件，过不了门禁。已格式化。

## 已核对、无问题

1. **语义**：
   - live overlay 的选择条件（运行时条目比存储的 `last_heartbeat` 新时才用它）和七个字段的覆盖顺序都与修复前一致。`RuntimeFields` 的下标 0–6 与 `fields[0..6]` 一一对应。
   - version/config_version 仍用存储的原始 JSON 做比较（`MemberJSON` + `sameStoredJSON`）。
   - 身份校验 `agentIDProven`（只认原始文本）和 HA 相关逻辑本提交没有改动。
2. **所有权与并发**：
   - 七个 RawMessage 的来源：`json.Marshal` 新分配、`MemberJSON` 返回副本、`Fields.Get` 返回副本。它们只会被整个替换，不会原地写入。不同 entry 之间共享 slice 是安全的，因为这些 slice 不可变。
   - 读取方面，`overlayModels` 经 `SetMemberJSON` 复制一份（`bytes.Clone`），`Entries()` 重新解析。`system_metrics` 同一个 slice 还会交给 `AddSample`，而它只读。
   - 删除和重新注册：`deleted[id] > mark` 的检查仍在锁内，逻辑没有变化。
3. **MemberJSON 快速路径**：`string`、`*string`、`bool`、`*bool` 走 `scalarJSON`。它和 `Marshal` → `marshalValue` 最终用的是同一个编码器（不转义 HTML，非法 UTF-8 替换为 U+FFFD，nil 指针输出 `null`）。新增 `FuzzMemberJSONScalarMatchesMarshal`，跑 60 s 没有发现差异。命名类型（例如 `type X string`）走 default 分支，不受影响。
4. **exact-token 快速返回**：两边去掉首尾空白后字节相同时，原来的比较对同一个输入必然得出相同结果。`storedText` 是纯函数：相同的字符串解码后相等；两边都不是字符串时回退到 `bytes.Equal`，结果也是 true。所以快速返回不会把不同的值判成相等，只是省掉了解码。
5. **阈值负控**：在 9193c4376 上，公共入口测得 28204 B/412 allocs，native 20948 B/317，都超过上限（10840/216、9600/200），测试失败，所以负控有效。

## 验证命令

见交回消息（`rt` 命令和最后几行输出）。
