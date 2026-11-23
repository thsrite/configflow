package routers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
)

func r07Comparable(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func r07Calls(calls []routerfake.Call) []routerfake.Call {
	out := append([]routerfake.Call{}, calls...)
	for i, call := range out {
		body, ok := call.Body.(map[string]any)
		if !ok {
			if row, yes := call.Body.(routerfake.Row); yes {
				body = map[string]any(row)
			}
		}
		if script, ok := body["script"].(string); ok && strings.Contains(script, "address-list/add") {
			if batch, err := routerfake.ParseAddressBatch(script); err == nil {
				out[i].Body = map[string]any{"as-string": body["as-string"], "address_batch": batch}
			}
		}
		if script, ok := body["script"].(string); ok && strings.HasPrefix(script, ":local f 0\n") {
			adds, err := routerfake.ParseConsoleAdds(script)
			if err == nil {
				out[i].Body = map[string]any{"as-string": body["as-string"], "console_adds": adds}
			}
		}
	}
	return out
}

func TestNATListWriteBoundary(t *testing.T) {
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("Python reference unavailable")
	}
	wd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(wd, "../../.."))
	helper := filepath.Join(wd, "testdata/r07_reference.py")
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, variant := range []string{"new", "legacy", "stale", "list", "foreign"} {
		for _, count := range []int{1, 5, 40} {
			for _, action := range []string{"nat_apply", "nat_roundtrip"} {
				t.Run(fmt.Sprintf("%s/%d/%s", variant, count, action), func(t *testing.T) {
					initial := r07State(variant, count)
					f.Reset(initial)
					d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
					state, err := d.ReadState(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					report, err := routers.BuildReport(r07Desired(count+1), state)
					if err != nil {
						t.Fatal(err)
					}
					plan := routers.PlanNATLists(context.Background(), report.Routes, report.NatBypass, &state.State, state.NatContext, &state.NATLists, &state.NATListsReader, state.ROSPolicyPlan, "192.0.2.3", nil)
					d.Close()
					if plan == nil || len(plan.Blockers) > 0 || len(plan.Ops) != 1 {
						t.Fatalf("bad fixture plan %#v", plan)
					}
					var op map[string]any
					_ = json.Unmarshal(rawValue(plan.Ops[0]), &op)
					tc := boundaryCase{action: action, state: initial, op: op}
					py, pyState := runPythonBoundary(t, python, root, helper, srv.URL, f, tc)
					f.Reset(initial)
					tc.action = strings.TrimPrefix(action, "nat_")
					goOut := runGoBoundary(tc, srv.URL)
					goState := f.Snapshot()
					switch os.Getenv("CONFIGFLOW_R07_MUTATE") {
					case "1", "result":
						goOut.Result = map[string]any{"deliberately_wrong": true}
					case "journal":
						var undo map[string]any
						_ = json.Unmarshal(rawValue(goOut.Result), &undo)
						steps := undo["steps"].([]any)
						steps[0].(map[string]any)["id"] = "*BAD"
						goOut.Result = undo
					case "request":
						goState.Calls[0].Method = "BROKEN"
					case "table":
						goState.Tables["/ip/firewall/nat"][0]["action"] = "BROKEN"
					}
					a, b := r07Comparable(t, py), r07Comparable(t, goOut)
					if !reflect.DeepEqual(a, b) {
						t.Errorf("outcome %s", jsonDiff(a, b))
					}
					a, b = r07Comparable(t, r07Calls(pyState.Calls)), r07Comparable(t, r07Calls(goState.Calls))
					if !reflect.DeepEqual(a, b) {
						t.Errorf("wire %s", jsonDiff(a, b))
					}
					a, b = r07Comparable(t, pyState.Tables), r07Comparable(t, goState.Tables)
					if !reflect.DeepEqual(a, b) {
						t.Errorf("tables %s", jsonDiff(a, b))
					}
				})
			}
		}
	}
}

