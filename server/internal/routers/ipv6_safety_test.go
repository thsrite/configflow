package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

func r10Driver(t *testing.T) (*RouterOS, *routerfake.Fake) {
	t.Helper()
	f := routerfake.New(nil)
	f.Reset(r10State())
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	d := NewRouterOS(s.URL, "synthetic-user", "synthetic-password", false)
	t.Cleanup(d.Close)
	return d, f
}
func r10FakePlan(t *testing.T, d *RouterOS) *ipv6.IPv6FakeIPPlan {
	t.Helper()
	r := r10Router()
	if d.IPv6FakeIPManifestSet() {
		r.Extra["ipv6_fakeip_state"] = ros.GuardRaw(d.IPv6FakeIPManifest())
	}
	return ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r, r10Agent(), r10Host(), d.Client)
}
func r10RoutesPlan(t *testing.T, d *RouterOS, n int) *ipv6.IPv6RoutesPlan {
	t.Helper()
	r := r10Router()
	if d.IPv6RoutesManifestSet() {
		r.Extra["ipv6_routes_state"] = ros.GuardRaw(d.IPv6RoutesManifest())
	}
	return ipv6.InspectIPv6Routes(t.Context(), r10Config(r10YAML), r, r10Wanted(n), r10Agent(), r10Host(), d.Client)
}

func TestR10RawWireAndProductionTimeouts(t *testing.T) {
	f := routerfake.New(nil)
	f.Reset(r10State())
	type wire struct{ method, path, query, body, content string }
	seen := []wire{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u, p, ok := r.BasicAuth()
		if !ok || u != "synthetic-user" || p != "synthetic-password" {
			t.Error("Basic auth missing")
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
	p := r10FakePlan(t, d)
	undo, err := ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace))
	if err != nil {
		t.Fatal(err)
	}
	if err = ipv6.RestoreIPv6FakeIP(t.Context(), d.Client, *undo); err != nil {
		t.Fatal(err)
	}
	rp := r10RoutesPlan(t, d, 4)
	ru, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *rp.Ops[0].(*ipv6.IPv6RoutesReplace))
	if err != nil {
		t.Fatal(err)
	}
	if err = ipv6.RestoreIPv6Routes(t.Context(), d.Client, *ru); err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{"/ipv6/firewall/filter": {{".id": "*F", "chain": "forward", "action": "drop", "src-address-list": "Trusted LAN/中文"}}}}})
	ipv6.CheckForwardHint(t.Context(), d.Client, 6, "bridge", []string{"fd00:face::/64"}, nil)
	recorder.mu.Lock()
	for _, timeout := range recorder.timeouts {
		if timeout != 10*time.Second {
			t.Errorf("timeout replaced: %v", timeout)
		}
	}
	recorder.mu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	put, console, remove, query := false, false, false, false
	for _, call := range seen {
		if call.method == "GET" || call.method == "DELETE" {
			if call.body != "" || call.content != "" {
				t.Errorf("bodyless request: %+v", call)
			}
		} else if call.content != "application/json" {
			t.Errorf("content type: %+v", call)
		}
		if call.method == "PUT" && call.path == ros.PathIPv6Routes && strings.Contains(call.body, "cf-fakeip6:") {
			put = true
			if call.body != `{"comment":"cf-fakeip6:synthetic","disabled":"false","distance":"1","dst-address":"fd00:face::/64","gateway":"fe80::4%bridge","routing-table":"main"}` {
				t.Errorf("raw PUT: %s", call.body)
			}
		}
		if call.method == "POST" && call.path == "/execute" {
			console = true
			expected := `{"as-string":"","script":":local f 0\n:if ($f=0) do={:onerror e in={:put (\"cf-id:0:\" . [/ipv6/route/add dst-address=\"2001:db8:a::/48\" gateway=\"fe80::4%bridge\" comment=\"cf-rule6:synthetic\" disabled=no])} do={:set f 1; :put (\"cf-err:0:\" . $e)}}\n:if ($f=0) do={:onerror e in={:put (\"cf-id:1:\" . [/ipv6/route/add dst-address=\"2001:db8:b::/48\" gateway=\"fe80::4%bridge\" comment=\"cf-rule6:synthetic\" disabled=no])} do={:set f 1; :put (\"cf-err:1:\" . $e)}}\n:if ($f=0) do={:onerror e in={:put (\"cf-id:2:\" . [/ipv6/route/add dst-address=\"2001:db8:c::/48\" gateway=\"fe80::4%bridge\" comment=\"cf-rule6:synthetic\" disabled=no])} do={:set f 1; :put (\"cf-err:2:\" . $e)}}\n:if ($f=0) do={:onerror e in={:put (\"cf-id:3:\" . [/ipv6/route/add dst-address=\"2001:db8:d::/48\" gateway=\"fe80::4%bridge\" comment=\"cf-rule6:synthetic\" disabled=no])} do={:set f 1; :put (\"cf-err:3:\" . $e)}}\n:put \"cf-done\""}`
			if call.body != expected {
				t.Errorf("raw console request differs: %s", call.body)
			}
		}
		if call.method == "POST" && call.path == ros.PathIPv6Routes+"/remove" {
			remove = true
			if call.body != `{".id":"*69,*68,*67,*66"}` {
				t.Errorf("raw remove: %s", call.body)
			}
		}
		if call.path == "/ipv6/firewall/address-list" {
			query = true
			if call.query != "list=Trusted%20LAN%2F%E4%B8%AD%E6%96%87" {
				t.Errorf("query raw bytes: %q", call.query)
			}
		}
	}
	if !put || !console || !remove || !query {
		t.Fatalf("unexercised raw path put=%t console=%t remove=%t query=%t", put, console, remove, query)
	}
}

