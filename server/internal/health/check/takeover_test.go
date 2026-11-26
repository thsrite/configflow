package check

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"configflow/internal/model"
)

func TestServiceMapEncodesNativeConstructedResults(t *testing.T) {
	status := model.Int(200)
	var markers model.OrderedMap[NullableText]
	markers.Set("consent", NullableText{Value: "yes", Valid: true})
	verdicts, err := EvaluateServiceList([]RegionResult{{ID: "openai-compliance", Status: &status, Markers: markers}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(verdicts)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("service DTO map: %s %v", encoded, err)
	}
	openai, ok := verdicts.Get("openai")
	if !ok || openai.Status != "available" {
		t.Fatal(openai)
	}
}

func TestMalformedProbeTimestampDoesNotDiscardValidSiblings(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	const raw = `{"hosts":{"bad":{"checked_at":true},"valid":{"checked_at":"2030-01-01T00:00:00","extra":1.0}},"keep":"yes"}`
	if err := os.WriteFile(store.Path("synthetic"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	loaded := store.LoadEntries("synthetic", now)
	if loaded.Len() != 1 {
		t.Fatalf("expected valid sibling only: %v", loaded.Keys())
	}
	valid, ok := loaded.Get("valid")
	if !ok || valid.CheckedAt() != "2030-01-01T00:00:00" {
		t.Fatal("valid sibling lost")
	}
	var next model.OrderedMap[TimedRecord]
	next.Set("new", timedAt("2030-01-02T00:00:00"))
	if err := store.SaveEntries(t.Context(), "synthetic", next, now); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(store.Path("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Keep  string                     `json:"keep"`
		Hosts map[string]json.RawMessage `json:"hosts"`
	}
	if err := json.Unmarshal(bytes, &saved); err != nil {
		t.Fatal(err)
	}
	// The malformed entry expired on load; an undeclared top-level member
	// is not written back (O18 T1d).
	if saved.Keep != "" || len(saved.Hosts) != 2 {
		t.Fatalf("saved %s", bytes)
	}
}

func FuzzCheckTypedProjections(f *testing.F) {
	for _, seed := range []string{`{"ok":1,"total":2,"delays":[1,2]}`, `{"id":"exit","status":200,"markers":{"loc":"US"}}`, `{"ok":true,"total":"2"}`, `null`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16384 {
			return
		}
		var stats PathStats
		if json.Unmarshal([]byte(raw), &stats) == nil {
			before, _ := model.Marshal(stats)
			_ = JudgePath(&stats, &stats, DefaultJudgeOptions())
			_ = PathHealthy(&stats)
			after, _ := model.Marshal(stats)
			if string(before) != string(after) {
				t.Fatal("judge mutated statistics")
			}
		}
		var result RegionResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			_, _ = ClassifyDomain(&result, "https://example.test/")
		}
	})
}

func TestStoredHealthUsesSharedNumbersWhileRequestsStayStrict(t *testing.T) {
	for _, spelling := range []string{`60.0`, `6e1`, `"60"`} {
		var provider model.ProviderHealthCheck
		raw := []byte(`{"interval":` + spelling + `}`)
		if err := json.Unmarshal(raw, &provider); err != nil {
			t.Fatal(err)
		}
		if err := ValidateProviderHealth(&provider); err != nil {
			t.Fatalf("stored %s: %v", spelling, err)
		}
		if err := ValidateHealthJSON(raw, true); err == nil {
			t.Fatalf("request accepted non-integer token: %s", spelling)
		}
	}
	// O18 T1c: a stored member of another type reads leniently (lazy 1 keeps
	// its truth value, the others read as absent, so the default applies)
	// instead of being kept as an invalid R5 value; requests stay strict.
	for _, raw := range []string{`{"lazy":1}`, `{"interval":true}`, `{"interval":null}`, `{"url":false}`} {
		var provider model.ProviderHealthCheck
		if err := json.Unmarshal([]byte(raw), &provider); err != nil {
			t.Fatal(err)
		}
		if err := ValidateProviderHealth(&provider); err != nil {
			t.Fatalf("lenient stored %s rejected: %v", raw, err)
		}
		if err := ValidateHealthJSON([]byte(raw), true); err == nil {
			t.Fatalf("request accepted %s", raw)
		}
	}
}
