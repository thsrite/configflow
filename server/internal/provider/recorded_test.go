package provider

// Replay of every top-level provider-delivery call the Python test suite makes
// (testdata/c17_record.py → testdata/recorded.jsonl.gz; regenerate on the test
// server, see the plugin's docstring). Collaborators answer from the
// recording: deployment_locations (A11) by arguments, fetches by label/id/url
// in order, Sub-Store conversions, generate_aggregation_provider (C31),
// uuid4 node ids, the subscription/download caches before the call and the
// clock at the call's start. Timestamps inside the call's wall-clock window
// (and cache updated_at stamps) are masked on both sides.

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

type substoreResult = substore.SubscriptionResult

func loadRecorded(t *testing.T) []*jsonx.Object {
	f, err := os.Open("testdata/recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*jsonx.Object
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
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

// replayLocations answers A11 from the recorded calls.
type replayLocations struct {
	entries [][2]any // parsed key, answer
	missing *[]string
}

func newReplayLocations(row *jsonx.Object, missing *[]string) *replayLocations {
	l := &replayLocations{missing: missing}
	for key, answer := range row.GetObject("locations").All() {
		parsed, err := jsonx.Parse([]byte(key))
		if err != nil {
			panic(err)
		}
		l.entries = append(l.entries, [2]any{parsed, answer})
	}
	return l
}

func (l *replayLocations) lookup(key []any) string {
	for _, e := range l.entries {
		if jsonx.Equal(e[0], key) {
			return e[1].(string)
		}
	}
	text, _ := jsonx.Encode(key, jsonx.Options{})
	*l.missing = append(*l.missing, string(text))
	return "<<no recorded A11 answer>>"
}

func optInt(v *int) any {
	if v == nil {
		return nil
	}
	return jsonx.Int(int64(*v))
}

func (l *replayLocations) PreparationContext(message, name, path, kind string) string {
	return l.lookup([]any{"ctx", message, name, path, kind})
}

func (l *replayLocations) SourceError(message, text string, line, column *int, cause error, sourceLabel string) string {
	// N7-3 §5.7: the A11 collaborator receives native YAML diagnostics.
	// Only validated parser/policy failures may reuse the reference collaborator
	// answer; all owner/source/text arguments and every non-YAML call stay strict.
	if cause != nil {
		_, nativeErr := yamlx.Parse([]byte(text))
		if nativeErr != nil && cause.Error() == nativeErr.Error() {
			for _, entry := range l.entries {
				args, ok := entry[0].([]any)
				if !ok || len(args) != 7 || args[0] != "src" || args[2] != text || args[6] != sourceLabel {
					continue
				}
				if args[1] == message || errors.Is(nativeErr, yamlx.ErrDuplicateKey) {
					return entry[1].(string)
				}
			}
		}
	}
	var c any
	if cause != nil {
		var mark any
		if position := nativeCauseFixtureMark(cause); position != nil {
			mark = []any{jsonx.Int(int64(position[0])), jsonx.Int(int64(position[1]))}
		}
		c = []any{"YAMLError", "", cause.Error(), mark}
	}
	return l.lookup([]any{"src", message, text, optInt(line), optInt(column), c, sourceLabel})
}

// recordedError is an exception a recorded collaborator raised.
func recordedError(entry *jsonx.Object) error {
	class, message := entry.GetString("raise"), entry.GetString("message")
	switch class {
	case "StrategyReferenceError":
		return &rules.StrategyReferenceError{Message: message}
	case "DialerReferenceError":
		return &convert.DialerReferenceError{Message: message}
	case "ListenerReferenceError":
		return &subs.ListenerReferenceError{Message: message}
	}
	return &fakeErr{class, message}
}

// world is the recorded environment of one call.
type world struct {
	t       *testing.T
	row     *jsonx.Object
	root    string
	repo    *repo.Repository
	ctx     context.Context
	mu      sync.Mutex
	answers map[string][]*jsonx.Object
	calls   []any
	ids     []string
	aggs    []any
}

func newWorld(t *testing.T, row *jsonx.Object, r *repo.Repository) *world {
	w := &world{t: t, row: row, root: r.DataDir(), answers: map[string][]*jsonx.Object{}}
	w.repo, w.ctx = r, repo.WithRepository(context.Background(), r)
	for rel, content := range row.GetObject("files").All() {
		text, ok := content.(string)
		if !ok {
			continue
		}
		path := filepath.Join(w.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range row.GetList("fetch") {
		call := item.([]any)
		key := call[0].(string) + "|" + jsonx.Str(call[1]) + "|" + jsonx.Str(call[2])
		w.answers[key] = append(w.answers[key], call[3].(*jsonx.Object))
	}
	for _, id := range row.GetList("ids") {
		w.ids = append(w.ids, id.(string))
	}
	w.aggs = row.GetList("agg")
	return w
}

func (w *world) fetcher(label string) refresh.SourceFetch {
	return func(ctx context.Context, subID, url string) (*substoreResult, error) {
		w.mu.Lock()
		w.calls = append(w.calls, []any{label, subID, url})
		key := label + "|" + subID + "|" + url
		queue := w.answers[key]
		var entry *jsonx.Object
		if len(queue) > 0 {
			entry, w.answers[key] = queue[0], queue[1:]
		}
		w.mu.Unlock()
		if entry == nil {
			return nil, &fakeErr{"ReplayError", "no recorded fetch answer for " + key}
		}
		if entry.Has("raise") {
			return nil, recordedError(entry)
		}
		return &substoreResult{Text: entry.GetString("text"), Source: entry.GetString("source"),
			Traffic: entry.GetObject("traffic"), FetchedAt: entry.GetString("fetched_at")}, nil
	}
}

func (w *world) nodeID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ids) == 0 {
		return "node_exhausted"
	}
	id := w.ids[0]
	w.ids = w.ids[1:]
	return id
}

// ConvertProxyString answers Sub-Store conversions from the recording.
func (w *world) ConvertProxyString(_ context.Context, proxyString string) *jsonx.Object {
	for _, item := range w.row.GetList("convert") {
		call := item.([]any)
		if call[0] == proxyString {
			out, _ := jsonx.DeepCopy(call[2]).(*jsonx.Object)
			return out
		}
	}
	return nil
}

func cacheUpdatesFrom(v any) []CacheUpdate {
	out := []CacheUpdate{}
	for _, item := range asList(v) {
		u := item.([]any)
		meta, _ := u[2].(*jsonx.Object)
		out = append(out, CacheUpdate{SubID: jsonx.Str(u[0]), Nodes: asList(u[1]), Metadata: meta})
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// renderedFull converts a recorded provider dict to *Rendered.
func renderedFull(o *jsonx.Object) *Rendered {
	r := renderedFrom(o)
	r.CacheUpdates = cacheUpdatesFrom(o.Value("cache_updates"))
	r.AggregationID = o.GetString("aggregation_id")
	r.URL = o.GetString("url")
	for k, v := range o.All() {
		switch k {
		case "name", "content", "cache_updates", "aggregation_id", "url", "local_path":
			continue
		}
		if r.Extra == nil {
			r.Extra = jsonx.NewObject()
		}
		r.Extra.Set(k, v)
	}
	return r
}

func (w *world) aggregation(ctx context.Context, agg, config, main *jsonx.Object, fetch refresh.SourceFetch) (*Rendered, error) {
	w.mu.Lock()
	var entry []any
	for i, item := range w.aggs {
		e := item.([]any)
		if jsonx.Equal(e[0], agg.Value("id")) {
			entry = e
			w.aggs = append(append([]any{}, w.aggs[:i]...), w.aggs[i+1:]...)
			break
		}
	}
	w.mu.Unlock()
	if entry == nil {
		return nil, &fakeErr{"ReplayError", "no recorded aggregation answer for " + jsonx.Str(agg.Value("id"))}
	}
	for _, item := range asList(entry[1]) {
		call := item.([]any)
		_, _ = fetch(ctx, jsonx.Str(call[0]), jsonx.Str(call[1]))
	}
	answer := entry[2].(*jsonx.Object)
	if answer.Has("raise") && answer.Has("strategy") {
		return nil, recordedError(answer)
	}
	value := renderedFull(asObject(jsonx.DeepCopy(answer)))
	value.HasName = false
	return value, nil
}

// files lists the files under the given prefixes (as c17_record._files).
func (w *world) files(prefixes []string) *jsonx.Object {
	out := jsonx.NewObject()
	var names []string
	contents := map[string]string{}
	for _, prefix := range prefixes {
		base := filepath.Join(w.root, filepath.FromSlash(prefix))
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			name := info.Name()
			if strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".bak") || strings.Contains(name, ".tmp") {
				return nil
			}
			rel, _ := filepath.Rel(w.root, path)
			data, _ := os.ReadFile(path)
			names = append(names, filepath.ToSlash(rel))
			contents[filepath.ToSlash(rel)] = string(data)
			return nil
		})
	}
	sort.Strings(names)
	for _, n := range names {
		out.Set(n, contents[n])
	}
	return out
}

// masker replaces timestamps of the call's wall-clock window.
type masker struct{ from, to time.Time }

func newMasker(row *jsonx.Object) masker {
	now := row.GetList("now")
	if len(now) != 2 {
		return masker{}
	}
	from, _ := time.Parse(time.RFC3339Nano, now[0].(string))
	to, _ := time.Parse(time.RFC3339Nano, now[1].(string))
	return masker{from.Add(-2 * time.Second), to.Add(2 * time.Second)}
}

func (m masker) stamp(s string) bool {
	if m.from.IsZero() || len(s) < 19 || s[4] != '-' || s[10] != 'T' {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.Replace(s, "Z", "+00:00", 1))
	if err != nil {
		if parsed, err = time.Parse("2006-01-02T15:04:05.999999-07:00", strings.Replace(s, "Z", "+00:00", 1)); err != nil {
			return false
		}
	}
	return !parsed.Before(m.from) && !parsed.After(m.to)
}

func (m masker) value(v any) any {
	switch x := v.(type) {
	case string:
		if m.stamp(x) {
			return "<now>"
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = m.value(x[i])
		}
		return out
	case *jsonx.Object:
		out := jsonx.NewObject()
		for k, item := range x.All() {
			if k == "updated_at" {
				out.Set(k, "<updated_at>")
				continue
			}
			out.Set(k, m.value(item))
		}
		return out
	}
	return v
}

// fileValues parses JSON files (cache files) so their stamps can be masked.
func (m masker) files(o *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for k, v := range o.All() {
		text, _ := v.(string)
		if strings.HasSuffix(k, ".json") {
			if parsed, err := jsonx.Parse([]byte(text)); err == nil {
				out.Set(k, m.value(parsed))
				continue
			}
		}
		out.Set(k, v)
	}
	return out
}

func recordedSnapshot(v any) *Snapshot {
	list := asList(v)
	if list == nil {
		return nil
	}
	return &Snapshot{ProfileID: "default", MainJSON: list[0].(string), ChainNames: asList(list[1])}
}

func recordedOutcome(value any, err error) *jsonx.Object {
	if err != nil {
		return jsonx.Obj("err", []any{errClass(err), err.Error()})
	}
	return jsonx.Obj("out", value)
}

func sortedRepr(names []any) []any {
	out := append([]any{}, names...)
	sort.Slice(out, func(i, j int) bool { return jsonx.Repr(out[i]) < jsonx.Repr(out[j]) })
	return out
}

func TestRecordedPythonTests(t *testing.T) {
	rows := loadRecorded(t)
	repository := corpusRepositoryFactory(t)
	counts := map[string]int{}
	files := map[string]bool{}
	failures, unsupported := 0, 0
	for i, row := range rows {
		fn := row.GetString("fn")
		if row.Has("unsupported") {
			// Calls with a test double for the snapshot (SimpleNamespace in
			// test_subscription_refresh_policy.py) are covered by the hand-ported tests.
			if reason := row.GetString("unsupported"); reason != "snapshot is a test double" {
				t.Errorf("row %d (%s %s): unsupported %s", i, fn, row.GetString("test"), reason)
			}
			unsupported++
			continue
		}
		var missing []string
		restoreLocations := SetLocations(newReplayLocations(row, &missing))
		args := row.GetList("args")
		want := jsonx.NewObject()
		if row.Has("err") {
			want.Set("err", row.Value("err"))
		} else {
			want.Set("out", row.Value("out"))
		}
		var got *jsonx.Object
		var w *world
		var extra []string
		m := newMasker(row)
		switch fn {
		case "shapes":
			got = recordedOutcome(nil, ValidateProxyShapes(jsonx.DeepCopy(args[0])))
		case "parse":
			out, err := ParseProviderProxies(args[0].(string), args[1], args[2])
			got = recordedOutcome(out, err)
		case "capture":
			snap, err := Capture(args[0].(string), asObject(jsonx.DeepCopy(args[1])), asObject(jsonx.DeepCopy(args[2])))
			if err != nil {
				got = recordedOutcome(nil, err)
				break
			}
			main, err := jsonx.Parse([]byte(snap.MainJSON))
			if err != nil {
				t.Fatal(err)
			}
			got = recordedOutcome([]any{snap.ProfileID, main, sortedRepr(snap.ChainNames)}, nil)
		case "has_chains":
			chains, err := recordedSnapshot(args[0]).HasChains()
			got = recordedOutcome(chains, err)
		case "validate":
			got = recordedOutcome(nil, recordedSnapshot(args[0]).Validate(jsonx.DeepCopy(args[1]), args[2].(bool), ProviderName(args[3])))
		case "rendered_bundle":
			var providers []*Rendered
			for _, p := range asList(args[1]) {
				providers = append(providers, renderedFull(p.(*jsonx.Object)))
			}
			chains, err := ValidateRenderedBundle(recordedSnapshot(args[0]), providers)
			got = recordedOutcome(chains, err)
		case "prepare_sub", "bundle", "commit_bundle", "commit_updates":
			w = newWorld(t, row, repository(t))
			restoreEnv := SetEnv(&Env{Fetch: w.fetcher("default"), SubscriptionFetch: w.fetcher("subscriptions"),
				AggregationFetch: w.fetcher("aggregations"), NodeID: w.nodeID, Converter: w})
			var clock fixedClock
			if now := row.GetList("now"); len(now) > 0 {
				clock.now, _ = time.Parse(time.RFC3339Nano, now[0].(string))
			}
			restoreRefresh := setRefreshFixture(clock, w)
			restoreAgg := SetAggregationProvider(w.aggregation)
			got = w.run(fn, args, m)
			restoreAgg()
			restoreRefresh()
			restoreEnv()
			if wantCalls := row.GetList("fetch"); true {
				var calls []any
				for _, c := range wantCalls {
					calls = append(calls, c.([]any)[:3])
				}
				// Aggregation providers fetch on worker threads in Python: compare as a multiset.
				if !jsonx.Equal(sortedRepr(calls), sortedRepr(w.calls)) {
					extra = append(extra, "fetch calls"+diffText(calls, w.calls))
				}
			}
			if w.ids != nil && len(w.ids) > 0 {
				extra = append(extra, "unused node ids")
			}
			if row.Has("files_after") {
				prefixes := []string{"shared/subscribes"}
				if fn == "commit_bundle" {
					prefixes = append(prefixes, "profiles/"+args[0].(string)+"/providers")
				}
				wantFiles, gotFiles := m.files(row.GetObject("files_after")), m.files(w.files(prefixes))
				if !jsonx.Equal(wantFiles, gotFiles) {
					extra = append(extra, "files"+diffText(wantFiles, gotFiles))
				}
			}
		default:
			t.Fatalf("row %d: unknown fn %q", i, fn)
		}
		restoreLocations()
		if fn == "bundle" {
			dropTestDoubleCacheUpdates(want, got)
		}
		counts[fn]++
		files[strings.SplitN(row.GetString("test"), "::", 2)[0]] = true
		if len(missing) > 0 {
			extra = append(extra, "A11 calls without a recorded answer: "+strings.Join(missing, "; "))
		}
		if !writerOutcomeEqual(fn, m.value(want), m.value(got)) || len(extra) > 0 {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s)%s %s", i, fn, row.GetString("test"), diffText(m.value(want), m.value(got)), strings.Join(extra, "\n"))
			}
		}
	}
	t.Logf("recorded calls: %d from %d test files %v, unsupported %d, failures %d", len(rows), len(files), counts, unsupported, failures)
}

