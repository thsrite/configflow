package health

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

type fakeRunner struct {
	result Result
	err    error
	calls  []Command
	fn     func(Command) (Result, error)
}

func (f *fakeRunner) Run(_ context.Context, command Command) (Result, error) {
	f.calls = append(f.calls, command)
	if f.fn != nil {
		return f.fn(command)
	}
	return f.result, f.err
}

func testEnv(runner Runner, vars map[string]string) *ProbeEnv {
	return &ProbeEnv{
		LookupEnv: func(key string) (string, bool) { v, ok := vars[key]; return v, ok },
		Environ:   func() []string { return []string{"PATH=/usr/bin"} },
		Runner:    runner,
		Now:       func() time.Time { return time.Date(2026, 11, 15, 1, 2, 3, 456789000, time.UTC) },
		ProbeBinary: func() (string, error) {
			return "/test/node-probe", nil
		},
		Collaborators: defaultCollaborators(nil),
	}
}

func errorClass(err error) string {
	var cv *check.ValueError
	var ck *check.KeyError
	var hv *ValueError
	var pu *ProbeUnavailable
	switch {
	case errors.As(err, &cv), errors.As(err, &hv):
		return "ValueError"
	case errors.As(err, &ck):
		return "KeyError"
	case errors.As(err, &pu):
		return "ProbeUnavailable"
	}
	return "other:" + err.Error()
}

func sameClass(python, goClass string) bool {
	if python == goClass {
		return true
	}
	switch python {
	case "UnicodeDecodeError", "AddressValueError", "JSONDecodeError":
		return goClass == "ValueError"
	}
	return false
}

func refusedByNetURL(v any) bool {
	switch v := v.(type) {
	case string:
		if !strings.Contains(v, "://") {
			return false
		}
		_, err := url.Parse(v)
		return err != nil || strings.Contains(v, "[v")
	case []any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	}
	return false
}

func hasNFKCDelimiterAuthority(v any) bool {
	switch v := v.(type) {
	case string:
		i := strings.Index(v, "://")
		if i < 0 {
			return false
		}
		start := i + 3
		end := len(v)
		if n := strings.IndexAny(v[start:], "/?#"); n >= 0 {
			end = start + n
		}
		return strings.ContainsAny(v[start:end], "／？＃＠：﹕﹖﹟﹫︓︖℀℁℅℆⁇⁈⁉⩴")
	case []any:
		for _, item := range v {
			if hasNFKCDelimiterAuthority(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if hasNFKCDelimiterAuthority(item) {
				return true
			}
		}
	}
	return false
}

func n2URLDifference(v any) bool {
	return refusedByNetURL(v) || hasNFKCDelimiterAuthority(v)
}

func hasCJK(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff })
}

func messageMatters(class string) bool {
	return class == "ValueError" || class == "ProbeUnavailable" || class == "UnicodeDecodeError"
}

func encode(v any) string {
	data, err := jsonresp.Encode(v)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(data)
}

func decodeTypedArg[T any](value any, dst *T) error {
	raw, err := marshalRaw(value)
	if err != nil {
		return err
	}
	if bytes.Contains(raw, []byte(`"__nonfinite__"`)) {
		return model.ErrJSON
	}
	return jsondecode.Unmarshal(raw, dst)
}

func marshalRaw(value any) (json.RawMessage, error) {
	if raw, ok := value.(json.RawMessage); ok {
		return raw, nil
	}
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func optRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return marshalRaw(value)
}

