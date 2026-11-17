package routers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func r07State(variant string, count int) routerfake.State {
	const nat = "/ip/firewall/nat"
	const addresses = "/ip/firewall/address-list"
	tables := map[string][]routerfake.Row{"/ip/route": {{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "immediate-gw": "bridge", "active": "true", "connect": "true", "routing-table": "main", "disabled": "false"}}, nat: {{".id": "*3", "chain": "srcnat", "action": "masquerade", "disabled": "false"}}}
	switch variant {
	case "not_required":
		tables[nat][0]["out-interface"] = "pppoe-out1"
	case "no_nat":
		tables[nat] = []routerfake.Row{}
	case "adopt":
		tables[nat] = append([]routerfake.Row{{".id": "*A", "chain": "srcnat", "action": "accept", "src-address": "192.0.2.0/24", "comment": "manual", "disabled": "false"}}, tables[nat]...)
	case "unknown":
		tables[nat] = append([]routerfake.Row{{".id": "*A", "chain": "srcnat", "action": "accept", "protocol": "tcp", "comment": "manual", "disabled": "false"}}, tables[nat]...)
	}
	gateway := netip.MustParseAddr("192.0.2.3")
	if variant == "legacy" || variant == "stale" || variant == "retained" {
		id := 1000
		targets := []string{}
		for i := 0; i < count; i++ {
			targets = append(targets, r07CIDR(i))
		}
		if variant != "legacy" {
			targets = append(targets, "203.0.113.0/24")
		}
		old := []routerfake.Row{}
		for _, cidr := range targets {
			for _, source := range routers.ClientSourceNetworks(gateway) {
				id++
				old = append(old, routerfake.Row{".id": fmt.Sprintf("*%X", id), "chain": "srcnat", "action": "accept", "comment": "cf-mihomo", "src-address": source.String(), "dst-address": cidr, "disabled": "false"})
			}
		}
		tables[nat] = append(old, tables[nat]...)
		if variant == "retained" {
			tables["/ip/route"] = append(tables["/ip/route"], routerfake.Row{".id": "*4", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.3%bridge", "disabled": "false"})
		}
	}
	if variant == "list" || variant == "foreign" || variant == "dynamic" || variant == "disabled" || variant == "ref" || variant == "modified" || variant == "shadowed" {
		rule := routerfake.Row{".id": "*5", "chain": "srcnat", "action": "accept", "src-address-list": routers.NATSrcList, "dst-address-list": routers.NATDstList, "comment": routers.NATListComment, "disabled": "false"}
		tables[nat] = append([]routerfake.Row{rule}, tables[nat]...)
		for i, source := range routers.ClientSourceNetworks(gateway) {
			tables[addresses] = append(tables[addresses], routerfake.Row{".id": fmt.Sprintf("*%X", 2000+i), "list": routers.NATSrcList, "address": source.String(), "comment": routers.NATListComment, "disabled": "false"})
		}
		for i := 0; i < count; i++ {
			tables[addresses] = append(tables[addresses], routerfake.Row{".id": fmt.Sprintf("*%X", 3000+i), "list": routers.NATDstList, "address": r07CIDR(i), "comment": routers.NATListComment, "disabled": "false"})
		}
		switch variant {
		case "foreign":
			tables[addresses] = append(tables[addresses], routerfake.Row{".id": "*FFFF", "list": routers.NATDstList, "address": "203.0.113.0/24", "comment": "mine", "disabled": "false"})
		case "dynamic":
			tables[addresses][0]["dynamic"] = "true"
		case "disabled":
			tables[addresses][0]["disabled"] = "true"
		case "ref":
			tables["/ip/firewall/raw"] = []routerfake.Row{{".id": "*F1", "dst-address-list": routers.NATDstList, "action": "accept"}}
		case "modified":
			rule["protocol"] = "tcp"
		case "shadowed":
			tables[nat] = []routerfake.Row{tables[nat][1], rule}
		}
	}
	return routerfake.State{Tables: tables}
}
func r07CIDR(index int) string {
	if index >= 256 {
		return fmt.Sprintf("198.18.%d.%d/32", index/256, index%256)
	}
	return fmt.Sprintf("198.51.100.%d/32", index)
}
func r07Desired(count int) *routers.Desired {
	raw := map[string]any{"fake_ip_range": "", "proxy_gateway": "192.0.2.3", "warnings": []string{}, "proxy_cidrs": []map[string]any{}}
	items := []map[string]any{}
	for i := 0; i < count; i++ {
		items = append(items, map[string]any{"cidr": r07CIDR(i), "sources": []string{"synthetic"}})
	}
	raw["proxy_cidrs"] = items
	b, _ := json.Marshal(raw)
	var want routers.Desired
	_ = json.Unmarshal(b, &want)
	return &want
}

func TestNATListPlanBoundary(t *testing.T) {
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
	for _, variant := range []string{"new", "not_required", "no_nat", "adopt", "unknown", "legacy", "stale", "retained", "list", "foreign", "dynamic", "disabled", "ref", "modified", "shadowed"} {
		for _, count := range []int{1, 5, 12} {
			for _, change := range []int{-1, 0, 1} {
				t.Run(fmt.Sprintf("%s/%d/%d", variant, count, change), func(t *testing.T) {
					want := r07Desired(count + change)
					op := map[string]any{"desired": want, "gateway": "192.0.2.3", "skipped": []string{}}
					tc := boundaryCase{action: "plan", state: r07State(variant, count), op: op}
					py, pyState := runPythonBoundary(t, python, root, helper, srv.URL, f, tc)
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
					result := routers.PlanNATLists(context.Background(), report, state, "192.0.2.3", nil)
					a, b := r07Comparable(t, py.Result), r07Comparable(t, result)
					if !reflect.DeepEqual(a, b) {
						t.Errorf("plan %s", jsonDiff(a, b))
					}
					a, b = r07Comparable(t, pyState.Calls), r07Comparable(t, f.Calls())
					if !reflect.DeepEqual(a, b) {
						t.Errorf("wire %s", jsonDiff(a, b))
					}
				})
			}
		}
	}
}
