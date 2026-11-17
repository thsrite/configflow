package rulelibrary

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
)

func TestReviewTransactionCallbacksKeepOwnerShutdown(t *testing.T) {
	for _, kind := range []string{"shared", "system", "shared-profiles"} {
		t.Run(kind, func(t *testing.T) {
			app := fixture(t)
			runtime, ctx, _ := runtimeRequest(t, app.Context())
			ctx = lifecycle.Detach(ctx)
			before := persisted(t, app)
			stop := func(tx context.Context, candidate *record) error {
				runtime.Shutdown(t.Context())
				if !errors.Is(tx.Err(), context.Canceled) {
					t.Error("transaction lost owner shutdown", tx.Err())
				}
				return put(candidate, "review_should_not_commit", true)
			}
			var err error
			switch kind {
			case "shared":
				err = editShared(ctx, stop)
			case "system":
				err = editSystem(ctx, func(candidate *record) error { return stop(ctx, candidate) })
			case "shared-profiles":
				_, err = editWithProfiles(ctx, nil, stop, "http://example.com")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatal("callback candidate survived shutdown", err)
			}
			if after := persisted(t, app); after.Document != before.Document {
				t.Fatal("callback committed after owner shutdown")
			}
		})
	}
}
