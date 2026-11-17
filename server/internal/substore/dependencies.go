package substore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

var depsLog = logx.Get("backend.utils.dependencies")

const (
	latestTTL       = 30 * 60
	minBundleSize   = 500 * 1024
	notesLimit      = 2000
	latestAPIFormat = "https://api.github.com/repos/%s/releases/latest"
)

// DependencyUpdateError is dependencies.DependencyUpdateError: an update
// error that can be shown to the user as it is.
type DependencyUpdateError struct{ Message string }

func (e *DependencyUpdateError) Error() string       { return e.Message }
func (e *DependencyUpdateError) PythonClass() string { return "DependencyUpdateError" }

// Dependencies is dependencies.py: the bundled Sub-Store's versions, the
// latest release (cached 30 minutes) and the online update.
type Dependencies struct {
	env     *Env
	runtime *Runtime
	client  *Client

	// LatestReleaseURL is the GitHub API URL of the latest release.
	LatestReleaseURL string
	// MinBundleSize is _MIN_BUNDLE_SIZE.
	MinBundleSize int64
	// Spawner owns detached update work and supplies shutdown cancellation.
	Spawner lockx.Spawner

	latestMu        sync.Mutex
	latestCheckedAt float64
	latestData      *jsonx.Object
	latestError     string

	updateLock *lockx.Mutex
	stateMu    sync.Mutex
	state      *jsonx.Object
}

// NewDependencies returns the dependency manager.
func NewDependencies(env *Env, runtime *Runtime, client *Client) *Dependencies {
	return &Dependencies{env: env, runtime: runtime, client: client,
		LatestReleaseURL: fmt.Sprintf(latestAPIFormat, SubStoreRepo), MinBundleSize: minBundleSize,
		Spawner: lifecycle.DefaultSpawner{}, updateLock: lockx.NewMutex(lockx.LevelJob, "sub-store-update"),
		state: jsonx.Obj("state", "idle", "message", "", "target_version", "", "finished_at", jsonx.Float(0))}
}

// nodeVersion is _node_version: `node --version`, "" on failure.
func (d *Dependencies) nodeVersion(ctx context.Context) string {
	result, err := d.env.Commands.Run(ctx, []string{"node", "--version"}, 5*time.Second)
	if err != nil || result.ReturnCode != 0 {
		return ""
	}
	return pyStrip(result.Stdout)
}

// requestProxy is _request_options: GitHub requests use the rule download
// proxy ("" = none).
func (d *Dependencies) requestProxy(ctx context.Context) string {
	config, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return ""
	}
	proxy, err := rulefetch.GetRuleFetchProxy(config)
	if err != nil {
		return ""
	}
	return proxy
}

// downloadURL is _download_url: the GitHub proxy domain prefix applied to
// a download URL (not to API requests).
func (d *Dependencies) downloadURL(ctx context.Context, url any) any {
	text, ok := url.(string)
	if !ok {
		return url
	}
	config, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return url
	}
	proxied, err := convert.ApplyGithubProxyDomain(text, config)
	if err != nil {
		return url
	}
	return proxied
}

func (d *Dependencies) now() float64 { return pyTime(d.env.Clock.Now()) }

// FetchLatestRelease is fetch_latest_release(force): (release, error
// text). A failure keeps returning the last successful release.
func (d *Dependencies) FetchLatestRelease(ctx context.Context, force bool) (*jsonx.Object, string) {
	now := d.now()
	d.latestMu.Lock()
	if !force && d.latestData != nil && now-d.latestCheckedAt < latestTTL {
		data := d.latestData
		d.latestMu.Unlock()
		return data, ""
	}
	d.latestMu.Unlock()
	data, err := d.fetchRelease(ctx)
	d.latestMu.Lock()
	defer d.latestMu.Unlock()
	if err != nil {
		message := "检查更新失败：" + safeExceptionDetails(err)
		depsLog.Warning("Sub-Store " + message)
		d.latestCheckedAt, d.latestError = now, message
		// Network failures still return the last good result.
		return d.latestData, message
	}
	d.latestCheckedAt, d.latestData, d.latestError = now, data, ""
	return data, ""
}

