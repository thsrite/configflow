// Package autoupdate runs the latest source on container start (opt-in:
// CONFIGFLOW_AUTO_UPDATE=1) for the autoupdate image.
//
// It fetches the newest commit of CONFIGFLOW_UPDATE_BRANCH (default main)
// from CONFIGFLOW_UPDATE_REPO (default thsrite/configflow, or a git URL /
// local git path), rebuilds only what changed (Go server, frontend with npm,
// Agent binaries and node-probe with Go) and installs it over the image's
// copy. Builds and caches live under DATA_DIR/.auto-update, so recreating
// the container does not rebuild. Any failure keeps the last good build, or
// the image's own build when there is none: startup is never blocked.
//
// At image build time it also precompresses the frontend and records the
// source fingerprints of the bundled components. It does not start or
// supervise the server.
package autoupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"configflow/internal/logx"
)

var log = logx.Get("auto-update")

// databaseFilename is the SQLite store (repo); its absence next to older
// JSON files means the data was never migrated.
const databaseFilename = "configflow.sqlite3"

// Config is the environment of one run; FromEnv gives the image defaults.
type Config struct {
	Repo, Branch string
	DataDir      string
	State        string // DataDir/.auto-update
	ServerBin    string
	HTML         string
	Agents       string
	NodeProbe    string
	ImageCopy    string // the image's own artifacts, restored when no build is usable
	ImageTrees   string // source fingerprints of the bundled components
	GoProxy      string
	// GoBinary is the Go toolchain ("" = /usr/local/go/bin/go or PATH).
	GoBinary string
	// Machine is uname -m ("" = the running kernel's).
	Machine string
	// GitHubAPI and Codeload are the GitHub endpoints (tests replace them).
	GitHubAPI, Codeload string
	HTTP                *http.Client
	// LookPath finds git and npm (and go without /usr/local/go); nil is exec.LookPath.
	LookPath func(file string) (string, error)
	Enabled  bool
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// FromEnv returns the configuration of the image.
func FromEnv() Config {
	dataDir := getenv("DATA_DIR", "/data")
	return Config{
		Repo:       strings.TrimSpace(getenv("CONFIGFLOW_UPDATE_REPO", "thsrite/configflow")),
		Branch:     strings.TrimSpace(getenv("CONFIGFLOW_UPDATE_BRANCH", "main")),
		DataDir:    dataDir,
		State:      filepath.Join(dataDir, ".auto-update"),
		ServerBin:  "/usr/local/bin/configflow",
		HTML:       getenv("CONFIGFLOW_FRONTEND_DIR", "/usr/share/configflow/html"),
		Agents:     getenv("AGENTS_STATIC_DIR", "/opt/configflow/static/agents"),
		NodeProbe:  "/opt/configflow/node-probe",
		ImageCopy:  "/app/.image",
		ImageTrees: "/app/.image-trees.json",
		GoProxy:    getenv("GOPROXY", "https://goproxy.cn,direct"),
		GitHubAPI:  "https://api.github.com",
		Codeload:   "https://codeload.github.com",
		Enabled:    os.Getenv("CONFIGFLOW_AUTO_UPDATE") == "1",
	}
}

// Updater performs one start-up update.
type Updater struct {
	cfg Config
}

// New returns an updater for cfg.
func New(cfg Config) *Updater {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	return &Updater{cfg: cfg}
}

// Manifest records the installed build under State/manifest.json. A nil
// component path means "the image's copy".
type Manifest struct {
	SHA       string  `json:"sha,omitempty"`
	Source    string  `json:"source,omitempty"`
	Server    *string `json:"server"`
	Frontend  *string `json:"frontend"`
	Agents    *string `json:"agents"`
	NodeProbe *string `json:"node_probe"`
	Deps      string  `json:"deps,omitempty"`
}

// ErrMissingToolchain means a changed component cannot be built here;
// updating would mix versions.
var ErrMissingToolchain = errors.New("missing toolchain")

type toolchainError struct{ message string }

func (e *toolchainError) Error() string { return e.message }
func (e *toolchainError) Unwrap() error { return ErrMissingToolchain }

func missingToolchain(format string, args ...any) error {
	return &toolchainError{message: fmt.Sprintf(format, args...)}
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (m *Manifest) label() string {
	if m.SHA == "" {
		return "image"
	}
	return short(m.SHA)
}

func isGitSource(repo string) bool {
	return strings.Contains(repo, "://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") ||
		strings.HasPrefix(repo, "../") || strings.HasSuffix(repo, ".git")
}

func (u *Updater) findGo() string {
	if u.cfg.GoBinary != "" {
		return u.cfg.GoBinary
	}
	if _, err := os.Stat("/usr/local/go/bin/go"); err == nil {
		return "/usr/local/go/bin/go"
	}
	path, _ := u.cfg.LookPath("go")
	return path
}

func (u *Updater) machine() string {
	if u.cfg.Machine != "" {
		return u.cfg.Machine
	}
	var name unix.Utsname
	if err := unix.Uname(&name); err != nil {
		return runtime.GOARCH
	}
	return unix.ByteSliceToString(name.Machine[:])
}

// targetGoarch maps the machine to GOARCH (+GOARM); "" when unsupported.
func (u *Updater) targetGoarch() (string, map[string]string) {
	machine := u.machine()
	arch := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64",
		"armv7l": "arm", "armv6l": "arm"}[machine]
	if arch == "" {
		return "", nil
	}
	extra := map[string]string{}
	if arch == "arm" {
		extra["GOARM"] = "7"
		if machine == "armv6l" {
			extra["GOARM"] = "6"
		}
	}
	return arch, extra
}

func (u *Updater) goEnv(extra map[string]string) map[string]string {
	goDir := "/usr/local/go/bin"
	if found := u.findGo(); found != "" {
		goDir = filepath.Dir(found)
	}
	env := map[string]string{
		"CGO_ENABLED": "0", "GOOS": "linux", "GOTOOLCHAIN": "local", "GOPROXY": u.cfg.GoProxy,
		"GOCACHE": filepath.Join(u.cfg.State, "go-cache"), "GOMODCACHE": filepath.Join(u.cfg.State, "go-mod"),
		"PATH": goDir + ":" + os.Getenv("PATH"),
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// run executes argv in cwd with env added to the environment; a failure
// carries the last 2000 bytes of the combined output.
func run(ctx context.Context, argv []string, cwd string, env map[string]string, timeout time.Duration) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+env[k])
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := out.String()
		if len(text) > 2000 {
			text = text[len(text)-2000:]
		}
		return "", fmt.Errorf(msgCommandFailed, strings.Join(argv, " "), err, text)
	}
	return out.String(), nil
}

