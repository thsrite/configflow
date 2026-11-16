package oxidns

const (
	messageMergeSources      = "YAML 合并字段必须为对象或对象数组"
	messageUnsupportedFormat = "不支持的校验格式：%s"
	// These complete messages preserve the existing user-facing null-type diagnostics.
	messageNullRuleType    = "所选规则类型暂不能转换为 OxiDNS 规则：None"
	messageNullRulesetType = "「%s」的类型被改成了 None：请改回原来的类型"
	messageNullMainType    = "主处理链 sequence_main 的类型是 None，不是 sequence"
	messageNullForwardType = "本地上游 forward_local 的类型是 None，不是 forward"
)
