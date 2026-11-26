package convert_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/logx"
	"configflow/internal/model"
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
			cfg := &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: "secret: [" + secret}}}
			_, err = convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{SkipProviderPreflight: true})
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
		{"null", nil}, {"empty", ""}, {"false", false}, {"zero", convert.JInt(0)},
		{"empty-list", []any{}}, {"empty-object", convert.NewJObj()},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := convert.JObjKV(
				"rule_library", []any{convert.JObjKV("id", "lib", "name", "Library", "url", test.url)},
				"rule_configs", []any{convert.JObjKV("id", "r", "name", "Rules", "itemType", "ruleset", "library_rule_id", "lib", "url", "/fallback.yaml", "policy", "DIRECT")},
			)
			cfg := mustDecodeConfig(t, config)
			doc, err := convert.MihomoDocument(context.Background(), cfg, convert.MihomoOptions{SkipProviderPreflight: true})
			// A library URL of another kind (false, [], {}) reads as missing
			// (plain records, O18 T1c), like null: the rule's own URL format
			// applies (R5 refused it with rulesetfmt.ErrURLType).
			if err != nil {
				t.Fatal(err)
			}
			provider := yamlx.Get(yamlx.Get(doc, "rule-providers"), "Rules")
			format, extension := "yaml", "yaml"
			if test.name == "zero" {
				// Numeric URL reads as "0", so it no longer uses the YAML fallback.
				format, extension = "text", "list"
			}
			if yamlx.Get(provider, "format").Value != format || yamlx.Get(provider, "path").Value != "./ruleset/Rules."+extension {
				t.Fatalf("wrong fallback format: %v", provider)
			}
			downloads, err := convert.MihomoRulesetDownloads(context.Background(), cfg, "")
			if err != nil || len(downloads) != 1 || downloads[0].LocalPath != "./ruleset/Rules."+extension {
				t.Fatalf("wrong fallback download: %+v, %v", downloads, err)
			}
		})
	}
}

func TestMihomoReviewBoundaryReferences(t *testing.T) {
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
				err := validateBaseFromJ(args[0], args[1], nil, nil)
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
					profile, _ = args[0].(*convert.JObj)
				}
			}
			if settings := profile.GetObject("mihomo"); settings != nil {
				if _, isBool := settings.Value("provider_update_interval").(bool); isBool {
					// Accepted O18 T1c difference: a stored boolean interval
					// reads as missing (default), Python refused True.
					return
				}
			}
			if convert.MihomoReplayIssue(profile) == "R8 quoted provider interval" {
				t.Log("shared quoted provider interval: native boundary coverage")
				return
			}
			if msg := runMihomoReviewBoundary(t, readers.row(t, "review_mihomo.jsonl.gz", index, rowOf(entry)), oracle); msg != "" {
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
		restore := convert.OverrideMihomoIntegrationsForTest(t, convert.MihomoIntegrations{
			GatewayEnabled: func(*model.Config) (bool, error) {
				if args[1] == true {
					return false, errors.New("synthetic gateway failure")
				}
				return args[0] == true, nil
			},
			FullTun: func(*model.Config) (bool, error) { return args[2] == true, nil },
		})
		defer restore()
		mode, err := convert.GatewayMode(&model.Config{Profile: &model.Profile{}})
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		return compareOut(row, noneString(mode))
	}
	if e.GetBool("gateway_fixture") {
		restore := convert.OverrideMihomoIntegrationsForTest(t, convert.MihomoIntegrations{
			GatewayEnabled: func(*model.Config) (bool, error) { return true, nil },
			ApplyGateway: func(main *yamlx.Node, _ *model.Config, _ string) error {
				providers := yamlx.Get(main, "rule-providers")
				if providers == nil {
					providers = yamlx.Map()
					yamlx.Set(main, "rule-providers", providers)
				}
				yamlx.Set(providers, "Gateway", yamlx.Map(
					yamlx.Str("type"), yamlx.Str("http"),
					yamlx.Str("url"), yamlx.Str("/gateway.rules"),
					yamlx.Str("path"), yamlx.Str("./ruleset/gateway.list"),
					yamlx.Str("behavior"), yamlx.Str("classical"),
					yamlx.Str("format"), yamlx.Str("text"),
				))
				yamlx.Set(providers, "File", yamlx.Map(
					yamlx.Str("type"), yamlx.Str("file"),
					yamlx.Str("url"), yamlx.Str("/ignored"),
					yamlx.Str("path"), yamlx.Str("./ruleset/local.list"),
				))
				yamlx.Set(providers, "Empty", yamlx.Map(
					yamlx.Str("type"), yamlx.Str("http"),
					yamlx.Str("url"), yamlx.Str(""),
					yamlx.Str("path"), yamlx.Str("./ruleset/empty.list"),
				))
				return nil
			},
		})
		defer restore()
	}
	if row.fn == "generate" {
		restore := convert.OverrideMihomoEnvironmentForTest(t, corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		args := e.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		cfg, err := convert.DecodeConfigJ(profile)
		report := &convert.GenerationReport{}
		var doc any
		if err == nil {
			var version json.RawMessage
			if version, err = coreVersionRaw(args.Value("core_version")); err == nil {
				var text string
				text, err = convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{
					BaseURL: args.GetString("base_url"), SkipProviderPreflight: true,
					SyncLANHosts: args.GetBool("sync_lan_hosts"), RegionProbe: args.GetBool("region_probe"),
					CoreVersion: version, ForAgent: args.GetBool("for_agent"), Report: report,
				})
				if err == nil {
					var parsed *yamlx.Node
					if parsed, err = yamlx.Parse([]byte(text)); err == nil {
						doc, err = convert.JFromYAMLNode(parsed)
					}
				}
			}
		}
		if msg := sameError(t, row, err); msg != "" {
			return msg
		}
		if err == nil {
			if !sameValue(e.Value("document"), doc) {
				return "generated document differs:\nPython " + encode(e.Value("document")) + "\nGo " + encode(mihomoConv(doc))
			}
		}
		reportObj := reportToJ(report)
		if !sameValue(e.Value("report"), reportObj) {
			return "generation report differs"
		}
		if !sameValue(e.Value("mutated"), profile) {
			return "logical profile mutation differs"
		}
		return ""
	}
	if args := e.GetList("args"); row.fn == "provider_downloads" && len(args) == 3 {
		cfg, err := convert.DecodeConfigJ(args[0].(*convert.JObj))
		var main *yamlx.Node
		if err == nil && args[2] != nil {
			main, err = convert.YAMLNodeFromJ(args[2])
		}
		var downloads []convert.ResourceDownload
		if err == nil {
			downloads, err = convert.MihomoProviderDownloads(context.Background(), cfg, args[1].(string), main)
		}
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		return compareOut(row, downloadsToJ(downloads))
	}
	return runMihomoRow(t, row, oracle)
}
