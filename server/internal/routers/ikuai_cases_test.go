package routers_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/routers"
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
	return ikRoute(id, cidr, ikGateway, comment, routers.IkuaiTagname("rt", cidr))
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
	decodedRow := routers.DecodeIkuaiValue(map[string]any(server), "").(map[string]any)
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

func ikuaiCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	cases = append(cases, ikuaiSessionCases()...)
	cases = append(cases, ikuaiListCases()...)
	cases = append(cases, ikuaiDHCPCases()...)
	cases = append(cases, ikuaiReleaseCases()...)
	cases = append(cases, ikuaiGroupCases()...)
	cases = append(cases, ikuaiFixedCases()...)
	cases = append(cases, ikuaiReviewCases()...)
	add(ikuaiCase{name: "values_decode_and_tagnames", action: "values", input: map[string]any{
		"values": [][2]string{{"comment", "a%20b%0Ac%0ad%22%60%24%26%3B%5C%7C%27%3b%5c%2F%2f%25%2522"},
			{"tagname", "x%0Ay%20z%2520"}, {"comment", "%E4%B8%AD%20文"}, {"", "%%20%2"}},
		"tags": [][2]string{{"rt", "198.51.100.0/24"}, {"nat", "198.51.100.0/24,203.0.113.0/24"},
			{"pr", "cf-mihomo"}, {"pr", "cf-中文"}}}})
	add(ikuaiCase{name: "oversized_blockers", action: "oversized", input: map[string]any{"ops": oversizedOps()}})
	add(ikuaiCase{name: "request_bytes", action: "request_bytes", input: map[string]any{"items": requestByteItems()}})
	add(ikuaiCase{name: "transport_messages", action: "transport", input: map[string]any{
		"classes": []string{"SSLError", "ConnectTimeout", "ReadTimeout", "ConnectionError", "ChunkedEncodingError", "InvalidURL"}}})
	add(ikuaiCase{name: "transport_restore_timeout_message", action: "transport", input: map[string]any{
		"classes": []string{"ReadTimeout", "ConnectTimeout"}, "timeout": 13.003}})
	return cases
}

func ikuaiSessionCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	full := tables("static_rt", rows{ikRoute(1, "198.51.100.0/24", ikGateway, "x", "")})
	add(ikuaiCase{name: "probe_ok", action: "probe", state: routerfake.IkuaiState{Tables: full}})
	add(ikuaiCase{name: "probe_3x_version", action: "probe", state: routerfake.IkuaiState{Version: "3.7.21"}})
	add(ikuaiCase{name: "probe_missing_capability", action: "probe", state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{withCode(fault("nat_rule", "show", 0), 1001, "no permission")}}})
	add(ikuaiCase{name: "probe_without_verinfo", action: "probe", state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{{Func: "sysstat", Reply: map[string]any{"code": 0, "results": map[string]any{}}}}}})
	add(ikuaiCase{name: "login_wrong_password", action: "probe", input: map[string]any{"password": "wrong"}})
	add(ikuaiCase{name: "login_3x_failure", action: "probe", state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "login", Reply: map[string]any{"Result": 30000, "ErrMsg": "旧版错误"}}}}})
	add(ikuaiCase{name: "login_3x_failure_without_text", action: "probe", state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "login", Reply: map[string]any{"Result": 30000}}}}})
	add(ikuaiCase{name: "login_without_code_succeeds", action: "probe", state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "login", Reply: map[string]any{"Result": 10000}}}}})
	add(ikuaiCase{name: "login_code_with_result_10000_fails", action: "probe", state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{withCode(fault("login", "", 0), 1007, "")}}})
	for _, status := range []int{301, 302, 303, 307, 308} {
		add(ikuaiCase{name: fmt.Sprintf("redirect_to_https_%d", status), action: "probe", state: routerfake.IkuaiState{
			Faults: []routerfake.IkuaiFault{{Func: "login", Status: status, Location: "https://127.0.0.1/Action/login"}}}})
	}
	for i, location := range []string{"https://127.0.0.1:8443/login?token=secret-token",
		"https://user:secret-pass@127.0.0.1:443/secret-path#secret-fragment", "https://[2001:db8::1]:8443/login",
		"//router.example:8080/login", "HTTPS://Router.Example:08443/x", "https://router.example:99999/x",
		"", "/login?token=secret-token", "../login", "{origin}/login",
		"https://router.example:bad-port/login", "javascript:secret-token", "ftp://router.example/x"} {
		tc := ikuaiCase{name: fmt.Sprintf("redirect_location_%d", i), action: "probe", state: routerfake.IkuaiState{
			Faults: []routerfake.IkuaiFault{{Func: "login", Status: 302, Location: location}}}}
		if i == 10 { // Both reject a malformed port; Go identifies malformed response data.
			tc.fixed = "malformed Location has an explicit native response error"
			tc.check = func(t *testing.T, py, goResult ikuaiOutcome, pyState, goState routerfake.IkuaiState) {
				wantFixed(t, py.Error != nil && py.Error.Class == "RouterError" && strings.Contains(py.Error.Message, "HTTP 302"), "Python: %s", outcomeText(py))
				wantFixed(t, goResult.Error != nil && goResult.Error.Message == "iKuai 重定向地址格式无法识别，请在浏览器核对管理地址、协议和端口。", "Go: %s", outcomeText(goResult))
				compareIkuai(t, "requests", pyState.Calls, goState.Calls)
				compareIkuai(t, "tables", pyState.Tables, goState.Tables)
			}
		}
		add(tc)
	}
	add(ikuaiCase{name: "redirect_after_login", action: "probe", state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{{Func: "sysstat", Status: 302, Location: "/login"}}}})
	for _, status := range []int{401, 403, 404, 405, 408, 429, 500, 502, 503, 504, 400, 418, 201, 599} {
		add(ikuaiCase{name: fmt.Sprintf("http_status_%d", status), action: "probe", state: routerfake.IkuaiState{
			Faults: []routerfake.IkuaiFault{{Func: "login", Status: status, Reply: map[string]any{"message": "secret-server-content"}}}}})
	}
	for name, body := range map[string]string{"empty": "", "whitespace": " \r\n\t", "html": "<html>login</html>",
		"array": `[{"code":0}]`, "string": `"ok"`, "number": "0", "truncated": `{"code":0`} {
		add(ikuaiCase{name: "reply_body_" + name, action: "probe", state: routerfake.IkuaiState{
			Faults: []routerfake.IkuaiFault{{Func: "sysstat", RawBody: strp(body)}}}})
	}
	// /Action/call failures (codes as observed on 4.0.314).
	for i, reply := range []map[string]any{
		{"Result": 10000, "code": 1001, "message": "没有权限", "Label": "静态路由"},
		{"code": 1001, "Label": ""}, {"code": 1002, "Label": "端口分流"}, {"code": 1003, "message": "Access denied"},
		{"code": 1003}, {"code": 3001, "message": "参数错误"}, {"code": 3001}, {"code": 77, "message": ""},
		{"Result": 30000, "Data": []any{}}, {"Result": 10000}, {"ErrMsg": "x"}, {"code": nil},
		{"code": 0, "results": []any{}}, {"code": 0}, {"code": "0", "results": map[string]any{}},
	} {
		add(ikuaiCase{name: fmt.Sprintf("call_reply_%d", i), action: "call", input: map[string]any{"func": "static_rt",
			"param": map[string]any{"TYPE": "total,data", "limit": "0,1"}},
			state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "static_rt", Reply: reply}}}})
	}
	// Session expiry: one new login and one retry.
	expire := routerfake.IkuaiFault{Func: "static_rt", Count: 1, Code: intp(1008), Message: "session expired", Logout: true}
	add(ikuaiCase{name: "show_relogin_once", action: "call", state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{expire}},
		input: map[string]any{"func": "static_rt", "param": map[string]any{"TYPE": "total,data", "limit": "0,1"}}})
	twice := expire
	twice.Count = 2
	add(ikuaiCase{name: "show_1008_twice", action: "call", state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{expire, twice}},
		input: map[string]any{"func": "static_rt", "param": map[string]any{"TYPE": "total,data", "limit": "0,1"}}})
	route := ikRoute(0, "198.51.100.0/24", ikOther, "a b", "T1")
	delete(route, "id")
	writeInput := map[string]any{"func": "static_rt", "write_action": "add", "param": route}
	add(ikuaiCase{name: "write_relogin_once", action: "write", input: writeInput,
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, Code: intp(1008), Logout: true}}}})
	add(ikuaiCase{name: "write_1008_twice", action: "write", input: writeInput, state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "static_rt", Action: "add", Count: 1, Code: intp(1008), Logout: true},
		{Func: "static_rt", Action: "add", Count: 2, Code: intp(1008), Logout: true}}}})
	add(ikuaiCase{name: "write_session_lost_relogin_fails", action: "write", input: writeInput, state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "static_rt", Action: "add", Count: 1, Code: intp(1008), Logout: true}, withCode(fault("login", "", 2), 1005, "账号或密码错误")}}})
	add(ikuaiCase{name: "write_refused_with_details", action: "write", input: writeInput, state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "static_rt", Action: "add", Code: intp(3001), Message: "请求参数不合法",
			Details: rows{{"field": "tagname", "msg": "名称已存在，请重新输入"}, {"field": "prio", "msg": "范围 1-99"}}}}}})
	add(ikuaiCase{name: "write_refused_code_only", action: "write", input: writeInput, state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{withCode(fault("static_rt", "add", 0), 2, "")}}})
	add(ikuaiCase{name: "write_refused_permission", action: "write", input: writeInput, state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{
		{Func: "static_rt", Action: "add", Code: intp(1002), Label: "静态路由"}}}})
	add(ikuaiCase{name: "write_duplicate_name_refused_by_device", action: "write_twice",
		input: map[string]any{"func": "static_rt", "write_action": "add", "param": route, "param2": route}})
	add(ikuaiCase{name: "write_unknown_field_refused_by_device", action: "write",
		input: map[string]any{"func": "static_rt", "write_action": "add", "param": map[string]any{"tagname": "T2", "status": "up"}}})
	special := maps.Clone(route)
	special["comment"] = "  café \"q\" `b` $v & ; \\ | ' %25 100%\r\nnext\nline\ttab  "
	special["tagname"] = "T a$b"
	add(ikuaiCase{name: "write_encodes_like_web_ui", action: "write", input: map[string]any{"func": "static_rt",
		"write_action": "add", "param": special}})
	// backend/test_router_release.py test_encode_matches_ikuai_ui and test_ikuai_error_details_surface_in_message.
	add(ikuaiCase{name: "write_encode_matches_web_ui_test", action: "write", input: map[string]any{"func": "nat_rule",
		"write_action": "add", "param": map[string]any{"comment": " cf:Unraid SS\nnext ", "gateway": "192.0.2.4", "prio": 1,
			"dst_addr": map[string]any{"custom": []any{"a b"}, "object": []any{}}, "x": "100% \"a\";$b"}}})
	add(ikuaiCase{name: "write_error_details_surface", action: "write", input: map[string]any{"func": "static_rt",
		"write_action": "add", "param": map[string]any{"tagname": "", "dst_addr": "192.0.2.0"}}})
	huge := maps.Clone(route)
	huge["comment"] = strings.Repeat("长", 20000)
	add(ikuaiCase{name: "write_oversized_body_gets_empty_reply", action: "write", input: map[string]any{"func": "static_rt",
		"write_action": "add", "param": huge}})
	add(ikuaiCase{name: "write_lost_reply", action: "write", input: writeInput, state: routerfake.IkuaiState{
		Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", LoseReply: true}}}})
	return cases
}

func ikuaiListCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	add(ikuaiCase{name: "read_state_every_page", action: "read_state", state: routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(2500))}})
	add(ikuaiCase{name: "read_state_rows_change_between_pages", action: "read_state", state: routerfake.IkuaiState{
		Tables: tables("static_rt", ikBulk(1500)), Mutations: []routerfake.IkuaiMutation{{Func: "static_rt", Count: 2, Table: "static_rt", Pop: true}}}})
	add(ikuaiCase{name: "read_state_unreadable_active_table", action: "read_state", state: routerfake.IkuaiState{
		Tables: tables("static_rt", ikBulk(1500)),
		Stale:  rows{{"id": 9000, "dst_addr": "198.51.100.8", "netmask": "255.255.255.255", "gateway": ikGateway, "interface": "lan1", "prio": 20}},
		Mutations: []routerfake.IkuaiMutation{{Func: "static_rt_table", Count: 2, Table: "stale",
			Append: routerfake.Row{"id": 9001, "dst_addr": "198.51.100.9", "netmask": "255.255.255.255", "gateway": ikGateway}}}}})
	add(ikuaiCase{name: "read_state_no_active_table", action: "read_state", state: routerfake.IkuaiState{
		Tables: tables("static_rt", ikBulk(3)), NoActiveTable: true}})
	for _, n := range []int{0, 1, 999, 1000, 1001, 2000} {
		add(ikuaiCase{name: fmt.Sprintf("list_all_%d_rows", n), action: "list_all", state: routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(n))},
			input: map[string]any{"func": "static_rt"}})
	}
	for name, bad := range map[string]rows{
		"duplicate_ids": {ikRoute(1, "198.51.100.0/24", ikOther, "", ""), ikRoute(1, "198.51.100.16/28", ikOther, "", "")},
		"zero_id":       {ikRoute(0, "198.51.100.0/24", ikOther, "", "")},
		"string_id":     {{"id": "1", "dst_addr": "198.51.100.0"}},
		"fraction_id":   {{"id": 1.5}},
		"bool_id":       {{"id": true}},
		"missing_id":    {{"dst_addr": "198.51.100.0"}},
	} {
		add(ikuaiCase{name: "list_all_" + name, action: "list_all", state: routerfake.IkuaiState{Tables: tables("static_rt", bad)},
			input: map[string]any{"func": "static_rt"}})
	}
	for name, results := range map[string]map[string]any{
		"total_bool":      {"total": true, "data": []any{}},
		"total_negative":  {"total": -1, "data": []any{}},
		"total_string":    {"total": "1", "data": []any{map[string]any{"id": 1}}},
		"total_fraction":  {"total": json.Number("1.0"), "data": []any{map[string]any{"id": 1}}},
		"data_not_list":   {"total": 0, "data": map[string]any{}},
		"short_page":      {"total": 2, "data": []any{map[string]any{"id": 1}}},
		"long_page":       {"total": 1, "data": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}},
		"row_not_object":  {"total": 1, "data": []any{"x"}},
		"missing_total":   {"data": []any{}},
		"truncated_empty": {"total": 1, "data": []any{}},
	} {
		for _, ids := range []bool{true, false} {
			add(ikuaiCase{name: fmt.Sprintf("list_all_%s_ids_%v", name, ids), action: "list_all",
				state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "route_object",
					Reply: map[string]any{"code": 0, "results": results}}}},
				input: map[string]any{"func": "route_object", "ids": ids}})
		}
	}
	bindings := rows{}
	for i := 1; i <= 1500; i++ {
		bindings = append(bindings, routerfake.Row{"id": i, "ip_addr": fmt.Sprintf("10.1.%d.%d", i/250, i%250)})
	}
	add(ikuaiCase{name: "list_all_dhcp_static_pages", action: "list_all", state: routerfake.IkuaiState{Tables: tables("dhcp_static", bindings)},
		input: map[string]any{"func": "dhcp_static", "prefix": "static_", "ids": false}})
	add(ikuaiCase{name: "read_state_details", action: "read_state", state: routerfake.IkuaiState{
		Tables: tables(
			"static_rt", rows{
				ikRoute(1, "198.18.0.0/16", ikGateway, "fake%20ip%E4%B8%AD%zz", ""),
				{"id": 2, "dst_addr": "198.51.100.7", "netmask": "255.255.255.0", "gateway": ikGateway, "enabled": "no", "comment": "host bits", "prio": 1},
				{"id": 3, "dst_addr": "198.51.100.0", "netmask": "255.255.255.0 (24)", "gateway": ikGateway, "enabled": "yes", "prio": 1},
				{"id": 4, "dst_addr": "2001:db8::", "netmask": "64", "gateway": "2001:db8::1", "enabled": "yes", "comment": "v6", "prio": 2},
				{"id": 5, "dst_addr": "203.0.113.0", "netmask": 24, "gateway": 7, "enabled": "yes", "prio": 3},
				{"id": 6, "dst_addr": "203.0.113.0", "netmask": "0.0.0.255", "enabled": "yes", "prio": 4},
				{"id": 7},
			},
			"route_object", rows{
				ikGroup(10, "cf_pa_1", "cf-mihomo", "198.51.100.0/24", "203.0.113.0/24"),
				ikGroup(11, "cf_nat_1", "", "198.51.100.0/24"),
				ikGroup(12, "office", "my%20note", "192.0.2.0/25"),
				ikGroup(13, "dup", "", "192.0.2.128/25"),
				ikGroup(14, "dup", "", "192.0.2.192/26"),
				{"group_name": "noid", "group_id": "", "group_value": []any{map[string]any{"ip": "198.51.100.77"}, "bad", map[string]any{"comment": "x"}}},
			},
			"nat_rule", rows{
				ikNAT(1, "cf-mihomo%20nat", addressField([]string{"198.18.0.0/16"}, ref("IPGP11", "cf_nat_1"), ref("", "office"), ref("IPGP99", "gone"))),
				{"id": 2, "action": "dnat", "enabled": "no", "comment": "dns"},
				ikNAT(3, "user", map[string]any{"custom": map[string]any{}, "object": []any{ref("", "dup"), "bad"}}),
			},
			"stream_ipport", rows{
				ikPolicy(1, "cf-mihomo", addressField(nil, ref("IPGP10", "cf_pa_1"))),
				ikPolicy(2, "cf-mihomo-manual", addressField([]string{"198.51.100.0/24"})),
				{"id": 3, "enabled": "no", "comment": "wan%20line", "type": 0, "nexthop": "wan1", "prio": "5", "src_addr_inv": 0},
			}),
		Stale: rows{{"id": 9000, "dst_addr": "198.51.100.8", "netmask": "255.255.255.255", "gateway": ikGateway, "interface": "lan1", "prio": 20},
			{"id": 9001, "dst_addr": "bad", "netmask": "", "gateway": ikGateway}},
	}})
	cases[len(cases)-1].fixed = "Python silently omitted the static_rt mask display suffix"
	cases[len(cases)-1].check = checkIkuaiSuffixState
	return cases
}

func ikuaiDHCPCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	base := ikServer(nil)
	add(ikuaiCase{name: "dhcp_read_full", action: "read_dhcp_dns", state: dhcpState(
		rows{ikServer(routerfake.Row{"opt43": "01:02", "opt121": "10.8.0.0/16%2010.0.0.9", "opt_type121": 2})}, rows{ikBinding(nil)})})
	original := ikServer(routerfake.Row{"exclude_pool": "192.0.2.123", "lease": 360, "delay": 3, "domain": "home%20%26%20office",
		"opt43": "01:02", "opt121": "10.8.0.0/16%2010.0.0.9", "opt_type121": 2})
	second := ikServer(routerfake.Row{"id": 2, "addr_pool": "198.51.100.100-198.51.100.200", "gateway": "198.51.100.1"})
	add(ikuaiCase{name: "dhcp_update_and_undo_complete_form", action: "roundtrip", state: dhcpState(rows{original, second}, nil),
		input: map[string]any{"op": dhcpOp(original, nil)}})
	for i, values := range [][2]string{{ikDNS, ""}, {"", ikDNS}, {"", ""}} {
		server := ikServer(routerfake.Row{"dns1": values[0], "dns2": values[1]})
		add(ikuaiCase{name: fmt.Sprintf("dhcp_blank_slot_%d", i), action: "apply", state: dhcpState(rows{server}, nil),
			input: map[string]any{"op": dhcpOp(server, nil)}})
		add(ikuaiCase{name: fmt.Sprintf("dhcp_blank_slot_read_%d", i), action: "read_dhcp_dns", state: dhcpState(rows{server}, nil)})
	}
	edit := func(f routerfake.IkuaiFault) routerfake.IkuaiFault {
		f.Func, f.Action = "dhcp_server", "edit"
		return f
	}
	for name, faults := range map[string][]routerfake.IkuaiFault{
		"swallowed":       {edit(routerfake.IkuaiFault{Count: 1, Swallow: true})},
		"corrupted":       {edit(routerfake.IkuaiFault{Count: 1, Corrupt: routerfake.Row{"lease": 1}})},
		"lost_reply":      {edit(routerfake.IkuaiFault{Count: 1, LoseReply: true})},
		"refused":         {edit(routerfake.IkuaiFault{Count: 1, Code: intp(2), Message: "请求参数不合法"})},
		"dropped":         {edit(routerfake.IkuaiFault{Count: 1, Drop: true})},
		"rollback_refuse": {edit(routerfake.IkuaiFault{Count: 1, LoseReply: true}), edit(routerfake.IkuaiFault{Count: 2, Code: intp(2), Message: "请求参数不合法"})},
		"rollback_corrupt": {edit(routerfake.IkuaiFault{Count: 1, LoseReply: true}),
			edit(routerfake.IkuaiFault{Count: 2, Corrupt: routerfake.Row{"lease": 2}})},
		"readback_fails_twice": {{Func: "dhcp_server", Action: "show", Count: 2, Drop: true}, {Func: "dhcp_server", Action: "show", Count: 3, Drop: true}},
		"reads_down": func() []routerfake.IkuaiFault {
			var faults []routerfake.IkuaiFault
			for n := 2; n < 12; n++ {
				faults = append(faults, routerfake.IkuaiFault{Func: "dhcp_server", Action: "show", Count: n, Drop: true})
			}
			return faults
		}(),
		"static_read_fails_after_write": {{Func: "dhcp_static", Action: "show", Count: 2, Code: intp(1001)}},
	} {
		add(ikuaiCase{name: "dhcp_failure_" + name, action: "apply", state: dhcpState(rows{base}, nil, faults...),
			input: map[string]any{"op": dhcpOp(base, nil)}})
	}
	readsDown := []routerfake.IkuaiFault{}
	for n := 3; n < 12; n++ {
		readsDown = append(readsDown, routerfake.IkuaiFault{Func: "dhcp_server", Action: "show", Count: n, Drop: true})
	}
	add(ikuaiCase{name: "dhcp_undo_with_reads_down", action: "roundtrip", state: dhcpState(rows{base}, nil, readsDown...),
		input: map[string]any{"op": dhcpOp(base, nil)}})
	add(ikuaiCase{name: "dhcp_restore_record_incomplete", action: "apply", state: dhcpState(rows{base}, nil),
		input: map[string]any{"op": map[string]any{"kind": "dhcp_dns_restore", "record": map[string]any{"id": 1, "dns1": ""}}}})
	add(ikuaiCase{name: "dhcp_restore_record_missing", action: "apply", state: dhcpState(rows{base}, nil),
		input: map[string]any{"op": map[string]any{"kind": "dhcp_dns_restore"}}})
	for i, change := range []routerfake.Row{{"gateway": "192.0.2.2"}, {"dns2": "198.51.100.8"}, {"opt43": "01"}} {
		drifted := ikServer(change)
		add(ikuaiCase{name: fmt.Sprintf("dhcp_preview_drift_%d", i), action: "apply", state: dhcpState(rows{drifted}, nil),
			input: map[string]any{"op": dhcpOp(base, nil)}})
	}
	add(ikuaiCase{name: "dhcp_expected_missing", action: "apply", state: dhcpState(rows{base}, nil),
		input: map[string]any{"op": dhcpOp(base, map[string]any{"expected": nil})}})
	add(ikuaiCase{name: "dhcp_id_changed", action: "apply", state: dhcpState(rows{base}, nil),
		input: map[string]any{"op": dhcpOp(base, map[string]any{"id": "2"})}})
	add(ikuaiCase{name: "dhcp_overlap", action: "apply", state: dhcpState(rows{base, ikServer(routerfake.Row{"id": 2, "tagname": "overlap"})}, nil),
		input: map[string]any{"op": dhcpOp(base, nil)}})
	for i, change := range []routerfake.Row{{"opt6": "08:08:08:08"}, {"radius": "yes"}, {"dns1": "auto"}, {"check_relay_only": 1},
		{"phy_ifnames": "eth1"}, {"status": 0}, {"lease": "120"}, {"enabled": "unknown"}, {"addr_pool": "192.0.2.100-198.51.100.200"},
		{"addr_pool": "192.0.2.200-192.0.2.100"}, {"addr_pool": "192.0.2.100"}, {"gateway": "198.51.100.1"}, {"netmask": "bad"},
		{"status": true}, {"status": nil}, {"check_relay_only": nil}, {"id": "1"}, {"lease": 1.5}, {"dns2": "198.51.100.5"},
		{"opt119": nil}, {"netmask": "24"}, {"enabled": "no"}} {
		server := ikServer(change)
		add(ikuaiCase{name: fmt.Sprintf("dhcp_blocker_%d", i), action: "read_dhcp_dns", state: dhcpState(rows{server}, nil)})
		add(ikuaiCase{name: fmt.Sprintf("dhcp_blocker_write_%d", i), action: "apply", state: dhcpState(rows{server}, nil),
			input: map[string]any{"op": dhcpOp(server, nil)}})
	}
	for i, binding := range []routerfake.Row{{"dns1": "198.51.100.8"}, {"dns2": "198.51.100.9"}, {"ip_addr": ""}, {"ip_addr": 1},
		{"enabled": "unknown"}, {"opt6": "08:08:08:08"}, {"dns1": "198.51.100.8", "enabled": "no"},
		{"dns1": "198.51.100.8", "ip_addr": "198.51.100.20"}, {}, {"mac": 5}, {"comment": nil}} {
		b := ikBinding(binding)
		if binding["comment"] == nil {
			if _, ok := binding["comment"]; ok {
				delete(b, "comment")
			}
		}
		add(ikuaiCase{name: fmt.Sprintf("dhcp_binding_%d", i), action: "apply", state: dhcpState(rows{base}, rows{b}),
			input: map[string]any{"op": dhcpOp(base, nil)}})
	}
	add(ikuaiCase{name: "dhcp_binding_without_network", action: "read_dhcp_dns",
		state: dhcpState(rows{ikServer(routerfake.Row{"netmask": "bad"})}, rows{ikBinding(routerfake.Row{"dns1": "198.51.100.8"})})})
	for _, funcName := range []string{"dhcp_server", "dhcp_static"} {
		add(ikuaiCase{name: "dhcp_incomplete_" + funcName, action: "read_dhcp_dns", state: dhcpState(rows{base}, nil,
			routerfake.IkuaiFault{Func: funcName, Reply: map[string]any{"code": 0, "results": map[string]any{"data": []any{},
				"total": 1, "static_data": []any{}, "static_total": 1}}})})
	}
	for i, dns := range []any{[]any{}, []any{ikDNS, "198.51.100.8"}, []any{"127.0.0.1"}, []any{"0.0.0.0"}, []any{"224.0.0.1"},
		[]any{"255.255.255.255"}, []any{"host.example"}, []any{1}, []any{true}, "192.0.2.53", nil} {
		add(ikuaiCase{name: fmt.Sprintf("dhcp_invalid_dns_%d", i), action: "apply", state: dhcpState(rows{base}, nil),
			input: map[string]any{"op": dhcpOp(base, map[string]any{"dns": dns})}})
	}
	for i, scope := range []any{nil, map[string]any{}, map[string]any{"id": "1"},
		map[string]any{"id": "1", "gateway": ikGateway, "network": nil}, []any{"1"}, "x",
		map[string]any{"id": "1", "gateway": "198.51.100.4", "network": "192.0.2.0/24"},
		map[string]any{"id": "1", "gateway": ikGateway, "network": "192.0.2.0/25"}} {
		add(ikuaiCase{name: fmt.Sprintf("dhcp_scope_%d", i), action: "apply", state: dhcpState(rows{base}, nil),
			input: map[string]any{"op": dhcpOp(base, map[string]any{"scope": scope})}})
	}
	return cases
}

func ikuaiReleaseCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	release := func(name string, state routerfake.IkuaiState, ops []map[string]any, extra ...any) {
		input := map[string]any{"ops": ops}
		for i := 0; i+1 < len(extra); i += 2 {
			input[extra[i].(string)] = extra[i+1]
		}
		add(ikuaiCase{name: name, action: "release", state: state, input: input})
	}
	adds := func(cidrs []string, comment string) []map[string]any {
		var ops []map[string]any
		for _, cidr := range cidrs {
			ops = append(ops, routeAdd(cidr, comment))
		}
		return ops
	}
	cidrs300 := ikSubnets("10.200.0.0/16", 28, 300)
	release("routes_add_300_one_readback", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(1500))}, adds(cidrs300, "cf-lab"))
	managed := ikBulk(1500)
	var deletes []map[string]any
	for i, cidr := range cidrs300 {
		managed = append(managed, ikManaged(1501+i, cidr, "cf-lab"))
		deletes = append(deletes, byID("route_delete", 1501+i))
	}
	release("routes_delete_300_one_snapshot", routerfake.IkuaiState{Tables: tables("static_rt", managed)}, deletes)
	manual := ikBulk(1500)
	var comments []map[string]any
	for i, cidr := range ikSubnets("10.200.0.0/16", 28, 50) {
		manual = append(manual, ikRoute(1501+i, cidr, ikGateway, fmt.Sprintf("manual %d", i), ""))
		comments = append(comments, byID("route_comment", 1501+i, "comment", fmt.Sprintf("cf-manual %d", i)))
	}
	release("routes_adopt_reads_each_row_fresh", routerfake.IkuaiState{Tables: tables("static_rt", manual)}, comments)
	twoManual := append(ikBulk(5), ikRoute(11, "198.51.100.0/24", ikGateway, "a", ""), ikRoute(12, "203.0.113.0/24", ikGateway, "b", ""))
	release("routes_adopt_keeps_hand_change", routerfake.IkuaiState{Tables: tables("static_rt", twoManual),
		Mutations: []routerfake.IkuaiMutation{{Func: "static_rt", Action: "edit", Count: 1, OnWrite: true, Table: "static_rt", ID: 12, Set: routerfake.Row{"enabled": "no"}}}},
		[]map[string]any{byID("route_comment", 11, "comment", "cf-a"), byID("route_comment", 12, "comment", "cf-b")}, "always_rollback", true)
	swallow := routerfake.IkuaiFault{Func: "static_rt", Action: "add", Swallow: true}
	policies := rows{ikPolicy(1, "cf-mihomo", addressField([]string{"198.51.100.0/24"})), ikPolicy(2, "cf-mihomo-manual", addressField([]string{"203.0.113.0/24"}))}
	release("policies_kept_until_routes_exist", routerfake.IkuaiState{Tables: tables("stream_ipport", policies), Faults: []routerfake.IkuaiFault{swallow}},
		append(adds([]string{"198.51.100.0/25", "198.51.100.128/25", "203.0.113.0/25", "203.0.113.128/25"}, "cf-mihomo"),
			byID("policy_route_delete", 1), byID("policy_route_delete", 2)))
	narrow := ikManaged(50, "198.51.100.0/26", "cf-lab")
	release("covered_route_kept_until_wider_exists", routerfake.IkuaiState{Tables: tables("static_rt", append(ikBulk(5), narrow)), Faults: []routerfake.IkuaiFault{swallow}},
		[]map[string]any{routeAdd("198.51.100.0/24", "cf-lab"), byID("route_delete", 50)})
	add100 := ikSubnets("10.200.0.0/16", 28, 100)
	old100 := ikBulk(1500)
	ops := adds(add100, "cf-lab")
	for i, cidr := range ikSubnets("10.220.0.0/16", 28, 100) {
		old100 = append(old100, ikManaged(1501+i, cidr, "cf-lab"))
		ops = append(ops, byID("route_delete", 1501+i))
	}
	release("new_routes_read_once_before_deletes", routerfake.IkuaiState{Tables: tables("static_rt", old100)}, ops)
	mixed := ikBulk(5)
	var mixedOps []map[string]any
	for i, cidr := range ikSubnets("10.210.0.0/16", 28, 3) {
		mixed = append(mixed, ikRoute(11+i, cidr, ikGateway, "hand", ""))
		mixedOps = append(mixedOps, byID("route_comment", 11+i, "comment", "cf-hand"))
	}
	for i, cidr := range ikSubnets("10.220.0.0/16", 28, 3) {
		mixed = append(mixed, ikManaged(21+i, cidr, "cf-old"))
		mixedOps = append(mixedOps, byID("route_delete", 21+i))
	}
	mixedOps = append(adds(ikSubnets("10.200.0.0/16", 28, 4), "cf-lab"), mixedOps...)
	release("mixed_stage_batched", routerfake.IkuaiState{Tables: tables("static_rt", mixed)}, mixedOps)
	release("mixed_stage_unbatched", routerfake.IkuaiState{Tables: tables("static_rt", mixed)}, mixedOps, "batch", false)
	failNAT := withCode(fault("nat_rule", "add", 0), 2, "参数错误")
	natAdd := map[string]any{"kind": "nat_add", "src": []string{"192.0.2.1-192.0.2.3"}, "dst": cidrs300, "comment": "cf-mihomo"}
	release("rollback_removes_300_routes_one_snapshot", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(1500)), Faults: []routerfake.IkuaiFault{failNAT}},
		append(adds(cidrs300, "cf-lab"), natAdd))
	release("route_added_not_read_back_is_rolled_back", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(1200)),
		Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "show", Count: 1, Drop: true}}}, adds(ikSubnets("10.200.0.0/16", 28, 3), "cf-lab"))
	release("route_reply_lost_is_rolled_back", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(10)),
		Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 2, LoseReply: true}}}, adds(ikSubnets("10.200.0.0/16", 28, 3), "cf-lab"))
	release("refused_route_needs_no_undo", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(10)),
		Faults: []routerfake.IkuaiFault{withCode(fault("static_rt", "add", 0), 2, "参数错误")}}, adds(ikSubnets("10.200.0.0/16", 28, 3), "cf-lab"))
	release("lost_request_without_route_is_nothing_to_undo", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(10)),
		Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Drop: true}}}, adds(ikSubnets("10.200.0.0/16", 28, 1), "cf-lab"))
	release("route_changed_after_add_not_deleted", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(10)), Faults: []routerfake.IkuaiFault{failNAT},
		Mutations: []routerfake.IkuaiMutation{{Func: "stream_ipport", Action: "add", Count: 1, OnWrite: true, Table: "static_rt", ID: 100, Set: routerfake.Row{"gateway": "192.0.2.99"}}}},
		[]map[string]any{routeAdd("198.51.100.0/24", "cf-lab"),
			{"kind": "policy_route_add", "gateway": ikGateway, "dst": []string{"203.0.113.0/24"}, "comment": "cf-mihomo"}, natAdd})
	release("policy_added_not_read_back_is_rolled_back", routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "stream_ipport", Action: "show", Count: 1, Drop: true}}},
		[]map[string]any{{"kind": "policy_route_add", "gateway": ikGateway, "dst": []string{"198.51.100.0/24"}, "comment": "cf-mihomo"}})
	release("nat_reply_lost_is_rolled_back", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(5)),
		Faults: []routerfake.IkuaiFault{{Func: "nat_rule", Action: "add", LoseReply: true}}},
		append(adds(ikSubnets("10.200.0.0/16", 28, 2), "cf-lab"), natAdd))
	twoManaged := append(ikBulk(10), ikManaged(11, "198.51.100.0/24", "cf-lab"), ikManaged(12, "203.0.113.0/24", "cf-lab"))
	release("rollback_reports_readds_it_cannot_read_back", routerfake.IkuaiState{Tables: tables("static_rt", twoManaged),
		Faults: []routerfake.IkuaiFault{failNAT, swallow}}, []map[string]any{byID("route_delete", 11), byID("route_delete", 12), natAdd})
	release("swallowed_add_alone_fails_at_batch_end", routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{swallow}},
		adds([]string{"198.51.100.0/24"}, "cf-lab"))
	release("unbatched_route_add_proven_at_once", routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{swallow}},
		adds([]string{"198.51.100.0/24", "203.0.113.0/24"}, "cf-lab"), "batch", false)
	release("route_add_and_rollback_by_name", routerfake.IkuaiState{Tables: tables("static_rt", ikBulk(3))},
		adds([]string{"198.51.100.0/24", "203.0.113.0/24"}, "cf-lab"), "always_rollback", true)
	// Single operations and their undo.
	nat := ikNAT(1, "mihomo", addressField([]string{"198.18.0.0/16", "198.51.100.0/24"}))
	natTables := tables("nat_rule", rows{nat}, "static_rt", twoManaged)
	add(ikuaiCase{name: "nat_update_literal_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": byID("nat_update", 1, "add_dst", []string{"203.0.113.0/24", "198.51.100.0/24", "203.0.113.0/24"}, "comment", "cf-mihomo")}})
	add(ikuaiCase{name: "nat_update_missing_rule", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": byID("nat_update", 9, "add_dst", []string{}, "comment", "cf-mihomo")}})
	add(ikuaiCase{name: "nat_add_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: natTables}, input: map[string]any{"op": natAdd}})
	add(ikuaiCase{name: "nat_delete_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: natTables}, input: map[string]any{"op": byID("nat_delete", 1)}})
	add(ikuaiCase{name: "route_delete_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: natTables}, input: map[string]any{"op": byID("route_delete", 11)}})
	add(ikuaiCase{name: "route_comment_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": byID("route_comment", 11, "comment", "cf-new note")}})
	add(ikuaiCase{name: "route_delete_missing", action: "apply", state: routerfake.IkuaiState{Tables: natTables}, input: map[string]any{"op": byID("route_delete", 404)}})
	add(ikuaiCase{name: "route_delete_numeric_id", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": map[string]any{"kind": "route_delete", "id": 12}}})
	pol := ikPolicy(3, "cf-mihomo", addressField([]string{"198.51.100.0/24"}))
	pol["src_port"] = map[string]any{"custom": []any{"80"}, "object": map[string]any{}}
	polTables := tables("stream_ipport", rows{pol})
	add(ikuaiCase{name: "policy_update_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: polTables},
		input: map[string]any{"op": byID("policy_route_update", 3, "gateway", ikGateway, "dst", []string{"198.51.100.0/24", "203.0.113.0/24"}, "comment", "cf-mihomo")}})
	add(ikuaiCase{name: "policy_add_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: polTables},
		input: map[string]any{"op": map[string]any{"kind": "policy_route_add", "gateway": ikGateway, "dst": []string{"203.0.113.0/24"}, "comment": "cf-mihomo-manual"}}})
	add(ikuaiCase{name: "policy_delete_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: polTables}, input: map[string]any{"op": byID("policy_route_delete", 3)}})
	add(ikuaiCase{name: "policy_add_duplicate_name_refused", action: "apply", state: routerfake.IkuaiState{Tables: tables("stream_ipport",
		rows{func() routerfake.Row {
			r := ikPolicy(3, "x", addressField(nil))
			r["tagname"] = routers.IkuaiTagname("pr", "cf-mihomo")
			return r
		}()})},
		input: map[string]any{"op": map[string]any{"kind": "policy_route_add", "gateway": ikGateway, "dst": []string{"203.0.113.0/24"}, "comment": "cf-mihomo"}}})
	many := make([]any, 1200)
	for i := range many {
		many[i] = fmt.Sprintf("10.%d.%d.0/24", i/250, i%250)
	}
	bigPolicy := ikPolicy(3, "cf-mihomo", map[string]any{"custom": many, "object": []any{}})
	add(ikuaiCase{name: "policy_restore_many_literal_entries", action: "apply", state: routerfake.IkuaiState{Tables: polTables},
		input: map[string]any{"op": map[string]any{"kind": "policy_route_restore", "record": bigPolicy}}})
	add(ikuaiCase{name: "unsupported_kind", action: "apply", input: map[string]any{"op": map[string]any{"kind": "route_teleport"}}})
	add(ikuaiCase{name: "add_undo_other_func_by_name", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": map[string]any{"kind": "add_undo", "func": "nat_rule", "match": map[string]any{"tagname": "NAT_1", "comment": "mihomo"}, "confirmed": true}}})
	add(ikuaiCase{name: "add_undo_changed_object_kept", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": map[string]any{"kind": "add_undo", "func": "nat_rule", "match": map[string]any{"tagname": "NAT_1", "comment": "other"}, "confirmed": true}}})
	add(ikuaiCase{name: "add_undo_gone_confirmed", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": map[string]any{"kind": "add_undo", "func": "nat_rule", "match": map[string]any{"tagname": "cf_nat_x"}, "confirmed": true}}})
	add(ikuaiCase{name: "add_undo_gone_unconfirmed", action: "apply", state: routerfake.IkuaiState{Tables: natTables},
		input: map[string]any{"op": map[string]any{"kind": "add_undo", "func": "static_rt", "match": map[string]any{"tagname": "cf_rt_x"}, "confirmed": false}}})
	return cases
}

func ikuaiGroupCases() []ikuaiCase {
	var cases []ikuaiCase
	add := func(c ikuaiCase) { cases = append(cases, c) }
	values := []string{"198.51.100.0/24", "203.0.113.0/24"}
	digest := routers.GroupDigest(values)
	groupAdd := groupOp("ikuai_group_add", "cf_pa_1", "pa", values, "")
	policyAdd := map[string]any{"kind": "policy_route_add", "gateway": ikGateway, "dst": values, "comment": "cf-mihomo", "groups": []string{"cf_pa_1"}}
	add(ikuaiCase{name: "group_add_policy_refs_and_rollback", action: "release", input: map[string]any{
		"ops": []map[string]any{groupAdd, policyAdd}, "always_rollback": true}})
	add(ikuaiCase{name: "group_add_swallowed_then_unreadable", action: "release",
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "route_object", Action: "add", Swallow: true}}},
		input: map[string]any{"ops": []map[string]any{groupAdd, policyAdd}}})
	add(ikuaiCase{name: "group_add_lost_reply", action: "release",
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "route_object", Action: "add", LoseReply: true}}},
		input: map[string]any{"ops": []map[string]any{groupAdd, policyAdd}}})
	add(ikuaiCase{name: "group_add_refused", action: "release",
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{withCode(fault("route_object", "add", 0), 3001, "请求参数不合法")}},
		input: map[string]any{"ops": []map[string]any{groupAdd, policyAdd}}})
	add(ikuaiCase{name: "group_add_exists", action: "apply", state: routerfake.IkuaiState{Tables: tables("route_object", rows{ikGroup(5, "cf_pa_1", "", "192.0.2.0/24")})},
		input: map[string]any{"op": groupAdd}})
	add(ikuaiCase{name: "group_add_invalid_role", action: "apply", input: map[string]any{"op": groupOp("ikuai_group_add", "cf_pa_1", "nat", values, "")}})
	add(ikuaiCase{name: "group_add_invalid_name", action: "apply", input: map[string]any{"op": groupOp("ikuai_group_add", "office", "pa", values, "")}})
	notes := groupOp("ikuai_group_add", "cf_nat_2", "nat", values, "")
	notes["notes"] = map[string]string{"198.51.100.0/24": "家里 \"NAS\" & more"}
	add(ikuaiCase{name: "group_add_with_notes", action: "apply", input: map[string]any{"op": notes}})
	owned := ikGroup(5, "cf_pa_1", "cf-mihomo", "192.0.2.0/25", "192.0.2.128/25")
	owned["group_value"].([]any)[1].(map[string]any)["comment"] = "user%20note"
	oldDigest := routers.GroupDigest([]string{"192.0.2.0/25", "192.0.2.128/25"})
	ownedTables := tables("route_object", rows{owned}, "stream_ipport", rows{ikPolicy(3, "cf-mihomo", addressField(nil, ref("IPGP5", "cf_pa_1")))})
	update := groupOp("ikuai_group_update", "cf_pa_1", "pa", values, oldDigest)
	add(ikuaiCase{name: "group_update_and_restore", action: "release", state: routerfake.IkuaiState{Tables: ownedTables},
		input: map[string]any{"ops": []map[string]any{update}, "always_rollback": true}})
	add(ikuaiCase{name: "group_update_drift", action: "apply", state: routerfake.IkuaiState{Tables: ownedTables},
		input: map[string]any{"op": groupOp("ikuai_group_update", "cf_pa_1", "pa", values, digest)}})
	add(ikuaiCase{name: "group_update_lost_reply_restores", action: "release", state: routerfake.IkuaiState{Tables: ownedTables,
		Faults: []routerfake.IkuaiFault{{Func: "route_object", Action: "edit", Count: 1, LoseReply: true}}},
		input: map[string]any{"ops": []map[string]any{update}}})
	add(ikuaiCase{name: "group_restore_kept_after_hand_edit", action: "release", state: routerfake.IkuaiState{Tables: ownedTables,
		Mutations: []routerfake.IkuaiMutation{{Func: "route_object", Action: "edit", Count: 1, OnWrite: true, Table: "route_object", ID: 5,
			Set: routerfake.Row{"group_value": []any{map[string]any{"ip": "192.0.2.1", "comment": "cf-mihomo"}}}}}},
		input: map[string]any{"ops": []map[string]any{update}, "always_rollback": true}})
	unmarked := ikGroup(5, "cf_pa_1", "", "192.0.2.0/25")
	adoptTables := tables("route_object", rows{unmarked}, "stream_ipport", rows{ikPolicy(3, "cf-mihomo", addressField(nil, ref("IPGP5", "cf_pa_1")))})
	mark := groupOp("ikuai_group_update", "cf_pa_1", "pa", []string{"192.0.2.0/25"}, routers.GroupDigest([]string{"192.0.2.0/25"}))
	mark["mark"] = true
	add(ikuaiCase{name: "group_mark_adopted", action: "apply", state: routerfake.IkuaiState{Tables: adoptTables}, input: map[string]any{"op": mark}})
	add(ikuaiCase{name: "group_unmarked_update_refused", action: "apply", state: routerfake.IkuaiState{Tables: adoptTables},
		input: map[string]any{"op": groupOp("ikuai_group_update", "cf_pa_1", "pa", values, routers.GroupDigest([]string{"192.0.2.0/25"}))}})
	add(ikuaiCase{name: "group_delete_in_use", action: "apply", state: routerfake.IkuaiState{Tables: ownedTables},
		input: map[string]any{"op": groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, oldDigest)}})
	hidden := maps.Clone(owned)
	hidden["hidden_refs"] = 1
	add(ikuaiCase{name: "group_delete_hidden_reference_refused_by_device", action: "apply", state: routerfake.IkuaiState{Tables: tables("route_object", rows{hidden})},
		input: map[string]any{"op": groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, oldDigest)}})
	add(ikuaiCase{name: "group_delete_and_readd", action: "release", state: routerfake.IkuaiState{Tables: tables("route_object", rows{owned})},
		input: map[string]any{"ops": []map[string]any{groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, oldDigest)}, "always_rollback": true}})
	add(ikuaiCase{name: "group_delete_lost_reply_readds", action: "release", state: routerfake.IkuaiState{Tables: tables("route_object", rows{owned}),
		Faults: []routerfake.IkuaiFault{{Func: "route_object", Action: "del", LoseReply: true}}},
		input: map[string]any{"ops": []map[string]any{groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, oldDigest)}}})
	add(ikuaiCase{name: "group_readd_taken", action: "apply", state: routerfake.IkuaiState{Tables: tables("route_object", rows{ikGroup(7, "cf_pa_1", "", "192.0.2.0/24")})},
		input: map[string]any{"op": map[string]any{"kind": "ikuai_group_readd", "name": "cf_pa_1", "role": "pa", "values": values}}})
	add(ikuaiCase{name: "group_readd_already_back", action: "apply", state: routerfake.IkuaiState{Tables: tables("route_object", rows{owned})},
		input: map[string]any{"op": map[string]any{"kind": "ikuai_group_readd", "name": "cf_pa_1", "role": "pa",
			"values": []string{"192.0.2.0/25", "192.0.2.128/25"}}}})
	add(ikuaiCase{name: "group_undo_changed", action: "apply", state: routerfake.IkuaiState{Tables: ownedTables},
		input: map[string]any{"op": map[string]any{"kind": "ikuai_group_undo", "name": "cf_pa_1", "role": "pa", "expected": digest, "confirmed": true}}})
	add(ikuaiCase{name: "group_undo_in_use", action: "apply", state: routerfake.IkuaiState{Tables: ownedTables},
		input: map[string]any{"op": map[string]any{"kind": "ikuai_group_undo", "name": "cf_pa_1", "role": "pa", "expected": oldDigest, "confirmed": true}}})
	add(ikuaiCase{name: "group_undo_gone_confirmed", action: "apply",
		input: map[string]any{"op": map[string]any{"kind": "ikuai_group_undo", "name": "cf_pa_1", "role": "pa", "expected": oldDigest, "confirmed": true}}})
	add(ikuaiCase{name: "group_unknown_kind", action: "apply", input: map[string]any{"op": map[string]any{"kind": "ikuai_group_merge", "name": "cf_pa_1", "role": "pa"}}})
	foreign := tables("route_object", rows{ikGroup(5, "cf_pa_1", "", "192.0.2.0/24")})
	add(ikuaiCase{name: "group_foreign_same_name_not_ours", action: "apply", state: routerfake.IkuaiState{Tables: foreign},
		input: map[string]any{"op": groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, routers.GroupDigest([]string{"192.0.2.0/24"}))}})
	dup := tables("route_object", rows{ikGroup(5, "cf_pa_1", "cf-mihomo", "192.0.2.0/24"), ikGroup(6, "cf_pa_1", "cf-mihomo", "192.0.2.0/24")})
	add(ikuaiCase{name: "group_duplicate_names_not_ours", action: "apply", state: routerfake.IkuaiState{Tables: dup},
		input: map[string]any{"op": groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, routers.GroupDigest([]string{"192.0.2.0/24"}))}})
	noID := ikGroup(5, "cf_pa_1", "cf-mihomo", "192.0.2.0/24")
	delete(noID, "id")
	add(ikuaiCase{name: "group_without_id_not_edited", action: "apply", state: routerfake.IkuaiState{Tables: tables("route_object", rows{noID})},
		input: map[string]any{"op": groupOp("ikuai_group_update", "cf_pa_1", "pa", values, routers.GroupDigest([]string{"192.0.2.0/24"}))}})
	// NAT and 端口分流 rules referencing groups.
	natGroups := tables("route_object", rows{ikGroup(5, "cf_nat_1", "cf-mihomo-nat", "198.51.100.0/24"), ikGroup(6, "office", "", "192.0.2.0/24")},
		"nat_rule", rows{ikNAT(1, "cf-mihomo", addressField([]string{"203.0.113.0/24"}, ref("IPGP6", "office"), ref("IPGP5", "cf_nat_1")))})
	add(ikuaiCase{name: "nat_update_groups_keep_foreign", action: "roundtrip", state: routerfake.IkuaiState{Tables: natGroups},
		input: map[string]any{"op": byID("nat_update", 1, "comment", "cf-mihomo", "groups", []string{"cf_nat_1"}, "keep_foreign", true)}})
	add(ikuaiCase{name: "nat_update_groups_drop_foreign", action: "apply", state: routerfake.IkuaiState{Tables: natGroups},
		input: map[string]any{"op": byID("nat_update", 1, "comment", "cf-mihomo", "groups", []string{"cf_nat_1"})}})
	add(ikuaiCase{name: "nat_update_unknown_group", action: "apply", state: routerfake.IkuaiState{Tables: natGroups},
		input: map[string]any{"op": byID("nat_update", 1, "comment", "cf-mihomo", "groups", []string{"cf_nat_2"})}})
	add(ikuaiCase{name: "nat_add_with_groups", action: "roundtrip", state: routerfake.IkuaiState{Tables: natGroups},
		input: map[string]any{"op": map[string]any{"kind": "nat_add", "src": []string{"192.0.2.1"}, "dst": []string{"198.51.100.0/24"},
			"comment": "cf-x", "groups": []string{"cf_nat_1"}}}})
	moved := tables("route_object", rows{ikGroup(7, "cf_nat_1", "cf-mihomo-nat", "198.51.100.0/24"), ikGroup(6, "office", "", "192.0.2.0/24")},
		"nat_rule", rows{ikNAT(1, "cf-mihomo", addressField(nil))})
	record := ikNAT(1, "cf-mihomo", addressField([]string{"203.0.113.0/24"}, ref("IPGP6", "office"), ref("IPGP5", "cf_nat_1")))
	add(ikuaiCase{name: "nat_restore_rebinds_new_gid", action: "apply", state: routerfake.IkuaiState{Tables: moved},
		input: map[string]any{"op": map[string]any{"kind": "nat_restore", "record": record}}})
	add(ikuaiCase{name: "nat_readd_rebinds_new_gid", action: "apply", state: routerfake.IkuaiState{Tables: moved},
		input: map[string]any{"op": map[string]any{"kind": "nat_readd", "record": func() routerfake.Row {
			r := maps.Clone(record)
			r["tagname"] = "cf_nat_readd"
			return r
		}()}}})
	gone := tables("route_object", rows{ikGroup(6, "office", "", "192.0.2.0/24")}, "nat_rule", rows{ikNAT(1, "cf-mihomo", addressField(nil))})
	add(ikuaiCase{name: "nat_restore_group_gone", action: "apply", state: routerfake.IkuaiState{Tables: gone},
		input: map[string]any{"op": map[string]any{"kind": "nat_restore", "record": record}}})
	polGroups := tables("route_object", rows{ikGroup(5, "cf_pa_1", "cf-mihomo", "198.51.100.0/24"), ikGroup(8, "cf_pa_2", "cf-mihomo", "203.0.113.0/24")},
		"stream_ipport", rows{ikPolicy(3, "cf-mihomo", addressField([]string{"198.51.100.0/24"}))})
	add(ikuaiCase{name: "policy_update_to_groups_roundtrip", action: "roundtrip", state: routerfake.IkuaiState{Tables: polGroups},
		input: map[string]any{"op": byID("policy_route_update", 3, "gateway", ikGateway, "dst", values, "comment", "cf-mihomo", "groups", []string{"cf_pa_1", "cf_pa_2"})}})
	return cases
}

// ikuaiFixedCases are Python bugs the Go driver fixes; each asserts both sides.
func ikuaiFixedCases() []ikuaiCase {
	var cases []ikuaiCase
	route := ikRoute(11, "198.51.100.0/24", ikGateway, "user", "")
	cases = append(cases, ikuaiCase{name: "fixed_route_comment_lost_reply_carries_undo", action: "release",
		state: routerfake.IkuaiState{Tables: tables("static_rt", rows{route}),
			Faults: []routerfake.IkuaiFault{{Func: "static_rt", Action: "edit", Count: 1, LoseReply: true}}},
		input: map[string]any{"ops": []map[string]any{byID("route_comment", 11, "comment", "cf-user")}},
		fixed: "Python attached no undo to a route_comment whose reply was lost: the user's route stayed marked cf-",
		check: func(t *testing.T, py, golang ikuaiOutcome, pyState, goState routerfake.IkuaiState) {
			wantFixed(t, len(py.Records) == 0 && pyState.Tables["static_rt"][0]["comment"] == "cf-user",
				"Python keeps the adopted comment: %s", outcomeText(py))
			wantFixed(t, len(golang.Records) == 1 && len(golang.Rollback) == 1 && golang.Rollback[0]["ok"] != nil &&
				goState.Tables["static_rt"][0]["comment"] == "user", "Go restores the user's comment: %s", outcomeText(golang))
			wantFixed(t, py.Error != nil && golang.Error != nil && py.Error.Message == golang.Error.Message, "same failure message")
		}})
	bad := ikRoute(12, "203.0.113.0/24", ikGateway, "cf-lab", "")
	bad["netmask"] = "255.0.255.0"
	cases = append(cases, ikuaiCase{name: "fixed_route_delete_unreadable_network_not_deleted", action: "release",
		state: routerfake.IkuaiState{Tables: tables("static_rt", rows{bad})},
		input: map[string]any{"ops": []map[string]any{byID("route_delete", 12)}},
		fixed: "Python deleted the route, then crashed computing its undo",
		check: func(t *testing.T, py, golang ikuaiOutcome, pyState, goState routerfake.IkuaiState) {
			wantFixed(t, py.Error != nil && py.Error.Class == "ValueError" && len(pyState.Tables["static_rt"]) == 0,
				"Python crashes after deleting: %s", outcomeText(py))
			wantFixed(t, golang.Error != nil && golang.Error.Class == "RouterError" &&
				golang.Error.Message == "iKuai static_rt 中 id=12 的目标网段无法识别，未删除，请检查" &&
				len(goState.Tables["static_rt"]) == 1, "Go refuses before deleting: %s", outcomeText(golang))
		}})
	cases = append(cases, ikuaiCase{name: "fixed_write_reply_without_code_is_uncertain", action: "release",
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "nat_rule", Action: "add", LoseReply: false,
			Reply: map[string]any{"Result": 10000, "message": "ok?"}}}},
		input: map[string]any{"ops": []map[string]any{{"kind": "nat_add", "src": []string{"192.0.2.1"}, "dst": []string{"198.51.100.0/24"}, "comment": "cf-x"}}},
		fixed: "Python treated a write reply without a code as a refusal (no undo)",
		check: func(t *testing.T, py, golang ikuaiOutcome, _, _ routerfake.IkuaiState) {
			wantFixed(t, py.Error != nil && py.Error.Class == "_Rejected" && len(py.Records) == 0, "Python: %s", outcomeText(py))
			wantFixed(t, golang.Error != nil && golang.Error.Class == "RouterError" && len(golang.Records) == 1 &&
				golang.Error.Message == "iKuai 调用 nat_rule 返回格式无法识别", "Go: %s", outcomeText(golang))
		}})
	cases = append(cases, ikuaiCase{name: "fixed_unparseable_redirect_explained", action: "probe",
		state: routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "login", Status: 302, Location: "https://[invalid"}}},
		fixed: "requests raised ValueError (500) parsing a Location it was told not to follow",
		check: func(t *testing.T, py, golang ikuaiOutcome, _, _ routerfake.IkuaiState) {
			wantFixed(t, py.Error != nil && py.Error.Class != "RouterError", "Python crashes: %s", outcomeText(py))
			wantFixed(t, golang.Error != nil && golang.Error.Message == "iKuai 重定向地址格式无法识别，请在浏览器核对管理地址、协议和端口。", "Go explains it: %s", outcomeText(golang))
		}})
	reused := tables("route_object", rows{ikGroup(5, "office", "", "192.0.2.0/24")}, "nat_rule", rows{ikNAT(1, "cf-mihomo", addressField(nil))})
	record := ikNAT(1, "cf-mihomo", addressField(nil, ref("IPGP5", "cf_nat_1")))
	cases = append(cases, ikuaiCase{name: "fixed_rebind_refuses_gid_reused_by_user_group", action: "apply",
		state: routerfake.IkuaiState{Tables: reused}, input: map[string]any{"op": map[string]any{"kind": "nat_restore", "record": record}},
		fixed: "Python kept a gid that now names the user's group when ours was gone",
		check: func(t *testing.T, py, golang ikuaiOutcome, pyState, goState routerfake.IkuaiState) {
			pyRef := pyState.Tables["nat_rule"][0]["dst_addr"]
			wantFixed(t, py.Error == nil && strings.Contains(fmt.Sprint(pyRef), "IPGP5"), "Python points the rule at the user's group: %v", pyRef)
			wantFixed(t, golang.Error != nil && golang.Error.Message == "iKuai IP 分组 cf_nat_1 不存在，无法恢复规则引用" &&
				len(goState.Calls) < len(pyState.Calls), "Go refuses without writing: %s", outcomeText(golang))
		}})
	return cases
}

