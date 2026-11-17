package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestNativeProbeAndRegionFilesInteroperateWithPython(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := filepath.Abs("../../../..")
	base := t.TempDir()
	probeDir, regionDir := filepath.Join(base, "probe"), filepath.Join(base, "region")
	probe, region := NewProbeStore(probeDir), NewRegionStore(regionDir)
	stamp := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	results := jsonx.Obj("example.com", jsonx.Obj("checked_at", "2030-01-01T00:00:00", "reasons", []any{"中文 <>&"}, "target", nil))
	if err := probe.Save("go", results, stamp); err != nil {
		t.Fatal(err)
	}
	if err := region.Update("go", func(data *jsonx.Object) error { data.Set("domains", results); return nil }, stamp); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import sys;from datetime import datetime;from backend.utils.domain_probe import ProbeStore;from backend.utils.region_check import RegionStore
p=ProbeStore(sys.argv[1]);r=RegionStore(sys.argv[2]);now=datetime(2030,1,1)
a=p.load('go',now=now);assert a['example.com']['reasons']==['中文 <>&'];assert a['example.com']['target'] is None;p.save('python',a,now=now)
b=r.load('go');assert b['domains']==a;r.update('python',lambda data:data.update(b),now=now)
print('probe/region both directions')`, probeDir, regionDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	got, err := probe.Load("python", stamp)
	if err != nil || !jsonx.Equal(got, results) {
		t.Fatalf("%v %v", got, err)
	}
	loaded := region.Load("python")
	if !jsonx.Equal(loaded.GetObject("domains"), results) {
		t.Fatalf("%v", loaded)
	}
}
