package clients

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

type boundaryRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}
type boundaryCase struct {
	Config   json.RawMessage   `json:"config"`
	HandEdit bool              `json:"hand_edit,omitempty"`
	Policies []Device          `json:"policies,omitempty"`
	Requests []boundaryRequest `json:"requests"`
}
type boundaryResponse struct {
	Status       int             `json:"status"`
	Payload      json.RawMessage `json:"payload"`
	CacheControl string          `json:"cache_control"`
}
type boundaryOutput struct {
	Initial   json.RawMessage    `json:"initial"`
	Responses []boundaryResponse `json:"responses"`
	After     json.RawMessage    `json:"after"`
	Stored    []json.RawMessage  `json:"stored"`
}

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func pythonBoundary(t *testing.T, dir string, tc boundaryCase) boundaryOutput {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r16_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R16_DATA="+dir)
	cmd.Stdin = bytes.NewReader(raw(tc))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python: %v %s", err, stderr.Bytes())
	}
	var out boundaryOutput
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Python: %v %s", err, stderr.Bytes())
	}
	return out
}
func equalJSON(t *testing.T, label string, a, b json.RawMessage) {
	t.Helper()
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		t.Fatalf("invalid %s: %s / %s", label, a, b)
	}
	normalizeJSON(av)
	normalizeJSON(bv)
	if !reflect.DeepEqual(av, bv) {
		t.Fatalf("%s differs at %s", label, firstDifference(av, bv, "$"))
	}
}
func normalizeJSON(v any) {
	switch v := v.(type) {
	case map[string]any:
		for k, item := range v {
			if k == "checked_at" || k == "updated_at" {
				delete(v, k)
				continue
			}
			if k == "native_config" || k == "template_base_config" {
				if text, ok := item.(string); ok && text != "" {
					var doc any
					if node, err := yamlx.Parse([]byte(text)); err == nil && yamlx.Decode(node, &doc) == nil {
						v[k] = doc
					}
				}
			}
			normalizeJSON(v[k])
		}
	case []any:
		for _, item := range v {
			normalizeJSON(item)
		}
	}
}
func configJSON(t *testing.T, r *repo.Repository) json.RawMessage {
	t.Helper()
	doc, err := r.Config(context.Background(), "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	return raw(doc)
}

func applyProfileJSON(current *model.Profile, updated json.RawMessage) error {
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(raw(current), &before); err != nil {
		return err
	}
	if err := json.Unmarshal(updated, &after); err != nil {
		return err
	}
	for k, v := range after {
		if !bytes.Equal(before[k], v) {
			if err := current.SetJSON(k, v); err != nil {
				return err
			}
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			if err := current.Clear(k); err != nil {
				return err
			}
		}
	}
	return nil
}

// Only translates HTTP to R16 package calls; production profile/auth/shape
// routing is R17. The reference side always executes the real Flask handlers.
func testHTTP(t *testing.T, r *repo.Repository, discovery *DiscoveryService) http.Handler {
	t.Helper()
	cache := NewDiscoveryCache(nil, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		var result any
		var err error
		status := 200
		if strings.HasPrefix(req.URL.Path, "/api/client-devices/discovery") {
			var d Discovery
			d, err = discovery.Discover(req.Context(), configJSON(t, r), DiscoveryOptions{ProfileID: "default", Refresh: req.URL.Query().Get("refresh") == "1", Cache: cache})
			m := map[string]any{}
			_ = json.Unmarshal(raw(d), &m)
			m["success"] = true
			m["profile_id"] = "default"
			result = m
		} else {
			if req.Method == "PUT" {
				var c Change
				err = json.NewDecoder(req.Body).Decode(&c)
				if err == nil {
					_, err = r.UpdateProfile(req.Context(), "default", func(ctx context.Context, current *model.Profile) error {
						updated, e := UpdateDevice(ctx, raw(current), c, "")
						if e != nil {
							return e
						}
						return applyProfileJSON(current, updated)
					})
				}
			}
			if err == nil {
				result, err = ListDevices(configJSON(t, r))
			}
		}
		if err != nil {
			status = 400
			if errors.Is(err, ErrDeviceChanged) {
				status = 409
			}
			result = map[string]any{"success": false, "message": err.Error()}
		}
		w.WriteHeader(status)
		if req.Method != "HEAD" {
			_ = json.NewEncoder(w).Encode(result)
		}
	})
}
func body(c Change) json.RawMessage {
	m := map[string]any{}
	_ = json.Unmarshal(raw(c), &m)
	m["profile_id"] = "default"
	m["base_url"] = ""
	return raw(m)
}
func TestFlaskDeviceWritesAndStoredState(t *testing.T) {
	requests := []boundaryRequest{{"GET", "/api/client-devices", nil},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.5", "客厅电视", "direct", "电视", "direct"})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.9", "手机", "smart", "手机", ""})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.5", "", "", "客厅电视", "direct"})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.9", "新名", "direct", "旧名", "smart"})},
		{"PUT", "/api/client-devices", body(Change{"hostname.example.com", "x", "", "", ""})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.10", "a\nb", "", "", ""})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.10", "x", "proxy", "", ""})},
		{"PUT", "/api/client-devices", body(Change{"192.0.2.0/24", "网段", "direct", "", ""})},
		{"HEAD", "/api/client-devices", nil},
	}
	config := json.RawMessage(`{"device_notes":{"192.0.2.5":"电视","192.0.2.9":"手机"},"oxidns":{"worker_threads":3},"client_fixed_ips":{},"future_client_field":{"keep":[1,2]}}`)
	runBoundary(t, boundaryCase{Config: config, Policies: []Device{{IP: "192.0.2.5", Name: "电视", Mode: "direct"}}, Requests: requests})
}
func runBoundary(t *testing.T, tc boundaryCase) {
	t.Helper()
	dir := t.TempDir()
	py := pythonBoundary(t, dir, tc)
	r, err := repo.Open(filepath.Join(dir, "go"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	equalJSON(t, "initial", py.Initial, configJSON(t, r))
	handler := testHTTP(t, r, NewDiscoveryService())
	for i, request := range tc.Requests {
		t.Run(request.Method+request.Path+string(rune('A'+i)), func(t *testing.T) {
			req := httptest.NewRequest(request.Method, request.Path, bytes.NewReader(request.Body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			expected := py.Responses[i]
			if w.Code != expected.Status {
				t.Fatalf("status: %d / %d: %s", expected.Status, w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != expected.CacheControl {
				t.Fatal("cache control differs")
			}
			if request.Method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
			} else {
				equalJSON(t, "response", expected.Payload, w.Body.Bytes())
				if strings.HasPrefix(request.Path, "/api/client-devices/discovery") && (strings.Contains(t.Name(), "/routeros/") || strings.Contains(t.Name(), "/ikuai/")) {
					var payload Discovery
					_ = json.Unmarshal(w.Body.Bytes(), &payload)
					if payload.State != "ok" || len(payload.Discovered) == 0 {
						t.Fatalf("normal driver never succeeded: %s", w.Body.String())
					}
				}
			}
			equalJSON(t, "stored after request", py.Stored[i], configJSON(t, r))
		})
	}
	equalJSON(t, "stored profile", py.After, configJSON(t, r))
	// Inspect the actual SQLite payloads using each repository's store (not only
	// the handler result), and compare all user files after parsing JSON/YAML.
	compareFiles(t, filepath.Join(dir, "python"), filepath.Join(dir, "go"))
	compareDatabase(t, filepath.Join(dir, "python", "configflow.sqlite3"), filepath.Join(dir, "go", "configflow.sqlite3"))
}
func compareFiles(t *testing.T, a, b string) {
	t.Helper()
	read := func(root string) map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if strings.HasSuffix(rel, ".lock") || strings.Contains(rel, ".sqlite3") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = string(content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	if !reflect.DeepEqual(read(a), read(b)) {
		t.Fatalf("user files differ: %v / %v", read(a), read(b))
	}
}

type discoveryWire struct {
	mu     sync.Mutex
	tables map[string]json.RawMessage
	faults map[string]int
	ping   string
	calls  []string
	badGET bool
}

func (w *discoveryWire) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	w.calls = append(w.calls, r.Method+" "+r.URL.Path)
	rw.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" {
		if len(b) != 0 {
			w.badGET = true
		}
		path := strings.TrimPrefix(r.URL.Path, "/rest")
		if status := w.faults[path]; status != 0 {
			rw.WriteHeader(status)
			_, _ = rw.Write([]byte(`{"detail":"synthetic failure"}`))
			return
		}
		if rows, ok := w.tables[path]; ok {
			_, _ = rw.Write(rows)
		} else {
			_, _ = rw.Write([]byte(`[]`))
		}
		return
	}
	if r.URL.Path == "/rest/ping" {
		switch w.ping {
		case "denied":
			rw.WriteHeader(403)
			_, _ = rw.Write([]byte(`{"detail":"not enough permissions"}`))
		case "error":
			rw.WriteHeader(500)
			_, _ = rw.Write([]byte(`{"detail":"synthetic failure"}`))
		case "silent":
			_, _ = rw.Write([]byte(`[{"received":"0"}]`))
		default:
			_, _ = rw.Write([]byte(`[{"received":"1"}]`))
		}
		return
	}
	if r.URL.Path == "/Action/login" {
		_, _ = rw.Write([]byte(`{"Result":10000,"ErrMsg":"Success"}`))
		return
	}
	var call struct {
		Func  string `json:"func_name"`
		Param struct {
			Limit string `json:"limit"`
		} `json:"param"`
	}
	_ = json.Unmarshal(b, &call)
	if status := w.faults[call.Func]; status != 0 {
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte(`{"detail":"synthetic failure"}`))
		return
	}
	rows := w.tables[call.Func]
	if rows == nil {
		rows = json.RawMessage(`[]`)
	}
	_, _ = rw.Write(raw(map[string]any{"code": 0, "results": map[string]any{"data": rows, "total": 100}}))
}
func rosWire() *discoveryWire {
	return &discoveryWire{tables: map[string]json.RawMessage{
		"/ip/address":           json.RawMessage(`[{"address":"192.0.2.1/24","interface":"lan"}]`),
		"/ip/dhcp-server":       json.RawMessage(`[{"name":"dhcp","interface":"lan","lease-time":"30m"}]`),
		"/ip/dhcp-server/lease": json.RawMessage(`[{"address":"192.0.2.10","mac-address":"aa:bb:cc:00:00:10","host-name":"租约设备","status":"bound","last-seen":"2m","dynamic":"true","server":"dhcp"}]`),
		"/ip/arp":               json.RawMessage(`[{"address":"192.0.2.10","mac-address":"aa:bb:cc:00:00:10","interface":"lan","status":"stale"},{"address":"192.0.2.11","mac-address":"aa:bb:cc:00:00:11","interface":"lan","status":"stale"},{"address":"192.0.2.12","interface":"lan","status":"failed"},{"address":"198.51.100.9","mac-address":"aa:bb:cc:00:00:09","interface":"wan","status":"reachable"}]`),
	}, faults: map[string]int{}}
}
func TestFlaskDiscoveryRealDrivers(t *testing.T) {
	tests := []struct {
		name, vendor string
		modify       func(*discoveryWire)
	}{
		{"routeros", "routeros", func(w *discoveryWire) {}},
		{"probe_denied", "routeros", func(w *discoveryWire) { w.ping = "denied" }},
		{"probe_silent", "routeros", func(w *discoveryWire) { w.ping = "silent" }},
		{"probe_errors", "routeros", func(w *discoveryWire) { w.ping = "error" }},
		{"lease_degraded", "routeros", func(w *discoveryWire) { w.faults["/ip/dhcp-server/lease"] = 403 }},
		{"arp_required", "routeros", func(w *discoveryWire) { w.faults["/ip/arp"] = 403 }},
		{"private_fallback", "routeros", func(w *discoveryWire) {
			w.tables["/ip/dhcp-server"] = json.RawMessage(`[]`)
			w.tables["/ip/dhcp-server/lease"] = json.RawMessage(`[]`)
			w.tables["/ip/address"] = json.RawMessage(`[{"address":"10.23.0.1/24","interface":"lan"},{"address":"10.24.0.1/24","interface":"wan"}]`)
			w.tables["/interface/list/member"] = json.RawMessage(`[{"list":"WAN","interface":"wan"}]`)
		}},
		{"no_lan", "routeros", func(w *discoveryWire) {
			w.tables["/ip/dhcp-server"] = json.RawMessage(`[]`)
			w.tables["/ip/address"] = json.RawMessage(`[]`)
		}},
		{"ikuai", "ikuai", func(w *discoveryWire) {}},
		{"ikuai_optional", "ikuai", func(w *discoveryWire) { w.faults["dhcp_lease"] = 403; w.faults["lan"] = 403 }},
		{"ikuai_required", "ikuai", func(w *discoveryWire) { w.faults["monitor_lanip"] = 403 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := rosWire()
			if tc.vendor == "ikuai" {
				w.tables = map[string]json.RawMessage{"monitor_lanip": json.RawMessage(`[{"ip_addr":"192.0.2.10","mac":"a:b:c:d:e:f","comment":"%E7%94%B5%E8%A7%86"}]`), "dhcp_lease": json.RawMessage(`[{"ip_addr":"192.0.2.11","mac":"aa:bb:cc:00:00:11","hostname":"Phone","start_time":100,"end_time":1900,"timeout":1500}]`), "lan": json.RawMessage(`[{"ip_mask":"192.0.2.1/255.255.255.0"}]`)}
			}
			tc.modify(w)
			server := httptest.NewServer(w)
			defer server.Close()
			cfg := raw(map[string]any{"routers": map[string]any{"items": []any{map[string]any{"id": "r1", "name": "路由器", "type": tc.vendor, "endpoint": server.URL, "username": "synthetic-user", "password": "synthetic-password", "ros_policy_rule_sets": []string{"test-rule"}}}}, "device_notes": map[string]string{"192.0.2.1": "主路由", "192.0.2.10": "设备", "192.0.2.99": "未出现", "198.51.100.99": "外部", "2001:db8::1": "IPv6"}})
			runBoundary(t, boundaryCase{Config: cfg, Policies: []Device{{IP: "192.0.2.0/24", Name: "网段", Mode: "direct"}, {IP: "192.0.2.0/28", Name: "子网", Mode: "smart"}}, Requests: []boundaryRequest{{"GET", "/api/client-devices/discovery", nil}, {"GET", "/api/client-devices/discovery", nil}, {"GET", "/api/client-devices/discovery?refresh=1", nil}, {"HEAD", "/api/client-devices/discovery", nil}}})
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.badGET {
				t.Fatal("GET carried a request body")
			}
			for _, call := range w.calls {
				if strings.HasPrefix(call, "PUT") || strings.HasPrefix(call, "DELETE") || strings.HasPrefix(call, "PATCH") {
					t.Fatal("discovery mutated router")
				}
			}
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		runBoundary(t, boundaryCase{Config: json.RawMessage(`{"device_notes":{"192.0.2.1":"host","2001:db8::1":"v6"}}`), Requests: []boundaryRequest{{"GET", "/api/client-devices/discovery", nil}, {"HEAD", "/api/client-devices/discovery", nil}}})
	})
}

func TestNormalizationErrorType(t *testing.T) {
	_, err := NormalizeChange(Change{IP: "bad"})
	var settings *oxidns.SettingsError
	if !errors.As(err, &settings) {
		t.Fatalf("validation error loses type: %v", err)
	}
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if ok {
			for k, v := range av {
				if d := firstDifference(v, bv[k], path+"."+k); d != "" {
					return d
				}
			}
			for k := range bv {
				if _, ok := av[k]; !ok {
					return path + "." + k + " unexpected"
				}
			}
		}
	case []any:
		bv, ok := b.([]any)
		if ok {
			if len(av) != len(bv) {
				return path + " length"
			}
			for i := range av {
				if d := firstDifference(av[i], bv[i], path+"[]"); d != "" {
					return d
				}
			}
		}
	}
	return path + ": " + string(raw(a)) + " / " + string(raw(b))
}

func compareDatabase(t *testing.T, a, b string) {
	t.Helper()
	read := func(path string) map[string]json.RawMessage {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		rows, err := db.Query("SELECT scope,owner,field,value,revision FROM fields ORDER BY scope,owner,field")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := map[string]json.RawMessage{}
		for rows.Next() {
			var scope, owner, field, value string
			var revision int64
			if err = rows.Scan(&scope, &owner, &field, &value, &revision); err != nil {
				t.Fatal(err)
			}
			if field == "updated_at" {
				var stamp string
				if json.Unmarshal([]byte(value), &stamp) != nil {
					t.Fatal("timestamp type")
				}
				if _, err = time.Parse(time.RFC3339Nano, stamp); err != nil {
					t.Fatal(err)
				}
				value = `"<timestamp>"`
			}
			result[scope+"/"+owner+"/"+field] = raw(map[string]any{"value": json.RawMessage(value), "revision": revision})
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	equalJSON(t, "SQLite rows", raw(read(a)), raw(read(b)))
}

func TestFlaskPolicyRefusalStillAllowsRename(t *testing.T) {
	runBoundary(t, boundaryCase{Config: json.RawMessage(`{"device_notes":{"192.0.2.5":"电视"}}`), Policies: []Device{{IP: "192.0.2.5", Name: "电视", Mode: "direct"}}, HandEdit: true, Requests: []boundaryRequest{
		{"GET", "/api/client-devices", nil},
		{"PUT", "/api/client-devices", body(Change{IP: "192.0.2.5", Name: "new", Mode: "smart", PreviousName: "电视", PreviousMode: "direct"})},
		{"PUT", "/api/client-devices", body(Change{IP: "192.0.2.5", Name: "new", Mode: "direct", PreviousName: "电视", PreviousMode: "direct"})},
	}})
}
func TestFlaskPartialDiscovery(t *testing.T) {
	first, second := rosWire(), rosWire()
	second.faults["/ip/arp"] = 403
	a, b := httptest.NewServer(first), httptest.NewServer(second)
	defer a.Close()
	defer b.Close()
	config := raw(map[string]any{"device_notes": map[string]string{"192.0.2.99": "missing"}, "routers": map[string]any{"items": []any{map[string]string{"id": "r1", "type": "routeros", "endpoint": a.URL, "username": "synthetic-user"}, map[string]string{"id": "r2", "type": "routeros", "endpoint": b.URL, "username": "synthetic-user"}}}})
	runBoundary(t, boundaryCase{Config: config, Requests: []boundaryRequest{{"GET", "/api/client-devices/discovery", nil}}})
}
func TestDifferentialComparisonNegativeControl(t *testing.T) {
	// The comparator must keep policy, presence and unknown stored fields strict.
	for _, pair := range [][2]string{{`{"state":"ok"}`, `{"state":"failed"}`}, {`{"devices":[{"presence":"online"}]}`, `{"devices":[{"presence":"offline"}]}`}, {`{"future":1}`, `{"future":2}`}} {
		var a, b any
		_ = json.Unmarshal([]byte(pair[0]), &a)
		_ = json.Unmarshal([]byte(pair[1]), &b)
		normalizeJSON(a)
		normalizeJSON(b)
		if firstDifference(a, b, "$") == "" {
			t.Fatal("differential ignored a changed business field")
		}
	}
}
