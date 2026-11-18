// Package difftest compares A21 public HTTP and storage with real Flask.
package difftest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apionboarding "configflow/internal/api/gatewayonboarding"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/setup"
	"configflow/internal/setupflow"
)

const validForm = `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`

func testServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	setupflow.Wire()
	t.Cleanup(onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite))
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apionboarding.Register)})
	ctx := s.Context()
	_, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("nodes", []any{jsonx.Obj("id", "n", "name", "Example", "type", "ss", "server", "192.0.2.2", "port", 443, "cipher", "aes-128-gcm", "password", "synthetic-node-secret")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Proxy", "type", "select", "manual_nodes", []any{"n"})})
		c.Set("rule_configs", []any{jsonx.Obj("id", "match", "itemType", "rule", "rule_type", "MATCH", "policy", "Proxy")})
		c.Set("mihomo", jsonx.Obj("custom_config", "mixed-port: 7890\n"))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func requestBody(text string) httpxtest.Option {
	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(text))
		r.ContentLength = int64(len(text))
		r.Header.Set("Content-Type", "application/json")
	}
}
func snapshot(t *testing.T, s *httpxtest.Server) json.RawMessage {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(s.DataDir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), "configflow.sqlite3") || strings.HasSuffix(e.Name(), ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.DataDir, path)
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonx.Compact(doc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(struct {
		Document json.RawMessage
		Files    map[string]string
	}{raw, files})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
