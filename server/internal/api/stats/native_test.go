package stats

import (
	"bytes"
	"configflow/internal/httpx"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeHistoryInterchangeAndExactCounters(t *testing.T) {
	repository, ctx := statsRepository(t)
	previous := Now
	t.Cleanup(func() { Now = previous })
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return clock }
	if _, err := recordHistory(ctx, Counts{Nodes: 2}, true); err != nil {
		t.Fatal(err)
	}
	path, err := repository.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	script := `import json,sys
p=sys.argv[1];v=json.load(open(p));assert v['samples'][0]['nodes']==2
v['samples'][0]['nodes']=float('nan');v['samples'][0]['rules']=9007199254740993123456789;v['future']={'keep':1}
json.dump(v,open(p,'w'))`
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", script, path).CombinedOutput(); err != nil {
		t.Fatalf("Python: %s %v", out, err)
	}
	clock = clock.Add(SampleInterval)
	history, err := recordHistory(ctx, Counts{Nodes: 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if *history["nodes"][0] != "0" || *history["rules"][0] != "9007199254740993123456789" || *history["nodes"][1] != "3" {
		t.Fatal(history)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("nonfinite output", string(raw))
	}
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", `import json,sys
v=json.load(open(sys.argv[1]));assert v['samples'][0]['nodes'] is None;assert v['samples'][0]['rules']==9007199254740993123456789;assert v['future']=={'keep':1}`, path).CombinedOutput(); err != nil {
		t.Fatalf("Python reread: %s %v", out, err)
	}
}

func TestNativeOverviewOverflowIsMissingAndPreservesSamples(t *testing.T) {
	repository, ctx := statsRepository(t)
	previous := Now
	t.Cleanup(func() { Now = previous })
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return clock }
	path, err := repository.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"samples":[{"at":"2026-11-18T10:00:00Z","nodes":1e400,"rules":` + strings.Repeat("9", 4301) + `,"proxyGroups":-1e400,"subscriptions":1e-400}]}`)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	app := httpx.New()
	Register(app.Router)
	result, err := app.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: "/api/stats/overview"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 200 || !json.Valid(result.Body) || bytes.Contains(result.Body, []byte("Infinity")) || bytes.Contains(result.Body, []byte("NaN")) {
		t.Fatalf("overflow response: %d %s", result.Status, result.Body)
	}
	var response struct {
		Data map[string]struct {
			History []json.RawMessage `json:"history"`
		} `json:"data"`
	}
	if err = json.Unmarshal(result.Body, &response); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"nodes", "rules", "proxyGroups"} {
		if string(response.Data[key].History[0]) != "null" {
			t.Fatalf("%s overflow was not missing: %s", key, result.Body)
		}
	}
	if string(response.Data["subscriptions"].History[0]) != "0" {
		t.Fatal("small integer count did not truncate normally")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Samples []json.RawMessage `json:"samples"`
	}
	if err = json.Unmarshal(after, &stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored.Samples[0], []byte(`"nodes":1e400`)) || !bytes.Contains(stored.Samples[0], []byte(strings.Repeat("9", 4301))) || !bytes.Contains(stored.Samples[0], []byte(`"subscriptions":1e-400`)) {
		t.Fatal("original numeric literals changed")
	}
}
