package jsonstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the compatibility API as well: its delegated failure must clean the
// abandoned temp file without replacing the destination or changing its error.
func TestReviewLegacyWriteFailureCleansTemp(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(fmt.Sprintf("signed=%v", signed), func(t *testing.T) {
			s := New(t.TempDir())
			path := s.Path("synthetic")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(path, "keep")
			if err := os.WriteFile(sentinel, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			sig, err := s.Write(t.Context(), "synthetic", struct{}{}, signed)
			var linkErr *os.LinkError
			if sig != nil || !errors.As(err, &linkErr) || linkErr.Op != "rename" {
				t.Fatalf("signature=%v error=%v", sig, err)
			}
			if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("abandoned temp remains: %v", err)
			}
			if got, err := os.ReadFile(sentinel); err != nil || string(got) != "old" {
				t.Errorf("destination changed: %q %v", got, err)
			}
		})
	}
}