func (d *Dependencies) fetchRelease(ctx context.Context) (*jsonx.Object, error) {
	resp, err := d.env.HTTP.Do(ctx, &Request{Method: "GET", URL: d.LatestReleaseURL,
		Header:  http.Header{"Accept": {"application/vnd.github+json"}, "User-Agent": {"ConfigFlow"}},
		Timeout: Seconds(15), Proxy: d.requestProxy(ctx)})
	if err != nil {
		return nil, err
	}
	if err := resp.RaiseForStatus(); err != nil {
		return nil, err
	}
	value, err := resp.JSON()
	if err != nil {
		return nil, err
	}
	release, ok := value.(*jsonx.Object)
	if !ok {
		return nil, attributeError(value, "get")
	}
	assets, err := iterate(release.GetOr("assets", []any{}))
	if err != nil {
		return nil, err
	}
	var asset *jsonx.Object
	for _, item := range assets {
		object, ok := item.(*jsonx.Object)
		if !ok {
			return nil, attributeError(item, "get")
		}
		if jsonx.Equal(object.Value("name"), SubStoreAsset) {
			asset = object
			break
		}
	}
	if asset == nil {
		return nil, pyErr("ValueError", "最新 release 中没有 "+SubStoreAsset)
	}
	digest := asset.Value("digest")
	if !jsonx.Truthy(digest) {
		digest = ""
	}
	// The dict literal evaluates the notes before digest.startswith.
	notes, err := slice2000(release.Value("body"))
	if err != nil {
		return nil, err
	}
	digestText, ok := digest.(string)
	if !ok {
		return nil, attributeError(digest, "startswith")
	}
	sha := ""
	if strings.HasPrefix(digestText, "sha256:") {
		sha = strings.SplitN(digestText, ":", 2)[1]
	}
	size := asset.Value("size")
	if !jsonx.Truthy(size) {
		size = jsonx.Int(0)
	}
	return jsonx.Obj(
		"version", strings.TrimLeft(jsonx.Str(release.GetOr("tag_name", "")), "vV"),
		"published_at", release.GetOr("published_at", ""),
		"release_url", release.GetOr("html_url", ""),
		"notes", notes,
		"download_url", asset.GetOr("browser_download_url", ""),
		"size", size,
		"sha256", sha,
	), nil
}

