package ippresence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
)

func TestNativeFormatErrorsRemainUnknown(t *testing.T) {
	ctx, r, path, _, library := listRepo(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("192.0.2.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var entry model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"lib-tiktok","name":"TikTok","itemType":"ruleset","source_type":"url","url":"https://rules.test/TikTok.list","behavior":"classical","format":"text"}`), &entry); err != nil {
		t.Fatal(err)
	}
	var badID model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"lib-tiktok","name":"TikTok","itemType":"ruleset","library_rule_id":1,"source_type":"url","url":"https://rules.test/TikTok.list","behavior":"classical","format":"text"}`), &badID); err != nil {
		t.Fatal(err)
	}
	var badURL model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"lib-tiktok","name":"TikTok","itemType":"ruleset","source_type":"url","url":5,"behavior":"classical"}`), &badURL); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		item    model.RuleConfig
		library []model.RuleLibraryItem
	}{
		{"invalid reference", badID, library},
		{"invalid URL", badURL, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ClearMemo()
			value, err := TypedClassicalIPLines(ctx, r, tt.item, tt.library)
			if value != nil || err != nil {
				t.Fatalf("want unknown without error, got %v / %v", value, err)
			}
		})
	}
	ClearMemo()
	if value, err := TypedClassicalIPLines(ctx, r, entry, nil); value == nil || !*value || err != nil {
		t.Fatalf("valid cache no longer inspected: %v / %v", value, err)
	}
	mrs := library[0]
	mrsFmt := "mrs"
	mrs.Format = &mrsFmt
	if value, err := TypedClassicalIPLines(ctx, r, entry, []model.RuleLibraryItem{mrs}); value != nil || err != nil {
		t.Fatalf("selected declaration format lost: %v / %v", value, err)
	}
}
