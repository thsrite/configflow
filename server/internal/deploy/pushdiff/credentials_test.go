package pushdiff

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/agents/diag"
)

func TestCredentialFieldsAndSpans(t *testing.T) {
	for _, tc := range []struct {
		key    string
		secret bool
	}{
		{"obfs-password", true}, {"PASSWORD", true}, {"private_key", true}, {"client-secret", true}, {"cookie", true},
		{"proxy-authorization", true}, {"private-key-passphrase", true}, {"custom-psk", true},
		{"token.example.com", false}, {"public-key", false}, {"server", false}, {"contains space token", false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if diag.IsCredentialKey(tc.key) != tc.secret {
				t.Fatal(tc)
			}
		})
	}
	for _, tc := range []struct {
		text    string
		secrets []string
	}{
		{"https://dns.example.com/dns-query", nil},
		{"socks5 user:pa55word@192.0.2.1:1080 then https://rules.example.com/?key=abc&v=1", []string{"user:pa55word", "abc"}},
		{"alice:wonder@192.0.2.10:1080", []string{"alice:wonder"}},
		{"https://rules.example.com/?Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg", []string{"Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg"}},
		{"https://rules.example.com/a?api_key=k%2B1&x=2#frag", []string{"k%2B1"}},
		{"see http://[::1/broken", []string{"http://[::1/broken"}},
		{"mail me: someone@example.com", nil},
		{"中文 https://u:p@rules.example.com/Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg?access_token=value", []string{"u:p", "Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg", "value"}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			var secrets []string
			for _, r := range diag.CredentialSpans(tc.text) {
				secrets = append(secrets, tc.text[r[0]:r[1]])
			}
			compareJSON(t, secrets, tc.secrets)
		})
	}
	// Data keys named "secret" and domains named "token" remain visible.
	c := Config([]byte("hosts: {}\nrule-providers: {}\n"), []byte("hosts: {token.example.com: 192.0.2.1}\nrule-providers: {secret: {url: 'https://rules.example.com/a'}}\n"), "", "")
	for _, r := range c.Items {
		if r.Sensitive {
			t.Fatalf("data key treated as credential %+v", r)
		}
	}
	// Credential-bearing mapping keys in JSON display values must be marked too.
	c = Config([]byte("k: before\n"), []byte("k: {'https://u:key-password@rules.example.com/path': value}\n"), "", "")
	hidden, _ := json.Marshal(WithoutSecrets(c))
	if strings.Contains(string(hidden), "key-password") {
		t.Fatalf("mapping key leaked: %s", hidden)
	}
	c = Config([]byte("hosts: {'https://u:key-password@rules.example.com/path': old}\n"), []byte("hosts: {'https://u:key-password@rules.example.com/path': new}\n"), "", "")
	hidden, _ = json.Marshal(WithoutSecrets(c))
	if strings.Contains(string(hidden), "key-password") {
		t.Fatalf("metadata key leaked: %s", hidden)
	}

}
