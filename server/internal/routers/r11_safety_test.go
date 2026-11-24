package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

func r11Driver(t *testing.T) (*RouterOS, *routerfake.Fake) {
	t.Helper()
	f := routerfake.New(nil)
	f.Reset(r11State())
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	d := NewRouterOS(s.URL, "synthetic-user", "synthetic-password", false)
	t.Cleanup(d.Close)
	return d, f
}
func r11PDPlan(t *testing.T, d *RouterOS, enabled bool) *ipv6.IPv6PDGuardPlan {
	t.Helper()
	r := r10Router()
	r.IPv6PDGuard = &spec.PDGuard{Enabled: enabled}
	if d.IPv6PDGuardManifestSet() {
		r.Extra["ipv6_pd_guard_state"] = ros.GuardRaw(d.IPv6PDGuardManifest())
	}
	p, e := ipv6.InspectIPv6PDGuard(t.Context(), r, "192.0.2.4", d.Client)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func r11DNSPlan(t *testing.T, d *RouterOS) *ipv6.IPv6DNSPlan {
	t.Helper()
	s := r11StepFor("plan")
	p, e := ipv6.InspectIPv6DNS(t.Context(), s.Desired, s.Scope, d.Client)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func r11PDApply(t *testing.T, d *RouterOS, p *ipv6.IPv6PDGuardPlan) *ipv6.IPv6PDGuardUndo {
	t.Helper()
	if len(p.Blockers) > 0 || len(p.Ops) != 1 {
		t.Fatalf("invalid plan %#v", p)
	}
	u, e := ipv6.ApplyIPv6PDGuard(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6PDGuardSync))
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func r11Manifest(t *testing.T, d *RouterOS) ipv6.IPv6PDManifest {
	t.Helper()
	var m ipv6.IPv6PDManifest
	if e := json.Unmarshal(d.IPv6PDGuardManifest(), &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestR11TemplateBytes(t *testing.T) {
	for _, pair := range [][2]string{{"bridge", "pool-example"}, {"lan4", "ipv6pool"}, {"桥接口", "pool-α"}} {
		t.Run(pair[0], func(t *testing.T) {
			raw := r09Python(t, "r11_template_reference.py", pair, "")
			var want string
			if e := json.Unmarshal(raw, &want); e != nil {
				t.Fatal(e)
			}
			got, e := ipv6.IPv6PDWatchSource(pair[0], pair[1])
			if e != nil || got != want {
				t.Fatalf("template bytes differ: %v", e)
			}
			if !strings.Contains(got, "preferred-lifetime=0s valid-lifetime=10m") || !strings.Contains(got, ":local revived false") {
				t.Fatal("PD restart protections missing")
			}
		})
	}
	for _, name := range []string{"", "unsafe\x00name", `unsafe"name`, "unsafe\\name", "unsafe$name", "unsafe name", "unsafe;name", "unsafe[name]", strings.Repeat("a", 65)} {
		if _, e := ipv6.IPv6PDWatchSource(name, "pool-example"); e == nil {
			t.Fatalf("unsafe source: %q", name)
		}
	}
}
func TestR11RuntimeOwners(t *testing.T) {
	for _, feature := range []string{"pd", "dns", "readiness"} {
		for _, mode := range []string{"disconnect", "shutdown_apply", "shutdown_restore", "shutdown_empty_restore", "shutdown_after_snapshot", "shutdown_after_final_read"} {
			t.Run(feature+"/"+mode, func(t *testing.T) {
				d, f := r11Driver(t)
				p := r11PDPlan(t, d, true)
				dns := r11DNSPlan(t, d)
				s := r11StepFor("plan")
				readiness, e := ipv6.InspectIPv6Readiness(t.Context(), d.Client, r10Router(), s.Agent, s.Host, s.Configuration, "", ipv6.PolicyContext{})
				if e != nil || readiness.Plan == nil {
					t.Fatal("readiness", e)
				}
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
				rt.Start(t.Context())
				defer rt.Shutdown(t.Context())
				req, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx := rt.BindRequest(req)
				var pu *ipv6.IPv6PDGuardUndo
				var du ros.ROSUndo
				var ru *ipv6.IPv6ReadinessReceipt
				apply := func() error {
					var e error
					switch feature {
					case "pd":
						pu, e = ipv6.ApplyIPv6PDGuard(ctx, d.Client, *p.Ops[0].(*ipv6.IPv6PDGuardSync))
					case "dns":
						du, e = ipv6.ApplyIPv6DNS(ctx, d.Client, *dns.Ops[0].(*ipv6.IPv6DNSUpdate))
					case "readiness":
						ru, e = ipv6.AddMissingIPv6Route(ctx, d.Client, readiness.Plan)
					}
					return e
				}
				restore := func() error {
					switch feature {
					case "pd":
						return ipv6.RestoreIPv6PDGuard(ctx, d.Client, *pu)
					case "dns":
						raw, _ := json.Marshal(du)
						_, e := d.Apply(ctx, raw)
						return e
					default:
						return ipv6.UndoIPv6ReadinessRoute(ctx, d.Client, *ru)
					}
				}
				if mode == "disconnect" {
					cancel()
					if e := apply(); e != nil {
						t.Fatal(e)
					}
					if e := restore(); e != nil {
						t.Fatal(e)
					}
					return
				}
				if mode == "shutdown_restore" {
					if e := apply(); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "shutdown_empty_restore" {
					pu = &ipv6.IPv6PDGuardUndo{}
					du = ros.ROSUndo{Kind: "ros_restore", Changes: []ros.ROSChange{}}
					ru = &ipv6.IPv6ReadinessReceipt{}
				}
				before := f.Snapshot().Tables
				proof := d.Manifests()
				calls := len(f.Calls())
				triggered := false
				if strings.HasPrefix(mode, "shutdown_after_") {
					reads := 0
					d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
						if method != "GET" {
							return
						}
						trigger := false
						switch feature {
						case "pd":
							if strings.HasSuffix(url, ros.PathIPv6AddressLists+"?list="+pdLast) {
								reads++
								trigger = reads == 1 && mode == "shutdown_after_snapshot" || reads == 2 && mode == "shutdown_after_final_read"
							}
						case "dns":
							if strings.HasSuffix(url, ros.PathIPv6ND) {
								reads++
								trigger = reads == 1 && mode == "shutdown_after_snapshot" || reads == 3 && mode == "shutdown_after_final_read"
							}
						case "readiness":
							if strings.HasSuffix(url, ros.PathIPv6Routes) {
								reads++
								trigger = reads == 1 && mode == "shutdown_after_snapshot" || reads == 2 && mode == "shutdown_after_final_read"
							}
						}
						if trigger {
							triggered = true
							rt.Shutdown(t.Context())
						}
					}})
				} else {
					rt.Shutdown(t.Context())
				}
				if strings.Contains(mode, "restore") {
					e = restore()
				} else {
					e = apply()
				}
				if !strings.HasPrefix(mode, "shutdown_after_") && !errors.Is(e, context.Canceled) {
					t.Fatalf("owner ignored shutdown: %v", e)
				}
				if mode == "shutdown_after_snapshot" && (e == nil || !triggered) {
					t.Fatalf("snapshot boundary not canceled: %v trigger=%v", e, triggered)
				}
				if mode == "shutdown_after_final_read" && !triggered {
					t.Fatal("final read boundary unexercised")
				}
				if !strings.HasPrefix(mode, "shutdown_after_") || mode == "shutdown_after_snapshot" {
					if !reflect.DeepEqual(before, f.Snapshot().Tables) {
						t.Fatal("write after shutdown")
					}
				}
				if !reflect.DeepEqual(proof, d.Manifests()) {
					t.Fatal("manifest committed after shutdown")
				}
				if !strings.HasPrefix(mode, "shutdown_after_") && len(f.Calls()) != calls {
					t.Fatal("I/O after shutdown")
				}
			})
		}
	}
}
func TestR11PDRuntimeSweepAndIDNamespaces(t *testing.T) {
	d, f := r11Driver(t)
	r11PDApply(t, d, r11PDPlan(t, d, true))
	m := r11Manifest(t, d)
	id := m.Resources[0].ID
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{pdPrefixes: {{".id": id, "prefix": "2001:db8:2::/64", "interface": "bridge", "dynamic": "true"}, {".id": "*F1", "prefix": "2001:db8:3::/64", "interface": "bridge", "comment": "cf-pd: deprecated until 12345", "dynamic": "false"}, {".id": "*F2", "prefix": "2001:db8:4::/64", "interface": "bridge", "comment": "user", "dynamic": "false"}}, ros.PathIPv6AddressLists: {{".id": "*F3", "list": pdLast, "address": "2001:db8:1::/64"}, {".id": "*F4", "list": "geo6", "address": "2001:db8:5::/48"}}}}})
	// A hook loop already in flight writes cf-pd-last after deleting the script.
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: pdScripts + "/*65", Count: 1, AfterCommit: true, ExtraRows: map[string][]routerfake.Row{ros.PathIPv6AddressLists: {{".id": "*F5", "list": pdLast, "address": "2001:db8:1::/64"}}}}}})
	off := r11PDPlan(t, d, false)
	r11PDApply(t, d, off)
	if e := ipv6.VerifyIPv6PDGuard(t.Context(), d.Client, off.Check); e != nil {
		t.Fatal(e)
	}
	snap := f.Snapshot()
	if len(snap.Tables[ros.PathIPv6AddressLists]) != 1 || snap.Tables[ros.PathIPv6AddressLists][0]["list"] != "geo6" || len(snap.Tables[pdPrefixes]) != 2 {
		t.Fatal("sweep touched foreign or dynamic state", snap.Tables)
	}
}
func TestR11PDLostCompensationDelete(t *testing.T) {
	d, f := r11Driver(t)
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PATCH", Path: pdClients + "/*C0", Count: 1, Status: 403}, {Method: "DELETE", Path: pdSchedulers + "/*66", Count: 1, AfterCommit: true, LoseReply: true}}})
	p := r11PDPlan(t, d, true)
	_, e := ipv6.ApplyIPv6PDGuard(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6PDGuardSync))
	if e == nil || device.RouterErrorKind(e) == device.KindRollback {
		t.Fatalf("lost DELETE reply should be proven absent, retaining original failure only: %v", e)
	}
	if len(f.Snapshot().Tables[pdScripts]) != 0 || len(f.Snapshot().Tables[pdSchedulers]) != 0 || device.HasStoredState(d.IPv6PDGuardManifest()) {
		t.Fatal("compensation did not finish")
	}
}
func TestR11PDPartialRestoreRemapsProvenIDs(t *testing.T) {
	d, f := r11Driver(t)
	r11PDApply(t, d, r11PDPlan(t, d, true))
	old := r11Manifest(t, d)
	u := r11PDApply(t, d, r11PDPlan(t, d, false))
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: pdSchedulers, Count: 1, Status: 403}}})
	e := ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u)
	if device.RouterErrorKind(e) != device.KindRollback {
		t.Fatal("expected partial rollback", e)
	}
	current := r11Manifest(t, d)
	var scriptID string
	for _, r := range current.Resources {
		if r.Role == "script" {
			scriptID = r.ID
		}
	}
	if scriptID == old.Resources[0].ID || findFake(f.Snapshot().Tables[pdScripts], scriptID) == nil {
		t.Fatal("successful restore ID lost")
	}
	// Repeated recovery must never claim a same-config foreign object.
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{pdSchedulers: {{".id": "*F8", "name": pdPoll, "interval": "30s", "start-time": "startup", "on-event": pdEvent, "policy": pdPolicy, "disabled": "false"}}}}})
	if e = ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u); e == nil {
		t.Fatal("foreign named scheduler adopted during restore")
	}
}
func findFake(rows []routerfake.Row, id string) routerfake.Row {
	for _, r := range rows {
		if r[".id"] == id {
			return r
		}
	}
	return nil
}
func TestR11PDRefusedCompensationKeepsBirthProof(t *testing.T) {
	for _, externalRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "internal", true: "stage"}[externalRestore], func(t *testing.T) {
			d, f := r11Driver(t)
			var u *ipv6.IPv6PDGuardUndo
			if externalRestore {
				u = r11PDApply(t, d, r11PDPlan(t, d, true))
			}
			faults := []routerfake.Fault{{Method: "DELETE", Path: pdSchedulers + "/*66", Count: 1, Status: 403}}
			if !externalRestore {
				faults = append(faults, routerfake.Fault{Method: "PATCH", Path: pdClients + "/*C0", Count: 1, Status: 403})
			}
			f.ApplyExternal(routerfake.ExternalWrite{Faults: faults})
			var e error
			if externalRestore {
				e = ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u)
			} else {
				p := r11PDPlan(t, d, true)
				_, e = ipv6.ApplyIPv6PDGuard(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6PDGuardSync))
			}
			if device.RouterErrorKind(e) != device.KindRollback {
				t.Fatal(e)
			}
			var m ipv6.IPv6PDManifest
			if json.Unmarshal(d.IPv6PDGuardManifest(), &m) != nil {
				t.Fatal("trusted new object abandoned", string(d.IPv6PDGuardManifest()))
			}
			id := ""
			for _, r := range m.Resources {
				if r.Role == "scheduler" && r.Config != nil {
					id = r.ID
				}
			}
			if id == "" || findFake(f.Snapshot().Tables[pdSchedulers], id) == nil {
				t.Fatal("trusted new object abandoned")
			}
			if repair := r11PDPlan(t, d, true); len(repair.Blockers) > 0 || repair.Status != "pending" {
				t.Fatal("partially undone installation cannot be safely repaired", repair)
			}
			f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{pdSchedulers: {id: {"interval": "1h"}}}}})
			if p := r11PDPlan(t, d, true); len(p.Blockers) == 0 {
				t.Fatal("birth proof refreshed from external edit")
			}
		})
	}
}
func TestR11PDRejectsCorruptOwnershipAndCopies(t *testing.T) {
	for _, mutation := range []string{"version", "resources", "role", "path", "id", "duplicate", "config", "external", "copy"} {
		t.Run(mutation, func(t *testing.T) {
			d, f := r11Driver(t)
			r11PDApply(t, d, r11PDPlan(t, d, true))
			var doc map[string]any
			_ = json.Unmarshal(d.IPv6PDGuardManifest(), &doc)
			resources := doc["resources"].([]any)
			entry := resources[0].(map[string]any)
			switch mutation {
			case "version":
				doc["version"] = 2
			case "resources":
				doc["resources"] = true
			case "role":
				entry["role"] = "state"
			case "path":
				entry["path"] = "/ip/firewall/nat"
			case "id":
				entry["id"] = "*../wrong"
			case "duplicate":
				doc["resources"] = append(resources, resources[0])
			case "config":
				entry["config"] = true
			case "external":
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{pdScripts: {entry["id"].(string): {"source": ":log info mine"}}}}})
			case "copy":
				row := r09Copy(findFake(f.Snapshot().Tables[pdScripts], entry["id"].(string)))
				row[".id"] = "*FE"
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{pdScripts: {row}}}})
			}
			raw, _ := json.Marshal(doc)
			d.SetIPv6PDGuardManifest(raw)
			p := r11PDPlan(t, d, false)
			if len(p.Blockers) == 0 || len(p.Ops) != 0 {
				t.Fatal("corrupt/foreign ownership authorised write", mutation)
			}
		})
	}
}
func TestR11DNSFinalReadAndRollbackProtectsRA(t *testing.T) {
	for _, field := range []string{"dynamic", "invalid", "disabled", "ra-lifetime"} {
		t.Run(field, func(t *testing.T) {
			d, f := r11Driver(t)
			p := r11DNSPlan(t, d)
			count := 0
			d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
				if method == "GET" && strings.HasSuffix(url, ros.PathIPv6ND) {
					count++
					if count == 1 {
						value := "true"
						if field == "disabled" {
							value = "unknown"
						}
						if field == "ra-lifetime" {
							value = "1h"
						}
						f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6ND: {"*E0": {field: value}}}}})
					}
				}
			}})
			if _, e := ipv6.ApplyIPv6DNS(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6DNSUpdate)); e == nil {
				t.Fatal("late edit accepted")
			}
			if findFake(f.Snapshot().Tables[ros.PathIPv6ND], "*E0")["advertise-dns"] != "yes" {
				t.Fatal("changed edited RA")
			}
		})
	}
	d, f := r11Driver(t)
	p := r11DNSPlan(t, d)
	u, e := ipv6.ApplyIPv6DNS(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6DNSUpdate))
	if e != nil {
		t.Fatal(e)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6ND: {"*E0": {"ra-lifetime": "1h"}}}}})
	raw, _ := json.Marshal(u)
	if _, e = d.Apply(t.Context(), raw); device.RouterErrorKind(e) != device.KindRollback {
		t.Fatal("undo overwrote external RA", e)
	}
	if findFake(f.Snapshot().Tables[ros.PathIPv6ND], "*E0")["advertise-dns"] != "no" {
		t.Fatal("external RA overwritten")
	}
}
func TestR11ReadinessReusesSnapshots(t *testing.T) {
	d, f := r11Driver(t)
	s := r11StepFor("plan")
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{"/ipv6/firewall/filter": {{".id": "*F1", "chain": "forward", "action": "drop", "connection-state": "invalid", "src-address-list": "LAN list/中文"}}}}})
	p, e := ipv6.InspectIPv6Readiness(t.Context(), d.Client, r10Router(), s.Agent, s.Host, s.Configuration, "", ipv6.PolicyContext{})
	if e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	for _, c := range f.Calls() {
		counts[c.Path]++
	}
	if counts["/ipv6/firewall/filter"] != 1 || counts["/interface"] != 1 || counts[ros.PathIPv6AddressLists] != 1 {
		t.Fatal("reread snapshots", counts)
	}
	if len(r10Writes(t, f.Calls()).([]any)) != 0 {
		t.Fatal("readiness wrote")
	}
	initial := p.Fingerprint
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{ros.PathIPv6Routes: {{".id": "*F2", "dst-address": "2001:db8:9::/48", "gateway": "wan", "active": "true", "expires-after": "5m"}}}}})
	p, e = ipv6.InspectIPv6Readiness(t.Context(), d.Client, r10Router(), s.Agent, s.Host, s.Configuration, "", ipv6.PolicyContext{})
	if e != nil || p.Fingerprint == initial {
		t.Fatal("real route change not fingerprinted", e)
	}
	initial = p.Fingerprint
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {"*F2": {"expires-after": "4m"}}}}})
	p, e = ipv6.InspectIPv6Readiness(t.Context(), d.Client, r10Router(), s.Agent, s.Host, s.Configuration, "", ipv6.PolicyContext{})
	if e != nil || p.Fingerprint != initial {
		t.Fatal("lease timer changed stable fingerprint", e)
	}
}
func TestR11ReadinessRouteReceipts(t *testing.T) {
	for _, mode := range []string{"normal", "lost_put", "lost_delete", "external_edit"} {
		t.Run(mode, func(t *testing.T) {
			d, f := r11Driver(t)
			plan := ros.Row{"dst-address": "fd00:face::/64", "gateway": "fe80::4%bridge", "routing-table": "main", "disabled": "false", "distance": "1"}
			if mode == "lost_put" {
				f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, LoseReply: true}}})
			}
			u, e := ipv6.AddMissingIPv6Route(t.Context(), d.Client, plan)
			if mode == "lost_put" {
				if e == nil || len(f.Snapshot().Tables[ros.PathIPv6Routes]) != 0 {
					t.Fatal("lost write not compensated", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "lost_delete" {
				f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, LoseReply: true}}})
			}
			if mode == "external_edit" {
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {u.ID: {"gateway": "fe80::9%bridge"}}}}})
			}
			e = ipv6.UndoIPv6ReadinessRoute(t.Context(), d.Client, *u)
			if mode == "external_edit" {
				if e == nil || len(f.Snapshot().Tables[ros.PathIPv6Routes]) != 1 {
					t.Fatal("external object removed")
				}
			} else if e != nil || len(f.Snapshot().Tables[ros.PathIPv6Routes]) != 0 {
				t.Fatal("receipt undo failed", e)
			}
		})
	}
}
func TestR11RawWireAndProductionTimeouts(t *testing.T) {
	f := routerfake.New(nil)
	f.Reset(r11State())
	type wire struct{ method, path, query, body, content string }
	seen := []wire{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u, p, ok := r.BasicAuth()
		if !ok || u != "synthetic-user" || p != "synthetic-password" {
			t.Error("auth header")
		}
		mu.Lock()
		seen = append(seen, wire{r.Method, strings.TrimPrefix(r.URL.Path, "/rest"), r.URL.RawQuery, string(body), r.Header.Get("Content-Type")})
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	recorder := &r06TimeoutRecorder{client: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-user", Password: "synthetic-password"})}
	d := NewRouterOSWithHTTP(srv.URL, recorder)
	defer d.Close()
	p := r11PDPlan(t, d, true)
	u := r11PDApply(t, d, p)
	if e := ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u); e != nil {
		t.Fatal(e)
	}
	dns := r11DNSPlan(t, d)
	du, e := ipv6.ApplyIPv6DNS(t.Context(), d.Client, *dns.Ops[0].(*ipv6.IPv6DNSUpdate))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(du)
	if _, e = d.Apply(t.Context(), raw); e != nil {
		t.Fatal(e)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{"/ipv6/firewall/filter": {{".id": "*F1", "chain": "forward", "action": "drop", "src-address-list": "LAN list/中文"}}}}})
	step := r11StepFor("plan")
	if _, err := ipv6.InspectIPv6Readiness(t.Context(), d.Client, r10Router(), step.Agent, step.Host, step.Configuration, "", ipv6.PolicyContext{}); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	for _, v := range recorder.timeouts {
		if v != 10*time.Second {
			t.Errorf("production timeout overridden: %v", v)
		}
	}
	recorder.mu.Unlock()
	source, _ := ipv6.IPv6PDWatchSource("bridge", "pool-example")
	sourceJSON, _ := json.Marshal(source)
	wantScript := `{"comment":"cf-pd: IPv6 prefix change guard (ConfigFlow)","name":"cf-pd-watch","policy":"read,write,test","source":` + strings.NewReplacer(`\u0026`, "&", `\u003c`, "<", `\u003e`, ">").Replace(string(sourceJSON)) + `}`
	expected := map[string][]string{pdScripts: {wantScript}, pdSchedulers: {`{"comment":"cf-pd","disabled":"false","interval":"30s","name":"cf-pd-poll","on-event":"/system script run cf-pd-watch","policy":"read,write,test","start-time":"startup"}`}, pdClients: {`{"script":":if ($\"pd-valid\" = 1) do={:for i from=1 to=10 do={:delay 1s; /system script run cf-pd-watch}}"}`, `{"script":""}`}, ros.PathIPv6ND: {`{"advertise-dns":"no"}`, `{"advertise-dns":"yes"}`}}
	mu.Lock()
	defer mu.Unlock()
	matched := map[string]int{}
	querySeen := false
	for _, c := range seen {
		if c.path == ros.PathIPv6AddressLists && c.query == "list=LAN%20list%2F%E4%B8%AD%E6%96%87" {
			querySeen = true
		}
		if c.method == "GET" || c.method == "DELETE" {
			if c.body != "" || c.content != "" {
				t.Errorf("nonempty %s bytes: %q", c.method, c.body)
			}
			continue
		}
		if c.content != "application/json" {
			t.Fatal("content type", c.content)
		}
		for path, values := range expected {
			if c.path == path || strings.HasPrefix(c.path, path+"/") {
				if !slices.Contains(values, c.body) {
					t.Errorf("raw request bytes %s: %s", path, c.body)
				}
				matched[path]++
			}
		}
	}
	if !querySeen {
		t.Fatal("readiness query bytes not exercised")
	}
	for path := range expected {
		if matched[path] == 0 {
			t.Fatal("raw request unexercised", path)
		}
	}
}

func TestR11LegacyAndUnknownManifestRoundTrip(t *testing.T) {
	for _, mode := range []string{"legacy", "unknown", "legacy_external"} {
		t.Run(mode, func(t *testing.T) {
			d, f := r11Driver(t)
			r11PDApply(t, d, r11PDPlan(t, d, true))
			var doc map[string]json.RawMessage
			_ = json.Unmarshal(d.IPv6PDGuardManifest(), &doc)
			var resources []map[string]json.RawMessage
			_ = json.Unmarshal(doc["resources"], &resources)
			if mode == "legacy" || mode == "legacy_external" {
				for _, r := range resources {
					delete(r, "config")
				}
			} else {
				doc["future"] = json.RawMessage(`{"a":[false,1.0,null]}`)
				var spec map[string]json.RawMessage
				_ = json.Unmarshal(doc["spec"], &spec)
				spec["future"] = json.RawMessage(`{"order":[1,2]}`)
				doc["spec"], _ = json.Marshal(spec)
				for _, r := range resources {
					r["future"] = json.RawMessage(`{"safe":true}`)
				}
			}
			doc["resources"], _ = json.Marshal(resources)
			raw, _ := json.Marshal(doc)
			d.SetIPv6PDGuardManifest(raw)
			if mode == "legacy_external" {
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{pdSchedulers: {"*66": {"on-event": pdEvent + "; /system reboot"}}}}})
				if len(r11PDPlan(t, d, false).Blockers) == 0 {
					t.Fatal("legacy same-named foreign scheduler authorised")
				}
				return
			}
			p := r11PDPlan(t, d, true)
			if p.Status != "synced" {
				t.Fatal("stored manifest not compatible", p)
			}
			if mode == "unknown" {
				u := r11PDApply(t, d, r11PDPlan(t, d, false))
				if e := ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u); e != nil {
					t.Fatal(e)
				}
				var restored map[string]json.RawMessage
				_ = json.Unmarshal(d.IPv6PDGuardManifest(), &restored)
				if !bytes.Equal(restored["future"], doc["future"]) {
					t.Fatal("future metadata lost")
				}
				var entries []map[string]json.RawMessage
				_ = json.Unmarshal(restored["resources"], &entries)
				for _, r := range entries {
					if r["future"] == nil {
						t.Fatal("resource unknown lost")
					}
				}
				// The model-backed stored document retains this new proof as an unknown member
				// of the legacy ID resource, across an unrelated saved display-name edit.
				source := append([]byte(`{"id":"synthetic","name":"Before","ipv6_pd_guard_state":`), d.IPv6PDGuardManifest()...)
				source = append(source, '}')
				var modelRouter model.Router
				if e := json.Unmarshal(source, &modelRouter); e != nil {
					t.Fatal(e)
				}
				modelRouter.Name = "After"
				saved, e := json.Marshal(modelRouter)
				if e != nil {
					t.Fatal(e)
				}
				var stored struct {
					State json.RawMessage `json:"ipv6_pd_guard_state"`
				}
				_ = json.Unmarshal(saved, &stored)
				if !reflect.DeepEqual(r08Comparable(t, stored.State), r08Comparable(t, d.IPv6PDGuardManifest())) {
					t.Fatal("model save lost stable proofs")
				}
			}
		})
	}
}
func TestR11ReportsAndManifestCopies(t *testing.T) {
	d, _ := r11Driver(t)
	dns, pd := r11DNSPlan(t, d), r11PDPlan(t, d, true)
	r11PDApply(t, d, pd)
	public := ipv6.DNSPublic(dns)
	public.Items[0].DNSServers[0] = "2001:db8::99"
	public.Items[0].Status = "changed"
	if dns.Items[0].DNSServers[0] != "2001:db8::53" || dns.Items[0].Status != "pending" {
		t.Fatal("report aliases private plan")
	}
	view := ipv6.PDPublic(pd)
	view.Items[0].Status = "changed"
	if pd.Items[0].Status == "changed" {
		t.Fatal("PD report aliases plan")
	}
	manifests := d.Manifests()
	manifests.IPv6PDGuard.Value[0] = '['
	if d.Manifests().IPv6PDGuard.Value[0] != '{' {
		t.Fatal("manifest aliases stored receipt")
	}
	section, _ := json.Marshal(ipv6.DNSPublic(dns))
	for _, field := range []string{`"ops"`, `"check"`, `"evidence"`, `"expected"`} {
		if bytes.Contains(section, []byte(field)) {
			t.Fatal("report leaks internal proof", field)
		}
	}
}
func TestR11DeviceErrorsRedactOnlyCredentials(t *testing.T) {
	for _, echo := range []string{"username=api rest-api", "username=api rest-api synthetic-password"} {
		t.Run(echo, func(t *testing.T) {
			d, f := r11Driver(t)
			f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: pdScripts, Count: 1, Status: 400, Body: routerfake.Row{"message": echo}}}})
			p := r11PDPlan(t, d, true)
			_, e := ipv6.ApplyIPv6PDGuard(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6PDGuardSync))
			if e == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(e.Error(), "synthetic-password") {
				t.Fatal("secret escaped", e)
			}
			if !strings.Contains(echo, "synthetic-password") && (!strings.Contains(e.Error(), "username=api") || !strings.Contains(e.Error(), "rest-api")) {
				t.Fatal("username treated as secret", e)
			}
		})
	}
}

// FuzzR11PDInput exercises untrusted stored proofs and script parameters without I/O.
func FuzzR11PDInput(f *testing.F) {
	for _, s := range []string{`null`, `{}`, `{"version":1,"resources":[]}`, `{"version":1,"resources":[{"role":"script","path":"/system/script","id":"*50","config":false}]}`, "bridge", "unsafe\"$;{}[]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16384 {
			t.Skip()
		}
		d := &RouterOS{}
		_, _ = ipv6.PDOwned(d.Client, json.RawMessage(input), ipv6.PDLive{})
		_, _ = ipv6.IPv6PDWatchSource(input, "pool-example")
		_ = ipv6.PDNormalize(input)
		_ = ipv6.PDParameters(input)
		if source, e := ipv6.IPv6PDWatchSource(input, "pool-example"); e == nil {
			if !ipv6.PDName(input) || !strings.HasPrefix(source, `:local lan "`+input+`"`+"\n") {
				t.Fatal("unsafe script parameter emitted")
			}
		}
	})
}

// FuzzR11DNSRow exercises strict flags, legacy server list forms and CLI quoting.
func FuzzR11DNSRow(f *testing.F) {
	for _, s := range []string{`{}`, `{"dns":["2001:db8::53"]}`, `{"disabled":"unknown"}`, `{"dynamic":true}`, `{"dynamic":"1"}`, `{"invalid":"1"}`, `{"dns-servers":[{}]}`, `{"dns-servers":null}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16384 {
			t.Skip()
		}
		var row ros.Row
		if json.Unmarshal([]byte(input), &row) != nil {
			return
		}
		for _, key := range []string{"disabled", "invalid", "dynamic"} {
			_, _ = ros.IPv6DNSFlag(row, key)
		}
		_, _ = ipv6.DNSServers(row)
		_ = ipv6.DNSCLI(row["dns"])
		if p := ros.Payload(ros.PathIPv6ND, row); p != nil {
			for _, key := range []string{"disabled", "invalid", "dynamic"} {
				if flag, e := ros.IPv6DNSFlag(row, key); e != nil || flag && key != "disabled" {
					t.Fatal("unrecognised or noneditable flag became writable")
				}
			}
		}
	})
}
func TestR11BuildReportSections(t *testing.T) {
	for _, status := range []string{"disabled", "pending", "synced", "borrowed", "blocked"} {
		t.Run(status, func(t *testing.T) {
			desired := r10Wanted(0)
			desired.ProxyGateway = "192.0.2.4"
			state := &RouterState{IPv6DNSPlan: &ipv6.IPv6DNSPlan{IPv6DNSReport: ipv6.IPv6DNSReport{Status: status, Items: []ipv6.IPv6DNSItem{}, Blockers: []string{}, Warnings: []string{}}, Ops: []device.Op{}}, IPv6PDGuardPlan: &ipv6.IPv6PDGuardPlan{IPv6PDGuardReport: ipv6.IPv6PDGuardReport{Status: status, Items: []ipv6.IPv6PDItem{}, Blockers: []string{}, Warnings: []string{}}, Ops: []device.Op{}}}
			report, e := BuildReport(desired, state)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(report)
			var doc map[string]json.RawMessage
			_ = json.Unmarshal(raw, &doc)
			if doc["ipv6_dns"] == nil || doc["ipv6_pd_guard"] == nil {
				t.Fatal("missing report section")
			}
			if status == "blocked" && report.Summary["unknown"] < 1 {
				t.Fatal("PD blocked summary must be unknown")
			}
			if status == "pending" && report.Summary["pending"] != 2 {
				t.Fatal("pending stages missing from report", report.Summary)
			}
		})
	}
}
func TestR11DNSUnknownAndNoopEvidence(t *testing.T) {
	for _, field := range []string{"disabled", "invalid", "dynamic"} {
		for _, value := range []any{nil, "unknown", []any{}, map[string]any{}} {
			t.Run(field+"/"+fmt.Sprint(value), func(t *testing.T) {
				d, f := r11Driver(t)
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6ND: {"*E0": {field: value}}}}})
				p := r11DNSPlan(t, d)
				if p.Status != "blocked" || len(p.Ops) > 0 {
					t.Fatal("unknown flag authorised write")
				}
			})
		}
	}
	d, f := r11Driver(t)
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{RemoveIDs: map[string][]string{ros.PathIPv6ND: {"*E0"}}}})
	p := r11DNSPlan(t, d)
	if p.Status != "synced" {
		t.Fatal(p)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{ros.PathIPv6ND: {{".id": "*E1", "interface": "bridge", "advertise-dns": "yes"}}}}})
	if e := ipv6.VerifyIPv6DNS(t.Context(), d.Client, p.Check); e == nil {
		t.Fatal("noop failed to detect new DNS")
	}
}
func TestR11PDCountersAndLeaseEvidence(t *testing.T) {
	d, f := r11Driver(t)
	r11PDApply(t, d, r11PDPlan(t, d, true))
	first := r11PDPlan(t, d, true)
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{pdScripts: {"*65": {"run-count": "100", "last-started": "synthetic now"}}, pdSchedulers: {"*66": {"run-count": "99", "next-run": "synthetic later"}}, pdClients: {"*C0": {"prefix": "2001:db8:1::/60, 23h59m", "status": "bound", "expires-after": "23h59m"}}}}})
	next := r11PDPlan(t, d, true)
	if !reflect.DeepEqual(r08Comparable(t, first), r08Comparable(t, next)) {
		t.Fatal("runtime counters or lease state changed release evidence")
	}
}
func TestR11PDWithdrawalDetectsLateRuntimeState(t *testing.T) {
	for _, path := range []string{ros.PathIPv6AddressLists, pdPrefixes} {
		t.Run(path, func(t *testing.T) {
			d, f := r11Driver(t)
			r11PDApply(t, d, r11PDPlan(t, d, true))
			p := r11PDPlan(t, d, false)
			r11PDApply(t, d, p)
			row := routerfake.Row{".id": "*FA", "list": pdLast, "address": "2001:db8:1::/64"}
			if path == pdPrefixes {
				row = routerfake.Row{".id": "*FA", "prefix": "2001:db8:2::/64", "interface": "bridge", "comment": "cf-pd: deprecated until 12345", "dynamic": "false"}
			}
			f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{path: {row}}}})
			if e := ipv6.VerifyIPv6PDGuard(t.Context(), d.Client, p.Check); e == nil {
				t.Fatal("late runtime state reported as withdrawn")
			}
		})
	}
}
func TestR11PDStateCleanupDoesNotReadGeoIPLists(t *testing.T) {
	d, f := r11Driver(t)
	r11PDApply(t, d, r11PDPlan(t, d, true))
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{ros.PathIPv6AddressLists: {{".id": "*FA", "list": pdLast, "address": "2001:db8:1::/64"}, {".id": "*FB", "list": "geo6", "address": "2001:db8:9::/48"}}}}})
	p := r11PDPlan(t, d, false)
	u := r11PDApply(t, d, p)
	if e := ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u); e != nil {
		t.Fatal(e)
	}
	for _, c := range f.Calls() {
		if c.Method == "GET" && c.Path == ros.PathIPv6AddressLists && c.Query.Get("list") != pdLast {
			t.Fatal("PD transaction fetched the unbounded global IPv6 list", c)
		}
	}
	if findFake(f.Snapshot().Tables[ros.PathIPv6AddressLists], "*FB") == nil {
		t.Fatal("foreign GeoIP entry modified")
	}
}
func TestR11PDRestoredReplySurvivesFailedReadback(t *testing.T) {
	d, f := r11Driver(t)
	r11PDApply(t, d, r11PDPlan(t, d, true))
	previous := r11Manifest(t, d)
	u := r11PDApply(t, d, r11PDPlan(t, d, false))
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "GET", Path: pdScripts, Count: 3, Status: 403}}})
	if e := ipv6.RestoreIPv6PDGuard(t.Context(), d.Client, *u); device.RouterErrorKind(e) != device.KindRollback {
		t.Fatal("unverified restore reported success", e)
	}
	m := r11Manifest(t, d)
	if m.Resources[0].ID == previous.Resources[0].ID || findFake(f.Snapshot().Tables[pdScripts], m.Resources[0].ID) == nil {
		t.Fatal("response-proven restoration lost after read failure")
	}
	if !reflect.DeepEqual(m.Resources[0].Config, previous.Resources[0].Config) {
		t.Fatal("restore replaced original config proof")
	}
	// New independent reads can prove the actual successful restoration.
	f.ApplyExternal(routerfake.ExternalWrite{})
	if p := r11PDPlan(t, d, true); p.Status != "synced" {
		t.Fatal("valid restored installation lost ownership", p)
	}
}
