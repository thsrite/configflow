package weblistener_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"configflow/internal/api/settings"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/weblistener"
)

type edgeServer struct {
	*httpx.Edge
	server *http.Server
}

func (s *edgeServer) Serve(l net.Listener) error { return s.server.Serve(l) }

func unusedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// The remaining route-level listener contracts use real active native sockets;
// C06 alone owns the handler's HEAD/Bind changes.
func TestActiveListenerHTTPAuthenticationMissingPortAndSavedSettings(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	settingsHandler := settings.NewHandler(s.Repo, nil, nil)
	settingsHandler.Register(s.App.Router)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	edge := &edgeServer{Edge: &httpx.Edge{API: s.Handler(), Root: t.TempDir()}}
	edge.server = &http.Server{Handler: edge.Edge}
	m := &weblistener.Manager{DataDir: s.DataDir, Server: edge, Spawner: runtime, IPv6Available: func() bool { return false }, LookupEnv: func(string) (string, bool) { return "", false }}
	t.Cleanup(m.Close)
	first := unusedPort(t)
	path := filepath.Join(s.DataDir, "web-listener.json")
	if err := os.WriteFile(path, []byte(`{"port":`+strconv.Itoa(first)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := settingsHandler.SetWebListener(m); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	request := func(method string, port int, body string, token httpxtest.Option) (int, map[string]json.RawMessage) {
		t.Helper()
		req, err := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/settings/web-port", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != nil {
			token(req)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		var value map[string]json.RawMessage
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(string(data), err)
		}
		return res.StatusCode, value
	}
	for _, method := range []string{"GET", "POST"} {
		if status, _ := request(method, first, `{"port":18088}`, nil); status != 401 {
			t.Fatal("unprotected active route", method, status)
		}
	}
	token := s.Login()
	for _, body := range []string{`{}`, `[]`, `{"server_domain":"http://example.com"}`} {
		if status, _ := request("POST", first, body, token); status != 400 {
			t.Fatal("missing port accepted", body, status)
		}
	}
	second := unusedPort(t)
	status, value := request("POST", first, `{"port":`+strconv.Itoa(second)+`}`, token)
	if status != 200 || string(value["port"]) != strconv.Itoa(second) || string(value["source"]) != `"saved"` || string(value["success"]) != "true" {
		t.Fatal(status, value)
	}
	status, value = request("GET", second, "", token)
	if status != 200 || string(value["port"]) != strconv.Itoa(second) {
		t.Fatal("new socket disagrees with settings", status, value)
	}
	data, _ := os.ReadFile(path)
	var saved struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.Port != second {
		t.Fatal("saved port mismatch", err)
	}
	unsupported := httpxtest.New(t, httpxtest.Options{Auth: true, DataDir: s.DataDir})
	settings.NewHandler(unsupported.Repo, nil, nil).Register(unsupported.App.Router)
	edge.Edge.API = unsupported.Handler()
	unsupportedToken := unsupported.Login()
	status, value = request("GET", second, "", unsupportedToken)
	if status != 200 || string(value["supported"]) != "false" {
		t.Fatal(status, value)
	}
	if status, _ := request("POST", second, `{"port":18088}`, unsupportedToken); status != 503 {
		t.Fatal("unsupported deployment changed", status)
	}
}