// ikuaiReviewCases are the Python-compared cases of the O4 review of R05
// (parity/review-r05.md).
func ikuaiReviewCases() []ikuaiCase {
	var cases []ikuaiCase
	// F4: the rule a restore record names was deleted by hand and its id given
	// to the user's new rule; Python wrote the old rule over the user's.
	userRule := ikNAT(1, "user", addressField([]string{"198.51.100.0/24"}))
	userRule["tagname"] = "USER_NEW"
	ours := ikNAT(1, "cf-mihomo-nat", addressField([]string{"203.0.113.0/24"}))
	cases = append(cases, ikuaiCase{name: "fixed_restore_refuses_replaced_object", action: "apply",
		state: routerfake.IkuaiState{Tables: tables("nat_rule", rows{userRule})},
		input: map[string]any{"op": map[string]any{"kind": "nat_restore", "record": ours}},
		fixed: "Python restored a record onto whatever row held its id",
		check: func(t *testing.T, py, golang ikuaiOutcome, pyState, goState routerfake.IkuaiState) {
			wantFixed(t, py.Error == nil && pyState.Tables["nat_rule"][0]["tagname"] == "NAT_1",
				"Python overwrites the user's rule: %v", pyState.Tables["nat_rule"][0])
			wantFixed(t, golang.Error != nil && golang.Error.Message == "iKuai nat_rule 中 id=1 的对象已被替换或改名，未自动恢复，请检查" &&
				goState.Tables["nat_rule"][0]["tagname"] == "USER_NEW", "Go refuses: %s", outcomeText(golang))
		}})
	// The same restore onto the unchanged object still runs (control).
	edited := ikNAT(1, "cf-mihomo-nat", addressField([]string{"198.51.100.0/24", "203.0.113.0/24"}))
	cases = append(cases, ikuaiCase{name: "review_restore_same_object_runs", action: "apply",
		state: routerfake.IkuaiState{Tables: tables("nat_rule", rows{edited})},
		input: map[string]any{"op": map[string]any{"kind": "nat_restore", "record": ours}}})
	return cases
}

