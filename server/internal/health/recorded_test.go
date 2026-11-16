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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func loadRecordedRows(t *testing.T, name string) []*jsonx.Object {
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

// recordedClass is type(err).__name__ of a replayed error.
func recordedClass(err error) string {
	var keyErr *check.KeyError
	switch e := err.(type) {
	case *check.PyError:
		return e.Class
	case *check.ValueError, *ValueError:
		return "ValueError"
	case *ProbeUnavailable:
		return "ProbeUnavailable"
	case *ProbeBusy:
		return "ProbeBusy"
	case *pyTypeError:
		return "TypeError"
	}
	if errors.As(err, &keyErr) {
		return "KeyError"
	}
	if repo.IsValueError(err) {
		return "ValueError"
	}
	return fmt.Sprintf("%T", err)
}

// naive is a recorded naive datetime.now() as a time with those wall fields.
func naive(t *testing.T, text string) time.Time {
	parsed, err := time.Parse("2006-01-02T15:04:05.999999", text)
	if err != nil {
		t.Fatalf("naive time %q: %v", text, err)
	}
	return parsed
}

// replayRunner answers the recorded subprocess.run call and checks what Go
// sends: argv, stdin bytes, timeout and the two Go runtime variables.
type replayRunner struct {
	worker   *jsonx.Object
	mismatch []string
}

func (r *replayRunner) Run(_ context.Context, command Command) (Result, error) {
	w := r.worker
	if argv, _ := jsonx.Compact(w.Value("argv")); string(argv) != string(mustCompact(jsonx.ListOf(command.Argv))) {
		r.mismatch = append(r.mismatch, "argv "+string(argv)+" vs "+strings.Join(command.Argv, " "))
	}
	if input := w.GetString("input"); input != string(command.Stdin) {
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
		return Result{}, &OSError{errors.New("exec format error")}
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

func noneIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestRecordedPythonSuite(t *testing.T) { replayRecordedFile(t, "testdata/c16_recorded.jsonl.gz") }

// TestReviewCorpus replays the review corpus (testdata/gen_review_c16.py:
// edge inputs called through the same recorder, real Python on the test
// server).
func TestReviewCorpus(t *testing.T) { replayRecordedFile(t, "testdata/review_c16.jsonl.gz") }

func replayRecordedFile(t *testing.T, name string) {
	rows := loadRecordedRows(t, name)
	counts := map[string]int{}
	failures := 0
	for i, row := range rows {
		fn := row.GetString("fn")
		args := row.GetObject("args")
		arg := func(name string) any { return args.Value(name) }
		obj := func(name string) *jsonx.Object { o, _ := arg(name).(*jsonx.Object); return o }
		str := func(name string) string { s, _ := arg(name).(string); return s }
		dir := t.TempDir()
		r, err := repo.Open(dir, repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
		if system := row.GetObject("system"); system != nil && system.Len() > 0 {
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
					return "", &ProbeUnavailable{failure.GetString("text")}
				}
				return w.GetString("binary"), nil
			},
			Converter: converter,
		}
		restore := SetEnv(env)
		now := naive(t, row.GetString("now"))
		storeNow := now
		if o, ok := arg("now").(*jsonx.Object); ok {
			storeNow = naive(t, strings.SplitN(o.GetString("__datetime__"), "+", 2)[0])
		}
		var value any
		switch fn {
		case "validate_health":
			err = check.ValidateHealth(arg("values"), jsonx.Truthy(arg("provider")))
		case "initialize_group_health":
			group := obj("group")
			check.InitializeGroupHealth(group)
			value = group
		case "initialize_provider_health":
			source := obj("source")
			check.InitializeProviderHealth(source)
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
			var code string
			code, err = check.RegionOfNameValue(arg("name"))
			value = noneIfEmpty(code)
		case "region_label":
			value = check.RegionLabel(arg("code"))
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
			value = check.ProbeURL(str("host"), arg("port"))
		case "path_healthy":
			value, err = check.PathHealthy(arg("stats"))
		case "judge":
			value, err = check.Judge(arg("direct"), arg("proxy"), check.JudgeOptions{
				PassiveFailing: jsonx.Truthy(arg("passive_failing")), DirectHealthy: jsonx.Truthy(arg("direct_healthy")),
				ProxyHealthy: jsonx.Truthy(arg("proxy_healthy"))})
		case "domain_suggestion":
			value, err = check.DomainSuggestion(arg("hosts").([]any))
			if value == (*jsonx.Object)(nil) {
				value = nil
			}
		case "RegionStore.update", "RegionStore.load", "ProbeStore.save", "ProbeStore.load":
			storeDir := filepath.Join(dir, "store")
			agent := str("agent_id")
			region, probe := check.NewRegionStore(storeDir), check.NewProbeStore(storeDir)
			if before, ok := row.Value("before").(string); ok {
				os.MkdirAll(storeDir, 0o755)
				os.WriteFile(region.Path(agent), []byte(before), 0o644)
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
		restore()
		got, want := jsonx.NewObject(), jsonx.NewObject()
		if err != nil {
			got.Set("err", jsonx.Obj("class", recordedClass(err), "text", err.Error()))
		} else if row.Has("mutated") && strings.HasPrefix(fn, "initialize_") {
			got.Set("ok", value)
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
		if !bytes.Equal(gotText, wantText) || len(runner.mismatch) > 0 {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s):\n  got  %s\n  want %s\n  worker %v", i, fn, row.GetString("test"),
					clip(string(gotText)), clip(string(wantText)), runner.mismatch)
			}
		}
		counts[fn]++
	}
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
	t.Logf("replayed %d calls: %v", len(rows), counts)
}

func clip(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}
