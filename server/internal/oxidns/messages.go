package oxidns

var dnsSourceLabels = map[string]string{"local": "直连", "remote": "代理", "fallback": "备用"}

const (
	messageMergeSources      = "YAML 合并字段必须为对象或对象数组"
	messageUnsupportedFormat = "不支持的校验格式：%s"
	// These complete messages preserve the existing user-facing null-type diagnostics.
	messageNullRuleType       = "所选规则类型暂不能转换为 OxiDNS 规则：None"
	messageNullRulesetType    = "「%s」的类型被改成了 None：请改回原来的类型"
	messageNullMainType       = "主处理链 sequence_main 的类型是 None，不是 sequence"
	messageNullForwardType    = "本地上游 forward_local 的类型是 None，不是 forward"
	messageDNSUnknownKind     = "该类型"
	messageDNSUnsupportedKind = "%s 规则不参与 DNS 分流"
	messageDNSIPUnsupported   = "IP 规则仅用于连接分流，不参与 DNS 处理"
	messageDNSCachedHead      = "自定义匹配位于规则之前且含缓存，拦截结果可能被缓存"
)
