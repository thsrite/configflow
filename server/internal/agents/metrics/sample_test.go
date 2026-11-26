package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"configflow/internal/lifecycle"
)

func TestNativeSampleShutdownAndDisconnect(t *testing.T) {
	history := newTestHistory(t, filepath.Join(t.TempDir(), "metrics"), MaxPointsPerAgent)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	defer rt.Shutdown(context.Background())
	request, disconnect := context.WithCancel(rt.BindRequest(context.Background()))
	disconnect()
	raw := json.RawMessage(`{"cpu":{"usage_percent":12.5,"future":2.00},"memory":null}`)
	if ok, err := history.AddSample(request, "device", raw); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if history.PendingCount() != 1 {
		t.Fatal("sample not appended")
	}
	bound := rt.BindRequest(context.Background())
	rt.Shutdown(context.Background())
	if ok, err := history.AddSample(bound, "device", raw); ok || !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown appended sample", ok, err)
	}
	if history.PendingCount() != 1 {
		t.Fatal("shutdown changed pending points")
	}
}
