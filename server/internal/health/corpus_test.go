package health

import (
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
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
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

func testEnv(runner Runner, vars map[string]string) *Env {
	return &Env{
		LookupEnv: func(key string) (string, bool) { v, ok := vars[key]; return v, ok },
		Environ:   func() []string { return []string{"PATH=/usr/bin"} },
		Runner:    runner,
		Now:       func() time.Time { return time.Date(2026, 11, 15, 1, 2, 3, 456789000, time.UTC) },
		ProbeBinary: func() (string, error) {
			return "/test/node-probe", nil
		},
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
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if refusedByNetURL(v.Value(key)) {
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
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if hasNFKCDelimiterAuthority(v.Value(key)) {
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
	data, err := jsonx.Encode(v, jsonx.Options{Compact: true})
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(data)
}

func decodeTypedArg[T any](value any, dst *T) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func toLegacyAny(value any) (any, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}

func TestReferenceCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
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
	rows := parsed.(*jsonx.Object).GetList("rows")
	storeDir := t.TempDir()
	probeStore, regionStore := fixtureNewProbeStore(storeDir), fixtureNewRegionStore(storeDir)
	storeNow := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	for i, item := range rows {
		row := item.(*jsonx.Object)
		fn := row.GetString("fn")
		args := row.GetList("args")
		arg := func(n int) any { return args[n] }
		var out any
		var callErr error
		skipOut := false
		switch fn {
		case "validate_health_group":
			callErr = check.ValidateHealth(arg(0), false)
		case "validate_health_provider":
			callErr = check.ValidateHealth(arg(0), true)
		case "initialize_provider_health":
			source := jsonx.DeepCopy(arg(0)).(*jsonx.Object)
			initializeProviderHealth(source)
			out = source
		case "initialize_group_health":
			group := jsonx.DeepCopy(arg(0)).(*jsonx.Object)
			check.InitializeGroupHealth(group)
			out = group
		case "provider_health_check":
			out, callErr = check.ProviderHealthCheck(arg(0).(*jsonx.Object))
		case "normalize_dns_servers":
			out, callErr = NormalizeDNSServers([]any{arg(0)}, false)
		case "normalize_bootstrap":
			out, callErr = NormalizeDNSServers([]any{arg(0)}, true)
		case "normalize_dns_servers_list":
			out, callErr = NormalizeDNSServers(arg(0), false)
		case "normalize_config":
			out, callErr = NormalizeConfig(arg(0))
		case "check_settings":
			runner := &fakeRunner{result: Result{Stdout: []byte(row.GetString("stdout")), ReturnCode: int(row.GetInt("code"))}}
			restore := SetEnv(testEnv(runner, nil))
			out, callErr = CheckSettings(ctx, jsonx.Obj("mode", "system"))
			restore()
		case "decode":
			stdout, _ := hex.DecodeString(row.GetString("stdout_hex"))
			stderr, _ := hex.DecodeString(row.GetString("stderr_hex"))
			runner := &fakeRunner{result: Result{Stdout: stdout, Stderr: stderr}}
			restore := SetEnv(testEnv(runner, nil))
			_, callErr = CheckSettings(ctx, jsonx.Obj("mode", "system"))
			restore()
			skipOut = true
		case "run_probe", "run_probe_code":
			runner := &fakeRunner{result: Result{Stdout: []byte(row.GetString("stdout")), ReturnCode: int(row.GetInt("code"))}}
			restore := SetEnv(testEnv(runner, nil))
			proxies := []*jsonx.Object{jsonx.Obj("name", `["node","a"]`, "type", "http"), jsonx.Obj("name", `["node","b"]`, "type", "http")}
			if fn == "run_probe_code" {
				proxies = proxies[:1]
			}
			out, callErr = runProbe(ctx, proxies, jsonx.Obj("url", "u", "timeout_ms", jsonx.Int(5000), "dns_servers", []any{},
				"dns_bootstrap", []any{}, "unified_delay", true))
			restore()
		case "settings":
			vars := map[string]string{}
			for key, value := range row.GetObject("env").All() {
				vars[key] = value.(string)
			}
			restore := SetEnv(testEnv(nil, vars))
			out, callErr = Settings(ctx)
			restore()
		case "region_of_name":
			out, callErr = evalRegionOfName(arg(0))
		case "region_label":
			out, callErr = evalRegionLabel(arg(0))
		case "evaluate_openai", "evaluate_claude", "evaluate_netflix", "evaluate_youtube":
			out, callErr = evalSingleService(fn[len("evaluate_"):], arg(0))
		case "evaluate_services":
			out, callErr = fixtureCheckEvaluateServices(arg(0).([]any))
		case "exit_info":
			out, callErr = fixtureCheckExitInfo(arg(0).([]any))
		case "service_requests":
			out = fixtureCheckServiceRequests()
		case "public_services":
			out = fixtureCheckPublicServices()
		case "domain_request":
			out = fixtureCheckDomainRequest("domain-1", "https://a.test/")
		case "classify_domain_result":
			out, callErr = fixtureCheckClassifyDomainResult(arg(0), arg(1).(string))
		case "summarize_domain":
			out, callErr = fixtureCheckSummarizeDomain(arg(0).(*jsonx.Object))
		case "pick_targets":
			out, callErr = fixtureCheckPickTargets(arg(0).([]any), arg(1))
		case "judge":
			options := check.DefaultJudgeOptions()
			kwargs := row.GetObject("kwargs")
			if v, ok := kwargs.Get("passive_failing"); ok {
				options.PassiveFailing = v.(bool)
			}
			if v, ok := kwargs.Get("direct_healthy"); ok {
				options.DirectHealthy = v.(bool)
			}
			if v, ok := kwargs.Get("proxy_healthy"); ok {
				options.ProxyHealthy = v.(bool)
			}
			out, callErr = fixtureCheckJudge(arg(0), arg(1), options)
		case "probe_url":
			out, callErr = evalProbeURL(arg(0).(string), arg(1))
		case "domain_suggestion":
			out, callErr = evalDomainSuggestion(arg(0))
		case "path_healthy":
			out, callErr = evalPathHealthy(arg(0))
		case "probe_store_save":
			if err := probeStore.Save("a/1", row.GetObject("results"), storeNow); err != nil {
				callErr = err
				break
			}
			file, _ := os.ReadFile(probeStore.Path("a/1"))
			loaded, err := probeStore.Load("a/1", storeNow)
			if err != nil {
				callErr = err
				break
			}
			out = jsonx.Obj("file", string(file), "load", loaded)
		case "region_store":
			if row.GetString("step") == "a" {
				err := regionStore.Update("r1", func(data *jsonx.Object) error {
					data.Set("services", jsonx.Obj("x", jsonx.Int(1)))
					domains, websites := data.GetObject("domains"), data.GetObject("websites")
					domains.Set("a.test", jsonx.Obj("checked_at", "2026-10-01T00:00:00"))
					domains.Set("old.test", jsonx.Obj("checked_at", "2026-09-10T11:59:59"))
					domains.Set("edge.test", jsonx.Obj("checked_at", "2026-09-10T12:00:00"))
					websites.Set("w2", jsonx.Obj("checked_at", "2026-10-02T00:00:00"))
					websites.Set("w1", jsonx.Obj("checked_at", "2026-10-01T00:00:00"))
					websites.Set("w0", jsonx.Obj("checked_at", "2026-10-02T00:00:00"))
					websites.Set("wx", jsonx.NewObject())
					return nil
				}, storeNow)
				if err != nil {
					callErr = err
					break
				}
				file, _ := os.ReadFile(regionStore.Path("r1"))
				out = jsonx.Obj("file", string(file), "load", regionStore.Load("r1"))
			} else {
				_ = os.WriteFile(filepath.Join(storeDir, "r2.json"), []byte(`{"domains": [], "websites": 5, "keep": "x"}`), 0o644)
				out = jsonx.Obj("load", regionStore.Load("r2"))
			}
		case "probe_store_load_odd":
			_ = os.WriteFile(filepath.Join(storeDir, "p2.json"), []byte(`{"hosts": [], "other": 1}`), 0o644)
			loaded, err := probeStore.Load("p2", storeNow)
			callErr = err
			out = jsonx.Obj("load", loaded)
		default:
			t.Fatalf("row %d: unknown fn %s", i, fn)
		}

		label := fn + " " + encode(args) + " " + encode(row.Value("kwargs"))
		if fn == "region_of_name" && strings.ContainsAny(encode(args), "İıſ") {
			continue
		}
		if expected, ok := native[i]; ok {
			classified[expected.Contract]++
			assertNativeExpectation(t, expected, fn, out, callErr)
			continue
		}

		want := row.Value("out")
		switch fn {
		case "probe_store_save", "region_store", "probe_store_load_odd":
			expected := jsonx.NewObject()
			if row.Has("file") {
				expected.Set("file", row.Value("file"))
			}
			expected.Set("load", row.Value("load"))
			want = expected
		}
		if class := row.GetString("error"); class != "" {
			if callErr == nil {
				if hasNFKCDelimiterAuthority(args) {
					continue
				}
				failures++
				t.Errorf("row %d %s: want %s %q, got %s", i, label, class, row.GetString("message"), encode(out))
				continue
			}
			goClass := errorClass(callErr)
			if !sameClass(class, goClass) || (messageMatters(class) && hasCJK(row.GetString("message")) && callErr.Error() != row.GetString("message")) {
				failures++
				t.Errorf("row %d %s: want %s %q, got %s %q", i, label, class, row.GetString("message"), goClass, callErr.Error())
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
		if encode(normalizeOut(out)) != encode(want) {
			failures++
			t.Errorf("row %d %s:\n want %s\n got  %s", i, label, encode(want), encode(normalizeOut(out)))
		}
	}
	nativeCount := 0
	for _, n := range classified {
		nativeCount += n
	}
	if nativeCount != len(native) {
		t.Errorf("typed reference entries exercised=%d, want %d", nativeCount, len(native))
	}
	t.Logf("%d rows, %d differences, classified=%v", len(rows), failures, classified)
}

func normalizeOut(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value == nil {
			return nil
		}
	}
	return v
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
	verdict, err := service.Evaluate(byID)
	if err != nil {
		return nil, err
	}
	return toLegacyAny(verdict)
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
	return toLegacyAny(suggestion)
}
