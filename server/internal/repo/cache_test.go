package repo

// Port of backend/test_config_repository_cache.py: validated-document reuse
// must preserve persistence and snapshot boundaries. The Python tests count
// calls by monkeypatching store.load and _validate_document; here the
// repository's loads/validations counters are read.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"

	"configflow/internal/model"
)

func ccCounts(r *Repository) [2]int64 { return [2]int64{r.loads.Load(), r.validations.Load()} }

func ccSince(r *Repository, start [2]int64) [2]int64 {
	now := ccCounts(r)
	return [2]int64{now[0] - start[0], now[1] - start[1]}
}

// ccRawRename changes the default profile's name behind the repository's back.
func ccRawRename(t *testing.T, r *Repository, name string) {
	rcSQL(t, r.DatabasePath(),
		"UPDATE fields SET value = '"+string(mustMarshalJSON(t, name))+"' WHERE scope = 'profile' AND owner = 'default' AND field = 'name'",
		"UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'generation'")
}

func ccName(t *testing.T, r *Repository) string {
	t.Helper()
	return must(r.Profile(bg, "default"))(t).Name
}

func ccRename(t *testing.T, r *Repository, name string) {
	t.Helper()
	must(r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &name}))(t)
}

func TestUnchangedGettersReadAndValidateOnce(t *testing.T) {
	r := newRepo(t)
	r.dropCache()
	start := ccCounts(r)
	for range 3 {
		must(r.System(bg))(t)
		must(r.Shared(bg))(t)
		must(r.Profile(bg, "default"))(t)
		must(r.Config(bg, "default", nil))(t)
		must(r.ExportAllJSON(bg, false))(t)
		must(r.ListProfileRecords(bg))(t)
		must(r.ConfigRevision(bg, "default"))(t)
	}
	if got := ccSince(r, start); got != [2]int64{1, 1} {
		t.Fatalf("reads, validations = %v", got)
	}
}

