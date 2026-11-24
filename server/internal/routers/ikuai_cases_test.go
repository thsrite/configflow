package routers_test

import (
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strconv"

	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/routerfake"
)

// Synthetic addresses only (TEST-NET and private bulk ranges).
const (
	ikGateway = "192.0.2.4"  // the mihomo gateway
	ikOther   = "192.0.2.9"  // another next hop
	ikDNS     = "192.0.2.53" // the selected OxiDNS
)

type rows = []routerfake.Row

func intp(n int) *int       { return &n }
func strp(s string) *string { return &s }

func ikMask(bits int) string { return net.IP(net.CIDRMask(bits, 32)).String() }

func ikSubnets(base string, prefix, count int) []string {
	p := netip.MustParsePrefix(base)
	out := make([]string, 0, count)
	addr := p.Addr()
	step := uint32(1) << (32 - prefix)
	for range count {
		out = append(out, netip.PrefixFrom(addr, prefix).String())
		b := addr.As4()
		v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]) + step
		addr = netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	return out
}

func ikRoute(id int, cidr, gateway, comment, tag string) routerfake.Row {
	p := netip.MustParsePrefix(cidr)
	if tag == "" {
		tag = fmt.Sprintf("LB%d", id)
	}
	return routerfake.Row{"id": id, "dst_addr": p.Addr().String(), "netmask": ikMask(p.Bits()), "gateway": gateway,
		"enabled": "yes", "comment": comment, "tagname": tag, "ip_type": "4", "interface": "auto", "prio": 1}
}

func ikManaged(id int, cidr, comment string) routerfake.Row {
	return ikRoute(id, cidr, ikGateway, comment, ikuai.IkuaiTagname("rt", cidr))
}

func ikBulk(count int) rows {
	out := rows{}
	for i, cidr := range ikSubnets("10.100.0.0/16", 28, count) {
		out = append(out, ikRoute(i+1, cidr, ikOther, "lab", ""))
	}
	return out
}

func addressField(custom []string, objects ...map[string]any) map[string]any {
	c := []any{}
	for _, v := range custom {
		c = append(c, v)
	}
	o := []any{}
	for _, v := range objects {
		o = append(o, v)
	}
	return map[string]any{"custom": c, "object": o}
}

func ref(gid, name string) map[string]any {
	return map[string]any{"type": 0, "gid": gid, "gp_name": name}
}

func ikNAT(id int, comment string, dst map[string]any) routerfake.Row {
	return routerfake.Row{"id": id, "action": "filter", "enabled": "yes", "tagname": fmt.Sprintf("NAT_%d", id),
		"comment": comment, "protocol": "any", "iinterface": "any", "ointerface": "any", "nat_addr": "", "nat_port": "",
		"src_addr_inv": 0, "dst_addr_inv": 0,
		"src_addr": map[string]any{"custom": []any{"192.0.2.1-192.0.2.3", "192.0.2.5-192.0.2.255"}, "object": map[string]any{}},
		"dst_addr": dst, "src_port": map[string]any{"custom": map[string]any{}, "object": map[string]any{}},
		"dst_port": map[string]any{"custom": map[string]any{}, "object": map[string]any{}}}
}

func ikPolicy(id int, comment string, dst map[string]any) routerfake.Row {
	return routerfake.Row{"id": id, "enabled": "yes", "tagname": fmt.Sprintf("PR_%d", id), "interface": "",
		"src_addr": map[string]any{"custom": []any{ikGateway}, "object": map[string]any{}}, "dst_addr": dst,
		"iface_band": 0, "comment": comment, "time": map[string]any{"custom": []any{map[string]any{"type": "weekly",
			"weekdays": "1234567", "start_time": "00:00", "end_time": "23:59"}}, "object": []any{}},
		"prio": 0, "mode": 0, "type": 1, "nexthop": ikGateway, "area_code": "", "src_port": map[string]any{},
		"dst_port": map[string]any{}, "protocol": "any", "dst_type": 0, "src_addr_inv": 1, "dst_addr_inv": 0}
}

func ikGroup(id int, name string, marker string, values ...string) routerfake.Row {
	entries := []any{}
	for i, v := range values {
		comment := ""
		if i == 0 {
			comment = marker
		}
		entries = append(entries, map[string]any{"ip": v, "comment": comment})
	}
	return routerfake.Row{"id": id, "group_name": name, "tagname": name, "type": 0, "group_value": entries,
		"group_id": fmt.Sprintf("IPGP%d", id)}
}

