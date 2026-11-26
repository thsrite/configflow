package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func fixtureRecord(id, digest string) *Record {
	return &Record{ID: id, ProfileID: "setup_synthetic", Name: "Synthetic", Goal: "gateway", Step: 3, MaxStep: 3,
		CreatedAt: "2026-01-01T00:00:00Z", SubscriptionIDs: []string{}, NodeIDs: []string{},
		Prepared: pointer(true), ProfileRevision: pointer(model.Int(1)), Completed: pointer(false), DigestVersion: pointer(digestVersion), OwnedDigest: &digest}
}

func setupRepository(t *testing.T) (*repo.Repository, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r, repo.WithScope(t.Context())
}

func TestRecordPreservesUnknownAndExplicitClear(t *testing.T) {
	raw := []byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_id":"setup_synthetic","future":{"secret":"synthetic-private","nested":[0,null]},"router_draft":null,"prepared":false,"completed":false,"profile_revision":0,"domain_rules":{"direct":[],"proxy":[],"future":7}}`)
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]json.RawMessage
	json.Unmarshal(raw, &a)
	json.Unmarshal(encoded, &b)
	// Undeclared members are dropped (O18 T1d); declared ones keep their
	// values, false and 0 included.
	if b["future"] != nil || string(b["profile_revision"]) != "0" || string(b["prepared"]) != "false" || string(b["domain_rules"]) != `{"direct":[],"proxy":[]}` {
		t.Fatal("stored fields", string(encoded))
	}
	if err := record.clear("router_draft"); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(record)
	if err != nil || bytes.Contains(encoded, []byte(`"router_draft"`)) {
		t.Fatalf("clear failed: %s %v", encoded, err)
	}
	if err := json.Unmarshal([]byte(`{"prepared":"wrong"}`), &record); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("invalid evidence accepted: %v", err)
	}
}

func TestAdoptOwnedWriteMatchesSessionOwnership(t *testing.T) {
	r, ctx := setupRepository(t)
	s := storeFor(r)
	before := json.RawMessage(`{"_revision":1,"mihomo":{"custom_config":"secret: old"},"proxy_groups":[],"rule_configs":[],"oxidns":{}}`)
	after := json.RawMessage(`{"_revision":2,"mihomo":{"custom_config":"secret: new"},"proxy_groups":[],"rule_configs":[],"oxidns":{}}`)
	oldDigest, _ := OwnedDigest(before, false)
	newDigest, _ := OwnedDigest(after, false)
	baseDigest, _ := OwnedDigest(after, true)
	for i := range 4 {
		id := strings.Repeat(string(rune('a'+i)), 32)
		record := fixtureRecord(id, oldDigest)
		switch i {
		case 1:
			record.Completed = pointer(true)
		case 2:
			record.ProfileID = "other"
		case 3:
			record.OwnedDigest = pointer("not-owned")
		}
		if err := s.write(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := AdoptOwnedWrite(ctx, r, "setup_synthetic", before, after); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		record, err := s.read(ctx, strings.Repeat(string(rune('a'+i)), 32))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if text(record.OwnedDigest) != newDigest || text(record.OwnedDigestWithoutBase) != baseDigest {
				t.Fatal("matching session was not adopted")
			}
		} else if text(record.OwnedDigest) == newDigest {
			t.Fatal("non-owned session adopted")
		}
	}
	info, err := os.Stat(filepath.Join(s.root, strings.Repeat("a", 32)+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private session permissions: %v %v", info, err)
	}
}

func TestSessionOwnerRuntimeCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			r, base := setupRepository(t)
			s := storeFor(r)
			before := json.RawMessage(`{"mihomo":{},"oxidns":{}}`)
			after := json.RawMessage(`{"mihomo":{"custom_config":"changed"},"oxidns":{}}`)
			digest, _ := OwnedDigest(before, false)
			record := fixtureRecord(strings.Repeat("a", 32), digest)
			if err := s.write(base, record); err != nil {
				t.Fatal(err)
			}
			path, _ := s.path(record.ID)
			original, _ := os.ReadFile(path)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(t.Context())
			t.Cleanup(func() { runtime.Shutdown(t.Context()) })
			request, cancel := context.WithCancel(base)
			defer cancel()
			ctx := runtime.BindRequest(request)
			if shutdown {
				runtime.Shutdown(t.Context())
			} else {
				cancel()
			}
			err := AdoptOwnedWrite(ctx, r, record.ProfileID, before, after)
			actual, _ := os.ReadFile(path)
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(original, actual) {
					t.Fatal("shutdown changed session", err)
				}
			} else if err != nil || bytes.Equal(original, actual) {
				t.Fatal("disconnect interrupted committed owner", err)
			}
		})
	}
}

func TestWriteOwnerRechecksGenerationAfterSessionWait(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			s, base := serviceFixture(t, "gateway")
			store := storeFor(s.repository)
			path, _ := store.path(testSessionID)
			before, _ := os.ReadFile(path)
			commits := s.repository.CommitCount()
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(base)
			defer cancel()
			origin := runtime.BindRequest(request)
			_, release, err := sessionLocks.Lock(base, path, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			done := make(chan error, 1)
			if err := runtime.Spawn(origin, "synthetic-wizard-wait", func(context.Context) {
				close(entered)
				_, err := s.Patch(origin, testSessionID, json.RawMessage(`{"step":0}`))
				done <- err
			}); err != nil {
				release()
				t.Fatal(err)
			}
			<-entered
			if shutdown {
				runtime.Shutdown(t.Context())
			} else {
				cancel()
			}
			release()
			err = <-done
			after, _ := os.ReadFile(path)
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) || commits != s.repository.CommitCount() {
					t.Fatal("waiting owner committed after shutdown", err)
				}
			} else if err != nil || bytes.Equal(before, after) {
				t.Fatal("disconnect canceled detached waiting owner", err)
			}
		})
	}
}
