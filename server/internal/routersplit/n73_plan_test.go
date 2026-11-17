package routersplit_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routersplit"
)

func TestN73OpaqueValuesKeepTheRouterPlan(t *testing.T) {
	for _, pair := range [][2]string{
		{"extra: opaque\n", "extra: !custom opaque\n"},
		{"", "? [a, b]\n: opaque\n"},
		{"dns: {fake-ip-range: 198.19.0.0/16}\n", "dns: {fake-ip-range: 198.19.0.0/16, ? [a, b]: opaque}\n"},
		{"extra: [one, two]\n", "extra: !!python/tuple [one, two]\n"},
		{"extra: {value: opaque}\n", "extra: !custom {value: opaque}\n"},
	} {
		plans := make([]any, 2)
		for i, suffix := range pair {
			base, err := routersplit.FullTunBaseYAML(suffix)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := json.Marshal(map[string]any{"mihomo": map[string]any{"custom_config": base}})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := routers.ComputeDesired(profile, "192.0.2.1", nil, func(routers.RuleSet, *routers.LibraryRule) (string, error) {
				t.Fatal("unexpected source read")
				return "", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			plans[i] = plan
		}
		if !reflect.DeepEqual(plans[0], plans[1]) {
			t.Fatalf("opaque metadata changed router plan: %#v / %#v", plans[0], plans[1])
		}
	}
}
