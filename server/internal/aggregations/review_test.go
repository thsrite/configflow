package aggregations_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"configflow/internal/aggregations"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/refresh"
)

func TestReviewGeneratorKeepsShutdownCancellation(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	r := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
	entered, observed, abort := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(abort) })
	done := make(chan error, 1)
	svc := aggregations.New(aggregations.ServiceOptions{Repo: srv.Repo})
	err := r.Spawn(srv.Context(), "aggregation-review", func(ctx context.Context) {
		_, err := svc.Generate(ctx,
			json.RawMessage(`{"id":"a","name":"Aggregate","subscriptions":["s"]}`),
			json.RawMessage(`{"profile_id":"default","subscriptions":[{"id":"s","name":"Feed","url":"http://127.0.0.1:9/feed"}]}`),
			aggregations.Options{Main: json.RawMessage(`{"proxies":[],"proxy-groups":[]}`), SourcePrepared: true,
				Fetch: func(ctx context.Context, _, _ string) (*refresh.SourceResult, error) {
					close(entered)
					select {
					case <-ctx.Done():
						close(observed)
						return nil, ctx.Err()
					case <-abort:
						return nil, errors.New("test cleanup")
					}
				}})
		done <- err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	r.Shutdown(context.Background())
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("Runtime shutdown did not reach source I/O")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled generation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("generator did not join workers")
	}
}
