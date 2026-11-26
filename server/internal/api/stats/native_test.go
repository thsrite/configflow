package stats

import (
	"bytes"
	"configflow/internal/httpx"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeOverviewOverflowIsMissingAndPreservesSamples(t *testing.T) {
	repository, ctx := statsRepository(t)
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
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
	app.Repo = repository
	h := NewHandler(repository)
	h.Now = func() time.Time { return clock }
	h.Register(app.Router)
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
