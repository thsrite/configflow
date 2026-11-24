package rulesync

import (
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// BenchmarkBatch9SyncSources uses the same source-planning boundary on
// the previous batch and this integration tree, including unrelated raw data.
func BenchmarkBatch9SyncSources(b *testing.B) {
	for _, count := range []int{1, 16, 64} {
		for _, padding := range []int{0, 1 << 20} {
			b.Run(fmt.Sprintf("rules%d/padding%d", count, padding), func(b *testing.B) {
				db, err := repo.OpenDefault(b.TempDir())
				if err != nil {
					b.Fatal(err)
				}
				ctx := repo.WithRepository(context.Background(), db)
				if err := db.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
					s.Subscriptions = append(s.Subscriptions, model.Subscription{ID: "padding", Name: strings.Repeat("x", padding)})
					return nil
				}); err != nil {
					b.Fatal(err)
				}
				selected, library := []map[string]any{}, []map[string]any{}
				router := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", IPRuleSets: []string{}}}
				for i := 0; i < count; i++ {
					id, source := fmt.Sprintf("r%d", i), fmt.Sprintf("lib%d", i)
					router.IPRuleSets = append(router.IPRuleSets, id)
					selected = append(selected, map[string]any{"id": id, "itemType": "ruleset", "library_rule_id": source, "policy": "PROXY", "enabled": true})
					library = append(library, map[string]any{"id": source, "name": source, "source_type": "content", "content": "IP-CIDR,203.0.113.0/24", "behavior": "classical", "format": "text", "enabled": true})
				}
				config, err := json.Marshal(map[string]any{"rule_configs": selected, "rule_library": library, "system_config": map[string]any{}, "mihomo": map[string]string{"custom_config": strings.Repeat("x", padding)}})
				if err != nil {
					b.Fatal(err)
				}
				if _, records, err := loadSyncSources(ctx, db, config, router, nil); err != nil || len(records) != count {
					b.Fatalf("source fixture: %d %v", len(records), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, records, err := loadSyncSources(ctx, db, config, router, nil); err != nil || len(records) != count {
						b.Fatalf("source plan: %d %v", len(records), err)
					}
				}
			})
		}
	}
}
