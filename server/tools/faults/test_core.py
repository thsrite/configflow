"""Control tests for harness guards and comparisons; run only on a test server."""
import json
from pathlib import Path
import tempfile
import unittest

from core import CLOCK_FIELDS, Scenarios, World, normalized, normalized_bytes, snapshots_equal, strict_json, require


class HarnessControls(unittest.TestCase):
    def test_no_duplicate_or_empty_registry_stub(self):
        registry = Scenarios()
        registry.register("one", covers=["5.1"])(lambda env: None)
        with self.assertRaises(AssertionError):
            registry.register("one", covers=["5.2"])(lambda env: None)

    def test_normalization_keeps_business_fields_and_presence(self):
        base = {"run_count": 2, "last_status": "success", "_revision": 8, "nodes": [{"id": "stable", "server": "192.0.2.1"}], "updated_at": "2026-01-01T00:00:00Z"}
        other = dict(base, updated_at="2026-01-01T00:01:00Z")
        self.assertEqual(normalized(base), normalized(other))
        for key, value in (("run_count", 3), ("last_status", "failed"), ("_revision", 9), ("nodes", [])):
            with self.subTest(key=key):
                self.assertNotEqual(normalized(base), normalized(dict(base, **{key: value})))
        self.assertNotEqual(normalized({"updated_at": None}), normalized({"updated_at": "value"}))
        self.assertNotEqual(normalized({}), normalized({"updated_at": "value"}))
        self.assertNotEqual(normalized({"updated_at":"bad"}),normalized({"updated_at":"worse"}))

    def test_identity_mapping_is_consistent_and_unmapped_references_still_fail(self):
        a = normalized({"id": "node_a", "ref": "node_a"}, {"node_a": "node_<same>"})
        b = normalized({"id": "node_b", "ref": "node_b"}, {"node_b": "node_<same>"})
        self.assertEqual(a, b)
        self.assertNotEqual(a, normalized({"id": "node_b", "ref": "broken"}, {"node_b": "node_<same>"}))
        self.assertEqual(normalized("http://127.0.0.1:1/api", panel="http://127.0.0.1:1"), "<panel>/api")
        self.assertNotEqual(normalized("http://192.0.2.1/api", panel="http://127.0.0.1:1"), "<panel>/api")

    def test_provider_header_only_normalizes_its_known_timestamp(self):
        path="profiles/default/providers/served/subscription-feed.yaml"
        a=b"# ConfigFlow Subscription Proxies\n# Generated: 2026-01-01T00:00:00Z\nproxies: []\n"
        b=a.replace(b"00:00:00Z",b"00:00:01Z")
        self.assertEqual(normalized_bytes(a,path,"http://127.0.0.1:1"),normalized_bytes(b,path,"http://127.0.0.1:1"))
        self.assertNotEqual(normalized_bytes(a,"rules/file.list","http://127.0.0.1:1"),normalized_bytes(b,"rules/file.list","http://127.0.0.1:1"))
        self.assertNotEqual(normalized_bytes(a,path,"http://127.0.0.1:1"),normalized_bytes(b.replace(b"proxies: []",b"proxies: [lost]"),path,"http://127.0.0.1:1"))

    def test_snapshot_comparison_preserves_json_types(self):
        self.assertFalse(snapshots_equal({"value":True},{"value":1}))
        self.assertFalse(snapshots_equal({"value":"1"},{"value":1}))
        self.assertFalse(snapshots_equal({"value":1.0},{"value":1}))

    def test_nonfinite_values_are_not_valid_committed_json(self):
        for value in ("NaN","Infinity","-Infinity"):
            with self.assertRaises(ValueError):
                strict_json("{\"value\":"+value+"}")

    def test_harness_refuses_unknown_phase(self):
        w = object.__new__(World)
        w.catalog = {"points": {"store": "registered-store"}}
        with self.assertRaises(AssertionError):
            w.phase("../../token")
        self.assertEqual(w.phase("store"), "registered-store")

    def test_catalog_covers_only_explicit_clock_fields(self):
        self.assertNotIn("generation", CLOCK_FIELDS)
        self.assertNotIn("commit_id", CLOCK_FIELDS)
        self.assertNotIn("config", CLOCK_FIELDS)


if __name__ == "__main__":
    unittest.main()
