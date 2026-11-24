package routers_test

import (
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

// F2 (Python-compared): a NAT-list member or an old per-destination rule
// written as an address range is not the CIDR the range summarises to.
// Python's _key keeps the range text, so the range member is replaced by the
// CIDR and range-form cf-mihomo rules are never merged or deleted as legacy.
func TestR07RangeKeysMatchPython(t *testing.T) {
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
	const first = "198.51.100.0-198.51.100.0" // summarises to r07CIDR(0)
	variants := map[string]func(int) routerfake.State{
		"range_member": func(count int) routerfake.State {
			state := r07State("list", count)
			for _, row := range state.Tables["/ip/firewall/address-list"] {
				if row["list"] == ros.NATDstList && row["address"] == r07CIDR(0) {
					row["address"] = first
				}
			}
			return state
		},
		"range_legacy": func(count int) routerfake.State {
			state := r07State("legacy", count)
			for _, row := range state.Tables["/ip/firewall/nat"] {
				if row["comment"] == "cf-mihomo" && row["dst-address"] == r07CIDR(0) {
					row["dst-address"] = first
				}
			}
			return state
		},
	}
	for name, build := range variants {
		for _, count := range []int{1, 5} {
			t.Run(fmt.Sprintf("%s/%d", name, count), func(t *testing.T) {
				// One more desired CIDR than the device protects: a plan is due,
				// so the legacy candidates are actually evaluated.
				want := r07Desired(count + 1)
				tc := boundaryCase{action: "plan", state: build(count), op: map[string]any{"desired": want, "gateway": "192.0.2.3", "skipped": []string{}}}
				py, _ := runPythonBoundary(t, python, root, helper, srv.URL, f, tc)
				if py.Error != nil {
					t.Fatalf("Python %v", py.Error)
				}
				f.Reset(tc.state)
				d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
				defer d.Close()
				state, err := d.ReadState(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				report, err := routers.BuildReport(want, state)
				if err != nil {
					t.Fatal(err)
				}
				result := rospolicy.PlanNATLists(context.Background(), report.Routes, report.NatBypass, &state.State, state.NatContext, &state.NATLists, &state.NATListsReader, state.ROSPolicyPlan, "192.0.2.3", nil)
				a, b := r07Comparable(t, py.Result), r07Comparable(t, result)
				if !reflect.DeepEqual(a, b) {
					t.Errorf("plan %s", jsonDiff(a, b))
				}
			})
		}
	}
}
