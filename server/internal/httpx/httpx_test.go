package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// setSecrets stores a rule proxy token and an Agent token, the credentials
// the response hook hides (Python monkeypatched the two token readers).
func setSecrets(t *testing.T, srv *httpxtest.Server, ruleProxy []string, agentToken string) {
	t.Helper()
	profileID, err := srv.Repo.DefaultProfileID(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = ruleProxy[0]
		system.SystemConfig.RetiredRuleProxyTokens = append([]string(nil), ruleProxy[1:]...)
		system.Agents = []model.Agent{testjson.Decode[model.Agent](t, map[string]any{
			"id": "a1", "name": "agent", "token": agentToken, "profile_id": profileID,
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func payloadRoutes(payload func() any) func(*httpx.Router) {
	return func(r *httpx.Router) {
		r.GET("/static/ordinary", "test.ordinary", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(payload()), nil
		})
		r.GET("/static/single", "test.single", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.External(c, payload())
		})
	}
}

func jsonServer(t *testing.T, payload func() any, setup func(app *httpx.App)) *httpxtest.Server {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(payloadRoutes(payload)), Setup: setup})
	setSecrets(t, srv, []string{"private-secret"}, "agent-secret")
	return srv
}

// test_json_response.py::test_large_response_encodes_once_and_keeps_redaction
func TestLargeResponseEncodesOnceAndKeepsRedaction(t *testing.T) {
	build := func() any {
		items := []any{}
		for i := 0; i < 2000; i++ {
			items = append(items, map[string]any{"name": "例子.test", "secret_url": "https://host/private%252Dsecret",
				"agent-secret": "agent-secret", "rule_proxy_token": "hidden"})
		}
		return map[string]any{"items": items}
	}
	shared := build()
	original, err := json.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	srv := jsonServer(t, func() any { return shared }, nil)
	normal := srv.Get("/static/ordinary").WantStatus(200)
	single := srv.Get("/static/single").WantStatus(200)
	if string(single.Body) != string(normal.Body) {
		t.Fatal("bodies differ")
	}
	if single.Header.Get("Content-Type") != normal.Header.Get("Content-Type") ||
		single.Header.Get("Content-Length") != normal.Header.Get("Content-Length") {
		t.Fatal("headers differ")
	}
	var parsed struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(single.Body, &parsed); err != nil {
		t.Fatal(err)
	}
	if diff := testjson.Diff(parsed.Items[0], map[string]any{"name": "例子.test", "secret_url": "[REDACTED]", "[REDACTED]": "[REDACTED]"}); diff != "" {
		t.Fatal(diff)
	}
	if !testjson.Equal(shared, json.RawMessage(original)) {
		t.Fatal("payload was modified")
	}
	if !strings.HasPrefix(single.Text(), "{\"items\":[{\"[REDACTED]\":\"[REDACTED]\",\"name\":\"例子") {
		t.Fatal(single.Text()[:80])
	}
}

// test_json_response.py::test_non_native_encoder_values_and_scalars_keep_wire_behavior
// (the Python-only types cannot exist in the jsonx model).
func TestScalarsKeepWireBehavior(t *testing.T) {
	for _, payload := range []any{nil, "private-secret", []any{}, map[string]any{},
		map[string]any{"values": []any{nil, true, false, 0, -3, 1.5}}} {
		srv := jsonServer(t, func() any { return payload }, nil)
		single, ordinary := srv.Get("/static/single"), srv.Get("/static/ordinary")
		if string(single.Body) != string(ordinary.Body) {
			t.Fatalf("%s vs %s", single.Body, ordinary.Body)
		}
	}
	srv := jsonServer(t, func() any { return nil }, nil)
	if body := srv.Get("/static/ordinary").Text(); body != "null" {
		t.Fatalf("null body %q", body)
	}
	srv = jsonServer(t, func() any { return "private-secret" }, nil)
	if body := srv.Get("/static/ordinary").Text(); body != `"[REDACTED]"` {
		t.Fatalf("string body %q", body)
	}
}

// test_json_response.py::test_deep_json_keeps_global_depth_redaction
func TestDeepJSONKeepsGlobalDepthRedaction(t *testing.T) {
	build := func() any {
		var payload any = map[string]any{"value": "private-secret"}
		for i := 0; i < 150; i++ {
			payload = []any{payload}
		}
		return payload
	}
	srv := jsonServer(t, build, nil)
	single := srv.Get("/static/single")
	if string(single.Body) != string(srv.Get("/static/ordinary").Body) || !single.Contains("[REDACTED]") {
		t.Fatal("deep payload")
	}
	if single.Contains("private-secret") {
		t.Fatal("leak")
	}
}

// test_json_response.py::test_redacted_key_collisions_preserve_encoder_order
// (Flask's default provider sorts keys; Go has only that mode).
func TestRedactedKeyCollisionsPreserveEncoderOrder(t *testing.T) {
	build := func() any {
		return map[string]any{"private-secret-z": 1, "private-secret-a": 2, "[REDACTED]": 3,
			"nested": []any{map[string]any{"agent-secret-z": 4, "agent-secret-a": 5}}}
	}
	srv := jsonServer(t, build, nil)
	single := srv.Get("/static/single").Text()
	if single != srv.Get("/static/ordinary").Text() {
		t.Fatal("differ")
	}
	// sorted visit: "[REDACTED]"(3), "nested", "private-secret-a"(2), "private-secret-z"(1) → last wins
	if single != `{"[REDACTED]":1,"nested":[{"[REDACTED]":4}]}` {
		t.Fatal(single)
	}
}

// test_json_response.py::test_body_changed_after_encoding_is_sanitized_again
func TestBodyChangedAfterEncodingIsSanitizedAgain(t *testing.T) {
	srv := jsonServer(t, func() any { return map[string]any{"value": "safe"} }, func(app *httpx.App) {
		app.AfterRequest(func(c *httpx.Ctx, r *httpx.Response) {
			if c.Path() == "/static/single" {
				r.SetBody([]byte(`{"value": "agent-secret"}`))
			}
		})
	})
	wantJSON(t, srv.Get("/static/single"), map[string]any{"value": "[REDACTED]"})
}

// test_json_response.py::test_rotated_credentials_in_same_request_do_not_skip_redaction
func TestRotatedCredentialsInSameRequestDoNotSkipRedaction(t *testing.T) {
	var srv *httpxtest.Server
	srv = jsonServer(t, func() any { return map[string]any{"value": "new-private-secret"} }, func(app *httpx.App) {
		app.AfterRequest(func(c *httpx.Ctx, r *httpx.Response) {
			setSecrets(t, srv, []string{"private-secret", "new-private-secret"}, "agent-secret")
		})
	})
	wantJSON(t, srv.Get("/static/single"), map[string]any{"value": "[REDACTED]"})
}

// test_json_response.py::test_client_marker_cannot_disable_redaction
func TestClientMarkerCannotDisableRedaction(t *testing.T) {
	srv := jsonServer(t, func() any { return map[string]any{"_configflow_sanitized_json": true, "value": "agent-secret"} }, nil)
	res := srv.Get("/static/ordinary", httpxtest.Header("X-ConfigFlow-Sanitized", "true"))
	if res.Object().GetString("value") != "[REDACTED]" {
		t.Fatal(res.Text())
	}
}

func TestSensitiveKeysAreDroppedAndRawJSONBodiesAreRedacted(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/raw", "test.raw", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.RawJSON(200, []byte(`{"b":"x private-secret y","rule_fetch_proxy":"p","a":{"retired_rule_proxy_tokens":[1]}}`)), nil
		})
		r.GET("/static/text", "test.text", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.Text(200, "private-secret"), nil
		})
		r.GET("/static/file", "test.file", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Data: []byte(`{"v":"private-secret"}`), MimeType: "application/json"})
		})
	})})
	setSecrets(t, srv, []string{"private-secret"}, "agent-secret")
	if body := srv.Get("/static/raw").Text(); body != `{"a":{},"b":"[REDACTED]"}` {
		t.Fatal(body)
	}
	if body := srv.Get("/static/text").Text(); body != "private-secret" {
		t.Fatal("text bodies are not JSON and stay as they are")
	}
	if body := srv.Get("/static/file").Text(); body != `{"v":"private-secret"}` {
		t.Fatal("files are direct passthrough")
	}
}

