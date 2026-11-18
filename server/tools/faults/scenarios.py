"""Built-in scenarios; HR owners extend the same registry, fixtures and checks."""
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import time

from core import SCENARIOS, eventually, require


@SCENARIOS.register("startup-exclusion", covers=["5.1"])
def startup_exclusion(env):
    w = env.world()
    w.start()
    w.refresh()
    w.request("POST", "/api/tasks/run", {"key": "rule_source:rulesource"}, expected=202)
    w.receipt("rule_source:rulesource")
    w.task_idle("task-rule_source:rulesource")
    before = w.db()
    other = env.spawn_contender(w)
    require(other.wait(timeout=33) != 0, "second server was admitted")
    w.check("正被另一个 ConfigFlow 服务使用" in env.contender_log.read_text(), "second server reports the native Chinese busy diagnostic")
    w.check(w.db() == before, "rejected instance did not change configuration")
    w.signal()
    holder = w.lock(".task-scheduler.lock")
    w.start(ready=False)
    require(w.process.wait(timeout=33) != 0, "Python-style scheduler holder was ignored")
    w.check("正被另一个 ConfigFlow 服务使用" in (w.root / f"server-{w.serial}.log").read_text(), "Python lifetime lock rejects Go startup")
    w.unlock(holder)
    w.recovered()


@SCENARIOS.register("signal-during-instance-wait", covers=["5.1", "5.2"])
def instance_wait(env):
    w = env.world()
    holder = w.lock(".task-scheduler.lock")
    w.start(ready=False)
    eventually(lambda: any(p.name == ".configflow-server.lock" for p in w.data.iterdir()), label="partial instance acquisition")
    started = time.monotonic()
    w.signal()
    w.check(time.monotonic() - started < 2, "SIGTERM interrupts instance wait without opening a listener")
    w.unlock(holder)
    w.recovered()


@SCENARIOS.register("sigterm-heartbeats", covers=["5.2"])
def heartbeat_shutdown(env):
    w = env.world()
    w.start()
    for _ in range(10):
        w.heartbeat()
    w.check(w.signal() == 0, "heartbeat burst shuts down cleanly")
    receipt = w.receipt("metrics_history_flush")
    w.check(receipt.get("last_trigger") == "shutdown" and receipt["run_count"] == 1, "first shutdown flush recorded; unchanged second flush is silent")
    w.recovered()
    history = json.loads(w.request("GET", "/api/agents/edge/metrics/history")[1])["data"]["history"]
    w.check([p["cpu"]["usage_percent"] for p in history] == list(range(1, 11)), "all graceful heartbeat points survive restart")


@SCENARIOS.register("sigterm-drain-new-metrics", covers=["5.2"])
def shutdown_drain(env):
    w = env.world()
    w.start()
    w.heartbeat()
    gate = w.source.gate("GET", "/subscription")
    w.request("POST", "/api/tasks/run", {"key": "subscription:feed"}, expected=202)
    require(gate.entered.wait(10), "refresh did not reach loopback source")
    body = {"version": "1.8.99", "service_status": "running", "system_metrics": {"cpu": {"usage_percent": 2}}}
    sock, data = w.raw("POST", "/api/agents/edge/heartbeat", body, finish=False,
                       headers={"Authorization": "Bearer fault-synthetic-agent-token", "Expect": "100-continue"})
    try:
        w.signal(wait=False)
        first = w.receipt("metrics_history_flush")
        w.check(first.get("last_trigger") == "shutdown", "first metrics save precedes HTTP drain completion")
        sock.sendall(data)
        response = http.client.HTTPResponse(sock)
        response.begin()
        require(response.status == 200, "in-flight heartbeat did not drain")
        response.read()
        gate.release.set()
        require(w.process.wait(timeout=10) == 0, "draining process failed")
    finally:
        sock.close()
        gate.release.set()
    w.check(w.receipt("metrics_history_flush", 2)["run_count"] == 2, "new drain-time metrics cause exactly one final flush")
    w.check(w.receipt("subscription:feed").get("last_status") == "success", "slow refresh finished during drain")
    w.recovered()
    history = json.loads(w.request("GET", "/api/agents/edge/metrics/history")[1])["data"]["history"]
    w.check([p["cpu"]["usage_percent"] for p in history] == [1, 2], "both shutdown saves reload completely")


