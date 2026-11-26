package gateway

// Branches the Python corpus has no rows for (A19 HR review,
// server/parity/review-a19.md): content size and rule count limits and the
// TUN filter keys the corpus does not use.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func TestContentLimits(t *testing.T) {
	line := "1.1.1.0/24\n"
	tests := []struct {
		name    string
		content string
		want    string // message; "" = accepted
	}{
		{"16 MiB", "#" + strings.Repeat("x", maxContentBytes-1), ""},
		{"16 MiB + 1", "#" + strings.Repeat("x", maxContentBytes), msgContentTooLarge},
		{"250000 rules", strings.Repeat(line, maxRules), ""},
		{"250001 rules", strings.Repeat(line, maxRules+1), msgTooManyRules},
		{"250001 YAML rules", "payload: [" + strings.Repeat("1.1.1.0/24,", maxRules) + "1.1.1.0/24]", msgTooManyRules},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			networks, err := ExtractCIDRs(tt.content, "ipcidr")
			if tt.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			var gwErr *Error
			if !errors.As(err, &gwErr) || gwErr.Message != tt.want || networks != nil {
				t.Fatalf("got %v %v, want %q", networks, err, tt.want)
			}
		})
	}
}

func fixtureNode(t *testing.T, v any) *yamlx.Node {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	n, err := yamlx.FromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTUNFilterKeysRefused(t *testing.T) {
	config := NewFixtureConfig(map[string]any{"profile_id": "home",
		"mihomo": map[string]any{"linux_gateway": map[string]any{"enabled": true, "rule_set_ids": []any{}}}})
	for _, key := range tunFilterKeys {
		t.Run(key, func(t *testing.T) {
			for _, value := range []any{[]any{"x"}, "x", 1} {
				doc := fixtureNode(t, map[string]any{"tun": map[string]any{"enable": true, key: value}})
				err := config.Apply(doc, "")
				var gwErr *Error
				if !errors.As(err, &gwErr) || gwErr.Message != fmt.Sprintf(fmtTunFilter, key) {
					t.Fatalf("%v: %v", value, err)
				}
			}
			for _, value := range []any{[]any{}, "", 0, nil, false} {
				doc := fixtureNode(t, map[string]any{"tun": map[string]any{key: value}})
				if err := config.Apply(doc, ""); err != nil {
					t.Fatalf("unset %v refused: %v", value, err)
				}
			}
		})
	}
}

// TestScalarSections: a falsy tun/dns (false, 0, 0.0, "", null) is an empty
// section, a truthy scalar is refused (expected values from
// apply_linux_gateway on the test server).
func TestScalarSections(t *testing.T) {
	config := NewFixtureConfig(map[string]any{"profile_id": "home",
		"mihomo": map[string]any{"linux_gateway": map[string]any{"enabled": true, "rule_set_ids": []any{}}}})
	values := []struct {
		value  any
		truthy bool
	}{
		{false, false}, {true, true}, {0, false}, {1, true}, {0.0, false},
		{"", false}, {"x", true}, {nil, false},
	}
	for _, key := range []string{"tun", "dns"} {
		want := msgTunNotObject
		if key == "dns" {
			want = msgDNSNotObject
		}
		for _, v := range values {
			doc := fixtureNode(t, map[string]any{key: v.value})
			err := config.Apply(doc, "")
			var gwErr *Error
			switch {
			case v.truthy && (!errors.As(err, &gwErr) || gwErr.Message != want):
				t.Errorf("%s: %v: %v, want %q", key, v.value, err, want)
			case !v.truthy && (err != nil || yamlx.Get(yamlx.Get(doc, "tun"), "enable").Value != "true" ||
				yamlx.Get(yamlx.Get(doc, "dns"), "fake-ip-range").Value != DefaultFakeIPRange):
				t.Errorf("%s: %v: %v %v", key, v.value, err, doc)
			}
		}
	}
}
