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
	"configflow/internal/model"
	"configflow/internal/setup"
)

const validForm = `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`

func testServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	t.Cleanup(onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite))
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apionboarding.Register)})
	ctx := s.Context()
	err := s.Repo.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("nodes", json.RawMessage(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, c *model.Profile) error {
		if err := c.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`)); err != nil {
			return err
		}
		if err := c.SetJSON("rule_configs", json.RawMessage(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`)); err != nil {
			return err
		}
		return c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mixed-port: 7890\n"}`))
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
	raw, err := s.Repo.DiagnosticExportJSON(s.Context())
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
	result, err := json.Marshal(struct {
		Document json.RawMessage
		Files    map[string]string
	}{raw, files})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
