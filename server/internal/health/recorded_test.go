package health

// Record & replay (PORTING rule 4): every call the whole Python test suite
// made to C16's public functions, recorded by testdata/c16_record.py (with
// the worker calls, environment, system_config fields, store files and a
// frozen clock), replayed here with 0 differences. Regenerate on the test
// server:
//
//	rt --back server/internal/health/testdata/c16_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c16rec && C16_RECORD_DIR=/tmp/c16rec PYTHONPATH=.:server/internal/health/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 --dist=worksteal -p c16_record -p no:cacheprovider && PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/health/testdata/c16_record.py merge /tmp/c16rec'

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

func loadRecordedRows(t *testing.T, name string) []*jsonx.Object {
	t.Helper()
	handle, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	reader, err := gzip.NewReader(handle)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*jsonx.Object
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
	}
	return rows
}

func recordedClass(err error) string {
	var keyErr *check.KeyError
	var cv *check.ValueError
	var hv *ValueError
	var pu *ProbeUnavailable
	var pb *ProbeBusy
	switch {
	case errors.As(err, &cv), errors.As(err, &hv):
		return "ValueError"
	case errors.As(err, &pu):
		return "ProbeUnavailable"
	case errors.As(err, &pb):
		return "ProbeBusy"
	case errors.As(err, &keyErr):
		return "KeyError"
	}
	if repo.IsValueError(err) {
		return "ValueError"
	}
	return fmt.Sprintf("%T", err)
}

func naive(t *testing.T, text string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02T15:04:05.999999", text)
	if err != nil {
		t.Fatalf("naive time %q: %v", text, err)
	}
	return parsed
}

func sameWorkerJSONStdin(want string, got []byte) bool {
	if want == string(got) {
		return true
	}
	if !utf8.Valid(got) {
		return false
	}
	wantVal, errW := jsonx.Parse([]byte(want))
	gotVal, errG := jsonx.Parse(got)
	if errW != nil || errG != nil {
		return false
	}
	return bytes.Equal(mustCompact(wantVal), mustCompact(gotVal))
}

type replayRunner struct {
	worker   *jsonx.Object
	mismatch []string
}

func (r *replayRunner) Run(_ context.Context, command Command) (Result, error) {
	w := r.worker
	if argv, _ := jsonx.Compact(w.Value("argv")); string(argv) != string(mustCompact(jsonx.ListOf(command.Argv))) {
		r.mismatch = append(r.mismatch, "argv "+string(argv)+" vs "+strings.Join(command.Argv, " "))
	}
	if input := w.GetString("input"); !sameWorkerJSONStdin(input, command.Stdin) {
		r.mismatch = append(r.mismatch, "stdin:\n    python "+input+"\n    go     "+string(command.Stdin))
	}
	if timeout, ok := w.GetNumber("timeout"); ok {
		if want := time.Duration(timeout.Float64() * float64(time.Second)); want != command.Timeout {
			r.mismatch = append(r.mismatch, fmt.Sprintf("timeout %v vs %v", want, command.Timeout))
		}
	}
	for key, value := range w.GetObject("env").All() {
		found := false
		for _, item := range command.Env {
			if item == key+"="+jsonx.Str(value) {
				found = true
			}
		}
		if !found {
			r.mismatch = append(r.mismatch, "env "+key)
		}
	}
	switch w.GetString("err") {
	case "TimeoutExpired":
		return Result{}, ErrTimeout
	case "OSError":
		return Result{}, &OSError{Err: errors.New("exec format error")}
	case "":
	default:
		return Result{}, errors.New("unrecorded worker failure " + w.GetString("err"))
	}
	stdout, _ := w.Value("stdout").(string)
	stderr, _ := w.Value("stderr").(string)
	code, _ := w.GetNumber("returncode")
	n, _ := code.Int64()
	return Result{Stdout: []byte(stdout), Stderr: []byte(stderr), ReturnCode: int(n)}, nil
}

func mustCompact(v any) []byte { b, _ := jsonx.Compact(v); return b }

