package rulelibrary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/model"
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
	cache, err := rulefetch.ReadCache(s.Context(), ruleRecord(item), nil)
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
	if _, err := repo.Config(reader, "default"); err != nil {
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
		if !s.Repo.InTransaction(ctx) {
			t.Fatal("cache publication is not serialized with repository editors")
		}
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
	if _, err = service.SaveDraft(s.Context(), "manual", draftBody(t, token, "+.committed.example\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != "+.committed.example\n" {
		t.Fatalf("old reader overwrote committed cache: %q", got)
	}
}

func TestReviewStageRejectsNewerCommittedDeclaration(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	first, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	first.Content = new("+.first-commit.example\n")
	stage, err := stageCache(s.Context(), first, []byte(model.TextValue(first.Content)), nil)
	if err != nil {
		t.Fatal(err)
	}
	backup := stage.path + ".bak"
	if err = os.WriteFile(backup, []byte("+.prior-backup.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	backupBefore, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	// C33's DB commit is followed by a different owner's fresh transaction
	// before C33 reaches its cache publication. No sleeps or mocked stores.
	for _, content := range []string{"+.first-commit.example\n", "+.newer-owner.example\n"} {
		err = s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
			c := content
			p.RuleLibrary[0].Content = &c
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	before := doc(t, s)
	if err = stage.commit(s.Context()); !errors.Is(err, errStaleCacheStage) {
		t.Fatalf("published an obsolete stage: %v", err)
	}
	assertCacheBackup(t, stage.path, "+.prior-backup.example\n")
	backupAfter, err := os.Stat(backup)
	if err != nil || !os.SameFile(backupBefore, backupAfter) || !backupBefore.ModTime().Equal(backupAfter.ModTime()) {
		t.Fatal("obsolete stage touched backup", err)
	}
	if doc(t, s) != before {
		t.Fatal("cache guard changed DB/revision")
	}
	if _, err = os.Stat(stage.marker); err != nil {
		t.Fatal("obsolete stage cleared invalidation", err)
	}
	if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != "+.newer-owner.example\n" {
		t.Fatalf("obsolete stage resurrected old bytes: %q", got)
	}
	if _, err = (&Service{client: rulefetch.DefaultClient}).Cache(s.Context(), decodeTestJSON[cacheRequest](t, `{"rule_ids":["manual"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(stage.marker); !os.IsNotExist(err) {
		t.Fatal("manual repair did not clear invalidation", err)
	}
	content, err := os.ReadFile(stage.path)
	if err != nil || string(content) != "+.newer-owner.example\n" {
		t.Fatalf("manual repair did not publish the current DB: %q %v", content, err)
	}
}

func TestReviewStageAllowsMetadataAndPreservesPreparedBytes(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	prepared := "+.prepared-legacy.example\n"
	stage, err := stageCache(s.Context(), item, []byte(prepared), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		p.RuleLibrary[0].Name = "Renamed"
		v := model.Int(3600)
		p.RuleLibrary[0].UpdateInterval = &v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := doc(t, s)
	if err = stage.commit(s.Context()); err != nil {
		t.Fatal("independent metadata blocked cache publication", err)
	}
	content, err := os.ReadFile(stage.path)
	if err != nil || string(content) != prepared {
		t.Fatalf("prepared raw cache changed: %q %v", content, err)
	}
	if doc(t, s) != before {
		t.Fatal("cache-only publication changed metadata/revisions")
	}
	if _, err = os.Stat(stage.marker); !os.IsNotExist(err) {
		t.Fatal("successful publication retained invalidation", err)
	}
}

func TestReviewAbortClearsOnlyItsUnchangedBaseline(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "metadata-only"
		if changed {
			name = "source-changed"
		}
		t.Run(name, func(t *testing.T) {
			s := fixture(t)
			createManual(t, s)
			next, err := source(s.Context(), "manual")
			if err != nil {
				t.Fatal(err)
			}
			next.Content = new("+.rejected-proposal.example\n")
			stage, err := stageCache(s.Context(), next, []byte(model.TextValue(next.Content)), nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "+.old.example\n"
			if changed {
				want = "+.new-owner.example\n"
			}
			err = s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
				v := model.Int(7200)
				p.RuleLibrary[0].UpdateInterval = &v
				if changed {
					w := want
					p.RuleLibrary[0].Content = &w
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			before := doc(t, s)
			stage.abort(s.Context())
			_, markerErr := os.Stat(stage.marker)
			if (changed && markerErr != nil) || (!changed && !os.IsNotExist(markerErr)) {
				t.Fatalf("abort invalidation: changed=%t err=%v", changed, markerErr)
			}
			if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != want {
				t.Fatalf("abort exposed rejected/old cache: %q", got)
			}
			if doc(t, s) != before {
				t.Fatal("abort changed DB/revisions")
			}
			if _, err = os.Stat(stage.temp); !os.IsNotExist(err) {
				t.Fatal("abort retained its private temp", err)
			}
		})
	}
}

func TestReviewCreateUsesTheSourceCleanupLease(t *testing.T) {
	s := fixture(t)
	item := decodeTestJSON[libraryRequest](t, `{"id":"created","name":"Created","source_type":"content","behavior":"domain","format":"text","content":"+.created.example\n"}`)
	path, err := cachePath(s.Context(), item.previewItem(""))
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	s.Repo.Store().BeforeCommit = func() error {
		checked = true
		if !edits.Held(path) {
			t.Error("creation can commit during an old owner's edit/delete cleanup")
		}
		return nil
	}
	defer func() { s.Repo.Store().BeforeCommit = nil }()
	if _, err = (&Service{client: rulefetch.DefaultClient}).Create(s.Context(), item); err != nil {
		t.Fatal(err)
	}
	if !checked || edits.Held(path) {
		t.Fatal("creation commit observation or lease release failed")
	}
}

func TestReviewCreateDoesNotHoldCleanupLeaseDuringDownload(t *testing.T) {
	s := fixture(t)
	service := &Service{client: rulefetch.DefaultClient}
	winner := decodeTestJSON[libraryRequest](t, `{"id":"same","name":"Winner","source_type":"content","behavior":"domain","format":"text","content":"+.winner.example\n"}`)
	winnerResult := make(chan error, 1)
	var once sync.Once
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The real download cannot finish until a same-ID inline creation
		// commits. Taking the cleanup lease before network I/O would block it.
		once.Do(func() {
			_, err := service.Create(s.Context(), winner)
			winnerResult <- err
		})
		_, _ = w.Write([]byte("+.late-download.example\n"))
	}))
	defer origin.Close()
	remote := decodeTestJSON[libraryRequest](t, `{"id":"same","name":"Remote","source_type":"url","behavior":"domain","format":"text","url":`+jsonQuote(t, origin.URL+"/source")+`}`)
	_, err := service.Create(s.Context(), remote)
	var conflict *fault
	if !errors.Is(err, repo.ErrExists) && (!errors.As(err, &conflict) || conflict.status != 409) {
		t.Fatalf("late download replaced/blocked the inline winner: %v", err)
	}
	select {
	case err := <-winnerResult:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("the actual download did not admit the inline winner")
	}
	current, err := source(s.Context(), "same")
	if err != nil || model.TextValue(current.Content) != "+.winner.example\n" {
		t.Fatal("inline winner was overwritten", err)
	}
	path, err := cachePath(s.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "+.winner.example\n" {
		t.Fatalf("late download overwrote the winner cache: %q %v", content, err)
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
	if _, err := repo.Config(reader, "default"); err != nil {
		t.Fatal(err)
	}
	service := &Service{client: rulefetch.DefaultClient}
	draft, err := service.Draft(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(s.Context(), "manual", draftBody(t, draft.Revision, "+.committed.example\n")); err != nil {
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
	if _, err = (&Service{client: rulefetch.DefaultClient}).SaveDraft(s.Context(), "manual", draftBody(t, token, "+.committed.example\n")); err != nil {
		t.Fatal(err)
	}
	before := doc(t, s)
	// Discovery/Agent/compose owners can finish their post-commit cache work
	// after a newer C33 edit. Exercise their actual shared write entry point.
	if _, err = rulefetch.SaveSource(s.Context(), ruleRecord(old), nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/api/rules/local/Manual", s.Login()).WantStatus(200).Text(); got != "+.committed.example\n" {
		t.Fatalf("late shared writer replaced draft bytes: %q", got)
	}
	if doc(t, s) != before {
		t.Fatal("stale cache suppression changed DB/revision")
	}
}
