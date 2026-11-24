package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/server"
	"configflow/internal/weburl"
)

func c06Python(t *testing.T, script string, input any) []byte {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1", "ADMIN_USERNAME=matrix-admin", "ADMIN_PASSWORD=matrix-password", "JWT_SECRET_KEY=matrix-secret")
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("C06 Python oracle: %v: %s", err, stderr.String())
	}
	return out
}

func TestPythonTextRedactionAndStoredSecretFinder(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	secrets := []string{"abcab", "abc", "雪 /?+token"}
	texts := []string{"", "safe text", "雪🙂 abcabcab end", "prefix abcab\r\nsafe\n", "abcab\vabc\u2028safe"}
	for _, secret := range secrets {
		encoded := url.QueryEscape(secret)
		texts = append(texts, "prefix "+secret+" suffix", "prefix "+encoded+" suffix", "prefix "+strings.ToLower(encoded)+" suffix")
		for range 8 {
			encoded = url.QueryEscape(encoded)
			texts = append(texts, "safe\nencoded "+encoded+" tail\r\n")
		}
	}
	output := c06Python(t, `
import json, sys
from backend.common import config_export as c
data = json.load(sys.stdin)
secrets = set(data['secrets'])
c._external_payload_secrets = lambda *args, **kwargs: secrets
find = c.stored_secret_finder()
out = [{'contains': c._contains_encoded_secret(text, secrets), 'text': c.redact_secrets_in_text(text), 'ranges': find(text)} for text in data['texts']]
snapshot = find('abcab rotated-secret')
c._external_payload_secrets = lambda *args, **kwargs: {'rotated-secret'}
assert find('abcab rotated-secret') == snapshot
json.dump({'rows':out, 'snapshot':snapshot}, sys.stdout, ensure_ascii=False)
`, map[string]any{"secrets": secrets, "texts": texts})
	var expected struct {
		Rows []struct {
			Contains bool
			Text     string
			Ranges   [][2]int
		}
		Snapshot [][2]int
	}
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(context.Background())
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = secrets[0]
		system.SystemConfig.RetiredRuleProxyTokens = []string{secrets[1], secrets[2]}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	find, err := redact.StoredSecretFinder(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(expected.Rows) != len(texts) {
		t.Fatal("truncated oracle")
	}
	ranges := func(text string) [][2]int {
		out := [][2]int{}
		for _, span := range find(text) {
			out = append(out, [2]int{span.Start, span.End})
		}
		return out
	}
	for i, text := range texts {
		actual, err := redact.RedactSecretsInText(ctx, r, text)
		if err != nil {
			t.Fatal(err)
		}
		want := expected.Rows[i]
		if actual != want.Text || redact.ContainsEncodedSecret(text, redact.NewSecrets(secrets)) != want.Contains || !reflect.DeepEqual(ranges(text), want.Ranges) {
			t.Fatalf("row %d: text=%q ranges=%v Python=%+v", i, actual, ranges(text), want)
		}
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = "rotated-secret"
		system.SystemConfig.RetiredRuleProxyTokens = []string{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ranges("abcab rotated-secret"), expected.Snapshot) {
		t.Fatal("finder reread secrets instead of keeping its snapshot")
	}
}

func TestPythonAuthenticationMatrixForEveryRegisteredRoute(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	full, err := server.New(t.Context(), server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	routes := full.HTTP.Router.Routes()
	type entry struct{ Path, Endpoint, Method string }
	var entries []entry
	parameter := regexp.MustCompile(`<[^>]+>`)
	for _, route := range routes {
		path := parameter.ReplaceAllStringFunc(route.Path, func(parameter string) string {
			if strings.Contains(parameter, "profile") {
				return "default"
			}
			if strings.Contains(parameter, "int:") {
				return "1"
			}
			return "synthetic"
		})
		methods := append([]string{}, route.Methods...)
		if strings.Contains(strings.Join(methods, ","), "GET") {
			methods = append(methods, "HEAD")
		}
		methods = append(methods, "OPTIONS")
		for _, method := range methods {
			entries = append(entries, entry{path, route.Endpoint, method})
		}
	}
	output := c06Python(t, `
import json, sys
from flask import Flask, request
from backend.routes.auth import setup_before_request
from backend.routes import register_blueprints
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)
gate = next(h for h in app.before_request_funcs[None] if h.__name__ == "before_request_auth")
out = []
for row in json.load(sys.stdin):
    with app.test_request_context(row['Path'], method=row['Method']):
        response = gate()
        out.append(200 if response is None else app.make_response(response).status_code)
json.dump(out, sys.stdout)
`, entries)
	var expected []int
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(entries) || len(entries) == 0 {
		t.Fatal("empty or truncated matrix")
	}
	previous := auth.Configure(auth.Config{AdminUsername: "matrix-admin", AdminPassword: "matrix-password", SecretKey: "matrix-secret"})
	t.Cleanup(func() { auth.Configure(previous) })
	r, err = repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := httpx.New()
	app.Repo = r
	for _, route := range routes {
		app.Router.Handle(route.Methods, route.Path, route.Endpoint, func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(struct {
				Reached bool `json:"reached"`
			}{true}), nil
		})
	}
	auth.Install(app)
	token, err := auth.GenerateToken("matrix-admin")
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range entries {
		for _, credential := range []struct {
			token  string
			status int
		}{{"", expected[i]}, {"malformed", expected[i]}, {token, http.StatusOK}} {
			request := httptest.NewRequest(row.Method, row.Path, nil)
			if credential.token != "" {
				request.Header.Set("Authorization", "Bearer "+credential.token)
			}
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request.WithContext(request.Context()))
			if response.Code != credential.status {
				t.Fatalf("%s %s endpoint=%s: status=%d Python gate=%d body=%s", row.Method, row.Path, row.Endpoint, response.Code, credential.status, response.Body)
			}
		}
	}
	t.Logf("%d registered routes; %d method/path rows, checked anonymous, invalid and valid JWT", len(routes), len(entries))
}

func TestPythonSafeRequestErrorDetails(t *testing.T) {
	const privateURL = "https://upstream.example.com/sub?token=synthetic-request-secret#synthetic-fragment"
	output := c06Python(t, `
import json, requests, sys
from backend.utils.url_utils import safe_exception_details
url = json.load(sys.stdin)
response = requests.Response()
response.status_code = 502
json.dump(safe_exception_details(requests.HTTPError('failed for '+url, response=response)),sys.stdout)
`, privateURL)
	var expected string
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	actual := weburl.SafeErrorDetails(&rulefetch.FetchError{Class: "HTTPError", Status: 502, Message: "failed for " + privateURL})
	if actual != expected {
		t.Fatalf("safe metadata %q, Python %q", actual, expected)
	}
	for _, private := range []string{"upstream.example.com", "synthetic-request-secret", "synthetic-fragment"} {
		if strings.Contains(actual, private) {
			t.Fatal("private URL escaped")
		}
	}
}
