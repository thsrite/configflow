package dockerha

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBindingOwnerCanonicalForm(t *testing.T) {
	tests := []struct {
		name    string
		binding map[string]any
		want    string
	}{
		// Digests of the records earlier images wrote (sorted keys, compact
		// separators, non-ASCII escaped); recorded once from the former script.
		{"plain", map[string]any{"server_url": "http://192.0.2.10:5010", "name": "dns-a", "ip": "192.0.2.21", "port": 8082, "network": "host", "session": strings.Repeat("a", 32)},
			"12a0884e1dff0ba9a7737bc7eee1898e2fe8e82a0cf4283292536b4ba5d40a78"},
		{"escapes", map[string]any{"server_url": "https://例え.example/<p>&\"q\\ 😀", "name": "x", "ip": "192.0.2.21", "port": json.Number("1"), "network": "macvlan", "session": strings.Repeat("b", 32)},
			"ae5f724570d3a17c4062661e97ea31ea78967a086c2f4f7cd399b7ea4dac182c"},
	}
	for _, tt := range tests {
		got, ok := bindingOwner(tt.binding)
		if !ok || got != tt.want {
			t.Errorf("%s: %s %t", tt.name, got, ok)
		}
	}
	for _, value := range []any{json.Number("1.0"), true, nil, []any{}, map[string]any{}} {
		if _, ok := bindingOwner(map[string]any{"port": value}); ok {
			t.Errorf("%#v accepted", value)
		}
	}
}

func compactEnv(values map[string]string) func(string) (string, bool) {
	base := map[string]string{"SERVICE_TYPE": "oxidns-ha-host", "SERVER_URL": "http://192.0.2.10:5010/", "AGENT_IP": "192.0.2.21",
		"AGENT_PORT": "8082", "AGENT_NAME": "dns-a", "INSTALL_KEY": testSession + "." + testToken}
	for key, value := range values {
		if value == "<unset>" {
			delete(base, key)
		} else {
			base[key] = value
		}
	}
	return func(key string) (string, bool) {
		value, ok := base[key]
		return value, ok
	}
}

func TestCompactSettings(t *testing.T) {
	got, err := compactSettings(compactEnv(nil))
	want := CompactSettings{ServerURL: "http://192.0.2.10:5010", Session: testSession, Token: testToken, Name: "dns-a", IP: "192.0.2.21", Port: 8082, Network: "host"}
	if err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := compactSettings(compactEnv(map[string]string{"AGENT_PORT": "<unset>", "INSTALL_KEY": "", "SERVICE_TYPE": "oxidns-ha-macvlan"})); err != nil || got.Port != 8082 || got.Session != "" || got.Network != "macvlan" {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	refusals := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"SERVICE_TYPE": "oxidns"}, msgCompactRole},
		{map[string]string{"SERVICE_TYPE": "<unset>"}, msgCompactRole},
		{map[string]string{"SERVER_URL": "ftp://192.0.2.10"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://user@192.0.2.10"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://192.0.2.10/?a=1"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://192.0.2.10/#x"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://192.0.2.10/a b"}, msgCompactAddress},
		{map[string]string{"SERVER_URL": "http://192.0.2.10/\x1c"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "0.0.0.0"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "127.0.0.2"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "224.0.0.5"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "255.255.255.255"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "2001:db8::1"}, msgCompactAddress},
		{map[string]string{"AGENT_IP": "192.0.2.021"}, msgCompactAddress},
		{map[string]string{"AGENT_PORT": "0"}, msgCompactAddress},
		{map[string]string{"AGENT_PORT": "65536"}, msgCompactAddress},
		{map[string]string{"AGENT_PORT": "x"}, msgCompactAddress},
		{map[string]string{"AGENT_NAME": "dns a"}, msgCompactName},
		{map[string]string{"AGENT_NAME": ""}, msgCompactName},
		{map[string]string{"INSTALL_KEY": testSession}, msgCompactKey},
		{map[string]string{"INSTALL_KEY": strings.ToUpper(testSession) + "." + testToken}, msgCompactKey},
	}
	for _, tt := range refusals {
		if _, err := compactSettings(compactEnv(tt.env)); message(err) != tt.want {
			t.Errorf("%v: %q, want %q", tt.env, message(err), tt.want)
		}
	}
}

