package agents

// Manager-level ports of test_agent_registration_security.py,
// test_agent_profiles.py, test_profile_agent_bindings.py (rebind through
// the manager; the route is #17's), test_agent_light_reads.py.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func registrationPayload(pairs ...any) *jsonx.Object {
	payload := jsonx.Obj("name", "secure-agent", "host", "10.0.0.8", "port", 8080, "service_type", "mihomo", "version", "1.2.3")
	payload.Update(jsonx.Obj(pairs...))
	return payload
}

func TestManagerDistinguishesNewRegistrationFromAuthenticatedExistingRegistration(t *testing.T) {
	e, _ := setupRuntime(t)
	created := e.register(registrationPayload(), nil)
	registered := e.register(registrationPayload("port", 9090), created.Value("token"))
	if created.Value("is_new") != true || !jsonx.Truthy(created.Value("token")) {
		t.Fatalf("created %v", created)
	}
	if !jsonx.Equal(registered, jsonx.Obj("id", created.Value("id"), "status", "online", "is_new", false)) {
		t.Fatalf("registered %v", registered)
	}
	if !jsonx.Equal(e.get(created.GetString("id")).Value("port"), 9090) {
		t.Fatal("port not updated")
	}
}

func TestManagerRejectsUnauthenticatedExistingRegistrationWithoutMutation(t *testing.T) {
	for _, token := range []any{nil, "wrong-token", "令牌🔐"} {
		e, _ := setupRuntime(t)
		created := e.register(registrationPayload(), nil)
		before := e.system().GetList("agents")
		_, err := e.m.RegisterAgent(e.ctx, registrationPayload("port", 9999, "version", "attacker"), token, nil)
		var permission *PermissionError
		if !errors.As(err, &permission) {
			t.Fatalf("%v: err %v", token, err)
		}
		if !jsonx.Equal(e.system().GetList("agents"), before) || e.get(created.GetString("id")).Value("token") != created.Value("token") {
			t.Fatal("mutated")
		}
	}
}

func TestRegistrationKeepsManagedFieldsAndPreservesToken(t *testing.T) {
	e, _ := setupRuntime(t)
	created := e.register(registrationPayload(), nil)
	id := created.GetString("id")
	must[*jsonx.Object](t)(e.m.SetDomainDiscovery(e.ctx, id, true))
	must[*jsonx.Object](t)(e.m.SetTrafficHistory(e.ctx, id, true))
	e.register(registrationPayload("version", "2.0.0"), created.Value("token"))
	stored := e.stored(id)
	if stored.Value("domain_discovery_enabled") != true || stored.Value("traffic_history_enabled") != true ||
		!stored.Has("traffic_history_since") || stored.Value("token") != created.Value("token") || stored.Value("version") != "2.0.0" {
		t.Fatalf("stored %v", stored)
	}
	keys := strings.Join(stored.Keys(), ",")
	if !strings.HasPrefix(keys, "id,name,host,port,token,service_type,profile_id,deployment_method,status,last_heartbeat,version,config_version,enabled,created_at,updated_at,deployments,latest_deployment,latest_upgrade,domain_discovery_enabled,traffic_history_enabled,traffic_history_since") {
		t.Fatalf("key order %s", keys)
	}
}

