package rulefetch

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

type rawStep struct {
	Raw  string `json:"raw"`
	Then string `json:"then"`
}

type fetchScenario struct {
	Name         string           `json:"name"`
	URL          string           `json:"url"`
	Proxy        *string          `json:"proxy"`
	ServerDomain *string          `json:"server_domain"`
	Opts         map[string]any   `json:"opts"`
	Script       []rawStep        `json:"script"`
	Result       map[string]any   `json:"result"`
	Seen         []map[string]any `json:"seen"`
}

var rawHeaders = map[string]bool{"host": true, "proxy-authorization": true, "accept-encoding": true, "accept": true,
	"user-agent": true, "authorization": true}

// rawServer is the scripted server of testdata/gen_fetch_c14.py.
type rawServer struct {
	ln    net.Listener
	port  string
	mu    sync.Mutex
	seen  []map[string]any
	stop  chan struct{}
	wg    sync.WaitGroup
	steps []rawStep
}

func newRawServer(t *testing.T, steps []rawStep) *rawServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &rawServer{ln: ln, port: strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), stop: make(chan struct{}), steps: steps}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for index := 0; ; index++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			step := rawStep{Raw: "HTTP/1.1 500 Unscripted\r\nContent-Length: 0\r\n\r\n", Then: "close"}
			if index < len(steps) {
				step = steps[index]
			}
			s.wg.Add(1)
			go s.serve(conn.(*net.TCPConn), step)
		}
	}()
	return s
}

func (s *rawServer) serve(conn *net.TCPConn, step rawStep) {
	defer s.wg.Done()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var data []byte
	buffer := make([]byte, 4096)
	for !strings.Contains(string(data), "\r\n\r\n") && len(data) < 65536 {
		n, err := conn.Read(buffer)
		data = append(data, buffer[:n]...)
		if err != nil {
			break
		}
	}
	head, _, _ := strings.Cut(string(data), "\r\n\r\n")
	lines := strings.Split(latin1Text(head), "\r\n")
	entry := map[string]any{"line": strings.ReplaceAll(lines[0], s.port, "{PORT}")}
	for _, line := range lines[1:] {
		name, value, _ := strings.Cut(line, ":")
		if name = strings.ToLower(strings.TrimSpace(name)); rawHeaders[name] {
			entry[name] = strings.ReplaceAll(strings.TrimSpace(value), s.port, "{PORT}")
		}
	}
	s.mu.Lock()
	s.seen = append(s.seen, entry)
	s.mu.Unlock()
	var payload []byte
	if strings.HasPrefix(step.Raw, "b64:") {
		payload, _ = base64.StdEncoding.DecodeString(step.Raw[4:])
	} else {
		payload = latin1Bytes(strings.ReplaceAll(step.Raw, "{PORT}", s.port))
	}
	_, _ = conn.Write(payload)
	switch step.Then {
	case "reset":
		_ = conn.SetLinger(0)
	case "hang":
		select {
		case <-s.stop:
		case <-time.After(3 * time.Second):
		}
	}
	conn.Close()
}

func (s *rawServer) close() {
	close(s.stop)
	s.ln.Close()
	s.wg.Wait()
}

// fetchDetailClasses are compared by class only because their non-Chinese
// error detail is library-specific or outside the user-facing contract.
var fetchDetailClasses = map[string]bool{"ConnectionError": true, "ChunkedEncodingError": true, "ContentDecodingError": true,
	"ProxyError": true, "SSLError": true, "ConnectTimeout": true, "ReadTimeout": true, "ValueError": true}