func TestPublicResultsCannotMutateCachedDocument(t *testing.T) {
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		if err := shared.SetJSON("nodes", json.RawMessage(`[{"id": "n", "name": "Node"}]`)); err != nil {
			return err
		}
		if err := shared.SetJSON("subscription_aggregations", json.RawMessage(`[{"id": "a", "name": {"display": ["Aggregate"]}, "nodes": ["n"]}]`)); err != nil {
			return err
		}
		return shared.SetJSON("rule_library", json.RawMessage(`[{"id": "r", "name": "Rule", "enabled": {"custom": []}}]`))
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["n"]}],
		"rule_configs": [{"id": "rule", "itemType": "ruleset", "library_rule_id": "r"}]}`), nil)
	noError(t, err)
	before := must(r.ExportAllJSON(bg, false))(t)

	sys := must(r.System(bg))(t)
	noError(t, sys.SystemConfig.Fields.Set("surprise", json.RawMessage(`[]`)))
	sh := must(r.CloneShared(bg))(t)
	sh.Nodes[0].Name = "Changed"
	prof := must(r.CloneProfile(bg, "default"))(t)
	prof.ProxyGroups[0].Name = "Changed"
	// Borrowed reads: the returned records and the Config's own slices are
	// shallow copies, so top-level changes stay with the caller.
	borrowedShared := must(r.Shared(bg))(t)
	borrowedShared.Nodes = nil
	borrowedShared.Revision = 99
	borrowedProfile := must(r.Profile(bg, "default"))(t)
	borrowedProfile.Name = "Changed"
	borrowedProfile.ProxyGroups = append(borrowedProfile.ProxyGroups, model.ProxyGroup{ID: "extra"})
	settings := must(r.SystemConfig(bg))(t)
	settings.ServerDomain = "https://changed.example"
	all := must(r.ExportAllJSON(bg, false))(t)
	all[0] = ' '
	exp := must(r.ExportProfileJSON(bg, "default"))(t)
	exp[0] = ' '
	list := must(r.ListProfileRecords(bg))(t)
	list[0].Name = "Changed"
	resolved := must(r.Config(bg, "default", nil))(t)
	resolved.Nodes[0].Name = "Changed"
	noError(t, resolved.Profile.RuleConfigs[0].Fields.Set("library_enabled", json.RawMessage(`{"custom":["Changed"]}`)))
	usages := must(r.ResourceUsage(bg, "nodes", "n"))(t)
	// The aggregation's name is stored with a wrong type: usages read it under
	// R5 (zero value, so the id is shown) instead of passing the object on.
	if len(usages) == 0 || usages[0].Name != "a" {
		t.Fatalf("usages %+v", usages)
	}
	usages[0].Name = "Changed"
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
}

func TestExternalCommitInvalidatesByGeneration(t *testing.T) {
	r := newRepo(t)
	ccRename(t, r, "Alpha")
	if got := ccName(t, r); got != "Alpha" {
		t.Fatalf("got %q", got)
	}
	start := ccCounts(r)
	other := openRepo(t, r.DataDir())
	ccRename(t, other, "Bravo")
	if got := ccName(t, r); got != "Bravo" {
		t.Fatalf("got %q", got)
	}
	if got := ccName(t, r); got != "Bravo" {
		t.Fatalf("got %q", got)
	}
	if got := ccSince(r, start)[0]; got != 1 {
		t.Fatalf("reads = %d", got)
	}
	ccRawRename(t, r, "Charlie")
	if got := ccName(t, r); got != "Charlie" {
		t.Fatalf("got %q", got)
	}
}

func TestRebuiltDatabaseWithSameGenerationInvalidates(t *testing.T) {
	r := newRepo(t)
	ccRename(t, r, "Alpha")
	head := r.cached().Head
	doc, _, err := decodeModelRows(must(r.store.Load())(t).Document)
	noError(t, err)
	profileByKey(doc, "default").Name = "Rebuilt"
	// Same generation number, different database: store_id must differ.
	must(r.createStore(doc, nil))(t)
	rcSQL(t, r.DatabasePath(), "UPDATE meta SET value = '"+strconv.FormatInt(head.Generation, 10)+"' WHERE key = 'generation'")
	if got := must(r.store.Load())(t).Head.Generation; got != head.Generation {
		t.Fatalf("generation %d, want %d", got, head.Generation)
	}
	if got := ccName(t, r); got != "Rebuilt" {
		t.Fatalf("got %q", got)
	}
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
				ext := "External"
				_, err := other.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &ext})
				done <- err
			}()
		}
	}
	r.dropCache()
	if ccName(t, r) == "External" {
		t.Fatal("the read saw the commit made after it")
	}
	noError(t, <-done)
	if got := ccName(t, r); got != "External" {
		t.Fatalf("got %q", got)
	}
}

func TestDamagedDatabaseRaisesInsteadOfUsingCachedCopy(t *testing.T) {
	r := newRepo(t)
	original := must(r.ExportAllJSON(bg, false))(t)
	origDoc, _, err := decodeModelRows(must(r.store.Load())(t).Document)
	noError(t, err)
	rcSQL(t, r.DatabasePath(),
		"UPDATE fields SET value = '{' WHERE scope = 'system' AND field = 'agents'",
		"UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'generation'")
	_, err = r.System(bg)
	wantError(t, err, ErrRepository, "配置数据库内容无效")
	must(r.createStore(origDoc, nil))(t)
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), original)
}

func TestInvalidWriteAndCallbackFailureDoNotPoisonCache(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAllJSON(bg, false))(t)
	err := r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = append(system.Agents, model.Agent{ID: "bad", ProfileID: "missing"})
		return nil
	})
	wantError(t, err, ErrValidation, "")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)

	failure := errors.New("callback failed")
	err = r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		if setErr := system.SystemConfig.Fields.Set("should_not_persist", json.RawMessage(`true`)); setErr != nil {
			return setErr
		}
		return failure
	})
	if err != failure {
		t.Fatalf("got %v", err)
	}
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
}

func TestFailedCommitKeepsDataAndDropsCache(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAllJSON(bg, false))(t)
	fingerprint := must(r.store.Fingerprint())(t)
	failure := errors.New("injected failure at COMMIT")
	r.store.BeforeCommit = func() error { return failure }
	uncommitted := "Uncommitted"
	_, err := r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &uncommitted})
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
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
	if got := ccSince(r, start)[0]; got != 1 {
		t.Fatalf("reads = %d", got)
	}
}

func TestNestedReadsUseTransactionAndFailedTransactionIsNotCached(t *testing.T) {
	r := newRepo(t)
	before := must(r.ExportAllJSON(bg, false))(t)
	failure := errors.New("abort")
	err := r.Update(bg, func(ctx context.Context, document *model.Document) error {
		profileByKey(document, "default").Name = "In transaction"
		if got := must(r.Profile(ctx, "default"))(t).Name; got != "In transaction" {
			t.Fatalf("got %q", got)
		}
		return failure
	})
	if err != failure {
		t.Fatalf("got %v", err)
	}
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
}

func TestSeparateRepositoryCachesObserveConcurrentCommits(t *testing.T) {
	r := newRepo(t)
	other := openRepo(t, r.DataDir())
	must(r.System(bg))(t)
	must(other.System(bg))(t)
	var group sync.WaitGroup
	failures := make(chan error, 20)
	for _, repository := range []*Repository{r, other} {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 5 {
				err := repository.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
					var count int64
					if raw, ok := system.SystemConfig.Fields.Get("counter"); ok {
						_ = json.Unmarshal(raw, &count)
					}
					raw, err := json.Marshal(count + 1)
					if err != nil {
						return err
					}
					return system.SystemConfig.Fields.Set("counter", raw)
				})
				if err == nil {
					_, err = repository.System(bg)
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
	for _, repo := range []*Repository{r, other} {
		raw, _ := must(repo.System(bg))(t).SystemConfig.Fields.Get("counter")
		wantJSONEqual(t, raw, 10)
	}
}

func TestLargeDocumentIsNotRetainedInCache(t *testing.T) {
	r := newRepo(t)
	must(r.System(bg))(t)
	r.cacheMaxBytes = must(r.store.Load())(t).Size - 1
	r.dropCache()
	start := ccCounts(r)
	must(r.System(bg))(t)
	must(r.System(bg))(t)
	if got := ccSince(r, start); got != [2]int64{2, 2} {
		t.Fatalf("reads, validations = %v", got)
	}
	if r.cached() != nil {
		t.Fatal("large document cached")
	}
}

func TestImportAndResetInvalidatePreviousGeneration(t *testing.T) {
	r := newRepo(t)
	saved := must(r.ExportAllJSON(bg, false))(t)
	imported := parseJSONObj(t, saved)
	imported.GetObject("profiles").GetObject("default").Set("name", "Imported")
	must(r.ImportAllJSON(bg, mustMarshalJSON(t, imported)))(t)
	if got := ccName(t, r); got != "Imported" {
		t.Fatalf("got %q", got)
	}
	must(r.ResetAllJSON(bg))(t)
	wantName := parseJSONObj(t, saved).GetObject("profiles").GetObject("default").GetString("name")
	if got := ccName(t, r); got != wantName {
		t.Fatalf("got %q, want %q", got, wantName)
	}
}
