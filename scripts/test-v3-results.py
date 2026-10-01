#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""Exercise the real V3 collector and gate with bounded, comparable fixtures."""

import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent


def fixture():
    return {"schema_version": 2, "runs": [{
        "n": 100, "kind": "shared", "repeat": 1, "shard_count": 4,
        "environment": {"host": "fixture", "go": "go1.26.7", "os": "linux", "arch": "amd64",
                        "cpus": 4, "gomaxprocs": 4, "endpoint": "fixture:6379",
                        "server": {"redis_version": "8.10.1", "redis_build_id": "fixture",
                                   "redis_mode": "standalone", "os": "Linux", "arch_bits": "64"}},
        "passed": True,
        "measurements": [{"operation": "add_1", "n": 100, "kind": "shared",
                          "shard_count": 4, "changed_shards": 1,
                          "shard_min_keywords": 20, "shard_max_keywords": 30,
                          "shard_skew_ratio": 1.2, "commit_ms": 10,
                          "ready_ms": 30, "refresh_lag_ms": 20,
                          "max_rss_bytes": 1000, "gc_pause_ns": 10, "gc_cycles": 1,
                          "search_samples": 10, "search_p50_ns": 10,
                          "search_p95_ns": 20, "search_p99_ns": 30}]}]}


class ResultsTest(unittest.TestCase):
    def gate(self, candidate, baseline=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "baseline.json").write_text(json.dumps(baseline or fixture()))
            (root / "candidate.json").write_text(json.dumps(candidate))
            return subprocess.run([sys.executable, str(SCRIPTS / "check-v3-regression.py"),
                                   str(root / "baseline.json"), str(root / "candidate.json")],
                                  capture_output=True, text=True, check=False)

    def test_same_environment_passes(self):
        self.assertEqual(self.gate(fixture()).returncode, 0)

    def test_missing_comparable_metrics_rejected(self):
        for field in ("changed_shards", "shard_skew_ratio", "refresh_lag_ms", "gc_pause_ns"):
            with self.subTest(field=field):
                candidate = fixture()
                del candidate["runs"][0]["measurements"][0][field]
                result = self.gate(candidate)
                self.assertNotEqual(result.returncode, 0, result.stdout)

    def test_environment_mismatch_rejected(self):
        candidate = fixture()
        candidate["runs"][0]["environment"]["host"] = "another-host"
        self.assertNotEqual(self.gate(candidate).returncode, 0)

    def test_missing_environment_identity_rejected_even_when_equal(self):
        for field in ("host", "go", "os", "arch", "cpus", "gomaxprocs", "endpoint", "server"):
            with self.subTest(field=field):
                candidate = fixture()
                del candidate["runs"][0]["environment"][field]
                self.assertNotEqual(self.gate(candidate, candidate).returncode, 0)

        for field in ("redis_version", "redis_build_id", "redis_mode", "os", "arch_bits"):
            with self.subTest(server_field=field):
                candidate = fixture()
                del candidate["runs"][0]["environment"]["server"][field]
                self.assertNotEqual(self.gate(candidate, candidate).returncode, 0)
        for field, value in (("host", " "), ("go", ""), ("cpus", 0), ("gomaxprocs", True), ("server", {})):
            with self.subTest(field=field, value=value):
                candidate = fixture()
                candidate["runs"][0]["environment"][field] = value
                self.assertNotEqual(self.gate(candidate, candidate).returncode, 0)

    def test_valkey_identity_passes(self):
        candidate = fixture()
        server = candidate["runs"][0]["environment"]["server"]
        server["valkey_version"] = server.pop("redis_version")
        server["valkey_build_id"] = server.pop("redis_build_id")
        self.assertEqual(self.gate(candidate, candidate).returncode, 0)

    def test_changed_shard_and_repetition_mismatch_rejected(self):
        candidate = fixture()
        candidate["runs"][0]["measurements"][0]["changed_shards"] = 2
        self.assertNotEqual(self.gate(candidate).returncode, 0)
        candidate = fixture()
        second = copy.deepcopy(candidate["runs"][0])
        second["repeat"] = 2
        candidate["runs"].append(second)
        self.assertNotEqual(self.gate(candidate).returncode, 0)

    def test_legacy_schema_rejected(self):
        candidate = fixture()
        del candidate["schema_version"]
        self.assertNotEqual(self.gate(candidate).returncode, 0)

    def test_missing_operation_rejected(self):
        baseline = fixture()
        extra = copy.deepcopy(baseline["runs"][0]["measurements"][0])
        extra["operation"] = "remove_1"
        baseline["runs"][0]["measurements"].append(extra)
        self.assertNotEqual(self.gate(fixture(), baseline).returncode, 0)

    def test_crud_regression_rejected(self):
        candidate = fixture()
        candidate["runs"][0]["measurements"][0]["commit_ms"] = 20
        self.assertEqual(self.gate(candidate).returncode, 1)

    def test_invalid_or_unsampled_metrics_rejected(self):
        for field, value in (("search_samples", 0), ("search_p95_ns", float("nan")),
                             ("changed_shards", 5), ("shard_skew_ratio", -1),
                             ("search_samples", 1.5), ("gc_cycles", 0.5),
                             ("shard_min_keywords", 31), ("shard_skew_ratio", 5)):
            with self.subTest(field=field):
                candidate = fixture()
                candidate["runs"][0]["measurements"][0][field] = value
                self.assertNotEqual(self.gate(candidate).returncode, 0)

    def test_collector_preserves_shard_profile_and_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = fixture()["runs"][0]
            (root / "100-shared-4-1.txt").write_text(
                "environment " + json.dumps(run["environment"]) + "\n" +
                json.dumps(run["measurements"][0]) + "\nPASS\n")
            result = subprocess.run([sys.executable, str(SCRIPTS / "collect-v3-results.py"),
                                     str(root), str(root / "results.json")],
                                    capture_output=True, text=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            collected = json.loads((root / "results.json").read_text())
            self.assertEqual(collected["schema_version"], 2)
            self.assertEqual(collected["runs"][0]["shard_count"], 4)
            self.assertEqual(collected["runs"][0]["environment"], run["environment"])


if __name__ == "__main__":
    unittest.main()
