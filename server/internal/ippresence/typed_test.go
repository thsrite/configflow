package ippresence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

type denyingTransport struct{ calls atomic.Int64 }

func (d *denyingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, os.ErrPermission
}

func TestTypedPresenceMatchesOfflineLegacy(t *testing.T) {
	ctx, _, _, config := listRepo(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	entry := config.GetList("rule_library")[0].(*jsonx.Object)
	entry.Set("url", server.URL+"/rules.list")
	path, err := rulefetch.RuleCacheReadPath(ctx, entry, config.GetList("rule_library"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	var library []model.RuleLibraryItem
	data, err := jsonx.Compact(config.Value("rule_library"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &library); err != nil {
		t.Fatal(err)
	}
	var rule model.RuleConfig
	data, err = jsonx.Compact(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &rule); err != nil {
		t.Fatal(err)
	}
	// Neither path may try HTTP, even for missing or invalid cache bytes.
	transport := &denyingTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous }()
	for _, tt := range []struct {
		name     string
		body     []byte
		absent   bool
		expected any
	}{{"domain", []byte("DOMAIN-SUFFIX,example.com\n"), false, false}, {"ip", []byte("IP-CIDR,192.0.2.0/24\n"), false, true}, {"invalid UTF8", []byte{0xff}, false, nil}, {"missing", nil, true, nil}} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.absent {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, tt.body, 0600); err != nil {
				t.Fatal(err)
			}
			ClearMemo()
			want, oldErr := ClassicalIPLines(ctx, entry, config)
			if want != tt.expected {
				t.Fatalf("legacy path did not inspect cache: got %v want %v", want, tt.expected)
			}
			ClearMemo()
			got, err := TypedClassicalIPLines(ctx, rule, library)
			if (err == nil) != (oldErr == nil) {
				t.Fatalf("error: %v / %v", err, oldErr)
			}
			if want == nil {
				if got != nil {
					t.Fatalf("want unknown: %v", *got)
				}
			} else if got == nil || *got != want {
				t.Fatalf("presence: %v / %v", got, want)
			}
		})
	}
	if transport.calls.Load() != 0 || requests.Load() != 0 {
		t.Fatalf("%d outbound requests", transport.calls.Load())
	}
	// A known format must preserve validation, not return arbitrary cache bytes.
	if err := os.WriteFile(path, []byte("DOMAIN-SUFFIX,example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := rulefetch.ReadValidatedRuleCache(ctx, entry, config)
	if err != nil {
		t.Fatal(err)
	}
	known, err := rulefetch.ReadValidatedRuleCacheWithFormat(ctx, entry, config, "text")
	if err != nil {
		t.Fatal(err)
	}
	if len(old) == 0 || string(old) != string(known) {
		t.Fatalf("known-format entry changed validation: %q / %q", old, known)
	}
	invalid, err := rulefetch.ReadValidatedRuleCacheWithFormat(ctx, entry, config, "yaml")
	if err != nil || invalid != nil {
		t.Fatalf("known format bypassed validation: %q / %v", invalid, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := TypedClassicalIPLines(canceled, rule, library); err != context.Canceled {
		t.Fatalf("canceled read: %v", err)
	}
}

func TestTypedPresenceInlineDeclarationFirstMatch(t *testing.T) {
	str := func(s string) *string { return &s }
	rule := model.RuleConfig{LibraryRuleID: "library", Content: str("IP-CIDR,192.0.2.0/24"), SourceType: str("content")}
	library := []model.RuleLibraryItem{{ID: "library", Name: "first", SourceType: str("content"), Content: str("DOMAIN-SUFFIX,example.com")}, {ID: "library", SourceType: str("content"), Content: str("IP-CIDR,192.0.2.0/24")}}
	got, err := TypedClassicalIPLines(t.Context(), rule, library)
	if err != nil || got == nil || *got {
		t.Fatalf("first declaration not respected: %v / %v", got, err)
	}
	library[0].Content = str("IP-CIDR,192.0.2.0/24")
	got, err = TypedClassicalIPLines(t.Context(), rule, library)
	if err != nil || got == nil || !*got {
		t.Fatalf("inline IP lost: %v / %v", got, err)
	}
}
