package provider

// The reference corpus from the real Python modules (testdata/gen.py →
// testdata/corpus.json.gz; regenerate on the test server:
// rt --back server/internal/provider/testdata/corpus.json.gz -- bash -lc
// 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/gen.py').
// deployment_locations (A11) is a fake on both sides (fakeLocations).

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

const corpusNowISO = "2026-11-01T00:00:00.250000+00:00"

var corpusNow = time.Date(2026, 11, 1, 0, 0, 0, 250000000, time.UTC)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time        { return c.now }
func (c fixedClock) Monotonic() float64    { return 0 }
func (c fixedClock) Sleep(d time.Duration) {}

// fakeLocations renders like gen.py's fake_ctx / fake_src.
type fakeLocations struct{}

func (fakeLocations) PreparationContext(message, name, path, kind string) string {
	return "CTX[" + kind + "|" + name + "|" + path + "]" + message
}

func pyOptInt(v *int) string {
	if v == nil {
		return "None"
	}
	return strconv.Itoa(*v)
}

func pyOptStr(s string) string {
	if s == "" {
		return "None"
	}
	return s
}

func (fakeLocations) SourceError(message, text string, line, column *int, cause error, sourceLabel string) string {
	c := "None"
	if cause != nil {
		mark := "None"
		if position := nativeCauseFixtureMark(cause); position != nil {
			mark = fmt.Sprintf("%d,%d", position[0], position[1])
		}
		c = "YAMLError:None:" + pyOptStr(cause.Error()) + ":" + mark
	}
	return "SRC[" + message + "|" + pyOptInt(line) + "|" + pyOptInt(column) + "|" + sourceLabel + "|" + c + "]"
}

// nativeCauseFixtureMark encodes only the historical test collaborator trace.
// Runtime diagnostic coordinates remain native and one-based.
func nativeCauseFixtureMark(cause error) *[2]int {
	var syntax *yamlx.SyntaxError
	if errors.As(cause, &syntax) && syntax.Line > 0 {
		return &[2]int{syntax.Line - 1, max(1, syntax.Column) - 1}
	}
	return nil
}

// fakeErr is an exception a fake raises.
type fakeErr struct{ class, message string }

func (e *fakeErr) Error() string       { return e.message }
func (e *fakeErr) PythonClass() string { return e.class }

// errClass is type(exc).__name__.
func errClass(err error) string {
	var dialer *convert.DialerReferenceError
	if errors.As(err, &dialer) {
		return "DialerReferenceError"
	}
	var strategy *rules.StrategyReferenceError
	if errors.As(err, &strategy) {
		return "StrategyReferenceError"
	}
	var classed interface{ PythonClass() string }
	if errors.As(err, &classed) {
		return classed.PythonClass()
	}
	if c := convert.PyErrorClass(err); c != "" {
		return c
	}
	return fmt.Sprintf("%T", err)
}

func outcomeOf(value any, err error) *jsonx.Object {
	if err != nil {
		out := jsonx.Obj("error", errClass(err))
		if isStrategyReference(err) {
			out.Set("message", err.Error())
		}
		return out
	}
	return jsonx.Obj("output", value)
}

func expectedOf(row *jsonx.Object) *jsonx.Object {
	if row.Has("error") {
		out := jsonx.Obj("error", row.Value("error"))
		if row.Has("message") {
			out.Set("message", row.Value("message"))
		}
		return out
	}
	return jsonx.Obj("output", row.Value("output"))
}

