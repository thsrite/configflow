package repo

// Port of backend/test_config_repository.py: persistence boundaries,
// independent writers and recoverable imports.

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
)

// rcDefaultRepo is ProfileRepository(root, get_default_config).
func rcDefaultRepo(t *testing.T, dataDir string) *Repository {
	t.Helper()
	return must(Open(dataDir, Options{DefaultSettings: DefaultConfig}))(t)
}

// rcSQL runs statements on the database behind the repository's back.
func rcSQL(t testing.TB, path string, statements ...string) {
	t.Helper()
	db := must(sql.Open("sqlite", "file:"+path))(t)
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

// rcPragma reads one pragma through a plain connection.
func rcPragma(t testing.TB, path, statement string) string {
	t.Helper()
	db := must(sql.Open("sqlite", "file:"+path))(t)
	defer db.Close()
	var value any
	noError(t, db.QueryRow(statement).Scan(&value))
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	case int64:
		return strconv.FormatInt(typed, 10)
	}
	t.Fatalf("unexpected pragma value %T", value)
	return ""
}

func rcLegacyObj(t testing.TB, hostname string) *testObj {
	t.Helper()
	// Settings only: the profile record always writes (empty) metadata.
	data := parseJSONObj(t, must(configSettingsJSON(DefaultConfig()))(t))
	extra := parseJSONObj(t, []byte(strings.ReplaceAll(`{
		"subscriptions": [{"id": "sub", "name": "Source", "url": "https://HOST/subscription"}],
		"nodes": [{"id": "node", "name": "Node", "server": "HOST", "type": "socks5", "port": 1080}],
		"subscription_aggregations": [{"id": "agg", "name": "Aggregate", "subscriptions": ["sub"], "nodes": ["node"]}],
		"rule_library": [{"id": "lib", "name": "Domains", "source_type": "content", "content": "HOST", "behavior": "domain"}],
		"proxy_groups": [{"id": "group", "name": "Proxy", "type": "select", "subscriptions": ["sub"], "manual_nodes": ["node", "DIRECT"], "aggregations": ["agg"]}],
		"rule_configs": [
			{"id": "r1", "itemType": "ruleset", "library_rule_id": "lib", "name": "Domains", "behavior": "domain", "policy": "DIRECT"},
			{"id": "r2", "itemType": "rule", "rule_type": "MATCH", "policy": "Proxy"}
		]}`, "HOST", hostname)))
	for key, value := range extra.All() {
		data.Set(key, value)
	}
	data.GetObject("mosdns").Set("direct_rulesets", []any{"r1"})
	return data
}

func TestOriginalInlineRulesBecomeSharedSourcesWithoutChangingPolicies(t *testing.T) {
	legacy := rcLegacyObj(t, "one.example")
	inline := parseJSONObj(t, []byte(`{"id": "inline", "itemType": "ruleset", "name": "Domains",
		"content": "private.example", "behavior": "domain", "policy": "Proxy", "enabled": false}`))
	legacy.Set("rule_configs", append(legacy.GetList("rule_configs"), inline))
	r := rcDefaultRepo(t, t.TempDir())
	must(r.ImportAllJSON(bg, mustMarshalJSON(t, legacy)))(t)
	resolved := parseJSONObj(t, must(model.Marshal(must(r.Config(bg, "default", nil))(t)))(t))
	rules := map[string]*testObj{}
	for _, item := range resolved.GetList("rule_configs") {
		rule := item.(*testObj)
		rules[rule.GetString("id")] = rule
	}
	wantJSONEqual(t, rules["r1"].Value("content"), "one.example")
	wantJSONEqual(t, rules["inline"].Value("content"), "private.example")
	wantJSONEqual(t, rules["inline"].Value("policy"), "Proxy")
	wantJSONEqual(t, rules["inline"].Value("enabled"), false)
	if rules["inline"].GetString("library_rule_id") == rules["r1"].GetString("library_rule_id") {
		t.Fatal("inline rule shares the library entry of r1")
	}
	stored := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t)).GetList("rule_configs")
	last := stored[len(stored)-1].(*testObj)
	if _, ok := last.Get("content"); ok {
		t.Fatal("content persisted in the profile")
	}
}

