package rules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/testjson"
)

type refusingTransport struct{ calls atomic.Int32 }

func (t *refusingTransport) Do(context.Context, rulefetch.TransportRequest) (*rulefetch.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("all outbound requests forbidden")
}
func TestRouterOfflineSourceOrderAndZeroNetwork(t *testing.T) {
	s := fixture(t)
	transport := &refusingTransport{}
	previousClient, previousLoader := rulefetch.DefaultClient, routers.RulesetContent
	rulefetch.DefaultClient = &rulefetch.Client{Transport: transport}
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	Wire()
	t.Cleanup(func() { rulefetch.DefaultClient = previousClient; routers.RulesetContent = previousLoader })
	// The same source carries an URL and inline text: offline still stops before
	// either URL or mirror fallback, and existing bytes win over inline content.
	for _, sourceType := range []string{"content", "url"} {
		t.Run(sourceType, func(t *testing.T) {
			source := map[string]any{"id": "offline", "name": "Offline", "source_type": sourceType, "url": "http://127.0.0.1:1/forbidden", "behavior": "domain", "format": "text", "content": "+.inline.example\n"}
			config := map[string]any{"profile_id": "other", "rule_library": []any{source}}
			configRaw, _ := json.Marshal(config)
			itemRaw := json.RawMessage(`{"id":"selected","itemType":"ruleset","library_rule_id":"offline"}`)
			libRaw, _ := json.Marshal(source)
			item := routers.RuleSet{Raw: itemRaw}
			lib := &routers.LibraryRule{Raw: libRaw}
			value, err := routers.RulesetContent(s.Context(), configRaw, item, lib)
			want := ""
			if sourceType == "content" {
				want = "+.inline.example\n"
			}
			if err != nil || value != want {
				t.Fatalf("offline missing cache: %q %v", value, err)
			}
			source["id"] = "cached-" + sourceType
			libRaw, _ = json.Marshal(source)
			lib.Raw = libRaw
			configRaw, _ = json.Marshal(config)
			// Seed an existing cache from an offline/legacy snapshot. This test
			// exercises read precedence, not permission to publish an unregistered
			// declaration through the guarded current-library writer.
			if _, err = s.Repo.WriteSharedText(s.Context(), "rules/"+shared.CacheFilename(testjson.Decode[model.RuleConfig](t, source)), []byte("+.cached.example\n")); err != nil {
				t.Fatal(err)
			}
			value, err = routers.RulesetContent(s.Context(), configRaw, item, lib)
			if err != nil || value != "+.cached.example\n" {
				t.Fatalf("cache precedence: %q %v", value, err)
			}
		})
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("offline source issued %d outbound calls", transport.calls.Load())
	}
}
func TestRouterMissingCacheIsAnIncompletePlan(t *testing.T) {
	s := fixture(t)
	previousLoader := routers.RulesetContent
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	Wire()
	t.Cleanup(func() { routers.RulesetContent = previousLoader })
	config := map[string]any{"profile_id": "other", "rule_configs": []any{map[string]any{"id": "remote", "name": "Remote", "itemType": "ruleset", "library_rule_id": "offline", "policy": "PROXY"}}, "rule_library": []any{map[string]any{"id": "offline", "name": "Remote", "source_type": "url", "url": "http://127.0.0.1:1/forbidden", "behavior": "ipcidr", "format": "text"}}}
	raw, _ := json.Marshal(config)
	plan, err := routers.ComputeDesired(repo.WithScope(s.Context()), raw, "192.0.2.1", []string{"remote"}, nil)
	if err != nil || plan == nil || len(plan.BlockingWarnings) != 1 || !strings.Contains(plan.BlockingWarnings[0], "没有本地缓存") {
		t.Fatalf("missing cache should match Python's incomplete plan: %+v %v", plan, err)
	}
}
