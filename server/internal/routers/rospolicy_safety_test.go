package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

func r08Driver(t *testing.T) (*RouterOS, *routerfake.Fake) {
	t.Helper()
	f := routerfake.New(nil)
	f.Reset(r08State())
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	t.Cleanup(d.Close)
	return d, f
}
func r08Publish(t *testing.T, d *RouterOS, desired *Desired) (*ROSPolicyPlan, *ROSPolicyUndo) {
	t.Helper()
	plan, err := r08Plan(context.Background(), d, desired)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) > 0 || len(plan.Ops) != 1 {
		t.Fatalf("plan: %s", device.JSON(plan))
	}
	undo, err := rospolicy.ApplyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Ops[0].(*ROSPolicyReplace))
	if err != nil {
		t.Fatal(err)
	}
	if err = rospolicy.VerifyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Check); err != nil {
		t.Fatal(err)
	}
	return plan, undo
}

func TestR08RequestsKeepProductionTimeoutAndRawBodies(t *testing.T) {
	fake := routerfake.New(nil)
	state := r08State()
	delete(state.Tables[ros.PathNAT][0], "out-interface")
	fake.Reset(state)
	type wire struct{ method, path, body, contentType string }
	var mu sync.Mutex
	seen := []wire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-user" || password != "synthetic-password" {
			t.Error("missing production Basic auth")
		}
		mu.Lock()
		seen = append(seen, wire{r.Method, strings.TrimPrefix(r.URL.Path, "/rest"), string(data), r.Header.Get("Content-Type")})
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(data))
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	recorder := &r06TimeoutRecorder{client: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-user", Password: "synthetic-password"})}
	d := NewRouterOSWithHTTP(server.URL, recorder)
	defer d.Close()
	_, undo := r08Publish(t, d, r08Desired(true))
	if err := rospolicy.RestoreROSPolicy(context.Background(), d.Client, *undo); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for i, timeout := range recorder.timeouts {
		if timeout != 10*time.Second {
			t.Errorf("%s timeout=%v", recorder.requests[i], timeout)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	table, route, nat, deletes := false, false, false, false
	for _, call := range seen {
		if call.method == "GET" || call.method == "DELETE" {
			if call.body != "" || call.contentType != "" {
				t.Errorf("read/delete wire=%+v", call)
			}
			deletes = deletes || call.method == "DELETE"
			continue
		}
		if call.contentType != "application/json" {
			t.Errorf("write header=%+v", call)
		}
		if call.method == "PUT" {
			switch call.path {
			case ros.PathTables:
				table = true
				if call.body != `{"fib":"true","name":"CF_Proxy_Routing_01"}` {
					t.Errorf("table bytes=%s", call.body)
				}
			case ros.PathRoutes:
				route = true
				if call.body != `{"comment":"cf-ros-policy-test","disabled":"false","dst-address":"0.0.0.0/0","gateway":"192.0.2.3@main","routing-table":"CF_Proxy_Routing_01"}` {
					t.Errorf("route bytes=%s", call.body)
				}
			case ros.PathNAT:
				nat = true
				if call.body != `{"action":"accept","chain":"srcnat","comment":"cf-ros-policy-test","disabled":"false","dst-address-list":"CF_Destination_Proxy_IP_01","out-interface":"bridge","place-before":"*3","src-address-list":"CF_Source_Proxy_IP_01"}` {
					t.Errorf("NAT bytes=%s", call.body)
				}
			}
		}
	}
	if !table || !route || !nat || !deletes {
		t.Fatal("raw check missed a production write path")
	}
}

func TestR08DeviceTimeoutActuallyExpires(t *testing.T) {
	// No short-timeout transport or injected timer: the detached write's first
	// read uses the production ten-second watchdog against a silent loopback peer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := rospolicy.ApplyROSPolicy(ctx, d.Client, d.ReadPolicyState, &ROSPolicyReplace{Bundle: []ROSReplacement{}, Migrations: []ROSResource{}, Replacements: []ROSReplacement{}, Evidence: ROSPolicySnapshot{}})
	elapsed := time.Since(start)
	if _, ok := errors.AsType[*RouterError](err); !ok {
		t.Fatalf("expected bounded RouterError, got %v", err)
	}
	if elapsed < 9*time.Second || elapsed > 20*time.Second {
		t.Fatalf("production timeout elapsed=%v", elapsed)
	}
}

func TestR08CancellationAndManifestCopies(t *testing.T) {
	d, _ := r08Driver(t)
	plan, err := r08Plan(context.Background(), d, r08Desired(true))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	undo, err := rospolicy.ApplyROSPolicy(ctx, d.Client, d.ReadPolicyState, plan.Ops[0].(*ROSPolicyReplace))
	if err != nil {
		t.Fatal(err)
	}
	if err = rospolicy.VerifyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Check); err != nil {
		t.Fatal(err)
	}
	first := d.Manifests()
	if !first.Policy.Set {
		t.Fatal("manifest not marked set")
	}
	first.Policy.Value[0] = '!'
	if !json.Valid(d.Manifests().Policy.Value) {
		t.Fatal("manifest aliases caller")
	}
	if err = rospolicy.RestoreROSPolicy(ctx, d.Client, *undo); err != nil {
		t.Fatal(err)
	}
	if len(d.Manifests().Policy.Value) > 0 && string(d.Manifests().Policy.Value) != "null" {
		t.Fatal("rollback left manifest")
	}
	if _, err = rospolicy.SnapshotROSPolicy(ctx, d.Client); err == nil {
		t.Fatal("read detached unexpectedly")
	}
}

func TestR08PreviewAndOwnershipGuards(t *testing.T) {
	tests := []struct {
		name     string
		mutation routerfake.ExternalWrite
	}{
		{"foreign config after preview", routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{ros.PathFilter: {{".id": "*F", "action": "drop", "chain": "input"}}}}}},
		{"lookup order after preview", routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{rospolicy.SettingsPath: {{"policy-rules": "main,mangle"}}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, f := r08Driver(t)
			plan, err := r08Plan(context.Background(), d, r08Desired(true))
			if err != nil {
				t.Fatal(err)
			}
			f.ApplyExternal(tt.mutation)
			before := f.Snapshot()
			_, err = rospolicy.ApplyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Ops[0].(*ROSPolicyReplace))
			if err == nil {
				t.Fatal("stale plan accepted")
			}
			if !reflect.DeepEqual(before.Tables, f.Snapshot().Tables) {
				t.Fatal("stale plan wrote")
			}
		})
	}
	d, f := r08Driver(t)
	_, undo := r08Publish(t, d, r08Desired(true))
	manifest := d.Manifests().Policy.Value
	var parsed ROSPolicyManifest
	_ = json.Unmarshal(manifest, &parsed)
	source := parsed.Resources[0]
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathAddresses: {source.ID: {"list": "moved", "address": "192.0.2.99"}}}}})
	live, err := rospolicy.SnapshotROSPolicy(context.Background(), d.Client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rospolicy.OwnedROSPolicyRows(manifest, live); err == nil {
		t.Fatal("moved member looked deleted")
	}
	objects, err := rospolicy.ReadPolicyObjects(context.Background(), d.Client, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if objects[0].Editor != "source" || objects[0].Status != "changed" || objects[0].ConfiguredAddress != source.Config.Text("address") {
		t.Fatal("editor role followed external name")
	}
	if err = rospolicy.RestoreROSPolicy(context.Background(), d.Client, *undo); err == nil {
		t.Fatal("rollback deleted external edit")
	}
	if row := findRowFake(f.Snapshot().Tables[ros.PathAddresses], source.ID); row == nil || row["address"] != "192.0.2.99" {
		t.Fatal("external member lost")
	}
}

func findRowFake(rows []routerfake.Row, id string) routerfake.Row {
	for _, row := range rows {
		if row[".id"] == id {
			return row
		}
	}
	return nil
}

func TestR08OwnershipProofShapesAndCopies(t *testing.T) {
	row := Row{".id": "*1", "name": "x", "unknown": `[{"a":"original"}]`}
	config := rospolicy.ROSPolicyConfigRow(row)
	config["unknown"] = `[{"a":"changed"}]`
	if row["unknown"] != `[{"a":"original"}]` {
		t.Fatal("stable baseline aliases caller")
	}
	for _, manifest := range []json.RawMessage{json.RawMessage(`{"resources":[{"path":"/routing/table","id":"","config":{}}]}`), json.RawMessage(`{"resources":[{"path":"/routing/table","id":"*1"}]}`), json.RawMessage(`[]`)} {
		snap := ROSPolicySnapshot{ros.PathTables: []Row{row}}
		if _, err := rospolicy.OwnedROSPolicyRows(manifest, &snap); err == nil {
			t.Fatalf("missing proof accepted: %s", manifest)
		}
	}
	manifest := device.JSON(ROSPolicyManifest{Resources: []ROSResource{{Path: ros.PathTables, ID: "*1", Config: ros.ConfigROSRow(row)}}})
	snap := ROSPolicySnapshot{ros.PathTables: []Row{row, row}}
	if _, err := rospolicy.OwnedROSPolicyRows(manifest, &snap); err == nil {
		t.Fatal("duplicate ID accepted")
	}
}

func TestR08TableFlagsAndNetworkSubtraction(t *testing.T) {
	// A JSON true arrives as "true"; null as "" (Row decoding).
	for _, value := range []string{"", "true", "yes"} {
		if !ros.ValuesMatch(ros.PathTables, Row{"name": "x", "fib": value}, Row{"name": "x", "fib": "true"}) {
			t.Fatalf("enabled fib=%v", value)
		}
	}
	for _, value := range []string{"false", "no", "unknown"} {
		if ros.ValuesMatch(ros.PathTables, Row{"name": "x", "fib": value}, Row{"name": "x", "fib": "true"}) {
			t.Fatalf("unset fib=%v", value)
		}
	}
	if ros.ValuesMatch(ros.PathMangle, Row{"fib": ""}, Row{"fib": "true"}) {
		t.Fatal("FIB normalization escaped table")
	}
	got := rospolicy.SubtractNetworks([]netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}, []netip.Prefix{netip.MustParsePrefix("198.51.100.0/25")})
	if !slices.Equal(got, []netip.Prefix{netip.MustParsePrefix("198.51.100.128/25")}) {
		t.Fatal(got)
	}
	if rospolicy.NATKey("198.51.100.0-198.51.100.0") == rospolicy.NATKey("198.51.100.0/32") {
		t.Fatal("range identity normalized")
	}
	for _, value := range []string{r08Gateway, r08Gateway + "@main"} {
		if !ros.ValuesMatch(ros.PathRoutes, Row{"gateway": value}, Row{"gateway": r08Gateway + "@main"}) {
			t.Fatal("default gateway alias rejected")
		}
	}
	for _, value := range []string{"", r08Gateway + "@other", r08Gateway + "%wan", r08Gateway + ",192.0.2.4"} {
		if ros.ValuesMatch(ros.PathRoutes, Row{"gateway": value}, Row{"gateway": r08Gateway + "@main"}) {
			t.Fatal("different gateway alias accepted")
		}
	}
	for _, value := range []string{"", "unknown", "true", "yes"} {
		if ros.ValuesMatch(ros.PathMangle, Row{"passthrough": value}, Row{"passthrough": "false"}) {
			t.Fatal("invalid passthrough accepted")
		}
	}
}

