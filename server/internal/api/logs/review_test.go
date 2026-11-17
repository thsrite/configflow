package logs

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestReviewLogFailuresRedactStoredTokens(t *testing.T) {
	c := newLogClient(t)
	secret := "synthetic log credential"
	_, err := c.repo.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("rule_proxy_token", secret)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), c.repo)
	for _, token := range []string{secret, url.QueryEscape(secret), url.QueryEscape(url.QueryEscape(secret))} {
		LogFilePath = func() (string, error) { return "", &os.PathError{Op: "stat", Path: token, Err: os.ErrPermission} }
		for _, endpoint := range []string{"tail", "info", "clear"} {
			method := "GET"
			if endpoint == "clear" {
				method = "POST"
			}
			r, err := c.app.Invoke(ctx, httpx.InvokeRequest{Method: method, Path: "/api/logs/" + endpoint})
			if err != nil || r.Status != 500 || strings.Contains(string(r.Body), token) {
				t.Fatal("error response leaked stored token", endpoint, err)
			}
			body, err := jsonx.Parse(r.Body)
			if err != nil || body.(*jsonx.Object).GetString("error") != "[REDACTED]" {
				t.Fatal("failure skipped central redaction", string(r.Body), err)
			}
		}
	}
}
