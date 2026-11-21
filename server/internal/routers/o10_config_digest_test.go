package routers

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

// TestO10RouterDigestFollowsOxiDNSProjection checks that router check receipts
// (built from Records) and the notification snapshot digest the same view, that
// the digest ignores Agent deployment history, and that it still changes when an
// OxiDNS Agent member read by policy entries changes.
func TestO10RouterDigestFollowsOxiDNSProjection(t *testing.T) {
	ctx := context.Background()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setAgents := func(agents string) {
		t.Helper()
		if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
			return s.SetJSON("agents", json.RawMessage(agents))
		}); err != nil {
			t.Fatal(err)
		}
	}
	routerJSON := json.RawMessage(`{"id":"r1","type":"routeros"}`)
	digest := func() (management, snapshot string) {
		t.Helper()
		records, err := (&ManagementService{Repo: r}).Records(ctx, "default")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := records.Encoded()
		if err != nil {
			t.Fatal(err)
		}
		if management, err = notifications.RouterDigest(raw, routerJSON); err != nil {
			t.Fatal(err)
		}
		view, err := r.ConfigWithOxiDNSAgents(ctx, "default", nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot, err = notifications.RouterDigest(encoded, routerJSON); err != nil {
			t.Fatal(err)
		}
		return management, snapshot
	}

	tests := []struct {
		name    string
		agents  string
		changed bool
	}{
		{"base", `[{"id":"a-oxi","name":"Oxi","host":"192.0.2.10","service_type":"oxidns","profile_id":"default","token":"synthetic-token"}]`, false},
		{"deployment history only", `[{"id":"a-oxi","name":"Oxi","host":"192.0.2.10","service_type":"oxidns","profile_id":"default","token":"synthetic-token","deployments":{"d1":{"id":"d1","status":"success"}}}]`, false},
		{"token only", `[{"id":"a-oxi","name":"Oxi","host":"192.0.2.10","service_type":"oxidns","profile_id":"default","token":"synthetic-other"}]`, false},
		{"oxidns host", `[{"id":"a-oxi","name":"Oxi","host":"192.0.2.11","service_type":"oxidns","profile_id":"default","token":"synthetic-other"}]`, true},
	}
	var first string
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgents(tt.agents)
			management, snapshot := digest()
			if management != snapshot {
				t.Fatalf("router receipt digest %s != notification digest %s", management, snapshot)
			}
			if i == 0 {
				first = management
				return
			}
			if got := management != first; got != tt.changed {
				t.Fatalf("digest changed = %v, want %v", got, tt.changed)
			}
		})
	}
}