func TestR08ActivationWaitsOnlyForRuntimeFlags(t *testing.T) {
	for _, condition := range []string{"ready", "stuck", "edit"} {
		t.Run(condition, func(t *testing.T) {
			state := r08State()
			for i := range 3 {
				state.Mutations = append(state.Mutations, routerfake.Mutation{Method: "PUT", Path: ros.PathMangle, Count: i + 1, Table: ros.PathMangle, ID: fmt.Sprintf("*%X", 0x6A+i), Set: routerfake.Row{"invalid": "true"}})
			}
			if condition != "stuck" {
				for i := range 3 {
					fields := routerfake.Row{"invalid": "false"}
					if condition == "edit" && i == 2 {
						fields["src-address-list"] = "foreign-edit"
					}
					state.Mutations = append(state.Mutations, routerfake.Mutation{Method: "GET", Path: ros.PathMangle, Count: 7, Table: ros.PathMangle, ID: fmt.Sprintf("*%X", 0x6A+i), Set: fields})
				}
			}
			f := routerfake.New(nil)
			f.Reset(state)
			server := httptest.NewServer(f)
			defer server.Close()
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			plan, err := r08Plan(context.Background(), d, r08Desired(true))
			if err != nil {
				t.Fatal(err)
			}
			undo, err := rospolicy.ApplyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Ops[0].(*ROSPolicyReplace))
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			err = rospolicy.VerifyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Check)
			if condition == "ready" {
				if err != nil {
					t.Fatal(err)
				}
				if time.Since(start) < 250*time.Millisecond {
					t.Fatal("invalid flag not waited for")
				}
			} else {
				if err == nil {
					t.Fatal("invalid or edited bundle verified")
				}
				restore := rospolicy.RestoreROSPolicy(context.Background(), d.Client, *undo)
				if condition == "stuck" && restore != nil {
					t.Fatal(restore)
				}
				if condition == "edit" {
					if restore == nil {
						t.Fatal("external edit overwritten")
					}
					if row := findRowFake(f.Snapshot().Tables[ros.PathMangle], "*6C"); row == nil || row["src-address-list"] != "foreign-edit" {
						t.Fatal("external edit lost")
					}
				}
			}
			if strings.Contains(string(d.Manifests().Policy.Value), `"invalid"`) {
				t.Fatal("runtime flag became ownership config")
			}
		})
	}
}

