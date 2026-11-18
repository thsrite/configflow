package taskstatus

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeStatusFileInterchange(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.SaveResult(t.Context(), "first", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	script := `import json,sys
p=sys.argv[1]
v=json.load(open(p)); assert v['first']['run_count']==1
v['first']['extra']={'kept':9};v['other']={'run_count':float('nan'),'note':'keep'}
json.dump(v,open(p,'w'))`
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", script, store.path()).CombinedOutput(); err != nil {
		t.Fatalf("Python: %s %v", out, err)
	}
	if _, err := store.SaveResult(t.Context(), "other", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(store.DataDir, Filename))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["other"]["run_count"]) != "1" || string(doc["other"]["note"]) != `"keep"` || string(doc["first"]["extra"]) != `{"kept":9}` {
		t.Fatalf("lost receipt members: %s", raw)
	}
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", `import json,sys
v=json.load(open(sys.argv[1]));assert v['other']['run_count']==1;assert v['first']['extra']=={'kept':9}`, store.path()).CombinedOutput(); err != nil {
		t.Fatalf("Python reread: %s %v", out, err)
	}
}