func compactFixture(t *testing.T) CompactPaths {
	t.Helper()
	root := t.TempDir()
	paths := CompactPaths{AgentDir: filepath.Join(root, "agent"), CoreDir: filepath.Join(root, "oxidns"), HADir: filepath.Join(root, "ha")}
	for _, dir := range []string{paths.AgentDir, paths.CoreDir, paths.HADir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func compactSettingsFor(t *testing.T, o *origin, values map[string]string) CompactSettings {
	t.Helper()
	env := map[string]string{"SERVER_URL": o.server.URL}
	for key, value := range values {
		env[key] = value
	}
	settings, err := compactSettings(compactEnv(env))
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestCompactFirstInstallThenRestarts(t *testing.T) {
	o := newOrigin(t)
	paths := compactFixture(t)
	settings := compactSettingsFor(t, o, nil)
	var out bytes.Buffer
	if err := RunCompact(context.Background(), settings, paths, BundleClient(), &out); err != nil {
		t.Fatal(err)
	}
	if want := msgIdentitySaved + "\n" + msgCompactStarting + "\n"; out.String() != want {
		t.Fatalf("output %q", out.String())
	}
	marker := readObject(t, filepath.Join(paths.AgentDir, compactMarker))
	owner, _ := bindingOwner(map[string]any{"server_url": o.server.URL, "name": "dns-a", "ip": "192.0.2.21", "port": 8082, "network": "host", "session": testSession})
	if marker["owner"] != owner || marker["ready"] != true || marker["protocol"] != 2.0 || !hex32.MatchString(marker["installation_id"].(string)) {
		t.Fatalf("marker %v", marker)
	}
	id := marker["installation_id"].(string)
	for _, dir := range []string{paths.CoreDir, paths.HADir} {
		record := readObject(t, filepath.Join(dir, compactMarker))
		if !reflect.DeepEqual(record, map[string]any{"protocol": 2.0, "owner": owner, "installation_id": id}) {
			t.Errorf("%s record %v", dir, record)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(paths.AgentDir, compactIdentity))
	if string(raw) != id+"\n" {
		t.Errorf("identity %q", raw)
	}
	if calls := o.recorded(); len(calls) != 1 || !strings.Contains(calls[0], "/api/agents/docker-oxidns-ha-enroll/"+testSession+" Bearer "+testToken) {
		t.Fatalf("calls %v", calls)
	}
	config := readObject(t, filepath.Join(paths.AgentDir, agentConfig))
	if config["agent_id"] != testAgentID {
		t.Fatalf("config %v", config)
	}
	// The receipt is named after the owner while being written; no residue.
	entries, _ := os.ReadDir(paths.AgentDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".compose-write-") || strings.HasPrefix(entry.Name(), ".compose-enroll-") {
			t.Errorf("residue %s", entry.Name())
		}
	}

	// Later starts: the key may be removed; the origin may be down.
	o.set(500, "", "")
	for _, values := range []map[string]string{nil, {"INSTALL_KEY": ""}} {
		out.Reset()
		if err := RunCompact(context.Background(), compactSettingsFor(t, o, values), paths, BundleClient(), &out); err != nil {
			t.Fatalf("restart %v: %v", values, err)
		}
		if want := msgIdentityVerified + "\n" + msgCompactStarting + "\n"; out.String() != want {
			t.Fatalf("restart output %q", out.String())
		}
	}
	if calls := o.recorded(); len(calls) != 1 {
		t.Fatalf("restart enrolled again: %v", calls)
	}
}

func TestCompactRefusals(t *testing.T) {
	tests := []struct {
		name   string
		prep   func(t *testing.T, paths CompactPaths, o *origin) map[string]string
		want   string
		before bool // refused before installing (no marker written)
	}{
		{"foreign data in the core directory", func(t *testing.T, p CompactPaths, _ *origin) map[string]string {
			writeFile(t, filepath.Join(p.CoreDir, "config.yaml"), "x", 0o600)
			return nil
		}, msgCompactHasData, true},
		{"first install without key", func(t *testing.T, _ CompactPaths, _ *origin) map[string]string {
			return map[string]string{"INSTALL_KEY": ""}
		}, msgCompactNeedsKey, true},
		{"data directory is a symlink", func(t *testing.T, p CompactPaths, _ *origin) map[string]string {
			os.Remove(p.HADir)
			os.Symlink(p.CoreDir, p.HADir)
			return nil
		}, msgCompactNotPlain, true},
		{"data directory missing", func(t *testing.T, p CompactPaths, _ *origin) map[string]string {
			os.Remove(p.HADir)
			return nil
		}, msgCompactMissing, true},
		{"lock is a directory", func(t *testing.T, p CompactPaths, _ *origin) map[string]string {
			os.Mkdir(filepath.Join(p.AgentDir, compactLock), 0o700)
			return nil
		}, msgCompactUnreadable, true},
		{"marker is damaged", func(t *testing.T, p CompactPaths, _ *origin) map[string]string {
			writeFile(t, filepath.Join(p.AgentDir, compactMarker), "{", 0o600)
			return nil
		}, msgFileCorrupt, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOrigin(t)
			paths := compactFixture(t)
			values := tt.prep(t, paths, o)
			err := RunCompact(context.Background(), compactSettingsFor(t, o, values), paths, BundleClient(), &bytes.Buffer{})
			if message(err) != tt.want {
				t.Fatalf("%q, want %q", message(err), tt.want)
			}
			if tt.before && tt.name != "marker is damaged" && present(filepath.Join(paths.AgentDir, compactMarker)) {
				t.Error("refusal wrote the marker")
			}
			if len(o.recorded()) != 0 {
				t.Error("refusal enrolled")
			}
		})
	}
}

// installedCompact performs a first installation and returns its paths.
func installedCompact(t *testing.T) (CompactPaths, *origin) {
	t.Helper()
	o := newOrigin(t)
	paths := compactFixture(t)
	if err := RunCompact(context.Background(), compactSettingsFor(t, o, nil), paths, BundleClient(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return paths, o
}

func TestCompactRefusalsAfterInstall(t *testing.T) {
	tests := []struct {
		name string
		prep func(t *testing.T, paths CompactPaths) map[string]string
		want string
	}{
		{"agent name changed", func(*testing.T, CompactPaths) map[string]string { return map[string]string{"AGENT_NAME": "dns-b"} }, msgCompactForeignRecord},
		{"network changed", func(*testing.T, CompactPaths) map[string]string {
			return map[string]string{"SERVICE_TYPE": "oxidns-ha-macvlan"}
		}, msgCompactForeignRecord},
		{"new key for another scope", func(*testing.T, CompactPaths) map[string]string {
			return map[string]string{"INSTALL_KEY": strings.Repeat("c", 32) + "." + testToken}
		}, msgCompactForeignRecord},
		// The owner digest is unchanged but the stored binding was edited.
		{"stored binding tampered", func(t *testing.T, p CompactPaths) map[string]string {
			record := readObject(t, filepath.Join(p.AgentDir, compactMarker))
			record["binding"].(map[string]any)["name"] = "dns-b"
			raw, _ := json.Marshal(record)
			writeFile(t, filepath.Join(p.AgentDir, compactMarker), string(raw), 0o600)
			return nil
		}, msgCompactBinding},
		{"core record lost", func(t *testing.T, p CompactPaths) map[string]string {
			os.Remove(filepath.Join(p.CoreDir, compactMarker))
			return nil
		}, msgCompactRecordLost},
		{"identity lost", func(t *testing.T, p CompactPaths) map[string]string {
			os.Remove(filepath.Join(p.AgentDir, compactIdentity))
			return nil
		}, msgCompactIdentityLost},
		{"identity differs", func(t *testing.T, p CompactPaths) map[string]string {
			writeFile(t, filepath.Join(p.AgentDir, compactIdentity), strings.Repeat("0", 32)+"\n", 0o600)
			return nil
		}, msgCompactIdentityDiffers},
		{"HA record of another installation", func(t *testing.T, p CompactPaths) map[string]string {
			record := readObject(t, filepath.Join(p.HADir, compactMarker))
			record["installation_id"] = strings.Repeat("0", 32)
			raw, _ := json.Marshal(record)
			writeFile(t, filepath.Join(p.HADir, compactMarker), string(raw), 0o600)
			return nil
		}, msgCompactForeignRecord},
		{"marker owner tampered", func(t *testing.T, p CompactPaths) map[string]string {
			record := readObject(t, filepath.Join(p.AgentDir, compactMarker))
			record["owner"] = testOwner
			raw, _ := json.Marshal(record)
			writeFile(t, filepath.Join(p.AgentDir, compactMarker), string(raw), 0o600)
			return nil
		}, msgCompactForeignRecord},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, o := installedCompact(t)
			_, err := PrepareCompact(compactSettingsFor(t, o, tt.prep(t, paths)), paths)
			if message(err) != tt.want {
				t.Fatalf("%q, want %q", message(err), tt.want)
			}
		})
	}
}

func TestCompactResumesInterruptedInstall(t *testing.T) {
	o := newOrigin(t)
	paths := compactFixture(t)
	settings := compactSettingsFor(t, o, nil)
	if _, err := PrepareCompact(settings, paths); err != nil {
		t.Fatal(err)
	}
	marker := readObject(t, filepath.Join(paths.AgentDir, compactMarker))
	owner := marker["owner"].(string)
	// An interrupted first start: ready=false, the HA record not yet written,
	// an owner-named residue and the identity already published.
	marker["ready"] = false
	raw, _ := json.Marshal(marker)
	writeFile(t, filepath.Join(paths.AgentDir, compactMarker), string(raw), 0o600)
	os.Remove(filepath.Join(paths.HADir, compactMarker))
	writeFile(t, filepath.Join(paths.HADir, ownerTempPrefix(owner)+"abc_1234"), "{", 0o600)
	if _, err := PrepareCompact(settings, paths); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if record := readObject(t, filepath.Join(paths.AgentDir, compactMarker)); record["ready"] != true || record["installation_id"] != marker["installation_id"] {
		t.Fatalf("marker after resume %v", record)
	}
	// Unknown data during a pending installation is refused.
	marker["ready"] = false
	raw, _ = json.Marshal(marker)
	writeFile(t, filepath.Join(paths.AgentDir, compactMarker), string(raw), 0o600)
	writeFile(t, filepath.Join(paths.CoreDir, "config.yaml"), "x", 0o600)
	if _, err := PrepareCompact(settings, paths); message(err) != msgCompactPendingData {
		t.Fatalf("pending with data: %v", err)
	}
}

func TestCompactEnvironment(t *testing.T) {
	got := CompactEnvironment([]string{"PATH=/bin", "INSTALL_KEY=x.y", "SERVICE_TYPE=oxidns-ha-host", "ENABLE_MOSDNS=true", "TZ=UTC"}, "host")
	want := []string{"PATH=/bin", "TZ=UTC", "SERVICE_TYPE=oxidns", "CONFIGFLOW_DNS_HA=1", "CONFIGFLOW_DNS_HA_NETWORK=host",
		"CONFIGFLOW_DNS_HA_IDENTITY=installation", "ENABLE_MIHOMO=false", "ENABLE_MOSDNS=false", "ENABLE_OXIDNS=true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}
