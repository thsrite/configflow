package monitor

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
)

func TestNativeRankingCacheOwnsPublicRowsAndShares(t *testing.T) {
	ResetRankingsCache()
	t.Cleanup(ResetRankingsCache)
	m := dashboardManager(t, t.TempDir())
	client := transport(t, nil, nil)
	clients := client.rk().GetObject("clients")
	clients.Set("sample_size", 2)
	clients.Set("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 2)})
	blocked := client.rk().GetObject("blocked")
	blocked.Set("sample_size", 1)
	blocked.Set("rows", []any{jsonx.Obj("key", "ads.example", "count", 1)})
	blocked.Set("matcher_tags", []any{"adguard_match"})
	first, err := ReadOverview(ctx, registryFor(m), "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := json.Marshal(first.Rankings)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rankings.Clients.Rows) == 0 {
		t.Fatal("fixture has no client ranking")
	}
	first.Rankings.Clients.Rows[0].Count = 999
	if first.Rankings.Blocked.MatcherTag != nil {
		*first.Rankings.Blocked.MatcherTag = "changed"
	}
	first.Rankings.Blocked.MatcherTags[0] = "changed"
	if err := first.Rankings.Clients.Rows[0].Share.UnmarshalJSON([]byte(`999.0`)); err != nil {
		t.Fatal(err)
	}
	if len(first.Rankings.RetentionDays) > 0 {
		first.Rankings.RetentionDays[0] = 'x'
	}
	if first.Rankings.Blocked != nil && first.Rankings.Blocked.Section != nil && len(first.Rankings.Blocked.Section.Rows) > 0 {
		first.Rankings.Blocked.Section.Rows[0].Key = "changed.example"
	}
	again, err := ReadOverview(ctx, registryFor(m), "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(again.Rankings)
	if err != nil || !bytes.Equal(saved, encoded) {
		t.Fatalf("public mutation changed cached ranking: %s vs %s err=%v", saved, encoded, err)
	}
	if len(client.rankingCalls()) != 1 {
		t.Fatal("ownership check did not hit cached result")
	}
}
