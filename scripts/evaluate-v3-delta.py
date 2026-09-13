#!/usr/bin/env python3
"""Evaluate paired million-keyword delta-search runs from benchmark-v3.sh."""

import argparse
import json
import re
import statistics
import sys
from pathlib import Path


MEASURED = ("add_1", "remove_1", "add_128", "remove_128", "remove_base_1")
RSS_PHASES = ("add_129", "remove_129", "add_1_compact", "post_compaction")
KINDS = ("shared", "diverse", "korean")
LOG_LINE = re.compile(r"\{.*\}")


def records(path):
    if not path.is_file():
        raise ValueError(f"missing run: {path}")
    result = {}
    lines = path.read_text().splitlines()
    if not any(line == "PASS" for line in lines) or any(line.startswith("--- FAIL:") for line in lines):
        raise ValueError(f"failed or incomplete test: {path}")
    for line in lines:
        match = LOG_LINE.search(line)
        if match:
            try:
                item = json.loads(match.group())
            except json.JSONDecodeError:
                continue
            if "operation" in item:
                result[item["operation"]] = item
    if not all(op in result for op in (*MEASURED, *RSS_PHASES)):
        raise ValueError(f"incomplete run: {path}")
    return result


def median(values):
    return statistics.median(values)


def evaluate(directory, repeats):
    checks = []
    for kind in KINDS:
        runs = {mode: [records(directory / f"1000000-{kind}-{mode}-{i}.txt")
                       for i in range(1, repeats + 1)]
                for mode in ("baseline", "delta")}
        for op in MEASURED:
            base = runs["baseline"]
            changed = runs["delta"]
            b_ready = median([r[op]["ready_ms"] for r in base])
            d_ready = median([r[op]["ready_ms"] for r in changed])
            b_search = median([r[op]["search_p95_ns"] for r in base])
            d_search = median([r[op]["search_p95_ns"] for r in changed])
            search_ok = d_search <= b_search * 1.10
            ready_ok = d_ready <= b_ready * 0.30
            api_compaction_overlap = any(
                r[op].get("building_during_api", False) for mode in ("baseline", "delta")
                for r in runs[mode])
            checks.append({"kind": kind, "operation": op,
                           "ready_baseline_ms": b_ready, "ready_delta_ms": d_ready,
                           "ready_ok": ready_ok, "p95_baseline_ns": b_search,
                           "p95_delta_ns": d_search, "p95_ok": search_ok,
                           "api_compaction_overlap": api_compaction_overlap,
                           "steady_ok": not api_compaction_overlap,
                           "rss_highwater_baseline_bytes": median([
                               r[op]["rss_highwater_at_sample_bytes"] for r in base]),
                           "rss_highwater_delta_bytes": median([
                               r[op]["rss_highwater_at_sample_bytes"] for r in changed]),
                           "heap_alloc_baseline_bytes": median([
                               r[op]["heap_alloc_at_sample_bytes"] for r in base]),
                           "heap_alloc_delta_bytes": median([
                               r[op]["heap_alloc_at_sample_bytes"] for r in changed])})
            for api, inputs in base[0][op]["search_apis"].items():
                for input_kind in inputs:
                    b_api = median([r[op]["search_apis"][api][input_kind] for r in base])
                    d_api = median([r[op]["search_apis"][api][input_kind] for r in changed])
                    checks.append({"kind": kind, "operation": op, "api": api,
                                   "input": input_kind, "p95_baseline_ns": b_api,
                                   "p95_delta_ns": d_api, "p95_ok": d_api <= b_api * 1.10})
        for phase in RSS_PHASES:
            b_rss = median([r[phase]["rss_highwater_at_sample_bytes"]
                            for r in runs["baseline"]])
            d_rss = median([r[phase]["rss_highwater_at_sample_bytes"]
                            for r in runs["delta"]])
            item = {"kind": kind, "operation": phase + "_rss",
                    "baseline_bytes": b_rss, "delta_bytes": d_rss}
            if phase == "post_compaction":
                item["rss_ok"] = d_rss <= b_rss
                item["compaction_wait_ms"] = median([
                    r[phase]["compaction_wait_ms"] for r in runs["delta"]])
            checks.append(item)
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("result_dir", type=Path)
    parser.add_argument("--repeats", type=int, default=3)
    args = parser.parse_args()
    if args.repeats < 3:
        parser.error("release gate requires at least three independent runs")
    try:
        checks = evaluate(args.result_dir, args.repeats)
    except ValueError as exc:
        parser.error(str(exc))
    passed = all(all(value for key, value in item.items() if key.endswith("_ok"))
                 for item in checks)
    print(json.dumps({"passed": passed, "checks": checks}, indent=2))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
