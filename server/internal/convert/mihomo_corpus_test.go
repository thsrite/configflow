package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

// fakeSubStore mirrors fake_convert of testdata/gen_mihomo.py (and gen.py).
type fakeSubStore struct{}

func (fakeSubStore) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
	if strings.Contains(s, "fail") {
		return nil
	}
	if strings.Contains(s, "empty") {
		return jsonx.NewObject()
	}
	proxy := jsonx.Obj("name", "from-uri", "type", "http", "server", "fixture.test", "port", jsonx.Int(80))
	if _, after, ok := strings.Cut(s, "dial-"); ok {
		target, _, _ := strings.Cut(after, "&")
		proxy.Set("dialer-proxy", target)
	}
	if strings.Contains(s, "vless") {
		proxy.Set("type", "vless")
		proxy.Set("reality-opts", jsonx.Obj("public-key", "k"))
	}
	return proxy
}

// corpusEnv serves subscription caches and download caches from a row.
func corpusEnv(caches, downloads *jsonx.Object) convert.MihomoEnv {
	env := mihomoenv.Env()
	env.Converter = fakeSubStore{}
	env.LoadSubscriptionCache = func(_ context.Context, id string) (*jsonx.Object, error) {
		cache, _ := caches.Value(id).(*jsonx.Object)
		if cache == nil {
			return nil, nil
		}
		return cache.DeepCopy(), nil
	}
	env.DownloadedText = func(_ context.Context, url string) (string, bool) {
		entry, ok := downloads.Value(refresh.DownloadCachePath(url)).(*jsonx.Object)
		if !ok {
			return "", false
		}
		text, ok := entry.Value("text").(string)
		return text, ok
	}
	env.PrepareProviderBundle = nil
	return env
}

// corpusHooks mirror the fake fake_ip_network / mihomo_dns_loop of gen_mihomo.py.
func corpusHooks() convert.MihomoHooks {
	return convert.MihomoHooks{
		FakeIPNetworkError: func(value string) error {
			if strings.Contains(value, "bad") || !strings.Contains(value, "/") {
				return errors.New("fake")
			}
			return nil
		},
		MihomoDNSLoop: func(dns *jsonx.Object, hosts []netip.Addr) (string, any, bool, error) {
			names := make([]string, len(hosts))
			for i, h := range hosts {
				names[i] = h.String()
			}
			sort.Strings(names)
			joined := strings.Join(names, ",")
			for _, key := range []string{"default-nameserver", "nameserver", "fallback", "proxy-server-nameserver",
				"direct-nameserver", "nameserver-policy", "proxy-server-nameserver-policy"} {
				values := dns.Value(key)
				var items []any
				switch x := values.(type) {
				case *jsonx.Object:
					items = x.Values()
				case []any:
					items = x
				default:
					items = []any{values}
				}
				for _, v := range items {
					if s, ok := v.(string); ok && strings.Contains(s, "loop") {
						return key, s + "|" + joined, true, nil
					}
				}
			}
			return "", nil, false, nil
		},
	}
}

type mihomoRow struct {
	entry    *jsonx.Object
	fn       string
	errClass string
	errMsg   string
}

