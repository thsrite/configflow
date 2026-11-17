package difftest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
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
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	old := contentDiffBundle(t, false)
	fp, err := deploy.Fingerprint(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	digests, err := deploy.Digests(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	files := jsonx.NewObject()
	for path, digest := range digests {
		files.Set(path, digest)
	}
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		a := system.GetList("agents")[0].(*jsonx.Object)
		a.Set("config_version", digests["config.yaml"])
		a.Set("pushed_at", "2026-01-01T00:00:00Z")
		a.Set("config_revision", 1)
		a.Set("applied_content", jsonx.Obj("fingerprint", fp, "files", files, "profile_id", "default", "service_type", "mihomo", "config_version", digests["config.yaml"], "deployment_id", "previous", "config_path", "config.yaml", "base_url", "https://original.example.test/prefix"))
		return system, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := r.GetCompatConfig(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = deploy.NewReceipts(dir).Capture(ctx, "agent-1", old, deploy.NewSource(cfg, nil), "https://original.example.test/prefix", nil); err != nil {
		t.Fatal(err)
	}
}
