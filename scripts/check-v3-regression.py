#!/usr/bin/env python3
"""Fail when current V3 measurements regress beyond a bounded threshold."""

import argparse
import json
import statistics
import sys
from pathlib import Path


METRICS = {
    "ready_ms": "ready_ms",
    "search_p95_ns": "search_p95_ns",
    "max_rss_bytes": "max_rss_bytes",
}


def load(path):
    data = json.loads(path.read_text())
    rows = {}
    for run in data.get("runs", []):
        for measurement in run.get("measurements", []):
            key = (run.get("n"), run.get("kind"), measurement.get("operation"))
            if None not in key:
                rows.setdefault(key, []).append(measurement)
    if not rows:
        raise ValueError(f"no V3 measurements in {path}")
    return rows


def medians(rows, field):
    """Median each operation within a workload, then median those operations."""
    workloads = {}
    for (n, kind, operation), measurements in rows.items():
        if not all(field in row for row in measurements):
            continue
        workloads.setdefault((n, kind), {})[operation] = statistics.median(
            row[field] for row in measurements)
    return {key: statistics.median(operation_values.values())
            for key, operation_values in workloads.items()}


def compare(baseline, candidate, threshold):
    checks = []
    for label, field in METRICS.items():
        before = medians(baseline, field)
        after = medians(candidate, field)
        missing = sorted(set(before) - set(after))
        if missing:
            raise ValueError(f"candidate is missing {label} measurements: {missing[:3]}")
        for key in sorted(set(before) & set(after)):
            limit = before[key] * (1 + threshold)
            checks.append({
                "metric": label,
                "key": key,
                "aggregation": "median of operation medians",
                "baseline": before[key],
                "candidate": after[key],
                "limit": limit,
                "ok": after[key] <= limit,
            })
    if not checks:
        raise ValueError("baseline and candidate have no comparable measurements")
    return checks


def self_test():
    rows = {(1, "shared", "add_1"): [{"ready_ms": 10, "search_p95_ns": 20, "max_rss_bytes": 30}]}
    assert compare(rows, rows, 0) and all(item["ok"] for item in compare(rows, rows, 0))
    slower = {(1, "shared", "add_1"): [{"ready_ms": 13, "search_p95_ns": 20, "max_rss_bytes": 30}]}
    assert not compare(rows, slower, 0.1)[0]["ok"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path, nargs="?")
    parser.add_argument("candidate", type=Path, nargs="?")
    parser.add_argument("--max-regression", type=float, default=0.25,
                        help="allowed relative increase, default 0.25")
    parser.add_argument("--self-test", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.max_regression < 0:
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
