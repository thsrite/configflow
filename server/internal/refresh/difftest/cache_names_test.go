package difftest

import (
	"configflow/internal/repo"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// referenceCacheNames asks the real Python owner for its path, and the Python
// standard encoder for D2's compact JSON declaration digest. Neither expected
// filename comes from rules.CacheFilename or other product code.
func referenceCacheNames(t *testing.T, address string) (map[string]string, map[string]string) {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Fatal("live filenames require the explicit Flask differential")
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	script := `import hashlib,json,sys
from backend.utils.rule_utils import rule_cache_filename
address=sys.argv[1]
fields=['r-ok',address,'domain','text']
rule=dict(id=fields[0],url=address,behavior='domain',format='text',source_type='url')
print(json.dumps({'python':rule_cache_filename(rule),'native':'by-source/'+hashlib.sha256(json.dumps(fields,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()+'.list'}))`
	cmd := exec.Command(python, "-c", script, address)
	cmd.Dir, _ = filepath.Abs("../../../..")
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent cache names: %v %s", err, raw)
	}
	var paths struct {
		Python string `json:"python"`
		Native string `json:"native"`
	}
	if json.Unmarshal(raw, &paths) != nil || paths.Python == "" || paths.Native == "" || paths.Python == paths.Native {
		t.Fatal("missing independent D2 names")
	}
	keys := func(name string) map[string]string {
		prefix := "shared/rules/" + name
		return map[string]string{prefix: "shared/rules/declaration-r-ok.list", prefix + ".bak": "shared/rules/declaration-r-ok.list.bak"}
	}
	return keys(paths.Python), keys(paths.Native)
}

func TestD2FileKeysPreserveContentAndUnknownFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Open(dir, repo.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task-status.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{"shared/rules/by-source/known.list": "logical"}
	for _, file := range []string{"shared/rules/by-source/known.list", "shared/rules/by-source/other.list", "shared/rules/by-id/known.list", "shared/rules/by-source/known.list.invalid"} {
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unchanged content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := files(t, dir, names)
	if first["logical"] != "unchanged content" {
		t.Fatal("known content omitted")
	}
	for _, key := range []string{"shared/rules/by-source/other.list", "shared/rules/by-id/known.list", "shared/rules/by-source/known.list.invalid"} {
		if first[key] != "unchanged content" {
			t.Fatal("non-declaration file was normalized", key)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "shared/rules/by-source/known.list"), []byte("changed content"), 0600); err != nil {
		t.Fatal(err)
	}
	second := files(t, dir, names)
	if first["logical"] == second["logical"] {
		t.Fatal("D2 mapping hid content change")
	}
}
