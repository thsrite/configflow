package devicenotes

// Validation messages are shared by repository and editor adapters.
const (
	// NameMessage identifies an invalid display name.
	NameMessage = "设备备注最多 80 个字符，不能包含换行或控制字符。"
	// NotesMessage identifies an invalid stored notes object.
	NotesMessage = "设备备注必须为对象，最多 2048 条。"
	// AddressMessage identifies an invalid client address or range.
	AddressMessage = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"
	// PoliciesMessage identifies invalid policy-list shape or capacity.
	PoliciesMessage = "设备策略必须为列表，最多 512 条策略。"
	// PolicyFieldsMessage identifies unsupported policy-row fields.
	PolicyFieldsMessage = "设备策略仅支持 IP / 网段、备注和策略。"
	// PolicyModeMessage identifies an unsupported device routing mode.
	PolicyModeMessage      = "设备策略必须为智能分流或直连。"
	duplicateNoteMessage   = "设备备注地址重复：%s"
	duplicatePolicyMessage = "客户端 IP / 网段重复：%s"
)
