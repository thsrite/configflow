package trafficstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/jsonx"
)

func TestTrafficFilesInteroperateWithPython(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := filepath.Abs("../../../..")
	store := New(t.TempDir())
	value := jsonx.Obj("status", jsonx.Obj("status", "running", "window_end", "synthetic"), "days", jsonx.NewObject(), "future", nil)
	if _, err := store.WriteJSON("go", value); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import sys;from backend.agents.traffic_store import TrafficStore
s=TrafficStore(sys.argv[1]);p=s._read('go');assert p['status']['status']=='running';assert p['future'] is None;s._write_json('python',p)
assert s.load_status('python')['status']=='running'
print('traffic files read and written by Python')`, store.files.DataDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := store.ReadJSON("python"); !jsonx.Equal(got, value) {
		t.Fatalf("%v", got)
	}
}
