package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Python _text_content uses json.dumps(ensure_ascii=False): URL query
// separators and markup stay literal instead of becoming & / <.
func TestTextContentKeepsLiteralHTMLCharacters(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tests := []struct {
		name    string
		payload json.RawMessage
		want    string
	}{
		{"object", json.RawMessage(`{"url":"https://example.test/s?a=1&b=<2>"}`), `"url": "https://example.test/s?a=1&b=<2>"`},
		{"list", json.RawMessage(`["a&b"]`), `"a&b"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewHandler(r).publicText(ctx, tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("text content %q does not contain %q", got, tc.want)
			}
		})
	}
}

// A JSON-RPC request with "id": null is a request (Python: 'id' in message),
// not a notification: it must be answered with id null.
func TestNullIDRequestIsAnswered(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := httpx.New()
	app.Repo = r
	NewHandler(r).Register(app.Router)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":null,"method":"ping"}`)).WithContext(context.Background())
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != 200 || !nativeEqual(w.Body.Bytes(), json.RawMessage(`{"jsonrpc":"2.0","id":null,"result":{}}`)) {
		t.Fatalf("null id: %d %s", w.Code, w.Body.Bytes())
	}
}

// Python logs every business failure of a tool as a warning
// ("MCP 工具 {name} 调用失败: {message}"); internal tokens stay out of the log.
func TestToolFailureIsLoggedWithoutInternalToken(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	var buf bytes.Buffer
	if _, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &buf}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr}) })
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.RuleProxyToken = "synthetic-rule-secret"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := httpx.New()
	app.Repo = r
	NewHandler(r).Register(app.Router)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"manage_node","arguments":{"action":"synthetic-rule-secret","id":"node-a"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatalf("tool failure: %d %s", w.Code, w.Body.String())
	}
	logged := buf.String()
	if !strings.Contains(logged, "WARNING - MCP 工具 manage_node 调用失败: 不支持的 action: ") {
		t.Fatalf("missing warning: %q", logged)
	}
	if strings.Contains(logged, "synthetic-rule-secret") {
		t.Fatalf("internal token logged: %q", logged)
	}
}
