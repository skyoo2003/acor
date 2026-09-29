#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""Collect benchmark-v3.sh logs into the JSON shape used by the regression gate."""

import argparse
import json
import re
from datetime import date
from pathlib import Path


JSON_LINE = re.compile(r"\{.*\}")
ENV_LINE = re.compile(r"environment (.*)")


def collect(directory):
    runs = []
    for path in sorted(directory.glob("*-*-*.txt")):
        match = re.fullmatch(r"(\d+)-(shared|diverse|korean)-(\d+)\.txt", path.name)
        if not match:
            continue
        if not any(line.strip() == "PASS" for line in path.read_text().splitlines()):
            raise ValueError(f"failed or incomplete run: {path}")
        n, kind, repeat = int(match[1]), match[2], int(match[3])
        measurements = []
        environment = ""
        for line in path.read_text().splitlines():
            env = ENV_LINE.search(line)
            if env:
                environment = env.group(1)
            item = JSON_LINE.search(line)
            if item:
                try:
                    measurement = json.loads(item.group())
                except json.JSONDecodeError:
                    continue
                if "operation" in measurement:
                    measurements.append(measurement)
        if not measurements:
            raise ValueError(f"no measurements in {path}")
        runs.append({"n": n, "kind": kind, "repeat": repeat,
                     "measurements": measurements, "environment": environment,
                     "passed": True})
    if not runs:
        raise ValueError(f"no benchmark runs in {directory}")
    return {"date": date.today().isoformat(), "runs": runs}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.write_text(json.dumps(collect(args.directory), indent=2) + "\n")


if __name__ == "__main__":
    main()
