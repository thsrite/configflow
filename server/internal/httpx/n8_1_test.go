package httpx_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

type n8ResponseCase struct {
	Name     string          `json:"name"`
	Endpoint string          `json:"endpoint"`
	Status   int             `json:"status"`
	Method   string          `json:"method"`
	Raw      bool            `json:"raw"`
	Body     string          `json:"body"`
	Expected json.RawMessage `json:"expected"`
}

// TestN8JSONResponsePythonBoundary uses the actual Python global response hook
// as its oracle (testdata/n8_1_gen.py). Every exemption includes failed status,
// failed success identity, nested credentials, GET and POST variants.
func TestN8JSONResponsePythonBoundary(t *testing.T) {
	file, err := os.Open("testdata/n8_1.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	var cases []n8ResponseCase
	scanner := bufio.NewScanner(zipped)
	for scanner.Scan() {
		var row n8ResponseCase
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 500 {
		t.Fatalf("truncated oracle: %d rows", len(cases))
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		for i, row := range cases {
			r.Handle([]string{row.Method}, fmt.Sprintf("/static/n8/%d", i), row.Endpoint, func(c *httpx.Ctx) (*httpx.Response, error) {
				if row.Raw {
					return httpx.RawJSON(row.Status, []byte(row.Body)), nil
				}
				return httpx.JSONStatus(row.Status, json.RawMessage(row.Body)), nil
			})
		}
	})})
	setSecrets(t, srv, []string{"rule /+密钥", "retired-secret"}, "agent /+secret")
	for i, row := range cases {
		t.Run(row.Name, func(t *testing.T) {
			got := srv.Do(row.Method, fmt.Sprintf("/static/n8/%d", i)).WantStatus(row.Status)
			want, err := jsonx.Parse(row.Expected)
			if err != nil {
				t.Fatal(err)
			}
			got.WantJSON(want)
		})
	}
}

func TestN8NativeValuesCannotBypassResponsePolicy(t *testing.T) {
	type label string
	type child struct {
		Secret label  `json:"secret"`
		Hidden string `json:"rule_proxy_token"`
	}
	type record struct {
		Nested []child         `json:"nested"`
		Raw    json.RawMessage `json:"raw"`
		Exact  json.Number     `json:"exact"`
	}
	payload := record{[]child{{"before agent-secret after", "not returned"}}, json.RawMessage(`{"value":"agent\u002dsecret","rule_fetch_proxy":"hidden"}`), json.Number("9007199254740993123456789")}
	srv := jsonServer(t, func() any { return payload }, func(app *httpx.App) {
		app.AfterRequest(func(_ *httpx.Ctx, r *httpx.Response) { _ = r.Body() }) // early encoding still gets final policy
	})
	for _, path := range []string{"/static/ordinary", "/static/single"} {
		res := srv.Get(path).WantStatus(200)
		res.WantJSON(jsonx.Obj("nested", []any{jsonx.Obj("secret", "[REDACTED]")}, "raw", jsonx.Obj("value", "[REDACTED]"), "exact", jsonx.Number("9007199254740993123456789")))
		if !strings.HasPrefix(res.Text(), `{"exact":9007199254740993123456789,"nested":`) {
			t.Fatal("output keys are not sorted", res.Text())
		}
	}
}

func TestN8ResponseEncodingFailureReturns500(t *testing.T) {
	cyclic := jsonx.NewObject()
	cyclic.Set("self", cyclic)
	for _, value := range []any{cyclic, make(chan int), jsonx.Obj("n", jsonx.Number("NaN"))} {
		srv := jsonServer(t, func() any { return value }, nil)
		srv.Get("/static/ordinary").WantStatus(500)
		srv.Get("/static/single").WantStatus(500)
	}
}

// N8-1 must not activate D2's one-time identity changes. These goldens were
// independently calculated with Python's existing json.dumps/hashlib forms.
func TestN8PersistedIdentitiesRemainUnchanged(t *testing.T) {
	item := jsonx.Obj("source", "notice", "title", "中文", "message", "message", "severity", "warning", "status_label", "pending", "target", jsonx.Obj("id", "router"), "evidence", []any{"a", "b"})
	tests := []struct{ name, got, want string }{
		{"notification fingerprint", notifications.Fingerprint("notice", item), "796d41c65fccf7be4c1f6905927f38f7f68e19ff85cd21bbda74bc5b0b851228"},
		{"router signature", notifications.RouterSignature(jsonx.Obj("z", "中文", "a", 1), jsonx.Obj("id", "router")), "7d96d6c8c83a86b93be94139573ba2f7dd84d73d69229b31d424e377f9e38239"},
		{"rule cache filename", rules.RuleCacheFilename(jsonx.Obj("id", "rule-中文", "url", "https://example.com/rules", "behavior", "domain", "format", "yaml")), "by-source/4426c62b307f4cf12000498eb551a75eb9f77bec2df4f934d1069ff52488381b.list"},
		{"hosts digest", subs.HostsDigest(jsonx.Obj("entries", jsonx.Obj("z.example.com", []any{"192.0.2.1"}, "a.example.com", []any{"2001:db8::1"}))), "b8cb494707314c3de1a4489ad0b63c0dfbdfe553d368ebcd7bfa7446c9919d48"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("persisted identity changed: got %s want %s", tt.got, tt.want)
			}
		})
	}
}
