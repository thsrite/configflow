package autoupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/logx"
)

func TestMain(m *testing.M) {
	logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard})
	os.Exit(m.Run())
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// fakeGo is a Go toolchain stand-in: it writes "<GOARCH>/<GOARM>/<CGO_ENABLED>"
// to the -o file and appends the same line plus the package to calls.
func fakeGo(t *testing.T, fail bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then out="$a"; fi
  prev="$a"
done
echo "$GOARCH/$GOARM/$CGO_ENABLED/$GOTOOLCHAIN $(pwd)" >> "` + calls + `"
`
	if fail {
		script += "echo 'compile error: boom' >&2\nexit 1\n"
	} else {
		script += `printf '%s' "built:$GOARCH/$GOARM" > "$out"` + "\n"
	}
	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, calls
}

func callCount(t *testing.T, calls string) int {
	data, err := os.ReadFile(calls)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

type world struct {
	cfg  Config
	base string
}

// newWorld lays out an image (server binary, html, agents, node-probe).
func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	cfg := Config{
		Repo: "thsrite/configflow", Branch: "main", DataDir: filepath.Join(base, "data"),
		State: filepath.Join(base, "data", ".auto-update"), ServerBin: filepath.Join(base, "bin", "configflow"),
		HTML: filepath.Join(base, "html"), Agents: filepath.Join(base, "agents"), NodeProbe: filepath.Join(base, "node-probe"),
		ImageCopy: filepath.Join(base, "app", ".image"), ImageTrees: filepath.Join(base, "app", ".image-trees.json"),
		GoProxy: "off", Machine: "x86_64", Enabled: true,
	}
	write(t, cfg.ServerBin, "image server")
	write(t, filepath.Join(cfg.HTML, "index.html"), "image html")
	write(t, filepath.Join(cfg.Agents, "configflow-agent-linux-amd64"), "image agent")
	write(t, filepath.Join(cfg.NodeProbe, "configflow-node-probe"), "image probe")
	os.MkdirAll(cfg.State, 0o755)
	return &world{cfg: cfg, base: base}
}

// sourceTree writes a minimal repository layout.
func sourceTree(t *testing.T, root string) {
	t.Helper()
	write(t, filepath.Join(root, "server", "go.mod"), "module configflow\n")
	write(t, filepath.Join(root, "server", "cmd", "configflow", "main.go"), "package main\n")
	write(t, filepath.Join(root, "server", "internal", "x", "x.go"), "package x\n")
	write(t, filepath.Join(root, "frontend", "package.json"), "{}\n")
	write(t, filepath.Join(root, "frontend", "package-lock.json"), "{}\n")
	write(t, filepath.Join(root, "frontend", "src", "main.ts"), "console.log(1)\n")
	write(t, filepath.Join(root, "backend", "agents", "go-agent", "go.mod"), "module agent\n")
	write(t, filepath.Join(root, "backend", "agents", "go-agent", "main.go"), "package main\n")
	write(t, filepath.Join(root, "backend", "node-probe", "go.mod"), "module probe\n")
	write(t, filepath.Join(root, "backend", "node-probe", "main.go"), "package main\n")
	write(t, filepath.Join(root, "backend", "node-probe", "LICENSE"), "license\n")
}

// recordAsImage records root's fingerprints as the image's.
func (w *world) recordAsImage(t *testing.T, root string) {
	t.Helper()
	image := filepath.Join(w.base, "image-src")
	os.RemoveAll(image)
	for from, to := range map[string]string{"server": "server", "frontend": "frontend", "backend/agents/go-agent": "go-agent", "backend/node-probe": "node-probe"} {
		if err := copyTree(filepath.Join(root, from), filepath.Join(image, to)); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Dir(w.cfg.ImageTrees), 0o755)
	if _, err := RecordImage(image, w.cfg.ImageTrees); err != nil {
		t.Fatal(err)
	}
}

func TestTreeHashSelectsBuildInputs(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.ts"), "a")
	write(t, filepath.Join(root, "sub", "b.ts"), "b")
	base, err := TreeHash(root, FrontendInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, ignored := range []string{"README.md", ".env", "node_modules/x.js", "dist/app.js", ".cache/y"} {
		write(t, filepath.Join(root, ignored), "ignored")
	}
	if got, _ := TreeHash(root, FrontendInput); got != base {
		t.Fatal("non-input files changed the fingerprint")
	}
	write(t, filepath.Join(root, "sub", "b.ts"), "changed")
	if got, _ := TreeHash(root, FrontendInput); got == base {
		t.Fatal("input change kept the fingerprint")
	}
	// Renaming a file changes the fingerprint even with identical content.
	os.Rename(filepath.Join(root, "a.ts"), filepath.Join(root, "c.ts"))
	write(t, filepath.Join(root, "sub", "b.ts"), "b")
	if got, _ := TreeHash(root, FrontendInput); got == base {
		t.Fatal("rename kept the fingerprint")
	}

	src := t.TempDir()
	sourceTree(t, src)
	srv := filepath.Join(src, "server")
	key, err := ServerTreeHash(srv)
	if err != nil {
		t.Fatal(err)
	}
	for _, ignored := range []string{"internal/x/x_test.go", "internal/x/testdata/f.go", "internal/difftest/d.go", "PORTING.md", "tools/t.go", "internal/x/r.py", "scripts/a.sh"} {
		write(t, filepath.Join(srv, ignored), "ignored")
	}
	if got, _ := ServerTreeHash(srv); got != key {
		t.Fatal("tests/tools changed the server fingerprint")
	}
	write(t, filepath.Join(srv, "internal", "x", "embed.json"), "{}")
	if got, _ := ServerTreeHash(srv); got == key {
		t.Fatal("an embedded asset did not change the server fingerprint")
	}
}

// Fingerprints are pinned: a change here makes every existing autoupdate
// container rebuild once (the values match docker/auto_update.py).
func TestTreeHashPinned(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "b.go"), "package b\n")
	write(t, filepath.Join(root, "go.mod"), "module m\n")
	write(t, filepath.Join(root, "z", "a.go"), "package a\n")
	got, err := TreeHash(root, GoModuleInput)
	if err != nil {
		t.Fatal(err)
	}
	if got != pinnedModuleHash {
		t.Fatalf("fingerprint %s, want %s", got, pinnedModuleHash)
	}
}

// pinnedModuleHash was computed by docker/auto_update.py's tree_hash on the
// tree of TestTreeHashPinned.
const pinnedModuleHash = "3e3c450ca4a7fc1814219574206283b8171525a6147a339d57dcd552425fa7ed"

func TestPrecompressDir(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("console.log('configflow');\n", 100)
	write(t, filepath.Join(dir, "assets", "app.js"), big)
	write(t, filepath.Join(dir, "small.css"), "a{}")
	write(t, filepath.Join(dir, "logo.png"), big)
	if err := PrecompressDir(dir); err != nil {
		t.Fatal(err)
	}
	first := read(t, filepath.Join(dir, "assets", "app.js.gz"))
	zr, err := gzip.NewReader(strings.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != big || zr.Name != "" || !zr.ModTime.IsZero() && zr.ModTime.Unix() != 0 {
		t.Fatalf("gzip header name=%q mtime=%v", zr.Name, zr.ModTime)
	}
	for _, absent := range []string{"small.css.gz", "logo.png.gz"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s written", absent)
		}
	}
	if err := PrecompressDir(dir); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "assets", "app.js.gz")) != first {
		t.Fatal("precompression is not deterministic")
	}
}

func TestLoadManifest(t *testing.T) {
	w := newWorld(t)
	u := New(w.cfg)
	path := filepath.Join(w.cfg.State, "manifest.json")
	write(t, path, `{"sha": "abcdef1234", "source": "/x", "frontend": "/f"}`)
	if m := u.LoadManifest(); m.SHA != "" {
		t.Fatalf("Python-era manifest reused: %+v", m)
	}
	write(t, path, `not json`)
	if m := u.LoadManifest(); m.SHA != "" {
		t.Fatal("broken manifest read")
	}
	built := "/state/server-1"
	in := &Manifest{SHA: "abcdef1234", Source: "/state/src-abc", Server: &built}
	if err := u.saveManifest(in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), `"frontend": null`) {
		t.Fatalf("manifest %s", read(t, path))
	}
	out := u.LoadManifest()
	if out.SHA != in.SHA || out.Server == nil || *out.Server != built || out.Frontend != nil {
		t.Fatalf("round trip %+v", out)
	}
}

func TestLegacyPreSQLiteFile(t *testing.T) {
	dir := t.TempDir()
	if got := LegacyPreSQLiteFile(dir); got != "" {
		t.Fatal(got)
	}
	write(t, filepath.Join(dir, "profiles", "p2", "config.json"), "{}")
	write(t, filepath.Join(dir, "profiles", "p1", "config.json"), "{}")
	if got := LegacyPreSQLiteFile(dir); got != "profiles/p1/config.json" {
		t.Fatal(got)
	}
	write(t, filepath.Join(dir, "system.json"), "{}")
	if got := LegacyPreSQLiteFile(dir); got != "system.json" {
		t.Fatal(got)
	}
	write(t, filepath.Join(dir, databaseFilename), "")
	if got := LegacyPreSQLiteFile(dir); got != "" {
		t.Fatal(got)
	}
}

// test_node_probe_packaging.py::test_changed_probe_without_compiler_refuses_mixed_versions
func TestChangedProbeWithoutCompilerRefusesMixedVersions(t *testing.T) {
	w := newWorld(t)
	source := filepath.Join(w.base, "src")
	sourceTree(t, source)
	w.recordAsImage(t, source)
	w.cfg.GoBinary = ""
	w.cfg.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	u := New(w.cfg)
	if u.findGo() != "" {
		t.Skip("/usr/local/go exists on this host")
	}
	m := &Manifest{}
	if err := u.buildNodeProbe(context.Background(), source, m); err != nil || m.NodeProbe != nil {
		t.Fatalf("unchanged probe: %v %+v", err, m)
	}
	write(t, filepath.Join(source, "backend", "node-probe", "main.go"), "package main // changed\n")
	if err := u.buildNodeProbe(context.Background(), source, m); !errors.Is(err, ErrMissingToolchain) {
		t.Fatalf("err %v", err)
	}
}

// test_node_probe_packaging.py::test_probe_build_is_for_host_and_reused
func TestProbeBuildIsForHostAndReused(t *testing.T) {
	w := newWorld(t)
	source := filepath.Join(w.base, "src")
	sourceTree(t, source)
	goBin, calls := fakeGo(t, false)
	w.cfg.GoBinary, w.cfg.Machine = goBin, "aarch64"
	u := New(w.cfg)
	m := &Manifest{}
	if err := u.buildNodeProbe(context.Background(), source, m); err != nil {
		t.Fatal(err)
	}
	if m.NodeProbe == nil || !strings.HasSuffix(*m.NodeProbe, "-aarch64") {
		t.Fatalf("manifest %+v", m)
	}
	if got := read(t, filepath.Join(*m.NodeProbe, "configflow-node-probe")); got != "built:arm64/" {
		t.Fatalf("binary %q", got)
	}
	if !strings.HasPrefix(read(t, calls), "arm64//0/local ") {
		t.Fatalf("calls %s", read(t, calls))
	}
	if read(t, filepath.Join(*m.NodeProbe, "LICENSE")) != "license\n" {
		t.Fatal("license not copied")
	}
	first := *m.NodeProbe
	if err := u.buildNodeProbe(context.Background(), source, m); err != nil || *m.NodeProbe != first || callCount(t, calls) != 1 {
		t.Fatalf("rebuild: %v %d", err, callCount(t, calls))
	}
}

func TestAgentsBuildAllArches(t *testing.T) {
	w := newWorld(t)
	source := filepath.Join(w.base, "src")
	sourceTree(t, source)
	goBin, _ := fakeGo(t, false)
	w.cfg.GoBinary = goBin
	m := &Manifest{}
	if err := New(w.cfg).buildAgents(context.Background(), source, m); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"amd64": "built:amd64/", "arm64": "built:arm64/", "armv7": "built:arm/7"} {
		if got := read(t, filepath.Join(*m.Agents, "configflow-agent-linux-"+name)); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// test_node_probe_packaging.py::test_restore_and_install_keep_worker_with_matching_sources
func TestRestoreAndInstallKeepWorkerWithMatchingSources(t *testing.T) {
	w := newWorld(t)
	u := New(w.cfg)
	if err := u.PreserveImage(); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(w.cfg.State, "node-probe-new")
	write(t, filepath.Join(built, "configflow-node-probe"), "new worker")
	server := filepath.Join(w.cfg.State, "server-new")
	write(t, filepath.Join(server, "configflow"), "new server")
	m := &Manifest{SHA: "1234567", Source: "/src", NodeProbe: &built, Server: &server}
	if ok, err := u.Install(m); !ok || err != nil {
		t.Fatal(ok, err)
	}
	probe := filepath.Join(w.cfg.NodeProbe, "configflow-node-probe")
	if read(t, probe) != "new worker" || read(t, w.cfg.ServerBin) != "new server" {
		t.Fatal("build not installed")
	}
	if info, _ := os.Stat(w.cfg.ServerBin); info.Mode().Perm() != 0o755 {
		t.Fatalf("server mode %v", info.Mode())
	}
	m.NodeProbe, m.Server = nil, nil
	if ok, err := u.Install(m); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if read(t, probe) != "image probe" || read(t, w.cfg.ServerBin) != "image server" {
		t.Fatal("image copy not used for components without a build")
	}
	write(t, probe, "tampered")
	if err := u.RestoreImage(); err != nil {
		t.Fatal(err)
	}
	if read(t, probe) != "image probe" {
		t.Fatal("restore")
	}
	if ok, _ := u.Install(&Manifest{}); ok {
		t.Fatal("install without sha reported a build")
	}
}

// test_node_probe_packaging.py::test_prune_keeps_current_worker
func TestPruneKeepsCurrentBuilds(t *testing.T) {
	w := newWorld(t)
	for _, name := range []string{"node-probe-current", "node-probe-old", "src-old", "deps-cur", "go-cache", "npm-cache", "x.tmp"} {
		os.MkdirAll(filepath.Join(w.cfg.State, name), 0o755)
	}
	write(t, filepath.Join(w.cfg.State, "abc.tar.gz"), "")
	current := filepath.Join(w.cfg.State, "node-probe-current")
	New(w.cfg).Prune(&Manifest{NodeProbe: &current, Deps: filepath.Join(w.cfg.State, "deps-cur")})
	entries, _ := os.ReadDir(w.cfg.State)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "deps-cur,go-cache,node-probe-current,npm-cache" {
		t.Fatalf("left %v", names)
	}
}

func tarGz(t *testing.T, entries []tar.Header, content map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, h := range entries {
		body := content[h.Name]
		h.Size = int64(len(body))
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(body))
		}
	}
	tw.Close()
	zw.Close()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	os.WriteFile(path, buf.Bytes(), 0o644)
	return path
}

func TestExtractTarGzDataFilter(t *testing.T) {
	good := tarGz(t, []tar.Header{
		{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "abc"}},
		{Typeflag: tar.TypeDir, Name: "repo-abc/", Mode: 0o755},
		{Typeflag: tar.TypeReg, Name: "repo-abc/run.sh", Mode: 0o4777},
		{Typeflag: tar.TypeSymlink, Name: "repo-abc/link", Linkname: "run.sh"},
	}, map[string]string{"repo-abc/run.sh": "#!/bin/sh\n"})
	dest := filepath.Join(t.TempDir(), "out")
	if err := extractTarGz(good, dest); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 || entries[0].Name() != "repo-abc" {
		t.Fatalf("entries %v", entries)
	}
	info, _ := os.Stat(filepath.Join(dest, "repo-abc", "run.sh"))
	if info.Mode().Perm() != 0o755 || info.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("mode %v", info.Mode())
	}
	if read(t, filepath.Join(dest, "repo-abc", "link")) != "#!/bin/sh\n" {
		t.Fatal("symlink")
	}
	for name, h := range map[string]tar.Header{
		"parent":           {Typeflag: tar.TypeReg, Name: "../evil", Mode: 0o644},
		"absolute":         {Typeflag: tar.TypeReg, Name: "/evil", Mode: 0o644},
		"symlink-outside":  {Typeflag: tar.TypeSymlink, Name: "repo/l", Linkname: "../../etc/passwd"},
		"symlink-absolute": {Typeflag: tar.TypeSymlink, Name: "repo/l", Linkname: "/etc/passwd"},
		"device":           {Typeflag: tar.TypeChar, Name: "repo/dev", Mode: 0o644},
	} {
		err := extractTarGz(tarGz(t, []tar.Header{h}, nil), filepath.Join(t.TempDir(), "out"))
		if !errors.Is(err, ErrUnsafeArchive) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A local git repository as update source: build what changed, install,
// reuse, refuse to mix versions without a toolchain, keep the last good
// build on a failed build, and fall back when the source is unreachable.
func TestRunFromGitSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	w := newWorld(t)
	repo := filepath.Join(w.base, "repo")
	sourceTree(t, repo)
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "image")
	v1 := git(t, repo, "rev-parse", "HEAD")
	w.recordAsImage(t, repo)
	goBin, calls := fakeGo(t, false)
	w.cfg.Repo, w.cfg.GoBinary = repo, goBin
	w.cfg.LookPath = func(file string) (string, error) { // no npm
		if file == "git" {
			return exec.LookPath(file)
		}
		return "", exec.ErrNotFound
	}

	// 1. Same source as the image: nothing to build, image copies stay.
	New(w.cfg).Run(context.Background())
	m := New(w.cfg).LoadManifest()
	if m.SHA != git(t, repo, "rev-parse", "HEAD") || m.Server != nil || m.Frontend != nil || callCount(t, calls) != 0 {
		t.Fatalf("manifest %+v, %d builds", m, callCount(t, calls))
	}
	if read(t, w.cfg.ServerBin) != "image server" {
		t.Fatal("server replaced")
	}

	// 2. A server change is built and installed; the rest stays the image's.
	write(t, filepath.Join(repo, "server", "internal", "x", "x.go"), "package x // v2\n")
	git(t, repo, "commit", "-q", "-am", "server v2")
	v2 := git(t, repo, "rev-parse", "HEAD")
	New(w.cfg).Run(context.Background())
	m = New(w.cfg).LoadManifest()
	if m.SHA != v2 || m.Server == nil || read(t, w.cfg.ServerBin) != "built:amd64/" || callCount(t, calls) != 1 {
		t.Fatalf("v2 manifest %+v binary %q", m, read(t, w.cfg.ServerBin))
	}
	if !strings.Contains(read(t, calls), filepath.Join("src-"+v2, "server")) {
		t.Fatalf("built in %s", read(t, calls))
	}

	// 3. Unchanged remote: reinstall the recorded build, no rebuild.
	write(t, w.cfg.ServerBin, "image server") // a recreated container
	New(w.cfg).Run(context.Background())
	if read(t, w.cfg.ServerBin) != "built:amd64/" || callCount(t, calls) != 1 {
		t.Fatal("recorded build not reinstalled")
	}

	// 4. A frontend change without npm: keep v2 for every component.
	write(t, filepath.Join(repo, "frontend", "src", "main.ts"), "console.log(3)\n")
	git(t, repo, "commit", "-q", "-am", "frontend v3")
	v3 := git(t, repo, "rev-parse", "HEAD")
	New(w.cfg).Run(context.Background())
	if m := New(w.cfg).LoadManifest(); m.SHA != v2 || read(t, w.cfg.ServerBin) != "built:amd64/" {
		t.Fatalf("mixed versions: %+v", m)
	}

	// 5. A failing build keeps v2.
	write(t, filepath.Join(repo, "frontend", "src", "main.ts"), "console.log(1)\n")
	write(t, filepath.Join(repo, "server", "internal", "x", "x.go"), "package x // v4\n")
	git(t, repo, "commit", "-q", "-am", "server v4")
	w.cfg.GoBinary, _ = fakeGo(t, true)
	New(w.cfg).Run(context.Background())
	if m := New(w.cfg).LoadManifest(); m.SHA != v2 || read(t, w.cfg.ServerBin) != "built:amd64/" {
		t.Fatalf("failed build replaced v2: %+v", m)
	}

	// 6. Unreachable source: keep v2.
	w.cfg.Repo = filepath.Join(w.base, "missing.git")
	write(t, w.cfg.ServerBin, "image server")
	New(w.cfg).Run(context.Background())
	if read(t, w.cfg.ServerBin) != "built:amd64/" {
		t.Fatal("offline start did not install the recorded build")
	}
	// Sources older than v2 were pruned (v1 by the v2 update, v3 by the
	// skipped update); the failed v4 source stays until the next success,
	// as before.
	for _, sha := range []string{v1, v3} {
		if _, err := os.Stat(filepath.Join(w.cfg.State, "src-"+sha)); err == nil {
			t.Errorf("source %s not pruned", sha)
		}
	}
	if _, err := os.Stat(filepath.Join(w.cfg.State, "src-"+v2)); err != nil {
		t.Error("v2 source pruned")
	}
}

func TestRunSkipsLegacyData(t *testing.T) {
	w := newWorld(t)
	write(t, filepath.Join(w.cfg.DataDir, "config.json"), "{}")
	u := New(w.cfg)
	if err := u.PreserveImage(); err != nil {
		t.Fatal(err)
	}
	write(t, w.cfg.ServerBin, "stale build")
	u.Run(context.Background())
	if read(t, w.cfg.ServerBin) != "image server" {
		t.Fatal("image not restored for legacy data")
	}
	if _, err := os.Stat(filepath.Join(w.cfg.State, "manifest.json")); err == nil {
		t.Fatal("updated despite legacy data")
	}
}

func TestRunDisabled(t *testing.T) {
	w := newWorld(t)
	w.cfg.Enabled = false
	w.cfg.Repo = "/nonexistent"
	New(w.cfg).Run(context.Background())
	if _, err := os.Stat(w.cfg.ImageCopy); err == nil {
		t.Fatal("disabled run touched the image")
	}
}
