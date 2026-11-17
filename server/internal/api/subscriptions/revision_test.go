package subscriptions_test

import (
	"path/filepath"
	"testing"

	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"database/sql"
	_ "modernc.org/sqlite"
)

func TestConfigFindingsIdenticalSubscriptionPutDoesNotChangeRevision(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
	_, err := srv.Repo.SaveShared(srv.Context(), jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Synthetic", "url", "https://feed.example/sub", "unknown", jsonx.Obj("z", 1, "a", 2))}), nil)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := srv.Repo.GetShared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	readField := func() (int64, string) {
		t.Helper()
		db, err := sql.Open("sqlite", filepath.Join(srv.DataDir, "configflow.sqlite3"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var revision int64
		var value string
		if err := db.QueryRow("SELECT revision,value FROM fields WHERE scope='shared' AND owner='' AND field='subscriptions'").Scan(&revision, &value); err != nil {
			t.Fatal(err)
		}
		return revision, value
	}
	beforeRevision, beforeValue := readField()
	for range 2 {
		srv.Put("/api/subscriptions/s", httpxtest.JSON(shared.GetList("subscriptions")[0])).WantStatus(200)
	}
	afterRevision, afterValue := readField()
	if beforeRevision != afterRevision || beforeValue != afterValue {
		t.Fatalf("same PUT rewrote subscription row: revision %d -> %d", beforeRevision, afterRevision)
	}
}
