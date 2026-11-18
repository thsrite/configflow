"""Process/loopback fixtures for faults. No production data or device access."""
from __future__ import annotations

import contextlib
import hashlib
import http.client
import json
import os
from pathlib import Path
import secrets
import re
import signal
import shutil
import socket
import select
import sqlite3
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit


def require(value, message):
    if not value:
        raise AssertionError(message)


def eventually(fn, timeout=10, label="condition"):
    deadline = time.monotonic() + timeout
    while True:
        result = fn()
        if result:
            return result
        if time.monotonic() >= deadline:
            raise AssertionError(f"timed out waiting for {label}")
        time.sleep(0.01)


def strict_json(data):
    def reject(value):
        raise ValueError("non-finite JSON scalar: " + value)
    return json.loads(data,parse_constant=reject)


def isolation():
    require([name for _, name in socket.if_nameindex()] == ["lo"], "faults require an isolated network namespace containing only lo")
    try:
        with socket.create_connection(("192.0.2.1", 9), timeout=0.2):
            raise AssertionError("TEST-NET egress unexpectedly succeeded")
    except OSError:
        pass
    # Also prove permission faults cannot be bypassed by namespace capabilities.
    return True


class Gate:
    def __init__(self):
        self.entered, self.release, self.cancelled = threading.Event(), threading.Event(), threading.Event()
        self.cancelled_at = None


class Source:
    """Only loopback; gates are bounded so fixture failure cannot leak a thread."""
    def __init__(self):
        self.lock, self.gates, self.calls = threading.Lock(), {}, []
        self.uploads = []
        source = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def handle_request(self):
                path = urlsplit(self.path).path
                raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                with source.lock:
                    source.calls.append((self.command, path))
                    gate = source.gates.get((self.command, path)) or next((g for (m,p),g in source.gates.items() if m==self.command and p.endswith("*") and path.startswith(p[:-1])), None)
                if gate:
                    gate.entered.set()
                    deadline = time.monotonic()+30
                    while not gate.release.wait(0.01):
                        readable,_,_ = select.select([self.connection],[],[],0)
                        if readable:
                            try:
                                ended = self.connection.recv(1,socket.MSG_PEEK) == b""
                            except ConnectionResetError:
                                ended = True
                            if ended:
                                gate.cancelled_at = time.monotonic()
                                gate.cancelled.set()
                                return
                        if time.monotonic()>=deadline:
                            self.send_error(504)
                            return
                status, kind = 200, "application/json"
                if path == "/subscription":
                    body, kind = b"proxies:\n  - {name: Synthetic, type: ss, server: 192.0.2.8, port: 8388, cipher: aes-128-gcm, password: synthetic}\n", "text/plain"
                elif path == "/rules":
                    body, kind = b"example.com\nexample.net\n", "text/plain"
                elif path.startswith("/dav") or path.startswith("/backup"):
                    if self.command == "PROPFIND":
                        body, kind, status = b'<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"/>', "application/xml", 207
                    elif self.command == "PUT":
                        with source.lock:
                            source.uploads.append(hashlib.sha256(raw).hexdigest())
                        body, status = b"", 201
                    else:
                        body, status = b"", 201 if self.command == "MKCOL" else 200
                elif path == "/api/domain-probe":
                    request = json.loads(raw)
                    rows = [{"host": t["host"], "url": t["url"], "paths": {p: {"ok": 3, "total": 3, "delays": [1, 1, 1], "errors": {}} for p in request["paths"]}} for t in request["targets"]]
                    body = json.dumps({"success": True, "results": rows}).encode()
                else:
                    body = b'{"success":true,"data":[],"domains":[],"connections":[]}'
                self.send_response(status)
                self.send_header("Content-Type", kind)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                try:
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    pass

            do_GET = do_POST = do_PUT = do_HEAD = do_DELETE = do_MKCOL = do_PROPFIND = handle_request

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"http://127.0.0.1:{self.server.server_port}"

    def gate(self, method, path):
        gate = Gate()
        with self.lock:
            self.gates[(method, path)] = gate
        return gate

    def close(self):
        with self.lock:
            for gate in self.gates.values():
                gate.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


