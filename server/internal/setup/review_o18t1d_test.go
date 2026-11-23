package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/repo"
	"configflow/internal/substore"
)

// TestReviewO18T1dCorruptSessionRefused: a stored wizard session with a
// member of the wrong type is refused whole (ErrCorruptRecord), never read
// with that member absent; undeclared members are dropped.
func TestReviewO18T1dCorruptSessionRefused(t *testing.T) {
	head := `"id":"s1","profile_id":"p1","name":"n","goal":"g","step":1,"max_step":2,"created_at":"a","updated_at":"b"`
	base := head + `,"subscription_ids":[],"node_ids":[]`
	for _, member := range []string{
		`"prepared":"yes"`, `"completed":1`, `"abandoned":"x"`, `"profile_revision":"7x"`, `"owned_digest":{}`,
		`"domain_rules":"x"`, `"domain_rules":{"direct":"a"}`, `"domain_rules":[]`, `"mihomo_changes":{}`,
		`"router_id":{}`, `"legacy_takeover":0`,
	} {
		var record Record
		err := json.Unmarshal([]byte(`{`+base+`,`+member+`}`), &record)
		if !errors.Is(err, ErrCorruptRecord) {
			t.Errorf("%s: err = %v", member, err)
		}
	}
	for _, member := range []string{`"subscription_ids":"x","node_ids":[]`, `"subscription_ids":[],"node_ids":{}`} {
		var record Record
		data := `{` + head + `,` + member + `}`
		if err := json.Unmarshal([]byte(data), &record); !errors.Is(err, ErrCorruptRecord) {
			t.Errorf("%s: err = %v", member, err)
		}
	}
	var record Record
	if err := json.Unmarshal([]byte(`{`+base+`,"extra":1,"domain_rules":{"direct":["a"],"proxy":[],"x":1}}`), &record); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	want := `{` + base + `,"domain_rules":{"direct":["a"],"proxy":[]}}`
	if string(data) != want {
		t.Fatalf("written %s\nwant    %s", data, want)
	}
	for _, bad := range []string{`[]`, `"x"`, `null`, `5`} {
		if err := json.Unmarshal([]byte(bad), &record); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// TestReviewO18T1dLegacyCacheWithoutURL: a cache whose metadata has no url
// member (a legacy cache) is not "订阅链接已变化"; a stored URL that differs
// (or null) still is.
func TestReviewO18T1dLegacyCacheWithoutURL(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	fields := map[string]json.RawMessage{
		"subscriptions": json.RawMessage(`[{"id":"s1","name":"S","url":"https://example.com/a"}]`),
		"nodes":         json.RawMessage(`[]`),
	}
	record := &Record{SubscriptionIDs: []string{"s1"}}
	path, err := substore.CachePath(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	node := `{"name":"n1","type":"http","server":"192.0.2.1","port":80}`
	tests := []struct {
		metadata string
		changed  bool
	}{
		{`{"subscription_name":"S"}`, false},
		{`{"subscription_name":"S","url":"https://example.com/a"}`, false},
		{`{"subscription_name":"S","url":"https://example.com/b"}`, true},
		{`{"subscription_name":"S","url":null}`, true},
	}
	for i, tt := range tests {
		data := `{"subscription_id":"s1","updated_at":"2026-01-01T00:00:0` + strconv.Itoa(i) + `Z","count":1,"nodes":[` + node + `],"metadata":` + tt.metadata + `}`
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := resourceNodes(ctx, fields, record)
		if changed := err != nil && strings.Contains(err.Error(), msgChangedSourceURL); changed != tt.changed {
			t.Errorf("%s: err = %v, want changed=%v", tt.metadata, err, tt.changed)
		}
	}
}
