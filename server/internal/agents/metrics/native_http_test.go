package metrics_test

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

func TestNativeHugeMetricHTTPResponsesRemainValid(t *testing.T) {
	dir := t.TempDir()
	history, err := metrics.NewHistory(t.Context(), dir, metrics.MaxPointsPerAgent)
	if err != nil {
		t.Fatal(err)
	}
	// Each value is valid JSON even when float64 arithmetic overflows/underflows.
	for i, number := range []string{"1e400", "1e-400", strings.Repeat("9", 4301)} {
		id := []string{"overflow", "underflow", "integer"}[i]
		raw := `{"timestamp":"` + time.Now().Format("2006-01-02T15:04:05") + `","cpu":{"usage_percent":` + number + `},"network":{"bytes_sent":` + number + `,"bytes_recv":0}}` + "\n"
		path := filepath.Join(dir, id+metrics.FileSuffix)
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := history.Load(t.Context(), []string{"overflow", "underflow", "integer"}); err != nil {
			t.Fatal(err)
		}
		srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
			r.GET("/metrics/summary", "test.summary", func(c *httpx.Ctx) (*httpx.Response, error) {
				v, e := history.Summarize(id, big.NewInt(1))
				return httpx.JSON(v), e
			})
			r.GET("/metrics/traffic", "test.traffic", func(c *httpx.Ctx) (*httpx.Response, error) {
				v, e := history.TrafficStatistics(id, "total")
				return httpx.JSON(v), e
			})
			r.GET("/metrics/trend", "test.trend", func(c *httpx.Ctx) (*httpx.Response, error) {
				v, e := history.TrafficTrend(id, big.NewInt(24), big.NewInt(5))
				return httpx.JSON(v), e
			})
		})})
		for _, endpoint := range []string{"summary", "traffic", "trend"} {
			result := srv.Get("/metrics/" + endpoint).WantStatus(200)
			var finite any
			if json.Unmarshal([]byte(result.Text()), &finite) != nil || !json.Valid([]byte(result.Text())) {
				t.Fatalf("invalid HTTP JSON: %s", result.Text())
			}
		}
		after, _ := os.ReadFile(path)
		if string(after) != raw {
			t.Fatal("derived calculation changed original number")
		}
	}
}
