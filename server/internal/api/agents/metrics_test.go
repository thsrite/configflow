package agents_test

// Routes of A04: GET /api/agents/<id>/metrics, /metrics/history,
// /metrics/summary (backend/routes/agents.py 2007–2099; Python has no
// route tests for them, the expectations follow the handlers).

import (
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
	"configflow/internal/jsonx"
)

func metricsServer(t *testing.T) (*httpxtest.Server, *agents.Manager, httpxtest.Option) {
	t.Helper()
	reset := func() { metrics.Reset(); agents.ResetRuntime(); agents.ResetHooks() }
	reset()
	t.Cleanup(reset)
	metrics.Wire()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apiagents.Register)})
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "agent_1", "name", "Edge", "service_type", "mihomo",
			"profile_id", "default", "token", "agent-token-0123456789abcdef", "host", "h", "port", 8080)})
		return nil, nil
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

func TestMetricsRoutesNeedLoginAndAKnownAgent(t *testing.T) {
	s, _, admin := metricsServer(t)
	for _, path := range []string{"/metrics", "/metrics/history", "/metrics/summary"} {
		s.Get("/api/agents/agent_1" + path).WantStatus(401)
		s.Get("/api/agents/missing"+path, admin).WantStatus(404).WantJSON(jsonx.Obj("message", "Agent not found", "success", false))
	}
}

func TestMetricsRoutesServeTheLatestHistoryAndSummary(t *testing.T) {
	s, m, admin := metricsServer(t)
	// No heartbeat yet: empty metrics, no history, empty summary.
	s.Get("/api/agents/agent_1/metrics", admin).WantStatus(200).WantJSON(jsonx.Obj("success", true,
		"data", jsonx.Obj("agent_id", "agent_1", "metrics", jsonx.NewObject(), "collected_at", nil)))
	s.Get("/api/agents/agent_1/metrics/summary", admin).WantStatus(200).WantJSON(jsonx.Obj("success", true,
		"data", jsonx.Obj("agent_id", "agent_1", "summary", jsonx.NewObject(), "hours", 1)))

	sent := jsonx.Obj("cpu", jsonx.Obj("usage_percent", 12.5, "core_count", 4),
		"network", jsonx.Obj("bytes_sent", 10, "bytes_recv", 20, "speed_sent", 1, "speed_recv", 2),
		"collected_at", "2026-08-31T00:00:00Z")
	for _, cpu := range []any{12.5, 7} {
		sent.GetObject("cpu").Set("usage_percent", cpu)
		if ok, err := m.ObserveHeartbeat(s.Context(), "agent_1", agentModelForTest(t, jsonx.Obj("system_metrics", sent.DeepCopy()))); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	s.Get("/api/agents/agent_1/metrics", admin).WantStatus(200).WantJSON(jsonx.Obj("success", true,
		"data", jsonx.Obj("agent_id", "agent_1", "metrics", sent, "collected_at", "2026-08-31T00:00:00Z")))

	data := s.Get("/api/agents/agent_1/metrics/history", admin).WantStatus(200).Object().GetObject("data")
	if data.GetInt("hours") != 24 || data.GetInt("data_points") != 2 || len(data.GetList("history")) != 2 {
		t.Fatal(data)
	}
	first := data.GetList("history")[0].(*jsonx.Object)
	if strings.Join(first.Keys(), ",") != "cpu,disk,memory,network,timestamp" { // jsonify sorts keys
		t.Fatal(first.Keys())
	}
	if !jsonx.Equal(first.Value("disk"), jsonx.NewObject()) || !jsonx.Equal(first.Value("cpu"), jsonx.Obj("usage_percent", 12.5, "core_count", 4)) {
		t.Fatal(first)
	}
	for query, hours := range map[string]any{"?hours=abc": 24, "?hours=%206%20": 6, "?hours=%D9%A3": 3, "?hours=1_0": 10,
		"?hours=0": 0, "?hours=-1": -1, "?hours=" + strings.Repeat("9", 4301): 24} {
		nativeHours, status := nativeMetricsQueryHours(t, query, hours)
		response := s.Get("/api/agents/agent_1/metrics/history"+query, admin).WantStatus(status)
		if status == 500 {
			if response.Object().Value("success") != false {
				t.Fatal("overflow query succeeded")
			}
			continue
		}
		hours = nativeHours
		got := response.Object().GetObject("data")
		if !jsonx.Equal(got.Value("hours"), hours) {
			t.Errorf("%s: hours %v", query, got.Value("hours"))
		}
		if want := 2; hours == 0 || hours == -1 {
			if got.GetInt("data_points") != 0 {
				t.Errorf("%s: %v", query, got)
			}
		} else if got.GetInt("data_points") != int64(want) {
			t.Errorf("%s: %v", query, got)
		}
	}
	summary := s.Get("/api/agents/agent_1/metrics/summary?hours=2", admin).WantStatus(200).Object().GetObject("data")
	want := jsonx.Obj("cpu", jsonx.Obj("avg", 9.75, "max", 12.5, "min", 7),
		"data_points", 2, "disk", jsonx.Obj("avg", 0, "max", 0, "min", 0), "memory", jsonx.Obj("avg", 0, "max", 0, "min", 0),
		"network_recv", jsonx.Obj("avg", 2.0, "max", 2, "min", 2), "network_sent", jsonx.Obj("avg", 1.0, "max", 1, "min", 1),
		"time_range_hours", 2)
	if !jsonx.Equal(summary.Value("summary"), want) || summary.GetInt("hours") != 2 {
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
			s.Get("/api/agents/agent_1"+path+"?hours="+hours, admin).WantStatus(500).
				WantJSON(jsonx.Obj("message", message, "success", false))
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
