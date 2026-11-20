package httpx_test

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testjson"
)

func wantJSON(t *testing.T, res *httpxtest.Result, want any) {
	t.Helper()
	if diff := testjson.Diff(json.RawMessage(res.Body), want); diff != "" {
		t.Fatalf("body %s\n%s", res.Body, diff)
	}
}

func TestBindingHTTPMediaSyntaxAndFields(t *testing.T) {
	type dto struct {
		N     httpx.Number `json:"n"`
		Extra any          `json:"extra"`
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/bind", "test.bind", func(c *httpx.Ctx) (*httpx.Response, error) {
			var value dto
			if err := c.Bind(&value); err != nil {
				return nil, err
			}
			return httpx.JSON(value), nil
		})
		r.POST("/api/silent", "test.silent", func(c *httpx.Ctx) (*httpx.Response, error) {
			var value dto
			_ = json.Unmarshal([]byte(`{"n":123,"extra":"stale"}`), &value)
			ok := c.BindSilent(&value)
			return httpx.JSON(struct {
				OK    bool `json:"ok"`
				Value dto  `json:"value"`
			}{ok, value}), nil
		})
	})})
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "application/merge-patch+json", "APPLICATION/JSON"} {
		wantJSON(t, srv.Post("/api/bind", httpxtest.Body([]byte(`{"n":6e1,"extra":9007199254740993123456789}`), ct)).WantStatus(200), map[string]any{"n": json.Number("6e1"), "extra": json.Number("9007199254740993123456789")})
	}
	for _, body := range []string{"", " ", `{"n":`, "{} {}", `{"n":NaN}`, `{"n":Infinity}`, `{"n":-Infinity}`, strings.Repeat("[", 10001) + strings.Repeat("]", 10001)} {
		wantJSON(t, srv.Post("/api/bind", httpxtest.Body([]byte(body), "application/json")).WantStatus(400), map[string]any{"success": false, "message": httpx.DescBadRequest})
		wantJSON(t, srv.Post("/api/silent", httpxtest.Body([]byte(body), "application/json")).WantStatus(200), map[string]any{"ok": false, "value": map[string]any{"n": nil, "extra": nil}})
	}
	for _, ct := range []string{"", "text/plain", "text/json", "application/jsonp"} {
		srv.Post("/api/bind", httpxtest.Body([]byte(`{}`), ct)).WantStatus(415)
		wantJSON(t, srv.Post("/api/silent", httpxtest.Body([]byte(`{}`), ct)), map[string]any{"ok": false, "value": map[string]any{"n": nil, "extra": nil}})
	}
	for _, body := range [][]byte{[]byte("\xef\xbb\xbf{\"n\":60}"), {0xff, 0xfe, '{', 0, '"', 0, 'n', 0, '"', 0, ':', 0, '6', 0, '0', 0, '}', 0}} {
		wantJSON(t, srv.Post("/api/bind", httpxtest.Body(body, "application/json")).WantStatus(200), map[string]any{"n": 60, "extra": nil})
	}
	wantJSON(t, srv.Post("/api/silent", httpxtest.Body([]byte(`{}`), "application/json")), map[string]any{"ok": true, "value": map[string]any{"n": nil, "extra": nil}})
}

func TestBindingNestedDuplicatesReplaceWholeValues(t *testing.T) {
	type child struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	type dto struct {
		Object child        `json:"object"`
		Array  []int        `json:"array"`
		Scalar int          `json:"scalar"`
		Raw    httpx.Number `json:"raw"`
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/bind", "test.bind", func(c *httpx.Ctx) (*httpx.Response, error) {
			var v dto
			if err := c.Bind(&v); err != nil {
				return nil, err
			}
			return httpx.JSON(v), nil
		})
	})})
	body := `{"object":{"a":1,"b":2},"object":{"a":3,"a":4},"array":[1,2],"array":[3],"scalar":1,"scalar":2,"raw":{"x":{"a":1},"x":[2],"x":3}}`
	wantJSON(t, srv.Post("/api/bind", httpxtest.Body([]byte(body), "application/json")).WantStatus(200), map[string]any{"object": map[string]any{"a": 4, "b": 0}, "array": []any{3}, "scalar": 2, "raw": map[string]any{"x": 3}})
}

