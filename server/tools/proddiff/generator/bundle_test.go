package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/server"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func TestBundleMetadataChecksRemainStrictForStructuredProviders(t *testing.T) {
	data := []byte("proxies: []\n")
	sum := sha256.Sum256(data)
	file := deploy.File{Path: "providers/a.yaml", Role: deploy.RoleProvider, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	if err := verifyBundleFile(file, data); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"bytes", "size", "hash"} {
		t.Run(change, func(t *testing.T) {
			bad, body := file, append([]byte(nil), data...)
			switch change {
			case "bytes":
				body[0] = 'X'
			case "size":
				bad.Size++
			case "hash":
				bad.SHA256 = "wrong"
			}
			if verifyBundleFile(bad, body) == nil {
				t.Fatal("corrupt manifest/file accepted")
			}
		})
	}
}

func TestDeploymentBundlePassesRepoAndRefresh(t *testing.T) {
	ctx := context.Background()
	repository, err := repo.OpenDefaultWithOptions(t.TempDir(), server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}

	const defaultID = "custom-default"
	if _, err := repository.CreateProfileRecord(ctx, repo.ProfileCreation{ID: defaultID, Name: "Custom Default"}, ""); err != nil {
		t.Fatal(err)
	}
	const ruleURL = "https://rules.example.com/domain.txt"
	ruleItem := model.RuleLibraryItem{
		ID:         "lib-rule-1",
		Name:       "Synthetic Rule",
		SourceType: new("url"),
		URL:        new(ruleURL),
		Behavior:   new("domain"),
		Format:     new("text"),
	}
	if err := repository.UpdateShared(ctx, func(_ context.Context, sh *model.Shared) error {
		sh.RuleLibrary = append(sh.RuleLibrary, ruleItem)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rulefetch.WriteCache(ctx, repository, model.RuleConfig{
		ID:         ruleItem.ID,
		Name:       ruleItem.Name,
		SourceType: ruleItem.SourceType,
		URL:        ruleItem.URL,
		Behavior:   ruleItem.Behavior,
		Format:     ruleItem.Format,
	}, rulefetch.Content{Data: []byte("example.com\n"), Text: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateProfile(ctx, defaultID, func(_ context.Context, p *model.Profile) error {
		p.RuleConfigs = append(p.RuleConfigs, model.RuleConfig{
			ID:            "rule-cfg-1",
			ItemType:      "ruleset",
			LibraryRuleID: ruleItem.ID,
			Name:          "Synthetic Rule",
			Enabled:       new(true),
			Policy:        new("DIRECT"),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.DefaultProfileID = new(defaultID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dbPath := repository.DatabasePath()
	dataDir := repository.DataDir()
	repository.Close()

	// Stored legacy/snapshot Agent rows can omit profile_id; write one directly
	// so deploymentBundle must resolve the default profile through BuildInput.Repo.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE fields SET value = '[{"id":"agent-no-profile","name":"Agent Without Profile","service_type":"mihomo","enabled":true}]' WHERE scope = 'system' AND field = 'agents'`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = repo.OpenDefaultWithOptions(dataDir, server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repository.Close)

	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	srv, err := server.New(ctx, server.Options{Repo: repository})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)

	cfg, err := repository.Config(ctx, defaultID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawView, err := deploymentBundle(ctx, srv, repository, cfg, job{
		Kind:      "agent-deployment-bundle",
		ProfileID: defaultID,
		AgentID:   "agent-no-profile",
	}, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true, ForAgent: true})
	if err != nil {
		t.Fatalf("deploymentBundle (cached remote rule) failed: %v", err)
	}
	var view struct {
		ServiceType string `json:"service_type"`
		Files       []struct {
			Path    string `json:"path"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(rawView), &view); err != nil {
		t.Fatal(err)
	}
	if view.ServiceType != "mihomo" {
		t.Fatalf("service_type = %q, want mihomo", view.ServiceType)
	}
	foundRule := false
	for _, f := range view.Files {
		if f.Role != string(deploy.RoleRule) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(decoded), "example.com") {
			foundRule = true
		}
	}
	if !foundRule {
		t.Fatalf("expected cached remote rule file in bundle, got %+v", view.Files)
	}

	// Target and snapshot both omit profile_id: deploymentBundle must pass Repo so
	// deploy.Build resolves the repository default profile instead of failing closed.
	noProfileCopy := *cfg
	noProfileProfile := *cfg.Profile
	noProfileProfile.ID = ""
	noProfileProfile.RuleConfigs = nil
	noProfileCopy.Profile = &noProfileProfile
	noProfileCopy.ProfileID = ""
	if _, err := deploymentBundle(ctx, srv, repository, &noProfileCopy, job{
		Kind:      "agent-deployment-bundle",
		ProfileID: defaultID,
		AgentID:   "agent-no-profile",
	}, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true, ForAgent: true}); err != nil {
		t.Fatalf("deploymentBundle (default profile fallback) failed: %v", err)
	}

	// Negative control: calling deploy.Build with a declared remote rule and nil Refresh fails closed.
	src, err := deploySource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deploy.Build(ctx, deploy.BuildInput{
		Repo:          repository,
		Target:        deploy.Target{ID: "agent-no-profile", ServiceType: "mihomo"},
		Source:        src,
		ConfigContent: "mode: rule\nrule-providers:\n  synthetic:\n    type: http\n    behavior: domain\n    format: text\n    url: " + ruleURL + "\n    path: ./rules/synthetic.txt\n",
		BaseURL:       baseURL,
		DeploymentID:  "no-refresh-fail-closed",
	})
	if err == nil || !strings.Contains(err.Error(), "refresh: repository is nil") {
		t.Fatalf("expected nil Refresh to fail closed, got: %v", err)
	}
}

func TestGeneratorUsesAppTrafficStore(t *testing.T) {
	ctx := t.Context()
	repository, err := repo.OpenDefaultWithOptions(t.TempDir(), server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repository.Close)

	agentJob := job{Kind: "agent-mihomo", ProfileID: "default", AgentID: "synthetic-agent"}
	if err := repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = append(s.Agents, model.Agent{
			ID: agentJob.AgentID, Name: "Synthetic Agent", ServiceType: "mihomo",
			ProfileID: agentJob.ProfileID, DomainDiscoveryEnabled: new(true),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A cached subscription host mapping produces a DNS policy only when the
	// reported core supports it (or its version is unknown).
	const subID, subURL, nodeHost = "synthetic-source", "https://feed.example.com/config.yaml", "node.example.com"
	if err := repository.UpdateShared(ctx, func(_ context.Context, sh *model.Shared) error {
		sh.Subscriptions = append(sh.Subscriptions, model.Subscription{ID: subID, Name: "Synthetic Source", URL: subURL})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateProfile(ctx, agentJob.ProfileID, func(_ context.Context, p *model.Profile) error {
		p.ProxyGroups = append(p.ProxyGroups, model.ProxyGroup{ID: "synthetic-group", Name: "Synthetic Group", Type: "select", Subscriptions: new([]string{subID})})
		p.Mihomo.CustomConfig = "mode: rule\ndns:\n  enable: true\n  listen: '127.0.0.1:5353'\n  proxy-server-nameserver: ['https://dns.example.com/dns-query']\n"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	srv, err := server.New(ctx, server.Options{Repo: repository})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)

	nodes := []model.Node{{Name: "Synthetic Node", Type: "http", Server: nodeHost, Port: model.IntPtr(8080)}}
	hosts := subs.ExtractHosts("hosts: {node.example.com: upstream.example.com}\n")
	if _, err := srv.SubStore.SaveCache(ctx, repository, subID, nodes, &substore.CacheMetadata{URL: new(subURL), Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	// Keep the reported versions outside the repository's default traffic path.
	srv.Traffic = trafficstore.NewService(t.TempDir())
	for _, tc := range []struct {
		version string
		policy  *yamlx.Node
	}{
		{version: "v1.19.10"},
		{version: "v1.19.20", policy: yamlx.Str("udp://127.0.0.1:5353")},
	} {
		t.Run(tc.version, func(t *testing.T) {
			report, err := trafficstore.DecodeReport([]byte(fmt.Sprintf(`{"schema":1,"status":"running","mihomo_version":%q,"window_start":"2026-10-10T10:00:00Z","window_end":"2026-10-10T10:05:00Z","ip_only_conns":0,"dropped":0,"items":[]}`, tc.version)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := srv.Traffic.Store().Add(ctx, agentJob.AgentID, report, nil); err != nil {
				t.Fatal(err)
			}
			generated, err := generate(ctx, srv, repository, agentJob)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := yamlx.Parse([]byte(generated))
			if err != nil {
				t.Fatal(err)
			}
			policy := yamlx.Get(yamlx.Get(doc, "dns"), subs.PolicyKey)
			if got := yamlx.Get(policy, nodeHost); !yamlx.Equal(got, tc.policy) {
				t.Fatalf("generated DNS policy for reported core %s: got %v, want %v", tc.version, got, tc.policy)
			}
		})
	}
}
