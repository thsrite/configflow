package routers

import (
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

func TestR12A01ReturnAgentRawWireAndRedaction(t *testing.T) {
	var seen []r09Wire
	response := `{"success":true,"managed":null,"safety":{"version":1,"ok":true,"blockers":[]},"future":{"generation":9007199254740993}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		seen = append(seen, r09Wire{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body), ContentType: r.Header.Get("Content-Type"), Authorization: r.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()
	u, e := url.Parse(server.URL)
	if e != nil {
		t.Fatal(e)
	}
	host, port, e := net.SplitHostPort(u.Host)
	if e != nil {
		t.Fatal(e)
	}
	n, e := strconv.Atoi(port)
	if e != nil {
		t.Fatal(e)
	}
	raw := device.JSON(map[string]any{"id": "gateway", "name": "synthetic-api", "host": host, "port": n, "token": "synthetic-token", "service_type": "mihomo", "profile_id": "default", "status": "online"})
	var record model.Agent
	var view model.Agent
	if e = json.Unmarshal(raw, &record); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &view); e != nil {
		t.Fatal(e)
	}
	db := r12Repository(t, r10Router(), "{}")
	manager, e := agents.New(t.Context(), db)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	client := NewReturnAgent(manager, &GatewayAgent{Agent: &view, Record: &record})
	status, e := client.Status(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(device.JSON(status)), "9007199254740993") {
		t.Fatal("unknown field/large integer lost")
	}
	spec := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"priority":104,"mark":"0x400000","safety_version":2}`)
	if e = client.Apply(t.Context(), spec); e != nil {
		t.Fatal(e)
	}
	if e = client.Remove(t.Context(), false); e != nil {
		t.Fatal(e)
	}
	want := []r09Wire{{Method: "GET", Path: "/api/return-routing", Authorization: "Bearer synthetic-token"}, {Method: "PUT", Path: "/api/return-routing", Body: string(spec), ContentType: "application/json", Authorization: "Bearer synthetic-token"}, {Method: "DELETE", Path: "/api/return-routing", Authorization: "Bearer synthetic-token"}}
	r12Compare(t, "raw A01 requests", want, seen)
	response = `{"success":false,"message":"api diagnostic synthetic-token token=synthetic-token"}`
	_, e = client.Status(t.Context())
	if e == nil || strings.Contains(e.Error(), "synthetic-token") || !strings.Contains(e.Error(), "api diagnostic") {
		t.Fatalf("unsafe Agent error %v", e)
	}
	if e = client.Apply(context.Background(), spec); e == nil || strings.Contains(e.Error(), "synthetic-token") {
		t.Fatalf("unsafe Agent apply %v", e)
	}
}
