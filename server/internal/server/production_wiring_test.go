package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/rulesource"
	"configflow/internal/setup"
	"configflow/internal/substore"
	"configflow/internal/tasks"
)

// nilFuncFields lists the exported func and interface fields of a struct value
// that are nil.
func nilFuncFields(v any) []string {
	rv := reflect.ValueOf(v)
	var missing []string
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Func, reflect.Interface:
			if rv.Field(i).IsNil() {
				missing = append(missing, f.Name)
			}
		}
	}
	return missing
}

// TestProductionAssemblyLeavesNoDependencyUnset builds the application the way
// cmd/configflow.run does, starting from cleared process hooks, and checks that
// every dependency the W0–W8 rewiring moved is provided: by the App's own
// instances, by the transitional process defaults pointing at those instances,
// or by the remaining package hooks. A hook that only tests used to fill (the
// D9 accident) fails here instead of in production.
func TestProductionAssemblyLeavesNoDependencyUnset(t *testing.T) {
	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	t.Cleanup(convert.ResetMihomoIntegrationsForTest(t))
	t.Cleanup(setup.SetExternalOwners(setup.ExternalOwners{}))
	t.Cleanup(onboarding.SetAdoptOwnedWrite(nil))
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	previousRepoHooks := repo.SetHooks(repo.Hooks{})
	t.Cleanup(func() { repo.SetHooks(previousRepoHooks) })
	agents.ResetHooks()
	content, ipv6Host := routers.RulesetContent, routers.IPv6HostRequest
	routers.RulesetContent, routers.IPv6HostRequest = nil, nil
	t.Cleanup(func() { routers.RulesetContent, routers.IPv6HostRequest = content, ipv6Host })
	t.Cleanup(func() {
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})

	// cmd/configflow.run: repository with server Options, explicit Runtime, New.
	r, err := repo.OpenDefaultWithOptions(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})
	if err := lifecycle.UseRuntime(rt); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app, err := New(ctx, Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(app.Agents.Close)

	checks := []struct {
		name string
		ok   bool
	}{
		{"runtime is the process default", app.Runtime == rt && lifecycle.Current() == rt},
		{"tasks registry is the default", app.Tasks != nil && tasks.Default() == app.Tasks},
		{"notifications service is the default", app.Notifications != nil && notifications.Default() == app.Notifications},
		{"Sub-Store service is the default", app.SubStore != nil && substore.Default() == app.SubStore},
		{"refresh service is the default", app.Refresh != nil && refresh.Default() == app.Refresh},
		{"provider service is the default", app.Provider != nil && provider.Default() == app.Provider},
		{"aggregations service", app.Aggregations != nil},
		{"deploy service is the default", app.Deploy != nil && deploy.Default() == app.Deploy},
		{"rule source service is the default", app.RuleSource != nil && rulesource.Default() == app.RuleSource},
		{"onboarding adoption owner", onboarding.RegisteredAdoptOwnedWrite() != nil},
		{"gateway rule source reader", gateway.RegisteredRuleSourceReader() != nil},
		{"routers rule set content", routers.RulesetContent != nil},
		{"routers IPv6 host", routers.IPv6HostRequest != nil},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("production wiring: %s", c.name)
		}
	}
	for name, validate := range map[string]func() error{
		"tasks": app.Tasks.Validate, "notifications": app.Notifications.Validate,
		"provider": app.Provider.Validate, "deploy": app.Deploy.Validate,
	} {
		if err := validate(); err != nil {
			t.Errorf("%s.Validate after New: %v", name, err)
		}
	}
	owners := setup.RegisteredOwners()
	if owners.DNS == nil || owners.Installation == nil || owners.ReadRouteContent == nil || owners.VerifyRouter == nil {
		t.Errorf("setup owners: dns=%t installation=%t content=%t router=%t",
			owners.DNS != nil, owners.Installation != nil, owners.ReadRouteContent != nil, owners.VerifyRouter != nil)
	}
	// Since W5/W6 the Agent domain is wired through the Manager's Options and
	// the D1 two-phase setter, not the global agents.Hooks (a sealed Manager
	// ignores them): check the instances instead.
	if app.Agents == nil || app.Metrics == nil || app.Monitor == nil || app.DNSHA == nil || app.DNSFollow == nil || app.RuleSync == nil || app.IPv6 == nil || app.Upgrades == nil || app.Traffic == nil {
		t.Error("production wiring: an Agent-domain service is missing")
	}
	if err := app.Agents.SetPairPublisher(app.DNSHA); err != agents.ErrManagerStarted {
		t.Errorf("Manager not sealed after New: SetPairPublisher = %v", err)
	}
	if len(app.Agents.DeletedStores()) != len(agents.AgentDeletedStores) {
		t.Errorf("Manager deleted stores = %d, want %d", len(app.Agents.DeletedStores()), len(agents.AgentDeletedStores))
	}
	if missing := nilFuncFields(convert.CurrentMihomoIntegrations()); len(missing) > 0 {
		t.Errorf("Mihomo integrations left unset: %v", missing)
	}
	env := convert.CurrentMihomoEnvironment()
	if got, want := nilFuncFields(env), nilFuncFields(mihomoenv.NativeEnv()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Mihomo environment nil fields = %v, want the production builder's %v", got, want)
	}
	for name, set := range map[string]bool{
		"Converter": env.Converter != nil, "ProviderInterval": env.ProviderInterval != nil,
		"LoadSubscriptionCache": env.LoadSubscriptionCache != nil, "LoadSubscriptionCacheTyped": env.LoadSubscriptionCacheTyped != nil,
		"DownloadedText": env.DownloadedText != nil, "Preflight": env.Preflight != nil,
	} {
		if !set {
			t.Errorf("Mihomo environment %s left unset", name)
		}
	}

	// The export sanitizer is the repository's own instance: with every global
	// repository hook cleared, a desensitized export still succeeds and redacts.
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.1","username":"admin","password":"synthetic-router-secret"}]}`))
	}); err != nil {
		t.Fatal(err)
	}
	installed := repo.SetHooks(repo.Hooks{})
	exported, err := r.ExportAllJSON(ctx, true)
	repo.SetHooks(installed)
	if err != nil {
		t.Fatalf("desensitized export without global hooks: %v", err)
	}
	if strings.Contains(string(exported), "synthetic-router-secret") {
		t.Fatal("desensitized export leaked the router password")
	}
	// The router job lock is installed: a connection change is admitted, where
	// a missing lock fails closed with ErrInUse.
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"R1","type":"routeros","endpoint":"http://192.0.2.2","username":"admin","password":"synthetic-router-secret"}]}`))
	}); err != nil {
		t.Fatalf("router connection change with the production job lock: %v", err)
	}

	// Finished rule-source refreshes wake the Agent rule-sync loop of this
	// App's own rule-sync Service through this App's refresh Service, also for
	// a second App in the same process (the subscription is not a once-only
	// registration on an earlier default).
	first := app.RuleSync.Loop().Wake
	first.Take()
	app.Refresh.NotifyRulesRefreshed()
	if !first.Take() {
		t.Fatal("rule refresh did not wake the first App's rule-sync loop")
	}
	second, err := New(ctx, Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Agents.Close)
	wake := second.RuleSync.Loop().Wake
	wake.Take()
	second.Refresh.NotifyRulesRefreshed()
	if !wake.Take() {
		t.Fatal("rule refresh did not wake the second App's rule-sync loop")
	}
}

// TestDeployValidateRejectsMissingCollaborators is the negative control for the
// deploy check New runs: a Service without the HA pair publisher or IPv6
// enrollment is refused at construction instead of failing HA members later.
func TestDeployValidateRejectsMissingCollaborators(t *testing.T) {
	if err := deploy.New(deploy.Options{}).Validate(); err != deploy.ErrPairPublisherMissing {
		t.Fatalf("Validate without pair publisher = %v", err)
	}
	pair := func(context.Context, *agents.Manager, string, *deploy.Bundle, deploy.PublishOptions) (*model.Deployment, bool, error) {
		return nil, false, nil
	}
	if err := deploy.New(deploy.Options{Pair: pair}).Validate(); err != deploy.ErrIPv6EnrollerMissing {
		t.Fatalf("Validate without IPv6 enrollment = %v", err)
	}
}