func TestRuleFetchProxyWhitelistKeepsTheProxyForItsEndpoint(t *testing.T) {
	proxy := "http://user:private-secret@proxy.test:8080"
	route := func(endpoint string) func(*httpx.Router) {
		return func(r *httpx.Router) {
			r.GET("/static/"+endpoint, endpoint, func(c *httpx.Ctx) (*httpx.Response, error) {
				return httpx.JSON(map[string]any{"rule_fetch_proxy": proxy, "other": proxy}), nil
			})
		}
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(route("settings.handle_rule_fetch_proxy"), route("test.other"))})
	setSecrets(t, srv, []string{"private-secret"}, "agent-secret")
	wantJSON(t, srv.Get("/static/settings.handle_rule_fetch_proxy"), map[string]any{"rule_fetch_proxy": proxy, "other": "[REDACTED]"})
	wantJSON(t, srv.Get("/static/test.other"), map[string]any{"other": "[REDACTED]"})
}

func routerServer(t *testing.T, auth bool) *httpxtest.Server {
	ok := func(name string) httpx.HandlerFunc {
		return func(c *httpx.Ctx) (*httpx.Response, error) {
			params := map[string]string{}
			for key, value := range c.Params() {
				params[key] = value
			}
			return httpx.JSON(map[string]any{"route": name, "params": params, "method": c.Method()}), nil
		}
	}
	return httpxtest.New(t, httpxtest.Options{Auth: auth, Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/agents/<agent_id>", "agents.item", ok("item"))
		r.GET("/api/agents/register", "agents.register_page", ok("static"))
		r.Handle([]string{"GET", "POST"}, "/api/agents/<agent_id>/mihomo/<path:resource>", "agents.mihomo_panel", ok("panel"))
		r.POST("/api/thing", "agents.thing", ok("thing"))
		r.GET("/mcp/", "mcp.slash", ok("slash"))
		r.GET("/api/profiles/<profile_id>/stuff", "profiles.stuff", ok("stuff"))
		r.GET("/api/scoped", "scoped.view", ok("scoped"))
		r.GET("/api/conflict", "scoped.conflict", func(c *httpx.Ctx) (*httpx.Response, error) {
			return nil, repo.NewInUse("in use", []any{map[string]any{"kind": "x"}})
		})
		r.GET("/api/panic", "scoped.panic", func(c *httpx.Ctx) (*httpx.Response, error) { panic("boom") })
		r.GET("/api/none", "scoped.none", func(c *httpx.Ctx) (*httpx.Response, error) { return nil, nil })
		r.GET("/api/fail", "scoped.fail", func(c *httpx.Ctx) (*httpx.Response, error) { return nil, errors.New("x") })
	})})
}

