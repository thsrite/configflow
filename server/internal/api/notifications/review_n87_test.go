package notifications_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestN87ReviewStoreContainerErrorsRetainUnavailable(t *testing.T) {
	for _, raw := range []string{`[]`, `{"items":[]}`} {
		t.Run(raw, func(t *testing.T) {
			s, _, _ := notificationServer(t)
			path := filepath.Join(s.DataDir, "notifications.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				s.Do(method, "/api/notifications").WantStatus(503)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(raw)) {
				t.Fatalf("invalid receipt replaced: %s %v", after, err)
			}
			leftovers, _ := filepath.Glob(filepath.Join(s.DataDir, ".notifications-*"))
			if len(leftovers) != 0 {
				t.Fatal("temporary files leaked", leftovers)
			}
		})
	}
}
