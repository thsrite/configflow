package dockerha

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type companionFixture struct {
	paths  CompanionPaths
	env    map[string]string
	mounts []string // mountinfo lines
}

func newCompanion(t *testing.T) *companionFixture {
	t.Helper()
	root := t.TempDir()
	f := &companionFixture{env: map[string]string{}}
	f.paths = CompanionPaths{AgentDir: filepath.Join(root, "agent"), CoreDir: filepath.Join(root, "oxidns"), HADir: filepath.Join(root, "ha"),
		HostIDFile: filepath.Join(root, "machine-id"), IdentityDir: filepath.Join(root, "identity"),
		MountInfo: filepath.Join(root, "mountinfo"), ProcStatus: filepath.Join(root, "status"), NSDir: "/proc/self/ns",
		Wait: 200 * time.Millisecond, Poll: 10 * time.Millisecond}
	for _, dir := range []string{f.paths.AgentDir, f.paths.CoreDir, f.paths.HADir, f.paths.IdentityDir} {
		os.Mkdir(dir, 0o755)
	}
	writeFile(t, f.paths.HostIDFile, "0123456789abcdef0123456789abcdef\n", 0o444)
	writeFile(t, f.paths.ProcStatus, "Name:\tx\nCapEff:\t0000000000003000\n", 0o444)
	return f
}

func (f *companionFixture) mount(path, options, fstype string) {
	escaped := strings.NewReplacer(" ", `\040`).Replace(path)
	f.mounts = append(f.mounts, fmt.Sprintf("100 90 0:50 / %s %s - %s /dev/sda1 rw", escaped, options, fstype))
}

func (f *companionFixture) check(t *testing.T) error {
	t.Helper()
	writeFile(t, f.paths.MountInfo, strings.Join(f.mounts, "\n")+"\n", 0o444)
	return CheckCompanion(context.Background(), f.paths, func(key string) string { return f.env[key] })
}

// installCompact claims the companion fixture's directories as one compact
// installation for the given scope.
func (f *companionFixture) installCompact(t *testing.T, scope string, ready bool) string {
	t.Helper()
	binding := map[string]any{"server_url": "http://192.0.2.10", "name": "dns-a", "ip": "192.0.2.21", "port": 8082, "network": "host", "session": scope}
	owner, _ := bindingOwner(binding)
	id := strings.Repeat("e", 32)
	record, _ := json.Marshal(map[string]any{"protocol": 2, "owner": owner, "binding": binding, "installation_id": id, "ready": ready})
	writeFile(t, filepath.Join(f.paths.AgentDir, compactMarker), string(record), 0o600)
	short, _ := json.Marshal(map[string]any{"protocol": 2, "owner": owner, "installation_id": id})
	for _, dir := range []string{f.paths.CoreDir, f.paths.HADir} {
		writeFile(t, filepath.Join(dir, compactMarker), string(short), 0o600)
	}
	writeFile(t, filepath.Join(f.paths.AgentDir, compactIdentity), id+"\n", 0o600)
	return id
}

func TestCompanionEvidence(t *testing.T) {
	tests := []struct {
		name     string
		prep     func(t *testing.T, f *companionFixture)
		identity string
	}{
		{"file", func(*testing.T, *companionFixture) {}, "0123456789abcdef0123456789abcdef"},
		{"default mode is file", func(_ *testing.T, f *companionFixture) { f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "" }, "0123456789abcdef0123456789abcdef"},
		{"volume", func(t *testing.T, f *companionFixture) {
			f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "volume"
			f.mount(f.paths.IdentityDir, "ro,relatime", "ext4")
			writeFile(t, filepath.Join(f.paths.IdentityDir, "host-id"), " volume-identity-0001 \n", 0o444)
		}, "volume-identity-0001"},
		{"installation", func(t *testing.T, f *companionFixture) {
			f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "installation"
			f.env["INSTALL_SCOPE"] = testSession
			f.mount(f.paths.AgentDir, "ro,relatime", "ext4")
			f.installCompact(t, testSession, true)
		}, strings.Repeat("e", 32)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCompanion(t)
			tt.prep(t, f)
			if err := f.check(t); err != nil {
				t.Fatal(err)
			}
			run := filepath.Join(f.paths.HADir, "run")
			if info, err := os.Stat(run); err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("run directory: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(run, "companion.json"))
			if err != nil {
				t.Fatal(err)
			}
			var evidence companionEvidence
			if err := json.Unmarshal(raw, &evidence); err != nil {
				t.Fatal(err)
			}
			self, _ := namespace("/proc/self/ns", "net")
			if evidence.Protocol != 1 || evidence.Mode != "host" || evidence.HostID != hexDigest([]byte(tt.identity)) ||
				evidence.CapEff != "0000000000003000" || evidence.Network != self || evidence.PID.Inode == 0 {
				t.Fatalf("evidence %+v", evidence)
			}
			entries, _ := os.ReadDir(run)
			if len(entries) != 1 {
				t.Errorf("run directory holds %d entries", len(entries))
			}
		})
	}
}

