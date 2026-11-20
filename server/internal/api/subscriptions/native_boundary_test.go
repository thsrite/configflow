package subscriptions_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/api/aggregations"
	"configflow/internal/api/nodes"
	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestNativeResourceRequestsRejectNonfiniteBeforeEffects(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register, aggregations.Register, nodes.Register)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{{ID: "s", Name: "Synthetic", URL: "https://feed.example/s"}}
		shared.Aggregations = []model.Aggregation{{ID: "a", Name: "Aggregate", Subscriptions: []string{}, Nodes: []string{}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := srv.Repo.Head(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ method, path string }{
		{"POST", "/api/subscriptions"}, {"PUT", "/api/subscriptions/s"}, {"POST", "/api/subscriptions/reorder"}, {"POST", "/api/subscriptions/s/fetch"},
		{"POST", "/api/aggregations"}, {"PUT", "/api/aggregations/a"}, {"POST", "/api/aggregations/reorder"},
		{"POST", "/api/nodes"}, {"PUT", "/api/nodes/missing"}, {"POST", "/api/nodes/reorder"}, {"POST", "/api/nodes/delete-preview"},
		{"POST", "/api/nodes/latency"}, {"POST", "/api/nodes/latency/settings"}, {"POST", "/api/nodes/latency/dns/check"},
	}
	for _, tc := range tests {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			for _, number := range []string{"NaN", "Infinity", "-Infinity"} {
				srv.Do(tc.method, tc.path, httpxtest.Body([]byte(`{"id":"rejected","name":"Rejected","url":"https://feed.example/s","ids":["s"],"nodes":[],"subscriptions":[],"nested":[`+number+`]}`), "application/json")).WantStatus(400)
				after, err := srv.Repo.Head(srv.Context())
				if err != nil || after != before {
					t.Fatalf("nonfinite request changed repository: before=%v after=%v err=%v", before, after, err)
				}
			}
		})
	}
	// A standard JSON body containing those strings remains valid and reaches
	// the writer; the rejection test cannot pass because all writes are disabled.
	srv.Post("/api/subscriptions", httpxtest.Body([]byte(`{"id":"control","name":"NaN Infinity -Infinity","url":"https://feed.example/valid"}`), "application/json")).WantStatus(200)
	after, err := srv.Repo.Head(srv.Context())
	if err != nil || after == before {
		t.Fatalf("finite control did not write: %v", err)
	}
}

func TestNativeRetiredCapabilityRemainsPrivateWithMalformedSibling(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
	err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
		return system.SystemConfig.SetJSON("retired_rule_proxy_tokens", []byte(`["synthetic-retired-capability",42]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Get("/api/profiles/default/subscriptions/missing/proxies?token=synthetic-retired-capability").WantStatus(401)
}

func TestNativeMalformedConfigTokenFailsClosed(t *testing.T) {
	for _, raw := range []string{"false", "0", "[]", "{}"} {
		t.Run(raw, func(t *testing.T) {
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
			if err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
				return system.SystemConfig.SetJSON("config_token", []byte(raw))
			}); err != nil {
				t.Fatal(err)
			}
			srv.Get("/api/profiles/default/subscriptions/missing/proxies").WantStatus(401)
			// An empty string is an unconfigured credential. With login disabled,
			// authentication admits it and the missing resource returns 404.
			if err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
				return system.SystemConfig.SetJSON("config_token", []byte(`""`))
			}); err != nil {
				t.Fatal(err)
			}
			srv.Get("/api/profiles/default/subscriptions/missing/proxies").WantStatus(404)
		})
	}
}

func TestSubscriptionWritesKeepRuntimeThroughPreflight(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		for _, tc := range []struct{ method, path, body string }{
			{"POST", "/api/subscriptions", `{"id":"new","name":"New","url":"https://feed.example/new"}`},
			{"PUT", "/api/subscriptions/s", `{"name":"Changed","url":"https://feed.example/s"}`},
			{"DELETE", "/api/subscriptions/s", ``},
			{"POST", "/api/subscriptions/reorder", `{"ids":["s"]}`},
		} {
			label := "disconnect/"
			if stopped {
				label = "shutdown/"
			}
			t.Run(label+tc.method+tc.path, func(t *testing.T) {
				srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
				if err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
					shared.Subscriptions = []model.Subscription{{ID: "s", Name: "Synthetic", URL: "https://feed.example/s"}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				ctx, cancel := context.WithCancel(runtime.BindRequest(srv.Context()))
				defer cancel()
				if stopped {
					runtime.Shutdown(context.Background())
				} else {
					cancel()
				}
				before, err := srv.Repo.Head(srv.Context())
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)).WithContext(ctx)
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				srv.Handler().ServeHTTP(response, request)
				after, err := srv.Repo.Head(srv.Context())
				if err != nil {
					t.Fatal(err)
				}
				if stopped {
					if response.Code == 200 || after != before {
						t.Fatalf("shutdown wrote: status=%d before=%v after=%v", response.Code, before, after)
					}
				} else if response.Code != 200 || after == before {
					t.Fatalf("disconnect lost write: status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
}