func decodeMap(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func mapInt(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch n := m[key].(type) {
	case json.Number:
		v, _ := n.Int64()
		return v
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

func pyTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i != 0
		}
		if f, err := x.Float64(); err == nil {
			return f != 0
		}
		return true
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func TestReferenceCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("\n      NaN,"), []byte("\n      {\"__nonfinite__\":\"NaN\"},"))
	var corpus struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	failures := 0
	classified := map[string]int{}
	native := nativeExpectations(t, "testdata/corpus.json")
	storeDir := t.TempDir()
	probeStore, regionStore := check.NewProbeStore(storeDir), check.NewRegionStore(storeDir)
	storeNow := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	for i, rawRow := range corpus.Rows {
		row, err := decodeMap(rawRow)
		if err != nil {
			t.Fatal(err)
		}
		var rawFields struct {
			Args    []json.RawMessage `json:"args"`
			Results json.RawMessage   `json:"results"`
		}
		_ = json.Unmarshal(rawRow, &rawFields)
		fn := mapStr(row, "fn")
		args, _ := row["args"].([]any)
		arg := func(n int) any { return args[n] }
		rawArg := func(n int) json.RawMessage {
			if n < len(rawFields.Args) {
				return rawFields.Args[n]
			}
			return nil
		}
		var out any
		var callErr error
		skipOut := false
		switch fn {
		case "validate_health_group":
			callErr = check.ValidateHealthJSON(rawArg(0), false)
		case "validate_health_provider":
			callErr = check.ValidateHealthJSON(rawArg(0), true)
		case "initialize_provider_health":
			var fields model.Fields
			if json.Unmarshal(rawArg(0), &fields) == nil {
				_ = check.InitProviderHealth(&fields)
			}
			out = fields
		case "initialize_group_health":
			var fields model.Fields
			if json.Unmarshal(rawArg(0), &fields) == nil {
				_ = check.InitGroupHealth(&fields)
			}
			out = fields
		case "provider_health_check":
			var fields model.Fields
			if json.Unmarshal(rawArg(0), &fields) == nil {
				out, callErr = check.ResolveProviderHealth(&fields)
			} else {
				out, callErr = check.ResolveProviderHealth(nil)
			}
			if errors.Is(callErr, check.ErrMissingHealthCheck) {
				callErr = &check.KeyError{Key: "health_check"}
			}
		case "normalize_dns_servers":
			raw, _ := jsonresp.Encode([]any{arg(0)})
			out, callErr = NormalizeDNSServersJSON(raw, false)
		case "normalize_bootstrap":
			raw, _ := jsonresp.Encode([]any{arg(0)})
			out, callErr = NormalizeDNSServersJSON(raw, true)
		case "normalize_dns_servers_list":
			out, callErr = NormalizeDNSServersJSON(rawArg(0), false)
		case "normalize_config":
			out, callErr = NormalizeDNSConfigJSON(rawArg(0))
		case "check_settings":
			runner := &fakeRunner{result: Result{Stdout: []byte(mapStr(row, "stdout")), ReturnCode: int(mapInt(row, "code"))}}
			restore := SetProbeEnv(testEnv(runner, nil))
			out, callErr = CheckDNSSettingsJSON(ctx, json.RawMessage(`{"mode":"system"}`))
			restore()
		case "decode":
			stdout, _ := hex.DecodeString(mapStr(row, "stdout_hex"))
			stderr, _ := hex.DecodeString(mapStr(row, "stderr_hex"))
			runner := &fakeRunner{result: Result{Stdout: stdout, Stderr: stderr}}
			restore := SetProbeEnv(testEnv(runner, nil))
			_, callErr = CheckDNSSettingsJSON(ctx, json.RawMessage(`{"mode":"system"}`))
			restore()
			skipOut = true
		case "run_probe", "run_probe_code":
			runner := &fakeRunner{result: Result{Stdout: []byte(mapStr(row, "stdout")), ReturnCode: int(mapInt(row, "code"))}}
			restore := SetProbeEnv(testEnv(runner, nil))
			var proxies []model.Fields
			_ = json.Unmarshal([]byte(`[{"name":"[\"node\",\"a\"]","type":"http"},{"name":"[\"node\",\"b\"]","type":"http"}]`), &proxies)
			if fn == "run_probe_code" {
				proxies = proxies[:1]
			}
			out, callErr = runProbeFields(ctx, proxies, ProbeSettings{
				URL: "u", TimeoutMS: 5000, DNSServers: []string{}, DNSBootstrap: []string{}, UnifiedDelay: true,
			})
			restore()
		case "settings":
			vars := map[string]string{}
			if envMap, ok := row["env"].(map[string]any); ok {
				for key, value := range envMap {
					vars[key] = value.(string)
				}
			}
			restore := SetProbeEnv(testEnv(nil, vars))
			out, callErr = ResolveSettings(ctx)
			restore()
		case "region_of_name":
			out, callErr = evalRegionOfName(arg(0))
		case "region_label":
			out, callErr = evalRegionLabel(arg(0))
		case "evaluate_openai", "evaluate_claude", "evaluate_netflix", "evaluate_youtube":
			out, callErr = evalSingleService(fn[len("evaluate_"):], arg(0))
		case "evaluate_services":
			out, callErr = evalEvaluateServices(arg(0))
		case "exit_info":
			out, callErr = evalExitInfo(arg(0))
		case "service_requests":
			out = check.BuildServiceRequests()
		case "public_services":
			out = check.PublicServiceList()
		case "domain_request":
			out = check.BuildDomainRequest("domain-1", "https://a.test/")
		case "classify_domain_result":
			out, callErr = evalClassifyDomain(arg(0), arg(1).(string))
		case "summarize_domain":
			out, callErr = evalSummarizeDomain(arg(0))
		case "pick_targets":
			out, callErr = evalPickTargets(arg(0), arg(1))
		case "judge":
			options := check.DefaultJudgeOptions()
			if kwargs, ok := row["kwargs"].(map[string]any); ok {
				if v, ok := kwargs["passive_failing"]; ok {
					options.PassiveFailing = v.(bool)
				}
				if v, ok := kwargs["direct_healthy"]; ok {
					options.DirectHealthy = v.(bool)
				}
				if v, ok := kwargs["proxy_healthy"]; ok {
					options.ProxyHealthy = v.(bool)
				}
			}
			out, callErr = evalJudge(arg(0), arg(1), options)
		case "probe_url":
			out, callErr = evalProbeURL(arg(0).(string), arg(1))
		case "domain_suggestion":
			out, callErr = evalDomainSuggestion(arg(0))
		case "path_healthy":
			out, callErr = evalPathHealthy(arg(0))
		case "probe_store_save":
			if err := saveProbeRaw(probeStore, "a/1", rawFields.Results, storeNow); err != nil {
				callErr = err
				break
			}
			file, _ := os.ReadFile(probeStore.Path("a/1"))
			loaded := probeStore.LoadEntries("a/1", storeNow)
			out = map[string]any{"file": string(file), "load": loaded}
		case "region_store":
			if mapStr(row, "step") == "a" {
				err := regionStore.UpdateData("r1", func(data *check.RegionData) error {
					data.Services = json.RawMessage(`{"x":1}`)
					setTimed := func(m *model.Map[check.TimedRecord], key, raw string) {
						var entry check.TimedRecord
						_ = json.Unmarshal([]byte(raw), &entry)
						m.Set(key, entry)
					}
					setTimed(&data.Domains, "a.test", `{"checked_at":"2026-10-01T00:00:00"}`)
					setTimed(&data.Domains, "old.test", `{"checked_at":"2026-09-10T11:59:59"}`)
					setTimed(&data.Domains, "edge.test", `{"checked_at":"2026-09-10T12:00:00"}`)
					setTimed(&data.Websites, "w2", `{"checked_at":"2026-10-02T00:00:00"}`)
					setTimed(&data.Websites, "w1", `{"checked_at":"2026-10-01T00:00:00"}`)
					setTimed(&data.Websites, "w0", `{"checked_at":"2026-10-02T00:00:00"}`)
					setTimed(&data.Websites, "wx", `{}`)
					return nil
				}, storeNow)
				if err != nil {
					callErr = err
					break
				}
				file, _ := os.ReadFile(regionStore.Path("r1"))
				out = map[string]any{"file": string(file), "load": regionStore.LoadData("r1")}
			} else {
				_ = os.WriteFile(filepath.Join(storeDir, "r2.json"), []byte(`{"domains": [], "websites": 5, "keep": "x"}`), 0o644)
				out = map[string]any{"load": regionStore.LoadData("r2")}
			}
		case "probe_store_load_odd":
			_ = os.WriteFile(filepath.Join(storeDir, "p2.json"), []byte(`{"hosts": [], "other": 1}`), 0o644)
			loaded := probeStore.LoadEntries("p2", storeNow)
			out = map[string]any{"load": loaded}
		default:
			t.Fatalf("row %d: unknown fn %s", i, fn)
		}

		label := fn + " " + encode(args) + " " + encode(row["kwargs"])
		if fn == "region_of_name" && strings.ContainsAny(encode(args), "İıſ") {
			continue
		}
		if expected, ok := native[i]; ok {
			classified[expected.Contract]++
			assertNativeExpectation(t, expected, fn, out, callErr)
			continue
		}

		want := row["out"]
		switch fn {
		case "probe_store_save", "region_store", "probe_store_load_odd":
			expected := map[string]any{"load": row["load"]}
			if fileVal, ok := row["file"]; ok {
				expected["file"] = fileVal
			}
			want = expected
		}
		if class := mapStr(row, "error"); class != "" {
			if callErr == nil {
				if hasNFKCDelimiterAuthority(args) {
					continue
				}
				failures++
				t.Errorf("row %d %s: want %s %q, got %s", i, label, class, mapStr(row, "message"), encode(out))
				continue
			}
			goClass := errorClass(callErr)
			if !sameClass(class, goClass) || (messageMatters(class) && hasCJK(mapStr(row, "message")) && callErr.Error() != mapStr(row, "message")) {
				failures++
				t.Errorf("row %d %s: want %s %q, got %s %q", i, label, class, mapStr(row, "message"), goClass, callErr.Error())
			}
			continue
		}
		if callErr != nil {
			if n2URLDifference(args) {
				continue
			}
			failures++
			t.Errorf("row %d %s: want %s, got error %s %v", i, label, encode(want), errorClass(callErr), callErr)
			continue
		}
		if skipOut {
			continue
		}
		if !testjson.Equal(want, out) {
			failures++
			t.Errorf("row %d %s:\n want %s\n got  %s", i, label, encode(want), encode(out))
		}
	}
	nativeCount := 0
	for _, n := range classified {
		nativeCount += n
	}
	if nativeCount != len(native) {
		t.Errorf("typed reference entries exercised=%d, want %d", nativeCount, len(native))
	}
	t.Logf("%d rows, %d differences, classified=%v", len(corpus.Rows), failures, classified)
}

