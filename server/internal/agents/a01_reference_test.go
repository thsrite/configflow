package agents

// Regression tests of the A01 review against testdata/a01_reference.json,
// produced by running backend/agents/manager.py (CPython 3.11, requests 2.31)
// with testdata/gen_a01_reference.py on the test server.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

type a01Reference struct {
	URLs           []a01URLCase            `json:"urls"`
	JSONBodies     map[string]a01JSONBody  `json:"json_bodies"`
	RecordStatuses []json.RawMessage       `json:"record_statuses"`
	HeartbeatFresh []a01HeartbeatFreshCase `json:"heartbeat_fresh"`
}

type a01URLCase struct {
	Host    string         `json:"host"`
	Port    string         `json:"port"`
	Outcome string         `json:"outcome"`
	Request *a01URLRequest `json:"request"`
}

type a01URLRequest struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	Authorization string `json:"authorization"`
}

type a01JSONBody struct {
	Message     *string `json:"message"`
	Body        string  `json:"body"`
	ContentType string  `json:"content_type"`
}

type a01HeartbeatFreshCase struct {
	Now       string `json:"now"`
	Heartbeat string `json:"heartbeat"`
	Fresh     bool   `json:"fresh"`
}

func loadA01Reference(t *testing.T) a01Reference {
	t.Helper()
	data, err := os.ReadFile("testdata/a01_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref a01Reference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

type seenRequest struct{ method, path, host, authorization, body, contentType string }

// loopbackAgent listens on 127.0.0.1 and [::1] (same port) like the Python
// generator and records every request.
func loopbackAgent(t *testing.T) (int, func() []seenRequest, func()) {
	t.Helper()
	var mu sync.Mutex
	var seen []seenRequest
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, seenRequest{r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("Authorization"), string(body), r.Header.Get("Content-Type")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success": true, "seen": true}`)
	})
	l4, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l4.Addr().(*net.TCPAddr).Port
	servers := []*http.Server{{Handler: handler}}
	go servers[0].Serve(l4)
	if l6, err := net.Listen("tcp6", "[::1]:"+strconv.Itoa(port)); err == nil {
		s := &http.Server{Handler: handler}
		servers = append(servers, s)
		go s.Serve(l6)
	}
	t.Cleanup(func() {
		for _, s := range servers {
			s.Close()
		}
	})
	get := func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
	clear := func() {
		mu.Lock()
		seen = nil
		mu.Unlock()
	}
	return port, get, clear
}

// noDNS mirrors the generator's DNS restriction: Go's standard networking
// handles IP literals directly; only "localhost" is resolved by the hook.
func noDNS(t *testing.T) {
	previous := resolveHook
	resolveHook = func(host string) (string, error) {
		if strings.EqualFold(host, "localhost") {
			return "127.0.0.1", nil
		}
		return "", errors.New("Name or service not known (DNS disabled)")
	}
	t.Cleanup(func() { resolveHook = previous })
}

func refPort(kind string, port int) any {
	p := strconv.Itoa(port)
	full := strings.NewReplacer("0", "０", "1", "１", "2", "２", "3", "３", "4", "４", "5", "５", "6", "６", "7", "７", "8", "８", "9", "９")
	switch kind {
	case "int":
		return port
	case "str":
		return p
	case "float":
		return json.Number(p + ".0")
	case "space_before":
		return " " + p
	case "space_after":
		return p + " "
	case "plus":
		return "+" + p
	case "zero_pad":
		return "0" + p
	case "fullwidth":
		return full.Replace(p)
	case "slash":
		return p + "/x"
	case "true":
		return true
	case "none":
		return nil
	case "empty":
		return ""
	case "big":
		return port + 65536
	case "underscore":
		return p[:2] + "_" + p[2:]
	}
	panic(kind)
}

// TestReferenceAgentURLs: every host/port form an Agent record may hold
// reaches (or fails to reach) the Agent as requests does, with the same path
// and Authorization header (Basic auth from user:password@ replaces the
// Bearer token).
func TestReferenceAgentURLs(t *testing.T) {
	e := newEnv(t)
	e.m.SetClient(nil)
	restore := SetDefaultClient(HTTPClient{})
	defer restore()
	noDNS(t)
	port, seen, clear := loopbackAgent(t)
	for _, c := range loadA01Reference(t).URLs {
		host, kind := c.Host, c.Port
		name := strconv.Quote(host) + "/" + kind
		if host == "::1" || host == "[::1]" {
			if _, err := net.Listen("tcp6", "[::1]:0"); err != nil {
				t.Logf("%s: no IPv6 loopback, skipped", name)
				continue
			}
		}
		e.editSystem(func(system *model.System) { system.Agents = []model.Agent{} })
		id := e.register(map[string]any{"name": "u", "host": host, "port": refPort(kind, port)}, "").ID
		agent := e.get(id)
		clear()
		rawResp, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/status", Timeout: Seconds(5)})
		outcome := "ok"
		if err != nil {
			outcome = "exception"
		} else {
			var obj struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(rawResp, &obj) == nil && strings.HasPrefix(obj.Message, "Connection error: ") {
				outcome = "connection_error"
			}
		}
		if outcome != c.Outcome {
			if host == "127.1" || host == "0x7f.0.0.1" || host == "2130706433" {
				// Accepted N2 difference: requests/glibc treats legacy inet_aton
				// forms as IPv4; the Go client leaves them to the system resolver.
				t.Logf("%s: Go-native resolver does not reinterpret this IPv4 spelling", name)
				continue
			}
			if strings.Contains(host, "%") && !strings.Contains(host, ":") && outcome == "connection_error" {
				// Accepted (N2): net/url refuses a percent-encoded host name
				// ("%31%32%37.0.0.1") that requests decoded.
				continue
			}
			t.Errorf("%s: outcome %s (%s %v), Python %s", name, outcome, rawResp, err, c.Outcome)
			continue
		}
		want := c.Request
		got := seen()
		if (want == nil) != (len(got) == 0) {
			t.Errorf("%s: requests seen %v, Python %v", name, got, want)
			continue
		}
		if want == nil {
			continue
		}
		auth := want.Authorization
		if strings.HasPrefix(auth, "Bearer ") {
			auth = "Bearer " + agent.Token
		}
		if got[0].method != want.Method || got[0].path != want.Path || got[0].authorization != auth {
			t.Errorf("%s: %+v, Python %+v", name, got[0], want)
		}
	}
}

// TestReferenceJSONBodiesRefuseNaN: requests' json= (allow_nan=False) raises
// InvalidJSONError, a RequestException, before sending anything.
func TestReferenceJSONBodiesRefuseNaN(t *testing.T) {
	e := newEnv(t)
	e.m.SetClient(HTTPClient{})
	port, seen, clear := loopbackAgent(t)
	id := e.register(map[string]any{"name": "j", "host": "127.0.0.1", "port": port}, "").ID
	agent := e.get(id)
	payloads := map[string]json.RawMessage{
		"nan": json.RawMessage(`{"x": NaN}`),
		"inf": json.RawMessage(`[Infinity]`),
		"ok":  json.RawMessage(`{"x": 1.5, "y": "中"}`),
	}
	for name, want := range loadA01Reference(t).JSONBodies {
		clear()
		result, err := e.m.ProbeDomainsModel(e.ctx, agent, payloads[name], Timeout{})
		if want.Message != nil {
			if !IsTransportError(err) || err.Error() != *want.Message || len(seen()) != 0 {
				t.Errorf("%s: %v %v (sent %d), Python RequestException %q", name, result, err, len(seen()), *want.Message)
			}
			continue
		}
		got := seen()
		if err != nil || len(got) != 1 || !testjson.Equal(json.RawMessage(want.Body), json.RawMessage(got[0].body)) || got[0].contentType != want.ContentType {
			t.Errorf("%s: %v %v %+v, Python %+v", name, result, err, got, want)
		}
	}
}

func TestURLCredentialsUseUTF8BasicAuth(t *testing.T) {
	port, seen, clear := loopbackAgent(t)
	clear()
	url := "http://%E7%94%A8%E6%88%B7:%E5%AF%86%E7%A0%81@127.0.0.1:" + strconv.Itoa(port) + "/api/status"
	response, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: url, NoProxy: true, Timeout: Pair(1, 1)})
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("response %v, err %v", response, err)
	}
	requests := seen()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("用户:密码"))
	if len(requests) != 1 || requests[0].authorization != want {
		got := ""
		if len(requests) > 0 {
			got = requests[0].authorization
		}
		t.Fatalf("authorization %q, want %q; requests %+v", got, want, requests)
	}
}

