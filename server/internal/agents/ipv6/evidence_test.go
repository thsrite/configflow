package ipv6

import (
	"bufio"
	"bytes"
	"configflow/internal/agents"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/routers"
)

func TestTupleEvidenceAgainstPython(t *testing.T) {
	request := Document{"source_ip": raw("192.0.2.8"), "source_port": raw(34567), "address": raw("198.51.100.9"), "status": raw("ok"), "family": raw("ipv4")}
	match := Document{"source_ip": raw("192.0.2.8"), "source_port": raw(34567), "host": raw("site.example"), "network": raw("tcp"), "destination_port": raw(443), "proxy_name": raw("node"), "proxy_type": raw("ss"), "chains": raw([]string{"node"}), "ipv6_transport_verified": raw(true)}
	fixtures := []Document{}
	for _, change := range []Document{nil, {"source_port": raw(1)}, {"host": raw("other.example")}, {"chains": raw([]string{"DIRECT"})}, {"ipv6_transport_verified": raw(false)}} {
		m := object(raw(match))
		for k, v := range change {
			m[k] = v
		}
		report := Document{"target_host": raw("site.example"), "requests": raw([]Document{request})}
		trace := Document{"target_host": raw("site.example"), "matches": raw([]Document{m})}
		fixtures = append(fixtures, Document{"report": raw(report), "trace": raw(trace), "url": raw("https://site.example/")})
	}
	fixtures = append(fixtures, Document{"report": raw(Document{"requests": raw([]any{request, nil})}), "trace": raw(Document{}), "url": raw("https://site.example/")})
	var input bytes.Buffer
	actual := []json.RawMessage{}
	for _, f := range fixtures {
		checks := evaluate(context.Background(), nil, nil, object(f["report"]), object(f["trace"]), text(f["url"]), nil)
		actual = append(actual, raw(checks))
		input.Write(raw(f))
		input.WriteByte('\n')
	}
	for i, b := range actual {
		var checks []Check
		_ = json.Unmarshal(b, &checks)
		if i == 0 && (checks[0].Status != "ok" || checks[1].Status != "ok") {
			t.Fatal("positive control lacks evidence")
		}
		if i > 0 && checks[1].Status == "ok" {
			t.Fatal("invalid tuple proved IPv6")
		}
	}
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		return
	}
	comparePython(t, "evidence_reference.py", input.Bytes(), actual)
}
func comparePython(t *testing.T, script string, input []byte, want []json.RawMessage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "testdata/"+script)
	root, _ := filepath.Abs("../../../..")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = bytes.NewReader(input)
	var errors bytes.Buffer
	cmd.Stderr = &errors
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e, errors.String())
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	i := 0
	for scanner.Scan() {
		if i >= len(want) {
			t.Fatal("extra row")
		}
		var a, b any
		_ = json.Unmarshal(want[i], &a)
		_ = json.Unmarshal(scanner.Bytes(), &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("row%d Go%s Python%s", i, want[i], scanner.Bytes())
		}
		i++
	}
	if i != len(want) {
		t.Fatal("missing reference rows", i)
	}
}
func TestConnectionTupleCountersAndLegacy(t *testing.T) {
	rows := []routers.Row{{"protocol": "tcp", "src-address": "192.0.2.8", "src-port": "34567", "dst-address": "198.51.100.9", "dst-port": "443", "orig-packets": "1"}, {"protocol": "tcp", "src-address": "[2001:db8::8]:34567", "dst-address": "[2001:db8::9]:443", "orig-fasttrack-packets": "2"}}
	for i, row := range rows {
		source, destination := "192.0.2.8", "198.51.100.9"
		if i == 1 {
			source, destination = "2001:db8::8", "2001:db8::9"
		}
		if !rowMatches(row, source, 34567, destination, 443) {
			t.Fatal("valid tuple rejected", row)
		}
		if rowMatches(row, source, 34567, "2001:db8::10", 443) {
			t.Fatal("unrelated flow accepted")
		}
		row["orig-packets"] = "0"
		row["orig-fasttrack-packets"] = "0"
		if rowMatches(row, source, 34567, destination, 443) {
			t.Fatal("zero packets proves path")
		}
	}
}
func TestObserveIncompleteBaselineNeverCreatesFreshEvidence(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "bad-baseline"}[incomplete], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/rest/") || r.URL.Query().Get("protocol") != "tcp" || r.URL.Query().Get("dst-port") != "443" || r.URL.Query().Get(".proplist") == "" {
					t.Error("unrestricted request", r.Method, r.URL.String())
				}
				n := calls.Add(1)
				if n <= 2 {
					if incomplete {
						_, _ = w.Write([]byte(`[false,{".id":"old"}]`))
					} else {
						_, _ = w.Write([]byte(`[{".id":"old"}]`))
					}
					return
				}
				_, _ = w.Write([]byte(`[{".id":"fresh","protocol":"tcp","src-address":"192.0.2.8","src-port":"34567","dst-address":"198.51.100.9","dst-port":"443","orig-packets":"1"}]`))
			}))
			defer server.Close()
			config := Document{"routers": raw(map[string]any{"items": []any{map[string]any{"id": "r1", "type": "routeros", "endpoint": server.URL, "username": "api", "password": "synthetic-router-pass", "proxy_gateway_agent_id": "source"}}})}
			ctx, cancel := context.WithCancel(context.Background())
			ready := make(chan struct{}, 1)
			done := make(chan observed, 1)
			go func() { done <- observe(ctx, config, Document{"id": raw("source")}, "https://site.example", ready) }()
			<-ready
			if !incomplete {
				deadline := time.Now().Add(3 * time.Second)
				for calls.Load() < 4 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			result := <-done
			if incomplete && len(result["r1"]) != 0 {
				t.Fatal("bad baseline proved freshness")
			}
			if !incomplete && len(result["r1"]) == 0 {
				t.Fatal("fresh connection discarded")
			}
		})
	}
}