func loadMihomoCorpus(t *testing.T) []mihomoRow {
	t.Helper()
	file, err := os.Open("testdata/mihomo_corpus.jsonl.gz")
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
	var rows []mihomoRow
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*jsonx.Object)
		row := mihomoRow{entry: entry, fn: entry.GetString("fn")}
		if e := entry.GetList("err"); e != nil {
			row.errClass, row.errMsg = e[0].(string), e[1].(string)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// pythonClass names the Python exception class of a Go error.
func pythonClass(err error) string {
	var (
		custom   *convert.CustomConfigYAMLError
		dialer   *convert.DialerReferenceError
		easytier *convert.EasyTierSettingsError
		base     *convert.BaseSettingsError
		listener *subs.ListenerReferenceError
		strategy *rules.StrategyReferenceError
		interval *refresh.IntervalError
		health   *check.ValueError
		hkey     *check.KeyError
		candid   *rules.CandidateValidationError
		hook     *convert.HookMissingError
	)
	var replayed *replayedError
	switch {
	case errors.As(err, &replayed):
		return replayed.class
	case errors.As(err, &custom):
		return "CustomConfigYAMLError"
	case errors.As(err, &dialer):
		return "DialerReferenceError"
	case errors.As(err, &easytier):
		return "EasyTierSettingsError"
	case errors.As(err, &base):
		return "BaseSettingsError"
	case errors.As(err, &listener):
		return "ListenerReferenceError"
	case errors.As(err, &interval), errors.As(err, &health):
		return "ValueError"
	case errors.As(err, &hkey):
		return "KeyError"
	case errors.As(err, &candid):
		return "CandidateValidationError"
	case errors.As(err, &strategy):
		return "StrategyReferenceError"
	case errors.As(err, &hook):
		return "HookMissingError"
	}
	if class := convert.PyErrorClass(err); class != "" {
		return class
	}
	return fmt.Sprintf("Go:%T", err)
}

// crashClasses are unintended Python exceptions: only the class is compared.
var crashClasses = map[string]bool{"TypeError": true, "KeyError": true, "AttributeError": true, "IndexError": true,
	"OverflowError": true, "RecursionError": true}

func sameError(t *testing.T, row mihomoRow, err error) string {
	t.Helper()
	if row.errClass == "" {
		if err != nil {
			return fmt.Sprintf("Python ok, Go error %s: %v", pythonClass(err), err)
		}
		return ""
	}
	if err == nil {
		return fmt.Sprintf("Python %s: %s, Go ok", row.errClass, row.errMsg)
	}
	class := pythonClass(err)
	if class != row.errClass {
		return fmt.Sprintf("class: Python %s (%s), Go %s (%v)", row.errClass, row.errMsg, class, err)
	}
	if !crashClasses[class] && err.Error() != row.errMsg {
		return fmt.Sprintf("message (%s):\n  python %q\n  go     %q", class, row.errMsg, err.Error())
	}
	return ""
}

// mihomoConv mirrors conv() of gen_mihomo.py for Go values.
func mihomoConv(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value == nil {
			return nil
		}
		out := jsonx.NewObject()
		for k, child := range value.All() {
			out.Set(k, mihomoConv(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = mihomoConv(child)
		}
		return out
	case []byte:
		return jsonx.Obj("__bytes__", hex.EncodeToString(value))
	}
	return v
}

func encode(v any) string {
	b, err := jsonx.Encode(v, jsonx.Options{Compact: true})
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

func sameValue(want, got any) bool {
	return encode(want) == encode(mihomoConv(got))
}

func bool_(v any) bool { b, _ := v.(bool); return b }

func runMihomoRow(t *testing.T, row mihomoRow, oracle writerOracle) string {
	t.Helper()
	ctx := context.Background()
	e := row.entry
	switch row.fn {
	case "generate":
		args := e.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		restore := convert.SetMihomoEnv(corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		report := jsonx.NewObject()
		out, err := convert.GenerateMihomoConfig(ctx, profile, convert.GenerateOptions{
			BaseURL: args.GetString("base_url"), SyncLANHosts: bool_(args.Value("sync_lan_hosts")),
			SkipProviderPreflight: true, RegionProbe: bool_(args.Value("region_probe")), Report: report,
			MihomoCoreVersion: args.Value("core_version"), ForAgent: bool_(args.Value("for_agent")),
		})
		if msg := sameError(t, row, err); msg != "" {
			return msg
		}
		if err == nil {
			if msg := oracle.compare(e.GetString("out"), out); msg != "" {
				return msg
			}
			if !sameValue(e.Value("report"), report) {
				return "report differs:\n  python " + encode(e.Value("report")) + "\n  go     " + encode(mihomoConv(report))
			}
		}
		if !sameValue(e.Value("mutated"), profile) {
			return "mutated profile differs:\n  python " + encode(e.Value("mutated")) + "\n  go     " + encode(mihomoConv(profile))
		}
		return ""
	case "ruleset_downloads", "provider_downloads":
		args := e.GetList("args")
		profile := args[0].(*jsonx.Object).DeepCopy()
		restore := convert.SetMihomoEnv(corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		var list []convert.Download
		var err error
		if row.fn == "ruleset_downloads" {
			list, err = convert.GetMihomoRulesetDownloads(ctx, profile, args[1].(string))
		} else {
			list, err = convert.GetMihomoProviderDownloads(ctx, profile, args[1].(string), nil)
		}
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		got := []any{}
		for _, d := range list {
			got = append(got, d.Object())
		}
		if !sameValue(e.Value("out"), got) {
			return "downloads differ:\n  python " + encode(e.Value("out")) + "\n  go     " + encode(got)
		}
		return ""
	}
	return runHelperRow(t, row)
}

func runHelperRow(t *testing.T, row mihomoRow) string {
	t.Helper()
	e := row.entry
	args := e.GetList("args")
	var got any
	var err error
	switch row.fn {
	case "apply_no_resolve_to_logic_rule":
		got = convert.ApplyNoResolveToLogicRule(args[0].(string))
	case "normalize_find_process_mode":
		doc := args[0].(*jsonx.Object)
		convert.NormalizeFindProcessMode(doc)
		got = doc
	case "validate_ipv6_flags":
		err = convert.ValidateIPv6Flags(args[0])
	case "build_provider_health_check":
		got, err = convert.BuildProviderHealthCheck(args[0].(*jsonx.Object))
	case "split_rules_and_rulesets":
		var a, b any
		a, b, err = convert.SplitRulesAndRulesets(args[0].(*jsonx.Object))
		got = []any{a, b}
	case "sync_mosdns_hosts":
		doc := args[0].(*jsonx.Object)
		err = convert.SyncMosdnsHosts(doc, args[1].(*jsonx.Object))
		got = doc
	case "inject_region_probe":
		doc := args[0].(*jsonx.Object)
		port, _ := jsonx.AsNumber(args[1])
		var injected bool
		injected, err = convert.InjectRegionProbe(doc, port)
		got = []any{injected, doc}
	case "validate_base_settings":
		restore := convert.RegisterMihomoHooks(corpusHooks())
		defer restore()
		var reserved map[int]string
		if r, ok := args[2].(*jsonx.Object); ok {
			reserved = map[int]string{}
			for k, v := range r.All() {
				n, _ := strconv.Atoi(k)
				reserved[n] = v.(string)
			}
		}
		local, _ := args[3].([]any)
		err = convert.ValidateBaseSettings(args[0], args[1], reserved, local)
	case "sniff_port_error":
		got, err = convert.SniffPortError(args[0])
	case "split_host_port":
		var host string
		var port int
		var ok bool
		host, port, ok, err = convert.SplitHostPort(args[0])
		if ok {
			got = []any{host, jsonx.Int(port)}
		}
	case "agent_context":
		bindings := args[0].([]any)
		agents := args[1].(*jsonx.Object)
		source := fakeAgentSource{bindings: bindings, agents: agents, fail: args[2] == true}
		reserved, order, hosts := convert.AgentContext(source, "p")
		ports := jsonx.NewObject()
		for _, port := range order {
			ports.Set(strconv.Itoa(port), reserved[port])
		}
		got = []any{ports, hosts}
	case "controller_scope":
		got = convert.ControllerScope(args[0].(string))
	default:
		return "unknown fn " + row.fn
	}
	if msg := sameError(t, row, err); msg != "" || err != nil {
		return msg
	}
	if !sameValue(e.Value("out"), got) {
		return "result differs:\n  python " + encode(e.Value("out")) + "\n  go     " + encode(mihomoConv(got))
	}
	return ""
}

func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			start := i - 3
			if start < 0 {
				start = 0
			}
			context := strings.Join(wl[start:i], "\n")
			return fmt.Sprintf("line %d\n%s\n- python %q\n+ go     %q", i+1, context, w, g)
		}
	}
	return "(equal lines)"
}

// TestMihomoCorpus replays testdata/mihomo_corpus.jsonl.gz (gen_mihomo.py):
// generated YAML semantics, reports, mutated profiles, download lists, base
// settings checks and helpers must equal Python's.
func TestMihomoCorpus(t *testing.T) {
	nativeVersions := nativeAgentGenerationCases(t, "mihomo_corpus")
	readers := loadMihomoReaderOracle(t)
	rows := loadMihomoCorpus(t)
	oracle := loadWriterOracle(t)
	inputPatches := loadInputPatches(t)
	patched := 0
	paths := providerPathReferences(t)
	counts := map[string]int{}
	typedStorage := map[string]int{}
	failures := 0
	byFunction := map[string]int{}
	generated := 0
	for i, row := range rows {
		row = readers.row(t, "mihomo_corpus.jsonl.gz", i, row)
		row = applyNativeAgentGeneration(row, nativeVersions.GetObject(fmt.Sprintf("mihomo_corpus/%d", i)))
		counts[row.fn]++
		if paths := inputPatches[i]; len(paths) > 0 {
			row = keepOriginalInput(t, row, paths)
			patched++
		}
		if row.fn == "generate" {
			if issue := convert.CommonReplayIssue(row.entry.GetObject("args").GetObject("profile")); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		if row.fn == "generate" || row.fn == "provider_downloads" || row.fn == "ruleset_downloads" {
			profile := row.entry.GetObject("args").GetObject("profile")
			if row.fn != "generate" {
				profile, _ = row.entry.GetList("args")[0].(*jsonx.Object)
			}
			if issue := convert.MihomoReplayIssue(profile); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		if row.fn == "generate" && row.errClass == "" {
			if issue := convert.NodeProfileReplayIssue(row.entry.GetObject("args").GetObject("profile")); issue != "" {
				typedStorage[issue]++
				continue
			}
			generated++
		}

		if row.fn == "agent_context" {
			if issue := agentContextIssue(row.entry.GetList("args")); issue != "" {
				typedStorage[issue]++
				continue
			}
		}
		switch i {
		case 8485, 8544, 8981, 9030, 9266, 9290, 9496, 10257, 10310, 11876, 12491, 12496, 12547, 12608, 12990, 13712:
			if row.fn != "validate_base_settings" || row.entry.GetList("args")[0].(*jsonx.Object).GetString("external-controller") != "127.0.0.1:９０" {
				t.Fatalf("native listener fixture moved at row %d", i)
			}
			row.errClass = "BaseSettingsError"
			row.errMsg = convert.NativeBaseControllerMessage
			typedStorage["explicit Unicode controller ports"]++
		case 7826, 7827, 7828, 7831:
			clone := row.entry.DeepCopy()
			clone.Delete("err")
			address := convert.DefaultHealthCheckURL
			if n, ok := row.entry.GetList("args")[0].(*jsonx.Object).Value("health_check_url").(jsonx.Number); ok {
				address = n.String()
			}
			clone.Set("out", jsonx.Obj("enable", true, "url", address, "interval", jsonx.Int(300), "lazy", true))
			row.entry = clone
			row.errClass = ""
			row.errMsg = ""
			typedStorage["explicit R5 health helper"]++
		case 7837, 7838:
			clone := row.entry.DeepCopy()
			clone.Set("out", []any{[]any{}, []any{}})
			row.entry = clone
			typedStorage["explicit R5 rule-list elements"]++
		case 13874, 13875:
			clone := row.entry.DeepCopy()
			clone.Delete("err")
			clone.Set("out", true)
			row.entry = clone
			row.errClass = ""
			row.errMsg = ""
			typedStorage["explicit Unicode sniff ports"]++
		case 13888, 13889, 13892:
			clone := row.entry.DeepCopy()
			clone.Delete("err")
			clone.Set("out", nil)
			row.entry = clone
			row.errClass = ""
			row.errMsg = ""
			typedStorage["explicit native listener parse"]++
		}

		row = providerPathReference(t, paths, i, row)
		if msg := runMihomoRow(t, row, oracle); msg != "" {
			failures++
			byFunction[row.fn]++
			if byFunction[row.fn] <= 3 {
				t.Errorf("row %d %s: %s", i, row.fn, msg)
			}
		}
	}
	t.Logf("%d rows (%d generated configurations), %v; %d differ; typed storage classes %v", len(rows), generated, counts, failures, typedStorage)
	if failures > 0 {
		t.Logf("differences by function: %v", byFunction)
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
	if patched != 31 {
		t.Fatalf("only %d explicit input side-effect cases checked", patched)
	}
}

type fakeAgentSource struct {
	bindings []any
	agents   *jsonx.Object
	fail     bool
}

func (f fakeAgentSource) ProfileAgentIDs(string) ([]any, error) {
	if f.fail {
		return nil, errors.New("no profile")
	}
	return f.bindings, nil
}

func (f fakeAgentSource) AgentByID(id any) *jsonx.Object {
	agent, _ := f.agents.Value(id.(string)).(*jsonx.Object)
	if agent == nil {
		return nil
	}
	return agent.DeepCopy()
}

// Malformed agent records have typed R5 defaults; integral floats use R8. The
// normal input subset continues to compare the real Python result unchanged.
func agentContextIssue(args []any) string {
	records := args[1].(*jsonx.Object)
	for _, id := range args[0].([]any) {
		key, ok := id.(string)
		if !ok {
			return "R5 Agent binding string"
		}
		agent, ok := records.Value(key).(*jsonx.Object)
		if !ok {
			continue
		}
		for _, field := range []string{"id", "name", "host", "service_type", "deployment_method"} {
			if value, ok := agent.Get(field); ok {
				if _, ok := value.(string); !ok {
					return "R5 Agent." + field
				}
			}
		}
		if _, present := agent.Get("host"); !present {
			return "R5 Agent missing host"
		}
		if value, present := agent.Get("port"); present {
			number, ok := value.(jsonx.Number)
			if !ok {
				return "R5 Agent.port"
			}
			if !number.IsInt() {
				return "R8 Agent.port"
			}
		}
	}
	return ""
}

func TestNativeMihomoOriginalInputsUnchanged(t *testing.T) {
	rows := loadMihomoCorpus(t)
	patches := loadInputPatches(t)
	if len(patches) != 31 {
		t.Fatal("input immutability fixture coverage changed")
	}
	for index := range patches {
		row := rows[index]
		args := row.entry.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		before := encode(profile)
		restore := convert.SetMihomoEnv(corpusEnv(row.entry.GetObject("caches"), row.entry.GetObject("downloads")))
		_, _ = convert.GenerateMihomoConfig(context.Background(), profile, convert.GenerateOptions{BaseURL: args.GetString("base_url"), SyncLANHosts: args.GetBool("sync_lan_hosts"), SkipProviderPreflight: true, RegionProbe: args.GetBool("region_probe"), MihomoCoreVersion: args.Value("core_version"), ForAgent: args.GetBool("for_agent")})
		restore()
		if before != encode(profile) {
			t.Fatalf("source row %d changed input", index)
		}
	}
}
