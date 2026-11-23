package fixedip

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
	"configflow/internal/routerdoc"
)

var fixedROSRuntime = device.StringSet(".id", "dynamic", "status", "last-seen", "expires-after", "host-name", "age", "class-id", "active-address", "active-mac-address", "active-client-id", "active-server", "active-agent-circuit-id", "active-agent-remote-id", "agent-circuit-id", "agent-remote-id", "radius", "blocked")

func fixedROSFields(row ros.Row) ros.Row {
	if row == nil {
		return nil
	}
	out := ros.Row{}
	for key, value := range row {
		if !fixedROSRuntime[key] && !strings.HasPrefix(key, ".") {
			out[key] = value
		}
	}
	if !out.Has("comment") {
		out["comment"] = ""
	}
	if !out.Has("disabled") {
		out["disabled"] = "false"
	}
	if out.Has("mac-address") {
		out["mac-address"] = routerdoc.NormalizeMAC(out.Text("mac-address"))
	}
	return out
}
func fixedROSID(value string) error {
	if !ros.IDPattern.MatchString(value) {
		return device.Error(messageFixedROSID)
	}
	return nil
}
func fixedROSBinding(row ros.Row) (*Binding, error) {
	if err := fixedROSID(row.ID()); err != nil {
		return nil, err
	}
	return &Binding{Vendor: "routeros", ID: row.ID(), IP: routerdoc.IPv4(row.Text("address")), MAC: fixedMAC(row.Text("mac-address")), Comment: row.Text("comment"), Server: fixedPointer(row.Text("server"))}, nil
}
func fixedROSMatches(row ros.Row, b *Binding) bool {
	return row != nil && b != nil && b.ID != "" && b.IP != "" && b.MAC != "" && b.Server != nil && *b.Server != "" && row.ID() == b.ID && !row.Flag("dynamic") && routerdoc.IPv4(row.Text("address")) == b.IP && fixedMAC(row.Text("mac-address")) == b.MAC && row.Text("comment") == b.Comment && row.Text("server") == fixedText(b.Server)
}
func fixedROSCheck(row ros.Row, b *Binding, disabled bool) error {
	if !fixedROSMatches(row, b) || row.Flag("disabled") != disabled {
		return device.Error(messageFixedROSCheck)
	}
	return nil
}

