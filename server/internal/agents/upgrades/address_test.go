package upgrades

// The two cases of backend/test_agent_address_follow.py that exercise
// _reaches and _ask themselves (the route cases are in internal/api/agents),
// against a loopback fake Agent instead of a monkeypatched requests.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

type recorded struct {
	method, path, authorization, contentType, body string
}

func fakeAgent(t *testing.T, status *int, reply string) (string, string, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	var calls []recorded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(*status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	host, port, _ := strings.Cut(strings.TrimPrefix(server.URL, "http://"), ":")
	return host, port, &calls
}

func plainManager(t *testing.T) *agents.Manager {
	t.Helper()
	agents.ResetRuntime()
	agents.ResetHooks()
	t.Cleanup(func() { agents.ResetRuntime(); agents.ResetHooks() })
	r, err := repo.Open(filepath.Join(t.TempDir(), "config"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheNewAddressIsProbedWithTheAgentsTokenOnAnEndpointTheGoAgentHas(t *testing.T) {
	m := plainManager(t)
	status := 200
	host, port, calls := fakeAgent(t, &status, `{}`)
	record := jsonx.Obj("port", jsonx.Number(port), "token", "agent-token")
	if ok, err := reaches(m, record, host); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if len(*calls) != 1 || (*calls)[0] != (recorded{"GET", "/api/capabilities", "Bearer agent-token", "", ""}) || VerifyTimeout != 4 {
		t.Fatalf("calls %v", *calls)
	}
	status = 401 // another Agent, or one that lost its identity
	if ok, _ := reaches(m, record, host); ok {
		t.Fatal("401 reached")
	}
	if ok, _ := reaches(m, jsonx.Obj("port", 1, "token", "agent-token"), "127.0.0.1"); ok {
		t.Fatal("closed port reached")
	}
}

func TestAddressEndpointRequestsCarryTheAgentsToken(t *testing.T) {
	m := plainManager(t)
	status := 200
	host, port, calls := fakeAgent(t, &status, `{"writable": true}`)
	record := jsonx.Obj("port", jsonx.Number(port), "token", "agent-token")
	code, data, err := ask(m, record, host, "POST", jsonx.Obj("host", "192.0.2.11"))
	if err != nil || code != 200 || !jsonx.Equal(data, jsonx.Obj("writable", true)) {
		t.Fatalf("%d %v %v", code, data, err)
	}
	if (*calls)[0] != (recorded{"POST", "/api/agent-address", "Bearer agent-token", "application/json", `{"host": "192.0.2.11"}`}) {
		t.Fatalf("calls %v", *calls)
	}
	code, data, _ = ask(m, jsonx.Obj("port", 1, "token", "agent-token"), "127.0.0.1", "GET", nil)
	if code != 0 || data.Len() != 0 {
		t.Fatalf("%d %v", code, data)
	}
	// A GET sends no body; a non-object answer is {}.
	host2, port2, calls2 := fakeAgent(t, &status, `[1]`)
	code, data, _ = ask(m, jsonx.Obj("port", jsonx.Number(port2), "token", "t"), host2, "GET", nil)
	if code != 200 || data.Len() != 0 || (*calls2)[0] != (recorded{"GET", "/api/agent-address", "Bearer t", "", ""}) {
		t.Fatalf("%d %v %v", code, data, *calls2)
	}
}
