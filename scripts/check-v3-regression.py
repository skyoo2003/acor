#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""Fail when current V3 measurements regress beyond a bounded threshold."""

import argparse
import json
import math
import statistics
import sys
from pathlib import Path


METRICS = ("ready_ms", "search_p95_ns", "refresh_lag_ms", "max_rss_bytes")
REQUIRED = (*METRICS, "commit_ms", "search_p50_ns", "search_p99_ns", "search_samples",
            "shard_count", "changed_shards", "shard_min_keywords", "shard_max_keywords",
            "shard_skew_ratio", "gc_pause_ns", "gc_cycles")


def identified_string(value):
    return isinstance(value, str) and bool(value.strip())


def validate_environment(environment, path):
    if not isinstance(environment, dict):
        raise ValueError(f"{path}: missing environment identity")
    for field in ("host", "go", "os", "arch", "endpoint"):
        if not identified_string(environment.get(field)):
            raise ValueError(f"{path}: missing or invalid environment {field}")
    for field in ("cpus", "gomaxprocs"):
        value = environment.get(field)
        if isinstance(value, bool) or not isinstance(value, int) or value < 1:
            raise ValueError(f"{path}: missing or invalid environment {field}")
    server = environment.get("server")
    if not isinstance(server, dict):
        raise ValueError(f"{path}: missing server identity")
    for alternatives in (("redis_version", "valkey_version"), ("redis_build_id", "valkey_build_id")):
        if not any(identified_string(server.get(field)) for field in alternatives):
            raise ValueError(f"{path}: missing server {'/'.join(alternatives)}")
    for field in ("redis_mode", "os", "arch_bits"):
        if not identified_string(server.get(field)):
            raise ValueError(f"{path}: missing or invalid server {field}")


def load(path):
    data = json.loads(path.read_text())
    if data.get("schema_version") != 2:
        raise ValueError(f"{path}: requires fresh schema_version 2 shard measurements")
    rows = {}
    seen_runs = set()
    for run in data.get("runs", []):
        n, kind, shards, repeat = (run.get(field) for field in ("n", "kind", "shard_count", "repeat"))
        if (not isinstance(n, int) or isinstance(n, bool) or n < 1 or kind not in ("shared", "diverse", "korean") or
                not isinstance(shards, int) or isinstance(shards, bool) or not 1 <= shards <= 256 or shards & (shards - 1) or
                not isinstance(repeat, int) or isinstance(repeat, bool) or repeat < 1):
            raise ValueError(f"{path}: invalid workload profile")
        run_key = (n, kind, shards, repeat)
        if run_key in seen_runs:
            raise ValueError(f"{path}: duplicate run {run_key}")
        seen_runs.add(run_key)
        environment = run.get("environment")
        if run.get("passed") is not True:
            raise ValueError(f"{path}: run lacks successful environment evidence")
        validate_environment(environment, path)
        seen_operations = set()
        if not run.get("measurements"):
            raise ValueError(f"{path}: run has no measurements")
        for measurement in run.get("measurements", []):
            operation = measurement.get("operation")
            if not isinstance(operation, str) or not operation or operation in seen_operations:
                raise ValueError(f"{path}: missing or duplicate operation")
            seen_operations.add(operation)
            key = (n, kind, shards, operation)
            for field in REQUIRED:
                value = measurement.get(field)
                if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
                    raise ValueError(f"{path}: missing or invalid {field} in {key}")
            if (measurement.get("n"), measurement.get("kind"), measurement["shard_count"]) != (n, kind, shards):
                raise ValueError(f"{path}: measurement profile differs from run")
            if measurement["changed_shards"] > shards or measurement["changed_shards"] % 1 or measurement["search_samples"] < 1:
                raise ValueError(f"{path}: invalid changed shards or no search samples in {key}")
            counts = ("search_samples", "gc_cycles", "shard_min_keywords", "shard_max_keywords")
            if any(measurement[field] % 1 for field in counts):
                raise ValueError(f"{path}: fractional count in {key}")
            minimum, maximum, skew = (measurement[field] for field in
                                      ("shard_min_keywords", "shard_max_keywords", "shard_skew_ratio"))
            if minimum > maximum or skew > shards or (maximum == 0 and skew != 0) or (maximum > 0 and skew < 1):
                raise ValueError(f"{path}: invalid shard balance in {key}")
            if not measurement["search_p50_ns"] <= measurement["search_p95_ns"] <= measurement["search_p99_ns"]:
                raise ValueError(f"{path}: unordered search percentiles in {key}")
            rows.setdefault(key, []).append({**measurement, "_environment": environment})
    if not rows:
        raise ValueError(f"no V3 measurements in {path}")
    return rows


def compare(baseline, candidate, threshold):
    if set(baseline) != set(candidate):
        raise ValueError("baseline and candidate must have identical workload/shard/operation keys")
    checks = []
    for key in sorted(baseline):
        old, new = baseline[key], candidate[key]
        environment = old[0]["_environment"]
        if len(old) != len(new) or any(row["_environment"] != environment for row in old + new):
            raise ValueError(f"environment or repetition count differs for {key}")
        if any(row["changed_shards"] != old[0]["changed_shards"] for row in old + new):
            raise ValueError(f"changed-shard count differs for {key}; CRUD costs are not comparable")
        fields = METRICS + (("commit_ms",) if key[-1].startswith(("add_", "remove_")) else ())
        for field in fields:
            before = statistics.median(row[field] for row in old)
            after = statistics.median(row[field] for row in new)
            limit = before * (1 + threshold)
            checks.append({
                "metric": field,
                "key": key,
                "aggregation": "median across repetitions of this operation",
                "baseline": before,
                "candidate": after,
                "limit": limit,
                "ok": after <= limit,
            })
    if not checks:
        raise ValueError("baseline and candidate have no comparable measurements")
    return checks


def self_test():
    rows = {(1, "shared", 4, "add_1"): [{"ready_ms": 10, "search_p95_ns": 20, "max_rss_bytes": 30,
                                         "refresh_lag_ms": 5, "commit_ms": 2, "changed_shards": 1, "_environment": {"test": True}}]}
    assert compare(rows, rows, 0) and all(item["ok"] for item in compare(rows, rows, 0))
    slower = {key: [{**value[0], "ready_ms": 13}] for key, value in rows.items()}
    assert not compare(rows, slower, 0.1)[0]["ok"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path, nargs="?")
    parser.add_argument("candidate", type=Path, nargs="?")
    parser.add_argument("--max-regression", type=float, default=0.25,
                        help="allowed relative increase, default 0.25")
    parser.add_argument("--self-test", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if not math.isfinite(args.max_regression) or args.max_regression < 0:
        parser.error("--max-regression must be non-negative")
    if args.self_test:
        self_test()
        return 0
    if args.baseline is None or args.candidate is None:
        parser.error("baseline and candidate are required")
    try:
        checks = compare(load(args.baseline), load(args.candidate), args.max_regression)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        parser.error(str(exc))
    failed = [item for item in checks if not item["ok"]]
    print(json.dumps({"passed": not failed, "checks": checks}, indent=2))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
