package refresh

// Regression tests of the adversarial review of C15 (PROGRESS-config.md,
// "Review C15").

import (
	"context"
	"testing"

	"configflow/internal/model"
	"configflow/internal/substore"
)

// A request whose client went away (cancelled context) must still store the
// download, the nodes, the fetch history and the task receipt: Python's
// request thread is never interrupted. Repository locks taken with a
// cancelled context fail at random, so the run is repeated.
func TestReviewCancelledRequestContextFinishesTheRefresh(t *testing.T) {
	f := setup(t, 86400)
	f.download(proxiesYAML(t), "direct_yaml", nil)
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		if _, _, _, err := RefreshSource(ctx, f.sub(), "manual"); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if got := mapInt(f.status("subscription:s"), "run_count"); got != int64(i+1) {
			t.Fatalf("round %d: run_count %d", i, got)
		}
		health, err := ReadHealth(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		entry := health.Entry("s")
		if entry == nil || len(entry.History) != min(i+1, HistorySize) {
			t.Fatalf("round %d: history %+v", i, entry)
		}
	}
	if c := f.cache(); c == nil || c.Count == nil || int64(*c.Count) != 1 {
		t.Fatalf("unexpected cache: %+v", c)
	}
	// A delivery served from the transport cache under a cancelled request
	// context, and the fetch path that writes it.
	f.updateShared(func(shared *model.Shared) {
		shared.Subscriptions[0].URL = subURL + "&moved=1"
	})
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		result, err := FetchSource(ctx, f.sub(), func(context.Context, string, string) (*SourceResult, error) {
			return &SourceResult{Text: proxiesYAML(t), Source: "direct_yaml"}, nil
		})
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if want := map[bool]string{true: "direct_yaml", false: "source_cache"}[i == 0]; result.Source != want {
			t.Fatalf("round %d: source %s, want %s", i, result.Source, want)
		}
	}
}

// StoredRule/LookupResource use native model lists rather than Python iteration.
// Public found/missing behavior and declaration changes remain covered through
// repository-backed tests; malformed collections follow the shared R5 codec.
func TestReviewTypedLookupUsesCurrentDeclaration(t *testing.T) {
	f := setup(t, 86400)
	found, err := LookupResource(f.ctx, "subscription", "s")
	if err != nil || found.Subscription == nil || found.Subscription.ID != "s" {
		t.Fatal(found, err)
	}
	if _, err := LookupResource(f.ctx, "subscription", "missing"); err == nil {
		t.Fatal("missing source was found")
	}
	current := ruleDecl(t)
	stored, err := StoredRule(f.ctx, current)
	if err != nil || !stored {
		t.Fatal(stored, err)
	}
	changed := "https://example.com/changed.list"
	current.URL = &changed
	if stored, err := StoredRule(f.ctx, current); err != nil || stored {
		t.Fatal("changed declaration claimed stored cache", err)
	}
}

func TestN89FetchWithoutCausesRetainsBusinessDiagnostic(t *testing.T) {
	if got := FailureReason(&substore.SubscriptionFetchError{}); got != msgSubscriptionFetchUnknown {
		t.Fatal(got)
	}
}
