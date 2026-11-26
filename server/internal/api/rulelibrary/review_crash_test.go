package rulelibrary

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

// Re-execution leaves real SQLite and cache files behind a SIGKILL. No defer,
// repository close or in-process recovery is allowed to repair the child state.
func TestReviewCacheCrashRecovery(t *testing.T) {
	for _, point := range []string{"staged", "committed", "renamed", "complete"} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			child := exec.Command(os.Args[0], "-test.run=^TestReviewCacheCrashChild$")
			child.Env = append(os.Environ(), "C33_REVIEW_CRASH="+point, "C33_REVIEW_DATA="+dir)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatalf("child did not reach crash point: %v %s", err, output)
			}
			s := httpxtest.New(t, httpxtest.Options{DataDir: dir, Auth: true})
			apirules.New(apirules.Deps{Repo: s.Repo}).Register(s.App.Router)
			New(Deps{Repo: s.Repo}).Register(s.App.Router)
			svc := &Service{repo: s.Repo, client: rulefetch.DefaultClient}
			item, err := svc.source(s.Context(), "manual")
			if err != nil {
				t.Fatal(err)
			}
			want := "+.committed.example\n"
			if point == "staged" {
				want = "+.old.example\n"
			}
			if model.TextValue(item.Content) != want {
				t.Fatalf("unexpected reopened DB: %s", model.TextValue(item.Content))
			}
			path, err := svc.cachePath(s.Context(), item)
			if err != nil {
				t.Fatal(err)
			}
			backup := "+.prior-backup.example\n"
			if point == "complete" {
				backup = "+.old.example\n"
			}
			assertCacheBackup(t, path, backup)
			raw := ruleRecord(item)
			cache, err := rulefetch.ReadCache(s.Context(), s.Repo, raw, nil)
			if err != nil || (point != "complete" && cache != nil) || (point == "complete" && string(cache) != want) {
				t.Fatalf("reopened cache: %q %v", cache, err)
			}
			for _, route := range []string{"/api/rule-library/content/manual", "/api/profiles/default/rule-library/content/manual", "/api/rules/local/Manual"} {
				if got := s.Get(route, s.Login()).WantStatus(200).Text(); got != want {
					t.Fatalf("restart delivery %s: %q", route, got)
				}
			}
			if _, err = svc.Cache(s.Context(), decodeTestJSON[cacheRequest](t, `{"rule_ids":["manual"]}`)); err != nil {
				t.Fatal(err)
			}
			previousPrimary := "+.old.example\n"
			if point == "renamed" || point == "complete" {
				previousPrimary = "+.committed.example\n"
			}
			assertCacheBackup(t, path, previousPrimary)
			if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
				t.Fatal("restart repair retained invalidation", err)
			}
			temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rule-edit-*.tmp"))
			if err != nil || len(temps) != 0 {
				t.Fatalf("restart repair retained stages: %v %v", temps, err)
			}
			cache, err = rulefetch.ReadCache(s.Context(), s.Repo, raw, nil)
			if err != nil || string(cache) != want {
				t.Fatalf("repaired cache: %q %v", cache, err)
			}
		})
	}
}

func TestReviewCacheCrashChild(t *testing.T) {
	point := os.Getenv("C33_REVIEW_CRASH")
	if point == "" {
		return
	}
	s := httpxtest.New(t, httpxtest.Options{DataDir: os.Getenv("C33_REVIEW_DATA"), Auth: true})
	New(Deps{Repo: s.Repo}).Register(s.App.Router)
	svc := &Service{repo: s.Repo, client: rulefetch.DefaultClient}
	createManual(t, s)
	item, err := svc.source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := svc.cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".bak", []byte("+.prior-backup.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	crash := func() {
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	if point == "staged" {
		if _, err = svc.stageCache(s.Context(), item, []byte("+.committed.example\n"), nil); err != nil {
			t.Fatal(err)
		}
		crash()
	}
	service := &Service{repo: s.Repo, client: rulefetch.DefaultClient, replaceCache: func(ctx context.Context, from, to string) error {
		if point == "committed" {
			crash()
		}
		if err := replaceFile(ctx, from, to); err != nil {
			return err
		}
		if point == "renamed" {
			crash()
		}
		return nil
	}}
	token, err := revision(item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveDraft(s.Context(), "manual", draftBody(t, token, "+.committed.example\n")); err != nil {
		t.Fatal(err)
	}
	crash()
}
