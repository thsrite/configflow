package weblistener

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeListenerFileInterchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-listener.json")
	data, err := json.Marshal(struct {
		Port int `json:"port"`
	}{18080})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	script := `import json,sys
p=sys.argv[1]
assert json.load(open(p)) == {"port":18080}
json.dump({"port":18081,"extra":float("nan")},open(p,"w"))`
	if output, err := exec.CommandContext(t.Context(), "python3", "-c", script, path).CombinedOutput(); err != nil {
		t.Fatalf("Python: %s %v", output, err)
	}
	manager := &Manager{DataDir: filepath.Dir(path), LookupEnv: func(string) (string, bool) { return "", false }}
	port, source, err := manager.selection()
	if err != nil || port != 18081 || source != "saved" {
		t.Fatalf("selection %d %s %v", port, source, err)
	}
	if err = os.WriteFile(path, []byte(`{"PORT":18081}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.selection(); err == nil {
		t.Fatal("non-exact port key accepted")
	}
}
func TestNativeEnvironmentWhitespace(t *testing.T) {
	manager := &Manager{LookupEnv: func(string) (string, bool) { return "\u001c8080\u001d", true }}
	if _, err := manager.EnvironmentPort("CONFIGFLOW_WEB_PORT", 80); err == nil {
		t.Fatal("CPython-only whitespace accepted")
	}
}
