package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/yamlx"
)

// replayedError is an exception recorded from Python (class + message).
type replayedError struct{ class, message string }

func (e *replayedError) Error() string       { return e.message }
func (e *replayedError) PythonClass() string { return e.class }

func recordedError(v any) error {
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		return nil
	}
	return &replayedError{class: list[0].(string), message: list[1].(string)}
}

// recordedEnv replays what the collaborators answered during the Python call.
type recordedEnv struct {
	env          *convert.JObj
	gatewayCalls int
	preflight    bool
	problems     []string
}

func (r *recordedEnv) mihomoEnv(base convert.MihomoEnvironment) convert.MihomoEnvironment {
	env := base
	conversions := r.env.GetObject("conversions")
	env.Converter = recordedConverter{conversions}
	caches := r.env.GetObject("caches")
	env.LoadSubscriptionCache = func(_ context.Context, id string) (json.RawMessage, error) {
		cache, _ := caches.Value(id).(*convert.JObj)
		if cache == nil {
			return nil, nil
		}
		return convert.CompactJ(cache)
	}
	downloads := r.env.GetObject("downloads")
	env.DownloadedText = func(_ context.Context, url string) (string, bool) {
		entry, ok := downloads.Value(refresh.DownloadCachePath(url)).(*convert.JObj)
		if !ok {
			return "", false
		}
		text, ok := entry.Value("text").(string)
		return text, ok
	}
	env.Preflight = func(context.Context, *model.Config, *yamlx.Node) error {
		r.preflight = true
		switch value := r.env.Value("preflight").(type) {
		case string:
			return nil
		case []any:
			return recordedError(value)
		}
		r.problems = append(r.problems, "Go ran the provider preflight, Python did not")
		return nil
	}
	return env
}

func (r *recordedEnv) integrations() convert.MihomoIntegrations {
	return convert.MihomoIntegrations{
		ApplyGateway: func(mihomoNode *yamlx.Node, _ *model.Config, baseURL string) error {
			calls := r.env.GetList("gateway")
			if r.gatewayCalls >= len(calls) {
				r.problems = append(r.problems, "Go applied the Linux gateway more often than Python")
				return nil
			}
			entry := calls[r.gatewayCalls].(*convert.JObj)
			r.gatewayCalls++
			mihomoVal, _ := convert.JFromYAMLNode(mihomoNode)
			if before := encode(entry.Value("before")); before != encode(mihomoConv(mihomoVal)) {
				r.problems = append(r.problems, "Linux gateway input differs:\n  python "+before+"\n  go     "+encode(mihomoConv(mihomoVal)))
			}
			if entry.GetString("base_url") != baseURL {
				r.problems = append(r.problems, fmt.Sprintf("Linux gateway base_url: python %q go %q", entry.GetString("base_url"), baseURL))
			}
			if err := recordedError(entry.Value("err")); err != nil {
				return err
			}
			afterNode, err := convert.YAMLNodeFromJ(entry.GetObject("after"))
			if err != nil {
				return err
			}
			mihomoNode.Kind = afterNode.Kind
			mihomoNode.Tag = afterNode.Tag
			mihomoNode.Content = afterNode.Content
			return nil
		},
		GatewayEnabled: func(*model.Config) (bool, error) {
			calls := r.env.GetList("gateway_enabled")
			if len(calls) == 0 {
				r.problems = append(r.problems, "Go read the gateway setting, Python did not")
				return false, nil
			}
			entry := calls[0].(*convert.JObj)
			if err := recordedError(entry.Value("err")); err != nil {
				return false, err
			}
			return entry.Value("out") == true, nil
		},
		FakeIPNetworkError: func(value string) error {
			result, ok := r.env.GetObject("fake_ip").Get(value)
			if !ok {
				r.problems = append(r.problems, "fake_ip_network not called by Python for "+strconv.Quote(value))
				return nil
			}
			return recordedError(result)
		},
		DNSLoop: func(dnsNode *yamlx.Node, hosts []netip.Addr) (string, string, bool, error) {
			dnsVal, _ := convert.JFromYAMLNode(dnsNode)
			names := make([]any, len(hosts))
			text := make([]string, len(hosts))
			for i, h := range hosts {
				text[i] = h.String()
			}
			sort.Strings(text)
			for i, s := range text {
				names[i] = s
			}
			for _, item := range r.env.GetList("dns_loop") {
				entry := item.(*convert.JObj)
				if encode(entry.Value("dns")) != encode(mihomoConv(dnsVal)) || encode(entry.Value("hosts")) != encode(names) {
					continue
				}
				out, ok := entry.Value("out").([]any)
				if !ok {
					return "", "", false, nil
				}
				var target string
				if s, ok := out[1].(string); ok {
					target = s
				} else {
					target = fmt.Sprint(out[1])
				}
				return out[0].(string), target, true, nil
			}
			r.problems = append(r.problems, "mihomo_dns_loop call not recorded: "+encode(mihomoConv(dnsVal))+" "+encode(names))
			return "", "", false, nil
		},
	}
}

type recordedConverter struct{ conversions *convert.JObj }

func (c recordedConverter) ConvertProxy(_ context.Context, s string) (*yamlx.Node, error) {
	proxy, _ := c.conversions.Value(s).(*convert.JObj)
	if proxy == nil {
		return nil, nil
	}
	return convert.YAMLNodeFromJ(proxy)
}

