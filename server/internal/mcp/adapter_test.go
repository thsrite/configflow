package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

type observedCall struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Body   json.RawMessage   `json:"body"`
	Query  map[string]string `json:"query"`
}

func nativeEqual(a, b json.RawMessage) bool {
	var x, y interface{}
	dx := json.NewDecoder(strings.NewReader(string(a)))
	dx.UseNumber()
	dy := json.NewDecoder(strings.NewReader(string(b)))
	dy.UseNumber()
	return dx.Decode(&x) == nil && dy.Decode(&y) == nil && reflect.DeepEqual(x, y)
}
func TestPythonToolAdapterContracts(t *testing.T) {
	data, err := os.ReadFile("testdata/adapters.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Calls     []observedCall  `json:"calls"`
		Out       json.RawMessage `json:"out"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	covered := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			app := httpx.New()
			var calls []observedCall
			app.Router.Handle([]string{"GET", "POST", "PUT", "DELETE"}, "/api/<path:p>", "test.echo", func(c *httpx.Ctx) (*httpx.Response, error) {
				raw, err := io.ReadAll(c.Request.Body)
				if err != nil {
					t.Fatal(err)
				}
				if c.Request.Method == "GET" || c.Request.Method == "DELETE" {
					if len(raw) != 0 || c.Request.Header.Get("Content-Type") != "" {
						t.Errorf("bodyless method sent %s / %s", raw, c.Request.Header.Get("Content-Type"))
					}
				}
				if !c.IsInternalCall() {
					t.Error("missing internal authentication")
				}
				if c.Request.Header.Get("X-ConfigFlow-Profile") != "default" {
					t.Error("missing profile pin")
				}
				query := map[string]string{}
				for key, values := range c.Request.URL.Query() {
					query[key] = values[0]
				}
				body := json.RawMessage(`null`)
				if len(raw) > 0 {
					body = raw
				}
				calls = append(calls, observedCall{c.Request.Method, c.Path(), body, query})
				item := json.RawMessage(`{"id":"synthetic","name":"Existing","keep":true,"health_check":{"enable":false,"interval":300}}`)
				if c.Request.Method == "GET" {
					switch c.Path() {
					case "/api/subscriptions", "/api/nodes", "/api/rules", "/api/rule-library", "/api/proxy-groups":
						return httpx.JSON([]json.RawMessage{item}), nil
					}
					if strings.HasSuffix(c.Path(), "/provider") || strings.HasSuffix(c.Path(), "/rule-proxy") || strings.HasPrefix(c.Path(), "/api/generate") {
						return httpx.JSON("synthetic text"), nil
					}
					return httpx.JSON(item), nil
				}
				if strings.HasSuffix(c.Path(), "/reorder") {
					return httpx.JSON(struct {
						Order []string `json:"order"`
					}{[]string{"synthetic"}}), nil
				}
				return httpx.JSON(struct {
					Data json.RawMessage `json:"data"`
				}{body}), nil
			})
			invocation, err := newInvocation(ctx, app, tc.Arguments)
			if err != nil {
				t.Fatal(err)
			}
			result, err := invocation.run(ctx, tc.Name)
			if err != nil {
				t.Fatal(err)
			}
			gotCalls, _ := json.Marshal(calls)
			wantCalls, _ := json.Marshal(tc.Calls)
			// Query map iteration is irrelevant; compare decoded values and actual body absence above.
			if !nativeEqual(gotCalls, wantCalls) {
				t.Errorf("calls got %s want %s", gotCalls, wantCalls)
			}
			if !nativeEqual(result, tc.Out) {
				t.Errorf("out got %s want %s", result, tc.Out)
			}
			covered[tc.Name] = true
		})
	}
	var definitions []Definition
	if err := json.Unmarshal(catalog, &definitions); err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 53 || len(covered) != len(definitions) {
		t.Fatalf("covered tools=%d definitions=%d", len(covered), len(definitions))
	}
	for _, d := range definitions {
		if !covered[d.Name] {
			t.Errorf("unexecuted tool %s", d.Name)
		}
	}
	t.Logf("%d actual Python adapters, %d tools", len(cases), len(covered))
}
