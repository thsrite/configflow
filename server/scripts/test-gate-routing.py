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
import json, os, pathlib, sys
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
 if os.getenv("FAIL_RACE"): sys.exit(1)
 print("ok  configflow/internal/fake  " + ("(cached)" if (home / "cached").exists() else "1.00s"))
 (home / "cached").touch()
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
        (scripts / "proddiff.sh").write_text('#!/bin/bash\n# --no-cache\necho "proddiff $*"\n')
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
        for key in ("GATE_ONLY", "GATE_FULL", "GATE_SLOTS", "FAIL_RACE", "EMPTY_FINGERPRINT"):
            self.env.pop(key, None)

    def run_gate(self, **env):
        return subprocess.run(["bash", str(self.server / "scripts/gate.sh"), "-p", "6"],
                              env=dict(self.env, **env), text=True, capture_output=True, timeout=10)

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

    def test_compare_only_keeps_stamp_and_bypasses_response_cache(self):
        result = self.run_gate(GATE_ONLY="compare", GATE_FULL="1")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("proddiff --no-cache", result.stdout)
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


if __name__ == "__main__":
    unittest.main()
