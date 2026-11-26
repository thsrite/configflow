package clientlist

import (
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/routerdoc"
)

func TestClientLeaseEvidence(t *testing.T) {
	tests := []struct {
		name, arp, status, age, period, presence, basis string
		dynamic                                         bool
		probe                                           bool
	}{
		{"reachable", "reachable", "bound", "50m", "30m", "online", "arp", true, false},
		{"fresh", "failed", "bound", "20m", "30m", "online", "lease", true, false},
		{"renewal_missed", "stale", "bound", "21m", "30m", "offline", "lease", true, false},
		{"long_lease", "stale", "bound", "25m", "2h", "recent", "stale", true, true},
		{"static_waiting", "stale", "waiting", "1d", "30m", "recent", "stale", false, true},
		{"failed_arp", "failed", "waiting", "never", "30m", "offline", "arp", false, false},
		{"lease_only", "", "bound", "never", "30m", "recent", "stale", false, true},
		{"waiting_only", "", "waiting", "never", "30m", "offline", "lease", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			arp := []ros.Row{}
			if tc.arp != "" {
				arp = append(arp, ros.Row{"address": "192.0.2.10", "interface": "lan", "status": tc.arp, "mac-address": "a:b:c:d:e:f"})
			}
			leases := []ros.Row{{"address": "192.0.2.10", "status": tc.status, "last-seen": tc.age, "lease-time": tc.period, "dynamic": strconv.FormatBool(tc.dynamic), "server": "dhcp", "host-name": "host"}}
			s, c := classifyClients(arp, leases, []ros.Row{{"name": "dhcp", "interface": "lan"}}, map[string]bool{"lan": true})
			v := s["192.0.2.10"]
			if v.Presence != tc.presence || v.Basis != tc.basis || (len(c) > 0) != tc.probe {
				t.Fatalf("%+v %v", v, c)
			}
		})
	}
}
func TestClientDuration(t *testing.T) {
	for _, tc := range []struct {
		text    string
		seconds int64
		valid   bool
	}{{"2d1h36m57s", 178617, true}, {"00:30:00", 1800, true}, {"1w2d3ms", 777600, true}, {"0s", 0, true}, {"never", 0, false}, {"bad", 0, false}} {
		t.Run(tc.text, func(t *testing.T) {
			v := ClientDuration(tc.text)
			if (v != nil) != tc.valid || v != nil && *v != tc.seconds {
				t.Fatalf("%s: %v", tc.text, v)
			}
		})
	}
}
func TestClientProbeCapPriorityAndRawWire(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	var rawPing string
	var header string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/ping" {
			var body struct {
				Address string `json:"address"`
			}
			_ = json.Unmarshal(data, &body)
			seen = append(seen, body.Address)
			if body.Address == "192.0.2.99" {
				rawPing = string(data)
				header = r.Header.Get("Content-Type")
			}
			_, _ = w.Write([]byte(`[{"received":"1"}]`))
		} else {
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	d := rosDevice{ros.NewClient(server.URL, "synthetic-user", "synthetic-password", false)}
	defer d.Close()
	r := routerdoc.NewReading()
	candidates := []clientProbe{}
	for i := 1; i <= 40; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i)
		r.Sightings[ip] = routerdoc.NewSighting(ip)
		candidates = append(candidates, clientProbe{ip, "lan"})
	}
	r.Sightings["192.0.2.99"] = routerdoc.NewSighting("192.0.2.99")
	candidates = append(candidates, clientProbe{"192.0.2.99", "lan"})
	notes := d.confirmClients(context.Background(), r, candidates, map[string]bool{"lan": true}, ReadOptions{Priority: map[string]bool{"192.0.2.99": true}, Deadline: time.Now().Add(15 * time.Second)})
	if len(seen) != 32 || !slices.Contains(seen, "192.0.2.99") || slices.Contains(seen, "192.0.2.32") || !reflect.DeepEqual(notes, []string{fmt.Sprintf(messageClientsCap, 32, 9)}) {
		t.Fatalf("cap/priority: %v %v", seen, notes)
	}
	// Assert literal bytes and headers, independently of the fake's JSON decoder.
	if !strings.Contains(rawPing, `"address": "192.0.2.99"`) && !strings.Contains(rawPing, `"address":"192.0.2.99"`) {
		t.Fatalf("raw ping: %q", rawPing)
	}
	for _, field := range []string{`"arp-ping"`, `"count"`, `"interval"`, `"interface"`} {
		if !strings.Contains(rawPing, field) {
			t.Fatalf("missing wire field: %s", rawPing)
		}
	}
	if !strings.HasPrefix(header, "application/json") {
		t.Fatalf("content type: %q", header)
	}
}
func TestClientDeadlineDoesNotSend(t *testing.T) {
	d := NewROS(ros.NewClient("http://127.0.0.1:1", "", "", false))
	defer d.Close()
	_, err := d.ReadClients(context.Background(), ReadOptions{Deadline: time.Now().Add(-time.Second)})
	if err == nil || err.Error() != messageClientsSlow {
		t.Fatalf("deadline: %v", err)
	}
}
func TestClientIkuaiPagingAndWire(t *testing.T) {
	var pages []string
	var callBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/Action/login" {
			_, _ = w.Write([]byte(`{"Result":10000}`))
			return
		}
		callBodies = append(callBodies, string(data))
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Error("call content type")
		}
		var c struct {
			Param struct {
				Limit string `json:"limit"`
				Type  string `json:"TYPE"`
			} `json:"param"`
		}
		_ = json.Unmarshal(data, &c)
		pages = append(pages, c.Param.Limit)
		if c.Param.Type != "data,total" {
			t.Error("TYPE")
		}
		rows := []ros.Row{}
		count := 1000
		if len(pages) == 2 {
			count = 1
		}
		for range count {
			rows = append(rows, ros.Row{"ip_addr": "192.0.2.10"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "results": map[string]any{"data": rows, "total": 1001}})
	}))
	defer server.Close()
	d := ikuaiDevice{ikuai.NewClient(server.URL, "synthetic-user", "synthetic-password", false)}
	defer d.Close()
	rows, err := d.clientRows(context.Background(), "monitor_lanip")
	if err != nil || len(rows) != 1001 || !reflect.DeepEqual(pages, []string{"0,1000", "1000,1000"}) {
		t.Fatalf("paging: %d %v %v", len(rows), pages, err)
	}
	if len(callBodies) != 2 || !strings.Contains(callBodies[0], `"action"`) {
		t.Fatal("raw call body missing action")
	}
}
func FuzzClientDuration(f *testing.F) {
	for _, s := range []string{"never", "0s", "1w2d3h4m5s", "2ms", "00:01:30", "9223372036854775807w"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		v := ClientDuration(text)
		if v != nil && *v < 0 {
			t.Fatalf("negative duration for %q", text)
		}
	})
}

