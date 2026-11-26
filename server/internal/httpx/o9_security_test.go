package httpx_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// O9 round 1: the Edge replaces nginx. Static files must never be served
// from outside Root, whatever dot segments or encodings the request uses.
func TestO9EdgeStaticNeverLeavesRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "dist")
	os.MkdirAll(filepath.Join(root, "assets"), 0o755)
	os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>spa</html>"), 0o644)
	os.WriteFile(filepath.Join(base, "secret.js"), []byte("outside-root-secret"), 0o644)
	os.WriteFile(filepath.Join(base, "secret.txt"), []byte("outside-root-secret"), 0o644)
	srv := httpxtest.New(t, httpxtest.Options{})
	edge := &httpx.Edge{API: srv.Handler(), Root: root}
	for _, target := range []string{
		"/../secret.js", "/assets/../../secret.js", "/assets/..%2f..%2fsecret.txt", "/%2e%2e/secret.txt",
		"/assets/%2e%2e/%2e%2e/secret.txt", "/assets/..%5c..%5csecret.txt", "/frontend-version.json/../../secret.js",
	} {
		req := httptest.NewRequest("GET", "http://config.example"+target, nil)
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "outside-root-secret") {
			t.Errorf("%s served a file outside Root", target)
		}
	}
}

// The gate checks the raw request path against public prefixes while the
// Edge routes on the normalised one. A dot segment after a public prefix must
// never reach a protected handler.
func TestO9GateDotSegmentsAfterPublicPrefixStayProtected(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/o9/secret", "test.o9_secret", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(map[string]any{"secret": "o9-protected"}), nil
		})
		r.GET("/api/o9/files/<path:name>", "test.o9_files", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(map[string]any{"secret": "o9-protected"}), nil
		})
	})})
	if !auth.IsAuthEnabled() {
		t.Fatal("auth not enabled")
	}
	edge := &httpx.Edge{API: srv.Handler(), Root: t.TempDir()}
	for _, target := range []string{
		"/api/o9/secret", "/assets/../api/o9/secret", "/static/../api/o9/secret", "/api/rules/local/../../o9/secret",
		"/api/rule-library/content/../../o9/secret", "/api/static/agents/../../o9/secret", "/mcp/../api/o9/secret",
		"/api/rules/local/../../o9/files/x", "/api/config/a/loon/../../../o9/secret",
	} {
		req := httptest.NewRequest("GET", "http://config.example"+target, nil)
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "o9-protected") {
			t.Errorf("%s reached a protected handler without login: %d", target, w.Code)
		}
		// Direct access to the backend port (no Edge) must hold as well.
		direct := srv.Get(target)
		if strings.Contains(string(direct.Body), "o9-protected") {
			t.Errorf("%s reached a protected handler on the backend port: %d", target, direct.Status)
		}
	}
}

// GO-2026-6859: an HTTP/1 server using http.MaxBytesReader (the Edge's body
// limits) can be crashed remotely before Go 1.26.11. go.mod pins the
// toolchain; this guards builds that override it (GOTOOLCHAIN=local).
func TestO9ToolchainHasMaxBytesReaderFix(t *testing.T) {
	version := strings.TrimPrefix(runtime.Version(), "go")
	if i := strings.IndexAny(version, "-+ "); i >= 0 {
		version = version[:i]
	}
	parts := strings.Split(version, ".")
	number := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			t.Skipf("unparsable toolchain version %q", runtime.Version())
		}
		return n
	}
	major, minor, patch := number(0), number(1), number(2)
	if major == 1 && (minor < 26 || minor == 26 && patch < 11) {
		t.Fatalf("toolchain %s is affected by GO-2026-6859; build with go1.26.11 or later", runtime.Version())
	}
}
