package rules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/rulefetch"
)

func TestLocalDeliveryReviewWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, behavior, format, target, content, message string
	}{
		{"refused-yaml", "domain", "yaml", "mihomo", "payload: {}\n", fmt.Sprintf(msgLocalReview, "Ads", 1, "0")},
		{"many-lines", "classical", "text", "mihomo", strings.Repeat("UNKNOWN,item.example\n", 12), fmt.Sprintf(msgLocalReview, "Ads", 12, "1, 2, 3, 4, 5, 6, 7, 8, 9, 10")},
		{"normal", "domain", "text", "mihomo", "+.example.com\n", ""},
		{"other-target", "domain", "yaml", "surge", "payload: {}\n", ""},
		{"invalid-utf8", "domain", "text", "mihomo", "\xff\n", fmt.Sprintf(msgLocalUTF8, "Ads")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			_, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
				source := shared.GetList("rule_library")[0].(*jsonx.Object)
				source.Set("behavior", tc.behavior)
				source.Set("format", tc.format)
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			path, err := rulefetch.RuleCacheReadPath(s.Context(), jsonx.Obj("id", "ads", "name", "Ads"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			if _, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = logx.Setup(logx.Config{}) })
			for _, prefix := range []string{"/api", "/api/profiles/other"} {
				// The second request also exercises the normalizer's memoized result.
				logs.Reset()
				s.Get(prefix + "/rules/local/Ads?target=" + tc.target).WantStatus(200)
				if tc.message == "" {
					if logs.Len() != 0 {
						t.Fatalf("unexpected warning: %s", logs.String())
					}
				} else if strings.Count(logs.String(), " - WARNING - ") != 1 || !strings.Contains(logs.String(), " - WARNING - "+tc.message+"\n") {
					t.Fatalf("expected exactly one warning %q, got %s", tc.message, logs.String())
				}
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, []byte(tc.content)) {
					t.Fatal("delivery changed source cache", err)
				}
			}
		})
	}
}

func TestLocalDeliveryWarningRedactsRuleName(t *testing.T) {
	s := fixture(t)
	config, err := s.Repo.GetSystem(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	token := config.GetObject("system_config").GetString("rule_proxy_token")
	if token == "" {
		t.Fatal("fixture lacks a live token")
	}
	name := "Operator " + token + " https://reader:synthetic-password@example.com/list?token=synthetic-query\nforged"
	_, err = s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		source := shared.GetList("rule_library")[0].(*jsonx.Object)
		source.Set("name", name)
		source.Set("format", "yaml")
		source.Set("content", "payload: {}\n")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	if _, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{}) })
	if _, err := (&Service{}).Local(s.Context(), "default", name, "mihomo", ""); err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	for _, secret := range []string{token, "synthetic-password", "synthetic-query", "\nforged"} {
		if strings.Contains(text, secret) {
			t.Fatal("warning leaked credentials or forged a log line")
		}
	}
	if strings.Count(text, " - WARNING - ") != 1 || !strings.Contains(text, fmt.Sprintf(msgLocalReview, redact.Redacted, 1, "0")) {
		t.Fatal("redacted warning lost count or source line", text)
	}
}
