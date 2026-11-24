package routers_test

import (
	"bytes"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

type boundaryCase struct {
	name                 string
	action               string
	state                routerfake.State
	op                   map[string]any
	ops                  []map[string]any
	undos                []map[string]any
	stop                 bool
	expectedPythonBugFix bool
}

type boundaryError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

type boundaryOutcome struct {
	Result  any              `json:"result"`
	Records []map[string]any `json:"records"`
	Error   *boundaryError   `json:"error"`
}

type boundaryCall struct {
	Method string              `json:"method"`
	Path   string              `json:"path"`
	Query  map[string][]string `json:"query,omitempty"`
	Body   any                 `json:"body,omitempty"`
}

func TestRouterOSBoundaryCorpus(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test", ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("R04 Python boundary reference is provided by ~/cf-test/.venv/bin/python")
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(working, "..", "..", ".."))
	helper := filepath.Join(working, "testdata", "r04_reference.py")
	fake := routerfake.New(nil)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	for _, tc := range r04Cases() {
		t.Run(tc.name, func(t *testing.T) {
			pythonOutcome, pythonState := runPythonBoundary(t, python, repoRoot, helper, server.URL, fake, tc)
			fake.Reset(tc.state)
			goOutcome := runGoBoundary(tc, server.URL)
			goState := fake.Snapshot()
			if tc.expectedPythonBugFix {
				if pythonOutcome.Error == nil || pythonOutcome.Error.Class != "IndexError" {
					t.Fatalf("Python reference no longer reproduces the known extra-ID bug: %#v", pythonOutcome.Error)
				}
				if goOutcome.Error == nil || goOutcome.Error.Class != "RouterRollbackError" {
					t.Fatalf("Go did not fail closed on an unowned batch ID: %#v", goOutcome.Error)
				}
				pythonCalls := jsonComparable(t, normalizeCalls(pythonState.Calls)).([]any)
				goCalls := jsonComparable(t, normalizeCalls(goState.Calls)).([]any)
				if len(pythonCalls) > len(goCalls) || !reflect.DeepEqual(pythonCalls, goCalls[:len(pythonCalls)]) {
					t.Fatal("Go request sequence diverged before its safe cleanup of the confirmed batch rows")
				}
				if len(goState.Tables["/ip/firewall/nat"]) != len(tc.state.Tables["/ip/firewall/nat"])+1 ||
					len(pythonState.Tables["/ip/firewall/nat"]) != len(goState.Tables["/ip/firewall/nat"])+4 {
					t.Fatalf("expected Go to remove four proven rows and retain one unowned row, Python to leave all five: Go=%d Python=%d",
						len(goState.Tables["/ip/firewall/nat"]), len(pythonState.Tables["/ip/firewall/nat"]))
				}
				return
			}
			pythonComparable := jsonComparable(t, pythonOutcome)
			goComparable := jsonComparable(t, goOutcome)
			if !reflect.DeepEqual(pythonComparable, goComparable) {
				t.Errorf("driver outcome differs: %s", jsonDiff(pythonComparable, goComparable))
			}
			pythonCalls := jsonComparable(t, normalizeCalls(pythonState.Calls))
			goCalls := jsonComparable(t, normalizeCalls(goState.Calls))
			if !reflect.DeepEqual(pythonCalls, goCalls) {
				t.Errorf("REST request sequence differs: %s", jsonDiff(pythonCalls, goCalls))
			}
			pythonTables := jsonComparable(t, pythonState.Tables)
			goTables := jsonComparable(t, goState.Tables)
			if !reflect.DeepEqual(pythonTables, goTables) {
				t.Errorf("final device tables differ: %s", jsonDiff(pythonTables, goTables))
			}
		})
	}
}

func jsonComparable(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		t.Fatal(err)
	}
	stripNulls(normalized)
	return normalized
}

func stripNulls(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if item == nil {
				delete(typed, key)
				continue
			}
			stripNulls(item)
		}
	case []any:
		for _, item := range typed {
			stripNulls(item)
		}
	}
}

