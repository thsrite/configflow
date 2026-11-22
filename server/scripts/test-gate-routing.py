#!/usr/bin/env python3
"""Exercise the real gate shell with bounded fake tools; no product tests here."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).with_name("gate.sh")
FAKE_GO = '''#!/usr/bin/env python3
import fcntl, json, os, pathlib, sys, time
home = pathlib.Path(os.environ["HOME"])
args = sys.argv[1:]
with (home / "calls.jsonl").open("a") as f:
 f.write(json.dumps([args, os.getenv("PYREF_INPUT_FINGERPRINT")]) + "\\n")
if args[:1] == ["run"]:
 print("" if os.getenv("EMPTY_FINGERPRINT") else "synthetic-fingerprint")
elif args == ["list", "./..."]:
 print("configflow/internal/httpx/difftest")
elif args[:1] == ["clean"]:
 (home / "cached").unlink(missing_ok=True)
elif "-race" in args:
 (home / "race-temp.json").write_text(json.dumps({"tmp": os.getenv("TMPDIR"), "go": os.getenv("GOTMPDIR")}))
 previous_umask = os.umask(0)
 os.umask(previous_umask)
 (home / "test-umask").write_text(str(previous_umask))
 if os.getenv("FAIL_RACE"): sys.exit(1)
 print("ok  configflow/internal/fake  " + ("(cached)" if (home / "cached").exists() else "1.00s"))
 (home / "cached").touch()
elif any(a.startswith("-fuzz=") for a in args):
 def update(delta):
  with (home / "fuzz.lock").open("a+") as lock:
   fcntl.flock(lock, fcntl.LOCK_EX)
   statefile = home / "fuzz.json"
   state = json.loads(statefile.read_text()) if statefile.exists() else {"active": 0, "peak": 0, "starts": [], "finished": 0}
   state["active"] += delta
   state["peak"] = max(state["peak"], state["active"])
   if delta == 1:
    state["starts"].append({"tmp": os.environ["TMPDIR"], "go": os.environ["GOTMPDIR"], "args": args})
   else:
    state["finished"] += 1
   statefile.write_text(json.dumps(state))
 update(1)
 name = next(a for a in args if a.startswith("-fuzz="))
 for key in ("TMPDIR", "GOTMPDIR"):
  (pathlib.Path(os.environ[key]) / str(os.getpid())).write_text("synthetic fuzz work")
 time.sleep(0.25)
 update(-1)
 if os.getenv("FAIL_FUZZ") and "Fuzz0$" in name: sys.exit(1)
 print("PASS synthetic fuzz")
'''


FAKE_NPM = """#!/bin/sh
echo "npm $*" >> "$HOME/frontend-calls.log"
case "$1" in
  ci) mkdir -p node_modules ;;
  run) [ -n "$FAIL_TYPECHECK" ] && { echo "synthetic type error"; exit 1; } ;;
