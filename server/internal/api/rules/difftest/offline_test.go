package difftest

import (
	"context"
	"encoding/json"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/repo"
	routers "configflow/internal/routers/spec"
)

func TestRouterOfflineMissingCacheMatchesPython(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		err := r.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
			raw, err := json.Marshal(p.RuleLibrary)
			if err != nil {
				return err
			}
			var items []any
			if err := json.Unmarshal(raw, &items); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": "offline", "name": "Offline", "source_type": "url", "url": "http://127.0.0.1:1/forbidden", "behavior": "ipcidr", "format": "text"})
			merged, err := json.Marshal(items)
			if err != nil {
				return err
			}
			return p.SetJSON("rule_library", merged)
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
			return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"selected","itemType":"ruleset","library_rule_id":"offline","policy":"PROXY","enabled":true}]`))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	previous := routers.RulesetContent
	t.Cleanup(func() { routers.RulesetContent = previous })
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	apirules.Wire()
	reply := w.call("DESIRED", "other", `{"gateway":"192.0.2.1","rule_ids":["selected"]}`, false)
	warnings := reply.Body.(map[string]any)["blocking_warnings"].([]any)
	if len(warnings) != 1 || warnings[0] != "规则集「Offline」没有本地缓存，未计入其中的 IP 段" {
		t.Fatal("missing cache must produce Python's incomplete-plan diagnostic", warnings)
	}
}
