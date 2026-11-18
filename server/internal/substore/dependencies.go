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
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"encoding/json"
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

func (e *DependencyUpdateError) Error() string { return e.Message }

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
	latestData      *Release
	latestError     string

	updateLock   *lockx.Mutex
	stateMu      sync.Mutex
	state        UpdateState
	beforeCommit func(stage string)
}

// NewDependencies returns the dependency manager.
func NewDependencies(env *Env, runtime *Runtime, client *Client) *Dependencies {
	return &Dependencies{env: env, runtime: runtime, client: client,
		LatestReleaseURL: fmt.Sprintf(latestAPIFormat, SubStoreRepo), MinBundleSize: minBundleSize,
		Spawner: lifecycle.DefaultSpawner{}, updateLock: lockx.NewMutex(lockx.LevelJob, "sub-store-update"),
		state: UpdateState{State: "idle"}}
}

// nodeVersion is _node_version: `node --version`, "" on failure.
func (d *Dependencies) nodeVersion(ctx context.Context) string {
	result, err := d.env.Commands.Run(ctx, []string{"node", "--version"}, 5*time.Second)
	if err != nil || result.ReturnCode != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// requestProxy is _request_options: GitHub requests use the rule download
// proxy ("" = none).
func (d *Dependencies) requestProxy(ctx context.Context) string {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return ""
	}
	var value string
	if r.View(ctx, func(doc *model.Document) error { value = doc.System.SystemConfig.RuleFetchProxy; return nil }) != nil {
		return ""
	}
	proxy, err := rulefetch.NormalizeRuleFetchProxy(value)
	if err != nil {
		return ""
	}
	return proxy
}

func (d *Dependencies) downloadURL(ctx context.Context, address string) string {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return address
	}
	var system model.SystemConfig
	if r.View(ctx, func(doc *model.Document) error { system = doc.System.SystemConfig; return nil }) != nil {
		return address
	}
	proxied, err := convert.GitHubURL(address, &model.Config{SystemConfig: system})
	if err != nil {
		return address
	}
	return proxied
}

func (d *Dependencies) now() float64 { return unixTime(d.env.Clock.Now()) }

// Latest is fetch_latest_release(force): (release, error
// text). A failure keeps returning the last successful release.
func (d *Dependencies) Latest(ctx context.Context, force bool) (*Release, string) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err.Error()
	}
	now := d.now()
	d.latestMu.Lock()
	if err := ctx.Err(); err != nil {
		d.latestMu.Unlock()
		return nil, err.Error()
	}
	if !force && d.latestData != nil && now-d.latestCheckedAt < latestTTL {
		data := d.latestData
		d.latestMu.Unlock()
		return data, ""
	}
	d.latestMu.Unlock()
	data, err := d.fetchRelease(ctx)
	d.latestMu.Lock()
	defer d.latestMu.Unlock()
	if canceled := ctx.Err(); canceled != nil {
		return d.latestData, canceled.Error()
	}
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

func (d *Dependencies) fetchRelease(ctx context.Context) (*Release, error) {
	resp, err := d.env.HTTP.Do(ctx, &Request{Method: "GET", URL: d.LatestReleaseURL,
		Header:  http.Header{"Accept": {"application/vnd.github+json"}, "User-Agent": {"ConfigFlow"}},
		Timeout: Seconds(15), Proxy: d.requestProxy(ctx)})
	if err != nil {
		return nil, err
	}
	if err := resp.RaiseForStatus(); err != nil {
		return nil, err
	}
	data, err := resp.JSONData()
	if err != nil {
		return nil, err
	}
	var source releasePayload
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, &ReleaseError{Message: msgReleaseShape, Cause: err}
	}
	for _, issue := range model.Issues(&source) {
		return nil, &ReleaseError{Message: fmt.Sprintf(msgReleaseField, issue.Path)}
	}
	for _, asset := range source.Assets {
		if asset.Name != SubStoreAsset {
			continue
		}
		sha := ""
		if strings.HasPrefix(asset.Digest, "sha256:") {
			sha = strings.TrimPrefix(asset.Digest, "sha256:")
		}
		return &Release{Version: strings.TrimLeft(source.TagName, "vV"), PublishedAt: source.PublishedAt, ReleaseURL: source.HTMLURL, Notes: runePrefix(source.Body, notesLimit), DownloadURL: asset.DownloadURL, Size: asset.Size, SHA256: sha}, nil
	}
	return nil, &ReleaseError{Message: msgReleaseMissing}
}

// UpdateSnapshot returns an independently owned status value.
func (d *Dependencies) UpdateSnapshot() UpdateState {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	return d.state
}
func (d *Dependencies) setState(state, message string) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.state.State = state
	d.state.Message = message
	if state == "success" || state == "failed" {
		d.state.FinishedAt = d.now()
	}
}
func (d *Dependencies) prepareUpdate(version string) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.state = UpdateState{State: "running", Message: fmt.Sprintf(msgPreparingUpdate, version), TargetVersion: version}
}