func TestUnsupportedImportPreservesExistingDataAndOriginalFile(t *testing.T) {
	r := rcDefaultRepo(t, t.TempDir())
	must(r.ImportAllJSON(bg, mustMarshalJSON(t, rcLegacyObj(t, "one.example"))))(t)
	before := must(r.Store().Fingerprint())(t)
	invalid := parseJSONObj(t, must(r.ExportAllJSON(bg, false))(t))
	invalid.Set("schema_version", 2)
	_, err := r.ImportAllJSON(bg, mustMarshalJSON(t, invalid))
	wantError(t, err, ErrValidation, "")
	if must(r.Store().Fingerprint())(t) != before {
		t.Fatal("database changed")
	}
}

func TestIndependentConcurrentProfileWritesAndSharedUpdatesSurvive(t *testing.T) {
	root := t.TempDir()
	r := rcDefaultRepo(t, root)
	must(r.CreateProfileRecord(bg, ProfileCreation{Name: "Second", ID: "second"}, ""))(t)
	otherWriter := rcDefaultRepo(t, root)
	one := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t))
	two := parseJSONObj(t, must(otherWriter.ExportProfileJSON(bg, "second"))(t))
	one.GetObject("mihomo").Set("custom_config", "mixed-port: 7901")
	two.GetObject("mihomo").Set("custom_config", "mixed-port: 7902")
	oneRaw := mustMarshalJSON(t, one)
	twoRaw := mustMarshalJSON(t, two)
	shared := must(r.Shared(bg))(t)
	shared.Nodes = []model.Node{decodeJSON[model.Node](t, `{"id": "node", "name": "Node", "server": "new.example", "port": 1080, "type": "socks5"}`)}
	var group sync.WaitGroup
	errs := make([]error, 3)
	jobs := []func() error{
		func() error { _, _, err := r.SaveProfileFieldsJSON(bg, "default", oneRaw, nil); return err },
		func() error { _, _, err := otherWriter.SaveProfileFieldsJSON(bg, "second", twoRaw, nil); return err },
		func() error { _, err := r.SaveSharedRecord(bg, shared, nil); return err },
	}
	for index, job := range jobs {
		group.Add(1)
		go func() { defer group.Done(); errs[index] = job() }()
	}
	group.Wait()
	for _, err := range errs {
		noError(t, err)
	}
	if got := must(r.Config(bg, "default", nil))(t).Profile.Mihomo.CustomConfig; got != "mixed-port: 7901" {
		t.Fatalf("default custom_config %q", got)
	}
	if got := must(r.Config(bg, "second", nil))(t).Profile.Mihomo.CustomConfig; got != "mixed-port: 7902" {
		t.Fatalf("second custom_config %q", got)
	}
	if got := must(r.Shared(bg))(t).Nodes[0].Server; got != "new.example" {
		t.Fatalf("node server %q", got)
	}
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", oneRaw, nil)
	wantError(t, err, ErrInUse, "")
}

