package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/agentfake"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
)

// TestPublishLegacyProxyAgainstFlask covers the first A14 trust-env route with
// real production clients. N2 intentionally keeps Go's native loopback bypass.
func TestPublishLegacyProxyAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	fake := agentfake.New(agentfake.Options{AgentID: "a14-agent", Token: "a14-synthetic-token", ServiceType: "surge"})
	receiver := httptest.NewServer(fake)
	defer receiver.Close()
	_, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(receiver.URL, "http://"))
	port, _ := strconv.Atoi(rawPort)
	proxy := &recordingProxy{}
	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()
	for _, key := range []string{"HTTP_PROXY", "http_proxy"} {
		t.Setenv(key, proxyServer.URL)
	}
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	root := t.TempDir()
	pyDir, goDir := filepath.Join(root, "python"), filepath.Join(root, "go")
	publishSeed(t, pyDir, port)
	repository, err := repo.Open(pyDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = repository.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		return system.Agents[0].SetJSON("service_type", []byte(`"surge"`))
	})
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, pyDir, goDir)
	py, stopPy := startPythonArgs(t, pyDir, "server/internal/api/agents/difftest/testdata/publish_app.py")
	defer stopPy()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pt, gt := login(t, py), login(t, goBase)
	request := request{name: "Surge publish through environment proxy", method: "POST", path: "/api/agents/a14-agent/push-config", body: `{"restart":false}`, contentType: "application/json", admin: true}
	proxy.take()
	left := send(t, py, request, pt, nil)
	pyProxy := proxy.take()
	first := fake.Calls()
	right := send(t, goBase, request, gt, nil)
	goProxy := proxy.take()
	all := fake.Calls()
	var leftJSON, rightJSON any
	if json.Unmarshal(left.body, &leftJSON) != nil || json.Unmarshal(right.body, &rightJSON) != nil {
		t.Fatal("invalid response JSON")
	}
	if left.status != 200 || right.status != 200 || !reflect.DeepEqual(leftJSON, rightJSON) {
		t.Fatalf("response: Python=%d %s Go=%d %s", left.status, left.body, right.status, right.body)
	}
	for _, header := range []string{"Content-Type", "Cache-Control"} {
		if left.header.Get(header) != right.header.Get(header) {
			t.Fatal("response header", header)
		}
	}
	expected := "POST " + receiver.URL + "/api/config/update"
	if !reflect.DeepEqual(pyProxy, []string{expected}) || len(goProxy) != 0 {
		t.Fatalf("N2 native proxy choice: Python=%q Go=%q", pyProxy, goProxy)
	}
	if len(first) != 1 || len(all) != 2 {
		t.Fatalf("unexpected Agent calls %d/%d", len(first), len(all))
	}
	a, b := all[0], all[1]
	if a.Method != "POST" || a.Path != "/api/config/update" || a.Method != b.Method || a.Path != b.Path || !bytes.Equal(a.Body, b.Body) {
		t.Fatal("legacy Agent raw request differs")
	}
	for _, header := range []string{"Authorization", "Content-Type", "Content-Length"} {
		if a.Headers.Get(header) != b.Headers.Get(header) {
			t.Fatal("Agent raw header", header)
		}
	}
	// Compare the complete durable document, not just the public Agent projection.
	var documents [2]any
	for i, dir := range []string{pyDir, goDir} {
		entry, e := store.New(filepath.Join(dir, "configflow.sqlite3"), func() time.Duration { return 10 * time.Second }).Load()
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(entry.Document)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(raw, &documents[i]); e != nil {
			t.Fatal(e)
		}
		normalizePublication(documents[i], map[string]string{})
	}
	if !reflect.DeepEqual(documents[0], documents[1]) {
		t.Fatal("complete DB document: ", publishDifference("", documents[0], documents[1]))
	}
	comparePublicationFiles(t, pyDir, goDir, []map[string]string{{}, {}})
	t.Log("legacy publication: same HTTP response, raw Agent bytes/headers and full DB/files; Python proxy=1, Go native loopback bypass=0")
}
