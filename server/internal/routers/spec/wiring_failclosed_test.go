package spec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/routers/device"
)

func TestComputeDesiredNilLoaderFailsClosed(t *testing.T) {
	profile := json.RawMessage(`{
		"rule_configs":[{"id":"r1","itemType":"ruleset","name":"R1","behavior":"ipcidr","policy":"PROXY","enabled":true}],
		"rule_library":[]
	}`)
	if _, err := ComputeDesired(context.Background(), profile, "192.0.2.1", []string{"r1"}, nil); !errors.Is(err, device.ErrNotPorted) {
		t.Fatalf("ComputeDesired with nil loader and selected rule set = %v, want %v", err, device.ErrNotPorted)
	}
	// Empty rule set selection does not invoke the loader.
	desired, err := ComputeDesired(context.Background(), profile, "192.0.2.1", nil, nil)
	if err != nil || desired == nil {
		t.Fatalf("ComputeDesired with nil loader and no rule sets = %+v, %v", desired, err)
	}
}

func TestNormalizeWithIDOverrideAndDefault(t *testing.T) {
	body := []byte(`{"name":"Synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"admin","password":"secret"}`)
	fixed, err := NormalizeWithID(body, nil, func() string { return "router_fixed" })
	if err != nil || fixed.ID != "router_fixed" {
		t.Fatalf("NormalizeWithID override = %+v, %v", fixed, err)
	}
	generated, err := Normalize(body, nil)
	if err != nil || !strings.HasPrefix(generated.ID, "router_") || len(generated.ID) != len("router_")+12 {
		t.Fatalf("Normalize default ID = %+v, %v", generated, err)
	}
}
