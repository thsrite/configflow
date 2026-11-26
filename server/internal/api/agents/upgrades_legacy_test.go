package agents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func decodeJSONAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseJSONAny(t testing.TB, data []byte) any {
	t.Helper()
	v, err := decodeJSONAny(data)
	if err != nil {
		t.Fatalf("parse JSON (%v): %s", err, data)
	}
	return v
}

func parseJSONMap(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v := parseJSONAny(t, data)
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %T: %s", v, data)
	}
	return m
}

func parseBodyAny(t testing.TB, r *httpxtest.Result) any {
	t.Helper()
	return parseJSONAny(t, r.Body)
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

func mustRawJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func cloneMap(t testing.TB, m map[string]any) map[string]any {
	t.Helper()
	return parseJSONMap(t, mustRawJSON(t, m))
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

func mapFloat(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
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

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func upgradeAgentResponse(payload any, status int) *agents.Response {
	raw, _ := json.Marshal(payload)
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: raw}
}

func registerAgentForTest(t testing.TB, ctx context.Context, m *agents.Manager, body map[string]any) (id, token string) {
	t.Helper()
	input, err := agents.DecodeRegistrationInput(mustRawJSON(t, body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.RegisterIdentity(ctx, input, agents.RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != nil {
		token = *res.Token
	}
	return res.ID, token
}

func TestN810TypedAddressSnapshotPreservesHeartbeatPreferences(t *testing.T) {
	h := newHeartbeat(t)
	manager := h.m
	if err := manager.EditAgents(h.s.Context(), "", func(_ context.Context, rows *[]model.Agent) error {
		if err := model.SetStoredMember(&(*rows)[0], "domain_discovery_enabled", []byte(`"enabled"`)); err != nil {
			return err
		}
		return model.SetStoredMember(&(*rows)[0], "traffic_history_enabled", []byte(`1`))
	}); err != nil {
		t.Fatal(err)
	}
	result := parseBodyMap(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{})).WantStatus(200))
	// Both are device-write gates (O18 T1b review): stored with another type
	// than a boolean they read as off, and the heartbeat reports them off.
	if result["domain_discovery_enabled"] != false || result["traffic_history_enabled"] != false {
		t.Fatal("typed snapshot changed existing heartbeat preferences", result)
	}
}
