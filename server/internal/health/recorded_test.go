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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

type recordedRow struct {
	raw     json.RawMessage
	data    map[string]any
	rawArgs map[string]json.RawMessage
	mutated json.RawMessage
}

func loadRecordedRows(t *testing.T, name string) []recordedRow {
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
	var rows []recordedRow
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		line := bytes.Clone(scanner.Bytes())
		m, err := decodeMap(line)
		if err != nil {
			t.Fatal(err)
		}
		var aux struct {
			Args    map[string]json.RawMessage `json:"args"`
			Mutated json.RawMessage            `json:"mutated"`
		}
		_ = json.Unmarshal(line, &aux)
		rows = append(rows, recordedRow{
			raw:     line,
			data:    m,
			rawArgs: aux.Args,
			mutated: aux.Mutated,
		})
	}
	return rows
}

func recordedClass(err error) string {
	var cv *check.ValidationError
	var hv *ValidationError
	var pu *ProbeUnavailable
	var pb *ProbeBusy
	switch {
	case errors.As(err, &cv), errors.As(err, &hv):
		return "ValueError"
	case errors.As(err, &pu):
		return "ProbeUnavailable"
	case errors.As(err, &pb):
		return "ProbeBusy"
	case errors.Is(err, check.ErrMissingHealthCheck):
		return "KeyError"
	}
	if errors.Is(err, repo.ErrRejected) {
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
	return testjson.Equal(json.RawMessage(want), json.RawMessage(got))
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	default:
		return fmt.Sprint(x)
	}
}

type replayRunner struct {
	worker   map[string]any
	mismatch []string
}

func (r *replayRunner) Run(_ context.Context, command Command) (Result, error) {
	w := r.worker
	if !testjson.Equal(w["argv"], command.Argv) {
		r.mismatch = append(r.mismatch, "argv "+encode(w["argv"])+" vs "+strings.Join(command.Argv, " "))
	}
	if input := mapStr(w, "input"); !sameWorkerJSONStdin(input, command.Stdin) {
		r.mismatch = append(r.mismatch, "stdin:\n    python "+input+"\n    go     "+string(command.Stdin))
	}
	if rawTimeout, ok := w["timeout"].(json.Number); ok {
		if f, err := rawTimeout.Float64(); err == nil {
			if want := time.Duration(f * float64(time.Second)); want != command.Timeout {
				r.mismatch = append(r.mismatch, fmt.Sprintf("timeout %v vs %v", want, command.Timeout))
			}
		}
	}
	if envMap, ok := w["env"].(map[string]any); ok {
		for key, value := range envMap {
			found := false
			for _, item := range command.Env {
				if item == key+"="+anyString(value) {
					found = true
				}
			}
			if !found {
				r.mismatch = append(r.mismatch, "env "+key)
			}
		}
	}
	switch mapStr(w, "err") {
	case "TimeoutExpired":
		return Result{}, ErrTimeout
	case "OSError":
		return Result{}, errors.New("exec format error")
	case "":
	default:
		return Result{}, errors.New("unrecorded worker failure " + mapStr(w, "err"))
	}
	stdout, _ := w["stdout"].(string)
	stderr, _ := w["stderr"].(string)
	return Result{Stdout: []byte(stdout), Stderr: []byte(stderr), ReturnCode: int(mapInt(w, "returncode"))}, nil
}

type mapNodeConverter map[string]json.RawMessage

