package upgrades

// Regression tests of the A15 review, asserting results of the real Python
// modules recorded by testdata/gen_a15_review.py (testdata/a15_review.json).

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

type a15ReviewReference struct {
	Now    float64           `json:"now"`
	Poll   []json.RawMessage `json:"poll"`
	Starts map[string]string `json:"starts"`
	Paths  map[string]struct {
		Error string `json:"error"`
	} `json:"paths"`
	EncodeErrors map[string]string          `json:"encode_errors"`
	Headers      map[string]json.RawMessage `json:"headers"`
}

func loadReviewReference(t *testing.T) a15ReviewReference {
	t.Helper()
	data, err := os.ReadFile("testdata/a15_review.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref a15ReviewReference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

// pyErrorText is "Class: message" of an error as the reference records it.
func pyErrorText(err error) string {
	return ErrorClass(err) + ": " + err.Error()
}

func withoutVolatile(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		var generic any
		_ = json.Unmarshal(raw, &generic)
		return generic
	}
	delete(m, "updated_at")
	delete(m, "events")
	return m
}

func agentLatestUpgradeRaw(t *testing.T, a *model.Agent) json.RawMessage {
	t.Helper()
	raw, present, err := a.MemberJSON("latest_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		return json.RawMessage("null")
	}
	return raw
}

// TestReviewPollReference runs poll on the reference records (malformed
// records included: Python's dict() of a str/int/list, pairs, unhashable
// statuses, odd started_at/recovery_reported_at, protocol True/2.0/3,
// heartbeats in year 1, aware, None) with the Agent unreachable, at the
// reference's clock.
func TestReviewPollReference(t *testing.T) {
	reference := loadReviewReference(t)
	clock := reference.Now
	previous := now
	now = func() float64 { return clock + 1 }
	t.Cleanup(func() { now = previous })
	for _, itemRaw := range reference.Poll {
		var c struct {
			Name   string                     `json:"name"`
			Record json.RawMessage            `json:"record"`
			Agent  map[string]json.RawMessage `json:"agent"`
			Error  *string                    `json:"error"`
			Result json.RawMessage            `json:"result"`
			Stored json.RawMessage            `json:"stored"`
		}
		if err := json.Unmarshal(itemRaw, &c); err != nil {
			t.Fatal(err)
		}
		t.Run(c.Name, func(t *testing.T) {
			e := newUpgradeEnv(t)
			t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
				return nil, &agents.TransportError{Err: errors.New("refused")}
			}))
			fields := map[string]any{
				"name": "n", "host": "127.0.0.1", "port": 1, "token": "t", "version": "1.0",
				"last_heartbeat": "2026-11-16T00:00:00", "latest_upgrade": c.Record,
			}
			for key, value := range c.Agent {
				fields[key] = value
			}
			e.updateMap(fields)
			state, err := PollModel(e.ctx, e.m, e.ident)
			policy := nativeReference(t, "poll", c.Name, itemRaw)
			if policy != nil {
				verifyNativeCorruptReference(t, policy, err)
				if !testjson.Equal(withoutVolatile(c.Record), withoutVolatile(agentLatestUpgradeRaw(t, e.agent()))) {
					t.Fatal("damaged record was written")
				}
				return
			}
			if c.Error != nil {
				if err == nil {
					t.Fatalf("poll = %+v, Python %s", state, *c.Error)
				}
				if got := pyErrorText(err); got != *c.Error {
					t.Fatalf("poll error %q, Python %q", got, *c.Error)
				}
			} else {
				if err != nil {
					t.Fatalf("poll error %v, Python %s", err, c.Result)
				}
				if diff := testjson.Diff(c.Result, withoutVolatile(state)); diff != "" {
					t.Fatalf("poll diff: %s", diff)
				}
			}
			if diff := testjson.Diff(c.Stored, withoutVolatile(agentLatestUpgradeRaw(t, e.agent()))); diff != "" {
				t.Fatalf("stored diff: %s", diff)
			}
		})
	}
}

// TestReviewStartReadsPreviousRecordInsideReservation: Python reads the
// stored latest_upgrade of its snapshot inside reserve(), after the DNS HA
// check, so a busy pair is reported even when the record is malformed.
func TestReviewStartReadsPreviousRecordInsideReservation(t *testing.T) {
	starts := loadReviewReference(t).Starts
	for name, extra := range map[string]map[string]any{
		"malformed previous, HA active": {agents.HAField: map[string]any{"status": "running"}},
		"malformed previous":            {agents.HAField: nil},
	} {
		t.Run(name, func(t *testing.T) {
			e := newUpgradeEnv(t)
			t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
				return respond(map[string]any{}, 404), nil
			}))
			fields := map[string]any{"token": "t", "latest_upgrade": "abc"}
			for k, v := range extra {
				fields[k] = v
			}
			e.updateMap(fields)
			_, err := StartModel(e.ctx, e.m, e.ident)
			raw, _ := compactUnescapedJSON(struct {
				Name        string `json:"name"`
				PythonError string `json:"python_error"`
			}{name, starts[name]})
			if policy := nativeReference(t, "start", name, raw); policy != nil {
				verifyNativeCorruptReference(t, policy, err)
				return
			}
			if err == nil || pyErrorText(err) != starts[name] {
				t.Fatalf("start error %v, Python %s", err, starts[name])
			}
		})
	}
}

