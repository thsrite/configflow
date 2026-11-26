package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo/staterows"
	"configflow/internal/store"
)

// TestReviewB53AlreadyV2RouterLastReleaseMovesOnFirstCommit covers a database
// upgraded to storage version 2 before router last_release moved to
// agent_state (batch-50): the version is not bumped again, so opening it
// moves nothing; the view still reads last_release from the configuration
// row, and the first commit moves it to exactly one state row.
func TestReviewB53AlreadyV2RouterLastReleaseMovesOnFirstCommit(t *testing.T) {
	r := newRepo(t)
	_, err := r.UpdateProfile(bg, DefaultProfileID, func(_ context.Context, profile *model.Profile) error {
		profile.Routers.Items = []model.Router{{ID: "r1", Name: "Main", Type: "routeros", Endpoint: "http://192.0.2.1",
			LastRelease: &model.RouterRelease{At: "2026-11-23T00:00:00", OK: true, Fingerprint: "fp-1"}}}
		return nil
	})
	noError(t, err)
	path := r.DatabasePath()
	r.Close()

	// Rewrite the file into the batch-50 v2 layout: last_release inline in
	// the routers row, no router state rows, storage_version still 2.
	db := must(sql.Open("sqlite", "file:"+path))(t)
	var routers string
	noError(t, db.QueryRow("SELECT value FROM fields WHERE scope='profile' AND owner=? AND field='routers'", DefaultProfileID).Scan(&routers))
	var rows []store.StateRow
	result := must(db.Query("SELECT agent_id, member, value FROM agent_state WHERE agent_id LIKE 'router/%'"))(t)
	for result.Next() {
		var row store.StateRow
		var value string
		noError(t, result.Scan(&row.Owner, &row.Member, &value))
		row.Value = json.RawMessage(value)
		rows = append(rows, row)
	}
	noError(t, result.Close())
	if len(rows) != 1 {
		t.Fatalf("router state rows = %d, want 1", len(rows))
	}
	inline := string(staterows.MergeRouters(DefaultProfileID, []byte(routers), rows))
	if !strings.Contains(inline, `"last_release"`) {
		t.Fatalf("merge did not inline last_release: %s", inline)
	}
	must(db.Exec("UPDATE fields SET value=? WHERE scope='profile' AND owner=? AND field='routers'", inline, DefaultProfileID))(t)
	must(db.Exec("DELETE FROM agent_state WHERE agent_id LIKE 'router/%'"))(t)
	noError(t, db.Close())

	reopened := openRepo(t, r.DataDir())
	if v := o17Version(t, path); v != store.StorageVersion {
		t.Fatalf("version = %d", v)
	}
	if n := o17Count(t, path, "SELECT count(*) FROM agent_state WHERE agent_id LIKE 'router/%'"); n != 0 {
		t.Fatalf("open moved %d router rows; an already-v2 file is not upgraded again", n)
	}
	got := must(reopened.Profile(bg, DefaultProfileID))(t).Routers.Items[0]
	if got.LastRelease == nil || got.LastRelease.Fingerprint != "fp-1" || got.StateRestoredAt != nil {
		t.Fatalf("view before first commit: %+v", got)
	}
	exported := must(reopened.ExportAllJSON(bg, false))(t)
	if strings.Contains(string(exported), `"last_release"`) {
		t.Fatal("export of a not-yet-moved v2 file carries last_release")
	}

	// Any commit moves it: one state row, no inline copy, same view.
	noError(t, reopened.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "a", Name: "A", Host: "192.0.2.2", Port: 8080, ServiceType: "mihomo", ProfileID: "default"}}
		return nil
	}))
	if n := o17Count(t, path, "SELECT count(*) FROM agent_state WHERE agent_id='router/"+DefaultProfileID+"/r1' AND member='router.last_release'"); n != 1 {
		t.Fatalf("router last_release state rows after first commit = %d, want 1", n)
	}
	if n := o17Count(t, path, "SELECT count(*) FROM fields WHERE scope='profile' AND field='routers' AND value LIKE '%last_release%'"); n != 0 {
		t.Fatal("last_release still inline after the first commit")
	}
	got = must(reopened.Profile(bg, DefaultProfileID))(t).Routers.Items[0]
	if got.LastRelease == nil || got.LastRelease.Fingerprint != "fp-1" || got.StateRestoredAt != nil {
		t.Fatalf("view after first commit: %+v", got)
	}
}
