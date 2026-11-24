package provider

// The reference corpus from the real Python modules (testdata/gen.py →
// testdata/corpus.json.gz; regenerate on the test server:
// rt --back server/internal/provider/testdata/corpus.json.gz -- bash -lc
// 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/gen.py').
// deployment_locations (A11) is a fake on both sides (fakeLocations).

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
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
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
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
	return fmt.Sprintf("%T", err)
}

func decodeAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeMap(data []byte) (map[string]any, error) {
	v, err := decodeAny(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %T", v)
	}
	return m, nil
}

func decodeJSON(value any, target any) error {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return err
	}
	if err := jsondecode.Unmarshal(data, target); err != nil {
		return &InputError{Field: "record", Cause: err}
	}
	return nil
}

func graphFromAny(value any) (*yamlx.Node, error) {
	if raw, ok := value.(json.RawMessage); ok {
		return yamlx.FromJSON(raw)
	}
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}

func nodesFromAny(value any) ([]*yamlx.Node, error) {
	node, err := graphFromAny(value)
	if err != nil {
		return nil, err
	}
	if node.Kind != yaml.SequenceNode {
		return nil, shapeError(msgProxyList, FieldPath{{Key: "proxies"}})
	}
	return node.Content, nil
}

func nodesToAny(nodes []*yamlx.Node) ([]any, error) {
	out := make([]any, 0, len(nodes))
	for _, node := range nodes {
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		v, err := decodeAny(data)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
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

func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func mapObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	o, _ := m[key].(map[string]any)
	return o
}

func mapList(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	l, _ := m[key].([]any)
	return l
}

func anyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
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

func labelStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if !x {
			return ""
		}
		return "True"
	case string:
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil && i == 0 {
			return ""
		}
		if f, err := x.Float64(); err == nil && f == 0 {
			return ""
		}
		return x.String()
	case int:
		if x == 0 {
			return ""
		}
		return strconv.Itoa(x)
	case int64:
		if x == 0 {
			return ""
		}
		return strconv.FormatInt(x, 10)
	case float64:
		if x == 0 {
			return ""
		}
		return fmt.Sprint(x)
	default:
		return anyStr(v)
	}
}

func providerNamePtr(value any) *string {
	name, ok := value.(string)
	if !ok {
		return nil
	}
	return &name
}

func configFromMap(value map[string]any) (*model.Config, error) {
	var profile model.Profile
	fields := map[string]any{}
	if v, ok := value["proxy_groups"]; ok {
		fields["proxy_groups"] = v
	}
	if err := decodeJSON(fields, &profile); err != nil {
		return nil, err
	}
	var catalog struct {
		ProfileID     string               `json:"profile_id"`
		Subscriptions []model.Subscription `json:"subscriptions"`
		Nodes         []model.Node         `json:"nodes"`
		Aggregations  []model.Aggregation  `json:"subscription_aggregations"`
	}
	catFields := map[string]any{}
	for _, key := range []string{"profile_id", "subscriptions", "nodes", "subscription_aggregations"} {
		if v, ok := value[key]; ok {
			catFields[key] = v
		}
	}
	if err := decodeJSON(catFields, &catalog); err != nil {
		return nil, err
	}
	// gen.py's fake aggregation provider reads a fixture-only member that the
	// plain Aggregation record does not declare; keep it beside the record.
	if items, ok := value["subscription_aggregations"].([]any); ok {
		for _, item := range items {
			if fields, ok := item.(map[string]any); ok {
				if content, ok := fields["fake_content"].(string); ok {
					if id, ok := fields["id"].(string); ok {
						fakeContents.Store(id, content)
					}
				}
			}
		}
	}
	return &model.Config{
		Profile:       &profile,
		ProfileID:     catalog.ProfileID,
		Subscriptions: catalog.Subscriptions,
		Nodes:         catalog.Nodes,
		Aggregations:  catalog.Aggregations,
	}, nil
}

func captureFromMap(profileID string, main, profile any) (*DeliverySnapshot, error) {
	graph, err := graphFromAny(main)
	if err != nil {
		return nil, err
	}
	var typed *model.Profile
	if profileMap, ok := profile.(map[string]any); ok && profileMap != nil {
		typed = &model.Profile{}
		groups := map[string]any{}
		if v, ok := profileMap["proxy_groups"]; ok {
			groups["proxy_groups"] = v
		}
		if err := decodeJSON(groups, typed); err != nil {
			return nil, err
		}
	}
	return CaptureDelivery(profileID, graph, typed)
}

