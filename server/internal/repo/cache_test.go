package repo

// Port of backend/test_config_repository_cache.py: validated-document reuse
// must preserve persistence and snapshot boundaries. The Python tests count
// calls by monkeypatching store.load and _validate_document; here the
// repository's loads/validations counters are read.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

func ccCounts(r *Repository) [2]int64 { return [2]int64{r.loads.Load(), r.validations.Load()} }

func ccSince(r *Repository, start [2]int64) [2]int64 {
	now := ccCounts(r)
	return [2]int64{now[0] - start[0], now[1] - start[1]}
}

// ccRawRename changes the default profile's name behind the repository's back.
func ccRawRename(t *testing.T, r *Repository, name string) {
	rcSQL(t, r.DatabasePath(),
		"UPDATE fields SET value = '"+string(must(jsonx.Default(name))(t))+"' WHERE scope = 'profile' AND owner = 'default' AND field = 'name'",
		"UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'generation'")
}

func ccName(t *testing.T, r *Repository) any {
	t.Helper()
	return must(r.GetProfile(bg, "default"))(t).Value("name")
}

func ccRename(t *testing.T, r *Repository, name string) {
	t.Helper()
	must(r.UpdateProfile(bg, "default", jsonx.Obj("name", name)))(t)
}

func TestUnchangedGettersReadAndValidateOnce(t *testing.T) {
	r := newRepo(t)
	r.dropCache()
	start := ccCounts(r)
	for range 3 {
		must(r.GetSystem(bg))(t)
		must(r.GetShared(bg))(t)
		must(r.GetProfile(bg, "default"))(t)
		must(r.GetCompatConfig(bg, "default", nil))(t)
		must(r.ExportAll(bg, false))(t)
		must(r.ListProfiles(bg))(t)
		must(r.ConfigRevision(bg, "default"))(t)
	}
	if got := ccSince(r, start); got != [2]int64{1, 1} {
		t.Fatalf("reads, validations = %v", got)
	}
}

