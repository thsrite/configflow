package difftest

import (
	"context"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/routers"
)

func TestRouterOfflineMissingCacheMatchesPython(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("rule_library", append(p.GetList("rule_library"), jsonx.Obj("id", "offline", "name", "Offline", "source_type", "url", "url", "http://127.0.0.1:1/forbidden", "behavior", "ipcidr", "format", "text")))
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("rule_configs", []any{jsonx.Obj("id", "selected", "itemType", "ruleset", "library_rule_id", "offline", "policy", "PROXY", "enabled", true)})
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	previous := routers.RulesetContent
	t.Cleanup(func() { routers.RulesetContent = previous })
	t.Cleanup(gateway.SetRulesetContent(nil))
	apirules.Wire()
	reply := w.call("DESIRED", "other", `{"gateway":"192.0.2.1","rule_ids":["selected"]}`, false)
	warnings := reply.Body.(map[string]any)["blocking_warnings"].([]any)
	if len(warnings) != 1 || warnings[0] != "规则集「Offline」没有本地缓存，未计入其中的 IP 段" {
		t.Fatal("missing cache must produce Python's incomplete-plan diagnostic", warnings)
	}
}
