#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Run against one disposable Redis/Valkey endpoint at a time. No FLUSHDB is used.
set -eu
: "${ACOR_V3_SCALE_ADDR:?set ACOR_V3_SCALE_ADDR to a disposable real server}"
repeats=${ACOR_V3_SCALE_REPEATS:-3}
output=${ACOR_V3_SCALE_OUTPUT:-/tmp/acor-v3-results}
counts=${ACOR_V3_SCALE_COUNTS:-"1000000 10000000 ${ACOR_V3_SCALE_UPPER_N:?set a capacity-approved upper-bound keyword count, or override ACOR_V3_SCALE_COUNTS for a smoke run}"}
# A bounded profile matrix, never a sweep across all supported shard counts.
profiles=${ACOR_V3_SCALE_PROFILES:-"1 4 16"}
mkdir -p "$output"
binary=$(mktemp /tmp/acor-v3-test.XXXXXX)
trap 'rm -f "$binary"' EXIT HUP INT TERM
go test -c ./pkg/acor -o "$binary"
repeat=1
while [ "$repeat" -le "$repeats" ]; do
  for count in $counts; do
    for kind in shared diverse korean; do
      for shards in $profiles; do
        result="$output/$count-$kind-$shards-$repeat.txt"
        ACOR_V3_SCALE_N=$count ACOR_V3_SCALE_KIND=$kind ACOR_V3_SCALE_SHARDS=$shards \
          "$binary" -test.run='^TestVersionedScale$' -test.v -test.timeout="${ACOR_V3_SCALE_TIMEOUT:-60m}" > "$result" 2>&1
        echo "completed $count $kind shards $shards repeat $repeat"
      done
    done
  done
  repeat=$((repeat + 1))
done
"$binary" -test.run='^TestVersionedMillionSafety$' -test.v -test.timeout=15m \
  > "$output/million-safety.txt" 2>&1
python3 scripts/collect-v3-results.py "$output" "$output/results.json"
