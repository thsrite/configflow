package installertest

import (
	"strings"
	"testing"
)

func TestBootstrapBaselineKeepsOtherFieldsAndMalformedProofs(t *testing.T) {
	token := strings.Repeat("x", 43)
	for _, test := range []struct{ name, before, after string }{
		{"json", "{\n  \"name\": \"synthetic\"\n}\n", "{\n  \"name\": \"synthetic\",\n  \"registration_token\": \"" + token + "\"\n}\n"},
		{"compose", "services:\n  agent:\n    environment:\n      - ENABLE_MIHOMO=true\n", "services:\n  agent:\n    environment:\n      - AGENT_MIHOMO_REGISTRATION_TOKEN=" + token + "\n      - ENABLE_MIHOMO=true\n"},
		{"run", "docker run \\\n  image\n", "docker run \\\n  -e AGENT_MIHOMO_REGISTRATION_TOKEN=" + token + " \\\n  image\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := BootstrapBaseline(test.after); got != test.before {
				t.Fatalf("baseline %q, want %q", got, test.before)
			}
			malformed := strings.ReplaceAll(test.after, token, "short")
			if BootstrapBaseline(malformed) != malformed {
				t.Fatal("masked malformed credential")
			}
			unrelated := strings.ReplaceAll(test.after, "REGISTRATION_TOKEN", "UNKNOWN_TOKEN")
			unrelated = strings.ReplaceAll(unrelated, "registration_token", "unknown_token")
			if BootstrapBaseline(unrelated) != unrelated {
				t.Fatal("masked unknown field")
			}
			changed := strings.ReplaceAll(test.after, "synthetic", "changed")
			if test.name == "json" && BootstrapBaseline(changed) == test.before {
				t.Fatal("masked another config field")
			}
		})
	}
}