func TestAgentsAreSystemScopedAndBindProfiles(t *testing.T) {
	e, _ := setupRuntime(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	legacy := e.register(jsonx.Obj("name", "legacy", "host", "127.0.0.1"), nil)
	bound := e.register(jsonx.Obj("name", "bound", "host", "127.0.0.2", "profile_id", "alpha"), nil)
	if e.stored(legacy.GetString("id")).Value("profile_id") != "default" || e.stored(bound.GetString("id")).Value("profile_id") != "alpha" {
		t.Fatal("profiles")
	}
}

func TestAgentMutationsPersistThroughSystemTransactions(t *testing.T) {
	e, _ := setupRuntime(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	id := e.register(jsonx.Obj("name", "managed", "host", "10.0.0.9", "profile_id", "alpha"), nil).GetString("id")
	if e.stored(id).Value("name") != "managed" {
		t.Fatal("not stored")
	}
	if !must[bool](t)(e.m.UpdateAgent(e.ctx, id, jsonx.Obj("name", "updated", "profile_id", "beta"))) {
		t.Fatal("update")
	}
	e.beat(id, jsonx.Obj("version", "2.0.0", "config_version", "heartbeat"))
	persisted := e.stored(id)
	if persisted.Value("name") != "updated" || persisted.Value("profile_id") != "beta" || persisted.Value("config_version") != "heartbeat" || persisted.Value("version") != "2.0.0" {
		t.Fatalf("persisted %v", persisted)
	}
	fake := &fakeAgent{handle: always(jsonx.Obj("success", true), 200)}
	e.m.SetClient(fake)
	if got := must[*jsonx.Object](t)(e.m.PushLegacyConfig(e.ctx, id, "new config", PushConfig{})).Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("push %v", got)
	}
	if e.stored(id).Value("config_version") != "heartbeat" || len(fake.urls()) != 0 {
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
	if _, err := e.m.RegisterAgent(e.ctx, jsonx.Obj("name", "race", "host", "127.0.0.9", "profile_id", "alpha"), nil, nil); err == nil {
		t.Fatal("registered into a deleted profile")
	}
	if len(e.system().GetList("agents")) != 0 {
		t.Fatal("agent stored")
	}
}

func TestRegistrationRejectsInvalidProfileID(t *testing.T) {
	e, _ := setupRuntime(t)
	for _, profile := range []any{"bad id!", 5, true} {
		_, err := e.m.RegisterAgent(e.ctx, jsonx.Obj("name", "x", "host", "h", "profile_id", profile), nil, nil)
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
	_, err := e.m.UpdateAgent(e.ctx, e.id(), jsonx.Obj("profile_id", "other"))
	var conflict *DeploymentConflict
	if !errors.As(err, &conflict) || e.get(e.id()).Value("profile_id") != "default" {
		t.Fatalf("err %v", err)
	}
	e.record("raced-publish", jsonx.Obj("status", "succeeded"))
	if !must[bool](t)(e.m.UpdateAgent(e.ctx, e.id(), jsonx.Obj("profile_id", "other"))) {
		t.Fatal("update after success")
	}
	e.finish("raced-publish")
}

// test_profile_agent_bindings.py

type bindingsEnv struct {
	*env
	agents []*jsonx.Object
}

func newBindingsEnv(t *testing.T) *bindingsEnv {
	e, id := setupRuntime(t)
	must[bool](t)(e.m.DeleteAgent(e.ctx, id))
	if _, err := e.repo.CreateProfile(e.ctx, jsonx.Obj("id", "outside", "name", "外面"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.CreateProfile(e.ctx, jsonx.Obj("id", "home", "name", "家里"), ""); err != nil {
		t.Fatal(err)
	}
	b := &bindingsEnv{env: e}
	for index, service := range []string{"mihomo", "oxidns"} {
		registered := e.register(jsonx.Obj("name", "外面 "+service, "host", "192.0.2."+string(rune('1'+index)), "profile_id", "outside", "service_type", service), nil)
		b.agents = append(b.agents, e.get(registered.GetString("id")))
	}
	e.register(jsonx.Obj("name", "unrelated", "host", "192.0.2.9", "profile_id", "home"), nil)
	return b
}

func (b *bindingsEnv) ids() []any {
	var ids []any
	for _, agent := range b.agents {
		ids = append(ids, agent.Value("id"))
	}
	return ids
}

func TestRebindIsExplicitAndPreservesAgentContents(t *testing.T) {
	b := newBindingsEnv(t)
	before := b.stored(b.agents[0].GetString("id"))
	result := must[*jsonx.Object](t)(b.m.RebindProfileAgents(b.ctx, "outside", "home", b.ids()))
	if result.GetObject("from_profile").Value("name") != "外面" || result.GetObject("to_profile").Value("name") != "家里" || len(result.GetList("agents")) != 2 {
		t.Fatalf("result %v", result)
	}
	for _, item := range result.GetList("agents") {
		if item.(*jsonx.Object).Has("token") {
			t.Fatal("token in summary")
		}
	}
	after := b.stored(b.agents[0].GetString("id"))
	if after.Value("profile_id") != "home" || after.Value("token") != before.Value("token") {
		t.Fatalf("after %v", after)
	}
}

func TestOneBusyAgentBlocksTheEntireBatch(t *testing.T) {
	for _, field := range []string{"latest_upgrade", "deployments"} {
		b := newBindingsEnv(t)
		id := b.agents[1].GetString("id")
		b.editSystem(func(system *jsonx.Object) {
			agent, _ := findAgent(system.GetList("agents"), id)
			if field == "latest_upgrade" {
				agent.Set(field, jsonx.Obj("status", "installing"))
			} else {
				agent.Set(field, jsonx.Obj("x", jsonx.Obj("status", "uploading")))
			}
		})
		_, err := b.m.RebindProfileAgents(b.ctx, "outside", "home", b.ids())
		var conflict *DeploymentConflict
		if !errors.As(err, &conflict) || !strings.Contains(conflict.Message, "外面 oxidns") {
			t.Fatalf("%s: %v", field, err)
		}
		if b.stored(b.agents[0].GetString("id")).Value("profile_id") != "outside" {
			t.Fatal("partially moved")
		}
	}
}

func TestChangedBindingSetRequiresReviewBeforeMutating(t *testing.T) {
	b := newBindingsEnv(t)
	_, err := b.m.RebindProfileAgents(b.ctx, "outside", "home", b.ids()[:1])
	var conflict *DeploymentConflict
	if !errors.As(err, &conflict) || conflict.Message != "Agent 绑定已变化，请刷新列表后重新确认" {
		t.Fatalf("err %v", err)
	}
}

func TestInvalidRebindDoesNotChangeAnything(t *testing.T) {
	b := newBindingsEnv(t)
	cases := []struct {
		target any
		ids    any
		want   string
	}{
		{"outside", b.ids(), "请选择其他配置空间"},
		{"home", []any{}, "请提供已确认的 Agent ID 列表"},
		{"home", []any{"a", "a"}, "请提供已确认的 Agent ID 列表"},
		{"home", "x", "请提供已确认的 Agent ID 列表"},
		{nil, b.ids(), "Invalid profile id"},
	}
	for _, c := range cases {
		_, err := b.m.RebindProfileAgents(b.ctx, "outside", c.target, c.ids)
		if !errors.Is(err, repo.ErrValidation) || err.Error() != c.want {
			t.Fatalf("%v: %v", c, err)
		}
	}
	if _, err := b.m.RebindProfileAgents(b.ctx, "outside", "missing", b.ids()); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing profile: %v", err)
	}
}

// test_agent_light_reads.py (manager parts)

func withHistory(t *testing.T, m *Manager, id string) {
	_, err := m.UpdateAgents(context.Background(), "", func(_ context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, id)
		deployments := jsonx.NewObject()
		for i := 0; i < 5; i++ {
			key := "release-" + string(rune('0'+i))
			deployments.Set(key, jsonx.Obj("deployment_id", key, "status", "succeeded", "events", []any{jsonx.Obj("message", strings.Repeat("x", 200))}))
		}
		agent.Set("deployments", deployments)
		agent.Set("latest_deployment", deployments.GetObject("release-4").DeepCopy())
		agent.Set("ipv6_verifications", jsonx.Obj("release-4", jsonx.Obj("status", "ok")))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func withoutHistory(agent *jsonx.Object) *jsonx.Object {
	out := agent.Clone()
	out.Delete("deployments")
	out.Delete("ipv6_verifications")
	return out
}

func TestManagerReadsLayHeartbeatStateOverBothKindsOfRecord(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	must[bool](t)(e.m.UpdateHeartbeat(e.ctx, e.id(), jsonx.Obj("version", "9.9.9-go", "service_status", "running", "system_metrics", jsonx.Obj("cpu", 12.5))))
	full := e.get(e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	if light.Value("service_status") != "running" || !jsonx.Equal(light.Value("cpu"), 12.5) || light.Value("status") != "online" {
		t.Fatalf("light %v", light)
	}
	if !jsonx.Equal(withoutHistory(light), withoutHistory(full)) || full.GetObject("deployments").Len() != 5 || light.Value("deployments") != repo.AgentHistoryNotRead {
		t.Fatal("history")
	}
	if must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, "missing", false)) != nil {
		t.Fatal("missing")
	}
}

func TestUnreadHistoryIsNeverSaved(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	head, _ := e.repo.Head(e.ctx)
	_, err := e.m.UpdateAgents(e.ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		(*agents)[0] = light
		return nil, nil
	})
	if err == nil {
		t.Fatal("a record without history was saved")
	}
	if after, _ := e.repo.Head(e.ctx); after != head || e.get(e.id()).GetObject("deployments").Len() != 5 {
		t.Fatal("changed")
	}
}

// test_agent_light_reads.py::test_records_without_history_equal_the_full_ones_in_every_other_field
func TestRecordsWithoutHistoryEqualTheFullOnesInEveryOtherField(t *testing.T) {
	e := newEnv(t)
	e.register(jsonx.Obj("name", "second", "host", "second.test", "port", 8080, "service_type", "oxidns"), nil)
	withHistory(t, e.m, e.id())
	full := must[[]any](t)(e.repo.GetAgents(e.ctx, true))
	light := must[[]any](t)(e.repo.GetAgents(e.ctx, false))
	if !jsonx.Equal(full, e.system().GetList("agents")) || len(full) != 2 || len(light) != 2 {
		t.Fatalf("full %v", full)
	}
	for i := range full {
		if !jsonx.Equal(withoutHistory(light[i].(*jsonx.Object)), withoutHistory(full[i].(*jsonx.Object))) {
			t.Fatalf("%d: light %v full %v", i, light[i], full[i])
		}
	}
	first := full[0].(*jsonx.Object)
	if first.GetObject("deployments").Len() != 5 || !first.GetObject("ipv6_verifications").Has("release-4") {
		t.Fatalf("first %v", first)
	}
	for _, item := range light {
		// Also on a device without any history: a missing field must not read as "no publications".
		for _, field := range repo.AgentHistoryFields {
			if item.(*jsonx.Object).Value(field) != repo.AgentHistoryNotRead {
				t.Fatalf("%s of %v", field, item)
			}
		}
	}
	// Both are copies: editing them never reaches the stored document.
	light[0].(*jsonx.Object).GetObject("latest_deployment").Set("status", "edited")
	first.GetObject("deployments").GetObject("release-0").Set("status", "edited")
	again := must[[]any](t)(e.repo.GetAgents(e.ctx, true))[0].(*jsonx.Object)
	if again.GetObject("latest_deployment").Value("status") != "succeeded" ||
		again.GetObject("deployments").GetObject("release-0").Value("status") != "succeeded" {
		t.Fatalf("again %v", again)
	}
}

// test_agent_light_reads.py::test_manager_reads_lay_heartbeat_state_over_both_kinds_of_record
// (the list half; the single-record half is TestManagerReadsLayHeartbeatStateOverBothKindsOfRecord)
func TestManagerListsLayHeartbeatStateOverBothKindsOfRecord(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	must[bool](t)(e.m.UpdateHeartbeat(e.ctx, e.id(), jsonx.Obj("version", "9.9.9-go", "service_status", "running")))
	listedFull := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, true))
	listedLight := must[[]*jsonx.Object](t)(e.m.GetAllAgents(e.ctx, false))
	if len(listedFull) != len(listedLight) || len(listedFull) != 1 {
		t.Fatal("length")
	}
	for i := range listedFull {
		if !jsonx.Equal(withoutHistory(listedLight[i]), withoutHistory(listedFull[i])) || listedLight[i].Value("service_status") != "running" {
			t.Fatalf("light %v full %v", listedLight[i], listedFull[i])
		}
	}
}

// test_agent_light_reads.py::test_every_use_of_unread_history_fails. Python's
// sentinel raises on every dict operation; in Go the typed jsonx getters
// cannot refuse it (documented on repo.AgentHistoryNotRead), so the
// observable equivalents are: AgentHistory refuses it, it is never an object
// or a list to the getters (no history is ever mistaken for an empty one by
// type assertion), and every encoder refuses it.
func TestEveryUseOfUnreadHistoryFails(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	for _, field := range repo.AgentHistoryFields {
		if _, err := repo.AgentHistory(light, field); err == nil || !strings.Contains(err.Error(), "history=True") {
			t.Fatalf("%s: %v", field, err)
		}
		value := light.Value(field)
		if _, ok := value.(*jsonx.Object); ok {
			t.Fatalf("%s reads as an object", field)
		}
		if _, ok := value.([]any); ok {
			t.Fatalf("%s reads as a list", field)
		}
		for name, encode := range map[string]func(any) ([]byte, error){"Compact": jsonx.Compact, "Default": jsonx.Default, "Sorted": jsonx.Sorted} {
			if _, err := encode(value); err == nil {
				t.Fatalf("%s: %s encoded the sentinel", field, name)
			}
		}
	}
	full := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), true))
	if deployments, err := repo.AgentHistory(full, "deployments"); err != nil || deployments.(*jsonx.Object).Len() != 5 {
		t.Fatalf("full history %v %v", deployments, err)
	}
}

