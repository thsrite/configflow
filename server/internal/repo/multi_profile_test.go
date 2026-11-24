package repo

// Port of backend/test_multi_profile_repository.py. Helpers carry the
// prefix "mp". Cases about the pre-SQLite migration are not ported (see the
// hand-back); cases that reached into Python internals test the same
// observable behaviour through the Go seams (Store().BeforeCommit,
// tokenURLSafe, a re-executed test binary as the second process).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/model"
)

const mpHelperEnv = "CONFIGFLOW_REPO_MP_HELPER"

// TestMPHelperProcess is the second process of the lock tests, not a test:
// it runs only when re-executed with mpHelperEnv set.
//
//	hold:<lock>:<ready file>:<release file>  hold the lock until the release file exists
//	crash:<lock>:<ready file>                exit(23) while holding the lock
func TestMPHelperProcess(t *testing.T) {
	spec := os.Getenv(mpHelperEnv)
	if spec == "" {
		return
	}
	parts := strings.Split(spec, "\x1f")
	err := withLock(context.Background(), parts[1], func(context.Context) error {
		if err := os.WriteFile(parts[2], []byte("ready"), 0o600); err != nil {
			return err
		}
		if parts[0] == "crash" {
			os.Exit(23)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(parts[3]); err == nil {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return errors.New("parent did not release holder")
	})
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

// mpHelper starts the helper process and waits until it holds the lock.
func mpHelper(t *testing.T, mode, lockPath string) (command *exec.Cmd, release func()) {
	t.Helper()
	directory := t.TempDir()
	ready, releaseFile := filepath.Join(directory, "ready"), filepath.Join(directory, "release")
	command = exec.Command(os.Args[0], "-test.run=^TestMPHelperProcess$")
	command.Env = append(os.Environ(), mpHelperEnv+"="+strings.Join([]string{mode, lockPath, ready, releaseFile}, "\x1f"),
		LockTimeoutEnv+"=30")
	command.Stderr = os.Stderr
	noError(t, command.Start())
	released := false
	release = func() {
		if !released {
			released = true
			os.WriteFile(releaseFile, []byte("go"), 0o600)
		}
	}
	t.Cleanup(func() {
		release()
		if command.ProcessState == nil {
			done := make(chan struct{})
			go func() { command.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				command.Process.Kill()
			}
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return command, release
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process never took the lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type mpLockResult struct {
	err          error
	elapsed      time.Duration
	afterRelease bool
}

// test_windows_cross_process_lock_waits_beyond_msvcrt_implicit_retry_window:
// not ported, the Go server has no Windows lock path.

func TestPosixCrossProcessLockWaitsThenSucceeds(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "posix-held.lock")
	holder, release := mpHelper(t, "hold", lockPath)
	info := must(os.Stat(lockPath))(t)

	// A real failed OS syscall proves contention, without replacing the
	// repository's flock/retry implementation or inferring it from time.
	probe := must(os.OpenFile(lockPath, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o666))(t)
	probeInfo := must(probe.Stat())(t)
	err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	probe.Close()
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("holder did not exclude OS lock probe: %v", err)
	}
	if !os.SameFile(info, probeInfo) {
		t.Fatal("probe opened another file than the holder's lock")
	}

	var releasedFlag sync.Mutex
	released := false
	results := make(chan mpLockResult, 1)
	started := time.Now()
	go func() {
		var result mpLockResult
		result.err = withLock(bg, lockPath, func(context.Context) error {
			result.elapsed = time.Since(started)
			releasedFlag.Lock()
			result.afterRelease = released
			releasedFlag.Unlock()
			return nil
		})
		results <- result
	}()
	select {
	case result := <-results:
		t.Fatalf("contender entered while holder owned lock: %+v", result)
	case <-time.After(400 * time.Millisecond):
	}
	releasedFlag.Lock()
	released = true
	releasedFlag.Unlock()
	release()
	select {
	case result := <-results:
		noError(t, result.err)
		if !result.afterRelease {
			t.Fatal("contender acquired before the holder was released")
		}
		if result.elapsed < 200*time.Millisecond || result.elapsed >= 5*time.Second {
			t.Fatalf("elapsed %v", result.elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("contender never acquired the lock")
	}
	noError(t, holder.Wait())
}

func TestCrossProcessLockTimeoutIsShortConfigurableAndPathSafe(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "sensitive-profile-name.lock")
	holder, release := mpHelper(t, "hold", lockPath)
	t.Setenv(LockTimeoutEnv, "0.2")

	acquired := false
	started := time.Now()
	err := withLock(bg, lockPath, func(context.Context) error { acquired = true; return nil })
	elapsed := time.Since(started)

	// The holder keeps ownership through the error.
	if acquired {
		t.Fatal("contender acquired a held lock")
	}
	if holder.ProcessState != nil {
		t.Fatal("holder exited")
	}
	wantError(t, err, ErrRepository, "Timed out waiting for profile repository lock")
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != ErrRepository {
		t.Fatalf("want a plain ProfileRepositoryError, got %#v", err)
	}
	if elapsed < 100*time.Millisecond || elapsed >= 900*time.Millisecond {
		t.Fatalf("elapsed %v", elapsed)
	}
	if strings.Contains(err.Error(), lockPath) {
		t.Fatalf("lock path leaked into %q", err.Error())
	}
	release()
	noError(t, holder.Wait())
}

func TestLockTimeoutEnvironmentIsBoundedAndNonfiniteValuesAreSafe(t *testing.T) {
	for _, configured := range []string{"-1", "0", "nan", "inf", "999999", "not-a-number"} {
		t.Run(configured, func(t *testing.T) {
			t.Setenv(LockTimeoutEnv, configured)
			timeout := LockTimeout()
			if timeout < 100*time.Millisecond || timeout > 3600*time.Second {
				t.Fatalf("timeout %v", timeout)
			}
		})
	}
}

// Python runs the failing holder in a second process; a process exit would
// release the lock anyway, so the stronger check here is in-process: both
// the flock and the in-process lock are released when fn fails.
func TestFileLockIsReleasedWhenProtectedOperationRaises(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "exception.lock")
	injected := errors.New("injected protected operation failure")
	err := withLock(bg, lockPath, func(context.Context) error { return injected })
	if err != injected {
		t.Fatalf("got %v", err)
	}
	started := time.Now()
	noError(t, withLock(bg, lockPath, func(context.Context) error { return nil }))
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("elapsed %v", elapsed)
	}
	// Another descriptor of this process can take the flock again.
	probe := must(os.OpenFile(lockPath, os.O_RDWR, 0))(t)
	defer probe.Close()
	noError(t, syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}

func TestOperatingSystemReleasesFileLockWhenHolderProcessCrashes(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "crashed-holder.lock")
	crashed, _ := mpHelper(t, "crash", lockPath)
	err := crashed.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("holder exit: %v", err)
	}
	started := time.Now()
	noError(t, withLock(bg, lockPath, func(context.Context) error { return nil }))
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("elapsed %v", elapsed)
	}
}

// test_thread_lock_remains_reentrant_for_same_thread checks Python's RLock
// registry. The Go equivalent: one in-process lock per resolved path, and a
// call chain that already holds a path is recognised through its context.
func TestThreadLockRemainsReentrantForSameThread(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "reentrant.lock")
	noError(t, withLock(bg, lockPath, func(ctx context.Context) error {
		if !holdsLock(ctx, lockPath) {
			t.Fatal("held lock is not recorded in the context")
		}
		if holdsLock(bg, lockPath) {
			t.Fatal("an unrelated context claims the lock")
		}
		return nil
	}))
}

func TestRepositoryCreatesIsolatedDefaultProfile(t *testing.T) {
	repository := newRepo(t)
	root := repository.DataDir()
	profiles := must(repository.ListProfileRecords(bg))(t)
	if profiles[0].ID != "default" {
		t.Fatalf("first profile %q", profiles[0].ID)
	}
	if got := must(repository.ProfileDir("default"))(t); got != filepath.Join(root, "profiles", "default") {
		t.Fatalf("profile dir %q", got)
	}
	if got := repository.DatabasePath(); got != filepath.Join(root, "configflow.sqlite3") {
		t.Fatalf("db path %q", got)
	}
	mpAbsent(t, filepath.Join(root, "config.json"))
	wantJSONEqual(t, parseJSONObj(t, must(repository.ExportAllJSON(bg, false))(t)).Value("schema_version"), 5)
	mpAbsent(t, filepath.Join(root, "system.json"))
	mpAbsent(t, filepath.Join(root, "profiles", "default", "config.json"))
	profile := parseJSONObj(t, must(repository.ExportProfileJSON(bg, "default"))(t))
	wantJSONEqual(t, profile.Value("proxy_groups"), []any{})
	if _, ok := profile.Get("resource_refs"); ok {
		t.Fatal("intermediate-format fields in a new profile")
	}
	if _, ok := profile.Get("node_dialers"); ok {
		t.Fatal("intermediate-format fields in a new profile")
	}
	if got := must(repository.SharedCacheDir())(t); got != filepath.Join(root, "shared", "subscribes") {
		t.Fatalf("shared cache dir %q", got)
	}
}

func mpAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("%s exists", path)
	}
}

