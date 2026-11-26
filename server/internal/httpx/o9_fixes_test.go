package httpx_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

func TestO9AccessLogRedactsCredentials(t *testing.T) {
	edge, _ := edgeFixture(t)
	var log bytes.Buffer
	edge.AccessLog = &log
	edgeDo(edge, "GET", "/api/missing?token=synthetic-access-secret&ok=yes", nil, map[string]string{"Referer": "https://example.com/?token=synthetic-referrer-secret"})
	for _, secret := range []string{"synthetic-access-secret", "synthetic-referrer-secret"} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("credential leaked: %s", log.String())
		}
	}
	if !strings.Contains(log.String(), "ok=yes") {
		t.Fatal("non-sensitive request query lost")
	}
}

// A timing test cannot reliably prove constant-time comparison. Guard the
// implementation as well as checking the internal-call behaviour in httpx_test.
func TestO9InternalCallUsesConstantTimeComparison(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "ctx.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "IsInternalCall" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "subtle" && sel.Sel.Name == "ConstantTimeCompare" {
				found = true
			}
			return true
		})
	}
	if !found {
		t.Fatal("internal-call credential must use subtle.ConstantTimeCompare")
	}
}

func TestO9NoSniffStaticAndTextDownloads(t *testing.T) {
	edge, _ := edgeFixture(t)
	for _, path := range []string{"/", "/assets/index-AbCdEf12.js", "/assets/missing.js", "/frontend-version.json", "/assets/plain.txt"} {
		t.Run(path, func(t *testing.T) {
			if w := edgeDo(edge, "GET", path, nil, nil); w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal(w.Header())
			}
		})
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/rules/local/<name>", "rules.download", func(*httpx.Ctx) (*httpx.Response, error) { return httpx.Text(200, "DOMAIN,example.com\n"), nil })
	})})
	for _, method := range []string{"GET", "HEAD"} {
		res := srv.Do(method, "/api/rules/local/test").WantStatus(200)
		if res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(res.Header)
		}
	}
}

func TestO9AnonymousProfileResolutionRequiresAuthentication(t *testing.T) {
	srv := routerServer(t, true)
	for _, path := range []string{"/api/scoped?profile=default", "/api/scoped?profile=missing", "/api/scoped?profile=bad!id", "/api/profiles/missing/stuff"} {
		res := srv.Get(path).WantStatus(401)
		if res.Header.Get("X-ConfigFlow-Profile") != "" {
			t.Fatal("unauthenticated response disclosed profile", res.Header)
		}
	}
	srv.Get("/api/scoped", httpxtest.Header("X-ConfigFlow-Profile", "missing")).WantStatus(401)
	srv.Get("/api/scoped?profile=missing", srv.Login()).WantStatus(404)
}

// Promoted from go-opus-o9-security's o9repro build-tag reproduction.
func TestO9ReproCORSReflectsForeignOrigin(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(echoRoutes)})
	res := srv.Do("OPTIONS", "/api/echo", httpxtest.Header("Origin", "https://attacker.example"), httpxtest.Header("Access-Control-Request-Method", "POST"))
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal(res.Header)
	}
}