// slice2000 is (release.get('body') or ”)[:2000].
func slice2000(value any) (any, error) {
	if !jsonx.Truthy(value) {
		return "", nil
	}
	switch v := value.(type) {
	case string:
		return runePrefix(v, notesLimit), nil
	case []any:
		if len(v) > notesLimit {
			return v[:notesLimit], nil
		}
		return v, nil
	}
	return nil, pyErr("TypeError", fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

// GetUpdateState is get_update_state().
func (d *Dependencies) GetUpdateState() *jsonx.Object {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	return d.state.Clone()
}

// setState is _set_state.
func (d *Dependencies) setState(state, message string, extra ...any) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.state.Set("state", state)
	d.state.Set("message", message)
	for i := 0; i+1 < len(extra); i += 2 {
		d.state.Set(extra[i].(string), extra[i+1])
	}
	if state == "success" || state == "failed" {
		d.state.Set("finished_at", jsonx.Float(d.now()))
	}
}

// GetSubStoreStatus is get_sub_store_status(force_check).
func (d *Dependencies) GetSubStoreStatus(ctx context.Context, forceCheck bool) (*jsonx.Object, error) {
	builtin := d.env.IsBuiltinAvailable()
	base, err := d.env.BaseURL()
	if err != nil {
		return nil, err
	}
	timeout := 5.0
	if builtin {
		timeout = 1
	}
	runningVersion := d.env.FetchRunningVersion(ctx, base, timeout)
	bundleDir, bundleVersion := "", ""
	if builtin {
		if bundleDir, bundleVersion, err = d.env.ActiveBundle(); err != nil {
			return nil, err
		}
	}
	current := runningVersion
	if current == "" {
		current = bundleVersion
	}
	latest, latestError := d.FetchLatestRelease(ctx, forceCheck)
	latestVersion := ""
	if latest != nil {
		latestVersion, _ = latest.Value("version").(string)
	}
	lifecycle, err := d.runtime.Status(ctx, runningVersion != "")
	if err != nil {
		return nil, err
	}
	builtinVersion, runtimeVersion := "", ""
	if builtin {
		if builtinVersion, err = readVersionFile(d.env.BuiltinDir()); err != nil {
			return nil, err
		}
		runtimeVersion = d.nodeVersion(ctx)
	}
	fromLatest := func(key string) any {
		if latest == nil {
			return ""
		}
		return latest.Value(key)
	}
	return jsonx.Obj(
		"key", "sub-store",
		"name", "Sub-Store",
		"description", "订阅解析与节点格式转换",
		"homepage", "https://github.com/"+SubStoreRepo,
		"running", runningVersion != "",
		"lifecycle", lifecycle,
		"current_version", current,
		"builtin_version", builtinVersion,
		"online_updated", bundleDir != "" && bundleDir != d.env.BuiltinDir(),
		"runtime", runtimeVersion,
		"latest_version", latestVersion,
		"latest_published_at", fromLatest("published_at"),
		"release_url", fromLatest("release_url"),
		"release_notes", fromLatest("notes"),
		"check_error", latestError,
		"has_update", current != "" && IsNewer(latestVersion, current),
		// Development environments have no bundle: detection only.
		"updatable", builtin,
		"update", d.GetUpdateState(),
	), nil
}

// ListDependencies is list_dependencies(force_check).
func (d *Dependencies) ListDependencies(ctx context.Context, forceCheck bool) ([]any, error) {
	status, err := d.GetSubStoreStatus(ctx, forceCheck)
	if err != nil {
		return nil, err
	}
	return []any{status}, nil
}

// restartSubStore is _restart_sub_store.
func (d *Dependencies) restartSubStore(ctx context.Context) error {
	result, err := d.env.Commands.Run(ctx, []string{"supervisorctl", "-c", SupervisorConfig, "restart", SubStoreProgram},
		60*time.Second)
	if err != nil {
		return err
	}
	if result.ReturnCode != 0 {
		message := pyStrip(result.Stdout + result.Stderr)
		if message == "" {
			message = "supervisorctl restart 失败"
		}
		return pyErr("RuntimeError", message)
	}
	return nil
}

// waitForVersion is _wait_for_version(expected, timeout): poll the bundled
// Sub-Store until it reports the expected version (any version when
// expected is empty); the last version seen otherwise.
func (d *Dependencies) waitForVersion(ctx context.Context, expected string, timeout float64) (string, error) {
	deadline := d.now() + timeout
	version := ""
	for d.now() < deadline {
		base, err := d.env.BuiltinSubStoreURL()
		if err != nil {
			return "", err
		}
		version = d.env.FetchRunningVersion(ctx, base, 3)
		if version != "" && (expected == "" || sameVersion(ParseVersion(version), ParseVersion(expected))) {
			return version, nil
		}
		d.env.Clock.Sleep(time.Second)
	}
	return version, nil
}

// downloadBundle is _download_bundle.
func (d *Dependencies) downloadBundle(ctx context.Context, release *jsonx.Object, dest string) error {
	url := d.downloadURL(ctx, release.Value("download_url"))
	urlText, ok := url.(string)
	if !ok {
		urlText = jsonx.Str(url)
	}
	resp, err := d.env.HTTP.Do(ctx, &Request{Method: "GET", URL: urlText,
		Timeout: Timeout{Connect: 15 * time.Second, Read: 120 * time.Second}, Proxy: d.requestProxy(ctx)})
	if err != nil {
		return err
	}
	if err := resp.RaiseForStatus(); err != nil {
		return err
	}
	if err := os.WriteFile(dest, resp.Body, 0o666); err != nil {
		return osError(err, dest)
	}
	size := int64(len(resp.Body))
	if size < d.MinBundleSize {
		return &DependencyUpdateError{fmt.Sprintf("下载的文件过小（%d 字节），可能不是有效的 Sub-Store 后端", size)}
	}
	sum := sha256.Sum256(resp.Body)
	if expected := release.Value("sha256"); jsonx.Truthy(expected) && !jsonx.Equal(hex.EncodeToString(sum[:]), expected) {
		return &DependencyUpdateError{"文件校验失败（SHA256 不匹配），请检查 GitHub 代理设置"}
	}
	check, err := d.env.Commands.Run(ctx, []string{"node", "--check", dest}, 60*time.Second)
	if err != nil {
		return err
	}
	if check.ReturnCode != 0 {
		return &DependencyUpdateError{"下载的文件不是有效的 JavaScript：" + runePrefix(pyStrip(check.Stderr), 200)}
	}
	return nil
}

// runUpdate is _run_update (the update lock is held by the caller and
// released here).
func (d *Dependencies) runUpdate(ctx context.Context, release *jsonx.Object, unlock func()) {
	defer unlock()
	err := func() (err error) {
		// A panic must not take the server down: Python's thread caught the
		// exception and recorded the failure.
		defer func() {
			if p := recover(); p != nil {
				err = pyErr("Exception", "")
			}
		}()
		return d.runtime.Maintenance(ctx, func() error { return d.installRelease(ctx, release) })
	}()
	if err != nil {
		depsLog.Warning("Sub-Store 更新等待失败: " + safeExceptionDetails(err))
		d.setState("failed", msgUpdateUnavailable)
	}
}

// removeAll is shutil.rmtree(ignore_errors=False) for an existing tree.
func removeTree(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return osError(err, path)
	}
	if !info.IsDir() {
		return pyErr("NotADirectoryError", pyOSError(errNotDir, path))
	}
	if err := os.RemoveAll(path); err != nil {
		return osError(err, path)
	}
	return nil
}

