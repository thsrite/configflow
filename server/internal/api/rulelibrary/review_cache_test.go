package rulelibrary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

func TestReviewUnreadableReceiptCannotReviveLegacyCache(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".invalid", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.DataDir, "shared/rules/Manual.list"), []byte("+.stale-legacy.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err = os.Chmod(dir, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if _, err = os.Stat(path + ".invalid"); !os.IsPermission(err) {
		t.Fatalf("fixture must deny marker lookup: %v", err)
	}
	raw, err := legacy(item)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := rulefetch.ReadValidatedRuleCache(s.Context(), raw, nil)
	if err != nil || cache != nil {
		t.Fatalf("unreadable invalidation served cache: %q %v", cache, err)
	}
	response := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200)
	if response.Text() != "+.old.example\n" {
		t.Fatalf("delivery revived legacy bytes: %q", response.Text())
	}
}

func TestReviewOldInlineReaderCannotOverwriteCommittedDraft(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	// A request keeps its compat snapshot while an editor commits a newer DB.
	reader := s.Context()
	if _, err := repo.GetConfig(reader, "default"); err != nil {
		t.Fatal(err)
	}
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	token, err := revision(item)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{client: rulefetch.DefaultClient, replaceCache: func(ctx context.Context, from, to string) error {
		if err := replaceFile(ctx, from, to); err != nil {
			return err
		}
		// Run that already-admitted reader after the actual rename, before the
		// receipt is cleared. Its old response is allowed; its cache write is not.
		out, err := (&apirules.Service{}).Local(reader, "default", "Manual", "", "http://localhost")
		if err != nil || string(out.Content) != "+.old.example\n" {
			t.Fatalf("old request snapshot: %q %v", out.Content, err)
		}
		return nil
	}}
	body := recordJSON(t, `{"content":"+.committed.example\n"}`)
	if err = put(&body, "revision", token); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(s.Context(), "manual", body); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != "+.committed.example\n" {
		t.Fatalf("old reader overwrote committed cache: %q", got)
	}
}

func TestReviewInlineFallbackDoesNotPublishOutsideEditLease(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	// This can be an uncommitted stage or an interrupted committed edit. The
	// reader cannot tell which, and must never publish its request snapshot.
	stale := "+.stale-cache.example\n"
	if err = os.WriteFile(path, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".invalid", nil, 0600); err != nil {
		t.Fatal(err)
	}
	s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != stale {
		t.Fatalf("read published an unlocked inline snapshot: %q %v", got, err)
	}
	if _, err = os.Stat(path + ".invalid"); err != nil {
		t.Fatal("reader cleared the editor's receipt", err)
	}
}

func TestReviewColdInlineReaderPopulatesCurrentDeclaration(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	reader := s.Context()
	if _, err := repo.GetConfig(reader, "default"); err != nil {
		t.Fatal(err)
	}
	service := &Service{client: rulefetch.DefaultClient}
	draft, err := service.Draft(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	body := recordJSON(t, `{"content":"+.committed.example\n"}`)
	if err = put(&body, "revision", draft.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(s.Context(), "manual", body); err != nil {
		t.Fatal(err)
	}
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	before := doc(t, s)
	out, err := (&apirules.Service{}).Local(reader, "default", "Manual", "", "http://localhost")
	if err != nil || string(out.Content) != "+.old.example\n" {
		t.Fatalf("old request snapshot: %q %v", out.Content, err)
	}
	cache, err := os.ReadFile(path)
	if err != nil || string(cache) != "+.committed.example\n" {
		t.Fatalf("cold reader published an old snapshot: %q %v", cache, err)
	}
	if doc(t, s) != before {
		t.Fatal("cache-only read changed DB or revisions")
	}
}

func TestReviewOtherInlineWritersCannotOverwriteCommittedDraft(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	old, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	token, err := revision(old)
	if err != nil {
		t.Fatal(err)
	}
	body := recordJSON(t, `{"content":"+.committed.example\n"}`)
	if err = put(&body, "revision", token); err != nil {
		t.Fatal(err)
	}
	if _, err = (&Service{client: rulefetch.DefaultClient}).SaveDraft(s.Context(), "manual", body); err != nil {
		t.Fatal(err)
	}
	raw, err := legacy(old)
	if err != nil {
		t.Fatal(err)
	}
	before := doc(t, s)
	// Discovery/Agent/compose owners can finish their post-commit cache work
	// after a newer C33 edit. Exercise their actual shared write entry point.
	if _, err = rulefetch.SaveRuleToLocal(s.Context(), raw, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != "+.committed.example\n" {
		t.Fatalf("late shared writer replaced draft bytes: %q", got)
	}
	if doc(t, s) != before {
		t.Fatal("stale cache suppression changed DB/revision")
	}
}