func loadCorpus(t *testing.T) []*jsonx.Object {
	f, err := os.Open("testdata/corpus.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*jsonx.Object
	for _, row := range value.([]any) {
		rows = append(rows, row.(*jsonx.Object))
	}
	return rows
}

func renderedObject(r *Rendered, bundle bool) *jsonx.Object {
	o := jsonx.NewObject()
	if r.HasName {
		o.Set("name", r.Name)
	}
	o.Set("content", r.Content)
	updates := []any{}
	for _, u := range r.CacheUpdates {
		updates = append(updates, []any{u.SubID, u.Nodes, u.Metadata})
	}
	o.Set("cache_updates", updates)
	if r.AggregationID != "" {
		o.Set("aggregation_id", r.AggregationID)
	}
	if bundle {
		o.Set("url", r.URL)
		o.Set("local_path", r.LocalPath)
	}
	for k, v := range r.Extra.All() {
		o.Set(k, v)
	}
	return o
}

func renderedFrom(o *jsonx.Object) *Rendered {
	r := &Rendered{Content: o.GetString("content")}
	if name, ok := o.Get("name"); ok {
		r.Name, r.HasName = jsonx.Str(name), true
		if s, ok := name.(string); ok {
			r.Name = s
		}
	}
	if lp, ok := o.Get("local_path"); ok {
		r.LocalPath = lp
	}
	return r
}

func asObject(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	return o
}

func diffText(want, got any) string {
	w, _ := jsonx.Encode(want, jsonx.Options{SortKeys: true})
	g, _ := jsonx.Encode(got, jsonx.Options{SortKeys: true})
	return "\nwant " + string(w) + "\n got " + string(g)
}

func TestCorpus(t *testing.T) {
	defer SetLocations(fakeLocations{})()
	rows := loadCorpus(t)
	repository := corpusRepositoryFactory(t)
	nativeSnapshots := nativeSnapshotExpectations(t, rows)
	for index, expected := range nativeDeliveryReferences(t, rows) {
		nativeSnapshots[index] = expected
	}
	counts := map[string]int{}
	failures := 0
	for i, row := range rows {
		kind := row.GetString("kind")
		input := row.Value("input")
		var got *jsonx.Object
		var extra func(*testing.T)
		switch kind {
		case "shapes":
			got = outcomeOf(nil, ValidateProxyShapes(jsonx.DeepCopy(input)))
		case "parse":
			in := asObject(input)
			out, err := ParseProviderProxies(in.GetString("text"), in.Value("name"), in.Value("path"))
			got = outcomeOf(out, err)
		case "snapshot":
			got = snapshotRow(asObject(input))
		case "rendered_bundle":
			in := asObject(input)
			got = outcomeOf(func() (any, error) {
				snap, err := Capture("default", asObject(jsonx.DeepCopy(in.Value("main"))), asObject(jsonx.DeepCopy(in.Value("profile"))))
				if err != nil {
					return nil, err
				}
				var providers []*Rendered
				for _, p := range in.GetList("providers") {
					providers = append(providers, renderedFrom(p.(*jsonx.Object)))
				}
				return ValidateRenderedBundle(snap, providers)
			}())
		case "prepare_sub", "bundle":
			var check func(t *testing.T, files map[string]string)
			got, check = scenarioRow(t, kind, asObject(input), row, repository(t))
			extra = func(t *testing.T) { check(t, nil) }
		default:
			t.Fatalf("row %d: unknown kind %q", i, kind)
		}
		counts[kind]++
		want := expectedOf(row)
		if native, ok := nativeSnapshots[i]; ok {
			want = native
		}
		if reason := knownDifference(kind, input); reason != "" {
			if jsonx.Equal(want, got) {
				t.Errorf("row %d: known difference (%s) no longer differs", i, reason)
			}
			continue
		}
		if kind == "snapshot" && !jsonx.Equal(want, got) {
			w, _ := want.Value("output").([]any)
			g, _ := got.Value("output").([]any)
			for j := 0; j < len(w) && j < len(g); j++ {
				if !jsonx.Equal(w[j], g[j]) {
					t.Errorf("row %d snapshot outcome %d: %s", i, j, diffText(w[j], g[j]))
					break
				}
			}
		}
		sameOutput := writerOutcomeEqual(kind, want, got)
		nativeMatch := !sameOutput && nativeProviderOutcome(kind, asObject(input), want, got)
		if nativeMatch {
			counts["N7-3 native boundary"]++
		}
		if !sameOutput && !nativeMatch {
			failures++
			if failures <= 40 {
				t.Errorf("row %d (%s) input %s%s", i, kind, diffText(input, nil)[6:], diffText(want, got))
			}
		}
		if extra != nil {
			extra(t)
		}
	}
	t.Logf("corpus rows: %v, failures %d", counts, failures)
}

// knownDifference names accepted differences (documented in PROGRESS C17).
func knownDifference(kind string, input any) string {
	return ""
}

func snapshotRow(in *jsonx.Object) *jsonx.Object {
	snap, err := Capture("default", asObject(jsonx.DeepCopy(in.Value("main"))), asObject(jsonx.DeepCopy(in.Value("profile"))))
	if err != nil {
		return outcomeOf(nil, err)
	}
	names := []string{}
	for _, n := range snap.ChainNames {
		names = append(names, jsonx.Str(n))
	}
	sort.Strings(names)
	namesAny := []any{}
	for _, n := range names {
		namesAny = append(namesAny, n)
	}
	out := []any{jsonx.Obj("chain_names", namesAny)}
	chains, err := snap.HasChains()
	out = append(out, outcomeOf(chains, err))
	for _, payload := range in.GetList("payloads") {
		for _, force := range []bool{false, true} {
			for _, name := range []any{nil, "Feed", "Other", "Nope", "Alias"} {
				err := snap.Validate(jsonx.DeepCopy(payload), force, ProviderName(name))
				out = append(out, outcomeOf(nil, err))
			}
		}
	}
	return jsonx.Obj("output", out)
}

// scenario is gen.py's Scenario: a fresh repository with seeded caches,
// fake fetchers keyed "id|url" and the fake aggregation provider.
type scenario struct {
	spec  *jsonx.Object
	mu    sync.Mutex
	calls []any
}

func (s *scenario) record(call ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *scenario) fetcher(label string) refresh.SourceFetch {
	return func(ctx context.Context, subID, url string) (*substore.SubscriptionResult, error) {
		s.record(label, subID, url)
		entry := s.spec.GetObject("fetch").GetObject(subID + "|" + url)
		if entry == nil {
			return nil, &fakeErr{"ConnectionError", "no fake answer"}
		}
		if raise, ok := entry.Get("raise"); ok {
			message := entry.GetString("message")
			if raise == "StrategyReferenceError" {
				return nil, &rules.StrategyReferenceError{Message: message}
			}
			return nil, &fakeErr{raise.(string), message}
		}
		fetchedAt := entry.GetString("fetched_at")
		if fetchedAt == "" && !entry.GetBool("tuple") {
			fetchedAt = corpusNowISO
		}
		return &substore.SubscriptionResult{Text: entry.GetString("text"), Source: entry.GetString("source"),
			Traffic: entry.GetObject("traffic"), FetchedAt: fetchedAt}, nil
	}
}

func (s *scenario) fakeAggregation(ctx context.Context, agg, config, main *jsonx.Object, fetch refresh.SourceFetch) (*Rendered, error) {
	s.record("agg", agg.Value("id"), false, true, main != nil)
	proxies := []any{}
	for _, sid := range agg.GetList("subscriptions") {
		sub, err := findByID(config.Value("subscriptions"), sid.(string))
		if err != nil {
			return nil, err
		}
		result, err := fetch(ctx, sid.(string), sub.GetString("url"))
		if err != nil {
			if isStrategyReference(err) {
				return nil, err
			}
			s.record("agg-fetch-error", sid, errClass(err))
			continue
		}
		data, err := readProviderTestValue(result.Text)
		if err != nil {
			return nil, err
		}
		if doc, ok := data.(*jsonx.Object); ok {
			proxies = append(proxies, doc.GetList("proxies")...)
		}
	}
	for _, nid := range agg.GetList("nodes") {
		proxies = append(proxies, jsonx.Obj("name", "node-"+jsonx.Str(nid), "type", "http", "server", "example.test", "port", jsonx.Int(80)))
	}
	var content string
	if fake, ok := agg.Get("fake_content"); ok {
		content = fake.(string)
	} else {
		node, err := yamlbridge.Encode(jsonx.Obj("proxies", proxies))
		if err != nil {
			return nil, err
		}
		data, err := yamlx.Marshal(node)
		if err != nil {
			return nil, err
		}
		content = string(data)
	}
	return &Rendered{Content: content, CacheUpdates: []CacheUpdate{}, Extra: jsonx.Obj("total_count", jsonx.Int(int64(len(proxies))))}, nil
}

func dumpFiles(t *testing.T, root string) map[string]string {
	files := map[string]string{}
	for _, base := range []string{"shared", "profiles"} {
		_ = filepath.Walk(filepath.Join(root, base), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			name := info.Name()
			if strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".bak") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if !strings.HasPrefix(rel, "shared/subscribes/") && !strings.HasPrefix(rel, "shared/subscription-downloads/") &&
				!strings.Contains(rel, "/providers/") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files[rel] = string(data)
			return nil
		})
	}
	return files
}

