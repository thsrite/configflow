package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/netipx"
	"configflow/internal/routerdoc"
)

func fixedIKFields(row ikuai.Row) (Row, error) {
	if row == nil {
		return nil, device.Error(messageFixedIKFields)
	}
	for key := range row {
		if !slices.Contains(ikuai.IkuaiDHCPStaticFields, key) && !slices.Contains([]string{"termname", "ip_addr_int", "hostname"}, key) {
			return nil, device.Error(messageFixedIKFields)
		}
	}
	if id, ok := ikuai.JSONInt(row["id"]); !ok || id < 1 {
		return nil, device.Error(messageFixedIKFields)
	}
	result := Row{}
	for _, key := range ikuai.IkuaiDHCPStaticFields {
		if key == "id" {
			continue
		}
		text, ok := row.Str(key)
		if !ok {
			return nil, device.Error(messageFixedIKFields)
		}
		result[key] = text
	}
	if !slices.Contains([]string{"yes", "no"}, result.Text("enabled")) || routerdoc.IPv4(result.Text("ip_addr")) == "" || fixedMAC(result.Text("mac")) == "" {
		return nil, device.Error(messageFixedIKFields)
	}
	result["mac"] = fixedMAC(result.Text("mac"))
	return result, nil
}
func (d *Ikuai) fixedStatic(ctx context.Context) ([]ikuai.Row, error) {
	rows, err := d.ListAll(ctx, "dhcp_static", "static_", true, "")
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		decoded := ikuai.DecodeRow(row)
		if decoded == nil {
			return nil, device.Error(messageFixedIKFields)
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
		servers[i] = ikuai.DecodeRow(row)
	}
	static, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	leases, err := d.ListAll(ctx, "dhcp_lease", "", false, "")
	if err != nil {
		return nil, err
	}
	return &FixedIPSnapshot{Vendor: "ikuai", ikuaiServers: servers, ikuaiStatic: static, ikuaiLeases: leases}, nil
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
	for _, row := range s.ikuaiServers {
		status, ok := ikuai.JSONInt(row["status"])
		relay, has := ikuai.JSONInt(row["check_relay_only"])
		if !row.Is("enabled", "yes") || !ok || status != 1 || !has || relay != 0 || row.Text("interface") == "" {
			continue
		}
		a, b, found := strings.Cut(row.Text("addr_pool"), "-")
		if !found {
			continue
		}
		start, e1 := netip.ParseAddr(a)
		end, e2 := netip.ParseAddr(b)
		gateway, e3 := netip.ParseAddr(row.Text("gateway"))
		network, valid := netipx.ParseNetwork4(a+"/"+row.Text("netmask"), false)
		if e1 != nil || e2 != nil || e3 != nil || !valid || start.Compare(end) > 0 || !network.Contains(end) || !network.Contains(gateway) || !network.Contains(address) || address == network.Addr() || address == netipx.Broadcast(network) || address == gateway {
			continue
		}
		out = append(out, fixedIKScope{device.JSON(row["id"]), row.Text("interface"), gateway.String(), network.String(), row.Text("addr_pool"), row.Text("netmask")})
	}
	return out
}
func fixedIKBinding(row ikuai.Row) *FixedIPBinding {
	return &FixedIPBinding{Vendor: "ikuai", ID: fixedIKIDText(row["id"]), IP: row.Text("ip_addr"), MAC: fixedMAC(row.Text("mac")), Comment: row.Text("comment"), Interface: fixedPointer(row.Text("interface")), Gateway: fixedPointer(row.Text("gateway"))}
}
func fixedIKMatches(row ikuai.Row, b *FixedIPBinding) bool {
	return row != nil && b != nil && b.ID != "" && b.IP != "" && b.MAC != "" && fixedText(b.Interface) != "" && fixedText(b.Gateway) != "" && device.JSONEqual(fixedIKBinding(row), &FixedIPBinding{Vendor: b.Vendor, ID: b.ID, IP: b.IP, MAC: b.MAC, Comment: b.Comment, Interface: b.Interface, Gateway: b.Gateway})
}
func inspectFixedIK(s *FixedIPSnapshot, ip, mac string) FixedIPState {
	out := FixedIPState{}
	deny := func(message string) FixedIPState { out.Reason = message; return out }
	sameMAC := []ikuai.Row{}
	for _, row := range s.ikuaiStatic {
		if routerdoc.IPv4(row.Text("ip_addr")) == ip && fixedMAC(row.Text("mac")) != mac {
			return deny(messageFixedOtherMAC)
		}
		if fixedMAC(row.Text("mac")) == mac {
			sameMAC = append(sameMAC, row)
		}
	}
	if len(sameMAC) > 0 {
		for _, row := range sameMAC {
			if routerdoc.IPv4(row.Text("ip_addr")) == ip {
				out.Fixed = out.Fixed || row.Is("enabled", "yes")
				if len(sameMAC) == 1 && row.Text("interface") != "" && row.Text("gateway") != "" && strings.HasPrefix(row.Text("comment"), fixedIPPrefix) {
					out.Owned = true
					out.Binding = fixedIKBinding(row)
				}
			}
		}
		return deny(messageFixedIKStatic)
	}
	leases := []ikuai.Row{}
	for _, row := range s.ikuaiLeases {
		if routerdoc.IPv4(row.Text("ip_addr")) == ip || fixedMAC(row.Text("mac")) == mac {
			if routerdoc.IPv4(row.Text("ip_addr")) != ip || fixedMAC(row.Text("mac")) != mac {
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
		if row.Text("interface") != scopes[0].Interface {
			return deny(messageFixedIKInterface)
		}
	}
	out.Eligible = true
	out.Scope = device.JSON(scopes[0])
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
		return nil, device.KindError(KindConflict, state.Reason)
	}
	ip, mac = routerdoc.IPv4(ip), fixedMAC(mac)
	name = strings.TrimSpace(device.OrText(name, ip))
	if utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return nil, device.Error(messageFixedNameLong)
	}
	var scope fixedIKScope
	_ = jsondecode.Unmarshal(state.Scope, &scope)
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
			if routerdoc.IPv4(row.Text("ip_addr")) == ip && routerdoc.NormalizeMAC(row.Text("mac")) == mac {
				return true, nil
			}
		}
	}
	return false, nil
}

func fixedIKIDText(value any) string {
	id, ok := ikuai.JSONInt(value)
	if !ok || id < 1 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
