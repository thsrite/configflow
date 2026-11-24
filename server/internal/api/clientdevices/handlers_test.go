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
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func decodeBody(t *testing.T, res *httpxtest.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatalf("%v: %s", err, res.Body)
	}
	return out
}

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
	previous := repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: routers.JobLockRecord}})
	t.Cleanup(func() { repo.SetHooks(previous) })
	routersRaw, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"id": "router-one", "name": "Synthetic", "type": "routeros", "endpoint": device.URL, "username": "synthetic-user", "password": "synthetic-pass", "verify_tls": false}}})
	_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, current *model.Profile) error {
		if err := current.SetJSON("device_notes", json.RawMessage(`{"192.0.2.21":"Synthetic"}`)); err != nil {
			return err
		}
		return current.SetJSON("routers", routersRaw)
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
	token, _ := decodeBody(t, preview)["token"].(string)
	if token == "" || token == "[REDACTED]" {
		t.Fatal("preview token missing/redacted")
	}
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": token})).WantStatus(200)
	stored, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := stored.ClientFixedIPs.Get("192.0.2.21")
	if !ok || binding.RouterID != "router-one" {
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
	delToken, _ := decodeBody(t, deletion)["token"].(string)
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": delToken})).WantStatus(200)
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
	_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, current *model.Profile) error {
		if err := current.SetJSON("device_notes", json.RawMessage(`{"192.0.2.21":"reference synthetic-peer-password"}`)); err != nil {
			return err
		}
		var peer model.Router
		if err := json.Unmarshal([]byte(`{"id":"peer","type":"unsupported","username":"visible-user","password":"synthetic-peer-password"}`), &peer); err != nil {
			return err
		}
		current.Routers.Items = append(current.Routers.Items, peer)
		return nil
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
	if _, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, current *model.System) error {
		return current.SetJSON("agents", json.RawMessage(`[{"id":"agent-other","name":"Other OxiDNS","service_type":"oxidns","profile_id":"other","host":"192.0.2.80","token":"synthetic-other-agent-token"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	result := s.Get("/api/client-devices?agent=agent-other", login)
	result.WantStatus(200)
	if decodeBody(t, result)["profile_id"] != "other" {
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
	previewToken, _ := decodeBody(t, preview)["token"].(string)
	signer := clients.FixedIPToken{Secret: "synthetic-fixed-secret"}
	payload, err := signer.Verify(previewToken)
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
	s.Post("/api/client-devices/fixed-ip/apply", login, httpxtest.JSON(map[string]string{"profile_id": "default", "token": previewToken})).WantStatus(409)
	if len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 1 || f.Snapshot().Tables["/ip/dhcp-server/lease"][0]["dynamic"] != "true" {
		t.Fatal("blocked write changed lease")
	}
}

func TestR17UDPProtocolSideEntryHTTP(t *testing.T) {
	s, _ := fixture(t)
	login := s.Login()
	p, err := oxidns.NewTypedProfile(nil)
	if err != nil {
		t.Fatal(err)
	}
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
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, current *model.Profile) error {
		return current.OxiDNS.SetNode(draft)
	})
	if err != nil {
		t.Fatal(err)
	}
	get := s.Get("/api/client-devices", login)
	get.WantStatus(200)
	gotBody := decodeBody(t, get)
	sideEntries, _ := gotBody["side_entries"].([]any)
	if gotBody["editable"] != true || len(sideEntries) != 1 {
		t.Fatal("udp scheme misclassified as plugin reference")
	}
	s.Put("/api/client-devices", login, httpxtest.JSON(map[string]string{"profile_id": "default", "ip": "192.0.2.21", "name": "Synthetic", "mode": "direct", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""})).WantStatus(200)
	stored, _ := s.Repo.Profile(s.Context(), "default")
	oxiRaw, _ := json.Marshal(stored.OxiDNS)
	oxiVal, _ := testjson.Parse(oxiRaw)
	nativeCfg, _ := testjson.Path(oxiVal, "native_config")
	if nc, _ := nativeCfg.(string); !strings.Contains(nc, "side_forward") {
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
	devices, _ := decodeBody(t, result)["devices"].([]any)
	if len(devices) != 1 {
		t.Fatalf("healthy device hidden: %s", result.Body)
	}
	row, _ := devices[0].(map[string]any)
	if row["mac"] != "02:00:00:00:00:21" || row["fixed_ip"] == nil {
		t.Fatalf("healthy evidence dropped: %s", result.Body)
	}
}
