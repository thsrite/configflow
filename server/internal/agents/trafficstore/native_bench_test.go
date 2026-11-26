package trafficstore

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func BenchmarkNativeDomainReport(b *testing.B) {
	for _, shape := range []string{"2000_hosts", "one_host_2000_routes"} {
		b.Run(shape, func(b *testing.B) {
			items := make([]json.RawMessage, ReportMaxItems)
			for i := range items {
				host, payload := fmt.Sprintf("h%d.example.com", i), ""
				if shape == "one_host_2000_routes" {
					host = "shared.example.com"
					payload = fmt.Sprintf("payload-%d", i)
				}
				items[i] = json.RawMessage(fmt.Sprintf(`{"host":%q,"policy":"P","outlet":"direct","rule":"MATCH","rule_payload":%q,"conns":1}`, host, payload))
			}
			raw, err := json.Marshal(struct {
				Schema int               `json:"schema"`
				Status string            `json:"status"`
				Items  []json.RawMessage `json:"items"`
			}{1, "running", items})
			if err != nil {
				b.Fatal(err)
			}
			report, err := DecodeReport(raw)
			if err != nil || report == nil {
				b.Fatal(err)
			}
			store := NewNative(b.TempDir())
			at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := store.Add(b.Context(), "synthetic", report, &at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