@SCENARIOS.register("sigterm-webdav", covers=["5.2"])
def webdav_shutdown(env):
    w = env.world(dav=True)
    w.start()
    gate = w.source.gate("PUT", "/dav*")
    w.request("PUT", "/api/profiles/default", {"name": "Synthetic backup"})
    require(gate.entered.wait(12), "automatic WebDAV upload did not start")
    w.signal(wait=False)
    w.receipt("metrics_history_flush")
    gate.release.set()
    require(w.process.wait(timeout=10) == 0, "WebDAV drain failed")
    w.check(bool(w.source.uploads) and w.receipt("webdav_backup").get("last_status")=="success", "in-flight backup completed during drain")
    w.audit()
    w.recovered()


@SCENARIOS.register("sigkill-refresh", covers=["5.3"])
def kill_refresh(env):
    w = env.world()
    w.start()
    before = w.db()
    gate = w.source.gate("GET", "/subscription")
    w.request("POST", "/api/tasks/run", {"key": "subscription:feed"}, expected=202)
    require(gate.entered.wait(10), "refresh did not start")
    require(w.kill() == -signal.SIGKILL, "process was not killed")
    gate.release.set()
    w.start()
    w.check(w.db() == before, "killed download has no partial configuration commit")
    w.refresh()
    w.check(w.receipt("subscription:feed").get("last_status") == "success", "restart has no stale refresh busy key")
    w.audit()


@SCENARIOS.register("sigkill-metrics", covers=["5.3"])
def kill_metrics(env):
    w = env.world()
    w.start()
    w.heartbeat()
    w.flush()
    w.heartbeat()
    w.arm("metrics")
    w.request("POST", "/api/tasks/run", {"key": "metrics_history_flush"}, expected=202)
    w.entered("metrics")
    require(w.kill() == -signal.SIGKILL, "metrics process was not killed in its active barrier")
    w.release("metrics")
    w.recovered()
    history = json.loads(w.request("GET", "/api/agents/edge/metrics/history")[1])["data"]["history"]
    w.check([p["cpu"]["usage_percent"] for p in history] == [1], "committed metrics prefix survives; unflushed memory is not invented")
    w.heartbeat()
    w.flush()
    w.signal()
    w.recovered()
    history = json.loads(w.request("GET", "/api/agents/edge/metrics/history")[1])["data"]["history"]
    w.check([p["cpu"]["usage_percent"] for p in history] == [1, 3], "metrics task runs again and converges after kill")


@SCENARIOS.register("commit-interruption", covers=["5.2", "5.3"])
def commit_interruption(env):
    for sig in (signal.SIGTERM, signal.SIGKILL):
        w = env.world(suffix=sig.name)
        w.start()
        before = w.db()
        w.arm("store")
        sock, _ = w.raw("PUT", "/api/profiles/default", {"name": "Synthetic interrupted"})
        try:
            w.entered("store")
            w.signal(sig)
        finally:
            sock.close()
            w.release("store")
        w.start()
        w.check(w.db() == before, f"{sig.name} before COMMIT leaves every configuration row unchanged")
        w.request("PUT", "/api/profiles/default", {"name": "Synthetic recovered"})
        w.check(w.db() != before, "fresh transaction commits after interrupted predecessor")
        w.audit()