func runR07Boundary(t *testing.T, tc boundaryCase) {
	t.Helper()
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("Python reference unavailable")
	}
	wd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(wd, "../../.."))
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	py, pyState := runPythonBoundary(t, python, root, filepath.Join(wd, "testdata/r07_reference.py"), srv.URL, f, tc)
	f.Reset(tc.state)
	goOut := boundaryOutcome{Records: []map[string]any{}}
	if tc.action == "addresses" {
		d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
		defer d.Close()
		var entries []routers.Row
		raw, _ := json.Marshal(tc.op["entries"])
		_ = json.Unmarshal(raw, &entries)
		ids := routers.ROSIDs{"/ip/firewall/address-list": map[string]bool{}}
		for _, row := range tc.state.Tables["/ip/firewall/address-list"] {
			ids["/ip/firewall/address-list"][fmt.Sprint(row[".id"])] = true
		}
		journal := []routers.NATStep{}
		resources, err := d.CreateAddresses(context.Background(), entries, &journal, ids)
		if err == nil {
			goOut.Result = resources
		} else {
			goOut.Error = &boundaryError{goErrorClass(err), err.Error()}
		}
		for _, step := range journal {
			goOut.Records = append(goOut.Records, map[string]any{"step": step})
		}
	} else if tc.action == "nat_restore" {
		d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
		defer d.Close()
		undo, err := d.Apply(context.Background(), rawValue(tc.op["sync"]))
		if err == nil {
			goOut.Records = append(goOut.Records, map[string]any{"undo": json.RawMessage(undo)})
			response, e := http.Post(srv.URL+"/__r04/mutate", "application/json", bytes.NewReader(rawValue(tc.op["mutation"])))
			if e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			var restored json.RawMessage
			restored, err = d.Apply(context.Background(), undo)
			if err == nil {
				goOut.Result = restored
			}
		}
		if err != nil {
			goOut.Error = &boundaryError{goErrorClass(err), err.Error()}
		}
	} else if tc.action == "helper" {
		goOut = runR07Helper(tc, srv.URL)
	} else {
		tc.action = strings.TrimPrefix(tc.action, "nat_")
		goOut = runGoBoundary(tc, srv.URL)
	}
	a, b := r07Comparable(t, py), r07Comparable(t, goOut)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("outcome %s", jsonDiff(a, b))
	}
	a, b = r07Comparable(t, r07Calls(pyState.Calls)), r07Comparable(t, r07Calls(f.Calls()))
	if !reflect.DeepEqual(a, b) {
		diff := jsonDiff(a, b)
		t.Errorf("wire %s", diff[:min(len(diff), 1500)])
	}
	a, b = r07Comparable(t, pyState.Tables), r07Comparable(t, f.Snapshot().Tables)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("tables %s", jsonDiff(a, b))
	}
}

func r07PlannedOperation(t *testing.T, initial routerfake.State, count int) map[string]any {
	t.Helper()
	f := routerfake.New(initial.Tables)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	state, err := d.ReadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	report, err := routers.BuildReport(r07Desired(count), state)
	if err != nil {
		t.Fatal(err)
	}
	plan := routers.PlanNATLists(context.Background(), report.Routes, report.NatBypass, &state.State, state.NatContext, &state.NATLists, &state.NATListsReader, state.ROSPolicyPlan, "192.0.2.3", nil)
	if plan == nil || len(plan.Blockers) > 0 || len(plan.Ops) != 1 {
		t.Fatalf("bad fixture plan %#v", plan)
	}
	var op map[string]any
	_ = json.Unmarshal(rawValue(plan.Ops[0]), &op)
	return op
}

func TestNATListFailureBoundary(t *testing.T) {
	for _, variant := range []string{"new", "legacy", "stale"} {
		initial := r07State(variant, 5)
		op := r07PlannedOperation(t, initial, 6)
		for _, fault := range []struct {
			name   string
			faults []routerfake.Fault
		}{
			{"partial_source", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 3}}},
			{"partial_destination", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 16}}},
			{"lost_source", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 3, AfterCommit: true, LoseReply: true}}},
			{"lost_destination", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 16, AfterCommit: true, LoseReply: true}}},
			{"refused_rule", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/nat", Count: 1}}},
			{"lost_rule", []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/nat", Count: 1, AfterCommit: true, LoseReply: true}}},
			{"remove_fallback", []routerfake.Fault{{Method: "POST", Path: "/ip/firewall/nat/remove", Count: 1}}},
			{"partial_remove", []routerfake.Fault{{Method: "DELETE", Path: "/ip/firewall/nat/*3EB"}}},
		} {
			t.Run(variant+"/"+fault.name, func(t *testing.T) {
				state := initial
				state.Faults = fault.faults
				runR07Boundary(t, boundaryCase{action: "nat_apply", state: state, op: op})
			})
		}
	}
}