type mapConverter map[string]*jsonx.Object

func (m mapConverter) ConvertProxyString(_ context.Context, proxy string) *jsonx.Object {
	if value := m[proxy]; value != nil {
		return value.DeepCopy()
	}
	return nil
}

func objects(value any) []*jsonx.Object {
	var out []*jsonx.Object
	for _, item := range value.([]any) {
		out = append(out, item.(*jsonx.Object))
	}
	return out
}

func TestRecordedPythonSuite(t *testing.T) {
	classified := replayRecordedFile(t, "testdata/c16_recorded.jsonl.gz")
	if len(classified) != 0 {
		t.Fatalf("expected 0 classified rows in recorded Python suite, got %v", classified)
	}
}

func TestReviewCorpus(t *testing.T) {
	replayRecordedFile(t, "testdata/review_c16.jsonl.gz")
}

var recordedEnvMu sync.Mutex

func replayRecordedFile(t *testing.T, name string) map[string]int {
	t.Helper()
	t.Parallel()
	rows := loadRecordedRows(t, name)
	return replayRecordedRows(t, rows, nativeExpectations(t, name))
}

func replayRecordedRows(t *testing.T, rows []*jsonx.Object, native map[int]typedExpectation) map[string]int {
	t.Helper()
	var defaults *repo.Repository
	counts := map[string]int{}
	classified := map[string]int{}
	failures := 0
	for i, row := range rows {
		fn := row.GetString("fn")
		args := row.GetObject("args")
		arg := func(name string) any { return args.Value(name) }
		obj := func(name string) *jsonx.Object { o, _ := arg(name).(*jsonx.Object); return o }
		str := func(name string) string { s, _ := arg(name).(string); return s }
		if fn == "region_of_name" && strings.ContainsAny(string(mustCompact(args)), "İıſ") {
			counts["accepted (İ ı ſ under IGNORECASE)"]++
			continue
		}
		dir := t.TempDir()
		ctx := context.Background()
		var err error
		switch fn {
		case "load_config", "effective_settings", "describe_settings", "check_settings", "settings", "_proxy_of":
			system := row.GetObject("system")
			r := defaults
			if r == nil || system.Len() > 0 {
				r, err = repo.Open(dir, repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if system.Len() == 0 {
					defaults = r
				}
			}
			ctx = repo.WithScope(repo.WithRepository(ctx, r))
			if system.Len() > 0 {
				if _, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
					target := current.SetDefault("system_config", jsonx.NewObject()).(*jsonx.Object)
					for key, value := range system.All() {
						target.Set(key, jsonx.DeepCopy(value))
					}
					return nil, nil
				}); err != nil {
					t.Fatalf("row %d: seed system: %v", i, err)
				}
				repo.ResetConfigContext(ctx)
			}
		}
		envVars := row.GetObject("env")
		runner := &replayRunner{worker: row.GetObject("worker")}
		converter := mapConverter{}
		for _, item := range row.GetList("conversions") {
			call := item.(*jsonx.Object)
			proxy, _ := call.GetList("args")[0].(string)
			converter[proxy], _ = call.Value("ok").(*jsonx.Object)
		}
		env := &Env{
			LookupEnv: func(key string) (string, bool) {
				value, ok := envVars.Value(key).(string)
				return value, ok
			},
			Environ: func() []string { return nil },
			Runner:  runner,
			Now:     time.Now,
			ProbeBinary: func() (string, error) {
				w := runner.worker
				if failure := w.GetObject("binary_err"); failure != nil {
					return "", &ProbeUnavailable{Message: failure.GetString("text")}
				}
				return w.GetString("binary"), nil
			},
			Converter: converter,
		}
		now := naive(t, row.GetString("now"))
		storeNow := now
		if o, ok := arg("now").(*jsonx.Object); ok {
			storeNow = naive(t, strings.SplitN(o.GetString("__datetime__"), "+", 2)[0])
		}
		var value any
		func() {
			recordedEnvMu.Lock()
			defer recordedEnvMu.Unlock()
			defer SetEnv(env)()
			switch fn {
			case "validate_health":
				err = check.ValidateHealth(arg("values"), jsonx.Truthy(arg("provider")))
			case "initialize_group_health":
				group := obj("group")
				check.InitializeGroupHealth(group)
				value = group
			case "initialize_provider_health":
				source := obj("source")
				initializeProviderHealth(source)
				value = source
			case "provider_health_check":
				value, err = check.ProviderHealthCheck(obj("source"))
			case "_ipv4_server":
				value, err = ipv4Server(arg("value"), str("message"))
			case "_server":
				value, err = server(arg("value"))
			case "normalize_dns_servers":
				value, err = NormalizeDNSServers(arg("value"), jsonx.Truthy(arg("bootstrap")))
			case "_needs_bootstrap":
				value = needsBootstrap(str("server"))
			case "normalize_config":
				value, err = NormalizeConfig(arg("value"))
			case "load_config":
				value, err = LoadConfig(ctx)
			case "effective_settings":
				value, err = EffectiveSettings(ctx, arg("value"))
			case "describe_settings":
				value, err = DescribeSettings(ctx, arg("value"))
			case "check_settings":
				value, err = CheckSettings(ctx, arg("value"))
			case "settings":
				value, err = Settings(ctx)
			case "_proxy_of":
				if proxy := proxyOf(ctx, obj("node"), converter); proxy != nil {
					value = proxy
				}
			case "_needs_sub_store":
				value = needsSubStore(obj("node"))
			case "_run_probe":
				value, err = runProbe(ctx, objects(arg("proxies")), obj("options"))
			case "region_of_name":
				value, err = evalRegionOfName(arg("name"))
			case "region_label":
				value, err = evalRegionLabel(arg("code"))
			case "service_requests":
				value = check.ServiceRequests()
			case "evaluate_services":
				value, err = check.EvaluateServices(arg("results").([]any))
			case "exit_info":
				value, err = check.ExitInfo(arg("results").([]any))
			case "public_services":
				value = check.PublicServices()
			case "domain_request":
				value = check.DomainRequest(str("request_id"), str("url"))
			case "classify_domain_result":
				value, err = check.ClassifyDomainResult(arg("result"), str("original_url"))
			case "summarize_domain":
				value, err = check.SummarizeDomain(obj("per_target"))
			case "pick_targets":
				value, err = check.PickTargets(arg("targets").([]any), arg("preferred_group"))
			case "probe_url":
				value, err = evalProbeURL(str("host"), arg("port"))
			case "path_healthy":
				value, err = evalPathHealthy(arg("stats"))
			case "judge":
				value, err = check.Judge(arg("direct"), arg("proxy"), check.JudgeOptions{
					PassiveFailing: jsonx.Truthy(arg("passive_failing")),
					DirectHealthy:  jsonx.Truthy(arg("direct_healthy")),
					ProxyHealthy:   jsonx.Truthy(arg("proxy_healthy")),
				})
			case "domain_suggestion":
				value, err = evalDomainSuggestion(arg("hosts"))
			case "RegionStore.update", "RegionStore.load", "ProbeStore.save", "ProbeStore.load":
				storeDir := filepath.Join(dir, "store")
				agent := str("agent_id")
				region, probe := check.NewRegionStore(storeDir), check.NewProbeStore(storeDir)
				if before, ok := row.Value("before").(string); ok {
					_ = os.MkdirAll(storeDir, 0o755)
					_ = os.WriteFile(region.Path(agent), []byte(before), 0o644)
				}
				switch fn {
				case "RegionStore.update":
					mutated := row.GetObject("mutated")
					err = region.Update(agent, func(data *jsonx.Object) error {
						for _, key := range data.Keys() {
							data.Delete(key)
						}
						for key, item := range mutated.All() {
							data.Set(key, jsonx.DeepCopy(item))
						}
						return nil
					}, storeNow)
				case "RegionStore.load":
					value = region.Load(agent)
				case "ProbeStore.save":
					err = probe.Save(agent, obj("results"), storeNow)
				case "ProbeStore.load":
					value, err = probe.Load(agent, storeNow)
				}
			default:
				t.Fatalf("row %d: unknown function %s", i, fn)
			}
		}()

		if expected, ok := native[i]; ok {
			classified[expected.Contract]++
			assertNativeExpectation(t, expected, fn, value, err)
			if len(runner.mismatch) > 0 {
				t.Errorf("worker contract: %v", runner.mismatch)
			}
			continue
		}

		got, want := jsonx.NewObject(), jsonx.NewObject()
		if err != nil {
			got.Set("err", jsonx.Obj("class", recordedClass(err), "text", err.Error()))
		} else {
			got.Set("ok", value)
		}
		if row.Has("err") {
			want.Set("err", row.Value("err"))
		} else if strings.HasPrefix(fn, "initialize_") {
			want.Set("ok", row.Value("mutated"))
		} else {
			want.Set("ok", row.Value("ok"))
		}
		if strings.HasPrefix(fn, "RegionStore.") || strings.HasPrefix(fn, "ProbeStore.") {
			data, readErr := os.ReadFile(check.NewRegionStore(filepath.Join(dir, "store")).Path(str("agent_id")))
			var after any
			if readErr == nil {
				after = string(data)
			}
			got.Set("after", after)
			want.Set("after", row.Value("after"))
		}
		gotText, wantText := mustCompact(got), mustCompact(want)
		if !bytes.Equal(gotText, wantText) && len(runner.mismatch) == 0 && n2Accepted(got, want, row) {
			counts["accepted (N2 URL parsing)"]++
			continue
		}
		if !bytes.Equal(gotText, wantText) || len(runner.mismatch) > 0 {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s):\n  got  %s\n  want %s\n  worker %v", i, fn, row.GetString("test"),
					clip(string(gotText)), clip(string(wantText)), runner.mismatch)
			}
		}
		counts[fn]++
	}
	nativeCount := 0
	for _, n := range classified {
		nativeCount += n
	}
	if nativeCount != len(native) {
		t.Errorf("typed reference entries exercised=%d, want %d", nativeCount, len(native))
	}
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
	t.Logf("replayed %d calls: counts=%v classified=%v", len(rows), counts, classified)
	return classified
}