func TestRoutingPrefersStaticSegmentsAndMatchesPaths(t *testing.T) {
	srv := routerServer(t, false)
	wantJSON(t, srv.Get("/api/agents/register"), map[string]any{"route": "static", "params": map[string]any{}, "method": "GET"})
	wantJSON(t, srv.Get("/api/agents/a1"), map[string]any{"route": "item", "params": map[string]any{"agent_id": "a1"}, "method": "GET"})
	wantJSON(t, srv.Post("/api/agents/a1/mihomo/proxies/GLOBAL/delay"), map[string]any{"route": "panel",
		"params": map[string]any{"agent_id": "a1", "resource": "proxies/GLOBAL/delay"}, "method": "POST"})
	srv.Get("/api/agents/a%2Fb").WantStatus(404) // decoded slash: two segments, as in Werkzeug
	srv.Get("/api/agents/").WantStatus(404)
}

func TestNotFoundAndMethodNotAllowedAPIErrorsAreJSON(t *testing.T) {
	srv := routerServer(t, false)
	res := srv.Get("/api/missing").WantStatus(404)
	if res.Header.Get("Content-Type") != "application/json" || res.Object().GetBool("success") ||
		!strings.Contains(res.Text(), httpx.DescNotFound) {
		t.Fatal(res.Text())
	}
	res = srv.Get("/api/thing").WantStatus(405)
	if res.Header.Get("Allow") != "OPTIONS, POST" || res.Object().GetString("message") != httpx.DescMethodNotAllowed {
		t.Fatal(res.Header, res.Text())
	}
	res = srv.Do("OPTIONS", "/api/agents/a1/mihomo/x").WantStatus(200)
	if res.Header.Get("Allow") != "GET, HEAD, OPTIONS, POST" || len(res.Body) != 0 {
		t.Fatal(res.Header)
	}
	res = srv.Get("/mcp").WantStatus(308)
	if res.Header.Get("Location") != "http://localhost/mcp/" {
		t.Fatal(res.Header)
	}
	head := srv.Do("HEAD", "/api/agents/a1").WantStatus(200)
	if len(head.Body) != 0 || head.Header.Get("Content-Length") == "0" {
		t.Fatal("HEAD keeps the GET headers without the body")
	}
}

