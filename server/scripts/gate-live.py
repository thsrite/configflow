#!/usr/bin/env python3
"""Run the exact synthetic live boundary manifest, failing closed on missing/skip.

No product imports and no snapshot inputs. The WebDAV exec trace contains only
synthetic argv; keep it with the other mode-0700 gate logs.
"""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

MANIFEST = {
    "repo": [
        "TestCrossCheckFreshInit", "TestCrossCheckOperations",
        "TestCrossCheckInterchange", "TestCrossCheckC04Operations",
        "TestCrossCheckLockExcludesPythonAndGo",
        "TestCrossCheckPythonWriterKilledBeforeCommit",
        "TestConfigTemplateMatchesBackend", "TestWriteFromAnotherProcessIsSeen",
        "TestStoredRoundTrip",
        # This subtest only exists when the real Python branch is enabled.
        "TestStoredRoundTrip/python-written-rows",
    ],
    "store": [
        "TestCrossCheckPython", "TestCrossCheckPythonErrors",
        "TestCrossCheckPythonConcurrentCommits", "TestCrossCheckPythonOddDocuments",
        "TestStoredRoundTrip", "TestStoredRoundTrip/committed-python-fixture",
    ],
    "model": ["TestTypesPythonRoundTrip", "TestHRPythonWriters"],
    "api/agents": [
        "TestPublicAgentPython", "TestA24FlaskBoundaries",
        "TestReviewA24TargetsIgnoreUnrelatedLegacySettings",
    ],
    "agents": ["TestProxyDifferentialAgainstRequests"],
    "webdav": ["TestTLSVerificationMatchesPythonDefault"],
}


def validate(events, trace):
    """Require run+pass for every entry and reject all selected skips/failures."""
    seen = {}
    problems = []
    for event in events:
        action = event.get("Action")
        test = event.get("Test")
        if action in ("skip", "fail"):
            problems.append(f"{action}: {event.get('Package')}/{test or '<package>'}")
        if test:
            seen.setdefault((event.get("Package"), test), set()).add(action)
    for package, tests in MANIFEST.items():
        for test in tests:
            key = (f"configflow/internal/{package}", test)
            if not {"run", "pass"} <= seen.get(key, set()):
                problems.append(f"missing run/pass: {'/'.join(key)}")
    # The test's Go success alone is insufficient: it returns early without DIFF.
    # Match a successful exec of the actual inline oracle, not a shell/preflight.
    if not any(
        'execve(' in line and '= 0' in line and
        'from webdav3.client import Client' in line and 'TLS_REJECTED' in line
        for line in trace.splitlines()
    ):
        problems.append("missing executed WebDAV Python TLS oracle")
    if problems:
        raise RuntimeError("\n".join(problems))
    return sum(map(len, MANIFEST.values()))


def main():
    logs = Path(sys.argv[1]).resolve()
    logs.mkdir(parents=True, exist_ok=True)
    server = Path(__file__).resolve().parent.parent
    os.chdir(server)
    env = os.environ.copy()
    python = Path(env.get("CONFIGFLOW_PY") or env.get("CONFIGFLOW_DIFF_PYTHON") or
                  str(Path.home() / "cf-test/.venv/bin/python")).absolute()
    if not python.is_file() or not os.access(python, os.X_OK):
        raise RuntimeError(f"missing executable Python: {python}")
    if not shutil.which("strace"):
        raise RuntimeError("synthetic live requires strace for WebDAV branch evidence")
    # The legacy WebDAV test uses this exact interpreter, independently of PY.
    dav_python = Path.home() / "cf-test/.venv/bin/python"
    for interpreter in {str(python), str(dav_python)}:
        subprocess.run([interpreter, "-c", "import flask, requests, yaml, webdav3.client"],
                       check=True)
    env.update(CONFIGFLOW_PY=str(python), CONFIGFLOW_REPO=str(server.parent),
               CONFIGFLOW_DIFF_PYTHON=str(python), CONFIGFLOW_DIFF="1",
               PYTHONDONTWRITEBYTECODE="1")
    env.pop("CONFIGFLOW_SNAPSHOT", None)
    trace = logs / "live-webdav-exec.log"
    trace.unlink(missing_ok=True)
    env["GATE_LIVE_TRACE"] = str(trace)
    names = sorted({test.split("/")[0] for tests in MANIFEST.values() for test in tests})
    pattern = "^(" + "|".join(map(re.escape, names)) + ")$"
    command = ["go", "test", "-json", "-race", "-count=1", "-timeout=40m",
               "-run", pattern, "-skip", "private-snapshot",
               "-exec", str(server / "scripts/gate-test-exec.sh"), *sys.argv[2:],
               *(f"./internal/{package}" for package in MANIFEST)]
    (logs / "live-manifest.json").write_text(json.dumps(MANIFEST, indent=2) + "\n")
    print("synthetic live: real Python, snapshot unset; " + " ".join(command), flush=True)
    with (logs / "live.json").open("w") as output:
        result = subprocess.run(command, env=env, stdout=output, stderr=subprocess.STDOUT)
    events = []
    for line in (logs / "live.json").read_text().splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            raise RuntimeError(f"non-JSON go test output: {line[:200]}")
    count = validate(events, trace.read_text() if trace.exists() else "")
    if result.returncode:
        raise RuntimeError(f"go test exited {result.returncode}; see {logs / 'live.json'}")
    print(f"SYNTHETIC LIVE PASS: {count} required entries ran and passed; 0 skips; "
          "repo Python subtest and WebDAV Python exec verified", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(f"SYNTHETIC LIVE FAIL: {error}")