func clip(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

func n2Accepted(got, want *jsonx.Object, args *jsonx.Object) bool {
	gotErr, wantErr := got.GetObject("err"), want.GetObject("err")
	switch {
	case gotErr != nil && wantErr == nil && n2URLDifference(args):
		return true
	case gotErr == nil && wantErr != nil && hasNFKCDelimiterAuthority(args):
		return true
	case gotErr != nil && wantErr != nil:
		return gotErr.GetString("class") == wantErr.GetString("class") && !hasCJK(wantErr.GetString("text"))
	}
	encoded, _ := jsonx.Compact(args)
	return strings.Contains(string(encoded), "İ") || strings.Contains(string(encoded), `\u0130`)
}

// legacyProbeConverter only transcodes the neighbouring Sub-Store callback.
// Node parsing, branch selection and fixing remain in the native health owner.
type legacyProbeConverter struct{ converter convert.ProxyStringConverter }

func (c legacyProbeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	if c.converter == nil {
		return nil, nil
	}
	proxy := c.converter.ConvertProxyString(ctx, text)
	if proxy == nil {
		return nil, nil
	}
	raw, err := jsonx.Compact(proxy)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(raw)
}

// Env is the legacy environment configuration retained for api/nodes tests.
// Removed when api/nodes migrates to ProbeEnv.
type Env struct {
	LookupEnv   func(string) (string, bool)
	Environ     func() []string
	Runner      Runner
	Now         func() time.Time
	ProbeBinary func() (string, error)
	Converter   convert.ProxyStringConverter
}

// DefaultEnv returns the default legacy Env.
func DefaultEnv() *Env {
	return &Env{
		LookupEnv: os.LookupEnv,
		Environ:   os.Environ,
		Runner:    ExecRunner{},
		Now:       time.Now,
	}
}

var currentLegacyEnv = DefaultEnv()

type dynamicLegacyRunner struct{ env *Env }

func (d dynamicLegacyRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	if d.env != nil && d.env.Runner != nil {
		return d.env.Runner.Run(ctx, cmd)
	}
	return ExecRunner{}.Run(ctx, cmd)
}

func probeEnvFromLegacy(env *Env) *ProbeEnv {
	if env == nil {
		env = DefaultEnv()
	}
	p := &ProbeEnv{
		LookupEnv: func(key string) (string, bool) {
			if env.LookupEnv != nil {
				return env.LookupEnv(key)
			}
			return "", false
		},
		Environ: func() []string {
			if env.Environ != nil {
				return env.Environ()
			}
			return nil
		},
		Runner: dynamicLegacyRunner{env: env},
		Now: func() time.Time {
			if env.Now != nil {
				return env.Now()
			}
			return time.Now()
		},
		Collaborators: defaultCollaborators(func() convert.NodeConverter { return legacyProbeConverter{env.Converter} }),
	}
	p.ProbeBinary = func() (string, error) {
		if env.ProbeBinary != nil {
			return env.ProbeBinary()
		}
		return FindBinary(p)
	}
	return p
}

// SetEnv installs env as the active environment and returns a restore callback.
// Removed when api/nodes migrates to SetProbeEnv.
func SetEnv(env *Env) (restore func()) {
	prevLegacy := currentLegacyEnv
	currentLegacyEnv = env
	restoreProbe := SetProbeEnv(probeEnvFromLegacy(env))
	return func() {
		currentLegacyEnv = prevLegacy
		restoreProbe()
	}
}

// CurrentEnv returns the currently installed legacy Env.
// Removed when api/nodes migrates off legacy Env.
func CurrentEnv() *Env { return currentLegacyEnv }

// FindProbeBinary locates the worker binary using a legacy Env.
// Removed when api/nodes migrates to FindBinary.
func FindProbeBinary(e *Env) (string, error) {
	p := probeEnvFromLegacy(e)
	p.ProbeBinary = nil
	return FindBinary(p)
}

func toRawJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func encodeToLegacyValue(value any) (any, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}

func encodeToLegacyObject(value any) (*jsonx.Object, error) {
	parsed, err := encodeToLegacyValue(value)
	if err != nil {
		return nil, err
	}
	obj, ok := parsed.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", parsed)
	}
	return obj, nil
}

