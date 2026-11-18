#!/usr/bin/env python3
"""Negative controls for the live execution evidence checker (run on server)."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("gate_live", Path(__file__).with_name("gate-live.py"))
live = importlib.util.module_from_spec(spec)
spec.loader.exec_module(live)


class EvidenceTests(unittest.TestCase):
    def events(self):
        return [dict(Package="configflow/internal/" + pkg, Test=test, Action=action)
                for pkg, tests in live.MANIFEST.items() for test in tests
                for action in ("run", "pass")]

    trace = '123 execve("python", ["python", "-c", "from webdav3.client import Client\\nTLS_REJECTED"], []) = 0'

    def test_complete(self):
        self.assertEqual(live.validate(self.events(), self.trace), 23)

    def test_missing_test(self):
        with self.assertRaisesRegex(RuntimeError, "missing run/pass"):
            live.validate(self.events()[2:], self.trace)

    def test_skip(self):
        with self.assertRaisesRegex(RuntimeError, "skip:"):
            live.validate(self.events() + [dict(Action="skip", Test="child")], self.trace)

    def test_fail(self):
        with self.assertRaisesRegex(RuntimeError, "fail:"):
            live.validate(self.events() + [dict(Action="fail")], self.trace)

    def test_silent_repo_branch(self):
        events = [e for e in self.events() if e["Test"] != "TestStoredRoundTrip/python-written-rows"]
        with self.assertRaisesRegex(RuntimeError, "python-written-rows"):
            live.validate(events, self.trace)

    def test_silent_webdav_branch(self):
        for trace in ("", self.trace.replace("= 0", "= -1 ENOENT")):
            with self.subTest(trace=trace), self.assertRaisesRegex(RuntimeError, "WebDAV"):
                live.validate(self.events(), trace)


if __name__ == "__main__":
    unittest.main()
