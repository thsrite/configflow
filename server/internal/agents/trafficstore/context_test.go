package trafficstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/ctxx"
)

func TestDeleteContextPreservesFileAfterShutdown(t *testing.T) {
	s := New(t.TempDir())
	path := s.Path("machine")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("traffic"), 0600); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.DeleteContext(ctxx.WithParent(context.Background(), parent), "machine"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "traffic" {
		t.Fatalf("cancelled deletion: %q %v", raw, err)
	}
}
