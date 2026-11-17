package deploy

import (
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

func TestContentInputProjection(t *testing.T) {
	base := jsonx.Obj("profile_id", "default", "profile_name", "Original", "plugin", jsonx.Obj("created_at", "first"))
	initial, err := NewSource(base, nil).InputKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"agents", "backup", "_revision", "updated_at", "created_at", "name", "description"} {
		copy := base.DeepCopy()
		copy.Set(key, "runtime metadata")
		got, err := NewSource(copy, nil).InputKey()
		if err != nil || got != initial {
			t.Fatalf("metadata %s: %v", key, err)
		}
	}
	for _, key := range []string{"profile_name", "nested"} {
		copy := base.DeepCopy()
		if key == "profile_name" {
			copy.Set(key, "Renamed")
		} else {
			copy.GetObject("plugin").Set("created_at", "second")
		}
		got, err := NewSource(copy, nil).InputKey()
		if err != nil || got == initial {
			t.Fatalf("generation input %s ignored: %v", key, err)
		}
	}
	source := NewSource(base, nil)
	base.Set("profile_name", "Caller mutation")
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
	a := NewSource(jsonx.Obj("profile_name", "Original"), nil)
	b := NewSource(jsonx.Obj("profile_name", "Renamed"), nil)
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
