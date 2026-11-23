package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

type r09Wire struct{ Method, Path, Body, ContentType, Authorization string }
type r09TimeoutHTTP struct {
	DeviceHTTP
	mu       sync.Mutex
	timeouts []time.Duration
	cancel   context.CancelFunc
}

func (c *r09TimeoutHTTP) Do(ctx context.Context, method, url string, body any, timeout time.Duration) (*devhttp.Response, error) {
	c.mu.Lock()
	c.timeouts = append(c.timeouts, timeout)
	c.mu.Unlock()
	response, err := c.DeviceHTTP.Do(ctx, method, url, body, timeout)
	if method == "PUT" && c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return response, err
}
func r09Plan(t *testing.T, d *RouterOS, settings DNSGuard, previous json.RawMessage) *DNSGuardPlan {
	t.Helper()
	tc := r09Base("plan")
	state, err := d.ReadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state.DNSGuard, err = SnapshotDNSGuard(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	tc.Desired.DNSGuard = &DesiredDNSGuard{Settings: settings, Previous: previous}
	return BuildDNSGuard(tc.Desired, state)
}
func r09ApplyPlan(t *testing.T, d *RouterOS, p *DNSGuardPlan) *DNSGuardUndo {
	t.Helper()
	if len(p.Blockers) > 0 || len(p.Ops) != 1 {
		t.Fatalf("bad plan %s", r09Raw(p))
	}
	undo, err := d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
	if err != nil {
		t.Fatal(err)
	}
	return undo
}
func r09Seed(t *testing.T) (*routerfake.Fake, *httptest.Server, *RouterOS, json.RawMessage) {
	t.Helper()
	tc := r09Base("seed")
	f := routerfake.New(tc.State.Tables)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	t.Cleanup(d.Close)
	r09ApplyPlan(t, d, r09Plan(t, d, tc.Settings, nil))
	return f, srv, d, guardRaw(d.dnsGuardManifest)
}

func TestR09ProductionTimeoutAndRawWire(t *testing.T) {
	tc := r09Base("wire")
	f := routerfake.New(tc.State.Tables)
	var mu sync.Mutex
	wire := []r09Wire{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(data))
		if strings.HasPrefix(r.URL.Path, "/rest") {
			mu.Lock()
			wire = append(wire, r09Wire{r.Method, r.URL.Path, string(data), r.Header.Get("Content-Type"), r.Header.Get("Authorization")})
			mu.Unlock()
		}
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	// Python uses its real requests transport. Record raw bytes before the fake
	// decodes them, so no-body and the JSON literal null cannot collapse together.
	_ = r09Python(t, "r09_reference.py", tc, srv.URL)
	mu.Lock()
	pythonWire := append([]r09Wire{}, wire...)
	wire = nil
	mu.Unlock()
	f.Reset(tc.State)
	client := &r09TimeoutHTTP{DeviceHTTP: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-user", Password: "synthetic-password"})}
	d := NewRouterOSWithHTTP(srv.URL, client)
	defer d.Close()
	undo := r09ApplyPlan(t, d, r09Plan(t, d, tc.Settings, nil))
	if err := d.RestoreDNSGuard(context.Background(), *undo); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	goWire := append([]r09Wire{}, wire...)
	mu.Unlock()
	for _, records := range [][]r09Wire{pythonWire, goWire} {
		for _, call := range records {
			if call.Method == "GET" || call.Method == "DELETE" {
				if call.Body != "" || call.ContentType != "" {
					t.Fatalf("bodyless request has %q %q", call.Body, call.ContentType)
				}
			} else if call.Method == "PUT" {
				if call.ContentType != "application/json" {
					t.Fatalf("PUT type %q", call.ContentType)
				}
			}
			if call.Authorization != "Basic c3ludGhldGljLXVzZXI6c3ludGhldGljLXBhc3N3b3Jk" {
				t.Fatal("unexpected auth header")
			}
		}
	}
	firstPut := func(wire []r09Wire) string {
		for _, r := range wire {
			if r.Method == "PUT" {
				return r.Body
			}
		}
		return ""
	}
	if got := firstPut(goWire); got != `{"address":"192.0.2.2/32","comment":"cf-dnsguard:bypass","disabled":"false","list":"cf-dnsguard-bypass"}` {
		t.Fatalf("raw Go PUT %q", got)
	}
	if got := firstPut(pythonWire); got != `{"list": "cf-dnsguard-bypass", "address": "192.0.2.2/32", "comment": "cf-dnsguard:bypass", "disabled": "false"}` {
		t.Fatalf("raw Python PUT %q", got)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.timeouts) == 0 {
		t.Fatal("no requests")
	}
	for _, timeout := range client.timeouts {
		if timeout != 10*time.Second {
			t.Fatalf("device timeout %s", timeout)
		}
	}
}

func TestR09WritesAndRollbackFinishAfterCancellation(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "compensation"}[failing], func(t *testing.T) {
			tc := r09Base("cancel")
			if failing {
				tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: rosNetwatch, Status: 400, Body: routerfake.Row{"message": "refused"}}}
			}
			f := routerfake.New(nil)
			f.Reset(tc.State)
			srv := httptest.NewServer(f)
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &r09TimeoutHTTP{DeviceHTTP: devhttp.New(devhttp.Config{}), cancel: cancel}
			d := NewRouterOSWithHTTP(srv.URL, client)
			defer d.Close()
			p := r09Plan(t, d, tc.Settings, nil)
			undo, err := d.ApplyDNSGuard(ctx, *p.Ops[0].(*DNSGuardReplace))
			if !failing {
				if err != nil {
					t.Fatal(err)
				}
				if err = d.RestoreDNSGuard(ctx, *undo); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("refusal ignored")
			}
			if ctx.Err() == nil {
				t.Fatal("context was not cancelled")
			}
			for _, rows := range f.Snapshot().Tables {
				for _, row := range rows {
					if strings.HasPrefix(fmt.Sprint(row["comment"]), dnsGuardPrefix) {
						t.Fatal("left owned objects")
					}
				}
			}
		})
	}
}

