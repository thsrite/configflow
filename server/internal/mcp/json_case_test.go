package mcp

import (
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditRPCExactMembers(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	db, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app := httpx.New()
	app.Repo = db
	NewHandler(db).Register(app.Router)
	for _, tc := range []struct {
		raw    string
		status int
		want   string
	}{
		{`{"jsonrpc":"2.0","id":1,"METHOD":"ping"}`, 200, `"code":-32600`},
		{`{"jsonrpc":"2.0","ID":1,"method":"ping"}`, 202, ""},
		{`{"jsonrpc":"2.0","id":1,"method":"ping","METHOD":"unknown"}`, 200, `"result":{}`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/mcp", strings.NewReader(tc.raw)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
}