func TestPublicResultsCannotMutateCachedDocument(t *testing.T) {
	r := newRepo(t)
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Update(parse(t, `{"nodes": [{"id": "n", "name": "Node"}],
			"subscription_aggregations": [{"id": "a", "name": {"display": ["Aggregate"]}, "nodes": ["n"]}],
			"rule_library": [{"id": "r", "name": "Rule", "enabled": {"custom": []}}]}`))
		return nil, nil
	}))(t)
	_, _, err := r.UpdateProfileFields(bg, "default", parse(t, `{
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["n"]}],
		"rule_configs": [{"id": "rule", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	before := must(r.ExportAll(bg, false))(t)
	must(r.GetSystem(bg))(t).GetObject("system_config").Set("surprise", []any{})
	must(r.GetShared(bg))(t).GetList("nodes")[0].(*jsonx.Object).Set("name", "Changed")
	must(r.GetProfile(bg, "default"))(t).GetList("proxy_groups")[0].(*jsonx.Object).Set("name", "Changed")
	must(r.ExportAll(bg, false))(t).GetObject("profiles").Delete("default")
	must(r.ExportProfile(bg, "default"))(t).GetList("rule_configs")[0].(*jsonx.Object).Set("id", "Changed")
	must(r.ListProfiles(bg))(t)[0].(*jsonx.Object).Set("name", "Changed")
	resolved := must(r.GetCompatConfig(bg, "default", nil))(t)
	resolved.GetList("nodes")[0].(*jsonx.Object).Set("name", "Changed")
	resolved.GetList("rule_configs")[0].(*jsonx.Object).GetObject("library_enabled").Set("custom", []any{"Changed"})
	usages := must(r.GetResourceUsage(bg, "nodes", "n"))(t)
	usages[0].(*jsonx.Object).GetObject("name").Set("display", []any{"Changed"})
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
}

func TestExternalCommitInvalidatesByGeneration(t *testing.T) {
	r := newRepo(t)
	ccRename(t, r, "Alpha")
	wantEqual(t, ccName(t, r), "Alpha")
	start := ccCounts(r)
	other := openRepo(t, r.DataDir())
	ccRename(t, other, "Bravo")
	wantEqual(t, ccName(t, r), "Bravo")
	wantEqual(t, ccName(t, r), "Bravo")
	if got := ccSince(r, start)[0]; got != 1 {
		t.Fatalf("reads = %d", got)
	}
	ccRawRename(t, r, "Charlie")
	wantEqual(t, ccName(t, r), "Charlie")
}

func TestRebuiltDatabaseWithSameGenerationInvalidates(t *testing.T) {
	r := newRepo(t)
	ccRename(t, r, "Alpha")
	head := r.cached().Head
	document := must(decodeEntry(must(r.store.Load())(t)))(t).Document.DeepCopy()
	document.GetObject("profiles").GetObject("default").Set("name", "Rebuilt")
	// Same generation number, different database: store_id must differ.
	must(r.createStore(document, jsonx.NewObject()))(t)
	rcSQL(t, r.DatabasePath(), "UPDATE meta SET value = '"+jsonx.Int(head.Generation).String()+"' WHERE key = 'generation'")
	if got := must(r.store.Load())(t).Head.Generation; got != head.Generation {
		t.Fatalf("generation %d, want %d", got, head.Generation)
	}
	wantEqual(t, ccName(t, r), "Rebuilt")
}

func TestCommitDuringReadNeverLabelsOldDocumentAsNew(t *testing.T) {
	r := newRepo(t)
	other := openRepo(t, r.DataDir())
	done := make(chan error, 1)
	started := false
	// A writer in another goroutine cannot commit while this read
	// transaction is open; it commits right after it ends.
	r.afterLoad = func() {
		if !started {
			started = true
			go func() {
				_, err := other.UpdateProfile(bg, "default", jsonx.Obj("name", "External"))
				done <- err
			}()
		}
	}
	r.dropCache()
	if ccName(t, r) == "External" {
		t.Fatal("the read saw the commit made after it")
	}
	noError(t, <-done)
	wantEqual(t, ccName(t, r), "External")
}

func TestDamagedDatabaseRaisesInsteadOfUsingCachedCopy(t *testing.T) {
	r := newRepo(t)
	original := must(r.ExportAll(bg, false))(t)
	rcSQL(t, r.DatabasePath(),
		"UPDATE fields SET value = '{' WHERE scope = 'system' AND field = 'agents'",
		"UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'generation'")
	_, err := r.GetSystem(bg)
	wantError(t, err, ErrRepository, "配置数据库内容无效")
	must(r.createStore(original, jsonx.NewObject()))(t)
	wantEqual(t, must(r.ExportAll(bg, false))(t), original)
}

func TestInvalidWriteAndCallbackFailureDoNotPoisonCache(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAll(bg, false))(t)
	_, err := r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", append(system.GetList("agents"), jsonx.Obj("id", "bad", "profile_id", "missing")))
		return nil, nil
	})
	wantError(t, err, ErrValidation, "")
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)

	failure := errors.New("callback failed")
	_, err = r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("should_not_persist", true)
		return nil, failure
	})
	if err != failure {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
}

func TestFailedCommitKeepsDataAndDropsCache(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAll(bg, false))(t)
	fingerprint := must(r.store.Fingerprint())(t)
	failure := errors.New("injected failure at COMMIT")
	r.store.BeforeCommit = func() error { return failure }
	_, err := r.UpdateProfile(bg, "default", jsonx.Obj("name", "Uncommitted"))
	if !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
	r.store.BeforeCommit = nil
	if r.cached() != nil {
		t.Fatal("cache kept after a failed commit")
	}
	if got := must(r.store.Fingerprint())(t); got != fingerprint {
		t.Fatal("database changed")
	}
	start := ccCounts(r)
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
	if got := ccSince(r, start)[0]; got != 1 {
		t.Fatalf("reads = %d", got)
	}
}

func TestNestedReadsUseTransactionAndFailedTransactionIsNotCached(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAll(bg, false))(t)
	failure := errors.New("abort")
	err := r.update(bg, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		document.GetObject("profiles").GetObject("default").Set("name", "In transaction")
		wantEqual(t, must(r.GetProfile(ctx, "default"))(t).Value("name"), "In transaction")
		return failure
	})
	if err != failure {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
}

func TestSeparateRepositoryCachesObserveConcurrentCommits(t *testing.T) {
	r := newRepo(t)
	other := openRepo(t, r.DataDir())
	must(r.GetSystem(bg))(t)
	must(other.GetSystem(bg))(t)
	var group sync.WaitGroup
	failures := make(chan error, 20)
	for _, repository := range []*Repository{r, other} {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 5 {
				_, err := repository.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
					settings := system.GetObject("system_config")
					settings.Set("counter", jsonx.Int(settings.GetInt("counter")+1))
					return nil, nil
				})
				if err == nil {
					_, err = repository.GetSystem(bg)
				}
				if err != nil {
					failures <- err
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	wantEqual(t, must(r.GetSystem(bg))(t).GetObject("system_config").Value("counter"), 10)
	wantEqual(t, must(other.GetSystem(bg))(t).GetObject("system_config").Value("counter"), 10)
}

func TestLargeDocumentIsNotRetainedInCache(t *testing.T) {
	r := newRepo(t)
	must(r.GetSystem(bg))(t)
	r.cacheMaxBytes = must(r.store.Load())(t).Size - 1
	r.dropCache()
	start := ccCounts(r)
	must(r.GetSystem(bg))(t)
	must(r.GetSystem(bg))(t)
	if got := ccSince(r, start); got != [2]int64{2, 2} {
		t.Fatalf("reads, validations = %v", got)
	}
	if r.cached() != nil {
		t.Fatal("large document cached")
	}
}

func TestImportAndResetInvalidatePreviousGeneration(t *testing.T) {
	r := newRepo(t)
	saved := must(r.ExportAll(bg, false))(t)
	imported := saved.DeepCopy()
	imported.GetObject("profiles").GetObject("default").Set("name", "Imported")
	must(r.ImportAll(bg, imported))(t)
	wantEqual(t, ccName(t, r), "Imported")
	must(r.ResetAll(bg))(t)
	wantEqual(t, ccName(t, r), saved.GetObject("profiles").GetObject("default").Value("name"))
}