// mpToken fixes secrets.token_urlsafe for the test.
func mpToken(t *testing.T, generate func() string) {
	t.Helper()
	original := tokenURLSafe
	tokenURLSafe = generate
	t.Cleanup(func() { tokenURLSafe = original })
}

func mpFixedToken(t *testing.T, token string) {
	mpToken(t, func() string { return token })
}

func mpSystemConfig(t *testing.T, repository *Repository) *testObj {
	t.Helper()
	sys := must(repository.System(bg))(t)
	return parseJSONObj(t, must(model.Marshal(sys.SystemConfig))(t))
}

func mpPersistedSystemConfig(t *testing.T, repository *Repository) *testObj {
	t.Helper()
	entry := must(repository.Store().Load())(t)
	for _, field := range entry.Document.System.Fields {
		if field.Name == "system_config" {
			return parseJSONObj(t, field.Value)
		}
	}
	t.Fatal("system_config field missing")
	return nil
}

func mpNoTemporaries(t *testing.T, repository *Repository) {
	t.Helper()
	if leftovers, _ := filepath.Glob(filepath.Join(repository.DataDir(), ".configflow.sqlite3.*.tmp")); len(leftovers) > 0 {
		t.Fatalf("temporary databases left: %v", leftovers)
	}
}

func mpTokens(t *testing.T, repository *Repository, want ...string) {
	t.Helper()
	tokens := must(repository.RuleProxyTokensForSanitization(bg))(t)
	if len(tokens) != len(want) {
		t.Fatalf("tokens %v, want %v", tokens, want)
	}
	for _, token := range want {
		if !tokens.Has(token) {
			t.Fatalf("tokens %v, want %v", tokens, want)
		}
	}
}

