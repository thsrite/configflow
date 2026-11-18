package routers

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"configflow/internal/routers/routerfake"
)

// r11ReviewCases are the R11 Opus review probes: everyday edits of the user's
// own dhcp-client after ConfigFlow installed the hook. ConfigFlow owns only the
// hook line; the client's other settings, a pool rename or a move of the prefix
// to another client must neither block the update nor the withdrawal.
func r11ReviewCases() []r11Case {
	patch := func(rows map[string]map[string]routerfake.Row) *routerfake.ExternalWrite {
		return &routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: rows}}
	}
	step := func(enabled bool, mutation *routerfake.ExternalWrite) r11Step {
		s := r11StepFor("publish")
		s.Enabled = enabled
		s.Mutation = mutation
		return s
	}
	peerDNS := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"use-peer-dns": "no"}}}
	renamed := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"pool-name": "pool-next"}}, rosIPv6Addresses: {"*A2": {"from-pool": "pool-next"}}}
	moved := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"disabled": "true"}, "*C1": {"disabled": "false", "pool-name": "pool-example", "interface": "wan2"}}}
	cleared := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"script": ""}}}
	shared := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"script": ":log info mine\n" + pdHook}}}
	foreign := map[string]map[string]routerfake.Row{pdClients: {"*C0": {"script": ":log info mine"}}}
	cases := []struct {
		name  string
		steps []r11Step
	}{
		{"client_setting_update", []r11Step{step(true, nil), step(true, patch(peerDNS))}},
		{"client_setting_withdraw", []r11Step{step(true, nil), step(false, patch(peerDNS))}},
		{"pool_renamed", []r11Step{step(true, nil), step(true, patch(renamed))}},
		{"client_moved", []r11Step{step(true, nil), step(true, patch(moved))}},
		{"client_moved_withdraw", []r11Step{step(true, nil), step(true, patch(moved)), step(false, nil)}},
		{"hook_cleared_withdraw", []r11Step{step(true, nil), step(false, patch(cleared))}},
		{"hook_shared_update", []r11Step{step(true, nil), step(true, patch(shared))}},
		{"hook_foreign_withdraw", []r11Step{step(true, nil), step(false, patch(foreign))}},
	}
	out := []r11Case{}
	for _, c := range cases {
		out = append(out, r11Case{"pd/review_" + c.name, "pd", r11State(), r10Router(), c.steps})
	}
	return out
}

func TestR11ReviewPythonProbes(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, tc := range r11ReviewCases() {
		t.Run(tc.Name, func(t *testing.T) {
			var py r08Output
			raw := r09Python(t, "r11_reference.py", tc, srv.URL)
			if e := json.Unmarshal(raw, &py); e != nil {
				t.Fatal(e, string(raw))
			}
			pystate := f.Snapshot()
			if py.Error != nil {
				t.Fatalf("Python probe must succeed: %+v", *py.Error)
			}
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

// TestR11ReviewFingerprintExcludesPassword: the readiness fingerprint reaches
// the browser, so it must not be an offline oracle for the device password,
// while a real router setting change still changes it.
func TestR11ReviewFingerprintExcludesPassword(t *testing.T) {
	d, _ := r11Driver(t)
	s := r11StepFor("plan")
	fingerprint := func(r *Router) string {
		t.Helper()
		got, e := InspectIPv6Readiness(t.Context(), d, r, s.Agent, s.Host, s.Configuration, "", PolicyContext{})
		if e != nil {
			t.Fatal(e)
		}
		return got.Fingerprint
	}
	first, second, renamed := r10Router(), r10Router(), r10Router()
	first.Password, second.Password, renamed.Password = "synthetic-secret-1", "synthetic-secret-2", "synthetic-secret-1"
	renamed.Endpoint = "https://192.0.2.9"
	base := fingerprint(first)
	if base != fingerprint(second) {
		t.Fatal("fingerprint depends on the device password")
	}
	if base == fingerprint(renamed) {
		t.Fatal("fingerprint ignores router settings")
	}
	if first.Password != "synthetic-secret-1" {
		t.Fatal("caller's router mutated")
	}
}
