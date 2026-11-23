package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

type r11Step struct {
	Action        string                     `json:"action"`
	Enabled       bool                       `json:"enabled"`
	Desired       *Desired                   `json:"desired"`
	Scope         DNSCheck                   `json:"scope"`
	Mutation      *routerfake.ExternalWrite  `json:"mutation,omitempty"`
	Agent         *model.Agent               `json:"agent,omitempty"`
	Host          IPv6Host                   `json:"host"`
	Configuration IPv6ReadinessConfiguration `json:"configuration"`
	DNSTarget     string                     `json:"dns_target"`
	PolicyContext PolicyContext              `json:"policy_context"`
}
type r11Case struct {
	Name    string           `json:"name"`
	Feature string           `json:"feature"`
	State   routerfake.State `json:"state"`
	Router  *Router          `json:"router"`
	Steps   []r11Step        `json:"steps"`
}

func r11State() routerfake.State {
	s := r10State()
	s.Tables["/ip/dns"] = []routerfake.Row{{"servers": "192.0.2.2", "dynamic-servers": ""}}
	for _, path := range []string{pdScripts, pdSchedulers, pdClients, ros.PathIPv6AddressLists, pdPrefixes, ros.PathIPv6ND, "/ipv6/firewall/nat", "/ipv6/firewall/raw", "/ipv6/firewall/mangle"} {
		s.Tables[path] = []routerfake.Row{}
	}
	s.Tables[pdClients] = []routerfake.Row{{".id": "*C0", "interface": "wan", "pool-name": "pool-example", "request": "prefix", "disabled": "false", "status": "bound", "prefix": "2001:db8:1::/60, 1d"}, {".id": "*C1", "interface": "wan", "request": "prefix", "disabled": "true"}}
	s.Tables[rosIPv6Addresses] = append(s.Tables[rosIPv6Addresses], routerfake.Row{".id": "*A2", "address": "2001:db8:1::1/64", "interface": "bridge", "from-pool": "pool-example", "advertise": "true", "disabled": "false"})
	s.Tables[ros.PathDHCPNetworks] = []routerfake.Row{{".id": "*D0", "address": "192.0.2.0/24", "gateway": "192.0.2.1", "dns-server": "192.0.2.2", "dns-none": "false"}}
	s.Tables[ros.PathDHCPServers] = []routerfake.Row{{".id": "*D1", "name": "lan", "interface": "bridge", "disabled": "false"}}
	for _, p := range []string{ros.PathDHCPLeases, ros.PathDHCPOptions, ros.PathDHCPOptionSets, ros.PathDHCPMatchers} {
		s.Tables[p] = []routerfake.Row{}
	}
	s.Tables[ros.PathIPv6ND] = []routerfake.Row{{".id": "*E0", "interface": "bridge", "advertise-dns": "yes", "dns-servers": "2001:db8::53", "disabled": "false", "dynamic": "false", "invalid": "false", "ra-lifetime": "30m", "mtu": "1500", "comment": "synthetic LAN"}}
	return s
}
func r11StepFor(action string) r11Step {
	return r11Step{Action: action, Enabled: true, Desired: &Desired{ProxyCIDRs: []ProxyCIDR{}, IPv6CIDRs: []ProxyCIDR{}, Warnings: []string{}, BlockingWarnings: []string{}, ProxyGateway: "192.0.2.4", DNSSettings: &DNSSettings{AgentID: "dns", Server: "192.0.2.2"}}, Scope: DNSCheck{ID: "*D0", Network: "192.0.2.0/24", Gateway: "192.0.2.4", DNS: []string{"192.0.2.2"}}, Agent: r10Agent(), Host: r10Host(), Configuration: IPv6ReadinessConfiguration{PolicyConfiguration: PolicyConfiguration{FakeIPv6Enabled: true, FakeIPv6Range: "fd00:face::/64"}, IPv6: true, FakeIPv6Route: new(true), TUNEnabled: true}}
}
func r11Run(t *testing.T, f *routerfake.Fake, endpoint string, tc r11Case) r08Output {
	t.Helper()
	f.Reset(tc.State)
	d := NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	out := r08Output{Results: []json.RawMessage{}}
	var plan *IPv6PDGuardPlan
	var dns *IPv6DNSPlan
	var undo json.RawMessage
	var err error
	add := func(v any) {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		out.Results = append(out.Results, raw)
	}
	for _, s := range tc.Steps {
		if s.Mutation != nil {
			f.ApplyExternal(*s.Mutation)
		}
		if tc.Feature == "readiness" {
			var result *IPv6Readiness
			result, err = InspectIPv6Readiness(context.Background(), d, tc.Router, s.Agent, s.Host, s.Configuration, s.DNSTarget, s.PolicyContext)
			if err == nil {
				add(result)
			}
		} else if s.Action == "undo" {
			var v json.RawMessage
			v, err = d.Apply(t.Context(), undo)
			if err == nil {
				add(v)
			}
		} else if s.Action == "apply" {
			var op Op
			if tc.Feature == "pd" {
				op = plan.Ops[0]
			} else {
				op = dns.Ops[0]
			}
			raw, _ := json.Marshal(op)
			undo, err = d.Apply(t.Context(), raw)
			if err == nil {
				add(undo)
			}
		} else {
			var ops []Op
			if tc.Feature == "pd" {
				r := *tc.Router
				r.Extra = map[string]json.RawMessage{}
				for k, v := range tc.Router.Extra {
					r.Extra[k] = v
				}
				r.IPv6PDGuard = &PDGuard{Enabled: s.Enabled}
				if d.IPv6PDGuardManifestSet() {
					r.Extra["ipv6_pd_guard_state"] = d.IPv6PDGuardManifest()
				}
				plan, err = InspectIPv6PDGuard(t.Context(), &r, "192.0.2.4", d)
				if err == nil {
					add(plan)
					ops = plan.Ops
					if len(plan.Blockers) > 0 {
						continue
					}
				}
			} else {
				dns, err = InspectIPv6DNS(t.Context(), s.Desired, s.Scope, d)
				if err == nil {
					add(dns)
					ops = dns.Ops
					if len(dns.Blockers) > 0 {
						continue
					}
				}
			}
			if err == nil && s.Action == "report" {
				state, e := d.ReadState(t.Context())
				if e != nil {
					err = e
				} else {
					if tc.Feature == "pd" {
						state.IPv6PDGuardPlan = plan
					} else {
						state.IPv6DNSPlan = dns
					}
					report, e := BuildReport(s.Desired, state)
					err = e
					if e == nil {
						add(report)
					}
				}
				if err == nil {
					continue
				}
			}
			if err == nil && s.Action != "plan" {
				if len(ops) > 0 {
					raw, _ := json.Marshal(ops[0])
					undo, err = d.Apply(t.Context(), raw)
					if err == nil {
						add(undo)
					}
				}
				if err == nil {
					if tc.Feature == "pd" {
						err = VerifyIPv6PDGuard(t.Context(), d, plan.Check)
					} else {
						err = VerifyIPv6DNS(t.Context(), d, dns.Check)
					}
				}
				if err == nil && s.Action == "rollback" && len(ops) > 0 {
					var v json.RawMessage
					v, err = d.Apply(t.Context(), undo)
					if err == nil {
						add(v)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		class := "RouterError"
		if device.RouterErrorKind(err) == KindRollback {
			class = "RouterRollbackError"
		}
		out.Error = &r08Error{class, err.Error()}
	}
	out.Manifest = ros.GuardRaw(d.IPv6PDGuardManifest())
	return out
}

// r11Comparable removes only documented additions (stable PD proofs), optional
// empty internal pointers, and the process-local readiness fingerprint format.
// Every status, message, operation, expected field and final device row remains.
func r11Comparable(t *testing.T, v any) any {
	t.Helper()
	x := r08Comparable(t, v)
	var visit func(any)
	visit = func(x any) {
		switch a := x.(type) {
		case map[string]any:
			if _, ok := a["role"]; ok {
				if _, ok := a["path"]; ok {
					if _, ok := a["method"]; !ok {
						delete(a, "config")
					}
				}
			}
			if _, ok := a["route_publication_required"]; ok {
				delete(a, "fingerprint")
			}
			for _, k := range []string{"check", "evidence"} {
				if value, exists := a[k]; exists && value == nil {
					a[k] = map[string]any{}
				}
			}
			// Omitted false/empty fields in the typed check are semantically absent.
			if _, ok := a["withdraw"]; ok {
				if a["withdraw"] == false {
					delete(a, "removed")
				}
				if a["withdraw"] == true {
					delete(a, "enabled")
					delete(a, "owned")
				}
			}
			// IPv6 address-list KnownIDs is now list-scoped. RESTORE only uses
			// KnownIDs for route conflict checks, so foreign list IDs have no effect.
			if a["path"] == ros.PathIPv6AddressLists && a["method"] == "RESTORE" {
				a["known_ids"] = []any{a["id"]}
			}
			// Native JSON text replaces Python repr only inside stable snapshots.
			if a["dns-servers"] == `["2001:db8::53","2001:db8::54"]` {
				a["dns-servers"] = "['2001:db8::53', '2001:db8::54']"
			}
			for _, v := range a {
				visit(v)
			}
		case []any:
			for _, v := range a {
				visit(v)
			}
		}
	}
	visit(x)
	return x
}
func TestR11PythonBoundary(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	cases := r11Cases()
	t.Logf("real Python boundary sequences: %d", len(cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var py r08Output
			raw := r09Python(t, "r11_reference.py", tc, srv.URL)
			if e := json.Unmarshal(raw, &py); e != nil {
				t.Fatal(e, string(raw))
			}
			pystate := f.Snapshot()
			got := r11Run(t, f, srv.URL, tc)
			state := f.Snapshot()
			a, b := r11Comparable(t, py), r11Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("result: %s", r08Difference(a, b, "$"))
			}
			a, b = r08Comparable(t, pystate.Tables), r08Comparable(t, state.Tables)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("tables: %s", r08Difference(a, b, "$"))
			}
			a, b = r10Writes(t, pystate.Calls), r10Writes(t, state.Calls)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("writes: %s", r08Difference(a, b, "$"))
			}
		})
	}
}
func r11Cases() []r11Case {
	out := []r11Case{}
	base := func(feature, name string, steps ...r11Step) r11Case {
		return r11Case{feature + "/" + name, feature, r11State(), r10Router(), steps}
	}
	for _, feature := range []string{"pd", "dns"} {
		out = append(out, base(feature, "publish_idempotent", r11StepFor("publish"), r11StepFor("publish")), base(feature, "rollback", r11StepFor("rollback")), base(feature, "report", r11StepFor("report")))
		for _, which := range []string{"lan", "lifetime", "new_all", "client_hook"} {
			tc := base(feature, "preview_"+which, r11StepFor("plan"), r11StepFor("apply"))
			path, id := ros.PathIPv6ND, "*E0"
			patch := routerfake.Row{"ra-lifetime": "1h"}
			switch which {
			case "lan":
				path, id, patch = rosIPv4Addresses, "*B1", routerfake.Row{"interface": "guest", "actual-interface": "guest"}
			case "new_all":
				patch = routerfake.Row{"interface": "all"}
			case "client_hook":
				path, id, patch = pdClients, "*C0", routerfake.Row{"script": ":log info other"}
			}
			tc.Steps[1].Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{path: {id: patch}}}}
			out = append(out, tc)
		}
	}
	off := r11StepFor("publish")
	off.Enabled = false
	out = append(out, base("pd", "withdraw", r11StepFor("publish"), off))
	off.Action = "rollback"
	out = append(out, base("pd", "withdraw_rollback", r11StepFor("publish"), off))
	for _, action := range []string{"publish", "rollback"} {
		step := r11StepFor(action)
		step.Enabled = false
		step.Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{
			ros.PathIPv6AddressLists: {{".id": "*F1", "list": pdLast, "address": "2001:db8:1::/64", "dynamic": "false"}, {".id": "*F2", "list": "geo6", "address": "2001:db8:9::/48", "dynamic": "false"}},
			pdPrefixes:               {{".id": "*F3", "prefix": "2001:db8:2::/64", "interface": "bridge", "comment": "cf-pd: deprecated until 12345", "dynamic": "false"}, {".id": "*F4", "prefix": "2001:db8:3::/64", "interface": "bridge", "comment": "manual", "dynamic": "false"}, {".id": "*65", "prefix": "2001:db8:1::/64", "interface": "bridge", "dynamic": "true"}},
		}}}
		out = append(out, base("pd", "runtime_"+action, r11StepFor("publish"), step))
	}

	for _, which := range []string{"manual_formatted", "hold_diff", "body_diff", "policy_diff", "poll_interval", "poll_disabled", "manual", "update", "shared_hook", "foreign_hook", "foreign_script", "foreign_poll", "pool_shared", "pool_ambiguous", "no_client", "two_clients", "no_pool", "no_lan", "bad_name", "bad_length", "no_advertise", "two_addresses", "rollback_helper"} {
		tc := base("pd", which, r11StepFor("publish"))
		switch which {
		case "manual_formatted", "hold_diff", "body_diff", "policy_diff", "poll_interval", "poll_disabled", "manual", "update", "shared_hook", "foreign_script", "foreign_poll", "rollback_helper":
			source, _ := IPv6PDWatchSource("bridge", "pool-example")
			if which == "update" {
				source = strings.ReplaceAll(source, `"bridge"`, `"old-lan"`)
			}
			if which == "foreign_script" {
				source = ":log info mine"
			}
			tc.State.Tables[pdScripts] = []routerfake.Row{{".id": "*50", "name": pdWatch, "source": source, "policy": pdPolicy, "comment": "manual", "dont-require-permissions": "false"}}
			event := pdEvent
			if which == "foreign_poll" {
				event = ":log info mine"
			}
			tc.State.Tables[pdSchedulers] = []routerfake.Row{{".id": "*51", "name": pdPoll, "on-event": event, "interval": "30s", "start-time": "startup", "policy": pdPolicy, "disabled": "false"}}
			hook := pdHook
			if which == "update" {
				hook = pdEvent
			}
			if which == "shared_hook" {
				hook = ":log info mine\n" + pdHook
			}
			tc.State.Tables[pdClients][0]["script"] = hook
			switch which {
			case "manual_formatted":
				tc.State.Tables[pdScripts][0]["source"] = "# manual install\r\n" + strings.ReplaceAll(source, "\n", "\r\n    ")
				tc.State.Tables[pdScripts][0]["policy"] = "test,read,write"
				tc.State.Tables[pdSchedulers][0]["interval"] = "00:00:30"
			case "hold_diff":
				tc.State.Tables[pdScripts][0]["source"] = strings.Replace(source, ":local hold 3600", ":local hold 1800", 1)
			case "body_diff":
				tc.State.Tables[pdScripts][0]["source"] = strings.ReplaceAll(source, ":log info", ":log debug")
			case "policy_diff":
				tc.State.Tables[pdScripts][0]["policy"] = "read"
			case "poll_interval":
				tc.State.Tables[pdSchedulers][0]["interval"] = "1m"
			case "poll_disabled":
				tc.State.Tables[pdSchedulers][0]["disabled"] = "true"
			}

			if which == "rollback_helper" {
				tc.State.Tables[pdScripts] = append(tc.State.Tables[pdScripts], routerfake.Row{".id": "*52", "name": pdRollback, "source": ":log info helper"})
			}
		case "foreign_hook":
			tc.State.Tables[pdClients][0]["script"] = ":log info mine"
		case "pool_shared", "pool_ambiguous":
			tc.State.Tables[rosIPv6Addresses] = append(tc.State.Tables[rosIPv6Addresses], routerfake.Row{".id": "*A3", "address": "2001:db8:2::1/64", "interface": "guest", "from-pool": "pool-example", "advertise": "true"})
			if which == "pool_ambiguous" {
				tc.State.Tables[rosIPv4Addresses][0]["address"] = "198.51.100.1/24"
			}
		case "no_client":
			tc.State.Tables[pdClients][0]["disabled"] = "true"
		case "two_clients":
			tc.State.Tables[pdClients][1]["disabled"] = "false"
		case "no_pool":
			tc.State.Tables[pdClients][0]["pool-name"] = ""
		case "no_lan":
			tc.State.Tables[rosIPv6Addresses] = tc.State.Tables[rosIPv6Addresses][:1]
		case "bad_name":
			tc.State.Tables[pdClients][0]["pool-name"] = `unsafe"pool`
		case "bad_length":
			tc.State.Tables[rosIPv6Addresses][1]["address"] = "2001:db8:1::1/60"
		case "no_advertise":
			tc.State.Tables[rosIPv6Addresses][1]["advertise"] = "false"
		case "two_addresses":
			tc.State.Tables[rosIPv6Addresses] = append(tc.State.Tables[rosIPv6Addresses], routerfake.Row{".id": "*A3", "address": "2001:db8:2::1/64", "interface": "bridge", "from-pool": "pool-example", "advertise": "true"})
		}
		out = append(out, tc)
	}
	for _, which := range []string{"quoted_all", "self", "no", "empty", "disabled", "all", "all_off", "all_disabled", "override", "dns_list", "dns_legacy", "bad_dns", "dynamic", "invalid", "bad_disabled", "duplicate", "no_selected_dns", "dhcp_pending", "dhcp_ambiguous", "invalid_gateway"} {
		tc := base("dns", which, r11StepFor("publish"))
		r := tc.State.Tables[ros.PathIPv6ND][0]
		switch which {
		case "quoted_all":
			r["interface"] = "all"
			r["mtu"] = `a "b" $c`
			tc.State.Tables[rosIPv4Addresses][0]["interface"] = "lan bridge"
			tc.State.Tables[rosIPv4Addresses][0]["actual-interface"] = "lan bridge"
		case "self", "no":
			r["advertise-dns"] = which
		case "empty":
			tc.State.Tables[ros.PathIPv6ND] = []routerfake.Row{}
		case "disabled":
			r["disabled"] = "true"
		case "all", "all_off", "all_disabled":
			r["interface"] = "all"
			if which == "all_off" {
				r["advertise-dns"] = "no"
			}
			if which == "all_disabled" {
				r["disabled"] = "true"
			}
		case "override":
			other := r09Copy(r)
			other[".id"], other["interface"] = "*E1", "all"
			tc.State.Tables[ros.PathIPv6ND] = append(tc.State.Tables[ros.PathIPv6ND], other)
		case "dns_list":
			r["dns-servers"] = []any{"2001:db8::53", "2001:db8::54"}
		case "dns_legacy":
			delete(r, "dns-servers")
			r["dns"] = "2001:db8::53"
		case "bad_dns":
			r["dns-servers"] = "unknown"
		case "dynamic", "invalid":
			r[which] = "true"
		case "bad_disabled":
			r["disabled"] = "unknown"
		case "duplicate":
			other := r09Copy(r)
			other[".id"] = "*E1"
			tc.State.Tables[ros.PathIPv6ND] = append(tc.State.Tables[ros.PathIPv6ND], other)
		case "no_selected_dns":
			tc.Steps[0].Desired.AgentID = ""
		case "dhcp_pending":
			tc.State.Tables[ros.PathDHCPNetworks][0]["dns-server"] = "192.0.2.1"
		case "dhcp_ambiguous":
			other := r09Copy(tc.State.Tables[ros.PathDHCPNetworks][0])
			other[".id"], other["address"] = "*D2", "192.0.2.0/25"
			tc.State.Tables[ros.PathDHCPNetworks] = append(tc.State.Tables[ros.PathDHCPNetworks], other)
		case "invalid_gateway":
			tc.Steps[0].Desired.ProxyGateway = "198.51.100.4"
		}
		out = append(out, tc)
	}
	for _, feature := range []string{"pd", "dns"} {
		for _, path := range []string{pdScripts, pdSchedulers, pdClients, ros.PathIPv6ND} {
			tc := base(feature, "refuse_"+path, r11StepFor("publish"))
			method := "PATCH"
			if path == pdScripts || path == pdSchedulers {
				method = "PUT"
			}
			faultPath := path
			if method == "PATCH" {
				if path == pdClients {
					faultPath += "/*C0"
				} else {
					faultPath += "/*E0"
				}
			}
			tc.State.Faults = []routerfake.Fault{{Method: method, Path: faultPath, Count: 1, Status: 403}}
			out = append(out, tc)
		}
	}
	for _, which := range []string{"base", "idle", "idle_no_tun", "off", "no_tun", "foreign_good", "foreign_wrong", "overlap", "drop", "read_failure", "peer_dns"} {
		tc := base("readiness", which, r11StepFor("plan"))
		switch which {
		case "idle", "idle_no_tun":
			tc.Steps[0].Configuration.FakeIPv6Enabled = false
			tc.Steps[0].Configuration.TUNEnabled = which != "idle_no_tun"
		case "off":
			tc.Steps[0].Configuration.FakeIPv6Route = new(false)
		case "no_tun":
			tc.Steps[0].Configuration.TUNEnabled = false
		case "foreign_good", "foreign_wrong":
			gateway := "fe80::4%bridge"
			if which == "foreign_wrong" {
				gateway = "fe80::9%bridge"
			}
			tc.State.Tables[ros.PathIPv6Routes] = []routerfake.Row{r10Route("*50", "fd00:face::/64", gateway)}
		case "overlap":
			tc.State.Tables[rosIPv6Addresses][0]["address"] = "fd00:face::1/64"
		case "drop":
			tc.State.Tables["/ipv6/firewall/filter"] = []routerfake.Row{{".id": "*50", "chain": "forward", "action": "drop", "connection-state": "invalid"}}
		case "read_failure":
			tc.State.Faults = []routerfake.Fault{{Method: "GET", Path: "/ipv6/firewall/filter", Status: 403}}
		case "peer_dns":
			tc.State.Tables[pdClients][0]["use-peer-dns"] = "true"
		}
		out = append(out, tc)
	}
	return out
}
