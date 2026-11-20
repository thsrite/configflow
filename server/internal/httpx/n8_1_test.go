package httpx_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/testjson"
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
	for _, value := range []any{cyclic, make(chan int)} {
		srv := jsonServer(t, func() any { return value }, nil)
		srv.Get("/static/ordinary").WantStatus(500)
		srv.Get("/static/single").WantStatus(500)
	}
}

// N8-1 originally locked Python identities until each consumer migrates.
// N8-7 activates D2 for notifications and N8-9 for rule caches and hosts. Keep the
// historical hashes as evidence beside each migrated native identity.
func TestN8PersistedIdentitiesAtConsumerMigration(t *testing.T) {
	var hosts subs.HostsData
	if err := json.Unmarshal([]byte(`{"entries":{"z.example.com":["192.0.2.1"],"a.example.com":["2001:db8::1"]}}`), &hosts); err != nil {
		t.Fatal(err)
	}
	hostsDigest, err := subs.HostDigest(&hosts)
	if err != nil {
		t.Fatal(err)
	}

	item := jsonx.Obj("source", "notice", "title", "中文", "message", "message", "severity", "warning", "status_label", "pending", "target", jsonx.Obj("id", "router"), "evidence", []any{"a", "b"})
	tests := []struct{ name, got, want string }{
		{"notification fingerprint", notifications.Fingerprint("notice", item), "0267bf46778074b7eeb98037dc5d72c1d23748a3309dd1832c5d53fac18f1ebe" /* Python: 796d41c65fccf7be4c1f6905927f38f7f68e19ff85cd21bbda74bc5b0b851228 */},
		{"router signature", notifications.RouterSignature(jsonx.Obj("z", "中文", "a", 1), jsonx.Obj("id", "router")), "17da2ce6aa800a88b9437370d51348e25d8ec354edcc9a68f494a480f401cbfa" /* Python: 7d96d6c8c83a86b93be94139573ba2f7dd84d73d69229b31d424e377f9e38239 */},
		{"rule cache filename", rules.CacheFilename(testjson.Decode[model.RuleConfig](t, jsonx.Obj("id", "rule-中文", "url", "https://example.com/rules", "behavior", "domain", "format", "yaml"))), "by-source/e2364080e064f144014eb27e7dee12c0a88691411bbd10e8fb11fe2d4ae2883a.list" /* Python: by-source/4426c62b307f4cf12000498eb551a75eb9f77bec2df4f934d1069ff52488381b.list */},
		{"hosts digest", hostsDigest, "203f1eaef2b304a1972e5313ea3d41d096aeddeee5ae1c45623154fd11fa6aef" /* Python: b8cb494707314c3de1a4489ad0b63c0dfbdfe553d368ebcd7bfa7446c9919d48 */},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("persisted identity changed: got %s want %s", tt.got, tt.want)
			}
		})
	}
}

// Flask materializes jsonify with sorted original keys. A hook that merely
// reassigns those bytes must not change which colliding redacted key wins.
func TestN8MaterializedBodyKeepsCollisionOrdering(t *testing.T) {
	payload := jsonx.Obj("private-secret-z", 1, "private-secret-a", 2, "[REDACTED]", 3)
	srv := jsonServer(t, func() any { return payload }, func(app *httpx.App) {
		app.AfterRequest(func(_ *httpx.Ctx, r *httpx.Response) { r.SetBody(r.Body()) })
	})
	for _, path := range []string{"/static/ordinary", "/static/single"} {
		srv.Get(path).WantStatus(200).WantJSON(jsonx.Obj("[REDACTED]", 1))
	}
}

func TestN8NonFiniteResponsesAreValidJSON(t *testing.T) {
	type Embedded struct {
		NonFinite float64 `json:"embedded,omitempty"`
	}
	type native struct {
		Embedded
		Values  []float64          `json:"values"`
		Map     map[string]float64 `json:"map"`
		Pointer *float64           `json:"pointer"`
		Binary  []byte             `json:"binary"`
		At      time.Time          `json:"at"`
		Omitted string             `json:"omitted,omitempty"`
		Hidden  float64            `json:"-"`
	}
	inf := math.Inf(1)
	legacyNaN := jsonx.Number("NaN")
	payload := native{Embedded: Embedded{math.NaN()}, Values: []float64{math.NaN(), inf, math.Inf(-1), 1e308}, Map: map[string]float64{"nan": math.NaN(), "finite": 2}, Pointer: &inf, Binary: []byte{1, 2, 3}, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Hidden: math.NaN()}
	cases := []struct {
		name  string
		value any
		want  any
	}{
		{"native DTO", payload, jsonx.Obj("embedded", nil, "values", []any{nil, nil, nil, jsonx.Number("1e308")}, "map", jsonx.Obj("nan", nil, "finite", 2), "pointer", nil, "binary", "AQID", "at", "2026-01-02T03:04:05Z")},
		{"legacy mixed", jsonx.Obj("none", nil, "legacy", []any{nil, jsonx.Number("NaN"), jsonx.Number("Infinity"), jsonx.Number("-Infinity")}, "native", payload), jsonx.Obj("none", nil, "legacy", []any{nil, nil, nil, nil}, "native", jsonx.Obj("embedded", nil, "values", []any{nil, nil, nil, jsonx.Number("1e308")}, "map", jsonx.Obj("nan", nil, "finite", 2), "pointer", nil, "binary", "AQID", "at", "2026-01-02T03:04:05Z"))},
		{"scalar legacy pointer", &legacyNaN, nil},
		{"scalar NaN", math.NaN(), nil}, {"scalar infinity", inf, nil}, {"scalar legacy", jsonx.Number("-Infinity"), nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv := jsonServer(t, func() any { return tt.value }, nil)
			for _, path := range []string{"/static/ordinary", "/static/single"} {
				res := srv.Get(path).WantStatus(200).WantJSON(tt.want)
				if !json.Valid(res.Body) {
					t.Fatal("non-finite response is not valid JSON")
				}
			}
		})
	}
}

func TestN8RawNonFiniteResponses(t *testing.T) {
	raw := []byte(`{"numbers":[NaN,Infinity,-Infinity,1e308],"text":"NaN Infinity -Infinity \"NaN\""}`)
	want := jsonx.Obj("numbers", []any{nil, nil, nil, jsonx.Number("1e308")}, "text", `NaN Infinity -Infinity "NaN"`)
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/raw-nonfinite", "test.raw_nonfinite", func(_ *httpx.Ctx) (*httpx.Response, error) { return httpx.RawJSON(200, raw), nil })
		r.GET("/static/value-nonfinite", "test.value_nonfinite", func(_ *httpx.Ctx) (*httpx.Response, error) { return httpx.JSON(json.RawMessage(raw)), nil })
		r.GET("/static/scalar-nonfinite", "test.scalar_nonfinite", func(_ *httpx.Ctx) (*httpx.Response, error) { return httpx.RawJSON(200, []byte("NaN")), nil })
	})})
	for _, path := range []string{"/static/raw-nonfinite", "/static/value-nonfinite"} {
		srv.Get(path).WantStatus(200).WantJSON(want)
	}
	scalar := srv.Get("/static/scalar-nonfinite").WantStatus(200)
	if string(scalar.Body) != "null" {
		t.Fatal(string(scalar.Body))
	}
}
