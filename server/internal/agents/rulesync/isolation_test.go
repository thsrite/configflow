package rulesync

import (
	"bytes"
	"configflow/internal/deploy"
	"configflow/internal/yamlx"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type bundleView struct {
	Manifest deploy.Manifest `json:"manifest"`
	Files    map[string]struct {
		Data []byte      `json:"data"`
		Role deploy.Role `json:"role"`
	} `json:"files"`
}

func (v bundleView) pack(t *testing.T) *deploy.Bundle {
	t.Helper()
	files := map[string]deploy.Content{}
	for p, f := range v.Files {
		files[p] = deploy.Content{Data: f.Data, Role: f.Role}
	}
	b, e := deploy.Pack(v.Manifest, files, digest(files[v.Manifest.ConfigPath].Data)[:8], nil)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestIsolationAgainstPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 for real Python")
	}
	root, e := filepath.Abs("../../../..")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "server/internal/agents/rulesync/testdata/isolation_reference.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, e := cmd.Output()
	if e != nil {
		t.Fatalf("python %v %s", e, stderr.String())
	}
	var cases []struct {
		Name, Service, Error    string
		Base, Candidate, Result bundleView
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	if len(cases) == 0 {
		t.Fatal("empty isolation oracle")
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			old, new := tt.Base.pack(t), tt.Candidate.pack(t)
			out, e := IsolateRuleBundle(old, new, tt.Service)
			if tt.Error != "" {
				if e == nil || e.Error() != tt.Error {
					t.Fatalf("got %v want %s", e, tt.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			files, e := out.Contents()
			if e != nil {
				t.Fatal(e)
			}
			if len(files) != len(tt.Result.Files) {
				t.Fatalf("file count %d want %d", len(files), len(tt.Result.Files))
			}
			for path, want := range tt.Result.Files {
				got, ok := files[path]
				if !ok || got.Role != want.Role {
					t.Fatalf("file %s role mismatch", path)
				}
				if path == tt.Result.Manifest.ConfigPath {
					a, e := yamlx.Parse(got.Data)
					if e != nil {
						t.Fatal(e)
					}
					b, e := yamlx.Parse(want.Data)
					if e != nil || !yamlx.Equal(a, b) {
						t.Fatal("main document mismatch")
					}
				} else if !bytes.Equal(got.Data, want.Data) {
					t.Errorf("file %s differs", path)
				}
			}
		})
	}
}

func TestIsolationRejectsAliasAmplification(t *testing.T) {
	for _, main := range []string{"x: &x [*x]\nrules: []\n", func() string {
		var b strings.Builder
		b.WriteString("a0: &a0 [x]\n")
		for i := 1; i < 40; i++ {
			fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
		}
		b.WriteString("rules: []\n")
		return b.String()
	}()} {
		files := map[string]deploy.Content{"config.yaml": {Data: []byte(main), Role: deploy.RoleConfig}}
		m := deploy.Manifest{ProtocolVersion: 1, AgentID: "device", ProfileID: "default", ServiceType: "mihomo", DeploymentID: "old", ConfigPath: "config.yaml", Files: manifestFiles(files)}
		old, e := deploy.Pack(m, files, digest([]byte(main))[:8], nil)
		if e != nil {
			t.Fatal(e)
		}
		m.DeploymentID = "new"
		next, e := deploy.Pack(m, files, digest([]byte(main))[:8], nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = IsolateRuleBundle(old, next, "mihomo"); e == nil {
			t.Fatal("unsafe aliases accepted")
		}
	}
}
