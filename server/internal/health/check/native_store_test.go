package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
)

func TestNativeProbeAndRegionFilesInteroperateWithPython(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := filepath.Abs("../../../..")
	base := t.TempDir()
	probeDir, regionDir := filepath.Join(base, "probe"), filepath.Join(base, "region")
	probe, region := NewProbeStore(probeDir), NewRegionStore(regionDir)
	stamp := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	var entry TimedRecord
	if err := json.Unmarshal([]byte(`{"checked_at":"2030-01-01T00:00:00","reasons":["中文 <>&"],"target":null}`), &entry); err != nil {
		t.Fatal(err)
	}
	var results model.OrderedMap[TimedRecord]
	results.Set("example.com", entry)

	if err := probe.SaveEntries(t.Context(), "go", results, stamp); err != nil {
		t.Fatal(err)
	}
	if err := region.UpdateData(t.Context(), "go", func(data *RegionData) error {
		data.Domains = results
		return nil
	}, stamp); err != nil {
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
	wantBytes, err := jsonresp.Encode(results)
	if err != nil {
		t.Fatal(err)
	}
	gotProbe, err := jsonresp.Encode(probe.LoadEntries("python", stamp))
	if err != nil || !bytes.Equal(gotProbe, wantBytes) {
		t.Fatalf("probe mismatch: %s vs %s (%v)", gotProbe, wantBytes, err)
	}
	gotRegion, err := jsonresp.Encode(region.LoadData("python").Domains)
	if err != nil || !bytes.Equal(gotRegion, wantBytes) {
		t.Fatalf("region mismatch: %s vs %s (%v)", gotRegion, wantBytes, err)
	}
}
