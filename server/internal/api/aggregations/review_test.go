package aggregations_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	generation "configflow/internal/aggregations"
	"configflow/internal/api/aggregations"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
)

func TestReviewCatalogCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		for _, tc := range []struct{ method, path, body string }{
			{"POST", "/api/aggregations", `{"name":"New","subscriptions":[],"nodes":[]}`},
			{"PUT", "/api/aggregations/a", `{"name":"Changed","subscriptions":[],"nodes":[]}`},
			{"DELETE", "/api/aggregations/a", ``},
			{"POST", "/api/aggregations/reorder", `{"ids":["b","a"]}`},
		} {
			label := "disconnect/"
			if shutdown {
				label = "shutdown/"
			}
			t.Run(label+tc.method+tc.path, func(t *testing.T) {
				srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(aggregations.Register)})
				err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
					if err := s.SetJSON("nodes", json.RawMessage(`[]`)); err != nil {
						return err
					}
					if err := s.SetJSON("subscriptions", json.RawMessage(`[]`)); err != nil {
						return err
					}
					return s.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"a","name":"A","nodes":[],"subscriptions":[]},{"id":"b","name":"B","nodes":[],"subscriptions":[]}]`))
				})
				if err != nil {
					t.Fatal(err)
				}
				before, err := srv.Repo.Head(srv.Context())
				if err != nil {
					t.Fatal(err)
				}
				ctx := srv.Context()
				if shutdown {
					r := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
					contexts := make(chan context.Context, 1)
					if err := r.Spawn(ctx, "capture", func(work context.Context) { contexts <- work }); err != nil {
						t.Fatal(err)
					}
					ctx = <-contexts
					if err := r.WaitIdle(context.Background()); err != nil {
						t.Fatal(err)
					}
					r.Shutdown(context.Background())
				} else {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				reply := httptest.NewRecorder()
				srv.Handler().ServeHTTP(reply, req)
				after, err := srv.Repo.Head(srv.Context())
				if err != nil {
					t.Fatal(err)
				}
				if shutdown {
					if reply.Code == 200 || before != after {
						t.Fatalf("shutdown wrote catalog: status=%d before=%v after=%v", reply.Code, before, after)
					}
				} else if reply.Code != 200 || before == after {
					t.Fatalf("disconnect lost write: status=%d body=%s", reply.Code, reply.Body.String())
				}
			})
		}
	}
}

func TestReviewDeliveryShutdownDoesNotPublish(t *testing.T) {
	for _, suffix := range []string{"preview", "provider"} {
		t.Run(suffix, func(t *testing.T) {
			generation.Wire()
			mihomoenv.OverrideForTest(t, nil, nil, nil)
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(aggregations.Register)})
			err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
				if err := s.SetJSON("nodes", json.RawMessage(`[]`)); err != nil {
					return err
				}
				if err := s.SetJSON("subscriptions", json.RawMessage(`[{"id":"s","name":"Feed","url":"http://127.0.0.1:9/feed"}]`)); err != nil {
					return err
				}
				return s.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"a","name":"Aggregate","nodes":[],"subscriptions":["s"]}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = srv.Repo.WriteProfileText(srv.Context(), "default", "providers/a.yaml", []byte("proxies: [{name: LastGood}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			started, observed, abort := make(chan struct{}), make(chan struct{}), make(chan struct{})
			t.Cleanup(func() { close(abort) })
			t.Cleanup(provider.SetDeliveryEnv(&provider.DeliveryEnv{AggregationFetch: func(ctx context.Context, _, _ string) (*refresh.SourceResult, error) {
				close(started)
				select {
				case <-ctx.Done():
					close(observed)
					return nil, ctx.Err()
				case <-abort:
					return nil, errors.New("test cleanup")
				}
			}}))
			t.Cleanup(provider.SetResponseSeconds(60))
			// Provider Flight uses DefaultSpawner, just as production main does.
			// Bind the request and shutdown to that same configured Runtime.
			startDeliveryRuntime(t, srv.Context())
			done := make(chan *httptest.ResponseRecorder, 1)
			if err := lifecycle.Spawn(srv.Context(), "delivery-review", func(ctx context.Context) {
				reply := httptest.NewRecorder()
				srv.Handler().ServeHTTP(reply, httptest.NewRequest("GET", "/api/aggregations/a/"+suffix, nil).WithContext(ctx))
				done <- reply
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("source I/O did not start")
			}
			lifecycle.Stop(3 * time.Second)
			select {
			case <-observed:
			case <-time.After(time.Second):
				t.Fatal("shutdown did not reach source I/O")
			}
			select {
			case reply := <-done:
				if reply.Code != 500 {
					t.Fatalf("cancelled delivery succeeded: %d", reply.Code)
				}
			case <-time.After(time.Second):
				t.Fatal("handler did not finish")
			}
			path, err := srv.Repo.ProfilePath("default", "providers/a.yaml")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "proxies: [{name: LastGood}]\n" {
				t.Fatal("cancelled delivery replaced last-good provider")
			}
		})
	}
}
