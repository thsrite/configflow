package monitor

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

type referenceCorpus struct {
	once  sync.Once
	value *jsonx.Object
	err   error
}

var corpusCache = struct {
	sync.Mutex
	files map[string]*referenceCorpus
}{files: make(map[string]*referenceCorpus)}

// referenceFields parses each immutable corpus once, then copies only the
// sections the caller used before. Tests retain independent mutable inputs;
// unrelated sections no longer get decompressed/parsed for every test.
func referenceFields(t *testing.T, path string, fields ...string) *jsonx.Object {
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
		value, err := jsonx.Parse(data)
		if err != nil {
			c.err = err
			return
		}
		var ok bool
		c.value, ok = value.(*jsonx.Object)
		if !ok {
			c.err = fmt.Errorf("%s: reference root must be an object", path)
		}
	})
	if c.err != nil {
		t.Fatal(c.err)
	}
	if len(fields) == 0 {
		t.Fatal("reference fields must be explicit")
	}
	out := jsonx.Obj()
	for _, key := range fields {
		value, ok := c.value.Get(key)
		if !ok {
			t.Fatalf("%s: missing reference section %q", path, key)
		}
		out.Set(key, jsonx.DeepCopy(value))
	}
	return out
}

func pick(values *jsonx.Object, fields ...string) *jsonx.Object {
	out := jsonx.NewObject()
	for _, field := range fields {
		out.Set(field, values.Value(field))
	}
	return out
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
		label := raw.(*jsonx.Object)
		key, _ := jsonx.Compact([]any{label.Value("plugin_tag"), label.Value("upstream")})
		addresses[string(key)] = label.GetString("address")
	}
	out := make([]any, 0, len(rows))
	for _, raw := range rows {
		row := raw.(*jsonx.Object)
		encoded, err := jsonx.Compact(row)
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
		key, _ := jsonx.Compact([]any{row.Value("tag"), row.Value("upstream")})
		result := row.Clone()
		result.Set("id", digest)
		result.Set("tag", displayAddress(fieldText(fields, "tag")))
		result.Set("upstream", displayAddress(fieldText(fields, "upstream")))
		result.Set("address", displayAddress(addresses[string(key)]))
		out = append(out, result)
	}
	return out, nil
}

// dashboardRankings adapts the historical private-helper corpus, which supplies
// only the state fields consumed by liveRankings rather than a persisted file.
// Production always enters through ReadOverview and its strict DecodeState.
func dashboardRankings(ctx context.Context, m Manager, raw *jsonx.Object, params query, snapshot, state *jsonx.Object, recorders []any) (*jsonx.Object, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return nil, err
	}
	var stamp SnapshotIdentity
	if err := metricJSON(snapshot, &stamp); err != nil {
		return nil, err
	}
	var base stateBase
	if err := metricJSON(state, &base); err != nil {
		return nil, err
	}
	tags, valid := nativeTags(recorders)
	if !valid {
		return nil, invalidQuery()
	}
	s := DashboardState{StartedAtMS: base.StartedAtMS, Generation: base.Generation, InstanceID: base.InstanceID, SourceRevision: base.SourceRevision, RuntimeRevision: base.RuntimeRevision}
	result, err := liveRankings(ctx, registryFor(m), a, params, &stamp, &s, tags)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}
