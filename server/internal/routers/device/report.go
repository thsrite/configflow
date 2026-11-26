package device

// ForwardHintResult is advisory only. Never include it in a release operation,
// check, evidence snapshot, or fingerprint.
type ForwardHintResult struct {
	Status     string `json:"status"`
	FixCommand string `json:"fix_command"`
	Warning    string `json:"warning"`
}

// RouteItem is the route status of one proxy CIDR.
type RouteItem struct {
	CIDR     string   `json:"cidr"`
	Sources  []string `json:"sources"`
	Status   string   `json:"status"`
	Detail   string   `json:"detail"`
	RouteIDs []string `json:"route_ids"`
}

// ExtraRoute is a managed route (or policy destination) no longer desired.
type ExtraRoute struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	RouteID string `json:"route_id"`
	Comment string `json:"comment"`
}

// UnmanagedRoute is a hand-made route to the gateway ConfigFlow does not
// derive; Migrate (policy mode) means it becomes a policy rule.
type UnmanagedRoute struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Comment string `json:"comment"`
	Enabled bool   `json:"enabled"`
	Migrate *bool  `json:"migrate,omitempty"`
}

// RetiredPolicyItem is a destination of a policy rule being withdrawn.
type RetiredPolicyItem struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Comment string `json:"comment"`
	Restore bool   `json:"restore"`
}

// PolicyRules are our two policy rules (nil when absent).
type PolicyRules struct {
	Auto   *PolicyRouteEntry `json:"auto"`
	Manual *PolicyRouteEntry `json:"manual"`
}

// RoutesReport is the routes section; Mode and Policy only in policy mode.
type RoutesReport struct {
	ForwardHint    *ForwardHintResult  `json:"forward_hint,omitempty"`
	Items          []RouteItem         `json:"items"`
	Extra          []ExtraRoute        `json:"extra"`
	UnmanagedOther []UnmanagedRoute    `json:"unmanaged_other"`
	Mode           string              `json:"mode,omitempty"`
	Policy         *PolicyRules        `json:"policy,omitempty"`
	RetiredPolicy  []RetiredPolicyItem `json:"retired_policy"`
}

// NatItem is the NAT bypass status of one proxy CIDR.
type NatItem struct {
	CIDR    string   `json:"cidr"`
	Status  string   `json:"status"`
	Detail  string   `json:"detail"`
	RuleIDs []string `json:"rule_ids"`
}

// DisabledNat is a disabled bypass rule.
type DisabledNat struct {
	RuleID  string   `json:"rule_id"`
	Comment string   `json:"comment"`
	Dst     []string `json:"dst"`
}

// NatReport is the NAT bypass section of the drift report.
type NatReport struct {
	Items         []NatItem     `json:"items"`
	DisabledRules []DisabledNat `json:"disabled_rules"`
}