func TestEmptyDataDirectoryGeneratesAndAtomicallyPersistsRuleProxyToken(t *testing.T) {
	mpFixedToken(t, "generated-default-token")
	repository := newRepo(t)

	wantJSONEqual(t, mpSystemConfig(t, repository).Value("rule_proxy_token"), "generated-default-token")
	if got := mpSystemConfig(t, repository).GetString("config_token"); got != "" {
		t.Fatalf("config_token %q", got)
	}
	wantJSONEqual(t, mpPersistedSystemConfig(t, repository).Value("rule_proxy_token"), "generated-default-token")
	mpNoTemporaries(t, repository)
	mpAbsent(t, filepath.Join(repository.DataDir(), "config.json"))
}

// test_legacy_missing_or_empty_rule_proxy_token_is_generated_and_persisted
// starts from a legacy config.json, which this port refuses. The token rule
// itself is unchanged; it is exercised with the same four system_config
// values through the initial configuration of a fresh data directory, which
// takes the same conversion path (_convert_import of a single config).
func TestLegacyMissingOrEmptyRuleProxyTokenIsGeneratedAndPersisted(t *testing.T) {
	for name, text := range map[string]string{
		"missing":      `{}`,
		"empty":        `{"rule_proxy_token": ""}`,
		"config token": `{"config_token": "legacy-public-token"}`,
		"equal tokens": `{"config_token": "shared-token", "rule_proxy_token": "shared-token"}`,
	} {
		t.Run(name, func(t *testing.T) {
			mpFixedToken(t, "generated-legacy-token")
			legacySystemConfig := parseJSONObj(t, []byte(text))
			repository := must(Open(t.TempDir(), Options{InitialSettings: func() *model.Config {
				p := decodeJSON[model.Profile](t, `{"rule_configs":[]}`)
				return &model.Config{Profile: &p, SystemConfig: decodeJSON[model.SystemConfig](t, text)}
			}}))(t)

			wantJSONEqual(t, mpSystemConfig(t, repository).Value("rule_proxy_token"), "generated-legacy-token")
			if token := legacySystemConfig.GetString("config_token"); token != "" {
				wantJSONEqual(t, mpSystemConfig(t, repository).Value("config_token"), token)
			}
			wantJSONEqual(t, mpPersistedSystemConfig(t, repository).Value("rule_proxy_token"), "generated-legacy-token")
		})
	}
}

