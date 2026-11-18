package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/ippresence"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func preparedFixture(t *testing.T, goal string) (*Service, context.Context) {
	t.Helper()
	s, ctx := serviceFixture(t, goal)
	s.owners.DNS = identityDNS()
	gateway.Wire()
	ippresence.Wire()
	convert.SetMihomoEnv(mihomoenv.Env())
	_, err := s.repository.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("nodes", []any{jsonx.Obj("id", "synthetic-node", "name", "Synthetic node", "type", "ss", "server", "192.0.2.10", "port", jsonx.Int(443), "params", jsonx.Obj("password", "synthetic-node-password", "cipher", "aes-128-gcm"))})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seeded, _, err := s.repository.UpdateProfileTransaction(ctx, "setup_"+testSessionID[:20], func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		profile.GetObject("mihomo").Set("custom_config", "secret: synthetic-controller\nexternal-controller: 127.0.0.1:9090\ndns:\n  nameserver: [https://192.0.2.54/dns-query]\n")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSources(ctx, testSessionID, json.RawMessage(`{"node_ids":["synthetic-node"]}`)); err != nil {
		t.Fatal(err)
	}
	store := storeFor(s.repository)
	record, err := store.read(ctx, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	record.ProfileRevision = pointer(model.Int(seeded.GetInt("_revision")))
	if err := store.write(ctx, record); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}
func ownedForTest(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	owned := map[string]json.RawMessage{}
	for _, key := range []string{"proxy_groups", "rule_configs", "oxidns", "mihomo"} {
		owned[key] = fields[key]
	}
	var mihomo map[string]json.RawMessage
	json.Unmarshal(owned["mihomo"], &mihomo)
	var base string
	json.Unmarshal(mihomo["custom_config"], &base)
	node, err := yamlx.Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	mihomo["custom_config"], err = yamlx.ToJSON(node)
	if err != nil {
		t.Fatal(err)
	}
	owned["mihomo"], _ = json.Marshal(mihomo)
	out, _ := json.Marshal(owned)
	return out
}
func TestPrepareFieldsMatchRealPythonAndRetainOwnership(t *testing.T) {
	for _, goal := range []string{"gateway", "router"} {
		t.Run(goal, func(t *testing.T) {
			s, ctx := preparedFixture(t, goal)
			store := storeFor(s.repository)
			record, _ := store.read(ctx, testSessionID)
			profile, err := s.repository.GetProfile(ctx, record.ProfileID)
			if err != nil {
				t.Fatal(err)
			}
			shared, err := s.repository.GetShared(ctx)
			if err != nil {
				t.Fatal(err)
			}
			body := json.RawMessage(`{"domain_rules":{"direct":["builtin:chinamax"],"proxy":["builtin:telegram"]},"local_dns":"https://192.0.2.53/dns-query"}`)
			input, _ := json.Marshal(map[string]any{"profile": profile, "shared": shared, "session": record, "body": body})
			want := pythonSetup(t, `import json,sys,tempfile;from backend.common.config_repository import ProfileRepository;from backend.common import setup_flow as flow
x=json.load(sys.stdin)
with tempfile.TemporaryDirectory() as root:
 r=ProfileRepository(root);r.create_profile({'id':x['session']['profile_id'],'name':x['session']['name']});r.update_shared_transaction(lambda s:s.update({k:v for k,v in x['shared'].items() if k!='_revision'}));r.update_profile_transaction(x['session']['profile_id'],lambda p:p.update({k:v for k,v in x['profile'].items() if k!='_revision'}));flow.prepare(r,x['session'],x['body'],'http://localhost');json.dump(r.get_profile(x['session']['profile_id']),sys.stdout,ensure_ascii=False)`, input)
			if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
				t.Fatal(err)
			}
			saved, err := s.repository.GetProfile(ctx, record.ProfileID)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(saved)
			equalJSON(t, ownedForTest(t, raw), ownedForTest(t, want))
			revision := saved.GetInt("_revision")
			if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
				t.Fatal(err)
			}
			saved, _ = s.repository.GetProfile(ctx, record.ProfileID)
			if saved.GetInt("_revision") != revision {
				t.Fatal("idempotent prepare advanced profile revision")
			}
			_, _, err = s.repository.UpdateProfileTransaction(ctx, record.ProfileID, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
				p.GetObject("oxidns").GetObject("query_recorder").Set("enabled", false)
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			before := s.repository.CommitCount()
			path, _ := store.path(testSessionID)
			previous, _ := os.ReadFile(path)
			if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err == nil || err.Error() != msgForeignEdit {
				t.Fatal("foreign edit overwritten", err)
			}
			after, _ := os.ReadFile(path)
			if s.repository.CommitCount() != before || !bytes.Equal(previous, after) {
				t.Fatal("rejected preparation changed storage")
			}
		})
	}
}
