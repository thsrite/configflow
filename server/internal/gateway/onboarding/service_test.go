package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
)

func testService(t *testing.T) *Service {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	setupflow.Wire()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.UpdateSharedTransaction(context.Background(), func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("nodes", []any{jsonx.Obj("id", "n", "name", "Example", "type", "ss", "server", "192.0.2.2", "port", 443, "cipher", "aes-128-gcm", "password", "synthetic-node-secret")})
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.UpdateProfileTransaction(context.Background(), "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Proxy", "type", "select", "manual_nodes", []any{"n"})})
		c.Set("rule_configs", []any{jsonx.Obj("id", "match", "itemType", "rule", "rule_type", "MATCH", "policy", "Proxy")})
		c.Set("mihomo", jsonx.Obj("custom_config", "mixed-port: 7890\n", "future_member", jsonx.Obj("keep", false)))
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	return &Service{Repository: r, DNSLoop: noLoop}
}
func testOperation() Operation {
	return Operation{ProfileID: "default", BaseURL: "https://panel.example", Write: true, Body: json.RawMessage(`{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`)}
}
func stored(t *testing.T, s *Service) string {
	t.Helper()
	v, err := s.Repository.ExportAll(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := jsonx.Compact(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func files(t *testing.T, s *Service) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(s.Repository.DataDir(), func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "configflow.sqlite3") || strings.HasSuffix(base, ".lock") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.Repository.DataDir(), path)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func writeSession(t *testing.T, s *Service, raw string) {
	t.Helper()
	dir := filepath.Join(s.Repository.DataDir(), "setup-sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strings.Repeat("a", 32)+".json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSetupReadinessContract(t *testing.T) {
	for _, tc := range []struct {
		name, session string
		blocked       bool
	}{{"none", "", false}, {"unfinished", `{"profile_id":"default","goal":"gateway","completed":false}`, true}, {"corrupt", "{", true}, {"wrong-evidence", `{"profile_id":"default","completed":[]}`, true}, {"complete", `{"profile_id":"default","completed":true}`, false}, {"abandoned", `{"profile_id":"default","abandoned":true}`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testService(t)
			t.Cleanup(SetAdoptOwnedWrite(nil))
			if tc.session != "" {
				writeSession(t, s, tc.session)
			}
			before := stored(t, s)
			beforeFiles := encode(files(t, s))
			revision, _ := s.Repository.ConfigRevision(context.Background(), "default")
			_, err := s.Execute(context.Background(), testOperation())
			if tc.blocked {
				afterRevision, _ := s.Repository.ConfigRevision(context.Background(), "default")
				if !errors.Is(err, ErrSetupUnavailable) || stored(t, s) != before || !bytes.Equal(beforeFiles, encode(files(t, s))) || afterRevision != revision {
					t.Fatalf("blocked save err=%v changed=%v", err, stored(t, s) != before)
				}
			} else if err != nil || stored(t, s) == before {
				t.Fatalf("normal save err=%v", err)
			}
		})
	}
}
func TestPreviewAndSaveHaveSameDrafts(t *testing.T) {
	s := testService(t)
	op := testOperation()
	op.Preview = true
	before := stored(t, s)
	beforeFiles := encode(files(t, s))
	raw, err := s.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	var preview Report
	if err = json.Unmarshal(raw, &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.Ready || before != stored(t, s) || !bytes.Equal(beforeFiles, encode(files(t, s))) {
		t.Fatalf("preview %+v", preview)
	}
	config, _, err := s.Repository.GetPublishSnapshot(context.Background(), "default", true)
	if err != nil {
		t.Fatal(err)
	}
	native, err := fromLegacy(config)
	if err != nil {
		t.Fatal(err)
	}
	form, err := ParseRequest(op.Body)
	if err != nil {
		t.Fatal(err)
	}
	candidate, report, err := Prepare(context.Background(), native, form, Options{BaseURL: op.BaseURL, DNSLoop: noLoop, Takeover: "selective"})
	if err != nil || !report.Ready {
		t.Fatalf("prepare %+v %v", report, err)
	}
	op.Preview = false
	raw, err = s.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	var saved Report
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	current, err := s.Repository.GetProfile(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := fromLegacy(current)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mihomo", "oxidns"} {
		equalJSON(t, "saved "+key, actual[key], candidate[key])
	}
	if !saved.Draft || saved.Revision == nil || saved.PublishGuards["mihomo"].Revision != *saved.Revision || saved.PublishGuards["oxidns"].Revision != *saved.Revision {
		t.Fatal("unbound publication guards", saved)
	}
}
func TestOnboardingRuntimeShutdownAndDisconnect(t *testing.T) {
	for _, stage := range []string{"shutdown-before", "shutdown-during", "disconnect"} {
		t.Run(stage, func(t *testing.T) {
			s := testService(t)
			before := stored(t, s)
			beforeFiles := encode(files(t, s))
			runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			request, cancel := context.WithCancel(context.Background())
			ctx := runtime.BindRequest(request)
			cancel()
			if stage == "shutdown-before" {
				runtime.Shutdown(context.Background())
			}
			if stage == "shutdown-during" {
				s.DNSLoop = func(*yamlx.Node, []netip.Addr) (bool, error) {
					runtime.Shutdown(context.Background())
					return false, nil
				}
			}
			_, err := s.Execute(ctx, testOperation())
			if stage == "disconnect" {
				if err != nil || stored(t, s) == before {
					t.Fatalf("request disconnect aborted save %v", err)
				}
			} else if !errors.Is(err, context.Canceled) || stored(t, s) != before || !bytes.Equal(beforeFiles, encode(files(t, s))) {
				t.Fatalf("shutdown committed err=%v changed=%v", err, stored(t, s) != before)
			}
		})
	}
}
func TestConcurrentExpectedRevisionAndStaleGuard(t *testing.T) {
	s := testService(t)
	revision, _ := s.Repository.ConfigRevision(context.Background(), "default")
	op := testOperation()
	body := object(op.Body)
	body["expected_config_revision"] = encode(revision)
	op.Body = encode(body)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Execute(context.Background(), op); results <- err }()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, repo.ErrInUse) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}
func TestAdopterHookReceivesOneCommittedRevision(t *testing.T) {
	s := testService(t)
	writeSession(t, s, `{"profile_id":"default","goal":"gateway"}`)
	calls := 0
	t.Cleanup(SetAdoptOwnedWrite(func(ctx context.Context, r *repo.Repository, id string, before, after json.RawMessage) error {
		calls++
		var a, b struct {
			Revision int64 `json:"_revision"`
		}
		_ = json.Unmarshal(before, &a)
		_ = json.Unmarshal(after, &b)
		if r != s.Repository || id != "default" || b.Revision != a.Revision+1 || ctx.Err() != nil {
			t.Fatal("wrong adoption boundary")
		}
		return nil
	}))
	if _, err := s.Execute(context.Background(), testOperation()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("adoption calls", calls)
	}
}
func TestSharedCredentialsAreCollectedBeforeProjection(t *testing.T) {
	s := testService(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-registration-key")
	_, err := s.Repository.UpdateSystemTransaction(context.Background(), func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		sys := c.GetObject("system_config")
		sys.Set("retired_rule_proxy_tokens", []any{"synthetic-retired-key"})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repository.UpdateProfileTransaction(context.Background(), "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "synthetic-retired-key", "type", "select", "manual_nodes", []any{"n"})})
		c.Set("rule_configs", []any{jsonx.Obj("id", "match", "itemType", "rule", "rule_type", "MATCH", "policy", "synthetic-retired-key")})
		c.GetObject("mihomo").Set("custom_config", "dns:\n  nameserver: [https://192.0.2.53/dns-query]\nsecret: synthetic-config-key\n")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	op := testOperation()
	op.Write = false
	raw, err := s.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("synthetic-retired-key")) {
		t.Fatal("retired credential leaked")
	}
}