@SCENARIOS.register("repository-lock-timeout", covers=["5.4"])
def lock_timeout(env):
    w = env.world()
    w.start()
    before = w.db()
    holder = w.lock(".config.lock")
    try:
        w.request("GET", "/api/profiles")
        status, body = w.request("PUT", "/api/profiles/default", {"name": "Synthetic blocked"}, expected=None)
        w.check(status == 400 and json.loads(body).get("message") == "Timed out waiting for profile repository lock", "external config lock gives the repository timeout response while reads work")
        w.check(w.db() == before, "timed-out writer has no partial commit")
    finally:
        w.unlock(holder)
    w.request("PUT", "/api/profiles/default", {"name": "Synthetic after timeout"})
    w.signal()
    w.recovered()


@SCENARIOS.register("shutdown-lock-wait", covers=["5.2", "5.4"])
def lock_cancel(env):
    w = env.world()
    w.start(lock_timeout=30)
    before = w.db()
    holder = w.lock(".config.lock")
    sock, _ = w.raw("PUT", "/api/profiles/default", {"name": "Synthetic waiting"})
    try:
        def waiting():
            for fd in Path(f"/proc/{w.process.pid}/fd").iterdir():
                try:
                    if os.readlink(fd) == str(w.data / ".config.lock"):
                        return True
                except FileNotFoundError:
                    pass
            return False
        eventually(waiting, label="real repository flock wait descriptor")
        w.signal()
    finally:
        sock.close()
        w.unlock(holder)
    w.start()
    w.check(w.db() == before, "shutdown interrupted lock wait before any commit")
    w.request("PUT", "/api/profiles/default", {"name": "Synthetic recovered lock"})
    w.audit()


@SCENARIOS.register("disk-write-failure", covers=["5.3", "5.4"])
def disk_failure(env):
    w = env.world()
    w.start()
    before = w.db()
    database = w.data / "configflow.sqlite3"
    original = database.stat().st_mode & 0o777
    database.chmod(0o400)
    try:
        status, _ = w.request("PUT", "/api/profiles/default", {"name": "Synthetic denied"}, expected=None)
        w.check(status >= 400, "read-only database produces a visible write failure")
    finally:
        database.chmod(original)
    w.check(w.db() == before, "failed SQLite write preserves all rows")
    w.refresh()
    cache = w.data / "shared/subscription-downloads"
    require(cache.is_dir(), "real transport cache not created")
    saved = {p.name: p.read_bytes() for p in cache.iterdir() if p.is_file()}
    original = cache.stat().st_mode & 0o777
    cache.chmod(0o500)
    try:
        receipt = w.refresh()
        w.check(receipt.get("last_status") == "failed", "directory permission fault is recorded as a failed refresh")
    finally:
        cache.chmod(original)
    w.check(saved == {p.name: p.read_bytes() for p in cache.iterdir() if p.is_file()}, "failed atomic cache replacement preserved prior files exactly")
    w.check(w.refresh().get("last_status") == "success", "cache writes recover after fault removal")
    w.signal()
    w.recovered()


@SCENARIOS.register("production-isolation", covers=["test-build isolation"])
def production_isolation(env):
    w = env.world()
    w.start(binary=env.production_binary)
    w.request("GET", "/__faults/state", expected=404, headers={w.catalog["header"]: w.token})
    w.arm("store")
    w.request("PUT", "/api/profiles/default", {"name": "Synthetic production"})
    w.check(not (w.control / "events.jsonl").exists(), "production ignores fault control directory and barriers")
    w.signal()
    w.release("store")
    w.recovered()


@SCENARIOS.register("goroutine-quiescence", covers=["5.6"])
def goroutine_quiescence(env):
    w = env.world()
    w.start()
    for _ in range(20):
        w.request("GET", "/api/profiles")
        w.request("GET", "/api/rules/local/Synthetic")
    baseline = w.state()["goroutines"]
    for index in range(1000):
        if index % 10 == 0:
            sock, _ = w.raw("GET", "/api/profiles")
            sock.close()
        elif index % 3 == 0:
            w.request("GET", "/api/rules/local/Synthetic")
        else:
            w.request("GET", "/api/profiles")
    def quiet():
        state = w.state()
        return state if not state["phases"] and abs(state["goroutines"] - baseline) <= 5 else None
    after = eventually(quiet, 20, "1000 mixed requests returning to goroutine baseline ±5")
    w.observations.update(goroutine_baseline=baseline,goroutines_after=after["goroutines"],requests=1000,disconnects=100)
    w.check(abs(after["goroutines"] - baseline) <= 5, "1000 requests including 100 disconnects return to baseline ±5")
    w.signal()
    w.recovered()


