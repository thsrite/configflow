package rulelibrary

import (
	"bytes"
	"context"
	"testing"

	"configflow/internal/model"
)

func TestN811SectionEditsPreserveUnchangedBytes(t *testing.T) {
	s := fixture(t)
	ctx := s.Context()
	if err := s.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", []byte(`[{"id":"untouched","name":"Untouched","url":"https://subscription.example/source","opaque":"<subscription>","number":1e+02}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", []byte(`{"opaque":"<backup>","number":1e+02}`))
	}); err != nil {
		t.Fatal(err)
	}
	read := func() ([]byte, []byte) {
		t.Helper()
		shared, err := s.Repo.Shared(ctx)
		if err != nil {
			t.Fatal(err)
		}
		system, err := s.Repo.System(ctx)
		if err != nil {
			t.Fatal(err)
		}
		subscriptions, err := model.Marshal(shared.Subscriptions)
		if err != nil {
			t.Fatal(err)
		}
		backup, err := model.Marshal(system.Backup)
		if err != nil {
			t.Fatal(err)
		}
		return subscriptions, backup
	}
	beforeSubscriptions, beforeBackup := read()
	for _, raw := range [][]byte{beforeSubscriptions, beforeBackup} {
		if !bytes.Contains(raw, []byte("<")) || !bytes.Contains(raw, []byte("1e+02")) {
			t.Fatalf("fixture lost original text: %s", raw)
		}
	}
	if err := editShared(ctx, func(_ context.Context, catalog *record) error {
		return put(catalog, "rule_library", []record{recordJSON(t, `{"id":"record","name":"Record","source_type":"content","content":"+.records.example\n","behavior":"domain","format":"text"}`)})
	}); err != nil {
		t.Fatal(err)
	}
	if err := editSystem(ctx, func(system *record) error {
		settings := child(*system, "system_config")
		if err := put(&settings, "github_proxy_domain", "https://proxy.example"); err != nil {
			return err
		}
		return put(system, "system_config", settings)
	}); err != nil {
		t.Fatal(err)
	}
	afterSubscriptions, afterBackup := read()
	if !bytes.Equal(beforeSubscriptions, afterSubscriptions) {
		t.Fatalf("unmodified subscriptions: %s -> %s", beforeSubscriptions, afterSubscriptions)
	}
	if !bytes.Equal(beforeBackup, afterBackup) {
		t.Fatalf("unmodified backup: %s -> %s", beforeBackup, afterBackup)
	}
}
