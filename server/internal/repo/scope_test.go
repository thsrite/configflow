package repo

// Port of backend/test_request_config_snapshots.py: per-request config
// reads validate a profile once and copy no whole document. A Flask request
// context is a Scope with SetRequest; the two cases that run a Flask app
// replay what install_profile_context's hooks do (the hooks themselves
// belong to package httpx). sanitize_external_payload belongs to the export
// package: its three cases assert the token sets it reads.

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/jsonx"
)

func scRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "other", "name", "Other"), ""))(t)
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
	must(r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", append(system.GetList("agents"), parse(t, agent)))
		return nil, nil
	}))(t)
}

func TestReadsAndSavesNeverModifyTheCommittedDocument(t *testing.T) {
	r, ctx := scRepository(t)
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Update(parse(t, `{"nodes": [{"id": "n", "name": "Node"}],
			"rule_library": [{"id": "r", "name": "Rule", "enabled": {"custom": []}}]}`))
		return nil, nil
	}))(t)
	_, _, err := r.UpdateProfileFields(bg, "default", parse(t, `{
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["n"]}],
		"rule_configs": [{"id": "rule", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	entry := must(r.readEntry(bg, false))(t)
	source := must(entry.legacyDocument())(t)
	before := source.DeepCopy()
	config := must(GetConfig(ctx, "default"))(t)
	config.GetList("proxy_groups")[0].(*jsonx.Object).Set("name", "Changed")
	config.GetList("rule_configs")[0].(*jsonx.Object).GetObject("library_enabled").Set("custom", []any{"Changed"})
	shared := must(GetSharedConfig(ctx))(t)
	shared.GetList("nodes")[0].(*jsonx.Object).Set("name", "Changed")
	system := must(GetSystemConfig(ctx))(t)
	system.GetObject("system_config").Set("server_domain", "https://changed.example")
	noError(t, SaveConfig(ctx, config, "default"))
	noError(t, SaveSharedConfig(ctx, shared, nil))
	noError(t, SaveSystemConfig(ctx, system, nil))
	if must(must(r.readEntry(bg, false))(t).legacyDocument())(t) == source {
		t.Fatal("the committed generation was not replaced")
	}
	wantEqual(t, source, before)
	wantEqual(t, must(r.GetProfile(bg, "default"))(t).GetList("proxy_groups")[0].(*jsonx.Object).Value("name"), "Changed")
	wantEqual(t, must(r.GetShared(bg))(t).GetList("nodes")[0].(*jsonx.Object).Value("name"), "Changed")
}

func TestProfileBaselineIsTheReadViewAndKeepsConcurrentEdits(t *testing.T) {
	r, ctx := scRepository(t)
	config := must(GetConfig(ctx, "default"))(t)
	readView := config.DeepCopy()
	config.GetObject("mihomo").Set("custom_config", "mixed-port: 7901")
	group := parse(t, `{"id": "g", "name": "Concurrent", "type": "select"}`)
	_, _, err := r.UpdateProfileFields(bg, "default", jsonx.Obj("proxy_groups", []any{group}), nil)
	noError(t, err)
	wantEqual(t, must(ScopeOf(ctx).baselineOf(r, "default"))(t), readView)
	noError(t, SaveConfig(ctx, config, "default"))
	saved := must(r.GetProfile(bg, "default"))(t)
	wantEqual(t, saved.GetObject("mihomo").Value("custom_config"), "mixed-port: 7901")
	wantEqual(t, saved.Value("proxy_groups"), []any{group})
}

func TestSharedBaselineKeepsConcurrentEdits(t *testing.T) {
	r, ctx := scRepository(t)
	shared := must(GetSharedConfig(ctx))(t)
	shared.Set("nodes", []any{parse(t, `{"id": "mine", "name": "Mine"}`)})
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("subscriptions", []any{parse(t, `{"id": "s", "name": "Concurrent", "url": "https://example.test/s"}`)})
		return nil, nil
	}))(t)
	noError(t, SaveSharedConfig(ctx, shared, nil))
	saved := must(r.GetShared(bg))(t)
	wantEqual(t, saved.Value("nodes"), []any{parse(t, `{"id": "mine", "name": "Mine"}`)})
	wantEqual(t, saved.GetList("subscriptions")[0].(*jsonx.Object).Value("id"), "s")
}

func TestTransactionReadsKeepAnEagerBaseline(t *testing.T) {
	r, ctx := scRepository(t)
	var view *jsonx.Object
	var entry baseline
	_, _, err := r.UpdateProfileTransaction(ctx, "default", func(ctx context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		view = must(GetConfig(ctx, "default"))(t)
		entry = ScopeOf(ctx).baselines["default"]
		profile.Set("name", "Changed in transaction")
		return nil, nil
	})
	noError(t, err)
	// The transaction's working copy may still change: never keep it as a baseline source.
	if entry.build != nil || entry.value == nil || entry.value == view {
		t.Fatal("baseline inside a transaction must be an eager copy")
	}
	wantEqual(t, entry.value, view)
}

func TestRequestReusesTheValidatedProfile(t *testing.T) {
	r, _ := scRepository(t)
	start := r.metadataReads.Load()
	ctx := scRequest(r, "other")
	ResetConfigContext(ctx)
	wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "other")
	config := must(GetConfig(ctx, ""))(t)
	wantEqual(t, config.Value("profile_id"), "other")
	if must(GetConfig(ctx, "other"))(t) != config {
		t.Fatal("the request's snapshot was not reused")
	}
	wantEqual(t, must(ResolveProfileID(ctx, "other"))(t), "other")
	wantEqual(t, must(ResolveProfileID(ctx, "default"))(t), "default")
	wantEqual(t, must(ResolveProfileID(ctx, "default"))(t), "default")
	if calls := r.metadataReads.Load() - start; calls != 2 {
		t.Fatalf("profile validated %d times, want 2 (other, default)", calls)
	}
}

func TestDeletingAProfileInTheSameRequestIsNoticed(t *testing.T) {
	r, _ := scRepository(t)
	ctx := scRequest(r, "other")
	ResetConfigContext(ctx)
	wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "other")
	wantEqual(t, must(ResolveProfileID(ctx, "other"))(t), "other")
	must(GetConfig(ctx, ""))(t)
	noError(t, r.DeleteProfile(bg, "other", ""))
	_, err := ResolveProfileID(ctx, "")
	wantError(t, err, ErrNotFound, "")
	_, err = ResolveProfileID(ctx, "other")
	wantError(t, err, ErrNotFound, "")
	_, err = GetConfig(ctx, "")
	wantError(t, err, ErrNotFound, "")
}

func TestSwitchingTheDefaultInTheSameRequestIsNoticed(t *testing.T) {
	r, _ := scRepository(t)
	ctx := scRequest(r, "")
	ResetConfigContext(ctx)
	wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "default")
	must(r.SetDefaultProfile(bg, "other"))(t)
	wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "other")
}

func TestAnotherRequestDoesNotReuseTheValidatedProfile(t *testing.T) {
	r, _ := scRepository(t)
	outer := scRequest(r, "other")
	wantEqual(t, must(ResolveProfileID(outer, ""))(t), "other")
	wantEqual(t, must(ResolveProfileID(scRequest(r, ""), ""))(t), "default")
	wantEqual(t, must(ResolveProfileID(outer, ""))(t), "other")
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
		config := must(GetConfig(ctx, ""))(t)
		if must(GetConfig(ctx, must(ResolveProfileID(ctx, ""))(t)))(t) != config {
			t.Fatal("the request's snapshot was not reused")
		}
		wantEqual(t, config.Value("profile_id"), "other")
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
		wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "default")
		must(r.SetDefaultProfile(ctx, "other"))(t)
		wantEqual(t, must(ResolveProfileID(ctx, ""))(t), "other")
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
	wantEqual(t, must(ResolveProfileID(ctx, "other"))(t), "other")
	abort := errors.New("abort")
	err := r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		document.GetObject("profiles").Delete("other")
		// commit_count only grows after the commit: never answer from the memo here.
		_, err := ResolveProfileID(ctx, "other")
		wantError(t, err, ErrNotFound, "")
		return abort
	})
	if err != abort {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, must(ResolveProfileID(ctx, "other"))(t), "other")
}

func TestRedactionTokensAreReadOncePerGenerationWithoutCopyingSystem(t *testing.T) {
	r, _ := scRepository(t)
	scAppendAgent(t, r, `{"id": "a", "name": "A", "profile_id": "default", "token": "agent-token-one"}`)
	ruleToken := must(r.GetSystem(bg))(t).GetObject("system_config").GetString("rule_proxy_token")
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
	oldRuleToken := must(r.GetSystem(bg))(t).GetObject("system_config").GetString("rule_proxy_token")
	if !must(r.AgentTokensForSanitization(bg))(t).Has("agent-token-one") || !must(r.RuleProxyTokensForSanitization(bg))(t).Has(oldRuleToken) {
		t.Fatal("token missing")
	}
	must(r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetList("agents")[0].(*jsonx.Object).Set("token", "agent-token-two")
		settings := system.GetObject("system_config")
		settings.Set("retired_rule_proxy_tokens", []any{oldRuleToken})
		settings.Set("rule_proxy_token", "rule-token-two")
		return nil, nil
	}))(t)
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
	must(r.GetSystem(bg))(t)
	_, _, err := r.GetPublishSnapshot(bg, "default", true)
	noError(t, err)
	must(r.GetShared(bg))(t)
	if must(r.Head(bg))(t) != head {
		t.Fatal("head changed by reads")
	}
	seen := map[any]bool{head: true}
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", append(shared.GetList("rule_library"),
			parse(t, `{"id": "r", "name": "Rule", "source_type": "content", "content": "a.test"}`)))
		return nil, nil
	}))(t)
	seen[must(r.Head(bg))(t)] = true
	// description is not a profile field: only the revision and updated_at change.
	_, _, err = r.UpdateProfileFields(bg, "other", jsonx.Obj("description", "edited"), nil)
	noError(t, err)
	seen[must(r.Head(bg))(t)] = true
	scAppendAgent(t, r, `{"id": "agent", "name": "Agent", "token": "agent-token", "profile_id": "default"}`)
	seen[must(r.Head(bg))(t)] = true
	if len(seen) != 4 {
		t.Fatalf("%d distinct heads", len(seen))
	}
	// A write by another process (another repository object on the same files) is noticed too.
	other := openRepo(t, r.DataDir())
	_, _, err = other.UpdateProfileFields(bg, "default", jsonx.Obj("description", "elsewhere"), nil)
	noError(t, err)
	if now := must(r.Head(bg))(t); now != must(other.Head(bg))(t) || seen[now] {
		t.Fatal("external commit not noticed")
	}
	// Saving a value and saving the old one back are two generations, not the first one again.
	before := must(r.Head(bg))(t)
	_, _, err = r.UpdateProfileFields(bg, "default", jsonx.Obj("description", "x"), nil)
	noError(t, err)
	_, _, err = r.UpdateProfileFields(bg, "default", jsonx.Obj("description", "elsewhere"), nil)
	noError(t, err)
	if must(r.Head(bg))(t) == before {
		t.Fatal("head repeated")
	}
}

func TestPublishSnapshotWithoutAgentsOnlyLeavesTheAgentRecordsOut(t *testing.T) {
	r, _ := scRepository(t)
	scAppendAgent(t, r, `{"id": "agent", "name": "Agent", "token": "agent-token", "profile_id": "default",
		"deployments": {"d": {"status": "succeeded"}}}`)
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", append(shared.GetList("rule_library"),
			parse(t, `{"id": "r", "name": "Rule", "source_type": "content", "content": "a.test"}`)))
		return nil, nil
	}))(t)
	_, _, err := r.UpdateProfileFields(bg, "default", parse(t, `{"rule_configs": [{"id": "set", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	full, revision, err := r.GetPublishSnapshot(bg, "default", true)
	noError(t, err)
	bare, bareRevision, err := r.GetPublishSnapshot(bg, "default", false)
	noError(t, err)
	if bare.Has("agents") || full.GetList("agents")[0].(*jsonx.Object).Value("id") != "agent" {
		t.Fatal("agents")
	}
	full.Delete("agents")
	wantEqual(t, full, bare)
	if revision != bareRevision {
		t.Fatal("revision")
	}
	wantEqual(t, bare.GetList("rule_configs")[0].(*jsonx.Object).Value("library_rule_id"), "r")

	// get_agents(history=False): history fields hold the sentinel.
	agents := must(r.GetAgents(bg, false))(t)
	agent := agents[0].(*jsonx.Object)
	if agent.Value("deployments") != AgentHistoryNotRead || agent.Value("token") != "agent-token" {
		t.Fatal("history sentinel")
	}
	if _, err := AgentHistory(agent, "deployments"); err == nil || err.Error() != AgentHistoryNotReadMessage {
		t.Fatalf("got %v", err)
	}
	if _, err := jsonx.Compact(agent); err == nil {
		t.Fatal("the sentinel must not be encodable")
	}
	withHistory := must(r.GetAgents(bg, true))(t)[0].(*jsonx.Object)
	wantEqual(t, must(AgentHistory(withHistory, "deployments"))(t), parse(t, `{"d": {"status": "succeeded"}}`))
}