func jsonDiff(left, right any) string {
	var differences []string
	var walk func(any, any, string)
	walk = func(a, b any, path string) {
		if len(differences) >= 8 || reflect.DeepEqual(a, b) {
			return
		}
		am, aMap := a.(map[string]any)
		bm, bMap := b.(map[string]any)
		if aMap && bMap {
			keys := map[string]bool{}
			for key := range am {
				keys[key] = true
			}
			for key := range bm {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			slices.Sort(ordered)
			for _, key := range ordered {
				walk(am[key], bm[key], path+"."+key)
			}
			return
		}
		aa, aSlice := a.([]any)
		bb, bSlice := b.([]any)
		if aSlice && bSlice {
			for index := 0; index < max(len(aa), len(bb)); index++ {
				if index >= len(aa) || index >= len(bb) {
					differences = append(differences, fmt.Sprintf("%s[%d]: lengths %d vs %d", path, index, len(aa), len(bb)))
					return
				}
				walk(aa[index], bb[index], fmt.Sprintf("%s[%d]", path, index))
			}
			return
		}
		differences = append(differences, fmt.Sprintf("%s: Python=%#v Go=%#v", path, a, b))
	}
	walk(left, right, "$")
	return strings.Join(differences, "; ")
}

func runPythonBoundary(t *testing.T, python, repoRoot, helper, endpoint string,
	fake *routerfake.Fake, tc boundaryCase) (boundaryOutcome, routerfake.State) {
	t.Helper()
	fake.Reset(tc.state)
	input := map[string]any{"action": tc.action, "state": tc.state, "stop": tc.stop}
	if tc.op != nil {
		input["op"] = tc.op
	}
	if tc.ops != nil {
		input["ops"] = tc.ops
	}
	if tc.undos != nil {
		input["undos"] = tc.undos
	}
	if tc.action == "ping" {
		input["address"], input["interface"] = "192.0.2.44", "bridge-lan"
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, python, helper)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R04_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python R04 reference failed: %v\n%s", err, output)
	}
	var outcome boundaryOutcome
	if err := json.Unmarshal(output, &outcome); err != nil {
		t.Fatalf("decode Python R04 result: %v\n%s", err, output)
	}
	return outcome, fake.Snapshot()
}

func runGoBoundary(tc boundaryCase, endpoint string) boundaryOutcome {
	ctx := context.Background()
	driver := routers.NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer driver.Close()
	outcome := boundaryOutcome{Records: []map[string]any{}}
	apply := func(op map[string]any) (any, error) {
		raw, _ := json.Marshal(op)
		result, err := driver.Apply(ctx, raw)
		if err != nil {
			return nil, err
		}
		var decoded any
		_ = json.Unmarshal(result, &decoded)
		return decoded, nil
	}
	var err error
	switch tc.action {
	case "probe":
		var result *device.RouterInfo
		result, err = driver.Probe(ctx)
		outcome.Result = result
	case "read_state":
		var result *routers.RouterState
		result, err = driver.ReadState(ctx)
		outcome.Result = result
	case "read_dhcp_dns":
		var result *device.DhcpDnsState
		result, err = driver.ReadDHCPDNS(ctx)
		outcome.Result = result
	case "ping":
		var result bool
		result, err = driver.Ping(ctx, "192.0.2.44", "bridge-lan", 500000000)
		outcome.Result = result
	case "apply":
		outcome.Result, err = apply(tc.op)
	case "roundtrip":
		var undo any
		undo, err = apply(tc.op)
		if err == nil {
			var restored any
			restored, err = apply(asMap(undo))
			outcome.Result = map[string]any{"undo": undo, "restored": restored}
		}
	case "apply_many", "batch_roundtrip":
		ops := make([]json.RawMessage, len(tc.ops))
		for i, op := range tc.ops {
			ops[i], _ = json.Marshal(op)
		}
		err = driver.ApplyMany(ctx, ops, func(op, undo json.RawMessage) {
			var item map[string]any
			_ = json.Unmarshal(op, &item)
			var inverse any
			_ = json.Unmarshal(undo, &inverse)
			outcome.Records = append(outcome.Records, map[string]any{"op": item, "undo": inverse})
		})
		if err == nil && tc.action == "batch_roundtrip" {
			undos := make([]json.RawMessage, len(outcome.Records))
			for i, record := range outcome.Records {
				undos[len(outcome.Records)-i-1], _ = json.Marshal(record["undo"])
			}
			restoreErrors := driver.RestoreMany(ctx, undos, tc.stop)
			outcome.Result = map[string]any{"records": outcome.Records, "restore_errors": pythonRestoreResults(restoreErrors)}
		} else if err == nil {
			outcome.Result = map[string]any{"records": outcome.Records}
		}
	case "restore_many":
		undos := make([]json.RawMessage, len(tc.undos))
		for i, undo := range tc.undos {
			undos[i], _ = json.Marshal(undo)
		}
		outcome.Result = pythonRestoreResults(driver.RestoreMany(ctx, undos, tc.stop))
	default:
		err = fmt.Errorf("unsupported boundary action %s", tc.action)
	}
	if err != nil {
		outcome.Error = &boundaryError{Class: goErrorClass(err), Message: err.Error()}
		outcome.Result = nil
	}
	return outcome
}

func pythonRestoreResults(messages []string) []any {
	result := make([]any, len(messages))
	for i, message := range messages {
		if message != "" {
			result[i] = message
		}
	}
	return result
}