@SCENARIOS.register("barrier-orphan-timeout", covers=["test-build isolation"])
def orphan_timeout(env):
    w = env.world()
    w.start()
    before = w.db()
    w.arm("store")
    started = time.monotonic()
    sock, _ = w.raw("PUT", "/api/profiles/default", {"name": "Synthetic orphan controller"})
    sock.settimeout(40)
    try:
        w.entered("store")
        response = http.client.HTTPResponse(sock)
        response.begin()
        response.read()
        w.check(response.status == 200, "abandoned barrier eventually lets work complete")
    finally:
        sock.close()
    elapsed = time.monotonic() - started
    w.check(29 <= elapsed < 40, "real production 30s barrier bound is enforced")
    events = [json.loads(line) for line in (w.control / "events.jsonl").read_text().splitlines()]
    w.check(any(e["phase"] == w.phase("store") and e["event"] == "timeout" for e in events), "barrier timeout is recorded")
    w.release("store")
    w.check(w.db() != before, "orphaned harness cannot stall a valid commit forever")
    w.signal()
    w.recovered()


@SCENARIOS.register("client-disconnect-convergence", covers=["5.5"])
def disconnect_convergence(env):
    from core import convergence_snapshot, snapshots_equal
    source = env.shared_source()
    base = env.world(suffix="primed", source=source)
    base.start()
    base.refresh()
    base.request("GET", "/api/rules/local/Synthetic")
    base.signal()
    # Preserve generated stable resource IDs, but expire the transport cache so
    # both copies traverse the same real I/O rather than silently hitting cache.
    for path in (base.data / "shared/subscription-downloads").glob("*.json"):
        value = json.loads(path.read_text())
        value["fetched_at"] = "1970-01-01T00:00:00"
        path.write_text(json.dumps(value))
    for operation in ("config", "subscription", "generate", "provider", "discovery"):
        snapshots = []
        for disconnect in (False, True):
            label = operation + ("-disconnect" if disconnect else "-control")
            w = env.world(suffix=label, source=source, clone_from=base.data)
            w.start()
            if operation == "discovery":
                w.heartbeat()
            if operation == "config":
                w.arm("store")
                method, path, body = "PUT", "/api/profiles/default", {"name": "Synthetic converged"}
                gate = None
            elif operation == "subscription":
                method, path, body = "POST", "/api/subscriptions/feed/fetch", {}
                gate = source.gate("GET", "/subscription")
            elif operation == "generate":
                method, path, body = "POST", "/api/generate/mihomo", {}
                w.arm("generate")
                gate = None
            elif operation == "provider":
                method, path, body = "GET", "/api/subscriptions/feed/proxies?token=fault-config-token", None
                w.arm("provider")
                gate = None
            else:
                method, path, body = "POST", "/api/domain-discovery/probe", {"domains": ["example.com"]}
                gate = source.gate("POST", "/api/domain-probe")
            sock, _ = w.raw(method, path, body)
            try:
                if gate:
                    require(gate.entered.wait(10), f"{operation} did not reach its real I/O boundary")
                else:
                    w.entered(operation if operation in {"generate","provider"} else "store")
                if disconnect:
                    sock.close()
                if gate:
                    gate.release.set()
                else:
                    w.release(operation if operation in {"generate","provider"} else "store")
                if not disconnect:
                    response = http.client.HTTPResponse(sock)
                    response.begin()
                    response.read()
                    require(response.status in (200, 202), f"{operation} control failed: {response.status}")
            finally:
                sock.close()
                if gate:
                    gate.release.set()
                else:
                    w.release(operation if operation in {"generate","provider"} else "store")
            def quiet():
                events = (w.control / "lifecycle.jsonl")
                active = 0
                if events.exists():
                    for line in events.read_text().splitlines():
                        e = json.loads(line)
                        if e["loop"] == "matcher-cache-cleanup":
                            continue
                        if e["event"] in {"job-start", "round-start"}:
                            active += 1
                        elif e["event"] in {"job-end", "round-end"}:
                            active -= 1
                # Matcher eviction is an intentionally idle long-lived owner;
                # it has no pending domain/network/commit work to compare.
                state = w.state()
                return not state["phases"] and active == 0
            eventually(quiet, 15, f"{operation} owners quiescent")
            if operation == "config":
                eventually(lambda: b"Synthetic converged" in w.request("GET", "/api/profiles/default")[1], label="detached configuration commit")
            if operation == "subscription":
                w.receipt("subscription:feed", 2)
            if operation=="generate":
                eventually(lambda: any(b"mixed-port" in p.read_bytes() for p in w.data.glob("**/generated/config.yaml")),label="committed generated configuration")
            if operation=="provider":
                eventually(lambda: any(b"192.0.2.8" in p.read_bytes() for p in w.data.glob("**/providers/served/subscription-feed.yaml")),label="committed provider render")
            if operation=="discovery":
                eventually(lambda: any(b"example.com" in p.read_bytes() and b"verdict" in p.read_bytes() for p in (w.data/"probes").glob("*.json")),label="persisted discovery probe")
            if snapshots:
                try:
                    eventually(lambda: snapshots_equal(convergence_snapshot(w),snapshots[0]), 10, f"{operation} final snapshot convergence")
                except AssertionError:
                    current=convergence_snapshot(w)
                    (w.root/"expected-snapshot.json").write_text(json.dumps(snapshots[0],sort_keys=True))
                    (w.root/"actual-snapshot.json").write_text(json.dumps(current,sort_keys=True))
                    changed=[k for k in sorted(set(current)|set(snapshots[0])) if not snapshots_equal(current.get(k),snapshots[0].get(k))]
                    raise AssertionError(f"{operation} disconnect diverged at exact artifact paths: {changed[:8]}")
            snapshots.append(convergence_snapshot(w))
            w.signal()
        left, right = snapshots
        if not snapshots_equal(left,right):
            keys = [key for key in sorted(set(left) | set(right)) if not snapshots_equal(left.get(key),right.get(key))]
            raise AssertionError(f"{operation} disconnect diverged at exact artifact paths: {keys[:8]}")
        w.check(True, f"{operation}: complete DB/file/task snapshot equals uninterrupted control")


