package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/netipx"
	"configflow/internal/routerdoc"
)

var fixedROSRuntime = stringSet(".id", "dynamic", "status", "last-seen", "expires-after", "host-name", "age", "class-id", "active-address", "active-mac-address", "active-client-id", "active-server", "active-agent-circuit-id", "active-agent-remote-id", "agent-circuit-id", "agent-remote-id", "radius", "blocked")

func fixedROSFields(row Row) Row {
	if row == nil {
		return nil
	}
	out := Row{}
	for key, value := range row {
		if !fixedROSRuntime[key] && !strings.HasPrefix(key, ".") {
			out[key] = value
		}
	}
	if !out.has("comment") {
		out["comment"] = ""
	}
	if !out.has("disabled") {
		out["disabled"] = "false"
	}
	for _, key := range []string{"disabled", "always-broadcast", "block-access", "use-src-mac", "allow-dual-stack-queue", "insert-queue-before"} {
		if v, ok := out[key].(bool); ok {
			out[key] = fmt.Sprint(v)
		}
	}
	if out.has("mac-address") {
		out["mac-address"] = routerdoc.NormalizeMAC(out.text("mac-address"))
	}
	return out
}
func fixedROSID(value string) error {
	if !rosIDPattern.MatchString(value) {
		return routerError(messageFixedROSID)
	}
	return nil
}
func fixedROSBinding(row Row) (*FixedIPBinding, error) {
	if err := fixedROSID(row.id()); err != nil {
		return nil, err
	}
	return &FixedIPBinding{Vendor: "routeros", ID: row.id(), IP: routerdoc.IPv4(row.text("address")), MAC: fixedMAC(row.text("mac-address")), Comment: row.text("comment"), Server: fixedPointer(row.text("server"))}, nil
}
func fixedROSMatches(row Row, b *FixedIPBinding) bool {
	return row != nil && b != nil && b.ID != "" && row.id() == b.ID && !row.flag("dynamic") && routerdoc.IPv4(row.text("address")) == b.IP && fixedMAC(row.text("mac-address")) == b.MAC && row.text("comment") == b.Comment && row.text("server") == fixedText(b.Server)
}
func fixedROSCheck(row Row, b *FixedIPBinding, disabled bool) error {
	if !fixedROSMatches(row, b) || row.flag("disabled") != disabled {
		return routerError(messageFixedROSCheck)
	}
	return nil
}