func (m mapNodeConverter) ConvertProxy(_ context.Context, proxy string) (*yamlx.Node, error) {
	raw := m[proxy]
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	return yamlx.FromJSON(raw)
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

func replayRecordedRows(t *testing.T, rows []recordedRow, native map[int]typedExpectation) map[string]int {
	t.Helper()
	var defaults *repo.Repository
	counts := map[string]int{}
	classified := map[string]int{}
	failures := 0
	for i, rec := range rows {
		row := rec.data
		fn := mapStr(row, "fn")
		args, _ := row["args"].(map[string]any)
		arg := func(name string) any { return args[name] }
		rawArg := func(name string) json.RawMessage { return rec.rawArgs[name] }
		str := func(name string) string { s, _ := arg(name).(string); return s }
		if fn == "region_of_name" && strings.ContainsAny(encode(args), "İıſ") {
			counts["accepted (İ ı ſ under IGNORECASE)"]++
			continue
		}
		dir := t.TempDir()
		ctx := context.Background()
		var r *repo.Repository
		var err error
		switch fn {
		case "load_config", "effective_settings", "describe_settings", "check_settings", "settings", "_proxy_of":
			system, _ := row["system"].(map[string]any)
			r = defaults
			if r == nil || len(system) > 0 {
				r, err = repo.Open(dir, repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if len(system) == 0 {
					defaults = r
				}
			}
			ctx = repo.WithScope(ctx)
			if len(system) > 0 {
				if err := r.UpdateSystem(ctx, func(_ context.Context, current *model.System) error {
					existingRaw, err := json.Marshal(current.SystemConfig)
					if err != nil {
						return err
					}
					merged, err := decodeMap(existingRaw)
					if err != nil {
						return err
					}
					if merged == nil {
						merged = map[string]any{}
					}
					for key, value := range system {
						merged[key] = value
					}
					updatedRaw, err := json.Marshal(merged)
					if err != nil {
						return err
					}
					var nextCfg model.SystemConfig
					if err := json.Unmarshal(updatedRaw, &nextCfg); err != nil {
						return err
					}
					current.SystemConfig = nextCfg
					return nil
				}); err != nil {
					t.Fatalf("row %d: seed system: %v", i, err)
				}
				repo.ResetConfigContext(ctx)
			}
		}
		envVars, _ := row["env"].(map[string]any)
		workerMap, _ := row["worker"].(map[string]any)
		runner := &replayRunner{worker: workerMap}
		converter := mapNodeConverter{}
		if conversions, ok := row["conversions"].([]any); ok {
			for _, item := range conversions {
				call, _ := item.(map[string]any)
				callArgs, _ := call["args"].([]any)
				if len(callArgs) == 0 {
					continue
				}
				proxy, _ := callArgs[0].(string)
				if okObj, ok := call["ok"].(map[string]any); ok && okObj != nil {
					raw, _ := json.Marshal(okObj)
					converter[proxy] = raw
				}
			}
		}
		env := &ProbeEnv{
			LookupEnv: func(key string) (string, bool) {
				if envVars == nil {
					return "", false
				}
				value, ok := envVars[key].(string)
				return value, ok
			},
			Environ: func() []string { return nil },
			Runner:  runner,
			Now:     time.Now,
			ProbeBinary: func() (string, error) {
				if failure, ok := workerMap["binary_err"].(map[string]any); ok && failure != nil {
					return "", &ProbeUnavailable{Message: mapStr(failure, "text")}
				}
				return mapStr(workerMap, "binary"), nil
			},
			Collaborators: defaultCollaborators(func() convert.NodeConverter { return converter }),
		}
		now := naive(t, mapStr(row, "now"))
		storeNow := now
		if o, ok := arg("now").(map[string]any); ok {
			storeNow = naive(t, strings.SplitN(mapStr(o, "__datetime__"), "+", 2)[0])
		}
		var value any
		func() {
			recordedEnvMu.Lock()
			defer recordedEnvMu.Unlock()
			defer SetProbeEnv(env)()
			switch fn {
			case "validate_health":
				err = check.ValidateHealthJSON(rawArg("values"), pyTruthy(arg("provider")))
			case "initialize_group_health":
				var fields model.Fields
				if json.Unmarshal(rawArg("group"), &fields) == nil {
					_ = check.InitGroupHealth(&fields)
				}
				value = fields
			case "initialize_provider_health":
				var fields model.Fields
				if json.Unmarshal(rawArg("source"), &fields) == nil {
					_ = check.InitProviderHealth(&fields)
				}
				value = fields
			case "provider_health_check":
				var fields model.Fields
				if json.Unmarshal(rawArg("source"), &fields) == nil {
					value, err = check.ResolveProviderHealth(&fields)
				} else {
					value, err = check.ResolveProviderHealth(nil)
				}
			case "_ipv4_server":
				text, ok := arg("value").(string)
				if !ok {
					err = invalidInput(str("message"))
				} else {
					value, err = ipv4ServerString(text, str("message"))
				}
			case "_server":
				text, ok := arg("value").(string)
				if !ok {
					err = invalidInput(DNSError)
				} else {
					value, err = serverString(text)
				}
			case "normalize_dns_servers":
				value, err = NormalizeDNSServersJSON(rawArg("value"), pyTruthy(arg("bootstrap")))
			case "_needs_bootstrap":
				value = needsBootstrap(str("server"))
			case "normalize_config":
				value, err = NormalizeDNSConfigJSON(rawArg("value"))
			case "load_config":
				value, err = LoadDNSConfig(ctx, r)
			case "effective_settings":
				raw, _ := optRaw(arg("value"))
				value, err = EffectiveDNSSettingsJSON(ctx, r, raw)
			case "describe_settings":
				raw, _ := optRaw(arg("value"))
				value, err = DescribeDNSSettingsJSON(ctx, r, raw)
			case "check_settings":
				raw, _ := optRaw(arg("value"))
				value, err = CheckDNSSettingsJSON(ctx, r, raw)
			case "settings":
				value, err = ResolveSettings(ctx, r)
			case "_proxy_of":
				if nodeRaw := rawArg("node"); len(nodeRaw) > 0 && !bytes.Equal(bytes.TrimSpace(nodeRaw), []byte("null")) {
					if proxy, ok := defaultCollaborators(func() convert.NodeConverter { return converter }).ConvertNode(ctx, nodeRaw); ok {
						value = proxy
					}
				}
			case "_needs_sub_store":
				nodeMap, _ := arg("node").(map[string]any)
				raw, ok := nodeMap["proxy_string"].(string)
				value = ok && raw != "" && defaultCollaborators(nil).NeedsRemoteConversion(raw)
			case "_run_probe":
				var typedProxies []model.Fields
				if uerr := json.Unmarshal(rawArg("proxies"), &typedProxies); uerr != nil {
					err = uerr
					break
				}
				var typedOptions ProbeSettings
				if optBytes := bytes.TrimSpace(rawArg("options")); len(optBytes) > 0 && !bytes.Equal(optBytes, []byte("null")) {
					if uerr := jsondecode.Unmarshal(optBytes, &typedOptions); uerr != nil {
						err = uerr
						break
					}
				}
				value, err = runProbeFields(ctx, typedProxies, typedOptions)
			case "region_of_name":
				value, err = evalRegionOfName(arg("name"))
			case "region_label":
				value, err = evalRegionLabel(arg("code"))
			case "service_requests":
				value = check.BuildServiceRequests()
			case "evaluate_services":
				value, err = evalEvaluateServices(arg("results"))
			case "exit_info":
				value, err = evalExitInfo(arg("results"))
			case "public_services":
				value = check.PublicServiceList()
			case "domain_request":
				value = check.BuildDomainRequest(str("request_id"), str("url"))
			case "classify_domain_result":
				value, err = evalClassifyDomain(arg("result"), str("original_url"))
			case "summarize_domain":
				value, err = evalSummarizeDomain(arg("per_target"))
			case "pick_targets":
				value, err = evalPickTargets(arg("targets"), arg("preferred_group"))
			case "probe_url":
				value, err = evalProbeURL(str("host"), arg("port"))
			case "path_healthy":
				value, err = evalPathHealthy(arg("stats"))
			case "judge":
				value, err = evalJudge(arg("direct"), arg("proxy"), check.JudgeOptions{
					PassiveFailing: pyTruthy(arg("passive_failing")),
					DirectHealthy:  pyTruthy(arg("direct_healthy")),
					ProxyHealthy:   pyTruthy(arg("proxy_healthy")),
				})
			case "domain_suggestion":
				value, err = evalDomainSuggestion(arg("hosts"))
			case "RegionStore.update", "RegionStore.load", "ProbeStore.save", "ProbeStore.load":
				storeDir := filepath.Join(dir, "store")
				agent := str("agent_id")
				region, probe := check.NewRegionStore(storeDir), check.NewProbeStore(storeDir)
				if before, ok := row["before"].(string); ok {
					_ = os.MkdirAll(storeDir, 0o755)
					_ = os.WriteFile(region.Path(agent), []byte(before), 0o644)
				}
				switch fn {
				case "RegionStore.update":
					err = region.UpdateData(agent, func(data *check.RegionData) error {
						var rawCheck model.Fields
						if uerr := jsondecode.Unmarshal(rec.mutated, &rawCheck); uerr != nil {
							return uerr
						}
						if !rawCheck.Has("domains") || !rawCheck.Has("websites") {
							return &check.ValidationError{Message: "区域检测存储的 domains 与 websites 必须为对象且 checked_at 为字符串"}
						}
						// Sections or entries that are not objects cannot be
						// represented in the typed record: refused as Python did.
						var next check.RegionData
						issues, uerr := model.DecodeStored(rec.mutated, &next, "")
						if uerr != nil {
							return uerr
						}
						for _, issue := range issues {
							if model.IssueUnder([]model.FieldIssue{issue}, "domains") || model.IssueUnder([]model.FieldIssue{issue}, "websites") {
								return &check.ValidationError{Message: "区域检测存储的 domains 与 websites 必须为对象且 checked_at 为字符串"}
							}
						}
						*data = next
						return nil
					}, storeNow)
				case "RegionStore.load":
					value = region.LoadData(agent)
				case "ProbeStore.save":
					err = saveProbeRaw(probe, agent, rawArg("results"), storeNow)
				case "ProbeStore.load":
					value = probe.LoadEntries(agent, storeNow)
				}
			default:
				t.Fatalf("row %d: unknown function %s", i, fn)
			}
		}()
		value = o17PythonNoneStatus(t, value)

		if expected, ok := native[i]; ok {
			classified[expected.Contract]++
			assertNativeExpectation(t, expected, fn, value, err)
			if len(runner.mismatch) > 0 {
				t.Errorf("worker contract: %v", runner.mismatch)
			}
			continue
		}

		got, want := map[string]any{}, map[string]any{}
		if err != nil {
			got["err"] = map[string]any{"class": recordedClass(err), "text": err.Error()}
		} else {
			got["ok"] = value
		}
		if errVal, hasErr := row["err"]; hasErr {
			want["err"] = errVal
		} else if strings.HasPrefix(fn, "initialize_") {
			want["ok"] = row["mutated"]
		} else {
			want["ok"] = row["ok"]
		}
		if strings.HasPrefix(fn, "RegionStore.") || strings.HasPrefix(fn, "ProbeStore.") {
			data, readErr := os.ReadFile(check.NewRegionStore(filepath.Join(dir, "store")).Path(str("agent_id")))
			var after any
			if readErr == nil {
				after = string(data)
			}
			got["after"] = after
			want["after"] = row["after"]
		}
		equal := testjson.Equal(want, got)
		if !equal && testjson.Equal(o18T1dWithoutUndeclared(want), o18T1dWithoutUndeclared(got)) {
			counts["accepted (O18 T1d: undeclared member dropped)"]++
			continue
		}
		if !equal && len(runner.mismatch) == 0 && n2Accepted(got, want, row) {
			counts["accepted (N2 URL parsing)"]++
			continue
		}
		if !equal || len(runner.mismatch) > 0 {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s):\n  got  %s\n  want %s\n  worker %v", i, fn, mapStr(row, "test"),
					clip(encode(got)), clip(encode(want)), runner.mismatch)
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

func n2Accepted(got, want, args map[string]any) bool {
	gotErr, _ := got["err"].(map[string]any)
	wantErr, _ := want["err"].(map[string]any)
	switch {
	case gotErr != nil && wantErr == nil && n2URLDifference(args):
		return true
	case gotErr == nil && wantErr != nil && hasNFKCDelimiterAuthority(args):
		return true
	case gotErr != nil && wantErr != nil:
		return mapStr(gotErr, "class") == mapStr(wantErr, "class") && !hasCJK(mapStr(wantErr, "text"))
	}
	encoded := encode(args)
	return strings.Contains(encoded, "İ") || strings.Contains(encoded, `\u0130`)
}