@SCENARIOS.register("sigterm-cancel-deadline", covers=["5.2"])
def cancel_deadline(env):
    w=env.world()
    gate=w.source.gate("GET","/subscription")
    began=time.monotonic()
    w.start()
    # Use the production 30s periodic spec, so this also checks that work
    # detached from a round survives stopLoops until stopJobs (R14 M1).
    require(gate.entered.wait(40),"production periodic refresh I/O did not start")
    w.receipt("rule_source:rulesource")
    w.observations["scheduled_refresh_wait_seconds"]=round(time.monotonic()-began,3)
    before=w.db()
    signalled=time.monotonic()
    w.signal(wait=False)
    require(not gate.cancelled.wait(5),"detached I/O was cancelled before stopJobs drain deadline")
    require(gate.cancelled.wait(4),"stopJobs did not cancel in-flight I/O")
    elapsed=gate.cancelled_at-signalled
    w.observations["io_cancel_seconds"]=round(elapsed,3)
    w.check(5<=elapsed<9,"in-flight detached I/O lives through drain and cancels at stopJobs")
    require(w.process.wait(timeout=3)==0,"shutdown did not finish after cancellation")
    gate.release.set()
    w.start()
    w.check(w.db()==before,"cancelled download did not change configuration")
    w.refresh()
    w.audit()
