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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func loadRecorded(t *testing.T) []map[string]any {
	rows, _ := loadRecordedWithRaw(t)
	return rows
}

func loadRecordedWithRaw(t *testing.T) ([]map[string]any, [][]json.RawMessage) {
	f, err := os.Open("testdata/recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	var rawArgs [][]json.RawMessage
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		row, err := decodeMap(line)
		if err != nil {
			t.Fatal(err)
		}
		var aux struct {
			Args []json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(line, &aux)
		rows = append(rows, row)
		rawArgs = append(rawArgs, aux.Args)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, rawArgs
}

// replayLocations answers A11 from the recorded calls.
type replayLocations struct {
	entries [][2]any // parsed key, answer
	missing *[]string
}

func newReplayLocations(row map[string]any, missing *[]string) *replayLocations {
	l := &replayLocations{missing: missing}
	for key, answer := range mapObj(row, "locations") {
		parsed, err := decodeAny([]byte(key))
		if err != nil {
			panic(err)
		}
		l.entries = append(l.entries, [2]any{parsed, answer})
	}
	return l
}

func (l *replayLocations) lookup(key []any) string {
	for _, e := range l.entries {
		if testjson.Equal(e[0], key) {
			return e[1].(string)
		}
	}
	text, _ := json.Marshal(key)
	*l.missing = append(*l.missing, string(text))
	return "<<no recorded A11 answer>>"
}

func optInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
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
			mark = []any{position[0], position[1]}
		}
		c = []any{"YAMLError", "", cause.Error(), mark}
	}
	return l.lookup([]any{"src", message, text, optInt(line), optInt(column), c, sourceLabel})
}

// recordedError is an exception a recorded collaborator raised.
func recordedError(entry map[string]any) error {
	class, message := mapStr(entry, "raise"), mapStr(entry, "message")
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
	row     map[string]any
	root    string
	repo    *repo.Repository
	ctx     context.Context
	mu      sync.Mutex
	answers map[string][]map[string]any
	calls   []any
	ids     []string
	aggs    []any
}

func newWorld(t *testing.T, row map[string]any, r *repo.Repository) *world {
	w := &world{t: t, row: row, root: r.DataDir(), answers: map[string][]map[string]any{}}
	w.repo, w.ctx = r, repo.WithRepository(context.Background(), r)
	for rel, content := range mapObj(row, "files") {
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
	for _, item := range mapList(row, "fetch") {
		call := item.([]any)
		key := call[0].(string) + "|" + anyStr(call[1]) + "|" + anyStr(call[2])
		w.answers[key] = append(w.answers[key], call[3].(map[string]any))
	}
	for _, id := range mapList(row, "ids") {
		w.ids = append(w.ids, id.(string))
	}
	w.aggs = mapList(row, "agg")
	return w
}

func (w *world) fetcher(label string) refresh.FetchSourceFn {
	return func(ctx context.Context, subID, url string) (*refresh.SourceResult, error) {
		w.mu.Lock()
		w.calls = append(w.calls, []any{label, subID, url})
		key := label + "|" + subID + "|" + url
		queue := w.answers[key]
		var entry map[string]any
		if len(queue) > 0 {
			entry, w.answers[key] = queue[0], queue[1:]
		}
		w.mu.Unlock()
		if entry == nil {
			return nil, &fakeErr{"ReplayError", "no recorded fetch answer for " + key}
		}
		if _, hasRaise := entry["raise"]; hasRaise {
			return nil, recordedError(entry)
		}
		var traffic *refresh.Traffic
		if tObj := mapObj(entry, "traffic"); tObj != nil {
			traffic = &refresh.Traffic{}
			_ = decodeJSON(tObj, traffic)
		}
		return &refresh.SourceResult{
			Text:      mapStr(entry, "text"),
			Source:    mapStr(entry, "source"),
			Traffic:   traffic,
			FetchedAt: mapStr(entry, "fetched_at"),
		}, nil
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

// ConvertProxy answers Sub-Store conversions from the recording.
func (w *world) ConvertProxy(_ context.Context, proxyString string) (*yamlx.Node, error) {
	for _, item := range mapList(w.row, "convert") {
		call := item.([]any)
		if call[0] == proxyString {
			if call[2] == nil {
				return nil, nil
			}
			return graphFromAny(call[2])
		}
	}
	return nil, nil
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func (w *world) aggregation(ctx context.Context, agg model.Aggregation, _ *model.Config, _ *yamlx.Node, _ *DeliverySnapshot, fetch refresh.FetchSourceFn) (*Artifact, error) {
	w.mu.Lock()
	var entry []any
	for i, item := range w.aggs {
		e := item.([]any)
		if testjson.Equal(e[0], agg.ID) {
			entry = e
			w.aggs = append(append([]any{}, w.aggs[:i]...), w.aggs[i+1:]...)
			break
		}
	}
	w.mu.Unlock()
	if entry == nil {
		return nil, &fakeErr{"ReplayError", "no recorded aggregation answer for " + agg.ID}
	}
	for _, item := range asList(entry[1]) {
		call := item.([]any)
		_, _ = fetch(ctx, anyStr(call[0]), anyStr(call[1]))
	}
	answer := entry[2].(map[string]any)
	_, hasRaise := answer["raise"]
	_, hasStrategy := answer["strategy"]
	if hasRaise && hasStrategy {
		return nil, recordedError(answer)
	}
	value, err := artifactFromMap(answer)
	if err != nil {
		return nil, err
	}
	value.HasName = false
	return value, nil
}

// files lists the files under the given prefixes (as c17_record._files).
func (w *world) files(prefixes []string) map[string]any {
	out := map[string]any{}
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
			out[filepath.ToSlash(rel)] = string(data)
			return nil
		})
	}
	return out
}

// masker replaces timestamps of the call's wall-clock window.
type masker struct{ from, to time.Time }

func newMasker(row map[string]any) masker {
	now := mapList(row, "now")
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
	case map[string]any:
		out := map[string]any{}
		for k, item := range x {
			if k == "updated_at" {
				out[k] = "<updated_at>"
				continue
			}
			out[k] = m.value(item)
		}
		return out
	}
	return v
}

