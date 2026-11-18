package notifications

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/tasks"
)

func TestNativeSignatureChild(t *testing.T) {
	path := os.Getenv("N87_SIGNATURE_OUT")
	if path == "" {
		return
	}
	signature, err := RouterDigest(json.RawMessage(`{"z":"中文","a":1}`), json.RawMessage(`{"id":"router"}`))
	if err != nil {
		t.Fatal(err)
	}
	var item Item
	if err = json.Unmarshal([]byte(`{"source":"notice","title":"中文","message":"message","severity":"warning","status_label":"pending","target":{"id":"router"},"evidence":["a","b"]}`), &item); err != nil {
		t.Fatal(err)
	}
	identity, err := IncidentFingerprint("notice", &item)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(signature+"\n"+identity), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestNativeSignaturesAcrossProcessRestarts(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte("17da2ce6aa800a88b9437370d51348e25d8ec354edcc9a68f494a480f401cbfa\n0267bf46778074b7eeb98037dc5d72c1d23748a3309dd1832c5d53fac18f1ebe")
	var first []byte
	for i := range 2 {
		path := filepath.Join(t.TempDir(), "signature.json")
		cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestNativeSignatureChild$")
		cmd.Env = append(os.Environ(), "N87_SIGNATURE_OUT="+path)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child %d: %s %v", i, output, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
		} else if !bytes.Equal(data, first) {
			t.Fatalf("restart changed signatures: %s != %s", data, first)
		}
	}
	if !bytes.Equal(first, expected) {
		t.Fatalf("native identity fixture: %s", first)
	}
}
func TestNativeNotificationFileInterchange(t *testing.T) {
	dir := t.TempDir()
	item := &Item{State: "pending", Source: NoticeSource, Title: new("synthetic"), Evidence: jsonString("one")}
	if err := UpdateItems(t.Context(), dir, func(items *Items) error { return ObserveItem(items, "notice:x", item) }); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "notifications.json")
	script := `import json,sys
p=sys.argv[1];v=json.load(open(p));assert v['items']['notice:x']['title']=='synthetic'
v['items']['notice:x']['future']={'missing':float('nan'),'keep':1}
json.dump(v,open(p,'w'))`
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", script, path).CombinedOutput(); err != nil {
		t.Fatalf("Python: %s %v", out, err)
	}
	if err := UpdateItems(t.Context(), dir, func(items *Items) error { item, _ := items.Get("notice:x"); item.Stale = new(true); return nil }); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "python3", "-c", `import json,sys
v=json.load(open(sys.argv[1]));assert v['items']['notice:x']['future']=={'missing':None,'keep':1}`, path).CombinedOutput(); err != nil {
		t.Fatalf("Python reread: %s %v", out, err)
	}
}

func TestNativeD2DismissalReappearsOnceAndRouterStateIsPreserved(t *testing.T) {
	items := newItems()
	items.Set("notice:x", &Item{State: "dismissed", Source: NoticeSource, Fingerprint: new("old-python-fingerprint")})
	observation := &Item{State: "pending", Source: NoticeSource, Title: new("synthetic"), Evidence: jsonString("one")}
	if err := ObserveItem(items, "notice:x", observation); err != nil {
		t.Fatal(err)
	}
	current, _ := items.Get("notice:x")
	if current.State != "pending" {
		t.Fatal("old dismissal hid native incident")
	}
	marker, err := dismissedItem("notice:x", current)
	if err != nil {
		t.Fatal(err)
	}
	items.Set("notice:x", marker)
	if err := ObserveItem(items, "notice:x", observation); err != nil {
		t.Fatal(err)
	}
	current, _ = items.Get("notice:x")
	if current.State != "dismissed" {
		t.Fatal("same native incident replayed twice")
	}
	key := "router:default:r:firewall"
	items.Set(key, &Item{State: "pending", Source: "router-check", Signature: new("old-signature"), Version: new("unchanged-version")})
	collected := &collectedObservations{profiles: map[string]string{}, routers: map[string]string{"router:default:r:": "native-signature"}, keys: map[string]bool{}}
	if err := collected.apply(items, false); err != nil {
		t.Fatal(err)
	}
	router, _ := items.Get(key)
	if router.State != "pending" || stringValue(router.Version) != "unchanged-version" || router.Stale == nil || !*router.Stale {
		t.Fatal("signature update changed incident state", router)
	}
}

// Python can load/save null, but its observe += 1 cannot consume a manually
// corrupted nonfinite occurrence counter after Go repairs it to null.
func TestNativeNotificationRollbackNullOccurrence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.json")
	if err := os.WriteFile(path, []byte(`{"items":{"notice:x":{"state":"pending","source":"notice","occurrences":NaN,"evidence":"one"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateItems(t.Context(), dir, func(items *Items) error { item, _ := items.Get("notice:x"); item.Stale = new(true); return nil }); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	script := `import json,sys
from backend.utils.notification_store import observe
p=sys.argv[1];v=json.load(open(p));assert v['items']['notice:x']['occurrences'] is None
json.dump(v,open(p,'w'));v=json.load(open(p))
try:
    observe(v['items'],'notice:x',{'state':'pending','source':'notice','evidence':'two'})
except TypeError:
    pass
else:
    raise AssertionError('Python null occurrence arithmetic changed')`
	command := exec.CommandContext(t.Context(), filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", script, path)
	command.Dir = root
	command.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Python rollback: %s %v", out, err)
	}
}

func TestNativeD2SubscriptionDismissalReappearsOnce(t *testing.T) {
	task := &tasks.Task{Key: "subscription:synthetic", Category: "subscription", Name: "Synthetic", ResourceID: "synthetic", Enabled: true,
		HistoryData: &tasks.HistoryData{LastStatus: new("failed"), LastError: new("synthetic failure"), LastRunAt: new("2026-01-01T00:00:00+00:00"), RunCount: "2", FailureCount: "1", CacheRetained: new(true)}}
	observation, err := TaskIncident(task, map[string]model.Agent{}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	key := "task:subscription:synthetic"
	items := newItems()
	items.Set(key, &Item{State: "dismissed", Source: "task", Fingerprint: new("old-python-fingerprint")})
	if err := ObserveItem(items, key, observation); err != nil {
		t.Fatal(err)
	}
	current, _ := items.Get(key)
	if current.State != "pending" || current.Message == nil || *current.Message != "synthetic failure" {
		t.Fatalf("old dismissal did not reappear: %+v", current)
	}
	marker, err := dismissedItem(key, current)
	if err != nil {
		t.Fatal(err)
	}
	items.Set(key, marker)
	if err := ObserveItem(items, key, observation); err != nil {
		t.Fatal(err)
	}
	current, _ = items.Get(key)
	if current.State != "dismissed" {
		t.Fatal("same native incident reappeared twice")
	}
	task.HistoryData.LastError = new("new synthetic failure")
	changed, err := TaskIncident(task, map[string]model.Agent{}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ObserveItem(items, key, changed); err != nil {
		t.Fatal(err)
	}
	current, _ = items.Get(key)
	if current.State != "pending" {
		t.Fatal("a genuinely new incident stayed hidden")
	}
}
