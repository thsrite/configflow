package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"configflow/internal/repo/repotest"
	"configflow/internal/routers/routerfake"
)

func r14Python(t *testing.T, endpoint string, input any, out any) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if fields, ok := input.(map[string]any); ok {
		if dir, ok := fields["data_dir"].(string); ok {
			fields["data_dir"] = pythonDataDir(t, dir)
		}
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r14_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R14_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(mustPolicyJSON(input))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python: %v %s %s", err, raw, stderr.String())
	}
	if err = json.Unmarshal(raw, out); err != nil {
		t.Fatalf("Python output: %v %s", err, raw)
	}
}
// pythonDataDir copies a Go data directory for the Python reference, with the
// database rewritten in the version-1 layout Python reads.
func pythonDataDir(t *testing.T, dir string) string {
	t.Helper()
	copied := filepath.Join(t.TempDir(), "data")
	if err := os.CopyFS(copied, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	repotest.DowngradeToV1(t, filepath.Join(copied, "configflow.sqlite3"))
	return copied
}

func TestR14ClassifyPython(t *testing.T) {
	type sample struct {
		Content  string `json:"content"`
		Behavior string `json:"behavior"`
	}
	samples := []sample{{r14Old, "classical"}, {"payload:\n - IP-CIDR,192.0.2.1/24\n - DOMAIN,example.com", "classical"}, {"domain:example.com\nfull:another.example.com\nkeyword:example", "domain"}, {"IP-CIDR6,2001:db8::/32\nGEOIP,example", "classical"}, {"", "classical"}, {"# empty", "classical"}, {"<html>bad</html>", "classical"}, {"{}", "classical"}, {"192.0.2.1/24\nexample.com", "ipcidr"}, {"payload:\n - [bad]", "classical"}, {"payload: bad", "classical"}, {"payload: [unclosed", "classical"}, {"IP-CIDR,::1/128", "classical"}, {"IP-CIDR6,192.0.2.1/24", "classical"}, {"please log in", "classical"}, {"example.com", "mrs"}, {"payload:\n - true\n - 1\n - null\n - invalid prose", "classical"}, {"IP-CIDR,192.0.2.0/24", "domain"}, {"::1/128\n192.0.2.1/24", "ipcidr"}, {"\ufeffDOMAIN,example.com", "classical"}, {"domain:198.51.100.0/24", "ipcidr"}, {"full:203.0.113.1", "ipcidr"}, {"198.51.100.0/24\r203.0.113.0/24", "ipcidr"}, {"payload:\r - IP-CIDR,192.0.2.1/24\r - DOMAIN,example.com", "classical"}}
	var expected []struct {
		Value map[string]json.RawMessage `json:"value"`
		Error string                     `json:"error"`
	}
	r14Python(t, "", map[string]any{"action": "classify", "cases": samples}, &expected)
	for i, tc := range samples {
		t.Run(tc.Content+"/"+tc.Behavior, func(t *testing.T) {
			got, err := ClassifySyncRules(tc.Content, tc.Behavior)
			if expected[i].Error != "" {
				if err == nil || err.Error() != expected[i].Error {
					t.Fatalf("error %v expected %q", err, expected[i].Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(mustPolicyJSON(got), &fields)
			for _, key := range []string{"name", "source_type", "library_id"} {
				delete(fields, key)
			}
			r12Compare(t, "classification", fields, expected[i].Value)
		})
	}
}
func r14Plan(t *testing.T, ctx context.Context, d *RouterOS, r *Router, desired *Desired) *Plan {
	t.Helper()
	desired.ROSPolicy.Previous = slices.Clone(d.policyManifest)
	state, err := r08InspectState(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := OwnedROSPolicyRows(d.policyManifest, state.ROSPolicy)
	if err != nil {
		owned = nil
	}
	ids := map[string]bool{}
	for _, v := range owned {
		if v.Path == rosMangle {
			ids[v.ID] = true
		}
	}
	state.NatContext.Mangle = []Row{}
	for _, row := range (*state.ROSPolicy)[rosMangle] {
		if !ids[row.id()] {
			copy := cloneRow(row)
			delete(copy, "bytes")
			delete(copy, "packets")
			state.NatContext.Mangle = append(state.NatContext.Mangle, copy)
		}
	}
	RetirePolicyRoutes(state)
	report, err := BuildReport(desired, state)
	if err != nil {
		t.Fatal(err)
	}
	report.ReturnRouting = &ReturnStatus{Status: "disabled"}
	plan, err := BuildPlan(ctx, r, &Inspection{Desired: desired, State: state, Report: report}, d)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func TestR14DeltaPython(t *testing.T) {
	for _, mode := range []string{"static", "policy", "nat", "unchanged", "foreign_cf", "foreign_target", "static_drift", "nat_reference", "policy_reference", "delete_failure", "verify_failure"} {
		t.Run(mode, func(t *testing.T) {
			state := r08State()
			if mode == "nat" || mode == "nat_reference" || mode == "delete_failure" {
				delete(state.Tables[rosNAT][0], "out-interface")
			}
			f := routerfake.New(nil)
			f.Reset(state)
			http := httptest.NewServer(f)
			defer http.Close()
			d := NewRouterOS(http.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			r := r10Router()
			r.ID = "r-sync"
			r.ManageReturnRouting = false
			policy := mode == "policy" || mode == "policy_reference"
			old := r08Desired(policy)
			old.IPv6CIDRs = []ProxyCIDR{}
			old.BlockingWarnings = []string{}
			plan := r14Plan(t, t.Context(), d, r, old)
			if len(plan.Blockers) > 0 {
				t.Fatal(plan.Blockers)
			}
			if result := Execute(t.Context(), plan, d, nil); !result.OK {
				t.Fatalf("enrollment %+v", result)
			}
			static, err := syncStaticResources(t.Context(), d, old)
			if err != nil {
				t.Fatal(err)
			}
			nat, err := d.NATResources(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			baseline := &RuleSyncBaseline{Control: "synthetic", Rules: map[string]*RuleSyncRecord{}, Desired: old, StaticResources: static, Policy: syncNull(slices.Clone(d.policyManifest)), NATList: nat}
			next := r08Desired(policy)
			next.IPv6CIDRs = []ProxyCIDR{}
			next.BlockingWarnings = []string{}
			if policy {
				next.ROSPolicy.CIDRs = []ProxyCIDR{{"203.0.113.0/24", []string{"Synthetic"}}}
			} else if mode != "unchanged" {
				next.ProxyCIDRs[1].CIDR = "203.0.113.0/24"
			}
			next.ROSPolicy.Previous = slices.Clone(d.policyManifest)
			seed := f.Snapshot()
			seed.Calls = nil
			switch mode {
			case "foreign_cf":
				seed.Tables[rosRoutes] = append(seed.Tables[rosRoutes], routerfake.Row{".id": "*E0", "dst-address": "198.51.100.128/25", "gateway": r08Gateway, "comment": "cf-manual", "disabled": "false", "routing-table": "main"})
			case "foreign_target":
				seed.Tables[rosRoutes] = append(seed.Tables[rosRoutes], routerfake.Row{".id": "*E0", "dst-address": "203.0.113.0/24", "gateway": r08Gateway, "comment": "manual", "disabled": "false", "routing-table": "main", "active": "true", "immediate-gw": r08Gateway + "%bridge"})
			case "static_drift":
				for _, row := range seed.Tables[rosRoutes] {
					if row["dst-address"] == r08Target {
						row["comment"] = "outside-edit"
					}
				}
			case "nat_reference":
				seed.Tables[rosFilter] = append(seed.Tables[rosFilter], routerfake.Row{".id": "*E0", "chain": "forward", "action": "drop", "dst-address-list": NATDstList, "disabled": "false"})
			case "policy_reference":
				var manifest ROSPolicyManifest
				_ = json.Unmarshal(d.policyManifest, &manifest)
				for _, item := range manifest.Resources {
					if item.Path == rosAddresses {
						seed.Tables[rosFilter] = append(seed.Tables[rosFilter], routerfake.Row{".id": "*E0", "chain": "forward", "action": "drop", "dst-address-list": item.Config.text("list"), "disabled": "false"})
						break
					}
				}
			case "delete_failure":
				seed.Faults = []routerfake.Fault{{Method: "DELETE", Path: rosRoutes + "/" + static[len(static)-1].ID, Count: 1, Status: 403}}
			case "verify_failure":
				seed.Faults = []routerfake.Fault{{Method: "PUT", Path: rosRoutes, Count: 1, PatchCreated: nil, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosRoutes: {static[0].ID: {"comment": "outside-edit"}}}}}
			}
			input := map[string]any{"action": "delta", "state": seed, "router": r, "desired": next, "baseline": baseline}
			var expected struct {
				Value  *RuleSyncDelta  `json:"value"`
				Error  string          `json:"error"`
				Class  string          `json:"class"`
				Policy json.RawMessage `json:"policy"`
			}
			r14Python(t, http.URL, input, &expected)
			python := f.Snapshot()
			f.Reset(seed)
			d.Close()
			d = NewRouterOS(http.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			d.SetPolicyManifest(baseline.Policy)
			plan = r14Plan(t, t.Context(), d, r, next)
			got, err := ApplyRuleSyncDelta(t.Context(), d, next, plan, baseline, nil)
			if expected.Error != "" {
				if err == nil {
					t.Fatalf("accepted Python rejection %s", expected.Error)
				}
				if err.Error() != expected.Error {
					t.Errorf("error %s expected %s", err, expected.Error)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				r12Compare(t, "delta", got, expected.Value)
			}
			if mode == "verify_failure" {
				if expected.Class != "ReleaseError" {
					t.Fatalf("negative control missed final verifier: %+v", expected)
				}
				after := f.Snapshot().Tables[rosRoutes]
				foundOld, foundNew := false, false
				for _, row := range after {
					if row["dst-address"] == r08Target {
						foundOld = true
					}
					if row["dst-address"] == "203.0.113.0/24" {
						foundNew = true
					}
				}
				if !foundOld || foundNew {
					t.Fatal("verification failure did not compensate all destinations")
				}
				if len(r08Writes(t, f.Calls()).([]any)) <= len(r08Writes(t, python.Calls).([]any)) {
					t.Fatal("Python ReleaseError regression control no longer demonstrates compensation")
				}
			} else {
				r12Compare(t, "device state", f.Snapshot().Tables, python.Tables)
				r12Compare(t, "policy ownership", syncNull(d.policyManifest), syncNull(expected.Policy))
				r12Compare(t, "raw operation effects", r08Writes(t, f.Calls()), r08Writes(t, python.Calls))
			}

		})
	}
}

func TestR14ServicePython(t *testing.T) {
	for _, mode := range []string{"synced", "download", "controls", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			s, f, r := r14Fixture(t)
			seed := f.Snapshot()
			input := map[string]any{"action": "service", "state": seed, "data_dir": s.Repo.DataDir(), "agent": r12SourceAgent(s.Agents), "id": r.ID, "content": r14New}
			if mode == "download" {
				input["download_error"] = true
				s.Download = func(context.Context, json.RawMessage, json.RawMessage) (string, error) {
					return "", &syncDownloadError{routerError(messageSyncDownloadError + "ConnectionError")}
				}
			}
			if mode == "controls" {
				input["changed_control"] = true
				receipt, _ := s.store().Read(t.Context(), "default", r.ID)
				receipt.Baseline.Control = "changed"
				if err := s.store().Write(t.Context(), "default", r.ID, receipt); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid" {
				input["content"] = "please log in"
				s.Download = func(context.Context, json.RawMessage, json.RawMessage) (string, error) { return "please log in", nil }
			}
			var expected RuleSyncStatus
			r14Python(t, r.Endpoint, input, &expected)
			python := f.Snapshot()
			f.Reset(seed)
			got, err := s.Sync(t.Context(), "default", r.ID, "manual")
			if err != nil {
				t.Fatal(err)
			}
			// Timestamps are receipt clock readings, not hash/ownership evidence.
			got.CheckedAt, got.SyncedAt, got.UpdatedAt = nil, nil, nil
			expected.CheckedAt, expected.SyncedAt, expected.UpdatedAt = nil, nil, nil
			r12Compare(t, "service response", got, expected)
			r12Compare(t, "service device", f.Snapshot().Tables, python.Tables)
		})
	}
}
func r12SourceAgent(source AgentSource) *GatewayAgent { return source.(r12Source).gateway }
