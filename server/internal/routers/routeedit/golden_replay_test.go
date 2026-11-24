package routeedit_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routeedit"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func stripOpaqueHashes(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			if (k == "revision" && x["cidr"] != nil && x["snapshot"] == nil) || (k == "fingerprint" && x["summary"] != nil && x["action"] != nil) {
				if s, ok := item.(string); ok && len(s) == 64 {
					x[k] = "<opaque-sha256>"
				}
			} else if k != "snapshot" {
				stripOpaqueHashes(item)
			}
		}
	case []any:
		for _, item := range x {
			stripOpaqueHashes(item)
		}
	}
}

func comparableValue(t *testing.T, v any) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(goldenMarshal(t, v), &out); err != nil {
		t.Fatal(err)
	}
	stripOpaqueHashes(out)
	return out
}

type rosEditorAdapter struct{ c *ros.Client }

func (a rosEditorAdapter) ReadRoutes(ctx context.Context) ([]*routeedit.EditableRoute, error) {
	return routeedit.ReadROSRoutes(ctx, a.c)
}

func (a rosEditorAdapter) MutateRoute(ctx context.Context, action string, expected *routeedit.EditableRoute, values *routeedit.RouteValues) (*routeedit.RouteMutation, error) {
	return routeedit.MutateROSRoute(ctx, a.c, action, expected, values)
}

type ikuaiEditorAdapter struct{ c *ikuai.Client }

func (a ikuaiEditorAdapter) ReadRoutes(ctx context.Context) ([]*routeedit.EditableRoute, error) {
	return routeedit.ReadIkuaiRoutes(ctx, a.c)
}

func (a ikuaiEditorAdapter) MutateRoute(ctx context.Context, action string, expected *routeedit.EditableRoute, values *routeedit.RouteValues) (*routeedit.RouteMutation, error) {
	return routeedit.MutateIkuaiRoute(ctx, a.c, action, expected, values)
}

// TestRouteEditGoldenReplay records and replays deterministic Go-native
// boundary outputs for R06 (RouterOS & iKuai static route editor and
// reconciliation: read, add, update, delete, conflict/drift).
func TestRouteEditGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	rosSeed := func() routerfake.State {
		return routerfake.State{Tables: map[string][]routerfake.Row{
			"/ip/route": {{
				".id": "*10", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.1",
				"routing-table": "main", "disabled": "false", "comment": "office",
				"distance": "17", "scope": "20", "target-scope": "12",
				"check-gateway": "ping", "pref-src": "192.0.2.2",
				"suppress-hw-offload": "true", "dynamic": "false", "static": "true",
				"active": "true", "blackhole": "false", "immediate-gw": "192.0.2.1%ether1",
				"hw-offloaded": "false",
			}},
		}}
	}
	ikSeed := func() routerfake.IkuaiState {
		return routerfake.IkuaiState{Tables: map[string][]routerfake.Row{
			"static_rt": {{
				"id": 1, "enabled": "yes", "tagname": "RT_1", "ip_type": "4",
				"interface": "lan4", "dst_addr": "203.0.113.0", "gateway": "192.0.2.1",
				"netmask": "255.255.255.0", "prio": 7, "comment": "office", "status": 1,
			}},
		}}
	}

	for _, vendor := range []string{"routeros", "ikuai"} {
		t.Run(vendor+"/crud_and_reconcile", func(t *testing.T) {
			var editor routeedit.RouteEditor
			var tables func() any
			if vendor == "routeros" {
				f := routerfake.New(nil)
				f.Reset(rosSeed())
				srv := httptest.NewServer(f)
				defer srv.Close()
				c := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
				defer c.Close()
				editor = rosEditorAdapter{c}
				tables = func() any { return f.Snapshot().Tables["/ip/route"] }
			} else {
				f := routerfake.NewIkuai(ikSeed())
				srv := httptest.NewServer(f)
				defer srv.Close()
				c := ikuai.NewClient(srv.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
				defer c.Close()
				editor = ikuaiEditorAdapter{c}
				tables = func() any { return f.Snapshot().Tables["static_rt"] }
			}

			r := spec.Router{RouterSettings: spec.RouterSettings{ID: "r1", Type: vendor, Endpoint: "http://example.com"}}
			ctx := context.Background()

			initial, err := routeedit.ReadRoutes(ctx, editor)
			if err != nil {
				t.Fatal(err)
			}

			// Add route via BuildRoutePlan + ExecuteRoutePlan.
			addReq := goldenMarshal(t, map[string]any{
				"action": "add",
				"values": map[string]any{"cidr": "198.51.100.0/24", "gateway": "192.0.2.3", "enabled": true, "comment": "用户 & backup"},
			})
			addPlan, err := routeedit.BuildRoutePlan(r, initial, addReq)
			if err != nil {
				t.Fatal(err)
			}
			addRes, err := routeedit.ExecuteRoutePlan(ctx, editor, initial, addPlan)
			if err != nil {
				t.Fatal(err)
			}
			afterAddRows, err := routeedit.ReadRoutes(ctx, editor)
			if err != nil {
				t.Fatal(err)
			}

			// Update the newly added route.
			updateReq := goldenMarshal(t, map[string]any{
				"action":   "update",
				"id":       addRes.Entry.ID,
				"revision": addRes.Entry.Revision,
				"values":   map[string]any{"cidr": "198.51.100.0/24", "gateway": "192.0.2.4", "enabled": false, "comment": "updated"},
			})
			updatePlan, err := routeedit.BuildRoutePlan(r, afterAddRows, updateReq)
			if err != nil {
				t.Fatal(err)
			}
			updateRes, err := routeedit.ExecuteRoutePlan(ctx, editor, afterAddRows, updatePlan)
			if err != nil {
				t.Fatal(err)
			}
			afterUpdateRows, err := routeedit.ReadRoutes(ctx, editor)
			if err != nil {
				t.Fatal(err)
			}

			// Delete the updated route.
			delReq := goldenMarshal(t, map[string]any{
				"action":   "delete",
				"id":       updateRes.Entry.ID,
				"revision": updateRes.Entry.Revision,
			})
			delPlan, err := routeedit.BuildRoutePlan(r, afterUpdateRows, delReq)
			if err != nil {
				t.Fatal(err)
			}
			delRes, err := routeedit.ExecuteRoutePlan(ctx, editor, afterUpdateRows, delPlan)
			if err != nil {
				t.Fatal(err)
			}

			// Stale revision conflict check (reconciliation refusal).
			_, staleErr := routeedit.BuildRoutePlan(r, afterUpdateRows, goldenMarshal(t, map[string]any{
				"action":   "delete",
				"id":       updateRes.Entry.ID,
				"revision": "stale-revision",
			}))
			staleMsg := ""
			if staleErr != nil {
				staleMsg = staleErr.Error()
			}

			ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R06", "vendor": vendor, "case": "crud_and_reconcile"}), goldenMarshal(t, map[string]any{
				"add_plan":    comparableValue(t, addPlan),
				"add_res":     comparableValue(t, addRes),
				"update_plan": comparableValue(t, updatePlan),
				"update_res":  comparableValue(t, updateRes),
				"del_plan":    comparableValue(t, delPlan),
				"del_res":     comparableValue(t, delRes),
				"stale_err":   staleMsg,
				"final_table": comparableValue(t, tables()),
			}))
		})
	}
}
