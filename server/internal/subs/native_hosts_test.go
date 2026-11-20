package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

type nativeHostsCase struct {
	InputSHA string          `json:"input_sha256"`
	Reason   string          `json:"reason"`
	Output   json.RawMessage `json:"output"`
}

func nativeHostsExpectations(t *testing.T) map[string]nativeHostsCase {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_hosts_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources   map[string]string          `json:"sources"`
		LoaderSHA string                     `json:"native_loader_sha256"`
		Cases     map[string]nativeHostsCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	lines := map[string][]string{}
	for name, want := range manifest.Sources {
		source, err := os.ReadFile("testdata/" + name + ".jsonl.gz")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("hosts source drift %s", name)
		}
		reader, err := gzip.NewReader(bytes.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			lines[name] = append(lines[name], scanner.Text())
		}
		reader.Close()
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}
	for key, entry := range manifest.Cases {
		if entry.Reason == "" {
			t.Fatalf("unclassified hosts boundary %s", key)
		}
		archive, indexText, ok := strings.Cut(key, "/")
		index, err := strconv.Atoi(indexText)
		if !ok || err != nil || index < 0 || index >= len(lines[archive]) {
			t.Fatal("hosts fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[archive][index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("hosts input drift %s", key)
		}
	}
	return manifest.Cases
}

