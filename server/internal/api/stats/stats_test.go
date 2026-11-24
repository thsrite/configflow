package stats

import (
	"configflow/internal/timefmt"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func overviewOf(t *testing.T, app *httpx.App, repository *repo.Repository, header string) (int, map[string]any) {
	t.Helper()
	request := httpx.InvokeRequest{Method: "GET", Path: "/api/stats/overview"}
	if header != "" {
		request.Header = map[string][]string{"X-ConfigFlow-Profile": {header}}
	}
	res, err := app.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(res.Body, &value); err != nil {
		t.Fatalf("%v %s", err, res.Body)
	}
	return res.Status, value
}

func TestOverviewCountsAndHistorySampling(t *testing.T) {
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repository.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", json.RawMessage(`[{"id":"s1","name":"S","url":"https://sub.test/x"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	app := httpx.New()
	app.Repo = repository
	clock := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	h := NewHandler(repository)
	h.Now = func() time.Time { return clock }
	h.Register(app.Router)

	status, body := overviewOf(t, app, repository, "")
	data := body["data"].(map[string]any)
	subs := data["subscriptions"].(map[string]any)
	rules := data["rules"].(map[string]any)
	if status != 200 || subs["total"].(float64) != 1 ||
		!testjson.Equal([]any{1}, subs["history"]) ||
		!testjson.Equal([]any{0}, rules["history"]) {
		t.Fatalf("%#v", body)
	}
	profile := data["profile"].(map[string]any)
	if _, ok := profile["updated_at"].(string); !ok || profile["revision"].(float64) < 1 {
		t.Fatalf("%#v", profile)
	}
	historyPath, err := repository.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	rawSaved, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Samples []struct {
			At string `json:"at"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(rawSaved, &saved); err != nil || len(saved.Samples) != 1 || saved.Samples[0].At != "2026-11-14T12:00:00Z" {
		t.Fatalf("%v %s", err, rawSaved)
	}
	// Within the sampling interval: not saved, the last point shows the current value.
	clock = clock.Add(10 * time.Minute)
	if err := repository.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", json.RawMessage(`[]`))
	}); err != nil {
		t.Fatal(err)
	}
	_, body = overviewOf(t, app, repository, "")
	if !testjson.Equal([]any{0}, body["data"].(map[string]any)["subscriptions"].(map[string]any)["history"]) {
		t.Fatalf("%#v", body)
	}
	rawSaved, _ = os.ReadFile(historyPath)
	saved.Samples = nil
	_ = json.Unmarshal(rawSaved, &saved)
	if len(saved.Samples) != 1 {
		t.Fatal("sampled too early")
	}
	clock = clock.Add(30 * time.Minute)
	_, body = overviewOf(t, app, repository, "")
	if !testjson.Equal([]any{1, 0}, body["data"].(map[string]any)["subscriptions"].(map[string]any)["history"]) {
		t.Fatalf("%#v", body)
	}
	status, body = overviewOf(t, app, repository, "missing")
	if status != 404 || body["message"] != "Profile not found: missing" {
		t.Fatalf("%d %#v", status, body)
	}
}

func TestIsoformatZ(t *testing.T) {
	if got := timefmt.UTCZ(time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)); got != "2026-01-02T03:04:05.123456Z" {
		t.Fatal(got)
	}
	if got := timefmt.UTCZ(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); got != "2026-01-02T03:04:05Z" {
		t.Fatal(got)
	}
}