func goErrorClass(err error) string {
	var routerErr *device.RouterError
	if !errors.As(err, &routerErr) {
		return "error"
	}
	switch routerErr.Kind {
	case device.KindRollback:
		return "RouterRollbackError"
	case device.KindConflict:
		return "RouterConflictError"
	case device.KindUncertainWrite:
		return "UncertainWrite"
	case device.KindMissingResource:
		return "MissingResource"
	case device.KindPingNotPermitted:
		return "PingNotPermitted"
	default:
		return "RouterError"
	}
}

func normalizeCalls(calls []routerfake.Call) []boundaryCall {
	out := make([]boundaryCall, len(calls))
	for i, call := range calls {
		var body any = call.Body
		object, isMap := call.Body.(map[string]any)
		if !isMap {
			if row, ok := call.Body.(routerfake.Row); ok {
				object, isMap = map[string]any(row), true
			}
		}
		if isMap {
			if script, ok := object["script"].(string); ok {
				adds, err := routerfake.ParseConsoleAdds(script)
				if err == nil {
					body = map[string]any{"as-string": object["as-string"], "console_adds": adds}
				} else {
					body = map[string]any{"console_parse_error": err.Error(), "script": script}
				}
			}
		}
		query := map[string][]string{}
		for key, values := range call.Query {
			query[key] = slices.Clone(values)
			slices.Sort(query[key])
		}
		out[i] = boundaryCall{Method: call.Method, Path: call.Path, Query: query, Body: body}
	}
	return out
}