func TestBoundNumberPreservesFormsAndNativeParsing(t *testing.T) {
	for _, tc := range []struct{ raw, form string }{
		{"60", "int"}, {"-0", "int"}, {"60.0", "number"}, {"6e1", "number"}, {"9223372036854775808", "int"},
		{"true", "bool"}, {"false", "bool"}, {`" +5_0 ٥ "`, "string"}, {`"\u0036\ud800"`, "string"}, {"null", "null"}, {"[]", "container"}, {"{}", "container"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var n httpx.Number
			if err := json.Unmarshal([]byte(tc.raw), &n); err != nil {
				t.Fatal(err)
			}
			if n.Raw() != tc.raw || !n.IsSet() {
				t.Fatalf("lost literal %q", n.Raw())
			}
			got := ""
			switch {
			case n.IsIntegerLiteral():
				got = "int"
			case n.IsNumber():
				got = "number"
			case n.IsBool():
				got = "bool"
			case n.IsString():
				got = "string"
			case n.IsNull():
				got = "null"
			case n.IsContainer():
				got = "container"
			}
			if got != tc.form {
				t.Fatalf("form %s want %s", got, tc.form)
			}
			if tc.form != "int" {
				if _, err := n.Int64(); !errors.Is(err, httpx.ErrNumberForm) {
					t.Fatal(err)
				}
			}
			data, err := json.Marshal(n)
			if err != nil || string(data) != tc.raw {
				t.Fatalf("%s %v", data, err)
			}
		})
	}
	var n httpx.Number
	_ = json.Unmarshal([]byte("9223372036854775808"), &n)
	if _, err := n.Int64(); !errors.Is(err, strconv.ErrRange) {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(`" +5_0 ٥ "`), &n)
	if text, ok := n.Text(); !ok || text != " +5_0 ٥ " {
		t.Fatalf("string coercion: %q", text)
	}
	var zero httpx.Number
	if zero.IsSet() || zero.IsNull() {
		t.Fatal("missing became null")
	}
	for _, bad := range []string{"NaN", "Infinity", "-Infinity"} {
		if err := json.Unmarshal([]byte(bad), &n); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestBindingAndNonFiniteResponseUseSeparatePolicies(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/bind", "test.bind", func(c *httpx.Ctx) (*httpx.Response, error) {
			var v struct {
				N httpx.Number `json:"n"`
			}
			if err := c.Bind(&v); err != nil {
				return nil, err
			}
			return httpx.JSON(map[string]any{"n": v.N, "missing": math.NaN()}), nil
		})
	})})
	wantJSON(t, srv.Post("/api/bind", httpxtest.Body([]byte(`{"n":1e400}`), "application/json")).WantStatus(200), map[string]any{"n": json.Number("1e400"), "missing": nil})
}

func TestBindingRetainsScalarLexemesAfterNativeTokens(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/bind", "test.lexeme", func(c *httpx.Ctx) (*httpx.Response, error) {
			var dto struct {
				N httpx.Number `json:"n"`
			}
			if err := c.Bind(&dto); err != nil {
				return nil, err
			}
			return httpx.JSON(map[string]string{"raw": dto.N.Raw()}), nil
		})
	})})
	for _, literal := range []string{"6e1", "60.00", `"\u0036\ud800"`, "true", "null"} {
		source := `{"前缀":"中","n" : ` + literal + `}`
		wantJSON(t, srv.Post("/api/bind", httpxtest.Body([]byte(source), "application/json")).WantStatus(200), map[string]any{"raw": literal})
	}
}