func toRequested(v any) *Requested {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	return &Requested{Kind: list[0].(string), Resource: list[1].(*jsonx.Object)}
}

func scenarioRow(t *testing.T, kind string, in, row *jsonx.Object, r *repo.Repository) (*jsonx.Object, func(*testing.T, map[string]string)) {
	root := r.DataDir()
	ctx := repo.WithRepository(context.Background(), r)
	for id, cache := range in.GetObject("caches").All() {
		if _, err := r.WriteSharedJSON(ctx, "subscribes/"+id+".json", cache); err != nil {
			t.Fatal(err)
		}
	}
	for url, doc := range in.GetObject("downloads").All() {
		if _, err := r.WriteSharedJSON(ctx, refresh.DownloadCachePath(url), doc); err != nil {
			t.Fatal(err)
		}
	}
	s := &scenario{spec: in}
	defer SetEnv(&Env{Fetch: s.fetcher("default"), SubscriptionFetch: s.fetcher("subscriptions"),
		AggregationFetch: s.fetcher("aggregations"), NodeID: SequentialNodeIDs(),
		Converter: nilConverter{}})()
	defer setRefreshFixture(fixedClock{corpusNow}, nilConverter{})()
	defer SetAggregationProvider(s.fakeAggregation)()

	var got *jsonx.Object
	switch kind {
	case "prepare_sub":
		snap, err := Capture("default", asObject(jsonx.DeepCopy(in.Value("main"))), asObject(jsonx.DeepCopy(in.Value("profile"))))
		if err != nil {
			got = outcomeOf(nil, err)
			break
		}
		opts := in.GetObject("opts")
		result, err := PrepareSubscriptionProvider(ctx, asObject(jsonx.DeepCopy(in.Value("sub"))), jsonx.NewObject(), snap, PrepareOptions{
			AllowTransportFallback: opts.GetBool("allow_transport_fallback"),
			SourcePrepared:         opts.GetBool("source_prepared"),
			SourcePath:             opts.Value("source_path"),
		})
		if err != nil {
			got = outcomeOf(nil, err)
		} else {
			got = outcomeOf(renderedObject(result, false), nil)
		}
	case "bundle":
		opts := in.GetObject("opts")
		config := asObject(jsonx.DeepCopy(in.Value("config")))
		main := asObject(jsonx.DeepCopy(in.Value("main")))
		before := [2]any{jsonx.DeepCopy(config), jsonx.DeepCopy(main)}
		var progress []any
		var prepared map[string]*Rendered
		if p := opts.GetObject("prepared"); p != nil {
			prepared = map[string]*Rendered{}
			for name, value := range p.All() {
				prepared[name] = renderedFrom(value.(*jsonx.Object))
				prepared[name].HasName = false
			}
		}
		bundle, err := PrepareProviderBundle(ctx, config, main, BundleOptions{
			Requested:              toRequested(opts.Value("requested")),
			RenderAll:              opts.GetBool("render_all"),
			AllowTransportFallback: opts.GetBool("allow_transport_fallback"),
			Prepared:               prepared,
			Progress:               func(message string, force bool) { progress = append(progress, []any{message, force}) },
		})
		if !jsonx.Equal(before[0], config) || !jsonx.Equal(before[1], main) {
			t.Errorf("bundle modified its arguments: %s", diffText(before, []any{config, main}))
		}
		if err == nil {
			err = CommitProviderBundle(ctx, "default", bundle)
		}
		if err != nil {
			got = outcomeOf(nil, err)
		} else {
			items := []any{}
			for _, item := range bundle {
				items = append(items, renderedObject(item, true))
			}
			if progress == nil {
				progress = []any{}
			}
			got = outcomeOf(jsonx.Obj("bundle", items, "progress", progress), nil)
		}
	}
	calls := s.calls
	if calls == nil {
		calls = []any{}
	}
	files := dumpFiles(t, root)
	return got, func(t *testing.T, _ map[string]string) {
		if want := row.Value("calls"); !jsonx.Equal(want, calls) {
			t.Errorf("%s calls for %s%s", kind, diffText(in, nil)[6:], diffText(want, calls))
		}
		wantFiles := row.GetObject("files")
		gotFiles := jsonx.NewObject()
		for _, k := range sortedKeys(files) {
			gotFiles.Set(k, maskUpdatedAt(files[k], wantFiles.GetString(k)))
		}
		if !providerWriterFilesEqual(wantFiles, gotFiles) {
			t.Errorf("%s files for %s%s", kind, diffText(in, nil)[6:], diffText(wantFiles, gotFiles))
		}
	}
}