func (u *Updater) fetch(ctx context.Context, url string, timeout time.Duration, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	req.Header.Set("User-Agent", "configflow-auto-update")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("fetch: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// LoadManifest reads the manifest; unreadable or foreign content is empty.
func (u *Updater) LoadManifest() *Manifest {
	data, err := os.ReadFile(filepath.Join(u.cfg.State, "manifest.json"))
	if err != nil {
		return &Manifest{}
	}
	var keys map[string]json.RawMessage
	var manifest Manifest
	if json.Unmarshal(data, &keys) != nil || json.Unmarshal(data, &manifest) != nil {
		return &Manifest{}
	}
	if _, ok := keys["server"]; manifest.SHA != "" && !ok {
		// Written by the Python-era image: its builds never included the Go
		// server, so reusing them would pair this image's server with a
		// frontend/Agent from another commit. Start over from the image build.
		log.Infof(msgPythonManifest, short(manifest.SHA))
		return &Manifest{}
	}
	return &manifest
}

func (u *Updater) saveManifest(m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	path := filepath.Join(u.cfg.State, "manifest.json")
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	return nil
}

func (u *Updater) resolveRemoteSHA(ctx context.Context) (string, error) {
	ref := "refs/heads/" + u.cfg.Branch
	if isGitSource(u.cfg.Repo) {
		if _, err := u.cfg.LookPath("git"); err != nil {
			return "", errors.New(msgNoGit)
		}
		out, err := run(ctx, []string{"git", "ls-remote", "--heads", u.cfg.Repo, ref}, u.cfg.State, nil, 30*time.Second)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == ref {
				return parts[0], nil
			}
		}
		return "", fmt.Errorf(msgBranchMissing, u.cfg.Branch, u.cfg.Repo)
	}
	var body bytes.Buffer
	if err := u.fetch(ctx, u.cfg.GitHubAPI+"/repos/"+u.cfg.Repo+"/commits/"+u.cfg.Branch, 30*time.Second, &body); err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body.Bytes(), &commit); err != nil || commit.SHA == "" {
		return "", fmt.Errorf("parse commit: %w", errors.Join(err, errors.New("no sha")))
	}
	return commit.SHA, nil
}

