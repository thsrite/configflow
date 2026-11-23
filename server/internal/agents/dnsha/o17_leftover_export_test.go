package dnsha

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/repo"
)

// TestO17FollowupHALeftoverExportRestoreDisableCleansDevice proves M2: when
// enabling DNS HA fails and rolling back keepalived also fails, a
// configuration-only export retains the leftover member list (and no other
// runtime state), restoring it into a fresh repository keeps the restore
// marker and lets Disable clean keepalived on the leftover device.
func TestO17FollowupHALeftoverExportRestoreDisableCleansDevice(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	f.client.setBefore(func(_ context.Context, r agents.Request) error {
		u, _ := url.Parse(r.URL)
		if u.Path != "/api/oxidns-ha" {
			return nil
		}
		// Secondary PUT succeeds, primary PUT fails, then rollback DELETE
		// on both also fails so keepalived is left on secondary.
		if r.Method == "PUT" && u.Hostname() == primaryHost {
			return errors.New("synthetic primary put failure")
		}
		if r.Method == "DELETE" {
			return errors.New("synthetic rollback delete failure")
		}
		return nil
	})
	if _, err := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"`+vip+`"}`), "https://panel.example"); err != nil {
		t.Fatal(err)
	}
	last := f.drain(t, 12)
	if last.Status != "failed" {
		t.Fatalf("expected failed enable publication, got %+v", last)
	}
	beforePrimary, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if beforePrimary.OxiDNSHA != nil || beforePrimary.OxiDNSHAState == nil || beforePrimary.OxiDNSHAState.Status != "failed" || !slices.Contains(beforePrimary.OxiDNSHAState.Members, "secondary") {
		t.Fatalf("expected leftover HA state before export, got ha=%+v state=%+v", beforePrimary.OxiDNSHA, beforePrimary.OxiDNSHAState)
	}

	exported, err := f.r.ExportAllJSON(f.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(exported)
	for _, forbidden := range []string{"deployments", "latest_deployment", "oxidns_ha_publication", "synthetic primary put failure", "synthetic rollback delete failure"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("export leaked runtime state %q", forbidden)
		}
	}
	if !strings.Contains(text, `"oxidns_ha_state":{"status":"failed","members":[`) {
		t.Fatalf("export missing minimal HA leftover state: %s", text)
	}

	// Restore into a brand-new repository and verify Disable cleans the device.
	restoredRepo, err := repo.Open(t.TempDir(), repo.Options{DefaultSettings: repo.DefaultConfig})
	if err != nil {
		t.Fatal(err)
	}
	restoredCtx := repo.WithRepository(t.Context(), restoredRepo)
	if _, err := restoredRepo.ImportAllJSON(restoredCtx, exported); err != nil {
		t.Fatal(err)
	}
	restoredMgr, err := agents.New(restoredCtx, restoredRepo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restoredMgr.Close)
	f.client.setBefore(nil)
	restoredMgr.SetClient(f.client)
	restoredSvc := New(restoredMgr, Options{})

	afterPrimary, err := restoredSvc.get(restoredCtx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if afterPrimary.StateRestoredAt == nil {
		t.Fatal("restored primary missing state_restored_at")
	}
	if afterPrimary.OxiDNSHAState == nil || !slices.Contains(afterPrimary.OxiDNSHAState.Members, "secondary") {
		t.Fatalf("restored primary missing leftover members: %+v", afterPrimary.OxiDNSHAState)
	}

	callsBefore := len(f.segment.Calls())
	if _, err := restoredSvc.Disable(restoredCtx, "primary", false); err != nil {
		t.Fatalf("Disable after restore failed: %v", err)
	}
	var deleted []string
	for _, c := range f.segment.Calls()[callsBefore:] {
		if c.Method == "DELETE" && c.Path == "/api/oxidns-ha" {
			deleted = append(deleted, c.AgentID)
		}
	}
	if !slices.Contains(deleted, "secondary") {
		t.Fatalf("Disable after restore did not clean leftover secondary device, DELETE calls = %v", deleted)
	}
	cleanedPrimary, err := restoredSvc.get(restoredCtx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	cleanedSecondary, err := restoredSvc.get(restoredCtx, "secondary")
	if err != nil {
		t.Fatal(err)
	}
	if cleanedPrimary.OxiDNSHAState != nil || cleanedSecondary.OxiDNSHAState != nil {
		t.Fatalf("leftover oxidns_ha_state not cleared after Disable: primary=%+v secondary=%+v", cleanedPrimary.OxiDNSHAState, cleanedSecondary.OxiDNSHAState)
	}
}