func oversizedOps() []map[string]any {
	cidrs := ikSubnets("10.0.0.0/8", 24, 6000)
	return []map[string]any{
		{"kind": "policy_route_add", "gateway": ikGateway, "dst": cidrs, "comment": "cf-mihomo"},
		{"kind": "nat_add", "src": []string{"192.0.2.1-192.0.2.3"}, "dst": cidrs, "comment": "cf-mihomo"},
		{"kind": "ikuai_group_add", "name": "cf_pa_1", "role": "pa", "values": cidrs[:3000]},
		{"kind": "policy_route_add", "gateway": ikGateway, "dst": cidrs, "groups": []string{"cf_pa_1", "cf_pa_2", "cf_pa_3"}, "comment": "cf-mihomo"},
		{"kind": "ikuai_group_add", "name": "cf_pa_2", "role": "pa", "values": cidrs[:2000]},
		{"kind": "ikuai_group_update", "name": "cf_pa_3", "role": "pa", "values": cidrs[:4000], "notes": map[string]string{cidrs[0]: "说明"}},
		{"kind": "policy_route_update", "gateway": ikGateway, "dst": cidrs[:5711], "comment": "cf-mihomo"},
		{"kind": "policy_route_update", "gateway": ikGateway, "dst": cidrs[:5705], "comment": "cf-mihomo"},
		{"kind": "nat_update", "add_dst": cidrs, "comment": "cf-mihomo-nat"},
		{"kind": "nat_update", "comment": "cf-mihomo-nat"},
		{"kind": "route_add", "cidr": cidrs[0], "gateway": ikGateway, "comment": "cf-x"},
	}
}

func requestByteItems() []map[string]any {
	probe := func(count int) map[string]any {
		return map[string]any{"func": "stream_ipport", "action": "add", "param": map[string]any{
			"enabled": "yes", "type": 1, "nexthop": "192.0.2.250", "protocol": "any", "prio": 0,
			"src_addr": map[string]any{"custom": []string{"192.0.2.250"}, "object": []any{}}, "src_addr_inv": 1,
			"dst_addr": map[string]any{"custom": ikSubnets("10.0.0.0/8", 24, count), "object": []any{}}, "dst_addr_inv": 0,
			"comment": "cf-sizeprobe"}}
	}
	return []map[string]any{probe(5706), probe(5712),
		{"func": "static_rt", "action": "add", "param": map[string]any{"comment": "中文 😀 \"q\" \\ \x01\b\f\n\t</>&  é", "tagname": "a,b:c"}},
		{"func": "route_object", "action": "edit", "param": map[string]any{"id": 9999999999, "group_name": "cf_nat_9999", "type": 0,
			"group_value": []any{map[string]any{"ip": "198.51.100.0/24", "comment": "cf-mihomo-manual"}}}},
		{"func": "x", "action": "del", "param": map[string]any{}},
	}
}
