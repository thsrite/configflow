package trafficstore

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/testjson"
)

func TestTrafficFilesInteroperateWithPython(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := filepath.Abs("../../../..")
	native := NewNative(t.TempDir())
	value := testjson.Obj(
		testjson.Field{Key: "status", Value: testjson.Obj(
			testjson.Field{Key: "status", Value: "running"},
			testjson.Field{Key: "window_end", Value: "synthetic"},
		)},
		testjson.Field{Key: "days", Value: testjson.Obj()},
		testjson.Field{Key: "future", Value: nil},
	)
	if _, err := native.files.Write(t.Context(), "go", value, true); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if _, err := native.files.Write(t.Context(), "native", json.RawMessage(`{"status":null,"days":{},"future":{"n":2.00}}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := native.Add(t.Context(), "native", nativeReport(t), &at); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import sys;from backend.agents.traffic_store import TrafficStore
s=TrafficStore(sys.argv[1]);p=s._read('go');assert p['status']['status']=='running';assert p['future'] is None;s._write_json('python',p)
assert s.load_status('python')['status']=='running'
n=s._read('native');assert n['future']=={'n':2};assert n['days']['2026-11-19']['hosts']['a.example.com']['by_route']['MATCH\t\tdirect\tP']['conns']==2;s._write_json('native-python',n)
print('traffic files read and written by Python')`, native.files.DataDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	raw, err := os.ReadFile(native.Path("python"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := testjson.Parse(raw)
	if err != nil || !testjson.Equal(value, got) {
		t.Fatalf("python round trip mismatch: %v %s", err, raw)
	}
	original, err := json.Marshal(native.LoadDocument("native", &at))
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := json.Marshal(native.LoadDocument("native-python", &at))
	if err != nil {
		t.Fatal(err)
	}
	left, e1 := testjson.Parse(original)
	right, e2 := testjson.Parse(roundtrip)
	if e1 != nil || e2 != nil || !testjson.Equal(left, right) {
		t.Fatal("native file changed in Python round trip")
	}
}