// TestReferenceRecordDeploymentStatuses: record_deployment with any status
// value an Agent can answer; a list or dict status that changes the task is
// a TypeError (nothing stored), every scalar is stored as Python stores it.
func TestReferenceRecordDeploymentStatuses(t *testing.T) {
	e := newEnv(t)
	for index, rowRaw := range loadA01Reference(t).RecordStatuses {
		var c struct {
			Stored       string          `json:"stored"`
			Status       json.RawMessage `json:"status"`
			Options      map[string]any  `json:"options"`
			Error        *string         `json:"error"`
			ResultStatus json.RawMessage `json:"result_status"`
			SavedStatus  json.RawMessage `json:"saved_status"`
		}
		if err := json.Unmarshal(rowRaw, &c); err != nil {
			t.Fatal(err)
		}
		e.editSystem(func(system *model.System) {
			var a model.Agent
			rawAgent := mustJSON(t, map[string]any{
				"id": "a", "name": "a", "token": "tok", "host": "127.0.0.1", "port": 9,
				"service_type": "mihomo", "profile_id": "default",
				"deployments":       map[string]any{"d": map[string]any{"deployment_id": "d", "status": c.Stored, "profile_id": "default", "service_type": "mihomo"}},
				"latest_deployment": map[string]any{"deployment_id": "d", "status": c.Stored},
			})
			if err := json.Unmarshal(rawAgent, &a); err != nil {
				t.Fatal(err)
			}
			system.Agents = []model.Agent{a}
		})
		_, actReceipt := c.Options["activation_receipt"]
		_, upReceipt := c.Options["upload_receipt"]
		options := RecordOptions{ActivationReceipt: actReceipt, UploadReceipt: upReceipt}
		before, readErr := e.repo.Store().Fingerprint()
		if readErr != nil {
			t.Fatal(readErr)
		}
		patchRaw := mustJSON(t, map[string]any{"status": c.Status, "message": "m"})
		patch, err := DecodeDeploymentUpdate(patchRaw)
		var result *model.Deployment
		if err == nil {
			result, err = e.m.RecordDeploymentModel(e.ctx, "a", "d", patch, options)
		}
		if c.Error != nil {
			verifyNativeRecordStatusRow(t, index, rowRaw, *c.Error, err)
			after, readErr := e.repo.Store().Fingerprint()
			if readErr != nil || after != before {
				t.Fatalf("error row %d wrote transaction: %v", index, readErr)
			}
		} else {
			if err != nil {
				t.Errorf("row %d: %v", index, err)
			} else {
				gotStatus, _, _ := result.MemberJSON("status")
				if diff := diffJSONValues(t, c.ResultStatus, gotStatus); diff != "" {
					t.Errorf("row %d result_status: %s", index, diff)
				}
			}
		}
		savedDep, _ := e.stored("a").Deployments.Get("d")
		savedStatus, _, _ := savedDep.MemberJSON("status")
		if diff := diffJSONValues(t, c.SavedStatus, savedStatus); diff != "" {
			t.Errorf("row %d saved_status: %s", index, diff)
		}
	}
}

