package difftest

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
)

func TestCFG06DuplicateTargetDifference(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 enables Flask differential")
	}
	for _, tc := range []struct {
		name          string
		input, unique []string
	}{
		{"direct", []string{"direct", "direct"}, []string{"direct"}},
		{"mixed", []string{"proxy", "direct", "proxy", "direct"}, []string{"proxy", "direct"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed := t.TempDir()
			runPython(t, seed, "cfg06-seed")
			pyDir, goDir := t.TempDir(), t.TempDir()
			for _, directory := range []string{pyDir, goDir} {
				if output, err := exec.Command("cp", "-a", seed+"/.", directory).CombinedOutput(); err != nil {
					t.Fatalf("copy seed: %v %s", err, output)
				}
			}
			py := startPython(t, pyDir)
			goServer := httpxtest.New(t, httpxtest.Options{DataDir: goDir})
			domaindiscovery.NewHandler(goServer.Repo, nil).Register(goServer.App.Router)
			body, err := json.Marshal(map[string]any{"targets": tc.input, "proxy_policy": "PROXY"})
			if err != nil {
				t.Fatal(err)
			}
			for round := 1; round <= 2; round++ {
				for index, peer := range []string{py, goServer.URL()} {
					status, result := call(t, peer, "POST", "/api/domain-discovery/rulesets/init", string(body))
					if status != 200 {
						t.Fatalf("peer=%d status=%d result=%v", index, status, result)
					}
					var snapshot any
					if index == 0 {
						_, snapshot = call(t, py, "GET", "/__state", "")
					} else {
						snapshot = state(t, goServer.Repo, goDir)
					}
					current := snapshot.(map[string]any)
					library := current["library"].([]any)
					refs := current["profile"].(map[string]any)["rule_configs"].([]any)
					want := tc.unique
					if index == 0 {
						want = tc.input
					}
					// Assert the intentional difference, rather than remapping or
					// suppressing the orphan rows/references emitted by Python.
					if len(library) != round*len(want) || len(refs) != round*len(want)+1 {
						t.Fatalf("peer=%d round=%d library=%d refs=%d", index, round, len(library), len(refs))
					}
					if refs[len(refs)-1].(map[string]any)["id"] != "fallback" {
						t.Fatal("MATCH moved")
					}
					selected := result.(map[string]any)
					for i, target := range want {
						ref := refs[(round-1)*len(want)+i].(map[string]any)
						binding := selected[target].(map[string]any)
						policy := "DIRECT"
						if target == "proxy" {
							policy = "PROXY"
						}
						if ref["library_rule_id"] != binding["id"] || ref["policy"] != policy {
							t.Fatalf("peer=%d lost ordered target binding", index)
						}
					}
					if index == 1 {
						for i, target := range tc.unique {
							item := library[(round-1)*len(tc.unique)+i].(map[string]any)
							if item["id"] != selected[target].(map[string]any)["id"] {
								t.Fatal("Go allocated an orphan library")
							}
						}
					}
					directory := []string{pyDir, goDir}[index]
					caches, backups := 0, 0
					if err := filepath.WalkDir(filepath.Join(directory, "shared", "rules"), func(path string, entry fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if entry.IsDir() {
							return nil
						}
						if strings.HasSuffix(path, ".bak") {
							backups++
						}
						if strings.HasSuffix(path, ".list") {
							caches++
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if caches != round*len(tc.unique) || backups != 0 {
						t.Fatalf("peer=%d caches=%d backups=%d", index, caches, backups)
					}
				}
			}
		})
	}
}
