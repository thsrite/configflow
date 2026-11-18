package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestN82ReviewUnchangedMetadataIsNoop(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(5))
	meta := map[string]json.RawMessage{"opaque": json.RawMessage(` { "z":1.0,"a":null } `)}
	base := must(s.Create(rawFixture(t), meta))(t)
	// Legal Python-readable formatting must survive a no-op too.
	execSQL(t, s.Path(), `UPDATE meta SET value=' 1 ' WHERE key='generation'`)
	stamp := must(s.Fingerprint())(t)
	_, changed, err := s.Commit(base, base.Document, meta)
	if err != nil || changed || must(s.Fingerprint())(t) != stamp {
		t.Fatalf("unchanged metadata wrote rows: changed=%v err=%v", changed, err)
	}

	before := fieldRows(t, s.Path())
	next, changed, err := s.Commit(base, base.Document, map[string]json.RawMessage{"opaque": json.RawMessage(`{"changed":true}`)})
	if err != nil || !changed || next.Head != base.Head {
		t.Fatal("metadata-only update changed head")
	}
	if !bytes.Equal(must(s.Meta("generation"))(t), []byte(" 1 ")) {
		t.Fatal("metadata-only update rewrote generation bytes")
	}
	for key, row := range before {
		if fieldRows(t, s.Path())[key] != row {
			t.Fatal("metadata-only update changed field revision/bytes")
		}
	}
}

func TestN82ReviewCorruptRowsAreSafe(t *testing.T) {
	const secret = "synthetic-password-and-token-never-log"
	tests := []struct{ name, table, key, value, category string }{
		{"bad-json", "fields", "system//agents", `{"token":"` + secret + `",]`, msgInvalidJSON},
		{"truncated-json", "fields", "system//agents", `["` + secret + `"`, msgInvalidJSON},
		{"utf8", "fields", "system//agents", "\"" + secret + "\xff\"", msgInvalidUTF8},
		{"metadata-json", "meta", "future", `{"token":"` + secret + `"`, msgInvalidJSON},
		{"metadata-utf8", "meta", "future", "\"" + secret + "\xff\"", msgInvalidUTF8},
		{"excessive-depth", "fields", "system//agents", strings.Repeat("[", 10001) + `"` + secret + `"` + strings.Repeat("]", 10001), msgInvalidJSON},
		{"large-corrupt", "fields", "system//agents", `["` + strings.Repeat("x", 10<<20) + secret + `"`, msgInvalidJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(filepath.Join(t.TempDir(), Filename), seconds(5))
			must(s.Create(rawFixture(t), nil))(t)
			c := must(connect(s.Path(), false, 0))(t)
			var err error
			if tt.table == "fields" {
				err = c.exec("UPDATE fields SET value=? WHERE scope='system' AND field='agents'", tt.value)
			} else {
				err = c.exec("INSERT INTO meta (key,value) VALUES (?,?)", tt.key, tt.value)
			}
			c.close()
			if err != nil {
				t.Fatal(err)
			}
			before := must(os.ReadFile(s.Path()))(t)
			if tt.table == "fields" {
				_, err = s.Load()
			} else {
				_, err = s.Meta(tt.key)
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("expected corruption, got %T", err)
			}
			text := err.Error()
			if len(text) > 400 || strings.Contains(text, secret) || !strings.Contains(text, tt.table) || !strings.Contains(text, tt.key) || !strings.Contains(text, tt.category) || !strings.Contains(text, "字节") {
				t.Fatal("unsafe/incomplete diagnostic")
			}
			if after := must(os.ReadFile(s.Path()))(t); !bytes.Equal(before, after) {
				t.Fatal("failed read changed database bytes")
			}
		})
	}
}

func TestN82ReviewLargeOpaqueRowsAndConcurrentRawCommits(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(10))
	doc := rawFixture(t)
	value := json.RawMessage(` { "wrong_type":"kept", "large":"` + strings.Repeat("例", 3<<20) + `", "negative_zero":-0.0 } `)
	doc.Shared.Fields = append(doc.Shared.Fields, Field{Name: "large", Value: value})
	base := must(s.Create(doc, nil))(t)
	if _, changed, err := s.Commit(base, base.Document, nil); err != nil || changed {
		t.Fatal("large no-op failed")
	}
	const writers = 8
	var wg sync.WaitGroup
	results := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			proposal := rawFixture(t)
			proposal.Shared.Fields = append(proposal.Shared.Fields, Field{Name: "large", Value: value}, Field{Name: "writer", Value: json.RawMessage{byte('0' + i)}})
			_, changed, err := s.Commit(base, proposal, nil)
			if err == nil && !changed {
				err = errors.New("empty commit")
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	final := must(s.Load())(t)
	if wins != 1 || final.Head.Generation != base.Head.Generation+1 {
		t.Fatal("lost update/conflict")
	}
	for _, f := range final.Document.Shared.Fields {
		if f.Name == "large" && !bytes.Equal(f.Value, value) {
			t.Fatal("opaque row was rewritten")
		}
	}
}

// The compatibility Commit entry has no context and delegates to Background.
// Cancelling an unrelated request must not change it; shutdown-aware owners use
// CommitContext instead (O3-6c).
func TestN82ReviewCommitFinishesAfterCancellation(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(5))
	base := must(s.Create(rawFixture(t), nil))(t)
	proposal := rawFixture(t)
	proposal.Root = append(proposal.Root, Field{Name: "cancel_probe", Value: json.RawMessage(`true`)})
	proposal.Order = append(proposal.Order, "cancel_probe")
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	s.BeforeCommit = func() error { close(started); <-release; return nil }
	result := make(chan error, 1)
	go func() { _, _, err := s.Commit(base, proposal, nil); result <- err }()
	<-started
	cancel()
	if request.Err() != context.Canceled {
		t.Fatal("request not canceled")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	final := must(s.Load())(t)
	if final.Head.Generation != base.Head.Generation+1 || len(final.Document.Root) != len(base.Document.Root)+1 {
		t.Fatal("request cancellation aborted commit")
	}
}

func TestN82ReviewHugeVersionDiagnosticIsBounded(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(5))
	must(s.Create(rawFixture(t), nil))(t)
	c := must(connect(s.Path(), false, 0))(t)
	if err := c.exec("UPDATE meta SET value=? WHERE key='storage_version'", strings.Repeat("9", 100000)); err != nil {
		t.Fatal(err)
	}
	c.close()
	before := must(os.ReadFile(s.Path()))(t)
	err := s.Check()
	if !errors.Is(err, ErrUnsupported) || err.Error() != msgVersionTooLarge {
		t.Fatal("huge version diagnostic not bounded")
	}
	if !bytes.Equal(before, must(os.ReadFile(s.Path()))(t)) {
		t.Fatal("check changed huge version row")
	}
}

func TestN82ReviewVersionMustBeIntegerBeforeRangeCheck(t *testing.T) {
	for _, raw := range []string{"999999999999999999999999999.0", "999999999999999999999999999e0", "-999999999999999999999999999"} {
		t.Run(raw, func(t *testing.T) {
			s := New(filepath.Join(t.TempDir(), Filename), seconds(5))
			must(s.Create(rawFixture(t), nil))(t)
			c := must(connect(s.Path(), false, 0))(t)
			if err := c.exec("UPDATE meta SET value=? WHERE key='storage_version'", raw); err != nil {
				t.Fatal(err)
			}
			c.close()
			if err := s.Check(); !errors.Is(err, ErrCorrupt) || err.Error() != msgVersionInvalid {
				t.Fatal("non-positive-integer version misclassified")
			}
		})
	}
}