func TestPacketCountersFailClosed(t *testing.T) {
	for _, value := range []any{nil, "bad", "-1", "18446744073709551616", true} {
		if hasPackets(routers.Row{"orig-packets": value, "orig-fasttrack-packets": "1"}) {
			t.Fatal("invalid counter proved packet", value)
		}
	}
	if !hasPackets(routers.Row{"orig-packets": "18446744073709551615", "orig-fasttrack-packets": "18446744073709551615"}) {
		t.Fatal("uint64 counters overflowed")
	}
}

func TestAgentTransportAgainstPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	cases := []struct {
		status     int
		body       string
		disconnect bool
	}{{200, `{"success":true,"interface":"eth0"}`, false}, {404, `{}`, false}, {403, `{}`, false}, {200, `{`, false}, {200, `[]`, false}, {422, `{"success":false,"message":"synthetic failure"}`, false}, {500, `{"success":false,"message":"synthetic failure"}`, false}, {200, strings.Repeat("x", 512*1024+1), false}, {200, "", true}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, e := strconv.Atoi(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer synthetic-case-"))
		if e != nil || index < 0 || index >= len(cases) {
			t.Error("unexpected transport request")
			w.WriteHeader(400)
			return
		}
		c := cases[index]
		if c.disconnect {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
			return
		}
		w.WriteHeader(c.status)
		_, _ = io.WriteString(w, c.body)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	p, _ := strconv.Atoi(u.Port())
	s, ctx := fixture(t)
	s.Manager.SetClient(agents.HTTPClient{})
	var input bytes.Buffer
	want := []json.RawMessage{}
	for i := range cases {
		a := Document{"host": raw(u.Hostname()), "port": raw(p), "token": raw("synthetic-case-" + strconv.Itoa(i))}
		answer, e := s.request(ctx, a, "", nil)
		if e != nil {
			var boundary *Error
			if !errors.As(e, &boundary) {
				t.Fatal(e)
			}
			want = append(want, raw(map[string]any{"code": boundary.Code, "status": boundary.Status, "message": boundary.Message}))
		} else {
			want = append(want, raw(map[string]any{"result": answer}))
		}
		input.Write(raw(a))
		input.WriteByte('\n')
	}
	comparePython(t, "request_reference.py", input.Bytes(), want)
}
