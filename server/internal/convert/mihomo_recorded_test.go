package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
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
	env          *jsonx.Object
	gatewayCalls int
	preflight    bool
	problems     []string
}

func (r *recordedEnv) mihomoEnv(base convert.MihomoEnv) convert.MihomoEnv {
	env := base
	conversions := r.env.GetObject("conversions")
	env.Converter = recordedConverter{conversions}
	caches := r.env.GetObject("caches")
	env.LoadSubscriptionCache = func(_ context.Context, id string) (*jsonx.Object, error) {
		cache, _ := caches.Value(id).(*jsonx.Object)
		if cache == nil {
			return nil, nil
		}
		return cache.DeepCopy(), nil
	}
	downloads := r.env.GetObject("downloads")
	env.DownloadedText = func(_ context.Context, url string) (string, bool) {
		entry, ok := downloads.Value(refresh.DownloadCachePath(url)).(*jsonx.Object)
		if !ok {
			return "", false
		}
		text, ok := entry.Value("text").(string)
		return text, ok
	}
	env.PrepareProviderBundle = func(context.Context, *jsonx.Object, *jsonx.Object) error {
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

func (r *recordedEnv) hooks() convert.MihomoHooks {
	return convert.MihomoHooks{
		ApplyLinuxGateway: func(mihomo, _ *jsonx.Object, baseURL string) error {
			calls := r.env.GetList("gateway")
			if r.gatewayCalls >= len(calls) {
				r.problems = append(r.problems, "Go applied the Linux gateway more often than Python")
				return nil
			}
			entry := calls[r.gatewayCalls].(*jsonx.Object)
			r.gatewayCalls++
			if before := encode(entry.Value("before")); before != encode(mihomoConv(mihomo)) {
				r.problems = append(r.problems, "Linux gateway input differs:\n  python "+before+"\n  go     "+encode(mihomoConv(mihomo)))
			}
			if entry.GetString("base_url") != baseURL {
				r.problems = append(r.problems, fmt.Sprintf("Linux gateway base_url: python %q go %q", entry.GetString("base_url"), baseURL))
			}
			if err := recordedError(entry.Value("err")); err != nil {
				return err
			}
			after := entry.GetObject("after").DeepCopy()
			for _, k := range mihomo.Keys() {
				mihomo.Delete(k)
			}
			for k, v := range after.All() {
				mihomo.Set(k, v)
			}
			return nil
		},
		GatewayEnabled: func(*jsonx.Object) (bool, error) {
			calls := r.env.GetList("gateway_enabled")
			if len(calls) == 0 {
				r.problems = append(r.problems, "Go read the gateway setting, Python did not")
				return false, nil
			}
			entry := calls[0].(*jsonx.Object)
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
		MihomoDNSLoop: func(dns *jsonx.Object, hosts []netip.Addr) (string, any, bool, error) {
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
				entry := item.(*jsonx.Object)
				if encode(entry.Value("dns")) != encode(mihomoConv(dns)) || encode(entry.Value("hosts")) != encode(names) {
					continue
				}
				out, ok := entry.Value("out").([]any)
				if !ok {
					return "", nil, false, nil
				}
				return out[0].(string), out[1], true, nil
			}
			r.problems = append(r.problems, "mihomo_dns_loop call not recorded: "+encode(mihomoConv(dns))+" "+encode(names))
			return "", nil, false, nil
		},
	}
}

type recordedConverter struct{ conversions *jsonx.Object }

func (c recordedConverter) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
	proxy, _ := c.conversions.Value(s).(*jsonx.Object)
	if proxy == nil {
		return nil
	}
	return proxy.DeepCopy()
}

func loadRecorded(t *testing.T) []*jsonx.Object {
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
	var rows []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func replayRow(t *testing.T, entry *jsonx.Object, oracle writerOracle) string {
	t.Helper()
	ctx := context.Background()
	rec := &recordedEnv{env: entry.GetObject("env")}
	restoreEnv := convert.SetMihomoEnv(rec.mihomoEnv(corpusEnv(nil, nil)))
	defer restoreEnv()
	restoreHooks := convert.RegisterMihomoHooks(rec.hooks())
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
		var report *jsonx.Object
		if args.Value("report") == true {
			report = jsonx.NewObject()
		}
		out, err := convert.GenerateMihomoConfig(ctx, profile, convert.GenerateOptions{
			BaseURL: args.GetString("base_url"), SyncLANHosts: args.Value("sync_lan_hosts") == true,
			SkipProviderPreflight: args.Value("preflight_providers") != true, RegionProbe: args.Value("region_probe") == true,
			Report: report, MihomoCoreVersion: args.Value("mihomo_core_version"), ForAgent: args.Value("for_agent") == true,
		})
		msg = sameError(t, row, err)
		if msg == "" && err == nil {
			if comparison := oracle.compare(entry.GetString("out"), out); comparison != "" {
				msg = comparison
			} else if report != nil && !sameValue(entry.Value("report"), report) {
				msg = "report differs:\n  python " + encode(entry.Value("report")) + "\n  go     " + encode(mihomoConv(report))
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
		list, err := convert.GetMihomoRulesetDownloads(ctx, args[0].(*jsonx.Object).DeepCopy(), args[1].(string))
		msg = sameError(t, row, err)
		if msg == "" && err == nil {
			got := []any{}
			for _, d := range list {
				got = append(got, d.Object())
			}
			if !sameValue(entry.Value("out"), got) {
				msg = "downloads differ:\n  python " + encode(entry.Value("out")) + "\n  go     " + encode(got)
			}
		}
	case "validate_base_settings":
		args := entry.GetList("args")
		var reserved map[int]string
		if r, ok := args[2].(*jsonx.Object); ok {
			reserved = map[int]string{}
			for k, v := range r.All() {
				n, _ := strconv.Atoi(k)
				reserved[n] = v.(string)
			}
		}
		local, _ := args[3].([]any)
		msg = sameError(t, row, convert.ValidateBaseSettings(args[0], args[1], reserved, local))
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
	oracle := loadWriterOracle(t)
	failures, skipped := 0, 0
	counts := map[string]int{}
	index := map[string]int{}
	for _, entry := range rows {
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
	t.Logf("%d recorded calls from %d Python tests %v; %d skipped (patched internals); %d differ",
		len(rows), len(index), counts, skipped, failures)
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
}

var _ = errors.New