// GetSubStoreStatus is get_sub_store_status(force_check).
func (d *Dependencies) Snapshot(ctx context.Context, forceCheck bool) (*DependencyStatus, error) {
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
	latest, latestError := d.Latest(ctx, forceCheck)
	latestVersion := ""
	if latest != nil {
		latestVersion = latest.Version
	}
	lifecycle, err := d.runtime.Snapshot(ctx, runningVersion != "")
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
	published, releaseURL, notes := "", "", ""
	if latest != nil {
		published, releaseURL, notes = latest.PublishedAt, latest.ReleaseURL, latest.Notes
	}
	return &DependencyStatus{
		Key: "sub-store", Name: "Sub-Store", Description: msgDependencyDescription, Homepage: "https://github.com/" + SubStoreRepo,
		Running: runningVersion != "", Lifecycle: lifecycle, CurrentVersion: current, BuiltinVersion: builtinVersion,
		OnlineUpdated: bundleDir != "" && bundleDir != d.env.BuiltinDir(), Runtime: runtimeVersion, LatestVersion: latestVersion,
		LatestPublishedAt: published, ReleaseURL: releaseURL, ReleaseNotes: notes, CheckError: latestError,
		HasUpdate: current != "" && VersionNewer(latestVersion, current), Updatable: builtin, Update: d.UpdateSnapshot()}, nil
}

// List returns the supported dependency statuses in UI order.
func (d *Dependencies) List(ctx context.Context, forceCheck bool) ([]*DependencyStatus, error) {
	value, err := d.Snapshot(ctx, forceCheck)
	if err != nil {
		return nil, err
	}
	return []*DependencyStatus{value}, nil
}

// restartSubStore is _restart_sub_store.
func (d *Dependencies) restartSubStore(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := d.env.Commands.Run(ctx, []string{"supervisorctl", "-c", SupervisorConfig, "restart", SubStoreProgram},
		60*time.Second)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if result.ReturnCode != 0 {
		message := strings.TrimSpace(result.Stdout + result.Stderr)
		if message == "" {
			message = "supervisorctl restart 失败"
		}
		return errors.New(message)
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
		if err := ctx.Err(); err != nil {
			return "", err
		}
		base, err := d.env.BuiltinSubStoreURL()
		if err != nil {
			return "", err
		}
		version = d.env.FetchRunningVersion(ctx, base, 3)
		if version != "" && (expected == "" || sameVersion(VersionParts(version), VersionParts(expected))) {
			return version, nil
		}
		d.env.Clock.Sleep(time.Second)
	}
	return version, nil
}