// maskUpdatedAt replaces the updated_at Go's cache writer stamped with the
// system clock by the fixed time Python's patched clock wrote.
func maskUpdatedAt(got, want string) string {
	if !strings.Contains(want, `"updated_at": "`+corpusNowISO+`"`) {
		return got
	}
	start := strings.Index(got, `"updated_at": "`)
	if start < 0 {
		return got
	}
	start += len(`"updated_at": "`)
	end := strings.Index(got[start:], `"`)
	return got[:start] + corpusNowISO + got[start+end:]
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nilConverter is convert_proxy_string patched to return None.
type nilConverter struct{}

func (nilConverter) ConvertProxyString(context.Context, string) *jsonx.Object { return nil }

// nativeParseOutcome accepts only native parser diagnostics or the D1 policy.
// The complete Go error must identify the same owner and source, and include
// the exact facade error. Valid-input differences cannot enter this branch.
func nativeParseOutcome(input, want, got *jsonx.Object) bool {
	_, err := yamlx.Parse([]byte(input.GetString("text")))
	if err == nil || got.GetString("error") != "DialerReferenceError" || got.Len() != 2 {
		return false
	}
	if !want.Has("error") && !errors.Is(err, yamlx.ErrDuplicateKey) {
		return false
	}
	prefix := ""
	if input.GetString("name") != "" || input.GetString("path") != "" {
		prefix = "CTX[订阅|" + input.GetString("name") + "|" + input.GetString("path") + "]"
	}
	message := got.GetString("message")
	return strings.HasPrefix(message, prefix+"SRC[Provider YAML 内容无效|") && strings.Contains(message, "|本次订阅返回的内容|YAMLError:None:"+err.Error()+":")
}