// TestReferenceHeartbeatFreshness: naive heartbeats against a naive now are
// wall-clock differences (America/New_York DST), aware against naive stale.
func TestReferenceHeartbeatFreshness(t *testing.T) {
	withZone(t, "America/New_York", func() {
		for _, c := range loadA01Reference(t).HeartbeatFresh {
			wall, ok := fromISOWall(c.Now)
			if !ok {
				t.Fatal(c)
			}
			got := FreshHeartbeat(c.Heartbeat, &wall)
			if got != c.Fresh {
				t.Errorf("%+v: %v", c, got)
			}
		}
	})
}

// fromISOWall is datetime.fromisoformat(naive) as a time whose wall clock in
// localZone is the text (the Go form of a naive now).
func fromISOWall(text string) (time.Time, bool) {
	parsed, naive, ok := fromISO(text + "+00:00") // the wall fields, read as UTC
	if !ok || naive {
		return time.Time{}, false
	}
	u := parsed.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), u.Nanosecond(), localZone), true
}

// TestHeartbeatFreshNowNilUsesWallClockAndMicroseconds: the nil-now path
// (production) compares wall clocks for naive heartbeats and truncates the
// current time to microseconds like datetime.now().
func TestHeartbeatFreshNowNilUsesWallClockAndMicroseconds(t *testing.T) {
	withZone(t, "America/New_York", func() {
		previous := clock
		defer func() { clock = previous }()
		// 2026-03-08 03:00:30 EDT is 90 s after 01:59:00 EST but 3690 s on the wall.
		clock = func() time.Time { return time.Date(2026, 3, 8, 7, 0, 30, 0, time.UTC) }
		if FreshHeartbeat("2026-03-08T01:59:00", nil) {
			t.Fatal("naive heartbeat across spring-forward must be stale (wall clock)")
		}
		if !FreshHeartbeat("2026-03-08T06:59:00+00:00", nil) {
			t.Fatal("aware heartbeat 90 s ago must be fresh")
		}
		// 120.0000009 s later: datetime.now() has no nanoseconds, so still fresh.
		clock = func() time.Time { return time.Date(2026, 3, 8, 12, 2, 0, 900, time.UTC) }
		if !FreshHeartbeat("2026-03-08T12:00:00+00:00", nil) {
			t.Fatal("nanoseconds must be truncated")
		}
	})
}

// TestAgentDeletedDuringRollbackIsAnErrorNotAnUnknownOutcome: Python's
// _deployment_request(None, ...) raises TypeError (not a RequestException).
func TestAgentDeletedDuringRollbackIsAnErrorNotAnUnknownOutcome(t *testing.T) {
	e := newEnv(t)
	before, err := e.repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.m.DeploymentRequestModel(context.Background(), nil, "POST", "deployments/x/rollback", nil, nil, nil)
	verifyDeletedAgentRequestReference(t, err)
	if after, readErr := e.repo.Store().Fingerprint(); readErr != nil || after != before {
		t.Fatal("deleted Agent request wrote transaction", readErr)
	}
}

// O3-6 no longer accesses Python preparation files, even a damaged legacy path.
func TestPreparationStatusIgnoresLegacyLockPath(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(e.dir+"/.agent-deployment-locks", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if held, err := e.m.defaultPreparationRunning(e.id(), "d"); err != nil || held {
		t.Fatal(held, err)
	}
	data, err := os.ReadFile(e.dir + "/.agent-deployment-locks")
	if err != nil || len(data) != 0 {
		t.Fatal("legacy sentinel changed", err)
	}
}
