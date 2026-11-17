package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
	"configflow/internal/substore"
)

type timedResponse struct {
	response *httpx.Response
	err      error
}

func TestProductionProviderDeadlineKeepsTheSameRender(t *testing.T) {
	for _, mode := range []string{"missing", "cached", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			f := newServeFixture(t, ResponseSeconds)
			if currentResponseSeconds() != 12 || cap(pool) != 8 {
				t.Fatal("production response limits changed")
			}
			if mode != "missing" {
				content := []byte("proxies: [old]")
				if mode == "unreadable" {
					content = []byte{0xff}
				}
				if _, err := f.srv.Repo.WriteProfileText(f.srv.Context(), "alpha", lastGoodPath("aggregation", "agg-1"), content); err != nil {
					t.Fatal(err)
				}
			}
			clock := lifecycletest.NewClock(time.Now())
			runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock})
			runner.Start(f.srv.Context())
			release, entered := make(chan struct{}), make(chan struct{})
			defer runner.Shutdown(context.Background())
			defer close(release)
			shared := lockx.NewFlight[string, *responseCopy](runner, "provider-deadline")
			var calls atomic.Int32
			key := "one"
			result := shared.DoChan(f.srv.Context(), key, func(ctx context.Context) (*responseCopy, error) {
				calls.Add(1)
				close(entered)
				<-release
				return runFlight(ctx, true, "alpha", "aggregation", "agg-1", yamlResponse("proxies: [new]"))
			})
			<-entered
			done := make(chan timedResponse, 1)
			go func() {
				resp, err := awaitResponse(f.srv.Context(), clock, result, true, "alpha", "aggregation", "agg-1")
				done <- timedResponse{resp, err}
			}()
			waitFor(t, func() bool { return clock.Pending() == 1 })
			clock.Advance(11999 * time.Millisecond)
			select {
			case <-done:
				t.Fatal("response before production12s deadline")
			default:
			}
			clock.Advance(time.Millisecond)
			if mode == "missing" {
				// Processing the fired timer must not complete or start another call.
				waitFor(t, func() bool { return clock.Pending() == 0 })
				select {
				case <-done:
					t.Fatal("missing cache ended wait")
				default:
				}
			} else {
				select {
				case r := <-done:
					if mode == "cached" && (r.err != nil || r.response.Header().Get("X-ConfigFlow-Provider") != "last-good") {
						t.Fatal(r)
					}
					if mode == "unreadable" && r.err == nil {
						t.Fatal("invalid UTF-8 fallback accepted")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("deadline ignored")
				}
			}
			joined := shared.DoChan(f.srv.Context(), key, func(context.Context) (*responseCopy, error) {
				calls.Add(1)
				return nil, errors.New("replacement render")
			})
			release <- struct{}{}
			if mode == "missing" {
				select {
				case r := <-done:
					if r.err != nil || string(r.response.Body()) != "proxies: [new]" {
						t.Fatal(r)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("same render result lost")
				}
			}
			select {
			case r := <-joined:
				if r.Err != nil || !r.Shared || calls.Load() != 1 {
					t.Fatal(r, calls.Load())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("joined result lost")
			}
			wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := runner.WaitIdle(wait); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderWaitCancellationLeavesRenderAndBufferedResultAlive(t *testing.T) {
	f := newServeFixture(t, ResponseSeconds)
	clock := lifecycletest.NewClock(time.Now())
	results := make(chan lockx.Result[*responseCopy], 1)
	ctx, cancel := context.WithCancel(f.srv.Context())
	done := make(chan error, 1)
	go func() {
		_, err := awaitResponse(ctx, clock, results, true, "alpha", "aggregation", "agg-1")
		done <- err
	}()
	waitFor(t, func() bool { return clock.Pending() == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if clock.Pending() != 0 {
		t.Fatal("request timer leaked")
	}
	results <- lockx.Result[*responseCopy]{Value: snapshotResponse(httpx.Bytes(200, "text/yaml", []byte("later")))}
	if r := <-results; string(r.Value.body) != "later" {
		t.Fatal(r)
	}
}

func TestProviderPreparationKeepsRuntimeCancellation(t *testing.T) {
	ctx, _ := repoContext(t)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(ctx)
	defer runner.Shutdown(context.Background())
	entered, done := make(chan struct{}), make(chan error, 1)
	fetch := func(ctx context.Context, _ string, _ string) (*substore.SubscriptionResult, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer SetEnv(&Env{SubscriptionFetch: fetch, NodeID: SequentialNodeIDs(), Converter: nilConverter{}})()
	config := jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://example.com/feed", "enabled", true)})
	main := jsonx.Obj("proxies", []any{}, "proxy-groups", []any{}, "proxy-providers", jsonx.Obj("Feed", jsonx.Obj("url", "/subscriptions/s/proxies", "path", "./providers/Feed.yaml")))
	if err := runner.Spawn(ctx, "provider-prepare", func(work context.Context) {
		_, err := PrepareProviderBundle(work, config, main, BundleOptions{RenderAll: true})
		done <- err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch not reached")
	}
	runner.Shutdown(context.Background())
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled fetch succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prepare stripped job cancellation")
	}
}
