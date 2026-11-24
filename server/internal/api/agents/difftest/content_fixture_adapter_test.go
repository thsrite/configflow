package difftest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func contentDiffBundle(t *testing.T, changed bool) *deploy.Bundle {
	t.Helper()
	main, rules := "mode: rule\n", "DOMAIN,old.example.test\n"
	if changed {
		main = "mode: global\nsecret: external-user-secret\n"
		var lines strings.Builder
		lines.WriteString("DOMAIN," + agentToken + ".example.test\n")
		for i := 0; i < 80; i++ {
			fmt.Fprintf(&lines, "DOMAIN,新-%03d.example.test\n", i)
		}
		rules = lines.String()
	}
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "previous", AgentID: "agent-1", ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	files := map[string]deploy.Content{"config.yaml": {Data: []byte(main), Role: deploy.RoleConfig}, "rules/example.list": {Data: []byte(rules), Role: deploy.RoleRule}}
	for _, path := range []string{"config.yaml", "rules/example.list"} {
		v := files[path]
		sum := sha256.Sum256(v.Data)
		m.Files = append(m.Files, deploy.File{Path: path, Role: v.Role, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(v.Data))})
	}
	b, err := deploy.Pack(m, files, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func seedContentBaseline(t *testing.T, dir string) {
	t.Helper()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(context.Background())
	old := contentDiffBundle(t, false)
	fp, err := deploy.Fingerprint(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	digests, err := deploy.Digests(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	files := oObj()
	for path, digest := range digests {
		files.Set(path, digest)
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		raw, err := json.Marshal(system)
		if err != nil {
			return err
		}
		doc, err := parsePythonJSON(raw)
		if err != nil {
			return err
		}
		agentsList := doc.(*orderedObject).GetList("agents")
		a := agentsList[0].(*orderedObject)
		a.Set("config_version", digests["config.yaml"])
		a.Set("pushed_at", "2026-01-01T00:00:00Z")
		a.Set("config_revision", 1)
		a.Set("applied_content", oObj("fingerprint", fp, "files", files, "profile_id", "default", "service_type", "mihomo", "config_version", digests["config.yaml"], "deployment_id", "previous", "config_path", "config.yaml", "base_url", "https://original.example.test/prefix"))
		return system.SetJSON("agents", mustRawJSON(t, agentsList))
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Config(ctx, "default", repo.ResourceRoots{})
	if err != nil {
		t.Fatal(err)
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	src, err := deploy.SourceFromJSON(cfgRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = deploy.NewReceipts(r).Capture(ctx, "agent-1", old, src, "https://original.example.test/prefix", nil); err != nil {
		t.Fatal(err)
	}
}