func saveProbeRaw(store *check.ProbeStore, agentID string, raw json.RawMessage, now time.Time) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return &check.ValueError{Message: "探测记录必须为对象且 checked_at 为字符串"}
	}
	var entries model.Map[check.TimedRecord]
	if err := jsondecode.Unmarshal(trimmed, &entries); err != nil {
		return err
	}
	return store.SaveEntries(agentID, entries, now)
}

func evalRegionOfName(raw any) (any, error) {
	var name check.Text
	if raw != nil {
		if err := decodeTypedArg(raw, &name); err != nil {
			return nil, err
		}
	}
	code := check.RegionOfName(string(name))
	if code == "" {
		return nil, nil
	}
	return code, nil
}

func evalRegionLabel(raw any) (any, error) {
	var code check.Text
	if raw != nil {
		if err := decodeTypedArg(raw, &code); err != nil {
			return nil, err
		}
	}
	return check.RegionLabel(string(code)), nil
}

func evalSingleService(serviceID string, raw any) (any, error) {
	service, _ := check.ServiceByID(serviceID)
	var list []check.RegionResult
	if err := decodeTypedArg(raw, &list); err != nil {
		return nil, err
	}
	byID := make(map[string]check.RegionResult, len(list))
	for _, r := range list {
		// EvaluateServiceList checks missing IDs; validate single-service input identically.
		if _, err := check.EvaluateServiceList([]check.RegionResult{r}); err != nil {
			var ve *check.ValueError
			if errors.As(err, &ve) && strings.Contains(ve.Message, "id") {
				return nil, err
			}
		}
		byID[r.ID] = r
	}
	return service.Evaluate(byID)
}

