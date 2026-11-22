package agents

// Manager-level ports of test_agent_registration_security.py,
// test_agent_profiles.py, test_profile_agent_bindings.py (rebind through
// the manager; the route is #17's).

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func registrationPayload(pairs ...any) map[string]any {
	payload := map[string]any{
		"name": "secure-agent", "host": "10.0.0.8", "port": 8080,
		"service_type": "mihomo", "version": "1.2.3",
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		payload[pairs[i].(string)] = pairs[i+1]
	}
	return payload
}

func (e *env) editAgentJSON(id string, updates map[string]any) (bool, error) {
	e.t.Helper()
	edits, err := DecodeAgentEdits(mustJSON(e.t, updates))
	if err != nil {
		return false, err
	}
	return e.m.EditAgent(e.ctx, id, edits)
}

func TestManagerDistinguishesNewRegistrationFromAuthenticatedExistingRegistration(t *testing.T) {
	e, _ := setupRuntime(t)
	created := e.register(registrationPayload(), "")
	if !created.IsNew || created.Token == nil || *created.Token == "" {
		t.Fatalf("created %+v", created)
	}
	registered := e.register(registrationPayload("port", 9090), *created.Token)
	if diff := diffJSONValues(t, map[string]any{"id": created.ID, "status": "online", "is_new": false}, registered); diff != "" {
		t.Fatalf("registered %+v: %s", registered, diff)
	}
	if e.get(created.ID).Port != 9090 {
		t.Fatal("port not updated")
	}
}

func TestManagerRejectsUnauthenticatedExistingRegistrationWithoutMutation(t *testing.T) {
	for _, token := range []string{"", "wrong-token", "令牌🔐"} {
		e, _ := setupRuntime(t)
		created := e.register(registrationPayload(), "")
		before := e.system().Agents
		input, err := DecodeRegistrationInput(mustJSON(t, registrationPayload("port", 9999, "version", "attacker")))
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{ExistingToken: token})
		var permission *PermissionError
		if !errors.As(err, &permission) {
			t.Fatalf("%q: err %v", token, err)
		}
		if diffJSONValues(t, before, e.system().Agents) != "" || e.get(created.ID).Token != *created.Token {
			t.Fatal("mutated")
		}
	}
}

func TestRegistrationKeepsManagedFieldsAndPreservesToken(t *testing.T) {
	e, _ := setupRuntime(t)
	created := e.register(registrationPayload(), "")
	id := created.ID
	must[*model.Agent](t)(e.m.SetDomainDiscoveryModel(e.ctx, id, true))
	must[*model.Agent](t)(e.m.SetTrafficHistoryModel(e.ctx, id, true))
	e.register(registrationPayload("version", "2.0.0"), *created.Token)
	stored := e.stored(id)
	if stored.DomainDiscoveryEnabled == nil || !*stored.DomainDiscoveryEnabled ||
		stored.TrafficHistoryEnabled == nil || !*stored.TrafficHistoryEnabled ||
		stored.TrafficHistorySince == nil || stored.Token != *created.Token || stored.Version != "2.0.0" {
		t.Fatalf("stored %+v", stored)
	}
	// Members are written in the record's declaration order (O18 T1b);
	// empty history and null journals are missing members.
	keys := orderedJSONKeys(t, mustJSON(t, stored))
	for _, key := range strings.Split("id,name,host,port,token,service_type,profile_id,deployment_method,status,last_heartbeat,version,config_version,enabled,created_at,updated_at,domain_discovery_enabled,traffic_history_enabled,traffic_history_since", ",") {
		if !slices.Contains(keys, key) {
			t.Fatalf("missing %s in %v", key, keys)
		}
	}
	if false {
	}
}

