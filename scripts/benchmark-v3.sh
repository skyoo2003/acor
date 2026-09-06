#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Run against one disposable Redis/Valkey endpoint at a time. No FLUSHDB is used.
set -eu
: "${ACOR_V3_SCALE_ADDR:?set ACOR_V3_SCALE_ADDR to a disposable real server}"
repeats=${ACOR_V3_SCALE_REPEATS:-2}
output=${ACOR_V3_SCALE_OUTPUT:-/tmp/acor-v3-results}
mkdir -p "$output"
binary=$(mktemp /tmp/acor-v3-test.XXXXXX)
trap 'rm -f "$binary"' EXIT HUP INT TERM
go test -c ./pkg/acor -o "$binary"
repeat=1
while [ "$repeat" -le "$repeats" ]; do
  for count in 10000 100000 1000000; do
    for kind in shared diverse korean; do
      ACOR_V3_SCALE_N=$count ACOR_V3_SCALE_KIND=$kind \
        "$binary" -test.run='^TestVersionedScale$' -test.v -test.timeout=15m \
        > "$output/$count-$kind-$repeat.txt" 2>&1
      echo "completed $count $kind repeat $repeat"
    done
  done
  repeat=$((repeat + 1))
done
"$binary" -test.run='^TestVersionedMillionSafety$' -test.v -test.timeout=15m \
  > "$output/million-safety.txt" 2>&1
