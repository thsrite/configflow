package convert_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

func TestMihomoReviewYAMLFailureLogOmitsConfiguration(t *testing.T) {
	for _, secret := range []string{"synthetic-controller-secret", "https://dns.example.com/query?token=synthetic-token"} {
		t.Run(secret, func(t *testing.T) {
			var logs bytes.Buffer
			root, err := logx.Setup(logx.Config{Level: logx.Error, Console: &logs})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				root.Close()
				if _, err := logx.Setup(logx.Config{Level: logx.Critical}); err != nil {
					t.Error(err)
				}
			})
			config := jsonx.Obj("mihomo", jsonx.Obj("custom_config", "secret: ["+secret))
			_, err = convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{SkipProviderPreflight: true})
			var custom *convert.CustomConfigYAMLError
			if !errors.As(err, &custom) {
				t.Fatalf("expected custom YAML rejection, got %v", err)
			}
			if !strings.Contains(err.Error(), secret) {
				t.Fatal("fixture did not exercise an error carrying the source text")
			}
			if strings.Contains(logs.String(), secret) {
				t.Fatal("ERROR log exposed custom YAML content")
			}
		})
	}
}

func TestMihomoReviewEmptyLibraryURLUsesRuleFormat(t *testing.T) {
	withFakeSubStore(t)
	for _, test := range []struct {
		name string
		url  any
	}{
		{"null", nil}, {"empty", ""}, {"false", false}, {"zero", jsonx.Int(0)},
		{"empty-list", []any{}}, {"empty-object", jsonx.NewObject()},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := jsonx.Obj(
				"rule_library", []any{jsonx.Obj("id", "lib", "name", "Library", "url", test.url)},
				"rule_configs", []any{jsonx.Obj("id", "r", "name", "Rules", "itemType", "ruleset", "library_rule_id", "lib", "url", "/fallback.yaml", "policy", "DIRECT")},
			)
			doc, err := convert.GenerateMihomoDocument(context.Background(), config, convert.GenerateOptions{SkipProviderPreflight: true})
			if err != nil {
				t.Fatal(err)
			}
			provider := doc.GetObject("rule-providers").GetObject("Rules")
			if provider.GetString("format") != "yaml" || provider.GetString("path") != "./ruleset/Rules.yaml" {
				t.Fatalf("wrong fallback format: %v", provider)
			}
			downloads, err := convert.GetMihomoRulesetDownloads(context.Background(), config, "")
			if err != nil || len(downloads) != 1 || downloads[0].LocalPath != "./ruleset/Rules.yaml" {
				t.Fatalf("wrong fallback download: %+v, %v", downloads, err)
			}
		})
	}
}
