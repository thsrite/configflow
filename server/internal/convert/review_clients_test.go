package convert_test

import (
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