func (w *world) run(fn string, args []any, m masker) *jsonx.Object {
	ctx := w.ctx
	switch fn {
	case "prepare_sub":
		snap := recordedSnapshot(args[1])
		if snap == nil {
			w.t.Fatalf("prepare_sub without a snapshot")
		}
		opts := asObject(args[2])
		result, err := PrepareSubscriptionProvider(ctx, asObject(jsonx.DeepCopy(args[0])), jsonx.NewObject(), snap, PrepareOptions{
			Fetch:                  w.fetcher("fetch"),
			AllowTransportFallback: opts.GetBool("allow_transport_fallback"),
			SourcePrepared:         opts.GetBool("source_prepared"),
			SourcePath:             opts.Value("source_path"),
		})
		if err != nil {
			return recordedOutcome(nil, err)
		}
		return recordedOutcome(renderedObject(result, false), nil)
	case "bundle":
		config := asObject(jsonx.DeepCopy(args[0]))
		main := asObject(jsonx.DeepCopy(args[1]))
		opts := asObject(args[2])
		before := []any{jsonx.DeepCopy(config), jsonx.DeepCopy(main)}
		var prepared map[string]*Rendered
		if p := opts.GetObject("prepared"); p != nil {
			prepared = map[string]*Rendered{}
			for name, value := range p.All() {
				prepared[name] = renderedFull(value.(*jsonx.Object))
				prepared[name].HasName = value.(*jsonx.Object).Has("name")
			}
		}
		bundleOpts := BundleOptions{
			Requested:              toRequested(opts.Value("requested")),
			RenderAll:              opts.GetBool("render_all"),
			AllowTransportFallback: opts.GetBool("allow_transport_fallback"),
			Prepared:               prepared,
		}
		if opts.GetBool("subscription_fetch") {
			bundleOpts.SubscriptionFetch = w.fetcher("option")
		}
		progress := []any{}
		if opts.GetBool("progress") {
			bundleOpts.Progress = func(message string, force bool) { progress = append(progress, []any{message, force}) }
		}
		bundle, err := PrepareProviderBundle(ctx, config, main, bundleOpts)
		if !jsonx.Equal(before, []any{config, main}) {
			w.t.Errorf("bundle modified its arguments (%s)", w.row.GetString("test"))
		}
		if wantProgress := w.row.GetList("progress"); !jsonx.Equal(wantProgress, progress) && !(len(wantProgress) == 0 && len(progress) == 0) {
			w.t.Errorf("progress (%s)%s", w.row.GetString("test"), diffText(wantProgress, progress))
		}
		if err != nil {
			return recordedOutcome(nil, err)
		}
		items := []any{}
		for _, item := range bundle {
			items = append(items, renderedObject(item, true))
		}
		return recordedOutcome(items, nil)
	case "commit_bundle":
		profileID := args[0].(string)
		if _, err := w.repo.ProfileMetadata(ctx, profileID); err != nil {
			if _, err := w.repo.CreateProfile(ctx, jsonx.Obj("id", profileID, "name", profileID), ""); err != nil {
				w.t.Fatal(err)
			}
		}
		var bundle []*Rendered
		for _, item := range asList(args[1]) {
			r := renderedFull(item.(*jsonx.Object))
			if len(args) > 2 && args[2] == true { // the test replaced commit_cache_updates
				r.CacheUpdates = nil
			}
			bundle = append(bundle, r)
		}
		return recordedOutcome(nil, CommitProviderBundle(ctx, profileID, bundle))
	case "commit_updates":
		return recordedOutcome(nil, CommitCacheUpdates(ctx, cacheUpdatesFrom(args[0])))
	}
	return nil
}

