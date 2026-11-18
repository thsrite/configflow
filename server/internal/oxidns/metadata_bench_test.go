package oxidns_test

import (
	"configflow/internal/ippresence"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/oxidns"
)

func metadataProfile(t testing.TB, count, unusedBytes int) *oxidns.Profile {
	t.Helper()
	type rule struct {
		ID       string `json:"id"`
		Kind     string `json:"itemType"`
		Name     string `json:"name"`
		Library  string `json:"library_rule_id"`
		Behavior string `json:"behavior"`
		URL      string `json:"url"`
	}
	type library struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Source   string `json:"source_type"`
		Behavior string `json:"behavior"`
		URL      string `json:"url"`
	}
	rules := make([]rule, count)
	libraries := make([]library, count)
	ids := make([]string, count)
	for i := range rules {
		id := fmt.Sprintf("set-%d", i)
		ids[i] = id
		rules[i] = rule{id, "ruleset", id, id, "classical", "https://rules.example/" + id}
		libraries[i] = library{id, id, "url", "classical", "https://rules.example/" + id}
	}
	value := struct {
		Rules   []rule    `json:"rule_configs"`
		Library []library `json:"rule_library"`
		System  struct {
			Token string `json:"rule_proxy_token"`
		} `json:"system_config"`
		Settings struct {
			Proxy  []string `json:"proxy_rulesets"`
			Local  string   `json:"local_dns"`
			Remote string   `json:"remote_dns"`
		} `json:"oxidns"`
		Unrelated string `json:"unrelated"`
	}{Rules: rules, Library: libraries, Unrelated: strings.Repeat("x", unusedBytes)}
	value.System.Token = "synthetic-capability"
	value.Settings.Proxy = ids
	value.Settings.Local = "udp://192.0.2.53"
	value.Settings.Remote = "udp://192.0.2.54"
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := oxidns.ProfileFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	text, err := oxidns.Generate(profile, "http://panel.example")
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(profile.Settings(), "config_mode", yamlx.Str("native"))
	yamlx.Set(profile.Settings(), "native_config", yamlx.Str(text))
	return profile
}

// BenchmarkOxiDNSMetadataChain exercises Generate -> IP-presence -> rulefetch /
// RulesetFormat using the real registered offline reader. Profile parsing stays
// outside the timed region. Unrelated data must not be decoded inside the chain.
func BenchmarkOxiDNSMetadataChain(b *testing.B) {
	repository, err := repo.Open(b.TempDir(), repo.Options{})
	if err != nil {
		b.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), repository)
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("set-%d", i)
		item := jsonx.Obj("id", id, "name", id, "source_type", "url", "behavior", "classical", "url", "https://rules.example/"+id)
		if _, err := rulefetch.WriteValidatedRuleCache(ctx, item, "DOMAIN-SUFFIX,example.com", jsonx.Obj("rule_library", []any{})); err != nil {
			b.Fatal(err)
		}
	}
	var calls atomic.Int64
	restore := oxidns.SetNativeClassicalIPLines(func(ctx context.Context, item model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
		calls.Add(1)
		return ippresence.TypedClassicalIPLines(repo.WithRepository(ctx, repository), item, library)
	})
	defer restore()

	for _, width := range []int{0, 2 << 20} {
		b.Run(fmt.Sprintf("rules128/unused%d", width), func(b *testing.B) {
			profile := metadataProfile(b, 128, width)
			b.ReportAllocs()
			calls.Store(0)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ippresence.ClearMemo()
				if _, err := oxidns.Generate(profile, "http://panel.example"); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if calls.Load() < 128*int64(b.N) {
				b.Fatal("benchmark did not reach IP-presence for every ruleset")
			}
		})
	}
}

// TestMetadataChainAllocationBound protects the entire cold validated-cache
// path. The former per-rule whole-library decode allocated over 1 GB here.
func TestMetadataChainAllocationBound(t *testing.T) {
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), repository)
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("set-%d", i)
		item := jsonx.Obj("id", id, "name", id, "source_type", "url", "behavior", "classical", "url", "https://rules.example/"+id)
		if _, err := rulefetch.WriteValidatedRuleCache(ctx, item, "DOMAIN-SUFFIX,example.com", jsonx.Obj("rule_library", []any{})); err != nil {
			t.Fatal(err)
		}
	}
	var calls int
	restore := oxidns.SetNativeClassicalIPLines(func(ctx context.Context, item model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
		calls++
		return ippresence.TypedClassicalIPLines(repo.WithRepository(ctx, repository), item, library)
	})
	defer restore()
	var sizes []uint64
	for _, width := range []int{0, 2 << 20} {
		profile := metadataProfile(t, 128, width)
		ippresence.ClearMemo()
		calls = 0
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := oxidns.Generate(profile, "http://panel.example")
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		if calls < 128 {
			t.Fatalf("only %d metadata callbacks", calls)
		}
		bytes := after.TotalAlloc - before.TotalAlloc
		sizes = append(sizes, bytes)
		if bytes > 80<<20 || after.Mallocs-before.Mallocs > 900000 {
			t.Fatalf("whole-library decode regression: %d bytes / %d allocations", bytes, after.Mallocs-before.Mallocs)
		}
		t.Logf("unused=%d bytes=%d allocations=%d", width, bytes, after.Mallocs-before.Mallocs)
	}
	if sizes[1] > sizes[0]+(4<<20) {
		t.Fatalf("unrelated metadata amplified allocations: %v", sizes)
	}
}