func TestImportIsWholeSystemAtomicAndInvalidatesOldSnapshots(t *testing.T) {
	r := rcDefaultRepo(t, t.TempDir())
	must(r.ImportAllJSON(bg, mustMarshalJSON(t, rcLegacyObj(t, "one.example"))))(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{Name: "Second", ID: "second"}, "default"))(t)
	system := must(r.System(bg))(t)
	system.Agents = []model.Agent{decodeJSON[model.Agent](t, `{"id": "agent", "profile_id": "second", "token": "secret"}`)}
	noError(t, system.SetJSON("backup", json.RawMessage(`{"webdav_password": "restore-me"}`)))
	must(r.SaveSystemRecord(bg, system, nil))(t)
	backupRaw := must(r.ExportAllJSON(bg, false))(t)
	backupObj := parseJSONObj(t, backupRaw)
	staleRaw := must(r.ExportProfileJSON(bg, "default"))(t)
	changed := parseJSONObj(t, must(r.ExportProfileJSON(bg, "second"))(t))
	changed.GetObject("mihomo").Set("custom_config", "mixed-port: 9900")
	_, _, err := r.SaveProfileFieldsJSON(bg, "second", mustMarshalJSON(t, changed), nil)
	noError(t, err)
	invalid := parseJSONObj(t, backupRaw)
	invalid.GetObject("profiles").GetObject("second").GetList("proxy_groups")[0].(*testObj).Set("manual_nodes", []any{"missing"})
	before := must(r.ExportAllJSON(bg, false))(t)
	_, err = r.ImportAllJSON(bg, mustMarshalJSON(t, invalid))
	wantError(t, err, ErrInUse, "")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
	must(r.ImportAllJSON(bg, backupRaw))(t)
	wantJSONEqual(t, parseJSONObj(t, must(r.ExportProfileJSON(bg, "second"))(t)).Value("mihomo"),
		backupObj.GetObject("profiles").GetObject("second").Value("mihomo"))
	gotSys := parseJSONObj(t, must(model.Marshal(must(r.System(bg))(t)))(t))
	wantJSONEqual(t, gotSys.Value("agents"), backupObj.GetObject("system").Value("agents"))
	wantJSONEqual(t, gotSys.GetObject("backup").Value("webdav_password"), "restore-me")
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", staleRaw, nil)
	wantError(t, err, ErrInUse, "")
}

func TestProfileOnlyAcceptsReferenceIdsAndNeverPersistsSourceCopies(t *testing.T) {
	r := rcDefaultRepo(t, t.TempDir())
	must(r.ImportAllJSON(bg, mustMarshalJSON(t, rcLegacyObj(t, "one.example"))))(t)
	snapshot := parseJSONObj(t, must(model.Marshal(must(r.Config(bg, "default", nil))(t)))(t))
	snapshot.GetList("nodes")[0].(*testObj).Set("server", "override.example")
	snapshot.GetList("rule_configs")[0].(*testObj).Set("content", "override.example")
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", mustMarshalJSON(t, snapshot), nil)
	noError(t, err)
	resolved := parseJSONObj(t, must(model.Marshal(must(r.Config(bg, "default", nil))(t)))(t))
	wantJSONEqual(t, resolved.GetList("nodes")[0].(*testObj).Value("server"), "one.example")
	wantJSONEqual(t, resolved.GetList("rule_configs")[0].(*testObj).Value("content"), "one.example")
	raw := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t))
	if _, hasNodes := raw.Get("nodes"); hasNodes {
		t.Fatal("source copies persisted")
	}
	if _, hasContent := raw.GetList("rule_configs")[0].(*testObj).Get("content"); hasContent {
		t.Fatal("source copies persisted")
	}
	raw.GetList("proxy_groups")[0].(*testObj).Set("manual_nodes", []any{testObjKV("id", "node", "server", "override.example")})
	_, _, err = r.SaveProfileFieldsJSON(bg, "default", mustMarshalJSON(t, raw), nil)
	wantError(t, err, ErrValidation, "")
}

