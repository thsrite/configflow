package rules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	routers "configflow/internal/routers/spec"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/rulesource"
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
	previousClient := rulefetch.DefaultClient
	rulefetch.DefaultClient = &rulefetch.Client{Transport: transport}
	t.Cleanup(func() { rulefetch.DefaultClient = previousClient })
	rs := rulesource.New(rulesource.Options{})
	readOffline := func(ctx context.Context, profile json.RawMessage, item routers.RuleSet, library *routers.LibraryRule) (string, error) {
		var libRaw json.RawMessage
		if library != nil {
			libRaw = library.Raw
		}
		return rs.ReadOfflineJSON(ctx, profile, item.Raw, libRaw)
	}
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
			value, err := readOffline(s.Context(), configRaw, item, lib)
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
			value, err = readOffline(s.Context(), configRaw, item, lib)
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
	rs := rulesource.New(rulesource.Options{})
	config := map[string]any{"profile_id": "other", "rule_configs": []any{map[string]any{"id": "remote", "name": "Remote", "itemType": "ruleset", "library_rule_id": "offline", "policy": "PROXY"}}, "rule_library": []any{map[string]any{"id": "offline", "name": "Remote", "source_type": "url", "url": "http://127.0.0.1:1/forbidden", "behavior": "ipcidr", "format": "text"}}}
	raw, _ := json.Marshal(config)
	ctx := repo.WithScope(s.Context())
	loader := func(item routers.RuleSet, library *routers.LibraryRule) (string, error) {
		var libRaw json.RawMessage
		if library != nil {
			libRaw = library.Raw
		}
		return rs.ReadOfflineJSON(ctx, raw, item.Raw, libRaw)
	}
	plan, err := routers.ComputeDesired(ctx, raw, "192.0.2.1", []string{"remote"}, loader)
	if err != nil || plan == nil || len(plan.BlockingWarnings) != 1 || !strings.Contains(plan.BlockingWarnings[0], "没有本地缓存") {
		t.Fatalf("missing cache should match Python's incomplete plan: %+v %v", plan, err)
	}
}
