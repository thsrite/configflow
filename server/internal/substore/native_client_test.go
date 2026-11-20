package substore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// Explicit approved native boundaries; the original archive/comparator stay fixed.
func nativeClientExpectations(t *testing.T) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_client_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources map[string]string         `json:"sources"`
		Cases   map[string]map[string]any `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	for name, want := range manifest.Sources {
		source, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("native client source drift %s", name)
		}
	}
	return manifest.Cases
}

func TestN89NativeProxyRecords(t *testing.T) {
	proxies, err := ParseProxyNodes(`proxies: [{name: 5, type: SS, server: 123, port: "443", password: 123, future: [true, 5]}]`)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := ProxyNodes(proxies)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "5" || nodes[0].Server != "123" || nodes[0].Port != 443 || nodes[0].Type != "ss" {
		t.Fatalf("nodes %#v", nodes)
	}
	params, err := nodes[0].Params.Node()
	if err != nil {
		t.Fatal(err)
	}
	if password := yamlx.Get(params, "password"); password == nil || password.ShortTag() != "!!int" || password.Value != "123" {
		t.Fatalf("lost password: %#v", password)
	}
	if yamlx.Get(params, "future") == nil {
		t.Fatal("lost open parameters")
	}
	_, err = ProxyNodes([]*yamlx.Node{must[*yamlx.Node](t)(yamlx.FromJSON([]byte(`{"type":5}`)))})
	if err == nil || err.Error() != "节点 type 必须是字符串，当前值：5" {
		t.Fatalf("type: %v", err)
	}
	encoded, err := model.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"name":"5"`, `"server":"123"`, `"port":443`, `"password":123`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("missing %s in %s", key, encoded)
		}
	}
}

func TestN89ClientOwnerLifetime(t *testing.T) {
	for _, operation := range []string{"fetch", "convert"} {
		for _, shutdown := range []bool{false, true} {
			name := operation + "/disconnect"
			if shutdown {
				name = operation + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t, harnessOptions{builtin: true, version: "2.42.3"})
				c := h.service.Client
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				rt.Start(context.Background())
				defer rt.Shutdown(context.Background())
				ctx, disconnect := context.WithCancel(rt.BindRequest(context.Background()))
				defer disconnect()
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				pause := func() { once.Do(func() { close(entered) }); <-release }
				c.fetch = func(context.Context, string, string, string) (*FetchResult, error) {
					pause()
					return newFetchResult("proxies: []", "direct_yaml", nil, "", h.clock), nil
				}
				c.convert = func(context.Context, string, string) (*yamlx.Node, error) {
					pause()
					return yamlx.FromJSON([]byte(`{"type":"ss"}`))
				}
				done := make(chan error, 1)
				go func() {
					if operation == "fetch" {
						_, err := c.Fetch(ctx, "id", "https://example.com/sub", "", FetchOptions{})
						done <- err
					} else {
						_, err := c.ConvertProxy(ctx, "ss://x")
						done <- err
					}
				}()
				<-entered
				if shutdown {
					rt.Shutdown(context.Background())
				} else {
					disconnect()
				}
				close(release)
				err := <-done
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel error %v", err)
					}
					if c.results.len() != 0 || c.failures.len() != 0 || c.conversions.len() != 0 {
						t.Fatal("shutdown result/cooldown committed")
					}
				} else if err != nil {
					t.Fatalf("disconnect aborted: %v", err)
				}
			})
		}
	}
}

func TestN89TemporarySubscriptionRawRequests(t *testing.T) {
	type wire struct{ Method, Path, Body, ContentType string }
	calls := make(chan wire, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		calls <- wire{r.Method, r.URL.RequestURI(), string(data), r.Header.Get("Content-Type")}
		w.WriteHeader(200)
	}))
	defer server.Close()
	h, c := external(t)
	h.service.Env.HTTP = RequestsClient{}
	text := "ss://opaque"
	if !c.createSubscription(context.Background(), server.URL, "temporary", "https://example.com/sub", &text) {
		t.Fatal("create failed")
	}
	c.deleteSubscription(context.Background(), server.URL, "temporary")
	want := []wire{
		{"POST", "/api/subs", `{"name":"temporary","url":"https://example.com/sub#noFlow","noFlow":true,"source":"local","content":"ss://opaque"}`, "application/json"},
		{"DELETE", "/api/sub/temporary", "", ""},
	}
	for _, expected := range want {
		select {
		case got := <-calls:
			if got != expected {
				t.Fatalf("wire %#v want %#v", got, expected)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no request")
		}
	}
}

func TestN89FlightWaiterShutdownDoesNotWaitForOwner(t *testing.T) {
	_, c := external(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	defer rt.Shutdown(context.Background())
	request := rt.BindRequest(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	c.fetch = func(context.Context, string, string, string) (*FetchResult, error) {
		close(entered)
		<-release
		return &FetchResult{Text: "ok"}, nil
	}
	owner := make(chan error, 1)
	go func() { _, err := c.Fetch(request, "a", "https://example.com/sub", "", FetchOptions{}); owner <- err }()
	<-entered
	// An independent request binds to the same live Runtime and existing flight.
	waiter := make(chan error, 1)
	go func() { _, err := c.Fetch(request, "b", "https://example.com/sub", "", FetchOptions{}); waiter <- err }()
	rt.Shutdown(context.Background())
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter ignored Runtime shutdown")
	}
	// The owner is deliberately not released until this test returns.
	select {
	case <-owner:
		t.Fatal("fake owner should remain blocked")
	default:
	}
	unblock()
	if err := <-owner; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestN89ProxyReaderIgnoresUnconsumedComplexKeys(t *testing.T) {
	for _, source := range []string{"{[a]: b}", "? [a]\n: b\nproxies: []\n"} {
		nodes, err := ParseProxyNodes(source)
		if err != nil || len(nodes) != 0 {
			t.Fatalf("unconsumed metadata rejected: %v %#v", err, nodes)
		}
	}
}