func TestClientICMPFallbackAndReread(t *testing.T) {
	for _, tc := range []struct{ status, presence, basis string }{{"reachable", "online", "probe"}, {"failed", "offline", "probe"}, {"stale", "recent", "stale"}} {
		t.Run(tc.status, func(t *testing.T) {
			var pings int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					_ = json.NewEncoder(w).Encode([]ros.Row{{"address": "192.0.2.10", "interface": "lan", "status": tc.status}})
					return
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				pings++
				if body["arp-ping"] == "yes" {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"detail":"interface does not support arp ping"}`))
					return
				}
				if _, exists := body["interface"]; exists {
					t.Error("ICMP fallback kept ARP interface")
				}
				_, _ = w.Write([]byte(`[{"received":"0"}]`))
			}))
			defer server.Close()
			d := rosDevice{ros.NewClient(server.URL, "synthetic-user", "synthetic-password", false)}
			defer d.Close()
			r := routerdoc.NewReading()
			s := routerdoc.NewSighting("192.0.2.10")
			s.Presence = "recent"
			s.Basis = "stale"
			r.Sightings[s.IP] = s
			notes := d.confirmClients(context.Background(), r, []clientProbe{{s.IP, "lan"}}, map[string]bool{"lan": true}, ReadOptions{Deadline: time.Now().Add(15 * time.Second)})
			if pings != 2 || s.Presence != tc.presence || s.Basis != tc.basis || len(notes) != 0 {
				t.Fatalf("fallback/re-read: %d %+v %v", pings, s, notes)
			}
		})
	}
}