// LegacyPreSQLiteFile returns the first pre-SQLite configuration file when
// configflow.sqlite3 is absent, else "".
func LegacyPreSQLiteFile(dataDir string) string {
	if _, err := os.Lstat(filepath.Join(dataDir, databaseFilename)); err == nil {
		return ""
	}
	for _, name := range []string{"config.json", "config.json.bak", "system.json", "system.json.bak"} {
		if _, err := os.Lstat(filepath.Join(dataDir, name)); err == nil {
			return name
		}
	}
	for _, pattern := range []string{"*/config.json", "*/config.json.bak"} {
		matches, _ := filepath.Glob(filepath.Join(dataDir, "profiles", pattern))
		sort.Strings(matches)
		if len(matches) > 0 {
			rel, err := filepath.Rel(dataDir, matches[0])
			if err == nil {
				return rel
			}
		}
	}
	return ""
}

func (u *Updater) download(ctx context.Context, sha string) (string, error) {
	source := filepath.Join(u.cfg.State, "src-"+sha)
	if isDir(source) {
		return source, nil
	}
	staging := source + ".tmp"
	os.RemoveAll(staging)
	if isGitSource(u.cfg.Repo) {
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return "", fmt.Errorf("download: %w", err)
		}
		ref := "refs/heads/" + u.cfg.Branch
		if _, err := run(ctx, []string{"git", "init", "-q"}, staging, nil, 30*time.Second); err != nil {
			return "", err
		}
		if _, err := run(ctx, []string{"git", "fetch", "--depth", "1", u.cfg.Repo, ref}, staging, nil, 180*time.Second); err != nil {
			// Dumb HTTP git servers do not support shallow fetches.
			if _, err := run(ctx, []string{"git", "fetch", u.cfg.Repo, ref}, staging, nil, 300*time.Second); err != nil {
				return "", err
			}
		}
		if _, err := run(ctx, []string{"git", "checkout", "-q", "--detach", sha}, staging, nil, 60*time.Second); err != nil {
			return "", err
		}
		os.RemoveAll(filepath.Join(staging, ".git"))
		if err := os.Rename(staging, source); err != nil {
			return "", fmt.Errorf("download: %w", err)
		}
		return source, nil
	}
	archive := filepath.Join(u.cfg.State, sha+".tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	err = u.fetch(ctx, u.cfg.Codeload+"/"+u.cfg.Repo+"/tar.gz/"+sha, 120*time.Second, f)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("download: %w", cerr)
	}
	if err != nil {
		return "", err
	}
	if err := extractTarGz(archive, staging); err != nil {
		return "", err
	}
	os.Remove(archive)
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if len(entries) != 1 { // codeload wraps everything in <repo>-<sha>/
		return "", fmt.Errorf(msgArchiveLayout, len(entries))
	}
	if err := os.Rename(filepath.Join(staging, entries[0].Name()), source); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	os.RemoveAll(staging)
	return source, nil
}