func TestR10RuntimeCancellationBoundaries(t *testing.T) {
	for _, feature := range []string{"fake", "routes"} {
		for _, action := range []string{"disconnect", "shutdown_apply", "shutdown_restore", "shutdown_empty_restore", "shutdown_after_snapshot", "shutdown_after_final_read"} {
			t.Run(feature+"/"+action, func(t *testing.T) {
				d, f := r10Driver(t)
				fp := r10FakePlan(t, d)
				rp := r10RoutesPlan(t, d, 4)
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
				runtime.Start(t.Context())
				defer runtime.Shutdown(t.Context())
				req, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx := runtime.BindRequest(req)
				var fu *ipv6.IPv6FakeIPUndo
				var ru *ipv6.IPv6RoutesUndo
				var err error
				apply := func() error {
					if feature == "fake" {
						fu, err = ipv6.ApplyIPv6FakeIP(ctx, d.Client, *fp.Ops[0].(*ipv6.IPv6FakeIPReplace))
					} else {
						ru, err = ipv6.ApplyIPv6Routes(ctx, d.Client, *rp.Ops[0].(*ipv6.IPv6RoutesReplace))
					}
					return err
				}
				restore := func() error {
					if feature == "fake" {
						return ipv6.RestoreIPv6FakeIP(ctx, d.Client, *fu)
					}
					return ipv6.RestoreIPv6Routes(ctx, d.Client, *ru)
				}
				if action == "disconnect" {
					cancel()
					if err := apply(); err != nil {
						t.Fatal("disconnect aborted apply", err)
					}
					if err := restore(); err != nil {
						t.Fatal("disconnect aborted restore", err)
					}
					return
				}
				if action == "shutdown_restore" {
					if err := apply(); err != nil {
						t.Fatal(err)
					}
				}
				if action == "shutdown_empty_restore" {
					fu = &ipv6.IPv6FakeIPUndo{}
					ru = &ipv6.IPv6RoutesUndo{Undos: []json.RawMessage{}}
				}
				before := f.Snapshot().Tables
				proof := d.Manifests()
				calls := len(f.Calls())
				if strings.HasPrefix(action, "shutdown_after_") {
					count := 0
					d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
						if method != "GET" {
							return
						}
						if action == "shutdown_after_snapshot" && strings.HasSuffix(url, rosIPv4Addresses) {
							runtime.Shutdown(t.Context())
						}
						if action == "shutdown_after_final_read" && (strings.HasSuffix(url, ros.PathIPv6Routes) && feature == "routes" || strings.Contains(url, ros.PathIPv6Routes+"/*") && feature == "fake") {
							count++
							limit := 2
							if feature == "routes" {
								limit = 2
							}
							if count == limit {
								runtime.Shutdown(t.Context())
							}
						}
					}})
				} else {
					runtime.Shutdown(t.Context())
				}
				if strings.Contains(action, "restore") {
					err = restore()
				} else {
					err = apply()
				}
				if err == nil {
					t.Fatal("shutdown owner succeeded")
				}
				if !strings.HasPrefix(action, "shutdown_after_") && !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation classification", err)
				}
				if !strings.HasPrefix(action, "shutdown_after_") || action == "shutdown_after_snapshot" {
					if !reflect.DeepEqual(before, f.Snapshot().Tables) {
						t.Fatal("shutdown changed device")
					}
				}
				if !reflect.DeepEqual(proof, d.Manifests()) {
					t.Fatal("shutdown committed manifest")
				}
				if !strings.HasPrefix(action, "shutdown_after_") && len(f.Calls()) != calls {
					t.Fatal("shutdown performed I/O")
				}
			})
		}
	}
}

