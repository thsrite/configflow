package convert_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
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
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/health/check"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// fakeSubStore mirrors fake_convert of testdata/gen_mihomo.py (and gen.py).
type fakeSubStore struct{}

func (fakeSubStore) ConvertProxy(_ context.Context, s string) (*yamlx.Node, error) {
	if strings.Contains(s, "fail") {
		return nil, nil
	}
	if strings.Contains(s, "empty") {
		return yamlx.Map(), nil
	}
	proxy := yamlx.Map(
		yamlx.Str("name"), yamlx.Str("from-uri"),
		yamlx.Str("type"), yamlx.Str("http"),
		yamlx.Str("server"), yamlx.Str("fixture.test"),
		yamlx.Str("port"), yamlx.Int(80),
	)
	if _, after, ok := strings.Cut(s, "dial-"); ok {
		target, _, _ := strings.Cut(after, "&")
		yamlx.Set(proxy, "dialer-proxy", yamlx.Str(target))
	}
	if strings.Contains(s, "vless") {
		yamlx.Set(proxy, "type", yamlx.Str("vless"))
		yamlx.Set(proxy, "reality-opts", yamlx.Map(yamlx.Str("public-key"), yamlx.Str("k")))
	}
	return proxy, nil
}

// corpusEnv serves subscription caches and download caches from a row.
func corpusEnv(caches, downloads *convert.JObj) convert.MihomoEnvironment {
	env := mihomoenv.NativeEnv()
	env.Converter = fakeSubStore{}
	env.LoadSubscriptionCache = func(_ context.Context, id string) (json.RawMessage, error) {
		cache, _ := caches.Value(id).(*convert.JObj)
		if cache == nil {
			return nil, nil
		}
		return convert.CompactJ(cache)
	}
	env.DownloadedText = func(_ context.Context, url string) (string, bool) {
		entry, ok := downloads.Value(refresh.DownloadCachePath(url)).(*convert.JObj)
		if !ok {
			return "", false
		}
		text, ok := entry.Value("text").(string)
		return text, ok
	}
	env.Preflight = nil
	return env
}

// corpusIntegrations mirrors the fake fake_ip_network / mihomo_dns_loop of gen_mihomo.py.
func corpusIntegrations() convert.MihomoIntegrations {
	return convert.MihomoIntegrations{
		FakeIPNetworkError: func(value string) error {
			if strings.Contains(value, "bad") || !strings.Contains(value, "/") {
				return errors.New("fake")
			}
			return nil
		},
		DNSLoop: func(dnsNode *yamlx.Node, hosts []netip.Addr) (string, string, bool, error) {
			dnsVal, _ := convert.JFromYAMLNode(dnsNode)
			dns, _ := dnsVal.(*convert.JObj)
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
				case *convert.JObj:
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
			return "", "", false, nil
		},
	}
}

type mihomoRow struct {
	entry    *convert.JObj
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
		value, err := convert.ParseJ(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*convert.JObj)
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
	case errors.Is(err, check.ErrMissingHealthCheck):
		return "KeyError"
	case errors.As(err, &candid):
		return "CandidateValidationError"
	case errors.As(err, &strategy):
		return "StrategyReferenceError"
	case errors.As(err, &hook):
		return "HookMissingError"
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
	case *convert.JObj:
		if value == nil {
			return nil
		}
		out := convert.NewJObj()
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
		return convert.JObjKV("__bytes__", hex.EncodeToString(value))
	}
	return v
}

