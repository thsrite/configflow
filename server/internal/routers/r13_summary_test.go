package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func r13Stage(key string, kinds ...string) Stage {
	s := stageOf(key, false, []Op{})
	for _, kind := range kinds {
		s.Ops = append(s.Ops, OpHead{Kind: kind, Summary: "synthetic"})
	}
	return s
}
func TestR13SummariesPython(t *testing.T) {
	base := func() *Report { return &Report{InSync: true} }
	all := base()
	all.InSync = false
	all.Parts = []ReportPart{{Key: "ipv6_fakeip", Section: IPv6FakeIPReport{Enabled: true, Status: "missing"}}, {Key: "ipv6_routes", Section: IPv6RoutesReport{Enabled: true, Items: []IPv6RouteItem{{Status: "missing"}, {Status: "borrowed"}}}}, {Key: "ipv6_pd_guard", Section: IPv6PDGuardReport{Enabled: true, Items: []IPv6PDItem{{Status: "missing"}, {Status: "synced"}}}}}
	withdraw := base()
	withdraw.Parts = []ReportPart{{Key: "ipv6_fakeip", Section: IPv6FakeIPReport{Enabled: false}}, {Key: "ipv6_routes", Section: IPv6RoutesReport{Enabled: false}}, {Key: "dns_guard", Section: DNSGuardReport{Enabled: false}}, {Key: "ipv6_pd_guard", Section: IPv6PDGuardReport{Enabled: false}}}
	skipped := base()
	skipped.InSync = false
	skipped.Parts = []ReportPart{{Key: "ipv6_routes", Section: IPv6RoutesReport{Enabled: true, Skip: []string{"synthetic missing next hop"}}}}
	conflict := base()
	conflict.InSync = false
	conflict.Routes.Items = []RouteItem{{CIDR: "198.51.100.0/24", Status: "conflict", Detail: "synthetic conflict"}}
	pd := base()
	pd.InSync = false
	pd.Parts = []ReportPart{{Key: "ipv6_pd_guard", Section: IPv6PDGuardReport{Enabled: true, Items: []IPv6PDItem{{Status: "adopt"}}}}}
	cases := []struct {
		name   string
		report *Report
		plan   *Plan
	}{
		{"synced", base(), &Plan{Stages: []Stage{r13Stage(StageRoutes), r13Stage(StageNATBypass)}}},
		{"normal stages", all, &Plan{Stages: []Stage{r13Stage(StageReturnRouting, "return_routing_apply"), r13Stage(StageRoutes, "route_add"), r13Stage(StageIPv6FakeIP, "ipv6_fakeip_replace"), r13Stage(StageIPv6Routes, "ipv6_routes_replace"), r13Stage(StageDHCPDNS, "dhcp_dns_update"), r13Stage(StageIPv6DNS, "ipv6_dns_update"), r13Stage(StageIPv6PDGuard, "ipv6_pd_guard_sync"), r13Stage(StageReturnCleanup, "return_routing_remove")}}},
		{"withdraw", withdraw, &Plan{Stages: []Stage{r13Stage(StageIPv6FakeIP, "ipv6_fakeip_replace"), r13Stage(StageIPv6Routes, "ipv6_routes_replace"), r13Stage(StageDNSGuard, "dns_guard_replace"), r13Stage(StageIPv6PDGuard, "ipv6_pd_guard_sync")}}},
		{"blocked", all, &Plan{Stages: []Stage{r13Stage(StageRoutes, "route_add")}, Blockers: []string{"synthetic blocker", "second blocker"}}},
		{"skipped", skipped, &Plan{Stages: []Stage{{Key: StageIPv6Routes, Title: StageTitles[StageIPv6Routes], Skipped: pointer(true), Ops: []Op{}}}}},
		{"conflict", conflict, &Plan{}},
		{"adopt", pd, &Plan{Stages: []Stage{r13Stage(StageRoutes, "route_comment"), r13Stage(StageIPv6PDGuard, "ipv6_pd_guard_sync")}}},
		{"generic", &Report{}, &Plan{}},
	}
	payload := []map[string]any{}
	for _, c := range cases {
		payload = append(payload, map[string]any{"report": c.report, "plan": c.plan})
	}
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	wd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	input, e := json.Marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r13_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	cmd.Stdin = bytes.NewReader(input)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("Python %v %s", e, raw)
	}
	var expected []PendingSummary
	if e = json.Unmarshal(raw, &expected); e != nil {
		t.Fatal(e, string(raw))
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := device.JSON(c.report)
			actual := Summarize(c.report, c.plan)
			if !reflect.DeepEqual(expected[i], actual) {
				t.Fatalf("Python %+v Go %+v", expected[i], actual)
			}
			fromJSON, e := SummarizeJSON(device.JSON(c.report), device.JSON(c.plan))
			if e != nil || !reflect.DeepEqual(actual, fromJSON) {
				t.Fatalf("C07 projection differs %+v %v", fromJSON, e)
			}
			if !bytes.Equal(before, device.JSON(c.report)) {
				t.Fatal("summary mutated report")
			}
		})
	}
	disabled := r13Stage(StageRoutes, "route_add")
	disabled.Enabled = pointer(false)
	if got := Summarize(base(), &Plan{Stages: []Stage{disabled}}); got.State != "synced" || got.Pending != 0 {
		t.Fatal("disabled stage counted", got)
	}
	if _, e := SummarizeJSON(json.RawMessage(`{"in_sync":"true"}`), json.RawMessage(`{}`)); e == nil {
		t.Fatal("wrong typed report accepted")
	}
}