func TestR08NamesGatewayAndReportIntegration(t *testing.T) {
	d, f := r08Driver(t)
	state := r08State()
	for i := 1; i <= 256; i++ {
		state.Tables[ros.PathTables] = append(state.Tables[ros.PathTables], routerfake.Row{".id": fmt.Sprintf("*%X", 1000+i), "name": fmt.Sprintf("CF_Proxy_Routing_%02d", i), "fib": "true"})
	}
	f.Reset(state)
	plan, err := r08Plan(context.Background(), d, r08Desired(true))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Blockers, rospolicy.MessageNamesExhausted) || len(plan.Ops) > 0 {
		t.Fatal("occupied groups reused")
	}
	for _, condition := range []string{"missing", "inactive", "exception", "ambiguous"} {
		state := r08State()
		switch condition {
		case "missing":
			state.Tables[ros.PathRoutes] = state.Tables[ros.PathRoutes][1:]
		case "inactive":
			state.Tables[ros.PathRoutes][0]["active"] = "false"
		case "exception":
			state.Tables[ros.PathRoutes] = append(state.Tables[ros.PathRoutes], routerfake.Row{".id": "*F", "dst-address": r08Gateway + "/32", "gateway": "192.0.2.9"})
		case "ambiguous":
			state.Tables[ros.PathRoutes] = append(state.Tables[ros.PathRoutes], routerfake.Row{".id": "*F", "dst-address": "192.0.2.0/24", "gateway": "wan", "routing-table": "main", "active": "true", "connect": "true", "dynamic": "true", "immediate-gw": "wan"})
		}
		f.Reset(state)
		plan, err = r08Plan(context.Background(), d, r08Desired(true))
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Blockers) == 0 {
			t.Fatalf("unproven gateway %s accepted", condition)
		}
	}
	f.Reset(r08State())
	desired := r08Desired(true)
	rs, err := d.ReadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rs.ROSPolicy, err = rospolicy.SnapshotROSPolicy(context.Background(), d.Client)
	if err != nil {
		t.Fatal(err)
	}
	rs.NatContext.RoutingRules = []Row{}
	rs.NatContext.VRFs = []Row{}
	rs.NatContext.Mangle = []Row{}
	report, err := BuildReport(desired, rs)
	if err != nil {
		t.Fatal(err)
	}
	if rs.ROSPolicyPlan == nil || report.Summary["pending"] != 1 || report.InSync {
		t.Fatal("policy report part not wired")
	}
	raw, _ := json.Marshal(report)
	var document map[string]json.RawMessage
	_ = json.Unmarshal(raw, &document)
	if len(document["policy_routing"]) == 0 || strings.Contains(string(document["desired"]), "ros_policy") {
		t.Fatal("private policy desired leaked into report")
	}
	// R07 consumes exactly the policy check. The temporary alternate state field
	// has been removed, so callers cannot populate disagreeing migration proofs.
	if len(rospolicy.BuildNATPolicyEvidence(rs.ROSPolicyPlan).Migrations) != len(rs.ROSPolicyPlan.Check.Migrations) {
		t.Fatal("NAT planner used alternate policy evidence")
	}
}

