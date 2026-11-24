package rulelibrary

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestN811SectionEditsPreserveUnchangedBytes(t *testing.T) {
	s := fixture(t)
	ctx := s.Context()
	if err := s.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", []byte(`[{"id":"untouched","name":"Untouched","url":"https://subscription.example/source","opaque":"<subscription>","number":1e+02}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", []byte(`{"opaque":"<backup>","number":1e+02}`))
	}); err != nil {
		t.Fatal(err)
	}
	read := func() ([]byte, []byte) {
		t.Helper()
		shared, err := s.Repo.Shared(ctx)
		if err != nil {
			t.Fatal(err)
		}
		system, err := s.Repo.System(ctx)
		if err != nil {
			t.Fatal(err)
		}
		subscriptions, err := model.Marshal(shared.Subscriptions)
		if err != nil {
			t.Fatal(err)
		}
		backup, err := model.Marshal(system.Backup)
		if err != nil {
			t.Fatal(err)
		}
		return subscriptions, backup
	}
	beforeSubscriptions, beforeBackup := read()
	// Undeclared members are dropped by the plain records (O18 T1c); the
	// declared values must stay unchanged by edits of other sections.
	if bytes.Contains(beforeSubscriptions, []byte("opaque")) || bytes.Contains(beforeBackup, []byte("opaque")) {
		t.Fatalf("undeclared member kept: %s %s", beforeSubscriptions, beforeBackup)
	}
	if err := editShared(s.Repo, ctx, func(_ context.Context, catalog *model.Shared) error {
		return catalog.SetJSON("rule_library", []byte(`[{"id":"record","name":"Record","source_type":"content","content":"+.records.example\n","behavior":"domain","format":"text"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := editSystem(s.Repo, ctx, func(system *model.System) error {
		system.SystemConfig.GitHubProxyDomain = "https://proxy.example"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	afterSubscriptions, afterBackup := read()
	if !bytes.Equal(beforeSubscriptions, afterSubscriptions) {
		t.Fatalf("unmodified subscriptions: %s -> %s", beforeSubscriptions, afterSubscriptions)
	}
	if !bytes.Equal(beforeBackup, afterBackup) {
		t.Fatalf("unmodified backup: %s -> %s", beforeBackup, afterBackup)
	}
}

func TestReviewN811EditSharedAndSystemInvalidateScopeOnlyOnSuccess(t *testing.T) {
	s := fixture(t)
	ctx := repo.WithScope(s.Context())
	if _, err := repo.Config(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	sharedSnap, err := repo.SharedConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("abort edit")
	if err := editShared(s.Repo, ctx, func(_ context.Context, catalog *model.Shared) error {
		if err := catalog.SetJSON("rule_library", []byte(`[{"id":"temp","name":"Temp","source_type":"content","content":"+.temp.example\n","behavior":"domain","format":"text"}]`)); err != nil {
			return err
		}
		return stop
	}); !errors.Is(err, stop) {
		t.Fatalf("expected stop error, got %v", err)
	}
	if err := editSystem(s.Repo, ctx, func(system *model.System) error {
		system.SystemConfig.GitHubProxyDomain = "https://failed.example"
		return stop
	}); !errors.Is(err, stop) {
		t.Fatalf("expected stop error, got %v", err)
	}
	// Failed transactions must not reset the Scope snapshot or baseline.
	cached, err := repo.Config(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.RuleLibrary) != 0 {
		t.Fatalf("failed editShared changed snapshot: %v", cached.RuleLibrary)
	}
	if err := editShared(s.Repo, ctx, func(_ context.Context, catalog *model.Shared) error {
		return catalog.SetJSON("rule_library", []byte(`[{"id":"saved","name":"Saved","source_type":"content","content":"+.saved.example\n","behavior":"domain","format":"text"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := repo.Config(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.RuleLibrary) != 1 {
		t.Fatalf("editShared did not invalidate Scope cache: %v", refreshed.RuleLibrary)
	}
	if err := repo.SaveSharedRecord(ctx, sharedSnap, nil); !errors.Is(err, repo.ErrInUse) {
		t.Fatalf("editShared did not clear Scope shared baseline: %v", err)
	}
	if err := editSystem(s.Repo, ctx, func(system *model.System) error {
		system.SystemConfig.GitHubProxyDomain = "https://mirror.example"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	afterSystem, err := repo.Config(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if got := afterSystem.SystemConfig.GitHubProxyDomain; got != "https://mirror.example" {
		t.Fatalf("editSystem did not invalidate Scope cache: %q", got)
	}
}

func TestReviewN811CheckContentRejectsNonStringFormat(t *testing.T) {
	svc := &Service{}
	for _, raw := range []string{
		`{"content":"example.com","behavior":"domain","format":123}`,
		`{"content":"example.com","behavior":"domain","format":true}`,
		`{"content":"example.com","behavior":"domain","format":[]}`,
		`{"content":"example.com","behavior":"domain","format":{}}`,
	} {
		result, err := svc.Check(decodeTestJSON[checkContentRequest](t, raw))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		errs := result.Errors
		if len(errs) != 1 || errs[0].Message != "规则集格式必须是 text、yaml 或 mrs。" {
			t.Fatalf("%s: expected format validation error, got %+v (%v)", raw, errs, err)
		}
	}
	for _, raw := range []string{
		`{"content":"example.com","behavior":"domain"}`,
		`{"content":"example.com","behavior":"domain","format":null}`,
		`{"content":"example.com","behavior":"domain","format":""}`,
	} {
		result, err := svc.Check(decodeTestJSON[checkContentRequest](t, raw))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		errs := result.Errors
		if len(errs) != 0 {
			t.Fatalf("%s: expected valid empty/null format, got %+v (%v)", raw, errs, err)
		}
	}
}
