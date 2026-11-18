package ippresence

import (
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/jsonx"
)

func TestNativeFormatErrorsRemainUnknown(t *testing.T) {
	ctx, path, _, config := listRepo(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("192.0.2.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := config.GetList("rule_library")[0].(*jsonx.Object).DeepCopy()
	entry.Set("itemType", "ruleset")
	badID := entry.DeepCopy()
	badID.Set("library_rule_id", 1)
	badURL := entry.DeepCopy()
	badURL.Delete("format")
	badURL.Set("url", 5)
	for _, tt := range []struct {
		name         string
		item, config *jsonx.Object
	}{
		{"null library", entry, jsonx.Obj("rule_library", nil)},
		{"scalar library", entry, jsonx.Obj("rule_library", "ab")},
		{"invalid reference", badID, config},
		{"invalid URL", badURL, jsonx.Obj("rule_library", []any{})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ClearMemo()
			value, err := ClassicalIPLines(ctx, tt.item, tt.config)
			if value != nil || err != nil {
				t.Fatalf("want unknown without error, got %v / %v", value, err)
			}
		})
	}
	ClearMemo()
	for _, library := range []any{[]any{}, "", jsonx.NewObject(), []any{entry, nil}} {
		if value, err := ClassicalIPLines(ctx, entry, jsonx.Obj("rule_library", library)); value != true || err != nil {
			t.Fatalf("valid cache no longer inspected: %v / %v", value, err)
		}
	}
	mrs := entry.DeepCopy()
	mrs.Set("format", "mrs")
	if value, err := ClassicalIPLines(ctx, entry, jsonx.Obj("rule_library", []any{mrs, nil})); value != nil || err != nil {
		t.Fatalf("selected declaration format lost: %v / %v", value, err)
	}
}
