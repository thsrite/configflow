package rulelibrary

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/rulefetch"
)

func assertCacheBackup(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path + ".bak")
	if err != nil || string(data) != want {
		t.Fatalf("backup bytes: %q, want %q: %v", data, want, err)
	}
	info, err := os.Stat(path + ".bak")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions: %v %v", info, err)
	}
	if _, err = os.Stat(path + ".bak.bak"); !os.IsNotExist(err) {
		t.Fatal("backup rotation created a second-level backup", err)
	}
}

func TestCachePublicationRetainsPreviousPrimary(t *testing.T) {
	for _, previous := range []string{"+.old.example\n", ""} {
		t.Run(previous, func(t *testing.T) {
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
			if err = os.WriteFile(path, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			s.Put("/api/rule-library/manual", s.Login(), httpxtest.JSON(map[string]string{
				"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.new.example\n",
			})).WantStatus(200)
			assertCacheBackup(t, path, previous)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "+.new.example\n" {
				t.Fatalf("primary not published: %q %v", data, err)
			}
		})
	}
}

func TestCacheBackupParticipatesInUnchangedCheck(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	s.Put("/api/rule-library/manual", s.Login(), httpxtest.JSON(map[string]string{"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.new.example\n"})).WantStatus(200)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	current := []byte("+.new.example\n")
	stamp := time.Unix(1, 0)
	if err = os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	assertCacheBackup(t, path, "+.old.example\n")
	shared, err := s.Repo.SharedPath(".")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(shared, path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	written, err := s.Repo.WriteSharedTextIfChanged(s.Context(), relative, current)
	if err != nil || written != path {
		t.Fatal(written, err)
	}
	replaced, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(previous, replaced) {
		t.Fatal("previous-version backup incorrectly qualified as unchanged")
	}
	assertCacheBackup(t, path, string(current))
	if _, err = (&Service{client: rulefetch.DefaultClient}).Cache(s.Context(), decodeTestJSON[cacheRequest](t, `{"rule_ids":["manual"]}`)); err != nil {
		t.Fatal(err)
	}
	assertCacheBackup(t, path, string(current))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	backupBefore, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	written, err = s.Repo.WriteSharedTextIfChanged(s.Context(), relative, current)
	if err != nil || written != path {
		t.Fatal(written, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !after.ModTime().After(stamp) {
		t.Fatal("unchanged cache was replaced or not touched", err)
	}
	backupAfter, err := os.Stat(path + ".bak")
	if err != nil || !os.SameFile(backupBefore, backupAfter) || !backupBefore.ModTime().Equal(backupAfter.ModTime()) {
		t.Fatal("unchanged backup was replaced or touched", err)
	}
}

func TestCacheBackupProtectsOrphanIdentity(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	s.Put("/api/rule-library/manual", s.Login(), httpxtest.JSON(map[string]string{"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.new.example\n"})).WantStatus(200)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	s.Delete("/api/rule-library/manual", s.Login()).WantStatus(200)
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("deleted source retained primary", err)
	}
	assertCacheBackup(t, path, "+.old.example\n")
	before := doc(t, s)
	s.Post("/api/rule-library", s.Login(), httpxtest.JSON(map[string]string{
		"id": "manual", "name": "Replacement", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.replacement.example\n",
	})).WantStatus(409)
	if doc(t, s) != before {
		t.Fatal("orphan backup refusal changed DB")
	}
	assertCacheBackup(t, path, "+.old.example\n")
}

func TestCacheBackupFinishesAfterRenameShutdown(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	runtime, ctx, _ := runtimeRequest(t, s.Context())
	service := &Service{client: rulefetch.DefaultClient, replaceCache: func(ctx context.Context, from, to string) error {
		if err := replaceFile(ctx, from, to); err != nil {
			return err
		}
		runtime.Shutdown(t.Context())
		return nil
	}}
	token, err := revision(item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(ctx, "manual", draftBody(t, token, "+.new.example\n")); err != nil {
		t.Fatal("post-rename shutdown aborted committed publication", err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	assertCacheBackup(t, path, "+.old.example\n")
	if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
		t.Fatal("post-rename shutdown retained invalidation", err)
	}
}

func TestCacheBackupFailureDoesNotFailPrimary(t *testing.T) {
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
	// A nonempty directory at the backup destination makes only its rename fail.
	if err = os.Mkdir(path+".bak", 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path+".bak", "keep"), []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Put("/api/rule-library/manual", s.Login(), httpxtest.JSON(map[string]string{"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.new.example\n"})).WantStatus(200)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "+.new.example\n" {
		t.Fatalf("backup failure lost primary: %q %v", data, err)
	}
	if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
		t.Fatal("backup failure retained invalidation", err)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rule-edit-*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatal("backup failure leaked temporary files", temps, err)
	}
}
