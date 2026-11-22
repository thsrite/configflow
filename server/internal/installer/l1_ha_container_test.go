package installer

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// l1HAImage is a minimal Agent-like image: the real Agent binary for
// ha-port-check, behind a wrapper whose `svc status oxidns` reports the state
// written to /srv/l1/state (the s6 states themselves are covered by the
// image lab), and socat to occupy TCP/UDP 53.
func l1HAImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binary, err := os.ReadFile(agentBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "configflow-agent.real"), string(binary), 0o755)
	writeFile(t, filepath.Join(dir, "configflow-agent"), "#!/bin/sh\nif [ \"$*\" = 'svc status oxidns' ]; then cat /srv/l1/state; grep -q RUNNING /srv/l1/state; exit $?; fi\nexec /usr/local/bin/configflow-agent.real \"$@\"\n", 0o755)
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3\nRUN apk add --no-cache socat\nCOPY configflow-agent configflow-agent.real /usr/local/bin/\nENTRYPOINT [\"/bin/sleep\",\"infinity\"]\n", 0o644)
	const image = "configflow-l1-ha-checks:2"
	if out, err := l1Docker(t, "build", "-t", image, dir); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return image
}

func TestL1HAStartupWithRealSockets(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	image := l1HAImage(t)
	for _, fatal := range []bool{false, true} {
		t.Run(fmt.Sprintf("fatal=%t", fatal), func(t *testing.T) {
			dir := t.TempDir()
			state := "oxidns                           RUNNING  pid 12, uptime 0:00:05"
			if fatal {
				state = "oxidns                           FATAL"
			}
			writeFile(t, filepath.Join(dir, "state"), state+"\n", 0o644)
			name := "cfl1d5-" + strings.ToLower(rand.Text()[:12])
			t.Cleanup(func() {
				if out, err := l1Docker(t, "rm", "-f", name); err != nil {
					t.Errorf("cleanup: %v %s", err, out)
				}
			})
			out, err := l1Docker(t, "run", "-d", "--name", name, "--network", "none", "--mount", "type=bind,src="+dir+",dst=/srv/l1,readonly", image)
			if err != nil {
				t.Fatalf("run: %v %s", err, out)
			}
			id := strings.TrimSpace(string(out))
			writeFile(t, filepath.Join(dir, "docker"), "#!/bin/sh\nexec sudo -n docker \"$@\"\n", 0o755)
			env := []string{"PATH=" + dir + ":" + os.Getenv("PATH")}
			// Exercise identical checks against an owned namespace, never bind 53
			// in the test server's host namespace.
			checks := strings.Replace(haStartupChecks, "--network host", "--network "+q("container:"+id), 1)
			health := runCommand(t, "#!/bin/sh\nset -eu\n"+checks+"cf_wait_oxidns "+q(id)+"\nprintf L1_SUCCESS\n", dir, env...)
			if (health.code == 0) == fatal {
				t.Fatalf("%s health: %d %s %s", state, health.code, health.stdout, health.stderr)
			}
			if fatal {
				notContains(t, health.stdout, "L1_SUCCESS")
				return
			}
			listeners := map[string]string{"tcp": "TCP4-LISTEN:53,reuseaddr", "udp": "UDP4-RECV:53"}
			for _, kind := range []string{"free", "udp", "tcp"} {
				if kind != "free" {
					if out, err := l1Docker(t, "exec", "-d", name, "socat", listeners[kind], "-"); err != nil {
						t.Fatalf("occupy %s53: %v %s", kind, err, out)
					}
					if out, err := l1Docker(t, "exec", name, "sh", "-c", "i=0; until netstat -ln 2>/dev/null | grep -q ':53 '; do i=$((i+1)); [ $i -lt 50 ] || exit 1; sleep 0.1; done"); err != nil {
						t.Fatalf("%s53 listener: %v %s", kind, err, out)
					}
				}
				r := runCommand(t, "#!/bin/sh\nset -eu\n"+checks+"cf_check_dns_port "+q(image)+"\n", dir, env...)
				if (r.code == 0) != (kind == "free") {
					t.Fatalf("%s53: %d %s %s", kind, r.code, r.stdout, r.stderr)
				}
				if kind != "free" {
					contains(t, r.stderr, "53 端口不可用")
					if out, err := l1Docker(t, "exec", name, "sh", "-c", "pkill socat; i=0; while netstat -ln 2>/dev/null | grep -q ':53 '; do i=$((i+1)); [ $i -lt 50 ] || exit 1; sleep 0.1; done"); err != nil {
						t.Fatalf("release socket: %v %s", err, out)
					}
				}
			}
			t.Log("real UDP/TCP53 busy/free and RUNNING/FATAL startup checks use only owned container namespaces")
		})
	}
}
