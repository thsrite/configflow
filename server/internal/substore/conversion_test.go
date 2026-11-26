package substore

// Port of backend/test_node_conversion_cache.py: converted nodes are
// remembered per bundled Sub-Store version. Python patched
// is_builtin_available / active_bundle; here the image's bundle directory
// and its VERSION file are real files of the test.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

const (
	testNode  = "anytls://secret@node.example.test:443?sni=example.test#one"
	otherNode = "trojan://secret@other.example.test:443#two"
)

type asked struct{ proxy, target string }

// conversionStore is the fixture `store`: a bundled Sub-Store that answers,
// with every question recorded.
type conversionStore struct {
	h      *harness
	c      *Client
	mu     sync.Mutex
	asked  []asked
	fail   map[string]bool
	during func()
}

func newConversionStore(t *testing.T) *conversionStore {
	h := newHarness(t, harnessOptions{builtin: true, version: "2.42.3"})
	s := &conversionStore{h: h, c: h.service.Client, fail: map[string]bool{}}
	s.c.convert = func(_ context.Context, proxyString, target string) (*yamlx.Node, error) {
		s.mu.Lock()
		s.asked = append(s.asked, asked{proxyString, target})
		during := s.during
		s.mu.Unlock()
		if during != nil {
			during()
		}
		if s.fail[proxyString] {
			return nil, nil
		}
		server := proxyString
		if r := []rune(server); len(r) > 12 {
			server = string(r[len(r)-12:])
		}
		data, err := json.Marshal(map[string]any{
			"name": "from-uri", "type": "anytls", "server": server, "port": 443,
			"converted-by": s.version(), "target": target, "alpn": []string{"h2"},
		})
		if err != nil {
			return nil, err
		}
		return yamlx.FromJSON(data)
	}
	return s
}

func (s *conversionStore) version() string {
	data, _ := os.ReadFile(filepath.Join(s.h.builtin, "VERSION"))
	return string(data[:len(data)-1])
}

func (s *conversionStore) setVersion(v string) {
	os.WriteFile(filepath.Join(s.h.builtin, "VERSION"), []byte(v+"\n"), 0o644)
}

func (s *conversionStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asked)
}

func (s *conversionStore) convert(proxy string) *yamlx.Node {
	node, _ := s.c.ConvertProxy(context.Background(), proxy)
	return node
}

func yamlStr(n *yamlx.Node, key string) string {
	if n == nil {
		return ""
	}
	v := yamlx.Get(n, key)
	if v == nil {
		return ""
	}
	return v.Value
}

func nodeJSON(t *testing.T, n *yamlx.Node) json.RawMessage {
	t.Helper()
	data, err := yamlx.ToJSON(n)
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(data)
}

// test_node_conversion_cache.py::test_the_same_node_is_converted_once_and_callers_get_their_own_copy
func TestTheSameNodeIsConvertedOnceAndCallersGetTheirOwnCopy(t *testing.T) {
	s := newConversionStore(t)
	first := s.convert(testNode)
	if s.count() != 1 || s.asked[0] != (asked{testNode, "ClashMeta"}) {
		t.Fatalf("asked %v", s.asked)
	}
	// Mutating the returned node and nested sequence must not affect cached copy.
	yamlx.Set(first, "name", yamlx.Str("renamed by the caller"))
	alpn := yamlx.Get(first, "alpn")
	alpn.Content = append(alpn.Content, yamlx.Str("changed"))
	second := s.convert(testNode)
	if s.count() != 1 {
		t.Fatal("asked again")
	}
	want := map[string]any{
		"name": "from-uri", "type": "anytls", "server": testNode[len(testNode)-12:], "port": 443,
		"converted-by": "2.42.3", "target": "ClashMeta", "alpn": []string{"h2"},
	}
	if !testjson.Equal(want, nodeJSON(t, second)) {
		t.Fatalf("second:\n%s", testjson.Diff(want, nodeJSON(t, second)))
	}
	yamlx.Get(second, "alpn").Content[0].Value = "changed again"
	if got := yamlx.Get(s.convert(testNode), "alpn"); len(got.Content) != 1 || got.Content[0].Value != "h2" {
		t.Fatal("caller's change reached the cache")
	}
	if yamlStr(s.convert(otherNode), "server") != otherNode[len(otherNode)-12:] || s.count() != 2 {
		t.Fatal("other node")
	}
}

