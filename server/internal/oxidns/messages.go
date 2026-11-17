package oxidns

var dnsSourceLabels = map[string]string{"local": "直连", "remote": "代理", "fallback": "备用"}

const (
	messageMergeSources      = "YAML 合并字段必须为对象或对象数组"
	messageUnsupportedFormat = "不支持的校验格式：%s"
	// These complete messages preserve the existing user-facing null-type diagnostics.
	messageNullRuleType             = "所选规则类型暂不能转换为 OxiDNS 规则：None"
	messageNullRulesetType          = "「%s」的类型被改成了 None：请改回原来的类型"
	messageNullMainType             = "主处理链 sequence_main 的类型是 None，不是 sequence"
	messageNullForwardType          = "本地上游 forward_local 的类型是 None，不是 forward"
	messageDNSUnknownKind           = "该类型"
	messageDNSUnsupportedKind       = "%s 规则不参与 DNS 分流"
	messageDNSIPUnsupported         = "IP 规则仅用于连接分流，不参与 DNS 处理"
	messageDNSCachedHead            = "自定义匹配位于规则之前且含缓存，拦截结果可能被缓存"
	messageDNSInvalidChoice         = "不支持的 DNS 处理：%s"
	messageDNSMissingRule           = "规则不存在：%s，请刷新后重试"
	messageDNSAssignmentUnsupported = "%s 规则不参与 DNS 分流，请把 DNS 设为默认上游"
	messageDNSLinkedUpstream        = "已绑定 DNS 联动，请改选直连 DNS"
	messageDNSStaleUpstream         = "所选上游已不在 OxiDNS 上游设置中，请刷新后重选"
	messageDNSUnchanged             = "OxiDNS 分流含自定义结构，DNS 未修改：%s"
	messageDNSInvalidResult         = "写入后 OxiDNS 配置校验失败：%s"
	messageDNSUpstreamKeyChanged    = "上游标识已更新，请刷新后重新选择；已有完整配置请在编辑器按当前上游重新生成分流"
)