func loadRecorded(t *testing.T) []*convert.JObj {
	t.Helper()
	file, err := os.Open("testdata/mihomo_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	var rows []*convert.JObj
	for scanner.Scan() {
		value, err := convert.ParseJ(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*convert.JObj))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func replayRow(t *testing.T, entry *convert.JObj, oracle writerOracle) string {
	t.Helper()
	ctx := context.Background()
	rec := &recordedEnv{env: entry.GetObject("env")}
	restoreEnv := convert.OverrideMihomoEnvironmentForTest(t, rec.mihomoEnv(corpusEnv(nil, nil)))
	defer restoreEnv()
	restoreHooks := convert.OverrideMihomoIntegrationsForTest(t, rec.integrations())
	defer restoreHooks()
	row := mihomoRow{entry: entry, fn: entry.GetString("fn")}
	if e := entry.GetList("err"); e != nil {
		row.errClass, row.errMsg = e[0].(string), e[1].(string)
	}
	var msg string
	switch row.fn {
	case "generate":
		args := entry.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		var report *convert.GenerationReport
		if args.Value("report") == true {
			report = &convert.GenerationReport{}
		}
		cfg, err := convert.DecodeConfigJ(profile)
		var out string
		if err == nil {
			var version json.RawMessage
			if version, err = coreVersionRaw(args.Value("mihomo_core_version")); err == nil {
				out, err = convert.MihomoConfig(ctx, cfg, convert.MihomoOptions{
					BaseURL: args.GetString("base_url"), SyncLANHosts: args.Value("sync_lan_hosts") == true,
					SkipProviderPreflight: args.Value("preflight_providers") != true, RegionProbe: args.Value("region_probe") == true,
					Report: report, CoreVersion: version, ForAgent: args.Value("for_agent") == true,
				})
			}
		}
		msg = sameError(t, row, err)
		if msg == "" && err == nil {
			if comparison := oracle.compare(entry.GetString("out"), out); comparison != "" {
				msg = comparison
			} else if report != nil {
				reportObj := reportToJ(report)
				if !sameValue(entry.Value("report"), reportObj) {
					msg = "report differs:\n  python " + encode(entry.Value("report")) + "\n  go     " + encode(mihomoConv(reportObj))
				}
			}
		}
		if msg == "" && entry.Has("mutated") && !sameValue(entry.Value("mutated"), profile) {
			msg = "mutated profile differs:\n  python " + encode(entry.Value("mutated")) + "\n  go     " + encode(mihomoConv(profile))
		}
		if msg == "" && rec.env.Value("preflight") != nil && !rec.preflight {
			msg = "Python ran the provider preflight, Go did not"
		}
	case "ruleset_downloads":
		args := entry.GetList("args")
		cfg, err := convert.DecodeConfigJ(args[0].(*convert.JObj).DeepCopy())
		var list []convert.ResourceDownload
		if err == nil {
			list, err = convert.MihomoRulesetDownloads(ctx, cfg, args[1].(string))
		}
		msg = sameError(t, row, err)
		if msg == "" && err == nil {
			got := downloadsToJ(list)
			if !sameValue(entry.Value("out"), got) {
				msg = "downloads differ:\n  python " + encode(entry.Value("out")) + "\n  go     " + encode(got)
			}
		}
	case "validate_base_settings":
		args := entry.GetList("args")
		var reserved map[int]string
		if r, ok := args[2].(*convert.JObj); ok {
			reserved = map[int]string{}
			for k, v := range r.All() {
				n, _ := strconv.Atoi(k)
				reserved[n] = v.(string)
			}
		}
		local, _ := args[3].([]any)
		msg = sameError(t, row, validateBaseFromJ(args[0], args[1], reserved, local))
	default:
		msg = "unknown fn " + row.fn
	}
	if msg == "" && len(rec.problems) > 0 {
		msg = strings.Join(rec.problems, "\n")
	}
	return msg
}

// TestMihomoRecordedPythonTests replays every generate_mihomo_config,
// get_mihomo_ruleset_downloads and validate_base_settings call the Python test
// suite makes (testdata/c18_record.py, a pytest plugin): each subtest is named
// by the Python test that made the call. The collaborators of other packages
// answer what they answered in Python (Sub-Store, caches, provider preflight,
// Linux gateway, gateway setting, fake_ip_network, mihomo_dns_loop); the
// generator itself must produce the same YAML semantics, report, errors and mutations.
func TestMihomoRecordedPythonTests(t *testing.T) {
	rows := loadRecorded(t)
	paths := recordedProviderReferences(t)
	usedPaths := 0
	oracle := loadWriterOracle(t)
	failures, skipped := 0, 0
	counts := map[string]int{}
	index := map[string]int{}
	for rowIndex, entry := range rows {
		if paths.Has(strconv.Itoa(rowIndex)) {
			usedPaths++
		}
		entry = recordedProviderReference(t, paths, rowIndex, entry)
		test := entry.GetString("test")
		counts[entry.GetString("fn")]++
		name := fmt.Sprintf("%s#%d", strings.TrimPrefix(test, "backend/"), index[test])
		index[test]++
		if patched := entry.GetString("patched"); patched != "" {
			// The Python test monkeypatched an internal of the generator
			// (Python mechanics); its observable behaviour is ported by hand.
			skipped++
			continue
		}
		if msg := replayRow(t, entry, oracle); msg != "" {
			failures++
			if failures <= 20 {
				t.Errorf("%s (%s): %s", name, entry.GetString("fn"), msg)
			}
		}
	}
	if usedPaths != paths.Len() {
		t.Fatal("unused recorded provider references")
	}
	t.Logf("%d recorded calls from %d Python tests %v; %d skipped (patched internals); %d differ",
		len(rows), len(index), counts, skipped, failures)
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
}

var _ = errors.New