// ImageTrees reads the recorded fingerprints of the image's components.
func (u *Updater) ImageTrees() map[string]string {
	trees := map[string]string{}
	data, err := os.ReadFile(u.cfg.ImageTrees)
	if err != nil {
		return trees
	}
	if json.Unmarshal(data, &trees) != nil {
		return map[string]string{}
	}
	return trees
}

func str(s string) *string { return &s }

func (u *Updater) buildServer(ctx context.Context, source string, m *Manifest, sha string) error {
	serverDir := filepath.Join(source, "server")
	if !isFile(filepath.Join(serverDir, "go.mod")) {
		return errors.New(msgNoServerModule)
	}
	key, err := ServerTreeHash(serverDir)
	if err != nil {
		return err
	}
	if key == u.ImageTrees()["server"] {
		m.Server = nil // unchanged: reuse the image's bundled binary
		return nil
	}
	if u.findGo() == "" {
		return missingToolchain(msgServerNoGo)
	}
	arch, extra := u.targetGoarch()
	if arch == "" {
		return missingToolchain(msgServerArch, u.machine())
	}
	out := filepath.Join(u.cfg.State, "server-"+key[:16]+"-"+u.machine())
	if !isFile(filepath.Join(out, "configflow")) {
		log.Info("building Go server binary")
		staging := out + ".tmp"
		os.RemoveAll(staging)
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return fmt.Errorf("build server: %w", err)
		}
		extra["GOARCH"] = arch
		label := u.cfg.Branch + "-" + short(sha)
		argv := []string{u.findGo(), "build", "-trimpath", "-ldflags=-s -w -X configflow/internal/version.Version=" + label,
			"-o", filepath.Join(staging, "configflow"), "./cmd/configflow"}
		if _, err := run(ctx, argv, serverDir, u.goEnv(extra), 900*time.Second); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(staging, "configflow"), 0o755); err != nil {
			return fmt.Errorf("build server: %w", err)
		}
		if err := os.Rename(staging, out); err != nil {
			return fmt.Errorf("build server: %w", err)
		}
	}
	m.Server = str(out)
	return nil
}

func (u *Updater) buildFrontend(ctx context.Context, source string, m *Manifest) error {
	frontend := filepath.Join(source, "frontend")
	key, err := TreeHash(frontend, FrontendInput)
	if err != nil {
		return err
	}
	if key == u.ImageTrees()["frontend"] {
		m.Frontend = nil // unchanged: reuse the image's bundled frontend
		return nil
	}
	npm, err := u.cfg.LookPath("npm")
	if err != nil {
		return missingToolchain(msgFrontendNoNPM)
	}
	out := filepath.Join(u.cfg.State, "frontend-"+key[:16])
	lock, err := os.ReadFile(filepath.Join(frontend, "package-lock.json"))
	if err != nil {
		return fmt.Errorf("build frontend: %w", err)
	}
	lockSum := sha256.Sum256(lock)
	// Node resolves packages by walking up to directories literally named
	// node_modules (on the symlink's real path), so keep that name.
	modules := filepath.Join(u.cfg.State, "deps-"+hex.EncodeToString(lockSum[:])[:16], "node_modules")
	m.Deps = filepath.Dir(modules) // kept by prune even when the build is reused
	if !isDir(out) {
		if !isDir(modules) {
			log.Info("installing frontend dependencies (package-lock changed)")
			argv := []string{npm, "ci", "--no-audit", "--no-fund", "--cache", filepath.Join(u.cfg.State, "npm-cache")}
			if _, err := run(ctx, argv, frontend, nil, 900*time.Second); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(modules), 0o755); err != nil {
				return fmt.Errorf("build frontend: %w", err)
			}
			if err := moveDir(filepath.Join(frontend, "node_modules"), modules); err != nil {
				return err
			}
		}
		link := filepath.Join(frontend, "node_modules")
		if _, err := os.Stat(link); err != nil {
			if err := os.Symlink(modules, link); err != nil {
				return fmt.Errorf("build frontend: %w", err)
			}
		}
		log.Info("building frontend")
		if _, err := run(ctx, []string{npm, "run", "build"}, frontend, nil, 900*time.Second); err != nil {
			return err
		}
		staging := out + ".tmp"
		os.RemoveAll(staging)
		if err := copyTree(filepath.Join(frontend, "dist"), staging); err != nil {
			return err
		}
		if err := PrecompressDir(staging); err != nil {
			return err
		}
		if err := os.Rename(staging, out); err != nil {
			return fmt.Errorf("build frontend: %w", err)
		}
	}
	m.Frontend = str(out)
	return nil
}

