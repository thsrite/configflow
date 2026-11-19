package provider

// Regression tests of the adversarial review of C17 (PROGRESS-config.md,
// section C17 "Review C17"). Python references: testdata/review_c17_ref.py.

import (
	"context"
	"errors"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/substore"
)

// PORTING rule 4b: a cancelled request context must not abort the shared
// subscription fetch, the bundle validation or the commit (Python's request
// thread finishes them).
func TestReviewBundleAndCommitFinishWhenTheRequestIsCancelled(t *testing.T) {
	base, r := repoContext(t)
	ctx, cancel := context.WithCancel(base)
	cancel()
	content := "proxies:\n- {name: Remote, type: http, server: example.test, port: 80}\n"
	var sawCancelled bool
	fetch := func(ctx context.Context, subID, url string) (*substore.SubscriptionResult, error) {
		if ctx.Err() != nil {
			sawCancelled = true
			return nil, ctx.Err()
		}
		return &substore.SubscriptionResult{Text: content, Source: "direct_yaml", FetchedAt: corpusNowISO}, nil
	}
	defer SetEnv(&Env{SubscriptionFetch: fetch, NodeID: SequentialNodeIDs(), Converter: nilConverter{}})()
	config := jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.test/s", "enabled", true)})
	main := jsonx.Obj("proxies", []any{}, "proxy-groups", []any{},
		"proxy-providers", jsonx.Obj("Feed", jsonx.Obj("url", "/subscriptions/s/proxies", "path", "./providers/Feed.yaml")))
	bundle, err := PrepareProviderBundle(ctx, config, main, BundleOptions{RenderAll: true})
	if err != nil || sawCancelled {
		t.Fatalf("bundle: %v (fetch saw a cancelled context: %v)", err, sawCancelled)
	}
	if len(bundle) != 1 || len(bundle[0].CacheUpdates) != 1 {
		t.Fatalf("bundle %+v", bundle)
	}
	bundle[0].AggregationID = "agg"
	if err := CommitProviderBundle(ctx, "default", bundle); err != nil {
		t.Fatal(err)
	}
	if cache, _ := substore.LoadSubscriptionCache(base, "s"); cache == nil {
		t.Fatal("node cache not committed")
	}
	path, _ := r.ProfilePath("default", "providers/agg.yaml")
	if data, err := os.ReadFile(path); err != nil || string(data) != bundle[0].Content {
		t.Fatalf("artifact %q %v", data, err)
	}
}

// Validate on a hand-made main whose proxies are not dicts: CPython 3.11's
// subscript texts (review_c17_ref.py).
func TestReviewValidateSubscriptTexts(t *testing.T) {
	proxies := []any{jsonx.Obj("name", "A", "type", "http", "server", "a.test", "port", jsonx.Int(80))}
	cases := []struct {
		main  *jsonx.Object
		name  *string
		class string
		text  string
	}{
		{jsonx.Obj("proxies", []any{}, "proxy-providers", jsonx.Obj("Feed", jsonx.Int(1))), ProviderName("Feed"),
			"AttributeError", "'int' object has no attribute 'get'"},
		{jsonx.Obj("proxies", []any{"x"}, "proxy-providers", jsonx.NewObject()), nil,
			"TypeError", "string indices must be integers, not 'str'"},
		{jsonx.Obj("proxies", []any{[]any{"x"}}, "proxy-providers", jsonx.NewObject()), nil,
			"TypeError", "list indices must be integers or slices, not str"},
	}
	for i, c := range cases {
		snap, err := Capture("default", c.main, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = snap.Validate(jsonx.DeepCopy(proxies), false, c.name)
		var input *InputError
		field := "proxies"
		if i == 0 {
			field = "proxy-providers"
		}
		if !errors.As(err, &input) || input.Field != field {
			t.Errorf("case %d: %v (%s)", i, err, errClass(err))
		}
	}
}
