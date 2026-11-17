package installer

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

const l1DNSResponder = `import socket,struct,pathlib
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.bind(('192.0.2.53',53))
pathlib.Path('/tmp/dns-ready').touch()
while True:
    q,peer=s.recvfrom(4096)
    end=12
    while q[end]: end+=1+q[end]
    end+=5
    kind=struct.unpack('!H',q[end-4:end-2])[0]
    payload=socket.inet_pton(socket.AF_INET6,'2001:db8::123') if kind==28 else socket.inet_aton('192.0.2.123')
    answer=b'\xc0\x0c'+struct.pack('!HHIH',kind,1,60,len(payload))+payload
    s.sendto(q[:2]+struct.pack('!HHHHH',0x8180,1,1,0,0)+q[12:end]+answer,peer)
`

func TestL1HAVerificationWithRealDNSClients(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	image := l1HAImage(t)
	dir := t.TempDir()
	command := mustGuide(t, jsonx.Obj("action", "verify", "vip", "192.0.2.53"), nil).GetString("verification_command")
	writeFile(t, filepath.Join(dir, "verify.sh"), command, 0o644)
	writeFile(t, filepath.Join(dir, "dns.py"), l1DNSResponder, 0o644)
	for _, tool := range []string{"dig", "nslookup", "busybox"} {
		path := filepath.Join(dir, tool)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		target := "/usr/bin/" + tool
		if tool == "busybox" {
			target = "/bin/busybox"
		}
		if err := os.Symlink(target, filepath.Join(path, tool)); err != nil {
			t.Fatal(err)
		}
		if tool == "busybox" {
			if err := os.Symlink("/bin/grep", filepath.Join(path, "grep")); err != nil {
				t.Fatal(err)
			}
		}
	}
	name := "cfl1d6-" + strings.ToLower(rand.Text()[:12])
	t.Cleanup(func() {
		if out, err := l1Docker(t, "rm", "-f", name); err != nil {
			t.Errorf("cleanup: %v %s", err, out)
		}
	})
	if out, err := l1Docker(t, "run", "-d", "--name", name, "--network", "none", "--cap-add", "NET_ADMIN", "--mount", "type=bind,src="+dir+",dst=/srv/l1,readonly", "--entrypoint", "/bin/sh", image, "-ec", "ip addr add 192.0.2.53/32 dev lo; exec python3 /srv/l1/dns.py"); err != nil {
		t.Fatalf("DNS fixture: %v %s", err, out)
	}
	if out, err := l1Docker(t, "exec", name, "sh", "-c", `i=0; until [ -f /tmp/dns-ready ]; do i=$((i+1)); [ "$i" -lt 50 ] || exit 1; sleep 0.1; done`); err != nil {
		t.Fatalf("DNS fixture ready: %v %s", err, out)
	}
	for _, tool := range []string{"dig", "nslookup", "busybox"} {
		out, err := l1Docker(t, "exec", name, "/usr/bin/env", "PATH=/srv/l1/"+tool, "/bin/sh", "/srv/l1/verify.sh")
		if err != nil || !strings.Contains(string(out), "192.0.2.123") {
			t.Fatalf("real %s query failed: %v %s", tool, err, out)
		}
		t.Logf("real %s queried isolated VIP192.0.2.53 and received fixture A192.0.2.123", tool)
	}
	// An actual minimal Debian12 has no DNS client. Preserve the clear install
	// hint rather than returning success using the default system resolver.
	out, err := l1Docker(t, "run", "--rm", "--network", "none", "--mount", "type=bind,src="+dir+",dst=/srv/l1,readonly", "--entrypoint", "/bin/sh", "debian:12", "/srv/l1/verify.sh")
	if err == nil || !strings.Contains(string(out), "dnsutils") || !strings.Contains(string(out), "bind-tools") {
		t.Fatalf("minimal Debian12 missing-tool hint: %v %s", err, out)
	}
	t.Log("minimal Debian12 gives explicit DNS-client installation hint")
}
