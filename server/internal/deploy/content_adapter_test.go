package deploy

import (
	"encoding/json"
	"sync"
	"testing"

	"configflow/internal/model"
)

func testSource(t testing.TB, v any) *Source {
	t.Helper()
	var raw json.RawMessage
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	case string:
		raw = json.RawMessage(x)
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
	}
	src, err := SourceFromJSON(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func testTarget(t testing.TB, v any) Target {
	t.Helper()
	var raw []byte
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	case string:
		raw = []byte(x)
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
	}
	var agent model.Agent
	if err := json.Unmarshal(raw, &agent); err != nil {
		t.Fatal(err)
	}
	target, err := NewTypedTarget(&agent)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func cloneMap(t testing.TB, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContentInputProjection(t *testing.T) {
	base := map[string]any{"profile_id": "default", "profile_name": "Original", "plugin": map[string]any{"created_at": "first"}}
	initial, err := testSource(t, base).InputKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"agents", "backup", "_revision", "updated_at", "created_at", "name", "description"} {
		cp := cloneMap(t, base)
		cp[key] = "runtime metadata"
		got, err := testSource(t, cp).InputKey()
		if err != nil || got != initial {
			t.Fatalf("metadata %s: %v", key, err)
		}
	}
	for _, key := range []string{"profile_name", "nested"} {
		cp := cloneMap(t, base)
		if key == "profile_name" {
			cp[key] = "Renamed"
		} else {
			cp["plugin"].(map[string]any)["created_at"] = "second"
		}
		got, err := testSource(t, cp).InputKey()
		if err != nil || got == initial {
			t.Fatalf("generation input %s ignored: %v", key, err)
		}
	}
	source := testSource(t, base)
	base["profile_name"] = "Caller mutation"
	if got, err := source.InputKey(); err != nil || got != initial {
		t.Fatal("source was not captured")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := source.InputKey(); err != nil || got != initial {
				t.Errorf("concurrent key %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestPreviewAndContentKeysKeepTheirDistinctMetadataPolicies(t *testing.T) {
	a := testSource(t, map[string]any{"profile_name": "Original"})
	b := testSource(t, map[string]any{"profile_name": "Renamed"})
	if a.inputDigest() != b.inputDigest() {
		t.Fatal("preview metadata policy changed")
	}
	ka, err := a.InputKey()
	if err != nil {
		t.Fatal(err)
	}
	kb, err := b.InputKey()
	if err != nil || ka == kb {
		t.Fatal("content-sync ignored profile_name")
	}
}
