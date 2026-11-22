package dockerha

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directSettingsText(mode, url string) string {
	raw, _ := json.Marshal(map[string]any{"protocol": 1, "owner": testOwner, "url": url, "identity_mode": mode, "token": testToken})
	return string(raw)
}

func TestDirectSettings(t *testing.T) {
	if settings, err := directSettings(directSettingsText("file", "http://192.0.2.10/x")); err != nil || settings["owner"] != testOwner {
		t.Fatalf("%v %v", settings, err)
	}
	tests := []struct {
		raw, want string
	}{
		{strings.Repeat(" ", directSettingsLimit+1), msgDirectSettingsTooLarge},
		{"", msgDirectSettingsInvalid},
		{"{", msgDirectSettingsInvalid},
		{"[]", msgDirectSettingsProtocol},
		{`{"protocol":2,"owner":"` + testOwner + `","url":"u","identity_mode":"file"}`, msgDirectSettingsProtocol},
		{`{"protocol":1,"owner":"AB","url":"u","identity_mode":"file"}`, msgDirectSettingsProtocol},
		{`{"protocol":1,"owner":"` + testOwner + `","url":"u","identity_mode":"volume"}`, msgDirectSettingsProtocol},
		{`{"protocol":1,"owner":"` + testOwner + `","url":1,"identity_mode":"file"}`, msgDirectSettingsProtocol},
	}
	for _, tt := range tests {
		if _, err := directSettings(tt.raw); message(err) != tt.want {
			t.Errorf("%.40q: %q, want %q", tt.raw, message(err), tt.want)
		}
	}
	// The limit counts characters, not bytes.
	if _, err := directSettings(`{"protocol":1,"owner":"` + testOwner + `","url":"` + strings.Repeat("例", 5000) + `","identity_mode":"file"}`); err != nil {
		t.Errorf("multi-byte settings under the limit: %v", err)
	}
}

type directFixture struct {
	paths    DirectPaths
	settings map[string]any
}

func newDirect(t *testing.T, mode, url string) directFixture {
	t.Helper()
	root := t.TempDir()
	paths := DirectPaths{DataRoot: filepath.Join(root, "data"), IdentityDir: filepath.Join(root, "identity"),
		IdentityFile: filepath.Join(root, "machine-id")}
	paths.AgentDir = filepath.Join(paths.DataRoot, "agent")
	os.Mkdir(paths.DataRoot, 0o755)
	os.Mkdir(paths.IdentityDir, 0o755)
	writeFile(t, paths.IdentityFile, "0123456789abcdef0123456789abcdef\n", 0o444)
	settings, err := directSettings(directSettingsText(mode, url))
	if err != nil {
		t.Fatal(err)
	}
	return directFixture{paths, settings}
}

func TestDirectPrepareAndRun(t *testing.T) {
	for _, mode := range []string{"file", "docker-volume"} {
		t.Run(mode, func(t *testing.T) {
			o := newOrigin(t)
			f := newDirect(t, mode, o.url())
			for i := 0; i < 2; i++ {
				var out bytes.Buffer
				if err := PrepareDirect(f.settings, f.paths, &out); err != nil || out.String() != msgDirectPrepared+"\n" {
					t.Fatalf("prepare %d: %v %q", i, err, out.String())
				}
			}
			marker := readObject(t, filepath.Join(f.paths.DataRoot, directMarker))
			setup := readObject(t, filepath.Join(f.paths.AgentDir, directMarker))
			if marker["ready"] != true || setup["ready"] != true || marker["host_id"] != setup["host_id"] || marker["identity_mode"] != mode {
				t.Fatalf("marker %v setup %v", marker, setup)
			}
			if mode == "docker-volume" {
				raw, err := os.ReadFile(filepath.Join(f.paths.IdentityDir, "host-id"))
				if err != nil || len(raw) != 33 {
					t.Fatalf("host-id %q %v", raw, err)
				}
				if marker["host_id"] != hexDigest(raw[:32]) {
					t.Fatal("host_id is not the digest of the created identity")
				}
			}
			var out bytes.Buffer
			if err := RunDirect(context.Background(), f.settings, f.paths, BundleClient(), &out); err != nil || out.String() != msgIdentitySaved+"\n" {
				t.Fatalf("run: %v %q", err, out.String())
			}
			out.Reset()
			if err := RunDirect(context.Background(), f.settings, f.paths, BundleClient(), &out); err != nil || out.String() != msgIdentityVerified+"\n" {
				t.Fatalf("second run: %v %q", err, out.String())
			}
			if calls := o.recorded(); len(calls) != 1 {
				t.Fatalf("calls %v", calls)
			}
		})
	}
}

