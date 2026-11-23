package ikuai

// Typed iKuai write forms for objects ConfigFlow creates. Edits and restores
// send an open device record (ikuaiRow) instead: they must write back every
// field the device returned, including ones this code does not know.

// ikuaiForm is a write form: a typed new-object form or a device record.
type ikuaiForm interface {
	// formTag is the object's unique name ("" when it has none).
	formTag() string
	// formID is the record's id as text ("" for a new object).
	formID() string
}

func (r Row) formTag() string { s, _ := r["tagname"].(string); return s }
func (r Row) formID() string  { return r.Text("id") }

// ikuaiAddressSet is an address or port field: literal entries and IP group
// references.
type ikuaiAddressSet struct {
	Custom []string `json:"custom"`
	Object []any    `json:"object"`
}

// ikuaiAddresses is a field listing values literally.
func ikuaiAddressList(values []string) ikuaiAddressSet {
	return ikuaiAddressSet{Custom: append([]string{}, values...), Object: []any{}}
}

// ikuaiTargetSet is a destination: refs (IP group references) or dst listed.
func ikuaiTargetSet(dst []string, refs []any, useRefs bool) ikuaiAddressSet {
	if useRefs {
		return ikuaiAddressSet{Custom: []string{}, Object: refs}
	}
	return ikuaiAddressList(dst)
}

// ikuaiSchedule is the 端口分流 time field.
type ikuaiSchedule struct {
	Custom []ikuaiWeekly `json:"custom"`
	Object []any         `json:"object"`
}

// ikuaiWeekly is one weekly time window.
type ikuaiWeekly struct {
	Type      string `json:"type"`
	Weekdays  string `json:"weekdays"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// ikuaiNATForm is a new nat_rule.
type ikuaiNATForm struct {
	ID         *int64          `json:"id,omitempty"`
	Enabled    string          `json:"enabled"`
	Tagname    string          `json:"tagname"`
	Action     string          `json:"action"`
	Protocol   string          `json:"protocol"`
	IInterface string          `json:"iinterface"`
	OInterface string          `json:"ointerface"`
	SrcAddr    ikuaiAddressSet `json:"src_addr"`
	DstAddr    ikuaiAddressSet `json:"dst_addr"`
	SrcPort    ikuaiAddressSet `json:"src_port"`
	DstPort    ikuaiAddressSet `json:"dst_port"`
	NATAddr    string          `json:"nat_addr"`
	NATPort    string          `json:"nat_port"`
	SrcAddrInv int             `json:"src_addr_inv"`
	DstAddrInv int             `json:"dst_addr_inv"`
	Comment    string          `json:"comment"`
}

func (f *ikuaiNATForm) formTag() string { return f.Tagname }
func (f *ikuaiNATForm) formID() string  { return "" }

// ikuaiPolicyForm is a new 端口分流 (stream_ipport) rule as the 4.0.308 web
// UI submits it: a field left out would reset the rule's other settings.
type ikuaiPolicyForm struct {
	ID         *int64          `json:"id,omitempty"`
	Enabled    string          `json:"enabled"`
	Tagname    string          `json:"tagname"`
	Interface  string          `json:"interface"`
	SrcAddr    ikuaiAddressSet `json:"src_addr"`
	DstAddr    ikuaiAddressSet `json:"dst_addr"`
	IfaceBand  int             `json:"iface_band"`
	Comment    string          `json:"comment"`
	Time       ikuaiSchedule   `json:"time"`
	Prio       int             `json:"prio"`
	Mode       int             `json:"mode"`
	Type       int             `json:"type"`
	Nexthop    string          `json:"nexthop"`
	AreaCode   string          `json:"area_code"`
	SrcPort    ikuaiAddressSet `json:"src_port"`
	DstPort    ikuaiAddressSet `json:"dst_port"`
	Protocol   string          `json:"protocol"`
	DstType    int             `json:"dst_type"`
	SrcAddrInv int             `json:"src_addr_inv"`
	DstAddrInv int             `json:"dst_addr_inv"`
}

func (f *ikuaiPolicyForm) formTag() string { return f.Tagname }
func (f *ikuaiPolicyForm) formID() string  { return "" }

// ikuaiRouteForm is a new static_rt route.
type ikuaiRouteForm struct {
	Enabled   string `json:"enabled"`
	Tagname   string `json:"tagname"`
	IPType    string `json:"ip_type"`
	Interface string `json:"interface"`
	DstAddr   string `json:"dst_addr"`
	Gateway   string `json:"gateway"`
	Netmask   string `json:"netmask"`
	Prio      int    `json:"prio"`
	Comment   string `json:"comment"`
}

func (f *ikuaiRouteForm) formTag() string { return f.Tagname }
func (f *ikuaiRouteForm) formID() string  { return "" }