var errNotDir = fs.ErrInvalid

// copyTree is shutil.copytree(src, dst, ignore=ignore_patterns('*.download.js')):
// symbolic links are followed, file modes and times copied.
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return osError(err, src)
	}
	info, err := os.Stat(src)
	if err != nil {
		return osError(err, src)
	}
	if err := os.MkdirAll(dst, 0o777); err != nil {
		return osError(err, dst)
	}
	for _, entry := range entries {
		if matched, _ := filepath.Match("*.download.js", entry.Name()); matched {
			continue
		}
		from, to := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		stat, err := os.Stat(from)
		if err != nil {
			return osError(err, from)
		}
		if stat.IsDir() {
			if err := copyTree(from, to); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(from, to, stat); err != nil {
			return err
		}
	}
	os.Chmod(dst, info.Mode().Perm())
	os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

func copyFile(from, to string, stat fs.FileInfo) error {
	in, err := os.Open(from)
	if err != nil {
		return osError(err, from)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return osError(err, to)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return osError(err, to)
	}
	if err := out.Close(); err != nil {
		return osError(err, to)
	}
	os.Chmod(to, stat.Mode().Perm())
	os.Chtimes(to, stat.ModTime(), stat.ModTime())
	return nil
}

// installRelease is _install_release: download, keep the previous online
// update for a rollback, swap, restart and wait for the new version; on
// any failure roll back and record the reason.
func (d *Dependencies) installRelease(ctx context.Context, release *jsonx.Object) error {
	runtimeDir := d.env.RuntimeDir()
	bundle := pyJoin(runtimeDir, SubStoreAsset)
	versionFile := pyJoin(runtimeDir, "VERSION")
	tmpBundle := pyJoin(runtimeDir, "sub-store.download.js") // node --check needs a .js suffix
	backupDir := runtimeDir + ".bak"
	target, _ := release.Value("version").(string)
	replaced, hadPrevious := false, false
	_, previousVersion, err := d.env.ActiveBundle()
	if err != nil {
		return err
	}
	err = func() (err error) {
		defer func() {
			// An unexpected failure takes the `except Exception` path below.
			if p := recover(); p != nil {
				err = pyErr("Exception", fmt.Sprint(p))
			}
		}()
		if err := pyMakedirs(runtimeDir); err != nil {
			path := runtimeDir
			var pathErr *fs.PathError
			if errors.As(err, &pathErr) {
				path = pathErr.Path
			}
			return osError(err, path)
		}
		d.setState("running", "正在下载 Sub-Store "+target+"…")
		if err := d.downloadBundle(ctx, release, tmpBundle); err != nil {
			return err
		}
		// Keep the previous online update for a rollback.
		os.RemoveAll(backupDir)
		hadPrevious = isFile(bundle)
		if hadPrevious {
			if err := copyTree(runtimeDir, backupDir); err != nil {
				return err
			}
		}
		if err := os.Rename(tmpBundle, bundle); err != nil {
			return osError(err, tmpBundle)
		}
		replaced = true
		if err := os.WriteFile(versionFile, []byte(target+"\n"), 0o666); err != nil {
			return osError(err, versionFile)
		}
		d.setState("running", "正在重启 Sub-Store "+target+"…")
		if err := d.restartSubStore(ctx); err != nil {
			return err
		}
		running, err := d.waitForVersion(ctx, target, 40)
		if err != nil {
			return err
		}
		if running != "" && sameVersion(ParseVersion(running), ParseVersion(target)) {
			os.RemoveAll(backupDir)
			previous := previousVersion
			if previous == "" {
				previous = "内置版本"
			}
			depsLog.Info(fmt.Sprintf("Sub-Store 已在线更新：%s → %s", previous, target))
			d.setState("success", "Sub-Store 已更新到 "+target)
			return nil
		}
		return &DependencyUpdateError{"Sub-Store " + target + " 启动后未就绪"}
	}()
	if err == nil {
		return nil
	}
	depsLog.Warning("Sub-Store 在线更新失败: " + safeExceptionDetails(err))
	os.Remove(tmpBundle)
	var updateErr *DependencyUpdateError
	detail := safeExceptionDetails(err)
	if errors.As(err, &updateErr) && err == error(updateErr) {
		detail = updateErr.Message
	}
	if replaced {
		rollback := func() (string, error) {
			if err := removeTree(runtimeDir); err != nil {
				return "", err
			}
			if hadPrevious {
				if err := os.Rename(backupDir, runtimeDir); err != nil {
					return "", osError(err, backupDir)
				}
			}
			if err := d.restartSubStore(ctx); err != nil {
				return "", err
			}
			restored, err := d.waitForVersion(ctx, previousVersion, 30)
			if err != nil {
				return "", err
			}
			if restored == "" || (previousVersion != "" && !sameVersion(ParseVersion(restored), ParseVersion(previousVersion))) {
				return "", &DependencyUpdateError{"原版本仍未就绪"}
			}
			return restored, nil
		}
		restored, rollbackErr := rollback()
		if rollbackErr != nil {
			depsLog.Warning("Sub-Store 回滚失败: " + safeExceptionDetails(rollbackErr))
			detail += "；回滚未完成，请检查容器日志"
		} else {
			detail += "；已回滚到 " + restored
			depsLog.Info("Sub-Store 已回滚到 " + restored)
		}
	}
	d.setState("failed", "更新失败："+detail)
	return nil
}

// StartSubStoreUpdate is start_sub_store_update(): start the background
// update; (ok, message).
func (d *Dependencies) StartSubStoreUpdate(ctx context.Context) (bool, string) {
	ctx = lifecycle.Detach(ctx)
	if !d.env.IsBuiltinAvailable() {
		return false, msgUpdateNoBuiltin
	}
	held, unlock, ok := d.updateLock.TryLock(ctx)
	if !ok {
		return false, msgUpdateBusy
	}
	ctx = held
	release, err := func() (*jsonx.Object, error) {
		release, message := d.FetchLatestRelease(ctx, true)
		if release == nil {
			if message == "" {
				message = msgLatestUnavailable
			}
			return nil, pyErr("RuntimeError", message)
		}
		base, err := d.env.BuiltinSubStoreURL()
		if err != nil {
			return nil, err
		}
		current := d.env.FetchRunningVersion(ctx, base, 5)
		if current == "" {
			if _, current, err = d.env.ActiveBundle(); err != nil {
				return nil, err
			}
		}
		if current != "" && !IsNewer(release.Value("version"), current) {
			return nil, pyErr("RuntimeError", fmt.Sprintf(msgAlreadyLatest, current))
		}
		return release, nil
	}()
	if err != nil {
		unlock()
		return false, err.Error()
	}
	version := jsonx.Str(release.Value("version"))
	d.setState("running", fmt.Sprintf(msgPreparingUpdate, version), "target_version", release.Value("version"), "finished_at", jsonx.Float(0))
	spawner := d.Spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	if err := spawner.Spawn(ctx, "sub-store-update", func(worker context.Context) { d.runUpdate(worker, release, unlock) }); err != nil {
		unlock()
		d.setState("failed", msgUpdateUnavailable)
		return false, msgUpdateUnavailable
	}
	return true, fmt.Sprintf(msgStartingUpdate, version)
}
