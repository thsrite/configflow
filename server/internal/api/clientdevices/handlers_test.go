package clientdevices_test

import (
	"configflow/internal/clients"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/clientdevices"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/yamlx"
)

func rosState() routerfake.State {
	return routerfake.State{Tables: map[string][]routerfake.Row{"/ip/dhcp-server": {{".id": "*1", "name": "dhcp-example", "interface": "bridge", "disabled": "false"}}, "/ip/dhcp-server/network": {{".id": "*2", "address": "192.0.2.0/24", "gateway": "192.0.2.1"}}, "/ip/address": {{".id": "*3", "address": "192.0.2.1/24", "interface": "bridge"}}, "/ip/dhcp-server/lease": {{".id": "*10", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "server": "dhcp-example", "dynamic": "true", "disabled": "false", "comment": ""}}, "/ip/arp": {{".id": "*11", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "interface": "bridge", "status": "reachable"}}}}
}
func fixture(t *testing.T) (*httpxtest.Server, *routerfake.Fake) {
	t.Helper()
	f := routerfake.New(nil)
	f.Reset(rosState())
	device := httptest.NewServer(f)
	t.Cleanup(device.Close)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, SecretKey: "synthetic-fixed-secret", Register: httpxtest.Routes(api.Register)})
	routers.Wire()
	_, _, err := s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("device_notes", jsonx.Obj("192.0.2.21", "Synthetic"))
		current.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "router-one", "name", "Synthetic", "type", "routeros", "endpoint", device.URL, "username", "synthetic-user", "password", "synthetic-pass", "verify_tls", false)}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}