func TestR10OwnershipAndFreshnessGuards(t *testing.T) {
	for _, feature := range []string{"fake", "routes"} {
		t.Run(feature, func(t *testing.T) {
			d, f := r10Driver(t)
			fp := r10FakePlan(t, d)
			rp := r10RoutesPlan(t, d, 4)
			var err error
			if feature == "fake" {
				_, err = ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *fp.Ops[0].(*ipv6.IPv6FakeIPReplace))
			} else {
				_, err = ipv6.ApplyIPv6Routes(t.Context(), d.Client, *rp.Ops[0].(*ipv6.IPv6RoutesReplace))
			}
			if err != nil {
				t.Fatal(err)
			}
			var raw json.RawMessage
			if feature == "fake" {
				raw = d.IPv6FakeIPManifest()
			} else {
				raw = d.IPv6RoutesManifest()
			}
			for _, change := range []string{"comment", "gateway", "dynamic", "connect", "id_reused"} {
				before := f.Snapshot()
				row := before.Tables[ros.PathIPv6Routes][0]
				patch := routerfake.Row{change: "true"}
				if change == "comment" {
					patch[change] = "outside"
				}
				if change == "gateway" {
					patch[change] = "fe80::9%bridge"
				}
				if change == "id_reused" {
					patch = routerfake.Row{"dst-address": "2001:db8:ffff::/48"}
				}
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {row[".id"].(string): patch}}}})
				live, _ := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
				if _, err := ipv6.Owned(raw, live, feature == "fake"); err == nil {
					t.Fatalf("accepted external %s", change)
				}
				f.Reset(before)
			}
			for _, bad := range []string{`"invalid"`, `{"version":1,"resources":null}`, `{"version":1,"gateway":"x","resources":[{"path":"/ipv6/route","id":"","config":{}}]}`, `{"version":1,"spec":{},"resources":[{"path":"/ipv6/route","id":"*1","config":{}}]}`} {
				live, _ := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
				if _, err := ipv6.Owned(json.RawMessage(bad), live, feature == "fake"); err == nil {
					t.Errorf("malformed proof accepted: %s", bad)
				}
			}
			// The preview must bind actual-interface and host bits, but not lifetimes.
			for _, field := range []string{"address", "actual-interface", "valid"} {
				state := f.Snapshot()
				var apply func() error
				if feature == "fake" {
					r := r10Router()
					r.Extra["ipv6_fakeip_state"] = raw
					p := ipv6.InspectIPv6FakeIP(t.Context(), r10Config("ipv6: false"), r, nil, ipv6.IPv6Host{}, d.Client)
					apply = func() error {
						_, e := ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace))
						return e
					}
					if field != "valid" {
						continue
					}
				} else {
					p := r10RoutesPlan(t, d, 2)
					apply = func() error {
						_, e := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace))
						return e
					}
				}
				value := "counting down"
				path, id := rosIPv6Addresses, "*A1"
				if field == "address" {
					value = "fd00:1::2/64"
				}
				if field == "actual-interface" {
					value = "other"
					path, id = rosIPv4Addresses, "*B1"
				}
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{path: {id: {field: value}}}}})
				err = apply()
				if field == "valid" && err != nil {
					t.Fatal("volatile lifetime invalidated preview", err)
				}
				if field != "valid" && (err == nil || err.Error() != messageIPv6PreviewChanged) {
					t.Fatal("stale preview accepted", err)
				}
				f.Reset(state)
			}
		})
	}
	var config model.Profile
	_ = json.Unmarshal([]byte(`{"mihomo":{"custom_config":7}}`), &config)
	if ipv6.IPv6FakeIPSettings(&config).Error == "" || ipv6.IPv6RoutesSettings(&config).Error == "" {
		t.Fatal("model fallback treated as disabled ownership proof")
	}
}