func FuzzR08PolicyInput(f *testing.F) {
	for _, seed := range []string{`null`, `{}`, `[]`, `{"resources":[{"path":"/routing/table","id":"*1","config":{"fib":""}}]}`, `[{"ip":"192.0.2.0/24","mode":"smart"}]`, `{"policy-rules":["mangle","main"]}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = rospolicy.ROSPolicyDigest(data)
		manifest, _ := rospolicy.ParseManifest(data)
		if manifest != nil {
			snap := ROSPolicySnapshot{}
			for _, item := range manifest.Resources {
				row := ros.ConfigROSRow(item.Config)
				row[".id"] = item.ID
				snap[item.Path] = append(snap[item.Path], row)
			}
			_, _ = rospolicy.OwnedROSPolicyRows(data, &snap)
		}
		_, _ = rospolicy.SmartROSPolicySources(data, r08Gateway)
		var row Row
		if json.Unmarshal(data, &row) == nil {
			_ = rospolicy.CheckOrder(ROSPolicySnapshot{rospolicy.SettingsPath: []Row{row}})
			_ = ros.ComparableValues(ros.PathTables, row)
		}
	})
}

func TestR08ReadbackBeforeActivation(t *testing.T) {
	for _, condition := range []string{"inactive", "next-hop", "external-comment", "fib-unset"} {
		t.Run(condition, func(t *testing.T) {
			state := r08State()
			path, id := ros.PathRoutes, "*69"
			fields := routerfake.Row{}
			switch condition {
			case "inactive":
				fields["active"] = "false"
			case "next-hop":
				fields["immediate-gw"] = r08Gateway + "%unexpected"
			case "external-comment":
				fields["comment"] = "keep operator edit"
			case "fib-unset":
				path, id = ros.PathTables, "*68"
				fields["fib"] = nil
			}
			state.Mutations = []routerfake.Mutation{{Method: "PUT", Path: path, Count: 1, Table: path, ID: id, Set: fields}}
			f := routerfake.New(nil)
			f.Reset(state)
			server := httptest.NewServer(f)
			defer server.Close()
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			plan, err := r08Plan(context.Background(), d, r08Desired(true))
			if err != nil {
				t.Fatal(err)
			}
			_, err = rospolicy.ApplyROSPolicy(context.Background(), d.Client, d.ReadPolicyState, plan.Ops[0].(*ROSPolicyReplace))
			if err == nil {
				t.Fatal("bad readback activated")
			}
			for _, call := range f.Calls() {
				if call.Method == "PUT" && call.Path == ros.PathMangle {
					t.Fatal("Mangle enabled before proof")
				}
			}
			if condition == "external-comment" {
				if row := findRowFake(f.Snapshot().Tables[ros.PathRoutes], id); row == nil || row["comment"] != "keep operator edit" {
					t.Fatal("operator edit deleted")
				}
			}
			if condition == "fib-unset" && !strings.Contains(err.Error(), "字段：fib") {
				t.Fatalf("wrong diagnostic: %v", err)
			}
		})
	}
}
