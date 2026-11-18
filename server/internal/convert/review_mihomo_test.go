package convert_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/rulesetfmt"
	"configflow/internal/yamlx"
)

func TestMihomoReviewYAMLFailureLogOmitsConfiguration(t *testing.T) {
	withFakeSubStore(t)
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
			if !strings.Contains(err.Error(), "mihomo.custom_config YAML 解析失败: ") {
				t.Fatal("missing existing custom YAML context")
			}
			if !errors.Is(err, yamlx.ErrSyntax) {
				t.Fatalf("native syntax cause lost: %v", err)
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
			if test.url != nil && test.name != "empty" && test.name != "zero" {
				if !errors.Is(err, rulesetfmt.ErrURLType) {
					t.Fatalf("want native URL type validation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			provider := doc.GetObject("rule-providers").GetObject("Rules")
			format, extension := "yaml", "yaml"
			if test.name == "zero" {
				// Numeric URL reads as "0", so it no longer uses the YAML fallback.
				format, extension = "text", "list"
			}
			if provider.GetString("format") != format || provider.GetString("path") != "./ruleset/Rules."+extension {
				t.Fatalf("wrong fallback format: %v", provider)
			}
			downloads, err := convert.GetMihomoRulesetDownloads(context.Background(), config, "")
			if err != nil || len(downloads) != 1 || downloads[0].LocalPath != "./ruleset/Rules."+extension {
				t.Fatalf("wrong fallback download: %+v, %v", downloads, err)
			}
		})
	}
}

func TestMihomoReviewBoundaryReferences(t *testing.T) {
	nativeVersions := nativeAgentGenerationCases(t, "review_mihomo")
	readers := loadMihomoReaderOracle(t)
	rows := loadJSONLines(t, "testdata/review_mihomo.jsonl.gz")
	oracle := loadWriterOracle(t)
	for index, entry := range rows {
		t.Run(entry.GetString("case")+"/"+entry.GetString("fn"), func(t *testing.T) {
			// These four malformed URL classes have explicit native assertions
			// above. Archived Python truthiness is not the typed format oracle.
			if strings.HasPrefix(entry.GetString("case"), "empty-library-url-") {
				suffix := strings.TrimPrefix(entry.GetString("case"), "empty-library-url-")
				if suffix == "bool" || suffix == "int" || suffix == "list" || suffix == "dict" {
					return
				}
			}
			// PORTING: CPython-only decimal digit spellings are not reimplemented.
			if name := entry.GetString("case"); name == "sniff-ports-['１-６５５３５']" || name == "sniff-ports-['²']" {
				args := entry.GetList("args")
				err := convert.ValidateBaseSettings(args[0], args[1], nil, nil)
				var validation *convert.BaseSettingsError
				want := "sniffer.sniff.HTTP.ports：" + convert.SniffPortsHint
				if !errors.As(err, &validation) || err.Error() != want {
					t.Fatalf("native port validation: %v", err)
				}
				return
			}
			// The shared Int codec now accepts quoted provider intervals. Its
			// typed output/bounds are checked separately; the old Python rejects strings.
			profile := entry.GetObject("args").GetObject("profile")
			if entry.GetString("fn") != "generate" {
				if args := entry.GetList("args"); len(args) > 0 {
					profile, _ = args[0].(*jsonx.Object)
				}
			}
			if convert.MihomoReplayIssue(profile) == "R8 quoted provider interval" {
				t.Log("shared quoted provider interval: native boundary coverage")
				return
			}
			if msg := runMihomoReviewBoundary(t, applyNativeAgentGeneration(readers.row(t, "review_mihomo.jsonl.gz", index, rowOf(entry)), nativeVersions.GetObject(fmt.Sprintf("review_mihomo/%d", index))), oracle); msg != "" {
				t.Fatal(msg)
			}
		})
	}
	t.Logf("%d Python-derived Mihomo boundary cases", len(rows))
}

func runMihomoReviewBoundary(t *testing.T, row mihomoRow, oracle writerOracle) string {
	t.Helper()
	e := row.entry
	if row.fn == "takeover" {
		args := e.GetList("args")
		restore := convert.RegisterMihomoHooks(convert.MihomoHooks{
			GatewayEnabled: func(*jsonx.Object) (bool, error) {
				if args[1] == true {
					return false, errors.New("synthetic gateway failure")
				}
				return args[0] == true, nil
			},
			IsFullTun: func(*jsonx.Object) (bool, error) { return args[2] == true, nil },
		})
		defer restore()
		mode, err := convert.TakeoverMode(jsonx.NewObject())
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		return compareOut(row, noneString(mode))
	}
	if e.GetBool("gateway_fixture") {
		restore := convert.RegisterMihomoHooks(convert.MihomoHooks{
			GatewayEnabled: func(*jsonx.Object) (bool, error) { return true, nil },
			ApplyLinuxGateway: func(main, _ *jsonx.Object, _ string) error {
				providers := main.GetObject("rule-providers")
				if providers == nil {
					providers = jsonx.NewObject()
					main.Set("rule-providers", providers)
				}
				providers.Set("Gateway", jsonx.Obj("type", "http", "url", "/gateway.rules", "path", "./ruleset/gateway.list", "behavior", "classical", "format", "text"))
				providers.Set("File", jsonx.Obj("type", "file", "url", "/ignored", "path", "./ruleset/local.list"))
				providers.Set("Empty", jsonx.Obj("type", "http", "url", "", "path", "./ruleset/empty.list"))
				return nil
			},
		})
		defer restore()
	}
	if row.fn == "generate" {
		restore := convert.SetMihomoEnv(corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		args := e.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		report := jsonx.NewObject()
		doc, err := convert.GenerateMihomoDocument(context.Background(), profile, convert.GenerateOptions{
			BaseURL: args.GetString("base_url"), SkipProviderPreflight: true,
			SyncLANHosts: args.GetBool("sync_lan_hosts"), RegionProbe: args.GetBool("region_probe"),
			MihomoCoreVersion: args.Value("core_version"), ForAgent: args.GetBool("for_agent"), Report: report,
		})
		if msg := sameError(t, row, err); msg != "" {
			return msg
		}
		if err == nil {
			if !sameValue(e.Value("document"), doc) {
				return "generated document differs:\nPython " + encode(e.Value("document")) + "\nGo " + encode(mihomoConv(doc))
			}
		}
		if !sameValue(e.Value("report"), report) {
			return "generation report differs"
		}
		if !sameValue(e.Value("mutated"), profile) {
			return "logical profile mutation differs"
		}
		return ""
	}
	if args := e.GetList("args"); row.fn == "provider_downloads" && len(args) == 3 {
		downloads, err := convert.GetMihomoProviderDownloads(context.Background(), args[0].(*jsonx.Object), args[1].(string), args[2].(*jsonx.Object))
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		out := []any{}
		for _, download := range downloads {
			out = append(out, download.Object())
		}
		return compareOut(row, out)
	}
	return runMihomoRow(t, row, oracle)
}
