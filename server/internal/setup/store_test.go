package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func fixtureRecord(id, digest string) *Record {
	return &Record{ID: id, ProfileID: "setup_synthetic", Name: "Synthetic", Goal: "gateway", Step: 3, MaxStep: 3,
		CreatedAt: "2026-01-01T00:00:00Z", SubscriptionIDs: []string{}, NodeIDs: []string{},
		Prepared: pointer(true), ProfileRevision: pointer(model.Int(1)), Completed: pointer(false), OwnedDigest: &digest}
}

func setupRepository(t *testing.T) (*repo.Repository, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r, repo.WithScope(repo.WithRepository(t.Context(), r))
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
	if !bytes.Equal(a["future"], b["future"]) || string(b["profile_revision"]) != "0" || string(b["prepared"]) != "false" {
		t.Fatal("lost stored fields", string(encoded))
	}
	if err := record.clear("router_draft"); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(record)
	if err != nil || bytes.Contains(encoded, []byte(`"router_draft"`)) || !bytes.Contains(encoded, a["future"]) {
		t.Fatalf("clear failed: %s %v", encoded, err)
	}
	if err := json.Unmarshal([]byte(`{"prepared":"wrong"}`), &record); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("invalid evidence accepted: %v", err)
	}
}

func TestOwnedDigestMatchesPython(t *testing.T) {
	type probe struct {
		Profile json.RawMessage `json:"profile"`
		Without bool            `json:"without"`
	}
	profiles := []json.RawMessage{
		[]byte(`{}`),
		[]byte(`{"_revision":7,"proxy_groups":[],"rule_configs":[],"mihomo":{"custom_config":"secret: synthetic\n","future":1.0},"oxidns":{"enabled":false,"list":[null,1.2,"中文😀"]},"routers":{"items":[]}}`),
		[]byte(`{"proxy_groups":[{"name":"<>&/中文"}],"mihomo":null,"oxidns":false,"rule_configs":[]}`),
	}
	tests := []probe{}
	for _, profile := range profiles {
		for _, without := range []bool{false, true} {
			tests = append(tests, probe{profile, without})
		}
	}
	raw, _ := json.Marshal(tests)
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import json,sys;from backend.common.setup_flow import owned_digest;json.dump([owned_digest(v['profile'],base=not v['without']) for v in json.load(sys.stdin)],sys.stdout)`)
	cmd.Dir = "../../.."
	cmd.Stdin = bytes.NewReader(raw)
	result, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python: %v %s", err, result)
	}
	var want []string
	if err := json.Unmarshal(result, &want); err != nil {
		t.Fatal(err)
	}
	for i, test := range tests {
		got, err := OwnedDigest(test.Profile, test.Without)
		if err != nil || got != want[i] {
			t.Fatalf("digest case %d: %s != %s: %v", i, got, want[i], err)
		}
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
