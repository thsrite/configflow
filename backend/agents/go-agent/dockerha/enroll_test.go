package dockerha

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func enrollSettings(url string) string {
	return settingsJSON("", url, testToken)
}

func runEnroll(dir, stdin string) (string, error) {
	var out bytes.Buffer
	err := Enroll(context.Background(), dir, strings.NewReader(stdin), EnrollClient(), &out)
	return out.String(), err
}

func TestEnrollFirstAndRepeated(t *testing.T) {
	o := newOrigin(t)
	dir := t.TempDir()
	out, err := runEnroll(dir, enrollSettings(o.url())+"\n")
	if err != nil || out != msgEnrollDone+"\n" {
		t.Fatalf("%q %v", out, err)
	}
	if calls := o.recorded(); len(calls) != 1 || calls[0] != "POST /api/agents/docker-oxidns-ha-enroll/"+testSession+" Bearer "+testToken+" {}" {
		t.Fatalf("calls %q", calls)
	}
	config := readObject(t, filepath.Join(dir, agentConfig))
	if len(config) != 2 || config["agent_id"] != testAgentID || config["token"] != testSecret {
		t.Fatalf("config %v", config)
	}
	if marker := readObject(t, filepath.Join(dir, bundleMarker)); len(marker) != 1 || marker["url"] != o.url() {
		t.Fatalf("marker %v", marker)
	}
	for _, name := range []string{agentConfig, bundleMarker} {
		if info, _ := os.Stat(filepath.Join(dir, name)); info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s mode %v", name, info.Mode())
		}
	}
	// Re-running the same command with the same identity keeps the files.
	out, err = runEnroll(dir, enrollSettings(o.url()))
	if err != nil || out != msgEnrollKept+"\n" {
		t.Fatalf("repeat %q %v", out, err)
	}
	o.set(200, `{"agent_id": "agent_other", "token": "`+testSecret+`"}`, "")
	if _, err := runEnroll(dir, enrollSettings(o.url())); message(err) != msgEnrollDiffers {
		t.Fatal(err)
	}
}

func TestEnrollRefusalsAndAnswers(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, dir, url string)
		status int
		body   string
		stdin  func(url string) string
		want   string
		calls  int
	}{
		{name: "settings not json", stdin: func(string) string { return "nope" }, want: msgSettings},
		{name: "settings without token", stdin: func(url string) string { return `{"url": "` + url + `"}` }, want: msgSettings},
		{name: "config symlink", setup: func(t *testing.T, dir, _ string) {
			os.Symlink(filepath.Join(dir, "x"), filepath.Join(dir, agentConfig))
		}, want: msgEnrollSymlink},
		{name: "marker symlink", setup: func(t *testing.T, dir, _ string) {
			os.Symlink(filepath.Join(dir, "x"), filepath.Join(dir, bundleMarker))
		}, want: msgEnrollSymlink},
		{name: "marker other installation", setup: func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `{"url": "http://other.example/enroll"}`, 0o600)
		}, want: msgEnrollForeign},
		{name: "marker corrupt", setup: func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `{`, 0o600)
		}, want: msgEnrollForeign},
		{name: "marker not object", setup: func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `[]`, 0o600)
		}, want: msgEnrollForeign},
		{name: "existing identity without marker", setup: func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, agentConfig), `{}`, 0o600)
		}, want: msgEnrollExisting},
		{name: "rejected with message", status: 403, body: `{"message": "安装凭据已过期"}`, want: "安装凭据已过期；请回到引导重新生成或重试。", calls: 1},
		{name: "rejected without message", status: 403, body: `{"success": false}`, want: "安装登记失败；请回到引导重新生成或重试。", calls: 1},
		{name: "rejected non-json", status: 502, body: `<html>`, want: "安装登记失败；请回到引导重新生成或重试。", calls: 1},
		{name: "rejected non-string message", status: 400, body: `{"message": 5}`, want: "安装登记失败；请回到引导重新生成或重试。", calls: 1},
		{name: "answer not json", status: 200, body: `nope`, want: msgEnrollUnreachable, calls: 1},
		{name: "answer not object", status: 200, body: `[]`, want: msgEnrollInvalid, calls: 1},
		{name: "answer empty token", status: 200, body: `{"agent_id": "a", "token": ""}`, want: msgEnrollInvalid, calls: 1},
		{name: "existing identity unreadable", setup: func(t *testing.T, dir, url string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `{"url": "`+url+`"}`, 0o600)
			writeFile(t, filepath.Join(dir, agentConfig), `{`, 0o600)
		}, want: msgEnrollUnreadable, calls: 1},
		{name: "existing identity differs", setup: func(t *testing.T, dir, url string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `{"url": "`+url+`"}`, 0o600)
			writeFile(t, filepath.Join(dir, agentConfig), `{"agent_id": "`+testAgentID+`", "token": "other"}`, 0o600)
		}, want: msgEnrollDiffers, calls: 1},
		{name: "marker kept, identity written", setup: func(t *testing.T, dir, url string) {
			writeFile(t, filepath.Join(dir, bundleMarker), `{"url": "`+url+`", "extra": true}`, 0o600)
		}, want: "", calls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			if test.status != 0 {
				o.set(test.status, test.body, "")
			}
			dir := t.TempDir()
			if test.setup != nil {
				test.setup(t, dir, o.url())
			}
			stdin := enrollSettings(o.url())
			if test.stdin != nil {
				stdin = test.stdin(o.url())
			}
			_, err := runEnroll(dir, stdin)
			if message(err) != test.want {
				t.Fatalf("got %q want %q", message(err), test.want)
			}
			if len(o.recorded()) != test.calls {
				t.Fatalf("calls %q", o.recorded())
			}
		})
	}
}

