package refresh

// Regression tests of the adversarial review of C15 (PROGRESS-config.md,
// "Review C15").

import (
	"context"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/substore"
)

// A request whose client went away (cancelled context) must still store the
// download, the nodes, the fetch history and the task receipt: Python's
// request thread is never interrupted. Repository locks taken with a
// cancelled context fail at random, so the run is repeated.
func TestReviewCancelledRequestContextFinishesTheRefresh(t *testing.T) {
	f := setup(t, jsonx.Int(86400))
	f.download(proxiesYAML(), "direct_yaml", nil)
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		if _, _, _, err := RefreshSubscription(ctx, f.sub(), "manual"); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if got := f.status("subscription:s").GetInt("run_count"); got != int64(i+1) {
			t.Fatalf("round %d: run_count %d", i, got)
		}
		health, err := LoadHealth(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(health.GetObject("s").GetList("history")); n != min(i+1, HistorySize) {
			t.Fatalf("round %d: history %d", i, n)
		}
	}
	if f.cache().GetInt("count") != 1 {
		t.Fatal(jsonx.Repr(f.cache()))
	}
	// A delivery served from the transport cache under a cancelled request
	// context, and the fetch path that writes it.
	f.updateShared(func(shared *jsonx.Object) {
		shared.GetList("subscriptions")[0].(*jsonx.Object).Set("url", subURL+"&moved=1")
	})
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		result, err := FetchSubscriptionSource(ctx, f.sub(), func(context.Context, string, string) (*substore.SubscriptionResult, error) {
			return &substore.SubscriptionResult{Text: proxiesYAML(), Source: "direct_yaml"}, nil
		})
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if want := map[bool]string{true: "direct_yaml", false: "source_cache"}[i == 0]; result.Source != want {
			t.Fatalf("round %d: source %s, want %s", i, result.Source, want)
		}
	}
}

// is_library_source / find_resource use next(): the search stops at the
// first match, so an item that is not an object after it does not fail.
func TestReviewNextStopsAtTheFirstMatch(t *testing.T) {
	match := func(item *jsonx.Object) bool { return item.Value("id") == "a" }
	found, err := nextObject([]any{jsonx.Obj("id", "a"), "junk"}, match)
	if err != nil || found == nil {
		t.Fatal(found, err)
	}
	if _, err := nextObject([]any{"junk", jsonx.Obj("id", "a")}, match); pythonClass(err) != "AttributeError" ||
		err.Error() != "'str' object has no attribute 'get'" {
		t.Fatal(err)
	}
	if found, err := nextObject([]any{jsonx.Obj("id", "b")}, match); found != nil || err != nil {
		t.Fatal(found, err)
	}
	for _, c := range []struct {
		value any
		class string
	}{{nil, "TypeError"}, {"", ""}, {"x", "AttributeError"}, {jsonx.Int(1), "TypeError"}, {jsonx.NewObject(), ""}} {
		_, err := nextObject(c.value, match)
		if (err == nil) != (c.class == "") || (err != nil && pythonClass(err) != c.class) {
			t.Errorf("%v: %v", c.value, err)
		}
	}
}
