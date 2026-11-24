//go:build configflow_difftest

package rulefetch_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/api/rulelibrary"
	apirules "configflow/internal/api/rules"
	"configflow/internal/discovery"
	"configflow/internal/faultprobe"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

// Both writers and the recovery reader are fresh processes. SIGKILL prevents
// defers, repository cleanup, or an orderly shutdown from repairing the gap.
func TestInlinePublicationCrashRecovery(t *testing.T) {
	for _, scenario := range []string{"adopt", "adopt-shared", "undo", "agent-ip"} {
		for _, crash := range []bool{false, true} {
			name := scenario + "/complete"
			if crash {
				name = scenario + "/sigkill"
			}
			t.Run(name, func(t *testing.T) {
				dir, control := t.TempDir(), t.TempDir()
				if err := os.Chmod(control, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(control, "token"), bytes.Repeat([]byte("x"), 32), 0600); err != nil {
					t.Fatal(err)
				}
				if crash {
					if err := os.WriteFile(filepath.Join(control, "pause."+string(faultprobe.RuleDBCommitted)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				writer := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInlinePublicationProcess$")
				writer.Env = append(os.Environ(), "C37_DATA="+dir, "C37_SCENARIO="+scenario, "C37_MODE=write", faultprobe.ControlEnv+"="+control)
				var output bytes.Buffer
				writer.Stdout, writer.Stderr = &output, &output
				if err := writer.Start(); err != nil {
					t.Fatal(err)
				}
				if crash {
					deadline := time.Now().Add(15 * time.Second)
					reached := false
					for time.Now().Before(deadline) {
						events, _ := os.ReadFile(filepath.Join(control, "events.jsonl"))
						if bytes.Contains(events, []byte(`"phase":"rule-db-committed","event":"entered"`)) {
							reached = true
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					if err := writer.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					err := writer.Wait()
					var exit *exec.ExitError
					if !reached || !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
						t.Fatalf("did not SIGKILL at committed boundary: reached=%v err=%v\n%s", reached, err, output.String())
					}
				} else if err := writer.Wait(); err != nil {
					t.Fatalf("normal writer: %v\n%s", err, output.String())
				}
				reader := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInlinePublicationProcess$")
				mode := "read-complete"
				if crash {
					mode = "read-crashed"
				}
				reader.Env = append(os.Environ(), "C37_DATA="+dir, "C37_SCENARIO="+scenario, "C37_MODE="+mode, faultprobe.ControlEnv+"=")
				if output, err := reader.CombinedOutput(); err != nil {
					t.Fatalf("restarted reader: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestInlinePublicationProcess(t *testing.T) {
	mode, scenario := os.Getenv("C37_MODE"), os.Getenv("C37_SCENARIO")
	if mode == "" {
		return
	}
	s := httpxtest.New(t, httpxtest.Options{DataDir: os.Getenv("C37_DATA"), Auth: true})
	m, err := agents.New(s.Context(), s.Repo, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: s.Repo, Agents: m}).Register(s.App.Router)
	rulelibrary.New(rulelibrary.Deps{Repo: s.Repo}).Register(s.App.Router)
	apirules.New(apirules.Deps{Repo: s.Repo}).Register(s.App.Router)
	if mode == "write" {
		seedInlinePublication(t, s)
		if scenario == "adopt-shared" {
			_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
				id := "domains"
				p.DomainDiscovery.ProxyRuleset = &id
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		disc := discovery.New(discovery.Options{Repo: s.Repo})
		switch scenario {
		case "adopt", "adopt-shared":
			_, err := disc.ApplyDomains(s.Context(), "default", []discovery.ApplyEntry{{Value: "new.example", RuleType: "DOMAIN-SUFFIX", Target: "direct"}, {Value: "proxy.example", RuleType: "DOMAIN-SUFFIX", Target: "proxy"}}, "manual", nil)
			if err != nil {
				t.Fatal(err)
			}
		case "undo":
			if _, err := disc.UndoDomain(s.Context(), "default", "remove.example", "direct"); err != nil {
				t.Fatal(err)
			}
		case "agent-ip":
			s.Post("/api/agents/client/mihomo/rule-targets/apply", s.Login(), httpxtest.JSON(map[string]string{"profile_id": "default", "ruleset_id": "ips", "target": "direct", "value": "198.51.100.8", "rule_type": "IP-CIDR"})).WantStatus(200)
		default:
			t.Fatal("unknown scenario", scenario)
		}
		return
	}
	catalog, err := s.Repo.Shared(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	expected := inlineSeedContent()
	switch scenario {
	case "adopt":
		expected["domains"] += "+.new.example\n"
		expected["proxy"] += "+.proxy.example\n"
	case "adopt-shared":
		expected["domains"] += "+.new.example\n+.proxy.example\n"
	case "undo":
		expected["domains"] = "+.old.example\n"
	case "agent-ip":
		expected["ips"] += "IP-CIDR,198.51.100.8/32,no-resolve\n"
	}
	ids := []string{}
	for _, item := range catalog.RuleLibrary {
		want, relevant := expected[item.ID]
		if !relevant {
			continue
		}
		if item.Content == nil || *item.Content != want {
			t.Fatalf("committed %s: %v want %q", item.ID, item.Content, want)
		}
		rule := rules.LibraryRule(item)
		root, err := rulefetch.GetRulesDir(s.Repo)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, rules.CacheFilename(rule))
		cache, err := rulefetch.ReadCache(s.Context(), s.Repo, rule, nil)
		changed := want != inlineSeedContent()[item.ID]
		if mode == "read-crashed" && changed {
			// Negative control: the old valid file still exists, so receipt bypass
			// would expose its stale bytes and fail both this assertion and HTTP.
			raw, readErr := os.ReadFile(path)
			if readErr != nil || string(raw) != inlineSeedContent()[item.ID] {
				t.Fatalf("old cache control: %q %v", raw, readErr)
			}
			if cache != nil || err != nil {
				t.Fatalf("stale cache was admitted: %q %v", cache, err)
			}
			if _, err := os.Lstat(path + ".invalid"); err != nil {
				t.Fatal("missing durable receipt", err)
			}
		} else if err != nil || string(cache) != want {
			t.Fatalf("normal cache %s: %q %v", item.ID, cache, err)
		}
		for _, route := range []string{"/api/rule-library/content/" + item.ID, "/api/profiles/default/rule-library/content/" + item.ID, "/api/rules/local/" + item.Name} {
			response := s.Get(route, s.Login()).WantStatus(200)
			if response.Text() != want {
				t.Fatalf("restart HTTP %s: %q want %q", route, response.Text(), want)
			}
			if got := s.Do("HEAD", route, s.Login()).WantStatus(200); len(got.Body) != 0 {
				t.Fatalf("HEAD body: %q", got.Body)
			}
		}
		ids = append(ids, item.ID)
	}
	if len(ids) != 3 {
		t.Fatalf("missing fixture declarations: %v", ids)
	}
	// Recovery reuses the same C33 publisher, including its receipt/temp cleanup.
	s.Post("/api/rule-library/cache", s.Login(), httpxtest.JSON(map[string]any{"rule_ids": ids})).WantStatus(200)
	root, err := rulefetch.GetRulesDir(s.Repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.RuleLibrary {
		want, relevant := expected[item.ID]
		if !relevant {
			continue
		}
		rule := rules.LibraryRule(item)
		cache, err := rulefetch.ReadCache(s.Context(), s.Repo, rule, nil)
		if err != nil || string(cache) != want {
			t.Fatalf("repaired cache: %q %v", cache, err)
		}
		if _, err := os.Lstat(filepath.Join(root, rules.CacheFilename(rule)) + ".invalid"); !os.IsNotExist(err) {
			t.Fatal("repair retained receipt", err)
		}
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".tmp") {
			t.Errorf("retained private stage: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func inlineSeedContent() map[string]string {
	return map[string]string{"domains": "+.old.example\n+.remove.example\n", "proxy": "+.existing.example\n", "ips": "IP-CIDR,192.0.2.0/24,no-resolve\n"}
}

func seedInlinePublication(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	text := func(s string) *string { return &s }
	enabled := true
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, shared *model.Shared) error {
		for _, id := range []string{"domains", "proxy", "ips"} {
			behavior := "domain"
			if id == "ips" {
				behavior = "classical"
			}
			shared.RuleLibrary = append(shared.RuleLibrary, model.RuleLibraryItem{ID: id, Name: id, Enabled: &enabled, SourceType: text("content"), Behavior: text(behavior), Format: text("text"), Content: text(inlineSeedContent()[id])})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
		p.DomainDiscovery.DirectRuleset, p.DomainDiscovery.ProxyRuleset = text("domains"), text("proxy")
		for _, id := range []string{"domains", "proxy", "ips"} {
			policy := "DIRECT"
			if id == "proxy" {
				policy = "PROXY"
			}
			p.RuleConfigs = append(p.RuleConfigs, model.RuleConfig{ID: "ref-" + id, ItemType: "ruleset", LibraryRuleID: id, Policy: text(policy), Enabled: &enabled})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.Agents = append(system.Agents, model.Agent{ID: "client", Name: "client", ProfileID: "default", ServiceType: "mihomo", Enabled: &enabled})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := s.Repo.Shared(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.RuleLibrary {
		if _, err := rulefetch.SaveSource(s.Context(), s.Repo, rules.LibraryRule(item), nil); err != nil {
			t.Fatal(err)
		}
	}
}