func encode(v any) string {
	b, err := convert.CompactJ(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

func sameValue(want, got any) bool {
	return encode(want) == encode(mihomoConv(got))
}

func bool_(v any) bool { b, _ := v.(bool); return b }

func reportToJ(report *convert.GenerationReport) *convert.JObj {
	target := convert.NewJObj()
	if report == nil {
		return target
	}
	if report.SubscriptionHosts != nil {
		if wire, err := convert.JFromYAMLNode(report.SubscriptionHosts); err == nil {
			target.Set("subscription_hosts", wire)
		}
	}
	if report.SubscriptionDialers != nil {
		if wire, err := convert.JFromYAMLNode(report.SubscriptionDialers); err == nil {
			target.Set("subscription_dialers", wire)
		}
	}
	return target
}

func downloadsToJ(list []convert.ResourceDownload) []any {
	got := []any{}
	for _, d := range list {
		got = append(got, convert.JObjKV("name", d.Name, "url", d.URL, "local_path", d.LocalPath))
	}
	return got
}

func coreVersionRaw(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	return convert.CompactJ(v)
}

func validateBaseFromJ(document, previous any, reserved map[int]string, localAddresses []any) error {
	root, err := convert.YAMLNodeFromJ(document)
	if err != nil {
		return err
	}
	var before *yamlx.Node
	if previous != nil {
		before, err = convert.YAMLNodeFromJ(previous)
		if err != nil {
			return err
		}
	}
	hosts := []netip.Addr{}
	for _, raw := range localAddresses {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		if address, err := netip.ParseAddr(value); err == nil {
			hosts = append(hosts, address)
		}
	}
	return convert.ValidateBaseNode(root, before, reserved, hosts)
}

func runMihomoRow(t *testing.T, row mihomoRow, oracle writerOracle) string {
	t.Helper()
	ctx := context.Background()
	e := row.entry
	switch row.fn {
	case "generate":
		args := e.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		restore := convert.SetMihomoEnvironment(corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		cfg, err := convert.DecodeConfigJ(profile)
		var out string
		report := &convert.GenerationReport{}
		if err == nil {
			var version json.RawMessage
			if version, err = coreVersionRaw(args.Value("core_version")); err == nil {
				out, err = convert.MihomoConfig(ctx, cfg, convert.MihomoOptions{
					BaseURL: args.GetString("base_url"), SyncLANHosts: bool_(args.Value("sync_lan_hosts")),
					SkipProviderPreflight: true, RegionProbe: bool_(args.Value("region_probe")), Report: report,
					CoreVersion: version, ForAgent: bool_(args.Value("for_agent")),
				})
			}
		}
		if msg := sameError(t, row, err); msg != "" {
			return msg
		}
		if err == nil {
			if msg := oracle.compare(e.GetString("out"), out); msg != "" {
				return msg
			}
			reportObj := reportToJ(report)
			if !sameValue(e.Value("report"), reportObj) {
				return "report differs:\n  python " + encode(e.Value("report")) + "\n  go     " + encode(mihomoConv(reportObj))
			}
		}
		if !sameValue(e.Value("mutated"), profile) {
			return "mutated profile differs:\n  python " + encode(e.Value("mutated")) + "\n  go     " + encode(mihomoConv(profile))
		}
		return ""
	case "ruleset_downloads", "provider_downloads":
		args := e.GetList("args")
		profile := args[0].(*convert.JObj).DeepCopy()
		restore := convert.SetMihomoEnvironment(corpusEnv(e.GetObject("caches"), e.GetObject("downloads")))
		defer restore()
		cfg, err := convert.DecodeConfigJ(profile)
		var list []convert.ResourceDownload
		if err == nil {
			if row.fn == "ruleset_downloads" {
				list, err = convert.MihomoRulesetDownloads(ctx, cfg, args[1].(string))
			} else {
				list, err = convert.MihomoProviderDownloads(ctx, cfg, args[1].(string), nil)
			}
		}
		if msg := sameError(t, row, err); msg != "" || err != nil {
			return msg
		}
		got := downloadsToJ(list)
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
		var doc *yamlx.Node
		if doc, err = convert.YAMLNodeFromJ(args[0]); err == nil {
			if err = convert.NormalizeProcessMode(doc); err == nil {
				got, err = convert.JFromYAMLNode(doc)
			}
		}
	case "validate_ipv6_flags":
		var doc *yamlx.Node
		if doc, err = convert.YAMLNodeFromJ(args[0]); err == nil {
			err = convert.ValidateIPv6Node(doc)
		}
	case "build_provider_health_check":
		var record model.Subscription
		if record, err = convert.DecodeJ[model.Subscription](args[0]); err == nil {
			var node *yamlx.Node
			if node, err = convert.BuildProviderHealthForTest(record); err == nil {
				got, err = convert.JFromYAMLNode(node)
			}
		}
	case "split_rules_and_rulesets":
		var cfg *model.Config
		if cfg, err = convert.DecodeConfigJ(args[0].(*convert.JObj)); err == nil {
			var a, b []model.RuleConfig
			if a, b, err = convert.MihomoRuleLists(cfg); err == nil {
				ab, err1 := model.Marshal(a)
				bb, err2 := model.Marshal(b)
				if err1 != nil {
					err = err1
				} else if err2 != nil {
					err = err2
				} else {
					av, _ := convert.ParseJ(ab)
					bv, _ := convert.ParseJ(bb)
					got = []any{av, bv}
				}
			}
		}
	case "sync_mosdns_hosts":
		var doc *yamlx.Node
		var cfg *model.Config
		if cfg, err = convert.DecodeConfigJ(args[1].(*convert.JObj)); err == nil {
			if doc, err = convert.YAMLNodeFromJ(args[0]); err == nil {
				if err = convert.SyncMihomoHostsForTest(doc, cfg); err == nil {
					got, err = convert.JFromYAMLNode(doc)
				}
			}
		}
	case "inject_region_probe":
		var doc *yamlx.Node
		if doc, err = convert.YAMLNodeFromJ(args[0]); err == nil {
			n, ok := args[1].(json.Number)
			var port int64
			if ok && convert.IsIntNumber(n) {
				port, err = n.Int64()
			} else {
				err = fmt.Errorf("region probe port must be int")
			}
			if err == nil {
				var injected bool
				if injected, err = convert.AddRegionProbeForTest(doc, port); err == nil {
					var out any
					if out, err = convert.JFromYAMLNode(doc); err == nil {
						got = []any{injected, out}
					}
				}
			}
		}
	case "validate_base_settings":
		restore := convert.SetMihomoIntegrations(corpusIntegrations())
		defer restore()
		var reserved map[int]string
		if r, ok := args[2].(*convert.JObj); ok {
			reserved = map[int]string{}
			for k, v := range r.All() {
				n, _ := strconv.Atoi(k)
				reserved[n] = v.(string)
			}
		}
		local, _ := args[3].([]any)
		err = validateBaseFromJ(args[0], args[1], reserved, local)
	case "sniff_port_error":
		var node *yamlx.Node
		if node, err = convert.YAMLNodeFromJ(args[0]); err == nil {
			got = convert.SniffPortInvalid(node)
		}
	case "split_host_port":
		if text, ok := args[0].(string); ok {
			if host, port, ok := convert.ParseListener(text); ok {
				got = []any{host, convert.JInt(int64(port))}
			}
		}
	case "agent_context":
		bindings := args[0].([]any)
		records := args[1].(*convert.JObj)
		fail := args[2] == true
		var agents []model.Agent
		if !fail {
			for _, id := range bindings {
				key, ok := id.(string)
				if !ok {
					continue
				}
				raw := records.GetObject(key)
				if raw == nil {
					continue
				}
				agent, err := convert.DecodeJ[model.Agent](raw)
				if err != nil {
					continue
				}
				agents = append(agents, agent)
			}
		}
		reserved, order, hosts := convert.AgentPorts(agents)
		ports := convert.NewJObj()
		for _, port := range order {
			ports.Set(strconv.Itoa(port), reserved[port])
		}
		got = []any{ports, convert.JListOf(hosts)}
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
				profile, _ = row.entry.GetList("args")[0].(*convert.JObj)
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
			if row.fn != "validate_base_settings" || row.entry.GetList("args")[0].(*convert.JObj).GetString("external-controller") != "127.0.0.1:９０" {
				t.Fatalf("native listener fixture moved at row %d", i)
			}
			row.errClass = "BaseSettingsError"
			row.errMsg = convert.NativeBaseControllerMessage
			typedStorage["explicit Unicode controller ports"]++
		case 7826, 7827, 7828, 7831:
			clone := row.entry.DeepCopy()
			clone.Delete("err")
			address := convert.DefaultHealthCheckURL
			if n, ok := row.entry.GetList("args")[0].(*convert.JObj).Value("health_check_url").(json.Number); ok {
				address = n.String()
			}
			clone.Set("out", convert.JObjKV("enable", true, "url", address, "interval", convert.JInt(300), "lazy", true))
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

// Malformed agent records have typed R5 defaults; integral floats use R8. The
// normal input subset continues to compare the real Python result unchanged.
func agentContextIssue(args []any) string {
	records := args[1].(*convert.JObj)
	for _, id := range args[0].([]any) {
		key, ok := id.(string)
		if !ok {
			return "R5 Agent binding string"
		}
		agent, ok := records.Value(key).(*convert.JObj)
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
			number, ok := value.(json.Number)
			if !ok {
				return "R5 Agent.port"
			}
			if !convert.IsIntNumber(number) {
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
		cfg, err := convert.DecodeConfigJ(profile)
		if err != nil {
			t.Fatal(err)
		}
		before, err := model.Marshal(cfg.Profile)
		if err != nil {
			t.Fatal(err)
		}
		version, _ := coreVersionRaw(args.Value("core_version"))
		restore := convert.SetMihomoEnvironment(corpusEnv(row.entry.GetObject("caches"), row.entry.GetObject("downloads")))
		_, _ = convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{
			BaseURL: args.GetString("base_url"), SyncLANHosts: args.GetBool("sync_lan_hosts"),
			SkipProviderPreflight: true, RegionProbe: args.GetBool("region_probe"),
			CoreVersion: version, ForAgent: args.GetBool("for_agent"),
		})
		restore()
		after, err := model.Marshal(cfg.Profile)
		if err != nil || string(before) != string(after) {
			t.Fatalf("source row %d changed input", index)
		}
	}
}