func TestNewRepositoryRotatesEqualFactoryTokens(t *testing.T) {
	mpFixedToken(t, "generated-new-token")
	repository := must(Open(t.TempDir(), Options{InitialSettings: func() *model.Config {
		return &model.Config{
			Profile:      &model.Profile{},
			SystemConfig: decodeJSON[model.SystemConfig](t, `{"config_token": "factory-shared-token", "rule_proxy_token": "factory-shared-token"}`),
		}
	}}))(t)

	want := json.RawMessage(`{
		"server_domain": "",
		"github_proxy_domain": "",
		"rule_fetch_proxy": "",
		"config_token": "factory-shared-token",
		"rule_proxy_token": "generated-new-token",
		"retired_rule_proxy_tokens": ["factory-shared-token"]
	}`)
	wantJSONEqual(t, mpSystemConfig(t, repository), want)
}

func mpSetSystemConfig(t *testing.T, r *Repository, key, value string) {
	t.Helper()
	raw := mustMarshalJSON(t, value)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		return system.SystemConfig.SetJSON(key, raw)
	}))
}

func TestExistingNonemptyRuleProxyTokenIsPreserved(t *testing.T) {
	repository := newRepo(t)
	mpSetSystemConfig(t, repository, "rule_proxy_token", "keep-existing")
	mpToken(t, func() string {
		t.Error("must not replace an existing token")
		return "replaced"
	})

	reloaded := openRepo(t, repository.DataDir())

	wantJSONEqual(t, mpSystemConfig(t, reloaded).Value("rule_proxy_token"), "keep-existing")
}

func TestRestartAtomicallyRotatesRuleProxyTokenEqualToConfigToken(t *testing.T) {
	repository := newRepo(t)
	// Persist a raw document that skips the repository's token rule.
	base := must(repository.Store().Load())(t)
	rows := cloneStoreDoc(t, base.Document)
	sc := mpPersistedSystemConfig(t, repository)
	sc.Set("config_token", "shared-token")
	sc.Set("rule_proxy_token", "shared-token")
	setStoreField(&rows.System.Fields, "system_config", mustMarshalJSON(t, sc))
	if _, _, err := repository.Store().Commit(t.Context(), base, rows, nil); err != nil {
		t.Fatal(err)
	}
	mpFixedToken(t, "rotated-internal-token")

	restarted := openRepo(t, repository.DataDir())

	systemConfig := mpSystemConfig(t, restarted)
	wantJSONEqual(t, systemConfig.Value("config_token"), "shared-token")
	wantJSONEqual(t, systemConfig.Value("rule_proxy_token"), "rotated-internal-token")
	wantJSONEqual(t, mpPersistedSystemConfig(t, restarted), systemConfig)
	mpTokens(t, restarted, "shared-token", "rotated-internal-token")
	wantJSONEqual(t, systemConfig.Value("retired_rule_proxy_tokens"), []any{"shared-token"})
	for range 3 {
		restarted = openRepo(t, repository.DataDir())
		mpTokens(t, restarted, "shared-token", "rotated-internal-token")
		wantJSONEqual(t, mpSystemConfig(t, restarted).Value("retired_rule_proxy_tokens"), []any{"shared-token"})
	}
	mpNoTemporaries(t, repository)
}

// test_legacy_retired_rule_proxy_tokens_are_normalized_and_preserved starts
// from a legacy config.json; the same system_config is given as the initial
// configuration of a fresh data directory (same conversion path).
func TestLegacyRetiredRuleProxyTokensAreNormalizedAndPreserved(t *testing.T) {
	repository := must(Open(t.TempDir(), Options{InitialSettings: func() *model.Config {
		return &model.Config{
			Profile: &model.Profile{},
			SystemConfig: decodeJSON[model.SystemConfig](t, `{
			"rule_proxy_token": "current-internal",
			"retired_rule_proxy_tokens": ["retired-one", "", null, "retired-one", "retired-two"]
		}`),
		}
	}}))(t)

	wantJSONEqual(t, mpSystemConfig(t, repository).Value("retired_rule_proxy_tokens"), []any{"retired-one", "retired-two"})
	mpTokens(t, repository, "current-internal", "retired-one", "retired-two")
	wantJSONEqual(t, mpSystemConfig(t, openRepo(t, repository.DataDir())).Value("retired_rule_proxy_tokens"),
		[]any{"retired-one", "retired-two"})
}

func TestSystemTransactionNeverPersistsEqualConfigAndRuleProxyTokens(t *testing.T) {
	repository := newRepo(t)
	internalToken := mpSystemConfig(t, repository).GetString("rule_proxy_token")
	mpFixedToken(t, "transaction-rotated-token")

	mpSetSystemConfig(t, repository, "config_token", internalToken)

	persisted := mpPersistedSystemConfig(t, repository)
	wantJSONEqual(t, persisted.Value("config_token"), internalToken)
	wantJSONEqual(t, persisted.Value("rule_proxy_token"), "transaction-rotated-token")
	wantJSONEqual(t, mpSystemConfig(t, repository), persisted)
}