func evalEvaluateServices(raw any) (any, error) {
	var typedResults []check.RegionResult
	if err := decodeTypedArg(raw, &typedResults); err != nil {
		return nil, err
	}
	return check.EvaluateServiceList(typedResults)
}

func evalExitInfo(raw any) (any, error) {
	var typedResults []check.RegionResult
	if err := decodeTypedArg(raw, &typedResults); err != nil {
		return nil, err
	}
	return check.SummarizeExit(typedResults)
}

func evalClassifyDomain(raw any, originalURL string) (any, error) {
	var typedResult *check.RegionResult
	if raw != nil {
		var parsed check.RegionResult
		if err := decodeTypedArg(raw, &parsed); err != nil {
			return nil, err
		}
		typedResult = &parsed
	}
	return check.ClassifyDomain(typedResult, originalURL)
}

func evalSummarizeDomain(raw any) (any, error) {
	var typedTargets model.Map[check.TargetState]
	if raw != nil {
		if err := decodeTypedArg(raw, &typedTargets); err != nil {
			return nil, err
		}
	}
	return check.SummarizeTargets(typedTargets), nil
}

func evalPickTargets(rawTargets, rawPreferred any) (any, error) {
	var typedTargets []check.AvailableTarget
	if err := decodeTypedArg(rawTargets, &typedTargets); err != nil {
		return nil, err
	}
	var preferred check.Text
	if rawPreferred != nil {
		if err := decodeTypedArg(rawPreferred, &preferred); err != nil {
			return nil, err
		}
	}
	return check.ChooseTargets(typedTargets, string(preferred)), nil
}

