package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/testjson"
)

// test_node_latency.py::test_raw_and_cached_node_credentials_are_preserved
func TestRawAndCachedNodeCredentialsArePreserved(t *testing.T) {
	t.Parallel()
	rawInput, err := json.Marshal(map[string]any{
		"id": "a", "name": "A", "type": "trojan", "server": "example.invalid",
		"port": 443, "password": "secret", "tls": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := defaultCollaborators(nil).ConvertNode(context.Background(), rawInput)
	if !ok {
		t.Fatal("expected converted raw node")
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	rawParsed, err := testjson.Parse(rawJSON)
	if err != nil {
		t.Fatal(err)
	}
	if pw, _ := testjson.Path(rawParsed, "password"); pw != "secret" {
		t.Fatalf("password = %v in %s", pw, rawJSON)
	}
	if tlsVal, _ := testjson.Path(rawParsed, "tls"); tlsVal != true {
		t.Fatalf("tls = %v in %s", tlsVal, rawJSON)
	}
	node := map[string]any{
		"name": "nested", "type": "vless", "server": "example.invalid", "port": 443,
		"params": map[string]any{
			"uuid": "uuid", "network": "ws",
			"ws-opts":      map[string]any{"path": "/path"},
			"reality-opts": map[string]any{"public-key": "public"},
		},
	}
	nodeInput, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	proxy, ok := defaultCollaborators(nil).ConvertNode(context.Background(), nodeInput)
	if !ok {
		t.Fatal("expected converted nested node")
	}
	proxyJSON, err := json.Marshal(proxy)
	if err != nil {
		t.Fatal(err)
	}
	proxyParsed, err := testjson.Parse(proxyJSON)
	if err != nil {
		t.Fatal(err)
	}
	uuid, _ := testjson.Path(proxyParsed, "uuid")
	wsOpts, _ := testjson.Path(proxyParsed, "ws-opts")
	pubKey, _ := testjson.Path(proxyParsed, "reality-opts", "public-key")
	if uuid != "uuid" || !testjson.Equal(wsOpts, map[string]any{"path": "/path"}) || pubKey != "public" {
		t.Fatalf("proxy = %s", proxyJSON)
	}
	if _, hasShortID := node["params"].(map[string]any)["reality-opts"].(map[string]any)["short-id"]; hasShortID {
		t.Fatal("cache mutated")
	}
}

func TestISOUTCOmitsZeroMicroseconds(t *testing.T) {
	t.Parallel()
	env := testEnv(nil, nil)
	if got := isoUTC(env.Now()); got != "2026-11-15T01:02:03.456789Z" {
		t.Fatal(got)
	}
}

// The DELETE /api/agents/<id> deleters clear both stores of the Agent in order.
func TestDeletedStoresDeleteBothStoresInOrder(t *testing.T) {
	dir := t.TempDir()
	stores := DeletedStores(dir)
	if len(stores) != 2 || stores[0].Name != "domain_probe" || stores[1].Name != "region_check" {
		t.Fatal(stores)
	}
	ctx := context.Background()
	for _, sub := range []string{"probes", "regions"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
		os.WriteFile(filepath.Join(dir, sub, "agent-1.json"), []byte("{}"), 0o644)
	}
	for _, deleter := range stores {
		if err := deleter.Delete(ctx, "agent-1"); err != nil {
			t.Fatal(err)
		}
	}
	for _, sub := range []string{"probes", "regions"} {
		if _, err := os.Stat(filepath.Join(dir, sub, "agent-1.json")); !os.IsNotExist(err) {
			t.Fatal(sub + " kept")
		}
	}
}