func outcomeOf(value any, err error) map[string]any {
	if err != nil {
		out := map[string]any{"error": errClass(err)}
		if isStrategyReference(err) {
			out["message"] = err.Error()
		}
		return out
	}
	return map[string]any{"output": value}
}

func expectedOf(row map[string]any) map[string]any {
	if errVal, ok := row["error"]; ok {
		out := map[string]any{"error": errVal}
		if msg, ok := row["message"]; ok {
			out["message"] = msg
		}
		return out
	}
	return map[string]any{"output": row["output"]}
}

func loadCorpus(t *testing.T) []map[string]any {
	rows, _ := loadCorpusWithRaw(t)
	return rows
}

func loadCorpusWithRaw(t *testing.T) ([]map[string]any, []json.RawMessage) {
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
	var rawRows []json.RawMessage
	if err := json.Unmarshal(data, &rawRows); err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, len(rawRows))
	for i, raw := range rawRows {
		row, err := decodeMap(raw)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = row
	}
	return rows, rawRows
}

func artifactToMap(a *Artifact, bundle bool) map[string]any {
	o := map[string]any{}
	if a.HasName {
		o["name"] = a.Name
	}
	o["content"] = a.Content
	updates := []any{}
	for _, u := range a.CacheUpdates {
		var nodesAny any = []any{}
		if data, err := jsonresp.Encode(u.Nodes); err == nil {
			if parsed, err := decodeAny(data); err == nil && parsed != nil {
				nodesAny = parsed
			}
		}
		var metaAny any
		if u.Metadata != nil {
			if data, err := jsonresp.Encode(u.Metadata); err == nil {
				metaAny, _ = decodeAny(data)
			}
		}
		updates = append(updates, []any{u.SubID, nodesAny, metaAny})
	}
	o["cache_updates"] = updates
	if a.AggregationID != "" {
		o["aggregation_id"] = a.AggregationID
	}
	if bundle {
		o["url"] = a.URL
		o["local_path"] = a.LocalPath
	}
	if len(a.Extra) > 0 {
		if extra, err := decodeMap(a.Extra); err == nil {
			for k, v := range extra {
				o[k] = v
			}
		}
	}
	return o
}