func TestExistingNonemptyRuleProxyTokenIsPreservedAcrossRestarts(t *testing.T) {
	for _, oldToken := range []string{"legacy token/&?=秘密", " whitespace ", "🔐/+=?%token", "\n\t"} {
		t.Run(oldToken, func(t *testing.T) {
			original := newRepo(t)
			system := must(original.System(bg))(t)
			system.SystemConfig.RuleProxyToken = oldToken
			must(original.SaveSystemRecord(bg, system, nil))(t)
			mpToken(t, func() string {
				t.Error("must not replace any existing nonempty token")
				return "replaced"
			})

			for range 3 {
				restarted := openRepo(t, original.DataDir())
				wantJSONEqual(t, mpSystemConfig(t, restarted).Value("rule_proxy_token"), oldToken)
				mpTokens(t, restarted, oldToken)
			}

			wantJSONEqual(t, mpPersistedSystemConfig(t, original).Value("rule_proxy_token"), oldToken)
			mpNoTemporaries(t, original)
		})
	}
}

func TestProfileIDCannotEscapeProfilesDirectory(t *testing.T) {
	repository := newRepo(t)

	for _, profileID := range []string{"../outside", "..", "a/b", "a\\b", "C:"} {
		_, err := repository.ProfileDir(profileID)
		wantError(t, err, ErrValidation, "")
		_, err = repository.Profile(bg, profileID)
		wantError(t, err, ErrValidation, "")
	}

	// Python also passes None, which the string parameter rules out.
	for _, relativePath := range []string{"../outside", ""} {
		_, err := repository.ProfilePath("default", relativePath)
		wantError(t, err, ErrValidation, "")
	}
}

// Not ported (legacy migration, which this port refuses):
//   test_legacy_config_migrates_without_data_loss_and_is_idempotent
//   test_legacy_initialization_is_serialized_across_processes_over_multiple_rounds
//   test_legacy_migration_cleans_partial_derived_copy_and_retry_succeeds

func mpGroups(names ...string) []any {
	groups := make([]any, len(names))
	for index, name := range names {
		groups[index] = testObjKV("id", name, "name", name)
	}
	return groups
}

func mpSaveGroups(t *testing.T, repository *Repository, profileID string, names ...string) error {
	t.Helper()
	_, _, err := repository.SaveProfileFieldsJSON(bg, profileID, mustMarshalJSON(t, testObjKV("proxy_groups", mpGroups(names...))), nil)
	return err
}

func mpProfileGroups(t *testing.T, repository *Repository, profileID string) any {
	t.Helper()
	return parseJSONObj(t, must(repository.ExportProfileJSON(bg, profileID))(t)).Value("proxy_groups")
}

func mpProfileIDs(t *testing.T, repository *Repository) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, profile := range must(repository.ListProfileRecords(bg))(t) {
		ids[profile.ID] = true
	}
	return ids
}

// mpFailCommit makes every commit of the repository's store fail after all
// rows were written and before COMMIT (Python: ConfigStore._apply/_commit
// replaced by a raising function).
func mpFailCommit(repository *Repository, message string) error {
	failure := errors.New(message)
	repository.Store().BeforeCommit = func() error { return failure }
	return failure
}

func mpWantFailure(t *testing.T, err, failure error) {
	t.Helper()
	if !errors.Is(err, failure) {
		t.Fatalf("expected %v, got %v", failure, err)
	}
}

func TestFailedDatabaseCommitKeepsPreviousData(t *testing.T) {
	repository := newRepo(t)
	noError(t, mpSaveGroups(t, repository, "default", "old"))
	before := must(repository.Store().Fingerprint())(t)

	failure := mpFailCommit(repository, "commit failed")
	mpWantFailure(t, mpSaveGroups(t, repository, "default", "new"), failure)

	if after := must(repository.Store().Fingerprint())(t); after != before {
		t.Fatalf("database changed: %+v → %+v", before, after)
	}
	wantJSONEqual(t, mpProfileGroups(t, repository, "default"), mpGroups("old"))
	wantJSONEqual(t, mpProfileGroups(t, openRepo(t, repository.DataDir()), "default"), mpGroups("old"))
}

