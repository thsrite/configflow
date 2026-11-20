package rules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/logx"
	"configflow/internal/model"
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
			err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, shared *model.Shared) error {
				behavior, format := tc.behavior, tc.format
				shared.RuleLibrary[0].Behavior = &behavior
				shared.RuleLibrary[0].Format = &format
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			path, err := rulefetch.CacheReadPath(s.Context(), model.RuleConfig{ID: "ads", Name: "Ads"}, nil)
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
	config, err := s.Repo.System(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	token := config.SystemConfig.RuleProxyToken
	if token == "" {
		t.Fatal("fixture lacks a live token")
	}
	name := "Operator " + token + " https://reader:synthetic-password@example.com/list?token=synthetic-query\nforged"
	err = s.Repo.UpdateShared(s.Context(), func(_ context.Context, shared *model.Shared) error {
		format, content := "yaml", "payload: {}\n"
		shared.RuleLibrary[0].Name = name
		shared.RuleLibrary[0].Format = &format
		shared.RuleLibrary[0].Content = &content
		return nil
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
