package clients

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func raw(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func equalJSON(t *testing.T, label string, a, b json.RawMessage) {
	t.Helper()
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		t.Fatalf("invalid %s: %s / %s", label, a, b)
	}
	if label != "response" && label != "SQLite rows" {
		av, bv = testjson.WithoutEmptyMembers(av), testjson.WithoutEmptyMembers(bv)
	}
	if !reflect.DeepEqual(av, bv) {
		t.Fatalf("%s differs:\ngot  %s\nwant %s", label, a, b)
	}
}

func TestNormalizationErrorType(t *testing.T) {
	_, err := NormalizeChange(Change{IP: "bad"})
	var settings *oxidns.SettingsError
	if !errors.As(err, &settings) {
		t.Fatalf("validation error loses type: %v", err)
	}
}

func configJSON(t *testing.T, r *repo.Repository) json.RawMessage {
	t.Helper()
	doc, err := r.ConfigWithOxiDNSAgents(context.Background(), "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	return raw(doc)
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