// dropTestDoubleCacheUpdates: a test's fake generate_aggregation_provider
// (test_proxy_chains.py) returns no 'cache_updates'; the real one always
// does, and Rendered always has the field. Drop an empty one where the
// recorded item has none.
func dropTestDoubleCacheUpdates(want, got *jsonx.Object) {
	wantItems, gotItems := asList(want.Value("out")), asList(got.Value("out"))
	for i := range wantItems {
		if i >= len(gotItems) {
			return
		}
		w, g := asObject(wantItems[i]), asObject(gotItems[i])
		if w != nil && g != nil && !w.Has("cache_updates") && len(asList(g.Value("cache_updates"))) == 0 {
			g.Delete("cache_updates")
		}
	}
}

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// ProviderName selects a textual provider name at the legacy boundary.
func ProviderName(value any) *string {
	name, ok := value.(string)
	if !ok {
		return nil
	}
	return &name
}

// SubscriptionDialerRecord transcodes one cached source topology.
func SubscriptionDialerRecord(ctx context.Context, sub *jsonx.Object) (*jsonx.Object, error) {
	var source model.Subscription
	if err := decodeLegacy(sub, &source); err != nil {
		return nil, err
	}
	record, err := DialerRecord(ctx, source)
	if err != nil {
		return nil, err
	}
	return legacyObject(record)
}

