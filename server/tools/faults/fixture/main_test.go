package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedGuardsAndNativeFixture(t *testing.T) {
	dir := t.TempDir()
	if err := run("seed", dir, "http://127.0.0.1:12345", false); err == nil {
		t.Fatal("unmarked data directory accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, ".faults-owned"), []byte("configflow-faults-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("seed", dir, "http://192.0.2.1:12345", false); err == nil {
		t.Fatal("nonloopback seed source accepted")
	}
	if err := run("seed", dir, "http://127.0.0.1:12345", false); err != nil {
		t.Fatal(err)
	}
	if err := run("seed", dir, "http://127.0.0.1:12345", false); err == nil {
		t.Fatal("existing repository overwritten")
	}
}
