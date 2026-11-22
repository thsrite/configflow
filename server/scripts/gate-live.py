#!/usr/bin/env python3
"""Run discovered live Python tests, failing closed on missing execution or skips.

No product imports and no snapshot inputs. Discovery uses Go's active file list
and AST helper references, so a new environment-gated package needs no whitelist.
"""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

# Branch evidence supplements discovery where top-level Go success is insufficient.
MANIFEST = {
    "repo": ["TestStoredRoundTrip/python-written-rows"],
    "store": ["TestStoredRoundTrip/committed-python-fixture"],
}



def webdav_oracle_executed(trace):
    """True when strace shows the WebDAV TLS oracle execve returning 0.

    Under strace -f a busy process prints the call as `<unfinished ...>` and its
    result later as `<... execve resumed>) = 0` on another line of the same pid.
    """
    pending = set()
    for line in trace.splitlines():
        pid = line.split(" ", 1)[0]
        if 'execve(' in line and 'from webdav3.client import Client' in line and 'TLS_REJECTED' in line:
            if line.rstrip().endswith('<unfinished ...>'):
                pending.add(pid)
            elif '= 0' in line:
                return True
        elif pid in pending and 'execve resumed>' in line and '= 0' in line:
            return True
    return False

def checked_manifest(discovery, mode):
    """Reject omitted environment-bearing packages before executing Go tests."""
    covered = set(discovery["diff"]) | set(discovery["live"])
    missing = set(discovery["sources"]) - covered
    if missing:
        raise RuntimeError("environment test packages omitted from compare: " + ", ".join(sorted(missing)))
    manifest = {pkg: list(tests) for pkg, tests in discovery[mode].items()}
    if not manifest or any(not tests for tests in manifest.values()):
        raise RuntimeError(f"empty {mode} execution manifest")
    if mode == "live":
        for pkg, tests in MANIFEST.items():
            key = "configflow/internal/" + pkg
            if key not in manifest:
                raise RuntimeError(f"missing Python branch package: {key}")
            manifest[key].extend(tests)
    return manifest


def validate(events, trace, manifest=None, webdav=True):
    """Require run+pass for every entry and reject all selected skips/failures."""
    if manifest is None:
        manifest = {"configflow/internal/" + pkg: tests for pkg, tests in MANIFEST.items()}
    seen = {}
    problems = []
    for event in events:
        action = event.get("Action")
        test = event.get("Test")
        # Existing difftest fuzz seeds may reject invalid inputs with t.Skip;
        # every ordinary selected test and subtest must execute without skips.
        if action == "fail" or (action == "skip" and not (test or "").startswith("Fuzz")):
            problems.append(f"{action}: {event.get('Package')}/{test or '<package>'}")
        if test:
            seen.setdefault((event.get("Package"), test), set()).add(action)
    for package, tests in manifest.items():
        for test in tests:
            key = (package, test)
            if not {"run", "pass"} <= seen.get(key, set()):
                problems.append(f"missing run/pass: {'/'.join(key)}")
    if webdav and not webdav_oracle_executed(trace):
        problems.append("missing executed WebDAV Python TLS oracle")
    if problems:
        raise RuntimeError("\n".join(problems))
    return sum(map(len, manifest.values()))


def python_environment(server):
    env = os.environ.copy()
    python = Path(env.get("CONFIGFLOW_PY") or env.get("CONFIGFLOW_DIFF_PYTHON") or
                  str(Path.home() / "cf-test/.venv/bin/python")).absolute()
    # Several legacy tests use this exact interpreter independently of env.
    dav_python = Path.home() / "cf-test/.venv/bin/python"
    for interpreter in {str(python), str(dav_python)}:
        if not Path(interpreter).is_file() or not os.access(interpreter, os.X_OK):
            raise RuntimeError(f"missing executable Python: {interpreter}")
        subprocess.run([interpreter, "-c", "import flask, requests, yaml, webdav3.client"], check=True)
    env.update(CONFIGFLOW_PY=str(python), CONFIGFLOW_REPO=str(server.parent),
               CONFIGFLOW_DIFF_PYTHON=str(python), CONFIGFLOW_YAML_PYTHON=str(python),
               CONFIGFLOW_DIFF="1", PYTHONDONTWRITEBYTECODE="1")
    # Comparison never records/accepts goldens, nor inherits a private snapshot.
    for key in ("CONFIGFLOW_SNAPSHOT", "PYREF_MODE", "PYREF_ACCEPT", "PYREF_INPUT_FINGERPRINT"):
        env.pop(key, None)
    return env


def main():
    args = sys.argv[1:]
    mode = "diff" if args[0] == "--difftest" else "live"
    if mode == "diff":
        args.pop(0)
    logs = Path(args[0]).resolve()
    logs.mkdir(parents=True, exist_ok=True)
    server = Path(__file__).resolve().parent.parent
    os.chdir(server)
    env = python_environment(server)
    discovery = json.loads(subprocess.check_output(["go", "run", "./tools/gatediscover"], env=env))
    manifest = checked_manifest(discovery, mode)
    (logs / f"{mode}-discovery.json").write_text(json.dumps(discovery, indent=2) + "\n")
    (logs / f"{mode}-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    trace = logs / "live-webdav-exec.log"
    command = ["go", "test", "-json", "-count=1", "-timeout=40m"]
    if mode == "live":
        if not shutil.which("strace"):
            raise RuntimeError("synthetic live requires strace for WebDAV branch evidence")
        trace.unlink(missing_ok=True)
        env["GATE_LIVE_TRACE"] = str(trace)
        names = sorted({test.split("/")[0] for tests in manifest.values() for test in tests})
        pattern = "^(" + "|".join(map(re.escape, names)) + ")$"
        command += ["-race", "-run", pattern, "-skip", "private-snapshot",
                    "-exec", str(server / "scripts/gate-test-exec.sh")]
    command += [*args[1:], *sorted(manifest)]
    print(f"{mode}: real Python, snapshot unset; " + " ".join(command), flush=True)
    with (logs / f"{mode}.json").open("w") as output:
        result = subprocess.run(command, env=env, stdout=output, stderr=subprocess.STDOUT)
    events = []
    for line in (logs / f"{mode}.json").read_text().splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            raise RuntimeError(f"non-JSON go test output: {line[:200]}")
    # Keep a concise per-test result even when validation or Go fails.
    summary = [{k: event[k] for k in ("Package", "Test", "Action") if k in event}
               for event in events if event.get("Test") and event.get("Action") in ("pass", "fail", "skip")]
    (logs / f"{mode}-results.json").write_text(json.dumps(summary, indent=2) + "\n")
    for event in events:
        if event.get("Action") == "output":
            print(event.get("Output", ""), end="")
    count = validate(events, trace.read_text() if mode == "live" and trace.exists() else "",
                     manifest, webdav=mode == "live")
    if result.returncode:
        raise RuntimeError(f"go test exited {result.returncode}; see {logs / (mode + '.json')}")
    print(f"{mode.upper()} PASS: {count} required entries ran and passed; 0 test skips", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(f"COMPARE EXECUTION FAIL: {error}")