// ValidateProxyShapes delegates legacy metadata validation to ValidateProxyNodes.
func ValidateProxyShapes(value any) error {
	nodes, err := nodesFromLegacy(value)
	if err != nil {
		return err
	}
	return ValidateProxyNodes(nodes)
}

// ValidateRenderedBundle adapts legacy artifacts to ValidateArtifacts.
func ValidateRenderedBundle(snapshot *Snapshot, providers []*Rendered) (bool, error) {
	delivery, err := snapshot.native()
	if err != nil {
		return false, err
	}
	list := make([]*Artifact, 0, len(providers))
	for _, item := range providers {
		value, err := nativeArtifact(item)
		if err != nil {
			return false, err
		}
		list = append(list, value)
	}
	return ValidateArtifacts(delivery, list)
}

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// Main returns an owned legacy projection of the frozen graph.
func (s *Snapshot) Main() (*jsonx.Object, error) {
	delivery, err := s.native()
	if err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(delivery.main)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, &InputError{Field: "main"}
	}
	return object, nil
}

// HasChains delegates to the immutable native snapshot.
func (s *Snapshot) HasChains() (bool, error) {
	delivery, err := s.native()
	if err != nil {
		return false, err
	}
	return delivery.HasChains()
}

// Validate transcodes only the supplied payload before native validation.
func (s *Snapshot) Validate(value any, force bool, name *string) error {
	nodes, err := nodesFromLegacy(value)
	if err != nil {
		return err
	}
	delivery, err := s.native()
	if err != nil {
		return err
	}
	return delivery.Validate(nodes, force, name)
}

