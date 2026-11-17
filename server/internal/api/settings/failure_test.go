package settings_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
)

type failureLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *failureLogs) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(data)
}
func (l *failureLogs) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }

func TestProxySaveFailureDoesNotEchoCredentials(t *testing.T) {
	s := newServer(t, false)
	s.Get(endpoint).WantStatus(200)
	before, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	var logs failureLogs
	if _, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{Console: io.Discard}) })
	if err := os.Chmod(s.Repo.DatabasePath(), 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.Repo.DatabasePath(), 0600) })
	response := s.Post(endpoint, httpxtest.Body([]byte(`{"rule_fetch_proxy":"http://synthetic-user:synthetic-secret@proxy.example.com:8080"}`), "application/json")).WantStatus(500)
	if response.Object().GetString("message") != "保存规则下载代理失败" {
		t.Fatal(response.Text())
	}
	after, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("failed save changed DB or revision")
	}
	for _, secret := range []string{"synthetic-user", "synthetic-secret", "proxy.example.com"} {
		if response.Contains(secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("save failure leaked %q", secret)
		}
	}
	if !strings.Contains(logs.String(), "exception_type=") {
		t.Fatal("missing safe failure diagnostic")
	}
}