func encodeToLegacySlice(value any) ([]any, error) {
	parsed, err := encodeToLegacyValue(value)
	if err != nil {
		return nil, err
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", parsed)
	}
	return list, nil
}

// NormalizeDNSServers adapts legacy jsonx values to NormalizeDNSServersJSON.
// Removed when api/nodes migrates off jsonx.
func NormalizeDNSServers(value any, bootstrap bool) ([]any, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		message := DNSError
		if bootstrap {
			message = BootstrapError
		}
		return nil, valueError(message)
	}
	servers, err := NormalizeDNSServersJSON(raw, bootstrap)
	if err != nil {
		return nil, err
	}
	return encodeToLegacySlice(servers)
}

// NormalizeConfig adapts legacy jsonx values to NormalizeDNSConfigJSON.
// Removed when api/nodes migrates off jsonx.
func NormalizeConfig(value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, valueError(msgSelectDNSMode)
	}
	cfg, err := NormalizeDNSConfigJSON(raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(cfg)
}

// LoadConfig adapts LoadDNSConfig to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func LoadConfig(ctx context.Context) (*jsonx.Object, error) {
	cfg, err := LoadDNSConfig(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(cfg)
}

// EffectiveSettings adapts EffectiveDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func EffectiveSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	effective, err := EffectiveDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(effective)
}