func artifactFromMap(o map[string]any) (*Artifact, error) {
	a := &Artifact{
		Content:       mapStr(o, "content"),
		AggregationID: mapStr(o, "aggregation_id"),
		URL:           mapStr(o, "url"),
		CacheUpdates:  []CacheWrite{},
	}
	if name, ok := o["name"]; ok {
		a.Name, a.HasName = anyStr(name), true
		if s, ok := name.(string); ok {
			a.Name = s
		}
	}
	if lp, ok := o["local_path"]; ok && lp != nil {
		if err := decodeJSON(lp, &a.LocalPath); err != nil {
			return nil, err
		}
	}
	if updates, ok := o["cache_updates"]; ok && updates != nil {
		parsed, err := cacheWritesFromAny(updates)
		if err != nil {
			return nil, err
		}
		a.CacheUpdates = parsed
	}
	extra := map[string]any{}
	for k, v := range o {
		switch k {
		case "name", "content", "cache_updates", "aggregation_id", "url", "local_path":
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		data, err := jsonresp.Encode(extra)
		if err != nil {
			return nil, err
		}
		a.Extra = data
	}
	return a, nil
}

func cacheWritesFromAny(v any) ([]CacheWrite, error) {
	out := []CacheWrite{}
	for _, item := range asList(v) {
		u := item.([]any)
		var w CacheWrite
		w.SubID = anyStr(u[0])
		if err := decodeJSON(u[1], &w.Nodes); err != nil {
			return nil, err
		}
		if len(u) > 2 && u[2] != nil {
			w.Metadata = &substore.CacheMetadata{}
			if err := decodeJSON(u[2], w.Metadata); err != nil {
				return nil, err
			}
		}
		out = append(out, w)
	}
	return out, nil
}

func diffText(want, got any) string {
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	return "\nwant " + string(w) + "\n got " + string(g)
}

func TestCorpus(t *testing.T) {
	corpusPV := New(Options{Locations: fakeLocations{}})
	rows, rawRows := loadCorpusWithRaw(t)
	repository := corpusRepositoryFactory(t)
	nativeSnapshots := nativeSnapshotExpectations(t, rows)
	for index, expected := range nativeDeliveryReferences(t, rows) {
		nativeSnapshots[index] = expected
	}
	counts := map[string]int{}
	failures := 0
	for i, row := range rows {
		kind := mapStr(row, "kind")
		input := row["input"]
		var rawInput struct {
			Input struct {
				Main      json.RawMessage            `json:"main"`
				Caches    map[string]json.RawMessage `json:"caches"`
				Downloads map[string]json.RawMessage `json:"downloads"`
			} `json:"input"`
		}
		_ = json.Unmarshal(rawRows[i], &rawInput)
		var got map[string]any
		var extra func(*testing.T)
		switch kind {
		case "shapes":
			nodes, err := nodesFromAny(input)
			if err == nil {
				err = ValidateProxyNodes(nodes)
			}
			got = outcomeOf(nil, err)
		case "parse":
			in, _ := input.(map[string]any)
			nodes, err := corpusPV.ParseNodes(mapStr(in, "text"), labelStr(in["name"]), labelStr(in["path"]))
			var out any
			if err == nil {
				out, err = nodesToAny(nodes)
			}
			got = outcomeOf(out, err)
		case "snapshot":
			in, _ := input.(map[string]any)
			got = snapshotRow(in)
		case "rendered_bundle":
			in, _ := input.(map[string]any)
			got = outcomeOf(func() (any, error) {
				snap, err := captureFromMap("default", rawInput.Input.Main, in["profile"])
				if err != nil {
					return nil, err
				}
				var providers []*Artifact
				for _, p := range mapList(in, "providers") {
					art, err := artifactFromMap(p.(map[string]any))
					if err != nil {
						return nil, err
					}
					providers = append(providers, art)
				}
				return corpusPV.ValidateArtifacts(snap, providers)
			}())
		case "prepare_sub", "bundle":
			in, _ := input.(map[string]any)
			var check func(t *testing.T, files map[string]string)
			got, check = scenarioRow(t, kind, in, rawInput.Input.Main, rawInput.Input.Caches, rawInput.Input.Downloads, row, repository(t))
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
			if testjson.Equal(want, got) {
				t.Errorf("row %d: known difference (%s) no longer differs", i, reason)
			}
			continue
		}
		if kind == "snapshot" && !testjson.Equal(want, got) {
			w, _ := want["output"].([]any)
			g, _ := got["output"].([]any)
			for j := 0; j < len(w) && j < len(g); j++ {
				if !testjson.Equal(w[j], g[j]) {
					t.Errorf("row %d snapshot outcome %d: %s", i, j, diffText(w[j], g[j]))
					break
				}
			}
		}
		sameOutput := writerOutcomeEqual(kind, want, got)
		inMap, _ := input.(map[string]any)
		nativeMatch := !sameOutput && nativeProviderOutcome(kind, inMap, want, got)
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
func knownDifference(_ string, _ any) string {
	return ""
}

func snapshotRow(in map[string]any) map[string]any {
	snap, err := captureFromMap("default", in["main"], in["profile"])
	if err != nil {
		return outcomeOf(nil, err)
	}
	names := make([]string, 0, len(snap.chainNames))
	for n := range snap.chainNames {
		names = append(names, n)
	}
	sort.Strings(names)
	namesAny := make([]any, len(names))
	for i, n := range names {
		namesAny[i] = n
	}
	out := []any{map[string]any{"chain_names": namesAny}}
	chains, err := snap.HasChains()
	out = append(out, outcomeOf(chains, err))
	for _, payload := range mapList(in, "payloads") {
		for _, force := range []bool{false, true} {
			for _, name := range []any{nil, "Feed", "Other", "Nope", "Alias"} {
				nodes, err := nodesFromAny(payload)
				if err == nil {
					err = snap.Validate(nodes, force, providerNamePtr(name))
				}
				out = append(out, outcomeOf(nil, err))
			}
		}
	}
	return map[string]any{"output": out}
}

// fakeContents holds the fixture-only fake_content of aggregations by id.
var fakeContents sync.Map

// scenario is gen.py's Scenario: a fresh repository with seeded caches,
// fake fetchers keyed "id|url" and the fake aggregation provider.
type scenario struct {
	spec  map[string]any
	mu    sync.Mutex
	calls []any
}

func (s *scenario) record(call ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *scenario) fetcher(label string) refresh.FetchSourceFn {
	return func(ctx context.Context, subID, url string) (*refresh.SourceResult, error) {
		s.record(label, subID, url)
		entry := mapObj(mapObj(s.spec, "fetch"), subID+"|"+url)
		if entry == nil {
			return nil, &fakeErr{"ConnectionError", "no fake answer"}
		}
		if raise, ok := entry["raise"]; ok {
			message := mapStr(entry, "message")
			if raise == "StrategyReferenceError" {
				return nil, &rules.StrategyReferenceError{Message: message}
			}
			return nil, &fakeErr{raise.(string), message}
		}
		fetchedAt := mapStr(entry, "fetched_at")
		if fetchedAt == "" && !mapBool(entry, "tuple") {
			fetchedAt = corpusNowISO
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
			FetchedAt: fetchedAt,
		}, nil
	}
}

func clearYAMLStyle(n *yamlx.Node) {
	if n == nil {
		return
	}
	n.Style = 0
	for _, c := range n.Content {
		clearYAMLStyle(c)
	}
}

func (s *scenario) fakeAggregation(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, _ *DeliverySnapshot, fetch refresh.FetchSourceFn) (*Artifact, error) {
	s.record("agg", agg.ID, false, true, main != nil)
	var proxies []*yamlx.Node
	for _, sid := range agg.Subscriptions {
		var sub *model.Subscription
		for i := range config.Subscriptions {
			if config.Subscriptions[i].ID == sid {
				sub = &config.Subscriptions[i]
				break
			}
		}
		if sub == nil {
			return nil, &InputError{Field: "resource.id"}
		}
		result, err := fetch(ctx, sid, sub.URL)
		if err != nil {
			if isStrategyReference(err) {
				return nil, err
			}
			s.record("agg-fetch-error", sid, errClass(err))
			continue
		}
		doc, err := yamlx.Parse([]byte(result.Text))
		if err != nil {
			return nil, err
		}
		if list := yamlx.Get(doc, "proxies"); list != nil && list.Kind == yaml.SequenceNode {
			for _, item := range list.Content {
				clearYAMLStyle(item)
				proxies = append(proxies, item)
			}
		}
	}
	for _, nid := range agg.Nodes {
		proxies = append(proxies, yamlx.Map(
			yamlx.Str("name"), yamlx.Str("node-"+nid),
			yamlx.Str("type"), yamlx.Str("http"),
			yamlx.Str("server"), yamlx.Str("example.test"),
			yamlx.Str("port"), yamlx.Int(80),
		))
	}
	var content string
	if fake, ok := fakeContents.Load(agg.ID); ok {
		content = fake.(string)
	} else {
		data, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(proxies...)))
		if err != nil {
			return nil, err
		}
		content = string(data)
	}
	return &Artifact{
		Content:      content,
		CacheUpdates: []CacheWrite{},
		Extra:        json.RawMessage(fmt.Sprintf(`{"total_count":%d}`, len(proxies))),
	}, nil
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

func bundleRequestFromAny(v any) (*BundleRequest, error) {
	list, ok := v.([]any)
	if !ok || len(list) < 2 {
		return nil, nil
	}
	kind, _ := list[0].(string)
	req := &BundleRequest{Kind: kind}
	switch kind {
	case "subscription":
		var sub model.Subscription
		if err := decodeJSON(list[1], &sub); err != nil {
			return nil, err
		}
		req.Subscription = &sub
	case "aggregation":
		var agg model.Aggregation
		if err := decodeJSON(list[1], &agg); err != nil {
			return nil, err
		}
		req.Aggregation = &agg
	}
	return req, nil
}

func writeSharedJSONFile(t *testing.T, ctx context.Context, r *repo.Repository, rel string, value any) {
	t.Helper()
	var raw []byte
	var err error
	if rVal, ok := value.(json.RawMessage); ok {
		raw = rVal
	} else {
		raw, err = jsonresp.Encode(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		t.Fatal(err)
	}
	indented.WriteByte('\n')
	if _, err := r.WriteSharedText(ctx, rel, indented.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func scenarioRow(t *testing.T, kind string, in map[string]any, rawMain json.RawMessage, rawCaches, rawDownloads map[string]json.RawMessage, row map[string]any, r *repo.Repository) (map[string]any, func(*testing.T, map[string]string)) {
	root := r.DataDir()
	ctx := context.Background()
	for id, cache := range rawCaches {
		writeSharedJSONFile(t, ctx, r, "subscribes/"+id+".json", cache)
	}
	for url, doc := range rawDownloads {
		writeSharedJSONFile(t, ctx, r, refresh.DownloadCachePath(url), doc)
	}
	s := &scenario{spec: in}
	pv := New(Options{
		Repo:         r,
		Refresh:      newRefreshFixture(r, fixedClock{corpusNow}, nilNodeConverter{}),
		Locations:    fakeLocations{},
		Aggregations: s.fakeAggregation,
		DeliveryEnv: &DeliveryEnv{
			Fetch:             s.fetcher("default"),
			SubscriptionFetch: s.fetcher("subscriptions"),
			AggregationFetch:  s.fetcher("aggregations"),
			NodeID:            SequentialNodeIDs(),
			Converter:         nilNodeConverter{},
		},
	})

	var got map[string]any
	switch kind {
	case "prepare_sub":
		snap, err := captureFromMap("default", rawMain, in["profile"])
		if err != nil {
			got = outcomeOf(nil, err)
			break
		}
		var sub model.Subscription
		if err := decodeJSON(in["sub"], &sub); err != nil {
			got = outcomeOf(nil, err)
			break
		}
		opts := mapObj(in, "opts")
		result, err := pv.PrepareSubscription(ctx, sub, snap, SubscriptionOptions{
			AllowTransportFallback: mapBool(opts, "allow_transport_fallback"),
			SourcePrepared:         mapBool(opts, "source_prepared"),
			SourcePath:             labelStr(opts["source_path"]),
		})
		if err != nil {
			got = outcomeOf(nil, err)
		} else {
			got = outcomeOf(artifactToMap(result, false), nil)
		}
	case "bundle":
		opts := mapObj(in, "opts")
		cfg, err := configFromMap(mapObj(in, "config"))
		if err != nil {
			got = outcomeOf(nil, err)
			break
		}
		mainNode, err := graphFromAny(rawMain)
		if err != nil {
			got = outcomeOf(nil, err)
			break
		}
		beforeMain := yamlx.CopyGraph(mainNode)
		var progress []any
		var prepared map[string]*Artifact
		if p := mapObj(opts, "prepared"); p != nil {
			prepared = map[string]*Artifact{}
			for name, value := range p {
				art, err := artifactFromMap(value.(map[string]any))
				if err != nil {
					t.Fatal(err)
				}
				art.HasName = false
				prepared[name] = art
			}
		}
		req, err := bundleRequestFromAny(opts["requested"])
		if err != nil {
			got = outcomeOf(nil, err)
			break
		}
		bundle, err := pv.PrepareBundle(ctx, cfg, mainNode, DeliveryOptions{
			Requested:              req,
			RenderAll:              mapBool(opts, "render_all"),
			AllowTransportFallback: mapBool(opts, "allow_transport_fallback"),
			Prepared:               prepared,
			Progress:               func(message string, force bool) { progress = append(progress, []any{message, force}) },
		})
		if !yamlx.Equal(beforeMain, mainNode) {
			t.Errorf("bundle modified its main graph")
		}
		if err == nil {
			err = pv.CommitArtifacts(ctx, "default", bundle)
		}
		if err != nil {
			got = outcomeOf(nil, err)
		} else {
			items := []any{}
			for _, item := range bundle {
				items = append(items, artifactToMap(item, true))
			}
			if progress == nil {
				progress = []any{}
			}
			got = outcomeOf(map[string]any{"bundle": items, "progress": progress}, nil)
		}
	}
	calls := s.calls
	if calls == nil {
		calls = []any{}
	}
	files := dumpFiles(t, root)
	return got, func(t *testing.T, _ map[string]string) {
		if want := row["calls"]; !testjson.Equal(want, calls) {
			t.Errorf("%s calls for %s%s", kind, diffText(in, nil)[6:], diffText(want, calls))
		}
		wantFiles := mapObj(row, "files")
		gotFiles := map[string]any{}
		for _, k := range sortedKeys(files) {
			gotFiles[k] = maskUpdatedAt(files[k], mapStr(wantFiles, k))
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

// nilNodeConverter is convert_proxy_string patched to return None.
type nilNodeConverter struct{}

func (nilNodeConverter) ConvertProxy(context.Context, string) (*yamlx.Node, error) { return nil, nil }

// nativeParseOutcome accepts only native parser diagnostics or the D1 policy.
// The complete Go error must identify the same owner and source, and include
// the exact facade error. Valid-input differences cannot enter this branch.
func nativeParseOutcome(input, want, got map[string]any) bool {
	_, err := yamlx.Parse([]byte(mapStr(input, "text")))
	if err == nil || mapStr(got, "error") != "DialerReferenceError" || len(got) != 2 {
		return false
	}
	if _, hasErr := want["error"]; !hasErr && !errors.Is(err, yamlx.ErrDuplicateKey) {
		return false
	}
	prefix := ""
	if mapStr(input, "name") != "" || mapStr(input, "path") != "" {
		prefix = "CTX[订阅|" + mapStr(input, "name") + "|" + mapStr(input, "path") + "]"
	}
	message := mapStr(got, "message")
	return strings.HasPrefix(message, prefix+"SRC[Provider YAML 内容无效|") && strings.Contains(message, "|本次订阅返回的内容|YAMLError:None:"+err.Error()+":")
}
