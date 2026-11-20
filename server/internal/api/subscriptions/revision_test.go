package subscriptions_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
	_ "modernc.org/sqlite"
)

func TestConfigFindingsIdenticalSubscriptionPutDoesNotChangeRevision(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
		raw, err := json.Marshal([]any{
			testjson.Obj(
				testjson.Field{Key: "id", Value: "s"},
				testjson.Field{Key: "name", Value: "Synthetic"},
				testjson.Field{Key: "url", Value: "https://feed.example/sub"},
				testjson.Field{Key: "unknown", Value: testjson.Obj(testjson.Field{Key: "z", Value: 1}, testjson.Field{Key: "a", Value: 2})},
			),
		})
		if err != nil {
			return err
		}
		return shared.SetJSON("subscriptions", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := srv.Repo.Shared(srv.Context())
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
		srv.Put("/api/subscriptions/s", httpxtest.JSON(shared.Subscriptions[0])).WantStatus(200)
	}
	afterRevision, afterValue := readField()
	if beforeRevision != afterRevision || beforeValue != afterValue {
		t.Fatalf("same PUT rewrote subscription row: revision %d -> %d", beforeRevision, afterRevision)
	}
}
