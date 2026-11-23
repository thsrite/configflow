package routers

import (
	"configflow/internal/jsondecode"
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

func fixedIKFields(row ikuaiRow) (Row, error) {
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
func (d *Ikuai) fixedStatic(ctx context.Context) ([]ikuaiRow, error) {
	rows, err := d.ListAll(ctx, "dhcp_static", "static_", true, "")
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		decoded := decodeIkuaiRow(row)
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
		servers[i] = decodeIkuaiRow(row)
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
func fixedIKBinding(row ikuaiRow) *FixedIPBinding {
	return &FixedIPBinding{Vendor: "ikuai", ID: fixedIKIDText(row["id"]), IP: row.text("ip_addr"), MAC: fixedMAC(row.text("mac")), Comment: row.text("comment"), Interface: fixedPointer(row.text("interface")), Gateway: fixedPointer(row.text("gateway"))}
}
func fixedIKMatches(row ikuaiRow, b *FixedIPBinding) bool {
	return row != nil && b != nil && b.ID != "" && b.IP != "" && b.MAC != "" && fixedText(b.Interface) != "" && fixedText(b.Gateway) != "" && jsonEqual(fixedIKBinding(row), &FixedIPBinding{Vendor: b.Vendor, ID: b.ID, IP: b.IP, MAC: b.MAC, Comment: b.Comment, Interface: b.Interface, Gateway: b.Gateway})
}
func inspectFixedIK(s *FixedIPSnapshot, ip, mac string) FixedIPState {
	out := FixedIPState{}
	deny := func(message string) FixedIPState { out.Reason = message; return out }
	sameMAC := []ikuaiRow{}
	for _, row := range s.ikuaiStatic {
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
				if len(sameMAC) == 1 && row.text("interface") != "" && row.text("gateway") != "" && strings.HasPrefix(row.text("comment"), fixedIPPrefix) {
					out.Owned = true
					out.Binding = fixedIKBinding(row)
				}
			}
		}
		return deny(messageFixedIKStatic)
	}
	leases := []ikuaiRow{}
	for _, row := range s.ikuaiLeases {
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
