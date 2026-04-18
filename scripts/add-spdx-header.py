#!/usr/bin/env python3
"""Pre-commit hook: Add SPDX-License-Identifier header to Go files."""
import re
import sys

HEADER = "// SPDX-License-Identifier: Apache-2.0\n"
BUILD_TAG_RE = re.compile(r"^(//go:build|// \+build)")


def add_spdx_header(path: str) -> None:
    with open(path, "r") as f:
        content = f.read()
    if "SPDX-License-Identifier" in content:
        return
    lines = content.splitlines(True)
    insert_at = 0
    for i, line in enumerate(lines):
        if BUILD_TAG_RE.match(line):
            insert_at = i + 1
        elif line.strip() == "" and insert_at == i:
            insert_at = i + 1
        else:
            break
    lines.insert(insert_at, HEADER)
    with open(path, "w") as f:
        f.writelines(lines)
    print(f"Added SPDX header to {path}")


def main() -> None:
    for path in sys.argv[1:]:
        add_spdx_header(path)


if __name__ == "__main__":
    main()