func TestSharedDeletionAndDisableReportsAllProfileUsage(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		shared.Nodes = append(shared.Nodes, model.Node{ID: "n", Name: "Node"})
		return nil
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{"proxy_groups": [
		{"id": "choice", "name": "Choice", "type": "select", "manual_nodes": ["n"]}]}`), nil)
	noError(t, err)
	must(r.CloneProfileRecord(bg, "default", ProfileCreation{ID: "other", Name: "Other"}))(t)
	off := false
	updaters := []func(context.Context, *model.Shared) error{
		func(_ context.Context, shared *model.Shared) error { shared.Nodes = []model.Node{}; return nil },
		func(_ context.Context, shared *model.Shared) error { shared.Nodes[0].Enabled = &off; return nil },
	}
	for _, updater := range updaters {
		err := r.UpdateShared(bg, updater)
		wantError(t, err, ErrInUse, "")
		ids := map[string]bool{}
		for _, usage := range UsagesOf(err) {
			ids[parseJSONObj(t, mustMarshalJSON(t, usage)).GetString("id")] = true
		}
		if len(ids) != 2 || !ids["default"] || !ids["other"] {
			t.Fatalf("usages %v", UsagesOf(err))
		}
	}
}

func TestSystemTransactionsValidateNestedProfileReadsAndBindings(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "bound", Name: "Bound"}, ""))(t)
	noError(t, r.UpdateSystem(bg, func(ctx context.Context, system *model.System) error {
		metadata, err := r.ProfileMetadataRecord(ctx, "bound")
		if err != nil {
			return err
		}
		if metadata.Name != "Bound" {
			t.Fatalf("metadata name %q", metadata.Name)
		}
		system.Agents = append(system.Agents, model.Agent{ID: "a", Name: "Agent", ProfileID: "bound"})
		return nil
	}))
	wantError(t, r.DeleteProfile(bg, "bound", ""), ErrInUse, "")
	before := must(r.ExportAllJSON(bg, false))(t)
	err := r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents[0].ProfileID = "missing"
		return nil
	})
	wantError(t, err, ErrValidation, "")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
	sysObj := parseJSONObj(t, must(model.Marshal(must(r.System(bg))(t)))(t))
	if !reflect.DeepEqual(sysObj.Keys(), []string{"system_config", "backup", "agents", "_revision"}) {
		t.Fatalf("system keys %v", sysObj.Keys())
	}
}

func TestSharedCachePathsAreAtomicAndRejectEscape(t *testing.T) {
	r := newRepo(t)
	must(r.WriteSharedJSON(bg, "subscribes/sub.json", map[string]any{"nodes": []any{}}))(t)
	wantJSONEqual(t, must(r.ReadSharedBytes(bg, "subscribes/sub.json"))(t), json.RawMessage(`{"nodes":[]}`))
	must(r.WriteSharedText(bg, "rules/domains.txt", []byte("example.com")))(t)
	text := must(readFileString(must(r.SharedRulesDir())(t) + "/domains.txt"))(t)
	if text != "example.com" {
		t.Fatalf("content %q", text)
	}
	for _, path := range []string{"../escape.json", "/tmp/escape.json", ""} {
		_, err := r.WriteSharedJSON(bg, path, map[string]any{})
		wantError(t, err, ErrValidation, "")
	}
}

func TestGlobalSettingsThreeWayMergePreservesConcurrentAgentUpdates(t *testing.T) {
	r := newRepo(t)
	baseline := must(r.System(bg))(t)
	snapshot := must(r.System(bg))(t)
	snapshot.SystemConfig.ServerDomain = "https://configured.example"
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = append(system.Agents, decodeJSON[model.Agent](t, `{"id": "a", "profile_id": "default"}`))
		return nil
	}))
	must(r.SaveSystemRecord(bg, snapshot, baseline))(t)
	system := must(r.System(bg))(t)
	wantJSONEqual(t, must(model.Marshal(system.Agents))(t), json.RawMessage(`[{"id": "a", "profile_id": "default"}]`))
	if system.SystemConfig.ServerDomain != "https://configured.example" {
		t.Fatalf("server_domain %q", system.SystemConfig.ServerDomain)
	}
}

func rcRuleDNSRepository(t *testing.T) *Repository {
	t.Helper()
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("rule_library", json.RawMessage(`[
			{"id": "lib", "name": "Ads", "source_type": "content", "content": "ads.test", "behavior": "domain"}]`))
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{"rule_configs": [
		{"id": "rule", "itemType": "rule", "rule_type": "DOMAIN", "value": "a.test", "policy": "DIRECT"},
		{"id": "set", "itemType": "ruleset", "library_rule_id": "lib", "policy": "REJECT"}]}`), nil)
	noError(t, err)
	must(r.CloneProfileRecord(bg, "default", ProfileCreation{ID: "other", Name: "Other"}))(t)
	return r
}

func rcOxidnsUpdate(t *testing.T, fields string) func(context.Context, *model.Profile) error {
	t.Helper()
	extra := parseJSONObj(t, []byte(fields))
	return func(_ context.Context, profile *model.Profile) error {
		raw, _, err := recordMemberJSON(profile, "oxidns")
		if err != nil {
			return err
		}
		obj := parseJSONObj(t, raw)
		for k, v := range extra.All() {
			obj.Set(k, v)
		}
		return profile.SetJSON("oxidns", mustMarshalJSON(t, obj))
	}
}

