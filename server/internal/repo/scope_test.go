package repo

// Port of backend/test_request_config_snapshots.py: per-request config
// reads validate a profile once and copy no whole document. A Flask request
// context is a Scope with SetRequest; the two cases that run a Flask app
// replay what install_profile_context's hooks do (the hooks themselves
// belong to package httpx). sanitize_external_payload belongs to the export
// package: its three cases assert the token sets it reads.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/model"
)

func scRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "other", Name: "Other"}, ""))(t)
	return r, WithRepository(WithScope(bg), r)
}

// scRequest is app.test_request_context(path, headers=...): a new Scope for
// a request without a matched route (endpoint None).
func scRequest(r *Repository, header string) context.Context {
	ctx := WithRepository(WithScope(bg), r)
	info := RequestInfo{}
	if header != "" {
		info.HeaderProfile = &header
	}
	SetRequest(ctx, info)
	return ctx
}

func scAppendAgent(t *testing.T, r *Repository, agent string) {
	t.Helper()
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		var record model.Agent
		if err := json.Unmarshal([]byte(agent), &record); err != nil {
			return err
		}
		system.Agents = append(system.Agents, record)
		return nil
	}))
}

func TestReadsAndSavesNeverModifyTheCommittedDocument(t *testing.T) {
	r, ctx := scRepository(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		if err := shared.SetJSON("nodes", []byte(`[{"id": "n", "name": "Node"}]`)); err != nil {
			return err
		}
		return nil
	}))
	storeSharedRows(t, r, map[string]string{"rule_library": `[{"id": "r", "name": "Rule", "enabled": {"custom": []}}]`})
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["n"]}],
		"rule_configs": [{"id": "rule", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	entry := must(r.readEntry(bg, false))(t)
	source := entry.Model
	before := mustMarshalJSON(t, source)

	// Config borrows the snapshot: editors change a decoded copy.
	config := must(Config(ctx, "default"))(t)
	baseline := mustMarshalJSON(t, config)
	edited := must(model.DecodeConfig(baseline))(t)
	edited.Profile.ProxyGroups[0].Name = "Changed"
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", mustMarshalJSON(t, edited), baseline)
	noError(t, err)

	shared := must(SharedConfig(ctx))(t)
	shared.Nodes[0].Name = "Changed"
	noError(t, SaveSharedRecord(ctx, shared, nil))

	system := must(r.System(bg))(t)
	system.SystemConfig.ServerDomain = "https://changed.example"
	must(r.SaveSystemRecord(bg, system, nil))(t)

	if must(r.readEntry(bg, false))(t).Model == source {
		t.Fatal("the committed generation was not replaced")
	}
	wantJSONEqual(t, mustMarshalJSON(t, source), before)
	if got := must(r.Profile(bg, "default"))(t).ProxyGroups[0].Name; got != "Changed" {
		t.Fatalf("profile group name %q", got)
	}
	if got := must(r.Shared(bg))(t).Nodes[0].Name; got != "Changed" {
		t.Fatalf("shared node name %q", got)
	}
}

func TestProfileBaselineIsTheReadViewAndKeepsConcurrentEdits(t *testing.T) {
	r, ctx := scRepository(t)
	config := must(Config(ctx, "default"))(t)
	readView := mustMarshalJSON(t, config)
	config.Profile.Mihomo.CustomConfig = "mixed-port: 7901"
	groupJSON := json.RawMessage(`{"id": "g", "name": "Concurrent", "type": "select"}`)
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{"proxy_groups":[`+string(groupJSON)+`]}`), nil)
	noError(t, err)
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", mustMarshalJSON(t, config), readView)
	noError(t, err)
	saved := must(r.Profile(bg, "default"))(t)
	if saved.Mihomo.CustomConfig != "mixed-port: 7901" {
		t.Fatalf("custom_config %q", saved.Mihomo.CustomConfig)
	}
	wantJSONEqual(t, saved.ProxyGroups, json.RawMessage(`[`+string(groupJSON)+`]`))
}

func TestSharedBaselineKeepsConcurrentEdits(t *testing.T) {
	r, ctx := scRepository(t)
	shared := must(SharedConfig(ctx))(t)
	noError(t, shared.SetJSON("nodes", []byte(`[{"id": "mine", "name": "Mine"}]`)))
	noError(t, r.UpdateShared(bg, func(_ context.Context, current *model.Shared) error {
		return current.SetJSON("subscriptions", []byte(`[{"id": "s", "name": "Concurrent", "url": "https://example.test/s"}]`))
	}))
	noError(t, SaveSharedRecord(ctx, shared, nil))
	saved := must(r.Shared(bg))(t)
	wantJSONEqual(t, saved.Nodes, json.RawMessage(`[{"id": "mine", "name": "Mine"}]`))
	if len(saved.Subscriptions) != 1 || saved.Subscriptions[0].ID != "s" {
		t.Fatalf("subscriptions %+v", saved.Subscriptions)
	}
}

func TestTransactionReadsKeepAnEagerSharedBaseline(t *testing.T) {
	r, ctx := scRepository(t)
	var viewRaw []byte
	var captured model.Fields
	noError(t, r.Update(ctx, func(ctx context.Context, d *model.Document) error {
		view := must(SharedConfig(ctx))(t)
		viewRaw = mustMarshalJSON(t, view)
		captured = ScopeOf(ctx).recordBaselines[sharedBaselineKey]
		d.Shared.Nodes = append(d.Shared.Nodes, model.Node{ID: "n1", Name: "Changed in transaction"})
		return nil
	}))
	capturedRaw, err := captured.MarshalJSON()
	noError(t, err)
	wantJSONEqual(t, capturedRaw, viewRaw)
}

func TestRequestReusesTheValidatedProfile(t *testing.T) {
	r, _ := scRepository(t)
	start := r.metadataReads.Load()
	ctx := scRequest(r, "other")
	ResetConfigContext(ctx)
	if got := must(ResolveProfileID(ctx, ""))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	config := must(Config(ctx, ""))(t)
	if config.ProfileID != "other" {
		t.Fatalf("profile_id %q", config.ProfileID)
	}
	if must(Config(ctx, "other"))(t) != config {
		t.Fatal("the request's snapshot was not reused")
	}
	if got := must(ResolveProfileID(ctx, "other"))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	if got := must(ResolveProfileID(ctx, "default"))(t); got != "default" {
		t.Fatalf("got %q", got)
	}
	if got := must(ResolveProfileID(ctx, "default"))(t); got != "default" {
		t.Fatalf("got %q", got)
	}
	if calls := r.metadataReads.Load() - start; calls != 2 {
		t.Fatalf("profile validated %d times, want 2 (other, default)", calls)
	}
}

func TestDeletingAProfileInTheSameRequestIsNoticed(t *testing.T) {
	r, _ := scRepository(t)
	ctx := scRequest(r, "other")
	ResetConfigContext(ctx)
	if got := must(ResolveProfileID(ctx, ""))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	if got := must(ResolveProfileID(ctx, "other"))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	must(Config(ctx, ""))(t)
	noError(t, r.DeleteProfile(bg, "other", ""))
	_, err := ResolveProfileID(ctx, "")
	wantError(t, err, ErrNotFound, "")
	_, err = ResolveProfileID(ctx, "other")
	wantError(t, err, ErrNotFound, "")
	_, err = Config(ctx, "")
	wantError(t, err, ErrNotFound, "")
}

func TestSwitchingTheDefaultInTheSameRequestIsNoticed(t *testing.T) {
	r, _ := scRepository(t)
	ctx := scRequest(r, "")
	ResetConfigContext(ctx)
	if got := must(ResolveProfileID(ctx, ""))(t); got != "default" {
		t.Fatalf("got %q", got)
	}
	must(r.SetDefaultProfile(bg, "other"))(t)
	if got := must(ResolveProfileID(ctx, ""))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
}

func TestAnotherRequestDoesNotReuseTheValidatedProfile(t *testing.T) {
	r, _ := scRepository(t)
	outer := scRequest(r, "other")
	if got := must(ResolveProfileID(outer, ""))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	if got := must(ResolveProfileID(scRequest(r, ""), ""))(t); got != "default" {
		t.Fatalf("got %q", got)
	}
	if got := must(ResolveProfileID(outer, ""))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
}

// scHandle replays install_profile_context around a handler: reset, resolve
// the profile unless the request is global, run, reset.
func scHandle(t *testing.T, r *Repository, route RequestRoute, header string, handler func(ctx context.Context)) (profileHeader string) {
	t.Helper()
	ctx := WithRepository(WithScope(bg), r)
	info := RequestInfo{Endpoint: route.Endpoint, Global: IsGlobalRequest(route)}
	if header != "" {
		info.HeaderProfile = &header
	}
	SetRequest(ctx, info)
	ResetConfigContext(ctx)
	if !info.Global {
		profileHeader = must(ResolveProfileID(ctx, ""))(t)
	}
	handler(ctx)
	ResetConfigContext(ctx)
	return profileHeader
}

func TestFullRequestValidatesTheProfileOnce(t *testing.T) {
	r, _ := scRepository(t)
	start := r.metadataReads.Load()
	route := RequestRoute{Endpoint: "rules.list_rules", Blueprint: "rules", Path: "/api/rules"}
	header := scHandle(t, r, route, "other", func(ctx context.Context) {
		config := must(Config(ctx, ""))(t)
		if must(Config(ctx, must(ResolveProfileID(ctx, ""))(t)))(t) != config {
			t.Fatal("the request's snapshot was not reused")
		}
		if config.ProfileID != "other" {
			t.Fatalf("profile_id %q", config.ProfileID)
		}
	})
	if header != "other" {
		t.Fatalf("X-ConfigFlow-Profile %q", header)
	}
	if calls := r.metadataReads.Load() - start; calls != 1 {
		t.Fatalf("profile validated %d times", calls)
	}
}

func TestGlobalRequestFollowsADefaultSwitch(t *testing.T) {
	r, _ := scRepository(t)
	route := RequestRoute{Endpoint: "settings.switch_default", Blueprint: "settings", Path: "/api/settings/default-profile"}
	header := scHandle(t, r, route, "", func(ctx context.Context) {
		if got := must(ResolveProfileID(ctx, ""))(t); got != "default" {
			t.Fatalf("got %q", got)
		}
		must(r.SetDefaultProfile(ctx, "other"))(t)
		if got := must(ResolveProfileID(ctx, ""))(t); got != "other" {
			t.Fatalf("got %q", got)
		}
	})
	if header != "" {
		t.Fatalf("global request got profile header %q", header)
	}
}

func TestIsGlobalRequest(t *testing.T) {
	for _, item := range []struct {
		route RequestRoute
		want  bool
	}{
		{RequestRoute{Endpoint: "rules.list_rules", Blueprint: "rules", Path: "/api/rules"}, false},
		{RequestRoute{Endpoint: "nodes.list", Blueprint: "nodes", Path: "/api/nodes"}, true},
		{RequestRoute{Endpoint: "profiles.x", Blueprint: "profiles", Path: "/api/profiles/a/x", HasPathProfileID: true}, false},
		{RequestRoute{Endpoint: "subscriptions.get_subscription_proxies", Blueprint: "subscriptions", Path: "/api/x"}, false},
		{RequestRoute{Endpoint: "config.export_config", Blueprint: "config", Path: "/api/config/export"}, true},
		{RequestRoute{Endpoint: "config.other", Blueprint: "config", Path: "/api/config/other"}, false},
		{RequestRoute{Endpoint: "static", Blueprint: "", Path: "/index.html"}, true},
		{RequestRoute{Endpoint: "", Blueprint: "", Path: "/api/missing"}, true},
	} {
		if got := IsGlobalRequest(item.route); got != item.want {
			t.Errorf("%+v: %v", item.route, got)
		}
	}
}

func TestTransactionWorkingCopyIsValidatedWithoutTheMemo(t *testing.T) {
	r, _ := scRepository(t)
	ctx := scRequest(r, "other")
	ResetConfigContext(ctx)
	if got := must(ResolveProfileID(ctx, "other"))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
	abort := errors.New("abort")
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		kept := document.Profiles[:0]
		for _, p := range document.Profiles {
			if p.Key() != "other" {
				kept = append(kept, p)
			}
		}
		document.Profiles = kept
		// commit_count only grows after the commit: never answer from the memo here.
		_, err := ResolveProfileID(ctx, "other")
		wantError(t, err, ErrNotFound, "")
		return abort
	})
	if err != abort {
		t.Fatalf("got %v", err)
	}
	if got := must(ResolveProfileID(ctx, "other"))(t); got != "other" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactionTokensAreReadOncePerGenerationWithoutCopyingSystem(t *testing.T) {
	r, _ := scRepository(t)
	scAppendAgent(t, r, `{"id": "a", "name": "A", "profile_id": "default", "token": "agent-token-one"}`)
	ruleToken := must(r.System(bg))(t).SystemConfig.RuleProxyToken
	var cached *sanitizationTokens
	for range 3 {
		if !must(r.AgentTokensForSanitization(bg))(t).Has("agent-token-one") || !must(r.RuleProxyTokensForSanitization(bg))(t).Has(ruleToken) {
			t.Fatal("token missing")
		}
		if cached == nil {
			cached = r.tokensCache.Load()
		} else if r.tokensCache.Load() != cached {
			t.Fatal("tokens were read again within one generation")
		}
	}
}

func TestRotatedTokensAreRedactedByTheNextResponse(t *testing.T) {
	r, _ := scRepository(t)
	scAppendAgent(t, r, `{"id": "a", "name": "A", "profile_id": "default", "token": "agent-token-one"}`)
	oldRuleToken := must(r.System(bg))(t).SystemConfig.RuleProxyToken
	if !must(r.AgentTokensForSanitization(bg))(t).Has("agent-token-one") || !must(r.RuleProxyTokensForSanitization(bg))(t).Has(oldRuleToken) {
		t.Fatal("token missing")
	}
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents[0].Token = "agent-token-two"
		system.SystemConfig.RetiredRuleProxyTokens = []string{oldRuleToken}
		system.SystemConfig.RuleProxyToken = "rule-token-two"
		return nil
	}))
	agents, rules := must(r.AgentTokensForSanitization(bg))(t), must(r.RuleProxyTokensForSanitization(bg))(t)
	if !agents.Has("agent-token-two") || agents.Has("agent-token-one") || !rules.Has("rule-token-two") || !rules.Has(oldRuleToken) {
		t.Fatalf("agents %v rules %v", agents, rules)
	}
}

func TestTokensWrittenByAnotherRepositoryAreRedacted(t *testing.T) {
	r, _ := scRepository(t)
	must(r.AgentTokensForSanitization(bg))(t)
	other := openRepo(t, r.DataDir())
	scAppendAgent(t, other, `{"id": "b", "name": "B", "profile_id": "default", "token": "external-agent-token"}`)
	if !must(r.AgentTokensForSanitization(bg))(t).Has("external-agent-token") {
		t.Fatal("token written by another repository is not known")
	}
}

func TestHeadChangesWithEveryCommitAndOnlyThen(t *testing.T) {
	r, _ := scRepository(t)
	head := must(r.Head(bg))(t)
	must(r.System(bg))(t)
	_, _, err := r.PublishConfig(bg, "default", true)
	noError(t, err)
	must(r.Shared(bg))(t)
	if must(r.Head(bg))(t) != head {
		t.Fatal("head changed by reads")
	}
	seen := map[any]bool{head: true}
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		var item model.RuleLibraryItem
		noError(t, json.Unmarshal([]byte(`{"id": "r", "name": "Rule", "source_type": "content", "content": "a.test"}`), &item))
		shared.RuleLibrary = append(shared.RuleLibrary, item)
		return nil
	}))
	seen[must(r.Head(bg))(t)] = true
	// description is not a profile field: only the revision and updated_at change.
	_, _, err = r.SaveProfileFieldsJSON(bg, "other", []byte(`{"description":"edited"}`), nil)
	noError(t, err)
	seen[must(r.Head(bg))(t)] = true
	scAppendAgent(t, r, `{"id": "agent", "name": "Agent", "token": "agent-token", "profile_id": "default"}`)
	seen[must(r.Head(bg))(t)] = true
	if len(seen) != 4 {
		t.Fatalf("%d distinct heads", len(seen))
	}
	// A write by another process (another repository object on the same files) is noticed too.
	other := openRepo(t, r.DataDir())
	_, _, err = other.SaveProfileFieldsJSON(bg, "default", []byte(`{"description":"elsewhere"}`), nil)
	noError(t, err)
	if now := must(r.Head(bg))(t); now != must(other.Head(bg))(t) || seen[now] {
		t.Fatal("external commit not noticed")
	}
	// Saving a value and saving the old one back are two generations, not the first one again.
	before := must(r.Head(bg))(t)
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", []byte(`{"description":"x"}`), nil)
	noError(t, err)
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", []byte(`{"description":"elsewhere"}`), nil)
	noError(t, err)
	if must(r.Head(bg))(t) == before {
		t.Fatal("head repeated")
	}
}

func TestPublishSnapshotWithoutAgentsOnlyLeavesTheAgentRecordsOut(t *testing.T) {
	r, _ := scRepository(t)
	scAppendAgent(t, r, `{"id": "agent", "name": "Agent", "token": "agent-token", "profile_id": "default",
		"deployments": {"d": {"status": "succeeded"}}}`)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		var item model.RuleLibraryItem
		noError(t, json.Unmarshal([]byte(`{"id": "r", "name": "Rule", "source_type": "content", "content": "a.test"}`), &item))
		shared.RuleLibrary = append(shared.RuleLibrary, item)
		return nil
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", []byte(`{"rule_configs": [{"id": "set", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	full, revision, err := r.PublishConfig(bg, "default", true)
	noError(t, err)
	bare, bareRevision, err := r.PublishConfig(bg, "default", false)
	noError(t, err)
	fullObj := parseJSONObj(t, mustMarshalJSON(t, full))
	bareObj := parseJSONObj(t, mustMarshalJSON(t, bare))
	if _, has := bareObj.Get("agents"); has || len(full.Agents) != 1 || full.Agents[0].ID != "agent" {
		t.Fatal("agents")
	}
	fullObj.Delete("agents")
	wantJSONEqual(t, fullObj, bareObj)
	if revision != bareRevision {
		t.Fatal("revision")
	}
	if bare.Profile.RuleConfigs[0].LibraryRuleID != "r" {
		t.Fatalf("library_rule_id %q", bare.Profile.RuleConfigs[0].LibraryRuleID)
	}
	historyRaw := must(AgentHistoryJSON(full.Agents[0], "deployments"))(t)
	wantJSONEqual(t, historyRaw, json.RawMessage(`{"d": {"status": "succeeded"}}`))
}
