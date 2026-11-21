#!/usr/bin/env python3
"""Run the latest source on container start (opt-in: CONFIGFLOW_AUTO_UPDATE=1).

Fetches the newest commit of CONFIGFLOW_UPDATE_BRANCH (default main) from
CONFIGFLOW_UPDATE_REPO (default thsrite/configflow, or a git URL / local git
path), rebuilds only what changed (Go server, frontend with npm, Agent binaries
and node-probe with Go) and installs it over the image's copy. Builds and caches
live under /data/.auto-update, so recreating the container does not rebuild. Any
failure keeps the last good build, or the image's own build when there is none:
startup must never be blocked.
"""
import glob
import gzip
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import time
import urllib.request

REPO = os.environ.get('CONFIGFLOW_UPDATE_REPO', 'thsrite/configflow').strip()
BRANCH = os.environ.get('CONFIGFLOW_UPDATE_BRANCH', 'main').strip()
DATA_DIR = os.environ.get('DATA_DIR', '/data')
STATE = os.path.join(DATA_DIR, '.auto-update')
DATABASE_FILENAME = 'configflow.sqlite3'
SERVER_BIN = '/usr/local/bin/configflow'
HTML = os.environ.get('CONFIGFLOW_FRONTEND_DIR', '/usr/share/configflow/html')
AGENTS = os.environ.get('AGENTS_STATIC_DIR', '/opt/configflow/static/agents')
NODE_PROBE = '/opt/configflow/node-probe'
IMAGE_COPY = '/app/.image'  # the image's own artifacts, restored when no build is usable
IMAGE_TREES = '/app/.image-trees.json'  # source fingerprints of bundled components
ARCHES = (('amd64', {}), ('arm64', {}), ('armv7', {'GOARCH': 'arm', 'GOARM': '7'}))
SERVER_IGNORED_DIRS = frozenset({
    'testdata', 'difftest', 'installertest', 'yamlxtest', 'testgolden', 'testjson', 'fuzz',
})
PRECOMPRESS_EXTS = ('.js', '.mjs', '.css', '.html', '.svg', '.json', '.webmanifest')


def log(message):
    print(f'[auto-update] {message}', flush=True)


