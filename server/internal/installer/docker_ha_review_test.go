package installer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Regression tests of the A09 review (references: testdata/gen_review_a09.py
// rows review/issue/*, replayed by TestDockerHAReviewCorpusMatchesPython).

func reviewBody() *jsonx.Object {
	return jsonx.Obj("server_url", "https://panel.test/", "agent_name", "new-dns", "agent_ip", "192.168.50.20",
		"data_dir", "/srv/oxidns", "oxidns_port", 8082, "network_mode", "host")
}

// A session file holding JSON null is Python's `session is None`: issue
// creates a new session; the existing index keeps its old id.
func TestReviewIssueNullSessionStartsANewSession(t *testing.T) {
	dataDir := t.TempDir()
	first, err := IssueEnrollment(dataDir, reviewBody(), "default")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	oldID := first.URL[strings.LastIndex(first.URL, "/")+1:]
	if err := os.WriteFile(filepath.Join(root, oldID+".json"), []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := IssueEnrollment(dataDir, reviewBody(), "default")
	if err != nil {
		t.Fatalf("python answers a new session, go: %v", err)
	}
	newID := second.URL[strings.LastIndex(second.URL, "/")+1:]
	if newID == oldID || second.Token == first.Token {
		t.Fatalf("expected a new session, got %s", second.URL)
	}
	if _, err := os.Stat(filepath.Join(root, newID+".json")); err != nil {
		t.Fatal(err)
	}
	indexes, _ := filepath.Glob(filepath.Join(root, "*.index"))
	if len(indexes) != 1 {
		t.Fatalf("indexes: %v", indexes)
	}
	if data, _ := os.ReadFile(indexes[0]); string(data) != oldID {
		t.Fatalf("index rewritten to %q", data)
	}
}

// body['agent_ip'] is a KeyError (500), not a session with host None.
func TestReviewIssueWithoutAgentIPIsAKeyError(t *testing.T) {
	body := reviewBody()
	body.Delete("agent_ip")
	_, err := IssueEnrollment(t.TempDir(), body, "default")
	var te *TypeError
	if !errors.As(err, &te) {
		t.Fatalf("want a non-ValueError, got %v", err)
	}
}

// root / (id + '.json') and with_suffix('.tmp') for hand-edited session ids.
func TestReviewSessionPathsFollowPathlib(t *testing.T) {
	for _, c := range []struct{ id, path, temp, err string }{
		{"ab", "/r/ab.json", "/r/ab.tmp", ""},
		{"", "/r/.json", "/r/.json.tmp", ""},
		{"a/", "/r/a/.json", "/r/a/.json.tmp", ""},
		{"a.b", "/r/a.b.json", "/r/a.b.tmp", ""},
		{"../x", "/r/../x.json", "/r/../x.tmp", ""},
		{"/abs/s", "/abs/s.json", "/abs/s.tmp", ""},
		{"x\x00y", "", "", "embedded null byte"},
	} {
		path, temp, err := sessionPaths("/r", c.id)
		if c.err != "" {
			var ve *ValueError
			if !errors.As(err, &ve) || ve.Message != c.err {
				t.Errorf("%q: want ValueError %q, got %v", c.id, c.err, err)
			}
			continue
		}
		if err != nil || path != c.path || temp != c.temp {
			t.Errorf("%q: got %q %q %v, want %q %q", c.id, path, temp, err, c.path, c.temp)
		}
	}
}
