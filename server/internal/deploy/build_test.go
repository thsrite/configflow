package deploy

import (
	"bytes"
	"configflow/internal/convert"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return repo.WithRepository(context.Background(), r)
}
func TestPythonBuild(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/build.json")
	if err != nil {
		t.Fatal(err)
	}
	var header struct{ Payloads map[string]string }
	if err = json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text, ok := header.Payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(text))
	}))
	t.Cleanup(srv.Close)
	raw = bytes.ReplaceAll(raw, []byte("@RULE_SERVER@"), []byte(srv.URL))
	var corpus struct {
		Rows []struct {
			Name, Service, Main, Error string
			Source                     json.RawMessage
			Providers                  []struct {
				Name, LocalPath, Content string
				Local                    string `json:"local_path"`
			}
			Downloads []struct {
				Name, URL, Behavior, Format string
				LocalPath                   string `json:"local_path"`
			}
			Custom []struct {
				Path, Name, Content string
				Native              bool
			}
			Pinned []struct {
				Path, Content string
				Role          Role
			}
			Watchdog bool
			Manifest Manifest
			Files    map[string]string
		}
		Geo []struct {
			Document json.RawMessage
			Required []string
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, row := range corpus.Rows {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()
			src := testSource(t, row.Source)
			in := BuildInput{Target: Target{ID: "agent-example", ServiceType: row.Service, ProfileID: "alpha"}, Source: src, ConfigContent: row.Main, DeploymentID: "a11-corpus", BaseURL: "https://panel.example", LocalDNSWatchdog: row.Watchdog}
			for _, p := range row.Providers {
				in.ProviderDownloads = append(in.ProviderDownloads, ProviderFile{Name: p.Name, LocalPath: p.Local, Content: []byte(p.Content)})
			}
			for _, d := range row.Downloads {
				in.RulesetDownloads = append(in.RulesetDownloads, convert.Download{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: d.Behavior, Format: d.Format})
			}
			for _, f := range row.Custom {
				in.CustomFiles = append(in.CustomFiles, CustomFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Native: f.Native})
			}
			for _, f := range row.Pinned {
				in.PinnedFiles = append(in.PinnedFiles, PinnedFile{Path: f.Path, Content: []byte(f.Content), Role: f.Role})
			}
			b, err := Build(testContext(t), in)
			if row.Error != "" {
				if err == nil {
					t.Fatal("accepted", b.Manifest())
				}
				if err.Error() != row.Error {
					t.Fatalf("error\n%s\nwant\n%s", err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m := b.Manifest()
			if len(m.Files) != len(row.Manifest.Files) {
				t.Fatal(m, row.Manifest)
			}
			actual, err := b.Files()
			if err != nil {
				t.Fatal(err)
			}
			for i, f := range m.Files {
				want := row.Manifest.Files[i]
				if f.Path != want.Path || f.Role != want.Role || f.Name != want.Name {
					t.Fatalf("file metadata %#v want %#v", f, want)
				}
				reference, err := base64.StdEncoding.DecodeString(row.Files[f.Path])
				reference = bytes.ReplaceAll(reference, []byte("@RULE_SERVER@"), []byte(srv.URL))
				if err != nil {
					t.Fatal(err)
				}
				if f.Role == RoleConfig {
					a, e := yamlx.Parse(actual[f.Path])
					if e != nil {
						t.Fatal(e)
					}
					r, e := yamlx.Parse(reference)
					if e != nil {
						t.Fatal(e)
					}
					if !yamlx.Equal(a, r) {
						t.Fatalf("main YAML\n%s\nwant\n%s", actual[f.Path], reference)
					}
				} else if !bytes.Equal(actual[f.Path], reference) {
					// Only normalized YAML rule sources are semantically compared; verbatim
					// input and text conversion remain exact byte comparisons.
					if !strings.HasSuffix(row.Name, "classical-yaml") {
						t.Fatalf("%s bytes %q want %q", f.Path, actual[f.Path], reference)
					}
					a, e := yamlx.Parse(actual[f.Path])
					r, e2 := yamlx.Parse(reference)
					if e != nil || e2 != nil || !yamlx.Equal(a, r) {
						t.Fatalf("rule YAML %q want %q", actual[f.Path], reference)
					}
				}
			}
		})
	}
	for i, row := range corpus.Geo {
		doc, err := yamlx.FromJSON(row.Document)
		if err != nil {
			t.Fatal(err)
		}
		if got := RequiredGeoResources(doc, nil); !reflect.DeepEqual(got, row.Required) {
			t.Errorf("geo %d got %v want %v", i, got, row.Required)
		}
	}
}

func TestBuildSharedSourcesAndProgress(t *testing.T) {
	var calls, active, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("DOMAIN,example.com\nIP-CIDR,192.0.2.0/24"))
	}))
	defer srv.Close()
	for _, shared := range []bool{true, false} {
		calls.Store(0)
		peak.Store(0)
		main := yamlx.Map(yamlx.Str("plugins"), yamlx.Seq())
		in := BuildInput{Target: Target{ID: "example", ServiceType: "mosdns", ProfileID: "alpha"}, Source: testSource(t, map[string]any{"profile_id": "alpha"})}
		for i := range 16 {
			p := fmt.Sprintf("rules/%d", i)
			u := srv.URL + "/source"
			if !shared {
				u += fmt.Sprint(i)
			}
			main.Content[1].Content = append(main.Content[1].Content, yamlx.Map(yamlx.Str("tag"), yamlx.Str(fmt.Sprint(i)), yamlx.Str("type"), yamlx.Str("domain_set"), yamlx.Str("args"), yamlx.Map(yamlx.Str("files"), yamlx.Seq(yamlx.Str(p)))))
			in.RulesetDownloads = append(in.RulesetDownloads, convert.Download{Name: fmt.Sprint(i), URL: u, LocalPath: p, Behavior: "classical", Format: "text"})
		}
		content, _ := yamlx.Marshal(main)
		in.ConfigContent = string(content)
		progress := []int{}
		in.Progress.Rules = func(done, total int) {
			progress = append(progress, done)
			if total != 16 {
				t.Error(total)
			}
		}
		for range 2 {
			if _, err := Build(testContext(t), in); err != nil {
				t.Fatal(err)
			}
		}
		expected := int32(32)
		if shared {
			expected = 2
		}
		if calls.Load() != expected {
			t.Errorf("shared %v calls %d want %d", shared, calls.Load(), expected)
		}
		if peak.Load() > 8 {
			t.Errorf("workers %d", peak.Load())
		}
		if len(progress) != 34 {
			t.Fatal(progress)
		}
	}
}

