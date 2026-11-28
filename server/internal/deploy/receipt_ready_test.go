package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

func TestReceiptReadyTracksFileAndAppliedIdentity(t *testing.T) {
	root := t.TempDir()
	fixtureCopy(t, "testdata/stores", root)
	data, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Agent model.Agent }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	r := &Receipts{dataDir: root}
	a := &fixture.Agent
	path := filepath.Join(r.folder(a.ID, a.AppliedContent.DeploymentID), "receipt.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		gen := agents.Generation{Shared: true}
		for range 2 {
			ready, err := r.Ready(t.Context(), a, gen)
			loaded, loadErr := r.LoadAgent(t.Context(), a, false)
			if err != nil || loadErr != nil || ready != (loaded != nil) || ready != want {
				t.Fatalf("ready=%v loaded=%v want=%v errors=%v/%v", ready, loaded != nil, want, err, loadErr)
			}
		}
	}
	check(true)
	saved := a.ConfigVersion
	a.ConfigVersion = "changed"
	check(false)
	a.ConfigVersion = saved
	check(true)
	other := path + ".replacement"
	if err = os.WriteFile(other, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err = os.Rename(path, other); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	check(false)
}
