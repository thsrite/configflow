package health

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/repo"
)

func BenchmarkPolledLatencyView(b *testing.B) {
	db, err := repo.Open(b.TempDir(), repo.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(db.Close)
	db.SetViewGuard(nil)
	data := make(map[string]any, 302)
	for i := range 302 {
		data[fmt.Sprintf("node-%d", i)] = map[string]any{"method": Method, "latency": i}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		b.Fatal(err)
	}
	if _, err = db.WriteSharedText(b.Context(), LatencyFile, raw); err != nil {
		b.Fatal(err)
	}
	var view LatencyView
	b.ReportAllocs()
	for b.Loop() {
		if _, err := view.JSON(b.Context(), db); err != nil {
			b.Fatal(err)
		}
	}
}
