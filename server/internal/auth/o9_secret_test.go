package auth

import (
	"bytes"
	"strings"
	"testing"

	"configflow/internal/logx"
)

func TestO9FromEnvReplacesUnsafeSigningKeys(t *testing.T) {
	tests := []struct{ name, value, reason string }{
		{"unset", "", msgJWTSecretUnset},
		{"compose-placeholder", "your-unique-secret-key-at-least-32-chars", msgJWTSecretPlaceholder},
		{"docs-placeholder", "replace-with-at-least-32-random-characters", msgJWTSecretPlaceholder},
		{"short", "synthetic-short-secret", msgJWTSecretShort},
		{"31-bytes", strings.Repeat("a", 31), msgJWTSecretShort},
		{"32-bytes", strings.Repeat("b", 32), ""},
		{"utf8-bytes", strings.Repeat("密", 11), ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET_KEY", test.value)
			var output bytes.Buffer
			root, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &output})
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			first, second := FromEnv(), FromEnv()
			previous := Configure(first)
			defer Configure(previous)
			token, err := GenerateToken("synthetic-admin")
			if err != nil {
				t.Fatal(err)
			}
			Configure(second)
			verified, err := VerifyToken(token)
			if err != nil {
				t.Fatal(err)
			}
			if verified.Failed() != (test.reason != "") {
				t.Error("session persistence does not match the signing key policy")
			}
			if test.reason == "" {
				if first.SecretKey != test.value || second.SecretKey != test.value || output.Len() != 0 {
					t.Fatal("valid key changed or warned")
				}
				return
			}
			if len(first.SecretKey) < 32 || first.SecretKey == test.value || first.SecretKey == second.SecretKey {
				t.Error("unsafe key was reused")
			}
			for _, want := range []string{"WARNING", test.reason, "JWT_SECRET_KEY"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("missing %q in %s", want, output.String())
				}
			}
			for _, secret := range []string{test.value, first.SecretKey, second.SecretKey} {
				if secret != "" && strings.Contains(output.String(), secret) {
					t.Error("warning disclosed key")
				}
			}
		})
	}
}