esac
exit 0
"""
FAKE_NPX = """#!/bin/sh
echo "npx $*" >> "$HOME/frontend-calls.log"
if [ -n "$FAIL_FRONTEND" ]; then echo " FAIL  tests/synthetic.test.ts > fails"; echo "      Tests  1 failed | 2 passed (3)"; exit 1; fi
echo "      Tests  3 passed (3)"
"""


class RoutingTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.server = self.root / "server"
        scripts = self.server / "scripts"
        scripts.mkdir(parents=True)
        shutil.copy(SOURCE, scripts / "gate.sh")
        (scripts / "proddiff.sh").write_text('#!/bin/bash\necho "proddiff mode=${PRODDIFF_MODE:-unset} base=${PRODDIFF_BASE_TREE:-} $*"\n')
        (scripts / "proddiff.sh").chmod(0o755)
        (scripts / "gate-live.py").write_text('import os, sys\nprint("synthetic live fake", *sys.argv[1:])\nsys.exit(1 if os.getenv("FAIL_DIFF") and "--difftest" in sys.argv else 0)\n')
        (self.server / "tools/pyref").mkdir(parents=True)
        (self.root / "rt").mkdir()
        binary = self.root / "bin"
        binary.mkdir()
        (binary / "go").write_text(FAKE_GO)
        (binary / "go").chmod(0o755)
        (binary / "gofmt").write_text('#!/bin/sh\nexit 0\n')
        (binary / "gofmt").chmod(0o755)
        for name, text in (("npm", FAKE_NPM), ("npx", FAKE_NPX)):
            (binary / name).write_text(text)
            (binary / name).chmod(0o755)
        # The core stage also gates the Agent module (ac12f4423); the fake go answers it.
        (self.root / "backend/agents/go-agent").mkdir(parents=True)
        frontend = self.root / "frontend"
        frontend.mkdir()
        (frontend / "package.json").write_text('{"name": "synthetic"}\n')
        (frontend / "package-lock.json").write_text('{"lockfileVersion": 3}\n')
        self.env = dict(os.environ, HOME=str(self.root), PATH=str(binary) + ":" + os.environ["PATH"],
                        GATE_LOGS=str(self.root / "logs"), CONFIGFLOW_DIFF_PYTHON=shutil.which("python3"))
        for key in ("GATE_ONLY", "GATE_FULL", "GATE_SLOTS", "FAIL_RACE", "EMPTY_FINGERPRINT",
                    "GATE_FUZZ_JOBS", "GATE_FUZZ_PARALLEL", "GATE_FUZZTIME", "FAIL_FUZZ", "FAIL_DIFF",
                    "FAIL_FRONTEND", "FAIL_TYPECHECK", "PRODDIFF_MODE", "PRODDIFF_BASE_TREE"):
            self.env.pop(key, None)

    def run_gate(self, targets=(), **env):
        return subprocess.run(["bash", str(self.server / "scripts/gate.sh"), "-p", "6", *targets],
                              env=dict(self.env, **env), text=True, capture_output=True, timeout=10, umask=0o022)

    def calls(self):
        return [json.loads(line) for line in (self.root / "calls.jsonl").read_text().splitlines()]

    def test_full_then_cache_then_forced(self):
        first = self.run_gate(GATE_ONLY="core")
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        self.assertIn("FULL mode", first.stdout)
        self.assertTrue((self.root / "rt/.gate-last-full").exists())
        second = self.run_gate(GATE_ONLY="core")
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
        self.assertIn("CACHE mode", second.stdout)
        self.assertIn("race cache hits=1", second.stdout)
        third = self.run_gate(GATE_ONLY="core", GATE_FULL="1")
        self.assertEqual(third.returncode, 0, third.stdout + third.stderr)
        self.assertIn("FULL mode (GATE_FULL)", third.stdout)
        race = [(args, fp) for args, fp in self.calls() if "-race" in args]
        # Each core run races the server and then the Agent module.
        self.assertEqual(len(race), 6)
        self.assertTrue(all("-count=1" not in args and fp == "synthetic-fingerprint" for args, fp in race))
        self.assertEqual(sum(args == ["clean", "-testcache"] for args, _ in self.calls()), 2)

    def test_gate_preserves_test_process_umask(self):
        result = self.run_gate(GATE_ONLY="core")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "test-umask").read_text(), str(0o022))
        self.assertEqual((self.root / "logs").stat().st_mode & 0o777, 0o700)

    def test_trace_keeps_original_umask_and_private_trace(self):
        trace = self.root / "trace.log"
        binary = self.root / "webdav.test"
        binary.write_text('#!/bin/bash\numask\n')
        binary.chmod(0o755)
        result = subprocess.run(["bash", str(SOURCE.with_name("gate-test-exec.sh")), str(binary)],
                                env=dict(os.environ, GATE_LIVE_TRACE=str(trace)),
                                text=True, capture_output=True, timeout=10, umask=0o022)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.strip(), "0022")
        self.assertEqual(trace.stat().st_mode & 0o777, 0o600)

    def test_stale_and_future_stamp_force_full(self):
        stamp = self.root / "rt/.gate-last-full"
        stamp.touch()
        for timestamp in (1, 4102444800):
            os.utime(stamp, (timestamp, timestamp))
            result = self.run_gate(GATE_ONLY="core")
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("FULL mode", result.stdout)

    def test_failed_race_never_refreshes_stamp(self):
        result = self.run_gate(GATE_ONLY="core", FAIL_RACE="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "rt/.gate-last-full").exists())

    def test_empty_fingerprint_fails(self):
        result = self.run_gate(GATE_ONLY="core", EMPTY_FINGERPRINT="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("empty pyref fingerprint", result.stderr)

    def test_old_tree_without_fingerprint_entry(self):
        (self.server / "tools/pyref").rmdir()
        result = self.run_gate(GATE_ONLY="core")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("entry not integrated", result.stdout)

    def test_compare_only_keeps_stamp_and_runs_live_proddiff(self):
        result = self.run_gate(GATE_ONLY="compare", GATE_FULL="1")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("proddiff (live; no response cache)", result.stdout)
        self.assertNotIn("--no-cache", result.stdout)
        self.assertIn("synthetic live fake", result.stdout)
        self.assertNotIn("== race", result.stdout)
        self.assertFalse((self.root / "rt/.gate-last-full").exists())
        self.assertFalse(any("-race" in args for args, _ in self.calls()))

    def test_failed_diff_still_runs_live(self):
        result = self.run_gate(GATE_ONLY="compare", FAIL_DIFF="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("== synthetic live", result.stdout)
        self.assertIn("GATE FAIL: compare", result.stderr)
        self.assertFalse((self.root / "rt/.gate-last-full").exists())

    def test_default_runs_both(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("== race", result.stdout)
        self.assertIn("== difftest", result.stdout)
        self.assertIn("synthetic live fake", result.stdout)

    def frontend_calls(self):
        log = self.root / "frontend-calls.log"
        return log.read_text().splitlines() if log.exists() else []

    def test_frontend_only_installs_once_per_lockfile(self):
        for _ in range(2):
            result = self.run_gate(GATE_ONLY="frontend")
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("gate: frontend PASS (typecheck; vitest Tests 3 passed (3))", result.stdout)
            self.assertIn("GATE STAGE PASS: frontend", result.stdout)
            self.assertNotIn("== race", result.stdout)
            self.assertNotIn("== difftest", result.stdout)
        calls = self.frontend_calls()
        self.assertEqual(sum(c.startswith("npm ci") for c in calls), 1, calls)
        self.assertEqual(calls.count("npm run -s typecheck"), 2)
        self.assertEqual(calls.count("npx vitest run"), 2)
        link = self.root / "frontend/node_modules"
        self.assertTrue(link.is_symlink())
        self.assertEqual(link.resolve().parent.parent, (self.root / "cf-test/node_modules").resolve())
        self.assertFalse((self.root / "rt/.gate-last-full").exists())

    def test_frontend_failures_fail_the_stage(self):
        for env, step in (({"FAIL_TYPECHECK": "1"}, "typecheck"), ({"FAIL_FRONTEND": "1"}, "vitest")):
            result = self.run_gate(GATE_ONLY="frontend", **env)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(f"GATE FAIL: frontend ({step}", result.stderr)
            self.assertNotIn("GATE STAGE PASS", result.stdout)
        self.assertIn("FAIL  tests/synthetic.test.ts", result.stdout)

    def test_compare_runs_frontend_and_keeps_other_groups(self):
        result = self.run_gate(GATE_ONLY="compare", FAIL_FRONTEND="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("== synthetic live", result.stdout)
        self.assertIn("== frontend", result.stdout)
        self.assertIn("GATE FAIL: frontend (vitest", result.stderr)
        self.assertIn("GATE FAIL: compare", result.stderr)
        default = self.run_gate()
        self.assertEqual(default.returncode, 0, default.stdout + default.stderr)
        self.assertIn("gate: frontend PASS", default.stdout)
        core = self.run_gate(GATE_ONLY="core")
        self.assertNotIn("== frontend", core.stdout)

    def test_proddiff_mode_defaults_to_python_and_go_needs_a_base_tree(self):
        default = self.run_gate(GATE_ONLY="proddiff")
        self.assertEqual(default.returncode, 0, default.stdout + default.stderr)
        self.assertIn("mode=python", default.stdout)
        self.assertIn("proddiff mode=python base=", default.stdout)
        for env in ({"PRODDIFF_MODE": "typo"}, {"PRODDIFF_MODE": "go"},
                    {"PRODDIFF_MODE": "go", "PRODDIFF_BASE_TREE": str(self.root / "missing")}):
            with self.subTest(env=env):
                result = self.run_gate(GATE_ONLY="proddiff", **env)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertNotIn("== proddiff", result.stdout)
        base = self.root / "base"
        (base / "server").mkdir(parents=True)
        go = self.run_gate(GATE_ONLY="proddiff", PRODDIFF_MODE="go", PRODDIFF_BASE_TREE=str(base))
        self.assertEqual(go.returncode, 0, go.stdout + go.stderr)
        self.assertIn(f"proddiff mode=go base={base}", go.stdout)

    def test_invalid_stage_fails_before_work(self):
        result = self.run_gate(GATE_ONLY="typo")
        self.assertEqual(result.returncode, 2)
        self.assertFalse((self.root / "calls.jsonl").exists())

    def assert_fuzz_work(self, count, limit):
        state = json.loads((self.root / "fuzz.json").read_text())
        self.assertEqual(len(state["starts"]), count)
        self.assertEqual(state["finished"], count)
        self.assertEqual(state["active"], 0)
        self.assertGreater(state["peak"], 0)
        self.assertLessEqual(state["peak"], limit)
        roots = set()
        for start in state["starts"]:
            for key in ("go", "tmp"):
                path = Path(start[key])
                self.assertEqual(path.parent.parent, self.root / ".cache/configflow-gate")
                self.assertFalse(path.exists(), "fuzz work was not removed")
                roots.add(path.parent)
        self.assertEqual(len(roots), 1)
        return state

    def test_fuzz_default_cap_disk_temp_and_cleanup(self):
        result = self.run_gate(targets=[f"Fuzz{i}:./internal/fake" for i in range(41)], GATE_ONLY="core",
                               TMPDIR="/tmp", GOTMPDIR="/tmp")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("at most 8 jobs", result.stdout)
        state = self.assert_fuzz_work(41, 8)
        self.assertTrue(all("-parallel=2" in s["args"] for s in state["starts"]))
        # Only fuzz inherits the private paths; earlier stages keep the caller's env.
        self.assertEqual(json.loads((self.root / "race-temp.json").read_text()), {"tmp": "/tmp", "go": "/tmp"})

    def test_fuzz_override_and_failure_still_reaps_and_cleans(self):
        result = self.run_gate(targets=[f"Fuzz{i}:./internal/fake" for i in range(5)], GATE_ONLY="core",
                               GATE_FUZZ_JOBS="2", GATE_FUZZ_PARALLEL="3", FAIL_FUZZ="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("at most 2 jobs", result.stdout)
        self.assertIn("GATE FAIL: fuzz", result.stderr)
        state = self.assert_fuzz_work(5, 2)
        self.assertTrue(all("-parallel=3" in s["args"] for s in state["starts"]))
        self.assertFalse((self.root / "rt/.gate-last-full").exists())

    def test_fuzz_one_job_and_unique_run_directories(self):
        targets = ["Fuzz0:./internal/fake", "Fuzz1:./internal/fake"]
        first = self.run_gate(targets=targets, GATE_ONLY="core", GATE_FUZZ_JOBS="1")
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        state = self.assert_fuzz_work(2, 1)
        first_root = Path(state["starts"][0]["tmp"]).parent
        (self.root / "fuzz.json").unlink()
        second = self.run_gate(targets=targets, GATE_ONLY="core", GATE_FUZZ_JOBS="1")
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
        state = self.assert_fuzz_work(2, 1)
        self.assertNotEqual(first_root, Path(state["starts"][0]["tmp"]).parent)

    def test_invalid_fuzz_concurrency_starts_no_fuzz(self):
        for value in ("0", "-1", "invalid"):
            with self.subTest(value=value):
                result = self.run_gate(targets=["Fuzz0:./internal/fake"], GATE_ONLY="core", GATE_FUZZ_JOBS=value)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Invalid GATE_FUZZ_JOBS", result.stderr)
                self.assertFalse((self.root / "fuzz.json").exists())


if __name__ == "__main__":
    unittest.main()