// test_agent_light_reads.py::test_unread_history_survives_copies_but_is_never_serialized_or_saved
// (the save half is TestUnreadHistoryIsNeverSaved).
func TestUnreadHistorySurvivesCopiesButIsNeverSerialized(t *testing.T) {
	e := newEnv(t)
	withHistory(t, e.m, e.id())
	light := must[*jsonx.Object](t)(e.m.GetAgentByID(e.ctx, e.id(), false))
	for name, copied := range map[string]*jsonx.Object{"deep": light.DeepCopy(), "shallow": light.Clone(),
		"value": jsonx.DeepCopy(light).(*jsonx.Object)} {
		for _, field := range repo.AgentHistoryFields {
			if copied.Value(field) != repo.AgentHistoryNotRead {
				t.Fatalf("%s copy lost %s", name, field)
			}
		}
	}
	if _, err := jsonx.Default(light); err == nil {
		t.Fatal("json.dumps(light) must fail")
	}
	if _, err := jsonx.Response(light); err == nil {
		t.Fatal("jsonify(light) must fail")
	}
}

// test_agent_profiles.py::test_agent_registration_does_not_overwrite_system_agents_from_profile_snapshot
func TestAgentRegistrationDoesNotOverwriteSystemAgentsFromProfileSnapshot(t *testing.T) {
	e := newEnv(t)
	e.createProfile("alpha")
	e.createProfile("beta")
	e.editSystem(func(system *jsonx.Object) {
		system.Set("agents", []any{jsonx.Obj("id", "stable", "name", "stable", "host", "10.0.0.1", "profile_id", "default")})
	})
	ctx := repo.WithScope(repo.WithRepository(context.Background(), e.repo))
	alpha := "alpha"
	repo.SetRequest(ctx, repo.RequestInfo{Endpoint: "agents.handle_agents", HeaderProfile: &alpha})
	defer repo.ResetConfigContext(ctx)
	// The request has read (and cached) its configuration snapshot.
	if _, err := repo.GetConfig(ctx, ""); err != nil {
		t.Fatal(err)
	}
	must[[]*jsonx.Object](t)(e.m.GetAllAgents(ctx, true))
	e.editSystem(func(system *jsonx.Object) {
		system.Set("agents", []any{
			jsonx.Obj("id", "stable", "name", "stable", "host", "10.0.0.1", "profile_id", "default"),
			jsonx.Obj("id", "concurrent", "name", "concurrent", "host", "10.0.0.2", "profile_id", "beta"),
		})
	})
	if _, err := e.m.RegisterAgent(ctx, jsonx.Obj("name", "new", "host", "10.0.0.3", "profile_id", "alpha"), nil, nil); err != nil {
		t.Fatal(err)
	}
	ids := map[any]bool{}
	for _, item := range e.system().GetList("agents") {
		ids[item.(*jsonx.Object).Value("id")] = true
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
		{jsonx.Int(1), "1", false}, {"1", jsonx.Int(1), false}, {"\x7f", "\x7f", true},
	} {
		if got := ConstantTimeASCIIEqual(c.provided, c.expected); got != c.want {
			t.Errorf("%#v %#v: %v", c.provided, c.expected, got)
		}
	}
}