func requestBody(operation string) any {
	return map[string]any{"profile_id": "default", "operation": operation, "devices": []any{map[string]any{"ip": "192.0.2.21", "mac": "02:00:00:00:00:21", "router_id": "router-one", "name": "Synthetic"}}}
}
func TestR17HTTPRoutes(t *testing.T) {
	s, f := fixture(t)
	login := s.Login()
	s.Get("/api/client-devices").WantStatus(401)
	s.Get("/api/client-devices", login).WantStatus(200)
	before, err := s.Repo.ConfigRevision(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	head := s.Do("HEAD", "/api/client-devices", login, httpxtest.JSON(map[string]any{"profile_id": "default", "ip": "192.0.2.21", "name": "must-not-save", "mode": "", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""}))
	head.WantStatus(200)
	if len(head.Body) != 0 {
		t.Fatal("HEAD returned body")
	}
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if before != after {
		t.Fatal("HEAD wrote profile")
	}
	if head.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	preview := s.Post("/api/client-devices/fixed-ip/preview", login, httpxtest.JSON(requestBody("add")))
	preview.WantStatus(200)
	token := preview.Object().GetString("token")
	if token == "" || token == "[REDACTED]" {
		t.Fatal("preview token missing/redacted")
	}
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": token})).WantStatus(200)
	stored, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	binding := stored.GetObject("client_fixed_ips").GetObject("192.0.2.21")
	if binding == nil || binding.GetString("router_id") != "router-one" {
		t.Fatalf("binding %+v", binding)
	}
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": token})).WantStatus(502)
	discovery := s.Get("/api/client-devices/discovery?refresh=1", login)
	discovery.WantStatus(200)
	if !strings.Contains(discovery.Text(), `"owned":true`) {
		t.Fatalf("fixed enrichment %s", discovery.Body)
	}
	deletion := s.Post("/api/client-devices/fixed-ip/preview", login, httpxtest.JSON(requestBody("delete")))
	deletion.WantStatus(200)
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": deletion.Object().GetString("token")})).WantStatus(200)
	if len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 0 {
		t.Fatal("owned reservation remains")
	}
	for _, call := range f.Calls() {
		if call.Method == http.MethodGet || call.Method == http.MethodDelete {
			if call.Body != nil {
				t.Fatal("GET/DELETE body")
			}
		}
	}
}
func TestR17HTTPFailureShapesAndPeerSecrets(t *testing.T) {
	s, _ := fixture(t)
	login := s.Login()
	for _, tc := range []struct {
		path, body string
		status     int
	}{{"/api/client-devices", `[]`, 400}, {"/api/client-devices/fixed-ip/preview", `{}`, 400}, {"/api/client-devices/fixed-ip/apply", `{"profile_id":"other","token":"bad"}`, 409}, {"/api/client-devices/fixed-ip/apply", `{"profile_id":"default","token":"bad"}`, 409}} {
		method := "POST"
		if tc.path == "/api/client-devices" {
			method = "PUT"
		}
		s.Do(method, tc.path, login, httpxtest.Body([]byte(tc.body), "application/json")).WantStatus(tc.status)
	}
	_, _, err := s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("device_notes", jsonx.Obj("192.0.2.21", "reference synthetic-peer-password"))
		current.GetObject("routers").Set("items", append(current.GetObject("routers").GetList("items"), jsonx.Obj("id", "peer", "type", "unsupported", "username", "visible-user", "password", "synthetic-peer-password")))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result := s.Get("/api/client-devices", login)
	result.WantStatus(200)
	if strings.Contains(result.Text(), "synthetic-peer-password") {
		t.Fatal("public list leaked a configured credential")
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(result.Body, &doc) != nil {
		t.Fatal("invalid public response")
	}
}

func TestR17AgentProfilePinning(t *testing.T) {
	s, _ := fixture(t)
	login := s.Login()
	if _, err := s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "other", "name", "Other"), ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("agents", []any{jsonx.Obj("id", "agent-other", "name", "Other OxiDNS", "service_type", "oxidns", "profile_id", "other", "host", "192.0.2.80", "token", "synthetic-other-agent-token")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result := s.Get("/api/client-devices?agent=agent-other", login)
	result.WantStatus(200)
	if result.Object().GetString("profile_id") != "other" {
		t.Fatal("Agent profile not pinned")
	}
	s.Do("HEAD", "/api/client-devices?agent=agent-other", login).WantStatus(200)
	s.Put("/api/client-devices?agent=agent-other", login, httpxtest.JSON(map[string]string{"profile_id": "other", "ip": "192.0.2.21", "name": "wrong profile", "mode": "", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""})).WantStatus(409)
}

func TestR17SetupGuardAndExpiry(t *testing.T) {
	s, f := fixture(t)
	login := s.Login()
	preview := s.Post("/api/client-devices/fixed-ip/preview", login, httpxtest.JSON(requestBody("add")))
	preview.WantStatus(200)
	signer := clients.FixedIPToken{Secret: "synthetic-fixed-secret"}
	payload, err := signer.Verify(preview.Object().GetString("token"))
	if err != nil {
		t.Fatal(err)
	}
	old := signer
	old.Now = func() time.Time { return time.Now().Add(-601 * time.Second) }
	expired, err := old.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": expired})).WantStatus(409)
	directory := filepath.Join(s.DataDir, "setup-sessions")
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, strings.Repeat("a", 32)+".json"), []byte(`{"profile_id":"default","completed":false,"abandoned":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	s.Post("/api/client-devices/fixed-ip/preview", login, httpxtest.JSON(requestBody("add"))).WantStatus(409)
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": preview.Object().GetString("token")})).WantStatus(409)
	if len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 1 || f.Snapshot().Tables["/ip/dhcp-server/lease"][0]["dynamic"] != "true" {
		t.Fatal("blocked write changed lease")
	}
}

func TestR17UDPProtocolSideEntryHTTP(t *testing.T) {
	s, _ := fixture(t)
	login := s.Login()
	p := oxidns.NewProfile(nil)
	draft, err := oxidns.PrepareEditor(oxidns.M("local_dns", "udp://192.0.2.53", "remote_dns", "https://dns.example/q"), p, "")
	if err != nil {
		t.Fatal(err)
	}
	document, err := yamlx.Parse([]byte(yamlx.Get(draft, "native_config").Value))
	if err != nil {
		t.Fatal(err)
	}
	plugins := yamlx.Get(document, "plugins")
	changed := false
	for _, plugin := range plugins.Content {
		if yamlx.Get(plugin, "type").Value == "udp_server" && yamlx.Get(yamlx.Get(plugin, "args"), "entry").Value == "sequence_main" {
			yamlx.Set(plugin, "tag", oxidns.S("udp"))
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("missing primary UDP fixture")
	}
	side := oxidns.M("tag", "side_forward", "type", "forward", "args", oxidns.M("upstreams", oxidns.L(oxidns.M("addr", "udp://192.0.2.54:53"))))
	plugins.Content = append(plugins.Content, side, oxidns.M("tag", "side_udp", "type", "udp_server", "args", oxidns.M("listen", "127.0.0.1:5353", "entry", "side_forward")))
	encoded, err := yamlx.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(draft, "native_config", oxidns.S(string(encoded)))
	var settings jsonx.Object
	if err = json.Unmarshal(oxidns.ToJSON(draft), &settings); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("oxidns", &settings)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	get := s.Get("/api/client-devices", login)
	get.WantStatus(200)
	if !get.Object().GetBool("editable") || len(get.Object().GetList("side_entries")) != 1 {
		t.Fatal("udp scheme misclassified as plugin reference")
	}
	s.Put("/api/client-devices", login, httpxtest.JSON(map[string]string{"profile_id": "default", "ip": "192.0.2.21", "name": "Synthetic", "mode": "direct", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""})).WantStatus(200)
	stored, _ := s.Repo.GetProfile(s.Context(), "default")
	if !strings.Contains(stored.GetObject("oxidns").GetString("native_config"), "side_forward") {
		t.Fatal("side plugin lost")
	}
}

func TestR17MalformedDiscoveryRowsPreserveHealthyDevice(t *testing.T) {
	s, f := fixture(t)
	seed := f.Snapshot()
	seed.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/arp", Count: 1, Status: 200, RawBody: `[{".id":"*11","address":"192.0.2.21","mac-address":"02:00:00:00:00:21","interface":"bridge","status":"reachable"},null,false,"bad"]`}}
	f.Reset(seed)
	result := s.Get("/api/client-devices/discovery?refresh=1", s.Login())
	result.WantStatus(200)
	devices := result.Object().GetList("devices")
	if len(devices) != 1 {
		t.Fatalf("healthy device hidden: %s", result.Body)
	}
	row := devices[0].(*jsonx.Object)
	if row.GetString("mac") != "02:00:00:00:00:21" || row.GetObject("fixed_ip") == nil {
		t.Fatalf("healthy evidence dropped: %s", result.Body)
	}
}
