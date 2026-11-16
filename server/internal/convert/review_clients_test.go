package convert_test

import (
	"bytes"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

// A delimiter inside an ID must not shift into the exclusion dimension of
// the remote cache key. The two Python tuple keys are distinct.
func TestLoonRemoteCacheKeepsSourceAndExclusionSeparate(t *testing.T) {
	config := jsonx.Obj(
		"subscriptions", []any{
			jsonx.Obj("id", "a", "name", "First"),
			jsonx.Obj("id", "a\x01b", "name", "Second"),
		},
		"proxy_groups", []any{
			jsonx.Obj("id", "one", "name", "One", "type", "select", "subscriptions", []any{"a"}, "exclude_regex", "b\x01c"),
			jsonx.Obj("id", "two", "name", "Two", "type", "select", "subscriptions", []any{"a\x01b"}, "exclude_regex", "c"),
		},
	)
	out := loonOf(t, config)
	remotes := udpSection(out, "Remote Proxy")
	if len(remotes) != 2 {
		t.Fatalf("distinct sources/exclusions need two remotes, got %q", remotes)
	}
	for _, want := range []string{
		"First = /subscriptions/a/proxies?format=loon&exclude_regex=b%01c,",
		"Second = /subscriptions/a\x01b/proxies?format=loon&exclude_regex=c,",
		"One = select,First",
		"Two = select,Second",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output\n%s", want, out)
		}
	}
}

func TestSurgeDebugLogOmitsSubscriptionURLsAndTokens(t *testing.T) {
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.Close()
		if _, err := logx.Setup(logx.Config{Level: logx.Critical}); err != nil {
			t.Error(err)
		}
	})
	config := jsonx.Obj(
		"system_config", jsonx.Obj("server_domain", "https://panel.example.com", "config_token", "synthetic-config-token"),
		"subscriptions", []any{jsonx.Obj("id", "feed", "name", "Feed")},
		"proxy_groups", []any{jsonx.Obj("id", "group", "name", "Proxy", "type", "select", "subscriptions", []any{"feed"})},
	)
	surgeOf(t, config)
	for _, secret := range []string{"synthetic-config-token", "https://panel.example.com", "/subscriptions/"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("debug log contains %q", secret)
		}
	}
	if !strings.Contains(logs.String(), "Generated Surge proxy group") {
		t.Fatal("group generation was not exercised with debug logging")
	}
}
