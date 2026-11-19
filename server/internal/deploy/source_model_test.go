package deploy

import (
	"bytes"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func TestCapturedSourceTypedViewCachesOneOwnedSnapshot(t *testing.T) {
	source := NewSource(jsonx.Obj("profile_id", "default", "future_profile", jsonx.Obj("n", jsonx.Float(2.5)), "system_config", jsonx.Obj("server_domain", "https://config.example"), "agents", []any{}), nil)
	before, err := source.Raw()
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ViewConfig(func(config *model.Config) error {
		if config.ProfileID != "default" || !config.HasAgents || config.Profile == nil {
			t.Error("lost captured metadata")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := source.ViewConfig(func(config *model.Config) error {
				if config != source.modelConfig {
					t.Error("decoded another snapshot")
				}
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	allocations := testing.AllocsPerRun(10, func() {
		if err := source.ViewConfig(func(*model.Config) error { return nil }); err != nil {
			t.Fatal(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("cached typed view allocates: %v", allocations)
	}
	after, err := source.Raw()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("typed view changed captured snapshot", err)
	}
}
