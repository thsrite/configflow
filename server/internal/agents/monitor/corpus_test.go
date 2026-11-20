package monitor

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

type referenceCorpus struct {
	once  sync.Once
	value map[string]json.RawMessage
	err   error
}

var corpusCache = struct {
	sync.Mutex
	files map[string]*referenceCorpus
}{files: make(map[string]*referenceCorpus)}

// referenceFields parses each immutable corpus once, then decodes only the
// sections the caller requested into independent mutable values.
func referenceFields(t *testing.T, path string, fields ...string) map[string]any {
	t.Helper()
	corpusCache.Lock()
	c := corpusCache.files[path]
	if c == nil {
		c = &referenceCorpus{}
		corpusCache.files[path] = c
	}
	corpusCache.Unlock()
	c.once.Do(func() {
		f, err := os.Open(path)
		if err != nil {
			c.err = err
			return
		}
		defer f.Close()
		reader, err := gzip.NewReader(f)
		if err != nil {
			c.err = err
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			c.err = err
			return
		}
		var sections map[string]json.RawMessage
		if err := json.Unmarshal(data, &sections); err != nil {
			c.err = fmt.Errorf("%s: reference root must be an object: %w", path, err)
			return
		}
		c.value = sections
	})
	if c.err != nil {
		t.Fatal(c.err)
	}
	if len(fields) == 0 {
		t.Fatal("reference fields must be explicit")
	}
	out := make(map[string]any, len(fields))
	for _, key := range fields {
		raw, ok := c.value[key]
		if !ok {
			t.Fatalf("%s: missing reference section %q", path, key)
		}
		out[key] = parseJSONVal(t, raw)
	}
	return out
}

func parseJSONVal(t testing.TB, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	return v
}

func parseJSONMap(t testing.TB, raw []byte) map[string]any {
	t.Helper()
	m, ok := parseJSONVal(t, raw).(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %s", raw)
	}
	return m
}

func mustMarshal(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func toJSONVal(t testing.TB, v any) any {
	t.Helper()
	return parseJSONVal(t, mustMarshal(t, v))
}

func toJSONMap(t testing.TB, v any) map[string]any {
	t.Helper()
	return parseJSONMap(t, mustMarshal(t, v))
}

func cloneVal(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneVal(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneVal(val)
		}
		return out
	default:
		return v
	}
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return cloneVal(m).(map[string]any)
}

func updateMap(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

func replaceMap(dst, src map[string]any) {
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range src {
		dst[k] = v
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asInt64OK(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Trunc(x) != x {
			return 0, false
		}
		return int64(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
			return 0, false
		}
		return int64(f), true
	}
	return 0, false
}

func asInt64(v any) int64 {
	n, _ := asInt64OK(v)
	return n
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func numString(v any) string {
	switch x := v.(type) {
	case json.Number:
		return string(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return fmt.Sprint(v)
}

func pick(values map[string]any, fields ...string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		out[field] = values[field]
	}
	return out
}

func decodeAgentMap(m map[string]any) (*model.Agent, error) {
	if m == nil {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err := jsondecode.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func mustAgentMap(m map[string]any) *model.Agent {
	a, err := decodeAgentMap(m)
	if err != nil {
		panic(err)
	}
	return a
}

func cloneAgent(a *model.Agent) *model.Agent {
	if a == nil {
		return nil
	}
	raw, err := model.Marshal(a)
	if err != nil {
		panic(err)
	}
	var out model.Agent
	if err := jsondecode.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return &out
}

func setAgentField(a *model.Agent, key string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	if err := a.SetJSON(key, raw); err != nil {
		if fErr := a.Fields.Set(key, raw); fErr != nil {
			panic(err)
		}
	}
}

func deleteAgentField(a *model.Agent, key string) {
	if err := a.Clear(key); err != nil {
		panic(err)
	}
}

// NewCounts builds Counts from key, count pairs.
func NewCounts(pairs ...any) *Counts {
	c := &Counts{Values: map[string]int64{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		c.Add(pairs[i].(string), int64(pairs[i+1].(int)))
	}
	return c
}

// publicUpstreams preserves the shape-free historical helper fixture in tests.
// Production projection is PublicUpstream over the typed MetricSummary; the
// legacy null/number synthetic labels cannot arise from validated metrics.
// Identity and display checks still call the native production primitives.
func publicUpstreams(rows, labels []any) ([]any, error) {
	addresses := map[string]string{}
	for _, raw := range labels {
		label := asMap(raw)
		key, _ := json.Marshal([]any{label["plugin_tag"], label["upstream"]})
		addresses[string(key)] = asStr(label["address"])
	}
	out := make([]any, 0, len(rows))
	for _, raw := range rows {
		row := asMap(raw)
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		var fields model.Fields
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return nil, err
		}
		parts := make([]json.RawMessage, 0, 2)
		for _, name := range []string{"tag", "upstream"} {
			value, present := fields.Get(name)
			if !present {
				value = []byte("null")
			}
			parts = append(parts, value)
		}
		digest, err := digestIdentity(parts)
		if err != nil {
			return nil, err
		}
		key, _ := json.Marshal([]any{row["tag"], row["upstream"]})
		result := cloneMap(row)
		result["id"] = digest
		result["tag"] = displayAddress(fieldText(fields, "tag"))
		result["upstream"] = displayAddress(fieldText(fields, "upstream"))
		result["address"] = displayAddress(addresses[string(key)])
		out = append(out, result)
	}
	return out, nil
}

// dashboardRankings adapts the historical private-helper corpus, which supplies
// only the state fields consumed by liveRankings rather than a persisted file.
// Production always enters through ReadOverview and its strict DecodeState.
func dashboardRankings(ctx context.Context, m Registry, a *model.Agent, params query, snapshot, state map[string]any, recorders []any) (*Rankings, error) {
	snapRaw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var stamp SnapshotIdentity
	if err := jsondecode.Unmarshal(snapRaw, &stamp); err != nil {
		return nil, err
	}
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	var base stateBase
	if err := jsondecode.Unmarshal(stateRaw, &base); err != nil {
		return nil, err
	}
	tags := make([]string, len(recorders))
	for i, item := range recorders {
		tag, ok := item.(string)
		if !ok {
			return nil, invalidQuery()
		}
		tags[i] = tag
	}
	s := DashboardState{StartedAtMS: base.StartedAtMS, Generation: base.Generation, InstanceID: base.InstanceID, SourceRevision: base.SourceRevision, RuntimeRevision: base.RuntimeRevision}
	return liveRankings(ctx, m, a, params, &stamp, &s, tags)
}
