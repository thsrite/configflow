package setupflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileSessions(t *testing.T) {
	dir := t.TempDir()
	records, e := ProfileSessions(context.Background(), dir, "alpha")
	if e != nil || records == nil || len(records) != 0 {
		t.Fatal(records, e)
	}
	root := filepath.Join(dir, "setup-sessions")
	if e = os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	files := map[string]string{strings.Repeat("a", 32) + ".json": `{"profile_id":"alpha","updated_at":"2026-01-01","unknown":{"keep":true},"goal":"gateway"}`, strings.Repeat("b", 32) + ".json": `{"profile_id":"alpha","updated_at":"2026-02-01","abandoned":true}`, strings.Repeat("c", 32) + ".json": `{"profile_id":"beta"}`, strings.Repeat("d", 32) + ".json": `[]`, "invalid.json": `broken`, strings.Repeat("e", 32) + ".lock": `broken`}
	for name, raw := range files {
		if e = os.WriteFile(filepath.Join(root, name), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	records, e = ProfileSessions(context.Background(), dir, "alpha")
	if e != nil || len(records) != 2 {
		t.Fatal(records, e)
	}
	if records[0].UpdatedAt != "2026-02-01" || !strings.Contains(string(records[1].Raw), `"unknown":{"keep":true}`) {
		t.Fatal("sort or unknown data lost", records)
	}
	if e = os.WriteFile(filepath.Join(root, strings.Repeat("f", 32)+".json"), []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ProfileSessions(context.Background(), dir, "alpha"); e == nil {
		t.Fatal("broken record must fail closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = ProfileSessions(ctx, dir, "alpha"); e != context.Canceled {
		t.Fatal(e)
	}
}

func TestProfileSessionsRefusesWrongTypedEvidence(t *testing.T) {
	for _, body := range []string{`{"profile_id":7}`, `{"profile_id":"alpha","goal":[]}`, `{"profile_id":"alpha","updated_at":4}`, `{"profile_id":"alpha","completed":"yes"}`, `{"profile_id":"alpha","abandoned":{}}`} {
		t.Run(body, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "setup-sessions")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, strings.Repeat("a", 32)+".json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := ProfileSessions(context.Background(), filepath.Dir(root), "alpha")
			if !errors.Is(err, ErrReadSession) || !strings.Contains(err.Error(), msgReadSession) {
				t.Fatal("must fail closed in Chinese", err)
			}
		})
	}
}

func FuzzSetupSession(f *testing.F) {
	for _, raw := range []string{`{}`, `[]`, `null`, `{"profile_id":"alpha","goal":"router","completed":true}`, `{"profile_id":"alpha","goal":[]}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 1<<20 {
			return
		}
		root := filepath.Join(t.TempDir(), "setup-sessions")
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, strings.Repeat("a", 32)+".json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		sessions, err := ProfileSessions(context.Background(), filepath.Dir(root), "alpha")
		if err != nil {
			return
		}
		for _, s := range sessions {
			if s.ProfileID != "alpha" || !json.Valid(s.Raw) {
				t.Fatal("invalid session escaped")
			}
		}
	})
}

func TestReadSessionSharesProfileReaderBoundary(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("a", 32)
	if _, err := ReadSession(t.Context(), dir, id); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing file classification", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-sessions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created directory")
	}
	root := filepath.Join(dir, "setup-sessions")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"native", []byte(`{"profile_id":"synthetic","future":[null,1],"completed":false}`), true},
		{"invalid-json", []byte(`{`), false},
		{"invalid-utf8", []byte{'"', 255, '"'}, false},
		{"over-limit", []byte(strings.Repeat(" ", MaxSessionBytes+1)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, id+".json"), test.body, 0600); err != nil {
				t.Fatal(err)
			}
			raw, err := ReadSession(t.Context(), dir, id)
			_, profileErr := ProfileSessions(t.Context(), dir, "synthetic")
			if test.valid {
				if err != nil || profileErr != nil || string(raw) != string(test.body) {
					t.Fatal("raw record changed", err, profileErr)
				}
			} else if !errors.Is(err, ErrInvalidSession) || !errors.Is(profileErr, ErrInvalidSession) {
				t.Fatal("reader policies diverged", err, profileErr)
			}
		})
	}
	if _, err := ReadSession(t.Context(), dir, "../outside"); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("invalid identity accepted", err)
	}
}