func TestAddressWriteBoundary(t *testing.T) {
	for _, count := range []int{1, 3, 200, 201, 450, 3000} {
		for _, failure := range []string{"none", "partial", "lost", "unreadable", "unknown", "bad_marker"} {
			if count > 450 && failure != "none" {
				continue
			}
			t.Run(fmt.Sprintf("%d/%s", count, failure), func(t *testing.T) {
				addresses := []string{}
				for i := 0; i < count; i++ {
					addresses = append(addresses, fmt.Sprintf("198.18.%d.%d/32", i/256, i%256))
				}
				entries := ros.NATEntries(routers.NATDstList, addresses)
				state := routerfake.State{Tables: map[string][]routerfake.Row{}}
				switch failure {
				case "partial":
					state.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 1}}
				case "lost":
					state.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/address-list", Count: 1, AfterCommit: true, LoseReply: true}}
				case "unreadable":
					state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 2}, {Method: "POST", Path: "/execute", Count: 3}, {Method: "POST", Path: "/execute", Count: 4}}
				case "unknown":
					state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, ExtraRows: map[string][]routerfake.Row{"/ip/firewall/address-list": {{".id": "*F0001", "list": routers.NATDstList, "address": "203.0.113.9", "comment": "foreign", "disabled": "false"}}}}}
				case "bad_marker":
					state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, Status: 200, Body: routerfake.Row{"ret": "cf-done:x:"}}}
				}
				runR07Boundary(t, boundaryCase{action: "addresses", state: state, op: map[string]any{"entries": entries}})
			})
		}
	}
}

func TestNATRestoreFaultBoundary(t *testing.T) {
	initial := r07State("legacy", 5)
	op := r07PlannedOperation(t, initial, 6)
	for _, tc := range []struct {
		name     string
		mutation routerfake.ExternalWrite
	}{
		{"anchor_gone", routerfake.ExternalWrite{Fault: routerfake.Fault{RemoveIDs: map[string][]string{"/ip/firewall/nat": {"*3"}}}}},
		{"foreign_reference", routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{"/ip/firewall/raw": {{".id": "*F1", "dst-address-list": routers.NATDstList, "chain": "prerouting", "action": "accept"}}}}}},
		{"changed_member", routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{"/ip/firewall/address-list": {"*65": {"comment": "external"}}}}}},
		{"lost_restore", routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 2, LoseReply: true}}}},
		{"partial_restore", routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "PUT", Path: "/ip/firewall/nat", Count: 3}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runR07Boundary(t, boundaryCase{action: "nat_restore", state: initial, op: map[string]any{"sync": op, "mutation": tc.mutation}})
		})
	}
}

func TestNATListRemovalBoundary(t *testing.T) {
	for _, count := range []int{1, 5, 600} {
		initial := r07State("list", count)
		for _, target := range []int{0, count - 1} {
			for _, action := range []string{"nat_apply", "nat_roundtrip"} {
				t.Run(fmt.Sprintf("%d/to%d/%s", count, target, action), func(t *testing.T) {
					op := r07PlannedOperation(t, initial, target)
					runR07Boundary(t, boundaryCase{action: action, state: initial, op: op})
				})
			}
		}
	}
	initial := r07State("list", 5)
	for _, action := range []string{"nat_apply", "nat_roundtrip"} {
		t.Run("restrict/"+action, func(t *testing.T) {
			op := r07PlannedOperation(t, initial, 6)
			op["replace_rule"], op["out_interface"] = true, "bridge"
			runR07Boundary(t, boundaryCase{action: action, state: initial, op: op})
		})
	}
}