func TestR09IncrementalKeepsPausedIDs(t *testing.T) {
	f, _, d, previous := r09Seed(t)
	before := f.Snapshot()
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{rosNAT: {"*6A": {"disabled": "true"}, "*6B": {"disabled": "true"}}}}})
	settings := r09Base("delta").Settings
	settings.Bypass = []string{"192.0.2.60/32"}
	p := r09Plan(t, d, settings, previous)
	count := len(f.Calls())
	r09ApplyPlan(t, d, p)
	for _, call := range f.Calls()[count:] {
		if call.Method != "GET" && (call.Path == rosNAT || strings.HasPrefix(call.Path, rosNAT+"/") || call.Path == rosFilter || strings.HasPrefix(call.Path, rosFilter+"/")) {
			t.Fatalf("unchanged object written %s %s", call.Method, call.Path)
		}
	}
	after := f.Snapshot()
	for _, path := range []string{rosNAT, rosFilter} {
		for i, row := range after.Tables[path] {
			if row[".id"] != before.Tables[path][i][".id"] {
				t.Fatal("stable ID changed")
			}
		}
	}
	for _, row := range after.Tables[rosNAT] {
		if row["comment"] == dnsGuardHijack && row["disabled"] != "true" {
			t.Fatal("paused hijack resumed")
		}
	}
	if err := VerifyDNSGuard(context.Background(), d, &DNSGuardCheck{Roles: p.Roles}); err != nil {
		t.Fatal(err)
	}
}

func TestR09ForeignHijackCommentBlocksPublish(t *testing.T) {
	for _, failover := range []bool{false, true} {
		for _, disabled := range []string{"false", "true"} {
			t.Run(fmt.Sprintf("failover=%t/disabled=%s", failover, disabled), func(t *testing.T) {
				tc := r09Base("foreign_hijack_comment")
				tc.Settings.Failover = failover
				tc.Steps = []r09Step{{Action: "plan"}}
				foreign := tc.State.Tables[rosNAT][0]
				foreign["comment"] = dnsGuardHijack
				foreign["disabled"] = disabled
				// The runtime selector has no chain, action or port predicate.
				if disabled == "false" {
					foreign["chain"], foreign["dst-port"] = "srcnat", "5353"
				}
				f := routerfake.New(tc.State.Tables)
				srv := httptest.NewServer(f)
				defer srv.Close()
				var python struct {
					Steps []struct {
						Plan struct{ Blockers []string }
					}
				}
				if err := json.Unmarshal(r09Python(t, "r09_reference.py", tc, srv.URL), &python); err != nil {
					t.Fatal(err)
				}
				if len(python.Steps[0].Plan.Blockers) != 0 {
					t.Fatalf("Python reference no longer permits collision: %s", r09Raw(python))
				}
				result := r09Go(t, tc, f, srv.URL)
				plan := result.Steps[0].Plan
				if plan.Status != "unknown" || !reflect.DeepEqual(plan.Blockers, []string{messageGuardHijackCollision}) || len(plan.Ops) != 0 || len(result.Writes) != 0 {
					t.Fatalf("foreign comment did not block without writes: %s", r09Raw(result))
				}
				if !reflect.DeepEqual(result.Tables[rosNAT], tc.State.Tables[rosNAT]) {
					t.Fatal("foreign NAT changed")
				}
				// Withdrawal must remain available; the foreign rule is never claimed.
				tc.Settings.Enabled = false
				if plan = r09Go(t, tc, f, srv.URL).Steps[0].Plan; len(plan.Blockers) != 0 || len(plan.Ops) != 0 {
					t.Fatalf("foreign comment blocked disabled cleanup: %s", r09Raw(plan))
				}
			})
		}
	}
}

