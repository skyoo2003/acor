#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""Pre-commit hook: add Apache-2.0 SPDX headers to authored source files."""
from pathlib import Path
import re
import sys

GO_PROTO_HEADER = "// SPDX-License-Identifier: Apache-2.0\n"
SCRIPT_HEADER = "# SPDX-License-Identifier: Apache-2.0\n"
BUILD_TAG_RE = re.compile(r"^(//go:build|// \+build)")


def add_spdx_header(path: str) -> None:
    suffix = Path(path).suffix
    if suffix not in {".go", ".proto", ".py", ".sh"}:
        return
    with open(path, "r") as f:
        content = f.read()
    if "SPDX-License-Identifier" in content:
        return
    lines = content.splitlines(True)
    header = GO_PROTO_HEADER if suffix in {".go", ".proto"} else SCRIPT_HEADER
    if suffix == ".go":
        insert_at = 0
        for i, line in enumerate(lines):
            if BUILD_TAG_RE.match(line):
                insert_at = i + 1
            elif line.strip() == "" and insert_at == i:
                insert_at = i + 1
            else:
                break
    else:
        insert_at = 1 if lines and lines[0].startswith("#!") else 0
    lines.insert(insert_at, header)
    with open(path, "w") as f:
        f.writelines(lines)
    print(f"Added SPDX header to {path}")


def main() -> None:
    for path in sys.argv[1:]:
        add_spdx_header(path)


if __name__ == "__main__":
    main()