func TestSharedAndProfilesTransactionCommitsOnce(t *testing.T) {
	r := rcRuleDNSRepository(t)
	sharedRevision := must(r.Shared(bg))(t).Revision
	revisions := map[string]int64{}
	for _, profileID := range []string{"default", "other"} {
		revisions[profileID] = must(r.Profile(bg, profileID))(t).Revision
	}
	seen := map[string][2][]string{}
	add := func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = append(shared.RuleLibrary, decodeJSON[model.RuleLibraryItem](t, `{"id": "new", "name": "New", "source_type": "content",
			"content": "new.test", "behavior": "classical"}`))
		return nil
	}
	ruleLibIDs := func(items []model.RuleLibraryItem) []string {
		ids := make([]string, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		return ids
	}
	compose := func(_ context.Context, profileID string, before, after *model.Config) error {
		// before is resolved against the shared data as it was, after against the new one.
		seen[profileID] = [2][]string{ruleLibIDs(before.RuleLibrary), ruleLibIDs(after.RuleLibrary)}
		rule := decodeJSON[model.RuleConfig](t, `{"id": "ref", "itemType": "ruleset", "library_rule_id": "new", "policy": "DIRECT", "behavior": "classical"}`)
		after.Profile.RuleConfigs = append([]model.RuleConfig{rule}, after.Profile.RuleConfigs...)
		return nil
	}
	selector := func(_ *model.Shared, profiles []*model.Profile) ([]string, error) {
		var ids []string
		for _, p := range profiles {
			if p.ID == "other" {
				ids = append(ids, p.ID)
			}
		}
		return ids, nil
	}
	err := r.UpdateSharedConfigs(bg, add, compose, selector)
	noError(t, err)
	if len(seen) != 1 {
		t.Fatalf("seen %v", seen)
	}
	if !reflect.DeepEqual(seen["other"][0], []string{"lib"}) || !reflect.DeepEqual(seen["other"][1], []string{"lib", "new"}) {
		t.Fatalf("seen %v", seen["other"])
	}
	if got := must(r.Shared(bg))(t).Revision; got != sharedRevision+1 {
		t.Fatalf("shared revision %d", got)
	}
	if got := must(r.Profile(bg, "other"))(t).Revision; got != revisions["other"]+1 {
		t.Fatalf("other revision %d", got)
	}
	if got := must(r.Profile(bg, "default"))(t).Revision; got != revisions["default"] {
		t.Fatalf("default revision %d", got)
	}
	stored := parseJSONObj(t, must(r.ExportProfileJSON(bg, "other"))(t)).GetList("rule_configs")[0]
	wantJSONEqual(t, stored, json.RawMessage(`{"id": "ref", "itemType": "ruleset", "library_rule_id": "new", "policy": "DIRECT"}`))
	resolved := parseJSONObj(t, must(model.Marshal(must(r.Config(bg, "other", nil))(t)))(t)).GetList("rule_configs")[0].(*testObj)
	wantJSONEqual(t, resolved.Value("behavior"), "classical")
	if !strings.HasSuffix(resolved.GetString("url"), "/rule-library/content/new") {
		t.Fatalf("url %q", resolved.GetString("url"))
	}
}

func TestSharedAndProfilesTransactionRollsBackAll(t *testing.T) {
	r := rcRuleDNSRepository(t)
	before := must(r.ExportAllJSON(bg, false))(t)
	rename := func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary[0].Name = "Renamed"
		return nil
	}
	fail := func(_ context.Context, profileID string, _, after *model.Config) error {
		after.Profile.RuleConfigs = []model.RuleConfig{}
		if profileID == "other" {
			return NewValidationError("boom")
		}
		return nil
	}
	selectBoth := func(*model.Shared, []*model.Profile) ([]string, error) { return []string{"default", "other"}, nil }
	err := r.UpdateSharedConfigs(bg, rename, fail, selectBoth)
	wantError(t, err, ErrValidation, "boom")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
	// Whole-document validation also rejects everything at once.
	follow := func(_ context.Context, _ string, _, after *model.Config) error {
		return after.Profile.SetJSON("oxidns", json.RawMessage(`{"rule_dns_follow":["missing"]}`))
	}
	selectDefault := func(*model.Shared, []*model.Profile) ([]string, error) { return []string{"default"}, nil }
	err = r.UpdateSharedConfigs(bg, rename, follow, selectDefault)
	wantError(t, err, ErrInUse, "")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
}