func TestR09LateForeignObjectsAreNeverClaimed(t *testing.T) {
	for _, variant := range []string{"name_collision", "hijack_comment_collision", "hijack_overtaken", "removed_owned", "moved_owned", "duplicate_owned", "delete_read_drift", "final_read_drift"} {
		t.Run(variant, func(t *testing.T) {
			f, _, d, previous := r09Seed(t)
			settings := r09Base("late").Settings
			settings.Bypass = []string{"192.0.2.60/32"}
			if variant == "delete_read_drift" {
				settings.BlockDoT = false
			}
			p := r09Plan(t, d, settings, previous)
			snapshot := f.Snapshot()
			switch variant {
			case "name_collision":
				snapshot.Tables[rosAddresses] = append(snapshot.Tables[rosAddresses], routerfake.Row{".id": "*F1", "list": DNSGuardBypassList, "address": "192.0.2.99"})
			case "hijack_comment_collision":
				snapshot.Tables[rosNAT] = append(snapshot.Tables[rosNAT], routerfake.Row{".id": "*F1", "chain": "dstnat", "action": "redirect", "dst-port": "53", "disabled": "true", "comment": dnsGuardHijack})
			case "hijack_overtaken":
				snapshot.Tables[rosNAT] = append([]routerfake.Row{{".id": "*F1", "chain": "dstnat", "action": "redirect", "dst-port": "53"}}, snapshot.Tables[rosNAT]...)
			case "removed_owned":
				snapshot.Tables[rosAddresses] = snapshot.Tables[rosAddresses][1:]
			case "moved_owned":
				snapshot.Tables[rosAddresses][0]["list"] = "foreign"
			case "duplicate_owned":
				snapshot.Tables[rosNAT] = append(snapshot.Tables[rosNAT], r09Copy(snapshot.Tables[rosNAT][0]))
			case "delete_read_drift":
				snapshot.Faults = []routerfake.Fault{{Method: "GET", Path: rosFilter, Count: 2, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosFilter: {"*6D": {"dst-port": "854"}}}}}
			case "final_read_drift":
				snapshot.Faults = []routerfake.Fault{{Method: "GET", Path: rosNAT, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosNAT: {"*68": {"dst-port": "54"}}}}}
			}
			f.ReplaceTables(snapshot.Tables)
			f.ApplyExternal(routerfake.ExternalWrite{Faults: snapshot.Faults})
			count := len(f.Calls())
			_, err := d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
			if err == nil {
				t.Fatal("late drift accepted")
			}
			if variant == "hijack_comment_collision" && !strings.Contains(err.Error(), messageGuardHijackCollision) {
				t.Fatalf("missing collision explanation: %v", err)
			}
			if variant != "delete_read_drift" && variant != "final_read_drift" {
				for _, call := range f.Calls()[count:] {
					if call.Method != "GET" {
						t.Fatalf("drift wrote %s", call.Method)
					}
				}
			}
			if variant == "name_collision" {
				found := false
				for _, row := range f.Snapshot().Tables[rosAddresses] {
					if row[".id"] == "*F1" {
						found = true
					}
				}
				if !found {
					t.Fatal("foreign entry removed")
				}
			}
		})
	}
}