// agentArches are the Agent binaries the panel hands out.
var agentArches = []struct {
	name  string
	extra map[string]string
}{
	{"amd64", nil}, {"arm64", nil}, {"armv7", map[string]string{"GOARCH": "arm", "GOARM": "7"}},
}

func (u *Updater) buildAgents(ctx context.Context, source string, m *Manifest) error {
	agent := filepath.Join(source, "backend", "agents", "go-agent")
	key, err := TreeHash(agent, GoModuleInput)
	if err != nil {
		return err
	}
	if key == u.ImageTrees()["agents"] {
		m.Agents = nil // unchanged: reuse the image's bundled binaries
		return nil
	}
	if u.findGo() == "" {
		return missingToolchain(msgAgentNoGo)
	}
	out := filepath.Join(u.cfg.State, "agents-"+key[:16])
	if !isDir(out) {
		log.Info("building Agent binaries")
		staging := out + ".tmp"
		os.RemoveAll(staging)
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return fmt.Errorf("build agents: %w", err)
		}
		for _, arch := range agentArches {
			extra := map[string]string{"GOARCH": arch.name}
			for k, v := range arch.extra { // armv7 is GOARCH=arm with GOARM=7
				extra[k] = v
			}
			argv := []string{u.findGo(), "build", "-trimpath", "-ldflags=-s -w",
				"-o", filepath.Join(staging, "configflow-agent-linux-"+arch.name), "."}
			if _, err := run(ctx, argv, agent, u.goEnv(extra), 900*time.Second); err != nil {
				return err
			}
		}
		if err := os.Rename(staging, out); err != nil {
			return fmt.Errorf("build agents: %w", err)
		}
	}
	m.Agents = str(out)
	return nil
}

func (u *Updater) buildNodeProbe(ctx context.Context, source string, m *Manifest) error {
	probe := filepath.Join(source, "backend", "node-probe")
	if !isFile(filepath.Join(probe, "go.mod")) {
		m.NodeProbe = nil
		return nil
	}
	key, err := TreeHash(probe, GoModuleInput)
	if err != nil {
		return err
	}
	if key == u.ImageTrees()["node_probe"] {
		m.NodeProbe = nil
		return nil
	}
	if u.findGo() == "" {
		return missingToolchain(msgProbeNoGo)
	}
	arch, extra := u.targetGoarch()
	if arch == "" {
		return missingToolchain(msgProbeArch)
	}
	out := filepath.Join(u.cfg.State, "node-probe-"+key[:16]+"-"+u.machine())
	if !isFile(filepath.Join(out, "configflow-node-probe")) {
		log.Info("building independent node latency worker")
		staging := out + ".tmp"
		os.RemoveAll(staging)
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return fmt.Errorf("build node-probe: %w", err)
		}
		extra["GOARCH"] = arch
		argv := []string{u.findGo(), "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(staging, "configflow-node-probe"), "."}
		if _, err := run(ctx, argv, probe, u.goEnv(extra), 900*time.Second); err != nil {
			return err
		}
		for _, notice := range []string{"LICENSE", "NOTICE"} {
			if isFile(filepath.Join(probe, notice)) {
				if err := copyFile(filepath.Join(probe, notice), filepath.Join(staging, notice)); err != nil {
					return err
				}
			}
		}
		if err := os.Rename(staging, out); err != nil {
			return fmt.Errorf("build node-probe: %w", err)
		}
	}
	m.NodeProbe = str(out)
	return nil
}