// TestReviewArtifactStatErrorsAreRaised: Path.exists()/is_file() of Python
// 3.11 raise PermissionError (start answers 500); a directory without search
// permission must not read as "artifact missing" (409 "更新文件未准备就绪").
func TestReviewArtifactStatErrorsAreRaised(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	reference := loadReviewReference(t).Paths
	root := t.TempDir()
	noexec := filepath.Join(root, "noexec")
	if err := os.Mkdir(noexec, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noexec, "configflow-agent-linux-amd64"), []byte("\x7fELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(noexec, 0o600)
	t.Cleanup(func() { os.Chmod(noexec, 0o700) })
	for name, configured := range map[string]string{
		"directory without search permission":         noexec,
		"below a directory without search permission": filepath.Join(noexec, "sub"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := reference[name].Error; got != "PermissionError" {
				t.Fatalf("reference %q", got)
			}
			ResetArtifactCache()
			t.Setenv("AGENTS_STATIC_DIR", configured)
			result, err := ArtifactsModel(context.Background())
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("artifacts() = %+v, %v; Python PermissionError", result, err)
			}
		})
	}
}

// TestReviewLatin1Tokens: _request/_reaches/_ask send the Bearer header as
// latin-1 bytes; a token latin-1 cannot encode is a UnicodeEncodeError: start
// cannot connect, _reaches/_ask raise (a logged failure, a 500).
func TestReviewLatin1Tokens(t *testing.T) {
	reference := loadReviewReference(t)
	for value, want := range reference.EncodeErrors {
		encoded, err := agents.EncodeHeaderLatin1(value)
		verifyNativeHeaderReference(t, value, want, encoded, err)
	}
	headers := reference.Headers
	for name, token := range map[string]any{
		"latin1": "tök", "cjk": "密钥", "nel": "tok\u0085",
		"none": nil, "int": 7, "newline": "a\nb",
	} {
		t.Run(name, func(t *testing.T) {
			wantRaw := headers[name]
			var wantCalls map[string]struct {
				Authorization *string `json:"authorization"`
				Error         *string `json:"error"`
			}
			if err := json.Unmarshal(wantRaw, &wantCalls); err != nil {
				t.Fatal(err)
			}
			for _, call := range []string{"request", "reaches", "ask"} {
				e := newUpgradeEnv(t)
				// Fixture initialization must not consume the listener's deadline.
				port, seen := captureAuthorization(t)
				agent := agentModelForTest(t, map[string]any{"id": "a1", "host": "127.0.0.1", "port": port, "token": token})
				var err error
				switch call {
				case "request":
					_, err = requestModel(e.ctx, e.m, agent, "GET", "/api/upgrade-info", nil)
				case "reaches":
					_, err = reachesModel(e.ctx, e.m, agent, "127.0.0.1")
				case "ask":
					_, _, err = askModel(e.ctx, e.m, agent, "127.0.0.1", "GET", nil)
				}
				expected := wantCalls[call]
				got := <-seen
				if verifyNativeCredentialReference(t, name, token, wantRaw, got, err) {
					continue
				}
				wantAuth := ""
				if expected.Authorization != nil {
					wantAuth = *expected.Authorization
				}
				if got != wantAuth {
					t.Errorf("%s: Authorization %q, Python %q", call, got, wantAuth)
				}
				pyClass := ""
				if expected.Error != nil {
					pyClass = *expected.Error
				}
				switch pyClass {
				case "UnicodeEncodeError", "TypeError":
					if err == nil || ErrorClass(err) != pyClass {
						t.Errorf("%s: error %v, Python %s", call, err, pyClass)
					}
				default: // a RequestException (or a result): no Python exception leaves these
					if err != nil && !agents.IsTransportError(err) {
						t.Errorf("%s: error %v, Python %s", call, err, pyClass)
					}
				}
			}
		})
	}
}

// captureAuthorization listens on a loopback port and records the raw bytes
// of the Authorization header of the first request (hex, "" when nothing
// connected). The returned function closes the listener and gives the value.
func captureAuthorization(t *testing.T) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 1)
	go func() {
		listener.(*net.TCPListener).SetDeadline(time.Now().Add(1500 * time.Millisecond))
		conn, err := listener.Accept()
		listener.Close()
		if err != nil {
			seen <- ""
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		var data []byte
		buffer := make([]byte, 4096)
		for !bytes.Contains(data, []byte("\r\n\r\n")) {
			n, err := conn.Read(buffer)
			data = append(data, buffer[:n]...)
			if err != nil {
				break
			}
		}
		found := ""
		for _, line := range bytes.Split(data, []byte("\r\n")) {
			if len(line) > 14 && strings.EqualFold(string(line[:14]), "authorization:") {
				found = hex.EncodeToString(bytes.Trim(line[14:], " \t\r\n\v\f"))
			}
		}
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}"))
		seen <- found
	}()
	return listener.Addr().(*net.TCPAddr).Port, seen
}
