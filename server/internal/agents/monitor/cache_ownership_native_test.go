package monitor

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNativeRankingCacheOwnsPublicRowsAndShares(t *testing.T) {
	ResetRankingsCache()
	t.Cleanup(ResetRankingsCache)
	m := dashboardManager(t, t.TempDir())
	client := transport(t, nil, nil)
	clients := asMap(client.rk()["clients"])
	clients["sample_size"] = 2
	clients["rows"] = []any{map[string]any{"key": "192.0.2.1", "count": 2}}
	blocked := asMap(client.rk()["blocked"])
	blocked["sample_size"] = 1
	blocked["rows"] = []any{map[string]any{"key": "ads.example", "count": 1}}
	blocked["matcher_tags"] = []any{"adguard_match"}
	first, err := ReadOverview(ctx, m, "one", nil)
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
	again, err := ReadOverview(ctx, m, "one", nil)
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