// fileValues parses JSON files (cache files) so their stamps can be masked.
func (m masker) files(o map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range o {
		text, _ := v.(string)
		if strings.HasSuffix(k, ".json") {
			if parsed, err := decodeAny([]byte(text)); err == nil {
				out[k] = m.value(parsed)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func recordedSnapshot(v any) (*DeliverySnapshot, error) {
	list := asList(v)
	if list == nil {
		return nil, &InputError{Field: "snapshot"}
	}
	mainNode, err := yamlx.FromJSON([]byte(list[0].(string)))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, raw := range asList(list[1]) {
		if name, ok := raw.(string); ok {
			names[name] = true
		}
	}
	return &DeliverySnapshot{ProfileID: "default", main: mainNode, chainNames: names}, nil
}

func recordedOutcome(value any, err error) map[string]any {
	if err != nil {
		return map[string]any{"err": []any{errClass(err), err.Error()}}
	}
	return map[string]any{"out": value}
}

func sortedKey(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedAny(names []any) []any {
	out := append([]any{}, names...)
	sort.Slice(out, func(i, j int) bool { return sortedKey(out[i]) < sortedKey(out[j]) })
	return out
}

func TestRecordedPythonTests(t *testing.T) {
	rows, allRawArgs := loadRecordedWithRaw(t)
	repository := corpusRepositoryFactory(t)
	counts := map[string]int{}
	files := map[string]bool{}
	failures, unsupported := 0, 0
	for i, row := range rows {
		rawArgs := allRawArgs[i]
		fn := mapStr(row, "fn")
		if reason, hasUnsupported := row["unsupported"]; hasUnsupported {
			// Calls with a test double for the snapshot (SimpleNamespace in
			// test_subscription_refresh_policy.py) are covered by the hand-ported tests.
			if rStr, _ := reason.(string); rStr != "snapshot is a test double" {
				t.Errorf("row %d (%s %s): unsupported %v", i, fn, mapStr(row, "test"), reason)
			}
			unsupported++
			continue
		}
		var missing []string
		restoreLocations := SetLocations(newReplayLocations(row, &missing))
		args := mapList(row, "args")
		want := map[string]any{}
		if errVal, hasErr := row["err"]; hasErr {
			want["err"] = errVal
		} else {
			want["out"] = row["out"]
		}
		var got map[string]any
		var w *world
		var extra []string
		m := newMasker(row)
		switch fn {
		case "shapes":
			nodes, err := nodesFromAny(args[0])
			if err == nil {
				err = ValidateProxyNodes(nodes)
			}
			got = recordedOutcome(nil, err)
		case "parse":
			nodes, err := ParseNodes(args[0].(string), labelStr(args[1]), labelStr(args[2]))
			var out any
			if err == nil {
				out, err = nodesToAny(nodes)
			}
			got = recordedOutcome(out, err)
		case "capture":
			snap, err := captureFromMap(args[0].(string), rawArgs[1], args[2])
			if err != nil {
				got = recordedOutcome(nil, err)
				break
			}
			data, err := yamlx.ToJSON(snap.main)
			if err != nil {
				t.Fatal(err)
			}
			main, err := decodeAny(data)
			if err != nil {
				t.Fatal(err)
			}
			chainNames := make([]any, 0, len(snap.chainNames))
			for n := range snap.chainNames {
				chainNames = append(chainNames, n)
			}
			got = recordedOutcome([]any{snap.ProfileID, main, sortedAny(chainNames)}, nil)
		case "has_chains":
			snap, err := recordedSnapshot(args[0])
			var chains bool
			if err == nil {
				chains, err = snap.HasChains()
			}
			got = recordedOutcome(chains, err)
		case "validate":
			snap, err := recordedSnapshot(args[0])
			if err == nil {
				var nodes []*yamlx.Node
				nodes, err = nodesFromAny(args[1])
				if err == nil {
					err = snap.Validate(nodes, args[2].(bool), providerNamePtr(args[3]))
				}
			}
			got = recordedOutcome(nil, err)
		case "rendered_bundle":
			snap, err := recordedSnapshot(args[0])
			var chains bool
			if err == nil {
				var providers []*Artifact
				for _, p := range asList(args[1]) {
					art, aerr := artifactFromMap(p.(map[string]any))
					if aerr != nil {
						err = aerr
						break
					}
					providers = append(providers, art)
				}
				if err == nil {
					chains, err = ValidateArtifacts(snap, providers)
				}
			}
			got = recordedOutcome(chains, err)
		case "prepare_sub", "bundle", "commit_bundle", "commit_updates":
			w = newWorld(t, row, repository(t))
			restoreEnv := SetDeliveryEnv(&DeliveryEnv{
				Fetch:             w.fetcher("default"),
				SubscriptionFetch: w.fetcher("subscriptions"),
				AggregationFetch:  w.fetcher("aggregations"),
				NodeID:            w.nodeID,
				Converter:         w,
			})
			var clock fixedClock
			if now := mapList(row, "now"); len(now) > 0 {
				clock.now, _ = time.Parse(time.RFC3339Nano, now[0].(string))
			}
			restoreRefresh := setRefreshFixture(clock, w)
			restoreAgg := SetAggregationRenderer(w.aggregation)
			got = w.run(fn, args, rawArgs, m)
			restoreAgg()
			restoreRefresh()
			restoreEnv()
			if wantCalls := mapList(row, "fetch"); true {
				var calls []any
				for _, c := range wantCalls {
					calls = append(calls, c.([]any)[:3])
				}
				// Aggregation providers fetch on worker threads in Python: compare as a multiset.
				if !testjson.Equal(sortedAny(calls), sortedAny(w.calls)) {
					extra = append(extra, "fetch calls"+diffText(calls, w.calls))
				}
			}
			if len(w.ids) > 0 {
				extra = append(extra, "unused node ids")
			}
			if filesAfter, hasFiles := row["files_after"]; hasFiles {
				prefixes := []string{"shared/subscribes"}
				if fn == "commit_bundle" {
					prefixes = append(prefixes, "profiles/"+args[0].(string)+"/providers")
				}
				wantFiles, gotFiles := m.files(filesAfter.(map[string]any)), m.files(w.files(prefixes))
				if !testjson.Equal(wantFiles, gotFiles) {
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
		files[strings.SplitN(mapStr(row, "test"), "::", 2)[0]] = true
		if len(missing) > 0 {
			extra = append(extra, "A11 calls without a recorded answer: "+strings.Join(missing, "; "))
		}
		if !writerOutcomeEqual(fn, m.value(want), m.value(got)) || len(extra) > 0 {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s)%s %s", i, fn, mapStr(row, "test"), diffText(m.value(want), m.value(got)), strings.Join(extra, "\n"))
			}
		}
	}
	t.Logf("recorded calls: %d from %d test files %v, unsupported %d, failures %d", len(rows), len(files), counts, unsupported, failures)
}

func (w *world) run(fn string, args []any, rawArgs []json.RawMessage, m masker) map[string]any {
	ctx := w.ctx
	switch fn {
	case "prepare_sub":
		snap, err := recordedSnapshot(args[1])
		if err != nil {
			w.t.Fatalf("prepare_sub without a snapshot: %v", err)
		}
		var sub model.Subscription
		if err := decodeJSON(args[0], &sub); err != nil {
			return recordedOutcome(nil, err)
		}
		opts, _ := args[2].(map[string]any)
		result, err := PrepareSubscription(ctx, sub, snap, SubscriptionOptions{
			Fetch:                  w.fetcher("fetch"),
			AllowTransportFallback: mapBool(opts, "allow_transport_fallback"),
			SourcePrepared:         mapBool(opts, "source_prepared"),
			SourcePath:             labelStr(opts["source_path"]),
		})
		if err != nil {
			return recordedOutcome(nil, err)
		}
		return recordedOutcome(artifactToMap(result, false), nil)
	case "bundle":
		cfgMap, _ := args[0].(map[string]any)
		cfg, err := configFromMap(cfgMap)
		if err != nil {
			return recordedOutcome(nil, err)
		}
		mainNode, err := graphFromAny(rawArgs[1])
		if err != nil {
			return recordedOutcome(nil, err)
		}
		opts, _ := args[2].(map[string]any)
		beforeCfg, err := json.Marshal(cfg)
		if err != nil {
			w.t.Fatal(err)
		}
		beforeMain := yamlx.CopyGraph(mainNode)
		var prepared map[string]*Artifact
		if p := mapObj(opts, "prepared"); p != nil {
			prepared = map[string]*Artifact{}
			for name, value := range p {
				valMap := value.(map[string]any)
				art, err := artifactFromMap(valMap)
				if err != nil {
					return recordedOutcome(nil, err)
				}
				_, art.HasName = valMap["name"]
				prepared[name] = art
			}
		}
		req, err := bundleRequestFromAny(opts["requested"])
		if err != nil {
			return recordedOutcome(nil, err)
		}
		bundleOpts := DeliveryOptions{
			Requested:              req,
			RenderAll:              mapBool(opts, "render_all"),
			AllowTransportFallback: mapBool(opts, "allow_transport_fallback"),
			Prepared:               prepared,
		}
		if mapBool(opts, "subscription_fetch") {
			bundleOpts.SubscriptionFetch = w.fetcher("option")
		}
		progress := []any{}
		if mapBool(opts, "progress") {
			bundleOpts.Progress = func(message string, force bool) { progress = append(progress, []any{message, force}) }
		}
		bundle, err := PrepareBundle(ctx, cfg, mainNode, bundleOpts)
		afterCfg, marshalErr := json.Marshal(cfg)
		if marshalErr != nil {
			w.t.Fatal(marshalErr)
		}
		if !testjson.Equal(json.RawMessage(beforeCfg), json.RawMessage(afterCfg)) || !yamlx.Equal(beforeMain, mainNode) {
			w.t.Errorf("bundle modified its arguments (%s)", mapStr(w.row, "test"))
		}
		if wantProgress := mapList(w.row, "progress"); !testjson.Equal(wantProgress, progress) && !(len(wantProgress) == 0 && len(progress) == 0) {
			w.t.Errorf("progress (%s)%s", mapStr(w.row, "test"), diffText(wantProgress, progress))
		}
		if err != nil {
			return recordedOutcome(nil, err)
		}
		items := []any{}
		for _, item := range bundle {
			items = append(items, artifactToMap(item, true))
		}
		return recordedOutcome(items, nil)
	case "commit_bundle":
		profileID := args[0].(string)
		if _, err := w.repo.ProfileMetadata(ctx, profileID); err != nil {
			if _, err := w.repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: profileID, Name: profileID}, ""); err != nil {
				w.t.Fatal(err)
			}
		}
		var bundle []*Artifact
		for _, item := range asList(args[1]) {
			r, err := artifactFromMap(item.(map[string]any))
			if err != nil {
				return recordedOutcome(nil, err)
			}
			if len(args) > 2 && args[2] == true { // the test replaced commit_cache_updates
				r.CacheUpdates = nil
			}
			bundle = append(bundle, r)
		}
		return recordedOutcome(nil, CommitArtifacts(ctx, profileID, bundle))
	case "commit_updates":
		updates, err := cacheWritesFromAny(args[0])
		if err != nil {
			return recordedOutcome(nil, err)
		}
		return recordedOutcome(nil, CommitWrites(ctx, updates))
	}
	return nil
}

// dropTestDoubleCacheUpdates: a test's fake generate_aggregation_provider
// (test_proxy_chains.py) returns no 'cache_updates'; the real one always
// does, and Artifact always has the field. Drop an empty one where the
// recorded item has none.
func dropTestDoubleCacheUpdates(want, got map[string]any) {
	wantItems, gotItems := asList(want["out"]), asList(got["out"])
	for i := range wantItems {
		if i >= len(gotItems) {
			return
		}
		w, _ := wantItems[i].(map[string]any)
		g, _ := gotItems[i].(map[string]any)
		if w != nil && g != nil {
			if _, has := w["cache_updates"]; !has && len(asList(g["cache_updates"])) == 0 {
				delete(g, "cache_updates")
			}
		}
	}
}