// ReadFixedIP reads complete RouterOS write evidence, refusing missing or duplicate IDs.
func (d rosDevice) ReadFixedIP(ctx context.Context) (*Snapshot, error) {
	out := &Snapshot{Vendor: "routeros"}
	var err error
	for _, item := range []struct {
		path   string
		target *[]ros.Row
	}{{ros.PathDHCPServers, &out.Servers}, {ros.PathDHCPNetworks, &out.Networks}, {"/ip/address", &out.Addresses}, {"/ip/dhcp-server/lease", &out.Leases}} {
		*item.target, err = d.List(ctx, item.path)
		if err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	for _, row := range out.Leases {
		if err = fixedROSID(row.ID()); err != nil {
			return nil, err
		}
		if seen[row.ID()] {
			return nil, device.Error(messageFixedROSDuplicate)
		}
		seen[row.ID()] = true
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

func fixedROSScopes(s *Snapshot, ip string) []fixedROSScope {
	address, _ := netip.ParseAddr(ip)
	candidates := []fixedROSScope{}
	prefix := -1
	for _, server := range s.Servers {
		if server.Text("name") == "" || server.Text("interface") == "" || server.Flag("disabled", "invalid") || device.OrText(server.Text("relay"), "0.0.0.0") != "0.0.0.0" {
			continue
		}
		for _, own := range s.Addresses {
			if own.Text("interface") != server.Text("interface") || own.Flag("disabled", "invalid") {
				continue
			}
			connected, ok := netipx.ParseNetwork4(own.Text("address"), false)
			if !ok || !connected.Contains(address) || address == connected.Addr() || address == netipx.Broadcast(connected) {
				continue
			}
			for _, network := range s.Networks {
				if network.Flag("disabled", "invalid") {
					continue
				}
				subnet, ok := netipx.ParseNetwork4(network.Text("address"), false)
				if !ok || !subnet.Contains(address) {
					continue
				}
				item := fixedROSScope{server.Text("name"), server.Text("interface"), subnet.String(), server.ID(), network.ID(), network.Text("gateway"), connected.String()}
				if !slices.Contains(candidates, item) {
					candidates = append(candidates, item)
				}
				prefix = max(prefix, subnet.Bits())
			}
		}
	}
	return slices.DeleteFunc(candidates, func(v fixedROSScope) bool { p, _ := netip.ParsePrefix(v.Network); return p.Bits() != prefix })
}

// Inspect derives reservation controls without changing the snapshot.
func Inspect(s *Snapshot, ip, mac string) State {
	out := State{}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	deny := func(message string) State { out.Reason = message; return out }
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
	sameMAC, sameIP := []ros.Row{}, []ros.Row{}
	for _, row := range s.Leases {
		if row.Flag("dynamic") {
			continue
		}
		if fixedMAC(row.Text("mac-address")) == mac {
			sameMAC = append(sameMAC, row)
		}
		if routerdoc.IPv4(row.Text("address")) == ip {
			sameIP = append(sameIP, row)
		}
	}
	for _, row := range sameIP {
		if fixedMAC(row.Text("mac-address")) != mac {
			return deny(messageFixedOtherMAC)
		}
	}
	if len(sameMAC) > 0 {
		exact := []ros.Row{}
		for _, row := range sameMAC {
			if routerdoc.IPv4(row.Text("address")) == ip {
				exact = append(exact, row)
			}
		}
		row := sameMAC[0]
		if len(exact) > 0 {
			row = exact[0]
			out.Fixed = !row.Flag("disabled")
		}
		out.Lease = maps.Clone(row)
		out.Owned = len(exact) > 0 && len(sameMAC) == 1 && row.Text("server") != "" && strings.HasPrefix(row.Text("comment"), fixedIPPrefix)
		if out.Owned {
			out.Binding, _ = fixedROSBinding(row)
		}
		return deny(messageFixedROSStatic)
	}
	for _, row := range s.Addresses {
		address, _, _ := strings.Cut(row.Text("address"), "/")
		if routerdoc.IPv4(address) == ip {
			return deny(messageFixedRouterAddress)
		}
	}
	dynamic := []ros.Row{}
	for _, row := range s.Leases {
		if row.Flag("dynamic") && (routerdoc.IPv4(row.Text("address")) == ip || fixedMAC(row.Text("mac-address")) == mac) {
			if routerdoc.IPv4(row.Text("address")) != ip || fixedMAC(row.Text("mac-address")) != mac {
				return deny(messageFixedLeaseChanged)
			}
			dynamic = append(dynamic, row)
		}
	}
	if len(dynamic) > 1 {
		return deny(messageFixedDynamicDuplicate)
	}
	var lease ros.Row
	if len(dynamic) > 0 {
		lease = dynamic[0]
	}
	out.Dynamic = lease != nil
	out.Lease = maps.Clone(lease)
	scopes := fixedROSScopes(s, ip)
	if lease != nil {
		scopes = slices.DeleteFunc(scopes, func(v fixedROSScope) bool { return v.Server != lease.Text("server") })
	}
	if len(scopes) == 0 {
		return deny(messageFixedROSScope)
	}
	if len(scopes) != 1 {
		return deny(messageFixedAmbiguous)
	}
	if lease != nil && lease.Flag("radius", "disabled", "blocked") {
		return deny(messageFixedROSManaged)
	}
	out.Eligible = true
	out.Scope = device.JSON(scopes[0])
	return out
}

// PreviewFixedIP reads and previews one RouterOS reservation, without I/O writes.
func (d rosDevice) PreviewFixedIP(ctx context.Context, ip, mac, name string) (*Operation, error) {
	snapshot, err := d.ReadFixedIP(ctx)
	if err != nil {
		return nil, err
	}
	state := Inspect(snapshot, ip, mac)
	if !state.Eligible {
		return nil, device.Error(state.Reason)
	}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	name = strings.TrimSpace(device.OrText(name, ip))
	if strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return nil, device.Error(messageFixedNameControl)
	}
	var scope fixedROSScope
	_ = jsondecode.Unmarshal(state.Scope, &scope)
	op := &Operation{Kind: "add", IP: ip, MAC: mac, Name: fixedPointer(name), Comment: fixedIPPrefix + name, Server: fixedPointer(scope.Server), Scope: state.Scope, ID: fixedPointer(""), Summary: fmt.Sprintf(messageFixedROSAddSummary, scope.Server, ip, mac)}
	if state.Lease != nil {
		op.Kind = "make_static"
		op.ID = fixedPointer(state.Lease.ID())
		op.Expected = fixedROSFields(state.Lease)
		op.Summary = fmt.Sprintf(messageFixedROSConvertSummary, ip, mac)
	}
	return op, nil
}

// SeenFixedIP rechecks the discovery MAC against fresh RouterOS lease/ARP evidence.
func (d rosDevice) SeenFixedIP(ctx context.Context, ip, mac string) (bool, error) {
	for _, path := range []string{"/ip/dhcp-server/lease", "/ip/arp"} {
		rows, err := d.List(ctx, path)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if routerdoc.IPv4(device.OrText(row.Text("active-address"), row.Text("address"))) == ip && routerdoc.NormalizeMAC(device.OrText(row.Text("active-mac-address"), row.Text("mac-address"))) == mac {
				return true, nil
			}
		}
	}
	return false, nil
}
