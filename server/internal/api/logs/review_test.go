package logs

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestReviewLogFailuresRedactStoredTokens(t *testing.T) {
	c := newLogClient(t)
	secret := "synthetic log credential"
	secretRaw, _ := json.Marshal(secret)
	err := c.repo.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		return system.SystemConfig.SetJSON("rule_proxy_token", secretRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), c.repo)
	for _, token := range []string{secret, url.QueryEscape(secret), url.QueryEscape(url.QueryEscape(secret))} {
		c.handler.LogFilePath = func() (string, error) { return "", &os.PathError{Op: "stat", Path: token, Err: os.ErrPermission} }
		for _, endpoint := range []string{"tail", "info", "clear"} {
			method := "GET"
			if endpoint == "clear" {
				method = "POST"
			}
			r, err := c.app.Invoke(ctx, httpx.InvokeRequest{Method: method, Path: "/api/logs/" + endpoint})
			if err != nil || r.Status != 500 || strings.Contains(string(r.Body), token) {
				t.Fatal("error response leaked stored token", endpoint, err)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(r.Body, &body); err != nil || body.Error != "[REDACTED]" {
				t.Fatal("failure skipped central redaction", string(r.Body), err)
			}
		}
	}
}