func TestUnauthenticatedUnknownPathsGet401BeforeRouting(t *testing.T) {
	srv := routerServer(t, true)
	srv.Get("/api/missing").WantStatus(401)
	srv.Get("/api/thing").WantStatus(401)
	admin := srv.Login()
	srv.Get("/api/missing", admin).WantStatus(404)
}

func TestAuthenticationRunsBeforeProfileContext(t *testing.T) {
	srv := routerServer(t, true)
	res := srv.Get("/api/scoped", httpxtest.Header("X-ConfigFlow-Profile", "missing")).WantStatus(401)
	wantJSON(t, res, map[string]any{"success": false, "message": "请先登录后再操作。"})
	srv.Get("/api/scoped", httpxtest.Query("profile", "bad id!")).WantStatus(401)
	srv.Get("/api/scoped").WantStatus(401)
	admin := srv.Login()
	ok := srv.Get("/api/scoped", admin).WantStatus(200)
	if ok.Header.Get("X-ConfigFlow-Profile") != "default" {
		t.Fatal(ok.Header)
	}
	global := srv.Get("/api/agents/a1", admin).WantStatus(200)
	if global.Header.Get("X-ConfigFlow-Profile") != "" {
		t.Fatal("global requests carry no profile header")
	}
	srv.Get("/api/profiles/missing/stuff", admin).WantStatus(404)
}

func TestErrorHandlersAndInternalErrors(t *testing.T) {
	srv := routerServer(t, false)
	wantJSON(t, srv.Get("/api/conflict").WantStatus(409), map[string]any{"success": false, "message": "in use",
		"usages": []any{map[string]any{"kind": "x"}}})
	for _, path := range []string{"/api/panic", "/api/none", "/api/fail"} {
		res := srv.Get(path).WantStatus(500)
		if res.Object().GetBool("success") || res.Object().GetString("message") != httpx.DescInternal {
			t.Fatal(res.Text())
		}
	}
	srv.Get("/api/agents/a1").WantStatus(200) // still serving
}

func TestCORSHeaders(t *testing.T) {
	srv := routerServer(t, false)
	res := srv.Get("/api/agents/a1")
	if res.Header.Get("Access-Control-Allow-Origin") != "" || res.Header.Get("Vary") != "" {
		t.Fatal(res.Header)
	}
	res = srv.Do("OPTIONS", "/api/thing", httpxtest.Header("Origin", "http://localhost"),
		httpxtest.Header("Access-Control-Request-Method", "POST"),
		httpxtest.Header("Access-Control-Request-Headers", "X-B, authorization")).WantStatus(200)
	h := res.Header
	if h.Get("Access-Control-Allow-Origin") != "http://localhost" || h.Get("Vary") != "Origin" ||
		h.Get("Access-Control-Allow-Headers") != "X-B, authorization" ||
		h.Get("Access-Control-Allow-Methods") != "DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT" {
		t.Fatal(h)
	}
	missing := srv.Get("/api/missing", httpxtest.Header("Origin", "https://o.example"))
	if missing.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign error responses must not carry CORS headers")
	}
}

// A preflight to a protected path is answered by the gate (Flask runs
// before_request functions for OPTIONS too), with CORS headers.
func TestPreflightPassesThroughTheGate(t *testing.T) {
	srv := routerServer(t, true)
	res := srv.Do("OPTIONS", "/api/thing", httpxtest.Header("Origin", "http://localhost"),
		httpxtest.Header("Access-Control-Request-Method", "POST")).WantStatus(401)
	if res.Header.Get("Access-Control-Allow-Origin") != "http://localhost" {
		t.Fatal(res.Header)
	}
}

