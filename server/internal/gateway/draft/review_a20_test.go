package draft

import (
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

// A20 review: the preview's field mask must never show less than Python's
// _safe, which hides any key containing one of its credential words. The
// shared credential-key list matches exact names and suffixes only.
func TestReviewA20DisplayHidesPythonCredentialKeys(t *testing.T) {
	tests := []struct{ key, value string }{
		{"secret_key", "synthetic-secret-key-value"},
		{"api_tokens", "synthetic-token-list-value"},
		{"vmess-uuid", "synthetic-uuid-value"},
		{"X-Authorization", "synthetic-header-value"},
		{"PasswordHint", "synthetic-password-hint"},
		{"private_key_file", "synthetic-private-key-file"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			node, err := yamlx.FromJSON(encode(map[string]any{tc.key: tc.value, "note": "plain"}))
			if err != nil {
				t.Fatal(err)
			}
			shown := display(node, "proxy", nil)
			if strings.Contains(shown, tc.value) || !strings.Contains(shown, msgHidden) || !strings.Contains(shown, "plain") {
				t.Fatalf("display %q:\n%s", tc.key, shown)
			}
			leaf, err := yamlx.FromJSON(encode(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			// yaml.safe_dump quotes the mask: Python shows '[已隐藏]' too.
			if shown := display(leaf, tc.key, nil); shown != "'"+msgHidden+"'" {
				t.Fatalf("leaf %q shown as %q", tc.key, shown)
			}
		})
	}
	// Ordinary keys that merely resemble credential words stay readable.
	node, err := yamlx.FromJSON([]byte(`{"username":"operator","server":"192.0.2.10"}`))
	if err != nil {
		t.Fatal(err)
	}
	if shown := display(node, "proxy", nil); !strings.Contains(shown, "operator") || !strings.Contains(shown, "192.0.2.10") {
		t.Fatalf("ordinary fields hidden:\n%s", shown)
	}
}

// A20 review: restore_point members are exact-case like Python's dict.get.
// encoding/json's case-insensitive struct matching accepted "ID"/"Mihomo"
// and let a later "ID" erase a valid "id".
func TestReviewA20RestorePointMembersAreExactCase(t *testing.T) {
	tests := []struct {
		name, point string
		valid       bool
	}{
		{"exact", `{"version":1,"id":"p1","created_at":"t","mihomo":{},"oxidns":{}}`, true},
		{"upper id only", `{"version":1,"ID":"p1","created_at":"t","mihomo":{},"oxidns":{}}`, false},
		{"upper sections only", `{"version":1,"id":"p1","created_at":"t","Mihomo":{},"OxiDNS":{}}`, false},
		{"later upper id ignored", `{"version":1,"id":"p1","ID":"","created_at":"t","mihomo":{},"oxidns":{}}`, true},
		{"later upper section ignored", `{"version":1,"id":"p1","created_at":"t","mihomo":{},"Mihomo":null,"oxidns":{}}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := document{"mihomo": []byte(`{"` + RestoreKey + `":` + tc.point + `}`)}
			p := point(config)
			if (p != nil) != tc.valid {
				t.Fatalf("valid=%v, want %v", p != nil, tc.valid)
			}
			if p != nil && (p.ID != "p1" || p.Mihomo == nil || p.OxiDNS == nil) {
				t.Fatalf("decoded %+v", p)
			}
		})
	}
}
