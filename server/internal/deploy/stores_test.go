package deploy

import (
	"archive/zip"
	"bytes"
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle/lifecycletest"
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
	agentValue, err := jsonx.Parse(expected.Agent)
	if err != nil {
		t.Fatal(err)
	}
	agent := agentValue.(*jsonx.Object)
	r := NewReceipts(root)
	receipt, err := r.Load(context.Background(), agent, true)
	if err != nil || receipt == nil {
		t.Fatal(receipt, err)
	}
	if bytes.Contains(receipt.Source, []byte("must-not-be-saved")) {
		t.Fatal("agent credentials retained")
	}
	if stamp, ok := r.Stamp(agent); !ok || stamp.Size == 0 {
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
	targetRecord := jsonx.Obj("id", "example", "name", "Original", "service_type", "mihomo", "profile_id", "default", "host", "192.0.2.10", "port", 8080, "token", "synthetic-token", "enabled", true)
	_, err = repository.UpdateSystemTransaction(ctx, func(ctx context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.Set("agents", []any{targetRecord})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	config, revision, err := repository.GetPublishSnapshot(ctx, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	src := NewSource(config, &revision)
	target := NewTarget(targetRecord)
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
	config.Set("name", "management rename")
	if src.inputDigest() != NewSource(config, nil).inputDigest() {
		t.Fatal("metadata changed input digest")
	}
	config.Set("unknown_edit", true)
	if src.inputDigest() == NewSource(config, nil).inputDigest() {
		t.Fatal("content edit not detected")
	}
	_, err = repository.UpdateSystemTransaction(ctx, func(ctx context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetList("agents")[0].(*jsonx.Object).Set("host", "192.0.2.20")
		return nil, nil
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
	_, err = repository.CreateProfile(ctx, jsonx.Obj("id", "profile-example", "name", "Example"), "")
	if err != nil {
		t.Fatal(err)
	}
	b := sample(t, "mihomo", 3)
	source := NewSource(jsonx.Obj("profile_id", "profile-example", "future", jsonx.Obj("value", true), "agents", []any{jsonx.Obj("token", "must-not-persist")}), nil)
	shaConfig := ""
	for _, f := range b.manifest.Files {
		if f.Role == RoleConfig {
			shaConfig = f.SHA256
		}
	}
	agent := jsonx.Obj("id", b.manifest.AgentID, "profile_id", b.manifest.ProfileID, "service_type", "mihomo", "config_version", shaConfig, "applied_content", jsonx.Obj("deployment_id", b.manifest.DeploymentID, "profile_id", b.manifest.ProfileID, "service_type", "mihomo", "config_version", shaConfig), "latest_deployment", jsonx.Obj("deployment_id", b.manifest.DeploymentID, "status", "succeeded"))
	_, err = repository.UpdateSystemTransaction(ctx, func(ctx context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.Set("agents", []any{agent.DeepCopy()})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := NewReceipts(repository.DataDir())
	if err = r.Capture(ctx, agent.GetString("id"), b, source, "https://panel.example", nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := r.Load(ctx, agent, true)
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
	accepted, err := r.AdvanceContent(ctx, m, agent, source, updated)
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
	current, err := m.GetAgentByID(ctx, agent.GetString("id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if current.GetObject("applied_content").GetString("fingerprint") == "" {
		t.Fatal(current)
	}
	accepted, err = r.AdvanceContent(ctx, m, agent, source, b)
	if err != nil || accepted {
		t.Fatal("stale applied pointer advanced", accepted, err)
	}
	// A profile-less Agent resolves to the repository's non-default-named default.
	_, err = repository.CreateProfile(ctx, jsonx.Obj("id", "alpha", "name", "Alpha"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.SetDefaultProfile(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	alias := agent.DeepCopy()
	alias.Delete("profile_id")
	alias.GetObject("applied_content").Set("profile_id", "alpha")
	am := b.Manifest()
	am.ProfileID = "alpha"
	ab, err := Pack(am, mustContents(t, b), b.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Capture(ctx, alias.GetString("id"), ab, source, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Load(ctx, alias, true); err != nil || got == nil {
		t.Fatal("resolved default receipt rejected", got, err)
	}
	repo.SetRepository(repository)
	t.Cleanup(func() { repo.SetRepository(nil) })
	proof := ValidationReceipt{Version: 1, RulesSHA256: RulesFingerprint(), AgentID: alias.GetString("id"), DeploymentID: "resolved-default", ProfileID: "alpha", ServiceType: "oxidns", SHA256: strings.Repeat("a", 64), Fingerprint: "v1:" + strings.Repeat("b", 64)}
	raw, _ := encodeJSON(proof)
	value, _ := jsonx.Parse(raw)
	alias.Set("service_type", "oxidns")
	task := jsonx.Obj("deployment_id", proof.DeploymentID, "profile_id", "alpha", "service_type", "oxidns", "sha256", proof.SHA256, "prepared_content", jsonx.Obj("fingerprint", proof.Fingerprint), "oxidns_validation_receipt", value)
	if !ReceiptMatches(alias, task) {
		t.Fatal("resolved non-default-named binding proof refused")
	}
	value.(*jsonx.Object).Set("unexpected", true)
	if ReceiptMatches(alias, task) {
		t.Fatal("extra receipt field accepted")
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
	Wire()
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
	source := NewSource(jsonx.Obj("profile_id", "default"), nil)
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
	value, _ := jsonx.Parse(raw)
	sha, _ := b.SHA256()
	fp, _ := Fingerprint(b.manifest)
	task := jsonx.Obj("deployment_id", b.manifest.DeploymentID, "profile_id", "default", "service_type", "oxidns", "status", "ready", "success", true, "submitted", true, "sha256", sha, "prepared_content", jsonx.Obj("fingerprint", fp), "oxidns_validation_receipt", value)
	agent := jsonx.Obj("id", target.ID, "profile_id", "default", "service_type", "oxidns", "host", parts[0], "port", parts[1], "token", "synthetic-token", "deployments", jsonx.Obj(b.manifest.DeploymentID, task), "latest_deployment", task.DeepCopy())
	if !ReceiptMatches(agent, task) {
		t.Fatal("valid proof refused")
	}
	task.GetObject("oxidns_validation_receipt").Set("rules_sha256", "python-era-proof")
	if ReceiptMatches(agent, task) {
		t.Fatal("old validator proof accepted")
	}
	_, err = repository.UpdateSystemTransaction(ctx, func(ctx context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.Set("agents", []any{agent})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.ActivateDeployment(ctx, target.ID, b.manifest.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetString("status") != "failed" || result.GetString("message") != UncheckedCandidateMessage || !jsonx.Equal(result.Value("http_status"), 409) {
		t.Fatal(result)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if postCalls != 0 {
		t.Fatal("old candidate activated")
	}
	public := PublicDeployment(task)
	if public.Has("prepared_content") || public.Has("oxidns_validation_receipt") {
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
	target := NewTarget(jsonx.Obj("id", "agent-example", "service_type", "mihomo", "host", parts[0], "port", parts[1], "token", "synthetic-token"))
	b := sample(t, "mihomo", 3)
	original, err := Fingerprint(b.manifest)
	if err != nil {
		t.Fatal(err)
	}
	final, err := FinalizeBundle(ctx, m, target, b, NewSource(jsonx.NewObject(), nil))
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
	a := NewTarget(jsonx.Obj("id", "example", "token", "synthetic-token", "port", 8080))
	b := a
	b.Record = a.Record.DeepCopy()
	b.Record.Set("config_version", "heartbeat")
	if !bytes.Equal(AgentIdentity(a), AgentIdentity(b)) {
		t.Fatal("heartbeat changed target identity")
	}
	b.Record.Set("token", "rotated-token")
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
	in := PreviewInput{Target: Target{ID: "surge-example", ServiceType: "surge"}, Source: NewSource(jsonx.Obj("profile_id", "default"), nil), ConfigContent: "[General]\nloglevel = info\n"}
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
