package rules

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

type reviewTransport func(context.Context, rulefetch.TransportRequest) (*rulefetch.Response, error)

func (f reviewTransport) Do(ctx context.Context, req rulefetch.TransportRequest) (*rulefetch.Response, error) {
	return f(ctx, req)
}

func reviewResponse(req rulefetch.TransportRequest, status int, content string) *rulefetch.Response {
	return &rulefetch.Response{StatusCode: status, URL: req.URL, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(content))}
}

func TestReviewSourceTimeoutAndMirror(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "HTTP_failure_uses_mirror"
		if invalid {
			name = "validation_failure_stops_before_mirror"
		}
		t.Run(name, func(t *testing.T) {
			srv := fixture(t)
			ctx := srv.Context()
			config, err := repo.GetConfig(ctx, "other")
			if err != nil {
				t.Fatal(err)
			}
			library := jsonx.Obj("id", "remote", "name", "Remote", "source_type", "url", "url", "https://primary.example.com/source", "behavior", "domain", "format", "text")
			item := jsonx.Obj("url", "https://mirror.example.com/source")
			var requests []string
			service := &Service{client: &rulefetch.Client{Transport: reviewTransport(func(_ context.Context, req rulefetch.TransportRequest) (*rulefetch.Response, error) {
				requests = append(requests, req.URL)
				if req.Method != "GET" || req.ConnectTimeout != 30*time.Second || req.ReadTimeout != 30*time.Second {
					t.Fatalf("download contract: %+v", req)
				}
				if req.URL == library.GetString("url") {
					if invalid {
						return reviewResponse(req, 200, "IP-CIDR,192.0.2.0/24\n"), nil
					}
					return reviewResponse(req, 503, "unavailable"), nil
				}
				if req.URL != item.GetString("url") {
					t.Fatalf("unexpected address: %s", req.URL)
				}
				return reviewResponse(req, 200, "+.mirror.example.com\n"), nil
			})}}
			content, err := service.readSource(ctx, fetchConfig(testSourceConfig(t, config)), testSourceRule(t, item), testSourceLibrary(t, library), sourceOptions{})
			if invalid {
				var validation *rulefetch.RemoteRuleValidationError
				if !errors.As(err, &validation) || len(requests) != 1 {
					t.Fatalf("invalid source: content=%q err=%v requests=%v", content, err, requests)
				}
				return
			}
			if err != nil || content != "+.mirror.example.com\n" || len(requests) != 2 {
				t.Fatalf("mirror: content=%q err=%v requests=%v", content, err, requests)
			}
			mirror := library.Clone()
			mirror.Set("url", item.GetString("url"))
			cached, err := rulefetch.ReadValidatedRuleCache(ctx, mirror, config)
			if err != nil || string(cached) != content {
				t.Fatalf("mirror cache: %q %v", cached, err)
			}
			primary, err := rulefetch.ReadValidatedRuleCache(ctx, library, config)
			if err != nil || primary != nil {
				t.Fatalf("mirror was cached as primary: %q %v", primary, err)
			}
		})
	}
}

func TestReviewShutdownDuringDownload(t *testing.T) {
	for _, compose := range []bool{false, true} {
		name := "match_source"
		if compose {
			name = "compose"
		}
		t.Run(name, func(t *testing.T) {
			srv := fixture(t)
			runtime, ctx := reviewRuntimeContext(t, srv.Context())
			before, err := srv.Repo.ExportAll(srv.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			service := &Service{now: time.Now, client: &rulefetch.Client{Transport: reviewTransport(func(_ context.Context, req rulefetch.TransportRequest) (*rulefetch.Response, error) {
				calls++
				// The response can race with shutdown and still deliver valid bytes.
				runtime.Shutdown(context.Background())
				return reviewResponse(req, 200, "+.remote.example.com\n"), nil
			})}}
			source := jsonx.Obj("id", "shutdown-source", "name", "Shutdown source", "source_type", "url", "url", "https://primary.example.com/source", "behavior", "domain", "format", "text")
			if compose {
				raw, marshalErr := json.Marshal(source)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				_, err = service.Compose(ctx, "other", composeBody(record{"kind": stringJSON("new_library"), "library": raw}, "direct"), "")
			} else {
				config, getErr := repo.GetConfig(srv.Context(), "other")
				if getErr != nil {
					t.Fatal(getErr)
				}
				_, err = service.readSource(lifecycle.Detach(ctx), fetchConfig(testSourceConfig(t, config)), testSourceRule(t, jsonx.Obj("url", "https://mirror.example.com/source")), testSourceLibrary(t, source), sourceOptions{})
			}
			if !errors.Is(err, context.Canceled) || calls != 1 {
				t.Fatalf("shutdown err=%v requests=%d", err, calls)
			}
			after, readErr := srv.Repo.ExportAll(srv.Context(), false)
			if readErr != nil || !jsonx.Equal(before, after) {
				t.Fatalf("shutdown committed source/profile/DNS: %v", readErr)
			}
			cached, readErr := rulefetch.ReadValidatedRuleCache(srv.Context(), source, nil)
			if readErr != nil || cached != nil {
				t.Fatalf("shutdown cached response: %q %v", cached, readErr)
			}
		})
	}
}

func TestReviewStoppedReadOwners(t *testing.T) {
	srv := fixture(t)
	runtime, ctx := reviewRuntimeContext(t, srv.Context())
	runtime.Shutdown(context.Background())
	for name, call := range map[string]func() error{
		"local": func() error { _, err := (&Service{}).Local(ctx, "other", "Ads", "", ""); return err },
		"match": func() error {
			_, err := (&Service{}).Match(ctx, "other", record{"query": stringJSON("example.com")}, "")
			return err
		},
		"duplicates": func() error { _, err := (&Service{}).FindDuplicates(ctx, "other", ""); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("stopped owner: %v", err)
			}
		})
	}
}

func testSourceConfig(t *testing.T, value any) *model.Config {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	config, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
func testSourceRule(t *testing.T, value any) model.RuleConfig {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var rule model.RuleConfig
	if err := json.Unmarshal(raw, &rule); err != nil {
		t.Fatal(err)
	}
	return rule
}
func testSourceLibrary(t *testing.T, value any) *model.RuleLibraryItem {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var rule model.RuleLibraryItem
	if err := json.Unmarshal(raw, &rule); err != nil {
		t.Fatal(err)
	}
	return &rule
}