// Adapt delegates legacy subscription delivery to AdaptDelivered.
func (s *Snapshot) Adapt(ctx context.Context, sub *jsonx.Object, proxies []any, chains bool) ([]any, error) {
	return AdaptDelivered(ctx, s, sub, proxies, chains)
}

// AdaptDelivered transcodes a legacy payload around the native dialer owner.
func AdaptDelivered(ctx context.Context, snapshot *Snapshot, sub *jsonx.Object, proxies []any, chains bool) ([]any, error) {
	if snapshot == nil {
		return proxies, nil
	}
	var source model.Subscription
	if err := decodeLegacy(sub, &source); err != nil {
		return nil, err
	}
	delivery, err := snapshot.native()
	if err != nil {
		return nil, err
	}
	nodes, err := nodesFromLegacy(proxies)
	if err != nil {
		return nil, err
	}
	nodes, err = delivery.AdaptNodes(ctx, source, nodes, chains)
	if err != nil {
		return nil, err
	}
	return legacyNodes(nodes)
}

func loadProviderProxies(text string, name, path any, kind, label string) ([]any, error) {
	n, p := legacyLabel(name), legacyLabel(path)
	nodes, err := readProviderNodes(text, n, p, kind, label)
	if err != nil {
		return nil, err
	}
	return legacyNodes(nodes)
}

