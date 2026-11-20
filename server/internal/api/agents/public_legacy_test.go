package agents

// Test-only codec keeps the frozen Python-era projection fixtures unchanged.
import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func publicAgent(record map[string]any) map[string]any {
	cleaned := make(map[string]any, len(record))
	for key, value := range record {
		if value == repo.AgentHistoryNotRead {
			continue
		}
		cleaned[key] = value
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return nil
	}
	var value model.Agent
	if jsondecode.Unmarshal(raw, &value) != nil {
		return nil
	}
	public, err := publicAgentModel(&value)
	if err != nil {
		return nil
	}
	raw, err = json.Marshal(public)
	if err != nil {
		return nil
	}
	parsed, err := decodeJSONAny(raw)
	if err != nil {
		return nil
	}
	out, _ := parsed.(map[string]any)
	return out
}

func decodeJSONAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseJSONMap(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v, err := decodeJSONAny(data)
	if err != nil {
		t.Fatalf("parse JSON (%v): %s", err, data)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %T: %s", v, data)
	}
	return m
}

func parseBodyMap(t testing.TB, r *httpxtest.Result) map[string]any {
	t.Helper()
	return parseJSONMap(t, r.Body)
}

func wantBodyJSON(t testing.TB, r *httpxtest.Result, want any) *httpxtest.Result {
	t.Helper()
	if !testjson.Equal(json.RawMessage(r.Body), want) {
		t.Fatalf("body %s\nwant %v\ndiff: %s", r.Body, want, testjson.Diff(json.RawMessage(r.Body), want))
	}
	return r
}

func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func mapInt(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		if f, err := v.Float64(); err == nil {
			return int64(f)
		}
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func mapObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func mapList(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	v, _ := m[key].([]any)
	return v
}

func mapHas(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func cloneMap(t testing.TB, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return parseJSONMap(t, raw)
}

func mustRawJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func setAgentJSONFields(t testing.TB, a *model.Agent, fields map[string]any) {
	t.Helper()
	doc := parseJSONMap(t, mustRawJSON(t, a))
	for k, v := range fields {
		doc[k] = v
	}
	var next model.Agent
	if err := json.Unmarshal(mustRawJSON(t, doc), &next); err != nil {
		t.Fatal(err)
	}
	*a = next
}
