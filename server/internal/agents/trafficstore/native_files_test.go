package trafficstore

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	native := NewNative(store.files.DataDir)
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if _, err := native.files.Write("native", json.RawMessage(`{"status":null,"days":{},"future":{"n":2.00}}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := native.Add(t.Context(), "native", nativeReport(t), &at); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import sys;from backend.agents.traffic_store import TrafficStore
s=TrafficStore(sys.argv[1]);p=s._read('go');assert p['status']['status']=='running';assert p['future'] is None;s._write_json('python',p)
assert s.load_status('python')['status']=='running'
n=s._read('native');assert n['future']=={'n':2};assert n['days']['2026-11-19']['hosts']['a.example.com']['by_route']['MATCH\t\tdirect\tP']['conns']==2;s._write_json('native-python',n)
print('traffic files read and written by Python')`, store.files.DataDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := store.ReadJSON("python"); !jsonx.Equal(got, value) {
		t.Fatalf("%v", got)
	}
	original, err := json.Marshal(native.LoadDocument("native", &at))
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := json.Marshal(native.LoadDocument("native-python", &at))
	if err != nil {
		t.Fatal(err)
	}
	left, e1 := jsonx.Parse(original)
	right, e2 := jsonx.Parse(roundtrip)
	if e1 != nil || e2 != nil || !jsonx.Equal(left, right) {
		t.Fatal("native file changed in Python round trip")
	}
}
