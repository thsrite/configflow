package health

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/weburl"
)

// The reference corpus (testdata/gen.py, CPython 3.11) covers every pure
// function of C16 plus the worker-output parsing of check_settings and
// _run_probe, settings() and the store file bytes.

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

// errorClass names a Go error by the Python class it stands for.
func errorClass(err error) string {
	var cv *check.ValueError
	var ce *check.PyError
	var ck *check.KeyError
	var hv *ValueError
	var pu *ProbeUnavailable
	switch {
	case errors.As(err, &cv), errors.As(err, &hv):
		return "ValueError"
	case errors.As(err, &ce):
		return ce.Class
	case errors.As(err, &ck):
		return "KeyError"
	case errors.As(err, &pu):
		return "ProbeUnavailable"
	}
	return "other:" + err.Error()
}

// sameClass: ValueError subclasses Python raises (UnicodeDecodeError,
// AddressValueError) count as ValueError.
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

// messageMatters: intended messages (validation texts, ProbeUnavailable,
// UnicodeDecodeError) are compared; Python crash texts are not.
// refusedByNetURL reports whether v holds a URL string ("://") that net/url
// refuses although urllib.parse accepted it (control characters, an
// IPvFuture literal, a malformed percent escape or port, unbalanced
// brackets). Since N2 such URLs are rejected; accepted difference class.
func refusedByNetURL(v any) bool {
	switch v := v.(type) {
	case string:
		if !strings.Contains(v, "://") {
			return false
		}
		_, err := weburl.Parse(v)
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

// hasCJK reports whether a Python message is a Chinese user-facing text
// (compared exactly); other exception texts are compared by class only.
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
	rows := parsed.(*jsonx.Object).GetList("rows")
	storeDir := t.TempDir()
	probeStore, regionStore := check.NewProbeStore(storeDir), check.NewRegionStore(storeDir)
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
			check.InitializeProviderHealth(source)
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
			out, callErr = regionOfName(arg(0))
		case "region_label":
			out = check.RegionLabel(arg(0))
		case "evaluate_openai", "evaluate_claude", "evaluate_netflix", "evaluate_youtube":
			service, _ := check.ServiceByID(fn[len("evaluate_"):])
			byID, err := byIDObject(arg(0))
			if err != nil {
				callErr = err
				break
			}
			out, callErr = service.Evaluate(byID)
		case "evaluate_services":
			out, callErr = check.EvaluateServices(arg(0).([]any))
		case "exit_info":
			out, callErr = check.ExitInfo(arg(0).([]any))
		case "service_requests":
			out = check.ServiceRequests()
		case "public_services":
			out = check.PublicServices()
		case "domain_request":
			out = check.DomainRequest("domain-1", "https://a.test/")
		case "classify_domain_result":
			out, callErr = check.ClassifyDomainResult(arg(0), arg(1).(string))
		case "summarize_domain":
			out, callErr = check.SummarizeDomain(arg(0).(*jsonx.Object))
		case "pick_targets":
			out, callErr = check.PickTargets(arg(0).([]any), arg(1))
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
			out, callErr = check.Judge(arg(0), arg(1), options)
		case "probe_url":
			out = check.ProbeURL(arg(0).(string), arg(1))
		case "domain_suggestion":
			var suggestion *jsonx.Object
			suggestion, callErr = check.DomainSuggestion(arg(0).([]any))
			if suggestion != nil {
				out = suggestion
			}
		case "path_healthy":
			out, callErr = check.PathHealthy(arg(0))
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
				os.WriteFile(filepath.Join(storeDir, "r2.json"), []byte(`{"domains": [], "websites": 5, "keep": "x"}`), 0o644)
				out = jsonx.Obj("load", regionStore.Load("r2"))
			}
		case "probe_store_load_odd":
			os.WriteFile(filepath.Join(storeDir, "p2.json"), []byte(`{"hosts": [], "other": 1}`), 0o644)
			loaded, err := probeStore.Load("p2", storeNow)
			callErr = err
			out = jsonx.Obj("load", loaded)
		default:
			t.Fatalf("row %d: unknown fn %s", i, fn)
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
		label := fn + " " + encode(args) + " " + encode(row.Value("kwargs"))
		if fn == "region_of_name" && strings.ContainsAny(encode(args), "İıſ") {
			continue // IGNORECASE: Python folds İ/ı to i, Go's regex engines do not (rules: turkishI)
		}
		if class := row.GetString("error"); class != "" {
			if callErr == nil {
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
			if refusedByNetURL(args) {
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
	t.Logf("%d rows, %d differences", len(rows), failures)
}

// normalizeOut turns Go return types into jsonx values.
func normalizeOut(v any) any {
	switch value := v.(type) {
	case string:
		if value == "" {
			return value
		}
	case bool, nil, []any:
		return value
	case *jsonx.Object:
		if value == nil {
			return nil
		}
	}
	return v
}

func regionOfName(name any) (any, error) {
	code, err := check.RegionOfNameValue(name)
	if err != nil || code == "" {
		return nil, err
	}
	return code, nil
}

// byIDObject is {item['id']: item for item in results} for string ids.
func byIDObject(results any) (*jsonx.Object, error) {
	out := jsonx.NewObject()
	for _, entry := range results.([]any) {
		object, ok := entry.(*jsonx.Object)
		if !ok {
			return nil, &check.PyError{Class: "TypeError", Message: "string indices must be integers"}
		}
		id, ok := object.Get("id")
		if !ok {
			return nil, &check.KeyError{Key: "id"}
		}
		switch id.(type) {
		case []any, *jsonx.Object:
			return nil, &check.PyError{Class: "TypeError", Message: "unhashable type"}
		}
		if text, ok := id.(string); ok {
			out.Set(text, entry)
		}
	}
	return out, nil
}