func TestPartialSystemMetadataSaveKeepsOtherFields(t *testing.T) {
	repository := newRepo(t)
	mpSetSystemConfig(t, repository, "config_token", "token")
	mpSetSystemConfig(t, repository, "server_domain", "http://configflow.test")

	systemConfig := mpSystemConfig(t, repository)
	if systemConfig.GetString("rule_proxy_token") == "" {
		t.Fatal("no rule proxy token")
	}
	systemConfig.Delete("rule_proxy_token")
	wantJSONEqual(t, systemConfig, json.RawMessage(`{
		"server_domain": "http://configflow.test",
		"github_proxy_domain": "",
		"rule_fetch_proxy": "",
		"config_token": "token",
		"retired_rule_proxy_tokens": []
	}`))
}

// mpRun runs the functions concurrently and returns their errors.
func mpRun(t *testing.T, timeout time.Duration, functions ...func() error) []error {
	t.Helper()
	var failures []error
	var mu sync.Mutex
	var group sync.WaitGroup
	for _, function := range functions {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := function(); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
			}
		}()
	}
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("workers did not finish")
	}
	return failures
}

func TestConcurrentProfileSavesDoNotCrossContaminate(t *testing.T) {
	repository := newRepo(t)
	must(repository.CreateProfileRecord(bg, ProfileCreation{ID: "alpha", Name: "Alpha"}, ""))(t)
	must(repository.CreateProfileRecord(bg, ProfileCreation{ID: "beta", Name: "Beta"}, ""))(t)

	save := func(profileID string) func() error {
		return func() error {
			for range 20 {
				if err := mpSaveGroups(t, repository, profileID, profileID); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if failures := mpRun(t, 60*time.Second, save("alpha"), save("beta")); len(failures) > 0 {
		t.Fatal(failures)
	}
	wantJSONEqual(t, mpProfileGroups(t, repository, "alpha"), mpGroups("alpha"))
	wantJSONEqual(t, mpProfileGroups(t, repository, "beta"), mpGroups("beta"))
}

func TestConcurrentProfileCreationPreservesSystemIndex(t *testing.T) {
	repository := newRepo(t)
	start := make(chan struct{})
	create := func(profileID string) func() error {
		return func() error {
			<-start
			_, err := repository.CreateProfileRecord(bg, ProfileCreation{ID: profileID}, "")
			return err
		}
	}
	go close(start)
	if failures := mpRun(t, 10*time.Second, create("alpha"), create("beta")); len(failures) > 0 {
		t.Fatal(failures)
	}
	ids := mpProfileIDs(t, repository)
	if !ids["default"] || !ids["alpha"] || !ids["beta"] {
		t.Fatalf("profiles %v", ids)
	}
}

func mpAppendGroup(id, name string) func(context.Context, *model.Profile) error {
	return func(_ context.Context, profile *model.Profile) error {
		profile.ProxyGroups = append(profile.ProxyGroups, model.ProxyGroup{ID: id, Name: name})
		return nil
	}
}

func TestProfileTransactionPreservesConcurrentListUpdates(t *testing.T) {
	repository := newRepo(t)
	var workers []func() error
	want := map[string]bool{}
	for index := range 12 {
		id := "sub-" + string(rune('a'+index))
		want[id] = true
		workers = append(workers, func() error {
			_, err := repository.UpdateProfile(bg, "default", mpAppendGroup(id, "Group "+id))
			return err
		})
	}
	if failures := mpRun(t, 30*time.Second, workers...); len(failures) > 0 {
		t.Fatal(failures)
	}
	got := map[string]bool{}
	for _, item := range must(repository.Profile(bg, "default"))(t).ProxyGroups {
		got[item.ID] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %v, want %v", got, want)
	}
}

// Python fails ConfigStore._apply before any row is written; the Go seam
// fails after the rows were written and before COMMIT. Either way the
// transaction must leave the previous data.
func TestSaveProfileRollsBackProfileWhenSystemCommitFails(t *testing.T) {
	repository := newRepo(t)
	noError(t, mpSaveGroups(t, repository, "default", "old"))
	failure := mpFailCommit(repository, "injected system commit failure")
	mpWantFailure(t, mpSaveGroups(t, repository, "default", "new"), failure)

	wantJSONEqual(t, mpProfileGroups(t, repository, "default"), mpGroups("old"))
}

func TestSaveProfileRollsBackSystemWhenCommitFailsAfterReplace(t *testing.T) {
	repository := newRepo(t)
	before := must(model.Marshal(must(repository.System(bg))(t)))(t)
	// Every row is written; the transaction must still roll back.
	failure := mpFailCommit(repository, "injected post-replace failure")
	mpWantFailure(t, mpSaveGroups(t, repository, "default", "new"), failure)

	wantJSONEqual(t, must(model.Marshal(must(repository.System(bg))(t)))(t), before)
	wantJSONEqual(t, mpProfileGroups(t, repository, "default"), []any{})
}

func TestCreateProfileCleansDirectoryWhenSystemCommitFails(t *testing.T) {
	repository := newRepo(t)
	failure := mpFailCommit(repository, "injected system commit failure")
	_, err := repository.CreateProfileRecord(bg, ProfileCreation{ID: "orphan"}, "")
	mpWantFailure(t, err, failure)

	mpAbsent(t, must(repository.ProfileDir("orphan"))(t))
	if mpProfileIDs(t, repository)["orphan"] {
		t.Fatal("profile was created")
	}
}

func TestCreateProfileRestoresPreexistingOrphanDirectoryOnCommitFailure(t *testing.T) {
	repository := newRepo(t)
	orphanDir := must(repository.ProfileDir("orphan"))(t)
	noError(t, os.Mkdir(orphanDir, 0o777))
	originalConfig := []byte("{\"unregistered\": \"original\"}\n")
	noError(t, os.WriteFile(filepath.Join(orphanDir, "config.json"), originalConfig, 0o666))
	noError(t, os.WriteFile(filepath.Join(orphanDir, "important.txt"), []byte("keep me"), 0o666))
	failure := mpFailCommit(repository, "system commit failed")
	_, err := repository.CreateProfileRecord(bg, ProfileCreation{ID: "orphan"}, "")
	mpWantFailure(t, err, failure)

	if got := must(os.ReadFile(filepath.Join(orphanDir, "config.json")))(t); !bytes.Equal(got, originalConfig) {
		t.Fatalf("config.json %q", got)
	}
	if got := string(must(os.ReadFile(filepath.Join(orphanDir, "important.txt")))(t)); got != "keep me" {
		t.Fatalf("important.txt %q", got)
	}
	var names []string
	for _, entry := range must(os.ReadDir(orphanDir))(t) {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"config.json", "important.txt"}) {
		t.Fatalf("names %v", names)
	}
	if mpProfileIDs(t, repository)["orphan"] {
		t.Fatal("profile was created")
	}
}

func TestDeleteProfileSerializesWithInFlightProfileWrite(t *testing.T) {
	repository := newRepo(t)
	must(repository.CreateProfileRecord(bg, ProfileCreation{ID: "deletable"}, ""))(t)
	writeStarted, allowWrite := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repository.Store().BeforeCommit = func() error {
		once.Do(func() { close(writeStarted) })
		select {
		case <-allowWrite:
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("the write was never allowed to commit")
		}
	}

	writeDone, deleteDone := make(chan error, 1), make(chan error, 1)
	go func() { writeDone <- mpSaveGroups(t, repository, "deletable", "write") }()
	select {
	case <-writeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the write never reached its commit")
	}
	go func() { deleteDone <- repository.DeleteProfile(bg, "deletable", "") }()
	select {
	case err := <-deleteDone:
		t.Fatalf("delete finished while a write held the document lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(allowWrite)
	for _, done := range []chan error{writeDone, deleteDone} {
		select {
		case err := <-done:
			noError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("writer or deleter did not finish")
		}
	}

	mpAbsent(t, must(repository.ProfileDir("deletable"))(t))
	if mpProfileIDs(t, repository)["deletable"] {
		t.Fatal("profile still listed")
	}
}

func TestUpdateProfileTransactionRollsBackProfileWhenSystemCommitFails(t *testing.T) {
	repository := newRepo(t)
	noError(t, mpSaveGroups(t, repository, "default", "old"))
	failure := mpFailCommit(repository, "system commit failed")
	_, err := repository.UpdateProfile(bg, "default", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("proxy_groups", mustMarshalJSON(t, mpGroups("new")))
	})
	mpWantFailure(t, err, failure)

	wantJSONEqual(t, mpProfileGroups(t, repository, "default"), mpGroups("old"))
}

func mpTombstones(t *testing.T, repository *Repository, profileID string) []string {
	t.Helper()
	return must(filepath.Glob(filepath.Join(repository.MigrationsDir(), "deleted-"+profileID+"-*")))(t)
}

func TestDeleteProfileArchivesDerivedFilesWithoutDestroyingRecoveryData(t *testing.T) {
	repository := newRepo(t)
	must(repository.CreateProfileRecord(bg, ProfileCreation{ID: "keep", Name: "Keep"}, ""))(t)
	directory := must(repository.ProfileDir("keep"))(t)
	noError(t, os.Mkdir(directory, 0o777))
	noError(t, os.WriteFile(filepath.Join(directory, "important.txt"), []byte("keep me"), 0o666))
	profile := must(repository.ExportProfileJSON(bg, "keep"))(t)

	noError(t, repository.DeleteProfile(bg, "keep", ""))

	if mpProfileIDs(t, repository)["keep"] {
		t.Fatal("profile still listed")
	}
	mpAbsent(t, directory)
	tombstones := mpTombstones(t, repository, "keep")
	if len(tombstones) != 1 {
		t.Fatalf("tombstones %v", tombstones)
	}
	if got := string(must(os.ReadFile(filepath.Join(tombstones[0], "important.txt")))(t)); got != "keep me" {
		t.Fatalf("important.txt %q", got)
	}
	exported := filepath.Join(tombstones[0], "deleted-profile.json")
	text := must(os.ReadFile(exported))(t)
	wantJSONEqual(t, text, profile)
	// The file is what Python writes: json.dumps(profile, ensure_ascii=False, indent=2) + "\n".
	if !bytes.Equal(text, must(fileJSON(json.RawMessage(profile)))(t)) {
		t.Fatal("deleted-profile.json formatting differs")
	}
	if mode := must(os.Stat(exported))(t).Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o", mode)
	}
}

func TestDeleteProfileRestoresDirectoryWhenSystemCommitFails(t *testing.T) {
	repository := newRepo(t)
	must(repository.CreateProfileRecord(bg, ProfileCreation{ID: "keep", Name: "Keep"}, ""))(t)
	directory := must(repository.ProfileDir("keep"))(t)
	noError(t, os.Mkdir(directory, 0o777))
	marker := filepath.Join(directory, "important.txt")
	noError(t, os.WriteFile(marker, []byte("original"), 0o666))
	before := must(model.Marshal(must(repository.System(bg))(t)))(t)
	failure := mpFailCommit(repository, "system commit failed")
	mpWantFailure(t, repository.DeleteProfile(bg, "keep", ""), failure)

	wantJSONEqual(t, must(model.Marshal(must(repository.System(bg))(t)))(t), before)
	if got := string(must(os.ReadFile(marker))(t)); got != "original" {
		t.Fatalf("marker %q", got)
	}
	if tombstones := mpTombstones(t, repository, "keep"); len(tombstones) != 0 {
		t.Fatalf("tombstones left: %v", tombstones)
	}
	if !mpProfileIDs(t, repository)["keep"] {
		t.Fatal("profile was deleted")
	}
}

func TestIndependentRepositoriesMergeLockedFieldAndListUpdates(t *testing.T) {
	first := newRepo(t)
	second := openRepo(t, first.DataDir())
	_, _, err := first.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{"mihomo": {"custom_config": "before"}}`), nil)
	noError(t, err)

	failures := mpRun(t, 10*time.Second,
		func() error {
			_, _, err := first.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{"mihomo": {"custom_config": "field"}}`), nil)
			return err
		},
		func() error {
			_, err := second.UpdateProfile(bg, "default", mpAppendGroup("concurrent", "concurrent"))
			return err
		})
	if len(failures) > 0 {
		t.Fatal(failures)
	}
	result := parseJSONObj(t, must(first.ExportProfileJSON(bg, "default"))(t))
	wantJSONEqual(t, result.GetObject("mihomo").Value("custom_config"), "field")
	wantJSONEqual(t, result.Value("proxy_groups"), mpGroups("concurrent"))
}

