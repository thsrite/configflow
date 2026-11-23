package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	agents.ResetHooks()
	t.Cleanup(agents.ResetHooks)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, SecretKey: "c32-synthetic-secret", Register: httpxtest.Routes(Register)})
	ctx := s.Context()
	if _, err := s.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	err := s.Repo.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
		return p.SetJSON("rule_library", json.RawMessage(`[{"id":"ads","name":"Ads","behavior":"domain","source_type":"content","content":"+.ads.example\n","enabled":true}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
		if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"proxy","name":"PROXY","type":"select"}]`)); err != nil {
			return err
		}
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"rule_1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","enabled":true,"future":{"keep":true}},{"id":"set","itemType":"ruleset","library_rule_id":"ads","policy":"DIRECT","enabled":true},{"id":"last","itemType":"rule","rule_type":"MATCH","value":"","policy":"DIRECT","enabled":true}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Repo.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
		return p.SystemConfig.SetJSON("server_domain", json.RawMessage(`"http://panel.example"`))
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestCoreRegistrationAndProfileIsolation(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	s.Get("/api/profiles/other/rules", auth).WantStatus(200)
	s.Post("/api/profiles/other/rules", auth, httpxtest.JSON(map[string]any{"id": "new", "rule_type": "DOMAIN-SUFFIX", "value": "new.example", "policy": "DIRECT"})).WantStatus(200)
	s.Put("/api/rules/new?profile=other", auth, httpxtest.JSON(map[string]any{"remark": "new label"})).WantStatus(200)
	s.Post("/api/rules/group-name?profile=other", auth, httpxtest.JSON(map[string]any{"ids": []string{"rule_1", "new"}, "group_name": "  Group  "})).WantStatus(200)
	s.Put("/api/rules/policy?profile=other", auth, httpxtest.JSON(map[string]any{"ids": []string{"new"}, "policy": "PROXY", "group_name": ""})).WantStatus(200)
	s.Post("/api/rules/batch?profile=other", auth, httpxtest.JSON(map[string]any{"domains": []string{"a.example", "b.example"}, "rule_type": "DOMAIN", "policy": "DIRECT"})).WantStatus(200)
	s.Post("/api/rules/reorder?profile=other", auth, httpxtest.JSON(map[string]any{"ids": []string{"new"}, "position": "bottom"})).WantStatus(200)
	s.Get("/api/rules/dns?profile=other", auth).WantStatus(200)
	s.Put("/api/rules/dns?profile=other", auth, httpxtest.JSON(map[string]any{"routes": map[string]string{"rule_1": "direct"}})).WantStatus(200)
	s.Delete("/api/rules/new?profile=other", auth).WantStatus(200)
	p, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.RuleConfigs) != 0 {
		t.Fatal("default profile changed")
	}
	p, err = s.Repo.Profile(s.Context(), "other")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range p.RuleConfigs {
		if entry.ID == "rule_1" {
			raw, _ := json.Marshal(entry)
			parsed, _ := testjson.Parse(raw)
			// Undeclared rule members are dropped (O18 T1c).
			if _, kept := testjson.Path(parsed, "future", "keep"); kept {
				t.Fatal("undeclared rule member kept")
			}
		}
	}
}
func TestPinnedWithheldHookIsBestEffort(t *testing.T) {
	for _, name := range []string{"no-device", "missing", "error", "true", "false"} {
		t.Run(name, func(t *testing.T) {
			s := fixture(t)
			ctx := s.Context()
			if name != "no-device" {
				err := s.Repo.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
					return p.SetJSON("agents", json.RawMessage(`[{"id":"dns","name":"DNS","profile_id":"other","service_type":"oxidns","enabled":true,"rule_sync":{"settings":{"library":true,"rules":false}}}]`))
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			var log bytes.Buffer
			root, err := logx.Setup(logx.Config{File: filepath.Join(t.TempDir(), "log"), Console: &log, Level: logx.Warning})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { root.Close() })
			calls := 0
			if name == "error" || name == "true" || name == "false" {
				agents.RegisterHooks(agents.Hooks{PinnedWithheld: func(_ context.Context, id string, ids []string) (bool, error) {
					calls++
					if id != "other" || len(ids) != 1 || ids[0] != "rule_1" {
						t.Errorf("incorrect hook scope %s %v", id, ids)
					}
					if name == "error" {
						return false, errors.New("secret-rule-content https://user:secret-password@source.example/path?token=secret-token")
					}
					return name == "true", nil
				}})
			}
			out, err := (&Service{}).SetPolicy(repo.WithScope(repo.WithRepository(ctx, s.Repo)), "other", request[policyRequest](record{"ids": json.RawMessage(`["rule_1"]`), "policy": stringJSON("PROXY")}), "http://localhost")
			if err != nil {
				t.Fatal(err)
			}
			expectedPresent := name == "no-device" || name == "true" || name == "false"
			if (out.PinnedWithheld != nil) != expectedPresent {
				t.Fatalf("field presence %v", out.PinnedWithheld)
			}
			if out.PinnedWithheld != nil && *out.PinnedWithheld != (name == "true") {
				t.Fatal("wrong field value")
			}
			if (name == "true" || name == "false" || name == "error") && calls != 1 {
				t.Fatal("hook not called")
			}
			p, err := s.Repo.Profile(ctx, "other")
			if err != nil {
				t.Fatal(err)
			}
			if p.RuleConfigs[0].Policy == nil || *p.RuleConfigs[0].Policy != "PROXY" {
				t.Fatal("save rolled back")
			}
			text := log.String()
			if strings.Contains(text, "secret-") || strings.Contains(text, "source.example") {
				t.Fatal("warning leaked content or credentials")
			}
			count := strings.Count(text, "Checking withheld pinned upstreams failed")
			if name == "missing" || name == "error" {
				if count != 1 {
					t.Fatalf("warning count %d: %s", count, text)
				}
			} else if count != 0 {
				t.Fatal("unexpected warning")
			}
		})
	}
}