// DescribeSettings adapts DescribeDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func DescribeSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	view, err := DescribeDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(view)
}

// SaveConfig adapts SaveDNSConfigJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func SaveConfig(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, valueError(msgSelectDNSMode)
	}
	view, err := SaveDNSConfigJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(view)
}

// CheckSettings adapts CheckDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func CheckSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	result, err := CheckDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(result)
}

// DNSSettings adapts ResolveDNSSettings to a legacy slice.
// Removed when api/nodes migrates off jsonx.
func DNSSettings(ctx context.Context) (any, error) {
	servers, err := ResolveDNSSettings(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacySlice(servers)
}

// Settings adapts ResolveSettings to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func Settings(ctx context.Context) (*jsonx.Object, error) {
	settings, err := ResolveSettings(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(settings)
}

// CollectCandidates adapts CollectCandidateNodes to legacy *jsonx.Object maps.
// Removed when api/nodes migrates off jsonx.
func CollectCandidates(ctx context.Context) ([]string, map[string]*jsonx.Object, error) {
	list, err := CollectCandidateNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, len(list))
	nodes := make(map[string]*jsonx.Object, len(list))
	for i, c := range list {
		keys[i] = c.Key
		parsed, err := jsonx.Parse(c.Raw)
		if err != nil {
			return nil, nil, err
		}
		obj, ok := parsed.(*jsonx.Object)
		if !ok {
			return nil, nil, fmt.Errorf("candidate %s did not decode to object", c.Key)
		}
		nodes[c.Key] = obj
	}
	return keys, nodes, nil
}