func TestAgentsAreSystemScopedAndBindProfiles(t *testing.T) {
	e, _ := setupRuntime(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	legacy := e.register(map[string]any{"name": "legacy", "host": "127.0.0.1"}, "")
	bound := e.register(map[string]any{"name": "bound", "host": "127.0.0.2", "profile_id": "alpha"}, "")
	if e.stored(legacy.ID).ProfileID != "default" || e.stored(bound.ID).ProfileID != "alpha" {
		t.Fatal("profiles")
	}
}

func TestAgentMutationsPersistThroughSystemTransactions(t *testing.T) {
	e, _ := setupRuntime(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	id := e.register(map[string]any{"name": "managed", "host": "10.0.0.9", "profile_id": "alpha"}, "").ID
	if e.stored(id).Name != "managed" {
		t.Fatal("not stored")
	}
	if !must[bool](t)(e.editAgentJSON(id, map[string]any{"name": "updated", "profile_id": "beta"})) {
		t.Fatal("update")
	}
	e.beat(id, map[string]any{"version": "2.0.0", "config_version": "heartbeat"})
	persisted := e.stored(id)
	if persisted.Name != "updated" || persisted.ProfileID != "beta" || persisted.ConfigVersion != "heartbeat" || persisted.Version != "2.0.0" {
		t.Fatalf("persisted %+v", persisted)
	}
	fake := &fakeAgent{handle: always(map[string]any{"success": true}, 200)}
	e.m.SetClient(fake)
	pushRaw := must[json.RawMessage](t)(e.m.PushLegacyConfigModel(e.ctx, id, "new config", LegacyConfigOptions{}))
	var pushResp struct {
		HTTPStatus int `json:"http_status"`
	}
	if err := json.Unmarshal(pushRaw, &pushResp); err != nil || pushResp.HTTPStatus != 409 {
		t.Fatalf("push %s %v", pushRaw, err)
	}
	if e.stored(id).ConfigVersion != "heartbeat" || len(fake.urls()) != 0 {
		t.Fatal("push touched the Agent")
	}
	if !must[bool](t)(e.m.DeleteAgent(e.ctx, id)) || e.stored(id) != nil {
		t.Fatal("delete")
	}
}

func TestAgentRegistrationValidatesProfileInsideSystemTransaction(t *testing.T) {
	e, id := setupRuntime(t)
	must[bool](t)(e.m.DeleteAgent(e.ctx, id))
	e.createProfile("alpha")
	testFailUpdate = func() error {
		testFailUpdate = nil
		return e.repo.DeleteProfile(e.ctx, "alpha", "")
	}
	t.Cleanup(func() { testFailUpdate = nil })
	input, err := DecodeRegistrationInput(mustJSON(t, map[string]any{"name": "race", "host": "127.0.0.9", "profile_id": "alpha"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{}); err == nil {
		t.Fatal("registered into a deleted profile")
	}
	if len(e.system().Agents) != 0 {
		t.Fatal("agent stored")
	}
}

func TestRegistrationRejectsInvalidProfileID(t *testing.T) {
	e, _ := setupRuntime(t)
	for _, profile := range []any{"bad id!", 5, true} {
		input, err := DecodeRegistrationInput(mustJSON(t, map[string]any{"name": "x", "host": "h", "profile_id": profile}))
		if err == nil {
			_, err = e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{})
		}
		if !errors.Is(err, repo.ErrValidation) || err.Error() != "Invalid profile id" {
			t.Fatalf("%v: %v", profile, err)
		}
	}
}

func TestProfileGuardObservesPublicationStartedBeforeTransaction(t *testing.T) {
	e := newEnv(t)
	e.createProfile("other")
	testFailUpdate = func() error {
		testFailUpdate = nil
		e.begin("raced-publish")
		return nil
	}
	t.Cleanup(func() { testFailUpdate = nil })
	_, err := e.editAgentJSON(e.id(), map[string]any{"profile_id": "other"})
	var conflict *DeploymentConflict
	if !errors.As(err, &conflict) || e.get(e.id()).ProfileID != "default" {
		t.Fatalf("err %v", err)
	}
	e.record("raced-publish", map[string]any{"status": "succeeded"})
	if !must[bool](t)(e.editAgentJSON(e.id(), map[string]any{"profile_id": "other"})) {
		t.Fatal("update after success")
	}
	e.finish("raced-publish")
}

// test_profile_agent_bindings.py

type bindingsEnv struct {
	*env
	agents []*model.Agent
}

func newBindingsEnv(t *testing.T) *bindingsEnv {
	e, id := setupRuntime(t)
	must[bool](t)(e.m.DeleteAgent(e.ctx, id))
	if _, err := e.repo.CreateProfileRecord(e.ctx, repo.ProfileCreation{ID: "outside", Name: "外面"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.CreateProfileRecord(e.ctx, repo.ProfileCreation{ID: "home", Name: "家里"}, ""); err != nil {
		t.Fatal(err)
	}
	b := &bindingsEnv{env: e}
	for index, service := range []string{"mihomo", "oxidns"} {
		registered := e.register(map[string]any{
			"name": "外面 " + service, "host": "192.0.2." + string(rune('1'+index)),
			"profile_id": "outside", "service_type": service,
		}, "")
		b.agents = append(b.agents, e.get(registered.ID))
	}
	e.register(map[string]any{"name": "unrelated", "host": "192.0.2.9", "profile_id": "home"}, "")
	return b
}

func (b *bindingsEnv) ids() []string {
	var ids []string
	for _, agent := range b.agents {
		ids = append(ids, agent.ID)
	}
	return ids
}

func TestRebindIsExplicitAndPreservesAgentContents(t *testing.T) {
	b := newBindingsEnv(t)
	before := b.stored(b.agents[0].ID)
	result := must[*ProfileRebinding](t)(b.m.RebindProfiles(b.ctx, "outside", "home", b.ids()))
	if result.FromProfile.Name != "外面" || result.ToProfile.Name != "家里" || len(result.Agents) != 2 {
		t.Fatalf("result %+v", result)
	}
	if strings.Contains(string(mustJSON(t, result.Agents)), "token") {
		t.Fatal("token in summary")
	}
	after := b.stored(b.agents[0].ID)
	if after.ProfileID != "home" || after.Token != before.Token {
		t.Fatalf("after %+v", after)
	}
}

func TestOneBusyAgentBlocksTheEntireBatch(t *testing.T) {
	for _, field := range []string{"latest_upgrade", "deployments"} {
		b := newBindingsEnv(t)
		id := b.agents[1].ID
		b.editSystem(func(system *model.System) {
			agent := registryAgent(system.Agents, id)
			if field == "latest_upgrade" {
				_ = model.SetStoredMember(&agent, field, []byte(`{"status":"installing"}`))
			} else {
				_ = model.SetStoredMember(&agent, field, []byte(`{"x":{"status":"uploading"}}`))
			}
		})
		_, err := b.m.RebindProfiles(b.ctx, "outside", "home", b.ids())
		var conflict *DeploymentConflict
		if !errors.As(err, &conflict) || !strings.Contains(conflict.Message, "外面 oxidns") {
			t.Fatalf("%s: %v", field, err)
		}
		if b.stored(b.agents[0].ID).ProfileID != "outside" {
			t.Fatal("partially moved")
		}
	}
}

func TestChangedBindingSetRequiresReviewBeforeMutating(t *testing.T) {
	b := newBindingsEnv(t)
	_, err := b.m.RebindProfiles(b.ctx, "outside", "home", b.ids()[:1])
	var conflict *DeploymentConflict
	if !errors.As(err, &conflict) || conflict.Message != "Agent 绑定已变化，请刷新列表后重新确认" {
		t.Fatalf("err %v", err)
	}
}

func TestInvalidRebindDoesNotChangeAnything(t *testing.T) {
	b := newBindingsEnv(t)
	cases := []struct {
		target string
		ids    []string
		want   string
	}{
		{"outside", b.ids(), "请选择其他配置空间"},
		{"home", []string{}, "请提供已确认的 Agent ID 列表"},
		{"home", []string{"a", "a"}, "请提供已确认的 Agent ID 列表"},
		{"", b.ids(), "Invalid profile id"},
	}
	for _, c := range cases {
		_, err := b.m.RebindProfiles(b.ctx, "outside", c.target, c.ids)
		if !errors.Is(err, repo.ErrValidation) || err.Error() != c.want {
			t.Fatalf("%v: %v", c, err)
		}
	}
	if _, err := b.m.RebindProfiles(b.ctx, "outside", "missing", b.ids()); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing profile: %v", err)
	}
}

// test_agent_profiles.py::test_agent_registration_does_not_overwrite_system_agents_from_profile_snapshot
func TestAgentRegistrationDoesNotOverwriteSystemAgentsFromProfileSnapshot(t *testing.T) {
	e := newEnv(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	e.editSystem(func(system *model.System) {
		system.Agents = []model.Agent{{ID: "stable", Name: "stable", Host: "10.0.0.1", ProfileID: "default"}}
	})
	ctx := repo.WithScope(repo.WithRepository(context.Background(), e.repo))
	alpha := "alpha"
	repo.SetRequest(ctx, repo.RequestInfo{Endpoint: "agents.handle_agents", HeaderProfile: &alpha})
	defer repo.ResetConfigContext(ctx)
	// The request has read (and cached) its configuration snapshot.
	if _, err := e.repo.Config(ctx, "alpha", repo.ResourceRoots{}); err != nil {
		t.Fatal(err)
	}
	must[[]model.Agent](t)(e.m.ListAgents(ctx, true))
	e.editSystem(func(system *model.System) {
		system.Agents = []model.Agent{
			{ID: "stable", Name: "stable", Host: "10.0.0.1", ProfileID: "default"},
			{ID: "concurrent", Name: "concurrent", Host: "10.0.0.2", ProfileID: "beta"},
		}
	})
	input, err := DecodeRegistrationInput(mustJSON(t, map[string]any{"name": "new", "host": "10.0.0.3", "profile_id": "alpha"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.RegisterIdentity(ctx, input, RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range e.system().Agents {
		ids[item.ID] = true
	}
	if !ids["stable"] || !ids["concurrent"] || len(ids) != 3 {
		t.Fatalf("ids %v", ids)
	}
}

// _constant_time_ascii_equal: hmac.compare_digest of ASCII bytes only.
func TestConstantTimeASCIIEqual(t *testing.T) {
	for _, c := range []struct {
		provided, expected any
		want               bool
	}{
		{"token", "token", true}, {"", "", true}, {"token", "tokeN", false}, {"tok", "token", false},
		{"令牌", "令牌", false}, {"café", "café", false}, {nil, "token", false}, {"token", nil, false},
		{1, "1", false}, {"1", 1, false}, {"\x7f", "\x7f", true},
	} {
		if got := ConstantTimeASCIIEqual(c.provided, c.expected); got != c.want {
			t.Errorf("%#v %#v: %v", c.provided, c.expected, got)
		}
	}
}