// ReadFixedIP reads complete RouterOS write evidence, refusing missing or duplicate IDs.
func (d *RouterOS) ReadFixedIP(ctx context.Context) (*FixedIPSnapshot, error) {
	out := &FixedIPSnapshot{Vendor: "routeros"}
	var err error
	for _, item := range []struct {
		path   string
		target *[]Row
	}{{rosDHCPServers, &out.Servers}, {rosDHCPNetworks, &out.Networks}, {"/ip/address", &out.Addresses}, {"/ip/dhcp-server/lease", &out.Leases}} {
		*item.target, err = d.list(ctx, item.path)
		if err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	for _, row := range out.Leases {
		if err = fixedROSID(row.id()); err != nil {
			return nil, err
		}
		if seen[row.id()] {
			return nil, routerError(messageFixedROSDuplicate)
		}
		seen[row.id()] = true
	}
	return out, nil
}

type fixedROSScope struct {
	Server    string `json:"server"`
	Interface string `json:"interface"`
	Network   string `json:"network"`
	ServerID  string `json:"server_id"`
	NetworkID string `json:"network_id"`
	Gateway   string `json:"gateway"`
	Connected string `json:"connected"`
}

func fixedROSScopes(s *FixedIPSnapshot, ip string) []fixedROSScope {
	address, _ := netip.ParseAddr(ip)
	candidates := []fixedROSScope{}
	prefix := -1
	for _, server := range s.Servers {
		if server.text("name") == "" || server.text("interface") == "" || server.flag("disabled", "invalid") || orText(server.text("relay"), "0.0.0.0") != "0.0.0.0" {
			continue
		}
		for _, own := range s.Addresses {
			if own.text("interface") != server.text("interface") || own.flag("disabled", "invalid") {
				continue
			}
			connected, ok := netipx.ParseNetwork4(own.text("address"), false)
			if !ok || !connected.Contains(address) || address == connected.Addr() || address == netipx.Broadcast(connected) {
				continue
			}
			for _, network := range s.Networks {
				if network.flag("disabled", "invalid") {
					continue
				}
				subnet, ok := netipx.ParseNetwork4(network.text("address"), false)
				if !ok || !subnet.Contains(address) {
					continue
				}
				item := fixedROSScope{server.text("name"), server.text("interface"), subnet.String(), server.id(), network.id(), network.text("gateway"), connected.String()}
				if !slices.Contains(candidates, item) {
					candidates = append(candidates, item)
				}
				prefix = max(prefix, subnet.Bits())
			}
		}
	}
	return slices.DeleteFunc(candidates, func(v fixedROSScope) bool { p, _ := netip.ParsePrefix(v.Network); return p.Bits() != prefix })
}

// InspectFixedIP derives reservation controls without changing the snapshot.
func InspectFixedIP(s *FixedIPSnapshot, ip, mac string) FixedIPState {
	out := FixedIPState{}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	deny := func(message string) FixedIPState { out.Reason = message; return out }
	if ip == "" {
		return deny(messageFixedIPv4)
	}
	if mac == "" {
		return deny(messageFixedMAC)
	}
	if s == nil {
		return deny(messageFixedSnapshot)
	}
	if s.Vendor == "ikuai" {
		return inspectFixedIK(s, ip, mac)
	}
	sameMAC, sameIP := []Row{}, []Row{}
	for _, row := range s.Leases {
		if row.flag("dynamic") {
			continue
		}
		if fixedMAC(row.text("mac-address")) == mac {
			sameMAC = append(sameMAC, row)
		}
		if routerdoc.IPv4(row.text("address")) == ip {
			sameIP = append(sameIP, row)
		}
	}
	for _, row := range sameIP {
		if fixedMAC(row.text("mac-address")) != mac {
			return deny(messageFixedOtherMAC)
		}
	}
	if len(sameMAC) > 0 {
		exact := []Row{}
		for _, row := range sameMAC {
			if routerdoc.IPv4(row.text("address")) == ip {
				exact = append(exact, row)
			}
		}
		row := sameMAC[0]
		if len(exact) > 0 {
			row = exact[0]
			out.Fixed = !row.flag("disabled")
		}
		out.Lease = maps.Clone(row)
		out.Owned = len(exact) > 0 && len(sameMAC) == 1 && strings.HasPrefix(row.text("comment"), fixedIPPrefix)
		if out.Owned {
			out.Binding, _ = fixedROSBinding(row)
		}
		return deny(messageFixedROSStatic)
	}
	for _, row := range s.Addresses {
		address, _, _ := strings.Cut(row.text("address"), "/")
		if routerdoc.IPv4(address) == ip {
			return deny(messageFixedRouterAddress)
		}
	}
	dynamic := []Row{}
	for _, row := range s.Leases {
		if row.flag("dynamic") && (routerdoc.IPv4(row.text("address")) == ip || fixedMAC(row.text("mac-address")) == mac) {
			if routerdoc.IPv4(row.text("address")) != ip || fixedMAC(row.text("mac-address")) != mac {
				return deny(messageFixedLeaseChanged)
			}
			dynamic = append(dynamic, row)
		}
	}
	if len(dynamic) > 1 {
		return deny(messageFixedDynamicDuplicate)
	}
	var lease Row
	if len(dynamic) > 0 {
		lease = dynamic[0]
	}
	out.Dynamic = lease != nil
	out.Lease = maps.Clone(lease)
	scopes := fixedROSScopes(s, ip)
	if lease != nil {
		scopes = slices.DeleteFunc(scopes, func(v fixedROSScope) bool { return v.Server != lease.text("server") })
	}
	if len(scopes) == 0 {
		return deny(messageFixedROSScope)
	}
	if len(scopes) != 1 {
		return deny(messageFixedAmbiguous)
	}
	if lease != nil && lease.flag("radius", "disabled", "blocked") {
		return deny(messageFixedROSManaged)
	}
	out.Eligible = true
	out.Scope = mustPolicyJSON(scopes[0])
	return out
}

// PreviewFixedIP reads and previews one RouterOS reservation, without I/O writes.
func (d *RouterOS) PreviewFixedIP(ctx context.Context, ip, mac, name string) (*FixedIPOperation, error) {
	snapshot, err := d.ReadFixedIP(ctx)
	if err != nil {
		return nil, err
	}
	state := InspectFixedIP(snapshot, ip, mac)
	if !state.Eligible {
		return nil, routerError(state.Reason)
	}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	name = strings.TrimSpace(orText(name, ip))
	if strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return nil, routerError(messageFixedNameControl)
	}
	var scope fixedROSScope
	_ = json.Unmarshal(state.Scope, &scope)
	op := &FixedIPOperation{Kind: "add", IP: ip, MAC: mac, Name: fixedPointer(name), Comment: fixedIPPrefix + name, Server: fixedPointer(scope.Server), Scope: state.Scope, ID: fixedPointer(""), Summary: fmt.Sprintf(messageFixedROSAddSummary, scope.Server, ip, mac)}
	if state.Lease != nil {
		op.Kind = "make_static"
		op.ID = fixedPointer(state.Lease.id())
		op.Expected = fixedROSFields(state.Lease)
		op.Summary = fmt.Sprintf(messageFixedROSConvertSummary, ip, mac)
	}
	return op, nil
}