func TestO9CORSOnlySameOrigin(t *testing.T) {
	tests := []struct {
		name, origin, target string
		allow                bool
	}{
		{"same", "http://config.example:8080", "http://config.example:8080/api/echo", true},
		{"tls", "https://config.example:8443", "https://config.example:8443/api/echo", true},
		{"default-port", "http://config.example", "http://config.example:80/api/echo", true},
		{"foreign", "https://attacker.example", "http://config.example:8080/api/echo", false},
		{"port", "http://config.example:8081", "http://config.example:8080/api/echo", false},
		{"scheme", "https://config.example:8080", "http://config.example:8080/api/echo", false},
		{"absent", "", "http://config.example:8080/api/echo", false},
		{"null", "null", "http://config.example:8080/api/echo", false},
		{"path", "http://config.example:8080/evil", "http://config.example:8080/api/echo", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(echoRoutes)})
			for _, method := range []string{"GET", "OPTIONS"} {
				for _, handler := range []http.Handler{srv.Handler(), &httpx.Edge{API: srv.Handler()}} {
					req := httptest.NewRequest(method, test.target, nil)
					req.Header.Set("Origin", test.origin)
					// Forwarded headers supplied by an external client never change the origin.
					req.Header.Set("X-Forwarded-Host", "attacker.example")
					req.Header.Set("X-Forwarded-Proto", "https")
					req.Header.Set("Access-Control-Request-Method", "POST")
					req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					if test.allow {
						if w.Header().Get("Access-Control-Allow-Origin") != test.origin || w.Header().Get("Vary") != "Origin" {
							t.Fatal(w.Header())
						}
						if method == "OPTIONS" && w.Header().Get("Access-Control-Allow-Methods") == "" {
							t.Fatal(w.Header())
						}
					} else {
						for name := range w.Header() {
							if strings.HasPrefix(name, "Access-Control-") {
								t.Fatal(w.Header())
							}
						}
					}
				}
			}
		})
	}
}

func TestO9ReverseProxyOriginDoesNotRejectWrites(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.Handle([]string{"POST", "PUT"}, "/api/write", "test.write", func(c *httpx.Ctx) (*httpx.Response, error) {
			data, err := c.Data()
			if err != nil {
				return nil, err
			}
			return httpx.Text(202, string(data)), nil
		})
	})})
	for _, method := range []string{"POST", "PUT"} {
		req := httptest.NewRequest(method, "http://127.0.0.1:5001/api/write", strings.NewReader("synthetic write"))
		req.Header.Set("Origin", "https://panel.example")
		req.Header.Set("X-Forwarded-Host", "panel.example")
		req.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != 202 || w.Body.String() != "synthetic write" || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
	}
}

func TestO9BackendAndEdgeSharePathBodyLimits(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		for _, path := range []string{"/api/oxidns/settings", "/api/oxidns/render", "/api/profiles/<profile_id>/oxidns/settings", "/api/profiles/<profile_id>/oxidns/render", "/api/echo"} {
			r.POST(path, "test."+path, func(c *httpx.Ctx) (*httpx.Response, error) {
				_, err := c.Data()
				if err != nil {
					return nil, err
				}
				return httpx.Text(200, "accepted"), nil
			})
		}
	})})
	for _, handler := range []http.Handler{httpx.LimitRequestBody(srv.Handler()), &httpx.Edge{API: srv.Handler()}} {
		for _, path := range []string{"/api/oxidns/settings", "/api/oxidns/render", "/api/profiles/default/oxidns/settings", "/api/profiles/default/oxidns/render", "/api/echo"} {
			for _, chunked := range []bool{false, true} {
				req := httptest.NewRequest("POST", path, strings.NewReader(strings.Repeat("x", httpx.DefaultBodyLimit+1)))
				if chunked {
					req.ContentLength = -1
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				want := 200
				if path == "/api/echo" {
					want = 413
				}
				if w.Code != want {
					t.Fatalf("%s chunked=%v: %d want %d", path, chunked, w.Code, want)
				}
			}
			req := httptest.NewRequest("POST", path, strings.NewReader(""))
			req.ContentLength = httpx.OxidnsBodyLimit + 1
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 413 {
				t.Fatalf("%s: declared 96 MiB overflow: %d", path, w.Code)
			}
		}
	}
}

func TestO9StreamingOverflowCannotBecomeSuccess(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/stream", "test.stream", func(c *httpx.Ctx) (*httpx.Response, error) {
			_, _ = io.Copy(io.Discard, c.Stream())
			return httpx.Text(200, "fallback"), nil
		})
	})})
	for _, handler := range []http.Handler{httpx.LimitRequestBody(srv.Handler()), &httpx.Edge{API: srv.Handler()}} {
		req := httptest.NewRequest("POST", "/api/stream", strings.NewReader(strings.Repeat("x", httpx.DefaultBodyLimit+1)))
		req.ContentLength = -1
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 413 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