func TestInvokeUsesTheInternalTokenAndSkipsAuthentication(t *testing.T) {
	srv := routerServer(t, true)
	ctx := repo.WithRepository(context.Background(), srv.Repo)
	res, err := srv.App.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: "/api/scoped"})
	if err != nil || res.Status != 200 {
		t.Fatalf("%v %d %s", err, res.Status, res.Body)
	}
	res, _ = srv.App.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: "/api/scoped",
		Header: http.Header{"X-Configflow-Profile": {"missing"}}})
	if res.Status != 404 {
		t.Fatalf("profile header honoured: %d", res.Status)
	}
	srv.Get("/api/scoped", httpxtest.Header(httpx.InternalCallHeader, "guess")).WantStatus(401)
}

func TestJSONRequestParsing(t *testing.T) {
	var got any
	var gotErr error
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/static/echo", "test.echo", func(c *httpx.Ctx) (*httpx.Response, error) {
			got = nil
			gotErr = c.Bind(&got)
			var silent any
			c.BindSilent(&silent)
			return httpx.JSON(map[string]any{"silent": silent}), nil
		})
	})})
	srv.Post("/static/echo", httpxtest.Body([]byte("\xef\xbb\xbf{\"a\": 1}"), "application/json"))
	if gotErr != nil || !testjson.Equal(got, map[string]any{"a": 1}) {
		t.Fatalf("BOM: %v %v", got, gotErr)
	}
	srv.Post("/static/echo", httpxtest.Body([]byte(`{"a": 1}`), "application/merge-patch+json"))
	if gotErr != nil {
		t.Fatal("+json is JSON")
	}
	srv.Post("/static/echo", httpxtest.Body([]byte(`{"a": 1}`), "text/plain"))
	var httpErr *httpx.HTTPError
	if !errors.As(gotErr, &httpErr) || httpErr.Code != 415 {
		t.Fatalf("415 expected: %v", gotErr)
	}
	srv.Post("/static/echo", httpxtest.Body([]byte(`{"a": `), "application/json"))
	if !errors.As(gotErr, &httpErr) || httpErr.Code != 400 || httpErr.Error() != "400 Bad Request: "+httpx.DescBadRequest {
		t.Fatalf("400 expected: %v", gotErr)
	}
	utf16 := []byte{0xff, 0xfe, '{', 0, '}', 0}
	srv.Post("/static/echo", httpxtest.Body(utf16, "application/json"))
	if gotErr != nil || !testjson.Equal(got, map[string]any{}) {
		t.Fatalf("utf-16: %v %v", got, gotErr)
	}
}

func TestQueryParsingFollowsWerkzeug(t *testing.T) {
	pairs := httpx.ParseQuery("a=1&a=2&b&&c=%E4%BD%A0+x&d=%zz;e=1")
	want := [][2]string{{"a", "1"}, {"a", "2"}, {"b", ""}, {"c", "你 x"}, {"d", "%zz;e=1"}}
	if len(pairs) != len(want) {
		t.Fatal(pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Fatalf("%d: %q want %q", i, pairs[i], want[i])
		}
	}
}

func TestArgInt(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/int", "test.int", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(map[string]any{"n": c.ArgInt("n", 99)}), nil
		})
	})})
	for text, want := range map[string]int64{" 12 ": 12, "+5": 5, "-7": -7, "99999999999999999999999": 1<<63 - 1, "-99999999999999999999999": -1 << 63} {
		if got := srv.Get("/static/int", httpxtest.Query("n", text)).WantStatus(200).Object().GetInt("n"); got != want {
			t.Errorf("%q → %d, want %d", text, got, want)
		}
	}
	for _, text := range []string{"", "1.5", "1_000", "abc", "0x10"} {
		if got := srv.Get("/static/int", httpxtest.Query("n", text)).WantStatus(200).Object().GetInt("n"); got != 99 {
			t.Errorf("%q → %d, want default 99", text, got)
		}
	}
}