// test_node_conversion_cache.py::test_a_failed_conversion_is_asked_again
func TestAFailedConversionIsAskedAgain(t *testing.T) {
	s := newConversionStore(t)
	s.fail[testNode] = true
	if s.convert(testNode) != nil || s.convert(testNode) != nil || s.count() != 2 {
		t.Fatal("failure kept")
	}
	delete(s.fail, testNode)
	s.convert(testNode)
	if yamlStr(s.convert(testNode), "type") != "anytls" || s.count() != 3 {
		t.Fatalf("asked %d", s.count())
	}
}

// test_node_conversion_cache.py::test_another_sub_store_version_converts_again
func TestAnotherSubStoreVersionConvertsAgain(t *testing.T) {
	s := newConversionStore(t)
	if yamlStr(s.convert(testNode), "converted-by") != "2.42.3" {
		t.Fatal("first")
	}
	s.setVersion("2.43.0")
	if yamlStr(s.convert(testNode), "converted-by") != "2.43.0" || yamlStr(s.convert(testNode), "converted-by") != "2.43.0" {
		t.Fatal("new version")
	}
	if s.count() != 2 {
		t.Fatalf("asked %d", s.count())
	}
	// Rolled back: the answers of that version are its own.
	s.setVersion("2.42.3")
	if yamlStr(s.convert(testNode), "converted-by") != "2.42.3" || s.count() != 2 {
		t.Fatal("rollback")
	}
}

// test_node_conversion_cache.py::test_an_update_during_the_conversion_keeps_nothing
func TestAnUpdateDuringTheConversionKeepsNothing(t *testing.T) {
	s := newConversionStore(t)
	s.during = func() { s.setVersion("2.43.0") }
	if s.convert(testNode) == nil {
		t.Fatal("nil")
	}
	s.during = nil
	for _, version := range []string{"2.42.3", "2.43.0"} {
		s.setVersion(version)
		before := s.count()
		s.convert(testNode)
		if s.count() != before+1 {
			t.Fatalf("version %s kept a result", version)
		}
	}
}

// test_node_conversion_cache.py::test_nothing_is_kept_without_a_known_version
func TestNothingIsKeptWithoutAKnownVersion(t *testing.T) {
	for _, name := range []string{"empty", "missing"} {
		t.Run(name, func(t *testing.T) {
			s := newConversionStore(t)
			if name == "empty" {
				s.setVersion("")
			} else {
				os.Remove(filepath.Join(s.h.builtin, "VERSION"))
			}
			s.c.convert = func(_ context.Context, proxyString, target string) (*yamlx.Node, error) {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.asked = append(s.asked, asked{proxyString, target})
				return yamlx.FromJSON([]byte(`{"type":"anytls"}`))
			}
			s.convert(testNode)
			s.convert(testNode)
			if s.count() != 2 {
				t.Fatalf("asked %d", s.count())
			}
		})
	}
}

// test_node_conversion_cache.py::test_a_sub_store_outside_the_image_is_always_asked
func TestASubStoreOutsideTheImageIsAlwaysAsked(t *testing.T) {
	s := newConversionStore(t)
	bundle := filepath.Join(s.h.builtin, SubStoreAsset)
	os.Rename(bundle, bundle+".off")
	s.convert(testNode)
	s.convert(testNode)
	if s.count() != 2 {
		t.Fatal("external answers kept")
	}
	os.Rename(bundle+".off", bundle)
	s.convert(testNode)
	s.convert(testNode)
	if s.count() != 3 {
		t.Fatalf("asked %d", s.count())
	}
}

// test_node_conversion_cache.py::test_each_target_format_has_its_own_result
func TestEachTargetFormatHasItsOwnResult(t *testing.T) {
	s := newConversionStore(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		surge, _ := s.c.ConvertTarget(ctx, testNode, "Surge")
		if yamlStr(s.convert(testNode), "target") != "ClashMeta" || yamlStr(surge, "target") != "Surge" {
			t.Fatal("target")
		}
	}
	if s.count() != 2 {
		t.Fatalf("asked %d", s.count())
	}
}