func TestR10PartialRollbackKeepsTrustedRestoredIDs(t *testing.T) {
	d, f := r10Driver(t)
	plan := r10RoutesPlan(t, d, 3)
	if _, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *plan.Ops[0].(*ipv6.IPv6RoutesReplace)); err != nil {
		t.Fatal(err)
	}
	original := ros.GuardRaw(d.IPv6RoutesManifest())
	withdraw := r10RoutesPlan(t, d, 0)
	undo, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *withdraw.Ops[0].(*ipv6.IPv6RoutesReplace))
	if err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: ros.PathIPv6Routes, Count: 2, Status: 403}}})
	if err = ipv6.RestoreIPv6Routes(t.Context(), d.Client, *undo); err == nil {
		t.Fatal("expected partial compensation failure")
	}
	var before, after ipv6.IPv6RoutesManifest
	_ = json.Unmarshal(original, &before)
	_ = json.Unmarshal(d.IPv6RoutesManifest(), &after)
	live, _ := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
	owned, err := ipv6.Owned(d.IPv6RoutesManifest(), live, false)
	if err != nil || len(owned) != 2 {
		t.Fatalf("restored objects lost ownership: %s %v", d.IPv6RoutesManifest(), err)
	}
	for _, entry := range owned {
		for _, old := range before.Resources {
			if old.Config.Text("dst-address") == entry.Config.Text("dst-address") && old.ID == entry.ID {
				t.Fatal("restored resource retains deleted ID")
			}
		}
	}
	again := r10RoutesPlan(t, d, 3)
	if len(again.Skip) > 0 || len(again.Ops) != 1 {
		t.Fatalf("partial rollback cannot be reconciled: %s", device.JSON(again))
	}
}

func TestR10RollbackNeverAdoptsMatchingForeignRoute(t *testing.T) {
	d, f := r10Driver(t)
	p := r10RoutesPlan(t, d, 1)
	if _, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace)); err != nil {
		t.Fatal(err)
	}
	previous := ros.GuardRaw(d.IPv6RoutesManifest())
	p = r10RoutesPlan(t, d, 0)
	undo, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace))
	if err != nil {
		t.Fatal(err)
	}
	var manifest ipv6.IPv6RoutesManifest
	_ = json.Unmarshal(previous, &manifest)
	row := fakeRow(manifest.Resources[0].Config)
	row[".id"] = "*AA"
	row["active"] = "true"
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{ros.PathIPv6Routes: {row}}}})
	if err = ipv6.RestoreIPv6Routes(t.Context(), d.Client, *undo); err == nil {
		t.Fatal("foreign same-destination route did not stop restore")
	}
	if bytes.Contains(d.IPv6RoutesManifest(), []byte("*AA")) {
		t.Fatal("foreign route adopted by config equality")
	}
}

func TestR10RollbackRemapDoesNotBlessExternalEdit(t *testing.T) {
	d, f := r10Driver(t)
	p := r10RoutesPlan(t, d, 1)
	if _, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace)); err != nil {
		t.Fatal(err)
	}
	previous := ros.GuardRaw(d.IPv6RoutesManifest())
	var m ipv6.IPv6RoutesManifest
	_ = json.Unmarshal(previous, &m)
	id := m.Resources[0].ID
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {id: {"comment": "external edit"}}}}})
	if err := ipv6.RemapIPv6(t.Context(), d.Client, previous, map[string]string{id: id}, false); err != nil {
		t.Fatal(err)
	}
	live, _ := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
	if _, err := ipv6.Owned(d.IPv6RoutesManifest(), live, false); err == nil {
		t.Fatal("remap blessed external configuration")
	}
}