// DHCP rows as official 4.0.120 firmware defaults them.
func ikServer(changes routerfake.Row) routerfake.Row {
	row := routerfake.Row{"id": 1, "enabled": "yes", "tagname": "DHS_1", "interface": "lan1", "phy_ifnames": "all",
		"addr_pool": "192.0.2.100-192.0.2.200", "exclude_pool": "", "netmask": "255.255.255.0", "gateway": "192.0.2.1",
		"next_server": "", "dns1": "198.51.100.5", "dns2": "198.51.100.6", "domain": "", "wins1": "", "wins2": "",
		"lease": 120, "delay": 0, "check_addr_valid": 1, "check_relay_only": 0, "status": 1, "available": 73}
	for _, n := range []int{15, 28, 43, 60, 66, 67, 80, 119, 125, 128, 138, 121} {
		row["opt_type"+strconv.Itoa(n)] = 0
		row["opt"+strconv.Itoa(n)] = ""
	}
	for key, value := range changes {
		if value == nil {
			delete(row, key)
		} else {
			row[key] = value
		}
	}
	return row
}

func ikBinding(changes routerfake.Row) routerfake.Row {
	row := routerfake.Row{"id": 5, "enabled": "yes", "tagname": "PC", "interface": "auto", "mac": "02:00:00:00:00:21",
		"gateway": "", "dns1": "", "dns2": "", "ip_addr": "192.0.2.20", "comment": "", "termname": "Computer",
		"hostname": "Computer", "ip_addr_int": 3221225492}
	maps.Copy(row, changes)
	return row
}

var ikDHCPFieldNames = func() []string {
	fields := []string{"id", "enabled", "tagname", "interface", "phy_ifnames", "addr_pool", "exclude_pool", "netmask",
		"gateway", "next_server", "dns1", "dns2", "domain", "wins1", "wins2", "lease", "delay", "check_addr_valid",
		"check_relay_only"}
	for _, n := range []int{15, 28, 43, 60, 66, 67, 80, 119, 125, 128, 138, 121} {
		fields = append(fields, "opt_type"+strconv.Itoa(n), "opt"+strconv.Itoa(n))
	}
	return fields
}()

// dhcpOp is the update a plan builds from the server row as read (decoded).
func dhcpOp(server routerfake.Row, changes map[string]any) map[string]any {
	attributes := map[string]any{}
	decodedRow := ikuai.DecodeIkuaiValue(map[string]any(server), "").(map[string]any)
	for _, key := range ikDHCPFieldNames {
		if value, ok := decodedRow[key]; ok {
			attributes[key] = value
		}
	}
	op := map[string]any{"kind": "dhcp_dns_update", "id": "1", "dns": []any{ikDNS}, "expected": attributes,
		"scope": map[string]any{"id": "1", "network": "192.0.2.0/24", "gateway": ikGateway}}
	for key, value := range changes {
		if value == nil {
			delete(op, key)
		} else {
			op[key] = value
		}
	}
	return op
}

func dhcpState(servers rows, bindings rows, faults ...routerfake.IkuaiFault) routerfake.IkuaiState {
	return routerfake.IkuaiState{Tables: map[string][]routerfake.Row{"dhcp_server": servers, "dhcp_static": bindings},
		Faults: faults}
}

func routeAdd(cidr, comment string) map[string]any {
	return map[string]any{"kind": "route_add", "cidr": cidr, "gateway": ikGateway, "comment": comment}
}

func byID(kind string, id int, extra ...any) map[string]any {
	op := map[string]any{"kind": kind, "id": strconv.Itoa(id)}
	for i := 0; i+1 < len(extra); i += 2 {
		op[extra[i].(string)] = extra[i+1]
	}
	return op
}

func groupOp(kind, name, role string, values []string, expected string) map[string]any {
	op := map[string]any{"kind": kind, "name": name, "role": role, "values": values}
	if expected != "" {
		op["expected"] = expected
	}
	return op
}

func tables(pairs ...any) map[string][]routerfake.Row {
	out := map[string][]routerfake.Row{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i].(string)] = pairs[i+1].(rows)
	}
	return out
}

func fault(funcName, action string, count int) routerfake.IkuaiFault {
	return routerfake.IkuaiFault{Func: funcName, Action: action, Count: count}
}

func withCode(f routerfake.IkuaiFault, code int, message string) routerfake.IkuaiFault {
	f.Code, f.Message = intp(code), message
	return f
}