func TestN89TypedHostCacheAndFilteredSource(t *testing.T) {
	var cache SubscriptionCache
	if err := json.Unmarshal([]byte(`{"nodes":[{"name":"JP","server":"node.example"}],"metadata":{"url":"https://sub.example/feed","hosts":{"entries":{"node.example":["192.0.2.1"]},"total":1,"skipped_count":0,"skipped":[]}}}`), &cache); err != nil {
		t.Fatal(err)
	}
	index, err := NewHostIndex(&cache)
	if err != nil || index == nil || len(index.Nodes) != 1 {
		t.Fatalf("cache index %#v metadata %#v err %v", index, cache.Metadata, err)
	}
	source := model.Subscription{ID: "source", Name: "Air", URL: "https://sub.example/feed"}
	if index.ForSubscription(source) == nil {
		t.Fatal("matching URL rejected")
	}
	main, err := yamlx.FromJSON([]byte(`{"proxy-providers":{"Air":{"url":"https://panel.example/api/subscriptions/source/proxies?regex=HK"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	config := &model.Config{Profile: &model.Profile{}, Subscriptions: []model.Subscription{source}}
	calls := 0
	report, merged, err := MergeHosts(main, config, nil, nil, nil, func(sub model.Subscription) (*HostIndex, error) { calls++; return index.ForSubscription(sub), nil })
	if err != nil || calls != 1 || report == nil || report.Ignored != 1 || report.Merged != 0 || len(report.Subscriptions) != 1 {
		t.Fatalf("filtered report %#v calls=%d merged=%#v err=%v", report, calls, merged, err)
	}
}

func TestN89HostMergeAliasAndMissingCacheOwner(t *testing.T) {
	main, err := yamlx.Parse([]byte("hosts: &hosts {kept.example: 192.0.2.2}\nbackup: *hosts\nproxy-providers: {Air: {url: 'https://panel.example/api/subscriptions/source/proxies'}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	config := &model.Config{Profile: &model.Profile{}, Subscriptions: []model.Subscription{{ID: "source", Name: "Air"}}}
	before, err := yamlx.ToJSON(main)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := MergeHosts(main, config, nil, nil, nil, nil); err == nil {
		t.Fatal("missing cache owner silently skipped work")
	}
	after, _ := yamlx.ToJSON(main)
	if !bytes.Equal(before, after) {
		t.Fatal("missing collaborator changed graph")
	}
	index := &HostIndex{Entries: yamlx.Map(yamlx.Str("node.example"), yamlx.Str("target.example")), Nodes: map[string][]string{"node.example": {"Node"}}, Total: 1}
	report, _, err := MergeHosts(main, config, nil, nil, nil, func(model.Subscription) (*HostIndex, error) { return index, nil })
	if err != nil || !yamlx.Equal(yamlx.Get(main, "backup"), yamlx.Map(yamlx.Str("kept.example"), yamlx.Str("192.0.2.2"))) || report.Merged != 1 {
		t.Fatalf("alias/report changed %#v %v", report, err)
	}
	if _, err := yamlx.Marshal(main); err != nil {
		t.Fatal("invalid generated alias", err)
	}
}
func TestN89HostIndexRejectsUnknownEntriesAndKeepsValidNodeSiblings(t *testing.T) {
	for _, entries := range []string{"", "null", "[]", "false"} {
		t.Run(entries, func(t *testing.T) {
			field := ""
			if entries != "" {
				field = `"entries":` + entries + `,`
			}
			var cache SubscriptionCache
			if err := json.Unmarshal([]byte(`{"metadata":{"hosts":{`+field+`"total":1}}}`), &cache); err != nil {
				t.Fatal(err)
			}
			if index, err := NewHostIndex(&cache); err != nil || index != nil {
				t.Fatalf("unknown map became a valid index %#v %v", index, err)
			}
		})
	}
	var cache SubscriptionCache
	if err := json.Unmarshal([]byte(`{"nodes":[{"name":"valid","server":"node.example"},"junk",null,0],"metadata":{"hosts":{"entries":{"node.example":["192.0.2.1"]}}}}`), &cache); err != nil {
		t.Fatal(err)
	}
	index, err := NewHostIndex(&cache)
	if err != nil || index == nil || len(index.Nodes["node.example"]) != 1 {
		t.Fatalf("usable sibling lost %#v %v", index, err)
	}
}
func hostsGenerationFixture(n, unrelated int) (*yamlx.Node, *model.Config, *HostIndex) {
	config := &model.Config{Profile: &model.Profile{}, Subscriptions: []model.Subscription{{ID: "source", Name: "Air"}}}
	if unrelated > 0 {
		data, _ := json.Marshal(strings.Repeat("x", unrelated))
		_ = config.Profile.Fields.Set("unrelated", data)
	}
	main := yamlx.Map(yamlx.Str("proxy-providers"), yamlx.Map(yamlx.Str("Air"), yamlx.Map(yamlx.Str("url"), yamlx.Str("https://panel.example/api/subscriptions/source/proxies"))))
	index := &HostIndex{Entries: yamlx.Map(), Nodes: map[string][]string{}, Total: n}
	for i := 0; i < n; i++ {
		domain := fmt.Sprintf("node-%d.example", i)
		name := fmt.Sprintf("Node %d", i)
		config.Nodes = append(config.Nodes, model.Node{ID: strconv.Itoa(i), Name: name, Server: domain})
		index.Entries.Content = append(index.Entries.Content, yamlx.Str(domain), yamlx.Seq(yamlx.Str("192.0.2.1")))
		index.Nodes[domain] = []string{name}
	}
	return main, config, index
}
func TestN89HostGenerationDoesNotDecodeUnrelatedConfig(t *testing.T) {
	measure := func(size int) int64 {
		main, config, index := hostsGenerationFixture(64, size)
		result := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				calls := 0
				_, _, err := MergeHosts(main, config, nil, nil, nil, func(model.Subscription) (*HostIndex, error) { calls++; return index, nil })
				if err != nil || calls != 1 {
					b.Fatalf("generation/cache callbacks %d %v", calls, err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := measure(0), measure(1<<20)
	if large > small+32768 {
		t.Fatalf("hosts copied unrelated config per generation: empty=%d, 1MiB=%d B/op", small, large)
	}
}
func BenchmarkN89HostGeneration(b *testing.B) {
	for _, size := range []int{32, 256, 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			main, config, index := hostsGenerationFixture(size, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, err := MergeHosts(main, config, nil, nil, nil, func(model.Subscription) (*HostIndex, error) { return index, nil })
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
