package agents_test

// Routes of A04: GET /api/agents/<id>/metrics, /metrics/history,
// /metrics/summary (backend/routes/agents.py 2007–2099; Python has no
// route tests for them, the expectations follow the handlers).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func metricsServer(t *testing.T) (*httpxtest.Server, *agents.Manager, httpxtest.Option) {
	t.Helper()
	reset := func() { metrics.Reset(); agents.ResetRuntime(); agents.ResetHooks() }
	reset()
	t.Cleanup(reset)
	metrics.Wire()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apiagents.Register)})
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", mustRawJSON(t, []any{map[string]any{
			"id": "agent_1", "name": "Edge", "service_type": "mihomo",
			"profile_id": "default", "token": "agent-token-0123456789abcdef", "host": "h", "port": 8080,
		}}))
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s, m, s.Login()
}

func jsonObjectKeys(t testing.TB, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("expected object: %v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func TestMetricsRoutesNeedLoginAndAKnownAgent(t *testing.T) {
	s, _, admin := metricsServer(t)
	for _, path := range []string{"/metrics", "/metrics/history", "/metrics/summary"} {
		s.Get("/api/agents/agent_1" + path).WantStatus(401)
		wantBodyJSON(t, s.Get("/api/agents/missing"+path, admin).WantStatus(404), map[string]any{"message": "Agent not found", "success": false})
	}
}

func TestMetricsRoutesServeTheLatestHistoryAndSummary(t *testing.T) {
	s, m, admin := metricsServer(t)
	// No heartbeat yet: empty metrics, no history, empty summary.
	wantBodyJSON(t, s.Get("/api/agents/agent_1/metrics", admin).WantStatus(200), map[string]any{
		"success": true,
		"data":    map[string]any{"agent_id": "agent_1", "metrics": map[string]any{}, "collected_at": nil},
	})
	wantBodyJSON(t, s.Get("/api/agents/agent_1/metrics/summary", admin).WantStatus(200), map[string]any{
		"success": true,
		"data":    map[string]any{"agent_id": "agent_1", "summary": map[string]any{}, "hours": 1},
	})

	sent := map[string]any{
		"cpu":          map[string]any{"usage_percent": 12.5, "core_count": 4},
		"network":      map[string]any{"bytes_sent": 10, "bytes_recv": 20, "speed_sent": 1, "speed_recv": 2},
		"collected_at": "2026-08-31T00:00:00Z",
	}
	for _, cpu := range []any{12.5, 7} {
		sent["cpu"].(map[string]any)["usage_percent"] = cpu
		if ok, err := m.ObserveHeartbeat(s.Context(), "agent_1", agentModelForTest(t, map[string]any{"system_metrics": cloneMap(t, sent)})); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	wantBodyJSON(t, s.Get("/api/agents/agent_1/metrics", admin).WantStatus(200), map[string]any{
		"success": true,
		"data":    map[string]any{"agent_id": "agent_1", "metrics": sent, "collected_at": "2026-08-31T00:00:00Z"},
	})

	histRes := s.Get("/api/agents/agent_1/metrics/history", admin).WantStatus(200)
	data := mapObj(parseBodyMap(t, histRes), "data")
	if mapInt(data, "hours") != 24 || mapInt(data, "data_points") != 2 || len(mapList(data, "history")) != 2 {
		t.Fatal(data)
	}
	var wire struct {
		Data struct {
			History []json.RawMessage `json:"history"`
		} `json:"data"`
	}
	if err := json.Unmarshal(histRes.Body, &wire); err != nil {
		t.Fatal(err)
	}
	if keys := jsonObjectKeys(t, wire.Data.History[0]); strings.Join(keys, ",") != "cpu,disk,memory,network,timestamp" { // jsonify sorts keys
		t.Fatal(keys)
	}
	first := mapList(data, "history")[0].(map[string]any)
	if !testjson.Equal(first["disk"], map[string]any{}) || !testjson.Equal(first["cpu"], map[string]any{"usage_percent": 12.5, "core_count": 4}) {
		t.Fatal(first)
	}
	for query, hours := range map[string]any{"?hours=abc": 24, "?hours=%206%20": 6, "?hours=%D9%A3": 3, "?hours=1_0": 10,
		"?hours=0": 0, "?hours=-1": -1, "?hours=" + strings.Repeat("9", 4301): 24} {
		nativeHours, status := nativeMetricsQueryHours(t, query, hours)
		response := s.Get("/api/agents/agent_1/metrics/history"+query, admin).WantStatus(status)
		if status == 500 {
			if parseBodyMap(t, response)["success"] != false {
				t.Fatal("overflow query succeeded")
			}
			continue
		}
		hours = nativeHours
		got := mapObj(parseBodyMap(t, response), "data")
		if !testjson.Equal(got["hours"], hours) {
			t.Errorf("%s: hours %v", query, got["hours"])
		}
		if want := 2; hours == 0 || hours == -1 {
			if mapInt(got, "data_points") != 0 {
				t.Errorf("%s: %v", query, got)
			}
		} else if mapInt(got, "data_points") != int64(want) {
			t.Errorf("%s: %v", query, got)
		}
	}
	summary := mapObj(parseBodyMap(t, s.Get("/api/agents/agent_1/metrics/summary?hours=2", admin).WantStatus(200)), "data")
	want := map[string]any{
		"cpu":              map[string]any{"avg": 9.75, "max": 12.5, "min": 7},
		"data_points":      2,
		"disk":             map[string]any{"avg": 0, "max": 0, "min": 0},
		"memory":           map[string]any{"avg": 0, "max": 0, "min": 0},
		"network_recv":     map[string]any{"avg": 2.0, "max": 2, "min": 2},
		"network_sent":     map[string]any{"avg": 1.0, "max": 1, "min": 1},
		"time_range_hours": 2,
	}
	if !testjson.Equal(summary["summary"], want) || mapInt(summary, "hours") != 2 {
		t.Fatal(summary)
	}
}

// Python's timedelta raises OverflowError for hours it cannot hold; the
// routes answer 500 with its text.
func TestMetricsRoutesAnswerOverflowAs500(t *testing.T) {
	s, _, admin := metricsServer(t)
	for hours, message := range map[string]string{
		"10000000000":                 "date value out of range",
		"1000000000000":               "Python int too large to convert to C int",
		"24000000000":                 "days=1000000000; must have magnitude <= 999999999",
		"-10000000000":                "date value out of range",
		"17760000":                    "date value out of range",
		"51539607552":                 "Python int too large to convert to C int",
		"23999999976":                 "date value out of range",
		"1" + strings.Repeat("0", 30): "Python int too large to convert to C int",
	} {
		for _, path := range []string{"/metrics/history", "/metrics/summary"} {
			wantBodyJSON(t, s.Get("/api/agents/agent_1"+path+"?hours="+hours, admin).WantStatus(500),
				map[string]any{"message": message, "success": false})
		}
	}
	// In range: an empty window.
	s.Get("/api/agents/agent_1/metrics/history?hours=17736000", admin).WantStatus(200)
}

func nativeMetricsQueryHours(t *testing.T, query string, old any) (any, int) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_metrics_query_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var rules []struct {
		Original map[string]any `json:"original"`
		SHA      string         `json:"sha256"`
		Native   *int           `json:"native_hours"`
		Status   int            `json:"native_status"`
	}
	if err = json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		encoded, err := json.Marshal(rule.Original)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(encoded)) != rule.SHA {
			t.Fatal("original query row changed")
		}
		if query != rule.Original["query"] {
			continue
		}
		if old != int(rule.Original["python_hours"].(float64)) {
			t.Fatal("original query expectation changed")
		}
		if rule.Native == nil {
			return nil, rule.Status
		}
		return *rule.Native, rule.Status
	}
	return old, 200
}
