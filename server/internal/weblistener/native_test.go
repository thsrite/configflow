package weblistener

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeListenerFileSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-listener.json")
	if err := os.WriteFile(path, []byte(`{"port":18081,"extra":NaN}`), 0600); err != nil {
		t.Fatal(err)
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