func TestRuleDnsIdsMustExist(t *testing.T) {
	r := rcRuleDNSRepository(t)
	_, err := r.UpdateProfile(bg, "default", rcOxidnsUpdate(t, `{
		"rule_actions": {"set": {"action": "blackhole"}}, "rule_dns_follow": ["rule"],
		"rule_dns_parked": {"rule": {"action": "direct"}}}`))
	noError(t, err)
	for _, fields := range []string{
		`{"rule_actions": {"gone": {"action": "refused"}}}`,
		`{"rule_dns_follow": ["gone"]}`,
		`{"rule_dns_parked": {"gone": {"action": "proxy"}}}`,
	} {
		_, err := r.UpdateProfile(bg, "default", rcOxidnsUpdate(t, fields))
		wantError(t, err, ErrInUse, "OxiDNS DNS 处理仍引用不存在的规则 gone")
	}
	_, err = r.UpdateProfile(bg, "default", rcOxidnsUpdate(t, `{"rule_actions": "x"}`))
	wantError(t, err, ErrValidation, "")
}

func TestRuleDnsActionShapesAreValidated(t *testing.T) {
	r := rcRuleDNSRepository(t)
	for _, entry := range [][2]string{
		{"rule_actions", `{"set": {"kind": "blackhole"}}`},
		{"rule_actions", `{"set": "reject"}`},
		{"rule_actions", `{"set": {"action": "direct"}}`},
		{"rule_actions", `{"set": {"action": "upstream", "upstream": {"addr": "1.1.1.1"}}}`},
		{"rule_dns_parked", `{"rule": {"action": "drop"}}`},
	} {
		_, err := r.UpdateProfile(bg, "default", rcOxidnsUpdate(t, `{"`+entry[0]+`": `+entry[1]+`}`))
		wantError(t, err, ErrValidation, "OxiDNS "+entry[0]+" 中规则")
	}
	// Imports skip the settings normalizer and must be refused the same way.
	exported := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t))
	exported.GetObject("oxidns").Set("rule_actions", parseJSONObj(t, []byte(`{"set": {"kind": "blackhole"}}`)))
	_, err := r.ImportProfileJSON(bg, "default", mustMarshalJSON(t, exported))
	wantError(t, err, ErrValidation, "")
	oxidns := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t)).GetObject("oxidns")
	if val, ok := oxidns.Get("rule_actions"); ok {
		wantJSONEqual(t, val, json.RawMessage(`{}`))
	}
}

// The route half of test_every_delete_path_cleans_rule_dns_fields (DELETE
// /api/rules/<id>) belongs to the rules routes package; this is the import
// half.
func TestEveryDeletePathCleansRuleDnsFields(t *testing.T) {
	r := rcRuleDNSRepository(t)
	fields := `{"rule_actions": {"set": {"action": "blackhole"}}, "rule_dns_follow": ["rule"], "rule_dns_parked": {}}`
	for _, profileID := range []string{"default", "other"} {
		_, err := r.UpdateProfile(bg, profileID, rcOxidnsUpdate(t, fields))
		noError(t, err)
	}
	// An import replacing the composition drops the DNS entries of rules it lacks.
	exported := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t))
	kept := []any{}
	for _, item := range exported.GetList("rule_configs") {
		if item.(*testObj).GetString("id") != "set" {
			kept = append(kept, item)
		}
	}
	exported.Set("rule_configs", kept)
	exported.Delete("oxidns")
	must(r.ImportProfileJSON(bg, "default", mustMarshalJSON(t, exported)))(t)
	oxidns := parseJSONObj(t, must(r.ExportProfileJSON(bg, "default"))(t)).GetObject("oxidns")
	wantJSONEqual(t, oxidns.Value("rule_actions"), json.RawMessage(`{}`))
	wantJSONEqual(t, oxidns.Value("rule_dns_follow"), []any{"rule"})
}
