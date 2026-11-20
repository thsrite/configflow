package rules

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

func TestPublicRulesetContentTimeoutsAndOffline(t *testing.T) {
	srv := fixture(t)
	config, err := repo.GetConfig(srv.Context(), "other")
	if err != nil {
		t.Fatal(err)
	}
	configRaw, _ := json.Marshal(config)
	item := json.RawMessage(`{"id":"source","name":"Synthetic source","source_type":"url","url":"https://example.com/source","behavior":"domain","format":"text"}`)
	previous := rulefetch.DefaultClient
	t.Cleanup(func() { rulefetch.DefaultClient = previous })
	t.Run("offline", func(t *testing.T) {
		transport := &refusingTransport{}
		rulefetch.DefaultClient = &rulefetch.Client{Transport: transport}
		value, err := ReadRulesetContent(srv.Context(), configRaw, item, nil, ContentOptions{})
		if err != nil || value != "" || transport.calls.Load() != 0 {
			t.Fatal("offline source used network", value, err)
		}
	})
	t.Run("connect_timeout", func(t *testing.T) {
		var calls atomic.Int32
		rulefetch.DefaultClient = &rulefetch.Client{Transport: reviewTransport(func(ctx context.Context, request rulefetch.TransportRequest) (*rulefetch.Response, error) {
			calls.Add(1)
			if request.ConnectTimeout != 20*time.Millisecond || request.ReadTimeout != 20*time.Second {
				t.Errorf("connect/read contract = %s/%s", request.ConnectTimeout, request.ReadTimeout)
			}
			timer := time.NewTimer(request.ConnectTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil, &rulefetch.FetchError{Class: "ConnectTimeout", Message: "synthetic connect timeout"}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})}
		started := time.Now()
		value, err := ReadRulesetContent(srv.Context(), configRaw, item, nil, ContentOptions{AllowNetwork: true, ConnectTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Second})
		if err != nil || value != "" || calls.Load() != 1 || time.Since(started) > time.Second {
			t.Fatalf("connect timeout not bounded: %q %v calls=%d", value, err, calls.Load())
		}
	})
	t.Run("read_shortened_by_budget", func(t *testing.T) {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-request.Context().Done()
		}))
		defer origin.Close()
		var captured rulefetch.TransportRequest
		var calls atomic.Int32
		rulefetch.DefaultClient = &rulefetch.Client{Transport: reviewTransport(func(ctx context.Context, request rulefetch.TransportRequest) (*rulefetch.Response, error) {
			calls.Add(1)
			captured = request
			return (rulefetch.HTTPTransport{}).Do(ctx, request)
		})}
		body, _ := json.Marshal(map[string]string{"id": "budget-source", "name": "Budget", "source_type": "url", "url": origin.URL, "behavior": "domain", "format": "text"})
		ctx, cancel := context.WithTimeout(srv.Context(), 150*time.Millisecond)
		defer cancel()
		started := time.Now()
		value, err := ReadRulesetContent(ctx, configRaw, body, nil, ContentOptions{AllowNetwork: true, ConnectTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second})
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if calls.Load() != 1 || value != "" || captured.ConnectTimeout <= 0 || captured.ConnectTimeout > 150*time.Millisecond || captured.ReadTimeout <= 0 || captured.ReadTimeout > 150*time.Millisecond || time.Since(started) > time.Second {
			t.Fatalf("read not bounded by deadline: %q %v timeouts=%s/%s calls=%d", value, err, captured.ConnectTimeout, captured.ReadTimeout, calls.Load())
		}
	})
}

// TestPublicRulesetContentCapturedRecords covers the captured-record decoder boundary.
func TestPublicRulesetContentCapturedRecords(t *testing.T) {
	app := fixture(t)
	validConfig := json.RawMessage(`{}`)
	validItem := json.RawMessage(`{"id":"captured-source","name":"Captured","source_type":"content","behavior":"domain","format":"text","content":"+.item.example\n"}`)
	validLibrary := json.RawMessage(`{"id":"captured-library","name":"Captured library","source_type":"content","behavior":"domain","format":"text","content":"+.library.example\n"}`)
	transport := &refusingTransport{}
	previous := rulefetch.DefaultClient
	rulefetch.DefaultClient = &rulefetch.Client{Transport: transport}
	t.Cleanup(func() { rulefetch.DefaultClient = previous })
	for _, tc := range []struct {
		name                  string
		config, item, library json.RawMessage
		want                  string
		invalid               bool
	}{
		{name: "item", config: validConfig, item: validItem, want: "+.item.example\n"},
		{name: "null-library", config: validConfig, item: validItem, library: json.RawMessage(`null`), want: "+.item.example\n"},
		{name: "library-precedence", config: validConfig, item: validItem, library: validLibrary, want: "+.library.example\n"},
		{name: "malformed-config", config: json.RawMessage(`{`), item: validItem, invalid: true},
		{name: "non-object-config", config: json.RawMessage(`null`), item: validItem, invalid: true},
		{name: "malformed-item", config: validConfig, item: json.RawMessage(`{`), invalid: true},
		{name: "non-object-item", config: validConfig, item: json.RawMessage(`[]`), invalid: true},
		{name: "malformed-library", config: validConfig, item: validItem, library: json.RawMessage(`{`), invalid: true},
		{name: "non-object-library", config: validConfig, item: validItem, library: json.RawMessage(`42`), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadRulesetContent(app.Context(), tc.config, tc.item, tc.library, ContentOptions{AllowNetwork: true})
			if tc.invalid {
				if err == nil {
					t.Fatalf("invalid captured record accepted: %q", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q err=%v, want %q", got, err, tc.want)
			}
		})
	}
	stopped, cancel := context.WithCancel(app.Context())
	cancel()
	if _, err := ReadRulesetContent(stopped, json.RawMessage(`{`), validItem, nil, ContentOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost before decoding: %v", err)
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("captured inline/invalid records caused %d requests", transport.calls.Load())
	}
}
