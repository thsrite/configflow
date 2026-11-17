package gateway

// Branches the Python corpus has no rows for (A19 HR review,
// server/parity/review-a19.md): content size and rule count limits and the
// TUN filter keys the corpus does not use.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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

func TestTUNFilterKeysRefused(t *testing.T) {
	config := jsonx.Obj("profile_id", "home",
		"mihomo", jsonx.Obj("linux_gateway", jsonx.Obj("enabled", true, "rule_set_ids", []any{})))
	for _, key := range tunFilterKeys {
		t.Run(key, func(t *testing.T) {
			for _, value := range []any{[]any{"x"}, "x", jsonx.Int(1)} {
				doc := jsonx.Obj("tun", jsonx.Obj("enable", true, key, value))
				err := ApplyJSONX(doc, config, "")
				var gwErr *Error
				if !errors.As(err, &gwErr) || gwErr.Message != fmt.Sprintf(fmtTunFilter, key) {
					t.Fatalf("%v: %v", value, err)
				}
			}
			for _, value := range []any{[]any{}, "", jsonx.Int(0), nil, false} {
				doc := jsonx.Obj("tun", jsonx.Obj(key, value))
				if err := ApplyJSONX(doc, config, ""); err != nil {
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
	config := jsonx.Obj("profile_id", "home",
		"mihomo", jsonx.Obj("linux_gateway", jsonx.Obj("enabled", true, "rule_set_ids", []any{})))
	values := []struct {
		value  any
		truthy bool
	}{
		{false, false}, {true, true}, {jsonx.Int(0), false}, {jsonx.Int(1), true}, {jsonx.Float(0), false},
		{"", false}, {"x", true}, {nil, false},
	}
	for _, key := range []string{"tun", "dns"} {
		want := msgTunNotObject
		if key == "dns" {
			want = msgDNSNotObject
		}
		for _, v := range values {
			doc := jsonx.Obj(key, v.value)
			err := ApplyJSONX(doc, config, "")
			var gwErr *Error
			switch {
			case v.truthy && (!errors.As(err, &gwErr) || gwErr.Message != want):
				t.Errorf("%s: %v: %v, want %q", key, v.value, err, want)
			case !v.truthy && (err != nil || !doc.GetObject("tun").GetBool("enable") ||
				doc.GetObject("dns").GetString("fake-ip-range") != DefaultFakeIPRange):
				t.Errorf("%s: %v: %v %v", key, v.value, err, doc)
			}
		}
	}
}