// DefaultEnv returns codecs for the production typed delivery environment.
func DefaultEnv() *Env {
	native := DefaultDeliveryEnv()
	return &Env{Fetch: legacyFetch(native.Fetch), SubscriptionFetch: legacyFetch(native.SubscriptionFetch), AggregationFetch: legacyFetch(native.AggregationFetch), NodeID: native.NodeID}
}

// SetEnv installs legacy collaborator codecs and returns their restoration.
func SetEnv(env *Env) func() {
	if env == nil {
		return SetDeliveryEnv(nil)
	}
	typed := &DeliveryEnv{Fetch: nativeFetch(env.Fetch), SubscriptionFetch: nativeFetch(env.SubscriptionFetch), AggregationFetch: nativeFetch(env.AggregationFetch), NodeID: env.NodeID}
	if env.Converter != nil {
		typed.Converter = legacyConverter{env.Converter}
	}
	return SetDeliveryEnv(typed)
}

// SetAggregationProvider installs an archived callback codec around typed rendering.
func SetAggregationProvider(fn AggregationProvider) func() {
	if fn == nil {
		return SetAggregationRenderer(nil)
	}
	return SetAggregationRenderer(func(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, snapshot *DeliverySnapshot, fetch refresh.FetchSourceFn) (*Artifact, error) {
		configValue, err := legacyObject(config)
		if err != nil {
			return nil, err
		}
		mainValue, err := legacyObjectFromGraph(main)
		if err != nil {
			return nil, err
		}
		a, err := legacyObject(agg)
		if err != nil {
			return nil, err
		}
		rendered, err := fn(ctx, a, configValue, mainValue, legacyFetch(fetch))
		if err != nil {
			return nil, err
		}
		return nativeArtifact(rendered)
	})
}

