package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestExecRunnerUsesDetachedRuntimeGeneration(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
	owner := lifecycle.Detach(request)
	disconnect()
	if owner.Err() != nil {
		t.Fatal("disconnect reached owner")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	finished := make(chan error, 1)
	go func() {
		_, err := (ExecRunner{}).Run(owner, Command{Argv: []string{"/bin/sh", "-c", `printf ready > "$READY"; exec sleep 30`}, Env: append(os.Environ(), "READY="+ready), Timeout: time.Minute})
		finished <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	waiting := true
	for waiting {
		select {
		case <-ticker.C:
			if _, err := os.Stat(ready); err == nil {
				waiting = false
			}
		case err := <-finished:
			t.Fatalf("worker exited before ready: %v", err)
		case <-deadline.C:
			t.Fatal("worker did not start")
		}
	}
	runtime.Shutdown(context.Background())
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not kill/reap worker")
	}
}

func TestConversionPanicsDoNotStrandBatchOrPublishPanicText(t *testing.T) {
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), repository)
	if err := repository.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		for i := 0; i < MaxNodes; i++ {
			shared.Nodes = append(shared.Nodes, model.Node{ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("Synthetic %d", i), Type: "http", Server: "example.test", Port: 80})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	env := testEnv(&fakeRunner{fn: func(Command) (Result, error) {
		t.Error("all conversions failed; worker must not run")
		return Result{}, nil
	}}, nil)
	env.Collaborators.ConvertNode = func(context.Context, json.RawMessage) (model.Fields, bool) { panic("synthetic-private-detail") }
	var saved json.RawMessage
	env.Collaborators.WriteLatencyFile = func(_ context.Context, raw json.RawMessage) error {
		saved = append(json.RawMessage(nil), raw...)
		return nil
	}
	t.Cleanup(SetProbeEnv(env))
	done := make(chan error, 1)
	go func() {
		out, err := RunLatencyTests(ctx, nil, nil)
		if err == nil && out.Results.Len() != MaxNodes {
			err = fmt.Errorf("results=%d", out.Results.Len())
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conversion panic stranded job producer")
	}
	if len(saved) == 0 || strings.Contains(string(saved), "synthetic-private-detail") {
		t.Fatal("panic detail reached stored result")
	}
}

func TestLatencyEntryTypedRoundTripAndExtensionRetention(t *testing.T) {
	const raw = `{"latency":42,"method":"proxy_http_v2","history":[1.0,null],"extra":{"number":1e2,"text":"\\u4e2d"},"dns_servers":"bad"}`
	var entry LatencyEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Latency == nil || entry.Latency.String() != "42" || entry.Method != Method || len(entry.DNSServers) != 0 {
		t.Fatal("typed attributes not decoded")
	}
	before, err := model.Marshal(entry)
	if err != nil || string(before) != raw {
		t.Fatalf("round trip: %s %v", before, err)
	}
	copy := entry
	if err := copy.set("tested_at", json.RawMessage(`"2030-01-01T00:00:00Z"`)); err != nil {
		t.Fatal(err)
	}
	after, err := model.Marshal(copy)
	if err != nil || !strings.Contains(string(after), `"dns_servers":"bad"`) || !strings.Contains(string(after), `"number":1e2`) {
		t.Fatalf("lost stored bytes: %s %v", after, err)
	}
	unchanged, _ := model.Marshal(entry)
	if string(unchanged) != raw {
		t.Fatal("copy mutation changed source")
	}
}

func FuzzHealthWorkerOutputs(f *testing.F) {
	for _, seed := range []string{`{"version":4,"results":[{"name":"synthetic","latency":1}]}`, `{"version":"4"}`, `{"version":4,"dns_check":{"success":true,"addresses":["192.0.2.1"]}}`, `{"version":NaN}`, `[]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 32768 {
			return
		}
		_, _ = parseCheckResult(raw)
		var proxy model.Fields
		_ = proxy.Set("name", json.RawMessage(`"synthetic"`))
		_, _ = parseProbeResults(raw, []model.Fields{proxy})
	})
}

func TestDefaultCollaboratorsInstallEveryNativeBoundary(t *testing.T) {
	c := defaultCollaborators(nil)
	if c.NeedsRemoteConversion == nil || c.ConvertNode == nil || c.LoadSubscriptionCache == nil || c.FilterSubscriptionNodes == nil || c.ReadLatencyFile == nil || c.WriteLatencyFile == nil {
		t.Fatal("incomplete native collaborator wiring")
	}
}

func TestWorkerFailureRequiresTextAndRetainsIntegerPrecision(t *testing.T) {
	var proxy model.Fields
	_ = proxy.Set("name", json.RawMessage(`"synthetic"`))
	for _, failure := range []string{`true`, `{"message":"failed"}`, `["failed"]`, `0`, `null`, `""`} {
		_, err := parseProbeResults(`{"version":4,"results":[{"name":"synthetic","latency":null,"error":`+failure+`}]}`, []model.Fields{proxy})
		if err == nil {
			t.Fatalf("non-text worker error accepted: %s", failure)
		}
	}
	results, err := parseProbeResults(`{"version":4,"results":[{"name":"synthetic","latency":18446744073709551616}]}`, []model.Fields{proxy})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := results.Get("synthetic")
	if !ok || entry.Latency == nil || entry.Latency.String() != "18446744073709551616" {
		t.Fatal("worker integer rounded")
	}
}
