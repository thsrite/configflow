package upgrades

// Regression tests of the A15 review, asserting results of the real Python
// modules recorded by testdata/gen_a15_review.py (testdata/a15_review.json).

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func reviewReference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/a15_review.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

// pyErrorText is "Class: message" of an error as the reference records it.
func pyErrorText(err error) string {
	return ErrorClass(err) + ": " + err.Error()
}

func withoutVolatile(value any) any {
	object, ok := value.(*jsonx.Object)
	if !ok {
		return value
	}
	out := object.Clone()
	out.Delete("updated_at")
	out.Delete("events")
	return out
}

// TestReviewPollReference runs poll on the reference records (malformed
// records included: Python's dict() of a str/int/list, pairs, unhashable
// statuses, odd started_at/recovery_reported_at, protocol True/2.0/3,
// heartbeats in year 1, aware, None) with the Agent unreachable, at the
// reference's clock.
func TestReviewPollReference(t *testing.T) {
	reference := reviewReference(t)
	clock := reference.GetFloat("now")
	previous := now
	now = func() float64 { return clock + 1 }
	t.Cleanup(func() { now = previous })
	for _, item := range reference.GetList("poll") {
		c := item.(*jsonx.Object)
		t.Run(c.GetString("name"), func(t *testing.T) {
			e := newUpgradeEnv(t)
			requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
				return nil, &agents.TransportError{Err: errors.New("refused")}
			}
			fields := []any{"name", "n", "host", "127.0.0.1", "port", 1, "token", "t", "version", "1.0",
				"last_heartbeat", "2026-11-16T00:00:00", "latest_upgrade", c.Value("record")}
			for key, value := range c.GetObject("agent").All() {
				fields = append(fields, key, value)
			}
			e.update(fields...)
			state, err := Poll(e.ctx, e.m, e.ident)
			if want := c.Value("error"); want != nil {
				if err == nil {
					t.Fatalf("poll = %s, Python %s", jsonx.Repr(state), want)
				}
				if got := pyErrorText(err); got != want {
					t.Fatalf("poll error %q, Python %q", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("poll error %v, Python %s", err, jsonx.Repr(c.Value("result")))
				}
				if got := withoutVolatile(state); !jsonx.Equal(got, c.Value("result")) {
					t.Fatalf("poll = %s\nPython  %s", jsonx.Repr(got), jsonx.Repr(c.Value("result")))
				}
			}
			if stored := withoutVolatile(e.agent().Value("latest_upgrade")); !jsonx.Equal(stored, c.Value("stored")) {
				t.Fatalf("stored %s\nPython %s", jsonx.Repr(stored), jsonx.Repr(c.Value("stored")))
			}
		})
	}
}

// TestReviewStartReadsPreviousRecordInsideReservation: Python reads the
// stored latest_upgrade of its snapshot inside reserve(), after the DNS HA
// check, so a busy pair is reported even when the record is malformed.
func TestReviewStartReadsPreviousRecordInsideReservation(t *testing.T) {
	starts := reviewReference(t).GetObject("starts")
	for name, extra := range map[string][]any{
		"malformed previous, HA active": {agents.HAField, jsonx.Obj("status", "running")},
		"malformed previous":            {agents.HAField, nil},
	} {
		t.Run(name, func(t *testing.T) {
			e := newUpgradeEnv(t)
			requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
				return respond(jsonx.Obj(), 404), nil
			}
			e.update(append([]any{"token", "t", "latest_upgrade", "abc"}, extra...)...)
			_, err := Start(e.ctx, e.m, e.ident)
			if err == nil || pyErrorText(err) != starts.GetString(name) {
				t.Fatalf("start error %v, Python %s", err, starts.GetString(name))
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
	reference := reviewReference(t).GetObject("paths")
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
			if got := reference.GetObject(name).GetString("error"); got != "PermissionError" {
				t.Fatalf("reference %q", got)
			}
			ResetArtifactCache()
			t.Setenv("AGENTS_STATIC_DIR", configured)
			result, err := Artifacts()
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("artifacts() = %v, %v; Python PermissionError", result, err)
			}
		})
	}
}

// TestReviewLatin1Tokens: _request/_reaches/_ask send the Bearer header as
// latin-1 bytes; a token latin-1 cannot encode is a UnicodeEncodeError: start
// cannot connect, _reaches/_ask raise (a logged failure, a 500).
func TestReviewLatin1Tokens(t *testing.T) {
	reference := reviewReference(t)
	for value, want := range reference.GetObject("encode_errors").All() {
		_, err := agents.EncodeHeaderLatin1(value)
		if err == nil || err.Error() != want {
			t.Errorf("encode %q: %v, Python %v", value, err, want)
		}
	}
	headers := reference.GetObject("headers")
	for name, token := range map[string]any{"latin1": "tök", "cjk": "密钥", "nel": "tok\u0085",
		"none": nil, "int": jsonx.Int(7), "newline": "a\nb"} {
		t.Run(name, func(t *testing.T) {
			want := headers.GetObject(name)
			for _, call := range []string{"request", "reaches", "ask"} {
				port, seen := captureAuthorization(t)
				e := newUpgradeEnv(t)
				agent := jsonx.Obj("id", "a1", "host", "127.0.0.1", "port", port, "token", token)
				var err error
				switch call {
				case "request":
					_, err = request(e.ctx, e.m, agent, "GET", "/api/upgrade-info", nil)
				case "reaches":
					_, err = reaches(e.ctx, e.m, agent, "127.0.0.1")
				case "ask":
					_, _, err = ask(e.ctx, e.m, agent, "127.0.0.1", "GET", nil)
				}
				expected := want.GetObject(call)
				got := <-seen
				if wantAuth, _ := expected.Value("authorization").(string); got != wantAuth {
					t.Errorf("%s: Authorization %q, Python %q", call, got, wantAuth)
				}
				pyClass, _ := expected.Value("error").(string)
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
