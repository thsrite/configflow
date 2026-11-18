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


class RoutingTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.server = self.root / "server"
        scripts = self.server / "scripts"
        scripts.mkdir(parents=True)
        shutil.copy(SOURCE, scripts / "gate.sh")
        (scripts / "proddiff.sh").write_text('#!/bin/bash\necho "proddiff $*"\n')
        (scripts / "proddiff.sh").chmod(0o755)
        (scripts / "gate-live.py").write_text('print("synthetic live fake")\n')
        (self.server / "tools/pyref").mkdir(parents=True)
        (self.root / "rt").mkdir()
        binary = self.root / "bin"
        binary.mkdir()
        (binary / "go").write_text(FAKE_GO)
        (binary / "go").chmod(0o755)
        (binary / "gofmt").write_text('#!/bin/sh\nexit 0\n')
        (binary / "gofmt").chmod(0o755)
        self.env = dict(os.environ, HOME=str(self.root), PATH=str(binary) + ":" + os.environ["PATH"],
                        GATE_LOGS=str(self.root / "logs"), CONFIGFLOW_DIFF_PYTHON=shutil.which("python3"))
        for key in ("GATE_ONLY", "GATE_FULL", "GATE_SLOTS", "FAIL_RACE", "EMPTY_FINGERPRINT",
                    "GATE_FUZZ_JOBS", "GATE_FUZZ_PARALLEL", "GATE_FUZZTIME", "FAIL_FUZZ"):
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
        self.assertEqual(len(race), 3)
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

    def test_default_runs_both(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("== race", result.stdout)
        self.assertIn("== difftest", result.stdout)
        self.assertIn("synthetic live fake", result.stdout)

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
