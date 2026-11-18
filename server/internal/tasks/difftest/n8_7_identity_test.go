package difftest

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

// Only this synthetic incident's D2 identity changes. Its pre-dismissal item
// comes from the real Python route. An independent Python encoder computes the
// approved native compact/UTF-8/sorted representation; no business hook is patched.
func nativeIncidentReference(t *testing.T, state json.RawMessage) string {
	t.Helper()
	item := stateIncident(t, state)
	command := exec.CommandContext(t.Context(), "python3", "-c", `import json,sys,hashlib
item=json.load(sys.stdin)
fields={k:item.get(k) for k in ['title','message','severity','status_label','target','evidence']}
print(hashlib.sha256(json.dumps(fields,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode()).hexdigest())`)
	command.Stdin = bytes.NewReader(item)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(output))
}
func stateIncident(t *testing.T, state json.RawMessage) json.RawMessage {
	t.Helper()
	var document struct {
		Files map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(state, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Files["notifications.json"]) == 0 {
		return nil
	}
	var store struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(document.Files["notifications.json"], &store); err != nil {
		t.Fatal(err)
	}
	return store.Items["task:subscription:s"]
}
func withNativeDismissal(t *testing.T, got, want answer, nativeID string) answer {
	t.Helper()
	var old, newItem struct {
		State       string `json:"state"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(stateIncident(t, want.State), &old); err != nil {
		if len(stateIncident(t, want.State)) == 0 {
			return want
		}
		t.Fatal(err)
	}
	if old.State != "dismissed" {
		return want
	}
	if err := json.Unmarshal(stateIncident(t, got.State), &newItem); err != nil {
		t.Fatal(err)
	}
	const pythonID = "6507bc03098e9257973837c3b508953c7cb19bad61da59f1e01bb44d3999ac5b"
	if old.Fingerprint != pythonID || newItem.Fingerprint != nativeID || nativeID == pythonID {
		t.Fatalf("D2 identity: Python %s, Go %s, native reference %s", old.Fingerprint, newItem.Fingerprint, nativeID)
	}
	// Replace exactly the approved fingerprint; every other field/file is still
	// compared by the original comparator, including the dismissed marker shape.
	var document map[string]json.RawMessage
	if err := json.Unmarshal(want.State, &document); err != nil {
		t.Fatal(err)
	}
	var files map[string]json.RawMessage
	_ = json.Unmarshal(document["files"], &files)
	var store map[string]json.RawMessage
	_ = json.Unmarshal(files["notifications.json"], &store)
	var items map[string]json.RawMessage
	_ = json.Unmarshal(store["items"], &items)
	var marker map[string]json.RawMessage
	_ = json.Unmarshal(items["task:subscription:s"], &marker)
	marker["fingerprint"], _ = json.Marshal(nativeID)
	items["task:subscription:s"], _ = json.Marshal(marker)
	store["items"], _ = json.Marshal(items)
	files["notifications.json"], _ = json.Marshal(store)
	document["files"], _ = json.Marshal(files)
	want.State, _ = json.Marshal(document)
	return want
}
