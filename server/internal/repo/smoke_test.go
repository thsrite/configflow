package repo

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSmokeFreshOpen(t *testing.T) {
	ctx := context.Background()
	r, err := OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := r.ListProfileRecords(ctx)
	if err != nil || len(profiles) != 1 {
		t.Fatalf("profiles %v %v", profiles, err)
	}
	if _, err := r.CreateProfileRecord(ctx, ProfileCreation{ID: "work", Name: "Work"}, "default"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.SaveProfileFieldsJSON(ctx, "work", json.RawMessage(`{"mihomo":{"custom_config":"x: 1"}}`), nil); err != nil {
		t.Fatal(err)
	}
	exported, err := r.ExportAllJSON(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ImportAllJSON(ctx, exported); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteProfile(ctx, "work", ""); err != nil {
		t.Fatal(err)
	}
	again, err := OpenDefault(r.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := again.DefaultProfileID(ctx); id != "default" {
		t.Fatal(id)
	}
}
