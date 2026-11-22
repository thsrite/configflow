package repo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func o10LargeHistoryRepo(t *testing.T, deployments int) *repo.Repository {
	t.Helper()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	history := make([]string, 0, deployments)
	payload := strings.Repeat("x", 1024)
	for i := range deployments {
		history = append(history, fmt.Sprintf(`"d%d":{"id":"d%d","status":"success","config":%q}`, i, i, payload))
	}
	agentsJSON := `[{"id":"agent-oxi","name":"Oxi Main","host":"192.0.2.10","port":8080,"token":"synthetic-secret-token",` +
		`"service_type":"oxidns","profile_id":"default","enabled":true,` +
		`"dns_follow":{"upstream_tag":"forward_remote","upstream_index":1},` +
		`"rule_sync":{"settings":{"library":true,"rules":false}},` +
		`"deployments":{` + strings.Join(history, ",") + `},` +
		`"ipv6_verifications":{"v1":{"id":"v1","status":"ok","summary":` + fmt.Sprintf("%q", payload) + `}},` +
		`"latest_deployment":{"id":"d0","status":"success","config":` + fmt.Sprintf("%q", payload) + `}},` +
		`{"id":"agent-legacy","name":42,"host":"192.0.2.11","service_type":"oxidns","profile_id":"default","enabled":null}]`
	if err := r.UpdateSystem(context.Background(), func(_ context.Context, s *model.System) error {
		s.Backup.WebDAVURL = "https://dav.example.com/backup"
		s.Backup.WebDAVUsername = "synthetic-user"
		return s.SetJSON("agents", json.RawMessage(agentsJSON))
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func o10AvgAllocBytes(t *testing.T, read func() error) uint64 {
	t.Helper()
	if err := read(); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 20 {
		if err := read(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 20
}

func TestO10ConfigDefaultOmitsAgentsAndNarrowOxiDNSProjectionSkipsHistory(t *testing.T) {
	ctx := context.Background()
	small := o10LargeHistoryRepo(t, 1)
	large := o10LargeHistoryRepo(t, 500)

	// Default Config omits Agents completely.
	cfg, err := large.Config(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HasAgents || len(cfg.Agents) != 0 {
		t.Fatalf("Config unexpectedly included agents: has=%v len=%d", cfg.HasAgents, len(cfg.Agents))
	}
	rawCfg, err := model.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var cfgFields model.Fields
	if err := json.Unmarshal(rawCfg, &cfgFields); err != nil {
		t.Fatal(err)
	}
	if cfgFields.Has("agents") {
		t.Fatal("marshaled default Config must not include agents key")
	}

	// ConfigWithOxiDNSAgents includes only the 8 OxiDNS fields with exact presence/tokens.
	oxiCfg, err := large.ConfigWithOxiDNSAgents(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !oxiCfg.HasAgents || len(oxiCfg.Agents) != 2 {
		t.Fatalf("ConfigWithOxiDNSAgents agents = %d (has=%v)", len(oxiCfg.Agents), oxiCfg.HasAgents)
	}
	first, second := oxiCfg.Agents[0], oxiCfg.Agents[1]
	if first.ID != "agent-oxi" || first.Name != "Oxi Main" || first.Host != "192.0.2.10" ||
		first.ServiceType != "oxidns" || first.ProfileID != "default" ||
		first.Enabled == nil || !*first.Enabled ||
		first.DNSFollow == nil || first.DNSFollow.UpstreamTag != "forward_remote" ||
		first.RuleSync == nil || first.RuleSync.Settings.Library == nil || !*first.RuleSync.Settings.Library {
		t.Fatalf("projected OxiDNS agent lost required fields: %+v", first)
	}
	if first.Token != "" {
		t.Fatal("OxiDNS agent projection leaked token")
	}
	rawFirst, err := model.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"deployments", "ipv6_verifications", "latest_deployment", "token", "synthetic-secret-token"} {
		if bytes.Contains(rawFirst, []byte(forbidden)) {
			t.Fatalf("OxiDNS agent projection unexpectedly contained %q: %s", forbidden, rawFirst)
		}
	}
	// Second agent preserves numeric name, explicit null enabled, and omits absent dns_follow/rule_sync.
	// Stored values read leniently (O18 T1b): a numeric name is its text, an
	// explicit null switch is off.
	if nameRaw, present, err := model.EncodeMember(&second, "name"); err != nil || !present || string(nameRaw) != `"42"` {
		t.Fatalf("numeric name not read as text: present=%v raw=%s err=%v", present, nameRaw, err)
	}
	if enabledRaw, present, err := model.EncodeMember(&second, "enabled"); err != nil || !present || string(enabledRaw) != "false" {
		t.Fatalf("explicit null enabled not read as off: present=%v raw=%s err=%v", present, enabledRaw, err)
	}
	if _, present, err := model.EncodeMember(&second, "dns_follow"); err != nil || present {
		t.Fatalf("absent dns_follow must remain absent: present=%v err=%v", present, err)
	}

	// Caller owns returned copies.
	oxiCfg.Agents[0].Host = "198.51.100.99"
	again, err := large.ConfigWithOxiDNSAgents(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Agents[0].Host != "192.0.2.10" {
		t.Fatal("caller mutation to ConfigWithOxiDNSAgents leaked into repository")
	}

	// Allocation bounds: narrow reads must not scale with Agent history size.
	// The measurements exclude the test-only view guard's encodings.
	small.SetViewGuard(nil)
	large.SetViewGuard(nil)
	reads := []struct {
		name string
		read func(*repo.Repository) func() error
	}{
		{"Config", func(r *repo.Repository) func() error {
			return func() error { _, err := r.Config(ctx, "default", nil); return err }
		}},
		{"ConfigWithOxiDNSAgents", func(r *repo.Repository) func() error {
			return func() error { _, err := r.ConfigWithOxiDNSAgents(ctx, "default", nil); return err }
		}},
	}
	for _, tt := range reads {
		t.Run(tt.name+"/history", func(t *testing.T) {
			smallBytes := o10AvgAllocBytes(t, tt.read(small))
			largeBytes := o10AvgAllocBytes(t, tt.read(large))
			if largeBytes > smallBytes+64*1024 {
				t.Fatalf("%s allocates %d bytes with 500 deployments (%d with 1)", tt.name, largeBytes, smallBytes)
			}
		})
	}
	// Negative control: PublishConfig(..., true) copies full Agent histories.
	fullSmall := o10AvgAllocBytes(t, func() error { _, _, err := small.PublishConfig(ctx, "default", true); return err })
	fullLarge := o10AvgAllocBytes(t, func() error { _, _, err := large.PublishConfig(ctx, "default", true); return err })
	if fullLarge < fullSmall+512*1024 {
		t.Fatalf("control: PublishConfig(true) allocates %d bytes with 500 deployments (%d with 1); bound would not detect a full copy", fullLarge, fullSmall)
	}
}

func TestO10SaveSystemConfigAndBackupMergeAndShutdownSemantics(t *testing.T) {
	ctx := context.Background()
	r := o10LargeHistoryRepo(t, 2)

	// Concurrent sibling edit and Agent edit must both survive three-way merge.
	settings, err := r.SystemConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseSettings := *settings
	backup, err := r.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseBackup := *backup

	// External concurrent commit changes another setting, backup field and Agent.
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ServerDomain = "https://concurrent.example.com"
		concurrentPath := "/concurrent-backup/"
		s.Backup.WebDAVPath = &concurrentPath
		s.Agents[0].Name = "Concurrent Rename"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := settings.SetJSON("config_token", json.RawMessage(`"synthetic-new-token"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveSystemConfig(ctx, settings, &baseSettings); err != nil {
		t.Fatal(err)
	}
	backup.WebDAVUsername = "updated-user"
	if _, err := r.SaveBackup(ctx, backup, &baseBackup); err != nil {
		t.Fatal(err)
	}

	sys, err := r.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sys.SystemConfig.ConfigToken != "synthetic-new-token" || sys.SystemConfig.ServerDomain != "https://concurrent.example.com" {
		t.Fatalf("SystemConfig three-way merge lost field: %+v", sys.SystemConfig)
	}
	if sys.Backup.WebDAVUsername != "updated-user" || sys.Backup.WebDAVPath == nil || *sys.Backup.WebDAVPath != "/concurrent-backup/" {
		t.Fatalf("Backup three-way merge lost field: %+v", sys.Backup)
	}
	if len(sys.Agents) != 2 || sys.Agents[0].Name != "Concurrent Rename" {
		t.Fatalf("SaveSystemConfig/SaveBackup clobbered Agents: %+v", sys.Agents)
	}

	// HR 4th standing item: after Runtime shutdown no write lands; after request
	// disconnect every write still completes.
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			rt.Start(ctx)
			defer rt.Shutdown(ctx)
			reqCtx, disconnect := context.WithCancel(rt.BindRequest(repo.WithRepository(ctx, r)))
			defer disconnect()

			cur, err := r.SystemConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			base := *cur
			wantDomain := "https://" + name + ".example.com"
			if err := cur.SetJSON("server_domain", json.RawMessage(`"`+wantDomain+`"`)); err != nil {
				t.Fatal(err)
			}
			beforeHead, err := r.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				rt.Shutdown(ctx)
			} else {
				disconnect()
			}
			_, err = r.SaveSystemConfig(reqCtx, cur, &base)
			afterHead, headErr := r.Head(ctx)
			if headErr != nil {
				t.Fatal(headErr)
			}
			if shutdown {
				if err == nil {
					t.Fatal("SaveSystemConfig committed after Runtime shutdown")
				}
				if afterHead != beforeHead {
					t.Fatal("repository head advanced after Runtime shutdown")
				}
			} else {
				if err != nil {
					t.Fatalf("SaveSystemConfig aborted on client disconnect: %v", err)
				}
				got, err := r.SystemConfig(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if got.ServerDomain != wantDomain {
					t.Fatalf("disconnected write did not persist: got %q want %q", got.ServerDomain, wantDomain)
				}
			}
		})
	}
}