func TestGatewayHostileProviderFailsClosed(t *testing.T) {
	src := testSource(t, map[string]any{"profile_id": "alpha", "mihomo": map[string]any{"linux_gateway": map[string]any{"enabled": true, "rule_set_ids": []any{}}}})
	for _, content := range []string{"proxies: [{name: example, routing-mark: 1}]", "proxies: &loop [*loop]", aliasProvider(64)} {
		t.Run(fmt.Sprint(len(content)), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
			defer cancel()
			_, err := Build(ctx, BuildInput{Target: Target{ID: "example", ServiceType: "mihomo"}, Source: src, ConfigContent: "mode: rule", ProviderDownloads: []ProviderFile{{Name: "Feed", LocalPath: "providers/a", Content: []byte(content)}}})
			if err == nil {
				t.Fatal("hostile provider accepted")
			}
		})
	}
}
func aliasProvider(depth int) string {
	var b strings.Builder
	b.WriteString("l0: &l0 [a, b]\n")
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	return b.String()
}

func TestRawCacheBoundsAndCoalescing(t *testing.T) {
	c := newRawCache()
	var calls atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	fetch := func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		close(start)
		<-release
		return []byte{}, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := c.get(context.Background(), "same", fetch); err != nil {
				t.Error(err)
			}
		})
	}
	<-start
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	for i := range maxRawCacheEntries + 1 {
		_, err := c.get(context.Background(), fmt.Sprint(i), func(context.Context, string) ([]byte, error) { return []byte{}, nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(c.entries) > maxRawCacheEntries {
		t.Fatal(len(c.entries))
	}
	_, err := c.get(context.Background(), "large", func(context.Context, string) ([]byte, error) { return make([]byte, maxRawCacheBytes+1), nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.entries["large"] != nil {
		t.Fatal("oversized response retained")
	}
}

func TestGeoPreparation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/html" {
			_, _ = w.Write([]byte("<html>failure"))
			return
		}
		_, _ = w.Write([]byte("synthetic geo bytes"))
	}))
	defer srv.Close()
	src := testSource(t, map[string]any{"profile_id": "alpha"})
	in := BuildInput{Target: Target{ID: "example", ServiceType: "mihomo", ProfileID: "alpha"}, Source: src, ConfigContent: "rules: [GEOSITE,cn,DIRECT]\n"}
	// The rule must be one scalar rather than a YAML flow list split at commas.
	in.ConfigContent = fmt.Sprintf("rules: ['GEOSITE,cn,DIRECT']\ngeox-url: {geosite: '%s/geo'}\n", srv.URL)
	b, err := Build(testContext(t), in)
	if err != nil {
		t.Fatal(err)
	}
	phases := []string{}
	with, err := PrepareMihomoResources(context.Background(), b, Capabilities{MihomoResources: []string{}, ConfigDirectory: "/opt/example"}, src, Progress{Phase: func(s string) { phases = append(phases, s) }})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := with.Contents()
	if string(files["GeoSite.dat"].Data) != "synthetic geo bytes" || files["GeoSite.dat"].Role != RoleResource {
		t.Fatal(files)
	}
	if calls.Load() != 1 || len(phases) != 3 || strings.Contains(strings.Join(phases, ""), srv.URL) {
		t.Fatal(calls.Load(), phases)
	}
	if _, err := Fingerprint(with.Manifest()); err == nil {
		t.Fatal("resource entered content fingerprint")
	}
	for _, capability := range []Capabilities{{}, {MihomoResources: []string{"GEOSITE.DAT"}}} {
		got, err := PrepareMihomoResources(context.Background(), b, capability, src, Progress{})
		if err != nil || got != b {
			t.Fatal(got, err)
		}
	}
	in.ConfigContent = strings.ReplaceAll(in.ConfigContent, "/geo", "/html")
	bad, err := Build(testContext(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareMihomoResources(context.Background(), bad, Capabilities{MihomoResources: []string{}}, src, Progress{}); err == nil || !strings.Contains(err.Error(), "GeoSite 下载失败") {
		t.Fatal(err)
	}
}

func TestRawCacheFailureDoesNotPoisonRetry(t *testing.T) {
	cache := newRawCache()
	var calls atomic.Int32
	failing := true
	fetch := func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		if failing {
			return nil, fmt.Errorf("synthetic failure")
		}
		return []byte("good"), nil
	}
	if _, err := cache.get(context.Background(), "example", fetch); err == nil {
		t.Fatal("failure lost")
	}
	failing = false
	value, err := cache.get(context.Background(), "example", fetch)
	if err != nil || string(value) != "good" || calls.Load() != 2 {
		t.Fatal(string(value), err, calls.Load())
	}
	for i := range 3 {
		_, err := cache.get(context.Background(), fmt.Sprint(i), func(context.Context, string) ([]byte, error) { return make([]byte, maxRawCacheBytes/2), nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	if cache.size > maxRawCacheBytes || cache.entries["0"] != nil {
		t.Fatal("byte LRU limit not enforced")
	}
}

func TestUnusedSourceChangesDoNotChangeBundle(t *testing.T) {
	source := map[string]any{"profile_id": "alpha", "system_config": map[string]any{"server_domain": "https://panel.example"}, "rule_library": []any{map[string]any{"id": "unused", "name": "Unused", "source_type": "url", "url": "https://unused.example/rules"}}}
	in := BuildInput{Target: Target{ID: "example", ProfileID: "alpha", ServiceType: "mihomo"}, Source: testSource(t, source), ConfigContent: "mode: rule", DeploymentID: "metadata"}
	first, err := Build(testContext(t), in)
	if err != nil {
		t.Fatal(err)
	}
	source["name"] = "rename"
	source["rule_library"].([]any)[0].(map[string]any)["url"] = "https://changed.example/rules"
	in.Source = testSource(t, source)
	second, err := Build(testContext(t), in)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Fingerprint(first.Manifest())
	b, _ := Fingerprint(second.Manifest())
	if a != b {
		t.Fatal("unused draft changed content identity")
	}
}