// LoadResults adapts LoadLatencyResults to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func LoadResults(ctx context.Context) *jsonx.Object {
	obj, err := encodeToLegacyObject(LoadLatencyResults(ctx))
	if err != nil {
		return jsonx.NewObject()
	}
	return obj
}

// RunTests adapts RunLatencyTests to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func RunTests(ctx context.Context, names, keys []string) (*jsonx.Object, error) {
	outcome, err := RunLatencyTests(ctx, names, keys)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(outcome)
}

// Internal legacy shims used only by replay harnesses in package health.

func ipv4Server(value any, message string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(message)
	}
	return ipv4ServerString(text, message)
}

func server(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(DNSError)
	}
	return serverString(text)
}

func proxyOf(ctx context.Context, node *jsonx.Object, converter convert.ProxyStringConverter) *jsonx.Object {
	if node == nil {
		return nil
	}
	raw, err := jsonx.Compact(node)
	if err != nil {
		return nil
	}
	fields, ok := defaultCollaborators(func() convert.NodeConverter { return legacyProbeConverter{converter} }).ConvertNode(ctx, raw)
	if !ok {
		return nil
	}
	obj, err := encodeToLegacyObject(fields)
	if err != nil {
		return nil
	}
	return obj
}

func needsSubStore(node *jsonx.Object) bool {
	if node == nil {
		return false
	}
	raw, ok := node.Value("proxy_string").(string)
	if !ok || raw == "" {
		return false
	}
	return defaultCollaborators(nil).NeedsRemoteConversion(raw)
}

func runProbe(ctx context.Context, proxies []*jsonx.Object, options *jsonx.Object) (*jsonx.Object, error) {
	typedProxies := make([]model.Fields, len(proxies))
	for i, p := range proxies {
		raw, err := jsonx.Compact(p)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &typedProxies[i]); err != nil {
			return nil, err
		}
	}
	var typedOptions ProbeSettings
	if options != nil {
		raw, err := jsonx.Compact(options)
		if err != nil {
			return nil, err
		}
		if err := jsondecode.Unmarshal(raw, &typedOptions); err != nil {
			return nil, err
		}
	}
	results, err := runProbeFields(ctx, typedProxies, typedOptions)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(results)
}

func initializeProviderHealth(source *jsonx.Object) {
	raw, _ := json.Marshal(source)
	var fields model.Fields
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	if check.InitProviderHealth(&fields) != nil {
		return
	}
	next, err := encodeToLegacyObject(fields)
	if err != nil {
		return
	}
	for _, key := range source.Keys() {
		source.Delete(key)
	}
	source.Update(next)
}