// downloadBundle is _download_bundle.
func (d *Dependencies) downloadBundle(ctx context.Context, release *Release, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	urlText := d.downloadURL(ctx, release.DownloadURL)
	resp, err := d.env.HTTP.Do(ctx, &Request{Method: "GET", URL: urlText,
		Timeout: Timeout{Connect: 15 * time.Second, Read: 120 * time.Second}, Proxy: d.requestProxy(ctx)})
	if err != nil {
		return err
	}
	if err := resp.RaiseForStatus(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.commit(ctx, "bundle-write", func() error { return os.WriteFile(dest, resp.Body, 0o666) }); err != nil {
		return osError(err, dest)
	}
	size := int64(len(resp.Body))
	if size < d.MinBundleSize {
		return &DependencyUpdateError{fmt.Sprintf("下载的文件过小（%d 字节），可能不是有效的 Sub-Store 后端", size)}
	}
	sum := sha256.Sum256(resp.Body)
	if expected := release.SHA256; expected != "" && hex.EncodeToString(sum[:]) != expected {
		return &DependencyUpdateError{"文件校验失败（SHA256 不匹配），请检查 GitHub 代理设置"}
	}
	check, err := d.env.Commands.Run(ctx, []string{"node", "--check", dest}, 60*time.Second)
	if err != nil {
		return err
	}
	if check.ReturnCode != 0 {
		return &DependencyUpdateError{"下载的文件不是有效的 JavaScript：" + runePrefix(strings.TrimSpace(check.Stderr), 200)}
	}
	return nil
}

// runUpdate is _run_update (the update lock is held by the caller and
// released here).
func (d *Dependencies) runUpdate(ctx context.Context, release *Release, unlock func()) {
	defer unlock()
	err := func() (err error) {
		// A panic must not take the server down: Python's thread caught the
		// exception and recorded the failure.
		defer func() {
			if p := recover(); p != nil {
				err = &DependencyUpdateError{msgUnexpectedUpdate}
			}
		}()
		return d.runtime.Maintenance(ctx, func(ctx context.Context) error { return d.installRelease(ctx, release) })
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
		return fmt.Errorf("remove tree %s: %w", path, errNotDir)
	}
	if err := os.RemoveAll(path); err != nil {
		return osError(err, path)
	}
	return nil
}

var errNotDir = fs.ErrInvalid

// copyTree is shutil.copytree(src, dst, ignore=ignore_patterns('*.download.js')):
// symbolic links are followed, file modes and times copied.
func copyTree(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return osError(err, src)
	}
	info, err := os.Stat(src)
	if err != nil {
		return osError(err, src)
	}
	if err := ctx.Err(); err != nil {
		return err
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
			if err := copyTree(ctx, from, to); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(ctx, from, to, stat); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	os.Chmod(dst, info.Mode().Perm())
	if err := ctx.Err(); err != nil {
		return err
	}
	os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

func copyFile(ctx context.Context, from, to string, stat fs.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return osError(err, from)
	}
	defer in.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return osError(err, to)
	}
	if _, err := io.Copy(out, &contextReader{ctx: ctx, reader: in}); err != nil {
		out.Close()
		return osError(err, to)
	}
	if err := out.Close(); err != nil {
		return osError(err, to)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	os.Chmod(to, stat.Mode().Perm())
	if err := ctx.Err(); err != nil {
		return err
	}
	os.Chtimes(to, stat.ModTime(), stat.ModTime())
	return nil
}

// installRelease is _install_release: download, keep the previous online
// update for a rollback, swap, restart and wait for the new version; on
// any failure roll back and record the reason.
func (d *Dependencies) installRelease(ctx context.Context, release *Release) error {
	runtimeDir := d.env.RuntimeDir()
	bundle := filepath.Join(runtimeDir, SubStoreAsset)
	versionFile := filepath.Join(runtimeDir, "VERSION")
	tmpBundle := filepath.Join(runtimeDir, "sub-store.download.js") // node --check needs a .js suffix
	backupDir := runtimeDir + ".bak"
	target := release.Version
	replaced, hadPrevious := false, false
	_, previousVersion, err := d.env.ActiveBundle()
	if err != nil {
		return err
	}
	err = func() (err error) {
		defer func() {
			// An unexpected failure takes the `except Exception` path below.
			if p := recover(); p != nil {
				err = &DependencyUpdateError{msgUnexpectedUpdate}
			}
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.MkdirAll(runtimeDir, 0o777); err != nil {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		os.RemoveAll(backupDir)
		hadPrevious = isFile(bundle)
		if hadPrevious {
			if err := copyTree(ctx, runtimeDir, backupDir); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.commit(ctx, "bundle-replace", func() error { return os.Rename(tmpBundle, bundle) }); err != nil {
			return osError(err, tmpBundle)
		}
		replaced = true
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.commit(ctx, "version-write", func() error { return os.WriteFile(versionFile, []byte(target+"\n"), 0o666) }); err != nil {
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
		if running != "" && sameVersion(VersionParts(running), VersionParts(target)) {
			if err := ctx.Err(); err != nil {
				return err
			}
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
	if canceled := ctx.Err(); canceled != nil {
		d.setState("failed", msgUpdateUnavailable)
		return canceled
	}
	if replaced {
		rollback := func() (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if err := removeTree(runtimeDir); err != nil {
				return "", err
			}
			if hadPrevious {
				if err := ctx.Err(); err != nil {
					return "", err
				}
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
			if restored == "" || (previousVersion != "" && !sameVersion(VersionParts(restored), VersionParts(previousVersion))) {
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
func (d *Dependencies) StartUpdate(ctx context.Context) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !d.env.IsBuiltinAvailable() {
		return "", &DependencyUpdateError{msgUpdateNoBuiltin}
	}
	held, unlock, ok := d.updateLock.TryLock(ctx)
	if !ok {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", &DependencyUpdateError{msgUpdateBusy}
	}
	ctx = held
	if err := ctx.Err(); err != nil {
		unlock()
		return "", err
	}
	release, err := func() (*Release, error) {
		release, message := d.Latest(ctx, true)
		if release == nil {
			if message == "" {
				message = msgLatestUnavailable
			}
			return nil, errors.New(message)
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
		if current != "" && !VersionNewer(release.Version, current) {
			return nil, fmt.Errorf(msgAlreadyLatest, current)
		}
		return release, nil
	}()
	if canceled := ctx.Err(); canceled != nil {
		unlock()
		return "", canceled
	}
	if err != nil {
		unlock()
		return "", err
	}
	if err := ctx.Err(); err != nil {
		unlock()
		return "", err
	}
	version := release.Version
	d.prepareUpdate(version)
	spawner := d.Spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	if err := spawner.Spawn(ctx, "sub-store-update", func(worker context.Context) { d.runUpdate(worker, release, unlock) }); err != nil {
		unlock()
		d.setState("failed", msgUpdateUnavailable)
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", &DependencyUpdateError{msgUpdateUnavailable}
	}
	return fmt.Sprintf(msgStartingUpdate, version), nil
}

func (d *Dependencies) commit(ctx context.Context, stage string, fn func() error) error {
	if d.beforeCommit != nil {
		d.beforeCommit(stage)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
