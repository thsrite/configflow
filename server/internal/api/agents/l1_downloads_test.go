package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// TestL1UnsatisfiableBinaryRangeIs416: unusable ranges get net/http's
// plain-text 416 with the file size (O18 C15).
func TestL1UnsatisfiableBinaryRangeIs416(t *testing.T) {
	s := newServer(t)
	dir := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "configflow-agent-linux-amd64"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, value := range []string{"bytes=10-", "bytes=99999999999999999999999999-", "bytes=x-y"} {
			r := s.Do(method, "/api/agents/download/configflow-agent-linux-amd64", httpxtest.Header("Range", value)).WantStatus(416)
			if value != "bytes=10-" {
				continue // syntactically invalid: 416 without Content-Range
			}
			if r.Header.Get("Content-Range") != "bytes */10" || r.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("invalid range failure headers: %v", r.Header)
			}
		}
	}
	s.Get("/api/agents/download/configflow-agent-linux-amd64", httpxtest.Header("Range", "bytes=2-4")).WantStatus(206)
}
