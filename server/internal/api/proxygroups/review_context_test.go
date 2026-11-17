package proxygroups

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
)

func TestReviewTransactionCallbackKeepsOwnerShutdown(t *testing.T) {
	app := fixture(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	t.Cleanup(func() { runtime.Shutdown(t.Context()) })
	ctx := lifecycle.Detach(runtime.BindRequest(app.Context()))
	before := state(t, app)
	err := edit(ctx, "default", func(tx context.Context, candidate *object) error {
		runtime.Shutdown(t.Context())
		if !errors.Is(tx.Err(), context.Canceled) {
			t.Error("transaction lost owner shutdown", tx.Err())
		}
		return set(candidate, "review_should_not_commit", true)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("callback candidate survived shutdown", err)
	}
	if state(t, app) != before {
		t.Fatal("callback committed after owner shutdown")
	}
}
