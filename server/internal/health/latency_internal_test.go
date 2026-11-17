package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// test_node_latency.py::test_raw_and_cached_node_credentials_are_preserved
func TestRawAndCachedNodeCredentialsArePreserved(t *testing.T) {
	t.Parallel()
	raw := proxyOf(context.Background(), jsonx.Obj("id", "a", "name", "A", "type", "trojan", "server", "example.invalid",
		"port", jsonx.Int(443), "password", "secret", "tls", true), nil)
	if raw.GetString("password") != "secret" || raw.Value("tls") != true {
		t.Fatal(raw)
	}
	node := jsonx.Obj("name", "nested", "type", "vless", "server", "example.invalid", "port", jsonx.Int(443),
		"params", jsonx.Obj("uuid", "uuid", "network", "ws", "ws-opts", jsonx.Obj("path", "/path"),
			"reality-opts", jsonx.Obj("public-key", "public")))
	proxy := proxyOf(context.Background(), node, nil)
	if proxy.GetString("uuid") != "uuid" || !jsonx.Equal(proxy.Value("ws-opts"), jsonx.Obj("path", "/path")) ||
		proxy.GetObject("reality-opts").GetString("public-key") != "public" {
		t.Fatal(proxy)
	}
	if node.GetObject("params").GetObject("reality-opts").Has("short-id") {
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

// The DELETE /api/agents/<id> hooks clear both stores of the Agent.
func TestWireRegistersTheAgentDeleteHooksInOrder(t *testing.T) {
	// Sequential: resets and installs the process-wide Agent delete hooks.
	agents.ResetHooks()
	t.Cleanup(agents.ResetHooks)
	Wire()
	names := agents.AgentDeletedHookNames()
	if len(names) != 2 || names[0] != "domain_probe" || names[1] != "region_check" {
		t.Fatal(names)
	}
	dir := t.TempDir()
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	for _, sub := range []string{"probes", "regions"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
		os.WriteFile(filepath.Join(dir, sub, "agent-1.json"), []byte("{}"), 0o644)
	}
	for _, hook := range agents.AgentDeletedHooks() {
		if err := hook(ctx, "agent-1"); err != nil {
			t.Fatal(err)
		}
	}
	for _, sub := range []string{"probes", "regions"} {
		if _, err := os.Stat(filepath.Join(dir, sub, "agent-1.json")); !os.IsNotExist(err) {
			t.Fatal(sub + " kept")
		}
	}
}