def fetch(url, timeout=30):
    request = urllib.request.Request(
        url,
        headers={
            'User-Agent': 'configflow-auto-update',
            'Accept': 'application/vnd.github+json',
        },
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return response.read()


def _is_git_source(repo):
    return (
        '://' in repo
        or repo.startswith('/')
        or repo.startswith('./')
        or repo.startswith('../')
        or repo.endswith('.git')
    )


def _find_go():
    if os.path.exists('/usr/local/go/bin/go'):
        return '/usr/local/go/bin/go'
    return shutil.which('go')


def _target_goarch():
    machine = platform.machine()
    arch = {
        'x86_64': 'amd64',
        'amd64': 'amd64',
        'aarch64': 'arm64',
        'arm64': 'arm64',
        'armv7l': 'arm',
        'armv6l': 'arm',
    }.get(machine)
    if not arch:
        return None, None
    extra = {}
    if arch == 'arm':
        extra['GOARM'] = '6' if machine == 'armv6l' else '7'
    return arch, extra


def _go_env(extra=None):
    go_bin_dir = os.path.dirname(_find_go() or '/usr/local/go/bin/go')
    env = {
        'CGO_ENABLED': '0',
        'GOOS': 'linux',
        'GOTOOLCHAIN': 'local',
        'GOPROXY': os.environ.get('GOPROXY', 'https://goproxy.cn,direct'),
        'GOCACHE': os.path.join(STATE, 'go-cache'),
        'GOMODCACHE': os.path.join(STATE, 'go-mod'),
        'PATH': go_bin_dir + ':' + os.environ.get('PATH', ''),
    }
    if extra:
        env.update(extra)
    return env


def _frontend_input(name):
    # Matches what reaches the image build: .dockerignore drops *.md and dotfiles.
    return not name.endswith('.md') and not name.startswith('.')


def _agent_input(name):
    return name.endswith('.go') or name in ('go.mod', 'go.sum')


def _server_input(name):
    if name.startswith('.') or name.endswith('_test.go') or name.endswith('.md') or name.endswith('.py'):
        return False
    return True


def tree_hash(path, include=_frontend_input):
    """Hash of the files that affect a build, to rebuild only what changed.

    Only build inputs count, so the fingerprint taken from the Docker build
    context equals the one taken from the fetched source archive.
    """
    digest = hashlib.sha256()
    for root, dirs, files in os.walk(path):
        dirs[:] = sorted(d for d in dirs if d not in ('node_modules', 'dist') and not d.startswith('.'))
        for name in sorted(n for n in files if include(n)):
            full = os.path.join(root, name)
            digest.update(os.path.relpath(full, path).encode())
            with open(full, 'rb') as handle:
                digest.update(hashlib.sha256(handle.read()).digest())
    return digest.hexdigest()


def server_tree_hash(path):
    """Hash Go server build inputs (go.mod, go.sum, cmd/, internal/ incl. go:embed assets)."""
    digest = hashlib.sha256()
    for root_file in ('go.mod', 'go.sum'):
        full = os.path.join(path, root_file)
        if os.path.isfile(full):
            digest.update(root_file.encode())
            with open(full, 'rb') as handle:
                digest.update(hashlib.sha256(handle.read()).digest())
    for subdir in ('cmd', 'internal'):
        base = os.path.join(path, subdir)
        if not os.path.isdir(base):
            continue
        for root, dirs, files in os.walk(base):
            dirs[:] = sorted(
                d for d in dirs
                if not d.startswith('.') and d not in SERVER_IGNORED_DIRS
            )
            for name in sorted(n for n in files if _server_input(n)):
                full = os.path.join(root, name)
                digest.update(os.path.relpath(full, path).encode())
                with open(full, 'rb') as handle:
                    digest.update(hashlib.sha256(handle.read()).digest())
    return digest.hexdigest()


def precompress_dir(directory):
    """Create deterministic .gz siblings for static assets >1 KiB (for Go Edge gzip_static)."""
    for root, _dirs, files in os.walk(directory):
        for name in files:
            if not name.endswith(PRECOMPRESS_EXTS):
                continue
            full = os.path.join(root, name)
            try:
                if os.path.getsize(full) <= 1024:
                    continue
            except OSError:
                continue
            with open(full, 'rb') as src, open(full + '.gz', 'wb') as raw:
                with gzip.GzipFile(filename='', mode='wb', compresslevel=9, fileobj=raw, mtime=0) as gz:
                    shutil.copyfileobj(src, gz)


def run(args, cwd, env=None, timeout=900):
    merged = dict(os.environ, **(env or {}))
    result = subprocess.run(
        args,
        cwd=cwd,
        env=merged,
        timeout=timeout,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f'{" ".join(args)} failed:\n{result.stdout[-2000:]}')
    return result.stdout


def load_manifest():
    try:
        with open(os.path.join(STATE, 'manifest.json')) as handle:
            manifest = json.load(handle)
    except (OSError, ValueError):
        return {}
    if not isinstance(manifest, dict):
        return {}
    if manifest.get('sha') and 'server' not in manifest:
        # Written by the Python-era image: its builds never included the Go
        # server, so reusing them would pair this image's server with a
        # frontend/Agent from another commit. Start over from the image build.
        log(f'ignoring build {manifest["sha"][:7]} recorded by the Python image; rebuilding for the Go server')
        return {}
    return manifest


def save_manifest(manifest):
    path = os.path.join(STATE, 'manifest.json')
    with open(path + '.tmp', 'w') as handle:
        json.dump(manifest, handle, indent=2)
    os.replace(path + '.tmp', path)


def resolve_remote_sha():
    if _is_git_source(REPO):
        if not shutil.which('git'):
            raise RuntimeError('git is not installed; cannot fetch from git source')
        out = run(['git', 'ls-remote', '--heads', REPO, f'refs/heads/{BRANCH}'], STATE, timeout=30)
        for line in out.splitlines():
            parts = line.split()
            if len(parts) >= 2 and parts[1] == f'refs/heads/{BRANCH}':
                return parts[0]
        raise RuntimeError(f'branch {BRANCH!r} not found in {REPO}')
    return json.loads(fetch(f'https://api.github.com/repos/{REPO}/commits/{BRANCH}'))['sha']


def legacy_pre_sqlite_file(data_dir):
    """Return the first pre-SQLite config file when configflow.sqlite3 is absent, else ''."""
    if os.path.lexists(os.path.join(data_dir, DATABASE_FILENAME)):
        return ''
    for name in ('config.json', 'config.json.bak', 'system.json', 'system.json.bak'):
        if os.path.lexists(os.path.join(data_dir, name)):
            return name
    profiles_dir = os.path.join(data_dir, 'profiles')
    for pattern in ('*/config.json', '*/config.json.bak'):
        matches = sorted(glob.glob(os.path.join(profiles_dir, pattern)))
        if matches:
            return os.path.relpath(matches[0], data_dir)
    return ''


def download(sha):
    source = os.path.join(STATE, f'src-{sha}')
    if os.path.isdir(source):
        return source
    staging = source + '.tmp'
    shutil.rmtree(staging, ignore_errors=True)
    if _is_git_source(REPO):
        os.makedirs(staging)
        run(['git', 'init', '-q'], staging, timeout=30)
        ref = f'refs/heads/{BRANCH}'
        try:
            run(['git', 'fetch', '--depth', '1', REPO, ref], staging, timeout=180)
        except RuntimeError:
            # Dumb HTTP git servers do not support shallow fetches; fall back to full fetch.
            run(['git', 'fetch', REPO, ref], staging, timeout=300)
        run(['git', 'checkout', '-q', '--detach', sha], staging, timeout=60)
        shutil.rmtree(os.path.join(staging, '.git'), ignore_errors=True)
        os.replace(staging, source)
        return source
    archive = os.path.join(STATE, f'{sha}.tar.gz')
    with open(archive, 'wb') as handle:
        handle.write(fetch(f'https://codeload.github.com/{REPO}/tar.gz/{sha}', timeout=120))
    with tarfile.open(archive) as bundle:
        try:
            bundle.extractall(staging, filter='data')
        except TypeError:  # Python without extraction filters
            bundle.extractall(staging)
    os.remove(archive)
    (top,) = os.listdir(staging)  # codeload wraps everything in <repo>-<sha>/
    os.replace(os.path.join(staging, top), source)
    shutil.rmtree(staging, ignore_errors=True)
    return source


def image_trees():
    try:
        with open(IMAGE_TREES) as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return {}


class MissingToolchain(Exception):
    """A changed component cannot be built here; updating would mix versions."""


def build_server(source, manifest, sha):
    server_dir = os.path.join(source, 'server')
    if not os.path.isfile(os.path.join(server_dir, 'go.mod')):
        raise RuntimeError('source does not contain server/go.mod')
    key = server_tree_hash(server_dir)
    if key == image_trees().get('server'):
        manifest['server'] = None  # unchanged: reuse the image's bundled binary
        return None
    if not _find_go():
        raise MissingToolchain('Go 后端有改动但镜像里没有 Go')
    arch, extra = _target_goarch()
    if not arch:
        raise MissingToolchain(f'此 CPU 架构 ({platform.machine()}) 尚未支持自动重建 Go 服务')
    out = os.path.join(STATE, f'server-{key[:16]}-{platform.machine()}')
    binary = os.path.join(out, 'configflow')
    if not os.path.isfile(binary):
        log('building Go server binary')
        staging = out + '.tmp'
        shutil.rmtree(staging, ignore_errors=True)
        os.makedirs(staging)
        version_label = f'{BRANCH}-{sha[:7]}'
        env = _go_env(dict(extra, GOARCH=arch))
        run(
            [
                'go', 'build', '-trimpath',
                f'-ldflags=-s -w -X configflow/internal/version.Version={version_label}',
                '-o', os.path.join(staging, 'configflow'),
                './cmd/configflow',
            ],
            server_dir,
            env=env,
        )
        os.chmod(os.path.join(staging, 'configflow'), 0o755)
        os.replace(staging, out)
    manifest['server'] = out
    return out


def build_frontend(source, manifest):
    frontend = os.path.join(source, 'frontend')
    key = tree_hash(frontend)
    if key == image_trees().get('frontend'):
        manifest['frontend'] = None  # unchanged: reuse the image's bundled frontend
        return None
    if not shutil.which('npm'):
        raise MissingToolchain('前端有改动但镜像里没有 npm')
    out = os.path.join(STATE, f'frontend-{key[:16]}')
    lock_key = hashlib.sha256(open(os.path.join(frontend, 'package-lock.json'), 'rb').read()).hexdigest()[:16]
    # Node resolves packages by walking up to directories literally named
    # node_modules (on the symlink's real path), so keep that name.
    modules = os.path.join(STATE, f'deps-{lock_key}', 'node_modules')
    manifest['deps'] = os.path.dirname(modules)  # kept by prune even when the build is reused
    if not os.path.isdir(out):
        if not os.path.isdir(modules):
            log('installing frontend dependencies (package-lock changed)')
            run(['npm', 'ci', '--no-audit', '--no-fund', '--cache', os.path.join(STATE, 'npm-cache')], frontend)
            os.makedirs(os.path.dirname(modules), exist_ok=True)
            shutil.move(os.path.join(frontend, 'node_modules'), modules)
        node_modules_link = os.path.join(frontend, 'node_modules')
        if not os.path.exists(node_modules_link):
            os.symlink(modules, node_modules_link)
        log('building frontend')
        run(['npm', 'run', 'build'], frontend)
        staging = out + '.tmp'
        shutil.rmtree(staging, ignore_errors=True)
        shutil.copytree(os.path.join(frontend, 'dist'), staging)
        precompress_dir(staging)
        os.replace(staging, out)
    manifest['frontend'] = out
    return out


def build_agents(source, manifest):
    agent = os.path.join(source, 'backend', 'agents', 'go-agent')
    key = tree_hash(agent, _agent_input)
    if key == image_trees().get('agents'):
        manifest['agents'] = None  # unchanged: reuse the image's bundled binaries
        return None
    if not _find_go():
        raise MissingToolchain('Agent 有改动但镜像里没有 Go')
    out = os.path.join(STATE, f'agents-{key[:16]}')
    if not os.path.isdir(out):
        log('building Agent binaries')
        staging = out + '.tmp'
        shutil.rmtree(staging, ignore_errors=True)
        os.makedirs(staging)
        env = _go_env()
        for name, extra in ARCHES:
            arch_env = dict(env, GOARCH=name)
            arch_env.update(extra)  # armv7 is GOARCH=arm with GOARM=7
            run(
                ['go', 'build', '-trimpath', '-ldflags=-s -w',
                 '-o', os.path.join(staging, f'configflow-agent-linux-{name}'), '.'],
                agent,
                env=arch_env,
            )
        os.replace(staging, out)
    manifest['agents'] = out
    return out


def build_node_probe(source, manifest):
    probe = os.path.join(source, 'backend', 'node-probe')
    if not os.path.isfile(os.path.join(probe, 'go.mod')):
        manifest['node_probe'] = None
        return None
    key = tree_hash(probe, _agent_input)
    if key == image_trees().get('node_probe'):
        manifest['node_probe'] = None
        return None
    if not _find_go():
        raise MissingToolchain('独立测速组件有改动但镜像里没有 Go')
    arch, extra = _target_goarch()
    if not arch:
        raise MissingToolchain('此 CPU 架构尚未支持独立测速组件')
    out = os.path.join(STATE, f'node-probe-{key[:16]}-{platform.machine()}')
    if not os.path.isfile(os.path.join(out, 'configflow-node-probe')):
        log('building independent node latency worker')
        staging = out + '.tmp'
        shutil.rmtree(staging, ignore_errors=True)
        os.makedirs(staging)
        env = _go_env(dict(extra, GOARCH=arch))
        run(
            ['go', 'build', '-trimpath', '-ldflags=-s -w',
             '-o', os.path.join(staging, 'configflow-node-probe'), '.'],
            probe,
            env=env,
        )
        for notice in ('LICENSE', 'NOTICE'):
            if os.path.isfile(os.path.join(probe, notice)):
                shutil.copy2(os.path.join(probe, notice), staging)
        os.replace(staging, out)
    manifest['node_probe'] = out
    return out


def replace_dir(src, dst):
    # Directories from image layers cannot be renamed on overlayfs (EXDEV), so
    # delete and copy. Services are not running yet; a failure here is undone
    # by restoring the preserved image copy.
    if os.path.isdir(dst):
        shutil.rmtree(dst)
    shutil.copytree(src, dst, symlinks=True)


def replace_file(src, dst, mode=0o755):
    tmp = dst + '.tmp'
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copy2(src, tmp)
    os.chmod(tmp, mode)
    os.replace(tmp, dst)


def preserve_image():
    """Keep the image's own artifacts once per container, so a failed update can return to them."""
    if os.path.isdir(IMAGE_COPY):
        return
    staging = IMAGE_COPY + '.tmp'
    shutil.rmtree(staging, ignore_errors=True)
    os.makedirs(staging)
    if os.path.isfile(SERVER_BIN):
        shutil.copy2(SERVER_BIN, os.path.join(staging, 'configflow'))
    for name, path in (('html', HTML), ('agents', AGENTS), ('node_probe', NODE_PROBE)):
        if os.path.isdir(path):
            shutil.copytree(path, os.path.join(staging, name), symlinks=True)
    os.replace(staging, IMAGE_COPY)


def restore_image():
    saved_bin = os.path.join(IMAGE_COPY, 'configflow')
    if os.path.isfile(saved_bin):
        replace_file(saved_bin, SERVER_BIN)
    for name, path in (('html', HTML), ('agents', AGENTS), ('node_probe', NODE_PROBE)):
        saved = os.path.join(IMAGE_COPY, name)
        if os.path.isdir(saved):
            replace_dir(saved, path)


def install(manifest):
    """Install the recorded build over the image (also after a container recreate)."""
    if not manifest.get('sha'):
        restore_image()
        return False
    server_dir = manifest.get('server')
    if server_dir and os.path.isfile(os.path.join(server_dir, 'configflow')):
        replace_file(os.path.join(server_dir, 'configflow'), SERVER_BIN)
    elif os.path.isfile(os.path.join(IMAGE_COPY, 'configflow')):
        replace_file(os.path.join(IMAGE_COPY, 'configflow'), SERVER_BIN)
    # A component without its own build uses the image's copy, never a stale
    # build left over from an earlier commit.
    for key, path, saved in (('frontend', HTML, 'html'), ('agents', AGENTS, 'agents'), ('node_probe', NODE_PROBE, 'node_probe')):
        if manifest.get(key) and os.path.isdir(manifest[key]):
            replace_dir(manifest[key], path)
        elif os.path.isdir(os.path.join(IMAGE_COPY, saved)):
            replace_dir(os.path.join(IMAGE_COPY, saved), path)
    return True


def prune(manifest):
    """Drop sources and builds the manifest no longer points at (caches stay)."""
    keep = {os.path.basename(manifest.get(k) or '') for k in ('source', 'server', 'frontend', 'agents', 'node_probe', 'deps')}
    for name in os.listdir(STATE):
        path = os.path.join(STATE, name)
        built = name.startswith(('src-', 'server-', 'frontend-', 'agents-', 'node-probe-', 'deps-'))
        leftover = name.endswith(('.tmp', '.tar.gz'))
        if (built and name not in keep) or leftover:
            if os.path.isdir(path):
                shutil.rmtree(path, ignore_errors=True)
            else:
                os.remove(path)


def record_image(root):
    """Build time: fingerprint the sources the image's server/frontend/Agent/node-probe were built from."""
    trees = {
        'server': server_tree_hash(os.path.join(root, 'server')),
        'frontend': tree_hash(os.path.join(root, 'frontend')),
        'agents': tree_hash(os.path.join(root, 'go-agent'), _agent_input),
        'node_probe': tree_hash(os.path.join(root, 'node-probe'), _agent_input),
    }
    with open(IMAGE_TREES, 'w') as handle:
        json.dump(trees, handle)
    print(f'recorded image source fingerprints: {trees}')


def main():
    if len(sys.argv) == 3 and sys.argv[1] == '--record-image':
        record_image(sys.argv[2])
        return
    if len(sys.argv) == 3 and sys.argv[1] == '--precompress':
        precompress_dir(sys.argv[2])
        return
    if os.environ.get('CONFIGFLOW_AUTO_UPDATE') != '1':
        return
    legacy = legacy_pre_sqlite_file(DATA_DIR)
    if legacy:
        log(
            f'检测到数据目录中仅有旧版（SQLite 之前）配置文件（{legacy}），且没有 {DATABASE_FILENAME}；'
            '已跳过自动更新并保持镜像自带版本。请先使用最后一个 Python 版本的 ConfigFlow 完成数据迁移，再升级到此版本。'
        )
        if os.path.isdir(IMAGE_COPY):
            restore_image()
        return
    os.makedirs(STATE, exist_ok=True)
    preserve_image()
    manifest = load_manifest()
    started = time.time()
    try:
        sha = resolve_remote_sha()
    except Exception as error:  # noqa: BLE001 - offline must not block startup
        log(f'cannot reach update source ({error.__class__.__name__}: {error}); using {manifest.get("sha", "image")[:7]}')
        install(manifest)
        return
    if sha == manifest.get('sha'):
        log(f'{BRANCH} is at {sha[:7]}, already built')
        install(manifest)
        return
    log(f'updating to {REPO}@{BRANCH} {sha[:7]}')
    candidate = dict(manifest)
    try:
        candidate['source'] = download(sha)
        build_server(candidate['source'], candidate, sha)
        build_frontend(candidate['source'], candidate)
        build_agents(candidate['source'], candidate)
        build_node_probe(candidate['source'], candidate)
        candidate['sha'] = sha
        install(candidate)
        save_manifest(candidate)
        prune(candidate)
        log(f'running {sha[:7]} (took {time.time() - started:.0f}s)')
    except MissingToolchain as error:
        log(
            f'skipped {sha[:7]}: {error}; use the autoupdate image to build it. '
            f'Keeping {manifest.get("sha", "image")[:7]} so components stay in step'
        )
        install(manifest)
        prune(manifest)
    except Exception as error:  # noqa: BLE001 - fall back, never block startup
        log(f'update failed, keeping {manifest.get("sha", "image")[:7]}: {error}')
        try:
            install(manifest)
        except Exception as restore_error:  # noqa: BLE001
            log(f'restoring the previous build failed ({restore_error}); using the image build')
            restore_image()


if __name__ == '__main__':
    main()
