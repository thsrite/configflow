package substore_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server/servertest"
	"configflow/internal/substore"
	"configflow/internal/testgolden"
)

func ssFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func ssFakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/utils/env" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"2.40.0"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

var (
	ssLocalPort = regexp.MustCompile(`127\.0\.0\.1:\d+`)
	ssTmpDir    = regexp.MustCompile(`/[^\s"]+/(TestSubStoreGoldenReplay|substore)[^\s"]*`)
)

func normalizeSSGolden(v any, bundleDir string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "checked_at" || k == "updated_at" {
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			}
			out[k] = normalizeSSGolden(val, bundleDir)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeSSGolden(item, bundleDir)
		}
		return out
	case string:
		s := x
		if bundleDir != "" {
			s = strings.ReplaceAll(s, bundleDir, "<BUNDLE>")
		}
		s = ssLocalPort.ReplaceAllString(s, "127.0.0.1:<port>")
		return ssTmpDir.ReplaceAllString(s, "<TMP>")
	}
	return v
}

func TestSubStoreGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	runMode := func(t *testing.T, prefix string, env map[string]string, bundleDir string, reqs []struct {
		name, method, path, body string
		anonymous                bool
	}) {
		for k, v := range env {
			t.Setenv(k, v)
		}
		s, _ := servertest.New(t, httpxtest.Options{
			Auth: true,
		})
		admin := s.Login()
		for _, r := range reqs {
			var opts []httpxtest.Option
			if !r.anonymous {
				opts = append(opts, admin)
			}
			if r.body != "" {
				opts = append(opts, httpxtest.Body([]byte(r.body), "application/json"))
			}
			resp := s.Do(r.method, r.path, opts...)
			var parsed any
			if err := json.Unmarshal(resp.Body, &parsed); err != nil {
				parsed = string(resp.Body)
			}
			key, _ := json.Marshal(map[string]string{"mode": prefix, "case": r.name})
			actual, _ := json.Marshal(normalizeSSGolden(map[string]any{
				"status": resp.Status,
				"body":   parsed,
			}, bundleDir))
			ref.Assert(t, key, actual)
		}
	}

	const modePath = "/api/settings/dependencies/sub-store/mode"
	const updatePath = "/api/settings/dependencies/sub-store/update"

	t.Run("bundled", func(t *testing.T) {
		fake := ssFakeServer(t)
		bundle := t.TempDir()
		_ = os.WriteFile(filepath.Join(bundle, substore.SubStoreAsset), []byte("bundle"), 0o644)
		_ = os.WriteFile(filepath.Join(bundle, "VERSION"), []byte("2.40.0\n"), 0o644)
		port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
		proxy := "http://127.0.0.1:" + strconv.Itoa(ssFreePort(t))
		runMode(t, "bundled", map[string]string{
			"SUB_STORE_BUILTIN_DIR":      bundle,
			"SUB_STORE_BACKEND_API_PORT": port,
			"HTTPS_PROXY":                proxy,
			"HTTP_PROXY":                 proxy,
			"NO_PROXY":                   "127.0.0.1,localhost",
			"SUB_STORE_URL":              "",
		}, bundle, []struct {
			name, method, path, body string
			anonymous                bool
		}{
			{"list-anon", "GET", "/api/settings/dependencies", "", true},
			{"list", "GET", "/api/settings/dependencies", "", false},
			{"mode-no-body", "POST", modePath, "", false},
			{"mode-list-body", "POST", modePath, `[]`, false},
			{"mode-bad-value", "POST", modePath, `{"mode":"stop"}`, false},
			{"mode-list-value", "POST", modePath, `{"mode":["always"]}`, false},
			{"mode-broken-json", "POST", modePath, `{"mode"`, false},
			{"mode-anon", "POST", modePath, `{"mode":"always"}`, true},
			{"mode-always", "POST", modePath, `{"mode":"always"}`, false},
			{"list-after-always", "GET", "/api/settings/dependencies", "", false},
			{"mode-on-demand", "POST", modePath, `{"mode":"on_demand"}`, false},
			{"update-unreachable", "POST", updatePath, "", false},
			{"update-anon", "POST", updatePath, "", true},
			{"update-get-405", "GET", updatePath, "", false},
		})
	})

	t.Run("external", func(t *testing.T) {
		fake := ssFakeServer(t)
		proxy := "http://127.0.0.1:" + strconv.Itoa(ssFreePort(t))
		runMode(t, "external", map[string]string{
			"SUB_STORE_BUILTIN_DIR": "/nonexistent",
			"SUB_STORE_URL":         fake.URL + "/",
			"HTTPS_PROXY":           proxy,
			"HTTP_PROXY":            proxy,
			"NO_PROXY":              "127.0.0.1,localhost",
		}, "", []struct {
			name, method, path, body string
			anonymous                bool
		}{
			{"list", "GET", "/api/settings/dependencies", "", false},
			{"mode-external", "POST", modePath, `{"mode":"always"}`, false},
			{"mode-bad", "POST", modePath, `{}`, false},
			{"update-without-bundle", "POST", updatePath, "", false},
		})
	})
}
