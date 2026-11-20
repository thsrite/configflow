package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests of the A09 review (references: testdata/gen_review_a09.py
// rows review/issue/*, replayed by TestDockerHAReviewCorpusMatchesPython).

func reviewBody() map[string]any {
	return map[string]any{
		"server_url":   "https://panel.test/",
		"agent_name":   "new-dns",
		"agent_ip":     "192.168.50.20",
		"data_dir":     "/srv/oxidns",
		"oxidns_port":  8082,
		"network_mode": "host",
	}
}

// A session file holding JSON null is Python's `session is None`: issue
// creates a new session; the existing index keeps its old id.
func TestReviewIssueNullSessionRefusesWithoutWrites(t *testing.T) {
	dataDir := t.TempDir()
	first, err := IssueEnrollment(context.Background(), dataDir, reviewBody(), "default")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	oldID := first.URL[strings.LastIndex(first.URL, "/")+1:]
	if err := os.WriteFile(filepath.Join(root, oldID+".json"), []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := canonical(readTree(root))
	second, err := IssueEnrollment(context.Background(), dataDir, reviewBody(), "default")
	var refusal *EnrollmentError
	if second != nil || !errors.As(err, &refusal) || refusal.Status != 409 || refusal.Message != enrollmentUnreadable {
		t.Fatalf("native null refusal: %v", err)
	}
	if canonical(readTree(root)) != before {
		t.Fatal("null refusal wrote files")
	}

}

// body['agent_ip'] is a KeyError (500), not a session with host None.
func TestReviewIssueWithoutAgentIPIsAKeyError(t *testing.T) {
	body := reviewBody()
	delete(body, "agent_ip")
	_, err := IssueEnrollment(context.Background(), t.TempDir(), body, "default")
	var te *TypeError
	if !errors.As(err, &te) {
		t.Fatalf("want a non-ValueError, got %v", err)
	}
}