func decodeUnreservedEscapes(s string) string {
	// Dot remains encoded so comparison does not collapse path segments before
	// URL resolution.
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			value, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err == nil {
				b := byte(value)
				if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '~' {
					out.WriteByte(b)
					i += 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func normalizeRequestTarget(line string) string {
	method, rest, ok := strings.Cut(line, " ")
	if !ok {
		return line
	}
	target, version, ok := strings.Cut(rest, " ")
	if !ok {
		return line
	}
	return method + " " + decodeUnreservedEscapes(target) + " " + version
}

// TestFetchDifferentialAgainstRequests replays testdata/fetch_c14.json
// (testdata/gen_fetch_c14.py: real requests 2.31 against the same scripted
// loopback server): result and every request head the server saw.
func TestFetchDifferentialAgainstRequests(t *testing.T) {
	data, err := os.ReadFile("testdata/fetch_c14.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []fetchScenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	client := &Client{Transport: HTTPTransport{}, Local: DefaultClient.Local, Sleep: func(time.Duration) {}}
	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			if reason := n2Accepted[strings.ReplaceAll(sc.Name, " ", "_")]; reason != "" {
				t.Skip("accepted N2 difference: " + reason)
			}
			server := newRawServer(t, sc.Script)
			url := sc.URL
			if url == "" {
				url = "http://127.0.0.1:{PORT}/rules.list"
			}
			url = strings.ReplaceAll(url, "{PORT}", server.port)
			system := jsonx.NewObject()
			if sc.Proxy != nil {
				system.Set("rule_fetch_proxy", strings.ReplaceAll(*sc.Proxy, "{PORT}", server.port))
			}
			if sc.ServerDomain != nil {
				system.Set("server_domain", strings.ReplaceAll(*sc.ServerDomain, "{PORT}", server.port))
			}
			req := Request{Config: jsonx.Obj("system_config", system), ConnectTimeout: time.Second, ReadTimeout: time.Second}
			if m, ok := sc.Opts["method"].(string); ok {
				req.Method = m
			}
			if s, ok := sc.Opts["stream"].(bool); ok {
				req.Stream = s
			}
			if n, ok := sc.Opts["max_bytes"].(float64); ok {
				req.MaxBytes = int64(n)
			}
			if n, ok := sc.Opts["max_redirects"].(float64); ok {
				m := int(n)
				req.MaxRedirects = &m
			}
			if v, ok := sc.Opts["verify"].(string); ok {
				req.CABundle = v
			}
			got := map[string]any{}
			response, err := client.RequestRule(context.Background(), url, req)
			if err == nil {
				var body []byte
				if req.Stream && req.MaxBytes == 0 {
					body, err = io.ReadAll(response)
				} else {
					body, err = response.Content()
				}
				if err == nil {
					got["ok"] = map[string]any{"status": float64(response.StatusCode), "reason": response.Reason,
						"url": strings.ReplaceAll(response.URL, server.port, "{PORT}"), "body": hex.EncodeToString(body)}
				}
				response.Close()
			}
			if err != nil {
				class := PythonClass(err)
				if errors.Is(err, ErrRedirectEncoding) {
					class = "RedirectEncoding"
				}
				text := strings.ReplaceAll(err.Error(), server.port, "{PORT}")
				var ruleFetch *RuleFetchError
				if (fetchDetailClasses[class] && !errors.Is(err, ErrRedirectEncoding)) || errors.As(err, &ruleFetch) {
					text = ""
				}
				got["err"] = map[string]any{"class": class, "text": text}
			}
			server.close()
			want := sc.Result
			if e, ok := want["err"].(map[string]any); ok && fetchDetailClasses[e["class"].(string)] {
				want = map[string]any{"err": map[string]any{"class": e["class"], "text": ""}}
			}
			if sc.Name == "latin1 location" {
				archived, _ := sc.Result["err"].(map[string]any)
				if archived["class"] != "UnicodeDecodeError" || archived["text"] != "'utf-8' codec can't decode byte 0xe9 in position 4: unexpected end of data" {
					t.Fatal("original redirect diagnostic drift")
				}
				if !errors.Is(err, ErrRedirectEncoding) {
					t.Fatalf("native redirect refusal lost: %v", err)
				}
				want = map[string]any{"err": map[string]any{"class": "RedirectEncoding", "text": "redirect Location is not valid UTF-8"}}
			}

			// The url_requote case sends unreserved escapes in a different byte
			// form. Compare those after decoding: the scripted loopback server
			// receives the request and returns the same 200 response and body.
			if result, ok := got["ok"].(map[string]any); ok {
				if value, ok := result["url"].(string); ok {
					result["url"] = decodeUnreservedEscapes(value)
				}
			}
			if result, ok := want["ok"].(map[string]any); ok {
				if value, ok := result["url"].(string); ok {
					result["url"] = decodeUnreservedEscapes(value)
				}
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("result\n got %s\nwant %s", gotJSON, wantJSON)
			}
			gotSeen := append([]map[string]any{}, server.seen...)
			wantSeenRows := append([]map[string]any{}, sc.Seen...)
			for _, request := range gotSeen {
				if line, ok := request["line"].(string); ok {
					request["line"] = normalizeRequestTarget(line)
				}
			}
			for _, request := range wantSeenRows {
				if line, ok := request["line"].(string); ok {
					request["line"] = normalizeRequestTarget(line)
				}
			}
			seenJSON, _ := json.Marshal(gotSeen)
			wantSeen, _ := json.Marshal(wantSeenRows)
			if string(seenJSON) != string(wantSeen) {
				t.Errorf("requests\n got %s\nwant %s", seenJSON, wantSeen)
			}
		})
	}
}

// n2Accepted lists the scenarios whose difference from requests was accepted
// when URL and proxy handling moved to net/url and net/http (N2).
var n2Accepted = map[string]string{
	"redirect_requote":   "a Location with a malformed escape: requests quoted every %, Go keeps the valid escapes",
	"redirect_fullwidth": "the synthetic NFKC-delimiter host is refused by urllib but accepted by net/url before failing DNS",
	"proxy_user_only":    "net/http sends Basic auth when a proxy URL has only a username; requests omits it",
	"proxy_empty_user":   "net/http sends Basic auth when a proxy URL has only a password; requests omits it",
	"proxy_bad_escape":   "a proxy URL with a malformed escape in its credentials is refused (InvalidProxyURL)",
	"proxy_latin1":       "non-ASCII proxy credentials must be percent-encoded (InvalidProxyURL)",
	"proxy_non_latin1":   "non-ASCII proxy credentials must be percent-encoded (InvalidProxyURL)",
}
