package substore

import (
	"context"
	"errors"
	"testing"
)

func TestSourceURLConfigured(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"https://sub.example.com/link/abc?clash=1", true},
		{"http://198.51.100.7:8080/sub#options", true},
		{"  https://sub.example.com/a \n", true},
		{"订阅链接\nhttps://sub.example.com/b", true},
		{"订阅链接", false},
		{"", false},
		{"sub.example.com/link", false},
		{"https:///no-host", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := SourceURLConfigured(tt.raw); got != tt.want {
			t.Errorf("SourceURLConfigured(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

// The template subscription's placeholder address fails at once: no
// Sub-Store start, no request to it.
func TestPlaceholderSubscriptionDoesNotWakeSubStore(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	_, err := h.service.Client.Fetch(context.Background(), "sub_template", "订阅链接", "", FetchOptions{Refresh: true})
	var request *RequestError
	if !errors.As(err, &request) || request.Class != "InvalidURL" || request.Message != msgSourceURLUnconfigured {
		t.Fatalf("err %v", err)
	}
	if starts, _ := h.commands.counts(); starts != 0 || h.store.envRequests != 0 || len(h.store.eventList()) != 0 {
		t.Fatal("placeholder address woke Sub-Store")
	}
}
