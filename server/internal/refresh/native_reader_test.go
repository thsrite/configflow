package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/substore"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestUnsafeSourceYAMLNeverWritesCaches(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies: []\na0: &a0 [example.com]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, tc := range []struct {
		name, source string
		kind         error
	}{
		{"alias40", b.String(), yamlx.ErrAliasBudget},
		{"self", "proxies: []\na: &a [*a]", yamlx.ErrAliasRecursion},
		{"policy", "proxies: []\nx: !!str [secret]", yamlx.ErrPolicy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, 86400)
			sub := f.sub()
			f.cacheSource(0, subURL)
			good := &SourceResult{Text: proxiesYAML(t), Source: "download"}
			if _, err := RememberDownload(f.ctx, sub, good); err != nil {
				t.Fatal(err)
			}
			before, err := readSharedData(f.ctx, DownloadCachePath(subURL))
			if err != nil {
				t.Fatal(err)
			}
			nodesBefore := f.cache()
			bad := &SourceResult{Text: tc.source, Source: "download"}
			if _, err := CacheMetadata(f.ctx, sub, bad); !errors.Is(err, tc.kind) {
				t.Fatalf("metadata: %v", err)
			}
			if _, err := RememberDownload(f.ctx, sub, bad); !errors.Is(err, tc.kind) {
				t.Fatalf("remember: %v", err)
			}
			f.env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*SourceResult, error) {
				return bad, nil
			}
			if _, _, _, err := DownloadNodes(f.ctx, sub); !errors.Is(err, tc.kind) {
				t.Fatalf("download: %v", err)
			}
			// Force a source fetch without a current validated-node match.
			drySub := sub
			drySub.URL = "https://example.com/other"
			if _, err := FetchSource(f.ctx, drySub, func(context.Context, string, string) (*SourceResult, error) { return bad, nil }); !errors.Is(err, tc.kind) {
				t.Fatalf("dry source: %v", err)
			}
			after, err := readSharedData(f.ctx, DownloadCachePath(subURL))
			if err != nil || !testjson.Equal(before, after) || !testjson.Equal(nodesBefore, f.cache()) {
				t.Fatal("unsafe YAML modified caches")
			}
		})
	}
}

func FuzzSourceYAMLPolicy(f *testing.F) {
	for _, s := range []string{"proxies: []", "a: &a [*a]", "x: !!str [x]", "proxies: [broken"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) { _ = validateSourceYAMLPolicy(source) })
}

func TestUnsafeDownloadedCacheRejectedBeforeReuse(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies: []\na0: &a0 [example.com]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, source := range []string{b.String(), "a: &a [*a]", "x: !!str [secret]"} {
		f := setup(t, 86400)
		sub := f.sub()
		cache := map[string]any{"text": source, "fetched_at": isoMicro(f.clock.Now()), "traffic": nil}
		cacheBytes, err := jsonresp.Encode(cache)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.WriteSharedText(f.ctx, DownloadCachePath(subURL), cacheBytes); err != nil {
			t.Fatal(err)
		}
		result, err := FetchSource(f.ctx, sub, func(context.Context, string, string) (*SourceResult, error) {
			t.Fatal("unsafe fresh cache triggered a fetch")
			return nil, nil
		})
		if result != nil || err == nil {
			t.Fatalf("unsafe cached source reused: %v", err)
		}
		after, err := readSharedData(f.ctx, DownloadCachePath(subURL))
		if err != nil || !testjson.Equal(json.RawMessage(after), cache) {
			t.Fatal("refusal modified the original cache")
		}
	}
}