func TestCompanionRefusals(t *testing.T) {
	installation := func(f *companionFixture) {
		f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "installation"
		f.env["INSTALL_SCOPE"] = testSession
		f.mount(f.paths.AgentDir, "ro,relatime", "ext4")
	}
	tests := []struct {
		name string
		prep func(t *testing.T, f *companionFixture)
		want string
	}{
		{"unknown mode", func(_ *testing.T, f *companionFixture) { f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "host" }, msgCompanionMode},
		{"short identity", func(t *testing.T, f *companionFixture) {
			os.Chmod(f.paths.HostIDFile, 0o644)
			writeFile(t, f.paths.HostIDFile, "short", 0o644)
		}, msgCompanionIdentity},
		{"volume not read-only", func(t *testing.T, f *companionFixture) {
			f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "volume"
			f.mount(f.paths.IdentityDir, "rw,relatime", "ext4")
		}, msgCompanionVolume},
		{"volume identity is a symlink", func(t *testing.T, f *companionFixture) {
			f.env["CONFIGFLOW_DNS_HA_IDENTITY"] = "volume"
			f.mount(f.paths.IdentityDir, "ro", "ext4")
			os.Symlink(f.paths.HostIDFile, filepath.Join(f.paths.IdentityDir, "host-id"))
		}, msgCompanionVolumeFile},
		{"bad scope", func(_ *testing.T, f *companionFixture) {
			installation(f)
			f.env["INSTALL_SCOPE"] = "x"
		}, msgCompanionScope},
		{"agent directory writable", func(_ *testing.T, f *companionFixture) {
			installation(f)
			f.mounts = nil
			f.mount(f.paths.AgentDir, "rw", "ext4")
		}, msgCompanionAgentMount},
		{"agent directory on tmpfs", func(_ *testing.T, f *companionFixture) {
			installation(f)
			f.mounts = nil
			f.mount(f.paths.AgentDir, "ro", "tmpfs")
		}, msgCompanionAgentMount},
		{"first mount entry decides", func(_ *testing.T, f *companionFixture) {
			installation(f)
			f.mounts = nil
			f.mount(f.paths.AgentDir, "rw", "ext4")
			f.mount(f.paths.AgentDir, "ro", "ext4")
		}, msgCompanionAgentMount},
		{"record of another scope", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, strings.Repeat("c", 32), true)
		}, msgCompanionRecordForeign},
		{"record damaged", func(t *testing.T, f *companionFixture) {
			installation(f)
			writeFile(t, filepath.Join(f.paths.AgentDir, compactMarker), "{", 0o600)
		}, msgCompanionRecordInvalid},
		{"record is a symlink", func(t *testing.T, f *companionFixture) {
			installation(f)
			os.Symlink(f.paths.HostIDFile, filepath.Join(f.paths.AgentDir, compactMarker))
		}, "missing, unreadable or a symlink"},
		{"readiness not boolean", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, testSession, true)
			record := readObject(t, filepath.Join(f.paths.AgentDir, compactMarker))
			record["ready"] = "yes"
			raw, _ := json.Marshal(record)
			writeFile(t, filepath.Join(f.paths.AgentDir, compactMarker), string(raw), 0o600)
		}, msgCompanionReadiness},
		{"never ready", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, testSession, false)
		}, msgCompanionTimeout},
		{"no record yet", func(_ *testing.T, f *companionFixture) { installation(f) }, msgCompanionTimeout},
		{"core directory of another installation", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, testSession, true)
			writeFile(t, filepath.Join(f.paths.CoreDir, compactMarker), `{"protocol":2,"owner":"x","installation_id":"y"}`, 0o600)
		}, msgCompanionDataForeign},
		{"HA directory is a symlink", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, testSession, true)
			os.RemoveAll(f.paths.HADir)
			os.Symlink(f.paths.CoreDir, f.paths.HADir)
		}, msgCompanionDataSymlink},
		{"identity differs from record", func(t *testing.T, f *companionFixture) {
			installation(f)
			f.installCompact(t, testSession, true)
			writeFile(t, filepath.Join(f.paths.AgentDir, compactIdentity), strings.Repeat("f", 32), 0o600)
		}, msgCompanionIdentityRecord},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCompanion(t)
			tt.prep(t, f)
			err := f.check(t)
			if err == nil || !strings.Contains(err.Error(), tt.want) && err.Error() != tt.want {
				t.Fatalf("%v, want %q", err, tt.want)
			}
			if tt.want != "missing, unreadable or a symlink" && message(err) != tt.want {
				t.Fatalf("%q, want %q", message(err), tt.want)
			}
			if present(filepath.Join(f.paths.HADir, "run", "companion.json")) {
				t.Error("refusal published evidence")
			}
		})
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/a\040b\011c\012d\134e`); got != "/a b\tc\nd\\e" {
		t.Fatalf("%q", got)
	}
}
