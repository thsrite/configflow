# N8-9 原生边界期望

原 Python `.jsonl.gz` 录制和 N7 历史索引不改。以下文件只登记明确的新合同，不能自动扩展：

- `native-request-expectations.json`：99 个原输入的严格请求类型拒绝。读取旧存储另做兼容投影；不能把这些拒绝用于存储读取。key 是各原始 JSON 参数去除字符串以外空白后、逐项加 NUL 的 SHA-256。
- `native-validation-expectations.json`：52 个完整诊断结果。布尔值不当数值零，错误的 weekday/monthday 容器由 schema 报类型错误，不再追加误导的缺失条件。基于原 Python 结果只去掉这些明确的冗余条目，其余消息、位置、顺序、来源与错误壳全部保留。key 加上函数名前缀。
- `native-capability-expectations.json`：2 个 UDP entry 为数字 5 的输入。不能从非文本的处理入口自动添加 TCP。只有 tcp_enabled capability 改成明确的不可编辑提示；其他完整结果继续比较。真实 Python 仍复现错误的可编辑状态，正确文本入口的正控必须可编辑。
- `native-reader-expectations.json`：108 个合成 YAML 源文本 SHA-256。它们只登记私有 OxiDNS 标量解释器退役后的原生图；必须同时逐节点匹配共享 yamlx 的完整图，不能仅凭 fingerprint 放行。未知新文本仍失败，递归/别名预算与 policy 拒绝不变。

R5 参考输入先经共享 model 解码，仅把声明字段的消费值投影给真实 Python。YAML 输入经共享 ReferenceText 表达原生标量语义；不 patch Python 模块，不从 Go 生成结果推算期望，不跳过其他字段。Python 若原样保留源文本，只恢复这份完全相同的原文；生成或修改后的文字不替换。

JSON 边界遵守共享 yamlx.ToJSON 的实际 token：大整数源码被 yaml.v3 标为 float 时，原整数 token 的 JSON 输出仍按整数比较。普通浮点、整数和布尔正负控继续严格区分。生成 YAML 的完整语义比较保留非字符串映射键，不能经只适用于原生文档的安全字段名验证器丢掉这些键。