// test_node_conversion_cache.py::test_only_the_most_recently_used_nodes_are_kept
func TestOnlyTheMostRecentlyUsedNodesAreKept(t *testing.T) {
	s := newConversionStore(t)
	s.c.conversionLimit = 3
	nodes := []string{}
	for n := 0; n < 4; n++ {
		nodes = append(nodes, "ss://secret@node"+string(rune('0'+n))+".example.test:443#n")
	}
	for _, node := range nodes[:3] {
		s.convert(node)
	}
	s.convert(nodes[0]) // used again: the oldest is now nodes[1]
	s.convert(nodes[3])
	if s.count() != 4 || s.c.conversions.len() != 3 {
		t.Fatalf("asked %d kept %d", s.count(), s.c.conversions.len())
	}
	s.convert(nodes[0])
	s.convert(nodes[2])
	s.convert(nodes[3])
	if s.count() != 4 {
		t.Fatal("recent node evicted")
	}
	s.convert(nodes[1])
	if s.count() != 5 {
		t.Fatal("oldest node kept")
	}
}

// test_node_conversion_cache.py::test_a_version_file_that_cannot_be_read_only_costs_the_memory
func TestAVersionFileThatCannotBeReadOnlyCostsTheMemory(t *testing.T) {
	s := newConversionStore(t)
	os.WriteFile(filepath.Join(s.h.builtin, "VERSION"), []byte{0xff, 0xfe, '\n'}, 0o644)
	if s.convert(testNode) == nil || s.convert(testNode) == nil || s.count() != 2 {
		t.Fatalf("asked %d", s.count())
	}
}

// test_node_conversion_cache.py::test_concurrent_requests_for_one_node_agree
func TestConcurrentRequestsForOneNodeAgree(t *testing.T) {
	s := newConversionStore(t)
	results := make([]*yamlx.Node, 64)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			results[i] = s.convert(testNode)
			<-sem
		}(i)
	}
	wg.Wait()
	seen := map[*yamlx.Node]bool{}
	for _, r := range results {
		if !yamlx.Equal(r, results[0]) {
			t.Fatal("results differ")
		}
		seen[r] = true
	}
	if len(seen) != 64 {
		t.Fatal("results shared")
	}
	before := s.count()
	s.convert(testNode)
	if s.count() != before {
		t.Fatal("asked again")
	}
}

// test_node_conversion_cache.py::test_rendering_again_does_not_start_sub_store:
// through the real client and convert.NodeProxy, the second conversion makes
// no request at all, so the on-demand runtime is never entered and Sub-Store
// stays asleep.
func TestRenderingAgainDoesNotStartSubStore(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true, version: "2.42.3"})
	h.commands.runVersion = "2.42.3"
	h.store.download = func(_, _ string, query url.Values, _ map[string]any) (int, string, http.Header) {
		if query.Get("content") != testNode {
			t.Errorf("content %q", query.Get("content"))
		}
		return 200, "proxies:\n  - {name: from-uri, type: anytls, server: node.example.test, port: 443, password: secret}\n", nil
	}
	c := h.service.Client
	ctx := context.Background()
	first := must[*yamlx.Node](t)(convert.NodeProxy(ctx, model.Node{Name: "Home", ProxyString: new(testNode)}, c))
	starts, _ := h.commands.counts()
	if events := h.store.eventList(); starts != 1 || len(events) != 3 {
		t.Fatalf("starts %d events %v", starts, events)
	}
	second := must[*yamlx.Node](t)(convert.NodeProxy(ctx, model.Node{Name: "Home", ProxyString: new(testNode)}, c))
	third := must[*yamlx.Node](t)(convert.NodeProxy(ctx, model.Node{Name: "Away", ProxyString: new(testNode)}, c))
	starts, _ = h.commands.counts()
	if events := h.store.eventList(); starts != 1 || len(events) != 3 {
		t.Fatalf("starts %d events %v", starts, events)
	}
	if !yamlx.Equal(first, second) || yamlStr(first, "name") != "Home" || yamlStr(first, "server") != "node.example.test" {
		t.Fatalf("first %s second %s", nodeJSON(t, first), nodeJSON(t, second))
	}
	want := yamlx.Clone(first)
	yamlx.Set(want, "name", yamlx.Str("Away"))
	if !yamlx.Equal(third, want) {
		t.Fatalf("third %s", nodeJSON(t, third))
	}
}
