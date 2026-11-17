package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func pythonBaseline(t *testing.T) *deploy.Bundle {
	t.Helper()
	raw, err := os.ReadFile("../../../internal/deploy/testdata/prepare.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name     string
		Manifest deploy.Manifest
		Files    map[string]string
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name != "oxidns-empty" {
			continue
		}
		files := map[string]deploy.Content{}
		version := ""
		for _, f := range row.Manifest.Files {
			raw, err := base64.StdEncoding.DecodeString(row.Files[f.Path])
			if err != nil {
				t.Fatal(err)
			}
			files[f.Path] = deploy.Content{Data: raw, Role: f.Role}
			if f.Role == deploy.RoleConfig {
				version = f.SHA256
			}
		}
		b, err := deploy.Pack(row.Manifest, files, version, nil)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	t.Fatal("Python oracle case missing")
	return nil
}

func TestOriginalPythonBaselineRequiresOnePublish(t *testing.T) {
	old := pythonBaseline(t)
	m := old.Manifest()
	files, err := old.Files()
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlx.Parse(files[m.ConfigPath])
	if err != nil {
		t.Fatal(err)
	}
	native, err := deploy.RenderConfig(node, m.ServiceType, false)
	if err != nil {
		t.Fatal(err)
	}
	next, evidence, err := converted(old, native)
	if err != nil || !evidence.YAMLEqual || !evidence.NonMainEqual || !evidence.MainBytesChanged {
		t.Fatal(evidence, err)
	}
	digests, err := deploy.Digests(m)
	if err != nil {
		t.Fatal(err)
	}
	record := jsonx.Obj("id", m.AgentID, "profile_id", m.ProfileID, "service_type", m.ServiceType, "config_version", old.ConfigVersion(), "applied_content", jsonx.Obj("deployment_id", m.DeploymentID, "profile_id", m.ProfileID, "service_type", m.ServiceType, "config_path", m.ConfigPath, "config_version", old.ConfigVersion(), "fingerprint", evidence.OldFingerprint))
	source := deploy.NewSource(jsonx.Obj("profile_id", m.ProfileID), nil)
	checker := contentsync.New(contentsync.Options{Prepare: func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error) {
		return next, nil
	}})
	defer checker.Close()
	if result := checker.CompareNow(context.Background(), deploy.NewTarget(record), source, "https://panel.example.com"); result.Status != "changed" {
		t.Fatal("old Python baseline must show changed", result)
	}
	diff, err := pushdiff.Build(pushdiff.AppliedView{ServiceType: m.ServiceType, Files: digests}, next, &deploy.Receipt{Manifest: m, Bundle: old}, nil)
	if err != nil || diff.Status != "changed" || diff.Counts.Modified != 1 || diff.Counts.Added != 0 || diff.Counts.Removed != 0 || len(diff.Files) != 1 || diff.Config == nil || diff.Config.Status != "formatting" || diff.Config.Total == nil || *diff.Config.Total != 0 || len(diff.Config.Items) != 0 {
		raw, _ := json.Marshal(diff)
		t.Fatal("format-only push-diff lost its exact meaning", string(raw), err)
	}
}

func TestBaselineProofRejectsSemanticAndNonMainMutations(t *testing.T) {
	old := pythonBaseline(t)
	for _, main := range []string{"plugins: []\n", "plugins: [{tag: changed}]\n"} {
		if _, _, err := converted(old, []byte(main)); err == nil {
			t.Fatal("semantic mutation accepted")
		}
	}
	m := old.Manifest()
	files, err := old.Contents()
	if err != nil {
		t.Fatal(err)
	}
	extra := deploy.File{Path: "rules/synthetic.list", Role: deploy.RoleRule, Size: 4, SHA256: hash([]byte("old\n"))}
	m.Files = append(m.Files, extra)
	files[extra.Path] = deploy.Content{Data: []byte("old\n"), Role: extra.Role}
	baseline, err := deploy.Pack(m, files, old.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	extra.SHA256 = hash([]byte("new\n"))
	m.Files[len(m.Files)-1] = extra
	files[extra.Path] = deploy.Content{Data: []byte("new\n"), Role: extra.Role}
	changed, err := deploy.Pack(m, files, old.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = certify(baseline, changed); err == nil {
		t.Fatal("non-main byte mutation accepted")
	}
	delete(files, extra.Path)
	m.Files = m.Files[:len(m.Files)-1]
	changed, err = deploy.Pack(m, files, old.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = certify(baseline, changed); err == nil {
		t.Fatal("missing file accepted")
	}
}