func TestDirectRefusals(t *testing.T) {
	tests := []struct {
		name string
		prep func(t *testing.T, f directFixture)
		run  bool
		want string
	}{
		{"unknown file in data root", func(t *testing.T, f directFixture) {
			writeFile(t, filepath.Join(f.paths.DataRoot, "notes.txt"), "x", 0o600)
		}, false, msgDirectUnknown},
		{"old data in a subdirectory", func(t *testing.T, f directFixture) {
			os.Mkdir(filepath.Join(f.paths.DataRoot, "oxidns"), 0o700)
			writeFile(t, filepath.Join(f.paths.DataRoot, "oxidns", "config.yaml"), "x", 0o600)
		}, false, msgDirectSubdirFull},
		{"data root is a symlink", func(t *testing.T, f directFixture) {
			os.Remove(f.paths.DataRoot)
			os.Symlink(f.paths.IdentityDir, f.paths.DataRoot)
		}, false, msgDirectNotPlain},
		{"data root missing", func(t *testing.T, f directFixture) {
			os.Remove(f.paths.DataRoot)
		}, false, msgDirectMissing},
		{"short host identity", func(t *testing.T, f directFixture) {
			os.Chmod(f.paths.IdentityFile, 0o644)
			writeFile(t, f.paths.IdentityFile, "short\n", 0o644)
		}, false, msgDirectHostID},
		{"another installation's marker", func(t *testing.T, f directFixture) {
			writeFile(t, filepath.Join(f.paths.DataRoot, directMarker), `{"protocol":1,"owner":"`+strings.Repeat("cd", 32)+`","url":"u","identity_mode":"file"}`, 0o600)
		}, false, msgDirectForeign},
		{"run before prepare", func(*testing.T, directFixture) {}, true, msgFileUnreadable},
		{"run with another host identity", func(t *testing.T, f directFixture) {
			if err := PrepareDirect(f.settings, f.paths, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			os.Chmod(f.paths.IdentityFile, 0o644)
			writeFile(t, f.paths.IdentityFile, strings.Repeat("9", 32)+"\n", 0o644)
		}, true, msgDirectRunHost},
		{"prepare on another host", func(t *testing.T, f directFixture) {
			if err := PrepareDirect(f.settings, f.paths, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			os.Chmod(f.paths.IdentityFile, 0o644)
			writeFile(t, f.paths.IdentityFile, strings.Repeat("9", 32)+"\n", 0o644)
		}, false, msgDirectHostMismatch},
		{"setup record lost after completion", func(t *testing.T, f directFixture) {
			if err := PrepareDirect(f.settings, f.paths, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			os.Remove(filepath.Join(f.paths.AgentDir, directMarker))
		}, false, msgDirectSetupLost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOrigin(t)
			f := newDirect(t, "file", o.url())
			tt.prep(t, f)
			var err error
			if tt.run {
				err = RunDirect(context.Background(), f.settings, f.paths, BundleClient(), &bytes.Buffer{})
			} else {
				err = PrepareDirect(f.settings, f.paths, &bytes.Buffer{})
			}
			if message(err) != tt.want {
				t.Fatalf("%q, want %q", message(err), tt.want)
			}
			if len(o.recorded()) != 0 {
				t.Error("refusal enrolled")
			}
		})
	}
}

func TestDirectAcceptsOwnInterruptedWrites(t *testing.T) {
	o := newOrigin(t)
	f := newDirect(t, "file", o.url())
	writeFile(t, filepath.Join(f.paths.DataRoot, ownerTempPrefix(testOwner)+"zz_00000"), "{", 0o600)
	if err := PrepareDirect(f.settings, f.paths, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Someone else's residue is unknown data.
	f = newDirect(t, "file", o.url())
	writeFile(t, filepath.Join(f.paths.DataRoot, ownerTempPrefix(strings.Repeat("cd", 32))+"zz_00000"), "{", 0o600)
	if err := PrepareDirect(f.settings, f.paths, &bytes.Buffer{}); message(err) != msgDirectUnknown {
		t.Fatalf("foreign residue: %v", err)
	}
}

func TestComposeTemporaryNames(t *testing.T) {
	dir := t.TempDir()
	if err := composeFlavor.write(filepath.Join(dir, "a.json"), []field{{"owner", testOwner}}, false); err != nil {
		t.Fatal(err)
	}
	file, err := createTemp(dir, ownerTempPrefix(testOwner))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if owned, err := ownedTemporary(file.Name(), testOwner); err != nil || !owned {
		t.Fatalf("own temporary not recognised: %s %v", filepath.Base(file.Name()), err)
	}
}