func TestUpdateProfileFieldsThreeWayMergesStaleListAppends(t *testing.T) {
	first := newRepo(t)
	second := openRepo(t, first.DataDir())
	noError(t, mpSaveGroups(t, first, "default", "existing"))

	firstBaseline := parseJSONObj(t, must(first.ExportProfileJSON(bg, "default"))(t))
	secondBaseline := parseJSONObj(t, must(second.ExportProfileJSON(bg, "default"))(t))
	firstValue := append(append([]any{}, firstBaseline.GetList("proxy_groups")...), mpGroups("from-first")...)
	secondValue := append(append([]any{}, secondBaseline.GetList("proxy_groups")...), mpGroups("from-second")...)

	_, _, err := first.SaveProfileFieldsJSON(bg, "default",
		mustMarshalJSON(t, testObjKV("proxy_groups", firstValue)),
		mustMarshalJSON(t, testObjKV("proxy_groups", firstBaseline.Value("proxy_groups"))))
	noError(t, err)
	_, _, err = second.SaveProfileFieldsJSON(bg, "default",
		mustMarshalJSON(t, testObjKV("proxy_groups", secondValue)),
		mustMarshalJSON(t, testObjKV("proxy_groups", secondBaseline.Value("proxy_groups"))))
	noError(t, err)

	wantJSONEqual(t, mpProfileGroups(t, first, "default"), mpGroups("existing", "from-first", "from-second"))
}
