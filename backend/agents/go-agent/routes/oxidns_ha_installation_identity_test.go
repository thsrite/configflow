package routes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func haTestInstallationIdentity(t *testing.T, f *haContainerFixture) string {
	t.Helper()
	t.Setenv("CONFIGFLOW_DNS_HA_IDENTITY", "installation")
	directory := filepath.Dir(f.cfg.AgentConfigFile)
	haWriteTestInstallation(t, directory, "0123456789abcdef0123456789abcdef")
	haTestBootProof(t, f)
	return directory
}

func haWriteTestInstallation(t *testing.T, directory, identity string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, haInstallationIdentityFile), []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"protocol": 2, "ready": true, "installation_id": identity})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, haInstallationMarkerFile), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHAInstallationIdentitySupportsHostAndMacvlanWithBootProof(t *testing.T) {
	for _, mode := range []string{"host", "macvlan"} {
		t.Run(mode, func(t *testing.T) {
			f := newHAContainerFixture(t, mode)
			haTestInstallationIdentity(t, f)
			env := haEnvironment(f.cfg)
			if !env.Ready || env.HostIDSource != "installation" || env.HostID != haHostIdentity(haHostIDPath) || env.HostBootID == "" {
				t.Fatalf("valid compact installation rejected: %+v", env)
			}
			if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHAInstallationIdentityRejectsMissingInvalidAndMovedProofs(t *testing.T) {
	for _, bad := range []string{"unmounted", "readonly", "tmpfs", "missing-id", "id-symlink", "id-directory", "invalid-id", "multiline-id", "oversize-id", "missing-marker", "marker-symlink", "marker-directory", "broken-marker", "oversize-marker", "unknown-protocol", "not-ready", "other-installation"} {
		t.Run(bad, func(t *testing.T) {
			f := newHAContainerFixture(t, "host")
			directory := haTestInstallationIdentity(t, f)
			identity := filepath.Join(directory, haInstallationIdentityFile)
			marker := filepath.Join(directory, haInstallationMarkerFile)
			switch bad {
			case "unmounted":
				f.files["/proc/self/mountinfo"] = []byte(strings.ReplaceAll(string(f.files["/proc/self/mountinfo"]), directory+" rw", "/not-agent-data rw"))
			case "readonly":
				f.files["/proc/self/mountinfo"] = []byte(strings.ReplaceAll(string(f.files["/proc/self/mountinfo"]), directory+" rw", directory+" ro"))
			case "tmpfs":
				f.files["/proc/self/mountinfo"] = []byte(strings.ReplaceAll(string(f.files["/proc/self/mountinfo"]), directory+" rw - ext4", directory+" rw - tmpfs"))
			case "missing-id":
				os.Remove(identity)
			case "id-symlink":
				os.Rename(identity, identity+"-actual")
				os.Symlink(identity+"-actual", identity)
			case "id-directory":
				os.Remove(identity)
				os.Mkdir(identity, 0700)
			case "invalid-id":
				os.WriteFile(identity, []byte("not-a-generated-installation-id"), 0600)
			case "multiline-id":
				os.WriteFile(identity, []byte("0123456789abcdef\n0123456789abcdef"), 0600)
			case "oversize-id":
				os.WriteFile(identity, []byte(strings.Repeat("a", 129)), 0600)
			case "missing-marker":
				os.Remove(marker)
			case "marker-symlink":
				os.Rename(marker, marker+"-actual")
				os.Symlink(marker+"-actual", marker)
			case "marker-directory":
				os.Remove(marker)
				os.Mkdir(marker, 0700)
			case "broken-marker":
				os.WriteFile(marker, []byte("{"), 0600)
			case "oversize-marker":
				os.WriteFile(marker, []byte(strings.Repeat(" ", 65537)), 0600)
			case "unknown-protocol":
				os.WriteFile(marker, []byte(`{"protocol":1,"ready":true,"installation_id":"0123456789abcdef0123456789abcdef"}`), 0600)
			case "not-ready":
				os.WriteFile(marker, []byte(`{"protocol":2,"ready":false,"installation_id":"0123456789abcdef0123456789abcdef"}`), 0600)
			case "other-installation":
				os.WriteFile(marker, []byte(`{"protocol":2,"ready":true,"installation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), 0600)
			}
			if env := haEnvironment(f.cfg); env.Ready || env.HostID != "" {
				t.Fatalf("unproven installation %s accepted: %+v", bad, env)
			}
			if err := applyOxiDNSHA(f.cfg, f.spec); err == nil || len(f.commands) != 0 {
				t.Fatalf("invalid installation mutated services: %v %v", err, f.commands)
			}
		})
	}
}

func TestHAInstallationIdentityRequiresRealReadonlyBootProof(t *testing.T) {
	for _, bad := range []string{"unmounted", "writable", "missing", "symlink", "invalid"} {
		t.Run(bad, func(t *testing.T) {
			f := newHAContainerFixture(t, "host")
			haTestInstallationIdentity(t, f)
			path := haTestBootProof(t, f)
			switch bad {
			case "unmounted":
				f.files["/proc/self/mountinfo"] = []byte(strings.ReplaceAll(string(f.files["/proc/self/mountinfo"]), haHostBootIDPath+" ro", "/unproven-boot ro"))
			case "writable":
				f.files["/proc/self/mountinfo"] = []byte(strings.ReplaceAll(string(f.files["/proc/self/mountinfo"]), haHostBootIDPath+" ro", haHostBootIDPath+" rw"))
			case "missing":
				os.Remove(path)
			case "symlink":
				os.Rename(path, path+"-actual")
				os.Symlink(path+"-actual", path)
			case "invalid":
				os.WriteFile(path, []byte("not-a-host-boot-id"), 0600)
			}
			env := haEnvironment(f.cfg)
			if env.Ready || env.HostBootID != "" || env.HostIDSource != "installation" {
				t.Fatalf("installation accepted missing host proof %s: %+v", bad, env)
			}
			if err := applyOxiDNSHA(f.cfg, f.spec); err == nil || len(f.commands) != 0 {
				t.Fatalf("missing boot proof mutated services: %v %v", err, f.commands)
			}
		})
	}
}

func TestHAInstallationJournalSurvivesRebootButRejectsReplacementIdentity(t *testing.T) {
	f := newHAContainerFixture(t, "macvlan")
	directory := haTestInstallationIdentity(t, f)
	path := haTestBootProof(t, f)
	before := haEnvironment(f.cfg)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("9e4ba5a2-2c84-4cb9-a63a-2a57a48d23b3\n"), 0600)
	f.running = false
	if err := reconcileOxiDNSHA(f.cfg); err != nil || !f.running {
		t.Fatalf("reboot rejected existing installation: %v", err)
	}
	if after := haEnvironment(f.cfg); !after.Ready || before.HostID != after.HostID || before.HostBootID == after.HostBootID {
		t.Fatalf("reboot changed installation ownership: %+v", after)
	}
	haWriteTestInstallation(t, directory, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if env := haEnvironment(f.cfg); !env.Ready || env.HostID == before.HostID {
		t.Fatalf("replacement fixture is not a different valid installation: %+v", env)
	}
	commands := len(f.commands)
	if err := reconcileOxiDNSHA(f.cfg); err == nil || len(f.commands) != commands {
		t.Fatalf("replacement installation silently adopted old journal: %v %v", err, f.commands[commands:])
	}
}
