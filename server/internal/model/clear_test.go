package model

import (
	"encoding/json"
	"errors"
	"testing"
)

// Three write intents for a stored member (Clarified in N8-4f; N8-5):
// untouched (nil/zero keeps a stored null or wrong-typed bytes), replace
// (assign a value) and clear (ClearMember).
func TestClearMemberIntents(t *testing.T) {
	const stored = `{"rule_proxy_token":"t","default_profile_id":null,"retired_rule_proxy_tokens":"bad","sub_store_url":"u"}`
	tests := []struct {
		name string
		edit func(t *testing.T, c *SystemConfig)
		want string
	}{
		{"untouched keeps null and wrong-typed bytes", func(t *testing.T, c *SystemConfig) {
			c.DefaultProfileID, c.RetiredRuleProxyTokens = nil, nil
		}, stored},
		{"clear drops a stored null", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("default_profile_id"))
		}, `{"rule_proxy_token":"t","retired_rule_proxy_tokens":"bad","sub_store_url":"u"}`},
		{"clear drops wrong-typed bytes", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("retired_rule_proxy_tokens"))
		}, `{"rule_proxy_token":"t","default_profile_id":null,"sub_store_url":"u"}`},
		{"clear drops a valid value", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("sub_store_url"))
		}, `{"rule_proxy_token":"t","default_profile_id":null,"retired_rule_proxy_tokens":"bad"}`},
		{"clear then a non-zero value replaces in place", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("retired_rule_proxy_tokens"))
			c.RetiredRuleProxyTokens = []string{"x"}
		}, `{"rule_proxy_token":"t","default_profile_id":null,"retired_rule_proxy_tokens":["x"],"sub_store_url":"u"}`},
		{"replace without clear", func(t *testing.T, c *SystemConfig) {
			id := "p"
			c.DefaultProfileID = &id
		}, `{"rule_proxy_token":"t","default_profile_id":"p","retired_rule_proxy_tokens":"bad","sub_store_url":"u"}`},
		{"clear of a member that is not stored", func(t *testing.T, c *SystemConfig) {
			noErr(t, c.Clear("sub_store_mode"))
		}, stored},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config SystemConfig
			noErr(t, json.Unmarshal([]byte(stored), &config))
			before := config
			tt.edit(t, &config)
			got, err := Marshal(&config)
			noErr(t, err)
			if string(got) != tt.want {
				t.Fatalf("got %s", got)
			}
			untouched, err := Marshal(&before)
			noErr(t, err)
			if string(untouched) != stored {
				t.Fatalf("an earlier copy changed: %s", untouched)
			}
		})
	}
}

func TestClearMemberIssuesAndErrors(t *testing.T) {
	var config SystemConfig
	noErr(t, json.Unmarshal([]byte(`{"retired_rule_proxy_tokens":"bad","node_test_dns":7}`), &config))
	if len(Issues(&config)) != 2 {
		t.Fatalf("issues %v", Issues(&config))
	}
	noErr(t, config.Clear("retired_rule_proxy_tokens"))
	if issues := Issues(&config); len(issues) != 1 || issues[0].Path != "node_test_dns" {
		t.Fatalf("issues after clear %v", issues)
	}
	if err := config.Clear("not_declared"); !errors.Is(err, ErrType) {
		t.Fatalf("undeclared member: %v", err)
	}
	var built SystemConfig
	noErr(t, built.Clear("server_domain"))
	got, err := Marshal(&built)
	noErr(t, err)
	if string(got) != `{}` {
		t.Fatalf("built record %s", got)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