func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func r04Cases() []boundaryCase {
	base := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/route": {
			{".id": "*1", "dst-address": "198.51.100.0/24", "gateway": "192.0.2.1", "routing-table": "main",
				"comment": "existing", "disabled": "false", "distance": "1", "active": "true"},
			{".id": "*2", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.9", "routing-table": "backup",
				"comment": "backup", "disabled": "true", "distance": "5", "active": "false"},
		},
		"/ip/firewall/nat": {
			{".id": "*A", "chain": "srcnat", "action": "accept", "comment": "manual", "disabled": "false",
				"src-address": "192.0.2.0/24", "dst-address-list": "proxy-dst", "out-interface-list": "WAN",
				"protocol": "tcp", "dst-port": "443", "bytes": "40", "packets": "2", "dynamic": "false"},
			{".id": "*F", "chain": "srcnat", "action": "masquerade", "comment": "uplink", "disabled": "false",
				"out-interface": "ether1"},
		},
		"/ip/firewall/mangle": {},
		"/ip/firewall/address-list": {
			{".id": "*B", "list": "proxy-dst", "address": "198.51.100.0/24", "disabled": "false"},
			{".id": "*C", "list": "limited", "address": "192.0.2.32/27", "disabled": "false"},
		},
		"/interface/list":        {{".id": "*L", "name": "WAN"}},
		"/interface/list/member": {{".id": "*M", "list": "WAN", "interface": "ether1"}},
	}}

	dhcp := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1",
			"netmask": "24", "dns-server": "192.0.2.53,192.0.2.54", "dns-none": "false", "domain": "example.com"}},
		"/ip/dhcp-server":       {{".id": "*S", "name": "dhcp-lan", "disabled": "false", "use-radius": "no", "interface": "bridge-lan"}},
		"/ip/dhcp-server/lease": {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpOverride := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network":     {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53"}},
		"/ip/dhcp-server":             {{".id": "*S", "name": "dhcp-lan", "disabled": "false", "dhcp-option": "dns-custom"}},
		"/ip/dhcp-server/lease":       {},
		"/ip/dhcp-server/option":      {{".id": "*O", "name": "dns-custom", "code": "6", "value": "192.0.2.99"}},
		"/ip/dhcp-server/option/sets": {}, "/ip/dhcp-server/matcher": {},
	}}
	makeDHCPOptionState := func(level string) routerfake.State {
		network := routerfake.Row{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53"}
		server := routerfake.Row{".id": "*S", "name": "dhcp-lan", "disabled": "false", "use-radius": "no"}
		lease := routerfake.Row{".id": "*L", "address": "192.0.2.10", "server": "dhcp-lan"}
		matcher := routerfake.Row{".id": "*M", "name": "special", "server": "dhcp-lan"}
		optionSets := []routerfake.Row{{".id": "*OS", "name": "custom-dns", "options": "dns-six"}}
		options := []routerfake.Row{{".id": "*O", "name": "dns-six", "code": "6", "value": "0x01010101"}}
		switch level {
		case "network":
			network["dhcp-option"] = "dns-six"
		case "server":
			server["dhcp-option-set"] = "custom-dns"
		case "lease":
			lease["dhcp-option-set"] = "custom-dns"
		case "matcher":
			matcher["option-set"] = "custom-dns"
		}
		return routerfake.State{Tables: map[string][]routerfake.Row{
			"/ip/dhcp-server/network": {network}, "/ip/dhcp-server": {server},
			"/ip/dhcp-server/lease": {lease}, "/ip/dhcp-server/option": options,
			"/ip/dhcp-server/option/sets": optionSets, "/ip/dhcp-server/matcher": {matcher},
		}}
	}
	dhcpRadius := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": dhcp.Tables["/ip/dhcp-server/network"],
		"/ip/dhcp-server":         {{".id": "*S", "name": "dhcp-lan", "disabled": "false", "use-radius": "yes"}},
		"/ip/dhcp-server/lease":   {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpUnknownOption := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dhcp-option": "future"}},
		"/ip/dhcp-server":         dhcp.Tables["/ip/dhcp-server"], "/ip/dhcp-server/lease": {},
		"/ip/dhcp-server/option":      {{".id": "*O", "name": "future", "code": "future-option"}},
		"/ip/dhcp-server/option/sets": {}, "/ip/dhcp-server/matcher": {},
	}}
	dhcpNonDNSOption := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53"}},
		"/ip/dhcp-server":         {{".id": "*S", "name": "dhcp-lan", "disabled": "false", "use-radius": "no", "dhcp-option-set": "ntp-set"}},
		"/ip/dhcp-server/lease":   {}, "/ip/dhcp-server/option": {{".id": "*O", "name": "ntp", "code": "42"}},
		"/ip/dhcp-server/option/sets": {{".id": "*OS", "name": "ntp-set", "options": "ntp"}},
		"/ip/dhcp-server/matcher":     {},
	}}
	dhcpNullOption := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": dhcp.Tables["/ip/dhcp-server/network"],
		"/ip/dhcp-server":         {{".id": "*S", "name": "dhcp-lan", "disabled": "false", "use-radius": "no", "dhcp-option": nil}},
		"/ip/dhcp-server/lease":   {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpNoDNS := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "netmask": "24", "domain": "example.com"}},
		"/ip/dhcp-server":         dhcp.Tables["/ip/dhcp-server"],
		"/ip/dhcp-server/lease":   {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpDNSNone := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53", "dns-none": "true"}},
		"/ip/dhcp-server":         dhcp.Tables["/ip/dhcp-server"],
		"/ip/dhcp-server/lease":   {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpOverlap := routerfake.State{Tables: map[string][]routerfake.Row{
		"/ip/dhcp-server/network": {
			{".id": "*A", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53"},
			{".id": "*B", "address": "192.0.2.0/25", "gateway": "192.0.2.1", "dns-server": "192.0.2.54"},
		},
		"/ip/dhcp-server":       dhcp.Tables["/ip/dhcp-server"],
		"/ip/dhcp-server/lease": {}, "/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {},
		"/ip/dhcp-server/matcher": {},
	}}
	dhcpOp := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"},
		"scope": map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"},
		"expected": map[string]any{"address": "192.0.2.0/24", "gateway": "192.0.2.1", "netmask": "24",
			"dns-server": "192.0.2.53,192.0.2.54", "dns-none": "false", "domain": "example.com"}}
	dhcpDriftOp := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"},
		"scope": map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"},
		"expected": map[string]any{"address": "192.0.2.0/24", "gateway": "192.0.2.99", "netmask": "24",
			"dns-server": "192.0.2.53,192.0.2.54", "dns-none": "false", "domain": "example.com"}}
	dhcpNoDNSOp := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"},
		"scope": map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"},
		"expected": map[string]any{"address": "192.0.2.0/24", "gateway": "192.0.2.1", "netmask": "24",
			"dns-server": "", "dns-none": "false", "domain": "example.com"}}
	dhcpDNSNoneOp := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.54"},
		"scope":    map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"},
		"expected": map[string]any{"address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53", "dns-none": "true"}}
	dhcpOverrideOp := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"},
		"scope":    map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"},
		"expected": map[string]any{"address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.53", "dns-none": "false"}}

	makeRouteOps := func(count int) []map[string]any {
		ops := make([]map[string]any, count)
		for i := 0; i < count; i++ {
			ops[i] = map[string]any{"kind": "route_add", "cidr": fmt.Sprintf("45.%d.%d.0/24", 10+i/250, i%250),
				"gateway": "192.0.2.4", "comment": "cf-组\" \"\";\\$\nemoji-🙂"}
		}
		return ops
	}
	makeLongRouteOps := func(count int) []map[string]any {
		ops := makeRouteOps(count)
		for _, op := range ops {
			op["comment"] = strings.Repeat("路", 4500)
		}
		return ops
	}
	makeRouteDeletes := func(count int) (routerfake.State, []map[string]any) {
		rows := make([]routerfake.Row, count)
		ops := make([]map[string]any, count)
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("*%X", i+1)
			cidr := fmt.Sprintf("45.%d.%d.0/24", 20+i/250, i%250)
			rows[i] = routerfake.Row{".id": id, "dst-address": cidr, "gateway": "192.0.2.1",
				"routing-table": "main", "comment": "cf-delete", "disabled": "false"}
			ops[i] = map[string]any{"kind": "route_delete", "id": id, "cidr": cidr}
		}
		return routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": rows}}, ops
	}
	routeDeleteState, routeDeleteOps := makeRouteDeletes(205)

	natUpdate := map[string]any{"kind": "nat_update", "id": "*A", "add_dst": []string{
		"45.10.0.0/24", "45.10.1.0/24", "45.10.2.0/24", "45.10.3.0/24"}, "comment": "cf-policy",
		"out_interface": "ether1", "expected": map[string]any{"chain": "srcnat", "action": "accept",
			"comment": "manual", "disabled": "false", "src-address": "192.0.2.0/24", "dst-address-list": "proxy-dst",
			"out-interface-list": "WAN", "protocol": "tcp", "dst-port": "443"}}

	cases := []boundaryCase{
		{name: "probe_capabilities", action: "probe", state: base},
		{name: "probe_access_denied", action: "probe", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/system/resource", Status: 401}}}},
		{name: "probe_rest_service_forbidden", action: "probe", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/system/resource", Status: 403}}}},
		{name: "unrelated_read_server_error", action: "probe", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/system/resource", Status: 500,
				Body: routerfake.Row{"message": "Internal Server Error", "detail": "routing process failed"}}}}},
		{name: "read_state_scoped_nat_context", action: "read_state", state: base},
		{name: "read_dhcp_dns_overrides", action: "read_dhcp_dns", state: dhcp},
		{name: "read_dhcp_custom_option6_blocker", action: "read_dhcp_dns", state: dhcpOverride},
		{name: "read_dhcp_explicit_null_option_rejected", action: "read_dhcp_dns", state: dhcpNullOption},
		{name: "read_dhcp_radius_blocker", action: "read_dhcp_dns", state: dhcpRadius},
		{name: "read_dhcp_unknown_option_code_blocker", action: "read_dhcp_dns", state: dhcpUnknownOption},
		{name: "read_dhcp_non_dns_option_allowed", action: "read_dhcp_dns", state: dhcpNonDNSOption},
		{name: "read_dhcp_unavailable_override_table_fails_closed", action: "read_dhcp_dns",
			state: routerfake.State{Tables: dhcp.Tables, Faults: []routerfake.Fault{{Method: "GET", Path: "/ip/dhcp-server/option", Status: 404}}}},
		{name: "dhcp_preview_drift_rejected_before_write", action: "apply", state: dhcp, op: dhcpDriftOp},
		{name: "dhcp_overlapping_network_rejected", action: "apply", state: dhcpOverlap, op: dhcpDriftOp},
		{name: "dhcp_custom_option6_rejected_before_write", action: "apply", state: dhcpOverride, op: dhcpOverrideOp},
		{name: "dhcp_missing_scope_rejected_before_read", action: "apply", state: dhcp,
			op: map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"}}},
		{name: "ping_arp_probe", action: "ping", state: base},
		{name: "ping_permission_guidance", action: "ping", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "POST", Path: "/ping", Status: 400,
				Body: routerfake.Row{"error": 400, "message": "Bad Request", "detail": "not enough permissions (9)"}}}}},
		{name: "write_permission_message_http_400", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "PATCH", Path: "/ip/route/*1", Status: 400,
				Body: routerfake.Row{"error": 400, "message": "Bad Request", "detail": "not enough permissions (9)"}}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-permission"}},
		{name: "read_permission_message_http_400", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/ip/route/*1", Status: 400,
				Body: routerfake.Row{"error": 400, "message": "Bad Request", "detail": "not enough permissions (9)"}}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-permission"}},
		{name: "write_permission_message_http_500", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "PATCH", Path: "/ip/route/*1", Status: 500,
				Body: routerfake.Row{"error": 500, "message": "Internal Server Error", "detail": "not enough permissions (9)"}}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-permission"}},
		{name: "read_permission_message_http_500", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/ip/route/*1", Status: 500,
				Body: routerfake.Row{"error": 500, "message": "Internal Server Error", "detail": "not enough permissions (9)"}}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-permission"}},
		{name: "unrelated_write_server_error", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "PATCH", Path: "/ip/route/*1", Status: 500,
				Body: routerfake.Row{"error": 500, "message": "Internal Server Error", "detail": "routing process failed"}}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-failure"}},
		{name: "non_json_http_error_keeps_status", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "GET", Path: "/ip/route/*1", Status: 502, RawBody: "<html>Bad Gateway</html>"}}},
			op: map[string]any{"kind": "route_comment", "id": "*1", "comment": "cf-no-write"}},
		{name: "route_add_restore_with_unicode", action: "roundtrip", state: base, op: map[string]any{
			"kind": "route_add", "cidr": "45.11.12.0/24", "gateway": "192.0.2.4", "comment": "cf-引用 \"$;\\\n行🙂"}},
		{name: "route_comment_restore", action: "roundtrip", state: base, op: map[string]any{
			"kind": "route_comment", "id": "*1", "comment": "配置流量\";\\$"}},
		{name: "route_delete_restore_order", action: "roundtrip", state: base,
			op: map[string]any{"kind": "route_delete", "id": "*2"}},
		{name: "nat_add_single_rest", action: "roundtrip", state: base, op: map[string]any{
			"kind": "nat_add", "src": []string{"192.0.2.0/24"}, "dst": []string{"45.20.0.0/24"}, "comment": "cf-mihomo"}},
		{name: "nat_add_console_batch", action: "roundtrip", state: base, op: map[string]any{
			"kind": "nat_add", "src": []string{"192.0.2.0/24"}, "dst": []string{"45.21.0.0/24", "45.21.1.0/24", "45.21.2.0/24", "45.21.3.0/24"}, "comment": "cf-批量 \"$;🙂"}},
		{name: "nat_batch_extra_id_python_index_bug", action: "apply", expectedPythonBugFix: true,
			state: routerfake.State{Tables: base.Tables, Faults: []routerfake.Fault{{Method: "POST", Path: "/execute",
				AfterCommit: true, OutputSuffix: []string{"cf-id:4:*FA"}, ExtraRows: map[string][]routerfake.Row{
					"/ip/firewall/nat": {{".id": "*FA", "chain": "srcnat", "action": "accept", "comment": "foreign", "src-address": "192.0.2.1"}},
				}}}},
			op: map[string]any{"kind": "nat_add", "src": []string{"192.0.2.0/24"},
				"dst": []string{"45.22.0.0/24", "45.22.1.0/24", "45.22.2.0/24", "45.22.3.0/24"}, "comment": "cf-overflow"}},
		{name: "nat_update_console_batch_and_restore_order", action: "roundtrip", state: base, op: natUpdate},
		{name: "dhcp_dns_update_and_restore", action: "roundtrip", state: dhcp, op: dhcpOp},
		{name: "dhcp_unset_dns_restores_default", action: "roundtrip", state: dhcpNoDNS, op: dhcpNoDNSOp},
		{name: "dhcp_dns_none_update_and_restore", action: "roundtrip", state: dhcpDNSNone, op: dhcpDNSNoneOp},
		{name: "dhcp_invalid_network_is_not_ignored", action: "read_dhcp_dns",
			state: routerfake.State{Tables: map[string][]routerfake.Row{
				"/ip/dhcp-server/network": {{".id": "*A", "address": "not-a-network"}},
				"/ip/dhcp-server":         dhcp.Tables["/ip/dhcp-server"], "/ip/dhcp-server/lease": {},
				"/ip/dhcp-server/option": {}, "/ip/dhcp-server/option/sets": {}, "/ip/dhcp-server/matcher": {},
			}}},
		{name: "dhcp_patch_failure_keeps_original", action: "apply",
			state: routerfake.State{Tables: dhcp.Tables, Faults: []routerfake.Fault{{Method: "PATCH",
				Path: "/ip/dhcp-server/network/*A", Status: 400,
				Body: routerfake.Row{"error": 400, "message": "Bad Request", "detail": "write rejected"}}}}, op: dhcpOp},
		{name: "dhcp_dns_lost_patch_reply_restores", action: "roundtrip",
			state: routerfake.State{Tables: dhcp.Tables, Faults: []routerfake.Fault{{Method: "PATCH",
				Path: "/ip/dhcp-server/network/*A", Count: 1, LoseReply: true, AfterCommit: true}}}, op: dhcpOp},
		{name: "route_batch_write_and_remove", action: "batch_roundtrip", state: base, ops: makeRouteOps(12)},
		{name: "route_batch_limit_100_operations", action: "apply_many", state: base, ops: makeRouteOps(205)},
		{name: "route_batch_script_byte_limit", action: "apply_many", state: base, ops: makeLongRouteOps(10)},
		{name: "route_batch_delete_and_restore_205", action: "batch_roundtrip", state: routeDeleteState, ops: routeDeleteOps},
		{name: "route_batch_delete_and_recreate", action: "batch_roundtrip", state: routerfake.State{Tables: map[string][]routerfake.Row{
			"/ip/route": {
				{".id": "*1", "dst-address": "198.51.100.0/24", "gateway": "192.0.2.1", "routing-table": "main", "comment": "existing", "disabled": "false"},
				{".id": "*2", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.9", "routing-table": "main", "comment": "second", "disabled": "false"},
				{".id": "*3", "dst-address": "45.1.0.0/24", "gateway": "192.0.2.3", "routing-table": "main", "comment": "third", "disabled": "false"},
				{".id": "*4", "dst-address": "45.2.0.0/24", "gateway": "192.0.2.4", "routing-table": "main", "comment": "fourth", "disabled": "false"},
				{".id": "*5", "dst-address": "45.3.0.0/24", "gateway": "192.0.2.5", "routing-table": "main", "comment": "fifth", "disabled": "false"},
				{".id": "*6", "dst-address": "45.4.0.0/24", "gateway": "192.0.2.6", "routing-table": "main", "comment": "sixth", "disabled": "false"},
			},
		}}, ops: []map[string]any{
			{"kind": "route_delete", "id": "*1", "cidr": "198.51.100.0/24"},
			{"kind": "route_delete", "id": "*2", "cidr": "203.0.113.0/24"},
			{"kind": "route_delete", "id": "*3", "cidr": "45.1.0.0/24"},
			{"kind": "route_delete", "id": "*4", "cidr": "45.2.0.0/24"},
			{"kind": "route_delete", "id": "*5", "cidr": "45.3.0.0/24"},
			{"kind": "route_delete", "id": "*6", "cidr": "45.4.0.0/24"},
		}},
		{name: "batch_console_permission_refusal_falls_back", action: "apply_many", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "POST", Path: "/execute", Status: 500,
				Body: routerfake.Row{"error": 500, "message": "Internal Server Error", "detail": "not enough permissions (9)"}}}},
			ops: makeRouteOps(5)},
		{name: "batch_readback_retries_transient_http_error", action: "apply_many", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "POST", Path: "/ip/route/print", Count: 2, Status: 503,
				Body: routerfake.Row{"error": 503, "message": "Service Unavailable", "detail": "temporary read failure"}}}},
			ops: makeRouteOps(5)},
		{name: "lost_batch_reply_keeps_unknown_routes", action: "apply_many", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "POST", Path: "/execute", LoseReply: true, AfterCommit: true}}}, ops: makeRouteOps(5)},
		{name: "lost_single_reply_is_uncertain", action: "apply", state: routerfake.State{Tables: base.Tables,
			Faults: []routerfake.Fault{{Method: "PUT", Path: "/ip/route", LoseReply: true, AfterCommit: true}}},
			op: map[string]any{"kind": "route_add", "cidr": "45.99.0.0/24", "gateway": "192.0.2.4", "comment": "cf-uncertain"}},
	}
	cases = append(cases, r04ReviewCases(base, dhcp)...)
	for _, level := range []string{"network", "server", "lease", "matcher"} {
		cases = append(cases, boundaryCase{name: "read_dhcp_option6_" + level, action: "read_dhcp_dns", state: makeDHCPOptionState(level)})
	}
	return cases
}

// routeSnapshot is the driver's ownership snapshot of a plain static route
// (RouterOS defaults overlaid with the row's writable fields).
func routeSnapshot(row routerfake.Row) map[string]any {
	out := map[string]any{"disabled": "false", "comment": "", "routing-table": "main", "distance": "1",
		"scope": "30", "target-scope": "10", "check-gateway": "none", "pref-src": "",
		"suppress-hw-offload": "false", "blackhole": "false", "vrf-interface": ""}
	for key, value := range row {
		if !strings.HasPrefix(key, ".") && key != "dynamic" {
			out[key] = value
		}
	}
	return out
}

func routeUndo(method string, row routerfake.Row, extra map[string]any) map[string]any {
	change := map[string]any{"method": method, "path": "/ip/route", "id": row[".id"], "expected": routeSnapshot(row)}
	for key, value := range extra {
		change[key] = value
	}
	return map[string]any{"kind": "ros_restore", "changes": []any{change}}
}

// r04ReviewCases are the O4 review's Python-compared cases (server/parity/review-r04.md).
func r04ReviewCases(base, dhcp routerfake.State) []boundaryCase {
	routes := func(count int, first int) []routerfake.Row {
		rows := make([]routerfake.Row, count)
		for i := range rows {
			rows[i] = routerfake.Row{".id": fmt.Sprintf("*%X", first+i), "dst-address": fmt.Sprintf("45.30.%d.0/24", i),
				"gateway": "192.0.2.1", "routing-table": "main", "comment": "cf-sync", "disabled": "false"}
		}
		return rows
	}
	deleteUndos := func(rows []routerfake.Row) []map[string]any {
		undos := make([]map[string]any, len(rows))
		for i, row := range rows {
			undos[i] = routeUndo("DELETE", row, nil)
		}
		return undos
	}

	// F2: batch rollback messages carry the "RouterOS 回滚未完成：" prefix.
	changed := routes(4, 0x31)
	changedTable := cloneFakeRows(changed)
	changedTable[1]["comment"] = "edited-by-user"
	dynamic := routes(4, 0x41)
	dynamicTable := cloneFakeRows(dynamic)
	dynamicTable[2]["dynamic"] = "true"
	removed := routes(4, 0x51)
	restoreUndos := make([]map[string]any, len(removed))
	for i, row := range removed {
		values := map[string]any{}
		for key, value := range row {
			if key != ".id" {
				values[key] = value
			}
		}
		restoreUndos[i] = routeUndo("RESTORE", row, map[string]any{"values": values, "known_ids": []string{row[".id"].(string)}})
	}
	foreign := routerfake.Row{".id": "*5F", "dst-address": "45.30.1.0/24", "gateway": "192.0.2.77",
		"routing-table": "main", "comment": "manual", "disabled": "false"}
	presentDynamic := routes(1, 0x61)[0]
	presentDynamicTable := cloneFakeRows([]routerfake.Row{presentDynamic})
	presentDynamicTable[0]["dynamic"] = "true"
	presentDynamicValues := map[string]any{"dst-address": presentDynamic["dst-address"], "gateway": "192.0.2.1",
		"routing-table": "main", "comment": "cf-sync", "disabled": "false"}

	// F5: a dynamic NAT rule is refused as dynamic, not as preview drift.
	dynamicNAT := routerfake.State{Tables: map[string][]routerfake.Row{"/ip/firewall/nat": {
		{".id": "*D", "chain": "srcnat", "action": "accept", "comment": "manual", "disabled": "false", "dynamic": "true"},
	}}}

	addOps := make([]map[string]any, 5)
	for i := range addOps {
		addOps[i] = map[string]any{"kind": "route_add", "cidr": fmt.Sprintf("45.31.%d.0/24", i),
			"gateway": "192.0.2.4", "comment": "cf-sync"}
	}
	dhcpNoExpected := map[string]any{"kind": "dhcp_dns_update", "id": "*A", "dns": []string{"192.0.2.53"},
		"scope": map[string]any{"id": "*A", "network": "192.0.2.0/24", "gateway": "192.0.2.2"}}

	return []boundaryCase{
		{name: "review_batch_delete_rollback_changed_route_message", action: "restore_many",
			state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": changedTable}}, undos: deleteUndos(changed)},
		{name: "review_single_delete_rollback_changed_route_message", action: "restore_many",
			state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": changedTable[1:2]}}, undos: deleteUndos(changed[1:2])},
		{name: "review_batch_delete_rollback_dynamic_route_message", action: "restore_many",
			state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": dynamicTable}}, undos: deleteUndos(dynamic)},
		{name: "review_batch_recreate_rollback_conflict_message", action: "restore_many",
			state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": {foreign}}}, undos: restoreUndos},
		{name: "review_restore_present_dynamic_route_message", action: "restore_many",
			state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": presentDynamicTable}},
			undos: []map[string]any{routeUndo("RESTORE", presentDynamic, map[string]any{"values": presentDynamicValues})}},
		{name: "review_nat_update_dynamic_rule_message", action: "apply", state: dynamicNAT, op: map[string]any{
			"kind": "nat_update", "id": "*D", "add_dst": []string{"45.32.0.0/24"}, "comment": "cf-policy",
			"expected": map[string]any{"chain": "srcnat", "action": "accept", "comment": "manual", "disabled": "false"}}},
		{name: "review_dhcp_missing_expected_rejected_before_write", action: "apply", state: dhcp, op: dhcpNoExpected},
		// F4: a batch whose first add reads back changed is cleaned up with the
		// same batched remove Python uses, not one REST round trip per route.
		{name: "review_batch_readback_mismatch_cleans_up_in_batch", action: "apply_many", ops: addOps,
			state: routerfake.State{Tables: base.Tables, Faults: []routerfake.Fault{{Method: "POST", Path: "/execute",
				AfterCommit: true, PatchCreated: map[int]routerfake.Row{0: {"comment": "normalized-by-device"}}}}}},
	}
}

func cloneFakeRows(rows []routerfake.Row) []routerfake.Row {
	out := make([]routerfake.Row, len(rows))
	for i, row := range rows {
		out[i] = routerfake.Row{}
		for key, value := range row {
			out[i][key] = value
		}
	}
	return out
}