func TestR09RejectedVersusUncertain(t *testing.T) {
	for _, variant := range []string{"rejected", "lost_put", "server_put", "reused_id", "lost_delete", "bad_undo"} {
		t.Run(variant, func(t *testing.T) {
			tc := r09Base(variant)
			f := routerfake.New(nil)
			fault := routerfake.Fault{Method: "PUT", Path: rosAddresses, Count: 1}
			switch variant {
			case "rejected":
				fault.Status = 400
				fault.Body = routerfake.Row{"message": "refused"}
			case "lost_put":
				fault.AfterCommit = true
				fault.LoseReply = true
			case "server_put":
				fault.AfterCommit = true
				fault.Status = 500
			case "reused_id":
				tc.State.Tables[rosAddresses] = []routerfake.Row{{".id": "*F1", "list": "foreign", "address": "203.0.113.1"}}
				fault.Status = 201
				fault.Body = routerfake.Row{".id": "*F1", "list": "foreign", "address": "203.0.113.1"}
			case "lost_delete", "bad_undo":
				fault = routerfake.Fault{}
			}
			if fault.Method != "" {
				tc.State.Faults = []routerfake.Fault{fault}
			}
			f.Reset(tc.State)
			srv := httptest.NewServer(f)
			defer srv.Close()
			d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			p := r09Plan(t, d, tc.Settings, nil)
			undo, err := d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
			if variant == "bad_undo" {
				if err != nil {
					t.Fatal(err)
				}
				f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{rosAddresses: {"*65": {"address": "203.0.113.99"}}}}})
				err = d.RestoreDNSGuard(context.Background(), *undo)
				if err == nil || device.RouterErrorKind(err) != KindRollback {
					t.Fatal("changed undo not refused", err)
				}
				return
			}
			if variant == "lost_delete" {
				if err != nil {
					t.Fatal(err)
				}
				previous := guardRaw(d.dnsGuardManifest)
				off := tc.Settings
				off.Enabled = false
				p = r09Plan(t, d, off, previous)
				f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: rosNetwatch + "/*6E", Count: 1, AfterCommit: true, LoseReply: true}}})
				_, err = d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
				if err == nil {
					t.Fatal("lost reply accepted")
				}
				if len(f.Snapshot().Tables[rosNetwatch]) != 1 {
					t.Fatal("delete not compensated")
				}
				return
			}
			if err == nil {
				t.Fatal("failure accepted")
			}
			if variant == "rejected" {
				if device.RouterErrorKind(err) == KindUncertainWrite || device.RouterErrorKind(err) == KindRollback {
					t.Fatalf("refusal marked uncertain %v", err)
				}
				if len(f.Snapshot().Tables[rosAddresses]) != 0 {
					t.Fatal("refusal changed table")
				}
			} else if device.RouterErrorKind(err) != KindRollback {
				t.Fatalf("unproven create did not preserve scene %v", err)
			}
			if variant == "lost_put" || variant == "server_put" {
				if len(f.Snapshot().Tables[rosAddresses]) != 1 {
					t.Fatal("unknown create was deleted or retried")
				}
			}
		})
	}
}

func TestR09RealDeviceTimeout(t *testing.T) {
	tc := r09Base("timeout")
	tc.State.Faults = []routerfake.Fault{{Method: "PUT", Path: rosAddresses, Count: 1, AfterCommit: true, DelayMS: 11000}}
	f := routerfake.New(nil)
	f.Reset(tc.State)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	p := r09Plan(t, d, tc.Settings, nil)
	started := time.Now()
	_, err := d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
	elapsed := time.Since(started)
	if err == nil || elapsed < 9*time.Second || elapsed > 15*time.Second {
		t.Fatalf("production timeout %s %v", elapsed, err)
	}
	if len(f.Snapshot().Tables[rosAddresses]) != 1 {
		t.Fatal("uncertain write not preserved")
	}
}

func TestR09HintDoesNotEnterPlan(t *testing.T) {
	tc := r09Base("report")
	state := &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{}}
	before, err := BuildReport(tc.Desired, state)
	if err != nil {
		t.Fatal(err)
	}
	p1 := r09Raw(state)
	desired := r09Raw(before.Desired)
	parts := r09Raw(before.Parts)
	before.Routes.ForwardHint = &ForwardHintResult{Status: "drop", Warning: messageForwardIPv4Drop, FixCommand: "synthetic command"}
	if !reflect.DeepEqual(p1, r09Raw(state)) || !reflect.DeepEqual(desired, r09Raw(before.Desired)) || !reflect.DeepEqual(parts, r09Raw(before.Parts)) {
		t.Fatal("hint leaked into planning inputs")
	}
}