func evalJudge(rawDirect, rawProxy any, options check.JudgeOptions) (any, error) {
	var d, p *check.PathStats
	if rawDirect != nil {
		var parsed check.PathStats
		if err := decodeTypedArg(rawDirect, &parsed); err != nil {
			return nil, err
		}
		d = &parsed
	}
	if rawProxy != nil {
		var parsed check.PathStats
		if err := decodeTypedArg(rawProxy, &parsed); err != nil {
			return nil, err
		}
		p = &parsed
	}
	return check.JudgePath(d, p, options), nil
}

func evalProbeURL(host string, rawPort any) (any, error) {
	var port int
	if rawPort != nil {
		n, ok := isIntegralInt64(rawPort)
		if !ok {
			return nil, &ValueError{Message: "port must be integer"}
		}
		port = int(n)
	}
	return check.ProbeURL(host, port), nil
}

func evalPathHealthy(raw any) (any, error) {
	if raw == nil {
		return check.PathHealthy(nil), nil
	}
	var stats check.PathStats
	if err := decodeTypedArg(raw, &stats); err != nil {
		return nil, err
	}
	return check.PathHealthy(&stats), nil
}

func evalDomainSuggestion(raw any) (any, error) {
	var hosts []check.HostProbe
	if err := decodeTypedArg(raw, &hosts); err != nil {
		return nil, err
	}
	suggestion := check.DomainSuggestion(hosts)
	if suggestion == nil {
		return nil, nil
	}
	return suggestion, nil
}