func TestR10HostDiagnosticsRedactCredentials(t *testing.T) {
	d, _ := r10Driver(t)
	agent := r10Agent()
	agent.Token = "synthetic-agent-token"
	host := r10Host()
	host.Error = "rest-api username=api password=synthetic-password token=synthetic-agent-token"
	p := ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r10Router(), agent, host, d.Client)
	rp := ipv6.InspectIPv6Routes(t.Context(), r10Config(r10YAML), r10Router(), r10Wanted(1), agent, host, d.Client)
	for _, value := range []any{p, rp} {
		raw := device.JSON(value)
		if bytes.Contains(raw, []byte("synthetic-password")) || bytes.Contains(raw, []byte(agent.Token)) {
			t.Fatal("credential escaped diagnostic", string(raw))
		}
		if !bytes.Contains(raw, []byte("username=api")) || !bytes.Contains(raw, []byte("rest-api")) {
			t.Fatal("username treated as a secret", string(raw))
		}
	}
}

func TestR10ManifestSpecAndUnknownFields(t *testing.T) {
	d, _ := r10Driver(t)
	p := r10FakePlan(t, d)
	if _, err := ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace)); err != nil {
		t.Fatal(err)
	}
	var manifest map[string]json.RawMessage
	_ = json.Unmarshal(d.IPv6FakeIPManifest(), &manifest)
	manifest["audit_note"] = json.RawMessage(`{"future":["keep"]}`)
	var entries []map[string]json.RawMessage
	_ = json.Unmarshal(manifest["resources"], &entries)
	entries[0]["future_resource"] = json.RawMessage(`{"keep":true}`)
	manifest["resources"], _ = json.Marshal(entries)
	raw := device.JSON(manifest)
	r := r10Router()
	r.Extra["ipv6_fakeip_state"] = raw
	p = ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r, r10Agent(), r10Host(), d.Client)
	if p.Status != "synced" || len(p.Ops) > 0 || !ros.JSONValuesEqual(p.Check.Previous, raw) {
		t.Fatal("noop lost previous manifest fingerprint", string(device.JSON(p)))
	}
	var spec ipv6.IPv6RouteSpec
	_ = json.Unmarshal(manifest["spec"], &spec)
	spec.Gateway = "fe80::99%bridge"
	manifest["spec"] = device.JSON(spec)
	r.Extra["ipv6_fakeip_state"] = device.JSON(manifest)
	p = ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r, r10Agent(), r10Host(), d.Client)
	if len(p.Blockers) == 0 || len(p.Ops) > 0 {
		t.Fatal("corrupted spec became successful noop")
	}
	if err := ipv6.RemapIPv6(t.Context(), d.Client, raw, map[string]string{}, true); err != nil {
		t.Fatal(err)
	}
	if !ros.JSONValuesEqual(d.IPv6FakeIPManifest(), raw) {
		t.Fatal("unknown manifest/resource fields lost")
	}
}