// SeenFixedIP rechecks the discovery MAC against fresh RouterOS lease/ARP evidence.
func (d *RouterOS) SeenFixedIP(ctx context.Context, ip, mac string) (bool, error) {
	for _, path := range []string{"/ip/dhcp-server/lease", "/ip/arp"} {
		rows, err := d.list(ctx, path)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if routerdoc.IPv4(orText(row.text("active-address"), row.text("address"))) == ip && routerdoc.NormalizeMAC(orText(row.text("active-mac-address"), row.text("mac-address"))) == mac {
				return true, nil
			}
		}
	}
	return false, nil
}

func fixedIKFields(row Row) (Row, error) {
	if row == nil {
		return nil, routerError(messageFixedIKFields)
	}
	for key := range row {
		if !slices.Contains(IkuaiDHCPStaticFields, key) && !slices.Contains([]string{"termname", "ip_addr_int", "hostname"}, key) {
			return nil, routerError(messageFixedIKFields)
		}
	}
	if id, ok := jsonInt(row["id"]); !ok || id < 1 {
		return nil, routerError(messageFixedIKFields)
	}
	result := Row{}
	for _, key := range IkuaiDHCPStaticFields {
		if key == "id" {
			continue
		}
		text, ok := row.str(key)
		if !ok {
			return nil, routerError(messageFixedIKFields)
		}
		result[key] = text
	}
	if !slices.Contains([]string{"yes", "no"}, result.text("enabled")) || routerdoc.IPv4(result.text("ip_addr")) == "" || fixedMAC(result.text("mac")) == "" {
		return nil, routerError(messageFixedIKFields)
	}
	result["mac"] = fixedMAC(result.text("mac"))
	return result, nil
}
func (d *Ikuai) fixedStatic(ctx context.Context) ([]Row, error) {
	rows, err := d.ListAll(ctx, "dhcp_static", "static_", true, "")
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		decoded := asRow(DecodeIkuaiValue(row, ""))
		if decoded == nil {
			return nil, routerError(messageFixedIKFields)
		}
		rows[i] = decoded
		if _, err = fixedIKFields(rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// ReadFixedIP reads the full static_ allocation view, never the live lease view.
func (d *Ikuai) ReadFixedIP(ctx context.Context) (*FixedIPSnapshot, error) {
	servers, err := d.ListAll(ctx, "dhcp_server", "", false, "")
	if err != nil {
		return nil, err
	}
	for i, row := range servers {
		servers[i] = asRow(DecodeIkuaiValue(row, ""))
	}
	static, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	leases, err := d.ListAll(ctx, "dhcp_lease", "", false, "")
	if err != nil {
		return nil, err
	}
	return &FixedIPSnapshot{Vendor: "ikuai", Servers: servers, Static: static, Leases: leases}, nil
}

type fixedIKScope struct {
	ID        json.RawMessage `json:"id"`
	Interface string          `json:"interface"`
	Gateway   string          `json:"gateway"`
	Network   string          `json:"network"`
	AddrPool  string          `json:"addr_pool"`
	Netmask   string          `json:"netmask"`
}

func fixedIKScopes(s *FixedIPSnapshot, ip string) []fixedIKScope {
	address, _ := netip.ParseAddr(ip)
	out := []fixedIKScope{}
	for _, row := range s.Servers {
		status, ok := jsonInt(row["status"])
		relay, has := jsonInt(row["check_relay_only"])
		if !row.is("enabled", "yes") || !ok || status != 1 || !has || relay != 0 || row.text("interface") == "" {
			continue
		}
		a, b, found := strings.Cut(row.text("addr_pool"), "-")
		if !found {
			continue
		}
		start, e1 := netip.ParseAddr(a)
		end, e2 := netip.ParseAddr(b)
		gateway, e3 := netip.ParseAddr(row.text("gateway"))
		network, valid := netipx.ParseNetwork4(a+"/"+row.text("netmask"), false)
		if e1 != nil || e2 != nil || e3 != nil || !valid || start.Compare(end) > 0 || !network.Contains(end) || !network.Contains(gateway) || !network.Contains(address) || address == network.Addr() || address == netipx.Broadcast(network) || address == gateway {
			continue
		}
		out = append(out, fixedIKScope{mustPolicyJSON(row["id"]), row.text("interface"), gateway.String(), network.String(), row.text("addr_pool"), row.text("netmask")})
	}
	return out
}
func fixedIKBinding(row Row) *FixedIPBinding {
	return &FixedIPBinding{Vendor: "ikuai", ID: fixedIKIDText(row["id"]), IP: row.text("ip_addr"), MAC: fixedMAC(row.text("mac")), Comment: row.text("comment"), Interface: fixedPointer(row.text("interface")), Gateway: fixedPointer(row.text("gateway"))}
}
func fixedIKMatches(row Row, b *FixedIPBinding) bool {
	return row != nil && b != nil && b.ID != "" && jsonEqual(fixedIKBinding(row), &FixedIPBinding{Vendor: b.Vendor, ID: b.ID, IP: b.IP, MAC: b.MAC, Comment: b.Comment, Interface: b.Interface, Gateway: b.Gateway})
}
func inspectFixedIK(s *FixedIPSnapshot, ip, mac string) FixedIPState {
	out := FixedIPState{}
	deny := func(message string) FixedIPState { out.Reason = message; return out }
	sameMAC := []Row{}
	for _, row := range s.Static {
		if routerdoc.IPv4(row.text("ip_addr")) == ip && fixedMAC(row.text("mac")) != mac {
			return deny(messageFixedOtherMAC)
		}
		if fixedMAC(row.text("mac")) == mac {
			sameMAC = append(sameMAC, row)
		}
	}
	if len(sameMAC) > 0 {
		for _, row := range sameMAC {
			if routerdoc.IPv4(row.text("ip_addr")) == ip {
				out.Fixed = out.Fixed || row.is("enabled", "yes")
				if len(sameMAC) == 1 && strings.HasPrefix(row.text("comment"), fixedIPPrefix) {
					out.Owned = true
					out.Binding = fixedIKBinding(row)
				}
			}
		}
		return deny(messageFixedIKStatic)
	}
	leases := []Row{}
	for _, row := range s.Leases {
		if routerdoc.IPv4(row.text("ip_addr")) == ip || fixedMAC(row.text("mac")) == mac {
			if routerdoc.IPv4(row.text("ip_addr")) != ip || fixedMAC(row.text("mac")) != mac {
				return deny(messageFixedLeaseChanged)
			}
			leases = append(leases, row)
		}
	}
	out.Dynamic = len(leases) > 0
	scopes := fixedIKScopes(s, ip)
	if len(scopes) == 0 {
		return deny(messageFixedIKScope)
	}
	if len(scopes) != 1 {
		return deny(messageFixedAmbiguous)
	}
	for _, row := range leases {
		if row.text("interface") != scopes[0].Interface {
			return deny(messageFixedIKInterface)
		}
	}
	out.Eligible = true
	out.Scope = mustPolicyJSON(scopes[0])
	return out
}

// PreviewFixedIP previews an iKuai reservation with intentionally empty DNS.
func (d *Ikuai) PreviewFixedIP(ctx context.Context, ip, mac, name string) (*FixedIPOperation, error) {
	snapshot, err := d.ReadFixedIP(ctx)
	if err != nil {
		return nil, err
	}
	state := InspectFixedIP(snapshot, ip, mac)
	if !state.Eligible {
		return nil, rosError(KindConflict, state.Reason)
	}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	name = strings.TrimSpace(orText(name, ip))
	if utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return nil, routerError(messageFixedNameLong)
	}
	var scope fixedIKScope
	_ = json.Unmarshal(state.Scope, &scope)
	return &FixedIPOperation{Kind: "add", IP: ip, MAC: mac, Name: fixedPointer(name), Scope: state.Scope, Comment: fixedIPPrefix + name, Summary: fmt.Sprintf(messageFixedIKAddSummary, scope.Interface, ip, mac)}, nil
}

// SeenFixedIP verifies the discovery identity against the iKuai device tables.
func (d *Ikuai) SeenFixedIP(ctx context.Context, ip, mac string) (bool, error) {
	for _, path := range []string{"dhcp_lease", "monitor_lanip"} {
		rows, err := d.ListAll(ctx, path, "", false, "")
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if routerdoc.IPv4(row.text("ip_addr")) == ip && routerdoc.NormalizeMAC(row.text("mac")) == mac {
				return true, nil
			}
		}
	}
	return false, nil
}

func fixedIKIDText(value any) string {
	id, ok := jsonInt(value)
	if !ok || id < 1 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