// ServeProvider adapts legacy JSON identity data to the native response owner.
func ServeProvider(c *httpx.Ctx, kind, id, profileID string, configData any, render Render) (*httpx.Response, error) {
	data, err := jsonresp.Encode(configData)
	if err != nil {
		return nil, err
	}
	return serveProviderJSON(c, kind, id, profileID, data, render)
}

// findByID remains a codec for archived callback fixtures only.
func findByID(value any, id string) (*jsonx.Object, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, &InputError{Field: "resources"}
	}
	for _, item := range list {
		object, ok := item.(*jsonx.Object)
		if !ok {
			return nil, &InputError{Field: "resource"}
		}
		if object.GetString("id") == id {
			return object, nil
		}
	}
	return nil, &InputError{Field: "resource.id"}
}

// nodeAt translates archived mixed field paths at the compatibility boundary.
func nodeAt(text string, field []any) (*yamlx.Node, bool) {
	path := make(FieldPath, 0, len(field))
	for _, value := range field {
		switch step := value.(type) {
		case string:
			path = append(path, FieldStep{Key: step})
		case int:
			path = append(path, FieldStep{Index: step, Indexed: true})
		default:
			return nil, false
		}
	}
	return nodeAtPath(text, path)
}

// Env retains legacy test/integration hooks; use DeliveryEnv for typed wiring.
type Env struct {
	Fetch, SubscriptionFetch, AggregationFetch refresh.SourceFetch
	Converter                                  convert.ProxyStringConverter
	NodeID                                     func() string
}

// AggregationProvider is a temporary callback codec for unmigrated callers.
type AggregationProvider func(context.Context, *jsonx.Object, *jsonx.Object, *jsonx.Object, refresh.SourceFetch) (*Rendered, error)

// Archived JSON fixtures adapt only at this test boundary to the native owners.
func (c legacyConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	value := c.source.ConvertProxyString(ctx, text)
	if value == nil {
		return nil, nil
	}
	return legacyGraph(value)
}

type legacyConverter struct{ source convert.ProxyStringConverter }

func legacyObjectFromGraph(graph *yamlx.Node) (*jsonx.Object, error) {
	raw, err := yamlx.ToJSON(graph)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}
