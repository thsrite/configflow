package subs

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

// Regressions from the N7-2 Opus review (server/parity/review-n7-2.md).

func TestReviewN72RelinkKeepsAliasChains(t *testing.T) {
	var b strings.Builder
	b.WriteString("pad: [" + strings.Repeat("0, ", 3000) + "0]\n")
	b.WriteString("x: &x [" + strings.Repeat("a, ", 50) + "a]\n")
	b.WriteString("u: &u [" + strings.Repeat("*x, ", 30) + "*x]\n")
	b.WriteString("listeners:\n  - name: in\n    proxy: Pinned\n    users: *u # shared users\n")
	source := b.String()
	for _, newName := range []*string{strp("New"), nil} {
		profile, changed, err := relink(t, source, newName)
		if err != nil || strings.Join(changed, ",") != "listeners[0].proxy" {
			t.Fatalf("changed %v err %v", changed, err)
		}
		out := profile.GetObject("mihomo").GetString("custom_config")
		got := yamlx.Get(yamlx.Get(mustListenerParse(t, out), "listeners").Content[0], "users")
		want := yamlx.Get(mustListenerParse(t, source), "u")
		if !yamlx.Equal(got, want) {
			t.Fatalf("listener users changed: %s", out[strings.Index(out, "listeners:"):])
		}
		if len(out) > len(source)+64 || !strings.Contains(out, "# shared users") {
			t.Fatalf("alias chain expanded or comment lost: %d bytes from %d", len(out), len(source))
		}
	}
}

func TestReviewN72RelinkKeepsAliasItemComments(t *testing.T) {
	tests := []struct{ source, comment string }{
		{"# top\nshared: &base {name: pinned, type: mixed, proxy: Pinned} # base\nlisteners:\n- <<: *base # merge\n  port: 17890 # port\n", "# merge"},
		{"# top\nshared: &base\n  name: pinned\n  proxy: Pinned\nlisteners:\n- *base # list\n", "# list"},
	}
	for _, tt := range tests {
		for _, newName := range []*string{strp("New"), nil} {
			profile, _, err := relink(t, tt.source, newName)
			if err != nil {
				t.Fatal(err)
			}
			out := profile.GetObject("mihomo").GetString("custom_config")
			if !strings.HasPrefix(out, "# top\n") || strings.Index(out, tt.comment) < strings.Index(out, "listeners:") {
				t.Fatalf("alias comment moved away from its listener:\n%s", out)
			}
			shared := yamlx.Get(mustListenerParse(t, out), "shared")
			if yamlx.Get(shared, "proxy").Value != "Pinned" {
				t.Fatalf("shared anchor changed:\n%s", out)
			}
		}
	}
}

func TestReviewN72RelinkMultipleDocumentsMessage(t *testing.T) {
	_, _, err := relink(t, "listeners: [{proxy: Pinned}]\n---\nother: 1\n", nil)
	want := msgListenerBaseParse + "：YAML 只能包含一个文档（第 2 行开始第二个文档）"
	if err == nil || err.Error() != want || !errors.Is(err, yamlx.ErrMultipleDocuments) {
		t.Fatalf("got %v want %s", err, want)
	}
}