func TestEnrollUnreachable(t *testing.T) {
	o := newOrigin(t)
	o.server.Close()
	if _, err := runEnroll(t.TempDir(), enrollSettings(o.url())); message(err) != msgEnrollUnreachable {
		t.Fatal(err)
	}
}

func TestEnrollUnwritableDirectory(t *testing.T) {
	o := newOrigin(t)
	if _, err := runEnroll(filepath.Join(t.TempDir(), "missing"), enrollSettings(o.url())); message(err) != msgSaveFailed {
		t.Fatal(err)
	}
}

func TestCheckEnrollment(t *testing.T) {
	const url = "http://panel.example/api/agents/docker-oxidns-ha-enroll/x"
	identity := `{"agent_id": "` + testAgentID + `", "token": "` + testSecret + `", "service_type": "oxidns"}`
	tests := []struct {
		name           string
		config, marker string
		link           string
		want           string
	}{
		{"verified", identity, `{"url": "` + url + `"}`, "", ""},
		{"config missing", "", `{"url": "` + url + `"}`, "", msgProofMissing},
		{"marker missing", identity, "", "", msgProofMissing},
		{"config symlink", identity, `{"url": "` + url + `"}`, agentConfig, msgProofMissing},
		{"marker symlink", identity, `{"url": "` + url + `"}`, bundleMarker, msgProofMissing},
		{"config corrupt", `{`, `{"url": "` + url + `"}`, "", msgProofUnreadable},
		{"marker corrupt", identity, `nope`, "", msgProofUnreadable},
		{"other url", identity, `{"url": "http://other.example"}`, "", msgProofMismatch},
		{"marker not object", identity, `[]`, "", msgProofMismatch},
		{"redacted token", `{"agent_id": "a", "token": "[REDACTED]"}`, `{"url": "` + url + `"}`, "", msgProofMismatch},
		{"empty agent id", `{"agent_id": "", "token": "t"}`, `{"url": "` + url + `"}`, "", msgProofMismatch},
		{"token not string", `{"agent_id": "a", "token": 1}`, `{"url": "` + url + `"}`, "", msgProofMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range map[string]string{agentConfig: test.config, bundleMarker: test.marker} {
				if content == "" {
					continue
				}
				target := filepath.Join(dir, name)
				if test.link == name {
					target = filepath.Join(dir, "real-"+name)
					os.Symlink(target, filepath.Join(dir, name))
				}
				writeFile(t, target, content, 0o600)
			}
			var out bytes.Buffer
			err := CheckEnrollment(dir, url, &out)
			if message(err) != test.want {
				t.Fatalf("got %q want %q", message(err), test.want)
			}
			if test.want == "" && out.String() != msgProofVerified+"\n" {
				t.Fatalf("out %q", out.String())
			}
		})
	}
}

func TestCheckPort(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network+" taken", func(t *testing.T) {
			var port int
			if network == "tcp" {
				listener, err := net.Listen("tcp4", "0.0.0.0:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				port = listener.Addr().(*net.TCPAddr).Port
			} else {
				conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				port = conn.LocalAddr().(*net.UDPAddr).Port
			}
			if err := CheckPort(port); message(err) != msgPortUnavailable {
				t.Fatalf("port %d: %v", port, err)
			}
		})
	}
	t.Run("free", func(t *testing.T) {
		listener, err := net.Listen("tcp4", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		if err := CheckPort(port); err != nil {
			t.Fatalf("port %s: %v", strconv.Itoa(port), err)
		}
	})
}
