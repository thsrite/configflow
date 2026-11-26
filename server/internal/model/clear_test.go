package model

import (
	"errors"
	"slices"
	"testing"
)

// TestClearMemberIntents (O18 T1c): SystemConfig is a plain record. A stored
// null or wrong-typed member reads as its zero value and is not written back
// (the N8-4f "untouched keeps the stored bytes" intent no longer exists); Clear
// sets a declared member to its zero value; assigning replaces in place.
func TestClearMemberIntents(t *testing.T) {
	const stored = `{"rule_proxy_token":"t","default_profile_id":null,"retired_rule_proxy_tokens":"bad","sub_store_url":"u"}`
	const always = `"server_domain":"","github_proxy_domain":"","rule_fetch_proxy":""`
	const read = `{` + always + `,"rule_proxy_token":"t","sub_store_url":"u"}`
	tests := []struct {
		name string
		edit func(t *testing.T, c *SystemConfig)
		want string
	}{
		{"untouched writes the lenient value", func(t *testing.T, c *SystemConfig) {}, read},
		{"clear of a stored null", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("default_profile_id"))
		}, read},
		{"clear drops a valid value", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("sub_store_url"))
		}, `{` + always + `,"rule_proxy_token":"t"}`},
		{"a non-zero value replaces in place", func(t *testing.T, c *SystemConfig) {
			c.RetiredRuleProxyTokens = []string{"x"}
		}, `{` + always + `,"rule_proxy_token":"t","retired_rule_proxy_tokens":["x"],"sub_store_url":"u"}`},
		{"replace a stored null", func(t *testing.T, c *SystemConfig) {
			id := "p"
			c.DefaultProfileID = &id
		}, `{` + always + `,"rule_proxy_token":"t","default_profile_id":"p","sub_store_url":"u"}`},
		{"clear of a member that is not stored", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("sub_store_mode"))
		}, read},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config SystemConfig
			_, err := DecodeStored([]byte(stored), &config, "")
			noErr(t, err)
			before := config
			tt.edit(t, &config)
			got, err := Marshal(&config)
			noErr(t, err)
			if string(got) != tt.want {
				t.Fatalf("got %s", got)
			}
			untouched, err := Marshal(&before)
			noErr(t, err)
			if string(untouched) != read {
				t.Fatalf("an earlier copy changed: %s", untouched)
			}
		})
	}
}

// TestClearMemberIssuesAndErrors: wrong-typed stored members are reported as
// value-free type issues on read; Clear rejects undeclared members.
func TestClearMemberIssuesAndErrors(t *testing.T) {
	var config SystemConfig
	issues, err := DecodeStored([]byte(`{"retired_rule_proxy_tokens":"bad","node_test_dns":7}`), &config, "")
	noErr(t, err)
	if len(issues) != 2 || !slices.Contains(issues, FieldIssue{"retired_rule_proxy_tokens", IssueType}) ||
		!slices.Contains(issues, FieldIssue{"node_test_dns", IssueType}) {
		t.Fatalf("issues %v", issues)
	}
	if config.RetiredRuleProxyTokens != nil {
		t.Fatalf("wrong-typed list kept: %v", config.RetiredRuleProxyTokens)
	}
	if err := config.Clear("not_declared"); !errors.Is(err, ErrType) {
		t.Fatalf("undeclared member: %v", err)
	}
	var built SystemConfig
	noErr(t, built.Clear("server_domain"))
	got, err := Marshal(&built)
	noErr(t, err)
	if string(got) != `{"server_domain":"","github_proxy_domain":"","rule_fetch_proxy":""}` {
		t.Fatalf("built record %s", got)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