type component struct {
	saved string
	path  string
	built func(*Manifest) *string
}

func (u *Updater) components() []component {
	return []component{
		{"html", u.cfg.HTML, func(m *Manifest) *string { return m.Frontend }},
		{"agents", u.cfg.Agents, func(m *Manifest) *string { return m.Agents }},
		{"node_probe", u.cfg.NodeProbe, func(m *Manifest) *string { return m.NodeProbe }},
	}
}

// PreserveImage keeps the image's own artifacts once per container, so a
// failed update can return to them.
func (u *Updater) PreserveImage() error {
	if isDir(u.cfg.ImageCopy) {
		return nil
	}
	staging := u.cfg.ImageCopy + ".tmp"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("preserve image: %w", err)
	}
	if isFile(u.cfg.ServerBin) {
		if err := copyFile(u.cfg.ServerBin, filepath.Join(staging, "configflow")); err != nil {
			return err
		}
	}
	for _, c := range u.components() {
		if isDir(c.path) {
			if err := copyTree(c.path, filepath.Join(staging, c.saved)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(staging, u.cfg.ImageCopy); err != nil {
		return fmt.Errorf("preserve image: %w", err)
	}
	return nil
}

// RestoreImage puts the preserved image artifacts back.
func (u *Updater) RestoreImage() error {
	saved := filepath.Join(u.cfg.ImageCopy, "configflow")
	if isFile(saved) {
		if err := replaceFile(saved, u.cfg.ServerBin, 0o755); err != nil {
			return err
		}
	}
	for _, c := range u.components() {
		if dir := filepath.Join(u.cfg.ImageCopy, c.saved); isDir(dir) {
			if err := replaceDir(dir, c.path); err != nil {
				return err
			}
		}
	}
	return nil
}

// Install installs the recorded build over the image (also after a
// container recreate); without a build it restores the image's copy and
// reports false.
func (u *Updater) Install(m *Manifest) (bool, error) {
	if m.SHA == "" {
		return false, u.RestoreImage()
	}
	if m.Server != nil && isFile(filepath.Join(*m.Server, "configflow")) {
		if err := replaceFile(filepath.Join(*m.Server, "configflow"), u.cfg.ServerBin, 0o755); err != nil {
			return false, err
		}
	} else if saved := filepath.Join(u.cfg.ImageCopy, "configflow"); isFile(saved) {
		if err := replaceFile(saved, u.cfg.ServerBin, 0o755); err != nil {
			return false, err
		}
	}
	// A component without its own build uses the image's copy, never a
	// stale build left over from an earlier commit.
	for _, c := range u.components() {
		if built := c.built(m); built != nil && *built != "" && isDir(*built) {
			if err := replaceDir(*built, c.path); err != nil {
				return false, err
			}
		} else if saved := filepath.Join(u.cfg.ImageCopy, c.saved); isDir(saved) {
			if err := replaceDir(saved, c.path); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// Prune drops sources and builds the manifest no longer points at (caches
// stay).
func (u *Updater) Prune(m *Manifest) {
	keep := map[string]bool{}
	for _, p := range []*string{&m.Source, m.Server, m.Frontend, m.Agents, m.NodeProbe, &m.Deps} {
		if p != nil && *p != "" {
			keep[filepath.Base(*p)] = true
		}
	}
	entries, err := os.ReadDir(u.cfg.State)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		built := false
		for _, prefix := range []string{"src-", "server-", "frontend-", "agents-", "node-probe-", "deps-"} {
			built = built || strings.HasPrefix(name, prefix)
		}
		leftover := strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".tar.gz")
		if built && !keep[name] || leftover {
			os.RemoveAll(filepath.Join(u.cfg.State, name))
		}
	}
}

// RecordImage fingerprints, at image build time, the sources the image's
// server/frontend/Agent/node-probe were built from (root holds server/,
// frontend/, go-agent/ and node-probe/) and writes them to out.
func RecordImage(root, out string) (map[string]string, error) {
	server, err := ServerTreeHash(filepath.Join(root, "server"))
	if err != nil {
		return nil, err
	}
	frontend, err := TreeHash(filepath.Join(root, "frontend"), FrontendInput)
	if err != nil {
		return nil, err
	}
	agents, err := TreeHash(filepath.Join(root, "go-agent"), GoModuleInput)
	if err != nil {
		return nil, err
	}
	probe, err := TreeHash(filepath.Join(root, "node-probe"), GoModuleInput)
	if err != nil {
		return nil, err
	}
	trees := map[string]string{"server": server, "frontend": frontend, "agents": agents, "node_probe": probe}
	data, err := json.Marshal(trees)
	if err != nil {
		return nil, fmt.Errorf("record image: %w", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, fmt.Errorf("record image: %w", err)
	}
	return trees, nil
}

// Run is the container start-up step. It never returns an error: every
// failure falls back to the last good build or the image's own.
func (u *Updater) Run(ctx context.Context) {
	if !u.cfg.Enabled {
		return
	}
	if legacy := LegacyPreSQLiteFile(u.cfg.DataDir); legacy != "" {
		log.Infof(msgLegacyData, legacy, databaseFilename)
		if isDir(u.cfg.ImageCopy) {
			u.restoreOrLog()
		}
		return
	}
	if err := os.MkdirAll(u.cfg.State, 0o755); err != nil {
		log.Infof(msgStateDirFailed, err)
		return
	}
	if err := u.PreserveImage(); err != nil {
		log.Infof(msgPreserveFailed, err)
	}
	manifest := u.LoadManifest()
	started := time.Now()
	sha, err := u.resolveRemoteSHA(ctx)
	if err != nil {
		log.Infof(msgSourceUnreachable, err, manifest.label())
		u.installOrRestore(manifest)
		return
	}
	if sha == manifest.SHA {
		log.Infof(msgAlreadyBuilt, u.cfg.Branch, short(sha))
		u.installOrRestore(manifest)
		return
	}
	log.Infof(msgUpdating, u.cfg.Repo, u.cfg.Branch, short(sha))
	candidate := *manifest
	err = func() error {
		source, err := u.download(ctx, sha)
		if err != nil {
			return err
		}
		candidate.Source = source
		if err := u.buildServer(ctx, source, &candidate, sha); err != nil {
			return err
		}
		if err := u.buildFrontend(ctx, source, &candidate); err != nil {
			return err
		}
		if err := u.buildAgents(ctx, source, &candidate); err != nil {
			return err
		}
		if err := u.buildNodeProbe(ctx, source, &candidate); err != nil {
			return err
		}
		candidate.SHA = sha
		if _, err := u.Install(&candidate); err != nil {
			return err
		}
		if err := u.saveManifest(&candidate); err != nil {
			return err
		}
		u.Prune(&candidate)
		return nil
	}()
	switch {
	case err == nil:
		log.Infof(msgRunning, short(sha), time.Since(started).Seconds())
	case errors.Is(err, ErrMissingToolchain):
		log.Infof(msgSkippedToolchain, short(sha), err, manifest.label())
		u.installOrRestore(manifest)
		u.Prune(manifest)
	default:
		log.Infof(msgUpdateFailed, manifest.label(), err)
		u.installOrRestore(manifest)
	}
}

func (u *Updater) installOrRestore(m *Manifest) {
	if _, err := u.Install(m); err != nil {
		log.Infof(msgRestoreFailed, err)
		u.restoreOrLog()
	}
}

func (u *Updater) restoreOrLog() {
	if err := u.RestoreImage(); err != nil {
		log.Infof(msgImageRestoreFailed, err)
	}
}