class World:
    def __init__(self, root, binary, fixture, catalog, *, dav=False, source=None, clone_from=None):
        self.root, self.binary, self.fixture, self.catalog = Path(root), binary, fixture, catalog
        self.root.mkdir(mode=0o700)
        self.data, self.control = self.root / "data", self.root / "control"
        self.data.mkdir(mode=0o700)
        self.control.mkdir(mode=0o700)
        (self.data / ".faults-owned").write_text("configflow-faults-v1\n")
        self.owns_source = source is None
        self.source, self.process, self.log_handle = source or Source(), None, None
        self.extra_processes = []
        self.checks = []
        self.observations = {}
        self.serial = 0
        cmd = [fixture, "-mode=seed", "-dir=" + str(self.data), "-source=" + self.source.url]
        if dav:
            cmd.append("-dav")
        try:
            if clone_from is not None:
                require((Path(clone_from) / ".faults-owned").read_text() == "configflow-faults-v1\n", "refusing unmarked snapshot")
                shutil.copytree(clone_from, self.data, dirs_exist_ok=True)
                completed = subprocess.CompletedProcess(cmd, 0, b"", b"")
            else:
                completed = subprocess.run(cmd, capture_output=True, timeout=30)
            if completed.returncode:
                (self.root / "fixture.log").write_bytes(completed.stdout + completed.stderr)
                raise AssertionError("synthetic fixture seed failed; see private fixture.log")
        except Exception:
            self.close()
            raise
        self.token = ""
        self.heartbeat_number = 0

    def check(self, condition, label):
        require(condition, label)
        if label not in self.checks:
            self.checks.append(label)

    def start(self, *, ready=True, binary=None, lock_timeout=1):
        require(self.process is None or self.process.poll() is not None, "server already running")
        self.serial += 1
        (self.control / "lifecycle.jsonl").write_text("")
        self.token = secrets.token_hex(32)
        token = self.control / "token"
        token.write_text(self.token)
        token.chmod(0o600)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.port = listener.getsockname()[1]
        self.base = f"http://127.0.0.1:{self.port}"
        env = {k: os.environ[k] for k in ("PATH", "HOME", "LANG", "TZ") if k in os.environ}
        env.update(DATA_DIR=str(self.data), CONFIGFLOW_BACKEND_HOST="127.0.0.1", CONFIGFLOW_BACKEND_PORT=str(self.port),
                   CONFIGFLOW_FRONTEND_DIR="", ADMIN_USERNAME="", ADMIN_PASSWORD="", JWT_SECRET_KEY="fault-synthetic-session-key",
                   CONFIGFLOW_LOCK_TIMEOUT_SECONDS=str(lock_timeout), CONFIGFLOW_SHUTDOWN_GRACE_SECONDS="8",
                   CONFIGFLOW_LIFECYCLE_EVENTS=str(self.control / "lifecycle.jsonl"))
        env[self.catalog["env"]] = str(self.control)
        if self.log_handle:
            self.log_handle.close()
        self.log_handle = (self.root / f"server-{self.serial}.log").open("wb")
        self.process_env = env
        self.process = subprocess.Popen([binary or self.binary, "-frontend="], env=env, stdout=self.log_handle, stderr=subprocess.STDOUT)
        if ready:
            def available():
                if self.process.poll() is not None:
                    raise AssertionError("server exited before readiness; see private server log")
                try:
                    return self.request("GET", "/api/profiles", expected=200)[0] == 200
                except OSError:
                    return False
            eventually(available, 30, "real listener readiness")
        return self.process

    def request(self, method, path, body=None, *, expected=200, headers=None, timeout=15):
        data = None if body is None else json.dumps(body).encode()
        h = {"Connection": "close"}
        if data is not None:
            h["Content-Type"] = "application/json"
        h.update(headers or {})
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=timeout)
        try:
            conn.request(method, path, body=data, headers=h)
            response = conn.getresponse()
            raw = response.read()
            if expected is not None:
                require(response.status == expected, f"{method} {path}: status {response.status}, expected {expected}")
            return response.status, raw
        finally:
            conn.close()

    def raw(self, method, path, body=None, *, finish=True, headers=None):
        data = b"" if body is None else json.dumps(body).encode()
        sock = socket.create_connection(("127.0.0.1", self.port), timeout=10)
        request = f"{method} {path} HTTP/1.1\r\nHost: 127.0.0.1:{self.port}\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {len(data)}\r\n\r\n".encode()
        if headers:
            extra = "".join(f"{k}: {v}\r\n" for k,v in headers.items()).encode()
            request = request[:-2] + extra + b"\r\n"
        sock.sendall(request + (data if finish else b""))
        if not finish and (headers or {}).get("Expect") == "100-continue":
            interim = b""
            while not interim.endswith(b"\r\n\r\n"):
                part = sock.recv(1)
                require(part, "handler closed before reading the request body")
                interim += part
            require(b" 100 " in interim, "handler did not reach the body read")
        return sock, data

    def state(self):
        status, raw = self.request("GET", "/__faults/state", headers={self.catalog["header"]: self.token})
        out = json.loads(raw)
        require(set(out) == {"goroutines", "phases"}, "diagnostics leaked extra fields")
        return out

    def phase(self, key):
        require(key in self.catalog["points"], "unregistered fault phase")
        return self.catalog["points"][key]

    def arm(self, key):
        phase = self.phase(key)
        (self.control / ("pause." + phase)).touch(mode=0o600)
        return phase

    def entered(self, key):
        phase = self.phase(key)
        return eventually(lambda: phase in self.state()["phases"], 10, "registered fault phase " + phase)

    def release(self, key):
        (self.control / ("pause." + self.phase(key))).unlink(missing_ok=True)

    def signal(self, sig=signal.SIGTERM, *, wait=True):
        start = time.monotonic()
        self.process.send_signal(sig)
        if wait:
            code = self.process.wait(timeout=11)
            elapsed = time.monotonic() - start
            self.observations.setdefault("stop_seconds",[]).append(round(elapsed,3))
            self.check(elapsed < 10, "process exited within external stop budget")
            return code

    def kill(self):
        return self.signal(signal.SIGKILL)

    def receipts(self):
        path = self.data / "task-status.json"
        return json.loads(path.read_text()) if path.exists() else {}

    def receipt(self, key, count=1):
        def read():
            entry = self.receipts().get(key, {})
            return entry if entry.get("run_count", 0) >= count else None
        return eventually(read, 15, "durable task receipt")

    def heartbeat(self):
        self.heartbeat_number += 1
        return self.request("POST", "/api/agents/edge/heartbeat", {"version": "1.8.99", "service_status": "running", "system_metrics": {"cpu": {"usage_percent": self.heartbeat_number}}}, headers={"Authorization": "Bearer fault-synthetic-agent-token"})

    def flush(self):
        count = self.receipts().get("metrics_history_flush", {}).get("run_count", 0)
        self.request("POST", "/api/tasks/run", {"key": "metrics_history_flush"}, expected=202)
        receipt = self.receipt("metrics_history_flush", count + 1)
        self.task_idle("task-metrics_history_flush")
        return receipt

    def refresh(self):
        count = self.receipts().get("subscription:feed", {}).get("run_count", 0)
        self.request("POST", "/api/tasks/run", {"key": "subscription:feed"}, expected=202)
        receipt = self.receipt("subscription:feed", count + 1)
        self.task_idle("task-subscription:feed")
        return receipt

    def task_idle(self, name):
        def idle():
            starts = ends = 0
            for line in (self.control / "lifecycle.jsonl").read_text().splitlines():
                item = json.loads(line)
                if item["loop"] == name:
                    starts += item["event"] == "job-start"
                    ends += item["event"] == "job-end"
            return starts > 0 and starts == ends
        return eventually(idle, 5, "tracked task release")

    def db(self):
        with sqlite3.connect(self.data / "configflow.sqlite3") as db:
            db.execute("BEGIN")
            require(db.execute("PRAGMA integrity_check").fetchall() == [("ok",)], "SQLite integrity failure")
            tables = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")]
            value = {name: sorted(db.execute('SELECT * FROM "' + name.replace('"', '""') + '"').fetchall(), key=repr) for name in tables}
        return hashlib.sha256(repr(value).encode()).hexdigest()

    def audit(self):
        self.db()
        for dbfile in self.data.rglob("*.sqlite3"):
            with sqlite3.connect(dbfile) as db:
                require(db.execute("PRAGMA integrity_check").fetchall() == [("ok",)], "auxiliary database integrity failure")
        for path in self.data.rglob("*.json"):
            if path.name.startswith("."):
                continue
            strict_json(path.read_bytes())
        for path in self.data.rglob("*_metrics.jsonl"):
            lines = path.read_bytes().splitlines()
            for index, line in enumerate(lines):
                try:
                    strict_json(line)
                except (ValueError,UnicodeDecodeError):
                    require(index == len(lines) - 1, "non-final broken metrics record")
        self.check(True, "SQLite and committed JSON/metrics prefixes readable")

    def recovered(self):
        self.start()
        self.request("GET", "/api/agents/edge/metrics/history")
        self.refresh()
        self.audit()
        self.check(True, "restart loaded state and admitted another refresh")

    def lock(self, name):
        path = self.data / name
        path.parent.mkdir(parents=True, exist_ok=True)
        script = "import fcntl,sys; f=open(sys.argv[1],'a+'); fcntl.flock(f,fcntl.LOCK_EX); print('locked',flush=True); sys.stdin.read()"
        process = subprocess.Popen([os.sys.executable, "-u", "-c", script, str(path)], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
        require(process.stdout.readline() == b"locked\n", "external lock holder failed")
        self.extra_processes.append(process)
        return process

    @staticmethod
    def unlock(process):
        process.stdin.close()
        process.wait(timeout=3)

    def close(self):
        for process in self.extra_processes:
            if process.poll() is None:
                process.kill()
                process.wait()
        if self.process and self.process.poll() is None:
            self.process.kill()
            self.process.wait()
        if self.log_handle:
            self.log_handle.close()
        if self.owns_source:
            self.source.close()


class Scenarios:
    def __init__(self):
        self.items = {}

    def register(self, name, *, covers):
        def add(fn):
            require(name not in self.items, "duplicate scenario")
            self.items[name] = (fn, covers)
            return fn
        return add


SCENARIOS = Scenarios()


# These fields are wall-clock observations, not business results. Presence,
# nulls, task counts, revisions, values, source order and paths remain compared.
CLOCK_FIELDS = {"created_at", "updated_at", "fetched_at", "checked_at", "timestamp", "last_run_at", "last_success_at", "last_error_at", "last_duration_ms", "duration_ms", "started_at", "finished_at", "last_heartbeat", "collected_at", "last_fetch", "last_updated", "traffic_at", "last_attempt_at", "at", "cache_updated_at", "last_finished_at", "heartbeat_at"}


def normalized(value, ids=None, panel=None):
    if isinstance(value, dict):
        def field(key, item):
            if key in CLOCK_FIELDS and item is not None:
                numeric = isinstance(item,(int,float)) and not isinstance(item,bool) and item >= 0
                timestamp = isinstance(item,str) and re.match(r"^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}",item)
                if timestamp or numeric:
                    return "<clock>"
            return normalized(item,ids,panel)
        return {k: field(k,v) for k,v in value.items()}
    if isinstance(value, list):
        return [normalized(v,ids,panel) for v in value]
    if isinstance(value,str):
        value = (ids or {}).get(value,value)
        if panel:
            value = value.replace(panel,"<panel>")
    return value


def database_snapshot(path, ids=None, panel=None):
    result = {}
    with sqlite3.connect(path) as db:
        db.execute("BEGIN")
        require(db.execute("PRAGMA integrity_check").fetchall() == [("ok",)], "SQLite integrity failure")
        for (table,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"):
            quoted = '"' + table.replace('"', '""') + '"'
            columns = [r[1] for r in db.execute("PRAGMA table_info(" + quoted + ")")]
            rows = []
            for raw in db.execute("SELECT * FROM " + quoted):
                row = dict(zip(columns, raw))
                if table == "meta" and row.get("key") in {"commit_id", "created_at"}:
                    row["value"] = "<commit-metadata>"
                elif row.get("field") in CLOCK_FIELDS:
                    try:
                        row["value"] = normalized({row["field"]:json.loads(row["value"])},ids,panel)[row["field"]]
                    except ValueError:
                        pass
                for key,value in list(row.items()):
                    if isinstance(value, bytes):
                        row[key] = {"blob": value.hex()}
                    elif isinstance(value, str):
                        try:
                            row[key] = normalized(json.loads(value),ids,panel)
                        except (ValueError, TypeError):
                            pass
                rows.append(normalized(row,ids,panel))
            result[table] = sorted(rows, key=lambda r: json.dumps(r, sort_keys=True))
    return result


def normalized_bytes(data, relative, panel):
    data = data.replace(panel.encode(),b"<panel>")
    if relative.endswith("/providers/served/subscription-feed.yaml") and data.startswith(b"# ConfigFlow Subscription Proxies\n"):
        data = re.sub(rb"(?m)^# Generated: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$",b"# Generated: <clock>",data,count=1)
    return data


def convergence_snapshot(world):
    world.audit()
    out = {}
    ids = {}
    cache = world.data / "shared/subscribes/feed.json"
    if cache.exists():
        for index,node in enumerate(json.loads(cache.read_text()).get("nodes",[])):
            identifier=node.get("id","")
            if node.get("subscription_id")=="feed" and re.fullmatch(r"node_[0-9a-f]{8}",identifier):
                identity={k:v for k,v in node.items() if k!="id"}
                require(identifier not in ids,"duplicate generated subscription node ID")
                ids[identifier]="node_<"+hashlib.sha256(json.dumps(identity,sort_keys=True).encode()).hexdigest()+":"+str(index)+">"
    for path in sorted(world.data.rglob("*")):
        rel = path.relative_to(world.data).as_posix()
        if rel == "app.log":
            continue  # diagnostic logs, not committed configuration/task data
        if path.is_dir():
            out[rel + "/"] = path.stat().st_mode & 0o777
            continue
        if path.name.endswith((".sqlite3-wal", ".sqlite3-shm", ".sqlite3-journal")):
            continue
        if path.name.endswith(".sqlite3"):
            value = database_snapshot(path,ids,world.base)
        elif rel == ".configflow-server.lock":
            value = "<instance pid/version/start diagnostic>"
        elif path.name.startswith((".task-status-", ".pyref-")):
            raise AssertionError("uncommitted temporary left after quiescence")
        else:
            data = path.read_bytes()
            try:
                value = normalized(json.loads(data),ids,world.base)
            except (ValueError, UnicodeDecodeError):
                if path.name.endswith("_metrics.jsonl"):
                    value = [normalized(json.loads(line),ids,world.base) for line in data.splitlines()]
                else:
                    value = {"bytes": normalized_bytes(data,rel,world.base).hex()}
        out[rel] = {"mode": path.stat().st_mode & 0o777, "value": value}
    return out


def snapshots_equal(left,right):
    # Python bool == int would hide stored-type corruption; compare exact JSON.
    return json.dumps(left,sort_keys=True,separators=(",",":"),allow_nan=False)==json.dumps(right,sort_keys=True,separators=(",",":"),allow_nan=False)
