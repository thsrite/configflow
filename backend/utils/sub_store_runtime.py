"""Lifecycle of the bundled Sub-Store, shared by all API worker processes.

Conversions hold a shared file lock through temporary-subscription cleanup.
Stopping/updating takes the exclusive lock; a second lock serializes startup.
External Sub-Store instances are never started or stopped here.
"""
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time

from backend.utils.logger import get_logger

logger = get_logger(__name__)
IDLE_SECONDS = 300
SUPERVISOR_CONFIG = '/etc/supervisor/conf.d/supervisord.conf'


class RuntimeUnavailable(RuntimeError):
    pass


def get_mode():
    # The idle monitor asks every few seconds: read the settings alone, not the
    # whole system section with every Agent's deployment history.
    from backend.common.config import get_repository
    mode = get_repository().get_system_settings().get('sub_store_mode')
    return mode if mode in ('on_demand', 'always') else 'on_demand'


def _save_mode(mode):
    from backend.common.config import get_repository, reset_config_context
    get_repository().update_system_transaction(
        lambda system: system['system_config'].update(sub_store_mode=mode))
    reset_config_context()


class SubStoreRuntime:
    def __init__(self, directory=None, idle_seconds=IDLE_SECONDS):
        self.directory = directory
        self.idle_seconds = idle_seconds
        self._thread_lock = threading.Lock()
        self._thread = None

    @property
    def root(self):
        if self.directory is None:
            from backend.common.config import DATA_DIR
            key = hashlib.sha256(os.path.abspath(DATA_DIR).encode()).hexdigest()[:12]
            return Path(tempfile.gettempdir()) / ('configflow-sub-store-' + key)
        return Path(self.directory)

    @contextmanager
    def _lock(self, name, exclusive=True, timeout=45):
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        with open(self.root / (name + '.lock'), 'a') as handle:
            deadline = time.monotonic() + timeout
            operation = fcntl.LOCK_EX if exclusive else fcntl.LOCK_SH
            while True:
                try:
                    fcntl.flock(handle, operation | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise RuntimeUnavailable('Sub-Store 正忙或正在更新，请稍后重试')
                    time.sleep(0.05)
            try:
                yield
            finally:
                fcntl.flock(handle, fcntl.LOCK_UN)

    def _read(self):
        try:
            return json.loads((self.root / 'state.json').read_text())
        except (OSError, ValueError):
            return {}

    def _write(self, **changes):
        # Callers hold control.lock; readers see either complete snapshot.
        state = {**self._read(), **changes}
        temp = self.root / 'state.tmp'
        temp.write_text(json.dumps(state))
        temp.replace(self.root / 'state.json')

    def _command(self, action):
        try:
            result = subprocess.run(
                ['supervisorctl', '-c', SUPERVISOR_CONFIG, action, 'sub-store'],
                capture_output=True, text=True, timeout=20)
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise RuntimeUnavailable('无法管理内置 Sub-Store，请检查容器服务状态') from exc
        if action == 'status':
            parts = result.stdout.split()
            if len(parts) >= 2 and parts[0] == 'sub-store':
                return parts[1]
        elif result.returncode == 0:
            return result.stdout
        raise RuntimeUnavailable('Sub-Store 服务操作失败，请检查容器日志')

    def _ready(self):
        from backend.utils.dependencies import builtin_sub_store_url, fetch_running_version
        return bool(fetch_running_version(builtin_sub_store_url(), timeout=1))

    def _ensure_running(self):
        if self._ready():
            self._write(state='running', error='')
            return
        self._write(state='starting', error='')
        try:
            if self._command('status') not in ('RUNNING', 'STARTING'):
                self._command('start')
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if self._ready():
                    self._write(state='running', error='')
                    return
                time.sleep(0.2)
            raise RuntimeUnavailable('Sub-Store 启动后未就绪，请稍后重试或检查容器日志')
        except RuntimeUnavailable as exc:
            self._write(state='failed', error=str(exc))
            raise

    @contextmanager
    def use(self):
        from backend.utils.dependencies import is_builtin_available
        if not is_builtin_available():
            yield
            return
        with self._lock('work', exclusive=False):
            with self._lock('control'):
                self._ensure_running()
            try:
                yield
            finally:
                # Count idle time from completion, including failed conversions
                # and cleanup. A crashed worker releases its OS lock naturally.
                with self._lock('control'):
                    self._write(last_used=time.monotonic())

    @contextmanager
    def maintenance(self):
        with self._lock('work', timeout=90):
            with self._lock('control'):
                self._write(state='updating', error='')
            try:
                yield
            finally:
                with self._lock('control'):
                    ready = self._ready()
                    self._write(state='running' if ready else 'failed',
                                error='' if ready else 'Sub-Store 更新后无法连接，请检查容器日志',
                                last_used=time.monotonic())

    def set_mode(self, mode):
        from backend.utils.dependencies import is_builtin_available
        if mode not in ('on_demand', 'always'):
            raise ValueError('运行模式必须是 on_demand 或 always')
        if not is_builtin_available():
            raise RuntimeUnavailable('外部 Sub-Store 的运行模式由其部署环境管理')
        with self._lock('work', timeout=0):
            with self._lock('control'):
                if mode == 'always':
                    self._ensure_running()
                _save_mode(mode)
                self._write(last_used=time.monotonic())

    def tick(self):
        """Non-blocking idle sweep; observing status never calls this method."""
        from backend.utils.dependencies import is_builtin_available
        if not is_builtin_available():
            return
        try:
            with self._lock('work', timeout=0):
                with self._lock('control'):
                    if get_mode() == 'always':
                        self._ensure_running()
                        return
                    state = self._read()
                    now = time.monotonic()
                    if not state:
                        # Also handles upgrades from the former autostart image.
                        running = self._command('status') in ('RUNNING', 'STARTING')
                        self._write(state='running' if running else 'sleeping', last_used=now)
                    elif state.get('state') != 'sleeping' and now - state.get('last_used', 0) >= self.idle_seconds:
                        if self._command('status') not in ('STOPPED', 'EXITED', 'FATAL'):
                            self._command('stop')
                        # Retain a startup/update failure for the UI and retry.
                        self._write(state='failed' if state.get('error') else 'sleeping')
        except RuntimeUnavailable as exc:
            # Busy conversions/updates are normal, not failures.
            if '正忙' not in str(exc):
                logger.warning('%s', exc)

    def status(self, running):
        from backend.utils.dependencies import is_builtin_available
        managed = is_builtin_available()
        state = self._read() if managed else {}
        current = state.get('state')
        if current != 'updating':
            if running:
                current = 'running'
            elif current not in ('starting', 'failed'):
                current = 'sleeping' if managed and get_mode() == 'on_demand' and current in (None, 'sleeping') else 'failed'
        return {'managed': managed, 'mode': get_mode() if managed else 'external',
                'state': current, 'idle_seconds': self.idle_seconds,
                'error': state.get('error', '') if current == 'failed' else ''}

    def start_monitor(self):
        from backend.utils.dependencies import is_builtin_available
        if not is_builtin_available():
            return
        with self._thread_lock:
            if self._thread is not None and self._thread.is_alive():
                return
            self._thread = threading.Thread(target=self._monitor, name='sub-store-idle', daemon=True)
            self._thread.start()

    def _monitor(self):
        while True:
            try:
                self.tick()
            except Exception:
                logger.exception('Sub-Store 空闲检查失败')
            time.sleep(5)


runtime = SubStoreRuntime()
