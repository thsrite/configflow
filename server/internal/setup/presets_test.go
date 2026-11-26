package setup

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestSetupBuiltinResolutionIsIdempotentAndKeepsNamesakes(t *testing.T) {
	r, ctx := setupRepository(t)
	c := &catalog{Items: []builtin{
		{"builtin:synthetic", model.RuleLibraryItem{Name: "Synthetic", SourceType: pointer("content"), Content: pointer("+.example.com"), Behavior: pointer("domain"), Format: pointer("text"), Enabled: pointer(true)}, "proxy"},
	}}
	err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("rule_library", json.RawMessage(`[{"id":"existing","name":"Synthetic","source_type":"content","content":"DOMAIN,example.com","behavior":"classical","format":"text"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before := r.CommitCount()
	items, err := c.ensureLibrary(ctx, r, nil)
	if err != nil || len(items) != 0 || r.CommitCount() != before {
		t.Fatal("unselected conflicting template was copied", err)
	}
	items, err = c.ensureLibrary(ctx, r, map[string]bool{"builtin:synthetic": true})
	if err != nil || items["builtin:synthetic"].Name != "Synthetic（内置）" {
		t.Fatal("selected namesake not copied safely", err)
	}
	before = r.CommitCount()
	again, err := c.ensureLibrary(ctx, r, nil)
	if err != nil || r.CommitCount() != before || again["builtin:synthetic"].ID != items["builtin:synthetic"].ID {
		t.Fatal("resolved template was duplicated", err)
	}
}