func TestR10RestoreFailureKeepsPublishedOwnership(t *testing.T) {
	for _, feature := range []string{"fake", "routes"} {
		t.Run(feature, func(t *testing.T) {
			d, f := r10Driver(t)
			var undo json.RawMessage
			if feature == "fake" {
				p := r10FakePlan(t, d)
				raw, err := d.Apply(t.Context(), device.JSON(p.Ops[0]))
				if err != nil {
					t.Fatal(err)
				}
				undo = raw
			} else {
				p := r10RoutesPlan(t, d, 1)
				if _, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace)); err != nil {
					t.Fatal(err)
				}
				p = r10RoutesPlan(t, d, 2)
				u, err := ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace))
				if err != nil {
					t.Fatal(err)
				}
				undo = device.JSON(u)
			}
			before := d.Manifests()
			f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: ros.PathIPv6Routes + "/*65", Status: 403}, {Method: "DELETE", Path: ros.PathIPv6Routes + "/*66", Status: 403}}})
			if _, err := d.Apply(t.Context(), undo); err == nil {
				t.Fatal("expected refused rollback")
			}
			live, _ := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
			raw := d.Manifests().IPv6FakeIP.Value
			if feature == "routes" {
				raw = d.Manifests().IPv6Routes.Value
			}
			owned, err := ipv6.Owned(raw, live, feature == "fake")
			if err != nil || len(owned) != len(live["routes"]) {
				t.Fatalf("failed restore abandoned published proof: %s %v", raw, err)
			}
			if feature == "fake" && !ros.JSONValuesEqual(before.IPv6FakeIP.Value, raw) {
				t.Fatal("failed Fake-IP rollback replaced current publication")
			}
		})
	}
}

func TestR10FailedApplyRetainsProvenCreationWhenDeleteRefused(t *testing.T) {
	for _, feature := range []string{"fake", "routes"} {
		t.Run(feature, func(t *testing.T) {
			d, f := r10Driver(t)
			faults := []routerfake.Fault{{Method: "DELETE", Path: ros.PathIPv6Routes + "/*65", Status: 403}}
			if feature == "fake" {
				faults = append(faults, routerfake.Fault{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {"*65": {"active": "false"}}}})
			} else {
				faults = append(faults, routerfake.Fault{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, PatchCreated: map[int]routerfake.Row{0: {"active": "false"}}})
			}
			f.ApplyExternal(routerfake.ExternalWrite{Faults: faults})
			var err error
			if feature == "fake" {
				p := r10FakePlan(t, d)
				_, err = ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace))
			} else {
				p := r10RoutesPlan(t, d, 4)
				_, err = ipv6.ApplyIPv6Routes(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6RoutesReplace))
			}
			if err == nil || device.RouterErrorKind(err) != device.KindRollback {
				t.Fatalf("expected incomplete rollback, got %v", err)
			}
			live, e := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
			if e != nil {
				t.Fatal(e)
			}
			raw := d.IPv6FakeIPManifest()
			if feature == "routes" {
				raw = d.IPv6RoutesManifest()
			}
			owned, e := ipv6.Owned(raw, live, feature == "fake")
			if e != nil || len(owned) == 0 {
				t.Fatalf("trusted new object abandoned: manifest=%s err=%v", raw, e)
			}
			for _, row := range live["routes"] {
				found := false
				for _, entry := range owned {
					found = found || entry.ID == row.ID()
				}
				if !found {
					t.Fatalf("unowned residual route %s", row.ID())
				}
			}
		})
	}
}

func TestR10BorrowedRouteMustRemainActive(t *testing.T) {
	for _, feature := range []string{"fake", "routes"} {
		t.Run(feature, func(t *testing.T) {
			d, f := r10Driver(t)
			state := r10State()
			cidr := "fd00:face::/64"
			if feature == "routes" {
				cidr = r10Wanted(1).IPv6CIDRs[0].CIDR
			}
			state.Tables[ros.PathIPv6Routes] = []routerfake.Row{r10Route("*50", cidr, "fe80::4%bridge")}
			f.Reset(state)
			var verify func() error
			if feature == "fake" {
				p := r10FakePlan(t, d)
				if p.Status != "borrowed" {
					t.Fatal("not borrowed", p)
				}
				verify = func() error { return ipv6.VerifyIPv6FakeIP(t.Context(), d.Client, &p.Check) }
			} else {
				p := r10RoutesPlan(t, d, 1)
				if len(p.Check.Borrowed) != 1 {
					t.Fatal("not borrowed", p)
				}
				verify = func() error { return ipv6.VerifyIPv6Routes(t.Context(), d.Client, p.Check) }
			}
			if err := verify(); err != nil {
				t.Fatal("active borrowed route failed", err)
			}
			f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {"*50": {"active": "false"}}}}})
			if err := verify(); err == nil {
				t.Fatal("inactive borrowed route reported verified")
			}
		})
	}
}