func TestR09FinalReadCannotWidenOwnership(t *testing.T) {
	tc := r09Base("late_new_edit")
	tc.State.Faults = []routerfake.Fault{{Method: "GET", Path: rosAddresses + "/*65", Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosAddresses: {"*65": {"address": "203.0.113.99/32"}}}}}
	f := routerfake.New(nil)
	f.Reset(tc.State)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	p := r09Plan(t, d, tc.Settings, nil)
	_, err := d.ApplyDNSGuard(context.Background(), *p.Ops[0].(*DNSGuardReplace))
	if err == nil || device.RouterErrorKind(err) != KindRollback {
		t.Fatal("late edit absorbed into manifest", err)
	}
	for _, row := range f.Snapshot().Tables[rosAddresses] {
		if row[".id"] == "*65" && row["address"] == "203.0.113.99/32" {
			return
		}
	}
	t.Fatal("edited row deleted")
}

func TestR09RemapPreservesUnknownStoredFields(t *testing.T) {
	_, _, d, raw := r09Seed(t)
	var document map[string]json.RawMessage
	_ = json.Unmarshal(raw, &document)
	document["future"] = json.RawMessage(`{"keep":true}`)
	var entries []map[string]json.RawMessage
	_ = json.Unmarshal(document["resources"], &entries)
	entries[0]["future_resource"] = json.RawMessage(`"keep"`)
	document["resources"] = r09Raw(entries)
	original := r09Raw(document)
	if err := d.remapDNSGuard(context.Background(), original, map[[2]string]string{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r09JSON(t, original), r09JSON(t, d.dnsGuardManifest)) {
		t.Fatal("unknown manifest members lost")
	}
}

// RouterOS IDs are table-local. Seed overlapping IDs instead of relying on the
// fake allocator's globally fresh IDs, then prove restored ownership at the
// exported verification boundary. Python's ID-only remap addresses the wrong table.
func TestR09TableLocalIDsSurviveRestore(t *testing.T) {
	f, srv, _, previous := r09Seed(t)
	tc := r09Base("table_local_ids")
	tc.State.Tables = f.Snapshot().Tables
	manifest, err := parseDNSGuardManifest(previous)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[[2]string]string{}
	for _, path := range dnsGuardPaths {
		next := 1
		for _, row := range tc.State.Tables[path] {
			if !strings.HasPrefix(fmt.Sprint(row["comment"]), dnsGuardPrefix) {
				continue
			}
			old := fmt.Sprint(row[".id"])
			id := fmt.Sprintf("*%X", next)
			next++
			ids[[2]string{path, old}] = id
			row[".id"] = id
		}
	}
	roles := []string{}
	for i, e := range manifest.Resources {
		manifest.Resources[i].ID = ids[[2]string{e.Path, e.ID}]
		roles = append(roles, e.Role)
	}
	tc.Previous = r09Raw(manifest)
	tc.Settings.Enabled = false
	tc.Steps = []r09Step{{Action: "roundtrip"}}
	var python struct {
		Steps []struct {
			Error *r09Error `json:"error"`
		} `json:"steps"`
	}
	if err = json.Unmarshal(r09Python(t, "r09_reference.py", tc, srv.URL), &python); err != nil {
		t.Fatal(err)
	}
	if len(python.Steps) != 1 || python.Steps[0].Error == nil || python.Steps[0].Error.Class != "RouterError" || !strings.Contains(python.Steps[0].Error.Message, rosAddresses) {
		t.Fatalf("expected the real Python cross-table remap defect, got %s", r09Raw(python))
	}
	got := r09Go(t, tc, f, srv.URL)
	if len(got.Steps) != 1 || got.Steps[0].Error != nil {
		t.Fatalf("Go restore: %s", r09Raw(got))
	}
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	d.SetDNSGuardManifest(got.Steps[0].Manifest)
	if err = VerifyDNSGuard(context.Background(), d, &DNSGuardCheck{Roles: roles}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"*20", "*21"} {
		present := false
		for _, row := range got.Tables[rosNAT] {
			if row[".id"] == id {
				present = true
			}
		}
		if !present {
			t.Fatalf("foreign NAT %s removed", id)
		}
	}
}
