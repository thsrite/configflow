package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

// The group size bound must never be below what a driver sends with
// encoding/json (HTML escaping off): iKuai refuses larger bodies.
func TestGroupBytesBoundsTheRequest(t *testing.T) {
	notes := map[string]string{}
	var values []string
	for i, note := range []string{"公司", "a<b>&c", "x\"y\\z", "\x01ctl", "emoji \U0001F600", "line\nbreak", " pad ", "　wide", "%;$|`'"} {
		value := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, byte(i), 0}), 24).String()
		values = append(values, value, "1.2.3."+string(rune('1'+i)))
		notes[value] = note
	}
	id := int64(9999999999)
	form := GroupPayload("cf_nat_12", "nat", values, &id, notes)
	form.GroupName = ikuaiEncode(form.GroupName, false)
	for i := range form.GroupValue {
		form.GroupValue[i] = GroupEntry{ikuaiEncode(form.GroupValue[i].IP, false), ikuaiEncode(form.GroupValue[i].Comment, true)}
	}
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"func_name": "route_object", "action": "edit", "param": form}); err != nil {
		t.Fatal(err)
	}
	if got, bound := body.Len()-1, GroupBytes("cf_nat_12", "nat", values, notes); got > bound {
		t.Fatalf("request %d bytes, bound %d", got, bound)
	}
}

func TestIkuaiEncode(t *testing.T) {
	for _, c := range []struct {
		in      string
		comment bool
		want    string
	}{
		{"1.2.3.4/32", false, "1.2.3.4/32"},
		{" a b ", false, "%20a%20b%20"},
		{" a\r\nb\nc\rd ", true, "a%0Ab%0Ac%0Ad"},
		{"a\nb", false, "a%20b"},
		{"50%\"`$&;\\|'", false, "50%25%22%60%24%26%3B%5C%7C%27"},
		{"中　文", true, "中%20文"},
	} {
		if got := ikuaiEncode(c.in, c.comment); got != c.want {
			t.Errorf("ikuaiEncode(%q, %v) = %q, want %q", c.in, c.comment, got, c.want)
		}
	}
}

func TestAddressExcludeCoversTheRest(t *testing.T) {
	a, b := netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("10.1.2.0/24")
	parts := ros.AddressExclude(a, b)
	if len(parts) != 16 {
		t.Fatalf("%d parts", len(parts))
	}
	size := uint64(1) << (32 - b.Bits())
	for i, p := range parts {
		if p.Overlaps(b) || !ros.SubnetOf(p, a) {
			t.Fatalf("part %s", p)
		}
		for _, q := range parts[i+1:] {
			if p.Overlaps(q) {
				t.Fatalf("%s overlaps %s", p, q)
			}
		}
		size += uint64(1) << (32 - p.Bits())
	}
	if size != 1<<24 {
		t.Fatalf("parts cover %d addresses", size)
	}
	if ros.AddressExclude(a, a) != nil {
		t.Fatal("a without a is empty")
	}
}

func TestReportPartsJoinSummaryAndSync(t *testing.T) {
	defer func(old func(*Desired, *RouterState) ([]ReportPart, error)) { ReportParts = old }(ReportParts)
	desired := &Desired{ProxyGateway: "192.168.0.3", ProxyCIDRs: []ProxyCIDR{}, IPv6CIDRs: []ProxyCIDR{},
		Warnings: []string{}, BlockingWarnings: []string{}}
	state := &RouterState{Routes: []*RouteEntry{device.NewRoute("*9", "203.0.113.0/24", "192.168.0.3", true, "cf-old")}}
	ReportParts = func(*Desired, *RouterState) ([]ReportPart, error) {
		return []ReportPart{{Key: "dns_guard", Section: map[string]string{"status": "pending"}, Statuses: []string{"pending"},
			RetireExtra: []string{"*9"}}}, nil
	}
	report, err := BuildReport(desired, state)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(report)
	if report.InSync || report.Summary["pending"] != 1 || report.Summary["extra"] != 0 ||
		!strings.Contains(string(data), `"dns_guard":{"status":"pending"}`) {
		t.Fatalf("report %s", data)
	}
}

func TestNATTargetPrefersGroupedThenMostCustom(t *testing.T) {
	s := ReadGroupRules(GroupsFromRows([]ikuaiRow{{"id": 1.0, "group_id": "G1", "group_name": "cf_nat_1",
		"group_value": []any{map[string]any{"ip": "1.1.1.1", "comment": "cf-mihomo-nat"}}}}),
		[]ikuaiRow{
			{"id": 1.0, "action": "filter", "enabled": "yes", "comment": "cf-a", "dst_addr": map[string]any{"custom": []any{"1.1.1.1", "2.2.2.2"}}},
			{"id": 2.0, "action": "filter", "enabled": "yes", "comment": "cf-b", "dst_addr": map[string]any{"custom": []any{"3.3.3.3", "4.4.4.4"}}},
			{"id": 3.0, "action": "filter", "enabled": "yes", "comment": "cf-c", "dst_addr": map[string]any{"object": []any{map[string]any{"gid": "G1"}}}},
		}, nil)
	a, b, c := &NatBypassEntry{ID: "1"}, &NatBypassEntry{ID: "2"}, &NatBypassEntry{ID: "3"}
	if got := NATTarget(s, []*NatBypassEntry{a, b}); got != a {
		t.Fatalf("tie: %v", got.ID)
	}
	if got := NATTarget(s, []*NatBypassEntry{a, b, c}); got != c {
		t.Fatalf("grouped: %v", got.ID)
	}
	if NATTarget(s, nil) != nil {
		t.Fatal("no managed rule")
	}
}
