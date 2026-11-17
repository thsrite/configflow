package installer

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func l1HAImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3\nRUN apk add --no-cache python3 supervisor iproute2 bind-tools && mkdir -p /etc/supervisor\nENTRYPOINT [\"/usr/bin/supervisord\",\"-n\",\"-c\",\"/etc/supervisor/supervisord.conf\"]\n", 0o644)
	const image = "configflow-l1-ha-checks:1"
	if out, err := l1Docker(t, "build", "-t", image, dir); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return image
}

func TestL1HAStartupWithRealSocketsAndSupervisor(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	image := l1HAImage(t)
	for _, fatal := range []bool{false, true} {
		t.Run(fmt.Sprintf("fatal=%t", fatal), func(t *testing.T) {
			dir := t.TempDir()
			command := "/bin/sleep 100000"
			if fatal {
				command = "/bin/sh -c 'exit 37'"
			}
			config := "[supervisord]\nnodaemon=true\nlogfile=/tmp/supervisor.log\npidfile=/tmp/supervisor.pid\n[unix_http_server]\nfile=/tmp/supervisor.sock\n[rpcinterface:supervisor]\nsupervisor.rpcinterface_factory=supervisor.rpcinterface:make_main_rpcinterface\n[supervisorctl]\nserverurl=unix:///tmp/supervisor.sock\n[program:oxidns]\ncommand=" + command + "\nautostart=true\nstartsecs=1\nstartretries=0\n"
			for _, kind := range []string{"tcp", "udp"} {
				config += "[program:" + kind + "53]\ncommand=python3 /srv/l1/listen.py " + kind + "\nautostart=false\nstartsecs=1\n"
			}
			writeFile(t, filepath.Join(dir, "supervisord.conf"), config, 0o644)
			writeFile(t, filepath.Join(dir, "listen.py"), "import socket,sys,time\ns=socket.socket(socket.AF_INET,socket.SOCK_STREAM if sys.argv[1]=='tcp' else socket.SOCK_DGRAM)\ns.bind(('0.0.0.0',53))\nif sys.argv[1]=='tcp': s.listen()\ntime.sleep(100000)\n", 0o644)
			name := "cfl1d5-" + strings.ToLower(rand.Text()[:12])
			t.Cleanup(func() {
				if out, err := l1Docker(t, "rm", "-f", name); err != nil {
					t.Errorf("cleanup: %v %s", err, out)
				}
			})
			out, err := l1Docker(t, "run", "-d", "--name", name, "--network", "none", "--mount", "type=bind,src="+dir+",dst=/srv/l1,readonly", "--mount", "type=bind,src="+filepath.Join(dir, "supervisord.conf")+",dst=/etc/supervisor/supervisord.conf,readonly", image)
			if err != nil {
				t.Fatalf("run: %v %s", err, out)
			}
			id := strings.TrimSpace(string(out))
			state := "RUNNING"
			if fatal {
				state = "FATAL"
			}
			out, err = l1Docker(t, "exec", name, "sh", "-c", `i=0; until supervisorctl -c /etc/supervisor/supervisord.conf status oxidns 2>/dev/null | grep -q "$1"; do i=$((i+1)); [ "$i" -lt 50 ] || exit 1; sleep 0.1; done`, "sh", state)
			if err != nil {
				t.Fatalf("real supervisor did not reach %s: %v %s", state, err, out)
			}
			writeFile(t, filepath.Join(dir, "docker"), "#!/bin/sh\nexec sudo -n docker \"$@\"\n", 0o755)
			env := []string{"PATH=" + dir + ":" + os.Getenv("PATH")}
			// Exercise identical checks against an owned namespace, never bind 53
			// in the test server's host namespace.
			checks := strings.Replace(haStartupChecks, "--network host", "--network "+q("container:"+id), 1)
			health := runCommand(t, "#!/bin/sh\nset -eu\n"+checks+"cf_wait_oxidns "+q(id)+"\nprintf L1_SUCCESS\n", dir, env...)
			if (health.code == 0) == fatal {
				t.Fatalf("real %s health: %d %s %s", state, health.code, health.stdout, health.stderr)
			}
			if fatal {
				notContains(t, health.stdout, "L1_SUCCESS")
				return
			}
			for _, kind := range []string{"free", "udp", "tcp"} {
				if kind != "free" {
					if out, err := l1Docker(t, "exec", name, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "start", kind+"53"); err != nil {
						t.Fatalf("occupy %s53: %v %s", kind, err, out)
					}
				}
				r := runCommand(t, "#!/bin/sh\nset -eu\n"+checks+"cf_check_dns_port "+q(image)+"\n", dir, env...)
				if (r.code == 0) != (kind == "free") {
					t.Fatalf("%s53: %d %s %s", kind, r.code, r.stdout, r.stderr)
				}
				if kind != "free" {
					contains(t, r.stderr, "53 端口不可用")
					if out, err := l1Docker(t, "exec", name, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "stop", kind+"53"); err != nil {
						t.Fatalf("release socket: %v %s", err, out)
					}
				}
			}
			t.Log("real UDP/TCP53 busy/free and supervisor RUNNING/FATAL checks use only owned container namespaces")
		})
	}
}
