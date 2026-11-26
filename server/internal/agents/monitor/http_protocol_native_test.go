package monitor

import (
	"configflow/internal/model"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"configflow/internal/agents"
)

func TestNativeMonitorHTTPWireBytes(t *testing.T) {
	for _, tc := range []struct {
		name, method, uri, contentType, body, reply string
		status                                      int
	}{
		{"cache-list", "GET", "/api/oxidns/cache", "", "", `{"success":true,"caches":[]}`, 200},
		{"cache-flush", "POST", "/api/oxidns/cache/flush", "application/json", `{"tags":["cache"]}`, `{"success":true,"flushed":["cache"]}`, 200},
		{"rule-get", "GET", "/api/oxidns/rules/update", "", "", `{"success":true,"task":{"id":"rule-1","status":"running","phase":"downloading","downloaded":0,"download_total":1,"reloaded":0,"reload_total":1}}`, 202},
		{"rule-paths", "POST", "/api/oxidns/rules/update", "application/json", `{"paths":["rules/中.json"]}`, `{"success":true,"task":{"id":"rule-1","status":"running","phase":"downloading","downloaded":0,"download_total":1,"reloaded":0,"reload_total":1}}`, 202},
		{"panel-delay", "GET", "/api/mihomo/proxies/A%2FB/delay?url=https%3A%2F%2Fexample.test%2Fprobe&timeout=5000", "", "", `{"delay":5}`, 200},
		{"panel-selector", "PUT", "/api/mihomo/proxies/A%2FB", "application/json", `{"name":"中"}`, `{}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != tc.method || r.RequestURI != tc.uri || string(body) != tc.body || r.Header.Get("Authorization") != "Bearer synthetic-token" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != tc.contentType {
					t.Errorf("wire: method=%q uri=%q auth=%q accept=%q type=%q body=%q err=%v", r.Method, r.RequestURI, r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("Content-Type"), body, err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.reply))
				received <- struct{}{}
			}))
			defer server.Close()
			m, _ := nativeFixture(t, tc.reply)
			host, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			if err := model.SetStoredMember(&m.agent, "host", []byte(strconv.Quote(host))); err != nil {
				t.Fatal(err)
			}
			if err := model.SetStoredMember(&m.agent, "port", []byte(port)); err != nil {
				t.Fatal(err)
			}
			m.client = agents.HTTPClient{}
			switch tc.name {
			case "cache-list":
				_, err = CallCache(context.Background(), m, "dns", "GET", nil)
			case "cache-flush":
				_, err = CallCache(context.Background(), m, "dns", "POST", []string{"cache"})
			case "rule-get":
				_, _, err = CallRuleUpdate(context.Background(), m, "dns", "GET", nil)
			case "rule-paths":
				_, _, err = CallRuleUpdate(context.Background(), m, "dns", "POST", []string{"rules/中.json"})
			default:
				if e := model.SetStoredMember(&m.agent, "service_type", []byte(`"mihomo"`)); e != nil {
					t.Fatal(e)
				}
				if tc.name == "panel-delay" {
					_, err = CallPanel(context.Background(), m, "dns", "proxy/delay", "GET", [][2]string{{"name", "A/B"}, {"url", "https://example.test/probe"}}, nil, "")
				} else {
					_, err = CallPanel(context.Background(), m, "dns", "proxy", "PUT", [][2]string{{"name", "A/B"}}, []byte(`{"name":"中"}`), "")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			<-received
		})
	}
}
