package rulesource

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestServiceReadInlineAndOffline(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(context.Background())
	svc := New(Options{Repo: r})

	cfgRaw := json.RawMessage(`{"profile_id":"default","rule_library":[{"id":"lib1","name":"Lib","source_type":"content","behavior":"ipcidr","format":"text","content":"192.0.2.0/24\n"}]}`)
	itemRaw := json.RawMessage(`{"id":"r1","library_rule_id":"lib1","behavior":"ipcidr"}`)
	libRaw := json.RawMessage(`{"id":"lib1","name":"Lib","source_type":"content","behavior":"ipcidr","format":"text","content":"192.0.2.0/24\n"}`)

	got, err := svc.ReadJSON(ctx, cfgRaw, itemRaw, libRaw, ContentOptions{})
	if err != nil || got != "192.0.2.0/24\n" {
		t.Fatalf("ReadJSON inline = (%q, %v), want (192.0.2.0/24\\n, nil)", got, err)
	}

	gotOffline, err := svc.ReadOfflineJSON(ctx, cfgRaw, itemRaw, libRaw)
	if err != nil || gotOffline != "192.0.2.0/24\n" {
		t.Fatalf("ReadOfflineJSON = (%q, %v), want (192.0.2.0/24\\n, nil)", gotOffline, err)
	}

	cfg, err := model.DecodeConfig(cfgRaw)
	if err != nil {
		t.Fatal(err)
	}
	var row model.RuleConfig
	if err := json.Unmarshal(itemRaw, &row); err != nil {
		t.Fatal(err)
	}
	var lib model.RuleLibraryItem
	if err := json.Unmarshal(libRaw, &lib); err != nil {
		t.Fatal(err)
	}
	gotGateway, err := svc.ReadGateway(ctx, row, &lib, FetchConfig(cfg))
	if err != nil || gotGateway != "192.0.2.0/24\n" {
		t.Fatalf("ReadGateway = (%q, %v), want (192.0.2.0/24\\n, nil)", gotGateway, err)
	}
}
