package deploy

import (
	"archive/zip"
	"bytes"
	"configflow/internal/agents"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/version"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureCopy(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		to := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestPythonPrivateStoresLoad(t *testing.T) {
	root := t.TempDir()
	fixtureCopy(t, "testdata/stores", root)
	raw, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Agent     json.RawMessage
		PreviewID string `json:"preview_id"`
		Public    json.RawMessage
	}
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	var agent model.Agent
	if err = json.Unmarshal(expected.Agent, &agent); err != nil {
		t.Fatal(err)
	}
	r := NewReceipts(root)
	receipt, err := r.LoadAgent(context.Background(), &agent, true)
	if err != nil || receipt == nil {
		t.Fatal(receipt, err)
	}
	if bytes.Contains(receipt.Source, []byte("must-not-be-saved")) {
		t.Fatal("agent credentials retained")
	}
	if stamp, ok := r.Stamp(&agent); !ok || stamp.Size == 0 {
		t.Fatal(stamp, ok)
	}
	p := NewPreviews(root, lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	pv, err := p.Load(expected.PreviewID, "agent-example")
	if err != nil {
		t.Fatal(err)
	}
	public, err := encodeJSON(pv.Public())
	if err != nil || !equalJSON(public, expected.Public) {
		t.Fatalf("public\n%s\nwant\n%s\n%v", public, expected.Public, err)
	}
	archive, err := pv.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range zr.File {
		f, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		want, ok, err2 := pv.File(file.Name)
		if err != nil || err2 != nil || !ok || !bytes.Equal(data, want) {
			t.Fatal(file.Name, err, err2)
		}
	}
	if _, err = p.Load(expected.PreviewID, "foreign"); err == nil {
		t.Fatal("foreign preview read")
	}
	if _, ok, err := pv.File("../record.json"); err != nil || ok {
		t.Fatal(ok, err)
	}
	bundlePath := filepath.Join(root, ".generated-previews", pv.ID, "bundle.tar.gz")
	if err = os.WriteFile(bundlePath, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Load(pv.ID, pv.AgentID); err == nil {
		t.Fatal("tampered preview accepted")
	}
}

func TestPreviewsFrozenIdentityExpiryAndQuota(t *testing.T) {
	ctx := testContext(t)
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	targetRecord := model.Agent{
		ID:          "example",
		Name:        "Original",
		ServiceType: "mihomo",
		ProfileID:   "default",
		Host:        "192.0.2.10",
		Port:        8080,
		Token:       "synthetic-token",
		Enabled:     &enabled,
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = []model.Agent{targetRecord}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	config, revision, err := repository.PublishConfig(ctx, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	src, err := SourceFromJSON(configRaw, &revision)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTypedTarget(&targetRecord)
	if err != nil {
		t.Fatal(err)
	}
	clock := lifecycletest.NewClock(time.Now())
	p := NewPreviews(repository.DataDir(), clock)
	b, err := Build(ctx, BuildInput{Source: src, Target: target, ConfigContent: "mode: rule", DeploymentID: "preview-test"})
	if err != nil {
		t.Fatal(err)
	}
	in := PreviewInput{Target: target, Source: src, Bundle: b, Members: []Target{target}, ConfigRevision: &Revision{Value: revision}}
	pv, err := p.Save(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RequireCurrent(ctx, m, pv); err != nil {
		t.Fatal(err)
	}
	before, _, _ := pv.File("config.yaml")
	var configMap map[string]any
	if err := json.Unmarshal(configRaw, &configMap); err != nil {
		t.Fatal(err)
	}
	configMap["name"] = "management rename"
	if src.inputDigest() != testSource(t, configMap).inputDigest() {
		t.Fatal("metadata changed input digest")
	}
	configMap["unknown_edit"] = true
	if src.inputDigest() == testSource(t, configMap).inputDigest() {
		t.Fatal("content edit not detected")
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Host = "192.0.2.20"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RequireCurrent(ctx, m, pv); err == nil || err.Error() != msgPreviewTargetChanged {
		t.Fatal(err)
	}
	after, _, _ := pv.File("config.yaml")
	if !bytes.Equal(before, after) {
		t.Fatal("draft/target change rerendered preview")
	}
	for range MaxPreviews {
		clock.Advance(time.Second)
		if _, err = p.Save(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = p.Load(pv.ID, target.ID); err == nil {
		t.Fatal("oldest not evicted")
	}
	folders, err := privateFolders(p.root())
	if err != nil || len(folders) != MaxPreviews {
		t.Fatal(len(folders), err)
	}
	clock.Advance(PreviewTTL)
	for _, folder := range folders {
		if _, err = p.Load(filepath.Base(folder.path), target.ID); err == nil {
			t.Fatal("expired preview accepted")
		}
	}
	// Count and byte accounting includes every metadata file; no huge allocation.
	folder := folders[0].path
	f, err := os.Create(filepath.Join(folder, "large-private-source"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxPreviewBytes); err != nil {
		t.Fatal(err)
	}
	f.Close()
	clock.Advance(time.Second)
	if _, err = p.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("metadata quota did not evict")
	}
}

func TestReceiptsCaptureAdvanceAndDefaultProfile(t *testing.T) {
	ctx := testContext(t)
	repository, _ := repo.GetRepository(ctx)
	m, err := agents.New(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "profile-example", Name: "Example"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b := sample(t, "mihomo", 3)
	source := testSource(t, map[string]any{"profile_id": "profile-example", "future": map[string]any{"value": true}, "agents": []any{map[string]any{"token": "must-not-persist"}}})
	shaConfig := ""
	for _, f := range b.manifest.Files {
		if f.Role == RoleConfig {
			shaConfig = f.SHA256
		}
	}
	agent := map[string]any{
		"id": b.manifest.AgentID, "profile_id": b.manifest.ProfileID, "service_type": "mihomo", "config_version": shaConfig,
		"applied_content":   map[string]any{"deployment_id": b.manifest.DeploymentID, "profile_id": b.manifest.ProfileID, "service_type": "mihomo", "config_version": shaConfig},
		"latest_deployment": map[string]any{"deployment_id": b.manifest.DeploymentID, "status": "succeeded"},
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = []model.Agent{*fixtureAgent(agent)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := NewReceipts(repository.DataDir())
	if err = r.Capture(ctx, agent["id"].(string), b, source, "https://panel.example", nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := r.LoadAgent(ctx, fixtureAgent(agent), true)
	if err != nil || loaded == nil {
		t.Fatal(loaded, err)
	}
	if bytes.Contains(loaded.Source, []byte("must-not-persist")) {
		t.Fatal("source credentials persisted")
	}
	updatedFiles, _ := b.Contents()
	updatedFiles["empty.txt"] = Content{[]byte("changed"), RoleCustom}
	manifest := b.Manifest()
	for i, f := range manifest.Files {
		if f.Path == "empty.txt" {
			manifest.Files[i].Size = 7
			manifest.Files[i].SHA256 = digest([]byte("changed"))
		}
	}
	updated, err := Pack(manifest, updatedFiles, b.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := r.AdvanceContent(ctx, m, fixtureAgent(agent), source, updated)
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
	current, err := m.ReadAgent(ctx, agent["id"].(string), true)
	if err != nil {
		t.Fatal(err)
	}
	if current.AppliedContent == nil || current.AppliedContent.Fingerprint == "" {
		t.Fatal(current)
	}
	accepted, err = r.AdvanceContent(ctx, m, fixtureAgent(agent), source, b)
	if err != nil || accepted {
		t.Fatal("stale applied pointer advanced", accepted, err)
	}
	// A profile-less Agent resolves to the repository's non-default-named default.
	_, err = repository.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "alpha", Name: "Alpha"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.SetDefaultProfile(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	alias := cloneMap(t, agent)
	delete(alias, "profile_id")
	alias["applied_content"].(map[string]any)["profile_id"] = "alpha"
	am := b.Manifest()
	am.ProfileID = "alpha"
	ab, err := Pack(am, mustContents(t, b), b.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Capture(ctx, alias["id"].(string), ab, source, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, err := r.LoadAgent(ctx, fixtureAgent(alias), true); err != nil || got == nil {
		t.Fatal("resolved default receipt rejected", got, err)
	}
	repo.SetRepository(repository)
	t.Cleanup(func() { repo.SetRepository(nil) })
	proof := ValidationReceipt{Version: 1, RulesSHA256: RulesFingerprint(), AgentID: alias["id"].(string), DeploymentID: "resolved-default", ProfileID: "alpha", ServiceType: "oxidns", SHA256: strings.Repeat("a", 64), Fingerprint: "v1:" + strings.Repeat("b", 64)}
	raw, _ := encodeJSON(proof)
	var proofMap map[string]any
	if err := json.Unmarshal(raw, &proofMap); err != nil {
		t.Fatal(err)
	}
	alias["service_type"] = "oxidns"
	task := map[string]any{"deployment_id": proof.DeploymentID, "profile_id": "alpha", "service_type": "oxidns", "sha256": proof.SHA256, "prepared_content": map[string]any{"fingerprint": proof.Fingerprint}, "oxidns_validation_receipt": proofMap}
	if !ReceiptMatches(fixtureAgent(alias), fixtureTask(task)) {
		t.Fatal("resolved non-default-named binding proof refused")
	}
	// A stored receipt member the record does not declare is dropped on
	// load (O18 T1b); the declared proof still decides.
	proofMap["unexpected"] = true
	if !ReceiptMatches(fixtureAgent(alias), fixtureTask(task)) {
		t.Fatal("undeclared receipt member changed the proof")
	}
	proofMap["sha256"] = strings.Repeat("c", 64)
	if ReceiptMatches(fixtureAgent(alias), fixtureTask(task)) {
		t.Fatal("changed receipt accepted")
	}

}
func mustContents(t *testing.T, b *Bundle) map[string]Content {
	t.Helper()
	out, err := b.Contents()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRulesProofAndOldCandidateRefusal(t *testing.T) {
	oldVersion := version.Version
	beforeRules := RulesFingerprint()
	version.Version = "changed-build-for-test"
	afterRules := RulesFingerprint()
	version.Version = oldVersion
	if beforeRules == afterRules {
		t.Fatal("RulesFingerprint ignored validator build version")
	}
	if rulesFingerprint("schema-a", "build-a") == rulesFingerprint("schema-b", "build-a") || rulesFingerprint("schema-a", "build-a") == rulesFingerprint("schema-a", "build-b") {
		t.Fatal("schema/build change did not invalidate")
	}
	if len(oxidns.SchemaFingerprint()) != 64 || len(RulesFingerprint()) != 64 {
		t.Fatal("invalid rule digest")
	}
	ctx := testContext(t)
	repository, _ := repo.GetRepository(ctx)
	m, err := agents.New(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	previousHooks := agents.CurrentHooks()
	agents.RegisterHooks(agents.Hooks{ReceiptMatches: ReceiptMatches})
	t.Cleanup(func() { agents.ResetHooks(); agents.RegisterHooks(previousHooks) })
	var callsMu sync.Mutex
	postCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			callsMu.Lock()
			postCalls++
			callsMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"status":"ready"}`))
	}))
	defer srv.Close()
	parts := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")
	source := testSource(t, map[string]any{"profile_id": "default"})
	target := Target{ID: "proof-example", ProfileID: "default", ServiceType: "oxidns"}
	b, err := Build(ctx, BuildInput{Target: target, Source: source, ConfigContent: "plugins: []", DeploymentID: "old-candidate"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ValidateBundle(target.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := encodeJSON(proof)
	var proofMap map[string]any
	if err := json.Unmarshal(raw, &proofMap); err != nil {
		t.Fatal(err)
	}
	sha, _ := b.SHA256()
	fp, _ := Fingerprint(b.manifest)
	task := map[string]any{"deployment_id": b.manifest.DeploymentID, "profile_id": "default", "service_type": "oxidns", "status": "ready", "success": true, "submitted": true, "sha256": sha, "prepared_content": map[string]any{"fingerprint": fp}, "oxidns_validation_receipt": proofMap}
	agent := map[string]any{"id": target.ID, "profile_id": "default", "service_type": "oxidns", "host": parts[0], "port": parts[1], "token": "synthetic-token", "deployments": map[string]any{b.manifest.DeploymentID: task}, "latest_deployment": cloneMap(t, task)}
	if !ReceiptMatches(fixtureAgent(agent), fixtureTask(task)) {
		t.Fatal("valid proof refused")
	}
	proofMap["rules_sha256"] = "python-era-proof"
	if ReceiptMatches(fixtureAgent(agent), fixtureTask(task)) {
		t.Fatal("old validator proof accepted")
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = []model.Agent{*fixtureAgent(agent)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.ActivateCandidateModel(ctx, target.ID, b.manifest.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || textValue(result.Message) != agents.MessageUncheckedCandidate || result.HTTPStatus == nil || int(*result.HTTPStatus) != 409 {
		t.Fatal(result)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if postCalls != 0 {
		t.Fatal("old candidate activated")
	}
	pubRaw, err := json.Marshal(PublicDeployment(fixtureTask(task)))
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(pubRaw, &public); err != nil {
		t.Fatal(err)
	}
	if _, hasPrepared := public["prepared_content"]; hasPrepared {
		t.Fatal("private proof exposed")
	}
	if _, hasReceipt := public["oxidns_validation_receipt"]; hasReceipt {
		t.Fatal("private proof exposed")
	}
}

func TestFinalizeBundleUsesFrozenDeviceBytes(t *testing.T) {
	ctx := testContext(t)
	repository, _ := repo.GetRepository(ctx)
	m, err := agents.New(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/capabilities" {
			_, _ = w.Write([]byte(`{"service_type":"mihomo","final_config_protocols":[1],"config_path":"installed.yaml"}`))
			return
		}
		var body struct{ Content string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(struct {
			Success bool   `json:"success"`
			Content string `json:"content"`
		}{true, body.Content + "x-device-rendered: true\n"})
	}))
	defer srv.Close()
	parts := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")
	target := testTarget(t, map[string]any{"id": "agent-example", "service_type": "mihomo", "host": parts[0], "port": parts[1], "token": "synthetic-token"})
	b := sample(t, "mihomo", 3)
	original, err := Fingerprint(b.manifest)
	if err != nil {
		t.Fatal(err)
	}
	final, err := FinalizeBundle(ctx, m, target, b, testSource(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !final.manifest.FinalConfig || final.manifest.ConfigPath != "installed.yaml" {
		t.Fatal(final.Manifest())
	}
	fp, err := Fingerprint(ComparisonManifest(final.Manifest(), final.Comparison()))
	if err != nil || fp != original {
		t.Fatal(fp, original, err)
	}
	files, _ := final.Files()
	if !bytes.Contains(files["installed.yaml"], []byte("x-device-rendered")) {
		t.Fatal(files)
	}
}

func TestIdentityUnknownFieldsAndSettingsCanonical(t *testing.T) {
	a := testTarget(t, map[string]any{"id": "example", "token": "synthetic-token", "port": 8080})
	b := a
	b.Record = fixtureAgent(a.Record)
	b.Record.ConfigVersion = "heartbeat"
	if !bytes.Equal(AgentIdentity(a), AgentIdentity(b)) {
		t.Fatal("heartbeat changed target identity")
	}
	b.Record.Token = "rotated-token"
	if bytes.Equal(AgentIdentity(a), AgentIdentity(b)) {
		t.Fatal("rotation ignored")
	}
	if SettingsDigest(json.RawMessage(`{"b":{"x":1},"a":2}`)) != SettingsDigest(json.RawMessage(`{"a":2,"b":{"x":1}}`)) {
		t.Fatal("noncanonical native digest")
	}
	if reflect.DeepEqual(AgentIdentity(a), AgentIdentity(b)) {
		t.Fatal("identities equal")
	}
}

func TestLegacyPreviewFreezesSurgeText(t *testing.T) {
	clock := lifecycletest.NewClock(time.Now())
	p := NewPreviews(t.TempDir(), clock)
	in := PreviewInput{Target: Target{ID: "surge-example", ServiceType: "surge"}, Source: testSource(t, map[string]any{"profile_id": "default"}), ConfigContent: "[General]\nloglevel = info\n"}
	pv, err := p.Save(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Bundle != nil || pv.Public().ConfigPath != "surge.conf" || pv.Public().Content != in.ConfigContent {
		t.Fatal(pv.Public())
	}
	data, ok, err := pv.File("surge.conf")
	if err != nil || !ok || string(data) != in.ConfigContent {
		t.Fatal(string(data), ok, err)
	}
}
