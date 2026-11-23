package repo

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

type stubExporter struct {
	sanitizeCalls int
	oxidnsCalls   int
	routerCalls   int
}

func (s *stubExporter) SanitizeExternalDocument(_ context.Context, doc *model.Document, _ model.SystemConfig) (*model.Document, error) {
	s.sanitizeCalls++
	return doc, nil
}

func (s *stubExporter) RedactOxiDNSJSON(raw json.RawMessage) (json.RawMessage, error) {
	s.oxidnsCalls++
	return raw, nil
}

func (s *stubExporter) RedactRoutersJSON(raw json.RawMessage) (json.RawMessage, error) {
	s.routerCalls++
	return raw, nil
}

func TestOptionsExportSanitizerAndFailClosed(t *testing.T) {
	defer SetHooks(SetHooks(Hooks{}))

	// Negative control: nil Export and empty global hooks must fail closed.
	rNil, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rNil.Close)
	_, err = rNil.ExportAllJSON(context.Background(), true)
	wantError(t, err, ErrRepository, "脱敏")

	// Instance Options.Export works with zero global hooks.
	exp := &stubExporter{}
	r, err := Open(t.TempDir(), Options{Export: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if _, err := r.ExportAllJSON(context.Background(), true); err != nil {
		t.Fatalf("ExportAllJSON with Options.Export: %v", err)
	}
	if exp.sanitizeCalls != 1 || exp.oxidnsCalls != 1 || exp.routerCalls != 1 {
		t.Fatalf("unexpected exporter calls: %+v", exp)
	}
}

func TestOptionsRouterJobLockAndFailClosed(t *testing.T) {
	defer SetHooks(SetHooks(Hooks{}))
	ctx := context.Background()

	// Negative control: nil RouterJobLock and empty global hooks fail closed on router connection change.
	rNil, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rNil.Close)
	_, err = rNil.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.1"}]}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rNil.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.2"}]}`))
	})
	wantError(t, err, ErrInUse, msgRouterJobBusy)

	// Instance Options.RouterJobLock acquires and releases without global hooks.
	locked, unlocked := 0, 0
	r, err := Open(t.TempDir(), Options{
		RouterJobLock: func(_ context.Context, _, _ string, _ model.Router) (func(), bool) {
			locked++
			return func() { unlocked++ }, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.1"}]}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.2"}]}`))
	})
	if err != nil {
		t.Fatalf("UpdateProfile with Options.RouterJobLock: %v", err)
	}
	if locked != 1 || unlocked != 1 {
		t.Fatalf("locked=%d unlocked=%d, want 1/1", locked, unlocked)
	}
}

func TestOnConfigurationCommittedObserver(t *testing.T) {
	defer SetHooks(SetHooks(Hooks{}))
	r, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	calls := 0
	cancel := r.OnConfigurationCommitted(func(_ *Repository, _, _ *model.Document) { calls++ })
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("backup", []byte(`{"auto_backup":true}`))
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	cancel()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("backup", []byte(`{"auto_backup":true,"webdav_path":"/b"}`))
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls after cancel=%d, want 1", calls)
	}
}
